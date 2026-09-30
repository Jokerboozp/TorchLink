package quality

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"
)

var epoch = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func at(seconds int) time.Time                { return epoch.Add(time.Duration(seconds) * time.Second) }
func number(n float64) *float64               { return &n }
func duration(d time.Duration) *time.Duration { return &d }

func profile() Profile {
	return Profile{ID: "pressure", Version: "quality-1", Mode: Periodic, EffectiveFrom: at(-1000), ScheduleAnchor: at(0), Period: 10 * time.Second, Tolerance: 2 * time.Second, ValueType: "number", Required: true, Unit: "MPa", UnitConfirmed: true, RangeConfirmed: true, Minimum: number(0), Maximum: number(1000), FutureTolerance: duration(3 * time.Second)}
}

func sample(id string, event, receive int, value any) Sample {
	return Sample{ID: id, MessageID: id, RawMessageID: "raw-" + id, EventAt: at(event), ReceivedAt: at(receive), AvailableAt: at(receive + 1), Value: value, Unit: "MPa", ProtocolVersion: "protocol-1", ConfigurationVersion: "point-map-1", OperatingCondition: "normal"}
}

func input(p Profile, start, end int, samples ...Sample) Input {
	w := Window{Start: at(start), End: at(end)}
	return Input{DeviceID: "visible-device", AttributeID: "pressure", Window: w, Profiles: []Profile{p}, Samples: samples, Coverage: Coverage{State: Assessed, Window: Window{Start: w.Start.Add(-p.Tolerance), End: w.End.Add(p.Tolerance)}}, ParseCoverage: Coverage{State: Assessed, Window: w}}
}

