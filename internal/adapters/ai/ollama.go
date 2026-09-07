package aiadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type Ollama struct {
	baseURL, model string
	http           *http.Client
}

func NewOllama(baseURL, model string) (*Ollama, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("Ollama base URL must be an absolute HTTP(S) URL")
	}
	if u.Path == "/v1" {
		// Ollama's native API lives at /api; users often copy an OpenAI
		// compatible /v1 base URL, so accept and normalize that suffix.
		u.Path = ""
		u.RawPath = ""
		baseURL = strings.TrimRight(u.String(), "/")
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("Ollama model is required")
	}
	client := &http.Client{
		Timeout: 2 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many Ollama redirects")
			}
			if !strings.EqualFold(req.URL.Scheme, u.Scheme) || !strings.EqualFold(req.URL.Host, u.Host) {
				return fmt.Errorf("cross-origin Ollama redirect rejected")
			}
			return nil
		},
	}
	return &Ollama{baseURL, strings.TrimSpace(model), client}, nil
}

type chatRequest struct {
	Model    string              `json:"model"`
	Stream   bool                `json:"stream"`
	Format   any                 `json:"format,omitempty"`
	Messages []map[string]string `json:"messages"`
}
type chatResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
}

func (o *Ollama) call(ctx context.Context, system, user string) (string, error) {
	body, err := json.Marshal(chatRequest{Model: o.model, Stream: false, Messages: []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}}})
	if err != nil {
		return "", fmt.Errorf("Ollama request could not be encoded")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("Ollama request could not be created")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("Ollama request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("ollama request failed with HTTP %d", resp.StatusCode)
	}
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxAIProviderResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("ollama response could not be read")
	}
	if len(responseBody) > maxAIProviderResponseBytes {
		return "", fmt.Errorf("ollama response exceeded the size limit")
	}
	var out chatResponse
	if err := json.Unmarshal(responseBody, &out); err != nil {
		return "", fmt.Errorf("ollama returned an invalid response")
	}
	if strings.TrimSpace(out.Message.Content) == "" {
		return "", fmt.Errorf("ollama returned an empty response")
	}
	return out.Message.Content, nil
}
func (o *Ollama) AnalyzeAlarm(ctx context.Context, a model.Alarm, history []map[string]any, knowledge []string) (model.AIAnalysis, error) {
	input, _ := json.Marshal(map[string]any{"alarm": a, "recentHistory": history, "knowledge": knowledge})
	system := "你是消防物联网运维专家。只能提供分析和人工处置建议，禁止建议自动控制设备。请严格输出 JSON：summary 字符串、possibleReasons 字符串数组、suggestions 字符串数组、riskLevel、confidence(0-1)。"
	content, err := o.call(ctx, system, string(input))
	if err != nil {
		return model.AIAnalysis{}, err
	}
	return decodeAIAnalysis(content, a.ID, o.model)
}
func (o *Ollama) Chat(ctx context.Context, tenant, question string) (string, error) {
	return o.call(ctx, "你是消防物联网运维助手。回答必须基于提供的受控平台数据；缺少数据时明确说明，不能编造，也不能直接控制设备。租户："+tenant, question)
}
func (o *Ollama) GenerateJSON(ctx context.Context, tenant, system, user string) (string, error) {
	return o.callJSON(ctx, system+"\n租户："+tenant, user)
}
func (o *Ollama) callJSON(ctx context.Context, system, user string) (string, error) {
	body, err := json.Marshal(chatRequest{Model: o.model, Stream: false, Format: "json", Messages: []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}}})
	if err != nil {
		return "", fmt.Errorf("Ollama request could not be encoded")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("Ollama request could not be created")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("Ollama request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("ollama request failed with HTTP %d", resp.StatusCode)
	}
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxAIProviderResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("Ollama response could not be read")
	}
	if len(responseBody) > maxAIProviderResponseBytes {
		return "", fmt.Errorf("Ollama response exceeded the size limit")
	}
	var out chatResponse
	if err := json.Unmarshal(responseBody, &out); err != nil {
		return "", fmt.Errorf("Ollama returned an invalid response")
	}
	if strings.TrimSpace(out.Message.Content) == "" {
		return "", fmt.Errorf("Ollama returned an empty response")
	}
	return out.Message.Content, nil
}
func (o *Ollama) RuleDraft(ctx context.Context, tenant, text string) (model.AlarmRule, error) {
	content, err := o.call(ctx, ruleDraftSystemPrompt+"租户："+tenant, text)
	if err != nil {
		return model.AlarmRule{}, err
	}
	rule, err := decodeRuleDraft(content)
	if err != nil {
		return rule, err
	}
	rule.TenantID = tenant
	rule.Enabled = false
	rule.Version = 1
	rule.CreatedAt = time.Now().UnixMilli()
	rule.UpdatedAt = rule.CreatedAt
	return rule, nil
}
func (o *Ollama) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+"/api/tags", nil)
	if err != nil {
		return fmt.Errorf("Ollama health check could not be created")
	}
	resp, err := o.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("Ollama health check failed")
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("ollama %s", resp.Status)
	}
	return nil
}
func (o *Ollama) ProviderInfo() ports.AIPluginInfo {
	return ports.AIPluginInfo{ID: "ollama", Name: "Ollama", Description: "Local Ollama model provider plugin", DefaultBaseURL: o.baseURL, DefaultModel: o.model, Model: o.model, Enabled: true, Capabilities: []string{"chat", "alarm-analysis", "rule-draft", "local-model"}}
}
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

type NoopAI struct{}

func (NoopAI) AnalyzeAlarm(_ context.Context, a model.Alarm, _ []map[string]any, _ []string) (model.AIAnalysis, error) {
	return model.AIAnalysis{AlarmID: a.ID, Summary: "AI 模型未启用，已保留告警供人工研判。", RiskLevel: a.AlarmLevel, Confidence: 0, Model: "disabled", PromptVersion: "fallback-v1", CreatedAt: time.Now().UnixMilli()}, nil
}
func (NoopAI) Chat(context.Context, string, string) (string, error) {
	return "AI 模型未启用。请配置 IOT_OLLAMA_URL 与模型后重试。", nil
}
func (NoopAI) GenerateJSON(context.Context, string, string, string) (string, error) {
	return "", fmt.Errorf("AI model disabled")
}
func (NoopAI) RuleDraft(context.Context, string, string) (model.AlarmRule, error) {
	return model.AlarmRule{}, fmt.Errorf("AI model disabled")
}
func (NoopAI) Health(context.Context) error { return nil }
func (NoopAI) ProviderInfo() ports.AIPluginInfo {
	return ports.AIPluginInfo{ID: "disabled", Name: "未启用", Description: "AI provider is disabled", Enabled: false, Capabilities: []string{"fallback"}}
}
