package recurring

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/alarmgovernance"
	"iot-platform/internal/analytics"
	"iot-platform/internal/config"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func TestPersistentRecurringSnapshotSourceWindowAndNewStandaloneFact(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := memory.NewRepository()
	gov := alarmgovernance.New(repo, nil, nil, repo)
	if err := gov.EnsureBuiltins(ctx, "t"); err != nil {
		t.Fatal(err)
	}
	actor := analytics.Actor{TenantID: "t", Username: "owner", AllDevices: true, Permissions: []string{"*"}, AccessVersion: "v1"}
	shared := analytics.NewService(analytics.NewMemoryStore(), config.AnalyticsConfig{Poll: 10 * time.Millisecond}, func(context.Context, analytics.Actor) (analytics.Actor, error) { return actor, nil }, func(context.Context, string, string) error { return nil })
	svc := NewService(shared, repo)
	if err := svc.Register(); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	end := start + 60000
	for i := 0; i < 3; i++ {
		o := model.AlarmObservation{TenantID: "t", DeviceID: "d", SourceSystem: "DEVICE", SourceEventID: string(rune('a' + i)), EventIndex: "fire", OriginKind: "DEVICE_DIRECT", AlarmType: "FIRE", SignalKey: "device:FIRE", FactKind: "REPORT", TimeQuality: "TRUSTED", EventAt: start + int64(i)*1000, ReceivedAt: start + int64(i)*1000, Acceptance: "ACCEPTED"}
		if _, _, e := repo.SaveAlarmObservation(ctx, o); e != nil {
			t.Fatal(e)
		}
	}
	params, _ := json.Marshal(Parameters{TimeBasis: "EVENT_AT", ProfileRevisionID: "profile-report-only-v1", DiscoveryPolicy: DiscoveryPolicy{MinReportCount: 2, Version: "fixture-v1"}})
	q := analytics.CreateRequest{DeviceIDs: []string{"d"}, Start: start, End: end, Parameters: params, IdempotencyKey: "reports"}
	if e := svc.ValidateCreate(ctx, actor, &q); e != nil {
		t.Fatal(e)
	}
	run, e := shared.Create(ctx, actor, analytics.KindRecurring, AlgorithmVersion, q)
	if e != nil {
		t.Fatal(e)
	}
	go shared.RunWorkers(ctx, "fixture", slog.New(slog.NewTextHandler(io.Discard, nil)))
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		run, e = shared.Get(ctx, actor, analytics.KindRecurring, run.ID)
		if e != nil {
			t.Fatal(e)
		}
		if analytics.TerminalAnalysisStatus(run.Status) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if run.Status != model.AnalysisPartial {
		t.Fatalf("run did not finish conservatively: %+v", run)
	}
	snap, e := shared.Snapshot(ctx, actor, analytics.KindRecurring, run.ID)
	if e != nil {
		t.Fatal(e)
	}
	var stats Statistics
	if e = json.Unmarshal(snap.Statistics, &stats); e != nil {
		t.Fatal(e)
	}
	if len(stats.Metrics) != 1 || stats.Metrics[0].ReportCount != 3 || len(stats.Metrics[0].Cycles) != 0 || stats.Metrics[0].NewStartsPer1000Hours != nil {
		t.Fatal(stats)
	}
	if e = svc.ValidateSnapshot(ctx, run); e != nil {
		t.Fatal(e)
	}
	later := model.AlarmObservation{TenantID: "t", DeviceID: "d", SourceSystem: "DEVICE", SourceEventID: "outside", EventIndex: "fire", OriginKind: "DEVICE_DIRECT", AlarmType: "FIRE", SignalKey: "device:FIRE", FactKind: "REPORT", TimeQuality: "TRUSTED", EventAt: end + 48*3600000, ReceivedAt: end + 48*3600000, Acceptance: "ACCEPTED"}
	if _, _, e = repo.SaveAlarmObservation(ctx, later); e != nil {
		t.Fatal(e)
	}
	if e = svc.ValidateSnapshot(ctx, run); e != nil {
		t.Fatalf("unrelated outside-window input invalidated: %v", e)
	}
	body, _ := json.Marshal(model.ActivityCoverage{CoverageID: "independent", DeviceIDs: []string{"d"}, ActivityType: "COOKING", StartAt: start, EndAt: end, Coverage: "PARTIAL", Basis: "later standalone registration", RevisionNumber: 1})
	e = repo.GovernanceTransaction(ctx, "t", func(tx ports.AlarmGovernanceTx) error {
		_, e := tx.Put(model.GovernanceDocument{Kind: model.GovernanceCoverageKind, ID: "new-coverage", DeviceIDs: []string{"d"}, Body: body}, 0)
		if e != nil {
			return e
		}
		return tx.BumpSourceVersions(depKeys(model.GovernancePoint{DeviceID: "d"}, "COVERAGE", start, end))
	})
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.ValidateSnapshot(ctx, run); !errors.Is(e, model.ErrAnalysisConflict) {
		t.Fatalf("new independent coverage did not invalidate snapshot: %v", e)
	}
	same, e := shared.Snapshot(ctx, actor, analytics.KindRecurring, run.ID)
	if e != nil || same.FactsHash != snap.FactsHash {
		t.Fatal("old frozen snapshot was rewritten", e)
	}
}

