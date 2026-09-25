#!/usr/bin/env bash
# Start local dependencies; Go and Vite run on the host.
# 执行当前脚本步骤。
set -euo pipefail

# 执行当前脚本步骤。
script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
# 执行当前脚本步骤。
project_root="$(dirname -- "$script_dir")"
# 执行当前脚本步骤。
source "$script_dir/lib/deployment.sh"
# 执行当前脚本步骤。
env_file="$project_root/.env.local"
# 执行当前脚本步骤。
skip_code_deps=false
# 执行当前脚本步骤。
include_ai=false
# 执行当前脚本步骤。
include_deepseek=false
# 执行当前脚本步骤。
include_harness=auto
# 执行当前脚本步骤。
include_backup=false
include_ops=false
# 执行当前脚本步骤。
ollama_model=qwen3:1.7b
# 执行当前脚本步骤。
deepseek_model=deepseek-v4-flash
# 执行当前脚本步骤。
dependency_host=127.0.0.1
# 执行当前脚本步骤。
dependency_host_set=false
# 执行当前脚本步骤。
api_host=host.docker.internal
# 遍历数据并执行循环体。
while [ "$#" -gt 0 ]; do
  # 执行当前脚本步骤。
  case "$1" in
    # 执行当前脚本步骤。
    --env-file) [ "$#" -ge 2 ] || { echo '--env-file 需要路径。' >&2; exit 1; }; env_file="$2"; shift 2 ;;
    # 执行当前脚本步骤。
    --skip-code-deps) skip_code_deps=true; shift ;;
    # 执行当前脚本步骤。
    --include-ai) include_ai=true; shift ;;
    # 执行当前脚本步骤。
    --include-deepseek) include_deepseek=true; shift ;;
    # 执行当前脚本步骤。
    --include-harness) include_harness=true; shift ;;
    # 执行当前脚本步骤。
    --no-harness) include_harness=false; shift ;;
    # 执行当前脚本步骤。
    --include-backup|--include-backup-service) include_backup=true; shift ;;
    --include-ops) include_ops=true; shift ;;
    # 执行当前脚本步骤。
    --dependency-host) [ "$#" -ge 2 ] || { echo '--dependency-host 需要源码机可访问的主机名或 IPv4 地址。' >&2; exit 1; }; dependency_host="$2"; dependency_host_set=true; shift 2 ;;
    # 执行当前脚本步骤。
    --api-host) [ "$#" -ge 2 ] || { echo '--api-host 需要依赖容器可访问的源码机主机名或 IPv4 地址。' >&2; exit 1; }; api_host="$2"; shift 2 ;;
    # 执行当前脚本步骤。
    --ollama-model) [ "$#" -ge 2 ] || { echo '--ollama-model 需要模型名。' >&2; exit 1; }; ollama_model="$2"; shift 2 ;;
    # 执行当前脚本步骤。
    --deepseek-model) [ "$#" -ge 2 ] || { echo '--deepseek-model 需要模型名。' >&2; exit 1; }; deepseek_model="$2"; shift 2 ;;
    # 执行当前脚本步骤。
    -h|--help) echo 'Usage: bash scripts/setup-local.sh [--env-file PATH] [--skip-code-deps] [--dependency-host HOST] [--api-host HOST] [--include-ai|--include-deepseek] [--ollama-model MODEL] [--deepseek-model MODEL] [--include-harness|--no-harness] [--include-backup] [--include-ops]'; exit 0 ;;
    # 执行当前脚本步骤。
    *) printf '未知参数：%s\n' "$1" >&2; exit 1 ;;
  # 执行当前脚本步骤。
  esac
