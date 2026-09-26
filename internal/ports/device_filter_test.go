package ports

import (
	"testing"

	"iot-platform/internal/model"
)

func TestDeviceFilterRestrictDevices(t *testing.T) {
	d := model.ManagedDevice{TenantID: "t", ID: "granted"}
	if !(DeviceFilter{TenantID: "t", RestrictDevices: true, DeviceIDs: []string{"granted"}}).Matches(d, nil) {
		t.Fatal("a granted device must match")
	}
	if (DeviceFilter{TenantID: "t", RestrictDevices: true, DeviceIDs: []string{"other"}}).Matches(d, nil) {
		t.Fatal("a device outside the grant must not match")
	}
	if (DeviceFilter{TenantID: "t", RestrictDevices: true}).Matches(d, nil) {
		t.Fatal("an empty grant matches nothing")
	}
}
