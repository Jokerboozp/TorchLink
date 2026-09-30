package continuity

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"

	"iot-platform/internal/model"
)

type proof struct {
	Range
	ID string
}

func ValidateProfile(p Profile) error {
	if p.ID == "" || p.EffectiveFrom < 0 || p.EffectiveTo < 0 || (p.EffectiveTo != 0 && p.EffectiveTo <= p.EffectiveFrom) {
		return fmt.Errorf("invalid profile effective interval")
	}
	if (p.Mode != "periodic" && p.Mode != "event") || (p.Merge != "ALL" && p.Merge != "ANY") || len(p.Attributes) == 0 || len(p.Attributes) > 50 || len(p.MessageTypes) == 0 {
		return fmt.Errorf("mode, merge, attributes and accepted message types are required")
	}
	if p.PeriodMs < 0 || p.ToleranceMs < 0 || p.PeriodMs > math.MaxInt64-p.ToleranceMs || p.LongGapMs < 0 || p.FrequentGapCount < 0 || (p.Mode == "periodic" && p.PeriodMs == 0) {
		return fmt.Errorf("invalid monitoring timing")
	}
	seen := map[string]bool{}
	for _, a := range p.Attributes {
		if a.ID == "" || seen[a.ID] || !slices.Contains([]string{"number", "integer", "boolean", "string"}, a.ValueType) {
			return fmt.Errorf("invalid or duplicated attribute")
		}
		seen[a.ID] = true
		if (a.Minimum != nil && (math.IsNaN(*a.Minimum) || math.IsInf(*a.Minimum, 0))) || (a.Maximum != nil && (math.IsNaN(*a.Maximum) || math.IsInf(*a.Maximum, 0))) || (a.Minimum != nil && a.Maximum != nil && *a.Minimum > *a.Maximum) {
			return fmt.Errorf("invalid valid-value range")
		}
	}
	return nil
}
func valueValid(a Attribute, v any) bool {
	switch a.ValueType {
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	default:
		var n float64
		switch value := v.(type) {
		case float64:
			n = value
		case float32:
			n = float64(value)
		case int:
			n = float64(value)
		case int64:
			n = float64(value)
		case int32:
			n = float64(value)
		case uint64:
			n = float64(value)
		case json.Number:
			var err error
			n, err = value.Float64()
			if err != nil {
				return false
			}
		default:
			return false
		}
		return !math.IsNaN(n) && !math.IsInf(n, 0) && (a.ValueType != "integer" || math.Trunc(n) == n) && (a.Minimum == nil || n >= *a.Minimum) && (a.Maximum == nil || n <= *a.Maximum)
	}
}

