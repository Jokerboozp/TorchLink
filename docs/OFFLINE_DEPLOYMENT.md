# 离线部署

离线部署分两步：有网机器一键打包，目标机器一键导入并启动。目标机器不需要 Go、Node.js 或源码依赖；打包机需预装并启动 Docker Engine / Docker Desktop（Linux 容器）及 Docker Compose 2.24.4+；Linux 目标机缺少 Docker 时会自动从离线包安装。Windows/macOS 目标机仍需预装 Docker Desktop。Linux/macOS 的部署健康检查还需要 curl。

打包机与目标机应使用相同 CPU 架构（例如均为 linux/amd64）；Apple Silicon 默认生成的 ARM64 镜像不能直接作为 x86 服务器离线包。CentOS 7.9 x86_64 使用 linux/amd64 包；新包默认携带 Linux Docker、Compose 和 Buildx 的安装文件与 SHA-256 校验值，首次安装请用 root 或 sudo 执行。

## 1. 有网机器打包

在 `platform` 目录执行：

```powershell
# Windows
powershell -ExecutionPolicy Bypass -File .\scripts\package-offline.ps1
```

```bash
# Linux / macOS
bash ./scripts/package-offline.sh
```

默认打包平台、存储、消息、备份、监控、Ollama、Weaviate、AI 工作流 Harness、`qwen3:1.7b` 对话模型，以及知识库必需的 `nomic-embed-text` 嵌入模型。无须预先创建 `.env`；脚本先生成独立的 `.env.offline` 和随机凭据，再构建镜像。告警研判和 AI 工作流默认共用 `qwen3:1.7b`，整个运行过程不访问外网。

生成目录：`platform/offline-bundles/iot-platform-offline-时间戳/`。将整个目录复制到目标机器，包括隐藏文件 `.env.offline`。镜像、模型文件、配置和部署脚本必须一起传输。

常用选项：

| 用途 | PowerShell | Bash |
| --- | --- | --- |
| 使用已有配置 | `-EnvFile .\.env.production` | `--env-file ./.env.production` |
| 选择对话模型（默认 `qwen3:1.7b`） | `-OllamaModel qwen3:4b` | `--ollama-model qwen3:4b` |
| 跳过模型归档（目标机已有模型时） | `-SkipOllamaModel` | `--skip-ollama-model` |
| 输出父目录 | `-OutputDir D:\offline-bundles` | `--output-dir /data/offline-bundles` |

已有配置会保留业务地址和模型设置；如果配置已启用 Ollama，会自动携带实际配置的对话模型（`IOT_AI_MODEL` 优先于 `IOT_OLLAMA_MODEL`），并让 Harness 使用同一模型。使用 `-EnvFile` 时仍需确保内网地址和所选组件匹配。示例密码和空的必需密钥会被拒绝。

`-SkipOllamaModel` / `--skip-ollama-model` 仅适用于目标机的 `iot-platform_ollama-data` 卷已经包含所需模型；部署默认会检查模型是否存在。当前知识库固定使用 `nomic-embed-text`，不能随意替换嵌入模型。8 GB 环境建议保留 `qwen3:1.7b`；更换更大模型前应先评估其内存占用。

打包时模型缓存保存在 `iot-platform-offline-build_ollama-data` 卷，完成后停止打包用 Ollama；不操作已有 `iot-platform` 部署。模型归档仅包含模型文件，不包含 Ollama 身份密钥。重复打包可复用缓存；曾下载的其他模型也可能保留在归档中。

## 2. 目标机器一键部署

进入复制后的离线包目录：

```powershell
# Windows
powershell -ExecutionPolicy Bypass -File .\scripts\deploy-offline.ps1
```

```bash
# Linux / macOS
bash ./scripts/deploy-offline.sh
```

脚本依次校验镜像和模型的 SHA-256、检查 Compose 配置、导入镜像、检查所需镜像、恢复模型、启动服务并检查平台及模型。启动固定使用 `--no-build --pull never`；模型恢复容器也禁止拉取镜像。

默认 Web 地址是 `http://服务器IP:8080`。管理员凭据见 `OFFLINE-CREDENTIALS.txt`；使用外部配置打包时，凭据仍以该配置为准。若需改端口，请先编辑包内 `.env.offline` 的 `IOT_WEB_PORT` 和 `IOT_API_PORT`。整个离线包包含密码，请限制访问和传输范围。

