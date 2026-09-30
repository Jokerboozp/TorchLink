package lab

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"iot-platform/internal/analytics"
	"iot-platform/internal/model"
	"iot-platform/internal/rulelab/eval"
)

type branchMetrics struct {
	Branch                 string   `json:"branch"`
	AttemptCount           int      `json:"attemptCount"`
	InputCount             int      `json:"inputCount"`
	WarmupInputCount       int      `json:"warmupInputCount"`
	MainInputCount         int      `json:"mainInputCount"`
	MatchedMessages        int      `json:"matchedMessages"`
	NewCycles              int      `json:"newCycles"`
	RepeatReports          int      `json:"repeatReports"`
	UnrecoveredCycles      int      `json:"unrecoveredCycles"`
	RetriggerAfterRecovery int      `json:"retriggerAfterRecovery"`
	IntentActions          int      `json:"intentActions"`
	DeviceAssertions       int      `json:"deviceAssertions"`
	RuleCycles             int      `json:"ruleCycles"`
	DirectCycles           int      `json:"directCycles"`
	ComponentCycles        int      `json:"componentCycles"`
	EvaluationErrors       int      `json:"evaluationErrors"`
	UnknownInitialCycles   int      `json:"unknownInitialCycles"`
	UncomputedMessages     int      `json:"uncomputedMessages"`
	RecoveredCycles        int      `json:"recoveredCycles"`
	RecoveredDurationMs    int64    `json:"recoveredDurationMs"`
	DurationUnknownCycles  int      `json:"durationUnknownCycles"`
	Limitations            []string `json:"limitations"`
}
type branchState struct {
	Name        string                               `json:"name"`
	Pending     map[string]eval.Pending              `json:"pending"`
	Alarms      map[string]model.Alarm               `json:"alarms"`
	Watermarks  map[string]model.ComponentAlarmState `json:"watermarks"`
	Initialized map[string]bool                      `json:"initialized"`
	Seen        map[string]bool                      `json:"seen"`
	Counted     map[string]bool                      `json:"counted"`
	Assertions  map[string]bool                      `json:"assertions"`
	Outcomes    map[string]model.RuleLabOutcome      `json:"outcomes"`
	Relevant    map[string]bool                      `json:"relevant"`
	OutcomeIDs  map[string]string                    `json:"outcomeIds"`
	Revisions   map[string]model.AlarmRuleRevision   `json:"revisions"`
	Metrics     branchMetrics                        `json:"metrics"`
}
type workItem struct {
	InputIndex int    `json:"inputIndex"`
	TraceIndex int    `json:"traceIndex"`
	At         int64  `json:"at"`
	ID         string `json:"id"`
}
type comparisonCheckpoint struct {
	Index           int                     `json:"index"`
	Branches        map[string]*branchState `json:"branches"`
	Limits          []string                `json:"limits"`
	DocumentsFrozen bool                    `json:"documentsFrozen"`
}

