package aiadapter

import (
	"context"
	"errors"
	"testing"

	"iot-platform/internal/ports"
)

func TestRuntimeAllowsInitialDeepSeekWithoutKeyButRejectsActivation(t *testing.T) {
	registry := NewProviderRegistry()
	cfg := ports.AIPluginConfig{Provider: "deepseek", Model: "deepseek-flash"}
	runtime, err := NewRuntimeProvider(registry, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.ProviderInfo().Enabled || runtime.CurrentConfig().Provider != "deepseek" {
		t.Fatal("missing-key provider must remain selected but unavailable")
	}
	if !errors.Is(runtime.Health(context.Background()), errDeepSeekKeyRequired) {
		t.Fatal("health should explain that a DeepSeek key is required")
	}
	if _, err := runtime.Chat(context.Background(), "tenant", "hello"); !errors.Is(err, errDeepSeekKeyRequired) {
		t.Fatal("missing-key chat should fail without contacting a provider")
	}
	if err := runtime.Configure(context.Background(), cfg); err == nil {
		t.Fatal("activation without a key must still be rejected")
	}
	if runtime.ProviderInfo().Enabled {
		t.Fatal("failed activation changed the pending provider")
	}
}
