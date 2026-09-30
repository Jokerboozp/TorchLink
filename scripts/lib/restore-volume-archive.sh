#!/bin/sh
# Runs inside the Alpine helper. Arguments: archive, mounted volume directory.
# Restores regular files and directories only; existing files are retained so
# a rerun never replaces data already in the volume.
set -eu
archive=${1:?archive required}
destination=${2:?destination directory required}
[ -d "$destination" ] || { echo 'Destination is not a mounted directory' >&2; exit 1; }
work=$(mktemp -d)
pending=''
cleanup() {
  [ -z "$pending" ] || rm -f -- "$pending"
  rm -rf -- "$work"
}
trap cleanup EXIT
# Reject absolute and parent-directory member names before extracting.
tar -tzf "$archive" > "$work/members"
if grep -Eq '^/|(^|/)\.\.(/|$)' "$work/members"; then
  echo 'Archive contains unsafe paths' >&2; exit 1
fi
mkdir "$work/tree"
tar -xzf "$archive" -C "$work/tree"
find "$work/tree" -type l > "$work/links"
[ ! -s "$work/links" ] || { echo 'Archive must not contain symbolic links' >&2; exit 1; }
find "$work/tree" -type f > "$work/files"
[ -s "$work/files" ] || { echo 'Archive contains no files' >&2; exit 1; }
copied=0
retained=0
while IFS= read -r file; do
  target="$destination/${file#"$work/tree/"}"
  if [ -e "$target" ] || [ -L "$target" ]; then
    [ -f "$target" ] && [ ! -L "$target" ] || { echo "Invalid existing file: $target" >&2; exit 1; }
    retained=$((retained + 1))
    continue
  fi
  mkdir -p "${target%/*}"
  # An interrupted copy must not leave a partial file that a retry would skip.
  pending=$(mktemp "$target.restore.XXXXXX")
  cp -p "$file" "$pending"
  mv -n "$pending" "$target"
  rm -f -- "$pending"
  pending=''
  copied=$((copied + 1))
done < "$work/files"
printf 'Volume files restored: %s copied, %s existing files retained\n' "$copied" "$retained"
