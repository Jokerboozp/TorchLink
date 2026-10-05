package clusterplan

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"iot-platform/deploy/toolaccounts"
	promrules "iot-platform/ops/prometheus"
)

// Secrets are read from a private YAML file and only written into the .env
// file of nodes that need them; compose.yaml and configs reference variables.
type Secrets struct {
	ServiceAdminUser            string `yaml:"serviceAdminUser,omitempty"`
	ServiceAdminPassword        string `yaml:"serviceAdminPassword,omitempty"`
	PostgresPassword            string `yaml:"postgresPassword"`
	PostgresSuperuserPassword   string `yaml:"postgresSuperuserPassword"`
	PostgresReplicationPassword string `yaml:"postgresReplicationPassword"`
	RedisPassword               string `yaml:"redisPassword"`
	ClickHousePassword          string `yaml:"clickhousePassword"`
	MinIORootUser               string `yaml:"minioRootUser"`
	MinIORootPassword           string `yaml:"minioRootPassword"`
	JWTSecret                   string `yaml:"jwtSecret"`
	AdminPassword               string `yaml:"adminPassword"`
	HarnessToken                string `yaml:"harnessToken"`
	EMQXAPIKey                  string `yaml:"emqxApiKey"`
	EMQXAPISecret               string `yaml:"emqxApiSecret"`
	EMQXCookie                  string `yaml:"emqxCookie"`
	EMQXDashboardPassword       string `yaml:"emqxDashboardPassword"`
	MQTTToolUsername            string `yaml:"mqttToolUsername,omitempty"`
	MQTTToolPassword            string `yaml:"mqttToolPassword,omitempty"`
	// Kafka client/admin settings also configure the managed broker bootstrap identity.
	KafkaSASLUsername  string `yaml:"kafkaSaslUsername,omitempty"`
	KafkaSASLPassword  string `yaml:"kafkaSaslPassword,omitempty"`
	KafkaSASLMechanism string `yaml:"kafkaSaslMechanism,omitempty"`
	KafkaTLS           string `yaml:"kafkaTls,omitempty"`
	KafkaTLSCAFile     string `yaml:"kafkaTlsCaFile,omitempty"`
	KafkaAdminURL      string `yaml:"kafkaAdminUrl,omitempty"`
	KafkaAdminUsername string `yaml:"kafkaAdminUsername,omitempty"`
	KafkaAdminPassword string `yaml:"kafkaAdminPassword,omitempty"`
	KafkaPublicBrokers string `yaml:"kafkaPublicBrokers,omitempty"`
	MQTTPublicURL      string `yaml:"mqttPublicUrl,omitempty"`
	BackupToken        string `yaml:"backupToken"`
	// CapacityToken is shared by the platform and the capacity module.
	CapacityToken string `yaml:"capacityToken,omitempty"`
	// BackupRestoreTargetDSN is an optional separate database for restore checks.
	BackupRestoreTargetDSN      string `yaml:"backupRestoreTargetDSN"`
	BackupRestoreMinIOEndpoint  string `yaml:"backupRestoreMinioEndpoint,omitempty"`
	BackupRestoreMinIOAccessKey string `yaml:"backupRestoreMinioAccessKey,omitempty"`
	BackupRestoreMinIOSecretKey string `yaml:"backupRestoreMinioSecretKey,omitempty"`
	DeepSeekAPIKey              string `yaml:"deepseekApiKey"`
	// EmbeddingAPIKey is the external Embedding API key; never auto-generated.
	EmbeddingAPIKey string `yaml:"embeddingApiKey,omitempty"`
	// TLSCertFile and TLSKeyFile (PEM, relative to the secrets file) enable
	// HTTPS (8443) on web nodes and MQTTS (8883) / WSS (8084) on EMQX nodes.
	TLSCertFile string `yaml:"tlsCertFile,omitempty"`
	TLSKeyFile  string `yaml:"tlsKeyFile,omitempty"`
	// AlertWebhookURL receives Alertmanager notifications of the platform
	// alert rules; empty keeps alerts in Alertmanager and the operations center.
	AlertWebhookURL    string `yaml:"alertWebhookUrl,omitempty"`
	VideoMediaSecret   string `yaml:"videoMediaSecret"`
	VideoHookSecret    string `yaml:"videoHookSecret"`
	VideoCredentialKey string `yaml:"videoCredentialKey"`
	baseDir            string `yaml:"-"`
}

// CheckSecretsMode rejects secrets files readable by group or others. The
// Windows deployment script turns it off for Docker bind mounts, which show
// every file as 0777, and restricts the file with an ACL instead.
var CheckSecretsMode = true

func LoadSecrets(path string) (Secrets, error) {
	var s Secrets
	info, err := os.Stat(path)
	if err != nil {
		return s, err
	}
	if CheckSecretsMode && runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return s, errors.New("secrets file must not be readable by group or others (chmod 600)")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err = dec.Decode(&s); err != nil {
		return s, fmt.Errorf("secrets: %w", err)
	}
	s.baseDir, err = filepath.Abs(filepath.Dir(path))
	if err != nil {
		return s, errors.New("could not resolve secrets directory")
	}
	return s, nil
}

const kafkaCAContainerPath = "/app/kafka/ca.pem"

func (s Secrets) kafkaEnv() map[string]string {
	secure := strings.ToLower(strings.TrimSpace(s.KafkaTLS))
	if secure == "" {
		secure = "false"
	}
	mechanism := strings.ToUpper(strings.TrimSpace(s.KafkaSASLMechanism))
	if mechanism == "" {
		mechanism = "SCRAM-SHA-256"
	}
	caFile := ""
	if s.KafkaTLSCAFile != "" {
		caFile = kafkaCAContainerPath
	}
	return map[string]string{
		"IOT_KAFKA_SASL_USERNAME": s.KafkaSASLUsername, "IOT_KAFKA_SASL_PASSWORD": s.KafkaSASLPassword,
		"IOT_KAFKA_SASL_MECHANISM": mechanism, "IOT_KAFKA_TLS": secure, "IOT_KAFKA_TLS_CA_FILE": caFile,
		"IOT_KAFKA_ADMIN_URL": s.KafkaAdminURL, "IOT_KAFKA_ADMIN_USERNAME": s.KafkaAdminUsername,
		"IOT_KAFKA_ADMIN_PASSWORD": s.KafkaAdminPassword, "IOT_KAFKA_PUBLIC_BROKERS": s.KafkaPublicBrokers,
	}
}

// Fill deployment defaults after inventory validation, so existing secret
// files gain tool identities without replacing an explicitly configured pair.
func (s Secrets) brokerDefaults(inv *Inventory) Secrets {
	user, password := s.ServiceAdminUser, s.ServiceAdminPassword
	if user == "" {
		user = "admin"
	}
	if password == "" {
		password = "admin123"
	}
	if s.MQTTToolUsername == "" && s.MQTTToolPassword == "" {
		s.MQTTToolUsername, s.MQTTToolPassword = user, password
	}
	if s.KafkaSASLUsername == "" && s.KafkaSASLPassword == "" {
		s.KafkaSASLUsername, s.KafkaSASLPassword = user, password
	}
	if s.KafkaAdminUsername == "" && s.KafkaAdminPassword == "" && s.KafkaAdminURL == "" {
		s.KafkaAdminUsername, s.KafkaAdminPassword = user, password
	}
	r := renderer{inv: inv, s: s}
	if s.KafkaAdminURL == "" && s.KafkaAdminUsername != "" && s.KafkaAdminPassword != "" && len(inv.Redpanda.Nodes) > 0 {
		s.KafkaAdminURL = "http://" + net.JoinHostPort(r.ip(inv.Redpanda.Nodes[0]), "9644")
	}
	if s.KafkaPublicBrokers == "" {
		s.KafkaPublicBrokers = r.hostList(inv.Redpanda.Nodes, 9092, ",")
		if v := inv.Env["IOT_KAFKA_BROKERS"]; v != "" {
			s.KafkaPublicBrokers = v
		}
	}
	if s.MQTTPublicURL == "" && len(inv.EMQX.Nodes) > 0 {
		s.MQTTPublicURL = "tcp://" + net.JoinHostPort(r.ip(inv.EMQX.Nodes[0]), "1883")
		if v := inv.Env["IOT_DEVICE_MQTT_PUBLIC_URL"]; v != "" {
			s.MQTTPublicURL = v
		}
	}
	return s
}

// tlsPair reads the entry-point certificate and key, which must match.
func (s Secrets) tlsPair() (*tlsFiles, error) {
	if s.TLSCertFile == "" && s.TLSKeyFile == "" {
		return nil, nil
	}
	if s.TLSCertFile == "" || s.TLSKeyFile == "" {
		return nil, errors.New("tlsCertFile and tlsKeyFile must be set together")
	}
	read := func(path string) ([]byte, error) {
		if !filepath.IsAbs(path) {
			path = filepath.Join(s.baseDir, path)
		}
		b, err := os.ReadFile(path)
		if err == nil && len(b) > 1024*1024 {
			err = errors.New("larger than 1 MiB")
		}
		return b, err
	}
	cert, err := read(s.TLSCertFile)
	if err != nil {
		return nil, fmt.Errorf("could not read tlsCertFile: %w", err)
	}
	key, err := read(s.TLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("could not read tlsKeyFile: %w", err)
	}
	if _, err = tls.X509KeyPair(cert, key); err != nil {
		return nil, fmt.Errorf("tlsCertFile and tlsKeyFile must be a matching PEM certificate and key: %w", err)
	}
	return &tlsFiles{cert: cert, key: key}, nil
}

// kafkaCA copies public trust certificates, never a client private key.
func (s Secrets) kafkaCA() ([]byte, error) {
	if s.KafkaTLSCAFile == "" {
		return nil, nil
	}
	path := s.KafkaTLSCAFile
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.baseDir, path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("could not read kafkaTlsCaFile")
	}
	if len(b) > 1024*1024 || bytes.Contains(b, []byte("PRIVATE KEY")) || !x509.NewCertPool().AppendCertsFromPEM(b) {
		return nil, errors.New("kafkaTlsCaFile must contain PEM certificates only (maximum 1 MiB)")
	}
	return b, nil
}

