#!/usr/bin/env bash
# One-click cluster deployment and upgrade.
#
#   bash scripts/cluster-up.sh                                   # wizard: nodes, SSH passwords, service password
#   bash scripts/cluster-up.sh --name torchlink                  # upgrade a cluster deployed before (no questions)
#   bash scripts/cluster-up.sh --name torchlink --capacity off   # capacity-test module off (on by default)
#   bash scripts/cluster-up.sh --inventory deploy/cluster/my.yaml   # hand-written inventory
#   bash scripts/cluster-up.sh --name torchlink --bundle cluster-images.tar   # online machine: save all images
#   bash scripts/cluster-up.sh --name torchlink --images cluster-images.tar   # offline controller
#   bash scripts/cluster-up.sh --dry-run
#
# The wizard asks for the node count and addresses, the SSH user (root by
# default), whether all nodes share one SSH password or each has its own, the
# unified service password (databases, Redis, ClickHouse, MinIO, EMQX console,
# platform administrator) and the optional modules. Passwords are only kept in
# memory: SSH passwords are used once to install a deployment key
# (.cluster/<name>/deploy_key); later runs log in with that key.
# Unattended: --nodes IP,IP,IP plus TORCHLINK_SSH_PASSWORD and
# TORCHLINK_SERVICE_PASSWORD in the environment.
#
# Then: build the platform images (or load an offline bundle) → install the
# deployment key → generate missing secrets → render every node → check the
# nodes (Docker, Compose, disk, clock, free ports) → send each node only the
# images it lacks → start stages in order, initialise databases and topics,
# check readiness. Rollback:
#   bash scripts/cluster-deploy.sh --rendered .cluster/<name>/rendered.prev --ssh-key .cluster/<name>/deploy_key --known-hosts .cluster/<name>/known_hosts
# Requirements: Docker with Compose v2 and an OpenSSH client here; on every
# node Docker with Compose v2, usable by the SSH user without sudo.
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
inventory=""
name=""
nodes_arg=""
video=""
capacity=""
ssh_user=""
ssh_key=""
ssh_port=""
secrets=""
state_dir=""
images_tar=""
bundle=""
build=1
dry_run=0
assume_yes=0

usage() { sed -n '2,32p' "$0"; }
while [ $# -gt 0 ]; do
  case "$1" in
    --inventory) inventory="$2"; shift 2;;
    --name) name="$2"; shift 2;;
    --nodes) nodes_arg="$2"; shift 2;;
    --video) video="$2"; shift 2;;
    --capacity) capacity="$2"; shift 2;;
    --ssh-user) ssh_user="$2"; shift 2;;
    --ssh-key) ssh_key="$2"; shift 2;;
    --ssh-port) ssh_port="$2"; shift 2;;
    --secrets) secrets="$2"; shift 2;;
    --state-dir) state_dir="$2"; shift 2;;
    --images) images_tar="$2"; shift 2;;
    --bundle) bundle="$2"; shift 2;;
    --no-build) build=0; shift;;
    --dry-run) dry_run=1; shift;;
    --yes|-y) assume_yes=1; shift;;
    -h|--help) usage; exit 0;;
    *) echo "unknown option $1" >&2; usage >&2; exit 2;;
  esac
done
case "$video" in ""|on|off) ;; *) echo "--video must be on or off" >&2; exit 2;; esac
case "$capacity" in ""|on|off) ;; *) echo "--capacity must be on or off" >&2; exit 2;; esac

say() { printf '\n== %s\n' "$*"; }
fail() { printf 'error: %s\n' "$*" >&2; exit 1; }
abs() { (cd "$(dirname "$1")" && printf '%s/%s' "$(pwd)" "$(basename "$1")"); }
run() { if [ "$dry_run" = 1 ]; then printf 'DRY-RUN %s\n' "$*"; else "$@"; fi; }
interactive=0
# TORCHLINK_FORCE_INTERACTIVE drives the wizard from a pipe (tests).
if [ -t 0 ] || [ "${TORCHLINK_FORCE_INTERACTIVE:-}" = 1 ]; then interactive=1; fi

