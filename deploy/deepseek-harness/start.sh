#!/bin/sh
# Keep this script LF-only so POSIX sh can parse it inside Linux containers.
set -eu

plugin_dir="${IOT_HARNESS_PLUGIN_DIR:-/data/plugins}"
seed_dir="${IOT_HARNESS_PLUGIN_SEED_DIR:-/harness/examples/iot-ops-agent/plugins}"
mkdir -p "$plugin_dir"
for source in "$seed_dir"/*.json; do
  target="$plugin_dir/$(basename "$source")"
  cp "$source" "$target"
done
exec node /harness/examples/iot-ops-agent/gateway.mjs
