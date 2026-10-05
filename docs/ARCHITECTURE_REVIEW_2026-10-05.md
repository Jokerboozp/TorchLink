# 平台架构评审与改进方案（设备接入与 AI 方向）

- 日期：2026-10-05
- 依据：当前 `main` 分支源码静态阅读（`d87413ac`），未启动服务、未运行测试；所有判断均附文件位置，可直接核对。
- 范围：整体分层、设备接入与消息链路、规则与告警、AI / Harness / 知识库。前端只看了结构，不做 UI 评审。
- 定位：本文是评审结论与分阶段建议，不是执行授权。每项改动仍按 `AGENTS.md` 第 9 节单独立项、验证后合入。

## 1. 结论摘要

整体架构是健康的：端口 / 适配器分层清晰，消息链路具备"原文先归档 → 幂等预留 → 设备分区有序流 → 租约认领 → 版本校验写状态 → 事务 outbox"的完整保障，协议代码在无网络的 runner 容器隔离执行，AI 以 Harness 侧车 + 受控 MCP 只读工具 + 每次运行短期 JWT 的方式收口，权限在 MCP 回调时按实时权限重算。CI 用真实 PostgreSQL / ClickHouse / Redis 跑测试，410 个测试文件。这个底座不需要重做。

真正的问题集中在三类：

1. **几个"巨型对象"开始拖慢演进**：`ports.Repository` 约 150 个方法、`core.Engine` 约 110 个方法、`httpapi/server.go` 3251 行 221 条路由。每加一个存储能力要改 4 个实现（memory / postgres / clickhouse / redis 装饰器 / 设备范围装饰器），每个 AI 功能都挂在 Engine 上。
2. **设备数据没有"语义层"**：`Product.ThingModel`（属性 / 事件 / 命令定义）只用于模板准备和前端编辑，入库、规则、告警研判、巡检都不读它。结果是：解析结果不做范围 / 类型校验，规则只能写裸字段名，AI 研判拿到的属性历史是一份写死的 6 个字段清单，不知道单位、阈值和设备类型含义。
3. **AI 只有"执行"没有"闭环"**：没有 token / 耗时 / 费用记录，没有把 AI 研判结论与人工核实结果（真火 / 误报 / 测试 / 故障）对齐的统计，提示词以 Go 常量散落在各处，结构化输出不校验枚举。现在无法回答"AI 研判准不准、花多少钱、哪个版本更好"。

另外有若干具体性能与健壮性点（规则表达式每条消息重新编译、总览和巡检把整租户数据拉进内存、自动解析器按产品 ID 含 `json` 匹配、离线判定阈值写死 300/60 秒），都是局部改动可解决的。

最值得先做的五件事（按收益 / 成本排序）：

| 优先级 | 事项 | 收益 | 规模 |
|---|---|---|---|
| P0 | 规则表达式编译缓存；总览 / 巡检改用 SQL 聚合 | 直接降低每条消息 CPU 和大租户接口延迟 | 小 |
| P0 | AI 运行记录补齐 token / 耗时 / 工具调用数，并与人工核实结果关联 | AI 功能从"能用"变成"可评估、可控费" | 小到中 |
| P1 | 让物模型进入链路：解析后校验 → 规则字段选择 → AI 上下文携带单位 / 阈值 / 类型 | 数据质量与 AI 研判质量的最大来源 | 中 |
| P1 | 告警研判上下文扩展：位置、同单位近期告警、触发规则、相似告警的人工处置结论、关联摄像头 | 研判从"看一台设备"变成"看一个现场" | 中 |
| P2 | 按业务域拆分 Repository 与 Engine；HTTP 路由按模块注册 | 长期可维护性，和前四项不冲突，可渐进 | 大、可分批 |

## 2. 现状架构（简要）

