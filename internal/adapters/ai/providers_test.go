package aiadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iot-platform/internal/model"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"iot-platform/internal/ports"
)

func TestOpenAICompatibleProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("unexpected authorization header %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/models":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		case "/chat/completions":
			var request openAIChatRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.Model != "test-model" || len(request.Messages) != 2 {
				t.Fatalf("unexpected request %#v", request)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "chat_test", "choices": []any{map[string]any{"message": map[string]any{"content": "插件连接成功"}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewOpenAICompatible("test", "Test Provider", server.URL, "test-model", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	answer, err := client.Chat(context.Background(), "tenant", "hello")
	if err != nil || answer != "插件连接成功" {
		t.Fatalf("answer=%q err=%v", answer, err)
	}
}

func TestOpenAICompatibleBoundsAndSanitizesProviderErrors(t *testing.T) {
	leaky := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "Authorization: Bearer super-secret-key")
	}))
	defer leaky.Close()
	client, err := NewOpenAICompatible("test", "Test", leaky.URL, "model", "super-secret-key")
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Chat(context.Background(), "tenant", "hello")
	if err == nil || strings.Contains(err.Error(), "super-secret-key") {
		t.Fatalf("provider error was not sanitized: %v", err)
	}

	oversized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"`)
		_, _ = io.WriteString(w, strings.Repeat("x", maxAIProviderResponseBytes+1))
		_, _ = io.WriteString(w, `"}}]}`)
	}))
	defer oversized.Close()
	client, err = NewOpenAICompatible("test", "Test", oversized.URL, "model", "key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Chat(context.Background(), "tenant", "hello"); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversized provider response was accepted: %v", err)
	}
}

func TestOpenAICompatibleRejectsUserinfoAndCrossOriginRedirect(t *testing.T) {
	if _, err := NewOpenAICompatible("test", "Test", "https://secret@example.com", "model", "key"); err == nil {
		t.Fatal("provider URL with userinfo was accepted")
	}
	if _, err := NewOpenAICompatible("test", "Test", "https://example.com?token=secret", "model", "key"); err == nil {
		t.Fatal("provider URL with query credentials was accepted")
	}

	var redirectedRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectedRequests.Add(1)
	}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	client, err := NewOpenAICompatible("test", "Test", redirector.URL, "model", "key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Chat(context.Background(), "tenant", "hello"); err == nil {
		t.Fatal("cross-origin redirect was followed")
	}
	if redirectedRequests.Load() != 0 {
		t.Fatalf("redirect target received %d requests", redirectedRequests.Load())
	}
}

