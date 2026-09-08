# 消防 IoT 平台

Go API + Vue 3 管理端，提供设备接入、Go 协议源码上传与版本切换、Modbus 点表采集、告警、原始报文查询与回放、知识库和 AI 辅助运维。

## 选择方案

| 方案 | 前后端运行位置 | 依赖准备 | 访问地址 |
|---|---|---|---|
| **本地运行** | 本机运行源码，可用 IDE 调试 | 一键启动依赖容器、下载 Go/npm 依赖 | `http://localhost:5173` |
| **在线部署** | 全部运行在 Docker | Linux 自动准备 Docker，再构建并启动服务 | `http://服务器IP:8080` |
| **离线部署** | 全部运行在 Docker | 有网打包，离线安装 Docker 并导入服务 | `http://服务器IP:8080` |

源码相关命令在**本仓库根目录**执行；离线安装命令在**生成的离线包根目录**执行。

| 运行环境 | 需要提前准备 |
|---|---|
| Ubuntu / CentOS 在线部署 | systemd、root 或 sudo 权限；脚本检测并安装缺失的 Docker、Compose 和 Buildx |
| Ubuntu / CentOS 离线部署 | 完整离线包、系统基础依赖、root 或 sudo 权限；Docker 和 Compose 从包内安装 |
| Windows / macOS 部署 | 已安装并启动 Docker Desktop，使用 Linux 容器 |
| 有网打包机 | 已启动 Docker Engine 或 Docker Desktop，以及 Compose 2.24.4+ |
| 本地源码调试 | 依赖机预装 Docker 和 Compose；源码机安装 Go ≥ 1.25.5、Node.js ≥ 22.12（也兼容 20.19+ 的 Node.js 20） |