```
设备 ──MQTT/HTTP/TCP/UDP/Modbus──▶ 接入(gateway/access)
   onboarding.PrepareStandard / protocolruntime.Listeners / protocolruntime(Modbus 主动采集)
        │ engine.IngestRaw：Normalize → 绑定协议 → ReserveRawMessage(幂等) → RawStore.PutRaw(PG/CH) → SaveRawIndex → Bus(iot.raw.message)
        ▼
   parser 角色 handleRaw：协议发布版 → 产品协议包 → 自动匹配  ⇒ StandardMessage → Bus(iot.device.business, 设备分区) + 对外主题 + MQTT 实时
        ▼
   processor 角色 handleStandard：ClaimStandardMessage(租约) → 规则匹配 / 部件告警 / 设备直报告警 → CompleteStandardMessage(版本校验+处理标记)
        ▼
   outbox → iot.alarm.* / iot.device.state → 通知(notify)、消息主题分发(messagetopics)、前端实时
   jobs 角色：离线扫描、重试、留存、备份清理、外部数据拉取 等集群单例

AI：httpapi → core.runBusinessWorkflow → 签发 MCP JWT(租户/用户/run/工具范围) → HarnessPool(rendezvous) → Node 侧车(dsh) → DeepSeek
     侧车经回环代理回调 /mcp/harness（mcpserver，6 个只读查询工具 + 规则草稿保存），每次调用重查权限与设备范围
知识库：PG + pgvector，向量 + 关键词 + 重排，按租户 / Agent 绑定过滤；候选索引重建后原子激活
```

进程角色 `combined / api / gateway / parser / processor / jobs / protocol-runner`（`internal/config/roles.go`），同一二进制按角色装配（`internal/platformapp/app.go`）。

## 3. 问题清单与证据

### 3.1 整体分层

**A1. `ports.Repository` 是单一巨型接口。** `internal/ports/ports.go:50-245` 把设备、协议、原文、标准消息、状态、规则、告警、视频、AI、知识、回放、审计全部放在一个接口里，并内嵌 `AccessStore / MessageTopicStore / FireSafetyStore / SiteStore / ...`。影响：memory、postgres、clickhouse 包装、redis 装饰器、`deviceScopeRepository` 装饰器都要实现或转发全部方法；新增一个查询要改多处；测试替身只能整体伪造。仓库里已经出现了按域拆出的子接口（`AccessStore`、`VideoStore`、`KnowledgeDocumentJobs` 等），方向是对的，但主体还没拆。

**A2. `core.Engine` 承担过多职责。** `internal/core/` 下 `Engine` 方法约 110 个，涵盖接入、解析、规则、告警、状态、回放、视频媒体、外部数据、AI 研判、巡检、报告、协议助手、知识重建、对象清理。`Engine` 结构体同时持有 `AI / AIPlugins / AIWorkflows / HarnessTokens / KB / KnowledgeReindex / MessageTopics / Locator`（`engine.go:27-63`）。任何一个 AI 功能的改动都在同一个包、同一个类型上进行，编译与测试边界模糊。

**A3. HTTP 层集中注册。** `internal/httpapi/server.go` 3251 行，221 条路由分散在 90 个文件但由同一个 `Server` 挂载；`Server` 上通过 `SetXxx` 注入十余个运行时依赖。可读，但新模块只能继续往上堆。

**A4. 设备范围过滤在内存中做。** `internal/httpapi/device_scope.go:155-195`：`deviceScopeRepository` 先取整租户 `ListManagedDevices`，再逐条 `deviceAllowed` 过滤，分页用 `pageSlice` 在过滤后切片。租户设备数上万时，普通用户的每次列表请求都是 O(N)。同时过滤依赖请求上下文中是否带 scope，后台路径天然绕过（文档已说明，是设计选择，但属于"易漏"的安全边界）。

**A5. JSONB 文档式存储 + 少量提升列。** `schema.sql` 50 张表中 31 张为 `body jsonb` 加几列索引字段（如 `alarm_record` 提升了 status / level / source / last_triggered_at）。灵活，但凡未提升的字段既不能高效过滤也不能聚合，于是出现了下面 D3 的"全量拉到内存统计"。

