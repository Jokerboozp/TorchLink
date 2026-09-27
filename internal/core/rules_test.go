package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

func TestMatchRuleAll(t *testing.T) {
	rule := model.AlarmRule{TenantID: "t", Enabled: true, Match: "all", Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}, {Field: "smoke", Operator: "eq", Value: true}}}
	msg := model.StandardMessage{TenantID: "t", Properties: map[string]any{"temperature": 81.2, "smoke": true}}
	if !MatchRule(rule, msg) {
		t.Fatal("expected match")
	}
	msg.Properties["smoke"] = false
	if MatchRule(rule, msg) {
		t.Fatal("unexpected match")
	}
}

func TestMatchRuleEventPath(t *testing.T) {
	rule := model.AlarmRule{TenantID: "t", Enabled: true, Conditions: []model.RuleCondition{{Field: "event.smoke", Operator: "eq", Value: true}}}
	msg := model.StandardMessage{TenantID: "t", Event: map[string]any{"smoke": true}}
	if !MatchRule(rule, msg) {
		t.Fatal("event-prefixed condition should match the event field")
	}
}

func TestAlarmTopicSanitizesSegments(t *testing.T) {
	a := model.Alarm{CityCode: "city", DistrictCode: "district/escape", BuildingID: "A", DeviceType: "smoke", DeviceID: "d"}
	if got := a.MQTTTopic("raised"); got != "/iot/alarm/raised/city/district_escape/A/smoke/d" {
		t.Fatalf("unexpected topic %s", got)
	}
}

