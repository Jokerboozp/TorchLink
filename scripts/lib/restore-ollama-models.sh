#!/bin/sh
# Runs inside the Alpine helper. Arguments: model archive, mounted Ollama home.
# 执行当前脚本步骤。
set -eu
# 执行当前脚本步骤。
archive=${1:?model archive required}
# 执行当前脚本步骤。
destination=${2:?Ollama data directory required}
# 执行当前脚本步骤。
[ -d "$destination" ] || { echo 'Ollama destination is not a mounted directory' >&2; exit 1; }
# 执行当前脚本步骤。
work=$(mktemp -d)
# 执行当前脚本步骤。
pending=''
# 执行当前脚本步骤。
cleanup() {
  # 执行当前脚本步骤。
  [ -z "$pending" ] || rm -f -- "$pending"
  # 执行当前脚本步骤。
  rm -rf -- "$work"
# 结束当前控制块。
}
# 执行当前脚本步骤。
trap cleanup EXIT
# 执行当前脚本步骤。
tar -xzf "$archive" -C "$work"
# 执行当前脚本步骤。
[ -d "$work/models/manifests" ] && [ -d "$work/models/blobs" ] || {
  # 执行当前脚本步骤。
  echo 'Model archive must contain models/manifests and models/blobs' >&2; exit 1;
# 结束当前控制块。
}
# 结束当前控制块。
find "$work/models" -type l > "$work/links"
# 执行当前脚本步骤。
[ ! -s "$work/links" ] || { echo 'Model archive must not contain symbolic links' >&2; exit 1; }
# 结束当前控制块。
find "$work/models/manifests" -type f > "$work/manifests"
# 执行当前脚本步骤。
[ -s "$work/manifests" ] || { echo 'Model archive contains no manifests; blobs alone are not usable models' >&2; exit 1; }
# Publish manifests only after their blobs have been copied. BusyBox cp -n skips
# existing directories entirely, so never pass the whole tree to cp -an.
# 结束当前控制块。
find "$work/models/blobs" -type f > "$work/files"
# 执行当前脚本步骤。
cat "$work/manifests" >> "$work/files"
# 执行当前脚本步骤。
copied=0
# 执行当前脚本步骤。
retained=0
# 遍历数据并执行循环体。
while IFS= read -r file; do
  # 执行当前脚本步骤。
  target="$destination/${file#"$work/"}"
  # 判断条件后执行对应操作。
  if [ -e "$target" ] || [ -L "$target" ]; then
    # 执行当前脚本步骤。
    [ -f "$target" ] && [ ! -L "$target" ] || { echo "Invalid existing model file: $target" >&2; exit 1; }
    # 执行当前脚本步骤。
    retained=$((retained + 1))
    # 执行当前脚本步骤。
    continue
  # 结束当前控制块。
  fi
  # 执行当前脚本步骤。
  mkdir -p "${target%/*}"
  # An interrupted copy must not leave a partial blob that a retry would skip.
  # 执行当前脚本步骤。
  pending=$(mktemp "$target.restore.XXXXXX")
  # 执行当前脚本步骤。
  cp -p "$file" "$pending"
  # 执行当前脚本步骤。
  mv -n "$pending" "$target"
  # 执行当前脚本步骤。
  rm -f -- "$pending"
  # 执行当前脚本步骤。
  pending=''
  # 执行当前脚本步骤。
  copied=$((copied + 1))
# 结束当前控制块。
done < "$work/files"
# 执行当前脚本步骤。
printf 'Ollama files restored: %s copied, %s existing files retained\n' "$copied" "$retained"