### 3.2 设备接入与消息链路

**D1. 规则表达式每条消息重新编译。** `internal/core/rules.go:51-85`：`evaluateGengine` 每次都 `NewDataContext → bindExpressionFields → BuildRuleFromString → Execute`，`rules.go` / `rule_cache.go` 中没有编译缓存（已 grep）。`handleStandard` 对每条消息遍历租户全部规则（`engine.go:419-453`，有 `ruleCovers` 先按产品裁剪）。规则数 × 消息量一大，processor CPU 会被 gengine 编译占满。条件式规则（`Conditions`）不受影响。

**D2. 自动解析器匹配过于宽松。** `internal/parser/parser.go:JSONParser.Match`：`PayloadFormat == json || Protocol == json || ProductID 含 "json"`。`handleRaw` 在没有协议绑定、没有产品协议包时会落到 `Parsers.Parse(raw)` 自动匹配（`engine.go:316-318`）。一个漏配协议的产品会被"通用 JSON 解析器"静默解析成属性上报，而不是记录为解析失败。既然平台已经走"显式绑定 + 发布"路线，自动匹配应只保留标准报文（`StandardParser`），其余一律要求显式绑定。

**D3. 大租户路径把整租户数据拉进内存。**
- `internal/mcpserver/server.go:buildSystemOverview`：`ListProducts / ListProtocolPackages / ListManagedDevices / ListDeviceStates / ListRules / ListAlarms(Limit 10000) / ListVideoCameraMappings / ListKnowledgeDocs` 全部不分页取回再在 Go 里计数；`alarms.truncated` 字段本身就承认了上限。
- `internal/core/health_inspection.go:InspectDeviceHealth`：`ListManagedDevices + ListDeviceStates + ListAlarms(ACTIVE, 10000)` 全量。
- 而 `ports.AlarmReportStore`（`AlarmDispositionStats / AlarmBreakdown / EachAlarm`）与 `DashboardCounts` 已经是 SQL 聚合 / 流式接口，只是这些路径没有用。

**D4. 物模型没有进入运行链路。** `Product.ThingModel`（`model/device_operations.go:16`）只被 `httpapi/template_preparation.go`、`onboarding/operations.go`、`onboarding/template_preparation.go` 和两个前端组件引用。`handleRaw` 解析后仅做 `MessageComponents` 校验，不校验属性是否在物模型内、类型 / 范围是否合法；规则编辑只能手填字段名；AI 不知道字段含义。这是设备数据质量与 AI 质量的共同上游。

**D5. 离线判定阈值写死。** `engine.go:532` 与 `connections.go:13` 在首次建状态时固定 `ReportIntervalSec: 300, OfflineToleranceSec: 60`；在 `httpapi` / `onboarding` 中未找到产品级或设备级配置入口（grep `ReportIntervalSec` 仅命中 core 与 rawstore）。心跳 10 秒的烟感和 1 小时上报一次的水压表用同一套 6 分钟判定，前者离线发现太慢，后者会被误判。`rawstore` 又用这个值决定 PG / ClickHouse 路由（`rawstore/store.go:150`），所以它实际上是"设备上报频率"而不只是离线参数，更应该来自产品 / 模板。

**D6. TCP 命令依赖本地会话。** `protocolruntime/listeners.go:758`：`Command` 只在本进程 `r.hosts` 中找会话；跨副本依赖 `httpapi/execution_route.go` 按 PostgreSQL 里登记的执行端点做反向代理，前提是 `AccessCoordination` 开启。单副本或未开启协调时，命令请求打到非持有者会得到"protocol listener is not running"。这是已知边界（`AGENTS.md` 第 6 节），建议在 API 层把"未开启协调且本机无会话"的错误改成明确的可操作提示，并在文档中把两种部署形态的命令路由写清楚。

