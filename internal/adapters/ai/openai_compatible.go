package aiadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iot-platform/internal/netguard"
	"net/http"
	"net/url"
	"strings"
	"time"

	"iot-platform/internal/ports"
)

const maxAIProviderResponseBytes = 4 << 20

type OpenAICompatible struct {
	providerID, providerName, baseURL, model, apiKey string
	http                                             *http.Client
}

type openAIChatRequest struct {
	Model    string              `json:"model"`
	Messages []map[string]string `json:"messages"`
	Stream   bool                `json:"stream"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// localHosts lists the model services the deployment runs itself
// (IOT_LOCAL_AI_HOSTS); set once at startup.
var localHosts = ports.DefaultLocalAIHosts

// SetLocalHosts replaces the list of self-hosted model services.
func SetLocalHosts(hosts string) { localHosts = hosts }

func NewOpenAICompatible(providerID, providerName, baseURL, model, apiKey string) (*OpenAICompatible, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("AI provider base URL must be an absolute HTTP(S) URL")
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("AI provider model is required")
	}
	// The API key travels with every request, so a provider across the
	// internet must use HTTPS; plain HTTP is for models on the local network
	// (vLLM, Ollama, IOT_LOCAL_AI_HOSTS). Cloud metadata stays unreachable.
	if u.Scheme != "https" && !netguard.LocalHost(u.Hostname()) && !ports.LocalAIEndpoint(baseURL, localHosts) {
		return nil, fmt.Errorf("AI provider base URL must use HTTPS unless it is on the local network")
	}
	transport := netguard.PrivateNetworks.Transport()
	client := &http.Client{
		Timeout:   2 * time.Minute,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many AI provider redirects")
			}
			if !strings.EqualFold(req.URL.Scheme, u.Scheme) || !strings.EqualFold(req.URL.Host, u.Host) {
				return fmt.Errorf("cross-origin AI provider redirect rejected")
			}
			return nil
		},
	}
	return &OpenAICompatible{providerID: providerID, providerName: providerName, baseURL: baseURL, model: model, apiKey: strings.TrimSpace(apiKey), http: client}, nil
}

func (o *OpenAICompatible) call(ctx context.Context, system, user string) (string, error) {
	payload := openAIChatRequest{Model: o.model, Stream: false, Messages: []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}}}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("%s request could not be created", o.providerName)
	}
	req.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	resp, err := o.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("%s request failed", o.providerName)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("%s request failed with HTTP %d", o.providerName, resp.StatusCode)
	}
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxAIProviderResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("%s response could not be read", o.providerName)
	}
	if len(responseBody) > maxAIProviderResponseBytes {
		return "", fmt.Errorf("%s response exceeded the size limit", o.providerName)
	}
	var out openAIChatResponse
	if err := json.Unmarshal(responseBody, &out); err != nil {
		return "", fmt.Errorf("%s returned an invalid response", o.providerName)
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("%s returned an empty response", o.providerName)
	}
	return out.Choices[0].Message.Content, nil
}

func (o *OpenAICompatible) Chat(ctx context.Context, tenant, question string) (string, error) {
	return o.call(ctx, "你是消防物联网运维助手。仅依据受控平台数据回答，缺少数据时明确说明，不能直接控制设备。租户："+tenant, question)
}

func (o *OpenAICompatible) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+"/models", nil)
	if err != nil {
		return fmt.Errorf("%s health check could not be created", o.providerName)
	}
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	resp, err := o.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%s health check failed", o.providerName)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s %s", o.providerName, resp.Status)
	}
	return nil
}

func (o *OpenAICompatible) ProviderInfo() ports.AIPluginInfo {
	return ports.AIPluginInfo{ID: o.providerID, Name: o.providerName, Description: "OpenAI-compatible model provider plugin", DefaultBaseURL: o.baseURL, DefaultModel: o.model, Model: o.model, RequiresAPIKey: o.providerID == "deepseek", Enabled: true, Capabilities: []string{"chat", "alarm-analysis", "rule-draft", "json-output"}}
}

var _ ports.AIClient = (*OpenAICompatible)(nil)
var _ ports.AIInspectable = (*OpenAICompatible)(nil)
