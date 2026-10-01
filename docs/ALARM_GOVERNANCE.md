# 反复报警核查与治理

[使用流程](#使用流程) · [模板与预设](#模板与场景预设) · [事实与周期](#逐次事实与报警周期) · [活动与评价](#活动覆盖与观察评价) · [AI](#ai-解读) · [权限与附件](#权限附件与关联) · [API](#api-与开发入口)

入口为“运行监控 → 反复报警处理”。按单一点位、原始报警类型、来源语义及信号键建立事项，整理独立现场核实、实际活动、原因、措施与固定观察评价。每次新报警仍沿告警中心的原有流程处置，治理记录、历史原因和 AI 内容不改变生产告警性质。

## 使用流程

1. 从具体上报独立登记现场核实，不要求先有治理事项或 ACTIVE 值班班次。填写实际核实时间、核实方式、检查范围、现场结果、活动关系、设施状态和证据；登记时间由服务端保存。无法判断的内容保持未知。
2. 独立登记实际发生的活动，包括没有报警的活动，并声明活动类型、单位、覆盖区间、设备范围和登记完整性。计划和实际活动分别保存；实际时间重叠只提供候选，不自动确认关联或原因。
3. 负责人选择已发布的通用模板、场景预设和报警类型配置，建立事项首轮，人工关联明确的上报及核实成员。原始类型、设备/部件和信号含义保持独立，同一信号只允许一个活动事项。
4. 按“待核查 → 核查 → 改善 → 观察 → 本轮完成”推进。原因候选、确认、争议与否定保留证据和人工签名；措施先记录实施，需要专业验收时再由获授权人员独立验收。
5. 观察前确认前后窗口、实物与现场条件、实际活动单位、准入/排除、最低监测时长与活动数。有效监测区间需要人工核实依据，通信在线不能替代可靠监测。
6. 运行固定事实分析，以观察计划版本、快照、`factsHash`、`dataRevision` 和来源时间桶版本保存正式评价。迟到事实、补录或更正使待确认评价过期，旧报告保留；重新分析产生新版本。
7. 本轮结束须引用已确认评价并记录后续责任。资料不足或未改善也可结束本轮，完成不表示风险解除。完成后新上报产生待复核提示，由人员决定是否重开新轮，旧原因不自动沿用。

固定分析显示实际阶段与已处理数量，支持显式停止；刷新或重新打开后恢复服务端进度。

事项状态为 PENDING_INVESTIGATION、INVESTIGATING、IMPROVING、OBSERVING、COMPLETED、CANCELLED；轮次另存 ACTIVE、COMPLETED、CANCELLED。正式记录通过追加更正保存，原版本、人员、时间和替代关系保留。实物或信号语义改变时建立关联新事项，不把新对象并入旧结论。

问题发现条件逐项启用并填写现场阈值，包括接纳后的上报数量、可靠新开始、持续未解除及完成后的新增事实。阈值只决定核查工作入口，没有通用消防阈值；同对象命中多个条件合并候选，不自动建事项或确认成因。`TriggerCount` 表示生产告警上报负担，不能当作独立周期或误报数量。

## 模板与场景预设

| 配置层 | 当前职责 | 版本边界 |
| --- | --- | --- |
| 通用模板 | 现场结果、活动关系、设施状态及补充字段 | 三个核心字段受保护，不能删除未知或争议选项 |
| 场景预设 | 活动类别、环境选项、设施问题和所需资料 | 提供取证问题，不预填原因、正常或已恢复 |
| 报警类型配置 | 产品/设备范围、原始类型、来源、信号键、周期方法、时间与恢复语义、资料依据 | 可靠周期配置须明确断言和恢复证据；默认 REPORT_ONLY |
| 治理轮次 | 实际采用的三类版本 ID 与 hash、点位身份和位置 | 新配置不回写旧轮次，历史按原版本读取 |

内置配置只读，可复制为租户 DRAFT，校验后发布为 PUBLISHED；RETIRED 不用于新建，历史仍可读。字段控件限文字、单选、多选、时间、有限数值和附件，选项使用稳定代码。

源字段从已成功处理的样本目录选择 `properties.<key>` 或 `event.<key>`。发布时校验来源设备、产品、字段类型与枚举并保存签名；不接受脚本、SQL、表达式、任意路径或 URL。现场结果、活动关系与设施实际状态继续由人员填写。

| 内置场景 | 默认主要活动类别 | 可补充的取证例子 |
| --- | --- | --- |
| 餐饮厨房 | COOKING | 厨房内或相邻区域、实际烹饪/清洁区间、排风观测、型号与专业检查 |
| 高湿与洗衣 | STEAM | 清洗/烘干/热水活动、通风观测、测点位置与环境适用资料 |
| 施工装修 | CONSTRUCTION | 切割/焊接/打磨记录、设施受限、点位与线路检查 |
| 车库车辆 | VEHICLE | 启动/充电/检修区间、车辆与报警位置、通风和独立核实依据 |
| 机房配电 | ELECTRICAL_WORK | 负载变化、设备启停、测点与工况、专业检测和维修资料 |
| 泵房水系统 | WATER_MAINTENANCE | 泵/阀/补水/试验记录、物理系统、压力或液位证据 |
| 通用场所 | OTHER | 信号含义、准确位置、自定义实际活动、现场核实与适用资料 |

所有场景还保留 OTHER、UNKNOWN、DISPUTED，环境默认有蒸汽、粉尘、未知、争议。上表细活动是可按实际需要添加的取证例子；内置预设没有预先包含全部细选项。厨房与邻近餐区/走廊分别建档，活动重叠与场所名称不能证明此次报警的原因。

现场结果区分发现/未发现火情、发现/未发现目标异常、其他异常、无法确定和争议；“未发现火情”不会自动升级为确认扰动或故障。核实方式为 ON_SITE、PHONE、DOCUMENT_REVIEW、UNVERIFIED。资料核查可以确认记录，但不作为完整现场核实；未知记录可以保存，不自动满足原因确认要求。原因状态、现场结果与生产告警状态分别保存。

## 逐次事实与报警周期

生产报警事务保留每次 incoming、明确 CLEAR、实际接纳结果、过旧输入、源身份冲突及部件状态；重复源身份不新增合法观测，相同身份不同内容保留冲突。同一输入的多个合法部件/信号槽位分别记录，不能只按内容 hash 删除可能的新上报。治理分析读取事实，不重投生产队列或修改生产计数。

设备时间 `eventAt`、可靠原文接收时间 `receivedAt`、业务登记时间 `recordedAt`、持久确认时间 `availableAt`、实际规则求值时间 `evaluationAt` 分开保存。缺失或未验证时钟、来源历史、实物身份与采集覆盖均保留限制。原文导航使用 `rawMessageId`。

| `cycleMethod` | 所需证据 | 可计算范围 |
| --- | --- | --- |
| COMPONENT_BOOLEAN | 同一部件/类型的明确状态、接纳结果及可靠起点 | 部件正常 → 断言 → 明确恢复的信号周期 |
| DIRECT_EXPLICIT_STATE | 同一设备信号的明确 ASSERT/CLEAR 语义与关联键 | 设备显式信号周期 |
| RECORDED_RULE_LIFECYCLE | 保存的规则版本、条件 hash 和实际 evaluation 时间 | 已记录规则周期，单独标明其来源 |
| REPORT_ONLY | 只有报警上报，没有可靠恢复定义 | 合法去重上报与时间分布；新开始、物理周期和可靠持续时长不可计算 |

确认/关闭生产告警、报文没有该字段、心跳、重新上线或更换告警 ID 都不代表 CLEAR。持续 ASSERT 属于同一未恢复信号；较旧 CLEAR 不结束较新 ASSERT。设备、部件、报警类型、规则与信号来源分别分组。

窗口为 UTC `[start,end)`。窗口前已经报警、窗口后仍未恢复分别标记左/右删失；完整恢复周期、窗口内可靠新开始、未结束数量、已知时长下界和未知边界分别展示。缺口前后两个 ASSERT 不证明中间持续报警。状态为 OPEN、CLEARED、TERMINATED_UNKNOWN，后者表示无法继续追踪，不表示恢复。

高级区域可以为明确设备和区间创建独立历史归一化任务，显式选择 PostgreSQL 或 ClickHouse 来源、`historicalSources` 和读取条数。页面显示扫描输入、归一化事实、已提交批次和缺口，支持持久租约恢复与取消。历史成功解析输入仅在用户选择后读取为 PARTIAL 投影，不补造过去的规则判断、接纳结果或物理边界。

投影结果存为不可变 AnalysisOutput 和任务/快照/hash，与生产观测台账分开；新分析必须明确引用并完整覆盖其设备集合。ClickHouse 缺原标准消息或接纳元数据时披露限制，各来源记录独立截止点，不声明全历史完整。

## 活动覆盖与观察评价

| 登记覆盖 | 含义 |
| --- | --- |
| FULL_DECLARED | 声明区间内全部实际活动均已登记，仍需可评价观测和核实依据 |
| PARTIAL | 仅登记子集，不能当完整分母 |
| ALARM_ONLY | 只登记报警关联活动，不能计算所有活动出现报警的比例 |
| UNKNOWN | 登记覆盖无法确定 |

活动单位、划分方式与实际起止固定，不把一段持续作业拆成大量次数。统计展示 N（符合准入的实际活动）、C（至少一次经完整核实确认相关）、U（关联尚未完成核实），并列出识别区间 `[C/N,(C+U)/N]`。同一活动内多次上报不重复增加活动数；N 为零或覆盖不足时不输出完整比例。

有效监测小时采用已核实区间的并集，扣除测试、停机、离线、采集缺口和未知时钟区间。前后实物、位置、类型配置、规则、协议或工况变化分段，保留混杂及不可比较原因。活动完整覆盖、最低资料或前后准入不满足时不确认改善；无可靠周期边界时不输出周期发生率。

评价使用 IMPROVED、NOT_IMPROVED、WORSENED、INSUFFICIENT_DATA、NOT_COMPARABLE，保留系统结论与限制，由人员确认。报警减少只能描述本次可比观察，不能独立证明消防能力、因果效果或未来安全。

## AI 解读

用户点击“开始解读”后启动只读 `recurring-alarm-analyst` Harness 工作流，固定事实与人工流程不依赖 AI 成功。每次发送、工具读取及结果保存复核当前权限和快照；事实引用可以单独打开，不要求先启动 AI。

| 输出字段 | 内容 |
| --- | --- |
| facts、patterns | 已提供事实与观察到的模式 |
| hypotheses | 候选解释、支持与冲突证据 |
| checks、measures | 需人工核查及评审的措施建议 |
| observation | 后续观察建议 |
| limitations、summary | 实际资料限制与简述 |

逐项 `factIds` 只能引用本次提供或绑定工具读过的事实，数字通过服务端 `metrics` 的 `metricRefs` 呈现。工作流上限为 8192 token，输出结构及引用由服务端校验；模型不能写回正式原因、验收或业务结论。备注、报文、知识和附件元数据作为资料读取，不作为执行指令，首期不向模型发送附件图像或任意 URL。

## 权限、附件与关联

菜单为 `menu:alarmGovernance`，普通业务对象还需设备和告警查看权限。`action:alarmGovernance:` 下的 record、cases、cause、measures、acceptance、observation、complete、reopen、templates、publish、analyse、ai、export 分别控制记录、事项、原因、措施、专业验收、观察、完成/重开、模板、发布、事实分析、解读和导出。负责人身份不授予额外设备或来源权限。

附件只接受私有 JPEG/PNG/WebP/PDF，实际 MIME 与扩展名一致，最大 16 MiB，服务端生成对象键并保存 SHA256。下载重新鉴权和校验摘要；正式证据不物理删除，撤回、缺失与损坏以新的可用性版本保存，原元数据/hash与审计保留。证据到期或损坏显示不能复查，源权限撤销则整份相关派生正文、AI 与报告拒绝读取。

跨模块目前可关联值班事项、值班记录和历史 VIDEO_EVENT，按来源菜单和完整设备成员读取。维修和规则验证缺受控详情适配时明确不可关联。历史视频还需摄像头菜单或按设备授权的 `action:cameras:history`，影像下载另需 `action:cameras:download`，不取得直播权限。摄像头换绑后同时校验发生时原设备与当前设备；旧绑定未知只允许全部设备范围读取，失效影像仍保留元数据，不代理任意 URL。

治理待办与消防告警分开。措施逾期、待验收、观察资料不足及完成后新增待复核事实按当前接收人权限生成，可持久标记已读/已处理；同类重复事实合并计数，不启动 AI。

## API 与开发入口

接口前缀为 `/api/v1/alarm-governance`。写入携带 `expectedVersion` 和 `idempotencyKey`，租户/用户由登录身份确定；UTC 时间使用 Unix 毫秒。列表默认 20、上限 100，后台创建返回 202 及持久任务 ID，版本或幂等冲突返回 409。

| 路由族 | 当前入口 |
| --- | --- |
| 配置 | source-fields、templates、scene-presets、type-profiles；`:id/revisions/:revisionId` 的 validate/publish/retire |
| 事项与轮次 | cases；`:id/start`、assign、confirm-identity、start-observation、complete、cancel、reopen；`:id/rounds`、reports、events |
| 核实与关联 | observations、verifications；confirm/corrections；rounds/:id/alarm-links、verification-links |
| 活动与原因 | activities、activity-coverages、causes；活动确认/版本、原因 confirm/dispute/reject/corrections |
| 措施与评价 | measures 的 start/implement/verify/cancel/corrections；observation-plans 和 observation-reviews 的 confirm |
| 固定分析 | runs 及 snapshot、metrics、findings、observations、cycles、verifications、activities、coverages、measures、observation-results、activity-candidates、evidence；`:id/stop` |
| 精确事实与 AI | runs/:id/facts/:factId；runs/:id/ai-jobs 及作业查询/停止 |
| 历史归一化 | historical-projections 及 `:id`、stop、snapshot、observations |
| 附件、关联、提醒 | cases/verifications/activities 的 attachments；attachments/:id/download；business-links、video-events、reminders |
| 报告 | reports/:id、`:id/analysis`、`:id/export` |

以下是保存独立核实草稿的正文示例，ID 应替换为当前授权点位、实际观测及已发布配置。未知结果只表示资料不足，之后确认还须满足配置要求。

```json
{
  "deviceId": "device-example",
  "componentId": "component-example",
  "alarmType": "FIRE",
  "originKind": "COMPONENT_STATE",
  "signalKey": "smoke-alarm",
  "templateRevisionId": "template-general-v1",
  "scenePresetRevisionId": "scene-kitchen-v1",
  "typeProfileRevisionId": "profile-report-only-v1",
  "observationIds": ["observation-example"],
  "verificationMethod": "ON_SITE",
  "verifiedAt": 1790824200000,
  "fieldResult": "UNABLE_TO_DETERMINE",
  "activityRelation": "SUSPECTED",
  "fieldValues": {"facilityStatus": "UNKNOWN"},
  "activityIds": [],
  "description": "到场时见蒸汽；排风运行情况尚未核实。",
  "attachmentIds": [],
  "expectedVersion": 0,
  "idempotencyKey": "verification-example"
}
```

源码入口为 `internal/alarmgovernance/`、`internal/analytics/recurring/`、`internal/httpapi/alarm_governance*.go`、`internal/model/alarm_governance.go`、`internal/adapters/postgres/alarm_governance_schema.sql` 和 `iot_front/src/views/AlarmGovernanceView.vue`。数据库沿已有启动迁移；资源限制复用 [业务分析任务配置](DEPLOYMENT.md#业务分析任务)。治理历史、来源修订、关联、提醒、持久分析/AI 任务和附件纳入 [FULL 备份与隔离恢复](BACKUP.md)。恢复后未知外部 AI 请求不自动重发，附件映射到独立恢复前缀。

专项运行条件见 [业务分析与治理回归](DEVELOPMENT.md#业务分析与治理回归)。模拟和隔离样本不能替代现场试点；实际填表量、核实覆盖、来源覆盖与措施前后可比性需按现场资料另行验证。
