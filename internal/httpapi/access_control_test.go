package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"iot-platform/internal/aiworkflow"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/knowledge"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/aitest"
	"iot-platform/internal/auth"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/ports"

	"golang.org/x/crypto/bcrypt"
)

func TestAccessControlLifecycleAndIsolation(t *testing.T) {
	repo := memory.NewRepository()
	cfg := config.Load()
	cfg.AdminUser = "root"
	cfg.AdminPassword = "root-password-test"
	cfg.AdminTenants = []string{"tenant_a", "tenant_b"}
	cfg.JWTSecret = "test-only-secret-for-iam-at-least-32"
	cfg.DevMode = true
	engine := &core.Engine{Repo: repo}
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	req := func(method, path, token string, body any, status int) map[string]any {
		return requestJSON(t, server.Client(), method, server.URL+path, token, body, status)
	}
	login := func(user, password, tenant string, status int) map[string]any {
		return req("POST", "/api/v1/auth/login", "", map[string]any{"username": user, "password": password, "tenantId": tenant}, status)
	}
	root := login("root", cfg.AdminPassword, "tenant_a", 200)["accessToken"].(string)
	role := map[string]any{"id": "device_reader", "name": "设备查看", "permissions": []string{"menu:devices"}}
	req("POST", "/api/v1/access/roles", root, role, 200)
	user := map[string]any{"username": "demo_user", "displayName": "演示用户", "password": "initial-password-test", "enabled": true, "roleIds": []string{"device_reader"}, "permissions": []string{}, "deviceScope": "all"}
	req("POST", "/api/v1/access/users", root, user, 200)
	token := login("demo_user", "initial-password-test", "tenant_a", 200)["accessToken"].(string)
	login("demo_user", "initial-password-test", "tenant_b", 401)
	req("GET", "/api/v1/device-registry", token, nil, 200)
	req("GET", "/api/v1/products", token, nil, 200) // Read-only lookup for device editor.
	req("POST", "/api/v1/device-registry", token, map[string]string{}, 403)
	req("GET", "/api/v1/access/users", token, nil, 403)
	req("GET", "/api/v2/protocols/test/releases/v1/source", token, nil, 403)
	req("POST", "/mcp", token, map[string]string{}, 403)
	state, err := repo.LoadAccessState(context.Background(), "tenant_a")
	if err != nil || state.Users[0].PasswordHash == "initial-password-test" || state.Users[0].PasswordHash == "" {
		t.Fatal("password is not hashed")
	}
	list := req("GET", "/api/v1/access/users", root, nil, 200)
	for _, u := range list["items"].([]any) {
		if _, ok := u.(map[string]any)["passwordHash"]; ok {
			t.Fatal("password hash exposed")
		}
	}
	// Permissions are loaded on every request, so existing tokens reflect role edits.
	role["permissions"] = []string{"menu:devices", "POST /api/v1/device-registry"}
	req("PUT", "/api/v1/access/roles/device_reader", root, role, 200)
	req("POST", "/api/v1/device-registry", token, map[string]string{}, 422)
	role["permissions"] = []string{}
	req("PUT", "/api/v1/access/roles/device_reader", root, role, 200)
	req("GET", "/api/v1/device-registry", token, nil, 403)
	for _, permission := range api.permissionCatalog() {
		if permission.Kind != "action" {
			continue
		}
		parts := strings.SplitN(permission.ID, " ", 2)
		path := regexp.MustCompile(`:[A-Za-z]+`).ReplaceAllString(parts[1], "denied-test")
		req(parts[0], path, token, map[string]any{}, 403)
	}
	role["permissions"] = []string{"*"}
	req("PUT", "/api/v1/access/roles/device_reader", root, role, 422)
	role["permissions"] = []string{}
	req("DELETE", "/api/v1/access/roles/device_reader", root, nil, 409)
	req("POST", "/api/v1/access/users/demo_user/password", root, map[string]string{"password": "replacement-password-test"}, 200)
	req("GET", "/api/v1/auth/me", token, nil, 401)
	login("demo_user", "initial-password-test", "tenant_a", 401)
	token = login("demo_user", "replacement-password-test", "tenant_a", 200)["accessToken"].(string)
	user["enabled"] = false
	req("PUT", "/api/v1/access/users/demo_user", root, user, 200)
	req("GET", "/api/v1/auth/me", token, nil, 401)
	login("demo_user", "replacement-password-test", "tenant_a", 401)
	otherRoot := login("root", cfg.AdminPassword, "tenant_b", 200)["accessToken"].(string)
	req("DELETE", "/api/v1/access/users/demo_user", otherRoot, nil, 404)
	req("DELETE", "/api/v1/access/users/demo_user", root, nil, 200)
	user["enabled"] = true
	req("POST", "/api/v1/access/users", root, user, 200)
	req("GET", "/api/v1/auth/me", token, nil, 401)
	// Old revisions may not overwrite concurrent changes.
	if ok, err := repo.SaveAccessState(context.Background(), "tenant_a", state); err != nil || ok {
		t.Fatal("stale access state was accepted")
	}
	broker, err := api.auth.IssueBrowserMQTT("demo_user", "tenant_a", []string{"/iot/alarm/tenant_a/#"}, 1000000000)
	if err != nil {
		t.Fatal(err)
	}
	req("GET", "/api/v1/device-registry", broker, nil, 403)
	// Console broker credentials never collide with broker built-in accounts
	// (the MQTT tool account defaults to the admin name) and never work as
	// console tokens, for the built-in administrator either.
	grant := req("POST", "/api/v1/mqtt/token", root, nil, 200)
	if grant["username"] != "web:root" {
		t.Fatalf("broker username = %v", grant["username"])
	}
	claims, err := api.auth.Parse(grant["token"].(string))
	if err != nil || claims.Username != "web:root" || claims.TokenUse != "browser-mqtt" {
		t.Fatalf("broker claims = %+v, %v", claims, err)
	}
	req("GET", "/api/v1/device-registry", grant["token"].(string), nil, 403)
}

func TestUserPermissionsCombineRolesAndIndividualGrants(t *testing.T) {
	state := model.AccessState{Roles: []model.PlatformRole{{ID: "reader", Permissions: []string{"menu:devices"}}}}
	viewer := model.PlatformUser{DeviceScope: "all", RoleIDs: []string{"reader"}}
	editor := model.PlatformUser{DeviceScope: "all", RoleIDs: []string{"reader"}, Permissions: []string{"POST /api/v1/device-registry"}}
	if allowsRoute(effectivePermissions(state, viewer), "POST", "/api/v1/device-registry") {
		t.Fatal("viewer unexpectedly inherited another user's permission")
	}
	if !allowsRoute(effectivePermissions(state, editor), "POST", "/api/v1/device-registry") {
		t.Fatal("individual grant missing")
	}
	if !allowsRoute(effectivePermissions(state, editor), "GET", "/api/v1/device-registry") {
		t.Fatal("role menu missing")
	}
}

