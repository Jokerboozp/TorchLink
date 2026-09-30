package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/recurring"
	"iot-platform/internal/config"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type postgresProjectionInterruption struct {
	ports.AnalysisStore
	stop context.CancelFunc
	once sync.Once
}

func (s *postgresProjectionInterruption) CommitAnalysisBatch(ctx context.Context, tenant, id string, token int64, b model.AnalysisBatch) (model.AnalysisRun, error) {
	r, err := s.AnalysisStore.CommitAnalysisBatch(ctx, tenant, id, token, b)
	if err == nil && b.Stage == "normalizing-history" {
		s.once.Do(s.stop)
	}
	return r, err
}

func TestGovernancePostgresHistoricalProjectionSurvivesWorkerReplacement(t *testing.T) {
	r := testRepository(t)
	ctx := context.Background()
	actor := analytics.Actor{TenantID: "projection-t", Username: "owner", Permissions: []string{"*"}, AllDevices: true, AccessVersion: "v1"}
	for _, id := range []string{"m1", "m2", "pending"} {
		msg := model.StandardMessage{TenantID: actor.TenantID, DeviceID: "d", MessageID: id, RawMessageID: "raw-" + id, MessageType: model.AlarmReport, Timestamp: 1000, Event: map[string]any{"alarmType": "FIRE"}}
		if err := r.SaveStandardMessage(ctx, msg); err != nil {
			t.Fatal(err)
		}
		if id == "pending" {
			continue
		}
		claim, err := r.ClaimStandardMessage(ctx, msg, "projection-test", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.MarkStandardMessageProcessed(ctx, actor.TenantID, id, claim.Token); err != nil {
			t.Fatal(err)
		}
	}
	limits := config.AnalyticsConfig{Workers: 1, BatchSize: 1, Lease: 300 * time.Millisecond, Poll: 5 * time.Millisecond, RunTimeout: time.Minute}
	resolve := func(context.Context, analytics.Actor) (analytics.Actor, error) { return actor, nil }
	device := func(context.Context, string, string) error { return nil }
	firstContext, firstStop := context.WithCancel(ctx)
	hook := &postgresProjectionInterruption{AnalysisStore: r, stop: firstStop}
	shared := analytics.NewService(hook, limits, resolve, device)
	svc := recurring.NewService(shared, r)
	if err := svc.Register(); err != nil {
		t.Fatal(err)
	}
	params, _ := json.Marshal(recurring.Parameters{JobMode: recurring.HistoricalProjectionMode, TimeBasis: "EVENT_AT", HistoricalSources: []string{"POSTGRESQL"}})
	q := analytics.CreateRequest{DeviceIDs: []string{"d"}, Start: 1000, End: 2000, Parameters: params, IdempotencyKey: "pg-projection"}
	if err := svc.ValidateCreate(ctx, actor, &q); err != nil {
		t.Fatal(err)
	}
	run, err := shared.Create(ctx, actor, analytics.KindRecurring, recurring.ProjectionAlgorithmVersion, q)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		shared.RunWorkers(firstContext, "first-pg-worker", slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	<-done
	checkpoint, err := r.GetAnalysisRun(ctx, actor.TenantID, run.ID)
	if err != nil || checkpoint.Processed != 1 || !checkpoint.InputsFrozen {
		t.Fatal(checkpoint, err)
	}
	// Construct both services afresh; the checkpoint and frozen originals must
	// come from PostgreSQL rather than the original process's local memory.
	second := analytics.NewService(r, limits, resolve, device)
	secondSvc := recurring.NewService(second, r)
	if err := secondSvc.Register(); err != nil {
		t.Fatal(err)
	}
	secondContext, secondStop := context.WithCancel(ctx)
	defer secondStop()
	secondDone := make(chan struct{})
	go func() {
		defer close(secondDone)
		second.RunWorkers(secondContext, "replacement-pg-worker", slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, err = r.GetAnalysisRun(ctx, actor.TenantID, run.ID)
		if err != nil {
			t.Fatal(err)
		}
		if analytics.TerminalAnalysisStatus(run.Status) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	secondStop()
	<-secondDone
	if run.Status != model.AnalysisPartial || run.Processed != 2 || run.LeaseToken <= checkpoint.LeaseToken {
		t.Fatal("PG projection did not recover", run)
	}
	snap, err := r.GetAnalysisSnapshot(ctx, actor.TenantID, run.SnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	var stats recurring.ProjectionStatistics
	if json.Unmarshal(snap.Statistics, &stats) != nil || stats.NormalizedFacts != 2 || stats.CompletedBatches != 2 || stats.MessageScanned != 2 {
		t.Fatal(stats)
	}
	_, n, err := r.ListAnalysisOutputs(ctx, actor.TenantID, model.AnalysisFilter{RunID: run.ID, Kind: "projection-observations"})
	if err != nil || n != 2 {
		t.Fatal(n, err)
	}
	production, err := r.ListAlarmObservations(ctx, actor.TenantID, ports.AlarmObservationFilter{DeviceIDs: []string{"d"}, Start: 1000, End: 2000, Limit: 10})
	if err != nil || len(production) != 0 {
		t.Fatal("historical task wrote production facts", production, err)
	}
	if _, err := r.CommitAnalysisBatch(ctx, actor.TenantID, run.ID, checkpoint.LeaseToken, model.AnalysisBatch{ID: "expired-writer", Status: model.AnalysisRunning}); !errors.Is(err, model.ErrAnalysisLeaseLost) {
		t.Fatal("old PG writer crossed fence", err)
	}
}
