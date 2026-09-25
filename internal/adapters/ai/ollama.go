package aiadapter /* 声明 aiadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"bytes"         /* 执行当前语句并推进处理流程。 */
	"context"       /* 执行当前语句并推进处理流程。 */
	"encoding/json" /* 执行当前语句并推进处理流程。 */
	"fmt"           /* 执行当前语句并推进处理流程。 */
	"io"            /* 执行当前语句并推进处理流程。 */
	"net/http"      /* 执行当前语句并推进处理流程。 */
	"net/url"       /* 执行当前语句并推进处理流程。 */
	"strings"       /* 执行当前语句并推进处理流程。 */
	"time"          /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/aioutput"
	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type Ollama struct { /* 定义 Ollama 类型。 */
	baseURL, model string       /* 执行当前语句并推进处理流程。 */
	http           *http.Client /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func NewOllama(baseURL, model string) (*Ollama, error) { /* 定义 NewOllama 函数。 */
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")                                                                            /* 更新 baseURL 的值。 */
	u, err := url.Parse(baseURL)                                                                                                            /* 更新 err 的值。 */
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("Ollama base URL must be an absolute HTTP(S) URL") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if u.Path == "/v1" { /* 判断条件并选择处理分支。 */
		// Ollama's native API lives at /api; users often copy an OpenAI
		// compatible /v1 base URL, so accept and normalize that suffix.
		u.Path = ""                                  /* 更新 u.Path 的值。 */
		u.RawPath = ""                               /* 更新 u.RawPath 的值。 */
		baseURL = strings.TrimRight(u.String(), "/") /* 更新 baseURL 的值。 */
	} /* 结束当前表达式或代码块。 */
	if strings.TrimSpace(model) == "" { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("Ollama model is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	client := &http.Client{ /* 更新 client 的值。 */
		Timeout: 2 * time.Minute, /* 执行当前语句并推进处理流程。 */
		CheckRedirect: func(req *http.Request, via []*http.Request) error { /* 执行当前语句并推进处理流程。 */
			if len(via) >= 10 { /* 判断条件并选择处理分支。 */
				return fmt.Errorf("too many Ollama redirects") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if !strings.EqualFold(req.URL.Scheme, u.Scheme) || !strings.EqualFold(req.URL.Host, u.Host) { /* 判断条件并选择处理分支。 */
				return fmt.Errorf("cross-origin Ollama redirect rejected") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			return nil /* 返回当前处理结果。 */
		}, /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return &Ollama{baseURL, strings.TrimSpace(model), client}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

type chatRequest struct { /* 定义 chatRequest 类型。 */
	Model    string              `json:"model"`            /* 执行当前语句并推进处理流程。 */
	Stream   bool                `json:"stream"`           /* 执行当前语句并推进处理流程。 */
	Format   any                 `json:"format,omitempty"` /* 执行当前语句并推进处理流程。 */
	Messages []map[string]string `json:"messages"`         /* 执行当前语句并推进处理流程。 */
}                          /* 结束当前表达式或代码块。 */
type chatResponse struct { /* 定义 chatResponse 类型。 */
	Message struct { /* 执行当前语句并推进处理流程。 */
		Content string `json:"content"` /* 执行当前语句并推进处理流程。 */
	} `json:"message"` /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (o *Ollama) call(ctx context.Context, system, user string) (string, error) { /* 定义 call 函数。 */
	body, err := json.Marshal(chatRequest{Model: o.model, Stream: false, Messages: []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}}}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("Ollama request could not be encoded") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/chat", bytes.NewReader(body)) /* 更新 err 的值。 */
	if err != nil {                                                                                            /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("Ollama request could not be created") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	req.Header.Set("Content-Type", "application/json") /* 执行当前语句并推进处理流程。 */
	resp, err := o.http.Do(req)                        /* 更新 err 的值。 */
	if err != nil {                                    /* 判断条件并选择处理分支。 */
		if ctx.Err() != nil { /* 判断条件并选择处理分支。 */
			return "", ctx.Err() /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return "", fmt.Errorf("Ollama request failed") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer resp.Body.Close()       /* 安排函数结束时执行清理。 */
	if resp.StatusCode/100 != 2 { /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("ollama request failed with HTTP %d", resp.StatusCode) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxAIProviderResponseBytes+1)) /* 更新 err 的值。 */
	if err != nil {                                                                          /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("ollama response could not be read") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(responseBody) > maxAIProviderResponseBytes { /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("ollama response exceeded the size limit") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var out chatResponse                                       /* 声明 out。 */
	if err := json.Unmarshal(responseBody, &out); err != nil { /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("ollama returned an invalid response") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.TrimSpace(out.Message.Content) == "" { /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("ollama returned an empty response") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return out.Message.Content, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (o *Ollama) AnalyzeAlarm(ctx context.Context, a model.Alarm, history []map[string]any, knowledge []string) (model.AIAnalysis, error) { /* 定义 AnalyzeAlarm 函数。 */
	input, _ := json.Marshal(map[string]any{"alarm": a, "recentHistory": history, "knowledge": knowledge})                                     /* 更新 _ 的值。 */
	system := "你是消防物联网运维专家。只能提供分析和人工处置建议，禁止建议自动控制设备。请严格输出 JSON：summary 字符串、possibleReasons 字符串数组、suggestions 字符串数组、riskLevel、confidence(0-1)。" /* 更新 system 的值。 */
	content, err := o.call(ctx, system, string(input))                                                                                         /* 更新 err 的值。 */
	if err != nil {                                                                                                                            /* 判断条件并选择处理分支。 */
		return model.AIAnalysis{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return aioutput.DecodeAlarmAnalysis(content, a.ID, o.model) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (o *Ollama) Chat(ctx context.Context, tenant, question string) (string, error) { /* 定义 Chat 函数。 */
	return o.call(ctx, "你是消防物联网运维助手。回答必须基于提供的受控平台数据；缺少数据时明确说明，不能编造，也不能直接控制设备。租户："+tenant, question) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (o *Ollama) GenerateJSON(ctx context.Context, tenant, system, user string) (string, error) { /* 定义 GenerateJSON 函数。 */
	return o.callJSON(ctx, system+"\n租户："+tenant, user) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (o *Ollama) callJSON(ctx context.Context, system, user string) (string, error) { /* 定义 callJSON 函数。 */
	body, err := json.Marshal(chatRequest{Model: o.model, Stream: false, Format: "json", Messages: []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}}}) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("Ollama request could not be encoded") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/chat", bytes.NewReader(body)) /* 更新 err 的值。 */
	if err != nil {                                                                                            /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("Ollama request could not be created") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	req.Header.Set("Content-Type", "application/json") /* 执行当前语句并推进处理流程。 */
	resp, err := o.http.Do(req)                        /* 更新 err 的值。 */
	if err != nil {                                    /* 判断条件并选择处理分支。 */
		if ctx.Err() != nil { /* 判断条件并选择处理分支。 */
			return "", ctx.Err() /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return "", fmt.Errorf("Ollama request failed") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer resp.Body.Close()       /* 安排函数结束时执行清理。 */
	if resp.StatusCode/100 != 2 { /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("ollama request failed with HTTP %d", resp.StatusCode) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxAIProviderResponseBytes+1)) /* 更新 err 的值。 */
	if err != nil {                                                                          /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("Ollama response could not be read") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(responseBody) > maxAIProviderResponseBytes { /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("Ollama response exceeded the size limit") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var out chatResponse                                       /* 声明 out。 */
	if err := json.Unmarshal(responseBody, &out); err != nil { /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("Ollama returned an invalid response") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.TrimSpace(out.Message.Content) == "" { /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("Ollama returned an empty response") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return out.Message.Content, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (o *Ollama) RuleDraft(ctx context.Context, tenant, text string) (model.AlarmRule, error) { /* 定义 RuleDraft 函数。 */
	content, err := o.call(ctx, aioutput.RuleDraftInstructions+"租户："+tenant, text) /* 更新 err 的值。 */
	if err != nil {                                                                /* 判断条件并选择处理分支。 */
		return model.AlarmRule{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rule, err := aioutput.DecodeRuleDraft(content) /* 更新 err 的值。 */
	if err != nil {                                /* 判断条件并选择处理分支。 */
		return rule, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rule.TenantID = tenant                  /* 更新 rule.TenantID 的值。 */
	rule.Enabled = false                    /* 更新 rule.Enabled 的值。 */
	rule.Version = 1                        /* 更新 rule.Version 的值。 */
	rule.CreatedAt = time.Now().UnixMilli() /* 更新 rule.CreatedAt 的值。 */
	rule.UpdatedAt = rule.CreatedAt         /* 更新 rule.UpdatedAt 的值。 */
	return rule, nil                        /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (o *Ollama) Health(ctx context.Context) error { /* 定义 Health 函数。 */
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+"/api/tags", nil) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                         /* 判断条件并选择处理分支。 */
		return fmt.Errorf("Ollama health check could not be created") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	resp, err := o.http.Do(req) /* 更新 err 的值。 */
	if err != nil {             /* 判断条件并选择处理分支。 */
		if ctx.Err() != nil { /* 判断条件并选择处理分支。 */
			return ctx.Err() /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return fmt.Errorf("Ollama health check failed") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	resp.Body.Close()             /* 执行当前语句并推进处理流程。 */
	if resp.StatusCode/100 != 2 { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("ollama %s", resp.Status) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (o *Ollama) ProviderInfo() ports.AIPluginInfo { /* 定义 ProviderInfo 函数。 */
	return ports.AIPluginInfo{ID: "ollama", Name: "Ollama", Description: "Local Ollama model provider plugin", DefaultBaseURL: o.baseURL, DefaultModel: o.model, Model: o.model, Enabled: true, Capabilities: []string{"chat", "alarm-analysis", "rule-draft", "local-model"}} /* 返回当前处理结果。 */
}                    /* 结束当前表达式或代码块。 */
type NoopAI struct{} /* 定义 NoopAI 类型。 */

func (NoopAI) AnalyzeAlarm(_ context.Context, a model.Alarm, _ []map[string]any, _ []string) (model.AIAnalysis, error) { /* 定义 AnalyzeAlarm 函数。 */
	return model.AIAnalysis{AlarmID: a.ID, Summary: "AI 模型未启用，已保留告警供人工研判。", RiskLevel: a.AlarmLevel, Confidence: 0, Model: "disabled", PromptVersion: "fallback-v1", CreatedAt: time.Now().UnixMilli()}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (NoopAI) Chat(context.Context, string, string) (string, error) { /* 定义 Chat 函数。 */
	return "AI 模型未启用。请配置 IOT_OLLAMA_URL 与模型后重试。", nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (NoopAI) GenerateJSON(context.Context, string, string, string) (string, error) { /* 定义 GenerateJSON 函数。 */
	return "", fmt.Errorf("AI model disabled") /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (NoopAI) RuleDraft(context.Context, string, string) (model.AlarmRule, error) { /* 定义 RuleDraft 函数。 */
	return model.AlarmRule{}, fmt.Errorf("AI model disabled") /* 返回当前处理结果。 */
}                                           /* 结束当前表达式或代码块。 */
func (NoopAI) Health(context.Context) error { return nil } /* 定义 Health 函数。 */
func (NoopAI) ProviderInfo() ports.AIPluginInfo { /* 定义 ProviderInfo 函数。 */
	return ports.AIPluginInfo{ID: "disabled", Name: "未启用", Description: "AI provider is disabled", Enabled: false, Capabilities: []string{"fallback"}} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
