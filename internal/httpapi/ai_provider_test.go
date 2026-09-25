package httpapi /* 声明 httpapi 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context"           /* 执行当前语句并推进处理流程。 */
	"io"                /* 执行当前语句并推进处理流程。 */
	"log/slog"          /* 执行当前语句并推进处理流程。 */
	"net/http"          /* 执行当前语句并推进处理流程。 */
	"net/http/httptest" /* 执行当前语句并推进处理流程。 */
	"sync"              /* 执行当前语句并推进处理流程。 */
	"testing"           /* 执行当前语句并推进处理流程。 */
	"time"              /* 执行当前语句并推进处理流程。 */

	aiadapter "iot-platform/internal/adapters/ai" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/local"        /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/adapters/memory"       /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/config"                /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/core"                  /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/metrics"               /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"                 /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/parser"                /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"                 /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type providerConfigTestRuntime struct { /* 定义 providerConfigTestRuntime 类型。 */
	mu      sync.Mutex             /* 执行当前语句并推进处理流程。 */
	config  ports.AIPluginConfig   /* 执行当前语句并推进处理流程。 */
	updates []ports.AIPluginConfig /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (r *providerConfigTestRuntime) AnalyzeAlarm(context.Context, model.Alarm, []map[string]any, []string) (model.AIAnalysis, error) { /* 定义 AnalyzeAlarm 函数。 */
	return model.AIAnalysis{}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *providerConfigTestRuntime) Chat(context.Context, string, string) (string, error) { /* 定义 Chat 函数。 */
	return "", nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *providerConfigTestRuntime) RuleDraft(context.Context, string, string) (model.AlarmRule, error) { /* 定义 RuleDraft 函数。 */
	return model.AlarmRule{}, nil /* 返回当前处理结果。 */
}                                                                 /* 结束当前表达式或代码块。 */
func (r *providerConfigTestRuntime) Health(context.Context) error { return nil } /* 定义 Health 函数。 */
func (r *providerConfigTestRuntime) CurrentConfig() ports.AIPluginConfig { /* 定义 CurrentConfig 函数。 */
	r.mu.Lock()         /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock() /* 安排函数结束时执行清理。 */
	return r.config     /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *providerConfigTestRuntime) Configure(_ context.Context, config ports.AIPluginConfig) error { /* 定义 Configure 函数。 */
	r.mu.Lock()                           /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()                   /* 安排函数结束时执行清理。 */
	r.config = config                     /* 更新 r.config 的值。 */
	r.updates = append(r.updates, config) /* 更新 r.updates 的值。 */
	return nil                            /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (r *providerConfigTestRuntime) ProviderInfo() ports.AIPluginInfo { /* 定义 ProviderInfo 函数。 */
	r.mu.Lock()               /* 执行当前语句并推进处理流程。 */
	defer r.mu.Unlock()       /* 安排函数结束时执行清理。 */
	name := r.config.Provider /* 更新 name 的值。 */
	if name == "deepseek" {   /* 判断条件并选择处理分支。 */
		name = "DeepSeek" /* 更新 name 的值。 */
	} /* 结束当前表达式或代码块。 */
	return ports.AIPluginInfo{ID: r.config.Provider, Name: name, Model: r.config.Model, Enabled: true} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type providerConfigTestWorkflow struct { /* 定义 providerConfigTestWorkflow 类型。 */
	mu      sync.Mutex             /* 执行当前语句并推进处理流程。 */
	updates []ports.AIPluginConfig /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func (w *providerConfigTestWorkflow) ConfigureProvider(_ context.Context, config ports.AIPluginConfig) error { /* 定义 ConfigureProvider 函数。 */
	w.mu.Lock()                           /* 执行当前语句并推进处理流程。 */
	defer w.mu.Unlock()                   /* 安排函数结束时执行清理。 */
	w.updates = append(w.updates, config) /* 更新 w.updates 的值。 */
	return nil                            /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func TestAIProviderConfigSwitchesRuntimeAndRedactsKey(t *testing.T) { /* 定义 TestAIProviderConfigSwitchesRuntimeAndRedactsKey 函数。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	runtime := &providerConfigTestRuntime{config: ports.AIPluginConfig{Provider: "ollama", BaseURL: "http://localhost:11434", Model: "qwen3:1.7b"}}                                   /* 更新 runtime 的值。 */
	workflow := &providerConfigTestWorkflow{}                                                                                                                                         /* 更新 workflow 的值。 */
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 engine 的值。 */
	engine.AI = runtime                                                                                                                                                               /* 更新 engine.AI 的值。 */
	engine.AIPlugins = aiadapter.NewProviderRegistry()                                                                                                                                /* 更新 engine.AIPlugins 的值。 */
	api := New(config.Config{DevMode: true, AITestOllamaURL: "http://localhost:11434"}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))                        /* 更新 api 的值。 */
	api.SetAIProviderRuntime(runtime)                                                                                                                                                 /* 执行当前语句并推进处理流程。 */
	api.SetAIProviderStore(repo)                                                                                                                                                      /* 执行当前语句并推进处理流程。 */
	api.SetAIWorkflowProvider(workflow)                                                                                                                                               /* 执行当前语句并推进处理流程。 */
	server := newTestHTTPServer(api)                                                                                                                                                  /* 更新 server 的值。 */
	defer server.Close()                                                                                                                                                              /* 安排函数结束时执行清理。 */

	adminToken, err := api.auth.Issue("admin", "tenant-a", "admin", nil, time.Hour) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	viewerToken, err := api.auth.Issue("viewer", "tenant-a", "viewer", nil, time.Hour) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                    /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	updated := requestJSON(t, server.Client(), "PUT", server.URL+"/api/v1/ai/providers/config", adminToken, map[string]any{ /* 更新 updated 的值。 */
		"provider":  "deepseek",                 /* 执行当前语句并推进处理流程。 */
		"baseUrl":   "https://api.deepseek.com", /* 执行当前语句并推进处理流程。 */
		"model":     "deepseek-chat",            /* 执行当前语句并推进处理流程。 */
		"apiKey":    "provider-secret",          /* 执行当前语句并推进处理流程。 */
		"maxTokens": 3072,
	}, 200) /* 结束当前表达式或代码块。 */
	if updated["provider"] != "deepseek" || updated["model"] != "deepseek-chat" || updated["maxTokens"] != float64(3072) || updated["apiKeyConfigured"] != true || updated["apiKeyHint"] != "prov***" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected redacted provider response: %#v", updated) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, leaked := updated["apiKey"]; leaked { /* 判断条件并选择处理分支。 */
		t.Fatalf("provider key leaked in response: %#v", updated) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if got := runtime.CurrentConfig(); got.Provider != "deepseek" || got.APIKey != "provider-secret" || got.MaxTokens != 3072 { /* 判断条件并选择处理分支。 */
		t.Fatalf("runtime was not updated: %#v", got) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if got, found, loadErr := repo.LoadAIProviderConfig(context.Background()); loadErr != nil || !found || got != runtime.CurrentConfig() { /* 判断条件并选择处理分支。 */
		t.Fatalf("provider config was not persisted: %#v found=%v err=%v", got, found, loadErr) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	workflow.mu.Lock()                                                            /* 执行当前语句并推进处理流程。 */
	if len(workflow.updates) != 1 || workflow.updates[0].Provider != "deepseek" { /* 判断条件并选择处理分支。 */
		t.Fatalf("workflow provider was not updated: %#v", workflow.updates) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	workflow.mu.Unlock()                                                                                                /* 执行当前语句并推进处理流程。 */
	viewer := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/ai/providers/config", viewerToken, nil, 200)   /* 更新 viewer 的值。 */
	if viewer["provider"] != "deepseek" || viewer["maxTokens"] != float64(3072) || viewer["apiKeyConfigured"] != true { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected viewer provider response: %#v", viewer) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, exposed := viewer["baseUrl"]; exposed { /* 判断条件并选择处理分支。 */
		t.Fatalf("viewer response exposed provider address: %#v", viewer) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if _, exposed := viewer["apiKeyHint"]; exposed { /* 判断条件并选择处理分支。 */
		t.Fatalf("viewer response exposed provider key hint: %#v", viewer) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), "PUT", server.URL+"/api/v1/ai/providers/config", viewerToken, map[string]any{ /* 执行当前语句并推进处理流程。 */
		"provider": "ollama",                 /* 执行当前语句并推进处理流程。 */
		"baseUrl":  "http://localhost:11434", /* 执行当前语句并推进处理流程。 */
		"model":    "qwen3:1.7b",             /* 执行当前语句并推进处理流程。 */
	}, 403) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestAIProviderTestDoesNotApplyAndReusesActiveKey(t *testing.T) { /* 定义 TestAIProviderTestDoesNotApplyAndReusesActiveKey 函数。 */
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { /* 更新 providerServer 的值。 */
		if r.URL.Path != "/chat/completions" { /* 判断条件并选择处理分支。 */
			http.NotFound(w, r) /* 执行当前语句并推进处理流程。 */
			return              /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if r.Header.Get("Authorization") != "Bearer active-secret" { /* 判断条件并选择处理分支。 */
			http.Error(w, "missing authorization", http.StatusUnauthorized) /* 执行当前语句并推进处理流程。 */
			return                                                          /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		w.Header().Set("Content-Type", "application/json")                       /* 执行当前语句并推进处理流程。 */
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"测试成功"}}]}`) /* 更新 _ 的值。 */
	})) /* 结束当前表达式或代码块。 */
	defer providerServer.Close()                  /* 安排函数结束时执行清理。 */
	repo := memory.NewRepository()                /* 更新 repo 的值。 */
	archive, err := local.NewArchive(t.TempDir()) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	active := ports.AIPluginConfig{Provider: "deepseek", BaseURL: providerServer.URL, Model: "active-model", APIKey: "active-secret"}                                                 /* 更新 active 的值。 */
	runtime := &providerConfigTestRuntime{config: active}                                                                                                                             /* 更新 runtime 的值。 */
	engine := core.New(ScopedRepository(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 engine 的值。 */
	engine.AI = runtime                                                                                                                                                               /* 更新 engine.AI 的值。 */
	engine.AIPlugins = aiadapter.NewProviderRegistry()                                                                                                                                /* 更新 engine.AIPlugins 的值。 */
	// A newly supplied endpoint must work without an address allowlist.
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil))) /* 更新 api 的值。 */
	api.SetAIProviderRuntime(runtime)                                                                               /* 执行当前语句并推进处理流程。 */
	server := newTestHTTPServer(api)                                                                                /* 更新 server 的值。 */
	defer server.Close()                                                                                            /* 安排函数结束时执行清理。 */
	token, err := api.auth.Issue("admin", "tenant-a", "admin", nil, time.Hour)                                      /* 检查错误并决定后续处理。 */
	if err != nil {                                                                                                 /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	result := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/providers/test", token, map[string]any{ /* 更新 result 的值。 */
		"provider": "deepseek",         /* 执行当前语句并推进处理流程。 */
		"baseUrl":  providerServer.URL, /* 执行当前语句并推进处理流程。 */
		"model":    "active-model",     /* 执行当前语句并推进处理流程。 */
		"question": "连接测试",             /* 执行当前语句并推进处理流程。 */
	}, http.StatusOK) /* 结束当前表达式或代码块。 */
	if result["success"] != true || result["answer"] != "测试成功" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected provider test response: %#v", result) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if got := runtime.CurrentConfig(); got != active { /* 判断条件并选择处理分支。 */
		t.Fatalf("testing changed active provider: got %#v want %#v", got, active) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	for _, invalidURL := range []string{"file:///etc/passwd", "http://user:secret@example.test", "http://example.test?key=secret", "[http://ollama:11434](http://ollama:11434)"} { /* 循环处理当前数据。 */
		requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/providers/test", token, map[string]any{ /* 执行当前语句并推进处理流程。 */
			"provider": "deepseek", "baseUrl": invalidURL, "model": "active-model", /* 执行当前语句并推进处理流程。 */
		}, http.StatusUnprocessableEntity) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	viewerToken, err := api.auth.Issue("viewer", "tenant-a", "viewer", nil, time.Hour) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                    /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/providers/test", viewerToken, map[string]any{ /* 执行当前语句并推进处理流程。 */
		"provider": "deepseek", "baseUrl": providerServer.URL, "model": "active-model", /* 执行当前语句并推进处理流程。 */
	}, http.StatusForbidden) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func newTestHTTPServer(api *Server) *httptest.Server { /* 定义 newTestHTTPServer 函数。 */
	return httptest.NewServer(api.Handler()) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

var _ ports.AIProviderRuntime = (*providerConfigTestRuntime)(nil)          /* 声明 _。 */
var _ ports.AIWorkflowProviderRuntime = (*providerConfigTestWorkflow)(nil) /* 声明 _。 */
