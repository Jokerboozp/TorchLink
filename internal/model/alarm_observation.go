package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AlarmObservation is an immutable incoming source fact, independent of the
// mutable platform alarm lifecycle. All times are UTC Unix milliseconds.
type AlarmObservation struct {
	ID                 string         `json:"id"`
	TenantID           string         `json:"tenantId"`
	SourceSystem       string         `json:"sourceSystem"`
	SourceEventID      string         `json:"sourceEventId"`
	EventIndex         string         `json:"eventIndex"`
	RawMessageID       string         `json:"rawMessageId,omitempty"`
	StandardMessageID  string         `json:"standardMessageId,omitempty"`
	DeviceID           string         `json:"deviceId"`
	ComponentID        string         `json:"componentId,omitempty"`
	AssetInstanceID    string         `json:"assetInstanceId,omitempty"`
	AlarmType          string         `json:"alarmType"`
	OriginKind         string         `json:"originKind"`
	SignalKey          string         `json:"signalKey"`
	FactKind           string         `json:"factKind"`
	EventAt            int64          `json:"eventAt"`
	TimeQuality        string         `json:"timeQuality"`
	ReceivedAt         int64          `json:"receivedAt,omitempty"`
	RecordedAt         int64          `json:"recordedAt"`
	AvailableAt        int64          `json:"availableAt,omitempty"`
	EvaluationAt       int64          `json:"evaluationAt,omitempty"`
	AvailabilitySource string         `json:"availabilitySource,omitempty"`
	RuleID             string         `json:"ruleId,omitempty"`
	RuleVersion        int            `json:"ruleVersion,omitempty"`
	ConditionHash      string         `json:"conditionHash,omitempty"`
	ProtocolVersion    string         `json:"protocolVersion,omitempty"`
	SourceInputHash    string         `json:"sourceInputHash"`
	SourceContentHash  string         `json:"sourceContentHash"`
	IdentityQuality    string         `json:"identityQuality"`
	Acceptance         string         `json:"acceptance"`
	Reason             string         `json:"reason,omitempty"`
	AlarmID            string         `json:"alarmId,omitempty"`
	WatermarkAt        int64          `json:"watermarkAt,omitempty"`
	HistoricalQuality  string         `json:"historicalQuality"`
	Payload            map[string]any `json:"payload,omitempty"`
}

type AlarmObservationConflict struct {
	ID            string           `json:"id"`
	TenantID      string           `json:"tenantId"`
	ObservationID string           `json:"observationId,omitempty"`
	Incoming      AlarmObservation `json:"incoming"`
	Reason        string           `json:"reason"`
	RecordedAt    int64            `json:"recordedAt"`
}

type AlarmObservationAttempt struct {
	ObservationID string `json:"observationId"`
	Acceptance    string `json:"acceptance"`
	Reason        string `json:"reason,omitempty"`
	AlarmID       string `json:"alarmId,omitempty"`
	EvaluationAt  int64  `json:"evaluationAt,omitempty"`
	RecordedAt    int64  `json:"recordedAt"`
}

func ObservationHash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// SlotKey deliberately excludes the signal value: ASSERT -> CLEAR under the
// same identity is a conflict, never a second legal observation.
func (o AlarmObservation) SlotKey() string {
	return ObservationHash([]string{o.TenantID, o.SourceSystem, o.SourceEventID, o.DeviceID, o.ComponentID, o.OriginKind, o.SignalKey, o.AlarmType, o.EventIndex})
}
func (o AlarmObservation) InputKey() string {
	return ObservationHash([]string{o.TenantID, o.SourceSystem, o.SourceEventID})
}
func (o *AlarmObservation) Normalize() error {
	if o.TenantID == "" || o.DeviceID == "" || o.SourceSystem == "" || o.SourceEventID == "" || o.SignalKey == "" || o.AlarmType == "" {
		return fmt.Errorf("alarm observation source identity and signal are required")
	}
	if o.FactKind != "ASSERT" && o.FactKind != "CLEAR" && o.FactKind != "REPORT" && o.FactKind != "UNKNOWN" {
		return fmt.Errorf("invalid alarm observation fact kind")
	}
	if o.RecordedAt == 0 {
		o.RecordedAt = time.Now().UnixMilli()
	}
	if o.TimeQuality == "" {
		o.TimeQuality = "UNVERIFIED"
	}
	if o.IdentityQuality == "" {
		o.IdentityQuality = "PLATFORM_INPUT"
	}
	if o.HistoricalQuality == "" {
		o.HistoricalQuality = "LIVE"
	}
	if o.Acceptance == "" {
		o.Acceptance = "ACCEPTED"
	}
	if o.SourceInputHash == "" {
		o.SourceInputHash = ObservationHash(o.Payload)
	}
	if o.SourceContentHash == "" {
		o.SourceContentHash = ObservationHash(struct {
			Kind                     string
			At                       int64
			Quality, Input, Protocol string
			RuleVersion              int
			Condition                string
		}{o.FactKind, o.EventAt, o.TimeQuality, o.SourceInputHash, o.ProtocolVersion, o.RuleVersion, o.ConditionHash})
	}
	if o.ID == "" {
		o.ID = "aobs_" + o.SlotKey()[:32]
	}
	return nil
}

