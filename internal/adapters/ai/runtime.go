package aiadapter

import (
	"context"
	"strings"
	"sync"

	"iot-platform/internal/model"
	"iot-platform/internal/ports"
)

// RuntimeProvider is a small atomic switch in front of a concrete provider.
// Eino keeps a reference to this object, so replacing the selected client
// updates alarm analysis, chat, rule drafts and structured AI calls together.
type RuntimeProvider struct {
	mu       sync.RWMutex
	registry ports.AIPluginRegistry
	client   ports.AIClient
	config   ports.AIPluginConfig
}

func NewRuntimeProvider(registry ports.AIPluginRegistry, config ports.AIPluginConfig) (*RuntimeProvider, error) {
	client, err := registry.Create(config)
	if err != nil {
		return nil, err
	}
	return &RuntimeProvider{registry: registry, client: client, config: normalizeConfig(config)}, nil
}

func normalizeConfig(config ports.AIPluginConfig) ports.AIPluginConfig {
	config.Provider = normalizeProvider(config.Provider)
	return config
}

func normalizeProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case "ollama", "deepseek", "openai-compatible", "disabled":
		return provider
	default:
		return provider
	}
}

func (r *RuntimeProvider) current() ports.AIClient {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.client
}

func (r *RuntimeProvider) CurrentConfig() ports.AIPluginConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config
}

// Configure validates the new client and checks its health before swapping it
// into service. The caller controls the timeout, which keeps an unavailable
// remote endpoint from blocking the API indefinitely.
func (r *RuntimeProvider) Configure(ctx context.Context, config ports.AIPluginConfig) error {
	config = normalizeConfig(config)
	client, err := r.registry.Create(config)
	if err != nil {
		return err
	}
	if err := client.Health(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	r.client = client
	r.config = config
	r.mu.Unlock()
	return nil
}

func (r *RuntimeProvider) AnalyzeAlarm(ctx context.Context, alarm model.Alarm, history []map[string]any, knowledge []string) (model.AIAnalysis, error) {
	return r.current().AnalyzeAlarm(ctx, alarm, history, knowledge)
}

func (r *RuntimeProvider) Chat(ctx context.Context, tenant, question string) (string, error) {
	return r.current().Chat(ctx, tenant, question)
}

func (r *RuntimeProvider) GenerateJSON(ctx context.Context, tenant, system, user string) (string, error) {
	client := r.current()
	if generator, ok := client.(ports.AIJSONGenerator); ok {
		return generator.GenerateJSON(ctx, tenant, system, user)
	}
	return client.Chat(ctx, tenant, system+"\n\n请只返回合法 JSON。\n"+user)
}

func (r *RuntimeProvider) RuleDraft(ctx context.Context, tenant, text string) (model.AlarmRule, error) {
	return r.current().RuleDraft(ctx, tenant, text)
}

func (r *RuntimeProvider) Health(ctx context.Context) error {
	return r.current().Health(ctx)
}

func (r *RuntimeProvider) ProviderInfo() ports.AIPluginInfo {
	if provider, ok := r.current().(ports.AIInspectable); ok {
		return provider.ProviderInfo()
	}
	return ports.AIPluginInfo{ID: r.CurrentConfig().Provider, Name: r.CurrentConfig().Provider, Enabled: true}
}

var _ ports.AIClient = (*RuntimeProvider)(nil)
var _ ports.AIJSONGenerator = (*RuntimeProvider)(nil)
var _ ports.AIInspectable = (*RuntimeProvider)(nil)
var _ ports.AIProviderRuntime = (*RuntimeProvider)(nil)
