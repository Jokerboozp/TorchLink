**反复报警核查与治理详细开发方案**

设计日期：2026-10-01。方案版本：1.0.0。产品范围依据[通用业务模板](RECURRING_ALARM_GOVERNANCE_TEMPLATE.md)，面向后端、前端、测试和现场试点人员。

本方案交付一个可配置、跨班持续跟进的治理模块：发现反复问题，记录每次核实，调查原因，实施改善，再用固定口径观察复发。餐饮厨房是首个现场试点，其他场所使用同一机制和不同预设。下文新增对象、API、工作流和行为均为拟实施契约；源码现状经本次只读核对，未执行业务功能验收。

**一 交付范围与基本约定**

完整交付包括通用模板与场景管理、报警类型配置、反复问题汇总、独立治理事项、快速核实、实际活动记录、成因评审、改善和验收、观察分析、Harness解读、版本报告、附件及跨班关联。

生产报警继续按现有链路展示、通知和处置。治理服务不调用生产告警状态修改、设备控制、规则发布或报文REINGEST；场所和历史原因不能自动决定下一次报警性质。复杂聚合、周期投影、报告与模型调用均在后台执行。

确定性服务负责身份、时间、周期、区间、指标、版本和权限；AI组织证据及候选解释；受权人员确认现场结论、原因和措施。事实查询或模型失败保留未知、部分覆盖和错误，人工记录仍可使用。

默认一个事项对应一个明确点位、一个原始报警类型和一种来源语义。点位可以是设备或设备下的部件；多个点位可以关联同一现场活动，各自保留结论。首期不建设跨点位共享结论的事项，也不把不同报警类型合成风险分或误报排行榜。

使用现有Go、Vue、PostgreSQL、ClickHouse、MinIO和Harness，不新增模型管理入口、本地模型、GPU或独立向量数据库。完全断网时表单、计算、人工评审和模板报告继续可用；AI及需要外部Embedding的新知识索引显示真实不可用状态。

**二 当前源码依据与复用落点**

| 当前入口 | 本次源码可确认的能力 | 本模块需要补齐的内容 |
| --- | --- | --- |
| [告警仓储](../internal/adapters/postgres/repository.go) | ACTIVE/ACKED按tenant、device、rule聚合；普通重复上报更新计数和时间，旧TriggerID/Details未全部更新 | 原事务保存本次incoming身份与证据；聚合Alarm不能充当物理周期 |
| [部件状态契约](../internal/model/component.go)、[部件事务](../internal/adapters/postgres/component.go) | 明确布尔状态、时间水位、重复与旧时间处理；组件状态和告警同事务提交 | 历次状态观察、明确CLEAR及接纳结果；保留原有生产语义 |
| [值班业务事件](../internal/model/duty.go)、[事务事件实现](../internal/adapters/postgres/duty.go) | 持久记录告警产生、上报、确认、恢复和关闭 | 普通上报事件正文是聚合快照，不能当完整逐次样本；核实与活动仍需独立对象 |
| [标准消息仓储](../internal/adapters/postgres/repository.go) | 成功解析消息及rawMessageId关联；有处理与存储确认信息 | 只读归一化信号与来源质量，不重放到生产 |
| [分析模型](../internal/model/analytics.go)、[分析服务](../internal/analytics/service.go) | 持久任务、租约、分批提交、快照、证据、输出和版本 | 注册新Kind、菜单、Prefix、processor及治理事实读取 |
| [分析AI服务](../internal/analytics/ai.go)、[核心调用](../internal/core/analytics_ai.go)、[MCP](../internal/mcpserver/analytics.go) | 有受控快照和AI revision；实际AI路径存在数据质量专用白名单 | 按工作流注册治理schema、事实类别、身份及权限映射；单放一个Manifest不能完成接入 |
| [值班记录](../internal/duty/run.go)、[附件接口](../internal/httpapi/duty_export.go) | 记录及附件绑定有效班次、人员和设备范围 | 治理主档及附件独立于ACTIVE班次；值班仅作跟进关联 |
| [数据质量附件](../internal/analytics/dataquality/read.go)、[视频接口](../internal/httpapi/video.go) | 服务端存储键、权限下载及失败补偿有可参考实现；直播单独授权 | 治理附件独立资源；历史影像新增受控读适配，不把URL或相机时间关联当原因 |

当前分析模块的菜单名称和规划端口，不代表全部应用已注册或已验收。实施前复核工作区中相关在途修改的最终契约，沿用已完成实现，不覆盖其他任务代码。维修和规则实验仅定义可选适配，不将既有设计文档当可调用接口。

**三 页面与完整用户路径**

新增菜单“反复报警治理”，放在消防业务管理区域。报警详情及设备、部件详情提供“填写核实”“查看治理事项”入口，保持原告警操作的目的与权限。

| 页面或组件 | 主要内容 | 必须处理的状态 |
| --- | --- | --- |
| 问题汇总 | 反复进入报警、重复上报待核实、持续未解除、改善后复发；场所、点位、类型、数量和覆盖 | 边界不明、历史不足、无设备权限、分页 |
| 治理事项列表 | 负责人、阶段、当前轮次、待核实记录、到期措施、新事件提示 | 完成后待核实、失去权限、关联模块不可用 |
| 事项详情 | 报警证据、现场核实、活动、原因、措施、观察、版本历史 | 新资料未重算、争议、来源过期、正在编辑 |
| 快速核实弹窗 | 看到什么、当时活动、关联设施状态；实际时间与方式紧凑呈现 | 未到场、电话核实、补登记、未知、重复提交 |
| 活动记录页 | 实际活动开始/结束、无报警活动、观察登记覆盖 | 计划未确认、结束未知、时钟不确定 |
| 模板设置 | 通用模板、场景预设、类型配置、验证与发布 | 内置只读、草稿、版本冲突、影响范围不足 |
| 观察报告 | 前后指标、准确分母、排除区间、条件变化和正式确认 | 资料不足、不可比、固定版本、AI失败 |

现场三项字段无默认“正常”。核实方式为现场、电话、资料核查、未核实；实际核实时间与登记时间分开。选择未知可以保存，但不自动满足完整核实或原因确认的要求。类型配置要求的检查范围及证据按需展开。

问题发现采用确定性DiscoveryPolicy，不由模型筛选。配置包含window、minReportCount、minKnownCycleStarts、minOpenDuration、postCompletionWindow、活动事项合并键及版本；各项独立启用，类型不支持可靠周期时禁用相应周期条件。阈值由现场负责人按上报机制和工作负担设置，页面显示触发口径；例如“最近7天出现3个可靠新开始”只能作为试点配置示例，不是通用消防标准。已完成人工核实和仍未核实的成员分别列出，生产告警性质不随筛选变化。

候选由已完成分析快照提供，保存point/signal、命中条件、成员、窗口、覆盖及policyVersion。同对象命中多个条件合并一张候选卡；候选不会自动建事项或确认成因。已有活动事项提供追加关联入口，已完成事项提供复核/重开入口；用户也可不经候选从具体报警手工建立事项。纯重复上报只提示上报问题，不冒充重复发生的信号周期。

负责人建立事项并指派人员，现场人员追加记录，负责人集中调查和安排措施；措施实施、必要专业验收后进入观察，确认固定观察版本。新事件只产生待核实提示，人工决定关联及复发；不会复制上次原因。

核实可以先于事项独立保存：按单一授权点位/信号和具体observationIds登记，冻结当时使用的表单配置。未建事项、旧轮已完成或记录人没有建事项/重开权限时，均可在自己的记录权限内完成现场登记。负责人之后追加“核实与轮次关联”，不改原记录，不自动建事项、重开或追加到已完成旧轮；事项关联也不能扩大原核实事件范围。

实际活动独立于报警登记。“开始活动/结束活动”用于快捷记录；复制计划或昨日记录只生成待确认草稿。餐饮试点可登记实际午晚餐作业区间，活动单位和划分方式固定，不能把持续作业随意拆成大量次数。

前端复用当前App页面注册、Naive UI、Tailwind CSS和Lucide组件，不更换技术栈。桌面使用分页表格和时间线，390px窄屏使用卡片、抽屉和独立正文滚动。刷新恢复持久任务进度，AI更新不覆盖人员未提交输入。

**四 模板与报警类型配置**

采用三份独立的不可变发布版本：GovernanceTemplateRevision、ScenePresetRevision、AlarmTypeProfileRevision。状态为DRAFT、PUBLISHED、RETIRED；内置模板和首批预设只读，租户复制后自定义。

| 配置 | 必要字段 |
| --- | --- |
| 通用模板 | templateId、revisionId、version、status、schemaVersion、受保护字段、字段ID、控件、验证规则、hash、作者与发布时间 |
| 场景预设 | presetId、version、适用类型、活动选项、环境选项、设施问题、补充字段和资料要求 |
| 报警类型配置 | profileId、version、productId、原始alarmType、originKind、signalKey、cycleMethod、字段含义、时间依据、恢复与状态语义、必要证据、观察准入、资料依据、适用设备范围 |

