package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"iot-platform/internal/ports"
)

const (
	defaultJWTSecret     = "change-me-in-production"
	defaultAdminPassword = "admin123"
)

type Config struct {
	// InstanceID names this process in metrics, leases and durable paths.
	InstanceID string
	// InstanceIDExplicit is true when IOT_INSTANCE_ID was set; only then do
	// durable per-instance paths (MQTT inbox) include the ID.
	InstanceIDExplicit bool
	// ClusterInstances is the replica count used to split budgets when the
	// shared rate-limit store is unavailable (1 = single process).
	ClusterInstances int64
	// APIEmbeddedWorkers keeps parser/processor/jobs inside the api role.
	APIEmbeddedWorkers bool
	// PublishExternalTopics keeps publishing parsed messages to the external
	// property/event/parsed topics in addition to the internal business stream.
	PublishExternalTopics bool
	AccessCoordination    bool
	AccessNodeURL         string
	// NodeURL is this API instance's own reachable origin; setting it on
	// several API instances elects one to run live video control.
	NodeURL            string
	ProcessRole        string
	AccessGatewayURL   string
	HTTPAddr           string
	CORSAllowedOrigins []string
	DataDir            string
	JWTSecret          string
	AdminUser          string
	AdminPassword      string
	AdminTenants       []string
	PostgresDSN        string
	RedisAddr          string
	RedisPassword      string
	// RedisMasterName and RedisSentinels select Sentinel failover instead of
	// the single RedisAddr.
	RedisMasterName string
	RedisSentinels  []string
	ClickHouseURL   string
	// ClickHouseCluster enables replicated/distributed tables on that
	// cluster; ClickHouseInsertQuorum sets the replica acknowledgement.
	ClickHouseCluster      string
	ClickHouseInsertQuorum string
	// Postgres pool and replica tuning (see postgres.PoolOptions).
	PostgresMaxConnLifetime   time.Duration
	PostgresHealthCheckPeriod time.Duration
	PostgresConnectTimeout    time.Duration
	PostgresReadDSN           string
	PostgresMaxReplicaLag     time.Duration
	// KafkaAutoCreateTopics lets publishing create missing topics (local).
	KafkaAutoCreateTopics       bool
	RawHighFrequencyIntervalSec int64
	// MQTTDeviceTokenTTL is the lifetime of standard MQTT/HTTP device tokens
	// when EMQX revocation (ban and kick) is configured; without it tokens
	// stay at five minutes because expiry is the only revocation.
	MQTTDeviceTokenTTL time.Duration
	// IngestMaxBacklog pauses new raw ingest while the parser plus storage
	// backlog exceeds it; ingest resumes below 80%. Zero disables the pause.
	IngestMaxBacklog int64
	// ProtocolListenerMaxSessions bounds concurrent peers per TCP/UDP listener.
	ProtocolListenerMaxSessions int64
	// PostgresMaxConns sizes this process's PostgreSQL pool unless the DSN sets
	// pool_max_conns. The sum over all processes must stay below the server's
	// max_connections.
	PostgresMaxConns int64
	// KafkaConsumerConcurrency is the parallel lanes per Kafka subscription;
	// messages of one device keep their order within a lane.
	KafkaConsumerConcurrency int64
	// ConsumerMaxBlock bounds how long a dependency outage may hold a
	// message before it is moved to the dead-letter topic.
	ConsumerMaxBlock time.Duration
	// ProtocolRunnerSocket is the protocol runner's Unix socket; empty runs
	// uploaded protocol code in-process (DevMode or IOT_PROTOCOL_SANDBOX=none).
	ProtocolRunnerSocket string
	// ProtocolSandbox is "runner" (the runner is required), "none" (in-process
	// on purpose) or empty (use the runner when configured, otherwise warn).
	ProtocolSandbox      string
	MinIOEndpoint        string
	MinIOAccessKey       string
	MinIOSecretKey       string
	MinIOUseTLS          bool
	KafkaBrokers         []string
	KafkaPublicBrokers   []string
	KafkaSASLUsername    string
	KafkaSASLPassword    string
	KafkaSASLMechanism   string
	KafkaTLS             bool
	KafkaTLSCAFile       string
	KafkaAdminURL        string
	KafkaAdminUsername   string
	KafkaAdminPassword   string
	EMQXAPIURL           string
	EMQXAPIKey           string
	EMQXAPISecret        string
	MQTTBroker           string
	MQTTUsername         string
	MQTTPassword         string
	MQTTToolUsername     string
	MQTTWebSocketURL     string
	MQTTPublicURL        string
	DeviceHTTPPublicURL  string
	AIProvider           string
	AIBaseURL            string
	AIModel              string
	AIAPIKey             string
	AIHarnessURL         string
	AIHarnessToken       string
	AIHarnessMCPURL      string
	AIHarnessModel       string
	AIHarnessTimeout     time.Duration
	AIBusinessTimeout    time.Duration
	EmbeddingDimensions  int
	EmbeddingBatchSize   int
	EmbeddingURL         string
	EmbeddingModel       string
	EmbeddingAPIKey      string
	EmbeddingQueryPrompt string
	EmbeddingTimeout     time.Duration
	LocalAIHosts         string
	RerankURL            string
	RerankTimeout        time.Duration
	BackupURL            string
	BackupToken          string
	OfflineScan          time.Duration
	// DeviceSignalInterval is how often the jobs role recomputes device health
	// signals over DeviceSignalWindow; DeviceSignalAlarm turns strong signals
	// into DEVICE_HEALTH alarms.
	DeviceSignalInterval time.Duration
	DeviceSignalWindow   time.Duration
	DeviceSignalAlarm    bool
	ModbusAllowedCIDRs   []string
	DevMode              bool
	Ops                  OpsConfig
	Video                VideoConfig
	Retention            RetentionConfig
	Notify               NotifyConfig
	loadErr              error
}

