package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/auth"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"iot-platform/internal/protocolbuild"
	"iot-platform/internal/protocolruntime"
	"iot-platform/internal/protocolworker"
)

// protocolDataDir is a data directory for published protocol releases. Its
// resident Workers are stopped before the directory is removed, because
// Windows cannot delete a running executable.
func protocolDataDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Cleanup(func() { parser.StopResidentWorkers(root) })
	return root
}

func TestGoSourceUploadHotSwitchFailureAndRollback(t *testing.T) {
	if !protocolbuild.Available() {
		t.Skip("Go compiler unavailable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Setenv("GOFLAGS", "-this-flag-must-not-reach-protocol-compiler")
	t.Setenv("IOT_PROTOCOL_TEST_SECRET", "must-not-reach-uploaded-code")
	defer cancel()
	repo := memory.NewRepository()
	root := protocolDataDir(t)
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DataDir = root
	cfg.JWTSecret = "source-test-secret-at-least-32-characters"
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.ExternalParser{Root: root}), log)
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

func TestLegacyGoProtocolUploadIsUnavailable(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DataDir = t.TempDir()
	cfg.DevMode = true
	cfg.JWTSecret = "test-secret-at-least-32-characters"
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.ExternalParser{Root: cfg.DataDir}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.Metrics = metrics.New()
	if err := engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	login := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "admin123", "tenantId": "tenant_001"}, http.StatusOK)
	token := login["accessToken"].(string)
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/protocol-packages", token, map[string]any{
		"id": "protocol_go", "name": "Go Worker", "parserType": parser.GoProtocolParserName,
	}, http.StatusUnprocessableEntity)
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/protocol-packages/protocol_go/artifact", token, map[string]any{}, http.StatusNotFound)
}

func TestSourceManifestUsesPackageMetadataAndExplicitOverrides(t *testing.T) {
	req := httptest.NewRequest("POST", "/", nil)
	files := map[string][]byte{"protocol.json": []byte(`{"id":"package","name":"Vendor","version":"1.0.0","runtime":"go-protocol-v2","transport":"TCP_UDP","payloadFormat":"hex","capabilities":["decode","ingress","encode"],"entrypoint":"cmd/worker"}`)}
	m, entry, err := sourceProtocolManifest(req, "package", files)
	if err != nil || m.Runtime != protocolworker.Runtime || m.Version != "1.0.0" || m.Transport != "TCP_UDP" || entry != "cmd/worker" {
		t.Fatalf("metadata %+v %s %v", m, entry, err)
	}
	req.Form = url.Values{"version": {"1.1.0"}, "transport": {"TCP"}}
	m, _, err = sourceProtocolManifest(req, "package", files)
	if err != nil || m.Version != "1.1.0" || m.Transport != "TCP" {
		t.Fatalf("override %+v %v", m, err)
	}
	if _, _, err = sourceProtocolManifest(req, "other", files); err == nil {
		t.Fatal("mismatched source id accepted")
	}
	req.Form.Set("runtime", "go-json-lines-v1")
	if _, _, err = sourceProtocolManifest(req, "package", files); err == nil {
		t.Fatal("v1 accepted ingress/encode")
	}
	req.Form.Set("capabilities", `["decode"]`)
	if _, _, err = sourceProtocolManifest(req, "package", files); err == nil {
		t.Fatal("decode-only v1 accepted")
	}
	req.Form = url.Values{"version": {"1.0.0"}}
	m, _, err = sourceProtocolManifest(req, "package", nil)
	if err != nil || m.Runtime != protocolworker.Runtime {
		t.Fatalf("current runtime default: %+v %v", m, err)
	}

}

