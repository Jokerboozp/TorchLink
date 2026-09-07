package aiadapter

import (
	"fmt"
	"sort"
	"strings"

	"iot-platform/internal/ports"
)

type providerFactory struct {
	info  ports.AIPluginInfo
	build func(ports.AIPluginConfig) (ports.AIClient, error)
}

type ProviderRegistry struct {
	factories map[string]providerFactory
}

func NewProviderRegistry() *ProviderRegistry {
	r := &ProviderRegistry{factories: map[string]providerFactory{}}
	r.register(providerFactory{
		info:  ports.AIPluginInfo{ID: "disabled", Name: "未启用", Description: "保留安全降级响应，不连接外部模型。", Capabilities: []string{"fallback"}},
		build: func(ports.AIPluginConfig) (ports.AIClient, error) { return NoopAI{}, nil },
	})
	r.register(providerFactory{
		info: ports.AIPluginInfo{ID: "deepseek", Name: "DeepSeek", Description: "DeepSeek 官方 OpenAI-compatible API。", DefaultBaseURL: "https://api.deepseek.com", DefaultModel: "deepseek-v4-flash", RequiresAPIKey: true, Enabled: true, Capabilities: []string{"chat", "alarm-analysis", "rule-draft", "json-output"}},
		build: func(cfg ports.AIPluginConfig) (ports.AIClient, error) {
			if strings.TrimSpace(cfg.APIKey) == "" {
				return nil, fmt.Errorf("DeepSeek API Key is required")
			}
			return NewOpenAICompatible("deepseek", "DeepSeek", valueOr(cfg.BaseURL, "https://api.deepseek.com"), valueOr(cfg.Model, "deepseek-v4-flash"), cfg.APIKey)
		},
	})
	r.register(providerFactory{
		info: ports.AIPluginInfo{ID: "ollama", Name: "Ollama", Description: "连接本地或私有网络中的 Ollama 模型服务。", DefaultBaseURL: "http://localhost:11434", DefaultModel: "qwen3:1.7b", Enabled: true, Capabilities: []string{"chat", "alarm-analysis", "rule-draft", "json-output", "local-model"}},
		build: func(cfg ports.AIPluginConfig) (ports.AIClient, error) {
			return NewOllama(valueOr(cfg.BaseURL, "http://localhost:11434"), valueOr(cfg.Model, "qwen3:1.7b"))
		},
	})
	r.register(providerFactory{
		info: ports.AIPluginInfo{ID: "openai-compatible", Name: "OpenAI Compatible", Description: "连接实现 Chat Completions 接口的私有或第三方模型服务。", RequiresAPIKey: false, Enabled: true, Capabilities: []string{"chat", "alarm-analysis", "rule-draft", "json-output"}},
		build: func(cfg ports.AIPluginConfig) (ports.AIClient, error) {
			if strings.TrimSpace(cfg.BaseURL) == "" || strings.TrimSpace(cfg.Model) == "" {
				return nil, fmt.Errorf("baseUrl and model are required for OpenAI-compatible providers")
			}
			return NewOpenAICompatible("openai-compatible", "OpenAI Compatible", cfg.BaseURL, cfg.Model, cfg.APIKey)
		},
	})
	return r
}

func (r *ProviderRegistry) register(factory providerFactory) {
	r.factories[factory.info.ID] = factory
}

func (r *ProviderRegistry) List() []ports.AIPluginInfo {
	items := make([]ports.AIPluginInfo, 0, len(r.factories))
	for _, factory := range r.factories {
		items = append(items, factory.info)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func (r *ProviderRegistry) Create(cfg ports.AIPluginConfig) (ports.AIClient, error) {
	id := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if id == "" {
		id = "disabled"
	}
	factory, ok := r.factories[id]
	if !ok {
		return nil, fmt.Errorf("unknown AI provider plugin %q", id)
	}
	cfg.Provider = id
	return factory.build(cfg)
}

func valueOr(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}

var _ ports.AIPluginRegistry = (*ProviderRegistry)(nil)
