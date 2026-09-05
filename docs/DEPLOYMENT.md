# 部署配置与维护

首次运行命令见 [README](../README.md)。本文用于选择可选组件、修改配置和维护已有环境。

## 配置与数据归属

| 方案 | 配置文件 | Compose 文件 | 项目名 / 数据卷前缀 |
|---|---|---|---|
| 本地运行 | `.env.local` | `compose.local.yaml` | `iot-platform-local` |
| 在线部署 | `.env.online` | `compose.yaml` | `iot-platform-online` |
| 离线部署 | 离线包内 `.env.offline` | `compose.yaml` + `compose.offline.yaml` | `iot-platform` |

脚本显式选择配置和 Compose 文件，在线/离线部署不会自动加载用于旧版本地调试的 `compose.override.yaml`。

首次执行生成随机凭据，重复执行保留已有凭据；显式指定 AI 开关时只调整对应功能配置。不要重新生成配置文件来“重置”已有数据库。配置文件和离线包包含凭据，不应提交或公开分享。

**已有部署沿用原项目和凭据。** 新默认项目名会创建一套新数据卷，不会自动迁移旧数据。例如原服务用项目 `iot-platform`、配置 `.env`，在线更新应执行：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\deploy-online.ps1 -EnvFile .env -ProjectName iot-platform
```

```bash
bash ./scripts/deploy-online.sh --env-file .env --project-name iot-platform
```

已有自定义 Compose 覆盖文件、外部数据卷或外部数据库时，先核对原部署参数；上述命令只使用 `compose.yaml`。

## 可选 AI

### 本地 Ollama 对话模型

三个准备入口 `setup-local`、`deploy-online`、`package-offline` 均支持 `-IncludeAi` / `--include-ai`。

以在线部署为例：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\deploy-online.ps1 -IncludeAi
```

```bash
bash ./scripts/deploy-online.sh --include-ai
```

默认额外下载 `qwen3:8b`。本地/在线脚本在 Provider 未启用或已为 Ollama 时启用本地模型；已配置远程 Provider 时保留它。在线环境可用 `IOT_OLLAMA_MODEL`（已有 Ollama 配置优先用 `IOT_AI_MODEL`）选择模型；本地与离线打包可用 `-OllamaModel` / `--ollama-model`。模型运行所需内存取决于所选模型。

知识库嵌入模型 `nomic-embed-text` 始终准备，不需要 `IncludeAi`。Ollama Provider 用于告警研判等后端能力；“AI 工作流”页面的 Agent 对话另走 Harness。

### DeepSeek Harness 工作流

三个准备入口也支持 `-IncludeHarness` / `--include-harness`。在线和本地会获取锁定的 Harness 源码并构建侧车，需要 Git 和网络：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\deploy-online.ps1 -IncludeHarness
```

```bash
bash ./scripts/deploy-online.sh --include-harness
```

在对应环境文件设置 `DEEPSEEK_API_KEY`，再用相同参数运行脚本使配置生效。内部 Token、侧车 URL 和 MCP 回调地址由脚本准备。Harness 健康只说明运行时就绪；实际工作流仍需要可达的模型服务和有效 API Key。

Harness 的模型地址使用独立变量 `DEEPSEEK_BASE_URL`，默认 `https://api.deepseek.com`。旧环境若通过 `IOT_AI_BASE_URL` 指定了 Harness 私有代理，请将该地址补到 `DEEPSEEK_BASE_URL`；`IOT_AI_BASE_URL` 继续用于后端告警 Provider。

若告警研判也要走 DeepSeek，在环境文件另外设置：

```dotenv
IOT_AI_PROVIDER=deepseek
IOT_AI_BASE_URL=https://api.deepseek.com
IOT_AI_MODEL=deepseek-v4-flash
```

完全断网环境优先使用离线包内的 Ollama；带入 Harness 镜像不会让云端 DeepSeek API 离线可用。更多说明见 [AI 工作流](AI_PLUGIN_HARNESS.md)。

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
| 备份服务 / Harness | `8092` / `8091` | `8092` / `8091`，仅宿主机 |

本地依赖端口只绑定 `127.0.0.1`，供本机代码和模拟设备使用。局域网真实设备接入需要按需修改 `compose.local.yaml` 中的 MQTT 监听绑定；API 设备上报使用运行 Go 的主机地址。Kafka 通过独立 external listener 向本机返回 `127.0.0.1:19092`，容器间仍使用 `redpanda:9092`。

本地 API 默认参数写在 `.env.local`；修改 API 端口时同步修改前端 `VITE_API_PROXY_TARGET`，使用 Harness 时还需同步其 MCP 回调和允许的 Origin。`--env-file` 读取字面的 `KEY=VALUE`，支持注释和单/双引号，不展开 `${变量}` 或执行 shell；已有进程环境变量优先。

在线/离线默认提供 HTTP 服务。需要公网域名与 HTTPS 时，由现有 Nginx/网关终结 TLS 并转发到 Web 端口；部署脚本不管理域名和证书。

## 查看状态、日志与停止

本地依赖：

```bash
docker compose -p iot-platform-local --env-file .env.local -f compose.local.yaml ps
docker compose -p iot-platform-local --env-file .env.local -f compose.local.yaml logs --tail=100
docker compose -p iot-platform-local --env-file .env.local -f compose.local.yaml down
```

在线部署：

```bash
docker compose -p iot-platform-online --env-file .env.online -f compose.yaml ps
docker compose -p iot-platform-online --env-file .env.online -f compose.yaml logs -f platform-api platform-web backup-service
docker compose -p iot-platform-online --env-file .env.online -f compose.yaml down
```

启用 Harness 时，在子命令 `ps` / `logs` / `down` 前加 `--profile harness`。自定义项目名和配置路径时，上述命令也要使用相同参数。离线包的维护命令见 [离线部署说明](OFFLINE_DEPLOYMENT.md)。

`down` 保留命名数据卷；`down -v` 会删除它们。日常代码更新重跑对应部署脚本即可。备份页面调用独立 `backup-service`，默认每天上海时间 `00:05` 汇总前一天原始日志，可由 `IOT_BACKUP_TIME`、`IOT_BACKUP_TIMEZONE` 调整；这是业务数据备份，不替代环境配置文件的保管。

## 排查入口

| 现象 | 先检查 |
|---|---|
| 提示找不到 Docker / Engine 不可用 | Docker 是否安装、启动并使用 Linux 容器 |
| 容器端口被占用 | 是否同时运行旧环境或另一套部署；先停止冲突实例 |
| 数据库认证失败 | 当前项目名、数据卷与配置文件是否来自同一环境 |
| 本地 Kafka 无法连接 | 是否运行 `compose.local.yaml`，API 是否读取 `.env.local` |
| API 启动后前端无法访问 | `8081` 端口、Vite 代理、旧 IDE 环境变量覆盖 |
| 知识库索引失败 | Ollama 模型下载是否完成，Weaviate 与 Ollama 日志 |
| 工作流失败但 Harness 健康 | API Key、模型可达性和 MCP 回调地址 |

API `/health/live` 检查进程存活，`/health/ready` 检查已配置的存储、消息和知识库依赖。脚本和配置校验通过不等于真实设备、生产容量或目标离线环境已经验收。

维护部署脚本时，可运行 `scripts/tests/deployment-smoke.ps1 -ComposeExe <独立Compose程序路径>` 或 `bash scripts/tests/deployment-smoke.sh <独立Compose程序路径>`。它们使用真实 Compose 解析配置，模拟 Docker 和 HTTP 操作，检查一键流程与失败分支，不会启动服务。
