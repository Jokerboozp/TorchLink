package postgres

import (
	"context"
	"errors"
	"fmt"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"testing"
)

func pgObservation(source, kind string, at int64) model.AlarmObservation {
	return model.AlarmObservation{TenantID: "t", DeviceID: "d", SourceSystem: "protocol", SourceEventID: source, SourceInputHash: source, EventIndex: "fire", AlarmType: "FIRE", OriginKind: "DEVICE_DIRECT", SignalKey: "device:FIRE", FactKind: kind, EventAt: at, TimeQuality: "TRUSTED", HistoricalQuality: "LIVE", RecordedAt: at + 1000, EvaluationAt: at + 1000}
}
func TestAlarmObservationPostgresDedupConflictAndIncoming(t *testing.T) {
	r := testRepository(t)
	ctx := context.Background()
	first := pgObservation("first", "ASSERT", 100)
	saved, newFact, err := r.SaveAlarmObservation(ctx, first)
	if err != nil || !newFact {
		t.Fatal(saved, newFact, err)
	}
	retry := first
	retry.RecordedAt = 9999
	retry.EvaluationAt = 9999
	retry.Acceptance = "REJECTED"
	got, newFact, err := r.SaveAlarmObservation(ctx, retry)
	if err != nil || newFact || got.Acceptance != "ACCEPTED" {
		t.Fatal(got, newFact, err)
	}
	changed := first
	changed.FactKind = "CLEAR"
	got, newFact, err = r.SaveAlarmObservation(ctx, changed)
	if err != nil || newFact || got.Acceptance != "CONFLICT" {
		t.Fatal(got, newFact, err)
	}
	changed = first
	changed.ComponentID = "other"
	changed.SourceInputHash = "changed"
	got, newFact, err = r.SaveAlarmObservation(ctx, changed)
	if err != nil || newFact || got.Acceptance != "CONFLICT" {
		t.Fatal(got, newFact, err)
	}
	var count int
	if err = r.pool.QueryRow(ctx, `SELECT count(*) FROM alarm_observation_conflict`).Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	facts, err := r.ListAlarmObservations(ctx, "t", ports.AlarmObservationFilter{DeviceIDs: []string{"d"}})
	if err != nil || len(facts) != 1 {
		t.Fatal(facts, err)
	}
	facts, err = r.ListAlarmObservations(ctx, "t", ports.AlarmObservationFilter{DeviceIDs: []string{}})
	if err != nil || len(facts) != 0 {
		t.Fatal(facts, err)
	}
}
func TestAlarmObservationFailureRollsBackProductionAlarm(t *testing.T) {
	r := testRepository(t)
	ctx := context.Background()
	_, err := r.pool.Exec(ctx, `CREATE FUNCTION fail_observation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'observation unavailable'; END $$; CREATE TRIGGER fail_observation BEFORE INSERT ON alarm_observation FOR EACH ROW EXECUTE FUNCTION fail_observation()`)
	if err != nil {
		t.Fatal(err)
	}
	a := model.Alarm{ID: "a", TenantID: "t", DeviceID: "d", RuleID: "r", AlarmType: "FIRE", Status: "ACTIVE", TriggerID: "one", TriggerCount: 1}
	_, _, err = r.UpsertAlarm(model.WithAlarmObservation(ctx, pgObservation("one", "ASSERT", 100)), a)
	if err == nil {
		t.Fatal("fact failure swallowed")
	}
	_, err = r.GetAlarm(ctx, "t", "a")
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatal("alarm committed without fact", err)
	}
	var count int
	r.pool.QueryRow(ctx, `SELECT count(*) FROM event_outbox`).Scan(&count)
	if count != 0 {
		t.Fatal("outbox escaped rollback")
	}
}
func TestAlarmObservationRecoveryAndScopedSnapshot(t *testing.T) {
	r := testRepository(t)
	ctx := context.Background()
	o := pgObservation("normal", "CLEAR", 100)
	o.ReceivedAt = 5000
	alarms, err := r.RecoverAlarmSignal(ctx, o, "r")
	if err != nil || len(alarms) != 0 {
		t.Fatal(alarms, err)
	}
	a := model.Alarm{ID: "a", TenantID: "t", DeviceID: "d", RuleID: "r", AlarmType: "FIRE", Status: "ACTIVE", TriggerID: "assert", TriggerCount: 1}
	_, _, err = r.UpsertAlarm(model.WithAlarmObservation(ctx, pgObservation("assert", "ASSERT", 300)), a)
	if err != nil {
		t.Fatal(err)
	}
	alarms, err = r.RecoverAlarmSignal(ctx, pgObservation("late", "CLEAR", 200), "r")
	if err != nil || len(alarms) != 0 {
		t.Fatal(alarms, err)
	}
	current, _ := r.GetAlarm(ctx, "t", "a")
	if current.Status != "ACTIVE" {
		t.Fatal(current)
	}
	alarms, err = r.RecoverAlarmSignal(ctx, pgObservation("clear", "CLEAR", 400), "r")
	if err != nil || len(alarms) != 1 {
		t.Fatal(alarms, err)
	}
	err = r.GovernanceRead(ctx, "t", func(tx ports.AlarmGovernanceTx) error {
		reader, ok := tx.(ports.AlarmObservationReader)
		if !ok {
			return fmt.Errorf("reader missing")
		}
		facts, err := reader.ListAlarmObservations(ports.AlarmObservationFilter{DeviceIDs: []string{"d"}, TimeBasis: "RECEIVED_AT", Start: 4000, End: 6000})
		if err != nil {
			return err
		}
		if len(facts) != 1 || facts[0].FactKind != "CLEAR" {
			return fmt.Errorf("received time read wrong: %#v", facts)
		}
		seed, err := reader.GetAlarmSignalSeed(ports.AlarmObservationFilter{DeviceIDs: []string{"d"}, TimeBasis: "EVALUATION_AT"}, 1401)
		if err != nil || seed.EventAt != 400 {
			return fmt.Errorf("evaluation seed: %#v %v", seed, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestAlarmObservationVersionsIncludeKnownTimeViewsAndDeviceDiscovery(t *testing.T) {
	r := testRepository(t)
	ctx := context.Background()
	o := pgObservation("one", "REPORT", 86400000*2)
	o.ReceivedAt = 86400000 * 3
	o.EvaluationAt = 86400000 * 4
	if _, _, err := r.SaveAlarmObservation(ctx, o); err != nil {
		t.Fatal(err)
	}
	point := model.GovernancePoint{DeviceID: "d", AlarmType: "FIRE", OriginKind: "DEVICE_DIRECT", SignalKey: "device:FIRE"}
	for _, key := range []string{point.Key("OBSERVATION"), (model.GovernancePoint{DeviceID: "d"}).Key("OBSERVATION")} {
		for _, bucket := range []int64{2 * 86400000, 3 * 86400000, 4 * 86400000} {
			var generation int64
			if err := r.pool.QueryRow(ctx, `SELECT generation FROM alarm_governance_source_version WHERE tenant_id=$1 AND dependency_key=$2 AND bucket_start=$3`, "t", key, bucket).Scan(&generation); err != nil || generation != 1 {
				t.Fatal(generation, err)
			}
		}
	}
	var count int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM alarm_governance_source_version WHERE bucket_start=-1`).Scan(&count); err != nil || count != 0 {
		t.Fatal("known event invalidated every window", count, err)
	}
}