首批预设为餐饮厨房、高湿与洗衣、施工装修、车库车辆、机房配电、泵房水系统、通用场所。预设提供取证选项，不预填原因；厨房内部与相邻餐区、走廊分别建档。原始报警代码沿用当前产品，展示分类不能修改代码或合并不同信号。

字段控件限文字、单选、多选、时间、有限数值和附件。字段ID稳定，字段选项用稳定代码。禁止脚本、SQL、任意表达式、文件访问、外部抓取URL或自动业务动作。源字段绑定只引用成功解析的已确认字段路径和有限枚举，不让模型编写可执行判断。

发布验证包括结构、ID唯一性、保护字段、未知和争议选项、适用类型、资料依据、源字段类型和影响范围。租户级或产品共享配置须具备管理权限并覆盖全部受影响对象；私人分析参数不能改变共享取证语义。

事项保留初版三类revisionId及hash，每个轮次独立冻结三类版本及hash；核实和活动记录同时保存自己的表单版本，报告引用轮次及实际输入版本。新版本不修改旧事项和旧轮；显式迁移记录旧新版本与字段映射，旧记录按原版本呈现。改变报警类型、来源语义或实物身份时建立关联新事项；兼容的选项扩展可在新治理轮次采用新版本。已退休版本不能用于新建轮次，历史仍可读。

**五 数据对象与存储约束**

业务表使用PostgreSQL。通用字段为id、tenant_id、version、created_by、created_at、updated_at；追加记录另含occurred_at、recorded_at、corrects_id、correction_reason、idempotency_key及资源版本。时间为UTC Unix毫秒，区间统一[start,end)。

| 模型及建议表 | 核心字段和责任 |
| --- | --- |
| GovernanceCase / alarm_governance_case | 标题、场所位置、deviceId、componentId、alarmType、originKind、signalKey、三类revisionId、owner、status、currentRoundId、dataRevision；默认单点位 |
| GovernanceRound / alarm_governance_round | caseId、number、previousRoundId、三类revisionId/hash、起止、实物和位置版本、ACTIVE/COMPLETED/CANCELLED、确认报告、结束说明 |
| AlarmObservation / alarm_observation | 本次源身份、原文/标准消息、点位/类型/通道、ASSERT/CLEAR/REPORT/UNKNOWN、时间、版本、接纳结果、关联平台告警 |
| AlarmCycleRevision / alarm_cycle_revision | 对象键、method、开始/结束证据、状态与边界质量、成员引用、关联平台告警、算法/配置版本、替代关系 |
| AlarmEvidenceLink / alarm_governance_alarm_link | case/round、observation或cycleRevision、关联依据、确认人、更正/解除关系；不扩大现场结论适用范围 |
| FieldVerification / alarm_governance_verification | 单点位/信号、具体事件范围、表单版本、核实方式/时间/范围、现场结果、活动及设施观测、原始说明、证据、确认状态；可独立保存 |
| VerificationRoundLink / alarm_governance_verification_link | verificationId/version、roundId、适用事件、关联人、时间及更正关系；不修改原核实 |
| FieldActivityRevision / alarm_governance_activity | 稳定activityId、位置、明确deviceIds、实际/计划、类型与单位、起止、时间质量、来源、确认状态、条件、版本 |
| ActivityCoverage / alarm_governance_activity_coverage | 位置与设备范围、活动类型、登记区间、FULL_DECLARED/PARTIAL/ALARM_ONLY/UNKNOWN、声明人及依据 |
| CauseAssessment / alarm_governance_cause | round、候选原因、适用observationIds、支持/冲突证据、状态、确认人及确认依据 |
| ImprovementMeasure / alarm_governance_measure | round、措施、负责人、期限、状态、实施记录、专业依据和独立验收 |
| ObservationPlan / alarm_governance_observation_plan | round、措施版本、前后窗口、活动单位、评价口径、准入与排除条件、最小观察要求、版本 |
| ObservationReview / alarm_governance_observation_review | plan版本、analysisSnapshotId、factsHash、结论、限制、确认人、时间及更正关系 |
| GovernanceAttachment / alarm_governance_attachment | case、verification或activity归属、明确deviceIds、服务端StorageKey、MIME、大小、hash、上传者、来源和可用性 |
| GovernanceBusinessLink / alarm_governance_business_link | 主对象、目标资源类别及ID、目标版本、关系、受控摘要、同步状态；不复制对方主责状态 |
| GovernanceEvent / alarm_governance_event | 事项动作、资源版本、actor、occurredAt/recordedAt及不可变最小正文，用于审计、复发和提醒 |
| GovernanceSourceVersion / alarm_governance_source_version | 点位/信号、sourceKind、UTC时间桶、generation；保证晚到事实和独立活动更正使受影响快照过期 |
| DiscoveryPolicy / 分析配置版本 | 问题发现窗口、独立条件阈值、适用范围、版本/hash；归入现有分析配置，不影响报警规则 |
| GovernanceReminder / alarm_governance_reminder | 接收用户、case/round、类别、幂等范围、期限、已读/处理状态；正文发送前受权生成 |

模板三类版本可使用专门表或沿AnalysisConfigRevision存储不可变正文，但必须有可索引的类型、资源、版本、状态与当前发布指针；不能把全部对象无约束堆入一个JSON列。建议专门配置表，保留与共享分析配置的引用适配。

数据库约束至少包含：tenant+资源+version唯一；case+roundNumber唯一；活动revision唯一；来源观测身份唯一；幂等请求唯一；所有关联校验租户。用组合外键或事务校验保护case、round、device、component、activity与附件关系。空设备集合不得解释为全租户。

同一租户、实物点位、alarmType、originKind及signalKey只允许一个活动治理事项，使用相应条件唯一约束防并发重复创建；已完成事项优先复核重开。实物或信号语义改变建立新的关联事项，不将新对象强塞旧事项。事项和活动使用历史位置快照与明确设备成员，位置筛选沿用现有设备/部件字段，不依赖已移除的拓扑或设备影子；不存在实物更换历史时保存UNKNOWN及首次人工确认版本。

事项索引按tenant+status+owner+updated_at；观察事实按tenant+device+component+alarmType+originKind+event_at及recorded_at；活动按tenant+时间和成员关联表；证据按tenant+round+来源；轮次与措施按tenant+case/round+状态。大时间范围查询使用稳定游标，不以海量OFFSET扫描源事实。

当前状态和发布指针采用乐观版本控制。核实、活动、原因及正式确认使用追加和更正关系，正式版本不可原地覆盖。人员同时追加不同现场记录均可成功；每次有效追加原子递增dataRevision，编辑同一对象或改变阶段使用expectedVersion。

**六 可靠逐次事实与时间契约**

新增AlarmObservation是最重要的基础，不能仅复制当前告警列表、TriggerCount、DutyAlarmEvents聚合正文或短期outbox。每条事实包含sourceSystem、sourceEventId或源序号、eventIndex、rawMessageId、standardMessageId、deviceId、componentId、assetInstanceId或未知、alarmType、originKind、signalKey和factKind。

originKind由实际产生路径确定：COMPONENT_STATE、DEVICE_DIRECT、RULE_LIFECYCLE等；Alarm.Source=device不能区分设备断言与属性规则。signalKey使用明确来源命名空间，规则信号必须包含ruleId，另存ruleVersion/conditionHash，设备信号存协议与字段版本；不同规则不能因alarmType相同而合并。视频作为受控辅助证据，首期不据视频自行重建点位物理周期。

| 时间 | 来源和用途 |
| --- | --- |
| eventAt | 设备或源系统声明发生时间，保存TRUSTED/UNVERIFIED/ESTIMATED/MISSING/CONFLICTED质量 |
| receivedAt | 对应原文的可靠平台接收时间，可缺失；不能用设备时间填充 |
| recordedAt | 本条业务事实登记时间，用于追溯，不冒充现场发生时间 |
| availableAt | 成功持久化确认后的可用时间凭据及阶段来源，可缺失；丢失凭据保持未知 |
| evaluationAt | 实际规则求值或状态接纳时间，按产生路径记录 |

原文导航始终使用rawMessageId，标准消息ID只作关联。平台告警首末时间存在处理时钟来源，不能直接当设备触发时间。活动精确关联需要可比较的时间依据；时钟未验证时仅展示保守关联及限制。

写入在现有告警/部件仓储事务内保留本次incoming及接纳结果。普通UpsertAlarm的首次和再次上报分支均记录；部件明确CLEAR、过旧但有效输入、状态冲突以及未产生平台告警的源状态也要有出处。重复源身份不新增观察。ACK/CLOSE继续引用既有生命周期事件，不重复造一套人工操作账本。

