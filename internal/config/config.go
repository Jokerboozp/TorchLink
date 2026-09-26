package config /* 声明 config 包。 */

import ( /* 引入当前代码需要的依赖。 */
	"fmt"     /* 执行当前语句并推进处理流程。 */
	"net/url" /* 执行当前语句并推进处理流程。 */
	"os"      /* 执行当前语句并推进处理流程。 */
	"strconv" /* 执行当前语句并推进处理流程。 */
	"strings" /* 执行当前语句并推进处理流程。 */
	"time"    /* 执行当前语句并推进处理流程。 */
) /* 结束当前表达式或代码块。 */

const ( /* 执行当前语句并推进处理流程。 */
	defaultJWTSecret     = "change-me-in-production" /* 更新 defaultJWTSecret 的值。 */
	defaultAdminPassword = "admin123"                /* 更新 defaultAdminPassword 的值。 */
) /* 结束当前表达式或代码块。 */

type Config struct { /* 定义 Config 类型。 */
	AccessCoordination          bool     /* 执行当前语句并推进处理流程。 */
	AccessNodeURL               string   /* 执行当前语句并推进处理流程。 */
	ProcessRole                 string   /* 执行当前语句并推进处理流程。 */
	AccessGatewayURL            string   /* 执行当前语句并推进处理流程。 */
	HTTPAddr                    string   /* 执行当前语句并推进处理流程。 */
	CORSAllowedOrigins          []string /* 执行当前语句并推进处理流程。 */
	DataDir                     string   /* 执行当前语句并推进处理流程。 */
	JWTSecret                   string   /* 执行当前语句并推进处理流程。 */
	AdminUser                   string   /* 执行当前语句并推进处理流程。 */
	AdminPassword               string   /* 执行当前语句并推进处理流程。 */
	AdminTenants                []string /* 执行当前语句并推进处理流程。 */
	PostgresDSN                 string   /* 执行当前语句并推进处理流程。 */
	RedisAddr                   string   /* 执行当前语句并推进处理流程。 */
	RedisPassword               string   /* 执行当前语句并推进处理流程。 */
	ClickHouseURL               string   /* 执行当前语句并推进处理流程。 */
	RawHighFrequencyIntervalSec int64    /* 执行当前语句并推进处理流程。 */
	// KafkaConsumerConcurrency is the parallel lanes per Kafka subscription;
	// messages of one device keep their order within a lane.
	KafkaConsumerConcurrency int64
	// AIAnalysisConcurrency bounds automatic alarm analyses running at once.
	AIAnalysisConcurrency int64
	MinIOEndpoint         string            /* 执行当前语句并推进处理流程。 */
	MinIOAccessKey        string            /* 执行当前语句并推进处理流程。 */
	MinIOSecretKey        string            /* 执行当前语句并推进处理流程。 */
	MinIOUseTLS           bool              /* 执行当前语句并推进处理流程。 */
	KafkaBrokers          []string          /* 执行当前语句并推进处理流程。 */
	EMQXAPIURL            string            /* 执行当前语句并推进处理流程。 */
	EMQXAPIKey            string            /* 执行当前语句并推进处理流程。 */
	EMQXAPISecret         string            /* 执行当前语句并推进处理流程。 */
	MQTTBroker            string            /* 执行当前语句并推进处理流程。 */
	MQTTUsername          string            /* 执行当前语句并推进处理流程。 */
	MQTTPassword          string            /* 执行当前语句并推进处理流程。 */
	MQTTWebSocketURL      string            /* 执行当前语句并推进处理流程。 */
	MQTTPublicURL         string            /* 执行当前语句并推进处理流程。 */
	DeviceHTTPPublicURL   string            /* 执行当前语句并推进处理流程。 */
	OllamaURL             string            /* 执行当前语句并推进处理流程。 */
	OllamaModel           string            /* 执行当前语句并推进处理流程。 */
	AIProvider            string            /* 执行当前语句并推进处理流程。 */
	AIBaseURL             string            /* 执行当前语句并推进处理流程。 */
	AIModel               string            /* 执行当前语句并推进处理流程。 */
	AIAPIKey              string            /* 执行当前语句并推进处理流程。 */
	AIHarnessURL          string            /* 执行当前语句并推进处理流程。 */
	AIHarnessToken        string            /* 执行当前语句并推进处理流程。 */
	AIHarnessMCPURL       string            /* 执行当前语句并推进处理流程。 */
	AIHarnessModel        string            /* 执行当前语句并推进处理流程。 */
	AIHarnessTimeout      time.Duration     /* 执行当前语句并推进处理流程。 */
	AITestOllamaURL       string            /* 执行当前语句并推进处理流程。 */
	WeaviateURL           string            /* 执行当前语句并推进处理流程。 */
	BackupURL             string            /* 执行当前语句并推进处理流程。 */
	BackupToken           string            /* 执行当前语句并推进处理流程。 */
	VideoSecrets          map[string]string /* 执行当前语句并推进处理流程。 */
	VideoPlatformTenants  map[string]string /* 执行当前语句并推进处理流程。 */
	VideoMediaHosts       []string          /* 执行当前语句并推进处理流程。 */
	OfflineScan           time.Duration     /* 执行当前语句并推进处理流程。 */
	ModbusAllowedCIDRs    []string          /* 执行当前语句并推进处理流程。 */
	DevMode               bool              /* 执行当前语句并推进处理流程。 */
	Ops                   OpsConfig
	loadErr               error /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

func Load() Config { /* 定义 Load 函数。 */
	devMode, devModeErr := strictBoolValue("IOT_DEV_MODE", true)                   /* 更新 devModeErr 的值。 */
	aiProvider := strings.ToLower(strings.TrimSpace(os.Getenv("IOT_AI_PROVIDER"))) /* 更新 aiProvider 的值。 */
	deepSeekAPIKey := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))             /* 更新 deepSeekAPIKey 的值。 */
	if aiProvider == "" {                                                          /* 判断条件并选择处理分支。 */
		aiProvider = "deepseek" /* 更新 aiProvider 的值。 */
	} /* 结束当前表达式或代码块。 */
	aiAPIKey := strings.TrimSpace(os.Getenv("IOT_AI_API_KEY")) /* 更新 aiAPIKey 的值。 */
	if aiAPIKey == "" && aiProvider == "deepseek" {            /* 判断条件并选择处理分支。 */
		aiAPIKey = deepSeekAPIKey /* 更新 aiAPIKey 的值。 */
	} /* 结束当前表达式或代码块。 */
	return Config{ /* 返回当前处理结果。 */
		AccessCoordination:          boolValue("IOT_ACCESS_COORDINATION", false),                 /* 执行当前语句并推进处理流程。 */
		AccessNodeURL:               strings.TrimRight(os.Getenv("IOT_ACCESS_NODE_URL"), "/"),    /* 执行当前语句并推进处理流程。 */
		ProcessRole:                 strings.ToLower(get("IOT_PROCESS_ROLE", "combined")),        /* 执行当前语句并推进处理流程。 */
		AccessGatewayURL:            strings.TrimRight(os.Getenv("IOT_ACCESS_GATEWAY_URL"), "/"), /* 执行当前语句并推进处理流程。 */
		HTTPAddr:                    get("IOT_HTTP_ADDR", ":8080"),                               /* 执行当前语句并推进处理流程。 */
		CORSAllowedOrigins:          split(os.Getenv("IOT_CORS_ALLOWED_ORIGINS")),                /* 执行当前语句并推进处理流程。 */
		DataDir:                     get("IOT_DATA_DIR", "./data"),                               /* 执行当前语句并推进处理流程。 */
		JWTSecret:                   get("IOT_JWT_SECRET", defaultJWTSecret),                     /* 执行当前语句并推进处理流程。 */
		AdminUser:                   get("IOT_ADMIN_USER", "admin"),                              /* 执行当前语句并推进处理流程。 */
		AdminPassword:               get("IOT_ADMIN_PASSWORD", defaultAdminPassword),             /* 执行当前语句并推进处理流程。 */
		AdminTenants:                split(get("IOT_ADMIN_TENANTS", "tenant_001")),               /* 执行当前语句并推进处理流程。 */
		PostgresDSN:                 os.Getenv("IOT_POSTGRES_DSN"),                               /* 执行当前语句并推进处理流程。 */
		RedisAddr:                   os.Getenv("IOT_REDIS_ADDR"),                                 /* 执行当前语句并推进处理流程。 */
		RedisPassword:               os.Getenv("IOT_REDIS_PASSWORD"),                             /* 执行当前语句并推进处理流程。 */
		ClickHouseURL:               os.Getenv("IOT_CLICKHOUSE_URL"),                             /* 执行当前语句并推进处理流程。 */
		RawHighFrequencyIntervalSec: int64Value("IOT_RAW_HIGH_FREQUENCY_INTERVAL_SEC", 60),
		KafkaConsumerConcurrency:    int64Value("IOT_KAFKA_CONSUMER_CONCURRENCY", 8),
		AIAnalysisConcurrency:       int64Value("IOT_AI_ANALYSIS_CONCURRENCY", 1),                                                                   /* 执行当前语句并推进处理流程。 */
		MinIOEndpoint:               os.Getenv("IOT_MINIO_ENDPOINT"),                                                                                /* 执行当前语句并推进处理流程。 */
		MinIOAccessKey:              os.Getenv("IOT_MINIO_ACCESS_KEY"),                                                                              /* 执行当前语句并推进处理流程。 */
		MinIOSecretKey:              os.Getenv("IOT_MINIO_SECRET_KEY"),                                                                              /* 执行当前语句并推进处理流程。 */
		MinIOUseTLS:                 boolValue("IOT_MINIO_USE_TLS", false),                                                                          /* 执行当前语句并推进处理流程。 */
		KafkaBrokers:                split(os.Getenv("IOT_KAFKA_BROKERS")),                                                                          /* 执行当前语句并推进处理流程。 */
		EMQXAPIURL:                  os.Getenv("IOT_EMQX_API_URL"),                                                                                  /* 执行当前语句并推进处理流程。 */
		EMQXAPIKey:                  os.Getenv("IOT_EMQX_API_KEY"),                                                                                  /* 执行当前语句并推进处理流程。 */
		EMQXAPISecret:               os.Getenv("IOT_EMQX_API_SECRET"),                                                                               /* 执行当前语句并推进处理流程。 */
		MQTTBroker:                  os.Getenv("IOT_MQTT_BROKER"),                                                                                   /* 执行当前语句并推进处理流程。 */
		MQTTUsername:                os.Getenv("IOT_MQTT_USERNAME"),                                                                                 /* 执行当前语句并推进处理流程。 */
		MQTTPassword:                os.Getenv("IOT_MQTT_PASSWORD"),                                                                                 /* 执行当前语句并推进处理流程。 */
		MQTTWebSocketURL:            os.Getenv("IOT_MQTT_WEBSOCKET_PUBLIC_URL"),                                                                     /* 执行当前语句并推进处理流程。 */
		MQTTPublicURL:               os.Getenv("IOT_DEVICE_MQTT_PUBLIC_URL"),                                                                        /* 执行当前语句并推进处理流程。 */
		DeviceHTTPPublicURL:         os.Getenv("IOT_DEVICE_HTTP_PUBLIC_URL"),                                                                        /* 执行当前语句并推进处理流程。 */
		OllamaURL:                   get("IOT_OLLAMA_URL", "http://localhost:11434"),                                                                /* 执行当前语句并推进处理流程。 */
		OllamaModel:                 strings.TrimSpace(os.Getenv("IOT_OLLAMA_MODEL")),                                                               /* 执行当前语句并推进处理流程。 */
		AIProvider:                  aiProvider,                                                                                                     /* 执行当前语句并推进处理流程。 */
		AIBaseURL:                   strings.TrimRight(os.Getenv("IOT_AI_BASE_URL"), "/"),                                                           /* 执行当前语句并推进处理流程。 */
		AIModel:                     strings.TrimSpace(os.Getenv("IOT_AI_MODEL")),                                                                   /* 执行当前语句并推进处理流程。 */
		AIAPIKey:                    aiAPIKey,                                                                                                       /* 执行当前语句并推进处理流程。 */
		AIHarnessURL:                strings.TrimRight(strings.TrimSpace(os.Getenv("IOT_AI_HARNESS_URL")), "/"),                                     /* 执行当前语句并推进处理流程。 */
		AIHarnessToken:              strings.TrimSpace(os.Getenv("IOT_AI_HARNESS_TOKEN")),                                                           /* 执行当前语句并推进处理流程。 */
		AIHarnessMCPURL:             strings.TrimSpace(os.Getenv("IOT_AI_HARNESS_MCP_URL")),                                                         /* 执行当前语句并推进处理流程。 */
		AIHarnessModel:              strings.TrimSpace(os.Getenv("IOT_AI_HARNESS_MODEL")),                                                           /* 执行当前语句并推进处理流程。 */
		AIHarnessTimeout:            duration("IOT_AI_HARNESS_TIMEOUT", 90*time.Second),                                                             /* 执行当前语句并推进处理流程。 */
		AITestOllamaURL:             get("IOT_AI_OLLAMA_URL", get("IOT_OLLAMA_URL", "http://localhost:11434")),                                      /* 执行当前语句并推进处理流程。 */
		WeaviateURL:                 os.Getenv("IOT_WEAVIATE_URL"),                                                                                  /* 执行当前语句并推进处理流程。 */
		BackupURL:                   strings.TrimRight(strings.TrimSpace(os.Getenv("IOT_BACKUP_URL")), "/"),                                         /* 执行当前语句并推进处理流程。 */
		BackupToken:                 strings.TrimSpace(os.Getenv("IOT_BACKUP_ADMIN_TOKEN")),                                                         /* 执行当前语句并推进处理流程。 */
		VideoSecrets:                parsePairs(os.Getenv("IOT_VIDEO_PLATFORM_SECRETS")),                                                            /* 执行当前语句并推进处理流程。 */
		VideoPlatformTenants:        parsePairs(os.Getenv("IOT_VIDEO_PLATFORM_TENANTS")),                                                            /* 执行当前语句并推进处理流程。 */
		VideoMediaHosts:             split(os.Getenv("IOT_VIDEO_MEDIA_ALLOWED_HOSTS")),                                                              /* 执行当前语句并推进处理流程。 */
		OfflineScan:                 duration("IOT_OFFLINE_SCAN_INTERVAL", 30*time.Second),                                                          /* 执行当前语句并推进处理流程。 */
		ModbusAllowedCIDRs:          split(get("IOT_MODBUS_ALLOWED_CIDRS", "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,127.0.0.0/8,fc00::/7,::1/128")), /* 执行当前语句并推进处理流程。 */
		DevMode:                     devMode,                                                                                                        /* 执行当前语句并推进处理流程。 */
		Ops:                         loadOps(),
		loadErr:                     devModeErr, /* 执行当前语句并推进处理流程。 */
	} /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

