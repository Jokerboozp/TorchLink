#!/bin/sh
# Keep this script LF-only so POSIX sh can parse it inside Linux containers.
# 执行当前脚本步骤。
set -eu

# 执行当前脚本步骤。
plugin_dir="${IOT_HARNESS_PLUGIN_DIR:-/data/plugins}"
# 执行当前脚本步骤。
seed_dir="${IOT_HARNESS_PLUGIN_SEED_DIR:-/harness/examples/iot-ops-agent/plugins}"
# 执行当前脚本步骤。
mkdir -p "$plugin_dir"
# 遍历数据并执行循环体。
for source in "$seed_dir"/*.json; do
  # 执行当前脚本步骤。
  target="$plugin_dir/$(basename "$source")"
  # 执行当前脚本步骤。
  cp "$source" "$target"
# 结束当前控制块。
done
# 执行当前脚本步骤。
exec node /harness/examples/iot-ops-agent/gateway.mjs
