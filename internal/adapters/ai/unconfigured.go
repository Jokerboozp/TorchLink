package aiadapter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

var errDeepSeekKeyRequired = errors.New("请在模型管理中填写并保存 DeepSeek API Key 后启用 AI 功能")

type unconfiguredDeepSeek struct{}

func (unconfiguredDeepSeek) Health(context.Context) error { return errDeepSeekKeyRequired }
func (unconfiguredDeepSeek) Chat(context.Context, string, string) (string, error) {
	return "", errDeepSeekKeyRequired
}
func (unconfiguredDeepSeek) AnalyzeAlarm(context.Context, model.Alarm, []map[string]any, []string) (model.AIAnalysis, error) {
	return model.AIAnalysis{}, errDeepSeekKeyRequired
}
func (unconfiguredDeepSeek) RuleDraft(context.Context, string, string) (model.AlarmRule, error) {
	return model.AlarmRule{}, errDeepSeekKeyRequired
}
func (unconfiguredDeepSeek) ProviderInfo() ports.AIPluginInfo {
	return ports.AIPluginInfo{ID: "deepseek", Name: "DeepSeek（待配置密钥）", RequiresAPIKey: true, Enabled: false}
}

// NoopAI backs the "disabled" provider with a safe fallback response.
type NoopAI struct{}

func (NoopAI) AnalyzeAlarm(_ context.Context, a model.Alarm, _ []map[string]any, _ []string) (model.AIAnalysis, error) {
	return model.AIAnalysis{AlarmID: a.ID, Summary: "AI 模型未启用，已保留告警供人工研判。", RiskLevel: a.AlarmLevel, Confidence: 0, Model: "disabled", PromptVersion: "fallback-v1", CreatedAt: time.Now().UnixMilli()}, nil
}
func (NoopAI) Chat(context.Context, string, string) (string, error) {
	return "AI 模型未启用。请在模型管理中配置模型服务后重试。", nil
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
