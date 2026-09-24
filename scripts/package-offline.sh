#!/usr/bin/env bash
# 执行当前脚本步骤。
set -Eeuo pipefail

# 执行当前脚本步骤。
umask 077
# 执行当前脚本步骤。
script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
# 执行当前脚本步骤。
project_root="$(CDPATH= cd -- "$script_dir/.." && pwd)"
# 执行当前脚本步骤。
source "$script_dir/lib/env-comments.sh"
# 执行当前脚本步骤。
source "$script_dir/lib/docker-bootstrap.sh"

# 执行当前脚本步骤。
output_dir="offline-bundles"
# 执行当前脚本步骤。
env_file=""
# 执行当前脚本步骤。
include_ai=1
# 执行当前脚本步骤。
include_harness=1
# 执行当前脚本步骤。
full=0
# 执行当前脚本步骤。
ollama_model="qwen3:1.7b"
# 执行当前脚本步骤。
ollama_embedding_model="nomic-embed-text"
# 执行当前脚本步骤。
skip_ollama_model=0
# 执行当前脚本步骤。
skip_docker_runtime=0
# 执行当前脚本步骤。
docker_packages_dir=""
# 执行当前脚本步骤。
target_os="generic"

# 执行当前脚本步骤。
usage() {
  # 执行当前脚本步骤。
  cat <<'EOF'
用法：
  package-offline.sh [选项]

选项：
  --output-dir DIR       输出父目录，默认 offline-bundles
  --env-file FILE        使用已有正式环境配置；不传则自动生成随机密钥
  --include-ai           兼容参数；默认已经打包并启用本地对话模型
  --include-harness      兼容参数；默认已经打包 AI 工作流 Harness
  --ollama-model MODEL   需要一起打包的 Ollama 对话模型，默认 qwen3:1.7b
  --ollama-embedding-model MODEL  Weaviate 向量模型，默认 nomic-embed-text
  --skip-ollama-model    跳过全部模型；仅用于目标机已准备模型的情况
  --skip-docker-runtime 不携带 Docker 安装文件（目标机须已有 Docker 和 Compose）
  --target-os OS        generic（默认）或 openeuler-24.03-lts-sp4；后者自动准备容器策略及系统依赖
  --docker-packages-dir DIR  可选：匹配目标系统的系统工具/SELinux 及依赖 RPM/DEB 目录
  --full                 兼容参数；AI 与 Harness 已默认启用
  -h, --help             显示帮助
EOF
# 结束当前控制块。
}

# 执行当前脚本步骤。
die() {
  # 执行当前脚本步骤。
  echo "错误：$*" >&2
  # 返回结果或结束当前脚本。
  exit 1
# 结束当前控制块。
}

# 执行当前脚本步骤。
run_docker() {
  # 执行当前脚本步骤。
  echo "> docker $*" >&2
  # 执行当前脚本步骤。
  docker "$@"
# 结束当前控制块。
}

# 执行当前脚本步骤。
random_hex() {
  # 执行当前脚本步骤。
  local bytes="${1:-24}"
  # 判断条件后执行对应操作。
  if command -v openssl >/dev/null 2>&1; then
    # 执行当前脚本步骤。
    openssl rand -hex "$bytes" | tr -d '\r\n'
  # 执行当前脚本步骤。
  else
    # 执行当前脚本步骤。
    od -An -N "$bytes" -tx1 /dev/urandom | tr -d ' \r\n'
  # 结束当前控制块。
  fi
# 结束当前控制块。
}

# 执行当前脚本步骤。
sha256_file() {
  # 判断条件后执行对应操作。
  if command -v sha256sum >/dev/null 2>&1; then
    # 执行当前脚本步骤。
    sha256sum "$1" | awk '{print tolower($1)}'
  # 执行当前脚本步骤。
  elif command -v shasum >/dev/null 2>&1; then
    # 执行当前脚本步骤。
    shasum -a 256 "$1" | awk '{print tolower($1)}'
  # 执行当前脚本步骤。
  else
    # 执行当前脚本步骤。
    die "找不到 sha256sum 或 shasum，无法校验镜像包"
  # 结束当前控制块。
  fi
# 结束当前控制块。
}

