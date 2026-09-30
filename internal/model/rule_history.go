package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

var ErrRuleConflict = errors.New("rule baseline version changed")
var ErrRuleHistoryInvalid = errors.New("invalid rule history operation")

// Revisions are immutable. Initial registration records what is known now and
// makes no assertion about rules or activation intervals before RegisteredAt.
type AlarmRuleRevision struct {
	ID                  string    `json:"id"`
	TenantID            string    `json:"tenantId"`
	RuleID              string    `json:"ruleId"`
	Version             int       `json:"version"`
	Hash                string    `json:"hash"`
	Rule                AlarmRule `json:"rule"`
	Reason              string    `json:"reason"`
	Actor               string    `json:"actor"`
	RegisteredAt        int64     `json:"registeredAt"`
	InitialRegistration bool      `json:"initialRegistration"`
	RollbackFrom        string    `json:"rollbackFrom,omitempty"`
	ExperimentID        string    `json:"experimentId,omitempty"`
	SemanticsVersion    string    `json:"semanticsVersion"`
}

// An activation event is append-only; Until is derived from the next event.
type AlarmRuleActivation struct {
	ID         string `json:"id"`
	TenantID   string `json:"tenantId"`
	RuleID     string `json:"ruleId"`
	RevisionID string `json:"revisionId"`
	Version    int    `json:"version"`
	Since      int64  `json:"since"`
	Until      *int64 `json:"until,omitempty"`
	Deleted    bool   `json:"deleted"`
	Actor      string `json:"actor"`
	Reason     string `json:"reason"`
}

type RulePublishRequest struct {
	Rule                    AlarmRule `json:"rule"`
	ExpectedBaselineVersion int       `json:"expectedBaselineVersion"`
	Reason                  string    `json:"reason"`
	Actor                   string    `json:"actor"`
	ExperimentID            string    `json:"experimentId,omitempty"`
	RollbackFrom            string    `json:"rollbackFrom,omitempty"`
	SemanticsVersion        string    `json:"semanticsVersion"`
	Delete                  bool      `json:"delete"`
}

