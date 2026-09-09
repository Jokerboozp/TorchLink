package core

import (
	"context"
	"crypto/sha256"
	"fmt"
	"iot-platform/internal/model"
	"sort"
)

func (e *Engine) applyComponentAlarms(ctx context.Context, msg model.StandardMessage, components []model.ComponentStatus) error {
	for _, component := range components {
		kinds := make([]string, 0, len(component.Alarms))
		for kind := range component.Alarms {
			kinds = append(kinds, kind)
		}
		sort.Strings(kinds)
		for _, kind := range kinds {
			now := e.Clock.Now().UnixMilli()
			a := model.Alarm{ID: id("alarm"), TenantID: msg.TenantID, DeviceID: msg.DeviceID, DeviceName: e.alarmDeviceName(ctx, msg.TenantID, msg.DeviceID),
				RuleID: fmt.Sprintf("%s%s:component:%x", directAlarmRulePrefix, kind, sha256.Sum256([]byte(component.ID))), TriggerID: msg.MessageID,
				ComponentID: component.ID, ComponentName: component.Name, ComponentLocation: component.Location,
				AlarmType: kind, AlarmLevel: "HIGH", Status: "ACTIVE", Source: "device", FirstTriggeredAt: now, LastTriggeredAt: now, TriggerCount: 1,
				CityCode: tag(msg, "cityCode", "unknown"), DistrictCode: tag(msg, "districtCode", "unknown"), BuildingID: tag(msg, "buildingId", "unknown"), AreaID: tag(msg, "areaId", ""), DeviceType: tag(msg, "deviceType", msg.ProductID),
				Details: map[string]any{"message": msg, "component": component, "direct": true}}
			a.Cameras, _ = e.ListCameraSummaries(ctx, msg.TenantID, msg.DeviceID)
			saved, event, err := e.Repo.ApplyComponentAlarm(ctx, a, model.ComponentAlarmState{Timestamp: component.Timestamp, MessageID: msg.MessageID, Active: component.Alarms[kind]})
			if err != nil {
				return err
			}
			if event != "" {
				topic := model.TopicAlarmRaised
				if event == "recovered" {
					topic = model.TopicAlarmRecovered
				}
				payload := mustJSON(saved)
				if err = e.Bus.Publish(ctx, topic, saved.ID, payload); err != nil {
					return err
				}
				if e.Realtime != nil {
					if err = e.Realtime.Publish(ctx, saved.MQTTTopic(event), payload, 1, false); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// A normal flag only clears its own category; connection/registration packets
// and omitted flags carry no recovery evidence.
func directAlarmTypeCleared(msg model.StandardMessage, kind string) bool {
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