func TestRoleDeviceScopeResolution(t *testing.T) {
	state := model.AccessState{Roles: []model.PlatformRole{
		{ID: "east", DeviceScope: "selected", DeviceIDs: []string{"east", "shared"}},
		{ID: "west", DeviceScope: "selected", DeviceIDs: []string{"west", "shared"}},
		{ID: "admin", DeviceScope: "all"},
		{ID: "legacy"},
	}}
	for _, tc := range []struct {
		name, scope string
		roles, ids  []string
		want        string
		wantIDs     []string
	}{
		{"union", "inherit", []string{"east", "west"}, nil, "selected", []string{"east", "shared", "west"}},
		{"all", "inherit", []string{"east", "admin"}, nil, "all", []string{}},
		{"missing role", "inherit", []string{"missing", "legacy"}, []string{"stale"}, "none", []string{}},
		{"unassigned", "inherit", nil, nil, "none", []string{}},
		{"legacy", "", []string{"admin"}, nil, "", nil},
		{"explicit none", "none", []string{"admin"}, nil, "none", nil},
		{"explicit selected", "selected", []string{"admin"}, []string{"own"}, "selected", []string{"own"}},
		{"explicit all", "all", []string{"east"}, nil, "all", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := model.PlatformUser{RoleIDs: tc.roles, DeviceScope: tc.scope, DeviceIDs: tc.ids}
			got := resolveUserDeviceScope(state, user)
			if got.DeviceScope != tc.want || !reflect.DeepEqual(got.DeviceIDs, tc.wantIDs) {
				t.Fatalf("scope=%s ids=%v", got.DeviceScope, got.DeviceIDs)
			}
			if user.DeviceScope != tc.scope || !reflect.DeepEqual(user.DeviceIDs, tc.ids) {
				t.Fatal("stored user mutated")
			}
		})
	}
	// Tenant-wide menus require the resolved all-device scope, not the raw inherit value.
	user := model.PlatformUser{DeviceScope: "inherit", RoleIDs: []string{"admin"}, Permissions: []string{"menu:devices", "menu:backups"}}
	if !effectivePermissions(state, user)["menu:backups"] {
		t.Fatal("inherited all-device menu lost")
	}
	user.RoleIDs = []string{"east"}
	if effectivePermissions(state, user)["menu:backups"] {
		t.Fatal("limited scope grants tenant-wide menu")
	}
}

