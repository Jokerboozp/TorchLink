package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iot-platform/internal/aiprompt"
	"iot-platform/internal/aiworkflow"
	"iot-platform/internal/devicescope"
	"iot-platform/internal/sites"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	aiadapter "iot-platform/internal/adapters/ai"
	"iot-platform/internal/adapters/knowledge"
	"iot-platform/internal/adapters/local"
	"iot-platform/internal/adapters/memory"
	"iot-platform/internal/aitest"
	"iot-platform/internal/auth"
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

func (r *providerConfigTestRuntime) Chat(context.Context, string, string) (string, error) {
	return "", nil
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
	runtime := &providerConfigTestRuntime{config: ports.AIPluginConfig{Provider: "openai-compatible", BaseURL: "http://localhost:8000/v1", Model: "Qwen/Qwen3-8B"}}
	workflow := &providerConfigTestWorkflow{}
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AI = runtime
	engine.AIPlugins = aiadapter.NewProviderRegistry()
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetAIProviderRuntime(runtime)
	api.SetAIProviderStore(repo)
	api.SetAIWorkflowProvider(workflow)
	server := newTestHTTPServer(api)
	defer server.Close()

	adminToken, err := api.auth.IssueWithVersion("admin", "tenant-a", "admin", api.adminSessionVersion(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	viewerToken, err := api.auth.Issue("viewer", "tenant-a", "viewer", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	updated := requestJSON(t, server.Client(), "PUT", server.URL+"/api/v1/ai/providers/config", adminToken, map[string]any{
		"provider":  "deepseek",
		"baseUrl":   "https://api.deepseek.com",
		"model":     "deepseek-chat",
		"apiKey":    "provider-secret",
		"maxTokens": 3072,
	}, 200)
	if updated["provider"] != "deepseek" || updated["model"] != "deepseek-chat" || updated["maxTokens"] != float64(3072) || updated["apiKeyConfigured"] != true || updated["apiKeyHint"] != "prov***" {
		t.Fatalf("unexpected redacted provider response: %#v", updated)
	}
	if _, leaked := updated["apiKey"]; leaked {
		t.Fatalf("provider key leaked in response: %#v", updated)
	}
	if got := runtime.CurrentConfig(); got.Provider != "deepseek" || got.APIKey != "provider-secret" || got.MaxTokens != 3072 {
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
	if viewer["provider"] != "deepseek" || viewer["maxTokens"] != float64(3072) || viewer["apiKeyConfigured"] != true {
		t.Fatalf("unexpected viewer provider response: %#v", viewer)
	}
	if _, exposed := viewer["baseUrl"]; exposed {
		t.Fatalf("viewer response exposed provider address: %#v", viewer)
	}
	if _, exposed := viewer["apiKeyHint"]; exposed {
		t.Fatalf("viewer response exposed provider key hint: %#v", viewer)
	}
	requestJSON(t, server.Client(), "PUT", server.URL+"/api/v1/ai/providers/config", viewerToken, map[string]any{
		"provider": "openai-compatible",
		"baseUrl":  "http://localhost:8000/v1",
		"model":    "Qwen/Qwen3-8B",
	}, 403)
}

func TestAIProviderConfigReportsActiveWorkflowsAndCanRetry(t *testing.T) {
	var active atomic.Bool
	active.Store(true)
	harnessServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/provider" {
			http.NotFound(w, r)
			return
		}
		if active.Load() {
			w.WriteHeader(http.StatusConflict)
			_, _ = io.WriteString(w, `{"error":{"code":"RUNS_ACTIVE","message":"private upstream detail"}}`)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer harnessServer.Close()
	workflow, err := aiadapter.NewHarnessPool(harnessServer.URL, "0123456789abcdef0123456789abcdef", "http://localhost:8081/mcp/harness", "old-model", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	previous := ports.AIPluginConfig{Provider: "deepseek", BaseURL: "https://api.deepseek.com", Model: "old-model", APIKey: "old-test-key"}
	if err := repo.SaveAIProviderConfig(context.Background(), previous); err != nil {
		t.Fatal(err)
	}
	runtime := &providerConfigTestRuntime{config: previous}
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AI = runtime
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetAIProviderRuntime(runtime)
	api.SetAIProviderStore(repo)
	api.SetAIWorkflowProvider(workflow)
	server := newTestHTTPServer(api)
	defer server.Close()
	token, err := api.auth.IssueWithVersion("admin", "tenant-a", "admin", api.adminSessionVersion(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	candidate := map[string]any{"provider": "deepseek", "baseUrl": "https://api.deepseek.com", "model": "new-model", "apiKey": "new-test-key"}
	result := requestJSON(t, server.Client(), http.MethodPut, server.URL+"/api/v1/ai/providers/config", token, candidate, http.StatusConflict)
	encoded, _ := json.Marshal(result)
	if !strings.Contains(string(encoded), "等待任务结束后重试") || strings.Contains(string(encoded), "private upstream detail") || strings.Contains(string(encoded), "new-test-key") {
		t.Fatalf("expected actionable, redacted conflict: %s", encoded)
	}
	if runtime.CurrentConfig() != previous {
		t.Fatal("busy workflow changed the direct provider")
	}
	if saved, found, err := repo.LoadAIProviderConfig(context.Background()); err != nil || !found || saved != previous {
		t.Fatal("busy workflow changed the persisted provider")
	}
	active.Store(false)
	requestJSON(t, server.Client(), http.MethodPut, server.URL+"/api/v1/ai/providers/config", token, candidate, http.StatusOK)
	if runtime.CurrentConfig().Model != "new-model" || workflow.CurrentConfig().Model != "new-model" {
		t.Fatal("retry did not update both providers")
	}
}

func TestAIProviderConfigSavesWithoutSuccessfulConnectionTest(t *testing.T) {
	var providerRequests atomic.Int32
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerRequests.Add(1)
		http.Error(w, "provider unavailable", http.StatusServiceUnavailable)
	}))
	defer providerServer.Close()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registry := aiadapter.NewProviderRegistry()
	runtime, err := aiadapter.NewRuntimeProvider(registry, ports.AIPluginConfig{Provider: "disabled"})
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AI = runtime
	engine.AIPlugins = registry
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetAIProviderRuntime(runtime)
	api.SetAIProviderStore(repo)
	api.SetAIWorkflowProvider(&providerConfigTestWorkflow{})
	server := newTestHTTPServer(api)
	defer server.Close()
	token, err := api.auth.IssueWithVersion("admin", "tenant-a", "admin", api.adminSessionVersion(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	candidate := map[string]any{"provider": "deepseek", "baseUrl": providerServer.URL, "model": "test-model", "apiKey": "test-key", "maxTokens": 2048}
	for _, scenario := range []string{"untested", "failed-test"} {
		t.Run(scenario, func(t *testing.T) {
			if scenario == "failed-test" {
				result := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/providers/test", token, candidate, http.StatusOK)
				if result["success"] != false {
					t.Fatal("unavailable provider must fail the optional connection test")
				}
				candidate["model"] = "updated-model"
			}
			before := providerRequests.Load()
			requestJSON(t, server.Client(), http.MethodPut, server.URL+"/api/v1/ai/providers/config", token, candidate, http.StatusOK)
			if providerRequests.Load() != before {
				t.Fatal("saving configuration contacted the model provider")
			}
			saved, found, err := repo.LoadAIProviderConfig(context.Background())
			if err != nil || !found || saved.Model != candidate["model"] || saved != runtime.CurrentConfig() {
				t.Fatal("saved configuration and active runtime do not match")
			}
		})
	}
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
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AI = runtime
	engine.AIPlugins = aiadapter.NewProviderRegistry()
	// A newly supplied endpoint must work without an address allowlist.
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetAIProviderRuntime(runtime)
	server := newTestHTTPServer(api)
	defer server.Close()
	token, err := api.auth.IssueWithVersion("admin", "tenant-a", "admin", api.adminSessionVersion(), time.Hour)
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
	for _, invalidURL := range []string{"file:///etc/passwd", "http://user:secret@example.test", "http://example.test?key=secret", "[http://vllm:8000/v1](http://vllm:8000/v1)"} {
		requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/providers/test", token, map[string]any{
			"provider": "deepseek", "baseUrl": invalidURL, "model": "active-model",
		}, http.StatusUnprocessableEntity)
	}
	viewerToken, err := api.auth.Issue("viewer", "tenant-a", "viewer", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/providers/test", viewerToken, map[string]any{
		"provider": "deepseek", "baseUrl": providerServer.URL, "model": "active-model",
	}, http.StatusForbidden)
}

func newTestHTTPServer(api *Server) *httptest.Server {
	return httptest.NewServer(api.Handler())
}

var _ ports.AIProviderRuntime = (*providerConfigTestRuntime)(nil)
var _ ports.AIWorkflowProviderRuntime = (*providerConfigTestWorkflow)(nil)

func TestAIAnalysisJobReportsProgressAndPersistsResult(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AIWorkflows, engine.HarnessTokens = &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) { <-release; return testAnalysisAnswer, nil }}, aitest.Tokens()
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, _, err = repo.UpsertAlarm(context.Background(), model.Alarm{ID: "alarm-progress", TenantID: "tenant-a", DeviceID: "device-a", AlarmType: "SMOKE_DETECTED", AlarmLevel: "HIGH", Status: "ACTIVE", LastTriggeredAt: time.Now().UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	job, err := api.startAIAnalysisJob(context.Background(), "tenant-a", "alarm-progress", "operator", model.AIAnalysisScopeNone, testRunIdentity("operator"))
	if err != nil || job.Status != "running" || job.Progress != 8 || job.EstimatedRemainingMs <= 0 {
		t.Fatalf("unexpected initial progress: %#v", aiAnalysisJobView(job))
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		// A second API instance on the same store stands in for a restart or replica.
		current, found, loadErr := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil))).loadAIAnalysisJob(context.Background(), "tenant-a", "alarm-progress", model.AIAnalysisScopeNone)
		if loadErr != nil || !found {
			t.Fatalf("stored job not readable: found=%v err=%v", found, loadErr)
		}
		if current.Status != "running" {
			if current.Status != "succeeded" || current.Progress != 100 || current.Analysis.Summary != "研判完成" {
				t.Fatalf("unexpected completed progress: %#v", aiAnalysisJobView(current))
			}
			if saved, getErr := repo.GetAIAnalysis(context.Background(), "tenant-a", "alarm-progress", model.AIAnalysisScopeNone); getErr != nil || saved.Summary != "研判完成" {
				t.Fatalf("analysis was not persisted: %#v err=%v", saved, getErr)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("AI analysis job did not complete")
}

func TestAIAnalysisProgressCanBeLoadedWithoutJobID(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.KB = knowledge.NewLocal()
	engine.AIWorkflows, engine.HarnessTokens = &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) { <-release; return testAnalysisAnswer, nil }}, aitest.Tokens()
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, _, err = repo.UpsertAlarm(context.Background(), model.Alarm{ID: "alarm-progress-resume", TenantID: "tenant-a", DeviceID: "device-a", AlarmType: "SMOKE_DETECTED", AlarmLevel: "HIGH", Status: "ACTIVE", LastTriggeredAt: time.Now().UnixMilli()})
	if err != nil {
		t.Fatal(err)
	}
	job, err := api.startAIAnalysisJob(context.Background(), "tenant-a", "alarm-progress-resume", "operator", model.AlarmAnalysisWorkflowID, testRunIdentity("operator")) // 未托管的测试令牌拥有知识库权限，进度按同一知识范围查询。
	if err != nil {
		t.Fatal(err)
	}
	server := newTestHTTPServer(api)
	defer server.Close()
	defer close(release)
	viewerToken, err := api.auth.Issue("viewer", "tenant-a", "viewer", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	progress := requestJSON(t, server.Client(), "GET", server.URL+"/api/v1/ai/alarm-analysis/alarm-progress-resume/progress", viewerToken, nil, 200)
	if progress["jobId"] != job.ID || progress["status"] != "running" {
		t.Fatalf("unexpected resumable progress: %#v", progress)
	}
}

// A running job whose process stopped heartbeating is reported as interrupted,
// and a new run can start for that alarm.
func TestStaleAIAnalysisJobIsMarkedInterrupted(t *testing.T) {
	repo := memory.NewRepository()
	engine := &core.Engine{Repo: devicescope.Wrap(repo), Clock: ports.RealClock{}, Bus: local.NewBus(), Realtime: local.NewRealtime()}
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	old := time.Now().Add(-time.Minute).UnixMilli()
	stale := model.AlarmAnalysisJob{ID: "ai_job_stale", TenantID: "tenant-a", AlarmID: "alarm-stale", Status: "running", Stage: "calling_model", StartedAt: old, UpdatedAt: old}
	if created, err := repo.CreateAlarmAnalysisJob(context.Background(), stale); err != nil || !created {
		t.Fatalf("seed job: %v %v", created, err)
	}
	job, found, err := api.loadAIAnalysisJob(context.Background(), "tenant-a", "alarm-stale", model.AIAnalysisScopeNone)
	if err != nil || !found || job.Status != "failed" || job.Error == "" {
		t.Fatalf("stale job must be interrupted: %#v found=%v err=%v", job, found, err)
	}
	if created, err := repo.CreateAlarmAnalysisJob(context.Background(), model.AlarmAnalysisJob{ID: "ai_job_new", TenantID: "tenant-a", AlarmID: "alarm-stale", Status: "running"}); err != nil || !created {
		t.Fatalf("a new run must be allowed after the interruption: %v %v", created, err)
	}
}

func TestProtocolAssistantExcelUploadDoesNotRequireAI(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.ModbusCoilParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.Metrics = metrics.New()
	if err = engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DevMode = true
	cfg.JWTSecret = "test-secret-at-least-32-characters"
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	login := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "admin123", "tenantId": "tenant_001"}, http.StatusOK)
	token := login["accessToken"].(string)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("name", "Excel 火花探测器")
	_ = writer.WriteField("transport", "MODBUS_TCP")
	_ = writer.WriteField("payloadFormat", "hex")
	file, _ := writer.CreateFormFile("file", "变量地址表.xlsx")
	_, _ = file.Write(protocolAssistantXLSXFixture(t))
	_ = writer.Close()
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/ai/protocol-assistant/generate", &body)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var draft model.ProtocolAssistantDraft
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&draft) != nil {
		t.Fatalf("Excel generate status=%d", response.StatusCode)
	}
	if draft.ParserType != parser.ModbusCoilParserName || len(draft.Fields) != 2 || draft.Source != "" {
		t.Fatalf("unexpected Excel draft %#v", draft)
	}
}

