package model

import "encoding/json"

const (
	MonitoringProfileKind     = "MONITORING_PROFILE"
	MonitoringObservationKind = "MONITORING_OBSERVATION"
	MonitoringHypothesisKind  = "MONITORING_HYPOTHESIS"
)

type MonitoringAttribute struct {
	ID        string   `json:"id"`
	ValueType string   `json:"valueType"`
	Minimum   *float64 `json:"minimum,omitempty"`
	Maximum   *float64 `json:"maximum,omitempty"`
}
type MonitoringProfile struct {
	TargetType       string                `json:"targetType,omitempty"`
	ProductID        string                `json:"productId,omitempty"`
	EffectiveFrom    int64                 `json:"effectiveFrom"`
	EffectiveTo      int64                 `json:"effectiveTo,omitempty"`
	Mode             string                `json:"mode"`
	Attributes       []MonitoringAttribute `json:"attributes"`
	MessageTypes     []MessageType         `json:"messageTypes"`
	Merge            string                `json:"merge"`
	PeriodMs         int64                 `json:"periodMs"`
	ToleranceMs      int64                 `json:"toleranceMs"`
	Importance       string                `json:"importance,omitempty"`
	LongGapMs        int64                 `json:"longGapMs,omitempty"`
	FrequentGapCount int                   `json:"frequentGapCount,omitempty"`
}
type MonitoringObservation struct {
	DeviceID    string `json:"deviceId"`
	Start       int64  `json:"start"`
	End         int64  `json:"end"`
	Type        string `json:"type"`
	Reason      string `json:"reason"`
	Basis       string `json:"basis"`
	Status      string `json:"status"`
	ConfirmedBy string `json:"confirmedBy,omitempty"`
	ConfirmedAt int64  `json:"confirmedAt,omitempty"`
}
type MonitoringCommonGapPolicy struct {
	Version          string  `json:"version"`
	MinimumGapCount  int     `json:"minimumGapCount"`
	MinimumOverlapMs int64   `json:"minimumOverlapMs"`
	MinimumJaccard   float64 `json:"minimumJaccard"`
}
type MonitoringRunParameters struct {
	ProfileRevisionIDs     []string                   `json:"profileRevisionIds"`
	ObservationRevisionIDs []string                   `json:"observationRevisionIds,omitempty"`
	QualityRunIDs          []string                   `json:"qualityRunIds,omitempty"`
	CommonGapPolicy        *MonitoringCommonGapPolicy `json:"commonGapPolicy,omitempty"`
}
type MonitoringConfigRequest struct {
	ResourceID      string          `json:"resourceId"`
	ExpectedVersion int64           `json:"expectedVersion"`
	Scope           string          `json:"scope"`
	DeviceIDs       []string        `json:"deviceIds"`
	Body            json.RawMessage `json:"body"`
}
type MonitoringHypothesisRequest struct {
	GroupID            string `json:"groupId"`
	At                 int64  `json:"at"`
	ExpectedRunVersion int64  `json:"expectedRunVersion"`
	IdempotencyKey     string `json:"idempotencyKey"`
}
type MonitoringHypothesis struct {
	ID          string   `json:"id"`
	RunID       string   `json:"runId"`
	SnapshotID  string   `json:"snapshotId"`
	FactsHash   string   `json:"factsHash"`
	GroupID     string   `json:"groupId"`
	At          int64    `json:"at"`
	DeviceIDs   []string `json:"deviceIds"`
	CreatedBy   string   `json:"createdBy"`
	CreatedAt   int64    `json:"createdAt"`
	Limitations []string `json:"limitations"`
}
type MonitoringReviewRequest = QualityReviewRequest
