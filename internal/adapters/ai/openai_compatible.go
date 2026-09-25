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

const maxAIProviderResponseBytes = 4 << 20 /* 声明 maxAIProviderResponseBytes。 */

type OpenAICompatible struct { /* 定义 OpenAICompatible 类型。 */
	providerID, providerName, baseURL, model, apiKey string       /* 执行当前语句并推进处理流程。 */
	http                                             *http.Client /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type openAIChatRequest struct { /* 定义 openAIChatRequest 类型。 */
	Model          string              `json:"model"`                     /* 执行当前语句并推进处理流程。 */
	Messages       []map[string]string `json:"messages"`                  /* 执行当前语句并推进处理流程。 */
	Stream         bool                `json:"stream"`                    /* 执行当前语句并推进处理流程。 */
	ResponseFormat map[string]string   `json:"response_format,omitempty"` /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type openAIChatResponse struct { /* 定义 openAIChatResponse 类型。 */
	ID      string     `json:"id"` /* 执行当前语句并推进处理流程。 */
	Choices []struct { /* 执行当前语句并推进处理流程。 */
		Message struct { /* 执行当前语句并推进处理流程。 */
			Content string `json:"content"` /* 执行当前语句并推进处理流程。 */
		} `json:"message"` /* 结束当前表达式或代码块。 */
	} `json:"choices"` /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func NewOpenAICompatible(providerID, providerName, baseURL, model, apiKey string) (*OpenAICompatible, error) { /* 定义 NewOpenAICompatible 函数。 */
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")                                                                            /* 更新 baseURL 的值。 */
	u, err := url.Parse(baseURL)                                                                                                            /* 更新 err 的值。 */
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("AI provider base URL must be an absolute HTTP(S) URL") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if strings.TrimSpace(model) == "" { /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("AI provider model is required") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	client := &http.Client{ /* 更新 client 的值。 */
		Timeout: 2 * time.Minute, /* 执行当前语句并推进处理流程。 */
		CheckRedirect: func(req *http.Request, via []*http.Request) error { /* 执行当前语句并推进处理流程。 */
			if len(via) >= 10 { /* 判断条件并选择处理分支。 */
				return fmt.Errorf("too many AI provider redirects") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			if !strings.EqualFold(req.URL.Scheme, u.Scheme) || !strings.EqualFold(req.URL.Host, u.Host) { /* 判断条件并选择处理分支。 */
				return fmt.Errorf("cross-origin AI provider redirect rejected") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			return nil /* 返回当前处理结果。 */
		}, /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return &OpenAICompatible{providerID: providerID, providerName: providerName, baseURL: baseURL, model: model, apiKey: strings.TrimSpace(apiKey), http: client}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (o *OpenAICompatible) call(ctx context.Context, system, user string, jsonOutput bool) (string, error) { /* 定义 call 函数。 */
	payload := openAIChatRequest{Model: o.model, Stream: false, Messages: []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}}} /* 更新 payload 的值。 */
	if jsonOutput {                                                                                                                                                      /* 判断条件并选择处理分支。 */
		payload.ResponseFormat = map[string]string{"type": "json_object"} /* 更新 payload.ResponseFormat 的值。 */
	} /* 结束当前表达式或代码块。 */
	body, err := json.Marshal(payload) /* 更新 err 的值。 */
	if err != nil {                    /* 判断条件并选择处理分支。 */
		return "", err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/chat/completions", bytes.NewReader(body)) /* 更新 err 的值。 */
	if err != nil {                                                                                                    /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("%s request could not be created", o.providerName) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	req.Header.Set("Content-Type", "application/json") /* 执行当前语句并推进处理流程。 */
	if o.apiKey != "" {                                /* 判断条件并选择处理分支。 */
		req.Header.Set("Authorization", "Bearer "+o.apiKey) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	resp, err := o.http.Do(req) /* 更新 err 的值。 */
	if err != nil {             /* 判断条件并选择处理分支。 */
		if ctx.Err() != nil { /* 判断条件并选择处理分支。 */
			return "", ctx.Err() /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return "", fmt.Errorf("%s request failed", o.providerName) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer resp.Body.Close()       /* 安排函数结束时执行清理。 */
	if resp.StatusCode/100 != 2 { /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("%s request failed with HTTP %d", o.providerName, resp.StatusCode) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxAIProviderResponseBytes+1)) /* 更新 err 的值。 */
	if err != nil {                                                                          /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("%s response could not be read", o.providerName) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(responseBody) > maxAIProviderResponseBytes { /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("%s response exceeded the size limit", o.providerName) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var out openAIChatResponse                                 /* 声明 out。 */
	if err := json.Unmarshal(responseBody, &out); err != nil { /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("%s returned an invalid response", o.providerName) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" { /* 判断条件并选择处理分支。 */
		return "", fmt.Errorf("%s returned an empty response", o.providerName) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return out.Choices[0].Message.Content, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (o *OpenAICompatible) AnalyzeAlarm(ctx context.Context, alarm model.Alarm, history []map[string]any, knowledge []string) (model.AIAnalysis, error) { /* 定义 AnalyzeAlarm 函数。 */
	input, _ := json.Marshal(map[string]any{"alarm": alarm, "recentHistory": history, "knowledge": knowledge})                                               /* 更新 _ 的值。 */
	content, err := o.call(ctx, "你是消防物联网运维专家。只能提供分析和人工处置建议，禁止自动控制设备。输出 JSON：summary、possibleReasons、suggestions、riskLevel、confidence。", string(input), true) /* 更新 err 的值。 */
	if err != nil {                                                                                                                                          /* 判断条件并选择处理分支。 */
		return model.AIAnalysis{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return aioutput.DecodeAlarmAnalysis(content, alarm.ID, o.model) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (o *OpenAICompatible) Chat(ctx context.Context, tenant, question string) (string, error) { /* 定义 Chat 函数。 */
	return o.call(ctx, "你是消防物联网运维助手。仅依据受控平台数据回答，缺少数据时明确说明，不能直接控制设备。租户："+tenant, question, false) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (o *OpenAICompatible) GenerateJSON(ctx context.Context, tenant, system, user string) (string, error) { /* 定义 GenerateJSON 函数。 */
	if strings.TrimSpace(system) == "" { /* 判断条件并选择处理分支。 */
		system = "你是消防物联网平台的结构化数据助手，只返回合法 JSON。" /* 更新 system 的值。 */
	} /* 结束当前表达式或代码块。 */
	return o.call(ctx, system+"\n租户："+tenant, user, true) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (o *OpenAICompatible) RuleDraft(ctx context.Context, tenant, text string) (model.AlarmRule, error) { /* 定义 RuleDraft 函数。 */
	content, err := o.call(ctx, aioutput.RuleDraftInstructions, text, true) /* 更新 err 的值。 */
	if err != nil {                                                         /* 判断条件并选择处理分支。 */
		return model.AlarmRule{}, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rule, err := aioutput.DecodeRuleDraft(content) /* 更新 err 的值。 */
	if err != nil {                                /* 判断条件并选择处理分支。 */
		return rule, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	rule.TenantID, rule.Enabled, rule.Version = tenant, false, 1                    /* 更新 rule.Version 的值。 */
	rule.CreatedAt, rule.UpdatedAt = time.Now().UnixMilli(), time.Now().UnixMilli() /* 更新 rule.UpdatedAt 的值。 */
	return rule, nil                                                                /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (o *OpenAICompatible) Health(ctx context.Context) error { /* 定义 Health 函数。 */
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.baseURL+"/models", nil) /* 检查错误并决定后续处理。 */
	if err != nil {                                                                       /* 判断条件并选择处理分支。 */
		return fmt.Errorf("%s health check could not be created", o.providerName) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if o.apiKey != "" { /* 判断条件并选择处理分支。 */
		req.Header.Set("Authorization", "Bearer "+o.apiKey) /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
	resp, err := o.http.Do(req) /* 更新 err 的值。 */
	if err != nil {             /* 判断条件并选择处理分支。 */
		if ctx.Err() != nil { /* 判断条件并选择处理分支。 */
			return ctx.Err() /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		return fmt.Errorf("%s health check failed", o.providerName) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	defer resp.Body.Close()       /* 安排函数结束时执行清理。 */
	if resp.StatusCode/100 != 2 { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("%s %s", o.providerName, resp.Status) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (o *OpenAICompatible) ProviderInfo() ports.AIPluginInfo { /* 定义 ProviderInfo 函数。 */
	return ports.AIPluginInfo{ID: o.providerID, Name: o.providerName, Description: "OpenAI-compatible model provider plugin", DefaultBaseURL: o.baseURL, DefaultModel: o.model, Model: o.model, RequiresAPIKey: o.providerID == "deepseek", Enabled: true, Capabilities: []string{"chat", "alarm-analysis", "rule-draft", "json-output"}} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

var _ ports.AIClient = (*OpenAICompatible)(nil)      /* 声明 _。 */
var _ ports.AIInspectable = (*OpenAICompatible)(nil) /* 声明 _。 */
