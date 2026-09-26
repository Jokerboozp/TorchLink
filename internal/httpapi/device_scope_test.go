package httpapi

import (
	"context"
	"fmt"
	"io"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDeviceScopeHTTPIsolation(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	now := time.Now().UnixMilli()
	cfg := config.Load()
	cfg.AdminUser = "root"
	cfg.AdminPassword = "scope-root-test"
	cfg.AdminTenants = []string{"tenant_a", "tenant_b"}
	cfg.JWTSecret = "scope-test-secret-at-least-32-bytes"
	cfg.DevMode = true
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	must(repo.SaveProduct(ctx, model.Product{TenantID: "tenant_a", ID: "product", Name: "演示产品"}))
	for i := 0; i < 46; i++ {
		id := fmt.Sprintf("device-%02d", i)
		must(repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant_a", ID: id, AccessKey: id, Name: id, ProductID: "product", DeviceRole: "DIRECT"}))
		must(repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "tenant_a", DeviceID: id, ProductID: "product", BusinessStatus: "ONLINE"}))
		_, _, e := repo.UpsertAlarm(ctx, model.Alarm{TenantID: "tenant_a", ID: "alarm-" + id, DeviceID: id, RuleID: id, Status: "ACTIVE", AlarmLevel: "HIGH", FirstTriggeredAt: now, LastTriggeredAt: now})
		must(e)
		_, e = repo.SaveRawIndex(ctx, model.RawArchiveIndex{TenantID: "tenant_a", MessageID: "raw-" + id, DeviceID: id, ReceivedAt: now})
		must(e)
	}
	must(repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant_b", ID: "foreign-device", AccessKey: "foreign-key", Name: "其他租户设备"}))
	engine := &core.Engine{Repo: repo, Clock: ports.RealClock{}}
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()
	req := func(method, path, token string, body any, status int) map[string]any {
		t.Helper()
		return requestJSON(t, srv.Client(), method, srv.URL+path, token, body, status)
	}
	login := func(name string) string {
		t.Helper()
		return req("POST", "/api/v1/auth/login", "", map[string]any{"username": name, "password": "scope-password-test", "tenantId": "tenant_a"}, 200)["accessToken"].(string)
	}
	root := req("POST", "/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "tenant_a"}, 200)["accessToken"].(string)
	_, _, err := repo.UpsertAlarm(ctx, model.Alarm{TenantID: "tenant_b", ID: "foreign-alarm", DeviceID: "foreign-device", RuleID: "foreign", Status: "ACTIVE"})
	must(err)
	adminEvents := req("GET", "/api/v1/events", root, nil, 200)
	if len(adminEvents["alarms"].([]any)) != 46 || len(adminEvents["devices"].([]any)) != 46 {
		t.Fatal("administrator events must contain only the current tenant's data", adminEvents)
	}
	if permissions := adminEvents["permissions"].([]any); len(permissions) != 1 || permissions[0] != "*" {
		t.Fatal("administrator permissions changed", permissions)
	}
	for _, item := range adminEvents["alarms"].([]any) {
		if item.(map[string]any)["tenantId"] != "tenant_a" {
			t.Fatal("administrator events leak another tenant")
		}
	}
	perms := []string{"menu:devices", "menu:alarms", "menu:dashboard", "menu:raw", "menu:ai", "menu:inspection", "menu:backups", "POST /api/v1/alarms/:id/actions"}
	ids := []string{}
	for i := 0; i < 46; i += 2 {
		ids = append(ids, fmt.Sprintf("device-%02d", i))
	}
	u := map[string]any{"username": "scope-user", "password": "scope-password-test", "enabled": true, "permissions": perms, "deviceScope": "selected", "deviceIds": ids}
	req("POST", "/api/v1/access/users", root, u, 200)
	token := login("scope-user")
	list := req("GET", "/api/v1/device-registry?page=2&pageSize=20", token, nil, 200)
	if list["total"].(float64) != 23 || len(list["items"].([]any)) != 3 {
		t.Fatalf("scope pagination: %v", list)
	}
	for _, item := range list["items"].([]any) {
		id := item.(map[string]any)["device"].(map[string]any)["id"].(string)
		n := 0
		fmt.Sscanf(id, "device-%d", &n)
		if n%2 != 0 {
			t.Fatal("ungranted device returned")
		}
	}
	for _, path := range []string{"/api/v1/alarms", "/api/v1/raw-messages", "/api/v1/raw-messages?parseStatus=UNPARSED", "/api/v1/devices"} {
		v := req("GET", path, token, nil, 200)
		if v["total"].(float64) != 23 {
			t.Fatalf("%s total=%v", path, v["total"])
		}
	}
	req("GET", "/api/v1/device-registry/device-01/connection", token, nil, 403)
	req("GET", "/api/v1/devices/device-01/properties/history?property=x", token, nil, 403)
	req("GET", "/api/v1/alarms/alarm-device-01", token, nil, 403)
	req("POST", "/api/v1/alarms/alarm-device-01/actions", token, map[string]string{"action": "ACK"}, 403)
	req("GET", "/api/v1/raw-messages/raw-device-01", token, nil, 404)
	req("GET", "/api/v1/alarms/alarm-device-00", token, nil, 200)
	v := req("GET", "/api/v1/alarms?deviceId=device-01", token, nil, 200)
	if v["total"].(float64) != 0 {
		t.Fatal("query bypass")
	}
	v = req("GET", "/api/v1/dashboard", token, nil, 200)
	if v["devices"].(float64) != 23 || v["activeAlarms"].(float64) != 23 {
		t.Fatal("dashboard leaks outside scope", v)
	}
	v = req("GET", "/api/v1/events", token, nil, 200)
	if len(v["alarms"].([]any)) != 23 || len(v["devices"].([]any)) != 23 {
		t.Fatal("events leak scope")
	}
	req("POST", "/api/v1/mqtt/token", token, nil, 403)
	req("POST", "/api/v1/mqtt/load-token", token, nil, 403)
	req("GET", "/api/v1/backups", token, nil, 403)
	req("POST", "/api/v1/ai/chat", token, map[string]string{"question": "列出所有设备"}, 403)
	// Even a broad dashboard/alarms grant cannot replace device access.
	u["permissions"] = []string{"menu:alarms", "menu:dashboard"}
	u["deviceScope"] = "all"
	req("PUT", "/api/v1/access/users/scope-user", root, u, 200)
	req("GET", "/api/v1/events", token, nil, 401)
	token = login("scope-user")
	v = req("GET", "/api/v1/events", token, nil, 200)
	if len(v["alarms"].([]any)) != 0 || len(v["devices"].([]any)) != 0 {
		t.Fatal("missing device menu must mean no device data")
	}
	v = req("GET", "/api/v1/dashboard", token, nil, 200)
	if v["devices"].(float64) != 0 || v["activeAlarms"].(float64) != 0 {
		t.Fatal("dashboard ignores device menu")
	}
	// Cross-tenant grants rejected by storage lookup, not only the picker.
	u["deviceScope"] = "selected"
	u["deviceIds"] = []string{"foreign-device"}
	req("PUT", "/api/v1/access/users/scope-user", root, u, 422)
	// Existing users with no explicit data grant fail closed.
	u["permissions"] = perms
	delete(u, "deviceScope")
	delete(u, "deviceIds")
	req("PUT", "/api/v1/access/users/scope-user", root, u, 200)
	token = login("scope-user")
	v = req("GET", "/api/v1/device-registry", token, nil, 200)
	if v["total"].(float64) != 0 {
		t.Fatal("missing scope defaults to all")
	}
	v = req("GET", "/api/v1/device-registry", root, nil, 200)
	if v["total"].(float64) != 46 {
		t.Fatal("request scope contaminated administrator")
	}
	rows, e := engine.Repo.ListManagedDevices(ctx, "tenant_a")
	must(e)
	if len(rows) != 46 {
		t.Fatal("request scope contaminated background ingest")
	}
}
