package model

// ExecutionLease uses a monotonic fencing token when ownership changes.
type ExecutionLease struct {
	TenantID  string `json:"tenantId"`
	Resource  string `json:"resource"`
	Owner     string `json:"owner"`
	Endpoint  string `json:"endpoint"`
	Token     int64  `json:"token"`
	ExpiresAt int64  `json:"expiresAt"`
}
