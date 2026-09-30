package quality

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type calculation struct {
	result   Result
	findings map[string]int
	evidence map[string]bool
}

// Analyze processes one explicitly authorized device/attribute series. Source
// authorization, persistent snapshots and human reviews belong to its caller.
func Analyze(in Input) (Result, error) {
	if !in.Window.Start.Before(in.Window.End) {
		return Result{}, fmt.Errorf("analysis window must be nonempty")
	}
	profiles := append([]Profile(nil), in.Profiles...)
	for _, p := range profiles {
		if err := ValidateProfile(p); err != nil {
			return Result{}, fmt.Errorf("profile %s: %w", p.ID, err)
		}
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].EffectiveFrom.Before(profiles[j].EffectiveFrom) })
	for i := 1; i < len(profiles); i++ {
		if profiles[i-1].EffectiveTo.IsZero() || profiles[i-1].EffectiveTo.After(profiles[i].EffectiveFrom) {
			return Result{}, fmt.Errorf("quality profile effective intervals overlap")
		}
	}
	c := calculation{result: Result{AlgorithmVersion: AlgorithmVersion, DeviceID: in.DeviceID, AttributeID: in.AttributeID, Window: in.Window, State: coverageState(in.Coverage, in.Window), Metrics: []WindowMetric{}, Findings: []Finding{}, Evidence: []Evidence{}}, findings: map[string]int{}, evidence: map[string]bool{}}
	maxSlots := in.MaximumSlots
	if maxSlots <= 0 {
		maxSlots = 100000
	}
	remainingSlots := maxSlots
	cursor := in.Window.Start
	for _, p := range profiles {
		w := intersect(in.Window, Window{Start: p.EffectiveFrom, End: p.EffectiveTo})
		if !w.Start.Before(w.End) {
			continue
		}
		if cursor.Before(w.Start) {
			c.result.Unconfigured = append(c.result.Unconfigured, Window{Start: cursor, End: w.Start})
		}
		cursor = w.End
		m := WindowMetric{ID: identity(in.DeviceID, in.AttributeID, p.ID, p.Version, w.Start.Format(time.RFC3339Nano), w.End.Format(time.RFC3339Nano)), ProfileID: p.ID, ProfileVersion: p.Version, Window: w, State: coverageState(in.Coverage, w), Reasons: append([]string(nil), in.Coverage.Reasons...)}
		// Event history and real-time receipt history have independent windows.
		main := mainSamples(in.Samples, w)
		eventSamples := make([]Sample, 0, len(main))
		invalid, outOfRange, rangeUnknown := 0, 0, 0
		for _, s := range main {
			m.InspectedSamples++
			valid := validValue(p, s.Value)
			if valid {
				m.ValidSamples++
			} else if s.Value != nil || p.Required {
				invalid++
				c.addFinding(m, "invalid_format", "issue", "物模型类型、必填或有限数值校验未通过。", nil, nil, "", s)
			}
			if inWindow(s.EventAt, w) {
				eventSamples = append(eventSamples, s)
			}
			if !valid {
				continue
			}
			n, numericValue := numeric(s.Value)
			if !numericValue {
				continue
			}
			if !p.UnitConfirmed || !p.RangeConfirmed || s.Unit != p.Unit {
				rangeUnknown++
				continue
			}
			m.Range.Denominator++
			if n < *p.Minimum || n > *p.Maximum {
				outOfRange++
				kind, threshold := "range_above", p.Maximum
				if n < *p.Minimum {
					kind, threshold = "range_below", p.Minimum
				}
				c.addFinding(m, kind, "issue", "值超出已确认单位与量程。", threshold, &n, "", s)
			}
		}
		m.Format = ratio(invalid, m.InspectedSamples, m.State)
		m.Range = ratio(outOfRange, m.Range.Denominator, m.State)
		if valueType(p.ValueType) != "number" && valueType(p.ValueType) != "integer" {
			m.Range = unavailable(NotApplicable, "属性不是数值类型")
		} else if !p.UnitConfirmed || !p.RangeConfirmed {
			m.Range = unavailable(Unknown, "单位或量程未经确认")
		} else if rangeUnknown > 0 {
			m.Range.State = Partial
			m.Range.Reasons = []string{"存在单位不匹配或单位依据不足的样本"}
		}
		var err error
		slotState := coverageState(in.Coverage, Window{Start: w.Start.Add(-p.Tolerance), End: w.End.Add(p.Tolerance)})
		m.EventCompleteness, err = completeness(p, w, in.Samples, false, slotState, remainingSlots)
		if err != nil {
			return Result{}, err
		}
		remainingSlots -= len(m.EventCompleteness.Slots)
		m.ReceptionCompleteness, err = completeness(p, w, in.Samples, true, slotState, remainingSlots)
		if err != nil {
			return Result{}, err
		}
		remainingSlots -= len(m.ReceptionCompleteness.Slots)
		if m.EventCompleteness.Missing.Numerator > 0 && m.EventCompleteness.Missing.State == Assessed {
			c.addFinding(m, "missing_event_slots", "issue", "设备事件时间存在未覆盖周期槽；补报可改变历史覆盖，不能补足过去的接收连续性。", nil, m.EventCompleteness.Missing.Value, "")
		}
		if m.ReceptionCompleteness.Missing.Numerator > 0 && m.ReceptionCompleteness.Missing.State == Assessed {
			c.addFinding(m, "missing_reception_slots", "issue", "平台接收时间存在未覆盖周期槽。", nil, m.ReceptionCompleteness.Missing.Value, "")
		}
		if p.Mode == Periodic && slotState != Assessed && slotState != NotApplicable {
			m.EventCompleteness.Missing.Reasons = append(m.EventCompleteness.Missing.Reasons, "边界容忍 padding 的来源覆盖不足")
			m.ReceptionCompleteness.Missing.Reasons = append(m.ReceptionCompleteness.Missing.Reasons, "边界容忍 padding 的来源覆盖不足")
			if c.result.State == Assessed {
				c.result.State = Partial
			}
		}
		m.Time = c.timeQuality(p, m, main)
		m.Sequence = c.sequence(p, m, eventSamples, in.Baselines)
		c.result.Metrics = append(c.result.Metrics, m)
	}
	if cursor.Before(in.Window.End) {
		c.result.Unconfigured = append(c.result.Unconfigured, Window{Start: cursor, End: in.Window.End})
	}
	if len(c.result.Unconfigured) > 0 {
		c.result.State = Partial
		c.result.Limitations = append(c.result.Limitations, "部分区间没有生效的质量配置，不能评价为通过。")
	}
	if len(c.result.Metrics) == 0 {
		c.result.State = Unknown
	}
	c.result.Parse = parseMetrics(in)
	if c.result.State == Assessed && c.result.Parse.State != Assessed && c.result.Parse.State != NotApplicable {
		c.result.State = Partial
		c.result.Limitations = append(c.result.Limitations, "接入和解析来源覆盖不足；测量指标和解析指标分别展示。")
	}
	c.result.Limitations = append(c.result.Limitations, in.Coverage.Reasons...)
	return c.result, nil
}