**D7. 本地事件总线是同步内存实现。** `adapters/local/bus.go`：无 Kafka 时 `Publish` 同步调用所有订阅者，一个订阅者出错整个发布失败，无重试与 DLQ。`scripts/setup-local.sh` 生成的 `.env.local` 已配置 Kafka，所以本地标准流程不走这条路径；只有未设置 `IOT_KAFKA_BROKERS` 的精简运行和单元测试受影响。建议在 `docs/DEVELOPMENT.md` 明示：无 Kafka 时复现的"消费失败"行为与线上不同。

### 3.3 AI、Harness 与知识库

**I1. 没有运行级成本与质量记录。** `ports.AIWorkflowEvent`（`ports.go:421`）没有 usage 字段，`deploy/deepseek-harness/gateway.mjs` 也未透传 token 用量（grep `usage / promptTokens` 无结果）。指标只有 `ai_analysis_success/failed/timeout_total` 三个计数（`metrics.go:27`）。`AIToolCallLog` 记录工具调用，但没有一条"运行记录"把 workflow、prompt 版本、输入字节、输出字节、耗时、工具调用次数、模型、结果状态串起来。这意味着：DeepSeek 费用不可归因、慢在模型还是慢在工具不可区分、换模型前后无对比。

**I2. 研判结论与人工核实没有对齐。** `model.AIAnalysis`（`model.go:764`）有 `RiskLevel / Confidence / PromptVersion`，`model.AlarmDisposition`（`model.go:681`）有人工核实结果（真火 / 误报 / 测试 / 维护 / 故障），两者在 `httpapi/alarm_disposition.go` 中没有任何交叉引用。平台其实已经持有评估 AI 准确率所需的全部数据，只差一张关联表和一个统计查询。

**I3. 研判上下文偏窄。** `core/engine.go:1194-1240` 组装的上下文只有：设备元数据、固定 6 个属性的 24 小时历史（`alarm_history.go:15`：temperature / smoke / water_pressure / voltage / current / gas）、同设备同类型近 20 条告警、知识库片段。缺少平台已有的高价值信息：
- 告警位置快照（`AlarmLocation`，单位 / 建筑 / 楼层）和同一建筑 / 楼层近期其它告警（判断是否多点联动，这是火警最关键的佐证）；
- 触发的规则定义（`rule_id` 有了，规则正文没给）；
- 相似告警当时的人工处置结论（I2 的数据，等于告诉模型"这台设备历史上 8 次中 7 次是误报"）；
- 关联摄像头是否存在、有无视频告警事件（`VideoAlarmEvent`）；
- 物模型中该属性的单位 / 正常范围（D4）。
知识检索问题也固定为 `AlarmType + DeviceType + "处置 SOP 维修"`（`alarm_analysis_knowledge.go:48`），没有带上现场症状。

**I4. 结构化输出校验不完整。** `aioutput.DecodeAlarmAnalysis` 只要求 `summary` 非空并归一化 `confidence`，不校验 `riskLevel` 是否属于 `CRITICAL|HIGH|MEDIUM|LOW|INFO`，也不校验数组长度上限；`ExtractJSON` 取首个 `{` 到最后一个 `}`，模型若在 JSON 前后输出带花括号的文字会解析错位。`AGENTS.md` 第 8 节"AI 风险等级兼容大小写"正是这个口子的下游补丁。

**I5. 提示词散落且版本号不一致。** 提示词为 Go 常量嵌在 `ai_business.go / health_inspection.go / ops.go / protocol_assistant.go`，`PromptVersion` 在 `DecodeAlarmAnalysis` 写成 `alarm-diagnosis-v1` 后又被 `runAlarmAnalysisWorkflow` 覆盖为 `harness-alarm-analysis-v1`。没有集中管理，也没有针对提示词的回归样例（`internal/aitest` 仅 75 行）。

