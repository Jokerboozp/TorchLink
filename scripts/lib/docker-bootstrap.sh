#!/usr/bin/env bash
# Linux runtime bootstrap. Offline mode never downloads anything.
# 执行当前脚本步骤。
docker_runtime_arch() {
  # 执行当前脚本步骤。
  case "${1:-$(uname -m)}" in
    # 执行当前脚本步骤。
    x86_64|amd64) printf x86_64;;
    # 执行当前脚本步骤。
    aarch64|arm64) printf aarch64;;
    # 执行当前脚本步骤。
    *) echo 'Docker 自动安装仅支持 Linux amd64 / arm64。' >&2; return 1;;
  # 执行当前脚本步骤。
  esac
# 结束当前控制块。
}

# 执行当前脚本步骤。
docker_compose_ready() {
  # 执行当前脚本步骤。
  local value major minor patch
  # 执行当前脚本步骤。
  value="$(docker compose version --short 2>/dev/null)" || return 1
  # 执行当前脚本步骤。
  value="${value#v}"
  # 执行当前脚本步骤。
  IFS=. read -r major minor patch <<< "$value"
  # 执行当前脚本步骤。
  [[ "$major" =~ ^[0-9]+$ && "$minor" =~ ^[0-9]+$ ]] || return 1
  # 执行当前脚本步骤。
  patch="${patch%%-*}"
  # 执行当前脚本步骤。
  [[ "$patch" =~ ^[0-9]+$ ]] || return 1
  # 执行当前脚本步骤。
  [ "$major" -gt 2 ] || { [ "$major" -eq 2 ] && { [ "$minor" -gt 24 ] || { [ "$minor" -eq 24 ] && [ "$patch" -ge 4 ]; }; }; }
# 结束当前控制块。
}

# 执行当前脚本步骤。
docker_runtime_hash() {
  # 判断条件后执行对应操作。
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
  # 执行当前脚本步骤。
  else shasum -a 256 "$1" | awk '{print $1}'; fi
# 结束当前控制块。
}

# 执行当前脚本步骤。
docker_runtime_download() {
  # 执行当前脚本步骤。
  local url="$1" path="$2"
  # 判断条件后执行对应操作。
  if command -v curl >/dev/null 2>&1; then
    # 执行当前脚本步骤。
    curl --fail --location --retry 3 --connect-timeout 20 --output "$path" "$url"
  # 执行当前脚本步骤。
  elif command -v wget >/dev/null 2>&1; then
    # 执行当前脚本步骤。
    wget -O "$path" "$url"
  # 执行当前脚本步骤。
  elif command -v apt-get >/dev/null 2>&1; then
    # Minimal Ubuntu images may have neither curl nor wget.
    # 执行当前脚本步骤。
    docker_runtime_root apt-get update || return 1
    # 执行当前脚本步骤。
    docker_runtime_root env DEBIAN_FRONTEND=noninteractive apt-get install -y curl ca-certificates || return 1
    # 执行当前脚本步骤。
    curl --fail --location --retry 3 --connect-timeout 20 --output "$path" "$url"
  # 执行当前脚本步骤。
  else
    # 执行当前脚本步骤。
    echo '下载 Docker 需要 curl 或 wget。' >&2; return 1
  # 结束当前控制块。
  fi
  # 执行当前脚本步骤。
  [ -s "$path" ] || { echo "下载文件为空：$path" >&2; return 1; }
# 结束当前控制块。
}

# Called on the connected packaging machine; also used by online bootstrap.
# 执行当前脚本步骤。
prepare_docker_runtime() {
  # 执行当前脚本步骤。
  local directory="$1" arch version name build_arch
  # 执行当前脚本步骤。
  arch="$(docker_runtime_arch "$2")" || return 1
  # 执行当前脚本步骤。
  mkdir -p "$directory"
  # 执行当前脚本步骤。
  printf '%s\n' "$arch" > "$directory/architecture"
  # The compatibility runtime is for CentOS 7 / older kernels only.
  # 遍历数据并执行循环体。
  for version in 24.0.9 28.5.2; do
    # 执行当前脚本步骤。
    name="docker-$version.tgz"
    # 执行当前脚本步骤。
    docker_runtime_download "https://download.docker.com/linux/static/stable/$arch/$name" "$directory/$name" || return 1
    # 执行当前脚本步骤。
    docker_runtime_hash "$directory/$name" > "$directory/$name.sha256"
  # 结束当前控制块。
  done
  # 执行当前脚本步骤。
  name=docker-compose
  # 执行当前脚本步骤。
  docker_runtime_download "https://github.com/docker/compose/releases/download/v2.27.3/docker-compose-linux-$arch" "$directory/$name" || return 1
  # 执行当前脚本步骤。
  docker_runtime_hash "$directory/$name" > "$directory/$name.sha256"
  # 执行当前脚本步骤。
  build_arch=amd64; [ "$arch" != aarch64 ] || build_arch=arm64
  # 执行当前脚本步骤。
  name=docker-buildx
  # 执行当前脚本步骤。
  docker_runtime_download "https://github.com/docker/buildx/releases/download/v0.14.1/buildx-v0.14.1.linux-$build_arch" "$directory/$name" || return 1
  # 执行当前脚本步骤。
  docker_runtime_hash "$directory/$name" > "$directory/$name.sha256"
# 结束当前控制块。
}

