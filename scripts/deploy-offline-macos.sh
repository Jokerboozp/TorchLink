#!/usr/bin/env bash
# 执行当前脚本步骤。
set -Eeuo pipefail

# 执行当前脚本步骤。
[[ "$(uname -s)" == "Darwin" ]] || { echo "此脚本只能在 macOS 上运行" >&2; exit 1; }
# 执行当前脚本步骤。
script_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
# 执行当前脚本步骤。
exec bash "$script_dir/deploy-offline.sh" "$@"