# 结束当前控制块。
done
# 执行当前脚本步骤。
case "$env_file" in /*|[A-Za-z]:/*) ;; *) env_file="$project_root/$env_file" ;; esac
# 执行当前脚本步骤。
mkdir -p -- "$(dirname -- "$env_file")"
# 执行当前脚本步骤。
env_file="$(cd -- "$(dirname -- "$env_file")" && pwd)/$(basename -- "$env_file")"
# 执行当前脚本步骤。
[ "$env_file" != "$project_root/.env" ] || { echo '本地环境请使用 .env.local，不能覆盖在线部署的 .env。' >&2; exit 1; }

# 执行当前脚本步骤。
set_local_env_value() {
  # 执行当前脚本步骤。
  local key="$1" value="$2" replace="${3:-false}" updated
  # 判断条件后执行对应操作。
  if [[ "$value" == *"'"* || "$value" == *$'\n'* || "$value" == *$'\r'* ]]; then
    printf '配置 %s 含不支持的引号或换行，未写入。\n' "$key" >&2; return 1
  fi
  if [ "$replace" != true ] && awk -v key="$key" '
      # 执行当前脚本步骤。
      { line=$0; sub(/^[[:space:]]*(export[[:space:]]+)?/, "", line) }
      # 执行当前脚本步骤。
      line ~ "^" key "[[:space:]]*=" { found=1 }
      # 执行当前脚本步骤。
      END { exit !found }
    # 执行当前脚本步骤。
    ' "$env_file"; then return; fi
  # Single quotes keep dotenv values literal, including $ in user passwords.
  updated="$(LOCAL_ENV_LINE="$key='$value'" awk -v key="$key" '
    # 执行当前脚本步骤。
    { line=$0; sub(/^[[:space:]]*(export[[:space:]]+)?/, "", line) }
    # 执行当前脚本步骤。
    line ~ "^" key "[[:space:]]*=" { if (!found++) print ENVIRON["LOCAL_ENV_LINE"]; next }
    # 执行当前脚本步骤。
    { print }
    # 执行当前脚本步骤。
    END { if (!found) print ENVIRON["LOCAL_ENV_LINE"] }
  # 执行当前脚本步骤。
  ' "$env_file")"
  printf '%s\n' "$updated" > "$env_file"
}

urlencode() {
  local LC_ALL=C input="$1" char i
  for ((i=0; i<${#input}; i++)); do
    char="${input:i:1}"
    case "$char" in [a-zA-Z0-9.~_-]) printf '%s' "$char" ;; *) printf '%%%02X' "'$char" ;; esac
  # 结束当前控制块。
  done
# 结束当前控制块。
}

# 执行当前脚本步骤。
source "$script_dir/lib/docker-bootstrap.sh"
# 执行当前脚本步骤。
ensure_deployment_docker online
# 执行当前脚本步骤。
assert_docker_available
# 执行当前脚本步骤。
[[ "$ollama_model" =~ ^[A-Za-z0-9][A-Za-z0-9._:/-]*$ ]] || { echo 'Ollama 模型名称无效。' >&2; exit 1; }
# 执行当前脚本步骤。
[[ "$deepseek_model" =~ ^[A-Za-z0-9][A-Za-z0-9._:/-]*$ ]] || { echo 'DeepSeek 模型名称无效。' >&2; exit 1; }
# 执行当前脚本步骤。
[[ "$dependency_host" =~ ^[A-Za-z0-9][A-Za-z0-9.-]*$ ]] || { echo '依赖主机必须是主机名或 IPv4 地址。' >&2; exit 1; }
# 执行当前脚本步骤。
[[ "$api_host" =~ ^[A-Za-z0-9][A-Za-z0-9.-]*$ ]] || { echo '源码机必须是主机名或 IPv4 地址。' >&2; exit 1; }
# 执行当前脚本步骤。
[ "$include_ai" = false ] || [ "$include_deepseek" = false ] || { echo '--include-ai 与 --include-deepseek 只能二选一。' >&2; exit 1; }
# 执行当前脚本步骤。
command -v curl >/dev/null 2>&1 || { echo '健康检查需要 curl，请先安装。' >&2; exit 1; }
# 判断条件后执行对应操作。
if [ "$skip_code_deps" = false ]; then
  # 遍历数据并执行循环体。
  for command_name in go npm; do
    # 执行当前脚本步骤。
    command -v "$command_name" >/dev/null 2>&1 || { printf '请先安装 %s，或使用 --skip-code-deps 跳过代码依赖安装。\n' "$command_name" >&2; exit 1; }
  # 结束当前控制块。
  done
# 结束当前控制块。
fi
# 执行当前脚本步骤。
new_env=false
# 执行当前脚本步骤。
[ -f "$env_file" ] || new_env=true
# 执行当前脚本步骤。
ensure_deployment_env "$env_file"
# 执行当前脚本步骤。
postgres_password="$(urlencode "$(get_deployment_env_value "$env_file" POSTGRES_PASSWORD)")"
# 执行当前脚本步骤。
clickhouse_password="$(urlencode "$(get_deployment_env_value "$env_file" CLICKHOUSE_PASSWORD)")"
# 执行当前脚本步骤。
bind_address=127.0.0.1
# 判断条件后执行对应操作。
if [ "$dependency_host" != 127.0.0.1 ] && [ "$dependency_host" != localhost ]; then bind_address=0.0.0.0; fi
# 执行当前脚本步骤。
defaults=(
  # 执行当前脚本步骤。
  "IOT_LOCAL_BIND_ADDRESS=$bind_address"
  # 执行当前脚本步骤。
  "IOT_LOCAL_ADVERTISED_HOST=$dependency_host"
  # 执行当前脚本步骤。
  'IOT_HTTP_ADDR=:8081'
  # 执行当前脚本步骤。
  'IOT_DEV_MODE=false'
  # 执行当前脚本步骤。
  'IOT_DATA_DIR=./data'
  # 执行当前脚本步骤。
  "IOT_POSTGRES_DSN=postgres://iot:${postgres_password}@${dependency_host}:15432/iot?sslmode=disable"
  # 执行当前脚本步骤。
  "IOT_REDIS_ADDR=${dependency_host}:16379"
  # 执行当前脚本步骤。
  "IOT_REDIS_PASSWORD=$(get_deployment_env_value "$env_file" REDIS_PASSWORD)"
  # 执行当前脚本步骤。
  "IOT_CLICKHOUSE_URL=http://iot:${clickhouse_password}@${dependency_host}:18123?database=iot"
  # 执行当前脚本步骤。
  "IOT_MINIO_ENDPOINT=${dependency_host}:19000"
  # 执行当前脚本步骤。
  "IOT_MINIO_ACCESS_KEY=$(get_deployment_env_value "$env_file" MINIO_ROOT_USER)"
  # 执行当前脚本步骤。
  "IOT_MINIO_SECRET_KEY=$(get_deployment_env_value "$env_file" MINIO_ROOT_PASSWORD)"
  # 执行当前脚本步骤。
  "IOT_KAFKA_BROKERS=${dependency_host}:19092"
  # 执行当前脚本步骤。
  "IOT_MQTT_BROKER=tcp://${dependency_host}:1883"
  # 执行当前脚本步骤。
  "IOT_MQTT_WEBSOCKET_PUBLIC_URL=ws://${dependency_host}:8083/mqtt"
  # 执行当前脚本步骤。
  "IOT_OLLAMA_URL=http://${dependency_host}:11434"
  # 执行当前脚本步骤。
  "IOT_AI_OLLAMA_URL=http://${dependency_host}:11434"
  # 执行当前脚本步骤。
  'IOT_AI_PROVIDER=deepseek'
  # 执行当前脚本步骤。
  'IOT_AI_BASE_URL=https://api.deepseek.com'
  # 执行当前脚本步骤。
  "IOT_AI_MODEL=$deepseek_model"
  # 执行当前脚本步骤。
  "IOT_WEAVIATE_URL=http://${dependency_host}:18080"
  # 执行当前脚本步骤。
  'IOT_BACKUP_URL=http://127.0.0.1:8092'
  # 执行当前脚本步骤。
  'IOT_BACKUP_HTTP_ADDR=:8092'
  # 执行当前脚本步骤。
  'IOT_AI_HARNESS_ENABLED=true'
  # 执行当前脚本步骤。
  "IOT_AI_HARNESS_URL=http://${dependency_host}:8091"
  # 执行当前脚本步骤。
  "IOT_AI_HARNESS_MCP_URL=http://${api_host}:8081/mcp/harness"
  # 执行当前脚本步骤。
  'IOT_AI_HARNESS_PROVIDER=deepseek-official'
  # 执行当前脚本步骤。
  "IOT_AI_HARNESS_MODEL=$deepseek_model"
  # 执行当前脚本步骤。
  "IOT_HARNESS_MCP_ALLOWED_ORIGINS=http://${api_host}:8081"
# 执行当前脚本步骤。
)
# 遍历数据并执行循环体。
for entry in "${defaults[@]}"; do
  # 执行当前脚本步骤。
  key="${entry%%=*}"
  # 执行当前脚本步骤。
  replace="$new_env"
  # 判断条件后执行对应操作。
  if [ "$dependency_host_set" = true ]; then
    # 执行当前脚本步骤。
    case "$key" in IOT_LOCAL_*|IOT_POSTGRES_DSN|IOT_REDIS_ADDR|IOT_CLICKHOUSE_URL|IOT_MINIO_ENDPOINT|IOT_KAFKA_BROKERS|IOT_MQTT_BROKER|IOT_MQTT_WEBSOCKET_PUBLIC_URL|IOT_OLLAMA_URL|IOT_AI_OLLAMA_URL|IOT_WEAVIATE_URL|IOT_BACKUP_URL|IOT_AI_HARNESS_MCP_URL|IOT_HARNESS_MCP_ALLOWED_ORIGINS) replace=true;; esac
  # 结束当前控制块。
  fi
  # 执行当前脚本步骤。
  set_local_env_value "$key" "${entry#*=}" "$replace"
# 结束当前控制块。
done
# The source-debugged API and backup worker run on the same host. Keep the
# worker endpoint local even when middleware containers are remote.
# 执行当前脚本步骤。
set_local_env_value IOT_BACKUP_URL 'http://127.0.0.1:8092' true
# 执行当前脚本步骤。
set_local_env_value IOT_BACKUP_HTTP_ADDR ':8092'
# 判断条件后执行对应操作。
if [ "$include_backup" = true ]; then set_local_env_value IOT_BACKUP_URL "http://${dependency_host}:8092" true; fi
# 判断条件后执行对应操作。
if [ "$include_ai" = true ]; then
  # 判断条件后执行对应操作。
  if [ "$(get_deployment_env_value "$env_file" IOT_AI_PROVIDER)" = ollama ]; then
    # 执行当前脚本步骤。
    configured_model="$(get_deployment_env_value "$env_file" IOT_AI_MODEL)"
    # 执行当前脚本步骤。
    ollama_model="${configured_model:-$ollama_model}"
  # 结束当前控制块。
  fi
  # 执行当前脚本步骤。
  set_local_env_value IOT_AI_PROVIDER ollama true
  # 执行当前脚本步骤。
  set_local_env_value IOT_OLLAMA_MODEL "$ollama_model" true
  # 执行当前脚本步骤。
  set_local_env_value IOT_AI_MODEL "$ollama_model" true
  # 执行当前脚本步骤。
  set_local_env_value IOT_AI_BASE_URL "http://${dependency_host}:11434" true
  # 执行当前脚本步骤。
  set_local_env_value IOT_AI_HARNESS_PROVIDER ollama true
  # 执行当前脚本步骤。
  set_local_env_value IOT_AI_HARNESS_OLLAMA_BASE_URL 'http://ollama:11434/v1' true
  # 执行当前脚本步骤。
  set_local_env_value IOT_AI_HARNESS_CONTEXT_WINDOW 8192 true
  # 执行当前脚本步骤。
  set_local_env_value IOT_AI_HARNESS_MODEL "$ollama_model" true
# 结束当前控制块。
fi
# 判断条件后执行对应操作。
if [ "$include_deepseek" = true ]; then
  # 执行当前脚本步骤。
  set_local_env_value IOT_AI_PROVIDER deepseek true
  # 执行当前脚本步骤。
  set_local_env_value IOT_AI_BASE_URL "https://api.deepseek.com" true
  # 执行当前脚本步骤。
  set_local_env_value IOT_AI_MODEL "$deepseek_model" true
  # 执行当前脚本步骤。
  set_local_env_value IOT_AI_HARNESS_PROVIDER deepseek-official true
  # 执行当前脚本步骤。
  set_local_env_value IOT_AI_HARNESS_MODEL "$deepseek_model" true
# 结束当前控制块。
fi
# 判断条件后执行对应操作。
if [ "$(get_deployment_env_value "$env_file" IOT_AI_PROVIDER)" = deepseek ]; then
  # 执行当前脚本步骤。
  deepseek_key="$(get_deployment_env_value "$env_file" DEEPSEEK_API_KEY)"
  # 判断条件后执行对应操作。
  if [ -z "$deepseek_key" ]; then deepseek_key="$(get_deployment_env_value "$env_file" IOT_AI_API_KEY)"; fi
  # 判断条件后执行对应操作。
  if [ -n "$deepseek_key" ] && [ -z "$(get_deployment_env_value "$env_file" DEEPSEEK_API_KEY)" ]; then set_local_env_value DEEPSEEK_API_KEY "$deepseek_key" true; fi
  # 执行当前脚本步骤。
  deepseek_base_url="$(get_deployment_env_value "$env_file" DEEPSEEK_BASE_URL)"
  # 执行当前脚本步骤。
  deepseek_base_url="${deepseek_base_url:-https://api.deepseek.com}"
  # 执行当前脚本步骤。
  [ -n "$(get_deployment_env_value "$env_file" IOT_AI_BASE_URL)" ] || set_local_env_value IOT_AI_BASE_URL "$deepseek_base_url" true
  # 执行当前脚本步骤。
  [ -n "$(get_deployment_env_value "$env_file" IOT_AI_MODEL)" ] || set_local_env_value IOT_AI_MODEL "$deepseek_model" true
  # 判断条件后执行对应操作。
  if [ -z "$deepseek_key" ]; then echo '提示：请在配置文件中填写 DEEPSEEK_API_KEY，自动研判和 AI 工作流将共用该密钥。' >&2; fi
# 结束当前控制块。
fi
# 执行当前脚本步骤。
case "$include_harness" in
  # 执行当前脚本步骤。
  true) set_local_env_value IOT_AI_HARNESS_ENABLED true true;;
  # 执行当前脚本步骤。
  false) set_local_env_value IOT_AI_HARNESS_ENABLED false true;;
# 执行当前脚本步骤。
esac
# 执行当前脚本步骤。
include_harness="$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_ENABLED)"
# 执行当前脚本步骤。
case "$include_harness" in true|false) ;; *) echo 'IOT_AI_HARNESS_ENABLED 只能是 true 或 false。' >&2; exit 1;; esac
# 判断条件后执行对应操作。
if [ "$include_harness" = true ]; then
  # 执行当前脚本步骤。
  ensure_deployment_git
  # 执行当前脚本步骤。
  bash "$script_dir/fetch-deepseek-harness.sh"
  # 执行当前脚本步骤。
  harness_url="$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_URL)"
  # 判断条件后执行对应操作。
  if [ -z "$harness_url" ] || [ "$dependency_host_set" = true ]; then set_local_env_value IOT_AI_HARNESS_URL "http://${dependency_host}:8091" true; fi
  # 执行当前脚本步骤。
  set_local_env_value IOT_AI_HARNESS_MCP_URL "http://${api_host}:8081/mcp/harness" true
  # 执行当前脚本步骤。
  set_local_env_value IOT_HARNESS_MCP_ALLOWED_ORIGINS "http://${api_host}:8081" true
# 执行当前脚本步骤。
else
  # 执行当前脚本步骤。
  set_local_env_value IOT_AI_HARNESS_URL '' true
# 结束当前控制块。
fi

# 执行当前脚本步骤。
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

# 执行当前脚本步骤。
cd -- "$project_root"
# 判断条件后执行对应操作。
if [ "$skip_code_deps" = false ]; then
  # 执行当前脚本步骤。
  echo '准备 Go 依赖……'
  # 执行当前脚本步骤。
  go mod download
  # 执行当前脚本步骤。
  echo '准备前端依赖……'
  # 执行当前脚本步骤。
  (cd iot_front && npm ci)
# 结束当前控制块。
fi
# 执行当前脚本步骤。
compose=(compose --project-name iot-platform-local --env-file "$env_file" -f compose.local.yaml)
# 判断条件后执行对应操作。
if [ "$include_harness" = true ]; then compose+=(--profile harness); fi
# 判断条件后执行对应操作。
if [ "$include_backup" = true ]; then compose+=(--profile backup); fi
# 判断条件后执行对应操作。
if [ "$include_ops" = true ]; then compose+=(--profile ops); fi
# 判断条件后执行对应操作。
if [ "$include_backup" = false ]; then
  # Stop an older worker; preserve the container and all backup data.
  # 执行当前脚本步骤。
  backup_compose=(compose --project-name iot-platform-local --env-file "$env_file" -f compose.local.yaml --profile backup)
  # 执行当前脚本步骤。
  run_docker "${backup_compose[@]}" stop backup-service
# 结束当前控制块。
fi
# 执行当前脚本步骤。
run_docker "${compose[@]}" config --quiet
# 执行当前脚本步骤。
run_docker "${compose[@]}" up -d --build --wait --wait-timeout 300
# 执行当前脚本步骤。
wait_deployment_http http://127.0.0.1:11434/api/tags 180
# 执行当前脚本步骤。
run_docker "${compose[@]}" exec -T ollama ollama pull nomic-embed-text
# 判断条件后执行对应操作。
if [ "$include_ai" = true ]; then run_docker "${compose[@]}" exec -T ollama ollama pull "$ollama_model"; fi
# 判断条件后执行对应操作。
if [ "$include_backup" = true ]; then wait_deployment_http http://127.0.0.1:8092/health/ready 180; fi
# 判断条件后执行对应操作。
if [ "$include_harness" = true ]; then wait_deployment_http http://127.0.0.1:8091/health 180; fi
# 判断条件后执行对应操作。
if [ "$include_ops" = true ]; then
  wait_deployment_http http://127.0.0.1:19090/-/ready 180
  wait_deployment_http http://127.0.0.1:13000/api/health 180
fi
# 执行当前脚本步骤。
printf '本地依赖已就绪。配置和管理员账号保存在：%s（凭据不输出）。\n' "$env_file"
# 执行当前脚本步骤。
printf '在 platform 目录启动后端：go run ./cmd/iot-platform --env-file %q\n' "$env_file"
# 执行当前脚本步骤。
echo '在 platform/iot_front 目录启动前端：npm run dev'
# 执行当前脚本步骤。
echo '备份服务默认不启动容器；在 VS Code 选择“IoT Platform (API + Web + Backup)”进行源码调试。'
# 判断条件后执行对应操作。
if [ "$include_backup" = true ]; then echo '已按 --include-backup 启动备份容器；停止后可改用 VS Code 源码调试。'; fi
# 执行当前脚本步骤。
echo '前端：http://localhost:5173；后端：http://localhost:8081'
# 判断条件后执行对应操作。
if [ "$include_ops" = true ]; then echo '运维中心依赖已启动：内置管理员可在“运维中心”菜单使用；其他账号需把所在租户加入 IOT_OPS_TENANTS。'; fi
# 判断条件后执行对应操作。
if [ "$bind_address" = 0.0.0.0 ]; then
  # 执行当前脚本步骤。
  printf '依赖已开放给远程源码环境：%s。请把 %s 复制到源码机器后使用。\n' "$dependency_host" "$env_file"
# 结束当前控制块。
fi
