package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/parser"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDeviceOperationsHTTPAndRawReply(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := memory.NewRepository()
	root := t.TempDir()
	archive, e := local.NewArchive(root)
	if e != nil {
		t.Fatal(e)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	if e = engine.Start(ctx); e != nil {
		t.Fatal(e)
	}
	cfg := config.Load()
	cfg.JWTSecret = "operations-test-key-32-characters"
	srv := New(cfg, engine, metrics.New(), log)
	sent := 0
	srv.SetDeviceOperations(func(context.Context, string, []byte, byte, bool) error { sent++; return nil }, nil)
	repo.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", Status: "ENABLED"})
	repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "t", ProtocolID: parser.StandardProtocolID, Version: "1.0.0", Status: "PUBLISHED", ParserType: parser.StandardParserName})
	repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: "d", ProductID: "p", Status: "ENABLED", AccessKey: "key", SecretHash: onboarding.Hash("secret"), Tags: map[string]string{"connector": "MQTT"}})
	call := func(method, path, body, tenant, role string) *httptest.ResponseRecorder {
		token, _ := srv.auth.Issue("test", tenant, role, nil, time.Hour)
		r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("X-Device-Key", "key")
		r.Header.Set("X-Device-Secret", "secret")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, r)
		return w
	}
	if w := call("POST", "/api/v1/device-registry/d/commands", `{"confirmed":true,"id":"c1","type":"set","data":{"value":1}}`, "t", "viewer"); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call("POST", "/api/v1/device-registry/d/commands", `{"id":"unconfirmed","type":"reset","data":{}}`, "t", "operator"); w.Code != 422 || sent != 0 {
		t.Fatal("unconfirmed command dispatched", w.Code)
	}
	for i := 0; i < 2; i++ {
		if w := call("POST", "/api/v1/device-registry/d/commands", `{"confirmed":true,"id":"c1","type":"set","data":{"value":1}}`, "t", "operator"); w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if sent != 1 {
		t.Fatal(sent)
	}
	if w := call("GET", "/api/v1/device-registry/d/history", "", "other", "admin"); w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
	reply := `{"id":"reply1","timestamp":1788850000001,"data":{"commandId":"c1","success":true,"result":"ok"}}`
	w := call("POST", "/api/v1/device-ingest/standard/t/p/d/command-reply", reply, "t", "admin")
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var accepted struct {
		MessageID string `json:"messageId"`
	}
	json.Unmarshal(w.Body.Bytes(), &accepted)
	deadline := time.Now().Add(5 * time.Second)
	for {
		v, _, err := repo.ListDeviceCommands(ctx, "t", "d", 20, 0)
		if err != nil {
			t.Fatal(err)
		}
		if v[0].Status == "SUCCEEDED" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("reply not applied", v)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, e = repo.GetRawIndex(ctx, "t", accepted.MessageID); e != nil {
		t.Fatal("reply bypassed archive", e)
	}
	m, e := repo.GetStandardMessageByRaw(ctx, "t", accepted.MessageID)
	if e != nil || m.MessageType != model.CommandReply {
		t.Fatal(m, e)
	}
	if e = engine.ReportConnection(ctx, "t", "p", "d", true, 3); e != nil {
		t.Fatal(e)
	}
	if e = engine.ReportConnection(ctx, "t", "p", "d", false, 4); e != nil {
		t.Fatal(e)
	}
	events, _, e := repo.ListDeviceStateEvents(ctx, "t", "d", 20, 0)
	if e != nil || len(events) < 2 || events[0].State.ConnectionStatus != "DISCONNECTED" || events[1].State.ConnectionStatus != "CONNECTED" {
		t.Fatal(events, e)
	}
	if w = call("POST", "/api/v1/edge-nodes", `{"id":"edge","name":"现场节点"}`, "t", "admin"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = call("GET", "/api/v1/edge-nodes", "", "other", "viewer")
	if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte("现场节点")) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = call("DELETE", "/api/v1/device-registry/d/credentials", "", "t", "admin")
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte("PENDING")) {
		t.Fatal(w.Code, w.Body.String())
	}
	w = call("POST", "/api/v1/device-mqtt/token", "", "t", "admin")
	if w.Code != 401 {
		t.Fatal("disabled token accepted", w.Code)
	}
	// Details follow the current binding even while a profile still records the
	// original release, and the alarm preview must remain device/tenant scoped.
	d, _ := repo.GetManagedDevice(ctx, "t", "d")
	d.Tags = map[string]string{"connector": "TCP", "connectorProfileId": "profile"}
	repo.SaveManagedDevice(ctx, d)
	repo.SaveDeviceAccessProfile(ctx, model.DeviceAccessProfile{TenantID: "t", ID: "profile", ProductID: "p", ProtocolID: "old", ProtocolVersion: "1"})
	repo.CreateProtocolRelease(ctx, model.ProtocolRelease{TenantID: "t", ProtocolID: "current", Version: "2", Status: "PUBLISHED", Capabilities: []string{"encode"}})
	repo.SaveProductProtocolBinding(ctx, model.ProductProtocolBinding{TenantID: "t", ProductID: "p", ProtocolID: "current", Version: "2"})
	repo.UpsertAlarm(ctx, model.Alarm{TenantID: "t", DeviceID: "d", ID: "own", RuleID: "r", AlarmType: "FIRE", LastTriggeredAt: 1})
	repo.UpsertAlarm(ctx, model.Alarm{TenantID: "other", DeviceID: "d", ID: "foreign", RuleID: "r", AlarmType: "FIRE", LastTriggeredAt: 2})
	w = call("GET", "/api/v1/device-registry/d/connection", "", "t", "viewer")
	var detail struct {
		ProtocolID string        `json:"protocolId"`
		Version    string        `json:"protocolVersion"`
		CanCommand bool          `json:"canCommand"`
		Alarms     []model.Alarm `json:"recentAlarms"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &detail) != nil || detail.ProtocolID != "current" || detail.Version != "2" || !detail.CanCommand || len(detail.Alarms) != 1 || detail.Alarms[0].ID != "own" {
		t.Fatal(w.Code, w.Body.String())
	}
}
