# 离线部署

离线部署分两步：有网机器一键打包，目标机器一键导入并启动。目标机器不需要 Go、Node.js 或源码依赖；打包机和目标机都需预装并启动 Docker Engine / Docker Desktop（Linux 容器）及 Docker Compose 2.24.4+。Linux/macOS 的部署健康检查还需要 curl。

打包机与目标机应使用相同 CPU 架构（例如均为 linux/amd64）；Apple Silicon 默认生成的 ARM64 镜像不能直接作为 x86 服务器离线包。CentOS 7.9 x86_64 可运行 linux/amd64 包，前提是已安装并启动 Docker Engine、Docker Compose v2，并有 curl。离线服务器安装 Docker 所需的软件包必须提前准备，本项目离线包不包含 Docker 安装程序。

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