同一个包可以重复执行部署命令：配置与数据卷保持原值，模型恢复只补齐缺失文件。模型解压会临时额外占用一份模型大小的磁盘空间。不要用重新生成随机凭据的新包直接替换已有数据库部署；升级时应沿用原来的 `.env.offline` 作为打包配置。

离线部署项目名固定为 `iot-platform`，本地运行和在线部署使用各自的项目名；同一台机器运行多套系统时仍需调整重叠的宿主机端口。

## 查看状态与排错

在离线包目录执行（Windows / Linux / macOS 通用）：

```text
docker compose --project-name iot-platform --env-file .env.offline -f compose.yaml -f compose.offline.yaml ps
docker compose --project-name iot-platform --env-file .env.offline -f compose.yaml -f compose.offline.yaml logs --tail=100 platform-api
```

- 缺少镜像或 SHA-256 不匹配：在有网机器重新打包并完整复制，不要在离线目标机执行拉取。
- 缺少 `nomic-embed-text` 或对话模型：重新携带模型打包，再部署到原目录/配置；无需删除已有模型卷。
- 需要自行诊断：可显式使用 `-SkipHealthCheck` / `--skip-health-check`；只在确认传输完整性后使用 `-SkipHashCheck` / `--skip-hash-check`。跳过检查不代表部署验收通过。
- 已有业务数据迁移：使用项目备份及数据库、对象存储恢复流程；离线安装包只包含程序、配置和模型，不包含业务数据。

GB26875 等协议统一上传 Go 源码包，并在平台启用通用 TCP/UDP 监听实例；新离线包不再打包专用 GB 网关。默认映射 26875，可用 IOT_PROTOCOL_PORTS 预留同号端口范围。


## Docker 自动安装

Linux 在线部署与离线部署共用检测逻辑：已有可用 Docker 和 Compose 2.24.4+ 时直接复用；仅缺 Compose 时只补插件；Docker 服务未启动时尝试启动。在线构建还会检测 Buildx。脚本不删除数据卷、不更改现有 daemon.json，也不会自动升级或降级已有 Docker Engine。

- 在线：从 Docker / docker GitHub 官方地址下载缺失文件，安装后继续部署。
- 离线：仅读取包内 `docker-runtime/`，校验架构和 SHA-256 后安装，不会访问下载地址或软件源。旧包若没有安装文件且目标机缺少 Docker，需在有网机器重新打包。
- 首次安装使用 Linux 静态二进制和 systemd；amd64 / arm64 均支持。3.x / 4.x 内核选择 Docker 24.0.9 兼容分支，其余选择 28.5.2；Compose 2.27.3，Buildx 0.14.1。旧内核兼容分支不代表 CentOS 7 仍受 Docker 官方维护。静态安装方式见 [Docker 官方说明](https://docs.docker.com/engine/install/binaries/)。
- Linux 系统需已有 systemd、tar、iptables、xz 和 ps；健康检查需 curl。在线安装会通过系统包管理器补充 iptables/xz/procps，CentOS 7 使用临时 Vault 源，不覆盖已有 yum 配置。
- 精简离线系统若缺少这些基础包，打包时使用 `--docker-packages-dir /path/to/packages` 或 `-DockerPackagesDir C:\\packages` 加入与目标发行版、版本和架构匹配的 RPM/DEB 及全部依赖。安装使用本地 rpm/dpkg，不联网解决依赖、不跳过依赖检查。
- 已有 Docker 的服务器可通过打包参数 `--skip-docker-runtime` / `-SkipDockerRuntime` 减小包体。该参数不适用于尚未安装 Docker 的目标机。

Linux 首次部署示例：

```bash
# 在线：在仓库根目录执行
sudo bash ./scripts/deploy-online.sh
# 离线：在新离线包根目录执行
sudo bash ./scripts/deploy-offline-linux.sh
```

Windows/macOS 的 Docker Desktop 安装与虚拟化设置不在此 Linux 自动安装流程内；PowerShell 打包脚本仍会准备 Linux 安装文件，方便在 Windows 打包后拷贝到 CentOS。
