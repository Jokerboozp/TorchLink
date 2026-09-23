package core /* 声明 core 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"                     /* 执行当前语句并推进处理流程。 */
	"crypto/sha256"               /* 执行当前语句并推进处理流程。 */
	"fmt"                         /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"sort"                        /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func (e *Engine) applyComponentAlarms(ctx context.Context, msg model.StandardMessage, components []model.ComponentStatus) error { /* 定义 applyComponentAlarms 函数。 */
	for _, component := range components { /* 循环处理当前数据。 */
		kinds := make([]string, 0, len(component.Alarms)) /* 更新 kinds 的值。 */
		for kind := range component.Alarms {              /* 循环处理当前数据。 */
			kinds = append(kinds, kind) /* 更新 kinds 的值。 */
		} /* 结束当前表达式或代码块。 */
		sort.Strings(kinds)          /* 执行当前语句并推进处理流程。 */
		for _, kind := range kinds { /* 循环处理当前数据。 */
			now := e.Clock.Now().UnixMilli()                                                                                                                  /* 更新 now 的值。 */
			a := model.Alarm{ID: id("alarm"), TenantID: msg.TenantID, DeviceID: msg.DeviceID, DeviceName: e.alarmDeviceName(ctx, msg.TenantID, msg.DeviceID), /* 更新 a 的值。 */
				RuleID: fmt.Sprintf("%s%s:component:%x", directAlarmRulePrefix, kind, sha256.Sum256([]byte(component.ID))), TriggerID: msg.MessageID, /* 执行当前语句并推进处理流程。 */
				ComponentID: component.ID, ComponentName: component.Name, ComponentLocation: component.Location, /* 执行当前语句并推进处理流程。 */
				AlarmType: kind, AlarmLevel: "HIGH", Status: "ACTIVE", Source: "device", FirstTriggeredAt: now, LastTriggeredAt: now, TriggerCount: 1, /* 执行当前语句并推进处理流程。 */
				CityCode: tag(msg, "cityCode", "unknown"), DistrictCode: tag(msg, "districtCode", "unknown"), BuildingID: tag(msg, "buildingId", "unknown"), AreaID: tag(msg, "areaId", ""), DeviceType: tag(msg, "deviceType", msg.ProductID), /* 执行当前语句并推进处理流程。 */
				Details: map[string]any{"message": msg, "component": component, "direct": true}} /* 执行当前语句并推进处理流程。 */
			a.Cameras, _ = e.ListCameraSummaries(ctx, msg.TenantID, msg.DeviceID)                                                                                                        /* 更新 _ 的值。 */
			saved, event, err := e.Repo.ApplyComponentAlarm(ctx, a, model.ComponentAlarmState{Timestamp: component.Timestamp, MessageID: msg.MessageID, Active: component.Alarms[kind]}) /* 更新 err 的值。 */
			if err != nil {                                                                                                                                                              /* 判断条件并选择处理分支。 */
				return err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if event != "" { /* 判断条件并选择处理分支。 */
				topic := model.TopicAlarmRaised /* 更新 topic 的值。 */
				if event == "recovered" {       /* 判断条件并选择处理分支。 */
					topic = model.TopicAlarmRecovered /* 更新 topic 的值。 */
				} /* 结束当前表达式或代码块。 */
				payload := mustJSON(saved)                                          /* 更新 payload 的值。 */
				if err = e.Bus.Publish(ctx, topic, saved.ID, payload); err != nil { /* 判断条件并选择处理分支。 */
					return err /* 返回当前处理结果。 */
				} /* 结束当前表达式或代码块。 */
				if e.Realtime != nil { /* 判断条件并选择处理分支。 */
					if err = e.Realtime.Publish(ctx, saved.MQTTTopic(event), payload, 1, false); err != nil { /* 判断条件并选择处理分支。 */
						return err /* 返回当前处理结果。 */
					} /* 结束当前表达式或代码块。 */
				} /* 结束当前表达式或代码块。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// A normal flag only clears its own category; connection/registration packets
// and omitted flags carry no recovery evidence.
func directAlarmTypeCleared(msg model.StandardMessage, kind string) bool { /* 定义 directAlarmTypeCleared 函数。 */
	keys := map[string][]string{ /* 更新 keys 的值。 */
		"FIRE": {"fireAlarm"}, "SMOKE_DETECTED": {"smoke", "smokeDetected"}, /* 执行当前语句并推进处理流程。 */
		"DEVICE_FAULT":   {"fault", "powerFault", "openCircuit", "shortCircuit", "removed", "sensorFault", "upgradeFault"}, /* 执行当前语句并推进处理流程。 */
		"DEVICE_OFFLINE": {"offline"}, "MANUAL_ALARM": {"alarm"},                                                           /* 执行当前语句并推进处理流程。 */
	}[kind] /* 结束当前表达式或代码块。 */
	found := false             /* 更新 found 的值。 */
	for _, key := range keys { /* 循环处理当前数据。 */
		if value, ok := messageValue(msg, key); ok { /* 判断条件并选择处理分支。 */
			if truthy(value) { /* 判断条件并选择处理分支。 */
				return false /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			found = true /* 更新 found 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return found /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
