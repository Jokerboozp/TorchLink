package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"iot-platform/internal/model"
	"iot-platform/internal/rulelab/eval"
)

func (e *Engine) evaluateRules(ctx context.Context, msg model.StandardMessage, claim model.StandardClaim) (bool, model.RuleTraceBinding, error) {
	binding := model.RuleTraceBinding{TenantID: msg.TenantID, MessageID: msg.MessageID, ClaimToken: claim.Token}
	rules, err := e.Repo.RuleEvaluationRules(ctx, msg.TenantID, msg.DeviceID)
	if err != nil {
		return false, binding, err
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return false, binding, err
	}
	digest := sha256.Sum256(raw)
	trace := model.RuleEvaluationTrace{RuleTraceBinding: binding, RawMessageID: msg.RawMessageID, DeviceID: msg.DeviceID, ProductID: msg.ProductID, MessageTimestamp: msg.Timestamp, MessageHash: hex.EncodeToString(digest[:]), ClaimOwner: e.Identity(), Rules: rules, RuleSetHash: model.RuleSetHash(rules), SemanticsVersion: eval.RevisionV2}
	if err = e.Repo.BeginRuleEvaluationTrace(ctx, trace); err != nil {
		return false, binding, err
	}
	handled := false
	for index, revision := range rules {
		// Enrichment cannot run inside the repository's atomic state callback.
		// Stage clocks are still observed independently at the actual duration,
		// raise or recovery branch, and are persisted with its state mutation.
		deviceName := ""
		var cameras []model.CameraSummary
		if matched, _ := eval.Match(revision.Rule, msg); matched {
			deviceName = e.alarmDeviceName(ctx, msg.TenantID, msg.DeviceID)
			cameras, _ = e.ListCameraSummaries(ctx, msg.TenantID, msg.DeviceID)
		}
		step, err := e.Repo.CommitRuleEvaluationStep(ctx, binding, index, func(state model.RuleEvaluationState) (model.RuleEvaluationStep, error) {
			rule := revision.Rule
			if state.RecoveryRevision != nil {
				rule.Recovery = state.RecoveryRevision.Rule.Recovery
				if !eval.Covers(rule, msg) && eval.Covers(state.RecoveryRevision.Rule, msg) {
					rule.Enabled = false
					rule.ProductID = state.RecoveryRevision.Rule.ProductID
				}
			}
			times := eval.StageTimes{}
			matched, _ := eval.Match(rule, msg)
			if matched {
				ready := true
				if rule.DurationSeconds > 0 {
					now := e.Clock.Now().Unix()
					times.DurationAtSeconds = &now
					ready = eval.Duration(rule.DurationSeconds, state.Pending, now).Satisfied
				}
				if ready {
					now := e.Clock.Now().UnixMilli()
					times.RaiseAtMillis = &now
				}
			} else if eval.Covers(rule, msg) && state.Alarm.Status == "ACTIVE" && eval.MatchConditions(rule.Recovery, msg) {
				now := e.Clock.Now().UnixMilli()
				times.RecoverAtMillis = &now
			}
			decision, err := eval.TransitionRevision(revision, msg, eval.RuleState{Pending: state.Pending, Alarm: state.Alarm, NewAlarmID: id("alarm")}, state.RecoveryRevision, times)
			if err != nil {
				return model.RuleEvaluationStep{}, err
			}
			if decision.Created {
				decision.Alarm.DeviceName = deviceName
				decision.Alarm.Cameras = cameras
			}
			step := model.RuleEvaluationStep{RuleRevisionID: revision.ID, SemanticsVersion: decision.SemanticsVersion, Covered: decision.Covered, Matched: decision.Matched, EvaluationError: decision.EvaluationError, RecoveryMatched: decision.RecoveryMatched, DurationSatisfied: decision.DurationSatisfied, Pending: decision.Pending, PendingMutation: decision.PendingMutation, RuleAlarmHandled: decision.RuleAlarmHandled, Alarm: decision.Alarm, WriteAlarm: decision.WriteAlarm, Created: decision.Created, Event: decision.Event, Actions: decision.Actions, Times: decision.Times}
			if decision.RuleAlarmHandled {
				report := eval.RuleAlarmCandidate(rule, msg, decision.Alarm.ID, *times.RaiseAtMillis)
				report.DeviceName = deviceName
				report.Cameras = cameras
				step.ReportAlarm = &report
			}
			return step, nil
		})
		if err != nil {
			return handled, binding, err
		}
		handled = handled || step.RuleAlarmHandled
		e.publishRuleStep(ctx, msg, step)
	}
	return handled, binding, nil
}

func (e *Engine) publishRuleStep(ctx context.Context, msg model.StandardMessage, step model.RuleEvaluationStep) {
	if step.Created {
		e.count("alarm_trigger_total")
	}
	if step.Event != "" {
		topic := model.TopicAlarmRaised
		if step.Event == "recovered" {
			topic = model.TopicAlarmRecovered
		}
		payload := mustJSON(step.Alarm)
		_ = e.Bus.Publish(ctx, topic, step.Alarm.ID, payload)
		_ = e.Realtime.Publish(ctx, step.Alarm.MQTTTopic(step.Event), payload, 1, false)
	}
	e.flushOutbox(ctx)
	for _, intent := range step.Actions {
		event := model.UIActionEvent{ID: id("ui_action"), TenantID: msg.TenantID, RuleID: intent.RuleID, AlarmID: intent.AlarmID, DeviceID: intent.DeviceID, Action: intent.Action, TriggeredAt: intent.TriggeredAt}
		payload := mustJSON(event)
		_ = e.Bus.Publish(ctx, model.TopicUIAction, event.ID, payload)
		_ = e.Realtime.Publish(ctx, fmt.Sprintf("/iot/ui-action/%s", msg.TenantID), payload, 1, false)
	}
}

func (e *Engine) PublishRule(ctx context.Context, request model.RulePublishRequest) (model.AlarmRuleRevision, error) {
	request.SemanticsVersion = eval.RevisionV2
	if !request.Delete {
		if _, _, err := e.ValidateRuleDraft(ctx, request.Rule); err != nil {
			return model.AlarmRuleRevision{}, err
		}
	}
	value, err := e.Repo.PublishRule(ctx, request)
	if err == nil {
		e.RulesChanged(request.Rule.TenantID)
	}
	return value, err
}
