package recurring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/analytics"
	"iot-platform/internal/config"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type projectionStoreHook struct {
	ports.AnalysisStore
	before func(context.Context, string, string, int64, model.AnalysisBatch) error
	after  func(model.AnalysisRun, model.AnalysisBatch)
}

func (s *projectionStoreHook) CommitAnalysisBatch(ctx context.Context, tenant, id string, token int64, batch model.AnalysisBatch) (model.AnalysisRun, error) {
	if s.before != nil {
		if err := s.before(ctx, tenant, id, token, batch); err != nil {
			return model.AnalysisRun{}, err
		}
	}
	run, err := s.AnalysisStore.CommitAnalysisBatch(ctx, tenant, id, token, batch)
	if err == nil && s.after != nil {
		s.after(run, batch)
	}
	return run, err
}
func projectionFixture(t *testing.T, store ports.AnalysisStore, limit int) (*Service, *memory.Repository, analytics.Actor) {
	t.Helper()
	repo := memory.NewRepository()
	actor := analytics.Actor{TenantID: "t", Username: "owner", AllDevices: true, Permissions: []string{"*"}, AccessVersion: "v1"}
	shared := analytics.NewService(store, config.AnalyticsConfig{Workers: 1, BatchSize: 1, RecordLimit: limit, Lease: 300 * time.Millisecond, Poll: 5 * time.Millisecond, RunTimeout: time.Minute}, func(context.Context, analytics.Actor) (analytics.Actor, error) { return actor, nil }, func(context.Context, string, string) error { return nil })
	svc := NewService(shared, repo)
	if err := svc.Register(); err != nil {
		t.Fatal(err)
	}
	return svc, repo, actor
}
func saveProjectionMessage(t *testing.T, repo *memory.Repository, id, device string, at int64, processed bool) {
	t.Helper()
	ctx := context.Background()
	msg := model.StandardMessage{TenantID: "t", DeviceID: device, MessageID: id, RawMessageID: "raw-" + id, MessageType: model.AlarmReport, Timestamp: at, Event: map[string]any{"alarmType": "FIRE"}, Raw: map[string]any{"restricted": "never freeze"}}
	if err := repo.SaveStandardMessage(ctx, msg); err != nil {
		t.Fatal(err)
	}
	if processed {
		claim, err := repo.ClaimStandardMessage(ctx, msg, "test", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.MarkStandardMessageProcessed(ctx, "t", id, claim.Token); err != nil {
			t.Fatal(err)
		}
	}
}
func createProjection(t *testing.T, svc *Service, a analytics.Actor, key string, sources ...string) model.AnalysisRun {
	t.Helper()
	p, _ := json.Marshal(Parameters{JobMode: HistoricalProjectionMode, TimeBasis: "EVENT_AT", HistoricalSources: sources})
	q := analytics.CreateRequest{DeviceIDs: []string{"a", "b"}, Start: 1000, End: 2000, Parameters: p, IdempotencyKey: key}
	if err := svc.ValidateCreate(context.Background(), a, &q); err != nil {
		t.Fatal(err)
	}
	run, err := svc.Analysis.Create(context.Background(), a, analytics.KindRecurring, ProjectionAlgorithmVersion, q)
	if err != nil {
		t.Fatal(err)
	}
	return run
}
func projectionWorkers(svc *Service) (context.CancelFunc, <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.Analysis.RunWorkers(ctx, "projection-test", slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	return cancel, done
}
func waitProjection(t *testing.T, svc *Service, id string, terminal bool) model.AnalysisRun {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		run, err := svc.Analysis.Store.GetAnalysisRun(context.Background(), "t", id)
		if err != nil {
			t.Fatal(err)
		}
		if terminal && analytics.TerminalAnalysisStatus(run.Status) || !terminal && run.Processed > 0 {
			return run
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("projection did not reach expected persisted state")
	return model.AnalysisRun{}
}

func TestHistoricalProjectionPersistentCheckpointReclaimAndWholeScopeReferences(t *testing.T) {
	var now atomic.Int64
	now.Store(time.Now().UnixNano())
	base := analytics.NewMemoryStoreWithClock(func() time.Time { return time.Unix(0, now.Load()) })
	hook := &projectionStoreHook{AnalysisStore: base}
	svc, repo, actor := projectionFixture(t, hook, 20)
	for i := 0; i < 3; i++ {
		saveProjectionMessage(t, repo, fmt.Sprintf("m%d", i), "a", 1000+int64(i), true)
	}
	saveProjectionMessage(t, repo, "pending", "a", 1004, false)
	run := createProjection(t, svc, actor, "persistent", "POSTGRESQL")
	if same := createProjection(t, svc, actor, "persistent", "POSTGRESQL"); same.ID != run.ID {
		t.Fatal("idempotent task duplicated")
	}
	firstContext, stop := context.WithCancel(context.Background())
	var once sync.Once
	hook.after = func(_ model.AnalysisRun, b model.AnalysisBatch) {
		if b.Stage == "normalizing-history" {
			once.Do(func() { stop() })
		}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.Analysis.RunWorkers(firstContext, "projection-test-first", slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	<-done
	first := waitProjection(t, svc, run.ID, false)
	if first.Processed != 1 || !first.InputsFrozen || first.SnapshotID != "" {
		t.Fatal("uncommitted progress published", first)
	}
	var cp projectionCheckpoint
	if json.Unmarshal(first.Checkpoint, &cp) != nil || cp.NextFact != 1 || cp.CompletedBatches != 1 {
		t.Fatal(first.Checkpoint)
	}
	oldToken := first.LeaseToken
	// A new successful source after shutdown must not enter the frozen task.
	saveProjectionMessage(t, repo, "after-freeze", "a", 1005, true)
	now.Add(int64(time.Second))
	hook.after = nil
	cancel, secondDone := projectionWorkers(svc)
	finished := waitProjection(t, svc, run.ID, true)
	cancel()
	<-secondDone
	if finished.Status != model.AnalysisPartial || finished.Processed != 3 || finished.LeaseToken <= oldToken {
		t.Fatal("checkpoint was not reclaimed", finished)
	}
	snap, err := base.GetAnalysisSnapshot(context.Background(), "t", finished.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	var stats ProjectionStatistics
	if json.Unmarshal(snap.Statistics, &stats) != nil || stats.MessageScanned != 3 || stats.NormalizedFacts != 3 || stats.CompletedBatches != 3 || stats.Quality != "HISTORICAL_UNRESOLVED" {
		t.Fatal(stats)
	}
	if snap.Sources[0].Complete || snap.FactsHash == "" {
		t.Fatal("unknown historical coverage claimed complete", snap)
	}
	if _, err := base.CommitAnalysisBatch(context.Background(), "t", run.ID, oldToken, model.AnalysisBatch{ID: "stale", Status: model.AnalysisRunning}); !errors.Is(err, model.ErrAnalysisLeaseLost) {
		t.Fatal("old worker committed", err)
	}
	rows, n, err := base.ListAnalysisOutputs(context.Background(), "t", model.AnalysisFilter{RunID: run.ID, Kind: "projection-observations", Limit: 100})
	if err != nil || n != 3 {
		t.Fatal(rows, n, err)
	}
	for _, row := range rows {
		var o model.AlarmObservation
		_ = json.Unmarshal(row.Body, &o)
		if o.Payload != nil || o.Acceptance != "HISTORICAL_UNRESOLVED" || o.StandardMessageID == "pending" || o.StandardMessageID == "after-freeze" {
			t.Fatal("unsafe frozen fact", o)
		}
	}
	production, err := repo.ListAlarmObservations(context.Background(), "t", ports.AlarmObservationFilter{DeviceIDs: []string{"a", "b"}, Start: 1000, End: 2000, Limit: 10})
	if err != nil || len(production) != 0 {
		t.Fatal("projection changed production ledger", production, err)
	}
	_, refs, err := svc.projectionInputs(context.Background(), model.AnalysisRun{TenantID: "t", DeviceIDs: []string{"a", "b"}, Start: 1000, End: 2000}, []string{run.ID})
	if err != nil || len(refs) != 1 || refs[0].FactsHash != snap.FactsHash {
		t.Fatal(refs, err)
	}
	if _, _, err := svc.projectionInputs(context.Background(), model.AnalysisRun{TenantID: "t", DeviceIDs: []string{"a"}, Start: 1000, End: 2000}, []string{run.ID}); !errors.Is(err, analytics.ErrForbidden) {
		t.Fatal("shared projection trimmed to one member", err)
	}
	params, _ := json.Marshal(Parameters{HistoricalProjectionRunIDs: []string{run.ID}})
	if err := svc.AuthorizeInputs(context.Background(), model.AnalysisRun{TenantID: "t", DeviceIDs: []string{"a", "b"}, Parameters: params}, func(d model.GovernanceDocument) error {
		if slices.Contains(d.DeviceIDs, "b") {
			return analytics.ErrForbidden
		}
		return nil
	}); !errors.Is(err, analytics.ErrForbidden) {
		t.Fatal("source revocation bypassed before freeze", err)
	}
}

func TestHistoricalProjectionReadLimitAndMissingArchiveRemainPartial(t *testing.T) {
	svc, repo, actor := projectionFixture(t, analytics.NewMemoryStore(), 2)
	for i := 0; i < 4; i++ {
		saveProjectionMessage(t, repo, fmt.Sprintf("m%d", i), "a", 1000+int64(i), true)
	}
	run := createProjection(t, svc, actor, "bounded", "POSTGRESQL", "CLICKHOUSE")
	cancel, done := projectionWorkers(svc)
	defer func() { cancel(); <-done }()
	run = waitProjection(t, svc, run.ID, true)
	snap, err := svc.Analysis.Snapshot(context.Background(), actor, analytics.KindRecurring, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var stats ProjectionStatistics
	_ = json.Unmarshal(snap.Statistics, &stats)
	if run.Status != model.AnalysisPartial || stats.NormalizedFacts != 2 || stats.MessageScanned != 2 || !slices.Contains(snap.Limitations, "HISTORICAL_READ_LIMIT_REACHED") || !slices.Contains(snap.MissingSources, "CLICKHOUSE") || stats.SourceProgress[0].Exhausted {
		t.Fatal(run, snap, stats)
	}
}

func TestHistoricalProjectionCancellationFencesAtomicBatchAndFailureCannotBeReferenced(t *testing.T) {
	for _, cancelTask := range []bool{true, false} {
		t.Run(fmt.Sprint(cancelTask), func(t *testing.T) {
			base := analytics.NewMemoryStore()
			hook := &projectionStoreHook{AnalysisStore: base}
			svc, repo, actor := projectionFixture(t, hook, 10)
			saveProjectionMessage(t, repo, "m", "a", 1000, true)
			hook.before = func(ctx context.Context, tenant, id string, _ int64, b model.AnalysisBatch) error {
				if b.Stage != "normalizing-history" {
					return nil
				}
				if !cancelTask {
					return errors.New("injected durable batch failure")
				}
				current, err := base.GetAnalysisRun(ctx, tenant, id)
				if err != nil {
					return err
				}
				_, err = base.StopAnalysisRun(ctx, tenant, id, current.Version)
				return err
			}
			run := createProjection(t, svc, actor, "terminal", "POSTGRESQL")
			cancel, done := projectionWorkers(svc)
			defer func() { cancel(); <-done }()
			run = waitProjection(t, svc, run.ID, true)
			want := model.AnalysisFailed
			if cancelTask {
				want = model.AnalysisCancelled
			}
			if run.Status != want || run.Processed != 0 || run.SnapshotID != "" {
				t.Fatal("failed/cancelled batch exposed progress", run)
			}
			_, n, err := base.ListAnalysisOutputs(context.Background(), "t", model.AnalysisFilter{RunID: run.ID, Kind: "projection-observations"})
			if err != nil || n != 0 {
				t.Fatal("fenced batch wrote facts", n, err)
			}
			if _, _, err := svc.projectionInputs(context.Background(), model.AnalysisRun{TenantID: "t", DeviceIDs: []string{"a", "b"}, Start: 1000, End: 2000}, []string{run.ID}); !errors.Is(err, model.ErrAnalysisInvalid) {
				t.Fatal("unfinished projection was referenced", err)
			}
		})
	}
}
