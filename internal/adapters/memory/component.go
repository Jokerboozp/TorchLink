package memory

import (
	"context"
	"iot-platform/internal/model"
)

func (r *Repository) ApplyComponentAlarm(ctx context.Context, candidate model.Alarm, state model.ComponentAlarmState) (model.Alarm, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.componentAlarms == nil {
		r.componentAlarms = map[string]model.ComponentAlarmState{}
	}
	k := key(candidate.TenantID, candidate.DeviceID, candidate.RuleID)
	previous := r.componentAlarms[k]
	old := r.alarms[key(candidate.TenantID, previous.AlarmID)]
	o := model.AlarmObservationFromAlarm(ctx, candidate)
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
	_, createdObservation, observationErr := r.recordAlarmObservationLocked(o)
	if observationErr != nil {
		return candidate, "", observationErr
	}
	if !createdObservation {
		return cloneAlarm(old), previous.Event, nil
	}
	if state.MessageID == previous.MessageID && state.Timestamp == previous.Timestamp {
		return cloneAlarm(old), previous.Event, nil
	}
	if !state.Supersedes(previous) {
		return cloneAlarm(old), "", nil
	}
	alarm, event := model.TransitionComponentAlarm(candidate, old, state)
	state.AlarmID = alarm.ID
	state.Event = event
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
	return cloneAlarm(alarm), event, nil
}
