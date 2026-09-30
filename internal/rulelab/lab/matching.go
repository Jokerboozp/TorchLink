package lab

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"iot-platform/internal/model"
)

const maxMatchVertices = 500
const maxMatchPairs = 100000

type matchPoint struct {
	ID, Device, Type string
	At, End          int64
}
type paired struct {
	Left, Right int
	Difference  int64
}
type flowEdge struct {
	to, reverse, capacity int
	cost                  int64
}

// Minimum-cost maximum bipartite flow. Stable ID ordering and stable traversal
// make equivalent optima reproducible. Cardinality is never traded for cost.
func maximumMatch(left, right []matchPoint, tolerance int64) ([]paired, error) {
	if len(left)+len(right) > maxMatchVertices {
		return nil, fmt.Errorf("MATCH_VERTEX_CAP_REACHED")
	}
	vertices := len(left) + len(right) + 2
	source, sink := vertices-2, vertices-1
	graph := make([][]flowEdge, vertices)
	add := func(a, b int, cost int64) {
		graph[a] = append(graph[a], flowEdge{b, len(graph[b]), 1, cost})
		graph[b] = append(graph[b], flowEdge{a, len(graph[a]) - 1, 0, -cost})
	}
	for i := range left {
		add(source, i, 0)
	}
	for j := range right {
		add(len(left)+j, sink, 0)
	}
	count := 0
	for i, a := range left {
		for j, b := range right {
			if a.Device != b.Device || a.Type != b.Type {
				continue
			}
			end := b.End
			if end == 0 {
				end = b.At
			}
			if a.At < b.At-tolerance || a.At > end+tolerance {
				continue
			}
			delta := a.At - b.At
			if delta < 0 {
				delta = -delta
			}
			add(i, len(left)+j, delta)
			count++
			if count > maxMatchPairs {
				return nil, fmt.Errorf("MATCH_PAIR_CAP_REACHED")
			}
		}
	}
	potential := make([]int64, vertices)
	for {
		dist := make([]int64, vertices)
		prevNode, prevEdge := make([]int, vertices), make([]int, vertices)
		visited := make([]bool, vertices)
		for i := range dist {
			dist[i] = math.MaxInt64 / 4
			prevNode[i] = -1
		}
		dist[source] = 0
		for range vertices {
			node := -1
			for i := range dist {
				if !visited[i] && (node < 0 || dist[i] < dist[node]) {
					node = i
				}
			}
			if node < 0 || dist[node] == math.MaxInt64/4 {
				break
			}
			visited[node] = true
			for k, e := range graph[node] {
				if e.capacity == 0 || visited[e.to] {
					continue
				}
				candidate := dist[node] + e.cost + potential[node] - potential[e.to]
				if candidate < dist[e.to] {
					dist[e.to] = candidate
					prevNode[e.to] = node
					prevEdge[e.to] = k
				}
			}
		}
		if prevNode[sink] < 0 {
			break
		}
		for i, d := range dist {
			if d < math.MaxInt64/4 {
				potential[i] += d
			}
		}
		for node := sink; node != source; {
			prev, k := prevNode[node], prevEdge[node]
			e := &graph[prev][k]
			e.capacity--
			graph[node][e.reverse].capacity++
			node = prev
		}
	}
	result := []paired{}
	for i := range left {
		for _, e := range graph[i] {
			j := e.to - len(left)
			if j >= 0 && j < len(right) && e.capacity == 0 {
				result = append(result, paired{i, j, left[i].At - right[j].At})
			}
		}
	}
	return result, nil
}
func outcomePoints(values []model.RuleLabOutcome, basis string) []matchPoint {
	points := make([]matchPoint, 0, len(values))
	for _, v := range values {
		at := v.TriggerEventAt
		if basis == "PROCESSING" {
			at = v.TriggeredAt
		}
		if at > 0 {
			points = append(points, matchPoint{ID: v.ID, Device: v.DeviceID, Type: v.AlarmType, At: at})
		}
	}
	slices.SortFunc(points, func(a, b matchPoint) int { return strings.Compare(a.ID, b.ID) })
	return points
}

type labelEvaluation struct {
	Status                string                         `json:"status"`
	TP                    int                            `json:"tp"`
	AlarmCycles           int                            `json:"alarmCycles"`
	RealEvents            int                            `json:"realEvents"`
	ExtraCycles           int                            `json:"extraCycles"`
	Precision             *float64                       `json:"precision,omitempty"`
	Recall                *float64                       `json:"recall,omitempty"`
	Matching              []model.RuleLabEventMatch      `json:"matching"`
	CommonEvaluationSet   []model.AnalysisConfigRevision `json:"commonEvaluationSet"`
	ExcludedLabelIDs      []string                       `json:"excludedLabelIds"`
	IndependentValidation bool                           `json:"independentValidation"`
	Limitations           []string                       `json:"limitations"`
}