# ask VAR "question" [default] — reads one line (default on empty input).
# Helper locals carry a prefix so they never shadow the caller's variable.
ask() {
  local _ask_value _ask_prompt="$2"
  [ -n "${3:-}" ] && _ask_prompt="$_ask_prompt [$3]"
  printf '%s: ' "$_ask_prompt" >&2
  IFS= read -r _ask_value || _ask_value=""
  printf -v "$1" '%s' "${_ask_value:-${3:-}}"
}
# ask_secret VAR "question" — hidden input, never echoed or logged.
ask_secret() {
  local _ask_value
  printf '%s: ' "$2" >&2
  IFS= read -rs _ask_value || _ask_value=""
  printf '\n' >&2
  printf -v "$1" '%s' "$_ask_value"
}
valid_ip() { [[ "$1" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ || "$1" == *:* ]]; }

# Top-level scalar or images.<key> from the inventory, without a YAML parser.
yaml_top() { awk -v k="$1" '$1==k":" {print $2; exit}' "$inventory" | tr -d '"'"'"; }
yaml_image() { awk -v k="$1" '/^images:/ {f=1; next} f && /^[^ #]/ {f=0} f && $1==k":" {print $2; exit}' "$inventory" | tr -d '"'"'"; }

# 0. Which cluster: a hand-written inventory, a cluster deployed before, or
# the wizard for a new one.
generate_nodes=""
if [ -n "$inventory" ]; then
  [ -f "$inventory" ] || fail "--inventory must point at a cluster inventory file"
  name="$(yaml_top name)"
  [ -n "$name" ] || fail "inventory has no name"
else
  if [ -z "$name" ]; then
    if [ "$interactive" = 1 ]; then ask name "集群名称" torchlink; else name=torchlink; fi
  fi
  [[ "$name" =~ ^[a-z][a-z0-9-]*$ ]] || fail "集群名称只能包含小写字母、数字和短横线，并以字母开头"
  inventory="${state_dir:-$project_root/.cluster/$name}/inventory.yaml"
  if [ ! -f "$inventory" ]; then
    if [ -n "$nodes_arg" ]; then
      generate_nodes="$nodes_arg"
    elif [ "$interactive" = 1 ]; then
      say "新集群 $name"
      while :; do
        ask count "节点数量（至少 3 台）" 3
        case "$count" in
          1) echo "单台服务器请使用单机部署：bash scripts/deploy-online.sh（离线为 scripts/deploy-offline.sh）" >&2; exit 1;;
          2) echo "2 个节点无法形成数据库与消息的仲裁，请输入 3 或更多" >&2;;
          *) [[ "$count" =~ ^[0-9]+$ ]] && [ "$count" -ge 3 ] && break; echo "请输入不小于 3 的整数" >&2;;
        esac
      done
      addresses=()
      for i in $(seq 1 "$count"); do
        while :; do
          ask ip "第 $i 个节点的 IP"
          valid_ip "$ip" || { echo "不是有效的 IP 地址" >&2; continue; }
          case " ${addresses[*]:-} " in *" $ip "*) echo "该 IP 已输入过" >&2; continue;; esac
          addresses+=("$ip"); break
        done
      done
      generate_nodes="$(IFS=,; printf '%s' "${addresses[*]}")"
      if [ -z "$video" ]; then
        ask answer "部署摄像头直播模块？(y/n)" y
        case "$answer" in n|N|no) video=off;; *) video=on;; esac
      fi
      if [ -z "$capacity" ]; then
        ask answer "部署容量测试模块？(y/n)" y
        case "$answer" in n|N|no) capacity=off;; *) capacity=on;; esac
      fi
    else
      fail "no cluster named $name yet: run interactively, or pass --nodes IP,IP,IP (or --inventory)"
    fi
  fi
