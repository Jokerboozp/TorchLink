package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
)

func TestOpenAPIKeyScopesReportsAndAlarms(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	root := t.TempDir()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewPlatformRegistry(root), log)
	if err = engine.Start(ctx); err != nil {
		t.Fatal(err)
	}
	const tenant = "tenant_a"
	if err = repo.SaveProduct(ctx, model.Product{TenantID: tenant, ID: "detector", Name: "烟感", Status: "ENABLED"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"dev_a", "dev_b"} {
		if err = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: tenant, ProductID: "detector", ID: id, Name: "烟感 " + id, Status: "ENABLED", AccessKey: "key_" + id}); err != nil {
			t.Fatal(err)
		}
	}
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword, cfg.AdminTenants = "root", "root-password-test", []string{tenant}
	cfg.JWTSecret = "test-only-secret-for-open-api-at-least-32"
	cfg.DataDir = root
	server := httptest.NewServer(New(cfg, engine, metrics.New(), log).Handler())
	defer server.Close()
	req := func(method, path, token string, body any, status int) map[string]any {
		t.Helper()
		return requestJSON(t, server.Client(), method, server.URL+path, token, body, status)
	}
	admin := req("POST", "/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": tenant}, 200)["accessToken"].(string)
	req("POST", "/api/v1/access/roles", admin, map[string]any{"id": "external", "name": "外部系统", "permissions": []string{"menu:devices", "menu:alarms", "POST /api/v1/alarms/:id/actions"}, "deviceScope": "selected", "deviceIds": []string{"dev_a"}}, 200)
	req("POST", "/api/v1/access/users", admin, map[string]any{"username": "ext_system", "password": "initial-password-test", "enabled": true, "roleIds": []string{"external"}, "permissions": []string{}, "deviceScope": "inherit"}, 200)

	req("POST", "/api/v1/access/api-keys", admin, map[string]any{"name": "外部平台", "username": "missing", "capabilities": []string{"alarms:read"}}, 422)
	req("POST", "/api/v1/access/api-keys", admin, map[string]any{"name": "外部平台", "username": "ext_system", "capabilities": []string{"everything"}}, 422)
	created := req("POST", "/api/v1/access/api-keys", admin, map[string]any{"name": "外部平台", "username": "ext_system", "capabilities": []string{"alarms:read", "alarms:report", "messages:report", "ai:chat"}}, 201)
	key := created["apiKey"].(string)
	keyID := created["item"].(map[string]any)["id"].(string)
	if !strings.HasPrefix(key, "tlk.") || strings.Contains(key, keyID) == false {
		t.Fatal("unexpected API key format", key)
	}
	listed := req("GET", "/api/v1/access/api-keys", admin, nil, 200)
	for _, item := range listed["items"].([]any) {
		if _, ok := item.(map[string]any)["secretHash"]; ok {
			t.Fatal("secret hash exposed")
		}
	}

	req("GET", "/api/open/v1/me", "", nil, 401)
	req("GET", "/api/open/v1/me", key[:len(key)-2]+"00", nil, 401)
	req("GET", "/api/open/v1/me", admin, nil, 401) // Console sessions are not API keys.
	me := req("GET", "/api/open/v1/me", key, nil, 200)
	if me["username"] != "ext_system" || me["deviceScope"] != "selected" || me["tenantId"] != tenant {
		t.Fatal("unexpected open identity", me)
	}

	now := time.Now().UnixMilli()
	batch := req("POST", "/api/open/v1/device-messages", key, map[string]any{"messages": []map[string]any{
		{"deviceId": "dev_a", "kind": "property", "id": "p-1", "timestamp": now, "data": map[string]any{"temperature": 26.5}},
		{"deviceId": "dev_b", "kind": "property", "id": "p-1", "timestamp": now, "data": map[string]any{"temperature": 30}},
		{"deviceId": "dev_a", "kind": "command-reply", "id": "c-1", "timestamp": now, "data": map[string]any{"commandId": "x", "success": true}},
	}}, 207)
	results := batch["results"].([]any)
	if batch["accepted"].(float64) != 1 || results[0].(map[string]any)["status"] != "ACCEPTED" || results[1].(map[string]any)["errorCode"] != "DEVICE_NOT_FOUND" || results[2].(map[string]any)["errorCode"] != "INVALID_MESSAGE" {
		t.Fatal("unexpected batch results", batch)
	}
	// Retries with identical content are idempotent; reusing an ID for new content conflicts.
	req("POST", "/api/open/v1/device-messages", key, map[string]any{"messages": []map[string]any{{"deviceId": "dev_a", "kind": "property", "id": "p-1", "timestamp": now, "data": map[string]any{"temperature": 26.5}}}}, 202)
	req("POST", "/api/open/v1/device-messages", key, map[string]any{"messages": []map[string]any{{"deviceId": "dev_a", "kind": "property", "id": "p-1", "timestamp": now, "data": map[string]any{"temperature": 99}}}}, 422)

	req("POST", "/api/open/v1/alarms", key, map[string]any{"deviceId": "dev_b", "id": "a-1", "timestamp": now, "alarmType": "FIRE"}, 404)
	report := req("POST", "/api/open/v1/alarms", key, map[string]any{"deviceId": "dev_a", "id": "a-1", "timestamp": now, "alarmType": "FIRE", "alarmLevel": "CRITICAL", "content": "3 层烟感报警"}, 202)
	var alarm map[string]any
	for deadline := time.Now().Add(10 * time.Second); alarm == nil && time.Now().Before(deadline); time.Sleep(150 * time.Millisecond) {
		for _, item := range req("GET", "/api/open/v1/alarms?deviceId=dev_a", key, nil, 200)["items"].([]any) {
			if item.(map[string]any)["triggerId"] == report["triggerId"] {
				alarm = item.(map[string]any)
			}
		}
	}
	if alarm == nil || alarm["alarmType"] != "FIRE" || alarm["alarmLevel"] != "CRITICAL" || alarm["source"] != "device" || alarm["status"] != "ACTIVE" {
		t.Fatal("reported alarm was not raised", alarm)
	}
	alarmID := alarm["alarmId"].(string)
	req("GET", "/api/open/v1/alarms/"+alarmID, key, nil, 200)

	// Capabilities narrow the key even where the bound user has the permission.
	req("GET", "/api/open/v1/devices/dev_a/latest", key, nil, 403)
	req("POST", "/api/open/v1/alarms/"+alarmID+"/actions", key, map[string]string{"action": "ACKED"}, 403)
	// The bound user's permissions still apply: it has no AI assistant menu.
	req("POST", "/api/open/v1/ai/chat", key, map[string]string{"question": "hi"}, 403)
	req("PUT", "/api/v1/access/api-keys/"+keyID, admin, map[string]any{"name": "外部平台", "capabilities": []string{"alarms:read", "alarms:report", "alarms:handle", "messages:read", "messages:report"}}, 200)
	acked := req("POST", "/api/open/v1/alarms/"+alarmID+"/actions", key, map[string]string{"action": "ACKED"}, 200)
	if acked["status"] != "ACKED" {
		t.Fatal("alarm was not acknowledged", acked)
	}
	// A clearing property report recovers a device alarm of a known type.
	req("POST", "/api/open/v1/device-messages", key, map[string]any{"messages": []map[string]any{{"deviceId": "dev_a", "kind": "property", "id": "p-2", "timestamp": now + 1, "data": map[string]any{"fireAlarm": false}}}}, 202)
	alarm = acked
	for deadline := time.Now().Add(10 * time.Second); alarm["status"] != "RECOVERED" && time.Now().Before(deadline); time.Sleep(150 * time.Millisecond) {
		alarm = req("GET", "/api/open/v1/alarms/"+alarmID, key, nil, 200)
	}
	if alarm["status"] != "RECOVERED" {
		t.Fatal("alarm was not recovered by a clearing report", alarm["status"])
	}
	req("POST", "/api/open/v1/alarms/"+alarmID+"/actions", key, map[string]string{"action": "RECOVERED"}, 422)
	// Custom alarm types have no clearing property; the reporter recovers them explicitly.
	custom := req("POST", "/api/open/v1/alarms", key, map[string]any{"deviceId": "dev_a", "id": "a-2", "timestamp": now + 2, "alarmType": "GAS_LEAK", "content": "燃气泄漏"}, 202)
	var customID string
	for deadline := time.Now().Add(10 * time.Second); customID == "" && time.Now().Before(deadline); time.Sleep(150 * time.Millisecond) {
		for _, item := range req("GET", "/api/open/v1/alarms?deviceId=dev_a&status=ACTIVE", key, nil, 200)["items"].([]any) {
			if item.(map[string]any)["triggerId"] == custom["triggerId"] {
				customID = item.(map[string]any)["alarmId"].(string)
			}
		}
	}
	if recovered := req("POST", "/api/open/v1/alarms/"+customID+"/actions", key, map[string]string{"action": "RECOVERED"}, 200); recovered["status"] != "RECOVERED" || recovered["alarmType"] != "GAS_LEAK" {
		t.Fatal("custom alarm was not recovered", recovered)
	}
	req("GET", "/api/open/v1/devices/dev_a/latest", key, nil, 200)
	req("GET", "/api/open/v1/devices/dev_b/latest", key, nil, 403)
	devices := req("GET", "/api/open/v1/devices", key, nil, 200)
	if devices["total"].(float64) != 1 {
		t.Fatal("device list exceeded the bound user's scope", devices)
	}

	req("PUT", "/api/v1/access/api-keys/"+keyID, admin, map[string]any{"name": "外部平台", "capabilities": []string{"alarms:read"}, "enabled": false}, 200)
	req("GET", "/api/open/v1/me", key, nil, 401)
	req("PUT", "/api/v1/access/api-keys/"+keyID, admin, map[string]any{"name": "外部平台", "capabilities": []string{"alarms:read"}, "enabled": true}, 200)
	req("GET", "/api/open/v1/me", key, nil, 200)
	req("DELETE", "/api/v1/access/users/ext_system", admin, nil, 200)
	req("GET", "/api/open/v1/me", key, nil, 401)
	if state, _ := repo.LoadAccessState(ctx, tenant); len(state.APIKeys) != 0 {
		t.Fatal("deleting a user kept its API keys")
	}
}
