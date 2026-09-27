#!/usr/bin/env bash
set -Eeuo pipefail
script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
project_root="$(dirname -- "$script_dir")"
# shellcheck source=lib/deployment.sh
source "$script_dir/lib/deployment.sh"

env_file='.env.online'
project_name='iot-platform-online'
include_ai=0
include_harness=true
health_timeout=180
video=keep
while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file|--project-name|--health-timeout)
      [ "$#" -ge 2 ] && [ -n "$2" ] || { printf '参数缺少值：%s\n' "$1" >&2; exit 1; }
      case "$1" in --env-file) env_file="$2";; --project-name) project_name="$2";; --health-timeout) health_timeout="$2";; esac
      shift 2;;
    --video) [ "$#" -ge 2 ] || { echo '--video 需要 on 或 off。' >&2; exit 1; }; video="$2"; shift 2;;
    --include-ai) include_ai=1; shift;;
    --include-harness) shift;;
    --no-harness) echo 'AI 工作流服务（Harness）是必装组件，不能使用 --no-harness。' >&2; exit 1;;
    -h|--help)
      cat <<'EOF'
用法：bash scripts/deploy-online.sh [选项]
  --env-file PATH       配置文件（默认 platform/.env.online；已有凭据保留）
  --project-name NAME   Docker Compose 项目（默认 iot-platform-online）
  --include-ai          兼容参数；统一使用 DeepSeek API，不下载对话模型
  --include-harness     兼容参数；AI 工作流 Harness 为必装组件，始终启动
  --health-timeout SEC  每项 HTTP 健康检查超时（默认 180 秒）
  --video on|off        部署或关闭摄像头直播媒体服务；省略时沿用上次选择，新环境默认开启
默认拉取运行镜像、构建应用、启动全部服务，并仅下载知识库嵌入模型 nomic-embed-text。
Linux 缺少 Docker/Compose/Buildx 时自动安装；首次安装使用 root/sudo。Windows/macOS 需预装 Docker Desktop；Git 和 curl 需可用。
EOF
      exit 0;;
    *) printf '未知参数：%s\n' "$1" >&2; exit 1;;
  esac
done
[[ "$project_name" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || { echo '项目名只能包含小写字母、数字、下划线和短横线，且以字母或数字开头。' >&2; exit 1; }
[[ "$health_timeout" =~ ^[1-9][0-9]*$ ]] || { echo '健康检查超时必须是正整数。' >&2; exit 1; }
case "$video" in keep|on|off) ;; *) echo '--video 只能是 on 或 off。' >&2; exit 1;; esac
case "$env_file" in /*|[A-Za-z]:[\\/]*) ;; *) env_file="$project_root/$env_file";; esac
source "$script_dir/lib/docker-bootstrap.sh"
ensure_deployment_docker online
assert_docker_available
command -v curl >/dev/null 2>&1 || { echo '健康检查需要 curl，请先安装。' >&2; exit 1; }
ensure_deployment_env "$env_file"
configure_deepseek_env "$env_file"

# AI 工作流服务（Harness）为必装组件：告警研判、巡检、报告、协议助手和规则草稿都通过它运行。
if [ "$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_ENABLED)" = false ]; then echo '提示：Harness 已改为必装组件，已将 IOT_AI_HARNESS_ENABLED 改为 true。' >&2; fi
set_deployment_env_value "$env_file" IOT_AI_HARNESS_ENABLED true

if [ "$include_harness" = true ]; then
  [ -n "$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_URL)" ] || set_deployment_env_value "$env_file" IOT_AI_HARNESS_URL http://deepseek-harness:8091
  [ -n "$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_MCP_URL)" ] || set_deployment_env_value "$env_file" IOT_AI_HARNESS_MCP_URL http://platform-api:8080/mcp/harness
fi
# Live video is deployed by default; an earlier --video off is kept.
if [ "$video" = keep ]; then
  video=on
  [ "$(get_deployment_env_value "$env_file" IOT_VIDEO_MODULE)" = off ] && video=off
fi
if [ "$video" = on ]; then
  bash "$script_dir/video-module.sh" prepare --mode online --env-file "$env_file" --project-name "$project_name"
else
  set_deployment_env_value "$env_file" IOT_VIDEO_MEDIA_API_URL ''
  set_deployment_env_value "$env_file" IOT_VIDEO_MODULE off
  profiles="$(get_deployment_env_value "$env_file" COMPOSE_PROFILES | tr ',' '\n' | tr -d ' ' | grep -vx video | paste -sd, - || true)"
  set_deployment_env_value "$env_file" COMPOSE_PROFILES "$profiles"
fi
annotate_deployment_env_file "$env_file"
compose=(compose --project-name "$project_name" --env-file "$env_file" -f "$project_root/compose.yaml")
build_services=(platform-api platform-web backup-service)
if [ "$include_harness" = true ]; then
  command -v git >/dev/null 2>&1 || { echo 'AI 工作流服务（Harness）为必装组件，构建需要安装 Git。' >&2; exit 1; }
  build_services+=(deepseek-harness)
  sh "$script_dir/fetch-deepseek-harness.sh"
fi
run_docker "${compose[@]}" config --quiet
services="$(docker "${compose[@]}" config --services)"
pull_services=()
while IFS= read -r service; do
  service="${service%$'\r'}"
  case "$service" in
    platform-api|platform-web|backup-service|deepseek-harness|'') ;;
    # 摄像头直播媒体服务（video profile，默认启用），由固定 digest 的官方镜像构建。
    zlmediakit) build_services+=(zlmediakit) ;;
    *) pull_services+=("$service");;
  esac
done <<< "$services"
echo '拉取运行依赖镜像……'
run_docker "${compose[@]}" pull "${pull_services[@]}"
echo '构建 API、前端和备份服务镜像……'
run_docker "${compose[@]}" build --pull "${build_services[@]}"
echo '启动服务……'
run_docker "${compose[@]}" up -d --no-build --pull never
if [ "$video" = off ]; then
  # Profile services are not removed by up; stop a media server left from an earlier deployment.
  run_docker "${compose[@]}" --profile video rm -sf zlmediakit
fi
echo '下载知识库嵌入模型 nomic-embed-text（首次可能需要较长时间）……'
run_docker "${compose[@]}" exec -T ollama ollama pull nomic-embed-text

api_port="$(get_deployment_env_value "$env_file" IOT_API_PORT)"; api_port="${api_port:-8081}"
web_port="$(get_deployment_env_value "$env_file" IOT_WEB_PORT)"; web_port="${web_port:-8080}"
wait_deployment_http "http://127.0.0.1:$api_port/health/ready" "$health_timeout"
wait_deployment_http "http://127.0.0.1:$web_port/" "$health_timeout"
wait_deployment_http "http://127.0.0.1:$web_port/health/ready" "$health_timeout"
backup_port="$(get_deployment_env_value "$env_file" IOT_BACKUP_HTTP_PORT)"
wait_deployment_http "http://127.0.0.1:${backup_port:-8092}/health/ready" "$health_timeout"
if [ "$include_harness" = true ]; then
  harness_port="$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_PORT)"
  wait_deployment_http "http://127.0.0.1:${harness_port:-8091}/health" "$health_timeout"
  printf 'Harness 已启动；自动研判和工作流共用模型 %s。\n' "${model:-$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_MODEL)}"
fi
run_docker "${compose[@]}" ps
printf '在线部署完成：http://127.0.0.1:%s/；登录账号和密码查看 %s 中 IOT_ADMIN_USER / IOT_ADMIN_PASSWORD。\n' "$web_port" "$env_file"
