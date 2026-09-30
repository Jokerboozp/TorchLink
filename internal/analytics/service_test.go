package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"iot-platform/internal/config"
	"iot-platform/internal/model"
)

func serviceFixture() (*Service, *Actor, *sync.Mutex) {
	a := &Actor{TenantID: "t", Username: "user", Managed: true, SessionVersion: 1, AccessVersion: "v1", DeviceIDs: []string{"a", "b"}, Permissions: []string{"menu:devices", "menu:dataQuality", "POST /api/v1/data-quality/runs", "POST /api/v1/data-quality/runs/:id/stop"}}
	mu := &sync.Mutex{}
	s := NewService(NewMemoryStore(), config.AnalyticsConfig{Lease: 900 * time.Millisecond, Poll: 10 * time.Millisecond, RunTimeout: time.Second}, func(_ context.Context, identity Actor) (Actor, error) {
		mu.Lock()
		defer mu.Unlock()
		if identity.TenantID != a.TenantID || identity.Username != a.Username || identity.SessionVersion != a.SessionVersion {
			return Actor{}, ErrForbidden
		}
		return *a, nil
	}, func(_ context.Context, tenant, id string) error {
		if tenant == "t" && (id == "a" || id == "b") {
			return nil
		}
		return model.ErrNotFound
	})
	_ = s.Register(KindDataQuality, func(context.Context, *Execution) error { return nil })
	return s, a, mu
}

func serviceRequest(key string, ids ...string) CreateRequest {
	return CreateRequest{DeviceIDs: ids, Start: 1000, End: 10000, ConfigurationVersion: "profile/v1", IdempotencyKey: key}
}

func TestReclaimedExpiredRunUsesPartialHandlerAfterCurrentAuthorization(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "authorized", true: "revoked"}[revoke], func(t *testing.T) {
			s, a, mu := serviceFixture()
			old := time.Now().Add(-time.Hour)
			s.Store = NewMemoryStoreWithClock(func() time.Time { return old })
			called := false
			_ = s.Register(KindDataQuality, func(ctx context.Context, e *Execution) error {
				called = true
				if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
					t.Fatal("reclaimed run gained a new time budget")
				}
				return e.Commit(context.WithoutCancel(ctx), model.AnalysisBatch{ID: "partial-time-limit", Status: model.AnalysisPartial, Stage: "time-limit", Processed: e.Run.Processed, Snapshot: &model.AnalysisSnapshot{ID: e.Run.ID + "/partial", DataCutoff: old.UnixMilli(), InputHashes: []string{"fixed"}, Statistics: json.RawMessage(`{"incomplete":true}`), Limitations: []string{"RUN_TIME_BUDGET_EXHAUSTED"}}})
			})
			r, err := s.Create(context.Background(), *a, KindDataQuality, "v1", serviceRequest("expired", "a"))
			if err != nil {
				t.Fatal(err)
			}
			r, err = s.Store.ClaimAnalysisRun(context.Background(), "new-replica", time.Second, []string{KindDataQuality})
			if err != nil {
				t.Fatal(err)
			}
			if revoke {
				mu.Lock()
				a.AccessVersion = "v2"
				a.DeviceIDs = nil
				mu.Unlock()
			}
			s.execute(context.Background(), r, slog.New(slog.NewTextHandler(io.Discard, nil)))
			saved, err := s.Store.GetAnalysisRun(context.Background(), r.TenantID, r.ID)
			if err != nil {
				t.Fatal(err)
			}
			if revoke {
				if called || !TerminalAnalysisStatus(saved.Status) || saved.Status == model.AnalysisPartial {
					t.Fatalf("revoked worker ran timeout facts: %+v", saved)
				}
			} else if !called || saved.Status != model.AnalysisPartial || saved.SnapshotID == "" {
				t.Fatalf("partial handler bypassed on reclaim: %+v", saved)
			}
		})
	}
}

