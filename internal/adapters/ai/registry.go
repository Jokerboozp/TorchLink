package aiadapter /* 声明 aiadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"fmt"     /* 执行当前语句并推进处理流程。 */
	"sort"    /* 执行当前语句并推进处理流程。 */
	"strings" /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type providerFactory struct { /* 定义 providerFactory 类型。 */
	info  ports.AIPluginInfo                                 /* 执行当前语句并推进处理流程。 */
	build func(ports.AIPluginConfig) (ports.AIClient, error) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

type ProviderRegistry struct { /* 定义 ProviderRegistry 类型。 */
	factories map[string]providerFactory /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func NewProviderRegistry() *ProviderRegistry { /* 定义 NewProviderRegistry 函数。 */
	r := &ProviderRegistry{factories: map[string]providerFactory{}} /* 更新 r 的值。 */
	r.register(providerFactory{                                     /* 执行当前语句并推进处理流程。 */
		info:  ports.AIPluginInfo{ID: "disabled", Name: "未启用", Description: "保留安全降级响应，不连接外部模型。", Capabilities: []string{"fallback"}}, /* 执行当前语句并推进处理流程。 */
		build: func(ports.AIPluginConfig) (ports.AIClient, error) { return NoopAI{}, nil },                                           /* 检查错误并决定后续处理。 */
	}) /* 结束当前表达式或代码块。 */
	r.register(providerFactory{ /* 执行当前语句并推进处理流程。 */
		info: ports.AIPluginInfo{ID: "deepseek", Name: "DeepSeek", Description: "DeepSeek 官方 OpenAI-compatible API。", DefaultBaseURL: "https://api.deepseek.com", DefaultModel: "deepseek-v4-flash", RequiresAPIKey: true, Enabled: true, Capabilities: []string{"chat", "alarm-analysis", "rule-draft", "json-output"}}, /* 执行当前语句并推进处理流程。 */
		build: func(cfg ports.AIPluginConfig) (ports.AIClient, error) { /* 执行当前语句并推进处理流程。 */
			if strings.TrimSpace(cfg.APIKey) == "" { /* 判断条件并选择处理分支。 */
				return nil, fmt.Errorf("DeepSeek API Key is required") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			return NewOpenAICompatible("deepseek", "DeepSeek", valueOr(cfg.BaseURL, "https://api.deepseek.com"), valueOr(cfg.Model, "deepseek-v4-flash"), cfg.APIKey) /* 返回当前处理结果。 */
		}, /* 结束当前表达式或代码块。 */
	}) /* 结束当前表达式或代码块。 */
	r.register(providerFactory{ /* 执行当前语句并推进处理流程。 */
		info: ports.AIPluginInfo{ID: "ollama", Name: "Ollama", Description: "连接本地或私有网络中的 Ollama 模型服务。", DefaultBaseURL: "http://localhost:11434", DefaultModel: "qwen3:1.7b", Enabled: true, Capabilities: []string{"chat", "alarm-analysis", "rule-draft", "json-output", "local-model"}}, /* 执行当前语句并推进处理流程。 */
		build: func(cfg ports.AIPluginConfig) (ports.AIClient, error) { /* 执行当前语句并推进处理流程。 */
			return NewOllama(valueOr(cfg.BaseURL, "http://localhost:11434"), valueOr(cfg.Model, "qwen3:1.7b")) /* 返回当前处理结果。 */
		}, /* 结束当前表达式或代码块。 */
	}) /* 结束当前表达式或代码块。 */
	r.register(providerFactory{ /* 执行当前语句并推进处理流程。 */
		info: ports.AIPluginInfo{ID: "openai-compatible", Name: "OpenAI Compatible", Description: "连接实现 Chat Completions 接口的私有或第三方模型服务。", RequiresAPIKey: false, Enabled: true, Capabilities: []string{"chat", "alarm-analysis", "rule-draft", "json-output"}}, /* 执行当前语句并推进处理流程。 */
		build: func(cfg ports.AIPluginConfig) (ports.AIClient, error) { /* 执行当前语句并推进处理流程。 */
			if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "" { /* 判断条件并选择处理分支。 */
				return nil, fmt.Errorf("baseUrl and model are required for OpenAI-compatible providers") /* 返回当前处理结果。 */
			} /* 结束当前表达式或代码块。 */
			return NewOpenAICompatible("openai-compatible", "OpenAI Compatible", cfg.BaseURL, cfg.Model, cfg.APIKey) /* 返回当前处理结果。 */
		}, /* 结束当前表达式或代码块。 */
	}) /* 结束当前表达式或代码块。 */
	return r /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *ProviderRegistry) register(factory providerFactory) { /* 定义 register 函数。 */
	r.factories[factory.info.ID] = factory /* 更新 r.factories[factory.info.ID] 的值。 */
} /* 结束当前表达式或代码块。 */

func (r *ProviderRegistry) List() []ports.AIPluginInfo { /* 定义 List 函数。 */
	items := make([]ports.AIPluginInfo, 0, len(r.factories)) /* 更新 items 的值。 */
	for _, factory := range r.factories {                    /* 循环处理当前数据。 */
		items = append(items, factory.info) /* 更新 items 的值。 */
	} /* 结束当前表达式或代码块。 */
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID }) /* 执行当前语句并推进处理流程。 */
	return items                                                                /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *ProviderRegistry) Create(cfg ports.AIPluginConfig) (ports.AIClient, error) { /* 定义 Create 函数。 */
	id := strings.ToLower(strings.TrimSpace(cfg.Provider)) /* 更新 id 的值。 */
	if id == "" {                                          /* 判断条件并选择处理分支。 */
		id = "disabled" /* 更新 id 的值。 */
	} /* 结束当前表达式或代码块。 */
	factory, ok := r.factories[id] /* 更新 ok 的值。 */
	if !ok {                       /* 判断条件并选择处理分支。 */
		return nil, fmt.Errorf("unknown AI provider plugin %q", id) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	cfg.Provider = id         /* 更新 cfg.Provider 的值。 */
	return factory.build(cfg) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func valueOr(value, fallback string) string { /* 定义 valueOr 函数。 */
	if value = strings.TrimSpace(value); value != "" { /* 判断条件并选择处理分支。 */
		return value /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return fallback /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

var _ ports.AIPluginRegistry = (*ProviderRegistry)(nil) /* 声明 _。 */
