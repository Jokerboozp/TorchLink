# 部署配置与维护

首次运行命令见 [技术详情](TECHNICAL_DETAILS.md)，项目功能见 [README](../README.md)。本文用于选择可选组件、修改配置和维护已有环境。

## 配置与数据归属

| 方案 | 配置文件 | Compose 文件 | 项目名 / 数据卷前缀 |
|---|---|---|---|
| 本地运行 | `.env.local` | `compose.local.yaml` | `iot-platform-local` |
| 在线部署 | `.env.online` | `compose.yaml` | `iot-platform-online` |
| 离线部署 | 离线包内 `.env.offline` | `compose.yaml` + `compose.offline.yaml` | `iot-platform` |

脚本显式选择配置和 Compose 文件，在线/离线部署不会自动加载用于旧版本地调试的 `compose.override.yaml`。

首次执行生成随机凭据，并在每个配置项前写入中文说明；重复执行保留已有值并补齐说明。不要重新生成配置文件来“重置”已有数据库。配置文件和离线包包含凭据，不应提交或公开分享。

**已有部署沿用原项目和凭据。** 新默认项目名会创建一套新数据卷，不会自动迁移旧数据。例如原服务用项目 `iot-platform`、配置 `.env`，在线更新应执行：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\deploy-online.ps1 -EnvFile .env -ProjectName iot-platform
```

```bash
bash ./scripts/deploy-online.sh --env-file .env --project-name iot-platform
```

已有自定义 Compose 覆盖文件、外部数据卷或外部数据库时，先核对原部署参数；上述命令只使用 `compose.yaml`。

## AI 与工作流

在线和离线方案默认使用本地 Ollama `qwen3:1.7b`，Harness 默认启动。告警自动研判、规则辅助和 AI 工作流使用同一模型，不需要 API Key。默认配置为：

```dotenv
IOT_OLLAMA_URL=http://ollama:11434
IOT_OLLAMA_MODEL=qwen3:1.7b
IOT_AI_PROVIDER=ollama
IOT_AI_BASE_URL=http://ollama:11434
IOT_AI_MODEL=qwen3:1.7b
IOT_AI_HARNESS_ENABLED=true
IOT_AI_HARNESS_PROVIDER=ollama
IOT_AI_HARNESS_OLLAMA_BASE_URL=http://ollama:11434/v1
IOT_AI_HARNESS_CONTEXT_WINDOW=8192
IOT_AI_HARNESS_MODEL=qwen3:1.7b
```

自动研判使用 Ollama 原生地址，所以 `IOT_AI_BASE_URL` 不带 `/v1`；Harness 使用 OpenAI 兼容接口，所以 `IOT_AI_HARNESS_OLLAMA_BASE_URL` 必须带 `/v1`。修改模型时应同步 `IOT_OLLAMA_MODEL`、`IOT_AI_MODEL` 和 `IOT_AI_HARNESS_MODEL`。将 `IOT_AI_HARNESS_ENABLED` 设为 `false` 并重跑脚本可关闭 Harness。

### 本地 Ollama 对话模型

在线脚本默认下载对话模型；离线打包脚本默认把模型数据写入离线包。本地源码调试若要从 DeepSeek 切换到本地 Ollama，仍可使用 `-IncludeAi` / `--include-ai`：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\setup-local.ps1 -IncludeAi
```

```bash
bash ./scripts/setup-local.sh --include-ai
```

默认模型 `qwen3:1.7b` 的 Ollama 下载大小约 1.4 GB，在整套平台、数据库和消息组件同时运行的 8 GB 虚拟机上比 4B/8B 模型更稳妥。`qwen3:0.6b` 更小，但告警结构化输出和多步工具调用能力更弱。需要更强效果且内存充足时，可在配置或离线打包参数中改用更大模型。

知识库嵌入模型 `nomic-embed-text` 始终准备。Ollama Provider 与 Harness 是两条调用链，但默认连接同一个 Ollama 服务和模型。

本地开发的 `--include-deepseek` 与 `--include-ai` 只能二选一。在线脚本上的 `IncludeAi` 参数仅用于把旧配置强制切回本地 Ollama，新配置无须传该参数。

### 在界面切换 AI 模型服务

