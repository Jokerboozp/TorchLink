package postgres

import (
	"context"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/repositorytest"
)

func TestStandardCompletion(t *testing.T) { repositorytest.StandardCompletion(t, testRepository(t)) }

// TestOpenAlarmCounterFollowsEveryWrite checks the trigger-maintained
// counter, including alarms that existed before migration 12.
func TestOpenAlarmCounterFollowsEveryWrite(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	all, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	before := []migration{}
	for _, m := range all {
		if m.version < 12 {
			before = append(before, m)
		}
	}
	if err = migrateWith(ctx, pool, before); err != nil {
		t.Fatal(err)
	}
	insert := func(id, device, status string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO alarm_record(tenant_id,id,rule_id,device_id,status,level,source,last_triggered_at,body) VALUES('t',$1,$1,$2,$3,'HIGH','device',1,'{}')`, id, device, status); err != nil {
			t.Fatal(err)
		}
	}
	insert("old-open", "d1", "ACKED")
	insert("old-closed", "d1", "CLOSED")
	r := &Repository{pool: pool}
	if err = r.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	open := func(device string) bool {
		t.Helper()
		v, err := r.HasOpenAlarm(ctx, "t", device)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if !open("d1") || open("d2") {
		t.Fatal("backfill must count existing open alarms only")
	}
	for _, step := range []struct {
		sql  string
		want bool
	}{
		{`UPDATE alarm_record SET status='RECOVERED' WHERE id='old-open'`, false},
		{`UPDATE alarm_record SET status='ACTIVE' WHERE id='old-closed'`, true},
		{`UPDATE alarm_record SET status='ACKED' WHERE id='old-closed'`, true},
		{`DELETE FROM alarm_record WHERE id='old-closed'`, false},
	} {
		if _, err = pool.Exec(ctx, step.sql); err != nil {
			t.Fatal(err)
		}
		if open("d1") != step.want {
			t.Fatalf("%s: open=%v", step.sql, !step.want)
		}
	}
	// The repository's own alarm writes go through the same trigger.
	a := model.Alarm{ID: "new", TenantID: "t", DeviceID: "d2", RuleID: "r", Status: "ACTIVE", TriggerID: "x", LastTriggeredAt: 1}
	if _, _, err = r.UpsertAlarm(ctx, a); err != nil || !open("d2") {
		t.Fatalf("upsert: %v", err)
	}
}