**I6. MCP 工具返回体偏重。** `mcpserver/server.go:query_alarm_list` 默认 100 条且返回完整 `Alarm`（含 `Details` 原始遥测与 `Cameras`），`ports.AlarmFilter.Summary` 投影存在但工具没用；`query_system_overview` 一次返回全租户统计。对 DeepSeek 的上下文窗口和费用都是浪费，也更容易触发 32 KiB 请求体上限。

**I7. 知识切片不感知文档结构。** `core/knowledge_document.go:ChunkKnowledgeTextDetailed` 固定长度 + 200 字重叠；消防设备手册大量是表格（故障码表、参数表）和层级标题，固定切片会把一行故障码与它的说明切开。检索本身（向量 + 关键词 + 重排，`adapters/knowledge/postgres.go:576-621`）是合理的。

**I8. 平台层缺少"设备健康信号"的确定性计算。** 巡检（`InspectDeviceHealth`）的确定性部分只看离线 / 未上报 / 活动告警数。卡值（传感器长期不变）、上报周期漂移、属性超出物模型范围、同型号设备离群等都能用 SQL / ClickHouse 统计得到，不需要模型；它们既可直接生成告警，也可作为 I3 的上下文。目前这些信号不存在，所以 AI 只能对"已经响了的告警"做事后解释。

**I9. 其它小项。**
- `authorizeAIRun`（`httpapi/ai_access.go:52`）用 `(&http.Request{}).WithContext(ctx)` 伪造请求去复用 `managedIdentity`，能工作但耦合了 HTTP 形态，拆 Engine 时会碍事。
- Harness 默认并发 2、驻留会话 4（`compose.yaml:225`），业务运行等槽位最多 2 分钟；巡检单次 prompt 为整租户快照，设备上万时仍会撞 30 KiB 输入上限。应提前按设备数做分片或只送异常项。
- 研判仅手动触发是产品决策，不是缺陷；但一旦 I1 / I2 到位，可以给"高等级告警自动研判并有预算上限"留接口。

## 4. 改进方案（分阶段）

每一阶段都可独立合入，不要求前置阶段全部完成；阶段内条目按建议顺序排列。

### 阶段 0：低风险、立刻见效（约 1–2 周）

**0.1 规则表达式编译缓存。**
- 改动：`core/rules.go` 为 `(tenant, ruleID, version, expression)` 建编译结果缓存（`RuleBuilder` 可复用，`DataContext` 每次新建绑定消息），与现有 `ruleCache` 同生命周期，`RulesChanged` 时失效。
- 验收：现有 `rules_test.go` 通过；新增并发测试证明同一规则 1 万条消息只编译一次；`capacity` 场景 processor CPU 下降可量化。

**0.2 总览 / 巡检改用聚合接口。**
- 改动：`buildSystemOverview` 的告警部分改用 `AlarmBreakdown / AlarmDispositionStats`；设备 / 状态计数新增 `CountDeviceStatesByStatus`（SQL `GROUP BY business_status`，`device_state` 已有索引）；`InspectDeviceHealth` 改为流式 `EachAlarm` + 分页设备读取，先算汇总，再只对"异常项"组装 AI 快照。
- 验收：`mcpserver/server_test.go`、`httpapi/ai_test.go` 通过；对 1 万设备 / 10 万告警的租户，总览接口内存不随规模增长。

**0.3 AI 运行记录。**
- 改动：新增 `ai_workflow_run` 表与 `ports.AIRunStore`（独立小接口，不并入 `Repository`）：`run_id / tenant / actor / workflow / prompt_version / model / input_bytes / output_bytes / prompt_tokens / completion_tokens / tool_calls / duration_ms / status / error`。`gateway.mjs` 在 `run.completed` 事件透传 provider 返回的 usage；`HarnessClient.StreamChat` 解析并回填；`runBusinessWorkflow` 与聊天路径统一写入。指标增加 `ai_run_total{workflow,status}`、`ai_run_duration_seconds`、`ai_tokens_total{workflow,kind}`。
- 验收：模型管理页"运行中的 AI 工作流"可追溯历史运行与用量；Grafana 可按 workflow 看费用趋势。

