#!/usr/bin/env bash
# Requires real Docker and locally cached API/alpine images. No production mounts.
# 执行当前脚本步骤。
set -Eeuo pipefail
# 执行当前脚本步骤。
api_image="${1:-iot-platform-api:offline}"
# 执行当前脚本步骤。
helper_image="${2:-alpine:3.22}"
# 执行当前脚本步骤。
command -v docker >/dev/null 2>&1 || { echo '需要真实 Docker；未执行数据卷权限验证。' >&2; exit 2; }
# 执行当前脚本步骤。
docker image inspect "$api_image" "$helper_image" >/dev/null
# 结束当前控制块。
fixture_container=''
# 执行当前脚本步骤。
cleanup() {
  # Only remove the container and anonymous volume created by this test.
  # 执行当前脚本步骤。
  [ -z "$fixture_container" ] || docker rm -v "$fixture_container" >/dev/null
# 结束当前控制块。
}
# 执行当前脚本步骤。
trap cleanup EXIT
# 结束当前控制块。
fixture_container="$(docker create "$api_image")"
# 执行当前脚本步骤。
case "$(docker inspect --format '{{.Config.User}}' "$fixture_container")" in
  # 执行当前脚本步骤。
  nonroot:nonroot|65532:65532|65532) ;;
  # 执行当前脚本步骤。
  *) echo '镜像应以 nonroot（65532）运行，不能以 root 绕过目录权限检查。' >&2; exit 1;;
# 执行当前脚本步骤。
esac
# 执行当前脚本步骤。
docker inspect --format '{{range .Mounts}}{{if eq .Destination "/app/data"}}{{.Type}}{{end}}{{end}}' "$fixture_container" | grep -qx volume
# Reproduce the failing mkdir as the API user on the actual image-created volume.
docker run --rm --pull never --user 65532:65532 --volumes-from "$fixture_container" \
  "$helper_image" sh -ec 'mkdir -p /app/data/mqtt-inbox/0; printf persisted > /app/data/mqtt-inbox/0/probe'
docker run --rm --pull never --user 65532:65532 --volumes-from "$fixture_container" \
  "$helper_image" sh -ec 'test "$(cat /app/data/mqtt-inbox/0/probe)" = persisted'
# 执行当前脚本步骤。
echo 'PASS real image volume: nonroot MQTT directory creation and persisted write/read'
