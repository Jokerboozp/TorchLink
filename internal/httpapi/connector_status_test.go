package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type connectionSnapshot struct{}

func TestDeviceProfileRequiresIdentityEvidence(t *testing.T) {
	d := model.ManagedDevice{TenantID: "t", ID: "d", ProductID: "p", Tags: map[string]string{"connectorProfileId": "listener"}}
	p := model.DeviceAccessProfile{TenantID: "t", ID: "listener", ProductID: "p"}
	if !deviceUsesProfile(d, p, nil) {
		t.Fatal("explicit binding missing")
	}
	for _, other := range []model.DeviceAccessProfile{{TenantID: "other", ID: "listener", ProductID: "p", DeviceID: "d"}, {TenantID: "t", ID: "listener", ProductID: "other", DeviceID: "d"}} {
		if deviceUsesProfile(d, other, []map[string]any{{"deviceId": "d"}}) {
			t.Fatal("identity scope bypass")
		}
	}
	d.Tags = nil
	if deviceUsesProfile(d, p, []map[string]any{{"remoteAddress": "127.0.0.1"}}) {
		t.Fatal("unidentified session matched")
	}
}

func (connectionSnapshot) Status(string, string) (string, string, int64) { return "LISTENING", "", 123 }
func (connectionSnapshot) Sessions(tenant, id string) []map[string]any {
	if tenant == "t" && (id == "listener-a" || id == "listener-b") {
		return []map[string]any{{"deviceId": "legacy", "remoteAddress": "127.0.0.1:1234", "lastSeenAt": int64(123)}}
	}
	return nil
}
func (connectionSnapshot) Command(context.Context, string, string, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func TestLegacyDeviceConnectionResolution(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	root := t.TempDir()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	cfg := config.Load()
	cfg.JWTSecret = "legacy-test-key-32-characters"
	srv := New(cfg, engine, metrics.New(), log)
	srv.SetProtocolListeners(connectionSnapshot{})
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(repo.SaveProduct(ctx, model.Product{TenantID: "t", ID: "p", Status: "ENABLED"}))
	for _, id := range []string{"legacy", "poll-device", "unrelated"} {
		must(repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: id, AccessKey: id, ProductID: "p", Status: "ENABLED"}))
	}
	for _, p := range []model.DeviceAccessProfile{
		{TenantID: "t", ID: "listener-a", ProductID: "p", Mode: "listener", Network: "tcp", Enabled: true},
		{TenantID: "t", ID: "listener-b", ProductID: "p", Mode: "listener", Network: "udp", Enabled: false},
		{TenantID: "t", ID: "poll", ProductID: "p", DeviceID: "poll-device", Mode: "poll", Enabled: true},
		{TenantID: "other", ID: "listener-other", ProductID: "p", DeviceID: "legacy", Mode: "listener", Enabled: true},
	} {
		must(repo.SaveDeviceAccessProfile(ctx, p))
	}
	call := func(method, path, body string) (int, map[string]any) {
		t.Helper()
		token, _ := srv.auth.Issue("test", "t", "admin", nil, time.Hour)
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, r)
		var v map[string]any
		must(json.Unmarshal(w.Body.Bytes(), &v))
		return w.Code, v
	}
	code, v := call("GET", "/api/v1/device-registry/legacy/connection", "")
	if code != 200 || v["profile"] != nil || len(v["profiles"].([]any)) != 2 || len(v["sessions"].([]any)) != 2 {
		t.Fatalf("ambiguous: %d %+v", code, v)
	}
	code, v = call("GET", "/api/v1/device-registry/legacy/connection?profileId=listener-b", "")
	if code != 200 || v["connector"] != "UDP" || v["profile"].(map[string]any)["runtimeStatus"] != "DISABLED" || len(v["sessions"].([]any)) != 1 {
		t.Fatalf("selected: %d %+v", code, v)
	}
	for _, id := range []string{"poll", "listener-other"} {
		code, _ = call("GET", "/api/v1/device-registry/legacy/connection?profileId="+id, "")
		if code != 422 {
			t.Fatal(code)
		}
	}
	code, v = call("GET", "/api/v1/device-registry/poll-device/connection", "")
	if code != 200 || v["connector"] != "MODBUS_TCP" || v["profile"].(map[string]any)["id"] != "poll" {
		t.Fatalf("poll: %+v", v)
	}
	_, v = call("GET", "/api/v1/device-registry/unrelated/connection", "")
	if v["profile"] != nil || len(v["profiles"].([]any)) != 0 {
		t.Fatalf("product-only match: %+v", v)
	}
	_, v = call("GET", "/api/v1/connectors", "")
	for _, item := range v["items"].([]any) {
		x := item.(map[string]any)
		if len(x["recentDevices"].([]any)) != 1 {
			t.Fatalf("legacy device missing: %+v", x)
		}
	}
	device, err := repo.GetManagedDevice(ctx, "t", "legacy")
	must(err)
	if len(device.Tags) != 0 {
		t.Fatal("read mutated legacy device")
	}
	code, v = call("POST", "/api/v1/onboarding/test", `{"type":"HTTP","productId":"missing","deviceId":"test","name":"test"}`)
	if code != 422 || v["errorCode"] != "PROTOCOL_ERROR" || v["stage"] != "validate" || v["deviceId"] != "test" {
		t.Fatalf("validation result: %d %+v", code, v)
	}
}
