#!/usr/bin/env bash
# Start local dependencies; Go and Vite run on the host.
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
project_root="$(dirname -- "$script_dir")"
source "$script_dir/lib/deployment.sh"
env_file="$project_root/.env.local"
skip_code_deps=false
include_ai=false
include_deepseek=false
include_harness=true
include_backup=false
include_ops=false
deepseek_model=deepseek-flash
dependency_host=127.0.0.1
dependency_host_set=false
api_host=host.docker.internal
while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file) [ "$#" -ge 2 ] || { echo '--env-file 需要路径。' >&2; exit 1; }; env_file="$2"; shift 2 ;;
    --skip-code-deps) skip_code_deps=true; shift ;;
    --include-ai) include_ai=true; shift ;;
    --include-deepseek) include_deepseek=true; shift ;;
    --include-harness) include_harness=true; shift ;;
    --no-harness) echo 'AI 工作流服务（Harness）是必装组件，不能使用 --no-harness。' >&2; exit 1 ;;
    --include-backup|--include-backup-service) include_backup=true; shift ;;
    --include-ops) include_ops=true; shift ;;
    --dependency-host) [ "$#" -ge 2 ] || { echo '--dependency-host 需要源码机可访问的主机名或 IPv4 地址。' >&2; exit 1; }; dependency_host="$2"; dependency_host_set=true; shift 2 ;;
    --api-host) [ "$#" -ge 2 ] || { echo '--api-host 需要依赖容器可访问的源码机主机名或 IPv4 地址。' >&2; exit 1; }; api_host="$2"; shift 2 ;;
    --ollama-model) echo '已取消部署本地对话模型，请填写 DEEPSEEK_API_KEY。' >&2; exit 1 ;;
    --deepseek-model) [ "$#" -ge 2 ] || { echo '--deepseek-model 需要模型名。' >&2; exit 1; }; deepseek_model="$2"; shift 2 ;;
    -h|--help) echo 'Usage: bash scripts/setup-local.sh [--env-file PATH] [--skip-code-deps] [--dependency-host HOST] [--api-host HOST] [--include-ai|--include-deepseek] [--deepseek-model MODEL] [--include-harness] [--include-backup] [--include-ops]'; exit 0 ;;
    *) printf '未知参数：%s\n' "$1" >&2; exit 1 ;;
  esac