type alarmObservationContextKey struct{}

func WithAlarmObservation(ctx context.Context, o AlarmObservation) context.Context {
	return context.WithValue(ctx, alarmObservationContextKey{}, o)
}
func AlarmObservationFromContext(ctx context.Context) (AlarmObservation, bool) {
	o, ok := ctx.Value(alarmObservationContextKey{}).(AlarmObservation)
	return o, ok
}

// AlarmObservationCapture belongs to one synchronous rule calculation. The
// executor prepares provenance before storage locks; the atomic callback fills
// the actual branch/time, and storage persists it beside the production step.
type AlarmObservationCapture struct{ Observation *AlarmObservation }
type alarmObservationCaptureKey struct{}

func WithAlarmObservationCapture(ctx context.Context, capture *AlarmObservationCapture) context.Context {
	return context.WithValue(ctx, alarmObservationCaptureKey{}, capture)
}
func AlarmObservationCaptureFromContext(ctx context.Context) (*AlarmObservationCapture, bool) {
	v, ok := ctx.Value(alarmObservationCaptureKey{}).(*AlarmObservationCapture)
	return v, ok
}

// AlarmObservationFromAlarm retains incoming Details even when the repository
// aggregates this report into an older alarm. Legacy synthetic callers remain
// explicitly source-unknown and cannot yield reliable signal cycles.
func AlarmObservationFromAlarm(ctx context.Context, a Alarm) AlarmObservation {
	if o, ok := AlarmObservationFromContext(ctx); ok {
		return o
	}
	o := AlarmObservation{TenantID: a.TenantID, DeviceID: a.DeviceID, ComponentID: a.ComponentID, AlarmType: a.AlarmType, SignalKey: "rule:" + a.RuleID, OriginKind: "RULE_LIFECYCLE", FactKind: "ASSERT", EventAt: a.LastTriggeredAt, EvaluationAt: a.LastTriggeredAt, TimeQuality: "UNVERIFIED", SourceSystem: "PLATFORM_ALARM", SourceEventID: a.TriggerID, Payload: a.Details, HistoricalQuality: "PARTIAL"}
	if o.SourceEventID == "" {
		o.SourceEventID = a.ID
		o.IdentityQuality = "SOURCE_UNKNOWN"
	}
	if strings.HasPrefix(a.RuleID, "__device_alarm__:") || strings.HasPrefix(a.RuleID, "device-report:") || a.Source == "device" && a.Details["direct"] == true {
		o.OriginKind = "DEVICE_DIRECT"
		o.SignalKey = "device:" + a.AlarmType
		o.FactKind = "REPORT"
	}
	if a.ComponentID != "" {
		o.OriginKind = "COMPONENT_STATE"
		o.SignalKey = "component:" + a.ComponentID + ":" + a.AlarmType
	}
	if m, ok := a.Details["message"]; ok {
		b, _ := json.Marshal(m)
		var msg StandardMessage
		if json.Unmarshal(b, &msg) == nil {
			o.StandardMessageID = msg.MessageID
			o.RawMessageID = msg.RawMessageID
			o.EventAt = msg.Timestamp
			o.SourceSystem = "STANDARD_MESSAGE"
			o.SourceEventID = msg.MessageID
			if o.SourceEventID == "" {
				o.SourceEventID = msg.RawMessageID
			}
			o.SourceInputHash = ObservationHash(msg)
		}
	}
	if o.DeviceID == "" {
		o.DeviceID = "UNKNOWN"
	}
	if o.AlarmType == "" {
		o.AlarmType = "UNKNOWN"
	}
	return o
}

