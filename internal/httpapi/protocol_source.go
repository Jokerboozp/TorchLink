package httpapi

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"iot-platform/internal/protocolbuild"
	"iot-platform/internal/protocolworker"
)

const protocolSourceCases = `[{"name":"温度上报","input":{"payloadFormat":"hex","payload":"AA 01 2A"},"expectedMessageType":"PROPERTY_REPORT","expectedProperties":{"temperature":42}}]`

func (s *Server) protocolSourceTemplate(w http.ResponseWriter, r *http.Request) {
	write(w, 200, map[string]any{"filename": "protocol.go", "source": protocolbuild.Template, "cases": json.RawMessage(protocolSourceCases), "compilerAvailable": protocolbuild.Available(), "platform": runtime.GOOS + "-" + runtime.GOARCH})
}

func (s *Server) uploadProtocolSource(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, protocolbuild.MaxSource+(1<<20))
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
		problem(w, 422, "源码上传无效或超过 32 MiB")
		return
	}
	defer r.MultipartForm.RemoveAll()
	id := r.PathValue("id")
	if !protocolSegmentV2.MatchString(id) {
		problem(w, 422, "请填写有效的协议标识")
		return
	}
	tenant := claims(r).TenantID
	if !protocolSegmentV2.MatchString(tenant) {
		problem(w, 422, "租户标识不适用于协议制品存储")
		return
	}
	publish, err := formBoolStrict(r, "publish", true)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	if product := strings.TrimSpace(r.FormValue("productId")); product != "" {
		if !publish {
			problem(w, 422, "绑定产品须同时发布协议")
			return
		}
		if _, err := s.engine.Repo.GetProduct(r.Context(), tenant, product); err != nil {
			problem(w, 422, "绑定产品不存在")
			return
		}
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		problem(w, 422, "请选择 .go 源码文件或 Go 项目 ZIP")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, protocolbuild.MaxSource+1))
	if err != nil {
		problem(w, 422, "读取源码失败")
		return
	}
	files, err := protocolbuild.Sources(header.Filename, data)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	manifest, entrypoint, err := sourceProtocolManifest(r, id, files)
	if err != nil {
		problem(w, 422, err.Error())
		return
	}
	if _, err := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, id, manifest.Version); err == nil {
		problem(w, 409, "该协议版本已存在，请使用新版本号")
		return
	}
	casesData := []byte(strings.TrimSpace(r.FormValue("cases")))
	if len(casesData) == 0 {
		casesData = files["samples/cases.json"]
	}
	var cases []protocolPackageCaseV2
	if len(casesData) > 1<<20 || json.Unmarshal(casesData, &cases) != nil || len(cases) == 0 || len(cases) > 100 {
		problem(w, 422, "请提供 1 至 100 条样例报文及预期解析结果（页面填写或 ZIP 中 samples/cases.json）")
		return
	}
	for _, c := range cases {
		if c.ExpectedMessageType == "" || len(c.Input.Payload) == 0 {
			problem(w, 422, "每条样例须包含 input.payload 和 expectedMessageType")
			return
		}
	}
	select {
	case protocolbuild.Slots <- struct{}{}:
		defer func() { <-protocolbuild.Slots }()
	default:
		w.Header().Set("Retry-After", "5")
		problem(w, 429, "另一个协议正在编译或试跑，请稍后重试")
		return
	}
	started := time.Now()
	worker, buildLog, err := protocolbuild.Build(r.Context(), s.cfg.DataDir, files, entrypoint)
	if err != nil {
		write(w, 422, map[string]any{"detail": err.Error() + "\n" + buildLog, "stage": "compile", "buildLog": buildLog})
		return
	}
	digest := sha256.Sum256(data)
	manifestData, _ := yaml.Marshal(manifest)
	// Keep the exact uploaded source beside the generated binary and manifest.
	entries := map[string][]byte{"manifest.yaml": manifestData, "bin/worker": worker, "samples/cases.json": casesData, "source/upload.go": data}
	if operations := files["samples/operations.json"]; len(operations) > 0 {
		entries["samples/operations.json"] = operations
	}
	if strings.HasSuffix(strings.ToLower(header.Filename), ".zip") {
		delete(entries, "source/upload.go")
		entries["source/upload.zip"] = data
	}
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	for name, content := range entries {
		part, zipErr := zw.Create(name)
		if zipErr == nil {
			_, zipErr = part.Write(content)
		}
		if zipErr != nil {
			_ = zw.Close()
			problem(w, 500, "保存编译制品失败")
			return
		}
	}
	if err = zw.Close(); err != nil {
		problem(w, 500, "保存编译制品失败")
		return
	}
	if int64(archive.Len()) > maxProtocolPackageV2 {
		problem(w, 422, "源码与编译结果打包后超过 64 MiB")
		return
	}
	s.installProtocolPackageV2(w, r, archive.Bytes(), entries, manifest, header.Filename, map[string]any{"kind": "go-source", "sourceSha256": hex.EncodeToString(digest[:]), "durationMs": time.Since(started).Milliseconds(), "log": buildLog, "catalog": sourceCatalogProvenance(r.Context())})
}

