package memory

import (
	"context"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/rulelab/history"
)

func (r *Repository) routingSnapshotLocked(tenant, device string) model.RuleRoutingSnapshot {
	snapshot := model.RuleRoutingSnapshot{Direct: map[string]model.RuleRoutingAlarm{}, Components: map[string]model.RuleRoutingComponent{}}
	for _, alarm := range r.alarms {
		if alarm.TenantID == tenant && alarm.DeviceID == device && alarm.ComponentID == "" && strings.HasPrefix(alarm.RuleID, "device-report:") && (alarm.Status == "ACTIVE" || alarm.Status == "ACKED") {
			snapshot.Direct[alarm.RuleID] = history.RoutingAlarm(alarm)
		}
	}
	prefix := key(tenant, device) + "\x00"
	for key, value := range r.componentAlarms {
		if strings.HasPrefix(key, prefix) {
			rule := strings.TrimPrefix(key, prefix)
			snapshot.Components[rule] = model.RuleRoutingComponent{Watermark: value, Lifecycle: history.RoutingAlarm(r.alarms[keyAlarm(tenant, value.AlarmID)])}
		}
	}
	return snapshot
}
func keyAlarm(tenant, id string) string { return key(tenant, id) }
func (r *Repository) checkRoutingTraceLocked(ctx context.Context, tenant, device string) error {
	binding, ok := model.RuleTraceBindingFrom(ctx)
	if !ok {
		return nil
	}
	if binding.TenantID != tenant {
		return model.ErrStaleClaim
	}
	if err := r.checkRuleClaimLocked(binding); err != nil {
		return err
	}
	trace, ok := r.ruleHistory.traces[key(tenant, model.RuleTraceID(binding))]
	if !ok || trace.DeviceID != device || trace.Status != "IN_PROGRESS" {
		return model.ErrRuleHistoryInvalid
	}
	return nil
}
func (r *Repository) appendRoutingTraceLocked(ctx context.Context, step model.RuleRoutingStep) {
	binding, ok := model.RuleTraceBindingFrom(ctx)
	if !ok {
		return
	}
	k := key(binding.TenantID, model.RuleTraceID(binding))
	trace := r.ruleHistory.traces[k]
	step.CommittedAt = time.Now().UnixMilli()
	trace.RoutingSteps = append(trace.RoutingSteps, step)
	r.ruleHistory.traces[k] = trace
}
func routingRaiseStep(candidate, old, next model.Alarm, applied bool) model.RuleRoutingStep {
	now := candidate.LastTriggeredAt
	return model.RuleRoutingStep{Kind: "DIRECT_RAISE", RuleID: candidate.RuleID, Before: history.RoutingAlarm(old), After: history.RoutingAlarm(next), Applied: applied, Times: model.RuleStageTimes{RaiseAtMillis: &now}}
}
func routingRecoverStep(old, next model.Alarm, applied bool) model.RuleRoutingStep {
	now := next.RecoveredAt
	return model.RuleRoutingStep{Kind: "DIRECT_RECOVER", RuleID: next.RuleID, Before: history.RoutingAlarm(old), After: history.RoutingAlarm(next), Applied: applied, Times: model.RuleStageTimes{RecoverAtMillis: &now}}
}
