package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type countingEventRepo struct {
	*memory.Repository
	alarmReads, stateReads atomic.Int32
}

func (r *countingEventRepo) ListAlarms(ctx context.Context, f ports.AlarmFilter) ([]model.Alarm, error) {
	r.alarmReads.Add(1)
	return r.Repository.ListAlarms(ctx, f)
}

func (r *countingEventRepo) ListDeviceStates(ctx context.Context, tenant string) ([]model.DeviceState, error) {
	r.stateReads.Add(1)
	return r.Repository.ListDeviceStates(ctx, tenant)
}

// Concurrent polls of one tenant share a single read, and the snapshot is read
// again once it expires.
func TestEventSnapshotIsSharedPerTenantWindow(t *testing.T) {
	repo := &countingEventRepo{Repository: memory.NewRepository()}
	cache := newEventSnapshots()
	now := time.Now()
	cache.now = func() time.Time { return now }
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := cache.get(context.Background(), repo, "t1"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if repo.alarmReads.Load() != 1 || repo.stateReads.Load() != 1 {
		t.Fatalf("20 polls read alarms %d and states %d times, want once", repo.alarmReads.Load(), repo.stateReads.Load())
	}
	if _, _, err := cache.get(context.Background(), repo, "t2"); err != nil || repo.stateReads.Load() != 2 {
		t.Fatalf("tenants must not share snapshots: reads=%d err=%v", repo.stateReads.Load(), err)
	}
	now = now.Add(eventSnapshotTTL)
	if _, _, err := cache.get(context.Background(), repo, "t1"); err != nil || repo.stateReads.Load() != 3 {
		t.Fatalf("expired snapshot must be reloaded: reads=%d err=%v", repo.stateReads.Load(), err)
	}
}

// An unchanged event view answers 304 to the ETag the page already holds.
func TestUserEventsAnswerNotModifiedForSameView(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	if err := repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "tenant-a", DeviceID: "device-a", BusinessStatus: "ONLINE"}); err != nil {
		t.Fatal(err)
	}
	engine := &core.Engine{Repo: repo, Clock: ports.RealClock{}, Bus: local.NewBus(), Realtime: local.NewRealtime()}
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword = "root", "events-root-password"
	cfg.AdminTenants = []string{"tenant-a"}
	cfg.JWTSecret = "user-events-etag-secret-at-least-32-bytes"
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()
	token := requestJSON(t, srv.Client(), "POST", srv.URL+"/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "tenant-a"}, 200)["accessToken"].(string)

	poll := func(etag string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+"/api/v1/events", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp
	}
	first := poll("")
	tag := first.Header.Get("ETag")
	if first.StatusCode != 200 || tag == "" {
		t.Fatalf("first poll status=%d etag=%q", first.StatusCode, tag)
	}
	if resp := poll(tag); resp.StatusCode != http.StatusNotModified {
		t.Fatalf("unchanged view must answer 304, got %d", resp.StatusCode)
	}
	if resp := poll(`"stale"`); resp.StatusCode != 200 {
		t.Fatalf("a different ETag must receive the full view, got %d", resp.StatusCode)
	}
}