func sourceProtocolManifest(r *http.Request, id string, files map[string][]byte) (protocolPackageManifestV2, string, error) {
	var metadata struct {
		ID            string   `json:"id"`
		Name          string   `json:"name"`
		Version       string   `json:"version"`
		Runtime       string   `json:"runtime"`
		Transport     string   `json:"transport"`
		PayloadFormat string   `json:"payloadFormat"`
		Capabilities  []string `json:"capabilities"`
		Entrypoint    string   `json:"entrypoint"`
		Description   string   `json:"description"`
	}
	if data := files["protocol.json"]; len(data) > 0 {
		if len(data) > 1<<20 || json.Unmarshal(data, &metadata) != nil {
			return protocolPackageManifestV2{}, "", errors.New("protocol.json 元数据无效")
		}
		if metadata.ID != "" && metadata.ID != id {
			return protocolPackageManifestV2{}, "", errors.New("protocol.json 的 id 必须与上传的协议标识一致")
		}
	}
	capabilities := metadata.Capabilities
	if input := strings.TrimSpace(r.FormValue("capabilities")); input != "" {
		if json.Unmarshal([]byte(input), &capabilities) != nil {
			return protocolPackageManifestV2{}, "", errors.New("capabilities 必须是 JSON 字符串数组")
		}
	}
	if len(capabilities) == 0 {
		capabilities = []string{"decode"}
	}
	manifest := protocolPackageManifestV2{SchemaVersion: 1, ID: id,
		Name: firstNonBlank(r.FormValue("name"), metadata.Name, id), Version: firstNonBlank(r.FormValue("version"), metadata.Version),
		Runtime:       firstNonBlank(r.FormValue("runtime"), metadata.Runtime, "go-json-lines-v1"),
		Transport:     strings.ToUpper(firstNonBlank(r.FormValue("transport"), metadata.Transport, "MQTT")),
		PayloadFormat: strings.ToLower(firstNonBlank(r.FormValue("payloadFormat"), metadata.PayloadFormat, "hex")),
		Description:   metadata.Description, Capabilities: capabilities,
		Entrypoints: map[string]string{runtime.GOOS + "-" + runtime.GOARCH: "bin/worker"}}
	if err := validateProtocolManifestV2(id, manifest); err != nil {
		return manifest, "", err
	}
	if manifest.Runtime == protocolworker.Runtime && manifest.PayloadFormat != "hex" {
		return manifest, "", errors.New("完整协议包的原始帧格式必须为 hex")
	}
	return manifest, firstNonBlank(r.FormValue("entrypoint"), metadata.Entrypoint, "."), nil
}
