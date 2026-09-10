package model

import "testing"

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
			d := ManagedDevice{DeviceRole: tc.role, GatewayID: tc.parent, Tags: map[string]string{"connector": tc.connector}}
			if got := d.UsesPlatformCredentials(Product{Transport: tc.transport}); got != tc.want {
				t.Fatalf("credential support %v, want %v", got, tc.want)
			}
		})
	}
}
