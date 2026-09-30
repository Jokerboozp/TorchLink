package core

import (
	"context"
	"encoding/json"
	"io"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"log/slog"
	"testing"
	"time"
)

type observationWrappedRepository struct {
	ports.Repository
	receipts int
}

func TestRuleLifecycleObservationRetainsIncomingAndProducingRecoveryRevision(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	rule := historyRule()
	first := publishHistory(t, repo, rule, 0)
	clock := &ruleTestClock{now: time.Unix(1000, 0)}
	engine := newRuleTestEngine(t, repo, clock)
	process := func(id string, at int64, temperature int) {
		t.Helper()
		msg := model.StandardMessage{TenantID: "tenant-a", DeviceID: "device-a", ProductID: "sensor", MessageID: id, RawMessageID: "raw-" + id, Timestamp: at, MessageType: model.PropertyReport, Properties: map[string]any{"temperature": temperature}, Tags: map[string]string{"eventTimeQuality": "TRUSTED"}}
		if _, err := repo.SaveRawIndex(ctx, model.RawArchiveIndex{TenantID: msg.TenantID, DeviceID: msg.DeviceID, MessageID: msg.RawMessageID, ReceivedAt: at + 100}); err != nil {
			t.Fatal(err)
		}
		if err := engine.handleStandard(ctx, mustJSON(msg)); err != nil {
			t.Fatal(err)
		}
	}
	process("normal-seed", 100, 40)
	clock.now = time.Unix(1010, 0)
	process("first-report", 200, 90)
	clock.now = time.Unix(1020, 0)
	process("latest-report", 300, 95)
	alarm, _ := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", DeviceID: "device-a", Status: "ACTIVE"})
	if len(alarm) != 1 || alarm[0].TriggerCount != 2 || alarm[0].CreatedRuleRevision != first.ID {
		t.Fatal(alarm)
	}
	changed := rule
	changed.Recovery = []model.RuleCondition{{Field: "temperature", Operator: "<", Value: 20}}
	publishHistory(t, repo, changed, first.Version)
	if err := engine.DisableRule(ctx, "tenant-a", "r"); err != nil {
		t.Fatal(err)
	}
	stored, _ := repo.GetAlarm(ctx, "tenant-a", alarm[0].ID)
	if stored.Status != "ACTIVE" {
		t.Fatal("administrative disable invented recovery", stored)
	}
	clock.now = time.Unix(1030, 0)
	process("producing-clear", 400, 40)
	stored, _ = repo.GetAlarm(ctx, "tenant-a", alarm[0].ID)
	if stored.Status != "RECOVERED" || stored.CreatedRuleRevision != first.ID {
		t.Fatal("producing recovery changed", stored)
	}
	facts, err := repo.ListAlarmObservations(ctx, "tenant-a", ports.AlarmObservationFilter{DeviceIDs: []string{"device-a"}, OriginKind: "RULE_LIFECYCLE"})
	if err != nil || len(facts) != 4 {
		t.Fatal(facts, err)
	}
	for i, fact := range facts {
		wantKind := "ASSERT"
		if i == 0 || i == 3 {
			wantKind = "CLEAR"
		}
		if fact.FactKind != wantKind || fact.Acceptance != "ACCEPTED" || fact.RuleVersion != first.Version || fact.ConditionHash != ruleObservationConditionHash(first.Rule) || fact.EvaluationAt == fact.EventAt || fact.RawMessageID == "" || fact.SourceInputHash == "" || fact.ReceivedAt != fact.EventAt+100 {
			t.Fatal("lost rule provenance", fact)
		}
		if i == 2 {
			body, _ := json.Marshal(fact.Payload["message"])
			var incoming model.StandardMessage
			if json.Unmarshal(body, &incoming) != nil || incoming.Properties["temperature"] != float64(95) {
				t.Fatal("retained aggregate's old payload", fact)
			}
		}
	}
	traces, _, err := repo.ListRuleEvaluationTraces(ctx, "tenant-a", model.RuleTraceFilter{DeviceIDs: []string{"device-a"}})
	if err != nil || len(traces) != 4 {
		t.Fatal(traces, err)
	}
	for _, trace := range traces {
		if trace.Status != "COMPLETE" || trace.ReproductionQuality != "EXACT" {
			t.Fatal("observation broke production trace", trace)
		}
	}
}

func (r *observationWrappedRepository) GetRawIndex(ctx context.Context, tenant, id string) (model.RawArchiveIndex, error) {
	r.receipts++
	return r.Repository.GetRawIndex(ctx, tenant, id)
}
func TestAlarmObservationRecoverySurvivesRepositoryDecorators(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	wrapped := &observationWrappedRepository{Repository: repo}
	archive, _ := local.NewArchive(t.TempDir())
	e := New(wrapped, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	normal := model.StandardMessage{TenantID: "t", ProductID: "p", DeviceID: "d", MessageID: "normal", MessageType: model.StateChange, Timestamp: 100, Properties: map[string]any{"fireAlarm": false}}
	if err := e.handleStandard(ctx, mustJSON(normal)); err != nil {
		t.Fatal("normal seed failed through decorator", err)
	}
	facts, err := repo.ListAlarmObservations(ctx, "t", ports.AlarmObservationFilter{DeviceIDs: []string{"d"}})
	if err != nil || len(facts) != 1 || facts[0].FactKind != "CLEAR" {
		t.Fatal(facts, err)
	}
	alarm := normal
	alarm.MessageID = "alarm"
	alarm.Timestamp = 200
	alarm.MessageType = model.AlarmReport
	alarm.Properties = map[string]any{"fireAlarm": true}
	if err = e.handleStandard(ctx, mustJSON(alarm)); err != nil {
		t.Fatal(err)
	}
	normal.MessageID = "clear"
	normal.Timestamp = 300
	if err = e.handleStandard(ctx, mustJSON(normal)); err != nil {
		t.Fatal("recovery failed through decorator", err)
	}
	alarms, _ := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t", DeviceID: "d"})
	if len(alarms) != 1 || alarms[0].Status != "RECOVERED" {
		t.Fatal(alarms)
	}
}
func TestAlarmObservationReusesOneRawReceiptForComponentSlots(t *testing.T) {
	repo := memory.NewRepository()
	wrapped := &observationWrappedRepository{Repository: repo}
	archive, _ := local.NewArchive(t.TempDir())
	e := New(wrapped, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	msg := model.StandardMessage{TenantID: "t", ProductID: "p", DeviceID: "d", MessageID: "components", RawMessageID: "raw", Timestamp: 100, MessageType: model.StateChange, Event: map[string]any{"components": []model.ComponentStatus{{ID: "one", Alarms: map[string]bool{"FIRE": true, "FAULT": false}}, {ID: "two", Alarms: map[string]bool{"FIRE": false, "FAULT": true}}}}}
	if err := e.handleStandard(context.Background(), mustJSON(msg)); err != nil {
		t.Fatal(err)
	}
	if wrapped.receipts != 1 {
		t.Fatalf("four component slots read raw receipt %d times", wrapped.receipts)
	}
}
