# 部署与本地调试

[本地](#本地运行) · [在线](#在线部署) · [离线](#离线部署) · [摄像头](#摄像头部署) · [维护](#配置与维护) · [拆分 Gateway](#独立接入进程)

命令默认在源码仓库根目录执行；离线安装命令在离线包根目录执行。Go、Node 版本以 `go.mod`、`iot_front/package.json` 为准。源码调试时，API、Vue 和备份服务在本机运行，虚拟机只提供基础环境。

## 本地运行

### 首次准备

Windows PowerShell：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\setup-local.ps1
```

Linux / macOS：

```bash
bash ./scripts/setup-local.sh
```

脚本生成 `.env.local`，设置管理员默认值并随机生成其他服务凭据，启动依赖与 Harness，准备知识库模型，执行 `go mod download` 和 `npm ci`。重复执行复用已有配置与数据，登录信息见下文。

| 需求 | PowerShell 参数 | Bash 参数 |
| --- | --- | --- |
| 自定义 DeepSeek API 模型（默认无需传入） | `-DeepSeekModel deepseek-flash` | `--deepseek-model deepseek-flash` |
| 只准备依赖，不下载源码依赖 | `-SkipCodeDeps` | `--skip-code-deps` |
| Linux 虚拟机部署全部基础环境，源码在本机运行 | 在 Linux 虚拟机执行右侧命令 | `--dependencies-only` |
| 临时运行容器版备份服务 | `-IncludeBackup` | `--include-backup` |
| 启动运维中心依赖（Prometheus、Loki、Grafana、Alertmanager、采集器） | `-IncludeOps` | `--include-ops` |
| 开启 / 关闭摄像头直播媒体服务（默认开启，省略沿用上次选择） | `-Video on` / `-Video off` | `--video on` / `--video off` |

所有部署方式统一使用 DeepSeek API。启动后在“模型管理”填写 API Key 并保存即可，连接测试可选；也可通过各环境文件的 `DEEPSEEK_API_KEY` 配置。未填密钥不阻止平台启动；不再下载 Qwen 对话模型，Ollama 只准备知识库嵌入模型。完整配置、升级与离线联网边界见 [AI 配置](#ai-与工作流)。

依赖容器与源码分开运行时，在 Linux 依赖机执行：

```bash
sudo bash ./scripts/setup-local.sh --dependencies-only \
  --dependency-host <源码机可访问的依赖机地址> \
  --api-host <依赖容器可访问的源码机地址>
```

安全复制生成的 `.env.local` 到源码机仓库根目录，并在源码机执行 `go mod download`、在 `iot_front` 执行 `npm ci`。此模式包含运维组件，备份服务默认与 API、前端一起在源码机调试。依赖端口开放给可信网络；Kafka 公告地址和 Harness 回调须从各自调用端可达。OrbStack 可直接在 Mac 仓库执行 `orb -m develop sudo bash scripts/setup-local.sh --dependencies-only`，共用配置文件。详细网络配置和摄像头开关见 [端口与地址](#端口与地址)。

### 日常运行代码

API、前端和备份服务默认都在源码机运行，在三个独立终端启动；虚拟机仅提供基础环境：

```bash
# 终端一：API
go run ./cmd/iot-platform --env-file .env.local
```

```bash
# 终端二：前端
cd iot_front
npm run dev
```

```bash
# 终端三：备份源码服务
go run ./cmd/backup-service --env-file .env.local
```

访问 **http://localhost:5173**。Vite 默认代理到 API `8081`，可通过 `VITE_API_PROXY_TARGET` 修改；备份服务监听 `8092`。Windows 遇到 npm 执行策略限制时使用 `npm.cmd`。

### IDE 调试

- **GoLand**：工作目录为仓库根目录，运行 `cmd/iot-platform`，程序参数 `--env-file .env.local`。
- **WebStorm**：工作目录为 `iot_front`，运行 npm 的 `dev` 脚本。
- **VS Code**：安装 Go 扩展，使用 [launch.json](../.vscode/launch.json) 中的 `IoT Platform (API + Web)` 或 `IoT Platform (API + Web + Backup)` 组合，按 F5 启动。

进程环境变量优先于环境文件；IDE 中的旧地址和密码可能覆盖 `.env.local`。macOS 调试需要 Delve 和系统“开发者工具访问”授权，停在 `debugserver` 时先检查授权窗口；服务就绪以 `http://localhost:8081/health/ready` 为准。

## 在线部署

在有网目标机的仓库根目录执行：

```bash
# Linux；已使用 root 登录可省略 sudo
sudo bash ./scripts/deploy-online.sh
# macOS；先启动 Docker Desktop
bash ./scripts/deploy-online.sh
```

Windows PowerShell：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\deploy-online.ps1
```

脚本生成 `.env.online`，构建镜像，仅准备知识库嵌入模型，启动并检查服务。首次需要访问镜像、Go/npm 依赖、Harness 源码和模型源；服务器无需预装 Go 或 Node.js。更新源码后重跑同一脚本，沿用原配置、Compose 项目和数据卷。使用自定义旧环境时，先按 [配置与数据归属](#配置与维护) 指定原参数。

## 端口与地址

| 服务 | 本地运行 | 在线 / 离线部署 |
|---|---|---|
| Web | `5173`（Vite） | `8080`，可设 `IOT_WEB_PORT` |
| API | `8081`（本机 Go） | `8081`，可设 `IOT_API_PORT` |
| PostgreSQL / Redis | `15432` / `16379` | 仅容器网络 |
| ClickHouse / Kafka | `18123` / `19092` | 仅容器网络 |
| MinIO 数据 / 控制台 | `19000` / `19002` | 数据仅容器网络，控制台 `9001` |
| MQTT / WebSocket | `1883` / `8083` | `1883` / `8083` |
| Ollama / Weaviate | `11434` / `18080` | 仅容器网络 |
| 备份服务 / Harness | 备份源码进程 `8092` / Harness `8091` | `8092` / `8091`，仅宿主机 |
| Prometheus / Grafana | `19090` / `13000`（`--include-ops`） | Prometheus `9090` 仅宿主机（`PROMETHEUS_BIND_ADDRESS` 可改）/ Grafana `3000`（`GRAFANA_PORT`） |
| Loki / Alertmanager | `13100` / `19093`（`--include-ops`） | 仅容器网络 |
| 摄像头直播媒体服务 | API / HLS `18580`；WebRTC `8000`；GB28181 RTP `30000-30063`（均按本地绑定地址） | API / HLS 仅容器网络（HLS 经 Web 的 `/media/hls/`）；WebRTC `IOT_VIDEO_RTC_PORT`（默认 `8000`）UDP+TCP 对浏览器开放；RTP 端口范围 UDP+TCP 对摄像头网络开放 |
| GB28181 SIP | `5060` UDP+TCP（本机 Go API） | `IOT_GB28181_SIP_PORT`（默认 `5060`）UDP+TCP，对摄像头网络开放 |

本地依赖端口默认只绑定 `127.0.0.1`，供本机代码和模拟设备使用；传入 `--dependency-host` 时才开放到依赖机网络。API 设备上报使用运行 Go 的主机地址。Kafka 通过独立 external listener 返回源码机可访问的地址，容器间仍使用 `redpanda:9092`。

本地 API 默认参数写在 `.env.local`；修改 API 端口时同步修改前端 `VITE_API_PROXY_TARGET`，使用 Harness 时还需同步其 MCP 回调和允许的 Origin。`--env-file` 读取字面的 `KEY=VALUE`，支持注释和单/双引号，不展开 `${变量}` 或执行 shell；已有进程环境变量优先。

依赖容器与源码分处两台机器时，先把仓库克隆或复制到 Linux 依赖机，再在其仓库根目录执行：

```bash
sudo bash scripts/setup-local.sh --dependencies-only \
  --dependency-host <源码机可访问的依赖机地址> \
  --api-host <依赖容器可访问的源码机地址>
```

`--dependencies-only` 自动安装缺失的 Docker Engine、Compose、Buildx，部署 PostgreSQL、Redis、ClickHouse、Redpanda、EMQX、MinIO 主库/备库、Ollama（仅 `nomic-embed-text`）、Weaviate、Harness 及整套运维组件。备份服务不属于基础环境，默认与 API、Vue 一起在源码机调试；虚拟机不安装 Go/npm 源码依赖。首次需要联网下载镜像、构建 Harness 和 MinIO 镜像、下载嵌入模型；失败可原命令重试，重复执行复用凭据与数据，不清理机器。

本地 MinIO 复用 `deploy/minio/Dockerfile` 的官方二进制构建，版本仍为 `RELEASE.2025-09-07T16-13-09Z`，校验固定的 amd64/arm64 SHA-256。原 `quay.io/minio/minio` 已无法公开拉取，首次构建需要访问 GitHub Release。

脚本将依赖端口绑定到 `0.0.0.0`，并配置 Kafka 公告地址、Harness 地址和 API 回调；`IOT_BACKUP_URL` 保持 `http://127.0.0.1:8092`，指向源码机的备份进程，Prometheus 从源码机采集备份指标。安全复制 `.env.local` 到源码机仓库根目录；安装 Go/Node 并准备源码依赖后，在本机启动 Go API、前端和备份服务。普通虚拟机需让源码机能够访问依赖机，且容器能反向访问源码机 `8081` 和备份指标 `8092`；源码机防火墙需允许这些访问。两台机器没有共享文件目录时，运维指标、日志和组件状态可用，依赖本地配置文件的规则/通知编辑保持只读。只有显式追加 `--include-backup` 才启动备份容器；恢复默认命令会停止旧备份容器，保留备份数据。

### OrbStack 虚拟机本地调试

依赖使用 Ubuntu 虚拟机自己的 Docker Engine。Mac 安装 Go 和符合 `iot_front/package.json` 的 Node.js，使用共享的仓库目录编辑、运行和调试源码。以下命令均在 **Mac 仓库根目录**运行，`develop` 替换为 `orb list` 中的虚拟机名称：

```bash
# 仅首次创建；已有 develop 时跳过，不删除原机器
orb create ubuntu:24.04 develop
orb -m develop sudo bash scripts/setup-local.sh --dependencies-only
go mod download
(cd iot_front && npm ci)
```

也可以进入 `develop` 的共享仓库目录，直接运行 `sudo bash scripts/setup-local.sh --dependencies-only`。脚本识别 OrbStack，并自动把 API 回调设为 `host.orb.internal`；新配置使用 `127.0.0.1` 连接依赖，已有配置保留依赖地址，显式 `--dependency-host 127.0.0.1` 可切回本机转发。Mac 直接使用共享目录中的 `.env.local`，运维配置文件也通过共享目录生效。AI 通过 DeepSeek API 配置。第二套环境须指定独立 `--env-file`，并避免同时占用相同转发端口。

运维规则与通知配置默认在共享的 `data/ops`；可在 `.env.local` 设置 `IOT_LOCAL_OPS_DIR=./data/local-develop/ops` 指定单独目录，脚本同步 Compose 挂载和 API 配置路径。重建虚拟机不会删除 Mac 共享目录：需要干净运维配置时使用新的目录，旧规则和通知文件仍保留。

摄像头直播默认随依赖一起部署；不传 `--video` 沿用上次选择，需要关闭或重新开启时：

```bash
orb -m develop sudo bash scripts/setup-local.sh --dependencies-only --video on
orb -m develop sudo bash scripts/setup-local.sh --dependencies-only --video off
```

切换后重启本机 API 加载配置。开启会生成并保留媒体密钥；关闭仅移除媒体容器，保留摄像头资料与密钥。需要转码时加 `--transcode`，普通虚拟机还需 `--rtc-ip <浏览器可访问的虚拟机IP>`；允许的摄像头网段可用 `--allowed-cidrs` 指定。接入真实 GB28181 设备时，设备须能访问源码机的 SIP 端口 5060 与虚拟机的 RTP 端口范围，并在 `.env.local` 设置 `IOT_GB28181_MEDIA_IP=<设备可访问的虚拟机IP>`（OrbStack 的 localhost 转发只对本机可用）。观看权限在平台内分配。

Mac 使用 OrbStack 自动提供的 `localhost` 端口转发，因此上述命令不依赖虚拟机 IP 或 VPN 对内网 IP 的路由。确保 Mac 和其他虚拟机没有占用相同端口；同时测试两套依赖时先停掉其中一套，避免连接到错误的环境。`host.orb.internal` 是 OrbStack 提供的 Mac 回调地址；`host.docker.internal` 在虚拟机内安装的 Docker 中指向虚拟机，不能用于此处的 Mac API 回调。地址机制参见 [OrbStack 网络文档](https://docs.orbstack.dev/machines/network)。

需要其他源码机直接访问虚拟机时，可把 `--dependency-host` 换成 `orb -m develop hostname -I` 返回的 IPv4 或 `<机器名>.orb.local`，并确保 VPN/路由允许直连。该模式会开放依赖端口；虚拟机 IP 改变后重跑完整命令更新地址，凭据和数据保留。

随后在 Mac 启动 API、Vite 和备份源码服务；VS Code 选择 `IoT Platform (API + Web + Backup)`。命令见 [日常运行代码](#日常运行代码)。IDE 与终端均须选择符合 `iot_front/package.json` 的 Node.js。

查看、停止依赖仍在 Mac 仓库根目录执行，停止不会删除卷：

```bash
orb -m develop sudo docker compose --project-name iot-platform-local --env-file .env.local -f compose.local.yaml --profile ops ps
orb -m develop sudo docker compose --project-name iot-platform-local --env-file .env.local -f compose.local.yaml --profile ops stop
```

### ARM64 与 x86_64

Linux 目标支持 `arm64/aarch64` 与 `amd64/x86_64` 两种 64 位架构，不包含 32 位 ARM/x86。Compose 不固定 `platform`，基础镜像自动选择 Docker Engine 的原生架构，应用及 Harness 在目标架构构建。离线包仍须按目标架构分别打包，不能在两种架构间混用。修改镜像版本后，应重新检查镜像清单包含 `linux/arm64` 和 `linux/amd64`，并各自执行部署和实机检查；镜像清单与交叉编译通过不等同于目标系统部署验收。

在 ARM Mac 上通过 OrbStack 模拟 x86 Ubuntu 时，EMQX 的 Erlang JIT 默认双重内存映射可能导致 QUIC 模块报 `nif_library_not_loaded`。仅对此类模拟环境，在对应环境文件加入 `IOT_EMQX_ERL_FLAGS="+JMsingle true"` 后重跑部署命令。该参数保留 QUIC 功能，改用单一可读写执行的内存映射；原生 ARM64 和 x86_64 不需要设置，默认保持 Erlang 的内存保护行为。参数语义见 [Erlang JIT 文档](https://erlang.org/documentation/doc-14/erts-14.0/doc/html/erl.html)。

在线/离线默认提供 HTTP 服务。公网 HTTPS 由现有反向代理终结 TLS 并转发到 Web 端口，脚本不管理域名或证书。

## 离线部署

打包机需联网、Docker 和 Compose 2.24.4+，CPU 架构须与目标一致。Linux 目标机可从包内安装 Docker；Windows/macOS 须先安装 Docker Desktop。目标机无需 Go、Node 或源码。包内包含应用、基础环境、备份、运维组件、Harness 和 `nomic-embed-text`，不含对话模型权重；AI 使用 DeepSeek API，仍需联网。

### 获取或制作离线包

推送 `main` 会触发 `.github/workflows/offline-bundle.yml` 构建 Linux amd64 包，成功后发布到 Releases；下载同一版本的全部分卷、`SHA256SUMS` 与 `DEPLOY.txt`，按说明校验和解压。GitHub 的 Source code 不是部署包。公开包不含密码或 API Key，首次安装在目标机生成配置和随机管理员密码。

手工打包（可带现有私有配置）：

```bash
bash scripts/package-offline.sh
# openEuler 目标追加：--target-os openeuler-24.03-lts-sp4
# 沿用已有配置追加：--env-file /path/to/.env.offline
# 不打包直播媒体服务：--without-video
```

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\package-offline.ps1
# 对应参数：-TargetOS openeuler-24.03-lts-sp4 -EnvFile <路径> -WithoutVideo
```

Linux / macOS 共用 Bash 入口。输出为 `offline-bundles/iot-platform-offline-*`，需整体复制（含隐藏配置）。手工生成的私有包包含凭据，不作为公开下载包分发；实际管理员密码以包内环境文件为准。`--skip-ollama-model` 仅用于目标卷已有嵌入模型，`--skip-docker-runtime` 仅用于目标机已有 Docker。

### 安装与升级

升级前把原 `.env.offline` 复制到新包，保持原项目、数据卷、协议制品和密钥，不能用新随机凭据直接连接旧数据库。在包根目录执行：

```bash
sudo bash scripts/deploy-offline.sh
# macOS 使用同一脚本，省略 sudo
```

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\deploy-offline.ps1
```

脚本校验哈希、导入镜像、补齐嵌入模型、启动并检查服务，使用 `--no-build --pull never`。默认项目为 `iot-platform`，Web 为 `http://服务器IP:8080`。重复部署保留已有配置与数据；模型先恢复 blobs 再恢复 manifests，原子补齐缺失文件，不清空模型卷。部署包不含业务数据库备份。

仅替换 API/Web 镜像时，使用相同标签构建、导出与校验，在原包目录导入后依次重建 API、Web（让 nginx 重新解析地址）。此方式不更新 Compose、Harness 或模型；这些组件变化时交付完整新包，不能再用旧 `images.tar` 覆盖更新。

### Linux 与 openEuler

Linux 自动安装要求 systemd、tar、iptables、xz、ps；健康检查另需 curl。已有 Docker/Compose 可用则复用，不自动更换 daemon 配置、清理数据或升级 Engine。在线补齐缺失工具；离线只从包内 `docker-runtime/` 校验安装。精简发行版须携带匹配系统、版本和架构的 RPM/DEB 及依赖，参数为 `--docker-packages-dir` / `-DockerPackagesDir`。

openEuler 24.03 LTS-SP4 用专用目标参数打包，准备容器内生成本地 RPM 源、索引和公钥，不把 RPM 安装到打包机。目标机保留签名与受保护包检查，按包名解析依赖，修复受管 Docker 的 SELinux 标签；不关闭 SELinux、不删 Docker 数据。专用目标不能同时跳过 Docker runtime 或混用其他发行版依赖。

旧专用包出现 `protected packages: grub2-pc` 时，在联网源码机运行 `bash scripts/repair-offline-openeuler.sh <旧包路径>`，校验输出补丁后解压到对应旧包，再运行部署。补丁只含引导脚本、RPM 索引及公钥；不使用 `--allowerasing` 或关闭引导包保护。缺失、损坏 RPM 需重新打包。

## 摄像头部署

直播媒体服务默认随部署启动，平台业务开关默认开启；只登记摄像头资料不影响使用，只是没有视频。显式关闭会在环境文件写入 `IOT_VIDEO_MODULE=off`，之后不带参数的准备和部署都保持关闭。

本地使用 `setup-local.sh --video on|off`（PowerShell：`-Video on|off`），省略时沿用上次选择、新环境开启。虚拟机依赖模式同时传 `--dependencies-only`；OrbStack 命令见上文。切换后重启本机 API。在线部署同样支持 `--video on|off`（`-Video on|off`）。离线包默认包含媒体镜像（`--without-video` / `-WithoutVideo` 不打包）；目标机不能临时下载缺失的媒体镜像，环境文件为 `IOT_VIDEO_MODULE=off` 时离线部署不启动它。在线/离线也可用独立模块入口：

```bash
bash scripts/video-module.sh enable --mode offline --env-file .env.offline --rtc-ip <浏览器可达IP>
bash scripts/video-module.sh disable --mode offline --env-file .env.offline
```

WebRTC 需要浏览器可达的 `IOT_VIDEO_RTC_EXTERN_IP` 和 RTC UDP/TCP 端口（默认 8000）；跨 NAT 时配置转发，未设置外部地址时使用 HLS。媒体服务自带的 TURN 中继保持关闭（`enableTurn=0`）：它只在浏览器无法直连 WebRTC 端口时中转媒体，而这种情况播放器已自动改用经 Web 代理的 HLS，开启只会多暴露一组端口。

GB28181 需要两类端口对摄像头网络开放：API 的 SIP 端口 `IOT_GB28181_SIP_PORT`（默认 5060，UDP+TCP），以及媒体服务的 RTP 端口范围 `IOT_VIDEO_RTP_PORT_MIN`–`IOT_VIDEO_RTP_PORT_MAX`（默认 30000–30063，UDP+TCP，每路流占两个端口）。`IOT_GB28181_MEDIA_IP` 是设备发送 RTP 的目标地址，默认取 `IOT_VIDEO_RTC_EXTERN_IP` 的第一个地址；`IOT_GB28181_SIP_HOST` 是设备回连平台的地址（在线/离线默认同上，本地源码 API 为空时按路由自动选择）。服务器编号与域用 `IOT_GB28181_SERVER_ID`（默认 `34020000002000000001`）和 `IOT_GB28181_DOMAIN`（默认取编号前 10 位）；`IOT_GB28181_ENABLED=false` 关闭国标接入而不影响 ONVIF/RTSP。本地依赖端口默认只绑定 `127.0.0.1`，真实设备接入需用 `--dependency-host` 开放。API 需能访问媒体 HTTP API，媒体 Hook 需能回调 API；HLS 经 Web 代理，内部媒体 API 不对外开放。需要重编码时显式开启 `--transcode`，资源上限见 `internal/config/video.go`。摄像头网段/端口用 `IOT_VIDEO_ALLOWED_CIDRS`、`IOT_VIDEO_ALLOWED_PORTS` 限制。

媒体、Hook、凭据加密密钥由脚本生成并保留；`IOT_VIDEO_CREDENTIAL_KEY` 不能随意更换，否则已存密码无法解密。停止媒体容器保留摄像头资料和配置；平台内业务开关关闭会撤销播放与拉流。功能、权限与生命周期见 [平台功能](PLATFORM.md#摄像头)。

## 容量测试模块

容量测试模块**默认随部署启用**：运维中心出现“容量测试”页，选择测试类型（快速检查、容量搜索、长稳）并填写设备数、速率和时长即可运行，不需要编写清单、秘密文件或计划。不需要时可以关闭，关闭后页面菜单隐藏，平台其余功能不受影响。在线、离线和集群模块与平台使用同一镜像（`capacity-test serve --self`），只在内部网络监听，不对外发布端口；本地源码模式由 combined API 进程启动本机控制器，不创建容量容器。

| 部署方式 | 关闭 | 重新开启 |
| --- | --- | --- |
| 单机在线 | `bash scripts/deploy-online.sh --capacity off`，或已部署后 `bash scripts/capacity-module.sh disable` | `--capacity on` 或 `capacity-module.sh enable` |
| 本地源码 | `bash scripts/setup-local.sh --capacity off` | `--capacity on`；修改后重启源码 API |
| 单机离线 | `bash scripts/deploy-offline.sh --capacity off`，或 `bash scripts/capacity-module.sh disable --mode offline` | `--capacity on` 或 `capacity-module.sh enable --mode offline` |
| 集群 | 向导中回答不部署，或 `bash scripts/cluster-up.sh --name <名称> --capacity off` | `--capacity on` |

PowerShell 使用 `-Capacity on|off` 与 `scripts\capacity-module.ps1 enable|disable`。选择写入环境文件 `IOT_CAPACITY_MODULE`（集群写入清单 `capacity: {node: ...}`），显式关闭后不带参数的部署保持关闭；离线包打包时即写入开启配置。容器部署开启时自动生成服务令牌 `IOT_OPS_CAPACITY_TOKEN` 并设置 `IOT_OPS_CAPACITY_URL`，关闭时移除服务并隐藏页面，测试结果卷与令牌保留。本地控制器使用进程内生成的令牌和动态本机端口，沿用原有 API 启动命令；旧 `.env.local` 的补充配置见 [本地容量测试](DEVELOPMENT.md#容量测试模块)。

测试以发起人的账号权限运行（平台为其签发与测试时长一致的令牌，权限变更或停用即失效），自动准备标准协议测试产品 `cap-standard`、测试规则 `cap-stress-alarm` 与前缀为 `cap` 的测试设备，测试后保留以便复测。测试会给平台施加真实负载，生产环境请在低峰期运行或只用快速检查。

## 配置与维护

### 环境与数据

| 方案 | 配置文件 | Compose 文件 | 项目名 / 数据卷前缀 |
|---|---|---|---|
| 本地运行 | `.env.local` | `compose.local.yaml` | `iot-platform-local` |
| 在线部署 | `.env.online` | `compose.yaml` | `iot-platform-online` |
| 离线部署 | 离线包内 `.env.offline` | `compose.yaml` + `compose.offline.yaml` | `iot-platform` |

脚本显式选择配置和 Compose 文件，在线/离线部署不会自动加载用于旧版本地调试的 `compose.override.yaml`。

首次执行设置平台管理员为 `admin` / `admin123`，其他服务凭据随机生成，并在每个配置项前写入中文说明；重复执行保留业务凭据并补齐说明；AI 配置会按下文统一迁移为 DeepSeek。不要重新生成配置文件来“重置”已有数据库。配置文件和离线包包含凭据，不应提交或公开分享。

**已有部署沿用原项目和凭据。** 新默认项目名会创建一套新数据卷，不会自动迁移旧数据。例如原服务用项目 `iot-platform`、配置 `.env`，在线更新应执行：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\deploy-online.ps1 -EnvFile .env -ProjectName iot-platform
```

```bash
bash ./scripts/deploy-online.sh --env-file .env --project-name iot-platform
```

已有自定义 Compose 覆盖文件、外部数据卷或外部数据库时，先核对原部署参数；上述命令只使用 `compose.yaml`。

### AI 与工作流

本地、在线、离线分别使用自己的环境文件。首次可不填 `DEEPSEEK_API_KEY`；在“模型管理”填写并保存（连接测试可选），或写入对应环境文件后重启。Provider 连接成功、Harness 健康和真实工作流成功分别检查。Ollama 只提供知识库嵌入，不下载 Qwen。Harness 必装，默认模型和固定版本以部署配置及 `deploy/deepseek-harness/REVISION` 为准。

源码 API 到 Harness 使用 `IOT_AI_HARNESS_URL/TOKEN`；容器回调使用 `IOT_AI_HARNESS_MCP_URL`，必须能到达源码 API，OrbStack 为 `host.orb.internal`。旧 Provider 数据、IDE 进程变量可能覆盖环境文件，排查时核对实际运行配置。工作流和权限见 [平台功能](PLATFORM.md#ai-与知识库)。

### 运维组件

`--dependencies-only` 包含运维基础环境，普通本地准备可加 `--include-ops` / `-IncludeOps`。源码与容器共用 `IOT_LOCAL_OPS_DIR`（默认 `data/ops`）；源码 API 须能写、组件须能读。普通远程虚拟机没有共享目录时，规则与通知配置为只读。将 `IOT_OPS_TENANTS` 设置为可授权运维的租户；Grafana 告警关闭，统一使用 Alertmanager。

## 查看状态、日志与停止

本地依赖：

```bash
docker compose -p iot-platform-local --env-file .env.local -f compose.local.yaml ps
docker compose -p iot-platform-local --env-file .env.local -f compose.local.yaml logs --tail=100
docker compose -p iot-platform-local --env-file .env.local -f compose.local.yaml down
```

源码备份服务的日志在 VS Code 的 `Backup Service` 调试终端；临时容器版则在上述命令前加 `--profile backup`，例如 `docker compose -p iot-platform-local --env-file .env.local -f compose.local.yaml --profile backup logs -f backup-service`。

在线部署：

```bash
docker compose -p iot-platform-online --env-file .env.online -f compose.yaml ps
docker compose -p iot-platform-online --env-file .env.online -f compose.yaml logs -f platform-api platform-web backup-service
docker compose -p iot-platform-online --env-file .env.online -f compose.yaml down
```

本地备份服务默认由源码调试进程提供；若使用临时容器版，执行 `setup-local` 时加 `--include-backup`，或在子命令前加 `--profile backup`。启用运维中心依赖时加 `--profile ops`。自定义项目名和配置路径时，上述命令也要使用相同参数。离线包的维护命令见 [离线部署说明](#离线部署)。

摄像头直播媒体服务使用 profile `video`，默认启用。本地用 `setup-local.sh --video on|off`（PowerShell 为 `-Video on|off`）；虚拟机模式同时加 `--dependencies-only`。在线部署用 `--video on|off`，也可使用 `scripts/video-module.sh` / `video-module.ps1`，也可用它查看 `status`、`logs`（本地加 `--mode local` / `-Mode local`，离线加 `--mode offline`）。启用后 `COMPOSE_PROFILES` 包含 `video`，上面的 `ps`、`logs` 会一并列出 `zlmediakit`；网络、资源与排查见 [摄像头直播](PLATFORM.md#摄像头)。

`down` 保留命名数据卷，`down -v` 会删除它们。日常代码更新重跑对应部署脚本；备份范围与调度见 [设备数据备份](#设备数据备份)。

## 容量相关配置

本节维护连接池、并发和限额配置；拓扑、角色与迁移见 [集群部署](#集群部署) 和 [进程职责](#进程职责)，实测流程见 [容量验证](DEVELOPMENT.md#容量验证)。

默认值参考历史压测瓶颈调整（见 [历史基线](DEVELOPMENT.md#容量验证)），不代表当前吞吐已复测。多副本时按下表核对：

| 配置 | 默认 | 说明 |
| --- | --- | --- |
| `IOT_POSTGRES_MAX_CONNS` | 64 | 每个进程（含各 Worker 角色）的 PostgreSQL 连接池；所有进程之和须小于服务端 `max_connections` |
| `POSTGRES_MAX_CONNECTIONS` | 300 | Compose 中 PostgreSQL 的 `max_connections` |
| `IOT_KAFKA_CONSUMER_CONCURRENCY` | 64 | 每个 Kafka 订阅的并行通道，同一设备保持顺序 |
| `IOT_AI_ANALYSIS_CONCURRENCY` / `IOT_AI_ANALYSIS_RPM` | 2 / 12 | 历史自动研判并发 / 请求额度参数；已取消告警事件自动研判，手动任务不使用这两个参数 |
| `IOT_CLUSTER_INSTANCES` | 1 | 共享限额存储（Redis）不可用时，各进程按“额度 ÷ 实例数”退化执行，避免总额度放大 |
| `IOT_AI_ANALYSIS_TIMEOUT` / `IOT_AI_ANALYSIS_MAX_WAIT` | 90s / 10m | 历史自动研判执行期限 / 事件最长等待年龄；手动研判执行期限为 3 分钟 |
| `IOT_INGEST_MAX_BACKLOG` | 50000 | 解析与业务流（`processor` 组）积压超过该值时暂停接收新原文，0 关闭 |
| `IOT_PROTOCOL_LISTENER_MAX_SESSIONS` | 1024 | 每个 TCP / UDP 接入监听的会话上限 |
| `IOT_MQTT_DEVICE_TOKEN_TTL` | 24h | 标准设备 MQTT 令牌有效期，仅在配置 EMQX 管理 API 时生效，否则 5 分钟 |
| `IOT_EMQX_MAX_MQUEUE_LEN` / `IOT_EMQX_MAX_INFLIGHT` | 100000 / 128 | EMQX 会话队列与在途窗口；队列满时 Broker 丢弃报文 |
| `IOT_CLICKHOUSE_CLUSTER` / `IOT_CLICKHOUSE_INSERT_QUORUM` | 空 / 空 | 设置集群名后使用各分片 `*_local` 复制表与同名 `Distributed` 表，插入同步写入分片并按法定副本数确认（如 `2` 或 `auto`）；旧单节点表须先用 `cmd/clickhouse-migrate` 迁移，平台检测到未迁移时拒绝启动 |
| `IOT_POSTGRES_MAX_CONN_LIFETIME` / `IOT_POSTGRES_HEALTH_CHECK_PERIOD` / `IOT_POSTGRES_CONNECT_TIMEOUT` | 30m / 15s / 5s | 连接回收与探活；配合多主机 DSN（`host=a,b,c target_session_attrs=read-write`）在主备切换后连到新主库 |
| `IOT_POSTGRES_READ_DSN` / `IOT_POSTGRES_MAX_REPLICA_LAG` | 空 / 5s | 可选只读副本，仅用于原文列表、历史曲线、设备消息与状态历史、总览统计；副本不可达或延迟超限时回主库。权限、告警与业务状态始终读主库 |
| `IOT_KAFKA_AUTO_CREATE_TOPICS` | true | 集群设为 `false`，主题由 `cmd/cluster-init` 按正式清单创建，避免自动建出单副本主题 |

EMQX 容器的文件句柄上限在 Compose 中设为 1048576，每条 MQTT 连接占一个句柄；自行部署 EMQX 时须同样放开。配置 `IOT_EMQX_API_URL`、`IOT_EMQX_API_KEY`、`IOT_EMQX_API_SECRET` 后平台可即时撤销设备凭据，并采集 `mqtt_broker_dropped` 以发现 Broker 丢弃。

本地准备、在线部署和离线打包会补齐空的 EMQX 管理凭据，保留已有密钥；仅填写 Key 或 Secret 会报错。Compose 通过 EMQX 5.8 的 `api_key.bootstrap_file` 引导文件加载专用 API 凭据。源码机 URL 为可达的 Broker 管理根地址（不带 `/api/v5`）；在线/离线默认为 `http://emqx:18083`。管理端口不得公开到不受信任网络。既有运行环境可通过 EMQX 管理 API 创建专用 Key，保存到环境文件后重启平台；只编辑文件不等于已在 Broker 创建 Key。详情参见 [EMQX REST API](https://docs.emqx.com/en/emqx/latest/guides/api.html)。

集群规划先运行只读检查，不创建主题或实例：

```bash
go run ./cmd/capacity-check -env-file .env.local -replicas 3 -postgres-reserve 32
# 有真实供应商额度和实测平均延迟时，再填入 provider-rpm 与 model-latency
```

输出区分配置预算通过、阻塞、未验证及模型估算。它读取实际 PostgreSQL 最大连接数、Kafka 分区/副本、ClickHouse 表引擎及 MQTT 会话可观测性；存储分片、磁盘接管、连接路由和模型 token 配额仍须在目标集群验证。`clusterCapacityVerified=false` 始终保留，不能把 API 进程数乘以单机速率当作最高容量。

## 集群部署

多节点部署由**集群清单**统一描述，`scripts/cluster-up.sh` / `.ps1` 一条命令完成镜像、秘密、渲染、节点预检、下发、按阶段启动、初始化与就绪检查；其中 `cmd/cluster-render` 为每个节点生成独立的 Compose 项目（主机网络、固定端口），`scripts/cluster-deploy.sh` / `.ps1` 按阶段下发与启动。Compose 只管理本节点；跨节点布局、故障域和连接预算由清单校验。

| 组件 | 集群形态（示例清单 `deploy/cluster/inventory.example.yaml`） |
| --- | --- |
| Redpanda | 3 节点，业务主题与死信主题复制因子 3，关闭自动建主题 |
| PostgreSQL | Spilo（Patroni）3 成员 + etcd 3 节点，可选同步备库；平台使用多主机 DSN `target_session_attrs=read-write` 连接当前主库，历史查询使用 `prefer-standby` 只读 DSN |
| ClickHouse | 2 分片 × 2 副本 + Keeper 3 节点，`*_local` 复制表与同名 Distributed 表，插入按法定副本确认 |
| Redis | 主 + 2 副本 + Sentinel 3 个，平台经 Sentinel 跟随主节点 |
| EMQX | 3 节点静态集群 |
| 平台 | api、gateway、parser、processor、ai、jobs 各自多实例；API 之间选举视频控制实例；Harness 多实例按会话路由 |
| 监控 | Prometheus 按实例抓取所有平台进程、Redpanda、EMQX 与各节点 node-exporter |

校验规则：节点须写明故障域（独立主机/供电/机柜；同一宿主上的虚拟机属于同一故障域）；仲裁组（etcd、Patroni、Redpanda、Keeper、Sentinel）为奇数成员且任一故障域不占多数；同一 ClickHouse 分片的副本、Redis 主从、EMQX 成员跨故障域；同一节点端口不冲突；各角色 PostgreSQL 连接池合计（含一次滚动升级额外实例、备份与初始化连接及预留）不超过 `max_connections`。

### 一键部署与升级

在仓库根目录执行（不带参数即进入向导）：

```bash
bash scripts/cluster-up.sh
```

Windows 控制机（PowerShell，需 Docker Desktop 与 OpenSSH 客户端）：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\cluster-up.ps1
```

向导在开始时一次问完，之后无人值守执行：

1. 集群名称（默认 `torchlink`）与**节点数量**（至少 3 台；1 台请用单机部署，2 台无法形成仲裁）。
2. 每个节点的 IP；SSH 用户名（默认 `root`）与端口（默认 22）。
3. **SSH 登录方式**：所有节点统一密码（输入一次）或每个节点独立密码（逐个输入）。密码隐藏输入、只在内存中使用：脚本用它登录一次，记录主机密钥（`.cluster/<名称>/known_hosts`，首次信任，之后变化即拒绝），并把部署专用公钥写入各节点 `~/.ssh/authorized_keys`（注释为 `torchlink-deploy@<名称>`，可据此撤销）；此后所有操作用私钥 `.cluster/<名称>/deploy_key` 登录，升级时不再询问密码。
4. **服务统一密码**：用于 PostgreSQL（应用、超级用户、复制）、Redis、ClickHouse、MinIO、EMQX 控制台和平台管理员 `admin`；至少 8 位，只能包含字母、数字与 `. _ ~ -`（要嵌入连接串）。直接回车则每项随机生成。JWT 密钥、Harness 令牌、摄像头凭据密钥和服务间令牌始终随机生成（有长度或格式要求）。服务统一密码只在首次部署时设置；再次执行沿用已有秘密，若指定了不同的密码会拒绝（修改数据库密码需单独操作）。
5. 是否部署摄像头直播模块；DeepSeek API Key（可留空，部署后可在“模型管理”填写）。

节点布局按节点数自动生成到 `.cluster/<名称>/inventory.yaml`：每个节点视为独立故障域；etcd、PostgreSQL、Redpanda、EMQX、Redis 与 Sentinel 放在前 3 台；ClickHouse 3 台时为 1 分片×3 副本，4–5 台为 2×2，6 台及以上为 2×3；MinIO、知识库、视频、备份与监控放在最后一台；api、gateway、Harness、Web 各 2 个实例，parser、processor 各 3 个。可以手动修改该文件后重新执行。已有自写清单时用 `--inventory <文件>`（首次同样询问 SSH 密码）；自有私钥用 `--ssh-key <文件>`，此时不安装部署密钥。

无人值守：`bash scripts/cluster-up.sh --name <名称> --nodes IP1,IP2,IP3 --yes`，SSH 密码与服务统一密码经环境变量 `TORCHLINK_SSH_PASSWORD`、`TORCHLINK_SERVICE_PASSWORD` 提供（PowerShell 为 `-Name`、`-Nodes`、`-Yes`）。

脚本依次完成：

1. **镜像**：用当前源码构建平台、Web、Harness、备份与媒体镜像（清单中写成 `镜像@sha256:` 的改为拉取），拉取其余第三方镜像。控制机不需要 Go：渲染、初始化与 SSH 准备工具随平台镜像提供。
2. **SSH 与秘密**：按上述方式安装部署密钥；在 `.cluster/<名称>/secrets.yaml`（0600，已被 Git 忽略；可用 `--secrets` 指定）生成缺失的密码与令牌，设置了服务统一密码的项使用它，已有值保持不变。`deepseekApiKey`、`backupRestoreTargetDSN` 需要时自行填写，也可部署后在“模型管理”配置模型密钥。
3. **渲染**：生成各节点的 Compose 项目到 `.cluster/<名称>/rendered`，上一版改名为 `rendered.prev` 用于回滚。
4. **节点预检**：经 SSH 检查每个节点的 Docker 与 Compose v2、Docker 可用空间（至少 20 GiB）、与控制机的时钟差（不超过 5 秒）；首次部署时检查所需端口未被其他程序占用。任一问题都会在改动节点之前列出并停止。
5. **下发镜像**：按镜像 ID 比较，只把节点缺少或版本不同的镜像以 `docker save | gzip | ssh docker load` 方式发送。
6. **启动与初始化**：按阶段启动（coordination → data → init → support → workers → edge），每阶段等待容器健康；init 在第一个 API 节点上用平台镜像运行 `cluster-init`，创建应用库、迁移结构、创建主题并核验 ClickHouse 表，数据库选主期间自动重试；最后逐实例检查 `/health/ready`。
7. **结果**：输出 Web、MQTT、设备 HTTP 与 TCP 入口地址，以及管理员账号和密码所在位置。

交互终端下，脚本在下发前会显示“首次部署/升级”和节点数并请求确认，`--yes`（`-Yes`）跳过确认。`--dry-run` 只渲染到 `rendered.dry-run` 并打印将执行的命令，不改动任何节点，也不询问密码。节点上的 SSH 用户须能直接执行 `docker`（root 或 docker 组成员），sshd 需允许密码登录一次以安装部署密钥。

**离线环境**：在可联网的机器上执行 `bash scripts/cluster-up.sh --name <名称> --bundle cluster-images.tar`（或 `--inventory <清单>`），构建并拉取全部镜像后写入一个归档，这一步不连接节点；把源码、`.cluster/<名称>/` 和归档带到离线控制机，执行时加 `--images cluster-images.tar`（PowerShell 为 `-Bundle` / `-Images`）。

**内部负载均衡**：清单中 `platform.internalURL`、`gatewayURL` 留空时，每个节点运行一个只监听 `127.0.0.1` 的 HAProxy（`18181` 转发到全部 API 实例，`18182` 转发到全部 Gateway，按 `/health/ready` 摘除故障实例），Web 代理、Harness 回调、媒体回调和 API 到 Gateway 的转发都走本机负载均衡，不需要额外的负载均衡设备。如已有负载均衡器，两个地址同时填写即可，此时不渲染 HAProxy。面向用户和设备的入口（Web、EMQX、Gateway 节点）仍需由 DNS、VIP 或外部负载均衡器统一，脚本结束时会列出各组地址。

**升级与回滚**：修改源码或清单后执行 `bash scripts/cluster-up.sh --name <名称>` 即为升级，不再询问问题，秘密沿用，只发送变化的镜像，容器按阶段重建。回滚：

```bash
bash scripts/cluster-deploy.sh --rendered .cluster/<名称>/rendered.prev --ssh-user root --ssh-key .cluster/<名称>/deploy_key --known-hosts .cluster/<名称>/known_hosts
```

`.cluster/<名称>/` 中的秘密文件决定已初始化数据库的密码，`deploy_key` 用于登录节点，务必另行备份；丢失秘密后重新生成的值无法连接已有数据。

### 分步执行

需要只校验清单、只渲染或只操作部分节点时，可单独使用各步骤：

```bash
go run ./cmd/cluster-render -inventory deploy/cluster/inventory.example.yaml -check
```

```bash
go run ./cmd/cluster-render -inventory <清单> -secrets <0600 秘密文件> -init-secrets -out dist/cluster/<名称>
```

```bash
bash scripts/cluster-deploy.sh --rendered dist/cluster/<名称> --ssh-user <用户>
```

- 秘密文件模板为 `deploy/cluster/secrets.example.yaml`，`-init-secrets` 会补齐缺项；嵌入连接串的密码只能用字母、数字与 `. _ ~ -`。秘密只写入需要它的节点的 `.env`（0600）与本机 `init.env`，`compose.yaml` 和配置文件只含变量引用。
- `cluster-deploy` 不负责镜像：节点需已有镜像，或用 `--images <归档>` 让每个节点整体导入。`--stage`、`--nodes` 可只执行指定阶段和节点，`--dry-run` 只打印命令；`--cluster-init "go run ./cmd/cluster-init"` 改为在控制机本地运行初始化。
- 部署顺序与每阶段内容同上一小节第 6 步。

**扩缩容与升级**：修改清单后重新执行 `cluster-up`；新节点需先满足预检要求。只想操作部分节点时，用 `cluster-deploy` 对变化的阶段和节点执行，例如 `--stage workers --nodes n5`。扩容前先看渲染输出的连接预算。升级按 workers → edge 逐角色滚动；processor/parser 缩容时进程先停止领取，未完成消息的领取租约到期后由其他实例接管。Redpanda、ClickHouse 扩容涉及数据重分布，按组件文档限制重建流量，并把重建期间纳入容量测试。

**回滚**：`cluster-up` 自动保留上一版渲染（`rendered.prev`），用它重新执行部署脚本即可回到旧配置；不删除数据卷。已写入新集群的数据不会因切回配置而回退，涉及存储迁移的回滚按下方迁移检查点处理。

**从单节点迁移**：

1. 在单节点上排空旧 `storage`/`parser` 积压（见 [业务流升级](#按设备业务流与跨实例一致性)），停止设备接入或让设备保留未确认报文，记录 PostgreSQL 备份点与 Kafka 偏移。
2. 渲染并按阶段启动集群到 init 完成；PostgreSQL 数据用 `pg_dump`/`pg_restore` 导入新主库后再运行 `cluster-init -execute`。
3. ClickHouse 用迁移工具按月分区回填并核对（默认只输出计划）：

   ```bash
   go run ./cmd/clickhouse-migrate -source <单节点 URL> -target <集群节点 URL> -cluster iot_cluster
   ```

   计划无误后追加 `-execute`；部分填充的目标分区会被阻止，须检查后删除再重跑。核对使用行数、唯一消息数与消息 ID 校验和。
4. 启动 workers 与 edge，用少量设备核对原文、解析、状态、告警与权限，再切换入口并逐步放量。旧环境保留到新集群通过核对与容量快速回归。

渲染与脚本的仓库内验证覆盖清单校验、端口与连接预算、生成文件的 `docker compose config` 解析、部署脚本的阶段顺序（`--dry-run`），以及用模拟的 `docker`/`ssh` 走完一键部署全流程（首次部署、升级、预检拦截、离线归档）；目标机上的真实启动、Patroni/Sentinel/Keeper 实际选主与切换须在目标环境演练并记录。

## 高可用边界

默认 Compose（本地、在线、离线）是**单节点**配置：PostgreSQL、ClickHouse、Redis、Redpanda、EMQX、MinIO、Weaviate、Ollama、Harness 与 API 各运行一个实例，Redpanda 主题创建为 `--replicas 1`。它可以承担单机生产，但不具备高可用：

- 容器自动重启只在进程退出后拉起同一实例，不能在宿主机、磁盘或数据卷故障时切换。
- MQTT 持久队列（`IOT_DATA_DIR/mqtt-inbox/`）保证已确认报文在本机磁盘上重启后可继续处理，不复制到其他节点。
- 备份用于事后恢复数据，恢复需要停机与人工操作，不是故障切换；备份存在不等于已验证可恢复。
- 拆分 `api` / `gateway` 与多副本 API 只分担接入和查询，前提是数据库、消息与对象存储本身可用。
- 运维中心依赖（`--profile ops` 的 Prometheus、Loki、Grafana、Alertmanager）同样各一个实例；它们停止时接入与告警链路不受影响，但期间的监控数据、日志与告警通知会缺失。

需要高可用时使用上一节的 [集群部署](#集群部署)：Redpanda、PostgreSQL、ClickHouse、Redis、EMQX 与各平台角色均为多实例；MinIO、知识库、视频媒体与 Prometheus 在示例清单中仍为单实例，需要时改用分布式/外部服务。节点故障与切换须在目标环境演练，仓库内只验证渲染与部署编排。

### 排查与迁移

| 现象 | 检查入口 |
| --- | --- |
| 地址或密码似乎未生效 | IDE 进程变量优先；确认原环境文件、Compose 项目和已有数据库密码 |
| API 存活但业务不可用 | `/health/ready`、消费者积压、数据库及 `docker compose logs`；live 只表示进程存活 |
| 镜像/模型哈希不符或缺失 | 在有网机器重做完整包，不在离线目标机拉取、不删除模型卷 |
| Harness `RUNTIME_ERROR` | 用镜像内 `runtime-smoke.mjs` 检查运行用户依赖权限，再分别检查 Key、模型请求和 MCP 回调 |
| 媒体不可播放 | `/api/v1/video/status`、连接测试、目标白名单、RTC 地址、编码及播放权限 |
| openEuler 镜像导入 `mknod` 失败 | 检查 `container-selinux`、受管程序/数据标签和 Docker 进程域 |

旧现场节点配置与协议分发已移除；迁移为 `combined` 或 `api+gateway`，逐项核对共享存储、协议制品和执行节点。已退役的影子/拓扑表不再创建或读取，迁移不删其旧数据。升级权限模型后，未配置设备范围的用户默认无设备；旧普通用户 MQTT 会话须断开或等待旧令牌过期，不以隐藏按钮代替撤销。

## 设备数据备份

备份范围仅包含 PostgreSQL 的原始报文、标准解析消息，以及 ClickHouse 的原始报文和解析遥测数据。不会备份数据库结构、账号、Redis、消息队列、知识库、配置文件或整个 MinIO。设备原始报文按接收时间分日；标准消息按处理时间（旧记录回退到消息时间）分日，ClickHouse 遥测按消息时间分日。两种存储的数据分别保留来源，可能包含同一解析消息的不同表示。

- **立即备份设备数据**：导出当前保存的设备数据。
- **备份昨日数据**：按配置时区导出前一个自然日的数据。
- **每日自动备份**：默认开启，每天上海时间 00:05 执行昨日备份。服务需要持续运行；停机期间不会自动补跑历史日期。
- 每个备份包含原始数据、解析数据两个 gzip JSONL 文件及清单，保存到 MinIO 的 `iot-backups` 桶；保留下载、SHA-256 文件校验及历史记录。文件校验不等于恢复到数据库。
- **恢复验证（恢复到独立库）**：备份列表的“恢复验证”调用 `POST /api/v1/backups/:id/restore`，由备份服务把该备份的全部记录写入 `IOT_BACKUP_RESTORE_TARGET_DSN` 指向的独立 PostgreSQL 库（表 `restored_message`、`restore_run`），并按清单核对条数与消息数。目标库与业务库的主机、端口和库名相同时拒绝执行（HTTP 412），不会覆盖业务数据；未配置时返回 412。该操作证明备份可读回数据库，不替换现网数据；同一时间只运行一个备份或恢复。

```dotenv
# 是否开启每日自动备份；关闭后仍可手动备份
IOT_BACKUP_ENABLED=true
# 每日执行时间，备份前一个自然日
IOT_BACKUP_TIME=00:05
# 日期与执行时间使用的时区
IOT_BACKUP_TIMEZONE=Asia/Shanghai
# 压缩文件暂存目录
IOT_BACKUP_DIR=./data/backups
# 恢复验证用的独立库（须与业务库不同，例如同实例的 iot_restore_check 库）；留空则不提供
IOT_BACKUP_RESTORE_TARGET_DSN=
```

Windows 源码调试只需 Go 环境，使用 `go run ./cmd/backup-service --env-file .env.local` 或 VS Code 的 `IoT Platform (API + Web + Backup)`；数据库与 MinIO 可继续运行在 CentOS。旧备份记录与文件不删除，旧接口类型 `RAW_LOGS` / `INCREMENTAL` 兼容映射为昨日设备数据备份。

## 独立接入进程

当前保留中心 API 与独立 Access Gateway。旧现场节点配置的迁移见 [旧版本迁移](#配置与维护)。

### 进程职责

- `cmd/iot-platform` 默认 `IOT_PROCESS_ROLE=combined`，保留单进程入口。
- `IOT_PROCESS_ROLE=api` 不启动 Modbus、Listener 或外部 MQTT 上行订阅；默认仍内嵌解析、业务处理、AI 研判消费与后台任务（`IOT_API_EMBEDDED_WORKERS=true`）。设为 `false` 后 API 只提供管理接口、运维中心和视频控制，必须同时部署下列 Worker 角色，否则上报不会被解析和处理。
- Worker 角色（同一镜像与入口，设置 `IOT_PROCESS_ROLE`）：

  | 角色 | 运行内容 | 扩容依据 |
  | --- | --- | --- |
  | `parser` | 消费 `iot.raw.message`，解析并发布到内部业务流 `iot.device.business`（及对外 property/event/parsed 主题） | 原文积压、解析耗时 |
  | `processor` | 按设备顺序消费 `iot.device.business`：规则、告警、设备状态、完成标记、outbox 转发 | 业务流积压、数据库等待 |
  | `ai` | 保留角色兼容，不再消费告警事件执行自动研判；手动研判由 API 进程运行 | 无告警研判消费者 |
  | `jobs` | 离线扫描、原文重发、视频媒体重试、凭据吊销重试、设备告警通知；每项任务以数据库租约保证全集群只有一个实例执行，多实例互为备用 | 待执行量 |

  Worker 只开放 `/health/*` 与 `/metrics`；`/health/ready` 只检查本角色依赖，并返回 `role`、`instance`。指标带 `process_info{role,instance}`。所有拆分角色都需要共享 PostgreSQL 与 Kafka；只有 `api`、`ai`（及 `combined`）需要 `IOT_AI_HARNESS_URL`。
- `IOT_INSTANCE_ID` 为实例名（默认主机名），用于指标、租约所有者和日志。显式设置后 MQTT 持久队列目录变为 `mqtt-inbox/<角色>/<实例>`；同一数据卷上运行同角色多副本时每个副本必须设置不同值。未设置时沿用旧目录，升级不会遗留未确认报文。
- `cmd/iot-access-gateway` 强制 gateway 角色：执行通信、鉴权和 Raw 归档，发布到共享 Kafka；不启动 Raw 业务消费者。HTTP 只开放接入与健康相关路由，管理用户身份在目标接口重新校验。
- api/gateway 两个进程必须配置同一个 PostgreSQL、Kafka 及一致的 Raw 分层存储。协议制品目录也必须共享；不能让两个进程各自使用内存仓库或本地消息总线。
- API 通过 `IOT_ACCESS_GATEWAY_URL` 转发添加设备的预检与保存、标准上报、设备连接详情与命令；Gateway 不可达返回 503，不将请求已发送视为操作成功。

本地运行示例（在仓库根目录，凭据来自各自环境文件）：

```bash
go run ./cmd/iot-platform --env-file .env.api
go run ./cmd/iot-access-gateway --env-file .env.gateway
```

可选容器拆分：`docker compose -p iot-platform-online --env-file .env.online -f compose.yaml -f compose.access.yaml config --quiet` 先检查渲染结果；实际启动再运行相同参数的 `up -d --build`。已有部署须替换为原项目名。覆盖层将 TCP/UDP 端口从 API 移到 Gateway，默认 Gateway HTTP 端口为 8082。覆盖层使用 `!override`，要求 Compose 2.24.4 或更新版本，见 [Docker 合并规则](https://docs.docker.com/reference/compose-file/merge/)。

拆分部署同样使用 DeepSeek API；密钥由 API 的模型管理和 Harness 使用，Gateway 不承担模型推理，也不需要部署对话模型。

### 按设备业务流与跨实例一致性

- 解析结果按 `(租户, 设备)` 键写入内部主题 `iot.device.business`，由 `processor` 消费组按设备顺序处理；不同设备并行。原 `iot.property.report`、`iot.event.report`、`iot.parsed.message` 继续发布供外部订阅（`IOT_PUBLISH_EXTERNAL_TOPICS=false` 可关闭），平台内部不再消费。
- 每条标准消息先原子领取（60 秒租约、领取代次），只有最新代次能写入完成标记；再均衡时新消费者等待旧持有者完成或租约到期，旧实例迟到的完成被拒绝并计入 `standard_claim_fenced_total`。
- 告警确认/恢复/关闭、规则停用、离线扫描、连接状态和设备状态写入都基于行版本做乐观并发，冲突时重读重试（`alarm_conflict_total`、`device_state_conflict_total`），不会用旧快照覆盖新上报。
- 规则与协议缓存在各实例本地保留最多 2 秒，跨实例生效时间以此为上界；权限每次请求读取数据库，撤销立即生效。
- 设备上报 20 条/秒、开放 API 密钥 100 次/秒、登录失败锁定（15 分钟窗口内 10 次）在配置 Redis 后由所有实例共享同一额度（固定时间窗口，窗口按 Unix 时间对齐）；Redis 故障时按 `IOT_CLUSTER_INSTANCES` 退化，并计入 `rate_limit_shared_errors`。
- Redis 支持 Sentinel：设置 `IOT_REDIS_MASTER_NAME` 与 `IOT_REDIS_SENTINELS`（逗号分隔）后跟随主节点切换；设备状态缓存按行版本写入，旧写入不会覆盖新缓存。
- 回放任务记录执行实例与心跳；执行进程退出后，超过 60 秒无心跳的任务在查询时标为 `INTERRUPTED`，不会永久停在运行中。
- MQTT 持久队列目录加进程独占锁，同一目录被第二个进程打开时启动失败，避免两个实例共用一个 client ID。

### 多 API 实例：视频控制与 Harness

- 多个 API 实例都设置 `IOT_NODE_URL`（本实例可被其他实例访问的 HTTP 地址）后，通过 `video/control` 租约选出一个实例运行直播模块（SIP 服务、播放会话、媒体任务与清理）；其他实例把 `/api/v1/video/*`、`/api/v1/integrations/video/*`（含媒体服务器回调与 HLS 鉴权）带原用户凭据转发到持有者，由持有者重新校验。持有者续租失败立即停止模块，租约过期后备用实例接管并从数据库恢复播放会话；现有 SIP 连接与 WebRTC 播放需要设备重新注册、浏览器重新点播。单实例部署不设置该变量时行为不变。
- 媒体服务器回调地址、SIP 端口映射须指向当前持有者或能转发到它的入口；权限变化由持有者每 15 秒复核一次后撤销播放。
- `IOT_AI_HARNESS_URL` 可填多个逗号分隔地址。同一会话按会话 ID 固定路由到同一 Harness 实例（多轮上下文保存在该实例）；该实例不可达时改由下一实例开始新会话。模型配置与动态智能体同步到全部实例，任一实例健康即视为可用。

**从旧版本升级**：旧版由 `storage` 消费组处理 property/event/parsed 主题，新版改为 `processor` 组处理业务流，两者不能同时产生副作用。升级顺序：

1. 在旧版本上确认 `kafka_lag_storage` 与 `kafka_lag_parser` 为 0（可短暂停止设备接入或等待积压排空）。
2. 停止全部旧 API/Worker 进程，再启动新版本；Compose 初始化会创建 `iot.device.business` 与各消费组死信主题，自建 Kafka 需先创建（分区数与 `iot.raw.message` 一致）。
3. 旧 `iot.dlq.storage` 中的死信用 `go run ./cmd/dlq-replay -group storage -source-topic <原主题> ...` 核对后重新送入业务流；新死信使用默认 `-group processor`。

### 执行所有权

启用 `IOT_ACCESS_COORDINATION=true`，并将 `IOT_ACCESS_NODE_URL` 设置为其他实例可达且精确指向本实例的 HTTP(S) 地址。不要使用随机负载均衡地址冒充固定执行节点。

共享 PostgreSQL 的 `execution_lease` 以租户和 Profile 为资源键。租约 10 秒，节点本地取消期限短于数据库期限，续租失败即取消旧执行；接管提升 fencing token。运行时在归档前检查所有权和当前配置。配置停止被扫描时释放租约。

可确定 Profile 的请求按租约中的节点地址转发，保留原用户授权并限制转发次数。设备级路由依赖明确的实例标签或唯一的设备配置关联，不能据此推断所有历史设备、子设备及任意多副本部署均可自动路由。该实现提供互斥执行与接管，不宣称已有按负载最优调度、跨节点迁移现有 TCP 会话或数据库之外的强制 OS 隔离。

### 验证入口

`go test ./internal/platformapp ./internal/httpapi ./internal/protocolruntime` 覆盖进程职责、认证转发与执行协调；环境相关集成测试的实际执行条件见各测试。源码入口为 `internal/httpapi/process_role.go`、`internal/httpapi/execution_route.go` 和 `internal/protocolruntime/coordinator.go`。

### 管理界面与用户范围

「设备模板 → 接入点」管理 `DeviceAccessProfile` 软件连接配置；「主设备」标签管理现场设备台账；`cmd/iot-access-gateway` 是部署进程，三者含义不同。API 和 Gateway 都需要使用包含设备权限校验的同版代码，转发保留原用户身份，并在目标服务重新校验。

普通用户的全租户接入配置需要全部设备范围和相应菜单/按钮权限。指定设备用户的连接详情不暴露共享网关配置及其他设备会话。用户设备和告警范围见 [用户权限](PLATFORM.md#权限与设备范围)。
