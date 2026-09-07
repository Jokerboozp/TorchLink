package aiadapter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"iot-platform/internal/ports"
)

const (
	maxHarnessResponseBytes = 1 << 20
	maxHarnessEventBytes    = 64 << 10
)

type HarnessClient struct {
	mu          sync.RWMutex
	configureMu sync.Mutex
	baseURL     string
	token       string
	mcpURL      string
	model       string
	config      ports.AIPluginConfig
	client      *http.Client
}

func NewHarness(baseURL, token, mcpURL, model string, timeout time.Duration) (*HarnessClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if err := validateHTTPURL(baseURL); err != nil {
		return nil, fmt.Errorf("invalid harness URL: %w", err)
	}
	token = strings.TrimSpace(token)
	if len(token) < 32 || len(token) > 512 {
		return nil, errors.New("harness service token must contain 32 to 512 characters")
	}
	if strings.TrimSpace(mcpURL) == "" {
		return nil, errors.New("harness MCP URL is required")
	}
	if err := validateHTTPURL(mcpURL); err != nil {
		return nil, fmt.Errorf("invalid harness MCP URL: %w", err)
	}
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	return &HarnessClient{
		baseURL: baseURL,
		token:   token,
		mcpURL:  strings.TrimSpace(mcpURL),
		model:   strings.TrimSpace(model),
		config:  ports.AIPluginConfig{Provider: "ollama", Model: strings.TrimSpace(model)},
		client:  &http.Client{Timeout: timeout},
	}, nil
}

// ConfigureProvider updates the sidecar for subsequent workflow runs. The
// gateway maps non-Ollama providers to its OpenAI-compatible DeepSeek runtime,
// which also supports custom API-compatible endpoints through baseUrl.
func (h *HarnessClient) ConfigureProvider(ctx context.Context, config ports.AIPluginConfig) error {
	h.configureMu.Lock()
	defer h.configureMu.Unlock()
	provider := strings.ToLower(strings.TrimSpace(config.Provider))
	switch provider {
	case "ollama", "deepseek", "openai-compatible":
	default:
		return errors.New("AI workflow provider must be ollama, deepseek or openai-compatible")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if err := validateHTTPURL(baseURL); err != nil {
		return fmt.Errorf("invalid AI workflow provider URL: %w", err)
	}
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil || parsedBaseURL.RawQuery != "" || parsedBaseURL.Fragment != "" {
		return errors.New("AI workflow provider URL must not contain query parameters or fragments")
	}
	if provider == "ollama" && parsedBaseURL.Path == "/v1" {
		// The direct Ollama adapter uses the native /api endpoints. Accept the
		// common OpenAI-compatible suffix in the UI and normalize it before
		// retaining the selected configuration.
		parsedBaseURL.Path = ""
		parsedBaseURL.RawPath = ""
		baseURL = strings.TrimRight(parsedBaseURL.String(), "/")
	}
	model := strings.TrimSpace(config.Model)
	if model == "" {
		return errors.New("AI workflow model is required")
	}
	sidecarProvider := "ollama"
	apiKey := strings.TrimSpace(config.APIKey)
	if provider != "ollama" {
		sidecarProvider = "deepseek-official"
		if apiKey == "" {
			return errors.New("AI workflow API key is required")
		}
	}
	config.Provider = provider
	config.BaseURL = baseURL
	config.Model = model
	config.APIKey = apiKey
	// Harness uses the OpenAI-compatible endpoint while the direct Ollama
	// client uses its native API. Keep the user-selected endpoint in h.config
	// and add the compatibility path only for the sidecar request.
	sidecarBaseURL := baseURL
	if provider == "ollama" {
		parsedHost := ""
		if parsed, parseErr := url.Parse(baseURL); parseErr == nil {
			parsedHost = strings.ToLower(parsed.Hostname())
		}
		// A loopback URL points at the API host from the user's perspective;
		// inside Compose the equivalent service is named ollama. Users who run
		// Ollama on another host can enter that host explicitly.
		if parsedHost == "localhost" || parsedHost == "127.0.0.1" || parsedHost == "::1" {
			sidecarBaseURL = "http://ollama:11434/v1"
		} else if !strings.HasSuffix(sidecarBaseURL, "/v1") {
			sidecarBaseURL += "/v1"
		}
	}
	payload, err := json.Marshal(map[string]string{"provider": sidecarProvider, "baseUrl": sidecarBaseURL, "model": model, "apiKey": apiKey})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, h.baseURL+"/v1/provider", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	h.authorizeService(req)
	res, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("configure AI workflow provider: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("configure AI workflow provider: status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	h.mu.Lock()
	h.model = model
	h.config = config
	h.mu.Unlock()
	return nil
}

func (h *HarnessClient) CurrentConfig() ports.AIPluginConfig {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.config
}

func validateHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("an absolute http(s) URL without userinfo is required")
	}
	return nil
}

