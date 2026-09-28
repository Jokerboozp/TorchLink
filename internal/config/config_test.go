package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalCapacityConfigurationAndExplicitDisable(t *testing.T) {
	t.Setenv("IOT_OPS_CAPACITY_LOCAL", "true")
	t.Setenv("IOT_CAPACITY_MODULE", "on")
	t.Setenv("IOT_OPS_CAPACITY_URL", "http://capacity:7080")
	t.Setenv("IOT_OPS_CAPACITY_TOKEN", strings.Repeat("s", 32))
	if ops := Load().Ops; !ops.CapacityLocal || ops.CapacityURL == "" || ops.CapacityToken == "" {
		t.Fatal("local capacity opt-in or configured remote service was lost")
	}
	t.Setenv("IOT_CAPACITY_MODULE", "off")
	if ops := Load().Ops; ops.CapacityLocal || ops.CapacityURL != "" || ops.CapacityToken != "" {
		t.Fatal("explicit opt-out left a capacity service enabled")
	}
	t.Setenv("IOT_CAPACITY_MODULE", "")
	t.Setenv("IOT_OPS_CAPACITY_LOCAL", "")
	if Load().Ops.CapacityLocal {
		t.Fatal("ordinary deployments must not implicitly start an in-process controller")
	}
}

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

func TestAdminTenantAllowlist(t *testing.T) {
	t.Setenv("IOT_ADMIN_TENANTS", "tenant-a, tenant-b")
	cfg := Load()
	if len(cfg.AdminTenants) != 2 || cfg.AdminTenants[0] != "tenant-a" || cfg.AdminTenants[1] != "tenant-b" {
		t.Fatalf("unexpected admin tenant allowlist: %#v", cfg.AdminTenants)
	}
}

func TestProductionConfigRequiresExplicitStrongJWTSecret(t *testing.T) {
	t.Setenv("IOT_AI_HARNESS_URL", testHarnessURL)
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
	t.Setenv("IOT_AI_HARNESS_URL", testHarnessURL)
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
	cfg := Config{DevMode: false, JWTSecret: strings.Repeat("j", 48), AdminPassword: strings.Repeat("p", 20), AIHarnessURL: testHarnessURL}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("explicit configuration values were rejected: %v", err)
	}
}

func TestSplitRolesRequireSharedDependencies(t *testing.T) {
	cfg := Config{DevMode: true, ProcessRole: "api", AIHarnessURL: testHarnessURL}
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
	// Worker roles need the shared stores but only the ai role needs Harness.
	for _, role := range []string{RoleParser, RoleProcessor, RoleJobs} {
		worker := Config{DevMode: true, ProcessRole: role, PostgresDSN: "dsn", KafkaBrokers: []string{"b:9092"}}
		if err := worker.Validate(); err != nil {
			t.Fatal(role, err)
		}
		worker.KafkaBrokers = nil
		if worker.Validate() == nil {
			t.Fatal(role, "accepted process-local queue")
		}
	}
	if (Config{DevMode: true, ProcessRole: RoleAI, PostgresDSN: "dsn", KafkaBrokers: []string{"b:9092"}}).Validate() == nil {
		t.Fatal("ai role accepted without Harness")
	}
	if (Config{DevMode: true, ProcessRole: RoleParser, PostgresDSN: "dsn", KafkaBrokers: []string{"b:9092"}, InstanceID: "bad id!"}).Validate() == nil {
		t.Fatal("invalid instance ID accepted")
	}
}

func TestRoleComponents(t *testing.T) {
	type want struct{ access, management, parser, processor, ai, jobs, aiRuntime bool }
	cases := map[string]struct {
		cfg  Config
		want want
	}{
		"combined":     {Config{ProcessRole: RoleCombined}, want{true, true, true, true, true, true, true}},
		"default":      {Config{}, want{true, true, true, true, true, true, true}},
		"api embedded": {Config{ProcessRole: RoleAPI, APIEmbeddedWorkers: true}, want{false, true, true, true, true, true, true}},
		"api only":     {Config{ProcessRole: RoleAPI}, want{false, true, false, false, false, false, true}},
		"gateway":      {Config{ProcessRole: RoleGateway, APIEmbeddedWorkers: true}, want{true, false, false, false, false, false, false}},
		"parser":       {Config{ProcessRole: RoleParser}, want{false, false, true, false, false, false, false}},
		"processor":    {Config{ProcessRole: RoleProcessor}, want{false, false, false, true, false, false, false}},
		"ai":           {Config{ProcessRole: RoleAI}, want{false, false, false, false, true, false, true}},
		"jobs":         {Config{ProcessRole: RoleJobs}, want{false, false, false, false, false, true, false}},
	}
	for name, c := range cases {
		got := want{c.cfg.Runs(ComponentAccess), c.cfg.Runs(ComponentManagement), c.cfg.Runs(ComponentParser), c.cfg.Runs(ComponentProcessor), c.cfg.Runs(ComponentAI), c.cfg.Runs(ComponentJobs), c.cfg.Runs(ComponentAIRuntime)}
		if got != c.want {
			t.Errorf("%s: got %+v want %+v", name, got, c.want)
		}
	}
	t.Setenv("IOT_INSTANCE_ID", "")
	if cfg := Load(); !cfg.APIEmbeddedWorkers || cfg.InstanceIDExplicit || cfg.InstanceID == "" || !cfg.PublishExternalTopics {
		t.Fatalf("defaults must keep existing deployments unchanged: %+v", cfg)
	}
	t.Setenv("IOT_INSTANCE_ID", "gw-2")
	if cfg := Load(); cfg.InstanceID != "gw-2" || !cfg.InstanceIDExplicit {
		t.Fatal("explicit instance ID ignored")
	}
}

