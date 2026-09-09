package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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
)

func TestStandardOnboardingHTTPChain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.JWTSecret = "onboarding-test-signing-key-32-characters"
	cfg.AdminTenants = []string{"tenant"}
	cfg.DeviceHTTPPublicURL = "https://devices.example.test"
	cfg.MQTTPublicURL = "mqtts://devices.example.test:8883"
	srv := New(cfg, engine, metrics.New(), log)
	token, _ := srv.auth.Issue("tester", "tenant", "admin", nil, time.Hour)
	_ = repo.SaveProduct(ctx, model.Product{TenantID: "tenant", ID: "product", Status: "ENABLED"})
	payload := json.RawMessage(`{"id":"a","timestamp":1788850000000,"data":{"temperature":26.5}}`)
	q := onboarding.Request{ProductID: "product", DeviceID: "device", Name: "传感器", Type: connector.HTTP, MessageKind: "property", Payload: payload}
	call := func(method, path string, body []byte, credential model.DeviceCredential, admin bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Device-Key", credential.AccessKey)
		r.Header.Set("X-Device-Secret", credential.Secret)
		if admin {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, r)
		return w
	}
	data, _ := json.Marshal(q)
	w := call("POST", "/api/v1/onboarding/test", data, model.DeviceCredential{}, true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var preview connector.Result
	_ = json.Unmarshal(w.Body.Bytes(), &preview)
	if !preview.Success {
		t.Fatal(w.Body.String())
	}
	q.TestToken = preview.TestToken
	data, _ = json.Marshal(q)
	w = call("POST", "/api/v1/onboarding", data, model.DeviceCredential{}, true)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var created onboarding.Result
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	guide := call("GET", "/api/v1/device-registry/device/connection-guide", nil, model.DeviceCredential{}, true)
	var guideData struct {
		HTTP            struct{ Method, URL string }
		MQTT            struct{ Broker, Topic string }
		PayloadTemplate map[string]any
	}
	if err := json.Unmarshal(guide.Body.Bytes(), &guideData); err != nil || guide.Code != 200 || guideData.HTTP.Method != "POST" || guideData.HTTP.URL != "https://devices.example.test/api/v1/device-ingest/standard/tenant/product/device/property" || guideData.MQTT.Topic != "/iot/up/tenant/product/device/property" || guideData.MQTT.Broker != cfg.MQTTPublicURL || guideData.PayloadTemplate["version"] != "1.0" {
		t.Fatal("standard connection guide contract", guide.Code, err)
	}
	connection := call("GET", "/api/v1/device-registry/device/connection", nil, model.DeviceCredential{}, true)
	if connection.Code != 200 || !strings.Contains(connection.Body.String(), "WAITING_FOR_DATA") {
		t.Fatal("configuration falsely reported receipt", connection.Code)
	}
	if replay := call("POST", "/api/v1/onboarding", data, model.DeviceCredential{}, true); replay.Code != 200 || !strings.Contains(replay.Body.String(), `"reused":true`) || strings.Contains(replay.Body.String(), created.Credential.Secret) {
		t.Fatal("lost response retry failed or leaked secret", replay.Code)
	}
	if legacy := call("POST", "/api/v1/device-ingest/device", []byte(`{"protocolId":"untrusted","payload":{"properties":{"x":1}}}`), created.Credential, false); legacy.Code != 422 {
		t.Fatal("standard device bypassed standard ingress", legacy.Code)
	}
	path := "/api/v1/device-ingest/standard/tenant/product/device/property"
	if w = call("POST", path, payload, model.DeviceCredential{}, false); w.Code != 401 {
		t.Fatal("anonymous", w.Code)
	}
	if w = call("POST", strings.Replace(path, "/device/", "/other-device/", 1), payload, created.Credential, false); w.Code != 401 {
		t.Fatal("device boundary", w.Code)
	}
	if w = call("POST", strings.Replace(path, "/tenant/", "/other/", 1), payload, created.Credential, false); w.Code != 401 {
		t.Fatal("tenant boundary", w.Code)
	}
	if w = call("POST", path, []byte(strings.Repeat("x", 65537)), created.Credential, false); w.Code != 413 {
		t.Fatal("body limit", w.Code)
	}
	if w = call("POST", path, payload, created.Credential, false); w.Code != http.StatusAccepted {
		t.Fatal(w.Code, w.Body.String())
	}
	var accepted struct {
		MessageID string `json:"messageId"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &accepted)
	idx, err := repo.GetRawIndex(ctx, "tenant", accepted.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := engine.GetRaw(ctx, idx)
	if err != nil || !bytes.Equal(raw.Payload, payload) {
		t.Fatal("raw archive missing", err)
	}
	m, err := repo.GetStandardMessageByRaw(ctx, "tenant", idx.MessageID)
	if err != nil || m.Properties["temperature"] != 26.5 {
		t.Fatal("standard message missing", err, m)
	}
	if w = call("POST", path, payload, created.Credential, false); w.Code != 202 || !strings.Contains(w.Body.String(), `"created":false`) {
		t.Fatal("duplicate", w.Code, w.Body.String())
	}
	conflict := bytes.Replace(payload, []byte("26.5"), []byte("99.9"), 1)
	if w = call("POST", path, conflict, created.Credential, false); w.Code != 409 || !strings.Contains(w.Body.String(), "MESSAGE_CONFLICT") {
		t.Fatal("conflicting duplicate", w.Code, w.Body.String())
	}
	preserved, err := engine.GetRaw(ctx, idx)
	if err != nil || !bytes.Equal(preserved.Payload, payload) {
		t.Fatal("conflict overwrote archived raw", err)
	}
	connection = call("GET", "/api/v1/device-registry/device/connection", nil, model.DeviceCredential{}, true)
	if connection.Code != 200 || !strings.Contains(connection.Body.String(), `"stage":"PARSED"`) || !strings.Contains(connection.Body.String(), idx.MessageID) {
		t.Fatal("parsed receipt missing", connection.Code)
	}
	// State still traverses raw archival and parsing before updating connectivity.
	statePayload := []byte(`{"id":"state-1","timestamp":1788850000001,"data":{"connectionStatus":"CONNECTED"}}`)
	w = call("POST", strings.TrimSuffix(path, "property")+"state", statePayload, created.Credential, false)
	if w.Code != 202 {
		t.Fatal("state ingest", w.Code, w.Body.String())
	}
	state, e := repo.GetDeviceState(ctx, "tenant", "device")
	if e != nil || state.ConnectionStatus != "CONNECTED" || state.LastConnectAt != 1788850000001 {
		t.Fatal("state projection", state, e)
	}
	w = call("POST", "/api/v1/device-mqtt/token", nil, created.Credential, false)
	if w.Code != 200 {
		t.Fatal("MQTT credential exchange", w.Code, w.Body.String())
	}
	var mqttToken struct {
		Token     string `json:"token"`
		ExpiresIn int    `json:"expiresIn"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &mqttToken)
	mqttClaims, e := srv.auth.Parse(mqttToken.Token)
	if e != nil || mqttToken.ExpiresIn != 300 {
		t.Fatal("invalid device MQTT token", e)
	}
	claimsJSON, _ := json.Marshal(mqttClaims)
	if strings.Contains(string(claimsJSON), "/external/raw/") || !strings.Contains(string(claimsJSON), "/iot/up/tenant/product/device/property") {
		t.Fatal("standard device ACL includes raw bypass or misses topic")
	}
	w = call("POST", "/api/v1/device-registry/device/credentials", []byte(`{}`), model.DeviceCredential{}, true)
	if w.Code != 200 {
		t.Fatal("rotate credential", w.Code, w.Body.String())
	}
	var rotated struct {
		Credential model.DeviceCredential `json:"credential"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &rotated)
	if w = call("POST", path, payload, created.Credential, false); w.Code != 401 {
		t.Fatal("rotated old credential accepted", w.Code)
	}
	if w = call("POST", path, payload, rotated.Credential, false); w.Code != 202 {
		t.Fatal("new credential rejected", w.Code, w.Body.String())
	}
	created.Credential = rotated.Credential
	w = call("DELETE", "/api/v1/device-registry/device/credentials", nil, model.DeviceCredential{}, true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call("POST", path, payload, created.Credential, false); w.Code != 401 {
		t.Fatal("disabled", w.Code)
	}
}
