#!/usr/bin/env bash
# Linux runtime bootstrap. Offline mode never downloads anything.
docker_runtime_arch() {
  case "${1:-$(uname -m)}" in
    x86_64|amd64) printf x86_64;;
    aarch64|arm64) printf aarch64;;
    *) echo 'Docker 自动安装仅支持 Linux amd64 / arm64。' >&2; return 1;;
  esac
}

docker_compose_ready() {
  local value major minor patch
  value="$(docker compose version --short 2>/dev/null)" || return 1
  value="${value#v}"
  IFS=. read -r major minor patch <<< "$value"
  [[ "$major" =~ ^[0-9]+$ && "$minor" =~ ^[0-9]+$ ]] || return 1
  patch="${patch%%-*}"
  [[ "$patch" =~ ^[0-9]+$ ]] || return 1
  [ "$major" -gt 2 ] || { [ "$major" -eq 2 ] && { [ "$minor" -gt 24 ] || { [ "$minor" -eq 24 ] && [ "$patch" -ge 4 ]; }; }; }
}

docker_runtime_hash() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'
  else shasum -a 256 "$1" | awk '{print $1}'; fi
}

docker_runtime_download() {
  local url="$1" path="$2"
  if command -v curl >/dev/null 2>&1; then
    curl --fail --location --retry 3 --connect-timeout 20 --output "$path" "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -O "$path" "$url"
  elif command -v apt-get >/dev/null 2>&1; then
    # Minimal Ubuntu images may have neither curl nor wget.
    docker_runtime_root apt-get update || return 1
    docker_runtime_root env DEBIAN_FRONTEND=noninteractive apt-get install -y curl ca-certificates || return 1
    curl --fail --location --retry 3 --connect-timeout 20 --output "$path" "$url"
  else
    echo '下载 Docker 需要 curl 或 wget。' >&2; return 1
  fi
  [ -s "$path" ] || { echo "下载文件为空：$path" >&2; return 1; }
}

# Called on the connected packaging machine; also used by online bootstrap.
prepare_docker_runtime() {
  local directory="$1" arch version name build_arch
  arch="$(docker_runtime_arch "$2")" || return 1
  mkdir -p "$directory"
  printf '%s\n' "$arch" > "$directory/architecture"
  # The compatibility runtime is for CentOS 7 / older kernels only.
  for version in 24.0.9 28.5.2; do
    name="docker-$version.tgz"
    docker_runtime_download "https://download.docker.com/linux/static/stable/$arch/$name" "$directory/$name" || return 1
    docker_runtime_hash "$directory/$name" > "$directory/$name.sha256"
  done
  name=docker-compose
  docker_runtime_download "https://github.com/docker/compose/releases/download/v2.27.3/docker-compose-linux-$arch" "$directory/$name" || return 1
  docker_runtime_hash "$directory/$name" > "$directory/$name.sha256"
  build_arch=amd64; [ "$arch" != aarch64 ] || build_arch=arm64
  name=docker-buildx
  docker_runtime_download "https://github.com/docker/buildx/releases/download/v0.14.1/buildx-v0.14.1.linux-$build_arch" "$directory/$name" || return 1
  docker_runtime_hash "$directory/$name" > "$directory/$name.sha256"
}

verify_docker_runtime_file() {
  local file="$1" expected actual
  [ -s "$file" ] && [ -f "$file.sha256" ] || { echo "离线包缺少 Docker 安装文件或校验值：${file}。请重新打包。" >&2; return 1; }
  expected="$(awk 'NR==1 {print tolower($1)}' "$file.sha256" | tr -d '\r')"
  actual="$(docker_runtime_hash "$file")"
  [ "$expected" = "$actual" ] || { echo "Docker 安装文件 SHA256 校验失败：$file" >&2; return 1; }
}

docker_runtime_root() {
  if [ "$(id -u)" -eq 0 ]; then "$@"
  elif command -v sudo >/dev/null 2>&1; then sudo "$@"
  else echo '安装或启动 Docker 需要 root 权限，请以 root 重新执行部署脚本。' >&2; return 1; fi
}

ensure_deployment_git() {
  command -v git >/dev/null 2>&1 && return 0
  if command -v apt-get >/dev/null 2>&1; then
    docker_runtime_root apt-get update || return 1
    docker_runtime_root env DEBIAN_FRONTEND=noninteractive apt-get install -y git ca-certificates || return 1
  elif command -v yum >/dev/null 2>&1; then
    docker_runtime_root yum install -y git ca-certificates || return 1
  else
    echo '准备 Harness 源码需要 Git，请安装 Git 后重新运行。' >&2; return 1
  fi
  command -v git >/dev/null 2>&1
}

