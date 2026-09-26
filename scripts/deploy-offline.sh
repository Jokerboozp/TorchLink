#!/usr/bin/env bash
# 执行当前脚本步骤。
set -Eeuo pipefail

# 执行当前脚本步骤。
script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
# 执行当前脚本步骤。
bundle_dir="$(dirname -- "$script_dir")"
# 执行当前脚本步骤。
skip_hash_check=0
# 执行当前脚本步骤。
skip_health_check=0
# 执行当前脚本步骤。
die() { echo "错误：$*" >&2; exit 1; }
# 遍历数据并执行循环体。
while [[ $# -gt 0 ]]; do
  # 执行当前脚本步骤。
  case "$1" in
    # 执行当前脚本步骤。
    --bundle-dir) [[ $# -ge 2 ]] || die "--bundle-dir 缺少目录"; bundle_dir="$2"; shift 2 ;;
    # 执行当前脚本步骤。
    --skip-hash-check) skip_hash_check=1; shift ;;
    # 执行当前脚本步骤。
    --skip-health-check) skip_health_check=1; shift ;;
    # 执行当前脚本步骤。
    -h|--help) echo "用法：deploy-offline.sh [离线包目录] [--bundle-dir DIR] [--skip-hash-check] [--skip-health-check]"; exit 0 ;;
    # 执行当前脚本步骤。
    -*) die "未知参数：$1" ;;
    # 执行当前脚本步骤。
    *) bundle_dir="$1"; shift ;;
  # 执行当前脚本步骤。
  esac
# 结束当前控制块。
done
# 执行当前脚本步骤。
[[ -d "$bundle_dir" ]] || die "离线包目录不存在：$bundle_dir"
# 执行当前脚本步骤。
bundle_dir="$(CDPATH= cd -- "$bundle_dir" && pwd)"

# 执行当前脚本步骤。
env_file="$bundle_dir/.env.offline"
# 执行当前脚本步骤。
compose_file="$bundle_dir/compose.yaml"
# 执行当前脚本步骤。
offline_compose_file="$bundle_dir/compose.offline.yaml"
# 执行当前脚本步骤。
archive_file="$bundle_dir/images.tar"
# 执行当前脚本步骤。
hash_file="$bundle_dir/images.tar.sha256"
# 执行当前脚本步骤。
profiles_file="$bundle_dir/profiles.txt"
# 执行当前脚本步骤。
ollama_archive="$bundle_dir/ollama-data.tgz"
# 执行当前脚本步骤。
ollama_volume_file="$bundle_dir/ollama-volume.txt"
# 遍历数据并执行循环体。
for file in "$env_file" "$compose_file" "$offline_compose_file" "$archive_file" "$hash_file"; do
  # 执行当前脚本步骤。
  [[ -f "$file" ]] || die "离线包缺少文件：$file"
# 结束当前控制块。
done
# 执行当前脚本步骤。
source "$script_dir/lib/docker-bootstrap.sh"
# 执行当前脚本步骤。
ensure_deployment_docker offline "$bundle_dir/docker-runtime"
# 判断条件后执行对应操作。
if (( ! skip_health_check )); then
  # 执行当前脚本步骤。
  command -v curl >/dev/null 2>&1 || die "健康检查需要 curl；请先安装，或显式使用 --skip-health-check"
# 结束当前控制块。
fi

# 执行当前脚本步骤。
check_hash() {
  # 执行当前脚本步骤。
  local archive="$1" hash="$2" actual expected
  # 执行当前脚本步骤。
  [[ -f "$hash" ]] || die "离线包缺少校验文件：$hash"
  # 判断条件后执行对应操作。
  if command -v sha256sum >/dev/null 2>&1; then
    # 执行当前脚本步骤。
    actual="$(sha256sum "$archive" | awk '{print tolower($1)}')"
  # 执行当前脚本步骤。
  elif command -v shasum >/dev/null 2>&1; then
    # 执行当前脚本步骤。
    actual="$(shasum -a 256 "$archive" | awk '{print tolower($1)}')"
  # 执行当前脚本步骤。
  else
    # 执行当前脚本步骤。
    die "找不到 sha256sum 或 shasum，无法校验归档"
  # 结束当前控制块。
  fi
  # 执行当前脚本步骤。
  expected="$(awk 'NR == 1 {print tolower($1)}' "$hash" | tr -d '\r')"
  # 执行当前脚本步骤。
  [[ "$actual" == "$expected" ]] || die "SHA256 校验失败：$archive"
# 结束当前控制块。
}
# 判断条件后执行对应操作。
if (( ! skip_hash_check )); then
  # 执行当前脚本步骤。
  check_hash "$archive_file" "$hash_file"
  # 判断条件后执行对应操作。
  if [[ -f "$ollama_archive" ]]; then check_hash "$ollama_archive" "$bundle_dir/ollama-data.tgz.sha256"; fi
  # 执行当前脚本步骤。
  echo "镜像和模型包 SHA256 校验通过。"
# 结束当前控制块。
fi

# 执行当前脚本步骤。
compose=(docker compose --project-name iot-platform --env-file "$env_file" -f "$compose_file" -f "$offline_compose_file")
# 判断条件后执行对应操作。
if [[ -f "$profiles_file" ]]; then
  # 遍历数据并执行循环体。
  while IFS= read -r profile || [[ -n "$profile" ]]; do
    # 执行当前脚本步骤。
    profile="${profile%$'\r'}"
    # 执行当前脚本步骤。
    [[ -n "$profile" ]] || continue
    # 执行当前脚本步骤。
    case "$profile" in harness|gb26875) compose+=(--profile "$profile") ;; *) die "离线包包含未知 profile：$profile" ;; esac
  # 结束当前控制块。
  done < "$profiles_file"
