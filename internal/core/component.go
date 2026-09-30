package core

import (
	"context"
	"iot-platform/internal/model"
	"iot-platform/internal/rulelab/eval"
)

func (e *Engine) applyComponentRoutes(ctx context.Context, msg model.StandardMessage, routes []eval.ComponentRoute) error {
	for _, route := range routes {
		component, kind := route.Component, route.AlarmType
		now := e.Clock.Now().UnixMilli()
		a := model.Alarm{ID: id("alarm"), TenantID: msg.TenantID, DeviceID: msg.DeviceID, DeviceName: e.alarmDeviceName(ctx, msg.TenantID, msg.DeviceID), RuleID: route.RuleID, TriggerID: msg.MessageID, ComponentID: component.ID, ComponentName: component.Name, ComponentLocation: component.Location, AlarmType: kind, AlarmLevel: "HIGH", Status: "ACTIVE", Source: "device", FirstTriggeredAt: now, LastTriggeredAt: now, TriggerCount: 1, CityCode: tag(msg, "cityCode", "unknown"), DistrictCode: tag(msg, "districtCode", "unknown"), BuildingID: tag(msg, "buildingId", "unknown"), AreaID: tag(msg, "areaId", ""), DeviceType: tag(msg, "deviceType", msg.ProductID), Details: map[string]any{"message": msg, "component": component, "direct": true}}
		a.Cameras, _ = e.ListCameraSummaries(ctx, msg.TenantID, msg.DeviceID)
		saved, event, err := e.Repo.ApplyComponentAlarm(ctx, a, route.State)
		if err != nil {
			return err
		}
		e.flushOutbox(ctx)
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
	return nil
}

// A normal flag only clears its own category; connection/registration packets
// and omitted flags carry no recovery evidence.
func directAlarmTypeCleared(msg model.StandardMessage, kind string) bool {
	return eval.DirectTypeCleared(msg, kind)
}