func protocolAssistantXLSXFixture(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	zw := zip.NewWriter(&data)
	shared, err := zw.Create("xl/sharedStrings.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = shared.Write([]byte(`<sst><si><t>序号</t></si><si><t>变量名称</t></si><si><t>PLC 线圈地址</t></si><si><t>Modbus地址（十进制）</t></si><si><t>数据类型</t></si><si><t>无报出状态</t></si><si><t>报出状态</t></si><si><t>备注</t></si><si><t>通讯心跳测试</t></si><si><t>M100</t></si><si><t>BOOL</t></si><si><t>火花探测组1报警</t></si><si><t>M3001</t></si></sst>`))
	sheet, err := zw.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = sheet.Write([]byte(`<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="s"><v>2</v></c><c r="D1" t="s"><v>3</v></c><c r="E1" t="s"><v>4</v></c><c r="F1" t="s"><v>5</v></c><c r="G1" t="s"><v>6</v></c><c r="H1" t="s"><v>7</v></c></row><row r="2"><c r="A2"><v>1</v></c><c r="B2" t="s"><v>8</v></c><c r="C2" t="s"><v>9</v></c><c r="D2"><v>100</v></c><c r="E2" t="s"><v>10</v></c><c r="F2"><v>0</v></c><c r="G2"><v>1</v></c></row><row r="3"><c r="A3"><v>2</v></c><c r="B3" t="s"><v>11</v></c><c r="C3" t="s"><v>12</v></c><c r="D3"><v>3001</v></c><c r="E3" t="s"><v>10</v></c><c r="F3"><v>0</v></c><c r="G3"><v>1</v></c></row></sheetData></worksheet>`))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

type protocolEndpointAI struct{}

func (protocolEndpointAI) RuleDraft(context.Context, string, string) (model.AlarmRule, error) {
	return model.AlarmRule{
		Name:        "AI 高温烟雾规则",
		Description: "温度持续过高且烟雾信号出现时提示人工处置",
		AlarmType:   "FIRE_RISK",
		Level:       "HIGH",
		Match:       "all",
		Conditions: []model.RuleCondition{
			{Field: "properties.temperature", Operator: ">", Value: 80},
			{Field: "properties.smoke", Operator: "eq", Value: true},
		},
		Recovery: []model.RuleCondition{{Field: "properties.temperature", Operator: "lt", Value: 70}},
		Actions:  []model.RuleAction{{Type: "OPEN_PAGE", Page: "alarms"}},
	}, nil
}
func (protocolEndpointAI) GenerateJSON(context.Context, string, string, string) (string, error) {
	return `{"name":"端点测试协议","protocol":"endpoint-modbus","transport":"MODBUS_TCP","payloadFormat":"hex","parserType":"modbus_coil_parser","messageType":"PROPERTY_REPORT","config":{"frame":"tcp","startAddress":0,"functionCode":1,"fields":[{"name":"smoke","coilAddress":0}]},"fields":[{"name":"smoke","label":"烟雾","type":"boolean","coilAddress":0,"dataType":"BOOL"}]}`, nil
}

func TestAIRuleDraftReturnsAnnotatedJSONAndCommentedGengine(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	installEndpointWorkflows(engine)
	engine.Metrics = metrics.New()
	if err = engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DevMode = true
	cfg.JWTSecret = "test-secret-at-least-32-characters"
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	login := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "admin123", "tenantId": "tenant_001"}, http.StatusOK)
	token := login["accessToken"].(string)
	result := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/rule-draft", token, map[string]any{"text": "温度超过 80 且烟雾出现"}, http.StatusOK)

	draft := result["draft"].(map[string]any)
	if draft["enabled"] != false || draft["expression"] != nil || draft["tenantId"] != "tenant_001" {
		t.Fatalf("AI rule draft was not kept as a safe tenant draft: %#v", draft)
	}
	presentation := result["presentation"].(map[string]any)
	var executableJSON map[string]any
	if err := json.Unmarshal([]byte(presentation["json"].(string)), &executableJSON); err != nil {
		t.Fatalf("presentation JSON is not executable JSON: %v", err)
	}
	if presentation["gengine"].(string) == "" || !strings.HasPrefix(strings.TrimSpace(presentation["genginePlaceholder"].(string)), "//") {
		t.Fatalf("Gengine presentation is not an explicitly commented alternative: %#v", presentation)
	}
	descriptions := presentation["fieldDescriptions"].([]any)
	needed := map[string]bool{"conditions[].field": false, "recovery[].value": false, "actions[].page": false}
	for _, item := range descriptions {
		field := item.(map[string]any)["field"].(string)
		if _, ok := needed[field]; ok {
			needed[field] = true
		}
	}
	for field, found := range needed {
		if !found {
			t.Fatalf("missing nested field description %q", field)
		}
	}
}