func (s Secrets) validate(inv *Inventory) error {
	s = s.brokerDefaults(inv)
	if s.AlertWebhookURL != "" {
		u, err := url.Parse(s.AlertWebhookURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || strings.ContainsAny(s.AlertWebhookURL, "\n\r") {
			return errors.New("alertWebhookUrl must be an http or https URL")
		}
	}
	required := map[string]string{"postgresPassword": s.PostgresPassword, "postgresSuperuserPassword": s.PostgresSuperuserPassword, "postgresReplicationPassword": s.PostgresReplicationPassword, "redisPassword": s.RedisPassword, "clickhousePassword": s.ClickHousePassword, "minioRootUser": s.MinIORootUser, "minioRootPassword": s.MinIORootPassword, "jwtSecret": s.JWTSecret, "adminPassword": s.AdminPassword, "harnessToken": s.HarnessToken, "emqxApiKey": s.EMQXAPIKey, "emqxApiSecret": s.EMQXAPISecret, "emqxCookie": s.EMQXCookie, "emqxDashboardPassword": s.EMQXDashboardPassword, "backupToken": s.BackupToken}
	if len(inv.Video.Members()) > 0 {
		required["videoMediaSecret"], required["videoHookSecret"], required["videoCredentialKey"] = s.VideoMediaSecret, s.VideoHookSecret, s.VideoCredentialKey
	}
	if inv.Capacity.Node != "" {
		required["capacityToken"] = s.CapacityToken
	}
	var missing []string
	if (s.ServiceAdminUser == "") != (s.ServiceAdminPassword == "") {
		missing = append(missing, "serviceAdminUser and serviceAdminPassword must be configured together")
	}
	for k, v := range map[string]string{"serviceAdminUser": s.ServiceAdminUser, "serviceAdminPassword": s.ServiceAdminPassword, "mqttToolUsername": s.MQTTToolUsername, "mqttToolPassword": s.MQTTToolPassword} {
		if strings.ContainsAny(v, "\n\r\"'$`\\") || strings.TrimSpace(v) != v {
			missing = append(missing, k+" (invalid environment file value)")
		}
	}
	for k, v := range required {
		if strings.TrimSpace(v) == "" || strings.Contains(strings.ToLower(v), "change-me") {
			missing = append(missing, k)
		}
		if strings.ContainsAny(v, "\n\r\"'$`\\") {
			missing = append(missing, k+" (must not contain quotes, $, backslashes or newlines)")
		}
	}
	for k, v := range map[string]string{"deepseekApiKey": s.DeepSeekAPIKey, "embeddingApiKey": s.EmbeddingAPIKey, "backupRestoreTargetDSN": s.BackupRestoreTargetDSN, "backupRestoreMinioEndpoint": s.BackupRestoreMinIOEndpoint, "backupRestoreMinioAccessKey": s.BackupRestoreMinIOAccessKey, "backupRestoreMinioSecretKey": s.BackupRestoreMinIOSecretKey} {
		if strings.ContainsAny(v, "\n\r\"'$`\\") {
			missing = append(missing, k+" (must not contain quotes, $, backslashes or newlines)")
		}
	}
	for k, v := range s.kafkaEnv() {
		if strings.ContainsAny(v, "\n\r\"'$`\\") || strings.TrimSpace(v) != v || strings.Contains(v, " #") {
			missing = append(missing, k+" (invalid environment file value)")
		}
		if _, set := inv.Env[k]; set {
			missing = append(missing, k+" (configure Kafka settings in the secrets file, not inventory env)")
		}
	}
	if (s.KafkaSASLUsername == "") != (s.KafkaSASLPassword == "") {
		missing = append(missing, "kafkaSaslUsername and kafkaSaslPassword must be configured together")
	}
	if m := s.kafkaEnv()["IOT_KAFKA_SASL_MECHANISM"]; m != "SCRAM-SHA-256" && m != "SCRAM-SHA-512" {
		missing = append(missing, "kafkaSaslMechanism must be SCRAM-SHA-256 or SCRAM-SHA-512")
	}
	if secure := s.kafkaEnv()["IOT_KAFKA_TLS"]; secure != "true" && secure != "false" {
		missing = append(missing, "kafkaTls must be true or false")
	} else if s.KafkaTLSCAFile != "" && secure != "true" {
		missing = append(missing, "kafkaTlsCaFile requires kafkaTls: true")
	}
	if s.KafkaAdminURL != "" || s.KafkaAdminUsername != "" || s.KafkaAdminPassword != "" {
		u, err := url.Parse(s.KafkaAdminURL)
		if s.KafkaAdminUsername == "" || s.KafkaAdminPassword == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			missing = append(missing, "kafkaAdminUrl (HTTP(S) root URL), kafkaAdminUsername and kafkaAdminPassword must be configured together")
		}
	}
	if s.KafkaPublicBrokers != "" {
		for _, address := range strings.Split(s.KafkaPublicBrokers, ",") {
			host, port, err := net.SplitHostPort(strings.TrimSpace(address))
			n, portErr := strconv.Atoi(port)
			if err != nil || portErr != nil || host == "" || strings.ContainsAny(host, "/@ \t\r\n") || n < 1 || n > 65535 {
				missing = append(missing, "kafkaPublicBrokers must contain comma-separated host:port addresses")
				break
			}
		}
	}
	if s.MQTTPublicURL != "" {
		u, err := url.Parse(s.MQTTPublicURL)
		if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(s.MQTTPublicURL, "\n\r\"'$`\\ \t") || (u.Scheme != "tcp" && u.Scheme != "ssl" && u.Scheme != "tls" && u.Scheme != "mqtt" && u.Scheme != "mqtts" && u.Scheme != "ws" && u.Scheme != "wss") {
			missing = append(missing, "mqttPublicUrl must be an MQTT endpoint URL without credentials, query or fragment")
		}
	}
	// These are embedded in connection URLs assembled by Compose, which
	// cannot percent-encode them.
	for k, v := range map[string]string{"postgresPassword": s.PostgresPassword, "clickhousePassword": s.ClickHousePassword, "redisPassword": s.RedisPassword} {
		if v != "" && !urlSafe.MatchString(v) {
			missing = append(missing, k+" (use letters, digits and . _ ~ - only)")
		}
	}
	if len(s.JWTSecret) < 32 || len(s.HarnessToken) < 32 {
		missing = append(missing, "jwtSecret and harnessToken need at least 32 characters")
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return errors.New("secrets missing or invalid: " + strings.Join(missing, ", "))
	}
	return nil
}

var urlSafe = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)

// Stages is the start order; each stage waits for the previous one to be
// healthy, and cluster-init runs between "data" and "workers".
var Stages = []string{"coordination", "data", "support", "workers", "edge"}

var serviceStage = map[string]string{
	"lb": "coordination", "etcd": "coordination", "keeper": "coordination", "redis": "coordination", "sentinel": "coordination", "rustfs": "coordination", "node-exporter": "coordination", "prometheus": "coordination",
	"postgres": "data", "redpanda": "data", "clickhouse": "data", "emqx": "data",
	"harness": "support", "video": "support", "backup": "support",
	"parser": "workers", "processor": "workers", "jobs": "workers",
	"api": "edge", "gateway": "edge", "web": "edge", "capacity": "edge",
}

// Summary is cluster.json: placement, endpoints, budget and start order.
type Summary struct {
	Name      string                         `json:"name"`
	Nodes     []Node                         `json:"nodes"`
	Services  map[string]map[string][]string `json:"services"` // node -> stage -> compose services
	Endpoints map[string]string              `json:"endpoints"`
	Budget    any                            `json:"postgresConnectionBudget"`
}

type renderer struct {
	inv *Inventory
	s   Secrets
	// tls holds the web and MQTT certificate and key when configured.
	tls *tlsFiles
}

type tlsFiles struct{ cert, key []byte }

func (r renderer) ip(node string) string {
	n, _ := r.inv.node(node)
	return n.Address
}

func (r renderer) hostList(nodes []string, port int, sep string) string {
	parts := make([]string, len(nodes))
	for i, n := range nodes {
		parts[i] = r.ip(n) + ":" + strconv.Itoa(port)
	}
	return strings.Join(parts, sep)
}

// pick prefers a member on the same node (locality), else a stable member.
func (r renderer) pick(node string, members []string, salt int) string {
	for _, m := range members {
		if m == node {
			return m
		}
	}
	return members[salt%len(members)]
}

func (r renderer) chNodes() []string {
	var out []string
	for _, shard := range r.inv.ClickHouse.Shards {
		out = append(out, shard...)
	}
	return out
}

func (r renderer) postgresDSN(attrs string) string {
	hosts := r.hostList(r.inv.Postgres.Nodes, 5432, ",")
	return "postgres://iot:${POSTGRES_PASSWORD}@" + hosts + "/iot?sslmode=disable&target_session_attrs=" + attrs
}

func (r renderer) clickhouseURL(node string, salt int) string {
	return "http://iot:${CLICKHOUSE_PASSWORD}@" + r.ip(r.pick(node, r.chNodes(), salt)) + ":8123?database=iot"
}

var rolePort = map[string]int{"api": 8081, "gateway": 8082, "parser": 8101, "processor": 8102, "jobs": 8104}

