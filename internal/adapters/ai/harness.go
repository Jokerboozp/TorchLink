package aiadapter /* 声明 aiadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bufio"         /* 执行当前语句并推进处理流程。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"errors"        /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"net/http"      /* 执行当前语句并推进处理流程。 */
	"net/url"       /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"sync"          /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

const ( /* 执行当前语句并推进处理流程。 */
	maxHarnessResponseBytes = 1 << 20  /* 更新 maxHarnessResponseBytes 的值。 */
	maxHarnessEventBytes    = 64 << 10 /* 更新 maxHarnessEventBytes 的值。 */
) /* 结束当前表达式或代码块。 */

type HarnessClient struct { /* 定义 HarnessClient 类型。 */
	mu          sync.RWMutex         /* 执行当前语句并推进处理流程。 */
	configureMu sync.Mutex           /* 执行当前语句并推进处理流程。 */
	baseURL     string               /* 执行当前语句并推进处理流程。 */
	token       string               /* 执行当前语句并推进处理流程。 */
	mcpURL      string               /* 执行当前语句并推进处理流程。 */
	model       string               /* 执行当前语句并推进处理流程。 */
	config      ports.AIPluginConfig /* 执行当前语句并推进处理流程。 */
	client      *http.Client         /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func NewHarness(baseURL, token, mcpURL, model string, timeout time.Duration) (*HarnessClient, error) { /* 定义 NewHarness 函数。 */
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/") /* 更新 baseURL 的值。 */
	if err := validateHTTPURL(baseURL); err != nil {             /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("invalid harness URL: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	token = strings.TrimSpace(token)         /* 更新 token 的值。 */
	if len(token) < 32 || len(token) > 512 { /* 判断条件并选择处理分支。 */
		return nil, errors.New("harness service token must contain 32 to 512 characters") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.TrimSpace(mcpURL) == "" { /* 判断条件并选择处理分支。 */
		return nil, errors.New("harness MCP URL is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := validateHTTPURL(mcpURL); err != nil { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("invalid harness MCP URL: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if timeout <= 0 { /* 判断条件并选择处理分支。 */
		timeout = 90 * time.Second /* 更新 timeout 的值。 */
	} /* 结束当前表达式或代码块。 */
	return &HarnessClient{ /* 返回当前处理结果。 */
		baseURL: baseURL,                                                                   /* 执行当前语句并推进处理流程。 */
		token:   token,                                                                     /* 执行当前语句并推进处理流程。 */
		mcpURL:  strings.TrimSpace(mcpURL),                                                 /* 执行当前语句并推进处理流程。 */
		model:   strings.TrimSpace(model),                                                  /* 执行当前语句并推进处理流程。 */
		config:  ports.AIPluginConfig{Provider: "ollama", Model: strings.TrimSpace(model)}, /* 执行当前语句并推进处理流程。 */
		client:  &http.Client{Timeout: timeout},                                            /* 执行当前语句并推进处理流程。 */
	}, nil /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

// ConfigureProvider updates the sidecar for subsequent workflow runs. The
// gateway maps non-Ollama providers to its OpenAI-compatible DeepSeek runtime,
// which also supports custom API-compatible endpoints through baseUrl.
func (h *HarnessClient) ConfigureProvider(ctx context.Context, config ports.AIPluginConfig) error { /* 定义 ConfigureProvider 函数。 */
	h.configureMu.Lock()                                            /* 执行当前语句并推进处理流程。 */
	defer h.configureMu.Unlock()                                    /* 安排函数结束时执行清理。 */
	provider := strings.ToLower(strings.TrimSpace(config.Provider)) /* 更新 provider 的值。 */
	switch provider {                                               /* 根据条件选择处理路径。 */
	case "ollama", "deepseek", "openai-compatible": /* 处理当前分支。 */
	default: /* 处理当前分支。 */
		return errors.New("AI workflow provider must be ollama, deepseek or openai-compatible") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/") /* 更新 baseURL 的值。 */
	if err := validateHTTPURL(baseURL); err != nil {                     /* 判断条件并选择处理分支。 */
		return fmt.Errorf("invalid AI workflow provider URL: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	parsedBaseURL, err := url.Parse(baseURL)                                        /* 更新 err 的值。 */
	if err != nil || parsedBaseURL.RawQuery != "" || parsedBaseURL.Fragment != "" { /* 判断条件并选择处理分支。 */
		return errors.New("AI workflow provider URL must not contain query parameters or fragments") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if provider == "ollama" && parsedBaseURL.Path == "/v1" { /* 判断条件并选择处理分支。 */
		// The direct Ollama adapter uses the native /api endpoints. Accept the
		// common OpenAI-compatible suffix in the UI and normalize it before
		// retaining the selected configuration.
		parsedBaseURL.Path = ""                                  /* 更新 parsedBaseURL.Path 的值。 */
		parsedBaseURL.RawPath = ""                               /* 更新 parsedBaseURL.RawPath 的值。 */
		baseURL = strings.TrimRight(parsedBaseURL.String(), "/") /* 更新 baseURL 的值。 */
	} /* 结束当前表达式或代码块。 */
	model := strings.TrimSpace(config.Model) /* 更新 model 的值。 */
	if model == "" {                         /* 判断条件并选择处理分支。 */
		return errors.New("AI workflow model is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	sidecarProvider := "ollama"                /* 更新 sidecarProvider 的值。 */
	apiKey := strings.TrimSpace(config.APIKey) /* 更新 apiKey 的值。 */
	if provider != "ollama" {                  /* 判断条件并选择处理分支。 */
		sidecarProvider = "deepseek-official" /* 更新 sidecarProvider 的值。 */
		if apiKey == "" {                     /* 判断条件并选择处理分支。 */
			return errors.New("AI workflow API key is required") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	config.Provider = provider /* 更新 config.Provider 的值。 */
	config.BaseURL = baseURL   /* 更新 config.BaseURL 的值。 */
	config.Model = model       /* 更新 config.Model 的值。 */
	config.APIKey = apiKey     /* 更新 config.APIKey 的值。 */
	// Harness uses the OpenAI-compatible endpoint while the direct Ollama
	// client uses its native API. Keep the user-selected endpoint in h.config
	// and add the compatibility path only for the sidecar request.
	sidecarBaseURL := baseURL /* 更新 sidecarBaseURL 的值。 */
	if provider == "ollama" { /* 判断条件并选择处理分支。 */
		parsedHost := ""                                             /* 更新 parsedHost 的值。 */
		if parsed, parseErr := url.Parse(baseURL); parseErr == nil { /* 判断条件并选择处理分支。 */
			parsedHost = strings.ToLower(parsed.Hostname()) /* 更新 parsedHost 的值。 */
		} /* 结束当前表达式或代码块。 */
		// A loopback URL points at the API host from the user's perspective;
		// inside Compose the equivalent service is named ollama. Users who run
		// Ollama on another host can enter that host explicitly.
		if parsedHost == "localhost" || parsedHost == "127.0.0.1" || parsedHost == "::1" { /* 判断条件并选择处理分支。 */
			sidecarBaseURL = "http://ollama:11434/v1" /* 更新 sidecarBaseURL 的值。 */
		} else if !strings.HasSuffix(sidecarBaseURL, "/v1") { /* 结束当前表达式或代码块。 */
			sidecarBaseURL += "/v1" /* 更新 sidecarBaseURL 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	payload, err := json.Marshal(map[string]string{"provider": sidecarProvider, "baseUrl": sidecarBaseURL, "model": model, "apiKey": apiKey}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                           /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, h.baseURL+"/v1/provider", bytes.NewReader(payload)) /* 更新 err 的值。 */
	if err != nil {                                                                                                 /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	req.Header.Set("Content-Type", "application/json") /* 执行当前语句并推进处理流程。 */
	h.authorizeService(req)                            /* 执行当前语句并推进处理流程。 */
	res, err := h.client.Do(req)                       /* 更新 err 的值。 */
	if err != nil {                                    /* 判断条件并选择处理分支。 */
		return fmt.Errorf("configure AI workflow provider: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer res.Body.Close()                             /* 安排函数结束时执行清理。 */
	if res.StatusCode < 200 || res.StatusCode >= 300 { /* 判断条件并选择处理分支。 */
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))                                                               /* 更新 _ 的值。 */
		return fmt.Errorf("configure AI workflow provider: status %d: %s", res.StatusCode, strings.TrimSpace(string(body))) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	h.mu.Lock()       /* 执行当前语句并推进处理流程。 */
	h.model = model   /* 更新 h.model 的值。 */
	h.config = config /* 更新 h.config 的值。 */
	h.mu.Unlock()     /* 执行当前语句并推进处理流程。 */
	return nil        /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (h *HarnessClient) CurrentConfig() ports.AIPluginConfig { /* 定义 CurrentConfig 函数。 */
	h.mu.RLock()         /* 执行当前语句并推进处理流程。 */
	defer h.mu.RUnlock() /* 安排函数结束时执行清理。 */
	return h.config      /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func validateHTTPURL(raw string) error { /* 定义 validateHTTPURL 函数。 */
	u, err := url.Parse(raw)                                                                        /* 更新 err 的值。 */
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") { /* 判断条件并选择处理分支。 */
		return errors.New("an absolute http(s) URL without userinfo is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (h *HarnessClient) ListWorkflows(ctx context.Context) ([]ports.AIWorkflowPlugin, error) { /* 定义 ListWorkflows 函数。 */
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.baseURL+"/v1/plugins", nil) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                           /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	h.authorize(req)             /* 执行当前语句并推进处理流程。 */
	res, err := h.client.Do(req) /* 更新 err 的值。 */
	if err != nil {              /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("list harness workflows: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer res.Body.Close()                             /* 安排函数结束时执行清理。 */
	if res.StatusCode < 200 || res.StatusCode >= 300 { /* 判断条件并选择处理分支。 */
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))                  /* 更新 _ 的值。 */
		return nil, fmt.Errorf("list harness workflows: status %d", res.StatusCode) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var envelope struct { /* 声明 envelope。 */
		Items   []ports.AIWorkflowPlugin `json:"items"`   /* 执行当前语句并推进处理流程。 */
		Plugins []ports.AIWorkflowPlugin `json:"plugins"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	limited := io.LimitReader(res.Body, maxHarnessResponseBytes+1) /* 更新 limited 的值。 */
	body, err := io.ReadAll(limited)                               /* 更新 err 的值。 */
	if err != nil {                                                /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("read harness workflows: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(body) > maxHarnessResponseBytes { /* 判断条件并选择处理分支。 */
		return nil, errors.New("harness workflow response exceeds 1 MiB") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var items []ports.AIWorkflowPlugin                  /* 声明 items。 */
	if err = json.Unmarshal(body, &items); err != nil { /* 判断条件并选择处理分支。 */
		if err = json.Unmarshal(body, &envelope); err != nil { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("decode harness workflows: %w", err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		items = envelope.Items /* 更新 items 的值。 */
		if items == nil {      /* 判断条件并选择处理分支。 */
			items = envelope.Plugins /* 更新 items 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if items == nil { /* 判断条件并选择处理分支。 */
		items = []ports.AIWorkflowPlugin{} /* 更新 items 的值。 */
	} /* 结束当前表达式或代码块。 */
	return items, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// ListWorkflowManifests is the administration view of the Harness catalog.
// Unlike ListWorkflows it includes disabled plugins and the private manifest
// fields needed to edit an Agent (persona and allowedTools).
func (h *HarnessClient) ListWorkflowManifests(ctx context.Context) ([]ports.AIWorkflowManifest, error) { /* 定义 ListWorkflowManifests 函数。 */
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.baseURL+"/v1/plugins/admin", nil) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                                 /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	h.authorizeService(req)      /* 执行当前语句并推进处理流程。 */
	res, err := h.client.Do(req) /* 更新 err 的值。 */
	if err != nil {              /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("list harness workflow manifests: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer res.Body.Close()                                                           /* 安排函数结束时执行清理。 */
	body, readErr := io.ReadAll(io.LimitReader(res.Body, maxHarnessResponseBytes+1)) /* 更新 readErr 的值。 */
	if readErr != nil {                                                              /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("read harness workflow manifests: %w", readErr) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(body) > maxHarnessResponseBytes { /* 判断条件并选择处理分支。 */
		return nil, errors.New("harness workflow manifest response exceeds 1 MiB") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if res.StatusCode < 200 || res.StatusCode >= 300 { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("list harness workflow manifests: status %d", res.StatusCode) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var envelope struct { /* 声明 envelope。 */
		Items []ports.AIWorkflowManifest `json:"items"` /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	var items []ports.AIWorkflowManifest                /* 声明 items。 */
	if err = json.Unmarshal(body, &items); err != nil { /* 判断条件并选择处理分支。 */
		if err = json.Unmarshal(body, &envelope); err != nil { /* 判断条件并选择处理分支。 */
			return nil, fmt.Errorf("decode harness workflow manifests: %w", err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		items = envelope.Items /* 更新 items 的值。 */
	} /* 结束当前表达式或代码块。 */
	if items == nil { /* 判断条件并选择处理分支。 */
		items = []ports.AIWorkflowManifest{} /* 更新 items 的值。 */
	} /* 结束当前表达式或代码块。 */
	return items, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (h *HarnessClient) SaveWorkflow(ctx context.Context, manifest ports.AIWorkflowManifest) (ports.AIWorkflowPlugin, error) { /* 定义 SaveWorkflow 函数。 */
	var plugin ports.AIWorkflowPlugin      /* 声明 plugin。 */
	payload, err := json.Marshal(manifest) /* 更新 err 的值。 */
	if err != nil {                        /* 判断条件并选择处理分支。 */
		return plugin, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.baseURL+"/v1/plugins", bytes.NewReader(payload)) /* 更新 err 的值。 */
	if err != nil {                                                                                                 /* 判断条件并选择处理分支。 */
		return plugin, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	req.Header.Set("Content-Type", "application/json") /* 执行当前语句并推进处理流程。 */
	h.authorizeService(req)                            /* 执行当前语句并推进处理流程。 */
	res, err := h.client.Do(req)                       /* 更新 err 的值。 */
	if err != nil {                                    /* 判断条件并选择处理分支。 */
		return plugin, fmt.Errorf("save harness workflow: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer res.Body.Close()                                                           /* 安排函数结束时执行清理。 */
	body, readErr := io.ReadAll(io.LimitReader(res.Body, maxHarnessResponseBytes+1)) /* 更新 readErr 的值。 */
	if readErr != nil {                                                              /* 判断条件并选择处理分支。 */
		return plugin, readErr /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(body) > maxHarnessResponseBytes { /* 判断条件并选择处理分支。 */
		return plugin, errors.New("harness workflow response exceeds 1 MiB") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if res.StatusCode < 200 || res.StatusCode >= 300 { /* 判断条件并选择处理分支。 */
		return plugin, fmt.Errorf("save harness workflow: status %d", res.StatusCode) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err = json.Unmarshal(body, &plugin); err != nil { /* 判断条件并选择处理分支。 */
		return plugin, fmt.Errorf("decode saved harness workflow: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return plugin, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (h *HarnessClient) DeleteWorkflow(ctx context.Context, workflowID string) error { /* 定义 DeleteWorkflow 函数。 */
	workflowID = strings.TrimSpace(workflowID) /* 更新 workflowID 的值。 */
	if workflowID == "" {                      /* 判断条件并选择处理分支。 */
		return errors.New("workflow id is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, h.baseURL+"/v1/plugins/"+url.PathEscape(workflowID), nil) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                                                          /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	h.authorizeService(req)      /* 执行当前语句并推进处理流程。 */
	res, err := h.client.Do(req) /* 更新 err 的值。 */
	if err != nil {              /* 判断条件并选择处理分支。 */
		return fmt.Errorf("delete harness workflow: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer res.Body.Close()                             /* 安排函数结束时执行清理。 */
	if res.StatusCode < 200 || res.StatusCode >= 300 { /* 判断条件并选择处理分支。 */
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))              /* 更新 _ 的值。 */
		return fmt.Errorf("delete harness workflow: status %d", res.StatusCode) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (h *HarnessClient) StreamChat(ctx context.Context, in ports.AIWorkflowRequest, emit func(ports.AIWorkflowEvent) error) (ports.AIWorkflowResult, error) { /* 定义 StreamChat 函数。 */
	if strings.TrimSpace(in.RunID) == "" || strings.TrimSpace(in.Question) == "" || strings.TrimSpace(in.MCPToken) == "" { /* 判断条件并选择处理分支。 */
		return ports.AIWorkflowResult{}, errors.New("runId, question and MCP token are required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if in.WorkflowID == "" { /* 判断条件并选择处理分支。 */
		in.WorkflowID = "ops-assistant" /* 更新 in.WorkflowID 的值。 */
	} /* 结束当前表达式或代码块。 */
	if in.ConversationID == "" { /* 判断条件并选择处理分支。 */
		in.ConversationID = in.RunID /* 更新 in.ConversationID 的值。 */
	} /* 结束当前表达式或代码块。 */
	h.mu.RLock()               /* 执行当前语句并推进处理流程。 */
	configuredModel := h.model /* 更新 configuredModel 的值。 */
	h.mu.RUnlock()             /* 执行当前语句并推进处理流程。 */
	if configuredModel != "" { /* 判断条件并选择处理分支。 */
		// The provider selected in the platform settings is authoritative for
		// every workflow; ignore stale per-run model overrides from the UI.
		in.Model = configuredModel /* 更新 in.Model 的值。 */
	} else if in.Model == "" { /* 结束当前表达式或代码块。 */
		in.Model = configuredModel /* 更新 in.Model 的值。 */
	} /* 结束当前表达式或代码块。 */
	if in.MCPURL == "" { /* 判断条件并选择处理分支。 */
		in.MCPURL = h.mcpURL /* 更新 in.MCPURL 的值。 */
	} /* 结束当前表达式或代码块。 */
	if in.MaxTokens <= 0 { /* 判断条件并选择处理分支。 */
		in.MaxTokens = 2048 /* 更新 in.MaxTokens 的值。 */
	} /* 结束当前表达式或代码块。 */
	if in.MaxTokens > 8192 { /* 判断条件并选择处理分支。 */
		in.MaxTokens = 8192 /* 更新 in.MaxTokens 的值。 */
	} /* 结束当前表达式或代码块。 */
	// Keep the short-lived MCP credential out of the JSON payload. The sidecar
	// receives it only as Authorization and can forward it to the MCP bridge.
	payload, err := json.Marshal(struct { /* 更新 err 的值。 */
		RunID          string `json:"runId"`          /* 执行当前语句并推进处理流程。 */
		ConversationID string `json:"conversationId"` /* 执行当前语句并推进处理流程。 */
		WorkflowID     string `json:"workflowId"`     /* 执行当前语句并推进处理流程。 */
		Question       string `json:"question"`       /* 执行当前语句并推进处理流程。 */
		MCPURL         string `json:"mcpUrl"`         /* 执行当前语句并推进处理流程。 */
		Model          string `json:"model"`          /* 执行当前语句并推进处理流程。 */
		MaxTokens      int    `json:"maxTokens"`      /* 执行当前语句并推进处理流程。 */
	}{in.RunID, in.ConversationID, in.WorkflowID, in.Question, in.MCPURL, in.Model, in.MaxTokens}) /* 结束当前表达式或代码块。 */
	if err != nil { /* 判断条件并选择处理分支。 */
		return ports.AIWorkflowResult{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.baseURL+"/v1/chat/stream", bytes.NewReader(payload)) /* 更新 err 的值。 */
	if err != nil {                                                                                                     /* 判断条件并选择处理分支。 */
		return ports.AIWorkflowResult{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	req.Header.Set("Content-Type", "application/json")     /* 执行当前语句并推进处理流程。 */
	req.Header.Set("Accept", "application/x-ndjson")       /* 执行当前语句并推进处理流程。 */
	req.Header.Set("Authorization", "Bearer "+in.MCPToken) /* 执行当前语句并推进处理流程。 */
	h.authorizeService(req)                                /* 执行当前语句并推进处理流程。 */
	res, err := h.client.Do(req)                           /* 更新 err 的值。 */
	if err != nil {                                        /* 判断条件并选择处理分支。 */
		return ports.AIWorkflowResult{}, fmt.Errorf("run harness workflow: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer res.Body.Close()                             /* 安排函数结束时执行清理。 */
	if res.StatusCode < 200 || res.StatusCode >= 300 { /* 判断条件并选择处理分支。 */
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096)) /* 更新 _ 的值。 */
		if res.StatusCode == http.StatusTooManyRequests {
			return ports.AIWorkflowResult{}, fmt.Errorf("run harness workflow: %w", ports.ErrAIWorkflowBusy)
		}
		return ports.AIWorkflowResult{}, fmt.Errorf("run harness workflow: status %d", res.StatusCode) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */

	result := ports.AIWorkflowResult{RunID: in.RunID, WorkflowID: in.WorkflowID, Model: in.Model} /* 更新 result 的值。 */
	var streamed strings.Builder                                                                  /* 声明 streamed。 */
	scanner := bufio.NewScanner(res.Body)                                                         /* 更新 scanner 的值。 */
	scanner.Buffer(make([]byte, 4096), maxHarnessEventBytes)                                      /* 执行当前语句并推进处理流程。 */
	for scanner.Scan() {                                                                          /* 循环处理当前数据。 */
		line := bytes.TrimSpace(scanner.Bytes()) /* 更新 line 的值。 */
		if len(line) == 0 {                      /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		var event ports.AIWorkflowEvent                     /* 声明 event。 */
		if err = json.Unmarshal(line, &event); err != nil { /* 判断条件并选择处理分支。 */
			return result, fmt.Errorf("decode harness event: %w", err) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if !AllowedWorkflowEvent(event.Type) { /* 判断条件并选择处理分支。 */
			return result, fmt.Errorf("unsupported harness event type %q", event.Type) /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if event.RunID == "" { /* 判断条件并选择处理分支。 */
			event.RunID = in.RunID /* 更新 event.RunID 的值。 */
		} /* 结束当前表达式或代码块。 */
		if event.WorkflowID == "" { /* 判断条件并选择处理分支。 */
			event.WorkflowID = in.WorkflowID /* 更新 event.WorkflowID 的值。 */
		} /* 结束当前表达式或代码块。 */
		if event.Model == "" { /* 判断条件并选择处理分支。 */
			event.Model = in.Model /* 更新 event.Model 的值。 */
		} /* 结束当前表达式或代码块。 */
		if event.Type == "text.delta" { /* 判断条件并选择处理分支。 */
			if event.Delta != "" { /* 判断条件并选择处理分支。 */
				streamed.WriteString(event.Delta) /* 执行当前语句并推进处理流程。 */
			} else { /* 结束当前表达式或代码块。 */
				streamed.WriteString(event.Text) /* 执行当前语句并推进处理流程。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if event.Answer != "" && event.Type == "run.completed" { /* 判断条件并选择处理分支。 */
			result.Answer = event.Answer /* 更新 result.Answer 的值。 */
		} /* 结束当前表达式或代码块。 */
		if emit != nil { /* 判断条件并选择处理分支。 */
			if err = emit(event); err != nil { /* 判断条件并选择处理分支。 */
				return result, err /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
		} /* 结束当前表达式或代码块。 */
		if event.Type == "run.failed" { /* 判断条件并选择处理分支。 */
			return result, errors.New("harness workflow failed") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if err = scanner.Err(); err != nil { /* 判断条件并选择处理分支。 */
		return result, fmt.Errorf("read harness event stream: %w", err) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if result.Answer == "" { /* 判断条件并选择处理分支。 */
		result.Answer = streamed.String() /* 更新 result.Answer 的值。 */
	} /* 结束当前表达式或代码块。 */
	return result, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (h *HarnessClient) Health(ctx context.Context) error { /* 定义 Health 函数。 */
	_, err := h.ListWorkflows(ctx) /* 更新 err 的值。 */
	return err                     /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (h *HarnessClient) authorize(req *http.Request) { /* 定义 authorize 函数。 */
	if h.token != "" { /* 判断条件并选择处理分支。 */
		req.Header.Set("X-IOT-Harness-Token", h.token) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (h *HarnessClient) authorizeService(req *http.Request) { h.authorize(req) } /* 定义 authorizeService 函数。 */

func AllowedWorkflowEvent(eventType string) bool { /* 定义 AllowedWorkflowEvent 函数。 */
	switch eventType { /* 根据条件选择处理路径。 */
	case "run.started", "text.delta", "tool.started", "tool.completed", "run.completed", "run.failed": /* 处理当前分支。 */
		return true /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return false /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

var _ ports.AIWorkflowRuntime = (*HarnessClient)(nil)         /* 声明 _。 */
var _ ports.AIWorkflowManager = (*HarnessClient)(nil)         /* 声明 _。 */
var _ ports.AIWorkflowAdminManager = (*HarnessClient)(nil)    /* 声明 _。 */
var _ ports.AIWorkflowProviderRuntime = (*HarnessClient)(nil) /* 声明 _。 */
