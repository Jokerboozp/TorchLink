package ports

import (
	"strings"

	"iot-platform/internal/model"
)

// DeviceFilter narrows the managed device list on the server side. Empty fields
// do not restrict the result.
type DeviceFilter struct {
	TenantID string
	// Role matches the stored DIRECT, GATEWAY or CHILD role; an empty role is DIRECT.
	Role string
	// RestrictProducts limits the result to ProductIDs; an empty list then matches nothing.
	RestrictProducts bool
	ProductIDs       []string
	// Query matches the device ID or name, case-insensitively.
	Query string
	// Status matches the enable status; Runtime matches the business status and
	// treats a device without state as NEVER_SEEN.
	Status  string
	Runtime string
}

func (f DeviceFilter) EffectiveRole(d model.ManagedDevice) string {
	if d.DeviceRole == "" {
		return "DIRECT"
	}
	return d.DeviceRole
}

// Matches applies the same rules as the SQL implementation.
func (f DeviceFilter) Matches(d model.ManagedDevice, state *model.DeviceState) bool {
	if f.TenantID != "" && d.TenantID != f.TenantID {
		return false
	}
	if f.Role != "" && f.EffectiveRole(d) != f.Role {
		return false
	}
	if f.RestrictProducts {
		found := false
		for _, id := range f.ProductIDs {
			if id == d.ProductID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if q := strings.ToLower(strings.TrimSpace(f.Query)); q != "" && !strings.Contains(strings.ToLower(d.ID), q) && !strings.Contains(strings.ToLower(d.Name), q) {
		return false
	}
	if f.Status != "" && d.Status != f.Status {
		return false
	}
	if f.Runtime != "" {
		business := "NEVER_SEEN"
		if state != nil && state.BusinessStatus != "" {
			business = state.BusinessStatus
		}
		if business != f.Runtime {
			return false
		}
	}
	return true
}