func (r renderer) platformEnv(role, node string, salt int) map[string]string {
	inv := r.inv
	apiCount, gwCount := len(inv.Platform.Roles["api"].Nodes), len(inv.Platform.Roles["gateway"].Nodes)
	env := map[string]string{
		"IOT_PROCESS_ROLE":             role,
		"IOT_INSTANCE_ID":              role + "-" + node,
		"IOT_HTTP_ADDR":                ":" + strconv.Itoa(rolePort[role]),
		"IOT_DEV_MODE":                 "false",
		"IOT_DATA_DIR":                 "/app/data",
		"IOT_JWT_SECRET":               "${IOT_JWT_SECRET}",
		"IOT_ADMIN_PASSWORD":           "${IOT_ADMIN_PASSWORD}",
		"IOT_POSTGRES_DSN":             r.postgresDSN("read-write"),
		"IOT_POSTGRES_READ_DSN":        r.postgresDSN("prefer-standby"),
		"IOT_POSTGRES_MAX_CONNS":       strconv.Itoa(inv.Platform.Roles[role].PoolMax),
		"IOT_KAFKA_BROKERS":            r.hostList(inv.Redpanda.Nodes, 9092, ","),
		"IOT_KAFKA_AUTO_CREATE_TOPICS": "false",
		"IOT_REDIS_MASTER_NAME":        "iot-redis",
		"IOT_REDIS_SENTINELS":          r.hostList(inv.Redis.Sentinels, 26379, ","),
		"IOT_REDIS_PASSWORD":           "${REDIS_PASSWORD}",
		"IOT_CLICKHOUSE_URL":           r.clickhouseURL(node, salt),
		"IOT_CLICKHOUSE_CLUSTER":       inv.ClickHouse.Cluster,
		"IOT_CLICKHOUSE_INSERT_QUORUM": inv.ClickHouse.InsertQuorum,
		"IOT_MINIO_ENDPOINT":           r.objectStorageEndpoint(),
		"IOT_MINIO_ACCESS_KEY":         "${MINIO_ROOT_USER}",
		"IOT_MINIO_SECRET_KEY":         "${MINIO_ROOT_PASSWORD}",
		"IOT_MQTT_BROKER":              "tcp://" + r.ip(r.pick(node, inv.EMQX.Nodes, salt)) + ":1883",
		"IOT_EMQX_API_URL":             "http://" + r.ip(r.pick(node, inv.EMQX.Nodes, salt)) + ":18083",
		"IOT_EMQX_API_KEY":             "${IOT_EMQX_API_KEY}",
		"IOT_EMQX_API_SECRET":          "${IOT_EMQX_API_SECRET}",
		"IOT_MQTT_TOOL_USERNAME":       "${IOT_MQTT_TOOL_USERNAME}",
		"IOT_MQTT_TOOL_PASSWORD":       "${IOT_MQTT_TOOL_PASSWORD}",
		"IOT_AI_HARNESS_URL":           harnessURLs(r),
		"IOT_AI_HARNESS_TOKEN":         "${IOT_AI_HARNESS_TOKEN}",
		"IOT_AI_HARNESS_MCP_URL":       inv.APIURL() + "/mcp/harness",
		"DEEPSEEK_API_KEY":             "${DEEPSEEK_API_KEY}",
		"IOT_BACKUP_URL":               "http://" + r.ip(inv.Backup.Node) + ":8090",
		"IOT_BACKUP_ADMIN_TOKEN":       "${IOT_BACKUP_ADMIN_TOKEN}",
		"IOT_API_EMBEDDED_WORKERS":     "false",
		"IOT_CLUSTER_INSTANCES":        strconv.Itoa(max(apiCount, gwCount)),
	}
	if inv.Backup.Node == "" {
		delete(env, "IOT_BACKUP_URL")
	}
	if monitoring := inv.Monitoring.Members(); len(monitoring) > 0 {
		prometheus, alertmanager := "http://"+r.ip(monitoring[0])+":9090", "http://"+r.ip(monitoring[0])+":9093"
		if len(monitoring) > 1 {
			// Any healthy replica answers through the local load balancer.
			prometheus, alertmanager = fmt.Sprintf("http://127.0.0.1:%d", LBPrometheusPort), fmt.Sprintf("http://127.0.0.1:%d", LBAlertmanagerPort)
		}
		env["IOT_OPS_PROMETHEUS_URL"], env["IOT_OPS_ALERTMANAGER_URL"] = prometheus, alertmanager
	}
	// Each API node runs its own embedding and reranker on loopback ports.
	env["IOT_EMBEDDING_URL"] = fmt.Sprintf("http://127.0.0.1:%d/v1", LocalEmbeddingPort)
	env["IOT_RERANK_URL"] = fmt.Sprintf("http://127.0.0.1:%d", LocalRerankPort)
	env["IOT_LOCAL_AI_HOSTS"] = "127.0.0.1"
	env["IOT_EMBEDDING_MODEL"] = "bge-m3"
	env["IOT_EMBEDDING_DIMENSIONS"] = "1024"
	env["IOT_EMBEDDING_BATCH_SIZE"] = "10"
	env["IOT_EMBEDDING_API_KEY"] = "${IOT_EMBEDDING_API_KEY}"
	if media := inv.Video.Members(); len(media) > 0 {
		v := r.ip(media[0])
		env["IOT_VIDEO_MEDIA_API_URL"] = "http://" + v + ":80"
		env["IOT_VIDEO_MEDIA_SERVER_ID"] = mediaServerID(inv, 0)
		env["IOT_VIDEO_MEDIA_SECRET"] = "${IOT_VIDEO_MEDIA_SECRET}"
		env["IOT_VIDEO_HOOK_SECRET"] = "${IOT_VIDEO_HOOK_SECRET}"
		env["IOT_VIDEO_CREDENTIAL_KEY"] = "${IOT_VIDEO_CREDENTIAL_KEY}"
		env["IOT_GB28181_MEDIA_IP"] = v
		if len(media) > 1 {
			var urls, ids, ips []string
			for i, n := range media[1:] {
				urls, ids, ips = append(urls, "http://"+r.ip(n)+":80"), append(ids, mediaServerID(inv, i+1)), append(ips, r.ip(n))
			}
			env["IOT_VIDEO_MEDIA_STANDBY_URLS"], env["IOT_VIDEO_MEDIA_STANDBY_IDS"], env["IOT_GB28181_MEDIA_STANDBY_IPS"] = strings.Join(urls, ","), strings.Join(ids, ","), strings.Join(ips, ",")
		}
	}
	switch role {
	case "api":
		env["IOT_ACCESS_GATEWAY_URL"] = inv.GatewayURL()
		if inv.Capacity.Node != "" {
			env["IOT_OPS_CAPACITY_URL"] = "http://" + r.ip(inv.Capacity.Node) + ":7080"
			env["IOT_OPS_CAPACITY_TOKEN"] = "${IOT_OPS_CAPACITY_TOKEN}"
		}
		// Elects the single live video controller among API instances.
		env["IOT_NODE_URL"] = "http://" + r.ip(node) + ":8081"
		env["IOT_GB28181_SIP_HOST"] = r.ip(node)
		if inv.Platform.PublicURL != "" {
			env["IOT_CORS_ALLOWED_ORIGINS"] = strings.TrimRight(inv.Platform.PublicURL, "/")
		}
	case "gateway":
		env["IOT_ACCESS_COORDINATION"] = "true"
		env["IOT_ACCESS_NODE_URL"] = "http://" + r.ip(node) + ":8082"
	}
	for k, v := range inv.Env {
		env[k] = v
	}
	for k := range r.s.kafkaEnv() {
		env[k] = "${" + k + "}"
	}
	if r.s.MQTTPublicURL != "" {
		env["IOT_DEVICE_MQTT_PUBLIC_URL"] = "${IOT_DEVICE_MQTT_PUBLIC_URL}"
	}
	return env
}

func harnessURLs(r renderer) string {
	var out []string
	for _, n := range r.inv.Harness.Nodes {
		out = append(out, "http://"+r.ip(n)+":8091")
	}
	return strings.Join(out, ",")
}

func service(image string, extra map[string]any) map[string]any {
	s := map[string]any{"image": image, "network_mode": "host", "restart": "unless-stopped"}
	for k, v := range extra {
		s[k] = v
	}
	return s
}

