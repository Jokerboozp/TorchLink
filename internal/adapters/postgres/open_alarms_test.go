package postgres

import (
	"context"
	"testing"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/repositorytest"
)

func TestStandardCompletion(t *testing.T) { repositorytest.StandardCompletion(t, testRepository(t)) }

func TestAlarmReports(t *testing.T)   { repositorytest.AlarmReports(t, testRepository(t)) }
func TestDeviceOverview(t *testing.T) { repositorytest.DeviceOverview(t, testRepository(t)) }

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

// While ClickHouse keeps telemetry properties, telemetry rows store none;
// other message types keep theirs.
func TestExternalTelemetryProperties(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	r.SetExternalTelemetryProperties(true)
	for _, msg := range []model.StandardMessage{
		{TenantID: "t", MessageID: "prop", RawMessageID: "raw-prop", ProductID: "p", DeviceID: "d", MessageType: model.PropertyReport, Timestamp: 1, Properties: map[string]any{"v": 1}},
		{TenantID: "t", MessageID: "event", RawMessageID: "raw-event", ProductID: "p", DeviceID: "d", MessageType: model.EventReport, Timestamp: 2, Properties: map[string]any{"v": 2}, Event: map[string]any{"type": "x"}},
	} {
		if claim, err := r.ClaimStandardMessage(ctx, msg, "w", time.Minute); err != nil || !claim.Created {
			t.Fatalf("claim %s: %+v %v", msg.MessageID, claim, err)
		}
	}
	var props, bodyProps string
	if err := r.pool.QueryRow(ctx, `SELECT properties::text, coalesce(body->>'properties','') FROM standard_message WHERE message_id='prop'`).Scan(&props, &bodyProps); err != nil || props != "{}" || bodyProps != "" {
		t.Fatalf("telemetry row kept properties: %q %q %v", props, bodyProps, err)
	}
	if got, err := r.GetStandardMessageByRaw(ctx, "t", "raw-event"); err != nil || got.Properties["v"] != float64(2) || got.Event["type"] != "x" {
		t.Fatalf("event message: %+v %v", got, err)
	}
	if got, err := r.GetStandardMessageByRaw(ctx, "t", "raw-prop"); err != nil || got.MessageType != model.PropertyReport || got.DeviceID != "d" {
		t.Fatalf("index columns of the telemetry row: %+v %v", got, err)
	}
}
