package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultJWTSecret     = "change-me-in-production"
	defaultAdminPassword = "admin123"
)

type Config struct {
	AccessCoordination          bool
	AccessNodeURL               string
	ProcessRole                 string
	AccessGatewayURL            string
	HTTPAddr                    string
	CORSAllowedOrigins          []string
	DataDir                     string
	JWTSecret                   string
	AdminUser                   string
	AdminPassword               string
	AdminTenants                []string
	PostgresDSN                 string
	RedisAddr                   string
	RedisPassword               string
	ClickHouseURL               string
	RawHighFrequencyIntervalSec int64
	// MQTTDeviceTokenTTL is the lifetime of standard MQTT/HTTP device tokens
	// when EMQX revocation (ban and kick) is configured; without it tokens
	// stay at five minutes because expiry is the only revocation.
	MQTTDeviceTokenTTL time.Duration
	// ProtocolListenerMaxSessions bounds concurrent peers per TCP/UDP listener.
	ProtocolListenerMaxSessions int64
	// PostgresMaxConns sizes this process's PostgreSQL pool unless the DSN sets
	// pool_max_conns. The sum over all processes must stay below the server's
	// max_connections.
	PostgresMaxConns int64
	// KafkaConsumerConcurrency is the parallel lanes per Kafka subscription;
	// messages of one device keep their order within a lane.
	KafkaConsumerConcurrency int64
	// AIAnalysisConcurrency bounds automatic alarm analyses running at once.
	AIAnalysisConcurrency int64
	MinIOEndpoint         string
	MinIOAccessKey        string
	MinIOSecretKey        string
	MinIOUseTLS           bool
	KafkaBrokers          []string
	EMQXAPIURL            string
	EMQXAPIKey            string
	EMQXAPISecret         string
	MQTTBroker            string
	MQTTUsername          string
	MQTTPassword          string
	MQTTWebSocketURL      string
	MQTTPublicURL         string
	DeviceHTTPPublicURL   string
	OllamaURL             string
	OllamaModel           string
	AIProvider            string
	AIBaseURL             string
	AIModel               string
	AIAPIKey              string
	AIHarnessURL          string
	AIHarnessToken        string
	AIHarnessMCPURL       string
	AIHarnessModel        string
	AIHarnessTimeout      time.Duration
	AITestOllamaURL       string
	WeaviateURL           string
	BackupURL             string
	BackupToken           string
	VideoSecrets          map[string]string
	VideoPlatformTenants  map[string]string
	VideoMediaHosts       []string
	OfflineScan           time.Duration
	ModbusAllowedCIDRs    []string
	DevMode               bool
	Ops                   OpsConfig
	loadErr               error
}

