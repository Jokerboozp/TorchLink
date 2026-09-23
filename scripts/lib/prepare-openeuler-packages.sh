#!/usr/bin/env bash
# Runs only inside the disposable, connected openEuler packaging container.
# 执行当前脚本步骤。
set -Eeuo pipefail
# 执行当前脚本步骤。
. /etc/os-release
# os-release IDs vary in casing; the SP display name also uses space/hyphen forms.
# 执行当前脚本步骤。
ID="$(printf '%s' "${ID:-}" | tr '[:upper:]' '[:lower:]')"
# 判断条件后执行对应操作。
if [[ "$ID" == openeuler && "${VERSION_ID:-}" == 24.03 ]]; then
  # 执行当前脚本步骤。
  case "${VERSION:-}" in '24.03 (LTS SP4)'|'24.03 (LTS-SP4)') VERSION='24.03 (LTS-SP4)';; esac
# 结束当前控制块。
fi
# 执行当前脚本步骤。
[[ "$ID" == openeuler && "${VERSION_ID:-}" == 24.03 && "${VERSION:-}" == '24.03 (LTS-SP4)' ]] || {
  # 执行当前脚本步骤。
  printf '依赖准备系统不匹配：ID=%s VERSION_ID=%s VERSION=%s；需要 openEuler 24.03 LTS-SP4。\n' "$ID" "${VERSION_ID:-未提供}" "${VERSION:-未提供}" >&2
  # 返回结果或结束当前脚本。
  exit 1
# 结束当前控制块。
}
# 执行当前脚本步骤。
out="${2:-/packages}"
# 执行当前脚本步骤。
roots=(container-selinux policycoreutils-python-utils iptables xz procps-ng curl)
# 执行当前脚本步骤。
mode="${1:-download}"
# 执行当前脚本步骤。
case "$mode" in download|--index-only) ;; *) echo "未知准备模式：$mode" >&2; exit 1;; esac
# 判断条件后执行对应操作。
if [ "$mode" = --index-only ]; then
  # Verify the old bundle before reindexing; never bless corrupted RPMs.
  # 执行当前脚本步骤。
  expected="$(printf '%s\n%s\n%s\n%s\n' "$ID" "$VERSION_ID" "$VERSION" "$(uname -m)")"
  # 执行当前脚本步骤。
  [ "$(cat "$out/target-os")" = "$expected" ] || { echo '旧包目标 OS/版本/架构与修复容器不匹配。' >&2; exit 1; }
  # 遍历数据并执行循环体。
  for file in "$out"/*.rpm "$out/target-os"; do
    # 执行当前脚本步骤。
    [ -s "$file" ] && [ -f "$file.sha256" ]
    # 执行当前脚本步骤。
    [ "$(sha256sum "$file" | awk '{print $1}')" = "$(tr -d '\r\n' < "$file.sha256")" ]
  # 结束当前控制块。
  done
# 结束当前控制块。
fi
# 执行当前脚本步骤。
dnf install -y dnf-plugins-core createrepo_c
# --alldeps also collects dependencies already installed in the build container.
# 判断条件后执行对应操作。
if [ "$mode" = download ]; then
  # 执行当前脚本步骤。
  dnf download --resolve --alldeps --setopt=install_weak_deps=False --destdir="$out" "${roots[@]}"
# 结束当前控制块。
fi
# 执行当前脚本步骤。
compgen -G "$out/container-selinux-*.rpm" >/dev/null
# 执行当前脚本步骤。
compgen -G "$out/policycoreutils-python-utils-*.rpm" >/dev/null
# 执行当前脚本步骤。
cp /etc/pki/rpm-gpg/RPM-GPG-KEY-openEuler "$out/"
# 执行当前脚本步骤。
createrepo_c "$out"
# Test only named roots against the local repository. Installing every candidate
# can pull in mutually exclusive boot packages even when the closure is complete.
dnf --installroot=/tmp/iot-rpm-check --releasever=24.03 \
  --disablerepo='*' --setopt=tsflags=test --setopt=install_weak_deps=False \
  --repofrompath="iot-offline,file://$out" --enablerepo=iot-offline \
  --setopt=iot-offline.gpgcheck=1 --setopt="iot-offline.gpgkey=file://$out/RPM-GPG-KEY-openEuler" \
  install -y "${roots[@]}"
# 执行当前脚本步骤。
printf '%s\n%s\n%s\n%s\n' "$ID" "$VERSION_ID" "$VERSION" "$(uname -m)" > "$out/target-os"
# 遍历数据并执行循环体。
for file in "$out"/*.rpm "$out/target-os" "$out/RPM-GPG-KEY-openEuler" "$out"/repodata/*; do
  # 执行当前脚本步骤。
  case "$file" in *.sha256) continue;; esac
  # 执行当前脚本步骤。
  sha256sum "$file" | awk '{print $1}' > "$file.sha256"
# 结束当前控制块。
done
# 执行当前脚本步骤。
printf 'Prepared openEuler SELinux and system dependencies.\n'
