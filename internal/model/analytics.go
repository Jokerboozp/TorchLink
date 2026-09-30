package model

import (
	"encoding/json"
	"errors"
)

const (
	AnalysisQueued    = "QUEUED"
	AnalysisPreparing = "PREPARING"
	AnalysisRunning   = "RUNNING"
	AnalysisSucceeded = "SUCCEEDED"
	AnalysisPartial   = "PARTIAL"
	AnalysisFailed    = "FAILED"
	AnalysisCancelled = "CANCELLED"
)

var (
	ErrAnalysisConflict  = errors.New("analysis version or idempotency conflict")
	ErrAnalysisLeaseLost = errors.New("analysis lease lost")
	ErrAnalysisQueueFull = errors.New("analysis queue limit exceeded")
	ErrAnalysisInvalid   = errors.New("invalid analysis input")
)

// Analysis timestamps are UTC Unix milliseconds. Every window is [Start,End).
// DeviceIDs is always explicit, including for tenant administrators.
type AnalysisRun struct {
	ID                    string                   `json:"id"`
	TenantID              string                   `json:"tenantId"`
	Kind                  string                   `json:"kind"`
	Creator               string                   `json:"creator"`
	CreatorSessionVersion int64                    `json:"creatorSessionVersion"`
	CreatorManaged        bool                     `json:"creatorManaged"`
	DeviceIDs             []string                 `json:"deviceIds"`
	PermissionsVersion    string                   `json:"permissionsVersion"`
	Start                 int64                    `json:"start"`
	End                   int64                    `json:"end"`
	ConfigurationVersion  string                   `json:"configurationVersion"`
	AlgorithmVersion      string                   `json:"algorithmVersion"`
	Parameters            json.RawMessage          `json:"parameters,omitempty"`
	PreviousRunID         string                   `json:"previousRunId,omitempty"`
	IdempotencyKey        string                   `json:"idempotencyKey"`
	RequestHash           string                   `json:"requestHash"`
	Status                string                   `json:"status"`
	Stage                 string                   `json:"stage"`
	Processed             int64                    `json:"processed"`
	LeaseOwner            string                   `json:"leaseOwner,omitempty"`
	LeaseToken            int64                    `json:"leaseToken"`
	LeaseExpiresAt        int64                    `json:"leaseExpiresAt,omitempty"`
	Checkpoint            json.RawMessage          `json:"checkpoint,omitempty"`
	SnapshotID            string                   `json:"snapshotId,omitempty"`
	InputsFrozen          bool                     `json:"inputsFrozen"`
	InputHashes           []string                 `json:"inputHashes"`
	Sources               []AnalysisSourceCoverage `json:"sources"`
	DataCutoff            int64                    `json:"dataCutoff,omitempty"`
	Error                 string                   `json:"error,omitempty"`
	Version               int64                    `json:"version"`
	CreatedAt             int64                    `json:"createdAt"`
	StartedAt             int64                    `json:"startedAt,omitempty"`
	UpdatedAt             int64                    `json:"updatedAt"`
	CompletedAt           int64                    `json:"completedAt,omitempty"`
}

