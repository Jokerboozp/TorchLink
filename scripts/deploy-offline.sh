#!/usr/bin/env bash
set -Eeuo pipefail

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
bundle_dir="$(dirname -- "$script_dir")"
skip_hash_check=0
skip_health_check=0
die() { echo "错误：$*" >&2; exit 1; }
while [[ $# -gt 0 ]]; do
  case "$1" in
    --bundle-dir) [[ $# -ge 2 ]] || die "--bundle-dir 缺少目录"; bundle_dir="$2"; shift 2 ;;
    --skip-hash-check) skip_hash_check=1; shift ;;
    --skip-health-check) skip_health_check=1; shift ;;
    -h|--help) echo "用法：deploy-offline.sh [离线包目录] [--bundle-dir DIR] [--skip-hash-check] [--skip-health-check]"; exit 0 ;;
    -*) die "未知参数：$1" ;;
    *) bundle_dir="$1"; shift ;;
  esac
done
[[ -d "$bundle_dir" ]] || die "离线包目录不存在：$bundle_dir"
bundle_dir="$(CDPATH= cd -- "$bundle_dir" && pwd)"

env_file="$bundle_dir/.env.offline"
compose_file="$bundle_dir/compose.yaml"
offline_compose_file="$bundle_dir/compose.offline.yaml"
archive_file="$bundle_dir/images.tar"
hash_file="$bundle_dir/images.tar.sha256"
profiles_file="$bundle_dir/profiles.txt"
ollama_archive="$bundle_dir/ollama-data.tgz"
ollama_volume_file="$bundle_dir/ollama-volume.txt"
for file in "$env_file" "$compose_file" "$offline_compose_file" "$archive_file" "$hash_file"; do
  [[ -f "$file" ]] || die "离线包缺少文件：$file"
done
command -v docker >/dev/null 2>&1 || die "找不到 docker 命令，请先安装 Docker Engine/Desktop"
docker info >/dev/null || die "Docker Engine 不可用，请先启动 Docker"
if (( ! skip_health_check )); then
  command -v curl >/dev/null 2>&1 || die "健康检查需要 curl；请先安装，或显式使用 --skip-health-check"
fi

check_hash() {
  local archive="$1" hash="$2" actual expected
  [[ -f "$hash" ]] || die "离线包缺少校验文件：$hash"
  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$archive" | awk '{print tolower($1)}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$archive" | awk '{print tolower($1)}')"
  else
    die "找不到 sha256sum 或 shasum，无法校验归档"
  fi
  expected="$(awk 'NR == 1 {print tolower($1)}' "$hash" | tr -d '\r')"
  [[ "$actual" == "$expected" ]] || die "SHA256 校验失败：$archive"
}
if (( ! skip_hash_check )); then
  check_hash "$archive_file" "$hash_file"
  if [[ -f "$ollama_archive" ]]; then check_hash "$ollama_archive" "$bundle_dir/ollama-data.tgz.sha256"; fi
  echo "镜像和模型包 SHA256 校验通过。"
fi

compose=(docker compose --project-name iot-platform --env-file "$env_file" -f "$compose_file" -f "$offline_compose_file")
if [[ -f "$profiles_file" ]]; then
  while IFS= read -r profile || [[ -n "$profile" ]]; do
    profile="${profile%$'\r'}"
    [[ -n "$profile" ]] || continue
    case "$profile" in harness|thingspanel|gb26875) compose+=(--profile "$profile") ;; *) die "离线包包含未知 profile：$profile" ;; esac
  done < "$profiles_file"
fi
"${compose[@]}" config --quiet
docker load -i "$archive_file"
images="$("${compose[@]}" config --images | sort -u)"
[[ -n "$images" ]] || die "无法解析离线镜像清单"
while IFS= read -r image; do
  docker image inspect "$image" >/dev/null 2>&1 || die "离线包缺少镜像：$image。请在有网机器重新打包。"
done <<< "$images"

ollama_volume="iot-platform_ollama-data"
if [[ -f "$ollama_volume_file" ]]; then
  recorded_volume="$(head -n 1 "$ollama_volume_file" | tr -d '\r')"
  [[ "$recorded_volume" == "$ollama_volume" ]] || die "模型卷名称与部署项目不一致，请重新打包：$recorded_volume"
fi
if [[ -f "$ollama_archive" ]]; then
  docker volume create "$ollama_volume" >/dev/null
  docker run --rm --pull never \
    --mount "type=volume,source=$ollama_volume,target=/dst" \
    --mount "type=bind,source=$bundle_dir,target=/backup,readonly" \
    alpine:3.22 sh -ec 'mkdir -p /tmp/restore; tar -xzf /backup/ollama-data.tgz -C /tmp/restore; cp -an /tmp/restore/. /dst/'
  echo "Ollama 模型已恢复（保留已有文件）。"
fi
"${compose[@]}" up -d --no-build --pull never --wait --wait-timeout 180
"${compose[@]}" ps

env_value() {
  if printenv "$1" >/dev/null 2>&1; then printenv "$1"; return; fi
  awk -v key="$1" '
    $0 ~ "^[[:space:]]*" key "[[:space:]]*=" {
      sub("^[[:space:]]*" key "[[:space:]]*=[[:space:]]*", "")
      sub(/\r$/, ""); sub(/[[:space:]]+$/, "")
      if (($0 ~ /^".*"$/) || ($0 ~ /^\047.*\047$/)) $0=substr($0,2,length($0)-2)
      value=$0
    }
    END { print value }
  ' "$env_file"
}
if (( ! skip_health_check )); then
  api_port="$(env_value IOT_API_PORT)"
  health_url="http://127.0.0.1:${api_port:-8081}/health/ready"
  healthy=0
  for _ in $(seq 1 60); do
    if curl --fail --silent --show-error --max-time 3 "$health_url" >/dev/null 2>&1; then
      healthy=1
      break
    fi
    sleep 2
  done
  if (( ! healthy )); then
    "${compose[@]}" logs --tail=100 platform-api postgres redpanda emqx || true
    die "平台健康检查失败：$health_url"
  fi
  "${compose[@]}" exec -T ollama ollama show nomic-embed-text
  if [[ "$(env_value IOT_AI_PROVIDER)" == ollama ]]; then
    chat_model="$(env_value IOT_AI_MODEL)"
    if [[ -z "$chat_model" ]]; then chat_model="$(env_value IOT_OLLAMA_MODEL)"; fi
    "${compose[@]}" exec -T ollama ollama show "${chat_model:-qwen3:8b}"
  fi
  echo "平台健康检查与本地模型检查通过：$health_url"
  check_web_port="$(env_value IOT_WEB_PORT)"
  check_backup_port="$(env_value IOT_BACKUP_HTTP_PORT)"
  for url in "http://127.0.0.1:${check_web_port:-8080}/" "http://127.0.0.1:${check_web_port:-8080}/health/ready" "http://127.0.0.1:${check_backup_port:-8092}/health/live"; do
    ready=0
    for _ in $(seq 1 60); do
      if curl --fail --silent --show-error --max-time 3 "$url" >/dev/null 2>&1; then ready=1; break; fi
      sleep 2
    done
    (( ready )) || die "服务健康检查失败：$url"
    echo "健康检查通过：$url"
  done
fi
web_port="$(env_value IOT_WEB_PORT)"
echo "离线部署完成。Web 地址：http://127.0.0.1:${web_port:-8080}"
echo "管理员凭据：$bundle_dir/OFFLINE-CREDENTIALS.txt"
