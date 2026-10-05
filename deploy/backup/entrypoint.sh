#!/bin/sh
# Hands the staging directory to the service user once (volumes created by
# earlier releases belong to root), then runs the service without root.
set -eu
dir="${IOT_BACKUP_DIR:-/app/data/backups}"
if [ "$(id -u)" = 0 ]; then
  mkdir -p "$dir"
  [ "$(stat -c %u "$dir")" = 65532 ] || chown -R 65532:65532 "$dir"
  exec su-exec 65532:65532 backup-service "$@"
fi
exec backup-service "$@"
