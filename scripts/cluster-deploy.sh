#!/usr/bin/env bash
# Deploy a rendered cluster (go run ./cmd/cluster-render ...) to its nodes.
#
#   bash scripts/cluster-deploy.sh --rendered dist/cluster/iot-cluster --ssh-user deploy
#   bash scripts/cluster-deploy.sh --rendered dist/cluster/iot-cluster --stage workers --nodes n5
#   bash scripts/cluster-deploy.sh --rendered dist/cluster/iot-cluster --dry-run
#
# Stages start in order (coordination, data, cluster-init, support, workers,
# edge); each stage is waited for before the next. cluster-init runs on the
# first API node inside the platform image (no Go toolchain needed; pass
# --cluster-init "go run ./cmd/cluster-init" to run it locally instead).
# Rolling back means running this script again with the previously rendered
# directory. The script only acts on nodes listed in the rendered plan; it
# never deletes volumes. For the complete one-click flow use cluster-up.sh.
set -euo pipefail

rendered=""
ssh_user="${USER:-root}"
remote_dir=""
stage_filter="all"
node_filter=""
images=""
dry_run=0
cluster_init=""
health_timeout=300
init_attempts=20
ssh_opts=(-o BatchMode=yes -o ConnectTimeout=10)

usage() { sed -n '2,16p' "$0"; }
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
    --init-attempts) init_attempts="$2"; shift 2;;
    --ssh-key) ssh_opts+=(-i "$2" -o IdentitiesOnly=yes); shift 2;;
    --known-hosts) ssh_opts+=(-o "UserKnownHostsFile=$2" -o StrictHostKeyChecking=accept-new); shift 2;;
    --ssh-port) ssh_port="$2"; shift 2;;
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
platform_image="$(awk '$1=="#" && $2=="platform-image" {print $3}' "$rendered/deploy-plan.txt")"
scp_opts=("${ssh_opts[@]}")
if [ -n "${ssh_port:-}" ]; then ssh_opts+=(-p "$ssh_port"); scp_opts+=(-P "$ssh_port"); fi

run() {
  if [ "$dry_run" = 1 ]; then printf 'DRY-RUN %s\n' "$*"; else "$@"; fi
}
# -n: never read stdin, which the plan loops below are reading from.
ssh() { command ssh -n "${ssh_opts[@]}" "$@"; }
scp() { command scp "${scp_opts[@]}" "$@"; }
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

# cluster-init is idempotent: application database, schema, topics (no
# auto-create) and ClickHouse table checks. Patroni and Redpanda may still be
# electing leaders right after the data stage, so it is retried.
run_init() {
  local attempt=1 init_node="" init_address="" init_ca_file="" init_ca_mount=""
  if [ -f "$rendered/kafka/ca.pem" ]; then
    init_ca_file="$(cd "$rendered" && pwd)/kafka/ca.pem"
    init_ca_mount=" --volume '$remote_dir/.init-kafka-ca.pem:/app/kafka/ca.pem:ro'"
  fi
  if [ -z "$cluster_init" ]; then
    read -r _ init_node init_address < <(awk '$1=="health" {split($3,u,"[/:]"); print "x", $2, u[4]; exit}' "$rendered/deploy-plan.txt")
    [ -n "$init_address" ] && [ -n "$platform_image" ] || { echo "deploy plan lacks a platform node or image for cluster-init" >&2; exit 1; }
    run scp -p "$rendered/init.env" "$ssh_user@$init_address:$remote_dir/.init.env"
    if [ -n "$init_ca_file" ]; then
      run scp -p "$init_ca_file" "$ssh_user@$init_address:$remote_dir/.init-kafka-ca.pem"
    fi
  fi
  while :; do
    if [ -n "$cluster_init" ]; then
      # shellcheck disable=SC2086
      if (
        if [ -n "$init_ca_file" ]; then export IOT_KAFKA_TLS_CA_FILE="$init_ca_file"; fi
        run $cluster_init -env-file "$rendered/init.env" -bootstrap-postgres -execute
      ); then break; fi
    elif run ssh "$ssh_user@$init_address" "cd '$remote_dir' && docker run --rm --network host --env-file .init.env$init_ca_mount --entrypoint /app/cluster-init '$platform_image' -bootstrap-postgres -execute"; then
      break
    fi
    if [ "$attempt" -ge "$init_attempts" ]; then
      echo "cluster-init did not succeed after $attempt attempts; check PostgreSQL (Patroni leader), Redpanda and ClickHouse on the data nodes" >&2
      [ -z "$cluster_init" ] && run ssh "$ssh_user@$init_address" "rm -f '$remote_dir/.init.env' '$remote_dir/.init-kafka-ca.pem'"
      exit 1
    fi
    attempt=$((attempt + 1))
    echo "cluster-init not ready yet (attempt $attempt/$init_attempts), retrying in 15s" >&2
    sleep 15
  done
  [ -z "$cluster_init" ] && run ssh "$ssh_user@$init_address" "rm -f '$remote_dir/.init.env' '$remote_dir/.init-kafka-ca.pem'"
  while read -r kind node address init_service; do
    [ "$kind" = init-service ] || continue
    selected_node "$node" || continue
    run ssh "$ssh_user@$address" "cd '$remote_dir' && docker compose -p $name --env-file .env run --rm --no-deps $init_service"
  done < "$rendered/deploy-plan.txt"
  return 0
}

for stage in coordination data init support workers edge; do
  selected_stage "$stage" || continue
  if [ "$stage" = init ]; then
    echo "== stage init (database, schema, topics)"
    run_init
    continue
  fi
  echo "== stage $stage"
  start_stage "$stage"
done

# 2. Every platform process must be ready (its own dependencies only).
if selected_stage edge || selected_stage workers || [ "$stage_filter" = all ]; then
  # Processes need a while after their containers start (schema checks,
  # cluster DDL), so each is polled until the shared deadline.
  failed=0 deadline=$(( $(date +%s) + health_timeout ))
  while read -r kind node url; do
    [ "$kind" = health ] || continue
    selected_node "$node" || continue
    if [ "$dry_run" = 1 ]; then
      printf 'DRY-RUN curl -fsS %s\n' "$url"
      continue
    fi
    until curl -fsS --max-time 5 "$url" >/dev/null 2>&1; do
      if [ "$(date +%s)" -ge "$deadline" ]; then
        echo "not ready: $node $url" >&2
        failed=1
        break
      fi
      sleep 5
    done
  done < "$rendered/deploy-plan.txt"
  [ "$failed" = 0 ] || { echo "some platform processes are not ready; see docker compose logs on those nodes" >&2; exit 1; }
fi
echo "cluster $name deployed; run a quick capacity check before opening traffic (docs/DEPLOYMENT.md#集群部署)"
