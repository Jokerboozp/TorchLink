#!/usr/bin/env bash
# Test real Ubuntu prerequisite selection; never run apt/dpkg against the host.
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
mkdir -p "$test_root/packages"
# 执行当前脚本步骤。
calls="$test_root/calls"
# 执行当前脚本步骤。
ready=0; fail_install=0; downloader_ready=1; git_ready=0
# 执行当前脚本步骤。
command() {
  # 判断条件后执行对应操作。
  if [ "${1:-}" = -v ]; then
    # 执行当前脚本步骤。
    case "${2:-}" in
      # 执行当前脚本步骤。
      iptables|xz|ps) [ "$ready" = 1 ]; return;;
      # 执行当前脚本步骤。
      curl|wget) [ "$downloader_ready" = 1 ]; return;;
      # 执行当前脚本步骤。
      git) [ "$git_ready" = 1 ]; return;;
      # 执行当前脚本步骤。
      apt-get|dpkg|rpm) return 0;;
    # 执行当前脚本步骤。
    esac
  # 结束当前控制块。
  fi
  # 执行当前脚本步骤。
  builtin command "$@"
# 结束当前控制块。
}
# 执行当前脚本步骤。
docker_runtime_root() {
  # 执行当前脚本步骤。
  printf '%s\n' "$*" >> "$calls"
  # 执行当前脚本步骤。
  case "$*" in
    # 执行当前脚本步骤。
    *'apt-get install'*|'dpkg -i '*)
      # 执行当前脚本步骤。
      [ "$fail_install" = 0 ] || return 42
      # 执行当前脚本步骤。
      ready=1; downloader_ready=1; git_ready=1;;
  # 执行当前脚本步骤。
  esac
# 结束当前控制块。
}
# 执行当前脚本步骤。
: > "$calls"
# 执行当前脚本步骤。
docker_runtime_prerequisites online "$test_root"
# 执行当前脚本步骤。
grep -q '^apt-get update$' "$calls"
# 执行当前脚本步骤。
grep -q '^env DEBIAN_FRONTEND=noninteractive apt-get install -y iptables xz-utils procps curl ca-certificates$' "$calls"
# 执行当前脚本步骤。
echo 'PASS Ubuntu online: noninteractive apt prerequisite installation'

# Mixed-format package directory must select DEB on an Ubuntu host.
# 执行当前脚本步骤。
printf 'test deb' > "$test_root/packages/dependency.deb"
# 执行当前脚本步骤。
printf 'test rpm' > "$test_root/packages/dependency.rpm"
# 执行当前脚本步骤。
docker_runtime_hash "$test_root/packages/dependency.deb" > "$test_root/packages/dependency.deb.sha256"
# 执行当前脚本步骤。
ready=0; : > "$calls"
# 执行当前脚本步骤。
docker_runtime_prerequisites offline "$test_root"
# 执行当前脚本步骤。
grep -q '^dpkg -i ' "$calls"
# 执行当前脚本步骤。
! grep -q 'apt-get\|rpm' "$calls"
# 执行当前脚本步骤。
echo 'PASS Ubuntu offline: verified DEB packages only, no software repository access'

# 执行当前脚本步骤。
ready=0; : > "$calls"
# 执行当前脚本步骤。
printf corrupt >> "$test_root/packages/dependency.deb"
# 判断条件后执行对应操作。
if docker_runtime_prerequisites offline "$test_root"; then echo 'Corrupt DEB accepted'; exit 1; fi
# 执行当前脚本步骤。
[ ! -s "$calls" ]
# 执行当前脚本步骤。
echo 'PASS corrupt DEB: no package installation'

# 执行当前脚本步骤。
ready=0; fail_install=1; : > "$calls"
# 判断条件后执行对应操作。
if docker_runtime_prerequisites online "$test_root"; then echo 'apt failure ignored'; exit 1; fi
# 执行当前脚本步骤。
[ "$ready" = 0 ]
# 执行当前脚本步骤。
echo 'PASS apt failure: bootstrap stops'

# 执行当前脚本步骤。
ready=1; : > "$calls"
# 执行当前脚本步骤。
docker_runtime_prerequisites offline "$test_root/absent"
# 执行当前脚本步骤。
[ ! -s "$calls" ]
# 执行当前脚本步骤。
echo 'PASS existing Ubuntu prerequisites: no changes'

# 执行当前脚本步骤。
downloader_ready=0; fail_install=0; : > "$calls"
# 执行当前脚本步骤。
curl() {
  # 执行当前脚本步骤。
  printf 'curl download\n' >> "$calls"
  # 遍历数据并执行循环体。
  while [ "$#" -gt 0 ]; do
    # 判断条件后执行对应操作。
    if [ "$1" = --output ]; then printf 'downloaded binary' > "$2"; return; fi
    # 执行当前脚本步骤。
    shift
  # 结束当前控制块。
  done
  # 返回结果或结束当前脚本。
  return 1
# 结束当前控制块。
}
# 执行当前脚本步骤。
docker_runtime_download 'https://example.invalid/docker.tgz' "$test_root/download"
# 执行当前脚本步骤。
grep -q '^env DEBIAN_FRONTEND=noninteractive apt-get install -y curl ca-certificates$' "$calls"
# 执行当前脚本步骤。
[ -s "$test_root/download" ]
# 执行当前脚本步骤。
echo 'PASS minimal Ubuntu: bootstrap downloader before fetching Docker'
# 执行当前脚本步骤。
git_ready=0; : > "$calls"
# 执行当前脚本步骤。
ensure_deployment_git
# 执行当前脚本步骤。
grep -q '^env DEBIAN_FRONTEND=noninteractive apt-get install -y git ca-certificates$' "$calls"
# 执行当前脚本步骤。
[ "$git_ready" = 1 ]
# 执行当前脚本步骤。
: > "$calls"
# 执行当前脚本步骤。
ensure_deployment_git
# 执行当前脚本步骤。
[ ! -s "$calls" ]
# 执行当前脚本步骤。
git_ready=0; fail_install=1
# 判断条件后执行对应操作。
if ensure_deployment_git; then echo 'Git install failure ignored'; exit 1; fi
# 执行当前脚本步骤。
echo 'PASS Git prerequisite: install only when missing, propagate installation failure'
# 执行当前脚本步骤。
echo 'Ubuntu Docker prerequisite smoke tests PASS (package manager mocked).'
