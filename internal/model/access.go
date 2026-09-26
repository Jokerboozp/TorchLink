package model

// AccessState is tenant-scoped and persisted with optimistic concurrency.
type AccessState struct {
	Revision int64          `json:"revision"`
	Users    []PlatformUser `json:"users"`
	Roles    []PlatformRole `json:"roles"`
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
