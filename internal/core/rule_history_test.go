package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"iot-platform/internal/rulelab/eval"
)

func historyRule() model.AlarmRule {
	return model.AlarmRule{ID: "r", TenantID: "tenant-a", ProductID: "sensor", Name: "高温", AlarmType: "FIRE", Level: "HIGH", Enabled: true, Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}, Recovery: []model.RuleCondition{{Field: "temperature", Operator: "<", Value: 50}}}
}
func historyMessage(id string, value int) []byte {
	raw, _ := json.Marshal(model.StandardMessage{MessageID: id, RawMessageID: "raw-" + id, TenantID: "tenant-a", ProductID: "sensor", DeviceID: "device-a", MessageType: model.PropertyReport, Timestamp: 1000000, Properties: map[string]any{"temperature": value}})
	return raw
}
func historyCurrent(t *testing.T, repo ports.Repository) model.AlarmRuleRevision {
	t.Helper()
	rows, _, err := repo.ListRuleRevisions(context.Background(), "tenant-a", "r", 20, 0)
	if err != nil || len(rows) == 0 {
		t.Fatal(rows, err)
	}
	return rows[0]
}
func publishHistory(t *testing.T, repo ports.Repository, rule model.AlarmRule, version int) model.AlarmRuleRevision {
	t.Helper()
	v, err := repo.PublishRule(context.Background(), model.RulePublishRequest{Rule: rule, ExpectedBaselineVersion: version, Reason: "isolated comparison", Actor: "operator", ExperimentID: "experiment", SemanticsVersion: eval.RevisionV2})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestRuleHistoryVersionPendingResetRecoveryAndImmutableCAS(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	rule := historyRule()
	rule.DurationSeconds = 10
	first := publishHistory(t, repo, rule, 0)
	clock := &ruleTestClock{now: time.Unix(1000, 0)}
	engine := newRuleTestEngine(t, repo, clock)
	if err := engine.handleStandard(ctx, historyMessage("pending-v1", 90)); err != nil {
		t.Fatal(err)
	}
	rule.Conditions = []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 70}}
	second := publishHistory(t, repo, rule, first.Version)
	clock.now = time.Unix(1011, 0)
	if err := engine.handleStandard(ctx, historyMessage("pending-v2", 90)); err != nil {
		t.Fatal(err)
	}
	if alarms, _ := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", Status: "ACTIVE"}); len(alarms) != 0 {
		t.Fatal("new revision inherited prior duration", alarms)
	}
	clock.now = time.Unix(1021, 0)
	if err := engine.handleStandard(ctx, historyMessage("raise-v2", 90)); err != nil {
		t.Fatal(err)
	}
	alarms, _ := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", Status: "ACTIVE"})
	if len(alarms) != 1 || alarms[0].CreatedRuleRevision != second.ID {
		t.Fatal(alarms)
	}
	thirdRule := rule
	thirdRule.DurationSeconds = 0
	thirdRule.Recovery = []model.RuleCondition{{Field: "temperature", Operator: "<", Value: 80}}
	third := publishHistory(t, repo, thirdRule, second.Version)
	if err := engine.handleStandard(ctx, historyMessage("new-report", 75)); err != nil {
		t.Fatal(err)
	}
	alarms, _ = repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", Status: "ACTIVE"})
	if len(alarms) != 1 || alarms[0].ID == "" || alarms[0].CreatedRuleRevision != second.ID || alarms[0].TriggerRuleRevision != third.ID || alarms[0].TriggerCount != 2 {
		t.Fatal("producing revision changed or new active identity created", alarms)
	}
	if err := engine.handleStandard(ctx, historyMessage("current-recovery-only", 60)); err != nil {
		t.Fatal(err)
	}
	if alarms, _ = repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", Status: "ACTIVE"}); len(alarms) != 1 {
		t.Fatal("current revision recovered a prior revision lifecycle", alarms)
	}
	if err := engine.handleStandard(ctx, historyMessage("producing-recovery", 40)); err != nil {
		t.Fatal(err)
	}
	if alarms, _ = repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", Status: "RECOVERED"}); len(alarms) != 1 || alarms[0].CreatedRuleRevision != second.ID {
		t.Fatal(alarms)
	}
	if _, err := repo.PublishRule(ctx, model.RulePublishRequest{Rule: rule, ExpectedBaselineVersion: second.Version, Reason: "stale", SemanticsVersion: eval.RevisionV2}); !errors.Is(err, model.ErrRuleConflict) {
		t.Fatal("stale publish overwrote current", err)
	}
	unchanged, _ := repo.GetRuleRevision(ctx, "tenant-a", first.ID)
	if unchanged.Hash != first.Hash || unchanged.Rule.DurationSeconds != 10 {
		t.Fatal("immutable revision was overwritten", unchanged)
	}
	if _, err := repo.GetRuleRevision(ctx, "other", first.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("tenant history leaked", err)
	}
	activations, total, err := repo.ListRuleActivations(ctx, "tenant-a", "r", 20, 0)
	if err != nil || total != 3 || activations[0].Until != nil || activations[1].Until == nil || *activations[1].Until != activations[0].Since {
		t.Fatal(activations, err)
	}
}

