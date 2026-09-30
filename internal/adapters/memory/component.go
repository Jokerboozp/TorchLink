package memory

import (
	"context"
	"iot-platform/internal/model"
	"iot-platform/internal/rulelab/eval"
	"iot-platform/internal/rulelab/history"
)

func (r *Repository) ApplyComponentAlarm(ctx context.Context, candidate model.Alarm, state model.ComponentAlarmState) (model.Alarm, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkRoutingTraceLocked(ctx, candidate.TenantID, candidate.DeviceID); err != nil {
		return candidate, "", err
	}
	if r.componentAlarms == nil {
		r.componentAlarms = map[string]model.ComponentAlarmState{}
	}
	k := key(candidate.TenantID, candidate.DeviceID, candidate.RuleID)
	previous := r.componentAlarms[k]
	old := r.alarms[key(candidate.TenantID, previous.AlarmID)]
	decision := eval.ComponentTransition(candidate, old, state, previous)
	componentStep := func(next model.Alarm, watermark model.ComponentAlarmState, applied bool) {
		now := candidate.LastTriggeredAt
		prior := previous
		mark := watermark
		r.appendRoutingTraceLocked(ctx, model.RuleRoutingStep{Kind: "COMPONENT", RuleID: candidate.RuleID, Before: history.RoutingAlarm(old), After: history.RoutingAlarm(next), PreviousWatermark: &prior, Watermark: &mark, Times: model.RuleStageTimes{ComponentAtMillis: &now}, Applied: applied, Event: decision.Event})
	}
	o := model.AlarmObservationFromComponent(ctx, candidate, state)
	o.FactKind = "CLEAR"
	if state.Active {
		o.FactKind = "ASSERT"
	}
	o.EventAt = state.Timestamp
	o.AlarmID = old.ID
	if o.AlarmID == "" && state.Active {
		o.AlarmID = candidate.ID
	}
	o.WatermarkAt = previous.Timestamp
	if !state.Supersedes(previous) {
		o.Acceptance = "REJECTED"
		o.Reason = "STALE_OR_EQUAL_STATE"
	}
	savedObservation, createdObservation, observationErr := r.recordAlarmObservationLocked(o)
	if observationErr != nil {
		return candidate, "", observationErr
	}
	if !createdObservation {
		decision.Event = previous.Event
		componentStep(old, previous, false)
		return cloneAlarm(old), previous.Event, nil
	}
	r.acceptSignalLocked(savedObservation)
	if !decision.Applied {
		componentStep(decision.Alarm, previous, false)
		return cloneAlarm(decision.Alarm), decision.Event, nil
	}
	alarm, event := decision.Alarm, decision.Event
	state = decision.Watermark
	if alarm.ID != "" {
		alarm.Version = r.alarms[key(alarm.TenantID, alarm.ID)].Version + 1
		r.alarms[key(alarm.TenantID, alarm.ID)] = cloneAlarm(alarm)
		for _, event := range model.DutyAlarmEvents(ctx, old, alarm) {
			r.appendDutyEventLocked(event)
		}
		if state.Active {
			r.addOutbox(model.AlarmReportEvent(alarm, candidate))
		}
	}
	r.componentAlarms[k] = state
	componentStep(alarm, state, true)
	return cloneAlarm(alarm), event, nil
}
