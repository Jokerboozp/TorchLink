package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/auth"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

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

	c, err := api.auth.Parse(first.MCPToken)
	must(err)
	if !c.ManagedUser || c.SessionVersion == 0 || c.HasScope(auth.ScopeCreateRuleDraft) || c.HasScope(auth.ScopeQueryKnowledgeBase) {
		t.Fatal("assistant token does not retain user authority")
	}
	for _, name := range []string{"query_alarm_list", "query_similar_alarms"} {
		text := tool(first.MCPToken, name, map[string]any{}, false)
		if !strings.Contains(text, "alarm-allowed") || strings.Contains(text, "hidden") || strings.Contains(text, "foreign") {
			t.Fatalf("%s leaked data: %s", name, text)
		}
		if text = tool(first.MCPToken, name, map[string]any{"deviceId": "hidden"}, false); text != "[]" {
			t.Fatal("device filter bypass", text)
		}
	}
	tool(first.MCPToken, "query_device_latest", map[string]any{"deviceId": "allowed"}, false)
	for _, id := range []string{"hidden", "foreign"} {
		tool(first.MCPToken, "query_device_latest", map[string]any{"deviceId": id}, true)
		tool(first.MCPToken, "query_property_history", map[string]any{"deviceId": id, "propertyCode": "temperature", "start": 0, "end": time.Now().UnixMilli()}, true)
	}
	overview := map[string]any{}
	must(json.Unmarshal([]byte(tool(first.MCPToken, "query_system_overview", nil, false)), &overview))
	if overview["devices"].(map[string]any)["total"] != float64(1) || overview["alarms"].(map[string]any)["loaded"] != float64(1) {
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
	if text := tool(none.MCPToken, "query_alarm_list", nil, false); text != "[]" {
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
