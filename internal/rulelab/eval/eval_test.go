package eval_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/core"
	"iot-platform/internal/model"
	"iot-platform/internal/rulelab/eval"
)

func instant(n int64) *int64 { return &n }
func sameAlarmBody(a, b model.Alarm) bool {
	a.Version, b.Version = 0, 0 // Storage CAS counters belong to each adapter.
	return reflect.DeepEqual(a, b)
}
func rule() model.AlarmRule {
	return model.AlarmRule{ID: "r", TenantID: "t", ProductID: "p", Name: "高温", AlarmType: "HIGH_TEMPERATURE", Level: "HIGH", Enabled: true, Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}, Recovery: []model.RuleCondition{{Field: "temperature", Operator: "<", Value: 100}}, Actions: []model.RuleAction{{Type: "OPEN_PAGE", Page: "alarms"}}, Version: 1}
}
func message(id string, value any) model.StandardMessage {
	return model.StandardMessage{MessageID: id, RawMessageID: "raw-" + id, TenantID: "t", ProductID: "p", DeviceID: "d", MessageType: model.PropertyReport, Timestamp: 700000, Properties: map[string]any{"temperature": value}}
}

func TestPredicatesMapCurrentProduction(t *testing.T) {
	base := rule()
	base.Match = "all"
	for _, tc := range []struct {
		name       string
		conditions []model.RuleCondition
		match      string
		msg        model.StandardMessage
	}{
		{"temperature", base.Conditions, "all", message("m", 90)},
		{"missing", []model.RuleCondition{{Field: "missing", Operator: ">", Value: 0}}, "all", message("m", 90)},
		{"any", []model.RuleCondition{{Field: "temperature", Operator: "<", Value: 0}, {Field: "temperature", Operator: "gt", Value: 80}}, "ANY", message("m", "90")},
		{"boolean-numeric", []model.RuleCondition{{Field: "temperature", Operator: "eq", Value: 1}}, "all", message("m", true)},
		{"legacy-string-relational", []model.RuleCondition{{Field: "temperature", Operator: ">", Value: "smoke"}}, "all", message("m", "SMOKE")},
		{"legacy-default-operator", []model.RuleCondition{{Field: "temperature", Operator: "old-op", Value: 90}}, "all", message("m", 90)},
		{"contains", []model.RuleCondition{{Field: "temperature", Operator: "contains", Value: "smoke"}}, "all", message("m", "heavy smoke")},
		{"in", []model.RuleCondition{{Field: "temperature", Operator: "in", Value: []any{10, 90}}}, "all", message("m", 90)},
		{"nil-exists", []model.RuleCondition{{Field: "temperature", Operator: "exists"}}, "all", message("m", nil)},
		{"json-number", []model.RuleCondition{{Field: "temperature", Operator: "eq", Value: json.Number("90")}}, "all", message("m", 90)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := base
			current.Conditions, current.Match = tc.conditions, tc.match
			actual, err := eval.Match(current, tc.msg)
			if err != nil || actual != core.MatchRule(current, tc.msg) || eval.MatchConditions(current.Conditions, tc.msg) != core.MatchConditions(current.Conditions, tc.msg) {
				t.Fatal("pure predicate differs from current production", actual, err)
			}
		})
	}
	for _, path := range []string{"properties.temperature", "tags.type", "event.smoke", "temperature"} {
		msg := message("path", 90)
		msg.Tags, msg.Event = map[string]string{"type": "fire"}, map[string]any{"smoke": true}
		r := base
		r.Conditions = []model.RuleCondition{{Field: path, Operator: "exists"}}
		got, err := eval.Match(r, msg)
		if err != nil || got != core.MatchRule(r, msg) {
			t.Fatal(path, got, err)
		}
	}
	for _, scope := range []struct {
		tenant, product string
		enabled, covers bool
	}{{"t", "p", false, true}, {"other", "p", true, false}, {"t", "other", true, false}, {"", "", true, true}} {
		r := base
		r.TenantID, r.ProductID, r.Enabled = scope.tenant, scope.product, scope.enabled
		got, err := eval.Match(r, message("scope", 90))
		if err != nil || eval.Covers(r, message("scope", 90)) != scope.covers || got != core.MatchRule(r, message("scope", 90)) {
			t.Fatal(scope, got, err)
		}
	}
}

