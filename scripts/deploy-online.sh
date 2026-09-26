#!/usr/bin/env bash
# 执行当前脚本步骤。
set -Eeuo pipefail
# 执行当前脚本步骤。
script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
# 执行当前脚本步骤。
project_root="$(dirname -- "$script_dir")"
# shellcheck source=lib/deployment.sh
# 执行当前脚本步骤。
source "$script_dir/lib/deployment.sh"

# 执行当前脚本步骤。
env_file='.env.online'
# 执行当前脚本步骤。
project_name='iot-platform-online'
# 执行当前脚本步骤。
include_ai=0
# 执行当前脚本步骤。
include_harness=true
# 执行当前脚本步骤。
health_timeout=180
# 遍历数据并执行循环体。
while [ "$#" -gt 0 ]; do
  # 执行当前脚本步骤。
  case "$1" in
    # 执行当前脚本步骤。
    --env-file|--project-name|--health-timeout)
      # 执行当前脚本步骤。
      [ "$#" -ge 2 ] && [ -n "$2" ] || { printf '参数缺少值：%s\n' "$1" >&2; exit 1; }
      # 执行当前脚本步骤。
      case "$1" in --env-file) env_file="$2";; --project-name) project_name="$2";; --health-timeout) health_timeout="$2";; esac
      # 执行当前脚本步骤。
      shift 2;;
    # 执行当前脚本步骤。
    --include-ai) include_ai=1; shift;;
    # 执行当前脚本步骤。
    --include-harness) shift;;
    # 执行当前脚本步骤。
    --no-harness) echo 'AI 工作流服务（Harness）是必装组件，不能使用 --no-harness。' >&2; exit 1;;
    # 执行当前脚本步骤。
    -h|--help)
      # 执行当前脚本步骤。
      cat <<'EOF'
用法：bash scripts/deploy-online.sh [选项]
  --env-file PATH       配置文件（默认 platform/.env.online；已有凭据保留）
  --project-name NAME   Docker Compose 项目（默认 iot-platform-online）
  --include-ai          兼容参数；统一使用 DeepSeek API，不下载对话模型
  --include-harness     兼容参数；AI 工作流 Harness 为必装组件，始终启动
  --health-timeout SEC  每项 HTTP 健康检查超时（默认 180 秒）
默认拉取运行镜像、构建应用、启动全部服务，并仅下载知识库嵌入模型 nomic-embed-text。
Linux 缺少 Docker/Compose/Buildx 时自动安装；首次安装使用 root/sudo。Windows/macOS 需预装 Docker Desktop；Git 和 curl 需可用。
EOF
      # 返回结果或结束当前脚本。
      exit 0;;
    # 执行当前脚本步骤。
    *) printf '未知参数：%s\n' "$1" >&2; exit 1;;
  # 执行当前脚本步骤。
  esac
