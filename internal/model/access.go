package model

// AccessState is tenant-scoped and persisted with optimistic concurrency.
type AccessState struct {
	Revision int64          `json:"revision"`
	Users    []PlatformUser `json:"users"`
	Roles    []PlatformRole `json:"roles"`
	APIKeys  []APIKey       `json:"apiKeys,omitempty"`
}
type PlatformUser struct {
	Username       string   `json:"username"`
	DisplayName    string   `json:"displayName"`
	PasswordHash   string   `json:"passwordHash,omitempty"`
	Enabled        bool     `json:"enabled"`
	RoleIDs        []string `json:"roleIds"`
	Permissions    []string `json:"permissions"`
	DeviceScope    string   `json:"deviceScope"`
	DeviceIDs      []string `json:"deviceIds"`
	SessionVersion int64    `json:"sessionVersion"`
}
type PlatformRole struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	DeviceScope string   `json:"deviceScope"`
	DeviceIDs   []string `json:"deviceIds"`
}

// APIKey lets an external system call the open API as its bound platform user:
// the user's roles, permissions and device scope still apply, and Capabilities
// narrow which open endpoints the key may use. Only the secret hash is stored.
type APIKey struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Username     string   `json:"username"`
	Capabilities []string `json:"capabilities"`
	SecretHash   string   `json:"secretHash,omitempty"`
	Enabled      bool     `json:"enabled"`
	CreatedBy    string   `json:"createdBy,omitempty"`
	CreatedAt    int64    `json:"createdAt"`
	ExpiresAt    int64    `json:"expiresAt,omitempty"`
}

const (
	APICapabilityAlarmsRead     = "alarms:read"
	APICapabilityAlarmsReport   = "alarms:report"
	APICapabilityAlarmsHandle   = "alarms:handle"
	APICapabilityMessagesRead   = "messages:read"
	APICapabilityMessagesReport = "messages:report"
	APICapabilityAIChat         = "ai:chat"
)

// APICapabilities lists every capability an API key may be granted.
var APICapabilities = []string{APICapabilityAlarmsRead, APICapabilityAlarmsReport, APICapabilityAlarmsHandle, APICapabilityMessagesRead, APICapabilityMessagesReport, APICapabilityAIChat}