func TestRuleHistoryDeleteRetainsProducingRecoveryAndACKED(t *testing.T) {
	for _, acked := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "acked"}[acked], func(t *testing.T) {
			ctx := context.Background()
			repo := memory.NewRepository()
			rule := historyRule()
			first := publishHistory(t, repo, rule, 0)
			engine := newRuleTestEngine(t, repo, &ruleTestClock{now: time.Unix(1000, 0)})
			if err := engine.handleStandard(ctx, historyMessage("raise", 90)); err != nil {
				t.Fatal(err)
			}
			alarms, _ := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a", Status: "ACTIVE"})
			if acked {
				alarms[0].Status = "ACKED"
				if err := repo.UpdateAlarm(ctx, alarms[0]); err != nil {
					t.Fatal(err)
				}
			}
			if err := engine.DeleteRule(ctx, "tenant-a", "r"); err != nil {
				t.Fatal(err)
			}
			stored, _ := repo.GetAlarm(ctx, "tenant-a", alarms[0].ID)
			if stored.Status != map[bool]string{false: "ACTIVE", true: "ACKED"}[acked] || stored.CreatedRuleRevision != first.ID {
				t.Fatal("delete silently resolved lifecycle", stored)
			}
			if err := engine.handleStandard(ctx, historyMessage("normal", 40)); err != nil {
				t.Fatal(err)
			}
			stored, _ = repo.GetAlarm(ctx, "tenant-a", alarms[0].ID)
			if stored.Status != map[bool]string{false: "RECOVERED", true: "ACKED"}[acked] {
				t.Fatal("producing recovery changed ACTIVE/ACKED semantics", stored)
			}
		})
	}
}

type stagedRuleClock struct {
	values []time.Time
	last   time.Time
}

func (c *stagedRuleClock) Now() time.Time {
	if len(c.values) > 0 {
		c.last = c.values[0]
		c.values = c.values[1:]
	}
	return c.last
}
func TestRuleTraceActualStageClocksAndPartialRetryQuality(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	rule := historyRule()
	rule.DurationSeconds = 10
	publishHistory(t, repo, rule, 0)
	engine := newRuleTestEngine(t, repo, &ruleTestClock{now: time.Unix(1000, 0)})
	if err := engine.handleStandard(ctx, historyMessage("first", 90)); err != nil {
		t.Fatal(err)
	}
	engine.Clock = &stagedRuleClock{values: []time.Time{time.Unix(1010, 900000000), time.Unix(1011, 200000000)}}
	if err := engine.handleStandard(ctx, historyMessage("second", 90)); err != nil {
		t.Fatal(err)
	}
	traces, total, err := repo.ListRuleEvaluationTraces(ctx, "tenant-a", model.RuleTraceFilter{DeviceIDs: []string{"device-a"}, MessageIDs: []string{"second"}, Limit: 20})
	if err != nil || total != 1 || traces[0].Status != "COMPLETE" || traces[0].ReproductionQuality != "EXACT" || len(traces[0].Steps) != 1 {
		t.Fatal(traces, err)
	}
	step := traces[0].Steps[0]
	if step.Times.DurationAtSeconds == nil || *step.Times.DurationAtSeconds != 1010 || step.Times.RaiseAtMillis == nil || *step.Times.RaiseAtMillis != 1011200 || step.Times.RecoverAtMillis != nil || step.Before.Pending.Since != 1000 || !step.Before.Pending.Exists {
		t.Fatal("actual multistage clocks collapsed or fabricated", step)
	}
	if _, _, err := repo.ListRuleEvaluationTraces(ctx, "tenant-a", model.RuleTraceFilter{}); !errors.Is(err, model.ErrRuleHistoryInvalid) {
		t.Fatal("unscoped trace read allowed", err)
	}
	base := memory.NewRepository()
	if err = base.SaveRule(ctx, historyRule()); err != nil {
		t.Fatal(err)
	}
	failing := &failFirstStateRepository{Repository: base, fail: true}
	retry := newRuleTestEngine(t, failing, &ruleTestClock{now: time.Unix(1000, 0)})
	if err = retry.handleStandard(ctx, historyMessage("retry", 90)); err == nil {
		t.Fatal("injected failure disappeared")
	}
	if err = retry.handleStandard(ctx, historyMessage("retry", 90)); err != nil {
		t.Fatal(err)
	}
	traces, total, err = base.ListRuleEvaluationTraces(ctx, "tenant-a", model.RuleTraceFilter{DeviceIDs: []string{"device-a"}, MessageIDs: []string{"retry"}})
	if err != nil || total != 2 {
		t.Fatal(traces, err)
	}
	var incomplete, complete bool
	for _, trace := range traces {
		if trace.ClaimToken == 1 {
			incomplete = trace.Status == "IN_PROGRESS" && trace.ReproductionQuality == "INCOMPLETE_TRACE" && trace.FinishedAt == 0
		} else if trace.ClaimToken == 2 {
			complete = trace.Status == "COMPLETE" && len(trace.Steps) == 1 && trace.Steps[0].Before.Alarm.ID != ""
		}
	}
	if !incomplete || !complete {
		t.Fatal("partial attempt was promoted to exact completion", traces)
	}
}