func (h *HarnessClient) ListWorkflows(ctx context.Context) ([]ports.AIWorkflowPlugin, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.baseURL+"/v1/plugins", nil)
	if err != nil {
		return nil, err
	}
	h.authorize(req)
	res, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list harness workflows: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return nil, fmt.Errorf("list harness workflows: status %d", res.StatusCode)
	}
	var envelope struct {
		Items   []ports.AIWorkflowPlugin `json:"items"`
		Plugins []ports.AIWorkflowPlugin `json:"plugins"`
	}
	limited := io.LimitReader(res.Body, maxHarnessResponseBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read harness workflows: %w", err)
	}
	if len(body) > maxHarnessResponseBytes {
		return nil, errors.New("harness workflow response exceeds 1 MiB")
	}
	var items []ports.AIWorkflowPlugin
	if err = json.Unmarshal(body, &items); err != nil {
		if err = json.Unmarshal(body, &envelope); err != nil {
			return nil, fmt.Errorf("decode harness workflows: %w", err)
		}
		items = envelope.Items
		if items == nil {
			items = envelope.Plugins
		}
	}
	if items == nil {
		items = []ports.AIWorkflowPlugin{}
	}
	return items, nil
}

// ListWorkflowManifests is the administration view of the Harness catalog.
// Unlike ListWorkflows it includes disabled plugins and the private manifest
// fields needed to edit an Agent (persona and allowedTools).
func (h *HarnessClient) ListWorkflowManifests(ctx context.Context) ([]ports.AIWorkflowManifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.baseURL+"/v1/plugins/admin", nil)
	if err != nil {
		return nil, err
	}
	h.authorizeService(req)
	res, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list harness workflow manifests: %w", err)
	}
	defer res.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(res.Body, maxHarnessResponseBytes+1))
	if readErr != nil {
		return nil, fmt.Errorf("read harness workflow manifests: %w", readErr)
	}
	if len(body) > maxHarnessResponseBytes {
		return nil, errors.New("harness workflow manifest response exceeds 1 MiB")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("list harness workflow manifests: status %d", res.StatusCode)
	}
	var envelope struct {
		Items []ports.AIWorkflowManifest `json:"items"`
	}
	var items []ports.AIWorkflowManifest
	if err = json.Unmarshal(body, &items); err != nil {
		if err = json.Unmarshal(body, &envelope); err != nil {
			return nil, fmt.Errorf("decode harness workflow manifests: %w", err)
		}
		items = envelope.Items
	}
	if items == nil {
		items = []ports.AIWorkflowManifest{}
	}
	return items, nil
}

func (h *HarnessClient) SaveWorkflow(ctx context.Context, manifest ports.AIWorkflowManifest) (ports.AIWorkflowPlugin, error) {
	var plugin ports.AIWorkflowPlugin
	payload, err := json.Marshal(manifest)
	if err != nil {
		return plugin, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.baseURL+"/v1/plugins", bytes.NewReader(payload))
	if err != nil {
		return plugin, err
	}
	req.Header.Set("Content-Type", "application/json")
	h.authorizeService(req)
	res, err := h.client.Do(req)
	if err != nil {
		return plugin, fmt.Errorf("save harness workflow: %w", err)
	}
	defer res.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(res.Body, maxHarnessResponseBytes+1))
	if readErr != nil {
		return plugin, readErr
	}
	if len(body) > maxHarnessResponseBytes {
		return plugin, errors.New("harness workflow response exceeds 1 MiB")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return plugin, fmt.Errorf("save harness workflow: status %d", res.StatusCode)
	}
	if err = json.Unmarshal(body, &plugin); err != nil {
		return plugin, fmt.Errorf("decode saved harness workflow: %w", err)
	}
	return plugin, nil
}

