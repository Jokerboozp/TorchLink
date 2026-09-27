#!/usr/bin/env bash
# One-click cluster deployment and upgrade from a cluster inventory.
#
#   bash scripts/cluster-up.sh --inventory deploy/cluster/my-cluster.yaml --ssh-user deploy
#   bash scripts/cluster-up.sh --inventory <file> --bundle cluster-images.tar     # online machine: build + pull, save all images
#   bash scripts/cluster-up.sh --inventory <file> --ssh-user deploy --images cluster-images.tar   # offline
#   bash scripts/cluster-up.sh --inventory <file> --dry-run
#
# Steps: build the platform images (or load an offline bundle) → generate
# missing secrets → render every node → check the nodes over SSH (Docker,
# Compose, disk, clock, free ports) → send each node only the images it lacks
# → start stages in order, initialise databases and topics, check readiness.
# Running it again upgrades in place with the same secrets; the previous
# rendering is kept for rollback:
#   bash scripts/cluster-deploy.sh --rendered .cluster/<name>/rendered.prev --ssh-user <user>
# Requirements: Docker with Compose v2 and an OpenSSH client here; on every
# node Docker with Compose v2, usable by the SSH user without sudo.
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
inventory=""
ssh_user="${USER:-root}"
ssh_key=""
ssh_port=""
secrets=""
state_dir=""
images_tar=""
bundle=""
build=1
dry_run=0
assume_yes=0

usage() { sed -n '2,18p' "$0"; }
while [ $# -gt 0 ]; do
  case "$1" in
    --inventory) inventory="$2"; shift 2;;
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
[ -n "$inventory" ] && [ -f "$inventory" ] || { echo "--inventory must point at a cluster inventory file" >&2; usage >&2; exit 2; }

say() { printf '\n== %s\n' "$*"; }
fail() { printf 'error: %s\n' "$*" >&2; exit 1; }
abs() { (cd "$(dirname "$1")" && printf '%s/%s' "$(pwd)" "$(basename "$1")"); }
run() { if [ "$dry_run" = 1 ]; then printf 'DRY-RUN %s\n' "$*"; else "$@"; fi; }

ssh_opts=(-o BatchMode=yes -o ConnectTimeout=10)
[ -n "$ssh_key" ] && ssh_opts+=(-i "$ssh_key")
[ -n "$ssh_port" ] && ssh_opts+=(-p "$ssh_port")
remote() { local address="$1"; shift; ssh "${ssh_opts[@]}" "$ssh_user@$address" "$@"; }

# Top-level scalar or images.<key> from the inventory, without a YAML parser.
yaml_top() { awk -v k="$1" '$1==k":" {print $2; exit}' "$inventory" | tr -d '"'"'"; }
yaml_image() { awk -v k="$1" '/^images:/ {f=1; next} f && /^[^ #]/ {f=0} f && $1==k":" {print $2; exit}' "$inventory" | tr -d '"'"'"; }

name="$(yaml_top name)"
[ -n "$name" ] || fail "inventory has no name"
platform_image="$(yaml_image platform)"
[ -n "$platform_image" ] || fail "inventory has no images.platform"
inventory="$(abs "$inventory")"
state_dir="${state_dir:-$project_root/.cluster/$name}"
secrets="${secrets:-$state_dir/secrets.yaml}"
mkdir -p "$state_dir" "$(dirname "$secrets")"
chmod 700 "$state_dir" 2>/dev/null || true
state_dir="$(cd "$state_dir" && pwd)"
secrets="$(abs "$secrets")"
rendered="$state_dir/rendered"
# A dry run renders beside the real output and never replaces it.
[ "$dry_run" = 1 ] && rendered="$state_dir/rendered.dry-run"

say "local tools"
for tool in docker ssh scp gzip; do command -v "$tool" >/dev/null || fail "$tool is not installed on this machine"; done
docker info >/dev/null 2>&1 || fail "Docker is not running or not usable by $(id -un)"
docker compose version >/dev/null 2>&1 || fail "Docker Compose v2 (docker compose) is required"

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
if [ "$dry_run" = 0 ]; then
  docker image inspect "$platform_image" >/dev/null 2>&1 || fail "platform image $platform_image is missing (build it, or pass --images)"
fi

tool() {
  # cluster-render runs from the platform image; no Go toolchain is needed.
  local user_args=()
  [ "$(uname -s)" = Linux ] && user_args=(--user "$(id -u):$(id -g)")
  docker run --rm ${user_args[@]+"${user_args[@]}"} \
    -v "$(dirname "$inventory"):/in/inventory:ro" -v "$(dirname "$secrets"):/in/secrets" -v "$state_dir:/state" \
    --entrypoint /app/cluster-render "$platform_image" \
    -inventory "/in/inventory/$(basename "$inventory")" "$@"
}

if [ "$dry_run" = 1 ] && ! docker image inspect "$platform_image" >/dev/null 2>&1; then
  echo "DRY-RUN platform image not built yet; later steps are listed without the rendered plan"
  exit 0
fi
image_list="$(tool -print-images)"

if [ -z "$images_tar" ] && [ "$build" = 1 ]; then
  build_env=() build_services=()
  while read -r key image; do
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
  echo "copy $bundle, the checkout and the inventory to the offline controller and run with --images $bundle"
  exit 0
fi

# 2. Secrets and rendering. Existing secrets are kept; the previous rendering
# becomes rendered.prev for rollback.
say "rendering $name into $rendered"
if [ "$dry_run" = 1 ]; then
  rm -rf "$rendered"
elif [ -d "$rendered" ]; then
  rm -rf "$rendered.prev"; mv "$rendered" "$rendered.prev"
fi
tool -secrets "/in/secrets/$(basename "$secrets")" -init-secrets -out "/state/$(basename "$rendered")"
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
deploy_args=(--rendered "$rendered" --ssh-user "$ssh_user")
[ -n "$ssh_key" ] && deploy_args+=(--ssh-key "$ssh_key")
[ -n "$ssh_port" ] && deploy_args+=(--ssh-port "$ssh_port")
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
  Administrator: ${admin_user:-admin}; password is adminPassword in $secrets
  Back up $secrets — the cluster's databases were initialised with these values.
  Rollback: bash scripts/cluster-deploy.sh --rendered $rendered.prev --ssh-user $ssh_user
EOF
