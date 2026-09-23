package aiadapter /* 声明 aiadapter 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"context" /* 执行当前语句并推进处理流程。 */

	"github.com/cloudwego/eino/compose" /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/model"       /* 执行当前语句并推进处理流程。 */
	"iot-platform/internal/ports"       /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

type alarmInput struct { /* 定义 alarmInput 类型。 */
	Alarm     model.Alarm      /* 执行当前语句并推进处理流程。 */
	History   []map[string]any /* 执行当前语句并推进处理流程。 */
	Knowledge []string         /* 执行当前语句并推进处理流程。 */
}                                                /* 结束当前表达式或代码块。 */
type chatInput struct{ Tenant, Question string } /* 定义 chatInput 类型。 */
type ruleInput struct{ Tenant, Text string }     /* 定义 ruleInput 类型。 */

// EinoOrchestrator keeps model access behind the platform AIClient while using
// Eino chains for alarm, chat and rule workflows. More steps can be inserted
// without changing core ingestion code.
type EinoOrchestrator struct { /* 定义 EinoOrchestrator 类型。 */
	base  ports.AIClient                                 /* 执行当前语句并推进处理流程。 */
	alarm compose.Runnable[alarmInput, model.AIAnalysis] /* 执行当前语句并推进处理流程。 */
	chat  compose.Runnable[chatInput, string]            /* 执行当前语句并推进处理流程。 */
	rule  compose.Runnable[ruleInput, model.AlarmRule]   /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func NewEino(ctx context.Context, base ports.AIClient) (*EinoOrchestrator, error) { /* 定义 NewEino 函数。 */
	alarmChain := compose.NewChain[alarmInput, model.AIAnalysis]().AppendLambda(compose.InvokableLambda(func(ctx context.Context, in alarmInput) (model.AIAnalysis, error) { /* 更新 alarmChain 的值。 */
		return base.AnalyzeAlarm(ctx, in.Alarm, in.History, in.Knowledge) /* 返回当前处理结果。 */
	})) /* 结束当前表达式或代码块。 */
	alarmRun, err := alarmChain.Compile(ctx, compose.WithGraphName("alarm-analysis-worker")) /* 更新 err 的值。 */
	if err != nil {                                                                          /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	chatChain := compose.NewChain[chatInput, string]().AppendLambda(compose.InvokableLambda(func(ctx context.Context, in chatInput) (string, error) { return base.Chat(ctx, in.Tenant, in.Question) })) /* 更新 chatChain 的值。 */
	chatRun, err := chatChain.Compile(ctx, compose.WithGraphName("ops-chat-service"))                                                                                                                   /* 更新 err 的值。 */
	if err != nil {                                                                                                                                                                                     /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	ruleChain := compose.NewChain[ruleInput, model.AlarmRule]().AppendLambda(compose.InvokableLambda(func(ctx context.Context, in ruleInput) (model.AlarmRule, error) { /* 更新 ruleChain 的值。 */
		return base.RuleDraft(ctx, in.Tenant, in.Text) /* 返回当前处理结果。 */
	})) /* 结束当前表达式或代码块。 */
	ruleRun, err := ruleChain.Compile(ctx, compose.WithGraphName("rule-assistant-service")) /* 更新 err 的值。 */
	if err != nil {                                                                         /* 判断条件并选择处理分支。 */
		return nil, err /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return &EinoOrchestrator{base: base, alarm: alarmRun, chat: chatRun, rule: ruleRun}, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (e *EinoOrchestrator) AnalyzeAlarm(ctx context.Context, a model.Alarm, h []map[string]any, k []string) (model.AIAnalysis, error) { /* 定义 AnalyzeAlarm 函数。 */
	return e.alarm.Invoke(ctx, alarmInput{a, h, k}) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (e *EinoOrchestrator) Chat(ctx context.Context, tenant, q string) (string, error) { /* 定义 Chat 函数。 */
	return e.chat.Invoke(ctx, chatInput{tenant, q}) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (e *EinoOrchestrator) GenerateJSON(ctx context.Context, tenant, system, user string) (string, error) { /* 定义 GenerateJSON 函数。 */
	if generator, ok := e.base.(ports.AIJSONGenerator); ok { /* 判断条件并选择处理分支。 */
		return generator.GenerateJSON(ctx, tenant, system, user) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return e.base.Chat(ctx, tenant, system+"\n\n请只返回合法 JSON。\n"+user) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func (e *EinoOrchestrator) RuleDraft(ctx context.Context, tenant, text string) (model.AlarmRule, error) { /* 定义 RuleDraft 函数。 */
	return e.rule.Invoke(ctx, ruleInput{tenant, text}) /* 返回当前处理结果。 */
}                                                            /* 结束当前表达式或代码块。 */
func (e *EinoOrchestrator) Health(ctx context.Context) error { return e.base.Health(ctx) } /* 定义 Health 函数。 */
func (e *EinoOrchestrator) ProviderInfo() ports.AIPluginInfo { /* 定义 ProviderInfo 函数。 */
	if provider, ok := e.base.(ports.AIInspectable); ok { /* 判断条件并选择处理分支。 */
		return provider.ProviderInfo() /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return ports.AIPluginInfo{ID: "unknown", Name: "Unknown AI provider", Enabled: true} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
