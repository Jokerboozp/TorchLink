#!/usr/bin/env sh
# Fetches the exact revision recorded by deploy/deepseek-harness/REVISION.
# 执行当前脚本步骤。
set -eu

# 执行当前脚本步骤。
script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
# 执行当前脚本步骤。
project_root="$(dirname -- "$script_dir")"
# 执行当前脚本步骤。
revision_file="$project_root/deploy/deepseek-harness/REVISION"
# 执行当前脚本步骤。
revision="$(tr -d '\r\n' < "$revision_file")"
# 执行当前脚本步骤。
repository="https://github.com/deepseek-ai/deepseek-harness.git"
# 执行当前脚本步骤。
target="$project_root/upstream/deepseek-harness"
# 执行当前脚本步骤。
revision_marker="$project_root/upstream/deepseek-harness.revision"

# 执行当前脚本步骤。
target_git() {
  # 执行当前脚本步骤。
  (CDPATH= cd -- "$target" && git "$@")
# 结束当前控制块。
}

# 执行当前脚本步骤。
clone_target() {
  # 执行当前脚本步骤。
  mkdir -p "$project_root/upstream"
  # 执行当前脚本步骤。
  git -c http.version=HTTP/1.1 -c core.autocrlf=false -c core.fileMode=false clone --depth 1 "$repository" "$target"
  # 执行当前脚本步骤。
  target_git config core.autocrlf false
  # 执行当前脚本步骤。
  target_git config core.fileMode false
  # 执行当前脚本步骤。
  target_git reset --hard HEAD >/dev/null
  # 执行当前脚本步骤。
  target_git clean -fd >/dev/null
# 结束当前控制块。
}

# 执行当前脚本步骤。
case "$revision" in
  # 执行当前脚本步骤。
  *[!0-9a-f]*|'') echo "无效的 DeepSeek Harness 提交：$revision_file" >&2; exit 1 ;;
# 执行当前脚本步骤。
esac
# 判断条件后执行对应操作。
if [ "${#revision}" -ne 40 ]; then
  # 执行当前脚本步骤。
  echo "无效的 DeepSeek Harness 提交：$revision_file" >&2
  # 返回结果或结束当前脚本。
  exit 1
# 结束当前控制块。
fi

# 判断条件后执行对应操作。
if [ ! -d "$target/.git" ]; then
  # 执行当前脚本步骤。
  clone_target
# 结束当前控制块。
fi

# 判断条件后执行对应操作。
if [ -n "$(target_git status --porcelain)" ]; then
  # 执行当前脚本步骤。
  backup="${target}.backup-$(date +%Y%m%d-%H%M%S)"
  # 执行当前脚本步骤。
  suffix=0
  # 遍历数据并执行循环体。
  while [ -e "$backup" ]; do
    # 执行当前脚本步骤。
    suffix=$((suffix + 1))
    # 执行当前脚本步骤。
    backup="${target}.backup-$(date +%Y%m%d-%H%M%S)-${suffix}"
  # 结束当前控制块。
  done
  # 执行当前脚本步骤。
  mv -- "$target" "$backup"
  # 执行当前脚本步骤。
  echo "DeepSeek Harness 源码目录存在修改，已备份到：$backup"
  # 执行当前脚本步骤。
  clone_target
# 结束当前控制块。
fi

# 执行当前脚本步骤。
current_revision="$(target_git rev-parse HEAD)"
# 判断条件后执行对应操作。
if [ "$current_revision" != "$revision" ]; then
  # 执行当前脚本步骤。
  target_git -c http.version=HTTP/1.1 fetch --depth 1 origin "$revision"
  # 执行当前脚本步骤。
  target_git checkout --detach "$revision"
# 结束当前控制块。
fi

# 执行当前脚本步骤。
actual_revision="$(target_git rev-parse HEAD)"
# 判断条件后执行对应操作。
if [ "$actual_revision" != "$revision" ]; then
  # 执行当前脚本步骤。
  echo "DeepSeek Harness 提交校验失败：期望 $revision，实际 $actual_revision" >&2
  # 返回结果或结束当前脚本。
  exit 1
# 结束当前控制块。
fi
# This file is a Docker COPY input; leave an unchanged marker untouched.
# 判断条件后执行对应操作。
if [ ! -f "$revision_marker" ] || ! printf '%s\n' "$actual_revision" | cmp -s - "$revision_marker"; then
  # 执行当前脚本步骤。
  printf '%s\n' "$actual_revision" > "$revision_marker"
# 结束当前控制块。
fi

# 执行当前脚本步骤。
echo "DeepSeek Harness ready: $actual_revision"