func (c Config) Validate() error { /* 定义 Validate 函数。 */
	if c.loadErr != nil { /* 判断条件并选择处理分支。 */
		return c.loadErr /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if err := c.Ops.validate(); err != nil {
		return err
	}
	if c.AccessCoordination && (c.PostgresDSN == "" || c.AccessNodeURL == "") { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("access coordination requires PostgreSQL and IOT_ACCESS_NODE_URL") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	switch c.ProcessRole { /* 根据条件选择处理路径。 */
	case "", "combined": /* 处理当前分支。 */
	case "api", "gateway": /* 处理当前分支。 */
		if c.PostgresDSN == "" || len(c.KafkaBrokers) == 0 { /* 判断条件并选择处理分支。 */
			return fmt.Errorf("split process roles require shared IOT_POSTGRES_DSN and IOT_KAFKA_BROKERS") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
		if c.ProcessRole == "api" && c.AccessGatewayURL == "" { /* 判断条件并选择处理分支。 */
			return fmt.Errorf("api role requires IOT_ACCESS_GATEWAY_URL") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	default: /* 处理当前分支。 */
		return fmt.Errorf("IOT_PROCESS_ROLE must be combined, api or gateway") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	if c.KafkaConsumerConcurrency < 0 || c.KafkaConsumerConcurrency > 64 {
		return fmt.Errorf("IOT_KAFKA_CONSUMER_CONCURRENCY must be between 1 and 64 (0 uses the default 8)")
	}
	if c.AIAnalysisConcurrency < 0 || c.AIAnalysisConcurrency > 32 {
		return fmt.Errorf("IOT_AI_ANALYSIS_CONCURRENCY must be between 1 and 32 (0 uses the default 1)")
	}
	// Every business AI feature runs as a Harness workflow; only the access
	// gateway, which serves no AI features, may run without it.
	if c.ProcessRole != "gateway" {
		if c.AIHarnessURL == "" {
			return fmt.Errorf("IOT_AI_HARNESS_URL is required: the AI workflow Harness is a mandatory component")
		}
		if u, err := url.Parse(c.AIHarnessURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return fmt.Errorf("IOT_AI_HARNESS_URL must be an HTTP(S) URL without credentials")
		}
	}
	for _, origin := range []string{c.AccessGatewayURL, c.AccessNodeURL} { /* 循环处理当前数据。 */
		if origin == "" { /* 判断条件并选择处理分支。 */
			continue /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
		u, err := url.Parse(origin)                                                                                                                                                /* 更新 err 的值。 */
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") { /* 判断条件并选择处理分支。 */
			return fmt.Errorf("gateway/node URL must be an HTTP(S) origin without credentials") /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	if c.DevMode { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	var invalid []string                                                                              /* 声明 invalid。 */
	if c.JWTSecret == defaultJWTSecret || len(c.JWTSecret) < 32 || insecurePlaceholder(c.JWTSecret) { /* 判断条件并选择处理分支。 */
		invalid = append(invalid, "IOT_JWT_SECRET must be explicitly set to at least 32 characters and must not be a placeholder") /* 更新 invalid 的值。 */
	} /* 结束当前表达式或代码块。 */
	if c.AdminPassword == "" { /* 判断条件并选择处理分支。 */
		invalid = append(invalid, "IOT_ADMIN_PASSWORD must not be empty") /* 更新 invalid 的值。 */
	} /* 结束当前表达式或代码块。 */
	if len(invalid) > 0 { /* 判断条件并选择处理分支。 */
		return fmt.Errorf("invalid production security configuration: %s", strings.Join(invalid, "; ")) /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func strictBoolValue(name string, fallback bool) (bool, error) { /* 定义 strictBoolValue 函数。 */
	value := os.Getenv(name) /* 更新 value 的值。 */
	if value == "" {         /* 判断条件并选择处理分支。 */
		return fallback, nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	parsed, err := strconv.ParseBool(value) /* 更新 err 的值。 */
	if err != nil {                         /* 判断条件并选择处理分支。 */
		return false, fmt.Errorf("invalid production security configuration: IOT_DEV_MODE must be true or false") /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return parsed, nil /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func insecurePlaceholder(value string) bool { /* 定义 insecurePlaceholder 函数。 */
	normalized := strings.ToLower(strings.TrimSpace(value))                                                                     /* 更新 normalized 的值。 */
	for _, marker := range []string{"change-me", "change-this", "replace_me", "replace-me", "local-iot-", "public-change-me"} { /* 循环处理当前数据。 */
		if strings.Contains(normalized, marker) { /* 判断条件并选择处理分支。 */
			return true /* 返回当前处理结果。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return false /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

func get(name, fallback string) string { /* 定义 get 函数。 */
	if v := os.Getenv(name); v != "" { /* 判断条件并选择处理分支。 */
		return v /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return fallback /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func split(v string) []string { /* 定义 split 函数。 */
	if strings.TrimSpace(v) == "" { /* 判断条件并选择处理分支。 */
		return nil /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	values := strings.Split(v, ",")       /* 更新 values 的值。 */
	out := make([]string, 0, len(values)) /* 更新 out 的值。 */
	for _, value := range values {        /* 循环处理当前数据。 */
		if value = strings.TrimSpace(value); value != "" { /* 判断条件并选择处理分支。 */
			out = append(out, value) /* 更新 out 的值。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return out /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func boolValue(name string, fallback bool) bool { /* 定义 boolValue 函数。 */
	v, err := strconv.ParseBool(os.Getenv(name)) /* 更新 err 的值。 */
	if err != nil {                              /* 判断条件并选择处理分支。 */
		return fallback /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return v /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func int64Value(name string, fallback int64) int64 { /* 定义 int64Value 函数。 */
	v, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(name)), 10, 64) /* 更新 err 的值。 */
	if err != nil || v <= 0 {                                              /* 判断条件并选择处理分支。 */
		return fallback /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return v /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func duration(name string, fallback time.Duration) time.Duration { /* 定义 duration 函数。 */
	v, err := time.ParseDuration(os.Getenv(name)) /* 更新 err 的值。 */
	if err != nil {                               /* 判断条件并选择处理分支。 */
		return fallback /* 返回当前处理结果。 */
	} /* 结束当前表达式或代码块。 */
	return v /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
func parsePairs(v string) map[string]string { /* 定义 parsePairs 函数。 */
	out := map[string]string{}                   /* 更新 out 的值。 */
	for _, pair := range strings.Split(v, ",") { /* 循环处理当前数据。 */
		p := strings.SplitN(pair, ":", 2) /* 更新 p 的值。 */
		if len(p) == 2 {                  /* 判断条件并选择处理分支。 */
			out[strings.TrimSpace(p[0])] = strings.TrimSpace(p[1]) /* 执行当前语句并推进处理流程。 */
		} /* 结束当前表达式或代码块。 */
	} /* 结束当前表达式或代码块。 */
	return out /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
