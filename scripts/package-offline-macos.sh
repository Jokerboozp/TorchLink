#!/usr/bin/env bash
set -Eeuo pipefail

[[ "$(uname -s)" == "Darwin" ]] || { echo "此脚本只能在 macOS 上运行" >&2; exit 1; }
script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
exec bash "$script_dir/package-offline.sh" "$@"