func intersect(a, b Window) Window {
	w := a
	if b.Start.After(w.Start) {
		w.Start = b.Start
	}
	if !b.End.IsZero() && b.End.Before(w.End) {
		w.End = b.End
	}
	return w
}

func coverageState(c Coverage, w Window) State {
	switch c.State {
	case Assessed:
		if !c.Window.Start.After(w.Start) && !c.Window.End.Before(w.End) {
			return Assessed
		}
		return Partial
	case Partial:
		return Partial
	case NotApplicable:
		return NotApplicable
	default:
		return Unknown
	}
}

func ratio(n, d int, state State) Ratio {
	r := Ratio{State: state, Numerator: n, Denominator: d}
	if d == 0 {
		if state == Assessed {
			r.State = NotApplicable
		}
		return r
	}
	v := float64(n) / float64(d)
	if state == Assessed || state == Partial {
		r.Value = &v
	}
	return r
}

func unavailable(state State, reason string) Ratio {
	return Ratio{State: state, Reasons: []string{reason}}
}

func mainSamples(samples []Sample, w Window) []Sample {
	out := make([]Sample, 0, len(samples))
	for _, s := range samples {
		if inWindow(s.EventAt, w) || inWindow(s.ReceivedAt, w) {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return sampleLess(out[i], out[j], false) })
	return out
}

