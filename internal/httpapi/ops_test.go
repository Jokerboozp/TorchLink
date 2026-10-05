package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/adapters/observability"
	"iot-platform/internal/capacity"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/onboarding"
	"iot-platform/internal/opscenter"
)

func TestCapacityCleanupThroughPlatformAndController(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	cfg := config.Load()
	cfg.JWTSecret = "capacity-cleanup-integration-key-32"
	cfg.AdminUser, cfg.AdminTenants = "root", []string{"t"}
	cfg.Ops.Tenants, cfg.Ops.CapacityToken = []string{"t"}, "integration-controller-secret"
	api := New(cfg, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	root := t.TempDir()
	id := "cap-20260930-142131-3911da"
	service := capacity.NewService(capacity.ServeOptions{ResultsDir: root, Token: cfg.Ops.CapacityToken, Self: &capacity.SelfEnvironment{API: server.URL, PostgresDSN: "unused", Metrics: []capacity.MetricsTarget{{Role: "combined", Instance: "a", URL: server.URL + "/metrics"}}}})
	controller := httptest.NewServer(service.Handler())
	defer controller.Close()
	api.cfg.Ops.CapacityURL = controller.URL
	token, err := api.auth.IssueWithVersion("root", "t", "admin", api.adminSessionVersion(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	p, err := capacity.ParsePlan([]byte("schemaVersion: 1\nname: cleanup\nfixtures: {tenant: t, product: cap-standard, deviceCount: 1, reuseDevices: true, autoProvision: true, alarmRuleId: cap-stress-alarm}\nload: {ingressShare: {http: 1}, initialMessagesPerSecond: 1}\n"))
	if err != nil {
		t.Fatal(err)
	}
	plan, _, err := p.Sanitized()
	if err != nil {
		t.Fatal(err)
	}
	writeFile := func(path string, b []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(filepath.Join(root, id, "plan.sanitized.yaml"), plan)
	writeFile(filepath.Join(root, id, "state.json"), []byte(`{"runId":"`+id+`","status":"FAILED"}`))
	writeFile(filepath.Join(root, id, "manifest.json"), []byte(`{"runId":"`+id+`","tenant":"t","product":"cap-standard","devices":["cap-000000"]}`))
	writeFile(filepath.Join(root, id, "cleanup-context.json"), []byte(`{"environment":"self","planFile":"test.yaml"}`))
	writeFile(filepath.Join(root, ".plans", "test.yaml"), plan)
	writeFile(filepath.Join(root, ".work", "agent-local", id, "work"), []byte("private agent cache"))
	cache := filepath.Join(root, ".work", "fixtures", "t-cap-standard-cap.json")
	writeFile(cache, []byte(`[{"id":"cap-000000","key":"k","secret":"test-only"},{"id":"other-cache","key":"other","secret":"keep"}]`))
	ledger, err := capacity.NewLedgerWriter(filepath.Join(root, id, "ledgers", "p1", "local.jsonl.gz"), capacity.LedgerHeader{RunID: id, Tenant: "t", Product: "cap-standard"})
	if err != nil {
		t.Fatal(err)
	}
	ledger.Write(capacity.LedgerEntry{Device: "cap-000000", RawID: "raw", Stream: "http", Result: "202", OK: true})
	if _, err = ledger.Close(); err != nil {
		t.Fatal(err)
	}
	_ = repo.SaveProduct(ctx, model.Product{TenantID: "t", ID: "cap-standard", Name: "容量测试标准设备 cap-standard", ProtocolPackageID: onboarding.StandardPackageID, Description: "capacity-test 自动创建"})
	_ = repo.SaveRule(ctx, model.AlarmRule{TenantID: "t", ID: "cap-stress-alarm", Name: "容量测试告警 cap-stress-alarm", ProductID: "cap-standard", AlarmType: "CAPACITY_TEST"})
	for _, d := range []model.ManagedDevice{{TenantID: "t", ProductID: "cap-standard", ID: "cap-000000", Name: "容量测试 cap-000000", RegistrationSource: "ONBOARDING", AccessKey: "fixture-key"}, {TenantID: "t", ProductID: "business", ID: "business", Name: "业务设备", AccessKey: "business-key"}} {
		if err = repo.SaveManagedDevice(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = repo.SaveRawIndex(ctx, model.RawArchiveIndex{TenantID: "t", ProductID: "cap-standard", DeviceID: "cap-000000", MessageID: "raw", ObjectBucket: "postgres", ParseAttemptedAt: 1})
	claim, err := repo.ClaimStandardMessage(ctx, model.StandardMessage{TenantID: "t", ProductID: "cap-standard", DeviceID: "cap-000000", MessageID: "std", RawMessageID: "raw"}, "test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = repo.MarkStandardMessageProcessed(ctx, "t", "std", claim.Token); err != nil {
		t.Fatal(err)
	}
	path := server.URL + "/api/v1/ops/capacity/runs/" + id
	preview := requestJSON(t, server.Client(), "GET", path+"/cleanup", token, nil, 200)
	if preview["devices"] != float64(1) || preview["runs"] != float64(1) {
		t.Fatal(preview)
	}
	if result := requestJSON(t, server.Client(), "DELETE", path, token, map[string]any{"tenant": "forged"}, 202); result["cleaning"] != true {
		t.Fatal(result)
	}
	// Cleanup continues on the controller; the run list reports its state.
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		list := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/ops/capacity/runs", token, nil, 200)
		if list["cleaningRunId"] == "" {
			if list["total"] != float64(0) {
				t.Fatal("cleanup failed", list)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cleanup did not finish", list)
		}
	}
	if _, err = repo.GetRawIndex(ctx, "t", "raw"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("raw message retained", err)
	}
	if _, err = repo.GetProduct(ctx, "t", "cap-standard"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("auto-provisioned product retained", err)
	}
	for _, path := range []string{filepath.Join(root, id), filepath.Join(root, ".work", "agent-local", id), filepath.Join(root, ".plans", "test.yaml")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("artifact retained", path, err)
		}
	}
	b, err := os.ReadFile(cache)
	if err != nil {
		t.Fatal(err)
	}
	var credentials []capacity.DeviceCredential
	if err = json.Unmarshal(b, &credentials); err != nil || len(credentials) != 1 || credentials[0].ID != "other-cache" {
		t.Fatal("wrong credential cleanup", err)
	}
	if _, err = repo.GetManagedDevice(ctx, "t", "cap-000000"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("fixture retained", err)
	}
	if _, err = repo.GetManagedDevice(ctx, "t", "business"); err != nil {
		t.Fatal("business device removed", err)
	}
	requestJSON(t, server.Client(), "GET", path+"/cleanup", token, nil, 404)
}

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
		case r.URL.Path == "/v1/runs/cap-20260928-120000-abcdef/cleanup":
			if r.URL.Query().Get("tenant") != "tenant_ops" {
				t.Error("preview did not bind the caller tenant")
			}
			_, _ = w.Write([]byte(`{"devices":0,"warnings":[]}`))
		case r.URL.Path == "/v1/runs/cap-20260928-120000-abcdef" && r.Method == "DELETE":
			var body struct {
				Tenant        string `json:"tenant"`
				OperatorToken string `json:"operatorToken"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Tenant != "tenant_ops" || body.OperatorToken == "forged" || body.OperatorToken == "" {
				t.Error("cleanup did not bind current operator")
			}
			w.WriteHeader(202)
			_, _ = w.Write([]byte(`{"runId":"cap-20260928-120000-abcdef","cleaning":true}`))
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
	plan := map[string]any{"environment": "lab", "plan": "schemaVersion: 1\nbudget: {maximumWallTime: 25h}\n", "tenant": "tenant_biz", "operatorToken": "forged"}
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
	if forwarded.Tenant != "tenant_ops" || err != nil || delegated.Username != "root" || delegated.TenantID != "tenant_ops" || delegated.ExpiresAt.Time.Before(time.Now().Add(25*time.Hour+29*time.Minute)) || delegated.ExpiresAt.Time.After(time.Now().Add(25*time.Hour+31*time.Minute)) {
		t.Fatalf("forwarded identity %+v %v %+v", forwarded.Tenant, err, delegated)
	}
	req("POST", "/api/v1/ops/capacity/runs/cap-20260928-120000-abcdef/stop", root, map[string]any{}, 202)
	req("GET", "/api/v1/ops/capacity/runs/cap-20260928-120000-abcdef/cleanup", root, nil, 200)
	req("DELETE", "/api/v1/ops/capacity/runs/cap-20260928-120000-abcdef", root, map[string]any{"tenant": "tenant_biz", "operatorToken": "forged"}, 202)
	req("GET", "/api/v1/ops/capacity/runs?page=2&pageSize=abc&tenant=x", root, nil, 200)
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
	for _, want := range []string{"capacity.run.start@tenant_ops", "capacity.run.stop@tenant_ops", "capacity.report.download@tenant_ops", "capacity.run.cleanup@tenant_ops"} {
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
	req("GET", "/api/v1/ops/capacity/runs/cap-20260928-120000-abcdef/cleanup", viewer, nil, 403)
	req("DELETE", "/api/v1/ops/capacity/runs/cap-20260928-120000-abcdef", viewer, map[string]any{}, 403)
	// Business tenants cannot be granted the capacity menu at all.
	rootBiz := login("root", cfg.AdminPassword, "tenant_biz")
	req("POST", "/api/v1/access/roles", rootBiz, map[string]any{"id": "cap", "name": "x", "permissions": []string{"menu:opsCapacity"}}, 422)
	for _, c := range calls {
		if strings.Contains(c, "..") {
			t.Fatal("traversal reached the controller", c)
		}
	}
	// Only numeric paging parameters are forwarded to the controller.
	if !slices.Contains(calls, "GET /v1/runs?page=2") {
		t.Fatalf("paging was not forwarded: %v", calls)
	}
}

func TestCapacityCleanupDataProtectsTenantsAndBusinessDevices(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	cfg := config.Load()
	cfg.JWTSecret = "capacity-cleanup-test-signing-key-32"
	cfg.AdminUser = "root"
	cfg.AdminTenants = []string{"t", "other"}
	cfg.Ops.Tenants = []string{"t"}
	cfg.Ops.CapacityToken = "controller-secret"
	cfg.MQTTBroker, cfg.KafkaBrokers = "", nil
	for _, tenant := range cfg.AdminTenants {
		p := model.Product{TenantID: tenant, ID: "owned", Name: model.CapacityFixtureProductName("owned"), Description: model.CapacityFixtureDescription + "，用于容量测试设备", ProtocolPackageID: "iot-standard@1.0.0", Status: "ENABLED"}
		_ = repo.SaveProduct(ctx, p)
		_ = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: tenant, ProductID: p.ID, ID: "fixture", Name: "容量测试 fixture", RegistrationSource: "ONBOARDING", Status: "ENABLED", AccessKey: tenant + "-fixture"})
		_ = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: tenant, ProductID: p.ID, ID: "renamed", Name: "三楼烟感", RegistrationSource: "ONBOARDING", Status: "ENABLED", AccessKey: tenant + "-renamed"})
		_ = repo.SaveProduct(ctx, model.Product{TenantID: tenant, ID: "cap-business", Name: "正常业务模板", ProtocolPackageID: p.ProtocolPackageID, Status: "ENABLED"})
		_ = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: tenant, ProductID: "cap-business", ID: "cap-real", Name: "容量测试 cap-real", RegistrationSource: "ONBOARDING", Status: "ENABLED", AccessKey: tenant + "-business"})
		_, _ = repo.SaveRawIndex(ctx, model.RawArchiveIndex{TenantID: tenant, ProductID: p.ID, DeviceID: "fixture", MessageID: "raw", ObjectBucket: "postgres", ParseAttemptedAt: 1})
	}
	api := New(cfg, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	token, _ := api.auth.IssueWithVersion("root", "t", "admin", api.adminSessionVersion(), time.Hour)
	call := func(method, path string, body any, secret string, status int) []byte {
		t.Helper()
		b, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(b))
		r.Header.Set("Authorization", "Bearer "+token)
		if secret != "" {
			r.Header.Set("X-Capacity-Service-Token", secret)
		}
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s HTTP %d want %d: %s", method, path, w.Code, status, w.Body.String())
		}
		return w.Body.Bytes()
	}
	call("GET", capacityFixturePath, nil, "", 403)
	call("GET", capacityFixturePath, nil, "forged", 403)
	var page struct {
		Products []model.CapacityFixtureProduct `json:"products"`
		Devices  []string                       `json:"devices"`
	}
	_ = json.Unmarshal(call("GET", capacityFixturePath, nil, cfg.Ops.CapacityToken, 200), &page)
	if len(page.Products) != 1 || page.Products[0].ProductID != "owned" || page.Products[0].DeviceCount != 2 {
		t.Fatalf("business template was discovered: %+v", page)
	}
	_ = json.Unmarshal(call("GET", capacityFixturePath+"?product=owned", nil, cfg.Ops.CapacityToken, 200), &page)
	if len(page.Devices) != 1 || page.Devices[0] != "fixture" {
		t.Fatalf("repurposed device listed: %+v", page.Devices)
	}
	call("POST", capacityCleanupDataPath, model.CapacityCleanupBatch{Product: "cap-business", Devices: []string{"cap-real"}}, cfg.Ops.CapacityToken, 409)
	call("POST", capacityCleanupDataPath, model.CapacityCleanupBatch{Product: "owned", Devices: []string{"renamed"}}, cfg.Ops.CapacityToken, 409)
	call("POST", capacityCleanupDataPath, model.CapacityCleanupBatch{Devices: []string{"fixture"}}, cfg.Ops.CapacityToken, 400)
	var result model.CapacityCleanupCounts
	_ = json.Unmarshal(call("POST", capacityCleanupDataPath, model.CapacityCleanupBatch{RunID: "cap-20261002-120000-abcdef", Product: "owned", Devices: []string{"fixture"}}, cfg.Ops.CapacityToken, 200), &result)
	if result.Devices != 1 || result.Raw != 1 {
		t.Fatal(result)
	}
	for _, check := range []struct{ tenant, id string }{{"other", "fixture"}, {"t", "renamed"}, {"t", "cap-real"}} {
		if _, err := repo.GetManagedDevice(ctx, check.tenant, check.id); err != nil {
			t.Fatal("device outside the scope removed", check, err)
		}
	}
	if _, err := repo.GetRawIndex(ctx, "other", "raw"); err != nil {
		t.Fatal("other tenant raw removed", err)
	}
	// The repurposed device keeps the product, which is reported, not failed.
	_ = json.Unmarshal(call("POST", capacityCleanupDataPath, model.CapacityCleanupBatch{Product: "owned", RemoveProduct: true}, cfg.Ops.CapacityToken, 200), &result)
	if result.Products != 0 || len(result.Warnings) == 0 {
		t.Fatal(result)
	}
	for _, path := range []string{capacityCleanupAllPath, capacityCleanupStatusPath, capacityFixturePath, capacityCleanupDataPath} {
		if allowsRoute(map[string]bool{"menu:opsCapacity": true}, "GET", path) {
			t.Errorf("menu-only user can access cleanup: %s", path)
		}
		if !allowsRoute(map[string]bool{"menu:opsCapacity": true, capacityCleanupPermission: true}, "GET", path) {
			t.Errorf("cleanup permission was not reused: %s", path)
		}
	}
}

func TestCapacityCleanAllThroughPlatformAndController(t *testing.T) {
	ctx := context.Background()
	repo := memory.NewRepository()
	cfg := config.Load()
	cfg.JWTSecret = "capacity-clean-all-integration-key-32"
	cfg.AdminUser, cfg.AdminTenants = "root", []string{"t"}
	cfg.Ops.Tenants, cfg.Ops.CapacityToken = []string{"t"}, "clean-all-controller-secret"
	api := New(cfg, &core.Engine{Repo: repo}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	root := t.TempDir()
	service := capacity.NewService(capacity.ServeOptions{ResultsDir: root, Token: cfg.Ops.CapacityToken, Self: &capacity.SelfEnvironment{API: server.URL, PostgresDSN: "unused", Metrics: []capacity.MetricsTarget{{Role: "combined", Instance: "a", URL: server.URL + "/metrics"}}}})
	controller := httptest.NewServer(service.Handler())
	defer controller.Close()
	api.cfg.Ops.CapacityURL = controller.URL
	token, err := api.auth.IssueWithVersion("root", "t", "admin", api.adminSessionVersion(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	p, err := capacity.ParsePlan([]byte("schemaVersion: 1\nname: cleanup\nfixtures: {tenant: t, product: cap-standard, deviceCount: 1, reuseDevices: true, autoProvision: true}\nload: {ingressShare: {http: 1}, initialMessagesPerSecond: 1}\n"))
	if err != nil {
		t.Fatal(err)
	}
	plan, _, err := p.Sanitized()
	if err != nil {
		t.Fatal(err)
	}
	writeFile := func(path string, b []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	runs := []string{"cap-20261001-120000-aaaaaa", "cap-20261002-120000-bbbbbb"}
	for _, id := range runs {
		writeFile(filepath.Join(root, id, "plan.sanitized.yaml"), plan)
		writeFile(filepath.Join(root, id, "state.json"), []byte(`{"runId":"`+id+`","status":"FINISHED"}`))
		writeFile(filepath.Join(root, id, "manifest.json"), []byte(`{"runId":"`+id+`","tenant":"t","product":"cap-standard","devices":["cap-000000"]}`))
		writeFile(filepath.Join(root, id, "cleanup-context.json"), []byte(`{"environment":"self"}`))
	}
	_ = repo.SaveProduct(ctx, model.Product{TenantID: "t", ID: "cap-standard", Name: model.CapacityFixtureProductName("cap-standard"), ProtocolPackageID: onboarding.StandardPackageID, Description: model.CapacityFixtureDescription})
	_ = repo.SaveManagedDevice(ctx, model.ManagedDevice{TenantID: "t", ProductID: "cap-standard", ID: "cap-000000", Name: "容量测试 cap-000000", RegistrationSource: "ONBOARDING", AccessKey: "fixture-key"})
	_ = repo.SaveReplay(ctx, model.ReplayRequest{TenantID: "t", ID: "run-replay", CapacityRunID: runs[0], Status: "COMPLETED"})
	// The shared device is kept when only one run is deleted.
	preview := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/ops/capacity/runs/"+runs[0]+"/cleanup", token, nil, 200)
	if preview["devices"] != float64(0) || preview["sharedDevices"] != float64(1) {
		t.Fatal(preview)
	}
	all := requestJSON(t, server.Client(), "GET", server.URL+capacityCleanupAllPath+"?environment=self", token, nil, 200)
	if summary := all["preview"].(map[string]any); summary["runs"] != float64(2) || summary["devices"] != float64(1) {
		t.Fatal(all)
	}
	requestJSON(t, server.Client(), "POST", server.URL+capacityCleanupAllPath, token, map[string]string{"environment": "self"}, 202)
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		status := requestJSON(t, server.Client(), "GET", server.URL+capacityCleanupStatusPath+"?environment=self", token, nil, 200)
		job, _ := status["job"].(map[string]any)
		if job != nil && job["status"] == "SUCCEEDED" {
			break
		}
		if job != nil && job["status"] == "FAILED" || time.Now().After(deadline) {
			t.Fatal("clean all did not succeed", status)
		}
	}
	if _, err = repo.GetProduct(ctx, "t", "cap-standard"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("test product retained", err)
	}
	if _, err = repo.GetReplay(ctx, "run-replay"); !errors.Is(err, model.ErrNotFound) {
		t.Fatal("run task retained", err)
	}
	for _, id := range runs {
		if _, err = os.Stat(filepath.Join(root, id)); !os.IsNotExist(err) {
			t.Fatal("run record retained", id, err)
		}
	}
}
