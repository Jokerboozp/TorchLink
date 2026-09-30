package model

// Response timestamps use UTC Unix milliseconds. Procedures and evaluations
// are immutable versions; a source correction never edits an old evaluation.
type ResponseProcedure struct {
	Name        string                   `json:"name"`
	Scenario    string                   `json:"scenario"`
	Steps       []ResponseStep           `json:"steps"`
	Published   bool                     `json:"published"`
	PublishedBy string                   `json:"publishedBy,omitempty"`
	PublishedAt int64                    `json:"publishedAt,omitempty"`
	Requests    []ResponseRequestReceipt `json:"requests"`
}
type ResponseStep struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Role             string   `json:"role"`
	Required         bool     `json:"required"`
	Prerequisites    []string `json:"prerequisites"`
	RequiredEvidence []string `json:"requiredEvidence"`
	ClockStart       string   `json:"clockStart"` // RUN_START, PLATFORM_RECEIVED, or a step ID
	TargetMs         int64    `json:"targetMs,omitempty"`
	SystemEventType  string   `json:"systemEventType,omitempty"`
}
type ResponsePerson struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}
type ResponseExecution struct {
	Name                string                      `json:"name"`
	Source              string                      `json:"source"` // DRILL or REAL_CASE; assigned by the service
	Status              string                      `json:"status"`
	ProcedureRevisionID string                      `json:"procedureRevisionId"`
	Procedure           ResponseProcedure           `json:"procedure"`
	People              []ResponsePerson            `json:"people"`
	PlannedStart        int64                       `json:"plannedStart,omitempty"`
	PlannedEnd          int64                       `json:"plannedEnd,omitempty"`
	StartedAt           int64                       `json:"startedAt,omitempty"`
	EndedAt             int64                       `json:"endedAt,omitempty"`
	AlarmID             string                      `json:"alarmId,omitempty"`
	AlarmReportEventID  string                      `json:"alarmReportEventId,omitempty"`
	PlatformReceivedAt  int64                       `json:"platformReceivedAt,omitempty"`
	Milestones          []ResponseMilestone         `json:"milestones"`
	SimulationEvents    []ResponseSimulationEvent   `json:"simulationEvents"`
	SampleAssociations  []ResponseSampleAssociation `json:"sampleAssociations"`
	Confirmations       []ResponseConfirmation      `json:"confirmations"`
	Actions             []ResponseActionEvent       `json:"actions"`
	Requests            []ResponseRequestReceipt    `json:"requests"`
}
type ResponseActionEvent struct {
	Action     string `json:"action"`
	Reason     string `json:"reason,omitempty"`
	Actor      string `json:"actor"`
	OccurredAt int64  `json:"occurredAt,omitempty"`
	RecordedAt int64  `json:"recordedAt"`
}
type ResponseRequestReceipt struct {
	Key      string `json:"key"`
	Hash     string `json:"hash"`
	Actor    string `json:"actor"`
	ResultID string `json:"resultId,omitempty"`
}
type ResponseEvidenceReference struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"` // ATTACHMENT, ALARM_LIFECYCLE, RAW_MESSAGE, or MANUAL_RECORD
	SourceID       string `json:"sourceId"`
	DeviceID       string `json:"deviceId"`
	Description    string `json:"description"`
	OccurredAt     int64  `json:"occurredAt,omitempty"`
	RecordedAt     int64  `json:"recordedAt,omitempty"`
	ReceivedAt     int64  `json:"receivedAt,omitempty"`
	ResourceID     string `json:"resourceId,omitempty"`
	EventType      string `json:"eventType,omitempty"`
	Classification string `json:"classification"` // assigned from the trusted source; never changes production processing
}

type ResponseAttachment struct {
	ExecutionID string                   `json:"executionId"`
	Name        string                   `json:"name"`
	ContentType string                   `json:"contentType"`
	Size        int64                    `json:"size"`
	SHA256      string                   `json:"sha256"`
	ObjectKey   string                   `json:"objectKey"`
	RecordedAt  int64                    `json:"recordedAt"`
	Author      string                   `json:"author"`
	Requests    []ResponseRequestReceipt `json:"requests"`
}
type ResponseMilestone struct {
	ID          string                      `json:"id"`
	StepID      string                      `json:"stepId"`
	OccurredAt  int64                       `json:"occurredAt"`
	RecordedAt  int64                       `json:"recordedAt"`
	Executor    string                      `json:"executor"`
	Recorder    string                      `json:"recorder"`
	Source      string                      `json:"source"` // SYSTEM is never accepted from a browser
	Status      string                      `json:"status"` // UNVERIFIED, CONFIRMED, DISPUTED
	Evidence    []ResponseEvidenceReference `json:"evidence"`
	Explanation string                      `json:"explanation,omitempty"`
	CorrectsID  string                      `json:"correctsId,omitempty"`
}
type ResponseSimulationEvent struct {
	ID         string `json:"id"`
	DeviceID   string `json:"deviceId"`
	OccurredAt int64  `json:"occurredAt"`
	RecordedAt int64  `json:"recordedAt"`
	Content    string `json:"content"`
	Source     string `json:"source"` // always EXERCISE; no ingress, parser, bus, or alarm writes
	Recorder   string `json:"recorder"`
}

