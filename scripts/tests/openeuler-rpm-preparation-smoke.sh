#!/usr/bin/env bash
# Exercise the real preparation script; external DNF/createrepo/key reads mocked.
# 执行当前脚本步骤。
set -Eeuo pipefail
# 执行当前脚本步骤。
scripts="$(cd "$(dirname "$0")/.." && pwd)"
# 结束当前控制块。
fixture_root="$(mktemp -d)"
# 执行当前脚本步骤。
trap 'rm -rf -- "$fixture_root"' EXIT
# 执行当前脚本步骤。
export IOT_TEST_RPM_ROOT="$fixture_root"
# 执行当前脚本步骤。
cat > "$fixture_root/bash-env" <<'ENV'
function .() {
  if [ "$1" = /etc/os-release ]; then
    ID=openEuler; VERSION_ID=24.03; VERSION='24.03 (LTS-SP4)'
  else builtin . "$@"; fi
}
dnf() {
  printf '%s\n' "$*" >> "$IOT_TEST_RPM_ROOT/calls"
  if [ "$1" = install ]; then
    mkdir -p "$out"
  elif [ "$1" = download ]; then
    for fixture_name in container-selinux policycoreutils-python-utils grub2-efi; do
      printf fixture > "$out/$fixture_name-test.rpm"
    done
  else
    [ -s "$out/repodata/repomd.xml" ]
    [[ "$*" == *'--disablerepo=*'* && "$*" == *'--repofrompath=iot-offline,file://'* ]]
    [[ "$*" == *'install -y container-selinux policycoreutils-python-utils iptables xz procps-ng curl' ]]
    [[ "$*" != *'.rpm'* && "$*" != *'--allowerasing'* ]]
    return "${IOT_TEST_TRANSACTION_FAILURE:-0}"
  fi
}
createrepo_c() { mkdir -p "$1/repodata"; printf metadata > "$1/repodata/repomd.xml"; }
cp() {
  if [ "$1" = /etc/pki/rpm-gpg/RPM-GPG-KEY-openEuler ]; then
    printf key > "$out/RPM-GPG-KEY-openEuler"
  else command cp "$@"; fi
}
ENV
# 执行当前脚本步骤。
BASH_ENV="$fixture_root/bash-env" bash "$scripts/lib/prepare-openeuler-packages.sh" download "$fixture_root/packages"
# 执行当前脚本步骤。
source "$scripts/lib/docker-bootstrap.sh"
# 遍历数据并执行循环体。
for fixture_file in "$fixture_root"/packages/*.rpm "$fixture_root/packages/target-os" "$fixture_root/packages/RPM-GPG-KEY-openEuler" "$fixture_root/packages/repodata/repomd.xml"; do
  # 执行当前脚本步骤。
  verify_docker_runtime_file "$fixture_file"
# 结束当前控制块。
done
# 执行当前脚本步骤。
echo 'PASS actual preparation writes indexed candidates, key and checksums; tests named roots'
# 执行当前脚本步骤。
: > "$fixture_root/calls"
# 执行当前脚本步骤。
BASH_ENV="$fixture_root/bash-env" bash "$scripts/lib/prepare-openeuler-packages.sh" --index-only "$fixture_root/packages"
# 执行当前脚本步骤。
! grep -q '^download ' "$fixture_root/calls"
# 执行当前脚本步骤。
echo 'PASS actual reindex mode verifies existing RPMs and never downloads replacements'
# 判断条件后执行对应操作。
if BASH_ENV="$fixture_root/bash-env" IOT_TEST_TRANSACTION_FAILURE=12 bash "$scripts/lib/prepare-openeuler-packages.sh" download "$fixture_root/packages" > "$fixture_root/failure" 2>&1; then
  # 执行当前脚本步骤。
  echo 'Accepted incomplete local dependency repository'; exit 1
# 结束当前控制块。
fi
# 执行当前脚本步骤。
! grep -q 'Prepared openEuler' "$fixture_root/failure"
# 执行当前脚本步骤。
echo 'PASS local transaction failure stops package preparation'
# 执行当前脚本步骤。
: > "$fixture_root/calls"
# 执行当前脚本步骤。
printf corrupt > "$fixture_root/packages/container-selinux-test.rpm"
# 判断条件后执行对应操作。
if BASH_ENV="$fixture_root/bash-env" bash "$scripts/lib/prepare-openeuler-packages.sh" --index-only "$fixture_root/packages"; then
  # 执行当前脚本步骤。
  echo 'Reindex accepted corrupt source RPM'; exit 1
# 结束当前控制块。
fi
# 执行当前脚本步骤。
[ ! -s "$fixture_root/calls" ]
# 执行当前脚本步骤。
printf 'wrong OS' > "$fixture_root/packages/target-os"
# 判断条件后执行对应操作。
if BASH_ENV="$fixture_root/bash-env" bash "$scripts/lib/prepare-openeuler-packages.sh" --index-only "$fixture_root/packages"; then
  # 执行当前脚本步骤。
  echo 'Reindex accepted wrong source OS'; exit 1
# 结束当前控制块。
fi
# 执行当前脚本步骤。
[ ! -s "$fixture_root/calls" ]
# 执行当前脚本步骤。
echo 'PASS actual reindex rejects damaged or mismatched source before DNF'
