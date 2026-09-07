#!/usr/bin/env bash
# Test real Ubuntu prerequisite selection; never run apt/dpkg against the host.
set -Eeuo pipefail
scripts="$(cd "$(dirname "$0")/.." && pwd)"
source "$scripts/lib/docker-bootstrap.sh"
test_root="$(mktemp -d)"
trap 'rm -rf -- "$test_root"' EXIT
mkdir -p "$test_root/packages"
calls="$test_root/calls"
ready=0; fail_install=0; downloader_ready=1
command() {
  if [ "${1:-}" = -v ]; then
    case "${2:-}" in
      iptables|xz|ps) [ "$ready" = 1 ]; return;;
      curl|wget) [ "$downloader_ready" = 1 ]; return;;
      apt-get|dpkg|rpm) return 0;;
    esac
  fi
  builtin command "$@"
}
docker_runtime_root() {
  printf '%s\n' "$*" >> "$calls"
  case "$*" in
    *'apt-get install'*|'dpkg -i '*)
      [ "$fail_install" = 0 ] || return 42
      ready=1; downloader_ready=1;;
  esac
}
: > "$calls"
docker_runtime_prerequisites online "$test_root"
grep -q '^apt-get update$' "$calls"
grep -q '^env DEBIAN_FRONTEND=noninteractive apt-get install -y iptables xz-utils procps curl ca-certificates$' "$calls"
echo 'PASS Ubuntu online: noninteractive apt prerequisite installation'

# Mixed-format package directory must select DEB on an Ubuntu host.
printf 'test deb' > "$test_root/packages/dependency.deb"
printf 'test rpm' > "$test_root/packages/dependency.rpm"
docker_runtime_hash "$test_root/packages/dependency.deb" > "$test_root/packages/dependency.deb.sha256"
ready=0; : > "$calls"
docker_runtime_prerequisites offline "$test_root"
grep -q '^dpkg -i ' "$calls"
! grep -q 'apt-get\|rpm' "$calls"
echo 'PASS Ubuntu offline: verified DEB packages only, no software repository access'

ready=0; : > "$calls"
printf corrupt >> "$test_root/packages/dependency.deb"
if docker_runtime_prerequisites offline "$test_root"; then echo 'Corrupt DEB accepted'; exit 1; fi
[ ! -s "$calls" ]
echo 'PASS corrupt DEB: no package installation'

ready=0; fail_install=1; : > "$calls"
if docker_runtime_prerequisites online "$test_root"; then echo 'apt failure ignored'; exit 1; fi
[ "$ready" = 0 ]
echo 'PASS apt failure: bootstrap stops'

ready=1; : > "$calls"
docker_runtime_prerequisites offline "$test_root/absent"
[ ! -s "$calls" ]
echo 'PASS existing Ubuntu prerequisites: no changes'

downloader_ready=0; fail_install=0; : > "$calls"
curl() {
  printf 'curl download\n' >> "$calls"
  while [ "$#" -gt 0 ]; do
    if [ "$1" = --output ]; then printf 'downloaded binary' > "$2"; return; fi
    shift
  done
  return 1
}
docker_runtime_download 'https://example.invalid/docker.tgz' "$test_root/download"
grep -q '^env DEBIAN_FRONTEND=noninteractive apt-get install -y curl ca-certificates$' "$calls"
[ -s "$test_root/download" ]
echo 'PASS minimal Ubuntu: bootstrap downloader before fetching Docker'
echo 'Ubuntu Docker prerequisite smoke tests PASS (package manager mocked).'