func Load() Config {
	devMode, devModeErr := strictBoolValue("IOT_DEV_MODE", true)
	aiProvider := strings.ToLower(strings.TrimSpace(os.Getenv("IOT_AI_PROVIDER")))
	deepSeekAPIKey := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
	if aiProvider == "" {
		aiProvider = "deepseek"
	}
	aiAPIKey := strings.TrimSpace(os.Getenv("IOT_AI_API_KEY"))
	if aiAPIKey == "" && aiProvider == "deepseek" {
		aiAPIKey = deepSeekAPIKey
	}
	return Config{
		AccessCoordination:          boolValue("IOT_ACCESS_COORDINATION", false),
		AccessNodeURL:               strings.TrimRight(os.Getenv("IOT_ACCESS_NODE_URL"), "/"),
		ProcessRole:                 strings.ToLower(get("IOT_PROCESS_ROLE", "combined")),
		AccessGatewayURL:            strings.TrimRight(os.Getenv("IOT_ACCESS_GATEWAY_URL"), "/"),
		HTTPAddr:                    get("IOT_HTTP_ADDR", ":8080"),
		CORSAllowedOrigins:          split(os.Getenv("IOT_CORS_ALLOWED_ORIGINS")),
		DataDir:                     get("IOT_DATA_DIR", "./data"),
		JWTSecret:                   get("IOT_JWT_SECRET", defaultJWTSecret),
		AdminUser:                   get("IOT_ADMIN_USER", "admin"),
		AdminPassword:               get("IOT_ADMIN_PASSWORD", defaultAdminPassword),
		AdminTenants:                split(get("IOT_ADMIN_TENANTS", "tenant_001")),
		PostgresDSN:                 os.Getenv("IOT_POSTGRES_DSN"),
		RedisAddr:                   os.Getenv("IOT_REDIS_ADDR"),
		RedisPassword:               os.Getenv("IOT_REDIS_PASSWORD"),
		ClickHouseURL:               os.Getenv("IOT_CLICKHOUSE_URL"),
		RawHighFrequencyIntervalSec: int64Value("IOT_RAW_HIGH_FREQUENCY_INTERVAL_SEC", 60),
		KafkaConsumerConcurrency:    int64Value("IOT_KAFKA_CONSUMER_CONCURRENCY", 64),
		PostgresMaxConns:            int64Value("IOT_POSTGRES_MAX_CONNS", 64),
		ProtocolListenerMaxSessions: int64Value("IOT_PROTOCOL_LISTENER_MAX_SESSIONS", 1024),
		MQTTDeviceTokenTTL:          duration("IOT_MQTT_DEVICE_TOKEN_TTL", 24*time.Hour),
		AIAnalysisConcurrency:       int64Value("IOT_AI_ANALYSIS_CONCURRENCY", 1),
		MinIOEndpoint:               os.Getenv("IOT_MINIO_ENDPOINT"),
		MinIOAccessKey:              os.Getenv("IOT_MINIO_ACCESS_KEY"),
		MinIOSecretKey:              os.Getenv("IOT_MINIO_SECRET_KEY"),
		MinIOUseTLS:                 boolValue("IOT_MINIO_USE_TLS", false),
		KafkaBrokers:                split(os.Getenv("IOT_KAFKA_BROKERS")),
		EMQXAPIURL:                  os.Getenv("IOT_EMQX_API_URL"),
		EMQXAPIKey:                  os.Getenv("IOT_EMQX_API_KEY"),
		EMQXAPISecret:               os.Getenv("IOT_EMQX_API_SECRET"),
		MQTTBroker:                  os.Getenv("IOT_MQTT_BROKER"),
		MQTTUsername:                os.Getenv("IOT_MQTT_USERNAME"),
		MQTTPassword:                os.Getenv("IOT_MQTT_PASSWORD"),
		MQTTWebSocketURL:            os.Getenv("IOT_MQTT_WEBSOCKET_PUBLIC_URL"),
		MQTTPublicURL:               os.Getenv("IOT_DEVICE_MQTT_PUBLIC_URL"),
		DeviceHTTPPublicURL:         os.Getenv("IOT_DEVICE_HTTP_PUBLIC_URL"),
		OllamaURL:                   get("IOT_OLLAMA_URL", "http://localhost:11434"),
		OllamaModel:                 strings.TrimSpace(os.Getenv("IOT_OLLAMA_MODEL")),
		AIProvider:                  aiProvider,
		AIBaseURL:                   strings.TrimRight(os.Getenv("IOT_AI_BASE_URL"), "/"),
		AIModel:                     strings.TrimSpace(os.Getenv("IOT_AI_MODEL")),
		AIAPIKey:                    aiAPIKey,
		AIHarnessURL:                strings.TrimRight(strings.TrimSpace(os.Getenv("IOT_AI_HARNESS_URL")), "/"),
		AIHarnessToken:              strings.TrimSpace(os.Getenv("IOT_AI_HARNESS_TOKEN")),
		AIHarnessMCPURL:             strings.TrimSpace(os.Getenv("IOT_AI_HARNESS_MCP_URL")),
		AIHarnessModel:              strings.TrimSpace(os.Getenv("IOT_AI_HARNESS_MODEL")),
		AIHarnessTimeout:            duration("IOT_AI_HARNESS_TIMEOUT", 90*time.Second),
		AITestOllamaURL:             get("IOT_AI_OLLAMA_URL", get("IOT_OLLAMA_URL", "http://localhost:11434")),
		WeaviateURL:                 os.Getenv("IOT_WEAVIATE_URL"),
		BackupURL:                   strings.TrimRight(strings.TrimSpace(os.Getenv("IOT_BACKUP_URL")), "/"),
		BackupToken:                 strings.TrimSpace(os.Getenv("IOT_BACKUP_ADMIN_TOKEN")),
		VideoSecrets:                parsePairs(os.Getenv("IOT_VIDEO_PLATFORM_SECRETS")),
		VideoPlatformTenants:        parsePairs(os.Getenv("IOT_VIDEO_PLATFORM_TENANTS")),
		VideoMediaHosts:             split(os.Getenv("IOT_VIDEO_MEDIA_ALLOWED_HOSTS")),
		OfflineScan:                 duration("IOT_OFFLINE_SCAN_INTERVAL", 30*time.Second),
		ModbusAllowedCIDRs:          split(get("IOT_MODBUS_ALLOWED_CIDRS", "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,127.0.0.0/8,fc00::/7,::1/128")),
		DevMode:                     devMode,
		Ops:                         loadOps(),
		loadErr:                     devModeErr,
	}
}