func TestProductionConfigAllowsCustomAdminPasswords(t *testing.T) {
	t.Setenv("IOT_DEV_MODE", "false")
	t.Setenv("IOT_AI_HARNESS_URL", testHarnessURL)
	t.Setenv("IOT_JWT_SECRET", strings.Repeat("j", 48))
	for _, password := range []string{"", "admin123", "1", "自定义密码", "a $!#'", "change-this-password"} {
		t.Run(password, func(t *testing.T) {
			t.Setenv("IOT_ADMIN_PASSWORD", password)
			cfg := Load()
			expected := password
			if expected == "" {
				expected = "admin123"
			}
			if cfg.AdminPassword != expected {
				t.Fatal("admin password was not preserved or defaulted correctly")
			}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("custom admin password rejected: %v", err)
			}
		})
	}
	cfg := Load()
	cfg.AdminPassword = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("explicit empty admin password accepted")
	}
}

const testHarnessURL = "http://deepseek-harness:8091"

func TestHarnessIsRequiredExceptForAccessGateway(t *testing.T) {
	cfg := Config{DevMode: true}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "IOT_AI_HARNESS_URL") {
		t.Fatalf("combined role accepted a missing Harness: %v", err)
	}
	cfg.AIHarnessURL = "ftp://harness"
	if cfg.Validate() == nil {
		t.Fatal("non-HTTP Harness URL was accepted")
	}
	gateway := Config{DevMode: true, ProcessRole: "gateway", PostgresDSN: "dsn", KafkaBrokers: []string{"broker:9092"}}
	if err := gateway.Validate(); err != nil {
		t.Fatalf("access gateway must not require Harness: %v", err)
	}
}

func TestDefaultDeepSeekDoesNotRequireKeyAtConfigLoad(t *testing.T) {
	t.Setenv("IOT_AI_PROVIDER", "")
	t.Setenv("IOT_AI_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("IOT_OLLAMA_MODEL", "")
	cfg := Load()
	if cfg.AIProvider != "deepseek" || cfg.AIAPIKey != "" || cfg.OllamaModel != "" {
		t.Fatal("first installation must select DeepSeek without a bundled chat model or key")
	}
}

func TestLoadEnvFileLocalConfiguration(t *testing.T) {
	for _, key := range []string{"IOT_TEST_DSN", "IOT_TEST_LITERAL", "IOT_TEST_EMPTY", "IOT_TEST_OVERRIDE", "IOT_TEST_LAST"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("IOT_TEST_OVERRIDE", "from-process")
	t.Setenv("IOT_TEST_EMPTY", "")
	path := filepath.Join(t.TempDir(), "local.env")
	contents := "\ufeff# local config\r\nIOT_TEST_DSN=postgres://iot:abc@127.0.0.1:15432/iot?sslmode=disable\r\n" +
		"IOT_TEST_LITERAL='a$HOME#b=c' # literal secret\nIOT_TEST_EMPTY=file-value\nIOT_TEST_OVERRIDE=file-value\n" +
		"IOT_TEST_LAST=old\nIOT_TEST_LAST=\"new value\"\n"
	if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	if err := LoadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	for key, expected := range map[string]string{
		"IOT_TEST_DSN":     "postgres://iot:abc@127.0.0.1:15432/iot?sslmode=disable",
		"IOT_TEST_LITERAL": "a$HOME#b=c", "IOT_TEST_EMPTY": "", "IOT_TEST_OVERRIDE": "from-process", "IOT_TEST_LAST": "new value",
	} {
		if os.Getenv(key) != expected {
			t.Errorf("unexpected value for %s", key)
		}
	}
}

func TestLoadEnvFileRejectsInvalidInputWithoutLeakingValues(t *testing.T) {
	for _, invalid := range []string{"bad line secret-value", "9KEY=secret-value", "IOT_TEST_BAD='secret-value", "IOT_TEST_BAD=\"secret-value\" trailing", "IOT_TEST_BAD=secret-value\x00"} {
		t.Run(invalid[:4], func(t *testing.T) {
			t.Setenv("IOT_TEST_ATOMIC", "")
			if err := os.Unsetenv("IOT_TEST_ATOMIC"); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "bad.env")
			if err := os.WriteFile(path, []byte("IOT_TEST_ATOMIC=must-not-load\n"+invalid), 0600); err != nil {
				t.Fatal(err)
			}
			err := LoadEnvFile(path)
			if err == nil || !strings.Contains(err.Error(), "line 2") {
				t.Fatalf("expected line error, got %v", err)
			}
			if strings.Contains(err.Error(), "secret-value") {
				t.Fatal("error leaked a secret")
			}
			if _, exists := os.LookupEnv("IOT_TEST_ATOMIC"); exists {
				t.Fatal("invalid file was partially applied")
			}
		})
	}
}

func TestLoadEnvFileRequiresExistingFile(t *testing.T) {
	if err := LoadEnvFile(filepath.Join(t.TempDir(), "missing.env")); err == nil {
		t.Fatal("missing file accepted")
	}
}
