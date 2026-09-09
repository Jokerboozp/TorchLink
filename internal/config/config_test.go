package config

import (
	"strings"
	"testing"
	"time"
)

func TestAIKeyFallbackIsProviderScoped(t *testing.T) {
	t.Setenv("IOT_AI_PROVIDER", "openai-compatible")
	t.Setenv("IOT_AI_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "deepseek-secret")
	if got := Load().AIAPIKey; got != "" {
		t.Fatalf("DeepSeek key leaked into generic provider config: %q", got)
	}

	t.Setenv("IOT_AI_PROVIDER", "deepseek")
	if got := Load().AIAPIKey; got != "deepseek-secret" {
		t.Fatalf("DeepSeek provider key=%q", got)
	}

	t.Setenv("IOT_AI_API_KEY", "generic-secret")
	if got := Load().AIAPIKey; got != "generic-secret" {
		t.Fatalf("generic provider key did not take precedence: %q", got)
	}
}

func TestDeepSeekKeyEnablesProviderWhenProviderIsUnset(t *testing.T) {
	t.Setenv("IOT_AI_PROVIDER", "")
	t.Setenv("IOT_AI_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "deepseek-secret")

	cfg := Load()
	if cfg.AIProvider != "deepseek" {
		t.Fatalf("AI provider=%q, want deepseek", cfg.AIProvider)
	}
	if cfg.AIAPIKey != "deepseek-secret" {
		t.Fatalf("AI API key=%q", cfg.AIAPIKey)
	}
}

func TestHarnessConfiguration(t *testing.T) {
	t.Setenv("IOT_AI_HARNESS_URL", "https://harness.example/")
	t.Setenv("IOT_AI_HARNESS_TOKEN", "service-token")
	t.Setenv("IOT_AI_HARNESS_MCP_URL", "https://api.example/mcp/harness")
	t.Setenv("IOT_AI_HARNESS_MODEL", "deepseek-chat")
	t.Setenv("IOT_AI_HARNESS_TIMEOUT", "45s")
	cfg := Load()
	if cfg.AIHarnessURL != "https://harness.example" || cfg.AIHarnessToken != "service-token" || cfg.AIHarnessMCPURL != "https://api.example/mcp/harness" || cfg.AIHarnessModel != "deepseek-chat" || cfg.AIHarnessTimeout != 45*time.Second {
		t.Fatalf("unexpected harness configuration: %#v", cfg)
	}
}

func TestVideoPlatformTenantBindings(t *testing.T) {
	t.Setenv("IOT_VIDEO_PLATFORM_TENANTS", "video-a:tenant-a,video-b:tenant-b")
	cfg := Load()
	if cfg.VideoPlatformTenants["video-a"] != "tenant-a" || cfg.VideoPlatformTenants["video-b"] != "tenant-b" {
		t.Fatalf("unexpected video tenant bindings: %#v", cfg.VideoPlatformTenants)
	}
}

func TestHikvisionArtemisConfig(t *testing.T) {
	t.Setenv("IOT_VIDEO_HIKVISION_API_URL", "https://hikcentral.example.internal")
	t.Setenv("IOT_VIDEO_HIKVISION_APP_KEY", "app-key")
	t.Setenv("IOT_VIDEO_HIKVISION_APP_SECRET", "app-secret")
	cfg := Load()
	if cfg.HikvisionVideoAPIURL != "https://hikcentral.example.internal" || cfg.HikvisionAppKey != "app-key" || cfg.HikvisionAppSecret != "app-secret" {
		t.Fatalf("unexpected Hikvision Artemis config: %#v", cfg)
	}
}

func TestAdminTenantAllowlist(t *testing.T) {
	t.Setenv("IOT_ADMIN_TENANTS", "tenant-a, tenant-b")
	cfg := Load()
	if len(cfg.AdminTenants) != 2 || cfg.AdminTenants[0] != "tenant-a" || cfg.AdminTenants[1] != "tenant-b" {
		t.Fatalf("unexpected admin tenant allowlist: %#v", cfg.AdminTenants)
	}
}

func TestProductionConfigRequiresExplicitStrongAuthenticationSecrets(t *testing.T) {
	t.Setenv("IOT_DEV_MODE", "false")
	t.Setenv("IOT_JWT_SECRET", "")
	t.Setenv("IOT_ADMIN_PASSWORD", "")
	if err := Load().Validate(); err == nil {
		t.Fatal("production configuration accepted built-in authentication fallbacks")
	}

	t.Setenv("IOT_JWT_SECRET", strings.Repeat("j", 48))
	t.Setenv("IOT_ADMIN_PASSWORD", strings.Repeat("p", 20))
	if err := Load().Validate(); err != nil {
		t.Fatalf("production configuration rejected explicit strong secrets: %v", err)
	}
}

func TestDevelopmentConfigAllowsLocalFallbacks(t *testing.T) {
	t.Setenv("IOT_DEV_MODE", "true")
	t.Setenv("IOT_JWT_SECRET", "")
	t.Setenv("IOT_ADMIN_PASSWORD", "")
	if err := Load().Validate(); err != nil {
		t.Fatalf("development configuration rejected local fallbacks: %v", err)
	}
}

func TestProductionConfigRejectsPlaceholderSecretsAndInvalidMode(t *testing.T) {
	t.Setenv("IOT_DEV_MODE", "false")
	t.Setenv("IOT_JWT_SECRET", "change-this-"+strings.Repeat("j", 48))
	t.Setenv("IOT_ADMIN_PASSWORD", "replace-me-"+strings.Repeat("p", 20))
	if err := Load().Validate(); err == nil {
		t.Fatal("production configuration accepted placeholder secrets")
	}

	t.Setenv("IOT_DEV_MODE", "not-a-boolean")
	t.Setenv("IOT_JWT_SECRET", strings.Repeat("j", 48))
	t.Setenv("IOT_ADMIN_PASSWORD", strings.Repeat("p", 20))
	if err := Load().Validate(); err == nil {
		t.Fatal("configuration accepted an invalid IOT_DEV_MODE value")
	}
}

func TestExplicitConfigValueCanBeValidatedWithoutEnvironmentProvenance(t *testing.T) {
	cfg := Config{DevMode: false, JWTSecret: strings.Repeat("j", 48), AdminPassword: strings.Repeat("p", 20)}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("explicit configuration values were rejected: %v", err)
	}
}

func TestSplitRolesRequireSharedDependencies(t *testing.T) {
	cfg := Config{DevMode: true, ProcessRole: "api"}
	if cfg.Validate() == nil {
		t.Fatal("split mode accepted process-local storage")
	}
	cfg.PostgresDSN = "test-dsn"
	cfg.KafkaBrokers = []string{"broker:9092"}
	if cfg.Validate() == nil {
		t.Fatal("API accepted missing gateway")
	}
	cfg.AccessGatewayURL = "http://gateway:8080"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.AccessGatewayURL = "http://user:password@gateway:8080"
	if cfg.Validate() == nil {
		t.Fatal("gateway URL accepted inline credentials")
	}
	cfg.AccessGatewayURL = ""
	cfg.ProcessRole = "gateway"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.ProcessRole = "other"
	if cfg.Validate() == nil {
		t.Fatal("unknown role accepted")
	}
}
