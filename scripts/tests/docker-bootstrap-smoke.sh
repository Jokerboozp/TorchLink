#!/usr/bin/env bash
set -Eeuo pipefail

# docker-bootstrap
(
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
cp "$test_root/runtime/docker-24.0.9.tgz" "$test_root/runtime/docker-28.5.2.tgz"
printf 'test compose' > "$test_root/runtime/docker-compose"
printf x86_64 > "$test_root/runtime/architecture"
for file in "$test_root/runtime/"docker-*; do docker_runtime_hash "$file" > "$file.sha256"; done
calls="$test_root/calls"
cli=0; daemon=0; compose_version=''; downloads=0
kernel=3.10.0
machine_arch=x86_64
command() {
  if [ "${1:-}" = -v ] && [ "${2:-}" = docker ]; then [ "$cli" = 1 ]; return; fi
  if [ "${1:-}" = -v ] && [ "${2:-}" = systemctl ]; then return 0; fi
  if [ "${1:-}" = -v ] && [ "${2:-}" = dockerd ]; then return 1; fi
  builtin command "$@"
}
uname() {
  case "$1" in -s) echo Linux;; -m) echo "$machine_arch";; -r) echo "$kernel";; esac
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
  printf 'download %s\n' "$1" >> "$calls"
  case "$1" in
    *.tgz) cp "$test_root/runtime/docker-24.0.9.tgz" "$2";;
    *) printf 'mock plugin' > "$2";;
  esac
}
ensure_deployment_docker online
[ "$downloads" = 3 ] && [ "$cli" = 1 ] && [ "$daemon" = 1 ]
grep -q 'cli-plugins/docker-buildx' "$calls"
echo 'PASS online installation: downloads engine, Compose and Buildx then starts Docker'
printf 'test compose' > "$test_root/runtime/docker-compose"
docker_runtime_hash "$test_root/runtime/docker-compose" > "$test_root/runtime/docker-compose.sha256"
for kernel in 5.15.0-generic 6.8.0-generic; do
  reset_case
  ensure_deployment_docker offline "$test_root/runtime"
  [ "$cli" = 1 ] && [ "$daemon" = 1 ] && [ "$downloads" = 0 ]
done
echo 'PASS Ubuntu kernels: offline engine and Compose installation without downloads'
for machine_arch in x86_64 aarch64; do
  reset_case
  ensure_deployment_docker online
  [ "$downloads" = 3 ] && [ "$cli" = 1 ] && [ "$daemon" = 1 ]
  grep -q "static/stable/$machine_arch/docker-28.5.2.tgz" "$calls"
  grep -q "docker-compose-linux-$machine_arch" "$calls"
  build_arch=amd64; [ "$machine_arch" != aarch64 ] || build_arch=arm64
  grep -q "buildx-v0.14.1.linux-$build_arch" "$calls"
done
echo 'PASS amd64 and arm64: native engine, Compose and Buildx download selection'
echo 'Docker bootstrap smoke tests PASS (host writes mocked).'
)

