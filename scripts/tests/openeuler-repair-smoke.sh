#!/usr/bin/env bash
# Real repair entry, archive generation and extraction; build container mocked.
# 执行当前脚本步骤。
set -Eeuo pipefail
# 执行当前脚本步骤。
scripts="$(cd "$(dirname "$0")/.." && pwd)"
# 结束当前控制块。
fixture_root="$(mktemp -d)"
# 执行当前脚本步骤。
trap 'rm -rf -- "$fixture_root"' EXIT
# 结束当前控制块。
fixture_bundle="$fixture_root/old bundle"
# 执行当前脚本步骤。
mkdir -p "$fixture_bundle/docker-runtime/packages"
# 执行当前脚本步骤。
printf 'existing images' > "$fixture_bundle/images.tar"
# 执行当前脚本步骤。
printf 'existing config' > "$fixture_bundle/.env.offline"
# 执行当前脚本步骤。
printf 'openeuler\n24.03\n24.03 (LTS-SP4)\nx86_64\n' > "$fixture_bundle/docker-runtime/packages/target-os"
# 执行当前脚本步骤。
printf fixture > "$fixture_bundle/docker-runtime/packages/container-selinux-test.rpm"
# 遍历数据并执行循环体。
for fixture_file in "$fixture_bundle"/docker-runtime/packages/*; do sha256sum "$fixture_file" | awk '{print $1}' > "$fixture_file.sha256"; done
# 执行当前脚本步骤。
docker() {
  # 执行当前脚本步骤。
  printf '%s\n' "$*" >> "$IOT_TEST_REPAIR_CALLS"
  # 执行当前脚本步骤。
  [[ "$*" == *'--index-only'* && "$*" != *' build '* && "$*" != *' save '* ]] || return 1
  # 执行当前脚本步骤。
  [ "${IOT_TEST_REPAIR_FAILURE:-0}" = 0 ] || return 14
  # 执行当前脚本步骤。
  local arg target=''
  # 遍历数据并执行循环体。
  for arg in "$@"; do
    # 执行当前脚本步骤。
    case "$arg" in type=bind,source=*,target=/packages) target="${arg#type=bind,source=}"; target="${target%,target=/packages}";; esac
  # 结束当前控制块。
  done
  # 执行当前脚本步骤。
  mkdir -p "$target/repodata"
  # 遍历数据并执行循环体。
  for arg in repodata/repomd.xml RPM-GPG-KEY-openEuler; do
    # 执行当前脚本步骤。
    printf fixture > "$target/$arg"
    # 执行当前脚本步骤。
    sha256sum "$target/$arg" | awk '{print $1}' > "$target/$arg.sha256"
  # 结束当前控制块。
  done
# 结束当前控制块。
}
# 执行当前脚本步骤。
export -f docker
# 执行当前脚本步骤。
export IOT_TEST_REPAIR_CALLS="$fixture_root/calls"
# 执行当前脚本步骤。
bash "$scripts/repair-offline-openeuler.sh" "$fixture_bundle"
# 执行当前脚本步骤。
patch="${fixture_bundle}-rpm-repair.tar.gz"
# 执行当前脚本步骤。
(cd "$fixture_root"; sha256sum -c "$(basename "$patch").sha256")
# 执行当前脚本步骤。
tar -tzf "$patch" > "$fixture_root/contents"
# 执行当前脚本步骤。
! grep -Eq 'images.tar|\.env|\.rpm$|ollama' "$fixture_root/contents"
# 执行当前脚本步骤。
tar -xzf "$patch" -C "$fixture_bundle"
# 执行当前脚本步骤。
cmp "$scripts/lib/docker-bootstrap.sh" "$fixture_bundle/scripts/lib/docker-bootstrap.sh"
# 执行当前脚本步骤。
[ "$(cat "$fixture_bundle/images.tar")" = 'existing images' ]
# 执行当前脚本步骤。
[ "$(cat "$fixture_bundle/.env.offline")" = 'existing config' ]
# 执行当前脚本步骤。
echo 'PASS repair creates and applies a metadata-only patch, preserving images/config'
# 执行当前脚本步骤。
rm "$patch" "$patch.sha256"
# 判断条件后执行对应操作。
if IOT_TEST_REPAIR_FAILURE=1 bash "$scripts/repair-offline-openeuler.sh" "$fixture_bundle"; then echo 'Accepted failed build'; exit 1; fi
# 执行当前脚本步骤。
[ ! -e "$patch" ]
# 执行当前脚本步骤。
: > "$IOT_TEST_REPAIR_CALLS"
# 执行当前脚本步骤。
printf corrupt > "$fixture_bundle/docker-runtime/packages/container-selinux-test.rpm"
# 判断条件后执行对应操作。
if bash "$scripts/repair-offline-openeuler.sh" "$fixture_bundle"; then echo 'Accepted damaged source bundle'; exit 1; fi
# 执行当前脚本步骤。
[ ! -s "$IOT_TEST_REPAIR_CALLS" ]
# 执行当前脚本步骤。
echo 'PASS failed preparation and corrupt source produce no patch'
