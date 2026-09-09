#!/usr/bin/env bash
set -Eeuo pipefail

umask 077
script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
project_root="$(CDPATH= cd -- "$script_dir/.." && pwd)"
source "$script_dir/lib/env-comments.sh"
source "$script_dir/lib/docker-bootstrap.sh"

output_dir="offline-bundles"
env_file=""
include_ai=1
include_harness=1
full=0
ollama_model="qwen3:1.7b"
ollama_embedding_model="nomic-embed-text"
skip_ollama_model=0
skip_docker_runtime=0
docker_packages_dir=""

usage() {
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
  --docker-packages-dir DIR  可选：精简 Linux 系统缺少的 iptables/xz/procps 及依赖 RPM/DEB 目录
  --full                 兼容参数；AI 与 Harness 已默认启用
  -h, --help             显示帮助
EOF
}

die() {
  echo "错误：$*" >&2
  exit 1
}

run_docker() {
  echo "> docker $*" >&2
  docker "$@"
}

random_hex() {
  local bytes="${1:-24}"
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex "$bytes" | tr -d '\r\n'
  else
    od -An -N "$bytes" -tx1 /dev/urandom | tr -d ' \r\n'
  fi
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print tolower($1)}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print tolower($1)}'
  else
    die "找不到 sha256sum 或 shasum，无法校验镜像包"
  fi
}

env_value() {
  local key="$1"
  local file="$2"
  awk -v key="$key" '
    $0 ~ "^[[:space:]]*" key "[[:space:]]*=" {
      sub("^[[:space:]]*" key "[[:space:]]*=[[:space:]]*", "")
      sub(/\r$/, ""); sub(/[[:space:]]+$/, "")
      if (($0 ~ /^".*"$/) || ($0 ~ /^\047.*\047$/)) $0=substr($0,2,length($0)-2)
      value=$0
    }
    END { print value }
  ' "$file"
}

set_env_value() {
  local file="$1"
  local key="$2"
  local value="$3"
  local tmp
  tmp="$(mktemp "${file}.tmp.XXXXXX")"
  awk -v key="$key" -v value="$value" '
    BEGIN { replaced = 0 }
    $0 ~ "^[[:space:]]*" key "[[:space:]]*=" {
      if (!replaced) { print key "=" value; replaced = 1 }
      next
    }
    { print }
    END { if (!replaced) print key "=" value }
  ' "$file" > "$tmp"
  mv "$tmp" "$file"
}

validate_env() {
  local file="$1"
  local key value
  local required_keys=(
    POSTGRES_PASSWORD REDIS_PASSWORD CLICKHOUSE_PASSWORD
    MINIO_ROOT_PASSWORD MINIO_DR_ROOT_PASSWORD IOT_JWT_SECRET
    IOT_ADMIN_USER IOT_ADMIN_PASSWORD IOT_ADMIN_TENANTS
    IOT_VIDEO_PLATFORM_SECRETS IOT_BACKUP_ADMIN_TOKEN
    EMQX_DASHBOARD_USER EMQX_DASHBOARD_PASSWORD
    GRAFANA_ADMIN_USER GRAFANA_ADMIN_PASSWORD
  )
  (( include_harness )) && required_keys+=(IOT_AI_HARNESS_TOKEN)
  for key in "${required_keys[@]}"; do
    value="$(env_value "$key" "$file")"
    [[ -n "${value//[[:space:]]/}" ]] || die "EnvFile 缺少必填安全配置：$key"
  done
  if grep -Eq '^[A-Za-z_][A-Za-z0-9_]*=.*(change-this|local-iot-|admin123|public-change-me|change-me)' "$file"; then
    die "EnvFile 仍包含示例密码或默认密钥，请先替换后再打包"
  fi
}

