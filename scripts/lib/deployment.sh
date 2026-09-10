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
  local env_path="$1"; shift
  if [ -f "$env_path" ]; then
    printf '保留已有配置：%s\n' "$env_path"
    return
  fi
  [ ! -e "$env_path" ] || { printf '配置路径不是文件：%s\n' "$env_path" >&2; return 1; }
  local defaults key value
  defaults="$(cat <<EOF
POSTGRES_PASSWORD=$(deployment_secret)
REDIS_PASSWORD=$(deployment_secret)
CLICKHOUSE_PASSWORD=$(deployment_secret)
MINIO_ROOT_USER=iotadmin
MINIO_ROOT_PASSWORD=$(deployment_secret)
MINIO_DR_ROOT_USER=iotdradmin
MINIO_DR_ROOT_PASSWORD=$(deployment_secret)
EMQX_DASHBOARD_USER=admin
EMQX_DASHBOARD_PASSWORD=$(deployment_secret)
GRAFANA_ADMIN_USER=admin
GRAFANA_ADMIN_PASSWORD=$(deployment_secret)
IOT_JWT_SECRET=$(deployment_secret)
IOT_ADMIN_USER=admin
IOT_ADMIN_PASSWORD=admin123
IOT_ADMIN_TENANTS=tenant_001
IOT_VIDEO_PLATFORM_SECRETS=video-platform-1:$(deployment_secret)
IOT_AI_HARNESS_TOKEN=$(deployment_secret)
IOT_BACKUP_ADMIN_TOKEN=$(deployment_secret)
GB26875_CONTROL_TOKEN=$(deployment_secret)
IOT_HTTP_ADDR=:8081
IOT_WEB_PORT=8080
IOT_API_PORT=8081
IOT_CORS_ALLOWED_ORIGINS=http://localhost:8080,http://127.0.0.1:8080,http://localhost:5173,http://127.0.0.1:5173
IOT_OLLAMA_MODEL=qwen3:1.7b
IOT_AI_PROVIDER=ollama
IOT_AI_BASE_URL=http://ollama:11434
IOT_AI_MODEL=qwen3:1.7b
IOT_AI_API_KEY=
DEEPSEEK_API_KEY=
DEEPSEEK_BASE_URL=https://api.deepseek.com
IOT_AI_HARNESS_ENABLED=true
IOT_AI_HARNESS_URL=http://deepseek-harness:8091
IOT_AI_HARNESS_MCP_URL=http://platform-api:8080/mcp/harness
IOT_AI_HARNESS_PROVIDER=ollama
IOT_AI_HARNESS_OLLAMA_BASE_URL=http://ollama:11434/v1
IOT_AI_HARNESS_CONTEXT_WINDOW=8192
IOT_AI_HARNESS_MODEL=qwen3:1.7b
IOT_BACKUP_TIME=00:05
IOT_BACKUP_ENABLED=true
IOT_BACKUP_TIMEZONE=Asia/Shanghai
EOF
)"
  for value in "$@"; do
    key="${value%%=*}"
    if [[ ! "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || [[ "$value" != *=* ]] || [[ "$value" == *$'\n'* || "$value" == *$'\r'* ]]; then
      echo '默认环境变量名称或值包含不支持的字符。' >&2
      return 1
    fi
    defaults="$(printf '%s\n' "$defaults" | awk -F= -v key="$key" '$1 != key')"$'\n'"$value"
  done
  mkdir -p -- "$(dirname -- "$env_path")"
  # noclobber protects existing files, including concurrent script invocations.
  (umask 077; set -o noclobber; {
    printf '# 此配置只在首次部署时生成，后续运行不会轮换凭据。\n'
    printf '# 文件包含敏感凭据，请勿提交到 Git 或公开分享。\n'
    printf '%s\n' "$defaults"
  } > "$env_path") || return 1
  printf '已生成配置：%s（随机凭据仅保存在文件中）。\n' "$env_path"
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
  # Only used for explicit feature settings; credentials are never changed.
  updated="$(awk -v key="$key" -v value="$value" '
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
  local url="$1" timeout="${2:-180}" deadline=$((SECONDS + ${2:-180}))
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