func (c Config) Validate() error {
	if c.loadErr != nil {
		return c.loadErr
	}
	if err := c.Ops.validate(); err != nil {
		return err
	}
	if c.AccessCoordination && (c.PostgresDSN == "" || c.AccessNodeURL == "") {
		return fmt.Errorf("access coordination requires PostgreSQL and IOT_ACCESS_NODE_URL")
	}
	switch c.ProcessRole {
	case "", "combined":
	case "api", "gateway":
		if c.PostgresDSN == "" || len(c.KafkaBrokers) == 0 {
			return fmt.Errorf("split process roles require shared IOT_POSTGRES_DSN and IOT_KAFKA_BROKERS")
		}
		if c.ProcessRole == "api" && c.AccessGatewayURL == "" {
			return fmt.Errorf("api role requires IOT_ACCESS_GATEWAY_URL")
		}
	default:
		return fmt.Errorf("IOT_PROCESS_ROLE must be combined, api or gateway")
	}
	if c.KafkaConsumerConcurrency < 0 || c.KafkaConsumerConcurrency > 64 {
		return fmt.Errorf("IOT_KAFKA_CONSUMER_CONCURRENCY must be between 1 and 64 (0 uses the default 64)")
	}
	if c.PostgresMaxConns < 0 || c.PostgresMaxConns > 1000 {
		return fmt.Errorf("IOT_POSTGRES_MAX_CONNS must be between 1 and 1000 (0 uses the default 64)")
	}
	if c.MQTTDeviceTokenTTL != 0 && (c.MQTTDeviceTokenTTL < time.Minute || c.MQTTDeviceTokenTTL > 30*24*time.Hour) {
		return fmt.Errorf("IOT_MQTT_DEVICE_TOKEN_TTL must be between 1m and 720h")
	}
	if c.ProtocolListenerMaxSessions < 0 || c.ProtocolListenerMaxSessions > 100000 {
		return fmt.Errorf("IOT_PROTOCOL_LISTENER_MAX_SESSIONS must be between 1 and 100000 (0 uses the default 1024)")
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
	for _, origin := range []string{c.AccessGatewayURL, c.AccessNodeURL} {
		if origin == "" {
			continue
		}
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("gateway/node URL must be an HTTP(S) origin without credentials")
		}
	}
	if c.DevMode {
		return nil
	}
	var invalid []string
	if c.JWTSecret == defaultJWTSecret || len(c.JWTSecret) < 32 || insecurePlaceholder(c.JWTSecret) {
		invalid = append(invalid, "IOT_JWT_SECRET must be explicitly set to at least 32 characters and must not be a placeholder")
	}
	if c.AdminPassword == "" {
		invalid = append(invalid, "IOT_ADMIN_PASSWORD must not be empty")
	}
	if len(invalid) > 0 {
		return fmt.Errorf("invalid production security configuration: %s", strings.Join(invalid, "; "))
	}
	return nil
}

func strictBoolValue(name string, fallback bool) (bool, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("invalid production security configuration: IOT_DEV_MODE must be true or false")
	}
	return parsed, nil
}

func insecurePlaceholder(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	for _, marker := range []string{"change-me", "change-this", "replace_me", "replace-me", "local-iot-", "public-change-me"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func get(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
func split(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	values := strings.Split(v, ",")
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}
func boolValue(name string, fallback bool) bool {
	v, err := strconv.ParseBool(os.Getenv(name))
	if err != nil {
		return fallback
	}
	return v
}
func int64Value(name string, fallback int64) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(name)), 10, 64)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}
func duration(name string, fallback time.Duration) time.Duration {
	v, err := time.ParseDuration(os.Getenv(name))
	if err != nil {
		return fallback
	}
	return v
}
func parsePairs(v string) map[string]string {
	out := map[string]string{}
	for _, pair := range strings.Split(v, ",") {
		p := strings.SplitN(pair, ":", 2)
		if len(p) == 2 {
			out[strings.TrimSpace(p[0])] = strings.TrimSpace(p[1])
		}
	}
	return out
}
