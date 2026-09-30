package recurring

import (
	"iot-platform/internal/model"
	"slices"
	"sort"
)

const CycleAlgorithmVersion = "alarm-signal-cycles-v1"

type CycleCoverage struct {
	Start, End int64
	Reliable   bool
	Reason     string
}
type CycleInput struct {
	TenantID, Method                string
	Start, End                      int64
	Observations                    []model.AlarmObservation
	Seed                            *model.AlarmObservation
	SeedMaxAge                      int64
	Coverage                        []CycleCoverage
	AlgorithmVersion, ConfigVersion string
}

// RebuildCycles consumes production acceptance in recorded order; it never
// reorders rejected late state into a new physical history. REPORT_ONLY emits
// no cycles. Coverage is explicit evidence, not inferred from heartbeats.
func RebuildCycles(in CycleInput) []model.AlarmCycleRevision {
	out := []model.AlarmCycleRevision{}
	if in.Method == "REPORT_ONLY" || in.Start >= in.End {
		return out
	}
	if !slices.Contains([]string{"COMPONENT_BOOLEAN", "DIRECT_EXPLICIT_STATE", "RECORDED_RULE_LIFECYCLE"}, in.Method) {
		return out
	}
	observations := append([]model.AlarmObservation{}, in.Observations...)
	sort.SliceStable(observations, func(i, j int) bool {
		if observations[i].RecordedAt != observations[j].RecordedAt {
			return observations[i].RecordedAt < observations[j].RecordedAt
		}
		if observations[i].EvaluationAt != observations[j].EvaluationAt {
			return observations[i].EvaluationAt < observations[j].EvaluationAt
		}
		return observations[i].ID < observations[j].ID
	})
	var anchor *model.AlarmObservation
	for i := range observations {
		if usableCycleFact(observations[i], in.Method) {
			anchor = &observations[i]
			break
		}
	}
	if anchor == nil && in.Seed != nil && usableCycleFact(*in.Seed, in.Method) {
		anchor = in.Seed
	}
	if anchor == nil {
		return out
	}
	basis := "SOURCE_EVENT"
	if in.Method == "RECORDED_RULE_LIFECYCLE" {
		basis = "RULE_EVALUATION"
	}
	at := func(o model.AlarmObservation) int64 {
		if basis == "RULE_EVALUATION" {
			return o.EvaluationAt
		}
		return o.EventAt
	}
	algorithm := in.AlgorithmVersion
	if algorithm == "" {
		algorithm = CycleAlgorithmVersion
	}
	var open *model.AlarmCycleRevision
	knownNormal := false
	var last *model.AlarmObservation
	var lastAt int64
	appendMember := func(c *model.AlarmCycleRevision, o model.AlarmObservation) {
		if !slices.Contains(c.ObservationIDs, o.ID) {
			c.ObservationIDs = append(c.ObservationIDs, o.ID)
		}
		if o.AlarmID != "" && !slices.Contains(c.AlarmIDs, o.AlarmID) {
			c.AlarmIDs = append(c.AlarmIDs, o.AlarmID)
		}
	}
	newCycle := func(o model.AlarmObservation, start int64, known bool) *model.AlarmCycleRevision {
		c := &model.AlarmCycleRevision{TenantID: o.TenantID, DeviceID: o.DeviceID, ComponentID: o.ComponentID, AssetInstanceID: o.AssetInstanceID, AlarmType: o.AlarmType, OriginKind: o.OriginKind, SignalKey: o.SignalKey, Method: in.Method, Status: "OPEN", BoundaryQuality: "BOTH_CENSORED", TimeBasis: basis, StartAt: start, StartKnown: known, NewStart: known && start >= in.Start && start < in.End, ObservationIDs: []string{}, AlarmIDs: []string{}, AlgorithmVersion: algorithm, ConfigVersion: in.ConfigVersion, Limitations: []string{}}
		if o.AssetInstanceID == "" {
			c.Limitations = append(c.Limitations, "ASSET_IDENTITY_UNKNOWN")
		}
		appendMember(c, o)
		return c
	}
	finish := func(c *model.AlarmCycleRevision) {
		if c == nil {
			return
		}
		switch {
		case c.StartKnown && c.EndKnown:
			c.BoundaryQuality = "COMPLETE"
		case c.StartKnown:
			c.BoundaryQuality = "RIGHT_CENSORED"
		case c.EndKnown:
			c.BoundaryQuality = "LEFT_CENSORED"
		default:
			c.BoundaryQuality = "BOTH_CENSORED"
		}
		if c.Status == "TERMINATED_UNKNOWN" {
			c.BoundaryQuality = "UNKNOWN"
			c.DurationMillis = nil
			c.KnownDurationLowerBound = nil
		}
		c.InputHash = model.ObservationHash(struct {
			Method            string
			Start, End        int64
			Seed              *model.AlarmObservation
			Members           []string
			Coverage          []CycleCoverage
			Algorithm, Config string
		}{in.Method, in.Start, in.End, in.Seed, c.ObservationIDs, in.Coverage, algorithm, in.ConfigVersion})
		c.ID = "acyc_" + model.ObservationHash(struct {
			Point   string
			Members []string
			Hash    string
		}{c.SignalKey, c.ObservationIDs, c.InputHash})[:32]
		out = append(out, *c)
	}
	seed := in.Seed
	if seed != nil && usableCycleFact(*seed, in.Method) && sameSignal(*seed, *anchor) && at(*seed) < in.Start && in.SeedMaxAge > 0 && in.Start-at(*seed) <= in.SeedMaxAge && continuouslyCovered(in.Coverage, at(*seed), in.Start) {
		copySeed := *seed
		last = &copySeed
		lastAt = at(*seed)
		if seed.FactKind == "CLEAR" {
			knownNormal = true
		} else {
			open = newCycle(*seed, 0, false)
			open.Limitations = append(open.Limitations, "WINDOW_LEFT_CENSORED")
		}
	}
	for _, o := range observations {
		t := at(o)
		if t < in.Start || t >= in.End || !usableCycleFact(o, in.Method) {
			continue
		}
		// A new object/version or a known monitoring gap terminates continuity.
		same := last == nil || sameSignal(*last, o)
		continuous := last == nil || continuouslyCovered(in.Coverage, lastAt, t)
		if !same || !continuous {
			if open != nil {
				open.Status = "TERMINATED_UNKNOWN"
				if !same {
					open.Limitations = append(open.Limitations, "IDENTITY_OR_SIGNAL_VERSION_CHANGED")
				} else {
					open.Limitations = append(open.Limitations, "MONITORING_CONTINUITY_UNKNOWN")
				}
				finish(open)
				open = nil
			}
			knownNormal = false
		}
		// Late states are retained as source evidence; production acceptance and
		// watermark prevent them from ending or reversing the newer state.
		if last != nil && same && t < lastAt {
			continue
		}
		switch o.FactKind {
		case "ASSERT":
			if open == nil {
				open = newCycle(o, 0, false)
				if knownNormal && continuous {
					open.StartAt = t
					open.StartKnown = true
					open.NewStart = true
				} else {
					open.Limitations = append(open.Limitations, "START_BOUNDARY_UNKNOWN")
				}
			} else {
				appendMember(open, o)
			}
			knownNormal = false
		case "CLEAR":
			if open != nil {
				appendMember(open, o)
				open.Status = "CLEARED"
				open.EndAt = t
				open.EndKnown = true
				if open.StartKnown && continuouslyCovered(in.Coverage, open.StartAt, t) {
					d := t - open.StartAt
					open.DurationMillis = &d
				}
				finish(open)
				open = nil
			}
			knownNormal = true
		}
		copyO := o
		last = &copyO
		lastAt = t
	}
	if open != nil {
		if !continuouslyCovered(in.Coverage, lastAt, in.End) {
			open.Status = "TERMINATED_UNKNOWN"
			open.Limitations = append(open.Limitations, "WINDOW_END_COVERAGE_UNKNOWN")
		} else if open.StartKnown && continuouslyCovered(in.Coverage, open.StartAt, in.End) {
			d := in.End - open.StartAt
			open.KnownDurationLowerBound = &d
		}
		finish(open)
	}
	return out
}