设备直接恢复和规则恢复当前经过[核心告警处理](../internal/core/engine.go)的mutateAlarm路径，既有生命周期正文不足以还原触发恢复的messageId/rawMessageId、规则版本与原因。这些路径也必须扩展为带源observation的状态事务，不能只补UpsertAlarm和部件路径。没有活动告警时仍记录明确正常状态作种子；无恢复定义时维持REPORT_ONLY。规则停用、人工关闭、来源缺失与实际规则条件恢复使用不同reason，不能统一当真实恢复。

建议提取现有事务内私有helper，或增加带observation的窄写端口，在同一事务保存既有状态与有界事实记录；内存适配保持同样语义。复杂周期计算、查询、模型和MinIO操作不进入生产事务。可靠事实写入失败按原持久化事务失败/重试机制处理，不能吞错后宣称历史完整；治理分析worker失败不阻断生产告警处理。

拟新增读取端口为ListAlarmObservations、GetAlarmSignalSeed、ListGovernanceRevisions、ListActivityExposures、ListActivityCoverage、GetObjectIdentityHistory、ListGovernanceEvidenceMetadata。生命周期仍复用已有受权事实接口。任何值班正文、维修资料或历史影像必须经过其源权限适配，不能仅按deviceId查询就送给治理用户或模型。

观测去重键为tenant+sourceSystem+可信源身份+稳定事件槽位；槽位包含点位、按已冻结类型配置确定的signalKey/类型和eventIndex，不包含ASSERT/CLEAR等信号值。factKind和时间/内容属于hash校验正文；同一槽位的ASSERT变CLEAR必须检测为冲突，不能因键改变变成两条合法输入。一条输入的“状态断言”和“收到上报”是同一观察的两种指标用途，不重复创建槽位。源身份入口另外校验原输入hash，防止改变类型或部件绕过同身份冲突。一条报文的多个合法部件和信号槽位分别保存。无可靠外部身份时使用平台rawMessageId或standardMessageId作为观测身份，并注明无法证明不同原文是否同一物理事件。

相同身份且相同内容只计一次；相同身份不同内容进入冲突记录，不覆盖；相同内容不同合法身份保留。回放、测试、调试和来源未明单列。治理去重不改写生产计数。

sourceContentHash仅覆盖原输入的规范内容、源时间和稳定字段身份，不包含recordedAt、evaluationAt、attempt、接纳结果或平台alarmId；处理尝试和实际接纳决定用独立追加记录保存，重试不制造源冲突，不改首次已提交观察的含义。eventIndex取协议稳定槽位；无法提供稳定槽位的来源保留解析版本与平台输入身份，不用重新排序后的数组下标冒充厂商事件序号。

每个来源记录collectionStartedAt、backfillRange/status、coverage及historicalQuality。历史标准消息和可靠事件可以只读归一化回填，保留原始配置和出处；禁止重投生产队列补历史。无法还原逐次输入、实物或恢复边界时保留PARTIAL，不从累计数均匀摊出虚构事件。回填与projection幂等， checkpoint按已消费成员提交，最大序号不表示全部较小事务都已提交。

**七 报警周期重建与历史不确定性**

报警上报次数、平台告警生命周期和点位信号周期分别计算。生产告警的确认或关闭只改变其人工处置状态，不代表现场信号恢复。

| cycleMethod | 必要输入 | 可输出的结论 |
| --- | --- | --- |
| COMPONENT_BOOLEAN | 同一部件/类型的明确布尔状态、接纳结果及窗口前状态 | 点位信号由正常转为报警、再明确恢复的周期 |
| DIRECT_EXPLICIT_STATE | 协议明确提供同一信号的ASSERT/CLEAR语义和关联键 | 设备显式信号周期；不能用不同类型恢复报文代替 |
| RECORDED_RULE_LIFECYCLE | 可追溯规则触发、恢复与规则版本 | 已记录规则周期；页面明确其不是物理探测器周期 |
| REPORT_ONLY | 只有报警上报，缺少可靠恢复或身份边界 | 上报次数、时间分布、核实记录；周期次数和持续时间保持不可计算 |

周期键为tenant+assetInstance/device+component+alarmType+originKind+signalKey。FIRE、FAULT、不同规则或不同通道分别处理；component缺失不得自动归并所有部件。重建使用源路径实际接纳决策与watermark，保留未接纳输入作证据，不能另造一种排序后覆盖生产接纳语义。当前同时间冲突按既有状态实现处理，较旧CLEAR不能结束较新ASSERT。

明确正常→ASSERT才产生新开始；持续ASSERT只增加成员事实；明确CLEAR结束当前周期。报文没有该字段、设备心跳、人工ACK/CLOSE、网络恢复、聚合告警ID变化均不能代替CLEAR。关闭后的持续ASSERT即使生成新平台告警ID，仍可能属于同一未恢复信号周期。

周期状态使用OPEN、CLEARED、TERMINATED_UNKNOWN；边界质量单独使用COMPLETE、LEFT_CENSORED、RIGHT_CENSORED、BOTH_CENSORED、UNKNOWN。例如窗口开始时已经报警、窗口结束时仍报警，显示OPEN+BOTH_CENSORED，不能把窗口长度写成完整持续时间。TERMINATED_UNKNOWN表示实物更换、来源中断等导致当前周期无法继续追踪，不表示已恢复。

窗口统一为UTC的半开区间[start,end)，页面按租户展示时区呈现。先查询窗口前最后可靠状态；窗口内明确新开始计入“新发周期”，包括到窗口结束仍未恢复的周期。窗口前已经开始的左删失周期单列，不计入窗口内新发次数。完整恢复周期才参与完整持续时间统计，同时展示未结束数量、已知持续时长下界及未知边界数量。

种子还须校验身份/配置一致、来源覆盖和时效，不能直接拿任意久远最后一条正常状态。采集缺口期间可能发生未记录的恢复与再报警：不能凭缺口前后ASSERT证明连续，也不能凭缺口前NORMAL和缺口后ASSERT确定新开始时刻。按可确认连续段保存未知边界，不更新生产状态为正常；持续时长只在有依据的连续段计算，不能把含未知间隙的墙钟差标为完整时长或可靠下界。问题汇总同时保留“平台状态仍报警”和“信号连续性未知”。

来源时间不可信时，允许另外计算“按平台接收时间的上报分布”，必须标明口径；不能与可信设备时间混算物理周期或精确活动重叠。缺少窗口前状态时，首个ASSERT显示开始未知，后续明确CLEAR与再次ASSERT才建立可确认的新开始。

设备实物更换产生新assetInstanceId，初始状态未知；位置、产品、协议、算法或类型配置变化将比较窗口分段。普通配置文字补充可以明确批准沿用身份，涉及信号语义变化必须建立新信号版本；不能通过配置切换凭空制造恢复或新发。

迟到事实、冲突解决和补录会生成新的AlarmCycleRevision及分析快照，保留supersedes关系。现场核实和原因确认绑定具体observationIds及核实时间范围；周期合并或拆分后提示复核关联，不能把旧“活动相关”结论自动扩展到新加入事实。已确认报告不覆盖，重分析形成新报告版本并显示差异和新增证据。

**八 现场活动关联、覆盖与评价指标**

活动是独立登记的实际发生记录，不是报警备注的另一种写法。餐厅可以登记“午餐烹饪，10:30—12:00，实际发生，活动单位=一次供餐”；同样登记未报警的供餐。施工、清洁、粉尘作业、蒸汽使用及设备维护使用各自活动单位。计划活动、只有模糊日期的补录、未确认记录可以作为线索，不直接进入精确暴露分母。

活动关联先由确定性程序生成候选：同一授权点位、可比较时间区间、配置允许的活动类型和来源。默认采用实际区间重叠；若存在经过审核的扩展观察窗口，保存依据、版本及具体分钟数，只影响事后关联。重叠只是时间相关，多个活动同时发生时全部保留，并允许确认“原因未明/存在其他因素”。AI不自动把最接近的活动确认为原因。

ActivityCoverage用于声明活动登记范围：FULL_DECLARED=负责人声明该区间按约定完整登记，PARTIAL=有缺漏，ALARM_ONLY=只在报警时填写，UNKNOWN=无法判断。完整声明仍需显示来源与抽查记录，不能描述成系统知道全部现场活动。ALARM_ONLY或UNKNOWN不生成“每次活动报警率”，仅展示已登记事件分布。

PARTIAL最多展示“已登记子集中的已确认相关占比”，不能标为全部活动或用于完整效果比较。正式活动比例限定在FULL_DECLARED范围与合格监测区间内、起止和活动单位明确的实际活动；跨边界或中间存在未知监测间隙的活动单列，不随意截成一次完整合格活动。存在可证明完整登记的子区间时另存明确coverage记录，报告仍展示其未覆盖部分。