func TestExpressionMappingAndRecordedNonfatalErrors(t *testing.T) {
	msg := message("expr", 90)
	msg.Properties["a-b"], msg.Properties["a_b"], msg.Properties["a.b"] = 90.0, 20.0, 5.0
	msg.Event = map[string]any{"text": "Properties['a-b']"}
	for _, expression := range []string{`Properties["a-b"] > 80 && Properties["a_b"] < 30 && Properties["a.b"] == 5`, `Contains(Event["text"], "Properties['a-b']")`, `Contains("weekend; system { begin }", "end")`, `Properties["omitted"] == 0`, `Exists("properties.omitted")`} {
		r := rule()
		r.Expression = expression
		got, err := eval.Match(r, msg)
		want, currentErr := core.EvaluateGengineExpression(expression, msg)
		if got != want || (err == nil) != (currentErr == nil) || core.MatchRule(r, msg) != (got && err == nil) {
			t.Fatal(expression, got, want, err, currentErr)
		}
		if err := eval.ValidateExpression(expression); (err == nil) != (core.ValidateGengineExpression(expression) == nil) {
			t.Fatal("expression validation differs", expression, err)
		}
	}
	r := rule()
	r.Expression = `true; false`
	got, err := eval.Transition(r, message("invalid", 70), eval.RuleState{Pending: eval.Pending{Exists: true, Since: 100}, Alarm: model.Alarm{ID: "alarm", TenantID: "t", DeviceID: "d", RuleID: "r", Status: "ACTIVE", Version: 2}}, eval.StageTimes{RecoverAtMillis: instant(123456)})
	if err != nil || got.EvaluationError == "" || got.Matched || !got.RecoveryMatched || got.Alarm.Status != "RECOVERED" {
		t.Fatal("expression errors changed the current false-to-recovery branch", got, err)
	}
}

func TestDurationTransitionsProcessingClockAndReset(t *testing.T) {
	r := rule()
	r.DurationSeconds = 10
	state := eval.RuleState{NewAlarmID: "alarm"}
	first, err := eval.Transition(r, message("one", 90), state, eval.StageTimes{DurationAtSeconds: instant(1000)})
	if err != nil || first.DurationSatisfied || first.RuleAlarmHandled || first.PendingMutation != eval.SetPending || first.Pending.Since != 1000 || first.RecoveryMatched {
		t.Fatal(first, err)
	}
	state.Pending = first.Pending
	before, err := eval.Transition(r, message("two", 90), state, eval.StageTimes{DurationAtSeconds: instant(1009)})
	if err != nil || before.DurationSatisfied || before.PendingMutation != eval.KeepPending {
		t.Fatal(before, err)
	}
	boundary, err := eval.Transition(r, message("three", 90), state, eval.StageTimes{DurationAtSeconds: instant(1010), RaiseAtMillis: instant(1010999), RecoverAtMillis: instant(8888888)})
	if err != nil || !boundary.DurationSatisfied || !boundary.RuleAlarmHandled || boundary.Alarm.FirstTriggeredAt != 1010999 || boundary.Times.RecoverAtMillis != nil || boundary.Pending.Since != 1000 {
		t.Fatal("duration boundary or separate clocks changed", boundary, err)
	}
	nonmatch := message("normal", 70)
	nonmatch.Properties = nil // Neither trigger nor Recovery receives a field.
	reset, err := eval.Transition(r, nonmatch, state, eval.StageTimes{})
	if err != nil || reset.PendingMutation != eval.ClearPending || reset.Pending.Exists || reset.RecoveryMatched {
		t.Fatal(reset, err)
	}
	restart, err := eval.Transition(r, message("again", 90), eval.RuleState{Pending: reset.Pending, NewAlarmID: "new"}, eval.StageTimes{DurationAtSeconds: instant(9000)})
	if err != nil || restart.DurationSatisfied || restart.Pending.Since != 9000 {
		t.Fatal("nonmatch must establish a new pending after the gap", restart, err)
	}
	if _, err := eval.Transition(r, message("no-clock", 90), state, eval.StageTimes{}); !errors.Is(err, eval.ErrMissingClock) {
		t.Fatal("event time substituted for missing processing clock", err)
	}
	if got := eval.Duration(10, eval.Pending{Exists: true, Since: 1000}, 999); got.Satisfied || got.Pending.Since != 1000 {
		t.Fatal("processing clock reversal invented elapsed time", got)
	}
	zero := eval.Duration(10, eval.Pending{Exists: true, Since: 0}, 10)
	if !zero.Satisfied || zero.Mutation != eval.KeepPending {
		t.Fatal("epoch-zero pending was mistaken for missing", zero)
	}
}