管理员打开独立的“AI 模型管理”菜单后，可以选择“本地模型（Ollama）”“DeepSeek 云端模型”或“兼容接口模型”，填写服务地址和模型名称。先点击“测试配置”，平台会向候选模型发送一条测试请求并显示响应和耗时；测试只验证配置，不会改变当前生效模型。确认结果后，再点击“应用配置”，平台会健康检查并同时更新告警研判、AI 对话、规则草稿、报告和 Harness 工作流；正在运行的工作流结束后即可使用新配置，无需重启 API。修改地址、模型或接口密钥后需要重新测试。Ollama 地址填写模型服务的根地址（例如 `http://192.168.24.133:11434`），若误填 `/v1` 平台会自动归一化；兼容接口模型填写兼容 Chat Completions 的根地址。AI 模型管理页面还会列出所有共用当前模型服务的 AI 业务入口。

接口密钥只在测试、应用和服务端调用时使用，页面只显示是否已配置及脱敏提示。应用后配置会写入 PostgreSQL 的活动模型服务记录，重启服务仍会沿用；没有 PostgreSQL 时仅保留在当前进程。切回同一个云端模型服务时，接口密钥输入框留空即可沿用已保存密钥。

告警详情中的“立即研判”现在会立即返回任务并显示进度条。进度包含准备上下文、调用模型和完成状态，并根据最近一次研判耗时估算剩余时间；浏览器关闭详情不会取消后台任务，重新打开同一告警会自动恢复当前任务进度或显示已保存的结果。

“智能巡检”的“立即巡检”也会立即返回后台任务，页面显示准备快照、生成 AI 建议等阶段和预计剩余时间。切换到其他菜单后，重新打开“智能巡检”会按当前租户自动恢复任务；任务完成后报告会保存到浏览器会话和服务端巡检缓存，仍可下载 PDF。

### AI 工作流 Harness

在线和本地默认获取锁定的 Harness 源码并构建侧车，需要 Git 和网络。以下参数可用于把旧配置显式切回启用状态：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\deploy-online.ps1 -IncludeHarness
```

```bash
bash ./scripts/deploy-online.sh --include-harness
```

内部 Token、侧车 URL、Ollama 地址和 MCP 回调地址由脚本准备。Harness 健康表示运行时已就绪；部署脚本还会确认本地模型存在。完全断网时，模型调用只发生在 Compose 内部网络。更多说明见 [AI 工作流](AI_PLUGIN_HARNESS.md)。

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

本地依赖端口默认只绑定 `127.0.0.1`，供本机代码和模拟设备使用；传入 `--dependency-host` 时才开放到依赖机网络。API 设备上报使用运行 Go 的主机地址。Kafka 通过独立 external listener 返回源码机可访问的地址，容器间仍使用 `redpanda:9092`。

本地 API 默认参数写在 `.env.local`；修改 API 端口时同步修改前端 `VITE_API_PROXY_TARGET`，使用 Harness 时还需同步其 MCP 回调和允许的 Origin。`--env-file` 读取字面的 `KEY=VALUE`，支持注释和单/双引号，不展开 `${变量}` 或执行 shell；已有进程环境变量优先。

依赖容器与源码分处两台机器时，在 Linux 依赖机执行 `sudo bash ./scripts/setup-local.sh --skip-code-deps --dependency-host <源码机可访问的依赖机地址> --api-host <依赖容器可访问的源码机地址>`。脚本将 Compose 端口绑定到 `0.0.0.0`，并把 Kafka 的外部公告地址、Harness 地址及回调地址写入 `.env.local`；备份服务地址固定为源码机本地 `8092`，不会在依赖机启动。把该文件复制到源码机后启动 Go API、前端和备份服务；再次显式传入 `--dependency-host 127.0.0.1` 可恢复仅本机访问。

### OrbStack 虚拟机本地调试

依赖使用 Ubuntu 虚拟机自己的 Docker Engine。Mac 安装 Go 和符合 `iot_front/package.json` 的 Node.js，使用共享的仓库目录编辑、运行和调试源码。以下命令均在 **Mac 仓库根目录**运行，`develop` 替换为 `orb list` 中的虚拟机名称：

```bash
orb list
orb -m develop sudo bash scripts/setup-local.sh --skip-code-deps --include-ai \
  --dependency-host 127.0.0.1 --api-host host.orb.internal
