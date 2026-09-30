package eval

import (
	"errors"
	"fmt"
	"slices"

	"iot-platform/internal/model"
)

const ProcessingV1 = "processing-seconds-multistage-active-recovery-v1"
const RevisionV2 = "processing-multistage-revisioned-retain-open-v2"

var ErrMissingClock = errors.New("required rule stage clock is missing")

// Optional stage times distinguish an unobserved processing clock from a
// genuine epoch-zero value. Only clocks used by the selected branch are needed.
type StageTimes = model.RuleStageTimes

type Pending = model.RuleDurationState

const (
	KeepPending  = "KEEP"
	SetPending   = "SET"
	ClearPending = "CLEAR"
)

type DurationDecision struct {
	Satisfied bool    `json:"satisfied"`
	Pending   Pending `json:"pending"`
	Mutation  string  `json:"mutation"`
}

// Duration uses whole processing seconds, just as Engine.durationSatisfied.
// Once the first match is saved, no timer fires independently of a new message.
// Even a satisfied duration leaves the original pending timestamp in place.
func Duration(seconds int64, pending Pending, nowSeconds int64) DurationDecision {
	out := DurationDecision{Satisfied: seconds <= 0, Pending: pending, Mutation: KeepPending}
	if seconds <= 0 {
		return out
	}
	if !pending.Exists {
		out.Pending = Pending{Since: nowSeconds, Exists: true}
		out.Mutation = SetPending
		return out
	}
	out.Satisfied = nowSeconds-pending.Since >= seconds
	return out
}

type RuleState struct {
	Pending    Pending     `json:"pending"`
	Alarm      model.Alarm `json:"alarm"`
	NewAlarmID string      `json:"newAlarmId"`
}

type ActionIntent = model.RuleActionIntent

type Decision struct {
	SemanticsVersion  string         `json:"semanticsVersion"`
	Covered           bool           `json:"covered"`
	Matched           bool           `json:"matched"`
	EvaluationError   string         `json:"evaluationError,omitempty"`
	RecoveryMatched   bool           `json:"recoveryMatched"`
	DurationSatisfied bool           `json:"durationSatisfied"`
	Pending           Pending        `json:"pending"`
	PendingMutation   string         `json:"pendingMutation"`
	RuleAlarmHandled  bool           `json:"ruleAlarmHandled"`
	Alarm             model.Alarm    `json:"alarm"`
	WriteAlarm        bool           `json:"writeAlarm"`
	Created           bool           `json:"created"`
	Event             string         `json:"event,omitempty"`
	Actions           []ActionIntent `json:"actions"`
	Times             StageTimes     `json:"times"`
}