# 结束当前控制块。
fi
# 执行当前脚本步骤。
"${compose[@]}" config --quiet
# 执行当前脚本步骤。
docker load -i "$archive_file"
# 执行当前脚本步骤。
images="$("${compose[@]}" config --images | sort -u)"
# 执行当前脚本步骤。
[[ -n "$images" ]] || die "无法解析离线镜像清单"
# 遍历数据并执行循环体。
while IFS= read -r image; do
  # 执行当前脚本步骤。
  docker image inspect "$image" >/dev/null 2>&1 || die "离线包缺少镜像：${image}。请在有网机器重新打包。"
# 结束当前控制块。
done <<< "$images"

# 执行当前脚本步骤。
ollama_volume="iot-platform_ollama-data"
# 判断条件后执行对应操作。
if [[ -f "$ollama_volume_file" ]]; then
  # 执行当前脚本步骤。
  recorded_volume="$(head -n 1 "$ollama_volume_file" | tr -d '\r')"
  # 执行当前脚本步骤。
  [[ "$recorded_volume" == "$ollama_volume" ]] || die "模型卷名称与部署项目不一致，请重新打包：$recorded_volume"
# 结束当前控制块。
fi
# 判断条件后执行对应操作。
if [[ -f "$ollama_archive" ]]; then
  # 执行当前脚本步骤。
  docker volume create "$ollama_volume" >/dev/null
  docker run --rm --pull never \
    --mount "type=volume,source=$ollama_volume,target=/dst" \
    --mount "type=bind,source=$bundle_dir,target=/backup,readonly" \
    alpine:3.22 sh /backup/scripts/lib/restore-ollama-models.sh /backup/ollama-data.tgz /dst
  # 执行当前脚本步骤。
  echo "Ollama 模型已恢复（保留已有文件）。"
# 结束当前控制块。
fi
# 执行当前脚本步骤。
"${compose[@]}" up -d --no-build --pull never --wait --wait-timeout 180
# 执行当前脚本步骤。
"${compose[@]}" ps

# 执行当前脚本步骤。
env_value() {
  # 判断条件后执行对应操作。
  if printenv "$1" >/dev/null 2>&1; then printenv "$1"; return; fi
  # 执行当前脚本步骤。
  awk -v key="$1" '
    $0 ~ "^[[:space:]]*" key "[[:space:]]*=" {
      sub("^[[:space:]]*" key "[[:space:]]*=[[:space:]]*", "")
      sub(/\r$/, ""); sub(/[[:space:]]+$/, "")
      if (($0 ~ /^".*"$/) || ($0 ~ /^\047.*\047$/)) $0=substr($0,2,length($0)-2)
      value=$0
    }
    END { print value }
  ' "$env_file"
# 结束当前控制块。
}
# 判断条件后执行对应操作。
if (( ! skip_health_check )); then
  # 执行当前脚本步骤。
  api_port="$(env_value IOT_API_PORT)"
  # 执行当前脚本步骤。
  health_url="http://127.0.0.1:${api_port:-8081}/health/ready"
  # 执行当前脚本步骤。
  healthy=0
  # 遍历数据并执行循环体。
  for _ in $(seq 1 60); do
    # 判断条件后执行对应操作。
    if curl --fail --silent --show-error --max-time 3 "$health_url" >/dev/null 2>&1; then
      # 执行当前脚本步骤。
      healthy=1
      # 执行当前脚本步骤。
      break
    # 结束当前控制块。
    fi
    # 执行当前脚本步骤。
    sleep 2
  # 结束当前控制块。
  done
  # 判断条件后执行对应操作。
  if (( ! healthy )); then
    # 执行当前脚本步骤。
    "${compose[@]}" logs --tail=100 platform-api postgres redpanda emqx || true
    # 执行当前脚本步骤。
    die "平台健康检查失败：$health_url"
  # 结束当前控制块。
  fi
  # 执行当前脚本步骤。
  "${compose[@]}" exec -T ollama ollama show nomic-embed-text
  # 判断条件后执行对应操作。
  echo "平台健康检查与知识库嵌入模型检查通过：$health_url"
  # 执行当前脚本步骤。
  check_web_port="$(env_value IOT_WEB_PORT)"
  # 执行当前脚本步骤。
  check_backup_port="$(env_value IOT_BACKUP_HTTP_PORT)"
  # 遍历数据并执行循环体。
  for url in "http://127.0.0.1:${check_web_port:-8080}/" "http://127.0.0.1:${check_web_port:-8080}/health/ready" "http://127.0.0.1:${check_backup_port:-8092}/health/ready"; do
    # 执行当前脚本步骤。
    ready=0
    # 遍历数据并执行循环体。
    for _ in $(seq 1 60); do
      # 判断条件后执行对应操作。
      if curl --fail --silent --show-error --max-time 3 "$url" >/dev/null 2>&1; then ready=1; break; fi
      # 执行当前脚本步骤。
      sleep 2
    # 结束当前控制块。
    done
    # 执行当前脚本步骤。
    (( ready )) || die "服务健康检查失败：$url"
    # 执行当前脚本步骤。
    echo "健康检查通过：$url"
  # 结束当前控制块。
  done
# 结束当前控制块。
fi
# 执行当前脚本步骤。
web_port="$(env_value IOT_WEB_PORT)"
# 执行当前脚本步骤。
echo "离线部署完成。Web 地址：http://127.0.0.1:${web_port:-8080}"
# 执行当前脚本步骤。
echo "管理员凭据：$bundle_dir/OFFLINE-CREDENTIALS.txt"