**0.4 结构化输出校验收紧。**
- 改动：`aioutput` 增加 `riskLevel` 枚举校验（大小写归一后不合法即失败，失败走现有 fallback 分支）、数组长度上限、`ExtractJSON` 改为从第一个 `{` 起做括号配对而不是取最后一个 `}`。
- 验收：`aioutput_test.go` 补三类坏输出样例。

**0.5 工具返回瘦身。**
- 改动：`query_alarm_list / query_similar_alarms` 使用 `Summary: true` 投影并默认 20 条；返回体附 `total` 与 `nextOffset`，模型需要细节时再按 `alarmId` 取单条（新增 `query_alarm_detail` 工具，仍走设备范围校验）。
- 验收：Harness 插件白名单 `READ_ONLY_TOOL_CEILING`、Manifest、`auth.HarnessReadScopes` 三处同步；`mcpserver` 测试覆盖范围校验。

### 阶段 1：数据语义与研判质量（约 3–5 周）

**1.1 物模型进入链路。**
- 改动：
  - `handleRaw` 解析成功后按产品物模型做"软校验"：未知属性、类型不符、越界值记入 `StandardMessage.Tags["quality"]` 与 `parse_quality_total{reason}` 指标，不拒收（先观察，再决定是否拒收）；
  - 规则编辑接口返回产品物模型字段供前端选择；`ValidateRuleDraft` 校验字段存在；
  - `alarmHistoryProperties` 改为从物模型取"数值型属性"，固定清单仅作无物模型时的兜底；
  - AI 上下文携带属性的单位、正常范围、告警阈值。
- 验收：物模型缺失的产品行为不变；有物模型的产品在回归样例中能标出越界值；规则草稿引用不存在字段时被拒。

**1.2 研判上下文扩展（I3）。**
- 改动：新增 `core/alarm_context.go` 统一组装（替代 `AnalyzeAlarm` 内联逻辑）：位置快照、同建筑 / 楼层 2 小时内其它活动告警（经 `sites` 包）、触发规则定义、相似告警的人工处置结论分布、关联摄像头与视频告警事件、物模型语义。每一块都有字节预算，总量受 30 KiB 约束。知识检索问题改为"告警类型 + 设备类型 + 关键症状（如温度 85℃ 持续上升）"。
- 验收：`ai_test.go` 用固定快照断言上下文结构；在普通用户设备范围下，同建筑告警只包含其有权设备。

**1.3 研判与人工核实对齐（I2）。**
- 改动：`AlarmDisposition` 增加 `aiAnalysisId / aiRiskLevel`（核实时快照当时的研判）；新增 `AIAnalysisStats(tenant, promptVersion, range)` 聚合：按 AI 风险等级 × 人工结果的混淆矩阵、采纳率（人工结果与建议一致）。模型管理页新增"研判质量"卡片。
- 验收：混淆矩阵在 `repository_test.go` 用真实 PostgreSQL 验证；普通用户只看到自己设备范围内的统计。

**1.4 设备健康信号（I8）。**
- 改动：jobs 角色新增集群单例 `device-signals`，每 N 分钟按租户计算：卡值（最近 K 次上报某属性方差为 0）、上报周期漂移（实际间隔与产品期望偏离）、越界比例、同产品离群（z-score）。结果写 `device_signal` 表（设备、信号类型、强度、窗口、依据），可选产生 `DEVICE_HEALTH` 类告警（由规则或默认阈值控制，默认只记录不告警）。巡检与研判读取该表。
- 验收：ClickHouse 可用时用其聚合，否则 PostgreSQL；容量测试档位下任务耗时有上限。

**1.5 离线参数来源（D5）。**
- 改动：`Product` 增加 `reportIntervalSec / offlineToleranceSec`（物模型旁的运行参数），设备级可覆盖；状态首次建立时从产品取值，`rawstore` 路由同步使用；提供一次性回填脚本更新已有 `device_state.body`。
- 验收：`engine_test.go` 覆盖产品级参数生效；旧设备无配置时保持 300 / 60。

