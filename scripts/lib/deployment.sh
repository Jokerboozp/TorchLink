#!/usr/bin/env bash
# Shared helpers. Do not source dotenv: user values are data, never shell code.
deployment_lib_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
source "$deployment_lib_dir/env-comments.sh"
unset deployment_lib_dir

deployment_secret() {
  # od is provided by both GNU coreutils and macOS. No OpenSSL dependency.
  od -An -N32 -tx1 /dev/urandom | tr -d ' \n'
}

ensure_deployment_env() {
  local env_path="$1"
  if [ -f "$env_path" ]; then
    printf '保留已有配置：%s\n' "$env_path"
    return
  fi
  [ ! -e "$env_path" ] || { printf '配置路径不是文件：%s\n' "$env_path" >&2; return 1; }
  local defaults
  defaults="$(cat <<EOF
SERVICE_ADMIN_USER=admin
SERVICE_ADMIN_PASSWORD=admin123
POSTGRES_PASSWORD=admin123
REDIS_PASSWORD=admin123
CLICKHOUSE_PASSWORD=admin123
MINIO_ROOT_USER=admin
MINIO_ROOT_PASSWORD=admin123
MINIO_DR_ROOT_USER=admin
MINIO_DR_ROOT_PASSWORD=admin123
EMQX_DASHBOARD_USER=admin
EMQX_DASHBOARD_PASSWORD=admin123
GRAFANA_ADMIN_USER=admin
GRAFANA_ADMIN_PASSWORD=admin123
IOT_JWT_SECRET=$(deployment_secret)
IOT_ADMIN_USER=admin
IOT_ADMIN_PASSWORD=admin123
IOT_ADMIN_TENANTS=tenant_001
IOT_MQTT_TOOL_USERNAME=admin
IOT_MQTT_TOOL_PASSWORD=admin123
IOT_KAFKA_SASL_USERNAME=admin
IOT_KAFKA_SASL_PASSWORD=admin123
IOT_KAFKA_SASL_MECHANISM=SCRAM-SHA-256
IOT_KAFKA_ADMIN_URL=http://redpanda:9644
IOT_KAFKA_ADMIN_USERNAME=admin
IOT_KAFKA_ADMIN_PASSWORD=admin123
IOT_KAFKA_ADVERTISED_HOST=127.0.0.1
IOT_KAFKA_PUBLIC_BROKERS=127.0.0.1:19092
IOT_AI_HARNESS_TOKEN=$(deployment_secret)
IOT_EMBEDDING_API_KEY=
IOT_BACKUP_ADMIN_TOKEN=$(deployment_secret)
GB26875_CONTROL_TOKEN=$(deployment_secret)
IOT_HTTP_ADDR=:8081
IOT_WEB_PORT=8080
IOT_API_PORT=8081
IOT_CORS_ALLOWED_ORIGINS=http://localhost:8080,http://127.0.0.1:8080,http://localhost:5173,http://127.0.0.1:5173
IOT_AI_PROVIDER=deepseek
IOT_AI_BASE_URL=https://api.deepseek.com
IOT_AI_MODEL=deepseek-flash
IOT_AI_API_KEY=
DEEPSEEK_API_KEY=
DEEPSEEK_BASE_URL=https://api.deepseek.com
IOT_AI_HARNESS_URL=http://deepseek-harness:8091
IOT_AI_HARNESS_MCP_URL=http://platform-api:8080/mcp/harness
IOT_AI_HARNESS_PROVIDER=deepseek-official
IOT_AI_HARNESS_MODEL=deepseek-flash
IOT_BACKUP_TIME=00:05
IOT_BACKUP_ENABLED=true
IOT_BACKUP_TIMEZONE=Asia/Shanghai
EOF
)"
  mkdir -p -- "$(dirname -- "$env_path")"
  # noclobber protects existing files, including concurrent script invocations.
  (umask 077; set -o noclobber; {
    printf '# 此配置只在首次部署时生成，后续运行不会轮换凭据。\n'
    printf '# 文件包含敏感凭据，请勿提交到 Git 或公开分享。\n'
    printf '%s\n' "$defaults"
  } > "$env_path") || return 1
  printf '已生成配置：%s（服务工具账号使用配置默认值，内部令牌随机生成）。\n' "$env_path"
}

