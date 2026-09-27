package httpapi

import (
	"context"
	"encoding/json"
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
	// The overview parts follow the overview menu.
	req("GET", "/api/v1/ops/overview/components/prometheus", viewer, nil, 403)
	req("GET", "/api/v1/ops/overview/kpis?group=host", viewer, nil, 403)
	if body := req("GET", "/api/v1/ops/overview/components/grafana", rootOps, nil, 200); body["id"] != "grafana" {
		t.Fatalf("component = %v", body)
	}
	req("GET", "/api/v1/ops/overview/components/unknown", rootOps, nil, 404)
	if body := req("GET", "/api/v1/ops/overview/kpis?group=host", rootOps, nil, 200); len(body["kpis"].([]any)) != 3 {
		t.Fatalf("host kpis = %v", body["kpis"])
	}
	req("GET", "/api/v1/ops/overview/kpis?group=nope", rootOps, nil, 422)
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
	// Without the capacity module the page learns it is off (and hides itself).
	if body := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/ops/capacity/status", token, nil, 200); body["enabled"] != false {
		t.Fatalf("capacity status = %v", body)
	}
}

func TestCapacityProxyFollowsOpsBoundaryAndAudits(t *testing.T) {
	var calls []string
	var startBody []byte
	var mu sync.Mutex
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer capacity-service-token-for-tests-000" {
			w.WriteHeader(401)
			return
		}
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		switch {
		case r.URL.Path == "/v1/environments":
			_, _ = w.Write([]byte(`{"items":[{"name":"lab","agents":1}]}`))
		case r.URL.Path == "/v1/plans/validate":
			_, _ = w.Write([]byte(`{"valid":true,"errors":[]}`))
		case r.URL.Path == "/v1/runs" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"items":[]}`))
		case r.URL.Path == "/v1/runs" && r.Method == http.MethodPost:
			mu.Lock()
			startBody, _ = io.ReadAll(r.Body)
			mu.Unlock()
			w.WriteHeader(202)
			_, _ = w.Write([]byte(`{"runId":"cap-20260928-120000-abcdef"}`))
		case r.URL.Path == "/v1/runs/cap-20260928-120000-abcdef/stop":
			w.WriteHeader(202)
			_, _ = w.Write([]byte(`{"runId":"cap-20260928-120000-abcdef","force":false}`))
		case r.URL.Path == "/v1/runs/cap-20260928-120000-abcdef/report":
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			w.Header().Set("Content-Disposition", `attachment; filename="r.md"`)
			_, _ = w.Write([]byte("# 容量测试报告"))
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":"run not found"}`))
		}
	}))
	defer upstream.Close()
	repo := &auditingRepo{Repository: memory.NewRepository()}
	cfg := config.Load()
	cfg.AdminUser, cfg.AdminPassword = "root", "root-password-test"
	cfg.AdminTenants = []string{"tenant_ops", "tenant_biz"}
	cfg.JWTSecret = "test-only-secret-for-ops-at-least-32"
	cfg.DevMode = true
	cfg.Ops.Tenants = []string{"tenant_ops"}
	cfg.Ops.CapacityURL, cfg.Ops.CapacityToken = upstream.URL, "capacity-service-token-for-tests-000"
	api := New(cfg, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	req := func(method, path, token string, body any, status int) map[string]any {
		return requestJSON(t, server.Client(), method, server.URL+path, token, body, status)
	}
	login := func(user, password, tenant string) string {
		return req("POST", "/api/v1/auth/login", "", map[string]any{"username": user, "password": password, "tenantId": tenant}, 200)["accessToken"].(string)
	}
	root := login("root", cfg.AdminPassword, "tenant_ops")
	if body := req("GET", "/api/v1/ops/capacity/status", root, nil, 200); body["enabled"] != true || body["reachable"] != true {
		t.Fatalf("module status %v", body)
	}
	plan := map[string]any{"environment": "lab", "plan": "schemaVersion: 1\nbudget: {maximumWallTime: 1h}\n", "tenant": "tenant_biz", "operatorToken": "forged"}
	req("GET", "/api/v1/ops/capacity/environments", root, nil, 200)
	req("POST", "/api/v1/ops/capacity/plans/validate", root, plan, 200)
	if body := req("POST", "/api/v1/ops/capacity/runs", root, plan, 202); body["runId"] != "cap-20260928-120000-abcdef" {
		t.Fatal(body)
	}
	// The platform decides tenant and operator identity; browser values are ignored.
	var forwarded struct {
		Tenant        string `json:"tenant"`
		OperatorToken string `json:"operatorToken"`
	}
	mu.Lock()
	_ = json.Unmarshal(startBody, &forwarded)
	mu.Unlock()
	delegated, err := api.auth.Parse(forwarded.OperatorToken)
	if forwarded.Tenant != "tenant_ops" || err != nil || delegated.Username != "root" || delegated.TenantID != "tenant_ops" || delegated.ExpiresAt.Time.After(time.Now().Add(91*time.Minute)) {
		t.Fatalf("forwarded identity %+v %v %+v", forwarded.Tenant, err, delegated)
	}
	req("POST", "/api/v1/ops/capacity/runs/cap-20260928-120000-abcdef/stop", root, map[string]any{}, 202)
	req("GET", "/api/v1/ops/capacity/runs/..%2Fetc", root, nil, 404)
	req("GET", "/api/v1/ops/capacity/runs/cap-20260928-120000-000000", root, nil, 404)
	resp, err := func() (*http.Response, error) {
		r, _ := http.NewRequest("GET", server.URL+"/api/v1/ops/capacity/runs/cap-20260928-120000-abcdef/report?format=markdown", nil)
		r.Header.Set("Authorization", "Bearer "+root)
		return server.Client().Do(r)
	}()
	if err != nil || resp.StatusCode != 200 || resp.Header.Get("Content-Disposition") == "" {
		t.Fatal("report download", err, resp.StatusCode)
	}
	resp.Body.Close()
	actions := strings.Join(repo.actions(), ",")
	for _, want := range []string{"capacity.run.start@tenant_ops", "capacity.run.stop@tenant_ops", "capacity.report.download@tenant_ops"} {
		if !strings.Contains(actions, want) {
			t.Fatalf("audits %s lack %s", actions, want)
		}
	}
	// A viewer with only the capacity menu can list but not start, stop or download.
	req("POST", "/api/v1/access/roles", root, map[string]any{"id": "cap_viewer", "name": "容量查看", "permissions": []string{"menu:opsCapacity"}}, 200)
	req("POST", "/api/v1/access/users", root, map[string]any{"username": "cap_user", "displayName": "容量", "password": "cap-password-test", "enabled": true, "roleIds": []string{"cap_viewer"}, "permissions": []string{}, "deviceScope": "none"}, 200)
	viewer := login("cap_user", "cap-password-test", "tenant_ops")
	req("GET", "/api/v1/ops/capacity/runs", viewer, nil, 200)
	req("POST", "/api/v1/ops/capacity/runs", viewer, plan, 403)
	req("GET", "/api/v1/ops/capacity/status", viewer, nil, 200)
	req("POST", "/api/v1/ops/capacity/runs/cap-20260928-120000-abcdef/stop", viewer, map[string]any{}, 403)
	req("GET", "/api/v1/ops/capacity/runs/cap-20260928-120000-abcdef/report", viewer, nil, 403)
	// Business tenants cannot be granted the capacity menu at all.
	rootBiz := login("root", cfg.AdminPassword, "tenant_biz")
	req("POST", "/api/v1/access/roles", rootBiz, map[string]any{"id": "cap", "name": "x", "permissions": []string{"menu:opsCapacity"}}, 422)
	for _, c := range calls {
		if strings.Contains(c, "..") {
			t.Fatal("traversal reached the controller", c)
		}
	}
}
