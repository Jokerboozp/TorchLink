#!/usr/bin/env bash
# Run on the connected Linux build machine, never on the offline deployment host.
# 执行当前脚本步骤。
set -Eeuo pipefail
# 执行当前脚本步骤。
script_dir="$(cd "$(dirname "$0")" && pwd)"
# 执行当前脚本步骤。
source "$script_dir/lib/docker-bootstrap.sh"
# 判断条件后执行对应操作。
if [ "$#" -ne 1 ] || [ "$1" = --help ] || [ "$1" = -h ]; then
  # 执行当前脚本步骤。
  echo '用法：bash scripts/repair-offline-openeuler.sh 已解压的旧离线包目录'
  # 执行当前脚本步骤。
  echo '在联网 Linux/CentOS 打包机执行。生成仅含 RPM 索引、公钥和部署引导脚本的补丁，不重新构建镜像或模型。'
  # 判断条件后执行对应操作。
  if [ "$#" -eq 1 ] && { [ "$1" = --help ] || [ "$1" = -h ]; }; then exit 0; fi
  # 返回结果或结束当前脚本。
  exit 1
# 结束当前控制块。
fi
# 执行当前脚本步骤。
bundle="$(cd "$1" && pwd)"
# 执行当前脚本步骤。
packages="$bundle/docker-runtime/packages"
# 执行当前脚本步骤。
verify_docker_runtime_file "$packages/target-os"
# 执行当前脚本步骤。
arch="$(docker_runtime_arch "$(sed -n '4p' "$packages/target-os")")"
# 执行当前脚本步骤。
command -v docker >/dev/null || { echo '联网打包机需要可用的 Docker。' >&2; exit 1; }
# 执行当前脚本步骤。
work="$(mktemp -d)"
# 执行当前脚本步骤。
trap 'rm -rf -- "$work"' EXIT
# 执行当前脚本步骤。
mkdir -p "$work/docker-runtime/packages" "$work/scripts/lib"
# Work on a copy so preparation failures leave the original bundle untouched.
# 遍历数据并执行循环体。
for file in "$packages"/*.rpm "$packages/target-os"; do
  # 执行当前脚本步骤。
  verify_docker_runtime_file "$file"
  # 执行当前脚本步骤。
  cp "$file" "$file.sha256" "$work/docker-runtime/packages/"
# 结束当前控制块。
done
# 执行当前脚本步骤。
prepare_openeuler_packages "$work/docker-runtime/packages" "$script_dir/lib/prepare-openeuler-packages.sh" "$arch" --index-only
# 执行当前脚本步骤。
cp "$script_dir/lib/docker-bootstrap.sh" "$work/scripts/lib/"
# 执行当前脚本步骤。
patch="${bundle}-rpm-repair.tar.gz"
tar -czf "$patch" -C "$work" scripts/lib/docker-bootstrap.sh \
  docker-runtime/packages/repodata docker-runtime/packages/RPM-GPG-KEY-openEuler \
  docker-runtime/packages/RPM-GPG-KEY-openEuler.sha256
# 执行当前脚本步骤。
(
  # 执行当前脚本步骤。
  cd "$(dirname "$patch")"
  # 执行当前脚本步骤。
  sha256sum "$(basename "$patch")" > "$(basename "$patch").sha256"
# 执行当前脚本步骤。
)
# 执行当前脚本步骤。
printf '修复补丁：%s\n校验文件：%s.sha256\n' "$patch" "$patch"
# 执行当前脚本步骤。
echo '将两者传到目标服务器同一目录，sha256sum -c 校验后，将补丁解压到原离线包目录，再执行原部署命令。'
