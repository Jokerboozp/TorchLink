#!/usr/bin/env bash
# Start local dependencies; Go and Vite run on the host.
set -euo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
project_root="$(dirname -- "$script_dir")"
source "$script_dir/lib/deployment.sh"
env_file="$project_root/.env.local"
skip_code_deps=false
dependencies_only=false
video=keep
capacity=keep
video_args=()
include_backup=false
include_ops=false
deepseek_model=deepseek-flash
dependency_host=127.0.0.1
dependency_host_set=false
api_host=''
while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file) [ "$#" -ge 2 ] || { echo '--env-file 需要路径。' >&2; exit 1; }; env_file="$2"; shift 2 ;;
    --skip-code-deps) skip_code_deps=true; shift ;;
    --dependencies-only) dependencies_only=true; shift ;;
    --video) [ "$#" -ge 2 ] || { echo '--video 需要 on 或 off。' >&2; exit 1; }; video="$2"; shift 2 ;;
    --capacity) [ "$#" -ge 2 ] || { echo '--capacity 需要 on 或 off。' >&2; exit 1; }; capacity="$2"; shift 2 ;;
    --rtc-ip|--rtc-port|--allowed-cidrs) [ "$#" -ge 2 ] || { echo "$1 需要值。" >&2; exit 1; }; video_args+=("$1" "$2"); shift 2 ;;
    --transcode|--no-transcode) video_args+=("$1"); shift ;;
    --include-backup) include_backup=true; shift ;;
    --include-ops) include_ops=true; shift ;;
    --dependency-host) [ "$#" -ge 2 ] || { echo '--dependency-host 需要源码机可访问的主机名或 IPv4 地址。' >&2; exit 1; }; dependency_host="$2"; dependency_host_set=true; shift 2 ;;
    --api-host) [ "$#" -ge 2 ] || { echo '--api-host 需要依赖容器可访问的源码机主机名或 IPv4 地址。' >&2; exit 1; }; api_host="$2"; shift 2 ;;
    --deepseek-model) [ "$#" -ge 2 ] || { echo '--deepseek-model 需要模型名。' >&2; exit 1; }; deepseek_model="$2"; shift 2 ;;
    -h|--help)
      echo 'Usage: bash scripts/setup-local.sh [--dependencies-only] [--env-file PATH] [--skip-code-deps] [--dependency-host HOST] [--api-host HOST] [--deepseek-model MODEL] [--include-backup] [--include-ops] [--video on|off] [--capacity on|off] [--rtc-ip IP] [--rtc-port PORT] [--allowed-cidrs LIST] [--transcode|--no-transcode]'
      echo '--dependencies-only：仅在 Linux 部署全部基础环境（含运维），不安装源码依赖；API、Vue 和备份服务在源码机调试。OrbStack 自动使用 Mac 回调地址。'
      echo '--video：on 部署直播媒体服务，off 关闭并在后续运行中保持关闭；省略时沿用上次选择，新环境默认开启。'
      echo '--capacity on|off：本地容量控制服务随源码 API 启停；默认开启，显式关闭后保持关闭，不创建容量容器。'
      exit 0 ;;
    *) printf '未知参数：%s\n' "$1" >&2; exit 1 ;;
  esac
done
case "$video" in keep|on|off) ;; *) echo '--video 只能是 on 或 off。' >&2; exit 1;; esac
case "$capacity" in keep|on|off) ;; *) echo '--capacity 只能是 on 或 off。' >&2; exit 1;; esac
if [ "${#video_args[@]}" -gt 0 ] && [ "$video" = off ]; then echo '媒体选项不能与 --video off 同时使用。' >&2; exit 1; fi
if [ "$dependencies_only" = true ]; then
  [ "$(uname -s)" = Linux ] || { echo '--dependencies-only 请在 Linux 虚拟机内执行；OrbStack 使用 orb -m develop sudo bash scripts/setup-local.sh --dependencies-only。' >&2; exit 1; }
  skip_code_deps=true
  include_ops=true