有效评价时间由评价窗口与可用监测区间求交，再扣除测试、停用、明确离线、采集缺口、不可比较时钟等区间的并集。网络在线不等于探测功能已经验证；没有上报不等于离线。无法建立可用监测区间时，展示数量与覆盖限制，按监测小时计算的频率保持不可计算。区间重叠只扣一次，分子和分母使用同一点位、信号版本、时间口径与准入条件。

| 指标 | 计算口径与页面要求 |
| --- | --- |
| 原始上报数 | 按合法源身份去重后的REPORT/ASSERT数；生产TriggerCount另列，不混用 |
| 新发周期数 | 窗口内可靠新开始，包含未恢复周期；规则周期与点位周期分列 |
| 持续与边界 | 完整周期时长分布、OPEN数、左右删失数、未知边界数分别展示 |
| 现场核实覆盖 | 对约定待核实事件单元统计已完成且有实际核实方法的记录；未核实/资料核对单列 |
| 已确认成因分布 | 仅统计适用范围已明确的人工确认；候选、争议与原因未明单列，可多因素 |
| 每千有效监测小时新发数 | 合格新开始数/对应有效小时×1000；无可靠周期或有效小时不计算 |
| 已确认活动相关占比 | 至少发生一次已确认相关报警的合格实际活动数/完整声明范围内全部合格实际活动数；同时显示相关性未核实数和识别区间 |
| 改进前后变化 | 同时显示前后计数、分母、覆盖、配置版本和现场条件；可比较时才计算差值 |
| 工作闭环 | 待核实、超期措施、待验收、观察资料不足、完成后待复核复发分别计数 |

一场烹饪活动有多次报警，活动相关比例的分子仍为1；多个点位关联同一activityId时，场所总活动数按activityId去重，不能把各点位分母相加。每个点位可以分别评价，但总览必须说明汇总单位。未确认的新报警仍进入总体上报或新发统计，不纳入“已确认活动相关”子指标，也不能被遗漏以改善统计结果。

合格活动总数N中，C为至少一次已确认相关，U为存在报警但相关性未核实/有争议且尚不能排除相关，其余分别记录已核实无相关报警和合格监测中无已记录报警，类别互斥。N>0时显示C/N及[C/N,(C+U)/N]，明确该区间反映未核实事件归属，不是统计置信区间；N=0或监测覆盖不足时保持不可计算。U不默认算作正常；核实率变化或前后区间无法支持计划规定的结论时显示资料不足，不宣称改善。无已记录报警也不改写为已经证明现场无任何报警。

示例仅说明口径，假设登记与核实完整、U=0且条件可比：改进前50次供餐中5次出现已确认相关报警，改进后20次供餐中2次，均为10%。虽然相关次数从5降到2，不能写“下降60%”；供餐次数也下降。若前后登记覆盖、探测器型号、位置、运行时间或烹饪方式不同，应显示“条件不一致，暂不作效果比较”。

ObservationPlan在观察前保存：评价对象与信号版本、实施时间、前后窗口、活动单位、准入/排除条件、最低有效监测时长和实际活动数、覆盖要求及复核负责人。最小要求按现场确定，不设一个适用于所有场所的“观察7天即通过”。报告输出改善、未改善、恶化、资料不足或条件不可比较，并保留具体限制；这些结果描述本轮观察，不估计真实火灾漏报率、未来安全性或因果收益。

**九 治理状态、并发与跨轮次处理**

事项状态为PENDING_INVESTIGATION（待核查）、INVESTIGATING（核查中）、IMPROVING（改进中）、OBSERVING（观察中）、COMPLETED（本轮完成）、CANCELLED（取消）。轮次状态为ACTIVE/COMPLETED/CANCELLED，同一事项最多一个ACTIVE轮次。

| 动作 | 必须满足的条件 | 结果 |
| --- | --- | --- |
| 建立事项 | 点位/信号/模板合法、当前授权、明确负责人 | 固定三个配置版本并创建首轮；已有同对象活动事项提示关联 |
| 开始核查 | 负责人有效、轮次ACTIVE | 可追加核实与候选原因，不修改生产告警状态 |
| 确认原因 | 人工选择具体事件、核实依据与支持/冲突证据 | CONFIRMED或DISPUTED等追加版本，不确认未来事件 |
| 提交措施 | 有措施内容、负责人、期限与适用依据 | 进入改进；允许原因未明时提交进一步检查措施 |
| 开始观察 | 必要措施已实施并具备要求的验收，观察计划已确认 | 固定措施与计划版本，开始采集观察事实 |
| 确认评价 | 已完成确定性分析、授权证据可用或明确缺失、快照未过期 | 保存人工ObservationReview，AI解读作为可选附件 |
| 完成本轮 | 必要措施有结论，观察评价与后续责任已确认 | 固定正式报告与轮次；不自动关闭生产告警 |
| 复发重开 | 人工核对新增事件并有reopen权限 | 新建轮次，关联前轮；前轮报告与结论保持原样 |
| 取消 | 有权限、原因、未完成工作去向 | 取消当前轮次，保留事实与审计 |

CauseAssessment使用CANDIDATE/PENDING/CONFIRMED/DISPUTED/REJECTED；ImprovementMeasure使用PLANNED/IN_PROGRESS/IMPLEMENTED/VERIFIED/CANCELLED。实施记录、专业验收和效果观察是不同证据。涉及安装位置、型号或设施变更时，措施只记录由有权限的现场人员依据适用要求实施和验收，平台不直接操作设备。

“本轮完成”表示责任人完成本轮评价，可以是资料不足或未改善，页面必须同时显示结果和后续负责人，不能用绿色完成状态暗示风险解除。仍有必要措施待实施/验收时不能绕过条件；无法继续的措施先明确取消理由与后续安排，再结束本轮。完成后新报警生成待复核提醒，人工判断是否重开，不自动继承旧成因或开启新AI调用。

状态变更与正式确认携带expectedVersion、dataRevision、analysisSnapshotId及factsHash。服务端事务锁定事项/轮次，校验配置、证据成员及当前权限，原子保存动作、报告引用和审计。核实记录并发追加可以成功；若其改变了待确认快照的dataRevision，正式确认返回409并要求刷新差异，不能静默接受旧结论。

过期判断还必须包含sourceRevisionVector：源观测/周期、活动与覆盖、身份/位置、配置语义及关联资料版本。采用PostgreSQL持久依赖版本表，按tenant+点位/信号+sourceKind+受影响UTC时间桶记录generation；写入/更正同事务更新原、新时间范围，已知种子变更更新其影响范围，时间无法定位时采用保守范围。快照保存实际依赖桶版本、种子、成员hash及具体资源revision。正式确认在同一事务读取并核对依赖版本，与输入变化的版本行加锁顺序统一；只拒绝影响本次窗口/依赖的变化，窗口外普通报文不使全部历史报告失效。ClickHouse历史先归一化进带版本的治理事实，不用实时跨库查询替换正式确认依据。

冻结时确保所有依赖版本行已经存在，generation=0也创建行；写入和正式确认按相同键序锁定，防止“原来没有版本行”绕过并发检查。种子依赖范围延伸到下一个可确认状态边界；明确范围外的事件不更新本次依赖，无法确定影响范围则显示保守失效原因。状态变化用expectedVersion，sourceRevisionVector用于判断分析输入，二者不能互相替代。

重复请求按tenant+actor+action+idempotencyKey返回同一结果；同key不同正文返回冲突。改负责人只校验该账户在租户内有效和其目标资源访问条件，不为其自动增加权限。人员离岗后保留原记录和签名，当前任务可重新分派。治理流程独立于是否存在ACTIVE值班班次，避免无当班记录就无法核查。

**十 HTTP API与请求契约**

拟新增路由前缀为`/api/v1/alarm-governance`；以下是开发目标，尚未注册为可调用接口。参数沿用现有JSON命名、错误响应与认证中间件，不接受请求体自选tenantId。列表默认20、上限100，事实流使用稳定游标；批量ID数量、时间范围、上传大小和分析点位数均在服务端限制。

