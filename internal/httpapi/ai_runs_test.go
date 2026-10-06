package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"iot-platform/internal/devicescope"
	"iot-platform/internal/protocolruntime"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
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
	api := New(config.Config{DevMode: true}, &core.Engine{Repo: devicescope.Wrap(repo), AIWorkflows: runtime}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	engine := &core.Engine{Repo: devicescope.Wrap(repo), AIRuns: repo}
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
	admin, err := api.auth.IssueWithVersion("admin", "tenant-a", "admin", api.adminSessionVersion(), time.Hour)
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

// Device health signals follow the device scope like the rest of the device.
func TestDeviceSignalsFollowDeviceScope(t *testing.T) {
	repo := memory.NewRepository()
	engine := &core.Engine{Repo: devicescope.Wrap(repo), DeviceSignals: repo}
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := newTestHTTPServer(api)
	defer server.Close()
	ctx := context.Background()
	for _, id := range []string{"mine", "hidden"} {
		_ = repo.SaveManagedDevice(ctx, model.ManagedDevice{ID: id, TenantID: "tenant-a", ProductID: "p", Name: id, AccessKey: "ak-" + id})
	}
	if err := repo.ReplaceDeviceSignals(ctx, "tenant-a", []model.DeviceSignal{{TenantID: "tenant-a", DeviceID: "mine", SignalType: model.SignalStuckValue, Property: "t", Strength: 0.6}, {TenantID: "tenant-a", DeviceID: "hidden", SignalType: model.SignalStuckValue, Property: "t", Strength: 0.6}}); err != nil {
		t.Fatal(err)
	}
	user := model.PlatformUser{Username: "scoped", Enabled: true, SessionVersion: 1, DeviceScope: "selected", DeviceIDs: []string{"mine"}, Permissions: []string{"menu:devices"}}
	state, err := repo.LoadAccessState(ctx, "tenant-a")
	if err != nil {
		t.Fatal(err)
	}
	state.Users = []model.PlatformUser{user}
	if saved, err := repo.SaveAccessState(ctx, "tenant-a", state); err != nil || !saved {
		t.Fatal(err)
	}
	token, err := api.auth.IssueUser(user.Username, "tenant-a", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	mine := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/device-registry/mine/signals", token, nil, 200)
	if items := mine["items"].([]any); len(items) != 1 || items[0].(map[string]any)["deviceId"] != "mine" {
		t.Fatalf("signals %v", mine)
	}
	requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/device-registry/hidden/signals", token, nil, 403)
}

type notLocalCommander struct{}

func (notLocalCommander) Command(context.Context, string, string, string, map[string]any) (map[string]any, error) {
	return nil, protocolruntime.ErrListenerNotLocal
}

// A command for a listener this replica does not run says how to route it
// instead of returning the runtime's internal error.
func TestProtocolCommandWithoutLocalListenerIsActionable(t *testing.T) {
	for _, coordination := range []bool{false, true} {
		repo := memory.NewRepository()
		if err := repo.SaveDeviceAccessProfile(context.Background(), model.DeviceAccessProfile{ID: "listen", TenantID: "tenant-a", ProductID: "p", Mode: "listener", Network: "tcp", Enabled: true}); err != nil {
			t.Fatal(err)
		}
		api := New(config.Config{DevMode: true, AccessCoordination: coordination}, &core.Engine{Repo: devicescope.Wrap(repo)}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
		api.SetProtocolListeners(notLocalCommander{})
		server := newTestHTTPServer(api)
		admin, err := api.auth.IssueWithVersion("admin", "tenant-a", "admin", api.adminSessionVersion(), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		want, keyword := http.StatusConflict, "IOT_ACCESS_COORDINATION"
		if coordination {
			want, keyword = http.StatusServiceUnavailable, "没有在任何接入副本上运行"
		}
		body := requestJSON(t, server.Client(), "POST", server.URL+"/api/v2/device-access-profiles/listen/devices/d1/commands", admin, map[string]any{"type": "read", "confirmed": true}, want)
		if detail, _ := body["detail"].(string); !strings.Contains(detail, keyword) {
			t.Fatalf("coordination=%v detail=%q", coordination, detail)
		}
		server.Close()
	}
}
