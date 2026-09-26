package model

type ThingField struct {
	Writable   bool   `json:"writable,omitempty"`
	Identifier string `json:"identifier"`
	Name       string `json:"name"`
	DataType   string `json:"dataType"`
	Unit       string `json:"unit,omitempty"`
	Required   bool   `json:"required,omitempty"`
}
type ThingOperation struct {
	Identifier string       `json:"identifier"`
	Name       string       `json:"name"`
	Fields     []ThingField `json:"fields,omitempty"`
}
type ThingModel struct {
	Properties []ThingField     `json:"properties"`
	Events     []ThingOperation `json:"events"`
	Commands   []ThingOperation `json:"commands"`
}

type DeviceStateEvent struct {
	State      DeviceState `json:"state"`
	RecordedAt int64       `json:"recordedAt"`
}
type CredentialRevocation struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenantId"`
	DeviceID  string `json:"deviceId"`
	Username  string `json:"username"`
	Status    string `json:"status"`
	LastError string `json:"lastError,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}
type DeviceCommand struct {
	Confirmed bool           `json:"confirmed,omitempty"`
	ID        string         `json:"id"`
	TenantID  string         `json:"tenantId"`
	ProductID string         `json:"productId"`
	DeviceID  string         `json:"deviceId"`
	Type      string         `json:"type"`
	Data      map[string]any `json:"data"`
	Status    string         `json:"status"`
	LastError string         `json:"lastError,omitempty"`
	Reply     map[string]any `json:"reply,omitempty"`
	CreatedAt int64          `json:"createdAt"`
	UpdatedAt int64          `json:"updatedAt"`
}

// ObservedOutcome projects a bounded wait without inventing an execution result.
// A later authenticated reply can still supply the actual terminal outcome.
func (c DeviceCommand) ObservedOutcome(now int64) DeviceCommand {
	if c.Status == "QUEUED" {
		c.Status, c.LastError = "UNKNOWN", "现场命令执行器已移除，历史命令不会自动重发；请核实设备状态"
		return c
	}
	if (c.Status == "SENT" || c.Status == "DISPATCHING") && c.CreatedAt > 0 && now-c.CreatedAt >= 30000 {
		c.Status = "UNKNOWN"
		c.LastError = "30 秒内未收到设备执行结果；请核实设备，不自动重试"
	}
	return c
}

func (c DeviceCommand) Public() DeviceCommand { return c }
