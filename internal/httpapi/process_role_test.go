package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/connector"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSplitGatewayHTTPFlow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bus := local.NewBus()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	// Separate engines, shared test repository and queue. Only the API consumes Raw.
	gatewayEngine := core.New(repo, archive, bus, local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
	apiEngine := core.New(repo, archive, bus, local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
	if err = apiEngine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.JWTSecret = "split-gateway-test-signing-secret"
	cfg.ProcessRole = "gateway"
	gateway := New(cfg, gatewayEngine, metrics.New(), log)
	upstream := httptest.NewServer(gateway.Handler())
	defer upstream.Close()
	cfg.ProcessRole, cfg.AccessGatewayURL = "api", upstream.URL
	api := New(cfg, apiEngine, metrics.New(), log)
	token, err := api.auth.Issue("tester", "tenant", "admin", nil, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	call := func(handler http.Handler, method, path string, body any, auth string, credential model.DeviceCredential) *httptest.ResponseRecorder {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		if auth != "" {
			r.Header.Set("Authorization", "Bearer "+auth)
		}
		r.Header.Set("X-Device-Key", credential.AccessKey)
		r.Header.Set("X-Device-Secret", credential.Secret)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	q := onboarding.Request{ProductID: "product", ProductName: "产品", DeviceID: "device", Name: "设备", Type: connector.HTTP, MessageKind: "property", Payload: json.RawMessage(`{"id":"sample","timestamp":1788850000000,"data":{"temperature":42}}`)}
	if r := call(gateway.Handler(), "POST", "/api/v1/auth/login", nil, "", model.DeviceCredential{}); r.Code != 404 {
		t.Fatal("gateway exposed login", r.Code)
	}
	if r := call(api.Handler(), "POST", "/api/v1/onboarding/test", q, "", model.DeviceCredential{}); r.Code != 401 {
		t.Fatal("forward bypassed auth", r.Code)
	}
	test := call(api.Handler(), "POST", "/api/v1/onboarding/test", q, token, model.DeviceCredential{})
	var preview connector.Result
	if test.Code != 200 || json.Unmarshal(test.Body.Bytes(), &preview) != nil || !preview.Success {
		t.Fatal("preview", test.Code, test.Body.String())
	}
	q.TestToken = preview.TestToken
	saved := call(api.Handler(), "POST", "/api/v1/onboarding", q, token, model.DeviceCredential{})
	var result onboarding.Result
	if saved.Code != 201 || json.Unmarshal(saved.Body.Bytes(), &result) != nil {
		t.Fatal("save", saved.Code, saved.Body.String())
	}
	ingest := call(api.Handler(), "POST", "/api/v1/device-ingest/standard/tenant/product/device/property", q.Payload, "", result.Credential)
	if ingest.Code != 202 {
		t.Fatal("ingest", ingest.Code, ingest.Body.String())
	}
	var accepted struct {
		MessageID string `json:"messageId"`
	}
	_ = json.Unmarshal(ingest.Body.Bytes(), &accepted)
	deadline := time.Now().Add(3 * time.Second)
	for {
		msg, err := repo.GetStandardMessageByRaw(ctx, "tenant", accepted.MessageID)
		if err == nil {
			if msg.Properties["temperature"] != float64(42) {
				t.Fatal("decoded wrong value")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("API consumer did not parse gateway raw", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r := call(api.Handler(), "GET", "/api/v1/device-registry/device/connection", nil, token, model.DeviceCredential{}); r.Code != 200 {
		t.Fatal("connection forwarding", r.Code)
	}
	upstream.Close()
	if r := call(api.Handler(), "POST", "/api/v1/onboarding/test", q, token, model.DeviceCredential{}); r.Code != 503 {
		t.Fatal("unavailable gateway falsely succeeded", r.Code)
	}
}
