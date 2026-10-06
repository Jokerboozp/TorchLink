package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

const directAlarmRulePrefix = "device-report:"

func (e *Engine) raiseDirectAlarm(ctx context.Context, msg model.StandardMessage) (model.Alarm, bool, error) {
	now := e.Clock.Now().UnixMilli()
	alarmType, level := directAlarmMetadata(msg)
	a := model.Alarm{
		ID: id("alarm"), TenantID: msg.TenantID, RuleID: directAlarmRuleID(alarmType), TriggerID: msg.MessageID,
		DeviceID: msg.DeviceID, DeviceName: e.alarmDeviceName(ctx, msg.TenantID, msg.DeviceID), AlarmType: alarmType, AlarmLevel: level, Status: "ACTIVE", Source: "device",
		CityCode: tag(msg, "cityCode", "unknown"), DistrictCode: tag(msg, "districtCode", "unknown"),
		BuildingID: tag(msg, "buildingId", "unknown"), DeviceType: tag(msg, "deviceType", msg.ProductID),
		AreaID: tag(msg, "areaId", ""), FirstTriggeredAt: now, LastTriggeredAt: now, TriggerCount: 1,
		Content: alarmContent(msg), Details: map[string]any{"message": msg, "direct": true},
	}
	a.Cameras, _ = e.ListCameraSummaries(ctx, msg.TenantID, msg.DeviceID)
	a.Location = e.alarmLocation(ctx, msg.TenantID, msg.DeviceID, "")
	saved, created, _, err := e.upsertReportedAlarm(ctx, a, msg)
	if err != nil {
		return saved, false, err
	}
	if created {
		if e.Metrics != nil {
			e.Metrics.Inc("alarm_trigger_total")
		}
		payload, _ := json.Marshal(saved)
		e.publishEvent(ctx, model.TopicAlarmRaised, saved.ID, saved.MQTTTopic("raised"), payload)
	}
	e.flushOutbox(ctx)
	return saved, created, nil
}

func directAlarmRuleID(alarmType string) string {
	return directAlarmRulePrefix + alarmType
}

func firstMessageValue(msg model.StandardMessage, keys ...string) any {
	for _, key := range keys {
		if value, ok := messageValue(msg, key); ok {
			return value
		}
	}
	return nil
}

func messageValue(msg model.StandardMessage, key string) (any, bool) {
	for _, source := range directMessageSources(msg) {
		if value, ok := source[key]; ok {
			return value, true
		}
	}
	return nil, false
}

func directMessageSources(msg model.StandardMessage) []map[string]any {
	sources := []map[string]any{msg.Properties, msg.Event, msg.Raw}
	if payload := messageMap(msg.Raw["payload"]); payload != nil {
		sources = append(sources, payload)
		if alarm := messageMap(payload["alarm"]); alarm != nil {
			sources = append(sources, alarm)
		}
	}
	if alarm := messageMap(msg.Event["alarm"]); alarm != nil {
		sources = append(sources, alarm)
	}
	return sources
}

func messageMap(value any) map[string]any {
	switch item := value.(type) {
	case map[string]any:
		return item
	case json.RawMessage:
		var out map[string]any
		if json.Unmarshal(item, &out) == nil {
			return out
		}
	case []byte:
		var out map[string]any
		if json.Unmarshal(item, &out) == nil {
			return out
		}
	case string:
		var out map[string]any
		if json.Unmarshal([]byte(item), &out) == nil {
			return out
		}
	}
	return nil
}

func messageFlag(msg model.StandardMessage, key string) bool {
	value, ok := messageValue(msg, key)
	return ok && truthy(value)
}

func truthy(value any) bool {
	switch item := value.(type) {
	case bool:
		return item
	case float64:
		return item != 0
	case float32:
		return item != 0
	case int:
		return item != 0
	case int64:
		return item != 0
	case uint:
		return item != 0
	case uint64:
		return item != 0
	case string:
		return strings.EqualFold(strings.TrimSpace(item), "true") || strings.TrimSpace(item) == "1" || strings.EqualFold(strings.TrimSpace(item), "yes") || strings.EqualFold(strings.TrimSpace(item), "on")
	default:
		return false
	}
}