func sampleLess(a, b Sample, received bool) bool {
	at, bt := a.EventAt, b.EventAt
	if received {
		at, bt = a.ReceivedAt, b.ReceivedAt
	}
	if !at.Equal(bt) {
		return at.Before(bt)
	}
	if !a.ReceivedAt.Equal(b.ReceivedAt) {
		return a.ReceivedAt.Before(b.ReceivedAt)
	}
	if a.MessageID != b.MessageID {
		return a.MessageID < b.MessageID
	}
	if a.ID != b.ID {
		return a.ID < b.ID
	}
	return fmt.Sprint(a.Value) < fmt.Sprint(b.Value)
}

// slotAt performs floor division for dates before the anchor and uses only the
// fixed profile anchor. At an exact half-period tie, the earlier slot wins.
func slotAt(t, anchor time.Time, period time.Duration) (time.Time, error) {
	delta := t.Sub(anchor)
	// time.Sub saturates; refuse a shifted phase instead of silently continuing.
	if !anchor.Add(delta).Equal(t) {
		return time.Time{}, fmt.Errorf("sample or window is outside duration range from anchor")
	}
	rem := delta % period
	if rem < 0 {
		rem += period
	}
	center := t.Add(-rem)
	if rem > period-rem {
		center = center.Add(period)
	}
	return center, nil
}

func completeness(p Profile, w Window, samples []Sample, received bool, state State, maximum int) (Completeness, error) {
	if p.Mode == Event {
		return Completeness{Missing: unavailable(NotApplicable, "事件上报测点不计算周期缺报率；心跳须单独配置")}, nil
	}
	first, err := slotAt(w.Start, p.ScheduleAnchor, p.Period)
	if err != nil {
		return Completeness{}, err
	}
	if first.Before(w.Start) {
		first = first.Add(p.Period)
	}
	out := Completeness{Slots: []Slot{}}
	centers := map[time.Time]int{}
	for at := first; at.Before(w.End); at = at.Add(p.Period) {
		if len(out.Slots) >= maximum {
			return Completeness{}, fmt.Errorf("analysis exceeds maximum expected slots")
		}
		centers[at.UTC()] = len(out.Slots)
		out.Slots = append(out.Slots, Slot{At: at})
	}
	representatives := make([]Sample, len(out.Slots))
	distances := make([]time.Duration, len(out.Slots))
	for _, s := range samples {
		if !validValue(p, s.Value) {
			continue
		}
		at := s.EventAt
		if received {
			at = s.ReceivedAt
		}
		if at.IsZero() || at.Before(w.Start.Add(-p.Tolerance)) || !at.Before(w.End.Add(p.Tolerance)) {
			continue
		}
		center, err := slotAt(at, p.ScheduleAnchor, p.Period)
		if err != nil {
			return Completeness{}, err
		}
		i, exists := centers[center.UTC()]
		if !exists {
			continue
		}
		distance := at.Sub(center)
		if distance < 0 {
			distance = -distance
		}
		if distance > p.Tolerance {
			if inWindow(at, w) {
				out.OutOfSlot++
			}
			continue
		}
		slot := &out.Slots[i]
		slot.Samples++
		if slot.Covered {
			out.Duplicates++
		}
		if !slot.Covered || distance < distances[i] || (distance == distances[i] && representativeLess(s, representatives[i])) {
			slot.RepresentativeID, slot.RepresentativeMessageID = s.ID, s.MessageID
			representatives[i], distances[i] = s, distance
		}
		slot.Covered = true
	}
	out.Expected = len(out.Slots)
	for _, s := range out.Slots {
		if s.Covered {
			out.Covered++
		}
	}
	out.Missing = ratio(out.Expected-out.Covered, out.Expected, state)
	return out, nil
}

