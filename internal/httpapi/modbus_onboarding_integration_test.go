package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/connector"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"iot-platform/internal/protocolruntime"
)

func TestModbusOnboardingRuntimeChain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.SetDeadline(time.Now().Add(time.Second))
			request := make([]byte, 12)
			if _, err = io.ReadFull(conn, request); err == nil {
				_, _ = conn.Write([]byte{request[0], request[1], 0, 0, 0, 5, request[6], 3, 2, 0, 42})
			}
			_ = conn.Close()
		}
	}()
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
	cfg.DataDir, cfg.JWTSecret, cfg.ModbusAllowedCIDRs = root, "isolated-modbus-chain-signing-key", []string{"127.0.0.0/8"}
	srv := New(cfg, engine, metrics.New(), log)
	token, err := srv.auth.Issue("tester", "tenant", "admin", nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path string, body any) *httptest.ResponseRecorder {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		srv.Handler().ServeHTTP(out, req)
		return out
	}
	q := onboarding.Request{ProductID: "modbus-product", ProductName: "模拟温度产品", DeviceID: "modbus-device", Name: "模拟温度设备", Type: connector.ModbusTCP, PollIntervalSec: 1, Profile: model.DeviceAccessProfile{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, UnitID: 1, TimeoutMs: 500}, PointTableCSV: "name,functionCode,address,addressNotation,dataType,scale\ntemperature,3,0,zero_based,uint16,1\n"}
	preview := call("POST", "/api/v1/onboarding/test", q)
	var tested connector.Result
	if err = json.Unmarshal(preview.Body.Bytes(), &tested); err != nil || preview.Code != 200 || !tested.Success || tested.Source != "network-read" {
		t.Fatal("preview", preview.Code, preview.Body.String(), err)
	}
	q.TestToken = tested.TestToken
	created := call("POST", "/api/v1/onboarding", q)
	if created.Code != 201 {
		t.Fatal("save", created.Code, created.Body.String())
	}
	recovered := call("POST", "/api/v1/onboarding", q)
	if recovered.Code != 200 || !bytes.Contains(recovered.Body.Bytes(), []byte(`"reused":true`)) {
		t.Fatal("Modbus onboarding recovery failed", recovered.Code)
	}
	for _, response := range []*httptest.ResponseRecorder{created, recovered} {
		for _, field := range []string{`"credential"`, `"accessKey"`, `"clientId"`, `"username"`} {
			if bytes.Contains(response.Body.Bytes(), []byte(field)) {
				t.Fatalf("Modbus onboarding returned %s", field)
			}
		}
	}
	stored, err := repo.GetManagedDevice(ctx, "tenant", q.DeviceID)
	if err != nil || stored.SecretHash != "" || stored.AccessKey == "" {
		t.Fatal("Modbus must persist an internal identity without a secret", err)
	}
	connection := func() map[string]any {
		result := call("GET", "/api/v1/device-registry/modbus-device/connection", nil)
		var data map[string]any
		if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &data) != nil {
			t.Fatal("connection", result.Code)
		}
		return data
	}
	if connection()["ingest"].(map[string]any)["rawReceived"] != false {
		t.Fatal("preview persisted business data")
	}
	runtime := protocolruntime.New(repo, func(ctx context.Context, raw model.RawMessage) error {
		_, _, err := engine.IngestRaw(ctx, raw)
		return err
	}, log, "127.0.0.0/8")
	runtime.Start(ctx)
	wait := func(check func() bool) {
		t.Helper()
		for !check() {
			select {
			case <-ctx.Done():
				t.Fatal("runtime chain timed out")
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	wait(func() bool { return connection()["ingest"].(map[string]any)["parsed"] == true })
	records, count, err := repo.ListDeviceMessages(ctx, "tenant", "modbus-device", model.PropertyReport, 10, 0)
	if err != nil || count < 1 || records[0].Properties["temperature"] != float64(42) {
		t.Fatal("missing parsed simulator value", count, err)
	}
	index, err := repo.GetRawIndex(ctx, "tenant", records[0].RawMessageID)
	if err != nil || index.ParseAttemptedAt == 0 || index.ParseError != "" {
		t.Fatal("missing raw parse evidence", err)
	}
	_ = listener.Close()
	wait(func() bool { return connection()["profile"].(map[string]any)["runtimeStatus"] == "ERROR" })
	if connection()["ingest"].(map[string]any)["parsed"] != true {
		t.Fatal("failed polling removed previous evidence")
	}
}
