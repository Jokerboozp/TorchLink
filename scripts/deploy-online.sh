#!/usr/bin/env bash
set -Eeuo pipefail
script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
project_root="$(dirname -- "$script_dir")"
# shellcheck source=lib/deployment.sh
source "$script_dir/lib/deployment.sh"

env_file='.env.online'
project_name='iot-platform-online'
include_ai=0
include_harness=0
health_timeout=180
while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file|--project-name|--health-timeout)
      [ "$#" -ge 2 ] && [ -n "$2" ] || { printf '参数缺少值：%s\n' "$1" >&2; exit 1; }
      case "$1" in --env-file) env_file="$2";; --project-name) project_name="$2";; --health-timeout) health_timeout="$2";; esac
      shift 2;;
    --include-ai) include_ai=1; shift;;
    --include-harness) include_harness=1; shift;;
    -h|--help)
      cat <<'EOF'
用法：bash scripts/deploy-online.sh [选项]
  --env-file PATH       配置文件（默认 platform/.env.online；已有凭据保留）
  --project-name NAME   Docker Compose 项目（默认 iot-platform-online）
  --include-ai          额外下载对话模型；新配置自动启用 Ollama
  --include-harness     下载固定版本 Harness 源码并构建侧车（需要 Git）
  --health-timeout SEC  每项 HTTP 健康检查超时（默认 180 秒）
默认拉取运行镜像、构建应用、启动全部常规服务、下载 nomic-embed-text 并验证 API/前端。
需要 Docker Engine/Desktop、Compose v2 和 curl。Harness 云端调用另需配置 DEEPSEEK_API_KEY。
EOF
      exit 0;;
    *) printf '未知参数：%s\n' "$1" >&2; exit 1;;
  esac
done
[[ "$project_name" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || { echo '项目名只能包含小写字母、数字、下划线和短横线，且以字母或数字开头。' >&2; exit 1; }
[[ "$health_timeout" =~ ^[1-9][0-9]*$ ]] || { echo '健康检查超时必须是正整数。' >&2; exit 1; }
case "$env_file" in /*|[A-Za-z]:[\\/]*) ;; *) env_file="$project_root/$env_file";; esac
assert_docker_available
command -v curl >/dev/null 2>&1 || { echo '健康检查需要 curl，请先安装。' >&2; exit 1; }
defaults=()
if [ "$include_ai" -eq 1 ]; then
  defaults+=('IOT_AI_PROVIDER=ollama' 'IOT_OLLAMA_URL=http://ollama:11434' 'IOT_AI_BASE_URL=http://ollama:11434' 'IOT_AI_MODEL=qwen3:8b')
fi
[ "$include_harness" -ne 1 ] || defaults+=('IOT_AI_HARNESS_URL=http://deepseek-harness:8091')
ensure_deployment_env "$env_file" "${defaults[@]}"
if [ "$include_ai" -eq 1 ]; then
  provider="$(get_deployment_env_value "$env_file" IOT_AI_PROVIDER)"
  case "$provider" in
    ''|disabled|ollama)
      model="$(get_deployment_env_value "$env_file" IOT_OLLAMA_MODEL)"; model="${model:-qwen3:8b}"
      if [ "$provider" = ollama ]; then
        configured_model="$(get_deployment_env_value "$env_file" IOT_AI_MODEL)"
        model="${configured_model:-$model}"
      fi
      set_deployment_env_value "$env_file" IOT_AI_PROVIDER ollama
      set_deployment_env_value "$env_file" IOT_OLLAMA_URL http://ollama:11434
      set_deployment_env_value "$env_file" IOT_AI_BASE_URL http://ollama:11434
      set_deployment_env_value "$env_file" IOT_AI_MODEL "$model";;
    *) echo '保留已有远程 AI Provider；仅下载本地对话模型。';;
  esac
fi
if [ "$include_harness" -eq 1 ]; then
  set_deployment_env_value "$env_file" IOT_AI_HARNESS_URL http://deepseek-harness:8091
  set_deployment_env_value "$env_file" IOT_AI_HARNESS_MCP_URL http://platform-api:8080/mcp/harness
fi
compose=(compose --project-name "$project_name" --env-file "$env_file" -f "$project_root/compose.yaml")
build_services=(platform-api platform-web backup-service)
if [ "$include_harness" -eq 1 ]; then
  command -v git >/dev/null 2>&1 || { echo '--include-harness 需要 Git。' >&2; exit 1; }
  compose+=(--profile harness)
  build_services+=(deepseek-harness)
  sh "$script_dir/fetch-deepseek-harness.sh"
fi
run_docker "${compose[@]}" config --quiet
services="$(docker "${compose[@]}" config --services)"
pull_services=()
while IFS= read -r service; do
  service="${service%$'\r'}"
  case "$service" in platform-api|platform-web|backup-service|deepseek-harness|'') ;; *) pull_services+=("$service");; esac
done <<< "$services"
echo '拉取运行依赖镜像……'
run_docker "${compose[@]}" pull "${pull_services[@]}"
echo '构建 API、前端和备份服务镜像……'
run_docker "${compose[@]}" build --pull "${build_services[@]}"
echo '启动服务……'
run_docker "${compose[@]}" up -d --no-build --pull never
echo '下载知识库嵌入模型 nomic-embed-text（首次可能需要较长时间）……'
run_docker "${compose[@]}" exec -T ollama ollama pull nomic-embed-text
if [ "$include_ai" -eq 1 ]; then
  provider="$(get_deployment_env_value "$env_file" IOT_AI_PROVIDER)"
  model="$(get_deployment_env_value "$env_file" IOT_AI_MODEL)"
  if [ "$provider" != ollama ] || [ -z "$model" ]; then model="$(get_deployment_env_value "$env_file" IOT_OLLAMA_MODEL)"; fi
  run_docker "${compose[@]}" exec -T ollama ollama pull "${model:-qwen3:8b}"
  if [ "$provider" != ollama ]; then
    printf '模型已下载；已有 AI Provider 保持不变。要启用 Ollama，请在 %s 设置 IOT_AI_PROVIDER=ollama、IOT_OLLAMA_URL=http://ollama:11434 后重跑。\n' "$env_file"
  fi
fi
api_port="$(get_deployment_env_value "$env_file" IOT_API_PORT)"; api_port="${api_port:-8081}"
web_port="$(get_deployment_env_value "$env_file" IOT_WEB_PORT)"; web_port="${web_port:-8080}"
wait_deployment_http "http://127.0.0.1:$api_port/health/ready" "$health_timeout"
wait_deployment_http "http://127.0.0.1:$web_port/" "$health_timeout"
wait_deployment_http "http://127.0.0.1:$web_port/health/ready" "$health_timeout"
backup_port="$(get_deployment_env_value "$env_file" IOT_BACKUP_HTTP_PORT)"
wait_deployment_http "http://127.0.0.1:${backup_port:-8092}/health/live" "$health_timeout"
if [ "$include_harness" -eq 1 ]; then
  harness_port="$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_PORT)"
  wait_deployment_http "http://127.0.0.1:${harness_port:-8091}/health" "$health_timeout"
  printf 'Harness 已启动；使用前在 %s 配置 DEEPSEEK_API_KEY 和 IOT_AI_HARNESS_URL=http://deepseek-harness:8091 后重跑。\n' "$env_file"
fi
run_docker "${compose[@]}" ps
printf '在线部署完成：http://127.0.0.1:%s/；登录账号和密码查看 %s 中 IOT_ADMIN_USER / IOT_ADMIN_PASSWORD。\n' "$web_port" "$env_file"
