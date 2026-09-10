package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/protocolbuild"
)

func TestGoSourceUploadHotSwitchFailureAndRollback(t *testing.T) {
	if !protocolbuild.Available() {
		t.Skip("Go compiler unavailable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Setenv("GOFLAGS", "-this-flag-must-not-reach-protocol-compiler")
	t.Setenv("IOT_PROTOCOL_TEST_SECRET", "must-not-reach-uploaded-code")
	defer cancel()
	repo := memory.NewRepository()
	root := t.TempDir()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DataDir = root
	cfg.JWTSecret = "source-test-secret-at-least-32-characters"
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.ExternalParser{Root: root}), log)
	engine.Metrics = metrics.New()
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), log)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, err := api.auth.Issue("tester", "tenant_001", "operator", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	viewer, _ := api.auth.Issue("reader", "tenant_001", "viewer", nil, time.Hour)
	if err = repo.SaveProduct(ctx, model.Product{TenantID: "tenant_001", ID: "source-product", Name: "源码产品", Transport: "MQTT", PayloadFormat: "hex", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	upload := func(auth, version, filename string, code []byte, cases string, status int) map[string]any {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		f, _ := form.CreateFormFile("file", filename)
		_, _ = f.Write(code)
		for k, v := range map[string]string{"version": version, "productId": "source-product", "publish": "true", "transport": "MQTT", "payloadFormat": "hex", "cases": cases} {
			_ = form.WriteField(k, v)
		}
		if version == "1.0.0" {
			_ = form.WriteField("targetPlatforms", `["linux-amd64","windows-amd64"]`)
		}
		_ = form.Close()
		req, _ := http.NewRequest("POST", server.URL+"/api/v2/protocols/source-demo/source-releases", &body)
		req.Header.Set("Authorization", "Bearer "+auth)
		req.Header.Set("Content-Type", form.FormDataContentType())
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var result map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&result)
		if resp.StatusCode != status {
			t.Fatalf("version %s: status=%d want=%d: %v", version, resp.StatusCode, status, result)
		}
		return result
	}
	check := func(rawID, version string, want float64, pinned bool) model.RawMessage {
		t.Helper()
		raw := model.RawMessage{MessageID: rawID, TenantID: "tenant_001", ProductID: "source-product", DeviceID: "source-device", Protocol: "source-demo", Transport: "MQTT", PayloadFormat: "hex", Payload: json.RawMessage(`"AA 01 2A"`)}
		if pinned {
			raw.ProtocolID = "source-demo"
			raw.ProtocolVersion = version
		}
		idx, _, err := engine.IngestRaw(ctx, raw)
		if err != nil {
			t.Fatal(err)
		}
		message, err := repo.GetStandardMessageByRaw(ctx, "tenant_001", rawID)
		if err != nil || message.Properties["temperature"] != want {
			t.Fatalf("raw=%s message=%+v err=%v", rawID, message, err)
		}
		stored, err := engine.GetRaw(ctx, idx)
		if err != nil || stored.ProtocolVersion != version {
			t.Fatalf("raw pinned version=%s want=%s err=%v", stored.ProtocolVersion, version, err)
		}
		return stored
	}
	// Authorization is enforced before the compiler or uploaded code runs.
	upload(viewer, "1.0.0", "protocol.go", []byte(protocolbuild.Template), protocolSourceCases, 403)
	first := upload(token, "1.0.0", "protocol.go", []byte(protocolbuild.Template), protocolSourceCases, 201)
	if first["binding"] == nil {
		t.Fatal("upload did not bind product")
	}
	artifact := first["release"].(map[string]any)["artifact"].(map[string]any)
	if artifact["validation"] != "PASSED" {
		t.Fatal("native samples were not executed", artifact)
	}
	variants := artifact["variants"].(map[string]any)
	for _, platform := range []string{"linux-amd64", "windows-amd64"} {
		if platform == runtime.GOOS+"-"+runtime.GOARCH {
			continue
		}
		v := variants[platform].(map[string]any)
		if v["validation"] != "COMPILED" || v["testCases"] != float64(0) {
			t.Fatal("foreign compile was misreported as execution", v)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(v["path"].(string)))); err != nil {
			t.Fatal("compiled target not persisted", err)
		}
	}
	check("raw_source_v1", "1.0.0", 42, false)
	upload(token, "1.0.0", "protocol.go", []byte(protocolbuild.Template), protocolSourceCases, 409)
	// The next version is a complete multi-file module, including a vendored dependency.
	var project bytes.Buffer
	z := zip.NewWriter(&project)
	for name, content := range map[string]string{
		"go.mod":                            "module example.com/protocol\n\ngo 1.25.0\n\nrequire example.com/scale v1.0.0\n",
		"main.go":                           strings.ReplaceAll(strings.ReplaceAll(protocolbuild.Template, "int(data[2])", "scaleTemperature(int(data[2]))"), "func main() {", "func main() { if os.Getenv(\"IOT_PROTOCOL_TEST_SECRET\") != \"\" { panic(\"secret leaked\") };"),
		"scale.go":                          "package main\nimport \"example.com/scale\"\nfunc scaleTemperature(v int) int { return scale.Apply(v) }\n",
		"vendor/modules.txt":                "# example.com/scale v1.0.0\n## explicit; go 1.25.0\nexample.com/scale\n",
		"vendor/example.com/scale/scale.go": "package scale\nfunc Apply(v int) int { return v + 1 }\n",
		"samples/cases.json":                strings.ReplaceAll(protocolSourceCases, ":42", ":43"),
	} {
		f, _ := z.Create(name)
		_, _ = io.WriteString(f, content)
	}
	_ = z.Close()
	upload(token, "1.1.0", "project.zip", project.Bytes(), "", 201)
	check("raw_source_v2", "1.1.0", 43, false)
	check("raw_source_original_version", "1.0.0", 42, true)
	bad := upload(token, "1.2.0", "bad.go", []byte("package main\nfunc main() { doesNotExist() }"), protocolSourceCases, 422)
	if bad["stage"] != "compile" || !strings.Contains(bad["buildLog"].(string), "doesNotExist") {
		t.Fatalf("missing compiler diagnostics: %v", bad)
	}
	upload(token, "1.2.0", "protocol.go", []byte(protocolbuild.Template), strings.ReplaceAll(protocolSourceCases, ":42", ":99"), 422)
	if _, err := repo.GetProtocolRelease(ctx, "tenant_001", "source-demo", "1.2.0"); err == nil {
		t.Fatal("failed sample was published")
	}
	check("raw_source_after_failure", "1.1.0", 43, false)
	// Timeout and panic in an uploaded program cannot replace the working release.
	for _, code := range []string{"package main\nfunc main(){ for {} }", "package main\nfunc main(){ panic(\"broken parser\") }"} {
		upload(token, "1.2.0", "bad.go", []byte(code), protocolSourceCases, 422)
	}
	check("raw_source_after_crash", "1.1.0", 43, false)
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/products/source-product/protocol-binding/rollback", token, map[string]any{}, 200)
	check("raw_source_rollback", "1.0.0", 42, false)
	// Rebinding the current version must preserve rollback history.
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/products/source-product/protocol-binding", token, map[string]any{"protocolId": "source-demo", "version": "1.0.0"}, 200)
	bound, _ := repo.GetProductProtocolBinding(ctx, "tenant_001", "source-product")
	if bound.PreviousVersion != "1.1.0" {
		t.Fatalf("idempotent bind lost history: %+v", bound)
	}
	// Switching protocol families must remember the previous protocol ID too.
	alternate, _ := repo.GetProtocolRelease(ctx, "tenant_001", "source-demo", "1.1.0")
	alternate.ProtocolID = "alternate-demo"
	if err := repo.CreateProtocolRelease(ctx, alternate); err != nil {
		t.Fatal(err)
	}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/products/source-product/protocol-binding", token, map[string]any{"protocolId": "alternate-demo", "version": "1.1.0"}, 200)
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/products/source-product/protocol-binding/rollback", token, map[string]any{}, 200)
	bound, _ = repo.GetProductProtocolBinding(ctx, "tenant_001", "source-product")
	if bound.ProtocolID != "source-demo" || bound.Version != "1.0.0" {
		t.Fatalf("cross-protocol rollback failed: %+v", bound)
	}
	// A failed version is retryable; a fresh parser can load the stored artifact.
	upload(token, "1.2.0", "protocol.go", []byte(protocolbuild.Template), protocolSourceCases, 201)
	release, _ := repo.GetProtocolRelease(ctx, "tenant_001", "source-demo", "1.2.0")
	if release.Artifact["build"] == nil {
		t.Fatal("source provenance missing")
	}
	zr, err := zip.OpenReader(filepath.Join(root, release.Artifact["packagePath"].(string)))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	found := false
	for _, f := range zr.File {
		if f.Name == "source/upload.go" {
			found = true
			stream, _ := f.Open()
			content, _ := io.ReadAll(stream)
			_ = stream.Close()
			if string(content) != protocolbuild.Template {
				t.Fatal("source archive differs")
			}
		}
	}
	if !found {
		t.Fatal("source not retained")
	}
	worker := parser.ExternalParser{Root: root}
	if _, err := worker.ParseWithConfig(model.RawMessage{Payload: json.RawMessage(`"AA 01 2A"`)}, release.Config); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "protocol-builds"))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "build-") {
			t.Fatalf("build directory leaked: %s", entry.Name())
		}
	}
}
