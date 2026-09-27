#!/usr/bin/env bash
set -Eeuo pipefail

# openeuler-release
(
# Execute the actual package-preparation script up to its first DNF operation.
# Only the OS-release source and package manager are replaced; no RPM installs.
set -Eeuo pipefail
scripts="$(cd "$(dirname "$0")/.." && pwd)"
fixture_root="$(mktemp -d)"
trap 'rm -rf -- "$fixture_root"' EXIT
cat > "$fixture_root/bash-env" <<'ENV'
function .() {
  if [[ "$1" == /etc/os-release ]]; then builtin . "$IOT_TEST_OS_RELEASE"; else builtin . "$@"; fi
}
dnf() { echo 'REACHED_PACKAGE_PREPARATION'; exit 73; }
ENV
run_case() {
  local os_id="$1" os_version="$2" accept="$3" version_id="${4:-24.03}" result code
  printf 'ID="%s"\nVERSION_ID="%s"\nVERSION="%s"\n' "$os_id" "$version_id" "$os_version" > "$fixture_root/os-release"
  if result="$(BASH_ENV="$fixture_root/bash-env" IOT_TEST_OS_RELEASE="$fixture_root/os-release" bash "$scripts/lib/prepare-openeuler-packages.sh" 2>&1)"; then code=0; else code=$?; fi
  if [[ "$accept" == yes ]]; then
    [[ "$code" == 73 && "$result" == *REACHED_PACKAGE_PREPARATION* ]] || { printf 'FAIL official OS identity %s: %s\n' "$os_id" "$result"; return 1; }
  else
    [[ "$code" != 73 && "$code" != 0 && "$result" != *REACHED_PACKAGE_PREPARATION* ]] || { echo 'FAIL unsupported OS accepted'; return 1; }
  fi
}
run_case openEuler '24.03 (LTS-SP4)' yes
run_case openeuler '24.03 (LTS-SP4)' yes
run_case openEuler '24.03 (LTS SP4)' yes
run_case openeuler '24.03 (LTS SP4)' yes
run_case centos '24.03 (LTS-SP4)' no
run_case openEuler '24.03 (LTS-SP3)' no
echo 'PASS actual preparation entry accepts official ID casing, rejects wrong OS/SP'
run_case openEuler '24.03 (LTS-SP4)' no 22.03
# Target-side identity must normalize to exactly the marker written by preparation.
printf 'ID="openEuler"\nVERSION_ID="24.03"\nVERSION="24.03 (LTS SP4)"\n' > "$fixture_root/os-release"
identity="$(BASH_ENV="$fixture_root/bash-env" IOT_TEST_OS_RELEASE="$fixture_root/os-release" bash -c 'source "$1"; docker_runtime_host_identity' -- "$scripts/lib/docker-bootstrap.sh")"
expected="$(printf 'openeuler\n24.03\n24.03 (LTS-SP4)\n%s' "$(uname -m)")"
[[ "$identity" == "$expected" ]] || { echo 'FAIL target identity differs from package marker'; exit 1; }
echo 'PASS target identity uses the same canonical OS/version representation'
)

# openeuler-repair
(
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
)

# openeuler-rpm-preparation
(
# Exercise the real preparation script; external DNF/createrepo/key reads mocked.
set -Eeuo pipefail
scripts="$(cd "$(dirname "$0")/.." && pwd)"
fixture_root="$(mktemp -d)"
trap 'rm -rf -- "$fixture_root"' EXIT
export IOT_TEST_RPM_ROOT="$fixture_root"
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
BASH_ENV="$fixture_root/bash-env" bash "$scripts/lib/prepare-openeuler-packages.sh" download "$fixture_root/packages"
source "$scripts/lib/docker-bootstrap.sh"
for fixture_file in "$fixture_root"/packages/*.rpm "$fixture_root/packages/target-os" "$fixture_root/packages/RPM-GPG-KEY-openEuler" "$fixture_root/packages/repodata/repomd.xml"; do
  verify_docker_runtime_file "$fixture_file"
done
echo 'PASS actual preparation writes indexed candidates, key and checksums; tests named roots'
: > "$fixture_root/calls"
BASH_ENV="$fixture_root/bash-env" bash "$scripts/lib/prepare-openeuler-packages.sh" --index-only "$fixture_root/packages"
! grep -q '^download ' "$fixture_root/calls"
echo 'PASS actual reindex mode verifies existing RPMs and never downloads replacements'
if BASH_ENV="$fixture_root/bash-env" IOT_TEST_TRANSACTION_FAILURE=12 bash "$scripts/lib/prepare-openeuler-packages.sh" download "$fixture_root/packages" > "$fixture_root/failure" 2>&1; then
  echo 'Accepted incomplete local dependency repository'; exit 1
fi
! grep -q 'Prepared openEuler' "$fixture_root/failure"
echo 'PASS local transaction failure stops package preparation'
: > "$fixture_root/calls"
printf corrupt > "$fixture_root/packages/container-selinux-test.rpm"
if BASH_ENV="$fixture_root/bash-env" bash "$scripts/lib/prepare-openeuler-packages.sh" --index-only "$fixture_root/packages"; then
  echo 'Reindex accepted corrupt source RPM'; exit 1
fi
[ ! -s "$fixture_root/calls" ]
printf 'wrong OS' > "$fixture_root/packages/target-os"
if BASH_ENV="$fixture_root/bash-env" bash "$scripts/lib/prepare-openeuler-packages.sh" --index-only "$fixture_root/packages"; then
  echo 'Reindex accepted wrong source OS'; exit 1
fi
[ ! -s "$fixture_root/calls" ]
echo 'PASS actual reindex rejects damaged or mismatched source before DNF'
)
