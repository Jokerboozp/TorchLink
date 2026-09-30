package memory

import (
	"context"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"testing"
)

func observation(source, kind string, at int64) model.AlarmObservation {
	return model.AlarmObservation{TenantID: "t", DeviceID: "d", SourceSystem: "protocol", SourceEventID: source, SourceInputHash: source, EventIndex: "fire", AlarmType: "FIRE", OriginKind: "DEVICE_DIRECT", SignalKey: "device:FIRE", FactKind: kind, EventAt: at, TimeQuality: "TRUSTED", HistoricalQuality: "LIVE", RecordedAt: at + 1000, EvaluationAt: at + 1000}
}
func TestObservationTimeViewsInvalidateOnlyTheirOwnFrozenWindow(t *testing.T) {
	r := NewRepository()
	ctx := context.Background()
	const day = int64(86400000)
	point := model.GovernancePoint{DeviceID: "d", AlarmType: "FIRE", OriginKind: "DEVICE_DIRECT", SignalKey: "device:FIRE"}
	deps := []model.GovernanceSourceVersion{{DependencyKey: point.Key("OBSERVATION"), BucketStart: 2 * day}, {DependencyKey: point.Key("OBSERVATION_RECEIVED"), BucketStart: 2 * day}, {DependencyKey: point.Key("OBSERVATION"), BucketStart: -1}, {DependencyKey: point.Key("OBSERVATION_RECEIVED"), BucketStart: -1}}
	readVersions := func() []model.GovernanceSourceVersion {
		var out []model.GovernanceSourceVersion
		if err := r.GovernanceRead(ctx, "t", func(tx ports.AlarmGovernanceTx) error { var err error; out, err = tx.SourceVersions(deps); return err }); err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := readVersions()
	o := observation("late-known-event", "REPORT", day)
	o.ReceivedAt, o.EvaluationAt, o.RecordedAt = 2*day, 3*day, 2*day
	if _, _, err := r.SaveAlarmObservation(ctx, o); err != nil {
		t.Fatal(err)
	}
	after := readVersions()
	if after[0].Generation != before[0].Generation || after[1].Generation != before[1].Generation+1 || after[2].Generation != before[2].Generation || after[3].Generation != before[3].Generation {
		t.Fatal("known late receipt invalidated event window or missed receipt window", before, after)
	}
	o.SourceEventID, o.SourceInputHash, o.TimeQuality = "unknown-device-clock", "unknown-device-clock", "UNVERIFIED"
	if _, _, err := r.SaveAlarmObservation(ctx, o); err != nil {
		t.Fatal(err)
	}
	unknown := readVersions()
	if unknown[2].Generation != after[2].Generation+1 || unknown[3].Generation != after[3].Generation {
		t.Fatal("device clock uncertainty contaminated known platform receipt", after, unknown)
	}
}
func TestAlarmObservationSourceSlotConflictAndAttempts(t *testing.T) {
	r := NewRepository()
	ctx := context.Background()
	first := observation("one", "ASSERT", 100)
	saved, created, err := r.SaveAlarmObservation(ctx, first)
	if err != nil || !created {
		t.Fatalf("first: %v %v", created, err)
	}
	retry := first
	retry.RecordedAt = 2000
	retry.EvaluationAt = 2000
	retry.Acceptance = "REJECTED"
	retry.AlarmID = "another"
	got, created, err := r.SaveAlarmObservation(ctx, retry)
	if err != nil || created || got.ID != saved.ID || got.Acceptance != "ACCEPTED" {
		t.Fatalf("retry changed immutable fact: %#v %v", got, err)
	}
	changed := first
	changed.FactKind = "CLEAR"
	got, created, err = r.SaveAlarmObservation(ctx, changed)
	if err != nil || created || got.Acceptance != "CONFLICT" {
		t.Fatalf("changed signal: %#v %v", got, err)
	}
	changed = first
	changed.ComponentID = "other"
	changed.SourceInputHash = "mutated-input"
	got, created, err = r.SaveAlarmObservation(ctx, changed)
	if err != nil || created || got.Acceptance != "CONFLICT" {
		t.Fatalf("changed slot bypass: %#v %v", got, err)
	}
	next := first
	next.SourceEventID = "two"
	next.SourceInputHash = "two"
	_, created, err = r.SaveAlarmObservation(ctx, next)
	if err != nil || !created {
		t.Fatal("distinct source with equal signal lost", err)
	}
	facts, err := r.ListAlarmObservations(ctx, "t", ports.AlarmObservationFilter{DeviceIDs: []string{"d"}})
	if err != nil || len(facts) != 2 || len(r.alarmObservations.Conflicts) != 2 || len(r.alarmObservations.Attempts) != 1 {
		t.Fatalf("facts/conflicts/attempts: %d %d %d %v", len(facts), len(r.alarmObservations.Conflicts), len(r.alarmObservations.Attempts), err)
	}
	facts, _ = r.ListAlarmObservations(ctx, "t", ports.AlarmObservationFilter{DeviceIDs: []string{}})
	if len(facts) != 0 {
		t.Fatal("empty device scope read all observations")
	}
}
func TestAlarmObservationUpsertRetainsEachIncoming(t *testing.T) {
	r := NewRepository()
	ctx := context.Background()
	a := model.Alarm{TenantID: "t", DeviceID: "d", ID: "a", RuleID: "r", AlarmType: "FIRE", Status: "ACTIVE", TriggerID: "one", TriggerCount: 1, LastTriggeredAt: 100, Details: map[string]any{"value": "first"}}
	first := observation("one", "ASSERT", 100)
	first.Payload = a.Details
	if _, _, err := r.UpsertAlarm(model.WithAlarmObservation(ctx, first), a); err != nil {
		t.Fatal(err)
	}
	a.ID = "unused"
	a.TriggerID = "two"
	a.LastTriggeredAt = 200
	a.Details = map[string]any{"value": "second"}
	second := observation("two", "ASSERT", 200)
	second.Payload = a.Details
	saved, newAlarm, err := r.UpsertAlarm(model.WithAlarmObservation(ctx, second), a)
	if err != nil || newAlarm || saved.TriggerCount != 2 {
		t.Fatalf("aggregate regression: %#v %v", saved, err)
	}
	facts, _ := r.ListAlarmObservations(ctx, "t", ports.AlarmObservationFilter{DeviceIDs: []string{"d"}})
	if len(facts) != 2 || facts[1].Payload["value"] != "second" || facts[1].AlarmID != "a" {
		t.Fatalf("incoming evidence lost: %#v", facts)
	}
	// The aggregate intentionally retains its original TriggerID. Replaying a
	// later incoming identity must still be fenced by its explicit observation.
	a.LastTriggeredAt = 300
	second.EvaluationAt, second.RecordedAt = 1300, 1300
	retried, newAlarm, err := r.UpsertAlarm(model.WithAlarmObservation(ctx, second), a)
	if err != nil || newAlarm || retried.TriggerCount != 2 || retried.LastTriggeredAt != 200 || retried.Version != saved.Version {
		t.Fatalf("explicit report retry repeated aggregation: %#v %v", retried, err)
	}
	if len(r.alarmObservations.Observations) != 2 || len(r.alarmObservations.Attempts) != 1 {
		t.Fatal("explicit retry must retain one immutable fact and its attempt")
	}
}
func TestAtomicRecoveryKeepsNormalSeedAndFencesLateClear(t *testing.T) {
	r := NewRepository()
	ctx := context.Background()
	clear := observation("normal", "CLEAR", 100)
	alarms, err := r.RecoverAlarmSignal(ctx, clear, "r")
	if err != nil || len(alarms) != 0 {
		t.Fatal(alarms, err)
	}
	seed, err := r.GetAlarmSignalSeed(ctx, "t", ports.AlarmObservationFilter{DeviceIDs: []string{"d"}, SignalKey: "device:FIRE"}, 101)
	if err != nil || seed.FactKind != "CLEAR" {
		t.Fatalf("normal seed missing: %#v %v", seed, err)
	}
	assert := observation("active", "ASSERT", 300)
	a := model.Alarm{ID: "a", TenantID: "t", DeviceID: "d", RuleID: "r", AlarmType: "FIRE", TriggerID: "active", Status: "ACTIVE", TriggerCount: 1}
	if _, _, err := r.UpsertAlarm(model.WithAlarmObservation(ctx, assert), a); err != nil {
		t.Fatal(err)
	}
	alarms, err = r.RecoverAlarmSignal(ctx, observation("late", "CLEAR", 200), "r")
	if err != nil || len(alarms) != 0 {
		t.Fatal(alarms, err)
	}
	current, _ := r.GetAlarm(ctx, "t", "a")
	if current.Status != "ACTIVE" {
		t.Fatal("old clear recovered newer alarm")
	}
	facts, _ := r.ListAlarmObservations(ctx, "t", ports.AlarmObservationFilter{DeviceIDs: []string{"d"}})
	if len(facts) != 3 || facts[1].Acceptance != "REJECTED" {
		t.Fatalf("late clear evidence absent: %#v", facts)
	}
	alarms, err = r.RecoverAlarmSignal(ctx, observation("recovery", "CLEAR", 400), "r")
	if err != nil || len(alarms) != 1 || alarms[0].Status != "RECOVERED" {
		t.Fatal(alarms, err)
	}
}
func TestComponentRejectedAndNormalObservationRetained(t *testing.T) {
	r := NewRepository()
	ctx := context.Background()
	a := model.Alarm{ID: "a", TenantID: "t", DeviceID: "d", ComponentID: "c", RuleID: "r", AlarmType: "FIRE", Status: "ACTIVE", TriggerID: "normal", TriggerCount: 1}
	apply := func(source string, at int64, active bool) {
		t.Helper()
		a.TriggerID = source
		o := observation(source, "CLEAR", at)
		o.ComponentID = "c"
		o.OriginKind = "COMPONENT_STATE"
		o.SignalKey = "component:c:FIRE"
		if active {
			o.FactKind = "ASSERT"
		}
		if _, _, err := r.ApplyComponentAlarm(model.WithAlarmObservation(ctx, o), a, model.ComponentAlarmState{Timestamp: at, MessageID: source, Active: active}); err != nil {
			t.Fatal(err)
		}
	}
	apply("normal", 100, false)
	apply("assert", 300, true)
	apply("late", 200, false)
	apply("equal", 300, false)
	apply("clear", 400, false)
	facts, _ := r.ListAlarmObservations(ctx, "t", ports.AlarmObservationFilter{DeviceIDs: []string{"d"}})
	if len(facts) != 5 {
		t.Fatal("missing source states", facts)
	}
	rejected := 0
	for _, f := range facts {
		if f.Acceptance == "REJECTED" {
			rejected++
		}
	}
	if rejected != 2 {
		t.Fatal("stale/equal decisions lost", facts)
	}
	current, _ := r.GetAlarm(ctx, "t", "a")
	if current.Status != "RECOVERED" {
		t.Fatal(current)
	}
}

func TestComponentObservationIdentityUsesIncomingWatermark(t *testing.T) {
	r := NewRepository()
	ctx := context.Background()
	a := model.Alarm{ID: "a", TenantID: "t", DeviceID: "d", ComponentID: "c", RuleID: "device-report:FIRE:component:c", AlarmType: "FIRE", Status: "ACTIVE", TriggerID: "m1", LastTriggeredAt: 1000, TriggerCount: 1}
	if _, _, err := r.ApplyComponentAlarm(ctx, a, model.ComponentAlarmState{Timestamp: 1000, MessageID: "m1", Active: true}); err != nil {
		t.Fatal(err)
	}
	// A repository caller may retain the aggregate's previous TriggerID.
	a.LastTriggeredAt = 2000
	saved, event, err := r.ApplyComponentAlarm(ctx, a, model.ComponentAlarmState{Timestamp: 2000, MessageID: "m2", Active: false})
	if err != nil || saved.Status != "RECOVERED" || event != "recovered" {
		t.Fatal(saved, event, err)
	}
	facts, err := r.ListAlarmObservations(ctx, "t", ports.AlarmObservationFilter{DeviceIDs: []string{"d"}})
	if err != nil || len(facts) != 2 || facts[0].SourceEventID != "m1" || facts[1].SourceEventID != "m2" || facts[1].FactKind != "CLEAR" || facts[1].Acceptance != "ACCEPTED" {
		t.Fatal("incoming component identity lost", facts, err)
	}
}
