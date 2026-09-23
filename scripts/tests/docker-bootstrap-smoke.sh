#!/usr/bin/env bash
# Exercise real installer branching, archive checks and checksums. No host writes.
# 执行当前脚本步骤。
set -Eeuo pipefail
# 执行当前脚本步骤。
scripts="$(cd "$(dirname "$0")/.." && pwd)"
# 执行当前脚本步骤。
source "$scripts/lib/docker-bootstrap.sh"
# 执行当前脚本步骤。
test_root="$(mktemp -d)"
# 执行当前脚本步骤。
trap 'rm -rf -- "$test_root"' EXIT
# 执行当前脚本步骤。
mkdir -p "$test_root/source/docker" "$test_root/runtime"
# 遍历数据并执行循环体。
for name in docker dockerd containerd containerd-shim-runc-v2 ctr runc docker-init docker-proxy; do
  # 执行当前脚本步骤。
  printf 'test binary' > "$test_root/source/docker/$name"
# 结束当前控制块。
done
# 执行当前脚本步骤。
tar -czf "$test_root/runtime/docker-24.0.9.tgz" -C "$test_root/source" docker
# 执行当前脚本步骤。
cp "$test_root/runtime/docker-24.0.9.tgz" "$test_root/runtime/docker-28.5.2.tgz"
# 执行当前脚本步骤。
printf 'test compose' > "$test_root/runtime/docker-compose"
# 执行当前脚本步骤。
printf x86_64 > "$test_root/runtime/architecture"
# 遍历数据并执行循环体。
for file in "$test_root/runtime/"docker-*; do docker_runtime_hash "$file" > "$file.sha256"; done
# 执行当前脚本步骤。
calls="$test_root/calls"
# 执行当前脚本步骤。
cli=0; daemon=0; compose_version=''; downloads=0
# 执行当前脚本步骤。
kernel=3.10.0
# 执行当前脚本步骤。
machine_arch=x86_64
# 执行当前脚本步骤。
command() {
  # 判断条件后执行对应操作。
  if [ "${1:-}" = -v ] && [ "${2:-}" = docker ]; then [ "$cli" = 1 ]; return; fi
  # 判断条件后执行对应操作。
  if [ "${1:-}" = -v ] && [ "${2:-}" = systemctl ]; then return 0; fi
  # 判断条件后执行对应操作。
  if [ "${1:-}" = -v ] && [ "${2:-}" = dockerd ]; then return 1; fi
  # 执行当前脚本步骤。
  builtin command "$@"
# 结束当前控制块。
}
# 执行当前脚本步骤。
uname() {
  # 执行当前脚本步骤。
  case "$1" in -s) echo Linux;; -m) echo "$machine_arch";; -r) echo "$kernel";; esac
# 结束当前控制块。
}
# 执行当前脚本步骤。
docker() {
  # 执行当前脚本步骤。
  case "$1" in
    # 执行当前脚本步骤。
    info) [ "$cli" = 1 ] && [ "$daemon" = 1 ];;
    # 执行当前脚本步骤。
    compose) [ -n "$compose_version" ] || return 1; echo "$compose_version";;
    # 执行当前脚本步骤。
    buildx) return 0;;
  # 执行当前脚本步骤。
  esac
# 结束当前控制块。
}
# 执行当前脚本步骤。
docker_runtime_prerequisites() { printf 'prerequisites %s\n' "$1" >> "$calls"; }
# 执行当前脚本步骤。
docker_runtime_root() {
  # 执行当前脚本步骤。
  printf '%s\n' "$*" >> "$calls"
  # 执行当前脚本步骤。
  case "$*" in
    # 执行当前脚本步骤。
    *'install -m 0755'*'/iot-docker/'*) cli=1;;
    # 执行当前脚本步骤。
    *'install -m 0755'*'cli-plugins/docker-compose'*) compose_version=2.27.3;;
    # 执行当前脚本步骤。
    'systemctl enable --now docker') daemon=1;;
  # 执行当前脚本步骤。
  esac
# 结束当前控制块。
}
# 执行当前脚本步骤。
docker_runtime_download() { downloads=$((downloads+1)); return 91; }
# 执行当前脚本步骤。
reset_case() { cli=0; daemon=0; compose_version=''; downloads=0; : > "$calls"; }

# 执行当前脚本步骤。
reset_case
# 执行当前脚本步骤。
ensure_deployment_docker offline "$test_root/runtime"
# 执行当前脚本步骤。
[ "$cli" = 1 ] && [ "$daemon" = 1 ] && [ "$compose_version" = 2.27.3 ]
# 执行当前脚本步骤。
[ "$downloads" = 0 ]
# 执行当前脚本步骤。
grep -q 'daemon-reload' "$calls"
# 执行当前脚本步骤。
echo 'PASS missing engine: offline install, systemd start, Compose, no download'

# 执行当前脚本步骤。
: > "$calls"
# 执行当前脚本步骤。
ensure_deployment_docker offline "$test_root/does-not-exist"
# 执行当前脚本步骤。
[ ! -s "$calls" ]
# 执行当前脚本步骤。
echo 'PASS existing runtime: no install, restart or archive requirement'