func Load() Config {
	devMode, devModeErr := strictBoolValue("IOT_DEV_MODE", true)
	kafkaTLS, kafkaTLSErr := strictBoolValue("IOT_KAFKA_TLS", false)
	aiProvider := strings.ToLower(strings.TrimSpace(os.Getenv("IOT_AI_PROVIDER")))
	deepSeekAPIKey := strings.TrimSpace(os.Getenv("DEEPSEEK_API_KEY"))
	if aiProvider == "" {
		aiProvider = "deepseek"
	}
	aiAPIKey := strings.TrimSpace(os.Getenv("IOT_AI_API_KEY"))
	if aiAPIKey == "" && aiProvider == "deepseek" {
		aiAPIKey = deepSeekAPIKey
	}
	instance, explicitInstance := instanceID()
	return Config{
		InstanceID:                  instance,
		InstanceIDExplicit:          explicitInstance,
		ClusterInstances:            int64Value("IOT_CLUSTER_INSTANCES", 1),
		APIEmbeddedWorkers:          boolValue("IOT_API_EMBEDDED_WORKERS", true),
		PublishExternalTopics:       boolValue("IOT_PUBLISH_EXTERNAL_TOPICS", true),
		AccessCoordination:          boolValue("IOT_ACCESS_COORDINATION", false),
		AccessNodeURL:               strings.TrimRight(os.Getenv("IOT_ACCESS_NODE_URL"), "/"),
		NodeURL:                     strings.TrimRight(os.Getenv("IOT_NODE_URL"), "/"),
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
		RedisMasterName:             strings.TrimSpace(os.Getenv("IOT_REDIS_MASTER_NAME")),
		RedisSentinels:              split(os.Getenv("IOT_REDIS_SENTINELS")),
		ClickHouseURL:               os.Getenv("IOT_CLICKHOUSE_URL"),
		ClickHouseCluster:           strings.TrimSpace(os.Getenv("IOT_CLICKHOUSE_CLUSTER")),
		ClickHouseInsertQuorum:      strings.TrimSpace(os.Getenv("IOT_CLICKHOUSE_INSERT_QUORUM")),
		PostgresMaxConnLifetime:     duration("IOT_POSTGRES_MAX_CONN_LIFETIME", 30*time.Minute),
		PostgresHealthCheckPeriod:   duration("IOT_POSTGRES_HEALTH_CHECK_PERIOD", 15*time.Second),
		PostgresConnectTimeout:      duration("IOT_POSTGRES_CONNECT_TIMEOUT", 5*time.Second),
		PostgresReadDSN:             os.Getenv("IOT_POSTGRES_READ_DSN"),
		PostgresMaxReplicaLag:       duration("IOT_POSTGRES_MAX_REPLICA_LAG", 5*time.Second),
		KafkaAutoCreateTopics:       boolValue("IOT_KAFKA_AUTO_CREATE_TOPICS", true),
		RawHighFrequencyIntervalSec: int64Value("IOT_RAW_HIGH_FREQUENCY_INTERVAL_SEC", 60),
		KafkaConsumerConcurrency:    int64Value("IOT_KAFKA_CONSUMER_CONCURRENCY", 64),
		ConsumerMaxBlock:            duration("IOT_CONSUMER_MAX_BLOCK", 30*time.Minute),
		ProtocolRunnerSocket:        strings.TrimSpace(get("IOT_PROTOCOL_RUNNER_SOCKET", "")),
		ProtocolSandbox:             strings.ToLower(strings.TrimSpace(get("IOT_PROTOCOL_SANDBOX", ""))),
		PostgresMaxConns:            int64Value("IOT_POSTGRES_MAX_CONNS", 64),
		ProtocolListenerMaxSessions: int64Value("IOT_PROTOCOL_LISTENER_MAX_SESSIONS", 20000),
		MQTTDeviceTokenTTL:          duration("IOT_MQTT_DEVICE_TOKEN_TTL", 24*time.Hour),
		IngestMaxBacklog:            int64Value("IOT_INGEST_MAX_BACKLOG", 50000),
		MinIOEndpoint:               os.Getenv("IOT_MINIO_ENDPOINT"),
		MinIOAccessKey:              os.Getenv("IOT_MINIO_ACCESS_KEY"),
		MinIOSecretKey:              os.Getenv("IOT_MINIO_SECRET_KEY"),
		MinIOUseTLS:                 boolValue("IOT_MINIO_USE_TLS", false),
		KafkaBrokers:                split(os.Getenv("IOT_KAFKA_BROKERS")),
		KafkaPublicBrokers:          split(os.Getenv("IOT_KAFKA_PUBLIC_BROKERS")),
		KafkaSASLUsername:           strings.TrimSpace(os.Getenv("IOT_KAFKA_SASL_USERNAME")),
		KafkaSASLPassword:           os.Getenv("IOT_KAFKA_SASL_PASSWORD"),
		KafkaSASLMechanism:          strings.ToUpper(strings.TrimSpace(get("IOT_KAFKA_SASL_MECHANISM", "SCRAM-SHA-256"))),
		KafkaTLS:                    kafkaTLS,
		KafkaTLSCAFile:              strings.TrimSpace(os.Getenv("IOT_KAFKA_TLS_CA_FILE")),
		KafkaAdminURL:               strings.TrimRight(strings.TrimSpace(os.Getenv("IOT_KAFKA_ADMIN_URL")), "/"),
		KafkaAdminUsername:          strings.TrimSpace(os.Getenv("IOT_KAFKA_ADMIN_USERNAME")),
		KafkaAdminPassword:          os.Getenv("IOT_KAFKA_ADMIN_PASSWORD"),
		EMQXAPIURL:                  os.Getenv("IOT_EMQX_API_URL"),
		EMQXAPIKey:                  os.Getenv("IOT_EMQX_API_KEY"),
		EMQXAPISecret:               os.Getenv("IOT_EMQX_API_SECRET"),
		MQTTBroker:                  os.Getenv("IOT_MQTT_BROKER"),
		MQTTUsername:                os.Getenv("IOT_MQTT_USERNAME"),
		MQTTPassword:                os.Getenv("IOT_MQTT_PASSWORD"),
		MQTTToolUsername:            get("IOT_MQTT_TOOL_USERNAME", "admin"),
		MQTTWebSocketURL:            os.Getenv("IOT_MQTT_WEBSOCKET_PUBLIC_URL"),
		MQTTPublicURL:               os.Getenv("IOT_DEVICE_MQTT_PUBLIC_URL"),
		DeviceHTTPPublicURL:         os.Getenv("IOT_DEVICE_HTTP_PUBLIC_URL"),
		AIProvider:                  aiProvider,
		AIBaseURL:                   strings.TrimRight(os.Getenv("IOT_AI_BASE_URL"), "/"),
		AIModel:                     strings.TrimSpace(os.Getenv("IOT_AI_MODEL")),
		AIAPIKey:                    aiAPIKey,
		AIHarnessURL:                strings.TrimRight(strings.TrimSpace(os.Getenv("IOT_AI_HARNESS_URL")), "/"),
		AIHarnessToken:              strings.TrimSpace(os.Getenv("IOT_AI_HARNESS_TOKEN")),
		AIHarnessMCPURL:             strings.TrimSpace(os.Getenv("IOT_AI_HARNESS_MCP_URL")),
		AIHarnessModel:              strings.TrimSpace(os.Getenv("IOT_AI_HARNESS_MODEL")),
		AIHarnessTimeout:            duration("IOT_AI_HARNESS_TIMEOUT", 90*time.Second),
		AIBusinessTimeout:           duration("IOT_AI_HARNESS_BUSINESS_TIMEOUT", 4*time.Minute),
		EmbeddingDimensions:         int(int64Value("IOT_EMBEDDING_DIMENSIONS", 1024)),
		EmbeddingBatchSize:          int(int64Value("IOT_EMBEDDING_BATCH_SIZE", 10)),
		EmbeddingURL:                strings.TrimRight(strings.TrimSpace(get("IOT_EMBEDDING_URL", "http://embedding/v1")), "/"),
		EmbeddingModel:              get("IOT_EMBEDDING_MODEL", "bge-m3"),
		LocalAIHosts:                get("IOT_LOCAL_AI_HOSTS", ports.DefaultLocalAIHosts),
		RerankURL:                   strings.TrimRight(strings.TrimSpace(os.Getenv("IOT_RERANK_URL")), "/"),
		RerankTimeout:               duration("IOT_RERANK_TIMEOUT", 8*time.Second),
		EmbeddingAPIKey:             strings.TrimSpace(os.Getenv("IOT_EMBEDDING_API_KEY")),
		EmbeddingQueryPrompt:        embeddingQueryPrompt(),
		EmbeddingTimeout:            duration("IOT_EMBEDDING_TIMEOUT", time.Minute),
		BackupURL:                   strings.TrimRight(strings.TrimSpace(os.Getenv("IOT_BACKUP_URL")), "/"),
		BackupToken:                 strings.TrimSpace(os.Getenv("IOT_BACKUP_ADMIN_TOKEN")),
		OfflineScan:                 duration("IOT_OFFLINE_SCAN_INTERVAL", 30*time.Second),
		DeviceSignalInterval:        duration("IOT_DEVICE_SIGNAL_INTERVAL", 10*time.Minute),
		DeviceSignalWindow:          duration("IOT_DEVICE_SIGNAL_WINDOW", 24*time.Hour),
		DeviceSignalAlarm:           boolValue("IOT_DEVICE_SIGNAL_ALARM", false),
		ModbusAllowedCIDRs:          split(get("IOT_MODBUS_ALLOWED_CIDRS", "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,127.0.0.0/8,fc00::/7,::1/128")),
		DevMode:                     devMode,
		Ops:                         loadOps(),
		Video:                       loadVideo(),
		Retention:                   loadRetention(),
		Notify:                      loadNotify(),
		loadErr:                     errors.Join(devModeErr, kafkaTLSErr),
	}
}

