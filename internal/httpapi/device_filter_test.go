package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"slices"
	"testing"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
)

func TestDeviceRegistryFiltersBeforePagination(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword = "root", "root-password-test"
	cfg.AdminTenants = []string{"tenant_a"}
	cfg.JWTSecret = "test-only-secret-for-device-filters"
	cfg.DevMode = true
	api := New(cfg, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	req := func(method, path, token string, body any, status int) map[string]any {
		return requestJSON(t, server.Client(), method, server.URL+path, token, body, status)
	}
	for _, p := range []model.Product{{ID: "gw-product", Category: "gateway"}, {ID: "smoke-product", Category: "smoke"}, {ID: "bare"}} {
		p.TenantID, p.Name, p.Status = "tenant_a", p.ID, "ENABLED"
		if err := repo.SaveProduct(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	devices := []model.ManagedDevice{
		{ID: "gw-1", Name: "一号网关", ProductID: "gw-product", Status: "ENABLED", UpdatedAt: 4},
		{ID: "child-1", Name: "烟感", ProductID: "smoke-product", DeviceRole: "CHILD", GatewayID: "gw-1", Status: "ENABLED", UpdatedAt: 3},
		{ID: "direct-1", Name: "独立烟感", ProductID: "smoke-product", DeviceRole: "DIRECT", Status: "DISABLED", UpdatedAt: 2},
		{ID: "other-1", Name: "其他", ProductID: "bare", Status: "ENABLED", UpdatedAt: 1},
		{ID: "foreign", Name: "其他租户", ProductID: "bare", Status: "ENABLED", TenantID: "tenant_b", UpdatedAt: 5},
	}
	for _, d := range devices {
		if d.TenantID == "" {
			d.TenantID = "tenant_a"
		}
		d.AccessKey = model.ProtocolDeviceAccessKey(d.TenantID, d.ID)
		if err := repo.SaveManagedDevice(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	for id, status := range map[string]string{"gw-1": "ONLINE", "direct-1": "ALARM"} {
		if err := repo.UpsertDeviceState(ctx, model.DeviceState{TenantID: "tenant_a", DeviceID: id, ProductID: "p", BusinessStatus: status}); err != nil {
			t.Fatal(err)
		}
	}
	list := func(token, query string) ([]string, float64) {
		t.Helper()
		body := req("GET", "/api/v1/device-registry?"+query, token, nil, 200)
		ids := []string{}
		for _, row := range body["items"].([]any) {
			ids = append(ids, row.(map[string]any)["device"].(map[string]any)["id"].(string))
		}
		return ids, body["total"].(float64)
	}
	root := req("POST", "/api/v1/auth/login", "", map[string]any{"username": "root", "password": cfg.AdminPassword, "tenantId": "tenant_a"}, 200)["accessToken"].(string)
	cases := map[string][]string{
		"":                              {"gw-1", "child-1", "direct-1", "other-1"},
		"role=GATEWAY":                  {"gw-1"},
		"role=child":                    {"child-1"},
		"role=DIRECT":                   {"direct-1", "other-1"},
		"category=other":                {"other-1"},
		"category=smoke&role=DIRECT":    {"direct-1"},
		"productId=smoke-product":       {"child-1", "direct-1"},
		"category=missing":              {},
		"q=%E7%BD%91%E5%85%B3":          {"gw-1"},
		"q=CHILD":                       {"child-1"},
		"status=DISABLED":               {"direct-1"},
		"runtime=NEVER_SEEN":            {"child-1", "other-1"},
		"runtime=ALARM&status=DISABLED": {"direct-1"},
	}
	for query, want := range cases {
		if got, total := list(root, query); !slices.Equal(got, want) || int(total) != len(want) {
			t.Fatalf("%q: got %v (%v), want %v", query, got, total, want)
		}
	}
	child := req("GET", "/api/v1/device-registry?role=CHILD", root, nil, 200)["items"].([]any)[0].(map[string]any)
	if parent, _ := child["parent"].(map[string]any); parent["name"] != "一号网关" {
		t.Fatalf("child row parent: %+v", child)
	}
	if got, total := list(root, "role=DIRECT&pageSize=1&page=2"); !slices.Equal(got, []string{"other-1"}) || total != 2 {
		t.Fatalf("filtered page: %v %v", got, total)
	}
	for _, query := range []string{"role=OWNER", "status=ON", "runtime=BUSY"} {
		req("GET", "/api/v1/device-registry?"+query, root, nil, 422)
	}

	// Users limited to selected devices get the same filters within their scope.
	req("POST", "/api/v1/access/roles", root, map[string]any{"id": "viewer", "name": "查看", "permissions": []string{"menu:devices"}}, 200)
	req("POST", "/api/v1/access/users", root, map[string]any{"username": "viewer", "displayName": "查看", "password": "viewer-password", "enabled": true, "roleIds": []string{"viewer"}, "permissions": []string{}, "deviceScope": "selected", "deviceIds": []string{"gw-1", "direct-1"}}, 200)
	limited := req("POST", "/api/v1/auth/login", "", map[string]any{"username": "viewer", "password": "viewer-password", "tenantId": "tenant_a"}, 200)["accessToken"].(string)
	if got, total := list(limited, ""); !slices.Equal(got, []string{"gw-1", "direct-1"}) || total != 2 {
		t.Fatalf("limited scope: %v %v", got, total)
	}
	if got, total := list(limited, "role=DIRECT"); !slices.Equal(got, []string{"direct-1"}) || total != 1 {
		t.Fatalf("limited filter: %v %v", got, total)
	}
	if got, _ := list(limited, "runtime=NEVER_SEEN"); len(got) != 0 {
		t.Fatalf("limited scope leaked devices: %v", got)
	}
}
