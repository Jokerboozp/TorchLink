#!/usr/bin/env bash
# Verifies deployment scripts against the Git version shipped by CentOS 7,
# which supports `git -c` but not the later global `git -C` option.
# 执行当前脚本步骤。
set -Eeuo pipefail

# 执行当前脚本步骤。
test_root="$(mktemp -d "${TMPDIR:-/tmp}/iot-git-compat-test.XXXXXX")"
# 执行当前脚本步骤。
trap 'rm -rf -- "$test_root"' EXIT
# 执行当前脚本步骤。
project_root="$(cd "$(dirname "$0")/../.." && pwd)"
# 结束当前控制块。
fixture="$test_root/project"
# 执行当前脚本步骤。
expected_revision="$(tr -d '\r\n' < "$project_root/deploy/deepseek-harness/REVISION")"

mkdir -p \
  "$fixture/scripts" \
  "$fixture/deploy/deepseek-harness" \
  "$fixture/upstream/deepseek-harness/.git" \
  "$test_root/bin"
# 执行当前脚本步骤。
cp "$project_root/scripts/fetch-deepseek-harness.sh" "$fixture/scripts/"
# 执行当前脚本步骤。
cp "$project_root/deploy/deepseek-harness/REVISION" "$fixture/deploy/deepseek-harness/"

# 执行当前脚本步骤。
cat > "$test_root/bin/git" <<'EOF'
#!/usr/bin/env sh
if [ "${1:-}" = "-C" ]; then
  echo "Unknown option: -C" >&2
  exit 129
fi
case "${1:-} ${2:-}" in
  "status --porcelain")
    if [ -f .fresh-clone-dirty ]; then
      printf ' M package.json\n'
      exit 0
    fi
    if [ -n "${FAKE_GIT_DIRTY_FLAG:-}" ] && [ -f "$FAKE_GIT_DIRTY_FLAG" ]; then
      rm -f "$FAKE_GIT_DIRTY_FLAG"
      printf ' M package.json\n'
    fi
    exit 0 ;;
  "rev-parse HEAD") printf '%s\n' "$EXPECTED_REVISION" ;;
  "config core.autocrlf"|"config core.fileMode"|"clean -fd") exit 0 ;;
  "reset --hard") rm -f .fresh-clone-dirty; exit 0 ;;
  "-c http.version=HTTP/1.1")
    destination=''
    for argument in "$@"; do destination="$argument"; done
    mkdir -p "$destination/.git"
    touch "$destination/.fresh-clone-dirty"
    exit 0 ;;
  *) echo "Unexpected git invocation: $*" >&2; exit 2 ;;
esac
EOF
# 执行当前脚本步骤。
chmod +x "$test_root/bin/git"

PATH="$test_root/bin:$PATH" EXPECTED_REVISION="$expected_revision" \
  sh "$fixture/scripts/fetch-deepseek-harness.sh" > "$test_root/output.log"

# 执行当前脚本步骤。
grep -qx "$expected_revision" "$fixture/upstream/deepseek-harness.revision"
# 执行当前脚本步骤。
grep -q "DeepSeek Harness ready: $expected_revision" "$test_root/output.log"

# Re-running setup must not rewrite an unchanged Docker COPY input.
# 执行当前脚本步骤。
touch -t 200001010000 "$fixture/upstream/deepseek-harness.revision"
# 执行当前脚本步骤。
touch -t 200001010001 "$test_root/marker-cutoff"
PATH="$test_root/bin:$PATH" EXPECTED_REVISION="$expected_revision" \
  sh "$fixture/scripts/fetch-deepseek-harness.sh" > "$test_root/rerun-output.log"
# 判断条件后执行对应操作。
if [ "$fixture/upstream/deepseek-harness.revision" -nt "$test_root/marker-cutoff" ]; then
  # 执行当前脚本步骤。
  echo 'Unchanged Harness revision marker was rewritten' >&2
  # 返回结果或结束当前脚本。
  exit 1
# 结束当前控制块。
fi

# 执行当前脚本步骤。
touch "$test_root/dirty.flag"
PATH="$test_root/bin:$PATH" EXPECTED_REVISION="$expected_revision" FAKE_GIT_DIRTY_FLAG="$test_root/dirty.flag" \
  sh "$fixture/scripts/fetch-deepseek-harness.sh" > "$test_root/dirty-output.log"
# 执行当前脚本步骤。
backup_count="$(find "$fixture/upstream" -maxdepth 1 -type d -name 'deepseek-harness.backup-*' | wc -l | tr -d ' ')"
# 执行当前脚本步骤。
[ "$backup_count" = 1 ]
# 执行当前脚本步骤。
[ -d "$fixture/upstream/deepseek-harness/.git" ]
# 执行当前脚本步骤。
[ ! -e "$fixture/upstream/deepseek-harness/.fresh-clone-dirty" ]
# 执行当前脚本步骤。
grep -q '源码目录存在修改，已备份到：' "$test_root/dirty-output.log"

if grep -En 'git[[:space:]]+-C' \
  "$project_root/scripts/fetch-deepseek-harness.sh" \
  "$project_root/scripts/package-offline.sh"; then
  # 执行当前脚本步骤。
  echo "部署脚本仍使用 CentOS 7 Git 不支持的全局 -C 参数" >&2
  # 返回结果或结束当前脚本。
  exit 1
# 结束当前控制块。
fi

# 执行当前脚本步骤。
echo 'PASS git compatibility: deployment scripts work without global git -C'