func representativeLess(a, b Sample) bool {
	if !a.ReceivedAt.Equal(b.ReceivedAt) {
		if a.ReceivedAt.IsZero() {
			return false
		}
		if b.ReceivedAt.IsZero() {
			return true
		}
		return a.ReceivedAt.Before(b.ReceivedAt)
	}
	if a.MessageID != b.MessageID {
		return a.MessageID < b.MessageID
	}
	return a.ID < b.ID
}

func identity(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:16])
}

func (c *calculation) addFinding(m WindowMetric, kind, level, explanation string, threshold, value *float64, baselineID string, samples ...Sample) {
	key := m.ID + ":" + kind + ":" + baselineID
	i, exists := c.findings[key]
	if !exists {
		i = len(c.result.Findings)
		c.findings[key] = i
		c.result.Findings = append(c.result.Findings, Finding{ID: identity(key), Kind: kind, MetricID: m.ID, Window: m.Window, Level: level, Threshold: copyNumber(threshold), Value: copyNumber(value), BaselineID: baselineID, Explanation: explanation, EvidenceIDs: []string{}})
	}
	f := &c.result.Findings[i]
	for _, s := range samples {
		if len(f.EvidenceIDs) >= 3 {
			break
		}
		id := identity(s.ID, s.MessageID, s.RawMessageID, s.EventAt.Format(time.RFC3339Nano), s.ReceivedAt.Format(time.RFC3339Nano))
		already := false
		for _, existing := range f.EvidenceIDs {
			if existing == id {
				already = true
				break
			}
		}
		if already {
			continue
		}
		f.EvidenceIDs = append(f.EvidenceIDs, id)
		if !c.evidence[id] {
			c.evidence[id] = true
			v := frozenValue(s.Value)
			c.result.Evidence = append(c.result.Evidence, Evidence{ID: id, SampleID: s.ID, MessageID: s.MessageID, RawMessageID: s.RawMessageID, EventAt: s.EventAt, ReceivedAt: s.ReceivedAt, AvailableAt: s.AvailableAt, ProtocolVersion: s.ProtocolVersion, ConfigurationVersion: s.ConfigurationVersion, Value: v})
		}
	}
}

func copyNumber(n *float64) *float64 {
	if n == nil {
		return nil
	}
	v := *n
	return &v
}

func frozenValue(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	var out any
	d := json.NewDecoder(strings.NewReader(string(b)))
	d.UseNumber()
	if err := d.Decode(&out); err != nil {
		return fmt.Sprint(v)
	}
	return out
}

func (c *calculation) timeQuality(p Profile, m WindowMetric, samples []Sample) TimeQuality {
	out := TimeQuality{ClockCalibrated: p.ClockCalibrated}
	if !p.ClockCalibrated {
		out.Limitations = []string{"设备时钟未确认校准，接收时间减设备时间不是精确单向网络延迟。"}
	} else {
		out.Limitations = []string{"接收时间减设备时间包含时钟及处理因素，不等于精确单向网络延迟。"}
	}
	arrival := append([]Sample(nil), samples...)
	sort.SliceStable(arrival, func(i, j int) bool { return sampleLess(arrival[i], arrival[j], true) })
	var previous, latest time.Time
	differences := []float64{}
	count, outOfOrder, rollback, future := 0, 0, 0, 0
	for _, s := range arrival {
		if s.EventAt.IsZero() || s.ReceivedAt.IsZero() {
			out.MissingClocks++
			continue
		}
		count++
		difference := s.ReceivedAt.Sub(s.EventAt).Seconds()
		if !s.EventAt.Add(s.ReceivedAt.Sub(s.EventAt)).Equal(s.ReceivedAt) {
			out.MissingClocks++
			count--
			continue
		}
		differences = append(differences, difference)
		if !latest.IsZero() && s.EventAt.Before(latest) {
			outOfOrder++
			c.addFinding(m, "event_out_of_order", "verification_required", "按平台接收顺序观察到事件时间早于已接收的最大事件时间。", nil, nil, "", s)
		}
		if !previous.IsZero() && s.EventAt.Before(previous) {
			rollback++
		}
		if latest.IsZero() || s.EventAt.After(latest) {
			latest = s.EventAt
		}
		previous = s.EventAt
		if p.FutureTolerance != nil && s.EventAt.Sub(s.ReceivedAt) > *p.FutureTolerance {
			future++
			v, threshold := -difference, p.FutureTolerance.Seconds()
			c.addFinding(m, "future_event_time", "verification_required", "设备声明时间超出已配置未来时间容忍，须核实设备时钟。", &threshold, &v, "", s)
		}
	}
	state := m.State
	if out.MissingClocks > 0 && state == Assessed {
		state = Partial
	}
	out.OutOfOrder, out.Rollback = ratio(outOfOrder, max(0, count-1), state), ratio(rollback, max(0, count-1), state)
	out.Future = ratio(future, count, state)
	if p.FutureTolerance == nil {
		out.Future = unavailable(Unknown, "未来时间容忍未配置")
	}
	out.Difference = statistics(differences)
	return out
}