fi
case "$env_file" in /*|[A-Za-z]:/*) ;; *) env_file="$project_root/$env_file" ;; esac
mkdir -p -- "$(dirname -- "$env_file")"
env_file="$(cd -- "$(dirname -- "$env_file")" && pwd)/$(basename -- "$env_file")"
[ "$env_file" != "$project_root/.env" ] || { echo '本地环境请使用 .env.local，不能覆盖在线部署的 .env。' >&2; exit 1; }
# Reusing the setup entry point to switch video must retain the dependency
# addresses. OrbStack containers call the Mac through host.orb.internal.
if [ "$dependency_host_set" = false ] && [ -f "$env_file" ]; then
  dependency_host="$(get_deployment_env_value "$env_file" IOT_LOCAL_ADVERTISED_HOST)"
  dependency_host="${dependency_host:-127.0.0.1}"
fi
if [ -z "$api_host" ]; then
  if [ -f "$env_file" ]; then api_host="$(get_deployment_env_value "$env_file" IOT_LOCAL_API_HOST)"; fi
  if [ -z "$api_host" ] && [ -d /opt/orbstack-guest ]; then api_host=host.orb.internal; fi
  api_host="${api_host:-host.docker.internal}"
fi

set_local_env_value() {
  local key="$1" value="$2" replace="${3:-false}" fill_empty="${4:-false}" updated
  if [[ "$value" == *"'"* || "$value" == *$'\n'* || "$value" == *$'\r'* ]]; then
    printf '配置 %s 含不支持的引号或换行，未写入。\n' "$key" >&2; return 1
  fi
  if [ "$replace" != true ] && awk -v key="$key" -v fill_empty="$fill_empty" '
      { line=$0; sub(/\r$/, "", line); sub(/^[[:space:]]*(export[[:space:]]+)?/, "", line) }
      line ~ "^" key "[[:space:]]*=" {
        found=1
        sub(/^[^=]*=/, "", line)
        raw=line; sub(/^[[:space:]]*/, "", line)
        quote=substr(line,1,1)
        if (quote == "\047" || quote == "\042") {
          line=substr(line,2); end=index(line,quote); if (end) line=substr(line,1,end-1)
        } else {
          line=raw; sub(/[[:space:]]+#.*$/, "", line)
          sub(/^[[:space:]]*/, "", line); sub(/[[:space:]]*$/, "", line)
        }
        current=line
      }
      END { exit !(found && (fill_empty != "true" || current != "")) }
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
ensure_emqx_admin_env "$env_file" "http://${dependency_host}:18083"
# Live video is deployed by default; an earlier --video off is kept.
if [ "$video" = keep ]; then
  video=on
  [ "$(get_deployment_env_value "$env_file" IOT_VIDEO_MODULE)" = off ] && video=off
