package aiadapter

import (
	"context"
	"errors"
	"iot-platform/internal/ports"
)

var errDeepSeekKeyRequired = errors.New("请在模型管理中填写并保存 DeepSeek API Key 后启用 AI 功能")

type unconfiguredDeepSeek struct{}

func (unconfiguredDeepSeek) Health(context.Context) error { return errDeepSeekKeyRequired }
func (unconfiguredDeepSeek) Chat(context.Context, string, string) (string, error) {
	return "", errDeepSeekKeyRequired
}
func (unconfiguredDeepSeek) ProviderInfo() ports.AIPluginInfo {
	return ports.AIPluginInfo{ID: "deepseek", Name: "DeepSeek（待配置密钥）", RequiresAPIKey: true, Enabled: false}
}

// NoopAI backs the "disabled" provider with a safe fallback response.
type NoopAI struct{}

func (NoopAI) Chat(context.Context, string, string) (string, error) {
	return "AI 模型未启用。请在模型管理中配置模型服务后重试。", nil
}
func (NoopAI) Health(context.Context) error { return nil }
func (NoopAI) ProviderInfo() ports.AIPluginInfo {
	return ports.AIPluginInfo{ID: "disabled", Name: "未启用", Description: "AI provider is disabled", Enabled: false, Capabilities: []string{"fallback"}}
}
