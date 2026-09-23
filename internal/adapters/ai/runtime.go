package aiadapter /* 声明 aiadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */
	"strings" /* 执行当前语句并推进处理流程。 */
	"sync"    /* 执行当前语句并推进处理流程。 */

	"iot-platform/internal/model" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports" /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

// RuntimeProvider is a small atomic switch in front of a concrete provider.
// Eino keeps a reference to this object, so replacing the selected client
// updates alarm analysis, chat, rule drafts and structured AI calls together.
type RuntimeProvider struct { /* 定义 RuntimeProvider 类型。 */
	mu       sync.RWMutex           /* 执行当前语句并推进处理流程。 */
	registry ports.AIPluginRegistry /* 执行当前语句并推进处理流程。 */
	client   ports.AIClient         /* 执行当前语句并推进处理流程。 */
	config   ports.AIPluginConfig   /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func NewRuntimeProvider(registry ports.AIPluginRegistry, config ports.AIPluginConfig) (*RuntimeProvider, error) { /* 定义 NewRuntimeProvider 函数。 */
	client, err := registry.Create(config) /* 更新 err 的值。 */
	if err != nil {                        /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return &RuntimeProvider{registry: registry, client: client, config: normalizeConfig(config)}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func normalizeConfig(config ports.AIPluginConfig) ports.AIPluginConfig { /* 定义 normalizeConfig 函数。 */
	config.Provider = normalizeProvider(config.Provider) /* 更新 config.Provider 的值。 */
	return config                                        /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func normalizeProvider(provider string) string { /* 定义 normalizeProvider 函数。 */
	provider = strings.ToLower(strings.TrimSpace(provider)) /* 更新 provider 的值。 */
	switch provider {                                       /* 根据条件选择处理路径。 */
	case "ollama", "deepseek", "openai-compatible", "disabled": /* 处理当前分支。 */
		return provider /* 返回当前处理结果。 */
	default: /* 处理当前分支。 */
		return provider /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (r *RuntimeProvider) current() ports.AIClient { /* 定义 current 函数。 */
	r.mu.RLock()         /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock() /* 安排函数结束时执行清理。 */
	return r.client      /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *RuntimeProvider) CurrentConfig() ports.AIPluginConfig { /* 定义 CurrentConfig 函数。 */
	r.mu.RLock()         /* 执行当前语句并推进处理流程。 */
	defer r.mu.RUnlock() /* 安排函数结束时执行清理。 */
	return r.config      /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

// Configure validates the new client and checks its health before swapping it
// into service. The caller controls the timeout, which keeps an unavailable
// remote endpoint from blocking the API indefinitely.
func (r *RuntimeProvider) Configure(ctx context.Context, config ports.AIPluginConfig) error { /* 定义 Configure 函数。 */
	config = normalizeConfig(config)         /* 更新 config 的值。 */
	client, err := r.registry.Create(config) /* 更新 err 的值。 */
	if err != nil {                          /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := client.Health(ctx); err != nil { /* 判断条件并选择处理分支。 */
		return err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	r.mu.Lock()       /* 执行当前语句并推进处理流程。 */
	r.client = client /* 更新 r.client 的值。 */
	r.config = config /* 更新 r.config 的值。 */
	r.mu.Unlock()     /* 执行当前语句并推进处理流程。 */
	return nil        /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *RuntimeProvider) AnalyzeAlarm(ctx context.Context, alarm model.Alarm, history []map[string]any, knowledge []string) (model.AIAnalysis, error) { /* 定义 AnalyzeAlarm 函数。 */
	return r.current().AnalyzeAlarm(ctx, alarm, history, knowledge) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *RuntimeProvider) Chat(ctx context.Context, tenant, question string) (string, error) { /* 定义 Chat 函数。 */
	return r.current().Chat(ctx, tenant, question) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *RuntimeProvider) GenerateJSON(ctx context.Context, tenant, system, user string) (string, error) { /* 定义 GenerateJSON 函数。 */
	client := r.current()                                    /* 更新 client 的值。 */
	if generator, ok := client.(ports.AIJSONGenerator); ok { /* 判断条件并选择处理分支。 */
		return generator.GenerateJSON(ctx, tenant, system, user) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return client.Chat(ctx, tenant, system+"\n\n请只返回合法 JSON。\n"+user) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *RuntimeProvider) RuleDraft(ctx context.Context, tenant, text string) (model.AlarmRule, error) { /* 定义 RuleDraft 函数。 */
	return r.current().RuleDraft(ctx, tenant, text) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *RuntimeProvider) Health(ctx context.Context) error { /* 定义 Health 函数。 */
	return r.current().Health(ctx) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func (r *RuntimeProvider) ProviderInfo() ports.AIPluginInfo { /* 定义 ProviderInfo 函数。 */
	if provider, ok := r.current().(ports.AIInspectable); ok { /* 判断条件并选择处理分支。 */
		return provider.ProviderInfo() /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return ports.AIPluginInfo{ID: r.CurrentConfig().Provider, Name: r.CurrentConfig().Provider, Enabled: true} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

var _ ports.AIClient = (*RuntimeProvider)(nil)          /* 声明 _。 */
var _ ports.AIJSONGenerator = (*RuntimeProvider)(nil)   /* 声明 _。 */
var _ ports.AIInspectable = (*RuntimeProvider)(nil)     /* 声明 _。 */
var _ ports.AIProviderRuntime = (*RuntimeProvider)(nil) /* 声明 _。 */