# docker-rpm-repo
(
# Real installer, simulated BIOS host with grub2-pc installed. No host mutations.
set -Eeuo pipefail
source "$(cd "$(dirname "$0")/.." && pwd)/lib/docker-bootstrap.sh"
fixture_root="$(mktemp -d)"
trap 'rm -rf -- "$fixture_root"' EXIT
mkdir -p "$fixture_root/packages/repodata"
for fixture_file in container-selinux-test.rpm grub2-efi-test.rpm repodata/repomd.xml RPM-GPG-KEY-openEuler; do
  printf fixture > "$fixture_root/packages/$fixture_file"
  docker_runtime_hash "$fixture_root/packages/$fixture_file" > "$fixture_root/packages/$fixture_file.sha256"
done
fixture_manager=dnf
command() {
  if [ "${1:-}" = -v ]; then
    case "${2:-}" in dnf|yum) [ "$2" = "$fixture_manager" ]; return;; esac
  fi
  builtin command "$@"
}
docker_runtime_root() {
  printf '%s\n' "$*" >> "$fixture_root/calls"
  local arg repo_dir=''
  for arg in "$@"; do
    case "$arg" in
      *.rpm) echo 'The operation would result in removing the following protected packages: grub2-pc' >&2; return 1;;
      --setopt=reposdir=*) repo_dir="${arg#--setopt=reposdir=}";;
      --allowerasing|--skip-broken|--nogpgcheck|--nodeps) return 1;;
    esac
  done
  [[ "$*" == *'--disablerepo=*'* && "$*" == *'--enablerepo=iot-offline'* ]] || return 1
  grep -q '^baseurl=file://' "$repo_dir/iot-offline.repo"
  grep -q '^gpgcheck=1$' "$repo_dir/iot-offline.repo"
  ! grep -Eq 'https?://' "$repo_dir/iot-offline.repo"
  printf '%s' "$repo_dir" > "$fixture_root/repo-dir"
  return "${fixture_failure:-0}"
}
docker_runtime_install_packages "$fixture_root" container-selinux policycoreutils-python-utils
grep -q 'install -y container-selinux policycoreutils-python-utils$' "$fixture_root/calls"
[ ! -e "$(cat "$fixture_root/repo-dir")" ]
echo 'PASS only requested packages are installation goals; unrelated boot RPM stays a candidate'
fixture_manager=yum
docker_runtime_install_packages "$fixture_root" iptables xz procps-ng
grep -q '^yum .*install -y iptables xz procps-ng$' "$fixture_root/calls"
echo 'PASS yum uses the same isolated offline repository'
fixture_failure=42
if docker_runtime_install_packages "$fixture_root" iptables; then echo 'Accepted failed transaction'; exit 1; fi
[ ! -e "$(cat "$fixture_root/repo-dir")" ]
: > "$fixture_root/calls"
printf corrupt > "$fixture_root/packages/repodata/repomd.xml"
if docker_runtime_install_packages "$fixture_root" iptables; then echo 'Accepted corrupt repository'; exit 1; fi
[ ! -s "$fixture_root/calls" ]
rm "$fixture_root/packages/repodata/repomd.xml"
if docker_runtime_install_packages "$fixture_root" iptables; then echo 'Accepted unindexed legacy bundle'; exit 1; fi
[ ! -s "$fixture_root/calls" ]
echo 'PASS failed transaction propagates; corrupt or missing metadata stops before installation'
)

# docker-selinux
(
# Real bootstrap entry point with a simulated enforcing host; no host changes.
set -Eeuo pipefail
source "$(cd "$(dirname "$0")/.." && pwd)/lib/docker-bootstrap.sh"
root="$(mktemp -d)"
trap 'rm -rf -- "$root"' EXIT
command() {
  if [ "${1:-}" = -v ]; then
    case "${2:-}" in docker|getenforce|systemctl) return 0;; esac
  fi
  builtin command "$@"
}
getenforce() { echo Enforcing; }
docker() {
  case "$1" in
    info) return 0;;
    compose) echo 2.27.3;;
    context) echo unix:///var/run/docker.sock;;
    load)
      [ -f "$root/ready" ] || { echo "failed to mknod('/redpanda-rpk.deb', S_IFCHR, 0): permission denied" >&2; return 1; };;
  esac
}
systemctl() { echo 'ExecStart=/usr/local/lib/iot-docker/dockerd'; }
# The host setup is tested separately below once the bootstrap calls it.
docker_runtime_selinux() { touch "$root/ready"; }
ensure_deployment_docker offline "$root"
docker load -i images.tar
echo 'PASS existing managed Docker is checked before image loading'

