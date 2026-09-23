package memory /* 声明 memory 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (r *Repository) ApplyComponentAlarm(_ context.Context, candidate model.Alarm, state model.ComponentAlarmState) (model.Alarm, string, error) { /* 定义 ApplyComponentAlarm 函数。 */
	r.mu.Lock()                   /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()           /* 安排函数结束时执行清理。 */
	if r.componentAlarms == nil { /* 判断条件并选择处理分支。 */
		r.componentAlarms = map[string]model.ComponentAlarmState{} /* 更新 r.componentAlarms 的值。 */
	} /* 结束当前表达式或代码块。 */
	k := key(candidate.TenantID, candidate.DeviceID, candidate.RuleID)                  /* 更新 k 的值。 */
	previous := r.componentAlarms[k]                                                    /* 更新 previous 的值。 */
	old := r.alarms[key(candidate.TenantID, previous.AlarmID)]                          /* 更新 old 的值。 */
	if state.MessageID == previous.MessageID && state.Timestamp == previous.Timestamp { /* 判断条件并选择处理分支。 */
		return cloneAlarm(old), previous.Event, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if !state.Supersedes(previous) { /* 判断条件并选择处理分支。 */
		return cloneAlarm(old), "", nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	alarm, event := model.TransitionComponentAlarm(candidate, old, state) /* 更新 event 的值。 */
	state.AlarmID = alarm.ID                                              /* 更新 state.AlarmID 的值。 */
	state.Event = event                                                   /* 更新 state.Event 的值。 */
	if alarm.ID != "" {                                                   /* 判断条件并选择处理分支。 */
		r.alarms[key(alarm.TenantID, alarm.ID)] = cloneAlarm(alarm) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	r.componentAlarms[k] = state         /* 更新 r.componentAlarms[k] 的值。 */
	return cloneAlarm(alarm), event, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
