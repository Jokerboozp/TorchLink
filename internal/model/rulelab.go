package model

import "encoding/json"

const (
	RuleLabDatasetKind    = "RULE_LAB_DATASET"
	RuleLabExperimentKind = "RULE_LAB_EXPERIMENT"
	RuleLabLabelKind      = "RULE_LAB_LABEL"
)

// Dataset metadata is immutable after creation. Its queued run owns immutable
// input chunks; the dataset never resolves against the current source on read.
type RuleLabDatasetRequest struct {
	Scope              string   `json:"scope,omitempty"`
	DeviceIDs          []string `json:"deviceIds"`
	Start              int64    `json:"start"`
	End                int64    `json:"end"`
	WarmupStart        int64    `json:"warmupStart"`
	TimeBasis          string   `json:"timeBasis"`
	ClockPolicy        string   `json:"clockPolicy"`
	SemanticsVersion   string   `json:"semanticsVersion"`
	InitialStatePolicy string   `json:"initialStatePolicy"`
	IdempotencyKey     string   `json:"idempotencyKey"`
}
type RuleLabDataset struct {
	ID    string `json:"id"`
	RunID string `json:"runId"`
	RuleLabDatasetRequest
	Status              string                   `json:"status"`
	InputCount          int                      `json:"inputCount"`
	FactsHash           string                   `json:"factsHash"`
	Coverage            []AnalysisSourceCoverage `json:"coverage"`
	InitialStateQuality string                   `json:"initialStateQuality"`
	ReproductionQuality string                   `json:"reproductionQuality"`
	Limitations         []string                 `json:"limitations"`
}
type RuleLabInputQuery struct {
	DeviceIDs          []string `json:"deviceIds"`
	Start              int64    `json:"start"`
	End                int64    `json:"end"`
	TimeBasis          string   `json:"timeBasis"`
	MessageIDs         []string `json:"messageIds,omitempty"`
	Cursor             string   `json:"cursor,omitempty"`
	Limit              int      `json:"limit,omitempty"`
	AvailabilitySource string   `json:"-"`
}

// Whole standard messages retain event/direct/component assertions and all
// decoder outputs. Missing historical metadata is never filled from a current
// product or parser. AvailableAt identifies this source's actual storage ACK.
type RuleLabInput struct {
	ID                   string                `json:"id"`
	Message              StandardMessage       `json:"message"`
	ReceivedAt           int64                 `json:"receivedAt"`
	AvailableAt          int64                 `json:"availableAt"`
	AvailableAtSource    string                `json:"availableAtSource"`
	ProtocolVersion      string                `json:"protocolVersion"`
	PointTableVersion    string                `json:"pointTableVersion"`
	ConfigurationVersion string                `json:"configurationVersion"`
	Units                map[string]string     `json:"units"`
	MetadataQuality      string                `json:"metadataQuality"`
	Hash                 string                `json:"hash"`
	Traces               []RuleEvaluationTrace `json:"traces"`
}
type RuleLabConfigRequest struct {
	ResourceID      string          `json:"resourceId"`
	ExpectedVersion int64           `json:"expectedVersion"`
	Scope           string          `json:"scope"`
	DeviceIDs       []string        `json:"deviceIds"`
	Body            json.RawMessage `json:"body"`
}
type RuleLabEvaluationPolicy struct {
	Version                 string `json:"version"`
	ToleranceMs             int64  `json:"toleranceMs"`
	MatchTimeBasis          string `json:"matchTimeBasis"`
	ExtraCyclePolicy        string `json:"extraCyclePolicy"`
	Split                   string `json:"split"`
	RepresentativeConfirmed bool   `json:"representativeConfirmed"`
	RepresentativenessBasis string `json:"representativenessBasis"`
	HoldoutResourceID       string `json:"holdoutResourceId,omitempty"`
}
type RuleLabExperiment struct {
	DatasetID           string                  `json:"datasetId"`
	BaselineRevisionIDs []string                `json:"baselineRevisionIds"`
	BaselinePolicy      string                  `json:"baselinePolicy"`
	CandidateRuleID     string                  `json:"candidateRuleId"`
	Candidate           AlarmRule               `json:"candidate"`
	CandidateEnabled    bool                    `json:"candidateEnabled"`
	Hypothesis          string                  `json:"hypothesis"`
	LabelRevisionIDs    []string                `json:"labelRevisionIds"`
	EvaluationPolicy    RuleLabEvaluationPolicy `json:"evaluationPolicy"`
}
type RuleLabLabel struct {
	DeviceID    string `json:"deviceId"`
	EventType   string `json:"eventType"`
	Start       int64  `json:"start"`
	End         int64  `json:"end"`
	Conclusion  string `json:"conclusion"`
	Basis       string `json:"basis"`
	Status      string `json:"status"`
	ConfirmedBy string `json:"confirmedBy,omitempty"`
	ConfirmedAt int64  `json:"confirmedAt,omitempty"`
}
type RuleLabRunParameters struct {
	Phase                string                 `json:"phase"`
	DatasetID            string                 `json:"datasetId"`
	ExperimentRevisionID string                 `json:"experimentRevisionId,omitempty"`
	DatasetSelection     *RuleLabDatasetRequest `json:"datasetSelection,omitempty"`
}
type RuleLabRunRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
}
type RuleLabOutcome struct {
	ID                  string   `json:"id"`
	Branch              string   `json:"branch"`
	Category            string   `json:"category"`
	DeviceID            string   `json:"deviceId"`
	RuleID              string   `json:"ruleId"`
	AlarmType           string   `json:"alarmType"`
	CreatedRuleRevision string   `json:"createdRuleRevision"`
	TriggerRuleRevision string   `json:"triggerRuleRevision"`
	TriggerMessageID    string   `json:"triggerMessageId"`
	RawMessageID        string   `json:"rawMessageId"`
	TriggeredAt         int64    `json:"triggeredAt"`
	TriggerEventAt      int64    `json:"triggerEventAt"`
	RecoveredAt         int64    `json:"recoveredAt,omitempty"`
	RecoveredEventAt    int64    `json:"recoveredEventAt,omitempty"`
	DurationMs          *int64   `json:"durationMs,omitempty"`
	LastTriggeredAt     int64    `json:"lastTriggeredAt"`
	TriggerCount        int64    `json:"triggerCount"`
	Status              string   `json:"status"`
	IntentActionCount   int      `json:"intentActionCount"`
	IntentActionTypes   []string `json:"intentActionTypes"`
	InitialStateQuality string   `json:"initialStateQuality"`
	Uncertainties       []string `json:"uncertainties"`
	EvidenceIDs         []string `json:"evidenceIds"`
}
type RuleLabEventMatch struct {
	ID           string `json:"id"`
	BaselineID   string `json:"baselineId,omitempty"`
	CandidateID  string `json:"candidateId,omitempty"`
	LabelID      string `json:"labelId,omitempty"`
	DifferenceMs int64  `json:"differenceMs"`
	Kind         string `json:"kind"`
}