func TestTenantRulesAreCachedUntilChanged(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	e := &Engine{Repo: repo}
	if err := repo.SaveRule(ctx, model.AlarmRule{ID: "r1", TenantID: "t1", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if rules, err := e.tenantRules(ctx, "t1"); err != nil || len(rules) != 1 {
		t.Fatalf("first load: %v %v", rules, err)
	}
	if err := repo.SaveRule(ctx, model.AlarmRule{ID: "r2", TenantID: "t1", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if rules, _ := e.tenantRules(ctx, "t1"); len(rules) != 1 {
		t.Fatalf("rules must come from the cache within the TTL, got %d", len(rules))
	}
	e.RulesChanged("t1")
	if rules, _ := e.tenantRules(ctx, "t1"); len(rules) != 2 {
		t.Fatalf("a change must be visible immediately after RulesChanged, got %d", len(rules))
	}
	if rules, _ := e.tenantRules(ctx, "t2"); len(rules) != 0 {
		t.Fatalf("tenants must not share cached rules: %v", rules)
	}
}

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

type ruleQueryCountingRepository struct {
	ports.Repository
	pendingDeletes, alarmLists int
}

func (r *ruleQueryCountingRepository) DeleteRulePending(ctx context.Context, tenant, rule, device string) error {
	r.pendingDeletes++
	return r.Repository.DeleteRulePending(ctx, tenant, rule, device)
}

func (r *ruleQueryCountingRepository) ListAlarms(ctx context.Context, f ports.AlarmFilter) ([]model.Alarm, error) {
	r.alarmLists++
	return r.Repository.ListAlarms(ctx, f)
}

// A message pays no rule queries for other products' rules or for pending
// durations that only duration rules create.
func TestRulesOfOtherProductsCostNoQueries(t *testing.T) {
	ctx := context.Background()
	repo := &ruleQueryCountingRepository{Repository: memory.NewRepository()}
	for _, product := range []string{"other-a", "other-b"} {
		if err := repo.SaveRule(ctx, model.AlarmRule{ID: "rule-" + product, TenantID: "tenant-a", ProductID: product, Name: product, AlarmType: "FIRE", Level: "HIGH", Enabled: true, Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}, Recovery: []model.RuleCondition{{Field: "temperature", Operator: "<", Value: 100}}}); err != nil {
			t.Fatal(err)
		}
	}
	e := newRuleTestEngine(t, repo, &ruleTestClock{now: time.Unix(1000, 0)})
	baseline := repo.alarmLists
	if err := e.handleStandard(ctx, standardRuleMessage("message-1", 1000000)); err != nil {
		t.Fatal(err)
	}
	// Only the device business-status check lists alarms (active and acknowledged).
	if repo.pendingDeletes != 0 || repo.alarmLists-baseline != 2 {
		t.Fatalf("unexpected rule queries: pendingDeletes=%d alarmLists=%d", repo.pendingDeletes, repo.alarmLists-baseline)
	}
}

func TestPresentRuleKeepsJSONExecutableAndGengineCommentedByDefault(t *testing.T) {
	rule := model.AlarmRule{
		Name:        "高温烟雾",
		Description: "温度过高且烟雾信号出现时告警。",
		AlarmType:   "FIRE_RISK",
		Level:       "HIGH",
		Match:       "all",
		Conditions: []model.RuleCondition{
			{Field: "temperature", Operator: ">", Value: 80},
			{Field: "smoke", Operator: "eq", Value: true},
		},
		Enabled: false,
	}
	presentation, err := PresentRule(rule)
	if err != nil {
		t.Fatal(err)
	}
	var executable map[string]any
	if err = json.Unmarshal([]byte(presentation.JSON), &executable); err != nil {
		t.Fatal(err)
	}
	if executable["description"] != rule.Description || strings.Contains(presentation.JSON, "_comment") {
		t.Fatalf("unexpected JSON presentation: %s", presentation.JSON)
	}
	if presentation.Gengine != `Properties["temperature"] > 80 && Properties["smoke"] == true` {
		t.Fatalf("unexpected Gengine: %q", presentation.Gengine)
	}
	if !strings.HasPrefix(presentation.GenginePlaceholder, "//") || !strings.Contains(presentation.GenginePlaceholder, presentation.Gengine) {
		t.Fatalf("Gengine is not commented in placeholder: %q", presentation.GenginePlaceholder)
	}
	for _, required := range []string{"conditions", "recovery[].field", "recovery[].operator", "recovery[].value", "actions[].type", "actions[].cameraId", "actions[].page"} {
		found := false
		for _, description := range presentation.FieldDescriptions {
			if description.Field == required && strings.TrimSpace(description.Meaning) != "" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing field description for %q", required)
		}
	}
}

func TestGeneratedGengineSpecialOperatorsValidateAndEvaluate(t *testing.T) {
	rule := model.AlarmRule{Conditions: []model.RuleCondition{
		{Field: "smokeText", Operator: "contains", Value: "smoke"},
		{Field: "temperature", Operator: "in", Value: []any{70, 80}},
		{Field: "properties.temperature", Operator: "exists"},
	}}
	// Validate each generated condition separately so the test also documents
	// the supported expression snippets exposed by the rule editor.
	for _, condition := range rule.Conditions {
		expression := RenderGengine(model.AlarmRule{Conditions: []model.RuleCondition{condition}})
		if err := ValidateGengineExpression(expression); err != nil {
			t.Fatalf("generated expression %q did not compile: %v", expression, err)
		}
	}
	if matched, err := EvaluateGengineExpression(`Contains(Properties["smokeText"], "smoke")`, model.StandardMessage{Properties: map[string]any{"smokeText": "smoke detected"}}); err != nil || !matched {
		t.Fatalf("contains expression matched=%v err=%v", matched, err)
	}
	if matched, err := EvaluateGengineExpression(`Exists("properties.temperature")`, model.StandardMessage{Properties: map[string]any{"temperature": 80}}); err != nil || !matched {
		t.Fatalf("exists expression matched=%v err=%v", matched, err)
	}
}

func TestGengineFieldNamesAndStringLiterals(t *testing.T) {
	msg := model.StandardMessage{TenantID: "t", Properties: map[string]any{"a-b": 90.0, "a_b": 20.0, "a.b": 5.0}, Tags: map[string]string{"a-b": "weekend"}, Event: map[string]any{"system-status": "weekend", "text": "Properties['a-b']"}}
	for _, tc := range []struct {
		expression string
		want       bool
	}{
		{`Properties["a-b"] > 80 && Properties["a_b"] < 30 && Properties["a.b"] == 5`, true},
		{`Properties["a_b"] > 80 || Properties["a-b"] < 30`, false},
		{`Properties['a-b'] == 90 && Properties["a-b"] > Properties["a_b"]`, true},
		{`Tags["a-b"] == "weekend" && Event["system-status"] == "weekend"`, true},
		{`Contains(Event["text"], "Properties['a-b']")`, true},
		{`Contains("weekend; system { begin }", "end")`, true},
		{`Properties [ "a-b" ] == 90`, true},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			if err := ValidateGengineExpression(tc.expression); err != nil {
				t.Fatal(err)
			}
			got, err := EvaluateGengineExpression(tc.expression, msg)
			if err != nil || got != tc.want {
				t.Fatalf("match=%v want=%v err=%v", got, tc.want, err)
			}
			if got := MatchRule(model.AlarmRule{TenantID: "t", Enabled: true, Expression: tc.expression}, msg); got != tc.want {
				t.Fatalf("rule match=%v want=%v", got, tc.want)
			}
		})
	}
	for _, expression := range []string{`system("x")`, `true { MarkMatched() } end`, `true; false`, `"unterminated`, `Contains("escaped\" end", "end") { MarkMatched() }`} {
		if err := ValidateGengineExpression(expression); err == nil {
			t.Errorf("invalid expression accepted: %s", expression)
		}
	}
}
