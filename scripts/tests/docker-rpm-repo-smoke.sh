#!/usr/bin/env bash
# Real installer, simulated BIOS host with grub2-pc installed. No host mutations.
# 执行当前脚本步骤。
set -Eeuo pipefail
# 执行当前脚本步骤。
source "$(cd "$(dirname "$0")/.." && pwd)/lib/docker-bootstrap.sh"
# 结束当前控制块。
fixture_root="$(mktemp -d)"
# 执行当前脚本步骤。
trap 'rm -rf -- "$fixture_root"' EXIT
# 执行当前脚本步骤。
mkdir -p "$fixture_root/packages/repodata"
# 遍历数据并执行循环体。
for fixture_file in container-selinux-test.rpm grub2-efi-test.rpm repodata/repomd.xml RPM-GPG-KEY-openEuler; do
  # 执行当前脚本步骤。
  printf fixture > "$fixture_root/packages/$fixture_file"
  # 执行当前脚本步骤。
  docker_runtime_hash "$fixture_root/packages/$fixture_file" > "$fixture_root/packages/$fixture_file.sha256"
# 结束当前控制块。
done
# 结束当前控制块。
fixture_manager=dnf
# 执行当前脚本步骤。
command() {
  # 判断条件后执行对应操作。
  if [ "${1:-}" = -v ]; then
    # 执行当前脚本步骤。
    case "${2:-}" in dnf|yum) [ "$2" = "$fixture_manager" ]; return;; esac
  # 结束当前控制块。
  fi
  # 执行当前脚本步骤。
  builtin command "$@"
# 结束当前控制块。
}
# 执行当前脚本步骤。
docker_runtime_root() {
  # 执行当前脚本步骤。
  printf '%s\n' "$*" >> "$fixture_root/calls"
  # 执行当前脚本步骤。
  local arg repo_dir=''
  # 遍历数据并执行循环体。
  for arg in "$@"; do
    # 执行当前脚本步骤。
    case "$arg" in
      # 执行当前脚本步骤。
      *.rpm) echo 'The operation would result in removing the following protected packages: grub2-pc' >&2; return 1;;
      # 执行当前脚本步骤。
      --setopt=reposdir=*) repo_dir="${arg#--setopt=reposdir=}";;
      # 执行当前脚本步骤。
      --allowerasing|--skip-broken|--nogpgcheck|--nodeps) return 1;;
    # 执行当前脚本步骤。
    esac
  # 结束当前控制块。
  done
  # 执行当前脚本步骤。
  [[ "$*" == *'--disablerepo=*'* && "$*" == *'--enablerepo=iot-offline'* ]] || return 1
  # 执行当前脚本步骤。
  grep -q '^baseurl=file://' "$repo_dir/iot-offline.repo"
  # 执行当前脚本步骤。
  grep -q '^gpgcheck=1$' "$repo_dir/iot-offline.repo"
  # 执行当前脚本步骤。
  ! grep -Eq 'https?://' "$repo_dir/iot-offline.repo"
  # 执行当前脚本步骤。
  printf '%s' "$repo_dir" > "$fixture_root/repo-dir"
  # 返回结果或结束当前脚本。
  return "${fixture_failure:-0}"
# 结束当前控制块。
}
# 执行当前脚本步骤。
docker_runtime_install_packages "$fixture_root" container-selinux policycoreutils-python-utils
# 执行当前脚本步骤。
grep -q 'install -y container-selinux policycoreutils-python-utils$' "$fixture_root/calls"
# 执行当前脚本步骤。
[ ! -e "$(cat "$fixture_root/repo-dir")" ]
# 执行当前脚本步骤。
echo 'PASS only requested packages are installation goals; unrelated boot RPM stays a candidate'
# 结束当前控制块。
fixture_manager=yum
# 执行当前脚本步骤。
docker_runtime_install_packages "$fixture_root" iptables xz procps-ng
# 执行当前脚本步骤。
grep -q '^yum .*install -y iptables xz procps-ng$' "$fixture_root/calls"
# 执行当前脚本步骤。
echo 'PASS yum uses the same isolated offline repository'
# 结束当前控制块。
fixture_failure=42
# 判断条件后执行对应操作。
if docker_runtime_install_packages "$fixture_root" iptables; then echo 'Accepted failed transaction'; exit 1; fi
# 执行当前脚本步骤。
[ ! -e "$(cat "$fixture_root/repo-dir")" ]
# 执行当前脚本步骤。
: > "$fixture_root/calls"
# 执行当前脚本步骤。
printf corrupt > "$fixture_root/packages/repodata/repomd.xml"
# 判断条件后执行对应操作。
if docker_runtime_install_packages "$fixture_root" iptables; then echo 'Accepted corrupt repository'; exit 1; fi
# 执行当前脚本步骤。
[ ! -s "$fixture_root/calls" ]
# 执行当前脚本步骤。
rm "$fixture_root/packages/repodata/repomd.xml"
# 判断条件后执行对应操作。
if docker_runtime_install_packages "$fixture_root" iptables; then echo 'Accepted unindexed legacy bundle'; exit 1; fi
# 执行当前脚本步骤。
[ ! -s "$fixture_root/calls" ]
# 执行当前脚本步骤。
echo 'PASS failed transaction propagates; corrupt or missing metadata stops before installation'