fi
state_dir="${state_dir:-$project_root/.cluster/$name}"
secrets="${secrets:-$state_dir/secrets.yaml}"
mkdir -p "$state_dir" "$(dirname "$secrets")"
chmod 700 "$state_dir" 2>/dev/null || true
state_dir="$(cd "$state_dir" && pwd)"
secrets="$(abs "$secrets")"
inventory="$(abs "$inventory")"
rendered="$state_dir/rendered"
# A dry run renders beside the real output and never replaces it.
[ "$dry_run" = 1 ] && rendered="$state_dir/rendered.dry-run"

# SSH user and port are remembered per cluster (not secret).
ssh_conf="$state_dir/ssh.conf"
if [ -f "$ssh_conf" ]; then
  [ -n "$ssh_user" ] || ssh_user="$(awk -F= '$1=="user" {print $2}' "$ssh_conf")"
  [ -n "$ssh_port" ] || ssh_port="$(awk -F= '$1=="port" {print $2}' "$ssh_conf")"
elif [ -n "$generate_nodes" ] && [ "$interactive" = 1 ]; then
  [ -n "$ssh_user" ] || ask ssh_user "SSH 用户名" root
  [ -n "$ssh_port" ] || ask ssh_port "SSH 端口" 22
fi
ssh_user="${ssh_user:-root}"
ssh_port="${ssh_port:-22}"
[[ "$ssh_port" =~ ^[0-9]+$ ]] || fail "SSH 端口必须是数字"
known_hosts="$state_dir/known_hosts"
deploy_key="${ssh_key:-$state_dir/deploy_key}"

# SSH passwords: asked up front for a new cluster; for an existing one only
# when the deployment key no longer logs in (checked after the image build).
ssh_password_mode=""   # unified | per-node
ssh_default_password="${TORCHLINK_SSH_PASSWORD:-}"
ssh_node_passwords=()  # "ip=password" entries
collect_ssh_passwords() {
  local list=("$@") mode pw
  if [ -n "$ssh_default_password" ]; then ssh_password_mode=unified; return; fi
  [ "$interactive" = 1 ] || fail "SSH 登录需要密码：交互运行，或设置 TORCHLINK_SSH_PASSWORD"
  echo "SSH 登录方式：1) 所有节点统一密码  2) 每个节点独立密码" >&2
  ask mode "请选择" 1
  if [ "$mode" = 2 ]; then
    ssh_password_mode=per-node
    for ip in "${list[@]}"; do
      ask_secret pw "$ssh_user@$ip 的 SSH 密码"
      [ -n "$pw" ] || fail "密码不能为空"
      ssh_node_passwords+=("$ip=$pw")
    done
  else
    ssh_password_mode=unified
    ask_secret ssh_default_password "所有节点的 SSH 密码（$ssh_user）"
    [ -n "$ssh_default_password" ] || fail "密码不能为空"
  fi
}
if [ -n "$generate_nodes" ] && [ -z "$ssh_key" ] && [ ! -f "$deploy_key" ] && [ "$dry_run" = 0 ]; then
  IFS=, read -r -a new_addresses <<< "$generate_nodes"
  collect_ssh_passwords "${new_addresses[@]}"
fi

# Unified service password and DeepSeek key: only when the secrets file is
# created; an existing cluster keeps the passwords it was initialised with.
service_password="${TORCHLINK_SERVICE_PASSWORD:-}"
deepseek_key=""
if [ ! -f "$secrets" ] && [ "$dry_run" = 0 ] && [ "$interactive" = 1 ]; then
  if [ -z "$service_password" ]; then
    echo "服务统一密码用于 PostgreSQL、Redis、ClickHouse、MinIO、MQTT、Kafka、EMQX 控制台和平台管理员 admin；至少 8 位，只能包含字母、数字和 . _ ~ -" >&2
    while :; do
      ask_secret service_password "服务统一密码（直接回车使用 admin123，内部令牌独立随机）"
      [ -z "$service_password" ] && break
      if [ "${#service_password}" -lt 8 ] || ! [[ "$service_password" =~ ^[A-Za-z0-9._~-]+$ ]]; then
        echo "密码不符合要求，请重新输入" >&2; continue
      fi
      ask_secret again "再次输入服务统一密码"
      [ "$again" = "$service_password" ] && break
      echo "两次输入不一致，请重新输入" >&2
    done
  fi
  ask_secret deepseek_key "DeepSeek API Key（可留空，部署后也可在“模型管理”填写）"