func TestProtocolAssistantEndpoints(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.ModbusCoilParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	installEndpointWorkflows(engine)
	engine.Metrics = metrics.New()
	if err = engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DevMode = true
	cfg.JWTSecret = "test-secret-at-least-32-characters"
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	login := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "admin123", "tenantId": "tenant_001"}, http.StatusOK)
	token := login["accessToken"].(string)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("pointTable", "smoke = M0")
	_ = writer.WriteField("transport", "MODBUS_TCP")
	_ = writer.WriteField("samplePayload", "00 01 00 00 00 04 01 01 01 01")
	file, _ := writer.CreateFormFile("file", "protocol.csv")
	_, _ = file.Write([]byte("address,name\n0,smoke\n"))
	_ = writer.Close()
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/ai/protocol-assistant/generate", &body)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var draft model.ProtocolAssistantDraft
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&draft) != nil {
		response.Body.Close()
		t.Fatalf("generate protocol assistant status=%d", response.StatusCode)
	}
	response.Body.Close()
	if draft.Source != "" || draft.ParserType != parser.ModbusCoilParserName || len(draft.Fields) != 1 {
		t.Fatalf("unexpected generated draft %#v", draft)
	}
	draft.Fields[0].Name = "smoke_alarm"
	draft.Config["fields"] = []any{map[string]any{"name": "smoke_alarm", "coilAddress": 0}}
	preview := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/protocol-assistant/preview", token, map[string]any{"draft": draft, "payload": "00 01 00 00 00 04 01 01 01 01", "payloadFormat": "hex"}, http.StatusOK)
	if preview["success"] != true || preview["standardMessage"].(map[string]any)["properties"].(map[string]any)["smoke_alarm"] != true {
		t.Fatalf("unexpected preview %#v", preview)
	}
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/protocol-assistant/publish", token, map[string]any{"draft": draft, "status": "PUBLISHED"}, http.StatusUnprocessableEntity)
	published := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/protocol-assistant/publish", token, map[string]any{"draft": draft, "payload": "00 01 00 00 00 04 01 01 01 01", "payloadFormat": "hex", "status": "DRAFT"}, http.StatusCreated)
	pkg := published["package"].(map[string]any)
	if pkg["parserType"] != parser.ModbusCoilParserName || pkg["status"] != "DRAFT" {
		t.Fatalf("unexpected published package %#v", pkg)
	}
	if _, ok := pkg["config"].(map[string]any)["fields"]; !ok {
		t.Fatalf("published package did not persist edited field: %#v", pkg)
	}
}

func TestHealthInspectionPDFDownload(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.Metrics = metrics.New()
	if err = engine.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	cfg.DevMode = true
	cfg.JWTSecret = "test-secret-at-least-32-characters"
	api := New(cfg, engine, engine.Metrics.(*metrics.Registry), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	login := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "admin123", "tenantId": "tenant_001"}, http.StatusOK)
	report := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/health-inspection", login["accessToken"].(string), map[string]any{}, http.StatusOK)
	generatedAt, ok := report["generatedAt"].(float64)
	if !ok || generatedAt <= 0 {
		t.Fatalf("health inspection generatedAt = %#v", report["generatedAt"])
	}
	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/ai/health-inspection/pdf", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+login["accessToken"].(string))
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/pdf" || !bytes.Contains([]byte(response.Header.Get("Content-Disposition")), fmt.Appendf(nil, "health-inspection-%d.pdf", int64(generatedAt))) || !bytes.HasPrefix(data, []byte("%PDF-1.4")) {
		t.Fatalf("PDF response status=%d type=%q disposition=%q prefix=%q", response.StatusCode, response.Header.Get("Content-Type"), response.Header.Get("Content-Disposition"), data[:min(len(data), 8)])
	}
}

