// Package history validates immutable history and atomic production trace
// records shared by the memory and PostgreSQL adapters.
package history

import (
	"encoding/json"
	"errors"
	"slices"

	"iot-platform/internal/model"
	"iot-platform/internal/rulelab/eval"
)

// SameRulePolicy compares immutable business semantics while allowing the new
// publication to receive its own version and registration times.
func SameRulePolicy(a, b model.AlarmRule) bool {
	a.Version, b.Version = 0, 0
	a.CreatedAt, b.CreatedAt = 0, 0
	a.UpdatedAt, b.UpdatedAt = 0, 0
	return model.RuleBodyHash(a) == model.RuleBodyHash(b)
}

func Revision(request model.RulePublishRequest, current *model.AlarmRuleRevision, now int64) (model.AlarmRuleRevision, model.AlarmRuleActivation, error) {
	if err := model.ValidateRulePublish(request); err != nil {
		return model.AlarmRuleRevision{}, model.AlarmRuleActivation{}, err
	}
	version := 0
	if current != nil {
		version = current.Version
	}
	if version != request.ExpectedBaselineVersion {
		return model.AlarmRuleRevision{}, model.AlarmRuleActivation{}, model.ErrRuleConflict
	}
	rule := request.Rule
	if request.Delete {
		if current == nil {
			return model.AlarmRuleRevision{}, model.AlarmRuleActivation{}, model.ErrNotFound
		}
		rule = current.Rule
		rule.Enabled = false
	}
	rule.Version = version + 1
	if current != nil {
		rule.CreatedAt = current.Rule.CreatedAt
	}
	if rule.CreatedAt == 0 {
		rule.CreatedAt = now
	}
	rule.UpdatedAt = now
	v := model.AlarmRuleRevision{ID: model.RuleRevisionID(rule), TenantID: rule.TenantID, RuleID: rule.ID, Version: rule.Version, Hash: model.RuleBodyHash(rule), Rule: rule, Reason: request.Reason, Actor: request.Actor, RegisteredAt: now, InitialRegistration: current == nil, RollbackFrom: request.RollbackFrom, ExperimentID: request.ExperimentID, SemanticsVersion: request.SemanticsVersion}
	a := model.AlarmRuleActivation{ID: v.ID + "/activation", TenantID: v.TenantID, RuleID: v.RuleID, RevisionID: v.ID, Version: v.Version, Since: now, Deleted: request.Delete, Actor: v.Actor, Reason: v.Reason}
	return v, a, nil
}

// Register observes an existing row without changing its body or fabricating a
// past activation. Version zero is preserved as historical input metadata.
func Register(rule model.AlarmRule, now int64) (model.AlarmRuleRevision, model.AlarmRuleActivation) {
	v := model.AlarmRuleRevision{ID: model.RuleRevisionID(rule), TenantID: rule.TenantID, RuleID: rule.ID, Version: rule.Version, Hash: model.RuleBodyHash(rule), Rule: rule, Reason: "initial current-version registration; earlier activation history unknown", Actor: "system", RegisteredAt: now, InitialRegistration: true, SemanticsVersion: eval.RevisionV2}
	a := model.AlarmRuleActivation{ID: v.ID + "/activation", TenantID: v.TenantID, RuleID: v.RuleID, RevisionID: v.ID, Version: v.Version, Since: now, Actor: v.Actor, Reason: v.Reason}
	return v, a
}

func Begin(trace model.RuleEvaluationTrace, now int64) (model.RuleEvaluationTrace, error) {
	if trace.TenantID == "" || trace.MessageID == "" || trace.DeviceID == "" || trace.ClaimToken <= 0 || trace.ClaimOwner == "" || trace.MessageHash == "" || trace.SemanticsVersion != eval.RevisionV2 || trace.RuleSetHash != model.RuleSetHash(trace.Rules) {
		return trace, model.ErrRuleHistoryInvalid
	}
	seen := map[string]bool{}
	for _, r := range trace.Rules {
		if r.TenantID != trace.TenantID || r.ID == "" || r.Hash != model.RuleBodyHash(r.Rule) || seen[r.RuleID] {
			return trace, model.ErrRuleHistoryInvalid
		}
		seen[r.RuleID] = true
	}
	trace.ID = model.RuleTraceID(trace.RuleTraceBinding)
	trace.Attempt = trace.ClaimToken
	trace.StartedAt = now
	trace.FinishedAt = 0
	trace.Status = "IN_PROGRESS"
	trace.ReproductionQuality = "INCOMPLETE_TRACE"
	trace.Steps = []model.RuleEvaluationStep{}
	trace.Limitations = []string{}
	trace.Routing = model.RuleRoutingTrace{}
	trace.RoutingSteps = []model.RuleRoutingStep{}
	return trace, nil
}