func TestGoFunctionsUploadAndListener(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	root := protocolDataDir(t)
	repo := memory.NewRepository()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	if err := engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DataDir = root
	api := New(cfg, engine, metrics.New(), log)
	ingested := make(chan model.RawMessage, 16)
	listeners := protocolruntime.NewListeners(repo, root, func(ctx context.Context, raw model.RawMessage) error {
		_, _, err := engine.IngestRaw(ctx, raw)
		if err == nil {
			ingested <- raw
		}
		return err
	}, log)
	api.SetProtocolListeners(listeners)
	assets := http.FileServer(http.Dir(filepath.Join("..", "..", "iot_front", "dist")))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.Handler().ServeHTTP(w, r)
		} else {
			assets.ServeHTTP(w, r)
		}
	}))
	defer server.Close()
	token, _ := api.auth.Issue("operator", "tenant", "operator", nil, time.Hour)
	viewer, _ := api.auth.Issue("viewer", "tenant", "viewer", nil, time.Hour)
	if err := repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "product", Name: "Go 函数产品", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	upload := func(auth, name string, code []byte, fields map[string]string, want int) model.ProtocolRelease {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		f, _ := form.CreateFormFile("file", name)
		f.Write(code)
		if _, explicitProduct := fields["productId"]; !explicitProduct {
			form.WriteField("productId", "product")
			form.WriteField("publish", "true")
		}
		for k, v := range fields {
			form.WriteField(k, v)
		}
		form.Close()
		req := httptest.NewRequest("POST", "/api/v2/protocols/functions/source-releases", &body)
		req.Header.Set("Authorization", "Bearer "+auth)
		req.Header.Set("Content-Type", form.FormDataContentType())
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("upload %s: %d want %d: %s", name, w.Code, want, w.Body.String())
		}
		var result struct{ Release model.ProtocolRelease }
		json.Unmarshal(w.Body.Bytes(), &result)
		return result.Release
	}
	// No runtime, JSON samples, metadata or module required for a single Go file.
	upload(viewer, "protocol.go", []byte(protocolbuild.FunctionTemplate), nil, 403)
	first := upload(token, "protocol.go", []byte(protocolbuild.FunctionTemplate), nil, 201)
	// The platform adapter serves repeated requests, so the release stays resident.
	if !strings.HasPrefix(first.Version, "auto-") || first.Artifact["runtime"] != protocolworker.Runtime || first.Artifact["workerMode"] != parser.WorkerModeServe {
		t.Fatalf("release %+v", first)
	}
	// A template ID never implicitly publishes or switches an uploaded version.
	upload(token, "protocol.go", []byte(protocolbuild.FunctionTemplate), map[string]string{"productId": "product", "version": "implicit-bind"}, 422)
	if _, err := repo.GetProtocolRelease(ctx, "tenant", "functions", "implicit-bind"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("rejected implicit bind persisted a release: %v", err)
	}
	if err := repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "in-use", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ID: "upload-guard", ProductID: "in-use", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	// Even invalid source must be rejected before compilation when a combined
	// upload would replace the version used by registered devices.
	upload(token, "protocol.go", []byte("this source must not compile"), map[string]string{"productId": "in-use", "publish": "true", "version": "bound-blocked"}, 409)
	if _, err := repo.GetProtocolRelease(ctx, "tenant", "functions", "bound-blocked"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("rejected production bind persisted a release: %v", err)
	}
	// An upload without publish is a validated version only. Its decoder can
	// be previewed without switching an existing template or ingesting data.
	previewRelease := upload(token, "protocol.go", []byte(protocolbuild.FunctionTemplate), map[string]string{"productId": "", "version": "preview-only"}, 201)
	if previewRelease.Status != "VALIDATED" || previewRelease.PublishedAt != 0 {
		t.Fatalf("upload unexpectedly published: %+v", previewRelease)
	}
	previewPath := server.URL + "/api/v2/protocols/functions/releases/preview-only/preview"
	previewResult := requestJSON(t, server.Client(), "POST", previewPath, token, map[string]any{"payload": "AA012A", "readOnly": true}, 200)
	if previewResult["standardMessage"].(map[string]any)["properties"].(map[string]any)["temperature"] != float64(42) {
		t.Fatal(previewResult)
	}
	requestJSON(t, server.Client(), "POST", previewPath, viewer, map[string]any{"payload": "AA012A"}, 403)
	other, _ := api.auth.Issue("operator", "other", "operator", nil, time.Hour)
	requestJSON(t, server.Client(), "POST", previewPath, other, map[string]any{"payload": "AA012A"}, 404)
	requestJSON(t, server.Client(), "POST", previewPath, token, map[string]any{"payload": "broken"}, 422)
	indexes, _ := repo.ListRawIndexes(ctx, ports.RawFilter{TenantID: "tenant"})
	alarms, _ := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant"})
	bound, _ := repo.GetProductProtocolBinding(ctx, "tenant", "product")
	if len(indexes) != 0 || len(alarms) != 0 || bound.Version != first.Version {
		t.Fatalf("preview changed business state: indexes=%d alarms=%d binding=%+v", len(indexes), len(alarms), bound)
	}
	t.Run("archived preview preserves authorized full context", func(t *testing.T) {
		code := strings.Replace(protocolbuild.Template, "type RawMessage struct {", "type RawMessage struct {\n ProductID string `json:\"productId\"`\n GatewayID string `json:\"gatewayId\"`\n Headers map[string]string `json:\"headers\"`", 1)
		code = strings.Replace(code, `"temperature": int(data[2]),`, `"temperature": int(data[2]), "sourceProduct":raw.ProductID, "sourceGateway":raw.GatewayID, "sourceHeaders":raw.Headers, "sourceMetadata":raw.Metadata,`, 1)
		release := upload(token, "protocol.go", []byte(code), map[string]string{"productId": "", "version": "archived-context", "transport": "MQTT", "payloadFormat": "hex", "cases": protocolSourceCases}, 201)
		archived := model.RawMessage{MessageID: "raw_preview_snapshot", TenantID: "tenant", ProductID: "snapshot-product", DeviceID: "snapshot-device", GatewayID: "snapshot-gateway", Source: "device-mqtt", Protocol: "functions", ProtocolID: "functions", ProtocolVersion: release.Version, Transport: "MQTT", PayloadFormat: "hex", Payload: json.RawMessage(`"AA012A"`), ReceivedAt: 123456789, Headers: map[string]string{"topic": "vendor/site/7"}, Metadata: map[string]any{"vendor": map[string]any{"zone": "first-floor"}, "protocolState": map[string]any{"sequence": 7}}}
		index, err := archive.PutRaw(ctx, archived)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = repo.SaveRawIndex(ctx, index); err != nil {
			t.Fatal(err)
		}
		beforeIndexes, _ := repo.ListRawIndexes(ctx, ports.RawFilter{TenantID: "tenant"})
		beforeArchive, _ := archive.GetRaw(ctx, index)
		beforeJSON, _ := json.Marshal(beforeArchive)
		check := func(permissions map[string]bool, scope deviceScope, version string, body map[string]any, want int) map[string]any {
			t.Helper()
			requestCtx := context.WithValue(ctx, claimsKey, auth.Claims{TenantID: "tenant"})
			requestCtx = context.WithValue(requestCtx, permissionsKey{}, permissions)
			requestCtx = context.WithValue(requestCtx, deviceScopeKey{}, scope)
			data, _ := json.Marshal(body)
			req := httptest.NewRequest("POST", "/", bytes.NewReader(data)).WithContext(requestCtx)
			req.SetPathValue("id", "functions")
			req.SetPathValue("version", version)
			response := httptest.NewRecorder()
			api.previewGeneratedRelease(response, req)
			if response.Code != want {
				t.Fatalf("preview got %d want %d: %s", response.Code, want, response.Body.String())
			}
			var result map[string]any
			_ = json.Unmarshal(response.Body.Bytes(), &result)
			return result
		}
		permission := map[string]bool{"menu:raw": true}
		scope := deviceScope{Tenant: "tenant", IDs: map[string]bool{archived.DeviceID: true}}
		input := map[string]any{"rawMessageId": archived.MessageID}
		check(map[string]bool{}, scope, release.Version, input, 403)
		check(permission, deviceScope{Tenant: "tenant", IDs: map[string]bool{"other": true}}, release.Version, input, 404)
		check(permission, scope, previewRelease.Version, input, 409)
		result := check(permission, scope, release.Version, input, 200)
		standard := result["standardMessage"].(map[string]any)
		properties := standard["properties"].(map[string]any)
		metadata := properties["sourceMetadata"].(map[string]any)
		if standard["rawMessageId"] != archived.MessageID || standard["productId"] != archived.ProductID || standard["timestamp"] != float64(archived.ReceivedAt) || properties["sourceProduct"] != archived.ProductID || properties["sourceGateway"] != archived.GatewayID || properties["sourceHeaders"].(map[string]any)["topic"] != "vendor/site/7" || metadata["vendor"].(map[string]any)["zone"] != "first-floor" || metadata["protocolState"].(map[string]any)["sequence"] != float64(7) {
			t.Fatal("archive context lost", result)
		}
		input["payload"] = "AA012B"
		input["state"] = map[string]any{"sequence": 8}
		result = check(permission, scope, release.Version, input, 200)
		properties = result["standardMessage"].(map[string]any)["properties"].(map[string]any)
		if properties["temperature"] != float64(43) || properties["sourceMetadata"].(map[string]any)["protocolState"].(map[string]any)["sequence"] != float64(8) {
			t.Fatal("sample edits were not used", result)
		}
		afterArchive, _ := archive.GetRaw(ctx, index)
		afterJSON, _ := json.Marshal(afterArchive)
		afterIndexes, _ := repo.ListRawIndexes(ctx, ports.RawFilter{TenantID: "tenant"})
		stored, _ := repo.GetProtocolRelease(ctx, "tenant", "functions", release.Version)
		if !bytes.Equal(beforeJSON, afterJSON) || len(beforeIndexes) != len(afterIndexes) || stored.Status != release.Status {
			t.Fatal("preview changed archive or release")
		}
		if _, err = repo.GetStandardMessageByRaw(ctx, "tenant", archived.MessageID); !errors.Is(err, model.ErrNotFound) {
			t.Fatal("preview persisted a standard message", err)
		}
	})
	raw := model.RawMessage{MessageID: "raw_functions", TenantID: "tenant", ProductID: "product", DeviceID: "device", Protocol: "functions", PayloadFormat: "hex", Payload: json.RawMessage(`"AA012A"`)}
	if _, _, err := engine.IngestRaw(ctx, raw); err != nil {
		t.Fatal(err)
	}
	message, err := repo.GetStandardMessageByRaw(ctx, "tenant", raw.MessageID)
	if err != nil || message.Properties["temperature"] != float64(42) {
		t.Fatalf("message %+v %v", message, err)
	}
	bad := strings.Replace(protocolbuild.FunctionTemplate, "int(data[2])", "99", 1)
	upload(token, "protocol.go", []byte(bad), nil, 422)
	upload(token, "protocol.go", []byte(strings.Replace(protocolbuild.FunctionTemplate, "Samples: []Sample{{", "Samples: []Sample{/*", 1)), nil, 422)
	upload(token, "protocol.go", []byte(protocolbuild.FunctionTemplate), map[string]string{"capabilities": `["decode"]`}, 422)
	// Go samples are mandatory; supplied expected event/tag/time fields must
	// actually be checked rather than silently accepted as metadata.
	upload(token, "protocol.go", []byte(`package main
func Protocol() Definition {return Definition{Decode:func([]byte,Context)(Message,error){return properties(map[string]any{}),nil}}}`), nil, 422)
	for _, field := range []string{`Event:map[string]any{"type":"WRONG"}`, `Tags:map[string]string{"site":"WRONG"}`, `Timestamp:123`} {
		code := `package main
func Protocol() Definition {return Definition{
 Decode:func([]byte,Context)(Message,error){return properties(map[string]any{"temperature":42}),nil},
 Samples:[]Sample{{Data:[]byte{1},Want:Message{MessageType:"PROPERTY_REPORT",` + field + `}}},
}}`
		upload(token, "protocol.go", []byte(code), nil, 422)
	}
	binding, err := repo.GetProductProtocolBinding(ctx, "tenant", "product")
	if err != nil || binding.Version != first.Version {
		t.Fatalf("failed upload replaced binding %+v %v", binding, err)
	}
	// Downloaded template is a complete independently buildable Go project.
	req := httptest.NewRequest("GET", "/api/v2/protocol-source-template?format=go-functions&kind=tcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	sources, err := protocolbuild.Sources("template.zip", w.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	var wrapped bytes.Buffer
	zw := zip.NewWriter(&wrapped)
	for name, data := range sources {
		if strings.HasSuffix(name, ".json") {
			t.Fatal("template requires JSON", name)
		}
		os.WriteFile(filepath.Join(project, name), data, 0600)
		f, _ := zw.Create("my-protocol/" + name)
		f.Write(data)
	}
	zw.Close()
	command := exec.CommandContext(ctx, "go", "test", "./...")
	command.Dir = project
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("standalone template: %v %s", err, output)
	}
	second := upload(token, "project.zip", wrapped.Bytes(), map[string]string{"version": "2.0.0"}, 201)
	if len(second.Capabilities) != 3 {
		t.Fatal(second.Capabilities)
	}
	upload(token, "project.zip", wrapped.Bytes(), map[string]string{"version": "2.0.0"}, 409)
	// One-shot sample workbench exercises frame buffers, ACKs and encoding
	// without starting a socket, registering the observed identity or ingesting.
	workbenchPath := server.URL + "/api/v2/protocols/functions/releases/2.0.0/preview"
	beforeIndexes, _ := repo.ListRawIndexes(ctx, ports.RawFilter{TenantID: "tenant"})
	ingress := requestJSON(t, server.Client(), "POST", workbenchPath, token, map[string]any{"operation": "ingress", "readOnly": true, "chunks": []string{"AA01", "072ADC AA01072ADC"}, "expected": map[string]any{"needMore": false, "frames": []any{map[string]any{"frameHex": "AA01072ADC", "reply": "aa02072add", "standardMessage": map[string]any{"properties": map[string]any{"temperature": 42}}}, map[string]any{"consumed": 5}}}}, 200)
	actual := ingress["operationResult"].(map[string]any)
	if len(actual["frames"].([]any)) != 2 || len(actual["attempts"].([]any)) != 3 || !ingress["comparison"].(map[string]any)["matched"].(bool) {
		t.Fatal("split/concatenated sample mismatch", ingress)
	}
	half := requestJSON(t, server.Client(), "POST", workbenchPath, token, map[string]any{"operation": "ingress", "readOnly": true, "payload": "AA01"}, 200)
	if half["operationResult"].(map[string]any)["needMore"] != true {
		t.Fatal("partial sample lost", half)
	}
	requestJSON(t, server.Client(), "POST", workbenchPath, token, map[string]any{"operation": "ingress", "readOnly": true, "payload": "AA01", "datagram": true}, 422)
	state := map[string]any{"device": 7, "sequence": 0}
	encoded := requestJSON(t, server.Client(), "POST", workbenchPath, token, map[string]any{"operation": "encode", "readOnly": true, "state": state, "command": map[string]any{"type": "ping"}, "expected": map[string]any{"reply": "aa030701b5", "correlationId": "1"}}, 200)
	if !encoded["comparison"].(map[string]any)["matched"].(bool) {
		t.Fatal("encoded sample mismatch", encoded)
	}
	ack := requestJSON(t, server.Client(), "POST", workbenchPath, token, map[string]any{"operation": "ingress", "readOnly": true, "payload": "AA020701B4", "state": map[string]any{"device": 7, "sequence": 1}}, 200)
	if ack["operationResult"].(map[string]any)["frames"].([]any)[0].(map[string]any)["correlationId"] != "1" {
		t.Fatal("ACK correlation lost", ack)
	}
	mismatch := requestJSON(t, server.Client(), "POST", workbenchPath, token, map[string]any{"operation": "decode", "readOnly": true, "payload": "AA01072ADC", "expected": map[string]any{"standardMessage": map[string]any{"properties": map[string]any{"temperature": 43}}}}, 200)
	comparison := mismatch["comparison"].(map[string]any)
	if comparison["matched"] != false || comparison["differences"].([]any)[0].(map[string]any)["path"] != "$.standardMessage.properties.temperature" {
		t.Fatal("mismatch not explained", comparison)
	}
	requestJSON(t, server.Client(), "POST", previewPath, token, map[string]any{"operation": "encode", "readOnly": true, "command": map[string]any{"type": "ping"}}, 422)
	afterIndexes, _ := repo.ListRawIndexes(ctx, ports.RawFilter{TenantID: "tenant"})
	if len(beforeIndexes) != len(afterIndexes) {
		t.Fatal("workbench ingested raw messages")
	}
	if _, err := repo.GetManagedDevice(ctx, "tenant", "7"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("workbench registered observed device", err)
	}
	storedRelease, _ := repo.GetProtocolRelease(ctx, "tenant", "functions", "2.0.0")
	if storedRelease.Artifact["workerMode"] != parser.WorkerModeServe {
		t.Fatal("preview mutated saved execution configuration")
	}
	// Full template's samples exercise ingress, decode, ACK matching and encode.
	// Also prove real sockets still use inventory, archive and StandardMessage.
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := free.Addr().(*net.TCPAddr).Port
	free.Close()
	profile := model.DeviceAccessProfile{ID: "functions-tcp", ProductID: "product", ProtocolID: "functions", ProtocolVersion: second.Version, Mode: "listener", Network: "tcp", Host: "127.0.0.1", Port: port, TimeoutMs: 2000, Enabled: true, AutoRegister: true}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/device-access-profiles", token, profile, 201)
	listeners.Start(ctx)
	var conn net.Conn
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		conn, err = net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), time.Millisecond*100)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	frame := []byte{0xAA, 1, 7, 42, 0xDC}
	conn.Write(frame[:2])
	conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	reply := make([]byte, 5)
	if _, err := conn.Read(reply); err == nil {
		t.Fatal("half frame received ACK")
	}
	conn.Write(append(frame[2:], frame...))
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for i := 0; i < 2; i++ {
		if _, err := io.ReadFull(conn, reply); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(reply, []byte{0xAA, 2, 7, 42, 0xDD}) {
			t.Fatalf("ACK %x", reply)
		}
		select {
		case raw := <-ingested:
			message, err := repo.GetStandardMessageByRaw(ctx, "tenant", raw.MessageID)
			if err != nil || message.DeviceID != "7" || message.Properties["temperature"] != float64(42) {
				t.Fatalf("TCP message %+v %v", message, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no archived frame")
		}
	}
	// Downlink is encoded by the uploaded Go function, sent on the live socket,
	// then acknowledged only when the matching device response arrives.
	commandDone := make(chan error, 1)
	go func() {
		result, err := listeners.Command(ctx, "tenant", profile.ID, "7", map[string]any{"type": "ping"})
		if err == nil && result["status"] != "acknowledged" {
			err = fmt.Errorf("command status %v", result)
		}
		commandDone <- err
	}()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reply, []byte{0xAA, 3, 7, 1, 0xB5}) {
		t.Fatalf("downlink %x", reply)
	}
	conn.Write([]byte{0xAA, 2, 7, 1, 0xB4})
	select {
	case err := <-commandDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("command not acknowledged")
	}
	select {
	case raw := <-ingested:
		message, err := repo.GetStandardMessageByRaw(ctx, "tenant", raw.MessageID)
		if err != nil || message.MessageType != model.CommandReply {
			t.Fatalf("ACK message %+v %v", message, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ACK not archived")
	}
	// Invalid checksum must not be acknowledged or archived.
	frame[4] = 0
	conn.Write(frame)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.Read(reply); err == nil {
		t.Fatal("bad checksum acknowledged")
	}
	select {
	case <-ingested:
		t.Fatal("bad checksum archived")
	default:
	}
	t.Run("browser", func(t *testing.T) {
		if os.Getenv("IOT_TEST_BROWSER") == "" {
			t.Skip("Chrome not configured")
		}
		source := filepath.Join(t.TempDir(), "protocol.go")
		os.WriteFile(source, []byte(protocolbuild.FunctionTemplate), 0600)
		os.WriteFile(source+".invalid.go", []byte("package main\nfunc Protocol() Definition { invalid }"), 0600)
		command := exec.CommandContext(ctx, "node", filepath.Join("..", "..", "iot_front", "tests", "browser", "go-functions-check.mjs"))
		command.Env = append(os.Environ(), "IOT_TEST_ORIGIN="+server.URL, "IOT_TEST_TOKEN="+token, "IOT_TEST_SOURCE_GO="+source)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("browser: %v %s", err, output)
		} else {
			t.Log(string(output))
		}
	})
	if _, err := repo.GetProtocolRelease(ctx, "tenant", "functions-browser", "invalid-browser"); err == nil {
		t.Fatal("browser compile failure published")
	}
}

func TestImportModbusTCPV2RequiresGoPackage(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.ModbusTCPParser{}), log)
	engine.Metrics = metrics.New()
	if err = engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DataDir = t.TempDir()
	cfg.JWTSecret = "protocol-v2-test-secret-at-least-32"
	cfg.AdminTenants = []string{"tenant_001"}
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), log)
	token, err := api.auth.Issue("tester", "tenant_001", "operator", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	csv := []byte("标识,名称,功能码,地址,数据类型,倍率\ntemperature,温度,03,40001,int16,0.1\n")
	doImport := func() int {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, _ := writer.CreateFormFile("file", "points.csv")
		_, _ = part.Write(csv)
		for key, value := range map[string]string{"protocolId": "pump-modbus", "version": "1.0.0", "name": "消防泵 Modbus", "productId": "pump-product", "deviceId": "pump-01", "host": "127.0.0.1"} {
			_ = writer.WriteField(key, value)
		}
		_ = writer.Close()
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v2/modbus-tcp/import", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+token)
		response, requestErr := server.Client().Do(req)
		if requestErr != nil {
			t.Fatal(requestErr)
		}
		defer response.Body.Close()
		return response.StatusCode
	}
	if status := doImport(); status != http.StatusUnprocessableEntity {
		t.Fatalf("new builtin import status=%d", status)
	}
	if _, err := repo.GetProtocolRelease(context.Background(), "tenant_001", "pump-modbus", "1.0.0"); err == nil {
		t.Fatal("legacy release unexpectedly created")
	}

}

