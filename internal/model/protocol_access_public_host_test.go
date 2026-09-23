package model

import "testing"

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
