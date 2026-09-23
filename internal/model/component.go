package model /* 声明 model 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"regexp"        /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// ComponentStatus is a partial observation: omitted components/alarm types
// retain their previous state. IDs are stable within the parent device.
type ComponentStatus struct { /* 定义 ComponentStatus 类型。 */
	ID        string          `json:"id"`                 /* 执行当前语句并推进处理流程。 */
	Name      string          `json:"name,omitempty"`     /* 执行当前语句并推进处理流程。 */
	Location  string          `json:"location,omitempty"` /* 执行当前语句并推进处理流程。 */
	Timestamp int64           `json:"timestamp"`          /* 执行当前语句并推进处理流程。 */
	Alarms    map[string]bool `json:"alarms"`             /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

var componentAlarmType = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`) /* 声明 componentAlarmType。 */

func MessageComponents(msg StandardMessage) ([]ComponentStatus, error) { /* 定义 MessageComponents 函数。 */
	value, exists := msg.Event["components"] /* 更新 exists 的值。 */
	if !exists {                             /* 判断条件并选择处理分支。 */
		return nil, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if msg.MessageType != AlarmReport && msg.MessageType != StateChange && msg.MessageType != EventReport { /* 判断条件并选择处理分支。 */
		return nil, errors.New("component status requires alarm, state or event message") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	b, err := json.Marshal(value) /* 更新 err 的值。 */
	if err != nil {               /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var components []ComponentStatus             /* 声明 components。 */
	d := json.NewDecoder(bytes.NewReader(b))     /* 更新 d 的值。 */
	d.DisallowUnknownFields()                    /* 执行当前语句并推进处理流程。 */
	if err = d.Decode(&components); err != nil { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("invalid components: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(components) == 0 || len(components) > 256 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("components must contain 1..256 objects") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var raw []struct { /* 声明 raw。 */
		Alarms map[string]json.RawMessage `json:"alarms"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	if err := json.Unmarshal(b, &raw); err != nil { /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	seen := map[string]bool{}   /* 更新 seen 的值。 */
	for i := range components { /* 循环处理当前数据。 */
		c := &components[i]                                                                                                                                                        /* 更新 c 的值。 */
		if strings.TrimSpace(c.ID) != c.ID || c.ID == "" || len(c.ID) > 128 || strings.ContainsAny(c.ID, "\x00\r\n") || seen[c.ID] || len(c.Name) > 256 || len(c.Location) > 512 { /* 判断条件并选择处理分支。 */
			return nil, errors.New("component identity is invalid or duplicated") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		seen[c.ID] = true     /* 更新 seen[c.ID] 的值。 */
		if c.Timestamp == 0 { /* 判断条件并选择处理分支。 */
			c.Timestamp = msg.Timestamp /* 更新 c.Timestamp 的值。 */
		} /* 结束当前表达式或代码块。 */
		if c.Timestamp <= 0 || c.Timestamp > 253402300799999 || (msg.Timestamp > 0 && c.Timestamp > msg.Timestamp+300000) { /* 判断条件并选择处理分支。 */
			return nil, errors.New("invalid component timestamp") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if len(c.Alarms) == 0 || len(c.Alarms) > 32 { /* 判断条件并选择处理分支。 */
			return nil, errors.New("component alarms must contain 1..32 explicit boolean states") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		// encoding/json accepts null as false for bool. Reject that ambiguity.
		for kind := range c.Alarms { /* 循环处理当前数据。 */
			if !componentAlarmType.MatchString(kind) || string(raw[i].Alarms[kind]) == "null" { /* 判断条件并选择处理分支。 */
				return nil, errors.New("invalid component alarm type or boolean") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return components, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// The watermark and alarm transition must commit atomically. AlarmID refers to
// the last lifecycle, including recovered/closed records; it is not a device ID.
type ComponentAlarmState struct { /* 定义 ComponentAlarmState 类型。 */
	Timestamp int64  `json:"timestamp"`       /* 执行当前语句并推进处理流程。 */
	MessageID string `json:"messageId"`       /* 执行当前语句并推进处理流程。 */
	Active    bool   `json:"active"`          /* 执行当前语句并推进处理流程。 */
	AlarmID   string `json:"alarmId"`         /* 执行当前语句并推进处理流程。 */
	Event     string `json:"event,omitempty"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (next ComponentAlarmState) Supersedes(old ComponentAlarmState) bool { /* 定义 Supersedes 函数。 */
	if next.Timestamp < old.Timestamp || (next.MessageID == old.MessageID && old.MessageID != "") { /* 判断条件并选择处理分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	// Second-resolution fire clocks can collide. An equal-time normal state
	// must not clear an asserted alarm; a later explicit normal state can.
	return next.Timestamp != old.Timestamp || (!old.Active && next.Active) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// TransitionComponentAlarm is shared by persistent and in-memory repositories.
func TransitionComponentAlarm(candidate, old Alarm, state ComponentAlarmState) (Alarm, string) { /* 定义 TransitionComponentAlarm 函数。 */
	active := old.Status == "ACTIVE" || old.Status == "ACKED" /* 更新 active 的值。 */
	if state.Active {                                         /* 判断条件并选择处理分支。 */
		if active { /* 判断条件并选择处理分支。 */
			old.LastTriggeredAt = candidate.LastTriggeredAt /* 更新 old.LastTriggeredAt 的值。 */
			old.TriggerCount++                              /* 执行当前语句并推进处理流程。 */
			old.TriggerID = candidate.TriggerID             /* 更新 old.TriggerID 的值。 */
			old.Details = candidate.Details                 /* 更新 old.Details 的值。 */
			if candidate.ComponentName != "" {              /* 判断条件并选择处理分支。 */
				old.ComponentName = candidate.ComponentName /* 更新 old.ComponentName 的值。 */
			} /* 结束当前表达式或代码块。 */
			if candidate.ComponentLocation != "" { /* 判断条件并选择处理分支。 */
				old.ComponentLocation = candidate.ComponentLocation /* 更新 old.ComponentLocation 的值。 */
			} /* 结束当前表达式或代码块。 */
			return old, "" /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return candidate, "raised" /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if active { /* 判断条件并选择处理分支。 */
		old.Status = "RECOVERED"                    /* 更新 old.Status 的值。 */
		old.RecoveredAt = candidate.LastTriggeredAt /* 更新 old.RecoveredAt 的值。 */
		return old, "recovered"                     /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return old, "" /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
