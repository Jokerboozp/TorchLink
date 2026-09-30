package model

import "encoding/json"

type MaintenanceRevisionRequest struct {
	ResourceID      string          `json:"resourceId"`
	ExpectedVersion int64           `json:"expectedVersion"`
	DeviceIDs       []string        `json:"deviceIds"`
	IdempotencyKey  string          `json:"idempotencyKey"`
	Body            json.RawMessage `json:"body"`
}
type MaintenanceActionRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	IdempotencyKey  string `json:"idempotencyKey"`
	Action          string `json:"action"`
	Reason          string `json:"reason"`
	At              int64  `json:"at,omitempty"`
}
type MaintenanceFaultAssessment struct {
	AlarmID        string                      `json:"alarmId"`
	DeviceID       string                      `json:"deviceId"`
	ComponentID    string                      `json:"componentId,omitempty"`
	Type           string                      `json:"type"`
	FaultCode      string                      `json:"faultCode,omitempty"`
	Classification string                      `json:"classification"`
	Basis          string                      `json:"basis"`
	Status         string                      `json:"status"`
	ConfirmedBy    string                      `json:"confirmedBy,omitempty"`
	ConfirmedAt    int64                       `json:"confirmedAt,omitempty"`
	Evidence       []ResponseEvidenceReference `json:"evidence"`
	Requests       []ResponseRequestReceipt    `json:"requests"`
}
type MaintenanceVerificationRequest struct {
	ExpectedVersion int64                   `json:"expectedVersion"`
	IdempotencyKey  string                  `json:"idempotencyKey"`
	Verification    MaintenanceVerification `json:"verification"`
}
type MaintenanceObservationRequest struct {
	ExpectedVersion int64                            `json:"expectedVersion"`
	IdempotencyKey  string                           `json:"idempotencyKey"`
	Parameters      MaintenanceObservationParameters `json:"parameters"`
	UseFinance      bool                             `json:"useFinance"`
}
type MaintenanceRunParameters struct {
	Observation        *MaintenanceObservationParameters `json:"observation,omitempty"`
	ScenarioRevisionID string                            `json:"scenarioRevisionId,omitempty"`
	UseFinance         bool                              `json:"useFinance"`
}
type InvestmentEvaluationRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	IdempotencyKey  string `json:"idempotencyKey"`
	UseFinance      bool   `json:"useFinance"`
}
type InvestmentDecisionRequest struct {
	ExpectedVersion      int64    `json:"expectedVersion"`
	IdempotencyKey       string   `json:"idempotencyKey"`
	EvaluationRunID      string   `json:"evaluationRunId"`
	SelectedCandidateIDs []string `json:"selectedCandidateIds"`
	Reason               string   `json:"reason"`
}
type InvestmentAdjustmentRequest struct {
	ExpectedVersion int64    `json:"expectedVersion"`
	IdempotencyKey  string   `json:"idempotencyKey"`
	CandidateOrder  []string `json:"candidateOrder"`
	Reason          string   `json:"reason"`
}
type MaintenanceAdmissionRecord struct {
	ProductID  string                   `json:"productId,omitempty"`
	FaultType  string                   `json:"faultType"`
	Parameters MaintenanceAdmission     `json:"parameters"`
	Requests   []ResponseRequestReceipt `json:"requests"`
}
type MaintenanceAttachment struct {
	DeviceIDs   []string                 `json:"deviceIds"`
	Name        string                   `json:"name"`
	ContentType string                   `json:"contentType"`
	Size        int64                    `json:"size"`
	SHA256      string                   `json:"sha256"`
	ObjectKey   string                   `json:"objectKey"`
	Author      string                   `json:"author"`
	RecordedAt  int64                    `json:"recordedAt"`
	Requests    []ResponseRequestReceipt `json:"requests"`
}
type MaintenanceReviewRequest struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	SnapshotVersion int64  `json:"snapshotVersion"`
	FactsHash       string `json:"factsHash"`
	Result          string `json:"result"`
	Explanation     string `json:"explanation"`
	IdempotencyKey  string `json:"idempotencyKey"`
}
