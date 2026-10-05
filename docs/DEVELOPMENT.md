# 开发与测试

环境准备、源码启动、IDE 调试统一见 [部署与本地调试](DEPLOYMENT.md#本地运行)。本文维护开发约定、测试入口和演示工具；命令默认在仓库根目录执行。

## 源码与开发检查

| 入口 | 职责 |
| --- | --- |
| `cmd/iot-platform/`、`internal/platformapp/` | API 启动和依赖装配 |
| `internal/httpapi/`、`internal/core/`、`internal/adapters/` | 接口、业务、外部存储与服务 |
| `internal/onboarding/`、`internal/httpapi/template_preparation.go`、`internal/httpapi/onboarding_tasks.go` | 模板准备与验收、持久草稿、批量任务及完整配置切换 |
| `internal/firesafety/`、`internal/httpapi/fire_safety.go` | [消防管理](FIRE_SAFETY.md)：租户业务状态、排班、巡检整改、出勤与 API |
| `internal/protocolbuild/`、`internal/protocolruntime/`、`internal/protocolworker/` | 协议编译、连接运行时和 Worker |
| `internal/opscenter/`、`internal/adapters/observability/` | [运维中心](PLATFORM.md#运维中心) 业务与 Prometheus / Loki / Grafana / Alertmanager 适配 |
| `internal/capacity/`、`cmd/capacity-test/` | 容量计划、Controller / Agent、采集核对、报告与控制服务 |
| `internal/clusterplan/`、`cmd/cluster-*` | 集群清单、配置渲染、初始化与 SSH 准备 |
| `internal/video/`、`internal/backup/` | 摄像头直播/GB28181 与设备数据备份/恢复验证 |
| `iot_front/` | [Vue 管理端](#管理端开发)；[列表与分页](#列表与分页) |
| `protocol-packages/gb26875-dahua/` | 可独立维护的协议 module |
| `scripts/`、`deploy/`、`compose*.yaml` | 准备、部署与打包配置 |

按改动范围执行，不必为文档变更运行全套服务：

| 执行目录 | 命令 | 验证范围 |
| --- | --- | --- |
| 仓库根目录 | `go test ./cmd/... ./internal/... ./deploy/toolaccounts` | 正式后端与工具账号初始化，避免把本地生成目录纳入测试 |
| 仓库根目录 | `golangci-lint run ./cmd/... ./internal/... ./deploy/toolaccounts` | 静态检查，规则见 `.golangci.yml`；需 golangci-lint v2 且以 Go 1.26 构建 |
| `protocol-packages/gb26875-dahua` | `go test ./...` | 独立协议及模拟器 |
| `dev/` 下各协议 module 目录 | `go test ./...` | 对应厂商协议；与根 module 分开执行 |
| `iot_front` | `npm run lint`、`npm run format:check`、`npm test`、`npm run build` | ESLint、Prettier 格式、前端测试及构建；`npm run format` 自动格式化 |
| 仓库根目录 | `git diff --check` | 空白错误 |

本地集成入口：`go run scripts/tests/local-runtime-smoke.go --env-file .env.local` 检查依赖读写；前端、API 和备份启动后，`node scripts/tests/local-business-smoke.mjs` 检查登录、接入、归档、规则及回放。业务冒烟会创建唯一测试数据，结束停用本次规则与凭据并保留记录；仅在测试环境运行。

Kafka 消费失败三次后写入 `iot.dlq.<消费组>`，写入成功并提交原消息位点后才继续消费。读取出错的订阅按指数退避（最长 30 秒）自动重建；重建期间或在途消息 2 分钟无进展时，`/health/ready` 报告对应消费组未就绪。死信中的合法 JSON 报文保持 `payload` 原结构；非 JSON 或二进制报文放在 `payload` 的 Base64 字符串中，并带 `payloadEncoding: "base64"`，可还原原始字节。

真实依赖与浏览器检查按各测试的 `IOT_TEST_*` 环境变量启用；接入链路见 [接入验证](INTEGRATION.md#验证入口)，知识索引和备份见 [AI 与知识库回归](#ai-与知识库回归)。未配置而跳过的用例不算联调通过。

部署脚本修改使用独立 Compose 可执行文件（不能传 `docker compose` 子命令）：Bash 运行 `bash scripts/tests/deployment-smoke.sh /path/to/docker-compose`，PowerShell 运行 `pwsh -File scripts/tests/deployment-smoke.ps1 -ComposeExe /path/to/docker-compose`。它们使用真实 Compose 解析，模拟 Docker/HTTP 操作，不部署服务。安装器用例集中于 `scripts/tests/docker-bootstrap-smoke.sh`，openEuler 打包用例集中于 `scripts/tests/openeuler-smoke.sh`。

未设置 `IOT_KAFKA_BROKERS` 时平台使用进程内事件总线（`internal/adapters/local/bus.go`）：发布时按订阅顺序同步调用全部订阅者，第一个订阅者出错即中止本次发布并把错误返回给发布方，不重试、不进入死信，消费组参数被忽略。单元测试和不带 Kafka 的精简运行受此影响；`scripts/setup-local.sh` 生成的 `.env.local` 已配置 Kafka。在精简运行中复现的“消费失败”“重复处理”等现象不能代表线上 Kafka 的重试、死信和分区顺序行为。

### 持续集成

`.github/workflows/ci.yml` 在推送到 main 与 Pull Request 时运行：`gofmt`、`go vet`、`go mod tidy` 无差异、golangci-lint，以真实 PostgreSQL（pgvector 0.8.1 / PG17）、ClickHouse 25.7、Redis 7.4 服务容器执行 `go test ./cmd/... ./internal/... ./deploy/toolaccounts`（依赖 `IOT_TEST_POSTGRES_DSN` 等的仓储与迁移测试不再跳过），对核心、协议运行时、持久队列、MQTT / Kafka 与外部数据包加 `-race`，`protocol-packages/` 与 `dev/` 下各独立协议 module 的 vet 与测试，根 module 及各协议 module 的 `govulncheck`；前端 ESLint、Prettier 格式检查、`npm test`、`npm run build` 与提示性 `npm audit`；Bash 与 PowerShell 部署脚本冒烟与 Prometheus 规则 `promtool` 校验。本地可用相同变量指向一次性数据库复现。

### 脚本入口

同一操作使用 `.sh`（Linux / macOS）与 `.ps1`（Windows PowerShell），公共逻辑放在 `scripts/lib/`。Harness 始终准备，云端模型密钥由用户配置；本地临时备份容器使用 `--include-backup` / `-IncludeBackup`。参数以脚本帮助为准，部署步骤见 [部署指南](DEPLOYMENT.md)。

| 入口 | 用途 |
| --- | --- |
| `scripts/setup-local.*` | 准备本地开发依赖，支持 VM 依赖、直播与本地容量模块开关 |
| `scripts/deploy-online.*` | 单机在线构建、部署和健康检查 |
| `scripts/package-offline.*`、`deploy-offline.*` | 离线包生成、校验、安装与升级 |
| `scripts/cluster-up.*`、`cluster-deploy.*` | 集群向导全流程；按已渲染配置分步部署 |
| `scripts/video-module.*`、`capacity-module.*` | 已部署环境的模块开关与状态 |
| `scripts/init-offline-env.*`、`prepare-public-bundle.py`、`next-release-version.py` | 公开离线包移除现场凭据、保留工具账号默认值，目标机生成内部密钥及 Release 版本生成 |
| `scripts/repair-offline-openeuler.sh` | openEuler 离线 Docker / SELinux 修复 |
| `scripts/fetch-deepseek-harness.sh`、`scripts/lib/` | 固定版本 Harness 获取及各入口复用的部署/云 API/演示实现 |
| `scripts/generate-demo-data.mjs`、`scripts/tests/` | 演示入口、协议模拟器与按场景启用的冒烟；前端浏览器用例见下节 |

专项回归按对应改动运行：`python3 scripts/tests/public-bundle-test.py` 与 PowerShell 同名脚本检查公开包凭据；`python3 scripts/tests/release-version-test.py` 检查版本生成。`nginx-protocol-routing-smoke.mjs` 用 `IOT_TEST_NGINX` 指定 nginx 可执行文件，启动并清理隔离 HTTP/nginx 进程；`platform-data-permissions-smoke.sh` 需要真实 Docker 与本地镜像，创建并清理自己的测试容器。它们不属于纯源码检查。

### 设备接入回归

流程、状态和接口统一见[设备接入](INTEGRATION.md#设备接入)，此处只维护回归入口。按改动范围执行：

```bash
go test ./internal/onboarding -count=1
go test ./internal/httpapi -run 'Test(Template|Onboarding|StandardProtocolReadOnlyPreview|GoFunctionsUploadAndListener|GoProtocolListenerSourceHotSwitch|SourcePublication)' -count=1
go test -race ./internal/adapters/memory ./internal/protocolruntime -run 'Test(TemplateSwitch|PreparedEnrollment|Onboarding|PollingBinding)' -count=1
node --test iot_front/tests/onboarding-workflow.test.mjs iot_front/tests/protocol-generation.test.mjs iot_front/tests/list-behavior.test.mjs
```

覆盖模板草稿与首台通过正式上报入口的验证、隔离候选配置的应用与回滚、过期验收证据拒绝、批量重试和凭据交付、用户权限，以及只读样本不写业务数据。`internal/repositorytest/` 的共享用例同时检查 memory / PostgreSQL 的修订冲突、准备快照竞态和完整配置原子提交；Modbus 回归检查切换时归档实际使用的版本。

真实 PostgreSQL 通过私有进程环境设置 `IOT_TEST_POSTGRES_DSN`，指向独立测试数据库，然后执行：

```bash
go test -race ./internal/adapters/postgres -run 'Test(TemplateSwitch|PreparedEnrollment|Onboarding)' -count=1
```

这些仓储用例在连接目标中创建并清理临时 schema；未设置 DSN 会跳过，不能写成持久化联调通过。浏览器条件与用例见[浏览器验证](#浏览器验证)。源码、模拟设备和隔离数据库测试不代替现场设备验收；另起临时 API 时按[进程交接](DEPLOYMENT.md#本地-api-进程交接)释放端口与收件箱锁，结束后不留下后台实例。

### 接口路由

HTTP 路由按业务区域注册：各区域的 `xxxRoutes()` 写在对应处理文件或 `internal/httpapi/routes.go`，`routeModules` 决定注册顺序，`routes()` 只遍历模块并设置兜底处理。`TestRegisteredRoutesMatchSnapshot` 把全部“方法 + 路径”固定在 `internal/httpapi/testdata/routes.txt`；有意新增或删除路由时用 `IOT_UPDATE_ROUTES=1 go test ./internal/httpapi -run TestRegisteredRoutesMatchSnapshot` 更新快照，并同步权限目录（`access_control.go` 的 `routeMenu` / `routeAction` / `protectedRead`）。

### AI 与知识库回归

源码回归使用 `go test ./internal/core ./internal/httpapi ./internal/adapters/embedding ./internal/adapters/knowledge ./internal/backup`，Harness 使用 `node --test deploy/deepseek-harness/gateway.test.mjs`。知识任务测试覆盖进度、失败重试、重启恢复、删除、租户/Agent 范围和向量空间原子切换。

业务工作流的提示词与版本号集中在 `internal/aiprompt`（`versions.go`、`prompts.go`）；修改提示词或研判上下文时同步提升对应版本号，运行记录与研判质量统计按版本区分。`go test ./internal/aiprompt` 不调用真实模型：`testdata/samples.json` 中每条样例以固定上下文生成提示词，经 `internal/aitest` 的脚本化 Harness 返回录制的答案，再用 `internal/aioutput` 解码并核对风险等级、列表与规则字段；同时校验提示词中的输出契约与解码器一致、版本号互不相同。新增工作流或输出格式时补充样例。`cmd/harness-mock` 的固定答案与真实网关同样携带 `usage`，其测试用同一解码器校验。

| 联调 | 配置与命令 | 验证范围 |
| --- | --- | --- |
| PostgreSQL 知识索引 | `IOT_TEST_POSTGRES_DSN` 指向独立测试库；`go test -race ./internal/adapters/knowledge ./internal/core -run 'Test(PostgresKnowledgePersistenceScopesAndAtomicRebuild|KnowledgeRuntimePostgresHTTPJobsRecoveryAndModelSwitch)'` | 测试自行创建并清理 schema；真实 PostgreSQL + 模拟 Embedding HTTP，涵盖持久索引、任务恢复、多副本配置激活 |
| FULL 隔离恢复 | 用 `IOT_BACKUP_FULL_RESTORE_TEST_ENV` 指向私有测试环境文件；`go test ./internal/backup -run TestFullKnowledgeAndAgentRestoreIntegration -count=1` | 独立源 fixture、恢复数据库、RustFS 备库与 Harness 恢复目录；核对知识表、向量、原件和 Agent/会话 |
| 活跃 Harness 快照 | 配置服务端快照 URL/令牌并设置 `IOT_BACKUP_LIVE_HARNESS_TEST=1`；`go test ./internal/backup -run TestLiveHarnessSnapshotRestoreIntegration -count=1` | 只读收集实例快照，在临时隔离目录核对恢复数量和内容 |

私有测试环境文件须限制访问权限，不提交或输出凭据。模拟 Embedding 测试不依赖向量服务；真实索引验证需启动随平台部署的 `embedding` / `reranker` 服务（或配置外部 API），上传后确认状态达到 `INDEXED` 并检索到对应分片。配置与索引生命周期见 [知识库向量服务](DEPLOYMENT.md#知识库向量服务)。

### 消防管理回归

```bash
go test -race ./internal/firesafety ./internal/httpapi ./internal/adapters/memory
go test ./internal/adapters/postgres -run FireSafety -count=1
node --test iot_front/tests/duty.test.mjs iot_front/tests/fire-safety-management.test.mjs
```

业务及 HTTP 用例覆盖排班冲突与换班审批、巡检整改复核、出勤库存占用、过期版本、租户与菜单权限，以及仓储装饰链。真实 PostgreSQL 用例须通过进程环境提供 `IOT_TEST_POSTGRES_DSN`；测试自行创建并清理临时 schema，核对 revision 并发提交、重复迁移和重新连接后的持久化。未配置时跳过，memory 测试不能代替持久化联调。

浏览器核对须使用两个分别获授申请与审批操作的账户，检查跨午夜排班、整改驳回后重提、出勤归队、刷新后的记录和窄屏滚动。现有演示数据脚本和容量模块场景未覆盖这三项现场管理业务；流程与接口见 [消防管理](FIRE_SAFETY.md)。

### 外部数据接入回归

```sh
go test ./internal/externaldata ./internal/httpapi ./internal/core
go test ./internal/adapters/memory ./internal/adapters/postgres ./internal/backup -run ExternalData
```

PostgreSQL 与备份集成测试沿用 `IOT_TEST_POSTGRES_DSN`，使用隔离 schema，不将真实环境连接串写入报告。浏览器测试入口在 `iot_front/tests/browser/external-data-*`；专用 fixture 需显式设置 `IOT_EXTERNAL_BROWSER_FIXTURE=1` 启动，使用内存仓储和测试账户，不连接真实业务数据库。业务流程及现场验收边界见[外部数据接入](EXTERNAL_DATA.md)。

### 消息主题回归

仓库根目录执行 `go test -race ./internal/messagetopics` 和 `go test -race ./internal/adapters/memory ./internal/adapters/postgres ./internal/httpapi -run 'TestMessageTopic|TestSharedTopic'`，验证主题与查询、订阅密钥的原子保存、SQL 与表单往返、字段投影、类型及条件比较、真实数据预览、定时快照完整性、设备范围、历史授权、临时凭据、密钥轮换与删除、撤销重试及并发冲突。解析主链路由 `go test ./internal/core -run 'TestParsedMessageFanoutRequiresSuccessfulParsing|TestProcessorOnlyEngineConsumesBusinessStream'` 验证，包括成功解析后按查询条件向 MQTT / Kafka 发布，以及不匹配或解析失败不发送。PostgreSQL 测试通过私有环境变量 `IOT_TEST_POSTGRES_DSN` 连接现有依赖，自行创建并清理隔离 schema。

真实 Broker 测试为 `go test -race ./internal/messagetopics -run TestMessageTopicsExisting -v`，仅在明确配置以下私有进程环境时执行：

- MQTT：`IOT_TEST_MESSAGE_TOPICS_MQTT_BROKER`，以及 `IOT_TEST_MESSAGE_TOPICS_MQTT_USERNAME` / `IOT_TEST_MESSAGE_TOPICS_MQTT_PASSWORD`；也可只提供 `IOT_TEST_MESSAGE_TOPICS_JWT_SECRET`，由测试签发精确临时主题、有效期 2 分钟的 JWT。
- Kafka：`IOT_TEST_MESSAGE_TOPICS_KAFKA_BROKERS`（逗号分隔），认证使用同前缀的 `_USERNAME`、`_PASSWORD`、`_MECHANISM`（默认 `SCRAM-SHA-256`）；TLS 使用 `_TLS` 和可选 `_TLS_CA_FILE`。连接身份须有测试主题的创建、发布、读取及删除权限；默认部署已启用 SASL，须提供认证参数。

测试使用随机租户前缀、独立客户端和非 retained MQTT 消息；Kafka 创建独立单分区 Topic，仅清理确认本轮创建成功的主题，不改运行账号、ACL 或既有主题。不配置时测试跳过。前端回归为 `node --test iot_front/tests/message-topics.test.mjs`；浏览器应另外检查主题创建/编辑/删除、查询预览、订阅密钥授权、分页设备选择、开放接口密钥轮换、整页刷新、只读内置主题和窄屏弹窗。

工具账号回归为 `go test -race ./internal/adapters/mqtt -run TestExistingMQTTToolCredentials -count=1 -v`，需要 `IOT_TEST_MESSAGE_TOPICS_MQTT_BROKER` 及 `IOT_TEST_MQTT_TOOL_USERNAME` / `IOT_TEST_MQTT_TOOL_PASSWORD`，核对发布/订阅、错误密码与匿名拒绝，不改账号或认证链。数据库工具账号引导回归为 `go test ./deploy/toolaccounts`，使用模拟客户端检查仅创建缺失账号、保留已有账号及失败清理。

授权回归与上述路由回归分开：

- `go test -race ./internal/adapters/mqtt -run 'TestAdminTopic|TestExistingMQTTTopic' -count=1 -v`。真实用例需要 MQTT Broker、JWT secret，以及 `IOT_TEST_MESSAGE_TOPICS_EMQX_URL` / `_EMQX_KEY` / `_EMQX_SECRET`；显式测试也可用 `_EMQX_TOKEN` 替代管理 API key。仅生成随机临时账号、非 retained 消息和可清理的临时 ban，验证分别授权发布/订阅、越权拒绝、踢线及拒绝重连；`TestExistingMQTTTopicClientPublishSubscribe` 只需 Broker 与 JWT secret。
- `go test -race ./internal/adapters/kafka -run TestConsumerAdminDisposableSecuredBroker -count=1 -v`。只连接独立临时安全 Broker，配置 `IOT_TEST_SECURED_KAFKA_BROKERS` / `_USERNAME` / `_PASSWORD` / `_MECHANISM` / `_ADMIN_URL`。测试验证匿名拒绝、SCRAM、精确读写主题/消费组 ACL、只读拒绝发布、只写拒绝订阅、已有连接撤销、幂等、授权失败清理，以及平台 Bus 发布/订阅/健康/lag/容量查询的认证路径。测试不改变 Broker 安全开关；只删除 Broker 明确确认本轮创建成功的随机主题，已存在主题、内部队列和创建结果不明的主题不删除，已有主题也不写入测试消息。
- Kafka 公开地址只读检查：另配 `IOT_TEST_SECURED_KAFKA_PUBLIC_BROKERS` 后执行 `go test -race ./internal/adapters/kafka -run TestConsumerAdminSecuredPublicListeners -count=1 -v`，核对内外 listener 及其广告地址的非空 `clusterId` 一致且均拒绝匿名连接，不创建主题或账号。生产配置使用 `IOT_KAFKA_PUBLIC_BROKERS`，缺失地址、跨集群、缺失集群身份和最多 115 项授权的边界另由同包及配置单测覆盖；部署参数见 [Kafka 对接账号认证与授权](DEPLOYMENT.md#kafka-对接账号认证与授权)。

## 管理端开发

Vue 3 + Vite，沿用 Naive UI、Tailwind CSS 和 Lucide；依赖与 Node 版本以 `iot_front/package.json` 和锁文件为准。`npm --prefix iot_front ci` 安装依赖，`npm --prefix iot_front run dev` 启动。页面通过 `src/api.js` 调用同源接口；Vite 默认代理到 `localhost:8081`，可用 `VITE_API_PROXY_TARGET` 覆盖。生产镜像以 `iot_front` 为构建上下文。

- 页面在 `iot_front/src/views/`，共享业务组件在 `src/components/`，控件适配在 `src/ui/`；运维页面与数据适配见 [运维中心](PLATFORM.md#运维中心)。
- 颜色、字号、间距、圆角与阴影集中在 `src/theme/tokens.css`；`naiveTheme.js` 解析变量生成 Naive UI 主题，不用散落的颜色值或 `!important` 覆盖组件。
- 元素布局在 `styles/base.css`，应用框架在 `shell.css`，减少动态效果在 `motion.css`，共用业务样式在 `patterns.css`；页面专用样式留在对应 Vue 文件。
- 列表复用 `FilterBar`、`DataTableCard`、`StatusDot`、`RowActions`，窄屏侧栏为抽屉，长弹窗正文独立滚动。权限控制使用 `src/permissions.js`，实际授权仍由服务端校验。
- Node 测试保留实际行为、失败分支与隔离边界；不用固定菜单数量、文案、样式写法或复制版本号的断言代替功能检查。

### 页面地址

每个菜单对应一个地址（`/alarms`、`/ops-overview` 等，菜单键转为短横线形式），告警详情为 `/alarms/<告警编号>`，告警通知中的详情链接即使用该地址；刷新、前进后退和登录前打开的深链接都按地址恢复页面，无权限或未知地址回到首个可用页面。实现见 `iot_front/src/routing.js`，Web 的 nginx 与 Vite 开发服务器均把未知路径回退到 `index.html`。

### 浏览器验证

先在 `iot_front` 执行 `npm run build`，用 `IOT_TEST_BROWSER` 指定 Chrome / Edge 可执行文件。专项脚本位于 `iot_front/tests/browser/`，使用 Node.js 22.12+ 的原生 WebSocket，不包含在 `npm test` 中；共用 `tests/helpers/browser.mjs` 管理独立浏览器、CDP 超时和临时目录清理，场景断言留在各脚本中：

| 场景 | 入口与条件 |
| --- | --- |
| 合成界面、弹层、窄屏 | 在 `iot_front` 启动 `node tests/browser/ui-preview.mjs`，另开终端运行 `naive-pages-check.mjs`、`onboarding-modes-check.mjs`、`protocol-actions-check.mjs`（均在 `tests/browser/`）；只访问回环夹具 |
| 告警手动研判、AI 工作流停止 | 仓库根目录运行 `node iot_front/tests/browser/alarm-http-check.mjs`、`node iot_front/tests/browser/ai-workflow-runs-check.mjs`；各自启动合成 API，覆盖手动发起研判、停止确认、停止中状态和手动刷新，无须真实模型服务 |
| 用户管理与设备范围 | 仓库根目录运行 `node iot_front/tests/browser/access-management-check.mjs` 或 `device-scope-check.mjs`；先启动前后端并按脚本配置管理员环境，创建后清理临时账户；设备范围用例需至少两台设备，且一个独立设备已有告警 |
| 接入、通信与命令 | 根目录运行 `go test ./internal/httpapi -run 'Test(OnboardingBrowser\|DeviceOnboardingBrowser\|GoFunctionsUploadAndListener\|TCPParentChildSourceChain)$' -count=1`；Go 用例负责隔离 API 与浏览器生命周期 |
| 摄像头真实播放 | `node iot_front/tests/browser/camera-live-check.mjs`；需 API、前端、媒体服务与已配置的摄像头，见 [摄像头](PLATFORM.md#摄像头) |

macOS 若提前结束无头 Chrome，检查系统的后台运行授权；浏览器脚本可用 `IOT_TEST_HEADFUL=1`。源码测试、合成浏览器和真实设备验证分别记录，跳过项不算通过。

## 首页统计

首页通过 `GET /api/v1/dashboard?days=7&offset=480` 读取聚合数据。内置管理员查看当前租户；普通用户需要运行总览菜单，统计仅包含其有权访问的设备及告警，没有设备管理菜单或设备范围时返回零值。`days` 支持 7、30，`offset` 为相对 UTC 的分钟偏移（默认 480；页面使用浏览器当前偏移）。日期范围包含今天，按固定时区的自然日划分。

- 设备总数、状态与产品分布只统计已登记设备；`ONLINE`、`ALARM` 归为在线，未产生运行状态的设备归为待连接。
- 活动告警及其等级只统计 `ACTIVE`，高等级为 `HIGH` 和 `CRITICAL`，不受告警列表分页影响。
- 趋势按 `firstTriggeredAt` 统计每日新增告警记录，包含已确认、恢复或关闭的记录；重复触发次数不作为新增条数，缺失日期补零。
- `alarmStatuses`、`alarmTypes` 按所选时段内首次发生的告警统计，处置分布使用这些记录的当前状态，类型排行展示前五类并合并其余类型；两者总量与趋势一致，不受列表分页影响。
- `connections`、`dataStatuses` 展示已登记设备的当前连接与数据活跃状态，与告警时段无关；空状态归为 `UNKNOWN`，不把连接状态等同于业务在线状态。
- 产品图展示数量最多的五项，其余合并为“其他产品”。实时通知合并刷新，失败保留上次成功数据。
- “消防站概况”仅对有消防站管理菜单的用户显示，单独读取 `GET /api/v1/fire-stations/statistics`（不带时间范围，出勤与归队为累计次数；只统计已启用的消防站和人员），读取失败只影响该区块。

全部设备范围使用仓储聚合查询；指定设备范围经请求级设备仓储过滤后统计，不把全租户计数返回给受限用户。内存实现保持相同口径。入口为 `internal/httpapi/dashboard.go`，回归测试 `TestDashboard` 同时支持内存与 `IOT_TEST_POSTGRES_DSN` 指定的独立临时数据库 schema。

## 列表与分页

采用 `internal/httpapi/pagination.go` 的接口支持 `page/pageSize`，兼容 `limit/offset`，每页默认 20 条、上限 100 条，返回 `items`、`total`、`page`、`pageSize`。正数 `page` 优先于 `offset`；超大参数收敛到整数安全上界，越界页返回空列表并保留实际总数，非数字沿用默认行为。

需要完整目录的关联选项通过 `apiAll` 逐页加载，与表格当前页分开保存；任一页失败则整体失败，不显示不完整目录。摄像头关联设备按关键词向服务端检索，不预先拉取全部设备。列表仅允许最新请求写入数据、总数和加载状态，旧请求不覆盖当前结果。逐页请求不保证数据库快照一致性。

设备管理将设备分组、类型、关键词与运行状态交给服务端筛选分页，切换条件重置页码。协议目录按协议条目前端分页，版本在条目内展示。服务端设备、状态、告警、原始报文和总览均先按用户范围过滤再计数；具体权限见 [用户权限](PLATFORM.md#权限与设备范围)。

对应回归入口为 `go test ./internal/httpapi -run 'Test(OversizedPagination|PaginationArithmetic|PageItems|ParseListPagination)'` 和在 `iot_front` 中执行 `node --test tests/list-behavior.test.mjs`。

### 原始报文筛选

原始报文页支持设备标识、报文标识、解析状态、接收时间，以及“更多筛选”中的产品标识、协议、报文格式、消息类型和解析器；可组合查询，并提供最近 1 小时、24 小时、7 天快捷范围。标识和解析器使用完整值精确匹配，协议及格式忽略大小写。点击“查询”应用条件，翻页与刷新保留已应用条件，“重置筛选”清空全部条件并返回第一页。

`GET /api/v1/raw-messages` 对应参数为 `deviceId`、`messageId`、`productId`、`protocol`、`payloadFormat`、`parseStatus`、`messageType`、`parser`、`start`、`end`；时间为包含端点的接收时间毫秒值。解析状态为 `PARSED`（已存在标准消息）、`FAILED`（无标准消息且记录解析错误）、`UNPARSED`（无标准消息且无解析错误）。消息类型及解析器依据该原文最新的标准消息。筛选在存储查询阶段、分页之前执行，列表总数使用相同条件，保留租户与用户设备范围限制。普通用户不因筛选获得额外设备访问权限。

### 原文回放

`POST /api/v1/raw-messages/replay` 的 `ratePerSecond` 省略或非正时沿用默认 100，正值上限为 10000；超限在创建任务前返回 422。该参数是请求的发送节奏，不是系统吞吐保证。

`DRY_RUN`、`DIFF` 和 `REINGEST` 共用协议版本选择逻辑。默认使用原文归档的协议、点表版本及 `metadata.protocolState` 帧前快照；显式指定 `parserVersion` 时，对已绑定协议的原文选择该协议的新版本及其点表版本。不存在或已撤销的版本计为失败，不发布到原始消息队列。回放不修改原始归档。

`REINGEST` 可重新解析尚未成功存储的报文；已完成处理的标准消息保留现有幂等行为，不覆盖已成功的数据，也不重复触发告警。异步消息队列发布成功只代表回放投递成功，实际解析、入库和告警结果须分别核对。

## 演示数据与功能检查

`node scripts/generate-demo-data.mjs --help` 查看参数；需 Node.js 22.12+ 和前端 npm 依赖。先用 `--dry-run` 查看计划：

```bash
npm --prefix iot_front ci
node scripts/generate-demo-data.mjs --origin http://<平台地址>:8080 \
  --env-file /path/to/private-demo.env --tenant <测试租户> --dry-run
```

删除 `--dry-run` 后会真实写入并保留演示设备、协议、规则、摄像头、用户、知识文档和备份；只在测试环境执行。配置文件提供管理员凭据，不放在命令参数中。AI/备份读取当前租户；脚本不切换 Provider，不恢复数据库。`--skip-ai`、`--skip-backup`、`--skip-mqtt`、`--skip-sockets` 分别跳过对应阶段，跳过项不算通过。

TCP/UDP 默认 29075/29076，须已映射且空闲；可用 `--tcp-port`、`--udp-port` 修改。脚本检查本次网关/产品/协议及 `LISTENING` 状态，不改防火墙。`--skip-sockets` 只创建停用网关。前缀默认唯一，重复同前缀会更新资源并轮换凭据，不能同时运行该前缀模拟器。

报告在 `.e2e/<前缀>/report.html`、`report.json`，明细包含失败和跳过项，非零退出码表示失败或中断。演示账户密码默认随机且不输出，可显式提供 `IOT_DEMO_USER_PASSWORD`。运行结果与目标、租户、前缀绑定，不混用旧报告。

离线演示可在联网机用 `deploy/demo-data/Dockerfile` 构建 `iot-platform-demo:offline`，导出镜像和 SHA-256；目标机校验导入后，用 `docker run --network host` 执行同样参数。通过环境传管理员凭据，指定 `--output /workspace/report`，结束用 `docker cp` 取出报告后清理该演示容器。外网模型仍需联网。

回归入口为 `node --test scripts/tests/demo-data.test.mjs`；指定 `IOT_DEMO_TEST_API_EXE` 可启动隔离 API 检查真实 HTTP/Go 编译/TCP/UDP/Excel/权限链路。持续模拟器为 `scripts/tests/demo-devices.mjs`、`demo-mqtt-device.mjs`，参数见脚本，不计入日常单元测试。

## 容量验证

工具包含一键测量、业务模块场景、故障注入、管理页、续跑与跨运行比较；集群角色与部署见 [部署文档](DEPLOYMENT.md#集群部署)。目标环境按下方 [验收流程](#目标环境验收) 测扩容、长稳与故障容量。

### 一键容量测量

一条命令完成：计划校验 → 前置检查 → 准备测试设备 → 多 Agent 开环配速发压 → 逐实例采集 `/metrics` → 排空 → 按原文 ID 全量核对 → 边界搜索 → 报告。停止、失败也会写出部分报告。实现位于 `internal/capacity`，入口仍是 `cmd/capacity-test`。

```bash
go run ./cmd/capacity-test plan validate --plan cmd/capacity-test/examples/core-mixed.yaml
go run ./cmd/capacity-test run --plan cmd/capacity-test/examples/core-mixed.yaml --secrets capacity-secrets.yaml
go run ./cmd/capacity-test status --run <runId>
go run ./cmd/capacity-test stop --run <runId>          # 追加 --force 跳过排空
go run ./cmd/capacity-test report --run <runId>        # 只读证据重新生成报告
go run ./cmd/capacity-test run --plan <同一计划> --resume <runId>   # 控制器中断后续跑
go run ./cmd/capacity-test compare --runs <id1>,<id2>,<id3>        # 并列比较与扩容效率 E(n)
```

- **计划**：示例见 `cmd/capacity-test/examples/`（`core-mixed`、`quick-local`、`full-system`、`resilience`、`resilience-postgres`、`soak-24h`）。`preset` 为 `quick`（固定档回归，不认证最大值）、`capacity`（粗阶梯 → 二分 → 候选复测）、`soak`（单档长持有）或 `resilience`（固定背景负载 + 故障注入）。`suite: full` 时未启用的业务模块在报告中列为未覆盖，结论不会是全系统通过。未知字段直接报错。
- **清单**：`target.inventoryRef` 指向受信任清单，列出 API、MQTT/TCP 入口、每个平台进程的 `/metrics`（`combined`、`api`、`gateway` 及 `parser`/`processor`/`jobs` 等拆分角色都要列）、Agent 与核对库的秘密引用。可选 `web`（管理端地址，视频场景经其拉取 HLS）与 `nodes`（各主机 node-exporter 地址，报告生成主机 CPU/内存/磁盘图 `hosts.svg`，瓶颈归类识别主机饱和）。控制器只访问清单中的地址。
- **秘密**：计划与清单只写引用名；值来自环境变量 `TORCHLINK_CAPACITY_SECRET_<名称>`（`-`、`.` 换成 `_`，大写）或权限 0600 的 `--secrets` YAML 文件。需要：操作员 Bearer 令牌、核对用 PostgreSQL DSN（建议只读账户）、可选 ClickHouse URL、远程 Agent 共享令牌。报告生成时会检查秘密值没有出现在任何证据文件中。
- **测试设备**：通过 `/api/v1/onboarding` 在计划指定的现有标准协议产品下以 `trial: true` 创建试验设备，前缀区分；调用者仍须具有设备登记和模板配置权限。容量测试不会把自动创建的模板标记为已通过首台实机验证，普通设备登记仍须完成正式验证。`reuseDevices: true` 时凭据保存在 `<results>/.work/fixtures`（0600），不进入运行目录。测试结束保留设备以便复测，保留范围写在 `manifest.json`；不再需要时在管理页删除运行或清理全部测试数据（见 [测试数据清理](#测试数据清理)）。
- **启动失败**：前置检查或测试设备准备失败时，运行列表、详情和报告保留具体检查项或接口错误。此时尚未开始测量，容量结论为“证据不足”，不代表已达到系统容量上限。
- **MQTT 准备失败**：`token_credentials_401` 表示设备凭据被 API 拒绝，需核对设备启用状态、目标 API 和复用凭据；`token_product_disabled_401` 表示测试产品未启用，产品预检查也会拒绝 `ready:false`。页面可切换到“高级 YAML”，将 `fixtures` 下的 `reuseDevices: true` 改为 `reuseDevices: false` 后重试：这会使用带本次运行后缀的新设备，不删除或重置原设备。如果新设备仍失败，继续核对产品状态与 API 环境；不要删除数据卷或手工发送缓存文件中的密钥。`token_http_<状态码>` 保留其他 HTTP 错误的状态，`token_invalid_response` 表示成功响应缺少有效令牌或主题。
- **实时推送准备失败**：启用 `realtime` 时每个订阅者都须取得令牌、连接 broker 并获准订阅告警或设备状态主题，否则启动即失败，不进入测量。失败码 `realtime_token_<状态码>` 为取令牌失败，`realtime_connect_bad_credentials` 为 broker 拒绝凭据（核对 EMQX JWT 密钥与平台 `IOT_JWT_SECRET`），`realtime_subscribe_denied` 为订阅被拒，`realtime_no_alarm_or_state_scope` 为操作员缺少告警或设备菜单权限，`realtime_no_mqtt_url` 为清单未配置 `mqtt`。
- **远程 Agent**：负载机执行 `capacity-test agent --listen :7070 --token-ref capacity-agent --secrets <文件>`，并在清单 `agents` 中登记 URL。Agent 持有 20 秒租约，控制器失联后自动停发；旧运行或旧代次的指令被拒绝。
- **判定**：每档检查实发达成率（未发出记为发压不足，不是服务失败）、入口成功率（429 为策略限制）、查询与业务完成 P95/P99（样本不足不输出分位）、积压趋势、排空与 ID 核对。业务完成时延取标准消息 `processed_at`（毫秒完成时间）与预定发送时刻之差，经数据库和 Agent 时钟偏差校正。每档结束后等待平台就绪且积压清零（最长取 `search.drainTimeout` 与 2 分钟的较大值）再开始下一档，避免失败档的积压拖垮后续档；仍未恢复时下一档记为 inconclusive（`not_recovered`）并停止搜索。结论写成“稳定通过 L、在 U 失败”“至少 L 尚未找到上限”“首档即失败”“结果不稳定”（曾通过的档在同一或更低速率复测未通过，没有稳定通过档）或 inconclusive，并给出结构化停止原因。
- **产物**：`capacity-results/<runId>/` 下有 `summary.json`、`phases.csv`、`report.md`、离线可打开的 `report.html`、`charts/*.svg`（`outputs.formats` 含 `png` 时另有同名 PNG，英文标签、UTC 时间）、`plan.sanitized.yaml`、`environment.json`、`manifest.json`、各档 `phases/`、`verification/`、`ledgers/`（gzip JSONL 发送账本）、`observations/metrics.jsonl`（及 `nodes.jsonl`）、`events.jsonl`、`checksums.txt`。缺测在表格和图中显示为空，不填 0。
- **告警序列**：`fixtures.alarmRuleId` 指定一条在 `stressAlarm=1` 时触发的现有规则（`alarmRecovers: true` 表示 `stressAlarm=0` 时恢复），核对阶段按设备比对上报序列与告警记录：有告警上报必须触发、最后一条为告警时必须处于活动状态、恢复规则最后一条正常时必须已恢复；不符计为完整性失败并给出样例设备。每档只核对本档触发的告警：窗口起点取上一档排空完成与本档开始的中点（首档为开始前 60 秒），上一档的告警不计入下一档。未指定时只观测 `alarm_trigger_total`。
- **续跑与比较**：控制器进程中断后，用同一计划执行 `run --resume <runId>`：已完成档位按原搜索顺序回放，Agent 以下一代次重新准备并复用已登记设备，中断的那一档重新执行；最近 30 秒仍有心跳或已完成的运行会被拒绝。`compare` 读取多个运行的摘要，负载组合（报文、查询、模块与 SLO）一致且都有确认通过档时计算 E(n) = (C(n)/C(n₀)) ÷ (n/n₀) 并输出 `scaling.svg`；实例数默认取各运行的平台指标目标数，可用 `--instances id=n` 指定；条件不一致时只并列并写明原因。

当前边界：TCP（GB26875）只有协议 ACK 层证据；视频场景不覆盖 WebRTC；重复业务副作用未测量。即使已启用全部可选场景，`suite: full` 仍因这些覆盖缺口判为 inconclusive，不能报告全系统通过。

容量回归入口为 `go test ./internal/capacity ./cmd/capacity-test ./internal/platformapp` 和 `go test ./internal/httpapi -run TestCapacityRunUsesAuthorizedTrialForUnverifiedTemplate`；前端为 `node --test iot_front/tests/ops.test.mjs`。真实 HTTP 回归使用内存仓储，覆盖新模板试验设备登记、上报解析、正式验收与权限边界；控制器测试覆盖启动失败原因在终态、接口及报告中的保留。核对数据库查询需另在私有进程环境设置 `IOT_TEST_POSTGRES_DSN`，执行 `go test ./internal/capacity -run PGStore`，测试自行使用临时 schema。

### 业务模块场景

计划 `modules` 中启用的模块由 Agent 以固定背景速率（按分钟或秒）与设备负载同时执行，各自记录成功率与时延（`p95` 设为 0 表示只记录不判定），并进入覆盖表：

| 模块 | 执行内容 | 前提 |
| --- | --- | --- |
| `ai` | 对测试租户的活动告警发起手动研判并轮询完成；`maxRuns` 为整次运行的硬预算 | `mode: mock` 时平台 `IOT_AI_HARNESS_URL` 指向 `cmd/harness-mock`（只测平台调度，报告明确标注）；`real` 会消耗模型额度 |
| `knowledge` | 上传生成的 Markdown 文档到指定 `workflowId`，轮询至 `INDEXED`；失败、删除中或超时计失败，时延包含索引等待 | PostgreSQL + pgvector 与已配置的外部 Embedding API 可用 |
| `video` | 建立并释放 HLS 播放会话；清单有 `web` 时拉取播放列表和首个分片近似首帧 | 测试摄像头在直播白名单内；WebRTC 未覆盖 |
| `backup` | 每档在测量窗口内执行一次备份、逐文件下载校验 SHA-256；`restore: true` 时恢复到独立库并核对条数 | 备份服务配置 `IOT_BACKUP_RESTORE_TARGET_DSN`（与业务库不同） |
| `realtime` | 按页面方式取 MQTT 令牌并订阅告警与设备状态推送，测送达时延 | 清单有 `mqtt`；订阅者分摊到各 Agent |
| `exports` | 原文批量下载、回放 DRY_RUN 任务、巡检并导出 PDF | 测试租户已有原文 |
| `openapi` | 用绑定用户的 API Key 查询开放接口 | `keySecretRef` 指向秘密文件中的密钥 |

`harness-mock` 用法：`IOT_AI_HARNESS_TOKEN=<平台同一令牌> go run ./cmd/harness-mock -listen :8091 -latency 2s -jitter 1s -concurrency 4`，超过并发返回 429，与真实网关一致。

### 故障注入（resilience）

故障命令只在 Agent 主机本地登记：`capacity-test agent --fault-allow faults.yaml`（进程内 Agent 用 `run --fault-allow`），文件权限须为 0600，格式见 `cmd/capacity-test/examples/faults.example.yaml`（动作名 → `inject`/`recover` 的 argv，不经 shell）。计划的 `faults.actions` 只能按名称引用，预检确认动作确实登记在对应 Agent；释放运行或控制器租约过期时 Agent 自动执行未完成的恢复。

`resilience` 档在测量窗口内按 `at`/`duration` 注入并恢复。常规 SLO 改为“仅记录”，完整性、排空和恢复时间决定结论：恢复时间从恢复命令完成起算，直到解析成功速率回到注入前基线的 90% 且积压回到基线附近，超过 `faults.maxRecovery` 判为失败。报告包含故障表与 `recovery.svg`。

每一档都核对平台的 `dlq_published_total`：窗口内有消息进入死信主题即判为完整性失败（该计数只在首次写入死信后出现，成功抓取但没有该计数视为 0）。`faults.zeroLoss: true` 要求证明故障期间零丢失：计划须设置 `fixtures.alarmFraction > 0`，并给出 `alarmRuleId` 或 `autoProvision`，以便逐设备核对告警序列；死信计数无法观测或告警未完成核对时，结论为证据不足。

`resilience-postgres.yaml` 在背景负载下让 PostgreSQL 中断 60 秒（单机为停止再启动；集群改用 HA 工具的主备切换，见故障白名单示例的注释），以“死信为 0、告警零丢失、已确认消息不缺失、在 `faults.maxRecovery` 内恢复”判定。依赖故障时消费者原位暂停重试，中断时长须小于 `IOT_CONSUMER_MAX_BLOCK`。`soak-24h.yaml` 是 24 小时固定速率长稳示例，覆盖夜间备份与保留任务。仓库只校验计划；实际长稳与切换结论须在目标环境运行后得出。

### 容量测试模块

平台部署时开启容量测试模块后（见 [部署文档](DEPLOYMENT.md#容量测试模块)），运维中心“容量测试”页直接选择测试类型运行：页面把表单生成计划（`iot_front/src/ops/capacity.js`，`fixtures.autoProvision: true`），平台补上调用者租户与为其签发的令牌，模块 `capacity-test serve --self` 从环境变量取得本平台的地址与核对库（`IOT_CAPACITY_API_URL`、`IOT_CAPACITY_METRICS`、`IOT_CAPACITY_POSTGRES_DSN` 等，含义见 `internal/capacity/self.go`），自动准备测试产品与规则后执行。页面“高级”开关可直接编辑计划 YAML。`internal/capacity/uiplan_test.go` 用 Go 规则校验表单能生成的全部计划。

本地 `setup-local.sh` / `.ps1` 默认设置 `IOT_OPS_CAPACITY_LOCAL=true`、`IOT_CAPACITY_MODULE=on`，容量控制器跟随 combined API 启停；IDE 和 `go run ./cmd/iot-platform --env-file .env.local` 都无需另起容量进程。旧本地配置只需补充下面两项并重启 API，无需重建依赖：

```dotenv
IOT_OPS_CAPACITY_LOCAL=true
IOT_CAPACITY_MODULE=on
```

控制器仅监听 `127.0.0.1` 的动态端口，服务令牌仅保存在进程内；复用当前 API 地址、MQTT、PostgreSQL 和 ClickHouse 配置，Web 入口为本地 Vite `127.0.0.1:5173`，结果保存在 `IOT_DATA_DIR/capacity-results/`。启动不会创建测试设备或自动发压，页面仍执行运维租户、菜单/操作权限和开始确认。`IOT_CAPACITY_MODULE=off` 显式关闭；已配置独立的 `IOT_OPS_CAPACITY_URL` / `IOT_OPS_CAPACITY_TOKEN` 时沿用独立服务，不再启动本地控制器。拆分角色使用部署模块或下方的独立控制服务。

本地进程内发压与平台共享 CPU/内存，只用于功能调试，不能当作独立发压机测得的生产容量。停止 API 会同时停止控制器和正在执行的测试。

### 独立压测环境的控制服务

对独立的受测环境（远程 Agent、故障白名单等），在控制机运行控制服务，页面按清单名称选择环境：

```bash
go run ./cmd/capacity-test serve --listen 127.0.0.1:7080 --inventories <受信任清单目录> --token-ref capacity-service --secrets capacity-secrets.yaml --results capacity-results
```

清单目录中每个 `<环境名>.yaml` 即一个可选环境；页面只提交环境名与计划文本，秘密、地址与故障命令留在控制机。平台设置 `IOT_OPS_CAPACITY_URL`（控制服务地址，只让平台 API 可达）与 `IOT_OPS_CAPACITY_TOKEN`（与秘密文件中 `capacity-service` 相同，至少 32 个字符）。菜单 `opsCapacity` 只在 `IOT_OPS_TENANTS` 中授予，查看、校验、启动、停止、下载与清理分别授权；同一时间只运行一个测试，控制服务收到终止信号时会先软停止当前运行并写出报告。

### 测试数据清理

测试数据以容量测试自动创建的专用产品认定归属：产品名称为“容量测试标准设备 <产品编号>”、说明以“capacity-test 自动创建”开头、使用标准协议；设备名称为“容量测试 <设备编号>”。前置检查要求计划使用这样的专用产品（开启 `fixtures.autoProvision` 自动准备），不能向业务产品写入测试数据。

- **删除运行**：运行结束、失败或取消后，在该行点击“删除运行”。确认框显示仅本次使用的设备数和仍被其他运行使用的设备数。控制服务在后台删除本次运行的报告、账本、工作目录和凭据缓存，本次运行创建的 AI 研判、巡检、回放任务与知识文档（按 `capacityRunId` 标记），以及仅本次使用的设备及其全部原文、解析记录、告警、状态和审计；没有其他运行使用该产品时一并删除专用产品和测试规则。失败时保留运行记录并显示原因，可重试。
- **清理全部测试数据**：页首按钮预览本环境已结束的运行数、专用测试产品及其设备和原文数量，确认后在后台删除全部专用测试产品的设备与数据、产品与测试规则、所有带运行标记的测试任务和文档，以及全部运行记录。有测试运行未结束时拒绝执行。页面显示实际处理进度与结果，离页后可通过状态恢复。

接口：`GET /api/v1/ops/capacity/runs/:id/cleanup`（单次预览）、`DELETE /api/v1/ops/capacity/runs/:id`（删除运行）、`GET|POST /api/v1/ops/capacity/cleanup?environment=<环境>`（预览 / 清理全部）和 `GET /api/v1/ops/capacity/cleanup/status?environment=<环境>`。控制服务以操作员的委托凭据回调 `GET /api/v1/ops/capacity/cleanup-fixtures` 与 `POST /api/v1/ops/capacity/cleanup-data`，平台按已存储的产品和设备核对归属，浏览器不能指定租户、设备或报文。全部使用 `DELETE /api/v1/ops/capacity/runs/:id` 操作权限，并要求全租户设备范围。

边界：

- 被网关、摄像头、接入配置引用，或改名为业务用途的设备会阻止清理；仍在解析或处理的报文、运行中的测试任务须等待完成后重试。
- 测试产品仍有其他运行使用的设备，或被业务规则、接入配置、知识文档引用时保留，并在结果中说明。
- MQTT 只向被删除设备的精确状态主题发送 retained 清除请求（平台收到空状态消息时直接确认，不入收件箱），并丢弃处理清理请求的平台进程本地收件箱中这些设备的待处理与已隔离消息；拆分部署时接入网关进程各自的收件箱不在此范围。Kafka 中的测试消息按主题保留策略自然过期，不逐条删除。
- 监控历史、平台全量备份与恢复目标保留。
- 部署时 API、Web 和容量测试模块需一起更新；远程 Agent 也要更新才能清理其运行工作目录。

### 单项工具

在隔离环境使用实际设备凭据与代表性报文。HTTP 202、MQTT PUBACK、TCP ACK 只代表各自接收阶段，不代表解析、存储与告警完成；持续增长的积压说明当前速率不可持续。`capacity-test` 只保留按计划运行的子命令（`plan validate`、`run`、`status`、`stop`、`report`、`compare`、`serve`、`agent`）；HTTP、MQTT、TCP 场景、管理查询和端到端核对均通过计划的负载组合与业务模块配置，`quick` 预设可用于小规模定向检查。

每份报告记录日期、提交、硬件、部署形态、设备数、协议/报文、连接池和并发、规则/AI 配置、速率/时长、错误与丢失、队列起止量、端到端延迟及恢复时间。设备凭据与令牌文件含秘密，测试后清理。

### 目标环境验收

固定提交、镜像摘要、硬件、故障域、数据量、协议/报文比例、规则/AI 配置、SLO 和时长。被测机与发压机分开，清单列出每个角色的每个实例 `/metrics`、各主机 node-exporter、Agent 与核对库；同步时钟，给发压机保留 CPU 余量。秘密使用 0600 文件及只读核对账户，测试使用专用租户；AI 先用 `harness-mock` 验证调度，再以 `maxRuns` 预算执行真实模型。备份恢复须配置独立目标库，视频须有可用测试摄像头。

| 阶段 | 操作与通过依据 |
| --- | --- |
| 低档基线 | `quick` 预设确认接入、业务完成、ID 核对与报告证据完整；ACK 不代替处理完成 |
| 单实例边界 | `capacity` 搜索“稳定通过 L、在 U 失败”；未找到失败档只报告至少 L，发压受限或配额限制单列 |
| 扩容比较 | 同一计划分别测 1 / 2 / 3 / 6 个目标角色实例，只改变实例数并更新指标清单；比较共享存储、分区与连接预算 |
| 候选长稳 | 以候选运行速率执行 `soak`，至少 4 小时，上线前建议 24 小时；完整性、积压、业务 P95/P99 与资源趋势均达标 |
| 故障容量 | 每种故障单独用 `resilience` 在背景负载下注入、恢复；以完整性、排空与 `faults.maxRecovery` 判定，并核对人工恢复结果 |

扩容比较示例（运行 ID 对应实际结果目录）：

```bash
go run ./cmd/capacity-test compare --runs <n1>,<n2>,<n3>,<n6> --instances <n1>=1,<n2>=2,<n3>=3,<n6>=6
```

只有负载/SLO 一致且确认通过档、证据完整时才计算扩容效率 E(n)。原始实例数、资源增量与限制环节一并保留，不能以 API 数量乘单机吞吐代替测量。

故障白名单在对应 Agent 主机登记，计划按动作名引用，格式与恢复机制见 [故障注入](#故障注入resilience)。覆盖范围至少逐项标明：

| 故障对象 | 核对内容 |
| --- | --- |
| Parser / Processor / Jobs | 消费分区或任务租约接管、旧实例迟到写入、告警与设备状态完整性 |
| Gateway / EMQX | 设备重连、未确认消息重投、inbox 排空及唯一消息核对 |
| PostgreSQL / Redis / Redpanda / ClickHouse | 主备或分区切换、连接恢复、限额退化、quorum 写入及副本可读性 |
| Harness / 视频控制实例 | 会话重建、工作流失败分类、SIP 重新注册与重新点播、权限撤销 |
| 主机 / 磁盘 | 资源争用、磁盘写满、inbox 持久性与人工恢复边界；仅重启进程不覆盖这些故障 |

报告保存到独立的 `capacity-results/<runId>/`，包含执行人、日期、提交、清单和全部证据。推荐运行值须经过长稳验证；健康状态下的 0.7 系数不能证明 N−1 容量。未测或未通过的业务/故障逐项列明，不纳入承诺。

### 真实依赖回归与死信恢复

现有 Broker 断开回归可指定环境文件运行，不创建容器：

```bash
IOT_TEST_EXISTING_MQTT_ENV="$PWD/.env.local" go test ./internal/adapters/mqtt -run TestExistingBrokerDisconnectDuringDurableCallback -count=1
```

该测试只断开自己创建的临时订阅客户端，使用独立主题和临时目录。真实 PostgreSQL 测试使用 `IOT_TEST_POSTGRES_DSN` 指定数据库，在临时 schema 中建表并清理，不应把完整连接串写入终端历史。未配置时相应测试跳过。

消费者按错误类型处理失败消息：报文无法解码等永久错误直接转入死信；其他错误按退避重试，期间若同一消费组的其他消息仍在成功处理，判定为该消息自身问题，在 3 次尝试后转入死信；若所有消息都在失败（数据库、消息队列等依赖故障），该处理通道暂停并持续重试，依赖恢复后自动继续，火警等消息不会因短暂故障离开正常链路。等待超过 `IOT_CONSUMER_MAX_BLOCK`（默认 30 分钟）才转入死信。暂停期间 `consumer_blocked_seconds_<消费组>` 记录最长等待时间，规则 `ConsumerBlockedByOutage` 超过 1 分钟告警，`/health/ready` 在 2 分钟无进展后报告该消费组停滞。

运维中心“总览 → 死信消息”列出各环节保留的死信数量与最近记录（失败原因、内容预览），获授“查看死信消息”的运维用户可查看，“重新投递死信消息”把选中消息重新发布到该环节的原处理主题并写审计；处理按消息幂等，死信本身保留。

消息转入 `iot.dlq.<消费组>` 时计入 `dlq_published_total` 与 `dlq_published_<消费组>_total`，并记录错误日志；Compose 的 Prometheus 规则 `DeadLetterPublished` 立即告警，`KafkaConsumerLagHigh` 在消费积压持续 5 分钟超过一万条时告警。

存储死信恢复工具默认只读审计，显式指定租户、源业务主题和待恢复标准消息 ID 数组文件，核对全部 ID 后再追加 `-execute`：

```bash
go run ./cmd/dlq-replay -env-file .env.local -tenant <租户> -ids-file ids.json
# 核对后重新送入原存储消费链，不删除 DLQ、不重置消费者 offset
go run ./cmd/dlq-replay -env-file .env.local -tenant <租户> -ids-file ids.json -execute
```

重复死信按 messageId 合并，矛盾正文会拒绝整批发布。重新发布成功只代表 Kafka 收到；必须再核对 PostgreSQL `processed_at`、ClickHouse 行数/唯一 ID 和实际告警状态。发布途中失败可重跑同一 ID 列表，仍由原业务幂等处理。