func evaluateLabels(run model.AnalysisRun, fixed frozenExperiment, policy model.RuleLabEvaluationPolicy, cycles []model.RuleLabOutcome, computable bool) labelEvaluation {
	r := labelEvaluation{Status: "NOT_EVALUABLE", Matching: []model.RuleLabEventMatch{}, CommonEvaluationSet: []model.AnalysisConfigRevision{}, ExcludedLabelIDs: []string{}, Limitations: []string{}, IndependentValidation: policy.Split == "HOLDOUT" && fixed.HoldoutPreviousUses == 0}
	positive, negatives := []matchPoint{}, []matchPoint{}
	excluded := []matchPoint{}
	for _, v := range fixed.Labels {
		var b model.RuleLabLabel
		if decode(v.Body, &b) != nil {
			r.Limitations = unique(r.Limitations, "INVALID_FROZEN_LABEL")
			continue
		}
		p := matchPoint{ID: v.ID, Device: b.DeviceID, Type: b.EventType, At: b.Start, End: b.End}
		switch b.Conclusion {
		case "CONFIRMED_EVENT":
			positive = append(positive, p)
			r.CommonEvaluationSet = append(r.CommonEvaluationSet, v)
		case "NO_ABNORMALITY_FOUND":
			negatives = append(negatives, p)
			r.CommonEvaluationSet = append(r.CommonEvaluationSet, v)
		default:
			excluded = append(excluded, p)
			r.ExcludedLabelIDs = append(r.ExcludedLabelIDs, v.ID)
		}
	}
	// Identical exclusions apply to both branches. Any partially excluded true
	// event is removed as a whole rather than changing its event denominator.
	included := func(p matchPoint) bool {
		for _, x := range excluded {
			if p.Device == x.Device && p.Type == x.Type && p.At < x.End && p.End > x.At {
				return false
			}
		}
		return true
	}
	positive = slices.DeleteFunc(positive, func(p matchPoint) bool { return !included(p) })
	points := outcomePoints(cycles, policy.MatchTimeBasis)
	points = slices.DeleteFunc(points, func(p matchPoint) bool {
		for _, x := range excluded {
			if p.Device == x.Device && p.Type == x.Type && p.At >= x.At && p.At < x.End {
				return true
			}
		}
		for _, x := range append(slices.Clone(positive), negatives...) {
			if p.Device == x.Device && p.Type == x.Type && p.At >= x.At && p.At < x.End {
				return false
			}
		}
		return true
	})
	r.AlarmCycles = len(points)
	r.RealEvents = len(positive)
	if !policy.RepresentativeConfirmed || len(positive) == 0 || len(negatives) == 0 {
		r.Limitations = unique(r.Limitations, "REPRESENTATIVE_CONFIRMED_POSITIVE_AND_NEGATIVE_INTERVALS_REQUIRED")
	}
	if !computable {
		r.Limitations = unique(r.Limitations, "SOURCE_INITIAL_STATE_OR_BRANCH_CALCULATION_UNKNOWN")
	}
	if fixed.HoldoutPreviousUses > 0 {
		r.Limitations = unique(r.Limitations, "HOLDOUT_WINDOW_REUSED_NOT_INDEPENDENT_VALIDATION")
	}
	slices.SortFunc(positive, func(a, b matchPoint) int { return strings.Compare(a.ID, b.ID) })
	pairs, err := maximumMatch(points, positive, policy.ToleranceMs)
	if err != nil {
		r.Limitations = unique(r.Limitations, err.Error())
		return r
	}
	r.TP = len(pairs)
	r.ExtraCycles = max(0, r.AlarmCycles-r.TP)
	for i, p := range pairs {
		r.Matching = append(r.Matching, model.RuleLabEventMatch{ID: fmt.Sprintf("%s:label-match:%s:%d", run.ID, points[p.Left].ID, i), LabelID: positive[p.Right].ID, CandidateID: points[p.Left].ID, DifferenceMs: p.Difference, Kind: "COMMON"})
	}
	if computable && policy.RepresentativeConfirmed && len(positive) > 0 && len(negatives) > 0 {
		r.Status = "EVALUABLE"
		if r.AlarmCycles > 0 {
			v := float64(r.TP) / float64(r.AlarmCycles)
			r.Precision = &v
		}
		if r.RealEvents > 0 {
			v := float64(r.TP) / float64(r.RealEvents)
			r.Recall = &v
		}
	}
	r.Limitations = unique(r.Limitations, "RECALL_ONLY_FOR_FROZEN_COMMON_EVALUABLE_INTERVALS")
	return r
}