func TestDeviceScopeHTTPIsolation(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	now := time.Now().UnixMilli()
	cfg := config.Load()
	cfg.AdminUser = "root"
	cfg.AdminPassword = "scope-root-test"
	cfg.AdminTenants = []string{"tenant_a", "tenant_b"}
	// This test isolates the device-scope prerequisite, after the platform
	// tenant prerequisite for assigning backup permissions has been met.
	cfg.Ops.Tenants = []string{"tenant_a"}
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
	if len(adminEvents["alarms"].([]any)) != 46 || len(adminEvents["devices"].([]any)) != 46 || adminEvents["deviceTotal"] != float64(46) {
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
	if len(v["alarms"].([]any)) != 23 || len(v["devices"].([]any)) != 23 || v["deviceTotal"] != float64(23) {
		t.Fatal("events leak scope")
	}
	req("POST", "/api/v1/mqtt/token", token, nil, 403)
	req("POST", "/api/v1/mqtt/load-token", token, nil, 403)
	req("GET", "/api/v1/backups", token, nil, 403)
	req("POST", "/api/v1/ai/chat", token, map[string]string{"question": "列出所有设备"}, 403)
	// Conversations follow the assistant permission, not the menu alone.
	req("GET", "/api/v1/ai/conversations?workflowId=ops-assistant", token, nil, 403)
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

func TestOnboardingFollowsDevicePermissions(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword = "root", "root-password-test"
	cfg.AdminTenants = []string{"tenant_a"}
	cfg.JWTSecret = "test-only-secret-for-onboarding-access"
	cfg.DevMode = true
	api := New(cfg, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	req := func(method, path, token string, body any, status int) map[string]any {
		return requestJSON(t, server.Client(), method, server.URL+path, token, body, status)
	}
	login := func(user, password string) string {
		return req("POST", "/api/v1/auth/login", "", map[string]any{"username": user, "password": password, "tenantId": "tenant_a"}, 200)["accessToken"].(string)
	}
	if err := repo.SaveProduct(ctx, model.Product{TenantID: "tenant_a", ID: "product", Name: "温度", Status: "ENABLED", ProtocolPackageID: onboarding.StandardPackageID, Transport: "HTTP"}); err != nil {
		t.Fatal(err)
	}
	enroll := func(id string, newProduct bool) map[string]any {
		body := map[string]any{"requestId": "req-" + id, "productId": "product", "device": map[string]any{"id": id, "name": id}, "connection": map[string]any{"mode": "standard"}}
		if newProduct {
			delete(body, "productId")
			body["newProduct"] = map[string]any{"id": "template-" + id, "name": "新模板", "protocolPackageId": onboarding.StandardPackageID}
		}
		return body
	}
	root := login("root", cfg.AdminPassword)
	role := map[string]any{"id": "installer", "name": "安装人员", "permissions": []string{"menu:devices", "POST /api/v1/device-registry"}}
	req("POST", "/api/v1/access/roles", root, role, 200)
	user := map[string]any{"username": "installer", "displayName": "安装人员", "password": "installer-password", "enabled": true, "roleIds": []string{"installer"}, "permissions": []string{}, "deviceScope": "all"}
	req("POST", "/api/v1/access/users", root, user, 200)
	token := login("installer", "installer-password")

	// Adding a device uses the ordinary add-device permission.
	// An unverified template is unavailable even with registration permission.
	req("POST", "/api/v1/onboarding", token, enroll("device-1", false), 409)
	readyTemplateFixture(t, api, "tenant_a", "product")
	req("GET", "/api/v1/onboarding/preflight?productId=product", token, nil, 200)
	req("POST", "/api/v1/onboarding", token, enroll("device-1", false), 201)
	req("POST", "/api/v1/onboarding", token, enroll("device-2", true), 403)
	role["permissions"] = []string{"menu:devices", "POST /api/v1/device-registry", "menu:products", "POST /api/v1/products"}
	req("PUT", "/api/v1/access/roles/installer", root, role, 200)
	req("POST", "/api/v1/onboarding", token, enroll("device-2", true), 201)
	listener := enroll("device-3", false)
	listener["connection"] = map[string]any{"mode": "listener", "listener": map[string]any{"publicHost": "iot.example.com", "port": 9100}}
	req("POST", "/api/v1/onboarding", token, listener, 403)

	role["permissions"] = []string{"menu:devices"}
	req("PUT", "/api/v1/access/roles/installer", root, role, 200)
	req("POST", "/api/v1/onboarding", token, enroll("device-4", false), 403)

	// Users limited to selected devices cannot add devices or inspect templates here.
	role["permissions"] = []string{"menu:devices", "POST /api/v1/device-registry"}
	req("PUT", "/api/v1/access/roles/installer", root, role, 200)
	user["deviceScope"], user["deviceIds"] = "selected", []string{"device-1"}
	req("PUT", "/api/v1/access/users/installer", root, user, 200)
	limited := login("installer", "installer-password")
	req("GET", "/api/v1/onboarding/preflight?productId=product", limited, nil, 403)
	req("POST", "/api/v1/onboarding", limited, enroll("device-5", false), 403)
	if _, err := repo.GetManagedDevice(ctx, "tenant_a", "device-5"); err == nil {
		t.Fatal("limited user added a device")
	}

	// The wizard has no separate grant, and removed routes are hidden from saved roles.
	for _, item := range api.permissionCatalog() {
		if strings.Contains(item.ID, "/onboarding") {
			t.Fatalf("catalog lists %s", item.ID)
		}
	}
	state, err := repo.LoadAccessState(ctx, "tenant_a")
	if err != nil {
		t.Fatal(err)
	}
	state.Roles[0].Permissions = append(state.Roles[0].Permissions, "POST /api/v1/onboarding/test")
	if ok, err := repo.SaveAccessState(ctx, "tenant_a", state); err != nil || !ok {
		t.Fatal("seed stale permission", err)
	}
	for _, item := range req("GET", "/api/v1/access/roles", root, nil, 200)["items"].([]any) {
		permissions := []string{}
		for _, v := range item.(map[string]any)["permissions"].([]any) {
			permissions = append(permissions, v.(string))
		}
		if slices.Contains(permissions, "POST /api/v1/onboarding/test") || !slices.Contains(permissions, "POST /api/v1/device-registry") {
			t.Fatalf("role permissions %v", permissions)
		}
	}
}

func TestAssistantUsesCurrentUserPermissionsAndDeviceScope(t *testing.T) {
	for _, inherited := range []bool{false, true} {
		name := "user"
		if inherited {
			name = "role"
		}
		t.Run(name, func(t *testing.T) { testAssistantDeviceScope(t, inherited) })
	}
}

func testAssistantDeviceScope(t *testing.T, inherited bool) {
	ctx := context.Background()
	repo := memory.NewRepository()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []struct{ tenant, id string }{{"tenant-a", "allowed"}, {"tenant-a", "hidden"}, {"tenant-b", "foreign"}} {
		must(repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: d.tenant, ID: d.id, AccessKey: d.id, Name: d.id}))
		must(repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: d.tenant, DeviceID: d.id, BusinessStatus: "ONLINE"}))
		_, _, err := repo.UpsertAlarm(ctx, model.Alarm{TenantID: d.tenant, ID: "alarm-" + d.id, DeviceID: d.id, RuleID: d.id, Status: "ACTIVE", LastTriggeredAt: time.Now().UnixMilli()})
		must(err)
	}
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword = "root", "scope-root-password"
	cfg.AdminTenants = []string{"tenant-a", "tenant-b"}
	cfg.JWTSecret = "scope-assistant-secret-at-least-32-bytes"
	runtime := &captureWorkflowRuntime{}
	engine := &core.Engine{Repo: repo, Clock: ports.RealClock{}, AIWorkflows: runtime}
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()
	req := func(method, path, token string, body any, status int) map[string]any {
		t.Helper()
		return requestJSON(t, srv.Client(), method, srv.URL+path, token, body, status)
	}
	login := func(user, password string) map[string]any {
		return req("POST", "/api/v1/auth/login", "", map[string]any{"username": user, "password": password, "tenantId": "tenant-a"}, 200)
	}
	root := login("root", cfg.AdminPassword)["accessToken"].(string)
	base := []string{"menu:devices", "menu:ai", "POST /api/v1/ai/chat", "POST /api/v1/ai/chat/stream"}
	role := map[string]any{"id": "reader", "name": "设备查看", "permissions": append(append([]string{}, base...), "menu:alarms", "menu:dashboard")}
	if inherited {
		role["deviceScope"], role["deviceIds"] = "selected", []string{"foreign"}
		req("POST", "/api/v1/access/roles", root, role, 422)
		role["deviceScope"] = "inherit"
		req("POST", "/api/v1/access/roles", root, role, 422)
		role["deviceScope"], role["deviceIds"] = "selected", []string{"allowed"}
	}
	req("POST", "/api/v1/access/roles", root, role, 200)
	user := map[string]any{"username": "reader", "password": "scope-reader-password", "enabled": true, "roleIds": []string{"reader"}, "deviceScope": "selected", "deviceIds": []string{"allowed"}}
	if inherited {
		user["deviceScope"] = "inherit"
	}
	req("POST", "/api/v1/access/users", root, user, 200)
	identity := login("reader", "scope-reader-password")
	token := identity["accessToken"].(string)
	if identity["accessVersion"] == "" || identity["accessVersion"] != req("GET", "/api/v1/auth/me", token, nil, 200)["accessVersion"] {
		t.Fatal("inconsistent authorization version")
	}
	chat := func(token string) ports.AIWorkflowRequest {
		t.Helper()
		req("POST", "/api/v1/ai/chat", token, map[string]any{"question": "查询所有设备和告警", "workflowId": "system-observer", "conversationId": "same-browser-conversation"}, 200)
		runtime.mu.Lock()
		defer runtime.mu.Unlock()
		return runtime.requests[len(runtime.requests)-1]
	}
	tool := func(token, name string, args map[string]any, wantError bool) string {
		t.Helper()
		reply := req("POST", "/mcp/harness", token, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}, 200)
		result, ok := reply["result"].(map[string]any)
		if !ok {
			t.Fatalf("invalid MCP reply: %v", reply)
		}
		failed, _ := result["isError"].(bool)
		if failed != wantError {
			t.Fatalf("%s error=%v want=%v: %v", name, failed, wantError, result)
		}
		content := result["content"].([]any)
		return content[0].(map[string]any)["text"].(string)
	}
	first := chat(token)
	streamReq, err := http.NewRequest("POST", srv.URL+"/api/v1/ai/chat/stream", strings.NewReader(`{"question":"查询告警","workflowId":"system-observer","conversationId":"stream-conversation"}`))
	must(err)
	streamReq.Header.Set("Authorization", "Bearer "+token)
	streamReq.Header.Set("Content-Type", "application/json")
	streamResp, err := srv.Client().Do(streamReq)
	must(err)
	streamBody, err := io.ReadAll(streamResp.Body)
	must(err)
	streamResp.Body.Close()
	if streamResp.StatusCode != 200 || !strings.Contains(string(streamBody), "run.completed") {
		t.Fatalf("stream failed: %s", streamBody)
	}
	runtime.mu.Lock()
	streamed := runtime.requests[len(runtime.requests)-1]
	runtime.mu.Unlock()
	if text := tool(streamed.MCPToken, "query_alarm_list", nil, false); strings.Contains(text, "hidden") {
		t.Fatal("streaming authority leaked", text)
	}

	c, err := api.harnessAuth.Parse(first.MCPToken)
	must(err)
	if !c.ManagedUser || c.SessionVersion == 0 || c.HasScope(auth.ScopeCreateRuleDraft) || c.HasScope(auth.ScopeQueryKnowledgeBase) {
		t.Fatal("assistant token does not retain user authority")
	}
	for _, name := range []string{"query_alarm_list", "query_similar_alarms"} {
		text := tool(first.MCPToken, name, map[string]any{}, false)
		if !strings.Contains(text, "alarm-allowed") || strings.Contains(text, "hidden") || strings.Contains(text, "foreign") {
			t.Fatalf("%s leaked data: %s", name, text)
		}
		if text = tool(first.MCPToken, name, map[string]any{"deviceId": "hidden"}, false); !strings.Contains(text, `"items":[]`) || !strings.Contains(text, `"total":0`) {
			t.Fatal("device filter bypass", text)
		}
	}
	if text := tool(first.MCPToken, "query_alarm_detail", map[string]any{"alarmId": "alarm-allowed"}, false); !strings.Contains(text, `"deviceId":"allowed"`) {
		t.Fatal("granted alarm detail unavailable", text)
	}
	for _, id := range []string{"alarm-hidden", "alarm-foreign"} {
		tool(first.MCPToken, "query_alarm_detail", map[string]any{"alarmId": id}, true)
	}
	tool(first.MCPToken, "query_device_latest", map[string]any{"deviceId": "allowed"}, false)
	for _, id := range []string{"hidden", "foreign"} {
		tool(first.MCPToken, "query_device_latest", map[string]any{"deviceId": id}, true)
		tool(first.MCPToken, "query_property_history", map[string]any{"deviceId": id, "propertyCode": "temperature", "start": 0, "end": time.Now().UnixMilli()}, true)
	}
	overview := map[string]any{}
	must(json.Unmarshal([]byte(tool(first.MCPToken, "query_system_overview", nil, false)), &overview))
	if overview["devices"].(map[string]any)["total"] != float64(1) || overview["alarms"].(map[string]any)["total"] != float64(1) {
		t.Fatal("overview leaked device scope", overview)
	}
	for _, field := range []string{"rules", "products", "protocolPackages", "cameras", "knowledge"} {
		if _, ok := overview[field]; ok {
			t.Fatal("overview bypassed menu permission", field)
		}
	}
	tool(first.MCPToken, "create_rule_draft", map[string]any{"inputText": "创建规则"}, true)
	tool(first.MCPToken, "query_knowledge_base", map[string]any{"question": "秘密", "workflowId": "other"}, true)
	alarms := req("GET", "/api/v1/alarms?pageSize=1", token, nil, 200)
	if alarms["total"] != float64(1) || len(alarms["items"].([]any)) != 1 {
		t.Fatal("alarm scope", alarms)
	}
	events := req("GET", "/api/v1/events", token, nil, 200)
	if len(events["alarms"].([]any)) != 1 {
		t.Fatal("events scope", events)
	}
	req("GET", "/api/v1/alarms/alarm-hidden", token, nil, 403)
	req("POST", "/api/v1/ai/reports", token, nil, 403)
	req("GET", "/api/v1/rules", token, nil, 403)
	if inherited {
		// Same browser and MCP tokens must observe a role's device change immediately.
		role["deviceIds"] = []string{"hidden"}
		req("PUT", "/api/v1/access/roles/reader", root, role, 200)
		tool(first.MCPToken, "query_device_latest", map[string]any{"deviceId": "allowed"}, true)
		tool(first.MCPToken, "query_device_latest", map[string]any{"deviceId": "hidden"}, false)
		if text := tool(first.MCPToken, "query_alarm_list", nil, false); strings.Contains(text, "allowed") || !strings.Contains(text, "alarm-hidden") {
			t.Fatal("role scope not reloaded", text)
		}
		req("GET", "/api/v1/device-registry/allowed/history", token, nil, 403)
		if req("GET", "/api/v1/auth/me", token, nil, 200)["accessVersion"] == identity["accessVersion"] {
			t.Fatal("role scope must invalidate browser history")
		}
		if chat(token).ConversationID == first.ConversationID {
			t.Fatal("role scope must invalidate model history")
		}
		role["deviceIds"] = []string{"allowed"}
		req("PUT", "/api/v1/access/roles/reader", root, role, 200)
	}

	// A role edit applies to already issued MCP credentials and starts fresh context.
	role["permissions"] = base
	req("PUT", "/api/v1/access/roles/reader", root, role, 200)
	tool(first.MCPToken, "query_alarm_list", nil, true)
	tool(first.MCPToken, "query_system_overview", nil, true)
	req("GET", "/api/v1/alarms", token, nil, 403)
	second := chat(token)
	if first.ConversationID == second.ConversationID {
		t.Fatal("old privileged model conversation reused")
	}
	if req("GET", "/api/v1/auth/me", token, nil, 200)["accessVersion"] == identity["accessVersion"] {
		t.Fatal("browser history authorization unchanged")
	}
	// All-device access still does not grant alarm or knowledge menus.
	user["deviceScope"] = "all"
	req("PUT", "/api/v1/access/users/reader", root, user, 200)
	req("POST", "/mcp/harness", first.MCPToken, map[string]any{}, 401)
	token = login("reader", "scope-reader-password")["accessToken"].(string)
	all := chat(token)
	tool(all.MCPToken, "query_device_latest", map[string]any{"deviceId": "hidden"}, false)
	tool(all.MCPToken, "query_alarm_list", nil, true)
	// No device grant remains empty even when the dashboard is assigned.
	role["permissions"] = append(append([]string{}, base...), "menu:dashboard")
	req("PUT", "/api/v1/access/roles/reader", root, role, 200)
	user["deviceScope"] = "none"
	req("PUT", "/api/v1/access/users/reader", root, user, 200)
	token = login("reader", "scope-reader-password")["accessToken"].(string)
	none := chat(token)
	if text := tool(none.MCPToken, "query_alarm_list", nil, false); !strings.Contains(text, `"items":[]`) || !strings.Contains(text, `"total":0`) {
		t.Fatal("missing scope leaks alarms", text)
	}
	tool(none.MCPToken, "query_device_latest", map[string]any{"deviceId": "allowed"}, true)

	// AI-only users may chat, but cannot invoke any data tool.
	role["permissions"] = []string{"menu:ai", "POST /api/v1/ai/chat", "POST /api/v1/ai/chat/stream"}
	req("PUT", "/api/v1/access/roles/reader", root, role, 200)
	aiOnly := chat(token)
	tool(aiOnly.MCPToken, "query_device_latest", map[string]any{"deviceId": "allowed"}, true)
	tool(aiOnly.MCPToken, "query_alarm_list", nil, true)
	// Required knowledge evidence must not trigger an unauthorized prefetch.
	must(repo.SaveWorkflowKnowledgeBinding(ctx, model.WorkflowKnowledgeBinding{TenantID: "tenant-a", WorkflowID: "required-kb", RetrievalMode: "always", NoMatchPolicy: "require-evidence", TopK: 5}))
	req("POST", "/api/v1/ai/chat", token, map[string]any{"question": "查询知识", "workflowId": "required-kb"}, 502)
	// Disabling a user invalidates the already issued MCP credential.
	user["enabled"] = false
	req("PUT", "/api/v1/access/users/reader", root, user, 200)
	req("POST", "/mcp/harness", aiOnly.MCPToken, map[string]any{}, 401)
	// Neither user requests nor the MCP bridge contaminate administrators/background work.
	admin := chat(root)
	if text := tool(admin.MCPToken, "query_alarm_list", nil, false); !strings.Contains(text, "hidden") || strings.Contains(text, "foreign") {
		t.Fatal("administrator tenant scope", text)
	}
	// Managed users cannot fall back to the legacy, tenant-wide knowledge path.
	engine.AIWorkflows = nil
	user["enabled"] = true
	req("PUT", "/api/v1/access/users/reader", root, user, 200)
	token = login("reader", "scope-reader-password")["accessToken"].(string)
	req("POST", "/api/v1/ai/chat", token, map[string]any{"question": "查询告警"}, 503)
	rows, err := engine.Repo.ListAlarms(ctx, ports.AlarmFilter{TenantID: "tenant-a"})
	must(err)
	if len(rows) != 2 {
		t.Fatal("background repository scope contaminated")
	}
}