get_deployment_env_value() {
  local env_path="$1" key="$2"
  if printenv "$key" >/dev/null 2>&1; then printenv "$key"; return; fi
  awk -v key="$key" '
    { sub(/\r$/, ""); line=$0; sub(/^[[:space:]]*(export[[:space:]]+)?/, "", line) }
    line ~ "^" key "[[:space:]]*=" {
      sub(/^[^=]*=[[:space:]]*/, "", line)
      quote=substr(line,1,1)
      if (quote == "\047" || quote == "\042") {
        line=substr(line,2); end=index(line,quote); if (end) line=substr(line,1,end-1)
      } else { sub(/[[:space:]]+#.*$/, "", line); sub(/[[:space:]]*$/, "", line) }
      value=line
    }
    END { print value }
  ' "$env_path"
}

set_deployment_env_value() {
  local env_path="$1" key="$2" value="$3" updated
  if [[ ! "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || [[ "$value" == *$'\n'* || "$value" == *$'\r'* ]]; then
    echo '环境变量名称或值包含不支持的字符。' >&2; return 1
  fi
  # Keep values literal, including backslashes; never evaluate dotenv content.
  updated="$(DEPLOYMENT_ENV_VALUE="$value" awk -v key="$key" '
    BEGIN { value=ENVIRON["DEPLOYMENT_ENV_VALUE"] }
    { clean=$0; sub(/^[[:space:]]*(export[[:space:]]+)?/, "", clean) }
    clean ~ "^" key "[[:space:]]*=" { if (!found++) print key "=" value; next }
    { print }
    END { if (!found) print key "=" value }
  ' "$env_path")" || return 1
  printf '%s\n' "$updated" > "$env_path"
}

assert_docker_available() {
  command -v docker >/dev/null 2>&1 || { echo '找不到 docker，请先安装 Docker Engine/Desktop 和 Compose v2。' >&2; return 1; }
  docker info >/dev/null 2>&1 || { echo 'Docker Engine 不可用，请先启动 Docker。' >&2; return 1; }
  docker compose version >/dev/null 2>&1 || { echo 'Docker Compose v2 不可用，请先安装或升级。' >&2; return 1; }
}

run_docker() {
  docker "$@" || { local result=$?; printf 'Docker 命令失败，退出码 %s。\n' "$result" >&2; return "$result"; }
}

wait_deployment_http() {
  local url="$1" deadline=$((SECONDS + ${2:-180}))
  command -v curl >/dev/null 2>&1 || { echo '健康检查需要 curl，请先安装。' >&2; return 1; }
  while [ "$SECONDS" -lt "$deadline" ]; do
    if [ "$(curl --silent --output /dev/null --max-time 5 --write-out '%{http_code}' "$url" || true)" = 200 ]; then
      printf '健康检查通过：%s\n' "$url"
      return
    fi
    sleep 2
  done
  printf '健康检查超时：%s。请用相同的 Compose 项目和配置参数检查 ps / logs。\n' "$url" >&2
  return 1
}

has_deployment_env_key() {
  grep -Eq "^[[:space:]]*(export[[:space:]]+)?$2[[:space:]]*=" "$1" 2>/dev/null
}

# Knowledge vectors and reranking use the embedding / reranker services
# deployed with the platform. Earlier releases defaulted to the DashScope
# cloud API; that default is replaced, an operator-chosen API is kept.
# Arguments: env file, embedding URL, rerank URL, extra local hosts (optional).
configure_embedding_env() {
  local env_path="$1" embedding_url="${2:-http://embedding:8080/v1}" rerank_url="${3:-http://reranker:8080}" extra_hosts="${4:-}" url model hosts
  url="$(get_deployment_env_value "$env_path" IOT_EMBEDDING_URL)"
  model="$(get_deployment_env_value "$env_path" IOT_EMBEDDING_MODEL)"
  case "$url" in
    ''|https://dashscope.aliyuncs.com/compatible-mode/v1|http://embedding:8080/v1|http://*:18093/v1)
      [ "$url" != https://dashscope.aliyuncs.com/compatible-mode/v1 ] || echo '提示：知识库向量计算改为随平台部署的 embedding 服务（bge-m3），已有文档会在后台自动重建索引。若“模型管理”里保存过云端向量配置，请在该页切换为本地服务。' >&2
      set_deployment_env_value "$env_path" IOT_EMBEDDING_URL "$embedding_url"
      case "$model" in ''|text-embedding-v4) set_deployment_env_value "$env_path" IOT_EMBEDDING_MODEL bge-m3 ;; esac ;;
  esac
  [ -n "$(get_deployment_env_value "$env_path" IOT_EMBEDDING_DIMENSIONS)" ] || set_deployment_env_value "$env_path" IOT_EMBEDDING_DIMENSIONS 1024
  [ -n "$(get_deployment_env_value "$env_path" IOT_EMBEDDING_BATCH_SIZE)" ] || set_deployment_env_value "$env_path" IOT_EMBEDDING_BATCH_SIZE 10
  has_deployment_env_key "$env_path" IOT_EMBEDDING_API_KEY || set_deployment_env_value "$env_path" IOT_EMBEDDING_API_KEY ''
  # An explicitly empty IOT_RERANK_URL turns reranking off and is kept.
  if ! has_deployment_env_key "$env_path" IOT_RERANK_URL || [[ "$(get_deployment_env_value "$env_path" IOT_RERANK_URL)" == http://*:18094 ]]; then
    set_deployment_env_value "$env_path" IOT_RERANK_URL "$rerank_url"
  fi
  hosts="embedding,reranker${extra_hosts:+,$extra_hosts}"
  set_deployment_env_value "$env_path" IOT_LOCAL_AI_HOSTS "$hosts"
}

# Deployment inference defaults to the DeepSeek cloud API; another
# OpenAI-compatible external API is selected later in 模型管理.
configure_deepseek_env() {
  local env_path="$1" model="${2:-deepseek-flash}" old_provider key
  old_provider="$(get_deployment_env_value "$env_path" IOT_AI_PROVIDER)"
  key="$(get_deployment_env_value "$env_path" DEEPSEEK_API_KEY)"
  if [ -z "$key" ] && [ "$old_provider" = deepseek ]; then
    key="$(get_deployment_env_value "$env_path" IOT_AI_API_KEY)"
  fi
  if [[ "$key" == *"'"* || "$key" == *$'\n'* || "$key" == *$'\r'* ]]; then
    echo 'DeepSeek API Key 含不支持的引号或换行。' >&2; return 1
  fi
  set_deployment_env_value "$env_path" DEEPSEEK_API_KEY "'$key'"
  set_deployment_env_value "$env_path" IOT_AI_API_KEY ''
  set_deployment_env_value "$env_path" DEEPSEEK_BASE_URL https://api.deepseek.com
  set_deployment_env_value "$env_path" IOT_AI_PROVIDER deepseek
  set_deployment_env_value "$env_path" IOT_AI_BASE_URL https://api.deepseek.com
  set_deployment_env_value "$env_path" IOT_AI_MODEL "$model"
  set_deployment_env_value "$env_path" IOT_AI_HARNESS_PROVIDER deepseek-official
  set_deployment_env_value "$env_path" IOT_AI_HARNESS_MODEL "$model"
  if [ -z "$key" ]; then
    echo '提示：请填写 DEEPSEEK_API_KEY，或启动后在“模型管理”填写密钥并保存（连接测试可选）；未配置前 AI 功能不可用。' >&2
  fi
}

# Add the platform's own management credentials once; never rotate an existing key.
# Kafka's external listener binds to 127.0.0.1 by default. An existing
# environment that already handed consumers a non-local address keeps it open.
ensure_kafka_bind_address() {
  local env_path="$1" broker
  [ -z "$(get_deployment_env_value "$env_path" KAFKA_BIND_ADDRESS)" ] || return 0
  for broker in $(get_deployment_env_value "$env_path" IOT_KAFKA_PUBLIC_BROKERS | tr ',' ' '); do
    case "$broker" in
      127.0.0.1:*|localhost:*|'[::1]':*) ;;
      *) set_deployment_env_value "$env_path" KAFKA_BIND_ADDRESS 0.0.0.0
         printf 'Kafka 对外地址为 %s，保留外部监听（KAFKA_BIND_ADDRESS=0.0.0.0）。\n' "$broker"
         return 0 ;;
    esac
  done
}

ensure_emqx_admin_env() {
  local env_path="$1" default_url="$2" api_key api_secret
  api_key="$(get_deployment_env_value "$env_path" IOT_EMQX_API_KEY)"
  api_secret="$(get_deployment_env_value "$env_path" IOT_EMQX_API_SECRET)"
  if [ -z "$api_key" ] && [ -z "$api_secret" ]; then
    set_deployment_env_value "$env_path" IOT_EMQX_API_KEY "$(deployment_secret)"
    set_deployment_env_value "$env_path" IOT_EMQX_API_SECRET "$(deployment_secret)"
  elif [ -z "$api_key" ] || [ -z "$api_secret" ]; then
    echo 'EMQX 管理凭据不完整，请同时配置 IOT_EMQX_API_KEY 和 IOT_EMQX_API_SECRET。' >&2
    return 1
  fi
  [ -n "$(get_deployment_env_value "$env_path" IOT_EMQX_API_URL)" ] || set_deployment_env_value "$env_path" IOT_EMQX_API_URL "$default_url"
}

# apply_ops_module turns the monitoring stack (compose profile "ops":
# Prometheus, Loki, Alloy, Grafana, Alertmanager, node-exporter) on or off.
# Off clears the API's component URLs, so the operations center reports the
# components as not deployed instead of unreachable.
apply_ops_module() {
  local env_file="$1" state="$2" profiles key
  profiles="$(get_deployment_env_value "$env_file" COMPOSE_PROFILES | tr ',' '\n' | tr -d ' ' | grep -vx ops | grep -v '^$' || true)"
  if [ "$state" = on ]; then
    profiles="$(printf '%s\nops\n' "$profiles" | grep -v '^$')"
    set_deployment_env_value "$env_file" IOT_OPS_MODULE on
    for key in IOT_OPS_PROMETHEUS_URL:http://prometheus:9090 IOT_OPS_LOKI_URL:http://loki:3100 IOT_OPS_GRAFANA_URL:http://grafana:3000 IOT_OPS_ALERTMANAGER_URL:http://alertmanager:9093; do
      [ -n "$(get_deployment_env_value "$env_file" "${key%%:*}")" ] || set_deployment_env_value "$env_file" "${key%%:*}" "${key#*:}"
    done
  else
    set_deployment_env_value "$env_file" IOT_OPS_MODULE off
    for key in IOT_OPS_PROMETHEUS_URL IOT_OPS_LOKI_URL IOT_OPS_GRAFANA_URL IOT_OPS_ALERTMANAGER_URL; do
      set_deployment_env_value "$env_file" "$key" ''
    done
  fi
  set_deployment_env_value "$env_file" COMPOSE_PROFILES "$(printf '%s' "$profiles" | paste -sd, -)"
}

unset_deployment_env_value() {
  local env_path="$1" key="$2" updated
  updated="$(awk -v key="$key" '{ clean=$0; sub(/^[[:space:]]*(export[[:space:]]+)?/, "", clean) } clean ~ "^" key "[[:space:]]*=" { next } { print }' "$env_path")" || return 1
  printf '%s\n' "$updated" > "$env_path"
}

# apply_clickhouse_module turns ClickHouse (compose profile "clickhouse") on
# or off. Off clears IOT_CLICKHOUSE_URL, so the platform keeps raw messages
# and telemetry in PostgreSQL; on drops an empty URL so Compose supplies the
# bundled server's address again (a custom URL is kept).
apply_clickhouse_module() {
  local env_file="$1" state="$2" profiles previous
  previous="$(get_deployment_env_value "$env_file" IOT_CLICKHOUSE_MODULE)"
  profiles="$(get_deployment_env_value "$env_file" COMPOSE_PROFILES | tr ',' '\n' | tr -d ' ' | grep -vx clickhouse | grep -v '^$' || true)"
  if [ "$state" = on ]; then
    profiles="$(printf '%s\nclickhouse\n' "$profiles" | grep -v '^$')"
    set_deployment_env_value "$env_file" IOT_CLICKHOUSE_MODULE on
    [ -n "$(get_deployment_env_value "$env_file" IOT_CLICKHOUSE_URL)" ] || unset_deployment_env_value "$env_file" IOT_CLICKHOUSE_URL
  else
    set_deployment_env_value "$env_file" IOT_CLICKHOUSE_MODULE off
    set_deployment_env_value "$env_file" IOT_CLICKHOUSE_URL ''
    if [ "$previous" != off ]; then
      echo '提示：关闭 ClickHouse 后，已存入 ClickHouse 的高频原文、遥测历史与属性上报的属性不再可读（数据卷保留，重新开启后恢复）；新数据写入 PostgreSQL。' >&2
    fi
  fi
  set_deployment_env_value "$env_file" COMPOSE_PROFILES "$(printf '%s' "$profiles" | paste -sd, -)"
}