// nodeCompose builds the Compose project of one node. Secrets are ${VAR}
// references resolved from the node's .env file at `docker compose up`.
func (r renderer) nodeCompose(node string, services []string, files map[string][]byte) (map[string]any, map[string]string, map[string][]string) {
	inv, ip := r.inv, r.ip(node)
	svcs := map[string]any{}
	volumes := map[string]any{}
	env := map[string]string{}
	stages := map[string][]string{}
	add := func(kind, name string, def map[string]any, vols ...string) {
		svcs[name] = def
		for _, v := range vols {
			volumes[v] = map[string]any{}
		}
		stages[serviceStage[kind]] = append(stages[serviceStage[kind]], name)
	}
	idx := func(list []string) int {
		for i, v := range list {
			if v == node {
				return i
			}
		}
		return 0
	}
	for _, kind := range services {
		switch kind {
		case "etcd":
			var cluster []string
			for _, n := range inv.Etcd.Nodes {
				cluster = append(cluster, n+"=http://"+r.ip(n)+":2380")
			}
			add(kind, "etcd", service(inv.Images.Etcd, map[string]any{"command": []string{"etcd", "--name", node, "--data-dir", "/etcd-data", "--listen-client-urls", "http://0.0.0.0:2379", "--advertise-client-urls", "http://" + ip + ":2379", "--listen-peer-urls", "http://0.0.0.0:2380", "--initial-advertise-peer-urls", "http://" + ip + ":2380", "--initial-cluster", strings.Join(cluster, ","), "--initial-cluster-state", "new", "--initial-cluster-token", inv.Name + "-etcd"}, "volumes": []string{"etcd-data:/etcd-data"}}), "etcd-data")
		case "postgres":
			var etcdHosts []string
			for _, n := range inv.Etcd.Nodes {
				etcdHosts = append(etcdHosts, "'"+r.ip(n)+":2379'")
			}
			sync := "false"
			if inv.Postgres.Synchronous {
				sync = "true"
			}
			// Members advertise the node address: outside Kubernetes Spilo
			// resolves the host name, which Ubuntu maps to 127.0.1.1, and
			// replicas could never reach the leader.
			// Spilo's bg_mon monitor listens on 0.0.0.0:8080 by default, which
			// would take the web port of a node that also runs the web; it
			// listens on loopback port 8009 instead (local parameters apply
			// on every start).
			spilo := fmt.Sprintf("bootstrap:\n  dcs:\n    synchronous_mode: %s\n    postgresql:\n      parameters:\n        max_connections: %d\n        wal_level: replica\npostgresql:\n  connect_address: %[3]s:5432\n  parameters:\n    bg_mon.listen_address: 127.0.0.1\n    bg_mon.port: %[4]d\nrestapi:\n  connect_address: %[3]s:8008\n", sync, inv.Postgres.MaxConnections, ip, BgMonPort)
			add(kind, "postgres", service(inv.Images.Postgres, map[string]any{"environment": map[string]string{"SCOPE": inv.Name + "-pg", "PGVERSION": "17", "POD_IP": ip, "ETCD3_HOSTS": strings.Join(etcdHosts, ","), "PGUSER_SUPERUSER": "postgres", "PGPASSWORD_SUPERUSER": "${POSTGRES_SUPERUSER_PASSWORD}", "PGUSER_STANDBY": "standby", "PGPASSWORD_STANDBY": "${POSTGRES_REPLICATION_PASSWORD}", "SPILO_PROVIDER": "local", "ALLOW_NOSSL": "true", "PGROOT": "/home/postgres/pgdata/pgroot", "SPILO_CONFIGURATION": spilo}, "shm_size": "1g", "volumes": []string{"postgres-data:/home/postgres/pgdata"}}), "postgres-data")
			env["POSTGRES_SUPERUSER_PASSWORD"], env["POSTGRES_REPLICATION_PASSWORD"] = r.s.PostgresSuperuserPassword, r.s.PostgresReplicationPassword
			if node == inv.Postgres.Nodes[0] {
				files[node+"/toolaccounts/postgres.sh"] = toolaccounts.Postgres
				var hosts []string
				for _, member := range inv.Postgres.Nodes {
					hosts = append(hosts, r.ip(member))
				}
				svcs["postgres-tool-admin"] = service(inv.Images.Postgres, map[string]any{"restart": "no", "entrypoint": []string{"/bin/sh", "/toolaccounts/postgres.sh"}, "environment": map[string]string{"PGHOST": strings.Join(hosts, ","), "PGPORT": "5432", "PGDATABASE": "postgres", "PGUSER": "postgres", "PGPASSWORD": "${POSTGRES_SUPERUSER_PASSWORD}", "PGTARGETSESSIONATTRS": "read-write", "SERVICE_ADMIN_USER": "${SERVICE_ADMIN_USER}", "SERVICE_ADMIN_PASSWORD": "${SERVICE_ADMIN_PASSWORD}"}, "volumes": []string{"./toolaccounts/postgres.sh:/toolaccounts/postgres.sh:ro"}})
				env["SERVICE_ADMIN_USER"], env["SERVICE_ADMIN_PASSWORD"] = r.s.ServiceAdminUser, r.s.ServiceAdminPassword
			}
		case "redpanda":
			seeds := r.hostList(inv.Redpanda.Nodes, 33145, ",")
			add(kind, "redpanda", service(inv.Images.Redpanda, map[string]any{"command": []string{"redpanda", "start", "--smp", strconv.Itoa(inv.Redpanda.SMP), "--memory", inv.Redpanda.Memory, "--node-id", strconv.Itoa(idx(inv.Redpanda.Nodes)), "--check=false", "--kafka-addr", "0.0.0.0:9092", "--advertise-kafka-addr", ip + ":9092", "--rpc-addr", "0.0.0.0:33145", "--advertise-rpc-addr", ip + ":33145", "--seeds", seeds, "--schema-registry-addr", "0.0.0.0:18081", "--pandaproxy-addr", "0.0.0.0:18082", "--advertise-pandaproxy-addr", ip + ":18082", "--set", "redpanda.default_topic_replications=" + strconv.Itoa(inv.Redpanda.Replication), "--set", "redpanda.auto_create_topics_enabled=false", "--set", "redpanda.empty_seed_starts_cluster=false", "--set", "redpanda.enable_sasl=true", "--set", "redpanda.admin_api_require_auth=true", "--set", `redpanda.superusers=["${IOT_KAFKA_ADMIN_USERNAME:-admin}"]`}, "environment": map[string]string{"RP_BOOTSTRAP_USER": "${IOT_KAFKA_ADMIN_USERNAME:-admin}:${IOT_KAFKA_ADMIN_PASSWORD:-admin123}:${IOT_KAFKA_SASL_MECHANISM:-SCRAM-SHA-256}", "RPK_USER": "${IOT_KAFKA_ADMIN_USERNAME:-admin}", "RPK_PASS": "${IOT_KAFKA_ADMIN_PASSWORD:-admin123}", "RPK_SASL_MECHANISM": "${IOT_KAFKA_SASL_MECHANISM:-SCRAM-SHA-256}"}, "volumes": []string{"redpanda-data:/var/lib/redpanda/data"}}), "redpanda-data")
			for key, value := range r.s.kafkaEnv() {
				if key == "IOT_KAFKA_ADMIN_USERNAME" || key == "IOT_KAFKA_ADMIN_PASSWORD" || key == "IOT_KAFKA_SASL_MECHANISM" {
					env[key] = value
				}
			}
		case "emqx":
			var seeds []string
			for _, n := range inv.EMQX.Nodes {
				seeds = append(seeds, `"emqx@`+r.ip(n)+`"`)
			}
			add(kind, "emqx", service(inv.Images.EMQX, map[string]any{"ulimits": map[string]any{"nofile": map[string]int{"soft": 1048576, "hard": 1048576}}, "entrypoint": []string{"/bin/sh", "-ec"}, "command": []string{emqxEntrypoint}, "environment": map[string]string{"EMQX_NODE__NAME": "emqx@" + ip, "EMQX_NODE__COOKIE": "${EMQX_COOKIE}", "EMQX_CLUSTER__DISCOVERY_STRATEGY": "static", "EMQX_CLUSTER__STATIC__SEEDS": "[" + strings.Join(seeds, ",") + "]", "IOT_EMQX_API_KEY": "${IOT_EMQX_API_KEY}", "IOT_EMQX_API_SECRET": "${IOT_EMQX_API_SECRET}", "IOT_JWT_SECRET": "${IOT_JWT_SECRET}", "IOT_MQTT_TOOL_USERNAME": "${IOT_MQTT_TOOL_USERNAME:-admin}", "IOT_MQTT_TOOL_PASSWORD": "${IOT_MQTT_TOOL_PASSWORD:-admin123}", "EMQX_AUTHORIZATION__NO_MATCH": "deny", "EMQX_AUTHORIZATION__DENY_ACTION": "ignore", "EMQX_MQTT__MAX_MQUEUE_LEN": "100000", "EMQX_MQTT__MAX_INFLIGHT": "128", "EMQX_DASHBOARD__DEFAULT_USERNAME": "admin", "EMQX_DASHBOARD__DEFAULT_PASSWORD": "${EMQX_DASHBOARD_PASSWORD}"}, "volumes": r.withTLS(node, files, []string{"emqx-data:/opt/emqx/data", "emqx-log:/opt/emqx/log"}, "/opt/emqx/etc/torchlink-tls")}), "emqx-data", "emqx-log")
			env["EMQX_COOKIE"], env["EMQX_DASHBOARD_PASSWORD"] = r.s.EMQXCookie, r.s.EMQXDashboardPassword
			env["IOT_JWT_SECRET"], env["IOT_EMQX_API_KEY"], env["IOT_EMQX_API_SECRET"] = r.s.JWTSecret, r.s.EMQXAPIKey, r.s.EMQXAPISecret
			env["IOT_MQTT_TOOL_USERNAME"], env["IOT_MQTT_TOOL_PASSWORD"] = r.s.MQTTToolUsername, r.s.MQTTToolPassword
		case "clickhouse":
			files[node+"/clickhouse/config.d/cluster.xml"] = []byte(r.clickhouseConfig(node))
			add(kind, "clickhouse", service(inv.Images.ClickHouse, map[string]any{"ulimits": map[string]any{"nofile": map[string]int{"soft": 262144, "hard": 262144}}, "environment": map[string]string{"CLICKHOUSE_DB": "iot", "CLICKHOUSE_USER": "iot", "CLICKHOUSE_PASSWORD": "${CLICKHOUSE_PASSWORD}", "CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT": "1"}, "volumes": []string{"clickhouse-data:/var/lib/clickhouse", "./clickhouse/config.d/cluster.xml:/etc/clickhouse-server/config.d/cluster.xml:ro"}}), "clickhouse-data")
			env["CLICKHOUSE_PASSWORD"] = r.s.ClickHousePassword
			files[node+"/toolaccounts/clickhouse.sh"] = toolaccounts.ClickHouse
			svcs["clickhouse-tool-admin"] = service(inv.Images.ClickHouse, map[string]any{"restart": "no", "entrypoint": []string{"/bin/sh", "/toolaccounts/clickhouse.sh"}, "environment": map[string]string{"CLICKHOUSE_HOST": ip, "CLICKHOUSE_PORT": "9000", "CLICKHOUSE_USER": "iot", "CLICKHOUSE_PASSWORD": "${CLICKHOUSE_PASSWORD}", "SERVICE_ADMIN_USER": "${SERVICE_ADMIN_USER}", "SERVICE_ADMIN_PASSWORD": "${SERVICE_ADMIN_PASSWORD}"}, "volumes": []string{"./toolaccounts/clickhouse.sh:/toolaccounts/clickhouse.sh:ro"}})
			env["SERVICE_ADMIN_USER"], env["SERVICE_ADMIN_PASSWORD"] = r.s.ServiceAdminUser, r.s.ServiceAdminPassword
		case "keeper":
			files[node+"/keeper/keeper_config.xml"] = []byte(r.keeperConfig(node))
			add(kind, "keeper", service(inv.Images.Keeper, map[string]any{"volumes": []string{"keeper-data:/var/lib/clickhouse-keeper", "./keeper/keeper_config.xml:/etc/clickhouse-keeper/keeper_config.xml:ro"}}), "keeper-data")
		case "redis":
			cmd := []string{"redis-server", "--appendonly", "yes", "--requirepass", "${REDIS_PASSWORD}", "--masterauth", "${REDIS_PASSWORD}", "--replica-announce-ip", ip}
			cmd = append(cmd, "--user", "${SERVICE_ADMIN_USER}", "on", ">${SERVICE_ADMIN_PASSWORD}", "~*", "&*", "+@all")
			if node != inv.Redis.Master {
				cmd = append(cmd, "--replicaof", r.ip(inv.Redis.Master), "6379")
			}
			add(kind, "redis", service(inv.Images.Redis, map[string]any{"command": cmd, "volumes": []string{"redis-data:/data"}}), "redis-data")
			env["REDIS_PASSWORD"] = r.s.RedisPassword
			env["SERVICE_ADMIN_USER"], env["SERVICE_ADMIN_PASSWORD"] = r.s.ServiceAdminUser, r.s.ServiceAdminPassword
		case "sentinel":
			// Sentinel rewrites its config; it is created once from the
			// environment so the password never sits in a rendered file.
			script := fmt.Sprintf(`if [ ! -f /data/sentinel.conf ]; then printf 'port 26379\nsentinel announce-ip %s\nsentinel monitor iot-redis %s 6379 %d\nsentinel auth-pass iot-redis %%s\nsentinel down-after-milliseconds iot-redis 5000\nsentinel failover-timeout iot-redis 60000\nsentinel parallel-syncs iot-redis 1\n' "$$REDIS_PASSWORD" > /data/sentinel.conf; fi; exec redis-sentinel /data/sentinel.conf`, ip, r.ip(inv.Redis.Master), inv.Redis.Quorum)
			add(kind, "sentinel", service(inv.Images.Redis, map[string]any{"entrypoint": []string{"/bin/sh", "-ec"}, "command": []string{script}, "environment": map[string]string{"REDIS_PASSWORD": "${REDIS_PASSWORD}"}, "volumes": []string{"sentinel-data:/data"}}), "sentinel-data")
			env["REDIS_PASSWORD"] = r.s.RedisPassword
		case "rustfs":
			// One data directory per node. Distributed nodes are listed as
			// rustfs1...N (mapped to the node addresses), since RustFS
			// expands a single ellipsis argument into one erasure pool.
			environment := map[string]string{"RUSTFS_ACCESS_KEY": "${MINIO_ROOT_USER}", "RUSTFS_SECRET_KEY": "${MINIO_ROOT_PASSWORD}", "RUSTFS_ADDRESS": ":9002", "RUSTFS_CONSOLE_ENABLE": "true", "RUSTFS_CONSOLE_ADDRESS": "127.0.0.1:9003", "RUSTFS_VOLUMES": "/data"}
			def := map[string]any{"environment": environment, "volumes": []string{"rustfs-data:/data"}}
			if members := inv.RustFS.Members(); len(members) > 1 {
				environment["RUSTFS_VOLUMES"] = fmt.Sprintf("http://rustfs{1...%d}:9002/data", len(members))
				hosts := make([]string, len(members))
				for i, m := range members {
					hosts[i] = fmt.Sprintf("rustfs%d:%s", i+1, r.ip(m))
				}
				def["extra_hosts"] = hosts
			}
			add(kind, "rustfs", service(inv.Images.RustFS, def), "rustfs-data")
			env["MINIO_ROOT_USER"], env["MINIO_ROOT_PASSWORD"] = r.s.MinIORootUser, r.s.MinIORootPassword
		case "harness":
			origins := []string{inv.APIURL()}
			for _, n := range inv.Platform.Roles["api"].Nodes {
				origins = append(origins, "http://"+r.ip(n)+":8081")
			}
			add(kind, "harness", service(inv.Images.Harness, map[string]any{"environment": map[string]string{"IOT_HARNESS_HOST": "0.0.0.0", "IOT_HARNESS_PORT": "8091", "IOT_HARNESS_GATEWAY_TOKEN": "${IOT_AI_HARNESS_TOKEN}", "IOT_HARNESS_SESSION_ROOT": "/data/sessions", "IOT_HARNESS_HOME": "/data/runtime-home", "IOT_HARNESS_WORKSPACE": "/data/workspace", "IOT_HARNESS_PLUGIN_DIR": "/data/plugins", "IOT_HARNESS_PLUGIN_SEED_DIR": "/harness/examples/iot-ops-agent/plugins", "IOT_HARNESS_RUN_TIMEOUT_MS": "300000", "IOT_HARNESS_RPC_TIMEOUT_MS": "240000", "IOT_HARNESS_MCP_ALLOWED_ORIGINS": strings.Join(origins, ","), "DEEPSEEK_API_KEY": "${DEEPSEEK_API_KEY}", "DEEPSEEK_BASE_URL": "https://api.deepseek.com"}, "volumes": []string{"harness-data:/data"}}), "harness-data")
			env["IOT_AI_HARNESS_TOKEN"], env["DEEPSEEK_API_KEY"] = r.s.HarnessToken, r.s.DeepSeekAPIKey
		case "video":
			add(kind, "zlmediakit", service(inv.Images.Video, map[string]any{"environment": map[string]string{"IOT_VIDEO_MEDIA_SECRET": "${IOT_VIDEO_MEDIA_SECRET}", "IOT_VIDEO_HOOK_SECRET": "${IOT_VIDEO_HOOK_SECRET}", "IOT_VIDEO_MEDIA_SERVER_ID": mediaServerID(inv, idx(inv.Video.Members())), "IOT_VIDEO_HOOK_BASE": inv.APIURL() + "/api/v1/video/hooks", "IOT_VIDEO_RTC_PORT": "8000", "IOT_VIDEO_RTC_EXTERN_IP": ip, "IOT_VIDEO_RTP_PORT_MIN": "30000", "IOT_VIDEO_RTP_PORT_MAX": "30063"}, "tmpfs": []string{"/opt/media/hls:size=512m"}}))
			env["IOT_VIDEO_MEDIA_SECRET"], env["IOT_VIDEO_HOOK_SECRET"] = r.s.VideoMediaSecret, r.s.VideoHookSecret
		case "backup":
			add(kind, "backup-service", service(inv.Images.Backup, map[string]any{"environment": map[string]string{"IOT_BACKUP_HTTP_ADDR": ":8090", "IOT_BACKUP_DIR": "/app/data/backups", "IOT_POSTGRES_DSN": r.postgresDSN("read-write"), "IOT_MINIO_ENDPOINT": r.objectStorageEndpoint(), "IOT_MINIO_ACCESS_KEY": "${MINIO_ROOT_USER}", "IOT_MINIO_SECRET_KEY": "${MINIO_ROOT_PASSWORD}", "IOT_CLICKHOUSE_URL": r.clickhouseURL(node, 0), "IOT_BACKUP_ENABLED": "true", "IOT_BACKUP_TIME": "00:05", "IOT_BACKUP_TIMEZONE": "Asia/Shanghai", "IOT_BACKUP_ADMIN_TOKEN": "${IOT_BACKUP_ADMIN_TOKEN}", "IOT_BACKUP_RESTORE_TARGET_DSN": "${IOT_BACKUP_RESTORE_TARGET_DSN:-}", "IOT_BACKUP_HARNESS_DATA_DIR": "${IOT_BACKUP_HARNESS_DATA_DIR:-}", "IOT_BACKUP_HARNESS_SNAPSHOT_URLS": strings.ReplaceAll(harnessURLs(r), ":8091", ":8091/v1/backup/snapshot"), "IOT_AI_HARNESS_TOKEN": "${IOT_AI_HARNESS_TOKEN}", "IOT_BACKUP_RESTORE_HARNESS_DIR": "/app/data/backups/restored-harness", "IOT_BACKUP_RESTORE_MINIO_ENDPOINT": "${IOT_BACKUP_RESTORE_MINIO_ENDPOINT:-}", "IOT_BACKUP_RESTORE_MINIO_ACCESS_KEY": "${IOT_BACKUP_RESTORE_MINIO_ACCESS_KEY:-}", "IOT_BACKUP_RESTORE_MINIO_SECRET_KEY": "${IOT_BACKUP_RESTORE_MINIO_SECRET_KEY:-}"}, "volumes": []string{"backup-staging:/app/data/backups"}}), "backup-staging")
			env["POSTGRES_PASSWORD"], env["MINIO_ROOT_USER"], env["MINIO_ROOT_PASSWORD"], env["CLICKHOUSE_PASSWORD"], env["IOT_BACKUP_ADMIN_TOKEN"] = r.s.PostgresPassword, r.s.MinIORootUser, r.s.MinIORootPassword, r.s.ClickHousePassword, r.s.BackupToken
			env["IOT_BACKUP_RESTORE_TARGET_DSN"] = r.s.BackupRestoreTargetDSN
			env["IOT_BACKUP_RESTORE_MINIO_ENDPOINT"] = r.s.BackupRestoreMinIOEndpoint
			env["IOT_BACKUP_RESTORE_MINIO_ACCESS_KEY"] = r.s.BackupRestoreMinIOAccessKey
			env["IOT_BACKUP_RESTORE_MINIO_SECRET_KEY"] = r.s.BackupRestoreMinIOSecretKey
			env["IOT_BACKUP_RESTORE_MINIO_USE_TLS"] = inv.Env["IOT_BACKUP_RESTORE_MINIO_USE_TLS"]
			svcs["backup-service"].(map[string]any)["environment"].(map[string]string)["IOT_BACKUP_RESTORE_MINIO_USE_TLS"] = "${IOT_BACKUP_RESTORE_MINIO_USE_TLS:-false}"
			env["IOT_AI_HARNESS_TOKEN"] = r.s.HarnessToken
			if contains(inv.Harness.Nodes, node) {
				def := svcs["backup-service"].(map[string]any)
				def["volumes"] = append(def["volumes"].([]string), "harness-data:/app/harness:ro")
				def["environment"].(map[string]string)["IOT_BACKUP_HARNESS_DATA_DIR"] = "/app/harness"
			} else if dir := inv.Env["IOT_BACKUP_HARNESS_DATA_DIR"]; dir != "" {
				def := svcs["backup-service"].(map[string]any)
				def["volumes"] = append(def["volumes"].([]string), dir+":/app/harness:ro")
				def["environment"].(map[string]string)["IOT_BACKUP_HARNESS_DATA_DIR"] = "/app/harness"
			}
		case "prometheus":
			files[node+"/prometheus/prometheus.yml"] = []byte(r.prometheusConfig(node))
			files[node+"/prometheus/alerts.yml"] = []byte(clusterAlertRules())
			add(kind, "prometheus", service(inv.Images.Prometheus, map[string]any{"command": []string{"--config.file=/etc/prometheus/prometheus.yml", "--storage.tsdb.path=/prometheus", "--storage.tsdb.retention.time=30d", "--web.enable-lifecycle"}, "volumes": []string{"prometheus-data:/prometheus", "./prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro", "./prometheus/alerts.yml:/etc/prometheus/alerts.yml:ro"}}), "prometheus-data")
			files[node+"/alertmanager/alertmanager.yml"] = []byte(alertmanagerConfig(r.s.AlertWebhookURL != ""))
			if r.s.AlertWebhookURL != "" {
				files[node+"/alertmanager/webhook-url"] = []byte(r.s.AlertWebhookURL)
			}
			amCommand := []string{"--config.file=/etc/alertmanager/alertmanager.yml", "--storage.path=/alertmanager", "--web.listen-address=:9093", "--cluster.listen-address="}
			if peers := inv.Monitoring.Members(); len(peers) > 1 {
				// One Alertmanager cluster: silences and notification state
				// are shared, and alerts from every replica are deduplicated.
				amCommand = []string{"--config.file=/etc/alertmanager/alertmanager.yml", "--storage.path=/alertmanager", "--web.listen-address=:9093", "--cluster.listen-address=0.0.0.0:9094", "--cluster.advertise-address=" + ip + ":9094"}
				for _, p := range peers {
					if p != node {
						amCommand = append(amCommand, "--cluster.peer="+r.ip(p)+":9094")
					}
				}
			}
			// Single files: the rendered directories are not readable by the
			// container's unprivileged user.
			amVolumes := []string{"alertmanager-data:/alertmanager", "./alertmanager/alertmanager.yml:/etc/alertmanager/alertmanager.yml:ro"}
			if r.s.AlertWebhookURL != "" {
				amVolumes = append(amVolumes, "./alertmanager/webhook-url:/etc/alertmanager/webhook-url:ro")
			}
			add(kind, "alertmanager", service(inv.Images.Alertmanager, map[string]any{"command": amCommand, "volumes": amVolumes}), "alertmanager-data")
		case "capacity":
			add(kind, "capacity", service(inv.Images.Platform, map[string]any{
				"entrypoint": []string{"/app/capacity-test"},
				"command":    []string{"serve", "--self", "--listen", ":7080", "--results", "/app/data/capacity-results"},
				"environment": map[string]string{
					"IOT_CAPACITY_API_URL": inv.APIURL(), "IOT_CAPACITY_MQTT_URL": "tcp://" + r.ip(inv.EMQX.Nodes[0]) + ":1883",
					"IOT_CAPACITY_WEB_URL": "http://" + r.ip(inv.Platform.Web.Nodes[0]) + ":8080", "IOT_CAPACITY_METRICS": r.capacityMetrics(),
					"IOT_CAPACITY_NODES": r.capacityNodes(), "IOT_CAPACITY_POSTGRES_DSN": r.postgresDSN("prefer-standby"),
					"IOT_CAPACITY_CLICKHOUSE_URL": r.clickhouseURL(node, 0), "IOT_CAPACITY_SERVICE_TOKEN": "${IOT_OPS_CAPACITY_TOKEN}",
				},
				"volumes": []string{"capacity-data:/app/data"},
			}), "capacity-data")
			env["POSTGRES_PASSWORD"], env["CLICKHOUSE_PASSWORD"], env["IOT_OPS_CAPACITY_TOKEN"] = r.s.PostgresPassword, r.s.ClickHousePassword, r.s.CapacityToken
		case "lb":
			files[node+"/lb/haproxy.cfg"] = []byte(r.haproxyConfig())
			add(kind, "lb", service(inv.Images.LB, map[string]any{"volumes": []string{"./lb/haproxy.cfg:/usr/local/etc/haproxy/haproxy.cfg:ro"}}))
		case "node-exporter":
			add(kind, "node-exporter", service(inv.Images.NodeExporter, map[string]any{"command": []string{"--path.rootfs=/host"}, "pid": "host", "volumes": []string{"/:/host:ro"}}))
		case "web":
			api := strings.TrimPrefix(strings.TrimPrefix(inv.APIURL(), "http://"), "https://")
			if contains(inv.Platform.Roles["api"].Nodes, node) && !inv.AutoLB() {
				api = "127.0.0.1:8081"
			}
			videoUpstream := "127.0.0.1:1"
			if media := inv.Video.Members(); len(media) == 1 {
				videoUpstream = r.ip(media[0]) + ":80"
			} else if len(media) > 1 {
				// HLS follows the live module to the first healthy server.
				videoUpstream = fmt.Sprintf("127.0.0.1:%d", LBVideoPort)
			}
			web := service(inv.Images.Web, map[string]any{"environment": map[string]string{"IOT_API_UPSTREAM": api, "IOT_VIDEO_UPSTREAM": videoUpstream}})
			if r.tls != nil {
				// HTTPS on 8443; plain 8080 then redirects there.
				web["volumes"] = r.withTLS(node, files, nil, "/etc/torchlink/tls")
				web["environment"].(map[string]string)["IOT_WEB_HTTPS_PORT"] = "8443"
				web["environment"].(map[string]string)["IOT_WEB_TLS_REDIRECT"] = "${IOT_WEB_TLS_REDIRECT:-true}"
			}
			add(kind, "web", web)
		default: // platform roles
			salt := idx(inv.Platform.Roles[kind].Nodes)
			name := "iot-" + kind
			// Every platform role serves /health/live on IOT_HTTP_ADDR; the
			// distroless image probes it with its own healthcheck subcommand.
			def := service(inv.Images.Platform, map[string]any{"environment": r.platformEnv(kind, node, salt), "volumes": []string{name + "-data:/app/data"}, "healthcheck": map[string]any{
				"test": []string{"CMD", "/app/iot-platform", "healthcheck"}, "interval": "15s", "timeout": "5s", "retries": 4, "start_period": "60s",
			}})
			if kind == "gateway" || kind == "api" {
				// One file descriptor per TCP device session.
				def["ulimits"] = map[string]any{"nofile": map[string]int{"soft": 1048576, "hard": 1048576}}
			}
			if r.s.KafkaTLSCAFile != "" {
				def["volumes"] = append(def["volumes"].([]string), "./kafka/ca.pem:"+kafkaCAContainerPath+":ro")
			}
			if kind == "api" || kind == "gateway" || kind == "parser" {
				// Uploaded protocol code runs only in the node's isolated runner.
				def["volumes"] = append(def["volumes"].([]string), "protocol-runner-socket:/run/torchlink")
				environment := def["environment"].(map[string]string)
				environment["IOT_PROTOCOL_SANDBOX"], environment["IOT_PROTOCOL_RUNNER_SOCKET"] = "runner", "/run/torchlink/runner.sock"
				if _, exists := svcs["protocol-runner"]; !exists {
					add(kind, "protocol-runner", service(inv.Images.Platform, map[string]any{
						"network_mode": "none", "read_only": true, "cap_drop": []string{"ALL"}, "security_opt": []string{"no-new-privileges:true"},
						"pids_limit": 512, "mem_limit": "2g", "tmpfs": []string{"/tmp:size=2g,mode=1777,exec"},
						"environment": map[string]string{"IOT_PROCESS_ROLE": "protocol-runner", "IOT_PROTOCOL_RUNNER_SOCKET": "/run/torchlink/runner.sock", "IOT_PROTOCOL_RUNNER_DIR": "/tmp/protocol-runner"},
						"volumes":     []string{"protocol-runner-socket:/run/torchlink"},
					}), "protocol-runner-socket")
				}
			}
			if kind == "api" {
				// Knowledge vectors and reranking beside every API instance.
				for svc, mode := range map[string]map[string]string{
					"embedding": {"LLAMA_ARG_MODEL": "/models/bge-m3-Q8_0.gguf", "LLAMA_ARG_EMBEDDINGS": "true", "LLAMA_ARG_POOLING": "cls", "LLAMA_ARG_PORT": strconv.Itoa(LocalEmbeddingPort)},
					"reranker":  {"LLAMA_ARG_MODEL": "/models/bge-reranker-v2-m3-Q8_0.gguf", "LLAMA_ARG_RERANKING": "true", "LLAMA_ARG_PORT": strconv.Itoa(LocalRerankPort)},
				} {
					mode["LLAMA_ARG_HOST"] = "127.0.0.1"
					add(kind, svc, service(inv.Images.LocalAI, map[string]any{"environment": mode, "healthcheck": map[string]any{
						"test": []string{"CMD", "curl", "-fsS", "http://127.0.0.1:" + mode["LLAMA_ARG_PORT"] + "/health"}, "interval": "10s", "timeout": "5s", "retries": 12, "start_period": "30s",
					}}))
				}
			}
			add(kind, name, def, name+"-data")
			for k, v := range r.s.kafkaEnv() {
				env[k] = v
			}
			if r.s.MQTTPublicURL != "" {
				env["IOT_DEVICE_MQTT_PUBLIC_URL"] = r.s.MQTTPublicURL
			}
			env["IOT_JWT_SECRET"], env["IOT_ADMIN_PASSWORD"], env["POSTGRES_PASSWORD"], env["REDIS_PASSWORD"], env["CLICKHOUSE_PASSWORD"] = r.s.JWTSecret, r.s.AdminPassword, r.s.PostgresPassword, r.s.RedisPassword, r.s.ClickHousePassword
			env["MINIO_ROOT_USER"], env["MINIO_ROOT_PASSWORD"], env["IOT_EMQX_API_KEY"], env["IOT_EMQX_API_SECRET"] = r.s.MinIORootUser, r.s.MinIORootPassword, r.s.EMQXAPIKey, r.s.EMQXAPISecret
			env["IOT_MQTT_TOOL_USERNAME"], env["IOT_MQTT_TOOL_PASSWORD"] = r.s.MQTTToolUsername, r.s.MQTTToolPassword
			env["IOT_AI_HARNESS_TOKEN"], env["DEEPSEEK_API_KEY"], env["IOT_BACKUP_ADMIN_TOKEN"] = r.s.HarnessToken, r.s.DeepSeekAPIKey, r.s.BackupToken
			if len(inv.Video.Members()) > 0 {
				env["IOT_VIDEO_MEDIA_SECRET"], env["IOT_VIDEO_HOOK_SECRET"], env["IOT_VIDEO_CREDENTIAL_KEY"] = r.s.VideoMediaSecret, r.s.VideoHookSecret, r.s.VideoCredentialKey
			}
			if inv.Capacity.Node != "" {
				env["IOT_OPS_CAPACITY_TOKEN"] = r.s.CapacityToken
			}
			env["IOT_EMBEDDING_API_KEY"] = r.s.EmbeddingAPIKey
		}
	}
	compose := map[string]any{"name": inv.Name, "services": svcs}
	if len(volumes) > 0 {
		compose["volumes"] = volumes
	}
	for _, list := range stages {
		sort.Strings(list)
	}
	return compose, env, stages
}