// Alarm analysis follows the caller's role: knowledge-based results are stored
// beside the knowledge-free one and only roles with knowledge access read them.
func TestAlarmAnalysisKnowledgeVariantFollowsRole(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "tenant-a", ID: "device-a", AccessKey: "device-a", Name: "一层烟感"}))
	_, _, err := repo.UpsertAlarm(ctx, model.Alarm{TenantID: "tenant-a", ID: "alarm-a", DeviceID: "device-a", RuleID: "rule-a", AlarmType: "SMOKE", AlarmLevel: "HIGH", Status: "ACTIVE", LastTriggeredAt: time.Now().UnixMilli()})
	must(err)
	kb := knowledge.NewLocal()
	must(kb.IndexKnowledge(ctx, ports.KnowledgeIndexInput{TenantID: "tenant-a", WorkflowID: model.AlarmAnalysisWorkflowID, DocumentID: "doc-alarm", ChunkID: "doc-alarm-0", Content: []byte("烟感处置 SOP 维修：核实现场")}))
	captured := make(chan string, 4)
	engine := &core.Engine{Repo: repo, Clock: ports.RealClock{}, Bus: local.NewBus(), Realtime: local.NewRealtime(), KB: kb}
	engine.AIWorkflows = &aitest.Workflows{Answer: func(req ports.AIWorkflowRequest) (string, error) {
		captured <- req.Question
		return strings.Replace(testAnalysisAnswer, "研判完成", "手动研判", 1), nil
	}}
	engine.HarnessTokens = aitest.Tokens()

	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword = "root", "scope-root-password"
	cfg.AdminTenants = []string{"tenant-a"}
	cfg.JWTSecret = "alarm-analysis-scope-secret-at-least-32-bytes"
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()
	req := func(method, path, token string, body any, status int) map[string]any {
		t.Helper()
		return requestJSON(t, srv.Client(), method, srv.URL+path, token, body, status)
	}
	login := func(user, password string) string {
		t.Helper()
		return req("POST", "/api/v1/auth/login", "", map[string]any{"username": user, "password": password, "tenantId": "tenant-a"}, 200)["accessToken"].(string)
	}
	root := login("root", cfg.AdminPassword)
	alarmPermissions := []string{"menu:devices", "menu:alarms", "POST /api/v1/ai/alarm-analysis/:alarmId/run"}
	req("POST", "/api/v1/access/roles", root, map[string]any{"id": "alarm-only", "name": "告警处置", "permissions": alarmPermissions}, 200)
	req("POST", "/api/v1/access/roles", root, map[string]any{"id": "alarm-knowledge", "name": "告警与知识库", "permissions": append(append([]string{}, alarmPermissions...), "menu:knowledge")}, 200)
	for user, role := range map[string]string{"plain": "alarm-only", "expert": "alarm-knowledge"} {
		req("POST", "/api/v1/access/users", root, map[string]any{"username": user, "password": user + "-scope-password", "enabled": true, "roleIds": []string{role}, "deviceScope": "selected", "deviceIds": []string{"device-a"}}, 200)
	}
	plain, expert := login("plain", "plain-scope-password"), login("expert", "expert-scope-password")

	// Without Harness the chat list stays empty, but the knowledge page can still
	// manage documents for the alarm analysis Agent.
	if items := req("GET", "/api/v1/ai/workflows", root, nil, 200)["items"].([]any); len(items) != 0 {
		t.Fatalf("chat workbench must not list business Agents: %v", items)
	}
	items := req("GET", "/api/v1/ai/workflows?purpose=knowledge", root, nil, 200)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != model.AlarmAnalysisWorkflowID {
		t.Fatalf("knowledge page must offer the alarm analysis Agent: %v", items)
	}

	// Harness business runs are authorised by the feature's permission, not by
	// the chat assistant permission this role does not have.
	plainClaims, err := api.auth.Parse(plain)
	must(err)
	identity := ports.AIRunIdentity{TenantID: plainClaims.TenantID, Username: "plain", ManagedUser: true, SessionVersion: plainClaims.SessionVersion, Scopes: []string{auth.ScopeQueryAlarmList}}
	callTool := func(token string, status int) {
		t.Helper()
		requestJSON(t, srv.Client(), "POST", srv.URL+"/mcp/harness", token, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "query_alarm_list", "arguments": map[string]any{}}}, status)
	}
	businessToken, err := api.harnessAuth.IssueBusinessRunToken("tenant-a", identity, "run-business", aiworkflow.WorkflowAlarmAnalysis, identity.Scopes, nil, time.Minute)
	must(err)
	callTool(businessToken, 200)
	// Run credentials use their own key: one signed with the session key fails.
	sessionSigned, err := api.auth.IssueBusinessRunToken("tenant-a", identity, "run-session-key", aiworkflow.WorkflowAlarmAnalysis, identity.Scopes, nil, time.Minute)
	must(err)
	callTool(sessionSigned, 401)
	chatToken, err := api.harnessAuth.IssueHarnessForIdentity(plainClaims, "run-chat", identity.Scopes, nil, time.Minute)
	must(err)
	callTool(chatToken, 403)
	draftToken, err := api.harnessAuth.IssueBusinessRunToken("tenant-a", identity, "run-draft", aiworkflow.WorkflowRuleDraft, identity.Scopes, nil, time.Minute)
	must(err)
	callTool(draftToken, 403)

	// A knowledge-based result alone is invisible to a role without knowledge access.
	must(repo.SaveAIAnalysis(ctx, model.AIAnalysis{TenantID: "tenant-a", AlarmID: "alarm-a", Summary: "引用知识", KnowledgeScope: model.AlarmAnalysisWorkflowID, CreatedAt: 2000}))
	req("GET", "/api/v1/ai/alarm-analysis/alarm-a", plain, nil, 404)
	if got := req("GET", "/api/v1/ai/alarm-analysis/alarm-a", expert, nil, 200)["summary"]; got != "引用知识" {
		t.Fatalf("knowledge role must read the knowledge variant, got %v", got)
	}
	must(repo.SaveAIAnalysis(ctx, model.AIAnalysis{TenantID: "tenant-a", AlarmID: "alarm-a", Summary: "未引用知识", CreatedAt: 1000}))
	if got := req("GET", "/api/v1/ai/alarm-analysis/alarm-a", plain, nil, 200)["summary"]; got != "未引用知识" {
		t.Fatalf("plain role must read the knowledge-free variant, got %v", got)
	}
	// Results stored before scoped retrieval may hold tenant-wide knowledge.
	must(repo.SaveAIAnalysis(ctx, model.AIAnalysis{TenantID: "tenant-a", AlarmID: "alarm-a", Summary: "历史结果", KnowledgeScope: model.AIAnalysisScopeLegacyTenant, CreatedAt: 3000}))
	if got := req("GET", "/api/v1/ai/alarm-analysis/alarm-a", plain, nil, 200)["summary"]; got != "未引用知识" {
		t.Fatalf("legacy tenant-knowledge results must stay hidden from plain roles, got %v", got)
	}

	run := func(token string) (map[string]any, string) {
		t.Helper()
		job := req("POST", "/api/v1/ai/alarm-analysis/alarm-a/run", token, map[string]any{}, 202)
		var knowledge string
		select {
		case knowledge = <-captured:
		case <-time.After(2 * time.Second):
			t.Fatal("analysis job did not call the model")
		}
		for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			progress := req("GET", "/api/v1/ai/alarm-analysis/alarm-a/progress/"+job["jobId"].(string), token, nil, 200)
			if progress["status"] != "running" {
				return progress, knowledge
			}
		}
		t.Fatal("analysis job did not finish")
		return nil, ""
	}
	progress, knowledge := run(plain)
	if strings.Contains(knowledge, "核实现场") || progress["analysis"].(map[string]any)["knowledgeScope"] != nil {
		t.Fatalf("plain role run must not use knowledge: knowledge=%v progress=%v", knowledge, progress)
	}
	progress, knowledge = run(expert)
	if !strings.Contains(knowledge, "核实现场") || progress["analysis"].(map[string]any)["knowledgeScope"] != model.AlarmAnalysisWorkflowID {
		t.Fatalf("knowledge role run must use alarm-handler knowledge: knowledge=%v progress=%v", knowledge, progress)
	}
	if got := req("GET", "/api/v1/ai/alarm-analysis/alarm-a", plain, nil, 200)["summary"]; got != "手动研判" {
		t.Fatalf("plain role should see its own newest knowledge-free run, got %v", got)
	}
	saved, err := repo.GetAIAnalysis(ctx, "tenant-a", "alarm-a", model.AlarmAnalysisWorkflowID)
	must(err)
	if strings.Join(saved.KnowledgeDocuments, ",") != "doc-alarm" {
		t.Fatalf("knowledge run did not record its source documents: %#v", saved)
	}
}

