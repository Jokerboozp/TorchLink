package connector

import "testing"

func TestTypesDescribeOnlySupportedConnections(t *testing.T) {
	seen := map[Type]Capabilities{}
	for _, d := range Types() {
		if !d.Supported || d.Name == "" {
			t.Fatalf("unsupported or unnamed type listed: %+v", d)
		}
		seen[d.Type] = d.Capabilities
	}
	if len(seen) != 6 || !seen[ModbusTCP].ReadOnce || seen[HTTP].Command || !seen[MQTT].DeviceCredential || !seen[TCP].Listener {
		t.Fatalf("incorrect capabilities: %+v", seen)
	}
}