func TestRuleTraceCompleteDirectAndComponentRouting(t *testing.T) {
	for _, kind := range []string{"direct", "component"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			base := memory.NewRepository()
			repo := &failFirstStateRepository{Repository: base, fail: true}
			engine := newRuleTestEngine(t, repo, &ruleTestClock{now: time.Unix(1000, 0)})
			message := func(id string, at int64, active bool) []byte {
				msg := model.StandardMessage{TenantID: "tenant-a", DeviceID: "device-a", ProductID: "sensor", MessageID: id, RawMessageID: "raw-" + id, Timestamp: at, MessageType: model.AlarmReport, Properties: map[string]any{"fireAlarm": active}, Event: map[string]any{"alarmType": "FIRE"}}
				if !active {
					msg.MessageType = model.PropertyReport
				}
				if kind == "component" {
					msg.MessageType = model.StateChange
					msg.Event["components"] = []model.ComponentStatus{{ID: "c", Timestamp: at, Alarms: map[string]bool{"FIRE": active}}}
				}
				return mustJSON(msg)
			}
			if err := engine.handleStandard(ctx, message("route", 1000, true)); err == nil {
				t.Fatal("partial route injection failed")
			}
			if err := engine.handleStandard(ctx, message("route", 1000, true)); err != nil {
				t.Fatal(err)
			}
			if err := engine.handleStandard(ctx, message("normal", 2000, false)); err != nil {
				t.Fatal(err)
			}
			traces, total, err := base.ListRuleEvaluationTraces(ctx, "tenant-a", model.RuleTraceFilter{DeviceIDs: []string{"device-a"}})
			if err != nil || total != 3 {
				t.Fatal(traces, err)
			}
			for _, trace := range traces {
				if trace.MessageID == "route" && trace.ClaimToken == 1 {
					if trace.Status != "IN_PROGRESS" || trace.ReproductionQuality != "INCOMPLETE_TRACE" || len(trace.RoutingSteps) != 1 {
						t.Fatal("partial route claimed complete", trace)
					}
					continue
				}
				if trace.Status != "COMPLETE" || trace.ReproductionQuality != "EXACT" || len(trace.RoutingSteps) != 1 || !trace.Routing.Committed {
					t.Fatal("complete route lost atomic reproduction evidence", trace)
				}
				step := trace.RoutingSteps[0]
				if kind == "component" {
					if step.Kind != "COMPONENT" || step.Times.ComponentAtMillis == nil || *step.Times.ComponentAtMillis != 1000000 || step.PreviousWatermark == nil || step.Watermark == nil {
						t.Fatal(step)
					}
				} else if trace.MessageID == "normal" {
					if step.Kind != "DIRECT_RECOVER" || step.Times.RecoverAtMillis == nil || step.Before.Alarm.Status != "ACTIVE" || step.After.Alarm.Status != "RECOVERED" {
						t.Fatal(step)
					}
				} else if step.Kind != "DIRECT_RAISE" || step.Times.RaiseAtMillis == nil {
					t.Fatal(step)
				}
			}
		})
	}
}

func TestRuleRollbackReferenceMatchesImmutablePolicy(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	rule := model.AlarmRule{TenantID: "t", ID: "r", Name: "one", Enabled: true}
	first, e := repo.PublishRule(ctx, model.RulePublishRequest{Rule: rule, Actor: "a", Reason: "first", SemanticsVersion: eval.RevisionV2})
	if e != nil {
		t.Fatal(e)
	}
	rule.Name = "two"
	second, e := repo.PublishRule(ctx, model.RulePublishRequest{Rule: rule, ExpectedBaselineVersion: first.Version, Actor: "a", Reason: "second", SemanticsVersion: eval.RevisionV2})
	if e != nil {
		t.Fatal(e)
	}
	forged := first.Rule
	forged.Name = "forged"
	if _, e = repo.PublishRule(ctx, model.RulePublishRequest{Rule: forged, ExpectedBaselineVersion: second.Version, RollbackFrom: first.ID, Actor: "a", Reason: "fake", SemanticsVersion: eval.RevisionV2}); !errors.Is(e, model.ErrRuleHistoryInvalid) {
		t.Fatal(e)
	}
	restored := first.Rule
	restored.Version = 999
	restored.CreatedAt = 123
	restored.UpdatedAt = 456
	third, e := repo.PublishRule(ctx, model.RulePublishRequest{Rule: restored, ExpectedBaselineVersion: second.Version, RollbackFrom: first.ID, Actor: "a", Reason: "verified rollback", SemanticsVersion: eval.RevisionV2})
	if e != nil || third.Version != 3 || third.RollbackFrom != first.ID || third.Rule.Name != "one" || third.Rule.CreatedAt != first.Rule.CreatedAt {
		t.Fatal(third, e)
	}
}
