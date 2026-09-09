package fieldprotocol

import (
	"context"
	"encoding/json"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/testonvif"
	"net"
	"strings"
	"testing"
)

func TestONVIFAuthenticatedMetadata(t *testing.T) {
	for _, digest := range []bool{false, true} {
		name := "WSSE"
		if digest {
			name = "DigestAndWSSE"
		}
		t.Run(name, func(t *testing.T) {
			camera := testonvif.Start(t, digest, false)
			p := model.DeviceAccessProfile{Host: "127.0.0.1", Port: camera.Server.Listener.Addr().(*net.TCPAddr).Port, TimeoutMs: 2000, CredentialRef: "camera"}
			release := model.ProtocolRelease{Transport: "ONVIF", Config: map[string]any{"reads": []model.PollPoint{{Identifier: "metadata", Address: "device-information"}}}}
			credential := Credential{Username: "operator", Password: testonvif.Password, TLSCAFile: camera.CAFile}
			collector := Collector{AllowedCIDRs: []string{"127.0.0.0/8"}, Credentials: map[string]Credential{"camera": credential}}
			raw, err := collector.Read(context.Background(), p, release)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := (parser.PollResponseParser{}).ParseWithConfig(raw[0], release.Config)
			if err != nil || parsed.Properties["metadata"].(map[string]any)["serialNumber"] != "CAM-42" {
				t.Fatalf("metadata: %+v %v", parsed, err)
			}
			encoded, _ := json.Marshal(raw)
			if strings.Contains(string(encoded), testonvif.Password) {
				t.Fatal("secret leaked into raw response")
			}
			for _, bad := range []Credential{{Username: "operator", Password: "wrong", TLSCAFile: camera.CAFile}, {Username: "operator", Password: testonvif.Password}, {TLSCAFile: camera.CAFile}} {
				collector.Credentials["camera"] = bad
				if _, err := collector.Read(context.Background(), p, release); err == nil {
					t.Fatal("invalid credential or trust accepted")
				}
			}
		})
	}
	camera := testonvif.Start(t, false, true)
	p := model.DeviceAccessProfile{Port: camera.Server.Listener.Addr().(*net.TCPAddr).Port, Host: "127.0.0.1"}
	if _, err := readONVIF(context.Background(), p.Host, p, []model.PollPoint{{Address: "device-information"}}, Credential{Username: "operator", Password: testonvif.Password, TLSCAFile: camera.CAFile}); err == nil {
		t.Fatal("SOAP fault accepted")
	}
}

func FuzzDigestChallenge(f *testing.F) {
	f.Add(`Digest realm="camera", nonce="abc", qop="auth,auth-int", algorithm=SHA-256`)
	f.Add(`Digest realm="a\"b", nonce="abc"`)
	f.Fuzz(func(t *testing.T, challenge string) {
		_, _ = digestAuthorization(challenge, "u", "p", "POST", "/onvif/device_service")
	})
}
