#!/usr/bin/env bash
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
