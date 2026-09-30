# 开发与测试

环境准备、源码启动、IDE 调试统一见 [部署与本地调试](DEPLOYMENT.md#本地运行)。本文维护开发约定、测试入口和演示工具；命令默认在仓库根目录执行。

## 源码与开发检查

| 入口 | 职责 |
| --- | --- |
| `cmd/iot-platform/`、`internal/platformapp/` | API 启动和依赖装配 |
| `internal/httpapi/`、`internal/core/`、`internal/adapters/` | 接口、业务、外部存储与服务 |
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
| 仓库根目录 | `go test ./cmd/... ./internal/...` | 正式后端源码，避免把本地生成目录纳入测试 |
| `protocol-packages/gb26875-dahua` | `go test ./...` | 独立协议及模拟器 |
| `dev/` 下各协议 module 目录 | `go test ./...` | 对应厂商协议；与根 module 分开执行 |
| `iot_front` | `npm test`、`npm run build` | 前端测试及构建；没有独立 lint/typecheck 脚本 |
| 仓库根目录 | `git diff --check` | 空白错误 |

本地集成入口：`go run scripts/tests/local-runtime-smoke.go --env-file .env.local` 检查依赖读写；前端、API 和备份启动后，`node scripts/tests/local-business-smoke.mjs` 检查登录、接入、归档、规则及回放。业务冒烟会创建唯一测试数据，结束停用本次规则与凭据并保留记录；仅在测试环境运行。

Kafka 消费失败三次后写入 `iot.dlq.<消费组>`，写入成功并提交原消息位点后才继续消费。读取出错的订阅按指数退避（最长 30 秒）自动重建；重建期间或在途消息 2 分钟无进展时，`/health/ready` 报告对应消费组未就绪。死信中的合法 JSON 报文保持 `payload` 原结构；非 JSON 或二进制报文放在 `payload` 的 Base64 字符串中，并带 `payloadEncoding: "base64"`，可还原原始字节。

真实依赖与浏览器检查按各测试的 `IOT_TEST_*` 环境变量启用，接入链路见 [接入验证](INTEGRATION.md#验证入口)。未配置而跳过的用例不算联调通过，历史测试记录可从 Git 历史追溯。

部署脚本修改使用独立 Compose 可执行文件（不能传 `docker compose` 子命令）：Bash 运行 `bash scripts/tests/deployment-smoke.sh /path/to/docker-compose`，PowerShell 运行 `pwsh -File scripts/tests/deployment-smoke.ps1 -ComposeExe /path/to/docker-compose`。它们使用真实 Compose 解析，模拟 Docker/HTTP 操作，不部署服务。安装器用例集中于 `scripts/tests/docker-bootstrap-smoke.sh`，openEuler 打包用例集中于 `scripts/tests/openeuler-smoke.sh`。

### 脚本入口

同一操作只保留 `.sh`（Linux / macOS）与 `.ps1`（Windows PowerShell），不再增加按操作系统命名的转发包装。DeepSeek 配置与 Harness 始终准备，只需填写自己的 API Key。旧 AI 可选安装、跳过 Harness、本地对话模型参数及 `--include-backup-service` 别名已移除；备份容器使用 `--include-backup` / `-IncludeBackup`。参数以脚本帮助为准，部署步骤见 [部署指南](DEPLOYMENT.md)。

| 入口 | 用途 |
| --- | --- |
| `scripts/setup-local.*` | 准备本地开发依赖，支持 VM 依赖、直播与本地容量模块开关 |
| `scripts/deploy-online.*` | 单机在线构建、部署和健康检查 |
| `scripts/package-offline.*`、`deploy-offline.*` | 离线包生成、校验、安装与升级 |
| `scripts/cluster-up.*`、`cluster-deploy.*` | 集群向导全流程；按已渲染配置分步部署 |
| `scripts/video-module.*`、`capacity-module.*` | 已部署环境的模块开关与状态 |
| `scripts/init-offline-env.*`、`prepare-public-bundle.py`、`next-release-version.py` | 公开离线包去除凭据、目标机初始化和 Release 版本生成 |
| `scripts/repair-offline-openeuler.sh` | openEuler 离线 Docker / SELinux 修复 |
| `scripts/fetch-deepseek-harness.sh`、`scripts/lib/` | 固定版本 Harness 获取及各入口复用的部署/模型/演示实现 |
| `scripts/generate-demo-data.mjs`、`scripts/tests/` | 演示入口、协议模拟器与按场景启用的冒烟；前端浏览器用例见下节 |

专项回归按对应改动运行：`python3 scripts/tests/public-bundle-test.py` 与 PowerShell 同名脚本检查公开包凭据；`python3 scripts/tests/release-version-test.py` 检查版本生成；`sh scripts/tests/ollama-restore-smoke.sh` 检查模型补齐及重跑。`nginx-protocol-routing-smoke.mjs`、`platform-data-permissions-smoke.sh` 需要真实 Docker 与本地镜像，会创建并清理自己的测试容器，不能当作纯源码检查运行。

## 管理端开发

Vue 3 + Vite，沿用 Naive UI、Tailwind CSS 和 Lucide；依赖与 Node 版本以 `iot_front/package.json` 和锁文件为准。`npm --prefix iot_front ci` 安装依赖，`npm --prefix iot_front run dev` 启动。页面通过 `src/api.js` 调用同源接口；Vite 默认代理到 `localhost:8081`，可用 `VITE_API_PROXY_TARGET` 覆盖。生产镜像以 `iot_front` 为构建上下文。

- 页面在 `iot_front/src/views/`，共享业务组件在 `src/components/`，控件适配在 `src/ui/`；运维页面与数据适配见 [运维中心](PLATFORM.md#运维中心)。
- 颜色、字号、间距、圆角与阴影集中在 `src/theme/tokens.css`；`naiveTheme.js` 解析变量生成 Naive UI 主题，不用散落的颜色值或 `!important` 覆盖组件。
- 元素布局在 `styles/base.css`，应用框架在 `shell.css`，减少动态效果在 `motion.css`，共用业务样式在 `patterns.css`；页面专用样式留在对应 Vue 文件。
- 列表复用 `FilterBar`、`DataTableCard`、`StatusDot`、`RowActions`，窄屏侧栏为抽屉，长弹窗正文独立滚动。权限控制使用 `src/permissions.js`，实际授权仍由服务端校验。
- Node 测试保留实际行为、失败分支与隔离边界；不用固定菜单数量、文案、样式写法或复制版本号的断言代替功能检查。

### 浏览器验证

先在 `iot_front` 执行 `npm run build`，用 `IOT_TEST_BROWSER` 指定 Chrome / Edge 可执行文件。专项脚本位于 `iot_front/tests/browser/`，使用 Node.js 22.12+ 的原生 WebSocket，不包含在 `npm test` 中；共用 `tests/helpers/browser.mjs` 管理独立浏览器、CDP 超时和临时目录清理，场景断言留在各脚本中：

| 场景 | 入口与条件 |
| --- | --- |
| 合成界面、弹层、窄屏 | 在 `iot_front` 启动 `node tests/browser/ui-preview.mjs`，另开终端运行 `naive-pages-check.mjs`、`onboarding-modes-check.mjs`、`protocol-actions-check.mjs`（均在 `tests/browser/`）；只访问回环夹具 |
| 告警手动研判、AI 工作流停止 | 仓库根目录运行 `node iot_front/tests/browser/alarm-http-check.mjs`、`node iot_front/tests/browser/ai-workflow-runs-check.mjs`；各自启动合成 API，覆盖手动发起研判、停止确认、停止中状态和手动刷新，无须真实模型服务 |
| 用户管理与设备范围 | 仓库根目录运行 `node iot_front/tests/browser/access-management-check.mjs` 或 `device-scope-check.mjs`；先启动前后端并按脚本配置管理员环境，创建后清理临时账户 |
| 接入、通信与命令 | [接入验证](INTEGRATION.md#验证入口) 中的 Go 集成用例负责隔离 API 与浏览器生命周期 |
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

全部设备范围使用仓储聚合查询；指定设备范围经请求级设备仓储过滤后统计，不把全租户计数返回给受限用户。内存实现保持相同口径。入口为 `internal/httpapi/dashboard.go`，回归测试 `TestDashboard` 同时支持内存与 `IOT_TEST_POSTGRES_DSN` 指定的独立临时数据库 schema。

## 列表与分页

采用 `internal/httpapi/pagination.go` 的接口支持 `page/pageSize`，兼容 `limit/offset`，每页默认 20 条、上限 100 条，返回 `items`、`total`、`page`、`pageSize`。正数 `page` 优先于 `offset`；超大参数收敛到整数安全上界，越界页返回空列表并保留实际总数，非数字沿用默认行为。

设备、产品、规则、摄像头的关联选项通过 `apiAll` 逐页加载，与表格当前页分开保存；任一页失败则整体失败，不显示不完整目录。列表仅允许最新请求写入数据、总数和加载状态，旧请求不覆盖当前结果。逐页请求不保证数据库快照一致性。

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

- **计划**：示例见 `cmd/capacity-test/examples/`（`core-mixed`、`quick-local`、`full-system`、`resilience`）。`preset` 为 `quick`（固定档回归，不认证最大值）、`capacity`（粗阶梯 → 二分 → 候选复测）、`soak`（单档长持有）或 `resilience`（固定背景负载 + 故障注入）。`suite: full` 时未启用的业务模块在报告中列为未覆盖，结论不会是全系统通过。未知字段直接报错。
- **清单**：`target.inventoryRef` 指向受信任清单，列出 API、MQTT/TCP 入口、每个平台进程的 `/metrics`（`combined`、`api`、`gateway` 及 `parser`/`processor`/`jobs` 等拆分角色都要列）、Agent 与核对库的秘密引用。可选 `web`（管理端地址，视频场景经其拉取 HLS）与 `nodes`（各主机 node-exporter 地址，报告生成主机 CPU/内存/磁盘图 `hosts.svg`，瓶颈归类识别主机饱和）。控制器只访问清单中的地址。
- **秘密**：计划与清单只写引用名；值来自环境变量 `TORCHLINK_CAPACITY_SECRET_<名称>`（`-`、`.` 换成 `_`，大写）或权限 0600 的 `--secrets` YAML 文件。需要：操作员 Bearer 令牌、核对用 PostgreSQL DSN（建议只读账户）、可选 ClickHouse URL、远程 Agent 共享令牌。报告生成时会检查秘密值没有出现在任何证据文件中。
- **测试设备**：通过 `/api/v1/onboarding` 在计划指定的现有标准协议产品下创建，前缀区分；`reuseDevices: true` 时凭据保存在 `<results>/.work/fixtures`（0600），不进入运行目录。测试结束不删除设备，清理清单写在 `manifest.json`。
- **MQTT 准备失败**：`token_credentials_401` 表示设备凭据被 API 拒绝，需核对设备启用状态、目标 API 和复用凭据；`token_product_disabled_401` 表示测试产品未启用，产品预检查也会拒绝 `ready:false`。页面可切换到“高级 YAML”，将 `fixtures` 下的 `reuseDevices: true` 改为 `reuseDevices: false` 后重试：这会使用带本次运行后缀的新设备，不删除或重置原设备。如果新设备仍失败，继续核对产品状态与 API 环境；不要删除数据卷或手工发送缓存文件中的密钥。`token_http_<状态码>` 保留其他 HTTP 错误的状态，`token_invalid_response` 表示成功响应缺少有效令牌或主题。准备失败发生在发压前，不能作为容量结论。
- **远程 Agent**：负载机执行 `capacity-test agent --listen :7070 --token-ref capacity-agent --secrets <文件>`，并在清单 `agents` 中登记 URL。Agent 持有 20 秒租约，控制器失联后自动停发；旧运行或旧代次的指令被拒绝。
- **判定**：每档检查实发达成率（未发出记为发压不足，不是服务失败）、入口成功率（429 为策略限制）、查询与业务完成 P95/P99（样本不足不输出分位）、积压趋势、排空与 ID 核对。业务完成时延取标准消息 `processed_at`（毫秒完成时间）与预定发送时刻之差，经数据库和 Agent 时钟偏差校正。结论写成“稳定通过 L、在 U 失败”“至少 L 尚未找到上限”或 inconclusive，并给出结构化停止原因。
- **产物**：`capacity-results/<runId>/` 下有 `summary.json`、`phases.csv`、`report.md`、离线可打开的 `report.html`、`charts/*.svg`（`outputs.formats` 含 `png` 时另有同名 PNG，英文标签、UTC 时间）、`plan.sanitized.yaml`、`environment.json`、`manifest.json`、各档 `phases/`、`verification/`、`ledgers/`（gzip JSONL 发送账本）、`observations/metrics.jsonl`（及 `nodes.jsonl`）、`events.jsonl`、`checksums.txt`。缺测在表格和图中显示为空，不填 0。
- **告警序列**：`fixtures.alarmRuleId` 指定一条在 `stressAlarm=1` 时触发的现有规则（`alarmRecovers: true` 表示 `stressAlarm=0` 时恢复），核对阶段按设备比对上报序列与告警记录：有告警上报必须触发、最后一条为告警时必须处于活动状态、恢复规则最后一条正常时必须已恢复；不符计为完整性失败并给出样例设备。未指定时只观测 `alarm_trigger_total`。
- **续跑与比较**：控制器进程中断后，用同一计划执行 `run --resume <runId>`：已完成档位按原搜索顺序回放，Agent 以下一代次重新准备并复用已登记设备，中断的那一档重新执行；最近 30 秒仍有心跳或已完成的运行会被拒绝。`compare` 读取多个运行的摘要，负载组合（报文、查询、模块与 SLO）一致且都有确认通过档时计算 E(n) = (C(n)/C(n₀)) ÷ (n/n₀) 并输出 `scaling.svg`；实例数默认取各运行的平台指标目标数，可用 `--instances id=n` 指定；条件不一致时只并列并写明原因。

当前边界：TCP（GB26875）只有协议 ACK 层证据；视频场景不覆盖 WebRTC；重复业务副作用未测量。即使已启用全部可选场景，`suite: full` 仍因这些覆盖缺口判为 inconclusive，不能报告全系统通过。核对查询可在临时 schema 中用 `IOT_TEST_POSTGRES_DSN=... go test ./internal/capacity -run PGStore` 验证。

### 业务模块场景

计划 `modules` 中启用的模块由 Agent 以固定背景速率（按分钟或秒）与设备负载同时执行，各自记录成功率与时延（`p95` 设为 0 表示只记录不判定），并进入覆盖表：

| 模块 | 执行内容 | 前提 |
| --- | --- | --- |
| `ai` | 对测试租户的活动告警发起手动研判并轮询完成；`maxRuns` 为整次运行的硬预算 | `mode: mock` 时平台 `IOT_AI_HARNESS_URL` 指向 `cmd/harness-mock`（只测平台调度，报告明确标注）；`real` 会消耗模型额度 |
| `knowledge` | 上传生成的 Markdown 文档到指定 `workflowId` 并等待入库响应 | 知识库与嵌入服务可用 |
| `video` | 建立并释放 HLS 播放会话；清单有 `web` 时拉取播放列表和首个分片近似首帧 | 测试摄像头在直播白名单内；WebRTC 未覆盖 |
| `backup` | 每档在测量窗口内执行一次备份、逐文件下载校验 SHA-256；`restore: true` 时恢复到独立库并核对条数 | 备份服务配置 `IOT_BACKUP_RESTORE_TARGET_DSN`（与业务库不同） |
| `realtime` | 按页面方式取 MQTT 令牌并订阅告警与设备状态推送，测送达时延 | 清单有 `mqtt`；订阅者分摊到各 Agent |
| `exports` | 原文批量下载、回放 DRY_RUN 任务、巡检并导出 PDF | 测试租户已有原文 |
| `openapi` | 用绑定用户的 API Key 查询开放接口 | `keySecretRef` 指向秘密文件中的密钥 |

`harness-mock` 用法：`IOT_AI_HARNESS_TOKEN=<平台同一令牌> go run ./cmd/harness-mock -listen :8091 -latency 2s -jitter 1s -concurrency 4`，超过并发返回 429，与真实网关一致。

### 故障注入（resilience）

故障命令只在 Agent 主机本地登记：`capacity-test agent --fault-allow faults.yaml`（进程内 Agent 用 `run --fault-allow`），文件权限须为 0600，格式见 `cmd/capacity-test/examples/faults.example.yaml`（动作名 → `inject`/`recover` 的 argv，不经 shell）。计划的 `faults.actions` 只能按名称引用，预检确认动作确实登记在对应 Agent；释放运行或控制器租约过期时 Agent 自动执行未完成的恢复。

`resilience` 档在测量窗口内按 `at`/`duration` 注入并恢复。常规 SLO 改为“仅记录”，完整性、排空和恢复时间决定结论：恢复时间从恢复命令完成起算，直到解析成功速率回到注入前基线的 90% 且积压回到基线附近，超过 `faults.maxRecovery` 判为失败。报告包含故障表与 `recovery.svg`。

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

运行结束、失败或取消后，可在该行点击 **清理数据**。确认框先显示专用设备数、共享设备数和保留范围；清理会删除本次报文及派生告警/状态、独占测试设备、设备缓存、复用凭据、报告、账本和运行工作目录，最后一次关联运行清理时还会删除可确认由容量模块自动创建的测试产品和规则。清理后该行消失，报告不能再次下载，所需报告应先下载保存。有测试正在运行时禁止清理；存储、远程 Agent 或文件清理失败时保留运行记录，可重试已完成的步骤。

清理使用 `DELETE /api/v1/ops/capacity/runs/:id` 操作权限，并要求当前账号拥有全租户设备范围；浏览器不能指定租户、设备或报文 ID。共享设备及其凭据供其他保留的运行使用，只按本次账本删除报文；已经改名为业务用途、被网关/摄像头/采集配置引用的设备会阻止删除。新运行的知识文档、AI 研判结果、巡检与回放任务会记录运行归属，完成后随该运行清理；正在处理的任务或报文须等待完成。系统审计、监控历史、消息队列按保留策略存储的历史记录、平台全量备份和独立恢复目标保留。旧运行无法证明归属的模块任务，以及 TCP 场景仅有 ACK、无法定位平台原文的数据会保留，并在确认框提示。部署需同时更新 API、Web 和容量测试模块；远程 Agent 也需更新，以支持运行工作目录清理。

### 单项工具

在隔离环境使用实际设备凭据与代表性报文。HTTP 202、MQTT PUBACK、TCP ACK 只代表各自接收阶段，不代表解析、存储与告警完成；持续增长的积压说明当前速率不可持续。

```bash
go run ./cmd/capacity-test -h
# 先建立测试产品，将登录令牌存入权限为 0600 的 token.txt
go run ./cmd/capacity-test -mode provision -token @token.txt -product <产品> -count 5000 -devices devices.json
go run ./cmd/capacity-test -mode ingest -token @token.txt -devices devices.json -rates 25,50,100,150 -step 60s -out result.json
go run ./cmd/capacity-test -mode canary -token @token.txt -devices devices.json
```

| 场景 | 工具模式与观测 |
| --- | --- |
| HTTP 持续/突发/故障恢复 | `ingest`，用 `-data '{"fault":true}'` 或 `-data @file.json` 添加真实业务字段；分别记录接收与归档、解析、存储吞吐，长时间确认积压平稳 |
| MQTT 连接及发布 | 先 `mqtttok`，再 `mqttconn` / `mqttpub`；后者默认等待应用归档确认并原文重试，`-mqtt-confirm=false` 才只测 PUBACK；令牌按有效期使用，记录平台会话 `dropped_msgs`；令牌签发不完整或实际发布连接少于 `-conn-step` 时工具报错退出 |
| GB26875 TCP / UDP | `tcp -network tcp` 或 `tcp -network udp`，并发发送方数量由 `-levels` 控制；结合协议 ACK 与下游入库 |
| 管理查询与并发 | `http -path <接口>`，记录 P95/P99 以及对上报链路的影响 |
| 端到端与资源 | `canary`、`sample`、`docker stats`；按消息 ID 核对原文、标准消息与告警 |

每份报告记录日期、提交、硬件、部署形态、设备数、协议/报文、连接池和并发、规则/AI 配置、速率/时长、错误与丢失、队列起止量、端到端延迟及恢复时间。设备凭据与令牌文件含秘密，测试后清理。`loadgen` 管理员链路和少量客户端不能代替真实设备认证或连接规模测试。

`http` 模式完整读取响应体后才计为成功，适用于 PDF / 原文 / 备份下载；HTTP 207 等业务部分成功响应仍须单独解析内容。结果中 `okQps` 为成功数除以含在途收尾的 `durationSec`，`p95ms` / `p99ms` 含失败请求耗时；`lagEnd` 为全部消费组积压之和，`storageLagEnd` 只是存储消费组。`metricsValid=false` 表示管道指标缺失或计数器重置，归档 / 解析吞吐应标为未知，不按零吞吐或负数解读。大规模 MQTT 连接测试可用 `-mqtt-local-ips` 轮换负载机已有的源地址；先排除临时端口与文件句柄耗尽，不能将负载机限制当作 Broker 上限。

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

存储死信恢复工具默认只读审计，显式指定租户、源业务主题和待恢复标准消息 ID 数组文件，核对全部 ID 后再追加 `-execute`：

```bash
go run ./cmd/dlq-replay -env-file .env.local -tenant <租户> -ids-file ids.json
# 核对后重新送入原存储消费链，不删除 DLQ、不重置消费者 offset
go run ./cmd/dlq-replay -env-file .env.local -tenant <租户> -ids-file ids.json -execute
```

重复死信按 messageId 合并，矛盾正文会拒绝整批发布。重新发布成功只代表 Kafka 收到；必须再核对 PostgreSQL `processed_at`、ClickHouse 行数/唯一 ID 和实际告警状态。发布途中失败可重跑同一 ID 列表，仍由原业务幂等处理。
