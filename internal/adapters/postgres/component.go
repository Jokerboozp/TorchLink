package postgres

import (
	"context"
	"encoding/json"
	"iot-platform/internal/model"
)

func (r *Repository) ApplyComponentAlarm(ctx context.Context, candidate model.Alarm, state model.ComponentAlarmState) (model.Alarm, string, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return candidate, "", err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO component_alarm_state(tenant_id,device_id,rule_id,body) VALUES($1,$2,$3,'{}') ON CONFLICT DO NOTHING`, candidate.TenantID, candidate.DeviceID, candidate.RuleID)
	if err != nil {
		return candidate, "", err
	}
	var b []byte
	err = tx.QueryRow(ctx, `SELECT body FROM component_alarm_state WHERE tenant_id=$1 AND device_id=$2 AND rule_id=$3 FOR UPDATE`, candidate.TenantID, candidate.DeviceID, candidate.RuleID).Scan(&b)
	if err != nil {
		return candidate, "", err
	}
	var previous model.ComponentAlarmState
	if err = json.Unmarshal(b, &previous); err != nil {
		return candidate, "", err
	}
	var old model.Alarm
	var oldVersion int64
	if previous.AlarmID != "" {
		err = tx.QueryRow(ctx, `SELECT body,version FROM alarm_record WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, candidate.TenantID, previous.AlarmID).Scan(&b, &oldVersion)
		if err != nil {
			return candidate, "", err
		}
		if err = json.Unmarshal(b, &old); err != nil {
			return candidate, "", err
		}
	}
	old.Version = oldVersion
	if state.MessageID == previous.MessageID && state.Timestamp == previous.Timestamp {
		return old, previous.Event, tx.Commit(ctx)
	}
	if !state.Supersedes(previous) {
		return old, "", tx.Commit(ctx)
	}
	alarm, event := model.TransitionComponentAlarm(candidate, old, state)
	if alarm.ID != "" {
		alarm.Version = old.Version + 1
		b, err = json.Marshal(alarm)
		if err != nil {
			return candidate, "", err
		}
		_, err = tx.Exec(ctx, `INSERT INTO alarm_record(tenant_id,id,rule_id,device_id,status,level,source,last_triggered_at,body,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,1) ON CONFLICT(tenant_id,id) DO UPDATE SET status=excluded.status,last_triggered_at=excluded.last_triggered_at,body=excluded.body,version=alarm_record.version+1`, alarm.TenantID, alarm.ID, alarm.RuleID, alarm.DeviceID, alarm.Status, alarm.AlarmLevel, alarm.Source, alarm.LastTriggeredAt, b)
		if err == nil && state.Active {
			err = insertOutbox(ctx, tx, model.AlarmReportEvent(alarm, candidate))
		}
		if err == nil {
			for _, ev := range model.DutyAlarmEvents(ctx, old, alarm) {
				if err = insertDutyEvent(ctx, tx, ev); err != nil {
					break
				}
			}
		}
		if err != nil {
			return candidate, "", err
		}
	}
	state.AlarmID = alarm.ID
	state.Event = event
	b, _ = json.Marshal(state)
	_, err = tx.Exec(ctx, `UPDATE component_alarm_state SET body=$4 WHERE tenant_id=$1 AND device_id=$2 AND rule_id=$3`, candidate.TenantID, candidate.DeviceID, candidate.RuleID, b)
	if err != nil {
		return candidate, "", err
	}
	return alarm, event, tx.Commit(ctx)
}