docker_runtime_prerequisites() {
  local mode="$1" directory="$2" package repo
  if command -v iptables >/dev/null 2>&1 && command -v xz >/dev/null 2>&1 && command -v ps >/dev/null 2>&1; then return; fi
  if [ "$mode" = offline ]; then
    # Optional distro packages must be supplied for stripped-down Linux images.
    if command -v dpkg >/dev/null 2>&1 && compgen -G "$directory/packages/*.deb" >/dev/null; then
      for package in "$directory"/packages/*.deb; do verify_docker_runtime_file "$package" || return 1; done
      docker_runtime_root dpkg -i "$directory"/packages/*.deb || return 1
    elif ! command -v dpkg >/dev/null 2>&1 && command -v rpm >/dev/null 2>&1 && compgen -G "$directory/packages/*.rpm" >/dev/null; then
      for package in "$directory"/packages/*.rpm; do verify_docker_runtime_file "$package" || return 1; done
      docker_runtime_root rpm -Uvh "$directory"/packages/*.rpm || return 1
    else
      echo '系统缺少 iptables、xz 或 ps。请在打包时通过 --docker-packages-dir / -DockerPackagesDir 加入匹配目标系统的依赖包；离线部署不会访问软件源。' >&2
      return 1
    fi
  elif command -v apt-get >/dev/null 2>&1; then
    docker_runtime_root apt-get update || return 1
    docker_runtime_root env DEBIAN_FRONTEND=noninteractive apt-get install -y iptables xz-utils procps curl ca-certificates || return 1
  elif command -v yum >/dev/null 2>&1; then
    if [ -f /etc/centos-release ] && grep -q 'release 7\.' /etc/centos-release; then
      repo="$(mktemp -d)"
      cat > "$repo/centos7.repo" <<'REPO'
[iot-centos7]
name=CentOS 7.9 archive for Docker prerequisites
baseurl=https://vault.centos.org/7.9.2009/os/$basearch/
enabled=1
gpgcheck=1
gpgkey=file:///etc/pki/rpm-gpg/RPM-GPG-KEY-CentOS-7
REPO
      docker_runtime_root yum --setopt="reposdir=$repo" --disablerepo='*' --enablerepo=iot-centos7 install -y iptables xz procps-ng || return 1
    else
      docker_runtime_root yum install -y iptables xz procps-ng || return 1
    fi
  else
    echo '请先安装系统基础工具 iptables、xz 和 ps。' >&2; return 1
  fi
  command -v iptables >/dev/null && command -v xz >/dev/null && command -v ps >/dev/null
}

ensure_deployment_docker() {
  local mode="${1:-online}" directory="${2:-}" need_engine=0 need_compose=0 need_buildx=0 arch version stage unit name attempt build_arch
  # Keep working installations completely untouched, including remote contexts.
  if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1 && docker_compose_ready; then
    if [ "$mode" = offline ] || docker buildx version >/dev/null 2>&1; then return; fi
  fi
  case "${DOCKER_HOST:-}" in
    tcp://*|ssh://*) echo '当前配置的是远程 Docker，请先恢复远程连接或切换到本机；不会在本机安装替代服务。' >&2; return 1;;
  esac
  [ "$(uname -s)" = Linux ] || { echo '当前自动安装适用于 Linux；Windows/macOS 请安装并启动 Docker Desktop 后重试。' >&2; return 1; }
  command -v docker >/dev/null 2>&1 || need_engine=1
  if [ "$need_engine" -eq 1 ] || ! docker_compose_ready; then need_compose=1; fi
  if [ "$mode" = online ] && { [ "$need_engine" -eq 1 ] || ! docker buildx version >/dev/null 2>&1; }; then need_buildx=1; fi
  arch="$(docker_runtime_arch)" || return 1
  version=28.5.2
  # Legacy kernel compatibility; never downgrade an existing installation.
  case "$(uname -r)" in 3.*|4.*) version=24.0.9;; esac
  if [ "$need_engine" -eq 1 ] || [ "$need_compose" -eq 1 ] || [ "$need_buildx" -eq 1 ]; then
    if [ "$mode" = online ]; then
      directory="$(mktemp -d)"
      printf '%s\n' "$arch" > "$directory/architecture"
      if [ "$need_engine" -eq 1 ]; then
        docker_runtime_download "https://download.docker.com/linux/static/stable/$arch/docker-$version.tgz" "$directory/docker-$version.tgz" || return 1
        docker_runtime_hash "$directory/docker-$version.tgz" > "$directory/docker-$version.tgz.sha256"
      fi
      if [ "$need_compose" -eq 1 ]; then
        docker_runtime_download "https://github.com/docker/compose/releases/download/v2.27.3/docker-compose-linux-$arch" "$directory/docker-compose" || return 1
        docker_runtime_hash "$directory/docker-compose" > "$directory/docker-compose.sha256"
      fi
      if [ "$need_buildx" -eq 1 ]; then
        build_arch=amd64; [ "$arch" != aarch64 ] || build_arch=arm64
        docker_runtime_download "https://github.com/docker/buildx/releases/download/v0.14.1/buildx-v0.14.1.linux-$build_arch" "$directory/docker-buildx" || return 1
        docker_runtime_hash "$directory/docker-buildx" > "$directory/docker-buildx.sha256"
      fi
    fi
    [ -f "$directory/architecture" ] && [ "$(tr -d '\r\n' < "$directory/architecture")" = "$arch" ] || { echo 'Docker 离线安装包架构与目标系统不一致或安装包缺失。' >&2; return 1; }
    if [ "$need_engine" -eq 1 ]; then verify_docker_runtime_file "$directory/docker-$version.tgz" || return 1; fi
    if [ "$need_compose" -eq 1 ]; then verify_docker_runtime_file "$directory/docker-compose" || return 1; fi
    if [ "$need_buildx" -eq 1 ]; then verify_docker_runtime_file "$directory/docker-buildx" || return 1; fi
  fi
  if [ "$need_engine" -eq 1 ]; then
    command -v systemctl >/dev/null 2>&1 || { echo '自动安装 Docker 需要 systemd。' >&2; return 1; }
    # Avoid replacing a daemon when only its CLI is missing from PATH.
    if command -v dockerd >/dev/null 2>&1 || [ -f /etc/systemd/system/docker.service ] || [ -f /usr/lib/systemd/system/docker.service ]; then
      echo '检测到已有 Docker 服务但找不到 CLI，请修复 PATH/已有安装后重试；不会覆盖它。' >&2; return 1
    fi
    docker_runtime_prerequisites "$mode" "$directory" || return 1
    stage="$(mktemp -d)"
    # Only regular files under docker/ may be installed; never extract links
    # or arbitrary paths from a transported archive.
    tar -tzf "$directory/docker-$version.tgz" > "$stage/entries" || return 1
    while IFS= read -r name; do
      case "$name" in docker/|docker/docker|docker/dockerd|docker/containerd|docker/containerd-shim|docker/containerd-shim-runc-v2|docker/ctr|docker/runc|docker/docker-init|docker/docker-proxy) ;;
        *) echo "Docker 归档包含非预期路径：$name" >&2; return 1;;
      esac
    done < "$stage/entries"
    if tar -tvzf "$directory/docker-$version.tgz" | awk 'substr($0,1,1)!="-" && substr($0,1,1)!="d" {bad=1} END {exit !bad}'; then
      echo 'Docker 归档不能包含链接或特殊文件。' >&2; return 1
    fi
    tar -xzf "$directory/docker-$version.tgz" -C "$stage" || return 1
    for name in docker dockerd containerd containerd-shim-runc-v2 ctr runc docker-init docker-proxy; do
      [ -f "$stage/docker/$name" ] || { echo "Docker 归档缺少 $name" >&2; return 1; }
    done
    docker_runtime_root install -d /usr/local/lib/iot-docker /usr/local/bin || return 1
    # Private daemon tools avoid overwriting a separately installed containerd.
    docker_runtime_root install -m 0755 "$stage"/docker/* /usr/local/lib/iot-docker/ || return 1
    docker_runtime_root ln -s /usr/local/lib/iot-docker/docker /usr/local/bin/docker || return 1
    unit="$stage/docker.service"
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
    docker_runtime_root install -m 0644 "$unit" /etc/systemd/system/docker.service || return 1
    docker_runtime_root systemctl daemon-reload || return 1
    export PATH="/usr/local/bin:$PATH"
    hash -r
    echo "Docker $version 已安装。"
  fi
  if [ "$need_compose" -eq 1 ]; then
    docker_runtime_root install -d /usr/local/lib/docker/cli-plugins || return 1
    docker_runtime_root install -m 0755 "$directory/docker-compose" /usr/local/lib/docker/cli-plugins/docker-compose || return 1
  fi
  if [ "$need_buildx" -eq 1 ]; then
    docker_runtime_root install -d /usr/local/lib/docker/cli-plugins || return 1
    docker_runtime_root install -m 0755 "$directory/docker-buildx" /usr/local/lib/docker/cli-plugins/docker-buildx || return 1
  fi
  if ! docker info >/dev/null 2>&1; then
    docker_runtime_root systemctl enable --now docker || return 1
    for attempt in $(seq 1 30); do docker info >/dev/null 2>&1 && break; sleep 2; done
  fi
  docker info >/dev/null 2>&1 || { echo 'Docker 仍不可用；请检查 journalctl -u docker，以及当前账号的 Docker socket 权限。可用 sudo 重新执行部署脚本。' >&2; return 1; }
  docker_compose_ready || { echo '需要 Docker Compose 2.24.4+，请检查已有用户级插件是否覆盖了系统插件。' >&2; return 1; }
  if [ "$mode" = online ]; then docker buildx version >/dev/null 2>&1 || { echo 'Docker Buildx 插件不可用。' >&2; return 1; }; fi
}