func TestLifecycleIntentMatchesMemoryRepository(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	r := rule()
	state := eval.RuleState{NewAlarmID: "alarm"}
	for i, id := range []string{"m1", "m1", "m2", "m2"} {
		msg := message(id, 90) // Both trigger and Recovery match: trigger wins.
		before := state
		out, err := eval.Transition(r, msg, state, eval.StageTimes{RaiseAtMillis: instant(int64(1000 + i)), RecoverAtMillis: instant(99999)})
		if err != nil || !out.RuleAlarmHandled || out.RecoveryMatched || out.Alarm.Status != "ACTIVE" || !reflect.DeepEqual(before, state) {
			t.Fatal(out, err)
		}
		saved, created, err := repo.UpsertAlarm(ctx, eval.RuleAlarmCandidate(r, msg, "alarm", int64(1000+i)))
		if err != nil || created != out.Created || !sameAlarmBody(saved, out.Alarm) {
			t.Fatal("lifecycle differs from current store", saved, out.Alarm, err)
		}
		wantActions := 1
		if i == 1 {
			wantActions = 0
		}
		if len(out.Actions) != wantActions {
			t.Fatal("action intent semantics differ", i, out.Actions)
		}
		state.Alarm = saved
	}
	// V1 intentionally preserves the old TriggerID and exposes a partial retry
	// of m2 as another report/action. Completed-message idempotency is upstream.
	if state.Alarm.TriggerID != "m1" || state.Alarm.TriggerCount != 3 {
		t.Fatal("partial-retry V1 behavior silently repaired", state.Alarm)
	}
	state.Alarm.Status = "ACKED"
	out, err := eval.Transition(r, message("normal-acked", 70), state, eval.StageTimes{})
	if err != nil || out.WriteAlarm || out.Alarm.Status != "ACKED" {
		t.Fatal("rule auto-recovery expanded to ACKED", out, err)
	}
	state.Alarm.Status = "ACTIVE"
	out, err = eval.Transition(r, message("normal-active", 70), state, eval.StageTimes{RecoverAtMillis: instant(45678)})
	if err != nil || !out.WriteAlarm || out.Alarm.RecoveredAt != 45678 || out.Alarm.Status != "RECOVERED" || len(out.Actions) != 0 {
		t.Fatal(out, err)
	}
	r.Enabled = false
	out, err = eval.Transition(r, message("disabled-normal", 70), state, eval.StageTimes{RecoverAtMillis: instant(45679)})
	if err != nil || !out.RecoveryMatched || out.Alarm.Status != "RECOVERED" {
		t.Fatal("disabled rule recovery no longer covered", out, err)
	}
	if _, err := eval.Transition(r, message("wrong-device", 70), eval.RuleState{Alarm: model.Alarm{ID: "a", RuleID: "r", TenantID: "t", DeviceID: "foreign", Status: "ACTIVE"}}, eval.StageTimes{}); err == nil {
		t.Fatal("foreign lifecycle accepted")
	}
}

func TestConfidenceNaNUsesCurrentComparison(t *testing.T) {
	old := model.Alarm{ID: "a", Status: "ACTIVE", TriggerID: "m1", Confidence: 0.5}
	got, _, _ := eval.UpsertAlarm(model.Alarm{TriggerID: "m2", Confidence: math.NaN()}, old)
	if got.Confidence != old.Confidence {
		t.Fatal("NaN changed the current > comparison", got)
	}
}

