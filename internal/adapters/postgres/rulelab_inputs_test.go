package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"time"

	"iot-platform/internal/adapters/clickhouse"
	"iot-platform/internal/analytics"
	"iot-platform/internal/analytics/dataquality/testkit"
	"iot-platform/internal/config"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"iot-platform/internal/rulelab/eval"
	"iot-platform/internal/rulelab/lab"
)

func ruleLabInputs(t *testing.T, store ports.RuleLabInputStore, seed testkit.SeedResult, source string) []model.RuleLabInput {
	t.Helper()
	values := []model.RuleLabInput{}
	err := store.RuleLabInputsRead(context.Background(), seed.TenantID, func(reader ports.RuleLabInputReader) error {
		q := model.RuleLabInputQuery{DeviceIDs: []string{seed.DeviceID}, Start: seed.Start, End: seed.End, TimeBasis: "EVENT", AvailabilitySource: source, Limit: 7}
		for {
			page, err := reader.ListStandardInputs(q)
			if err != nil {
				return err
			}
			values = append(values, page.Items...)
			if !page.HasMore {
				return nil
			}
			if page.Cursor == "" || page.Cursor == q.Cursor {
				return fmt.Errorf("cursor did not progress")
			}
			if _, err = reader.ListStandardInputs(model.RuleLabInputQuery{DeviceIDs: []string{"hidden-device"}, Start: q.Start, End: q.End, TimeBasis: q.TimeBasis, Cursor: page.Cursor, Limit: 7}); err == nil {
				return fmt.Errorf("cursor accepted changed device scope")
			}
			q.Cursor = page.Cursor
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return values
}
func ruleLabRealService(t *testing.T, repo *Repository, inputs ports.RuleLabInputStore) *lab.Service {
	t.Helper()
	actor := analytics.Actor{TenantID: "quality-test", Username: "quality-analyst", Permissions: []string{"*"}, AllDevices: true, AccessVersion: "fixture-access-v1"}
	analysis := analytics.NewService(repo, config.AnalyticsConfig{Workers: 1, BatchSize: 7, Poll: time.Millisecond, Lease: time.Second, RunTimeout: time.Minute}, func(context.Context, analytics.Actor) (analytics.Actor, error) { return actor, nil }, func(ctx context.Context, tenant, id string) error {
		_, err := repo.GetManagedDevice(ctx, tenant, id)
		return err
	})
	svc := lab.NewService(analysis, inputs)
	svc.Catalog = repo
	svc.History = repo
	svc.Facts = repo
	svc.ValidateCandidate = func(context.Context, analytics.Actor, model.AlarmRule) error { return nil }
	if err := svc.Register(); err != nil {
		t.Fatal(err)
	}
	return svc
}
func awaitRuleLabReal(t *testing.T, svc *lab.Service, tenant, id string) model.AnalysisRun {
	t.Helper()
	until := time.Now().Add(10 * time.Second)
	for time.Now().Before(until) {
		r, err := svc.Analysis.Store.GetAnalysisRun(context.Background(), tenant, id)
		if err != nil {
			t.Fatal(err)
		}
		if analytics.TerminalAnalysisStatus(r.Status) {
			if r.Status == model.AnalysisFailed {
				t.Fatal("rule lab failed", r.Error)
			}
			return r
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("rule lab run did not finish")
	return model.AnalysisRun{}
}
func TestRuleLabPostgresClickHouseWholeMessagesFixedDatasetAndParity(t *testing.T) {
	repo := testRepository(t)
	ch := monitoringClickHouseFixture(t, repo)
	ctx := context.Background()
	seed, err := testkit.Seed(ctx, ch, "quality-test", 40)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.pool.Exec(ctx, `UPDATE analytics_source_collection SET collection_started_at=$1,backfill_status='FIXTURE',historical_quality='CONTROLLED_SYNTHETIC'`, seed.Start-1000); err != nil {
		t.Fatal(err)
	}
	// Multiple parser outputs and messages without measurements are part of the
	// dataset, preserving event assertions and true raw-frame identity.
	for _, msg := range []model.StandardMessage{{TenantID: seed.TenantID, DeviceID: seed.DeviceID, ProductID: seed.ProductID, MessageID: "expanded-empty", RawMessageID: "raw-quality-fixture-0000", Timestamp: seed.Start + 100, MessageType: model.PropertyReport, Properties: map[string]any{}}, {TenantID: seed.TenantID, DeviceID: seed.DeviceID, ProductID: seed.ProductID, MessageID: "expanded-event", RawMessageID: "raw-quality-fixture-0000", Timestamp: seed.Start + 200, MessageType: model.EventReport, Event: map[string]any{"status": "text-event", "timestampText": "2026-10-01T00:00:00Z"}}} {
		if _, err = ch.SaveStandardMessageIfAbsent(ctx, msg); err != nil {
			t.Fatal(err)
		}
	}
	pgValues, chValues := ruleLabInputs(t, repo, seed, "clickhouse_telemetry_ack"), ruleLabInputs(t, ch, seed, "")
	if len(pgValues) != 42 || len(chValues) != 42 {
		t.Fatal("whole-message selection flattened input", len(pgValues), len(chValues))
	}
	for i, a := range pgValues {
		b := chValues[i]
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("same physical member and ACK differ for %s", a.ID)
		}
		if a.AvailableAt <= seed.End || a.ReceivedAt == 0 || a.Message.RawMessageID == "" {
			t.Fatal("source clocks or raw identity missing", a.ID)
		}
		if a.ID == "expanded-event" && a.AvailableAtSource != "postgres_standard_commit" {
			t.Fatal("nontelemetry event fabricated CH ACK", a.AvailableAtSource)
		}
		if a.ID == "quality-fixture-0000" && (a.Units["pressure"] != "kPa" || a.ProtocolVersion != "quality-protocol-v1" || a.PointTableVersion != "quality-points-v1") {
			t.Fatal("source units/versions lost")
		}
	}
	svc := ruleLabRealService(t, repo, ch)
	workerctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		svc.Analysis.RunWorkers(workerctx, "lab-real-fixture", slog.New(slog.NewTextHandler(io.Discard, nil)))
		close(done)
	}()
	defer func() { cancel(); <-done }()
	actor := analytics.Actor{TenantID: seed.TenantID, Username: "quality-analyst"}
	d, err := svc.CreateDataset(ctx, actor, model.RuleLabDatasetRequest{DeviceIDs: []string{seed.DeviceID}, Start: seed.Start, End: seed.End, WarmupStart: seed.Start, TimeBasis: "EVENT", ClockPolicy: "EVENT_AS_PROCESSING", InitialStatePolicy: "EMPTY_UNKNOWN", SemanticsVersion: eval.RevisionV2, IdempotencyKey: "real-whole-dataset"})
	if err != nil {
		t.Fatal(err)
	}
	awaitRuleLabReal(t, svc, seed.TenantID, d.RunID)
	fixed, total, err := svc.DatasetInputs(ctx, actor, d.ID, model.AnalysisFilter{Limit: 100})
	if err != nil || total != 42 || len(fixed) != 42 {
		t.Fatal("fixed input lost whole messages", total, err)
	}
	revision, err := repo.PublishRule(ctx, model.RulePublishRequest{Rule: model.AlarmRule{ID: "fixture-pressure-rule", TenantID: seed.TenantID, ProductID: seed.ProductID, Name: "隔离压力求值", AlarmType: "PRESSURE", Level: "HIGH", Enabled: true, Conditions: []model.RuleCondition{{Field: "pressure", Operator: ">", Value: 102}}}, Reason: "controlled source fixture", Actor: "fixture", SemanticsVersion: eval.RevisionV2})
	if err != nil {
		t.Fatal(err)
	}
	candidate := revision.Rule
	candidate.Conditions[0].Value = 1000
	body, _ := json.Marshal(model.RuleLabExperiment{DatasetID: d.ID, BaselineRevisionIDs: []string{revision.ID}, CandidateRuleID: revision.RuleID, Candidate: candidate, CandidateEnabled: true, Hypothesis: "手算隔离样本高阈值不触发", EvaluationPolicy: model.RuleLabEvaluationPolicy{Version: "fixture-v1", MatchTimeBasis: "EVENT", ExtraCyclePolicy: "COUNT_EACH_CYCLE", Split: "TUNING"}})
	experiment, err := svc.SaveExperiment(ctx, actor, model.RuleLabConfigRequest{ResourceID: "real-comparison", DeviceIDs: []string{seed.DeviceID}, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.CreateExperimentRun(ctx, actor, experiment.ID, model.RuleLabRunRequest{IdempotencyKey: "real-compare"})
	if err != nil {
		t.Fatal(err)
	}
	run = awaitRuleLabReal(t, svc, seed.TenantID, run.ID)
	before, err := svc.Report(ctx, actor, experiment.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var alarms int
	if err = repo.pool.QueryRow(ctx, `SELECT count(*) FROM alarm_record WHERE tenant_id=$1`, seed.TenantID).Scan(&alarms); err != nil || alarms != 0 {
		t.Fatal("experiment wrote production", alarms, err)
	}
	if _, err = repo.pool.Exec(ctx, `DELETE FROM raw_archive_index WHERE tenant_id=$1 AND message_id='raw-quality-fixture-0000'`, seed.TenantID); err != nil {
		t.Fatal(err)
	}
	if err = repo.RuleLabInputsRead(ctx, seed.TenantID, func(reader ports.RuleLabInputReader) error {
		page, e := reader.ListStandardInputs(model.RuleLabInputQuery{DeviceIDs: []string{seed.DeviceID}, Start: seed.Start, End: seed.End, TimeBasis: "RECEIVED", Limit: 100})
		if e != nil {
			return e
		}
		if page.Complete || page.Source.Complete || len(page.Items) != 39 {
			return fmt.Errorf("unknown reception members were silently omitted as complete")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Source retention cannot mutate the dataset membership or result report.
	if _, err = repo.pool.Exec(ctx, `DELETE FROM standard_message WHERE tenant_id=$1`, seed.TenantID); err != nil {
		t.Fatal(err)
	}
	after, err := svc.Report(ctx, actor, experiment.ID, run.ID)
	if err != nil || string(before) != string(after) {
		t.Fatal("source retention changed immutable report", err)
	}
	fixed, total, err = svc.DatasetInputs(ctx, actor, d.ID, model.AnalysisFilter{Limit: 100})
	if err != nil || total != 42 {
		t.Fatal("fixed input expired with original store", total, err)
	}
	t.Log("controlled synthetic 42 whole messages (40 pressure + empty + event), actual VM PG/CH same ACK parity, fixed source retention, zero production alarm writes")
}

var _ ports.RuleLabInputStore = (*clickhouse.Repository)(nil)
