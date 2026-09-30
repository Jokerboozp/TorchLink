// Package maintenance computes descriptive observations of recorded physical
// assets and interventions. It never infers causality, safety or service life.
package maintenance

import (
	"fmt"
	"math"
	"slices"

	"iot-platform/internal/analytics/continuity"
	"iot-platform/internal/model"
)

const AlgorithmVersion = "maintenance-observation-v1"
const hourMs = int64(3600000)

type FaultCycle struct {
	ID             string
	DeviceID       string
	ComponentID    string
	EventAt        int64
	Type           string
	Classification string
	Confirmation   string
}
type SideInput struct {
	AssetID          string
	Asset            model.AssetInstance
	Window           model.FactRange
	Exclusions       []model.MaintenanceExclusion
	FaultSourceKnown []model.FactRange
	Faults           []FaultCycle
	States           []model.DeviceStateIntervalFact
	Admission        *model.MaintenanceAdmission
}

func ratio(numerator, denominator int64) *float64 {
	if denominator <= 0 {
		return nil
	}
	v := float64(numerator) / float64(denominator)
	return &v
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func ValidateAdmission(v *model.MaintenanceAdmission) error {
	if v == nil {
		return nil
	}
	if v.Version == "" || !finite(v.MinimumEffectiveHours) || v.MinimumEffectiveHours < 0 || !finite(v.MinimumObservationCoverage) || v.MinimumObservationCoverage < 0 || v.MinimumObservationCoverage > 1 || !finite(v.MinimumFaultSourceCoverage) || v.MinimumFaultSourceCoverage < 0 || v.MinimumFaultSourceCoverage > 1 || v.MinimumConfirmedCycles < 0 || v.PostMaintenanceWaitMs < 0 {
		return fmt.Errorf("比较准入参数无效")
	}
	return nil
}
func CalculateSide(input SideInput) (model.MaintenanceSideMetrics, error) {
	window := continuity.Range{Start: input.Window.Start, End: input.Window.End}
	if window.Start < 0 || window.End <= window.Start || len(input.Faults) > 50000 || len(input.States) > 100000 || len(input.Exclusions) > 5000 || len(input.FaultSourceKnown) > 100000 {
		return model.MaintenanceSideMetrics{}, fmt.Errorf("观察区间或输入数量无效")
	}
	if err := ValidateAdmission(input.Admission); err != nil {
		return model.MaintenanceSideMetrics{}, err
	}
	out := model.MaintenanceSideMetrics{WindowMs: window.End - window.Start, Exclusions: []model.MaintenanceExclusion{}, Limitations: []string{}}
	asset := input.Asset
	if asset.BoundaryStatus != "CONFIRMED" || asset.EffectiveStart <= 0 || asset.DeviceID == "" || asset.PhysicalID == "" {
		out.Limitations = append(out.Limitations, "ASSET_BOUNDARY_UNKNOWN_OR_DISPUTED")
		return out, nil
	}
	assetStart := asset.EffectiveStart
	if asset.CommissionedAt != nil {
		assetStart = max(assetStart, *asset.CommissionedAt)
	}
	assetEnd := window.End
	if asset.EffectiveEnd != nil {
		assetEnd = min(assetEnd, *asset.EffectiveEnd)
	}
	if asset.RetiredAt != nil {
		assetEnd = min(assetEnd, *asset.RetiredAt)
	}
	bounds := continuity.Range{Start: max(window.Start, assetStart), End: min(window.End, assetEnd)}
	if bounds.End <= bounds.Start {
		out.Limitations = append(out.Limitations, "WINDOW_OUTSIDE_PHYSICAL_ASSET")
		return out, nil
	}
	out.AssetWindowMs = bounds.End - bounds.Start
	excluded := []continuity.Range{}
	for _, v := range input.Exclusions {
		if !slices.Contains([]string{"PLANNED_STOP", "CONFIRMED_TEST", "PLATFORM_OBSERVATION_UNAVAILABLE"}, v.Kind) || v.SourceRevisionID == "" || v.End <= v.Start {
			return out, fmt.Errorf("排除依据类型、版本或区间无效")
		}
		r := continuity.Range{Start: max(bounds.Start, v.Start), End: min(bounds.End, v.End)}
		if r.End <= r.Start {
			continue
		}
		excluded = append(excluded, r)
		v.Start, v.End = r.Start, r.End
		out.Exclusions = append(out.Exclusions, v)
	}
	excluded = continuity.Union(excluded, bounds)
	out.ExcludedMs = duration(excluded)
	effective := subtract([]continuity.Range{bounds}, excluded)
	out.EffectiveMs = duration(effective)
	out.EffectiveHours = float64(out.EffectiveMs) / float64(hourMs)
	out.ObservationCoverage = ratio(out.EffectiveMs, out.AssetWindowMs)
	known := []continuity.Range{}
	for _, r := range input.FaultSourceKnown {
		known = append(known, continuity.Range{Start: r.Start, End: r.End})
	}
	known = intersections(effective, continuity.Union(known, bounds))
	out.FaultSourceKnownMs = duration(known)
	out.FaultSourceUnknownMs = out.EffectiveMs - out.FaultSourceKnownMs
	out.FaultSourceCoverage = ratio(out.FaultSourceKnownMs, out.EffectiveMs)
	cycles := map[string]FaultCycle{}
	disputed := map[string]bool{}
	unknownFaultClock := false
	for _, cycle := range input.Faults {
		if cycle.ID == "" {
			return out, fmt.Errorf("故障周期缺少稳定身份")
		}
		if old, ok := cycles[cycle.ID]; ok && old != cycle {
			disputed[cycle.ID] = true
		}
		cycles[cycle.ID] = cycle
	}
	cycleIDs := make([]string, 0, len(cycles))
	for id := range cycles {
		cycleIDs = append(cycleIDs, id)
	}
	slices.Sort(cycleIDs)
	for _, id := range cycleIDs {
		cycle := cycles[id]
		if disputed[id] {
			out.Limitations = unique(out.Limitations, "FAULT_CYCLE_IDENTITY_DISPUTED")
			continue
		}
		if cycle.DeviceID != asset.DeviceID || (asset.ComponentID != "" && cycle.ComponentID != asset.ComponentID) || cycle.Type != "FAULT" || cycle.Classification != "PRODUCTION" || cycle.Confirmation != "CONFIRMED" {
			continue
		}
		if cycle.EventAt <= 0 {
			out.Limitations = unique(out.Limitations, "FAULT_EVENT_CLOCK_UNKNOWN")
			unknownFaultClock = true
			continue
		}
		if covers(effective, cycle.EventAt) {
			out.ConfirmedFaultCycles++
		}
	}
	// No configured admission means descriptive counts only. Complete source
	// coverage does not authorize an invented comparison policy.
	if input.Admission != nil && out.EffectiveMs > 0 && out.FaultSourceKnownMs > 0 && !unknownFaultClock && len(disputed) == 0 && out.FaultSourceCoverage != nil && *out.FaultSourceCoverage >= input.Admission.MinimumFaultSourceCoverage {
		value := float64(out.ConfirmedFaultCycles) / out.EffectiveHours * 1000
		out.FaultsPer1000Hours = &value
	} else {
		out.Limitations = unique(out.Limitations, "FAULT_RATE_ADMISSION_OR_SOURCE_INSUFFICIENT")
	}
	// Sweep interval boundaries once. Unknown or conflicting overlapping states
	// remain unknown; gaps never become online. Work is O(n log n), including
	// the maximum permitted state history.
	type delta struct{ effective, online, offline, unknown int }
	changes := map[int64]delta{}
	add := func(at int64, change delta) {
		v := changes[at]
		v.effective += change.effective
		v.online += change.online
		v.offline += change.offline
		v.unknown += change.unknown
		changes[at] = v
	}
	for _, r := range effective {
		add(r.Start, delta{effective: 1})
		add(r.End, delta{effective: -1})
	}
	for _, state := range input.States {
		if state.DeviceID != asset.DeviceID {
			continue
		}
		if state.End <= state.Start {
			return out, fmt.Errorf("状态区间无效")
		}
		start, end := max(bounds.Start, state.Start), min(bounds.End, state.End)
		if end <= start {
			continue
		}
		change := delta{unknown: 1}
		if state.Quality == "KNOWN" && state.State != nil {
			switch state.State.ConnectionStatus {
			case "CONNECTED", "ONLINE":
				change = delta{online: 1}
			case "DISCONNECTED", "OFFLINE":
				change = delta{offline: 1}
			}
		}
		add(start, change)
		add(end, delta{online: -change.online, offline: -change.offline, unknown: -change.unknown})
	}
	points := make([]int64, 0, len(changes))
	for at := range changes {
		points = append(points, at)
	}
	slices.Sort(points)
	counts := delta{}
	for i := 0; i < len(points)-1; i++ {
		change := changes[points[i]]
		counts.effective += change.effective
		counts.online += change.online
		counts.offline += change.offline
		counts.unknown += change.unknown
		if counts.effective <= 0 {
			continue
		}
		elapsed := points[i+1] - points[i]
		switch {
		case counts.unknown > 0 || (counts.online > 0) == (counts.offline > 0):
			out.StateUnknownMs += elapsed
		case counts.online > 0:
			out.OnlineMs += elapsed
		default:
			out.OfflineMs += elapsed
		}
	}
	stateKnown := out.OnlineMs + out.OfflineMs
	out.KnownOfflineRatio = ratio(out.OfflineMs, stateKnown)
	out.StateCoverage = ratio(stateKnown, out.EffectiveMs)
	if out.StateUnknownMs == 0 {
		out.FullWindowOfflineRatio = ratio(out.OfflineMs, out.EffectiveMs)
	}
	if asset.CommissionedAt == nil {
		out.Limitations = unique(out.Limitations, "COMMISSIONING_DATE_AND_AGE_UNKNOWN")
	}
	if out.FaultSourceUnknownMs > 0 {
		out.Limitations = unique(out.Limitations, "FAULT_SOURCE_INTERVAL_UNKNOWN")
	}
	if out.StateUnknownMs > 0 {
		out.Limitations = unique(out.Limitations, "CONNECTION_STATE_INTERVAL_UNKNOWN")
	}
	return out, nil
}

func duration(ranges []continuity.Range) int64 {
	var sum int64
	for _, v := range ranges {
		sum += v.End - v.Start
	}
	return sum
}
func covers(ranges []continuity.Range, at int64) bool {
	index, _ := slices.BinarySearchFunc(ranges, at, func(r continuity.Range, v int64) int {
		if r.End <= v {
			return -1
		}
		if r.Start > v {
			return 1
		}
		return 0
	})
	return index < len(ranges) && at >= ranges[index].Start && at < ranges[index].End
}
func unique(values []string, v string) []string {
	if !slices.Contains(values, v) {
		values = append(values, v)
	}
	return values
}

// Both arguments are sorted, disjoint unions. Linear traversal avoids
// multiplying exclusion and history sizes.
func intersections(a, b []continuity.Range) []continuity.Range {
	result := []continuity.Range{}
	for i, j := 0, 0; i < len(a) && j < len(b); {
		start, end := max(a[i].Start, b[j].Start), min(a[i].End, b[j].End)
		if end > start {
			result = append(result, continuity.Range{Start: start, End: end})
		}
		if a[i].End < b[j].End {
			i++
		} else {
			j++
		}
	}
	return result
}
func subtract(a, b []continuity.Range) []continuity.Range {
	result := []continuity.Range{}
	j := 0
	for _, x := range a {
		cursor := x.Start
		for j < len(b) && b[j].End <= cursor {
			j++
		}
		for k := j; k < len(b) && b[k].Start < x.End; k++ {
			if b[k].Start > cursor {
				result = append(result, continuity.Range{Start: cursor, End: min(x.End, b[k].Start)})
			}
			cursor = max(cursor, b[k].End)
			if cursor >= x.End {
				break
			}
		}
		if cursor < x.End {
			result = append(result, continuity.Range{Start: cursor, End: x.End})
		}
	}
	return result
}

func Compare(before, after SideInput, params model.MaintenanceObservationParameters, endedAt int64, verification string, contextsComparable bool, confounders []string) (model.MaintenanceObservation, error) {
	result := model.MaintenanceObservation{Parameters: params, Verification: verification, Confounders: slices.Clone(confounders), Limitations: []string{"DESCRIPTIVE_CHANGE_DOES_NOT_PROVE_CAUSALITY_OR_SAFETY"}}
	if endedAt < 0 {
		return result, fmt.Errorf("维修结束时间无效")
	}
	if params.ComparisonType == "SAME_INSTANCE_REPAIR" {
		if before.AssetID != after.AssetID {
			return result, fmt.Errorf("同实例维修两侧必须绑定同一实物")
		}
	} else if params.ComparisonType == "CROSS_INSTANCE_REPLACEMENT" {
		if before.AssetID == after.AssetID || params.SwitchAt <= 0 || before.Asset.EffectiveEnd == nil || *before.Asset.EffectiveEnd != params.SwitchAt || after.Asset.EffectiveStart != params.SwitchAt {
			return result, fmt.Errorf("替换两侧必须绑定不同实物及已确认切换边界")
		}
	} else {
		return result, fmt.Errorf("比较类型无效")
	}
	var err error
	result.Before, err = CalculateSide(before)
	if err != nil {
		return result, err
	}
	result.After, err = CalculateSide(after)
	if err != nil {
		return result, err
	}
	result.Status = "READY"
	admission := params.Admission
	if admission == nil {
		result.Status = "INSUFFICIENT"
		result.Limitations = append(result.Limitations, "COMPARISON_ADMISSION_NOT_CONFIGURED")
	} else {
		if err = ValidateAdmission(admission); err != nil {
			return result, err
		}
		for _, side := range []model.MaintenanceSideMetrics{result.Before, result.After} {
			if side.EffectiveHours < admission.MinimumEffectiveHours || side.ObservationCoverage == nil || *side.ObservationCoverage < admission.MinimumObservationCoverage || side.StateCoverage == nil || *side.StateCoverage < admission.MinimumObservationCoverage || side.FaultSourceCoverage == nil || *side.FaultSourceCoverage < admission.MinimumFaultSourceCoverage || side.FaultsPer1000Hours == nil || side.ConfirmedFaultCycles < admission.MinimumConfirmedCycles {
				result.Status = "INSUFFICIENT"
			}
		}
		if after.Window.Start < endedAt || after.Window.Start-endedAt < admission.PostMaintenanceWaitMs {
			result.Status = "INSUFFICIENT"
			result.Limitations = append(result.Limitations, "POST_MAINTENANCE_WAIT_INSUFFICIENT")
		}
		if admission.ComparableContextRequired && !contextsComparable {
			result.Status = "INSUFFICIENT"
			result.Limitations = append(result.Limitations, "OPERATING_CONTEXT_UNKNOWN_OR_INCOMPARABLE")
		}
	}
	if verification != "PASSED" {
		result.Status = "INSUFFICIENT"
		result.Limitations = append(result.Limitations, "FUNCTIONAL_VERIFICATION_NOT_PASSED")
	}
	if len(confounders) > 0 {
		result.Status = "CONFOUNDED"
	}
	return result, nil
}