# 执行当前脚本步骤。
env_value() {
  # 执行当前脚本步骤。
  local key="$1"
  # 执行当前脚本步骤。
  local file="$2"
  # 执行当前脚本步骤。
  awk -v key="$key" '
    $0 ~ "^[[:space:]]*" key "[[:space:]]*=" {
      sub("^[[:space:]]*" key "[[:space:]]*=[[:space:]]*", "")
      sub(/\r$/, ""); sub(/[[:space:]]+$/, "")
      if (($0 ~ /^".*"$/) || ($0 ~ /^\047.*\047$/)) $0=substr($0,2,length($0)-2)
      value=$0
    }
    END { print value }
  ' "$file"
# 结束当前控制块。
}

# 执行当前脚本步骤。
set_env_value() {
  # 执行当前脚本步骤。
  local file="$1"
  # 执行当前脚本步骤。
  local key="$2"
  # 执行当前脚本步骤。
  local value="$3"
  # 执行当前脚本步骤。
  local tmp
  # 执行当前脚本步骤。
  tmp="$(mktemp "${file}.tmp.XXXXXX")"
  # 执行当前脚本步骤。
  awk -v key="$key" -v value="$value" '
    BEGIN { replaced = 0 }
    $0 ~ "^[[:space:]]*" key "[[:space:]]*=" {
      if (!replaced) { print key "=" value; replaced = 1 }
      next
    }
    { print }
    END { if (!replaced) print key "=" value }
  ' "$file" > "$tmp"
  # 执行当前脚本步骤。
  mv "$tmp" "$file"
# 结束当前控制块。
}

# 执行当前脚本步骤。
validate_env() {
  # 执行当前脚本步骤。
  local file="$1"
  # 执行当前脚本步骤。
  local key value
  # 执行当前脚本步骤。
  local required_keys=(
    # 执行当前脚本步骤。
    POSTGRES_PASSWORD REDIS_PASSWORD CLICKHOUSE_PASSWORD
    # 执行当前脚本步骤。
    MINIO_ROOT_PASSWORD MINIO_DR_ROOT_PASSWORD IOT_JWT_SECRET
    # 执行当前脚本步骤。
    IOT_ADMIN_USER IOT_ADMIN_PASSWORD IOT_ADMIN_TENANTS
    # 执行当前脚本步骤。
    IOT_VIDEO_PLATFORM_SECRETS IOT_BACKUP_ADMIN_TOKEN
    # 执行当前脚本步骤。
    EMQX_DASHBOARD_USER EMQX_DASHBOARD_PASSWORD
    # 执行当前脚本步骤。
    GRAFANA_ADMIN_USER GRAFANA_ADMIN_PASSWORD
  # 执行当前脚本步骤。
  )
  # 执行当前脚本步骤。
  (( include_harness )) && required_keys+=(IOT_AI_HARNESS_TOKEN)
  # 遍历数据并执行循环体。
  for key in "${required_keys[@]}"; do
    # 执行当前脚本步骤。
    value="$(env_value "$key" "$file")"
    # 执行当前脚本步骤。
    [[ -n "${value//[[:space:]]/}" ]] || die "EnvFile 缺少必填安全配置：$key"
  # 结束当前控制块。
  done
  # 判断条件后执行对应操作。
  if awk -F= '$1 != "IOT_ADMIN_PASSWORD"' "$file" | grep -Eq '^[A-Za-z_][A-Za-z0-9_]*=.*(change-this|local-iot-|admin123|public-change-me|change-me)'; then
    # 执行当前脚本步骤。
    die "EnvFile 仍包含示例密码或默认密钥，请先替换后再打包"
  # 结束当前控制块。
  fi
# 结束当前控制块。
}

