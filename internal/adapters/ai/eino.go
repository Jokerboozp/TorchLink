package aiadapter

import (
	"context"

	"github.com/cloudwego/eino/compose"
	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

type alarmInput struct {
	Alarm     model.Alarm
	History   []map[string]any
	Knowledge []string
}
type chatInput struct{ Tenant, Question string }
type ruleInput struct{ Tenant, Text string }

// EinoOrchestrator keeps model access behind the platform AIClient while using
// Eino chains for alarm, chat and rule workflows. More steps can be inserted
// without changing core ingestion code.
type EinoOrchestrator struct {
	base  ports.AIClient
	alarm compose.Runnable[alarmInput, model.AIAnalysis]
	chat  compose.Runnable[chatInput, string]
	rule  compose.Runnable[ruleInput, model.AlarmRule]
}

func NewEino(ctx context.Context, base ports.AIClient) (*EinoOrchestrator, error) {
	alarmChain := compose.NewChain[alarmInput, model.AIAnalysis]().AppendLambda(compose.InvokableLambda(func(ctx context.Context, in alarmInput) (model.AIAnalysis, error) {
		return base.AnalyzeAlarm(ctx, in.Alarm, in.History, in.Knowledge)
	}))
	alarmRun, err := alarmChain.Compile(ctx, compose.WithGraphName("alarm-analysis-worker"))
	if err != nil {
		return nil, err
	}
	chatChain := compose.NewChain[chatInput, string]().AppendLambda(compose.InvokableLambda(func(ctx context.Context, in chatInput) (string, error) { return base.Chat(ctx, in.Tenant, in.Question) }))
	chatRun, err := chatChain.Compile(ctx, compose.WithGraphName("ops-chat-service"))
	if err != nil {
		return nil, err
	}
	ruleChain := compose.NewChain[ruleInput, model.AlarmRule]().AppendLambda(compose.InvokableLambda(func(ctx context.Context, in ruleInput) (model.AlarmRule, error) {
		return base.RuleDraft(ctx, in.Tenant, in.Text)
	}))
	ruleRun, err := ruleChain.Compile(ctx, compose.WithGraphName("rule-assistant-service"))
	if err != nil {
		return nil, err
	}
	return &EinoOrchestrator{base: base, alarm: alarmRun, chat: chatRun, rule: ruleRun}, nil
}
func (e *EinoOrchestrator) AnalyzeAlarm(ctx context.Context, a model.Alarm, h []map[string]any, k []string) (model.AIAnalysis, error) {
	return e.alarm.Invoke(ctx, alarmInput{a, h, k})
}
func (e *EinoOrchestrator) Chat(ctx context.Context, tenant, q string) (string, error) {
	return e.chat.Invoke(ctx, chatInput{tenant, q})
}
func (e *EinoOrchestrator) GenerateJSON(ctx context.Context, tenant, system, user string) (string, error) {
	if generator, ok := e.base.(ports.AIJSONGenerator); ok {
		return generator.GenerateJSON(ctx, tenant, system, user)
	}
	return e.base.Chat(ctx, tenant, system+"\n\n请只返回合法 JSON。\n"+user)
}
func (e *EinoOrchestrator) RuleDraft(ctx context.Context, tenant, text string) (model.AlarmRule, error) {
	return e.rule.Invoke(ctx, ruleInput{tenant, text})
}
func (e *EinoOrchestrator) Health(ctx context.Context) error { return e.base.Health(ctx) }
func (e *EinoOrchestrator) ProviderInfo() ports.AIPluginInfo {
	if provider, ok := e.base.(ports.AIInspectable); ok {
		return provider.ProviderInfo()
	}
	return ports.AIPluginInfo{ID: "unknown", Name: "Unknown AI provider", Enabled: true}
}
