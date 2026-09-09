package fieldprotocol

import (
	"context"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/testfield"
	"strings"
	"testing"
	"time"
)

func TestAuthenticatedOPCUAAndSNMP(t *testing.T) {
	fixture := testfield.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	credential := Credential{BACnetMode: "ip", Username: "fixture-user", Password: "fixture-auth-password", PrivacyPassword: "fixture-privacy-password", SNMPVersion: "3", CertificateFile: fixture.Certificate, PrivateKeyFile: fixture.PrivateKey, ServerSHA256: fixture.SHA256}
	for _, transport := range []string{"OPC_UA", "SNMP", "BACNET"} {
		t.Run(transport, func(t *testing.T) {
			p := model.DeviceAccessProfile{ID: "read", TenantID: "t", ProductID: "p", DeviceID: "d", Host: "127.0.0.1", CredentialRef: "local", TimeoutMs: 3000}
			address := fixture.NodeID
			expected := any(float64(42))
			p.Port = fixture.OPCPort
			if transport == "SNMP" {
				address = ".1.3.6.1.2.1.1.1.0"
				expected = "actual-snmp-response"
				p.Port = fixture.SNMPPort
			}
			if transport == "BACNET" {
				address = "0:1:85"
				p.Port = fixture.BACnetPort
			}
			release := model.ProtocolRelease{ProtocolID: "protocol", Version: "1", Transport: transport, ParserType: parser.PollResponseParserName, Config: map[string]any{"reads": []model.PollPoint{{Identifier: "value", Address: address}}}}
			collector := Collector{AllowedCIDRs: []string{"127.0.0.0/8"}, Credentials: map[string]Credential{"local": credential}}
			raws, err := collector.Read(ctx, p, release)
			if err != nil || len(raws) != 1 {
				t.Fatal("authenticated read", err)
			}
			message, err := (parser.PollResponseParser{}).ParseWithConfig(raws[0], release.Config)
			if err != nil || message.Properties["value"] != expected {
				t.Fatal("actual response parse", message, err)
			}
			if strings.Contains(string(raws[0].Payload), credential.Password) || strings.Contains(string(raws[0].Payload), credential.PrivacyPassword) {
				t.Fatal("credential leaked into raw response")
			}
			if transport == "BACNET" {
				return
			}
			bad := credential
			bad.Password = "wrong-fixture-password"
			collector.Credentials["local"] = bad
			if _, err = collector.Read(ctx, p, release); err == nil {
				t.Fatal("invalid password accepted by real server")
			}
			if transport == "SNMP" {
				collector.Credentials["local"] = Credential{SNMPVersion: "2c", Community: "fixture-community"}
				if _, err = collector.Read(ctx, p, release); err != nil {
					t.Fatal("explicit SNMP v2c community", err)
				}
				collector.Credentials["local"] = Credential{SNMPVersion: "2c", Community: "wrong-community"}
				if _, err = collector.Read(ctx, p, release); err == nil {
					t.Fatal("invalid SNMP community accepted")
				}
			}
			if transport == "OPC_UA" {
				bad = credential
				bad.ServerSHA256 = strings.Repeat("0", 64)
				collector.Credentials["local"] = bad
				if _, err = collector.Read(ctx, p, release); err == nil {
					t.Fatal("untrusted server accepted")
				}
			}
		})
	}
}
