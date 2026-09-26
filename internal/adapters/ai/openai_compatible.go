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

	"iot-platform/internal/aioutput"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

const maxAIProviderResponseBytes = 4 << 20

type OpenAICompatible struct {
	providerID, providerName, baseURL, model, apiKey string
	http                                             *http.Client
}

type openAIChatRequest struct {
	Model          string              `json:"model"`
	Messages       []map[string]string `json:"messages"`
	Stream         bool                `json:"stream"`
	ResponseFormat map[string]string   `json:"response_format,omitempty"`
}

type openAIChatResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func NewOpenAICompatible(providerID, providerName, baseURL, model, apiKey string) (*OpenAICompatible, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("AI provider base URL must be an absolute HTTP(S) URL")
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("AI provider model is required")
	}
	client := &http.Client{
		Timeout: 2 * time.Minute,
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

func (o *OpenAICompatible) call(ctx context.Context, system, user string, jsonOutput bool) (string, error) {
	payload := openAIChatRequest{Model: o.model, Stream: false, Messages: []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}}}
	if jsonOutput {
		payload.ResponseFormat = map[string]string{"type": "json_object"}
	}
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

func (o *OpenAICompatible) AnalyzeAlarm(ctx context.Context, alarm model.Alarm, history []map[string]any, knowledge []string) (model.AIAnalysis, error) {
	input, _ := json.Marshal(map[string]any{"alarm": alarm, "recentHistory": history, "knowledge": knowledge})
	content, err := o.call(ctx, "你是消防物联网运维专家。只能提供分析和人工处置建议，禁止自动控制设备。输出 JSON：summary、possibleReasons、suggestions、riskLevel、confidence。", string(input), true)
	if err != nil {
		return model.AIAnalysis{}, err
	}
	return aioutput.DecodeAlarmAnalysis(content, alarm.ID, o.model)
}

func (o *OpenAICompatible) Chat(ctx context.Context, tenant, question string) (string, error) {
	return o.call(ctx, "你是消防物联网运维助手。仅依据受控平台数据回答，缺少数据时明确说明，不能直接控制设备。租户："+tenant, question, false)
}

func (o *OpenAICompatible) GenerateJSON(ctx context.Context, tenant, system, user string) (string, error) {
	if strings.TrimSpace(system) == "" {
		system = "你是消防物联网平台的结构化数据助手，只返回合法 JSON。"
	}
	return o.call(ctx, system+"\n租户："+tenant, user, true)
}

func (o *OpenAICompatible) RuleDraft(ctx context.Context, tenant, text string) (model.AlarmRule, error) {
	content, err := o.call(ctx, aioutput.RuleDraftInstructions, text, true)
	if err != nil {
		return model.AlarmRule{}, err
	}
	rule, err := aioutput.DecodeRuleDraft(content)
	if err != nil {
		return rule, err
	}
	rule.TenantID, rule.Enabled, rule.Version = tenant, false, 1
	rule.CreatedAt, rule.UpdatedAt = time.Now().UnixMilli(), time.Now().UnixMilli()
	return rule, nil
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