func TestOpenAICompatibleLimitsSameOriginRedirects(t *testing.T) {
	var requests atomic.Int32
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Redirect(w, r, serverURL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	serverURL = server.URL
	defer server.Close()
	client, err := NewOpenAICompatible("test", "Test", server.URL, "model", "key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Chat(context.Background(), "tenant", "hello"); err == nil {
		t.Fatal("same-origin redirect loop was not rejected")
	}
	if got := requests.Load(); got > 10 {
		t.Fatalf("redirect loop made %d requests", got)
	}
}

func TestProviderRegistry(t *testing.T) {
	registry := NewProviderRegistry()
	items := registry.List()
	ids := []string{}
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	if strings.Join(ids, ",") != "deepseek,disabled,openai-compatible" {
		t.Fatalf("expected DeepSeek, disabled and OpenAI-compatible providers only, got %v", ids)
	}
	if _, err := registry.Create(ports.AIPluginConfig{Provider: "ollama", BaseURL: "http://localhost:11434", Model: "qwen3"}); err == nil {
		t.Fatal("removed Ollama provider must not be creatable")
	}
	if _, err := registry.Create(ports.AIPluginConfig{Provider: "openai-compatible", BaseURL: "https://api.example/v1", Model: "remote-model"}); err != nil {
		t.Fatalf("keyless external OpenAI-compatible provider rejected: %v", err)
	}
	for _, item := range items {
		if item.ID != "disabled" && !item.Enabled {
			t.Fatalf("provider %q is not selectable in the test sandbox: %#v", item.ID, item)
		}
	}
	if _, err := registry.Create(ports.AIPluginConfig{Provider: "deepseek"}); err == nil {
		t.Fatal("DeepSeek plugin accepted a missing API key")
	}
	client, err := registry.Create(ports.AIPluginConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if info := client.(ports.AIInspectable).ProviderInfo(); info.ID != "disabled" || info.Enabled {
		t.Fatalf("unexpected fallback plugin %#v", info)
	}
}

func TestRuntimeAllowsInitialDeepSeekWithoutKeyButRejectsActivation(t *testing.T) {
	registry := NewProviderRegistry()
	cfg := ports.AIPluginConfig{Provider: "deepseek", Model: "deepseek-flash"}
	runtime, err := NewRuntimeProvider(registry, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.ProviderInfo().Enabled || runtime.CurrentConfig().Provider != "deepseek" {
		t.Fatal("missing-key provider must remain selected but unavailable")
	}
	if !errors.Is(runtime.Health(context.Background()), errDeepSeekKeyRequired) {
		t.Fatal("health should explain that a DeepSeek key is required")
	}
	if _, err := runtime.Chat(context.Background(), "tenant", "hello"); !errors.Is(err, errDeepSeekKeyRequired) {
		t.Fatal("missing-key chat should fail without contacting a provider")
	}
	if err := runtime.Configure(context.Background(), cfg); err == nil {
		t.Fatal("activation without a key must still be rejected")
	}
	if runtime.ProviderInfo().Enabled {
		t.Fatal("failed activation changed the pending provider")
	}
}

func TestHarnessClientSeparatesCredentialsAndParsesNDJSON(t *testing.T) {
	const serviceToken = "0123456789abcdef0123456789abcdef"
	var streamBody map[string]any
	var savedManifest ports.AIWorkflowManifest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-IOT-Harness-Token") != serviceToken {
			t.Errorf("missing service credential: %q", r.Header.Get("X-IOT-Harness-Token"))
		}
		switch r.URL.Path {
		case "/v1/plugins":
			if r.Header.Get("Authorization") != "" {
				t.Errorf("plugins request unexpectedly contains MCP credential")
			}
			if r.Method == http.MethodPost {
				if err := json.NewDecoder(r.Body).Decode(&savedManifest); err != nil {
					t.Fatal(err)
				}
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(map[string]any{"id": savedManifest.ID, "name": savedManifest.Name, "enabled": savedManifest.Enabled})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{{"id": "ops-assistant", "name": "Ops", "enabled": true, "defaultModel": "deepseek-v4-flash", "maxTokens": 16384}}})
		case "/v1/plugins/admin":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []ports.AIWorkflowManifest{{SchemaVersion: 1, ID: "dynamic", Name: "Dynamic", Description: "Dynamic Agent", Version: "1.0.0", Enabled: false, Persona: "Read-only status agent", DefaultModel: "deepseek-chat", MaxTokens: 2048, Capabilities: []string{"status"}, AllowedTools: []string{"mcp__iot__query_system_overview"}}}})
		case "/v1/plugins/dynamic":
			if r.Method != http.MethodDelete {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case "/v1/chat/stream":
			if r.Header.Get("Authorization") != "Bearer short-mcp-jwt" {
				t.Errorf("MCP JWT sent in wrong header: %q", r.Header.Get("Authorization"))
			}
			if err := json.NewDecoder(r.Body).Decode(&streamBody); err != nil {
				t.Fatal(err)
			}
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = fmt.Fprintln(w, `{"type":"run.started","workflowVersion":"1.2.0"}`)
			_, _ = fmt.Fprintln(w, `{"type":"text.delta","delta":"hello "}`)
			_, _ = fmt.Fprintln(w, `{"type":"tool.completed","callId":"call-1","tool":"query_alarm_list","success":true}`)
			_, _ = fmt.Fprintln(w, `{"type":"text.delta","delta":"world"}`)
			_, _ = fmt.Fprintln(w, `{"type":"run.completed","conversationId":"internal-conversation","usage":{"inputTokens":120,"outputTokens":30,"cacheReadTokens":64,"reasoningTokens":5},"toolCalls":2}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := NewHarness(server.URL, serviceToken, "https://api.example/mcp/harness", "deepseek-chat", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	plugins, err := client.ListWorkflows(context.Background())
	if err != nil || len(plugins) != 1 || plugins[0].ID != "ops-assistant" || plugins[0].DefaultModel != "deepseek-v4-flash" || plugins[0].MaxTokens != 16384 {
		t.Fatalf("plugins=%#v err=%v", plugins, err)
	}
	created, err := client.SaveWorkflow(context.Background(), ports.AIWorkflowManifest{SchemaVersion: 1, ID: "dynamic", Name: "Dynamic", Description: "Dynamic Agent", Version: "1.0.0", Enabled: true, Persona: "Read-only status agent", DefaultModel: "deepseek-chat", MaxTokens: 2048, Capabilities: []string{"status"}, AllowedTools: []string{"mcp__iot__query_system_overview"}})
	if err != nil || created.ID != "dynamic" || savedManifest.ID != "dynamic" {
		t.Fatalf("created=%#v saved=%#v err=%v", created, savedManifest, err)
	}
	manifests, err := client.ListWorkflowManifests(context.Background())
	if err != nil || len(manifests) != 1 || manifests[0].ID != "dynamic" || manifests[0].Persona == "" || manifests[0].Enabled {
		t.Fatalf("manifests=%#v err=%v", manifests, err)
	}
	if err = client.DeleteWorkflow(context.Background(), "dynamic"); err != nil {
		t.Fatalf("delete workflow: %v", err)
	}
	var types []string
	result, err := client.StreamChat(context.Background(), ports.AIWorkflowRequest{RunID: "run-1", ConversationID: "conv-1", WorkflowID: "ops-assistant", Question: "status?", MCPToken: "short-mcp-jwt"}, func(event ports.AIWorkflowEvent) error {
		types = append(types, event.Type)
		return nil
	})
	if err != nil || result.Answer != "hello world" || strings.Join(types, ",") != "run.started,text.delta,tool.completed,text.delta,run.completed" {
		t.Fatalf("result=%#v events=%v err=%v", result, types, err)
	}
	if !result.UsageReported || result.Usage != (model.AIUsage{InputTokens: 120, OutputTokens: 30, CacheReadTokens: 64, ReasoningTokens: 5}) || result.ToolCalls != 2 || result.WorkflowVersion != "1.2.0" {
		t.Fatalf("usage was not taken from run.completed: %#v", result)
	}
	if _, leaked := streamBody["mcpToken"]; leaked {
		t.Fatalf("short-lived MCP token leaked into JSON body: %#v", streamBody)
	}
	for _, field := range []string{"runId", "conversationId", "workflowId", "question", "mcpUrl", "model", "maxTokens"} {
		if _, ok := streamBody[field]; !ok {
			t.Fatalf("missing sidecar field %q in %#v", field, streamBody)
		}
	}
}

func TestHarnessClientRejectsUnknownEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintln(w, `{"type":"debug.secret","text":"nope"}`)
	}))
	defer server.Close()
	client, err := NewHarness(server.URL, "0123456789abcdef0123456789abcdef", "https://api.example/mcp/harness", "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.StreamChat(context.Background(), ports.AIWorkflowRequest{RunID: "run-1", Question: "q", MCPToken: "jwt"}, nil)
	if err == nil || !strings.Contains(err.Error(), "unsupported harness event type") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHarnessClientRejectsWeakServiceToken(t *testing.T) {
	if _, err := NewHarness("https://harness.example", "too-short", "https://api.example/mcp/harness", "", time.Second); err == nil {
		t.Fatal("weak harness service token was accepted")
	}
}

func TestHarnessClientConfiguresSelectedProvider(t *testing.T) {
	const serviceToken = "0123456789abcdef0123456789abcdef"
	var payload map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/provider" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-IOT-Harness-Token") != serviceToken {
			t.Errorf("missing service credential: %q", r.Header.Get("X-IOT-Harness-Token"))
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"provider": payload["provider"], "baseUrl": payload["baseUrl"], "model": payload["model"]})
	}))
	defer server.Close()
	client, err := NewHarness(server.URL, serviceToken, "https://api.example/mcp/harness", "remote-model", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	selected := ports.AIPluginConfig{Provider: "openai-compatible", BaseURL: "https://api.example/v1", Model: "remote-model"}
	if err := client.ConfigureProvider(context.Background(), selected); err != nil {
		t.Fatalf("keyless external OpenAI-compatible endpoint must be accepted: %v", err)
	}
	if payload["provider"] != "openai-compatible" || payload["baseUrl"] != "https://api.example/v1" || payload["model"] != selected.Model || payload["apiKey"] != "" {
		t.Fatalf("unexpected sidecar provider payload: %#v", payload)
	}
	if got := client.CurrentConfig(); got != selected {
		t.Fatalf("selected provider was not retained: %#v", got)
	}
	if err := client.ConfigureProvider(context.Background(), ports.AIPluginConfig{Provider: "ollama", BaseURL: "http://localhost:11434", Model: "remote-model"}); err == nil {
		t.Fatal("removed Ollama provider must be rejected")
	}
	if err := client.ConfigureProvider(context.Background(), ports.AIPluginConfig{Provider: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-chat"}); err == nil {
		t.Fatal("DeepSeek without an API key must be rejected")
	}
	if err := client.ConfigureProvider(context.Background(), ports.AIPluginConfig{Provider: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-chat", APIKey: "secret"}); err != nil {
		t.Fatal(err)
	}
	if payload["provider"] != "deepseek-official" || payload["baseUrl"] != "https://api.deepseek.com" || payload["apiKey"] != "secret" {
		t.Fatalf("unexpected DeepSeek sidecar provider payload: %#v", payload)
	}
}

func TestHarnessClientReportsCapacityAsBusy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"code":"CAPACITY_EXCEEDED"}}`, http.StatusTooManyRequests)
	}))
	defer server.Close()
	client, err := NewHarness(server.URL, "0123456789abcdef0123456789abcdef", "https://api.example/mcp/harness", "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.StreamChat(context.Background(), ports.AIWorkflowRequest{RunID: "run-1", Question: "q", MCPToken: "token"}, nil)
	if !errors.Is(err, ports.ErrAIWorkflowBusy) {
		t.Fatalf("429 must be reported as busy, got %v", err)
	}
}

func TestHarnessRejectsOversizedBodyBeforeNetwork(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(413) }))
	defer server.Close()
	client, err := NewHarness(server.URL, "0123456789abcdef0123456789abcdef", "https://api.example/mcp/harness", "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.StreamChat(context.Background(), ports.AIWorkflowRequest{RunID: "r", Question: strings.Repeat("设", 11000), MCPToken: "token"}, nil)
	if err == nil || calls != 0 {
		t.Fatal("oversized body reached Harness", calls, err)
	}
}
