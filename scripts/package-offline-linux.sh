#!/usr/bin/env bash
# 执行当前脚本步骤。
set -Eeuo pipefail

# 执行当前脚本步骤。
[[ "$(uname -s)" == "Linux" ]] || { echo "此脚本只能在 Linux 上运行" >&2; exit 1; }
# 执行当前脚本步骤。
script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
# 执行当前脚本步骤。
exec bash "$script_dir/package-offline.sh" "$@"
