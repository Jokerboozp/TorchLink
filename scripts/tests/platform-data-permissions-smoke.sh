#!/usr/bin/env bash
# Requires real Docker and locally cached API/alpine images. No production mounts.
set -Eeuo pipefail
api_image="${1:-iot-platform-api:offline}"
helper_image="${2:-alpine:3.22}"
command -v docker >/dev/null 2>&1 || { echo '需要真实 Docker；未执行数据卷权限验证。' >&2; exit 2; }
docker image inspect "$api_image" "$helper_image" >/dev/null
fixture_container=''
cleanup() {
  # Only remove the container and anonymous volume created by this test.
  [ -z "$fixture_container" ] || docker rm -v "$fixture_container" >/dev/null
}
trap cleanup EXIT
fixture_container="$(docker create "$api_image")"
case "$(docker inspect --format '{{.Config.User}}' "$fixture_container")" in
  nonroot:nonroot|65532:65532|65532) ;;
  *) echo '镜像应以 nonroot（65532）运行，不能以 root 绕过目录权限检查。' >&2; exit 1;;
esac
docker inspect --format '{{range .Mounts}}{{if eq .Destination "/app/data"}}{{.Type}}{{end}}{{end}}' "$fixture_container" | grep -qx volume
# Reproduce the failing mkdir as the API user on the actual image-created volume.
docker run --rm --pull never --user 65532:65532 --volumes-from "$fixture_container" \
  "$helper_image" sh -ec 'mkdir -p /app/data/mqtt-inbox/0; printf persisted > /app/data/mqtt-inbox/0/probe'
docker run --rm --pull never --user 65532:65532 --volumes-from "$fixture_container" \
  "$helper_image" sh -ec 'test "$(cat /app/data/mqtt-inbox/0/probe)" = persisted'
echo 'PASS real image volume: nonroot MQTT directory creation and persisted write/read'
