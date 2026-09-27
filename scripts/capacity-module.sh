#!/usr/bin/env bash
# Deploy-level switch for the capacity-test module (Compose profile
# "capacity"). It is off by default; IOT_CAPACITY_MODULE=on in the environment
# file records the choice so later deployments keep it. When on, the platform
# shows 运维中心 → 容量测试 and tests run from there with the operator's own
# permissions; nothing else needs configuring.
set -Eeuo pipefail
script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
project_root="$(dirname -- "$script_dir")"
# shellcheck source=lib/deployment.sh
source "$script_dir/lib/deployment.sh"

usage() {
  cat <<'EOF'
用法：bash scripts/capacity-module.sh <enable|disable|prepare|status|logs> [选项]
  enable    部署并启动容量测试服务，平台出现“运维中心 → 容量测试”（首次生成服务令牌）
  disable   停止并移除容量测试服务，页面隐藏；保留测试结果卷与令牌，后续部署保持关闭
  prepare   只写入开启配置，不操作容器；供部署脚本调用
  unprepare 只写入关闭配置，不操作容器；供部署脚本调用
  status    查看容量测试服务与配置状态
  logs      查看容量测试服务最近日志
选项：
  --mode online|offline        部署方式，默认 online（本地源码调试不提供此模块）
  --env-file PATH              配置文件（默认 online=.env.online，offline=.env.offline）
  --project-name NAME          Compose 项目（默认 online=iot-platform-online，offline=iot-platform）
EOF
}

action="${1:-}"; [ "$#" -gt 0 ] && shift || true
mode=online env_file='' project_name=''
while [ "$#" -gt 0 ]; do
  case "$1" in
    --mode|--env-file|--project-name)
      [ "$#" -ge 2 ] && [ -n "$2" ] || { printf '参数缺少值：%s\n' "$1" >&2; exit 1; }
      case "$1" in --mode) mode="$2";; --env-file) env_file="$2";; --project-name) project_name="$2";; esac
      shift 2;;
    -h|--help) usage; exit 0;;
    *) printf '未知参数：%s\n' "$1" >&2; usage >&2; exit 1;;
  esac
done
case "$action" in enable|disable|prepare|unprepare|status|logs) ;; *) usage >&2; exit 1;; esac

compose_files=(-f "$project_root/compose.yaml")
case "$mode" in
  online) env_file="${env_file:-.env.online}"; project_name="${project_name:-iot-platform-online}";;
  offline) env_file="${env_file:-.env.offline}"; project_name="${project_name:-iot-platform}"; compose_files+=(-f "$project_root/compose.offline.yaml");;
  *) echo '--mode 只能是 online 或 offline；本地源码调试可直接运行 go run ./cmd/capacity-test serve --self（见 docs/DEVELOPMENT.md）。' >&2; exit 1;;
esac
case "$env_file" in /*|[A-Za-z]:[\\/]*) ;; *) env_file="$project_root/$env_file";; esac
[ -f "$env_file" ] || { printf '配置文件不存在：%s（请先完成部署）\n' "$env_file" >&2; exit 1; }

compose=(compose --project-name "$project_name" --env-file "$env_file" "${compose_files[@]}" --profile capacity)

# Add or remove "capacity" in COMPOSE_PROFILES while keeping other profiles.
set_profile() {
  local want="$1" current next='' item
  current="$(get_deployment_env_value "$env_file" COMPOSE_PROFILES)"
  local items=()
  IFS=',' read -r -a items <<< "$current"
  for item in ${items[@]+"${items[@]}"}; do
    item="${item// /}"
    [ -n "$item" ] && [ "$item" != capacity ] && next="${next:+$next,}$item"
  done
  [ "$want" = on ] && next="${next:+$next,}capacity"
  set_deployment_env_value "$env_file" COMPOSE_PROFILES "$next"
}

prepare_config() {
  [ -n "$(get_deployment_env_value "$env_file" IOT_OPS_CAPACITY_TOKEN)" ] || set_deployment_env_value "$env_file" IOT_OPS_CAPACITY_TOKEN "$(deployment_secret)"
  set_deployment_env_value "$env_file" IOT_OPS_CAPACITY_URL http://capacity:7080
  set_deployment_env_value "$env_file" IOT_CAPACITY_MODULE on
  set_profile on
  annotate_deployment_env_file "$env_file"
}

disable_config() {
  set_deployment_env_value "$env_file" IOT_OPS_CAPACITY_URL ''
  set_deployment_env_value "$env_file" IOT_CAPACITY_MODULE off
  set_profile off
  annotate_deployment_env_file "$env_file"
}

case "$action" in
  prepare)
    prepare_config;;
  unprepare)
    disable_config;;
  enable)
    assert_docker_available
    prepare_config
    run_docker "${compose[@]}" config --quiet
    run_docker "${compose[@]}" up -d --no-build --pull never capacity
    # Recreate the API so it reads IOT_OPS_CAPACITY_URL / TOKEN.
    run_docker "${compose[@]}" up -d --no-deps --no-build platform-api
    echo '容量测试模块已启用：登录平台后在“运维中心 → 容量测试”选择预设即可运行（需要运维租户的容量测试权限）。';;
  disable)
    assert_docker_available
    disable_config
    run_docker "${compose[@]}" stop capacity || true
    run_docker "${compose[@]}" rm -f capacity || true
    run_docker compose --project-name "$project_name" --env-file "$env_file" "${compose_files[@]}" up -d --no-deps --no-build platform-api
    echo '容量测试服务已停止并移除，页面已隐藏。测试结果卷与服务令牌保留；再次 enable 即可恢复。';;
  status)
    printf '配置文件：%s\n' "$env_file"
    printf '模块：%s\n' "$(get_deployment_env_value "$env_file" IOT_CAPACITY_MODULE | sed 's/^$/off/')"
    run_docker "${compose[@]}" ps capacity;;
  logs)
    run_docker "${compose[@]}" logs --tail 200 capacity;;
esac
