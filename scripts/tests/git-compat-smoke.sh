#!/usr/bin/env bash
# Verifies deployment scripts against the Git version shipped by CentOS 7,
# which supports `git -c` but not the later global `git -C` option.
set -Eeuo pipefail

test_root="$(mktemp -d "${TMPDIR:-/tmp}/iot-git-compat-test.XXXXXX")"
trap 'rm -rf -- "$test_root"' EXIT
project_root="$(cd "$(dirname "$0")/../.." && pwd)"
fixture="$test_root/project"
expected_revision="$(tr -d '\r\n' < "$project_root/deploy/deepseek-harness/REVISION")"

mkdir -p \
  "$fixture/scripts" \
  "$fixture/deploy/deepseek-harness" \
  "$fixture/upstream/deepseek-harness/.git" \
  "$test_root/bin"
cp "$project_root/scripts/fetch-deepseek-harness.sh" "$fixture/scripts/"
cp "$project_root/deploy/deepseek-harness/REVISION" "$fixture/deploy/deepseek-harness/"

cat > "$test_root/bin/git" <<'EOF'
#!/usr/bin/env sh
if [ "${1:-}" = "-C" ]; then
  echo "Unknown option: -C" >&2
  exit 129
fi
case "${1:-} ${2:-}" in
  "status --porcelain")
    if [ -n "${FAKE_GIT_DIRTY_FLAG:-}" ] && [ -f "$FAKE_GIT_DIRTY_FLAG" ]; then
      rm -f "$FAKE_GIT_DIRTY_FLAG"
      printf ' M package.json\n'
    fi
    exit 0 ;;
  "rev-parse HEAD") printf '%s\n' "$EXPECTED_REVISION" ;;
  "-c http.version=HTTP/1.1")
    destination=''
    for argument in "$@"; do destination="$argument"; done
    mkdir -p "$destination/.git"
    exit 0 ;;
  *) echo "Unexpected git invocation: $*" >&2; exit 2 ;;
esac
EOF
chmod +x "$test_root/bin/git"

PATH="$test_root/bin:$PATH" EXPECTED_REVISION="$expected_revision" \
  sh "$fixture/scripts/fetch-deepseek-harness.sh" > "$test_root/output.log"

grep -qx "$expected_revision" "$fixture/upstream/deepseek-harness.revision"
grep -q "DeepSeek Harness ready: $expected_revision" "$test_root/output.log"

touch "$test_root/dirty.flag"
PATH="$test_root/bin:$PATH" EXPECTED_REVISION="$expected_revision" FAKE_GIT_DIRTY_FLAG="$test_root/dirty.flag" \
  sh "$fixture/scripts/fetch-deepseek-harness.sh" > "$test_root/dirty-output.log"
backup_count="$(find "$fixture/upstream" -maxdepth 1 -type d -name 'deepseek-harness.backup-*' | wc -l | tr -d ' ')"
[ "$backup_count" = 1 ]
[ -d "$fixture/upstream/deepseek-harness/.git" ]
grep -q '源码目录存在修改，已备份到：' "$test_root/dirty-output.log"

if grep -En 'git[[:space:]]+-C' \
  "$project_root/scripts/fetch-deepseek-harness.sh" \
  "$project_root/scripts/package-offline.sh"; then
  echo "部署脚本仍使用 CentOS 7 Git 不支持的全局 -C 参数" >&2
  exit 1
fi

echo 'PASS git compatibility: deployment scripts work without global git -C'