func analyze(t *testing.T, in Input) Result {
	t.Helper()
	out, err := Analyze(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := json.Marshal(out); err != nil {
		t.Fatalf("result must be encodable: %v", err)
	}
	return out
}

func requireRatio(t *testing.T, r Ratio, state State, numerator, denominator int) {
	t.Helper()
	if r.State != state || r.Numerator != numerator || r.Denominator != denominator {
		t.Fatalf("ratio = %+v, want %s %d/%d", r, state, numerator, denominator)
	}
	if denominator == 0 {
		if r.Value != nil {
			t.Fatalf("zero denominator emitted value %v", *r.Value)
		}
	} else if state == Assessed || state == Partial {
		if r.Value == nil || math.Abs(*r.Value-float64(numerator)/float64(denominator)) > 1e-12 {
			t.Fatalf("unexpected ratio value: %+v", r)
		}
	}
}

func findingCount(out Result, kind string) int {
	n := 0
	for _, f := range out.Findings {
		if f.Kind == kind {
			n++
		}
	}
	return n
}

func TestProfileValidationRejectsAmbiguousPhysicalConfiguration(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Profile)
	}{
		{"half period", func(p *Profile) { p.Tolerance = 5 * time.Second }},
		{"above half period", func(p *Profile) { p.Tolerance = 6 * time.Second }},
		{"negative tolerance", func(p *Profile) { p.Tolerance = -1 }},
		{"missing anchor", func(p *Profile) { p.ScheduleAnchor = time.Time{} }},
		{"zero period", func(p *Profile) { p.Period = 0 }},
		{"unconfirmed unit with confirmed range", func(p *Profile) { p.UnitConfirmed = false }},
		{"missing bound", func(p *Profile) { p.Maximum = nil }},
		{"reversed range", func(p *Profile) { p.Minimum = number(1001) }},
		{"infinite bound", func(p *Profile) { p.Minimum = number(math.Inf(-1)) }},
		{"zero epsilon", func(p *Profile) { p.Epsilon = number(0) }},
		{"negative rate", func(p *Profile) { p.MaxRate = number(-1) }},
		{"statistical protection", func(p *Profile) { p.MinimumSamples = 29 }},
		{"CUSUM missing version", func(p *Profile) { p.CUSUM = &CUSUMConfig{Threshold: 1} }},
		{"bad quantiles", func(p *Profile) { p.DeviationMethod = "quantile"; p.QuantileLower = .8; p.QuantileUpper = .2 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := profile()
			tc.edit(&p)
			if err := ValidateProfile(p); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	p := profile()
	p.Period, p.Tolerance = 5*time.Nanosecond, 2*time.Nanosecond
	if err := ValidateProfile(p); err != nil {
		t.Fatalf("odd period below half must be allowed: %v", err)
	}
	p.Tolerance = 3 * time.Nanosecond
	if err := ValidateProfile(p); err == nil {
		t.Fatal("odd period above half accepted")
	}
	// No guessed period, range, or epsilon is required for an event profile.
	p = Profile{ID: "status", Version: "1", EffectiveFrom: at(0), Mode: Event, ValueType: "boolean"}
	if err := ValidateProfile(p); err != nil {
		t.Fatal(err)
	}
}

func TestFixedAnchorSlotsHaveHandCountedMissingAndStableRepresentative(t *testing.T) {
	p := profile()
	samples := []Sample{
		sample("before-window", -2, -2, 1), sample("closer", 1, 1, 1),
		sample("late-received", 8, 100, 1), sample("earlier-received", 12, 12, 1),
		sample("b", 20, 20, 1), sample("a", 20, 20, 1),
		sample("outside-tolerance", 43, 43, 1), sample("after-window", 52, 52, 1),
	}
	out := analyze(t, input(p, 0, 51, samples...))
	m := out.Metrics[0]
	// Slot centers are 0,10,20,30,40,50. The -2 and 52 records are padding.
	if m.EventCompleteness.Expected != 6 || m.EventCompleteness.Covered != 4 || m.EventCompleteness.Duplicates != 3 || m.EventCompleteness.OutOfSlot != 1 {
		t.Fatalf("unexpected hand-counted completeness: %+v", m.EventCompleteness)
	}
	requireRatio(t, m.EventCompleteness.Missing, Assessed, 2, 6)
	if m.EventCompleteness.Slots[0].RepresentativeID != "closer" || m.EventCompleteness.Slots[1].RepresentativeID != "earlier-received" || m.EventCompleteness.Slots[2].RepresentativeID != "a" {
		t.Fatalf("unstable representatives: %+v", m.EventCompleteness.Slots)
	}
	if m.InspectedSamples != 6 {
		t.Fatalf("padding polluted main-window denominator: %d", m.InspectedSamples)
	}
	// Reverse source order: every field and evidence ID stays deterministic.
	for i, j := 0, len(samples)-1; i < j; i, j = i+1, j-1 {
		samples[i], samples[j] = samples[j], samples[i]
	}
	reversed := analyze(t, input(p, 0, 51, samples...))
	if !reflect.DeepEqual(out, reversed) {
		t.Fatal("source ordering changed deterministic result")
	}
}

func TestQueryStartDoesNotShiftCompleteSlotsAndBoundaryPadding(t *testing.T) {
	p := profile()
	samples := []Sample{sample("p0", -2, -2, 1), sample("p10", 8, 8, 1), sample("p20", 22, 22, 1), sample("p30", 30, 30, 1), sample("p50", 52, 52, 1), sample("excluded-center", 58, 58, 1)}
	full := analyze(t, input(p, 0, 60, samples...)).Metrics[0].EventCompleteness
	cropped := analyze(t, input(p, 5, 51, samples...)).Metrics[0].EventCompleteness
	if len(cropped.Slots) != 5 {
		t.Fatalf("wrong cropped slot count: %+v", cropped)
	}
	for i, slot := range cropped.Slots {
		if !reflect.DeepEqual(slot, full.Slots[i+1]) {
			t.Fatalf("query start changed center %s: %+v versus %+v", slot.At, slot, full.Slots[i+1])
		}
	}
	if cropped.Slots[4].RepresentativeID != "p50" {
		t.Fatal("end padding was discarded")
	}
	if len(full.Slots) != 6 || full.Slots[0].RepresentativeID != "p0" {
		t.Fatal("start padding was discarded or end center was included")
	}
}

func TestPaddingCoverageMissingIsPartialWithoutFalseMissingClaim(t *testing.T) {
	in := input(profile(), 0, 30)
	in.Coverage.Window = in.Window
	out := analyze(t, in)
	if out.State != Partial || out.Metrics[0].EventCompleteness.Missing.State != Partial || findingCount(out, "missing_event_slots") != 0 || len(out.Metrics[0].EventCompleteness.Missing.Reasons) == 0 {
		t.Fatal("missing boundary source was reported as confirmed device silence")
	}
}

func TestSlotTieChoosesEarlierIncludingBeforeAnchor(t *testing.T) {
	for _, tc := range []struct{ seconds, center int }{{5, 0}, {-5, -10}, {15, 10}, {-15, -20}, {6, 10}, {-6, -10}} {
		got, err := slotAt(at(tc.seconds), epoch, 10*time.Second)
		if err != nil || !got.Equal(at(tc.center)) {
			t.Fatalf("slot at %d = %s (%v), want %d", tc.seconds, got, err, tc.center)
		}
	}
	// Exact half-period samples cannot cover either slot under valid tolerance.
	out := analyze(t, input(profile(), 0, 20, sample("tie", 5, 5, 1)))
	requireRatio(t, out.Metrics[0].EventCompleteness.Missing, Assessed, 2, 2)
}

func TestEffectiveFromAndZeroDenominatorAndEventMode(t *testing.T) {
	p := profile()
	p.EffectiveFrom = at(7)
	out := analyze(t, input(p, 7, 9))
	requireRatio(t, out.Metrics[0].EventCompleteness.Missing, NotApplicable, 0, 0)
	p.Mode = Event
	p.Period, p.Tolerance, p.ScheduleAnchor = 0, 0, time.Time{}
	out = analyze(t, input(p, 7, 60, sample("event", 10, 10, 1)))
	if out.Metrics[0].EventCompleteness.Missing.State != NotApplicable || out.Metrics[0].ReceptionCompleteness.Missing.State != NotApplicable {
		t.Fatal("event series was assigned a periodic missing rate")
	}
}

func TestLateReceptionDoesNotRepairEarlierRealtimeGap(t *testing.T) {
	p := profile()
	samples := []Sample{sample("0", 0, 100, 1), sample("10", 10, 100, 1), sample("20", 20, 100, 1)}
	out := analyze(t, input(p, 0, 30, samples...))
	m := out.Metrics[0]
	requireRatio(t, m.EventCompleteness.Missing, Assessed, 0, 3)
	requireRatio(t, m.ReceptionCompleteness.Missing, Assessed, 3, 3)
	if m.Time.Difference == nil || m.Time.Difference.Median != 90 {
		t.Fatalf("wrong actual reception difference: %+v", m.Time)
	}
	if len(m.Time.Limitations) == 0 || m.Time.ClockCalibrated {
		t.Fatal("unconfirmed clock difference claimed to be network delay")
	}
	if out.Evidence != nil && len(out.Evidence) != 0 {
		t.Fatal("missing-slot aggregate needs no copied measurement series")
	}
}

func TestTimeQualityArrivalOrderRollbackAndFutureAreSeparate(t *testing.T) {
	p := profile()
	samples := []Sample{sample("a", 10, 11, 1), sample("b", 5, 12, 1), sample("c", 8, 13, 1), sample("future", 30, 14, 1), sample("missing", 15, 15, 1)}
	samples[4].ReceivedAt = time.Time{}
	out := analyze(t, input(p, 0, 40, samples...))
	tq := out.Metrics[0].Time
	// Receipt order: 10,5,8,30. Both 5 and 8 trail the maximum 10;
	// only 10->5 is an immediate rollback. One record lacks a receipt clock.
	requireRatio(t, tq.OutOfOrder, Partial, 2, 3)
	requireRatio(t, tq.Rollback, Partial, 1, 3)
	requireRatio(t, tq.Future, Partial, 1, 4)
	if tq.MissingClocks != 1 || tq.Difference == nil || tq.Difference.Median != 3 {
		t.Fatalf("wrong time quality: %+v", tq)
	}
}

func TestFiniteAndMixedTypesRangeNeedsConfirmedMatchingUnit(t *testing.T) {
	p := profile()
	p.ValueType = "integer"
	p.Maximum = number(10)
	samples := []Sample{
		sample("integer", 0, 0, json.Number("2")), sample("fraction", 10, 10, 2.5),
		sample("text", 20, 20, "2"), sample("infinite", 30, 30, math.Inf(1)),
		sample("nan", 40, 40, math.NaN()), sample("missing", 50, 50, nil),
		sample("boolean", 60, 60, true), sample("out", 70, 70, 11),
		sample("other-unit", 80, 80, 999),
	}
	samples[8].Unit = "kPa"
	out := analyze(t, input(p, 0, 90, samples...))
	m := out.Metrics[0]
	requireRatio(t, m.Format, Assessed, 6, 9)
	requireRatio(t, m.Range, Partial, 1, 2)
	if m.ValidSamples != 3 || findingCount(out, "invalid_format") != 1 || findingCount(out, "range_above") != 1 {
		t.Fatalf("wrong typed results: %+v", m)
	}
	// A finding contains only a few representatives, not all invalid payloads.
	for _, f := range out.Findings {
		if len(f.EvidenceIDs) > 3 {
			t.Fatal("unbounded evidence copying")
		}
	}
	p.RangeConfirmed = false
	out = analyze(t, input(p, 0, 90, samples...))
	if out.Metrics[0].Range.State != Unknown || findingCount(out, "range_above") != 0 {
		t.Fatal("unconfirmed range generated out-of-range finding")
	}
	obj := profile()
	obj.ValueType = "object"
	obj.RangeConfirmed = false
	out = analyze(t, input(obj, 0, 20, sample("nested-nan", 0, 0, map[string]any{"reading": math.NaN()}), sample("valid-object", 10, 10, map[string]any{"reading": 1})))
	requireRatio(t, out.Metrics[0].Format, Assessed, 1, 2)
}

func TestRateAndStabilityBreakAtVersionsSameTimeLongGapAndInvalidValue(t *testing.T) {
	p := profile()
	p.Epsilon, p.StableDuration, p.MaxRate, p.MaxSequenceGap = number(.5), 30*time.Second, number(1), 15*time.Second
	samples := []Sample{sample("0", 0, 0, 0), sample("10", 10, 10, 0), sample("20", 20, 20, 0), sample("30", 30, 30, 0), sample("40", 40, 40, 100), sample("50", 50, 50, 100), sample("60", 60, 60, 100), sample("100", 100, 100, 100), sample("110a", 110, 110, 200), sample("110b", 110, 110, 400), sample("bad", 115, 115, "bad"), sample("120", 120, 120, 500)}
	for i := 4; i < len(samples); i++ {
		samples[i].ProtocolVersion = "protocol-2"
	}
	out := analyze(t, input(p, 0, 130, samples...))
	m := out.Metrics[0].Sequence
	requireRatio(t, m.Rate, Assessed, 1, 6)
	requireRatio(t, m.Stable, Assessed, 1, 1)
	if m.VersionBreaks != 1 || m.LongGaps != 1 || m.SameTime != 1 || len(m.Segments) != 5 || m.Statistics != nil {
		t.Fatalf("sequence was not properly split: %+v", m)
	}
	if findingCount(out, "rate_change") != 1 || findingCount(out, "long_stability") != 1 {
		t.Fatalf("wrong sequence findings: %+v", out.Findings)
	}
}

func TestStableSubsequenceAndStrictPrecisionBoundary(t *testing.T) {
	p := profile()
	p.Epsilon, p.StableDuration = number(1), 20*time.Second
	out := analyze(t, input(p, 0, 60, sample("0", 0, 0, 0), sample("1", 10, 10, 10), sample("2", 20, 20, 10.5), sample("3", 30, 30, 10.7), sample("4", 40, 40, 40), sample("5", 50, 50, 50)))
	requireRatio(t, out.Metrics[0].Sequence.Stable, Assessed, 1, 1)
	out = analyze(t, input(p, 0, 30, sample("0", 0, 0, 0), sample("1", 10, 10, .5), sample("2", 20, 20, 1)))
	requireRatio(t, out.Metrics[0].Sequence.Stable, Assessed, 0, 1)
}

func TestBooleanStableDurationAndEventGapPolicy(t *testing.T) {
	p := profile()
	p.Mode, p.ValueType, p.StableDuration, p.MaxSequenceGap = Event, "boolean", 20*time.Second, 15*time.Second
	out := analyze(t, input(p, 0, 50, sample("a", 0, 0, true), sample("b", 10, 10, true), sample("c", 20, 20, true), sample("d", 30, 30, false), sample("e", 40, 40, false)))
	requireRatio(t, out.Metrics[0].Sequence.Stable, Assessed, 1, 1)
	p.MaxSequenceGap = 0
	out = analyze(t, input(p, 0, 50, sample("a", 0, 0, true), sample("b", 10, 10, true), sample("c", 20, 20, true)))
	if out.Metrics[0].Sequence.Stable.State != Unknown || findingCount(out, "long_stability") != 0 {
		t.Fatal("event sequence used an invented continuity gap")
	}
}

func baseline(p Profile) Baseline {
	return Baseline{ID: "confirmed-baseline", ProfileVersion: p.Version, Unit: p.Unit, ProtocolVersion: "protocol-1", ConfigurationVersion: "point-map-1", OperatingCondition: "normal", Window: Window{Start: at(-100), End: at(-10)}, ValidFrom: at(-10), ValidUntil: at(1000), SampleCount: 30, Median: 100, MAD: 0, ConfirmedBy: "reviewer", ConfirmedAt: at(-9), CUSUMVersion: "cusum-1"}
}

func TestBuildBaselineHandCalculatedMedianMADQuantilesAndCannotSelfConfirm(t *testing.T) {
	p := profile()
	samples := []Sample{}
	for i := 0; i < 30; i++ {
		samples = append(samples, sample(fmt.Sprint(i), -100+i, -100+i, i))
	}
	b := baseline(p)
	built, err := BuildBaseline(BaselineRequest{Baseline: b, Profile: p, Samples: samples})
	if err != nil {
		t.Fatal(err)
	}
	if built.SampleCount != 30 || built.Median != 14.5 || built.MAD != 7.5 || math.Abs(built.LowerValue-1.45) > 1e-12 || math.Abs(built.UpperValue-27.55) > 1e-12 || built.ConfirmedBy != "" || !built.ConfirmedAt.IsZero() {
		t.Fatalf("incorrect baseline: %+v", built)
	}
	if _, err := BuildBaseline(BaselineRequest{Baseline: b, Profile: p, Samples: samples[:29]}); err == nil {
		t.Fatal("baseline below protected 30-sample threshold accepted")
	}
	samples[0].Unit = "kPa"
	if _, err := BuildBaseline(BaselineRequest{Baseline: b, Profile: p, Samples: samples}); err == nil {
		t.Fatal("mixed-unit baseline accepted")
	}
	// A caller can raise, but never lower, the statistical sample guard.
	p.MinimumSamples = 40
	samples[0].Unit = "MPa"
	if _, err := BuildBaseline(BaselineRequest{Baseline: b, Profile: p, Samples: samples}); err == nil {
		t.Fatal("raised sample guard ignored")
	}
}

func TestMADZeroRequiresConfiguredPrecisionOrAbsoluteDeviation(t *testing.T) {
	p := profile()
	p.DeviationMethod, p.MADMultiplier = "mad", number(3)
	in := input(p, 0, 30, sample("a", 0, 0, 100), sample("b", 10, 10, 100.5), sample("c", 20, 20, 101))
	in.Baselines = []Baseline{baseline(p)}
	out := analyze(t, in)
	if out.Metrics[0].Sequence.Deviation.State != Unknown || findingCount(out, "historical_deviation") != 0 {
		t.Fatal("zero MAD manufactured an outlier threshold")
	}
	in.Profiles[0].Epsilon = number(.5)
	out = analyze(t, in)
	requireRatio(t, out.Metrics[0].Sequence.Deviation, Assessed, 1, 3)
	in.Profiles[0].Epsilon, in.Profiles[0].AbsoluteDeviation = nil, number(2)
	out = analyze(t, in)
	requireRatio(t, out.Metrics[0].Sequence.Deviation, Assessed, 0, 3)
}

func TestHistoricalComparisonsRejectUnconfirmedUndersizedExpiredOrMixedBaseline(t *testing.T) {
	p := profile()
	p.DeviationMethod, p.MADMultiplier = "mad", number(3)
	p.CUSUM = &CUSUMConfig{Version: "cusum-1", Allowance: .1, Threshold: 1}
	cases := []struct {
		name string
		edit func(*Baseline)
	}{
		{"not confirmed", func(b *Baseline) { b.ConfirmedBy = "" }},
		{"not confirmed time", func(b *Baseline) { b.ConfirmedAt = time.Time{} }},
		{"too small", func(b *Baseline) { b.SampleCount = 29 }},
		{"expired", func(b *Baseline) { b.ValidUntil = at(0) }},
		{"no expiry evidence", func(b *Baseline) { b.ValidUntil = time.Time{} }},
		{"future baseline", func(b *Baseline) { b.Window.End = at(1) }},
		{"different unit", func(b *Baseline) { b.Unit = "kPa" }},
		{"different protocol", func(b *Baseline) { b.ProtocolVersion = "protocol-2" }},
		{"different map", func(b *Baseline) { b.ConfigurationVersion = "point-map-2" }},
		{"different condition", func(b *Baseline) { b.OperatingCondition = "maintenance" }},
		{"different precision config", func(b *Baseline) { b.ProfileVersion = "quality-2" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := baseline(p)
			b.MAD = 1
			tc.edit(&b)
			in := input(p, 0, 10, sample("out", 0, 0, 200))
			in.Baselines = []Baseline{b}
			out := analyze(t, in)
			if out.Metrics[0].Sequence.Deviation.State != Unknown || out.Metrics[0].Sequence.Drift.State != Unknown || findingCount(out, "cusum_drift") != 0 || findingCount(out, "historical_deviation") != 0 {
				t.Fatalf("ineligible baseline was used: %+v", out)
			}
		})
	}
}

func TestCUSUMHandCalculationVersionBindingAndResetAtGap(t *testing.T) {
	p := profile()
	p.CUSUM = &CUSUMConfig{Version: "cusum-1", Allowance: .1, Threshold: 1}
	p.MaxSequenceGap = 15 * time.Second
	samples := []Sample{sample("0", 0, 0, 100.5), sample("1", 10, 10, 100.5), sample("2", 20, 20, 100.5), sample("3", 30, 30, 100.5), sample("4", 40, 40, 100.5)}
	in := input(p, 0, 50, samples...)
	in.Baselines = []Baseline{baseline(p)}
	out := analyze(t, in)
	// Upper sums: .4,.8,1.2 (cross and reset),.4,.8.
	requireRatio(t, out.Metrics[0].Sequence.Drift, Assessed, 1, 5)
	if findingCount(out, "cusum_drift") != 1 {
		t.Fatal("hand-calculated CUSUM crossing missing")
	}
	in.Baselines[0].CUSUMVersion = "old-parameter-version"
	out = analyze(t, in)
	if out.Metrics[0].Sequence.Drift.State != Unknown || findingCount(out, "cusum_drift") != 0 {
		t.Fatal("CUSUM parameter revision was not bound to confirmed baseline")
	}
	in.Baselines[0].CUSUMVersion = "cusum-1"
	in.Samples = []Sample{sample("0", 0, 0, 100.5), sample("1", 10, 10, 100.5), sample("2", 30, 30, 100.5), sample("3", 40, 40, 100.5)}
	out = analyze(t, in)
	requireRatio(t, out.Metrics[0].Sequence.Drift, Assessed, 0, 4)
}

func TestConfirmedMADQuantileAndPartiallyCompatibleSamples(t *testing.T) {
	p := profile()
	p.DeviationMethod, p.MADMultiplier = "mad", number(3)
	b := baseline(p)
	b.MAD = 2
	in := input(p, 0, 30, sample("normal", 0, 0, 106), sample("out", 10, 10, 107), sample("unknown-condition", 20, 20, 1000))
	in.Samples[2].OperatingCondition = "unconfirmed"
	in.Baselines = []Baseline{b}
	out := analyze(t, in)
	requireRatio(t, out.Metrics[0].Sequence.Deviation, Partial, 1, 2)
	p.DeviationMethod, p.QuantileLower, p.QuantileUpper = "quantile", .25, .75
	b.QuantileLower, b.QuantileUpper, b.LowerValue, b.UpperValue = .25, .75, 90, 106
	in.Profiles[0], in.Baselines[0] = p, b
	out = analyze(t, in)
	requireRatio(t, out.Metrics[0].Sequence.Deviation, Partial, 1, 2)
	in.Baselines[0].QuantileUpper = .95
	out = analyze(t, in)
	if out.Metrics[0].Sequence.Deviation.State != Unknown {
		t.Fatal("incompatible baseline quantile policy accepted")
	}
}

func TestParseStagesDistinguishLastResultAndImmutableAttempts(t *testing.T) {
	in := input(profile(), 0, 60)
	in.ParseOutcomes = []ParseOutcome{
		{RawMessageID: "not-tried", ReceivedAt: at(0), Archived: true, LastStatus: ParseNotAttempted},
		{RawMessageID: "success-after-retries", ReceivedAt: at(10), Archived: true, Attempted: true, LastStatus: ParseSucceeded, SuccessfulStandardMessages: 2, Attempts: []ParseAttempt{{ID: "1", Status: ParseFailed}, {ID: "2", Status: ParseFailed}, {ID: "3", Status: ParseSucceeded}}},
		{RawMessageID: "last-failed", ReceivedAt: at(20), Archived: true, Attempted: true, LastStatus: ParseFailed, Attempts: []ParseAttempt{{ID: "1", Status: ParseFailed}, {ID: "1", Status: ParseFailed}}},
		{RawMessageID: "success", ReceivedAt: at(30), Archived: true, Attempted: true, LastStatus: ParseSucceeded, SuccessfulStandardMessages: 1, Attempts: []ParseAttempt{{ID: "1", Status: ParseSucceeded}}},
	}
	out := analyze(t, in)
	pm := out.Parse
	if pm.Archived != 4 || pm.Attempted != 3 || pm.NotAttempted != 1 || pm.LastFailed != 1 || pm.LastSucceeded != 2 || pm.SuccessfulStandardMessages != 3 {
		t.Fatalf("wrong distinct parse stages: %+v", pm)
	}
	requireRatio(t, pm.LastFailure, Assessed, 1, 3)
	if pm.AttemptFailure.State != Unknown || pm.AttemptFailure.Value != nil {
		t.Fatal("attempt failure ratio claimed without complete immutable records")
	}
	in.AttemptsComplete = true
	out = analyze(t, in)
	requireRatio(t, out.Parse.AttemptFailure, Assessed, 3, 5)
	in.ParseOutcomes[3].LastStatus = ParseUnknown
	out = analyze(t, in)
	if out.Parse.UnknownLast != 1 || out.Parse.LastFailure.State != Partial {
		t.Fatal("unknown API-style parsed result was treated as a failed attempt")
	}
	in.ParseOutcomes[3].Attempts = nil
	out = analyze(t, in)
	if out.Parse.AttemptFailure.State != Partial {
		t.Fatal("reader complete flag masked missing immutable attempt records")
	}
}

func TestPartialAndExpiredCoverageNeverBecomeCompletePass(t *testing.T) {
	in := input(profile(), 0, 30, sample("a", 0, 0, 1))
	in.Coverage = Coverage{State: Partial, Window: Window{Start: at(0), End: at(10)}, Reasons: []string{"backfill_pending", "archive_expired"}}
	out := analyze(t, in)
	if out.State != Partial || out.Metrics[0].State != Partial || out.Metrics[0].EventCompleteness.Missing.State != Partial || len(out.Metrics[0].Reasons) != 2 {
		t.Fatalf("partial source was promoted to full: %+v", out)
	}
	in.Coverage.State = Unknown
	out = analyze(t, in)
	if out.State != Unknown || out.Metrics[0].Format.Value != nil || out.Metrics[0].EventCompleteness.Missing.Value != nil {
		t.Fatal("unknown source produced a trustworthy rate")
	}
	in.Coverage.State = Assessed
	out = analyze(t, in)
	if out.State != Partial {
		t.Fatal("reader claiming full coverage outside its actual interval was trusted")
	}
}

func TestConfigurationVersionsSplitAtEffectiveBoundaryAndMissingProfileIsPartial(t *testing.T) {
	p1, p2 := profile(), profile()
	p1.EffectiveFrom, p1.EffectiveTo = at(0), at(20)
	p2.Version, p2.EffectiveFrom, p2.EffectiveTo, p2.Unit = "quality-2", at(20), at(40), "kPa"
	p2.Epsilon = number(.1)
	in := input(p1, 0, 50, sample("old", 10, 10, 1), sample("new", 20, 20, 1000))
	in.Samples[1].Unit = "kPa"
	in.Profiles = []Profile{p2, p1}
	out := analyze(t, in)
	if len(out.Metrics) != 2 || out.Metrics[0].ProfileVersion != "quality-1" || out.Metrics[1].ProfileVersion != "quality-2" || out.Metrics[0].InspectedSamples != 1 || out.Metrics[1].InspectedSamples != 1 || out.State != Partial || len(out.Unconfigured) != 1 || !out.Unconfigured[0].Start.Equal(at(40)) {
		t.Fatalf("wrong immutable configuration split: %+v", out)
	}
	in.Profiles[0].EffectiveFrom = at(19)
	if _, err := Analyze(in); err == nil {
		t.Fatal("overlapping immutable profiles accepted")
	}
}

func TestResourceLimitAndExtremeArithmeticStayExplicit(t *testing.T) {
	in := input(profile(), 0, 60)
	in.MaximumSlots = 5
	if _, err := Analyze(in); err == nil {
		t.Fatal("slot resource limit ignored")
	}
	p := profile()
	p.MaxRate = number(1)
	p.RangeConfirmed = false
	in = input(p, 0, 30, sample("min", 0, 0, -math.MaxFloat64), sample("max", 10, 10, math.MaxFloat64))
	out := analyze(t, in)
	if out.Metrics[0].Sequence.Rate.State != Partial || out.Metrics[0].Sequence.Statistics == nil || out.Metrics[0].Sequence.Statistics.Median != 0 {
		t.Fatal("finite input arithmetic overflow was claimed as assessed")
	}
}

func TestNoMutationOfCallerProfilesSamplesOrProductionMetadata(t *testing.T) {
	p := profile()
	samples := []Sample{sample("b", 10, 10, 1), sample("a", 0, 0, 1)}
	in := input(p, 0, 30, samples...)
	before, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	_ = analyze(t, in)
	after, _ := json.Marshal(in)
	if string(before) != string(after) {
		t.Fatal("pure calculator changed caller-owned facts")
	}
	// Frozen finding values must also survive later caller mutation.
	p.Maximum = number(10)
	in = input(p, 0, 30, sample("high", 0, 0, 11))
	out := analyze(t, in)
	*in.Profiles[0].Maximum = 100
	for _, finding := range out.Findings {
		if finding.Kind == "range_above" && (finding.Threshold == nil || *finding.Threshold != 10) {
			t.Fatal("result retained a mutable caller-owned threshold")
		}
	}
}