# 执行当前脚本步骤。
verify_docker_runtime_file() {
  # 执行当前脚本步骤。
  local file="$1" expected actual
  # 执行当前脚本步骤。
  [ -s "$file" ] && [ -f "$file.sha256" ] || { echo "离线包缺少 Docker 安装文件或校验值：${file}。请重新打包。" >&2; return 1; }
  # 执行当前脚本步骤。
  expected="$(awk 'NR==1 {print tolower($1)}' "$file.sha256" | tr -d '\r')"
  # 执行当前脚本步骤。
  actual="$(docker_runtime_hash "$file")"
  # 执行当前脚本步骤。
  [ "$expected" = "$actual" ] || { echo "Docker 安装文件 SHA256 校验失败：$file" >&2; return 1; }
# 结束当前控制块。
}

# 执行当前脚本步骤。
docker_runtime_root() {
  # 判断条件后执行对应操作。
  if [ "$(id -u)" -eq 0 ]; then "$@"
  # 执行当前脚本步骤。
  elif command -v sudo >/dev/null 2>&1; then sudo "$@"
  # 执行当前脚本步骤。
  else echo '安装或启动 Docker 需要 root 权限，请以 root 重新执行部署脚本。' >&2; return 1; fi
# 结束当前控制块。
}

# The private runtime is the only installation this project may relabel/restart.
# 执行当前脚本步骤。
docker_runtime_is_managed_local() {
  # 执行当前脚本步骤。
  local endpoint
  # 执行当前脚本步骤。
  command -v systemctl >/dev/null 2>&1 || return 1
  # 判断条件后执行对应操作。
  if [ -n "${DOCKER_CONTEXT:-}" ]; then
    # 执行当前脚本步骤。
    endpoint="$(docker context inspect "$DOCKER_CONTEXT" --format '{{.Endpoints.docker.Host}}' 2>/dev/null)"
  # 执行当前脚本步骤。
  else
    # 执行当前脚本步骤。
    endpoint="${DOCKER_HOST:-$(docker context inspect --format '{{.Endpoints.docker.Host}}' 2>/dev/null)}"
  # 结束当前控制块。
  fi
  # 执行当前脚本步骤。
  case "$endpoint" in unix:///var/run/docker.sock|unix:///run/docker.sock) ;; *) return 1;; esac
  # 执行当前脚本步骤。
  systemctl show docker -p ExecStart 2>/dev/null | grep -q '/usr/local/lib/iot-docker/dockerd'
# 结束当前控制块。
}

# 执行当前脚本步骤。
docker_runtime_host_identity() {
  # 执行当前脚本步骤。
  (
    # 执行当前脚本步骤。
    . /etc/os-release
    # 执行当前脚本步骤。
    ID="$(printf '%s' "${ID:-}" | tr '[:upper:]' '[:lower:]')"
    # 判断条件后执行对应操作。
    if [ "$ID" = openeuler ] && [ "${VERSION_ID:-}" = 24.03 ]; then
      # 执行当前脚本步骤。
      case "${VERSION:-}" in '24.03 (LTS SP4)'|'24.03 (LTS-SP4)') VERSION='24.03 (LTS-SP4)';; esac
    # 结束当前控制块。
    fi
    # 执行当前脚本步骤。
    printf '%s\n%s\n%s\n%s\n' "$ID" "${VERSION_ID:-}" "${VERSION:-}" "$(uname -m)"
  # 执行当前脚本步骤。
  )
# 结束当前控制块。
}