func newBranch(name string) *branchState {
	return &branchState{Name: name, Pending: map[string]eval.Pending{}, Alarms: map[string]model.Alarm{}, Watermarks: map[string]model.ComponentAlarmState{}, Initialized: map[string]bool{}, Seen: map[string]bool{}, Counted: map[string]bool{}, Assertions: map[string]bool{}, Outcomes: map[string]model.RuleLabOutcome{}, Relevant: map[string]bool{}, OutcomeIDs: map[string]string{}, Revisions: map[string]model.AlarmRuleRevision{}, Metrics: branchMetrics{Branch: name, Limitations: []string{}}}
}
func work(m datasetChunk) []workItem {
	result := []workItem{}
	for i, v := range m.Inputs {
		at := v.Message.Timestamp
		if m.Selection.TimeBasis == "RECEIVED" {
			at = v.ReceivedAt
		}
		if m.Selection.ClockPolicy == "RECORDED_TRACE" && len(v.Traces) > 0 {
			for j, t := range v.Traces {
				result = append(result, workItem{i, j, t.StartedAt, t.ID})
			}
		} else {
			result = append(result, workItem{i, -1, at, v.ID})
		}
	}
	slices.SortFunc(result, func(a, b workItem) int {
		if a.At < b.At {
			return -1
		}
		if a.At > b.At {
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	return result
}
func alarmKey(device, rule string) string {
	body, _ := json.Marshal([]string{device, rule})
	return string(body)
}
func cloneAlarm(v model.Alarm) model.Alarm { v.Details = nil; v.Cameras = nil; return v }
func openAlarm(v model.Alarm) bool         { return v.Status == "ACTIVE" || v.Status == "ACKED" }
func candidateRevision(b model.RuleLabExperiment, f frozenExperiment) model.AlarmRuleRevision {
	rule := b.Candidate
	rule.Enabled = b.CandidateEnabled
	id := f.Experiment.ID + ":candidate"
	return model.AlarmRuleRevision{ID: id, TenantID: f.Experiment.TenantID, RuleID: rule.ID, Rule: rule, Version: rule.Version, Hash: model.RuleBodyHash(rule), SemanticsVersion: eval.RevisionV2}
}
func stageTimes(m datasetChunk, v model.RuleLabInput, trace *model.RuleEvaluationTrace, ruleID string) (eval.StageTimes, *model.RuleEvaluationStep) {
	if m.Selection.ClockPolicy == "RECORDED_TRACE" {
		if trace != nil {
			for i := range trace.Steps {
				for _, rule := range trace.Rules {
					if rule.ID == trace.Steps[i].RuleRevisionID && rule.RuleID == ruleID {
						return trace.Steps[i].Times, &trace.Steps[i]
					}
				}
			}
		}
		return eval.StageTimes{}, nil
	}
	at := v.Message.Timestamp
	if m.Selection.ClockPolicy == "RECEIVED_AS_PROCESSING" {
		at = v.ReceivedAt
	}
	if at <= 0 {
		return eval.StageTimes{}, nil
	}
	seconds := at / 1000
	return eval.StageTimes{DurationAtSeconds: &seconds, RaiseAtMillis: &at, RecoverAtMillis: &at, ComponentAtMillis: &at}, nil
}
func routeTimes(m datasetChunk, v model.RuleLabInput, trace *model.RuleEvaluationTrace, ruleID, kind string) eval.StageTimes {
	if m.Selection.ClockPolicy != "RECORDED_TRACE" {
		times, _ := stageTimes(m, v, nil, ruleID)
		return times
	}
	if trace != nil {
		for _, step := range trace.RoutingSteps {
			if step.RuleID == ruleID && step.Kind == kind {
				return step.Times
			}
		}
	}
	return eval.StageTimes{}
}
func routingStep(trace *model.RuleEvaluationTrace, rule, kind string) *model.RuleRoutingStep {
	if trace == nil {
		return nil
	}
	for i := range trace.RoutingSteps {
		v := &trace.RoutingSteps[i]
		if v.RuleID == rule && v.Kind == kind {
			return v
		}
	}
	return nil
}
func sameLogicalAlarm(a, b model.Alarm) bool {
	a = cloneAlarm(a)
	b = cloneAlarm(b)
	a.Version = 0
	b.Version = 0
	return reflect.DeepEqual(a, b)
}
func mainInput(m datasetChunk, v model.RuleLabInput) bool {
	at := v.Message.Timestamp
	if m.Selection.TimeBasis == "RECEIVED" {
		at = v.ReceivedAt
	}
	return at >= m.Selection.Start && at < m.Selection.End
}
func (s *branchState) initializeRule(v model.RuleLabInput, revision model.AlarmRuleRevision, step *model.RuleEvaluationStep, m datasetChunk, b model.RuleLabExperiment) {
	key := alarmKey(v.Message.DeviceID, revision.RuleID)
	if s.Initialized[key] {
		return
	}
	s.Initialized[key] = true
	if m.Selection.InitialStatePolicy != "TRACE_INITIAL" || step == nil || step.Before.InitialStateQuality != "KNOWN" {
		s.Metrics.Limitations = unique(s.Metrics.Limitations, "RULE_INITIAL_STATE_UNKNOWN")
		return
	}
	s.Alarms[key] = cloneAlarm(step.Before.Alarm)
	if step.Before.RecoveryRevision != nil {
		s.Revisions[step.Before.RecoveryRevision.ID] = *step.Before.RecoveryRevision
	}
	if s.Name == "CANDIDATE" && revision.RuleID == b.CandidateRuleID {
		s.Metrics.Limitations = unique(s.Metrics.Limitations, "CANDIDATE_PENDING_RESTARTED_AT_WARMUP")
		return
	}
	s.Pending[alarmKey(key, revision.ID)] = step.Before.Pending
}
func (s *branchState) record(run model.AnalysisRun, v model.RuleLabInput, category string, old, next model.Alarm, created bool, event string, actions []eval.ActionIntent, inMain bool, initial string) {
	key := alarmKey(v.Message.DeviceID, next.RuleID)
	s.Alarms[key] = cloneAlarm(next)
	id := s.OutcomeIDs[next.ID]
	if id == "" {
		hash, _ := analytics.AnalysisHash([]string{s.Name, key, next.ID})
		id = run.ID + ":outcome:" + hash
		s.OutcomeIDs[next.ID] = id
		s.Outcomes[id] = model.RuleLabOutcome{ID: id, Branch: s.Name, Category: category, DeviceID: next.DeviceID, RuleID: next.RuleID, AlarmType: next.AlarmType, CreatedRuleRevision: next.CreatedRuleRevision, TriggerRuleRevision: next.TriggerRuleRevision, TriggerMessageID: v.ID, RawMessageID: v.Message.RawMessageID, TriggeredAt: next.FirstTriggeredAt, TriggerEventAt: v.Message.Timestamp, LastTriggeredAt: next.LastTriggeredAt, TriggerCount: int64(next.TriggerCount), Status: next.Status, InitialStateQuality: initial, Uncertainties: []string{}, EvidenceIDs: []string{run.ID + ":evidence:" + v.Hash}}
		if !created {
			s.Metrics.Limitations = unique(s.Metrics.Limitations, "PREEXISTING_LIFECYCLE_RETAINED")
			out := s.Outcomes[id]
			out.Uncertainties = unique(out.Uncertainties, "TRIGGER_EVENT_BEFORE_DATASET_UNKNOWN")
			out.TriggerEventAt = 0
			s.Outcomes[id] = out
		}
	}
	out := s.Outcomes[id]
	out.LastTriggeredAt = next.LastTriggeredAt
	out.TriggerCount = int64(next.TriggerCount)
	out.Status = next.Status
	out.TriggerRuleRevision = next.TriggerRuleRevision
	if event == "recovered" {
		out.RecoveredAt = next.RecoveredAt
		out.RecoveredEventAt = v.Message.Timestamp
		if next.RecoveredAt >= next.FirstTriggeredAt && next.FirstTriggeredAt > 0 {
			duration := next.RecoveredAt - next.FirstTriggeredAt
			out.DurationMs = &duration
		}
	}
	if initial != "KNOWN" {
		out.Uncertainties = unique(out.Uncertainties, "UNKNOWN_INITIAL_STATE")
	}
	for _, action := range actions {
		out.IntentActionCount++
		out.IntentActionTypes = append(out.IntentActionTypes, action.Action.Type)
	}
	if len(out.EvidenceIDs) < 20 {
		out.EvidenceIDs = unique(out.EvidenceIDs, run.ID+":evidence:"+v.Hash)
	}
	s.Outcomes[id] = out
	if s.Relevant == nil {
		s.Relevant = map[string]bool{}
	}
	if inMain {
		s.Relevant[id] = true
	}
	if !inMain {
		return
	}
	s.Metrics.IntentActions += len(actions)
	if created {
		s.Metrics.NewCycles++
		switch category {
		case "RULE":
			s.Metrics.RuleCycles++
		case "DIRECT":
			s.Metrics.DirectCycles++
		case "COMPONENT":
			s.Metrics.ComponentCycles++
		}
		if old.Status == "RECOVERED" || old.Status == "CLOSED" {
			s.Metrics.RetriggerAfterRecovery++
		}
		if initial != "KNOWN" {
			s.Metrics.UnknownInitialCycles++
		}
	} else if next.TriggerCount > old.TriggerCount {
		s.Metrics.RepeatReports++
	}
}
func (s *branchState) apply(run model.AnalysisRun, m datasetChunk, f frozenExperiment, b model.RuleLabExperiment, item workItem) error {
	v := m.Inputs[item.InputIndex]
	msg := v.Message
	inMain := mainInput(m, v)
	var trace *model.RuleEvaluationTrace
	if item.TraceIndex >= 0 {
		trace = &v.Traces[item.TraceIndex]
	}
	if s.Seen[v.ID] {
		return nil
	}
	s.Metrics.AttemptCount++
	if s.Counted == nil {
		s.Counted = map[string]bool{}
	}
	if !s.Counted[v.ID] {
		s.Counted[v.ID] = true
		s.Metrics.InputCount++
		if inMain {
			s.Metrics.MainInputCount++
		} else {
			s.Metrics.WarmupInputCount++
		}
	}
	if msg.MessageType == model.AlarmReport && !s.Assertions[v.ID] {
		s.Assertions[v.ID] = true
		if inMain {
			s.Metrics.DeviceAssertions++
		}
	}
	rules := f.Baselines
	if b.BaselinePolicy == "RECORDED_ACTIVATIONS" && trace != nil {
		rules = trace.Rules
	}
	handled, matched := false, false
	for _, source := range rules {
		revision := source
		if s.Name == "CANDIDATE" && revision.RuleID == b.CandidateRuleID {
			revision = candidateRevision(b, f)
		}
		s.Revisions[source.ID] = source
		s.Revisions[revision.ID] = revision
		times, actual := stageTimes(m, v, trace, source.RuleID)
		s.initializeRule(v, revision, actual, m, b)
		key := alarmKey(msg.DeviceID, revision.RuleID)
		pendingKey := alarmKey(key, revision.ID)
		old := s.Alarms[key]
		newHash, _ := analytics.AnalysisHash([]any{s.Name, v.ID, revision.RuleID, len(s.Outcomes), item.ID})
		newID := run.ID + ":logical-alarm:" + newHash
		if s.Name == "BASELINE" && b.BaselinePolicy == "RECORDED_ACTIVATIONS" && actual != nil {
			// The complete trace is the authoritative state at this observed
			// production step, including mapped manual or concurrent mutations.
			old = cloneAlarm(actual.Before.Alarm)
			s.Pending[pendingKey] = actual.Before.Pending
			if actual.Before.RecoveryRevision != nil {
				s.Revisions[actual.Before.RecoveryRevision.ID] = *actual.Before.RecoveryRevision
			}
			if actual.Created {
				newID = actual.Alarm.ID
			}
		}
		state := eval.RuleState{Pending: s.Pending[pendingKey], Alarm: old, NewAlarmID: newID}
		var decision eval.Decision
		var err error
		if m.Selection.SemanticsVersion == eval.RevisionV2 {
			var recovery *model.AlarmRuleRevision
			if r, ok := s.Revisions[old.CreatedRuleRevision]; ok {
				recovery = &r
			}
			decision, err = eval.TransitionRevision(revision, msg, state, recovery, times)
		} else {
			decision, err = eval.Transition(revision.Rule, msg, state, times)
		}
		if err != nil {
			s.Metrics.EvaluationErrors++
			s.Metrics.UncomputedMessages++
			s.Metrics.Limitations = unique(s.Metrics.Limitations, "RULE_STAGE_CLOCK_OR_STATE_UNAVAILABLE")
			continue
		}
		if decision.EvaluationError != "" {
			s.Metrics.EvaluationErrors++
			s.Metrics.Limitations = unique(s.Metrics.Limitations, "EXPRESSION_EVALUATION_ERROR")
		}
		if s.Name == "BASELINE" && b.BaselinePolicy == "RECORDED_ACTIVATIONS" && actual != nil {
			if decision.Matched != actual.Matched || decision.RuleAlarmHandled != actual.RuleAlarmHandled || decision.WriteAlarm != actual.WriteAlarm || decision.Created != actual.Created || decision.Pending != actual.Pending || decision.WriteAlarm && !sameLogicalAlarm(decision.Alarm, actual.Alarm) {
				s.Metrics.Limitations = unique(s.Metrics.Limitations, "TRACE_COMMIT_DIFFERS_FROM_PURE_EVALUATION")
			}
			decision.Pending = actual.Pending
			decision.Matched = actual.Matched
			decision.RuleAlarmHandled = actual.RuleAlarmHandled
			decision.WriteAlarm = actual.WriteAlarm
			decision.Created = actual.Created
			decision.Event = actual.Event
			decision.Alarm = actual.Alarm
			decision.Actions = actual.Actions
		}
		s.Pending[pendingKey] = decision.Pending
		matched = matched || decision.Matched
		handled = handled || decision.RuleAlarmHandled
		initial := "UNKNOWN"
		if m.InitialStateQuality == "KNOWN_TRACE_INITIAL" && actual != nil && actual.Before.InitialStateQuality == "KNOWN" {
			initial = "KNOWN"
		}
		if decision.WriteAlarm {
			s.record(run, v, "RULE", old, decision.Alarm, decision.Created, decision.Event, decision.Actions, inMain, initial)
		}
		if len(s.Pending)+len(s.Alarms)+len(s.Outcomes) > MaxRecords {
			return invalid("实验分支状态数量超出保护上限")
		}
	}
	if matched && inMain {
		s.Metrics.MatchedMessages++
	}
	route, err := eval.Route(msg, handled)
	if err != nil {
		s.Metrics.EvaluationErrors++
		s.Metrics.UncomputedMessages++
		s.Metrics.Limitations = unique(s.Metrics.Limitations, "INVALID_COMPONENT_ASSERTION")
		return nil
	}
	initial := "UNKNOWN"
	if m.InitialStateQuality == "KNOWN_TRACE_INITIAL" && trace != nil {
		initial = "KNOWN"
	}
	if trace != nil && m.Selection.InitialStatePolicy == "TRACE_INITIAL" {
		for rule, value := range trace.Routing.Initial.Direct {
			key := alarmKey(msg.DeviceID, rule)
			if !s.Initialized[key] {
				s.Initialized[key] = true
				s.Alarms[key] = cloneAlarm(value.Alarm)
			}
		}
		for rule, value := range trace.Routing.Initial.Components {
			key := alarmKey(msg.DeviceID, rule)
			if !s.Initialized[key] {
				s.Initialized[key] = true
				s.Alarms[key] = cloneAlarm(value.Lifecycle.Alarm)
				s.Watermarks[key] = value.Watermark
			}
		}
	}
	for _, component := range route.Components {
		times := routeTimes(m, v, trace, component.RuleID, "COMPONENT")
		if times.ComponentAtMillis == nil {
			s.Metrics.UncomputedMessages++
			s.Metrics.Limitations = unique(s.Metrics.Limitations, "COMPONENT_STAGE_CLOCK_MISSING")
			continue
		}
		key := alarmKey(msg.DeviceID, component.RuleID)
		old := s.Alarms[key]
		actual := routingStep(trace, component.RuleID, "COMPONENT")
		if s.Name == "BASELINE" && b.BaselinePolicy == "RECORDED_ACTIVATIONS" && actual != nil {
			old = cloneAlarm(actual.Before.Alarm)
			if actual.PreviousWatermark != nil {
				s.Watermarks[key] = *actual.PreviousWatermark
			}
		}
		hash, _ := analytics.AnalysisHash([]any{s.Name, key, v.ID, len(s.Outcomes)})
		candidate := model.Alarm{ID: run.ID + ":logical-alarm:" + hash, TenantID: msg.TenantID, DeviceID: msg.DeviceID, RuleID: component.RuleID, TriggerID: msg.MessageID, ComponentID: component.Component.ID, AlarmType: component.AlarmType, AlarmLevel: "HIGH", Status: "ACTIVE", FirstTriggeredAt: *times.ComponentAtMillis, LastTriggeredAt: *times.ComponentAtMillis, TriggerCount: 1}
		if s.Name == "BASELINE" && b.BaselinePolicy == "RECORDED_ACTIVATIONS" && actual != nil && actual.Event == "raised" {
			candidate.ID = actual.After.Alarm.ID
		}
		decision := eval.ComponentTransition(candidate, old, component.State, s.Watermarks[key])
		if s.Name == "BASELINE" && b.BaselinePolicy == "RECORDED_ACTIVATIONS" && actual != nil {
			if decision.Applied != actual.Applied || decision.Event != actual.Event || actual.Applied && !sameLogicalAlarm(decision.Alarm, actual.After.Alarm) {
				s.Metrics.Limitations = unique(s.Metrics.Limitations, "TRACE_ROUTING_COMMIT_DIFFERS_FROM_PURE_EVALUATION")
			}
			decision.Applied = actual.Applied
			decision.Alarm = cloneAlarm(actual.After.Alarm)
			decision.Event = actual.Event
			if actual.Watermark != nil {
				decision.Watermark = *actual.Watermark
			}
		}
		s.Watermarks[key] = decision.Watermark
		if decision.Applied && decision.Alarm.ID != "" {
			s.record(run, v, "COMPONENT", old, decision.Alarm, decision.Event == "raised", decision.Event, nil, inMain, initial)
		}
	}
	if route.DirectRaise != nil {
		direct := route.DirectRaise
		times := routeTimes(m, v, trace, direct.RuleID, "DIRECT_RAISE")
		if times.RaiseAtMillis == nil {
			s.Metrics.UncomputedMessages++
			s.Metrics.Limitations = unique(s.Metrics.Limitations, "COUNTERFACTUAL_DIRECT_RAISE_CLOCK_MISSING")
		} else {
			key := alarmKey(msg.DeviceID, direct.RuleID)
			old := s.Alarms[key]
			actual := routingStep(trace, direct.RuleID, "DIRECT_RAISE")
			if s.Name == "BASELINE" && b.BaselinePolicy == "RECORDED_ACTIVATIONS" && actual != nil {
				old = cloneAlarm(actual.Before.Alarm)
			}
			hash, _ := analytics.AnalysisHash([]any{s.Name, key, v.ID, len(s.Outcomes)})
			candidate := eval.RuleAlarmCandidate(model.AlarmRule{ID: direct.RuleID, Name: "设备直接断言", AlarmType: direct.AlarmType, Level: direct.Level}, msg, run.ID+":logical-alarm:"+hash, *times.RaiseAtMillis)
			if s.Name == "BASELINE" && b.BaselinePolicy == "RECORDED_ACTIVATIONS" && actual != nil && !openAlarm(old) {
				candidate.ID = actual.After.Alarm.ID
			}
			next, created, write := eval.UpsertAlarm(candidate, old)
			if s.Name == "BASELINE" && b.BaselinePolicy == "RECORDED_ACTIVATIONS" && actual != nil {
				if write != actual.Applied || actual.Applied && !sameLogicalAlarm(next, actual.After.Alarm) {
					s.Metrics.Limitations = unique(s.Metrics.Limitations, "TRACE_ROUTING_COMMIT_DIFFERS_FROM_PURE_EVALUATION")
				}
				next = cloneAlarm(actual.After.Alarm)
				created = !openAlarm(old) && actual.Applied
				write = actual.Applied
			}
			if write {
				s.record(run, v, "DIRECT", old, next, created, "", nil, inMain, initial)
			}
		}
	}
	if route.DirectRecover {
		keys := make([]string, 0, len(s.Alarms))
		for key := range s.Alarms {
			keys = append(keys, key)
		}
		if s.Name == "BASELINE" && b.BaselinePolicy == "RECORDED_ACTIVATIONS" && trace != nil {
			for _, actual := range trace.RoutingSteps {
				if actual.Kind == "DIRECT_RECOVER" {
					key := alarmKey(msg.DeviceID, actual.RuleID)
					s.Alarms[key] = cloneAlarm(actual.Before.Alarm)
					if !slices.Contains(keys, key) {
						keys = append(keys, key)
					}
				}
			}
		}
		slices.Sort(keys)
		for _, key := range keys {
			old := s.Alarms[key]
			actual := routingStep(trace, old.RuleID, "DIRECT_RECOVER")
			if s.Name == "BASELINE" && b.BaselinePolicy == "RECORDED_ACTIVATIONS" && actual != nil {
				old = cloneAlarm(actual.Before.Alarm)
			}
			if old.DeviceID != msg.DeviceID || !strings.HasPrefix(old.RuleID, eval.DirectRulePrefix) || old.ComponentID != "" || !openAlarm(old) || !eval.DirectTypeCleared(msg, old.AlarmType) {
				continue
			}
			times := routeTimes(m, v, trace, old.RuleID, "DIRECT_RECOVER")
			if times.RecoverAtMillis == nil {
				s.Metrics.UncomputedMessages++
				s.Metrics.Limitations = unique(s.Metrics.Limitations, "DIRECT_RECOVERY_STAGE_CLOCK_MISSING")
				continue
			}
			next := old
			next.Status = "RECOVERED"
			next.RecoveredAt = *times.RecoverAtMillis
			if s.Name == "BASELINE" && b.BaselinePolicy == "RECORDED_ACTIVATIONS" && actual != nil {
				if !actual.Applied {
					s.Alarms[key] = cloneAlarm(actual.After.Alarm)
					s.Metrics.Limitations = unique(s.Metrics.Limitations, "TRACE_ROUTING_COMMIT_DIFFERS_FROM_PURE_EVALUATION")
					continue
				}
				next = cloneAlarm(actual.After.Alarm)
			}
			s.record(run, v, "DIRECT", old, next, false, "recovered", nil, inMain, initial)
		}
	}
	if trace == nil || trace.Status == "COMPLETE" {
		s.Seen[v.ID] = true
	}
	return nil
}
func (s *branchState) cycles(window model.FactRange) []model.RuleLabOutcome {
	result := []model.RuleLabOutcome{}
	for _, v := range s.Outcomes {
		if s.Relevant[v.ID] {
			result = append(result, v)
		}
	}
	slices.SortFunc(result, func(a, b model.RuleLabOutcome) int {
		if a.TriggeredAt < b.TriggeredAt {
			return -1
		}
		if a.TriggeredAt > b.TriggeredAt {
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	return result
}
func verifyTraceClockSelection(m datasetChunk, b model.RuleLabExperiment) []string {
	limitations := []string{}
	if m.Selection.ClockPolicy != "RECORDED_TRACE" {
		return unique(limitations, "HISTORICAL_SIMULATION_EXPLICIT_SORT_AND_CLOCK_POLICY")
	}
	if b.BaselinePolicy != "RECORDED_ACTIVATIONS" {
		limitations = unique(limitations, "FIXED_REVISIONS_DO_NOT_REPRODUCE_RECORDED_ACTIVATION_HISTORY")
	}
	return limitations
}
func (s *branchState) finish(window model.FactRange) {
	s.Metrics.UnrecoveredCycles = 0
	s.Metrics.RecoveredCycles = 0
	s.Metrics.RecoveredDurationMs = 0
	s.Metrics.DurationUnknownCycles = 0
	for _, v := range s.cycles(window) {
		if v.Status == "ACTIVE" || v.Status == "ACKED" {
			s.Metrics.UnrecoveredCycles++
		}
		if v.DurationMs != nil {
			s.Metrics.RecoveredCycles++
			s.Metrics.RecoveredDurationMs += *v.DurationMs
		} else {
			s.Metrics.DurationUnknownCycles++
		}
	}
}
func stepFinding(run model.AnalysisRun, key, device, kind, explanation string, values any) model.AnalysisOutput {
	body, _ := json.Marshal(map[string]any{"id": run.ID + ":finding:" + key, "deviceId": device, "kind": kind, "explanation": explanation, "values": values, "manualState": "UNREVIEWED", "evidenceIds": []string{}})
	return model.AnalysisOutput{ID: run.ID + ":finding:" + key, Kind: "findings", DeviceID: device, Body: body}
}
func outcomeSummary(v model.RuleLabOutcome) string {
	return fmt.Sprintf("%s:%s:%s", v.Branch, v.Category, v.Status)
}
