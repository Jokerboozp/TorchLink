# 部署配置与维护

首次运行命令见 [技术详情](TECHNICAL_DETAILS.md)，项目功能见 [README](../README.md)。本文用于选择可选组件、修改配置和维护已有环境。

## 配置与数据归属

| 方案 | 配置文件 | Compose 文件 | 项目名 / 数据卷前缀 |
|---|---|---|---|
| 本地运行 | `.env.local` | `compose.local.yaml` | `iot-platform-local` |
| 在线部署 | `.env.online` | `compose.yaml` | `iot-platform-online` |
| 离线部署 | 离线包内 `.env.offline` | `compose.yaml` + `compose.offline.yaml` | `iot-platform` |

脚本显式选择配置和 Compose 文件，在线/离线部署不会自动加载用于旧版本地调试的 `compose.override.yaml`。

首次执行设置平台管理员为 `admin` / `admin123`，其他服务凭据随机生成，并在每个配置项前写入中文说明；重复执行保留业务凭据并补齐说明；AI 配置会按下文统一迁移为 DeepSeek。不要重新生成配置文件来“重置”已有数据库。配置文件和离线包包含凭据，不应提交或公开分享。

**已有部署沿用原项目和凭据。** 新默认项目名会创建一套新数据卷，不会自动迁移旧数据。例如原服务用项目 `iot-platform`、配置 `.env`，在线更新应执行：

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\deploy-online.ps1 -EnvFile .env -ProjectName iot-platform
```

```bash
bash ./scripts/deploy-online.sh --env-file .env --project-name iot-platform
```

已有自定义 Compose 覆盖文件、外部数据卷或外部数据库时，先核对原部署参数；上述命令只使用 `compose.yaml`。

## AI 与工作流

本地、在线和离线包统一使用 **DeepSeek API**，默认模型为 `deepseek-flash`，不再下载、启动或归档 Qwen 对话模型。模型标识以 [DeepSeek 官方接口文档](https://api-docs.deepseek.com/zh-cn/) 为依据（2026-09-26 核对），可在模型管理中调整。Harness 为必装组件，承担告警研判、巡检、报告、协议助手、规则草稿和对话。

### 首次配置

最简单的方式是先完成部署，登录“模型管理”，保持预填的 DeepSeek 地址和模型，填写自己的 API Key，点击“测试配置”后“应用配置”。未配置密钥时平台仍可启动和接收设备数据，AI 状态显示待配置，AI 请求返回明确错误；不会自动下载本地模型或伪造分析结果。

也可在对应环境文件中填写：

```dotenv
IOT_AI_PROVIDER=deepseek
IOT_AI_BASE_URL=https://api.deepseek.com
IOT_AI_MODEL=deepseek-flash
IOT_AI_API_KEY=
DEEPSEEK_API_KEY=填写自己的密钥
DEEPSEEK_BASE_URL=https://api.deepseek.com
IOT_AI_HARNESS_ENABLED=true
IOT_AI_HARNESS_PROVIDER=deepseek-official
IOT_AI_HARNESS_MODEL=deepseek-flash
```

本地读取 `.env.local`，在线读取 `.env.online`，离线包读取 `.env.offline`。修改环境配置后，下次运行对应部署脚本并重启源码 API / 重建应用容器使其生效；不要只重启旧二进制。日常在模型管理中应用配置会同步 Provider 和 Harness，无需重启 API。已有数据库活动配置优先于环境文件；若已有 DeepSeek 配置，请在页面更新密钥。API 升级时在部署选择 DeepSeek 的前提下，不再恢复旧的 Ollama/Qwen 活动选择，待填写密钥并应用后保存新的活动配置。其他手工配置的外部模型保留，由管理员在模型管理中切换。

部署脚本会把旧环境中的本地对话模型设置迁移为 DeepSeek，保留 `DEEPSEEK_API_KEY`；只在旧提供方明确为 DeepSeek 时兼容迁移 `IOT_AI_API_KEY`，避免把其他服务密钥发送到 DeepSeek。`--include-ai` / `-IncludeAi`、`--include-deepseek` / `-IncludeDeepSeek` 为兼容参数，不再切换到本地推理；旧 `--ollama-model` / `-OllamaModel` 参数会明确报错。脚本不会删除已下载的旧模型或正在使用的数据卷。

### 知识库嵌入与联网边界

Ollama **仅用于**知识库的 `nomic-embed-text` 嵌入；Weaviate 保存向量与文档。嵌入模型与 DeepSeek 对话 API 作用不同，不能直接把向量接口指向 Chat Completions。离线包只归档该模型的 manifest 及引用的 blobs，历史 Qwen 缓存不会混入包中；不包含 Ollama 身份密钥。

“离线部署”表示安装依赖、镜像和嵌入模型可以离线完成。使用 AI 时，API（配置测试）和 Harness（工作流）仍须通过 HTTPS 访问 `api.deepseek.com:443`，账户需有可用额度；完全隔离网络中 AI 不可用。设备接入、规则、报文和已准备好的知识库不依赖 DeepSeek。离线包体积的实际减少量取决于旧模型与镜像缓存，本次没有重新打包测量。

### 模型管理与工作流服务

模型管理默认预填 DeepSeek；仍保留显式接入外部 Ollama / 兼容接口的能力，不随部署分发它们的对话权重。输入服务根地址与模型，密钥单独填写；地址、模型或密钥改变后重新测试。Ollama 使用原生根地址，兼容接口须支持 Chat Completions；不能把 Markdown 链接、带账号密码、查询或片段的 URL 当作根地址。旧 `IOT_AI_PROVIDER_TEST_ALLOWED_ORIGINS` 不再读取。

PostgreSQL 保存活动配置；内存模式只在当前进程生效。页面不返回明文密钥，同一服务留空可复用已存密钥。Provider 和 Harness 配置作用于整个部署，应只向可信管理员授予模型配置权限。密钥在服务端持久化，数据库备份也属于敏感材料。

本地和在线脚本获取锁定的上游源码并构建 Harness，需要 Git 和网络；离线包携带已构建镜像。`--include-harness` / `-IncludeHarness` 仅为兼容参数，Harness 始终启动；脚本拒绝关闭它。API / combined 角色必须配置 `IOT_AI_HARNESS_URL`，gateway 角色除外。Harness 健康只证明进程就绪，不证明 API Key、模型调用或 MCP 回调成功。实现和模型配置详情见 [AI 工作流](AI_PLUGIN_HARNESS.md) 与 [侧车开发](../deploy/deepseek-harness/README.md)。

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
| Prometheus / Grafana | `19090` / `13000`（`--include-ops`） | Prometheus `9090` 仅宿主机（`PROMETHEUS_BIND_ADDRESS` 可改）/ Grafana `3000`（`GRAFANA_PORT`） |
| Loki / Alertmanager | `13100` / `19093`（`--include-ops`） | 仅容器网络 |

本地依赖端口默认只绑定 `127.0.0.1`，供本机代码和模拟设备使用；传入 `--dependency-host` 时才开放到依赖机网络。API 设备上报使用运行 Go 的主机地址。Kafka 通过独立 external listener 返回源码机可访问的地址，容器间仍使用 `redpanda:9092`。

本地 API 默认参数写在 `.env.local`；修改 API 端口时同步修改前端 `VITE_API_PROXY_TARGET`，使用 Harness 时还需同步其 MCP 回调和允许的 Origin。`--env-file` 读取字面的 `KEY=VALUE`，支持注释和单/双引号，不展开 `${变量}` 或执行 shell；已有进程环境变量优先。

依赖容器与源码分处两台机器时，在 Linux 依赖机执行 `sudo bash ./scripts/setup-local.sh --skip-code-deps --dependency-host <源码机可访问的依赖机地址> --api-host <依赖容器可访问的源码机地址>`。脚本将 Compose 端口绑定到 `0.0.0.0`，并把 Kafka 的外部公告地址、Harness 地址及回调地址写入 `.env.local`；备份服务地址固定为源码机本地 `8092`，不会在依赖机启动。把该文件复制到源码机后启动 Go API、前端和备份服务；再次显式传入 `--dependency-host 127.0.0.1` 可恢复仅本机访问。

### OrbStack 虚拟机本地调试

依赖使用 Ubuntu 虚拟机自己的 Docker Engine。Mac 安装 Go 和符合 `iot_front/package.json` 的 Node.js，使用共享的仓库目录编辑、运行和调试源码。以下命令均在 **Mac 仓库根目录**运行，`develop` 替换为 `orb list` 中的虚拟机名称：

```bash
orb list
orb -m develop sudo bash scripts/setup-local.sh --skip-code-deps --include-ops \
  --dependency-host 127.0.0.1 --api-host host.orb.internal
