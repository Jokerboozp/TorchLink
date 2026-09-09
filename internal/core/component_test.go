package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
	"log/slog"
	"testing"
)

func TestComponentAlarmLifecycleAndRecoveryIsolation(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	archive, _ := local.NewArchive(t.TempDir())
	e := New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	send := func(id string, at int64, components ...model.ComponentStatus) {
		t.Helper()
		m := model.StandardMessage{MessageID: id, TenantID: "t", ProductID: "p", DeviceID: "controller", MessageType: model.StateChange, Timestamp: at, Event: map[string]any{"components": components}}
		b, _ := json.Marshal(m)
		if err := e.handleStandard(ctx, b); err != nil {
			t.Fatal(err)
		}
	}
	c := func(id string, fire, fault bool) model.ComponentStatus {
		return model.ComponentStatus{ID: id, Name: "探测器", Location: "二楼走廊", Alarms: map[string]bool{"FIRE": fire, "DEVICE_FAULT": fault}}
	}
	active := func(n int) {
		t.Helper()
		alarms, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t", DeviceID: "controller", Status: "ACTIVE", Limit: 100})
		if err != nil || len(alarms) != n {
			t.Fatalf("active=%+v err=%v want=%d", alarms, err, n)
		}
	}
	send("m1", 1000, c("A", false, false), c("B", true, true))
	active(2)
	send("normal-A", 2000, c("A", false, false))
	active(2)
	// Registration/connection status cannot recover either component.
	b, _ := json.Marshal(model.StandardMessage{MessageID: "online", TenantID: "t", ProductID: "p", DeviceID: "controller", MessageType: model.StateChange, Timestamp: 2500, Properties: map[string]any{"connectionStatus": "CONNECTED"}})
	if err := e.handleStandard(ctx, b); err != nil {
		t.Fatal(err)
	}
	active(2)
	// Recovery of one category must leave the other category active.
	send("recover-fire", 3000, model.ComponentStatus{ID: "B", Alarms: map[string]bool{"FIRE": false}})
	active(1)
	send("old-alarm", 1500, c("B", true, true))
	active(1)
	send("same-time-alarm", 3000, model.ComponentStatus{ID: "B", Alarms: map[string]bool{"FIRE": true}})
	active(2)
	send("same-time-normal", 3000, c("B", false, false))
	active(1)
	send("recover-all", 4000, c("B", false, false))
	active(0)
	send("late-replay", 1000, c("B", true, true))
	active(0)
	send("new-cycle", 5000, c("B", true, false))
	active(1)
	send("new-cycle", 5000, c("B", true, false))
	active(1)
	alarms, _ := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t", Status: "ACTIVE", Limit: 100})
	a := alarms[0]
	if a.ComponentID != "B" || a.ComponentLocation != "二楼走廊" || a.TriggerCount != 1 {
		t.Fatalf("bad component alarm %+v", a)
	}
	a.Status = "ACKED"
	if err := repo.UpdateAlarm(ctx, a); err != nil {
		t.Fatal(err)
	}
	send("acked-recovery", 6000, c("B", false, false))
	active(0)
	got, _ := repo.GetAlarm(ctx, "t", a.ID)
	if got.Status != "RECOVERED" {
		t.Fatal(got)
	}
	// More than the old 100-row recovery limit, each addressed independently.
	for batch := 0; batch < 2; batch++ {
		items := []model.ComponentStatus{}
		for i := 0; i < 80; i++ {
			items = append(items, c(fmt.Sprintf("many-%d-%d", batch, i), true, false))
		}
		send(fmt.Sprint("many", batch), 7000, items...)
	}
	for batch := 0; batch < 2; batch++ {
		items := []model.ComponentStatus{}
		for i := 0; i < 80; i++ {
			items = append(items, c(fmt.Sprintf("many-%d-%d", batch, i), false, false))
		}
		send(fmt.Sprint("clear", batch), 8000, items...)
	}
	active(0)
}

func TestInvalidComponentsAndPlainStateCannotClearFire(t *testing.T) {
	for _, value := range []any{nil, []any{}, []any{map[string]any{"id": "x", "alarms": map[string]any{"FIRE": nil}}}, []any{map[string]any{"id": "x", "alarms": map[string]any{"FIRE": "false"}}}, []model.ComponentStatus{{ID: "x", Alarms: map[string]bool{"FIRE": true}}, {ID: "x", Alarms: map[string]bool{"FIRE": false}}}} {
		if _, err := model.MessageComponents(model.StandardMessage{MessageType: model.AlarmReport, Timestamp: 1000, Event: map[string]any{"components": value}}); err == nil {
			t.Fatalf("accepted invalid %#v", value)
		}
	}
	if directAlarmCleared(model.StandardMessage{MessageType: model.StateChange, Properties: map[string]any{"connectionStatus": "CONNECTED"}}) {
		t.Fatal("connection cleared alarm")
	}
	if directAlarmTypeCleared(model.StandardMessage{Properties: map[string]any{"fault": false}}, "FIRE") {
		t.Fatal("fault recovery cleared fire")
	}
}