type noFullRegistryRepo struct {
	*memory.Repository
	fullReads int
}

func (r *noFullRegistryRepo) ListManagedDevices(ctx context.Context, tenant string) ([]model.ManagedDevice, error) {
	r.fullReads++
	return r.Repository.ListManagedDevices(ctx, tenant)
}
func TestScopedChildCountsStayInStorage(t *testing.T) {
	base := &noFullRegistryRepo{Repository: memory.NewRepository()}
	ctx := context.WithValue(context.Background(), deviceScopeKey{}, deviceScope{Tenant: "t", IDs: map[string]bool{"g": true, "c": true, "x": true}})
	for _, d := range []model.ManagedDevice{{TenantID: "t", ID: "c", GatewayID: "g"}, {TenantID: "t", ID: "hidden", GatewayID: "g"}, {TenantID: "t", ID: "x", GatewayID: "other"}} {
		_ = base.SaveManagedDevice(ctx, d)
	}
	counts, err := ScopedRepository(base).CountManagedDeviceChildren(ctx, "t", []string{"g"})
	if err != nil || counts["g"] != 1 || len(counts) != 1 || base.fullReads != 0 {
		t.Fatalf("counts=%v fullRegistryReads=%d err=%v", counts, base.fullReads, err)
	}
}

// Children and state counts of a limited user are paged and counted by the
// store over the grant, without reading the tenant's registry.
func TestScopedChildrenAndStateCountsStayInStorage(t *testing.T) {
	base := &noFullRegistryRepo{Repository: memory.NewRepository()}
	ctx := context.WithValue(context.Background(), deviceScopeKey{}, deviceScope{Tenant: "t", IDs: map[string]bool{"g": true, "c1": true, "c3": true}})
	for _, d := range []model.ManagedDevice{{ID: "g"}, {ID: "c1", GatewayID: "g"}, {ID: "c2", GatewayID: "g"}, {ID: "c3", GatewayID: "g"}} {
		d.TenantID, d.AccessKey = "t", "ak-"+d.ID
		_ = base.SaveManagedDevice(ctx, d)
		_ = base.UpsertDeviceState(ctx, model.DeviceState{TenantID: "t", DeviceID: d.ID, BusinessStatus: "ONLINE"})
	}
	repo := ScopedRepository(base)
	page, total, err := repo.ListManagedDeviceChildren(ctx, "t", "g", 1, 1)
	if err != nil || total != 2 || len(page) != 1 || page[0].ID != "c3" || base.fullReads != 0 {
		t.Fatalf("children page=%v total=%d reads=%d err=%v", page, total, base.fullReads, err)
	}
	if all, online, err := repo.CountDeviceStates(ctx, "t", false); err != nil || all != 3 || online != 3 {
		t.Fatalf("state counts all=%d online=%d err=%v", all, online, err)
	}
	if devices, err := repo.ListManagedDevices(ctx, "t"); err != nil || len(devices) != 3 || base.fullReads != 0 {
		t.Fatalf("devices=%v reads=%d err=%v", devices, base.fullReads, err)
	}
}

