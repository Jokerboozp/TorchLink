package opscenter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/core"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type notificationClock struct{ at time.Time }

func (c notificationClock) Now() time.Time { return c.at }

func TestDeviceNotificationAlarmLifecycle(t *testing.T) {
	for _, kind := range []string{"direct", "rule", "component"} {
		for _, status := range []string{"RECOVERED", "CLOSED"} {
			t.Run(kind+"/"+status, func(t *testing.T) {
				n, am, now := notificationFixture(t)
				ctx, cancel := context.WithCancel(context.Background())
				bus := local.NewBus()
				defer func() { cancel(); _ = bus.Close() }()
				repo := memory.NewRepository()
				engine := core.New(repo, nil, bus, local.NewRealtime(), nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
				engine.Clock = notificationClock{*now}
				if err := bus.Subscribe(ctx, model.TopicAlarmReported, "mail-test", n.enqueue); err != nil {
					t.Fatal(err)
				}
				if err := engine.Start(ctx); err != nil {
					t.Fatal(err)
				}
				if kind == "rule" {
					if err := repo.SaveRule(ctx, model.AlarmRule{ID: "r", TenantID: "t", Name: "烟雾规则", Enabled: true, AlarmType: "FIRE", Level: "HIGH", Conditions: []model.RuleCondition{{Field: "smoke", Operator: "eq", Value: true}}}); err != nil {
						t.Fatal(err)
					}
				}
				send := func(i int) {
					t.Helper()
					msg := model.StandardMessage{TenantID: "t", ProductID: "p", DeviceID: "d", MessageID: fmt.Sprintf("m%d", i), Timestamp: now.UnixMilli() + int64(i), MessageType: model.AlarmReport, Properties: map[string]any{"smoke": true}, Event: map[string]any{"alarmType": "FIRE", "description": "烟雾报警"}}
					if kind == "component" {
						msg.MessageType = model.StateChange
						msg.Event["components"] = []model.ComponentStatus{{ID: "c", Name: "探测器", Alarms: map[string]bool{"FIRE": true}}}
					}
					b, err := json.Marshal(msg)
					if err != nil {
						t.Fatal(err)
					}
					if err := bus.Publish(ctx, model.TopicParsed, msg.MessageID, b); err != nil {
						t.Fatal(err)
					}
				}
				flush := func(want int) {
					t.Helper()
					if err := n.flush(ctx); err != nil {
						t.Fatal(err)
					}
					if len(am.posted) != want {
						t.Fatalf("want %d notifications, got %d", want, len(am.posted))
					}
				}
				send(1)
				send(2)
				flush(1)
				alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t", Status: "ACTIVE"})
				if err != nil || len(alarms) != 1 || alarms[0].TriggerCount != 2 {
					t.Fatalf("incorrect active alarm: %+v %v", alarms, err)
				}
				originalID := alarms[0].ID
				if _, err := engine.SetAlarmStatus(ctx, "t", originalID, "ACKED", "test"); err != nil {
					t.Fatal(err)
				}
				send(3)
				flush(1)
				if _, err := engine.SetAlarmStatus(ctx, "t", originalID, status, "test"); err != nil {
					t.Fatal(err)
				}
				flush(1) // Resolving or closing does not itself send email.
				send(4)
				send(5)
				flush(2)
				alarms, err = repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t", Status: "ACTIVE"})
				if err != nil || len(alarms) != 1 || alarms[0].ID == originalID || alarms[0].TriggerCount != 2 {
					t.Fatalf("incorrect next alarm: %+v %v", alarms, err)
				}
			})
		}
	}
}
