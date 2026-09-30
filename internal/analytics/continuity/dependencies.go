package continuity

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"iot-platform/internal/model"
)

// DependencyGroups uses historical effective intervals. CURRENT_SNAPSHOT facts
// produce a point snapshot at the fixed read time, never a historical interval.
// The caller has already authorized profile/collector metadata. A parent device
// must additionally belong to this explicit device set before its ID is exposed.
func DependencyGroups(deviceIDs []string, window Range, facts []model.DependencyFact, readAt int64) ([]DependencyGroup, []string, error) {
	ids := slices.Clone(deviceIDs)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	if len(ids) == 0 || len(ids) > 1000 || !valid(window) {
		return nil, nil, fmt.Errorf("explicit dependency scope required")
	}
	byGroup := map[string][]model.DependencyFact{}
	limits := []string{}
	for _, f := range facts {
		if !slices.Contains(ids, f.DeviceID) || f.ResourceID == "" || f.Kind == "" {
			continue
		}
		if (f.Kind == "parent-device" || f.Kind == "PARENT_DEVICE") && !slices.Contains(ids, f.ResourceID) {
			limits = append(limits, "PARENT_RESOURCE_NOT_AUTHORIZED")
			continue
		}
		if f.Quality != "KNOWN" && f.Quality != "CURRENT_SNAPSHOT" {
			limits = append(limits, "DEPENDENCY_HISTORY_UNKNOWN")
			continue
		}
		key := identity(f.Kind, f.ResourceID, f.Quality)
		byGroup[key] = append(byGroup[key], f)
	}
	keys := make([]string, 0, len(byGroup))
	for key := range byGroup {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	groups := []DependencyGroup{}
	for _, key := range keys {
		members := byGroup[key]
		first := members[0]
		if first.Quality == "CURRENT_SNAPSHOT" {
			visible := []string{}
			evidence := []string{}
			for _, f := range members {
				visible = append(visible, f.DeviceID)
				if f.ID != "" && len(evidence) < 20 {
					evidence = append(evidence, f.ID)
				}
			}
			slices.Sort(visible)
			visible = slices.Compact(visible)
			slices.Sort(evidence)
			groups = append(groups, DependencyGroup{ID: identity(key, readAt, visible), Kind: first.Kind, ResourceID: first.ResourceID, Range: Range{readAt, readAt}, MemberIDs: visible, VisibleDeviceCount: len(visible), AnalysisDeviceCount: len(ids), Concentration: float64(len(visible)) / float64(len(ids)), HistoryQuality: "CURRENT_ONLY", EvidenceIDs: evidence})
			limits = append(limits, "CURRENT_DEPENDENCY_SNAPSHOT_HAS_NO_HISTORICAL_COVERAGE")
			continue
		}
		type change struct {
			at       int64
			id       string
			delta    int
			evidence string
		}
		changes := []change{}
		for _, f := range members {
			end := f.EffectiveTo
			if end == 0 {
				end = window.End
			}
			r := clip(window, Range{f.EffectiveFrom, end})
			if !valid(r) {
				continue
			}
			changes = append(changes, change{r.Start, f.DeviceID, 1, f.ID}, change{r.End, f.DeviceID, -1, f.ID})
		}
		slices.SortFunc(changes, func(a, b change) int {
			if a.at < b.at {
				return -1
			}
			if a.at > b.at {
				return 1
			}
			return strings.Compare(a.id, b.id)
		})
		counts := map[string]int{}
		activeEvidence := map[string]int{}
		i := 0
		for i < len(changes) {
			at := changes[i].at
			for i < len(changes) && changes[i].at == at {
				c := changes[i]
				counts[c.id] += c.delta
				activeEvidence[c.evidence] += c.delta
				i++
			}
			if i == len(changes) {
				break
			}
			end := changes[i].at
			visible := []string{}
			for id, count := range counts {
				if count > 0 {
					visible = append(visible, id)
				}
			}
			if len(visible) == 0 || at >= end {
				continue
			}
			slices.Sort(visible)
			evidence := []string{}
			for id, count := range activeEvidence {
				if count > 0 && id != "" {
					evidence = append(evidence, id)
				}
			}
			slices.Sort(evidence)
			if len(evidence) > 20 {
				evidence = evidence[:20]
			}
			group := DependencyGroup{ID: identity(key, at, end, visible), Kind: first.Kind, ResourceID: first.ResourceID, Range: Range{at, end}, MemberIDs: visible, VisibleDeviceCount: len(visible), AnalysisDeviceCount: len(ids), Concentration: float64(len(visible)) / float64(len(ids)), HistoryQuality: "KNOWN", EvidenceIDs: evidence}
			groups = append(groups, group)
			if len(groups) > 10000 {
				return nil, nil, fmt.Errorf("dependency interval resource limit exceeded")
			}
		}
	}
	slices.Sort(limits)
	limits = slices.Compact(limits)
	return groups, limits, nil
}

func Hypothesis(group DependencyGroup, deviceIDs []string, at int64) ([]string, error) {
	if group.ID == "" || len(group.MemberIDs) == 0 || !slices.Contains([]string{"KNOWN", "CURRENT_ONLY"}, group.HistoryQuality) {
		return nil, fmt.Errorf("authorized dependency snapshot required")
	}
	if group.HistoryQuality == "KNOWN" && (at < group.Start || at >= group.End) {
		return nil, fmt.Errorf("hypothesis time is outside the dependency snapshot")
	}
	if group.HistoryQuality == "CURRENT_ONLY" && at != group.Start {
		return nil, fmt.Errorf("current-only hypothesis must use its fixed snapshot time")
	}
	for _, id := range group.MemberIDs {
		if !slices.Contains(deviceIDs, id) {
			return nil, fmt.Errorf("dependency snapshot scope changed")
		}
	}
	return slices.Clone(group.MemberIDs), nil
}

// CommonGaps compares only known missing intervals. Unknown, excluded and
// unconfigured periods cannot prove a shared missing-report event.
func CommonGaps(results []DeviceResult, window Range, p CommonGapPolicy, maxPairs int) ([]Finding, error) {
	if p.Version == "" || p.MinimumGapCount < 1 || p.MinimumOverlapMs <= 0 || p.MinimumJaccard < 0 || p.MinimumJaccard > 1 || math.IsNaN(p.MinimumJaccard) {
		return nil, fmt.Errorf("versioned common-gap policy required")
	}
	if maxPairs <= 0 {
		maxPairs = 10000
	}
	count := len(results) * (len(results) - 1) / 2
	if count > maxPairs {
		return nil, fmt.Errorf("common-gap pair resource limit exceeded")
	}
	results = slices.Clone(results)
	slices.SortFunc(results, func(a, b DeviceResult) int { return strings.Compare(a.DeviceID, b.DeviceID) })
	type gaps struct {
		ranges []Range
		ids    []string
	}
	byDevice := map[string]gaps{}
	for _, r := range results {
		g := gaps{}
		for _, v := range r.Intervals {
			if v.Track == "data" && v.AttributeID == "" && v.State == Unavailable {
				g.ranges = append(g.ranges, v.Range)
				g.ids = append(g.ids, v.ID)
			}
		}
		g.ranges = Union(g.ranges, window)
		byDevice[r.DeviceID] = g
	}
	findings := []Finding{}
	for i, a := range results {
		ga := byDevice[a.DeviceID]
		if len(ga.ranges) < p.MinimumGapCount {
			continue
		}
		for _, b := range results[i+1:] {
			gb := byDevice[b.DeviceID]
			if len(gb.ranges) < p.MinimumGapCount {
				continue
			}
			overlap := Duration(Intersection(ga.ranges, gb.ranges, window))
			union := Duration(Union(append(slices.Clone(ga.ranges), gb.ranges...), window))
			if union == 0 || overlap < p.MinimumOverlapMs {
				continue
			}
			similarity := float64(overlap) / float64(union)
			if similarity < p.MinimumJaccard {
				continue
			}
			evidence := append(slices.Clone(ga.ids), gb.ids...)
			if len(evidence) > 16 {
				evidence = evidence[:16]
			}
			findings = append(findings, Finding{ID: identity(a.DeviceID, b.DeviceID, p.Version, window), Kind: "COMMON_MISSING_REPORT", Range: window, Explanation: "已知缺报区间存在重叠；相似性不能证明共同原因。", EvidenceIDs: evidence, Values: map[string]any{"deviceIds": []string{a.DeviceID, b.DeviceID}, "policyVersion": p.Version, "overlapMs": overlap, "unionMs": union, "jaccard": similarity, "minimumGapCount": p.MinimumGapCount, "minimumOverlapMs": p.MinimumOverlapMs, "minimumJaccard": p.MinimumJaccard}})
		}
	}
	return findings, nil
}
