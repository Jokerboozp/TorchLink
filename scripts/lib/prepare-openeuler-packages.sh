#!/usr/bin/env bash
# Runs only inside the disposable, connected openEuler packaging container.
set -Eeuo pipefail
. /etc/os-release
[[ "$ID" == openeuler && "$VERSION_ID" == 24.03 && "$VERSION" == '24.03 (LTS-SP4)' ]] || { echo 'Unexpected package preparation OS' >&2; exit 1; }
out="/packages"
dnf install -y dnf-plugins-core
# --alldeps also collects dependencies already installed in the build container.
dnf download --resolve --alldeps --destdir="$out" \
  container-selinux policycoreutils-python-utils iptables xz procps-ng curl
compgen -G "$out/container-selinux-*.rpm" >/dev/null
compgen -G "$out/policycoreutils-python-utils-*.rpm" >/dev/null
# Dependency solving against an empty install root catches incomplete closures.
dnf --installroot=/tmp/iot-rpm-check --releasever=24.03 \
  --disablerepo='*' --setopt=tsflags=test --setopt=install_weak_deps=False \
  install -y "$out"/*.rpm
printf '%s\n%s\n%s\n%s\n' "$ID" "$VERSION_ID" "$VERSION" "$(uname -m)" > "$out/target-os"
for file in "$out"/*.rpm "$out/target-os"; do
  sha256sum "$file" | awk '{print $1}' > "$file.sha256"
done
printf 'Prepared openEuler SELinux and system dependencies.\n'
