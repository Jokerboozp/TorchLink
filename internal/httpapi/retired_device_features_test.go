package httpapi

import (
	"context"
	"io"
	"log/slog"
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

func TestRetiredDeviceFeaturesHaveNoRoutes(t *testing.T) {
	repo := memory.NewRepository()
	root := t.TempDir()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Load()
	cfg.DataDir = root
	cfg.JWTSecret = "retired-feature-test-key-32-characters"
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	api := New(cfg, engine, metrics.New(), log)
	if err = repo.SaveManagedDevice(context.Background(), model.ManagedDevice{TenantID: "tenant", ID: "device", ProductID: "product", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	token, _ := api.auth.Issue("tester", "tenant", "admin", nil, time.Minute)
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/v1/device-twins/device"}, {"PATCH", "/api/v1/device-twin-topology"},
		{"GET", "/api/v1/device-registry/device/shadow"}, {"PATCH", "/api/v1/device-registry/device/shadow"},
		{"GET", "/api/v1/device-registry/device/shadow/history"}, {"GET", "/api/v1/device-registry/device/shadows"}, {"GET", "/api/v1/device-shadow"},
	} {
		req := httptest.NewRequest(route.method, route.path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, req)
		if w.Code != 404 {
			t.Fatalf("%s %s: got %d, want 404", route.method, route.path, w.Code)
		}
	}
	req := httptest.NewRequest("GET", "/api/v1/device-registry/device/connection", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("device detail was removed with advanced features: %d", w.Code)
	}
}
