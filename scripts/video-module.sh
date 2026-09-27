#!/usr/bin/env bash
# Deploy-level switch for the camera live module (ZLMediaKit media server,
# Compose profile "video"). It is deployed by default; IOT_VIDEO_MODULE=off in
# the environment file records an explicit opt-out that later setup and deploy
# runs keep. Who may watch is decided in the platform UI.
set -Eeuo pipefail
script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
project_root="$(dirname -- "$script_dir")"
# shellcheck source=lib/deployment.sh
source "$script_dir/lib/deployment.sh"

usage() {
  cat <<'EOF'
用法：bash scripts/video-module.sh <enable|disable|prepare|status|logs> [选项]
  enable    部署并启动媒体服务，向 API 写入直播配置（首次生成媒体密钥和凭据加密密钥）
  disable   停止并移除媒体服务容器，API 回到“未部署”；保留密钥、凭据和直播配置，后续部署保持关闭
  prepare   只写入直播配置（与 enable 相同的密钥和地址），不操作容器；供部署脚本调用
  status    查看媒体服务容器与配置状态
  logs      查看媒体服务最近日志（日志可能含摄像头地址，请勿公开）
选项：
  --mode online|local|offline  部署方式，默认 online
  --env-file PATH              配置文件（默认 online=.env.online，local=.env.local，offline=.env.offline）
  --project-name NAME          Compose 项目（默认 online=iot-platform-online，local=iot-platform-local，offline=iot-platform）
  --rtc-ip IP[,IP]             浏览器访问媒体服务使用的主机 IP（WebRTC 候选地址）；本地默认 127.0.0.1
  --rtc-port PORT              WebRTC UDP/TCP 端口，默认 8000
  --transcode | --no-transcode 启用或关闭可选转码（FFmpeg 已包含在固定版本媒体镜像中）
  --allowed-cidrs LIST         允许访问的摄像头网段，逗号分隔（默认 10.0.0.0/8,172.16.0.0/12,192.168.0.0/16）
EOF
}

action="${1:-}"; [ "$#" -gt 0 ] && shift || true
mode=online env_file='' project_name='' rtc_ip='' rtc_port='' transcode='' cidrs=''
while [ "$#" -gt 0 ]; do
  case "$1" in
    --mode|--env-file|--project-name|--rtc-ip|--rtc-port|--allowed-cidrs)
      [ "$#" -ge 2 ] && [ -n "$2" ] || { printf '参数缺少值：%s\n' "$1" >&2; exit 1; }
      case "$1" in --mode) mode="$2";; --env-file) env_file="$2";; --project-name) project_name="$2";; --rtc-ip) rtc_ip="$2";; --rtc-port) rtc_port="$2";; --allowed-cidrs) cidrs="$2";; esac
      shift 2;;
    --transcode) transcode=true; shift;;
    --no-transcode) transcode=false; shift;;
    -h|--help) usage; exit 0;;
    *) printf '未知参数：%s\n' "$1" >&2; usage >&2; exit 1;;
  esac
done
case "$action" in enable|disable|prepare|status|logs) ;; *) usage >&2; exit 1;; esac

compose_files=(-f "$project_root/compose.yaml")
case "$mode" in
  online) env_file="${env_file:-.env.online}"; project_name="${project_name:-iot-platform-online}";;
  local) env_file="${env_file:-.env.local}"; project_name="${project_name:-iot-platform-local}"; compose_files=(-f "$project_root/compose.local.yaml");;
  offline) env_file="${env_file:-.env.offline}"; project_name="${project_name:-iot-platform}"; compose_files+=(-f "$project_root/compose.offline.yaml");;
  *) echo '--mode 只能是 online、local 或 offline。' >&2; exit 1;;