type AnalysisSourceCoverage struct {
	Source    string `json:"source"`
	Start     int64  `json:"start"`
	End       int64  `json:"end"`
	Complete  bool   `json:"complete"`
	ReadAt    int64  `json:"readAt"`
	Watermark string `json:"watermark,omitempty"`
	Version   string `json:"version,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type AnalysisSnapshot struct {
	ID                  string                   `json:"id"`
	TenantID            string                   `json:"tenantId"`
	RunID               string                   `json:"runId"`
	Version             int64                    `json:"version"`
	DeviceIDs           []string                 `json:"deviceIds"`
	Start               int64                    `json:"start"`
	End                 int64                    `json:"end"`
	DataCutoff          int64                    `json:"dataCutoff"`
	InputHashes         []string                 `json:"inputHashes"`
	Sources             []AnalysisSourceCoverage `json:"sources"`
	InitialStateQuality string                   `json:"initialStateQuality"`
	FactsHash           string                   `json:"factsHash"`
	Statistics          json.RawMessage          `json:"statistics"`
	Limitations         []string                 `json:"limitations"`
	MissingSources      []string                 `json:"missingSources"`
	AffectedIntervals   []AnalysisSourceCoverage `json:"affectedIntervals"`
	UncomputableMetrics []string                 `json:"uncomputableMetrics"`
	CreatedAt           int64                    `json:"createdAt"`
}

type AnalysisEvidence struct {
	ID                   string          `json:"id"`
	TenantID             string          `json:"tenantId"`
	RunID                string          `json:"runId"`
	SnapshotID           string          `json:"snapshotId,omitempty"`
	SourceKind           string          `json:"sourceKind"`
	SourceID             string          `json:"sourceId"`
	DeviceID             string          `json:"deviceId"`
	OccurredAt           int64           `json:"occurredAt"`
	ResourceVersion      string          `json:"resourceVersion"`
	Summary              json.RawMessage `json:"summary"`
	PermissionCategory   string          `json:"permissionCategory"`
	OriginalAvailability string          `json:"originalAvailability"`
	RawMessageID         string          `json:"rawMessageId,omitempty"`
}

// Output stores compact metrics/findings; source time series remain in their
// existing stores. Output IDs are stable across checkpoint retries.
type AnalysisOutput struct {
	ID       string          `json:"id"`
	TenantID string          `json:"tenantId"`
	RunID    string          `json:"runId"`
	Kind     string          `json:"kind"`
	DeviceID string          `json:"deviceId,omitempty"`
	Body     json.RawMessage `json:"body"`
}

type AnalysisBatch struct {
	ID           string                   `json:"id"`
	FreezeInputs bool                     `json:"freezeInputs,omitempty"`
	InputHashes  []string                 `json:"inputHashes,omitempty"`
	Sources      []AnalysisSourceCoverage `json:"sources,omitempty"`
	DataCutoff   int64                    `json:"dataCutoff,omitempty"`
	Checkpoint   json.RawMessage          `json:"checkpoint"`
	Processed    int64                    `json:"processed"`
	Status       string                   `json:"status"`
	Stage        string                   `json:"stage"`
	Error        string                   `json:"error,omitempty"`
	Outputs      []AnalysisOutput         `json:"outputs"`
	Evidence     []AnalysisEvidence       `json:"evidence"`
	Snapshot     *AnalysisSnapshot        `json:"snapshot,omitempty"`
}

type AnalysisAIRevision struct {
	ID                string          `json:"id"`
	TenantID          string          `json:"tenantId"`
	RunID             string          `json:"runId"`
	SnapshotID        string          `json:"snapshotId"`
	SnapshotVersion   int64           `json:"snapshotVersion"`
	WorkflowID        string          `json:"workflowId"`
	Model             string          `json:"model"`
	PromptVersion     string          `json:"promptVersion"`
	Status            string          `json:"status"`
	HarnessRunID      string          `json:"harnessRunId,omitempty"`
	Interpretation    json.RawMessage `json:"interpretation,omitempty"`
	FactIDs           []string        `json:"factIds"`
	Coverage          json.RawMessage `json:"coverage,omitempty"`
	PermissionVersion string          `json:"permissionVersion"`
	Error             string          `json:"error,omitempty"`
	Version           int64           `json:"version"`
	CreatedAt         int64           `json:"createdAt"`
	CompletedAt       int64           `json:"completedAt,omitempty"`
}

type AnalysisReview struct {
	ID              string `json:"id"`
	TenantID        string `json:"tenantId"`
	RunID           string `json:"runId"`
	ResourceID      string `json:"resourceId"`
	ResourceVersion int64  `json:"resourceVersion"`
	Result          string `json:"result"`
	Explanation     string `json:"explanation"`
	Reviewer        string `json:"reviewer"`
	ReviewedAt      int64  `json:"reviewedAt"`
	CorrectsID      string `json:"correctsId,omitempty"`
	IdempotencyKey  string `json:"idempotencyKey"`
}

// Config revisions are immutable; ExpectedVersion guards the latest pointer.
// Shared configuration authorization is checked by the application layer.
type AnalysisConfigRevision struct {
	ID         string          `json:"id"`
	TenantID   string          `json:"tenantId"`
	Kind       string          `json:"kind"`
	ResourceID string          `json:"resourceId"`
	Version    int64           `json:"version"`
	DeviceIDs  []string        `json:"deviceIds"`
	Scope      string          `json:"scope"`
	Creator    string          `json:"creator"`
	Body       json.RawMessage `json:"body"`
	Hash       string          `json:"hash"`
	CreatedAt  int64           `json:"createdAt"`
}

// DeviceScopeSet must be true for user-facing lists. A nil explicit set means
// no devices, never unrestricted access. Trusted worker queries omit it.
type AnalysisFilter struct {
	Kind           string
	RunID          string
	ResourceID     string
	DeviceID       string
	DeviceIDs      []string
	DeviceScopeSet bool
	Statuses       []string
	Limit          int
	Offset         int
}
