package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	aiadapter "iot-platform/internal/adapters/ai"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

func TestAIWorkflowRunManagementPermissionsAndTenantIsolation(t *testing.T) {
	var stopped atomic.Int32
	harness := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-IOT-Harness-Token") != "0123456789abcdef0123456789abcdef" {
			t.Error("missing service authentication")
		}
		if r.Method == http.MethodGet {
			// The adapter and API must still filter if an upstream sends extra rows.
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []ports.AIWorkflowRun{
				{RunID: "run-a", TenantID: "tenant-a", Actor: "admin", Status: "running"},
				{RunID: "run-b", TenantID: "tenant-b", Actor: "other", Status: "running"},
			}})
			return
		}
		if r.Header.Get("X-IOT-Tenant-ID") != "tenant-a" || r.URL.Path != "/v1/runs/run-a/stop" {
			w.WriteHeader(404)
			return
		}
		stopped.Add(1)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer harness.Close()
	runtime, err := aiadapter.NewHarnessPool(harness.URL, "0123456789abcdef0123456789abcdef", "http://localhost:8081/mcp/harness", "model", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	repo := memory.NewRepository()
	api := New(config.Config{DevMode: true}, &core.Engine{Repo: repo, AIWorkflows: runtime}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := newTestHTTPServer(api)
	defer server.Close()
	issue := func(user, tenant, role string) string {
		token, err := api.auth.Issue(user, tenant, role, nil, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	admin := issue("admin", "tenant-a", "admin")
	viewer := issue("viewer", "tenant-a", "viewer")
	other := issue("admin", "tenant-b", "admin")
	call := func(method, path, token string, status int) map[string]any {
		return requestJSON(t, server.Client(), method, server.URL+path, token, nil, status)
	}
	call("GET", "/api/v1/ai/runs", "", 401)
	call("GET", "/api/v1/ai/runs", viewer, 403)
	call("POST", "/api/v1/ai/runs/run-a/stop", viewer, 403)
	rows := call("GET", "/api/v1/ai/runs", admin, 200)["items"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["runId"] != "run-a" {
		t.Fatalf("cross-tenant run list: %v", rows)
	}
	call("POST", "/api/v1/ai/runs/run-a/stop", other, 404)
	if stopped.Load() != 0 {
		t.Fatal("cross-tenant stop reached runtime")
	}
	result := call("POST", "/api/v1/ai/runs/run-a/stop", admin, 202)
	if result["status"] != "stopping" || stopped.Load() != 1 {
		t.Fatal("stop was not accepted")
	}
	for _, scenario := range []struct {
		name, scope string
		permissions []string
		list, stop  int
	}{
		{"read-only", "all", []string{"GET /api/v1/ai/runs"}, 200, 403},
		{"limited", "selected", []string{"GET /api/v1/ai/runs", "POST /api/v1/ai/runs/:id/stop"}, 403, 403},
		{"manager", "all", []string{"GET /api/v1/ai/runs", "POST /api/v1/ai/runs/:id/stop"}, 200, 202},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			user := model.PlatformUser{Username: scenario.name, Enabled: true, SessionVersion: 1, DeviceScope: scenario.scope, Permissions: append([]string{"menu:devices", "menu:aiProviders"}, scenario.permissions...)}
			state, err := repo.LoadAccessState(context.Background(), "tenant-a")
			if err != nil {
				t.Fatal(err)
			}
			state.Users = []model.PlatformUser{user}
			if saved, err := repo.SaveAccessState(context.Background(), "tenant-a", state); err != nil || !saved {
				t.Fatal(err)
			}
			token, err := api.auth.IssueUser(user.Username, "tenant-a", 1, time.Hour)
			if err != nil {
				t.Fatal(err)
			}
			requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/ai/runs", token, nil, scenario.list)
			requestJSON(t, server.Client(), "POST", server.URL+"/api/v1/ai/runs/run-a/stop", token, nil, scenario.stop)
		})
	}
}

// Run history and usage are operation permissions like the run list: listed
// in the catalog, granted per role, and limited to full device scope.
func TestAIRunHistoryPermissionsAndTenantIsolation(t *testing.T) {
	repo := memory.NewRepository()
	engine := &core.Engine{Repo: repo, AIRuns: repo}
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := newTestHTTPServer(api)
	defer server.Close()
	now := time.Now()
	for _, run := range []model.AIRunRecord{
		{RunID: "a1", TenantID: "tenant-a", WorkflowID: "alarm-handler", Status: model.AIRunSucceeded, Usage: model.AIUsage{InputTokens: 10}, StartedAt: now.UnixMilli()},
		{RunID: "b1", TenantID: "tenant-b", WorkflowID: "alarm-handler", Status: model.AIRunSucceeded, StartedAt: now.UnixMilli()},
	} {
		if err := repo.SaveAIRun(context.Background(), run); err != nil {
			t.Fatal(err)
		}
	}
	catalog := map[string]bool{}
	for _, item := range api.permissionCatalog() {
		catalog[item.ID] = true
	}
	if !catalog["GET /api/v1/ai/runs/history"] || !catalog["GET /api/v1/ai/runs/usage"] {
		t.Fatal("run history permissions are not assignable")
	}
	admin, err := api.auth.Issue("admin", "tenant-a", "admin", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	history := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/ai/runs/history", admin, nil, 200)
	if items := history["items"].([]any); len(items) != 1 || items[0].(map[string]any)["runId"] != "a1" {
		t.Fatalf("history %v", history)
	}
	usage := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/ai/runs/usage?workflowId=alarm-handler", admin, nil, 200)
	if items := usage["items"].([]any); len(items) != 1 {
		t.Fatalf("usage %v", usage)
	}
	for _, scenario := range []struct {
		name, scope string
		permissions []string
		status      int
	}{
		{"menu-only", "all", nil, 403},
		{"granted", "all", []string{"GET /api/v1/ai/runs/history"}, 200},
		{"limited", "selected", []string{"GET /api/v1/ai/runs/history"}, 403},
	} {
		user := model.PlatformUser{Username: scenario.name, Enabled: true, SessionVersion: 1, DeviceScope: scenario.scope, Permissions: append([]string{"menu:devices", "menu:aiProviders"}, scenario.permissions...)}
		state, err := repo.LoadAccessState(context.Background(), "tenant-a")
		if err != nil {
			t.Fatal(err)
		}
		state.Users = []model.PlatformUser{user}
		if saved, err := repo.SaveAccessState(context.Background(), "tenant-a", state); err != nil || !saved {
			t.Fatal(err)
		}
		token, err := api.auth.IssueUser(user.Username, "tenant-a", 1, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/ai/runs/history", token, nil, scenario.status)
	}
}
