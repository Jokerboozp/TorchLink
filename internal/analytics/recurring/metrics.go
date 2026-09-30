package recurring

import (
	"fmt"
	"iot-platform/internal/model"
	"math"
	"slices"
)

// Interval is half-open. Empty, backwards and non-finite intervals never
// contribute monitoring time or an activity denominator.
type Interval struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
}

func Union(in []Interval) []Interval {
	out := []Interval{}
	v := slices.Clone(in)
	slices.SortFunc(v, func(a, b Interval) int {
		if a.Start < b.Start {
			return -1
		}
		if a.Start > b.Start {
			return 1
		}
		return 0
	})
	for _, r := range v {
		if r.End <= r.Start {
			continue
		}
		n := len(out)
		if n > 0 && r.Start <= out[n-1].End {
			out[n-1].End = max(out[n-1].End, r.End)
		} else {
			out = append(out, r)
		}
	}
	return out
}
func EffectiveIntervals(window Interval, usable, excluded []Interval) []Interval {
	out := []Interval{}
	for _, r := range Union(usable) {
		r.Start = max(window.Start, r.Start)
		r.End = min(window.End, r.End)
		if r.Start < r.End {
			out = append(out, r)
		}
	}
	for _, x := range Union(excluded) {
		next := []Interval{}
		for _, r := range out {
			if x.End <= r.Start || x.Start >= r.End {
				next = append(next, r)
				continue
			}
			if x.Start > r.Start {
				next = append(next, Interval{r.Start, min(x.Start, r.End)})
			}
			if x.End < r.End {
				next = append(next, Interval{max(x.End, r.Start), r.End})
			}
		}
		out = next
	}
	return out
}
func Contains(intervals []Interval, r Interval) bool {
	for _, x := range Union(intervals) {
		if x.Start <= r.Start && x.End >= r.End {
			return true
		}
	}
	return false
}
func MonitoringHours(v []Interval) float64 {
	var ms int64
	for _, r := range Union(v) {
		ms += r.End - r.Start
	}
	return float64(ms) / 3600000
}

type ActivityRelation struct {
	ActivityID    string
	ObservationID string
	State         string
}
type ActivityMetricInput struct {
	Window                       Interval
	DeviceID, ActivityType, Unit string
	Monitoring                   []Interval
	Excluded                     []Interval
	Activities                   []model.FieldActivityRevision
	Coverages                    []model.ActivityCoverage
	Observations                 []model.AlarmObservation
	Relations                    []ActivityRelation
}
type ActivityMetrics struct {
	N                      int       `json:"n"`
	C                      int       `json:"c"`
	U                      int       `json:"u"`
	VerifiedUnrelated      int       `json:"verifiedUnrelated"`
	NoRecordedAlarm        int       `json:"noRecordedAlarm"`
	Ineligible             int       `json:"ineligible"`
	RelatedFraction        *float64  `json:"relatedFraction"`
	IdentificationInterval []float64 `json:"identificationInterval,omitempty"`
	Hours                  *float64  `json:"effectiveMonitoringHours"`
	Coverage               string    `json:"coverage"`
	Limitations            []string  `json:"limitations"`
	ActivityIDs            []string  `json:"activityIds"`
}

