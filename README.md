# 消防 IoT 平台

Go API + Vue 3 管理端，提供设备接入、Go 协议源码上传与版本切换、Modbus 点表采集、告警、原始报文查询与回放、知识库和 AI 辅助运维。

## 选择方案

| 方案 | 前后端运行位置 | 依赖准备 | 访问地址 |
|---|---|---|---|
| **本地运行** | 本机运行源码，可用 IDE 调试 | 一键启动依赖容器、下载 Go/npm 依赖 | `http://localhost:5173` |
| **在线部署** | 全部运行在 Docker | 一键生成配置、下载/构建镜像并启动 | `http://服务器IP:8080` |
| **离线部署** | 全部运行在 Docker | 有网机器打包，离线服务器一键导入并启动 | `http://服务器IP:8080` |

以下命令均在**本仓库根目录**执行。三种方案都需要已安装并启动 Docker（Linux 容器）及 Docker Compose v2；本地运行另需 **Go ≥ 1.25.5** 和 **Node.js 22 ≥ 22.12**（也兼容 Node.js 20 ≥ 20.19）。脚本检查运行环境，不负责安装 Docker、Go 或 Node.js。

默认包含 PostgreSQL、Redis、ClickHouse、Redpanda、EMQX、MinIO、备份服务、Ollama/Weaviate、`nomic-embed-text` 嵌入模型和 AI 工作流 Harness。在线与离线部署还会自动准备 `qwen3:1.7b`；告警研判、规则辅助和 AI 工作流统一使用该本地模型，不需要 API Key。这个模型下载约 1.4 GB，适合 8 GB 内存的整套虚拟机环境。

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

脚本生成带逐项中文说明的 `.env.local`，启动依赖容器、AI 工作流 Harness、初始化消息主题与知识库模型，并执行 `go mod download` 和 `npm ci`。本地源码方案默认使用 DeepSeek API；把 Key 填入 `DEEPSEEK_API_KEY` 后重跑一次脚本，使 Harness 容器加载密钥。需要改用本地模型时可传 `-IncludeAi` / `--include-ai`，或启动后在“AI 模型管理”菜单切换；再次执行会复用其他配置和数据。若只需启动依赖，可加 `-SkipCodeDeps` / `--skip-code-deps`。

如果依赖容器运行在 Linux 虚拟机、源码运行在 Windows，Linux 使用 Windows 可访问的虚拟机地址启动：

```bash
bash ./scripts/setup-local.sh --skip-code-deps --dependency-host <虚拟机IP> --api-host <Windows在虚拟机网段的IP>
```

`--dependency-host` 会开放依赖端口，并让 PostgreSQL、Kafka、MQTT、MinIO、ClickHouse、Ollama、Weaviate、备份服务和 Harness 使用虚拟机地址；`--api-host` 供 Harness 容器回调 Windows 上的 API，VMware NAT 环境通常填写对应虚拟网卡的主机地址。将 Linux 生成的 `.env.local` 安全复制到 Windows 仓库根目录，确认 `DEEPSEEK_API_KEY` 已填写，再按下面的日常命令运行源码。只应在可信的主机专用或局域网中使用此模式。

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

Windows PowerShell 若提示 npm 脚本执行策略错误，改用 `npm.cmd run dev`。访问 **http://localhost:5173**，前端自动代理到本机 API `8081` 端口。

GoLand 调试时，工作目录设为仓库根目录、程序设为 `cmd/iot-platform`，程序参数填 `--env-file .env.local`，无需手工复制数据库地址。已有进程环境变量优先于配置文件；旧 IDE 配置中的硬编码密码或地址应先移除。

### VS Code 一键启动

仓库已提供 [`.vscode/launch.json`](.vscode/launch.json)。安装 VS Code Go 扩展并确保 `.env.local` 已生成或已从依赖机复制后，打开“运行和调试”，选择 **IoT Platform (API + Web)**，按 `F5` 即可同时启动 Go API 和 Vue 前端。需要国标网关时选择 **IoT Platform + GB26875 Gateway**。配置文件只引用 `.env.local`，不包含密码；Windows 使用 `npm.cmd`，Linux/macOS 使用 `npm`。

## 2. 在线部署

把本仓库源码放到有网络的目标服务器，在仓库根目录执行：

Windows PowerShell：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\deploy-online.ps1
```

Linux / macOS：

```bash
bash ./scripts/deploy-online.sh
```

脚本首次生成带逐项中文说明的 `.env.online`，拉取基础镜像、构建 API/Web/备份服务和 Harness，下载 `qwen3:1.7b` 与知识库模型，启动服务并检查 API、网页和 Harness。完成后访问 **http://服务器IP:8080**。服务器无需安装 Go 或 Node.js；首次构建需要访问镜像仓库、Go/npm 依赖源、Harness 源码和 Ollama 模型源。

更新源码后重新执行同一命令即可构建并更新服务。已有凭据保持不变，数据库数据保存在 Docker 命名卷中。

## 3. 离线部署

离线部署分为“有网打包”和“无网安装”两个步骤。打包机与服务器的 Docker CPU 架构必须一致，目标服务器须提前安装好 Docker Engine、Compose v2 和 curl。CentOS 7.9 x86_64 可以直接运行 linux/amd64 离线包；系统无需 Go、Node.js、Git 和外网，但 Docker 及 Compose 安装包不包含在离线包内。

### 有网机器：一键打包

Windows PowerShell：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\package-offline.ps1
```

Linux / macOS：

```bash
bash ./scripts/package-offline.sh
```

输出位于 `offline-bundles/iot-platform-offline-*`，包括全部运行镜像、`qwen3:1.7b`、知识库模型、Harness、校验文件、配置和部署脚本。将**整个生成目录**复制到离线服务器。

### 离线服务器：一键安装

进入复制后的离线包目录，Windows PowerShell 执行：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\deploy-offline.ps1
```

Linux / macOS 执行：

```bash
bash ./scripts/deploy-offline.sh
```

脚本校验并导入镜像和模型，使用 `--no-build --pull never` 启动，随后检查服务。完成后访问 **http://服务器IP:8080**。目标服务器不需要源码、Go、Node.js 或网络。

离线更新时，用目标环境原有 `.env.offline` 打包以保持凭据一致。模型、可选组件、完整参数及更新步骤见 [离线部署说明](docs/OFFLINE_DEPLOYMENT.md)。

## 登录与配置

- 默认租户：`tenant_001`；用户名：`admin`。
- 密码为对应环境文件中的 `IOT_ADMIN_PASSWORD`：本地 `.env.local`，在线 `.env.online`，离线包 `.env.offline`。首次脚本会生成随机凭据。
- 不同方案使用独立 Compose 项目和数据卷；默认端口有重叠，同一台机器上不要同时启动多套默认配置。
- 配置文件应随数据库备份妥善保管。已有数据库卷时，直接改文件中的数据库密码不会同步修改库内账号。
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