func TestRoutesPreserveAssertionsAndCategoryRecovery(t *testing.T) {
	msg := message("alarm", 90)
	msg.MessageType = model.AlarmReport
	msg.Event = map[string]any{"fireAlarm": true, "level": "critical"}
	plain, err := eval.Route(msg, false)
	if err != nil || !plain.DeviceAssertion || plain.DirectRaise == nil || plain.DirectRaise.AlarmType != "FIRE" || plain.DirectRaise.Level != "CRITICAL" {
		t.Fatal(plain, err)
	}
	handled, err := eval.Route(msg, true)
	if err != nil || !handled.DeviceAssertion || handled.DirectRaise != nil {
		t.Fatal("classification lost assertion or added direct alarm", handled, err)
	}
	msg.Event["components"] = []model.ComponentStatus{{ID: "controller-A", Alarms: map[string]bool{"FIRE": true, "DEVICE_FAULT": false}}, {ID: "controller-B", Alarms: map[string]bool{"FIRE": true}}}
	parts, err := eval.Route(msg, false)
	if err != nil || !parts.DeviceAssertion || parts.DirectRaise != nil || len(parts.Components) != 3 || parts.Components[0].AlarmType != "DEVICE_FAULT" || parts.Components[1].AlarmType != "FIRE" || parts.Components[1].RuleID == parts.Components[2].RuleID {
		t.Fatal(parts, err)
	}
	msg.Event["components"] = []model.ComponentStatus{}
	if _, err := eval.Route(msg, false); err == nil {
		t.Fatal("invalid components became a direct fallback")
	}
	for _, tc := range []struct {
		typeCode model.MessageType
		property map[string]any
		event    map[string]any
		recover  bool
	}{
		{model.StateChange, map[string]any{"connectionStatus": "CONNECTED"}, nil, false},
		{model.PropertyReport, map[string]any{"fireAlarm": false}, nil, true},
		{model.PropertyReport, map[string]any{"fireAlarm": false, "fault": true}, nil, false},
		{model.StateChange, map[string]any{"fireAlarm": false}, map[string]any{"type": "COMPONENT_STATUS", "objects": []any{}}, false},
		{model.EventReport, map[string]any{"fireAlarm": false}, nil, false},
	} {
		current := message("normal", 0)
		current.MessageType, current.Properties, current.Event = tc.typeCode, tc.property, tc.event
		out, err := eval.Route(current, false)
		if err != nil || out.DirectRecover != tc.recover || out.DirectRaise != nil {
			t.Fatal(tc, out, err)
		}
	}
	if !eval.DirectTypeCleared(model.StandardMessage{Properties: map[string]any{"fireAlarm": false}}, "FIRE") || eval.DirectTypeCleared(model.StandardMessage{Properties: map[string]any{"fireAlarm": false}}, "DEVICE_FAULT") {
		t.Fatal("one normal flag recovered another category")
	}
}

func TestComponentTransitionMapsRepositoryWatermarksAndRetries(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	var previous model.Alarm
	var watermark model.ComponentAlarmState
	for i, tc := range []struct {
		id     string
		at     int64
		active bool
	}{{"m1", 1000, true}, {"m1", 1000, true}, {"same-normal", 1000, false}, {"m2", 2000, false}, {"late", 1500, true}, {"m3", 2000, true}, {"m4", 3000, false}} {
		candidate := model.Alarm{ID: "cycle-" + tc.id, TenantID: "t", DeviceID: "d", RuleID: "device-report:FIRE:component:A", TriggerID: tc.id, AlarmType: "FIRE", ComponentID: "A", Status: "ACTIVE", FirstTriggeredAt: int64(i), LastTriggeredAt: int64(i), TriggerCount: 1}
		next := model.ComponentAlarmState{MessageID: tc.id, Timestamp: tc.at, Active: tc.active}
		out := eval.ComponentTransition(candidate, previous, next, watermark)
		saved, event, err := repo.ApplyComponentAlarm(ctx, candidate, next)
		if err != nil || !sameAlarmBody(saved, out.Alarm) || event != out.Event {
			t.Fatal("component pure transition differs from persistent abstraction", i, saved, out, err)
		}
		if i == 1 && (!out.ReplayEvent || out.Applied) {
			t.Fatal("identical unfinished attempt lost its saved event", out)
		}
		if (i == 2 || i == 4) && out.Applied {
			t.Fatal("equal-time normal/late report overrode watermark", out)
		}
		previous, watermark = saved, out.Watermark
		if i == 5 {
			previous.Status = "ACKED"
			if err := repo.UpdateAlarm(ctx, previous); err != nil {
				t.Fatal(err)
			}
			previous, _ = repo.GetAlarm(ctx, "t", previous.ID)
		}
	}
	if previous.Status != "RECOVERED" {
		t.Fatal("component ACKED recovery was incorrectly restricted to rule ACTIVE policy", previous)
	}
}