// Repeated downloads of one report render it once; when every render slot is
// taken the caller gets errPDFBusy instead of queueing without bound.
func TestInspectionPDFCacheRendersOnceAndBoundsConcurrency(t *testing.T) {
	c := newInspectionPDFCache()
	var renders atomic.Int32
	release := make(chan struct{})
	c.renderPDF = func(report model.DeviceHealthReport) ([]byte, error) {
		renders.Add(1)
		if report.GeneratedAt == 2 {
			<-release
		}
		return []byte("pdf"), nil
	}
	report := model.DeviceHealthReport{GeneratedAt: 1}
	for i := 0; i < 3; i++ {
		if _, err := c.load(context.Background(), "tenant", "job-1", func(context.Context) (model.DeviceHealthReport, error) { return report, nil }); err != nil {
			t.Fatal(err)
		}
	}
	if renders.Load() != 1 {
		t.Fatalf("the same report must render once, rendered %d times", renders.Load())
	}
	for i := 0; i < pdfRenderSlots; i++ {
		go c.load(context.Background(), "tenant-"+string(rune('a'+i)), "job-2", func(context.Context) (model.DeviceHealthReport, error) {
			return model.DeviceHealthReport{GeneratedAt: 2}, nil
		})
	}
	for len(c.slots) < pdfRenderSlots {
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.load(ctx, "tenant-z", "job-3", func(context.Context) (model.DeviceHealthReport, error) {
		return model.DeviceHealthReport{GeneratedAt: 3}, nil
	}); !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, errPDFBusy) {
		t.Fatalf("a full renderer must not start another render, got %v", err)
	}
	close(release)
}

func TestHealthInspectionJobCanBeLoadedWithoutJobID(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseJob := func() { releaseOnce.Do(func() { close(release) }) }
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AIWorkflows, engine.HarnessTokens = &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) { <-release; return "巡检建议已生成", nil }}, aitest.Tokens()
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := newTestHTTPServer(api)
	defer server.Close()
	defer releaseJob()
	token, err := api.auth.Issue("viewer", "tenant-a", "viewer", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	started := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/health-inspection/run", token, map[string]any{}, http.StatusAccepted)
	if started["status"] != "running" || started["progress"] != float64(8) {
		t.Fatalf("unexpected initial inspection progress: %#v", started)
	}
	progress := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/health-inspection/progress", token, nil, http.StatusOK)
	if progress["jobId"] != started["jobId"] || progress["status"] != "running" {
		t.Fatalf("unexpected resumable inspection progress: %#v", progress)
	}
	releaseJob()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		progress = requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/health-inspection/progress", token, nil, http.StatusOK)
		if progress["status"] == "succeeded" {
			if progress["progress"] != float64(100) {
				t.Fatalf("unexpected completed inspection progress: %#v", progress)
			}
			if progress["report"] == nil {
				t.Fatalf("completed inspection did not include report: %#v", progress)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("health inspection job did not complete")
}

// Inspection progress and results live in the repository, so a restarted
// process or another replica sees the same job and the running-job guard.
func TestHealthInspectionJobIsSharedAcrossServerInstances(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseJob := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseJob()
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AIWorkflows, engine.HarnessTokens = &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) { <-release; return "巡检建议已生成", nil }}, aitest.Tokens()
	first := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	second := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	firstServer, secondServer := newTestHTTPServer(first), newTestHTTPServer(second)
	defer firstServer.Close()
	defer secondServer.Close()
	token, err := first.auth.Issue("viewer", "tenant-a", "viewer", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	started := requestJSON(t, firstServer.Client(), http.MethodPost, firstServer.URL+"/api/v1/ai/health-inspection/run", token, map[string]any{}, http.StatusAccepted)
	again := requestJSON(t, secondServer.Client(), http.MethodPost, secondServer.URL+"/api/v1/ai/health-inspection/run", token, map[string]any{}, http.StatusAccepted)
	if again["jobId"] != started["jobId"] {
		t.Fatalf("second instance started a duplicate inspection: first=%v second=%v", started["jobId"], again["jobId"])
	}
	releaseJob()
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		progress := requestJSON(t, secondServer.Client(), http.MethodGet, secondServer.URL+"/api/v1/ai/health-inspection/progress/"+started["jobId"].(string), token, nil, http.StatusOK)
		if progress["status"] == "succeeded" && progress["report"] != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("other instance did not observe the finished job: %v", progress)
		}
	}
	if _, err := second.engine.Repo.LatestHealthInspectionSummary(context.Background(), "tenant-a", "succeeded"); err != nil {
		t.Fatal("PDF download on another instance cannot reuse the finished report")
	}
}

