package clusterplan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Secrets are read from a private YAML file and only written into the .env
// file of nodes that need them; compose.yaml and configs reference variables.
type Secrets struct {
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
	BackupToken                 string `yaml:"backupToken"`
	// BackupRestoreTargetDSN is an optional separate database for restore checks.
	BackupRestoreTargetDSN string `yaml:"backupRestoreTargetDSN"`
	DeepSeekAPIKey         string `yaml:"deepseekApiKey"`
	VideoMediaSecret       string `yaml:"videoMediaSecret"`
	VideoHookSecret        string `yaml:"videoHookSecret"`
	VideoCredentialKey     string `yaml:"videoCredentialKey"`
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
	return s, nil
}

func (s Secrets) validate(inv *Inventory) error {
	required := map[string]string{"postgresPassword": s.PostgresPassword, "postgresSuperuserPassword": s.PostgresSuperuserPassword, "postgresReplicationPassword": s.PostgresReplicationPassword, "redisPassword": s.RedisPassword, "clickhousePassword": s.ClickHousePassword, "minioRootUser": s.MinIORootUser, "minioRootPassword": s.MinIORootPassword, "jwtSecret": s.JWTSecret, "adminPassword": s.AdminPassword, "harnessToken": s.HarnessToken, "emqxApiKey": s.EMQXAPIKey, "emqxApiSecret": s.EMQXAPISecret, "emqxCookie": s.EMQXCookie, "emqxDashboardPassword": s.EMQXDashboardPassword, "backupToken": s.BackupToken}
	if inv.Video.Node != "" {
		required["videoMediaSecret"], required["videoHookSecret"], required["videoCredentialKey"] = s.VideoMediaSecret, s.VideoHookSecret, s.VideoCredentialKey
	}
	var missing []string
	for k, v := range required {
		if strings.TrimSpace(v) == "" || strings.Contains(strings.ToLower(v), "change-me") {
			missing = append(missing, k)
		}
		if strings.ContainsAny(v, "\n\r\"'$`\\") {
			missing = append(missing, k+" (must not contain quotes, $, backslashes or newlines)")
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
	"lb": "coordination", "etcd": "coordination", "keeper": "coordination", "redis": "coordination", "sentinel": "coordination", "minio": "coordination", "node-exporter": "coordination", "prometheus": "coordination",
	"postgres": "data", "redpanda": "data", "clickhouse": "data", "emqx": "data",
	"harness": "support", "ollama": "support", "weaviate": "support", "video": "support", "backup": "support",
	"parser": "workers", "processor": "workers", "ai": "workers", "jobs": "workers",
	"api": "edge", "gateway": "edge", "web": "edge",
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
}

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

var rolePort = map[string]int{"api": 8081, "gateway": 8082, "parser": 8101, "processor": 8102, "ai": 8103, "jobs": 8104}

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
		"IOT_MINIO_ENDPOINT":           r.ip(inv.MinIO.Node) + ":9002",
		"IOT_MINIO_ACCESS_KEY":         "${MINIO_ROOT_USER}",
		"IOT_MINIO_SECRET_KEY":         "${MINIO_ROOT_PASSWORD}",
		"IOT_MQTT_BROKER":              "tcp://" + r.ip(r.pick(node, inv.EMQX.Nodes, salt)) + ":1883",
		"IOT_EMQX_API_URL":             "http://" + r.ip(r.pick(node, inv.EMQX.Nodes, salt)) + ":18083",
		"IOT_EMQX_API_KEY":             "${IOT_EMQX_API_KEY}",
		"IOT_EMQX_API_SECRET":          "${IOT_EMQX_API_SECRET}",
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
	if inv.Monitoring.Node != "" {
		env["IOT_OPS_PROMETHEUS_URL"] = "http://" + r.ip(inv.Monitoring.Node) + ":9090"
	}
	if inv.Knowledge.Node != "" {
		env["IOT_WEAVIATE_URL"] = "http://" + r.ip(inv.Knowledge.Node) + ":8085"
		env["IOT_OLLAMA_URL"] = "http://" + r.ip(inv.Knowledge.Node) + ":11434"
		env["IOT_AI_OLLAMA_URL"] = env["IOT_OLLAMA_URL"]
	}
	if inv.Video.Node != "" {
		v := r.ip(inv.Video.Node)
		env["IOT_VIDEO_MEDIA_API_URL"] = "http://" + v + ":80"
		env["IOT_VIDEO_MEDIA_SECRET"] = "${IOT_VIDEO_MEDIA_SECRET}"
		env["IOT_VIDEO_HOOK_SECRET"] = "${IOT_VIDEO_HOOK_SECRET}"
		env["IOT_VIDEO_CREDENTIAL_KEY"] = "${IOT_VIDEO_CREDENTIAL_KEY}"
		env["IOT_GB28181_MEDIA_IP"] = v
	}
	switch role {
	case "api":
		env["IOT_ACCESS_GATEWAY_URL"] = inv.GatewayURL()
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
			spilo := fmt.Sprintf("bootstrap:\n  dcs:\n    synchronous_mode: %s\n    postgresql:\n      parameters:\n        max_connections: %d\n        wal_level: replica\n", sync, inv.Postgres.MaxConnections)
			add(kind, "postgres", service(inv.Images.Postgres, map[string]any{"environment": map[string]string{"SCOPE": inv.Name + "-pg", "PGVERSION": "17", "POD_IP": ip, "ETCD3_HOSTS": strings.Join(etcdHosts, ","), "PGUSER_SUPERUSER": "postgres", "PGPASSWORD_SUPERUSER": "${POSTGRES_SUPERUSER_PASSWORD}", "PGUSER_STANDBY": "standby", "PGPASSWORD_STANDBY": "${POSTGRES_REPLICATION_PASSWORD}", "SPILO_PROVIDER": "local", "ALLOW_NOSSL": "true", "PGROOT": "/home/postgres/pgdata/pgroot", "SPILO_CONFIGURATION": spilo}, "volumes": []string{"postgres-data:/home/postgres/pgdata"}}), "postgres-data")
			env["POSTGRES_SUPERUSER_PASSWORD"], env["POSTGRES_REPLICATION_PASSWORD"] = r.s.PostgresSuperuserPassword, r.s.PostgresReplicationPassword
		case "redpanda":
			seeds := r.hostList(inv.Redpanda.Nodes, 33145, ",")
			add(kind, "redpanda", service(inv.Images.Redpanda, map[string]any{"command": []string{"redpanda", "start", "--smp", strconv.Itoa(inv.Redpanda.SMP), "--memory", inv.Redpanda.Memory, "--node-id", strconv.Itoa(idx(inv.Redpanda.Nodes)), "--check=false", "--kafka-addr", "0.0.0.0:9092", "--advertise-kafka-addr", ip + ":9092", "--rpc-addr", "0.0.0.0:33145", "--advertise-rpc-addr", ip + ":33145", "--seeds", seeds, "--schema-registry-addr", "0.0.0.0:18081", "--pandaproxy-addr", "0.0.0.0:18082", "--advertise-pandaproxy-addr", ip + ":18082", "--set", "redpanda.default_topic_replications=" + strconv.Itoa(inv.Redpanda.Replication), "--set", "redpanda.auto_create_topics_enabled=false", "--set", "redpanda.empty_seed_starts_cluster=false"}, "volumes": []string{"redpanda-data:/var/lib/redpanda/data"}}), "redpanda-data")
		case "emqx":
			var seeds []string
			for _, n := range inv.EMQX.Nodes {
				seeds = append(seeds, `"emqx@`+r.ip(n)+`"`)
			}
			add(kind, "emqx", service(inv.Images.EMQX, map[string]any{"ulimits": map[string]any{"nofile": map[string]int{"soft": 1048576, "hard": 1048576}}, "entrypoint": []string{"/bin/sh", "-ec"}, "command": []string{emqxEntrypoint}, "environment": map[string]string{"EMQX_NODE__NAME": "emqx@" + ip, "EMQX_NODE__COOKIE": "${EMQX_COOKIE}", "EMQX_CLUSTER__DISCOVERY_STRATEGY": "static", "EMQX_CLUSTER__STATIC__SEEDS": "[" + strings.Join(seeds, ",") + "]", "IOT_EMQX_API_KEY": "${IOT_EMQX_API_KEY}", "IOT_EMQX_API_SECRET": "${IOT_EMQX_API_SECRET}", "IOT_JWT_SECRET": "${IOT_JWT_SECRET}", "EMQX_AUTHORIZATION__NO_MATCH": "deny", "EMQX_AUTHORIZATION__DENY_ACTION": "ignore", "EMQX_MQTT__MAX_MQUEUE_LEN": "100000", "EMQX_MQTT__MAX_INFLIGHT": "128", "EMQX_DASHBOARD__DEFAULT_USERNAME": "admin", "EMQX_DASHBOARD__DEFAULT_PASSWORD": "${EMQX_DASHBOARD_PASSWORD}"}, "volumes": []string{"emqx-data:/opt/emqx/data", "emqx-log:/opt/emqx/log"}}), "emqx-data", "emqx-log")
			env["EMQX_COOKIE"], env["EMQX_DASHBOARD_PASSWORD"] = r.s.EMQXCookie, r.s.EMQXDashboardPassword
			env["IOT_JWT_SECRET"], env["IOT_EMQX_API_KEY"], env["IOT_EMQX_API_SECRET"] = r.s.JWTSecret, r.s.EMQXAPIKey, r.s.EMQXAPISecret
		case "clickhouse":
			files[node+"/clickhouse/config.d/cluster.xml"] = []byte(r.clickhouseConfig(node))
			add(kind, "clickhouse", service(inv.Images.ClickHouse, map[string]any{"ulimits": map[string]any{"nofile": map[string]int{"soft": 262144, "hard": 262144}}, "environment": map[string]string{"CLICKHOUSE_DB": "iot", "CLICKHOUSE_USER": "iot", "CLICKHOUSE_PASSWORD": "${CLICKHOUSE_PASSWORD}", "CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT": "1"}, "volumes": []string{"clickhouse-data:/var/lib/clickhouse", "./clickhouse/config.d/cluster.xml:/etc/clickhouse-server/config.d/cluster.xml:ro"}}), "clickhouse-data")
			env["CLICKHOUSE_PASSWORD"] = r.s.ClickHousePassword
		case "keeper":
			files[node+"/keeper/keeper_config.xml"] = []byte(r.keeperConfig(node))
			add(kind, "keeper", service(inv.Images.Keeper, map[string]any{"volumes": []string{"keeper-data:/var/lib/clickhouse-keeper", "./keeper/keeper_config.xml:/etc/clickhouse-keeper/keeper_config.xml:ro"}}), "keeper-data")
		case "redis":
			cmd := []string{"redis-server", "--appendonly", "yes", "--requirepass", "${REDIS_PASSWORD}", "--masterauth", "${REDIS_PASSWORD}", "--replica-announce-ip", ip}
			if node != inv.Redis.Master {
				cmd = append(cmd, "--replicaof", r.ip(inv.Redis.Master), "6379")
			}
			add(kind, "redis", service(inv.Images.Redis, map[string]any{"command": cmd, "volumes": []string{"redis-data:/data"}}), "redis-data")
			env["REDIS_PASSWORD"] = r.s.RedisPassword
		case "sentinel":
			// Sentinel rewrites its config; it is created once from the
			// environment so the password never sits in a rendered file.
			script := fmt.Sprintf(`if [ ! -f /data/sentinel.conf ]; then printf 'port 26379\nsentinel announce-ip %s\nsentinel monitor iot-redis %s 6379 %d\nsentinel auth-pass iot-redis %%s\nsentinel down-after-milliseconds iot-redis 5000\nsentinel failover-timeout iot-redis 60000\nsentinel parallel-syncs iot-redis 1\n' "$$REDIS_PASSWORD" > /data/sentinel.conf; fi; exec redis-sentinel /data/sentinel.conf`, ip, r.ip(inv.Redis.Master), inv.Redis.Quorum)
			add(kind, "sentinel", service(inv.Images.Redis, map[string]any{"entrypoint": []string{"/bin/sh", "-ec"}, "command": []string{script}, "environment": map[string]string{"REDIS_PASSWORD": "${REDIS_PASSWORD}"}, "volumes": []string{"sentinel-data:/data"}}), "sentinel-data")
			env["REDIS_PASSWORD"] = r.s.RedisPassword
		case "minio":
			add(kind, "minio", service(inv.Images.MinIO, map[string]any{"command": []string{"server", "/data", "--address", ":9002", "--console-address", ":9003"}, "environment": map[string]string{"MINIO_ROOT_USER": "${MINIO_ROOT_USER}", "MINIO_ROOT_PASSWORD": "${MINIO_ROOT_PASSWORD}"}, "volumes": []string{"minio-data:/data"}}), "minio-data")
			env["MINIO_ROOT_USER"], env["MINIO_ROOT_PASSWORD"] = r.s.MinIORootUser, r.s.MinIORootPassword
		case "harness":
			origins := []string{inv.APIURL()}
			for _, n := range inv.Platform.Roles["api"].Nodes {
				origins = append(origins, "http://"+r.ip(n)+":8081")
			}
			add(kind, "harness", service(inv.Images.Harness, map[string]any{"environment": map[string]string{"IOT_HARNESS_HOST": "0.0.0.0", "IOT_HARNESS_PORT": "8091", "IOT_HARNESS_GATEWAY_TOKEN": "${IOT_AI_HARNESS_TOKEN}", "IOT_HARNESS_SESSION_ROOT": "/data/sessions", "IOT_HARNESS_HOME": "/data/runtime-home", "IOT_HARNESS_WORKSPACE": "/data/workspace", "IOT_HARNESS_PLUGIN_DIR": "/data/plugins", "IOT_HARNESS_PLUGIN_SEED_DIR": "/harness/examples/iot-ops-agent/plugins", "IOT_HARNESS_MCP_ALLOWED_ORIGINS": strings.Join(origins, ","), "DEEPSEEK_API_KEY": "${DEEPSEEK_API_KEY}", "DEEPSEEK_BASE_URL": "https://api.deepseek.com"}, "volumes": []string{"harness-data:/data"}}), "harness-data")
			env["IOT_AI_HARNESS_TOKEN"], env["DEEPSEEK_API_KEY"] = r.s.HarnessToken, r.s.DeepSeekAPIKey
		case "ollama":
			add(kind, "ollama", service(inv.Images.Ollama, map[string]any{"environment": map[string]string{"OLLAMA_HOST": "0.0.0.0:11434", "OLLAMA_NUM_PARALLEL": "1"}, "volumes": []string{"ollama-data:/root/.ollama"}}), "ollama-data")
		case "weaviate":
			add(kind, "weaviate", service(inv.Images.Weaviate, map[string]any{"command": []string{"--host", "0.0.0.0", "--port", "8085", "--scheme", "http"}, "environment": map[string]string{"QUERY_DEFAULTS_LIMIT": "25", "AUTHENTICATION_ANONYMOUS_ACCESS_ENABLED": "true", "PERSISTENCE_DATA_PATH": "/var/lib/weaviate", "DEFAULT_VECTORIZER_MODULE": "text2vec-ollama", "ENABLE_MODULES": "text2vec-ollama,backup-filesystem", "OLLAMA_APIENDPOINT": "http://127.0.0.1:11434", "BACKUP_FILESYSTEM_PATH": "/var/lib/weaviate/backups", "CLUSTER_HOSTNAME": node, "GRPC_PORT": "50051"}, "volumes": []string{"weaviate-data:/var/lib/weaviate"}}), "weaviate-data")
		case "video":
			add(kind, "zlmediakit", service(inv.Images.Video, map[string]any{"environment": map[string]string{"IOT_VIDEO_MEDIA_SECRET": "${IOT_VIDEO_MEDIA_SECRET}", "IOT_VIDEO_HOOK_SECRET": "${IOT_VIDEO_HOOK_SECRET}", "IOT_VIDEO_MEDIA_SERVER_ID": inv.Name + "-media-1", "IOT_VIDEO_HOOK_BASE": inv.APIURL() + "/api/v1/video/hooks", "IOT_VIDEO_RTC_PORT": "8000", "IOT_VIDEO_RTC_EXTERN_IP": ip, "IOT_VIDEO_RTP_PORT_MIN": "30000", "IOT_VIDEO_RTP_PORT_MAX": "30063"}, "tmpfs": []string{"/opt/media/hls:size=512m"}}))
			env["IOT_VIDEO_MEDIA_SECRET"], env["IOT_VIDEO_HOOK_SECRET"] = r.s.VideoMediaSecret, r.s.VideoHookSecret
		case "backup":
			add(kind, "backup-service", service(inv.Images.Backup, map[string]any{"environment": map[string]string{"IOT_BACKUP_HTTP_ADDR": ":8090", "IOT_BACKUP_DIR": "/app/data/backups", "IOT_POSTGRES_DSN": r.postgresDSN("read-write"), "IOT_MINIO_ENDPOINT": r.ip(inv.MinIO.Node) + ":9002", "IOT_MINIO_ACCESS_KEY": "${MINIO_ROOT_USER}", "IOT_MINIO_SECRET_KEY": "${MINIO_ROOT_PASSWORD}", "IOT_CLICKHOUSE_URL": r.clickhouseURL(node, 0), "IOT_BACKUP_ENABLED": "true", "IOT_BACKUP_TIME": "00:05", "IOT_BACKUP_TIMEZONE": "Asia/Shanghai", "IOT_BACKUP_ADMIN_TOKEN": "${IOT_BACKUP_ADMIN_TOKEN}", "IOT_BACKUP_RESTORE_TARGET_DSN": "${IOT_BACKUP_RESTORE_TARGET_DSN:-}"}, "volumes": []string{"backup-staging:/app/data/backups"}}), "backup-staging")
			env["POSTGRES_PASSWORD"], env["MINIO_ROOT_USER"], env["MINIO_ROOT_PASSWORD"], env["CLICKHOUSE_PASSWORD"], env["IOT_BACKUP_ADMIN_TOKEN"] = r.s.PostgresPassword, r.s.MinIORootUser, r.s.MinIORootPassword, r.s.ClickHousePassword, r.s.BackupToken
			env["IOT_BACKUP_RESTORE_TARGET_DSN"] = r.s.BackupRestoreTargetDSN
		case "prometheus":
			files[node+"/prometheus/prometheus.yml"] = []byte(r.prometheusConfig())
			add(kind, "prometheus", service(inv.Images.Prometheus, map[string]any{"command": []string{"--config.file=/etc/prometheus/prometheus.yml", "--storage.tsdb.path=/prometheus", "--storage.tsdb.retention.time=30d", "--web.enable-lifecycle"}, "volumes": []string{"prometheus-data:/prometheus", "./prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro"}}), "prometheus-data")
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
			if inv.Video.Node != "" {
				videoUpstream = r.ip(inv.Video.Node) + ":80"
			}
			add(kind, "web", service(inv.Images.Web, map[string]any{"environment": map[string]string{"IOT_API_UPSTREAM": api, "IOT_VIDEO_UPSTREAM": videoUpstream}}))
		default: // platform roles
			salt := idx(inv.Platform.Roles[kind].Nodes)
			name := "iot-" + kind
			def := service(inv.Images.Platform, map[string]any{"environment": r.platformEnv(kind, node, salt), "volumes": []string{name + "-data:/app/data"}})
			add(kind, name, def, name+"-data")
			env["IOT_JWT_SECRET"], env["IOT_ADMIN_PASSWORD"], env["POSTGRES_PASSWORD"], env["REDIS_PASSWORD"], env["CLICKHOUSE_PASSWORD"] = r.s.JWTSecret, r.s.AdminPassword, r.s.PostgresPassword, r.s.RedisPassword, r.s.ClickHousePassword
			env["MINIO_ROOT_USER"], env["MINIO_ROOT_PASSWORD"], env["IOT_EMQX_API_KEY"], env["IOT_EMQX_API_SECRET"] = r.s.MinIORootUser, r.s.MinIORootPassword, r.s.EMQXAPIKey, r.s.EMQXAPISecret
			env["IOT_AI_HARNESS_TOKEN"], env["DEEPSEEK_API_KEY"], env["IOT_BACKUP_ADMIN_TOKEN"] = r.s.HarnessToken, r.s.DeepSeekAPIKey, r.s.BackupToken
			if inv.Video.Node != "" {
				env["IOT_VIDEO_MEDIA_SECRET"], env["IOT_VIDEO_HOOK_SECRET"], env["IOT_VIDEO_CREDENTIAL_KEY"] = r.s.VideoMediaSecret, r.s.VideoHookSecret, r.s.VideoCredentialKey
			}
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
	return b.String()
}

const emqxEntrypoint = `umask 077
jwt_secret_b64="$$(printf '%s' "$$IOT_JWT_SECRET" | base64 | tr -d '\n')"
printf 'authentication = [{mechanism = jwt, from = password, algorithm = "hmac-based", use_jwks = false, secret = "%s", secret_base64_encoded = true, acl_claim_name = "acl", verify_claims = [{name = "username", value = "$${username}"}], disconnect_after_expire = true}]\n' "$$jwt_secret_b64" > /opt/emqx/etc/base.hocon
if [ -n "$$IOT_EMQX_API_KEY" ] && [ -n "$$IOT_EMQX_API_SECRET" ]; then
  printf '%s:%s:administrator\n' "$$IOT_EMQX_API_KEY" "$$IOT_EMQX_API_SECRET" > /opt/emqx/etc/platform-api-keys
  printf '\napi_key.bootstrap_file = "/opt/emqx/etc/platform-api-keys"\n' >> /opt/emqx/etc/base.hocon
fi
sed -i 's/{allow, all}\./{deny, all}./' /opt/emqx/etc/acl.conf
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

// prometheusConfig scrapes every process per instance, labelled with role
// and instance, never through a load balancer.
func (r renderer) prometheusConfig() string {
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
	fmt.Fprintf(&b, "global:\n  scrape_interval: 15s\n  external_labels:\n    cluster: %s\nscrape_configs:\n", inv.Name)
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
	budget, err := inv.Validate()
	if err != nil {
		return nil, err
	}
	if err = s.validate(inv); err != nil {
		return nil, err
	}
	for name, img := range map[string]string{"platform": inv.Images.Platform, "web": inv.Images.Web, "harness": inv.Images.Harness, "redpanda": inv.Images.Redpanda, "emqx": inv.Images.EMQX, "etcd": inv.Images.Etcd, "postgres": inv.Images.Postgres, "redis": inv.Images.Redis, "clickhouse": inv.Images.ClickHouse, "keeper": inv.Images.Keeper, "minio": inv.Images.MinIO, "nodeExporter": inv.Images.NodeExporter} {
		if img == "" {
			return nil, fmt.Errorf("images.%s is required", name)
		}
	}
	r := renderer{inv: inv, s: s}
	files := map[string][]byte{}
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
	files["deploy-plan.txt"] = deployPlan(inv, summary, nodeImages)
	files["images.txt"] = imageList(inv)
	files["init.env"] = envFile(map[string]string{
		"IOT_POSTGRES_DSN":              strings.ReplaceAll(r.postgresDSN("read-write"), "${POSTGRES_PASSWORD}", s.PostgresPassword),
		"IOT_POSTGRES_ADMIN_DSN":        "postgres://postgres:" + s.PostgresSuperuserPassword + "@" + r.hostList(inv.Postgres.Nodes, 5432, ",") + "/postgres?sslmode=disable&target_session_attrs=read-write",
		"IOT_POSTGRES_APP_PASSWORD":     s.PostgresPassword,
		"IOT_KAFKA_BROKERS":             r.hostList(inv.Redpanda.Nodes, 9092, ","),
		"IOT_CLICKHOUSE_URL":            strings.ReplaceAll(r.clickhouseURL("", 0), "${CLICKHOUSE_PASSWORD}", s.ClickHousePassword),
		"IOT_CLICKHOUSE_CLUSTER":        inv.ClickHouse.Cluster,
		"IOT_CLUSTER_KAFKA_PARTITIONS":  strconv.Itoa(inv.Redpanda.Partitions),
		"IOT_CLUSTER_KAFKA_REPLICATION": strconv.Itoa(inv.Redpanda.Replication),
	})
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
