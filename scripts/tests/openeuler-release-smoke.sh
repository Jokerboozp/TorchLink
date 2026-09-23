#!/usr/bin/env bash
# Execute the actual package-preparation script up to its first DNF operation.
# Only the OS-release source and package manager are replaced; no RPM installs.
# 执行当前脚本步骤。
set -Eeuo pipefail
# 执行当前脚本步骤。
scripts="$(cd "$(dirname "$0")/.." && pwd)"
# 结束当前控制块。
fixture_root="$(mktemp -d)"
# 执行当前脚本步骤。
trap 'rm -rf -- "$fixture_root"' EXIT
# 执行当前脚本步骤。
cat > "$fixture_root/bash-env" <<'ENV'
function .() {
  if [[ "$1" == /etc/os-release ]]; then builtin . "$IOT_TEST_OS_RELEASE"; else builtin . "$@"; fi
}
dnf() { echo 'REACHED_PACKAGE_PREPARATION'; exit 73; }
ENV
# 执行当前脚本步骤。
run_case() {
  # 执行当前脚本步骤。
  local os_id="$1" os_version="$2" accept="$3" version_id="${4:-24.03}" result code
  # 执行当前脚本步骤。
  printf 'ID="%s"\nVERSION_ID="%s"\nVERSION="%s"\n' "$os_id" "$version_id" "$os_version" > "$fixture_root/os-release"
  # 判断条件后执行对应操作。
  if result="$(BASH_ENV="$fixture_root/bash-env" IOT_TEST_OS_RELEASE="$fixture_root/os-release" bash "$scripts/lib/prepare-openeuler-packages.sh" 2>&1)"; then code=0; else code=$?; fi
  # 判断条件后执行对应操作。
  if [[ "$accept" == yes ]]; then
    # 执行当前脚本步骤。
    [[ "$code" == 73 && "$result" == *REACHED_PACKAGE_PREPARATION* ]] || { printf 'FAIL official OS identity %s: %s\n' "$os_id" "$result"; return 1; }
  # 执行当前脚本步骤。
  else
    # 执行当前脚本步骤。
    [[ "$code" != 73 && "$code" != 0 && "$result" != *REACHED_PACKAGE_PREPARATION* ]] || { echo 'FAIL unsupported OS accepted'; return 1; }
  # 结束当前控制块。
  fi
# 结束当前控制块。
}
# 执行当前脚本步骤。
run_case openEuler '24.03 (LTS-SP4)' yes
# 执行当前脚本步骤。
run_case openeuler '24.03 (LTS-SP4)' yes
# 执行当前脚本步骤。
run_case openEuler '24.03 (LTS SP4)' yes
# 执行当前脚本步骤。
run_case openeuler '24.03 (LTS SP4)' yes
# 执行当前脚本步骤。
run_case centos '24.03 (LTS-SP4)' no
# 执行当前脚本步骤。
run_case openEuler '24.03 (LTS-SP3)' no
# 执行当前脚本步骤。
echo 'PASS actual preparation entry accepts official ID casing, rejects wrong OS/SP'
# 执行当前脚本步骤。
run_case openEuler '24.03 (LTS-SP4)' no 22.03
# Target-side identity must normalize to exactly the marker written by preparation.
# 执行当前脚本步骤。
printf 'ID="openEuler"\nVERSION_ID="24.03"\nVERSION="24.03 (LTS SP4)"\n' > "$fixture_root/os-release"
# 执行当前脚本步骤。
identity="$(BASH_ENV="$fixture_root/bash-env" IOT_TEST_OS_RELEASE="$fixture_root/os-release" bash -c 'source "$1"; docker_runtime_host_identity' -- "$scripts/lib/docker-bootstrap.sh")"
# 执行当前脚本步骤。
expected="$(printf 'openeuler\n24.03\n24.03 (LTS-SP4)\n%s' "$(uname -m)")"
# 执行当前脚本步骤。
[[ "$identity" == "$expected" ]] || { echo 'FAIL target identity differs from package marker'; exit 1; }
# 执行当前脚本步骤。
echo 'PASS target identity uses the same canonical OS/version representation'
