# 炬联 TorchLink 技术详情

[返回 README](../README.md) · [部署维护](DEPLOYMENT.md) · [离线交付](OFFLINE_DEPLOYMENT.md)

本文提供环境准备、启动和开发入口。除特别标注外，命令在仓库根目录执行。

## 环境与方案选择

| 方案 | 运行方式 | 需要准备 | 默认 Web 地址 |
| --- | --- | --- | --- |
| 本地开发 | 本机运行 Go API 和 Vue，容器运行依赖 | Go、Node.js；本机或可达的依赖机 | `http://localhost:5173` |
| 在线部署 | 目标机构建并运行容器 | Docker、网络；Linux 脚本可自动安装缺失组件 | `http://服务器IP:8080` |
| 离线部署 | 有网打包，目标机导入容器及模型 | 与目标 CPU 架构匹配的完整离线包 | `http://服务器IP:8080` |

- Go **1.25.5**，以 [go.mod](../go.mod) 为准；Node.js **`^20.19.0 || >=22.12.0`**，以 [package.json](../iot_front/package.json) 为准。前端使用 npm 和仓库锁文件。
- Windows / macOS 容器部署需先启动 Docker Desktop，使用 Linux 容器；macOS 本地依赖也可运行在 OrbStack。
- Linux 自动安装适用于使用 systemd 的 amd64 / arm64 系统，需要 root 或 sudo。系统依赖与 Compose 版本见 [Docker 自动安装](OFFLINE_DEPLOYMENT.md#docker-自动安装)。
- 默认部署包含 PostgreSQL、Redis、ClickHouse、Redpanda、EMQX、MinIO、Ollama、Weaviate 和 Harness；在线 / 离线另含备份服务。按设备频率、保留时间、模型和并发量评估资源。

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

脚本生成 `.env.local` 和随机凭据，启动依赖与 Harness，准备知识库模型，执行 `go mod download` 和 `npm ci`。重复执行复用已有配置与数据。

| 需求 | PowerShell 参数 | Bash 参数 |
| --- | --- | --- |
| 使用本地 Ollama 对话模型 | `-IncludeAi` | `--include-ai` |
| 只准备依赖，不下载源码依赖 | `-SkipCodeDeps` | `--skip-code-deps` |
| 临时运行容器版备份服务 | `-IncludeBackup` | `--include-backup` |

源码方案默认使用 DeepSeek API：在 `.env.local` 填写 `DEEPSEEK_API_KEY` 后重跑准备脚本，使 Harness 加载配置；使用本地模型则加 `--include-ai` / `-IncludeAi`。在线与离线方案默认使用 Ollama `qwen3:1.7b`，具体地址和模型切换见 [AI 配置](DEPLOYMENT.md#ai-与工作流)。

依赖容器与源码分开运行时，在 Linux 依赖机执行：

```bash
sudo bash ./scripts/setup-local.sh --skip-code-deps \
  --dependency-host <源码机可访问的依赖机地址> \
  --api-host <依赖容器可访问的源码机地址>
```

安全复制生成的 `.env.local` 到源码机仓库根目录。此模式会开放依赖端口，供可信网络使用；Kafka 公告地址和 Harness 回调须从各自调用端可达。详细网络配置和 OrbStack 示例见 [端口与地址](DEPLOYMENT.md#端口与地址)。

### 日常运行代码

在三个独立终端运行：

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
# 终端三：需要备份功能时启动
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

脚本生成 `.env.online`，构建镜像，准备本地模型，启动并检查服务。首次需要访问镜像、Go/npm 依赖、Harness 源码和模型源；服务器无需预装 Go 或 Node.js。更新源码后重跑同一脚本，沿用原配置、Compose 项目和数据卷。使用自定义旧环境时，先按 [配置与数据归属](DEPLOYMENT.md#配置与数据归属) 指定原参数。

## 离线部署

有网机器在仓库根目录执行 `bash ./scripts/package-offline.sh`，Windows 使用 `powershell -ExecutionPolicy Bypass -File .\scripts\package-offline.ps1`。

将 `offline-bundles/iot-platform-offline-*` **整个生成目录**复制到目标机，包括 `.env.offline`。在离线包根目录执行：

```bash
# Linux
sudo bash ./scripts/deploy-offline-linux.sh
# macOS；先启动 Docker Desktop
bash ./scripts/deploy-offline-macos.sh
```

Windows 使用 `powershell -ExecutionPolicy Bypass -File .\scripts\deploy-offline.ps1`。架构、基础依赖、模型选项、校验与更新步骤统一见 [离线部署](OFFLINE_DEPLOYMENT.md)。

## 登录与专题入口

默认租户 `tenant_001`、用户名 `admin`，密码读取相应环境文件中的 `IOT_ADMIN_PASSWORD`。本地、在线、离线配置相互独立，不能混用凭据和数据卷。

| 任务 | 文档 |
| --- | --- |
| 端口、模型、状态、停止与备份 | [部署维护](DEPLOYMENT.md) |
| 添加设备、上报、凭据和命令 | [统一设备接入](UNIFIED_DEVICE_ONBOARDING.md) |
| Go 协议与主子设备 | [协议包](GO_PROTOCOL_PACKAGES.md) · [TCP 接入](TCP_CHILD_DEVICE_ACCESS.md) |
| 拆分 API / Gateway | [独立接入进程](EDGE_AND_GATEWAY.md) |
| 跨平台分发源码 | [协议目录](PROTOCOL_CATALOG.md) · [组织审核](PRIVATE_PROTOCOL_MARKET.md) |
| 升级旧节点、拓扑和影子配置 | [旧版本迁移](EDGE_REMOVAL.md) |

## 源码与开发检查

| 入口 | 职责 |
| --- | --- |
| `cmd/iot-platform/`、`internal/platformapp/` | API 启动和依赖装配 |
| `internal/httpapi/`、`internal/core/`、`internal/adapters/` | 接口、业务、外部存储与服务 |
| `internal/protocolbuild/`、`internal/protocolruntime/`、`internal/protocolworker/` | 协议编译、连接运行时和 Worker |
| `iot_front/` | [Vue 管理端](../iot_front/README.md)；[分页与关联选择](LIST_PAGINATION.md) |
| `protocol-packages/gb26875-dahua/` | 可独立维护的协议 module |
| `scripts/`、`deploy/`、`compose*.yaml` | 准备、部署与打包配置 |

按改动范围执行，不必为文档变更运行全套服务：

| 执行目录 | 命令 | 验证范围 |
| --- | --- | --- |
| 仓库根目录 | `go test ./...` | 根 Go module；不包含独立协议 module |
| `protocol-packages/gb26875-dahua` | `go test ./...` | 独立协议及模拟器 |
| `iot_front` | `npm test`、`npm run build` | 前端测试及构建；没有独立 lint/typecheck 脚本 |
| 仓库根目录 | `git diff --check` | 空白错误 |

本地集成入口：`go run scripts/tests/local-runtime-smoke.go --env-file .env.local` 检查依赖读写；前端、API 和备份启动后，`node scripts/tests/local-business-smoke.mjs` 检查登录、接入、归档、规则及回放。业务冒烟会创建唯一测试数据，结束停用本次规则与凭据并保留记录；仅在测试环境运行。

真实 PostgreSQL、MQTT 与浏览器用例的环境变量见 [接入验证](UNIFIED_DEVICE_ONBOARDING.md#验证入口)。部署脚本检查见 [部署排查入口](DEPLOYMENT.md#排查入口)。未配置而跳过的用例不算联调通过，历史测试记录可从 Git 历史追溯。