fi
postgres_password="$(urlencode "$(get_deployment_env_value "$env_file" POSTGRES_PASSWORD)")"
clickhouse_password="$(urlencode "$(get_deployment_env_value "$env_file" CLICKHOUSE_PASSWORD)")"
bind_address=127.0.0.1
if [ "$dependency_host" != 127.0.0.1 ] && [ "$dependency_host" != localhost ]; then bind_address=0.0.0.0; fi
defaults=(
  "IOT_LOCAL_BIND_ADDRESS=$bind_address"
  "IOT_LOCAL_ADVERTISED_HOST=$dependency_host"
  "IOT_LOCAL_API_HOST=$api_host"
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
  'IOT_KAFKA_SASL_USERNAME=admin'
  'IOT_KAFKA_SASL_PASSWORD=admin123'
  'IOT_KAFKA_SASL_MECHANISM=SCRAM-SHA-256'
  'IOT_KAFKA_ADMIN_USERNAME=admin'
  'IOT_KAFKA_ADMIN_PASSWORD=admin123'
  'IOT_MQTT_TOOL_USERNAME=admin'
  'IOT_MQTT_TOOL_PASSWORD=admin123'
  "IOT_KAFKA_ADMIN_URL=http://${dependency_host}:19644"
  "IOT_KAFKA_ADVERTISED_HOST=${dependency_host}"
  "IOT_KAFKA_PUBLIC_BROKERS=${dependency_host}:19092"
  "IOT_MQTT_BROKER=tcp://${dependency_host}:1883"
  "IOT_MQTT_WEBSOCKET_PUBLIC_URL=ws://${dependency_host}:8083/mqtt"
  'IOT_AI_PROVIDER=deepseek'
  'IOT_AI_BASE_URL=https://api.deepseek.com'
  "IOT_AI_MODEL=$deepseek_model"
  'IOT_BACKUP_URL=http://127.0.0.1:8092'
  'IOT_BACKUP_HTTP_ADDR=:8092'
  "IOT_BACKUP_HARNESS_SNAPSHOT_URLS=http://${dependency_host}:8091/v1/backup/snapshot"
  'IOT_BACKUP_RESTORE_HARNESS_DIR=./data/backups/restored-harness'
  "IOT_BACKUP_RESTORE_MINIO_ENDPOINT=${dependency_host}:19001"
  "IOT_BACKUP_RESTORE_MINIO_ACCESS_KEY=$(get_deployment_env_value "$env_file" MINIO_DR_ROOT_USER)"
  "IOT_BACKUP_RESTORE_MINIO_SECRET_KEY=$(get_deployment_env_value "$env_file" MINIO_DR_ROOT_PASSWORD)"
  "IOT_AI_HARNESS_URL=http://${dependency_host}:8091"
  "IOT_AI_HARNESS_MCP_URL=http://${api_host}:8081/mcp/harness"
  'IOT_AI_HARNESS_PROVIDER=deepseek-official'
  "IOT_AI_HARNESS_MODEL=$deepseek_model"
  "IOT_HARNESS_MCP_ALLOWED_ORIGINS=http://${api_host}:8081"
)
for entry in "${defaults[@]}"; do
  key="${entry%%=*}"
  replace="$new_env"
  fill_empty=false
  if [ "$dependency_host_set" = true ]; then
    case "$key" in IOT_LOCAL_*|IOT_POSTGRES_DSN|IOT_REDIS_ADDR|IOT_CLICKHOUSE_URL|IOT_MINIO_ENDPOINT|IOT_KAFKA_BROKERS|IOT_KAFKA_ADMIN_URL|IOT_KAFKA_ADVERTISED_HOST|IOT_KAFKA_PUBLIC_BROKERS|IOT_MQTT_BROKER|IOT_MQTT_WEBSOCKET_PUBLIC_URL|IOT_BACKUP_URL|IOT_BACKUP_HARNESS_SNAPSHOT_URLS|IOT_BACKUP_RESTORE_MINIO_ENDPOINT|IOT_AI_HARNESS_MCP_URL|IOT_HARNESS_MCP_ALLOWED_ORIGINS) replace=true;; esac
  fi
  case "$key" in
    IOT_KAFKA_SASL_USERNAME|IOT_KAFKA_SASL_PASSWORD|IOT_KAFKA_SASL_MECHANISM|IOT_KAFKA_ADMIN_USERNAME|IOT_KAFKA_ADMIN_PASSWORD|IOT_KAFKA_ADMIN_URL|IOT_KAFKA_ADVERTISED_HOST|IOT_KAFKA_PUBLIC_BROKERS|IOT_MQTT_TOOL_USERNAME|IOT_MQTT_TOOL_PASSWORD)
      # Earlier templates left broker credentials empty. The setter checks the
      # file itself, preserving saved credentials despite process overrides.
      fill_empty=true
      ;;
  esac
  set_local_env_value "$key" "${entry#*=}" "$replace" "$fill_empty"
done
set_local_env_value IOT_LOCAL_API_HOST "$api_host" true
# The source-debugged API and backup worker run on the same host. Keep the
# worker endpoint local even when middleware containers are remote.
set_local_env_value IOT_BACKUP_URL 'http://127.0.0.1:8092' true
set_local_env_value IOT_BACKUP_HARNESS_SNAPSHOT_URLS "http://${dependency_host}:8091/v1/backup/snapshot" true
set_local_env_value IOT_BACKUP_HTTP_ADDR ':8092'
if [ "$include_backup" = true ]; then set_local_env_value IOT_BACKUP_URL "http://${dependency_host}:8092" true; fi
if [ "$include_backup" = true ]; then
  set_local_env_value IOT_LOCAL_BACKUP_METRICS_TARGET 'backup-service:8090' true
else
  set_local_env_value IOT_LOCAL_BACKUP_METRICS_TARGET "${api_host}:8092" true
fi
configure_deepseek_env "$env_file" "$deepseek_model"
configure_embedding_env "$env_file"

# The controller follows the source API lifecycle, using its local addresses.
if [ "$capacity" = keep ]; then
  capacity=on
  [ "$(get_deployment_env_value "$env_file" IOT_CAPACITY_MODULE)" != off ] || capacity=off
fi
set_local_env_value IOT_OPS_CAPACITY_LOCAL true true
set_local_env_value IOT_CAPACITY_MODULE "$capacity" true