fi

say "local tools"
for tool in docker ssh scp gzip; do command -v "$tool" >/dev/null || fail "$tool is not installed on this machine"; done
docker info >/dev/null 2>&1 || fail "Docker is not running or not usable by $(id -un)"
docker compose version >/dev/null 2>&1 || fail "Docker Compose v2 (docker compose) is required"

# The platform image name comes from the inventory, or the generator's default.
if [ -f "$inventory" ]; then platform_image="$(yaml_image platform)"; else platform_image="iot-platform-api:offline"; fi
[ -n "$platform_image" ] || fail "inventory has no images.platform"

# 1. Images: build from this checkout, or load an offline bundle.
own_keys=" platform web harness backup video "
if [ -n "$images_tar" ]; then
  say "loading offline image bundle $images_tar"
  [ -f "$images_tar" ] || fail "$images_tar not found"
  run docker load -i "$images_tar"
elif [ "$build" = 1 ]; then
  say "building platform image $platform_image"
  run docker build -t "$platform_image" -f "$project_root/Dockerfile" "$project_root"
fi
if [ "$dry_run" = 1 ] && ! docker image inspect "$platform_image" >/dev/null 2>&1; then
  echo "DRY-RUN platform image not built yet; later steps are listed without the rendered plan"
  exit 0
fi
docker image inspect "$platform_image" >/dev/null 2>&1 || fail "platform image $platform_image is missing (build it, or pass --images)"

# Tools run from the platform image; no Go toolchain is needed here.
user_args=()
[ "$(uname -s)" = Linux ] && user_args=(--user "$(id -u):$(id -g)")
image_tool() {
  local entry="$1"; shift
  docker run --rm -i ${user_args[@]+"${user_args[@]}"} \
    -v "$(dirname "$inventory"):/in/inventory" -v "$(dirname "$secrets"):/in/secrets" -v "$state_dir:/state" \
    --entrypoint "/app/$entry" "$platform_image" "$@"
}
tool() { image_tool cluster-render -inventory "/in/inventory/$(basename "$inventory")" "$@" < /dev/null; }

if [ -n "$generate_nodes" ]; then
  say "generating inventory $inventory"
  video_flag=true; [ "$video" = off ] && video_flag=false
  capacity_flag=true; [ "$capacity" = off ] && capacity_flag=false
  image_tool cluster-render -generate -name "$name" -nodes "$generate_nodes" -video="$video_flag" -capacity="$capacity_flag" -inventory "/in/inventory/$(basename "$inventory")" < /dev/null
elif [ -n "$capacity" ]; then
  # Module switch on an existing cluster: only the inventory entry changes.
  image_tool cluster-render -set-capacity "$capacity" -inventory "/in/inventory/$(basename "$inventory")" < /dev/null
fi
printf 'user=%s\nport=%s\n' "$ssh_user" "$ssh_port" > "$ssh_conf"
image_list="$(tool -print-images)"
node_addresses="$(tool -print-nodes | awk '{print $2}' | paste -sd, -)"

