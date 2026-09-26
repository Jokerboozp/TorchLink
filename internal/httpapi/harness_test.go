package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"iot-platform/internal/adapters/knowledge"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/auth"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

type captureWorkflowRuntime struct {
	mu        sync.Mutex
	requests  []ports.AIWorkflowRequest
	plugins   []ports.AIWorkflowPlugin
	manifests []ports.AIWorkflowManifest
	fail      bool
}

func (f *captureWorkflowRuntime) ListWorkflows(context.Context) ([]ports.AIWorkflowPlugin, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ports.AIWorkflowPlugin{{ID: "ops-assistant", Name: "Ops", Enabled: true}}, f.plugins...), nil
}

func (f *captureWorkflowRuntime) SaveWorkflow(_ context.Context, manifest ports.AIWorkflowManifest) (ports.AIWorkflowPlugin, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	knowledge := false
	for _, tool := range manifest.AllowedTools {
		if tool == "mcp__iot__query_knowledge_base" {
			knowledge = true
		}
	}
	plugin := ports.AIWorkflowPlugin{SchemaVersion: manifest.SchemaVersion, ID: manifest.ID, Name: manifest.Name, Description: manifest.Description, Version: manifest.Version, DefaultModel: manifest.DefaultModel, MaxTokens: manifest.MaxTokens, Enabled: manifest.Enabled, Capabilities: manifest.Capabilities, KnowledgeEnabled: knowledge}
	updated := false
	for index := range f.plugins {
		if f.plugins[index].ID == plugin.ID {
			f.plugins[index] = plugin
			updated = true
			break
		}
	}
	if !updated {
		f.plugins = append(f.plugins, plugin)
	}
	updated = false
	for index := range f.manifests {
		if f.manifests[index].ID == manifest.ID {
			f.manifests[index] = manifest
			updated = true
			break
		}
	}
	if !updated {
		f.manifests = append(f.manifests, manifest)
	}
	return plugin, nil
}

func (f *captureWorkflowRuntime) ListWorkflowManifests(context.Context) ([]ports.AIWorkflowManifest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ports.AIWorkflowManifest(nil), f.manifests...), nil
}

func (f *captureWorkflowRuntime) DeleteWorkflow(_ context.Context, workflowID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for index, manifest := range f.manifests {
		if manifest.ID != workflowID {
			continue
		}
		f.manifests = append(f.manifests[:index], f.manifests[index+1:]...)
		for pluginIndex, plugin := range f.plugins {
			if plugin.ID == workflowID {
				f.plugins = append(f.plugins[:pluginIndex], f.plugins[pluginIndex+1:]...)
				break
			}
		}
		return nil
	}
	return errors.New("workflow not found")
}

func (f *captureWorkflowRuntime) StreamChat(_ context.Context, in ports.AIWorkflowRequest, emit func(ports.AIWorkflowEvent) error) (ports.AIWorkflowResult, error) {
	f.mu.Lock()
	f.requests = append(f.requests, in)
	f.mu.Unlock()
	if f.fail {
		if emit != nil {
			_ = emit(ports.AIWorkflowEvent{Type: "run.started", RunID: in.RunID})
			_ = emit(ports.AIWorkflowEvent{Type: "run.failed", RunID: in.RunID, Code: "HARNESS_FAILED", Message: "Harness runtime request failed"})
		}
		return ports.AIWorkflowResult{RunID: in.RunID}, errors.New("sensitive internal runtime error")
	}
	for _, event := range []ports.AIWorkflowEvent{
		{Type: "run.started", RunID: in.RunID, WorkflowID: in.WorkflowID, Data: map[string]any{
			"conversationId": in.ConversationID,
			"visible":        "ok",
			"nested": map[string]any{
				"sessionId": "internal-session",
				"items":     []any{map[string]any{"api_key": "internal-api-key", "safe": "nested-ok"}},
			},
		}},
		{Type: "text.delta", RunID: in.RunID, Delta: "safe "},
		{Type: "run.completed", RunID: in.RunID, WorkflowID: in.WorkflowID, Answer: "safe answer"},
	} {
		if emit != nil {
			if err := emit(event); err != nil {
				return ports.AIWorkflowResult{RunID: in.RunID}, err
			}
		}
	}
	return ports.AIWorkflowResult{RunID: in.RunID, WorkflowID: in.WorkflowID, Answer: "safe answer"}, nil
}

