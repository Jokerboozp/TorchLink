#!/bin/sh
# Runs in the Alpine archive helper. Arguments: mounted HuggingFace cache,
# model id (Org/Name), archive path. Exports only that model's current
# snapshot as regular files under offline/<Name>, so the target loads it from
# a local directory without network access and the archive has no symlinks.
set -eu
cache=${1:?model cache directory required}
model=${2:?model id required}
archive=${3:?archive path required}
case "$model" in
  */*) ;;
  *) echo 'Model id must look like Org/Name' >&2; exit 1 ;;
esac
case "$model" in
  *..*|/*|*/*/*) echo 'Invalid model id' >&2; exit 1 ;;
esac
repo="$cache/models--$(printf '%s' "$model" | sed 's#/#--#')"
[ -f "$repo/refs/main" ] || { echo "Model $model is not cached (missing refs/main)" >&2; exit 1; }
revision=$(tr -d ' \r\n' < "$repo/refs/main")
printf '%s' "$revision" | grep -Eq '^[0-9a-f]{40}$' || { echo 'Invalid cached model revision' >&2; exit 1; }
snapshot="$repo/snapshots/$revision"
[ -d "$snapshot" ] || { echo "Snapshot $revision of $model is missing" >&2; exit 1; }
name=${model#*/}
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$work/offline"
# Snapshot entries are symlinks into blobs; -L copies the file contents.
cp -RL "$snapshot" "$work/offline/$name"
[ -n "$(find "$work/offline/$name" -type f -name '*.json' | head -n 1)" ] || { echo 'Model snapshot contains no configuration files' >&2; exit 1; }
find "$work/offline" -type l | grep -q . && { echo 'Exported model must not contain symbolic links' >&2; exit 1; }
printf '%s\n' "$model@$revision" > "$work/offline/$name/.iot-model-source"
tar -czf "$archive" -C "$work" offline