# Run the actual SELinux repair and offline package installer with host commands mocked.
source "$(cd "$(dirname "$0")/.." && pwd)/lib/docker-bootstrap.sh"
fixture_policy=0; fixture_mapping=0; fixture_active=1; fixture_domain=init_t; fixture_transition=1; fixture_state=Enforcing
calls="$root/calls"; : > "$calls"
command() {
  if [ "${1:-}" = -v ]; then
    case "${2:-}" in
      docker|getenforce|systemctl|dnf|matchpathcon|restorecon|semanage) return 0;;
    esac
  fi
  builtin command "$@"
}
getenforce() { echo "$fixture_state"; }
docker_runtime_host_identity() { printf 'openeuler\n24.03\n24.03 (LTS-SP4)\nx86_64\n'; }
matchpathcon() {
  case "$1:$2" in
    -n:/usr/bin/dockerd) [ "$fixture_policy" = 1 ] && echo system_u:object_r:container_runtime_exec_t:s0 || echo system_u:object_r:bin_t:s0;;
    -n:/usr/local/lib/iot-docker/dockerd) [ "$fixture_mapping" = 1 ] && echo system_u:object_r:container_runtime_exec_t:s0 || echo system_u:object_r:lib_t:s0;;
    -V:*) [ "$fixture_mapping" = 1 ];;
  esac
}
systemctl() {
  case "$1:$2" in
    is-active:*) [ "$fixture_active" = 1 ];;
    show:*) if [[ "$*" == *MainPID* ]]; then echo MainPID=123; else echo 'ExecStart=/usr/local/lib/iot-docker/dockerd'; fi;;
  esac
}
ps() { echo "system_u:system_r:$fixture_domain:s0"; }
docker() {
  case "$1" in
    info) if [[ "$*" == *DockerRootDir* ]]; then echo /var/lib/docker; fi;;
    context) echo "${fixture_endpoint:-unix:///var/run/docker.sock}";;
    compose) echo 2.27.3;;
    load) [ "$fixture_domain" = container_runtime_t ] || { echo "failed to mknod('/redpanda-rpk.deb', S_IFCHR, 0): permission denied" >&2; return 1; };;
  esac
}
docker_runtime_root() {
  printf '%s\n' "$*" >> "$calls"
  case "$1:$2" in
    dnf:*) [[ "$*" == *"--disablerepo=*"* ]] || return 1; fixture_policy=1;;
    semanage:fcontext) fixture_mapping=1;;
    systemctl:stop) fixture_active=0;;
    systemctl:start) fixture_active=1; [ "$fixture_transition" = 0 ] || fixture_domain=container_runtime_t;;
  esac
}
if ensure_deployment_docker offline "$root"; then echo 'Accepted enforcing host without fixture_policy packages'; exit 1; fi
[ ! -s "$calls" ]
echo 'PASS missing SELinux dependency stops before relabel, restart and image load'
mkdir -p "$root/packages"
printf 'fixture RPM' > "$root/packages/container-selinux-test.rpm"
printf corrupt > "$root/packages/container-selinux-test.rpm.sha256"
if ensure_deployment_docker offline "$root"; then echo 'Accepted corrupt RPM'; exit 1; fi
[ ! -s "$calls" ]
docker_runtime_hash "$root/packages/container-selinux-test.rpm" > "$root/packages/container-selinux-test.rpm.sha256"
printf 'different OS' > "$root/packages/target-os"
docker_runtime_hash "$root/packages/target-os" > "$root/packages/target-os.sha256"
if ensure_deployment_docker offline "$root"; then echo 'Accepted wrong target OS'; exit 1; fi
[ ! -s "$calls" ]
echo 'PASS corrupt RPM and mismatched target OS stop before host changes'
docker_runtime_host_identity > "$root/packages/target-os"
docker_runtime_hash "$root/packages/target-os" > "$root/packages/target-os.sha256"
mkdir -p "$root/packages/repodata"
for fixture_file in repodata/repomd.xml RPM-GPG-KEY-openEuler; do
  printf fixture > "$root/packages/$fixture_file"
  docker_runtime_hash "$root/packages/$fixture_file" > "$root/packages/$fixture_file.sha256"
