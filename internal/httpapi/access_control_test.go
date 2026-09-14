package httpapi

import (
	"context"
	"io"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"log/slog"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
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
	user := map[string]any{"username": "demo_user", "displayName": "演示用户", "password": "initial-password-test", "enabled": true, "roleIds": []string{"device_reader"}, "permissions": []string{}}
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
}

func TestUserPermissionsCombineRolesAndIndividualGrants(t *testing.T) {
	state := model.AccessState{Roles: []model.PlatformRole{{ID: "reader", Permissions: []string{"menu:devices"}}}}
	viewer := model.PlatformUser{RoleIDs: []string{"reader"}}
	editor := model.PlatformUser{RoleIDs: []string{"reader"}, Permissions: []string{"POST /api/v1/device-registry"}}
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
