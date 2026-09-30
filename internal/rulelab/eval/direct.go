package eval

import (
	"encoding/json"
	"fmt"
	"iot-platform/internal/model"
	"strings"
)

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

func DirectMetadata(msg model.StandardMessage) (string, string) {
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

func DirectCleared(msg model.StandardMessage) bool {
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

// A normal flag only clears its own category; connection/registration packets
// and omitted flags carry no recovery evidence.
func DirectTypeCleared(msg model.StandardMessage, kind string) bool {
	keys := map[string][]string{
		"FIRE": {"fireAlarm"}, "SMOKE_DETECTED": {"smoke", "smokeDetected"},
		"DEVICE_FAULT":   {"fault", "powerFault", "openCircuit", "shortCircuit", "removed", "sensorFault", "upgradeFault"},
		"DEVICE_OFFLINE": {"offline"}, "MANUAL_ALARM": {"alarm"},
	}[kind]
	found := false
	for _, key := range keys {
		if value, ok := messageValue(msg, key); ok {
			if truthy(value) {
				return false
			}
			found = true
		}
	}
	return found
}