# 结束当前控制块。
done
# 执行当前脚本步骤。
[[ "$project_name" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || { echo '项目名只能包含小写字母、数字、下划线和短横线，且以字母或数字开头。' >&2; exit 1; }
# 执行当前脚本步骤。
[[ "$health_timeout" =~ ^[1-9][0-9]*$ ]] || { echo '健康检查超时必须是正整数。' >&2; exit 1; }
# 执行当前脚本步骤。
case "$env_file" in /*|[A-Za-z]:[\\/]*) ;; *) env_file="$project_root/$env_file";; esac
# 执行当前脚本步骤。
source "$script_dir/lib/docker-bootstrap.sh"
# 执行当前脚本步骤。
ensure_deployment_docker online
# 执行当前脚本步骤。
assert_docker_available
# 执行当前脚本步骤。
command -v curl >/dev/null 2>&1 || { echo '健康检查需要 curl，请先安装。' >&2; exit 1; }
# 执行当前脚本步骤。
ensure_deployment_env "$env_file"
# 执行当前脚本步骤。
configure_deepseek_env "$env_file"

# 执行当前脚本步骤。
# AI 工作流服务（Harness）为必装组件：告警研判、巡检、报告、协议助手和规则草稿都通过它运行。
if [ "$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_ENABLED)" = false ]; then echo '提示：Harness 已改为必装组件，已将 IOT_AI_HARNESS_ENABLED 改为 true。' >&2; fi
# 执行当前脚本步骤。
set_deployment_env_value "$env_file" IOT_AI_HARNESS_ENABLED true
# 判断条件后执行对应操作。

# 判断条件后执行对应操作。
if [ "$include_harness" = true ]; then
  # 执行当前脚本步骤。
  [ -n "$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_URL)" ] || set_deployment_env_value "$env_file" IOT_AI_HARNESS_URL http://deepseek-harness:8091
  # 执行当前脚本步骤。
  [ -n "$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_MCP_URL)" ] || set_deployment_env_value "$env_file" IOT_AI_HARNESS_MCP_URL http://platform-api:8080/mcp/harness
# 结束当前控制块。
fi
# 执行当前脚本步骤。
annotate_deployment_env_file "$env_file"
# 执行当前脚本步骤。
compose=(compose --project-name "$project_name" --env-file "$env_file" -f "$project_root/compose.yaml")
# 执行当前脚本步骤。
build_services=(platform-api platform-web backup-service)
# 判断条件后执行对应操作。
if [ "$include_harness" = true ]; then
  # 执行当前脚本步骤。
  command -v git >/dev/null 2>&1 || { echo 'AI 工作流服务（Harness）为必装组件，构建需要安装 Git。' >&2; exit 1; }
  # 执行当前脚本步骤。
  build_services+=(deepseek-harness)
  # 执行当前脚本步骤。
  sh "$script_dir/fetch-deepseek-harness.sh"
# 结束当前控制块。
fi
# 执行当前脚本步骤。
run_docker "${compose[@]}" config --quiet
# 执行当前脚本步骤。
services="$(docker "${compose[@]}" config --services)"
# 执行当前脚本步骤。
pull_services=()
# 遍历数据并执行循环体。
while IFS= read -r service; do
  # 执行当前脚本步骤。
  service="${service%$'\r'}"
  # 执行当前脚本步骤。
  case "$service" in platform-api|platform-web|backup-service|deepseek-harness|'') ;; *) pull_services+=("$service");; esac
# 结束当前控制块。
done <<< "$services"
# 执行当前脚本步骤。
echo '拉取运行依赖镜像……'
# 执行当前脚本步骤。
run_docker "${compose[@]}" pull "${pull_services[@]}"
# 执行当前脚本步骤。
echo '构建 API、前端和备份服务镜像……'
# 执行当前脚本步骤。
run_docker "${compose[@]}" build --pull "${build_services[@]}"
# 执行当前脚本步骤。
echo '启动服务……'
# 执行当前脚本步骤。
run_docker "${compose[@]}" up -d --no-build --pull never
# 执行当前脚本步骤。
echo '下载知识库嵌入模型 nomic-embed-text（首次可能需要较长时间）……'
# 执行当前脚本步骤。
run_docker "${compose[@]}" exec -T ollama ollama pull nomic-embed-text
# 判断条件后执行对应操作。

# 执行当前脚本步骤。
api_port="$(get_deployment_env_value "$env_file" IOT_API_PORT)"; api_port="${api_port:-8081}"
# 执行当前脚本步骤。
web_port="$(get_deployment_env_value "$env_file" IOT_WEB_PORT)"; web_port="${web_port:-8080}"
# 执行当前脚本步骤。
wait_deployment_http "http://127.0.0.1:$api_port/health/ready" "$health_timeout"
# 执行当前脚本步骤。
wait_deployment_http "http://127.0.0.1:$web_port/" "$health_timeout"
# 执行当前脚本步骤。
wait_deployment_http "http://127.0.0.1:$web_port/health/ready" "$health_timeout"
# 执行当前脚本步骤。
backup_port="$(get_deployment_env_value "$env_file" IOT_BACKUP_HTTP_PORT)"
# 执行当前脚本步骤。
wait_deployment_http "http://127.0.0.1:${backup_port:-8092}/health/ready" "$health_timeout"
# 判断条件后执行对应操作。
if [ "$include_harness" = true ]; then
  # 执行当前脚本步骤。
  harness_port="$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_PORT)"
  # 执行当前脚本步骤。
  wait_deployment_http "http://127.0.0.1:${harness_port:-8091}/health" "$health_timeout"
  # 执行当前脚本步骤。
  printf 'Harness 已启动；自动研判和工作流共用模型 %s。\n' "${model:-$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_MODEL)}"
# 结束当前控制块。
fi
# 执行当前脚本步骤。
run_docker "${compose[@]}" ps
# 执行当前脚本步骤。
printf '在线部署完成：http://127.0.0.1:%s/；登录账号和密码查看 %s 中 IOT_ADMIN_USER / IOT_ADMIN_PASSWORD。\n' "$web_port" "$env_file"
