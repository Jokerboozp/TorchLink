#!/bin/sh
# Run with BusyBox sh to exercise the same utilities as the Alpine helper.
# 执行当前脚本步骤。
set -eu
# 执行当前脚本步骤。
scripts=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
# 结束当前控制块。
fixture=$(mktemp -d)
# 执行当前脚本步骤。
trap 'rm -rf -- "$fixture"' EXIT
# 执行当前脚本步骤。
mkdir -p "$fixture/source/models/blobs" "$fixture/source/models/manifests/registry/library/demo" "$fixture/dst/models/blobs"
# 执行当前脚本步骤。
printf new-blob > "$fixture/source/models/blobs/new"
# 执行当前脚本步骤。
printf archive-version > "$fixture/source/models/blobs/existing"
# 执行当前脚本步骤。
printf preserve > "$fixture/dst/models/blobs/existing"
# 执行当前脚本步骤。
printf manifest > "$fixture/source/models/manifests/registry/library/demo/latest"
# 执行当前脚本步骤。
tar -czf "$fixture/models.tgz" -C "$fixture/source" models
# 判断条件后执行对应操作。
if [ "${1:-}" = --legacy ]; then
  # 执行当前脚本步骤。
  cp -an "$fixture/source/." "$fixture/dst/"
# 执行当前脚本步骤。
else
  # 执行当前脚本步骤。
  sh "$scripts/lib/restore-ollama-models.sh" "$fixture/models.tgz" "$fixture/dst"
# 结束当前控制块。
fi
# 执行当前脚本步骤。
[ -f "$fixture/dst/models/manifests/registry/library/demo/latest" ] || {
  # 执行当前脚本步骤。
  echo 'FAIL restore returned success but Ollama manifests are absent'; exit 1;
# 结束当前控制块。
}
# 执行当前脚本步骤。
[ "$(cat "$fixture/dst/models/blobs/new")" = new-blob ]
# 执行当前脚本步骤。
[ "$(cat "$fixture/dst/models/blobs/existing")" = preserve ]
# 执行当前脚本步骤。
sh "$scripts/lib/restore-ollama-models.sh" "$fixture/models.tgz" "$fixture/dst"
# 执行当前脚本步骤。
[ "$(cat "$fixture/dst/models/blobs/existing")" = preserve ]
# 执行当前脚本步骤。
echo 'PASS existing destination: new files restored, old files retained, rerun succeeds'
# 执行当前脚本步骤。
mkdir -p "$fixture/empty/models/blobs"
# 执行当前脚本步骤。
printf orphan > "$fixture/empty/models/blobs/orphan"
# 执行当前脚本步骤。
tar -czf "$fixture/empty.tgz" -C "$fixture/empty" models
# 判断条件后执行对应操作。
if sh "$scripts/lib/restore-ollama-models.sh" "$fixture/empty.tgz" "$fixture/dst"; then
  # 执行当前脚本步骤。
  echo 'FAIL accepted archive without model manifests'; exit 1
# 结束当前控制块。
fi
# 执行当前脚本步骤。
[ ! -e "$fixture/dst/models/blobs/orphan" ]
# 执行当前脚本步骤。
echo 'PASS orphan blob archive rejected before writing destination'