func (c Config) Validate() error {
	if c.loadErr != nil {
		return c.loadErr
	}
	if err := c.Ops.validate(); err != nil {
		return err
	}
	if c.Notify.loadErr != nil {
		return c.Notify.loadErr
	}
	if c.Retention.Enabled || c.Retention.retentionError != nil {
		if err := c.Retention.Validate(); err != nil {
			return err
		}
	}
	if err := c.validateKafkaSecurity(); err != nil {
		return err
	}
	if c.AccessCoordination && (c.PostgresDSN == "" || c.AccessNodeURL == "") {
		return fmt.Errorf("access coordination requires PostgreSQL and IOT_ACCESS_NODE_URL")
	}
	if err := c.validateRole(); err != nil {
		return err
	}
	if c.ConsumerMaxBlock < 0 || c.ConsumerMaxBlock > 24*time.Hour {
		return fmt.Errorf("IOT_CONSUMER_MAX_BLOCK must be between 0 and 24h (0 uses the default 30m)")
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
	if c.IngestMaxBacklog < 0 {
		return fmt.Errorf("IOT_INGEST_MAX_BACKLOG must not be negative (0 disables ingest backpressure)")
	}
	if c.ProtocolListenerMaxSessions < 0 || c.ProtocolListenerMaxSessions > 100000 {
		return fmt.Errorf("IOT_PROTOCOL_LISTENER_MAX_SESSIONS must be between 1 and 100000 (0 uses the default 20000)")
	}
	// Every business AI feature runs as a Harness workflow; roles that run no
	// AI feature (gateway, parser, processor, jobs) may run without it.
	if c.Runs(ComponentAIRuntime) {
		if c.AIHarnessURL == "" {
			return fmt.Errorf("IOT_AI_HARNESS_URL is required: the AI workflow Harness is a mandatory component")
		}
		// Several comma-separated instances are routed by conversation.
		for _, raw := range split(c.AIHarnessURL) {
			if u, err := url.Parse(raw); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
				return fmt.Errorf("IOT_AI_HARNESS_URL must be one or more comma-separated HTTP(S) URLs without credentials")
			}
		}
	}
	// The persistent knowledge index uses the bundled vector service or an
	// external HTTPS embedding API.
	if c.Runs(ComponentManagement) && !c.DevMode && c.PostgresDSN == "" {
		return fmt.Errorf("IOT_POSTGRES_DSN is required for the persistent PostgreSQL knowledge index")
	}
	if c.Runs(ComponentManagement) && (!c.DevMode || c.PostgresDSN != "" || c.EmbeddingURL != "") {
		if c.EmbeddingURL == "" {
			return fmt.Errorf("IOT_EMBEDDING_URL is required for the PostgreSQL knowledge index")
		}
		if u, err := url.Parse(c.EmbeddingURL); err != nil || (u.Scheme != "https" && !ports.LocalAIEndpoint(c.EmbeddingURL, c.LocalAIHosts)) || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("IOT_EMBEDDING_URL must be the bundled vector service (IOT_LOCAL_AI_HOSTS) or an HTTPS external API URL without credentials or query")
		}
	}
	if c.RerankURL != "" && !ports.LocalAIEndpoint(c.RerankURL, c.LocalAIHosts) {
		if u, err := url.Parse(c.RerankURL); err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return fmt.Errorf("IOT_RERANK_URL must be the bundled rerank service (IOT_LOCAL_AI_HOSTS) or an HTTPS URL")
		}
	}
	if (c.EmbeddingDimensions != 0 && (c.EmbeddingDimensions < 1 || c.EmbeddingDimensions > 2000)) || (c.EmbeddingBatchSize != 0 && (c.EmbeddingBatchSize < 1 || c.EmbeddingBatchSize > 100)) {
		return fmt.Errorf("embedding dimensions must be 1..2000 and batch size 1..100")
	}
	if c.NodeURL != "" && c.PostgresDSN == "" {
		return fmt.Errorf("IOT_NODE_URL (live video control election) requires shared PostgreSQL")
	}
	for _, origin := range []string{c.AccessGatewayURL, c.AccessNodeURL, c.NodeURL} {
		if origin == "" {
			continue
		}
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return fmt.Errorf("gateway/node URL must be an HTTP(S) origin without credentials")
		}
	}
	if c.ProtocolSandbox != "" && c.ProtocolSandbox != "runner" && c.ProtocolSandbox != "none" {
		return fmt.Errorf("IOT_PROTOCOL_SANDBOX must be runner or none")
	}
	if c.ProtocolSandbox == "runner" && c.ProtocolRunnerSocket == "" {
		return fmt.Errorf("IOT_PROTOCOL_SANDBOX=runner requires IOT_PROTOCOL_RUNNER_SOCKET")
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

// embeddingQueryPrompt reads IOT_EMBEDDING_QUERY_INSTRUCTION. Empty keeps the
// empty instruction, "none" explicitly disables the prefix, and a
// literal \n is accepted so the value fits on one env-file line.
func embeddingQueryPrompt() string {
	value := os.Getenv("IOT_EMBEDDING_QUERY_INSTRUCTION")
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return ""
	case "none":
		return ""
	}
	return strings.ReplaceAll(value, `\n`, "\n")
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