if [ -z "$images_tar" ] && [ "$build" = 1 ]; then
  build_env=() build_services=()
  while read -r key image; do
    if [ "$key" = postgres ] && [[ "$image" == iot-platform-postgres-ha:* ]]; then
      run docker build --pull -t "$image" -f "$project_root/deploy/postgres/Dockerfile.spilo" "$project_root/deploy/postgres"
      continue
    fi
    if [ "$key" = minio ] && [[ "$image" == iot-platform-minio:* ]]; then
      run docker build --pull -t "$image" "$project_root/deploy/minio"
      continue
    fi
    case "$own_keys" in *" $key "*) ;; *) continue;; esac
    [[ "$image" == *@sha256:* ]] && continue   # pinned digests are pulled, not rebuilt
    case "$key" in
      web) build_env+=("IOT_PLATFORM_WEB_IMAGE=$image"); build_services+=(platform-web);;
      harness) build_env+=("IOT_DEEPSEEK_HARNESS_IMAGE=$image"); build_services+=(deepseek-harness);;
      backup) build_env+=("IOT_BACKUP_IMAGE=$image"); build_services+=(backup-service);;
      video) build_env+=("IOT_ZLMEDIAKIT_IMAGE=$image"); build_services+=(zlmediakit);;
    esac
  done <<< "$image_list"
  if [ "${#build_services[@]}" -gt 0 ]; then
    say "building ${build_services[*]}"
    # compose.yaml requires these runtime secrets even to build; the images
    # never contain them, so placeholders are enough here.
    build_env+=("IOT_JWT_SECRET=${IOT_JWT_SECRET:-build-only-placeholder}" "IOT_ADMIN_PASSWORD=${IOT_ADMIN_PASSWORD:-build-only-placeholder}")
    run env "${build_env[@]}" docker compose -f "$project_root/compose.yaml" --project-directory "$project_root" --profile video build "${build_services[@]}"
  fi
fi

say "checking third-party images"
missing=()
while read -r key image; do
  docker image inspect "$image" >/dev/null 2>&1 && continue
  case "$own_keys" in *" $key "*) [[ "$image" == *@sha256:* ]] || { missing+=("$image"); continue; };; esac
  if [ "$dry_run" = 1 ]; then printf 'DRY-RUN docker pull %s\n' "$image"; continue; fi
  docker pull "$image" >/dev/null || missing+=("$image")
done <<< "$image_list"
if [ "${#missing[@]}" -gt 0 ]; then
  msg="images not available locally: ${missing[*]} (connect this machine to the registry or pass an offline bundle with --images)"
  if [ "$dry_run" = 1 ]; then echo "DRY-RUN $msg"; else fail "$msg"; fi
fi

if [ -n "$bundle" ]; then
  say "saving every cluster image into $bundle"
  # shellcheck disable=SC2046
  run docker save -o "$bundle" $(awk '{print $2}' <<< "$image_list" | sort -u)
  echo "copy $bundle, the checkout and $state_dir to the offline controller and run with --images $bundle"
  exit 0
fi

# 1b. SSH: install the deployment key with the passwords (once), or confirm
# the existing key still works. A key passed with --ssh-key is used as is.
if [ -z "$ssh_key" ]; then
  say "preparing SSH access ($ssh_user, port $ssh_port)"
  key_args=(-key /state/deploy_key -known-hosts /state/known_hosts -user "$ssh_user" -port "$ssh_port" -comment "torchlink-deploy@$name")
  if [ "$dry_run" = 1 ]; then
    printf 'DRY-RUN cluster-ssh bootstrap -nodes %s (passwords on stdin)\n' "$node_addresses"
  else
    if ! image_tool cluster-ssh check "${key_args[@]}" -nodes "$node_addresses" < /dev/null > "$state_dir/.ssh-check" 2>&1; then
      if [ -z "$ssh_password_mode" ]; then
        IFS=, read -r -a need <<< "$(awk '$1=="fail" {sub(/:$/, "", $2); print $2}' "$state_dir/.ssh-check" | paste -sd, -)"
        echo "部署密钥尚不能登录：${need[*]}" >&2
        collect_ssh_passwords "${need[@]}"
      fi
      if ! { [ -n "$ssh_default_password" ] && printf 'default=%s\n' "$ssh_default_password"
             for entry in ${ssh_node_passwords[@]+"${ssh_node_passwords[@]}"}; do printf '%s\n' "$entry"; done
           } | image_tool cluster-ssh bootstrap "${key_args[@]}" -nodes "$node_addresses"; then
        fail "SSH preparation failed on the nodes above (/state/known_hosts above is $known_hosts)"
      fi
    fi
    rm -f "$state_dir/.ssh-check"
  fi
