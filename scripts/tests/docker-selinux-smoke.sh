#!/usr/bin/env bash
# Real bootstrap entry point with a simulated enforcing host; no host changes.
# 执行当前脚本步骤。
set -Eeuo pipefail
# 执行当前脚本步骤。
source "$(cd "$(dirname "$0")/.." && pwd)/lib/docker-bootstrap.sh"
# 执行当前脚本步骤。
root="$(mktemp -d)"
# 执行当前脚本步骤。
trap 'rm -rf -- "$root"' EXIT
# 执行当前脚本步骤。
command() {
  # 判断条件后执行对应操作。
  if [ "${1:-}" = -v ]; then
    # 执行当前脚本步骤。
    case "${2:-}" in docker|getenforce|systemctl) return 0;; esac
  # 结束当前控制块。
  fi
  # 执行当前脚本步骤。
  builtin command "$@"
# 结束当前控制块。
}
# 执行当前脚本步骤。
getenforce() { echo Enforcing; }
# 执行当前脚本步骤。
docker() {
  # 执行当前脚本步骤。
  case "$1" in
    # 执行当前脚本步骤。
    info) return 0;;
    # 执行当前脚本步骤。
    compose) echo 2.27.3;;
    # 执行当前脚本步骤。
    context) echo unix:///var/run/docker.sock;;
    # 执行当前脚本步骤。
    load)
      # 执行当前脚本步骤。
      [ -f "$root/ready" ] || { echo "failed to mknod('/redpanda-rpk.deb', S_IFCHR, 0): permission denied" >&2; return 1; };;
  # 执行当前脚本步骤。
  esac
# 结束当前控制块。
}
# 执行当前脚本步骤。
systemctl() { echo 'ExecStart=/usr/local/lib/iot-docker/dockerd'; }
# The host setup is tested separately below once the bootstrap calls it.
# 执行当前脚本步骤。
docker_runtime_selinux() { touch "$root/ready"; }
# 执行当前脚本步骤。
ensure_deployment_docker offline "$root"
# 执行当前脚本步骤。
docker load -i images.tar
# 执行当前脚本步骤。
echo 'PASS existing managed Docker is checked before image loading'

# Run the actual SELinux repair and offline package installer with host commands mocked.
# 执行当前脚本步骤。
source "$(cd "$(dirname "$0")/.." && pwd)/lib/docker-bootstrap.sh"
# 结束当前控制块。
fixture_policy=0; fixture_mapping=0; fixture_active=1; fixture_domain=init_t; fixture_transition=1; fixture_state=Enforcing
# 执行当前脚本步骤。
calls="$root/calls"; : > "$calls"
# 执行当前脚本步骤。
command() {
  # 判断条件后执行对应操作。
  if [ "${1:-}" = -v ]; then
    # 执行当前脚本步骤。
    case "${2:-}" in
      # 执行当前脚本步骤。
      docker|getenforce|systemctl|dnf|matchpathcon|restorecon|semanage) return 0;;
    # 执行当前脚本步骤。
    esac
  # 结束当前控制块。
  fi
  # 执行当前脚本步骤。
  builtin command "$@"
# 结束当前控制块。
}
# 执行当前脚本步骤。
getenforce() { echo "$fixture_state"; }
# 执行当前脚本步骤。
docker_runtime_host_identity() { printf 'openeuler\n24.03\n24.03 (LTS-SP4)\nx86_64\n'; }
# 执行当前脚本步骤。
matchpathcon() {
  # 执行当前脚本步骤。
  case "$1:$2" in
    # 执行当前脚本步骤。
    -n:/usr/bin/dockerd) [ "$fixture_policy" = 1 ] && echo system_u:object_r:container_runtime_exec_t:s0 || echo system_u:object_r:bin_t:s0;;
    # 执行当前脚本步骤。
    -n:/usr/local/lib/iot-docker/dockerd) [ "$fixture_mapping" = 1 ] && echo system_u:object_r:container_runtime_exec_t:s0 || echo system_u:object_r:lib_t:s0;;
    # 执行当前脚本步骤。
    -V:*) [ "$fixture_mapping" = 1 ];;
  # 执行当前脚本步骤。
  esac
# 结束当前控制块。
}
# 执行当前脚本步骤。
systemctl() {
  # 执行当前脚本步骤。
  case "$1:$2" in
    # 执行当前脚本步骤。
    is-active:*) [ "$fixture_active" = 1 ];;
    # 执行当前脚本步骤。
    show:*) if [[ "$*" == *MainPID* ]]; then echo MainPID=123; else echo 'ExecStart=/usr/local/lib/iot-docker/dockerd'; fi;;
  # 执行当前脚本步骤。
  esac
