#!/usr/bin/env bash
# Real repair entry, archive generation and extraction; build container mocked.
set -Eeuo pipefail
scripts="$(cd "$(dirname "$0")/.." && pwd)"
fixture_root="$(mktemp -d)"
trap 'rm -rf -- "$fixture_root"' EXIT
fixture_bundle="$fixture_root/old bundle"
mkdir -p "$fixture_bundle/docker-runtime/packages"
printf 'existing images' > "$fixture_bundle/images.tar"
printf 'existing config' > "$fixture_bundle/.env.offline"
printf 'openeuler\n24.03\n24.03 (LTS-SP4)\nx86_64\n' > "$fixture_bundle/docker-runtime/packages/target-os"
printf fixture > "$fixture_bundle/docker-runtime/packages/container-selinux-test.rpm"
for fixture_file in "$fixture_bundle"/docker-runtime/packages/*; do sha256sum "$fixture_file" | awk '{print $1}' > "$fixture_file.sha256"; done
docker() {
  printf '%s\n' "$*" >> "$IOT_TEST_REPAIR_CALLS"
  [[ "$*" == *'--index-only'* && "$*" != *' build '* && "$*" != *' save '* ]] || return 1
  [ "${IOT_TEST_REPAIR_FAILURE:-0}" = 0 ] || return 14
  local arg target=''
  for arg in "$@"; do
    case "$arg" in type=bind,source=*,target=/packages) target="${arg#type=bind,source=}"; target="${target%,target=/packages}";; esac
  done
  mkdir -p "$target/repodata"
  for arg in repodata/repomd.xml RPM-GPG-KEY-openEuler; do
    printf fixture > "$target/$arg"
    sha256sum "$target/$arg" | awk '{print $1}' > "$target/$arg.sha256"
  done
}
export -f docker
export IOT_TEST_REPAIR_CALLS="$fixture_root/calls"
bash "$scripts/repair-offline-openeuler.sh" "$fixture_bundle"
patch="${fixture_bundle}-rpm-repair.tar.gz"
(cd "$fixture_root"; sha256sum -c "$(basename "$patch").sha256")
tar -tzf "$patch" > "$fixture_root/contents"
! grep -Eq 'images.tar|\.env|\.rpm$|ollama' "$fixture_root/contents"
tar -xzf "$patch" -C "$fixture_bundle"
cmp "$scripts/lib/docker-bootstrap.sh" "$fixture_bundle/scripts/lib/docker-bootstrap.sh"
[ "$(cat "$fixture_bundle/images.tar")" = 'existing images' ]
[ "$(cat "$fixture_bundle/.env.offline")" = 'existing config' ]
echo 'PASS repair creates and applies a metadata-only patch, preserving images/config'
rm "$patch" "$patch.sha256"
if IOT_TEST_REPAIR_FAILURE=1 bash "$scripts/repair-offline-openeuler.sh" "$fixture_bundle"; then echo 'Accepted failed build'; exit 1; fi
[ ! -e "$patch" ]
: > "$IOT_TEST_REPAIR_CALLS"
printf corrupt > "$fixture_bundle/docker-runtime/packages/container-selinux-test.rpm"
if bash "$scripts/repair-offline-openeuler.sh" "$fixture_bundle"; then echo 'Accepted damaged source bundle'; exit 1; fi
[ ! -s "$IOT_TEST_REPAIR_CALLS" ]
echo 'PASS failed preparation and corrupt source produce no patch'
