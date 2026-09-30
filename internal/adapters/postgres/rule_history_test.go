package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"iot-platform/internal/model"
	"iot-platform/internal/rulelab/eval"
)

func ruleHistoryFixture(t *testing.T, r *Repository) (model.AlarmRuleRevision, model.StandardMessage, model.RuleTraceBinding) {
	t.Helper()
	ctx := context.Background()
	revision, err := r.PublishRule(ctx, model.RulePublishRequest{Rule: model.AlarmRule{TenantID: "t", ID: "r", Name: "rule", Enabled: true, AlarmType: "FIRE", Level: "HIGH", Conditions: []model.RuleCondition{{Field: "temperature", Operator: ">", Value: 80}}}, Reason: "test fixture", Actor: "operator", SemanticsVersion: eval.RevisionV2})
	if err != nil {
		t.Fatal(err)
	}
	msg := model.StandardMessage{TenantID: "t", DeviceID: "d", ProductID: "p", MessageID: "m", RawMessageID: "raw-m", Timestamp: 1000, MessageType: model.PropertyReport, Properties: map[string]any{"temperature": 90}}
	claim, err := r.ClaimStandardMessage(ctx, msg, "worker", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	binding := model.RuleTraceBinding{TenantID: "t", MessageID: "m", ClaimToken: claim.Token}
	trace := model.RuleEvaluationTrace{RuleTraceBinding: binding, DeviceID: "d", ProductID: "p", MessageTimestamp: 1000, MessageHash: "fixed-input-hash", ClaimOwner: "worker", Rules: []model.AlarmRuleRevision{revision}, RuleSetHash: model.RuleSetHash([]model.AlarmRuleRevision{revision}), SemanticsVersion: eval.RevisionV2}
	if err = r.BeginRuleEvaluationTrace(ctx, trace); err != nil {
		t.Fatal(err)
	}
	return revision, msg, binding
}
func ruleHistoryRaise(revision model.AlarmRuleRevision, msg model.StandardMessage) func(model.RuleEvaluationState) (model.RuleEvaluationStep, error) {
	return func(state model.RuleEvaluationState) (model.RuleEvaluationStep, error) {
		now := int64(2000)
		decision, err := eval.TransitionRevision(revision, msg, eval.RuleState{Pending: state.Pending, Alarm: state.Alarm, NewAlarmID: "alarm"}, state.RecoveryRevision, eval.StageTimes{RaiseAtMillis: &now})
		return model.RuleEvaluationStep{RuleRevisionID: revision.ID, SemanticsVersion: decision.SemanticsVersion, Covered: decision.Covered, Matched: decision.Matched, RecoveryMatched: decision.RecoveryMatched, DurationSatisfied: decision.DurationSatisfied, Pending: decision.Pending, PendingMutation: decision.PendingMutation, RuleAlarmHandled: decision.RuleAlarmHandled, Alarm: decision.Alarm, WriteAlarm: decision.WriteAlarm, Created: decision.Created, Event: decision.Event, Actions: decision.Actions, Times: decision.Times}, err
	}
}

func TestRuleHistorySQLCASImmutableAndAtomicTraceCompletion(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	revision, msg, binding := ruleHistoryFixture(t, r)
	pool, err := pgxpool.NewWithConfig(ctx, r.pool.Config().Copy())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	other := &Repository{pool: pool}
	var wg sync.WaitGroup
	var mu sync.Mutex
	success, conflict := 0, 0
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			target := r
			if index%2 == 1 {
				target = other
			}
			rule := revision.Rule
			rule.Name = "candidate"
			_, err := target.PublishRule(ctx, model.RulePublishRequest{Rule: rule, ExpectedBaselineVersion: revision.Version, Reason: "concurrent candidate", Actor: "operator", ExperimentID: "experiment", SemanticsVersion: eval.RevisionV2})
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				success++
			} else if errors.Is(err, model.ErrRuleConflict) {
				conflict++
			} else {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if success != 1 || conflict != 7 {
		t.Fatal(success, conflict)
	}
	old, err := other.GetRuleRevision(ctx, "t", revision.ID)
	if err != nil || old.Hash != revision.Hash || old.Rule.Name != "rule" {
		t.Fatal(old, err)
	}
	if _, err = other.GetRuleRevision(ctx, "other", revision.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("cross-tenant history", err)
	}
	// An in-flight message uses its fixed, actually selected revision, even if a
	// newer version activates before the stage commits.
	step, err := other.CommitRuleEvaluationStep(ctx, binding, 0, ruleHistoryRaise(revision, msg))
	if err != nil || !step.Created || step.Alarm.CreatedRuleRevision != revision.ID {
		t.Fatal(step, err)
	}
	traces, total, err := r.ListRuleEvaluationTraces(ctx, "t", model.RuleTraceFilter{DeviceIDs: []string{"d"}, MessageIDs: []string{"m"}})
	if err != nil || total != 1 || traces[0].Status != "IN_PROGRESS" || traces[0].ReproductionQuality != "INCOMPLETE_TRACE" || len(traces[0].Steps) != 1 {
		t.Fatal(traces, err)
	}
	if err = r.RecordRuleRoutingTrace(ctx, binding, model.RuleRoutingTrace{RuleAlarmHandled: true}); err != nil {
		t.Fatal(err)
	}
	if err = other.MarkStandardMessageProcessed(ctx, "t", "m", binding.ClaimToken); err != nil {
		t.Fatal(err)
	}
	traces, _, err = r.ListRuleEvaluationTraces(ctx, "t", model.RuleTraceFilter{DeviceIDs: []string{"d"}})
	if err != nil || traces[0].Status != "COMPLETE" || traces[0].ReproductionQuality != "EXACT" || traces[0].FinishedAt == 0 || traces[0].Steps[0].Times.RaiseAtMillis == nil {
		t.Fatal(traces, err)
	}
	if _, _, err = r.ListRuleEvaluationTraces(ctx, "t", model.RuleTraceFilter{}); !errors.Is(err, model.ErrRuleHistoryInvalid) {
		t.Fatal("missing scope accepted", err)
	}
	if _, err = other.CommitRuleEvaluationStep(ctx, binding, 0, ruleHistoryRaise(revision, msg)); !errors.Is(err, model.ErrStaleClaim) {
		t.Fatal("completed trace mutated", err)
	}
}

func TestRuleHistorySQLTraceFailureRollsBackAlarmAndProcessedFence(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	revision, msg, binding := ruleHistoryFixture(t, r)
	observation := model.AlarmObservation{TenantID: msg.TenantID, DeviceID: msg.DeviceID, AlarmType: revision.Rule.AlarmType, OriginKind: "RULE_LIFECYCLE", SignalKey: "rule:" + revision.RuleID, SourceSystem: "STANDARD_MESSAGE", SourceEventID: msg.MessageID, StandardMessageID: msg.MessageID, RawMessageID: msg.RawMessageID, EventIndex: "rule:" + revision.RuleID, SourceInputHash: model.ObservationHash(msg), RuleID: revision.RuleID, RuleVersion: revision.Version, ConditionHash: revision.Hash, FactKind: "ASSERT", EventAt: msg.Timestamp, EvaluationAt: 2000, RecordedAt: 2000, Payload: map[string]any{"message": msg}}
	ctx = model.WithAlarmObservationCapture(ctx, &model.AlarmObservationCapture{Observation: &observation})
	if _, err := r.pool.Exec(ctx, `CREATE FUNCTION fail_rule_trace() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected trace commit failure'; END $$; CREATE TRIGGER fail_rule_trace BEFORE UPDATE ON rule_evaluation_trace FOR EACH ROW EXECUTE FUNCTION fail_rule_trace()`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CommitRuleEvaluationStep(ctx, binding, 0, ruleHistoryRaise(revision, msg)); err == nil {
		t.Fatal("trace failure accepted")
	}
	var alarms, outbox, facts, signals, sources int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM alarm_record`).Scan(&alarms); err != nil {
		t.Fatal(err)
	}
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM event_outbox`).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	for table, target := range map[string]*int{"alarm_observation": &facts, "alarm_signal_state": &signals, "alarm_governance_source_version": &sources} {
		if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if alarms != 0 || outbox != 0 || facts != 0 || signals != 0 || sources != 0 {
		t.Fatal("alarm/observation/signal/source event committed without trace", alarms, outbox, facts, signals, sources)
	}
	if _, err := r.pool.Exec(ctx, `DROP TRIGGER fail_rule_trace ON rule_evaluation_trace`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.CommitRuleEvaluationStep(ctx, binding, 0, ruleHistoryRaise(revision, msg)); err != nil {
		t.Fatal(err)
	}
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM alarm_observation WHERE tenant_id='t' AND alarm_id='alarm'`).Scan(&facts); err != nil || facts != 1 {
		t.Fatal("successful rule step lost same-transaction fact", facts, err)
	}
	if err := r.RecordRuleRoutingTrace(ctx, binding, model.RuleRoutingTrace{RuleAlarmHandled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.pool.Exec(ctx, `CREATE TRIGGER fail_rule_trace BEFORE UPDATE ON rule_evaluation_trace FOR EACH ROW EXECUTE FUNCTION fail_rule_trace()`); err != nil {
		t.Fatal(err)
	}
	if err := r.MarkStandardMessageProcessed(ctx, "t", "m", binding.ClaimToken); err == nil {
		t.Fatal("processed marker accepted failed trace finish")
	}
	var processed int64
	if err := r.pool.QueryRow(ctx, `SELECT processed_at FROM standard_message WHERE tenant_id='t' AND message_id='m'`).Scan(&processed); err != nil || processed != 0 {
		t.Fatal("processed marker escaped trace rollback", processed, err)
	}
	if _, err := r.pool.Exec(ctx, `DROP TRIGGER fail_rule_trace ON rule_evaluation_trace`); err != nil {
		t.Fatal(err)
	}
	claim, err := r.ClaimStandardMessage(ctx, msg, "worker", time.Minute)
	if err != nil || claim.Token <= binding.ClaimToken {
		t.Fatal(claim, err)
	}
	if err = r.MarkStandardMessageProcessed(ctx, "t", "m", binding.ClaimToken); !errors.Is(err, model.ErrStaleClaim) {
		t.Fatal("old attempt completion not fenced", err)
	}
	traces, _, err := r.ListRuleEvaluationTraces(ctx, "t", model.RuleTraceFilter{DeviceIDs: []string{"d"}})
	if err != nil || traces[0].Status != "IN_PROGRESS" || traces[0].ReproductionQuality != "INCOMPLETE_TRACE" {
		t.Fatal("unfinished attempt falsely exact", traces, err)
	}
}

func TestRuleHistorySQLDirectComponentTraceRollbackAndExactState(t *testing.T) {
	for _, kind := range []string{"direct", "component"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			r := testRepository(t)
			msg := model.StandardMessage{TenantID: "t", DeviceID: "d", ProductID: "p", MessageID: "route", Timestamp: 1000, MessageType: model.AlarmReport}
			claim, err := r.ClaimStandardMessage(ctx, msg, "worker", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			binding := model.RuleTraceBinding{TenantID: "t", MessageID: "route", ClaimToken: claim.Token}
			trace := model.RuleEvaluationTrace{RuleTraceBinding: binding, DeviceID: "d", ProductID: "p", MessageTimestamp: 1000, MessageHash: "fixed", ClaimOwner: "worker", Rules: []model.AlarmRuleRevision{}, RuleSetHash: model.RuleSetHash(nil), SemanticsVersion: eval.RevisionV2}
			if err = r.BeginRuleEvaluationTrace(ctx, trace); err != nil {
				t.Fatal(err)
			}
			bound := model.WithRuleTraceBinding(ctx, binding)
			candidate := model.Alarm{TenantID: "t", ID: "alarm", DeviceID: "d", RuleID: "device-report:FIRE", TriggerID: "route", Status: "ACTIVE", Source: "device", AlarmType: "FIRE", AlarmLevel: "HIGH", FirstTriggeredAt: 2000, LastTriggeredAt: 2000, TriggerCount: 1}
			if kind == "component" {
				candidate.RuleID = "device-report:FIRE:component:c"
				candidate.ComponentID = "c"
			}
			apply := func() (model.Alarm, error) {
				if kind == "component" {
					saved, _, err := r.ApplyComponentAlarm(bound, candidate, model.ComponentAlarmState{Timestamp: 1000, MessageID: "route", Active: true})
					return saved, err
				}
				saved, _, err := r.UpsertAlarm(bound, candidate)
				return saved, err
			}
			if _, err = r.pool.Exec(ctx, `CREATE FUNCTION fail_route_trace() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected route trace failure'; END $$; CREATE TRIGGER fail_route_trace BEFORE UPDATE ON rule_evaluation_trace FOR EACH ROW EXECUTE FUNCTION fail_route_trace()`); err != nil {
				t.Fatal(err)
			}
			if _, err = apply(); err == nil {
				t.Fatal("route trace failure accepted")
			}
			var alarmCount, outboxCount, watermarks int
			if err = r.pool.QueryRow(ctx, `SELECT count(*) FROM alarm_record`).Scan(&alarmCount); err != nil {
				t.Fatal(err)
			}
			if err = r.pool.QueryRow(ctx, `SELECT count(*) FROM event_outbox`).Scan(&outboxCount); err != nil {
				t.Fatal(err)
			}
			if err = r.pool.QueryRow(ctx, `SELECT count(*) FROM component_alarm_state`).Scan(&watermarks); err != nil {
				t.Fatal(err)
			}
			if alarmCount != 0 || outboxCount != 0 || watermarks != 0 {
				t.Fatal("route escaped trace transaction", alarmCount, outboxCount, watermarks)
			}
			if _, err = r.pool.Exec(ctx, `DROP TRIGGER fail_route_trace ON rule_evaluation_trace`); err != nil {
				t.Fatal(err)
			}
			saved, err := apply()
			if err != nil {
				t.Fatal(err)
			}
			routing := model.RuleRoutingTrace{DeviceAssertion: true, ExpectedDirectRaise: kind == "direct"}
			if kind == "component" {
				routing.ExpectedComponents = 1
				routing.HasComponents = true
			}
			if err = r.RecordRuleRoutingTrace(ctx, binding, routing); err != nil {
				t.Fatal(err)
			}
			if err = r.MarkStandardMessageProcessed(ctx, "t", "route", binding.ClaimToken); err != nil {
				t.Fatal(err)
			}
			traces, _, err := r.ListRuleEvaluationTraces(ctx, "t", model.RuleTraceFilter{DeviceIDs: []string{"d"}})
			if err != nil || len(traces) != 1 || traces[0].ReproductionQuality != "EXACT" || len(traces[0].RoutingSteps) != 1 {
				t.Fatal("route exact evidence missing", traces, err)
			}
			msg.MessageID = "normal"
			msg.Timestamp = 2000
			msg.MessageType = model.StateChange
			claim, err = r.ClaimStandardMessage(ctx, msg, "worker", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			binding.MessageID = "normal"
			binding.ClaimToken = claim.Token
			trace.RuleTraceBinding = binding
			trace.MessageTimestamp = 2000
			if err = r.BeginRuleEvaluationTrace(ctx, trace); err != nil {
				t.Fatal(err)
			}
			bound = model.WithRuleTraceBinding(ctx, binding)
			if kind == "component" {
				candidate.LastTriggeredAt = 3000
				candidate.TriggerID = "normal"
				if _, _, err = r.ApplyComponentAlarm(bound, candidate, model.ComponentAlarmState{Timestamp: 2000, MessageID: "normal", Active: false}); err != nil {
					t.Fatal(err)
				}
			} else {
				o := pgObservation("normal", "CLEAR", 2000)
				o.EvaluationAt = 3000
				if recovered, err := r.RecoverAlarmSignal(bound, o, saved.RuleID); err != nil || len(recovered) != 1 {
					t.Fatal(recovered, err)
				}
			}
			routing.DeviceAssertion = false
			routing.ExpectedDirectRaise = false
			if err = r.RecordRuleRoutingTrace(ctx, binding, routing); err != nil {
				t.Fatal(err)
			}
			if err = r.MarkStandardMessageProcessed(ctx, "t", "normal", binding.ClaimToken); err != nil {
				t.Fatal(err)
			}
			traces, _, err = r.ListRuleEvaluationTraces(ctx, "t", model.RuleTraceFilter{DeviceIDs: []string{"d"}, MessageIDs: []string{"normal"}})
			if err != nil || len(traces) != 1 || traces[0].ReproductionQuality != "EXACT" || len(traces[0].RoutingSteps) != 1 || traces[0].RoutingSteps[0].After.Alarm.Status != "RECOVERED" {
				t.Fatal("route recovery stage lost", traces, err)
			}
		})
	}
}

