package httpapi

import (
	"context"
	"net/http/httptest"
	"testing"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/auth"
	"iot-platform/internal/core"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/protocolworker"
)

func TestTemplateDeviceProfilesRequireCompatibleCandidate(t *testing.T) {
	for _, test := range []struct {
		name                                         string
		mode, transport, wire                        string
		query, encode, child, disabled, blocks, want bool
	}{
		{name: "tcp-dial", mode: "listener", transport: "TCP", want: true},
		{name: "dial-network-changed", mode: "listener", transport: "UDP"},
		{name: "scheduled-query-lost-encode", mode: "listener", transport: "TCP", query: true},
		{name: "scheduled-query-preserved", mode: "listener", transport: "TCP", query: true, encode: true, want: true},
		{name: "modbus-poll", mode: "poll", transport: "MODBUS_TCP", blocks: true, want: true},
		{name: "rtu-poll", mode: "poll", transport: "MODBUS_RTU", wire: "rtu_over_tcp", blocks: true, want: true},
		{name: "modbus-wire-changed", mode: "poll", transport: "MODBUS_RTU", blocks: true},
		{name: "modbus-blocks-missing", mode: "poll", transport: "MODBUS_TCP"},
		{name: "child-mapping-needs-hex", child: true, transport: "HTTP"},
		{name: "disabled-profile-does-not-block", mode: "poll", transport: "HTTP", disabled: true, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := memory.NewRepository()
			profile := model.DeviceAccessProfile{TenantID: "tenant", ID: "device-profile", ProductID: "template", DeviceID: "device", ProtocolID: "proto", ProtocolVersion: "1", Mode: test.mode, Network: "tcp", Host: "127.0.0.1", Port: 15020, TimeoutMs: 1000, Enabled: !test.disabled, WireFormat: test.wire}
			if test.mode == "listener" {
				profile.ConnectionMode = "dial"
			}
			if test.query {
				profile.Queries = []model.ProtocolQuery{{Type: "read", IntervalSec: 1}}
			}
			release := model.ProtocolRelease{ProtocolID: "proto", Version: "2", Status: "PUBLISHED", Transport: test.transport, PayloadFormat: "hex", ParserType: parser.GoProtocolParserName, Artifact: map[string]any{"runtime": protocolworker.Runtime}, Capabilities: []string{"ingress", "decode"}, Config: map[string]any{}}
			if test.encode {
				release.Capabilities = append(release.Capabilities, "encode")
			}
			if test.blocks {
				release.Config["blocks"] = []model.ModbusReadBlock{{ID: "read", FunctionCode: 3, Quantity: 1}}
			}
			if test.child {
				profile.ProductID = "parent"
				profile.DeviceID = ""
				profile.Mode = "listener"
				profile.ChildProducts = []model.ChildProductBinding{{Type: "sensor", ProductID: "template"}}
				release.PayloadFormat = "json"
			}
			if err := repo.SaveDeviceAccessProfile(context.Background(), profile); err != nil {
				t.Fatal(err)
			}
			server := &Server{engine: &core.Engine{Repo: repo}}
			req := httptest.NewRequest("POST", "/", nil).WithContext(context.WithValue(context.Background(), claimsKey, auth.Claims{TenantID: "tenant"}))
			if err := server.validateTemplateDeviceProfiles(req, &release, "template"); (err == nil) != test.want {
				t.Fatalf("compatible=%v error=%v", test.want, err)
			}
		})
	}
}
