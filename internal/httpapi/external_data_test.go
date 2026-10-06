package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"iot-platform/internal/devicescope"
	"iot-platform/internal/netguard"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/externaldata"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

const externalTestTenant = "external_browser"

func externalAPIFixture(t *testing.T) (*Server, *memory.Repository, context.Context) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(t.TempDir()), log)
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err = repo.SaveProduct(ctx, model.Product{TenantID: externalTestTenant, ID: "ext-product", Name: "外部平台设备", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	for _, device := range []string{"ext-device", "other-device"} {
		if err = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: externalTestTenant, ID: device, AccessKey: "key-" + device, ProductID: "ext-product", Name: "外部设备", Status: "ENABLED"}); err != nil {
			t.Fatal(err)
		}
	}
	if err = repo.SaveVideoCameraMapping(ctx, model.VideoCameraMapping{TenantID: externalTestTenant, CameraID: "ext-camera", CameraName: "东门摄像头", DeviceID: "ext-device", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DevMode = true
	cfg.AdminUser = "root"
	cfg.AdminPassword = "external-browser-test-password"
	cfg.AdminTenants = []string{externalTestTenant}
	cfg.JWTSecret = "external-browser-test-jwt-key-only-32-characters"
	cfg.DataDir = t.TempDir()
	cfg.ProcessRole = "all"
	cfg.AccessCoordination = false
	// Partner systems in these tests are httptest servers on the loopback.
	cfg.ExternalDataAllowedCIDRs = netguard.Loopback.Allowed
	engine.MediaOutbound = netguard.Loopback
	api := New(cfg, engine, metrics.New(), log)
	if api.externalData == nil {
		t.Fatal("external data service unavailable")
	}
	return api, repo, ctx
}

func externalAlarmMapping() externaldata.Mapping {
	return externaldata.Mapping{Fields: []externaldata.Field{
		{Target: "id", Path: "eventId", Required: true}, {Target: "objectId", Path: "camera", Required: true}, {Target: "timestamp", Path: "time", Type: "timestamp", TimeFormat: "milliseconds", Required: true}, {Target: "version", Path: "version", Type: "number"}, {Target: "status", Path: "status", Default: "ACTIVE"}, {Target: "alarmType", Path: "type", Default: "FIRE"}, {Target: "alarmLevel", Constant: true, Value: "HIGH"}, {Target: "content", Path: "text"},
	}}
}

func TestExternalDataHTTPPushPullAlarmLifecycleAndPermissions(t *testing.T) {
	api, repo, ctx := externalAPIFixture(t)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	req := func(method, path, token string, payload any, status int) map[string]any {
		t.Helper()
		return requestJSON(t, server.Client(), method, server.URL+path, token, payload, status)
	}
	admin := req("POST", "/api/v1/auth/login", "", map[string]any{"tenantId": externalTestTenant, "username": "root", "password": api.cfg.AdminPassword}, 200)["accessToken"].(string)
	permissions := []string{"menu:devices", "menu:alarms", "menu:externalData", "POST /api/v1/alarms/:id/actions", "POST /api/v1/external-data/endpoints/:id/receive", "POST /api/v1/external-data/endpoints/:id/pull"}
	req("POST", "/api/v1/access/roles", admin, map[string]any{"id": "ext-role", "name": "外部执行", "permissions": permissions, "deviceScope": "selected", "deviceIds": []string{"ext-device"}}, 200)
	req("POST", "/api/v1/access/users", admin, map[string]any{"username": "external-user", "password": "external-user-test-password", "enabled": true, "roleIds": []string{"ext-role"}, "deviceScope": "inherit"}, 200)
	user := req("POST", "/api/v1/auth/login", "", map[string]any{"tenantId": externalTestTenant, "username": "external-user", "password": "external-user-test-password"}, 200)["accessToken"].(string)
	req("GET", externalBase+"/records", user, nil, 403)
	now := time.Now().Add(-time.Minute).UnixMilli()
	payload := map[string]any{"eventId": "event-1", "camera": "vendor-camera", "time": now, "version": 1, "status": "ACTIVE", "text": "识别到烟雾", "type": "FIRE"}
	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{payload}})
	}))
	defer vendor.Close()
	u, _ := url.Parse(vendor.URL)
	source := req("POST", externalBase+"/sources", admin, externaldata.Source{Name: "视频平台", Username: "external-user", Enabled: true, Auth: externaldata.Auth{Type: "none"}, AllowedHosts: []string{u.Host}}, 201)
	sourceID := source["id"].(string)
	push := req("POST", externalBase+"/endpoints", admin, externaldata.Endpoint{Name: "告警推送", SourceID: sourceID, Enabled: true, Mode: "push", Kind: "video_alarm", Mapping: externalAlarmMapping()}, 201)
	pushID := push["id"].(string)
	key := req("POST", externalBase+"/endpoints/"+pushID+"/rotate-key", admin, map[string]any{}, 200)["key"].(string)
	req("POST", externalBase+"/bindings", admin, externaldata.Binding{SourceID: sourceID, Kind: "camera", ExternalID: "vendor-camera", TargetID: "ext-camera"}, 201)
	callback := "/api/external/v1/" + externalTestTenant + "/" + pushID
	req("POST", callback, "incorrect", payload, 401)
	req("POST", callback, key, payload, 202)
	step := func(kind string) {
		t.Helper()
		worked, err := api.externalData.Step(ctx, kind, "test-worker")
		if err != nil || !worked {
			t.Fatalf("step %s worked=%v err=%v", kind, worked, err)
		}
	}
	step("record")
	records, _, err := api.externalData.Store.List(ctx, externaldata.Query{TenantID: externalTestTenant, Kind: "record", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var record externaldata.Record
	_ = json.Unmarshal(records[0].Body, &record)
	if records[0].Status != "PROCESSED" || record.AlarmID == "" || record.DeviceID != "ext-device" || record.CameraID != "ext-camera" {
		t.Fatalf("video ingestion: %+v %+v", records[0], record)
	}
	alarm := req("GET", "/api/v1/alarms/"+record.AlarmID, user, nil, 200)
	if alarm["source"] != "video" || alarm["deviceId"] != "ext-device" || alarm["content"] != payload["text"] {
		t.Fatal(alarm)
	}
	summaries, err := repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: externalTestTenant, Summary: true, Limit: 20})
	if err != nil || len(summaries) != 1 || summaries[0].Content != payload["text"] {
		t.Fatalf("notification summary lost external alarm content: %+v %v", summaries, err)
	}
	if _, err = repo.GetRawIndex(ctx, externalTestTenant, record.MessageID); err != nil {
		t.Fatal("missing raw archive", err)
	}
	mapping := externalAlarmMapping()
	mapping.ItemsPath = "items"
	pull := req("POST", externalBase+"/endpoints", admin, externaldata.Endpoint{Name: "告警拉取", SourceID: sourceID, Enabled: true, Mode: "pull", Kind: "video_alarm", URL: vendor.URL, Method: "GET", Mapping: mapping, Pagination: externaldata.Pagination{Mode: "none"}}, 201)
	req("POST", externalBase+"/endpoints/"+pull["id"].(string)+"/pull", admin, map[string]any{"from": now - 1000, "to": now + 1000}, 202)
	step("job")
	step("record")
	alarms := req("GET", "/api/v1/alarms", user, nil, 200)["items"].([]any)
	if len(alarms) != 1 {
		t.Fatalf("push/pull duplicate: %v", alarms)
	}
	// Distinct events of the same camera/type have independent lifecycles.
	payload["eventId"] = "event-2"
	payload["time"] = now + 100
	req("POST", callback, key, payload, 202)
	step("record")
	if n := len(req("GET", "/api/v1/alarms", user, nil, 200)["items"].([]any)); n != 2 {
		t.Fatalf("separate events merged: %d", n)
	}
	payload["eventId"] = "event-1"
	payload["status"] = "RECOVERED"
	payload["version"] = 2
	payload["time"] = now + 200
	req("POST", callback, key, payload, 202)
	step("record")
	alarm = req("GET", "/api/v1/alarms/"+record.AlarmID, user, nil, 200)
	if alarm["status"] != "RECOVERED" {
		t.Fatal(alarm)
	}
	if n := len(req("GET", "/api/v1/alarms?status=ACTIVE", user, nil, 200)["items"].([]any)); n != 1 {
		t.Fatalf("recovery affected other event: %d", n)
	}
	payload["version"] = 1
	payload["time"] = now
	payload["text"] = "晚到的旧事件"
	payload["status"] = "ACTIVE"
	req("POST", callback, key, payload, 202)
	step("record")
	alarm = req("GET", "/api/v1/alarms/"+record.AlarmID, user, nil, 200)
	if alarm["status"] != "RECOVERED" {
		t.Fatal("late event reopened alarm")
	}
	// Revocation is read freshly for each callback, not cached in source config.
	req("PUT", "/api/v1/access/users/external-user", admin, map[string]any{"username": "external-user", "enabled": false, "roleIds": []string{"ext-role"}, "deviceScope": "inherit"}, 200)
	req("POST", callback, key, payload, 403)
}

