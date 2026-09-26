#!/bin/sh
# Runs inside the Alpine helper. Arguments: model archive, mounted Ollama home.
set -eu
archive=${1:?model archive required}
destination=${2:?Ollama data directory required}
[ -d "$destination" ] || { echo 'Ollama destination is not a mounted directory' >&2; exit 1; }
work=$(mktemp -d)
pending=''
cleanup() {
  [ -z "$pending" ] || rm -f -- "$pending"
  rm -rf -- "$work"
}
trap cleanup EXIT
tar -xzf "$archive" -C "$work"
[ -d "$work/models/manifests" ] && [ -d "$work/models/blobs" ] || {
  echo 'Model archive must contain models/manifests and models/blobs' >&2; exit 1;
}
find "$work/models" -type l > "$work/links"
[ ! -s "$work/links" ] || { echo 'Model archive must not contain symbolic links' >&2; exit 1; }
find "$work/models/manifests" -type f > "$work/manifests"
[ -s "$work/manifests" ] || { echo 'Model archive contains no manifests; blobs alone are not usable models' >&2; exit 1; }
# Publish manifests only after their blobs have been copied. BusyBox cp -n skips
# existing directories entirely, so never pass the whole tree to cp -an.
find "$work/models/blobs" -type f > "$work/files"
cat "$work/manifests" >> "$work/files"
copied=0
retained=0
while IFS= read -r file; do
  target="$destination/${file#"$work/"}"
  if [ -e "$target" ] || [ -L "$target" ]; then
    [ -f "$target" ] && [ ! -L "$target" ] || { echo "Invalid existing model file: $target" >&2; exit 1; }
    retained=$((retained + 1))
    continue
  fi
  mkdir -p "${target%/*}"
  # An interrupted copy must not leave a partial blob that a retry would skip.
  pending=$(mktemp "$target.restore.XXXXXX")
  cp -p "$file" "$pending"
  mv -n "$pending" "$target"
  rm -f -- "$pending"
  pending=''
  copied=$((copied + 1))
done < "$work/files"
printf 'Ollama files restored: %s copied, %s existing files retained\n' "$copied" "$retained"