# 结束当前控制块。
}
# 执行当前脚本步骤。
ps() { echo "system_u:system_r:$fixture_domain:s0"; }
# 执行当前脚本步骤。
docker() {
  # 执行当前脚本步骤。
  case "$1" in
    # 执行当前脚本步骤。
    info) if [[ "$*" == *DockerRootDir* ]]; then echo /var/lib/docker; fi;;
    # 执行当前脚本步骤。
    context) echo "${fixture_endpoint:-unix:///var/run/docker.sock}";;
    # 执行当前脚本步骤。
    compose) echo 2.27.3;;
    # 执行当前脚本步骤。
    load) [ "$fixture_domain" = container_runtime_t ] || { echo "failed to mknod('/redpanda-rpk.deb', S_IFCHR, 0): permission denied" >&2; return 1; };;
  # 执行当前脚本步骤。
  esac
# 结束当前控制块。
}
# 执行当前脚本步骤。
docker_runtime_root() {
  # 执行当前脚本步骤。
  printf '%s\n' "$*" >> "$calls"
  # 执行当前脚本步骤。
  case "$1:$2" in
    # 执行当前脚本步骤。
    dnf:*) [[ "$*" == *"--disablerepo=*"* ]] || return 1; fixture_policy=1;;
    # 执行当前脚本步骤。
    semanage:fcontext) fixture_mapping=1;;
    # 执行当前脚本步骤。
    systemctl:stop) fixture_active=0;;
    # 执行当前脚本步骤。
    systemctl:start) fixture_active=1; [ "$fixture_transition" = 0 ] || fixture_domain=container_runtime_t;;
  # 执行当前脚本步骤。
  esac
# 结束当前控制块。
}
# 判断条件后执行对应操作。
if ensure_deployment_docker offline "$root"; then echo 'Accepted enforcing host without fixture_policy packages'; exit 1; fi
# 执行当前脚本步骤。
[ ! -s "$calls" ]
# 执行当前脚本步骤。
echo 'PASS missing SELinux dependency stops before relabel, restart and image load'
# 执行当前脚本步骤。
mkdir -p "$root/packages"
# 执行当前脚本步骤。
printf 'fixture RPM' > "$root/packages/container-selinux-test.rpm"
# 执行当前脚本步骤。
printf corrupt > "$root/packages/container-selinux-test.rpm.sha256"
# 判断条件后执行对应操作。
if ensure_deployment_docker offline "$root"; then echo 'Accepted corrupt RPM'; exit 1; fi
# 执行当前脚本步骤。
[ ! -s "$calls" ]
# 执行当前脚本步骤。
docker_runtime_hash "$root/packages/container-selinux-test.rpm" > "$root/packages/container-selinux-test.rpm.sha256"
# 执行当前脚本步骤。
printf 'different OS' > "$root/packages/target-os"
# 执行当前脚本步骤。
docker_runtime_hash "$root/packages/target-os" > "$root/packages/target-os.sha256"
# 判断条件后执行对应操作。
if ensure_deployment_docker offline "$root"; then echo 'Accepted wrong target OS'; exit 1; fi
# 执行当前脚本步骤。
[ ! -s "$calls" ]
# 执行当前脚本步骤。
echo 'PASS corrupt RPM and mismatched target OS stop before host changes'
# 执行当前脚本步骤。
docker_runtime_host_identity > "$root/packages/target-os"
# 执行当前脚本步骤。
docker_runtime_hash "$root/packages/target-os" > "$root/packages/target-os.sha256"
# 执行当前脚本步骤。
mkdir -p "$root/packages/repodata"
# 遍历数据并执行循环体。
for fixture_file in repodata/repomd.xml RPM-GPG-KEY-openEuler; do
  # 执行当前脚本步骤。
  printf fixture > "$root/packages/$fixture_file"
  # 执行当前脚本步骤。
  docker_runtime_hash "$root/packages/$fixture_file" > "$root/packages/$fixture_file.sha256"
