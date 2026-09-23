package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
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
)

func TestRegisterConfiguredChildUsesStableParentAddress(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
	cfg := config.Load()
	cfg.JWTSecret = "child-register-test-signing-key-32-characters"
	cfg.AdminTenants = []string{"tenant"}
	srv := New(cfg, engine, metrics.New(), log)
	token, _ := srv.auth.Issue("tester", "tenant", "admin", nil, time.Hour)
	for _, p := range []model.Product{{TenantID: "tenant", ID: "parent-product", Status: "ENABLED"}, {TenantID: "tenant", ID: "child-product", Status: "ENABLED"}} {
		if err := repo.SaveProduct(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "tenant", ProtocolID: "child-protocol", Version: "1", Status: "PUBLISHED"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "tenant", ProductID: "child-product", ProtocolID: "child-protocol", Version: "1"}); err != nil {
		t.Fatal(err)
	}
	profile := model.DeviceAccessProfile{TenantID: "tenant", ID: "listener", ProductID: "parent-product", Mode: "listener", Network: "tcp", Enabled: true, ChildProducts: []model.ChildProductBinding{{Type: "sensor", ProductID: "child-product"}}}
	if err := repo.SaveDeviceAccessProfile(ctx, profile); err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ID: "parent", ProductID: "parent-product", Name: "主设备", Status: "ENABLED", DeviceRole: "GATEWAY", Tags: map[string]string{"connectorProfileId": "listener"}}); err != nil {
		t.Fatal(err)
	}
	call := func(body string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/v1/device-registry/parent/children", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, r)
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return w.Code, result
	}
	code, first := call(`{"address":"1-7","type":"sensor","name":"一层探测器"}`)
	if code != 201 {
		t.Fatalf("first registration: %d %+v", code, first)
	}
	id := first["device"].(map[string]any)["id"]
	if id != model.ChildDeviceID("tenant", "parent", "1-7") {
		t.Fatalf("unstable child ID: %v", id)
	}
	code, again := call(`{"address":"1-7","type":"sensor","name":"一层探测器"}`)
	if code != 200 || again["device"].(map[string]any)["id"] != id {
		t.Fatalf("duplicate child: %d %+v", code, again)
	}
	if code, _ := call(`{"address":"1-7","type":"unknown"}`); code != 422 {
		t.Fatalf("unmapped child accepted: %d", code)
	}
	child, err := repo.GetManagedDevice(ctx, "tenant", id.(string))
	if err != nil || child.GatewayID != "parent" || child.Tags["connectorProfileId"] != "listener" {
		t.Fatalf("child relation: %+v %v", child, err)
	}
	if err := repo.SaveDeviceAccessProfile(ctx, model.DeviceAccessProfile{TenantID: "tenant", ID: "foreign", ProductID: "child-product", Mode: "listener", Network: "tcp", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/v1/device-registry", strings.NewReader(`{"id":"wrong","productId":"parent-product","name":"错误关联","tags":{"connectorProfileId":"foreign"}}`))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	if w.Code != 422 {
		t.Fatalf("cross-product connection accepted: %d %s", w.Code, w.Body.String())
	}
}
