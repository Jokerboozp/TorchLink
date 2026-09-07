package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	aiadapter "iot-platform/internal/adapters/ai"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/config"
	"iot-platform/internal/core"
	"iot-platform/internal/metrics"
	"iot-platform/internal/model"
	"iot-platform/internal/parser"
	"iot-platform/internal/ports"
)

type providerConfigTestRuntime struct {
	mu      sync.Mutex
	config  ports.AIPluginConfig
	updates []ports.AIPluginConfig
}

func (r *providerConfigTestRuntime) AnalyzeAlarm(context.Context, model.Alarm, []map[string]any, []string) (model.AIAnalysis, error) {
	return model.AIAnalysis{}, nil
}
func (r *providerConfigTestRuntime) Chat(context.Context, string, string) (string, error) {
	return "", nil
}
func (r *providerConfigTestRuntime) RuleDraft(context.Context, string, string) (model.AlarmRule, error) {
	return model.AlarmRule{}, nil
}
func (r *providerConfigTestRuntime) Health(context.Context) error { return nil }
func (r *providerConfigTestRuntime) CurrentConfig() ports.AIPluginConfig {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.config
}
func (r *providerConfigTestRuntime) Configure(_ context.Context, config ports.AIPluginConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.config = config
	r.updates = append(r.updates, config)
	return nil
}
func (r *providerConfigTestRuntime) ProviderInfo() ports.AIPluginInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	name := r.config.Provider
	if name == "deepseek" {
		name = "DeepSeek"
	}
	return ports.AIPluginInfo{ID: r.config.Provider, Name: name, Model: r.config.Model, Enabled: true}
}

type providerConfigTestWorkflow struct {
	mu      sync.Mutex
	updates []ports.AIPluginConfig
}

func (w *providerConfigTestWorkflow) ConfigureProvider(_ context.Context, config ports.AIPluginConfig) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.updates = append(w.updates, config)
	return nil
}

func TestAIProviderConfigSwitchesRuntimeAndRedactsKey(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime := &providerConfigTestRuntime{config: ports.AIPluginConfig{Provider: "ollama", BaseURL: "http://localhost:11434", Model: "qwen3:1.7b"}}
	workflow := &providerConfigTestWorkflow{}
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AI = runtime
	engine.AIPlugins = aiadapter.NewProviderRegistry()
	api := New(config.Config{DevMode: true, AITestOllamaURL: "http://localhost:11434"}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetAIProviderRuntime(runtime)
	api.SetAIProviderStore(repo)
	api.SetAIWorkflowProvider(workflow)
	server := newTestHTTPServer(api)
	defer server.Close()

	adminToken, err := api.auth.Issue("admin", "tenant-a", "admin", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	viewerToken, err := api.auth.Issue("viewer", "tenant-a", "viewer", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	updated := requestJSON(t, server.Client(), "PUT", server.URL+"/api/v1/ai/providers/config", adminToken, map[string]any{
		"provider": "deepseek",
		"baseUrl":  "https://api.deepseek.com",
		"model":    "deepseek-chat",
		"apiKey":   "provider-secret",
	}, 200)
	if updated["provider"] != "deepseek" || updated["model"] != "deepseek-chat" || updated["apiKeyConfigured"] != true || updated["apiKeyHint"] != "prov***" {
		t.Fatalf("unexpected redacted provider response: %#v", updated)
	}
	if _, leaked := updated["apiKey"]; leaked {
		t.Fatalf("provider key leaked in response: %#v", updated)
	}
	if got := runtime.CurrentConfig(); got.Provider != "deepseek" || got.APIKey != "provider-secret" {
		t.Fatalf("runtime was not updated: %#v", got)
	}
	if got, found, loadErr := repo.LoadAIProviderConfig(context.Background()); loadErr != nil || !found || got != runtime.CurrentConfig() {
		t.Fatalf("provider config was not persisted: %#v found=%v err=%v", got, found, loadErr)
	}
	workflow.mu.Lock()
	if len(workflow.updates) != 1 || workflow.updates[0].Provider != "deepseek" {
		t.Fatalf("workflow provider was not updated: %#v", workflow.updates)
	}
	workflow.mu.Unlock()
	viewer := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/ai/providers/config", viewerToken, nil, 200)
	if viewer["provider"] != "deepseek" || viewer["apiKeyConfigured"] != true {
		t.Fatalf("unexpected viewer provider response: %#v", viewer)
	}
	if _, exposed := viewer["baseUrl"]; exposed {
		t.Fatalf("viewer response exposed provider address: %#v", viewer)
	}
	if _, exposed := viewer["apiKeyHint"]; exposed {
		t.Fatalf("viewer response exposed provider key hint: %#v", viewer)
	}
	requestJSON(t, server.Client(), "PUT", server.URL+"/api/v1/ai/providers/config", viewerToken, map[string]any{
		"provider": "ollama",
		"baseUrl":  "http://localhost:11434",
		"model":    "qwen3:1.7b",
	}, 403)
}

func TestAIProviderTestDoesNotApplyAndReusesActiveKey(t *testing.T) {
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer active-secret" {
			http.Error(w, "missing authorization", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"测试成功"}}]}`)
	}))
	defer providerServer.Close()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	active := ports.AIPluginConfig{Provider: "deepseek", BaseURL: providerServer.URL, Model: "active-model", APIKey: "active-secret"}
	runtime := &providerConfigTestRuntime{config: active}
	engine := core.New(repo, archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AI = runtime
	engine.AIPlugins = aiadapter.NewProviderRegistry()
	api := New(config.Config{DevMode: true, AITestOrigins: []string{providerServer.URL}}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetAIProviderRuntime(runtime)
	server := newTestHTTPServer(api)
	defer server.Close()
	token, err := api.auth.Issue("admin", "tenant-a", "admin", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	result := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/providers/test", token, map[string]any{
		"provider": "deepseek",
		"baseUrl":  providerServer.URL,
		"model":    "active-model",
		"question": "连接测试",
	}, http.StatusOK)
	if result["success"] != true || result["answer"] != "测试成功" {
		t.Fatalf("unexpected provider test response: %#v", result)
	}
	if got := runtime.CurrentConfig(); got != active {
		t.Fatalf("testing changed active provider: got %#v want %#v", got, active)
	}
}

func newTestHTTPServer(api *Server) *httptest.Server {
	return httptest.NewServer(api.Handler())
}

var _ ports.AIProviderRuntime = (*providerConfigTestRuntime)(nil)
var _ ports.AIWorkflowProviderRuntime = (*providerConfigTestWorkflow)(nil)