func (h *HarnessClient) DeleteWorkflow(ctx context.Context, workflowID string) error {
	workflowID = strings.TrimSpace(workflowID)
	if workflowID == "" {
		return errors.New("workflow id is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, h.baseURL+"/v1/plugins/"+url.PathEscape(workflowID), nil)
	if err != nil {
		return err
	}
	h.authorizeService(req)
	res, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("delete harness workflow: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return fmt.Errorf("delete harness workflow: status %d", res.StatusCode)
	}
	return nil
}

func (h *HarnessClient) StreamChat(ctx context.Context, in ports.AIWorkflowRequest, emit func(ports.AIWorkflowEvent) error) (ports.AIWorkflowResult, error) {
	if strings.TrimSpace(in.RunID) == "" || strings.TrimSpace(in.Question) == "" || strings.TrimSpace(in.MCPToken) == "" {
		return ports.AIWorkflowResult{}, errors.New("runId, question and MCP token are required")
	}
	if in.WorkflowID == "" {
		in.WorkflowID = "ops-assistant"
	}
	if in.ConversationID == "" {
		in.ConversationID = in.RunID
	}
	h.mu.RLock()
	configuredModel := h.model
	h.mu.RUnlock()
	if configuredModel != "" {
		// The provider selected in the platform settings is authoritative for
		// every workflow; ignore stale per-run model overrides from the UI.
		in.Model = configuredModel
	} else if in.Model == "" {
		in.Model = configuredModel
	}
	if in.MCPURL == "" {
		in.MCPURL = h.mcpURL
	}
	if in.MaxTokens <= 0 {
		in.MaxTokens = 2048
	}
	if in.MaxTokens > 8192 {
		in.MaxTokens = 8192
	}
	// Keep the short-lived MCP credential out of the JSON payload. The sidecar
	// receives it only as Authorization and can forward it to the MCP bridge.
	payload, err := json.Marshal(struct {
		RunID          string `json:"runId"`
		ConversationID string `json:"conversationId"`
		WorkflowID     string `json:"workflowId"`
		Question       string `json:"question"`
		MCPURL         string `json:"mcpUrl"`
		Model          string `json:"model"`
		MaxTokens      int    `json:"maxTokens"`
	}{in.RunID, in.ConversationID, in.WorkflowID, in.Question, in.MCPURL, in.Model, in.MaxTokens})
	if err != nil {
		return ports.AIWorkflowResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.baseURL+"/v1/chat/stream", bytes.NewReader(payload))
	if err != nil {
		return ports.AIWorkflowResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-ndjson")
	req.Header.Set("Authorization", "Bearer "+in.MCPToken)
	h.authorizeService(req)
	res, err := h.client.Do(req)
	if err != nil {
		return ports.AIWorkflowResult{}, fmt.Errorf("run harness workflow: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return ports.AIWorkflowResult{}, fmt.Errorf("run harness workflow: status %d", res.StatusCode)
	}

	result := ports.AIWorkflowResult{RunID: in.RunID, WorkflowID: in.WorkflowID, Model: in.Model}
	var streamed strings.Builder
	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(make([]byte, 4096), maxHarnessEventBytes)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var event ports.AIWorkflowEvent
		if err = json.Unmarshal(line, &event); err != nil {
			return result, fmt.Errorf("decode harness event: %w", err)
		}
		if !AllowedWorkflowEvent(event.Type) {
			return result, fmt.Errorf("unsupported harness event type %q", event.Type)
		}
		if event.RunID == "" {
			event.RunID = in.RunID
		}
		if event.WorkflowID == "" {
			event.WorkflowID = in.WorkflowID
		}
		if event.Model == "" {
			event.Model = in.Model
		}
		if event.Type == "text.delta" {
			if event.Delta != "" {
				streamed.WriteString(event.Delta)
			} else {
				streamed.WriteString(event.Text)
			}
		}
		if event.Answer != "" && event.Type == "run.completed" {
			result.Answer = event.Answer
		}
		if emit != nil {
			if err = emit(event); err != nil {
				return result, err
			}
		}
		if event.Type == "run.failed" {
			return result, errors.New("harness workflow failed")
		}
	}
	if err = scanner.Err(); err != nil {
		return result, fmt.Errorf("read harness event stream: %w", err)
	}
	if result.Answer == "" {
		result.Answer = streamed.String()
	}
	return result, nil
}

func (h *HarnessClient) Health(ctx context.Context) error {
	_, err := h.ListWorkflows(ctx)
	return err
}

func (h *HarnessClient) authorize(req *http.Request) {
	if h.token != "" {
		req.Header.Set("X-IOT-Harness-Token", h.token)
	}
}

func (h *HarnessClient) authorizeService(req *http.Request) { h.authorize(req) }

func AllowedWorkflowEvent(eventType string) bool {
	switch eventType {
	case "run.started", "text.delta", "tool.started", "tool.completed", "run.completed", "run.failed":
		return true
	default:
		return false
	}
}

var _ ports.AIWorkflowRuntime = (*HarnessClient)(nil)
var _ ports.AIWorkflowManager = (*HarnessClient)(nil)
var _ ports.AIWorkflowAdminManager = (*HarnessClient)(nil)
var _ ports.AIWorkflowProviderRuntime = (*HarnessClient)(nil)
