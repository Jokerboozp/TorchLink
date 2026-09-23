package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"archive/zip"   /* 执行当前语句并推进处理流程。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"crypto/sha256" /* 执行当前语句并推进处理流程。 */
	"encoding/hex"  /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"net/http"      /* 执行当前语句并推进处理流程。 */
	"os"            /* 执行当前语句并推进处理流程。 */
	"path/filepath" /* 执行当前语句并推进处理流程。 */
	"strconv"       /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolbuild" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (s *Server) downloadProtocolPackageV2(w http.ResponseWriter, r *http.Request) { /* 定义 downloadProtocolPackageV2 函数。 */
	release, data, ok := s.readProtocolDownloadV2(w, r) /* 更新 ok 的值。 */
	if !ok {                                            /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	filename := release.ProtocolID + "-" + release.Version + "-package.zip"                                    /* 更新 filename 的值。 */
	writeProtocolDownloadV2(w, data, filename, "application/zip")                                              /* 执行当前语句并推进处理流程。 */
	s.audit(r, "protocol.v2.package.download", "protocolRelease", release.ProtocolID+"@"+release.Version, nil) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (s *Server) downloadProtocolSourceV2(w http.ResponseWriter, r *http.Request) { /* 定义 downloadProtocolSourceV2 函数。 */
	release, content, extension, ok := s.readProtocolSourceV2(w, r) /* 更新 ok 的值。 */
	if !ok {                                                        /* 判断条件并选择处理分支。 */
		return /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	contentType := "text/plain; charset=utf-8" /* 更新 contentType 的值。 */
	if extension == ".zip" {                   /* 判断条件并选择处理分支。 */
		contentType = "application/zip" /* 更新 contentType 的值。 */
	} /* 结束当前表达式或代码块。 */
	writeProtocolDownloadV2(w, content, release.ProtocolID+"-"+release.Version+"-source"+extension, contentType) /* 执行当前语句并推进处理流程。 */
	s.audit(r, "protocol.v2.source.download", "protocolRelease", release.ProtocolID+"@"+release.Version, nil)    /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) readProtocolSourceV2(w http.ResponseWriter, r *http.Request) (model.ProtocolRelease, []byte, string, bool) { /* 定义 readProtocolSourceV2 函数。 */
	return s.readProtocolSourceForTenantV2(w, r, claims(r).TenantID) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) readProtocolSourceForTenantV2(w http.ResponseWriter, r *http.Request, tenant string) (model.ProtocolRelease, []byte, string, bool) { /* 定义 readProtocolSourceForTenantV2 函数。 */
	release, data, ok := s.readProtocolDownloadForTenantV2(w, r, tenant) /* 更新 ok 的值。 */
	if !ok {                                                             /* 判断条件并选择处理分支。 */
		return model.ProtocolRelease{}, nil, "", false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data))) /* 更新 err 的值。 */
	if err != nil {                                                        /* 判断条件并选择处理分支。 */
		problem(w, 409, "协议制品 ZIP 已损坏，无法提取源码")         /* 执行当前语句并推进处理流程。 */
		return model.ProtocolRelease{}, nil, "", false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var source *zip.File                /* 声明 source。 */
	for _, file := range archive.File { /* 循环处理当前数据。 */
		if file.Name != "source/upload.go" && file.Name != "source/upload.zip" { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		if source != nil || !file.Mode().IsRegular() || file.UncompressedSize64 == 0 || file.UncompressedSize64 > protocolbuild.MaxSource { /* 判断条件并选择处理分支。 */
			problem(w, 409, "协议制品中的原始源码无效")                /* 执行当前语句并推进处理流程。 */
			return model.ProtocolRelease{}, nil, "", false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		source = file /* 更新 source 的值。 */
	} /* 结束当前表达式或代码块。 */
	if source == nil { /* 判断条件并选择处理分支。 */
		problem(w, 404, "该协议版本未保存原始 Go 源码，可下载完整制品；后续请通过 Go 源码接入上传") /* 执行当前语句并推进处理流程。 */
		return model.ProtocolRelease{}, nil, "", false              /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	reader, err := source.Open() /* 更新 err 的值。 */
	if err != nil {              /* 判断条件并选择处理分支。 */
		problem(w, 409, "无法读取协议版本的原始源码")               /* 执行当前语句并推进处理流程。 */
		return model.ProtocolRelease{}, nil, "", false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	content, readErr := io.ReadAll(io.LimitReader(reader, protocolbuild.MaxSource+1))                     /* 更新 readErr 的值。 */
	closeErr := reader.Close()                                                                            /* 更新 closeErr 的值。 */
	if readErr != nil || closeErr != nil || len(content) == 0 || len(content) > protocolbuild.MaxSource { /* 判断条件并选择处理分支。 */
		problem(w, 409, "协议版本的原始源码已损坏或超过大小限制")         /* 执行当前语句并推进处理流程。 */
		return model.ProtocolRelease{}, nil, "", false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if build, ok := release.Artifact["build"].(map[string]any); ok { /* 判断条件并选择处理分支。 */
		if expected, ok := build["sourceSha256"].(string); ok && expected != "" && !protocolDownloadHashMatchesV2(content, expected) { /* 判断条件并选择处理分支。 */
			problem(w, 409, "协议原始源码 SHA-256 校验失败")         /* 执行当前语句并推进处理流程。 */
			return model.ProtocolRelease{}, nil, "", false /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	extension := ".go"                          /* 更新 extension 的值。 */
	if strings.HasSuffix(source.Name, ".zip") { /* 判断条件并选择处理分支。 */
		extension = ".zip" /* 更新 extension 的值。 */
	} /* 结束当前表达式或代码块。 */
	return release, content, extension, true /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// Downloads resolve the release in the authenticated tenant, then accept only
// the archive location generated by installation for that exact release. An
// os.Root additionally prevents filesystem links from escaping the data root.
func (s *Server) readProtocolDownloadV2(w http.ResponseWriter, r *http.Request) (model.ProtocolRelease, []byte, bool) { /* 定义 readProtocolDownloadV2 函数。 */
	return s.readProtocolDownloadForTenantV2(w, r, claims(r).TenantID) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (s *Server) readProtocolDownloadForTenantV2(w http.ResponseWriter, r *http.Request, tenant string) (model.ProtocolRelease, []byte, bool) { /* 定义 readProtocolDownloadForTenantV2 函数。 */
	id, version := r.PathValue("id"), r.PathValue("version")                                                                     /* 更新 version 的值。 */
	var empty model.ProtocolRelease                                                                                              /* 声明 empty。 */
	if !protocolSegmentV2.MatchString(tenant) || !protocolSegmentV2.MatchString(id) || !protocolSegmentV2.MatchString(version) { /* 判断条件并选择处理分支。 */
		problem(w, 422, "协议标识或版本号无效") /* 执行当前语句并推进处理流程。 */
		return empty, nil, false      /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	release, err := s.engine.Repo.GetProtocolRelease(r.Context(), tenant, id, version) /* 更新 err 的值。 */
	if err != nil {                                                                    /* 判断条件并选择处理分支。 */
		problem(w, 404, "协议版本不存在") /* 执行当前语句并推进处理流程。 */
		return empty, nil, false   /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	storedPath, _ := release.Artifact["packagePath"].(string) /* 更新 _ 的值。 */
	if storedPath == "" {                                     /* 判断条件并选择处理分支。 */
		problem(w, 404, "该协议版本没有可下载的协议制品") /* 执行当前语句并推进处理流程。 */
		return empty, nil, false           /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	expectedPath := filepath.Join("protocol-releases", tenant, id, version, "package.zip") /* 更新 expectedPath 的值。 */
	if filepath.ToSlash(storedPath) != filepath.ToSlash(expectedPath) {                    /* 判断条件并选择处理分支。 */
		problem(w, 409, "协议制品路径与当前租户及版本不匹配") /* 执行当前语句并推进处理流程。 */
		return empty, nil, false             /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	root, err := os.OpenRoot(s.cfg.DataDir) /* 更新 err 的值。 */
	if err != nil {                         /* 判断条件并选择处理分支。 */
		problem(w, 500, "无法读取协议制品目录") /* 执行当前语句并推进处理流程。 */
		return empty, nil, false      /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer root.Close()                   /* 安排函数结束时执行清理。 */
	file, err := root.Open(expectedPath) /* 更新 err 的值。 */
	if err != nil {                      /* 判断条件并选择处理分支。 */
		problem(w, 404, "协议制品文件不存在或无法读取") /* 执行当前语句并推进处理流程。 */
		return empty, nil, false          /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer file.Close()                                                                                    /* 安排函数结束时执行清理。 */
	info, err := file.Stat()                                                                              /* 更新 err 的值。 */
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxProtocolPackageV2 { /* 判断条件并选择处理分支。 */
		problem(w, 409, "协议制品文件无效或超过大小限制") /* 执行当前语句并推进处理流程。 */
		return empty, nil, false           /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	data, err := io.ReadAll(io.LimitReader(file, maxProtocolPackageV2+1)) /* 更新 err 的值。 */
	if err != nil || int64(len(data)) > maxProtocolPackageV2 {            /* 判断条件并选择处理分支。 */
		problem(w, 500, "读取协议制品失败") /* 执行当前语句并推进处理流程。 */
		return empty, nil, false    /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	expectedHash, _ := release.Artifact["packageSha256"].(string) /* 更新 _ 的值。 */
	if !protocolDownloadHashMatchesV2(data, expectedHash) {       /* 判断条件并选择处理分支。 */
		problem(w, 409, "协议制品 SHA-256 校验失败") /* 执行当前语句并推进处理流程。 */
		return empty, nil, false             /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return release, data, true /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func protocolDownloadHashMatchesV2(data []byte, expected string) bool { /* 定义 protocolDownloadHashMatchesV2 函数。 */
	digest := sha256.Sum256(data)                                     /* 更新 digest 的值。 */
	return strings.EqualFold(hex.EncodeToString(digest[:]), expected) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func writeProtocolDownloadV2(w http.ResponseWriter, data []byte, filename, contentType string) { /* 定义 writeProtocolDownloadV2 函数。 */
	digest := sha256.Sum256(data)                                                             /* 更新 digest 的值。 */
	w.Header().Set("Content-Type", contentType)                                               /* 执行当前语句并推进处理流程。 */
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename)) /* 执行当前语句并推进处理流程。 */
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))                                 /* 执行当前语句并推进处理流程。 */
	w.Header().Set("X-Content-SHA256", hex.EncodeToString(digest[:]))                         /* 执行当前语句并推进处理流程。 */
	w.Header().Set("Cache-Control", "no-store")                                               /* 执行当前语句并推进处理流程。 */
	w.WriteHeader(http.StatusOK)                                                              /* 执行当前语句并推进处理流程。 */
	_, _ = w.Write(data)                                                                      /* 更新 _ 的值。 */
} /* 结束当前表达式或代码块。 */
