package httpapi

import (
	"reflect"
	"testing"

	"iot-platform/internal/model"
)

func TestRoleDeviceScopeResolution(t *testing.T) {
	state := model.AccessState{Roles: []model.PlatformRole{
		{ID: "east", DeviceScope: "selected", DeviceIDs: []string{"east", "shared"}},
		{ID: "west", DeviceScope: "selected", DeviceIDs: []string{"west", "shared"}},
		{ID: "admin", DeviceScope: "all"},
		{ID: "legacy"},
	}}
	for _, tc := range []struct {
		name, scope string
		roles, ids  []string
		want        string
		wantIDs     []string
	}{
		{"union", "inherit", []string{"east", "west"}, nil, "selected", []string{"east", "shared", "west"}},
		{"all", "inherit", []string{"east", "admin"}, nil, "all", []string{}},
		{"missing role", "inherit", []string{"missing", "legacy"}, []string{"stale"}, "none", []string{}},
		{"unassigned", "inherit", nil, nil, "none", []string{}},
		{"legacy", "", []string{"admin"}, nil, "", nil},
		{"explicit none", "none", []string{"admin"}, nil, "none", nil},
		{"explicit selected", "selected", []string{"admin"}, []string{"own"}, "selected", []string{"own"}},
		{"explicit all", "all", []string{"east"}, nil, "all", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := model.PlatformUser{RoleIDs: tc.roles, DeviceScope: tc.scope, DeviceIDs: tc.ids}
			got := resolveUserDeviceScope(state, user)
			if got.DeviceScope != tc.want || !reflect.DeepEqual(got.DeviceIDs, tc.wantIDs) {
				t.Fatalf("scope=%s ids=%v", got.DeviceScope, got.DeviceIDs)
			}
			if user.DeviceScope != tc.scope || !reflect.DeepEqual(user.DeviceIDs, tc.ids) {
				t.Fatal("stored user mutated")
			}
		})
	}
	// Tenant-wide menus require the resolved all-device scope, not the raw inherit value.
	user := model.PlatformUser{DeviceScope: "inherit", RoleIDs: []string{"admin"}, Permissions: []string{"menu:devices", "menu:backups"}}
	if !effectivePermissions(state, user)["menu:backups"] {
		t.Fatal("inherited all-device menu lost")
	}
	user.RoleIDs = []string{"east"}
	if effectivePermissions(state, user)["menu:backups"] {
		t.Fatal("limited scope grants tenant-wide menu")
	}
}
