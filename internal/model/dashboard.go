package model

// DashboardCount contains aggregate data only; no device credentials or alarm payloads.
type DashboardCount struct {
	Kind  string `json:"kind"`
	Key   string `json:"key"`
	Name  string `json:"name,omitempty"`
	Count int    `json:"count"`
}
