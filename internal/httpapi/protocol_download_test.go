package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"archive/zip"       /* 执行当前语句并推进处理流程。 */
	"bytes"             /* 执行当前语句并推进处理流程。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"crypto/sha256"     /* 执行当前语句并推进处理流程。 */
	"encoding/hex"      /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"os"                /* 执行当前语句并推进处理流程。 */
	"path/filepath"     /* 执行当前语句并推进处理流程。 */
	"strings"           /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
	"time"              /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/local"  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"           /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"          /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/protocolbuild"   /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type protocolDownloadFixtureV2 struct { /* 定义 protocolDownloadFixtureV2 类型。 */
	api    *Server            /* 执行当前语句并推进处理流程。 */
	server *httptest.Server   /* 执行当前语句并推进处理流程。 */
	repo   *memory.Repository /* 执行当前语句并推进处理流程。 */
	root   string             /* 执行当前语句并推进处理流程。 */
	token  string             /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func newProtocolDownloadFixtureV2(t *testing.T) protocolDownloadFixtureV2 { /* 定义 newProtocolDownloadFixtureV2 函数。 */
	t.Helper()                             /* 执行当前语句并推进处理流程。 */
	root := t.TempDir()                    /* 更新 root 的值。 */
	repo := memory.NewRepository()         /* 更新 repo 的值。 */
	archive, err := local.NewArchive(root) /* 更新 err 的值。 */
	if err != nil {                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	log := slog.New(slog.NewTextHandler(io.Discard, nil))                                             /* 更新 log 的值。 */
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(), log) /* 更新 engine 的值。 */
	engine.Metrics = metrics.New()                                                                    /* 更新 engine.Metrics 的值。 */
	cfg := config.Load()                                                                              /* 更新 cfg 的值。 */
	cfg.DataDir = root                                                                                /* 更新 cfg.DataDir 的值。 */
	cfg.JWTSecret = "protocol-download-test-secret-at-least-32-characters"                            /* 更新 cfg.JWTSecret 的值。 */
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), log)                                  /* 更新 api 的值。 */
	server := httptest.NewServer(api.Handler())                                                       /* 更新 server 的值。 */
	t.Cleanup(server.Close)                                                                           /* 执行当前语句并推进处理流程。 */
	token, err := api.auth.Issue("protocol-developer", "tenant_001", "operator", nil, time.Hour)      /* 检查错误并决定后续处理。 */
	if err != nil {                                                                                   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	return protocolDownloadFixtureV2{api: api, server: server, repo: repo, root: root, token: token} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func protocolDownloadZipV2(t *testing.T, entries map[string][]byte) []byte { /* 定义 protocolDownloadZipV2 函数。 */
	t.Helper()                           /* 执行当前语句并推进处理流程。 */
	var body bytes.Buffer                /* 声明 body。 */
	writer := zip.NewWriter(&body)       /* 更新 writer 的值。 */
	for name, content := range entries { /* 循环处理当前数据。 */
		entry, err := writer.Create(name) /* 更新 err 的值。 */
		if err != nil {                   /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if _, err = entry.Write(content); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err := writer.Close(); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	return body.Bytes() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (f protocolDownloadFixtureV2) save(t *testing.T, version string, entries map[string][]byte, mutate func(map[string]any)) ([]byte, string) { /* 定义 save 函数。 */
	t.Helper()                                                                                          /* 执行当前语句并推进处理流程。 */
	data := protocolDownloadZipV2(t, entries)                                                           /* 更新 data 的值。 */
	relative := filepath.Join("protocol-releases", "tenant_001", "vendor-fire", version, "package.zip") /* 更新 relative 的值。 */
	filename := filepath.Join(f.root, relative)                                                         /* 更新 filename 的值。 */
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {                                  /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := os.WriteFile(filename, data, 0o600); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	digest := sha256.Sum256(data)                                                                                         /* 更新 digest 的值。 */
	artifact := map[string]any{"packagePath": filepath.ToSlash(relative), "packageSha256": hex.EncodeToString(digest[:])} /* 更新 artifact 的值。 */
	if mutate != nil {                                                                                                    /* 判断条件并选择处理分支。 */
		mutate(artifact) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if err := f.repo.CreateProtocolRelease(context.Background(), model.ProtocolRelease{TenantID: "tenant_001", ProtocolID: "vendor-fire", Version: version, ParserType: parser.GoProtocolParserName, Status: "PUBLISHED", Artifact: artifact}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	return data, filename /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (f protocolDownloadFixtureV2) get(t *testing.T, token, version, kind string, status int) ([]byte, http.Header) { /* 定义 get 函数。 */
	t.Helper()                                                                                                                    /* 执行当前语句并推进处理流程。 */
	request, err := http.NewRequest(http.MethodGet, f.server.URL+"/api/v2/protocols/vendor-fire/releases/"+version+"/"+kind, nil) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                                                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if token != "" { /* 判断条件并选择处理分支。 */
		request.Header.Set("Authorization", "Bearer "+token) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	response, err := f.server.Client().Do(request) /* 更新 err 的值。 */
	if err != nil {                                /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer response.Body.Close()            /* 安排函数结束时执行清理。 */
	body, err := io.ReadAll(response.Body) /* 更新 err 的值。 */
	if err != nil {                        /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if response.StatusCode != status { /* 判断条件并选择处理分支。 */
		t.Fatalf("%s %s status=%d want=%d body=%s", version, kind, response.StatusCode, status, body) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	return body, response.Header /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func TestProtocolReleaseDownloadsPreserveOriginalSourceAndPackage(t *testing.T) { /* 定义 TestProtocolReleaseDownloadsPreserveOriginalSourceAndPackage 函数。 */
	f := newProtocolDownloadFixtureV2(t)                   /* 更新 f 的值。 */
	project := protocolDownloadZipV2(t, map[string][]byte{ /* 更新 project 的值。 */
		"go.mod":             []byte("module example.com/vendor-fire\n\ngo 1.25.0\n"), /* 执行当前语句并推进处理流程。 */
		"main.go":            []byte(protocolbuild.Template),                          /* 执行当前语句并推进处理流程。 */
		"samples/cases.json": []byte(protocolSourceCases),                             /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	for _, test := range []struct { /* 循环处理当前数据。 */
		name, version, extension, contentType string /* 执行当前语句并推进处理流程。 */
		source                                []byte /* 执行当前语句并推进处理流程。 */
	}{ /* 结束当前表达式或代码块。 */
		{name: "single-file", version: "1.0.0", extension: ".go", contentType: "text/plain; charset=utf-8", source: []byte(protocolbuild.Template)}, /* 执行当前语句并推进处理流程。 */
		{name: "standalone-project", version: "1.1.0", extension: ".zip", contentType: "application/zip", source: project},                          /* 执行当前语句并推进处理流程。 */
	} { /* 结束当前表达式或代码块。 */
		t.Run(test.name, func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			digest := sha256.Sum256(test.source)                                                                                                                                                                                   /* 更新 digest 的值。 */
			archive, _ := f.save(t, test.version, map[string][]byte{"manifest.yaml": []byte("schemaVersion: 1\n"), "bin/worker": []byte("worker"), "source/upload" + test.extension: test.source}, func(artifact map[string]any) { /* 更新 _ 的值。 */
				artifact["build"] = map[string]any{"kind": "go-source", "sourceSha256": hex.EncodeToString(digest[:])} /* 执行当前语句并推进处理流程。 */
			}) /* 结束当前表达式或代码块。 */
			body, headers := f.get(t, f.token, test.version, "source", 200) /* 更新 headers 的值。 */
			if !bytes.Equal(body, test.source) {                            /* 判断条件并选择处理分支。 */
				t.Fatal("download changed the uploaded source bytes") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if headers.Get("Content-Type") != test.contentType || !strings.Contains(headers.Get("Content-Disposition"), "vendor-fire-"+test.version+"-source"+test.extension) || headers.Get("X-Content-SHA256") != hex.EncodeToString(digest[:]) || headers.Get("Cache-Control") != "no-store" { /* 判断条件并选择处理分支。 */
				t.Fatalf("unexpected source download headers: %v", headers) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			files, err := protocolbuild.Sources("download"+test.extension, body)              /* 更新 err 的值。 */
			if err != nil || !bytes.Equal(files["main.go"], []byte(protocolbuild.Template)) { /* 判断条件并选择处理分支。 */
				t.Fatalf("download cannot be uploaded again as Go source: %v", err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			body, headers = f.get(t, f.token, test.version, "package", 200)                                                                                               /* 更新 headers 的值。 */
			if !bytes.Equal(body, archive) || headers.Get("Content-Type") != "application/zip" || !strings.Contains(headers.Get("Content-Disposition"), "-package.zip") { /* 判断条件并选择处理分支。 */
				t.Fatal("package download did not preserve the complete immutable artifact") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	// A compiled-only release remains exportable, with a clear source error.
	archive, _ := f.save(t, "2.0.0", map[string][]byte{"bin/worker": []byte("worker")}, nil) /* 更新 _ 的值。 */
	body, _ := f.get(t, f.token, "2.0.0", "source", 404)                                     /* 更新 _ 的值。 */
	if !strings.Contains(string(body), "未保存原始 Go 源码") {                                      /* 判断条件并选择处理分支。 */
		t.Fatalf("missing-source explanation: %s", body) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	body, _ = f.get(t, f.token, "2.0.0", "package", 200) /* 更新 _ 的值。 */
	if !bytes.Equal(body, archive) {                     /* 判断条件并选择处理分支。 */
		t.Fatal("compiled-only package download differs") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestProtocolReleaseDownloadsEnforceAuthorizationAndTenant(t *testing.T) { /* 定义 TestProtocolReleaseDownloadsEnforceAuthorizationAndTenant 函数。 */
	f := newProtocolDownloadFixtureV2(t)                                                           /* 更新 f 的值。 */
	f.save(t, "1.0.0", map[string][]byte{"source/upload.go": []byte(protocolbuild.Template)}, nil) /* 执行当前语句并推进处理流程。 */
	viewer, _ := f.api.auth.Issue("reader", "tenant_001", "viewer", nil, time.Hour)                /* 更新 _ 的值。 */
	otherTenant, _ := f.api.auth.Issue("other", "tenant_002", "operator", nil, time.Hour)          /* 更新 _ 的值。 */
	for _, kind := range []string{"source", "package"} {                                           /* 循环处理当前数据。 */
		f.get(t, "", "1.0.0", kind, 401)          /* 执行当前语句并推进处理流程。 */
		f.get(t, viewer, "1.0.0", kind, 403)      /* 执行当前语句并推进处理流程。 */
		f.get(t, otherTenant, "1.0.0", kind, 404) /* 执行当前语句并推进处理流程。 */
		f.get(t, f.token, "9.9.9", kind, 404)     /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestProtocolReleaseDownloadsRejectWrongPathsAndCorruption(t *testing.T) { /* 定义 TestProtocolReleaseDownloadsRejectWrongPathsAndCorruption 函数。 */
	f := newProtocolDownloadFixtureV2(t)    /* 更新 f 的值。 */
	for index, wrongPath := range []string{ /* 循环处理当前数据。 */
		"../outside.zip",                     /* 执行当前语句并推进处理流程。 */
		filepath.Join(f.root, "outside.zip"), /* 执行当前语句并推进处理流程。 */
		"protocol-releases/tenant_002/vendor-fire/1.0.0/package.zip", /* 执行当前语句并推进处理流程。 */
		"protocol-releases/tenant_001/vendor-fire/1.0.0/package.zip", /* 执行当前语句并推进处理流程。 */
	} { /* 结束当前表达式或代码块。 */
		version := []string{"2.0.0", "2.1.0", "2.2.0", "2.3.0"}[index]                                                                                                   /* 更新 version 的值。 */
		f.save(t, version, map[string][]byte{"source/upload.go": []byte(protocolbuild.Template)}, func(artifact map[string]any) { artifact["packagePath"] = wrongPath }) /* 执行当前语句并推进处理流程。 */
		for _, kind := range []string{"source", "package"} {                                                                                                             /* 循环处理当前数据。 */
			f.get(t, f.token, version, kind, 409) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	_, filename := f.save(t, "3.0.0", map[string][]byte{"source/upload.go": []byte(protocolbuild.Template)}, nil) /* 更新 filename 的值。 */
	if err := os.WriteFile(filename, []byte("tampered artifact"), 0o600); err != nil {                            /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, kind := range []string{"source", "package"} { /* 循环处理当前数据。 */
		body, _ := f.get(t, f.token, "3.0.0", kind, 409)                                                       /* 更新 _ 的值。 */
		if !strings.Contains(string(body), "SHA-256") || strings.Contains(string(body), "tampered artifact") { /* 判断条件并选择处理分支。 */
			t.Fatalf("corrupt artifact response: %s", body) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	f.save(t, "3.1.0", map[string][]byte{"source/upload.go": []byte(protocolbuild.Template)}, func(artifact map[string]any) { /* 执行当前语句并推进处理流程。 */
		artifact["build"] = map[string]any{"sourceSha256": strings.Repeat("0", 64)} /* 执行当前语句并推进处理流程。 */
	}) /* 结束当前表达式或代码块。 */
	f.get(t, f.token, "3.1.0", "source", 409)                                                                                                /* 执行当前语句并推进处理流程。 */
	f.save(t, "3.2.0", map[string][]byte{"source/upload.go": []byte(protocolbuild.Template), "source/upload.zip": []byte("ambiguous")}, nil) /* 执行当前语句并推进处理流程。 */
	f.get(t, f.token, "3.2.0", "source", 409)                                                                                                /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