// Overview counts for a limited user cover only granted devices and their
// alarms, and are computed by the store.
func TestScopedOverviewCountsStayInStorage(t *testing.T) {
	base := &noFullRegistryRepo{Repository: memory.NewRepository()}
	ctx := context.WithValue(context.Background(), deviceScopeKey{}, deviceScope{Tenant: "t", IDs: map[string]bool{"a": true}})
	for _, id := range []string{"a", "b"} {
		_ = base.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ID: id, Status: "ENABLED", AccessKey: "ak-" + id})
		_ = base.UpsertDeviceState(ctx, model.DeviceState{TenantID: "t", DeviceID: id, BusinessStatus: "ONLINE", LastSeenAt: 10})
		_, _, _ = base.UpsertAlarm(ctx, model.Alarm{TenantID: "t", ID: "alarm-" + id, DeviceID: id, Status: "ACTIVE", AlarmLevel: "HIGH", LastTriggeredAt: 10})
	}
	repo := ScopedRepository(base)
	devices, err := repo.DeviceOverviewCounts(ctx, "t", false, nil)
	if err != nil || devices.Total != 1 || devices.Reported != 1 || devices.DiscoveredUnregistered != 0 || base.fullReads != 0 {
		t.Fatalf("devices=%+v fullRegistryReads=%d err=%v", devices, base.fullReads, err)
	}
	alarms, err := repo.AlarmOverviewCounts(ctx, ports.AlarmFilter{TenantID: "t"}, 0)
	if err != nil || alarms.Total != 1 || alarms.HighRiskActive != 1 {
		t.Fatalf("alarms=%+v err=%v", alarms, err)
	}
	if outcomes, err := repo.AIAnalysisOutcomes(ctx, ports.AlarmFilter{TenantID: "t"}, ""); err != nil || len(outcomes) != 0 {
		t.Fatalf("unverified alarms counted: %+v %v", outcomes, err)
	}
	for _, id := range []string{"a", "b"} {
		alarm, _ := base.GetAlarm(ctx, "t", "alarm-"+id)
		alarm.Disposition = &model.AlarmDisposition{Result: model.DispositionFalseAlarm, AIRiskLevel: "LOW"}
		_ = base.UpdateAlarm(ctx, alarm)
	}
	if outcomes, err := repo.AIAnalysisOutcomes(ctx, ports.AlarmFilter{TenantID: "t"}, ""); err != nil || len(outcomes) != 1 || outcomes[0].Count != 1 {
		t.Fatalf("scoped outcomes %+v %v", outcomes, err)
	}
	if all, _ := repo.DeviceOverviewCounts(context.Background(), "t", false, nil); all.Total != 2 {
		t.Fatalf("unscoped total=%d", all.Total)
	}
}

type changingKnowledgeBase struct {
	ports.KnowledgeBase
	change func()
}

func (k changingKnowledgeBase) IndexKnowledge(context.Context, ports.KnowledgeIndexInput) error {
	return nil
}
func (k changingKnowledgeBase) SearchKnowledge(context.Context, ports.KnowledgeSearchRequest) ([]ports.KnowledgeHit, error) {
	k.change()
	return []ports.KnowledgeHit{{DocumentID: "private-doc", ChunkID: "private-chunk", Content: "仅在原授权范围可见的证据", Score: 1}}, nil
}

func TestAIRejectsPermissionChangesDuringKnowledgePrefetch(t *testing.T) {
	for _, business := range []bool{false, true} {
		name := "chat"
		if business {
			name = "business"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			repo := memory.NewRepository()
			user := model.PlatformUser{Username: "expert", Enabled: true, SessionVersion: 1, DeviceScope: "all", Permissions: []string{"menu:devices", "menu:ai", "menu:knowledge", "POST /api/v1/ai/chat", "POST /api/v1/ai/reports"}}
			state := model.AccessState{Users: []model.PlatformUser{user}}
			if saved, err := repo.SaveAccessState(ctx, "tenant-a", state); err != nil || !saved {
				t.Fatalf("save access: %v", err)
			}
			permissions := effectivePermissions(state, user)
			requestCtx := context.WithValue(ctx, permissionsKey{}, permissions)
			requestCtx = context.WithValue(requestCtx, deviceScopeKey{}, (&Server{}).scopeFor(user, permissions, "tenant-a"))
			c := auth.Claims{TenantID: "tenant-a", Username: "expert", TokenUse: "user", SessionVersion: 1}
			workflows := &aitest.Workflows{}
			engine := &core.Engine{Repo: repo, Clock: ports.RealClock{}, AIWorkflows: workflows, HarnessTokens: aitest.Tokens()}
			api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
			engine.KB = changingKnowledgeBase{change: func() {
				current, err := repo.LoadAccessState(ctx, "tenant-a")
				if err != nil {
					t.Fatal(err)
				}
				current.Users[0].Permissions = []string{"menu:devices", "menu:ai", "POST /api/v1/ai/chat", "POST /api/v1/ai/reports"}
				if saved, err := repo.SaveAccessState(ctx, "tenant-a", current); err != nil || !saved {
					t.Fatalf("revoke knowledge: %v", err)
				}
			}}
			var err error
			if business {
				_, err = api.ai.GenerateReport(aiRunContext(requestCtx, c), "tenant-a", "今日", 1, 2)
			} else {
				_, err = api.runAIWorkflow(requestCtx, c, "查询私有知识", "test-agent", "", "", 2048, nil)
			}
			if err == nil || len(workflows.Requests()) != 0 {
				t.Fatalf("stale evidence sent to model: err=%v requests=%d", err, len(workflows.Requests()))
			}
			// A queued business run must also be rejected before any prefetch.
			if _, err = api.authorizeAIRun(aiRunContext(requestCtx, c), "tenant-a", aiworkflow.WorkflowOpsReport); err == nil {
				t.Fatal("queued stale permission snapshot accepted")
			}
		})
	}
}

