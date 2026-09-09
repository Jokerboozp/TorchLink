package httpapi

import (
	"context"
	"io"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExecutionRouteUsesTenantLeaseAndPreservesAuth(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
	cfg := config.Load()
	cfg.ProcessRole = "gateway"
	cfg.AccessCoordination = true
	cfg.AccessNodeURL = "http://local"
	cfg.JWTSecret = "routing-shared-test-secret"
	server := New(cfg, engine, metrics.New(), log)
	token, err := server.auth.Issue("operator", "tenant", "operator", nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var received bool
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("X-Iot-Gateway-Hops") != "1" {
			t.Error("forward lost auth or routing bound")
		}
		received = true
		w.WriteHeader(202)
	}))
	defer remote.Close()
	if _, ok, err := repo.AcquireExecutionLease(ctx, "tenant", "profile/profile", "remote", remote.URL, time.Minute); err != nil || !ok {
		t.Fatal(err)
	}
	if err := repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant", ID: "device", Tags: map[string]string{"connectorProfileId": "profile"}}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/v1/device-registry/device/commands", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	if w.Code != 202 || !received {
		t.Fatal("did not reach owning runtime", w.Code)
	}
	other, err := server.auth.Issue("operator", "other-tenant", "operator", nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+other)
	received = false
	if target := server.executionTarget(r); target != "" {
		t.Fatal("cross tenant routed", target)
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-Iot-Gateway-Hops", "2")
	w = httptest.NewRecorder()
	server.Handler().ServeHTTP(w, r)
	if w.Code != 503 || received {
		t.Fatal("route loop not bounded", w.Code)
	}
}
