# 部署与本地调试

[本地](#本地运行) · [在线](#在线部署) · [离线](#离线部署) · [摄像头](#摄像头部署) · [容量测试](#容量测试模块) · [维护](#配置与维护) · [拆分 Gateway](#独立接入进程)

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

脚本生成 `.env.local`，将基础服务工具账号默认设为 `admin` / `admin123`，独立生成内部密钥与令牌，启动依赖与 Harness，设置云端 Embedding 默认配置，执行 `go mod download` 和 `npm ci`。重复执行复用已有配置与数据，登录与工具连接见[工具连接账号](#工具连接账号)。

| 需求 | PowerShell 参数 | Bash 参数 |
| --- | --- | --- |
| 自定义 DeepSeek API 模型（默认无需传入） | `-DeepSeekModel deepseek-flash` | `--deepseek-model deepseek-flash` |
| 只准备依赖，不下载源码依赖 | `-SkipCodeDeps` | `--skip-code-deps` |
| Linux 虚拟机部署全部基础环境，源码在本机运行 | 在 Linux 虚拟机执行右侧命令 | `--dependencies-only` |
| 临时运行容器版备份服务 | `-IncludeBackup` | `--include-backup` |
| 启动运维中心依赖（Prometheus、Loki、Grafana、Alertmanager、采集器） | `-IncludeOps` | `--include-ops` |
| 开启 / 关闭摄像头直播媒体服务（默认开启，省略沿用上次选择） | `-Video on` / `-Video off` | `--video on` / `--video off` |
| 开启 / 关闭随源码 API 运行的容量控制器（默认开启，省略沿用上次选择） | `-Capacity on` / `-Capacity off` | `--capacity on` / `--capacity off` |

对话与推理默认使用 DeepSeek API。启动后在“模型管理”填写 API Key 并保存即可，连接测试可选；也可通过各环境文件的 `DEEPSEEK_API_KEY` 配置。未填密钥不阻止平台启动。知识库使用 PostgreSQL + pgvector，向量计算与检索重排由随平台部署的 `embedding` / `reranker` 服务完成，无需密钥，见[知识库向量服务](#知识库向量服务)。完整配置、升级与离线联网边界见 [AI 配置](#ai-与工作流)。

依赖部署到虚拟机时使用 `--dependencies-only`，网络与配置共享见 [端口与地址](#端口与地址)；Mac 使用 [OrbStack 调试](#orbstack-虚拟机本地调试)。

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

### 本地 API 进程交接

终端、IDE 与临时测试 API 不能同时使用相同监听端口和 MQTT 收件箱目录。启动提示 `MQTT inbox directory is used by another process` 或 `8081` 被占用时，先核对实际环境文件、`IOT_DATA_DIR`、`IOT_PROCESS_ROLE` 和 `IOT_INSTANCE_ID`，再定位原进程。macOS / Linux 可执行：

```bash
lsof -nP -iTCP:8081 -sTCP:LISTEN
lsof ./data/mqtt-inbox/combined/inbox.lock
ps -p <已确认的PID> -o pid,ppid,command
```

收件箱路径按实际配置替换；默认目录为 `<IOT_DATA_DIR>/mqtt-inbox/<角色>`，显式实例 ID 会再增加一层实例目录。Windows 可用 `Get-NetTCPConnection -LocalPort 8081 -State Listen` 查看 `OwningProcess`，再用 `Get-Process -Id <PID>` 核对进程。

确认是需要交接的旧实例后，在原终端按 Ctrl+C 或停止 IDE 调试；无原终端时，macOS / Linux 可对该 PID 发送 `kill -TERM <PID>`。等待旧进程退出，再检查端口与锁持有者并启动新实例。锁由操作系统在进程退出时释放；**不要删除 `inbox.lock` 或收件箱数据**，删除锁文件可能让两个进程分别锁住不同文件。临时测试应使用隔离配置、数据目录及空闲端口，并在结束时正常退出；仅更换实例 ID 不能解决端口和其他资源冲突。基础依赖继续沿用，无需为交接重建容器。

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

脚本生成 `.env.online`，构建镜像，启动并检查服务；不下载 AI 模型权重。首次需要访问镜像、Go/npm 依赖、Harness 源码及 pgvector 源码；服务器无需预装 Go 或 Node.js。更新源码后重跑同一脚本，沿用原配置、Compose 项目和数据卷。使用自定义旧环境时，先按 [配置与数据归属](#配置与维护) 指定原参数。

## 端口与地址

| 服务 | 本地运行 | 在线 / 离线部署 |
|---|---|---|
| Web | `5173`（Vite） | `8080`，可设 `IOT_WEB_PORT` |
| API | `8081`（本机 Go） | `8081`，可设 `IOT_API_PORT` |
| PostgreSQL / Redis | `15432` / `16379` | 仅容器网络 |
| ClickHouse | `18123` | 仅容器网络 |
| Kafka | `19092` | `19092` 默认仅宿主机（`KAFKA_BIND_ADDRESS`），可设 `KAFKA_PORT`；对外提供 Kafka 订阅时设为 `0.0.0.0` 并把 `IOT_KAFKA_ADVERTISED_HOST` 设置为客户端可达地址。升级时 `IOT_KAFKA_PUBLIC_BROKERS` 已是非本机地址的环境由部署脚本自动保留外部监听 |
| RustFS 数据 / 控制台 | `19000` / `19002` | 数据仅容器网络，控制台 `9001` 默认仅宿主机（`MINIO_CONSOLE_BIND_ADDRESS`） |
| MQTT / WebSocket | `1883` / `8083` | `1883` / `8083` |
| EMQX 控制台 | `18083` | `18083`（`EMQX_DASHBOARD_PORT`）默认仅宿主机（`EMQX_DASHBOARD_BIND_ADDRESS`），需要远程管理时只开放给可信网络 |
| TCP / UDP 协议接入 | 本机 Go API 直接监听接入点端口 | `26875` TCP+UDP；其他监听端口写入 `IOT_PROTOCOL_PORTS`（单个端口或范围）后重跑部署，拆分 Gateway 时由 Gateway 发布 |
| 备份服务 / Harness | 备份源码进程 `8092` / Harness `8091` | `8092` / `8091`，仅宿主机 |
| Prometheus / Grafana | `19090` / `13000`（`--include-ops`） | Prometheus `9090` 仅宿主机（`PROMETHEUS_BIND_ADDRESS` 可改）/ Grafana `3000`（`GRAFANA_PORT`）默认仅宿主机（`GRAFANA_BIND_ADDRESS`） |
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

`--dependencies-only` 自动安装缺失的 Docker Engine、Compose、Buildx，部署 PostgreSQL（含 pgvector）、Redis、ClickHouse、Redpanda、EMQX、RustFS 主库/备库、Harness 及整套运维组件。备份服务不属于基础环境，默认与 API、Vue 一起在源码机调试；虚拟机不安装 Go/npm 源码依赖。首次需要联网下载镜像、构建 Harness、RustFS 和 PostgreSQL pgvector 镜像；失败可原命令重试，重复执行复用凭据与数据，不清理机器。

对象存储使用 S3 兼容的 RustFS（`rustfs/rustfs:1.0.1`，Apache 2.0），本地、在线、离线与集群部署相同；平台的 `IOT_MINIO_*` 与凭据变量 `MINIO_ROOT_*` / `MINIO_DR_ROOT_*` 沿用原名。RustFS 以非 root 用户运行，数据卷为 `rustfs-data` / `rustfs-dr-data`，与原 `minio-data` / `rustfs-dr-data` 不同名，见 [从 MinIO 迁移](#从-minio-迁移到-rustfs)。

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

Linux 目标支持 `arm64/aarch64` 与 `amd64/x86_64` 两种 64 位架构，不包含 32 位 ARM/x86。Compose 不固定 `platform`，基础镜像自动选择 Docker Engine 的原生架构，应用及 Harness 在目标架构构建。PostgreSQL 镜像按目标架构编译固定版本 pgvector，不携带 AI 模型权重，不要求 GPU。离线包仍须按目标架构分别打包，部署脚本会核对包内记录的架构，不能在两种架构间混用。修改镜像版本后，应重新检查镜像清单包含 `linux/arm64` 和 `linux/amd64`，并各自执行部署和实机检查；镜像清单与交叉编译通过不等同于目标系统部署验收。

在 ARM Mac 上通过 OrbStack 模拟 x86 Ubuntu 时，EMQX 的 Erlang JIT 默认双重内存映射可能导致 QUIC 模块报 `nif_library_not_loaded`。仅对此类模拟环境，在对应环境文件加入 `IOT_EMQX_ERL_FLAGS="+JMsingle true"` 后重跑部署命令。该参数保留 QUIC 功能，改用单一可读写执行的内存映射；原生 ARM64 和 x86_64 不需要设置，默认保持 Erlang 的内存保护行为。参数语义见 [Erlang JIT 文档](https://erlang.org/documentation/doc-14/erts-14.0/doc/html/erl.html)。

在线/离线默认提供 HTTP 服务。公网 HTTPS 由现有反向代理终结 TLS 并转发到 Web 端口，脚本不管理域名或证书。

## 离线部署

打包机需联网、Docker 和 Compose 2.24.4+，CPU 架构须与目标一致。Linux 目标机可从包内安装 Docker；Windows/macOS 须先安装 Docker Desktop。目标机无需 Go、Node 或源码。包内包含应用、基础环境、备份、运维组件、Harness 和带 pgvector 的 PostgreSQL。包内不携带 AI 模型或模型权重；离线包可安装平台，AI 对话及知识向量计算仍需访问外部 API。

### 获取或制作离线包

在 GitHub Actions 页面手动运行 `.github/workflows/offline-bundle.yml`（workflow_dispatch）构建 Linux amd64 包，成功后发布到 Releases，不随合并自动发布；下载同一版本的全部分卷、`SHA256SUMS` 与 `DEPLOY.txt`，按说明校验和解压。GitHub 的 Source code 不是部署包。公开包不含现场密码或 API Key，首次安装在目标机生成配置；管理员和基础服务工具账号默认使用 `admin` / `admin123`，内部密钥独立生成。

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

Linux / macOS 共用 Bash 入口。默认同时输出 `offline-bundles/iot-platform-offline-*` 目录、同名 `.tar` 和 `.tar.sha256`。归档含完整顶层目录及隐藏配置，只需上传归档和校验文件；目录输出仍可用于本机检查或直接部署。`.tar` 不压缩内容，可减少逐个传输文件的开销，但会额外占用约一份部署目录的磁盘空间。只需目录时，Bash 使用 `--skip-bundle-archive`，PowerShell 使用 `-SkipBundleArchive`。GitHub 公开发布流程跳过私有归档，在清除凭据后另行压缩分卷。

包内保留 `README.md` 和完整 `docs/` 目录，安装与维护入口为 `docs/DEPLOYMENT.md`，消防管理说明为 `docs/FIRE_SAFETY.md`。文档中的源码与开发测试入口在源码仓库使用。

在 Linux 服务器上，将归档和校验文件放在同一目录，替换下面的文件名后执行：

```bash
sha256sum -c iot-platform-offline-xxxx.tar.sha256
tar -xf iot-platform-offline-xxxx.tar
cd iot-platform-offline-xxxx
# 升级已有服务：部署前先复制旧包的 .env.offline 到当前目录。
bash scripts/deploy-offline.sh
```

已有目录也可以直接归档，无需重新构建镜像；在源码仓库根目录执行：

```bash
tar -cf offline-bundles/iot-platform-offline-xxxx.tar \
  -C offline-bundles iot-platform-offline-xxxx
cd offline-bundles
sha256sum iot-platform-offline-xxxx.tar > iot-platform-offline-xxxx.tar.sha256
```

手工生成的私有包包含凭据，不作为公开下载包分发；实际管理员密码以包内环境文件为准。`--skip-docker-runtime` 仅用于目标机已有 Docker。

若打包在 Harness 拉取阶段提示 `Your local changes ... would be overwritten by checkout`，且新克隆目录的修改集中于图片、字体等二进制文件，检查 `git --version`；Git 2.10 以前对上游 `text=auto eol=lf` 属性的处理可能触发此问题。拉取脚本通过 `.git/info/attributes` 保留仓库原始字节，在首次检出前设置该覆盖，不修改上游源码。真实源码修改仍会整体备份到 `upstream/deepseek-harness.backup-*`。同步最新 `scripts/fetch-deepseek-harness.sh`（Windows 对应 `scripts/lib/deployment.ps1`）后，可先单独拉取 Harness，再重跑原打包命令；无需删除 Docker 镜像或数据卷。

### 安装与升级

升级前把原 `.env.offline` 复制到新包，保持原项目、数据卷、协议制品和密钥，不能用新配置中的凭据直接连接旧数据库。在包根目录执行：

```bash
sudo bash scripts/deploy-offline.sh
# macOS 使用同一脚本，省略 sudo
```

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\deploy-offline.ps1
```

脚本校验哈希和 CPU 架构、导入镜像、启动并检查服务，使用 `--no-build --pull never`。默认项目为 `iot-platform`，Web 为 `http://服务器IP:8080`。重复部署保留已有配置与数据；不创建模型卷、不下载模型。离线部署包不含业务数据库备份，AI 与知识向量计算仍需联网和相应 API Key。

仅替换 API/Web 镜像时，使用相同标签构建、导出与校验，在原包目录导入后重建 API；若已启用容量模块，同时重建使用该 API 镜像的 `capacity` 服务，再重建 Web（让 nginx 重新解析地址）。此方式不更新 Compose 或 Harness；这些组件变化时交付完整新包，不能再用旧 `images.tar` 覆盖更新。

### Linux 与 openEuler

Linux 自动安装要求 systemd、tar、iptables、xz、ps；健康检查另需 curl。已有 Docker/Compose 可用则复用，不自动更换 daemon 配置、清理数据或升级 Engine。在线补齐缺失工具；离线只从包内 `docker-runtime/` 校验安装。精简发行版须在打包时用 `--docker-packages-dir` / `-DockerPackagesDir` 携带匹配系统、版本和架构的 RPM/DEB 及依赖。

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

**备用媒体服务器**：`IOT_VIDEO_MEDIA_STANDBY_URLS`（逗号分隔的媒体 HTTP API 地址）与等长的 `IOT_VIDEO_MEDIA_STANDBY_IDS`（各自的媒体服务器编号，须与该服务器配置的编号一致且互不重复）配置备用服务器，`IOT_GB28181_MEDIA_STANDBY_IPS` 为各自的国标 RTP 接收地址（缺省取 API 地址中的 IP）。直播模块按“主服务器、备用服务器”的顺序使用第一台健康的服务器：当前服务器健康检查失败（每 15 秒一次）时立即切换，主服务器恢复后连续 3 次健康才切回；只接受当前服务器的 Hook。切换时正在播放的拉流、转码与国标接收全部丢失，播放器下一次心跳在新服务器上重建（与媒体服务重启相同，浏览器需重新连接）。各服务器共用媒体密钥与 Hook 密钥，HLS 代理须按同一顺序选择健康服务器（集群渲染用 HAProxy `balance first`）。单机部署通常不配置。

媒体、Hook、凭据加密密钥由脚本生成并保留；`IOT_VIDEO_CREDENTIAL_KEY` 不能随意更换，否则已存密码无法解密。停止媒体容器保留摄像头资料和配置；平台内业务开关关闭会撤销播放与拉流。功能、权限与生命周期见 [平台功能](PLATFORM.md#摄像头)。

## 容量测试模块

容量测试会给平台施加真实负载，单机在线与离线部署的**新环境默认不部署**（本地源码与集群向导仍默认开启）；需要时用 `--capacity on` 部署，之后不带参数的部署沿用上次选择。开启后运维中心出现“容量测试”页，选择测试类型（快速检查、容量搜索、长稳）并填写设备数、速率和时长即可运行，不需要编写清单、秘密文件或计划。不需要时可以关闭，关闭后页面菜单隐藏，平台其余功能不受影响。在线、离线和集群模块与平台使用同一镜像（`capacity-test serve --self`），只在内部网络监听，不对外发布端口；本地源码模式由 combined API 进程启动本机控制器，不创建容量容器。

| 部署方式 | 关闭 | 重新开启 |
| --- | --- | --- |
| 单机在线 | `bash scripts/deploy-online.sh --capacity off`，或已部署后 `bash scripts/capacity-module.sh disable` | `--capacity on` 或 `capacity-module.sh enable`（新环境默认关闭） |
| 本地源码 | `bash scripts/setup-local.sh --capacity off` | `--capacity on`；修改后重启源码 API |
| 单机离线 | `bash scripts/deploy-offline.sh --capacity off`，或 `bash scripts/capacity-module.sh disable --mode offline` | `--capacity on` 或 `capacity-module.sh enable --mode offline`（新环境默认关闭） |
| 集群 | 向导中回答不部署，或 `bash scripts/cluster-up.sh --name <名称> --capacity off` | `--capacity on` |

PowerShell 使用 `-Capacity on|off` 与 `scripts\capacity-module.ps1 enable|disable`。选择写入环境文件 `IOT_CAPACITY_MODULE`（集群写入清单 `capacity: {node: ...}`），不带参数的部署沿用上次选择；离线包打包时沿用源配置的选择，未设置时写入关闭配置。容器部署开启时自动生成服务令牌 `IOT_OPS_CAPACITY_TOKEN` 并设置 `IOT_OPS_CAPACITY_URL`，关闭时移除服务并隐藏页面，测试结果卷与令牌保留。本地控制器使用进程内生成的令牌和动态本机端口，沿用原有 API 启动命令；旧 `.env.local` 的补充配置见 [本地容量测试](DEVELOPMENT.md#容量测试模块)。

测试以发起人的账号权限运行；操作凭据有效期为计划墙钟预算加 30 分钟，最长 24 小时，受管理账号的权限变更或停用会使凭据失效。页面预设自动准备标准协议测试模板 `cap-standard` 与前缀为 `cap` 的测试设备；启用告警核对时还准备 `cap-stress-alarm` 规则。新测试设备通过 `trial:true` 登记，需要设备登记和模板配置权限，测试不会自动生成模板验收记录。权限、普通登记的验收要求见[预检、保存与诊断](INTEGRATION.md#预检保存与诊断)。测试数据保留以便复测，删除与清理见[测试数据清理](DEVELOPMENT.md#测试数据清理)。测试会给平台施加真实负载，生产环境请在低峰期运行或只用快速检查。

容量控制器代码更新后，本地源码模式重启 API；在线、离线模式重跑原部署命令，同步更新 API 与 `capacity`；集群按原清单升级容量节点。仅刷新页面或重启 API 容器不会更新独立容量服务。先等待当前测试结束或停止测试，再交接控制器进程，沿用原结果目录、配置和数据卷。

## 配置与维护

### 环境与数据

| 方案 | 配置文件 | Compose 文件 | 项目名 / 数据卷前缀 |
|---|---|---|---|
| 本地运行 | `.env.local` | `compose.local.yaml` | `iot-platform-local` |
| 在线部署 | `.env.online` | `compose.yaml` | `iot-platform-online` |
| 离线部署 | 离线包内 `.env.offline` | `compose.yaml` + `compose.offline.yaml` | `iot-platform` |

各入口显式选择上表中的环境文件和 Compose 文件；自定义项目名须在准备、部署和日常维护时保持一致。

**已有部署沿用原项目和凭据。** 新默认项目名会创建一套新数据卷，不会自动迁移旧数据。例如原服务用项目 `iot-platform`、配置 `.env`，在线更新应执行：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\deploy-online.ps1 -EnvFile .env -ProjectName iot-platform
```

```bash
bash ./scripts/deploy-online.sh --env-file .env --project-name iot-platform
```

已有自定义 Compose 覆盖文件、外部数据卷或外部数据库时，先核对原部署参数；上述命令只使用 `compose.yaml`。

### 工具连接账号

首次部署的基础服务工具用户名为 `admin`，密码为 `admin123`，用于 MQTT、Kafka、PostgreSQL、Redis、ClickHouse、RustFS（含灾备）、EMQX 控制台及 Grafana；平台内置管理员使用相同默认值。PostgreSQL 和 ClickHouse 仍保留 `iot` 应用账号，Redis 保留 `default` 应用账号。数据库/缓存工具账号由 `SERVICE_ADMIN_USER` / `SERVICE_ADMIN_PASSWORD` 配置，其他服务沿用各自的账号变量。

这些用户名和密码是连接工具中填写的登录凭据，服务地址见[端口与地址](#端口与地址)：

- MQTT 使用 `IOT_MQTT_TOOL_USERNAME` / `IOT_MQTT_TOOL_PASSWORD`，可发布和订阅；设备及浏览器仍使用 JWT。EMQX 先查内置账号，同名用户密码不符即拒绝，因此管理端订阅凭据的用户名带 `web:` 前缀，与工具账号（默认 `admin`）分开。
- Kafka 选择 `SASL_PLAINTEXT`、`SCRAM-SHA-256`（启用 TLS 时选择 `SASL_SSL`），连接对外 Kafka 端口，填写配置中的用户名和密码。
- ClickHouse 工具账号通过 `GRANT CURRENT GRANTS` 继承初始化连接账号实际可授予的权限。

工具 `admin` 具有服务管理权限。外部业务订阅在[消息主题](INTEGRATION.md#订阅密钥与连接凭据)中按开放接口密钥单独授权；JWT、Harness、备份、EMQX 管理 API、摄像头密钥、集群复制凭据和外部对接临时凭据继续独立生成。

重复执行保留已有环境文件和数据库中的账号，不会把现场密码静默改成新默认。修改已有服务密码需同步服务端账号、环境文件及使用它的平台/备份进程；不要重新生成配置文件或删除数据卷来重置密码。配置文件和私有离线包包含现场凭据，不应提交或公开分享。

单机 EMQX 使用固定节点名，避免容器 IP 变化后切换到新的 Mnesia 数据库。已有部署更换节点名时，先在旧节点通过 `emqx ctl data export --dir <已存在的目录>` 导出配置和账号并备份，再在新节点执行 `emqx ctl data import <导出文件>`。持久配置中的认证列表会覆盖启动基线；旧配置应同步内置密码认证器的 `bootstrap_file` / `bootstrap_type` 并更新工具用户，保留 JWT 认证。仅改变启动变量不会覆盖已有账号密码。

### 数据库迁移

PostgreSQL 仓储启动时先执行 `internal/adapters/postgres/schema.sql` 幂等基线，再按版本号执行 `internal/adapters/postgres/migrations/` 中尚未执行的迁移，结果记录在 `schema_migration`；已执行的迁移被修改时拒绝启动。多个进程同时启动时由会话级 advisory lock 串行，其余进程等待后跳过。沿用原数据库与数据卷，无需清空数据；编写约定见该目录的 README。部署账户须有创建所需表及扩展的权限，外部 PostgreSQL 的 pgvector 要求见 [知识库配置](#知识库向量服务)。

排班、灭火器和消防站随 API 与 Web 提供，无独立容器或模块开关。升级后由迁移创建 `fire_safety_record` 等表并转换已有数据，业务数据仍保存在既有 PostgreSQL；配置和关联约束见 [消防管理持久化](FIRE_SAFETY.md#持久化)。升级前保留数据库备份；平台设备数据导出的覆盖范围见 [设备数据备份](#设备数据备份)。

外部数据接入随 API、Web、Parser、Processor 和 Jobs 提供；启动迁移创建 `external_data_entry`，Jobs 自动恢复推送处理与拉取任务。所有副本需保持 `IOT_JWT_SECRET` 一致以解密接口凭据。FULL 备份包含独立外部接入组件，恢复写入隔离 schema；配置、权限和验收边界见[外部数据接入](EXTERNAL_DATA.md)。

设备接入草稿、批量任务、模板准备、验收及配置历史保存在 `onboarding_record`，同样随启动幂等迁移。接入升级应同步 API、Web 和拆分的 Gateway / Jobs 代码；批量执行由启用 Jobs 职责的进程恢复。流程见[设备接入](INTEGRATION.md#设备接入)，其持久记录不在设备数据导出的范围内。

### AI 与工作流

本地、在线、离线分别使用自己的环境文件。首次可不填 `DEEPSEEK_API_KEY`；在“模型管理”填写并保存（连接测试可选），或写入对应环境文件后重启。“最大输出词元”（128–8192，默认 2048）是智能助手单次回复的默认上限。保存时若有 AI 工作流正在运行或排队，接口返回 409 并提示等待任务结束后重试，本次配置不保存；可在[运行中的 AI 工作流](PLATFORM.md#运行中的-ai-工作流)查看并停止当前租户任务。全部租户的运行及排队任务清空后可重新保存模型；`/health` 的 `activeRuns` 仅统计已开始运行的任务，不含队列。Provider 连接成功、Harness 健康和真实工作流成功分别检查。运行时限：智能助手对话为 `IOT_AI_HARNESS_TIMEOUT`（默认 90s）；业务任务（告警研判、巡检建议、运维报告、协议助手、规则草稿）为 `IOT_AI_HARNESS_BUSINESS_TIMEOUT`（默认 4m），等待 Harness 空闲最多另计 2 分钟。Harness 的 `IOT_HARNESS_RUN_TIMEOUT_MS`（默认 300000）只作上限，须大于以上两项；单次模型请求上限 `IOT_HARNESS_RPC_TIMEOUT_MS` 默认 240000。平台到时即断开，Harness 随之停止该任务。Harness 必装，默认模型和固定版本以部署配置及 `deploy/deepseek-harness/REVISION` 为准。

Harness 源码（`deploy/deepseek-harness/`）变化时须重建 Harness 并重启 API，例如升级 AI 工作流运行管理。依赖机与源码机分离时，先把最新源码同步到依赖机的原仓库，在依赖机仓库根目录执行以下命令，再重启源码 API。重建 Harness 会中断该实例当前任务；`.env.local` 和命名卷继续沿用。

```bash
sudo docker compose -p iot-platform-local --env-file .env.local \
  -f compose.local.yaml up -d --no-deps --build deepseek-harness
```

源码 API 到 Harness 使用 `IOT_AI_HARNESS_URL/TOKEN`；容器回调使用 `IOT_AI_HARNESS_MCP_URL`，必须能到达源码 API，OrbStack 为 `host.orb.internal`。旧 Provider 数据、IDE 进程变量可能覆盖环境文件，排查时核对实际运行配置。工作流和权限见 [平台功能](PLATFORM.md#ai-与知识库)。

### 知识库向量服务

对话模型（DeepSeek 等外部 API）与向量模型独立配置。向量计算和检索重排默认由随平台部署的两个服务完成，部署平台时一并构建和启动，无需额外机器或密钥：

| 服务 | 模型 | 作用 |
|---|---|---|
| `embedding` | bge-m3（Q8_0，1024 维） | 把知识分片和检索问题转换为向量 |
| `reranker` | bge-reranker-v2-m3（Q8_0） | 对检索候选按相关性重新排序 |

两者使用同一镜像 `deploy/local-ai`（llama.cpp 固定版本 b11382，按 digest 固定的多架构基础镜像，支持 **linux/amd64 与 linux/arm64**，只用 CPU）。模型在构建镜像时从固定版本下载并按 SHA-256 校验，运行时和离线环境都不再下载；镜像约 1.5 GB，每个服务常驻约 1 GB 内存。huggingface.co 不可达时设置 `IOT_HF_ENDPOINT=https://hf-mirror.com`；ghcr.io 不可达时把 `IOT_LLAMA_CPP_IMAGE` 指向镜像仓库中的同一镜像（保留 digest）。`IOT_LOCAL_AI_THREADS`（默认 -1，即全部核）可限制推理线程，主机繁忙时调小。

- 在线部署：`deploy-online` 构建并启动两个服务。离线包按打包机的架构构建并包含该镜像（`manifest.json` 中 `embeddingRequiresInternet=false`），目标机须与打包机架构一致。
- 源码调试：`compose.local.yaml` 把两个服务发布在依赖机的 `18093`（embedding）和 `18094`（reranker），准备脚本写入 `IOT_EMBEDDING_URL`、`IOT_RERANK_URL` 并把依赖机地址加入 `IOT_LOCAL_AI_HOSTS`。
- 集群：每个 API 节点在本机 `127.0.0.1:18093/18094` 运行一组，API 只访问本机实例；镜像键为 `images.localAI`，旧清单缺省时取默认值。

| 配置 | 默认或说明 |
|---|---|
| `IOT_EMBEDDING_URL` | `http://embedding:8080/v1`；也可填外部 HTTPS OpenAI 兼容 API |
| `IOT_EMBEDDING_MODEL` | `bge-m3` |
| `IOT_EMBEDDING_API_KEY` | 仅外部 API 需要；默认空 |
| `IOT_EMBEDDING_DIMENSIONS` | `1024`，须与模型实际输出一致（bge-m3 固定 1024） |
| `IOT_EMBEDDING_BATCH_SIZE` | `10` |
| `IOT_EMBEDDING_QUERY_INSTRUCTION` | 默认空；仅为需要查询前缀的模型配置 |
| `IOT_RERANK_URL` | `http://reranker:8080`；置空则不重排 |
| `IOT_RERANK_TIMEOUT` | `8s`，超时或失败时保留原排序 |
| `IOT_LOCAL_AI_HOSTS` | `embedding,reranker`；只有这里列出的主机名、IP 或网段允许使用 HTTP 且无需密钥，其他地址必须是外部 HTTPS API |

“模型管理”的知识库 Embedding 卡片显示当前是否使用本地服务，可一键“切换为本地向量服务”或改填外部 API；环境文件未指向本地服务时卡片提示未启用，不提供切换。卡片同时只读显示检索重排状态（本地、外部或未启用），重排由 `IOT_RERANK_URL` 决定，修改后重启平台生效；页面保存的配置持久化到 PostgreSQL，优先于环境文件默认值。早期版本默认使用 DashScope 云端 API：部署脚本会把仍为该默认值的环境文件改为本地服务；若曾在“模型管理”保存过云端配置，须在该页切换。改变向量服务地址、模型、维度或查询指令会在后台重建索引，建好后原子切换，失败继续使用旧索引；仅更新密钥不改变向量空间。

检索与索引的容错：

- 检索问题最长 512 字；业务功能使用各自的简短检索问题，不把设备快照等数据当作问题。
- 检索时向量计算限时 10 秒；向量服务不可用时退回关键词检索，结果标记为“仅关键词匹配”。重排每次最多看 20 条候选、每条前 400 字（CPU 上约数秒），失败或超时保留混合排序。
- 建索引对连接失败、429、5xx 重试，遵守 `Retry-After`；临时失败按 1、5、15、60 分钟自动重新排队，用完才标记索引失败，等待中的文档可手动立即重试。CPU 上大文档建索引较慢，单个任务上限 2 小时（每批完成都会续租）。

PostgreSQL 17 镜像包含固定版本 pgvector 0.8.1，沿用原 PostgreSQL 数据卷。API 迁移创建 `vector` 扩展及知识索引表；外部 PostgreSQL 须预先安装 pgvector，并由具备权限的账户执行扩展创建。知识原件继续保存在对象存储（RustFS），文档、分片、向量、Agent 绑定及索引版本存于 PostgreSQL。上传、删除、重试、原子重建及检索授权统一见[知识库使用](PLATFORM.md#ai-与知识库)；多副本共享 PostgreSQL 重建锁。

### 运维组件

单机在线与离线部署的监控组件（Prometheus、Loki、Alloy、Grafana、Alertmanager、node-exporter，Compose profile `ops`）默认部署；`--ops off`（PowerShell `-Ops off`）移除这些服务并清空运维中心的组件地址（页面显示未部署），之后不带参数的部署保持关闭，`--ops on` 恢复。开关写入 `IOT_OPS_MODULE`，离线包始终包含监控镜像。关闭后平台接入、告警和通知不受影响，但不再有指标、日志检索与 Alertmanager 告警（包括死信、消费阻塞、通知失败等平台自身告警），正式环境建议保留或接入已有监控。

`--dependencies-only` 包含运维基础环境，普通本地准备可加 `--include-ops` / `-IncludeOps`。源码与容器共用 `IOT_LOCAL_OPS_DIR`（默认 `data/ops`）；源码 API 须能写、组件须能读。普通远程虚拟机没有共享目录时，规则与通知配置为只读。将 `IOT_OPS_TENANTS` 设置为可授权运维的租户；Grafana 告警关闭，统一使用 Alertmanager。

### ClickHouse

单机在线与离线部署默认带 ClickHouse（Compose profile `clickhouse`），承载高频原文与遥测。`--clickhouse off`（PowerShell `-ClickHouse off`）移除 `clickhouse` 与 `clickhouse-tool-admin` 服务并把 `IOT_CLICKHOUSE_URL` 置空，平台改为把原文与遥测全部写 PostgreSQL（属性历史改用 PostgreSQL 查询）；之后不带参数的部署保持关闭，`--clickhouse on` 删除空地址并恢复内置服务地址（手工填写的外部地址保留）。开关写入 `IOT_CLICKHOUSE_MODULE`，离线包始终包含 ClickHouse 镜像。关闭只停止服务、不删除数据卷，已存入 ClickHouse 的高频原文、遥测历史与属性上报的属性在关闭期间不可读，重新开启后恢复。设备量小、上报频率低的场景可关闭以节省内存；高频接入或长期遥测查询建议保留。升级旧部署须重跑部署脚本，让 `COMPOSE_PROFILES` 加上 `clickhouse`，直接执行 `docker compose up` 会因缺少 ClickHouse 服务而使 API 启动失败。

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

摄像头模块的开关见[摄像头部署](#摄像头部署)；`scripts/video-module.* status|logs` 支持相应环境的状态和日志查询。启用后 `COMPOSE_PROFILES` 包含 `video`，上述 `ps`、`logs` 会一并列出 `zlmediakit`。

`down` 保留命名数据卷，`down -v` 会删除它们。日常代码更新重跑对应部署脚本；备份范围与调度见 [设备数据备份](#设备数据备份)。

## 容量相关配置

本节维护连接池、并发和限额配置；拓扑、角色与迁移见 [集群部署](#集群部署) 和 [进程职责](#进程职责)，实测流程见 [容量验证](DEVELOPMENT.md#容量验证)。

下表是配置默认值，实际容量按目标环境测量；多副本需核对每个进程的连接与并发预算。

| 配置 | 默认 | 说明 |
| --- | --- | --- |
| `IOT_POSTGRES_MAX_CONNS` | 64 | 每个进程（含各 Worker 角色）的 PostgreSQL 连接池；所有进程之和须小于服务端 `max_connections` |
| `POSTGRES_MAX_CONNECTIONS` | 300 | Compose 中 PostgreSQL 的 `max_connections` |
| `IOT_KAFKA_CONSUMER_CONCURRENCY` | 64 | 每个 Kafka 订阅的并行通道，同一设备保持顺序 |
| `IOT_CLUSTER_INSTANCES` | 1 | 共享限额存储（Redis）不可用时，各进程按“额度 ÷ 实例数”退化执行，避免总额度放大 |
| `IOT_INGEST_MAX_BACKLOG` | 50000 | 解析与业务流（`processor` 组）积压超过该值时暂停接收新原文，0 关闭 |
| `IOT_PROTOCOL_LISTENER_MAX_SESSIONS` | 20000 | 每个 TCP / UDP 接入监听的会话上限；每个会话占一个文件句柄，Compose 为平台进程设置 `nofile` 1048576，自行部署时须同样放开；拒绝新连接时计入 `protocol_listener_rejected_total` 并触发 `ProtocolListenerFull` 告警 |
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
```

输出区分配置预算通过、阻塞与未验证。它读取实际 PostgreSQL 最大连接数、Kafka 分区/副本、ClickHouse 表引擎及 MQTT 会话可观测性；存储分片、磁盘接管、连接路由、Harness 并发和模型供应商请求 / token 配额仍须在目标集群验证。`clusterCapacityVerified=false` 始终保留，不能把 API 进程数乘以单机速率当作最高容量。

### Kafka 对接账号认证与授权

“消息主题”的 Kafka 订阅凭据由平台管理 Redpanda SCRAM 凭据及精确 ACL。新建部署默认启用 SASL 与 Admin API 认证，并初始化 `admin` / `admin123`；平台页面仍检查 Broker 的实际状态，未满足下列条件时拒绝发放 Kafka 连接凭据。已有 Broker 的账号和集群配置存于数据卷，更新环境变量不会替换已有密码，需按下述步骤同步。

| 配置 | 用途 |
| --- | --- |
| `IOT_KAFKA_PUBLIC_BROKERS` | 返回对接方的 Kafka 地址列表，逗号分隔的 `host:port`；必须是对接方可达、已开启 SASL 的 listener，留空不发放 Kafka 连接凭据 |
| `IOT_KAFKA_SASL_USERNAME` / `IOT_KAFKA_SASL_PASSWORD` | 平台 API、Worker、容量检查及死信回放连接 Kafka 的服务账号，须成对填写 |
| `IOT_KAFKA_SASL_MECHANISM` | 服务账号机制，默认 `SCRAM-SHA-256`，也支持 `SCRAM-SHA-512` |
| `IOT_KAFKA_TLS` / `IOT_KAFKA_TLS_CA_FILE` | Kafka TLS 开关与可选 CA 文件；CA 为空时使用系统信任库，开启后校验服务端证书和主机名，不提供跳过校验选项 |
| `IOT_KAFKA_ADMIN_URL` | Redpanda Admin API 根地址，不带 `/v1`；必须是平台进程可达地址 |
| `IOT_KAFKA_ADMIN_USERNAME` / `IOT_KAFKA_ADMIN_PASSWORD` | 管理账号（默认与服务账号相同），用于 HTTP Admin 与 Kafka ACL 管理，不返回浏览器；SCRAM 机制与服务账号一致 |

源码启动使用相应环境文件。在线/离线 Compose 已透传上述变量；自签名 CA 文件还须通过部署覆盖配置挂载到容器内，并填写容器内路径。Admin API 为 HTTPS 时复用该 CA 信任配置。容器内 `IOT_KAFKA_BROKERS` 使用内部地址，`IOT_KAFKA_PUBLIC_BROKERS` 使用对接方可达地址。平台会检查两组地址及 Broker 返回的广告地址属于同一个非空 `clusterId`，并拒绝匿名连接；两组 listener 使用相同的 SASL 机制与 TLS 配置。不要将匿名 listener 暴露给消费者。

在既有 Redpanda 开启认证前，先安排平台进程切换使用服务账号，创建管理账号并加入 `superusers`，保留可恢复的管理入口。按 [Redpanda 25.2 认证说明](https://docs.redpanda.com/streaming/25.2/manage/security/authentication/) 完成以下步骤：

1. 创建 SCRAM 管理账号和平台服务账号。管理账号须能管理用户、创建主题、读写 ACL；普通服务账号需对平台 `iot.` 主题拥有实际运行所需的发布、消费、查询及容量清理权限，对 `iot-platform-` 消费组拥有读写位点与查询权限。先配置平台及命令工具的 SASL 参数，再切换 Broker；不要把已有数据库或消息卷重建作为切换认证的手段。
2. 默认使用 `rpk cluster config set enable_sasl true` 为所有 Kafka listener 开启 SASL，`kafka_enable_authorization` 保持默认值；不要混用全局开关与按 listener 配置的两套认证方案。若选择每个 listener 单独配置，应同时设置 `authentication_method: sasl` 和对应授权开关，并执行该方案要求的 Broker 重启。
3. 使用 `rpk cluster config set admin_api_require_auth true` 保护 Admin API，后续 `rpk` 操作使用已建立的管理身份。外部网络部署配置 Kafka TLS，并保护 Admin API 的访问网络和传输。应用环境文件中的凭据须与 Broker 中实际创建的账号相符；填写环境文件本身不会创建账号。
4. 重启使用新配置的平台进程并复查数据接入。消息主题授权会读取 Broker 实际授权配置，检查管理连接、每个配置及广告地址拒绝匿名请求，且拒绝存在 `User:*` 通配授权的环境；任一检查失败不发放连接凭据。

受管消费用户名及消费组采用 `iot-topic-` 命名空间，凭据固定使用 `SCRAM-SHA-256`。每个账号仅获得所选 `iot.external.<租户编码>.` 主题的 `READ` / `DESCRIBE` 和其专属消费组的 `READ`；不授权共享默认主题、内部队列、发布或任意消费组。新主题使用 Broker 默认分区和副本数，已存在主题的分区、副本和保留策略保持原值。授权失败会撤销该账号的 ACL 和凭据；撤销先删除 ACL 再删除 SCRAM 用户，使已有认证连接也失去读权限。账号撤销和页面删除发布配置均不清除 Kafka 主题历史数据，保留策略由 Broker 管理。具体 ACL 语义见 [Redpanda ACL 文档](https://docs.redpanda.com/streaming/25.2/manage/security/authorization/acl/)。

## 集群部署

多节点部署由**集群清单**统一描述，`scripts/cluster-up.sh` / `.ps1` 一条命令完成镜像、秘密、渲染、节点预检、下发、按阶段启动、初始化与就绪检查；其中 `cmd/cluster-render` 为每个节点生成独立的 Compose 项目（主机网络、固定端口），`scripts/cluster-deploy.sh` / `.ps1` 按阶段下发与启动。Compose 只管理本节点；跨节点布局、故障域和连接预算由清单校验。

| 组件 | 集群形态（示例清单 `deploy/cluster/inventory.example.yaml`） |
| --- | --- |
| Redpanda | 3 节点，业务主题与死信主题复制因子 3，关闭自动建主题 |
| PostgreSQL | Spilo（Patroni）3 成员 + etcd 3 节点，可选同步备库；平台使用多主机 DSN `target_session_attrs=read-write` 连接当前主库，历史查询使用 `prefer-standby` 只读 DSN |
| ClickHouse | 2 分片 × 2 副本 + Keeper 3 节点，`*_local` 复制表与同名 Distributed 表，插入按法定副本确认 |
| Redis | 主 + 2 副本 + Sentinel 3 个，平台经 Sentinel 跟随主节点 |
| EMQX | 3 节点静态集群 |
| 平台 | api、gateway、parser、processor、jobs 各自多实例；API 之间选举视频控制实例；Harness 多实例按会话路由 |
| 监控 | Prometheus 按实例抓取所有平台进程、Redpanda、EMQX 与各节点 node-exporter，加载与单机相同的平台告警规则；同节点 Alertmanager（9093）接收告警，运维中心可查看 |
| RustFS | 示例为单实例；`rustfs.nodes`（至少 4 个节点，每节点一个数据目录）渲染分布式纠删码部署，任一节点故障时读写可用，平台与备份经本机 HAProxy `127.0.0.1:18183` 访问健康节点。RustFS 拒绝同一块盘上的多个数据目录，3 节点单盘无法组成可用集群 |
| 视频媒体 | 示例为单实例；`video.nodes` 第一个为主媒体服务器、其余为备用，直播模块与 HLS 代理（HAProxy `127.0.0.1:18180`，`balance first`）都使用第一台健康的服务器，详见 [备用媒体服务器](#摄像头部署) |
| 监控高可用 | `monitoring.nodes`（≥2）在每个节点运行独立的 Prometheus 副本（相同抓取目标，外部标签 `replica`）和组成集群的 Alertmanager（9094 互联）；各副本向全部 Alertmanager 发送告警并去掉 `replica` 标签，由集群去重与共享静默。运维中心经 HAProxy `127.0.0.1:18190` / `18193` 访问健康实例，两个副本的历史数据各自独立 |

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
4. **服务统一密码**：用于数据库/缓存工具账号、PostgreSQL 应用、Redis、ClickHouse、RustFS、MQTT、Kafka、EMQX 控制台和平台管理员；直接回车使用 `admin123`，工具用户名默认 `admin`。自定义密码至少 8 位，只能包含字母、数字与 `. _ ~ -`。JWT、Harness、摄像头、服务间令牌以及 PostgreSQL 超级用户与复制凭据仍独立生成。默认值只填充缺项；再次执行保留已有秘密，显式指定与现有密码不同的值会拒绝，已有账号密码需单独同步。
5. 是否部署摄像头直播模块；DeepSeek API Key（可留空，部署后可在“模型管理”填写）。

节点布局按节点数自动生成到 `.cluster/<名称>/inventory.yaml`：每个节点视为独立故障域；etcd、PostgreSQL、Redpanda、EMQX、Redis 与 Sentinel 放在前 3 台；ClickHouse 3 台时为 1 分片×3 副本，4–5 台为 2×2，6 台及以上为 2×3；RustFS、视频、容量测试、备份与监控放在最后一台；api、gateway、jobs、Harness、Web 各 2 个实例，parser、processor 各 3 个。可以手动修改该文件后重新执行。已有自写清单时用 `--inventory <文件>`（首次同样询问 SSH 密码）；自有私钥用 `--ssh-key <文件>`，此时不安装部署密钥。

无人值守：`bash scripts/cluster-up.sh --name <名称> --nodes IP1,IP2,IP3 --yes`，SSH 密码与服务统一密码经环境变量 `TORCHLINK_SSH_PASSWORD`、`TORCHLINK_SERVICE_PASSWORD` 提供（PowerShell 为 `-Name`、`-Nodes`、`-Yes`）。

脚本依次完成：

1. **镜像**：用当前源码构建平台、Web、Harness、备份、PostgreSQL pgvector 与媒体镜像（清单中写成 `镜像@sha256:` 的改为拉取），拉取其余第三方镜像。控制机不需要 Go：渲染、初始化与 SSH 准备工具随平台镜像提供。
2. **SSH 与秘密**：按上述方式安装部署密钥；在 `.cluster/<名称>/secrets.yaml`（0600，已被 Git 忽略；可用 `--secrets` 指定）生成缺失的密码与令牌，设置了服务统一密码的项使用它，已有值保持不变。`deepseekApiKey`、`embeddingApiKey` 和独立恢复目标配置需要时自行填写，也可部署后在“模型管理”配置模型密钥。
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

知识索引复用 PostgreSQL HA 集群，Spilo/Patroni 镜像同样包含固定版本 pgvector；各管理 API 使用同一外部 Embedding 配置及重建锁。Harness 节点保留动态 Agent 和会话持久化，备份通过受控内部 snapshot 接口收集各实例；向量 API Key 在集群秘密文件中自行填写，脚本不生成虚假的云服务密钥。

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
- Broker 服务与管理账号首次默认 `admin` / `admin123`；需要自定义时，在秘密文件填写 `kafkaSaslUsername` / `kafkaSaslPassword`、`kafkaSaslMechanism`（默认 `SCRAM-SHA-256`）、`kafkaTls` 及可选 `kafkaTlsCaFile`；Redpanda 管理接口使用成组的 `kafkaAdminUrl` / `kafkaAdminUsername` / `kafkaAdminPassword`。这些参数统一进入所有平台角色及 `init.env`，不要在清单 `env` 中重复设置。使用一键 `cluster-up` 时，把 PEM CA 证书放在秘密文件目录或其子目录内，填写相对路径（以秘密文件目录为准）；直接运行 `cluster-render` 也支持本机绝对路径。渲染器复制证书，各角色和远程 `cluster-init` 只读挂载，本地初始化自动使用控制机上的副本。证书文件不得包含私钥。
- **告警**：监控节点的 Prometheus 加载平台告警规则（`ops/prometheus/alerts.yml` 内嵌进渲染器，平台进程的 job 名在集群中为 `platform`），Alertmanager 与其同节点、只监听 9093 且不组集群。秘密文件填写 `alertWebhookUrl`（http/https）后所有告警（含恢复）推送到该地址，否则只在 Alertmanager 与运维中心可见。清单未写 `images.alertmanager` 时使用 `prom/alertmanager:v0.34.1`。
- **HTTPS 与 MQTTS**：秘密文件填写 `tlsCertFile` 与 `tlsKeyFile`（PEM，路径规则同 `kafkaTlsCaFile`，证书与私钥须匹配）后，渲染器把它们复制到各 Web 与 EMQX 节点的 `tls/` 目录：Web 开启 8443 并把 8080 跳转到 HTTPS（`IOT_WEB_TLS_REDIRECT=false` 可关闭跳转），EMQX 开启 MQTTS 8883 与 WSS 8084；部署计划列出 `web-https`、`mqtts` 入口。未配置时保持明文入口。与单机相同，私钥以 0644 写出，供容器内非 root 用户读取；渲染目录本身已含各节点秘密，须按秘密保管。
- `kafkaPublicBrokers`（逗号分隔的 `host:port`）和 `mqttPublicUrl` 默认由 Broker 节点地址生成；若对接方通过域名、代理或 TLS 端口连接，应显式覆盖并与 Broker 广告地址一致。`mqttToolUsername` / `mqttToolPassword` 配置独立 MQTT 工具账号。首次启动会初始化工具账号和认证；已有数据卷须核对实际账号，参见[Kafka 对接账号认证与授权](#kafka-对接账号认证与授权)。
- `cluster-deploy` 不负责镜像：节点需已有镜像，或用 `--images <归档>` 让每个节点整体导入。`--stage`、`--nodes` 可只执行指定阶段和节点，`--dry-run` 只打印命令；`--cluster-init "go run ./cmd/cluster-init"` 改为在控制机本地运行初始化。
- 部署顺序与每阶段内容同上一小节第 6 步。

**扩缩容与升级**：修改清单后重新执行 `cluster-up`；新节点需先满足预检要求。只想操作部分节点时，用 `cluster-deploy` 对变化的阶段和节点执行，例如 `--stage workers --nodes n5`。扩容前先看渲染输出的连接预算。升级按 workers → edge 逐角色滚动；processor/parser 缩容时进程先停止领取，未完成消息的领取租约到期后由其他实例接管。Redpanda、ClickHouse 扩容涉及数据重分布，按组件文档限制重建流量，并把重建期间纳入容量测试。

回滚命令见 [一键部署与升级](#一键部署与升级)。切回配置不会回退已写入的数据，存储迁移需保留备份点与核对记录。

**从单节点迁移**：

1. 在单节点上排空 `parser`/`processor` 积压，停止设备接入或让设备保留未确认报文，记录 PostgreSQL 备份点与 Kafka 偏移。
2. 渲染并按阶段启动集群到 init 完成；PostgreSQL 数据用 `pg_dump`/`pg_restore` 导入新主库后再运行 `cluster-init -execute`。
3. ClickHouse 用迁移工具按月分区回填并核对（默认只输出计划）：

   ```bash
   go run ./cmd/clickhouse-migrate -source <单节点 URL> -target <集群节点 URL> -cluster iot_cluster
   ```

   计划无误后追加 `-execute`；部分填充的目标分区会被阻止，须检查后删除再重跑。核对使用行数、唯一消息数与消息 ID 校验和。
4. 启动 workers 与 edge，用少量设备核对原文、解析、状态、告警与权限，再切换入口并逐步放量。旧环境保留到新集群通过核对与容量快速回归。

渲染与脚本的仓库内验证覆盖清单校验、端口与连接预算、生成文件的 `docker compose config` 解析、部署脚本的阶段顺序（`--dry-run`），以及用模拟的 `docker`/`ssh` 走完一键部署全流程（首次部署、升级、预检拦截、离线归档）；目标机上的真实启动、Patroni/Sentinel/Keeper 实际选主与切换须在目标环境演练并记录。

## 高可用边界

默认 Compose（本地、在线、离线）是**单节点**配置：PostgreSQL、ClickHouse、Redis、Redpanda、EMQX、RustFS、Harness 与 API 各运行一个实例，Redpanda 主题创建为 `--replicas 1`。它可以承担单机生产，但不具备高可用：

- 容器自动重启只在进程退出后拉起同一实例，不能在宿主机、磁盘或数据卷故障时切换。
- MQTT 持久队列（`IOT_DATA_DIR/mqtt-inbox/`）保证已确认报文在本机磁盘上重启后可继续处理，不复制到其他节点。
- 备份用于事后恢复数据，恢复需要停机与人工操作，不是故障切换；备份存在不等于已验证可恢复。
- 拆分 `api` / `gateway` 与多副本 API 只分担接入和查询，前提是数据库、消息与对象存储本身可用。
- 运维中心依赖（`--profile ops` 的 Prometheus、Loki、Grafana、Alertmanager）同样各一个实例；它们停止时接入与告警链路不受影响，但期间的监控数据、日志与告警通知会缺失。

单机部署无法靠增加配置变成高可用：所有组件与数据都在一台宿主机上，宿主机或磁盘故障时只能在新机器上恢复（见 [运维手册](OPERATIONS.md#故障切换)）。需要高可用时使用上一节的 [集群部署](#集群部署)：Redpanda、PostgreSQL、ClickHouse、Redis、EMQX 与各平台角色均为多实例；RustFS（`rustfs.nodes` 分布式）、视频媒体（`video.nodes` 主备）与 Prometheus / Alertmanager（`monitoring.nodes`）也可多实例，示例清单为节省资源仍各放一个节点，按需改为多节点。视频媒体切换会中断正在播放的流并由播放器重建；已部署的单实例 RustFS 改为分布式时新集群从空盘开始，须先用 `rclone sync` 等工具迁移桶数据；Prometheus 副本之间不复制历史数据。知识索引随 PostgreSQL HA 集群保存。节点故障与切换须在目标环境演练，仓库内只验证渲染与部署编排。

### 常见排查

| 现象 | 检查入口 |
| --- | --- |
| 地址或密码似乎未生效 | IDE 进程变量优先；确认原环境文件、Compose 项目和已有数据库密码 |
| API 端口或 MQTT 收件箱已被占用 | 按[进程交接](#本地-api-进程交接)核对并正常退出旧实例，不删除锁文件或数据 |
| API 存活但业务不可用 | `/health/ready`、消费者积压、数据库及 `docker compose logs`；live 只表示进程存活 |
| 镜像或制品哈希不符或缺失 | 在有网机器重做完整包，不在离线目标机拉取 |
| Harness `RUNTIME_ERROR` | 用镜像内 `runtime-smoke.mjs` 检查运行用户依赖权限，再分别检查 Key、模型请求和 MCP 回调 |
| 媒体不可播放 | `/api/v1/video/status`、连接测试、目标白名单、RTC 地址、编码及播放权限 |
| openEuler 镜像导入 `mknod` 失败 | 检查 `container-selinux`、受管程序/数据标签和 Docker 进程域 |

## 协议运行器

`protocol-runner` 与平台使用同一镜像（`IOT_PROCESS_ROLE=protocol-runner`），在线/离线 Compose 默认部署，集群为每个运行 api、gateway 或 parser 的节点渲染一个。平台进程设置 `IOT_PROTOCOL_SANDBOX=runner` 与 `IOT_PROTOCOL_RUNNER_SOCKET=/run/torchlink/runner.sock`，运行器未就绪时协议上传、试跑与 Go 协议解析返回错误，不会退回进程内执行。资源上限用 `IOT_PROTOCOL_RUNNER_MEMORY`（默认 2g）、`IOT_PROTOCOL_RUNNER_CPUS`（2）、`IOT_PROTOCOL_RUNNER_PIDS`（512）调整。隔离与依赖限制见 [Go 协议](INTEGRATION.md#上传与发布)。

## HTTPS 与 MQTTS

在线与离线部署在 Compose 文件所在目录的 `tls/`（`IOT_TLS_DIR` 可改）中查找 `tls.crt`（含完整证书链）和 `tls.key`：

- 存在时 Web 在 `8443`（`IOT_WEB_HTTPS_PORT`）提供 HTTPS 并对其余 HTTP 访问返回 308 跳转（`IOT_WEB_TLS_REDIRECT=false` 关闭跳转；`/health/` 仍走 HTTP 供健康检查），响应带 HSTS；EMQX 启用 MQTTS `8883`（`MQTTS_PORT`）与 WSS `8084`（`MQTT_WSS_PORT`）。
- 不存在时只提供 HTTP / MQTT，EMQX 的 TLS 监听保持关闭（不使用 Broker 自带的演示证书）。
- 正式环境建议使用受信任 CA 签发的证书；测试或内网可生成自签名证书，设备和浏览器需导入信任：

```bash
bash scripts/generate-tls-cert.sh --host <平台域名或IP> [--host <其他地址>]
```

Windows 使用 `scripts/generate-tls-cert.ps1 -HostName <地址>`（需要 openssl）。生成或更换证书后重跑部署脚本或重启 `platform-web`、`emqx`。设备 HTTP 上报的 API 端口 `8081` 与 Gateway 端口仍为 HTTP，对外暴露时建议由前置负载均衡终止 TLS；集群部署同样由外部负载均衡终止 TLS。证书私钥需对容器内的非 root 用户可读，请限制 `tls/` 目录所在主机的访问。

## 数据保留与清理

设备上报、标准消息、告警和日志会持续增长。Jobs 职责的进程（`combined` 或拆分的 `jobs`）每天在 `IOT_RETENTION_TIME`（默认 03:30，时区 `IOT_RETENTION_TIMEZONE`，默认沿用 `IOT_BACKUP_TIMEZONE`）以集群单例删除超过保留期的数据，每批 `IOT_RETENTION_BATCH_SIZE`（默认 5000）行并在批间暂停，不长时间锁表。`IOT_RETENTION_ENABLED=false` 关闭清理。

| 数据 | 存储 | 默认保留 | 配置 | 不清理的行 |
| --- | --- | --- | --- | --- |
| 已处理标准消息 | PostgreSQL | 90 天 | `IOT_RETENTION_STANDARD_DAYS` | 尚未处理完成 |
| 原文索引、低频原文 | PostgreSQL | 180 天 | `IOT_RETENTION_RAW_DAYS` | 尚未发布到消息队列 |
| 原文去重预约 | PostgreSQL | 7 天 | `IOT_RETENTION_RESERVATION_DAYS` | — |
| 设备状态变更事件 | PostgreSQL | 90 天 | `IOT_RETENTION_STATE_EVENT_DAYS` | — |
| 告警记录 | PostgreSQL | 1095 天 | `IOT_RETENTION_ALARM_DAYS` | 活动、已确认告警 |
| 审计日志 | PostgreSQL | 1095 天 | `IOT_RETENTION_AUDIT_DAYS` | — |
| AI 工具调用日志、AI 运行记录 | PostgreSQL | 180 天 | `IOT_RETENTION_AI_LOG_DAYS` | — |
| 视频平台告警事件 | PostgreSQL | 1095 天 | `IOT_RETENTION_VIDEO_EVENT_DAYS` | — |
| 遥测 | ClickHouse 表 TTL | 365 天 | `IOT_RETENTION_TELEMETRY_DAYS` | — |
| 高频原文 | ClickHouse 表 TTL | 180 天 | `IOT_RETENTION_CLICKHOUSE_RAW_DAYS` | — |

天数为 0 表示永久保留。正式保留期按消防监控相关规范和合同要求确定。设备、模板、规则、用户、消防管理等业务资料不在清理范围。

- `IOT_RETENTION_REQUIRE_BACKUP=true` 时，标准消息、原文索引和低频原文按天清理，只删除已有成功 `DEVICE_DAILY` 备份覆盖的日期或成功 `FULL` 备份开始之前的数据；未覆盖的日期保留并计入 `retention_unbacked_days_<表>`。
- 配置 ClickHouse 时，属性上报与告警上报（`PROPERTY_REPORT`、`ALARM_REPORT`）的属性只存 ClickHouse `iot_telemetry`（`properties` 列供按属性查询，`properties_text` 保留原样 JSON 文本），PostgreSQL `standard_message` 只保留索引、处理状态、事件和标签列；消息详情、设备消息列表和原文关联解析结果由平台从 ClickHouse 补回属性。启用前写入的行保留原有属性。遥测保留期短于标准消息保留期时，超出遥测保留期的标准消息不再显示属性；升级时 API 自动为已有 `iot_telemetry` 补 `properties_text` 列。
- ClickHouse 在 API 启动时设置表 TTL，已应用的天数记在表注释中，重启不重复修改；不重写已有数据片段，按月分区整体到期后在后台删除，因此实际保留最多比配置多一个月。
- 指标：`retention_deleted_total`、`retention_deleted_<表>_total`、`retention_failed_total`、`retention_last_success_timestamp_seconds`；Prometheus 规则 `RetentionFailures` 在一天内出现失败时告警。
- 首次升级时迁移 `0001_retention_indexes` 以 `CREATE INDEX CONCURRENTLY` 为大表补时间索引，不阻塞写入，但大表上需要较长时间；建议在低峰升级。建索引中途失败留下的无效索引会在下次启动时自动删除并重建。

### 按月分区

原文索引 `raw_archive_index`、低频原文 `raw_message_log`、标准消息 `standard_message`、设备状态事件 `device_state_event` 和审计日志 `audit_log` 按服务端时间（原文接收时间、标准消息写入时间 `created_at`、事件或审计时间）以 UTC 自然月分区。保留任务先整体删除已过期月份的分区（`DROP TABLE`，不逐行删除），再按上表逐行清理剩余数据；`IOT_RETENTION_REQUIRE_BACKUP=true` 时只删除整月均有备份覆盖的分区。仍有未发布原文或未处理标准消息的分区保留，待其完成后再删。

- **升级方式**：迁移 `0010_partition_prepare` 在不阻塞写入的情况下为每张表建立包含分区键的唯一索引，并校验“所有已有行早于切换点”的约束（切换点为下下个月 1 日 UTC）；迁移 `0011_partition_large_tables` 在一个事务内把原表改名为 `<表>_legacy` 并作为切换点之前的分区挂上，不复制、不重扫数据，只短暂持有表锁（超过 60 秒拿不到锁则本次启动失败，下次重试）。大表首次升级的耗时主要在建索引和校验约束，建议低峰进行。
- **旧数据**：`_legacy` 分区中的数据继续按天逐行清理，清空后自动删除。之后的月份各自成表，另有 `_default` 分区兜底；Jobs 进程每天提前创建本月及之后 3 个月的分区（不受 `IOT_RETENTION_ENABLED` 影响），失败计入 `partition_maintenance_failed_total`。
- **消息去重**：分区表主键包含分区键，消息编号的唯一性由写入时的检查保证：原文仍以 `raw_ingest_reservation` 预约，标准消息在 `standard_message_key` 登记（保留期同原文去重预约）并检查各分区是否已有同一编号。
- **滚动升级**：旧版本的保留任务不识别分区表，升级到本版本时请先升级或停止所有 Jobs 进程（`combined` 或 `jobs`），避免旧进程在切换后执行清理。
- 指标 `retention_partitions_dropped_total` 记录删除的分区数。实现见 `internal/adapters/postgres/partitions.go`。

## 从 MinIO 迁移到 RustFS

RustFS 不能直接读取 MinIO 的数据目录。升级后新的 `rustfs` 服务使用空的 `rustfs-data` 卷，原 `minio-data` 卷保留不动（升级不会删除）。已有备份制品、知识原件、平面图和告警附件需要复制一次：

1. 升级前确认旧版本仍在运行，或记下旧 MinIO 的数据卷名（如 `iot-platform-online_minio-data`）。
2. 升级完成后，用旧镜像临时启动一个只读的 MinIO（原镜像为 `iot-platform-minio:*`，本地已有），并用 rclone 复制全部桶到 RustFS，例如在线部署：

```bash
docker run -d --name minio-old --network iot-platform-online_iot -v iot-platform-online_minio-data:/data -e MINIO_ROOT_USER=<原用户> -e MINIO_ROOT_PASSWORD=<原密码> iot-platform-minio:RELEASE.2025-09-07T16-13-09Z server /data
docker run --rm --network iot-platform-online_iot rclone/rclone sync --s3-provider Other :s3,endpoint=http://minio-old:9000,access_key_id=<原用户>,secret_access_key=<原密码>: :s3,endpoint=http://rustfs:9000,access_key_id=<用户>,secret_access_key=<密码>: --create-empty-src-dirs
docker rm -f minio-old
```

3. 在备份中心对一个整库备份点执行“恢复验证”，并打开一张已有平面图或知识原件，确认读取正常后再按需删除旧卷。

项目名与网络名以 `docker compose ls`、`docker network ls` 为准；离线环境需提前在有网机器拉取 `rclone/rclone` 镜像并导入。

## 设备数据备份

备份服务把制品保存到 RustFS 的 `iot-backups` 桶，提供下载、SHA-256 校验与隔离恢复验证。按以下范围选择：

| 类型 | 内容与时间范围 |
| --- | --- |
| `DEVICE_DAILY`（备份昨日数据） | PostgreSQL 原始报文、标准解析消息，ClickHouse 原始报文与解析遥测。原文按接收时间、标准消息按处理时间（旧记录回退消息时间）、遥测按消息时间分日；两种存储分别标明来源 |
| `DATABASE`（整库备份） | `pg_dump` 自定义格式导出整个 PostgreSQL 业务库（用户与角色、设备模板与凭据、消防管理、告警、通知配置、协议发布记录等全部业务表）以及 ClickHouse 遥测与高频原文表（Native 格式）；默认每天 `IOT_BACKUP_DATABASE_TIME`（01:30，留空关闭）执行并保留最近 `IOT_BACKUP_DATABASE_KEEP`（7）份 |
| `FULL`（立即备份设备数据） | 全量设备消息，外部数据接入的配置、密文凭据、记录与任务（`external-data.jsonl.gz`），四张知识表及索引、引用的 RustFS 原件，全部 Harness 实例的动态 Agent 与会话快照 |

每日自动备份默认开启，每天上海时间 00:05 执行昨日备份；服务停机期间不自动补跑。`FULL` 使用 v2 清单，按组件记录实际包含范围；旧备份缺少的组件显示“不包含”，不补记成功。

`DEVICE_DAILY` 与 `FULL` 不包含平台账号及开放密钥、Provider/API Key、消息主题与对接授权（`message_topic_configs`）、设备模板/凭据与接入配置、消防管理（`fire_safety_record`）、接入草稿/批量任务/验收/回滚历史（`onboarding_record`），也不包含 Redis、Kafka、环境文件或整个 RustFS。上述数据库内容由 `DATABASE` 整库备份覆盖；协议制品、运行配置与环境秘密另行保管。外部接入组件中的凭据仍需原环境秘密才能解密。

备份列表“恢复验证”调用 `POST /api/v1/backups/:id/restore`，逐项校验制品 SHA-256、大小与恢复数量：

- 设备消息写入 `IOT_BACKUP_RESTORE_TARGET_DSN` 的 `restored_message`、`restore_run`；目标库未配置，或与业务库主机、端口、库名相同，返回 412。
- 知识库恢复到该库的 `kb_restore_<标识>` schema，原件恢复到独立 RustFS 前缀，Harness 文件恢复到隔离目录。
- 外部接入记录恢复到 `external_restore_<标识>` schema，不覆盖在线配置或重新启动任务。

- 整库备份的恢复验证写入 `IOT_BACKUP_RESTORE_DATABASE_DSN` 指向的专用演练库（需预先创建，例如同实例的 `iot_drill` 库）：先清空其 `public` schema 再用 `pg_restore` 恢复并核对表数量；目标与业务库或消息恢复库相同时拒绝执行，未配置时返回 412。备份镜像内置 PostgreSQL 17 客户端，外部 PostgreSQL 主版本更高时需用 `IOT_BACKUP_POSTGRES_TOOLS_DIR` 指定匹配版本的 `pg_dump`/`pg_restore`。
- 配置 `IOT_BACKUP_OFFSITE_ENDPOINT`、`IOT_BACKUP_OFFSITE_BUCKET`、`IOT_BACKUP_OFFSITE_ACCESS_KEY`、`IOT_BACKUP_OFFSITE_SECRET_KEY`（可选 `IOT_BACKUP_OFFSITE_REGION`、`IOT_BACKUP_OFFSITE_USE_TLS`，默认 TLS）后，每次备份的全部制品与清单另写一份到该 S3 兼容存储并逐个校验大小与 SHA-256，异地写入失败即整次备份失败并触发 `BackupFailures` 告警。同机 RustFS 与数据位于同一主机，不能单独视为灾难恢复副本。

同一时间只运行一个备份或恢复。文件校验与隔离恢复是不同操作；隔离恢复不替换现网数据，也不等同于完整系统恢复。

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
# 内部 Harness 快照端点，多个实例用逗号分隔；沿用 IOT_AI_HARNESS_TOKEN
IOT_BACKUP_HARNESS_SNAPSHOT_URLS=http://deepseek-harness:8091/v1/backup/snapshot
# 恢复演练对象存储，Compose 默认已有 rustfs-dr
IOT_BACKUP_RESTORE_MINIO_ENDPOINT=rustfs-dr:9000
IOT_BACKUP_RESTORE_MINIO_ACCESS_KEY=
IOT_BACKUP_RESTORE_MINIO_SECRET_KEY=
# Agent/会话只恢复到此隔离目录，不写活跃 Harness 卷
IOT_BACKUP_RESTORE_HARNESS_DIR=./data/restore/harness
```

Windows 源码调试只需 Go 环境，使用 `go run ./cmd/backup-service --env-file .env.local` 或 VS Code 的 `IoT Platform (API + Web + Backup)`；数据库与 RustFS 可继续运行在 Linux 依赖机。旧备份记录与文件不删除，旧接口类型 `RAW_LOGS` / `INCREMENTAL` 兼容映射为昨日设备数据备份。

## 独立接入进程

中心 API 与独立 Access Gateway 共用业务存储和协议制品；可单进程运行，也可按以下职责拆分。

### 进程职责

- `cmd/iot-platform` 默认 `IOT_PROCESS_ROLE=combined`，保留单进程入口。
- `IOT_PROCESS_ROLE=api` 不启动 Modbus、Listener 或外部 MQTT 上行订阅；默认仍内嵌解析、业务处理与后台任务（`IOT_API_EMBEDDED_WORKERS=true`）。设为 `false` 后 API 只提供管理接口、运维中心和视频控制，必须同时部署下列 Worker 角色，否则上报不会被解析和处理。
- Worker 角色（同一镜像与入口，设置 `IOT_PROCESS_ROLE`）：

  | 角色 | 运行内容 | 扩容依据 |
  | --- | --- | --- |
  | `parser` | 消费 `iot.raw.message`，解析并发布到内部业务流 `iot.device.business`（及对外 property/event/parsed 主题） | 原文积压、解析耗时 |
  | `processor` | 按设备顺序消费 `iot.device.business`：规则、告警、设备状态、完成标记、outbox 转发 | 业务流积压、数据库等待 |
  | `jobs` | 离线扫描、原文重发、视频媒体重试、凭据吊销重试及批量接入恢复，按任务或资源租约协调执行；设备告警通知按消费组分摊，重复投递由 Alertmanager 去重 | 待执行量 |

  Worker 只开放 `/health/*` 与 `/metrics`；`/health/ready` 只检查本角色依赖，并返回 `role`、`instance`。指标带 `process_info{role,instance}`。所有拆分角色都需要共享 PostgreSQL 与 Kafka；只有 `api`（及 `combined`）需要 `IOT_AI_HARNESS_URL`。告警研判由 API 进程按用户操作运行。
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
- 一条没有告警变化的消息只访问 PostgreSQL 三次：领取（新消息写入与领取为同一语句）、一次读取设备状态及是否有活动告警、一条语句同时写入设备状态和完成标记（状态版本冲突时两者都不写，重读后重试）。活动告警数由 `alarm_record` 上的触发器维护在 `device_open_alarm`（迁移 0012），任何告警写入路径都会同步更新。
- 告警确认/恢复/关闭、规则停用、离线扫描、连接状态和设备状态写入都基于行版本做乐观并发，冲突时重读重试（`alarm_conflict_total`、`device_state_conflict_total`），不会用旧快照覆盖新上报。
- 规则与协议缓存在各实例本地保留最多 2 秒，跨实例生效时间以此为上界；权限每次请求读取数据库，撤销立即生效。
- 设备上报 20 条/秒、开放 API 密钥 100 次/秒、登录失败锁定（15 分钟窗口内 10 次）在配置 Redis 后由所有实例共享同一额度（固定时间窗口，窗口按 Unix 时间对齐）；Redis 故障时按 `IOT_CLUSTER_INSTANCES` 退化，并计入 `rate_limit_shared_errors`。
- Redis 支持 Sentinel：设置 `IOT_REDIS_MASTER_NAME` 与 `IOT_REDIS_SENTINELS`（逗号分隔）后跟随主节点切换；设备状态缓存按行版本写入，旧写入不会覆盖新缓存。
- 回放任务记录执行实例与心跳；执行进程退出后，超过 60 秒无心跳的任务在查询时标为 `INTERRUPTED`，不会永久停在运行中。
- MQTT 持久队列目录加进程独占锁，同一目录被第二个进程打开时启动失败，避免两个实例共用一个 client ID。

### 多 API 实例：视频控制与 Harness

- 多个 API 实例都设置 `IOT_NODE_URL`（本实例可被其他实例访问的 HTTP 地址）后，通过 `video/control` 租约选出一个实例运行直播模块（SIP 服务、播放会话、媒体任务与清理）；其他实例把 `/api/v1/video/*`、`/api/v1/integrations/video/*`（含媒体服务器回调与 HLS 鉴权）带原用户凭据转发到持有者，由持有者重新校验。持有者续租失败立即停止模块，租约过期后备用实例接管并从数据库恢复播放会话；现有 SIP 连接与 WebRTC 播放需要设备重新注册、浏览器重新点播。单实例部署不设置该变量时行为不变。
- 媒体服务器回调地址、SIP 端口映射须指向当前持有者或能转发到它的入口；权限变化由持有者每 15 秒复核一次后撤销播放。
- `IOT_AI_HARNESS_URL` 可填多个逗号分隔地址。同一会话按会话 ID 固定路由到同一 Harness 实例（多轮上下文保存在该实例）；该实例不可达时改由下一实例开始新会话。告警研判、巡检、运维报告等业务任务没有对话历史，首选实例繁忙时改用其他空闲实例，全部繁忙才排队等待。任一实例健康即视为可用。
- 模型配置与动态智能体以 PostgreSQL 为准（`ai_model_config`、`ai_workflow_manifest`）。每个 API 实例每 10 秒对账一次：采用其他实例保存的模型配置；Harness 重启后（模型配置只保存在 Harness 进程内存，重启会回到环境变量默认值）按实例 ID 识别并重新下发；动态智能体只同步到部分实例时补齐，删除时保留删除标记，防止漏删的实例把它带回。升级前已存在于 Harness 的动态智能体在首次对账时登记入库。

### 执行所有权

启用 `IOT_ACCESS_COORDINATION=true`，并将 `IOT_ACCESS_NODE_URL` 设置为其他实例可达且精确指向本实例的 HTTP(S) 地址。不要使用随机负载均衡地址冒充固定执行节点。

共享 PostgreSQL 的 `execution_lease` 以租户和 Profile 为资源键。租约 10 秒，节点本地取消期限短于数据库期限，续租失败即取消旧执行；接管提升 fencing token。运行时在归档前检查所有权和当前配置。配置停止被扫描时释放租约。

可确定 Profile 的请求按租约中的节点地址转发，保留原用户授权并限制转发次数。

设备命令（`POST /api/v2/device-access-profiles/:id/devices/:deviceId/commands`）只能由持有该接入点监听与设备会话的进程执行：单副本或单个接入网关时直接在本进程执行；多副本时须开启 `IOT_ACCESS_COORDINATION`，请求按租约转发到持有者。未开启协调而请求落到非持有副本时返回 409 并提示开启协调或改为单副本；已开启协调但没有副本持有该接入点（监听未运行）时返回 503，提示确认接入点已启用后重试。设备级路由依赖明确的实例标签或唯一的设备配置关联，不能据此推断所有历史设备、子设备及任意多副本部署均可自动路由。该实现提供互斥执行与接管，不宣称已有按负载最优调度、跨节点迁移现有 TCP 会话或数据库之外的强制 OS 隔离。

### 验证入口

`go test ./internal/platformapp ./internal/httpapi ./internal/protocolruntime` 覆盖进程职责、认证转发与执行协调；环境相关集成测试的实际执行条件见各测试。源码入口为 `internal/httpapi/process_role.go`、`internal/httpapi/execution_route.go` 和 `internal/protocolruntime/coordinator.go`。

### 管理界面与用户范围

设备模板准备页管理物模型、协议版本与公共连接，单台设备登记时填写专属连接参数；`DeviceAccessProfile` 是软件连接配置，`cmd/iot-access-gateway` 是部署进程。首台验收、批量复用、候选配置更新与回滚见[设备接入](INTEGRATION.md#设备接入)。API 和 Gateway 使用同版代码，转发保留原用户身份，并在目标服务重新校验。

普通用户的全租户接入配置需要全部设备范围和相应菜单/按钮权限。指定设备用户的连接详情不暴露共享网关配置及其他设备会话。用户设备和告警范围见 [用户权限](PLATFORM.md#权限与设备范围)。