// capacityMetrics lists every platform process for the capacity module as
// role@instance=url (instances match the Prometheus labels).
func (r renderer) capacityMetrics() string {
	var out []string
	for _, role := range RoleNames {
		for _, n := range r.inv.Platform.Roles[role].Nodes {
			out = append(out, fmt.Sprintf("%s@%s-%s=http://%s:%d/metrics", role, role, n, r.ip(n), rolePort[role]))
		}
	}
	return strings.Join(out, ",")
}

func (r renderer) capacityNodes() string {
	var out []string
	for _, n := range r.inv.Nodes {
		out = append(out, fmt.Sprintf("%s=http://%s:9100/metrics", n.Name, n.Address))
	}
	return strings.Join(out, ",")
}

// mediaServerID names the i-th media server of the video placement.
func mediaServerID(inv *Inventory, i int) string {
	return fmt.Sprintf("%s-media-%d", inv.Name, i+1)
}

// objectStorageEndpoint is the RustFS address platform services use: the
// server, or the local load balancer in front of a distributed deployment.
func (r renderer) objectStorageEndpoint() string {
	members := r.inv.RustFS.Members()
	if len(members) > 1 {
		return fmt.Sprintf("127.0.0.1:%d", LBObjectStoragePort)
	}
	return r.ip(members[0]) + ":9002"
}

