package memory

import (
	"context"
	"slices"
	"sort"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/rulelab/eval"
	"iot-platform/internal/rulelab/history"
)

type memoryRuleHistory struct {
	revisions   map[string]model.AlarmRuleRevision
	current     map[string]model.AlarmRuleActivation
	activations []model.AlarmRuleActivation
	pending     map[string]model.RuleDurationState
	traces      map[string]model.RuleEvaluationTrace
}

func (r *Repository) initRuleHistoryLocked() {
	if r.ruleHistory.revisions != nil {
		return
	}
	r.ruleHistory = memoryRuleHistory{revisions: map[string]model.AlarmRuleRevision{}, current: map[string]model.AlarmRuleActivation{}, pending: map[string]model.RuleDurationState{}, traces: map[string]model.RuleEvaluationTrace{}}
	for _, rule := range r.rules {
		revision, activation := history.Register(rule, time.Now().UnixMilli())
		r.ruleHistory.revisions[key(rule.TenantID, revision.ID)] = revision
		r.ruleHistory.current[key(rule.TenantID, rule.ID)] = activation
		r.ruleHistory.activations = append(r.ruleHistory.activations, activation)
	}
}
func (r *Repository) currentRuleVersionLocked(tenant, id string) int {
	r.initRuleHistoryLocked()
	return r.ruleHistory.current[key(tenant, id)].Version
}
func (r *Repository) saveRuleHistoryLocked(rule model.AlarmRule) error {
	r.initRuleHistoryLocked()
	if current, ok := r.rules[key(rule.TenantID, rule.ID)]; ok && model.RuleBodyHash(current) == model.RuleBodyHash(rule) {
		return nil
	}
	_, err := r.publishRuleLocked(model.RulePublishRequest{Rule: rule, ExpectedBaselineVersion: r.currentRuleVersionLocked(rule.TenantID, rule.ID), Reason: "rule saved", Actor: "system", SemanticsVersion: eval.RevisionV2})
	return err
}
func (r *Repository) publishRuleLocked(request model.RulePublishRequest) (model.AlarmRuleRevision, error) {
	r.initRuleHistoryLocked()
	var current *model.AlarmRuleRevision
	if activation, ok := r.ruleHistory.current[key(request.Rule.TenantID, request.Rule.ID)]; ok {
		value := r.ruleHistory.revisions[key(request.Rule.TenantID, activation.RevisionID)]
		current = &value
	}
	if request.RollbackFrom != "" {
		if old, ok := r.ruleHistory.revisions[key(request.Rule.TenantID, request.RollbackFrom)]; !ok || old.RuleID != request.Rule.ID || !history.SameRulePolicy(old.Rule, request.Rule) {
			return model.AlarmRuleRevision{}, model.ErrRuleHistoryInvalid
		}
	}
	revision, activation, err := history.Revision(request, current, time.Now().UnixMilli())
	if err != nil {
		return revision, err
	}
	r.ruleHistory.revisions[key(revision.TenantID, revision.ID)] = clone(revision)
	r.ruleHistory.current[key(revision.TenantID, revision.RuleID)] = activation
	r.ruleHistory.activations = append(r.ruleHistory.activations, activation)
	if request.Delete {
		delete(r.rules, key(revision.TenantID, revision.RuleID))
	} else {
		r.rules[key(revision.TenantID, revision.RuleID)] = clone(revision.Rule)
	}
	// All prior revision timers are discarded; restoring a prior body creates a
	// new revision and can never revive its old pending.
	r.clearRevisionPendingLocked(revision.TenantID, revision.RuleID)
	return clone(revision), nil
}
func (r *Repository) PublishRule(_ context.Context, request model.RulePublishRequest) (model.AlarmRuleRevision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.publishRuleLocked(request)
}
func (r *Repository) GetRuleRevision(_ context.Context, tenant, id string) (model.AlarmRuleRevision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.initRuleHistoryLocked()
	value, ok := r.ruleHistory.revisions[key(tenant, id)]
	if !ok {
		return value, model.ErrNotFound
	}
	return clone(value), nil
}
func ruleHistoryPage(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return limit, max(offset, 0)
}
func (r *Repository) ListRuleRevisions(_ context.Context, tenant, rule string, limit, offset int) ([]model.AlarmRuleRevision, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.initRuleHistoryLocked()
	limit, offset = ruleHistoryPage(limit, offset)
	rows := []model.AlarmRuleRevision{}
	for _, v := range r.ruleHistory.revisions {
		if v.TenantID == tenant && v.RuleID == rule {
			rows = append(rows, clone(v))
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Version > rows[j].Version })
	return page(rows, offset, limit), len(rows), nil
}
func (r *Repository) ListRuleActivations(_ context.Context, tenant, rule string, limit, offset int) ([]model.AlarmRuleActivation, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.initRuleHistoryLocked()
	limit, offset = ruleHistoryPage(limit, offset)
	rows := []model.AlarmRuleActivation{}
	for _, v := range r.ruleHistory.activations {
		if v.TenantID == tenant && v.RuleID == rule {
			rows = append(rows, clone(v))
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Version > rows[j].Version })
	for i := 1; i < len(rows); i++ {
		until := rows[i-1].Since
		rows[i].Until = &until
	}
	return page(rows, offset, limit), len(rows), nil
}
func (r *Repository) RuleEvaluationRules(_ context.Context, tenant, device string) ([]model.AlarmRuleRevision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.initRuleHistoryLocked()
	openRules := map[string]bool{}
	for _, alarm := range r.alarms {
		if alarm.TenantID == tenant && alarm.DeviceID == device && alarm.Status == "ACTIVE" {
			openRules[alarm.RuleID] = true
		}
	}
	rows := []model.AlarmRuleRevision{}
	for _, activation := range r.ruleHistory.current {
		if activation.TenantID == tenant && (!activation.Deleted || openRules[activation.RuleID]) {
			rows = append(rows, clone(r.ruleHistory.revisions[key(tenant, activation.RevisionID)]))
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].RegisteredAt != rows[j].RegisteredAt {
			return rows[i].RegisteredAt > rows[j].RegisteredAt
		}
		return rows[i].RuleID < rows[j].RuleID
	})
	return rows, nil
}
func (r *Repository) checkRuleClaimLocked(binding model.RuleTraceBinding) error {
	k := key(binding.TenantID, binding.MessageID)
	if _, ok := r.standard[k]; !ok {
		return model.ErrNotFound
	}
	if r.standardProcessed[k] || r.claims[k].token != binding.ClaimToken {
		return model.ErrStaleClaim
	}
	return nil
}
func (r *Repository) BeginRuleEvaluationTrace(_ context.Context, trace model.RuleEvaluationTrace) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.initRuleHistoryLocked()
	if err := r.checkRuleClaimLocked(trace.RuleTraceBinding); err != nil {
		return err
	}
	trace, err := history.Begin(trace, time.Now().UnixMilli())
	if err != nil {
		return err
	}
	trace.Routing.Initial = r.routingSnapshotLocked(trace.TenantID, trace.DeviceID)
	k := key(trace.TenantID, trace.ID)
	if old, ok := r.ruleHistory.traces[k]; ok {
		if old.RuleSetHash != trace.RuleSetHash || old.MessageHash != trace.MessageHash {
			return model.ErrRuleHistoryInvalid
		}
		return nil
	}
	for _, revision := range trace.Rules {
		if existing, ok := r.ruleHistory.revisions[key(trace.TenantID, revision.ID)]; !ok || existing.Hash != revision.Hash {
			return model.ErrRuleHistoryInvalid
		}
	}
	r.ruleHistory.traces[k] = clone(trace)
	return nil
}
func (r *Repository) CommitRuleEvaluationStep(ctx context.Context, binding model.RuleTraceBinding, sequence int, calculate func(model.RuleEvaluationState) (model.RuleEvaluationStep, error)) (model.RuleEvaluationStep, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.initRuleHistoryLocked()
	if err := r.checkRuleClaimLocked(binding); err != nil {
		return model.RuleEvaluationStep{}, err
	}
	k := key(binding.TenantID, model.RuleTraceID(binding))
	trace, ok := r.ruleHistory.traces[k]
	if !ok || sequence < 0 || sequence >= len(trace.Rules) {
		return model.RuleEvaluationStep{}, model.ErrRuleHistoryInvalid
	}
	revision := trace.Rules[sequence]
	pendingKey := key(binding.TenantID, revision.ID, trace.DeviceID)
	state := model.RuleEvaluationState{Pending: r.ruleHistory.pending[pendingKey], InitialStateQuality: "KNOWN"}
	for _, alarm := range r.alarms {
		if alarm.TenantID == binding.TenantID && alarm.DeviceID == trace.DeviceID && alarm.RuleID == revision.RuleID && (alarm.Status == "ACTIVE" || alarm.Status == "ACKED") {
			state.Alarm = cloneAlarm(alarm)
			state.AlarmVersion = alarm.Version
			break
		}
	}
	if state.Alarm.ID != "" {
		if recovery, ok := r.ruleHistory.revisions[key(binding.TenantID, state.Alarm.CreatedRuleRevision)]; ok {
			state.RecoveryRevision = &recovery
		} else {
			state.InitialStateQuality = "UNKNOWN"
		}
	}
	step, err := calculate(state)
	if err != nil {
		return step, err
	}
	step, err = history.Step(trace, sequence, state, step, time.Now().UnixMilli())
	if err != nil {
		return step, err
	}
	if capture, ok := model.AlarmObservationCaptureFromContext(ctx); ok && capture.Observation != nil {
		o := *capture.Observation
		o.AlarmID = step.Alarm.ID
		if o.AlarmID == "" {
			o.AlarmID = state.Alarm.ID
		}
		o.WatermarkAt = r.observationStateLocked().Signals[key(o.TenantID, o.DeviceID, o.SignalKey)].SignalWatermarkAt()
		saved, created, err := r.recordAlarmObservationLocked(o)
		if err != nil {
			return step, err
		}
		if created {
			r.acceptSignalLocked(saved)
		}
	}
	if step.WriteAlarm {
		step.Alarm.Version = state.Alarm.Version + 1
		step.AlarmVersion = step.Alarm.Version
		r.alarms[key(binding.TenantID, step.Alarm.ID)] = cloneAlarm(step.Alarm)
		if step.RuleAlarmHandled {
			report := step.Alarm
			if step.ReportAlarm != nil {
				report = *step.ReportAlarm
			}
			report.CreatedRuleRevision = step.Alarm.CreatedRuleRevision
			report.TriggerRuleRevision = step.Alarm.TriggerRuleRevision
			r.addOutbox(model.AlarmReportEvent(step.Alarm, report))
		}
		for _, event := range model.DutyAlarmEvents(ctx, state.Alarm, step.Alarm) {
			r.appendDutyEventLocked(event)
		}
	}
	switch step.PendingMutation {
	case eval.SetPending:
		r.ruleHistory.pending[pendingKey] = step.Pending
	case eval.ClearPending:
		delete(r.ruleHistory.pending, pendingKey)
	}
	trace.Steps = append(trace.Steps, history.Trim(step))
	r.ruleHistory.traces[k] = clone(trace)
	return step, nil
}