# AI 工作流服务（Harness）为必装组件：告警研判、巡检、报告、协议助手和规则草稿都通过它运行。
ensure_deployment_git
bash "$script_dir/fetch-deepseek-harness.sh"
harness_url="$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_URL)"
if [ -z "$harness_url" ] || [ "$dependency_host_set" = true ]; then set_local_env_value IOT_AI_HARNESS_URL "http://${dependency_host}:8091" true; fi
set_local_env_value IOT_AI_HARNESS_MCP_URL "http://${api_host}:8081/mcp/harness" true
set_local_env_value IOT_HARNESS_MCP_ALLOWED_ORIGINS "http://${api_host}:8081" true

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
  ops_path="$(get_deployment_env_value "$env_file" IOT_LOCAL_OPS_DIR)"
  ops_path="${ops_path:-./data/ops}"
  ops_dir="$ops_path"
  case "$ops_dir" in /*) ;; *) ops_dir="$project_root/$ops_dir";; esac
  mkdir -p "$ops_dir/prometheus-rules" "$ops_dir/loki/rules/fake" "$ops_dir/alertmanager"
  [ -f "$ops_dir/loki/runtime.yaml" ] || printf 'overrides: {}\n' > "$ops_dir/loki/runtime.yaml"
  [ -f "$ops_dir/alertmanager/alertmanager.yml" ] || printf '%s\n' 'route:' '  receiver: platform-null' '  group_by: [alertname, severity]' 'receivers:' '  - name: platform-null' > "$ops_dir/alertmanager/alertmanager.yml"
  chmod 0755 "$ops_dir" "$ops_dir/prometheus-rules" "$ops_dir/loki" "$ops_dir/loki/rules" "$ops_dir/loki/rules/fake" "$ops_dir/alertmanager"
  chmod 0644 "$ops_dir/loki/runtime.yaml" "$ops_dir/alertmanager/alertmanager.yml"
  if [ "$dependency_host" = 127.0.0.1 ] || [ "$dependency_host" = localhost ]; then
    set_local_env_value IOT_OPS_PROMETHEUS_RULES_DIR "$ops_path/prometheus-rules" true
    set_local_env_value IOT_OPS_LOKI_RULES_DIR "$ops_path/loki/rules/fake" true
    set_local_env_value IOT_OPS_LOKI_RUNTIME_FILE "$ops_path/loki/runtime.yaml" true
    set_local_env_value IOT_OPS_ALERTMANAGER_CONFIG_FILE "$ops_path/alertmanager/alertmanager.yml" true
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
if [ "$video" = off ]; then
  bash "$script_dir/video-module.sh" disable --mode local --env-file "$env_file"
fi
if [ "$include_backup" = false ]; then
  # Stop an older worker; preserve the container and all backup data.
  backup_compose=(compose --project-name iot-platform-local --env-file "$env_file" -f compose.local.yaml --profile backup)
  run_docker "${backup_compose[@]}" stop backup-service
fi
run_docker "${compose[@]}" config --quiet
run_docker "${compose[@]}" up -d --build --wait --wait-timeout 900
if [ "$include_backup" = true ]; then wait_deployment_http http://127.0.0.1:8092/health/ready 180; fi
wait_deployment_http http://127.0.0.1:8091/health 180
if [ "$include_ops" = true ]; then
  wait_deployment_http http://127.0.0.1:19090/-/ready 180
  wait_deployment_http http://127.0.0.1:13000/api/health 180
  wait_deployment_http http://127.0.0.1:13100/ready 180
  wait_deployment_http http://127.0.0.1:19093/-/ready 180
fi
if [ "$video" = on ]; then
  bash "$script_dir/video-module.sh" enable --mode local --env-file "$env_file" ${video_args[@]+"${video_args[@]}"}
fi
annotate_deployment_env_file "$env_file"
printf '本地依赖已就绪。配置和管理员账号保存在：%s（凭据不输出）。\n' "$env_file"
echo '在源码机仓库根目录启动后端：go run ./cmd/iot-platform --env-file .env.local'
if [ "$capacity" = on ]; then echo '本地容量测试随 API 启动，在“运维中心 → 容量测试”使用；不会自动开始发压。'; fi
echo '在源码机 iot_front 目录启动前端：npm run dev'
if [ "$include_backup" = true ]; then
  echo '备份服务已在依赖机运行；VS Code 选择“IoT Platform (API + Web)”，不要重复启动本机备份服务。'
else
  echo '备份服务默认不启动容器；在 VS Code 选择“IoT Platform (API + Web + Backup)”进行源码调试。'
fi
echo '前端：http://localhost:5173；后端：http://localhost:8081'
if [ "$include_ops" = true ]; then echo '运维中心依赖已启动：内置管理员可在“运维中心”菜单使用；其他账号需把所在租户加入 IOT_OPS_TENANTS。'; fi
if [ "$bind_address" = 0.0.0.0 ]; then
  printf '依赖已开放给远程源码环境：%s。请把 %s 复制到源码机器后使用。\n' "$dependency_host" "$env_file"
fi
