#!/usr/bin/env bash
# Deploy a rendered cluster (go run ./cmd/cluster-render ...) to its nodes.
#
#   bash scripts/cluster-deploy.sh --rendered dist/cluster/iot-cluster --ssh-user deploy
#   bash scripts/cluster-deploy.sh --rendered dist/cluster/iot-cluster --stage workers --nodes n5
#   bash scripts/cluster-deploy.sh --rendered dist/cluster/iot-cluster --dry-run
#
# Stages start in order (coordination, data, cluster-init, support, workers,
# edge); each stage is waited for before the next. Rolling back means running
# this script again with the previously rendered directory. The script only
# acts on nodes listed in the rendered plan; it never deletes volumes.
set -euo pipefail

rendered=""
ssh_user="${USER:-root}"
remote_dir=""
stage_filter="all"
node_filter=""
images=""
dry_run=0
cluster_init="go run ./cmd/cluster-init"
health_timeout=300

usage() { sed -n '2,12p' "$0"; }
while [ $# -gt 0 ]; do
  case "$1" in
    --rendered) rendered="$2"; shift 2;;
    --ssh-user) ssh_user="$2"; shift 2;;
    --remote-dir) remote_dir="$2"; shift 2;;
    --stage) stage_filter="$2"; shift 2;;
    --nodes) node_filter=",$2,"; shift 2;;
    --images) images="$2"; shift 2;;
    --cluster-init) cluster_init="$2"; shift 2;;
    --health-timeout) health_timeout="$2"; shift 2;;
    --dry-run) dry_run=1; shift;;
    -h|--help) usage; exit 0;;
    *) echo "unknown option $1" >&2; usage >&2; exit 2;;
  esac
done
[ -n "$rendered" ] && [ -f "$rendered/deploy-plan.txt" ] || { echo "--rendered must point at a cluster-render output directory" >&2; exit 2; }
case "$stage_filter" in all|coordination|data|init|support|workers|edge) ;; *) echo "invalid --stage" >&2; exit 2;; esac
name="$(awk '$1=="#" && $2=="name" {print $3}' "$rendered/deploy-plan.txt")"
[ -n "$name" ] || { echo "deploy plan has no cluster name" >&2; exit 2; }
remote_dir="${remote_dir:-/opt/$name}"

run() {
  if [ "$dry_run" = 1 ]; then printf 'DRY-RUN %s\n' "$*"; else "$@"; fi
}
selected_node() { [ -z "$node_filter" ] || [[ "$node_filter" == *",$1,"* ]]; }
selected_stage() { [ "$stage_filter" = all ] || [ "$stage_filter" = "$1" ]; }

# 1. Copy each node's project (and optionally the offline image bundle).
copied=" "
while read -r kind stage node address services; do
  [ "$kind" = service ] || continue
  selected_node "$node" || continue
  case "$copied" in *" $node "*) continue;; esac
  copied="$copied$node "
  target="$ssh_user@$address"
  run ssh "$target" "mkdir -p '$remote_dir' && chmod 700 '$remote_dir'"
  run scp -r -p "$rendered/$node/." "$target:$remote_dir/"
  if [ -n "$images" ]; then
    run scp "$images" "$target:$remote_dir/images.tar"
    run ssh "$target" "docker load -i '$remote_dir/images.tar'"
  fi
done < "$rendered/deploy-plan.txt"

wait_stage() {
  local stage="$1"
  while read -r kind s node address services; do
    [ "$kind" = service ] && [ "$s" = "$stage" ] || continue
    selected_node "$node" || continue
    run ssh "$ssh_user@$address" "cd '$remote_dir' && timeout $health_timeout sh -c 'until ! docker compose -p $name ps --format \"{{.Health}}\" | grep -q -E \"starting|unhealthy\"; do sleep 3; done'"
  done < "$rendered/deploy-plan.txt"
}

start_stage() {
  local stage="$1"
  while read -r kind s node address services; do
    [ "$kind" = service ] && [ "$s" = "$stage" ] || continue
    selected_node "$node" || continue
    # shellcheck disable=SC2086
    run ssh "$ssh_user@$address" "cd '$remote_dir' && docker compose -p $name --env-file .env up -d --no-build --pull never $services"
  done < "$rendered/deploy-plan.txt"
  wait_stage "$stage"
}

for stage in coordination data init support workers edge; do
  selected_stage "$stage" || continue
  if [ "$stage" = init ]; then
    # Once per deployment: application database, schema, topics (no auto-create).
    # shellcheck disable=SC2086
    run $cluster_init -env-file "$rendered/init.env" -bootstrap-postgres -execute
    continue
  fi
  echo "== stage $stage"
  start_stage "$stage"
done

# 2. Every platform process must be ready (its own dependencies only).
if selected_stage edge || selected_stage workers || [ "$stage_filter" = all ]; then
  failed=0
  while read -r kind node url; do
    [ "$kind" = health ] || continue
    selected_node "$node" || continue
    if [ "$dry_run" = 1 ]; then
      printf 'DRY-RUN curl -fsS %s\n' "$url"
    elif ! curl -fsS --max-time 5 "$url" >/dev/null; then
      echo "not ready: $node $url" >&2
      failed=1
    fi
  done < "$rendered/deploy-plan.txt"
  [ "$failed" = 0 ] || { echo "some platform processes are not ready; see docker compose logs on those nodes" >&2; exit 1; }
fi
echo "cluster $name deployed; run a quick capacity check before opening traffic (docs/DEPLOYMENT.md#集群部署)"
