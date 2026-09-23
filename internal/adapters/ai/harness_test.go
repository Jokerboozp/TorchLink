package aiadapter /* 声明 aiadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"encoding/json"     /* 执行当前语句并推进处理流程。 */
	"fmt"               /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"strings"           /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
	"time"              /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestHarnessClientSeparatesCredentialsAndParsesNDJSON(t *testing.T) { /* 定义 TestHarnessClientSeparatesCredentialsAndParsesNDJSON 函数。 */
	const serviceToken = "0123456789abcdef0123456789abcdef"                                      /* 声明 serviceToken。 */
	var streamBody map[string]any                                                                /* 声明 streamBody。 */
	var savedManifest ports.AIWorkflowManifest                                                   /* 声明 savedManifest。 */
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 server 的值。 */
		if r.Header.Get("X-IOT-Harness-Token") != serviceToken { /* 判断条件并选择处理分支。 */
			t.Errorf("missing service credential: %q", r.Header.Get("X-IOT-Harness-Token")) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		switch r.URL.Path { /* 根据条件选择处理路径。 */
		case "/v1/plugins": /* 处理当前分支。 */
			if r.Header.Get("Authorization") != "" { /* 判断条件并选择处理分支。 */
				t.Errorf("plugins request unexpectedly contains MCP credential") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if r.Method == http.MethodPost { /* 判断条件并选择处理分支。 */
				if err := json.NewDecoder(r.Body).Decode(&savedManifest); err != nil { /* 判断条件并选择处理分支。 */
					t.Fatal(err) /* 验证实际结果符合预期。 */
				} /* 结束当前表达式或代码块。 */
				w.WriteHeader(http.StatusCreated)                                                                                                   /* 执行当前语句并推进处理流程。 */
				_ = json.NewEncoder(w).Encode(map[string]any{"id": savedManifest.ID, "name": savedManifest.Name, "enabled": savedManifest.Enabled}) /* 更新 _ 的值。 */
				return                                                                                                                              /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "ops-assistant", "name": "Ops", "enabled": true, "defaultModel": "deepseek-v4-flash", "maxTokens": 16384}}}) /* 更新 _ 的值。 */
		case "/v1/plugins/admin": /* 处理当前分支。 */
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []ports.AIWorkflowManifest{{SchemaVersion: 1, ID: "dynamic", Name: "Dynamic", Description: "Dynamic Agent", Version: "1.0.0", Enabled: false, Persona: "Read-only status agent", DefaultModel: "deepseek-chat", MaxTokens: 2048, Capabilities: []string{"status"}, AllowedTools: []string{"mcp__iot__query_system_overview"}}}}) /* 更新 _ 的值。 */
		case "/v1/plugins/dynamic": /* 处理当前分支。 */
			if r.Method != http.MethodDelete { /* 判断条件并选择处理分支。 */
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed) /* 执行当前语句并推进处理流程。 */
				return                                                           /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			w.WriteHeader(http.StatusNoContent) /* 执行当前语句并推进处理流程。 */
		case "/v1/chat/stream": /* 处理当前分支。 */
			if r.Header.Get("Authorization") != "Bearer short-mcp-jwt" { /* 判断条件并选择处理分支。 */
				t.Errorf("MCP JWT sent in wrong header: %q", r.Header.Get("Authorization")) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if err := json.NewDecoder(r.Body).Decode(&streamBody); err != nil { /* 判断条件并选择处理分支。 */
				t.Fatal(err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			w.Header().Set("Content-Type", "application/x-ndjson")                                                         /* 执行当前语句并推进处理流程。 */
			_, _ = fmt.Fprintln(w, `{"type":"run.started"}`)                                                               /* 更新 _ 的值。 */
			_, _ = fmt.Fprintln(w, `{"type":"text.delta","delta":"hello "}`)                                               /* 更新 _ 的值。 */
			_, _ = fmt.Fprintln(w, `{"type":"tool.completed","callId":"call-1","tool":"query_alarm_list","success":true}`) /* 更新 _ 的值。 */
			_, _ = fmt.Fprintln(w, `{"type":"text.delta","delta":"world"}`)                                                /* 更新 _ 的值。 */
			_, _ = fmt.Fprintln(w, `{"type":"run.completed","conversationId":"internal-conversation"}`)                    /* 更新 _ 的值。 */
		default: /* 处理当前分支。 */
			http.NotFound(w, r) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	})) /* 结束当前表达式或代码块。 */
	defer server.Close() /* 安排函数结束时执行清理。 */

	client, err := NewHarness(server.URL, serviceToken, "https://api.example/mcp/harness", "deepseek-chat", time.Second) /* 更新 err 的值。 */
	if err != nil {                                                                                                      /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	plugins, err := client.ListWorkflows(context.Background())                                                                                                  /* 更新 err 的值。 */
	if err != nil || len(plugins) != 1 || plugins[0].ID != "ops-assistant" || plugins[0].DefaultModel != "deepseek-v4-flash" || plugins[0].MaxTokens != 16384 { /* 判断条件并选择处理分支。 */
		t.Fatalf("plugins=%#v err=%v", plugins, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	created, err := client.SaveWorkflow(context.Background(), ports.AIWorkflowManifest{SchemaVersion: 1, ID: "dynamic", Name: "Dynamic", Description: "Dynamic Agent", Version: "1.0.0", Enabled: true, Persona: "Read-only status agent", DefaultModel: "deepseek-chat", MaxTokens: 2048, Capabilities: []string{"status"}, AllowedTools: []string{"mcp__iot__query_system_overview"}}) /* 更新 err 的值。 */
	if err != nil || created.ID != "dynamic" || savedManifest.ID != "dynamic" {                                                                                                                                                                                                                                                                                                          /* 判断条件并选择处理分支。 */
		t.Fatalf("created=%#v saved=%#v err=%v", created, savedManifest, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	manifests, err := client.ListWorkflowManifests(context.Background())                                                         /* 更新 err 的值。 */
	if err != nil || len(manifests) != 1 || manifests[0].ID != "dynamic" || manifests[0].Persona == "" || manifests[0].Enabled { /* 判断条件并选择处理分支。 */
		t.Fatalf("manifests=%#v err=%v", manifests, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err = client.DeleteWorkflow(context.Background(), "dynamic"); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatalf("delete workflow: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	var types []string                                                                                                                                                                                                                               /* 声明 types。 */
	result, err := client.StreamChat(context.Background(), ports.AIWorkflowRequest{RunID: "run-1", ConversationID: "conv-1", WorkflowID: "ops-assistant", Question: "status?", MCPToken: "short-mcp-jwt"}, func(event ports.AIWorkflowEvent) error { /* 更新 err 的值。 */
		types = append(types, event.Type) /* 更新 types 的值。 */
		return nil                        /* 返回当前处理结果。 */
	}) /* 结束当前表达式或代码块。 */
	if err != nil || result.Answer != "hello world" || strings.Join(types, ",") != "run.started,text.delta,tool.completed,text.delta,run.completed" { /* 判断条件并选择处理分支。 */
		t.Fatalf("result=%#v events=%v err=%v", result, types, err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, leaked := streamBody["mcpToken"]; leaked { /* 判断条件并选择处理分支。 */
		t.Fatalf("short-lived MCP token leaked into JSON body: %#v", streamBody) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, field := range []string{"runId", "conversationId", "workflowId", "question", "mcpUrl", "model", "maxTokens"} { /* 循环处理当前数据。 */
		if _, ok := streamBody[field]; !ok { /* 判断条件并选择处理分支。 */
			t.Fatalf("missing sidecar field %q in %#v", field, streamBody) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestHarnessClientRejectsUnknownEvent(t *testing.T) { /* 定义 TestHarnessClientRejectsUnknownEvent 函数。 */
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { /* 更新 server 的值。 */
		_, _ = fmt.Fprintln(w, `{"type":"debug.secret","text":"nope"}`) /* 更新 _ 的值。 */
	})) /* 结束当前表达式或代码块。 */
	defer server.Close()                                                                                                          /* 安排函数结束时执行清理。 */
	client, err := NewHarness(server.URL, "0123456789abcdef0123456789abcdef", "https://api.example/mcp/harness", "", time.Second) /* 更新 err 的值。 */
	if err != nil {                                                                                                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	_, err = client.StreamChat(context.Background(), ports.AIWorkflowRequest{RunID: "run-1", Question: "q", MCPToken: "jwt"}, nil) /* 检查错误并决定后续处理。 */
	if err == nil || !strings.Contains(err.Error(), "unsupported harness event type") {                                            /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected error: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestHarnessClientRejectsWeakServiceToken(t *testing.T) { /* 定义 TestHarnessClientRejectsWeakServiceToken 函数。 */
	if _, err := NewHarness("https://harness.example", "too-short", "https://api.example/mcp/harness", "", time.Second); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("weak harness service token was accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestHarnessClientConfiguresSelectedProvider(t *testing.T) { /* 定义 TestHarnessClientConfiguresSelectedProvider 函数。 */
	const serviceToken = "0123456789abcdef0123456789abcdef"                                      /* 声明 serviceToken。 */
	var payload map[string]string                                                                /* 声明 payload。 */
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 server 的值。 */
		if r.Method != http.MethodPut || r.URL.Path != "/v1/provider" { /* 判断条件并选择处理分支。 */
			http.NotFound(w, r) /* 执行当前语句并推进处理流程。 */
			return              /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if r.Header.Get("X-IOT-Harness-Token") != serviceToken { /* 判断条件并选择处理分支。 */
			t.Errorf("missing service credential: %q", r.Header.Get("X-IOT-Harness-Token")) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil { /* 判断条件并选择处理分支。 */
			t.Fatal(err) /* 验证实际结果符合预期。 */
		} /* 结束当前表达式或代码块。 */
		_ = json.NewEncoder(w).Encode(map[string]any{"provider": payload["provider"], "baseUrl": payload["baseUrl"], "model": payload["model"]}) /* 更新 _ 的值。 */
	})) /* 结束当前表达式或代码块。 */
	defer server.Close()                                                                                              /* 安排函数结束时执行清理。 */
	client, err := NewHarness(server.URL, serviceToken, "https://api.example/mcp/harness", "qwen3:1.7b", time.Second) /* 更新 err 的值。 */
	if err != nil {                                                                                                   /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	selected := ports.AIPluginConfig{Provider: "ollama", BaseURL: "http://192.168.24.133:11434", Model: "qwen3:1.7b"} /* 更新 selected 的值。 */
	if err := client.ConfigureProvider(context.Background(), selected); err != nil {                                  /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if payload["provider"] != "ollama" || payload["baseUrl"] != "http://192.168.24.133:11434/v1" || payload["model"] != selected.Model { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected sidecar provider payload: %#v", payload) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if got := client.CurrentConfig(); got != selected { /* 判断条件并选择处理分支。 */
		t.Fatalf("selected provider was not retained: %#v", got) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	withV1 := ports.AIPluginConfig{Provider: "ollama", BaseURL: "http://192.168.24.133:11434/v1", Model: "qwen3:1.7b"} /* 更新 withV1 的值。 */
	if err := client.ConfigureProvider(context.Background(), withV1); err != nil {                                     /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if payload["baseUrl"] != "http://192.168.24.133:11434/v1" || client.CurrentConfig().BaseURL != "http://192.168.24.133:11434" { /* 判断条件并选择处理分支。 */
		t.Fatalf("Ollama /v1 suffix was not normalized: payload=%#v config=%#v", payload, client.CurrentConfig()) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if err := client.ConfigureProvider(context.Background(), ports.AIPluginConfig{Provider: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-chat", APIKey: "secret"}); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if payload["provider"] != "deepseek-official" || payload["baseUrl"] != "https://api.deepseek.com" || payload["apiKey"] != "secret" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected DeepSeek sidecar provider payload: %#v", payload) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
