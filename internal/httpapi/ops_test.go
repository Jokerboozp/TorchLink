package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/adapters/observability"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/opscenter"
)

type auditingRepo struct {
	*memory.Repository
	mu     sync.Mutex
	audits []model.AuditLog
}

func (r *auditingRepo) SaveAudit(ctx context.Context, v model.AuditLog) error {
	r.mu.Lock()
	r.audits = append(r.audits, v)
	r.mu.Unlock()
	return r.Repository.SaveAudit(ctx, v)
}

func (r *auditingRepo) actions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []string{}
	for _, a := range r.audits {
		out = append(out, a.Action+"@"+a.TenantID)
	}
	return out
}

// fakeObservability answers the few Prometheus and Grafana endpoints the
// permission tests touch.
func fakeObservability() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/targets":
			_, _ = w.Write([]byte(`{"status":"success","data":{"activeTargets":[{"labels":{"job":"iot-platform","instance":"api:8080"},"health":"up","scrapePool":"iot-platform"}]}}`))
		case "/api/v1/query":
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,"0"]}]}}`))
		case "/api/v1/query_range":
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
		case "/api/dashboards/uid/d1":
			if r.Method == http.MethodDelete {
				_, _ = w.Write([]byte(`{"title":"x"}`))
				return
			}
			_, _ = w.Write([]byte(`{"dashboard":{"uid":"d1","title":"系统"},"meta":{}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
		}
	}))
}

