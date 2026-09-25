package httpapi

import (
	"sort"

	"iot-platform/internal/model"
)

// Resolve a copy for this request. Explicit user scopes (including legacy empty
// scopes) override role scopes; only an explicit inherit opts into role grants.
func resolveUserDeviceScope(state model.AccessState, user model.PlatformUser) model.PlatformUser {
	if user.DeviceScope != "inherit" {
		return user
	}
	user.DeviceScope, user.DeviceIDs = "none", []string{}
	assigned := make(map[string]bool, len(user.RoleIDs))
	for _, id := range user.RoleIDs {
		assigned[id] = true
	}
	ids := map[string]bool{}
	for _, role := range state.Roles {
		if !assigned[role.ID] {
			continue
		}
		switch role.DeviceScope {
		case "all":
			user.DeviceScope = "all"
			return user
		case "selected":
			for _, id := range role.DeviceIDs {
				ids[id] = true
			}
		}
	}
	if len(ids) > 0 {
		user.DeviceScope = "selected"
		for id := range ids {
			user.DeviceIDs = append(user.DeviceIDs, id)
		}
		sort.Strings(user.DeviceIDs)
	}
	return user
}
