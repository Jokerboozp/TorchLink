package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
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
	"iot-platform/internal/ports"
)

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
	identity := ports.AIRunIdentity{Username: "plain", ManagedUser: true, SessionVersion: plainClaims.SessionVersion, Scopes: []string{auth.ScopeQueryAlarmList}}
	callTool := func(token string, status int) {
		t.Helper()
		requestJSON(t, srv.Client(), "POST", srv.URL+"/mcp/harness", token, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "query_alarm_list", "arguments": map[string]any{}}}, status)
	}
	businessToken, err := api.auth.IssueBusinessRunToken("tenant-a", identity, "run-business", core.WorkflowAlarmAnalysis, identity.Scopes, nil, time.Minute)
	must(err)
	callTool(businessToken, 200)
	chatToken, err := api.auth.IssueHarnessForIdentity(plainClaims, "run-chat", identity.Scopes, nil, time.Minute)
	must(err)
	callTool(chatToken, 403)
	draftToken, err := api.auth.IssueBusinessRunToken("tenant-a", identity, "run-draft", core.WorkflowRuleDraft, identity.Scopes, nil, time.Minute)
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
