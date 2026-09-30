package model

type GovernanceAttachment struct {
	AvailabilityReason    string   `json:"availabilityReason,omitempty"`
	AvailabilityChangedAt int64    `json:"availabilityChangedAt,omitempty"`
	AvailabilityChangedBy string   `json:"availabilityChangedBy,omitempty"`
	ParentKind            string   `json:"parentKind"`
	ParentID              string   `json:"parentId"`
	DeviceIDs             []string `json:"deviceIds"`
	Name                  string   `json:"name"`
	MIME                  string   `json:"mime"`
	Size                  int64    `json:"size"`
	SHA256                string   `json:"sha256"`
	StorageKey            string   `json:"storageKey,omitempty"`
	UploadedBy            string   `json:"uploadedBy"`
	UploadedAt            int64    `json:"uploadedAt"`
	Source                string   `json:"source"`
	Availability          string   `json:"availability"`
	IdempotencyKey        string   `json:"idempotencyKey"`
}
type GovernanceBusinessLink struct {
	CaseID           string   `json:"caseId"`
	TargetKind       string   `json:"targetKind"`
	TargetID         string   `json:"targetId"`
	TargetVersion    int64    `json:"targetVersion"`
	DeviceIDs        []string `json:"deviceIds"`
	Relation         string   `json:"relation"`
	Summary          string   `json:"summary"`
	SyncStatus       string   `json:"syncStatus"`
	CorrectsID       string   `json:"correctsId,omitempty"`
	CorrectionReason string   `json:"correctionReason,omitempty"`
}

// GovernanceReminder stores minimal task identity; message is generated only
// after current source authorization.
type GovernanceReminder struct {
	TaskKind       string   `json:"taskKind"`
	TaskVersion    int64    `json:"taskVersion"`
	ObservationID  string   `json:"observationId,omitempty"`
	ObservationIDs []string `json:"observationIds,omitempty"`
	FactsHash      string   `json:"factsHash,omitempty"`
	Status         string   `json:"status"`
	CreatedAt      int64    `json:"createdAt"`
	Message        string   `json:"message,omitempty"`
	UserID         string   `json:"userId"`
	CaseID         string   `json:"caseId"`
	RoundID        string   `json:"roundId"`
	Category       string   `json:"category"`
	TaskID         string   `json:"taskId"`
	ScopeKey       string   `json:"scopeKey"`
	DueAt          int64    `json:"dueAt"`
	Count          int      `json:"count"`
	ReadAt         int64    `json:"readAt"`
	HandledAt      int64    `json:"handledAt"`
}
