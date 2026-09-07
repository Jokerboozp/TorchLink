#!/usr/bin/env bash
# Exercise real installer branching, archive checks and checksums. No host writes.
set -Eeuo pipefail
scripts="$(cd "$(dirname "$0")/.." && pwd)"
source "$scripts/lib/docker-bootstrap.sh"
test_root="$(mktemp -d)"
trap 'rm -rf -- "$test_root"' EXIT
mkdir -p "$test_root/source/docker" "$test_root/runtime"
for name in docker dockerd containerd containerd-shim-runc-v2 ctr runc docker-init docker-proxy; do
  printf 'test binary' > "$test_root/source/docker/$name"
done
tar -czf "$test_root/runtime/docker-24.0.9.tgz" -C "$test_root/source" docker
printf 'test compose' > "$test_root/runtime/docker-compose"
printf x86_64 > "$test_root/runtime/architecture"
for file in "$test_root/runtime/"docker-*; do docker_runtime_hash "$file" > "$file.sha256"; done
calls="$test_root/calls"
cli=0; daemon=0; compose_version=''; downloads=0
command() {
  if [ "${1:-}" = -v ] && [ "${2:-}" = docker ]; then [ "$cli" = 1 ]; return; fi
  if [ "${1:-}" = -v ] && [ "${2:-}" = systemctl ]; then return 0; fi
  if [ "${1:-}" = -v ] && [ "${2:-}" = dockerd ]; then return 1; fi
  builtin command "$@"
}
uname() {
  case "$1" in -s) echo Linux;; -m) echo x86_64;; -r) echo 3.10.0;; esac
}
docker() {
  case "$1" in
    info) [ "$cli" = 1 ] && [ "$daemon" = 1 ];;
    compose) [ -n "$compose_version" ] || return 1; echo "$compose_version";;
    buildx) return 0;;
  esac
}
docker_runtime_prerequisites() { printf 'prerequisites %s\n' "$1" >> "$calls"; }
docker_runtime_root() {
  printf '%s\n' "$*" >> "$calls"
  case "$*" in
    *'install -m 0755'*'/iot-docker/'*) cli=1;;
    *'install -m 0755'*'cli-plugins/docker-compose'*) compose_version=2.27.3;;
    'systemctl enable --now docker') daemon=1;;
  esac
}
docker_runtime_download() { downloads=$((downloads+1)); return 91; }
reset_case() { cli=0; daemon=0; compose_version=''; downloads=0; : > "$calls"; }

reset_case
ensure_deployment_docker offline "$test_root/runtime"
[ "$cli" = 1 ] && [ "$daemon" = 1 ] && [ "$compose_version" = 2.27.3 ]
[ "$downloads" = 0 ]
grep -q 'daemon-reload' "$calls"
echo 'PASS missing engine: offline install, systemd start, Compose, no download'

: > "$calls"
ensure_deployment_docker offline "$test_root/does-not-exist"
[ ! -s "$calls" ]
echo 'PASS existing runtime: no install, restart or archive requirement'

reset_case; cli=1; daemon=1; compose_version=2.10.0
ensure_deployment_docker offline "$test_root/runtime"
[ "$compose_version" = 2.27.3 ]
! grep -q 'iot-docker\|daemon-reload' "$calls"
echo 'PASS old Compose: update plugin without replacing engine'

reset_case; cli=1; compose_version=2.27.3
ensure_deployment_docker offline "$test_root/does-not-exist"
[ "$daemon" = 1 ]
grep -q 'enable --now docker' "$calls"
! grep -q 'install' "$calls"
echo 'PASS stopped daemon: start existing service'

reset_case
if ensure_deployment_docker offline "$test_root/does-not-exist"; then echo 'Missing runtime accepted'; exit 1; fi
[ ! -s "$calls" ] && [ "$downloads" = 0 ]

printf aarch64 > "$test_root/runtime/architecture"
if ensure_deployment_docker offline "$test_root/runtime"; then echo 'Wrong architecture accepted'; exit 1; fi
[ ! -s "$calls" ]
printf x86_64 > "$test_root/runtime/architecture"

printf corrupt >> "$test_root/runtime/docker-compose"
if ensure_deployment_docker offline "$test_root/runtime"; then echo 'Corrupt runtime accepted'; exit 1; fi
[ ! -s "$calls" ] && [ "$downloads" = 0 ]
echo 'PASS missing, mismatched and corrupt packages: stop before host mutations'

reset_case
if ensure_deployment_docker online; then echo 'Failed download accepted'; exit 1; fi
[ ! -s "$calls" ] && [ "$downloads" = 1 ]
echo 'PASS failed download: no partial host installation'
reset_case
docker_runtime_download() {
  downloads=$((downloads+1))
  case "$1" in
    *.tgz) cp "$test_root/runtime/docker-24.0.9.tgz" "$2";;
    *) printf 'mock plugin' > "$2";;
  esac
}
ensure_deployment_docker online
[ "$downloads" = 3 ] && [ "$cli" = 1 ] && [ "$daemon" = 1 ]
grep -q 'cli-plugins/docker-buildx' "$calls"
echo 'PASS online installation: downloads engine, Compose and Buildx then starts Docker'
echo 'Docker bootstrap smoke tests PASS (host writes mocked).'
