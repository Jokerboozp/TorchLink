package memory

import (
	"context"
	"iot-platform/internal/model"
)

func (r *Repository) ApplyComponentAlarm(_ context.Context, candidate model.Alarm, state model.ComponentAlarmState) (model.Alarm, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.componentAlarms == nil {
		r.componentAlarms = map[string]model.ComponentAlarmState{}
	}
	k := key(candidate.TenantID, candidate.DeviceID, candidate.RuleID)
	previous := r.componentAlarms[k]
	old := r.alarms[key(candidate.TenantID, previous.AlarmID)]
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
		r.alarms[key(alarm.TenantID, alarm.ID)] = cloneAlarm(alarm)
	}
	r.componentAlarms[k] = state
	return cloneAlarm(alarm), event, nil
}