# 执行当前脚本步骤。
write_env() {
  # 执行当前脚本步骤。
  local destination="$1"
  # 执行当前脚本步骤。
  local generated=0
  # 判断条件后执行对应操作。
  if [[ -n "$env_file" ]]; then
    # 执行当前脚本步骤。
    [[ -f "$env_file" ]] || die "指定的 EnvFile 不存在：$env_file"
    # 执行当前脚本步骤。
    cp "$env_file" "$destination"
  # 执行当前脚本步骤。
  else
    # 执行当前脚本步骤。
    generated=1
    # 执行当前脚本步骤。
    local postgres_password="pg-$(random_hex 18)"
    # 执行当前脚本步骤。
    local redis_password="redis-$(random_hex 18)"
    # 执行当前脚本步骤。
    local clickhouse_password="ch-$(random_hex 18)"
    # 执行当前脚本步骤。
    local minio_password="minio-$(random_hex 18)"
    # 执行当前脚本步骤。
    local minio_dr_password="minio-dr-$(random_hex 18)"
    # 执行当前脚本步骤。
    local jwt_secret="$(random_hex 32)"
    # 执行当前脚本步骤。
    local admin_password="admin123"
    # 执行当前脚本步骤。
    local video_secret="$(random_hex 24)"
    # 执行当前脚本步骤。
    local harness_token="$(random_hex 32)"
    # 执行当前脚本步骤。
    local backup_token="$(random_hex 32)"
    # 执行当前脚本步骤。
    local emqx_password="Emqx-$(random_hex 12)"
    # 执行当前脚本步骤。
    local grafana_password="Grafana-$(random_hex 12)"
    # 执行当前脚本步骤。
    local ollama_url="http://ollama:11434"
    # 执行当前脚本步骤。
    local ai_provider="ollama"
    # 执行当前脚本步骤。
    local weaviate_url="http://weaviate:8080"
    # 执行当前脚本步骤。
    local harness_url="http://deepseek-harness:8091"
    # 执行当前脚本步骤。
    local harness_enabled="true"
    # 判断条件后执行对应操作。
    if (( include_ai )); then
      # 执行当前脚本步骤。
      ollama_url="http://ollama:11434"
      # 执行当前脚本步骤。
      ai_provider="ollama"
      # 执行当前脚本步骤。
      weaviate_url="http://weaviate:8080"
    # 结束当前控制块。
    fi
    # 判断条件后执行对应操作。
    if (( include_harness )); then
      # 执行当前脚本步骤。
      harness_url="http://deepseek-harness:8091"
      # 执行当前脚本步骤。
      harness_enabled="true"
    # 结束当前控制块。
    fi
    # 执行当前脚本步骤。
    cat > "$destination" <<EOF
# 自动生成的离线部署配置，请限制此文件权限。
POSTGRES_PASSWORD=$postgres_password
REDIS_PASSWORD=$redis_password
CLICKHOUSE_PASSWORD=$clickhouse_password
MINIO_ROOT_USER=iotadmin
MINIO_ROOT_PASSWORD=$minio_password
MINIO_DR_ROOT_USER=iotdradmin
MINIO_DR_ROOT_PASSWORD=$minio_dr_password
IOT_JWT_SECRET=$jwt_secret
IOT_ADMIN_USER=admin
IOT_ADMIN_PASSWORD=$admin_password
IOT_ADMIN_TENANTS=tenant_001
IOT_VIDEO_PLATFORM_SECRETS=video-platform-1:$video_secret
IOT_VIDEO_MEDIA_ALLOWED_HOSTS=
IOT_OLLAMA_URL=$ollama_url
IOT_OLLAMA_MODEL=$ollama_model
IOT_AI_PROVIDER=$ai_provider
IOT_AI_BASE_URL=http://ollama:11434
IOT_AI_MODEL=$ollama_model
IOT_AI_API_KEY=
IOT_AI_OLLAMA_URL=http://ollama:11434
DEEPSEEK_API_KEY=
IOT_AI_HARNESS_ENABLED=$harness_enabled
IOT_AI_HARNESS_URL=$harness_url
IOT_AI_HARNESS_TOKEN=$harness_token
IOT_AI_HARNESS_MCP_URL=http://platform-api:8080/mcp/harness
IOT_AI_HARNESS_PROVIDER=ollama
IOT_AI_HARNESS_OLLAMA_BASE_URL=http://ollama:11434/v1
IOT_AI_HARNESS_CONTEXT_WINDOW=8192
IOT_AI_HARNESS_MODEL=$ollama_model
IOT_AI_HARNESS_TIMEOUT=90s
IOT_WEAVIATE_URL=$weaviate_url
IOT_BACKUP_ADMIN_TOKEN=$backup_token
IOT_RAW_HIGH_FREQUENCY_INTERVAL_SEC=60
IOT_BACKUP_TIME=00:05
IOT_BACKUP_TIMEZONE=Asia/Shanghai
IOT_MQTT_WEBSOCKET_PUBLIC_URL=
IOT_DEVICE_HTTP_PUBLIC_URL=
IOT_DEVICE_MQTT_PUBLIC_URL=
IOT_PROCESS_ROLE=combined
IOT_ACCESS_GATEWAY_URL=
IOT_ACCESS_COORDINATION=false
IOT_ACCESS_NODE_URL=
IOT_WEB_PORT=8080
IOT_API_PORT=8081
IOT_CORS_ALLOWED_ORIGINS=http://localhost:8080,http://127.0.0.1:8080
EMQX_DASHBOARD_USER=admin
EMQX_DASHBOARD_PASSWORD=$emqx_password
GRAFANA_ADMIN_USER=admin
GRAFANA_ADMIN_PASSWORD=$grafana_password
EOF
    # 执行当前脚本步骤。
    cat > "$(dirname -- "$destination")/OFFLINE-CREDENTIALS.txt" <<EOF
# 离线部署凭据
# 请将本文件视为密码文件，不要提交 Git 或公开传输。
平台管理员：admin
平台管理员密码：$admin_password
备份服务 Token：$backup_token
EMQX Dashboard：admin / $emqx_password
Grafana：admin / $grafana_password
PostgreSQL 密码：$postgres_password
Redis 密码：$redis_password
ClickHouse 密码：$clickhouse_password
MinIO 主密码：$minio_password
MinIO 灾备密码：$minio_dr_password
EOF
  # 结束当前控制块。
  fi

  # 执行当前脚本步骤。
  set_env_value "$destination" IOT_PLATFORM_API_IMAGE iot-platform-api:offline
  # 执行当前脚本步骤。
  set_env_value "$destination" IOT_PLATFORM_WEB_IMAGE iot-platform-web:offline
  # 执行当前脚本步骤。
  set_env_value "$destination" IOT_BACKUP_IMAGE iot-platform-backup:offline
  # 执行当前脚本步骤。
  set_env_value "$destination" IOT_DEEPSEEK_HARNESS_IMAGE iot-deepseek-harness:offline
  # 判断条件后执行对应操作。
  if (( include_harness )); then
    # 执行当前脚本步骤。
    set_env_value "$destination" IOT_AI_HARNESS_ENABLED true
  # 执行当前脚本步骤。
  elif [[ -z "$(env_value IOT_AI_HARNESS_ENABLED "$destination")" ]]; then
    # 执行当前脚本步骤。
    set_env_value "$destination" IOT_AI_HARNESS_ENABLED false
  # 结束当前控制块。
  fi
  # 判断条件后执行对应操作。
  if [[ "$(env_value IOT_AI_PROVIDER "$destination")" == ollama ]]; then
    # 执行当前脚本步骤。
    local selected_model
    # 执行当前脚本步骤。
    selected_model="$(env_value IOT_AI_MODEL "$destination")"
    # 执行当前脚本步骤。
    [[ -n "$selected_model" ]] || selected_model="$(env_value IOT_OLLAMA_MODEL "$destination")"
    # 执行当前脚本步骤。
    selected_model="${selected_model:-$ollama_model}"
    # 执行当前脚本步骤。
    [[ "$selected_model" != qwen3:8b || "$ollama_model" == qwen3:8b ]] || selected_model="$ollama_model"
    # 执行当前脚本步骤。
    set_env_value "$destination" IOT_OLLAMA_URL http://ollama:11434
    # 执行当前脚本步骤。
    set_env_value "$destination" IOT_OLLAMA_MODEL "$selected_model"
    # 执行当前脚本步骤。
    set_env_value "$destination" IOT_AI_BASE_URL http://ollama:11434
    # 执行当前脚本步骤。
    set_env_value "$destination" IOT_AI_MODEL "$selected_model"
    # 执行当前脚本步骤。
    set_env_value "$destination" IOT_AI_HARNESS_PROVIDER ollama
    # 执行当前脚本步骤。
    set_env_value "$destination" IOT_AI_HARNESS_OLLAMA_BASE_URL http://ollama:11434/v1
    # 执行当前脚本步骤。
    set_env_value "$destination" IOT_AI_HARNESS_CONTEXT_WINDOW 8192
    # 执行当前脚本步骤。
    set_env_value "$destination" IOT_AI_HARNESS_MODEL "$selected_model"
  # 结束当前控制块。
  fi
  # 执行当前脚本步骤。
  annotate_deployment_env_file "$destination"

  # 判断条件后执行对应操作。
  if (( ! generated )); then
    # 执行当前脚本步骤。
    cat > "$(dirname -- "$destination")/OFFLINE-CREDENTIALS.txt" <<EOF
凭据来自外部 EnvFile：$env_file
本文件不复制外部 EnvFile 的内容，请单独保管原始凭据。
EOF
  # 结束当前控制块。
  fi
  # 执行当前脚本步骤。
  validate_env "$destination"
  # 执行当前脚本步骤。
  printf '%s\n' "$generated"
# 结束当前控制块。
}