已有可用 Docker 时直接复用。自动安装适用于使用 systemd 的 Linux amd64 / arm64；系统包要求见 [Docker 自动安装](docs/OFFLINE_DEPLOYMENT.md#docker-自动安装)。依赖运行在虚拟机时，Windows 源码机无需安装 Docker。

在线与离线部署默认包含 PostgreSQL、Redis、ClickHouse、Redpanda、EMQX、MinIO、备份服务、Ollama/Weaviate、`nomic-embed-text` 嵌入模型和 AI 工作流 Harness。本地运行会启动基础依赖和 Harness，备份服务默认作为源码进程单独调试。在线与离线部署还会自动准备 `qwen3:1.7b`；告警研判、规则辅助和 AI 工作流统一使用该本地模型，不需要 API Key。这个模型下载约 1.4 GB，适合 8 GB 内存的整套虚拟机环境。

## 1. 本地运行

### 首次准备

Windows PowerShell：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\setup-local.ps1
```

Linux / macOS：

```bash
bash ./scripts/setup-local.sh
```

脚本生成带逐项中文说明的 `.env.local`，启动基础依赖、AI 工作流 Harness、初始化消息主题与知识库模型，并执行 `go mod download` 和 `npm ci`。备份服务不会随依赖容器启动，默认由 VS Code 源码配置单独启动。源码方案默认使用 DeepSeek API；把 Key 填入 `DEEPSEEK_API_KEY` 后重跑一次脚本，使 Harness 容器加载密钥。需要改用本地模型时可传 `-IncludeAi` / `--include-ai`，或启动后在“AI 模型管理”菜单切换；再次执行会复用其他配置和数据。若只需启动依赖，可加 `-SkipCodeDeps` / `--skip-code-deps`。只有需要临时验证容器版备份服务时才传 `--include-backup` / `-IncludeBackup`。

如果依赖容器运行在 Linux 虚拟机、源码运行在 Windows，Linux 使用 Windows 可访问的虚拟机地址启动：

```bash
bash ./scripts/setup-local.sh --skip-code-deps --dependency-host <虚拟机IP> --api-host <Windows在虚拟机网段的IP>
```

`--dependency-host` 会开放依赖端口，并让 PostgreSQL、Kafka、MQTT、MinIO、ClickHouse、Ollama 和 Weaviate 使用虚拟机地址；Harness 仍使用虚拟机容器，备份服务使用源码机端口 `8092`，API 使用 `8081`。`--api-host` 供 Harness 容器回调 Windows 上的 API，VMware NAT 环境通常填写对应虚拟网卡的主机地址。将 Linux 生成的 `.env.local` 安全复制到 Windows 仓库根目录，确认 `DEEPSEEK_API_KEY` 已填写，再按下面的日常命令运行源码。只应在可信的主机专用或局域网中使用此模式。

### 日常运行代码

终端一，启动后端：

```bash
go run ./cmd/iot-platform --env-file .env.local
```

终端二，启动前端：

```bash
cd iot_front
npm run dev
```

终端三，调试备份服务（也可以在 VS Code 使用组合配置）：

```bash
go run ./cmd/backup-service --env-file .env.local
```

备份服务只导出设备原始报文和解析数据，通过 Go 直接连接 PostgreSQL、ClickHouse 和 MinIO；源码机不需要 Docker、`pg_dump` 或 `redis-cli`。支持手动备份与每日自动备份，详见 [设备数据备份](docs/DEPLOYMENT.md#设备数据备份)。

Windows PowerShell 若提示 npm 脚本执行策略错误，改用 `npm.cmd run dev`。访问 **http://localhost:5173**，前端自动代理到本机 API `8081` 端口。

GoLand 调试时，工作目录设为仓库根目录、程序设为 `cmd/iot-platform`，程序参数填 `--env-file .env.local`，无需手工复制数据库地址。已有进程环境变量优先于配置文件；旧 IDE 配置中的硬编码密码或地址应先移除。

### VS Code 一键启动

仓库已提供 [`.vscode/launch.json`](.vscode/launch.json)。安装 VS Code Go 扩展并确保 `.env.local` 已生成或已从依赖机复制后，打开“运行和调试”，选择 **IoT Platform (API + Web)**，按 `F5` 启动 Go API 和 Vue 前端；需要调试备份服务时选择 **IoT Platform (API + Web + Backup)**。需要国标网关时选择 **IoT Platform + GB26875 Gateway**。配置文件只引用 `.env.local`，不包含密码；Windows 使用 `npm.cmd`，Linux/macOS 使用 `npm`。

## 2. 在线部署

把本仓库源码放到有网络的目标服务器，在仓库根目录执行。Ubuntu 与 CentOS 使用同一入口：

```bash
sudo bash ./scripts/deploy-online.sh
```

已使用 root 登录时可省略 `sudo`。缺少 Docker 时自动安装并启动；缺少 Compose 或 Buildx 时补装插件。Ubuntu 通过 APT 安装缺失的基础依赖；CentOS 7 必要时使用临时 Vault 源，不覆盖已有 yum 配置。

Windows PowerShell：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\deploy-online.ps1
```

macOS（先启动 Docker Desktop）：

```bash
bash ./scripts/deploy-online.sh
```

脚本首次生成带逐项中文说明的 `.env.online`，拉取基础镜像、构建 API/Web/备份服务和 Harness，下载 `qwen3:1.7b` 与知识库模型，启动服务并检查 API、网页和 Harness。完成后访问 **http://服务器IP:8080**。服务器无需安装 Go 或 Node.js；首次构建需要访问镜像仓库、Go/npm 依赖源、Harness 源码和 Ollama 模型源。

更新源码后重新执行同一命令即可构建并更新服务。已有凭据保持不变，数据库数据保存在 Docker 命名卷中。

## 3. 离线部署

离线部署分为“有网打包”和“无网安装”两个步骤。Ubuntu 和 CentOS 均使用同一套 Linux 部署脚本。打包机与服务器的 Docker CPU 架构必须一致，例如 x86_64 服务器使用 linux/amd64 包；ARM64 包不能直接用于 x86_64 服务器。

### 有网机器：一键打包

Windows PowerShell：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\package-offline.ps1
```

Linux / macOS：

```bash
bash ./scripts/package-offline.sh
```

输出位于 `offline-bundles/iot-platform-offline-*`，包括运行镜像、对话和知识库模型、Harness、Linux Docker/Compose/Buildx 安装文件、校验文件、配置和部署脚本。将**整个生成目录**复制到离线服务器，包括隐藏文件 `.env.offline`。

精简 Linux 系统若缺少 iptables、xz、ps 等基础依赖，打包时通过 `--docker-packages-dir <目录>` / `-DockerPackagesDir <目录>` 加入匹配目标系统版本与架构的软件包及全部依赖：Ubuntu 使用 DEB，CentOS 使用 RPM。离线脚本不会联网补包。目标机已有 Docker 时，可用 `--skip-docker-runtime` / `-SkipDockerRuntime` 减小包体。

### 离线服务器：一键安装

进入复制后的离线包目录，Ubuntu / CentOS 执行：

```bash
sudo bash ./scripts/deploy-offline-linux.sh
```

脚本检测 Docker 与 Compose，缺失时校验并使用包内文件安装；已有可用安装则直接复用。旧离线包未携带 Docker 安装文件时，需要重新打包。

Windows PowerShell（先启动 Docker Desktop）：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\deploy-offline.ps1
```

macOS（先启动 Docker Desktop）：

```bash
bash ./scripts/deploy-offline-macos.sh
```

脚本校验并导入镜像和模型，使用 `--no-build --pull never` 启动，随后检查服务。完成后访问 **http://服务器IP:8080**。目标服务器不需要源码、Go、Node.js 或网络。

离线更新时，用目标环境原有 `.env.offline` 打包以保持凭据一致。模型、可选组件、完整参数及更新步骤见 [离线部署说明](docs/OFFLINE_DEPLOYMENT.md)。

## 登录与配置

- 默认租户：`tenant_001`；用户名：`admin`。
- 密码为对应环境文件中的 `IOT_ADMIN_PASSWORD`：本地 `.env.local`，在线 `.env.online`，离线包 `.env.offline`。首次脚本会生成随机凭据。
- 不同方案使用独立 Compose 项目和数据卷；默认端口有重叠，同一台机器上不要同时启动多套默认配置。
- 配置文件需单独妥善保管；设备数据备份不包含配置和账号。已有数据库卷时，直接改文件中的数据库密码不会同步修改库内账号。
- `IOT_AI_MODEL` 和 `IOT_OLLAMA_MODEL` 用于切换本地模型；`IOT_AI_HARNESS_MODEL` 应保持相同。`IOT_AI_HARNESS_ENABLED=false` 可关闭 Harness。
- 管理员可在独立的“AI 模型管理”菜单切换本地 Ollama、DeepSeek 云端模型或兼容接口模型。先点击“测试配置”确认地址、模型和接口密钥可用，再点击“应用配置”；应用后告警研判、AI 对话、规则草稿、报告和工作流会统一使用新模型服务。接口密钥会脱敏显示，并在使用 PostgreSQL 时保存到活动配置。
- 告警详情中的“立即研判”会显示实时进度和预计剩余时间，任务完成后自动展示研判结果。
- “智能巡检”同样以后台任务运行，显示阶段、进度和预计剩余时间；切换页面后会自动恢复任务进度，完成后保留巡检报告。

AI 可选参数、端口、日志、停止与升级命令见 [部署配置与维护](docs/DEPLOYMENT.md)。

## 源码与文档

| 目录 / 文档 | 内容 |
|---|---|
| `cmd/iot-platform`、`internal/` | API、业务逻辑、存储和协议运行时 |
| `iot_front/` | Vue 管理端 |
| `scripts/`、`compose*.yaml`、`deploy/` | 部署入口和运行配置 |
| [设备接入流程](docs/设备协议接入流程.md) | 产品、设备与协议接入操作 |
| [Go 源码协议](docs/GO_PROTOCOL_PACKAGES.md) | 源码上传、编译、发布与回滚 |
| [协议 V2 设计](docs/DEVICE_PROTOCOL_ACCESS_DESIGN.md) | 点表、版本与 Worker 契约 |
| [AI 工作流](docs/AI_PLUGIN_HARNESS.md) | Harness、Agent 和知识库 |
| [GB/T 26875](docs/GB26875_DAHUA_V103.md) | GB26875 独立 Go 协议包及通用 TCP/UDP 接入 |

开发检查：在仓库根目录执行 `go test ./...`；在 `iot_front` 执行 `npm test` 和 `npm run build`。摄像头模块仅管理元数据和设备关联，视频流由外部视频平台提供。
