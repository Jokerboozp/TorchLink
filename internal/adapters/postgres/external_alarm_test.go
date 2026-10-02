package postgres

import (
	"context"
	"errors"
	"testing"

	"iot-platform/internal/model"
	"iot-platform/internal/repositorytest"
)

func TestExternalAlarm(t *testing.T) {
	repositorytest.ExternalAlarm(t, testRepository(t))
}

func TestExternalAlarmOutboxFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	r := testRepository(t)
	v := model.Alarm{TenantID: "t", ID: "external_rollback", RuleID: "FIRE:external:rollback", DeviceID: "d", TriggerID: "one", Status: "ACTIVE", AlarmLevel: "HIGH", Source: "device", LastTriggeredAt: 1}
	if _, err := r.pool.Exec(ctx, `CREATE FUNCTION reject_external_alarm_outbox() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected external alarm outbox failure'; END $$; CREATE TRIGGER reject_external_alarm_outbox BEFORE INSERT ON event_outbox FOR EACH ROW EXECUTE FUNCTION reject_external_alarm_outbox()`); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := r.UpsertExternalAlarm(ctx, v); err == nil {
		t.Fatal("injected outbox failure was ignored")
	}
	if _, err := r.GetAlarm(ctx, v.TenantID, v.ID); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("creation survived outbox rollback: %v", err)
	}
	if _, err := r.pool.Exec(ctx, `ALTER TABLE event_outbox DISABLE TRIGGER reject_external_alarm_outbox`); err != nil {
		t.Fatal(err)
	}
	first, _, _, err := r.UpsertExternalAlarm(ctx, v)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.pool.Exec(ctx, `ALTER TABLE event_outbox ENABLE TRIGGER reject_external_alarm_outbox`); err != nil {
		t.Fatal(err)
	}
	v.TriggerID, v.LastTriggeredAt, v.AlarmLevel = "two", 2, "CRITICAL"
	if _, _, _, err = r.UpsertExternalAlarm(ctx, v); err == nil {
		t.Fatal("update ignored injected outbox failure")
	}
	after, err := r.GetAlarm(ctx, v.TenantID, v.ID)
	if err != nil || after.TriggerID != first.TriggerID || after.Version != first.Version || after.TriggerCount != first.TriggerCount || after.AlarmLevel != first.AlarmLevel {
		t.Fatalf("update survived outbox rollback: %+v %v", after, err)
	}
}