func TestOpsCenterPermissionBoundaryAndAudit(t *testing.T) {
	upstream := fakeObservability()
	defer upstream.Close()
	repo := &auditingRepo{Repository: memory.NewRepository()}
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword = "root", "root-password-test"
	cfg.AdminTenants = []string{"tenant_ops", "tenant_biz"}
	cfg.JWTSecret = "test-only-secret-for-ops-at-least-32"
	cfg.DevMode = true
	cfg.Ops.Tenants = []string{"tenant_ops"}
	api := New(cfg, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetOpsCenter(&opscenter.Service{
		Metrics:    observability.NewPrometheus(upstream.URL, time.Second),
		Dashboards: observability.NewGrafana(upstream.URL, "t", "", "", time.Second),
		Prefs:      repo.Repository,
		Limits:     opscenter.Limits{QueryTimeout: time.Second, MaxSeries: 10, MaxLogLines: 10, MaxExportLines: 10, MaxMetricRange: 24 * time.Hour, MaxLogRange: 24 * time.Hour},
	})
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	req := func(method, path, token string, body any, status int) map[string]any {
		return requestJSON(t, server.Client(), method, server.URL+path, token, body, status)
	}
	login := func(user, password, tenant string) string {
		return req("POST", "/api/v1/auth/login", "", map[string]any{"username": user, "password": password, "tenantId": tenant}, 200)["accessToken"].(string)
	}
	rootOps := login("root", cfg.AdminPassword, "tenant_ops")
	rootBiz := login("root", cfg.AdminPassword, "tenant_biz")

	// The built-in administrator is the platform operator in any tenant.
	req("GET", "/api/v1/ops/status", rootBiz, nil, 200)

	// Business tenants are never offered ops permissions.
	for _, item := range req("GET", "/api/v1/access/permissions", rootBiz, nil, 200)["items"].([]any) {
		if isOpsPermission(item.(map[string]any)["id"].(string)) {
			t.Fatalf("business tenant catalog contains %v", item)
		}
	}
	req("POST", "/api/v1/access/roles", rootBiz, map[string]any{"id": "sneaky", "name": "x", "permissions": []string{"menu:opsMetrics"}}, 422)
	found := false
	for _, item := range req("GET", "/api/v1/access/permissions", rootOps, nil, 200)["items"].([]any) {
		found = found || item.(map[string]any)["id"] == "POST /api/v1/ops/metrics/query"
	}
	if !found {
		t.Fatal("ops tenant catalog must offer the PromQL query permission")
	}

	// A viewer in the ops tenant can read but not run free-form queries.
	req("POST", "/api/v1/access/roles", rootOps, map[string]any{"id": "ops_viewer", "name": "运维查看", "permissions": []string{"menu:opsMetrics", "menu:opsLogs"}}, 200)
	req("POST", "/api/v1/access/users", rootOps, map[string]any{"username": "ops_user", "displayName": "运维", "password": "ops-password-test", "enabled": true, "roleIds": []string{"ops_viewer"}, "permissions": []string{}, "deviceScope": "none"}, 200)
	viewer := login("ops_user", "ops-password-test", "tenant_ops")
	req("GET", "/api/v1/ops/metrics/targets", viewer, nil, 200)
	req("GET", "/api/v1/ops/preferences/history", viewer, nil, 200)
	req("POST", "/api/v1/ops/metrics/query", viewer, map[string]any{"query": "up", "instant": true}, 403)
	req("GET", "/api/v1/ops/dashboards", viewer, nil, 403)
	req("GET", "/api/v1/ops/logs/tail?query=%7Ba%3D%22b%22%7D", viewer, nil, 403)
	// Logs are not wired in this server: the component is reported as unconfigured.
	if body := req("GET", "/api/v1/ops/logs/labels", viewer, nil, 503); body["code"] != "OPS_NOT_CONFIGURED" {
		t.Fatalf("unconfigured code = %v", body["code"])
	}

	req("PUT", "/api/v1/access/roles/ops_viewer", rootOps, map[string]any{"id": "ops_viewer", "name": "运维查看", "permissions": []string{"menu:opsMetrics", "menu:opsLogs", "POST /api/v1/ops/metrics/query"}}, 200)
	req("POST", "/api/v1/ops/metrics/query", viewer, map[string]any{"query": "up", "instant": true}, 200)
	if items := req("GET", "/api/v1/ops/preferences/history?language=promql", viewer, nil, 200)["items"].([]any); len(items) != 1 {
		t.Fatalf("viewer history = %v", items)
	}
	if items := req("GET", "/api/v1/ops/preferences/history", rootOps, nil, 200)["items"].([]any); len(items) != 0 {
		t.Fatalf("history leaked across accounts: %v", items)
	}

	// Ops grants stored for a business-tenant user have no effect.
	state, _ := repo.LoadAccessState(context.Background(), "tenant_biz")
	hash, _ := hashPassword("biz-password-test")
	state.Users = append(state.Users, model.PlatformUser{Username: "biz_user", DisplayName: "业务", PasswordHash: hash, Enabled: true, Permissions: []string{"menu:opsMetrics", "POST /api/v1/ops/metrics/query"}, DeviceScope: "none"})
	if ok, err := repo.SaveAccessState(context.Background(), "tenant_biz", state); !ok || err != nil {
		t.Fatal("seed business user", err)
	}
	biz := login("biz_user", "biz-password-test", "tenant_biz")
	req("GET", "/api/v1/ops/metrics/targets", biz, nil, 403)
	req("POST", "/api/v1/ops/metrics/query", biz, map[string]any{"query": "up", "instant": true}, 403)
	for _, p := range req("GET", "/api/v1/auth/me", biz, nil, 200)["permissions"].([]any) {
		if isOpsPermission(p.(string)) {
			t.Fatalf("business user effective permission %v", p)
		}
	}

	// Management actions are audited under the actor's tenant.
	req("DELETE", "/api/v1/ops/dashboards/d1", rootOps, nil, 200)
	if actions := strings.Join(repo.actions(), ","); !strings.Contains(actions, "ops.dashboard.delete@tenant_ops") {
		t.Fatalf("audits = %s", actions)
	}
	if body := req("GET", "/api/v1/ops/dashboards/missing", rootOps, nil, 404); body["code"] != "OPS_NOT_FOUND" {
		t.Fatalf("not found code = %v", body["code"])
	}
}

func TestOpsCenterUnavailableWithoutService(t *testing.T) {
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword, cfg.JWTSecret, cfg.DevMode = "root", "root-password-test", "test-only-secret-for-ops-at-least-32", true
	api := New(cfg, &core.Engine{Repo: memory.NewRepository()}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token := requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/auth/login", "", map[string]any{"username": "root", "password": "root-password-test", "tenantId": "tenant_001"}, 200)["accessToken"].(string)
	if body := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/ops/overview", token, nil, 503); body["code"] != "OPS_NOT_CONFIGURED" {
		t.Fatalf("code = %v", body["code"])
	}
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/ops/overview", "", nil, 401)
}
