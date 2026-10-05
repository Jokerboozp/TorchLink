#!/usr/bin/env bash
set -Eeuo pipefail
script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
project_root="$(dirname -- "$script_dir")"
# shellcheck source=lib/deployment.sh
source "$script_dir/lib/deployment.sh"

env_file='.env.online'
project_name='iot-platform-online'
health_timeout=180
video=keep
capacity=keep
ops=keep
clickhouse=keep
while [ "$#" -gt 0 ]; do
  case "$1" in
    --env-file|--project-name|--health-timeout)
      [ "$#" -ge 2 ] && [ -n "$2" ] || { printf '参数缺少值：%s\n' "$1" >&2; exit 1; }
      case "$1" in --env-file) env_file="$2";; --project-name) project_name="$2";; --health-timeout) health_timeout="$2";; esac
      shift 2;;
    --video) [ "$#" -ge 2 ] || { echo '--video 需要 on 或 off。' >&2; exit 1; }; video="$2"; shift 2;;
    --capacity) [ "$#" -ge 2 ] || { echo '--capacity 需要 on 或 off。' >&2; exit 1; }; capacity="$2"; shift 2;;
    --ops) [ "$#" -ge 2 ] || { echo '--ops 需要 on 或 off。' >&2; exit 1; }; ops="$2"; shift 2;;
    --clickhouse) [ "$#" -ge 2 ] || { echo '--clickhouse 需要 on 或 off。' >&2; exit 1; }; clickhouse="$2"; shift 2;;
    -h|--help)
      cat <<'EOF'
用法：bash scripts/deploy-online.sh [选项]
  --env-file PATH       配置文件（默认 .env.online；已有凭据保留）
  --project-name NAME   Docker Compose 项目（默认 iot-platform-online）
  --health-timeout SEC  每项 HTTP 健康检查超时（默认 180 秒）
  --video on|off        部署或关闭摄像头直播媒体服务；省略时沿用上次选择，新环境默认开启
  --capacity on|off     部署或关闭容量测试模块（运维中心 → 容量测试）；省略时沿用上次选择，新环境默认关闭
  --ops on|off          部署或关闭监控组件（Prometheus、Loki、Grafana、Alertmanager 等）；省略时沿用上次选择，新环境默认开启
  --clickhouse on|off   部署或关闭 ClickHouse（高频原文与遥测）；关闭后全部写 PostgreSQL；省略时沿用上次选择，新环境默认开启
默认拉取运行镜像、构建应用、PostgreSQL + pgvector 与知识库向量/重排服务（构建时下载并校验模型，约 1.3 GB）并启动服务；对话推理调用外部 API。
下载模型受限时可设 IOT_HF_ENDPOINT（如 https://hf-mirror.com）；ghcr.io 受限时可设 IOT_LLAMA_CPP_IMAGE 为镜像仓库中的同一镜像。
Linux 缺少 Docker/Compose/Buildx 时自动安装；首次安装使用 root/sudo。Windows/macOS 需预装 Docker Desktop；Git 和 curl 需可用。
EOF
      exit 0;;
    *) printf '未知参数：%s\n' "$1" >&2; exit 1;;
  esac
