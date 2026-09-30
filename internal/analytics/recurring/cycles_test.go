package recurring

import (
	"iot-platform/internal/model"
	"testing"
)

func fact(id, kind string, at int64) model.AlarmObservation {
	return model.AlarmObservation{ID: id, TenantID: "t", DeviceID: "d", ComponentID: "c", AssetInstanceID: "asset", AlarmType: "FIRE", OriginKind: "COMPONENT_STATE", SignalKey: "component:c:FIRE", FactKind: kind, EventAt: at, RecordedAt: at, EvaluationAt: at, TimeQuality: "TRUSTED", Acceptance: "ACCEPTED", HistoricalQuality: "LIVE", IdentityQuality: "PLATFORM_INPUT", ProtocolVersion: "v1"}
}
func cycleInput(observations ...model.AlarmObservation) CycleInput {
	return CycleInput{TenantID: "t", Method: "COMPONENT_BOOLEAN", Start: 100, End: 1000, Observations: observations, SeedMaxAge: 1000, Coverage: []CycleCoverage{{Start: 1, End: 1000, Reliable: true}}}
}
func TestCyclesReportOnlyNeverInventsRecovery(t *testing.T) {
	in := cycleInput(fact("a", "ASSERT", 200), fact("b", "CLEAR", 400))
	in.Method = "REPORT_ONLY"
	if got := RebuildCycles(in); len(got) != 0 {
		t.Fatal(got)
	}
}
func TestCyclesKnownStartsAndCensoring(t *testing.T) {
	seed := fact("seed", "CLEAR", 50)
	in := cycleInput(fact("a", "ASSERT", 200), fact("repeat", "ASSERT", 300), fact("clear", "CLEAR", 500), fact("open", "ASSERT", 800))
	in.Seed = &seed
	got := RebuildCycles(in)
	if len(got) != 2 || !got[0].NewStart || got[0].BoundaryQuality != "COMPLETE" || got[0].DurationMillis == nil || *got[0].DurationMillis != 300 || got[1].Status != "OPEN" || got[1].BoundaryQuality != "RIGHT_CENSORED" || got[1].KnownDurationLowerBound == nil || *got[1].KnownDurationLowerBound != 200 {
		t.Fatalf("%#v", got)
	}
}
func TestCyclesUnknownStartDoesNotCountAsNew(t *testing.T) {
	in := cycleInput(fact("a", "ASSERT", 200), fact("clear", "CLEAR", 500), fact("new", "ASSERT", 600))
	got := RebuildCycles(in)
	if len(got) != 2 || got[0].NewStart || got[0].BoundaryQuality != "LEFT_CENSORED" || got[0].DurationMillis != nil || !got[1].NewStart {
		t.Fatalf("%#v", got)
	}
}
func TestCyclesSeedStaleOrMismatchedCannotCreateStart(t *testing.T) {
	seed := fact("seed", "CLEAR", 20)
	in := cycleInput(fact("a", "ASSERT", 200))
	in.Seed = &seed
	in.SeedMaxAge = 10
	if got := RebuildCycles(in); len(got) != 1 || got[0].NewStart {
		t.Fatal(got)
	}
	in.SeedMaxAge = 1000
	seed.AssetInstanceID = "old"
	if got := RebuildCycles(in); len(got) != 1 || got[0].NewStart {
		t.Fatal(got)
	}
}
func TestCyclesGapAndReplacementTerminateUnknown(t *testing.T) {
	seed := fact("seed", "CLEAR", 50)
	b := fact("b", "ASSERT", 700)
	in := cycleInput(fact("a", "ASSERT", 200), b)
	in.Seed = &seed
	in.Coverage = append(in.Coverage, CycleCoverage{Start: 400, End: 600, Reliable: false})
	got := RebuildCycles(in)
	if len(got) != 2 || got[0].Status != "TERMINATED_UNKNOWN" || got[0].DurationMillis != nil || got[1].NewStart {
		t.Fatalf("gap: %#v", got)
	}
	in.Coverage = in.Coverage[:1]
	b.AssetInstanceID = "replacement"
	in.Observations[1] = b
	got = RebuildCycles(in)
	if len(got) != 2 || got[0].Status != "TERMINATED_UNKNOWN" || got[1].NewStart {
		t.Fatalf("replacement: %#v", got)
	}
}
func TestCyclesCloseAndLateClearCannotEndSignal(t *testing.T) {
	seed := fact("seed", "CLEAR", 50)
	a := fact("a", "ASSERT", 200)
	a.AlarmID = "lifecycle-one"
	b := fact("b", "ASSERT", 400)
	b.AlarmID = "lifecycle-two"
	late := fact("late", "CLEAR", 300)
	late.RecordedAt = 600
	late.Acceptance = "REJECTED"
	in := cycleInput(a, b, late)
	in.Seed = &seed
	got := RebuildCycles(in)
	if len(got) != 1 || got[0].Status != "OPEN" || len(got[0].AlarmIDs) != 2 {
		t.Fatalf("%#v", got)
	}
}
func TestCyclesWindowEndIsExclusiveAndSeedAloneKeepsOpen(t *testing.T) {
	seed := fact("seed", "ASSERT", 50)
	in := cycleInput(fact("end", "CLEAR", 1000))
	in.Seed = &seed
	got := RebuildCycles(in)
	if len(got) != 1 || got[0].Status != "OPEN" || got[0].BoundaryQuality != "BOTH_CENSORED" {
		t.Fatal(got)
	}
	in.Observations = nil
	got = RebuildCycles(in)
	if len(got) != 1 || got[0].Status != "OPEN" {
		t.Fatal(got)
	}
}
func TestCyclesUntrustedDeviceTimeAndUnknownAcceptanceNotPhysical(t *testing.T) {
	a := fact("a", "ASSERT", 200)
	a.TimeQuality = "UNVERIFIED"
	if got := RebuildCycles(cycleInput(a)); len(got) != 0 {
		t.Fatal(got)
	}
	a.TimeQuality = "TRUSTED"
	a.Acceptance = "HISTORICAL_UNRESOLVED"
	if got := RebuildCycles(cycleInput(a)); len(got) != 0 {
		t.Fatal(got)
	}
}
func TestHistoricalNormalizationRetainsPartialWithoutProductionAcceptance(t *testing.T) {
	msg := model.StandardMessage{MessageID: "s", RawMessageID: "raw", TenantID: "t", DeviceID: "d", Timestamp: 200, MessageType: model.StateChange, Event: map[string]any{"components": []model.ComponentStatus{{ID: "c", Timestamp: 200, Alarms: map[string]bool{"FIRE": false}}}}}
	out, err := NormalizeHistoricalMessage(msg, nil, 999)
	if err != nil || len(out.Observations) != 1 || out.Observations[0].FactKind != "CLEAR" || out.Observations[0].Acceptance != "HISTORICAL_UNRESOLVED" || out.Quality != "PARTIAL" {
		t.Fatal(out, err)
	}
	if got := RebuildCycles(cycleInput(out.Observations...)); len(got) != 0 {
		t.Fatal("history invented accepted cycle", got)
	}
}