// haproxyConfig balances the local API and gateway origins across every
// instance with readiness checks. It binds to 127.0.0.1 only: it serves the
// node's own services (web proxy, Harness callbacks, media hooks), not users.
func (r renderer) haproxyConfig() string {
	var b strings.Builder
	b.WriteString(`global
  maxconn 20000
defaults
  mode http
  option httpchk GET /health/ready
  option redispatch
  retries 2
  timeout connect 5s
  timeout client 10m
  timeout server 10m
  timeout tunnel 1h
  default-server check inter 3s fall 3 rise 2
`)
	if r.inv.AutoLB() {
		for _, fe := range []struct {
			name string
			port int
			role string
		}{{"api", LBAPIPort, "api"}, {"gateway", LBGatewayPort, "gateway"}} {
			fmt.Fprintf(&b, "frontend %s\n  bind 127.0.0.1:%d\n  default_backend %s\nbackend %s\n  balance roundrobin\n", fe.name, fe.port, fe.name, fe.name)
			for _, n := range r.inv.Platform.Roles[fe.role].Nodes {
				fmt.Fprintf(&b, "  server %s-%s %s:%d\n", fe.role, n, r.ip(n), rolePort[fe.role])
			}
		}
	}
	pool := func(name string, port int, nodes []string, target int, lines ...string) {
		fmt.Fprintf(&b, "frontend %s\n  bind 127.0.0.1:%d\n  default_backend %s\nbackend %s\n", name, port, name, name)
		for _, line := range lines {
			b.WriteString("  " + line + "\n")
		}
		for _, n := range nodes {
			fmt.Fprintf(&b, "  server %s-%s %s:%d\n", name, n, r.ip(n), target)
		}
	}
	if nodes := r.inv.RustFS.Members(); len(nodes) > 1 {
		pool("rustfs", LBObjectStoragePort, nodes, 9002, "balance leastconn", "option httpchk GET /health")
	}
	if nodes := r.inv.Monitoring.Members(); len(nodes) > 1 {
		pool("prometheus", LBPrometheusPort, nodes, 9090, "balance first", "option httpchk GET /-/ready")
		pool("alertmanager", LBAlertmanagerPort, nodes, 9093, "balance first", "option httpchk GET /-/ready")
	}
	if nodes := r.inv.Video.Members(); len(nodes) > 1 {
		// The first healthy media server in placement order, like the live
		// module: a failed server is dropped after 15 s and a recovered one
		// used again after 45 s healthy.
		pool("video", LBVideoPort, nodes, 80, "balance first", "option tcp-check", "default-server check inter 5s fall 3 rise 9")
	}
	return b.String()
}

