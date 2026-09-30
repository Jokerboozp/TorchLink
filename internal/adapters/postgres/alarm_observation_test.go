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
func TestAlarmObservationUpsertLegacyIntentAndExplicitRetryFence(t *testing.T) {
	r := testRepository(t)
	ctx := context.Background()
	for _, explicit := range []bool{false, true} {
		deviceID, alarmID := "legacy", "legacy-alarm"
		counts := []int{1, 1, 2, 3}
		if explicit {
			deviceID, alarmID = "explicit", "explicit-alarm"
			counts = []int{1, 1, 2, 2}
		}
		for i, sourceID := range []string{"m1", "m1", "m2", "m2"} {
			msg := model.StandardMessage{MessageID: sourceID, RawMessageID: "raw-" + sourceID, TenantID: "t", DeviceID: deviceID, MessageType: model.PropertyReport, Timestamp: 700, Properties: map[string]any{"temperature": 90}}
			a := model.Alarm{ID: alarmID, TenantID: "t", DeviceID: deviceID, RuleID: "r", AlarmType: "FIRE", Status: "ACTIVE", TriggerID: sourceID, FirstTriggeredAt: int64(1000 + i), LastTriggeredAt: int64(1000 + i), TriggerCount: 1, Details: map[string]any{"message": msg}}
			callCtx := ctx
			if explicit {
				o := pgObservation("explicit-"+sourceID, "ASSERT", 700)
				o.DeviceID, o.EvaluationAt, o.RecordedAt = deviceID, int64(1000+i), int64(1000+i)
				o.Payload = a.Details
				callCtx = model.WithAlarmObservation(ctx, o)
			}
			saved, created, err := r.UpsertAlarm(callCtx, a)
			if err != nil || created != (i == 0) || saved.TriggerID != "m1" || saved.TriggerCount != counts[i] {
				t.Fatalf("explicit=%v report=%d: %#v created=%v err=%v", explicit, i, saved, created, err)
			}
			if explicit && i == 3 && saved.LastTriggeredAt != 1002 {
				t.Fatal("explicit retry changed aggregate time", saved)
			}
		}
		facts, err := r.ListAlarmObservations(ctx, "t", ports.AlarmObservationFilter{DeviceIDs: []string{deviceID}})
		if err != nil || len(facts) != 2 {
			t.Fatal("production compatibility duplicated immutable evidence", explicit, facts, err)
		}
	}
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
		facts, err = reader.ListAlarmObservations(ports.AlarmObservationFilter{DeviceIDs: []string{"d"}, TimeBasis: "RECORDED_AT", Start: 1399, End: 1401})
		if err != nil || len(facts) != 1 || facts[0].EventAt != 400 {
			return fmt.Errorf("recorded time read wrong: %#v %v", facts, err)
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
	o.RecordedAt = 86400000 * 5
	if _, _, err := r.SaveAlarmObservation(ctx, o); err != nil {
		t.Fatal(err)
	}
	point := model.GovernancePoint{DeviceID: "d", AlarmType: "FIRE", OriginKind: "DEVICE_DIRECT", SignalKey: "device:FIRE"}
	for _, view := range []struct {
		kind   string
		bucket int64
	}{{"OBSERVATION", 2 * 86400000}, {"OBSERVATION_RECEIVED", 3 * 86400000}, {"OBSERVATION_EVALUATION", 4 * 86400000}} {
		for _, key := range []string{point.Key(view.kind), (model.GovernancePoint{DeviceID: "d"}).Key(view.kind)} {
			var generation int64
			if err := r.pool.QueryRow(ctx, `SELECT generation FROM alarm_governance_source_version WHERE tenant_id=$1 AND dependency_key=$2 AND bucket_start=$3`, "t", key, view.bucket).Scan(&generation); err != nil || generation != 1 {
				t.Fatal(generation, err)
			}
		}
	}
	var count int
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM alarm_governance_source_version WHERE bucket_start=-1`).Scan(&count); err != nil || count != 0 {
		t.Fatal("known event invalidated every window", count, err)
	}
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM alarm_governance_source_version WHERE bucket_start=$1`, o.RecordedAt).Scan(&count); err != nil || count != 0 {
		t.Fatal("registration cutoff invalidated unrelated event windows", count, err)
	}
}
func TestAlarmObservationTimeDependencyNamespacesDoNotCrossInvalidate(t *testing.T) {
	r := testRepository(t)
	ctx := context.Background()
	const day = int64(86400000)
	o := pgObservation("late-known-event", "REPORT", day)
	o.ReceivedAt, o.EvaluationAt, o.RecordedAt = 2*day, 3*day, 2*day
	if _, _, err := r.SaveAlarmObservation(ctx, o); err != nil {
		t.Fatal(err)
	}
	point := model.GovernancePoint{DeviceID: "d", AlarmType: "FIRE", OriginKind: "DEVICE_DIRECT", SignalKey: "device:FIRE"}
	deps := []model.GovernanceSourceVersion{{DependencyKey: point.Key("OBSERVATION"), BucketStart: 2 * day}, {DependencyKey: point.Key("OBSERVATION_RECEIVED"), BucketStart: 2 * day}, {DependencyKey: point.Key("OBSERVATION"), BucketStart: -1}, {DependencyKey: point.Key("OBSERVATION_RECEIVED"), BucketStart: -1}}
	readVersions := func() []model.GovernanceSourceVersion {
		var out []model.GovernanceSourceVersion
		if err := r.GovernanceRead(ctx, "t", func(tx ports.AlarmGovernanceTx) error { var err error; out, err = tx.SourceVersions(deps); return err }); err != nil {
			t.Fatal(err)
		}
		ordered := make([]model.GovernanceSourceVersion, len(deps))
		for i, dep := range deps {
			for _, v := range out {
				if v.DependencyKey == dep.DependencyKey && v.BucketStart == dep.BucketStart {
					ordered[i] = v
				}
			}
		}
		return ordered
	}
	v := readVersions()
	if v[0].Generation != 0 || v[1].Generation != 1 || v[2].Generation != 0 || v[3].Generation != 0 {
		t.Fatal("known outside event expired event view or missed receipt view", v)
	}
	o.SourceEventID, o.SourceInputHash, o.TimeQuality = "unknown-clock", "unknown-clock", "UNVERIFIED"
	if _, _, err := r.SaveAlarmObservation(ctx, o); err != nil {
		t.Fatal(err)
	}
	v = readVersions()
	if v[2].Generation != 1 || v[3].Generation != 0 {
		t.Fatal("device clock uncertainty contaminated known reception", v)
	}
}