// AlarmObservationFromComponent uses the incoming component watermark as the
// identity for synthetic repository callers. Their candidate may be the old
// aggregate alarm and retain its previous trigger and message details.
func AlarmObservationFromComponent(ctx context.Context, a Alarm, state ComponentAlarmState) AlarmObservation {
	o := AlarmObservationFromAlarm(ctx, a)
	if _, explicit := AlarmObservationFromContext(ctx); !explicit && state.MessageID != "" {
		if o.SourceEventID != state.MessageID {
			o.SourceSystem = "PLATFORM_COMPONENT"
			o.RawMessageID = ""
			o.SourceInputHash = ""
		}
		o.SourceEventID = state.MessageID
		o.StandardMessageID = state.MessageID
	}
	return o
}

// AlarmCycleRevision is an immutable projection, never a replacement for the
// production Alarm. Unknown boundaries retain zero timestamps and false flags.
type AlarmCycleRevision struct {
	ID                      string   `json:"id"`
	TenantID                string   `json:"tenantId"`
	DeviceID                string   `json:"deviceId"`
	ComponentID             string   `json:"componentId,omitempty"`
	AssetInstanceID         string   `json:"assetInstanceId,omitempty"`
	AlarmType               string   `json:"alarmType"`
	OriginKind              string   `json:"originKind"`
	SignalKey               string   `json:"signalKey"`
	Method                  string   `json:"method"`
	Status                  string   `json:"status"`
	BoundaryQuality         string   `json:"boundaryQuality"`
	TimeBasis               string   `json:"timeBasis"`
	StartAt                 int64    `json:"startAt,omitempty"`
	EndAt                   int64    `json:"endAt,omitempty"`
	StartKnown              bool     `json:"startKnown"`
	EndKnown                bool     `json:"endKnown"`
	NewStart                bool     `json:"newStart"`
	ObservationIDs          []string `json:"observationIds"`
	AlarmIDs                []string `json:"alarmIds"`
	DurationMillis          *int64   `json:"durationMillis,omitempty"`
	KnownDurationLowerBound *int64   `json:"knownDurationLowerBound,omitempty"`
	AlgorithmVersion        string   `json:"algorithmVersion"`
	ConfigVersion           string   `json:"configVersion"`
	InputHash               string   `json:"inputHash"`
	SupersedesID            string   `json:"supersedesId,omitempty"`
	Limitations             []string `json:"limitations"`
}

func (o AlarmObservation) TimeAt(basis string) int64 {
	switch basis {
	case "RECEIVED_AT":
		return o.ReceivedAt
	case "EVALUATION_AT":
		return o.EvaluationAt
	case "RECORDED_AT":
		return o.RecordedAt
	default:
		return o.EventAt
	}
}
func (o AlarmObservation) SignalWatermarkAt() int64 {
	if o.OriginKind == "RULE_LIFECYCLE" {
		return o.EvaluationAt
	}
	return o.EventAt
}

// Each observation clock has its own dependency namespace. An out-of-window
// event arriving now must not invalidate a frozen event-time analysis.
func ObservationSourceVersions(o AlarmObservation, conservative bool) []GovernanceSourceVersion {
	point := GovernancePoint{DeviceID: o.DeviceID, ComponentID: o.ComponentID, AlarmType: o.AlarmType, OriginKind: o.OriginKind, SignalKey: o.SignalKey}
	out := []GovernanceSourceVersion{}
	views := []struct {
		kind    string
		at      int64
		unknown bool
	}{
		{"OBSERVATION", o.EventAt, o.TimeQuality != "TRUSTED" || o.HistoricalQuality != "LIVE"},
		{"OBSERVATION_RECEIVED", o.ReceivedAt, false},
		{"OBSERVATION_EVALUATION", o.EvaluationAt, o.HistoricalQuality != "LIVE"},
	}
	for _, view := range views {
		keys := []string{point.Key(view.kind), (GovernancePoint{DeviceID: o.DeviceID}).Key(view.kind)}
		buckets := []int64{}
		if view.at > 0 {
			buckets = append(buckets, view.at/86400000*86400000)
		}
		if conservative || view.at <= 0 || view.unknown {
			buckets = append(buckets, -1)
		}
		for _, dep := range keys {
			for _, b := range buckets {
				out = append(out, GovernanceSourceVersion{DependencyKey: dep, BucketStart: b})
			}
		}
	}
	return out
}