**1.6 提示词集中与回归（I5）。**
- 改动：`internal/aiprompt` 包集中各工作流提示词，常量化 `PromptVersion`，修正 `DecodeAlarmAnalysis` 与 `runAlarmAnalysisWorkflow` 的版本不一致；`internal/aitest` 增加"固定上下文 → 期望输出结构"的样例集，可用 `harness-mock` 离线跑。
- 验收：CI 不调用真实模型即可校验提示词变更没有破坏输出契约。

### 阶段 2：结构性拆分（持续、可分批）

**2.1 `Repository` 按域拆分。** 以现有 `AccessStore / FireSafetyStore / SiteStore` 的做法为模板，从 `ports.Repository` 中依次拆出 `DeviceStore`、`ProtocolStore`、`RawStore`、`AlarmStore`、`RuleStore`、`AIStore`、`KnowledgeStore`、`VideoStore`；`Repository` 变为这些接口的组合，调用方逐步改为依赖最小接口。memory 实现保留组合，redis / scope 装饰器只包装它们真正拦截的域。每拆一个域跑一次全量 `go test ./cmd/... ./internal/...`。

**2.2 `Engine` 拆为编排器 + 领域服务。** 先把 AI 相关（`AnalyzeAlarm / InspectDeviceHealth / GenerateReport / DraftRule / GenerateProtocolAssistant / runBusinessWorkflow`）迁到 `internal/aiworkflow` 包，`Engine` 只保留消息链路、规则、告警、状态。`AuthorizeAIRun` 改为接口而不是函数字段，去掉 I9 的伪造请求。

**2.3 设备范围下推到 SQL。** `deviceScopeRepository` 的 `List*` 改为把 `DeviceIDs` 传给仓储（`ports.DeviceFilter / AlarmFilter.DeviceIDs` 已存在），分页与计数在数据库完成；内存过滤只保留给没有过滤参数的少数接口。

**2.4 HTTP 路由模块化。** 按现有文件边界引入 `type module interface{ Routes(*router) }`，`server.go` 只做中间件与装配。不改路径与契约。

**2.5 自动解析器收口（D2）。** `NewPlatformRegistry` 的 `automatic` 列表只保留 `StandardParser`；其它解析器必须由协议发布版或产品协议包显式指定。配合迁移检查：列出当前依赖自动匹配的产品并提示绑定。此项改变行为，需单独评审与公告。

## 5. 不建议做的事

- 不引入独立向量数据库、GPU 推理、设备影子或孪生拓扑；`AGENTS.md` 已明确移除，且本次评审没有发现必须依赖它们才能解决的问题。
- 不把 Harness 换成 Go 内嵌 agent 循环。侧车的隔离（无 shell / 文件系统、工具白名单、独立进程可强停）是当前 AI 安全边界的主要来源，重写收益不足以抵消风险。
- 不为了"实时 AI"订阅告警事件自动研判。先完成阶段 0 的成本记录与阶段 1 的质量对齐，再按预算决定是否开放自动触发。
- 不一次性重写 `Repository` 或 `Engine`。阶段 2 必须逐域、逐包、每步可回滚。

## 6. 未验证与假设

- 以上性能判断（D1、D3、A4）基于代码路径分析，未在本次用 `cmd/capacity-test` 实测；建议阶段 0 开工前先跑一档容量基线，便于量化收益。
- 未检查前端各页面对本方案中接口返回体变化（如 0.5 的工具返回、1.3 的核实字段）的适配量。
- 未核对 `deploy/deepseek-harness` 上游 `dsh` 版本是否已在事件中暴露 usage；若无，0.3 需要在网关侧从 provider 响应中读取。
- Harness 当前 `maxTokens` 上限与 DeepSeek 实际模型（`deepseek-flash`）的上下文窗口未逐项核对，1.2 的字节预算需以实际模型为准。

