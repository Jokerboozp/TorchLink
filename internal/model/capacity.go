package model

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
)

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
	Historical    bool                      `json:"historical,omitempty"`
	KeepProtocol  bool                      `json:"keepProtocol,omitempty"`
}

type CapacityCleanupResource struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type CapacityCleanupCounts struct {
	Raw                    int64    `json:"rawMessages"`
	Standard               int64    `json:"standardMessages"`
	Alarms                 int64    `json:"alarms"`
	Devices                int64    `json:"devices"`
	Products               int64    `json:"products"`
	Rules                  int64    `json:"rules"`
	Resources              int64    `json:"resources"`
	Audits                 int64    `json:"audits"`
	Profiles               int64    `json:"profiles"`
	AccessReferences       int64    `json:"accessReferences"`
	Protocols              int64    `json:"protocols"`
	Inbox                  int64    `json:"inbox"`
	InboxSkipped           int64    `json:"inboxSkipped"`
	RetainedRequests       int64    `json:"retainedRequests"`
	QueueOffsetSpan        int64    `json:"queueOffsetSpan"`
	QueueSkippedPartitions int64    `json:"queueSkippedPartitions"`
	Warnings               []string `json:"warnings,omitempty"`
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
	c.Profiles += o.Profiles
	c.AccessReferences += o.AccessReferences
	c.Protocols += o.Protocols
	c.Inbox += o.Inbox
	c.InboxSkipped += o.InboxSkipped
	c.RetainedRequests += o.RetainedRequests
	c.QueueOffsetSpan += o.QueueOffsetSpan
	c.QueueSkippedPartitions += o.QueueSkippedPartitions
	for _, warning := range o.Warnings {
		if !slices.Contains(c.Warnings, warning) {
			c.Warnings = append(c.Warnings, warning)
		}
	}
}

// CapacityFixtureProduct contains no device credentials. The fingerprint binds
// a preview to the product and its current device membership before preparation.
type CapacityFixtureProduct struct {
	ProductID     string `json:"productId"`
	Name          string `json:"name"`
	Source        string `json:"source"`
	DeviceCount   int64  `json:"deviceCount"`
	RawMessages   int64  `json:"rawMessages"`
	BlockedReason string `json:"blockedReason,omitempty"`
	Fingerprint   string `json:"fingerprint"`
	ProtocolID    string `json:"protocolId,omitempty"`
}

// CapacityFixtureSource requires tool-owned content, never an ID prefix alone.
// Existing canonical products predate explicit metadata; their exact generated
// name, description and standard package remain the ownership evidence.
func CapacityFixtureSource(p Product) string {
	if p.ProtocolPackageID == "iot-standard@1.0.0" && p.Name == "容量测试标准设备 "+p.ID && strings.HasPrefix(p.Description, "capacity-test 自动创建") {
		return "capacity-test"
	}
	if !strings.EqualFold(p.Status, "DISABLED") || p.Metadata["source"] != "CAP" || !strings.HasPrefix(p.Name, "容量") {
		return ""
	}
	if p.ProtocolPackageID == "iot-standard@1.0.0" {
		return "legacy-cap"
	}
	if strings.HasPrefix(p.ProtocolPackageID, "cap-") && strings.Contains(p.ProtocolPackageID, "gb26875") {
		return "legacy-cap-gb26875"
	}
	return ""
}

func CapacityFixtureDevice(p Product, d ManagedDevice) bool {
	if d.TenantID != p.TenantID || d.ProductID != p.ID || d.GatewayID != "" || (d.DeviceRole != "" && d.DeviceRole != "DIRECT") {
		return false
	}
	switch CapacityFixtureSource(p) {
	case "capacity-test", "legacy-cap":
		return d.RegistrationSource == "ONBOARDING" && (d.Name == "容量测试 "+d.ID || d.Name == "压测设备 "+d.ID)
	case "legacy-cap-gb26875":
		return d.RegistrationSource == "PROTOCOL_AUTO" && d.AutoRegistered && strings.HasPrefix(d.ID, "gb26875_") && d.Name == "GB26875 设备 "+strings.TrimPrefix(d.ID, "gb26875_")
	}
	return false
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