fi
ssh_default_password="" ssh_node_passwords=()
ssh_opts=(-o BatchMode=yes -o ConnectTimeout=10 -o IdentitiesOnly=yes -i "$deploy_key" -o "UserKnownHostsFile=$known_hosts" -o StrictHostKeyChecking=accept-new -p "$ssh_port")
remote() { local address="$1"; shift; ssh "${ssh_opts[@]}" "$ssh_user@$address" "$@"; }

# 2. Secrets and rendering. Existing secrets are kept; the previous rendering
# becomes rendered.prev for rollback. Wizard values travel on stdin only.
say "rendering $name into $rendered"
if [ "$dry_run" = 1 ]; then
  rm -rf "$rendered"
elif [ -d "$rendered" ]; then
  rm -rf "$rendered.prev"; mv "$rendered" "$rendered.prev"
fi
{ [ -n "$service_password" ] && printf 'servicePassword=%s\n' "$service_password"
  [ -n "$deepseek_key" ] && printf 'deepseekApiKey=%s\n' "$deepseek_key"
  true
} | image_tool cluster-render -inventory "/in/inventory/$(basename "$inventory")" -secrets "/in/secrets/$(basename "$secrets")" -init-secrets -secrets-stdin -out "/state/$(basename "$rendered")"
service_password="" deepseek_key=""
chmod 600 "$secrets" 2>/dev/null || true
plan="$rendered/deploy-plan.txt"
[ -f "$plan" ] || fail "rendering produced no deploy plan"
remote_dir="/opt/$name"

# 3. Node preflight: collect every problem before changing anything.
say "checking nodes"
problems=() upgrade_nodes=()
local_now="$(date +%s)"
while read -r kind node address rest; do
  [ "$kind" = ports ] || continue
  ports="$rest"
  if [ "$dry_run" = 1 ]; then printf 'DRY-RUN ssh %s@%s (docker, compose, disk, clock, ports)\n' "$ssh_user" "$address"; continue; fi
  # shellcheck disable=SC2086
  if ! out="$(remote "$address" sh -s -- "$name" $ports 2>&1 <<'NODE_CHECK'
project="$1"; shift
docker info --format 'docker={{.ServerVersion}}' 2>/dev/null || echo 'docker=unusable'
echo "compose=$(docker compose version --short 2>/dev/null || echo missing)"
usage="$(df -Pk /var/lib/docker 2>/dev/null || df -Pk /)"
echo "disk=$(printf '%s\n' "$usage" | awk 'NR==2 {print int($4/1048576)}')"
echo "clock=$(date +%s)"
echo "running=$(docker ps -q --filter "label=com.docker.compose.project=$project" 2>/dev/null | wc -l | tr -d ' ')"
listening="$( (ss -ltnH 2>/dev/null || netstat -ltn 2>/dev/null) | awk '{print $4}')"
busy=""
for p in "$@"; do
  if printf '%s\n' "$listening" | grep -Eq "[:.]$p\$"; then busy="$busy $p"; fi
done
echo "busy=$busy"
NODE_CHECK
)"; then
    problems+=("$node ($address): SSH failed as $ssh_user — $(printf '%s' "$out" | tail -1)")
    continue
  fi
  value() { printf '%s\n' "$out" | awk -F= -v k="$1" '$1==k {sub(/^[^=]*=/, ""); print; exit}'; }
  [ "$(value docker)" = unusable ] && problems+=("$node: Docker is not installed or $ssh_user cannot use it without sudo (add the user to the docker group)")
  [ "$(value compose)" = missing ] && problems+=("$node: Docker Compose v2 plugin is missing")
  disk="$(value disk)"
  if [ -n "$disk" ] && [ "$disk" -lt 20 ]; then problems+=("$node: only ${disk}GiB free for Docker (need at least 20GiB)"); fi
  clock="$(value clock)"; skew=$(( ${clock:-$local_now} - local_now )); skew=${skew#-}
  if [ "$skew" -gt 5 ]; then problems+=("$node: clock differs from this machine by ${skew}s; enable NTP/chrony on all nodes"); fi
  running="$(value running | tr -d ' ')"
  if [ "${running:-0}" -gt 0 ]; then
    upgrade_nodes+=("$node")
  elif [ -n "$(value busy | tr -d ' ')" ]; then
    problems+=("$node: ports already in use by other programs:$(value busy)")
  fi
done < "$plan"
if [ "${#problems[@]}" -gt 0 ]; then
  printf 'node check failed:\n' >&2
  printf '  - %s\n' "${problems[@]}" >&2
  exit 1
fi

if [ "$dry_run" = 0 ] && [ "$assume_yes" = 0 ] && [ -t 0 ]; then
  mode="first deployment"
  [ "${#upgrade_nodes[@]}" -gt 0 ] && mode="upgrade (running on: ${upgrade_nodes[*]})"
  printf '\nReady to deploy %s: %s, %s nodes. Continue? [y/N] ' "$name" "$mode" "$(grep -c '^ports ' "$plan")"
  read -r answer
  case "$answer" in y|Y|yes) ;; *) echo "cancelled"; exit 1;; esac