go mod download
(cd iot_front && npm ci)
```

`--skip-code-deps` 使虚拟机无需安装 Go/Node，`--include-ops` 启动运维中心依赖；AI 通过 DeepSeek API 配置。Mac 可直接使用共享目录中的 `.env.local`；第二套依赖环境应指定独立 `--env-file`，并避免同时占用相同转发端口。

Mac 使用 OrbStack 自动提供的 `localhost` 端口转发，因此上述命令不依赖虚拟机 IP 或 VPN 对内网 IP 的路由。确保 Mac 和其他虚拟机没有占用相同端口；同时测试两套依赖时先停掉其中一套，避免连接到错误的环境。`host.orb.internal` 是 OrbStack 提供的 Mac 回调地址；`host.docker.internal` 在虚拟机内安装的 Docker 中指向虚拟机，不能用于此处的 Mac API 回调。地址机制参见 [OrbStack 网络文档](https://docs.orbstack.dev/machines/network)。

需要其他源码机直接访问虚拟机时，可把 `--dependency-host` 换成 `orb -m develop hostname -I` 返回的 IPv4 或 `<机器名>.orb.local`，并确保 VPN/路由允许直连。该模式会开放依赖端口；虚拟机 IP 改变后重跑完整命令更新地址，凭据和数据保留。

随后在 Mac 启动 API、Vite 和备份服务，命令及依赖检查入口见 [技术详情](TECHNICAL_DETAILS.md#日常运行代码)。IDE 与终端均须选择符合 `iot_front/package.json` 的 Node.js。

查看、停止依赖仍在 Mac 仓库根目录执行，停止不会删除卷：

```bash
orb -m develop sudo docker compose --project-name iot-platform-local --env-file .env.local -f compose.local.yaml ps
orb -m develop sudo docker compose --project-name iot-platform-local --env-file .env.local -f compose.local.yaml stop
```

### ARM64 与 x86_64

Linux 目标支持 `arm64/aarch64` 与 `amd64/x86_64` 两种 64 位架构，不包含 32 位 ARM/x86。Compose 不固定 `platform`，基础镜像自动选择 Docker Engine 的原生架构，应用及 Harness 在目标架构构建。离线包仍须按目标架构分别打包，不能在两种架构间混用。修改镜像版本后，应重新检查镜像清单包含 `linux/arm64` 和 `linux/amd64`，并各自执行部署和实机检查；镜像清单与交叉编译通过不等同于目标系统部署验收。

在 ARM Mac 上通过 OrbStack 模拟 x86 Ubuntu 时，EMQX 的 Erlang JIT 默认双重内存映射可能导致 QUIC 模块报 `nif_library_not_loaded`。仅对此类模拟环境，在对应环境文件加入 `IOT_EMQX_ERL_FLAGS="+JMsingle true"` 后重跑部署命令。该参数保留 QUIC 功能，改用单一可读写执行的内存映射；原生 ARM64 和 x86_64 不需要设置，默认保持 Erlang 的内存保护行为。参数语义见 [Erlang JIT 文档](https://erlang.org/documentation/doc-14/erts-14.0/doc/html/erl.html)。

在线/离线默认提供 HTTP 服务。公网 HTTPS 由现有反向代理终结 TLS 并转发到 Web 端口，脚本不管理域名或证书。

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

本地备份服务默认由源码调试进程提供；若使用临时容器版，执行 `setup-local` 时加 `--include-backup`，或在子命令前加 `--profile backup`。启用运维中心依赖时加 `--profile ops`。自定义项目名和配置路径时，上述命令也要使用相同参数。离线包的维护命令见 [离线部署说明](OFFLINE_DEPLOYMENT.md)。

`down` 保留命名数据卷，`down -v` 会删除它们。日常代码更新重跑对应部署脚本；备份范围与调度见 [设备数据备份](#设备数据备份)。

## 高可用边界

默认 Compose（本地、在线、离线）是**单节点**配置：PostgreSQL、ClickHouse、Redis、Redpanda、EMQX、MinIO、Weaviate、Ollama、Harness 与 API 各运行一个实例，Redpanda 主题创建为 `--replicas 1`。它可以承担单机生产，但不具备高可用：

- 容器自动重启只在进程退出后拉起同一实例，不能在宿主机、磁盘或数据卷故障时切换。
- MQTT 持久队列（`IOT_DATA_DIR/mqtt-inbox/`）保证已确认报文在本机磁盘上重启后可继续处理，不复制到其他节点。
- 备份用于事后恢复数据，恢复需要停机与人工操作，不是故障切换；备份存在不等于已验证可恢复。
- 拆分 `api` / `gateway` 与多副本 API 只分担接入和查询，前提是数据库、消息与对象存储本身可用。
- 运维中心依赖（`--profile ops` 的 Prometheus、Loki、Grafana、Alertmanager）同样各一个实例；它们停止时接入与告警链路不受影响，但期间的监控数据、日志与告警通知会缺失。

需要高可用时，至少要为 Redpanda（三节点，主题 `--replicas 3`）、PostgreSQL（主备复制与自动切换）、ClickHouse（副本）、EMQX（集群）、MinIO（分布式或外部对象存储）和多副本 API / Harness（前置负载均衡）分别设计，并在目标环境演练节点故障与切换；这些不在默认 Compose 的范围内，也未经本仓库验证。

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
| 普通用户登录后没有设备或告警 | 当前租户、设备管理菜单、设备访问范围及告警菜单是否均已分配；历史用户默认无设备 |
| 编辑账户后旧登录返回401 | 修改用户、停用或重置密码会撤销旧会话，需要重新登录 |
| 工作流失败但 Harness 健康 | API Key、模型可达性和 MCP 回调地址 |
| 运维中心显示“未配置”或规则、通知只能查看 | API 环境中的 `IOT_OPS_*` 地址与受管文件路径；其他账号还需所在租户列入 `IOT_OPS_TENANTS`，见 [运维中心](OPS_CENTER.md#配置) |
| 保存规则提示“未确认加载，已恢复” | Prometheus 是否带 `auto-reload-config`、Loki ruler 轮询间隔，以及 API 与组件是否挂载同一规则目录 |

API `/health/live` 检查进程存活，`/health/ready` 检查已配置的存储、消息和知识库依赖。脚本和配置校验通过不等于真实设备、生产容量或目标离线环境已经验收。

维护部署脚本时，可运行 `scripts/tests/deployment-smoke.ps1 -ComposeExe <独立Compose程序路径>` 或 `bash scripts/tests/deployment-smoke.sh <独立Compose程序路径>`。它们使用真实 Compose 解析配置，模拟 Docker 和 HTTP 操作，检查一键流程与失败分支，不会启动服务。

## 旧版本迁移

旧现场 Agent、节点登记、现场任务与程序分发入口已移除，设备继续通过中心 MQTT / HTTP、Modbus、Go TCP / UDP、独立 Access Gateway 及主子设备关系接入。

- `DeviceAccessProfile.edgeNodeId` 非空的旧配置不会在中心执行，也不能保存为有效实例。先确认设备网络和产品协议，再到「设备模板 → 接入点」重新配置；旧串口任务不能仅清空节点标识后运行。外部已部署 Agent 需由部署者停用。
- 新库不创建 `edge_node`、`edge_read_job`、`edge_program`、`device_shadow`、`device_shadow_change` 或 `device_twin_topology`；启动迁移保留旧表与历史数据，当前 API 不再管理它们。`gatewayId` 表示业务主设备，主子设备状态和权限分别维护。
- 启动迁移把设备标签中的 `connector`、`connectorProfileId`、`childAddress`、`childType`、`onboardingRequestHash` 移为设备字段，为空的 `deviceRole` 按 `gatewayId` 与模板分类补为 `CHILD` / `GATEWAY` / `DIRECT`，并为引用已发布协议版本但缺少绑定的模板补建绑定。迁移语句可重复执行，不删除设备。
- 升级沿用原环境文件、Compose 项目名、数据卷、协议制品及 MQTT 接收目录；不可变协议版本不被新源码覆盖。TCP / UDP、Modbus 与子设备不使用历史内部凭据通过 HTTP / MQTT 认证。

无需清空数据库完成迁移。设备数据导出不包含完整环境备份，数据库、配置和密钥需分别保管；普通用户权限按下一节处理。

## 用户权限升级

升级时前端与 API/Gateway 使用同一版源码，沿用原 PostgreSQL 数据。启动迁移自动创建 `platform_access`；账户、角色、密码哈希和设备范围均保存在该表，不在浏览器持久保存密码。

1. 使用内置管理员登录原租户，核对「用户与权限」中显示的所属租户。
2. 为角色配置功能及设备范围，用户选择“继承角色”；已有用户保留单独范围，需明确切换才继承。历史未配置范围的账户默认「无设备」，不要批量自动提升为全部设备。主子设备分别授权。
3. 普通用户从 `/api/v1/events` 每3秒获取授权范围内的状态及活动告警；不再签发浏览器 MQTT 或压测令牌。升级前旧 MQTT 令牌最长15分钟有效，切换时应撤销旧普通用户的 Broker 会话和重连资格，或等旧令牌全部过期后再开放使用；不要误断开设备或管理员连接。
4. 使用两个不同设备范围的用户核对列表、总数、总览、详情和提醒，再恢复日常使用。验证入口见 [用户权限](USER_ACCESS_CONTROL.md)。

`platform_access` 不在下面的设备数据导出范围内，需随完整数据库备份保管。备份中心、巡检等全租户功能仅向具备全部设备范围及相应菜单/按钮权限的用户开放。

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

## 设备接入对外地址与执行边界

设备向导的 `IOT_DEVICE_HTTP_PUBLIC_URL`、`IOT_DEVICE_MQTT_PUBLIC_URL` 分别配置设备可达的 HTTPS 根地址和 MQTT TLS Broker；留空时 HTTP 使用相对路径，MQTT 明确未配置，不使用容器名或固定 localhost 冒充外部地址。WebSocket 沿用 `IOT_MQTT_WEBSOCKET_PUBLIC_URL`。本地 `.env.local`、在线 `.env.online`、离线 `.env.offline` 分别设置，不能互相替代。监听端口仍需实际容器映射与网络连通。

默认采用单实例；可选独立 Gateway 与执行协调见 [接入进程](EDGE_AND_GATEWAY.md)。旧现场配置处理见 [旧版本迁移](#旧版本迁移)。EMQX 要求 username claim 匹配及到期断连，已有动态认证器配置需核实实际生效。设备认证与验证命令见 [统一设备接入](UNIFIED_DEVICE_ONBOARDING.md)。

## MQTT 接收目录

平台接收 MQTT 报文后先写 `IOT_DATA_DIR/mqtt-inbox/<processRole>/`，再确认投递。该目录及其中的 `client-id` 必须随实例持久保存；多个活跃副本不可共用或复制同一份客户端身份。扩容、迁移及队列隔离处理边界见 [部件告警与 MQTT 持久接收](DEVICE_RECEIVE_RELIABILITY.md)。