| 路由族 | 方法和主要端点 | 说明 |
| --- | --- | --- |
| 配置 | GET templates/scene-presets/type-profiles及各资源`/:id`、`/:id/revisions`、`/:id/revisions/:revisionId`；POST各资源及`/:id/revisions`；PATCH草稿revision | 内置只读、租户复制与草稿编辑；版本正文和影响预览，发布版本不可PATCH |
| 配置发布 | POST各资源`/:id/revisions/:revisionId/validate`、`/publish`、`/retire` | 完整授权范围校验与原子发布；不执行模板中的代码 |
| 问题发现 | GET `/candidates`；POST `/runs` | 按上报/可靠周期/未核实/措施超期等筛选；确定性分析异步生成固定快照 |
| 点位事实 | GET `/observations`、`/observations/:id`、`/cycles/:id` | 明确deviceIds、alarmId及有界窗口筛选，供报警详情独立核实；源权限与当前范围检查 |
| 事项 | GET/POST `/cases`；GET/PATCH `/cases/:id` | 创建、列表、详情与有限基本信息编辑；状态只走动作端点 |
| 事项动作 | POST `/cases/:id/start`、`/assign`、`/start-observation`、`/complete`、`/cancel`、`/reopen` | 精确权限和版本校验；重开返回新轮次 |
| 轮次与时间线 | GET `/cases/:id/rounds`、`/rounds/:id`、`/rounds/:id/events` | 不可变历次记录与可分页时间线 |
| 报警关联 | GET/POST `/rounds/:id/alarm-links`；POST `/alarm-links/:id/corrections` | 显式观测成员与理由，解除保留更正链 |
| 现场核实 | GET/POST `/verifications`；GET/PATCH `/verifications/:id`；POST `/:id/confirm`、`/corrections`；GET/POST `/rounds/:id/verification-links` | PATCH仅限未确认草稿；支持独立核实及追加轮次关联；补录保留实际/登记时间 |
| 现场活动 | GET/POST `/activities`；GET `/activities/:id`及`/:id/revisions`；POST `/activities/:id/confirm`、`/revisions` | 实际活动、计划活动与时间质量；独立于是否报警；更正追加版本 |
| 活动覆盖 | GET/POST `/activity-coverages`；POST `/:id/revisions` | 声明登记完整性和范围，不从报警备注推断覆盖 |
| 原因 | GET/POST `/rounds/:id/causes`；GET/PATCH `/causes/:id`；POST `/causes/:id/confirm`、`/dispute`、`/reject`、`/corrections` | PATCH仅限未正式确认的候选；结论严格绑定事件与证据 |
| 措施 | GET/POST `/rounds/:id/measures`；PATCH `/measures/:id`；POST `/:id/start`、`/implement`、`/verify`、`/cancel` | 实施、验收及取消各有记录；已正式确认内容用更正版本 |
| 观察计划与评价 | GET/POST `/rounds/:id/observation-plans`；POST `/observation-plans/:id/confirm`；POST `/rounds/:id/observation-reviews`、`/observation-reviews/:id/confirm` | 固定评价口径与快照，不在确认时重新运行模型 |
| 分析任务 | GET `/runs`、`/runs/:id`、`/runs/:id/snapshot`、`/runs/:id/metrics`、`/runs/:id/findings`、`/runs/:id/evidence`；POST `/runs/:id/stop` | 复用现有analytics生命周期、结果路由与持久worker能力 |
| AI解读 | GET/POST `/runs/:id/ai-jobs`；GET `/runs/:id/ai-jobs/:jobId`；POST `/runs/:id/ai-jobs/:jobId/stop` | 沿用现有嵌套任务路由，明确按钮触发，绑定完成快照 |
| 附件 | POST `/cases/:id/attachments`、`/verifications/:id/attachments`或`/activities/:id/attachments`；GET `/attachments/:id/download`；DELETE `/:id` | 服务端生成存储键；草稿无引用可删，正式证据只撤回引用/标记不可用 |
| 业务关联 | GET/POST `/cases/:id/business-links`；POST `/business-links/:id/corrections` | 值班、维修、规则验证等使用专用授权适配 |
| 正式报告 | GET `/cases/:id/reports`、`/reports/:id`、`/reports/:id/export` | 每份对应固定轮次/快照/人工确认；导出重新鉴权 |

分析路由通过共享analytics注册表接入新kind，治理事项路由由专用handler处理。禁止另起一套与现有analysis run重复的AI队列。创建资源返回201，后台运行返回202及持久任务ID，版本/幂等冲突返回409，其余错误遵循现有响应约定。不得由前端把任意JSON状态值直接写回。

创建事项请求示例（全部ID为占位符）：

```json
{
  "deviceId": "device-example",
  "componentId": "component-example",
  "alarmType": "FIRE",
  "originKind": "COMPONENT_STATE",
  "signalKey": "smoke-alarm",
  "templateRevisionId": "template-revision-example",
  "scenePresetRevisionId": "scene-revision-example",
  "typeProfileRevisionId": "type-revision-example",
  "title": "厨房点位反复报警核查",
  "ownerUserId": "user-example",
  "observationIds": ["observation-example"],
  "idempotencyKey": "create-case-example"
}
```

核实记录示例：

```json
{
  "deviceId": "device-example",
  "componentId": "component-example",
  "alarmType": "FIRE",
  "originKind": "COMPONENT_STATE",
  "signalKey": "smoke-alarm",
  "templateRevisionId": "template-revision-example",
  "scenePresetRevisionId": "scene-revision-example",
  "typeProfileRevisionId": "type-revision-example",
  "observationIds": ["observation-example"],
  "verificationMethod": "ON_SITE",
  "verifiedAt": 1790824200000,
  "fieldResult": "UNABLE_TO_DETERMINE",
  "activityRelation": "SUSPECTED",
  "fieldValues": {"visibleSteam": "YES", "ventilationRunning": "UNKNOWN"},
  "activityIds": ["activity-example"],
  "description": "到场时仍在烹饪，见蒸汽；排风运行情况未核实。",
  "attachmentIds": [],
  "idempotencyKey": "verification-example"
}
```

火警配置的fieldResult至少保留FIRE_OBSERVED、OTHER_ABNORMALITY_OBSERVED、NO_FIRE_OBSERVED、UNABLE_TO_DETERMINE，中文为发现火情、其他异常、未发现火情、无法确定；其他报警类型用TARGET_ABNORMALITY_OBSERVED/NO_TARGET_ABNORMALITY_OBSERVED等与其目标匹配的选项，同样保护未知和其他异常。检查范围与当时条件同时记录，不能把“未发现”解释成以后不会发生。activityRelation=SUSPECTED是线索，不替代现场结果，也不直接映射成CONFIRMED成因或生产“误报”状态。创建记录允许信息未知，confirm时校验实际方法、事件范围和模板必填依据，缺失内容给出具体字段。发现真实火情或其他异常时继续原报警处置流程，治理表单不增加其前置条件。

所有API时间字段沿用UTC Unix毫秒整数，以上verifiedAt对应2026-10-01 11:10（Asia/Shanghai）；时间质量与实际/登记时间分别返回，未知值使用可空字段和明确质量，不能以0冒充1970年的现场时间。expectedVersion用于编辑/动作，幂等键采用请求体idempotencyKey并沿用现有requestHash校验，不再叠加另一套不一致的Header协议。

分析请求包含明确deviceIds、caseId/roundId、时间窗口、三个配置版本、观察计划版本及expectedDataRevision，服务端保存受权inputsHash与来源cutoff；不允许前端自带已计算指标作为事实。返回taskId/runId、真实阶段和已处理/已发现数量；无法预先知道总量时不展示虚构百分比。

**十一 Harness工作流与共享分析能力扩展**

新分析kind拟定为`RECURRING_ALARM_GOVERNANCE`，菜单为`alarmGovernance`，路由前缀对应上节。处理器建议放在`internal/analytics/recurring/`，确定性结果负责周期、活动候选、覆盖和比较；AI只解释固定事实并整理待核查问题。模型与知识配置复用现有管理入口。

当前共享analytics虽已有运行、快照、证据、lease及AI持久记录，实际处理器注册、AI token、核心调度和MCP集合白名单仍有DATA_QUALITY专用分支。开发时逐一扩展kind注册与权限映射，不能只加页面或只修改一个常量就声称工作流接通。

建议建立受控WorkflowDefinition注册表，集中保存kind、menu、prefix、processor、manifest、输出schema、允许的MCP集合和证据类别。HTTP、core、AIService及MCP按同一个已注册kind查询；未知kind拒绝，已有DATA_QUALITY行为保持原契约。表内不允许用户任意提供工具名称或manifest路径。

当前AnalysisAIResult为固定通用结构，AnalysisAIRevision.Interpretation虽可存JSON，也不能自动支持本方案八类业务字段。增加按workflowKind选择的schema/decoder与受控业务payload，在typed FinishAnalysisAIJob写入前完成校验；明确保持旧工作流兼容。禁止将模型任意JSON直接写进正式业务对象。

新增内置只读Manifest拟放`deploy/deepseek-harness/plugins/recurring-alarm-analyst.json`，知识归属绑定其workflowId。提示词与输出schema随版本保存。AI输出至少包含以下八类：

