package postgres

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"os"
	"sync"
	"testing"
	"time"
)

func TestComponentAlarmAtomicWatermarkAndRestart(t *testing.T) {
	dsn := os.Getenv("IOT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("IOT_TEST_POSTGRES_DSN not configured")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := fmt.Sprintf("component_test_%d", time.Now().UnixNano())
	ident := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = admin.Exec(ctx, "DROP SCHEMA "+ident+" CASCADE") }()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = name
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo := &Repository{pool: pool}
	if err = repo.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = repo.Migrate(ctx); err != nil {
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
	if _, _, err = repo.ApplyComponentAlarm(ctx, b, state); err != nil {
		t.Fatal(err)
	}
	pool.Close()
	pool, err = pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repo = &Repository{pool: pool}
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
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM component_alarm_state WHERE tenant_id='cancelled'`).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}