done
ensure_deployment_docker offline "$root"
docker load -i images.tar
grep -q '^dnf .*install -y container-selinux policycoreutils-python-utils$' "$calls"
grep -q '^semanage fcontext -a -e /usr/bin /usr/local/lib/iot-docker' "$calls"
grep -q '^systemctl stop docker' "$calls"
grep -q '^systemctl start docker' "$calls"
! grep -Eq 'setenforce|--nodeps|--force' "$calls"
echo 'PASS policy install, persistent mapping and process transition; no SELinux disable'
: > "$calls"
ensure_deployment_docker offline "$root"
[ ! -s "$calls" ]
echo 'PASS repaired runtime is idempotent: no reinstall or restart'
fixture_domain=init_t; fixture_transition=0
if ensure_deployment_docker offline "$root"; then echo 'Accepted daemon still in init_t'; exit 1; fi
echo 'PASS failed domain transition blocks image load'
: > "$calls"; fixture_endpoint=ssh://another-host
ensure_deployment_docker offline "$root"
[ ! -s "$calls" ]
echo 'PASS remote Docker leaves local policy and service untouched'
DOCKER_CONTEXT=remote; DOCKER_HOST=unix:///var/run/docker.sock
ensure_deployment_docker offline "$root"
[ ! -s "$calls" ]
unset DOCKER_CONTEXT DOCKER_HOST
echo 'PASS DOCKER_CONTEXT takes precedence over DOCKER_HOST'
fixture_endpoint=unix:///var/run/docker.sock; fixture_state=Disabled
ensure_deployment_docker offline "$root"
[ ! -s "$calls" ]
echo 'PASS SELinux-disabled runtime is unchanged'
echo 'SELinux bootstrap smoke PASS (system policy and kernel calls simulated).'

# Exercise the Bash packaging caller, with the external build container mocked.
fixture_prepare_dir="$root/prepared"
fixture_prepare_fail=0
docker() {
  [ "$1" = run ] && [[ "$*" == *'--platform linux/amd64'* ]] && [[ "$*" == *'openeuler/openeuler:24.03-lts-sp4'* ]] || return 1
  [ "$fixture_prepare_fail" = 0 ] || return 17
  printf 'fixture OS' > "$fixture_prepare_dir/target-os"
  docker_runtime_hash "$fixture_prepare_dir/target-os" > "$fixture_prepare_dir/target-os.sha256"
  printf 'fixture RPM' > "$fixture_prepare_dir/container-selinux-test.rpm"
  mkdir -p "$fixture_prepare_dir/repodata"
  for fixture_file in repodata/repomd.xml RPM-GPG-KEY-openEuler; do
    printf fixture > "$fixture_prepare_dir/$fixture_file"
    docker_runtime_hash "$fixture_prepare_dir/$fixture_file" > "$fixture_prepare_dir/$fixture_file.sha256"
  done
}
prepare_openeuler_packages "$fixture_prepare_dir" /prepare.sh x86_64
fixture_prepare_fail=1
if prepare_openeuler_packages "$fixture_prepare_dir" /prepare.sh x86_64; then echo 'Accepted failed package preparation'; exit 1; fi
echo 'PASS Bash package preparation selects target and propagates container failure'
)

# docker-ubuntu
(
# Test real Ubuntu prerequisite selection; never run apt/dpkg against the host.
set -Eeuo pipefail
scripts="$(cd "$(dirname "$0")/.." && pwd)"
source "$scripts/lib/docker-bootstrap.sh"
test_root="$(mktemp -d)"
trap 'rm -rf -- "$test_root"' EXIT
mkdir -p "$test_root/packages"
calls="$test_root/calls"
ready=0; fail_install=0; downloader_ready=1; git_ready=0
command() {
  if [ "${1:-}" = -v ]; then
    case "${2:-}" in
      iptables|xz|ps) [ "$ready" = 1 ]; return;;
      curl|wget) [ "$downloader_ready" = 1 ]; return;;
      git) [ "$git_ready" = 1 ]; return;;
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
      ready=1; downloader_ready=1; git_ready=1;;
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
git_ready=0; : > "$calls"
ensure_deployment_git
grep -q '^env DEBIAN_FRONTEND=noninteractive apt-get install -y git ca-certificates$' "$calls"
[ "$git_ready" = 1 ]
: > "$calls"
ensure_deployment_git
[ ! -s "$calls" ]
git_ready=0; fail_install=1
if ensure_deployment_git; then echo 'Git install failure ignored'; exit 1; fi
echo 'PASS Git prerequisite: install only when missing, propagate installation failure'
echo 'Ubuntu Docker prerequisite smoke tests PASS (package manager mocked).'
)
