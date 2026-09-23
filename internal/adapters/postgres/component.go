package postgres /* 声明 postgres 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"encoding/json"               /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (r *Repository) ApplyComponentAlarm(ctx context.Context, candidate model.Alarm, state model.ComponentAlarmState) (model.Alarm, string, error) { /* 定义 ApplyComponentAlarm 函数。 */
	tx, err := r.pool.Begin(ctx) /* 更新 err 的值。 */
	if err != nil {              /* 判断条件并选择处理分支。 */
		return candidate, "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer tx.Rollback(ctx)                                                                                                                                                                              /* 安排函数结束时执行清理。 */
	_, err = tx.Exec(ctx, `INSERT INTO component_alarm_state(tenant_id,device_id,rule_id,body) VALUES($1,$2,$3,'{}') ON CONFLICT DO NOTHING`, candidate.TenantID, candidate.DeviceID, candidate.RuleID) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                     /* 判断条件并选择处理分支。 */
		return candidate, "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var b []byte                                                                                                                                                                                      /* 声明 b。 */
	err = tx.QueryRow(ctx, `SELECT body FROM component_alarm_state WHERE tenant_id=$1 AND device_id=$2 AND rule_id=$3 FOR UPDATE`, candidate.TenantID, candidate.DeviceID, candidate.RuleID).Scan(&b) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                   /* 判断条件并选择处理分支。 */
		return candidate, "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var previous model.ComponentAlarmState              /* 声明 previous。 */
	if err = json.Unmarshal(b, &previous); err != nil { /* 判断条件并选择处理分支。 */
		return candidate, "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var old model.Alarm         /* 声明 old。 */
	if previous.AlarmID != "" { /* 判断条件并选择处理分支。 */
		err = tx.QueryRow(ctx, `SELECT body FROM alarm_record WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, candidate.TenantID, previous.AlarmID).Scan(&b) /* 更新 err 的值。 */
		if err != nil {                                                                                                                                /* 判断条件并选择处理分支。 */
			return candidate, "", err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if err = json.Unmarshal(b, &old); err != nil { /* 判断条件并选择处理分支。 */
			return candidate, "", err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if state.MessageID == previous.MessageID && state.Timestamp == previous.Timestamp { /* 判断条件并选择处理分支。 */
		return old, previous.Event, tx.Commit(ctx) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !state.Supersedes(previous) { /* 判断条件并选择处理分支。 */
		return old, "", tx.Commit(ctx) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	alarm, event := model.TransitionComponentAlarm(candidate, old, state) /* 更新 event 的值。 */
	if alarm.ID != "" {                                                   /* 判断条件并选择处理分支。 */
		b, err = json.Marshal(alarm) /* 更新 err 的值。 */
		if err != nil {              /* 判断条件并选择处理分支。 */
			return candidate, "", err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		_, err = tx.Exec(ctx, `INSERT INTO alarm_record(tenant_id,id,rule_id,device_id,status,level,source,last_triggered_at,body) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(tenant_id,id) DO UPDATE SET status=excluded.status,last_triggered_at=excluded.last_triggered_at,body=excluded.body`, alarm.TenantID, alarm.ID, alarm.RuleID, alarm.DeviceID, alarm.Status, alarm.AlarmLevel, alarm.Source, alarm.LastTriggeredAt, b) /* 更新 err 的值。 */
		if err != nil {                                                                                                                                                                                                                                                                                                                                                                                                                /* 判断条件并选择处理分支。 */
			return candidate, "", err /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	state.AlarmID = alarm.ID                                                                                                                                                          /* 更新 state.AlarmID 的值。 */
	state.Event = event                                                                                                                                                               /* 更新 state.Event 的值。 */
	b, _ = json.Marshal(state)                                                                                                                                                        /* 更新 _ 的值。 */
	_, err = tx.Exec(ctx, `UPDATE component_alarm_state SET body=$4 WHERE tenant_id=$1 AND device_id=$2 AND rule_id=$3`, candidate.TenantID, candidate.DeviceID, candidate.RuleID, b) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                   /* 判断条件并选择处理分支。 */
		return candidate, "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return alarm, event, tx.Commit(ctx) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
