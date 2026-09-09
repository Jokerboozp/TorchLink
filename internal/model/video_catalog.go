package model

type EdgeVideoChannel struct {
	DeviceID     string `json:"deviceId"`
	Name         string `json:"name"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Owner        string `json:"owner,omitempty"`
	CivilCode    string `json:"civilCode,omitempty"`
	Address      string `json:"address,omitempty"`
	Parental     int    `json:"parental"`
	ParentID     string `json:"parentId,omitempty"`
	Status       string `json:"status"`
}
type EdgeVideoDevice struct {
	DeviceID     string             `json:"deviceId"`
	Name         string             `json:"name"`
	Manufacturer string             `json:"manufacturer"`
	Model        string             `json:"model"`
	Firmware     string             `json:"firmware"`
	Registered   bool               `json:"registered"`
	LastSeenAt   int64              `json:"lastSeenAt"`
	CatalogAt    int64              `json:"catalogAt"`
	LastError    string             `json:"lastError,omitempty"`
	Channels     []EdgeVideoChannel `json:"channels"`
}