## 7. 实施记录（2026-10-05）

各条目已按阶段实现并推送到 `main`，验证环境为 OrbStack develop 虚拟机中的一次性 PostgreSQL 17 + pgvector 0.8.1、ClickHouse 25.7、Redis 7.4 容器（Mac 上执行 `go test`），未部署平台服务、未调用真实模型、未做浏览器与容量实测。

| 条目 | 提交 | 与方案的差异 |
| --- | --- | --- |
| 0.1 规则编译缓存 | `ce542693` | 规则 `version` 保存时不递增，改以表达式文本为缓存键；共享 gengine 规则树，每条消息新建 DataContext |
| 0.2 总览 / 巡检聚合 | `b71cc4d7` | 以 `DeviceOverviewCounts` / `AlarmOverviewCounts` 两个聚合代替单独的 `CountDeviceStatesByStatus` |
| 0.3 AI 运行记录 | `a61136b6`、`83c235dc` | Harness 上游已在 `assistant/message` 事件中提供 usage，网关汇总后随终止事件返回；运行记录接口按受保护读取纳入权限目录 |
| 0.4 输出校验 | `e9cce3c2` | — |
| 0.5 MCP 工具瘦身 | `b788f932` | `query_alarm_detail` 只加入 alarm-handler 的内置工具清单，运维助手不变 |
| 1.1 物模型进入链路 | `c54f5dc4` | 新增有效范围与高低阈值字段；越界只标记不拒收 |
| 1.2 研判上下文 | `20a6a2bd` | 视频事件取自告警明细，不新增按摄像头查询；提示词版本升为 `alarm-analysis-v2` |
| 1.3 研判与核实对齐 | `0e23ec46` | 一致率不计中风险，另给出漏判真实火警数 |
| 1.4 设备健康信号 | `cb4596ef` | 迁移编号 0016；同型号离群用中位数 / MAD 修正 z 分数；配置 ClickHouse 时数值属性必须从 ClickHouse 统计（PostgreSQL 此时不存遥测属性） |
| 1.5 离线参数 | `4909f9f8`、`9291aa04`、`9cd76cc3` | 未写一次性回填脚本：配置变更时由 `ApplyDeviceTiming` 回填已有状态，未配置的旧设备保持 300 / 60 |
| 1.6 提示词集中 | `dfeb998a` | 正文逐字迁移；同时修正 harness-mock 的工作流 ID 与字段名 |
| 2.1 Repository 拆分 | `5849f395` | Redis / ClickHouse / 设备范围装饰器仍嵌入完整 Repository（方法集不变），未按域收窄 |
| 2.2 Engine 拆分 | `8128223c` | 业务工作流逻辑迁入 `internal/aiworkflow`；Harness 连接相关字段（`AIWorkflows`、`HarnessTokens`、`AIRuns`、`BusinessRunTimeout`、`KB`）仍挂在 Engine 上供聊天与工作流共用；巡检 PDF 与月报绘制留在 core |
| 2.3 设备范围下推 | `abc13e6b` | 评审时多数列表已下推；本次补齐子设备分页、状态计数与整表设备列表，摄像头映射仍在内存过滤 |
| 2.4 路由模块化 | `abd0ad04` | 以 `routeModules` 有序清单代替接口；路由快照 412 条前后一致 |
| 2.5 自动解析器收口 | `72d45c0c` | 行为变化：未绑定协议的模板不再自动按 JSON 解析 |
| D6 / D7 / I9 | `16ee859b` | — |

评审正文的若干事实在实施时有出入：`Repository` 实际 162 个方法、`Engine` 92 个方法；设备范围装饰器的告警、原文、设备分页当时已下推到 SQL；D6 的英文错误位于 `protocolruntime/listeners.go`，不在 `execution_route.go`。第 6 节建议的容量基线未执行，性能收益仍待 `cmd/capacity-test` 实测。
