package quality

import (
	"math"
	"sort"
	"time"
)

func sameRevision(a, b Sample) bool {
	return a.Unit == b.Unit && a.ProtocolVersion == b.ProtocolVersion && a.ConfigurationVersion == b.ConfigurationVersion && a.OperatingCondition == b.OperatingCondition
}

func sequenceGap(p Profile) time.Duration {
	if p.MaxSequenceGap > 0 {
		return p.MaxSequenceGap
	}
	if p.Mode == Periodic {
		// Consecutive expected slots can each vary by tolerance. The profile's
		// configured cadence supplies the gap policy, not a universal duration.
		if p.Period <= time.Duration(math.MaxInt64)-2*p.Tolerance {
			return p.Period + 2*p.Tolerance
		}
	}
	return 0
}

func (c *calculation) sequence(p Profile, m WindowMetric, samples []Sample, baselines []Baseline) SequenceMetric {
	out := SequenceMetric{Segments: []Segment{}}
	ordered := append([]Sample(nil), samples...)
	sort.SliceStable(ordered, func(i, j int) bool { return sampleLess(ordered[i], ordered[j], false) })
	groups := [][]Sample{}
	current := []Sample{}
	gap := sequenceGap(p)
	flush := func() {
		if len(current) > 0 {
			groups = append(groups, current)
			current = nil
		}
	}
	for _, s := range ordered {
		if !validValue(p, s.Value) {
			flush()
			continue
		}
		if len(current) > 0 {
			previous := current[len(current)-1]
			dt := s.EventAt.Sub(previous.EventAt)
			switch {
			case !sameRevision(previous, s):
				out.VersionBreaks++
				flush()
			case dt <= 0:
				out.SameTime++
				flush()
			case gap == 0 || dt > gap:
				out.LongGaps++
				flush()
			}
		}
		current = append(current, s)
	}
	flush()
	stableCount, stableEligible, rateCount, rateEligible := 0, 0, 0, 0
	deviationCount, deviationEligible, deviationUnknown := 0, 0, 0
	driftCount, driftEligible, driftUnknown := 0, 0, 0
	arithmeticUnknown := false
	allValues := []float64{}
	homogeneous := true
	var first Sample
	hasFirst := false
	for _, group := range groups {
		s := group[0]
		if !hasFirst {
			first, hasFirst = s, true
		} else if !sameRevision(first, s) {
			homogeneous = false
		}
		values := []float64{}
		for _, sample := range group {
			if n, ok := numeric(sample.Value); ok {
				values = append(values, n)
			}
		}
		segment := Segment{Window: Window{Start: s.EventAt, End: group[len(group)-1].EventAt.Add(time.Nanosecond)}, Unit: s.Unit, ProtocolVersion: s.ProtocolVersion, ConfigurationVersion: s.ConfigurationVersion, OperatingCondition: s.OperatingCondition, SampleCount: len(group), Statistics: statistics(values)}
		out.Segments = append(out.Segments, segment)
		allValues = append(allValues, values...)
		if p.StableDuration > 0 && group[len(group)-1].EventAt.Sub(group[0].EventAt) >= p.StableDuration && stableSupported(p) {
			stableEligible++
			if start, end, ok := stableSpan(p, group); ok {
				stableCount++
				explanation := "连续有效样本跨越允许稳定时长且变化小于配置精度；正常恒定工况或量化精度可构成人工解释。"
				c.addFinding(m, "long_stability", "verification_required", explanation, p.Epsilon, nil, "", start, end)
			}
		}
		plus, minus, previousBaseline := 0.0, 0.0, ""
		for i, sample := range group {
			n, numericValue := numeric(sample.Value)
			if !numericValue {
				continue
			}
			if p.MaxRate != nil && i > 0 {
				previous := group[i-1]
				prev, ok := numeric(previous.Value)
				if ok {
					difference, valid := absFinite(n, prev)
					rate := difference / sample.EventAt.Sub(previous.EventAt).Seconds()
					if !valid || !finite(rate) {
						arithmeticUnknown = true
					} else {
						rateEligible++
						if rate > *p.MaxRate {
							rateCount++
							c.addFinding(m, "rate_change", "verification_required", "相邻同版本有效测量的变化率超过配置阈值，须核实倍率和工况。", p.MaxRate, &rate, "", previous, sample)
						}
					}
				}
			}
			baseline, found := compatibleBaseline(p, sample, baselines)
			if p.DeviationMethod != "" {
				if found {
					deviates, threshold, evaluated := deviation(p, baseline, n)
					if evaluated {
						deviationEligible++
						if deviates {
							deviationCount++
							c.addFinding(m, "historical_deviation", "verification_required", "测量偏离同单位、工况、协议和配置版本的已确认基线；该线索不证明物理故障。", threshold, &n, baseline.ID, sample)
						}
					} else {
						deviationUnknown++
					}
				} else {
					deviationUnknown++
				}
			}
			if p.CUSUM == nil {
				continue
			}
			if !found || baseline.CUSUMVersion != p.CUSUM.Version {
				driftUnknown++
				plus, minus, previousBaseline = 0, 0, ""
				continue
			}
			if previousBaseline != baseline.ID {
				plus, minus, previousBaseline = 0, 0, baseline.ID
			}
			nextPlus := math.Max(0, plus+n-baseline.Median-p.CUSUM.Allowance)
			nextMinus := math.Max(0, minus+baseline.Median-n-p.CUSUM.Allowance)
			if !finite(nextPlus) || !finite(nextMinus) {
				driftUnknown++
				plus, minus = 0, 0
				continue
			}
			plus, minus = nextPlus, nextMinus
			driftEligible++
			if plus > p.CUSUM.Threshold || minus > p.CUSUM.Threshold {
				driftCount++
				v := math.Max(plus, minus)
				c.addFinding(m, "cusum_drift", "verification_required", "已确认基线的双侧 CUSUM 越过绑定参数版本的门槛，只提供变化线索。", &p.CUSUM.Threshold, &v, baseline.ID, group[0], sample)
				// A subsequent excursion is a new crossing, not repeated reporting
				// of every sample while the same sum stays beyond its threshold.
				plus, minus = 0, 0
			}
		}
	}
	if homogeneous {
		out.Statistics = statistics(allValues)
	}
	out.Stable = ratio(stableCount, stableEligible, m.State)
	if p.StableDuration == 0 || !stableSupported(p) {
		out.Stable = unavailable(Unknown, "稳定时长或数值精度未配置，或类型不适用")
	} else if gap == 0 {
		out.Stable = unavailable(Unknown, "事件测点未配置连续样本最大间隔")
	}
	out.Rate = ratio(rateCount, rateEligible, m.State)
	if p.MaxRate == nil {
		out.Rate = unavailable(Unknown, "变化率阈值未配置")
	} else if valueType(p.ValueType) != "number" && valueType(p.ValueType) != "integer" {
		out.Rate = unavailable(NotApplicable, "变化率不适用于非数值类型")
	} else if gap == 0 {
		out.Rate = unavailable(Unknown, "事件测点未配置相邻比较的最大间隔")
	} else if arithmeticUnknown {
		out.Rate.State = Partial
		out.Rate.Reasons = []string{"部分相邻数值差超出有限算术范围"}
	}
	out.Deviation = evaluatedRatio(deviationCount, deviationEligible, deviationUnknown, m.State, "缺少同版本、同工况、已确认且不少于统计下限的有效基线，或 MAD 为零且没有精度/绝对偏差门槛")
	if p.DeviationMethod == "" {
		out.Deviation = unavailable(Unknown, "历史偏离方法未配置")
	} else if valueType(p.ValueType) != "number" && valueType(p.ValueType) != "integer" {
		out.Deviation = unavailable(NotApplicable, "统计偏离不适用于非数值类型")
	}
	out.Drift = evaluatedRatio(driftCount, driftEligible, driftUnknown, m.State, "缺少已确认且参数版本匹配的足量基线")
	if p.CUSUM == nil {
		out.Drift = unavailable(Unknown, "CUSUM 参数未配置")
	} else if valueType(p.ValueType) != "number" && valueType(p.ValueType) != "integer" {
		out.Drift = unavailable(NotApplicable, "CUSUM 不适用于非数值类型")
	} else if gap == 0 {
		out.Drift = unavailable(Unknown, "事件测点未配置连续样本最大间隔")
	}
	return out
}

