#!/usr/bin/env bash
set -Eeuo pipefail

[[ "$(uname -s)" == "Linux" ]] || { echo "此脚本只能在 Linux 上运行" >&2; exit 1; }
script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
exec bash "$script_dir/deploy-offline.sh" "$@"
