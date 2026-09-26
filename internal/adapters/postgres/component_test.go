package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func TestComponentAlarmAtomicWatermarkAndRestart(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	if err := repo.Migrate(ctx); err != nil {
		t.Fatal("migration not idempotent", err)
	}
	a := model.Alarm{ID: "a", TenantID: "t", DeviceID: "d", ComponentID: "c", ComponentName: "探测器", ComponentLocation: "三楼", RuleID: "device-report:FIRE:component:c", AlarmType: "FIRE", AlarmLevel: "HIGH", Status: "ACTIVE", Source: "device", FirstTriggeredAt: 1000, LastTriggeredAt: 1000, TriggerCount: 1, TriggerID: "m1"}
	state := model.ComponentAlarmState{Timestamp: 1000, MessageID: "m1", Active: true}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			saved, event, err := repo.ApplyComponentAlarm(ctx, a, state)
			if err != nil || saved.ID != "a" || event != "raised" || saved.TriggerCount != 1 {
				t.Errorf("concurrent receive: %+v %s %v", saved, event, err)
			}
		}()
	}
	wg.Wait()
	// Another component under the same controller stays active.
	b := a
	b.ID = "b"
	b.ComponentID = "other"
	b.RuleID = "device-report:FIRE:component:other"
	if _, _, err := repo.ApplyComponentAlarm(ctx, b, state); err != nil {
		t.Fatal(err)
	}
	a.LastTriggeredAt = 2000
	state = model.ComponentAlarmState{Timestamp: 2000, MessageID: "m2", Active: false}
	saved, event, err := repo.ApplyComponentAlarm(ctx, a, state)
	if err != nil || saved.Status != "RECOVERED" || event != "recovered" {
		t.Fatal(saved, event, err)
	}
	state = model.ComponentAlarmState{Timestamp: 1500, MessageID: "old", Active: true}
	saved, event, err = repo.ApplyComponentAlarm(ctx, a, state)
	if err != nil || saved.Status != "RECOVERED" || event != "" {
		t.Fatal("stale resurrected alarm", saved, event, err)
	}
	active, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "t", DeviceID: "d", Status: "ACTIVE", Limit: 100})
	if err != nil || len(active) != 1 || active[0].ComponentID != "other" {
		t.Fatal(active, err)
	}
	// Same IDs in another tenant are independent.
	a.TenantID = "other-tenant"
	a.ID = "a"
	state = model.ComponentAlarmState{Timestamp: 1000, MessageID: "m1", Active: true}
	if _, event, err = repo.ApplyComponentAlarm(ctx, a, state); err != nil || event != "raised" {
		t.Fatal(event, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	a.TenantID = "cancelled"
	if _, _, err = repo.ApplyComponentAlarm(cancelled, a, state); err == nil {
		t.Fatal("cancelled transaction succeeded")
	}
	var count int
	if err = repo.pool.QueryRow(ctx, `SELECT count(*) FROM component_alarm_state WHERE tenant_id='cancelled'`).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	// Only accepted active reports were committed to the outbox: a, b and the
	// other tenant's a; concurrent duplicates, recovery and stale reports were not.
	var keys []string
	if _, err = repo.DrainOutbox(ctx, 10, func(v model.OutboxEvent) error { keys = append(keys, v.Key); return nil }); err != nil || strings.Join(keys, ",") != "a,b,a" {
		t.Fatal(keys, err)
	}
}

func TestAlarmOutboxAndDeviceSetFilter(t *testing.T) {
	ctx := context.Background()
	repo := testRepository(t)
	a := model.Alarm{ID: "a", TenantID: "t", DeviceID: "d1", RuleID: "r", TriggerID: "m1", Status: "ACTIVE", Source: "device", LastTriggeredAt: 1}
	for _, trigger := range []string{"m1", "m1", "m2"} {
		a.TriggerID = trigger
		if _, _, err := repo.UpsertAlarm(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	a.ID, a.DeviceID = "b", "d2"
	if _, _, err := repo.UpsertAlarm(ctx, a); err != nil {
		t.Fatal(err)
	}
	var triggers []string
	collect := func(v model.OutboxEvent) error {
		var report model.Alarm
		_ = json.Unmarshal(v.Payload, &report)
		triggers = append(triggers, report.ID+":"+report.TriggerID)
		return nil
	}
	if n, err := repo.DrainOutbox(ctx, 10, func(model.OutboxEvent) error { return errors.New("bus down") }); n != 0 || err == nil {
		t.Fatal("failed publish removed events", n, err)
	}
	if n, err := repo.DrainOutbox(ctx, 10, collect); n != 3 || err != nil || strings.Join(triggers, ",") != "a:m1,a:m2,b:m2" {
		t.Fatal(triggers, n, err)
	}
	if n, _ := repo.DrainOutbox(ctx, 10, collect); n != 0 {
		t.Fatal("published events retained")
	}
	for _, tc := range []struct {
		ids  []string
		want int
	}{{nil, 2}, {[]string{"d2", "missing"}, 1}, {[]string{}, 0}} {
		f := ports.AlarmFilter{TenantID: "t", DeviceIDs: tc.ids}
		items, err := repo.ListAlarms(ctx, f)
		total, countErr := repo.CountAlarms(ctx, f)
		if err != nil || countErr != nil || len(items) != tc.want || total != tc.want {
			t.Fatal(tc.ids, items, total, err, countErr)
		}
	}
}