func Analyze(in DeviceInput) (DeviceResult, error) {
	r := DeviceResult{AlgorithmVersion: AlgorithmVersion, DeviceID: in.DeviceID, Intervals: []Interval{}, Metrics: []Metric{}, Findings: []Finding{}, Limitations: []string{}}
	if in.DeviceID == "" || in.Window.Start < 0 || !valid(in.Window) {
		return r, fmt.Errorf("explicit device and nonempty window required")
	}
	if len(in.Measurements) > 50000 || len(in.Connection) > 100000 || len(in.Profiles) > 500 || len(in.Observations) > 5000 {
		return r, fmt.Errorf("monitoring source resource limit exceeded")
	}
	limit := in.MaximumIntervals
	if limit <= 0 {
		limit = 100000
	}
	profiles := slices.Clone(in.Profiles)
	slices.SortFunc(profiles, func(a, b Profile) int {
		if a.EffectiveFrom < b.EffectiveFrom {
			return -1
		}
		if a.EffectiveFrom > b.EffectiveFrom {
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	for i, p := range profiles {
		if err := ValidateProfile(p); err != nil {
			return r, err
		}
		if i > 0 && (profiles[i-1].EffectiveTo == 0 || profiles[i-1].EffectiveTo > p.EffectiveFrom) {
			return r, fmt.Errorf("monitoring profile intervals overlap")
		}
	}
	exclusions := []Range{}
	for _, o := range in.Observations {
		if o.ID == "" || o.ConfirmedBy == "" || o.Basis == "" || o.Reason == "" || !valid(o.Range) {
			return r, fmt.Errorf("observation exclusions require confirmed evidence")
		}
		exclusions = append(exclusions, o.Range)
	}
	exclusions = Union(exclusions, in.Window)
	connection := connectionIntervals(in.DeviceID, in.Window, in.Connection, exclusions)
	r.Intervals = append(r.Intervals, connection...)
	r.Metrics = append(r.Metrics, summarize(in.DeviceID, "", "", "connection", in.Window, connection))
	cursor := in.Window.Start
	for _, p := range profiles {
		end := p.EffectiveTo
		if end == 0 {
			end = in.Window.End
		}
		w := clip(in.Window, Range{p.EffectiveFrom, end})
		if !valid(w) {
			continue
		}
		if cursor < w.Start {
			for _, track := range []string{"data", "received"} {
				r.Intervals = append(r.Intervals, timeline(in.DeviceID, "", "", track, Range{cursor, w.Start}, nil, nil, nil, exclusions, nil)...)
			}
			r.Limitations = append(r.Limitations, "MONITORING_PROFILE_MISSING")
		}
		cursor = w.End
		if p.Mode == "event" {
			for _, track := range []string{"received", "data"} {
				intervals := timeline(in.DeviceID, "", p.ID, track, w, nil, nil, nil, exclusions, nil)
				for i := range intervals {
					if intervals[i].State != Excluded {
						intervals[i].State = NotApplicable
						intervals[i].Reasons = []string{"EVENT_REPORTING_HAS_NO_FIXED_PERIOD"}
					}
				}
				decorate(intervals)
				r.Intervals = append(r.Intervals, intervals...)
				r.Metrics = append(r.Metrics, summarize(in.DeviceID, "", p.ID, track, w, intervals))
			}
			continue
		}
		allReceived := [][]Interval{}
		allData := [][]Interval{}
		for _, a := range p.Attributes {
			received, data, ambiguous := []proof{}, []proof{}, []Range{}
			ttl := p.PeriodMs + p.ToleranceMs
			for _, m := range in.Measurements {
				if m.DeviceID != in.DeviceID || m.Property != a.ID || !slices.Contains(p.MessageTypes, m.MessageType) || !valueValid(a, m.Value) {
					continue
				}
				if m.ReceivedAt <= 0 || m.ReceivedAt > math.MaxInt64-ttl {
					continue
				}
				received = append(received, proof{Range{m.ReceivedAt, m.ReceivedAt + ttl}, m.ID})
				// Event freshness bounds replay and queue delay. Neither receive
				// time nor parser time substitutes for unknown availability.
				if m.EventAt <= 0 || m.EventAt > math.MaxInt64-ttl || m.AvailableAt <= 0 || m.AvailableAt < m.ReceivedAt {
					ambiguous = append(ambiguous, Range{m.ReceivedAt, m.ReceivedAt + ttl})
					continue
				}
				if m.EventAt > m.ReceivedAt+p.ToleranceMs {
					ambiguous = append(ambiguous, Range{m.ReceivedAt, m.ReceivedAt + ttl})
					continue
				}
				if m.AvailableAtSource == "historical_processed_at" {
					// A historical processed marker proves later readability,
					// but cannot locate the first availability before that marker.
					if expiry := m.EventAt + ttl; m.ReceivedAt < expiry {
						ambiguous = append(ambiguous, Range{m.ReceivedAt, expiry})
					}
				}
				if expiry := m.EventAt + ttl; m.AvailableAt < expiry {
					data = append(data, proof{Range{m.AvailableAt, expiry}, m.ID})
				}
			}
			receivedIntervals := timeline(in.DeviceID, a.ID, p.ID, "received", w, proofRanges(received), in.ReceivedCoverage, nil, exclusions, received)
			dataIntervals := timeline(in.DeviceID, a.ID, p.ID, "attribute", w, proofRanges(data), in.AvailableCoverage, ambiguous, exclusions, data)
			r.Intervals = append(r.Intervals, receivedIntervals...)
			r.Intervals = append(r.Intervals, dataIntervals...)
			r.Metrics = append(r.Metrics, summarize(in.DeviceID, a.ID, p.ID, "received", w, receivedIntervals), summarize(in.DeviceID, a.ID, p.ID, "attribute", w, dataIntervals))
			allReceived = append(allReceived, receivedIntervals)
			allData = append(allData, dataIntervals)
			if len(r.Intervals) > limit {
				return DeviceResult{}, fmt.Errorf("monitoring interval resource limit exceeded")
			}
		}
		for _, entry := range []struct {
			track string
			sets  [][]Interval
		}{{"received", allReceived}, {"data", allData}} {
			merged := mergeAttributes(in.DeviceID, p.ID, entry.track, p.Merge, w, entry.sets)
			r.Intervals = append(r.Intervals, merged...)
			metric := summarize(in.DeviceID, "", p.ID, entry.track, w, merged)
			r.Metrics = append(r.Metrics, metric)
			if entry.track == "data" {
				if p.LongGapMs > 0 && metric.LongestGapMs >= p.LongGapMs {
					r.Findings = append(r.Findings, Finding{ID: identity(in.DeviceID, p.ID, "long_gap", w), DeviceID: in.DeviceID, ProfileID: p.ID, Kind: "LONG_DATA_GAP", Range: w, Explanation: "固定测点策略下存在已知有效数据中断；不推断现场设施状态或共同原因。", EvidenceIDs: gapIDs(merged), Values: map[string]any{"thresholdMs": p.LongGapMs, "longestGapMs": metric.LongestGapMs}})
				}
				if p.FrequentGapCount > 0 && metric.GapCount >= p.FrequentGapCount {
					r.Findings = append(r.Findings, Finding{ID: identity(in.DeviceID, p.ID, "frequent", w), DeviceID: in.DeviceID, ProfileID: p.ID, Kind: "FREQUENT_DATA_GAPS", Range: w, Explanation: "固定策略下多次中断，需结合接入与解析证据核实。", EvidenceIDs: gapIDs(merged), Values: map[string]any{"threshold": p.FrequentGapCount, "gapCount": metric.GapCount}})
				}
				if metric.UnknownMs > 0 {
					r.Findings = append(r.Findings, Finding{ID: identity(in.DeviceID, p.ID, "unknown", w), DeviceID: in.DeviceID, ProfileID: p.ID, Kind: "INSUFFICIENT_HISTORY", Range: w, Explanation: "部分监测区间依据未知；已知区间可用率不代表完整窗口。", EvidenceIDs: stateIDs(merged, Unknown), Values: map[string]any{"unknownMs": metric.UnknownMs, "plannedMs": metric.PlannedMs}})
				}
			}
		}
		if len(r.Intervals) > limit {
			return DeviceResult{}, fmt.Errorf("monitoring interval resource limit exceeded")
		}
	}
	if cursor < in.Window.End {
		for _, track := range []string{"data", "received"} {
			r.Intervals = append(r.Intervals, timeline(in.DeviceID, "", "", track, Range{cursor, in.Window.End}, nil, nil, nil, exclusions, nil)...)
		}
		r.Limitations = append(r.Limitations, "MONITORING_PROFILE_MISSING")
	}
	if len(profiles) == 0 {
		r.Limitations = append(r.Limitations, "MONITORING_PROFILE_MISSING")
	}
	for _, track := range []string{"data", "received"} {
		all := []Interval{}
		for _, v := range r.Intervals {
			if v.Track == track && v.AttributeID == "" {
				all = append(all, v)
			}
		}
		r.Metrics = append(r.Metrics, summarize(in.DeviceID, "", "", track, in.Window, all))
	}
	slices.Sort(r.Limitations)
	r.Limitations = slices.Compact(r.Limitations)
	if len(r.Intervals) > limit {
		return DeviceResult{}, fmt.Errorf("monitoring interval resource limit exceeded")
	}
	return r, nil
}
func proofRanges(proofs []proof) []Range {
	r := make([]Range, 0, len(proofs))
	for _, p := range proofs {
		r = append(r, p.Range)
	}
	return r
}
func decorate(intervals []Interval) {
	for i := range intervals {
		v := &intervals[i]
		v.ID = identity(v.DeviceID, v.AttributeID, v.ProfileID, v.Track, v.Range, v.State)
	}
}
func appendInterval(r []Interval, v Interval) []Interval {
	if len(r) > 0 {
		old := &r[len(r)-1]
		if old.End == v.Start && old.State == v.State && slices.Equal(old.Reasons, v.Reasons) {
			old.End = v.End
			for _, id := range v.EvidenceIDs {
				if len(old.EvidenceIDs) < 6 && !slices.Contains(old.EvidenceIDs, id) {
					old.EvidenceIDs = append(old.EvidenceIDs, id)
				}
			}
			return r
		}
	}
	return append(r, v)
}
func timeline(device, attribute, profile, track string, w Range, positive, known, ambiguous, excluded []Range, proofs []proof) []Interval {
	positive = Union(positive, w)
	known = Union(known, w)
	ambiguous = Union(ambiguous, w)
	excluded = Union(excluded, w)
	points := endpoints(w, positive, known, ambiguous, excluded)
	r := []Interval{}
	for i := 0; i < len(points)-1; i++ {
		v := Interval{DeviceID: device, AttributeID: attribute, ProfileID: profile, Track: track, Range: Range{points[i], points[i+1]}, State: Unknown}
		switch {
		case contains(excluded, v.Start):
			v.State = Excluded
			v.Reasons = []string{"CONFIRMED_OBSERVATION_EXCLUSION"}
		case contains(positive, v.Start):
			v.State = Available
		case contains(ambiguous, v.Start):
			v.Reasons = []string{"SOURCE_SAMPLE_CLOCK_OR_AVAILABILITY_UNKNOWN"}
		case contains(known, v.Start):
			v.State = Unavailable
		default:
			v.Reasons = []string{"SOURCE_COVERAGE_UNKNOWN"}
		}
		r = appendInterval(r, v)
	}
	slices.SortFunc(proofs, func(a, b proof) int {
		if a.Start < b.Start {
			return -1
		}
		if a.Start > b.Start {
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	index := 0
	for i := range r {
		if r[i].State != Available {
			continue
		}
		for index < len(proofs) && proofs[index].End <= r[i].Start {
			index++
		}
		for j := index; j < len(proofs) && proofs[j].Start < r[i].End && len(r[i].EvidenceIDs) < 3; j++ {
			if proofs[j].End > r[i].Start && proofs[j].ID != "" {
				r[i].EvidenceIDs = append(r[i].EvidenceIDs, proofs[j].ID)
			}
		}
	}
	decorate(r)
	return r
}
func mergeAttributes(device, profile, track, merge string, w Range, sets [][]Interval) []Interval {
	ranges := [][]Range{}
	for _, set := range sets {
		rr := []Range{}
		for _, v := range set {
			rr = append(rr, v.Range)
		}
		ranges = append(ranges, rr)
	}
	points := endpoints(w, ranges...)
	indices := make([]int, len(sets))
	r := []Interval{}
	for i := 0; i < len(points)-1; i++ {
		v := Interval{DeviceID: device, ProfileID: profile, Track: track, Range: Range{points[i], points[i+1]}, State: Available}
		available, unavailable, unknown := false, false, false
		for j, set := range sets {
			for indices[j] < len(set) && set[indices[j]].End <= v.Start {
				indices[j]++
			}
			state := Unknown
			if indices[j] < len(set) && set[indices[j]].Start <= v.Start {
				item := set[indices[j]]
				state = item.State
				v.EvidenceIDs = append(v.EvidenceIDs, item.ID)
			}
			switch state {
			case Available:
				available = true
			case Unavailable:
				unavailable = true
			case Excluded:
				v.State = Excluded
			case Unknown:
				unknown = true
			}
		}
		if v.State != Excluded {
			if merge == "ANY" {
				switch {
				case available:
					v.State = Available
				case unknown:
					v.State = Unknown
				default:
					v.State = Unavailable
				}
			} else {
				switch {
				case unavailable:
					v.State = Unavailable
				case unknown:
					v.State = Unknown
				default:
					v.State = Available
				}
			}
		}
		if v.State == Unknown {
			v.Reasons = []string{"ATTRIBUTE_HISTORY_UNKNOWN"}
		}
		r = appendInterval(r, v)
	}
	decorate(r)
	return r
}
func connectionIntervals(device string, w Range, states []model.DeviceStateIntervalFact, excluded []Range) []Interval {
	online, offline, unknown := []Range{}, []Range{}, []Range{}
	for _, s := range states {
		if s.DeviceID != device {
			continue
		}
		r := Range{s.Start, s.End}
		if s.Quality != "KNOWN" || s.State == nil {
			unknown = append(unknown, r)
			continue
		}
		switch strings.ToUpper(s.State.ConnectionStatus) {
		case "CONNECTED", "ONLINE":
			online = append(online, r)
		case "DISCONNECTED", "OFFLINE":
			offline = append(offline, r)
		default:
			unknown = append(unknown, r)
		}
	}
	online, offline, unknown, excluded = Union(online, w), Union(offline, w), Union(unknown, w), Union(excluded, w)
	points := endpoints(w, online, offline, unknown, excluded)
	r := []Interval{}
	for i := 0; i < len(points)-1; i++ {
		v := Interval{DeviceID: device, Track: "connection", Range: Range{points[i], points[i+1]}, State: Unknown}
		switch {
		case contains(excluded, v.Start):
			v.State = Excluded
			v.Reasons = []string{"CONFIRMED_OBSERVATION_EXCLUSION"}
		case contains(unknown, v.Start) || (contains(online, v.Start) && contains(offline, v.Start)):
			v.Reasons = []string{"CONNECTION_HISTORY_UNKNOWN_OR_CONFLICT"}
		case contains(online, v.Start):
			v.State = Available
		case contains(offline, v.Start):
			v.State = Unavailable
		default:
			v.Reasons = []string{"CONNECTION_SEED_OR_HISTORY_MISSING"}
		}
		r = appendInterval(r, v)
	}
	decorate(r)
	return r
}
func summarize(device, attribute, profile, track string, w Range, intervals []Interval) Metric {
	m := Metric{ID: identity(device, attribute, profile, track, w), DeviceID: device, AttributeID: attribute, ProfileID: profile, Track: track, Range: w, WindowMs: w.End - w.Start}
	var gapStart, gapEnd int64
	wasGap := false
	for _, v := range intervals {
		duration := v.End - v.Start
		switch v.State {
		case Available:
			m.AvailableMs += duration
		case Unavailable:
			m.UnavailableMs += duration
			if !wasGap || gapEnd != v.Start {
				m.GapCount++
				gapStart = v.Start
			}
			gapEnd = v.End
			m.LongestGapMs = max(m.LongestGapMs, gapEnd-gapStart)
		case Unknown:
			m.UnknownMs += duration
		case Excluded:
			m.ExcludedMs += duration
		case NotApplicable:
			m.NotApplicableMs += duration
		}
		wasGap = v.State == Unavailable
	}
	m.PlannedMs = m.WindowMs - m.ExcludedMs
	known := m.AvailableMs + m.UnavailableMs
	if known > 0 {
		v := float64(m.AvailableMs) / float64(known)
		m.KnownAvailability = &v
	}
	if m.PlannedMs > 0 && m.NotApplicableMs == 0 {
		v := float64(known) / float64(m.PlannedMs)
		m.KnownCoverage = &v
		if m.UnknownMs == 0 {
			availability := float64(m.AvailableMs) / float64(m.PlannedMs)
			m.FullWindowAvailability = &availability
		}
	}
	return m
}
func stateIDs(intervals []Interval, state string) []string {
	ids := []string{}
	for _, v := range intervals {
		if v.State == state && len(ids) < 16 {
			ids = append(ids, v.ID)
		}
	}
	return ids
}
func gapIDs(intervals []Interval) []string { return stateIDs(intervals, Unavailable) }