func TestProtocolCodeUploadIsPlatformOnly(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword = "root", "protocol-root-test"
	cfg.AdminTenants = []string{"tenant_ops", "tenant_biz"}
	cfg.Ops.Tenants = []string{"tenant_ops"}
	cfg.JWTSecret = "protocol-test-secret-at-least-32-bytes"
	cfg.DevMode = true
	cfg.DataDir = t.TempDir()
	api := New(cfg, &core.Engine{Repo: repo, Clock: ports.RealClock{}}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()
	req := func(method, path, token string, body any, status int) map[string]any {
		t.Helper()
		return requestJSON(t, srv.Client(), method, srv.URL+path, token, body, status)
	}
	upload := func(token string) int {
		t.Helper()
		r, _ := http.NewRequest("POST", srv.URL+"/api/v2/protocols/vendor/source-releases", strings.NewReader(""))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "multipart/form-data; boundary=x")
		resp, err := srv.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	const action = "POST /api/v2/protocols/:id/source-releases"
	login := func(tenant, user, password string) string {
		return req("POST", "/api/v1/auth/login", "", map[string]any{"username": user, "password": password, "tenantId": tenant}, 200)["accessToken"].(string)
	}
	bizRoot, opsRoot := login("tenant_biz", "root", cfg.AdminPassword), login("tenant_ops", "root", cfg.AdminPassword)
	for _, item := range req("GET", "/api/v1/access/permissions", bizRoot, nil, 200)["items"].([]any) {
		if id := item.(map[string]any)["id"]; id == action || id == "POST /api/v2/protocols/:id/package-releases" {
			t.Fatalf("business tenant catalog offers %v", id)
		}
	}
	user := map[string]any{"username": "developer", "password": "protocol-user-test", "enabled": true, "permissions": []string{"menu:protocols", action}, "deviceScope": "all"}
	req("POST", "/api/v1/access/users", bizRoot, user, 422)
	req("POST", "/api/v1/access/users", opsRoot, user, 200)
	if status := upload(login("tenant_ops", "developer", "protocol-user-test")); status == 403 {
		t.Fatal("ops tenant developer must pass the platform boundary")
	}
	// A grant stored before the boundary existed is not effective.
	state, err := repo.LoadAccessState(ctx, "tenant_biz")
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("protocol-user-test"), bcrypt.MinCost)
	state.Users = append(state.Users, model.PlatformUser{Username: "legacy", PasswordHash: string(hash), Enabled: true, Permissions: []string{"menu:protocols", action}, DeviceScope: "all"})
	if ok, err := repo.SaveAccessState(ctx, "tenant_biz", state); err != nil || !ok {
		t.Fatal("save legacy grant", err)
	}
	if status := upload(login("tenant_biz", "legacy", "protocol-user-test")); status != 403 {
		t.Fatalf("legacy business grant uploaded: %d", status)
	}
	operator, _ := api.auth.Issue("operator", "tenant_biz", "operator", nil, time.Hour)
	if status := upload(operator); status != 403 {
		t.Fatalf("built-in operator token uploaded: %d", status)
	}
	if status := upload(bizRoot); status == 403 {
		t.Fatal("built-in administrator is the platform operator")
	}
}

// Model, embedding and agent configuration serve every tenant, so business
// tenants cannot grant or use the actions that change it.
func TestAIConfigurationIsPlatformOnly(t *testing.T) {
	repo := memory.NewRepository()
	ctx := context.Background()
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword = "root", "ai-config-root-test"
	cfg.AdminTenants = []string{"tenant_ops", "tenant_biz"}
	cfg.Ops.Tenants = []string{"tenant_ops"}
	cfg.JWTSecret = "ai-config-test-secret-at-least-32-bytes"
	cfg.DevMode = true
	api := New(cfg, &core.Engine{Repo: repo, Clock: ports.RealClock{}}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()
	req := func(method, path, token string, body any, status int) map[string]any {
		t.Helper()
		return requestJSON(t, srv.Client(), method, srv.URL+path, token, body, status)
	}
	login := func(tenant, user, password string) string {
		return req("POST", "/api/v1/auth/login", "", map[string]any{"username": user, "password": password, "tenantId": tenant}, 200)["accessToken"].(string)
	}
	bizRoot := login("tenant_biz", "root", cfg.AdminPassword)
	for _, item := range req("GET", "/api/v1/access/permissions", bizRoot, nil, 200)["items"].([]any) {
		if id, _ := item.(map[string]any)["id"].(string); isAIPlatformPermission(id) {
			t.Fatalf("business tenant catalog offers %s", id)
		}
	}
	// A grant stored before the boundary existed is neither effective nor usable.
	state, err := repo.LoadAccessState(ctx, "tenant_biz")
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte("ai-config-user-test"), bcrypt.MinCost)
	grants := []string{"menu:aiProviders", "menu:ai", "PUT /api/v1/ai/providers/config", "PUT /api/v1/ai/embedding-config", "GET /api/v1/ai/embedding-config", "POST /api/v1/ai/workflows", "DELETE /api/v1/ai/workflows/:id"}
	state.Users = append(state.Users, model.PlatformUser{Username: "legacy", PasswordHash: string(hash), Enabled: true, Permissions: grants, DeviceScope: "all"})
	if ok, err := repo.SaveAccessState(ctx, "tenant_biz", state); err != nil || !ok {
		t.Fatal("save legacy grant", err)
	}
	legacy := login("tenant_biz", "legacy", "ai-config-user-test")
	req("PUT", "/api/v1/ai/providers/config", legacy, map[string]any{"provider": "deepseek", "baseUrl": "http://attacker.example", "model": "m"}, 403)
	req("PUT", "/api/v1/ai/embedding-config", legacy, map[string]any{}, 403)
	req("GET", "/api/v1/ai/embedding-config", legacy, nil, 403)
	req("POST", "/api/v1/ai/workflows", legacy, map[string]any{"id": "x"}, 403)
	req("DELETE", "/api/v1/ai/workflows/x", legacy, nil, 403)
}

