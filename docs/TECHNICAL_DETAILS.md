# 炬联 TorchLink 技术详情

[返回 README](../README.md) · [部署维护](DEPLOYMENT.md) · [离线交付](OFFLINE_DEPLOYMENT.md)

本文提供环境准备、启动和开发入口。除特别标注外，命令在仓库根目录执行。

## 环境与方案选择

| 方案 | 运行方式 | 需要准备 | 默认 Web 地址 |
| --- | --- | --- | --- |
| 本地开发 | 本机运行 Go API 和 Vue，容器运行依赖 | Go、Node.js；本机或可达的依赖机 | `http://localhost:5173` |
| 在线部署 | 目标机构建并运行容器 | Docker、网络；Linux 脚本可自动安装缺失组件 | `http://服务器IP:8080` |
| 离线部署 | 有网打包，目标机导入容器及嵌入模型；AI 调用仍需联网 | 与目标 CPU 架构匹配的完整离线包 | `http://服务器IP:8080` |

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

脚本生成 `.env.local`，设置管理员默认值并随机生成其他服务凭据，启动依赖与 Harness，准备知识库模型，执行 `go mod download` 和 `npm ci`。重复执行复用已有配置与数据，登录信息见下文。

| 需求 | PowerShell 参数 | Bash 参数 |
| --- | --- | --- |
| 自定义 DeepSeek API 模型（默认无需传入） | `-DeepSeekModel deepseek-flash` | `--deepseek-model deepseek-flash` |
| 只准备依赖，不下载源码依赖 | `-SkipCodeDeps` | `--skip-code-deps` |
| 临时运行容器版备份服务 | `-IncludeBackup` | `--include-backup` |
| 启动运维中心依赖（Prometheus、Loki、Grafana、Alertmanager、采集器） | `-IncludeOps` | `--include-ops` |