# 执行当前脚本步骤。
reset_case; cli=1; daemon=1; compose_version=2.10.0
# 执行当前脚本步骤。
ensure_deployment_docker offline "$test_root/runtime"
# 执行当前脚本步骤。
[ "$compose_version" = 2.27.3 ]
# 执行当前脚本步骤。
! grep -q 'iot-docker\|daemon-reload' "$calls"
# 执行当前脚本步骤。
echo 'PASS old Compose: update plugin without replacing engine'

# 执行当前脚本步骤。
reset_case; cli=1; compose_version=2.27.3
# 执行当前脚本步骤。
ensure_deployment_docker offline "$test_root/does-not-exist"
# 执行当前脚本步骤。
[ "$daemon" = 1 ]
# 执行当前脚本步骤。
grep -q 'enable --now docker' "$calls"
# 执行当前脚本步骤。
! grep -q 'install' "$calls"
# 执行当前脚本步骤。
echo 'PASS stopped daemon: start existing service'

# 执行当前脚本步骤。
reset_case
# 判断条件后执行对应操作。
if ensure_deployment_docker offline "$test_root/does-not-exist"; then echo 'Missing runtime accepted'; exit 1; fi
# 执行当前脚本步骤。
[ ! -s "$calls" ] && [ "$downloads" = 0 ]

# 执行当前脚本步骤。
printf aarch64 > "$test_root/runtime/architecture"
# 判断条件后执行对应操作。
if ensure_deployment_docker offline "$test_root/runtime"; then echo 'Wrong architecture accepted'; exit 1; fi
# 执行当前脚本步骤。
[ ! -s "$calls" ]
# 执行当前脚本步骤。
printf x86_64 > "$test_root/runtime/architecture"

# 执行当前脚本步骤。
printf corrupt >> "$test_root/runtime/docker-compose"
# 判断条件后执行对应操作。
if ensure_deployment_docker offline "$test_root/runtime"; then echo 'Corrupt runtime accepted'; exit 1; fi
# 执行当前脚本步骤。
[ ! -s "$calls" ] && [ "$downloads" = 0 ]
# 执行当前脚本步骤。
echo 'PASS missing, mismatched and corrupt packages: stop before host mutations'

# 执行当前脚本步骤。
reset_case
# 判断条件后执行对应操作。
if ensure_deployment_docker online; then echo 'Failed download accepted'; exit 1; fi
# 执行当前脚本步骤。
[ ! -s "$calls" ] && [ "$downloads" = 1 ]
# 执行当前脚本步骤。
echo 'PASS failed download: no partial host installation'
# 执行当前脚本步骤。
reset_case
# 执行当前脚本步骤。
docker_runtime_download() {
  # 执行当前脚本步骤。
  downloads=$((downloads+1))
  # 执行当前脚本步骤。
  printf 'download %s\n' "$1" >> "$calls"
  # 执行当前脚本步骤。
  case "$1" in
    # 执行当前脚本步骤。
    *.tgz) cp "$test_root/runtime/docker-24.0.9.tgz" "$2";;
    # 执行当前脚本步骤。
    *) printf 'mock plugin' > "$2";;
  # 执行当前脚本步骤。
  esac
# 结束当前控制块。
}
# 执行当前脚本步骤。
ensure_deployment_docker online
# 执行当前脚本步骤。
[ "$downloads" = 3 ] && [ "$cli" = 1 ] && [ "$daemon" = 1 ]
# 执行当前脚本步骤。
grep -q 'cli-plugins/docker-buildx' "$calls"
# 执行当前脚本步骤。
echo 'PASS online installation: downloads engine, Compose and Buildx then starts Docker'
# 执行当前脚本步骤。
printf 'test compose' > "$test_root/runtime/docker-compose"
# 执行当前脚本步骤。
docker_runtime_hash "$test_root/runtime/docker-compose" > "$test_root/runtime/docker-compose.sha256"
# 遍历数据并执行循环体。
for kernel in 5.15.0-generic 6.8.0-generic; do
  # 执行当前脚本步骤。
  reset_case
  # 执行当前脚本步骤。
  ensure_deployment_docker offline "$test_root/runtime"
  # 执行当前脚本步骤。
  [ "$cli" = 1 ] && [ "$daemon" = 1 ] && [ "$downloads" = 0 ]
# 结束当前控制块。
done
# 执行当前脚本步骤。
echo 'PASS Ubuntu kernels: offline engine and Compose installation without downloads'
# 遍历数据并执行循环体。
for machine_arch in x86_64 aarch64; do
  # 执行当前脚本步骤。
  reset_case
  # 执行当前脚本步骤。
  ensure_deployment_docker online
  # 执行当前脚本步骤。
  [ "$downloads" = 3 ] && [ "$cli" = 1 ] && [ "$daemon" = 1 ]
  # 执行当前脚本步骤。
  grep -q "static/stable/$machine_arch/docker-28.5.2.tgz" "$calls"
  # 执行当前脚本步骤。
  grep -q "docker-compose-linux-$machine_arch" "$calls"
  # 执行当前脚本步骤。
  build_arch=amd64; [ "$machine_arch" != aarch64 ] || build_arch=arm64
  # 执行当前脚本步骤。
  grep -q "buildx-v0.14.1.linux-$build_arch" "$calls"
# 结束当前控制块。
done
# 执行当前脚本步骤。
echo 'PASS amd64 and arm64: native engine, Compose and Buildx download selection'
# 执行当前脚本步骤。
echo 'Docker bootstrap smoke tests PASS (host writes mocked).'
