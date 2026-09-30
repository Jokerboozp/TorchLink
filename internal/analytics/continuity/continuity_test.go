package continuity

import (
	"math"
	"reflect"
	"testing"

	"iot-platform/internal/model"
)

func fixture() DeviceInput {
	w := Range{1000, 11000}
	return DeviceInput{DeviceID: "child", Window: w, Profiles: []Profile{{ID: "profile-v1", EffectiveFrom: 1000, Mode: "periodic", Merge: "ALL", PeriodMs: 1000, Attributes: []Attribute{{ID: "pressure", ValueType: "number"}}, MessageTypes: []model.MessageType{model.PropertyReport}}}, ReceivedCoverage: []Range{w}, AvailableCoverage: []Range{w}}
}
func measurement(id, property string, event, received, available int64) model.MeasurementFact {
	return model.MeasurementFact{ID: id, DeviceID: "child", Property: property, MessageType: model.PropertyReport, EventAt: event, ReceivedAt: received, AvailableAt: available, Value: 100.0}
}
func metric(t *testing.T, r DeviceResult, track string) Metric {
	t.Helper()
	for _, m := range r.Metrics {
		if m.Track == track && m.ProfileID == "" && m.AttributeID == "" {
			return m
		}
	}
	t.Fatalf("missing overall metric %s", track)
	return Metric{}
}
func analyze(t *testing.T, in DeviceInput) DeviceResult {
	t.Helper()
	r, err := Analyze(in)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func approx(t *testing.T, value *float64, want float64) {
	t.Helper()
	if value == nil || math.Abs(*value-want) > 1e-9 {
		t.Fatalf("ratio %v want %v", value, want)
	}
}

func TestKnownCoverageAndAvailabilityAreIndependent(t *testing.T) {
	for _, available := range []int64{0, 10000, 30000} {
		intervals := []Interval{{Range: Range{available, 40000}, State: Unavailable}}
		if available > 0 {
			intervals = append([]Interval{{Range: Range{0, available}, State: Available}}, intervals...)
		}
		m := summarize("d", "", "", "data", Range{0, 40000}, intervals)
		if m.AvailableMs != available || m.UnavailableMs != 40000-available || m.UnknownMs != 0 {
			t.Fatal(m)
		}
		approx(t, m.KnownCoverage, 1)
		approx(t, m.KnownAvailability, float64(available)/40000)
		approx(t, m.FullWindowAvailability, float64(available)/40000)
		if m.KnownCoverage == m.FullWindowAvailability {
			t.Fatal("coverage and availability share mutable storage", m)
		}
	}
}

func TestAvailabilityUsesActualReadabilityAndEventExpiry(t *testing.T) {
	for _, test := range []struct {
		name                                string
		event, received, available          int64
		wantData, wantReceived, wantUnknown int64
	}{{"readable_delay", 1000, 1000, 1200, 800, 1000, 0}, {"queued_past_expiry", 1000, 1000, 4000, 0, 1000, 0}, {"next_day_replay", 1000, 8000, 8100, 0, 1000, 0}, {"unknown_first_receipt", 1000, 1000, 0, 0, 1000, 1000}, {"missing_event_clock", 0, 1000, 1200, 0, 1000, 1000}, {"future_event", 5000, 1000, 1200, 0, 1000, 1000}, {"impossible_receipt", 1000, 1500, 1200, 0, 1000, 1000}} {
		t.Run(test.name, func(t *testing.T) {
			in := fixture()
			in.Measurements = []model.MeasurementFact{measurement("m", "pressure", test.event, test.received, test.available)}
			r := analyze(t, in)
			m := metric(t, r, "data")
			if m.AvailableMs != test.wantData || m.UnknownMs != test.wantUnknown || m.UnavailableMs != 10000-test.wantData-test.wantUnknown {
				t.Fatal(m)
			}
			if got := metric(t, r, "received"); got.AvailableMs != test.wantReceived {
				t.Fatal(got)
			}
			if test.wantUnknown > 0 && m.FullWindowAvailability != nil {
				t.Fatal("unknown was dropped from denominator")
			}
		})
	}
}
func TestHeartbeatParentInvalidValueAndWrongTypeDoNotRefreshProperty(t *testing.T) {
	in := fixture()
	parent := measurement("parent", "pressure", 1000, 1000, 1000)
	parent.DeviceID = "parent"
	heartbeat := measurement("heartbeat", "pressure", 2000, 2000, 2000)
	heartbeat.MessageType = model.StateChange
	invalid := measurement("invalid", "pressure", 3000, 3000, 3000)
	invalid.Value = math.NaN()
	other := measurement("other", "temperature", 4000, 4000, 4000)
	in.Measurements = []model.MeasurementFact{parent, heartbeat, invalid, other}
	m := metric(t, analyze(t, in), "data")
	if m.AvailableMs != 0 || m.UnavailableMs != 10000 {
		t.Fatal(m)
	}
}

func TestConservativeProcessedTimeRetainsUnknownBeforeReadability(t *testing.T) {
	in := fixture()
	m := measurement("legacy", "pressure", 1000, 1000, 1200)
	m.AvailableAtSource = "historical_processed_at"
	in.Measurements = []model.MeasurementFact{m}
	got := metric(t, analyze(t, in), "data")
	if got.AvailableMs != 800 || got.UnknownMs != 200 || got.UnavailableMs != 9000 || got.FullWindowAvailability != nil {
		t.Fatal(got)
	}
}
func TestALLANYMergeKeepsAttributeTruthAndUnknown(t *testing.T) {
	for _, merge := range []string{"ALL", "ANY"} {
		t.Run(merge, func(t *testing.T) {
			in := fixture()
			in.Profiles[0].Merge = merge
			in.Profiles[0].Attributes = append(in.Profiles[0].Attributes, Attribute{ID: "temperature", ValueType: "number"})
			in.Measurements = []model.MeasurementFact{measurement("p", "pressure", 1000, 1000, 1000), measurement("t", "temperature", 2000, 2000, 2000)}
			r := analyze(t, in)
			m := metric(t, r, "data")
			want := int64(0)
			if merge == "ANY" {
				want = 2000
			}
			if m.AvailableMs != want || m.UnknownMs != 0 {
				t.Fatal(m)
			}
			if len(r.Intervals) < 4 {
				t.Fatal("attribute tracks were discarded")
			}
			in.Measurements[1] = measurement("t", "temperature", 1000, 1000, 0)
			m = metric(t, analyze(t, in), "data")
			if merge == "ALL" {
				if m.UnknownMs != 1000 || m.AvailableMs != 0 {
					t.Fatal(m)
				}
			} else {
				if m.UnknownMs != 0 || m.AvailableMs != 1000 {
					t.Fatal(m)
				}
			}
		})
	}
}
func TestCoverageAndMissingProfileNeverBecomeZeroMissingData(t *testing.T) {
	in := fixture()
	in.ReceivedCoverage = nil
	in.AvailableCoverage = nil
	in.Measurements = []model.MeasurementFact{measurement("m", "pressure", 1000, 1000, 1200)}
	m := metric(t, analyze(t, in), "data")
	if m.AvailableMs != 800 || m.UnknownMs != 9200 || m.UnavailableMs != 0 || m.FullWindowAvailability != nil {
		t.Fatal(m)
	}
	approx(t, m.KnownAvailability, 1)
	approx(t, m.KnownCoverage, .08)
	in = fixture()
	in.Profiles[0].EffectiveFrom = 5000
	m = metric(t, analyze(t, in), "data")
	if m.UnknownMs != 4000 || m.UnavailableMs != 6000 || m.PlannedMs != 10000 {
		t.Fatal(m)
	}
	in.Profiles = nil
	m = metric(t, analyze(t, in), "data")
	if m.UnknownMs != 10000 || m.FullWindowAvailability != nil {
		t.Fatal(m)
	}
}
func TestConnectionSeedUnknownTransitionsAndConflictingHistory(t *testing.T) {
	in := fixture()
	in.Connection = []model.DeviceStateIntervalFact{{DeviceID: "child", Start: 6000, End: 9000, Quality: "KNOWN", State: &model.DeviceState{ConnectionStatus: "DISCONNECTED"}}, {DeviceID: "child", Start: 9000, End: 11000, Quality: "KNOWN", State: &model.DeviceState{ConnectionStatus: "CONNECTED"}}}
	m := metric(t, analyze(t, in), "connection")
	if m.UnknownMs != 5000 || m.UnavailableMs != 3000 || m.AvailableMs != 2000 || m.FullWindowAvailability != nil {
		t.Fatal(m)
	}
	approx(t, m.KnownAvailability, .4)
	approx(t, m.KnownCoverage, .5)
	in.Connection = []model.DeviceStateIntervalFact{{DeviceID: "child", Start: 1000, End: 11000, Quality: "KNOWN", State: &model.DeviceState{ConnectionStatus: "CONNECTED"}}, {DeviceID: "child", Start: 3000, End: 5000, Quality: "KNOWN", State: &model.DeviceState{ConnectionStatus: "DISCONNECTED"}}}
	m = metric(t, analyze(t, in), "connection")
	if m.UnknownMs != 2000 || m.AvailableMs != 8000 || m.UnavailableMs != 0 {
		t.Fatal(m)
	}
}
func TestExclusionUnionAndDeviceOfflineDenominator(t *testing.T) {
	in := fixture()
	in.Observations = []Observation{{ID: "shutdown1", Range: Range{2000, 5000}, Reason: "计划停运", Basis: "批准记录1", ConfirmedBy: "manager"}, {ID: "shutdown2", Range: Range{4000, 7000}, Reason: "检修", Basis: "批准记录2", ConfirmedBy: "manager"}}
	in.Connection = []model.DeviceStateIntervalFact{{DeviceID: "child", Start: 1000, End: 11000, Quality: "KNOWN", State: &model.DeviceState{ConnectionStatus: "DISCONNECTED"}}}
	r := analyze(t, in)
	for _, track := range []string{"data", "received", "connection"} {
		m := metric(t, r, track)
		if m.ExcludedMs != 5000 || m.PlannedMs != 5000 || m.UnavailableMs != 5000 {
			t.Fatal(m)
		}
		approx(t, m.FullWindowAvailability, 0)
	}
	in.Observations[0].Basis = ""
	if _, err := Analyze(in); err == nil {
		t.Fatal("unsupported exclusion was accepted")
	}
}
func TestEventModeHasNoInventedAvailabilityRate(t *testing.T) {
	in := fixture()
	in.Profiles[0].Mode = "event"
	in.Profiles[0].PeriodMs = 0
	in.Observations = []Observation{{ID: "o", Range: Range{2000, 4000}, Reason: "停运", Basis: "记录", ConfirmedBy: "manager"}}
	r := analyze(t, in)
	for _, track := range []string{"data", "received"} {
		m := metric(t, r, track)
		if m.NotApplicableMs != 8000 || m.ExcludedMs != 2000 || m.KnownAvailability != nil || m.FullWindowAvailability != nil {
			t.Fatal(m)
		}
	}
}
func TestVersionBoundaryAndIntervalResourceProtection(t *testing.T) {
	in := fixture()
	in.Profiles[0].EffectiveTo = 6000
	p := in.Profiles[0]
	p.ID = "profile-v2"
	p.EffectiveFrom = 6000
	p.EffectiveTo = 11000
	in.Profiles = append(in.Profiles, p)
	r := analyze(t, in)
	m := metric(t, r, "data")
	if m.GapCount != 1 || m.LongestGapMs != 10000 {
		t.Fatal("version split inflated interruptions", m)
	}
	in.MaximumIntervals = 2
	if _, err := Analyze(in); err == nil {
		t.Fatal("interval limit did not reject")
	}
	in.MaximumIntervals = 0
	in.Profiles[1].EffectiveFrom = 5000
	if _, err := Analyze(in); err == nil {
		t.Fatal("overlapping revisions accepted")
	}
}
func TestHistoricalDependencySwitchAndCurrentOnlyHypothesis(t *testing.T) {
	facts := []model.DependencyFact{{ID: "old", DeviceID: "child", Kind: "collector", ResourceID: "old-collector", EffectiveFrom: 1000, EffectiveTo: 5000, Quality: "KNOWN"}, {ID: "new", DeviceID: "child", Kind: "collector", ResourceID: "new-collector", EffectiveFrom: 5000, Quality: "KNOWN"}, {ID: "hidden", DeviceID: "other", Kind: "collector", ResourceID: "old-collector", EffectiveFrom: 1000, Quality: "KNOWN"}, {ID: "current", DeviceID: "child", Kind: "access-profile", ResourceID: "profile-now", Quality: "CURRENT_SNAPSHOT"}, {ID: "parent", DeviceID: "child", Kind: "parent-device", ResourceID: "unauthorized-parent", Quality: "KNOWN", EffectiveFrom: 1000}}
	groups, limits, err := DependencyGroups([]string{"child", "second"}, Range{1000, 11000}, facts, 20000)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 3 || len(limits) != 2 {
		t.Fatal(groups, limits)
	}
	for _, g := range groups {
		if g.VisibleDeviceCount != 1 || g.AnalysisDeviceCount != 2 || g.Concentration != .5 || !reflect.DeepEqual(g.MemberIDs, []string{"child"}) {
			t.Fatal(g)
		}
		switch g.ResourceID {
		case "old-collector":
			if g.Start != 1000 || g.End != 5000 {
				t.Fatal(g)
			}
			if _, err = Hypothesis(g, []string{"child"}, 5000); err == nil {
				t.Fatal("old relation used after switch")
			}
		case "new-collector":
			if g.Start != 5000 || g.End != 11000 {
				t.Fatal(g)
			}
		case "profile-now":
			if g.HistoryQuality != "CURRENT_ONLY" || g.Start != 20000 || g.End != 20000 {
				t.Fatal(g)
			}
		}
		if _, err = Hypothesis(g, []string{"second"}, g.Start); err == nil {
			t.Fatal("hypothesis escaped member authorization")
		}
	}
}
func TestCommonGapsExactJaccardAndUnknownExclusion(t *testing.T) {
	a := DeviceResult{DeviceID: "a", Intervals: []Interval{{ID: "a-gap", Track: "data", State: Unavailable, Range: Range{1000, 5000}}}}
	b := DeviceResult{DeviceID: "b", Intervals: []Interval{{ID: "b-gap", Track: "data", State: Unavailable, Range: Range{3000, 7000}}, {ID: "unknown", Track: "data", State: Unknown, Range: Range{7000, 9000}}}}
	policy := CommonGapPolicy{Version: "checked-v1", MinimumGapCount: 1, MinimumOverlapMs: 1000, MinimumJaccard: .3}
	f, err := CommonGaps([]DeviceResult{b, a}, Range{1000, 11000}, policy, 10)
	if err != nil || len(f) != 1 {
		t.Fatal(f, err)
	}
	if f[0].Values["overlapMs"] != int64(2000) || f[0].Values["unionMs"] != int64(6000) || f[0].Values["jaccard"] != 1.0/3 {
		t.Fatal(f)
	}
	b.Intervals[0].State = Unknown
	f, err = CommonGaps([]DeviceResult{a, b}, Range{1000, 11000}, policy, 10)
	if err != nil || len(f) != 0 {
		t.Fatal("unknown was classified as shared absence", f, err)
	}
	if _, err = CommonGaps([]DeviceResult{a, b, {DeviceID: "c"}}, Range{1000, 11000}, policy, 1); err == nil {
		t.Fatal("pair resource limit ignored")
	}
}
func TestAnalyzeDoesNotMutateSourceCollections(t *testing.T) {
	in := fixture()
	in.Measurements = []model.MeasurementFact{measurement("b", "pressure", 3000, 3000, 3000), measurement("a", "pressure", 1000, 1000, 1000)}
	before := reflect.ValueOf(in.Measurements).Interface()
	want := append([]model.MeasurementFact(nil), in.Measurements...)
	r := analyze(t, in)
	if !reflect.DeepEqual(before, want) || !reflect.DeepEqual(in.Measurements, want) {
		t.Fatal("source members mutated")
	}
	again := analyze(t, in)
	if !reflect.DeepEqual(r, again) {
		t.Fatal("deterministic result changed")
	}
}
