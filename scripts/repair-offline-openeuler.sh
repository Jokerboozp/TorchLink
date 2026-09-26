#!/usr/bin/env bash
# Run on the connected Linux build machine, never on the offline deployment host.
set -Eeuo pipefail
script_dir="$(cd "$(dirname "$0")" && pwd)"
source "$script_dir/lib/docker-bootstrap.sh"
if [ "$#" -ne 1 ] || [ "$1" = --help ] || [ "$1" = -h ]; then
  echo '用法：bash scripts/repair-offline-openeuler.sh 已解压的旧离线包目录'
  echo '在联网 Linux/CentOS 打包机执行。生成仅含 RPM 索引、公钥和部署引导脚本的补丁，不重新构建镜像或模型。'
  if [ "$#" -eq 1 ] && { [ "$1" = --help ] || [ "$1" = -h ]; }; then exit 0; fi
  exit 1
fi
bundle="$(cd "$1" && pwd)"
packages="$bundle/docker-runtime/packages"
verify_docker_runtime_file "$packages/target-os"
arch="$(docker_runtime_arch "$(sed -n '4p' "$packages/target-os")")"
command -v docker >/dev/null || { echo '联网打包机需要可用的 Docker。' >&2; exit 1; }
work="$(mktemp -d)"
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$work/docker-runtime/packages" "$work/scripts/lib"
# Work on a copy so preparation failures leave the original bundle untouched.
for file in "$packages"/*.rpm "$packages/target-os"; do
  verify_docker_runtime_file "$file"
  cp "$file" "$file.sha256" "$work/docker-runtime/packages/"
done
prepare_openeuler_packages "$work/docker-runtime/packages" "$script_dir/lib/prepare-openeuler-packages.sh" "$arch" --index-only
cp "$script_dir/lib/docker-bootstrap.sh" "$work/scripts/lib/"
patch="${bundle}-rpm-repair.tar.gz"
tar -czf "$patch" -C "$work" scripts/lib/docker-bootstrap.sh \
  docker-runtime/packages/repodata docker-runtime/packages/RPM-GPG-KEY-openEuler \
  docker-runtime/packages/RPM-GPG-KEY-openEuler.sha256
(
  cd "$(dirname "$patch")"
  sha256sum "$(basename "$patch")" > "$(basename "$patch").sha256"
)
printf '修复补丁：%s\n校验文件：%s.sha256\n' "$patch" "$patch"
echo '将两者传到目标服务器同一目录，sha256sum -c 校验后，将补丁解压到原离线包目录，再执行原部署命令。'