write_env() {
  local destination="$1"
  local generated=0
  if [[ -n "$env_file" ]]; then
    [[ -f "$env_file" ]] || die "指定的 EnvFile 不存在：$env_file"
    cp "$env_file" "$destination"
  else
    generated=1
    local postgres_password="pg-$(random_hex 18)"
    local redis_password="redis-$(random_hex 18)"
    local clickhouse_password="ch-$(random_hex 18)"
    local minio_password="minio-$(random_hex 18)"
    local minio_dr_password="minio-dr-$(random_hex 18)"
    local jwt_secret="$(random_hex 32)"
    local admin_password="Admin-$(random_hex 12)"
    local video_secret="$(random_hex 24)"
    local harness_token="$(random_hex 32)"
    local backup_token="$(random_hex 32)"
    local emqx_password="Emqx-$(random_hex 12)"
    local grafana_password="Grafana-$(random_hex 12)"
    local ollama_url="http://ollama:11434"
    local ai_provider="ollama"
    local weaviate_url="http://weaviate:8080"
    local harness_url="http://deepseek-harness:8091"
    local harness_enabled="true"
    if (( include_ai )); then
      ollama_url="http://ollama:11434"
      ai_provider="ollama"
      weaviate_url="http://weaviate:8080"
    fi
    if (( include_harness )); then
      harness_url="http://deepseek-harness:8091"
      harness_enabled="true"
    fi
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
IOT_AI_PROVIDER_TEST_ALLOWED_ORIGINS=http://ollama:11434
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
IOT_PROTOCOL_CATALOG_POLICY=
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
  fi

  set_env_value "$destination" IOT_PLATFORM_API_IMAGE iot-platform-api:offline
  set_env_value "$destination" IOT_PLATFORM_WEB_IMAGE iot-platform-web:offline
  set_env_value "$destination" IOT_BACKUP_IMAGE iot-platform-backup:offline
  set_env_value "$destination" IOT_DEEPSEEK_HARNESS_IMAGE iot-deepseek-harness:offline
  if (( include_harness )); then
    set_env_value "$destination" IOT_AI_HARNESS_ENABLED true
  elif [[ -z "$(env_value IOT_AI_HARNESS_ENABLED "$destination")" ]]; then
    set_env_value "$destination" IOT_AI_HARNESS_ENABLED false
  fi
  if [[ "$(env_value IOT_AI_PROVIDER "$destination")" == ollama ]]; then
    local selected_model
    selected_model="$(env_value IOT_AI_MODEL "$destination")"
    [[ -n "$selected_model" ]] || selected_model="$(env_value IOT_OLLAMA_MODEL "$destination")"
    selected_model="${selected_model:-$ollama_model}"
    [[ "$selected_model" != qwen3:8b || "$ollama_model" == qwen3:8b ]] || selected_model="$ollama_model"
    set_env_value "$destination" IOT_OLLAMA_URL http://ollama:11434
    set_env_value "$destination" IOT_OLLAMA_MODEL "$selected_model"
    set_env_value "$destination" IOT_AI_BASE_URL http://ollama:11434
    set_env_value "$destination" IOT_AI_MODEL "$selected_model"
    set_env_value "$destination" IOT_AI_HARNESS_PROVIDER ollama
    set_env_value "$destination" IOT_AI_HARNESS_OLLAMA_BASE_URL http://ollama:11434/v1
    set_env_value "$destination" IOT_AI_HARNESS_CONTEXT_WINDOW 8192
    set_env_value "$destination" IOT_AI_HARNESS_MODEL "$selected_model"
  fi
  annotate_deployment_env_file "$destination"

  if (( ! generated )); then
    cat > "$(dirname -- "$destination")/OFFLINE-CREDENTIALS.txt" <<EOF
凭据来自外部 EnvFile：$env_file
本文件不复制外部 EnvFile 的内容，请单独保管原始凭据。
EOF
  fi
  validate_env "$destination"
  printf '%s\n' "$generated"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --output-dir) output_dir="${2:-}"; shift 2 ;;
    --env-file) env_file="${2:-}"; shift 2 ;;
    --include-ai) include_ai=1; shift ;;
    --include-harness) include_harness=1; shift ;;
    --ollama-model) ollama_model="${2:-}"; shift 2 ;;
    --ollama-embedding-model) ollama_embedding_model="${2:-}"; shift 2 ;;
    --skip-ollama-model) skip_ollama_model=1; shift ;;
    --skip-docker-runtime) skip_docker_runtime=1; shift ;;
    --docker-packages-dir) docker_packages_dir="${2:?缺少系统依赖包目录}"; shift 2 ;;
    --full) full=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "未知参数：$1；使用 --help 查看帮助" ;;
  esac
done

if (( full )); then
  include_ai=1
  include_harness=1
fi

[[ "$ollama_embedding_model" == nomic-embed-text ]] || die "当前知识库使用 nomic-embed-text，嵌入模型必须与其一致"
if (( include_ai )); then
  [[ "$ollama_model" =~ ^[A-Za-z0-9][A-Za-z0-9._:/-]*$ ]] || die "Ollama 模型名称无效"
