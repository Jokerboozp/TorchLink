package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

type ruleTestClock struct{ now time.Time }

func (c *ruleTestClock) Now() time.Time { return c.now }

type failFirstStateRepository struct {
	ports.Repository
	fail bool
}

func (r *failFirstStateRepository) UpsertDeviceState(ctx context.Context, state model.DeviceState) error {
	if r.fail {
		r.fail = false
		return errors.New("simulated state write failure")
	}
	return r.Repository.UpsertDeviceState(ctx, state)
}

func newRuleTestEngine(t *testing.T, repo ports.Repository, clock *ruleTestClock) *Engine {
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.Clock = clock
	return e
}

func standardRuleMessage(id string, ts int64) []byte {
	b, _ := json.Marshal(model.StandardMessage{MessageID: id, RawMessageID: "raw-" + id, TenantID: "tenant-a", ProductID: "sensor", DeviceID: "device-a", MessageType: model.PropertyReport, Timestamp: ts, Properties: map[string]any{"temperature": 90}})
	return b
}

func TestDurationRuleSurvivesEngineRestart(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	if err := repo.SaveRule(ctx, model.AlarmRule{ID: "rule-duration", TenantID: "tenant-a", ProductID: "sensor", Name: "持续高温", AlarmType: "HIGH_TEMPERATURE", Level: "HIGH", Enabled: true, DurationSeconds: 10, Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}}); err != nil {
		t.Fatal(err)
	}
	clock := &ruleTestClock{now: time.Unix(1000, 0)}
	first := newRuleTestEngine(t, repo, clock)
	if err := first.handleStandard(ctx, standardRuleMessage("message-1", 1000000)); err != nil {
		t.Fatal(err)
	}
	if alarms, _ := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", Status: "ACTIVE"}); len(alarms) != 0 {
		t.Fatalf("alarm triggered before duration: %#v", alarms)
	}

	clock.now = time.Unix(1011, 0)
	second := newRuleTestEngine(t, repo, clock)
	if err := second.handleStandard(ctx, standardRuleMessage("message-2", 1011000)); err != nil {
		t.Fatal(err)
	}
	alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", Status: "ACTIVE"})
	if err != nil || len(alarms) != 1 {
		t.Fatalf("persisted duration did not trigger: alarms=%#v err=%v", alarms, err)
	}
}

func TestDuplicateStandardMessageDoesNotRetriggerAlarm(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	if err := repo.SaveRule(ctx, model.AlarmRule{ID: "rule-duplicate", TenantID: "tenant-a", ProductID: "sensor", Name: "高温", AlarmType: "HIGH_TEMPERATURE", Level: "HIGH", Enabled: true, Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}, Actions: []model.RuleAction{{Type: "OPEN_PAGE", Page: "alarms"}}}); err != nil {
		t.Fatal(err)
	}
	e := newRuleTestEngine(t, repo, &ruleTestClock{now: time.Unix(1000, 0)})
	realtime := e.Realtime.(*local.Realtime)
	payload := standardRuleMessage("same-message", 1000000)
	if err := e.handleStandard(ctx, payload); err != nil {
		t.Fatal(err)
	}
	if err := e.handleStandard(ctx, payload); err != nil {
		t.Fatal(err)
	}
	alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", Status: "ACTIVE"})
	if err != nil || len(alarms) != 1 || alarms[0].TriggerCount != 1 {
		t.Fatalf("duplicate retriggered alarm: alarms=%#v err=%v", alarms, err)
	}
	var actionCount int
	for _, published := range realtime.Messages {
		if published.Topic == "/iot/ui-action/tenant-a" {
			actionCount++
		}
	}
	if actionCount != 1 {
		t.Fatalf("duplicate message executed rule action %d times, want 1", actionCount)
	}
}