func parseMetrics(in Input) ParseMetric {
	state := coverageState(in.ParseCoverage, in.Window)
	out := ParseMetric{State: state, Reasons: append([]string(nil), in.ParseCoverage.Reasons...)}
	seenRaw, seenAttempt := map[string]bool{}, map[string]bool{}
	attempts, failedAttempts, missingAttemptRecords := 0, 0, 0
	for _, p := range in.ParseOutcomes {
		if !inWindow(p.ReceivedAt, in.Window) {
			continue
		}
		if p.RawMessageID == "" {
			out.State = Partial
			out.Reasons = append(out.Reasons, "解析来源缺少不可变原文标识")
			continue
		}
		if seenRaw[p.RawMessageID] {
			continue
		}
		seenRaw[p.RawMessageID] = true
		out.ObservedRaw++
		if p.Archived {
			out.Archived++
		}
		if p.Attempted {
			out.Attempted++
		} else {
			out.NotAttempted++
		}
		switch p.LastStatus {
		case ParseFailed:
			if p.Attempted {
				out.LastFailed++
			} else {
				out.UnknownLast++
			}
		case ParseSucceeded:
			if p.Attempted {
				out.LastSucceeded++
			} else {
				out.UnknownLast++
			}
		case ParseNotAttempted:
			if p.Attempted {
				out.UnknownLast++
			}
		default:
			out.UnknownLast++
		}
		if p.SuccessfulStandardMessages > 0 {
			out.SuccessfulStandardMessages += p.SuccessfulStandardMessages
		}
		validAttempts := 0
		for _, attempt := range p.Attempts {
			if attempt.ID == "" || (attempt.Status != ParseFailed && attempt.Status != ParseSucceeded) {
				out.State = Partial
				out.Reasons = append(out.Reasons, "不可变解析尝试记录不完整")
				continue
			}
			key := p.RawMessageID + ":" + attempt.ID
			if seenAttempt[key] {
				continue
			}
			seenAttempt[key] = true
			validAttempts++
			attempts++
			if attempt.Status == ParseFailed {
				failedAttempts++
			}
		}
		if p.Attempted && validAttempts == 0 {
			missingAttemptRecords++
		}
	}
	out.LastFailure = ratio(out.LastFailed, out.Attempted, out.State)
	if out.UnknownLast > 0 {
		out.LastFailure.State = Partial
		out.LastFailure.Reasons = []string{"部分原文没有可信的最后解析阶段状态"}
	}
	out.AttemptFailure = ratio(failedAttempts, attempts, out.State)
	if !in.AttemptsComplete || missingAttemptRecords > 0 {
		if in.AttemptsComplete && attempts > 0 {
			out.AttemptFailure.State = Partial
		} else {
			out.AttemptFailure.State = Unknown
			out.AttemptFailure.Value = nil
		}
		out.AttemptFailure.Reasons = []string{"不可变的完整解析尝试记录缺失，不能由最后结果反推每次尝试失败率"}
	}
	return out
}

// Keep malformed numeric input encodable without converting it into a valid
// standard measurement. JSON is also used only for deterministic scalar equality.
func sameValue(a, b any) bool {
	aa, aerr := json.Marshal(a)
	bb, berr := json.Marshal(b)
	return aerr == nil && berr == nil && string(aa) == string(bb)
}

func absFinite(a, b float64) (float64, bool) {
	v := math.Abs(a - b)
	return v, finite(v)
}
