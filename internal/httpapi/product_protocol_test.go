package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

func TestIndependentProductAndDeviceRegistration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
	if err := engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.JWTSecret = "independent-product-test-key-32-chars"
	api := New(cfg, engine, metrics.New(), log)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, _ := api.auth.Issue("tester", "tenant", "admin", nil, time.Hour)
	viewer, _ := api.auth.Issue("viewer", "tenant", "viewer", nil, time.Hour)
	product := map[string]any{"id": "standard-product", "name": "独立标准产品", "protocolPackageId": "iot-standard@1.0.0", "transport": "HTTP"}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/products", viewer, product, 403)
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/products", token, product, 201)
	release, err := repo.GetProtocolRelease(ctx, "tenant", parser.StandardProtocolID, "1.0.0")
	if err != nil || release.ParserType != parser.StandardParserName {
		t.Fatalf("missing standard release: %+v %v", release, err)
	}
	if _, err := repo.GetProtocolRelease(ctx, "other", parser.StandardProtocolID, "1.0.0"); err == nil {
		t.Fatal("release crossed tenant boundary")
	}
	result := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/device-registry", token, map[string]any{"id": "independent-device", "name": "独立设备", "productId": "standard-product"}, 201)
	credential, ok := result["credential"].(map[string]any)
	if !ok || credential["secret"] == "" {
		t.Fatal("device registration did not issue credential")
	}
	// Product creation is repeatable and never rewrites the immutable release.
	requestJSON(t, server.Client(), "PUT", server.URL+"/api/v1/products/standard-product", token, product, 201)
	again, _ := repo.GetProtocolRelease(ctx, "tenant", parser.StandardProtocolID, "1.0.0")
	if again.CreatedAt != release.CreatedAt {
		t.Fatal("standard release was replaced")
	}
	raw, err := api.onboarding.PrepareStandard(ctx, "tenant", "standard-product", "independent-device", "property", "HTTP", []byte(`{"version":"1.0","id":"first","timestamp":1789315000000,"data":{"temperature":26}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.IngestRaw(ctx, raw); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		state, err := repo.GetDeviceState(ctx, "tenant", "independent-device")
		if err == nil && state.LastSeenAt > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("first report was not parsed: %+v %v", state, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// A published Go release is selectable before a compatibility package exists.
	goRelease := model.ProtocolRelease{TenantID: "tenant", ProtocolID: "fire-go", Version: "1.0.0", Status: "PUBLISHED", ParserType: parser.GoProtocolParserName, Transport: "TCP", PayloadFormat: "hex"}
	if err := repo.CreateProtocolRelease(ctx, goRelease); err != nil {
		t.Fatal(err)
	}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/products", token, map[string]any{"id": "go-product", "name": "Go 产品", "protocolPackageId": "fire-go@1.0.0"}, 201)
	binding, err := repo.GetProductProtocolBinding(ctx, "tenant", "go-product")
	if err != nil || binding.ProtocolID != "fire-go" {
		t.Fatalf("new product is not bound: %+v %v", binding, err)
	}
	for _, status := range []string{"VALIDATED", "REVOKED"} {
		blocked := goRelease
		blocked.Version = status
		blocked.Status = status
		if err := repo.CreateProtocolRelease(ctx, blocked); err != nil {
			t.Fatal(err)
		}
		requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/products", token, map[string]any{"id": status, "name": "不可绑定", "protocolPackageId": "fire-go@" + status}, 422)
		if _, err := repo.GetProduct(ctx, "tenant", status); err == nil {
			t.Fatal("invalid release created a product")
		}
	}
}

func TestNewTemplateOnVersionedReleaseIsBound(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Load()
	cfg.JWTSecret = "versioned-template-binding-test-key-32"
	api := New(cfg, &core.Engine{Repo: repo}, metrics.New(), log)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, _ := api.auth.Issue("tester", "tenant", "admin", nil, time.Hour)
	if err := repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "tenant", ProtocolID: "meter", Version: "1", Transport: "MODBUS_TCP", PayloadFormat: "hex", ParserType: parser.ModbusTCPParserName, Status: "PUBLISHED"}); err != nil {
		t.Fatal(err)
	}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/products", token, map[string]any{"id": "meter-product", "name": "电表", "protocolPackageId": "meter@1"}, 201)
	binding, err := repo.GetProductProtocolBinding(ctx, "tenant", "meter-product")
	if err != nil || binding.ProtocolID != "meter" || binding.Version != "1" {
		t.Fatalf("versioned template not bound: %+v %v", binding, err)
	}
	requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/products", token, map[string]any{"id": "standard-product", "name": "标准", "protocolPackageId": "iot-standard@1.0.0"}, 201)
	if _, err = repo.GetProductProtocolBinding(ctx, "tenant", "standard-product"); err == nil {
		t.Fatal("the built-in standard protocol needs no binding")
	}
}
