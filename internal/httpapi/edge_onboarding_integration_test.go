package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/connector"
	"iot-platform/internal/core"
	"iot-platform/internal/edgeagent"
	"iot-platform/internal/fieldprotocol"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/testfield"
	"iot-platform/internal/testserial"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEdgeRTUOnboardingWithActualSerialRead(t *testing.T) {
	port := testserial.Start(t)
	q := onboarding.Request{ProductID: "rtu-product", ProductName: "RTU product", DeviceID: "rtu-device", Name: "RTU device", Type: connector.ModbusRTU, PollIntervalSec: 1, Profile: model.DeviceAccessProfile{EdgeNodeID: "edge", SerialPort: port, BaudRate: 9600, Parity: "N", StopBits: 1, UnitID: 1, TimeoutMs: 500}, PointTableCSV: "name,functionCode,address,addressNotation,dataType,scale\ntemperature,3,0,zero_based,uint16,1\n"}
	testEdgePollingChain(t, q, "", float64(42))
}

func testEdgePollingChain(t *testing.T, q onboarding.Request, credentialFile string, expected any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	repo := memory.NewRepository()
	root := t.TempDir()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DataDir = root
	server := New(cfg, engine, metrics.New(), log)
	upstream := httptest.NewServer(server.Handler())
	defer upstream.Close()
	if err = repo.SaveEdgeNode(ctx, model.EdgeNode{TenantID: "t", ID: "edge", Name: "RTU node", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	if err = repo.SetEdgeCredential(ctx, "t", "edge", onboarding.Hash("edge-test-secret")); err != nil {
		t.Fatal(err)
	}
	agent, err := edgeagent.New(edgeagent.Options{URL: upstream.URL, TenantID: "t", NodeID: "edge", Secret: "edge-test-secret", DataDir: t.TempDir(), AllowedCIDRs: []string{"127.0.0.0/8"}, CredentialFile: credentialFile, AllowedSerialPorts: []string{q.Profile.SerialPort}, AllowInsecureHTTP: true}, log)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- agent.Run(ctx) }()
	defer func() { cancel(); <-done; agent.Close() }()
	wait := func(fn func() bool) {
		t.Helper()
		for !fn() {
			select {
			case <-ctx.Done():
				t.Fatal("RTU chain timeout")
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	wait(func() bool { h, e := repo.GetEdgeHeartbeat(ctx, "t", "edge"); return e == nil && h.LastSeenAt > 0 })

	// A locally disallowed target must not earn an onboarding proof.
	bad := q
	if q.Type == connector.ModbusRTU {
		bad.Profile.SerialPort = "/etc/passwd"
	} else {
		bad.Profile.CredentialRef = "missing-local-credential"
	}
	preview, err := server.onboarding.Test(ctx, "t", bad)
	if err == nil && (preview.Success || preview.TestToken != "") {
		t.Fatal("blocked serial port earned a proof")
	}
	preview, err = server.onboarding.Test(ctx, "t", q)
	if err != nil || !preview.Success || preview.Source != "edge-read" || preview.StandardMessages[0].Properties["temperature"] != expected {
		t.Fatalf("actual remote preview: %+v %v", preview, err)
	}
	if _, err = repo.GetManagedDevice(ctx, "t", q.DeviceID); err == nil {
		t.Fatal("preview created a device")
	}
	q.TestToken = preview.TestToken
	created, err := server.onboarding.Create(ctx, "t", q)
	if err != nil {
		t.Fatal(err)
	}
	if created.Connector.Profile.EdgeNodeID != "edge" {
		t.Fatal("assignment lost")
	}
	wait(func() bool {
		messages, _, e := repo.ListDeviceMessages(ctx, "t", q.DeviceID, model.PropertyReport, 10, 0)
		return e == nil && len(messages) > 0
	})
	messages, _, err := repo.ListDeviceMessages(ctx, "t", q.DeviceID, model.PropertyReport, 10, 0)
	if err != nil || messages[0].Properties["temperature"] != expected {
		t.Fatal(messages, err)
	}
	index, err := repo.GetRawIndex(ctx, "t", messages[0].RawMessageID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := engine.GetRaw(ctx, index)
	if err != nil || raw.Transport != string(q.Type) || raw.CollectorID != "edge" {
		t.Fatal("RTU archive identity", raw, err)
	}
}

func TestEdgeAuthenticatedFieldOnboarding(t *testing.T) {
	fixture := testfield.Start(t)
	credential := fieldprotocol.Credential{BACnetMode: "ip", Username: "fixture-user", Password: "fixture-auth-password", PrivacyPassword: "fixture-privacy-password", SNMPVersion: "3", CertificateFile: fixture.Certificate, PrivateKeyFile: fixture.PrivateKey, ServerSHA256: fixture.SHA256}
	file := filepath.Join(t.TempDir(), "credentials.json")
	b, err := json.Marshal(map[string]fieldprotocol.Credential{"local": credential})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, b, 0600); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []connector.Type{connector.OPCUA, connector.SNMP, connector.BACnet} {
		t.Run(string(kind), func(t *testing.T) {
			port, address, expected := fixture.OPCPort, fixture.NodeID, any(float64(42))
			if kind == connector.SNMP {
				port, address, expected = fixture.SNMPPort, ".1.3.6.1.2.1.1.1.0", "actual-snmp-response"
			}
			if kind == connector.BACnet {
				port, address = fixture.BACnetPort, "0:1:85"
			}
			q := onboarding.Request{ProductID: "field-product", ProductName: "field product", DeviceID: "field-device", Name: "field device", Type: kind, PollIntervalSec: 1, Profile: model.DeviceAccessProfile{EdgeNodeID: "edge", Host: "127.0.0.1", Port: port, TimeoutMs: 3000, CredentialRef: "local"}, ReadPoints: []model.PollPoint{{Identifier: "temperature", Address: address}}}
			testEdgePollingChain(t, q, file, expected)
		})
	}
}
