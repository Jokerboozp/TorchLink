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
include_harness=auto
ollama_model=qwen3:8b
deepseek_model=deepseek-v4-flash
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
    --no-harness) include_harness=false; shift ;;
    --dependency-host) [ "$#" -ge 2 ] || { echo '--dependency-host 需要 Windows 可访问的主机名或 IPv4 地址。' >&2; exit 1; }; dependency_host="$2"; dependency_host_set=true; shift 2 ;;
    --api-host) [ "$#" -ge 2 ] || { echo '--api-host 需要依赖容器可访问的源码机主机名或 IPv4 地址。' >&2; exit 1; }; api_host="$2"; shift 2 ;;
    --ollama-model) [ "$#" -ge 2 ] || { echo '--ollama-model 需要模型名。' >&2; exit 1; }; ollama_model="$2"; shift 2 ;;
    --deepseek-model) [ "$#" -ge 2 ] || { echo '--deepseek-model 需要模型名。' >&2; exit 1; }; deepseek_model="$2"; shift 2 ;;
    -h|--help) echo 'Usage: bash scripts/setup-local.sh [--env-file PATH] [--skip-code-deps] [--dependency-host HOST] [--api-host HOST] [--include-ai|--include-deepseek] [--ollama-model MODEL] [--deepseek-model MODEL] [--include-harness|--no-harness]'; exit 0 ;;
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

assert_docker_available
[[ "$ollama_model" =~ ^[A-Za-z0-9][A-Za-z0-9._:/-]*$ ]] || { echo 'Ollama 模型名称无效。' >&2; exit 1; }
[[ "$deepseek_model" =~ ^[A-Za-z0-9][A-Za-z0-9._:/-]*$ ]] || { echo 'DeepSeek 模型名称无效。' >&2; exit 1; }
[[ "$dependency_host" =~ ^[A-Za-z0-9][A-Za-z0-9.-]*$ ]] || { echo '依赖主机必须是主机名或 IPv4 地址。' >&2; exit 1; }
[[ "$api_host" =~ ^[A-Za-z0-9][A-Za-z0-9.-]*$ ]] || { echo '源码机必须是主机名或 IPv4 地址。' >&2; exit 1; }
[ "$include_ai" = false ] || [ "$include_deepseek" = false ] || { echo '--include-ai 与 --include-deepseek 只能二选一。' >&2; exit 1; }
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
  "IOT_BACKUP_URL=http://${dependency_host}:8092"
  'IOT_AI_HARNESS_ENABLED=true'
  "IOT_AI_HARNESS_URL=http://${dependency_host}:8091"
  "IOT_AI_HARNESS_MCP_URL=http://${api_host}:8081/mcp/harness"
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
if [ "$include_ai" = true ]; then
  if [ "$(get_deployment_env_value "$env_file" IOT_AI_PROVIDER)" = ollama ]; then
    configured_model="$(get_deployment_env_value "$env_file" IOT_AI_MODEL)"
    ollama_model="${configured_model:-$ollama_model}"
  fi
  set_local_env_value IOT_AI_PROVIDER ollama true
  set_local_env_value IOT_OLLAMA_MODEL "$ollama_model" true
  set_local_env_value IOT_AI_MODEL "$ollama_model" true
  set_local_env_value IOT_AI_BASE_URL "http://${dependency_host}:11434" true
fi
if [ "$include_deepseek" = true ]; then
  set_local_env_value IOT_AI_PROVIDER deepseek true
  set_local_env_value IOT_AI_BASE_URL "https://api.deepseek.com" true
  set_local_env_value IOT_AI_MODEL "$deepseek_model" true
fi
if [ "$(get_deployment_env_value "$env_file" IOT_AI_PROVIDER)" = deepseek ]; then
  deepseek_key="$(get_deployment_env_value "$env_file" DEEPSEEK_API_KEY)"
  if [ -z "$deepseek_key" ]; then deepseek_key="$(get_deployment_env_value "$env_file" IOT_AI_API_KEY)"; fi
  if [ -n "$deepseek_key" ] && [ -z "$(get_deployment_env_value "$env_file" DEEPSEEK_API_KEY)" ]; then set_local_env_value DEEPSEEK_API_KEY "$deepseek_key" true; fi
  deepseek_base_url="$(get_deployment_env_value "$env_file" DEEPSEEK_BASE_URL)"
  deepseek_base_url="${deepseek_base_url:-https://api.deepseek.com}"
  [ -n "$(get_deployment_env_value "$env_file" IOT_AI_BASE_URL)" ] || set_local_env_value IOT_AI_BASE_URL "$deepseek_base_url" true
  [ -n "$(get_deployment_env_value "$env_file" IOT_AI_MODEL)" ] || set_local_env_value IOT_AI_MODEL "$deepseek_model" true
  if [ -z "$deepseek_key" ]; then echo '提示：请在配置文件中填写 DEEPSEEK_API_KEY，自动研判和 AI 工作流将共用该密钥。' >&2; fi
fi
case "$include_harness" in
  true) set_local_env_value IOT_AI_HARNESS_ENABLED true true;;
  false) set_local_env_value IOT_AI_HARNESS_ENABLED false true;;
esac
include_harness="$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_ENABLED)"
case "$include_harness" in true|false) ;; *) echo 'IOT_AI_HARNESS_ENABLED 只能是 true 或 false。' >&2; exit 1;; esac
if [ "$include_harness" = true ]; then
  bash "$script_dir/fetch-deepseek-harness.sh"
  harness_url="$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_URL)"
  if [ -z "$harness_url" ] || [ "$dependency_host_set" = true ]; then set_local_env_value IOT_AI_HARNESS_URL "http://${dependency_host}:8091" true; fi
  set_local_env_value IOT_AI_HARNESS_MCP_URL "http://${api_host}:8081/mcp/harness" true
  set_local_env_value IOT_HARNESS_MCP_ALLOWED_ORIGINS "http://${api_host}:8081" true
else
  set_local_env_value IOT_AI_HARNESS_URL '' true
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
if [ "$include_harness" = true ]; then compose+=(--profile harness); fi
run_docker "${compose[@]}" config --quiet
run_docker "${compose[@]}" up -d --build --wait --wait-timeout 300
wait_deployment_http http://127.0.0.1:11434/api/tags 180
run_docker "${compose[@]}" exec -T ollama ollama pull nomic-embed-text
if [ "$include_ai" = true ]; then run_docker "${compose[@]}" exec -T ollama ollama pull "$ollama_model"; fi
wait_deployment_http http://127.0.0.1:8092/health/live 180
if [ "$include_harness" = true ]; then wait_deployment_http http://127.0.0.1:8091/health 180; fi
printf '本地依赖已就绪。配置和管理员账号保存在：%s（凭据不输出）。\n' "$env_file"
printf '在 platform 目录启动后端：go run ./cmd/iot-platform --env-file %q\n' "$env_file"
echo '在 platform/iot_front 目录启动前端：npm run dev'
echo '前端：http://localhost:5173；后端：http://localhost:8081'
if [ "$bind_address" = 0.0.0.0 ]; then
  printf '依赖已开放给远程源码环境：%s。请把 %s 复制到源码机器后使用。\n' "$dependency_host" "$env_file"
fi