func stableSupported(p Profile) bool {
	switch valueType(p.ValueType) {
	case "number", "integer":
		return p.Epsilon != nil
	case "boolean", "string":
		return true
	default:
		return false
	}
}

// stableSpan uses monotonic min/max queues and a sliding left boundary. It
// finds stable subsequences too, rather than requiring the entire query window
// to be flat. Invalid samples and gaps have already split the input groups.
func stableSpan(p Profile, samples []Sample) (Sample, Sample, bool) {
	left := 0
	minQ, maxQ := []int{}, []int{}
	for right, s := range samples {
		if n, ok := numeric(s.Value); ok {
			for len(minQ) > 0 {
				v, _ := numeric(samples[minQ[len(minQ)-1]].Value)
				if v <= n {
					break
				}
				minQ = minQ[:len(minQ)-1]
			}
			for len(maxQ) > 0 {
				v, _ := numeric(samples[maxQ[len(maxQ)-1]].Value)
				if v >= n {
					break
				}
				maxQ = maxQ[:len(maxQ)-1]
			}
			minQ, maxQ = append(minQ, right), append(maxQ, right)
			for left <= right {
				lo, _ := numeric(samples[minQ[0]].Value)
				hi, _ := numeric(samples[maxQ[0]].Value)
				difference, valid := absFinite(hi, lo)
				if valid && difference < *p.Epsilon {
					break
				}
				left++
				if minQ[0] < left {
					minQ = minQ[1:]
				}
				if maxQ[0] < left {
					maxQ = maxQ[1:]
				}
			}
		} else if right > 0 && !sameValue(samples[right-1].Value, s.Value) {
			left = right
		}
		if samples[right].EventAt.Sub(samples[left].EventAt) >= p.StableDuration {
			return samples[left], samples[right], true
		}
	}
	return Sample{}, Sample{}, false
}

