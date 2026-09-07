#!/usr/bin/env bash
set -Eeuo pipefail
script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
project_root="$(dirname -- "$script_dir")"
# shellcheck source=lib/deployment.sh
source "$script_dir/lib/deployment.sh"

env_file='.env.online'
project_name='iot-platform-online'
include_ai=0
include_harness=auto
health_timeout=180
while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file|--project-name|--health-timeout)
      [ "$#" -ge 2 ] && [ -n "$2" ] || { printf '参数缺少值：%s\n' "$1" >&2; exit 1; }
      case "$1" in --env-file) env_file="$2";; --project-name) project_name="$2";; --health-timeout) health_timeout="$2";; esac
      shift 2;;
    --include-ai) include_ai=1; shift;;
    --include-harness) include_harness=1; shift;;
    --no-harness) include_harness=0; shift;;
    -h|--help)
      cat <<'EOF'
用法：bash scripts/deploy-online.sh [选项]
  --env-file PATH       配置文件（默认 platform/.env.online；已有凭据保留）
  --project-name NAME   Docker Compose 项目（默认 iot-platform-online）
  --include-ai          强制改用本地 Ollama（兼容旧配置）
  --include-harness     显式启用默认启动的 AI 工作流 Harness
  --no-harness          不启动 AI 工作流 Harness，并将配置开关设为 false
  --health-timeout SEC  每项 HTTP 健康检查超时（默认 180 秒）
默认拉取运行镜像、构建应用、启动全部服务，并下载 qwen3:1.7b 与 nomic-embed-text。
需要 Docker Engine/Desktop、Compose v2、Git 和 curl。自动研判与工作流默认共用本地 qwen3:1.7b。
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
ensure_deployment_env "$env_file"
provider="$(get_deployment_env_value "$env_file" IOT_AI_PROVIDER)"
configured_model="$(get_deployment_env_value "$env_file" IOT_AI_MODEL)"
if [ "$include_ai" -eq 1 ] || [ -z "$provider" ] || [ "$provider" = disabled ] || { [ "$provider" = deepseek ] && [ "$configured_model" = deepseek-v4-flash ]; } || { [ "$provider" = ollama ] && [ "$configured_model" = qwen3:8b ]; }; then
  if [ "$provider" = ollama ]; then model="$configured_model"; else model="$(get_deployment_env_value "$env_file" IOT_OLLAMA_MODEL)"; fi
  case "$model" in ''|qwen3:8b|deepseek-v4-flash) model=qwen3:1.7b;; esac
  set_deployment_env_value "$env_file" IOT_AI_PROVIDER ollama
  set_deployment_env_value "$env_file" IOT_OLLAMA_URL http://ollama:11434
  set_deployment_env_value "$env_file" IOT_OLLAMA_MODEL "$model"
  set_deployment_env_value "$env_file" IOT_AI_BASE_URL http://ollama:11434
  set_deployment_env_value "$env_file" IOT_AI_MODEL "$model"
fi
provider="$(get_deployment_env_value "$env_file" IOT_AI_PROVIDER)"
if [ "$provider" = ollama ]; then
  model="$(get_deployment_env_value "$env_file" IOT_AI_MODEL)"; model="${model:-qwen3:1.7b}"
  set_deployment_env_value "$env_file" IOT_OLLAMA_URL http://ollama:11434
  set_deployment_env_value "$env_file" IOT_OLLAMA_MODEL "$model"
  set_deployment_env_value "$env_file" IOT_AI_BASE_URL http://ollama:11434
  set_deployment_env_value "$env_file" IOT_AI_MODEL "$model"
  set_deployment_env_value "$env_file" IOT_AI_HARNESS_PROVIDER ollama
  set_deployment_env_value "$env_file" IOT_AI_HARNESS_OLLAMA_BASE_URL http://ollama:11434/v1
  set_deployment_env_value "$env_file" IOT_AI_HARNESS_CONTEXT_WINDOW 8192
  set_deployment_env_value "$env_file" IOT_AI_HARNESS_MODEL "$model"
fi
case "$include_harness" in
  1) set_deployment_env_value "$env_file" IOT_AI_HARNESS_ENABLED true;;
  0) set_deployment_env_value "$env_file" IOT_AI_HARNESS_ENABLED false;;
esac
include_harness="$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_ENABLED)"
if [ -z "$include_harness" ]; then include_harness=true; set_deployment_env_value "$env_file" IOT_AI_HARNESS_ENABLED true; fi
case "$include_harness" in true|false) ;; *) echo 'IOT_AI_HARNESS_ENABLED 只能是 true 或 false。' >&2; exit 1;; esac
if [ "$(get_deployment_env_value "$env_file" IOT_AI_PROVIDER)" = deepseek ]; then
  deepseek_key="$(get_deployment_env_value "$env_file" DEEPSEEK_API_KEY)"
  if [ -z "$deepseek_key" ]; then deepseek_key="$(get_deployment_env_value "$env_file" IOT_AI_API_KEY)"; fi
  if [ -n "$deepseek_key" ] && [ -z "$(get_deployment_env_value "$env_file" DEEPSEEK_API_KEY)" ]; then set_deployment_env_value "$env_file" DEEPSEEK_API_KEY "$deepseek_key"; fi
  [ -n "$(get_deployment_env_value "$env_file" IOT_AI_BASE_URL)" ] || set_deployment_env_value "$env_file" IOT_AI_BASE_URL https://api.deepseek.com
  [ -n "$(get_deployment_env_value "$env_file" IOT_AI_MODEL)" ] || set_deployment_env_value "$env_file" IOT_AI_MODEL deepseek-v4-flash
  if [ -z "$deepseek_key" ]; then echo '提示：请在配置文件中填写 DEEPSEEK_API_KEY，自动研判和 AI 工作流将共用该密钥。' >&2; fi
fi
if [ "$include_harness" = true ]; then
  [ -n "$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_URL)" ] || set_deployment_env_value "$env_file" IOT_AI_HARNESS_URL http://deepseek-harness:8091
  [ -n "$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_MCP_URL)" ] || set_deployment_env_value "$env_file" IOT_AI_HARNESS_MCP_URL http://platform-api:8080/mcp/harness
else
  set_deployment_env_value "$env_file" IOT_AI_HARNESS_URL ''
fi
annotate_deployment_env_file "$env_file"
compose=(compose --project-name "$project_name" --env-file "$env_file" -f "$project_root/compose.yaml")
build_services=(platform-api platform-web backup-service)
if [ "$include_harness" = true ]; then
  command -v git >/dev/null 2>&1 || { echo '默认启用 Harness，需要安装 Git；也可在配置中设置 IOT_AI_HARNESS_ENABLED=false。' >&2; exit 1; }
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
if [ "$provider" = ollama ]; then
  model="$(get_deployment_env_value "$env_file" IOT_AI_MODEL)"
  echo "下载统一 AI 模型 ${model:-qwen3:1.7b}（告警研判与工作流共用）……"
  run_docker "${compose[@]}" exec -T ollama ollama pull "${model:-qwen3:1.7b}"
fi
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
