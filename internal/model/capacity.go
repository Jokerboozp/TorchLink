package model

// CapacityCleanupBatch is sent only by the trusted capacity controller. Tenant
// comes from the authenticated operator, never from the request body.
type CapacityCleanupBatch struct {
	RunID         string                    `json:"runId"`
	Product       string                    `json:"product"`
	Devices       []string                  `json:"devices"`
	RemoveDevices []string                  `json:"removeDevices,omitempty"`
	RawIDs        []string                  `json:"rawIds,omitempty"`
	RemoveProduct bool                      `json:"removeProduct,omitempty"`
	RemoveRule    string                    `json:"removeRule,omitempty"`
	Resources     []CapacityCleanupResource `json:"resources,omitempty"`
}

type CapacityCleanupResource struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type CapacityCleanupCounts struct {
	Raw       int64 `json:"rawMessages"`
	Standard  int64 `json:"standardMessages"`
	Alarms    int64 `json:"alarms"`
	Devices   int64 `json:"devices"`
	Products  int64 `json:"products"`
	Rules     int64 `json:"rules"`
	Resources int64 `json:"resources"`
}

func (c *CapacityCleanupCounts) Add(o CapacityCleanupCounts) {
	c.Raw += o.Raw
	c.Standard += o.Standard
	c.Alarms += o.Alarms
	c.Devices += o.Devices
	c.Products += o.Products
	c.Rules += o.Rules
	c.Resources += o.Resources
}