go mod download
(cd iot_front && npm ci)
```

Linux 初次运行会按架构安装缺失的 Docker、Compose、Buildx 和 Git；后续复用已有安装、随机凭据及数据卷。Ubuntu 首次启动可能等待系统网络就绪约两分钟；镜像、Harness 依赖和模型下载时间取决于网络。`--skip-code-deps` 使虚拟机无需安装 Go/Node；`--include-ai` 下载并配置 `qwen3:1.7b`，让 API 和 Harness 无需 DeepSeek Key 即可启动。如果使用已有 DeepSeek Key，可省略 `--include-ai` 并在配置中填写密钥；默认 DeepSeek 模式缺少 Key 时 API 会拒绝启动。OrbStack 共享目录中的 `.env.local` 可由 Mac 直接使用，无需再复制；不要同时在两台虚拟机使用同一配置文件初始化不同的依赖环境，验证用第二台机器应指定独立 `--env-file`。

Mac 使用 OrbStack 自动提供的 `localhost` 端口转发，因此上述命令不依赖虚拟机 IP 或 VPN 对内网 IP 的路由。确保 Mac 和其他虚拟机没有占用相同端口；同时测试两套依赖时先停掉其中一套，避免连接到错误的环境。`host.orb.internal` 是 OrbStack 提供的 Mac 回调地址；`host.docker.internal` 在虚拟机内安装的 Docker 中指向虚拟机，不能用于此处的 Mac API 回调。地址机制参见 [OrbStack 网络文档](https://docs.orbstack.dev/machines/network)。

需要其他源码机直接访问虚拟机时，可把 `--dependency-host` 换成 `orb -m develop hostname -I` 返回的 IPv4 或 `<机器名>.orb.local`，并确保 VPN/路由允许直连。该模式会开放依赖端口；虚拟机 IP 改变后重跑完整命令更新地址，凭据和数据保留。

随后在 Mac 的三个终端分别运行[技术详情](TECHNICAL_DETAILS.md#日常运行代码)中的 Go API、Vite、备份服务命令。若 Homebrew 安装了 `node@22`，当前终端先执行 `export PATH="$(brew --prefix node@22)/bin:$PATH"`，IDE 也选择该 Node 解释器；系统中旧的 Node 18 不能运行当前前端。

可在 Mac 验证真实依赖读写和嵌入推理：

```bash
go run scripts/tests/local-runtime-smoke.go --env-file .env.local
curl --fail http://localhost:8081/health/ready
```

实机检查会创建独立临时表、Redis 键、MinIO 桶和 Kafka 主题并清理；不要在生产环境执行。API 就绪检查还覆盖 MQTT 连接和知识库。Harness 健康与 DeepSeek API 可用性分别验证；没有配置 API Key 时不能据此宣称 AI 对话或自动研判可用。

查看、停止依赖仍在 Mac 仓库根目录执行，停止不会删除卷：

```bash
orb -m develop sudo docker compose --project-name iot-platform-local --env-file .env.local -f compose.local.yaml --profile harness ps
orb -m develop sudo docker compose --project-name iot-platform-local --env-file .env.local -f compose.local.yaml --profile harness stop
```

### ARM64 与 x86_64

Linux 目标支持 `arm64/aarch64` 与 `amd64/x86_64` 两种 64 位架构，不包含 32 位 ARM/x86。Compose 不固定 `platform`，基础镜像自动选择 Docker Engine 的原生架构，应用及 Harness 在目标架构构建。离线包仍须按目标架构分别打包，不能在两种架构间混用。修改镜像版本后，应重新检查镜像清单包含 `linux/arm64` 和 `linux/amd64`，并各自执行部署和实机检查；镜像清单与交叉编译通过不等同于目标系统部署验收。

在 ARM Mac 上通过 OrbStack 模拟 x86 Ubuntu 时，EMQX 的 Erlang JIT 默认双重内存映射可能导致 QUIC 模块报 `nif_library_not_loaded`。仅对此类模拟环境，在对应环境文件加入 `IOT_EMQX_ERL_FLAGS="+JMsingle true"` 后重跑部署命令。该参数保留 QUIC 功能，改用单一可读写执行的内存映射；原生 ARM64 和 x86_64 不需要设置，默认保持 Erlang 的内存保护行为。参数语义见 [Erlang JIT 文档](https://erlang.org/documentation/doc-14/erts-14.0/doc/html/erl.html)。

2026-09-09 的环境、实测命令及验收边界见 [Ubuntu 虚拟机本地运行验证](LOCAL_RUNTIME_VALIDATION.md)。

## VS Code 调试配置

仓库内置 `.vscode/launch.json`，配置本身不保存环境密码。使用前完成两项准备：运行一次 `setup-local` 或从依赖机复制 `.env.local` 到仓库根目录，并在 `iot_front` 安装 npm 依赖。随后在 VS Code“运行和调试”中选择 `IoT Platform (API + Web)` 并按 `F5`。核心配置如下：

```json
{
  "name": "IoT Platform API",
  "type": "go",
  "request": "launch",
  "mode": "debug",
  "program": "${workspaceFolder}/cmd/iot-platform",
  "cwd": "${workspaceFolder}",
  "args": ["--env-file", "${workspaceFolder}/.env.local"]
}
```

备份服务的源码调试配置使用同一个 `.env.local`，监听 `8092`，通过 Go 直接读取设备数据，无需本机 Docker 或数据库命令行工具：

```json
{
  "name": "Backup Service",
  "type": "go",
  "request": "launch",
  "mode": "debug",
  "program": "${workspaceFolder}/cmd/backup-service",
  "cwd": "${workspaceFolder}",
  "args": ["--env-file", "${workspaceFolder}/.env.local"]
}
```

选择 `IoT Platform (API + Web + Backup)` 可同时调试 API、前端和备份服务。备份服务与 API 共用 `.env.local`，在其中设置 `IOT_BACKUP_URL=http://127.0.0.1:8092` 和 `IOT_BACKUP_HTTP_ADDR=:8092`。Windows 前端命令为 `npm.cmd run dev`，Linux/macOS 为 `npm run dev`。CentOS 只运行依赖、Windows 运行源码时，PostgreSQL、Kafka、MQTT 等地址指向 CentOS。