// Explicitly opt-in local browser fixture; no real environment credentials or
// dependency services are used, and the server exists only for this test run.
func TestExternalDataBrowserFixture(t *testing.T) {
	if os.Getenv("IOT_EXTERNAL_BROWSER_FIXTURE") != "1" {
		t.Skip("set IOT_EXTERNAL_BROWSER_FIXTURE=1 for browser acceptance")
	}
	api, _, ctx := externalAPIFixture(t)
	api.RunExternalData(ctx)
	mock := http.NewServeMux()
	mock.HandleFunc("/snapshot.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_ = png.Encode(w, image.NewRGBA(image.Rect(0, 0, 16, 16)))
	})
	mock.HandleFunc("/missing.mp4", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	mock.HandleFunc("/alarms", func(w http.ResponseWriter, r *http.Request) {
		write(w, 200, map[string]any{"items": []any{map[string]any{"eventId": "browser-alarm-1", "camera": "vendor-camera", "time": time.Now().Add(-time.Minute).Truncate(time.Hour).UnixMilli(), "version": 1, "status": "ACTIVE", "type": "FIRE", "text": "东门发现烟雾"}}})
	})
	for _, entry := range []struct {
		address string
		handler http.Handler
	}{{"127.0.0.1:8089", api.Handler()}, {"127.0.0.1:8090", mock}} {
		listener, err := net.Listen("tcp", entry.address)
		if err != nil {
			t.Fatal(err)
		}
		server := &http.Server{Handler: entry.handler, ReadHeaderTimeout: 5 * time.Second}
		t.Cleanup(func() { _ = server.Close() })
		go server.Serve(listener)
	}
	fmt.Println("EXTERNAL_BROWSER_READY 127.0.0.1:8089 mock=127.0.0.1:8090")
	select {
	case <-ctx.Done():
	case <-time.After(25 * time.Minute):
	}
}

func TestExternalPushAuthenticationContracts(t *testing.T) {
	src := externaldata.Source{Auth: externaldata.Auth{Type: "bearer", Secret: "test-only-secret"}}
	r := httptest.NewRequest("POST", "http://example.test", bytes.NewReader([]byte(`{}`)))
	r.Header.Set("Authorization", "Bearer test-only-secret")
	if !verifyExternalPush(r, []byte(`{}`), src, externaldata.Endpoint{}) {
		t.Fatal("bearer rejected")
	}
	r.Header.Set("Authorization", "Bearer incorrect")
	if verifyExternalPush(r, []byte(`{}`), src, externaldata.Endpoint{}) {
		t.Fatal("wrong bearer accepted")
	}
}
