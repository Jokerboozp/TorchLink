package config /* 声明 config 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"strings" /* 执行当前语句并推进处理流程。 */
	"testing" /* 执行当前语句并推进处理流程。 */
	"time"    /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

func TestAIKeyFallbackIsProviderScoped(t *testing.T) { /* 定义 TestAIKeyFallbackIsProviderScoped 函数。 */
	t.Setenv("IOT_AI_PROVIDER", "openai-compatible") /* 执行当前语句并推进处理流程。 */
	t.Setenv("IOT_AI_API_KEY", "")                   /* 执行当前语句并推进处理流程。 */
	t.Setenv("DEEPSEEK_API_KEY", "deepseek-secret")  /* 执行当前语句并推进处理流程。 */
	if got := Load().AIAPIKey; got != "" {           /* 判断条件并选择处理分支。 */
		t.Fatalf("DeepSeek key leaked into generic provider config: %q", got) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	t.Setenv("IOT_AI_PROVIDER", "deepseek")               /* 执行当前语句并推进处理流程。 */
	if got := Load().AIAPIKey; got != "deepseek-secret" { /* 判断条件并选择处理分支。 */
		t.Fatalf("DeepSeek provider key=%q", got) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	t.Setenv("IOT_AI_API_KEY", "generic-secret")         /* 执行当前语句并推进处理流程。 */
	if got := Load().AIAPIKey; got != "generic-secret" { /* 判断条件并选择处理分支。 */
		t.Fatalf("generic provider key did not take precedence: %q", got) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestDeepSeekKeyEnablesProviderWhenProviderIsUnset(t *testing.T) { /* 定义 TestDeepSeekKeyEnablesProviderWhenProviderIsUnset 函数。 */
	t.Setenv("IOT_AI_PROVIDER", "")                 /* 执行当前语句并推进处理流程。 */
	t.Setenv("IOT_AI_API_KEY", "")                  /* 执行当前语句并推进处理流程。 */
	t.Setenv("DEEPSEEK_API_KEY", "deepseek-secret") /* 执行当前语句并推进处理流程。 */

	cfg := Load()                     /* 更新 cfg 的值。 */
	if cfg.AIProvider != "deepseek" { /* 判断条件并选择处理分支。 */
		t.Fatalf("AI provider=%q, want deepseek", cfg.AIProvider) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	if cfg.AIAPIKey != "deepseek-secret" { /* 判断条件并选择处理分支。 */
		t.Fatalf("AI API key=%q", cfg.AIAPIKey) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestHarnessConfiguration(t *testing.T) { /* 定义 TestHarnessConfiguration 函数。 */
	t.Setenv("IOT_AI_HARNESS_URL", "https://harness.example/")                                                                                                                                                                                 /* 执行当前语句并推进处理流程。 */
	t.Setenv("IOT_AI_HARNESS_TOKEN", "service-token")                                                                                                                                                                                          /* 执行当前语句并推进处理流程。 */
	t.Setenv("IOT_AI_HARNESS_MCP_URL", "https://api.example/mcp/harness")                                                                                                                                                                      /* 执行当前语句并推进处理流程。 */
	t.Setenv("IOT_AI_HARNESS_MODEL", "deepseek-chat")                                                                                                                                                                                          /* 执行当前语句并推进处理流程。 */
	t.Setenv("IOT_AI_HARNESS_TIMEOUT", "45s")                                                                                                                                                                                                  /* 执行当前语句并推进处理流程。 */
	cfg := Load()                                                                                                                                                                                                                              /* 更新 cfg 的值。 */
	if cfg.AIHarnessURL != "https://harness.example" || cfg.AIHarnessToken != "service-token" || cfg.AIHarnessMCPURL != "https://api.example/mcp/harness" || cfg.AIHarnessModel != "deepseek-chat" || cfg.AIHarnessTimeout != 45*time.Second { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected harness configuration: %#v", cfg) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestVideoPlatformTenantBindings(t *testing.T) { /* 定义 TestVideoPlatformTenantBindings 函数。 */
	t.Setenv("IOT_VIDEO_PLATFORM_TENANTS", "video-a:tenant-a,video-b:tenant-b")                                 /* 执行当前语句并推进处理流程。 */
	cfg := Load()                                                                                               /* 更新 cfg 的值。 */
	if cfg.VideoPlatformTenants["video-a"] != "tenant-a" || cfg.VideoPlatformTenants["video-b"] != "tenant-b" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected video tenant bindings: %#v", cfg.VideoPlatformTenants) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestHikvisionArtemisConfig(t *testing.T) { /* 定义 TestHikvisionArtemisConfig 函数。 */
	t.Setenv("IOT_VIDEO_HIKVISION_API_URL", "https://hikcentral.example.internal")                                                                       /* 执行当前语句并推进处理流程。 */
	t.Setenv("IOT_VIDEO_HIKVISION_APP_KEY", "app-key")                                                                                                   /* 执行当前语句并推进处理流程。 */
	t.Setenv("IOT_VIDEO_HIKVISION_APP_SECRET", "app-secret")                                                                                             /* 执行当前语句并推进处理流程。 */
	cfg := Load()                                                                                                                                        /* 更新 cfg 的值。 */
	if cfg.HikvisionVideoAPIURL != "https://hikcentral.example.internal" || cfg.HikvisionAppKey != "app-key" || cfg.HikvisionAppSecret != "app-secret" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected Hikvision Artemis config: %#v", cfg) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestAdminTenantAllowlist(t *testing.T) { /* 定义 TestAdminTenantAllowlist 函数。 */
	t.Setenv("IOT_ADMIN_TENANTS", "tenant-a, tenant-b")                                                       /* 执行当前语句并推进处理流程。 */
	cfg := Load()                                                                                             /* 更新 cfg 的值。 */
	if len(cfg.AdminTenants) != 2 || cfg.AdminTenants[0] != "tenant-a" || cfg.AdminTenants[1] != "tenant-b" { /* 判断条件并选择处理分支。 */
		t.Fatalf("unexpected admin tenant allowlist: %#v", cfg.AdminTenants) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestProductionConfigRequiresExplicitStrongJWTSecret(t *testing.T) { /* 定义 TestProductionConfigRequiresExplicitStrongJWTSecret 函数。 */
	t.Setenv("IOT_DEV_MODE", "false")         /* 执行当前语句并推进处理流程。 */
	t.Setenv("IOT_JWT_SECRET", "")            /* 执行当前语句并推进处理流程。 */
	t.Setenv("IOT_ADMIN_PASSWORD", "")        /* 执行当前语句并推进处理流程。 */
	if err := Load().Validate(); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("production configuration accepted built-in authentication fallbacks") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	t.Setenv("IOT_JWT_SECRET", strings.Repeat("j", 48))     /* 执行当前语句并推进处理流程。 */
	t.Setenv("IOT_ADMIN_PASSWORD", strings.Repeat("p", 20)) /* 执行当前语句并推进处理流程。 */
	if err := Load().Validate(); err != nil {               /* 判断条件并选择处理分支。 */
		t.Fatalf("production configuration rejected explicit strong secrets: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestDevelopmentConfigAllowsLocalFallbacks(t *testing.T) { /* 定义 TestDevelopmentConfigAllowsLocalFallbacks 函数。 */
	t.Setenv("IOT_DEV_MODE", "true")          /* 执行当前语句并推进处理流程。 */
	t.Setenv("IOT_JWT_SECRET", "")            /* 执行当前语句并推进处理流程。 */
	t.Setenv("IOT_ADMIN_PASSWORD", "")        /* 执行当前语句并推进处理流程。 */
	if err := Load().Validate(); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatalf("development configuration rejected local fallbacks: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestProductionConfigRejectsPlaceholderSecretsAndInvalidMode(t *testing.T) { /* 定义 TestProductionConfigRejectsPlaceholderSecretsAndInvalidMode 函数。 */
	t.Setenv("IOT_DEV_MODE", "false")                                     /* 执行当前语句并推进处理流程。 */
	t.Setenv("IOT_JWT_SECRET", "change-this-"+strings.Repeat("j", 48))    /* 执行当前语句并推进处理流程。 */
	t.Setenv("IOT_ADMIN_PASSWORD", "replace-me-"+strings.Repeat("p", 20)) /* 执行当前语句并推进处理流程。 */
	if err := Load().Validate(); err == nil {                             /* 判断条件并选择处理分支。 */
		t.Fatal("production configuration accepted placeholder secrets") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */

	t.Setenv("IOT_DEV_MODE", "not-a-boolean")               /* 执行当前语句并推进处理流程。 */
	t.Setenv("IOT_JWT_SECRET", strings.Repeat("j", 48))     /* 执行当前语句并推进处理流程。 */
	t.Setenv("IOT_ADMIN_PASSWORD", strings.Repeat("p", 20)) /* 执行当前语句并推进处理流程。 */
	if err := Load().Validate(); err == nil {               /* 判断条件并选择处理分支。 */
		t.Fatal("configuration accepted an invalid IOT_DEV_MODE value") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestExplicitConfigValueCanBeValidatedWithoutEnvironmentProvenance(t *testing.T) { /* 定义 TestExplicitConfigValueCanBeValidatedWithoutEnvironmentProvenance 函数。 */
	cfg := Config{DevMode: false, JWTSecret: strings.Repeat("j", 48), AdminPassword: strings.Repeat("p", 20)} /* 更新 cfg 的值。 */
	if err := cfg.Validate(); err != nil {                                                                    /* 判断条件并选择处理分支。 */
		t.Fatalf("explicit configuration values were rejected: %v", err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestSplitRolesRequireSharedDependencies(t *testing.T) { /* 定义 TestSplitRolesRequireSharedDependencies 函数。 */
	cfg := Config{DevMode: true, ProcessRole: "api"} /* 更新 cfg 的值。 */
	if cfg.Validate() == nil {                       /* 判断条件并选择处理分支。 */
		t.Fatal("split mode accepted process-local storage") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg.PostgresDSN = "test-dsn"               /* 更新 cfg.PostgresDSN 的值。 */
	cfg.KafkaBrokers = []string{"broker:9092"} /* 更新 cfg.KafkaBrokers 的值。 */
	if cfg.Validate() == nil {                 /* 判断条件并选择处理分支。 */
		t.Fatal("API accepted missing gateway") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg.AccessGatewayURL = "http://gateway:8080" /* 更新 cfg.AccessGatewayURL 的值。 */
	if err := cfg.Validate(); err != nil {       /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg.AccessGatewayURL = "http://user:password@gateway:8080" /* 更新 cfg.AccessGatewayURL 的值。 */
	if cfg.Validate() == nil {                                 /* 判断条件并选择处理分支。 */
		t.Fatal("gateway URL accepted inline credentials") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg.AccessGatewayURL = ""              /* 更新 cfg.AccessGatewayURL 的值。 */
	cfg.ProcessRole = "gateway"            /* 更新 cfg.ProcessRole 的值。 */
	if err := cfg.Validate(); err != nil { /* 判断条件并选择处理分支。 */
		t.Fatal(err) /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
	cfg.ProcessRole = "other"  /* 更新 cfg.ProcessRole 的值。 */
	if cfg.Validate() == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("unknown role accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func TestProductionConfigAllowsCustomAdminPasswords(t *testing.T) { /* 定义 TestProductionConfigAllowsCustomAdminPasswords 函数。 */
	t.Setenv("IOT_DEV_MODE", "false")                                                                   /* 执行当前语句并推进处理流程。 */
	t.Setenv("IOT_JWT_SECRET", strings.Repeat("j", 48))                                                 /* 执行当前语句并推进处理流程。 */
	for _, password := range []string{"", "admin123", "1", "自定义密码", "a $!#'", "change-this-password"} { /* 循环处理当前数据。 */
		t.Run(password, func(t *testing.T) { /* 执行当前语句并推进处理流程。 */
			t.Setenv("IOT_ADMIN_PASSWORD", password) /* 执行当前语句并推进处理流程。 */
			cfg := Load()                            /* 更新 cfg 的值。 */
			expected := password                     /* 更新 expected 的值。 */
			if expected == "" {                      /* 判断条件并选择处理分支。 */
				expected = "admin123" /* 更新 expected 的值。 */
			} /* 结束当前表达式或代码块。 */
			if cfg.AdminPassword != expected { /* 判断条件并选择处理分支。 */
				t.Fatal("admin password was not preserved or defaulted correctly") /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
			if err := cfg.Validate(); err != nil { /* 判断条件并选择处理分支。 */
				t.Fatalf("custom admin password rejected: %v", err) /* 验证实际结果符合预期。 */
			} /* 结束当前表达式或代码块。 */
		}) /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	cfg := Load()                          /* 更新 cfg 的值。 */
	cfg.AdminPassword = ""                 /* 更新 cfg.AdminPassword 的值。 */
	if err := cfg.Validate(); err == nil { /* 判断条件并选择处理分支。 */
		t.Fatal("explicit empty admin password accepted") /* 验证实际结果符合预期。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