func normalizeAlarmToken(value any) string {
	if value == nil {
		return ""
	}
	text := strings.ToUpper(strings.TrimSpace(fmt.Sprint(value)))
	if text == "" || text == "<NIL>" || text == "TRUE" || text == "FALSE" {
		return ""
	}
	text = strings.NewReplacer(" ", "_", "-", "_").Replace(text)
	var out strings.Builder
	for _, r := range text {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.' {
			out.WriteRune(r)
		}
	}
	normalized := strings.Trim(out.String(), "_.")
	if runes := []rune(normalized); len(runes) > 64 {
		normalized = string(runes[:64])
	}
	return normalized
}

// maxAlarmContent bounds the description copied from a device report.
const maxAlarmContent = 500

// alarmContent is the description a device reported with the alarm; when the
// report has none, the first non-empty fallback (such as the rule
// description or name) is used.
func alarmContent(msg model.StandardMessage, fallbacks ...string) string {
	for _, key := range []string{"content", "alarmContent", "alarm_content", "description", "alarmDesc", "alarm_desc"} {
		value, ok := messageValue(msg, key)
		if !ok {
			continue
		}
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return truncateRunes(strings.TrimSpace(text), maxAlarmContent)
		}
	}
	for _, text := range fallbacks {
		if text = strings.TrimSpace(text); text != "" {
			return truncateRunes(text, maxAlarmContent)
		}
	}
	return ""
}

func truncateRunes(text string, limit int) string {
	if runes := []rune(text); len(runes) > limit {
		return string(runes[:limit])
	}
	return text
}

func directAlarmMetadata(msg model.StandardMessage) (string, string) {
	alarmType := normalizeAlarmToken(firstMessageValue(msg, "alarmType", "alarm_type"))
	if alarmType == "" {
		switch {
		case messageFlag(msg, "fireAlarm"):
			alarmType = "FIRE"
		case messageFlag(msg, "smoke") || messageFlag(msg, "smokeDetected"):
			alarmType = "SMOKE_DETECTED"
		case messageFlag(msg, "fault") || messageFlag(msg, "powerFault") || messageFlag(msg, "sensorFault"):
			alarmType = "DEVICE_FAULT"
		case messageFlag(msg, "offline"):
			alarmType = "DEVICE_OFFLINE"
		default:
			alarmType = "MANUAL_ALARM"
		}
	}
	level := normalizeAlarmToken(firstMessageValue(msg, "alarmLevel", "alarm_level", "level"))
	switch level {
	case "CRITICAL", "HIGH", "MEDIUM", "LOW", "INFO":
	default:
		level = "HIGH"
	}
	return alarmType, level
}

func directAlarmCleared(msg model.StandardMessage) bool {
	// Old immutable protocol releases expose aggregate objects without a safe
	// component identity contract. Never guess a whole-controller recovery.
	if msg.Event["type"] == "COMPONENT_STATUS" {
		if _, exists := msg.Event["objects"]; exists {
			return false
		}
	}
	if msg.MessageType != model.PropertyReport && msg.MessageType != model.StateChange {
		return false
	}
	found := false
	for _, key := range []string{"alarm", "fireAlarm", "smoke", "smokeDetected", "fault", "offline", "powerFault", "openCircuit", "shortCircuit", "removed", "sensorFault", "upgradeFault"} {
		value, ok := messageValue(msg, key)
		if !ok {
			continue
		}
		found = true
		if truthy(value) {
			return false
		}
	}
	return found
}

func (e *Engine) recoverDirectAlarms(ctx context.Context, msg model.StandardMessage) error {
	for _, status := range []string{"ACTIVE", "ACKED"} {
		alarms, err := e.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: msg.TenantID, DeviceID: msg.DeviceID, Status: status, Limit: 100})
		if err != nil {
			return err
		}
		for _, listed := range alarms {
			if listed.ComponentID != "" || strings.Contains(listed.RuleID, ":external:") || !strings.HasPrefix(listed.RuleID, directAlarmRulePrefix) || !directAlarmTypeCleared(msg, listed.AlarmType) {
				continue
			}
			alarm, written, err := e.mutateAlarm(ctx, listed.TenantID, listed.ID, func(a *model.Alarm) (bool, error) {
				if a.Status != "ACTIVE" && a.Status != "ACKED" {
					return false, nil
				}
				a.Status = "RECOVERED"
				a.RecoveredAt = e.Clock.Now().UnixMilli()
				return true, nil
			})
			if err != nil {
				return err
			}
			if written {
				payload := mustJSON(alarm)
				e.publishEvent(ctx, model.TopicAlarmRecovered, alarm.ID, alarm.MQTTTopic("recovered"), payload)
			}
		}
	}
	return nil
}