func (*captureWorkflowRuntime) Health(context.Context) error { return nil }

func TestHarnessHTTPBridgeAndTenantScopedConversation(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	runtime := &captureWorkflowRuntime{
		plugins: []ports.AIWorkflowPlugin{
			{ID: "alarm-handler", Name: "AI Alarm Handler", Enabled: true},
			{ID: "device-health-inspector", Name: "AI Device Health Inspector", Enabled: true},
			{ID: "protocol-assistant", Name: "AI Protocol Assistant", Enabled: true},
		},
		manifests: []ports.AIWorkflowManifest{
			{ID: "alarm-handler", Name: "AI Alarm Handler"},
			{ID: "device-health-inspector", Name: "AI Device Health Inspector"},
			{ID: "protocol-assistant", Name: "AI Protocol Assistant"},
		},
	}
	engine.AIWorkflows = runtime
	registry := metrics.New()
	cfg := config.Load()
	cfg.JWTSecret = "test-secret-at-least-32-characters"
	api := New(cfg, engine, registry, slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, err := api.auth.Issue("alice", "tenant-a", "viewer", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	operatorToken, err := api.auth.Issue("operator", "tenant-a", "operator", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	adminToken, err := api.auth.Issue("admin", "tenant-a", "admin", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	workflows := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/workflows", token, nil, http.StatusOK)
	if workflows["configured"] != true || workflows["healthy"] != true || workflows["count"].(float64) != 1 {
		t.Fatalf("unexpected workflows response: %#v", workflows)
	}
	manifest := map[string]any{"schemaVersion": 1, "id": "custom-status", "name": "Custom Status", "description": "Status statistics", "version": "1.0.0", "enabled": true, "persona": "Always query the system overview before answering status questions.", "defaultModel": "deepseek-v4-flash", "maxTokens": 2048, "capabilities": []string{"status"}, "allowedTools": []string{"mcp__iot__query_system_overview"}}
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/workflows", operatorToken, manifest, http.StatusForbidden)
	unsafe := map[string]any{}
	for key, value := range manifest {
		unsafe[key] = value
	}
	unsafe["id"] = "unsafe-agent"
	unsafe["allowedTools"] = []string{"mcp__iot__control_device"}
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/workflows", adminToken, unsafe, http.StatusUnprocessableEntity)
	createdAgent := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/workflows", adminToken, manifest, http.StatusCreated)
	if createdAgent["id"] != "custom-status" || createdAgent["name"] != "Custom Status" {
		t.Fatalf("unexpected dynamic Agent: %#v", createdAgent)
	}
	managed := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/workflows/admin", adminToken, nil, http.StatusOK)
	if managed["count"] != float64(1) || managed["items"].([]any)[0].(map[string]any)["persona"] != manifest["persona"] {
		t.Fatalf("unexpected managed Agent catalog: %#v", managed)
	}
	requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/workflows/admin", token, nil, http.StatusForbidden)
	updatedManifest := map[string]any{}
	for key, value := range manifest {
		updatedManifest[key] = value
	}
	updatedManifest["enabled"] = false
	updated := requestJSON(t, server.Client(), http.MethodPut, server.URL+"/api/v1/ai/workflows/custom-status", adminToken, updatedManifest, http.StatusOK)
	if updated["enabled"] != false {
		t.Fatalf("workflow update did not persist enabled=false: %#v", updated)
	}
	requestJSON(t, server.Client(), http.MethodDelete, server.URL+"/api/v1/ai/workflows/custom-status", adminToken, nil, http.StatusOK)
	requestJSON(t, server.Client(), http.MethodDelete, server.URL+"/api/v1/ai/workflows/ops-assistant", adminToken, nil, http.StatusConflict)
	defaultBinding := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/workflows/ops-assistant/knowledge-binding", token, nil, http.StatusOK)
	if defaultBinding["retrievalMode"] != "auto" || defaultBinding["topK"] != float64(5) {
		t.Fatalf("unexpected default knowledge binding: %#v", defaultBinding)
	}
	requestJSON(t, server.Client(), http.MethodPut, server.URL+"/api/v1/ai/workflows/ops-assistant/knowledge-binding", token, map[string]any{"retrievalMode": "disabled", "topK": 5, "minScore": .2, "noMatchPolicy": "allow-model"}, http.StatusForbidden)
	savedBinding := requestJSON(t, server.Client(), http.MethodPut, server.URL+"/api/v1/ai/workflows/ops-assistant/knowledge-binding", operatorToken, map[string]any{"retrievalMode": "auto", "topK": 3, "minScore": .4, "noMatchPolicy": "allow-model"}, http.StatusOK)
	if savedBinding["workflowId"] != "ops-assistant" || savedBinding["topK"] != float64(3) {
		t.Fatalf("unexpected saved knowledge binding: %#v", savedBinding)
	}
	chat := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/chat", token, map[string]any{"question": "status?", "workflowId": "ops-assistant", "conversationId": "browser-controlled", "model": "deepseek-chat", "maxTokens": 99999}, http.StatusOK)
	if chat["answer"] != "safe answer" || !strings.HasPrefix(chat["runId"].(string), "ai_run_") {
		t.Fatalf("unexpected rich chat response: %#v", chat)
	}
	if _, leaked := chat["mcpToken"]; leaked {
		t.Fatalf("MCP credential leaked to browser: %#v", chat)
	}

	runtime.mu.Lock()
	captured := runtime.requests[0]
	runtime.mu.Unlock()
	claims, err := api.auth.Parse(captured.MCPToken)
	if err != nil {
		t.Fatal(err)
	}
	if claims.TokenUse != "harness" || claims.RunID != captured.RunID || !claims.HasAudience(auth.HarnessAudience) || len(claims.Scopes) != len(auth.HarnessReadScopes()) {
		t.Fatalf("unsafe harness token: %#v", claims)
	}
	if claims.Knowledge == nil || claims.Knowledge.WorkflowID != "ops-assistant" || claims.Knowledge.TopK != 3 || claims.Knowledge.MinScore != .4 {
		t.Fatalf("knowledge binding was not enforced in harness token: %#v", claims.Knowledge)
	}
	if !strings.Contains(captured.Question, "平台知识策略") || !strings.Contains(captured.Question, "ops-assistant") {
		t.Fatalf("knowledge policy was not supplied to harness: %q", captured.Question)
	}
	if captured.ConversationID == "browser-controlled" || captured.ConversationID != harnessConversationID("tenant-a", "alice", "browser-controlled") {
		t.Fatalf("conversation ID was not tenant scoped: %q", captured.ConversationID)
	}
	if captured.MaxTokens != 8192 {
		t.Fatalf("maxTokens was not clamped: %d", captured.MaxTokens)
	}
	if captured.Model != "deepseek-chat" {
		t.Fatalf("model was not passed to harness: %q", captured.Model)
	}

	engine.KB = knowledge.NewLocal()
	if err = engine.KB.(ports.FilteredKnowledgeBase).IndexKnowledge(context.Background(), ports.KnowledgeIndexInput{TenantID: "tenant-a", WorkflowID: "ops-assistant", ProductID: "fire-smoke", Category: "alarm-sop", Tags: []string{"certified"}, DocumentID: "doc-1", ChunkID: "chunk-1", Content: []byte("烟雾 告警 处置 需要 现场 复核")}); err != nil {
		t.Fatal(err)
	}
	requestJSON(t, server.Client(), http.MethodPut, server.URL+"/api/v1/ai/workflows/ops-assistant/knowledge-binding", operatorToken, map[string]any{"retrievalMode": "always", "topK": 3, "minScore": .5, "noMatchPolicy": "require-evidence"}, http.StatusOK)
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/chat", token, map[string]any{"question": "烟雾 告警", "workflowId": "ops-assistant"}, http.StatusOK)
	runtime.mu.Lock()
	forced := runtime.requests[len(runtime.requests)-1]
	runtime.mu.Unlock()
	if !strings.Contains(forced.Question, "平台强制召回的知识证据") || !strings.Contains(forced.Question, "现场 复核") {
		t.Fatalf("forced knowledge evidence was not supplied to Harness: %q", forced.Question)
	}

	api.SetAIProviderRuntime(&providerConfigTestRuntime{config: ports.AIPluginConfig{Provider: "deepseek", Model: "deepseek-chat", MaxTokens: 3072}})
	streamReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/ai/chat/stream", bytes.NewBufferString(`{"question":"stream?","conversationId":"browser-controlled"}`))
	streamReq.Header.Set("Authorization", "Bearer "+token)
	streamReq.Header.Set("Content-Type", "application/json")
	streamResp, err := server.Client().Do(streamReq)
	if err != nil {
		t.Fatal(err)
	}
	streamBody, _ := io.ReadAll(streamResp.Body)
	streamResp.Body.Close()
	if streamResp.StatusCode != http.StatusOK || !strings.Contains(string(streamBody), "event: run.started") || !strings.Contains(string(streamBody), "event: text.delta") || !strings.Contains(string(streamBody), "event: run.completed") {
		t.Fatalf("unexpected SSE response: status=%d body=%s", streamResp.StatusCode, streamBody)
	}
	if strings.Contains(string(streamBody), "conv_") || strings.Contains(string(streamBody), "internal-session") || strings.Contains(string(streamBody), "internal-api-key") || !strings.Contains(string(streamBody), `"visible":"ok"`) || !strings.Contains(string(streamBody), `"safe":"nested-ok"`) {
		t.Fatalf("SSE leaked internal conversation/session data: %s", streamBody)
	}
	runtime.mu.Lock()
	streamRequest := runtime.requests[len(runtime.requests)-1]
	runtime.mu.Unlock()
	if streamRequest.MaxTokens != 3072 {
		t.Fatalf("configured maxTokens was not used by stream chat: %d", streamRequest.MaxTokens)
	}

	runtime.fail = true
	failedSync := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/chat", token, map[string]any{"question": "fail?"}, http.StatusBadGateway)
	if errorMessage, _ := failedSync["detail"].(string); errorMessage != "AI workflow request failed" || strings.Contains(errorMessage, "sensitive internal runtime error") {
		t.Fatalf("sync workflow leaked an internal error: %#v", failedSync)
	}
	failedReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/ai/chat/stream", bytes.NewBufferString(`{"question":"fail?"}`))
	failedReq.Header.Set("Authorization", "Bearer "+token)
	failedReq.Header.Set("Content-Type", "application/json")
	failedResp, err := server.Client().Do(failedReq)
	if err != nil {
		t.Fatal(err)
	}
	failedBody, _ := io.ReadAll(failedResp.Body)
	failedResp.Body.Close()
	if count := strings.Count(string(failedBody), "event: run.failed"); count != 1 || strings.Contains(string(failedBody), "sensitive internal runtime error") {
		t.Fatalf("unsafe or duplicate terminal event: %s", failedBody)
	}

	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/mcp/harness", token, map[string]any{}, http.StatusForbidden)
	getMCP, _ := http.NewRequest(http.MethodGet, server.URL+"/mcp/harness", nil)
	getMCP.Header.Set("Authorization", "Bearer "+token)
	getMCPResp, err := server.Client().Do(getMCP)
	if err != nil {
		t.Fatal(err)
	}
	getMCPResp.Body.Close()
	if getMCPResp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /mcp/harness status=%d", getMCPResp.StatusCode)
	}
	harnessToken, err := api.auth.IssueHarness("alice", "tenant-a", "run-x", auth.HarnessReadScopes(), 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/workflows", harnessToken, nil, http.StatusForbidden)
}

func TestHarnessConversationIDIsStableAndTenantScoped(t *testing.T) {
	a := harnessConversationID("tenant-a", "alice", "conversation-1")
	if a != harnessConversationID("tenant-a", "alice", "conversation-1") {
		t.Fatal("conversation derivation is not stable")
	}
	if a == harnessConversationID("tenant-b", "alice", "conversation-1") || a == harnessConversationID("tenant-a", "bob", "conversation-1") {
		t.Fatal("conversation derivation is not tenant/user scoped")
	}
	if !strings.HasPrefix(a, "conv_") || strings.Contains(a, "tenant-a") || strings.Contains(a, "alice") {
		t.Fatalf("conversation derivation leaks identity: %q", a)
	}
}