# 遍历数据并执行循环体。
while [[ $# -gt 0 ]]; do
  # 执行当前脚本步骤。
  case "$1" in
    # 执行当前脚本步骤。
    --output-dir) output_dir="${2:-}"; shift 2 ;;
    # 执行当前脚本步骤。
    --env-file) env_file="${2:-}"; shift 2 ;;
    # 执行当前脚本步骤。
    --include-ai) include_ai=1; shift ;;
    # 执行当前脚本步骤。
    --include-harness) include_harness=1; shift ;;
    # 执行当前脚本步骤。
    --ollama-model) ollama_model="${2:-}"; shift 2 ;;
    # 执行当前脚本步骤。
    --ollama-embedding-model) ollama_embedding_model="${2:-}"; shift 2 ;;
    # 执行当前脚本步骤。
    --skip-ollama-model) skip_ollama_model=1; shift ;;
    # 执行当前脚本步骤。
    --skip-docker-runtime) skip_docker_runtime=1; shift ;;
    # 执行当前脚本步骤。
    --target-os) target_os="${2:?缺少目标系统}"; shift 2 ;;
    # 执行当前脚本步骤。
    --docker-packages-dir) docker_packages_dir="${2:?缺少系统依赖包目录}"; shift 2 ;;
    # 执行当前脚本步骤。
    --full) full=1; shift ;;
    # 执行当前脚本步骤。
    -h|--help) usage; exit 0 ;;
    # 执行当前脚本步骤。
    *) die "未知参数：$1；使用 --help 查看帮助" ;;
  # 执行当前脚本步骤。
  esac