升级旧本地环境：在 CentOS 更新源码后重跑原 `setup-local.sh` 命令，脚本会停止旧备份容器并保留其数据卷；将更新后的 `.env.local` 复制到 Windows，再启动上述 VS Code 组合。可以访问 `http://127.0.0.1:8092/health/ready` 检查数据库和对象存储连接。旧配置中的工具模式、Redis、Weaviate 及配置归档参数不再被备份服务使用。

在线/离线默认提供 HTTP 服务。需要公网域名与 HTTPS 时，由现有 Nginx/网关终结 TLS 并转发到 Web 端口；部署脚本不管理域名和证书。

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

本地备份服务默认由源码调试进程提供；若使用临时容器版，执行 `setup-local` 时加 `--include-backup`，或在子命令前加 `--profile backup`。启用 Harness 时，在子命令 `ps` / `logs` / `down` 前加 `--profile harness`。自定义项目名和配置路径时，上述命令也要使用相同参数。离线包的维护命令见 [离线部署说明](OFFLINE_DEPLOYMENT.md)。

`down` 保留命名数据卷；`down -v` 会删除它们。日常代码更新重跑对应部署脚本即可。备份页面调用独立 `backup-service`，默认每天上海时间 `00:05` 汇总前一天设备原始报文与解析数据，可由 `IOT_BACKUP_TIME`、`IOT_BACKUP_TIMEZONE` 调整；这是业务数据备份，不替代环境配置文件的保管。

## 排查入口

| 现象 | 先检查 |
|---|---|
| 提示找不到 Docker / Engine 不可用 | Docker 是否安装、启动并使用 Linux 容器 |
| 容器端口被占用 | 是否同时运行旧环境或另一套部署；先停止冲突实例 |
| 数据库认证失败 | 当前项目名、数据卷与配置文件是否来自同一环境 |
| 本地 Kafka 无法连接 | 是否运行 `compose.local.yaml`，API 是否读取 `.env.local` |
| API 启动后前端无法访问 | `8081` 端口、Vite 代理、旧 IDE 环境变量覆盖 |
| 知识库索引失败 | Ollama 模型下载是否完成，Weaviate 与 Ollama 日志 |
| “立即备份设备数据”返回 502 | 先看平台 API 返回的具体备份错误；源码调试时确认 `Backup Service` 已启动、`IOT_BACKUP_URL=http://127.0.0.1:8092`，并确认数据库及 MinIO 地址可达 |
| 工作流失败但 Harness 健康 | API Key、模型可达性和 MCP 回调地址 |

