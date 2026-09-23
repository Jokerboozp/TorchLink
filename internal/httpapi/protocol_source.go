package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"archive/zip"   /* 执行当前语句并推进处理流程。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"crypto/sha256" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"net/http"      /* 执行当前语句并推进处理流程。 */
	"runtime"       /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"gopkg.in/yaml.v3"                     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolbuild"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolworker" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

const protocolSourceCases = `[{"name":"温度上报","input":{"payloadFormat":"hex","payload":"AA 01 2A"},"expectedMessageType":"PROPERTY_REPORT","expectedProperties":{"temperature":42}}]` /* 声明 protocolSourceCases。 */

func (s *Server) protocolSourceTemplate(w http.ResponseWriter, r *http.Request) { /* 定义 protocolSourceTemplate 函数。 */
	if r.URL.Query().Get("format") == "go-functions" { /* 判断条件并选择处理分支。 */
		data, err := protocolbuild.FunctionTemplateZIP(r.URL.Query().Get("kind")) /* 更新 err 的值。 */
		if err != nil {                                                           /* 判断条件并选择处理分支。 */
			problem(w, 500, "生成 Go 模板失败") /* 执行当前语句并推进处理流程。 */
			return                        /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		w.Header().Set("Content-Type", "application/zip")                               /* 执行当前语句并推进处理流程。 */
		w.Header().Set("Content-Disposition", `attachment; filename="go-protocol.zip"`) /* 执行当前语句并推进处理流程。 */
		_, _ = w.Write(data)                                                            /* 更新 _ 的值。 */
		return                                                                          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, map[string]any{"filename": "protocol.go", "source": protocolbuild.Template, "cases": json.RawMessage(protocolSourceCases), "compilerAvailable": protocolbuild.Available(), "platform": runtime.GOOS + "-" + runtime.GOARCH, "targetPlatforms": model.ProtocolPlatforms()}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) uploadProtocolSource(w http.ResponseWriter, r *http.Request) { /* 定义 uploadProtocolSource 函数。 */
	r.Body = http.MaxBytesReader(w, r.Body, protocolbuild.MaxSource+(1<<20)) /* 更新 r.Body 的值。 */
	if err := r.ParseMultipartForm(2 << 20); err != nil {                    /* 判断条件并选择处理分支。 */
		if r.MultipartForm != nil { /* 判断条件并选择处理分支。 */
			_ = r.MultipartForm.RemoveAll() /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
		problem(w, 422, "源码上传无效或超过 32 MiB") /* 执行当前语句并推进处理流程。 */
		return                              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer r.MultipartForm.RemoveAll()       /* 安排函数结束时执行清理。 */
	id := r.PathValue("id")                 /* 更新 id 的值。 */
	if !protocolSegmentV2.MatchString(id) { /* 判断条件并选择处理分支。 */
		problem(w, 422, "请填写有效的协议标识") /* 执行当前语句并推进处理流程。 */
		return                        /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	tenant := claims(r).TenantID                /* 更新 tenant 的值。 */
	if !protocolSegmentV2.MatchString(tenant) { /* 判断条件并选择处理分支。 */
		problem(w, 422, "租户标识不适用于协议制品存储") /* 执行当前语句并推进处理流程。 */
		return                            /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	publish, err := formBoolStrict(r, "publish", true) /* 更新 err 的值。 */
	if err != nil {                                    /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if product := strings.TrimSpace(r.FormValue("productId")); product != "" { /* 判断条件并选择处理分支。 */
		if !publish { /* 判断条件并选择处理分支。 */
			problem(w, 422, "绑定产品须同时发布协议") /* 执行当前语句并推进处理流程。 */
			return                         /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if _, err := s.engine.Repo.GetProduct(r.Context(), tenant, product); err != nil { /* 判断条件并选择处理分支。 */
			problem(w, 422, "绑定产品不存在") /* 执行当前语句并推进处理流程。 */
			return                     /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	file, header, err := r.FormFile("file") /* 更新 err 的值。 */
	if err != nil {                         /* 判断条件并选择处理分支。 */
		problem(w, 422, "请选择 .go 源码文件或 Go 项目 ZIP") /* 执行当前语句并推进处理流程。 */
		return                                     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer file.Close()                                                       /* 安排函数结束时执行清理。 */
	data, err := io.ReadAll(io.LimitReader(file, protocolbuild.MaxSource+1)) /* 更新 err 的值。 */
	if err != nil {                                                          /* 判断条件并选择处理分支。 */
		problem(w, 422, "读取源码失败") /* 执行当前语句并推进处理流程。 */
		return                    /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	files, err := protocolbuild.Sources(header.Filename, data) /* 更新 err 的值。 */
	if err != nil {                                            /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	// Hold the existing compiler slot through discovery, compilation and validation.
	select { /* 根据条件选择处理路径。 */
	case protocolbuild.Slots <- struct{}{}: /* 处理当前分支。 */
		defer func() { <-protocolbuild.Slots }() /* 安排函数结束时执行清理。 */
	default: /* 处理当前分支。 */
		w.Header().Set("Retry-After", "5")    /* 执行当前语句并推进处理流程。 */
		problem(w, 429, "另一个协议正在编译或试跑，请稍后重试") /* 执行当前语句并推进处理流程。 */
		return                                /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	started := time.Now()                                   /* 更新 started 的值。 */
	functions, err := protocolbuild.PrepareFunctions(files) /* 更新 err 的值。 */
	if err != nil {                                         /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var nativeWorker []byte /* 声明 nativeWorker。 */
	var nativeLog string    /* 声明 nativeLog。 */
	if functions {          /* 判断条件并选择处理分支。 */
		if entry := strings.TrimSpace(r.FormValue("entrypoint")); entry != "" && entry != "." { /* 判断条件并选择处理分支。 */
			problem(w, 422, "Go 函数模式使用项目根目录，请清空高级编译入口") /* 执行当前语句并推进处理流程。 */
			return                                      /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		nativeWorker, nativeLog, err = protocolbuild.Build(r.Context(), s.cfg.DataDir, files, ".") /* 更新 err 的值。 */
		if err != nil {                                                                            /* 判断条件并选择处理分支。 */
			write(w, 422, map[string]any{"detail": err.Error() + "\n" + nativeLog, "stage": "compile", "buildLog": nativeLog}) /* 执行当前语句并推进处理流程。 */
			return                                                                                                             /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		description, err := protocolbuild.DescribeFunctions(r.Context(), nativeWorker) /* 更新 err 的值。 */
		if err != nil {                                                                /* 判断条件并选择处理分支。 */
			problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
			return                       /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		var metadata map[string]any                                                       /* 声明 metadata。 */
		if json.Unmarshal(description["metadata"], &metadata) != nil || metadata == nil { /* 判断条件并选择处理分支。 */
			problem(w, 422, "Go 协议配置无效") /* 执行当前语句并推进处理流程。 */
			return                       /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if version, _ := metadata["version"].(string); strings.TrimSpace(version) == "" { /* 判断条件并选择处理分支。 */
			metadata["version"] = "auto-" + time.Now().UTC().Format("20060102-150405.000000000") /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		// The runtime and capabilities come from actual registered Go functions.
		for _, field := range []string{"runtime", "capabilities"} { /* 循环处理当前数据。 */
			if strings.TrimSpace(r.FormValue(field)) != "" { /* 判断条件并选择处理分支。 */
				problem(w, 422, "Go 函数模式自动识别协议能力，请清空高级运行时和能力设置") /* 执行当前语句并推进处理流程。 */
				return                                           /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		files["protocol.json"], _ = json.Marshal(metadata) /* 执行当前语句并推进处理流程。 */
		if len(files["samples/cases.json"]) == 0 {         /* 判断条件并选择处理分支。 */
			files["samples/cases.json"] = description["cases"] /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if ops := description["operations"]; len(ops) > 0 && string(ops) != "[]" && string(ops) != "null" { /* 判断条件并选择处理分支。 */
			files["samples/operations.json"] = ops /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	manifest, entrypoint, err := sourceProtocolManifest(r, id, files) /* 更新 err 的值。 */
	if err != nil {                                                   /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	targets, err := sourceTargetPlatforms(r, files) /* 更新 err 的值。 */
	if err != nil {                                 /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if _, err := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, id, manifest.Version); err == nil { /* 判断条件并选择处理分支。 */
		problem(w, 409, "该协议版本已存在，请使用新版本号") /* 执行当前语句并推进处理流程。 */
		return                              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	casesData := []byte(strings.TrimSpace(r.FormValue("cases"))) /* 更新 casesData 的值。 */
	if len(casesData) == 0 {                                     /* 判断条件并选择处理分支。 */
		casesData = files["samples/cases.json"] /* 更新 casesData 的值。 */
	} /* 结束当前表达式或代码块。 */
	var cases []protocolPackageCaseV2                                                                              /* 声明 cases。 */
	if len(casesData) > 1<<20 || json.Unmarshal(casesData, &cases) != nil || len(cases) == 0 || len(cases) > 100 { /* 判断条件并选择处理分支。 */
		problem(w, 422, "请提供 1 至 100 条样例报文及预期结果：Go 函数模式在 Protocol 中填写 Samples，完整 Go 项目可用 samples/cases.json") /* 执行当前语句并推进处理流程。 */
		return                                                                                                /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, c := range cases { /* 循环处理当前数据。 */
		if c.ExpectedMessageType == "" || len(c.Input.Payload) == 0 { /* 判断条件并选择处理分支。 */
			problem(w, 422, "每条样例须包含 input.payload 和 expectedMessageType") /* 执行当前语句并推进处理流程。 */
			return                                                         /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	entries := map[string][]byte{"samples/cases.json": casesData, "source/upload.go": data} /* 更新 entries 的值。 */
	buildLogs := map[string]string{}                                                        /* 更新 buildLogs 的值。 */
	buildStates := map[string]string{}                                                      /* 更新 buildStates 的值。 */
	for _, target := range targets {                                                        /* 循环处理当前数据。 */
		worker, buildLog, buildErr := nativeWorker, nativeLog, error(nil) /* 检查错误并决定后续处理。 */
		if worker == nil || target != runtime.GOOS+"-"+runtime.GOARCH {   /* 判断条件并选择处理分支。 */
			worker, buildLog, buildErr = protocolbuild.BuildForPlatform(r.Context(), s.cfg.DataDir, files, entrypoint, target) /* 更新 buildErr 的值。 */
		} /* 结束当前表达式或代码块。 */
		if buildErr != nil { /* 判断条件并选择处理分支。 */
			write(w, 422, map[string]any{"detail": target + ": " + buildErr.Error() + "\n" + buildLog, "stage": "compile", "platform": target, "buildLog": buildLog}) /* 执行当前语句并推进处理流程。 */
			return                                                                                                                                                    /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		name := "bin/worker-" + target      /* 更新 name 的值。 */
		entries[name] = worker              /* 更新 entries[name] 的值。 */
		manifest.Entrypoints[target] = name /* 更新 manifest.Entrypoints[target] 的值。 */
		buildLogs[target] = buildLog        /* 更新 buildLogs[target] 的值。 */
		buildStates[target] = "COMPILED"    /* 更新 buildStates[target] 的值。 */
	} /* 结束当前表达式或代码块。 */
	digest := sha256.Sum256(data)                                            /* 更新 digest 的值。 */
	manifestData, _ := yaml.Marshal(manifest)                                /* 更新 _ 的值。 */
	entries["manifest.yaml"] = manifestData                                  /* 执行当前语句并推进处理流程。 */
	if operations := files["samples/operations.json"]; len(operations) > 0 { /* 判断条件并选择处理分支。 */
		entries["samples/operations.json"] = operations /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if strings.HasSuffix(strings.ToLower(header.Filename), ".zip") { /* 判断条件并选择处理分支。 */
		delete(entries, "source/upload.go") /* 执行当前语句并推进处理流程。 */
		entries["source/upload.zip"] = data /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	var archive bytes.Buffer             /* 声明 archive。 */
	zw := zip.NewWriter(&archive)        /* 更新 zw 的值。 */
	for name, content := range entries { /* 循环处理当前数据。 */
		part, zipErr := zw.Create(name) /* 更新 zipErr 的值。 */
		if zipErr == nil {              /* 判断条件并选择处理分支。 */
			_, zipErr = part.Write(content) /* 更新 zipErr 的值。 */
		} /* 结束当前表达式或代码块。 */
		if zipErr != nil { /* 判断条件并选择处理分支。 */
			_ = zw.Close()              /* 更新 _ 的值。 */
			problem(w, 500, "保存编译制品失败") /* 执行当前语句并推进处理流程。 */
			return                      /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err = zw.Close(); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 500, "保存编译制品失败") /* 执行当前语句并推进处理流程。 */
		return                      /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if int64(archive.Len()) > maxProtocolPackageV2 { /* 判断条件并选择处理分支。 */
		problem(w, 422, "源码与编译结果打包后超过 64 MiB") /* 执行当前语句并推进处理流程。 */
		return                                 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.installProtocolPackageV2(w, r, archive.Bytes(), entries, manifest, header.Filename, map[string]any{"kind": "go-source", "sourceSha256": hex.EncodeToString(digest[:]), "durationMs": time.Since(started).Milliseconds(), "log": buildLogs[runtime.GOOS+"-"+runtime.GOARCH], "logs": buildLogs, "targets": buildStates, "entrypoint": entrypoint}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func sourceProtocolManifest(r *http.Request, id string, files map[string][]byte) (protocolPackageManifestV2, string, error) { /* 定义 sourceProtocolManifest 函数。 */
	var metadata struct { /* 声明 metadata。 */
		ID            string   `json:"id"`            /* 执行当前语句并推进处理流程。 */
		Name          string   `json:"name"`          /* 执行当前语句并推进处理流程。 */
		Version       string   `json:"version"`       /* 执行当前语句并推进处理流程。 */
		Runtime       string   `json:"runtime"`       /* 执行当前语句并推进处理流程。 */
		Transport     string   `json:"transport"`     /* 执行当前语句并推进处理流程。 */
		PayloadFormat string   `json:"payloadFormat"` /* 执行当前语句并推进处理流程。 */
		Capabilities  []string `json:"capabilities"`  /* 执行当前语句并推进处理流程。 */
		Entrypoint    string   `json:"entrypoint"`    /* 执行当前语句并推进处理流程。 */
		Description   string   `json:"description"`   /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if data := files["protocol.json"]; len(data) > 0 { /* 判断条件并选择处理分支。 */
		if len(data) > 1<<20 || json.Unmarshal(data, &metadata) != nil { /* 判断条件并选择处理分支。 */
			return protocolPackageManifestV2{}, "", errors.New("protocol.json 元数据无效") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if metadata.ID != "" && metadata.ID != id { /* 判断条件并选择处理分支。 */
			return protocolPackageManifestV2{}, "", errors.New("protocol.json 的 id 必须与上传的协议标识一致") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	capabilities := metadata.Capabilities                                     /* 更新 capabilities 的值。 */
	if input := strings.TrimSpace(r.FormValue("capabilities")); input != "" { /* 判断条件并选择处理分支。 */
		if json.Unmarshal([]byte(input), &capabilities) != nil { /* 判断条件并选择处理分支。 */
			return protocolPackageManifestV2{}, "", errors.New("capabilities 必须是 JSON 字符串数组") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if len(capabilities) == 0 { /* 判断条件并选择处理分支。 */
		capabilities = []string{"decode"} /* 更新 capabilities 的值。 */
	} /* 结束当前表达式或代码块。 */
	manifest := protocolPackageManifestV2{SchemaVersion: 1, ID: id, /* 更新 manifest 的值。 */
		Name: firstNonBlank(r.FormValue("name"), metadata.Name, id), Version: firstNonBlank(r.FormValue("version"), metadata.Version), /* 执行当前语句并推进处理流程。 */
		Runtime:       firstNonBlank(r.FormValue("runtime"), metadata.Runtime, protocolworker.Runtime),             /* 执行当前语句并推进处理流程。 */
		Transport:     strings.ToUpper(firstNonBlank(r.FormValue("transport"), metadata.Transport, "MQTT")),        /* 执行当前语句并推进处理流程。 */
		PayloadFormat: strings.ToLower(firstNonBlank(r.FormValue("payloadFormat"), metadata.PayloadFormat, "hex")), /* 执行当前语句并推进处理流程。 */
		Description:   metadata.Description, Capabilities: capabilities,                                            /* 执行当前语句并推进处理流程。 */
		Entrypoints: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: "bin/worker"}} /* 执行当前语句并推进处理流程。 */
	if err := validateProtocolManifestV2(id, manifest); err != nil { /* 判断条件并选择处理分支。 */
		return manifest, "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if manifest.Runtime == protocolworker.Runtime && manifest.PayloadFormat != "hex" { /* 判断条件并选择处理分支。 */
		return manifest, "", errors.New("完整协议包的原始帧格式必须为 hex") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return manifest, firstNonBlank(r.FormValue("entrypoint"), metadata.Entrypoint, "."), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// A source package may request additional targets; native validation is mandatory.
func sourceTargetPlatforms(r *http.Request, files map[string][]byte) ([]string, error) { /* 定义 sourceTargetPlatforms 函数。 */
	var metadata struct { /* 声明 metadata。 */
		Targets []string `json:"targetPlatforms"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if data := files["protocol.json"]; len(data) > 0 { /* 判断条件并选择处理分支。 */
		if err := json.Unmarshal(data, &metadata); err != nil { /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if input := strings.TrimSpace(r.FormValue("targetPlatforms")); input != "" { /* 判断条件并选择处理分支。 */
		if len(input) > 1024 || json.Unmarshal([]byte(input), &metadata.Targets) != nil { /* 判断条件并选择处理分支。 */
			return nil, errors.New("targetPlatforms must be a JSON platform array") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if len(metadata.Targets) > 6 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("at most six protocol platforms are supported") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	native := runtime.GOOS + "-" + runtime.GOARCH                    /* 更新 native 的值。 */
	targets, seen := []string{native}, map[string]bool{native: true} /* 更新 seen 的值。 */
	for _, target := range metadata.Targets {                        /* 循环处理当前数据。 */
		normalized := model.ProtocolPlatform(target) /* 更新 normalized 的值。 */
		if normalized == "" {                        /* 判断条件并选择处理分支。 */
			return nil, errors.New("unsupported protocol target platform: " + target) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if !seen[normalized] { /* 判断条件并选择处理分支。 */
			targets = append(targets, normalized) /* 更新 targets 的值。 */
			seen[normalized] = true               /* 更新 seen[normalized] 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return targets, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