func TestCoverageCorrectionAndPartialWindowCannotClaimFullComparison(t *testing.T) {
	coverage := model.ActivityCoverage{CoverageID: "c", DeviceIDs: []string{"d"}, ActivityType: "COOKING", StartAt: 0, EndAt: 100, Coverage: "FULL_DECLARED", Basis: "log", RevisionNumber: 1}
	in := ActivityMetricInput{Window: Interval{0, 100}, DeviceID: "d", ActivityType: "COOKING", Unit: "MEAL", Monitoring: []Interval{{0, 100}}, Coverages: []model.ActivityCoverage{coverage}}
	correction := coverage
	correction.RevisionNumber = 2
	correction.Coverage = "ALARM_ONLY"
	in.Coverages = append(in.Coverages, correction)
	if m := ComputeActivityMetrics(in); m.Coverage == "FULL_DECLARED" {
		t.Fatal("superseded coverage survived", m)
	}
	coverage.EndAt = 50
	in.Coverages = []model.ActivityCoverage{coverage}
	if m := ComputeActivityMetrics(in); m.Coverage != "PARTIAL" {
		t.Fatal("complete subwindow became full requested-window denominator", m)
	}
}

func TestActivityCandidatesKeepEveryOverlapAndRespectAllowedTypes(t *testing.T) {
	o := model.AlarmObservation{ID: "o", DeviceID: "d", FactKind: "ASSERT", Acceptance: "ACCEPTED", TimeQuality: "TRUSTED", EventAt: 25}
	docs := []model.GovernanceDocument{}
	for _, id := range []string{"a", "b"} {
		v := model.FieldActivityRevision{ActivityID: id, DeviceIDs: []string{"d"}, ActivityType: "COOKING", Actual: true, Status: "CONFIRMED", TimeQuality: "VERIFIED", StartAt: ptr(10), EndAt: ptr(30), RevisionNumber: 1}
		body, _ := json.Marshal(v)
		docs = append(docs, model.GovernanceDocument{Kind: model.GovernanceActivityKind, ID: id + "-revision", RevisionNumber: 1, Body: body})
	}
	v := ActivityCandidates([]model.AlarmObservation{o}, docs, []string{"COOKING"})
	if len(v) != 2 || v[0].Relation != "UNRESOLVED" || v[1].Relation != "UNRESOLVED" {
		t.Fatal("overlap falsely selected one cause", v)
	}
	if len(ActivityCandidates([]model.AlarmObservation{o}, docs, []string{"CLEANING"})) != 0 {
		t.Fatal("configuration ignored")
	}
	o.TimeQuality = "UNVERIFIED"
	if len(ActivityCandidates([]model.AlarmObservation{o}, docs, []string{"COOKING"})) != 0 {
		t.Fatal("unknown clock fabricated precise relation")
	}
}

func TestHistoricalProjectionReadsOnlyCompletedAndPreservesProductionState(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	for _, id := range []string{"done", "pending"} {
		msg := model.StandardMessage{TenantID: "t", DeviceID: "d", MessageID: id, RawMessageID: "raw-" + id, MessageType: model.AlarmReport, Timestamp: 1000, Event: map[string]any{"alarmType": "FIRE"}}
		if e := repo.SaveStandardMessage(ctx, msg); e != nil {
			t.Fatal(e)
		}
		if id == "done" {
			claim, e := repo.ClaimStandardMessage(ctx, msg, "test", time.Minute)
			if e != nil {
				t.Fatal(e)
			}
			if e = repo.MarkStandardMessageProcessed(ctx, "t", id, claim.Token); e != nil {
				t.Fatal(e)
			}
		}
	}
	e := repo.GovernanceRead(ctx, "t", func(tx ports.AlarmGovernanceTx) error {
		items, e := tx.(ports.GovernanceHistoricalReader).ListGovernanceHistoricalMessages(ports.AlarmObservationFilter{DeviceIDs: []string{"d"}, Start: 1, End: 2000, Limit: 10})
		if e != nil {
			return e
		}
		if len(items) != 1 || items[0].MessageID != "done" {
			t.Fatal(items)
		}
		normalized, e := NormalizeHistoricalMessage(items[0], nil, 3000)
		if e != nil {
			return e
		}
		if len(normalized.Observations) != 1 || normalized.Observations[0].Acceptance != "HISTORICAL_UNRESOLVED" || normalized.Observations[0].SourceSystem != "HISTORICAL_STANDARD_MESSAGE" {
			t.Fatal(normalized)
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	observations, e := repo.ListAlarmObservations(ctx, "t", ports.AlarmObservationFilter{DeviceIDs: []string{"d"}, Start: 1, End: 2000, Limit: 10})
	if e != nil || len(observations) != 0 {
		t.Fatal("read-only projection persisted production facts", e, observations)
	}
}