| 输出 | 内容与校验 |
| --- | --- |
| facts | 已知事实摘要，每项引用factIds；不重新计算或改写统计 |
| patterns | 时段、活动、位置及复发模式，说明口径与样本范围 |
| hypotheses | 候选原因、支持/冲突证据和缺失信息；只能是建议 |
| checks | 下一步现场核查问题、方法和所需记录 |
| measures | 可供人员评估的措施方向及专业依据来源，不包含自动控制指令 |
| observation | 观察计划草稿、需满足的登记条件和可比性要求 |
| limitations | 时钟、历史覆盖、身份、未核实、来源不可用及混杂因素 |
| summary | 供值班交接或治理报告引用的简述，保留事实与建议的区别 |

数值引用使用metricRefs，服务端从固定快照渲染数值、单位、分母及窗口，模型不能提供新的统计值替换结果。factIds/metricRefs必须属于绑定快照、当前可访问，并且已实际提供给该job或经其受控工具读取；沿用现有SentFactIDs类记录，不允许引用未读页充当已审阅依据，输出显示实际阅读覆盖。错误引用和未知字段拒绝或按schema显式剔除并标记校验结果。自然语言中的因果断言无法只靠JSON校验完全保证，用户必须查看证据并人工确认，模型文本不自动转成CONFIRMED。

工作流顺序为：点击“生成AI解读”→校验菜单/操作/设备与源资源权限→锁定完成的分析快照→创建持久AI job→Harness读取受控快照与知识→结构化输出校验→再验证权限/输入版本→保存AI revision→用户选择引用或修改。重复点击复用同一幂等请求；数据变化提示新建快照。打开页面、上报告警、定时问题发现及重开事项均不自动调用模型。

MCP工具沿用平台受控只读analytics工具，按job绑定run/snapshot及白名单集合：观测、周期、核实、活动、覆盖、措施、观察结果及已授权证据元数据。工具不能扩大时间/设备范围，不能跨租户、读完整原文或现场资料中的任意链接。知识检索使用现有workflow/Agent归属与SQL筛选，首次模型请求附授权证据。

启动前、每次工具读取前和结果保存前重新校验当前权限及scope。变更使待发送输入失效；已发出的外部调用不能宣称撤回，结果应隔离并记录状态，不能继续保存/显示给失权用户。失败或取消不改变确定性分析已完成状态；租约过期的RUNNING AI若外部执行情况未知，按现有恢复语义处理，不自动再调用产生重复请求。

活动说明、附件正文、知识和工具结果均视为资料，不能授权调用工具、改变工作流或生成设备命令。产品内Harness继续禁用shell、文件系统、jobs、goal、skills、subagent及设备控制。首期图片只用于人工核实，AI默认读取受控文字摘要和元数据；不假定已配置模型支持图片，也不将完整直播流、存储URL或现场附件自动发给模型。

**十二 权限、附件、跨模块关联与提醒**

菜单、操作和资源范围都由后端检查。点位分析需要devices、alarms及alarmGovernance的对应读取条件；新增操作至少区分查看、建事项、记录、确认原因、管理措施、验收、确认观察、完成/重开、AI解读、导出、模板编辑和发布。既有角色映射沿用当前权限存储，不靠前端隐藏按钮。写入动作必须逐项注册，不把所有POST统一当作一项宽泛写权限。

单点位事项按实际device/component逐项校验，主子设备不继承。活动涉及多个deviceIds时，完整说明和附件要求其完整成员范围授权；仅拥有其中一部分不能读取共享说明中的其他点位，也不能在列表泄露隐藏ID、名称和数量。对多点位问题总览，服务端可以只用当前明确可见点位生成新的授权子集分析，并标明其范围；已有完整报告不能简单裁剪几行后冒充同一正式报告。

分析输入、source adapter、详情、候选数、证据、下载、导出、提醒及AI读写均校验当前权限。事项负责人身份不授予源资料权限；转交不改变deviceScope。派生的摘录、核实摘要、AI正文和正式报告继承其来源权限，记录provenance依赖；必要来源撤权时拒绝整份相关正文/导出，不能只隐藏证据按钮后继续返回缓存全文。需局部结果时重新生成不含该来源的新授权快照及报告。单纯文件过期与权限撤销分别处理：仍有权限但文件过期可呈现原受权报告并明确证据现已无法复查，不能伪装为仍可下载。受控不可用说明不得泄露无权资源的身份。

治理附件独立于DutyRun保存，可复用现有受控上传/下载及失败补偿模式。服务端生成私有StorageKey；客户端只提交文件和合法归属，不接收任意MinIO键或URL。首期允许JPEG/PNG/WebP/PDF，文件名清洗、扩展名与实际MIME匹配、大小限制和hash校验；若复用现有16MiB单文件上限则明确配置来源。上传成功后重新校验父对象和设备范围再登记，登记失败补偿孤立对象，后台清理只处理已证实无引用的对象。

正式核实或报告引用的附件不可直接物理删除；撤回、保留到期和源文件损坏均作为新可用性状态记录。对象缺失/保留到期时报告保留证据元数据与hash，并明确无法复查。历史视频通过专用VideoEvent来源适配器读取，校验tenant、事件设备、来源权限与保留状态；直播观看继续要求独立观看权限和播放会话，不把历史关联当作观看授权，不提供任意URL代理。

跨模块使用GovernanceBusinessLink保存资源引用：

| 目标 | 接入规则 |
| --- | --- |
| 值班任务/记录 | 创建/跟进按现有ACTIVE班次等写入条件执行；引用合法历史DutyItem/Record按源读取的成员、主管/历史权限与完整设备范围检查，不强制当前班次ACTIVE；无班次时治理仍可独立记录；值班项完成不自动完成治理 |
| 维修/维护记录 | 仅在存在可用资源与源权限适配时开放；未实现时显示不可关联，不生成伪工单 |
| 规则验证 | 引用经过授权的规则版本和验证记录，作为证据；本模块不启用/发布规则 |
| 摄像头/历史事件 | 受控来源摘要与授权下载；同区域/相近时间是候选关联，不自动证明原因 |

提醒首期采用平台内待办和当前已有受控用户事件通道：负责人超期措施、待验收、观察资料不足、正式报告后新增待复核事件。待办按case/round+提醒类别+具体任务或新事实范围幂等合并，持续重复上报更新计数，不每条产生新待办；期限、已读和处理状态持久保存。发送前按接收用户当前权限生成最小内容，撤权后不保留旧设备正文。治理提醒与消防报警、运维告警分别展示；本轮不新增外部短信/微信/邮件通道，不改变生产报警通知策略。

**十三 后台任务、迁移、性能与恢复**

确定性分析复用AnalysisStore的持久任务、租约、fencing token、取消及阶段提交；AI任务复用现有独立生命周期。任务输入保存配置版本、设备范围、授权版本、时间口径、来源cutoff与inputsHash，提交前验证租约和快照。规则变化、迟到数据和现场补录只使新快照可生成，不修改旧快照。

PostgreSQL事实读取复用既有可重复读快照模式；新增治理只读端口加入同一snapshot读取上下文，完成全部分页和依赖版本捕获后才声明inputsFrozen。分批持久化的成员和hash对应同一源快照；冻结中途失败时不继续拼接另一时刻的实时事实，丢弃未完成冻结结果并重新冻结。已完成冻结后，worker只读取不可变成员/版本。跨库历史归一化任务分别记录来源读取cutoff和缺口，不能声称PostgreSQL与ClickHouse共享一个事务快照。

归一化投影与历史回填建立独立持久任务，记录明确来源、区间、已完成批次和缺口。顺序号最大值不能证明较小事务都已提交；增量处理需使用可验证提交边界，或重叠扫描+源身份幂等并持续补查已知缺口。展示实际已消费数和扫描范围，不把检查点当作历史完整性的证明。重启从已提交批次恢复，测试数据与回放数据单列。

归一化观测及治理业务对象建议存PostgreSQL，原文继续使用现有PostgreSQL/ClickHouse路由，附件使用MinIO；不新增独立时序库或向量数据库。周期和活动关联按点位/信号/时间有界分页与批量查询，禁止每条观察再查一次设备/附件造成N+1。候选总览依赖聚合快照，详情再读取成员，界面长列表分页。

迁移沿用项目现有schema与迁移装配方式：拟新增`internal/adapters/postgres/alarm_governance_schema.sql`，通过go:embed纳入Repository.Migrate的既有迁移事务，新增表、复合唯一键、租户关联、索引和schema版本；上线前以真实PostgreSQL验证执行、重复执行及现有数据保留。启用时记录collectionStartedAt；不会在首次启动自动全量扫描原文或重投报文。历史回填由有权限人员选择来源和区间，明确预计读取规模与当前缺口。