fi

# 4. Send each node only the images it lacks (compared by image ID).
say "distributing images"
while read -r kind node address list; do
  [ "$kind" = images ] || continue
  # shellcheck disable=SC2206
  imgs=($list)
  if [ "$dry_run" = 1 ]; then printf 'DRY-RUN docker save <missing of %s> | gzip | ssh %s docker load\n' "${#imgs[@]}" "$address"; continue; fi
  remote_ids="$(remote "$address" "for i in ${imgs[*]}; do docker image inspect -f '{{.Id}}' \"\$i\" 2>/dev/null || echo missing; done" < /dev/null)"
  send=() i=0
  while read -r rid; do
    [ "$rid" = "$(docker image inspect -f '{{.Id}}' "${imgs[$i]}")" ] || send+=("${imgs[$i]}")
    i=$((i + 1))
  done <<< "$remote_ids"
  if [ "${#send[@]}" = 0 ]; then echo "$node: images up to date"; continue; fi
  echo "$node: sending ${#send[@]} image(s): ${send[*]}"
  docker save "${send[@]}" | gzip -1 | remote "$address" "gunzip | docker load" >/dev/null
done < "$plan"

# 5. Start, initialise and check (same script as manual/partial deployments).
deploy_args=(--rendered "$rendered" --ssh-user "$ssh_user" --ssh-key "$deploy_key" --known-hosts "$known_hosts" --ssh-port "$ssh_port")
[ "$dry_run" = 1 ] && deploy_args+=(--dry-run)
bash "$project_root/scripts/cluster-deploy.sh" "${deploy_args[@]}"

if [ "$dry_run" = 0 ]; then
  { printf 'deployed_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"; printf 'commit=%s\n' "$(git -C "$project_root" rev-parse --short HEAD 2>/dev/null || echo unknown)"; } > "$state_dir/last-deploy.txt"
fi
admin_user="$(awk '/^env:/ {f=1; next} f && /^[^ #]/ {f=0} f && $1=="IOT_ADMIN_USER:" {print $2}' "$inventory")"
if [ "$dry_run" = 1 ]; then
  say "dry run finished: nothing was changed on the nodes (rendering in $rendered)"
  awk '$1=="entry" {printf "  %-12s %s\n", $2, $3}' "$plan"
  exit 0
fi
say "cluster $name is up"
awk '$1=="entry" {printf "  %-12s %s\n", $2, $3}' "$plan"
cat <<EOF
  Put DNS, a VIP or an external load balancer in front of each group above.
  Administrator: ${admin_user:-admin}; password is adminPassword in $secrets (the service password when you set one)
  Back up $state_dir — the databases were initialised with these secrets; deploy_key logs in to the nodes.
  Upgrade: bash scripts/cluster-up.sh --name $name
  Rollback: bash scripts/cluster-deploy.sh --rendered $rendered.prev --ssh-user $ssh_user --ssh-key $deploy_key --known-hosts $known_hosts --ssh-port $ssh_port
EOF