func TestRuleHistorySQLRollbackSourceAndDeletedHead(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	first, _, _ := ruleHistoryFixture(t, r)
	next := first.Rule
	next.Name = "two"
	second, e := r.PublishRule(ctx, model.RulePublishRequest{Rule: next, ExpectedBaselineVersion: first.Version, Reason: "change", Actor: "a", SemanticsVersion: eval.RevisionV2})
	if e != nil {
		t.Fatal(e)
	}
	forged := first.Rule
	forged.Name = "forged"
	if _, e = r.PublishRule(ctx, model.RulePublishRequest{Rule: forged, ExpectedBaselineVersion: second.Version, RollbackFrom: first.ID, Reason: "fake", Actor: "a", SemanticsVersion: eval.RevisionV2}); !errors.Is(e, model.ErrRuleHistoryInvalid) {
		t.Fatal(e)
	}
	restored, e := r.PublishRule(ctx, model.RulePublishRequest{Rule: first.Rule, ExpectedBaselineVersion: second.Version, RollbackFrom: first.ID, Reason: "verified", Actor: "a", SemanticsVersion: eval.RevisionV2})
	if e != nil || restored.Version != 3 {
		t.Fatal(restored, e)
	}
	if e = r.DeleteRule(ctx, "t", first.RuleID); e != nil {
		t.Fatal(e)
	}
	if e = r.DeleteRule(ctx, "t", first.RuleID); !errors.Is(e, model.ErrNotFound) {
		t.Fatal("repeated delete appended invented version", e)
	}
	// A same disabled body must restore a deleted head rather than become a no-op.
	history, n, e := r.ListRuleRevisions(ctx, "t", first.RuleID, 100, 0)
	if e != nil || n != 4 {
		t.Fatal(history, n, e)
	}
	disabled := history[0].Rule
	if e = r.SaveRule(ctx, disabled); e != nil {
		t.Fatal(e)
	}
	rules, e := r.ListRules(ctx, "t")
	if e != nil || len(rules) != 1 || rules[0].Version != 5 || rules[0].Enabled {
		t.Fatal(rules, e)
	}
}