func (r *Repository) clearRevisionPendingLocked(tenant, rule string) {
	for _, activation := range r.ruleHistory.activations {
		if activation.TenantID != tenant || activation.RuleID != rule {
			continue
		}
		prefix := key(tenant, activation.RevisionID) + "\x00"
		for pendingKey := range r.ruleHistory.pending {
			if strings.HasPrefix(pendingKey, prefix) {
				delete(r.ruleHistory.pending, pendingKey)
			}
		}
	}
}
func (r *Repository) RecordRuleRoutingTrace(_ context.Context, binding model.RuleTraceBinding, routing model.RuleRoutingTrace) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkRuleClaimLocked(binding); err != nil {
		return err
	}
	k := key(binding.TenantID, model.RuleTraceID(binding))
	trace, ok := r.ruleHistory.traces[k]
	if !ok {
		return model.ErrNotFound
	}
	routing.Committed = true
	routing.Initial = trace.Routing.Initial
	routing.Final = r.routingSnapshotLocked(binding.TenantID, trace.DeviceID)
	trace.Routing = routing
	r.ruleHistory.traces[k] = trace
	return nil
}
func (r *Repository) completeRuleTraceLocked(tenant, message string, token int64) {
	if r.ruleHistory.traces == nil {
		return
	}
	k := key(tenant, model.RuleTraceID(model.RuleTraceBinding{TenantID: tenant, MessageID: message, ClaimToken: token}))
	if trace, ok := r.ruleHistory.traces[k]; ok {
		r.ruleHistory.traces[k] = history.Finish(trace, time.Now().UnixMilli())
	}
}
func (r *Repository) ListRuleEvaluationTraces(_ context.Context, tenant string, filter model.RuleTraceFilter) ([]model.RuleEvaluationTrace, int, error) {
	if err := history.ValidateFilter(filter); err != nil {
		return nil, 0, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.initRuleHistoryLocked()
	limit, offset := ruleHistoryPage(filter.Limit, filter.Offset)
	rows := []model.RuleEvaluationTrace{}
	for _, trace := range r.ruleHistory.traces {
		if trace.TenantID == tenant && slices.Contains(filter.DeviceIDs, trace.DeviceID) && (len(filter.MessageIDs) == 0 || slices.Contains(filter.MessageIDs, trace.MessageID)) && (filter.Start == 0 || trace.MessageTimestamp >= filter.Start) && (filter.End == 0 || trace.MessageTimestamp < filter.End) {
			rows = append(rows, clone(trace))
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].StartedAt != rows[j].StartedAt {
			return rows[i].StartedAt < rows[j].StartedAt
		}
		return rows[i].ID < rows[j].ID
	})
	return page(rows, offset, limit), len(rows), nil
}
