#!/bin/sh
# Runs in the Alpine archive helper. Never export unrelated cached chat weights.
set -eu
source_dir=${1:?Ollama home required}
archive=${2:?archive path required}
manifest=models/manifests/registry.ollama.ai/library/nomic-embed-text/latest
[ -f "$source_dir/$manifest" ] && [ ! -L "$source_dir/$manifest" ] || {
  echo 'Missing nomic-embed-text manifest' >&2; exit 1;
}
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$work/$(dirname "$manifest")" "$work/models/blobs"
cp "$source_dir/$manifest" "$work/$manifest"
# Ollama manifests reference content-addressed config and layer blobs.
grep -oE 'sha256:[a-f0-9]{64}' "$source_dir/$manifest" | sort -u > "$work/digests"
[ -s "$work/digests" ] || { echo 'Embedding manifest contains no blob digests' >&2; exit 1; }
while IFS= read -r digest; do
  blob="models/blobs/$(printf '%s' "$digest" | tr ':' '-')"
  [ -f "$source_dir/$blob" ] && [ ! -L "$source_dir/$blob" ] || {
    echo 'Missing or invalid embedding blob' >&2; exit 1;
  }
  actual=$(sha256sum "$source_dir/$blob" | awk '{print $1}')
  [ "$actual" = "${digest#sha256:}" ] || { echo 'Embedding blob checksum mismatch' >&2; exit 1; }
  cp "$source_dir/$blob" "$work/$blob"
done < "$work/digests"
tar -czf "$archive" -C "$work" models
