#!/usr/bin/env bash
# Exercise setup-local's bootstrap ordering without installing host software.
set -Eeuo pipefail
project_root="$(cd "$(dirname "$0")/../.." && pwd)"
test_root="$(mktemp -d)"
trap 'rm -rf -- "$test_root"' EXIT
mkdir -p "$test_root/scripts/lib"
cp "$project_root/scripts/setup-local.sh" "$test_root/scripts/"
cp "$project_root/scripts/lib/"*.sh "$test_root/scripts/lib/"
export LOCAL_BOOTSTRAP_MARKER="$test_root/bootstrapped"
cat >> "$test_root/scripts/lib/docker-bootstrap.sh" <<'EOF'
ensure_deployment_docker() {
  [ "$1" = online ] || exit 93
  touch "$LOCAL_BOOTSTRAP_MARKER"
}
EOF
cat >> "$test_root/scripts/lib/deployment.sh" <<'EOF'
assert_docker_available() {
  [ -f "$LOCAL_BOOTSTRAP_MARKER" ] || { echo 'FAIL: local setup did not bootstrap missing Docker' >&2; exit 94; }
  # Stop before generating credentials or starting containers.
  exit 0
}
EOF
bash "$test_root/scripts/setup-local.sh" --skip-code-deps
echo 'PASS local setup bootstraps Docker before checking availability'