func RuleBodyHash(rule AlarmRule) string {
	raw, _ := json.Marshal(rule)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
func RuleRevisionID(rule AlarmRule) string {
	hash := sha256.Sum256([]byte(rule.TenantID + "\x00" + rule.ID + "\x00" + RuleBodyHash(rule)))
	return "rule_revision_" + hex.EncodeToString(hash[:])
}
func ValidateRulePublish(v RulePublishRequest) error {
	if v.Rule.TenantID == "" || v.Rule.ID == "" || v.ExpectedBaselineVersion < 0 || strings.TrimSpace(v.Reason) == "" || len(v.Reason) > 2000 || len(v.Actor) > 200 || v.SemanticsVersion == "" {
		return ErrRuleHistoryInvalid
	}
	return nil
}

type RuleStageTimes struct {
	DurationAtSeconds *int64 `json:"durationAtSeconds,omitempty"`
	RaiseAtMillis     *int64 `json:"raiseAtMillis,omitempty"`
	RecoverAtMillis   *int64 `json:"recoverAtMillis,omitempty"`
	ComponentAtMillis *int64 `json:"componentAtMillis,omitempty"`
}
type RuleDurationState struct {
	Since  int64 `json:"since"`
	Exists bool  `json:"exists"`
}
type RuleActionIntent struct {
	Action      RuleAction `json:"action"`
	RuleID      string     `json:"ruleId"`
	AlarmID     string     `json:"alarmId"`
	DeviceID    string     `json:"deviceId"`
	MessageID   string     `json:"messageId"`
	TriggeredAt int64      `json:"triggeredAt"`
}
type RuleEvaluationState struct {
	Pending             RuleDurationState  `json:"pending"`
	Alarm               Alarm              `json:"alarm"`
	AlarmVersion        int64              `json:"alarmVersion"`
	RecoveryRevision    *AlarmRuleRevision `json:"recoveryRevision,omitempty"`
	InitialStateQuality string             `json:"initialStateQuality"`
}
type RuleEvaluationStep struct {
	ReportAlarm       *Alarm              `json:"-"`
	Sequence          int                 `json:"sequence"`
	RuleRevisionID    string              `json:"ruleRevisionId"`
	SemanticsVersion  string              `json:"semanticsVersion"`
	Before            RuleEvaluationState `json:"before"`
	Covered           bool                `json:"covered"`
	Matched           bool                `json:"matched"`
	EvaluationError   string              `json:"evaluationError,omitempty"`
	RecoveryMatched   bool                `json:"recoveryMatched"`
	DurationSatisfied bool                `json:"durationSatisfied"`
	Pending           RuleDurationState   `json:"pending"`
	PendingMutation   string              `json:"pendingMutation"`
	RuleAlarmHandled  bool                `json:"ruleAlarmHandled"`
	Alarm             Alarm               `json:"alarm"`
	AlarmVersion      int64               `json:"alarmVersion"`
	WriteAlarm        bool                `json:"writeAlarm"`
	Created           bool                `json:"created"`
	Event             string              `json:"event,omitempty"`
	Actions           []RuleActionIntent  `json:"actions"`
	Times             RuleStageTimes      `json:"times"`
	CommittedAt       int64               `json:"committedAt"`
}
type RuleTraceBinding struct {
	TenantID   string `json:"tenantId"`
	MessageID  string `json:"messageId"`
	ClaimToken int64  `json:"claimToken"`
}
type RuleRoutingTrace struct {
	DeviceAssertion     bool                `json:"deviceAssertion"`
	RuleAlarmHandled    bool                `json:"ruleAlarmHandled"`
	HasComponents       bool                `json:"hasComponents"`
	UntracedSideEffects bool                `json:"untracedSideEffects"`
	Committed           bool                `json:"committed"`
	ExpectedComponents  int                 `json:"expectedComponents"`
	ExpectedDirectRaise bool                `json:"expectedDirectRaise"`
	Initial             RuleRoutingSnapshot `json:"initial"`
	Final               RuleRoutingSnapshot `json:"final"`
}

type RuleRoutingAlarm struct {
	Alarm   Alarm `json:"alarm"`
	Version int64 `json:"version"`
}
type RuleRoutingComponent struct {
	Watermark ComponentAlarmState `json:"watermark"`
	Lifecycle RuleRoutingAlarm    `json:"lifecycle"`
}
type RuleRoutingSnapshot struct {
	Direct     map[string]RuleRoutingAlarm     `json:"direct"`
	Components map[string]RuleRoutingComponent `json:"components"`
}
type RuleRoutingStep struct {
	Kind              string               `json:"kind"`
	RuleID            string               `json:"ruleId"`
	Before            RuleRoutingAlarm     `json:"before"`
	After             RuleRoutingAlarm     `json:"after"`
	PreviousWatermark *ComponentAlarmState `json:"previousWatermark,omitempty"`
	Watermark         *ComponentAlarmState `json:"watermark,omitempty"`
	Times             RuleStageTimes       `json:"times"`
	Applied           bool                 `json:"applied"`
	Event             string               `json:"event,omitempty"`
	CommittedAt       int64                `json:"committedAt"`
}
type ruleTraceContextKey struct{}

func WithRuleTraceBinding(ctx context.Context, binding RuleTraceBinding) context.Context {
	return context.WithValue(ctx, ruleTraceContextKey{}, binding)
}
func RuleTraceBindingFrom(ctx context.Context) (RuleTraceBinding, bool) {
	binding, ok := ctx.Value(ruleTraceContextKey{}).(RuleTraceBinding)
	return binding, ok
}

type RuleEvaluationTrace struct {
	ID string `json:"id"`
	RuleTraceBinding
	RawMessageID        string               `json:"rawMessageId"`
	DeviceID            string               `json:"deviceId"`
	ProductID           string               `json:"productId"`
	MessageTimestamp    int64                `json:"messageTimestamp"`
	MessageHash         string               `json:"messageHash"`
	ClaimOwner          string               `json:"claimOwner"`
	Attempt             int64                `json:"attempt"`
	RuleSetHash         string               `json:"ruleSetHash"`
	Rules               []AlarmRuleRevision  `json:"rules"`
	SemanticsVersion    string               `json:"semanticsVersion"`
	Steps               []RuleEvaluationStep `json:"steps"`
	Routing             RuleRoutingTrace     `json:"routing"`
	RoutingSteps        []RuleRoutingStep    `json:"routingSteps"`
	StartedAt           int64                `json:"startedAt"`
	FinishedAt          int64                `json:"finishedAt"`
	Status              string               `json:"status"`
	ReproductionQuality string               `json:"reproductionQuality"`
	Limitations         []string             `json:"limitations"`
}
type RuleTraceFilter struct {
	DeviceIDs     []string
	MessageIDs    []string
	Start, End    int64
	Limit, Offset int
}

func RuleTraceID(binding RuleTraceBinding) string {
	raw, _ := json.Marshal(binding)
	hash := sha256.Sum256(raw)
	return "rule_trace_" + hex.EncodeToString(hash[:])
}
func RuleSetHash(rules []AlarmRuleRevision) string {
	type member struct {
		ID   string
		Hash string
	}
	values := make([]member, len(rules))
	for i, rule := range rules {
		values[i] = member{rule.ID, rule.Hash}
	}
	raw, _ := json.Marshal(values)
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}