const emqxEntrypoint = `umask 077
jwt_secret_b64="$$(printf '%s' "$$IOT_JWT_SECRET" | base64 | tr -d '\n')"
tool_user_json="$$(printf '%s' "$$IOT_MQTT_TOOL_USERNAME" | sed 's/\\/\\\\/g; s/"/\\"/g')"
tool_password_json="$$(printf '%s' "$$IOT_MQTT_TOOL_PASSWORD" | sed 's/\\/\\\\/g; s/"/\\"/g')"
printf '[{"user_id":"%s","password":"%s","is_superuser":true}]\n' "$$tool_user_json" "$$tool_password_json" > /opt/emqx/etc/tool-users.json
printf 'authentication = [{mechanism = password_based, backend = built_in_database, user_id_type = username, password_hash_algorithm = {name = sha256, salt_position = suffix}, bootstrap_file = "/opt/emqx/etc/tool-users.json", bootstrap_type = plain}, {mechanism = jwt, from = password, algorithm = "hmac-based", use_jwks = false, secret = "%s", secret_base64_encoded = true, acl_claim_name = "acl", verify_claims = [{name = "username", value = "$${username}"}], disconnect_after_expire = true}]\n' "$$jwt_secret_b64" > /opt/emqx/etc/base.hocon
if [ -n "$$IOT_EMQX_API_KEY" ] && [ -n "$$IOT_EMQX_API_SECRET" ]; then
  printf '%s:%s:administrator\n' "$$IOT_EMQX_API_KEY" "$$IOT_EMQX_API_SECRET" > /opt/emqx/etc/platform-api-keys
  printf '\napi_key.bootstrap_file = "/opt/emqx/etc/platform-api-keys"\n' >> /opt/emqx/etc/base.hocon
fi
sed -i 's/{allow, all}\./{deny, all}./' /opt/emqx/etc/acl.conf
tls=/opt/emqx/etc/torchlink-tls
if [ -s "$$tls/tls.crt" ] && [ -s "$$tls/tls.key" ]; then
  for listener in SSL WSS; do
    export "EMQX_LISTENERS__$${listener}__DEFAULT__ENABLE=true"
    export "EMQX_LISTENERS__$${listener}__DEFAULT__SSL_OPTIONS__CERTFILE=$$tls/tls.crt"
    export "EMQX_LISTENERS__$${listener}__DEFAULT__SSL_OPTIONS__KEYFILE=$$tls/tls.key"
    export "EMQX_LISTENERS__$${listener}__DEFAULT__SSL_OPTIONS__CACERTFILE=$$tls/tls.crt"
  done
else
  export EMQX_LISTENERS__SSL__DEFAULT__ENABLE=false EMQX_LISTENERS__WSS__DEFAULT__ENABLE=false
fi
exec /usr/bin/docker-entrypoint.sh /opt/emqx/bin/emqx foreground
`

func (r renderer) clickhouseConfig(node string) string {
	inv := r.inv
	var b strings.Builder
	b.WriteString("<clickhouse>\n  <listen_host>0.0.0.0</listen_host>\n")
	fmt.Fprintf(&b, "  <interserver_http_host>%s</interserver_http_host>\n", r.ip(node))
	fmt.Fprintf(&b, "  <remote_servers>\n    <%s>\n", inv.ClickHouse.Cluster)
	shardOf, replicaName := 0, node
	for i, shard := range inv.ClickHouse.Shards {
		b.WriteString("      <shard>\n        <internal_replication>true</internal_replication>\n")
		for _, n := range shard {
			if n == node {
				shardOf = i + 1
			}
			fmt.Fprintf(&b, "        <replica><host>%s</host><port>9000</port><user>iot</user><password from_env=\"CLICKHOUSE_PASSWORD\"/></replica>\n", r.ip(n))
		}
		b.WriteString("      </shard>\n")
	}
	fmt.Fprintf(&b, "    </%s>\n  </remote_servers>\n  <zookeeper>\n", inv.ClickHouse.Cluster)
	for _, n := range inv.ClickHouse.Keeper {
		fmt.Fprintf(&b, "    <node><host>%s</host><port>9181</port></node>\n", r.ip(n))
	}
	b.WriteString("  </zookeeper>\n")
	fmt.Fprintf(&b, "  <macros><shard>%02d</shard><replica>%s</replica><cluster>%s</cluster></macros>\n", shardOf, replicaName, inv.ClickHouse.Cluster)
	b.WriteString("  <distributed_ddl><path>/clickhouse/task_queue/ddl</path></distributed_ddl>\n</clickhouse>\n")
	return b.String()
}

func (r renderer) keeperConfig(node string) string {
	var b strings.Builder
	id := 0
	for i, n := range r.inv.ClickHouse.Keeper {
		if n == node {
			id = i + 1
		}
	}
	fmt.Fprintf(&b, "<clickhouse>\n  <listen_host>0.0.0.0</listen_host>\n  <logger><level>information</level><console>1</console></logger>\n  <keeper_server>\n    <tcp_port>9181</tcp_port>\n    <server_id>%d</server_id>\n    <log_storage_path>/var/lib/clickhouse-keeper/log</log_storage_path>\n    <snapshot_storage_path>/var/lib/clickhouse-keeper/snapshots</snapshot_storage_path>\n    <raft_configuration>\n", id)
	for i, n := range r.inv.ClickHouse.Keeper {
		fmt.Fprintf(&b, "      <server><id>%d</id><hostname>%s</hostname><port>9234</port></server>\n", i+1, r.ip(n))
	}
	b.WriteString("    </raft_configuration>\n  </keeper_server>\n</clickhouse>\n")
	return b.String()
}

// withTLS mounts the node's copy of the entry-point certificate at target
// when TLS is configured.
func (r renderer) withTLS(node string, files map[string][]byte, volumes []string, target string) []string {
	if r.tls == nil {
		return volumes
	}
	files[node+"/tls/tls.crt"], files[node+"/tls/tls.key"] = r.tls.cert, r.tls.key
	return append(volumes, "./tls:"+target+":ro")
}

// clusterAlertRules is the platform rule file with the cluster's job name
// for platform processes.
func clusterAlertRules() string {
	return strings.ReplaceAll(promrules.Alerts, `job="iot-platform"`, `job="platform"`)
}

// alertmanagerConfig sends every alert to the webhook whose URL is in
// webhook-url next to the config, when one is configured; otherwise alerts
// stay visible in Alertmanager and the operations center only.
func alertmanagerConfig(webhook bool) string {
	receiver := "  - name: platform\n"
	if webhook {
		receiver += "    webhook_configs:\n      - url_file: /etc/alertmanager/webhook-url\n        send_resolved: true\n"
	}
	return "route:\n  receiver: platform\n  group_by: [alertname, cluster]\n  group_wait: 30s\n  group_interval: 5m\n  repeat_interval: 4h\nreceivers:\n" + receiver
}

// prometheusConfig scrapes every process per instance, labelled with role
// and instance, never through a load balancer.
func (r renderer) prometheusConfig(node string) string {
	inv := r.inv
	type job struct {
		name    string
		path    string
		targets map[string][2]string // address -> (role, instance)
	}
	jobs := []job{{name: "platform", path: "/metrics", targets: map[string][2]string{}}, {name: "node", path: "/metrics", targets: map[string][2]string{}}, {name: "redpanda", path: "/public_metrics", targets: map[string][2]string{}}, {name: "emqx", path: "/api/v5/prometheus/stats", targets: map[string][2]string{}}}
	for _, role := range RoleNames {
		for _, n := range inv.Platform.Roles[role].Nodes {
			jobs[0].targets[r.ip(n)+":"+strconv.Itoa(rolePort[role])] = [2]string{role, role + "-" + n}
		}
	}
	for _, n := range inv.Nodes {
		jobs[1].targets[n.Address+":9100"] = [2]string{"node", n.Name}
	}
	for _, n := range inv.Redpanda.Nodes {
		jobs[2].targets[r.ip(n)+":9644"] = [2]string{"redpanda", n}
	}
	for _, n := range inv.EMQX.Nodes {
		jobs[3].targets[r.ip(n)+":18083"] = [2]string{"emqx", n}
	}
	var b strings.Builder
	alertmanagers, replica, relabel := `"127.0.0.1:9093"`, "", ""
	if members := inv.Monitoring.Members(); len(members) > 1 {
		// Replicas scrape the same targets independently and alert every
		// Alertmanager; dropping the replica label lets the cluster
		// deduplicate their alerts.
		targets := make([]string, len(members))
		for i, m := range members {
			targets[i] = strconv.Quote(r.ip(m) + ":9093")
		}
		alertmanagers, replica = strings.Join(targets, ", "), fmt.Sprintf("    replica: %s\n", node)
		relabel = "  alert_relabel_configs:\n    - action: labeldrop\n      regex: replica\n"
	}
	fmt.Fprintf(&b, "global:\n  scrape_interval: 15s\n  external_labels:\n    cluster: %s\n%srule_files: [/etc/prometheus/alerts.yml]\nalerting:\n%s  alertmanagers:\n    - static_configs:\n        - targets: [%s]\nscrape_configs:\n", inv.Name, replica, relabel, alertmanagers)
	for _, j := range jobs {
		fmt.Fprintf(&b, "  - job_name: %s\n    metrics_path: %s\n    static_configs:\n", j.name, j.path)
		addrs := make([]string, 0, len(j.targets))
		for a := range j.targets {
			addrs = append(addrs, a)
		}
		sort.Strings(addrs)
		for _, a := range addrs {
			l := j.targets[a]
			fmt.Fprintf(&b, "      - targets: [%q]\n        labels: {role: %q, instance: %q}\n", a, l[0], l[1])
		}
	}
	return b.String()
}

