package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"archive/zip"   /* 执行当前语句并推进处理流程。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"crypto/sha256" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"net/http"      /* 执行当前语句并推进处理流程。 */
	"os"            /* 执行当前语句并推进处理流程。 */
	"path/filepath" /* 执行当前语句并推进处理流程。 */
	"regexp"        /* 执行当前语句并推进处理流程。 */
	"runtime"       /* 执行当前语句并推进处理流程。 */
	"strconv"       /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/onboarding"      /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolruntime" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolworker"  /* 执行当前语句并推进处理流程。 */

	"gopkg.in/yaml.v3" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

var protocolSegmentV2 = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`) /* 声明 protocolSegmentV2。 */

func (s *Server) protocolDefinitionsV2(w http.ResponseWriter, r *http.Request) { /* 定义 protocolDefinitionsV2 函数。 */
	tenant := claims(r).TenantID                                                   /* 更新 tenant 的值。 */
	definitions, err := s.engine.Repo.ListProtocolDefinitions(r.Context(), tenant) /* 更新 err 的值。 */
	if err != nil {                                                                /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	releases, err := s.engine.Repo.ListProtocolReleases(r.Context(), tenant, "") /* 更新 err 的值。 */
	if err != nil {                                                              /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	byProtocol := map[string][]model.ProtocolRelease{} /* 更新 byProtocol 的值。 */
	for _, release := range releases {                 /* 循环处理当前数据。 */
		byProtocol[release.ProtocolID] = append(byProtocol[release.ProtocolID], release) /* 更新 byProtocol[release.ProtocolID] 的值。 */
	} /* 结束当前表达式或代码块。 */
	items := make([]map[string]any, 0, len(definitions)) /* 更新 items 的值。 */
	for _, definition := range definitions {             /* 循环处理当前数据。 */
		items = append(items, map[string]any{"definition": definition, "releases": byProtocol[definition.ID]}) /* 更新 items 的值。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, map[string]any{"items": items, "count": len(items)}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) saveProtocolDefinitionV2(w http.ResponseWriter, r *http.Request) { /* 定义 saveProtocolDefinitionV2 函数。 */
	var v model.ProtocolDefinition /* 声明 v。 */
	if decode(w, r, &v) != nil {   /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v.ID = strings.TrimSpace(v.ID)                            /* 更新 v.ID 的值。 */
	v.Name = strings.TrimSpace(v.Name)                        /* 更新 v.Name 的值。 */
	if !protocolSegmentV2.MatchString(v.ID) || v.Name == "" { /* 判断条件并选择处理分支。 */
		problem(w, 422, "id and name are required; id may contain letters, numbers, dot, underscore and hyphen") /* 执行当前语句并推进处理流程。 */
		return                                                                                                   /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v.TenantID = claims(r).TenantID                                                                 /* 更新 v.TenantID 的值。 */
	now := time.Now().UnixMilli()                                                                   /* 更新 now 的值。 */
	if old, err := s.engine.Repo.GetProtocolDefinition(r.Context(), v.TenantID, v.ID); err == nil { /* 判断条件并选择处理分支。 */
		v.CreatedAt = old.CreatedAt /* 更新 v.CreatedAt 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.CreatedAt == 0 { /* 判断条件并选择处理分支。 */
		v.CreatedAt = now /* 更新 v.CreatedAt 的值。 */
	} /* 结束当前表达式或代码块。 */
	v.UpdatedAt = now                                                            /* 更新 v.UpdatedAt 的值。 */
	if err := s.engine.Repo.SaveProtocolDefinition(r.Context(), v); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "protocol.v2.definition.save", "protocol", v.ID, nil) /* 执行当前语句并推进处理流程。 */
	write(w, 201, v)                                                 /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) protocolReleasesV2(w http.ResponseWriter, r *http.Request) { /* 定义 protocolReleasesV2 函数。 */
	items, err := s.engine.Repo.ListProtocolReleases(r.Context(), claims(r).TenantID, r.PathValue("id")) /* 更新 err 的值。 */
	if err != nil {                                                                                      /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, map[string]any{"items": items, "count": len(items)}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) createProtocolReleaseV2(w http.ResponseWriter, r *http.Request) { /* 定义 createProtocolReleaseV2 函数。 */
	var v model.ProtocolRelease  /* 声明 v。 */
	if decode(w, r, &v) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v.TenantID = claims(r).TenantID                                                                       /* 更新 v.TenantID 的值。 */
	v.ProtocolID = r.PathValue("id")                                                                      /* 更新 v.ProtocolID 的值。 */
	if _, err := s.engine.Repo.GetProtocolDefinition(r.Context(), v.TenantID, v.ProtocolID); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 404, "protocol definition not found") /* 执行当前语句并推进处理流程。 */
		return                                           /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !protocolSegmentV2.MatchString(v.Version) { /* 判断条件并选择处理分支。 */
		problem(w, 422, "version is required and contains unsupported characters") /* 执行当前语句并推进处理流程。 */
		return                                                                     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.ParserType == "" { /* 判断条件并选择处理分支。 */
		problem(w, 422, "parserType is required") /* 执行当前语句并推进处理流程。 */
		return                                    /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.ParserType == parser.GoProtocolParserName { /* 判断条件并选择处理分支。 */
		problem(w, 422, "custom Go releases must be uploaded as a versioned ZIP package") /* 执行当前语句并推进处理流程。 */
		return                                                                            /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.ParserType != "configurable_json_parser" && v.ParserType != "configurable_hex_parser" { /* 判断条件并选择处理分支。 */
		problem(w, 422, "unsupported protocol v2 parserType") /* 执行当前语句并推进处理流程。 */
		return                                                /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.Status == "" { /* 判断条件并选择处理分支。 */
		v.Status = "DRAFT" /* 更新 v.Status 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.Status != "DRAFT" && v.Status != "VALIDATED" { /* 判断条件并选择处理分支。 */
		problem(w, 422, "a release must be created as DRAFT or VALIDATED and published separately") /* 执行当前语句并推进处理流程。 */
		return                                                                                      /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.Transport == "" { /* 判断条件并选择处理分支。 */
		v.Transport = "MQTT" /* 更新 v.Transport 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.PayloadFormat == "" { /* 判断条件并选择处理分支。 */
		v.PayloadFormat = "json" /* 更新 v.PayloadFormat 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err := validateProtocolReleaseV2(v); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v.CreatedAt = time.Now().UnixMilli()                                        /* 更新 v.CreatedAt 的值。 */
	if err := s.engine.Repo.CreateProtocolRelease(r.Context(), v); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, http.StatusConflict, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "protocol.v2.release.create", "protocolRelease", v.ProtocolID+"@"+v.Version, map[string]any{"status": v.Status}) /* 执行当前语句并推进处理流程。 */
	write(w, 201, v)                                                                                                            /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type protocolPackageManifestV2 struct { /* 定义 protocolPackageManifestV2 类型。 */
	SchemaVersion int               `yaml:"schemaVersion"` /* 执行当前语句并推进处理流程。 */
	ID            string            `yaml:"id"`            /* 执行当前语句并推进处理流程。 */
	Name          string            `yaml:"name"`          /* 执行当前语句并推进处理流程。 */
	Version       string            `yaml:"version"`       /* 执行当前语句并推进处理流程。 */
	Transport     string            `yaml:"transport"`     /* 执行当前语句并推进处理流程。 */
	PayloadFormat string            `yaml:"payloadFormat"` /* 执行当前语句并推进处理流程。 */
	Runtime       string            `yaml:"runtime"`       /* 执行当前语句并推进处理流程。 */
	Entrypoints   map[string]string `yaml:"entrypoints"`   /* 执行当前语句并推进处理流程。 */
	Capabilities  []string          `yaml:"capabilities"`  /* 执行当前语句并推进处理流程。 */
	Description   string            `yaml:"description"`   /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

const maxProtocolPackageV2 = int64(64 << 20)          /* 声明 maxProtocolPackageV2。 */
const maxProtocolPackageExpandedV2 = int64(128 << 20) /* 声明 maxProtocolPackageExpandedV2。 */

func (s *Server) uploadProtocolPackageV2(w http.ResponseWriter, r *http.Request) { /* 定义 uploadProtocolPackageV2 函数。 */
	r.Body = http.MaxBytesReader(w, r.Body, maxProtocolPackageV2+(1<<20))      /* 更新 r.Body 的值。 */
	if err := r.ParseMultipartForm(maxProtocolPackageV2 + 1<<20); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 400, "invalid protocol package upload") /* 执行当前语句并推进处理流程。 */
		return                                             /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer r.MultipartForm.RemoveAll()          /* 安排函数结束时执行清理。 */
	file, header, err := r.FormFile("package") /* 更新 err 的值。 */
	if err != nil {                            /* 判断条件并选择处理分支。 */
		file, header, err = r.FormFile("file") /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 422, "package is required") /* 执行当前语句并推进处理流程。 */
		return                                 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer file.Close()                                                           /* 安排函数结束时执行清理。 */
	data, err := io.ReadAll(io.LimitReader(file, maxProtocolPackageV2+1))        /* 更新 err 的值。 */
	if err != nil || len(data) == 0 || int64(len(data)) > maxProtocolPackageV2 { /* 判断条件并选择处理分支。 */
		problem(w, 422, "protocol package must be between 1 byte and 64 MiB") /* 执行当前语句并推进处理流程。 */
		return                                                                /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data))) /* 更新 err 的值。 */
	if err != nil {                                                       /* 判断条件并选择处理分支。 */
		problem(w, 422, "protocol package must be a ZIP file") /* 执行当前语句并推进处理流程。 */
		return                                                 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	entries, err := inspectProtocolPackageV2(reader) /* 更新 err 的值。 */
	if err != nil {                                  /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	manifestData, ok := entries["manifest.yaml"] /* 更新 ok 的值。 */
	if !ok {                                     /* 判断条件并选择处理分支。 */
		manifestData = entries["manifest.yml"] /* 更新 manifestData 的值。 */
	} /* 结束当前表达式或代码块。 */
	if len(manifestData) == 0 { /* 判断条件并选择处理分支。 */
		problem(w, 422, "manifest.yaml is required at the package root") /* 执行当前语句并推进处理流程。 */
		return                                                           /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var manifest protocolPackageManifestV2                         /* 声明 manifest。 */
	if err = yaml.Unmarshal(manifestData, &manifest); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 422, "manifest.yaml is invalid: "+err.Error()) /* 执行当前语句并推进处理流程。 */
		return                                                    /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	protocolID := r.PathValue("id")                                         /* 更新 protocolID 的值。 */
	if err = validateProtocolManifestV2(protocolID, manifest); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.installProtocolPackageV2(w, r, data, entries, manifest, header.Filename, nil) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

// Source and prebuilt uploads share validation, immutable storage and binding.
func (s *Server) installProtocolPackageV2(w http.ResponseWriter, r *http.Request, data []byte, entries map[string][]byte, manifest protocolPackageManifestV2, filename string, buildInfo map[string]any) { /* 定义 installProtocolPackageV2 函数。 */
	protocolID := manifest.ID                          /* 更新 protocolID 的值。 */
	publish, err := formBoolStrict(r, "publish", true) /* 更新 err 的值。 */
	if err != nil {                                    /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	productID := strings.TrimSpace(r.FormValue("productId")) /* 更新 productID 的值。 */
	if productID != "" {                                     /* 判断条件并选择处理分支。 */
		if !publish { /* 判断条件并选择处理分支。 */
			problem(w, 422, "a product can only be bound when publish=true") /* 执行当前语句并推进处理流程。 */
			return                                                           /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if _, productErr := s.engine.Repo.GetProduct(r.Context(), claims(r).TenantID, productID); productErr != nil { /* 判断条件并选择处理分支。 */
			problem(w, 422, "productId does not reference an existing product") /* 执行当前语句并推进处理流程。 */
			return                                                              /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	targetPlatform := runtime.GOOS + "-" + runtime.GOARCH                                   /* 更新 targetPlatform 的值。 */
	entrypoint := filepath.ToSlash(strings.TrimSpace(manifest.Entrypoints[targetPlatform])) /* 更新 entrypoint 的值。 */
	worker, ok := entries[entrypoint]                                                       /* 更新 ok 的值。 */
	if !ok || len(worker) == 0 {                                                            /* 判断条件并选择处理分支。 */
		problem(w, 422, "package does not contain an entrypoint for "+targetPlatform) /* 执行当前语句并推进处理流程。 */
		return                                                                        /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	tenant, now := claims(r).TenantID, time.Now().UnixMilli()                                                            /* 更新 now 的值。 */
	if _, getErr := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, protocolID, manifest.Version); getErr == nil { /* 判断条件并选择处理分支。 */
		problem(w, 409, "protocol release already exists") /* 执行当前语句并推进处理流程。 */
		return                                             /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	root, err := filepath.Abs(s.cfg.DataDir) /* 更新 err 的值。 */
	if err != nil {                          /* 判断条件并选择处理分支。 */
		problem(w, 500, "resolve protocol data directory") /* 执行当前语句并推进处理流程。 */
		return                                             /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	directory := filepath.Join(root, "protocol-releases", tenant, protocolID, manifest.Version) /* 更新 directory 的值。 */
	if err = os.MkdirAll(directory, 0o700); err != nil {                                        /* 判断条件并选择处理分支。 */
		problem(w, 500, "create protocol release directory") /* 执行当前语句并推进处理流程。 */
		return                                               /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	packagePath := filepath.Join(directory, "package.zip") /* 更新 packagePath 的值。 */
	workerName := "artifact"                               /* 更新 workerName 的值。 */
	if runtime.GOOS == "windows" {                         /* 判断条件并选择处理分支。 */
		workerName += ".exe" /* 更新 workerName 的值。 */
	} /* 结束当前表达式或代码块。 */
	workerPath := filepath.Join(directory, workerName)                  /* 更新 workerPath 的值。 */
	if err = writeExclusiveFile(packagePath, data, 0o600); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 409, "protocol release artifact already exists") /* 执行当前语句并推进处理流程。 */
		return                                                      /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err = writeExclusiveFile(workerPath, worker, 0o700); err != nil { /* 判断条件并选择处理分支。 */
		_ = os.Remove(packagePath)                                /* 更新 _ 的值。 */
		problem(w, 409, "protocol release worker already exists") /* 执行当前语句并推进处理流程。 */
		return                                                    /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	packageDigest, workerDigest := sha256.Sum256(data), sha256.Sum256(worker) /* 更新 workerDigest 的值。 */
	relativeWorker, _ := filepath.Rel(root, workerPath)                       /* 更新 _ 的值。 */
	relativePackage, _ := filepath.Rel(root, packagePath)                     /* 更新 _ 的值。 */
	status, publishedAt := "VALIDATED", int64(0)                              /* 更新 publishedAt 的值。 */
	if publish {                                                              /* 判断条件并选择处理分支。 */
		status, publishedAt = "PUBLISHED", now /* 更新 publishedAt 的值。 */
	} /* 结束当前表达式或代码块。 */
	definition := model.ProtocolDefinition{ID: protocolID, TenantID: tenant, Name: firstNonBlank(manifest.Name, protocolID), Description: manifest.Description, CreatedAt: now, UpdatedAt: now} /* 更新 definition 的值。 */
	if old, getErr := s.engine.Repo.GetProtocolDefinition(r.Context(), tenant, protocolID); getErr == nil {                                                                                     /* 判断条件并选择处理分支。 */
		definition.CreatedAt = old.CreatedAt /* 更新 definition.CreatedAt 的值。 */
	} /* 结束当前表达式或代码块。 */
	artifact := map[string]any{"path": filepath.ToSlash(relativeWorker), "packagePath": filepath.ToSlash(relativePackage), "filename": filename, "sha256": hex.EncodeToString(workerDigest[:]), "packageSha256": hex.EncodeToString(packageDigest[:]), "size": len(worker), "runtime": manifest.Runtime, "platform": targetPlatform, "uploadedAt": now} /* 更新 artifact 的值。 */
	if buildInfo != nil {                                                                                                                                                                                                                                                                                                                               /* 判断条件并选择处理分支。 */
		artifact["build"] = buildInfo /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	// Every custom release carries executable regression cases, even when it
	// is uploaded as VALIDATED and published in a later operation.
	testCount, testErr := validateProtocolPackageCasesContextV2(r.Context(), root, artifact, entries, manifest, true) /* 更新 testErr 的值。 */
	if testErr != nil {                                                                                               /* 判断条件并选择处理分支。 */
		_ = os.Remove(workerPath)        /* 更新 _ 的值。 */
		_ = os.Remove(packagePath)       /* 更新 _ 的值。 */
		problem(w, 422, testErr.Error()) /* 执行当前语句并推进处理流程。 */
		return                           /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	artifact["testCases"] = testCount /* 执行当前语句并推进处理流程。 */
	if buildInfo != nil {             /* 判断条件并选择处理分支。 */
		if targets, ok := buildInfo["targets"].(map[string]string); ok { /* 判断条件并选择处理分支。 */
			targets[targetPlatform] = "PASSED" /* 更新 targets[targetPlatform] 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	created, variantErr := storeProtocolVariants(root, directory, artifact, entries, manifest) /* 更新 variantErr 的值。 */
	if variantErr != nil {                                                                     /* 判断条件并选择处理分支。 */
		_ = os.Remove(workerPath)           /* 更新 _ 的值。 */
		_ = os.Remove(packagePath)          /* 更新 _ 的值。 */
		problem(w, 422, variantErr.Error()) /* 执行当前语句并推进处理流程。 */
		return                              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	retained := false /* 更新 retained 的值。 */
	defer func() {    /* 安排函数结束时执行清理。 */
		if !retained { /* 判断条件并选择处理分支。 */
			for _, path := range created { /* 循环处理当前数据。 */
				_ = os.Remove(path) /* 更新 _ 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	}() /* 结束当前表达式或代码块。 */

	release := model.ProtocolRelease{TenantID: tenant, ProtocolID: protocolID, Version: manifest.Version, Transport: strings.ToUpper(manifest.Transport), PayloadFormat: strings.ToLower(manifest.PayloadFormat), ParserType: parser.GoProtocolParserName, Status: status, Capabilities: manifest.Capabilities, Config: map[string]any{"artifact": artifact, "timeoutMs": 5000}, Artifact: artifact, CreatedAt: now, PublishedAt: publishedAt} /* 更新 release 的值。 */
	if err = s.engine.Repo.SaveProtocolDefinition(r.Context(), definition); err == nil {                                                                                                                                                                                                                                                                                                                                                       /* 判断条件并选择处理分支。 */
		err = s.engine.Repo.CreateProtocolRelease(r.Context(), release) /* 更新 err 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		_ = os.Remove(workerPath)    /* 更新 _ 的值。 */
		_ = os.Remove(packagePath)   /* 更新 _ 的值。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	retained = true      /* 更新 retained 的值。 */
	var binding any      /* 声明 binding。 */
	if productID != "" { /* 判断条件并选择处理分支。 */
		bound, bindErr := s.bindProtocolRelease(r, protocolID, manifest.Version, productID) /* 更新 bindErr 的值。 */
		if bindErr != nil {                                                                 /* 判断条件并选择处理分支。 */
			problem(w, 500, "protocol was published but product binding failed: "+bindErr.Error()) /* 执行当前语句并推进处理流程。 */
			return                                                                                 /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		binding = bound /* 更新 binding 的值。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "protocol.v2.package.upload", "protocolRelease", protocolID+"@"+manifest.Version, map[string]any{"sha256": artifact["packageSha256"], "published": publish, "platform": targetPlatform}) /* 执行当前语句并推进处理流程。 */
	write(w, 201, map[string]any{"definition": definition, "release": release, "manifest": manifest, "binding": binding, "testCases": testCount})                                                       /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type protocolPackageCaseV2 = protocolworker.SampleCase /* 定义 protocolPackageCaseV2 类型。 */

func validateProtocolPackageCasesContextV2(parent context.Context, root string, artifact map[string]any, entries map[string][]byte, manifest protocolPackageManifestV2, required bool) (int, error) { /* 定义 validateProtocolPackageCasesContextV2 函数。 */
	return protocolworker.ValidateSamples(parent, root, artifact, entries, protocolworker.SampleManifest{ID: manifest.ID, Runtime: manifest.Runtime, Transport: manifest.Transport, PayloadFormat: manifest.PayloadFormat, Capabilities: manifest.Capabilities}, required) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func inspectProtocolPackageV2(reader *zip.Reader) (map[string][]byte, error) { /* 定义 inspectProtocolPackageV2 函数。 */
	if len(reader.File) == 0 || len(reader.File) > 100 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("protocol package must contain between 1 and 100 entries") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	entries := make(map[string][]byte, len(reader.File)) /* 更新 entries 的值。 */
	var expanded int64                                   /* 声明 expanded。 */
	for _, entry := range reader.File {                  /* 循环处理当前数据。 */
		name := filepath.ToSlash(strings.TrimSpace(entry.Name))                                                                                    /* 更新 name 的值。 */
		clean := filepath.ToSlash(filepath.Clean(name))                                                                                            /* 更新 clean 的值。 */
		if name == "" || strings.HasPrefix(clean, "../") || clean == ".." || filepath.IsAbs(name) || entry.FileInfo().Mode()&os.ModeSymlink != 0 { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("unsafe ZIP entry %q", entry.Name) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if entry.FileInfo().IsDir() { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if _, exists := entries[clean]; exists { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("duplicate ZIP entry %q", clean) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if entry.UncompressedSize64 > uint64(maxProtocolPackageV2) { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("ZIP entry %q is too large", name) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		expanded += int64(entry.UncompressedSize64)  /* 更新 expanded 的值。 */
		if expanded > maxProtocolPackageExpandedV2 { /* 判断条件并选择处理分支。 */
			return nil, errors.New("expanded protocol package exceeds 128 MiB") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		stream, err := entry.Open() /* 更新 err 的值。 */
		if err != nil {             /* 判断条件并选择处理分支。 */
			return nil, err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		content, readErr := io.ReadAll(io.LimitReader(stream, maxProtocolPackageV2+1))       /* 更新 readErr 的值。 */
		closeErr := stream.Close()                                                           /* 更新 closeErr 的值。 */
		if readErr != nil || closeErr != nil || int64(len(content)) > maxProtocolPackageV2 { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("read ZIP entry %q", name) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		entries[clean] = content /* 更新 entries[clean] 的值。 */
	} /* 结束当前表达式或代码块。 */
	return entries, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func validateProtocolManifestV2(protocolID string, manifest protocolPackageManifestV2) error { /* 定义 validateProtocolManifestV2 函数。 */
	if manifest.SchemaVersion != 1 { /* 判断条件并选择处理分支。 */
		return errors.New("manifest schemaVersion must be 1") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if manifest.ID != protocolID || !protocolSegmentV2.MatchString(manifest.ID) { /* 判断条件并选择处理分支。 */
		return errors.New("manifest id must match the protocol id in the URL") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !protocolSegmentV2.MatchString(manifest.Version) { /* 判断条件并选择处理分支。 */
		return errors.New("manifest version is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if manifest.Runtime != protocolworker.Runtime { /* 判断条件并选择处理分支。 */
		return errors.New("manifest runtime must be go-protocol-v2") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	seen := map[string]bool{}                          /* 更新 seen 的值。 */
	for _, capability := range manifest.Capabilities { /* 循环处理当前数据。 */
		if seen[capability] || (capability != "decode" && capability != "ingress" && capability != "encode") { /* 判断条件并选择处理分支。 */
			return fmt.Errorf("unsupported or repeated protocol capability %q", capability) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		seen[capability] = true /* 更新 seen[capability] 的值。 */
	} /* 结束当前表达式或代码块。 */
	if !seen["decode"] { /* 判断条件并选择处理分支。 */
		return errors.New("go-protocol-v2 requires decode capability") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if seen["ingress"] && strings.ToLower(manifest.PayloadFormat) != "hex" { /* 判断条件并选择处理分支。 */
		return errors.New("ingress requires hex payloadFormat") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(manifest.Entrypoints) == 0 { /* 判断条件并选择处理分支。 */
		return errors.New("manifest entrypoints are required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.TrimSpace(manifest.Transport) == "" || strings.TrimSpace(manifest.PayloadFormat) == "" { /* 判断条件并选择处理分支。 */
		return errors.New("manifest transport and payloadFormat are required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for platform, entrypoint := range manifest.Entrypoints { /* 循环处理当前数据。 */
		if !protocolSegmentV2.MatchString(platform) { /* 判断条件并选择处理分支。 */
			return fmt.Errorf("invalid entrypoint platform %q", platform) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		clean := filepath.ToSlash(filepath.Clean(entrypoint))                                                   /* 更新 clean 的值。 */
		if entrypoint == "" || clean == ".." || strings.HasPrefix(clean, "../") || filepath.IsAbs(entrypoint) { /* 判断条件并选择处理分支。 */
			return fmt.Errorf("unsafe entrypoint for %s", platform) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func writeExclusiveFile(path string, data []byte, mode os.FileMode) error { /* 定义 writeExclusiveFile 函数。 */
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode) /* 更新 err 的值。 */
	if err != nil {                                                         /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	_, writeErr := file.Write(data) /* 更新 writeErr 的值。 */
	closeErr := file.Close()        /* 更新 closeErr 的值。 */
	if writeErr != nil {            /* 判断条件并选择处理分支。 */
		_ = os.Remove(path) /* 更新 _ 的值。 */
		return writeErr     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if closeErr != nil { /* 判断条件并选择处理分支。 */
		_ = os.Remove(path) /* 更新 _ 的值。 */
		return closeErr     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) publishProtocolReleaseV2(w http.ResponseWriter, r *http.Request) { /* 定义 publishProtocolReleaseV2 函数。 */
	tenant, id, version := claims(r).TenantID, r.PathValue("id"), r.PathValue("version") /* 更新 version 的值。 */
	release, err := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, id, version)   /* 更新 err 的值。 */
	if err != nil {                                                                      /* 判断条件并选择处理分支。 */
		problem(w, 404, "protocol release not found") /* 执行当前语句并推进处理流程。 */
		return                                        /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if release.Status == "REVOKED" { /* 判断条件并选择处理分支。 */
		problem(w, 409, "revoked release cannot be published") /* 执行当前语句并推进处理流程。 */
		return                                                 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err = validateProtocolReleaseV2(release); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if release.ParserType == parser.GoProtocolParserName && artifactTestCountV2(release.Artifact) == 0 { /* 判断条件并选择处理分支。 */
		problem(w, 422, "custom protocol release has no passing package test cases") /* 执行当前语句并推进处理流程。 */
		return                                                                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if release.Artifact["generatedMapping"] == true && release.Status != "VALIDATED" && release.Status != "PUBLISHED" { /* 判断条件并选择处理分支。 */
		problem(w, 422, "请先用真实样本完成解析预览") /* 执行当前语句并推进处理流程。 */
		return                           /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	now := time.Now().UnixMilli()                                                                                        /* 更新 now 的值。 */
	if err = s.engine.Repo.UpdateProtocolReleaseStatus(r.Context(), tenant, id, version, "PUBLISHED", now); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	release.Status = "PUBLISHED"                                                      /* 更新 release.Status 的值。 */
	release.PublishedAt = now                                                         /* 更新 release.PublishedAt 的值。 */
	s.audit(r, "protocol.v2.release.publish", "protocolRelease", id+"@"+version, nil) /* 执行当前语句并推进处理流程。 */
	write(w, 200, release)                                                            /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) bindProductProtocolV2(w http.ResponseWriter, r *http.Request) { /* 定义 bindProductProtocolV2 函数。 */
	var in struct { /* 声明 in。 */
		ProtocolID string `json:"protocolId"` /* 执行当前语句并推进处理流程。 */
		Version    string `json:"version"`    /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if decode(w, r, &in) != nil { /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	binding, err := s.bindProtocolRelease(r, in.ProtocolID, in.Version, r.PathValue("id"))
	if err != nil {
		bindingProblem(w, err)
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, binding) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) rollbackProductProtocolV2(w http.ResponseWriter, r *http.Request) { /* 定义 rollbackProductProtocolV2 函数。 */
	tenant, productID := claims(r).TenantID, r.PathValue("id")                              /* 更新 productID 的值。 */
	current, err := s.engine.Repo.GetProductProtocolBinding(r.Context(), tenant, productID) /* 更新 err 的值。 */
	if err != nil || current.PreviousVersion == "" {                                        /* 判断条件并选择处理分支。 */
		problem(w, 409, "there is no previous protocol release to roll back to") /* 执行当前语句并推进处理流程。 */
		return                                                                   /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	binding, err := s.bindProtocolRelease(r, firstNonBlank(current.PreviousProtocolID, current.ProtocolID), current.PreviousVersion, productID)
	if err != nil {
		bindingProblem(w, err)
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, binding) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func bindingProblem(w http.ResponseWriter, err error) {
	if errors.Is(err, model.ErrBindingChanged) {
		problem(w, 409, err.Error())
		return
	}
	problem(w, 422, err.Error())
}

func (s *Server) bindProtocolRelease(r *http.Request, protocolID, version, productID string) (model.ProductProtocolBinding, error) { /* 定义 bindProtocolRelease 函数。 */
	tenant := claims(r).TenantID                                                               /* 更新 tenant 的值。 */
	release, err := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, protocolID, version) /* 更新 err 的值。 */
	if err != nil {                                                                            /* 判断条件并选择处理分支。 */
		return model.ProductProtocolBinding{}, errors.New("protocol release not found") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if release.Status != "PUBLISHED" { /* 判断条件并选择处理分支。 */
		return model.ProductProtocolBinding{}, errors.New("only a published protocol release can be bound") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	product, err := s.engine.Repo.GetProduct(r.Context(), tenant, productID) /* 更新 err 的值。 */
	if err != nil {                                                          /* 判断条件并选择处理分支。 */
		return model.ProductProtocolBinding{}, errors.New("product not found") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	profiles, err := s.engine.Repo.ListDeviceAccessProfiles(r.Context(), tenant) /* 更新 err 的值。 */
	if err != nil {                                                              /* 判断条件并选择处理分支。 */
		return model.ProductProtocolBinding{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, profile := range profiles { /* 循环处理当前数据。 */
		if profile.Enabled && profile.ProductID == productID && len(profile.Queries) > 0 && !protocolworker.HasCapability(release, "encode") { /* 判断条件并选择处理分支。 */
			return model.ProductProtocolBinding{}, errors.New("该产品已有定时查询，新版本必须保留 encode 能力") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		for _, mapping := range profile.ChildProducts { /* 循环处理当前数据。 */
			if profile.Enabled && mapping.ProductID == productID && release.PayloadFormat != "hex" { /* 判断条件并选择处理分支。 */
				return model.ProductProtocolBinding{}, errors.New("子设备接入映射要求 HEX 解析协议") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if profile.Enabled && profile.Mode == "listener" && profile.ProductID == productID && !listenerSupports(release, profile.Network) { /* 判断条件并选择处理分支。 */
			return model.ProductProtocolBinding{}, errors.New("新版本不支持该产品已启用的 TCP/UDP 接入实例") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	previous, previousProtocol := "", ""
	var expected *model.ProductProtocolBinding
	if old, getErr := s.engine.Repo.GetProductProtocolBinding(r.Context(), tenant, productID); getErr == nil {
		if old.ProtocolID == protocolID && old.Version == version {
			return old, nil
		}
		previous, previousProtocol, expected = old.Version, old.ProtocolID, &old
	} else if !errors.Is(getErr, model.ErrNotFound) {
		return model.ProductProtocolBinding{}, getErr
	}
	binding := model.ProductProtocolBinding{TenantID: tenant, ProductID: productID, ProtocolID: protocolID, Version: version, PreviousVersion: previous, PreviousProtocolID: previousProtocol, UpdatedAt: time.Now().UnixMilli()}
	shim := legacyProtocolShim(release)
	product.ProtocolPackageID = shim.ID
	// A dual-network protocol keeps the network the template was narrowed to.
	if narrowed := strings.ToUpper(product.Transport); !(strings.EqualFold(release.Transport, "TCP_UDP") && (narrowed == "TCP" || narrowed == "UDP")) {
		product.Transport = release.Transport
	}
	product.PayloadFormat = release.PayloadFormat
	product.UpdatedAt = binding.UpdatedAt
	if err = s.engine.Repo.SwitchProductProtocol(r.Context(), model.ProtocolSwitch{Product: product, Package: shim, Binding: binding, Expected: expected}); err != nil {
		return binding, err
	}
	s.audit(r, "protocol.v2.binding.switch", "product", productID, map[string]any{"protocolId": protocolID, "version": version, "previousVersion": previous}) /* 执行当前语句并推进处理流程。 */
	return binding, nil                                                                                                                                       /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// Existing Modbus releases remain readable; new specialized protocols use Go packages.
func (s *Server) importModbusTCPV2(w http.ResponseWriter, r *http.Request) { /* 定义 importModbusTCPV2 函数。 */
	problem(w, 422, "新增 Modbus 协议请将点表和解析逻辑放入 Go 源码包上传；旧采集实例继续运行") /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) deviceAccessProfilesV2(w http.ResponseWriter, r *http.Request) { /* 定义 deviceAccessProfilesV2 函数。 */
	items, err := s.engine.Repo.ListDeviceAccessProfiles(r.Context(), claims(r).TenantID) /* 更新 err 的值。 */
	if err != nil {                                                                       /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for i := range items { /* 循环处理当前数据。 */
		if items[i].Mode == "listener" { /* 判断条件并选择处理分支。 */
			if !items[i].Enabled { /* 判断条件并选择处理分支。 */
				items[i].RuntimeStatus = "DISABLED" /* 更新 items[i].RuntimeStatus 的值。 */
				items[i].LastError = ""             /* 更新 items[i].LastError 的值。 */
			} else if runtime, ok := s.protocolListeners.(interface { /* 结束当前表达式或代码块。 */
				Status(string, string) (string, string, int64) /* 执行当前语句并推进处理流程。 */
			}); ok { /* 结束当前表达式或代码块。 */
				items[i].RuntimeStatus, items[i].LastError, items[i].LastSuccessAt = runtime.Status(items[i].TenantID, items[i].ID) /* 更新 items[i].LastSuccessAt 的值。 */
			} /* 结束当前表达式或代码块。 */
			if binding, err := s.engine.Repo.GetProductProtocolBinding(r.Context(), items[i].TenantID, items[i].ProductID); err == nil { /* 判断条件并选择处理分支。 */
				items[i].ProtocolID, items[i].ProtocolVersion = binding.ProtocolID, binding.Version /* 更新 items[i].ProtocolVersion 的值。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, map[string]any{"items": items, "count": len(items)}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) saveDeviceAccessProfileV2(w http.ResponseWriter, r *http.Request) { /* 定义 saveDeviceAccessProfileV2 函数。 */
	var v model.DeviceAccessProfile /* 声明 v。 */
	if decode(w, r, &v) != nil {    /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	v.TenantID = claims(r).TenantID                           /* 更新 v.TenantID 的值。 */
	v.Mode = strings.ToLower(strings.TrimSpace(v.Mode))       /* 更新 v.Mode 的值。 */
	v.Network = strings.ToLower(strings.TrimSpace(v.Network)) /* 更新 v.Network 的值。 */
	if id := r.PathValue("id"); id != "" {                    /* 判断条件并选择处理分支。 */
		v.ID = id /* 更新 v.ID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err := validateAccessProfile(v); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	release, err := s.engine.Repo.GetProtocolRelease(r.Context(), v.TenantID, v.ProtocolID, v.ProtocolVersion) /* 更新 err 的值。 */
	if err != nil || release.Status != "PUBLISHED" {                                                           /* 判断条件并选择处理分支。 */
		problem(w, 422, "a published protocol release is required") /* 执行当前语句并推进处理流程。 */
		return                                                      /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	product, err := s.engine.Repo.GetProduct(r.Context(), v.TenantID, v.ProductID) /* 更新 err 的值。 */
	if err != nil {                                                                /* 判断条件并选择处理分支。 */
		problem(w, 422, "product not found") /* 执行当前语句并推进处理流程。 */
		return                               /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.Mode == "listener" { /* 判断条件并选择处理分支。 */
		if !listenerSupports(release, v.Network) { /* 判断条件并选择处理分支。 */
			problem(w, 422, "请选择支持该 TCP/UDP 网络与 ingress 能力的完整 Go 协议包") /* 执行当前语句并推进处理流程。 */
			return                                                     /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if v.ConnectionMode != "dial" { /* 判断条件并选择处理分支。 */
			v.DeviceID = "" /* 更新 v.DeviceID 的值。 */
		} else { /* 结束当前表达式或代码块。 */
			device, e := s.engine.Repo.GetManagedDevice(r.Context(), v.TenantID, v.DeviceID) /* 更新 e 的值。 */
			if e != nil || device.ProductID != v.ProductID || device.GatewayID != "" {       /* 判断条件并选择处理分支。 */
				problem(w, 422, "主动连接需要已配置的主设备") /* 执行当前语句并推进处理流程。 */
				return                           /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		profiles, listErr := s.engine.Repo.ListDeviceAccessProfiles(r.Context(), "") /* 更新 listErr 的值。 */
		if listErr != nil {                                                          /* 判断条件并选择处理分支。 */
			problem(w, 500, "读取接入实例失败") /* 执行当前语句并推进处理流程。 */
			return                      /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		for _, other := range profiles { /* 循环处理当前数据。 */
			if v.ConnectionMode != "dial" && other.ConnectionMode != "dial" && v.Enabled && other.Enabled && other.Mode == "listener" && other.Network == v.Network && other.Port == v.Port && (other.TenantID != v.TenantID || other.ID != v.ID) { /* 判断条件并选择处理分支。 */
				problem(w, 409, "该监听端口已由另一个接入实例占用") /* 执行当前语句并推进处理流程。 */
				return                              /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} else { /* 结束当前表达式或代码块。 */
		if (v.WireFormat == "rtu_over_tcp") != (release.Transport == "MODBUS_RTU") { /* 判断条件并选择处理分支。 */
			problem(w, 422, "serial profile and release transport must match") /* 执行当前语句并推进处理流程。 */
			return                                                             /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		device, err := s.engine.Repo.GetManagedDevice(r.Context(), v.TenantID, v.DeviceID) /* 更新 err 的值。 */
		if err != nil || device.ProductID != product.ID {                                  /* 判断条件并选择处理分支。 */
			problem(w, 422, "device not found or does not belong to product") /* 执行当前语句并推进处理流程。 */
			return                                                            /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err := onboarding.ValidateChildProducts(r.Context(), s.engine.Repo, v, release); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	binding, err := s.engine.Repo.GetProductProtocolBinding(r.Context(), v.TenantID, v.ProductID) /* 更新 err 的值。 */
	if err != nil || binding.ProtocolID != v.ProtocolID || binding.Version != v.ProtocolVersion { /* 判断条件并选择处理分支。 */
		problem(w, 422, "access profile protocol must match the product's active protocol binding") /* 执行当前语句并推进处理流程。 */
		return                                                                                      /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	now := time.Now().UnixMilli()                                                                          /* 更新 now 的值。 */
	if old, getErr := s.engine.Repo.GetDeviceAccessProfile(r.Context(), v.TenantID, v.ID); getErr == nil { /* 判断条件并选择处理分支。 */
		v.CreatedAt = old.CreatedAt /* 更新 v.CreatedAt 的值。 */
	} /* 结束当前表达式或代码块。 */
	if v.CreatedAt == 0 { /* 判断条件并选择处理分支。 */
		v.CreatedAt = now /* 更新 v.CreatedAt 的值。 */
	} /* 结束当前表达式或代码块。 */
	v.UpdatedAt = now                            /* 更新 v.UpdatedAt 的值。 */
	v.RuntimeStatus, v.LastError = "PENDING", "" /* 更新 v.LastError 的值。 */
	if !v.Enabled {                              /* 判断条件并选择处理分支。 */
		v.RuntimeStatus = "DISABLED" /* 更新 v.RuntimeStatus 的值。 */
	} /* 结束当前表达式或代码块。 */
	if err = s.engine.Repo.SaveDeviceAccessProfile(r.Context(), v); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 500, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	s.audit(r, "protocol.v2.access.save", "deviceAccessProfile", v.ID, map[string]any{"deviceId": v.DeviceID, "enabled": v.Enabled}) /* 执行当前语句并推进处理流程。 */
	write(w, 201, v)                                                                                                                 /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) testDeviceAccessProfileV2(w http.ResponseWriter, r *http.Request) { /* 定义 testDeviceAccessProfileV2 函数。 */
	profile, err := s.engine.Repo.GetDeviceAccessProfile(r.Context(), claims(r).TenantID, r.PathValue("id")) /* 更新 err 的值。 */
	if err != nil {                                                                                          /* 判断条件并选择处理分支。 */
		problem(w, 404, "device access profile not found") /* 执行当前语句并推进处理流程。 */
		return                                             /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if profile.Mode == "listener" { /* 判断条件并选择处理分支。 */
		problem(w, 422, "TCP/UDP 监听请使用设备或协议模拟器发送报文验证") /* 执行当前语句并推进处理流程。 */
		return                                         /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	release, err := s.engine.Repo.GetProtocolRelease(r.Context(), profile.TenantID, profile.ProtocolID, profile.ProtocolVersion) /* 更新 err 的值。 */
	if err != nil {                                                                                                              /* 判断条件并选择处理分支。 */
		problem(w, 422, "protocol release not found") /* 执行当前语句并推进处理流程。 */
		return                                        /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := validateAccessProfile(profile); err != nil { /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	blocks, err := releaseBlocksForAPI(release) /* 更新 err 的值。 */
	if err != nil {                             /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := contextWithMaximum(r, 10*time.Second)                                                              /* 更新 cancel 的值。 */
	defer cancel()                                                                                                    /* 安排函数结束时执行清理。 */
	raws, err := protocolruntime.ReadModbusTCPWithPolicy(ctx, profile, release, blocks[:1], s.cfg.ModbusAllowedCIDRs) /* 更新 err 的值。 */
	if err != nil {                                                                                                   /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	message, err := s.engine.Parsers.ParseWithConfig(release.ParserType, release.Config, raws[0]) /* 更新 err 的值。 */
	if err != nil {                                                                               /* 判断条件并选择处理分支。 */
		problem(w, 422, err.Error()) /* 执行当前语句并推进处理流程。 */
		return                       /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	write(w, 200, map[string]any{"request": raws[0].Metadata, "response": raws[0].Payload, "standardMessage": message}) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func legacyProtocolShim(release model.ProtocolRelease) model.ProtocolPackage { /* 定义 legacyProtocolShim 函数。 */
	return model.ProtocolPackage{ID: release.ProtocolID + "@" + release.Version, TenantID: release.TenantID, Name: release.ProtocolID + " " + release.Version, Version: release.Version, Protocol: release.ProtocolID, Transport: release.Transport, PayloadFormat: release.PayloadFormat, ParserType: release.ParserType, Status: "PUBLISHED", Description: "Protocol v2 compatibility binding", Config: release.Config, CreatedAt: release.CreatedAt, UpdatedAt: release.PublishedAt} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func validateAccessProfile(v model.DeviceAccessProfile) error { /* 定义 validateAccessProfile 函数。 */
	if err := model.ValidateProtocolAccess(v); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.EdgeNodeID != "" { /* 判断条件并选择处理分支。 */
		return errors.New("边缘节点功能已移除，请使用中心直接接入") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.Mode == "listener" { /* 判断条件并选择处理分支。 */
		return validateListenerProfile(v) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.Mode != "" && v.Mode != "poll" { /* 判断条件并选择处理分支。 */
		return errors.New("不支持的设备接入模式") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.Network != "" && v.Network != "tcp" { /* 判断条件并选择处理分支。 */
		return errors.New("中心轮询仅支持 Modbus TCP") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.ID == "" || v.DeviceID == "" || v.ProductID == "" || v.ProtocolID == "" || v.ProtocolVersion == "" || v.Host == "" { /* 判断条件并选择处理分支。 */
		return errors.New("id, deviceId, productId, protocolId, protocolVersion and host are required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.Port <= 0 || v.Port > 65535 { /* 判断条件并选择处理分支。 */
		return errors.New("port must be between 1 and 65535") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.UnitID < 0 || v.UnitID > 255 { /* 判断条件并选择处理分支。 */
		return errors.New("unitId must be between 0 and 255") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.TimeoutMs <= 0 || v.TimeoutMs > 30000 { /* 判断条件并选择处理分支。 */
		return errors.New("timeoutMs must be between 1 and 30000") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if v.Retries < 0 || v.Retries > 5 { /* 判断条件并选择处理分支。 */
		return errors.New("retries must be between 0 and 5") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func releaseBlocksForAPI(release model.ProtocolRelease) ([]model.ModbusReadBlock, error) { /* 定义 releaseBlocksForAPI 函数。 */
	b, err := json.Marshal(release.Config["blocks"]) /* 更新 err 的值。 */
	if err != nil {                                  /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var blocks []model.ModbusReadBlock                /* 声明 blocks。 */
	if err = json.Unmarshal(b, &blocks); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(blocks) == 0 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("release does not contain collection blocks") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return blocks, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func validateProtocolReleaseV2(release model.ProtocolRelease) error { /* 定义 validateProtocolReleaseV2 函数。 */
	if strings.TrimSpace(release.Transport) == "" || strings.TrimSpace(release.PayloadFormat) == "" { /* 判断条件并选择处理分支。 */
		return errors.New("transport and payloadFormat are required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	switch release.ParserType { /* 根据条件选择处理路径。 */
	case parser.PollResponseParserName: /* 处理当前分支。 */
		_, err := parser.PollPoints(release.Config) /* 更新 err 的值。 */
		return err                                  /* 返回当前处理结果。 */
	case parser.ModbusTCPParserName, parser.ModbusRTUParserName: /* 处理当前分支。 */
		_, err := releaseBlocksForAPI(release) /* 更新 err 的值。 */
		return err                             /* 返回当前处理结果。 */
	case parser.GoProtocolParserName: /* 处理当前分支。 */
		artifact, ok := release.Config["artifact"].(map[string]any)       /* 更新 ok 的值。 */
		if !ok || strings.TrimSpace(fmt.Sprint(artifact["path"])) == "" { /* 判断条件并选择处理分支。 */
			return errors.New("custom protocol release artifact is missing") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if artifact["runtime"] != protocolworker.Runtime { /* 判断条件并选择处理分支。 */
			return errors.New("protocol runtime must be go-protocol-v2") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	case "configurable_json_parser", "configurable_hex_parser": /* 处理当前分支。 */
		if len(release.Config) == 0 { /* 判断条件并选择处理分支。 */
			return errors.New("configurable protocol release config is required") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	default: /* 处理当前分支。 */
		return errors.New("unsupported protocol v2 parserType") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func jsonValue(v any) any { /* 定义 jsonValue 函数。 */
	b, _ := json.Marshal(v)     /* 更新 _ 的值。 */
	var out any                 /* 声明 out。 */
	_ = json.Unmarshal(b, &out) /* 更新 _ 的值。 */
	return out                  /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func artifactTestCountV2(artifact map[string]any) int { /* 定义 artifactTestCountV2 函数。 */
	value := artifact["testCases"] /* 更新 value 的值。 */
	switch v := value.(type) {     /* 根据条件选择处理路径。 */
	case int: /* 处理当前分支。 */
		return v /* 返回当前处理结果。 */
	case int64: /* 处理当前分支。 */
		return int(v) /* 返回当前处理结果。 */
	case float64: /* 处理当前分支。 */
		return int(v) /* 返回当前处理结果。 */
	case json.Number: /* 处理当前分支。 */
		n, _ := strconv.Atoi(v.String()) /* 更新 _ 的值。 */
		return n                         /* 返回当前处理结果。 */
	case string: /* 处理当前分支。 */
		n, _ := strconv.Atoi(strings.TrimSpace(v)) /* 更新 _ 的值。 */
		return n                                   /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return 0 /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
func formBoolStrict(r *http.Request, name string, fallback bool) (bool, error) { /* 定义 formBoolStrict 函数。 */
	v := strings.TrimSpace(r.FormValue(name)) /* 更新 v 的值。 */
	if v == "" {                              /* 判断条件并选择处理分支。 */
		return fallback, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	n, err := strconv.ParseBool(v) /* 更新 err 的值。 */
	if err != nil {                /* 判断条件并选择处理分支。 */
		return false, fmt.Errorf("%s must be true or false", name) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return n, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func firstNonBlank(values ...string) string { /* 定义 firstNonBlank 函数。 */
	for _, v := range values { /* 循环处理当前数据。 */
		if strings.TrimSpace(v) != "" { /* 判断条件并选择处理分支。 */
			return strings.TrimSpace(v) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return "" /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func contextWithMaximum(r *http.Request, maximum time.Duration) (context.Context, context.CancelFunc) { /* 定义 contextWithMaximum 函数。 */
	if deadline, ok := r.Context().Deadline(); ok && time.Until(deadline) < maximum { /* 判断条件并选择处理分支。 */
		return context.WithCancel(r.Context()) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return context.WithTimeout(r.Context(), maximum) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