func TestHealthInspectionStaleRunningJobIsMarkedInterrupted(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := newTestHTTPServer(api)
	defer server.Close()
	token, err := api.auth.Issue("viewer", "tenant-a", "viewer", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// A job left running by a process that stopped heartbeating.
	old := time.Now().Add(-2 * healthInspectionStaleAfter).UnixMilli()
	if _, err = repo.CreateHealthInspectionJob(context.Background(), model.HealthInspectionJob{ID: "inspection_job_orphan", TenantID: "tenant-a", Status: "running", Progress: 40, StartedAt: old, UpdatedAt: old}); err != nil {
		t.Fatal(err)
	}
	progress := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/health-inspection/progress", token, nil, http.StatusOK)
	if progress["jobId"] != "inspection_job_orphan" || progress["status"] != "failed" || progress["error"] == nil {
		t.Fatalf("orphaned job must be reported as interrupted: %v", progress)
	}
	started := requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/health-inspection/run", token, map[string]any{}, http.StatusAccepted)
	if started["jobId"] == "inspection_job_orphan" || started["status"] != "running" {
		t.Fatalf("a new inspection must start after the interrupted one: %v", started)
	}
}

const testAnalysisAnswer = `{"summary":"研判完成","possibleReasons":["现场存在烟雾"],"suggestions":["核实现场"],"riskLevel":"HIGH","confidence":0.9}`

// installEndpointWorkflows answers every business workflow through the Harness
// fake: rule drafts and protocol drafts reuse protocolEndpointAI's content.
func installEndpointWorkflows(engine *core.Engine) *aitest.Workflows {
	workflows := &aitest.Workflows{Answer: func(req ports.AIWorkflowRequest) (string, error) {
		switch req.WorkflowID {
		case aiworkflow.WorkflowRuleDraft:
			rule, err := protocolEndpointAI{}.RuleDraft(context.Background(), "", "")
			if err != nil {
				return "", err
			}
			body, err := json.Marshal(rule)
			return string(body), err
		case aiworkflow.WorkflowProtocolAssist:
			return protocolEndpointAI{}.GenerateJSON(context.Background(), "", "", "")
		case aiworkflow.WorkflowHealthInspection, aiworkflow.WorkflowOpsReport:
			return "巡检建议已生成", nil
		}
		return testAnalysisAnswer, nil
	}}
	engine.AIWorkflows, engine.HarnessTokens = workflows, aitest.Tokens()
	return workflows
}

// testRunIdentity is an unmanaged caller allowed every Harness tool scope.
func testRunIdentity(username string) ports.AIRunIdentity {
	return ports.AIRunIdentity{TenantID: "tenant-a", Username: username, Scopes: auth.HarnessReadScopes()}
}

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
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	engine.HarnessTokens = harnessTokens(cfg)
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
	adminToken, err := api.auth.IssueWithVersion("admin", "tenant-a", "admin", api.adminSessionVersion(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	workflows := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/workflows", token, nil, http.StatusOK)
	if workflows["configured"] != true || workflows["healthy"] != true || workflows["total"].(float64) != 1 {
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
	if managed["total"] != float64(1) || managed["items"].([]any)[0].(map[string]any)["persona"] != manifest["persona"] {
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
	if defaultBinding["retrievalMode"] != "always" || defaultBinding["topK"] != float64(5) {
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
	claims, err := api.harnessAuth.Parse(captured.MCPToken)
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
	if captured.ConversationID == "browser-controlled" || captured.ConversationID != aiworkflow.ChatConversationID("tenant-a", "alice", "browser-controlled") {
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
	if !strings.Contains(forced.Question, "doc-1") || !strings.Contains(forced.Question, "chunk-1") || !strings.Contains(forced.Question, "现场 复核") {
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
	if errorMessage, _ := failedSync["detail"].(string); !strings.HasPrefix(errorMessage, "AI 工作流请求失败（编号 ") || strings.Contains(errorMessage, "sensitive internal runtime error") {
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

	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/mcp/harness", token, map[string]any{}, http.StatusUnauthorized) // session key cannot sign run credentials
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
	a := aiworkflow.ChatConversationID("tenant-a", "alice", "conversation-1")
	if a != aiworkflow.ChatConversationID("tenant-a", "alice", "conversation-1") {
		t.Fatal("conversation derivation is not stable")
	}
	if a == aiworkflow.ChatConversationID("tenant-b", "alice", "conversation-1") || a == aiworkflow.ChatConversationID("tenant-a", "bob", "conversation-1") {
		t.Fatal("conversation derivation is not tenant/user scoped")
	}
	if !strings.HasPrefix(a, "conv_") || strings.Contains(a, "tenant-a") || strings.Contains(a, "alice") {
		t.Fatalf("conversation derivation leaks identity: %q", a)
	}
}

func TestKnowledgeUploadAndTenantScopedList(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.KB = knowledge.NewLocal()
	cfg := config.Load()
	cfg.JWTSecret = "test-secret-at-least-32-characters"
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	operatorToken, err := api.auth.Issue("operator", "tenant-a", "operator", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	viewerToken, err := api.auth.Issue("viewer", "tenant-a", "viewer", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	otherTenantToken, err := api.auth.Issue("viewer", "tenant-b", "viewer", nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err = form.WriteField("workflowId", "ops-assistant"); err != nil {
		t.Fatal(err)
	}
	if err = form.WriteField("productId", "fire-smoke"); err != nil {
		t.Fatal(err)
	}
	if err = form.WriteField("category", "alarm-sop"); err != nil {
		t.Fatal(err)
	}
	if err = form.WriteField("tags", "smoke,certified"); err != nil {
		t.Fatal(err)
	}
	file, err := form.CreateFormFile("file", "fire-sop.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.Copy(file, strings.NewReader("高温烟雾告警处置：先核对设备状态，再通知现场人员复核。")); err != nil {
		t.Fatal(err)
	}
	if err = form.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/knowledge/documents", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+operatorToken)
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var uploaded map[string]any
	if err = json.NewDecoder(resp.Body).Decode(&uploaded); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated || uploaded["status"] != "INDEXED" || uploaded["filename"] != "fire-sop.txt" {
		t.Fatalf("unexpected upload response status=%d body=%#v", resp.StatusCode, uploaded)
	}

	listed := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/knowledge/documents", viewerToken, nil, http.StatusOK)
	if listed["total"] != float64(1) || listed["persistentIndex"] != false || listed["indexMode"] != "local-memory" {
		t.Fatalf("unexpected knowledge list %#v", listed)
	}
	items := listed["items"].([]any)
	item := items[0].(map[string]any)
	if item["tenantId"] != "tenant-a" || item["workflowId"] != "ops-assistant" || item["productId"] != "fire-smoke" || item["category"] != "alarm-sop" || item["filename"] != "fire-sop.txt" {
		t.Fatalf("unexpected knowledge item %#v", item)
	}
	tags := item["tags"].([]any)
	if len(tags) != 2 || tags[0] != "smoke" || tags[1] != "certified" {
		t.Fatalf("unexpected knowledge tags %#v", tags)
	}
	detail := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/knowledge/documents/"+item["id"].(string), viewerToken, nil, http.StatusOK)
	indexDetails := detail["index"].(map[string]any)
	if indexDetails["mode"] != "local-memory" || indexDetails["chunkCount"] != float64(1) {
		t.Fatalf("unexpected knowledge index details %#v", detail)
	}
	chunking := indexDetails["chunking"].(map[string]any)
	if chunking["size"] != float64(1200) || chunking["overlap"] != float64(200) {
		t.Fatalf("unexpected chunking policy %#v", chunking)
	}
	detailChunks := detail["chunks"].([]any)
	if len(detailChunks) != 1 || detailChunks[0].(map[string]any)["content"] != "高温烟雾告警处置：先核对设备状态，再通知现场人员复核。" || detailChunks[0].(map[string]any)["vectorized"] != false {
		t.Fatalf("unexpected knowledge chunks %#v", detailChunks)
	}

	isolated := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/knowledge/documents", otherTenantToken, nil, http.StatusOK)
	if isolated["total"] != float64(0) {
		t.Fatalf("knowledge documents leaked across tenants: %#v", isolated)
	}
	requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/knowledge/documents/"+item["id"].(string), otherTenantToken, nil, http.StatusNotFound)
	if summary := listed["summary"].(map[string]any); summary["documents"] != float64(1) || summary["indexed"] != float64(1) || summary["chunks"] != float64(1) {
		t.Fatalf("knowledge summary should count the whole tenant: %#v", summary)
	}
	if summary := isolated["summary"].(map[string]any); summary["documents"] != float64(0) {
		t.Fatalf("knowledge summary leaked across tenants: %#v", summary)
	}

	// Retrieval test searches only the caller's tenant and the named Agent.
	testPath := func(workflowID string) string {
		return server.URL + "/api/v1/ai/workflows/" + workflowID + "/knowledge-binding/test"
	}
	probe := map[string]any{"question": "高温烟雾告警处置", "topK": 3, "minScore": 0}
	found := requestJSON(t, server.Client(), http.MethodPost, testPath("ops-assistant"), viewerToken, probe, http.StatusOK)
	hits := found["items"].([]any)
	if len(hits) != 1 || hits[0].(map[string]any)["documentId"] != item["id"] || !strings.Contains(hits[0].(map[string]any)["content"].(string), "现场人员复核") {
		t.Fatalf("retrieval test did not return the Agent's document: %#v", found)
	}
	for name, response := range map[string]map[string]any{
		"other agent":  requestJSON(t, server.Client(), http.MethodPost, testPath("other-agent"), viewerToken, probe, http.StatusOK),
		"other tenant": requestJSON(t, server.Client(), http.MethodPost, testPath("ops-assistant"), otherTenantToken, probe, http.StatusOK),
	} {
		if len(response["items"].([]any)) != 0 {
			t.Fatalf("retrieval test leaked to %s: %#v", name, response)
		}
	}
	requestJSON(t, server.Client(), http.MethodPost, testPath("ops-assistant"), viewerToken, map[string]any{"question": " ", "topK": 3}, http.StatusUnprocessableEntity)
}

type inspectionReadCounter struct {
	*memory.Repository
	fullReads int
}

func (r *inspectionReadCounter) LatestHealthInspectionJob(ctx context.Context, tenant, status string) (model.HealthInspectionJob, error) {
	r.fullReads++
	return r.Repository.LatestHealthInspectionJob(ctx, tenant, status)
}
func TestInspectionDownloadReadsMetadataBeforePDFCache(t *testing.T) {
	repo := &inspectionReadCounter{Repository: memory.NewRepository()}
	now := time.Now().UnixMilli()
	items := make([]model.DeviceHealthItem, 10000)
	_, _ = repo.CreateHealthInspectionJob(context.Background(), model.HealthInspectionJob{ID: "stable-report", TenantID: "t", Status: "succeeded", StartedAt: now, FinishedAt: now, Report: model.DeviceHealthReport{GeneratedAt: now, Items: items}})
	e := &core.Engine{Repo: devicescope.Wrap(repo), Clock: ports.RealClock{}}
	api := New(config.Config{DevMode: true}, e, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.inspectionPDFs.renderPDF = func(model.DeviceHealthReport) ([]byte, error) { return []byte("%PDF-test"), nil }
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()
	token, _ := api.auth.IssueWithVersion("admin", "t", "admin", api.adminSessionVersion(), time.Hour)
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest("POST", srv.URL+"/api/v1/ai/health-inspection/pdf", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("download %d", res.StatusCode)
		}
	}
	if repo.fullReads != 0 {
		t.Fatalf("PDF cache hit still loads entire inspection: fullReads=%d", repo.fullReads)
	}
}

func TestInspectionReportPagesUseImmutableIDAndTenant(t *testing.T) {
	repo := memory.NewRepository()
	now := time.Now().UnixMilli()
	for _, id := range []string{"report-a", "report-b"} {
		items := make([]model.DeviceHealthItem, 205)
		for i := range items {
			items[i].DeviceID = fmt.Sprintf("%s-%d", id, i)
		}
		_, _ = repo.CreateHealthInspectionJob(context.Background(), model.HealthInspectionJob{ID: id, TenantID: "t", Status: "succeeded", StartedAt: now, Report: model.DeviceHealthReport{GeneratedAt: now, Items: items}})
	}
	api := New(config.Config{DevMode: true}, &core.Engine{Repo: devicescope.Wrap(repo), Clock: ports.RealClock{}}, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(api.Handler())
	defer srv.Close()
	token, _ := api.auth.IssueWithVersion("admin", "t", "admin", api.adminSessionVersion(), time.Hour)
	for _, id := range []string{"report-a", "report-b"} {
		got := requestJSON(t, srv.Client(), "GET", srv.URL+"/api/v1/ai/health-inspection/reports/"+id+"?limit=50&offset=200", token, nil, 200)
		items := got["items"].([]any)
		if len(items) != 5 || got["reportId"] != id || got["totalItems"] != float64(205) || items[0].(map[string]any)["deviceId"] != id+"-200" {
			t.Fatal(got)
		}
	}
	other, _ := api.auth.IssueWithVersion("admin", "other", "admin", api.adminSessionVersion(), time.Hour)
	requestJSON(t, srv.Client(), "GET", srv.URL+"/api/v1/ai/health-inspection/reports/report-a", other, nil, 404)
}

// Another replica saved a new key; this replica's memory is stale. A save
// that keeps the key must keep the stored one, not resurrect the old one.
func TestAIProviderConfigKeepsTheStoredKeyOnAStaleReplica(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stale := ports.AIPluginConfig{Provider: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-chat", APIKey: "old-key"}
	if err := repo.SaveAIProviderConfig(context.Background(), ports.AIPluginConfig{Provider: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-chat", APIKey: "key-saved-elsewhere"}); err != nil {
		t.Fatal(err)
	}
	runtime := &providerConfigTestRuntime{config: stale}
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AI = runtime
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetAIProviderRuntime(runtime)
	api.SetAIProviderStore(repo)
	api.SetAIWorkflowProvider(&providerConfigTestWorkflow{})
	server := newTestHTTPServer(api)
	defer server.Close()
	token, err := api.auth.IssueWithVersion("admin", "tenant-a", "admin", api.adminSessionVersion(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	requestJSON(t, server.Client(), http.MethodPut, server.URL+"/api/v1/ai/providers/config", token, map[string]any{"provider": "deepseek", "baseUrl": "https://api.deepseek.com", "model": "deepseek-reasoner"}, http.StatusOK)
	saved, _, _ := repo.LoadAIProviderConfig(context.Background())
	if saved.APIKey != "key-saved-elsewhere" || runtime.CurrentConfig().APIKey != "key-saved-elsewhere" {
		t.Fatalf("stale key resurrected: saved=%q active=%q", saved.APIKey, runtime.CurrentConfig().APIKey)
	}
}

// An Agent change that reached only some Harness instances is stored as the
// desired state for reconciliation; one no instance accepted is not stored.
func TestDynamicAgentChangesAreStoredForReconciliation(t *testing.T) {
	var saves atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/plugins" && r.Method == http.MethodPost:
			saves.Add(1)
			var m ports.AIWorkflowManifest
			_ = json.NewDecoder(r.Body).Decode(&m)
			_ = json.NewEncoder(w).Encode(ports.AIWorkflowPlugin{ID: m.ID, Name: m.Name})
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer up.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	down := "http://" + listener.Addr().String()
	listener.Close()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	var lock sync.Mutex
	api.SetAISync(&lock, repo)
	server := newTestHTTPServer(api)
	defer server.Close()
	token, err := api.auth.IssueWithVersion("admin", "tenant-a", "admin", api.adminSessionVersion(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{"schemaVersion": 1, "id": "night-shift", "name": "夜班助手", "description": "d", "version": "1", "enabled": true, "persona": "p", "defaultModel": "deepseek-chat", "maxTokens": 2048, "capabilities": []string{"c"}, "allowedTools": []string{"mcp__iot__query_alarm_list"}}
	for _, scenario := range []struct {
		urls   string
		status int
		stored bool
	}{{down, http.StatusBadGateway, false}, {up.URL + "," + down, http.StatusCreated, true}} {
		pool, err := aiadapter.NewHarnessPool(scenario.urls, "0123456789abcdef0123456789abcdef", "http://localhost:8081/mcp/harness", "m", time.Second)
		if err != nil {
			t.Fatal(err)
		}
		engine.AIWorkflows = pool
		requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/workflows", token, manifest, scenario.status)
		stored, _ := repo.ListAIWorkflowManifests(context.Background())
		if (len(stored) == 1) != scenario.stored {
			t.Fatalf("%s: stored %+v", scenario.urls, stored)
		}
	}
	requestJSON(t, server.Client(), http.MethodDelete, server.URL+"/api/v1/ai/workflows/night-shift", token, nil, http.StatusOK)
	if stored, _ := repo.ListAIWorkflowManifests(context.Background()); len(stored) != 1 || !stored[0].Deleted {
		t.Fatalf("partial delete not kept as a tombstone: %+v", stored)
	}
}

// Alarm analysis lists nearby alarms only for devices the requester may see.
func TestAlarmAnalysisNearbyAlarmsFollowDeviceScope(t *testing.T) {
	ctx := context.Background()
	base := memory.NewRepository()
	repo := devicescope.Wrap(base)
	engine := &core.Engine{Repo: devicescope.Wrap(repo), Clock: ports.RealClock{}, Bus: local.NewBus(), Realtime: local.NewRealtime(), Locator: sites.New(repo)}
	workflows := &aitest.Workflows{Answer: func(ports.AIWorkflowRequest) (string, error) { return testAnalysisAnswer, nil }}
	engine.AIWorkflows, engine.HarnessTokens = workflows, aitest.Tokens()
	for _, id := range []string{"mine", "visible", "hidden"} {
		_ = base.SaveManagedDevice(ctx, model.ManagedDevice{ID: id, TenantID: "t1", ProductID: "p", AccessKey: "ak-" + id})
	}
	state := model.SiteState{Units: []model.SiteUnit{{SiteRecord: model.SiteRecord{ID: "u"}}}, Buildings: []model.SiteBuilding{{SiteRecord: model.SiteRecord{ID: "b"}, UnitID: "u"}}, Floors: []model.SiteFloor{{SiteRecord: model.SiteRecord{ID: "f"}, BuildingID: "b"}}}
	for _, id := range []string{"mine", "visible", "hidden"} {
		state.Points = append(state.Points, model.SitePoint{SiteRecord: model.SiteRecord{ID: "p-" + id}, UnitID: "u", BuildingID: "b", FloorID: "f", DeviceID: id})
	}
	if saved, err := base.SaveSiteState(ctx, "t1", model.SiteState{}, state); err != nil || !saved {
		t.Fatal(saved, err)
	}
	now := time.Now().UnixMilli()
	for _, a := range []model.Alarm{
		{ID: "alarm-mine", TenantID: "t1", RuleID: "r1", DeviceID: "mine", AlarmType: "SMOKE_DETECTED", AlarmLevel: "HIGH", Status: "ACTIVE", LastTriggeredAt: now, Location: &model.AlarmLocation{UnitID: "u", BuildingID: "b", FloorID: "f", PointID: "p-mine"}},
		{ID: "alarm-visible", TenantID: "t1", RuleID: "r2", DeviceID: "visible", AlarmType: "SMOKE_DETECTED", Status: "ACTIVE", LastTriggeredAt: now - 1000},
		{ID: "alarm-hidden", TenantID: "t1", RuleID: "r3", DeviceID: "hidden", AlarmType: "SMOKE_DETECTED", Status: "ACTIVE", LastTriggeredAt: now - 1000},
	} {
		if _, _, err := base.UpsertAlarm(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	scoped := devicescope.With(aitest.Context(ctx), devicescope.Scope{Tenant: "t1", IDs: map[string]bool{"mine": true, "visible": true}})
	if _, err := aiworkflow.New(engine, nil).AnalyzeAlarm(scoped, "t1", "alarm-mine", false); err != nil {
		t.Fatal(err)
	}
	question := workflows.Last().Question
	if !strings.Contains(question, "alarm-visible") || strings.Contains(question, "alarm-hidden") || strings.Contains(question, `"hidden"`) {
		t.Fatalf("nearby alarms ignore the device scope: %s", question)
	}
}

// A blank key only falls back to the stored key for the address it was saved
// for; pointing the test at another address must not leak the stored key.
func TestAIProviderTestOnlyReusesStoredKeyForSameAddress(t *testing.T) {
	var authorizations []string
	recorder := func(w http.ResponseWriter, r *http.Request) {
		authorizations = append(authorizations, r.Host+" "+r.Header.Get("Authorization"))
		http.Error(w, "provider unavailable", http.StatusServiceUnavailable)
	}
	savedServer := httptest.NewServer(http.HandlerFunc(recorder))
	defer savedServer.Close()
	otherServer := httptest.NewServer(http.HandlerFunc(recorder))
	defer otherServer.Close()
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registry := aiadapter.NewProviderRegistry()
	runtime, err := aiadapter.NewRuntimeProvider(registry, ports.AIPluginConfig{Provider: "disabled"})
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AI = runtime
	engine.AIPlugins = registry
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetAIProviderRuntime(runtime)
	api.SetAIProviderStore(repo)
	api.SetAIWorkflowProvider(&providerConfigTestWorkflow{})
	server := newTestHTTPServer(api)
	defer server.Close()
	token, err := api.auth.IssueWithVersion("admin", "tenant-a", "admin", api.adminSessionVersion(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	requestJSON(t, server.Client(), http.MethodPut, server.URL+"/api/v1/ai/providers/config", token, map[string]any{"provider": "deepseek", "baseUrl": savedServer.URL, "model": "test-model", "apiKey": "stored-key", "maxTokens": 2048}, http.StatusOK)
	// Without the stored key the other address has nothing to authenticate with.
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/providers/test", token, map[string]any{"provider": "deepseek", "baseUrl": otherServer.URL, "model": "test-model"}, http.StatusUnprocessableEntity)
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/providers/test", token, map[string]any{"provider": "deepseek", "baseUrl": savedServer.URL, "model": "test-model"}, http.StatusOK)
	otherHost, savedHost := strings.TrimPrefix(otherServer.URL, "http://"), strings.TrimPrefix(savedServer.URL, "http://")
	for _, seen := range authorizations {
		if strings.HasPrefix(seen, otherHost) {
			t.Fatalf("another address was contacted with the stored key: %v", authorizations)
		}
	}
	if !slices.Contains(authorizations, savedHost+" Bearer stored-key") {
		t.Fatalf("stored key was not reused for its own address: %v", authorizations)
	}
}

type failingProviderStore struct{ *memory.Repository }

func (failingProviderStore) SaveAIProviderConfig(context.Context, ports.AIPluginConfig) error {
	return errors.New("database unavailable")
}

// Reconciliation pushes the stored configuration; when saving fails the
// switch is undone at once on the runtime and the Harness, matching the
// response instead of being reverted silently later.
func TestAIProviderConfigSaveFailureRestoresPreviousEverywhere(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	previous := ports.AIPluginConfig{Provider: "deepseek", BaseURL: "https://api.deepseek.com", Model: "old-model", APIKey: "old-test-key", MaxTokens: 2048}
	if err := repo.SaveAIProviderConfig(context.Background(), previous); err != nil {
		t.Fatal(err)
	}
	runtime := &providerConfigTestRuntime{config: previous}
	workflow := &providerConfigTestWorkflow{}
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AI = runtime
	api := New(config.Config{DevMode: true}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	api.SetAIProviderRuntime(runtime)
	api.SetAIProviderStore(failingProviderStore{repo})
	api.SetAIWorkflowProvider(workflow)
	server := newTestHTTPServer(api)
	defer server.Close()
	token, err := api.auth.IssueWithVersion("admin", "tenant-a", "admin", api.adminSessionVersion(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	result := requestJSON(t, server.Client(), http.MethodPut, server.URL+"/api/v1/ai/providers/config", token, map[string]any{"provider": "deepseek", "baseUrl": "https://api.deepseek.com", "model": "new-model", "apiKey": "new-test-key"}, http.StatusInternalServerError)
	if detail, _ := result["detail"].(string); !strings.Contains(detail, "原配置继续生效") {
		t.Fatalf("detail=%q", detail)
	}
	if runtime.CurrentConfig() != previous {
		t.Fatalf("runtime kept the unsaved provider: %+v", runtime.CurrentConfig())
	}
	if len(workflow.updates) != 2 || workflow.updates[0].Model != "new-model" || workflow.updates[1] != previous {
		t.Fatalf("Harness was not restored: %+v", workflow.updates)
	}
}

// Heartbeat comments keep a quiet stream open; they share the writer with
// events and stop before the handler returns.
func TestSSEWriterInterleavesHeartbeatsAndStopsOnClose(t *testing.T) {
	recorder := httptest.NewRecorder()
	stream := newSSEWriter(recorder, recorder)
	go stream.heartbeat(context.Background(), 5*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	if err := stream.event(ports.AIWorkflowEvent{Type: "run.started", RunID: "run-1"}); err != nil {
		t.Fatal(err)
	}
	stream.close()
	body := recorder.Body.String()
	if !strings.Contains(body, ": keepalive\n\n") || !strings.Contains(body, "event: run.started\n") {
		t.Fatalf("stream lacks heartbeat or event: %q", body)
	}
	time.Sleep(20 * time.Millisecond)
	if recorder.Body.String() != body {
		t.Fatal("heartbeat wrote after close")
	}
}

// Chat runs tell the model the same knowledge outcome as business runs: an
// empty match under allow-model must be stated as missing evidence.
func TestChatRunStatesMissingKnowledgeEvidence(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	runtime := &captureWorkflowRuntime{}
	engine.AIWorkflows = runtime
	engine.KB = knowledge.NewLocal()
	cfg := config.Load()
	cfg.JWTSecret = "test-secret-at-least-32-characters"
	engine.HarnessTokens = harnessTokens(cfg)
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token, err := api.auth.IssueWithVersion("admin", "tenant-a", "admin", api.adminSessionVersion(), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/chat", token, map[string]any{"question": "烟感告警怎么处置", "workflowId": "ops-assistant"}, http.StatusOK)
	runtime.mu.Lock()
	question := runtime.requests[len(runtime.requests)-1].Question
	runtime.mu.Unlock()
	if !strings.Contains(question, aiprompt.KnowledgeNoMatch) {
		t.Fatalf("chat prompt does not state missing evidence: %q", question)
	}
}

// Chat turns are saved to the signed-in user's conversation; other users and
// tenants never see them, and the owner can delete them.
func TestChatConversationsBelongToTheUser(t *testing.T) {
	repo := memory.NewRepository()
	archive, err := local.NewArchive(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	engine := core.New(devicescope.Wrap(repo), archive, local.NewBus(), local.NewRealtime(), parser.NewRegistry(parser.JSONParser{}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	engine.AIWorkflows = &captureWorkflowRuntime{}
	engine.AIConversations = repo
	cfg := config.Load()
	cfg.JWTSecret = "test-secret-at-least-32-characters"
	engine.HarnessTokens = harnessTokens(cfg)
	api := New(cfg, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	token := func(user, tenant string) string {
		t.Helper()
		v, err := api.auth.IssueWithVersion(user, tenant, "admin", api.adminSessionVersion(), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	alice := token("admin", "tenant-a")
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/chat", alice, map[string]any{"question": "今天有哪些告警需要处理", "workflowId": "ops-assistant", "conversationId": "conversation_1"}, http.StatusOK)
	requestJSON(t, server.Client(), http.MethodPost, server.URL+"/api/v1/ai/chat", alice, map[string]any{"question": "第二个问题", "workflowId": "ops-assistant", "conversationId": "conversation_1"}, http.StatusOK)
	list := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/conversations?workflowId=ops-assistant", alice, nil, http.StatusOK)
	items := list["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["title"] != "今天有哪些告警需要处理" || items[0].(map[string]any)["messageCount"] != float64(4) {
		t.Fatalf("conversation list %#v", list)
	}
	detail := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/conversations/conversation_1", alice, nil, http.StatusOK)
	if messages := detail["messages"].([]any); len(messages) != 4 || messages[2].(map[string]any)["text"] != "第二个问题" || messages[3].(map[string]any)["role"] != "assistant" {
		t.Fatalf("conversation messages %#v", detail)
	}
	other := token("admin", "tenant-b")
	if items := requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/conversations?workflowId=ops-assistant", other, nil, http.StatusOK)["items"].([]any); len(items) != 0 {
		t.Fatalf("conversation leaked across tenants: %#v", items)
	}
	requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/conversations/conversation_1", other, nil, http.StatusNotFound)
	requestJSON(t, server.Client(), http.MethodDelete, server.URL+"/api/v1/ai/conversations/conversation_1", other, nil, http.StatusNotFound)
	requestJSON(t, server.Client(), http.MethodDelete, server.URL+"/api/v1/ai/conversations/conversation_1", alice, nil, http.StatusOK)
	requestJSON(t, server.Client(), http.MethodGet, server.URL+"/api/v1/ai/conversations/conversation_1", alice, nil, http.StatusNotFound)
}

// AI failures keep user-actionable reasons and map busy, missing and
// oversized cases to statuses the page can act on; other failures get a
// generic message with a reference.
func TestAIFailuresMapToActionableStatuses(t *testing.T) {
	s := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	r := httptest.NewRequest("POST", "/api/v1/ai/chat", nil)
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{ports.AIRejected(http.StatusForbidden, "无智能助手访问权限"), 403, "AI_REQUEST_REJECTED"},
		{fmt.Errorf("run: %w", ports.ErrAIWorkflowBusy), 429, "AI_BUSY"},
		{aiworkflow.ErrAIWorkflowsUnavailable, 503, "AI_UNAVAILABLE"},
		{fmt.Errorf("prompt: %w", core.ErrAIInputTooLarge), 422, "AI_INPUT_TOO_LARGE"},
		{errors.New("upstream 500"), 502, "AI_WORKFLOW_FAILED"},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		s.aiProblem(w, r, c.err)
		if w.Code != c.status || !strings.Contains(w.Body.String(), c.code) {
			t.Fatalf("%v: %d %s", c.err, w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	s.aiProblem(w, r, ports.ErrAIWorkflowBusy)
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("busy answers must say when to retry")
	}
}

type usageRuns struct {
	ports.AIRunStore
	tokens int64
}

func (u usageRuns) AIRunUsage(context.Context, ports.AIRunFilter) ([]model.AIRunUsage, error) {
	return []model.AIRunUsage{{Usage: model.AIUsage{InputTokens: u.tokens / 2, OutputTokens: u.tokens - u.tokens/2}}}, nil
}

func TestAIQuotaLimitsRunsAndDailyTokens(t *testing.T) {
	ctx := context.Background()
	engine := &core.Engine{Repo: devicescope.Wrap(memory.NewRepository()), Clock: ports.RealClock{}}
	s := New(config.Config{DevMode: true, AIRunsPerMinute: 2}, engine, metrics.New(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	quota := &aiQuota{server: s, now: time.Now}
	for i := range 2 {
		if err := quota.AdmitAIRun(ctx, "t1"); err != nil {
			t.Fatalf("run %d refused: %v", i, err)
		}
	}
	var rejected *ports.AIRequestError
	if err := quota.AdmitAIRun(ctx, "t1"); !errors.As(err, &rejected) || rejected.Status != 429 {
		t.Fatalf("third run in a minute must be refused, got %v", err)
	}
	if err := quota.AdmitAIRun(ctx, "t2"); err != nil {
		t.Fatalf("tenants have separate budgets: %v", err)
	}
	s.cfg.AIRunsPerMinute, s.cfg.AIDailyTokenBudget = 0, 1000
	engine.AIRuns = usageRuns{tokens: 999}
	if err := quota.AdmitAIRun(ctx, "t3"); err != nil {
		t.Fatalf("under the daily budget: %v", err)
	}
	quota.usage = nil
	engine.AIRuns = usageRuns{tokens: 1000}
	if err := quota.AdmitAIRun(ctx, "t3"); !errors.As(err, &rejected) || rejected.Status != 429 {
		t.Fatalf("daily budget reached must refuse, got %v", err)
	}
}