// Render validates the inventory and secrets and returns every file to
// write, keyed by relative path.
func Render(inv *Inventory, s Secrets) (map[string][]byte, error) {
	if s.ServiceAdminUser == "" && s.ServiceAdminPassword == "" {
		s.ServiceAdminUser, s.ServiceAdminPassword = "admin", "admin123"
	}
	budget, err := inv.Validate()
	if err != nil {
		return nil, err
	}
	s = s.brokerDefaults(inv)
	if err = s.validate(inv); err != nil {
		return nil, err
	}
	ca, err := s.kafkaCA()
	if err != nil {
		return nil, err
	}
	for name, img := range map[string]string{"platform": inv.Images.Platform, "web": inv.Images.Web, "harness": inv.Images.Harness, "redpanda": inv.Images.Redpanda, "emqx": inv.Images.EMQX, "etcd": inv.Images.Etcd, "postgres": inv.Images.Postgres, "redis": inv.Images.Redis, "clickhouse": inv.Images.ClickHouse, "keeper": inv.Images.Keeper, "rustfs": inv.Images.RustFS, "nodeExporter": inv.Images.NodeExporter} {
		if img == "" {
			return nil, fmt.Errorf("images.%s is required", name)
		}
	}
	if inv.Images.Alertmanager == "" {
		inv.Images.Alertmanager = DefaultImages().Alertmanager
	}
	pair, err := s.tlsPair()
	if err != nil {
		return nil, err
	}
	r := renderer{inv: inv, s: s, tls: pair}
	files := map[string][]byte{}
	if len(ca) > 0 {
		files["kafka/ca.pem"] = ca
		for _, role := range RoleNames {
			for _, node := range inv.Platform.Roles[role].Nodes {
				files[node+"/kafka/ca.pem"] = ca
			}
		}
	}
	summary := Summary{Name: inv.Name, Nodes: inv.Nodes, Services: map[string]map[string][]string{}, Budget: budget, Endpoints: map[string]string{
		"postgresWriter": r.postgresDSN("read-write"), "kafka": r.hostList(inv.Redpanda.Nodes, 9092, ","), "redisSentinels": r.hostList(inv.Redis.Sentinels, 26379, ","),
		"clickhouse": r.hostList(r.chNodes(), 8123, ","), "internalAPI": inv.APIURL(), "gateways": inv.GatewayURL(),
	}}
	placement := inv.Placement()
	nodeImages := map[string][]string{}
	for _, n := range inv.Nodes {
		compose, env, stages := r.nodeCompose(n.Name, placement[n.Name], files)
		seen := map[string]bool{}
		for _, svc := range compose["services"].(map[string]any) {
			if img, _ := svc.(map[string]any)["image"].(string); img != "" && !seen[img] {
				seen[img] = true
				nodeImages[n.Name] = append(nodeImages[n.Name], img)
			}
		}
		sort.Strings(nodeImages[n.Name])
		b, err := yaml.Marshal(compose)
		if err != nil {
			return nil, err
		}
		files[n.Name+"/compose.yaml"] = append([]byte("# Rendered by cmd/cluster-render from the cluster inventory; edit the inventory, not this file.\n"), b...)
		files[n.Name+"/.env"] = envFile(env)
		summary.Services[n.Name] = stages
	}
	plan := deployPlan(inv, summary, nodeImages)
	if pair != nil {
		// HTTPS and MQTT over TLS entry points.
		var b strings.Builder
		for _, node := range inv.Platform.Web.Nodes {
			fmt.Fprintf(&b, "entry web-https https://%s:8443\n", rNodeAddress(inv, node))
		}
		for _, node := range inv.EMQX.Nodes {
			fmt.Fprintf(&b, "entry mqtts ssl://%s:8883\n", rNodeAddress(inv, node))
		}
		plan = append(plan, b.String()...)
	}
	files["deploy-plan.txt"] = plan
	files["images.txt"] = imageList(inv)
	initEnv := map[string]string{
		"IOT_POSTGRES_DSN":              strings.ReplaceAll(r.postgresDSN("read-write"), "${POSTGRES_PASSWORD}", s.PostgresPassword),
		"IOT_POSTGRES_ADMIN_DSN":        "postgres://postgres:" + s.PostgresSuperuserPassword + "@" + r.hostList(inv.Postgres.Nodes, 5432, ",") + "/postgres?sslmode=disable&target_session_attrs=read-write",
		"IOT_POSTGRES_APP_PASSWORD":     s.PostgresPassword,
		"IOT_KAFKA_BROKERS":             r.hostList(inv.Redpanda.Nodes, 9092, ","),
		"IOT_CLICKHOUSE_URL":            strings.ReplaceAll(r.clickhouseURL("", 0), "${CLICKHOUSE_PASSWORD}", s.ClickHousePassword),
		"IOT_CLICKHOUSE_CLUSTER":        inv.ClickHouse.Cluster,
		"IOT_CLUSTER_KAFKA_PARTITIONS":  strconv.Itoa(inv.Redpanda.Partitions),
		"IOT_CLUSTER_KAFKA_REPLICATION": strconv.Itoa(inv.Redpanda.Replication),
	}
	for k, v := range s.kafkaEnv() {
		initEnv[k] = v
	}
	if brokers := inv.Env["IOT_KAFKA_BROKERS"]; brokers != "" {
		initEnv["IOT_KAFKA_BROKERS"] = brokers
	}
	if s.MQTTPublicURL != "" {
		initEnv["IOT_DEVICE_MQTT_PUBLIC_URL"] = s.MQTTPublicURL
	} else if u := inv.Env["IOT_DEVICE_MQTT_PUBLIC_URL"]; u != "" {
		initEnv["IOT_DEVICE_MQTT_PUBLIC_URL"] = u
	}
	files["init.env"] = envFile(initEnv)
	sb, _ := json.MarshalIndent(summary, "", "  ")
	// Endpoints contain ${POSTGRES_PASSWORD} placeholders only.
	files["cluster.json"] = append(sb, '\n')
	return files, nil
}

func envFile(env map[string]string) []byte {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("# Secrets for this node only. Keep mode 0600; never commit.\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s\n", k, env[k])
	}
	return []byte(b.String())
}

// deployPlan is a line-oriented plan for the deploy scripts (no JSON parser
// needed on the operator machine): "service <stage> <node> <address> <compose services...>"
// and "health <node> <url>" lines, stages in start order.
func deployPlan(inv *Inventory, summary Summary, nodeImages map[string][]string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# name %s\n", inv.Name)
	fmt.Fprintf(&b, "# platform-image %s\n", inv.Images.Platform)
	placement := inv.Placement()
	for _, n := range inv.Nodes {
		// images: what the node must have loaded; ports: host ports its
		// services bind (checked free before the first start).
		fmt.Fprintf(&b, "images %s %s %s\n", n.Name, n.Address, strings.Join(nodeImages[n.Name], " "))
		var ports []string
		for _, svc := range placement[n.Name] {
			for _, p := range Ports[svc] {
				ports = append(ports, strconv.Itoa(p))
			}
		}
		fmt.Fprintf(&b, "ports %s %s %s\n", n.Name, n.Address, strings.Join(ports, " "))
	}
	for _, stage := range Stages {
		for _, n := range inv.Nodes {
			if list := summary.Services[n.Name][stage]; len(list) > 0 {
				fmt.Fprintf(&b, "service %s %s %s %s\n", stage, n.Name, n.Address, strings.Join(list, " "))
			}
		}
	}
	for _, role := range RoleNames {
		for _, node := range inv.Platform.Roles[role].Nodes {
			n, _ := inv.node(node)
			fmt.Fprintf(&b, "health %s http://%s:%d/health/ready\n", node, n.Address, rolePort[role])
		}
	}
	fmt.Fprintf(&b, "init-service %s %s postgres-tool-admin\n", inv.Postgres.Nodes[0], rNodeAddress(inv, inv.Postgres.Nodes[0]))
	for _, shard := range inv.ClickHouse.Shards {
		for _, node := range shard {
			fmt.Fprintf(&b, "init-service %s %s clickhouse-tool-admin\n", node, rNodeAddress(inv, node))
		}
	}
	// entry: addresses users and devices connect to (put DNS, a VIP or an
	// external load balancer in front of each group).
	entry := func(kind, scheme string, nodes []string, port int) {
		for _, node := range nodes {
			n, _ := inv.node(node)
			fmt.Fprintf(&b, "entry %s %s%s:%d\n", kind, scheme, n.Address, port)
		}
	}
	entry("web", "http://", inv.Platform.Web.Nodes, 8080)
	entry("mqtt", "tcp://", inv.EMQX.Nodes, 1883)
	entry("device-http", "http://", inv.Platform.Roles["gateway"].Nodes, 8082)
	entry("device-tcp", "", inv.Platform.Roles["gateway"].Nodes, 26875)
	return []byte(b.String())
}

func rNodeAddress(inv *Inventory, name string) string {
	node, _ := inv.node(name)
	return node.Address
}

// imageList names every image the cluster runs, for an offline bundle:
// docker save $(cat images.txt) -o cluster-images.tar
func imageList(inv *Inventory) []byte {
	seen := map[string]bool{}
	v := reflect.ValueOf(inv.Images)
	var out []string
	for i := 0; i < v.NumField(); i++ {
		if img := v.Field(i).String(); img != "" && !seen[img] {
			seen[img] = true
			out = append(out, img)
		}
	}
	sort.Strings(out)
	return []byte(strings.Join(out, "\n") + "\n")
}