func TestProtocolPackageV2RejectsTraversal(t *testing.T) {
	var body bytes.Buffer
	writer := zip.NewWriter(&body)
	entry, err := writer.Create("../artifact")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = entry.Write([]byte("bad"))
	_ = writer.Close()
	reader, err := zip.NewReader(bytes.NewReader(body.Bytes()), int64(body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = inspectProtocolPackageV2(reader); err == nil {
		t.Fatal("expected traversal package to be rejected")
	}
}

func TestProtocolPackageV2RejectsDuplicateNormalizedEntry(t *testing.T) {
	var body bytes.Buffer
	writer := zip.NewWriter(&body)
	for _, name := range []string{"workers/artifact", "workers/./artifact"} {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = entry.Write([]byte(name))
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(body.Bytes()), int64(body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = inspectProtocolPackageV2(reader); err == nil {
		t.Fatal("expected duplicate normalized entry to be rejected")
	}
}

func TestRemovedFieldProfilesCannotRunOnCentre(t *testing.T) {
	base := model.DeviceAccessProfile{ID: "p", TenantID: "t", DeviceID: "d", ProductID: "product", ProtocolID: "protocol", ProtocolVersion: "1", Host: "127.0.0.1", Port: 502, UnitID: 1, TimeoutMs: 1000, Mode: "poll", Network: "tcp"}
	if err := validateAccessProfile(base); err != nil {
		t.Fatal(err)
	}
	for _, network := range []string{"serial", "opc_ua", "snmp", "bacnet", "onvif"} {
		p := base
		p.Network = network
		if err := validateAccessProfile(p); err == nil {
			t.Fatalf("removed network %s accepted", network)
		}
	}
	for _, mode := range []string{"poll", "listener"} {
		p := base
		p.Mode = mode
		p.EdgeNodeID = "legacy-node"
		if err := validateAccessProfile(p); err == nil {
			t.Fatalf("legacy %s assignment accepted", mode)
		}
	}
}

func TestStandardProtocolReadOnlyPreview(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
	api := New(config.Load(), engine, metrics.New(), log)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, _ := api.auth.Issue("operator", "tenant", "operator", nil, time.Hour)
	// Nil config is valid. Even an unexpected draft status must not advance
	// through the standard protocol's read-only sample endpoint.
	release := model.ProtocolRelease{TenantID: "tenant", ProtocolID: parser.StandardProtocolID, Version: "1.0.0", ParserType: parser.StandardParserName, Transport: "MQTT_HTTP", PayloadFormat: "json", Status: "DRAFT"}
	if err = repo.CreateProtocolRelease(ctx, release); err != nil {
		t.Fatal(err)
	}
	path := server.URL + "/api/v2/protocols/iot-standard/releases/1.0.0/preview"
	now := time.Now().UnixMilli()
	for _, tc := range []struct {
		kind string
		want model.MessageType
		body map[string]any
	}{
		{"property", model.PropertyReport, map[string]any{"data": map[string]any{"temperature": 25}}},
		{"event", model.EventReport, map[string]any{"event": "selfTest", "data": map[string]any{"result": "ok"}}},
		{"alarm", model.AlarmReport, map[string]any{"data": map[string]any{"alarmType": "smoke"}}},
		{"state", model.StateChange, map[string]any{"online": false}},
		{"command-reply", model.CommandReply, map[string]any{"commandId": "preview-command", "success": true}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			tc.body["id"], tc.body["timestamp"], tc.body["version"] = "sample", now, "1.0"
			result := requestJSON(t, server.Client(), "POST", path, token, map[string]any{"messageKind": tc.kind, "payload": tc.body, "expected": map[string]any{"standardMessage": map[string]any{"messageType": tc.want}}}, 200)
			if result["standardMessage"].(map[string]any)["messageType"] != string(tc.want) || result["comparison"].(map[string]any)["matched"] != true || result["release"].(map[string]any)["status"] != "DRAFT" {
				t.Fatal(result)
			}
		})
	}
	requestJSON(t, server.Client(), "POST", path, token, map[string]any{"payload": map[string]any{"id": "missing-kind", "timestamp": now, "data": map[string]any{"temperature": 25}}}, 422)
	requestJSON(t, server.Client(), "POST", path, token, map[string]any{"operation": "ingress", "chunks": []string{"AA"}}, 422)
	archived := model.RawMessage{MessageID: "raw_standard_preview", TenantID: "tenant", ProductID: "archived-product", DeviceID: "archived-device", Protocol: parser.StandardProtocolID, ProtocolID: parser.StandardProtocolID, ProtocolVersion: "1.0.0", PayloadFormat: "json", Payload: json.RawMessage(fmt.Sprintf(`{"id":"sample","timestamp":%d,"data":{"alarmType":"smoke"}}`, now)), ReceivedAt: now, Headers: map[string]string{"messageKind": "alarm"}}
	index, err := archive.PutRaw(ctx, archived)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.SaveRawIndex(ctx, index); err != nil {
		t.Fatal(err)
	}
	result := requestJSON(t, server.Client(), "POST", path, token, map[string]any{"rawMessageId": archived.MessageID}, 200)
	message := result["standardMessage"].(map[string]any)
	if message["messageType"] != string(model.AlarmReport) || message["productId"] != archived.ProductID || message["deviceId"] != archived.DeviceID {
		t.Fatal("archive kind or identity lost", result)
	}
	requestJSON(t, server.Client(), "POST", path, token, map[string]any{"rawMessageId": archived.MessageID, "messageKind": "property"}, 422)
	indexes, _ := repo.ListRawIndexes(ctx, ports.RawFilter{TenantID: "tenant"})
	alarms, _ := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant"})
	states, _ := repo.ListDeviceStates(ctx, "tenant")
	products, _ := repo.ListProducts(ctx, "tenant")
	stored, _ := repo.GetProtocolRelease(ctx, "tenant", release.ProtocolID, release.Version)
	if len(indexes) != 1 || len(alarms) != 0 || len(states) != 0 || len(products) != 0 || stored.Status != "DRAFT" {
		t.Fatal("standard preview produced business state")
	}
	if _, err = repo.GetStandardMessageByRaw(ctx, "tenant", archived.MessageID); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("standard preview persisted its result", err)
	}
	actualRaw, _ := archive.GetRaw(ctx, index)
	if actualRaw.Headers["messageKind"] != "alarm" || !bytes.Equal(actualRaw.Payload, archived.Payload) {
		t.Fatal("standard preview changed archive")
	}
}

func TestUploadedProtocolLifecycle(t *testing.T) {
	repo := memory.NewRepository()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Load()
	cfg.JWTSecret = "protocol-generation-test-32-chars"
	api := New(cfg, &core.Engine{Repo: repo, Parsers: parser.NewPlatformRegistry(t.TempDir())}, metrics.New(), log)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, _ := api.auth.Issue("operator", "tenant", "operator", nil, time.Hour)
	viewer, _ := api.auth.Issue("viewer", "tenant", "viewer", nil, time.Hour)
	other, _ := api.auth.Issue("operator", "other", "operator", nil, time.Hour)
	upload := func(kind, filename string, data []byte, transport, format, auth string, status int) model.ProtocolAssistantDraft {
		t.Helper()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		for k, v := range map[string]string{"inputKind": kind, "transport": transport, "payloadFormat": format, "name": "上传生成"} {
			_ = form.WriteField(k, v)
		}
		file, _ := form.CreateFormFile("file", filename)
		_, _ = file.Write(data)
		_ = form.Close()
		r, _ := http.NewRequest("POST", server.URL+"/api/v1/ai/protocol-assistant/generate", &body)
		r.Header.Set("Authorization", "Bearer "+auth)
		r.Header.Set("Content-Type", form.FormDataContentType())
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		if response.StatusCode != status {
			t.Fatalf("upload %s status %d: %s", filename, response.StatusCode, raw)
		}
		var draft model.ProtocolAssistantDraft
		if status == 200 {
			if err = json.Unmarshal(raw, &draft); err != nil {
				t.Fatal(err)
			}
		}
		return draft
	}
	draft := upload("sample", "report.json", []byte(`{"data":{"temperature":25.5,"smoke":false}}`), "MQTT", "json", token, 200)
	if draft.ParserType != "configurable_json_parser" || draft.Preview.Properties["temperature"] != 25.5 {
		t.Fatal(draft)
	}
	upload("sample", "report.json", []byte(`{"temperature":1}`), "MQTT", "json", viewer, 403)
	body := map[string]any{"id": "json-generated", "version": "1.0.0", "draft": draft, "payload": map[string]any{"data": map[string]any{"temperature": 31.5, "smoke": true}}}
	result := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/ai/protocol-assistant/publish", token, body, 201)
	if result["release"].(map[string]any)["status"] != "VALIDATED" || result["standardMessage"].(map[string]any)["properties"].(map[string]any)["temperature"] != 31.5 {
		t.Fatal(result)
	}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/ai/protocol-assistant/publish", token, body, 409)
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/protocols/json-generated/releases/1.0.0/publish", other, map[string]any{}, 404)
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/protocols/json-generated/releases/1.0.0/publish", token, map[string]any{}, 200)
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/products", token, map[string]any{"id": "generated-product", "name": "报文产品", "protocolPackageId": "json-generated@1.0.0"}, 201)
	if _, err := repo.GetProduct(context.Background(), "other", "generated-product"); err == nil {
		t.Fatal("cross tenant product")
	}
	table := []byte("identifier,name,functionCode,address,addressNotation,dataType,scale\ntemperature,温度,3,0,zero_based,uint16,0.1\n")
	draft = upload("point-table", "points.csv", table, "MODBUS_TCP", "hex", token, 200)
	body = map[string]any{"id": "points-generated", "version": "1.0.0", "draft": draft}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/ai/protocol-assistant/publish", token, body, 201)
	path := server.URL + "/api/v2/protocols/points-generated/releases/1.0.0"
	requestJSON(t, server.Client(), "POST", path+"/publish", token, map[string]any{}, 422)
	requestJSON(t, server.Client(), "POST", path+"/preview", other, map[string]any{"payload": "bad"}, 404)
	requestJSON(t, server.Client(), "POST", path+"/preview", token, map[string]any{"payload": "00 01", "startAddress": 0}, 422)
	readOnly := requestJSON(t, server.Client(), "POST", path+"/preview", token, map[string]any{"payload": "00 01 00 00 00 05 01 03 02 00 FA", "startAddress": 0, "readOnly": true}, 200)
	if readOnly["release"].(map[string]any)["status"] != "DRAFT" {
		t.Fatal("simulation promoted a draft", readOnly)
	}
	result = requestJSON(t, server.Client(), "POST", path+"/preview", token, map[string]any{"payload": "00 01 00 00 00 05 01 03 02 00 FA", "startAddress": 0}, 200)
	if result["standardMessage"].(map[string]any)["properties"].(map[string]any)["temperature"] != float64(25) {
		t.Fatal(result)
	}
	requestJSON(t, server.Client(), "POST", path+"/publish", token, map[string]any{}, 200)
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/products", token, map[string]any{"id": "points-product", "name": "点表产品", "protocolPackageId": "points-generated@1.0.0"}, 201)
	excel := upload("point-table", "points.xlsx", protocolAssistantXLSXFixture(t), "MODBUS_TCP", "hex", token, 200)
	if excel.ParserType != parser.ModbusTCPParserName || len(excel.Fields) != 2 {
		t.Fatal(excel)
	}
	unchecked := model.ProtocolAssistantDraft{Name: "CRC mapping", ParserType: "configurable_hex_parser", Transport: "MQTT", PayloadFormat: "hex", Config: map[string]any{"checksum": "crc16", "fields": []any{map[string]any{"name": "value", "offset": 0, "length": 1, "type": "uint8"}}}}
	uncheckedBody := map[string]any{"id": "unchecked-crc", "version": "1.0.0", "draft": unchecked, "payload": "01 02 03"}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/ai/protocol-assistant/publish", token, uncheckedBody, 422)
	delete(uncheckedBody, "payload")
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/ai/protocol-assistant/publish", token, uncheckedBody, 201)
	uncheckedPath := server.URL + "/api/v2/protocols/unchecked-crc/releases/1.0.0"
	requestJSON(t, server.Client(), "POST", uncheckedPath+"/preview", token, map[string]any{"payload": "01 02 03"}, 422)
	requestJSON(t, server.Client(), "POST", uncheckedPath+"/publish", token, map[string]any{}, 422)
	for _, path := range []string{"/api/v2/protocol-catalog", "/api/v2/protocol-market", "/api/v2/market-distribution/tenant/catalog"} {
		req, _ := http.NewRequest("GET", server.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("removed route %s: %d", path, resp.StatusCode)
		}
	}
}

// Uses the separately maintained source package and real child processes,
// HTTP publication, sockets and the normal archive/parser path in one host.
func TestGoProtocolListenerSourceHotSwitch(t *testing.T) {
	if !protocolbuild.Available() {
		t.Skip("Go compiler unavailable")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	root := protocolDataDir(t)
	repo := memory.NewRepository()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	engine.Metrics = metrics.New()
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DataDir = root
	cfg.JWTSecret = "listener-test-secret-at-least-32-characters"
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), log)
	ingested := make(chan model.RawMessage, 32)
	listeners := protocolruntime.NewListeners(repo, root, func(ctx context.Context, raw model.RawMessage) error {
		_, _, err := engine.IngestRaw(ctx, raw)
		if err == nil {
			ingested <- raw
		}
		return err
	}, log)
	api.SetProtocolListeners(listeners)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, _ := api.auth.Issue("tester", "tenant_001", "operator", nil, time.Hour)
	_ = repo.SaveProduct(ctx, model.Product{TenantID: "tenant_001", ID: "gb-product", Name: "GB", Status: "ENABLED"})
	packageRoot := filepath.Join("..", "..", "protocol-packages", "gb26875-dahua")
	fixture, err := os.ReadFile(filepath.Join(packageRoot, "samples", "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var samples []protocolPackageCaseV2
	if err = json.Unmarshal(fixture, &samples); err != nil {
		t.Fatal(err)
	}
	var frameHex string
	_ = json.Unmarshal(samples[0].Input.Payload, &frameHex)
	frame, _ := hex.DecodeString(frameHex)
	upload := func(version string, bad bool, status int) {
		t.Helper()
		var packed bytes.Buffer
		zw := zip.NewWriter(&packed)
		err := filepath.WalkDir(packageRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			name, _ := filepath.Rel(packageRoot, path)
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if name == "protocol.json" {
				data = bytes.ReplaceAll(data, []byte("1.0.0"), []byte(version))
			}
			if bad && filepath.ToSlash(name) == "samples/operations.json" {
				data = []byte(`[]`)
			}
			if version == "1.1.0" && filepath.ToSlash(name) == "gb26875/codec.go" {
				data = bytes.ReplaceAll(data, []byte(`"Dahua"`), []byte(`"Dahua-v2"`))
			}
			f, err := zw.Create(filepath.ToSlash(name))
			if err == nil {
				_, err = f.Write(data)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = zw.Close()
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		f, _ := form.CreateFormFile("file", "gb.zip")
		_, _ = f.Write(packed.Bytes())
		_ = form.WriteField("publish", "true")
		_ = form.Close()
		req, _ := http.NewRequest("POST", server.URL+"/api/v2/protocols/gb26875-dahua/source-releases", &body)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", form.FormDataContentType())
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != status {
			t.Fatalf("upload %s: %d %s", version, resp.StatusCode, data)
		}
	}
	upload("1.0.0", false, 201)
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/products/gb-product/protocol-binding", token, map[string]any{"protocolId": "gb26875-dahua", "version": "1.0.0"}, 200)
	// This test isolates the runtime's frame/ACK/version boundary. The HTTP
	// trial and field-acceptance workflow is exercised by template tests.
	switchVersion := func(version string) {
		t.Helper()
		product, err := repo.GetProduct(ctx, "tenant_001", "gb-product")
		if err != nil {
			t.Fatal(err)
		}
		release, err := repo.GetProtocolRelease(ctx, product.TenantID, "gb26875-dahua", version)
		if err != nil {
			t.Fatal(err)
		}
		previous, err := repo.GetProductProtocolBinding(ctx, product.TenantID, product.ID)
		if err != nil {
			t.Fatal(err)
		}
		pkg := legacyProtocolShim(release)
		product.ProtocolPackageID = pkg.ID
		binding := model.ProductProtocolBinding{TenantID: product.TenantID, ProductID: product.ID, ProtocolID: release.ProtocolID, Version: version, PreviousProtocolID: previous.ProtocolID, PreviousVersion: previous.Version, UpdatedAt: time.Now().UnixMilli()}
		if err = repo.SwitchProductProtocol(ctx, model.ProtocolSwitch{Product: product, Package: pkg, Binding: binding, Expected: &previous}); err != nil {
			t.Fatal(err)
		}
		engine.ProtocolsChanged(product.TenantID)
	}
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := free.Addr().(*net.TCPAddr).Port
	_ = free.Close()
	profile := model.DeviceAccessProfile{ID: "gb-tcp", ProductID: "gb-product", ProtocolID: "gb26875-dahua", ProtocolVersion: "1.0.0", Mode: "listener", Network: "tcp", Host: "127.0.0.1", Port: port, TimeoutMs: 2000, Enabled: true, AutoRegister: true}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/device-access-profiles", token, profile, 201)
	udpFree, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	udpPort := udpFree.LocalAddr().(*net.UDPAddr).Port
	_ = udpFree.Close()
	udpProfile := profile
	udpProfile.ID = "gb-udp"
	udpProfile.Network = "udp"
	udpProfile.Port = udpPort
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/device-access-profiles", token, udpProfile, 201)
	listeners.Start(ctx)
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	var conn net.Conn
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		conn, err = net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Add a managed device through the unified service while reusing the live
	// listener. The existing runtime below must accept it without re-registration.
	onboardRequest := onboarding.EnrollRequest{Trial: true, RequestID: "gb-reuse", ProductID: "gb-product", Device: onboarding.EnrollDevice{ID: "gb26875_123456789012", Name: "GB onboarded"}, Connection: onboarding.EnrollConnection{Mode: onboarding.ModeListener, ProfileID: profile.ID}}
	if _, err = api.onboarding.Enroll(ctx, "tenant_001", onboardRequest); err != nil {
		t.Fatal("reuse listener onboarding", err)
	}
	profilesAfter, _ := repo.ListDeviceAccessProfiles(ctx, "tenant_001")
	if len(profilesAfter) != 2 {
		t.Fatal("onboarding duplicated the existing listener")
	}
	readFrame := func(c net.Conn) []byte {
		t.Helper()
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		head := make([]byte, 27)
		if _, err := io.ReadFull(c, head); err != nil {
			t.Fatal(err)
		}
		length := int(head[24]) + int(head[25])*256
		tail := make([]byte, length+3)
		if _, err := io.ReadFull(c, tail); err != nil {
			t.Fatal(err)
		}
		return append(head, tail...)
	}
	check := func(version, vendor string) {
		t.Helper()
		select {
		case raw := <-ingested:
			if raw.ProtocolVersion != version || raw.DeviceID != "gb26875_123456789012" {
				t.Fatalf("raw %+v", raw)
			}
			msg, err := repo.GetStandardMessageByRaw(ctx, "tenant_001", raw.MessageID)
			if err != nil || msg.Tags["terminalVendor"] != vendor {
				t.Fatalf("parsed %+v %v", msg, err)
			}
			stored, err := repo.GetRawIndex(ctx, "tenant_001", raw.MessageID)
			if err != nil {
				t.Fatal(err)
			}
			archived, err := engine.GetRaw(ctx, stored)
			if err != nil || archived.ProtocolVersion != version {
				t.Fatalf("archive %+v %v", archived, err)
			}
			if raw.Metadata["protocolState"] != nil && archived.Metadata["protocolState"] == nil {
				t.Fatal("session state was not archived for replay")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no ingested frame")
		}
	}
	_, _ = conn.Write(frame[:12])
	time.Sleep(50 * time.Millisecond)
	select {
	case <-ingested:
		t.Fatal("partial frame was ingested")
	default:
	}
	_, _ = conn.Write(append(append([]byte{}, frame[12:]...), frame...))
	for i := 0; i < 2; i++ {
		ack := readFrame(conn)
		if ack[26] != 3 {
			t.Fatalf("not ACK: %X", ack)
		}
		check("1.0.0", "Dahua")
	}
	// A real command must wait for a matching reply, without acknowledging ACKs.
	commandDone := make(chan error, 1)
	go func() {
		result, err := listeners.Command(ctx, "tenant_001", "gb-tcp", "gb26875_123456789012", map[string]any{"type": "time-sync"})
		if err == nil && result["status"] != "acknowledged" {
			err = io.ErrUnexpectedEOF
		}
		commandDone <- err
	}()
	command := readFrame(conn)
	ack := append([]byte{}, command[:27]...)
	copy(ack[12:18], frame[12:18])
	copy(ack[18:24], frame[18:24])
	ack[24], ack[25], ack[26] = 0, 0, 3
	var sum byte
	for _, b := range ack[2:] {
		sum += b
	}
	ack = append(ack, sum, '#', '#')
	_, _ = conn.Write(ack)
	check("1.0.0", "Dahua")
	if err = <-commandDone; err != nil {
		t.Fatal(err)
	}
	udp, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(udpPort)))
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	_, _ = udp.Write(frame)
	_ = udp.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1024)
	if n, err := udp.Read(buf); err != nil || n != 30 || buf[26] != 3 {
		t.Fatalf("UDP ACK %d %v", n, err)
	}
	check("1.0.0", "Dahua")
	upload("1.1.0", false, 201)
	switchVersion("1.1.0")
	_, _ = conn.Write(frame)
	_ = readFrame(conn)
	check("1.1.0", "Dahua-v2")
	upload("1.2.0", true, 422)
	_, _ = conn.Write(frame)
	_ = readFrame(conn)
	check("1.1.0", "Dahua-v2")
	switchVersion("1.0.0")
	_, _ = conn.Write(frame)
	_ = readFrame(conn)
	check("1.0.0", "Dahua")
	// A corrupt frame receives no ACK and cannot enter the archive.
	_, _ = conn.Write(append([]byte("noise"), frame...))
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if n, err := conn.Read(buf); n != 0 || err == nil || strings.Contains(err.Error(), "timeout") {
		t.Fatalf("invalid frame not rejected: %d %v", n, err)
	}
	select {
	case <-ingested:
		t.Fatal("corrupt frame ingested")
	default:
	}
}

type protocolDownloadFixtureV2 struct {
	api    *Server
	server *httptest.Server
	repo   *memory.Repository
	root   string
	token  string
}

type failedProtocolSwitchRepository struct{ ports.Repository }

func (failedProtocolSwitchRepository) SwitchProductProtocol(context.Context, model.ProtocolSwitch) error {
	return model.ErrBindingChanged
}

func TestSourcePublicationReportsLateBindingFailureWithoutLosingRelease(t *testing.T) {
	if !protocolbuild.Available() {
		t.Skip("Go compiler unavailable")
	}
	f := newProtocolDownloadFixtureV2(t)
	f.api.engine.Repo = failedProtocolSwitchRepository{Repository: f.api.engine.Repo}
	if err := f.repo.SaveProduct(context.Background(), model.Product{TenantID: "tenant_001", ID: "product", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, _ := form.CreateFormFile("file", "protocol.go")
	_, _ = file.Write([]byte(protocolbuild.FunctionTemplate))
	for key, value := range map[string]string{"version": "1.0.0", "publish": "true", "productId": "product"} {
		_ = form.WriteField(key, value)
	}
	_ = form.Close()
	req, _ := http.NewRequest("POST", f.server.URL+"/api/v2/protocols/vendor-fire/source-releases", &body)
	req.Header.Set("Authorization", "Bearer "+f.token)
	req.Header.Set("Content-Type", form.FormDataContentType())
	response, err := f.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result map[string]any
	if err = json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 201 || result["binding"] != nil || result["bindingWarning"] == "" {
		t.Fatalf("partial success hidden: status=%d result=%v", response.StatusCode, result)
	}
	steps := result["stepResults"].([]any)
	if len(steps) != 2 || steps[0].(map[string]any)["status"] != "SUCCEEDED" || steps[1].(map[string]any)["status"] != "FAILED" {
		t.Fatal(steps)
	}
	release, err := f.repo.GetProtocolRelease(context.Background(), "tenant_001", "vendor-fire", "1.0.0")
	if err != nil || release.Status != "PUBLISHED" {
		t.Fatal("published source was lost", release, err)
	}
	if _, err = os.Stat(filepath.Join(f.root, release.Artifact["packagePath"].(string))); err != nil {
		t.Fatal("source artifact was removed", err)
	}
	if _, err = f.repo.GetProductProtocolBinding(context.Background(), "tenant_001", "product"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("failed binding persisted", err)
	}
}

func newProtocolDownloadFixtureV2(t *testing.T) protocolDownloadFixtureV2 {
	t.Helper()
	root := t.TempDir()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(), log)
	engine.Metrics = metrics.New()
	cfg := config.Load()
	cfg.DataDir = root
	cfg.JWTSecret = "protocol-download-test-secret-at-least-32-characters"
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), log)
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	token, err := api.auth.Issue("protocol-developer", "tenant_001", "operator", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return protocolDownloadFixtureV2{api: api, server: server, repo: repo, root: root, token: token}
}

func protocolDownloadZipV2(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var body bytes.Buffer
	writer := zip.NewWriter(&body)
	for name, content := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = entry.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes()
}

func (f protocolDownloadFixtureV2) save(t *testing.T, version string, entries map[string][]byte, mutate func(map[string]any)) ([]byte, string) {
	t.Helper()
	data := protocolDownloadZipV2(t, entries)
	relative := filepath.Join("protocol-releases", "tenant_001", "vendor-fire", version, "package.zip")
	filename := filepath.Join(f.root, relative)
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, data, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	artifact := map[string]any{"packagePath": filepath.ToSlash(relative), "packageSha256": hex.EncodeToString(digest[:])}
	if mutate != nil {
		mutate(artifact)
	}
	if err := f.repo.CreateProtocolRelease(context.Background(), model.ProtocolRelease{TenantID: "tenant_001", ProtocolID: "vendor-fire", Version: version, ParserType: parser.GoProtocolParserName, Status: "PUBLISHED", Artifact: artifact}); err != nil {
		t.Fatal(err)
	}
	return data, filename
}

func (f protocolDownloadFixtureV2) get(t *testing.T, token, version, kind string, status int) ([]byte, http.Header) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, f.server.URL+"/api/v2/protocols/vendor-fire/releases/"+version+"/"+kind, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := f.server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status {
		t.Fatalf("%s %s status=%d want=%d body=%s", version, kind, response.StatusCode, status, body)
	}
	return body, response.Header
}

func TestProtocolReleaseDownloadsPreserveOriginalSourceAndPackage(t *testing.T) {
	f := newProtocolDownloadFixtureV2(t)
	project := protocolDownloadZipV2(t, map[string][]byte{
		"go.mod":             []byte("module example.com/vendor-fire\n\ngo 1.25.0\n"),
		"main.go":            []byte(protocolbuild.Template),
		"samples/cases.json": []byte(protocolSourceCases),
	})
	for _, test := range []struct {
		name, version, extension, contentType string
		source                                []byte
	}{
		{name: "single-file", version: "1.0.0", extension: ".go", contentType: "text/plain; charset=utf-8", source: []byte(protocolbuild.Template)},
		{name: "standalone-project", version: "1.1.0", extension: ".zip", contentType: "application/zip", source: project},
	} {
		t.Run(test.name, func(t *testing.T) {
			digest := sha256.Sum256(test.source)
			archive, _ := f.save(t, test.version, map[string][]byte{"manifest.yaml": []byte("schemaVersion: 1\n"), "bin/worker": []byte("worker"), "source/upload" + test.extension: test.source}, func(artifact map[string]any) {
				artifact["build"] = map[string]any{"kind": "go-source", "sourceSha256": hex.EncodeToString(digest[:])}
			})
			body, headers := f.get(t, f.token, test.version, "source", 200)
			if !bytes.Equal(body, test.source) {
				t.Fatal("download changed the uploaded source bytes")
			}
			if headers.Get("Content-Type") != test.contentType || !strings.Contains(headers.Get("Content-Disposition"), "vendor-fire-"+test.version+"-source"+test.extension) || headers.Get("X-Content-SHA256") != hex.EncodeToString(digest[:]) || headers.Get("Cache-Control") != "no-store" {
				t.Fatalf("unexpected source download headers: %v", headers)
			}
			files, err := protocolbuild.Sources("download"+test.extension, body)
			if err != nil || !bytes.Equal(files["main.go"], []byte(protocolbuild.Template)) {
				t.Fatalf("download cannot be uploaded again as Go source: %v", err)
			}
			body, headers = f.get(t, f.token, test.version, "package", 200)
			if !bytes.Equal(body, archive) || headers.Get("Content-Type") != "application/zip" || !strings.Contains(headers.Get("Content-Disposition"), "-package.zip") {
				t.Fatal("package download did not preserve the complete immutable artifact")
			}
		})
	}
	// A compiled-only release remains exportable, with a clear source error.
	archive, _ := f.save(t, "2.0.0", map[string][]byte{"bin/worker": []byte("worker")}, nil)
	body, _ := f.get(t, f.token, "2.0.0", "source", 404)
	if !strings.Contains(string(body), "未保存原始 Go 源码") {
		t.Fatalf("missing-source explanation: %s", body)
	}
	body, _ = f.get(t, f.token, "2.0.0", "package", 200)
	if !bytes.Equal(body, archive) {
		t.Fatal("compiled-only package download differs")
	}
}

func TestProtocolReleaseDownloadsEnforceAuthorizationAndTenant(t *testing.T) {
	f := newProtocolDownloadFixtureV2(t)
	f.save(t, "1.0.0", map[string][]byte{"source/upload.go": []byte(protocolbuild.Template)}, nil)
	viewer, _ := f.api.auth.Issue("reader", "tenant_001", "viewer", nil, time.Hour)
	otherTenant, _ := f.api.auth.Issue("other", "tenant_002", "operator", nil, time.Hour)
	for _, kind := range []string{"source", "package"} {
		f.get(t, "", "1.0.0", kind, 401)
		f.get(t, viewer, "1.0.0", kind, 403)
		f.get(t, otherTenant, "1.0.0", kind, 404)
		f.get(t, f.token, "9.9.9", kind, 404)
	}
}

func TestProtocolReleaseDownloadsRejectWrongPathsAndCorruption(t *testing.T) {
	f := newProtocolDownloadFixtureV2(t)
	for index, wrongPath := range []string{
		"../outside.zip",
		filepath.Join(f.root, "outside.zip"),
		"protocol-releases/tenant_002/vendor-fire/1.0.0/package.zip",
		"protocol-releases/tenant_001/vendor-fire/1.0.0/package.zip",
	} {
		version := []string{"2.0.0", "2.1.0", "2.2.0", "2.3.0"}[index]
		f.save(t, version, map[string][]byte{"source/upload.go": []byte(protocolbuild.Template)}, func(artifact map[string]any) { artifact["packagePath"] = wrongPath })
		for _, kind := range []string{"source", "package"} {
			f.get(t, f.token, version, kind, 409)
		}
	}
	_, filename := f.save(t, "3.0.0", map[string][]byte{"source/upload.go": []byte(protocolbuild.Template)}, nil)
	if err := os.WriteFile(filename, []byte("tampered artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"source", "package"} {
		body, _ := f.get(t, f.token, "3.0.0", kind, 409)
		if !strings.Contains(string(body), "SHA-256") || strings.Contains(string(body), "tampered artifact") {
			t.Fatalf("corrupt artifact response: %s", body)
		}
	}
	f.save(t, "3.1.0", map[string][]byte{"source/upload.go": []byte(protocolbuild.Template)}, func(artifact map[string]any) {
		artifact["build"] = map[string]any{"sourceSha256": strings.Repeat("0", 64)}
	})
	f.get(t, f.token, "3.1.0", "source", 409)
	f.save(t, "3.2.0", map[string][]byte{"source/upload.go": []byte(protocolbuild.Template), "source/upload.zip": []byte("ambiguous")}, nil)
	f.get(t, f.token, "3.2.0", "source", 409)
}