// Transition describes the inspected production rule loop. It changes no input
// maps and executes no side effect. The executor remains responsible for an
// atomic upsert, claim fencing, outbox delivery and optimistic recovery retries.
// RuleState.Alarm is the current lifecycle for the same tenant/device/rule.
func Transition(rule model.AlarmRule, msg model.StandardMessage, state RuleState, times StageTimes) (Decision, error) {
	out := Decision{SemanticsVersion: ProcessingV1, Covered: Covers(rule, msg), Pending: state.Pending, PendingMutation: KeepPending, Alarm: state.Alarm, Actions: []ActionIntent{}}
	if !out.Covered {
		return out, nil
	}
	if open(state.Alarm) && (state.Alarm.ID == "" || state.Alarm.RuleID != rule.ID || state.Alarm.TenantID != msg.TenantID || state.Alarm.DeviceID != msg.DeviceID) {
		return out, errors.New("alarm lifecycle is outside the evaluated rule scope")
	}
	matched, err := Match(rule, msg)
	if err != nil {
		out.EvaluationError = err.Error()
		matched = false // The current boolean predicate swallows expression errors.
	}
	out.Matched = matched
	if matched {
		out.DurationSatisfied = true
		if rule.DurationSeconds > 0 {
			if times.DurationAtSeconds == nil {
				return out, fmt.Errorf("%w: duration", ErrMissingClock)
			}
			out.Times.DurationAtSeconds = cloneTime(times.DurationAtSeconds)
			duration := Duration(rule.DurationSeconds, state.Pending, *times.DurationAtSeconds)
			out.DurationSatisfied, out.Pending, out.PendingMutation = duration.Satisfied, duration.Pending, duration.Mutation
			if !duration.Satisfied {
				return out, nil // No recovery branch or direct-rule handling yet.
			}
		}
		if times.RaiseAtMillis == nil {
			return out, fmt.Errorf("%w: raise", ErrMissingClock)
		}
		out.Times.RaiseAtMillis = cloneTime(times.RaiseAtMillis)
		if state.NewAlarmID == "" && !open(state.Alarm) {
			return out, errors.New("new alarm identity is required")
		}
		candidate := RuleAlarmCandidate(rule, msg, state.NewAlarmID, *times.RaiseAtMillis)
		out.RuleAlarmHandled = true
		out.Alarm, out.Created, out.WriteAlarm = UpsertAlarm(candidate, state.Alarm)
		if out.Created {
			out.Event = "raised"
		}
		// The original TriggerID is intentionally retained for v1, including its
		// partial-retry behavior. The standard processed marker rejects completed
		// duplicates before this function is called.
		if out.Created || out.Alarm.TriggerID != msg.MessageID {
			for _, action := range rule.Actions {
				out.Actions = append(out.Actions, ActionIntent{Action: action, RuleID: rule.ID, AlarmID: out.Alarm.ID, DeviceID: msg.DeviceID, MessageID: msg.MessageID, TriggeredAt: *times.RaiseAtMillis})
			}
		}
		return out, nil
	}
	if rule.DurationSeconds > 0 {
		out.Pending = Pending{}
		out.PendingMutation = ClearPending
	}
	out.RecoveryMatched = MatchConditions(rule.Recovery, msg)
	if !out.RecoveryMatched || state.Alarm.Status != "ACTIVE" || state.Alarm.RuleID != rule.ID || state.Alarm.TenantID != msg.TenantID || state.Alarm.DeviceID != msg.DeviceID {
		return out, nil
	}
	if times.RecoverAtMillis == nil {
		return out, fmt.Errorf("%w: recovery", ErrMissingClock)
	}
	out.Times.RecoverAtMillis = cloneTime(times.RecoverAtMillis)
	out.Alarm.Status = "RECOVERED"
	out.Alarm.RecoveredAt = *times.RecoverAtMillis
	out.WriteAlarm = true
	out.Event = "recovered"
	return out, nil
}

// RuleAlarmCandidate contains only message-derived rule data. Cameras and
// current device names are enrichment performed by a production executor.
func RuleAlarmCandidate(rule model.AlarmRule, msg model.StandardMessage, id string, nowMillis int64) model.Alarm {
	return model.Alarm{ID: id, TenantID: msg.TenantID, RuleID: rule.ID, TriggerID: msg.MessageID, DeviceID: msg.DeviceID, AlarmType: rule.AlarmType, AlarmLevel: rule.Level, Status: "ACTIVE", Source: "device", CityCode: messageTag(msg, "cityCode", "unknown"), DistrictCode: messageTag(msg, "districtCode", "unknown"), BuildingID: messageTag(msg, "buildingId", "unknown"), DeviceType: messageTag(msg, "deviceType", msg.ProductID), AreaID: messageTag(msg, "areaId", ""), FirstTriggeredAt: nowMillis, LastTriggeredAt: nowMillis, TriggerCount: 1, Details: map[string]any{"message": msg, "ruleName": rule.Name}}
}

// UpsertAlarm returns next, created, write. It preserves the current ordinary
// alarm repository semantics; component lifecycles use ComponentTransition.
// Storage Version belongs to the adapter: an update retains the read version
// for CAS, and a newly created lifecycle has no prior storage version.
func UpsertAlarm(candidate, previous model.Alarm) (model.Alarm, bool, bool) {
	if !open(previous) {
		candidate.Version = 0
		return candidate, true, true
	}
	if candidate.TriggerID != "" && previous.TriggerID == candidate.TriggerID {
		return previous, false, false
	}
	previous.LastTriggeredAt = candidate.LastTriggeredAt
	previous.TriggerCount++
	if candidate.Confidence > previous.Confidence {
		previous.Confidence = candidate.Confidence
	}
	return previous, false, true
}

func open(alarm model.Alarm) bool { return slices.Contains([]string{"ACTIVE", "ACKED"}, alarm.Status) }
func cloneTime(v *int64) *int64 {
	if v == nil {
		return nil
	}
	n := *v
	return &n
}
func messageTag(msg model.StandardMessage, key, fallback string) string {
	if value := msg.Tags[key]; value != "" {
		return value
	}
	return fallback
}
