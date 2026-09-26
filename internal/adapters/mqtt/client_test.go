package mqttadapter

import (
	"testing"

	"iot-platform/internal/model"
)

func TestTopicIdentityOverridesPayloadTenant(t *testing.T) {
	raw := model.RawMessage{TenantID: "spoofed", ProductID: "spoofed", DeviceID: "spoofed"}
	if err := applyRawTopicIdentity("/external/raw/tenant-a/product-a/device-a", &raw); err != nil {
		t.Fatal(err)
	}
	if raw.TenantID != "tenant-a" || raw.ProductID != "product-a" || raw.DeviceID != "device-a" {
		t.Fatalf("topic identity was not authoritative: %#v", raw)
	}

	state := model.DeviceState{TenantID: "spoofed", ProductID: "spoofed", DeviceID: "spoofed"}
	if err := applyStateTopicIdentity("/iot/device/state/tenant-a/product-a/device-a", &state); err != nil {
		t.Fatal(err)
	}
	if state.TenantID != "tenant-a" || state.ProductID != "product-a" || state.DeviceID != "device-a" {
		t.Fatalf("state topic identity was not authoritative: %#v", state)
	}

	v := model.VideoAlarmEvent{TenantID: "spoofed", CameraID: "spoofed"}
	if err := applyVideoTopicIdentity("/external/video/alarm/tenant-a/camera-a", &v); err != nil {
		t.Fatal(err)
	}
	if v.TenantID != "tenant-a" || v.CameraID != "camera-a" {
		t.Fatalf("video topic identity was not authoritative: %#v", v)
	}
}

func TestTopicIdentityRejectsMalformedTopics(t *testing.T) {
	if err := applyStateTopicIdentity("/iot/device/state/tenant-a/product-a", &model.DeviceState{}); err == nil {
		t.Fatal("expected malformed state topic to be rejected")
	}
	if err := applyVideoTopicIdentity("/external/video/alarm/tenant-a", &model.VideoAlarmEvent{}); err == nil {
		t.Fatal("expected malformed video topic to be rejected")
	}
}