API `/health/live` 检查进程存活，`/health/ready` 检查已配置的存储、消息和知识库依赖。脚本和配置校验通过不等于真实设备、生产容量或目标离线环境已经验收。

维护部署脚本时，可运行 `scripts/tests/deployment-smoke.ps1 -ComposeExe <独立Compose程序路径>` 或 `bash scripts/tests/deployment-smoke.sh <独立Compose程序路径>`。它们使用真实 Compose 解析配置，模拟 Docker 和 HTTP 操作，检查一键流程与失败分支，不会启动服务。


## 设备数据备份

备份范围仅包含 PostgreSQL 的原始报文、标准解析消息，以及 ClickHouse 的原始报文和解析遥测数据。不会备份数据库结构、账号、Redis、消息队列、知识库、配置文件或整个 MinIO。设备原始报文按接收时间分日；标准消息按处理时间（旧记录回退到消息时间）分日，ClickHouse 遥测按消息时间分日。两种存储的数据分别保留来源，可能包含同一解析消息的不同表示。

- **立即备份设备数据**：导出当前保存的设备数据。
- **备份昨日数据**：按配置时区导出前一个自然日的数据。
- **每日自动备份**：默认开启，每天上海时间 00:05 执行昨日备份。服务需要持续运行；停机期间不会自动补跑历史日期。
- 每个备份包含原始数据、解析数据两个 gzip JSONL 文件及清单，保存到 MinIO 的 `iot-backups` 桶；保留下载、SHA-256 文件校验及历史记录。文件校验不等于恢复到数据库。

```dotenv
# 是否开启每日自动备份；关闭后仍可手动备份
IOT_BACKUP_ENABLED=true
# 每日执行时间，备份前一个自然日
IOT_BACKUP_TIME=00:05
# 日期与执行时间使用的时区
IOT_BACKUP_TIMEZONE=Asia/Shanghai
# 压缩文件暂存目录
IOT_BACKUP_DIR=./data/backups
```

Windows 源码调试只需 Go 环境，使用 `go run ./cmd/backup-service --env-file .env.local` 或 VS Code 的 `IoT Platform (API + Web + Backup)`；数据库与 MinIO 可继续运行在 CentOS。旧备份记录与文件不删除，旧接口类型 `RAW_LOGS` / `INCREMENTAL` 兼容映射为昨日设备数据备份。


### 设备接入对外地址与执行边界

设备向导的 `IOT_DEVICE_HTTP_PUBLIC_URL`、`IOT_DEVICE_MQTT_PUBLIC_URL` 分别配置设备可达的 HTTPS 根地址和 MQTT TLS Broker；留空时 HTTP 使用相对路径，MQTT 明确未配置，不使用容器名或固定 localhost 冒充外部地址。WebSocket 沿用 `IOT_MQTT_WEBSOCKET_PUBLIC_URL`。本地 `.env.local`、在线 `.env.online`、离线 `.env.offline` 分别设置，不能互相替代。监听端口仍需实际容器映射与网络连通。

默认采用单实例；可选独立 Gateway 与执行协调见 [接入进程](EDGE_AND_GATEWAY.md)。现场节点已移除，中心仍拒绝执行旧 edgeNodeId 非空的任务，详见 [移除说明](EDGE_REMOVAL.md)。EMQX 新增 username claim 匹配及到期断连，已有动态认证器配置需核实实际生效；不要将本地修改当成已部署。迁移、设备认证与验证命令见 [统一设备接入](UNIFIED_DEVICE_ONBOARDING.md)。

### MQTT 接收目录

平台接收 MQTT 报文后先写 `IOT_DATA_DIR/mqtt-inbox/<processRole>/`，再确认投递。该目录及其中的 `client-id` 必须随实例持久保存；多个活跃副本不可共用或复制同一份客户端身份。扩容、迁移及队列隔离处理边界见 [部件告警与 MQTT 持久接收](DEVICE_RECEIVE_RELIABILITY.md)。
