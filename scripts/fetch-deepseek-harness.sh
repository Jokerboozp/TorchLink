#!/usr/bin/env sh
# Fetches the exact revision recorded by deploy/deepseek-harness/REVISION.
set -eu

script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
project_root="$(dirname -- "$script_dir")"
revision_file="$project_root/deploy/deepseek-harness/REVISION"
revision="$(tr -d '\r\n' < "$revision_file")"
repository="https://github.com/deepseek-ai/deepseek-harness.git"
target="$project_root/upstream/deepseek-harness"
revision_marker="$project_root/upstream/deepseek-harness.revision"

target_git() {
  (CDPATH= cd -- "$target" && git "$@")
}

clone_target() {
  mkdir -p "$project_root/upstream"
  git -c http.version=HTTP/1.1 -c core.autocrlf=false -c core.fileMode=false clone --depth 1 "$repository" "$target"
  target_git config core.autocrlf false
  target_git config core.fileMode false
  target_git reset --hard HEAD >/dev/null
  target_git clean -fd >/dev/null
}

case "$revision" in
  *[!0-9a-f]*|'') echo "无效的 DeepSeek Harness 提交：$revision_file" >&2; exit 1 ;;
esac
if [ "${#revision}" -ne 40 ]; then
  echo "无效的 DeepSeek Harness 提交：$revision_file" >&2
  exit 1
fi

if [ ! -d "$target/.git" ]; then
  clone_target
fi

if [ -n "$(target_git status --porcelain)" ]; then
  backup="${target}.backup-$(date +%Y%m%d-%H%M%S)"
  suffix=0
  while [ -e "$backup" ]; do
    suffix=$((suffix + 1))
    backup="${target}.backup-$(date +%Y%m%d-%H%M%S)-${suffix}"
  done
  mv -- "$target" "$backup"
  echo "DeepSeek Harness 源码目录存在修改，已备份到：$backup"
  clone_target
fi

current_revision="$(target_git rev-parse HEAD)"
if [ "$current_revision" != "$revision" ]; then
  target_git -c http.version=HTTP/1.1 fetch --depth 1 origin "$revision"
  target_git checkout --detach "$revision"
fi

actual_revision="$(target_git rev-parse HEAD)"
if [ "$actual_revision" != "$revision" ]; then
  echo "DeepSeek Harness 提交校验失败：期望 $revision，实际 $actual_revision" >&2
  exit 1
fi
# This file is a Docker COPY input; leave an unchanged marker untouched.
if [ ! -f "$revision_marker" ] || ! printf '%s\n' "$actual_revision" | cmp -s - "$revision_marker"; then
  printf '%s\n' "$actual_revision" > "$revision_marker"
fi

echo "DeepSeek Harness ready: $actual_revision"
