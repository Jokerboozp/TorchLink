#!/bin/sh
# Run with BusyBox sh to exercise the same utilities as the Alpine helper.
set -eu
scripts=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
fixture=$(mktemp -d)
trap 'rm -rf -- "$fixture"' EXIT
mkdir -p "$fixture/source/models/blobs" "$fixture/source/models/manifests/registry/library/demo" "$fixture/dst/models/blobs"
printf new-blob > "$fixture/source/models/blobs/new"
printf archive-version > "$fixture/source/models/blobs/existing"
printf preserve > "$fixture/dst/models/blobs/existing"
printf manifest > "$fixture/source/models/manifests/registry/library/demo/latest"
tar -czf "$fixture/models.tgz" -C "$fixture/source" models
if [ "${1:-}" = --legacy ]; then
  cp -an "$fixture/source/." "$fixture/dst/"
else
  sh "$scripts/lib/restore-ollama-models.sh" "$fixture/models.tgz" "$fixture/dst"
fi
[ -f "$fixture/dst/models/manifests/registry/library/demo/latest" ] || {
  echo 'FAIL restore returned success but Ollama manifests are absent'; exit 1;
}
[ "$(cat "$fixture/dst/models/blobs/new")" = new-blob ]
[ "$(cat "$fixture/dst/models/blobs/existing")" = preserve ]
sh "$scripts/lib/restore-ollama-models.sh" "$fixture/models.tgz" "$fixture/dst"
[ "$(cat "$fixture/dst/models/blobs/existing")" = preserve ]
echo 'PASS existing destination: new files restored, old files retained, rerun succeeds'
mkdir -p "$fixture/empty/models/blobs"
printf orphan > "$fixture/empty/models/blobs/orphan"
tar -czf "$fixture/empty.tgz" -C "$fixture/empty" models
if sh "$scripts/lib/restore-ollama-models.sh" "$fixture/empty.tgz" "$fixture/dst"; then
  echo 'FAIL accepted archive without model manifests'; exit 1
fi
[ ! -e "$fixture/dst/models/blobs/orphan" ]
echo 'PASS orphan blob archive rejected before writing destination'