# 结束当前控制块。
done
# 执行当前脚本步骤。
ensure_deployment_docker offline "$root"
# 执行当前脚本步骤。
docker load -i images.tar
# 执行当前脚本步骤。
grep -q '^dnf .*install -y container-selinux policycoreutils-python-utils$' "$calls"
# 执行当前脚本步骤。
grep -q '^semanage fcontext -a -e /usr/bin /usr/local/lib/iot-docker' "$calls"
# 执行当前脚本步骤。
grep -q '^systemctl stop docker' "$calls"
# 执行当前脚本步骤。
grep -q '^systemctl start docker' "$calls"
# 执行当前脚本步骤。
! grep -Eq 'setenforce|--nodeps|--force' "$calls"
# 执行当前脚本步骤。
echo 'PASS policy install, persistent mapping and process transition; no SELinux disable'
# 执行当前脚本步骤。
: > "$calls"
# 执行当前脚本步骤。
ensure_deployment_docker offline "$root"
# 执行当前脚本步骤。
[ ! -s "$calls" ]
# 执行当前脚本步骤。
echo 'PASS repaired runtime is idempotent: no reinstall or restart'
# 结束当前控制块。
fixture_domain=init_t; fixture_transition=0
# 判断条件后执行对应操作。
if ensure_deployment_docker offline "$root"; then echo 'Accepted daemon still in init_t'; exit 1; fi
# 执行当前脚本步骤。
echo 'PASS failed domain transition blocks image load'
# 执行当前脚本步骤。
: > "$calls"; fixture_endpoint=ssh://another-host
# 执行当前脚本步骤。
ensure_deployment_docker offline "$root"
# 执行当前脚本步骤。
[ ! -s "$calls" ]
# 执行当前脚本步骤。
echo 'PASS remote Docker leaves local policy and service untouched'
# 执行当前脚本步骤。
DOCKER_CONTEXT=remote; DOCKER_HOST=unix:///var/run/docker.sock
# 执行当前脚本步骤。
ensure_deployment_docker offline "$root"
# 执行当前脚本步骤。
[ ! -s "$calls" ]
# 执行当前脚本步骤。
unset DOCKER_CONTEXT DOCKER_HOST
# 执行当前脚本步骤。
echo 'PASS DOCKER_CONTEXT takes precedence over DOCKER_HOST'
# 结束当前控制块。
fixture_endpoint=unix:///var/run/docker.sock; fixture_state=Disabled
# 执行当前脚本步骤。
ensure_deployment_docker offline "$root"
# 执行当前脚本步骤。
[ ! -s "$calls" ]
# 执行当前脚本步骤。
echo 'PASS SELinux-disabled runtime is unchanged'
# 执行当前脚本步骤。
echo 'SELinux bootstrap smoke PASS (system policy and kernel calls simulated).'

# Exercise the Bash packaging caller, with the external build container mocked.
# 结束当前控制块。
fixture_prepare_dir="$root/prepared"
# 结束当前控制块。
fixture_prepare_fail=0
# 执行当前脚本步骤。
docker() {
  # 执行当前脚本步骤。
  [ "$1" = run ] && [[ "$*" == *'--platform linux/amd64'* ]] && [[ "$*" == *'openeuler/openeuler:24.03-lts-sp4'* ]] || return 1
  # 执行当前脚本步骤。
  [ "$fixture_prepare_fail" = 0 ] || return 17
  # 执行当前脚本步骤。
  printf 'fixture OS' > "$fixture_prepare_dir/target-os"
  # 执行当前脚本步骤。
  docker_runtime_hash "$fixture_prepare_dir/target-os" > "$fixture_prepare_dir/target-os.sha256"
  # 执行当前脚本步骤。
  printf 'fixture RPM' > "$fixture_prepare_dir/container-selinux-test.rpm"
  # 执行当前脚本步骤。
  mkdir -p "$fixture_prepare_dir/repodata"
  # 遍历数据并执行循环体。
  for fixture_file in repodata/repomd.xml RPM-GPG-KEY-openEuler; do
    # 执行当前脚本步骤。
    printf fixture > "$fixture_prepare_dir/$fixture_file"
    # 执行当前脚本步骤。
    docker_runtime_hash "$fixture_prepare_dir/$fixture_file" > "$fixture_prepare_dir/$fixture_file.sha256"
  # 结束当前控制块。
  done
# 结束当前控制块。
}
# 执行当前脚本步骤。
prepare_openeuler_packages "$fixture_prepare_dir" /prepare.sh x86_64
# 结束当前控制块。
fixture_prepare_fail=1
# 判断条件后执行对应操作。
if prepare_openeuler_packages "$fixture_prepare_dir" /prepare.sh x86_64; then echo 'Accepted failed package preparation'; exit 1; fi
# 执行当前脚本步骤。
echo 'PASS Bash package preparation selects target and propagates container failure'
