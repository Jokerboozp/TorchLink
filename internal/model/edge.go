package model

// EdgeReadJob is a read-only diagnostic, bounded by a server deadline. It never
// authorizes device writes or records its sample in the business ingest chain.
type EdgeReadJob struct {
	ID        string       `json:"id"`
	TenantID  string       `json:"tenantId"`
	NodeID    string       `json:"nodeId"`
	Task      EdgeTask     `json:"task"`
	ExpiresAt int64        `json:"expiresAt"`
	Status    string       `json:"status"`
	Token     string       `json:"token,omitempty"`
	Raw       []RawMessage `json:"raw,omitempty"`
	Error     string       `json:"error,omitempty"`
}

type EdgeTask struct {
	Profile DeviceAccessProfile `json:"profile"`
	Release ProtocolRelease     `json:"release"`
}
type EdgeConfiguration struct {
	ExpiresAt int64           `json:"expiresAt"`
	TenantID  string          `json:"tenantId"`
	NodeID    string          `json:"nodeId"`
	Revision  string          `json:"revision"`
	Tasks     []EdgeTask      `json:"tasks"`
	Devices   []ManagedDevice `json:"devices"`
	Products  []Product       `json:"products"`
}
type EdgeHeartbeat struct {
	CorruptDepth   int                   `json:"corruptDepth"`
	VideoCatalog   []EdgeVideoDevice     `json:"videoCatalog,omitempty"`
	RejectedDepth  int                   `json:"rejectedDepth"`
	LastSeenAt     int64                 `json:"lastSeenAt"`
	Version        string                `json:"version"`
	QueueDepth     int                   `json:"queueDepth"`
	LastError      string                `json:"lastError,omitempty"`
	ConfigRevision string                `json:"configRevision"`
	Capabilities   []string              `json:"capabilities"`
	Profiles       []DeviceAccessProfile `json:"profiles,omitempty"`
}