func TestMatchingAlarmRuleActionsRunForEachNewAlarmMessage(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	if err := repo.SaveRule(ctx, model.AlarmRule{
		ID: "rule-actions", TenantID: "tenant-a", ProductID: "sensor", Name: "高温动作",
		AlarmType: "HIGH_TEMPERATURE", Level: "HIGH", Enabled: true,
		Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}},
		Actions:    []model.RuleAction{{Type: "OPEN_PAGE", Page: "alarms"}},
	}); err != nil {
		t.Fatal(err)
	}
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	realtime := local.NewRealtime()
	e := New(repo, archive, local.NewBus(), realtime, parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.Clock = &ruleTestClock{now: time.Unix(1000, 0)}

	for _, messageID := range []string{"alarm-message-1", "alarm-message-2"} {
		if err := e.handleStandard(ctx, standardRuleMessage(messageID, e.Clock.Now().UnixMilli())); err != nil {
			t.Fatal(err)
		}
		e.Clock = &ruleTestClock{now: e.Clock.Now().Add(time.Second)}
	}

	alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", DeviceID: "device-a", Status: "ACTIVE"})
	if err != nil || len(alarms) != 1 || alarms[0].TriggerCount != 2 {
		t.Fatalf("unexpected deduplicated alarm: alarms=%#v err=%v", alarms, err)
	}
	var actionCount int
	for _, published := range realtime.Messages {
		if published.Topic == "/iot/ui-action/tenant-a" {
			actionCount++
		}
	}
	if actionCount != 2 {
		t.Fatalf("matching alarm rule action count = %d, want 2", actionCount)
	}
}

func TestUnprocessedStandardMessageIsRetriedAfterDownstreamFailure(t *testing.T) {
	ctx := context.Background()
	base := memory.NewRepository()
	repo := &failFirstStateRepository{Repository: base, fail: true}
	if err := base.SaveRule(ctx, model.AlarmRule{ID: "rule-retry", TenantID: "tenant-a", ProductID: "sensor", Name: "高温", AlarmType: "HIGH_TEMPERATURE", Level: "HIGH", Enabled: true, Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}}); err != nil {
		t.Fatal(err)
	}
	e := newRuleTestEngine(t, repo, &ruleTestClock{now: time.Unix(1000, 0)})
	payload := standardRuleMessage("retry-message", 1000000)
	if err := e.handleStandard(ctx, payload); err == nil {
		t.Fatal("expected first downstream failure")
	}
	if err := e.handleStandard(ctx, payload); err != nil {
		t.Fatal(err)
	}
	alarms, err := base.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", Status: "ACTIVE"})
	if err != nil || len(alarms) != 1 {
		t.Fatalf("unprocessed message was not retried: alarms=%#v err=%v", alarms, err)
	}
}

func TestDeleteRuleRecoversActiveAlarmsAndClearsPending(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	if err := repo.SaveRule(ctx, model.AlarmRule{ID: "rule-delete", TenantID: "tenant-a", ProductID: "sensor", Name: "高温", AlarmType: "HIGH_TEMPERATURE", Level: "HIGH", Enabled: true, Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}}); err != nil {
		t.Fatal(err)
	}
	e := newRuleTestEngine(t, repo, &ruleTestClock{now: time.Unix(1000, 0)})
	if err := e.handleStandard(ctx, standardRuleMessage("delete-message", 1000000)); err != nil {
		t.Fatal(err)
	}
	if err := e.DeleteRule(ctx, "tenant-a", "rule-delete"); err != nil {
		t.Fatal(err)
	}
	if active, _ := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", Status: "ACTIVE"}); len(active) != 0 {
		t.Fatalf("deleted rule left active alarms: %#v", active)
	}
	recovered, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", Status: "RECOVERED"})
	if err != nil || len(recovered) != 1 {
		t.Fatalf("deleted rule did not retain recovered history: alarms=%#v err=%v", recovered, err)
	}
}

func TestDisableRuleRecoversActiveAlarmsAndClearsPending(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	if err := repo.SaveRule(ctx, model.AlarmRule{ID: "rule-disable", TenantID: "tenant-a", ProductID: "sensor", Name: "高温", AlarmType: "HIGH_TEMPERATURE", Level: "HIGH", Enabled: true, Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}}); err != nil {
		t.Fatal(err)
	}
	e := newRuleTestEngine(t, repo, &ruleTestClock{now: time.Unix(1000, 0)})
	if err := e.handleStandard(ctx, standardRuleMessage("disable-message", 1000000)); err != nil {
		t.Fatal(err)
	}
	if err := e.DisableRule(ctx, "tenant-a", "rule-disable"); err != nil {
		t.Fatal(err)
	}
	if active, _ := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", Status: "ACTIVE"}); len(active) != 0 {
		t.Fatalf("disabled rule left active alarms: %#v", active)
	}
	recovered, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", Status: "RECOVERED"})
	if err != nil || len(recovered) != 1 {
		t.Fatalf("disabled rule did not retain recovered history: alarms=%#v err=%v", recovered, err)
	}
}
