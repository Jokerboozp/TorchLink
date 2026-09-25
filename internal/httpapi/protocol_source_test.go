package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"archive/zip"       /* 执行当前语句并推进处理流程。 */
	"bytes"             /* 执行当前语句并推进处理流程。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"mime/multipart"    /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"os"                /* 执行当前语句并推进处理流程。 */
	"path/filepath"     /* 执行当前语句并推进处理流程。 */
	"runtime"           /* 执行当前语句并推进处理流程。 */
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

func TestGoSourceUploadHotSwitchFailureAndRollback(t *testing.T) { /* 定义 TestGoSourceUploadHotSwitchFailureAndRollback 函数。 */
	if !protocolbuild.Available() { /* 判断条件并选择处理分支。 */
		t.Skip("Go compiler unavailable") /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	ctx, cancel := context.WithCancel(context.Background())              /* 更新 cancel 的值。 */
	t.Setenv("GOFLAGS", "-this-flag-must-not-reach-protocol-compiler")   /* 执行当前语句并推进处理流程。 */
	t.Setenv("IOT_PROTOCOL_TEST_SECRET", "must-not-reach-uploaded-code") /* 执行当前语句并推进处理流程。 */
	defer cancel()                                                       /* 安排函数结束时执行清理。 */
	repo := memory.NewRepository()                                       /* 更新 repo 的值。 */
	root := t.TempDir()                                                  /* 更新 root 的值。 */
	archive, err := local.NewArchive(root)                               /* 更新 err 的值。 */
	if err != nil {                                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg := config.Load()                                                                                                                                 /* 更新 cfg 的值。 */
	cfg.DataDir = root                                                                                                                                   /* 更新 cfg.DataDir 的值。 */
	cfg.JWTSecret = "source-test-secret-at-least-32-characters"                                                                                          /* 更新 cfg.JWTSecret 的值。 */
	log := slog.New(slog.NewTextHandler(io.Discard, nil))                                                                                                /* 更新 log 的值。 */
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.ExternalParser{Root: root}), log) /* 更新 engine 的值。 */
	engine.Metrics = metrics.New()                                                                                                                       /* 更新 engine.Metrics 的值。 */
	if err = engine.Start(ctx); err != nil {                                                                                                             /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), log)                 /* 更新 api 的值。 */
	server := httptest.NewServer(api.Handler())                                      /* 更新 server 的值。 */
	defer server.Close()                                                             /* 安排函数结束时执行清理。 */
	token, err := api.auth.Issue("tester", "tenant_001", "operator", nil, time.Hour) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                  /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	viewer, _ := api.auth.Issue("reader", "tenant_001", "viewer", nil, time.Hour)                                                                                                       /* 更新 _ 的值。 */
	if err = repo.SaveProduct(ctx, model.Product{TenantID: "tenant_001", ID: "source-product", Name: "源码产品", Transport: "MQTT", PayloadFormat: "hex", Status: "ENABLED"}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	upload := func(auth, version, filename string, code []byte, cases string, status int) map[string]any { /* 更新 upload 的值。 */
		t.Helper()                                                                                                                                                               /* 执行当前语句并推进处理流程。 */
		var body bytes.Buffer                                                                                                                                                    /* 声明 body。 */
		form := multipart.NewWriter(&body)                                                                                                                                       /* 更新 form 的值。 */
		f, _ := form.CreateFormFile("file", filename)                                                                                                                            /* 更新 _ 的值。 */
		_, _ = f.Write(code)                                                                                                                                                     /* 更新 _ 的值。 */
		for k, v := range map[string]string{"version": version, "productId": "source-product", "publish": "true", "transport": "MQTT", "payloadFormat": "hex", "cases": cases} { /* 循环处理当前数据。 */
			_ = form.WriteField(k, v) /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
		if version == "1.0.0" { /* 判断条件并选择处理分支。 */
			_ = form.WriteField("targetPlatforms", `["linux-amd64","windows-amd64"]`) /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
		_ = form.Close()                                                                                     /* 更新 _ 的值。 */
		req, _ := http.NewRequest("POST", server.URL+"/api/v2/protocols/source-demo/source-releases", &body) /* 更新 _ 的值。 */
		req.Header.Set("Authorization", "Bearer "+auth)                                                      /* 执行当前语句并推进处理流程。 */
		req.Header.Set("Content-Type", form.FormDataContentType())                                           /* 执行当前语句并推进处理流程。 */
		resp, err := server.Client().Do(req)                                                                 /* 更新 err 的值。 */
		if err != nil {                                                                                      /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		defer resp.Body.Close()                        /* 安排函数结束时执行清理。 */
		var result map[string]any                      /* 声明 result。 */
		_ = json.NewDecoder(resp.Body).Decode(&result) /* 更新 _ 的值。 */
		if resp.StatusCode != status {                 /* 判断条件并选择处理分支。 */
			t.Fatalf("version %s: status=%d want=%d: %v", version, resp.StatusCode, status, result) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		return result /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	check := func(rawID, version string, want float64, pinned bool) model.RawMessage { /* 更新 check 的值。 */
		t.Helper()                                                                                                                                                                                                                          /* 执行当前语句并推进处理流程。 */
		raw := model.RawMessage{MessageID: rawID, TenantID: "tenant_001", ProductID: "source-product", DeviceID: "source-device", Protocol: "source-demo", Transport: "MQTT", PayloadFormat: "hex", Payload: json.RawMessage(`"AA 01 2A"`)} /* 更新 raw 的值。 */
		if pinned {                                                                                                                                                                                                                         /* 判断条件并选择处理分支。 */
			raw.ProtocolID = "source-demo" /* 更新 raw.ProtocolID 的值。 */
			raw.ProtocolVersion = version  /* 更新 raw.ProtocolVersion 的值。 */
		} /* 结束当前表达式或代码块。 */
		idx, _, err := engine.IngestRaw(ctx, raw) /* 更新 err 的值。 */
		if err != nil {                           /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		message, err := repo.GetStandardMessageByRaw(ctx, "tenant_001", rawID) /* 更新 err 的值。 */
		if err != nil || message.Properties["temperature"] != want {           /* 判断条件并选择处理分支。 */
			t.Fatalf("raw=%s message=%+v err=%v", rawID, message, err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		stored, err := engine.GetRaw(ctx, idx)               /* 更新 err 的值。 */
		if err != nil || stored.ProtocolVersion != version { /* 判断条件并选择处理分支。 */
			t.Fatalf("raw pinned version=%s want=%s err=%v", stored.ProtocolVersion, version, err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		return stored /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	// Authorization is enforced before the compiler or uploaded code runs.
	upload(viewer, "1.0.0", "protocol.go", []byte(protocolbuild.Template), protocolSourceCases, 403)         /* 执行当前语句并推进处理流程。 */
	first := upload(token, "1.0.0", "protocol.go", []byte(protocolbuild.Template), protocolSourceCases, 201) /* 更新 first 的值。 */
	if first["binding"] == nil {                                                                             /* 判断条件并选择处理分支。 */
		t.Fatal("upload did not bind product") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	artifact := first["release"].(map[string]any)["artifact"].(map[string]any) /* 更新 artifact 的值。 */
	if artifact["validation"] != "PASSED" {                                    /* 判断条件并选择处理分支。 */
		t.Fatal("native samples were not executed", artifact) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	variants := artifact["variants"].(map[string]any)                   /* 更新 variants 的值。 */
	for _, platform := range []string{"linux-amd64", "windows-amd64"} { /* 循环处理当前数据。 */
		if platform == runtime.GOOS+"-"+runtime.GOARCH { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		v := variants[platform].(map[string]any)                           /* 更新 v 的值。 */
		if v["validation"] != "COMPILED" || v["testCases"] != float64(0) { /* 判断条件并选择处理分支。 */
			t.Fatal("foreign compile was misreported as execution", v) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(v["path"].(string)))); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal("compiled target not persisted", err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	check("raw_source_v1", "1.0.0", 42, false)                                                      /* 执行当前语句并推进处理流程。 */
	upload(token, "1.0.0", "protocol.go", []byte(protocolbuild.Template), protocolSourceCases, 409) /* 执行当前语句并推进处理流程。 */
	// The next version is a complete multi-file module, including a vendored dependency.
	var project bytes.Buffer                      /* 声明 project。 */
	z := zip.NewWriter(&project)                  /* 更新 z 的值。 */
	for name, content := range map[string]string{ /* 循环处理当前数据。 */
		"go.mod":                            "module example.com/protocol\n\ngo 1.25.0\n\nrequire example.com/scale v1.0.0\n",                                                                                                                                                    /* 执行当前语句并推进处理流程。 */
		"main.go":                           strings.ReplaceAll(strings.ReplaceAll(protocolbuild.Template, "int(data[2])", "scaleTemperature(int(data[2]))"), "func main() {", "func main() { if os.Getenv(\"IOT_PROTOCOL_TEST_SECRET\") != \"\" { panic(\"secret leaked\") };"), /* 执行当前语句并推进处理流程。 */
		"scale.go":                          "package main\nimport \"example.com/scale\"\nfunc scaleTemperature(v int) int { return scale.Apply(v) }\n",                                                                                                                          /* 执行当前语句并推进处理流程。 */
		"vendor/modules.txt":                "# example.com/scale v1.0.0\n## explicit; go 1.25.0\nexample.com/scale\n",                                                                                                                                                           /* 执行当前语句并推进处理流程。 */
		"vendor/example.com/scale/scale.go": "package scale\nfunc Apply(v int) int { return v + 1 }\n",                                                                                                                                                                           /* 执行当前语句并推进处理流程。 */
		"samples/cases.json":                strings.ReplaceAll(protocolSourceCases, ":42", ":43"),                                                                                                                                                                               /* 执行当前语句并推进处理流程。 */
	} { /* 结束当前表达式或代码块。 */
		f, _ := z.Create(name)            /* 更新 _ 的值。 */
		_, _ = io.WriteString(f, content) /* 更新 _ 的值。 */
	} /* 结束当前表达式或代码块。 */
	_ = z.Close()                                                                                                             /* 更新 _ 的值。 */
	upload(token, "1.1.0", "project.zip", project.Bytes(), "", 201)                                                           /* 执行当前语句并推进处理流程。 */
	check("raw_source_v2", "1.1.0", 43, false)                                                                                /* 执行当前语句并推进处理流程。 */
	check("raw_source_original_version", "1.0.0", 42, true)                                                                   /* 执行当前语句并推进处理流程。 */
	bad := upload(token, "1.2.0", "bad.go", []byte("package main\nfunc main() { doesNotExist() }"), protocolSourceCases, 422) /* 更新 bad 的值。 */
	if bad["stage"] != "compile" || !strings.Contains(bad["buildLog"].(string), "doesNotExist") {                             /* 判断条件并选择处理分支。 */
		t.Fatalf("missing compiler diagnostics: %v", bad) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	upload(token, "1.2.0", "protocol.go", []byte(protocolbuild.Template), strings.ReplaceAll(protocolSourceCases, ":42", ":99"), 422) /* 执行当前语句并推进处理流程。 */
	if _, err := repo.GetProtocolRelease(ctx, "tenant_001", "source-demo", "1.2.0"); err == nil {                                     /* 判断条件并选择处理分支。 */
		t.Fatal("failed sample was published") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	check("raw_source_after_failure", "1.1.0", 43, false) /* 执行当前语句并推进处理流程。 */
	// Timeout and panic in an uploaded program cannot replace the working release.
	for _, code := range []string{"package main\nfunc main(){ for {} }", "package main\nfunc main(){ panic(\"broken parser\") }"} { /* 循环处理当前数据。 */
		upload(token, "1.2.0", "bad.go", []byte(code), protocolSourceCases, 422) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	check("raw_source_after_crash", "1.1.0", 43, false)                                                                                           /* 执行当前语句并推进处理流程。 */
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/products/source-product/protocol-binding/rollback", token, map[string]any{}, 200) /* 执行当前语句并推进处理流程。 */
	check("raw_source_rollback", "1.0.0", 42, false)                                                                                              /* 执行当前语句并推进处理流程。 */
	// Rebinding the current version must preserve rollback history.
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/products/source-product/protocol-binding", token, map[string]any{"protocolId": "source-demo", "version": "1.0.0"}, 200) /* 执行当前语句并推进处理流程。 */
	bound, _ := repo.GetProductProtocolBinding(ctx, "tenant_001", "source-product")                                                                                                     /* 更新 _ 的值。 */
	if bound.PreviousVersion != "1.1.0" {                                                                                                                                               /* 判断条件并选择处理分支。 */
		t.Fatalf("idempotent bind lost history: %+v", bound) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	// Switching protocol families must remember the previous protocol ID too.
	alternate, _ := repo.GetProtocolRelease(ctx, "tenant_001", "source-demo", "1.1.0") /* 更新 _ 的值。 */
	alternate.ProtocolID = "alternate-demo"                                            /* 更新 alternate.ProtocolID 的值。 */
	if err := repo.CreateProtocolRelease(ctx, alternate); err != nil {                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/products/source-product/protocol-binding", token, map[string]any{"protocolId": "alternate-demo", "version": "1.1.0"}, 200) /* 执行当前语句并推进处理流程。 */
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/products/source-product/protocol-binding/rollback", token, map[string]any{}, 200)                                          /* 执行当前语句并推进处理流程。 */
	bound, _ = repo.GetProductProtocolBinding(ctx, "tenant_001", "source-product")                                                                                                         /* 更新 _ 的值。 */
	if bound.ProtocolID != "source-demo" || bound.Version != "1.0.0" {                                                                                                                     /* 判断条件并选择处理分支。 */
		t.Fatalf("cross-protocol rollback failed: %+v", bound) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	// A failed version is retryable; a fresh parser can load the stored artifact.
	upload(token, "1.2.0", "protocol.go", []byte(protocolbuild.Template), protocolSourceCases, 201) /* 执行当前语句并推进处理流程。 */
	release, _ := repo.GetProtocolRelease(ctx, "tenant_001", "source-demo", "1.2.0")                /* 更新 _ 的值。 */
	if release.Artifact["build"] == nil {                                                           /* 判断条件并选择处理分支。 */
		t.Fatal("source provenance missing") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	zr, err := zip.OpenReader(filepath.Join(root, release.Artifact["packagePath"].(string))) /* 更新 err 的值。 */
	if err != nil {                                                                          /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	defer zr.Close()            /* 安排函数结束时执行清理。 */
	found := false              /* 更新 found 的值。 */
	for _, f := range zr.File { /* 循环处理当前数据。 */
		if f.Name == "source/upload.go" { /* 判断条件并选择处理分支。 */
			found = true                                   /* 更新 found 的值。 */
			stream, _ := f.Open()                          /* 更新 _ 的值。 */
			content, _ := io.ReadAll(stream)               /* 更新 _ 的值。 */
			_ = stream.Close()                             /* 更新 _ 的值。 */
			if string(content) != protocolbuild.Template { /* 判断条件并选择处理分支。 */
				t.Fatal("source archive differs") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if !found { /* 判断条件并选择处理分支。 */
		t.Fatal("source not retained") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	worker := parser.ExternalParser{Root: root}                                                                                 /* 更新 worker 的值。 */
	if _, err := worker.ParseWithConfig(model.RawMessage{Payload: json.RawMessage(`"AA 01 2A"`)}, release.Config); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	entries, _ := os.ReadDir(filepath.Join(root, "protocol-builds")) /* 更新 _ 的值。 */
	for _, entry := range entries {                                  /* 循环处理当前数据。 */
		if strings.HasPrefix(entry.Name(), "build-") { /* 判断条件并选择处理分支。 */
			t.Fatalf("build directory leaked: %s", entry.Name()) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
