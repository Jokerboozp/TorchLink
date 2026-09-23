#!/usr/bin/env bash
# Exercise setup-local's bootstrap ordering without installing host software.
# 执行当前脚本步骤。
set -Eeuo pipefail
# 执行当前脚本步骤。
project_root="$(cd "$(dirname "$0")/../.." && pwd)"
# 执行当前脚本步骤。
test_root="$(mktemp -d)"
# 执行当前脚本步骤。
trap 'rm -rf -- "$test_root"' EXIT
# 执行当前脚本步骤。
mkdir -p "$test_root/scripts/lib"
# 执行当前脚本步骤。
cp "$project_root/scripts/setup-local.sh" "$test_root/scripts/"
# 执行当前脚本步骤。
cp "$project_root/scripts/lib/"*.sh "$test_root/scripts/lib/"
# 执行当前脚本步骤。
export LOCAL_BOOTSTRAP_MARKER="$test_root/bootstrapped"
# 执行当前脚本步骤。
cat >> "$test_root/scripts/lib/docker-bootstrap.sh" <<'EOF'
ensure_deployment_docker() {
  [ "$1" = online ] || exit 93
  touch "$LOCAL_BOOTSTRAP_MARKER"
}
EOF
# 执行当前脚本步骤。
cat >> "$test_root/scripts/lib/deployment.sh" <<'EOF'
assert_docker_available() {
  [ -f "$LOCAL_BOOTSTRAP_MARKER" ] || { echo 'FAIL: local setup did not bootstrap missing Docker' >&2; exit 94; }
  # Stop before generating credentials or starting containers.
  exit 0
}
EOF
# 执行当前脚本步骤。
bash "$test_root/scripts/setup-local.sh" --skip-code-deps
# 执行当前脚本步骤。
echo 'PASS local setup bootstraps Docker before checking availability'