func compatibleBaseline(p Profile, s Sample, baselines []Baseline) (Baseline, bool) {
	if !p.UnitConfirmed || s.Unit != p.Unit || s.ProtocolVersion == "" || s.ConfigurationVersion == "" || s.OperatingCondition == "" {
		return Baseline{}, false
	}
	var chosen Baseline
	found := false
	for _, b := range baselines {
		if b.ID == "" || b.ConfirmedBy == "" || b.ConfirmedAt.IsZero() || b.SampleCount < minimumSamples(p) || b.ProfileVersion != p.Version || b.Unit != s.Unit || b.ProtocolVersion != s.ProtocolVersion || b.ConfigurationVersion != s.ConfigurationVersion || b.OperatingCondition != s.OperatingCondition || !finite(b.Median) || !finite(b.MAD) || b.MAD < 0 {
			continue
		}
		if b.ValidFrom.IsZero() || b.ValidUntil.IsZero() || !b.ValidFrom.Before(b.ValidUntil) || s.EventAt.Before(b.ValidFrom) || !s.EventAt.Before(b.ValidUntil) || !b.Window.Start.Before(b.Window.End) || b.Window.End.After(s.EventAt) {
			continue
		}
		if !found || b.ValidFrom.After(chosen.ValidFrom) || (b.ValidFrom.Equal(chosen.ValidFrom) && (b.ConfirmedAt.After(chosen.ConfirmedAt) || (b.ConfirmedAt.Equal(chosen.ConfirmedAt) && b.ID < chosen.ID))) {
			chosen, found = b, true
		}
	}
	return chosen, found
}

func deviation(p Profile, b Baseline, n float64) (bool, *float64, bool) {
	if p.DeviationMethod == "quantile" {
		if b.QuantileLower != p.QuantileLower || b.QuantileUpper != p.QuantileUpper || !finite(b.LowerValue) || !finite(b.UpperValue) || b.LowerValue > b.UpperValue {
			return false, nil, false
		}
		threshold := b.UpperValue
		if n < b.LowerValue {
			threshold = b.LowerValue
		}
		return n < b.LowerValue || n > b.UpperValue, &threshold, true
	}
	var threshold float64
	if b.MAD > 0 {
		if p.MADMultiplier == nil {
			return false, nil, false
		}
		threshold = b.MAD * *p.MADMultiplier
	} else if p.AbsoluteDeviation != nil {
		threshold = *p.AbsoluteDeviation
	} else if p.Epsilon != nil {
		threshold = *p.Epsilon
	} else {
		return false, nil, false
	}
	difference, valid := absFinite(n, b.Median)
	if !valid || !finite(threshold) {
		return false, nil, false
	}
	return difference > threshold, &threshold, true
}

func evaluatedRatio(n, d, missing int, state State, reason string) Ratio {
	r := ratio(n, d, state)
	if missing > 0 {
		if d > 0 {
			r.State = Partial
		} else {
			r.State = Unknown
		}
		r.Reasons = []string{reason}
	}
	return r
}
