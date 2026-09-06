# 消防 IoT 平台

Go API + Vue 3 管理端，提供设备接入、Go 协议源码上传与版本切换、Modbus 点表采集、告警、原始报文查询与回放、知识库和 AI 辅助运维。

## 选择方案

| 方案 | 前后端运行位置 | 依赖准备 | 访问地址 |
|---|---|---|---|
| **本地运行** | 本机运行源码，可用 IDE 调试 | 一键启动依赖容器、下载 Go/npm 依赖 | `http://localhost:5173` |
| **在线部署** | 全部运行在 Docker | 一键生成配置、下载/构建镜像并启动 | `http://服务器IP:8080` |
| **离线部署** | 全部运行在 Docker | 有网机器打包，离线服务器一键导入并启动 | `http://服务器IP:8080` |

以下命令均在**本仓库根目录**执行。三种方案都需要已安装并启动 Docker（Linux 容器）及 Docker Compose v2；本地运行另需 **Go ≥ 1.25.5** 和 **Node.js 22 ≥ 22.12**（也兼容 Node.js 20 ≥ 20.19）。脚本检查运行环境，不负责安装 Docker、Go 或 Node.js。

默认包含 PostgreSQL、Redis、ClickHouse、Redpanda、EMQX、MinIO、备份服务，以及 Ollama/Weaviate 知识库依赖和 `nomic-embed-text` 嵌入模型。对话模型和 DeepSeek Harness 按需开启，基础设备与告警功能不要求云端 AI 密钥。

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

脚本生成 `.env.local`，启动依赖容器、初始化消息主题与知识库模型，并执行 `go mod download` 和 `npm ci`。API 和前端由你在本机运行。再次执行会复用配置和数据；若只需启动依赖，可加 `-SkipCodeDeps` / `--skip-code-deps`。

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

GoLand / VS Code 调试时，工作目录设为仓库根目录、程序设为 `cmd/iot-platform`，程序参数填 `--env-file .env.local`，无需手工复制数据库地址。已有进程环境变量优先于配置文件；旧 IDE 配置中的硬编码密码或地址应先移除。

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

脚本首次生成 `.env.online`，拉取基础镜像、构建 API/Web/备份服务、准备知识库模型、启动服务并检查 API 与网页。完成后访问 **http://服务器IP:8080**。服务器无需安装 Go 或 Node.js；首次构建需要访问镜像仓库、Go/npm 依赖源和 Ollama 模型源。

更新源码后重新执行同一命令即可构建并更新服务。已有凭据保持不变，数据库数据保存在 Docker 命名卷中。

## 3. 离线部署

离线部署分为“有网打包”和“无网安装”两个步骤。打包机与服务器的 Docker CPU 架构必须一致，目标服务器须提前安装好 Docker 和 Compose。

### 有网机器：一键打包

Windows PowerShell：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\package-offline.ps1
```

Linux / macOS：

```bash
bash ./scripts/package-offline.sh
```

输出位于 `offline-bundles/iot-platform-offline-*`，包括全部运行镜像、知识库模型、校验文件、配置和部署脚本。将**整个生成目录**复制到离线服务器。

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
| [GB/T 26875](docs/GB26875_DAHUA_V103.md) | 国标网关接入 |

开发检查：在仓库根目录执行 `go test ./...`；在 `iot_front` 执行 `npm test` 和 `npm run build`。摄像头模块仅管理元数据和设备关联，视频流由外部视频平台提供。
