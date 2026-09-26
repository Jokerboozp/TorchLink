#!/usr/bin/env bash
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
