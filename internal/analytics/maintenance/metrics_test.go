package maintenance

import (
	"iot-platform/internal/model"
	"math"
	"slices"
	"testing"
)

func sideFixture() SideInput {
	start := int64(1000)
	return SideInput{AssetID: "asset-a", Asset: model.AssetInstance{DeviceID: "d1", PhysicalID: "serial-a", EffectiveStart: start, BoundaryStatus: "CONFIRMED"}, Window: model.FactRange{Start: start, End: start + 10*hourMs}, Admission: &model.MaintenanceAdmission{Version: "v1", MinimumEffectiveHours: 1, MinimumObservationCoverage: .8, MinimumFaultSourceCoverage: 1}, FaultSourceKnown: []model.FactRange{{Start: start, End: start + 10*hourMs}}}
}
func stateRange(start, end int64, status string) model.DeviceStateIntervalFact {
	return model.DeviceStateIntervalFact{DeviceID: "d1", Start: start, End: end, Quality: "KNOWN", State: &model.DeviceState{ConnectionStatus: status}}
}
func TestUnknownStateDenominatorsAndOfflineIsNotExclusion(t *testing.T) {
	in := sideFixture()
	in.States = []model.DeviceStateIntervalFact{stateRange(in.Window.Start+9*hourMs, in.Window.End, "OFFLINE")}
	got, err := CalculateSide(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.EffectiveMs != 10*hourMs || got.OfflineMs != hourMs || got.StateUnknownMs != 9*hourMs || got.KnownOfflineRatio == nil || *got.KnownOfflineRatio != 1 || got.StateCoverage == nil || *got.StateCoverage != .1 || got.FullWindowOfflineRatio != nil {
		t.Fatalf("wrong denominators: %+v", got)
	}
	if !slices.Contains(got.Limitations, "COMMISSIONING_DATE_AND_AGE_UNKNOWN") {
		t.Fatal("invented asset age")
	}
	params := model.MaintenanceObservationParameters{ComparisonType: "SAME_INSTANCE_REPAIR", Admission: in.Admission}
	comparison, err := Compare(in, in, params, 0, "PASSED", true, nil)
	if err != nil || comparison.Status != "INSUFFICIENT" {
		t.Fatalf("unknown accepted: %+v %v", comparison, err)
	}
}
func TestOverlappingExclusionsAndConflictingIntervals(t *testing.T) {
	in := sideFixture()
	s := in.Window.Start
	in.Exclusions = []model.MaintenanceExclusion{{Kind: "PLANNED_STOP", Start: s, End: s + 2*hourMs, SourceRevisionID: "r1"}, {Kind: "CONFIRMED_TEST", Start: s + hourMs, End: s + 3*hourMs, SourceRevisionID: "r2"}}
	in.States = []model.DeviceStateIntervalFact{stateRange(s, in.Window.End, "ONLINE"), stateRange(s+3*hourMs, s+4*hourMs, "OFFLINE")}
	got, err := CalculateSide(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExcludedMs != 3*hourMs || got.EffectiveMs != 7*hourMs || got.OnlineMs != 6*hourMs || got.StateUnknownMs != hourMs || got.FullWindowOfflineRatio != nil || got.StateCoverage == nil || *got.StateCoverage != 6.0/7 {
		t.Fatalf("incorrect union/conflict: %+v", got)
	}
	in.Exclusions[0].Kind = "DEVICE_OFFLINE"
	if _, err = CalculateSide(in); err == nil {
		t.Fatal("offline removed from denominator")
	}
}
func TestFaultCyclesExcludeTestFireDuplicatesAndDispute(t *testing.T) {
	in := sideFixture()
	s := in.Window.Start
	cycle := FaultCycle{ID: "cycle", DeviceID: "d1", EventAt: s + hourMs, Type: "FAULT", Classification: "PRODUCTION", Confirmation: "CONFIRMED"}
	in.Faults = []FaultCycle{cycle, cycle, {ID: "fire", DeviceID: "d1", EventAt: s + 2*hourMs, Type: "FIRE", Classification: "PRODUCTION", Confirmation: "CONFIRMED"}, {ID: "test", DeviceID: "d1", EventAt: s + 3*hourMs, Type: "FAULT", Classification: "EXERCISE", Confirmation: "CONFIRMED"}}
	got, err := CalculateSide(in)
	if err != nil || got.ConfirmedFaultCycles != 1 || got.FaultsPer1000Hours == nil || *got.FaultsPer1000Hours != 100 {
		t.Fatalf("wrong cycle/rate: %+v %v", got, err)
	}
	changed := cycle
	changed.EventAt++
	in.Faults = append(in.Faults, changed)
	got, err = CalculateSide(in)
	if err != nil || got.ConfirmedFaultCycles != 0 || got.FaultsPer1000Hours != nil || !slices.Contains(got.Limitations, "FAULT_CYCLE_IDENTITY_DISPUTED") {
		t.Fatalf("dispute hidden: %+v %v", got, err)
	}
}
func TestZeroAdmissionCannotTurnUnknownFaultCoverageOrClockIntoZeroRate(t *testing.T) {
	for _, mode := range []string{"NO_SOURCE", "UNKNOWN_CLOCK", "DISPUTED_IDENTITY"} {
		t.Run(mode, func(t *testing.T) {
			in := sideFixture()
			in.Admission.MinimumFaultSourceCoverage = 0
			in.Admission.MinimumObservationCoverage = 0
			in.States = []model.DeviceStateIntervalFact{stateRange(in.Window.Start, in.Window.End, "ONLINE")}
			switch mode {
			case "NO_SOURCE":
				in.FaultSourceKnown = nil
			case "UNKNOWN_CLOCK":
				in.Faults = []FaultCycle{{ID: "unknown", DeviceID: "d1", Type: "FAULT", Classification: "PRODUCTION", Confirmation: "CONFIRMED"}}
			case "DISPUTED_IDENTITY":
				in.Faults = []FaultCycle{{ID: "same", DeviceID: "d1", EventAt: in.Window.Start + 1, Type: "FAULT", Classification: "PRODUCTION", Confirmation: "CONFIRMED"}, {ID: "same", DeviceID: "d1", EventAt: in.Window.Start + 2, Type: "FAULT", Classification: "PRODUCTION", Confirmation: "CONFIRMED"}}
			}
			metrics, err := CalculateSide(in)
			if err != nil || metrics.FaultsPer1000Hours != nil {
				t.Fatalf("unknown became zero rate: %+v %v", metrics, err)
			}
			comparison, err := Compare(in, in, model.MaintenanceObservationParameters{ComparisonType: "SAME_INSTANCE_REPAIR", Admission: in.Admission}, 0, "PASSED", true, nil)
			if err != nil || comparison.Status != "INSUFFICIENT" {
				t.Fatalf("unknown became READY: %+v %v", comparison, err)
			}
		})
	}
}
func TestPhysicalReplacementBoundariesAndAdmission(t *testing.T) {
	before := sideFixture()
	after := sideFixture()
	switchAt := before.Window.End
	after.AssetID = "asset-b"
	after.Asset.PhysicalID = "serial-b"
	after.Asset.EffectiveStart = switchAt
	after.Window = model.FactRange{Start: switchAt, End: switchAt + 10*hourMs}
	after.FaultSourceKnown = []model.FactRange{after.Window}
	before.Asset.EffectiveEnd = &switchAt
	before.States = []model.DeviceStateIntervalFact{stateRange(before.Window.Start, before.Window.End, "ONLINE")}
	after.States = []model.DeviceStateIntervalFact{stateRange(after.Window.Start, after.Window.End, "ONLINE")}
	params := model.MaintenanceObservationParameters{ComparisonType: "CROSS_INSTANCE_REPLACEMENT", SwitchAt: switchAt}
	got, err := Compare(before, after, params, switchAt, "PENDING", true, nil)
	if err != nil || got.Status != "INSUFFICIENT" {
		t.Fatalf("no policy must be insufficient %+v %v", got, err)
	}
	params.Admission = before.Admission
	params.Admission.PostMaintenanceWaitMs = math.MaxInt64
	got, err = Compare(before, after, params, switchAt, "PASSED", true, nil)
	if err != nil || got.Status != "INSUFFICIENT" || !slices.Contains(got.Limitations, "POST_MAINTENANCE_WAIT_INSUFFICIENT") {
		t.Fatalf("overflow bypassed post-maintenance wait %+v %v", got, err)
	}
	params.Admission.PostMaintenanceWaitMs = 0
	got, err = Compare(before, after, params, switchAt, "PENDING", true, nil)
	if err != nil || got.Status != "INSUFFICIENT" || !slices.Contains(got.Limitations, "FUNCTIONAL_VERIFICATION_NOT_PASSED") {
		t.Fatalf("unknown verification became READY %+v %v", got, err)
	}
	got, err = Compare(before, after, params, switchAt, "PASSED", true, []string{"PROTOCOL_CHANGED"})
	if err != nil || got.Status != "CONFOUNDED" {
		t.Fatalf("confounder ignored %+v %v", got, err)
	}
	after.Asset.BoundaryStatus = "DISPUTED"
	got, err = Compare(before, after, params, switchAt, "PASSED", true, nil)
	if err != nil || got.Status != "INSUFFICIENT" || got.After.EffectiveMs != 0 {
		t.Fatalf("disputed asset compared %+v %v", got, err)
	}
	before.Asset.EffectiveEnd = nil
	if _, err = Compare(before, after, params, switchAt, "PASSED", true, nil); err == nil {
		t.Fatal("replacement without actual boundary accepted")
	}
}
func TestZeroDenominatorAndLargeStateHistory(t *testing.T) {
	in := sideFixture()
	in.Exclusions = []model.MaintenanceExclusion{{Kind: "PLANNED_STOP", Start: in.Window.Start, End: in.Window.End, SourceRevisionID: "r"}}
	got, err := CalculateSide(in)
	if err != nil || got.FaultsPer1000Hours != nil || got.StateCoverage != nil || got.KnownOfflineRatio != nil || got.FullWindowOfflineRatio != nil {
		t.Fatalf("zero must NA %+v %v", got, err)
	}
	in.Exclusions = nil
	in.States = make([]model.DeviceStateIntervalFact, 100000)
	for i := range in.States {
		start := in.Window.Start + int64(i)*100
		in.States[i] = stateRange(start, start+100, "ONLINE")
	}
	got, err = CalculateSide(in)
	if err != nil || got.OnlineMs != 10000000 || got.StateUnknownMs != in.Window.End-in.Window.Start-10000000 {
		t.Fatalf("sweep lost history %+v %v", got, err)
	}
}
