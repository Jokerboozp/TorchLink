package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
)

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