运行限制复用`internal/config/analytics.go`的IOT_ANALYTICS_*配置；新增业务范围不能绕过MaxDevices、RecordLimit、MaxRange和队列限制。达到读取上限时输出PARTIAL、截断范围和不可计算指标，不静默给完整比例。超过单任务窗口的观察按受控分段任务处理，报告校验各段的条件/配置与重叠成员后合并，活动和跨段周期不重复计数；无法合并的段分别展示，不提高一个环境的限制后写成所有环境固定前提。

为新增事务事实写入测量告警持久化耗时、数据库错误和重试；为worker测量队列等待、扫描速率、projection延迟、快照规模、AI耗时与失败分类。初始并发与批量参数沿用现有任务限制并配置化，通过实际数据验证后调整；本方案不承诺未经实测的吞吐量或响应时间。模型不可用时事实、人工核实、措施和确定性观察评价仍可使用。

保留策略需分别覆盖原文、AlarmObservation、配置版本、活动修订、正式报告与附件。正式报告保存来源ID/hash与当时可用性，源到期不伪造可恢复证据。备份需同时包含新增数据库关系与治理附件，恢复测试检查引用完整性；恢复后未完成确定性任务按租约恢复，外部执行情况未知的AI请求保持可辨识失败/未知状态，不自动重发。

监控指标不包含现场敏感正文或全局跨租户设备标签。后台死信保留最小来源与错误分类，重试不重复生成治理事项、核实或正式报告。生产告警通道、分析任务失败和AI失败保持不同状态，方便判断问题发生在哪一层。

**十四 开发任务拆分与交付顺序**

按下面顺序开发，任务可以并行但依赖门槛不能跳过。前期先把现场记录和事实做好；AI接入建立在可追溯快照之上。开发阶段可以分批验收，但完整模块发布须满足所有必需批次，不以临时假数据页面代替交付。

| 编号 | 工作与建议落点 | 依赖与验收门槛 |
| --- | --- | --- |
| R01 | `internal/model/`定义配置、事项、轮次、核实、活动、措施和观察模型；确定状态/版本/错误契约 | 对照模板完成字段、状态和更正语义评审 |
| R02 | `internal/ports/`及`internal/adapters/postgres/`新增业务仓储、表与索引；匹配内存适配 | 租户关联、幂等、并发追加、正式记录不可变、迁移重复执行通过 |
| R03 | 告警与部件仓储事务保留逐次incoming和接纳结果，扩展直接/规则恢复及无活动告警正常种子路径；补齐来源/time契约 | 普通重复、CLEAR、过旧输入、同身份异信号冲突、重复源及生产链路回归通过 |
| R04 | `internal/analytics/recurring/`实现读取、来源种子、历史回填/投影及周期重建 | REPORT_ONLY不造周期；删失、关闭后持续报警、实物切换及迟到数据通过 |
| R05 | 模板/场景/类型配置服务与HTTP接口，内置7类场景初始化 | 草稿校验、只读内置、租户复制、发布与配置版本冻结通过 |
| R06 | 治理事项、轮次、独立核实及轮次关联、活动、覆盖、原因和措施业务服务 | 未建事项仍可核实、每轮配置冻结、状态门槛、来源范围、人员转交、更正和复发轮次通过 |
| R07 | 活动候选、覆盖区间、确定性指标和观察计划/评价 | 区间扣除、分母去重、未核实识别区间、PARTIAL限制、不可比较与资料不足结果通过 |
| R08 | `internal/httpapi/`专用handler与analytics kind/menu/prefix注册；`platformapp/`装配 | 完整API契约、操作权限、幂等错误与真实PG联调通过 |
| R09 | 私有附件、历史视频源、值班等BusinessLink适配 | 权限撤销、补偿、到期/损坏、无ACTIVE班次可独立记录通过 |
| R10 | `iot_front/`API、路由/菜单和问题总览、事项列表/详情、快捷核实、活动登记 | 真实接口、无默认正常结论、空/错/加载/并发冲突、390px交互通过 |
| R11 | 前端模板编辑、措施/观察流程、报告和差异复核 | 配置影响预览、最低观察要求、报告不可覆盖与复发重开通过 |
| R12 | 共享workflow注册、按kind输出schema/decoder、核心AI调度/MCP精确白名单与Manifest | 原DATA_QUALITY回归、新kind授权/输出校验/Harness实际运行通过 |
| R13 | AI解读页面、版本/证据引用与人工采纳 | 手动触发、数字引用、过期快照、取消/撤权/失败不影响事实通过 |
| R14 | 平台内待办、备份/恢复、监控指标、保留与部署说明 | 提醒当前范围、任务重启、备份对象引用、未知AI状态恢复通过 |
| R15 | 后端/前端/真实中间件及现场试点验收，更新仓库文档与操作指南 | 下节矩阵全部有明确结论；未验证项不得写为已验收 |

主要代码责任建议为：领域模型与专用业务服务承载状态和权限前置；ports定义最小事实/存储接口；PostgreSQL适配事务与查询；recurring处理器只做快照分析；HTTP负责参数和当前actor装配；MCP只读固定输入；Vue不计算权威统计、不直接操作数据库或模型。

前端组件按真实流程拆分CaseList、CaseDetail、VerificationForm、ActivityForm、CausePanel、MeasurePanel、ObservationPanel、TemplateEditor和ReportView，名称最终遵守仓库命名习惯。复用现有表格、表单、Markdown、任务状态及证据组件，不复制完整analytics页面另维护一套任务协议。

排期需按实际人员和历史数据质量估算，不把上述15项直接等同15个工作日。建议先选两个餐厅点位和一个非餐厅点位验证事实与活动登记，再扩展其余场景。任何功能默认启用前须确认真实数据链路和权限；尚未接通的跨模块来源在界面明确不可用。

**十五 验证矩阵、现场试点与完成标准**

| 场景 | 必须验证的结果 |
| --- | --- |
| 累计聚合与逐次输入 | 重复上报保留每次真实incoming来源；不同上报内容不被聚合正文替代；幂等重复不重复计数 |
| 持续报警/人工关闭 | 多次ASSERT形成一个可靠周期；ACK/CLOSE不恢复信号；关闭后同信号持续ASSERT即使新告警ID也不虚构新周期 |
| 类型与来源隔离 | FIRE恢复不结束FAULT；规则与部件各有周期；缺部件字段、心跳和网络恢复不当CLEAR |
| 乱序、等时与冲突 | 旧CLEAR不结束新ASSERT；同身份异内容保留冲突；重建遵循已接纳来源语义 |
| 边界与身份 | 窗口前开始、窗口后未结束、无种子、实物更换、信号语义变更显示正确删失/未知，不伪造时长 |
| 时间质量 | 设备时钟未验证、接收/登记时间不同、迟到补录保留来源；不同口径不强行精确关联 |
| 活动与覆盖 | 未报警实际活动进入合格分母；计划/仅报警登记不冒充完整覆盖；一次活动多次报警和多点位去重正确 |
| 有效观察区间 | 停用/离线/缺口重叠只扣一次；无充分区间不计算小时频率；在线不自动代表功能已验证 |
| 比较限制 | 5/50与2/20均10%；U与核实覆盖改变不伪造改善；N=0不可计算；PARTIAL子集不冒充全部；条件不同提示不可比较 |
| 正式记录和复发 | 未建事项/无重开权限仍可独立核实；每轮表单版本冻结；晚到观测、活动/覆盖更正及并发补录使受影响旧快照确认409，窗口外无关事件不失效；更正不覆盖；复发人工开新轮次 |
| 跨租户与范围 | 普通用户无设备范围默认无数据；主子设备分别授权；候选数、附件、导出、提醒、AI均不泄露隐藏资源 |
| 源权限和共享活动 | 有点位权限但无值班/视频来源权限仍不能读正文及派生摘要/AI/报告；部分活动成员授权不读取共享说明；撤权和保留到期分别处理 |
| 配置与模板 | 内置只读、版本冻结、非法字段/脚本拒绝、发布影响完整范围鉴权；旧报告仍能解释所用版本 |
| 附件与关联 | 伪MIME/越界键/任意URL拒绝；上传登记失败补偿；引用附件删除受限；过期/损坏证据明确不可用 |
| AI与提示注入 | 页面打开与报警上报不调用模型；越界工具和虚构factIds拒绝；模型数字不覆盖指标；恶意资料不改工作流 |
| 任务与授权变化 | 持久任务重启/取消/lease fencing正确；发送前和保存前撤权生效；未知外部执行不自动重复调用 |
| 原生产链路回归 | 解析、告警持久化、设备状态、outbox及既有通知语义保留；无自动压制、阈值修改、规则启用或新增设备控制 |
| 真实存储和恢复 | PG迁移/事务/分页、CH历史源、MinIO附件、备份恢复引用及任务恢复分别实测，不以mock通过代替 |
| 前端真实交互 | 实际登录与真实API；宽屏/390px、加载失败、无数据、重复点击、冲突刷新、长时间线和下载均可操作 |
| 现场可用性 | 当班人员可在快捷路径完成必要核实；独立活动登记持续可用；未知选项不迫使编造现场事实 |