# 结束当前控制块。
done

# 判断条件后执行对应操作。
if (( full )); then
  # 执行当前脚本步骤。
  include_ai=1
  # 执行当前脚本步骤。
  include_harness=1
# 结束当前控制块。
fi

# 执行当前脚本步骤。
[[ "$ollama_embedding_model" == nomic-embed-text ]] || die "当前知识库使用 nomic-embed-text，嵌入模型必须与其一致"
# 判断条件后执行对应操作。
if (( include_ai )); then
  # 执行当前脚本步骤。
  [[ "$ollama_model" =~ ^[A-Za-z0-9][A-Za-z0-9._:/-]*$ ]] || die "Ollama 模型名称无效"
# 结束当前控制块。
fi
# 执行当前脚本步骤。
command -v docker >/dev/null 2>&1 || die "找不到 docker 命令"
# 执行当前脚本步骤。
docker info >/dev/null || die "Docker Engine 不可用，请先启动 Docker"

# 判断条件后执行对应操作。
if [[ -n "$env_file" && "$env_file" != /* ]]; then
  # 执行当前脚本步骤。
  env_file="$project_root/$env_file"
# 结束当前控制块。
fi
# 判断条件后执行对应操作。
if [[ "$output_dir" = /* ]]; then
  # 执行当前脚本步骤。
  output_parent="$output_dir"
# 执行当前脚本步骤。
else
  # 执行当前脚本步骤。
  output_parent="$project_root/$output_dir"
# 结束当前控制块。
fi
# 执行当前脚本步骤。
mkdir -p "$output_parent"
# 执行当前脚本步骤。
case "$target_os" in generic|openeuler-24.03-lts-sp4) ;; *) die "不支持的 target-os：$target_os";; esac
# 判断条件后执行对应操作。
if [ "$target_os" != generic ] && { (( skip_docker_runtime )) || [ -n "$docker_packages_dir" ]; }; then
  # 执行当前脚本步骤。
  die '专用系统包不能与 skip-docker-runtime 或 docker-packages-dir 同时使用'
# 结束当前控制块。
fi
# 执行当前脚本步骤。
bundle_root="$output_parent/iot-platform-offline-$(date +%Y%m%d-%H%M%S)-$(random_hex 3)"
# 执行当前脚本步骤。
mkdir -p "$bundle_root"

# 执行当前脚本步骤。
generated_credentials="$(write_env "$bundle_root/.env.offline")"
# 执行当前脚本步骤。
runtime_provider="$(env_value IOT_AI_PROVIDER "$bundle_root/.env.offline")"
# 执行当前脚本步骤。
runtime_ollama_url="$(env_value IOT_OLLAMA_URL "$bundle_root/.env.offline")"
# 执行当前脚本步骤。
runtime_harness_enabled="$(env_value IOT_AI_HARNESS_ENABLED "$bundle_root/.env.offline")"
# 执行当前脚本步骤。
[[ "$runtime_harness_enabled" != true ]] || include_harness=1
# 判断条件后执行对应操作。
if [[ "$runtime_provider" == ollama || ( -z "$runtime_provider" && -n "$runtime_ollama_url" ) ]]; then
  # 执行当前脚本步骤。
  include_ai=1
  # 执行当前脚本步骤。
  configured_model="$(env_value IOT_AI_MODEL "$bundle_root/.env.offline")"
  # 判断条件后执行对应操作。
  if [[ -z "$configured_model" ]]; then configured_model="$(env_value IOT_OLLAMA_MODEL "$bundle_root/.env.offline")"; fi
  # 执行当前脚本步骤。
  ollama_model="${configured_model:-$ollama_model}"
# 结束当前控制块。
fi
# 判断条件后执行对应操作。
if (( include_ai )); then [[ "$ollama_model" =~ ^[A-Za-z0-9][A-Za-z0-9._:/-]*$ ]] || die "配置中的 Ollama 模型名称无效"; fi
# 执行当前脚本步骤。
compose=(docker compose --project-name iot-platform-offline-build --env-file "$bundle_root/.env.offline"
  # 执行当前脚本步骤。
  -f "$project_root/compose.yaml" -f "$project_root/compose.offline.yaml")
# 执行当前脚本步骤。
run_compose() {
  # 执行当前脚本步骤。
  echo "> docker compose $*" >&2
  # 执行当前脚本步骤。
  "${compose[@]}" "$@"
# 结束当前控制块。
}
# 执行当前脚本步骤。
ollama_started=0
# 执行当前脚本步骤。
cleanup() {
  # 判断条件后执行对应操作。
  if (( ollama_started )); then
    # 执行当前脚本步骤。
    "${compose[@]}" stop ollama >/dev/null 2>&1 || echo "打包用 Ollama 未能停止，请检查 iot-platform-offline-build 项目。" >&2
  # 结束当前控制块。
  fi
# 结束当前控制块。
}
# 执行当前脚本步骤。
trap cleanup EXIT

# 执行当前脚本步骤。
profiles=()
# 执行当前脚本步骤。
compose_profile_args=()
# 执行当前脚本步骤。
add_profile() {
  # 执行当前脚本步骤。
  profiles+=("$1")
  # 执行当前脚本步骤。
  compose_profile_args+=(--profile "$1")
# 结束当前控制块。
}
# 执行当前脚本步骤。
(( include_harness )) && add_profile harness

# 执行当前脚本步骤。
run_compose "${compose_profile_args[@]}" config --quiet
# 执行当前脚本步骤。
pull_services=(
  # 执行当前脚本步骤。
  postgres postgres-wal-init redis minio minio-dr redpanda redpanda-init
  # 执行当前脚本步骤。
  clickhouse emqx prometheus grafana loki ollama weaviate
# 执行当前脚本步骤。
)
# 执行当前脚本步骤。
run_compose pull "${pull_services[@]}"
# 执行当前脚本步骤。
run_compose build --pull platform-api platform-web backup-service

# 执行当前脚本步骤。
ollama_archive=""
# 执行当前脚本步骤。
ollama_volume_name=""
# 判断条件后执行对应操作。
if (( ! skip_ollama_model )); then
  # 执行当前脚本步骤。
  ollama_started=1
  # 执行当前脚本步骤。
  run_compose up -d --no-deps ollama
  # 执行当前脚本步骤。
  ollama_ready=0
  # 遍历数据并执行循环体。
  for _ in $(seq 1 30); do
    # 判断条件后执行对应操作。
    if "${compose[@]}" exec -T ollama ollama list >/dev/null 2>&1; then
      # 执行当前脚本步骤。
      ollama_ready=1
      # 执行当前脚本步骤。
      break
    # 结束当前控制块。
    fi
    # 执行当前脚本步骤。
    sleep 2
  # 结束当前控制块。
  done
  # 执行当前脚本步骤。
  (( ollama_ready )) || die "Ollama 容器未在规定时间内就绪"
  # 执行当前脚本步骤。
  run_compose exec -T ollama ollama pull "$ollama_embedding_model"
  # 判断条件后执行对应操作。
  if (( include_ai )) && [[ "$ollama_model" != "$ollama_embedding_model" ]]; then
    # 执行当前脚本步骤。
    run_compose exec -T ollama ollama pull "$ollama_model"
  # 结束当前控制块。
  fi
  # 执行当前脚本步骤。
  ollama_volume_name="iot-platform_ollama-data"
  run_docker run --rm --pull never \
    --mount "type=volume,source=iot-platform-offline-build_ollama-data,target=/src,readonly" \
    --mount "type=bind,source=$bundle_root,target=/backup" \
    alpine:3.22 sh -ec 'tar -czf /backup/ollama-data.tgz -C /src models'
  # 执行当前脚本步骤。
  ollama_archive="ollama-data.tgz"
  # 执行当前脚本步骤。
  model_hash="$(sha256_file "$bundle_root/$ollama_archive")"
  # 执行当前脚本步骤。
  printf '%s  %s\n' "$model_hash" "$ollama_archive" > "$bundle_root/ollama-data.tgz.sha256"
# 执行当前脚本步骤。
else
  # 执行当前脚本步骤。
  echo "已跳过模型打包：目标机必须预先具有 nomic-embed-text；否则知识库不可用。" >&2
# 结束当前控制块。
fi

# 判断条件后执行对应操作。
if (( include_harness )); then
  # 执行当前脚本步骤。
  command -v git >/dev/null 2>&1 || die "--include-harness 需要 Git"
  # 执行当前脚本步骤。
  sh "$script_dir/fetch-deepseek-harness.sh"
  # 执行当前脚本步骤。
  run_compose --profile harness build --pull deepseek-harness
# 结束当前控制块。
fi

# 执行当前脚本步骤。
mkdir -p "$bundle_root/scripts"
# 执行当前脚本步骤。
mkdir -p "$bundle_root/scripts/lib"
# 执行当前脚本步骤。
cp "$script_dir/lib/docker-bootstrap.sh" "$bundle_root/scripts/lib/"
# 执行当前脚本步骤。
cp "$script_dir/lib/restore-ollama-models.sh" "$bundle_root/scripts/lib/"
# 判断条件后执行对应操作。
if (( ! skip_docker_runtime )); then
  # 执行当前脚本步骤。
  runtime_arch="$(docker info --format '{{.Architecture}}')"
  # 执行当前脚本步骤。
  prepare_docker_runtime "$bundle_root/docker-runtime" "$runtime_arch"
  # 判断条件后执行对应操作。
  if [ "$target_os" = openeuler-24.03-lts-sp4 ]; then
    # 执行当前脚本步骤。
    prepare_openeuler_packages "$bundle_root/docker-runtime/packages" "$script_dir/lib/prepare-openeuler-packages.sh" "$runtime_arch"
  # 结束当前控制块。
  fi
  # 判断条件后执行对应操作。
  if [ -n "$docker_packages_dir" ]; then
    # 执行当前脚本步骤。
    [ -d "$docker_packages_dir" ] || die "系统依赖包目录不存在：$docker_packages_dir"
    # 执行当前脚本步骤。
    cp -R "$docker_packages_dir" "$bundle_root/docker-runtime/packages"
    for package in "$bundle_root/docker-runtime/packages/"*.rpm "$bundle_root/docker-runtime/packages/"*.deb \
      "$bundle_root/docker-runtime/packages/"RPM-GPG-KEY-* "$bundle_root/docker-runtime/packages/repodata/"*; do
      # 执行当前脚本步骤。
      [ -f "$package" ] || continue
      # 执行当前脚本步骤。
      case "$package" in *.sha256) continue;; esac
      # 执行当前脚本步骤。
      docker_runtime_hash "$package" > "$package.sha256"
    # 结束当前控制块。
    done
  # 结束当前控制块。
  fi
# 结束当前控制块。
fi
# 执行当前脚本步骤。
cp "$project_root/compose.yaml" "$bundle_root/"
# 执行当前脚本步骤。
cp "$project_root/compose.offline.yaml" "$bundle_root/"
# 执行当前脚本步骤。
cp "$project_root/compose.access.yaml" "$bundle_root/"
# 执行当前脚本步骤。
cp "$project_root/docs/EDGE_AND_GATEWAY.md" "$bundle_root/"
# 执行当前脚本步骤。
cp "$project_root/docs/DEPLOYMENT.md" "$bundle_root/"
# 执行当前脚本步骤。
cp -R "$project_root/deploy" "$bundle_root/"
# 执行当前脚本步骤。
cp "$project_root/docs/OFFLINE_DEPLOYMENT.md" "$bundle_root/"
# 遍历数据并执行循环体。
for script_name in deploy-offline.ps1 deploy-offline-windows.ps1 deploy-offline.sh deploy-offline-linux.sh deploy-offline-macos.sh; do
  # 执行当前脚本步骤。
  cp "$script_dir/$script_name" "$bundle_root/scripts/"
# 结束当前控制块。
done
# 判断条件后执行对应操作。
if [[ -n "$ollama_volume_name" ]]; then
  # 执行当前脚本步骤。
  printf '%s\n' "$ollama_volume_name" > "$bundle_root/ollama-volume.txt"
# 结束当前控制块。
fi

# 执行当前脚本步骤。
all_images_text="$("${compose[@]}" "${compose_profile_args[@]}" config --images | awk 'NF && !seen[$0]++' | sort -u)"
# 执行当前脚本步骤。
images=()
# 遍历数据并执行循环体。
while IFS= read -r image; do
  # 执行当前脚本步骤。
  [[ -n "$image" ]] && images+=("$image")
# 结束当前控制块。
done <<< "$all_images_text"
# 执行当前脚本步骤。
(( ${#images[@]} > 0 )) || die "没有解析出可导出的镜像"
# 遍历数据并执行循环体。
for image in "${images[@]}"; do
  # 执行当前脚本步骤。
  docker image inspect "$image" >/dev/null || die "镜像不存在，无法导出：$image"
# 结束当前控制块。
done

# 执行当前脚本步骤。
archive_path="$bundle_root/images.tar"
# 执行当前脚本步骤。
run_docker save -o "$archive_path" "${images[@]}"
# 执行当前脚本步骤。
archive_hash="$(sha256_file "$archive_path")"
# 执行当前脚本步骤。
printf '%s  images.tar\n' "$archive_hash" > "$bundle_root/images.tar.sha256"
# 执行当前脚本步骤。
printf '%s\n' "${profiles[@]}" > "$bundle_root/profiles.txt"

# 执行当前脚本步骤。
json_array() {
  # 执行当前脚本步骤。
  local separator=""
  # 执行当前脚本步骤。
  printf '['
  # 遍历数据并执行循环体。
  for value in "$@"; do
    # 执行当前脚本步骤。
    printf '%s"%s"' "$separator" "$value"
    # 执行当前脚本步骤。
    separator=,
  # 结束当前控制块。
  done
  # 执行当前脚本步骤。
  printf ']'
# 结束当前控制块。
}

# 执行当前脚本步骤。
commit="$(cd "$project_root" && git rev-parse HEAD 2>/dev/null || printf 'unknown')"
# 执行当前脚本步骤。
profiles_json="$(json_array "${profiles[@]}")"
# 执行当前脚本步骤。
images_json="$(json_array "${images[@]}")"
# 执行当前脚本步骤。
ollama_model_json="null"
# 执行当前脚本步骤。
ollama_archive_json="null"
# 执行当前脚本步骤。
ollama_volume_json="null"
# 判断条件后执行对应操作。
if [[ -n "$ollama_archive" ]]; then
  # 判断条件后执行对应操作。
  if (( include_ai )); then ollama_model_json="\"$ollama_model\""; fi
  # 执行当前脚本步骤。
  ollama_archive_json="\"$ollama_archive\""
  # 执行当前脚本步骤。
  ollama_volume_json="\"$ollama_volume_name\""
# 结束当前控制块。
fi
# 执行当前脚本步骤。
cat > "$bundle_root/manifest.json" <<EOF
{
  "format": 1,
  "project": "iot-platform",
  "targetOS": "$target_os",
  "createdAtUtc": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "gitCommit": "${commit//$'\n'/}",
  "profiles": $profiles_json,
  "images": $images_json,
  "imageArchive": "images.tar",
  "imageArchiveSha256": "$archive_hash",
  "envFile": ".env.offline",
  "composeFiles": ["compose.yaml", "compose.offline.yaml"],
  "ollamaModel": $ollama_model_json,
  "ollamaEmbeddingModel": $(if [[ -n "$ollama_archive" ]]; then printf '"%s"' "$ollama_embedding_model"; else printf 'null'; fi),
  "ollamaArchive": $ollama_archive_json,
  "ollamaVolume": $ollama_volume_json,
  "generatedCredentials": $([[ "$generated_credentials" = 1 ]] && echo true || echo false)
}
EOF

# 执行当前脚本步骤。
echo ""
# 执行当前脚本步骤。
echo "离线包已生成：$bundle_root"
# 执行当前脚本步骤。
echo "镜像数量：${#images[@]}"
# 执行当前脚本步骤。
du -h "$archive_path" | awk '{print "镜像包大小：" $1}'
# 执行当前脚本步骤。
echo "部署方式：按服务器系统运行 scripts/deploy-offline-windows.ps1、deploy-offline-macos.sh 或 deploy-offline-linux.sh"
# 判断条件后执行对应操作。
if [[ "$generated_credentials" = 1 ]]; then
  # 执行当前脚本步骤。
  echo "自动生成的凭据：$bundle_root/OFFLINE-CREDENTIALS.txt"
# 结束当前控制块。
fi
