package model

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
)

// CapacityCleanupBatch is sent only by the trusted capacity controller. Tenant
// comes from the authenticated operator, never from the request body. Devices
// must be fixture devices of Product; they are removed with all their data.
type CapacityCleanupBatch struct {
	RunID         string   `json:"runId,omitempty"`
	AllRuns       bool     `json:"allRuns,omitempty"`
	Product       string   `json:"product,omitempty"`
	Devices       []string `json:"devices,omitempty"`
	RemoveProduct bool     `json:"removeProduct,omitempty"`
}

// CapacityCleanupCounts reports what one cleanup step actually removed.
type CapacityCleanupCounts struct {
	Raw              int64    `json:"rawMessages"`
	Standard         int64    `json:"standardMessages"`
	Alarms           int64    `json:"alarms"`
	Devices          int64    `json:"devices"`
	Products         int64    `json:"products"`
	Rules            int64    `json:"rules"`
	Resources        int64    `json:"resources"`
	Audits           int64    `json:"audits"`
	AccessReferences int64    `json:"accessReferences"`
	RetainedRequests int64    `json:"retainedRequests"`
	Warnings         []string `json:"warnings,omitempty"`
}

func (c *CapacityCleanupCounts) Add(o CapacityCleanupCounts) {
	c.Raw += o.Raw
	c.Standard += o.Standard
	c.Alarms += o.Alarms
	c.Devices += o.Devices
	c.Products += o.Products
	c.Rules += o.Rules
	c.Resources += o.Resources
	c.Audits += o.Audits
	c.AccessReferences += o.AccessReferences
	c.RetainedRequests += o.RetainedRequests
	for _, warning := range o.Warnings {
		if !slices.Contains(c.Warnings, warning) {
			c.Warnings = append(c.Warnings, warning)
		}
	}
}

// CapacityFixtureProduct summarizes one dedicated test product. It contains no
// device credentials.
type CapacityFixtureProduct struct {
	ProductID   string `json:"productId"`
	Name        string `json:"name"`
	DeviceCount int64  `json:"deviceCount"`
	RawMessages int64  `json:"rawMessages"`
}

// CapacityFixtureProductName and CapacityFixtureDescription are written by the
// capacity tool when it creates its dedicated standard-protocol product.
const CapacityFixtureDescription = "capacity-test 自动创建"

func CapacityFixtureProductName(id string) string { return "容量测试标准设备 " + id }

// IsCapacityFixture requires the tool-generated name, description and standard
// package; an ID prefix alone never establishes test ownership.
func IsCapacityFixture(p Product) bool {
	return p.ProtocolPackageID == "iot-standard@1.0.0" && p.Name == CapacityFixtureProductName(p.ID) && strings.HasPrefix(p.Description, CapacityFixtureDescription)
}

// CapacityFixtureDevice reports whether a device was registered by the capacity
// tool in its dedicated product and has not been repurposed since.
func CapacityFixtureDevice(p Product, d ManagedDevice) bool {
	return IsCapacityFixture(p) && d.TenantID == p.TenantID && d.ProductID == p.ID && d.GatewayID == "" && (d.DeviceRole == "" || d.DeviceRole == "DIRECT") && d.RegistrationSource == "ONBOARDING" && d.Name == "容量测试 "+d.ID
}

// PruneCapacityAccessReferences preserves unknown persisted keys and numeric
// precision. All-device users keep their sessions: removing stale selected IDs
// does not change their effective scope or invalidate the cleanup operator.
func PruneCapacityAccessReferences(body []byte, ids []string) ([]byte, int64, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var state map[string]any
	if err := decoder.Decode(&state); err != nil {
		return nil, 0, err
	}
	var count int64
	prune := func(item map[string]any) bool {
		values, ok := item["deviceIds"].([]any)
		if !ok {
			return false
		}
		next := make([]any, 0, len(values))
		for _, value := range values {
			id, stringID := value.(string)
			if stringID && slices.Contains(ids, id) {
				count++
				continue
			}
			next = append(next, value)
		}
		if len(next) == len(values) {
			return false
		}
		item["deviceIds"] = next
		return true
	}
	roleAll, changedRoles := map[string]bool{}, map[string]bool{}
	roles, _ := state["roles"].([]any)
	for _, value := range roles {
		role, ok := value.(map[string]any)
		if !ok {
			continue
		}
		id, _ := role["id"].(string)
		roleAll[id] = role["deviceScope"] == "all"
		if prune(role) && role["deviceScope"] == "selected" {
			changedRoles[id] = true
		}
	}
	users, _ := state["users"].([]any)
	for _, value := range users {
		user, ok := value.(map[string]any)
		if !ok {
			continue
		}
		affected := prune(user) && user["deviceScope"] == "selected"
		if user["deviceScope"] == "inherit" {
			all, changed := false, false
			roleIDs, _ := user["roleIds"].([]any)
			for _, value := range roleIDs {
				if id, ok := value.(string); ok {
					all = all || roleAll[id]
					changed = changed || changedRoles[id]
				}
			}
			affected = !all && changed
		}
		if affected {
			version, numberVersion := user["sessionVersion"].(json.Number)
			if _, exists := user["sessionVersion"]; exists && !numberVersion {
				return nil, 0, ErrResourceInUse
			}
			var current int64
			if version != "" {
				var err error
				current, err = version.Int64()
				if err != nil {
					return nil, 0, err
				}
			}
			if current == (1<<63)-1 {
				return nil, 0, ErrResourceInUse
			}
			user["sessionVersion"] = current + 1
		}
	}
	if count == 0 {
		return body, 0, nil
	}
	encoded, err := json.Marshal(state)
	return encoded, count, err
}
