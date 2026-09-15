#!/usr/bin/env bash
# Runs only inside the disposable, connected openEuler packaging container.
set -Eeuo pipefail
. /etc/os-release
# os-release IDs vary in casing; the SP display name also uses space/hyphen forms.
ID="$(printf '%s' "${ID:-}" | tr '[:upper:]' '[:lower:]')"
if [[ "$ID" == openeuler && "${VERSION_ID:-}" == 24.03 ]]; then
  case "${VERSION:-}" in '24.03 (LTS SP4)'|'24.03 (LTS-SP4)') VERSION='24.03 (LTS-SP4)';; esac
fi
[[ "$ID" == openeuler && "${VERSION_ID:-}" == 24.03 && "${VERSION:-}" == '24.03 (LTS-SP4)' ]] || {
  printf '依赖准备系统不匹配：ID=%s VERSION_ID=%s VERSION=%s；需要 openEuler 24.03 LTS-SP4。\n' "$ID" "${VERSION_ID:-未提供}" "${VERSION:-未提供}" >&2
  exit 1
}
out="${2:-/packages}"
roots=(container-selinux policycoreutils-python-utils iptables xz procps-ng curl)
mode="${1:-download}"
case "$mode" in download|--index-only) ;; *) echo "未知准备模式：$mode" >&2; exit 1;; esac
if [ "$mode" = --index-only ]; then
  # Verify the old bundle before reindexing; never bless corrupted RPMs.
  expected="$(printf '%s\n%s\n%s\n%s\n' "$ID" "$VERSION_ID" "$VERSION" "$(uname -m)")"
  [ "$(cat "$out/target-os")" = "$expected" ] || { echo '旧包目标 OS/版本/架构与修复容器不匹配。' >&2; exit 1; }
  for file in "$out"/*.rpm "$out/target-os"; do
    [ -s "$file" ] && [ -f "$file.sha256" ]
    [ "$(sha256sum "$file" | awk '{print $1}')" = "$(tr -d '\r\n' < "$file.sha256")" ]
  done
fi
dnf install -y dnf-plugins-core createrepo_c
# --alldeps also collects dependencies already installed in the build container.
if [ "$mode" = download ]; then
  dnf download --resolve --alldeps --setopt=install_weak_deps=False --destdir="$out" "${roots[@]}"
fi
compgen -G "$out/container-selinux-*.rpm" >/dev/null
compgen -G "$out/policycoreutils-python-utils-*.rpm" >/dev/null
cp /etc/pki/rpm-gpg/RPM-GPG-KEY-openEuler "$out/"
createrepo_c "$out"
# Test only named roots against the local repository. Installing every candidate
# can pull in mutually exclusive boot packages even when the closure is complete.
dnf --installroot=/tmp/iot-rpm-check --releasever=24.03 \
  --disablerepo='*' --setopt=tsflags=test --setopt=install_weak_deps=False \
  --repofrompath="iot-offline,file://$out" --enablerepo=iot-offline \
  --setopt=iot-offline.gpgcheck=1 --setopt="iot-offline.gpgkey=file://$out/RPM-GPG-KEY-openEuler" \
  install -y "${roots[@]}"
printf '%s\n%s\n%s\n%s\n' "$ID" "$VERSION_ID" "$VERSION" "$(uname -m)" > "$out/target-os"
for file in "$out"/*.rpm "$out/target-os" "$out/RPM-GPG-KEY-openEuler" "$out"/repodata/*; do
  case "$file" in *.sha256) continue;; esac
  sha256sum "$file" | awk '{print $1}' > "$file.sha256"
done
printf 'Prepared openEuler SELinux and system dependencies.\n'