done
[[ "$project_name" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || { echo '项目名只能包含小写字母、数字、下划线和短横线，且以字母或数字开头。' >&2; exit 1; }
[[ "$health_timeout" =~ ^[1-9][0-9]*$ ]] || { echo '健康检查超时必须是正整数。' >&2; exit 1; }
case "$video" in keep|on|off) ;; *) echo '--video 只能是 on 或 off。' >&2; exit 1;; esac
case "$capacity" in keep|on|off) ;; *) echo '--capacity 只能是 on 或 off。' >&2; exit 1;; esac
case "$ops" in keep|on|off) ;; *) echo '--ops 只能是 on 或 off。' >&2; exit 1;; esac
case "$clickhouse" in keep|on|off) ;; *) echo '--clickhouse 只能是 on 或 off。' >&2; exit 1;; esac
case "$env_file" in /*|[A-Za-z]:[\\/]*) ;; *) env_file="$project_root/$env_file";; esac
source "$script_dir/lib/docker-bootstrap.sh"
ensure_deployment_docker online
assert_docker_available
command -v curl >/dev/null 2>&1 || { echo '健康检查需要 curl，请先安装。' >&2; exit 1; }
ensure_deployment_env "$env_file"
ensure_emqx_admin_env "$env_file" "http://emqx:18083"
ensure_kafka_bind_address "$env_file"
# HTTPS / MQTTS turn on when tls/tls.crt and tls/tls.key exist (scripts/generate-tls-cert.sh).
mkdir -p "$project_root/tls"
configure_deepseek_env "$env_file"
configure_embedding_env "$env_file"

# AI 工作流服务（Harness）为必装组件：告警研判、巡检、报告、协议助手和规则草稿都通过它运行。

[ -n "$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_URL)" ] || set_deployment_env_value "$env_file" IOT_AI_HARNESS_URL http://deepseek-harness:8091
[ -n "$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_MCP_URL)" ] || set_deployment_env_value "$env_file" IOT_AI_HARNESS_MCP_URL http://platform-api:8080/mcp/harness
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
# The capacity-test module puts real load on the platform, so production
# deployments leave it off unless it was turned on before or here.
if [ "$capacity" = keep ]; then
  capacity=off
  [ "$(get_deployment_env_value "$env_file" IOT_CAPACITY_MODULE)" = on ] && capacity=on
fi
# The monitoring stack is deployed by default; an earlier --ops off is kept.
if [ "$ops" = keep ]; then
  ops=on
  [ "$(get_deployment_env_value "$env_file" IOT_OPS_MODULE)" = off ] && ops=off
fi
apply_ops_module "$env_file" "$ops"
# ClickHouse is deployed by default; an earlier --clickhouse off is kept.
if [ "$clickhouse" = keep ]; then
  clickhouse=on
  [ "$(get_deployment_env_value "$env_file" IOT_CLICKHOUSE_MODULE)" = off ] && clickhouse=off
fi
apply_clickhouse_module "$env_file" "$clickhouse"
if [ "$capacity" = on ]; then
  bash "$script_dir/capacity-module.sh" prepare --mode online --env-file "$env_file" --project-name "$project_name"
else
  set_deployment_env_value "$env_file" IOT_OPS_CAPACITY_URL ''
  set_deployment_env_value "$env_file" IOT_CAPACITY_MODULE off
  profiles="$(get_deployment_env_value "$env_file" COMPOSE_PROFILES | tr ',' '\n' | tr -d ' ' | grep -vx capacity | paste -sd, - || true)"
  set_deployment_env_value "$env_file" COMPOSE_PROFILES "$profiles"
fi
annotate_deployment_env_file "$env_file"
compose=(compose --project-name "$project_name" --env-file "$env_file" -f "$project_root/compose.yaml")
build_services=(platform-api platform-web backup-service minio postgres)
command -v git >/dev/null 2>&1 || { echo 'AI 工作流服务（Harness）为必装组件，构建需要安装 Git。' >&2; exit 1; }
build_services+=(deepseek-harness)
# 知识库向量计算与重排（同一镜像，模型在构建时下载并校验）。
build_services+=(embedding reranker)
sh "$script_dir/fetch-deepseek-harness.sh"
run_docker "${compose[@]}" config --quiet
services="$(docker "${compose[@]}" config --services)"
pull_services=()
while IFS= read -r service; do
  service="${service%$'\r'}"
  case "$service" in
    # minio-dr runs the MinIO image built here.
    platform-api|platform-web|backup-service|deepseek-harness|minio|minio-dr|postgres|embedding|reranker|'') ;;
    # 摄像头直播媒体服务（video profile，默认启用），由固定 digest 的官方镜像构建。
    zlmediakit) build_services+=(zlmediakit) ;;
    # 容量测试模块使用平台镜像，不单独拉取。
    capacity) ;;
    *) pull_services+=("$service");;
  esac
done <<< "$services"
echo '拉取运行依赖镜像……'
run_docker "${compose[@]}" pull "${pull_services[@]}"
echo '构建 API、前端、备份服务和知识库模型镜像……'
run_docker "${compose[@]}" build --pull "${build_services[@]}"
echo '启动服务……'
run_docker "${compose[@]}" up -d --no-build --pull never
if [ "$video" = off ]; then
  # Profile services are not removed by up; stop a media server left from an earlier deployment.
  run_docker "${compose[@]}" --profile video rm -sf zlmediakit
fi
if [ "$capacity" = off ]; then
  run_docker "${compose[@]}" --profile capacity rm -sf capacity
fi
if [ "$ops" = off ]; then
  run_docker "${compose[@]}" --profile ops rm -sf prometheus loki alloy grafana alertmanager node-exporter
fi
if [ "$clickhouse" = off ]; then
  # The data volume is kept, so turning ClickHouse on again restores its data.
  run_docker "${compose[@]}" --profile clickhouse rm -sf clickhouse clickhouse-tool-admin
fi

api_port="$(get_deployment_env_value "$env_file" IOT_API_PORT)"; api_port="${api_port:-8081}"
web_port="$(get_deployment_env_value "$env_file" IOT_WEB_PORT)"; web_port="${web_port:-8080}"
wait_deployment_http "http://127.0.0.1:$api_port/health/ready" "$health_timeout"
wait_deployment_http "http://127.0.0.1:$web_port/" "$health_timeout"
wait_deployment_http "http://127.0.0.1:$web_port/health/ready" "$health_timeout"
backup_port="$(get_deployment_env_value "$env_file" IOT_BACKUP_HTTP_PORT)"
wait_deployment_http "http://127.0.0.1:${backup_port:-8092}/health/ready" "$health_timeout"
harness_port="$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_PORT)"
wait_deployment_http "http://127.0.0.1:${harness_port:-8091}/health" "$health_timeout"
printf 'Harness 已启动；工作流模型为 %s。\n' "${model:-$(get_deployment_env_value "$env_file" IOT_AI_HARNESS_MODEL)}"
run_docker "${compose[@]}" ps
printf '在线部署完成：http://127.0.0.1:%s/；登录账号和密码查看 %s 中 IOT_ADMIN_USER / IOT_ADMIN_PASSWORD。\n' "$web_port" "$env_file"
[ "$capacity" = on ] && echo '容量测试模块已部署：在“运维中心 → 容量测试”选择预设即可运行；关闭用 --capacity off 或 scripts/capacity-module.sh disable。'
true