周期、分母和权限测试使用独立手工标注的输入/期望样例，不只复述代码实现。生产链路回归核对原告警、设备状态与通知事件实际结果；不为固定文案或纯布局写重复测试。测试包含可靠资料充分和资料不足两类，后者必须稳定产出不可计算/需补证据。

未来实现阶段按修改范围先运行针对包测试，再运行后端`go test ./cmd/... ./internal/...`、前端`npm test`及`npm run build`，并检查`git diff --check`。浏览器用真实业务数据验证；真实PostgreSQL/ClickHouse/MinIO、Harness模型调用、现场设备和生产环境分别记录验证层级与日期，不能互相替代。

现场试点建议覆盖：餐厅A以蒸汽线索为主、餐厅B以烹饪烟雾线索为主、一个施工/清洁/粉尘或其他实际反复报警点位。先收集既有正常工作中的活动与核实，不人为制造危险烟雾或停用报警。由现场负责人确定观察长度与暴露次数，保留环境、设备与措施变化。研究关联仅供安排核查，设施变更按适用要求由有权限人员决定。

完整交付需同时具备：可追溯逐次事实；可配置但有边界的统一模板；独立活动与覆盖记录；单点事项和不可变轮次；确定性周期/指标及不足提示；真实API与页面；手动Harness解读；全链路源权限；附件/提醒/恢复；以及上述有结论的验证记录。必须能仅靠人工记录与确定性分析完成一轮治理，不因模型停用而丢失主流程。

**十六 研究依据与本方案交付边界**

[NIST关于不同烹饪来源与新型烟感表现的研究](https://www.nist.gov/publications/performance-new-smoke-alarms-and-aerosol-measurements-range-nuisance-cooking-sources)及[厨房火灾/扰动报警场景研究](https://www.nist.gov/publications/smoke-alarm-performance-kitchen-fires-and-nuisance-alarm-scenarios)支持把活动、报警器类型和位置作为需要记录与核查的变量。研究有其试验场景与条件，不能直接外推为本平台任意场所的已确认原因，更不能据历史规律认定下一次报警是误报。本方案的字段、周期口径和开发接口属于面向本项目的设计。

本文件最初作为开发方案及通用业务模板交付；下方实施记录说明后续源码改动和本次验证。设计章节不单独证明运行或现场验收，当前操作契约集中维护在 [平台指南](PLATFORM.md#反复报警治理)。

## 实施记录（2026-10-01）

阶段一已实现逐次来源观测、冲突/尝试记录、同事务恢复种子、来源时间桶版本及独立治理仓储基础。生产告警的聚合、部件事务和直接/规则恢复保留 incoming 证据；仓储包装继续传递恢复契约。周期算法区分 REPORT_ONLY、接纳水位、来源版本、删失和监测缺口，历史归一化只读且保留 PARTIAL。

本阶段验证：在 Mac 运行源码测试 `go test ./internal/core ./internal/adapters/memory ./internal/analytics/recurring`；使用 OrbStack `develop` PostgreSQL 的独立 `torchlink_governance_test` 数据库，执行 `go test ./internal/adapters/postgres -run 'AlarmObservation|Migrate' -count=1`，覆盖新观测事务和重复迁移。未启动本机基础服务。此记录只证明当前阶段；页面、正式评价、附件、AI、恢复及现场试点继续按后续阶段验证。

阶段二已实现 R01–R09 的治理业务、版本配置、人工原因/措施/观察门槛、独立现场核实与活动覆盖、确定性指标、历史归一化持久任务、专用 API、来源权限、私有附件及受控历史视频。R12 的固定工作流注册、MCP 绑定及输出 schema 同时接通；AI 执行失败不影响事实和人工流程。历史投影保存来源、cutoff、扫描数、提交批次与缺口，显式选择设备及区间，保留 HISTORICAL_UNRESOLVED/PARTIAL，不写入生产报警链路。

阶段二验证：`go test ./cmd/... ./internal/...` 全部通过；独立 `protocol-packages/gb26875-dahua` 的 `go test ./...` 通过；`node --test deploy/deepseek-harness/gateway.test.mjs` 31 项通过。OrbStack develop 的真实 PostgreSQL 事务/迁移/状态更新/源字段发布/视频历史读取，以及真实 PostgreSQL + ClickHouse 来源测试通过。权限目录支持正常 API 创建治理角色；历史事件读取与影像下载分别授权。重启/fencing、撤权、跨租户/全成员范围、失败回滚和正式记录不可变均有回归。上述测试不代表已完成真实现场试点；最终页面及 Harness 实际调用另记于阶段三。

阶段三包含 R10/R11/R13 的 Vue 页面与人工采纳入口，以及 R14 的治理待办、保留/维护、监控与 FULL v4 恢复。正式报告展示其固定分析版本及前后指标，分页事实、来源引用和证据可复查；治理页面不改生产报警处置。FULL v4 实测在独立 PostgreSQL schema 和恢复 MinIO 重建治理历史/外键/索引、固定分析与对象引用，并校验 SHA-256；上传补偿任务在恢复副本终结，不清理源环境对象。

本次验证层级与结论：

| 矩阵范围 | 本次结论与定位 |
| --- | --- |
| incoming、周期、来源/类型隔离、乱序冲突、删失/时钟 | 通过 core、memory、postgres、recurring 包回归；真实 PG/CH 测试覆盖事务水位及三种时间来源隔离 |
| 活动分母/覆盖、有效区间、5/50与2/20、U、不足与不可比较 | 通过 `recurring/metrics_test.go` 与固定任务回归；REPORT_ONLY 不输出物理周期零值作为正常 |
| 人工正式记录、配置冻结、更正、复发与并发过期 | 通过 `alarmgovernance/service_test.go`、source/evidence 回归及真实 PG 状态转换；实物身份未确认与条件不同分别说明 |
| 租户/全成员范围、来源撤权、模板/字段绑定 | 通过治理 HTTP、事务授权和来源发布回归；权限目录可实际保存角色；视频源校验原/当前设备与独立历史读取/下载动作 |
| 附件、损坏/缺失、补偿与保留 | 通过治理证据及补偿回归；正式引用不物理删除；受控视频对象缺失410与临时网络503分别返回 |
| 持久任务、取消、lease/fencing、服务替换 | 历史投影回归及 race 通过；真实 PG 更换 worker 后从固定 manifest/checkpoint 接管，旧 token 不能提交；AI 未知执行不自动重发 |
| 原生产链路 | 后端全测试及独立 GB26875 module 测试通过；未增加报警压制、规则自动发布或设备控制 |
| 存储/备份 | develop VM 的 PostgreSQL、ClickHouse 来源及 MinIO FULL v4/独立恢复通过；不以对象下载代替恢复结论 |
| Vue 页面与真实 API | 前端 `npm test` 186 项及 `npm run build` 通过。真实登录/PG API 验证模板来源发布、历史投影显式引用、未知独立核实、活动/覆盖、原因/措施/观察、资料不足正式评价、精确报告/下载、复发新轮及旧报告保留；宽屏/390px、私有附件、值班及受权历史视频均通过 |
| 手动 Harness 与事实引用 | develop VM 加载当前完整策略后，真实模型任务 `98f1312b-bc94-426e-80f1-f19df506d101`（run `5bdc8718-687d-4293-b4b9-535c2cfb0f3a`）为 SUCCEEDED。八组结构持久化、页面展示、固定 factIds 精确读取均通过，浏览器未捕获 JS 异常及治理 HTTP 失败为零。仅用户按钮触发；固定快照读取不启动 AI |
| 现场设备及生产验收 | 未执行。浏览器隔离数据属于明确标注的受控合成事实；餐厅A/B及非餐厅现场活动持续登记、真实硬件时钟/功能覆盖和目标生产升级仍须现场人员验收 |

开发环境为 Mac 源码进程与 OrbStack develop VM 基础服务。测试只使用独立数据库/schema/对象前缀；未在 Mac 部署基础服务，也未改动原主工作区的其他开发任务。

阶段三最终检查：`go test ./cmd/... ./internal/...` 全部通过，HTTP 包 67.760s；新增精确事实接口的固定快照、跨范围撤权与读取中撤权回归及 race 通过。独立恢复已通过真实 PostgreSQL/MinIO 联调。实际 AI 排查确认临时旧镜像工具策略未更新，加载完整当前部署文件后恢复；治理八组结果在共用 4096 token 预算下出现 max-tokens，改为有界 8192 和简短分组输出（prompt v2）后实际调用与浏览器复测通过。平台失败只暴露受控类别和中文说明，不输出上游秘密。`git diff --check` 通过。
