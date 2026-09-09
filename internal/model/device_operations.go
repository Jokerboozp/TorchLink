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

// EdgeNode is a control-plane inventory record. It does not claim an agent is
// deployed or move a platform collection task to a remote machine.
type EdgeNode struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenantId"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Description string `json:"description,omitempty"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
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
	Execution *EdgeCommandExecution `json:"execution,omitempty"`
	Confirmed bool                  `json:"confirmed,omitempty"`
	ID        string                `json:"id"`
	TenantID  string                `json:"tenantId"`
	ProductID string                `json:"productId"`
	DeviceID  string                `json:"deviceId"`
	Type      string                `json:"type"`
	Data      map[string]any        `json:"data"`
	Status    string                `json:"status"`
	LastError string                `json:"lastError,omitempty"`
	Reply     map[string]any        `json:"reply,omitempty"`
	CreatedAt int64                 `json:"createdAt"`
	UpdatedAt int64                 `json:"updatedAt"`
}

// ObservedOutcome projects a bounded wait without inventing an execution result.
// A later authenticated reply can still supply the actual terminal outcome.
func (c DeviceCommand) ObservedOutcome(now int64) DeviceCommand {
	if c.Execution != nil && c.Execution.ExpiresAt <= now && c.Status == "QUEUED" {
		c.Status = "EXPIRED"
		c.LastError = "命令在领取前到期，未自动下发"
		return c
	}
	if (c.Status == "SENT" || c.Status == "DISPATCHING") && c.CreatedAt > 0 && now-c.CreatedAt >= 30000 {
		c.Status = "UNKNOWN"
		c.LastError = "30 秒内未收到设备执行结果；请核实设备，不自动重试"
	}
	return c
}