// Classification is a reviewed association to one exact source record. It
// never replaces the canonical production classification or notification.
type ResponseSampleAssociation struct {
	ID                 string                      `json:"id"`
	Source             ResponseEvidenceReference   `json:"source"`
	Classification     string                      `json:"classification"`
	Status             string                      `json:"status"`
	ContainsProduction bool                        `json:"containsProduction"`
	Basis              string                      `json:"basis"`
	Evidence           []ResponseEvidenceReference `json:"evidence"`
	Reviewer           string                      `json:"reviewer"`
	RecordedAt         int64                       `json:"recordedAt"`
}
type ResponseConfirmation struct {
	RevisionID  string `json:"revisionId"`
	FactsHash   string `json:"factsHash"`
	Conclusion  string `json:"conclusion"`
	Confirmer   string `json:"confirmer"`
	ConfirmedAt int64  `json:"confirmedAt"`
}
type ResponseStepEvaluation struct {
	StepID            string   `json:"stepId"`
	Name              string   `json:"name"`
	Required          bool     `json:"required"`
	Status            string   `json:"status"`
	MilestoneID       string   `json:"milestoneId,omitempty"`
	OccurredAt        int64    `json:"occurredAt,omitempty"`
	RecordedAt        int64    `json:"recordedAt,omitempty"`
	StartAt           int64    `json:"startAt,omitempty"`
	DurationMs        *int64   `json:"durationMs,omitempty"`
	WaitingMs         *int64   `json:"waitingMs,omitempty"`
	TargetMs          int64    `json:"targetMs,omitempty"`
	TargetResult      string   `json:"targetResult"`
	RequiredEvidence  int      `json:"requiredEvidence"`
	ConfirmedEvidence int      `json:"confirmedEvidence"`
	MissingEvidence   []string `json:"missingEvidence"`
	Limitations       []string `json:"limitations"`
}
type ResponseEvaluation struct {
	ExecutionRevisionID    string                   `json:"executionRevisionId"`
	ProcedureRevisionID    string                   `json:"procedureRevisionId"`
	Source                 string                   `json:"source"`
	Cutoff                 int64                    `json:"cutoff"`
	RequiredSteps          int                      `json:"requiredSteps"`
	CompletedRequiredSteps int                      `json:"completedRequiredSteps"`
	RequiredEvidence       int                      `json:"requiredEvidence"`
	ConfirmedEvidence      int                      `json:"confirmedEvidence"`
	CompletionRate         *float64                 `json:"completionRate,omitempty"`
	EvidenceCoverage       *float64                 `json:"evidenceCoverage,omitempty"`
	Steps                  []ResponseStepEvaluation `json:"steps"`
	Limitations            []string                 `json:"limitations"`
}
type CorrectiveAction struct {
	Title                string                   `json:"title"`
	ReviewRevisionID     string                   `json:"reviewRevisionId"`
	ReviewFactsHash      string                   `json:"reviewFactsHash"`
	FindingID            string                   `json:"findingId"`
	Owner                string                   `json:"owner"`
	DueAt                int64                    `json:"dueAt"`
	Status               string                   `json:"status"`
	RequiredVerification []string                 `json:"requiredVerification"`
	Handling             []CorrectiveHandling     `json:"handling"`
	Verifications        []CorrectiveVerification `json:"verifications"`
	FollowUpExecutionIDs []string                 `json:"followUpExecutionIds"`
	Requests             []ResponseRequestReceipt `json:"requests"`
}
type CorrectiveHandling struct {
	ID          string                      `json:"id"`
	Actor       string                      `json:"actor"`
	RecordedAt  int64                       `json:"recordedAt"`
	Explanation string                      `json:"explanation"`
	Evidence    []ResponseEvidenceReference `json:"evidence"`
}
type CorrectiveVerification struct {
	ID          string                      `json:"id"`
	Actor       string                      `json:"actor"`
	RecordedAt  int64                       `json:"recordedAt"`
	Result      string                      `json:"result"`
	Items       []string                    `json:"items"`
	Explanation string                      `json:"explanation"`
	Evidence    []ResponseEvidenceReference `json:"evidence"`
}
type DutyActionLink struct {
	SourceKind       string   `json:"sourceKind"`
	SourceResourceID string   `json:"sourceResourceId"`
	RunID            string   `json:"runId"`
	StationID        string   `json:"stationId"`
	DeviceIDs        []string `json:"deviceIds"`
	DutyItemID       string   `json:"dutyItemId"`
	SourceVersion    int64    `json:"sourceVersion"`
	LinkedBy         string   `json:"linkedBy"`
	LinkedAt         int64    `json:"linkedAt"`
}