esac
case "$env_file" in /*|[A-Za-z]:[\\/]*) ;; *) env_file="$project_root/$env_file";; esac
[ -f "$env_file" ] || { printf '配置文件不存在：%s（请先完成对应的部署或本地准备）\n' "$env_file" >&2; exit 1; }
[[ -z "$rtc_ip" || "$rtc_ip" =~ ^[0-9A-Fa-f.:,]+$ ]] || { echo '--rtc-ip 只能是 IP 地址，多个用逗号分隔。' >&2; exit 1; }
[[ -z "$rtc_port" || "$rtc_port" =~ ^[1-9][0-9]{1,4}$ ]] || { echo '--rtc-port 必须是端口号。' >&2; exit 1; }
[[ -z "$cidrs" || "$cidrs" =~ ^[0-9A-Fa-f.:/,]+$ ]] || { echo '--allowed-cidrs 格式无效。' >&2; exit 1; }

compose=(compose --project-name "$project_name" --env-file "$env_file" "${compose_files[@]}" --profile video)

# Add or remove "video" in COMPOSE_PROFILES while keeping other profiles.
set_profile() {
  local want="$1" current next='' item
  current="$(get_deployment_env_value "$env_file" COMPOSE_PROFILES)"
  local items=()
  IFS=',' read -r -a items <<< "$current"
  # ${arr[@]+...} keeps bash 3.2 (macOS) from failing on an empty array under set -u.
  for item in ${items[@]+"${items[@]}"}; do
    item="${item// /}"
    [ -n "$item" ] && [ "$item" != video ] && next="${next:+$next,}$item"
  done
  [ "$want" = on ] && next="${next:+$next,}video"
  set_deployment_env_value "$env_file" COMPOSE_PROFILES "$next"
}

ensure_secret() {
  local key="$1" value
  value="$(get_deployment_env_value "$env_file" "$key")"
  if [ -z "$value" ]; then
    if [ "$key" = IOT_VIDEO_CREDENTIAL_KEY ]; then
      # 32 random bytes, standard base64. Never rotated automatically: stored
      # camera passwords can only be decrypted with this key.
      value="$(head -c 32 /dev/urandom | base64 | tr -d '\n')"
    else
      value="$(deployment_secret)"
    fi
    set_deployment_env_value "$env_file" "$key" "$value"
  fi
}

api_url() {
  case "$mode" in
    local) printf 'http://%s:18580' "$(get_deployment_env_value "$env_file" IOT_LOCAL_ADVERTISED_HOST | sed 's/^$/127.0.0.1/')";;
    *) printf 'http://zlmediakit:80';;
  esac
}

prepare_config() {
  for key in IOT_VIDEO_MEDIA_SECRET IOT_VIDEO_HOOK_SECRET IOT_VIDEO_CREDENTIAL_KEY; do ensure_secret "$key"; done
  [ -n "$rtc_ip" ] && set_deployment_env_value "$env_file" IOT_VIDEO_RTC_EXTERN_IP "$rtc_ip"
  if [ -z "$(get_deployment_env_value "$env_file" IOT_VIDEO_RTC_EXTERN_IP)" ]; then
    if [ "$mode" = local ]; then set_deployment_env_value "$env_file" IOT_VIDEO_RTC_EXTERN_IP 127.0.0.1
    else echo '提示：未设置 --rtc-ip，浏览器可能无法建立 WebRTC 连接，播放器会改用 HLS。' >&2; fi
  fi
  [ -n "$rtc_port" ] && set_deployment_env_value "$env_file" IOT_VIDEO_RTC_PORT "$rtc_port"
  [ -n "$transcode" ] && set_deployment_env_value "$env_file" IOT_VIDEO_TRANSCODE_ENABLED "$transcode"
  [ -n "$cidrs" ] && set_deployment_env_value "$env_file" IOT_VIDEO_ALLOWED_CIDRS "$cidrs"
  set_deployment_env_value "$env_file" IOT_VIDEO_MEDIA_API_URL "$(api_url)"
  set_deployment_env_value "$env_file" IOT_VIDEO_MODULE on
  set_profile on
  annotate_deployment_env_file "$env_file"
}

case "$action" in
  prepare)
    prepare_config;;
  enable)
    assert_docker_available
    prepare_config
    run_docker "${compose[@]}" config --quiet
    if [ "$mode" = offline ]; then
      image="$(get_deployment_env_value "$env_file" IOT_ZLMEDIAKIT_IMAGE)"
      docker image inspect "${image:-iot-zlmediakit:offline}" >/dev/null 2>&1 || { echo '离线包未包含媒体服务镜像，请重新打包（不要使用 --without-video）。' >&2; exit 1; }
      run_docker "${compose[@]}" up -d --no-build --pull never --wait --wait-timeout 120 zlmediakit
    else
      run_docker "${compose[@]}" build --pull zlmediakit
      run_docker "${compose[@]}" up -d --no-build --wait --wait-timeout 120 zlmediakit
    fi
    if [ "$mode" = local ]; then
      echo '媒体服务已启动。请重启本地 API（go run ./cmd/iot-platform --env-file .env.local）以加载 IOT_VIDEO_* 配置。'
    else
      # Recreate the API so it reads the new IOT_VIDEO_* settings.
      run_docker "${compose[@]}" up -d --no-deps --no-build platform-api
    fi
    echo '直播模块已部署并默认启用。为摄像头配置 ONVIF / RTSP / GB28181 接入后即可观看；未配置的摄像头只保留资料。';;
  disable)
    assert_docker_available
    set_deployment_env_value "$env_file" IOT_VIDEO_MEDIA_API_URL ''
    set_deployment_env_value "$env_file" IOT_VIDEO_MODULE off
    set_profile off
    run_docker "${compose[@]}" stop zlmediakit || true
    run_docker "${compose[@]}" rm -f zlmediakit || true
    if [ "$mode" != local ]; then
      run_docker compose --project-name "$project_name" --env-file "$env_file" "${compose_files[@]}" up -d --no-deps --no-build platform-api
    else
      echo '请重启本地 API 使“未部署”状态生效。'
    fi
    echo '媒体服务已停止并移除。摄像头资料、直播配置、加密凭据与密钥均已保留；再次 enable 即可恢复。';;
  status)
    printf '配置文件：%s\n' "$env_file"
    url="$(get_deployment_env_value "$env_file" IOT_VIDEO_MEDIA_API_URL)"
    printf 'API 媒体服务地址：%s\n' "${url:-（未设置，API 报告“未部署”）}"
    printf 'WebRTC 地址：%s，端口：%s\n' "$(get_deployment_env_value "$env_file" IOT_VIDEO_RTC_EXTERN_IP)" "$(get_deployment_env_value "$env_file" IOT_VIDEO_RTC_PORT | sed 's/^$/8000/')"
    printf '转码：%s\n' "$(get_deployment_env_value "$env_file" IOT_VIDEO_TRANSCODE_ENABLED | sed 's/^$/false/')"
    run_docker "${compose[@]}" ps zlmediakit;;
  logs)
    run_docker "${compose[@]}" logs --tail 200 zlmediakit;;
esac
