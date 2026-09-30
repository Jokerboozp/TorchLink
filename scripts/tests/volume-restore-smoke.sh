#!/bin/sh
# Run with BusyBox sh to exercise the same utilities as the Alpine helper.
set -eu
scripts=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
fixture=$(mktemp -d)
trap 'rm -rf -- "$fixture"' EXIT

# A HuggingFace cache: snapshot files are symlinks into content-addressed blobs.
revision=0123456789abcdef0123456789abcdef01234567
repo="$fixture/cache/models--Org--Demo"
mkdir -p "$repo/blobs" "$repo/refs" "$repo/snapshots/$revision/1_Pooling"
printf '{"model_type":"demo"}' > "$repo/blobs/aaa"
printf weights > "$repo/blobs/bbb"
printf '{"pooling":"last"}' > "$repo/blobs/ccc"
ln -s ../../blobs/aaa "$repo/snapshots/$revision/config.json"
ln -s ../../blobs/bbb "$repo/snapshots/$revision/model.safetensors"
ln -s ../../../blobs/ccc "$repo/snapshots/$revision/1_Pooling/config.json"
printf '%s' "$revision" > "$repo/refs/main"
sh "$scripts/lib/export-model-cache.sh" "$fixture/cache" Org/Demo "$fixture/model.tgz"
if tar -tvzf "$fixture/model.tgz" | grep -q '^l'; then echo 'FAIL export kept symbolic links'; exit 1; fi
if tar -tzf "$fixture/model.tgz" | grep -q blobs; then echo 'FAIL export copied the blob store'; exit 1; fi
echo 'PASS model cache exported as regular files without the blob store'

mkdir -p "$fixture/dst/offline/Demo"
printf preserve > "$fixture/dst/offline/Demo/model.safetensors"
sh "$scripts/lib/restore-volume-archive.sh" "$fixture/model.tgz" "$fixture/dst"
[ "$(cat "$fixture/dst/offline/Demo/config.json")" = '{"model_type":"demo"}' ]
[ "$(cat "$fixture/dst/offline/Demo/1_Pooling/config.json")" = '{"pooling":"last"}' ]
[ "$(cat "$fixture/dst/offline/Demo/model.safetensors")" = preserve ]
sh "$scripts/lib/restore-volume-archive.sh" "$fixture/model.tgz" "$fixture/dst"
echo 'PASS restore adds missing files, retains existing files and reruns'

mkdir -p "$fixture/evil/offline"
ln -s /etc/passwd "$fixture/evil/offline/link"
tar -czf "$fixture/evil.tgz" -C "$fixture/evil" offline
if sh "$scripts/lib/restore-volume-archive.sh" "$fixture/evil.tgz" "$fixture/dst" 2>/dev/null; then
  echo 'FAIL accepted archive with a symbolic link'; exit 1
fi
[ ! -e "$fixture/dst/offline/link" ]
if sh "$scripts/lib/export-model-cache.sh" "$fixture/cache" Org/Missing "$fixture/missing.tgz" 2>/dev/null; then
  echo 'FAIL exported a model that is not cached'; exit 1
fi
echo 'PASS symbolic links and missing models rejected before writing'