# 执行当前脚本步骤。
docker_runtime_install_packages() {
  # 执行当前脚本步骤。
  local directory="$1" package actual expected repo_dir manager status=0 baseurl keys=''
  # 执行当前脚本步骤。
  shift
  # 执行当前脚本步骤。
  [ "$#" -gt 0 ] || { echo '必须明确指定要安装的系统包。' >&2; return 1; }
  # Prepared OS-specific sets cannot be used on a different release/architecture.
  # 判断条件后执行对应操作。
  if [ -f "$directory/packages/target-os" ]; then
    # 执行当前脚本步骤。
    verify_docker_runtime_file "$directory/packages/target-os" || return 1
    # 执行当前脚本步骤。
    actual="$(docker_runtime_host_identity)" || return 1
    # 执行当前脚本步骤。
    expected="$(cat "$directory/packages/target-os")"
    # 执行当前脚本步骤。
    [ "$actual" = "$expected" ] || { echo '离线系统依赖与目标 OS/发行版/架构不匹配，请重新准备匹配的依赖包。' >&2; return 1; }
  # 结束当前控制块。
  fi
  # 执行当前脚本步骤。
  compgen -G "$directory/packages/*.rpm" >/dev/null || { echo '离线包缺少 SELinux/系统依赖 RPM；请使用 openEuler 专用打包选项或提供匹配的 DockerPackagesDir。' >&2; return 1; }
  # 遍历数据并执行循环体。
  for package in "$directory"/packages/*.rpm; do verify_docker_runtime_file "$package" || return 1; done
  # 执行当前脚本步骤。
  verify_docker_runtime_file "$directory/packages/repodata/repomd.xml" || {
    # 执行当前脚本步骤。
    echo '旧 RPM 目录缺少本地软件源索引。openEuler 包请在联网打包机运行 scripts/repair-offline-openeuler.sh 修复；不要强制安装全部 RPM。' >&2
    # 返回结果或结束当前脚本。
    return 1
  # 结束当前控制块。
  }
  # 遍历数据并执行循环体。
  for package in "$directory"/packages/repodata/*; do
    # 执行当前脚本步骤。
    case "$package" in *.sha256) continue;; esac
    # 执行当前脚本步骤。
    verify_docker_runtime_file "$package" || return 1
  # 结束当前控制块。
  done
  # 执行当前脚本步骤。
  directory="$(cd "$directory/packages" && pwd)" || return 1
  # Escape path characters that have special meaning in file URLs / repo config.
  # 执行当前脚本步骤。
  baseurl="${directory//%/%25}"; baseurl="${baseurl// /%20}"; baseurl="${baseurl//#/%23}"; baseurl="${baseurl//\$/%24}"
  # 遍历数据并执行循环体。
  for package in "$directory"/RPM-GPG-KEY-*; do
    # 执行当前脚本步骤。
    case "$package" in *.sha256) continue;; esac
    # 执行当前脚本步骤。
    [ -f "$package" ] || continue
    # 执行当前脚本步骤。
    verify_docker_runtime_file "$package" || return 1
    # 执行当前脚本步骤。
    keys+=" file://$baseurl/$(basename "$package")"
  # 结束当前控制块。
  done
  # 执行当前脚本步骤。
  [ -n "$keys" ] || { echo '本地 RPM 软件源缺少 RPM-GPG-KEY-* 签名公钥。' >&2; return 1; }
  # 判断条件后执行对应操作。
  if command -v dnf >/dev/null 2>&1; then manager=dnf
  # 执行当前脚本步骤。
  elif command -v yum >/dev/null 2>&1; then manager=yum
  # 执行当前脚本步骤。
  else echo '离线 RPM 依赖需要 DNF 或 YUM 按包名解析；不支持直接批量 rpm 安装。' >&2; return 1; fi
  # 执行当前脚本步骤。
  repo_dir="$(mktemp -d)" || return 1
  # 执行当前脚本步骤。
  cat > "$repo_dir/iot-offline.repo" <<REPO
[iot-offline]
name=IoT offline system dependencies
baseurl=file://$baseurl
enabled=1
gpgcheck=1
gpgkey=$keys
metadata_expire=0
skip_if_unavailable=0
REPO
  # RPMs are candidates, not installation goals: keep existing boot packages and
  # let the solver select only dependencies required by the named roots.
  docker_runtime_root "$manager" --setopt="reposdir=$repo_dir" --disablerepo='*' \
    --enablerepo=iot-offline --setopt=install_weak_deps=False install -y "$@" || status=$?
  # 执行当前脚本步骤。
  rm -rf -- "$repo_dir"
  # 返回结果或结束当前脚本。
  return "$status"
# 结束当前控制块。
}

# 执行当前脚本步骤。
docker_runtime_selinux_ready() {
  # 执行当前脚本步骤。
  command -v semanage >/dev/null 2>&1 && command -v restorecon >/dev/null 2>&1 &&
    # 执行当前脚本步骤。
    command -v matchpathcon >/dev/null 2>&1 &&
    # 执行当前脚本步骤。
    matchpathcon -n /usr/bin/dockerd 2>/dev/null | grep -q ':container_runtime_exec_t:'
# 结束当前控制块。
}

# 执行当前脚本步骤。
docker_runtime_selinux() {
  # 执行当前脚本步骤。
  local mode="$1" directory="$2" phase="${3:-installed}" state pid context path active=0 repair=0
  # 执行当前脚本步骤。
  command -v getenforce >/dev/null 2>&1 || return 0
  # 执行当前脚本步骤。
  state="$(getenforce)"
  # 执行当前脚本步骤。
  case "$state" in Enforcing|Permissive) ;; Disabled) return 0;; *) echo '无法读取 SELinux 状态。' >&2; return 1;; esac
  # 判断条件后执行对应操作。
  if ! docker_runtime_selinux_ready; then
    # 判断条件后执行对应操作。
    if [ "$mode" = offline ]; then
      # 执行当前脚本步骤。
      docker_runtime_install_packages "$directory" container-selinux policycoreutils-python-utils || return 1
    # 执行当前脚本步骤。
    elif command -v dnf >/dev/null 2>&1; then
      # 执行当前脚本步骤。
      docker_runtime_root dnf install -y container-selinux policycoreutils-python-utils || return 1
    # 执行当前脚本步骤。
    else
      # 执行当前脚本步骤。
      echo 'SELinux 已启用，请先安装本发行版的 container-selinux 与 semanage 工具。' >&2; return 1
    # 结束当前控制块。
    fi
    # 执行当前脚本步骤。
    docker_runtime_selinux_ready || { echo '容器 SELinux 策略或管理工具仍不可用；不会关闭 SELinux 继续部署。' >&2; return 1; }
    # 执行当前脚本步骤。
    repair=1
  # 结束当前控制块。
  fi
  # 执行当前脚本步骤。
  [ "$phase" != prerequisites ] || return 0
  # 判断条件后执行对应操作。
  if ! matchpathcon -n /usr/local/lib/iot-docker/dockerd | grep -q ':container_runtime_exec_t:'; then
    # 执行当前脚本步骤。
    docker_runtime_root semanage fcontext -a -e /usr/bin /usr/local/lib/iot-docker || return 1
    # 执行当前脚本步骤。
    repair=1
  # 结束当前控制块。
  fi
  # 遍历数据并执行循环体。
  for path in /usr/local/lib/iot-docker/dockerd /usr/local/lib/iot-docker/containerd /usr/local/lib/iot-docker/runc /var/lib/docker; do
    # 执行当前脚本步骤。
    [ ! -e "$path" ] || matchpathcon -V "$path" >/dev/null 2>&1 || repair=1
  # 结束当前控制块。
  done
  # 判断条件后执行对应操作。
  if systemctl is-active --quiet docker; then
    # 执行当前脚本步骤。
    active=1
    # 执行当前脚本步骤。
    pid="$(systemctl show docker -p MainPID | sed 's/^MainPID=//')"
    # 执行当前脚本步骤。
    context="$(ps -ww -p "$pid" -o label=)"
    # 执行当前脚本步骤。
    [[ "$context" == *:container_runtime_t:* ]] || repair=1
    # A customized data root is not implicitly relabeled by this installer.
    # 执行当前脚本步骤。
    path="$(docker info --format '{{.DockerRootDir}}')"
    # 执行当前脚本步骤。
    [ "$path" = /var/lib/docker ] || { echo '受管 Docker 使用了自定义数据目录，请先配置该目录的 SELinux 策略。' >&2; return 1; }
  # 结束当前控制块。
  fi
  # 判断条件后执行对应操作。
  if [ "$repair" = 1 ]; then
    # 执行当前脚本步骤。
    echo '正在配置受管 Docker 的 SELinux 标签；已运行的受管 Docker 将重启。'
    # 执行当前脚本步骤。
    [ "$active" = 0 ] || docker_runtime_root systemctl stop docker || return 1
    # 遍历数据并执行循环体。
    for path in /usr/local/lib/iot-docker /var/lib/docker /run/docker /run/docker.sock /etc/docker; do
      # 执行当前脚本步骤。
      [ ! -e "$path" ] || docker_runtime_root restorecon -R "$path" || return 1
    # 结束当前控制块。
    done
    # 执行当前脚本步骤。
    [ "$active" = 0 ] || docker_runtime_root systemctl start docker || return 1
  # 结束当前控制块。
  fi
  # daemon liveness alone is insufficient: reject the init_t failure mode.
  # 判断条件后执行对应操作。
  if [ "$active" = 1 ]; then
    # 执行当前脚本步骤。
    pid="$(systemctl show docker -p MainPID | sed 's/^MainPID=//')"
    # 执行当前脚本步骤。
    context="$(ps -ww -p "$pid" -o label=)"
    # 执行当前脚本步骤。
    [[ "$context" == *:container_runtime_t:* ]] || { echo 'Docker 仍未进入 container_runtime_t，停止部署；请检查容器策略和服务日志。' >&2; return 1; }
  # 结束当前控制块。
  fi
# 结束当前控制块。
}

# 执行当前脚本步骤。
ensure_deployment_git() {
  # 执行当前脚本步骤。
  command -v git >/dev/null 2>&1 && return 0
  # 判断条件后执行对应操作。
  if command -v apt-get >/dev/null 2>&1; then
    # 执行当前脚本步骤。
    docker_runtime_root apt-get update || return 1
    # 执行当前脚本步骤。
    docker_runtime_root env DEBIAN_FRONTEND=noninteractive apt-get install -y git ca-certificates || return 1
  # 执行当前脚本步骤。
  elif command -v yum >/dev/null 2>&1; then
    # 执行当前脚本步骤。
    docker_runtime_root yum install -y git ca-certificates || return 1
  # 执行当前脚本步骤。
  else
    # 执行当前脚本步骤。
    echo '准备 Harness 源码需要 Git，请安装 Git 后重新运行。' >&2; return 1
  # 结束当前控制块。
  fi
  # 执行当前脚本步骤。
  command -v git >/dev/null 2>&1
# 结束当前控制块。
}

# 执行当前脚本步骤。
docker_runtime_prerequisites() {
  # 执行当前脚本步骤。
  local mode="$1" directory="$2" package repo
  # 判断条件后执行对应操作。
  if command -v iptables >/dev/null 2>&1 && command -v xz >/dev/null 2>&1 && command -v ps >/dev/null 2>&1; then return; fi
  # 判断条件后执行对应操作。
  if [ "$mode" = offline ]; then
    # Optional distro packages must be supplied for stripped-down Linux images.
    # 判断条件后执行对应操作。
    if command -v dpkg >/dev/null 2>&1 && compgen -G "$directory/packages/*.deb" >/dev/null; then
      # 遍历数据并执行循环体。
      for package in "$directory"/packages/*.deb; do verify_docker_runtime_file "$package" || return 1; done
      # 执行当前脚本步骤。
      docker_runtime_root dpkg -i "$directory"/packages/*.deb || return 1
    # 执行当前脚本步骤。
    elif ! command -v dpkg >/dev/null 2>&1 && command -v rpm >/dev/null 2>&1 && compgen -G "$directory/packages/*.rpm" >/dev/null; then
      # 执行当前脚本步骤。
      docker_runtime_install_packages "$directory" iptables xz procps-ng || return 1
    # 执行当前脚本步骤。
    else
      # 执行当前脚本步骤。
      echo '系统缺少 iptables、xz 或 ps。请在打包时通过 --docker-packages-dir / -DockerPackagesDir 加入匹配目标系统的依赖包；离线部署不会访问软件源。' >&2
      # 返回结果或结束当前脚本。
      return 1
    # 结束当前控制块。
    fi
  # 执行当前脚本步骤。
  elif command -v apt-get >/dev/null 2>&1; then
    # 执行当前脚本步骤。
    docker_runtime_root apt-get update || return 1
    # 执行当前脚本步骤。
    docker_runtime_root env DEBIAN_FRONTEND=noninteractive apt-get install -y iptables xz-utils procps curl ca-certificates || return 1
  # 执行当前脚本步骤。
  elif command -v yum >/dev/null 2>&1; then
    # 判断条件后执行对应操作。
    if [ -f /etc/centos-release ] && grep -q 'release 7\.' /etc/centos-release; then
      # 执行当前脚本步骤。
      repo="$(mktemp -d)"
      # 执行当前脚本步骤。
      cat > "$repo/centos7.repo" <<'REPO'
[iot-centos7]
name=CentOS 7.9 archive for Docker prerequisites
baseurl=https://vault.centos.org/7.9.2009/os/$basearch/
enabled=1
gpgcheck=1
gpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-CentOS-7
REPO
      # 执行当前脚本步骤。
      docker_runtime_root yum --setopt="reposdir=$repo" --disablerepo='*' --enablerepo=iot-centos7 install -y iptables xz procps-ng || return 1
    # 执行当前脚本步骤。
    else
      # 执行当前脚本步骤。
      docker_runtime_root yum install -y iptables xz procps-ng || return 1
    # 结束当前控制块。
    fi
  # 执行当前脚本步骤。
  else
    # 执行当前脚本步骤。
    echo '请先安装系统基础工具 iptables、xz 和 ps。' >&2; return 1
  # 结束当前控制块。
  fi
  # 执行当前脚本步骤。
  command -v iptables >/dev/null && command -v xz >/dev/null && command -v ps >/dev/null
# 结束当前控制块。
}

# 执行当前脚本步骤。
ensure_deployment_docker() {
  # 执行当前脚本步骤。
  local mode="${1:-online}" directory="${2:-}" need_engine=0 need_compose=0 need_buildx=0 arch version stage unit name attempt build_arch
  # Keep working installations completely untouched, including remote contexts.
  # 判断条件后执行对应操作。
  if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1 && docker_compose_ready; then
    # 判断条件后执行对应操作。
    if [ "$mode" = offline ] || docker buildx version >/dev/null 2>&1; then
      # 判断条件后执行对应操作。
      if docker_runtime_is_managed_local; then docker_runtime_selinux "$mode" "$directory" || return 1; fi
      # 返回结果或结束当前脚本。
      return 0
    # 结束当前控制块。
    fi
  # 结束当前控制块。
  fi
  # 执行当前脚本步骤。
  case "${DOCKER_HOST:-}" in
    # 执行当前脚本步骤。
    tcp://*|ssh://*) echo '当前配置的是远程 Docker，请先恢复远程连接或切换到本机；不会在本机安装替代服务。' >&2; return 1;;
  # 执行当前脚本步骤。
  esac
  # 执行当前脚本步骤。
  [ "$(uname -s)" = Linux ] || { echo '当前自动安装适用于 Linux；Windows/macOS 请安装并启动 Docker Desktop 后重试。' >&2; return 1; }
  # 执行当前脚本步骤。
  command -v docker >/dev/null 2>&1 || need_engine=1
  # 判断条件后执行对应操作。
  if [ "$need_engine" -eq 1 ] || ! docker_compose_ready; then need_compose=1; fi
  # 判断条件后执行对应操作。
  if [ "$mode" = online ] && { [ "$need_engine" -eq 1 ] || ! docker buildx version >/dev/null 2>&1; }; then need_buildx=1; fi
  # 执行当前脚本步骤。
  arch="$(docker_runtime_arch)" || return 1
  # 执行当前脚本步骤。
  version=28.5.2
  # Legacy kernel compatibility; never downgrade an existing installation.
  # 执行当前脚本步骤。
  case "$(uname -r)" in 3.*|4.*) version=24.0.9;; esac
  # 判断条件后执行对应操作。
  if [ "$need_engine" -eq 1 ] || [ "$need_compose" -eq 1 ] || [ "$need_buildx" -eq 1 ]; then
    # 判断条件后执行对应操作。
    if [ "$mode" = online ]; then
      # 执行当前脚本步骤。
      directory="$(mktemp -d)"
      # 执行当前脚本步骤。
      printf '%s\n' "$arch" > "$directory/architecture"
      # 判断条件后执行对应操作。
      if [ "$need_engine" -eq 1 ]; then
        # 执行当前脚本步骤。
        docker_runtime_download "https://download.docker.com/linux/static/stable/$arch/docker-$version.tgz" "$directory/docker-$version.tgz" || return 1
        # 执行当前脚本步骤。
        docker_runtime_hash "$directory/docker-$version.tgz" > "$directory/docker-$version.tgz.sha256"
      # 结束当前控制块。
      fi
      # 判断条件后执行对应操作。
      if [ "$need_compose" -eq 1 ]; then
        # 执行当前脚本步骤。
        docker_runtime_download "https://github.com/docker/compose/releases/download/v2.27.3/docker-compose-linux-$arch" "$directory/docker-compose" || return 1
        # 执行当前脚本步骤。
        docker_runtime_hash "$directory/docker-compose" > "$directory/docker-compose.sha256"
      # 结束当前控制块。
      fi
      # 判断条件后执行对应操作。
      if [ "$need_buildx" -eq 1 ]; then
        # 执行当前脚本步骤。
        build_arch=amd64; [ "$arch" != aarch64 ] || build_arch=arm64
        # 执行当前脚本步骤。
        docker_runtime_download "https://github.com/docker/buildx/releases/download/v0.14.1/buildx-v0.14.1.linux-$build_arch" "$directory/docker-buildx" || return 1
        # 执行当前脚本步骤。
        docker_runtime_hash "$directory/docker-buildx" > "$directory/docker-buildx.sha256"
      # 结束当前控制块。
      fi
    # 结束当前控制块。
    fi
    # 执行当前脚本步骤。
    [ -f "$directory/architecture" ] && [ "$(tr -d '\r\n' < "$directory/architecture")" = "$arch" ] || { echo 'Docker 离线安装包架构与目标系统不一致或安装包缺失。' >&2; return 1; }
    # 判断条件后执行对应操作。
    if [ "$need_engine" -eq 1 ]; then verify_docker_runtime_file "$directory/docker-$version.tgz" || return 1; fi
    # 判断条件后执行对应操作。
    if [ "$need_compose" -eq 1 ]; then verify_docker_runtime_file "$directory/docker-compose" || return 1; fi
    # 判断条件后执行对应操作。
    if [ "$need_buildx" -eq 1 ]; then verify_docker_runtime_file "$directory/docker-buildx" || return 1; fi
  # 结束当前控制块。
  fi
  # 判断条件后执行对应操作。
  if [ "$need_engine" -eq 1 ]; then
    # 执行当前脚本步骤。
    command -v systemctl >/dev/null 2>&1 || { echo '自动安装 Docker 需要 systemd。' >&2; return 1; }
    # Avoid replacing a daemon when only its CLI is missing from PATH.
    # 判断条件后执行对应操作。
    if command -v dockerd >/dev/null 2>&1 || [ -f /etc/systemd/system/docker.service ] || [ -f /usr/lib/systemd/system/docker.service ]; then
      # 执行当前脚本步骤。
      echo '检测到已有 Docker 服务但找不到 CLI，请修复 PATH/已有安装后重试；不会覆盖它。' >&2; return 1
    # 结束当前控制块。
    fi
    # 执行当前脚本步骤。
    docker_runtime_prerequisites "$mode" "$directory" || return 1
    # 执行当前脚本步骤。
    docker_runtime_selinux "$mode" "$directory" prerequisites || return 1
    # 执行当前脚本步骤。
    stage="$(mktemp -d)"
    # Only regular files under docker/ may be installed; never extract links
    # or arbitrary paths from a transported archive.
    # 执行当前脚本步骤。
    tar -tzf "$directory/docker-$version.tgz" > "$stage/entries" || return 1
    # 遍历数据并执行循环体。
    while IFS= read -r name; do
      # 执行当前脚本步骤。
      case "$name" in docker/|docker/docker|docker/dockerd|docker/containerd|docker/containerd-shim|docker/containerd-shim-runc-v2|docker/ctr|docker/runc|docker/docker-init|docker/docker-proxy) ;;
        # 执行当前脚本步骤。
        *) echo "Docker 归档包含非预期路径：$name" >&2; return 1;;
      # 执行当前脚本步骤。
      esac
    # 结束当前控制块。
    done < "$stage/entries"
    # 判断条件后执行对应操作。
    if tar -tvzf "$directory/docker-$version.tgz" | awk 'substr($0,1,1)!="-" && substr($0,1,1)!="d" {bad=1} END {exit !bad}'; then
      # 执行当前脚本步骤。
      echo 'Docker 归档不能包含链接或特殊文件。' >&2; return 1
    # 结束当前控制块。
    fi
    # 执行当前脚本步骤。
    tar -xzf "$directory/docker-$version.tgz" -C "$stage" || return 1
    # 遍历数据并执行循环体。
    for name in docker dockerd containerd containerd-shim-runc-v2 ctr runc docker-init docker-proxy; do
      # 执行当前脚本步骤。
      [ -f "$stage/docker/$name" ] || { echo "Docker 归档缺少 $name" >&2; return 1; }
    # 结束当前控制块。
    done
    # 执行当前脚本步骤。
    docker_runtime_root install -d /usr/local/lib/iot-docker /usr/local/bin || return 1
    # Private daemon tools avoid overwriting a separately installed containerd.
    # 执行当前脚本步骤。
    docker_runtime_root install -m 0755 "$stage"/docker/* /usr/local/lib/iot-docker/ || return 1
    # 执行当前脚本步骤。
    docker_runtime_root ln -s /usr/local/lib/iot-docker/docker /usr/local/bin/docker || return 1
    # 执行当前脚本步骤。
    unit="$stage/docker.service"
    # 执行当前脚本步骤。
    cat > "$unit" <<'UNIT'
[Unit]
Description=Docker Engine (IoT deployment)
After=network-online.target
Wants=network-online.target
[Service]
Type=notify
Environment=PATH=/usr/local/lib/iot-docker:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
ExecStart=/usr/local/lib/iot-docker/dockerd
Restart=always
RestartSec=5
Delegate=yes
KillMode=process
LimitNOFILE=infinity
LimitNPROC=infinity
LimitCORE=infinity
TasksMax=infinity
[Install]
WantedBy=multi-user.target
UNIT
    # 执行当前脚本步骤。
    docker_runtime_root install -m 0644 "$unit" /etc/systemd/system/docker.service || return 1
    # 执行当前脚本步骤。
    docker_runtime_root systemctl daemon-reload || return 1
    # 执行当前脚本步骤。
    export PATH="/usr/local/bin:$PATH"
    # 执行当前脚本步骤。
    hash -r
    # 执行当前脚本步骤。
    echo "Docker $version 已安装。"
  # 结束当前控制块。
  fi
  # 判断条件后执行对应操作。
  if [ "$need_compose" -eq 1 ]; then
    # 执行当前脚本步骤。
    docker_runtime_root install -d /usr/local/lib/docker/cli-plugins || return 1
    # 执行当前脚本步骤。
    docker_runtime_root install -m 0755 "$directory/docker-compose" /usr/local/lib/docker/cli-plugins/docker-compose || return 1
  # 结束当前控制块。
  fi
  # 判断条件后执行对应操作。
  if [ "$need_buildx" -eq 1 ]; then
    # 执行当前脚本步骤。
    docker_runtime_root install -d /usr/local/lib/docker/cli-plugins || return 1
    # 执行当前脚本步骤。
    docker_runtime_root install -m 0755 "$directory/docker-buildx" /usr/local/lib/docker/cli-plugins/docker-buildx || return 1
  # 结束当前控制块。
  fi
  # 判断条件后执行对应操作。
  if ! docker info >/dev/null 2>&1; then
    # 判断条件后执行对应操作。
    if [ "$need_engine" = 1 ] || docker_runtime_is_managed_local; then
      # 执行当前脚本步骤。
      docker_runtime_selinux "$mode" "$directory" || return 1
    # 结束当前控制块。
    fi
    # 执行当前脚本步骤。
    docker_runtime_root systemctl enable --now docker || return 1
    # 遍历数据并执行循环体。
    for attempt in $(seq 1 30); do docker info >/dev/null 2>&1 && break; sleep 2; done
  # 结束当前控制块。
  fi
  # 执行当前脚本步骤。
  docker info >/dev/null 2>&1 || { echo 'Docker 仍不可用；请检查 journalctl -u docker，以及当前账号的 Docker socket 权限。可用 sudo 重新执行部署脚本。' >&2; return 1; }
  # 判断条件后执行对应操作。
  if [ "$need_engine" = 1 ] || docker_runtime_is_managed_local; then
    # 执行当前脚本步骤。
    docker_runtime_selinux "$mode" "$directory" || return 1
  # 结束当前控制块。
  fi
  # 执行当前脚本步骤。
  docker_compose_ready || { echo '需要 Docker Compose 2.24.4+，请检查已有用户级插件是否覆盖了系统插件。' >&2; return 1; }
  # 判断条件后执行对应操作。
  if [ "$mode" = online ]; then docker buildx version >/dev/null 2>&1 || { echo 'Docker Buildx 插件不可用。' >&2; return 1; }; fi
# 结束当前控制块。
}

# Packaging is performed on the connected host, never on the offline target.
# 执行当前脚本步骤。
prepare_openeuler_packages() {
  # 执行当前脚本步骤。
  local directory="$1" script="$2" arch="$3" mode="${4:-download}" platform
  # 执行当前脚本步骤。
  platform=amd64; case "$arch" in arm64|aarch64) platform=arm64;; amd64|x86_64) ;; *) return 1;; esac
  # 执行当前脚本步骤。
  mkdir -p "$directory"
  # 执行当前脚本步骤。
  directory="$(cd "$directory" && pwd)"
  docker run --rm --platform "linux/$platform" \
    --mount "type=bind,source=$directory,target=/packages" \
    --mount "type=bind,source=$script,target=/prepare.sh,readonly" \
    openeuler/openeuler:24.03-lts-sp4 bash /prepare.sh "$mode" || return 1
  # 执行当前脚本步骤。
  verify_docker_runtime_file "$directory/target-os" || return 1
  # 执行当前脚本步骤。
  verify_docker_runtime_file "$directory/repodata/repomd.xml" || return 1
  # 执行当前脚本步骤。
  verify_docker_runtime_file "$directory/RPM-GPG-KEY-openEuler" || return 1
  # 执行当前脚本步骤。
  compgen -G "$directory/container-selinux-*.rpm" >/dev/null || return 1
# 结束当前控制块。
}