func usableCycleFact(o model.AlarmObservation, method string) bool {
	if o.Acceptance != "ACCEPTED" || o.FactKind != "ASSERT" && o.FactKind != "CLEAR" || o.IdentityQuality == "SOURCE_UNKNOWN" || o.HistoricalQuality == "PARTIAL" {
		return false
	}
	if method == "RECORDED_RULE_LIFECYCLE" {
		return o.OriginKind == "RULE_LIFECYCLE" && o.RuleID != "" && o.RuleVersion > 0 && o.ConditionHash != "" && o.EvaluationAt > 0
	}
	if o.TimeQuality != "TRUSTED" || o.EventAt <= 0 {
		return false
	}
	if method == "COMPONENT_BOOLEAN" {
		return o.OriginKind == "COMPONENT_STATE" && o.ComponentID != ""
	}
	return o.OriginKind == "DEVICE_DIRECT"
}
func sameSignal(a, b model.AlarmObservation) bool {
	return a.TenantID == b.TenantID && a.DeviceID == b.DeviceID && a.ComponentID == b.ComponentID && a.AssetInstanceID == b.AssetInstanceID && a.AlarmType == b.AlarmType && a.OriginKind == b.OriginKind && a.SignalKey == b.SignalKey && a.ProtocolVersion == b.ProtocolVersion && a.RuleVersion == b.RuleVersion && a.ConditionHash == b.ConditionHash
}
func continuouslyCovered(coverage []CycleCoverage, start, end int64) bool {
	if start > end {
		return false
	}
	if start == end {
		return true
	}
	for _, c := range coverage {
		if !c.Reliable && c.Start < end && c.End > start {
			return false
		}
	}
	ranges := append([]CycleCoverage{}, coverage...)
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].Start < ranges[j].Start })
	pos := start
	for _, c := range ranges {
		if !c.Reliable || c.End <= pos {
			continue
		}
		if c.Start > pos {
			return false
		}
		pos = max(pos, c.End)
		if pos >= end {
			return true
		}
	}
	return false
}