func TestManagedUserMustChangePasswordBeforeAccess(t *testing.T) {
	repo := memory.NewRepository()
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword = "root", "password-root-test"
	cfg.AdminTenants = []string{"t"}
	cfg.JWTSecret = "password-test-secret-at-least-32-bytes"
	cfg.DevMode = true
	api := New(cfg, &core.Engine{Repo: repo, Clock: ports.RealClock{}}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()
	req := func(method, path, token string, body any, status int) map[string]any {
		t.Helper()
		return requestJSON(t, srv.Client(), method, srv.URL+path, token, body, status)
	}
	login := func(user, password string, status int) map[string]any {
		t.Helper()
		return req("POST", "/api/v1/auth/login", "", map[string]any{"username": user, "password": password, "tenantId": "t"}, status)
	}
	root := login("root", cfg.AdminPassword, 200)["accessToken"].(string)
	req("POST", "/api/v1/access/users", root, map[string]any{"username": "duty", "password": "initial-password", "enabled": true, "mustChangePassword": true, "permissions": []string{"menu:dashboard"}, "deviceScope": "none"}, 200)
	first := login("duty", "initial-password", 200)
	if first["passwordChangeRequired"] != true || first["accessToken"] != nil {
		t.Fatalf("login must require a password change first: %v", first)
	}
	change := first["changeToken"].(string)
	req("GET", "/api/v1/auth/me", change, nil, 403)
	req("POST", "/api/v1/auth/password", change, map[string]any{"currentPassword": "wrong-password", "newPassword": "new-password-1"}, 422)
	req("POST", "/api/v1/auth/password", change, map[string]any{"currentPassword": "initial-password", "newPassword": "initial-password"}, 422)
	session := req("POST", "/api/v1/auth/password", change, map[string]any{"currentPassword": "initial-password", "newPassword": "new-password-1"}, 200)
	token := session["accessToken"].(string)
	req("GET", "/api/v1/auth/me", token, nil, 200)
	// The change token is single-use: the session version moved on.
	req("POST", "/api/v1/auth/password", change, map[string]any{"currentPassword": "new-password-1", "newPassword": "new-password-2"}, 401)
	login("duty", "initial-password", 401)
	if login("duty", "new-password-1", 200)["accessToken"] == nil {
		t.Fatal("normal login after the change")
	}
	// A logged-in user changes the password; other sessions end.
	again := req("POST", "/api/v1/auth/password", token, map[string]any{"currentPassword": "new-password-1", "newPassword": "new-password-2"}, 200)["accessToken"].(string)
	req("GET", "/api/v1/auth/me", token, nil, 401)
	req("GET", "/api/v1/auth/me", again, nil, 200)
	// A reset can require another change.
	req("POST", "/api/v1/access/users/duty/password", root, map[string]any{"password": "reset-password-1", "mustChangePassword": true}, 200)
	if login("duty", "reset-password-1", 200)["passwordChangeRequired"] != true {
		t.Fatal("reset must require a change")
	}
	req("POST", "/api/v1/auth/password", root, map[string]any{"currentPassword": cfg.AdminPassword, "newPassword": "something-else-1"}, 422)
}

func TestCachedAuthorizationFollowsAccessChanges(t *testing.T) {
	repo := memory.NewRepository()
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword = "root", "cache-root-test"
	cfg.AdminTenants = []string{"t"}
	cfg.JWTSecret = "cache-test-secret-at-least-32-bytes!"
	cfg.DevMode = true
	api := New(cfg, &core.Engine{Repo: repo, Clock: ports.RealClock{}}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()
	req := func(method, path, token string, body any, status int) map[string]any {
		t.Helper()
		return requestJSON(t, srv.Client(), method, srv.URL+path, token, body, status)
	}
	root := req("POST", "/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "t"}, 200)["accessToken"].(string)
	user := map[string]any{"username": "viewer", "password": "viewer-password-1", "enabled": true, "permissions": []string{"menu:dashboard", "menu:rules"}, "deviceScope": "all"}
	req("POST", "/api/v1/access/users", root, user, 200)
	token := req("POST", "/api/v1/auth/login", "", map[string]any{"username": "viewer", "password": "viewer-password-1", "tenantId": "t"}, 200)["accessToken"].(string)
	for i := 0; i < 3; i++ { // served from the cache
		req("GET", "/api/v1/auth/me", token, nil, 200)
	}
	// Narrowing permissions applies to the next request.
	user["permissions"] = []string{"menu:dashboard"}
	req("PUT", "/api/v1/access/users/viewer", root, user, 200)
	if perms := req("GET", "/api/v1/auth/me", token, nil, 401); perms == nil {
		t.Fatal("session survived a permission change")
	}
	token = req("POST", "/api/v1/auth/login", "", map[string]any{"username": "viewer", "password": "viewer-password-1", "tenantId": "t"}, 200)["accessToken"].(string)
	me := req("GET", "/api/v1/auth/me", token, nil, 200)
	if slices.Contains(toStrings(me["permissions"]), "menu:rules") {
		t.Fatalf("stale cached permissions: %v", me["permissions"])
	}
}

func toStrings(v any) []string {
	out := []string{}
	for _, item := range v.([]any) {
		out = append(out, item.(string))
	}
	return out
}

// Every repository read that returns device data must be narrowed by
// deviceScopeRepository, or be listed here with the reason it cannot leak
// devices outside the caller's scope. A new method fails until it is decided.
func TestRepositoryDeviceReadsFollowDeviceScope(t *testing.T) {
	reviewed := map[string]string{
		"ApplyComponentAlarm":           "后台写入：组件告警",
		"ChangeDeviceCredential":        "写入：调用前已用受范围约束的 GetManagedDevice 校验设备",
		"CreateDeviceCommand":           "写入：调用前已用受范围约束的 GetManagedDevice 校验设备",
		"GetDeviceCommand":              "后台命令应答关联，无接口直接读取",
		"GetDeviceAccessProfile":        "租户接入配置：平台接入点菜单仅全量范围可见",
		"ListDeviceAccessProfiles":      "租户接入配置：平台接入点菜单仅全量范围可见",
		"GetReplay":                     "受限范围用户直接返回不存在",
		"GetVideoCameraMapping":         "摄像头菜单仅全量范围可见；直播入口另按关联设备校验",
		"ListVideoCameraMappingsPage":   "摄像头菜单仅全量范围可见",
		"HealthInspectionPage":          "智能巡检菜单仅全量范围可见",
		"LatestHealthInspectionJob":     "智能巡检菜单仅全量范围可见",
		"LatestHealthInspectionSummary": "智能巡检菜单仅全量范围可见",
		"ListCredentialRevocations":     "单设备：调用前已用受范围约束的 GetManagedDevice 校验",
		"ListDeviceCommands":            "单设备：调用前已用受范围约束的 GetManagedDevice 校验",
		"ListDeviceMessages":            "单设备：调用前已校验；消息主题快照按主题绑定用户的范围查询",
		"ListDeviceStateEvents":         "单设备：调用前已用受范围约束的 GetManagedDevice 校验",
		"ListOfflineDue":                "后台离线判定",
		"ListPendingRawIndexes":         "后台原始报文补处理",
		"LoadDeviceStateWithAlarms":     "后台接收链路",
		"ReserveRawMessage":             "后台接收链路",
		"UpsertAlarm":                   "后台告警写入",
		"LoadAccessState":               "租户权限配置，接口另有用户与权限菜单校验",
		"LoadMessageTopicConfig":        "消息主题服务按主题绑定用户的范围过滤",
		"LoadSiteState":                 "单位建筑接口逐个点位按设备范围过滤",
	}
	sources, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	wrapped := map[string]bool{}
	for _, source := range sources {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), source, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv != nil {
				if star, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok && fmt.Sprint(star.X) == "deviceScopeRepository" {
					wrapped[fn.Name.Name] = true
				}
			}
		}
	}
	repo := reflect.TypeFor[ports.Repository]()
	for i := range repo.NumMethod() {
		method := repo.Method(i)
		carries := false
		for j := range method.Type.NumOut() {
			carries = carries || carriesDeviceID(method.Type.Out(j), map[reflect.Type]bool{})
		}
		switch {
		case carries && !wrapped[method.Name] && reviewed[method.Name] == "":
			t.Errorf("%s returns device data: narrow it in deviceScopeRepository or record why it cannot leak", method.Name)
		case reviewed[method.Name] != "" && (!carries || wrapped[method.Name]):
			t.Errorf("%s no longer needs a reviewed exception", method.Name)
		}
	}
}

func carriesDeviceID(t reflect.Type, seen map[reflect.Type]bool) bool {
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		return carriesDeviceID(t.Elem(), seen)
	case reflect.Struct:
		if seen[t] {
			return false
		}
		seen[t] = true
		for i := range t.NumField() {
			if field := t.Field(i); field.Name == "DeviceID" || field.Name == "DeviceIDs" || carriesDeviceID(field.Type, seen) {
				return true
			}
		}
	}
	return false
}
