#!/usr/bin/env bash
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
