package postgres

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"iot-platform/internal/model"
	"iot-platform/internal/rulelab/history"
)

func routingSnapshot(ctx context.Context, tx pgx.Tx, tenant, device string) (model.RuleRoutingSnapshot, error) {
	snapshot := model.RuleRoutingSnapshot{Direct: map[string]model.RuleRoutingAlarm{}, Components: map[string]model.RuleRoutingComponent{}}
	rows, err := tx.Query(ctx, `SELECT body,version FROM alarm_record WHERE tenant_id=$1 AND device_id=$2 AND status IN ('ACTIVE','ACKED') AND rule_id LIKE 'device-report:%' AND COALESCE(body->>'componentId','')=''`, tenant, device)
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		var body []byte
		var alarm model.Alarm
		var version int64
		if err = rows.Scan(&body, &version); err == nil {
			err = json.Unmarshal(body, &alarm)
		}
		if err != nil {
			rows.Close()
			return snapshot, err
		}
		alarm.Version = version
		snapshot.Direct[alarm.RuleID] = history.RoutingAlarm(alarm)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return snapshot, err
	}
	rows, err = tx.Query(ctx, `SELECT rule_id,body FROM component_alarm_state WHERE tenant_id=$1 AND device_id=$2`, tenant, device)
	if err != nil {
		return snapshot, err
	}
	type entry struct {
		rule  string
		state model.ComponentAlarmState
	}
	entries := []entry{}
	for rows.Next() {
		var v entry
		var body []byte
		if err = rows.Scan(&v.rule, &body); err == nil {
			err = json.Unmarshal(body, &v.state)
		}
		if err != nil {
			rows.Close()
			return snapshot, err
		}
		entries = append(entries, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return snapshot, err
	}
	for _, v := range entries {
		var lifecycle model.RuleRoutingAlarm
		if v.state.AlarmID != "" {
			var body []byte
			var alarm model.Alarm
			var version int64
			if err = tx.QueryRow(ctx, `SELECT body,version FROM alarm_record WHERE tenant_id=$1 AND id=$2`, tenant, v.state.AlarmID).Scan(&body, &version); err != nil {
				return snapshot, err
			}
			if err = json.Unmarshal(body, &alarm); err != nil {
				return snapshot, err
			}
			alarm.Version = version
			lifecycle = history.RoutingAlarm(alarm)
		}
		snapshot.Components[v.rule] = model.RuleRoutingComponent{Watermark: v.state, Lifecycle: lifecycle}
	}
	return snapshot, nil
}
func checkRoutingTrace(ctx context.Context, tx pgx.Tx, tenant, device string) error {
	binding, ok := model.RuleTraceBindingFrom(ctx)
	if !ok {
		return nil
	}
	if binding.TenantID != tenant {
		return model.ErrStaleClaim
	}
	if err := checkRuleTraceClaim(ctx, tx, binding); err != nil {
		return err
	}
	trace, err := readRuleTrace(ctx, tx, binding)
	if err != nil {
		return err
	}
	if trace.DeviceID != device || trace.Status != "IN_PROGRESS" {
		return model.ErrRuleHistoryInvalid
	}
	return nil
}
func appendRoutingTrace(ctx context.Context, tx pgx.Tx, step model.RuleRoutingStep) error {
	binding, ok := model.RuleTraceBindingFrom(ctx)
	if !ok {
		return nil
	}
	trace, err := readRuleTrace(ctx, tx, binding)
	if err != nil {
		return err
	}
	step.CommittedAt, err = ruleHistoryClock(ctx, tx)
	if err != nil {
		return err
	}
	trace.RoutingSteps = append(trace.RoutingSteps, step)
	return writeRuleTrace(ctx, tx, trace)
}
func routingRaiseStep(candidate, old, next model.Alarm, applied bool) model.RuleRoutingStep {
	now := candidate.LastTriggeredAt
	return model.RuleRoutingStep{Kind: "DIRECT_RAISE", RuleID: candidate.RuleID, Before: history.RoutingAlarm(old), After: history.RoutingAlarm(next), Applied: applied, Times: model.RuleStageTimes{RaiseAtMillis: &now}}
}
func routingRecoverStep(old, next model.Alarm, applied bool) model.RuleRoutingStep {
	now := next.RecoveredAt
	return model.RuleRoutingStep{Kind: "DIRECT_RECOVER", RuleID: next.RuleID, Before: history.RoutingAlarm(old), After: history.RoutingAlarm(next), Applied: applied, Times: model.RuleStageTimes{RecoverAtMillis: &now}}
}