func addIssue(v []string, s string) []string {
	if !slices.Contains(v, s) {
		return append(v, s)
	}
	return v
}
func comparableTime(q string) bool { return q == "TRUSTED" || q == "VERIFIED" }
func ComputeActivityMetrics(in ActivityMetricInput) ActivityMetrics {
	m := ActivityMetrics{Coverage: "UNKNOWN", Limitations: []string{}, ActivityIDs: []string{}}
	usable := EffectiveIntervals(in.Window, in.Monitoring, in.Excluded)
	if len(usable) == 0 {
		m.Limitations = append(m.Limitations, "NO_VERIFIED_MONITORING_INTERVALS")
	} else {
		hours := MonitoringHours(usable)
		m.Hours = &hours
	}
	full := []Interval{}
	partial := []Interval{}
	latestCoverage := map[string]model.ActivityCoverage{}
	for i, c := range in.Coverages {
		key := c.CoverageID
		if key == "" {
			key = fmt.Sprintf("legacy:%d", i)
		}
		if old, ok := latestCoverage[key]; !ok || old.RevisionNumber < c.RevisionNumber {
			latestCoverage[key] = c
		}
	}
	for _, c := range latestCoverage {
		if !slices.Contains(c.DeviceIDs, in.DeviceID) || c.ActivityType != in.ActivityType || c.EndAt <= c.StartAt || c.Basis == "" {
			continue
		}
		if c.Coverage == "FULL_DECLARED" {
			full = append(full, Interval{c.StartAt, c.EndAt})
		} else if c.Coverage == "PARTIAL" {
			partial = append(partial, Interval{c.StartAt, c.EndAt})
		}
	}
	// A partial declaration cannot override an explicit complete declaration.
	if len(full) > 0 {
		m.Coverage = "FULL_DECLARED"
		if !Contains(full, in.Window) {
			m.Coverage = "PARTIAL"
			m.Limitations = addIssue(m.Limitations, "DECLARED_COMPLETE_SUBWINDOW_ONLY")
		}
	} else if len(partial) > 0 {
		m.Coverage = "PARTIAL"
		m.Limitations = addIssue(m.Limitations, "REGISTERED_SUBSET_ONLY")
	} else {
		m.Limitations = addIssue(m.Limitations, "NO_FULL_ACTIVITY_DECLARATION")
	}
	byID := map[string]model.FieldActivityRevision{}
	for _, a := range in.Activities {
		if a.ActivityID != "" {
			if old, ok := byID[a.ActivityID]; !ok || old.RevisionNumber < a.RevisionNumber {
				byID[a.ActivityID] = a
			}
		}
	}
	keys := []string{}
	for id := range byID {
		keys = append(keys, id)
	}
	slices.Sort(keys)
	for _, id := range keys {
		a := byID[id]
		if !slices.Contains(a.DeviceIDs, in.DeviceID) || a.ActivityType != in.ActivityType {
			continue
		}
		if !a.Actual || a.Status != "CONFIRMED" || a.Unit != in.Unit || a.StartAt == nil || a.EndAt == nil || !comparableTime(a.TimeQuality) {
			m.Ineligible++
			continue
		}
		r := Interval{*a.StartAt, *a.EndAt}
		if r.End <= r.Start || !Contains([]Interval{in.Window}, r) || !Contains(usable, r) {
			m.Ineligible++
			continue
		}
		qualifies := Contains(full, r)
		subset := !qualifies && m.Coverage == "PARTIAL" && Contains(partial, r)
		if !qualifies && !subset {
			m.Ineligible++
			continue
		}
		m.N++
		m.ActivityIDs = append(m.ActivityIDs, id)
		relevant := map[string]bool{}
		clockUnknown := false
		for _, o := range in.Observations {
			if o.DeviceID != in.DeviceID || o.Acceptance != "ACCEPTED" || (o.FactKind != "ASSERT" && o.FactKind != "REPORT") {
				continue
			}
			if !comparableTime(o.TimeQuality) {
				clockUnknown = true
				continue
			}
			if o.EventAt >= r.Start && o.EventAt < r.End {
				relevant[o.ID] = true
			}
		}
		related, unresolved := false, clockUnknown
		for obs := range relevant {
			resolved := false
			for _, rel := range in.Relations {
				if rel.ActivityID != id || rel.ObservationID != obs {
					continue
				}
				if rel.State == "CONFIRMED_RELATED" {
					related = true
					resolved = true
				}
				if rel.State == "CONFIRMED_UNRELATED" {
					resolved = true
				}
			}
			if !resolved {
				unresolved = true
			}
		}
		if related {
			m.C++
		} else if unresolved {
			m.U++
		} else if len(relevant) > 0 {
			m.VerifiedUnrelated++
		} else {
			m.NoRecordedAlarm++
		}
		if clockUnknown {
			m.Limitations = addIssue(m.Limitations, "UNCOMPARABLE_OBSERVATION_CLOCK")
		}
	}
	if m.N > 0 {
		f := float64(m.C) / float64(m.N)
		m.RelatedFraction = &f
		m.IdentificationInterval = []float64{f, float64(m.C+m.U) / float64(m.N)}
	} else {
		m.Limitations = addIssue(m.Limitations, "NO_ELIGIBLE_ACTIVITIES")
	}
	return m
}

type Comparison struct {
	Conclusion  string   `json:"conclusion"`
	Difference  *float64 `json:"difference"`
	Limitations []string `json:"limitations"`
}

func CompareActivities(before, after ActivityMetrics, conditionsEqual bool, minActivities int, minHours float64) Comparison {
	c := Comparison{Conclusion: "INSUFFICIENT_DATA", Limitations: []string{}}
	if !conditionsEqual {
		c.Conclusion = "NOT_COMPARABLE"
		c.Limitations = append(c.Limitations, "CONDITIONS_DIFFER")
		return c
	}
	if before.Coverage != "FULL_DECLARED" || after.Coverage != "FULL_DECLARED" || before.N < max(1, minActivities) || after.N < max(1, minActivities) || before.Hours == nil || after.Hours == nil || *before.Hours < minHours || *after.Hours < minHours || math.IsNaN(minHours) || before.RelatedFraction == nil || after.RelatedFraction == nil {
		c.Limitations = append(c.Limitations, "MINIMUM_OR_COVERAGE_NOT_MET")
		return c
	}
	if slices.Contains(before.Limitations, "DECLARED_COMPLETE_SUBWINDOW_ONLY") || slices.Contains(after.Limitations, "DECLARED_COMPLETE_SUBWINDOW_ONLY") {
		c.Limitations = append(c.Limitations, "DECLARED_COMPLETE_SUBWINDOW_ONLY")
		return c
	}
	if slices.Contains(before.Limitations, "UNCOMPARABLE_OBSERVATION_CLOCK") || slices.Contains(after.Limitations, "UNCOMPARABLE_OBSERVATION_CLOCK") {
		c.Limitations = append(c.Limitations, "UNCOMPARABLE_OBSERVATION_CLOCK")
		return c
	}
	d := *after.RelatedFraction - *before.RelatedFraction
	// Overlapping identification intervals cannot justify improvement; this is
	// the uncertainty from unfinished verification, not a confidence interval.
	if before.U > 0 || after.U > 0 {
		if after.IdentificationInterval[1] < before.IdentificationInterval[0] {
			c.Conclusion = "IMPROVED"
		} else if after.IdentificationInterval[0] > before.IdentificationInterval[1] {
			c.Conclusion = "WORSENED"
		} else {
			c.Limitations = append(c.Limitations, "UNRESOLVED_ACTIVITY_RELATIONS")
			return c
		}
	} else if d < 0 {
		c.Conclusion = "IMPROVED"
	} else if d > 0 {
		c.Conclusion = "WORSENED"
	} else {
		c.Conclusion = "NOT_IMPROVED"
	}
	c.Difference = &d
	return c
}
