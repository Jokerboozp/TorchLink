package core

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func TestAlarmReportsIncludeRepeatedTriggers(t *testing.T) {
	for _, kind := range []string{"direct", "rule", "component"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			repo, bus := memory.NewRepository(), local.NewBus()
			e := New(repo, nil, bus, local.NewRealtime(), nil, nil)
			var reports []model.Alarm
			if err := bus.Subscribe(ctx, model.TopicAlarmReported, "test", func(_ context.Context, b []byte) error {
				var a model.Alarm
				if err := json.Unmarshal(b, &a); err != nil {
					return err
				}
				reports = append(reports, a)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if kind == "rule" {
				if err := repo.SaveRule(ctx, model.AlarmRule{ID: "r", TenantID: "t", Name: "烟雾规则", Enabled: true, AlarmType: "FIRE", Level: "HIGH", Conditions: []model.RuleCondition{{Field: "smoke", Operator: "eq", Value: true}}}); err != nil {
					t.Fatal(err)
				}
			}
			send := func(id string, at int64, active bool, description string) {
				t.Helper()
				msg := model.StandardMessage{TenantID: "t", ProductID: "p", DeviceID: "d", MessageID: id, Timestamp: at, MessageType: model.AlarmReport, Properties: map[string]any{"smoke": active}, Event: map[string]any{"alarmType": "FIRE", "description": description}}
				if !active {
					msg.MessageType = model.PropertyReport
				}
				if kind == "component" {
					msg.MessageType = model.StateChange
					msg.Event["components"] = []model.ComponentStatus{{ID: "c", Name: "探测器", Location: "一楼", Alarms: map[string]bool{"FIRE": active}}}
				}
				if err := e.handleStandard(ctx, mustJSON(msg)); err != nil {
					t.Fatal(err)
				}
			}
			send("m1", 1000, true, "首次报警")
			send("m2", 2000, true, "再次报警")
			send("m2", 2000, true, "再次报警")
			if kind == "component" {
				send("stale", 500, true, "乱序旧报文")
			}
			if len(reports) != 2 {
				t.Fatalf("want 2 report events, got %d", len(reports))
			}
			if reports[0].ID != reports[1].ID || reports[0].TriggerID != "m1" || reports[1].TriggerID != "m2" {
				t.Fatalf("incorrect report identity: %+v", reports)
			}
			message := reports[1].Details["message"].(map[string]any)
			if message["event"].(map[string]any)["description"] != "再次报警" {
				t.Fatal("retained old alarm details")
			}
			alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t", Status: "ACTIVE"})
			if err != nil || len(alarms) != 1 || alarms[0].TriggerCount != 2 {
				t.Fatalf("changed alarm aggregation: %+v %v", alarms, err)
			}
			send("normal", 3000, false, "恢复")
			if len(reports) != 2 {
				t.Fatal("recovery must not send an alarm receipt")
			}
		})
	}
}

type failedAlarmReportBus struct{ *local.Bus }

func (b failedAlarmReportBus) Publish(ctx context.Context, topic, key string, payload []byte) error {
	if topic == model.TopicAlarmReported {
		return errors.New("report stream unavailable")
	}
	return b.Bus.Publish(ctx, topic, key, payload)
}

func TestAlarmReportPublishFailureIsRetryable(t *testing.T) {
	e := New(memory.NewRepository(), nil, failedAlarmReportBus{local.NewBus()}, local.NewRealtime(), nil, nil)
	_, _, err := e.raiseDirectAlarm(context.Background(), model.StandardMessage{TenantID: "t", DeviceID: "d", MessageID: "m", MessageType: model.AlarmReport})
	if err == nil {
		t.Fatal("notification stream failure was silently acknowledged")
	}
}
