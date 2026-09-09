# 炬联 TorchLink 技术详情

[返回项目介绍](../README.md)

本文承接原 README 的环境准备、源码运行、在线部署与离线交付说明，并提供当前扩展模块的配置入口。所有源码命令均在仓库根目录执行；本文件位于 `docs/`，不改变命令的工作目录。

维护日期：2026-09-10。版本和默认值依据当前源码与部署脚本核对，具体环境结果以实际执行为准。

[环境要求](#1-环境与方案选择) · [本地运行](#2-本地运行) · [在线部署](#3-在线部署) · [离线部署](#4-离线部署) · [扩展模块](#6-接入进程与扩展模块) · [开发检查](#按改动范围执行检查)

设备接入统一入口为 **设备管理 → 添加设备**。支持 MQTT / HTTP 标准上报、Modbus TCP、Go TCP/UDP 协议和经现场节点执行的 RTU、OPC UA、SNMP、BACnet/IP 读取；具体子集和本地许可见 [统一设备接入与 API](UNIFIED_DEVICE_ONBOARDING.md)。

Go API + Vue 3 管理端，包含协议版本与可信分发、原始报文与告警、默认/命名影子、设备拓扑、知识库和 AI 辅助运维。独立 Gateway、Edge Agent、启动器和 GB28181 元数据服务按需配置。项目定位、功能和平台对比见 [README](../README.md)。

## 1. 环境与方案选择

| 方案 | 前后端运行位置 | 依赖准备 | 访问地址 |
|---|---|---|---|
| **本地运行** | 本机运行源码，可用 IDE 调试 | 一键启动依赖容器、下载 Go/npm 依赖 | `http://localhost:5173` |
| **在线部署** | 全部运行在 Docker | Linux 自动准备 Docker，再构建并启动服务 | `http://服务器IP:8080` |
| **离线部署** | 全部运行在 Docker | 有网打包，离线安装 Docker 并导入服务 | `http://服务器IP:8080` |

源码相关命令在**本仓库根目录**执行；离线安装命令在**生成的离线包根目录**执行。

当前后端要求 Go **1.25.5**（以 [go.mod](../go.mod) 为准）；前端要求 Node.js **`^20.19.0 || >=22.12.0`**（以 [package.json](../iot_front/package.json) 为准），使用 npm 和现有锁文件。协议子目录中有独立 `go.mod` 的项目需单独检查和构建。

| 运行环境 | 需要提前准备 |
|---|---|
| Ubuntu / CentOS 在线部署 | systemd、root 或 sudo 权限；脚本检测并安装缺失的 Docker、Compose 和 Buildx |
| Ubuntu / CentOS 离线部署 | 完整离线包、系统基础依赖、root 或 sudo 权限；Docker 和 Compose 从包内安装 |
| Windows / macOS 部署 | 已安装并启动 Docker Desktop，使用 Linux 容器 |
| 有网打包机 | 已启动 Docker Engine 或 Docker Desktop，以及 Compose 2.24.4+ |
| 本地源码调试 | Linux 依赖机需要 systemd、root/sudo，脚本自动准备 Docker/Compose/Buildx 和 Harness 所需 Git；源码机 Go 版本见 `go.mod`，Node.js 要求见 `iot_front/package.json` |

已有可用 Docker 时直接复用。自动安装适用于使用 systemd 的 Linux amd64 / arm64；系统包要求见 [Docker 自动安装](OFFLINE_DEPLOYMENT.md#docker-自动安装)。依赖运行在虚拟机时，Windows/macOS 源码机无需安装 Docker。macOS 直接运行依赖容器时也可以使用 OrbStack。

在线与离线部署默认包含 PostgreSQL、Redis、ClickHouse、Redpanda、EMQX、MinIO、备份服务、Ollama/Weaviate、`nomic-embed-text` 嵌入模型和 AI 工作流 Harness。本地运行会启动基础依赖和 Harness，备份服务默认作为源码进程单独调试。在线与离线部署还会自动准备 `qwen3:1.7b`；告警研判、规则辅助和 AI 工作流统一使用该本地模型，不需要 API Key。这是当前脚本的默认模型，不构成固定资源配置承诺；根据模型大小、并发请求、设备数量、报文频率与保留时间评估内存、CPU 和磁盘。

## 2. 本地运行

### 首次准备

Windows PowerShell：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\setup-local.ps1
```

Linux / macOS：

```bash
bash ./scripts/setup-local.sh
```

脚本生成带逐项中文说明的 `.env.local`，启动基础依赖、AI 工作流 Harness、初始化消息主题与知识库模型，并执行 `go mod download` 和 `npm ci`。备份服务不会随依赖容器启动，默认由 VS Code 源码配置单独启动。源码方案默认使用 DeepSeek API；把 Key 填入 `DEEPSEEK_API_KEY` 后重跑一次脚本，使 Harness 容器加载密钥。需要改用本地模型时可传 `-IncludeAi` / `--include-ai`，或启动后在“模型管理”菜单切换；再次执行会复用其他配置和数据。若只需启动依赖，可加 `-SkipCodeDeps` / `--skip-code-deps`。只有需要临时验证容器版备份服务时才传 `--include-backup` / `-IncludeBackup`。

如果依赖容器运行在 Linux 虚拟机、源码运行在 Windows/macOS，Linux 使用源码机可访问的虚拟机地址启动（已有 Docker socket 权限或使用 root 时可省略 `sudo`）：

```bash
sudo bash ./scripts/setup-local.sh --skip-code-deps --dependency-host <虚拟机IP> --api-host <源码机在虚拟机网段的IP或主机名>
```

`--dependency-host` 会开放依赖端口，并让 PostgreSQL、Kafka、MQTT、MinIO、ClickHouse、Ollama 和 Weaviate 使用虚拟机地址；Harness 仍使用虚拟机容器，备份服务使用源码机端口 `8092`，API 使用 `8081`。`--api-host` 供 Harness 容器回调源码机上的 API，VMware NAT 环境通常填写对应虚拟网卡的主机地址。将 Linux 生成的 `.env.local` 安全复制到源码机仓库根目录，确认 `DEEPSEEK_API_KEY` 已填写，再按下面的日常命令运行源码。只应在可信的主机专用或局域网中使用此模式。OrbStack 的共享目录、命令和连通性验证见 [OrbStack 虚拟机本地调试](DEPLOYMENT.md#orbstack-虚拟机本地调试)。

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

备份服务只导出设备原始报文和解析数据，通过 Go 直接连接 PostgreSQL、ClickHouse 和 MinIO；源码机不需要 Docker、`pg_dump` 或 `redis-cli`。支持手动备份与每日自动备份，详见 [设备数据备份](DEPLOYMENT.md#设备数据备份)。

Windows PowerShell 若提示 npm 脚本执行策略错误，改用 `npm.cmd run dev`。访问 **http://localhost:5173**，前端自动代理到本机 API `8081` 端口。

GoLand 调试时，工作目录设为仓库根目录、程序设为 `cmd/iot-platform`，程序参数填 `--env-file .env.local`，无需手工复制数据库地址。已有进程环境变量优先于配置文件；旧 IDE 配置中的硬编码密码或地址应先移除。

### VS Code 一键启动

仓库已提供 [`.vscode/launch.json`](../.vscode/launch.json)。安装 VS Code Go 扩展并确保 `.env.local` 已生成或已从依赖机复制后，打开“运行和调试”，选择 **IoT Platform (API + Web)**，按 `F5` 启动 Go API 和 Vue 前端；需要调试备份服务时选择 **IoT Platform (API + Web + Backup)**。需要国标网关时选择 **IoT Platform + GB26875 Gateway**。配置文件只引用 `.env.local`，不包含密码；Windows 使用 `npm.cmd`，Linux/macOS 使用 `npm`。

macOS 首次调试需安装 Delve（`go install github.com/go-delve/delve/cmd/dlv@latest`），并由使用者完成系统弹出的“开发者工具访问”认证。调试会话显示“正在运行”不代表服务已启动；应同时检查 `http://localhost:8081/health/ready`。若终端停在 `debugserver` 且业务端口未监听，先检查系统授权窗口。调试器产生的 `__debug_bin*` 文件已加入忽略规则。

前后端和备份服务启动后，可从仓库根目录运行 `node scripts/tests/local-business-smoke.mjs`。脚本读取 `.env.local`，通过本机 `5173` 的前端代理验证登录、页面数据接口、产品与设备接入、上报归档解析、幂等冲突、在线状态、时序查询、规则告警处置及试运行回放。它只创建名称带“本地联调”的唯一数据，结束后停用本次规则及设备凭据，保留记录供页面复查，不删除已有数据。可用 `IOT_TEST_ENV_FILE` 指定配置文件、`IOT_TEST_ORIGIN` 指定本机代理地址；脚本不输出凭据。依赖单独验证入口为 `go run scripts/tests/local-runtime-smoke.go --env-file .env.local`。

2026-09-10 本地验证：macOS arm64 上通过 VS Code 组合配置运行前端、Go API 和备份服务，依赖复用 OrbStack 的 `develop` Ubuntu arm64 虚拟机。上述依赖检查 8 项及业务冒烟通过；浏览器产品查询命中 `internal/httpapi/server.go` 的产品列表断点，单步查询无错误，继续执行后页面显示同样的数据。浏览器复现并修复了设备详情跳转告警时遗漏设备筛选、连接历史显示英文状态码的问题；修复后的设备筛选与重置、历史中文状态已回归。`npm test` 61 项、`npm run build` 和 `go test ./internal/core ./internal/httpapi` 通过；构建仍有主包超过 500 kB 的体积提示。

本次模型验证需单独区分：嵌入推理通过，`qwen3:1.7b` 的 8 个生成词元短请求约 6 秒完成，但页面“测试配置”在 90 秒后超时，不能据模型列表健康声称智能业务已验收。未进行真实消防设备、现场协议硬件和数据库恢复验收。

同次浏览器备份验证：创建的设备数据备份与文件校验任务均完成；经前端代理下载原始报文、解析数据和清单 3 个制品，字节摘要与清单逐项一致。组件名称补齐中文；各页面显式声明统一导航事件，消除开发模式下多根节点的监听器透传警告。文件校验通过不代表已执行数据库恢复。

## 3. 在线部署

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

## 4. 离线部署

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

离线更新时，用目标环境原有 `.env.offline` 打包以保持凭据一致。模型、可选组件、完整参数及更新步骤见 [离线部署说明](OFFLINE_DEPLOYMENT.md)。

## 5. 登录与配置

- 默认租户：`tenant_001`；用户名：`admin`。
- 密码为对应环境文件中的 `IOT_ADMIN_PASSWORD`：本地 `.env.local`，在线 `.env.online`，离线包 `.env.offline`。首次脚本会生成随机凭据。
- 不同方案使用独立 Compose 项目和数据卷；默认端口有重叠，同一台机器上不要同时启动多套默认配置。
- 配置文件需单独妥善保管；设备数据备份不包含配置和账号，也不包含协议源码/Worker 目录、市场签名私钥和节点升级信任策略。已有数据库卷时，直接改文件中的数据库密码不会同步修改库内账号。
- `IOT_AI_MODEL` 和 `IOT_OLLAMA_MODEL` 用于切换本地模型；`IOT_AI_HARNESS_MODEL` 应保持相同。`IOT_AI_HARNESS_ENABLED=false` 可关闭 Harness。
- 管理员可在独立的“模型管理”菜单切换本地 Ollama、DeepSeek 云端模型或兼容接口模型。先点击“测试配置”确认地址、模型和接口密钥可用，再点击“应用配置”；应用后告警研判、AI 对话、规则草稿、报告和工作流会统一使用新模型服务。接口密钥会脱敏显示，并在使用 PostgreSQL 时保存到活动配置。
- 告警详情中的“立即研判”会显示实时进度和预计剩余时间，任务完成后自动展示研判结果。
- “智能巡检”同样以后台任务运行，显示阶段、进度和预计剩余时间；切换页面后会自动恢复任务进度，完成后保留巡检报告。

AI 可选参数、端口、日志、停止与升级命令见 [部署配置与维护](DEPLOYMENT.md)。

## 6. 接入进程与扩展模块

### API 与 Gateway 拆分

默认 `IOT_PROCESS_ROLE=combined`，保持原有平台入口。需要拆分时，API 使用 `api` 角色；`cmd/iot-access-gateway` 强制以 `gateway` 角色运行。API 保留管理、消费和业务处理，Gateway 执行设备通信、原文归档与队列投递。

两个进程必须共享 PostgreSQL、Kafka、原文存储和协议制品；不能分别使用本地内存仓库或独立消息总线。API 设置可达的 `IOT_ACCESS_GATEWAY_URL`。分别准备私有 `.env.api` / `.env.gateway` 后，在仓库根目录不同终端运行：

```bash
go run ./cmd/iot-platform --env-file .env.api
```

```bash
go run ./cmd/iot-access-gateway --env-file .env.gateway
```

使用现有在线环境的可选 Compose 覆盖层时，先检查配置：

```bash
docker compose --env-file .env.online -f compose.yaml -f compose.access.yaml config --quiet
```

完成配置检查并准备部署后，再运行：

```bash
docker compose --env-file .env.online -f compose.yaml -f compose.access.yaml up -d --build
```

覆盖层要求 Compose 2.24.4+，将接入监听端口移到 Gateway。启用 `IOT_ACCESS_COORDINATION=true` 时，`IOT_ACCESS_NODE_URL` 必须精确指向本实例，不能填写随机负载均衡地址。租约可管理接管与请求路由，但不迁移既有 TCP 连接。完整参数见 [独立接入进程](EDGE_AND_GATEWAY.md)。

### 现场节点与签名升级

在平台登记节点并生成凭据后，为现场节点准备私有 `.env.edge`，包括平台地址、租户、节点、凭据、数据目录及本地允许的网络、串口和凭据引用。运行：

```bash
go run ./cmd/iot-edge-agent --env-file .env.edge
```

这是直接运行 Agent 的方式；需要受控程序升级时，改由 `iot-edge-launcher` 管理同一节点，不同时运行两个使用相同数据目录的 Agent。启动器构建、签名目录、就绪检查和失败回退见 [Edge 程序升级](EDGE_PROGRAM_UPGRADES.md)。

| 配置或能力 | 默认与作用 | 详细入口 |
| --- | --- | --- |
| 网络 / 串口 / 现场凭据 | 由节点部署者本地配置，平台不能扩大许可范围 | [现场 Agent](EDGE_AND_GATEWAY.md#现场-agent) |
| `IOT_EDGE_ALLOW_GO_WORKERS` | 显式开启后允许已分配的 Go 协议执行；仍检查监听地址与制品 | [Go 协议与版本更新](EDGE_AND_GATEWAY.md#edge-go-协议与远程版本更新) |
| `IOT_EDGE_ALLOW_AUTO_REGISTER` | 默认关闭；还需平台监听实例允许自动登记 | [协议自动登记](EDGE_AND_GATEWAY.md#edge-协议自动登记) |
| `IOT_EDGE_ALLOW_COMMANDS` | 默认关闭；须结合 Worker 许可、角色、确认和当前配置检查 | [现场协议命令](EDGE_AND_GATEWAY.md#现场协议命令) |
| 异构协议制品 | 上传时选择目标平台，节点实际运行样例后启用 | [异构 Edge 制品](GO_PROTOCOL_PACKAGES.md#异构-edge-制品) |

### 状态、视频目录与组织协议分发

以下模块复用当前平台接口，按需准备相关外部服务或本地策略：

| 模块 | 配置与操作入口 | 边界 |
| --- | --- | --- |
| 默认 / 命名影子 | [设备影子](DEVICE_SHADOW.md)：物模型可写属性、版本检查、HTTP/MQTT 读取 | 保存 desired 不自动控制设备；reported 来自成功解析的上报 |
| 设备拓扑 | [设备孪生与拓扑](DEVICE_TWINS.md)：同租户关系与版本约束 | 不提供三维、物理仿真或关系驱动控制 |
| ONVIF | [发现与认证读取](EDGE_AND_GATEWAY.md#onvif-摄像头元数据读取)：网卡许可与现场认证 | 发现候选须认证读取后显式保存，不自动信任 |
| GB28181 | [独立元数据节点](../protocol-packages/gb28181-metadata/README.md)：专用节点身份、SIP 与目录配置 | 独立 module 单独构建；不提供视频流或云台 |
| 可信目录 | `IOT_PROTOCOL_CATALOG_POLICY`，见 [协议目录](PROTOCOL_CATALOG.md) | 默认留空关闭；HTTPS 与签名验证后仍在消费方编译、试跑 |
| 企业私有市场 | `IOT_PROTOCOL_MARKET_POLICY`，见 [组织发布与审核](PRIVATE_PROTOCOL_MARKET.md) | 独立管理员审核；机器读取令牌不使用浏览器 JWT |

策略文件、私钥、节点凭据和读取令牌不提交 Git。容器配置中的文件路径必须在该容器可读的挂载范围内；新增环境变量不会自动把宿主机文件挂载到容器中。市场和目录配置不会自动部署域名、TLS 证书或公开服务。

## 7. 源码、专题文档与检查

| 目录 / 文档 | 内容 |
|---|---|
| `cmd/iot-platform`、`internal/` | API、业务逻辑、存储和协议运行时 |
| `cmd/iot-access-gateway`、`cmd/iot-edge-agent`、`cmd/iot-edge-launcher` | 独立接入、现场节点与受控程序升级 |
| `cmd/iot-protocol-catalog` | 可信目录签名与密钥工具 |
| `iot_front/` | Vue 管理端 |
| [列表分页与关联选择](LIST_PAGINATION.md) | 分页边界、完整选项和请求顺序回归 |
| `scripts/`、`compose*.yaml`、`deploy/` | 部署入口和运行配置 |
| [设备接入流程](设备协议接入流程.md) | 产品、设备与协议接入操作 |
| [Go 源码协议](GO_PROTOCOL_PACKAGES.md) | 源码上传、编译、发布与回滚 |
| [协议 V2 设计](DEVICE_PROTOCOL_ACCESS_DESIGN.md) | 点表、版本与 Worker 契约 |
| [AI 工作流](AI_PLUGIN_HARNESS.md) | Harness、Agent 和知识库 |
| [GB/T 26875](GB26875_DAHUA_V103.md) | GB26875 独立 Go 协议包及通用 TCP/UDP 接入 |

### 按改动范围执行检查

| 执行目录 | 命令 | 说明 |
| --- | --- | --- |
| 仓库根目录 | `go test ./...` | 根 Go module 测试；不包含独立协议 module |
| `protocol-packages/gb26875-dahua` | `go test ./...` | 独立消防协议示例 |
| `protocol-packages/gb28181-metadata` | `go test ./...` | 独立视频目录服务 |
| `iot_front` | `npm test` | 前端现有测试 |
| `iot_front` | `npm run build` | 前端构建 |
| 仓库根目录 | `git diff --check` | 文档和代码的空白检查 |

需要真实 PostgreSQL、MQTT、浏览器或外部协议测试环境的用例，按对应专题文档配置。可选集成用例跳过不代表联调通过；交叉编译不代表目标操作系统或现场硬件已运行。

部署脚本修改后按 [部署说明](DEPLOYMENT.md) 检查 Bash / PowerShell 两套入口。部署冒烟使用真实 Compose 解析与模拟 Docker/HTTP 操作，应为 `scripts/tests/deployment-smoke.sh` 或 `.ps1` 提供可用的独立 Compose 可执行文件路径，不能把冒烟结果当作服务器部署验收。

### 数据保护与验证边界

- 设备数据备份导出原文和解析结果；“恢复演练”接口当前只执行制品读取与 SHA-256 完整性校验，不会向数据库恢复数据。
- 迁移与升级继续使用正确环境文件、Compose 项目名和已有数据卷。修改环境文件中的数据库密码不会同步修改已有数据库账号。
- 全平台恢复还需单独保留业务数据库、协议制品、部署配置和秘密；不要使用删除卷来代替普通重启。
- 摄像头模块提供元数据、发现/目录导入与设备关联，视频流由外部视频平台提供。
- `/health/live`、`/health/ready` 和指标各自反映有限范围。实际交付仍需检查首条原文、解析、告警、模型调用与必要的备份恢复流程。


## 管理端界面约定

管理端采用蓝白配色、分组侧栏、面包屑和统一表格、表单、弹窗。各页面的“使用说明”介绍用途与操作顺序，“相关功能”用于继续当前业务；首页提供日常运维入口和首次设备接入步骤。

- 菜单使用“模型管理”“智能助手”“知识库”，原有接口与导航键不变。
- 固定文案与已知状态使用中文；协议原值在 `iot_front/src/presentation.js` 中映射为显示名称，不修改请求值。设备标识、服务地址、模型标识、文件名及可复制的源码和原始报文保留原值。
- 页面标题、用途与步骤集中维护在 `iot_front/src/pageGuide.js`。新增功能需补充对应说明。
- 设备列表优先展示运行状态，编辑设备与轮换凭证位于“更多操作”。窄屏下左右滑动表格查看其余列，操作列不固定遮挡正文；页面内容和弹窗正文分别滚动。
- 原始数据和开发示例可包含协议字段代码，不应翻译后用于发送。常见请求失败显示中文说明，错误代码、追踪信息及原始错误仍保留在错误对象中。

界面独立验收：在 `iot_front` 执行 `npm test` 与 `npm run build`，随后执行 `node tests/browser/ui-preview.mjs`，访问终端显示的本机地址。该工具只展示合成数据；登录按钮进入模拟账户，其余写操作返回不可用。它不连接业务后端，不证明真实设备接入、模型调用或备份恢复成功。

本次界面验证（2026-09-10，本机）：前端测试 59 项通过，构建通过，保留主入口超过五百千字节的体积提示；浏览器使用合成数据走查全部 15 个主页面，并在 390 × 844 窄屏核对页面宽度，检查功能搜索、设备向导、设备操作菜单、中文确认框和弹窗独立滚动。未执行真实业务后端、现场设备、模型服务及备份恢复验收。需要真实后端的既有浏览器脚本已同步可见文案，本次仅检查这些脚本的语法。