所有部署方式统一使用 DeepSeek API。启动后在“模型管理”填写 API Key、测试并应用即可；也可通过各环境文件的 `DEEPSEEK_API_KEY` 配置。未填密钥不阻止平台启动；不再下载 Qwen 对话模型，Ollama 只准备知识库嵌入模型。完整配置、升级与离线联网边界见 [AI 配置](DEPLOYMENT.md#ai-与工作流)。

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

脚本生成 `.env.online`，构建镜像，仅准备知识库嵌入模型，启动并检查服务。首次需要访问镜像、Go/npm 依赖、Harness 源码和模型源；服务器无需预装 Go 或 Node.js。更新源码后重跑同一脚本，沿用原配置、Compose 项目和数据卷。使用自定义旧环境时，先按 [配置与数据归属](DEPLOYMENT.md#配置与数据归属) 指定原参数。

## 离线部署

在有网机器使用 `scripts/package-offline.sh` / `.ps1` 打包，再把 `offline-bundles/iot-platform-offline-*` 整个目录复制到目标机，包括 `.env.offline`。后续命令在生成的离线包目录执行，打包和安装参数统一见 [离线部署](OFFLINE_DEPLOYMENT.md)。

CentOS/Linux 也可作为 openEuler 的打包机，`target-os` 指定目标部署系统。在有网且 Docker 可用的 CentOS 打包机项目根目录运行：

```bash
bash ./scripts/package-offline-linux.sh --target-os openeuler-24.03-lts-sp4
```

系统依赖在临时 openEuler 容器中准备，不安装到 CentOS 宿主机。将完整离线包复制到 openEuler 后，在包内执行 `sudo bash ./scripts/deploy-offline-linux.sh`。Windows 对应参数、代码更新和凭据沿用见 [openEuler 专用离线包](OFFLINE_DEPLOYMENT.md#openeuler-2403-lts-sp4-专用离线包)。

RPM 依赖通过包内软件源按包名安装，保留签名校验和引导包保护。已有专用包若提示 `protected packages: grub2-pc`，可在联网 CentOS/Linux 打包机运行 `scripts/repair-offline-openeuler.sh 旧包目录` 生成仅含索引、公钥和部署脚本的补丁，无需重建镜像、模型；步骤与验证边界见 [旧包修复](OFFLINE_DEPLOYMENT.md#旧包提示-protected-packages-grub2-pc)。

## 登录与专题入口

新环境管理员默认值由部署脚本生成，实际登录使用对应环境文件中的 `IOT_ADMIN_USER`、`IOT_ADMIN_PASSWORD` 和 `IOT_ADMIN_TENANTS`；常用租户为 `tenant_001`。已有配置保留原凭据，修改内置管理员配置后重启 API 生效。普通用户在「用户与权限」中创建，密码为10至72字节，归属创建者当前登录租户；不能把内置管理员的环境配置规则套用到普通用户。详见 [用户与设备权限](USER_ACCESS_CONTROL.md)。本地、在线、离线配置相互独立。

模拟发送入口见 [测试设备](UNIFIED_DEVICE_ONBOARDING.md#测试设备)。

| 任务 | 文档 |
| --- | --- |
| 端口、模型、状态、停止与备份 | [部署维护](DEPLOYMENT.md) |
| 添加设备、上报、凭据和命令 | [统一设备接入](UNIFIED_DEVICE_ONBOARDING.md) |
| Go 协议与主子设备 | [协议包](GO_PROTOCOL_PACKAGES.md) · [TCP 接入](TCP_CHILD_DEVICE_ACCESS.md) |
| 用户、角色、菜单按钮和设备范围 | [用户权限](USER_ACCESS_CONTROL.md) |
| 指标、日志、仪表盘、监控告警与通知 | [运维中心](OPS_CENTER.md) |
| 全部专题 | [文档索引](README.md) |
| 拆分 API / Gateway | [独立接入进程](EDGE_AND_GATEWAY.md) |
| 升级旧节点、拓扑和影子配置 | [旧版本迁移](DEPLOYMENT.md#旧版本迁移) |

## 首页统计

首页通过 `GET /api/v1/dashboard?days=7&offset=480` 读取聚合数据。内置管理员查看当前租户；普通用户需要运行总览菜单，统计仅包含其有权访问的设备及告警，没有设备管理菜单或设备范围时返回零值。`days` 支持 7、30，`offset` 为相对 UTC 的分钟偏移（默认 480；页面使用浏览器当前偏移）。日期范围包含今天，按固定时区的自然日划分。

- 设备总数、状态与产品分布只统计已登记设备；`ONLINE`、`ALARM` 归为在线，未产生运行状态的设备归为待连接。
- 活动告警及其等级只统计 `ACTIVE`，高等级为 `HIGH` 和 `CRITICAL`，不受告警列表分页影响。
- 趋势按 `firstTriggeredAt` 统计每日新增告警记录，包含已确认、恢复或关闭的记录；重复触发次数不作为新增条数，缺失日期补零。
- `alarmStatuses`、`alarmTypes` 按所选时段内首次发生的告警统计，处置分布使用这些记录的当前状态，类型排行展示前五类并合并其余类型；两者总量与趋势一致，不受列表分页影响。
- `connections`、`dataStatuses` 展示已登记设备的当前连接与数据活跃状态，与告警时段无关；空状态归为 `UNKNOWN`，不把连接状态等同于业务在线状态。
- 产品图展示数量最多的五项，其余合并为“其他产品”。实时通知合并刷新，失败保留上次成功数据。

全部设备范围使用仓储聚合查询；指定设备范围经请求级设备仓储过滤后统计，不把全租户计数返回给受限用户。内存实现保持相同口径。入口为 `internal/httpapi/dashboard.go`，回归测试 `TestDashboard` 同时支持内存与 `IOT_TEST_POSTGRES_DSN` 指定的独立临时数据库 schema。

## 列表与分页

采用 `internal/httpapi/pagination.go` 的接口支持 `page/pageSize`，兼容 `limit/offset`，每页默认 20 条、上限 100 条，返回 `items`、`total`、`page`、`pageSize`。正数 `page` 优先于 `offset`；超大参数收敛到整数安全上界，越界页返回空列表并保留实际总数，非数字沿用默认行为。

设备、产品、规则、摄像头的关联选项通过 `apiAll` 逐页加载，与表格当前页分开保存；任一页失败则整体失败，不显示不完整目录。列表仅允许最新请求写入数据、总数和加载状态，旧请求不覆盖当前结果。逐页请求不保证数据库快照一致性。

设备管理先读取授权设备目录，再按独立设备、主设备、子设备及关键词、类型筛选并在页面分页，切换条件重置页码。协议目录按协议条目前端分页，版本在条目内展示。两者与后端通用分页的实现不同。服务端设备、状态、告警、原始报文和总览均先按用户范围过滤再计数；具体权限见 [用户权限](USER_ACCESS_CONTROL.md)。

对应回归入口为 `go test ./internal/httpapi -run 'Test(OversizedPagination|PaginationArithmetic|PageItems|ParseListPagination)'` 和在 `iot_front` 中执行 `node --test tests/list-behavior.test.mjs`。

### 原始报文筛选

原始报文页支持设备标识、报文标识、解析状态、接收时间，以及“更多筛选”中的产品标识、协议、报文格式、消息类型和解析器；可组合查询，并提供最近 1 小时、24 小时、7 天快捷范围。标识和解析器使用完整值精确匹配，协议及格式忽略大小写。点击“查询”应用条件，翻页与刷新保留已应用条件，“重置筛选”清空全部条件并返回第一页。

`GET /api/v1/raw-messages` 对应参数为 `deviceId`、`messageId`、`productId`、`protocol`、`payloadFormat`、`parseStatus`、`messageType`、`parser`、`start`、`end`；时间为包含端点的接收时间毫秒值。解析状态为 `PARSED`（已存在标准消息）、`FAILED`（无标准消息且记录解析错误）、`UNPARSED`（无标准消息且无解析错误）。消息类型及解析器依据该原文最新的标准消息。筛选在存储查询阶段、分页之前执行，列表总数使用相同条件，保留租户与用户设备范围限制。普通用户不因筛选获得额外设备访问权限。

## 容量边界

容量取决于目标硬件、报文频率、协议、规则、留存时间与管理查询负载。以下是源码限额，不是持续吞吐或生产容量承诺：

| 链路 | 限额与实现 |
| --- | --- |
| 标准 HTTP / MQTT 上报 | `internal/onboarding/service.go`：每设备每秒 20 条；进程限流表最多 100,000 个键，表满时清理一秒窗口已结束的键（每秒最多清理一次），即每进程每秒最多限流 10 万个不同设备 |
| 自定义 TCP 监听 | `internal/protocolruntime/listeners.go`：每个 Profile 最多 128 个会话，每进程 32 个 Worker 操作槽 |
| MQTT 持久接收 | 每实例 50,000 条 / 1 GiB，均分 32 个分片，各分片并行落盘与处理；热点可先填满。平台会话在 EMQX 的队列上限 100,000 条，满则丢弃，保障见 [接收可靠性](DEVICE_RECEIVE_RELIABILITY.md) |

标准消息处理按租户缓存规则 2 秒，本进程修改规则后立即失效，其他副本最多延迟 2 秒生效。产品的协议绑定与协议版本同样缓存 2 秒（原文入口和解析共用，未绑定也缓存），本进程切换、发布或删除版本后立即失效；其余逐条操作（幂等检查、原文预留与归档、索引、发布标记）属于接收可靠性链路，仍逐条执行。Worker 制品校验结果按路径、大小和修改时间缓存，最长 5 分钟重新计算 SHA-256。Go 协议的 ingress、decode、encode 对发布校验记录为 `workerMode: "serve"` 的制品复用常驻 Worker（每个制品最多 4 个进程，空闲 2 分钟回收），其他制品仍逐次启动，见 [Go 协议包](GO_PROTOCOL_PACKAGES.md)；Kafka 每个订阅在进程内按消息键（原始与标准消息为设备 ID，告警主题为告警 ID）分到 `IOT_KAFKA_CONSUMER_CONCURRENCY`（默认 64）条并行通道，同一设备的消息仍按顺序处理；各进程 PostgreSQL 连接池为 `IOT_POSTGRES_MAX_CONNS`（默认 64，DSN 中的 `pool_max_conns` 优先），所有进程之和须小于服务端 `max_connections`（Compose 默认 300）；规则只对所属产品的消息求值，非持续时间规则不再逐条清理待定记录；偏移只提交到每个分区连续处理完成的位置，进程中断会重复投递、不会跳过。告警自动研判只在新建告警时触发；排队期间已恢复或关闭的告警、已有成功结果的重复投递会跳过（计入 `ai_analysis_skipped_total`）。该订阅使用 `IOT_AI_ANALYSIS_CONCURRENCY`（默认 1，Harness 部署默认并发为 2，为交互使用留出空位）。未配置 Kafka 时（仅开发模式允许）内存总线同步处理报文链路，但告警自动研判放到后台队列，按 `IOT_AI_ANALYSIS_CONCURRENCY` 并发执行；队列超过 1,000 条时丢弃新的自动研判并可手动重跑，进程退出前处理完已排队的研判。启用 ClickHouse 时，原始报文与遥测行按表合批写入：最多等待 20 毫秒，或凑满 1,000 行 / 4 MiB 立即写入；调用方等到本批 INSERT 成功才返回，失败时同批调用方都收到错误并按原路径重试。只有重复投递的标准消息才先查询 ClickHouse 是否已有该遥测行。管理端每 3 秒请求一次 `/api/v1/events`；同一租户的活动告警与设备状态每 2 秒最多从仓储读取一次，由该租户所有在线用户共享，每个请求再按自身权限和设备范围过滤；视图未变化时按 `ETag` 返回 304 且不带正文。因此在线用户数不再线性放大数据库查询，但设备数仍决定每次读取和过滤的数据量，提醒最多再延迟 2 秒。入口 HTTP 202、MQTT PUBACK 或就绪检查成功均不能证明解析、告警已完成。

验收应在隔离环境使用实际设备凭据与代表性报文，分别测连接、持续上报、突发及断流恢复，同时记录 Raw 落盘、Kafka 积压、解析和告警完成延迟、MQTT 队列、数据库与管理请求 P95/P99。持续增长的积压表示该负载不可持续。`cmd/loadgen` 默认参数只是生成器输入，其管理员上报链路不能代替标准设备认证及完整端到端压测。具体场景、观测项和记录模板见 [容量压测方案](CAPACITY_TEST_PLAN.md)；2026-09-26 的单机实测结果、瓶颈和集群推算见 [容量压测报告](CAPACITY_TEST_REPORT.md)，默认配置端到端可持续约 45 条/秒。

## 源码与开发检查

| 入口 | 职责 |
| --- | --- |
| `cmd/iot-platform/`、`internal/platformapp/` | API 启动和依赖装配 |
| `internal/httpapi/`、`internal/core/`、`internal/adapters/` | 接口、业务、外部存储与服务 |
| `internal/protocolbuild/`、`internal/protocolruntime/`、`internal/protocolworker/` | 协议编译、连接运行时和 Worker |
| `internal/opscenter/`、`internal/adapters/observability/` | [运维中心](OPS_CENTER.md) 业务与 Prometheus / Loki / Grafana / Alertmanager 适配 |
| `iot_front/` | [Vue 管理端](../iot_front/README.md)；[列表与分页](#列表与分页) |
| `protocol-packages/gb26875-dahua/` | 可独立维护的协议 module |
| `scripts/`、`deploy/`、`compose*.yaml` | 准备、部署与打包配置 |

按改动范围执行，不必为文档变更运行全套服务：

| 执行目录 | 命令 | 验证范围 |
| --- | --- | --- |
| 仓库根目录 | `go test ./cmd/... ./internal/...` | 正式后端源码，避免把本地生成目录纳入测试 |
| `protocol-packages/gb26875-dahua` | `go test ./...` | 独立协议及模拟器 |
| `dev/` 下各协议 module 目录 | `go test ./...` | 对应厂商协议；与根 module 分开执行 |
| `iot_front` | `npm test`、`npm run build` | 前端测试及构建；没有独立 lint/typecheck 脚本 |
| 仓库根目录 | `git diff --check` | 空白错误 |

本地集成入口：`go run scripts/tests/local-runtime-smoke.go --env-file .env.local` 检查依赖读写；前端、API 和备份启动后，`node scripts/tests/local-business-smoke.mjs` 检查登录、接入、归档、规则及回放。业务冒烟会创建唯一测试数据，结束停用本次规则与凭据并保留记录；仅在测试环境运行。

Kafka 消费失败三次后写入 `iot.dlq.<消费组>`，写入成功并提交原消息位点后才继续消费。死信中的合法 JSON 报文保持 `payload` 原结构；非 JSON 或二进制报文放在 `payload` 的 Base64 字符串中，并带 `payloadEncoding: "base64"`，可还原原始字节。

真实 PostgreSQL、MQTT 用例及浏览器检查说明见 [接入验证](UNIFIED_DEVICE_ONBOARDING.md#验证入口)。部署脚本检查见 [部署排查入口](DEPLOYMENT.md#排查入口)。未配置而跳过的用例不算联调通过，历史测试记录可从 Git 历史追溯。
