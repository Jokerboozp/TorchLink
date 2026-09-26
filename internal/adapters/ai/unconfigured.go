package aiadapter

import (
	"context"
	"errors"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

var errDeepSeekKeyRequired = errors.New("请在模型管理中填写 DeepSeek API Key，测试并应用后启用 AI 功能")

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
