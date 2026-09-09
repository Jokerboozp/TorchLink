package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/protocolruntime"
)

func TestEdgeListenerHeartbeatProjectsActualObservation(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	root := t.TempDir()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	cfg := config.Load()
	cfg.DataDir = root
	api := New(cfg, engine, metrics.New(), log)
	api.SetProtocolListeners(protocolruntime.NewListeners(repo, root, nil, log))
	p := model.DeviceAccessProfile{TenantID: "tenant", ID: "profile", ProductID: "product", Mode: "listener", EdgeNodeID: "edge", Host: "127.0.0.1", Port: 20000, Enabled: true, RuntimeStatus: "PENDING"}
	for _, err := range []error{repo.SaveEdgeNode(ctx, model.EdgeNode{TenantID: "tenant", ID: "edge", Status: "ENABLED"}), repo.SetEdgeCredential(ctx, "tenant", "edge", onboarding.Hash("test-secret")), repo.SaveDeviceAccessProfile(ctx, p)} {
		if err != nil {
			t.Fatal(err)
		}
	}
	post := func(observed model.DeviceAccessProfile, secret string, want int) {
		t.Helper()
		data, _ := json.Marshal(model.EdgeHeartbeat{Profiles: []model.DeviceAccessProfile{observed}})
		req := httptest.NewRequest("POST", "/api/v1/edge/tenant/edge/heartbeat", bytes.NewReader(data))
		req.Header.Set("X-Edge-Secret", secret)
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, req)
		if w.Code != want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	p.RuntimeStatus = "LISTENING"
	post(p, "wrong", 401)
	post(p, "test-secret", 200)
	actual, _ := repo.GetDeviceAccessProfile(ctx, "tenant", "profile")
	if actual.RuntimeStatus != "LISTENING" || actual.LastSuccessAt != 0 || actual.LastErrorAt != 0 {
		t.Fatal("listening fabricated data/error time", actual)
	}
	p.LastSuccessAt, p.LastErrorAt = 100, 200
	post(p, "test-secret", 200)
	actual, _ = repo.GetDeviceAccessProfile(ctx, "tenant", "profile")
	if actual.LastSuccessAt != 100 || actual.LastErrorAt != 0 {
		t.Fatal("error timestamp was used for successful data", actual)
	}
	p.RuntimeStatus = "ERROR"
	p.LastSuccessAt, p.LastErrorAt = 300, 250
	post(p, "test-secret", 200)
	actual, _ = repo.GetDeviceAccessProfile(ctx, "tenant", "profile")
	if actual.LastSuccessAt != 100 || actual.LastErrorAt != 250 {
		t.Fatal("error projection corrupted success", actual)
	}
	p.RuntimeStatus = "LISTENING"
	p.LastSuccessAt = 0
	post(p, "test-secret", 200)
	actual, _ = repo.GetDeviceAccessProfile(ctx, "tenant", "profile")
	if actual.LastSuccessAt != 100 || actual.LastErrorAt != 250 {
		t.Fatal("idle listener erased history", actual)
	}
	observed, sessions := api.profileSnapshot(ctx, "tenant", actual)
	if observed.LastSuccessAt != 100 || observed.LastErrorAt != 250 || len(sessions) != 0 || observed.RuntimeStatus != "LISTENING" {
		t.Fatalf("center listener erased edge diagnostic evidence: %+v", observed)
	}
	actual.Enabled = false
	if err := repo.SaveDeviceAccessProfile(ctx, actual); err != nil {
		t.Fatal(err)
	}
	p.LastSuccessAt = 999
	post(p, "test-secret", 200)
	current, _ := repo.GetDeviceAccessProfile(ctx, "tenant", "profile")
	if current != actual {
		t.Fatal("stale node observation overwrote disabled configuration")
	}
}