fi
command -v docker >/dev/null 2>&1 || die "找不到 docker 命令"
docker info >/dev/null || die "Docker Engine 不可用，请先启动 Docker"

if [[ -n "$env_file" && "$env_file" != /* ]]; then
  env_file="$project_root/$env_file"
fi
if [[ "$output_dir" = /* ]]; then
  output_parent="$output_dir"
else
  output_parent="$project_root/$output_dir"
fi
mkdir -p "$output_parent"
bundle_root="$output_parent/iot-platform-offline-$(date +%Y%m%d-%H%M%S)-$(random_hex 3)"
mkdir -p "$bundle_root"

generated_credentials="$(write_env "$bundle_root/.env.offline")"
runtime_provider="$(env_value IOT_AI_PROVIDER "$bundle_root/.env.offline")"
runtime_ollama_url="$(env_value IOT_OLLAMA_URL "$bundle_root/.env.offline")"
runtime_harness_enabled="$(env_value IOT_AI_HARNESS_ENABLED "$bundle_root/.env.offline")"
[[ "$runtime_harness_enabled" != true ]] || include_harness=1
if [[ "$runtime_provider" == ollama || ( -z "$runtime_provider" && -n "$runtime_ollama_url" ) ]]; then
  include_ai=1
  configured_model="$(env_value IOT_AI_MODEL "$bundle_root/.env.offline")"
  if [[ -z "$configured_model" ]]; then configured_model="$(env_value IOT_OLLAMA_MODEL "$bundle_root/.env.offline")"; fi
  ollama_model="${configured_model:-$ollama_model}"
fi
if (( include_ai )); then [[ "$ollama_model" =~ ^[A-Za-z0-9][A-Za-z0-9._:/-]*$ ]] || die "配置中的 Ollama 模型名称无效"; fi
compose=(docker compose --project-name iot-platform-offline-build --env-file "$bundle_root/.env.offline"
  -f "$project_root/compose.yaml" -f "$project_root/compose.offline.yaml")
run_compose() {
  echo "> docker compose $*" >&2
  "${compose[@]}" "$@"
}
ollama_started=0
cleanup() {
  if (( ollama_started )); then
    "${compose[@]}" stop ollama >/dev/null 2>&1 || echo "打包用 Ollama 未能停止，请检查 iot-platform-offline-build 项目。" >&2
  fi
}
trap cleanup EXIT

profiles=()
compose_profile_args=()
add_profile() {
  profiles+=("$1")
  compose_profile_args+=(--profile "$1")
}
(( include_harness )) && add_profile harness

run_compose "${compose_profile_args[@]}" config --quiet
pull_services=(
  postgres postgres-wal-init redis minio minio-dr redpanda redpanda-init
  clickhouse emqx prometheus grafana loki ollama weaviate
)
run_compose pull "${pull_services[@]}"
run_compose build --pull platform-api platform-web backup-service

ollama_archive=""
ollama_volume_name=""
if (( ! skip_ollama_model )); then
  ollama_started=1
  run_compose up -d --no-deps ollama
  ollama_ready=0
  for _ in $(seq 1 30); do
    if "${compose[@]}" exec -T ollama ollama list >/dev/null 2>&1; then
      ollama_ready=1
      break
    fi
    sleep 2
  done
  (( ollama_ready )) || die "Ollama 容器未在规定时间内就绪"
  run_compose exec -T ollama ollama pull "$ollama_embedding_model"
  if (( include_ai )) && [[ "$ollama_model" != "$ollama_embedding_model" ]]; then
    run_compose exec -T ollama ollama pull "$ollama_model"
  fi
  ollama_volume_name="iot-platform_ollama-data"
  run_docker run --rm --pull never \
    --mount "type=volume,source=iot-platform-offline-build_ollama-data,target=/src,readonly" \
    --mount "type=bind,source=$bundle_root,target=/backup" \
    alpine:3.22 sh -ec 'tar -czf /backup/ollama-data.tgz -C /src models'
  ollama_archive="ollama-data.tgz"
  model_hash="$(sha256_file "$bundle_root/$ollama_archive")"
  printf '%s  %s\n' "$model_hash" "$ollama_archive" > "$bundle_root/ollama-data.tgz.sha256"
else
  echo "已跳过模型打包：目标机必须预先具有 nomic-embed-text；否则知识库不可用。" >&2
fi

if (( include_harness )); then
  command -v git >/dev/null 2>&1 || die "--include-harness 需要 Git"
  sh "$script_dir/fetch-deepseek-harness.sh"
  run_compose --profile harness build --pull deepseek-harness
fi

mkdir -p "$bundle_root/scripts"
mkdir -p "$bundle_root/scripts/lib"
cp "$script_dir/lib/docker-bootstrap.sh" "$bundle_root/scripts/lib/"
if (( ! skip_docker_runtime )); then
  runtime_arch="$(docker info --format '{{.Architecture}}')"
  prepare_docker_runtime "$bundle_root/docker-runtime" "$runtime_arch"
  if [ -n "$docker_packages_dir" ]; then
    [ -d "$docker_packages_dir" ] || die "系统依赖包目录不存在：$docker_packages_dir"
    cp -R "$docker_packages_dir" "$bundle_root/docker-runtime/packages"
    for package in "$bundle_root/docker-runtime/packages/"*.rpm "$bundle_root/docker-runtime/packages/"*.deb; do
      [ -f "$package" ] || continue
      docker_runtime_hash "$package" > "$package.sha256"
    done
  fi
fi
cp "$project_root/compose.yaml" "$bundle_root/"
cp "$project_root/compose.offline.yaml" "$bundle_root/"
cp "$project_root/compose.access.yaml" "$bundle_root/"
cp "$project_root/docs/EDGE_AND_GATEWAY.md" "$bundle_root/"
cp -R "$project_root/deploy" "$bundle_root/"
cp "$project_root/docs/OFFLINE_DEPLOYMENT.md" "$bundle_root/"
for script_name in deploy-offline.ps1 deploy-offline-windows.ps1 deploy-offline.sh deploy-offline-linux.sh deploy-offline-macos.sh; do
  cp "$script_dir/$script_name" "$bundle_root/scripts/"
done
if [[ -n "$ollama_volume_name" ]]; then
  printf '%s\n' "$ollama_volume_name" > "$bundle_root/ollama-volume.txt"
fi

all_images_text="$("${compose[@]}" "${compose_profile_args[@]}" config --images | awk 'NF && !seen[$0]++' | sort -u)"
images=()
while IFS= read -r image; do
  [[ -n "$image" ]] && images+=("$image")
done <<< "$all_images_text"
(( ${#images[@]} > 0 )) || die "没有解析出可导出的镜像"
for image in "${images[@]}"; do
  docker image inspect "$image" >/dev/null || die "镜像不存在，无法导出：$image"
done

archive_path="$bundle_root/images.tar"
run_docker save -o "$archive_path" "${images[@]}"
archive_hash="$(sha256_file "$archive_path")"
printf '%s  images.tar\n' "$archive_hash" > "$bundle_root/images.tar.sha256"
printf '%s\n' "${profiles[@]}" > "$bundle_root/profiles.txt"

json_array() {
  local separator=""
  printf '['
  for value in "$@"; do
    printf '%s"%s"' "$separator" "$value"
    separator=,
  done
  printf ']'
}

commit="$(cd "$project_root" && git rev-parse HEAD 2>/dev/null || printf 'unknown')"
profiles_json="$(json_array "${profiles[@]}")"
images_json="$(json_array "${images[@]}")"
ollama_model_json="null"
ollama_archive_json="null"
ollama_volume_json="null"
if [[ -n "$ollama_archive" ]]; then
  if (( include_ai )); then ollama_model_json="\"$ollama_model\""; fi
  ollama_archive_json="\"$ollama_archive\""
  ollama_volume_json="\"$ollama_volume_name\""
fi
cat > "$bundle_root/manifest.json" <<EOF
{
  "format": 1,
  "project": "iot-platform",
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

echo ""
echo "离线包已生成：$bundle_root"
echo "镜像数量：${#images[@]}"
du -h "$archive_path" | awk '{print "镜像包大小：" $1}'
echo "部署方式：按服务器系统运行 scripts/deploy-offline-windows.ps1、deploy-offline-macos.sh 或 deploy-offline-linux.sh"
if [[ "$generated_credentials" = 1 ]]; then
  echo "自动生成的凭据：$bundle_root/OFFLINE-CREDENTIALS.txt"
fi