func Step(trace model.RuleEvaluationTrace, sequence int, state model.RuleEvaluationState, step model.RuleEvaluationStep, now int64) (model.RuleEvaluationStep, error) {
	if sequence < 0 || sequence >= len(trace.Rules) || sequence != len(trace.Steps) || trace.Status != "IN_PROGRESS" {
		return step, model.ErrRuleHistoryInvalid
	}
	rule := trace.Rules[sequence]
	if step.RuleRevisionID != rule.ID || step.SemanticsVersion != trace.SemanticsVersion || !slices.Contains([]string{eval.KeepPending, eval.SetPending, eval.ClearPending}, step.PendingMutation) {
		return step, model.ErrRuleHistoryInvalid
	}
	if step.WriteAlarm && (step.Alarm.TenantID != trace.TenantID || step.Alarm.DeviceID != trace.DeviceID || step.Alarm.RuleID != rule.RuleID || step.Alarm.ID == "") {
		return step, model.ErrRuleHistoryInvalid
	}
	if step.Created && (!step.WriteAlarm || state.Alarm.ID != "" || step.Alarm.CreatedRuleRevision != rule.ID) {
		return step, model.ErrRuleHistoryInvalid
	}
	if state.Alarm.ID != "" && step.WriteAlarm && (step.Alarm.ID != state.Alarm.ID || step.Alarm.CreatedRuleRevision != state.Alarm.CreatedRuleRevision) {
		return step, model.ErrRuleHistoryInvalid
	}
	if step.Matched && rule.Rule.DurationSeconds > 0 && step.Times.DurationAtSeconds == nil {
		return step, eval.ErrMissingClock
	}
	if step.RuleAlarmHandled && step.Times.RaiseAtMillis == nil {
		return step, eval.ErrMissingClock
	}
	if step.Event == "recovered" && step.Times.RecoverAtMillis == nil {
		return step, eval.ErrMissingClock
	}
	step.Sequence = sequence
	step.Before = state
	step.Before.AlarmVersion = state.Alarm.Version
	step.CommittedAt = now
	return step, nil
}

func Finish(trace model.RuleEvaluationTrace, now int64) model.RuleEvaluationTrace {
	trace.Status = "COMPLETE"
	trace.FinishedAt = now
	trace.ReproductionQuality = "EXACT"
	if len(trace.Steps) != len(trace.Rules) || !trace.Routing.Committed {
		trace.ReproductionQuality = "INCOMPLETE_TRACE"
		trace.Limitations = append(trace.Limitations, "规则阶段或最终路由记录不完整")
	}
	if trace.Routing.UntracedSideEffects && trace.ReproductionQuality != "INCOMPLETE_TRACE" {
		trace.ReproductionQuality = "HISTORICAL_SIMULATION"
		trace.Limitations = append(trace.Limitations, "direct/部件副作用缺少同事务状态 trace，不能声称完整生产重现")
	}
	if !RoutingComplete(trace) && trace.ReproductionQuality != "INCOMPLETE_TRACE" {
		trace.ReproductionQuality = "HISTORICAL_SIMULATION"
		trace.Limitations = append(trace.Limitations, "路由阶段初态、时钟、提交或最终状态无法完整对应")
	}
	for _, step := range trace.Steps {
		if step.Before.InitialStateQuality != "KNOWN" && trace.ReproductionQuality != "INCOMPLETE_TRACE" {
			trace.ReproductionQuality = "HISTORICAL_SIMULATION"
			trace.Limitations = append(trace.Limitations, "存在未登记产生版本的旧告警初态")
		}
	}
	return trace
}

func RoutingAlarm(alarm model.Alarm) model.RuleRoutingAlarm {
	alarm.Details = nil
	alarm.Cameras = nil
	return model.RuleRoutingAlarm{Alarm: alarm, Version: alarm.Version}
}
func sameRoutingState(a, b any) bool {
	one, _ := json.Marshal(a)
	two, _ := json.Marshal(b)
	return string(one) == string(two)
}
func RoutingComplete(trace model.RuleEvaluationTrace) bool {
	direct := map[string]model.RuleRoutingAlarm{}
	components := map[string]model.RuleRoutingComponent{}
	for id, v := range trace.Routing.Initial.Direct {
		direct[id] = v
	}
	for id, v := range trace.Routing.Initial.Components {
		components[id] = v
	}
	componentCount, directCount := 0, 0
	for _, step := range trace.RoutingSteps {
		if step.Kind == "COMPONENT" {
			componentCount++
			before := components[step.RuleID]
			if step.Times.ComponentAtMillis == nil || step.PreviousWatermark == nil || step.Watermark == nil || !sameRoutingState(before.Lifecycle, step.Before) || !sameRoutingState(before.Watermark, *step.PreviousWatermark) {
				return false
			}
			components[step.RuleID] = model.RuleRoutingComponent{Watermark: *step.Watermark, Lifecycle: step.After}
		} else {
			if !sameRoutingState(direct[step.RuleID], step.Before) {
				return false
			}
			if step.Kind == "DIRECT_RAISE" {
				directCount++
				if step.Times.RaiseAtMillis == nil {
					return false
				}
			} else if step.Kind == "DIRECT_RECOVER" {
				if step.Times.RecoverAtMillis == nil {
					return false
				}
			} else {
				return false
			}
			if step.After.Alarm.Status == "ACTIVE" || step.After.Alarm.Status == "ACKED" {
				direct[step.RuleID] = step.After
			} else {
				delete(direct, step.RuleID)
			}
		}
	}
	if componentCount != trace.Routing.ExpectedComponents || directCount != map[bool]int{true: 1, false: 0}[trace.Routing.ExpectedDirectRaise] {
		return false
	}
	return sameRoutingState(direct, trace.Routing.Final.Direct) && sameRoutingState(components, trace.Routing.Final.Components)
}

func Trim(step model.RuleEvaluationStep) model.RuleEvaluationStep {
	step.ReportAlarm = nil
	step.Before.Alarm.Details = nil
	step.Before.Alarm.Cameras = nil
	step.Alarm.Details = nil
	step.Alarm.Cameras = nil
	return step
}

func ValidateFilter(filter model.RuleTraceFilter) error {
	if len(filter.DeviceIDs) == 0 {
		return model.ErrRuleHistoryInvalid
	}
	for _, id := range filter.DeviceIDs {
		if id == "" {
			return model.ErrRuleHistoryInvalid
		}
	}
	if filter.Start != 0 && filter.End != 0 && filter.Start >= filter.End {
		return errors.New("trace window must be [start,end)")
	}
	return nil
}