done
case "$env_file" in /*|[A-Za-z]:/*) ;; *) env_file="$project_root/$env_file" ;; esac
mkdir -p -- "$(dirname -- "$env_file")"
env_file="$(cd -- "$(dirname -- "$env_file")" && pwd)/$(basename -- "$env_file")"
[ "$env_file" != "$project_root/.env" ] || { echo '本地环境请使用 .env.local，不能覆盖在线部署的 .env。' >&2; exit 1; }

set_local_env_value() {
  local key="$1" value="$2" replace="${3:-false}" updated
  if [[ "$value" == *"'"* || "$value" == *$'\n'* || "$value" == *$'\r'* ]]; then
    printf '配置 %s 含不支持的引号或换行，未写入。\n' "$key" >&2; return 1
  fi
  if [ "$replace" != true ] && awk -v key="$key" '
      { line=$0; sub(/^[[:space:]]*(export[[:space:]]+)?/, "", line) }
      line ~ "^" key "[[:space:]]*=" { found=1 }
      END { exit !found }
    ' "$env_file"; then return; fi
  # Single quotes keep dotenv values literal, including $ in user passwords.
  updated="$(LOCAL_ENV_LINE="$key='$value'" awk -v key="$key" '
    { line=$0; sub(/^[[:space:]]*(export[[:space:]]+)?/, "", line) }
    line ~ "^" key "[[:space:]]*=" { if (!found++) print ENVIRON["LOCAL_ENV_LINE"]; next }
    { print }
    END { if (!found) print ENVIRON["LOCAL_ENV_LINE"] }
  ' "$env_file")"
  printf '%s\n' "$updated" > "$env_file"
}

urlencode() {
  local LC_ALL=C input="$1" char i
  for ((i=0; i<${#input}; i++)); do
    char="${input:i:1}"
    case "$char" in [a-zA-Z0-9.~_-]) printf '%s' "$char" ;; *) printf '%%%02X' "'$char" ;; esac
  done
}

source "$script_dir/lib/docker-bootstrap.sh"
ensure_deployment_docker online
assert_docker_available
[[ "$deepseek_model" =~ ^[A-Za-z0-9][A-Za-z0-9._:/-]*$ ]] || { echo 'DeepSeek 模型名称无效。' >&2; exit 1; }
[[ "$dependency_host" =~ ^[A-Za-z0-9][A-Za-z0-9.-]*$ ]] || { echo '依赖主机必须是主机名或 IPv4 地址。' >&2; exit 1; }
[[ "$api_host" =~ ^[A-Za-z0-9][A-Za-z0-9.-]*$ ]] || { echo '源码机必须是主机名或 IPv4 地址。' >&2; exit 1; }
command -v curl >/dev/null 2>&1 || { echo '健康检查需要 curl，请先安装。' >&2; exit 1; }
if [ "$skip_code_deps" = false ]; then
  for command_name in go npm; do
    command -v "$command_name" >/dev/null 2>&1 || { printf '请先安装 %s，或使用 --skip-code-deps 跳过代码依赖安装。\n' "$command_name" >&2; exit 1; }
  done
fi
new_env=false
[ -f "$env_file" ] || new_env=true
ensure_deployment_env "$env_file"
postgres_password="$(urlencode "$(get_deployment_env_value "$env_file" POSTGRES_PASSWORD)")"
clickhouse_password="$(urlencode "$(get_deployment_env_value "$env_file" CLICKHOUSE_PASSWORD)")"
bind_address=127.0.0.1
if [ "$dependency_host" != 127.0.0.1 ] && [ "$dependency_host" != localhost ]; then bind_address=0.0.0.0; fi
defaults=(
  "IOT_LOCAL_BIND_ADDRESS=$bind_address"
  "IOT_LOCAL_ADVERTISED_HOST=$dependency_host"
  'IOT_HTTP_ADDR=:8081'
  'IOT_DEV_MODE=false'
  'IOT_DATA_DIR=./data'
  "IOT_POSTGRES_DSN=postgres://iot:${postgres_password}@${dependency_host}:15432/iot?sslmode=disable"
  "IOT_REDIS_ADDR=${dependency_host}:16379"
  "IOT_REDIS_PASSWORD=$(get_deployment_env_value "$env_file" REDIS_PASSWORD)"
  "IOT_CLICKHOUSE_URL=http://iot:${clickhouse_password}@${dependency_host}:18123?database=iot"
  "IOT_MINIO_ENDPOINT=${dependency_host}:19000"
  "IOT_MINIO_ACCESS_KEY=$(get_deployment_env_value "$env_file" MINIO_ROOT_USER)"
  "IOT_MINIO_SECRET_KEY=$(get_deployment_env_value "$env_file" MINIO_ROOT_PASSWORD)"
  "IOT_KAFKA_BROKERS=${dependency_host}:19092"
  "IOT_MQTT_BROKER=tcp://${dependency_host}:1883"
  "IOT_MQTT_WEBSOCKET_PUBLIC_URL=ws://${dependency_host}:8083/mqtt"
  "IOT_OLLAMA_URL=http://${dependency_host}:11434"
  "IOT_AI_OLLAMA_URL=http://${dependency_host}:11434"
  'IOT_AI_PROVIDER=deepseek'
  'IOT_AI_BASE_URL=https://api.deepseek.com'
  "IOT_AI_MODEL=$deepseek_model"
  "IOT_WEAVIATE_URL=http://${dependency_host}:18080"
  'IOT_BACKUP_URL=http://127.0.0.1:8092'
  'IOT_BACKUP_HTTP_ADDR=:8092'
  'IOT_AI_HARNESS_ENABLED=true'
  "IOT_AI_HARNESS_URL=http://${dependency_host}:8091"
  "IOT_AI_HARNESS_MCP_URL=http://${api_host}:8081/mcp/harness"
  'IOT_AI_HARNESS_PROVIDER=deepseek-official'
  "IOT_AI_HARNESS_MODEL=$deepseek_model"
  "IOT_HARNESS_MCP_ALLOWED_ORIGINS=http://${api_host}:8081"
)
for entry in "${defaults[@]}"; do
  key="${entry%%=*}"
  replace="$new_env"
  if [ "$dependency_host_set" = true ]; then
    case "$key" in IOT_LOCAL_*|IOT_POSTGRES_DSN|IOT_REDIS_ADDR|IOT_CLICKHOUSE_URL|IOT_MINIO_ENDPOINT|IOT_KAFKA_BROKERS|IOT_MQTT_BROKER|IOT_MQTT_WEBSOCKET_PUBLIC_URL|IOT_OLLAMA_URL|IOT_AI_OLLAMA_URL|IOT_WEAVIATE_URL|IOT_BACKUP_URL|IOT_AI_HARNESS_MCP_URL|IOT_HARNESS_MCP_ALLOWED_ORIGINS) replace=true;; esac
  fi
  set_local_env_value "$key" "${entry#*=}" "$replace"
done
# The source-debugged API and backup worker run on the same host. Keep the
# worker endpoint local even when middleware containers are remote.
set_local_env_value IOT_BACKUP_URL 'http://127.0.0.1:8092' true
set_local_env_value IOT_BACKUP_HTTP_ADDR ':8092'
if [ "$include_backup" = true ]; then set_local_env_value IOT_BACKUP_URL "http://${dependency_host}:8092" true; fi
configure_deepseek_env "$env_file" "$deepseek_model"

# AI 工作流服务（Harness）为必装组件：告警研判、巡检、报告、协议助手和规则草稿都通过它运行。
if [ "$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_ENABLED)" = false ]; then echo '提示：Harness 已改为必装组件，已将 IOT_AI_HARNESS_ENABLED 改为 true。' >&2; fi
set_local_env_value IOT_AI_HARNESS_ENABLED true true
if [ "$include_harness" = true ]; then
  ensure_deployment_git
  bash "$script_dir/fetch-deepseek-harness.sh"
  harness_url="$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_URL)"
  if [ -z "$harness_url" ] || [ "$dependency_host_set" = true ]; then set_local_env_value IOT_AI_HARNESS_URL "http://${dependency_host}:8091" true; fi
  set_local_env_value IOT_AI_HARNESS_MCP_URL "http://${api_host}:8081/mcp/harness" true
  set_local_env_value IOT_HARNESS_MCP_ALLOWED_ORIGINS "http://${api_host}:8081" true
fi

# Ops center dependencies (Prometheus, Loki, Grafana, Alertmanager, log and
# host collectors) are optional. Rule and notification files are shared with
# the containers through bind mounts, which only works when they run here.
if [ "$include_ops" = true ]; then
  set_local_env_value IOT_OPS_PROMETHEUS_URL "http://${dependency_host}:19090" true
  set_local_env_value IOT_OPS_LOKI_URL "http://${dependency_host}:13100" true
  set_local_env_value IOT_OPS_GRAFANA_URL "http://${dependency_host}:13000" true
  set_local_env_value IOT_OPS_ALERTMANAGER_URL "http://${dependency_host}:19093" true
  set_local_env_value IOT_OPS_GRAFANA_USER "$(get_deployment_env_value "$env_file" GRAFANA_ADMIN_USER)" true
  set_local_env_value IOT_OPS_GRAFANA_PASSWORD "$(get_deployment_env_value "$env_file" GRAFANA_ADMIN_PASSWORD)" true
  set_local_env_value IOT_LOG_LOKI_URL "http://${dependency_host}:13100" true
  set_local_env_value IOT_LOCAL_API_HOST "$api_host" true
  # Containers read the shared files as their own users; local files are not secret-grade storage.
  set_local_env_value IOT_OPS_CONFIG_FILE_MODE 0644 true
  # The containers need these files even when the API runs on another machine.
  ops_dir="$project_root/data/ops"
  mkdir -p "$ops_dir/prometheus-rules" "$ops_dir/loki/rules/fake" "$ops_dir/alertmanager"
  [ -f "$ops_dir/loki/runtime.yaml" ] || printf 'overrides: {}\n' > "$ops_dir/loki/runtime.yaml"
  [ -f "$ops_dir/alertmanager/alertmanager.yml" ] || printf '%s\n' 'route:' '  receiver: platform-null' '  group_by: [alertname, severity]' 'receivers:' '  - name: platform-null' > "$ops_dir/alertmanager/alertmanager.yml"
  chmod 0755 "$ops_dir" "$ops_dir/prometheus-rules" "$ops_dir/loki" "$ops_dir/loki/rules" "$ops_dir/loki/rules/fake" "$ops_dir/alertmanager"
  chmod 0644 "$ops_dir/loki/runtime.yaml" "$ops_dir/alertmanager/alertmanager.yml"
  if [ "$dependency_host" = 127.0.0.1 ] || [ "$dependency_host" = localhost ]; then
    set_local_env_value IOT_OPS_PROMETHEUS_RULES_DIR ./data/ops/prometheus-rules true
    set_local_env_value IOT_OPS_LOKI_RULES_DIR ./data/ops/loki/rules/fake true
    set_local_env_value IOT_OPS_LOKI_RUNTIME_FILE ./data/ops/loki/runtime.yaml true
    set_local_env_value IOT_OPS_ALERTMANAGER_CONFIG_FILE ./data/ops/alertmanager/alertmanager.yml true
  else
    for key in IOT_OPS_PROMETHEUS_RULES_DIR IOT_OPS_LOKI_RULES_DIR IOT_OPS_LOKI_RUNTIME_FILE IOT_OPS_ALERTMANAGER_CONFIG_FILE; do set_local_env_value "$key" '' true; done
    echo '提示：依赖运行在远程主机时，规则、保留策略和通知渠道在运维中心只能查看；需要编辑时请在依赖主机运行 API。' >&2
  fi
fi

annotate_deployment_env_file "$env_file"

cd -- "$project_root"
if [ "$skip_code_deps" = false ]; then
  echo '准备 Go 依赖……'
  go mod download
  echo '准备前端依赖……'
  (cd iot_front && npm ci)
fi
compose=(compose --project-name iot-platform-local --env-file "$env_file" -f compose.local.yaml)
if [ "$include_backup" = true ]; then compose+=(--profile backup); fi
if [ "$include_ops" = true ]; then compose+=(--profile ops); fi
if [ "$include_backup" = false ]; then
  # Stop an older worker; preserve the container and all backup data.
  backup_compose=(compose --project-name iot-platform-local --env-file "$env_file" -f compose.local.yaml --profile backup)
  run_docker "${backup_compose[@]}" stop backup-service
fi
run_docker "${compose[@]}" config --quiet
run_docker "${compose[@]}" up -d --build --wait --wait-timeout 300
wait_deployment_http http://127.0.0.1:11434/api/tags 180
run_docker "${compose[@]}" exec -T ollama ollama pull nomic-embed-text
if [ "$include_backup" = true ]; then wait_deployment_http http://127.0.0.1:8092/health/ready 180; fi
if [ "$include_harness" = true ]; then wait_deployment_http http://127.0.0.1:8091/health 180; fi
if [ "$include_ops" = true ]; then
  wait_deployment_http http://127.0.0.1:19090/-/ready 180
  wait_deployment_http http://127.0.0.1:13000/api/health 180
fi
printf '本地依赖已就绪。配置和管理员账号保存在：%s（凭据不输出）。\n' "$env_file"
printf '在 platform 目录启动后端：go run ./cmd/iot-platform --env-file %q\n' "$env_file"
echo '在 platform/iot_front 目录启动前端：npm run dev'
echo '备份服务默认不启动容器；在 VS Code 选择“IoT Platform (API + Web + Backup)”进行源码调试。'
if [ "$include_backup" = true ]; then echo '已按 --include-backup 启动备份容器；停止后可改用 VS Code 源码调试。'; fi
echo '前端：http://localhost:5173；后端：http://localhost:8081'
if [ "$include_ops" = true ]; then echo '运维中心依赖已启动：内置管理员可在“运维中心”菜单使用；其他账号需把所在租户加入 IOT_OPS_TENANTS。'; fi
if [ "$bind_address" = 0.0.0.0 ]; then
  printf '依赖已开放给远程源码环境：%s。请把 %s 复制到源码机器后使用。\n' "$dependency_host" "$env_file"
fi
