package recurring

import (
	"iot-platform/internal/model"
	"slices"
	"testing"
)

func ptr(n int64) *int64 { return &n }
func TestEffectiveIntervalsSubtractOverlapOnce(t *testing.T) {
	v := EffectiveIntervals(Interval{0, 100}, []Interval{{0, 80}, {50, 100}}, []Interval{{10, 40}, {30, 60}, {90, 110}})
	if len(v) != 2 || v[0] != (Interval{0, 10}) || v[1] != (Interval{60, 90}) {
		t.Fatal(v)
	}
	if Contains(v, Interval{5, 65}) {
		t.Fatal("gap falsely qualified activity")
	}
}
func TestActivityDenominatorDedupAndUnverifiedBounds(t *testing.T) {
	in := ActivityMetricInput{Window: Interval{0, 100}, DeviceID: "d", ActivityType: "COOKING", Unit: "MEAL", Monitoring: []Interval{{0, 100}}, Coverages: []model.ActivityCoverage{{DeviceIDs: []string{"d"}, ActivityType: "COOKING", StartAt: 0, EndAt: 100, Coverage: "FULL_DECLARED", Basis: "daily log"}}, Activities: []model.FieldActivityRevision{{ActivityID: "a", DeviceIDs: []string{"d"}, ActivityType: "COOKING", Unit: "MEAL", Actual: true, Status: "CONFIRMED", StartAt: ptr(10), EndAt: ptr(30), TimeQuality: "TRUSTED", RevisionNumber: 1}, {ActivityID: "b", DeviceIDs: []string{"d"}, ActivityType: "COOKING", Unit: "MEAL", Actual: true, Status: "CONFIRMED", StartAt: ptr(40), EndAt: ptr(50), TimeQuality: "TRUSTED", RevisionNumber: 1}, {ActivityID: "c", DeviceIDs: []string{"d"}, ActivityType: "COOKING", Unit: "MEAL", Actual: true, Status: "CONFIRMED", StartAt: ptr(70), EndAt: ptr(80), TimeQuality: "TRUSTED", RevisionNumber: 1}}, Observations: []model.AlarmObservation{{ID: "o1", DeviceID: "d", FactKind: "ASSERT", Acceptance: "ACCEPTED", TimeQuality: "TRUSTED", EventAt: 15}, {ID: "o2", DeviceID: "d", FactKind: "REPORT", Acceptance: "ACCEPTED", TimeQuality: "TRUSTED", EventAt: 20}, {ID: "o3", DeviceID: "d", FactKind: "ASSERT", Acceptance: "ACCEPTED", TimeQuality: "TRUSTED", EventAt: 45}}, Relations: []ActivityRelation{{"a", "o1", "CONFIRMED_RELATED"}, {"a", "o2", "CONFIRMED_RELATED"}}}
	in.Activities = append(in.Activities, in.Activities[0])
	m := ComputeActivityMetrics(in)
	if m.N != 3 || m.C != 1 || m.U != 1 || m.NoRecordedAlarm != 1 || m.RelatedFraction == nil || *m.RelatedFraction != 1.0/3 || len(m.IdentificationInterval) != 2 || m.IdentificationInterval[1] != 2.0/3 {
		t.Fatal(m)
	}
	in.Coverages[0].Coverage = "ALARM_ONLY"
	if m = ComputeActivityMetrics(in); m.N != 0 || m.RelatedFraction != nil {
		t.Fatal("alarm-only fabricated denominator", m)
	}
	in.Coverages[0].Coverage = "PARTIAL"
	m = ComputeActivityMetrics(in)
	if m.Coverage != "PARTIAL" || m.N != 3 {
		t.Fatal(m)
	}
	if c := CompareActivities(m, m, true, 1, 0); c.Conclusion != "INSUFFICIENT_DATA" {
		t.Fatal(c)
	}
	in.Monitoring = nil
	if m = ComputeActivityMetrics(in); m.N != 0 || m.Hours != nil {
		t.Fatal("no functional observation but denominator calculated", m)
	}
}
func TestActivityComparisonsUseEqualRatios(t *testing.T) {
	hours := 100.0
	f := 0.1
	before := ActivityMetrics{N: 50, C: 5, Coverage: "FULL_DECLARED", Hours: &hours, RelatedFraction: &f, IdentificationInterval: []float64{f, f}}
	after := before
	after.N = 20
	after.C = 2
	if c := CompareActivities(before, after, true, 1, 1); c.Conclusion != "NOT_IMPROVED" || c.Difference == nil || *c.Difference != 0 {
		t.Fatal(c)
	}
	if c := CompareActivities(before, after, false, 1, 1); c.Conclusion != "NOT_COMPARABLE" || c.Difference != nil {
		t.Fatal(c)
	}
	after.U = 4
	after.IdentificationInterval = []float64{.1, .3}
	if c := CompareActivities(before, after, true, 1, 1); c.Conclusion != "INSUFFICIENT_DATA" {
		t.Fatal(c)
	}
}

func TestSameConditionsWithUnknownIdentityDoNotClaimConditionsDiffer(t *testing.T) {
	hours, fraction := 1.0, 0.1
	metric := ActivityMetrics{N: 10, C: 1, Coverage: "FULL_DECLARED", Hours: &hours, RelatedFraction: &fraction}
	frozen := frozenInputs{Case: &model.GovernanceCase{IdentityQuality: "UNVERIFIED"}, Plan: &model.ObservationPlan{BeforeConditionsHash: "same", AfterConditionsHash: "same", MinimumActivities: 1, MinimumMonitoringHours: 1}}
	result := comparePlan(frozen, metric, metric)
	if result.Conclusion != "INSUFFICIENT_DATA" || result.Difference != nil || slices.Contains(result.Limitations, "CONDITIONS_DIFFER") || !slices.Contains(result.Limitations, "POINT_IDENTITY_UNCONFIRMED") {
		t.Fatal(result)
	}
	frozen.Case.IdentityQuality = "CONFIRMED"
	frozen.Parameters.TimeBasis = "RECEIVED_AT"
	result = comparePlan(frozen, metric, metric)
	if result.Conclusion != "INSUFFICIENT_DATA" || !slices.Contains(result.Limitations, "RECEIVED_TIME_BASIS_ONLY") {
		t.Fatal(result)
	}
	frozen.Parameters.TimeBasis = "EVENT_AT"
	result = comparePlan(frozen, metric, metric)
	if result.Conclusion != "NOT_IMPROVED" {
		t.Fatal(result)
	}
	frozen.Plan.AfterConditionsHash = "different"
	result = comparePlan(frozen, metric, metric)
	if result.Conclusion != "NOT_COMPARABLE" || !slices.Contains(result.Limitations, "CONDITIONS_DIFFER") {
		t.Fatal(result)
	}
}
