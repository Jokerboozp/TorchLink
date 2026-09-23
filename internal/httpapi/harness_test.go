package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"             /* 执行当前语句并推进处理流程。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"errors"            /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"strings"           /* 执行当前语句并推进处理流程。 */
	"sync"              /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
	"time"              /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/adapters/knowledge" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/local"     /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory"    /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/auth"               /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"             /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"               /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"            /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"             /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"              /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type captureWorkflowRuntime struct { /* 定义 captureWorkflowRuntime 类型。 */
	mu        sync.Mutex                 /* 执行当前语句并推进处理流程。 */
	requests  []ports.AIWorkflowRequest  /* 执行当前语句并推进处理流程。 */
	plugins   []ports.AIWorkflowPlugin   /* 执行当前语句并推进处理流程。 */
	manifests []ports.AIWorkflowManifest /* 执行当前语句并推进处理流程。 */
	fail      bool                       /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (f *captureWorkflowRuntime) ListWorkflows(context.Context) ([]ports.AIWorkflowPlugin, error) { /* 定义 ListWorkflows 函数。 */
	f.mu.Lock()                                                                                                   /* 执行当前语句并推进处理流程。 */
	defer f.mu.Unlock()                                                                                           /* 安排函数结束时执行清理。 */
	return append([]ports.AIWorkflowPlugin{{ID: "ops-assistant", Name: "Ops", Enabled: true}}, f.plugins...), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (f *captureWorkflowRuntime) SaveWorkflow(_ context.Context, manifest ports.AIWorkflowManifest) (ports.AIWorkflowPlugin, error) { /* 定义 SaveWorkflow 函数。 */
	f.mu.Lock()                                  /* 执行当前语句并推进处理流程。 */
	defer f.mu.Unlock()                          /* 安排函数结束时执行清理。 */
	knowledge := false                           /* 更新 knowledge 的值。 */
	for _, tool := range manifest.AllowedTools { /* 循环处理当前数据。 */
		if tool == "mcp__iot__query_knowledge_base" { /* 判断条件并选择处理分支。 */
			knowledge = true /* 更新 knowledge 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	plugin := ports.AIWorkflowPlugin{SchemaVersion: manifest.SchemaVersion, ID: manifest.ID, Name: manifest.Name, Description: manifest.Description, Version: manifest.Version, DefaultModel: manifest.DefaultModel, MaxTokens: manifest.MaxTokens, Enabled: manifest.Enabled, Capabilities: manifest.Capabilities, KnowledgeEnabled: knowledge} /* 更新 plugin 的值。 */
	updated := false                                                                                                                                                                                                                                                                                                                             /* 更新 updated 的值。 */
	for index := range f.plugins {                                                                                                                                                                                                                                                                                                               /* 循环处理当前数据。 */
		if f.plugins[index].ID == plugin.ID { /* 判断条件并选择处理分支。 */
			f.plugins[index] = plugin /* 更新 f.plugins[index] 的值。 */
			updated = true            /* 更新 updated 的值。 */
			break                     /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if !updated { /* 判断条件并选择处理分支。 */
		f.plugins = append(f.plugins, plugin) /* 更新 f.plugins 的值。 */
	} /* 结束当前表达式或代码块。 */
	updated = false                  /* 更新 updated 的值。 */
	for index := range f.manifests { /* 循环处理当前数据。 */
		if f.manifests[index].ID == manifest.ID { /* 判断条件并选择处理分支。 */
			f.manifests[index] = manifest /* 更新 f.manifests[index] 的值。 */
			updated = true                /* 更新 updated 的值。 */
			break                         /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if !updated { /* 判断条件并选择处理分支。 */
		f.manifests = append(f.manifests, manifest) /* 更新 f.manifests 的值。 */
	} /* 结束当前表达式或代码块。 */
	return plugin, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (f *captureWorkflowRuntime) ListWorkflowManifests(context.Context) ([]ports.AIWorkflowManifest, error) { /* 定义 ListWorkflowManifests 函数。 */
	f.mu.Lock()                                                         /* 执行当前语句并推进处理流程。 */
	defer f.mu.Unlock()                                                 /* 安排函数结束时执行清理。 */
	return append([]ports.AIWorkflowManifest(nil), f.manifests...), nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (f *captureWorkflowRuntime) DeleteWorkflow(_ context.Context, workflowID string) error { /* 定义 DeleteWorkflow 函数。 */
	f.mu.Lock()                                /* 执行当前语句并推进处理流程。 */
	defer f.mu.Unlock()                        /* 安排函数结束时执行清理。 */
	for index, manifest := range f.manifests { /* 循环处理当前数据。 */
		if manifest.ID != workflowID { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		f.manifests = append(f.manifests[:index], f.manifests[index+1:]...) /* 更新 f.manifests 的值。 */
		for pluginIndex, plugin := range f.plugins {                        /* 循环处理当前数据。 */
			if plugin.ID == workflowID { /* 判断条件并选择处理分支。 */
				f.plugins = append(f.plugins[:pluginIndex], f.plugins[pluginIndex+1:]...) /* 更新 f.plugins 的值。 */
				break                                                                     /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return errors.New("workflow not found") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (f *captureWorkflowRuntime) StreamChat(_ context.Context, in ports.AIWorkflowRequest, emit func(ports.AIWorkflowEvent) error) (ports.AIWorkflowResult, error) { /* 定义 StreamChat 函数。 */
	f.mu.Lock()                         /* 执行当前语句并推进处理流程。 */
	f.requests = append(f.requests, in) /* 更新 f.requests 的值。 */
	f.mu.Unlock()                       /* 执行当前语句并推进处理流程。 */
	if f.fail {                         /* 判断条件并选择处理分支。 */
		if emit != nil { /* 判断条件并选择处理分支。 */
			_ = emit(ports.AIWorkflowEvent{Type: "run.started", RunID: in.RunID})                                                                   /* 更新 _ 的值。 */
			_ = emit(ports.AIWorkflowEvent{Type: "run.failed", RunID: in.RunID, Code: "HARNESS_FAILED", Message: "Harness runtime request failed"}) /* 更新 _ 的值。 */
		} /* 结束当前表达式或代码块。 */
		return ports.AIWorkflowResult{RunID: in.RunID}, errors.New("sensitive internal runtime error") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	for _, event := range []ports.AIWorkflowEvent{ /* 循环处理当前数据。 */
		{Type: "run.started", RunID: in.RunID, WorkflowID: in.WorkflowID, Data: map[string]any{ /* 执行当前语句并推进处理流程。 */
			"conversationId": in.ConversationID, /* 执行当前语句并推进处理流程。 */
			"visible":        "ok",              /* 执行当前语句并推进处理流程。 */
			"nested": map[string]any{ /* 执行当前语句并推进处理流程。 */
				"sessionId": "internal-session",                                                        /* 执行当前语句并推进处理流程。 */
				"items":     []any{map[string]any{"api_key": "internal-api-key", "safe": "nested-ok"}}, /* 执行当前语句并推进处理流程。 */
			}, /* 结束当前表达式或代码块。 */
		}}, /* 结束当前表达式或代码块。 */
		{Type: "text.delta", RunID: in.RunID, Delta: "safe "},                                      /* 执行当前语句并推进处理流程。 */
		{Type: "run.completed", RunID: in.RunID, WorkflowID: in.WorkflowID, Answer: "safe answer"}, /* 执行当前语句并推进处理流程。 */
	} { /* 结束当前表达式或代码块。 */
		if emit != nil { /* 判断条件并选择处理分支。 */
			if err := emit(event); err != nil { /* 判断条件并选择处理分支。 */
				return ports.AIWorkflowResult{RunID: in.RunID}, err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return ports.AIWorkflowResult{RunID: in.RunID, WorkflowID: in.WorkflowID, Answer: "safe answer"}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (*captureWorkflowRuntime) Health(context.Context) error { return nil } /* 定义 Health 函数。 */

func TestHarnessHTTPBridgeAndTenantScopedConversation(t *testing.T) { /* 定义 TestHarnessHTTPBridgeAndTenantScopedConversation 函数。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 engine 的值。 */
	runtime := &captureWorkflowRuntime{                                                                                                                             /* 更新 runtime 的值。 */
		plugins: []ports.AIWorkflowPlugin{ /* 执行当前语句并推进处理流程。 */
			{ID: "alarm-handler", Name: "AI Alarm Handler", Enabled: true},                     /* 执行当前语句并推进处理流程。 */
			{ID: "device-health-inspector", Name: "AI Device Health Inspector", Enabled: true}, /* 执行当前语句并推进处理流程。 */
			{ID: "protocol-assistant", Name: "AI Protocol Assistant", Enabled: true},           /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
		manifests: []ports.AIWorkflowManifest{ /* 执行当前语句并推进处理流程。 */
			{ID: "alarm-handler", Name: "AI Alarm Handler"},                     /* 执行当前语句并推进处理流程。 */
			{ID: "device-health-inspector", Name: "AI Device Health Inspector"}, /* 执行当前语句并推进处理流程。 */
			{ID: "protocol-assistant", Name: "AI Protocol Assistant"},           /* 执行当前语句并推进处理流程。 */
		}, /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	engine.AIWorkflows = runtime                                                      /* 更新 engine.AIWorkflows 的值。 */
	registry := metrics.New()                                                         /* 更新 registry 的值。 */
	cfg := config.Load()                                                              /* 更新 cfg 的值。 */
	cfg.JWTSecret = "test-secret-at-least-32-characters"                              /* 更新 cfg.JWTSecret 的值。 */
	api := New(cfg, engine, registry, slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 api 的值。 */
	server := httptest.NewServer(api.Handler())                                       /* 更新 server 的值。 */
	defer server.Close()                                                              /* 安排函数结束时执行清理。 */
	token, err := api.auth.Issue("alice", "tenant-a", "viewer", nil, time.Hour)       /* 检查错误并决定后续处理。 */
	if err != nil {                                                                   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	operatorToken, err := api.auth.Issue("operator", "tenant-a", "operator", nil, time.Hour) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                          /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	adminToken, err := api.auth.Issue("admin", "tenant-a", "admin", nil, time.Hour) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	workflows := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/workflows", token, nil, http.StatusOK) /* 更新 workflows 的值。 */
	if workflows["configured"] != true || workflows["healthy"] != true || workflows["count"].(float64) != 1 {                  /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected workflows response: %#v", workflows) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	manifest := map[string]any{"schemaVersion": 1, "id": "custom-status", "name": "Custom Status", "description": "Status statistics", "version": "1.0.0", "enabled": true, "persona": "Always query the system overview before answering status questions.", "defaultModel": "deepseek-v4-flash", "maxTokens": 2048, "capabilities": []string{"status"}, "allowedTools": []string{"mcp__iot__query_system_overview"}} /* 更新 manifest 的值。 */
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/workflows", operatorToken, manifest, http.StatusForbidden)                                                                                                                                                                                                                                                                                 /* 执行当前语句并推进处理流程。 */
	unsafe := map[string]any{}                                                                                                                                                                                                                                                                                                                                                                                         /* 更新 unsafe 的值。 */
	for key, value := range manifest {                                                                                                                                                                                                                                                                                                                                                                                 /* 循环处理当前数据。 */
		unsafe[key] = value /* 更新 unsafe[key] 的值。 */
	} /* 结束当前表达式或代码块。 */
	unsafe["id"] = "unsafe-agent"                                                                                                                 /* 执行当前语句并推进处理流程。 */
	unsafe["allowedTools"] = []string{"mcp__iot__control_device"}                                                                                 /* 执行当前语句并推进处理流程。 */
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/workflows", adminToken, unsafe, http.StatusUnprocessableEntity)       /* 执行当前语句并推进处理流程。 */
	createdAgent := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/workflows", adminToken, manifest, http.StatusCreated) /* 更新 createdAgent 的值。 */
	if createdAgent["id"] != "custom-status" || createdAgent["name"] != "Custom Status" {                                                         /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected dynamic Agent: %#v", createdAgent) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	managed := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/workflows/admin", adminToken, nil, http.StatusOK) /* 更新 managed 的值。 */
	if managed["count"] != float64(1) || managed["items"].([]any)[0].(map[string]any)["persona"] != manifest["persona"] {               /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected managed Agent catalog: %#v", managed) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/workflows/admin", token, nil, http.StatusForbidden) /* 执行当前语句并推进处理流程。 */
	updatedManifest := map[string]any{}                                                                                        /* 更新 updatedManifest 的值。 */
	for key, value := range manifest {                                                                                         /* 循环处理当前数据。 */
		updatedManifest[key] = value /* 更新 updatedManifest[key] 的值。 */
	} /* 结束当前表达式或代码块。 */
	updatedManifest["enabled"] = false                                                                                                                      /* 执行当前语句并推进处理流程。 */
	updated := requestJSON(t, server.Client(), http.MethodPut, server.URL+"/api/v1/ai/workflows/custom-status", adminToken, updatedManifest, http.StatusOK) /* 更新 updated 的值。 */
	if updated["enabled"] != false {                                                                                                                        /* 判断条件并选择处理分支。 */
		t.Fatalf("workflow update did not persist enabled=false: %#v", updated) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), http.MethodDelete, server.URL+"/api/v1/ai/workflows/custom-status", adminToken, nil, http.StatusOK)                             /* 执行当前语句并推进处理流程。 */
	requestJSON(t, server.Client(), http.MethodDelete, server.URL+"/api/v1/ai/workflows/ops-assistant", adminToken, nil, http.StatusConflict)                       /* 执行当前语句并推进处理流程。 */
	defaultBinding := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/workflows/ops-assistant/knowledge-binding", token, nil, http.StatusOK) /* 更新 defaultBinding 的值。 */
	if defaultBinding["retrievalMode"] != "auto" || defaultBinding["topK"] != float64(5) {                                                                          /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected default knowledge binding: %#v", defaultBinding) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), http.MethodPut, server.URL+"/api/v1/ai/workflows/ops-assistant/knowledge-binding", token, map[string]any{"retrievalMode": "disabled", "topK": 5, "minScore": .2, "noMatchPolicy": "allow-model"}, http.StatusForbidden)              /* 执行当前语句并推进处理流程。 */
	savedBinding := requestJSON(t, server.Client(), http.MethodPut, server.URL+"/api/v1/ai/workflows/ops-assistant/knowledge-binding", operatorToken, map[string]any{"retrievalMode": "auto", "topK": 3, "minScore": .4, "noMatchPolicy": "allow-model"}, http.StatusOK) /* 更新 savedBinding 的值。 */
	if savedBinding["workflowId"] != "ops-assistant" || savedBinding["topK"] != float64(3) {                                                                                                                                                                             /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected saved knowledge binding: %#v", savedBinding) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	chat := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/chat", token, map[string]any{"question": "status?", "workflowId": "ops-assistant", "conversationId": "browser-controlled", "model": "deepseek-chat", "maxTokens": 99999}, http.StatusOK) /* 更新 chat 的值。 */
	if chat["answer"] != "safe answer" || !strings.HasPrefix(chat["runId"].(string), "ai_run_") {                                                                                                                                                                            /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected rich chat response: %#v", chat) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, leaked := chat["mcpToken"]; leaked { /* 判断条件并选择处理分支。 */
		t.Fatalf("MCP credential leaked to browser: %#v", chat) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	runtime.mu.Lock()                                /* 执行当前语句并推进处理流程。 */
	captured := runtime.requests[0]                  /* 更新 captured 的值。 */
	runtime.mu.Unlock()                              /* 执行当前语句并推进处理流程。 */
	claims, err := api.auth.Parse(captured.MCPToken) /* 更新 err 的值。 */
	if err != nil {                                  /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if claims.TokenUse != "harness" || claims.RunID != captured.RunID || !claims.HasAudience(auth.HarnessAudience) || len(claims.Scopes) != len(auth.HarnessReadScopes()) { /* 判断条件并选择处理分支。 */
		t.Fatalf("unsafe harness token: %#v", claims) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if claims.Knowledge == nil || claims.Knowledge.WorkflowID != "ops-assistant" || claims.Knowledge.TopK != 3 || claims.Knowledge.MinScore != .4 { /* 判断条件并选择处理分支。 */
		t.Fatalf("knowledge binding was not enforced in harness token: %#v", claims.Knowledge) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if !strings.Contains(captured.Question, "平台知识策略") || !strings.Contains(captured.Question, "ops-assistant") { /* 判断条件并选择处理分支。 */
		t.Fatalf("knowledge policy was not supplied to harness: %q", captured.Question) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if captured.ConversationID == "browser-controlled" || captured.ConversationID != harnessConversationID("tenant-a", "alice", "browser-controlled") { /* 判断条件并选择处理分支。 */
		t.Fatalf("conversation ID was not tenant scoped: %q", captured.ConversationID) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if captured.MaxTokens != 8192 { /* 判断条件并选择处理分支。 */
		t.Fatalf("maxTokens was not clamped: %d", captured.MaxTokens) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if captured.Model != "deepseek-chat" { /* 判断条件并选择处理分支。 */
		t.Fatalf("model was not passed to harness: %q", captured.Model) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	engine.KB = knowledge.NewLocal()                                                                                                                                                                                                                                                                                                              /* 更新 engine.KB 的值。 */
	if err = engine.KB.(ports.FilteredKnowledgeBase).IndexKnowledge(context.Background(), ports.KnowledgeIndexInput{TenantID: "tenant-a", WorkflowID: "ops-assistant", ProductID: "fire-smoke", Category: "alarm-sop", Tags: []string{"certified"}, DocumentID: "doc-1", ChunkID: "chunk-1", Content: []byte("烟雾 告警 处置 需要 现场 复核")}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), http.MethodPut, server.URL+"/api/v1/ai/workflows/ops-assistant/knowledge-binding", operatorToken, map[string]any{"retrievalMode": "always", "topK": 3, "minScore": .5, "noMatchPolicy": "require-evidence"}, http.StatusOK) /* 执行当前语句并推进处理流程。 */
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/chat", token, map[string]any{"question": "烟雾 告警", "workflowId": "ops-assistant"}, http.StatusOK)                                                                                    /* 执行当前语句并推进处理流程。 */
	runtime.mu.Lock()                                                                                                                                                                                                                                           /* 执行当前语句并推进处理流程。 */
	forced := runtime.requests[len(runtime.requests)-1]                                                                                                                                                                                                         /* 更新 forced 的值。 */
	runtime.mu.Unlock()                                                                                                                                                                                                                                         /* 执行当前语句并推进处理流程。 */
	if !strings.Contains(forced.Question, "平台强制召回的知识证据") || !strings.Contains(forced.Question, "现场 复核") {                                                                                                                                                       /* 判断条件并选择处理分支。 */
		t.Fatalf("forced knowledge evidence was not supplied to Harness: %q", forced.Question) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	streamReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/ai/chat/stream", bytes.NewBufferString(`{"question":"stream?","conversationId":"browser-controlled"}`)) /* 更新 _ 的值。 */
	streamReq.Header.Set("Authorization", "Bearer "+token)                                                                                                                       /* 执行当前语句并推进处理流程。 */
	streamReq.Header.Set("Content-Type", "application/json")                                                                                                                     /* 执行当前语句并推进处理流程。 */
	streamResp, err := server.Client().Do(streamReq)                                                                                                                             /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                              /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	streamBody, _ := io.ReadAll(streamResp.Body)                                                                                                                                                                                              /* 更新 _ 的值。 */
	streamResp.Body.Close()                                                                                                                                                                                                                   /* 执行当前语句并推进处理流程。 */
	if streamResp.StatusCode != http.StatusOK || !strings.Contains(string(streamBody), "event: run.started") || !strings.Contains(string(streamBody), "event: text.delta") || !strings.Contains(string(streamBody), "event: run.completed") { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected SSE response: status=%d body=%s", streamResp.StatusCode, streamBody) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if strings.Contains(string(streamBody), "conv_") || strings.Contains(string(streamBody), "internal-session") || strings.Contains(string(streamBody), "internal-api-key") || !strings.Contains(string(streamBody), `"visible":"ok"`) || !strings.Contains(string(streamBody), `"safe":"nested-ok"`) { /* 判断条件并选择处理分支。 */
		t.Fatalf("SSE leaked internal conversation/session data: %s", streamBody) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	runtime.fail = true                                                                                                                                                       /* 更新 runtime.fail 的值。 */
	failedSync := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/chat", token, map[string]any{"question": "fail?"}, http.StatusBadGateway)           /* 更新 failedSync 的值。 */
	if errorMessage, _ := failedSync["detail"].(string); errorMessage != "AI workflow request failed" || strings.Contains(errorMessage, "sensitive internal runtime error") { /* 判断条件并选择处理分支。 */
		t.Fatalf("sync workflow leaked an internal error: %#v", failedSync) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	failedReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/ai/chat/stream", bytes.NewBufferString(`{"question":"fail?"}`)) /* 更新 _ 的值。 */
	failedReq.Header.Set("Authorization", "Bearer "+token)                                                                               /* 执行当前语句并推进处理流程。 */
	failedReq.Header.Set("Content-Type", "application/json")                                                                             /* 执行当前语句并推进处理流程。 */
	failedResp, err := server.Client().Do(failedReq)                                                                                     /* 更新 err 的值。 */
	if err != nil {                                                                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	failedBody, _ := io.ReadAll(failedResp.Body)                                                                                                                 /* 更新 _ 的值。 */
	failedResp.Body.Close()                                                                                                                                      /* 执行当前语句并推进处理流程。 */
	if count := strings.Count(string(failedBody), "event: run.failed"); count != 1 || strings.Contains(string(failedBody), "sensitive internal runtime error") { /* 判断条件并选择处理分支。 */
		t.Fatalf("unsafe or duplicate terminal event: %s", failedBody) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/mcp/harness", token, map[string]any{}, http.StatusForbidden) /* 执行当前语句并推进处理流程。 */
	getMCP, _ := http.NewRequest(http.MethodGet, server.URL+"/mcp/harness", nil)                                               /* 更新 _ 的值。 */
	getMCP.Header.Set("Authorization", "Bearer "+token)                                                                        /* 执行当前语句并推进处理流程。 */
	getMCPResp, err := server.Client().Do(getMCP)                                                                              /* 更新 err 的值。 */
	if err != nil {                                                                                                            /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	getMCPResp.Body.Close()                                   /* 执行当前语句并推进处理流程。 */
	if getMCPResp.StatusCode != http.StatusMethodNotAllowed { /* 判断条件并选择处理分支。 */
		t.Fatalf("GET /mcp/harness status=%d", getMCPResp.StatusCode) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	harnessToken, err := api.auth.IssueHarness("alice", "tenant-a", "run-x", auth.HarnessReadScopes(), 2*time.Minute) /* 更新 err 的值。 */
	if err != nil {                                                                                                   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/workflows", harnessToken, nil, http.StatusForbidden) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func TestHarnessConversationIDIsStableAndTenantScoped(t *testing.T) { /* 定义 TestHarnessConversationIDIsStableAndTenantScoped 函数。 */
	a := harnessConversationID("tenant-a", "alice", "conversation-1")      /* 更新 a 的值。 */
	if a != harnessConversationID("tenant-a", "alice", "conversation-1") { /* 判断条件并选择处理分支。 */
		t.Fatal("conversation derivation is not stable") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if a == harnessConversationID("tenant-b", "alice", "conversation-1") || a == harnessConversationID("tenant-a", "bob", "conversation-1") { /* 判断条件并选择处理分支。 */
		t.Fatal("conversation derivation is not tenant/user scoped") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if !strings.HasPrefix(a, "conv_") || strings.Contains(a, "tenant-a") || strings.Contains(a, "alice") { /* 判断条件并选择处理分支。 */
		t.Fatalf("conversation derivation leaks identity: %q", a) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
