package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManagedDeviceReadsLegacySystemTags(t *testing.T) {
	var d ManagedDevice
	legacy := `{"id":"d","connector":"MQTT","tags":{"floor":"1","connector":"HTTP","connectorProfileId":"p","childAddress":"3","childType":"smoke","onboardingRequestHash":"h"}}`
	if err := json.Unmarshal([]byte(legacy), &d); err != nil {
		t.Fatal(err)
	}
	if d.Connector != "MQTT" || d.ConnectorProfileID != "p" || d.ChildAddress != "3" || d.ChildType != "smoke" || d.OnboardingRequestHash != "h" {
		t.Fatalf("fields %+v", d)
	}
	if len(d.Tags) != 1 || d.Tags["floor"] != "1" {
		t.Fatalf("system keys must leave the labels: %v", d.Tags)
	}
	data, _ := json.Marshal(d.Public(Product{}))
	if strings.Contains(string(data), "onboardingRequestHash") || !strings.Contains(string(data), `"connectorProfileId":"p"`) {
		t.Fatalf("public device %s", data)
	}
	if !SystemTag("childType") || SystemTag("floor") {
		t.Fatal("reserved label keys")
	}
}

func TestDeviceCredentialScope(t *testing.T) {
	for _, tc := range []struct {
		name, transport, connector, role, parent string
		want                                     bool
	}{
		{"standard HTTP on TCP product", "TCP", "HTTP", "DIRECT", "", true},
		{"standard MQTT on Modbus product", "MODBUS_TCP", "MQTT", "DIRECT", "", true},
		{"explicit TCP on HTTP product", "HTTP", "TCP", "DIRECT", "", false},
		{"manual Modbus", "MODBUS_RTU_TCP", "", "DIRECT", "", false},
		{"child of HTTP gateway", "HTTP", "HTTP", "CHILD", "main", false},
		{"child with incomplete role", "HTTP", "HTTP", "", "main", false},
		{"HTTP gateway", "HTTP", "", "GATEWAY", "", true},
		{"managed HTTP default", "", "", "DIRECT", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := ManagedDevice{DeviceRole: tc.role, GatewayID: tc.parent, Connector: tc.connector}
			if got := d.UsesPlatformCredentials(Product{Transport: tc.transport}); got != tc.want {
				t.Fatalf("credential support %v, want %v", got, tc.want)
			}
		})
	}
}

func TestObservedCommandTimeoutPreservesTerminalReply(t *testing.T) {
	for _, status := range []string{"SENT", "DISPATCHING"} {
		c := DeviceCommand{Status: status, CreatedAt: 1000}
		if c.ObservedOutcome(30999).Status != status || c.ObservedOutcome(31000).Status != "UNKNOWN" {
			t.Fatal("incorrect wait boundary")
		}
		if c.Status != status {
			t.Fatal("query mutated stored outcome")
		}
	}
	for _, status := range []string{"SUCCEEDED", "FAILED"} {
		if (DeviceCommand{Status: status, CreatedAt: 1000}).ObservedOutcome(40000).Status != status {
			t.Fatal("terminal outcome lost")
		}
	}
}

func TestLegacyQueuedCommandIsUnknownAndHidesExecutionToken(t *testing.T) {
	var c DeviceCommand
	if err := json.Unmarshal([]byte(`{"id":"old-command","status":"QUEUED","execution":{"nodeId":"old-node","token":"legacy-secret"}}`), &c); err != nil {
		t.Fatal(err)
	}
	observed := c.ObservedOutcome(100).Public()
	if observed.Status != "UNKNOWN" || observed.LastError == "" || c.Status != "QUEUED" {
		t.Fatal("legacy queue claimed execution or mutated history", observed)
	}
	encoded, err := json.Marshal(observed)
	if err != nil || strings.Contains(string(encoded), "legacy-secret") || strings.Contains(string(encoded), "execution") {
		t.Fatal("legacy execution metadata leaked", err)
	}
}

func TestProtocolAccessPublicHostSeparatesListenAndDeviceAddress(t *testing.T) {
	profile := DeviceAccessProfile{Mode: "listener", Network: "tcp", ConnectionMode: "listen", Host: "0.0.0.0", PublicHost: "devices.example.test"}
	if err := ValidateProtocolAccess(profile); err != nil {
		t.Fatalf("valid public host: %v", err)
	}
	for _, host := range []string{"0.0.0.0", "::", "[::]", "https://devices.example.test", "devices.example.test/path"} {
		profile.PublicHost = host
		if err := ValidateProtocolAccess(profile); err == nil {
			t.Fatalf("unsafe device-facing host accepted: %q", host)
		}
	}
	profile.PublicHost = "devices.example.test"
	profile.ConnectionMode = "dial"
	if err := ValidateProtocolAccess(profile); err == nil {
		t.Fatal("dial-only profile accepted a device-facing listener host")
	}
}

func TestDeviceRoles(t *testing.T) {
	for _, tc := range []struct {
		device         ManagedDevice
		child, gateway bool
	}{
		{ManagedDevice{DeviceRole: "DIRECT"}, false, false},
		{ManagedDevice{DeviceRole: "GATEWAY"}, false, true},
		{ManagedDevice{DeviceRole: "CHILD", GatewayID: "gw"}, true, false},
		// Attached to a gateway counts as a child whatever the stored role says.
		{ManagedDevice{DeviceRole: "GATEWAY", GatewayID: "gw"}, true, false},
	} {
		if tc.device.IsChild() != tc.child || tc.device.IsGateway() != tc.gateway {
			t.Errorf("%+v: IsChild=%v IsGateway=%v", tc.device, tc.device.IsChild(), tc.device.IsGateway())
		}
	}
}

func TestClearLegacyStreamFieldsKeepsCameraMetadata(t *testing.T) {
	v := VideoCameraMapping{CameraID: "c", CameraName: "门厅", DeviceID: "d", StreamURL: "rtsp://10.0.0.1/live", StreamType: "rtsp", SDKEndpoint: "https://sdk", SDKCameraID: "1", SDKCredentialRef: "ref"}
	v.ClearLegacyStreamFields()
	if v.StreamURL != "" || v.StreamType != "" || v.SDKEndpoint != "" || v.SDKCameraID != "" || v.SDKCredentialRef != "" || v.CameraName != "门厅" || v.DeviceID != "d" {
		t.Fatalf("unexpected camera after clearing: %+v", v)
	}
}