func TestServiceScopeIdempotencyAndWholeArtifactRevocation(t *testing.T) {
	s, a, mu := serviceFixture()
	ctx := context.Background()
	r, err := s.Create(ctx, *a, KindDataQuality, "v1", serviceRequest("same", "b", "a", "a"))
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := s.Create(ctx, *a, KindDataQuality, "v1", serviceRequest("same", "a", "b"))
	if err != nil || repeat.ID != r.ID {
		t.Fatalf("same explicit set must reuse: %v %v", repeat, err)
	}
	if _, err = s.Create(ctx, *a, KindDataQuality, "v1", serviceRequest("same", "a")); !errors.Is(err, model.ErrAnalysisConflict) {
		t.Fatal("changed request reused key", err)
	}
	if _, err = s.Create(ctx, *a, KindDataQuality, "v1", serviceRequest("none")); !errors.Is(err, model.ErrAnalysisInvalid) {
		t.Fatal("empty scope", err)
	}
	if _, err = s.Create(ctx, *a, KindDataQuality, "v1", serviceRequest("hidden", "hidden")); !errors.Is(err, ErrForbidden) {
		t.Fatal("scope expanded", err)
	}
	mu.Lock()
	a.DeviceIDs = []string{"a"}
	a.AccessVersion = "v2"
	mu.Unlock()
	if _, err = s.Get(ctx, *a, KindDataQuality, r.ID); !errors.Is(err, ErrForbidden) {
		t.Fatal("whole artifact readable after shrink", err)
	}
	if _, _, err = s.Outputs(ctx, *a, KindDataQuality, r.ID, model.AnalysisFilter{}); !errors.Is(err, ErrForbidden) {
		t.Fatal("outputs readable after shrink", err)
	}
	items, total, err := s.List(ctx, *a, KindDataQuality, model.AnalysisFilter{})
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("list exposed hidden scope or count %v %d %v", items, total, err)
	}
	other := *a
	other.TenantID = "other"
	if _, err = s.Get(ctx, other, KindDataQuality, r.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("cross-tenant task", err)
	}
}

func TestServiceWorkerActualOutputAndFrozenSnapshot(t *testing.T) {
	s, a, _ := serviceFixture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = s.Register(KindDataQuality, func(ctx context.Context, e *Execution) error {
		return e.Commit(ctx, model.AnalysisBatch{ID: "final", Status: model.AnalysisSucceeded, Stage: "COMPLETE", Processed: 3, Checkpoint: json.RawMessage(`{"done":true}`), Outputs: []model.AnalysisOutput{{ID: "metric", Kind: "metrics", DeviceID: "a", Body: json.RawMessage(`{"expected":3,"covered":2}`)}}, Snapshot: &model.AnalysisSnapshot{ID: "snapshot", DataCutoff: 9000, InputHashes: []string{"fixed-input"}, Statistics: json.RawMessage(`{"processed":3}`)}})
	})
	r, err := s.Create(ctx, *a, KindDataQuality, "v1", serviceRequest("worker", "a"))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { s.RunWorkers(ctx, "test", slog.New(slog.NewTextHandler(io.Discard, nil))); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		current, err := s.Get(ctx, *a, KindDataQuality, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == model.AnalysisSucceeded {
			if current.Processed != 3 {
				t.Fatal("estimated progress", current)
			}
			snap, err := s.Snapshot(ctx, *a, KindDataQuality, r.ID)
			if err != nil || snap.FactsHash == "" || snap.DataCutoff != 9000 {
				t.Fatal("not frozen", snap, err)
			}
			items, total, err := s.Outputs(ctx, *a, KindDataQuality, r.ID, model.AnalysisFilter{Kind: "metrics"})
			if err != nil || total != 1 || len(items) != 1 {
				t.Fatal(items, total, err)
			}
			cancel()
			<-done
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("worker did not complete")
}

func TestServiceWorkerStopsAfterPermissionRevocation(t *testing.T) {
	s, a, mu := serviceFixture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	attempt := make(chan error, 1)
	_ = s.Register(KindDataQuality, func(ctx context.Context, e *Execution) error {
		close(started)
		<-ctx.Done()
		err := e.Commit(context.Background(), model.AnalysisBatch{ID: "stale", Status: model.AnalysisRunning, Processed: 1})
		attempt <- err
		return err
	})
	r, err := s.Create(ctx, *a, KindDataQuality, "v1", serviceRequest("revoke", "a"))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { s.RunWorkers(ctx, "test", slog.New(slog.NewTextHandler(io.Discard, nil))); close(done) }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("not started")
	}
	mu.Lock()
	a.Permissions = []string{"menu:devices"}
	a.AccessVersion = "v2"
	mu.Unlock()
	select {
	case err := <-attempt:
		if !errors.Is(err, ErrForbidden) {
			t.Fatal("revoked worker wrote output", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not cancel")
	}
	current, err := s.Store.GetAnalysisRun(context.Background(), "t", r.ID)
	if err != nil || current.Status != model.AnalysisCancelled || current.Processed != 0 {
		t.Fatal(current, err)
	}
	cancel()
	<-done
}
