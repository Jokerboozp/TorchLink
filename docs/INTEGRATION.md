# 设备接入、协议与开放接口

[设备接入](#设备接入) · [模板验收](#设备模板准备验收与更新) · [草稿与批量](#持久接入草稿与批量登记) · [标准报文](#http--mqtt-标准报文) · [Go 协议](#go-协议) · [解析预览](#协议解析预览) · [映射](#配置驱动协议) · [主子设备](#tcp-与主子设备接入) · [消息主题](#消息主题管理) · [开放接口](#开放接口) · [协议示例](#内置协议示例)

## 设备接入

接入主线为：模板准备（协议选择或开发 → 公共连接 → 首台真实验收）→ 日常单台或批量接入 → 诊断与升级。

实施人员从“设备模板 → 新建设备模板”连续完成模板信息、协议、公共连接与验收规则、首台实机验证。日常人员从“设备管理 → 添加设备”选择已经可以复用的模板，填写单台差异或导入批量清单，再完成现场配置与验证。模板代表共用协议的一套配置；共享监听、查询计划与子设备类型映射在模板中准备，主动连接地址和 Modbus 站号属于单台设备。页面统一称“设备模板”，接口沿用 `Product` / `productId`；独立 Gateway 是部署进程。

- 标准 HTTP/MQTT 选择内置标准协议；HTTP 自定义报文绑定已发布映射或 Go 协议，调用 `POST /api/v1/device-ingest/{deviceId}`。这两类设备使用 AccessKey/Secret，Secret 只首次创建或轮换返回。
- Go TCP/UDP 需要 ingress 能力，选择共享监听或 TCP 主动连接；设备编号必须与协议识别一致。Modbus 使用已发布点表版本，填写地址、端口和站号。TCP/UDP、Modbus 与子设备不生成平台 Secret，协议负责其自身认证。
- 子设备从主设备详情添加，地址和类型须匹配接入点中的 `childProducts`；沿用主设备连接，按租户/主设备/地址幂等登记。主子设备在权限上分别授权。
- 新建模板、设备或接入点需要对应操作权限和全部设备范围。设备名、位置与业务标签在台账维护；运行状态来自解析上报，不因保存配置而变成在线。

## 预检、保存与诊断

- `GET /api/v1/onboarding/preflight?productId={id}` 返回已有模板的接入方式、协议摘要、可用共享监听（含运行状态）和检查项。`canRegister` / `ready` 表示配置允许登记，`connectionReady` 表示接入前置条件齐备，`reusable` 表示模板有匹配当前配置的实机验收记录；日常登记还要求 `reusable=true`。缺少平台对外地址允许保存待配置记录，但不表示已经接通。新模板用 `protocolPackageId`、`transport`、`category` 代替 `productId`。
- `POST /api/v1/onboarding` 在一个事务中保存设备，并按需创建模板、协议绑定和接入点，任何一步失败都不留下部分资源。请求需带客户端生成的 `requestId`；`connection.mode` 取 `standard`、`managed`、`listener`、`dial` 或 `poll`，须与预检给出的接入方式一致。请求结构见 [onboarding/enroll.go](../internal/onboarding/enroll.go)。

幂等按租户和设备编号判断：首次成功返回 201；相同请求重试返回 200 和 `reused:true`，不再返回 Secret；同一编号的不同请求返回 409。并发的相同请求只创建一台设备、生成一个 Secret。共享监听端口已被占用，或模板、协议绑定在保存前发生变化时返回 409，刷新后重试。主动连接和 Modbus 目标若填写 IP，须在 `IOT_MODBUS_ALLOWED_CIDRS` 允许的网段内。

添加设备使用“新增设备”（`POST /api/v1/device-registry`）操作权限，不单列权限项。同时新建模板还需要设备模板的新增权限，新建共享监听还需要平台接入点的新增权限。通过 `trial:true` 登记首台验证设备或容量测试设备，还需设备模板配置权限（`PUT /api/v1/products/:id`）；缺少该权限返回 403。登记和测试上报不会自动生成模板验收记录，已有模板未通过首台实机验证时，普通登记仍返回 409。容量模块的部署和测试设备说明见[容量测试模块](DEPLOYMENT.md#容量测试模块)。只授权部分设备的账号不能添加设备，也不能调用预检。

旧台账新增和发现设备确认入口也复用同一原子登记及模板验收检查。已有设备在原模板中的资料编辑不受首台准备状态阻断，但不能通过台账 `PUT` 或相同设备编号的 `POST` 直接更换模板，尝试时返回 409，并提示从目标模板重新接入；原设备归属、连接和凭据保持不变。需要单台主动连接参数或多个共享监听需要选择时，旧入口提示转入设备接入流程。

`GET /api/v1/device-registry/{id}/connection` 返回 `diagnosis`，包括 `stage`、`tone`、`title`、`nextAction` 和分项 `checks`。结论按“模板与协议 → 设备 → 主设备 → 接入点 → 对外地址 → 解析 → 模板验收条件”的顺序给出最先需要处理的问题。传入 `since` 时只采信该时间之后的现场原文。以下来源计为现场数据：`device-http`（凭据认证的 HTTP 接口上报）、`standard-http`、`standard-mqtt`、Go 协议监听和 Modbus 采集；管理端测试报文和回放不计入。多接入点设备可用 `?profileId={id}` 选择。原文详情、下载和回放使用 `rawMessageId`，不是标准消息 ID。

## 设备模板准备、验收与更新

模板启停与准备状态相互独立。没有历史验收的既有模板显示 `UNVERIFIED`，已有设备、凭据和运行协议继续保留，不伪造验收记录。准备状态为 `DRAFT`、`AWAITING_VALIDATION`、`READY`；当前运行配置与已验证指纹不一致时返回 `CONFIGURATION_CHANGED`。只有启用且为 `READY` 的模板可用于日常单台或批量登记。模板工作人员可显式通过 `trial:true` 添加首台验证设备；这台设备验证后直接保留，无需删除重建。

| 接口 | 行为 |
| --- | --- |
| `GET /api/v1/products/{id}/preparation` | 当前配置、候选、修订、准备状态、影响设备数量、验收和历史 |
| `PUT /api/v1/products/{id}/preparation` | `{revision,candidate}` 保存可恢复候选草稿，允许尚未填完，拒绝明文凭据 |
| `POST /api/v1/products/{id}/preparation/trial` | `{revision,profiles?}` 创建独立模板和监听；`profiles` 只覆盖隔离试验地址与端口 |
| `POST /api/v1/products/{id}/preparation/apply` | `{revision}` 明确应用经过校验的候选；已有设备的配置变更须先通过隔离实机验证 |
| `POST /api/v1/products/{id}/preparation/rollback` | `{revision,targetRevision}` 恢复历史完整协议、共享连接、数据定义与验收规则 |
| `POST /api/v1/products/{id}/verification` | `{deviceId}` 从服务端真实证据生成模板验收结果 |
| `GET /api/v1/device-registry/{id}/verification` | 检查设备当前配置的真实证据，不写验收记录 |
| `POST /api/v1/device-registry/{id}/verification` | 检查并持久保存该设备的验收记录 |

候选包含 `product`、`protocolId`、`version`、共享 `profiles` 和 `verificationRules`。协议选择以已发布的准确版本为准；任意自定义 MQTT Topic/认证不属于现有托管接入能力，自定义协议标记 MQTT 时仍按预检明确的 HTTP 通道接入。公共表单仅暴露现有 Worker/传输支持的字段。

验收规则包含 `mode`（`periodic`、`low_frequency`、`event`、`child`）、`minMessages`、`windowSeconds`、`maxGapSeconds`，以及可选的 `requiredMessageTypes`、`requiredProperties`、`requiredEvents`。`maxGapSeconds=0` 表示不检查间隔；事件型须指定事件标识或消息类型。数量为 1–100，窗口最大 30 天。后端只统计当前租户、设备、模板、准确协议版本、连接及配置生效时间之后的现场原文，并检查已持久化的标准消息；主子设备还需匹配当前父设备、地址及类型。模拟、回放、未知版本、旧配置证据均不能使验收通过。

更新不会在上传源码或保存草稿时切换生产设备。完整配置、协议绑定和准备修订在同一数据库事务中保存，并检查读取时的产品、共享连接和流程修订。已有设备升级使用独立试验模板/端口；试验配置的指纹固定，事后改成另一协议或规则的试验结果不能放行原候选。正式应用或回滚后需要新的正式现场证据，不沿用试验地址的验证成功。实际使用的 HTTP / MQTT 对外地址也参与配置指纹，地址变化需要重新验收；名称与说明修改不会撤销匹配的验收结论。

旧接入点接口也在事务内检查模板绑定与使用情况，不能直接修改已使用模板的共享监听或改变接入点归属。单台主动连接、Modbus 参数仍可在设备诊断中纠正。仅关闭共享接入点、且不同时改变其他配置时允许立即停止；重新启用及公共配置变更须回到模板准备流程。

历史修订同时保存当时的正式验收及隔离试验证据，包含设备、协议版本与原文编号。当前 `applied.trialVerification` 仅表示应用前的试验依据，不表示正式设备已经完成验收。TCP/UDP、Modbus 及子设备原文保存网关实际使用的 `metadata.profileFingerprint`；验收要求它与当前连接配置一致，防止旧连接在配置切换后迟到的同版本数据被误算为新配置成功。缺少该字段的历史原文仍可查看和回放，但不自动补作新验收。

`applied.runtimeStatus=PENDING` 表示配置已保存、尚未得到当前配置的现场确认；`FIELD_CONFIRMED` 表示验收证据确认实际版本。监听服务的 `LISTENING` / `CONNECTED` 不代表所有会话已经切换。Go 协议继续在完整帧与待应答命令边界选择版本，原文保存实际使用版本。Modbus 已绑定模板按绑定版本进行后续轮询；单轮读取仍固定本轮版本，历史无绑定实例保持其原配置。回滚不重发物理控制命令，也不改写历史原文。

所有复合动作重验其实际需要的权限。模板准备不能授予发布协议、创建共享监听或登记设备权限；普通用户仍受设备范围限制。未创建模板的草稿使用账号隔离的持久接入草稿，已创建模板的候选使用修订号进行并发校验，过期保存返回 409。

## 持久接入草稿与批量登记

接入草稿按租户、用户持久保存，带 `revision` 乐观锁；创建传 `revision: 0`，更新传读取到的版本，过期版本返回 409。普通设备草稿要求设备登记权限与全设备范围；`preparation:*` 模板准备草稿使用独立的模板编辑权限，并检查完整设备范围。列表先按当前权限过滤再分页；更改权限后不能继续读取另一类草稿，也不能靠修改 `step` 改写原来无权访问的草稿。草稿只保存配置与普通表单，不保存密码、令牌或设备 Secret。

| 接口 | 用途 |
| --- | --- |
| `GET /api/v1/onboarding/drafts` | 查询自己的可访问草稿，支持 `limit`、`offset` 和 `purpose=preparation\|device`，先按类型与权限过滤再分页 |
| `GET /api/v1/onboarding/drafts/{id}` | 恢复草稿及其 `revision` |
| `PUT /api/v1/onboarding/drafts/{id}` | 保存 `{revision, step, productId, request}`；`request` 使用设备登记字段，允许尚未填完 |
| `POST /api/v1/onboarding/batches/preflight` | 对 `{productId, connection, rows}` 只读预检，返回逐行错误和配置指纹 |
| `POST /api/v1/onboarding/batches` | 提交相同输入，另带客户端任务 `id` 与预检 `fingerprint` |
| `GET /api/v1/onboarding/batches` | 查询自己的任务摘要 |
| `GET /api/v1/onboarding/batches/{id}` | 查询登记汇总与最多 30 行结果，支持 `limit`、`offset` |
| `POST /api/v1/onboarding/batches/{id}/retry` | 用 `{revision, indices}` 重试失败行或恢复因权限变化暂停的待处理行 |
| `POST /api/v1/onboarding/batches/{id}/credentials` | 用 `{indices}` 一次领取选中行的设备密钥；省略索引领取本批可领取项 |

每个 `rows` 项为 `{device, connection?}`：`device` 包含编号、名称、备注等设备差异；行内 `connection` 完整覆盖顶层公共连接。单任务最多 1000 台、输入最多 2 MiB。共享监听应先在模板准备中建立，批量不能逐行新建共享端口。模板须已完成首台验证；预检和提交内容变化、协议或公共连接配置变化均需重新预检。登记任务固定模板配置指纹，并在每台设备执行前重新核对当前权限与配置；失败重试复用原始幂等请求，不重复创建设备。模板配置变化后的未完成设备应另建任务，不能静默换用新配置。

任务和行结果存于 PostgreSQL 的 `onboarding_record`；关闭浏览器不会停止任务，进程恢复后可继续。具备 `jobs` 角色的进程运行内部全租户待执行队列，每个任务持有数据库执行租约，每进程最多同时运行两个任务，每个任务逐行登记。`INITIALIZING`、`QUEUED`、`RUNNING` 表示登记仍在执行；`COMPLETED` 仅表示本批登记完成，`PARTIAL_FAILED` 保留失败行，`PAUSED` 表示账户、权限或模板配置变化。

设备登记状态和真实验收状态分开：分页行的 `onboardingStatus` 表示待配置、待验证、已验证或异常；`verificationSummary.scope` 固定为 `page`，只汇总本页。列表读取已保存的设备验收记录，核对当前模板指纹及设备、连接配置更新时间，不对整批设备反复扫描原文。重新验收从设备详情触发；没有匹配的现场证据不能标为已接入。行 `accessInfo` 可导出现场所需地址、Topic、设备编号或站号，不含 Secret。

批量 Secret 使用由平台 JWT 密钥按独立用途派生的 AES-GCM 密钥加密，绑定租户、用户、任务行，领取窗口为 15 分钟；领取通过 CAS 清除密文，后台也会清理到期密文。普通草稿、任务查询和审计日志不返回明文 Secret；领取响应设置 `Cache-Control: no-store`，返回的 `items` 包含一次性凭据与现场配置。重复领取、领取响应中断、密钥更换、设备凭据被重置，或登记成功后交付结果落盘失败，都不会伪造或恢复旧 Secret；按 `unavailable` 原因从设备详情显式重新签发，并按现有凭据撤销流程使旧凭据失效。新密钥须同步配置到现场设备。

## HTTP / MQTT 标准报文

标准协议固定为 `iot-standard@1.0.0`。推荐 envelope 包含 `version:"1.0"`、唯一 `id` 和正数毫秒 `timestamp`，按上报类型提供以下字段：

| kind | 业务字段 | 标准消息类型 |
| --- | --- | --- |
| `property` | 非空对象 `data` | `PROPERTY_REPORT` |
| `event` | 顶层 `event` 名称，可附 `data` | `EVENT_REPORT` |
| `alarm` | 非空对象 `data`，建议含 `alarmType`、`alarmLevel`、`content` | `ALARM_REPORT` |
| `state` | 顶层 `online` 布尔值 | `STATE_CHANGE` |
| `command-reply` | 顶层 `commandId`、布尔 `success`，可附 `data` | `COMMAND_REPLY` |

例如（时间戳应替换为设备实际时间）：

```json
{"version":"1.0","id":"msg-001","timestamp":1789000000000,"data":{"temperature":26.5}}
```

```json
{"version":"1.0","id":"state-001","timestamp":1789000000000,"online":true}
```

```json
{"version":"1.0","id":"event-001","timestamp":1789000000000,"event":"self-test","data":{"result":"ok"}}
```

```json
{"version":"1.0","id":"alarm-001","timestamp":1789000000000,"data":{"alarmType":"FIRE","alarmLevel":"CRITICAL","content":"3 层烟感报警"}}
```

请求体最多 64 KiB、嵌套最多 16 层；重复 JSON 键、未知 version 和超前平台时间五分钟以上的设备时间被拒绝。历史时间可用于补传，晚到消息不回退最新状态。不带 version 的旧 `data.connectionStatus`、`data.commandId` / `data.success` 格式仍可读取；顶层和 data 同时提供的对应字段不能冲突。

普通 event 名称不会自动变为设备告警。`alarm` 上报或协议直接输出 `ALARM_REPORT` 时形成设备来源告警：匹配规则时按规则处理，未匹配规则也保留告警；`alarmLevel` 取 `CRITICAL`、`HIGH`、`MEDIUM`、`LOW`、`INFO`，缺省为 `HIGH`，`alarmType` 缺省为 `MANUAL_ALARM`。告警内容取首次触发报文的 `content`（也认 `alarmContent`、`description`、`alarmDesc`，最多 500 字）；规则告警的报文没有内容时用规则说明或规则名，部件告警为“部件名（位置）”；同一告警再次触发不改写内容。恢复按告警类型读取属性：`FIRE` 用 `fireAlarm:false`，`SMOKE_DETECTED` 用 `smoke` / `smokeDetected`，`DEVICE_FAULT` 用 `fault` 等故障项，`DEVICE_OFFLINE` 用 `offline`，`MANUAL_ALARM` 用 `alarm`；其他类型没有对应恢复属性，须通过[开放接口](#开放接口)的 `RECOVERED` 处置或在控制台关闭。部件状态使用 `data.components`，具体火警、故障与恢复契约见 [部件告警](#部件状态契约)。

### HTTP 上报

```http
POST /api/v1/device-ingest/standard/{tenant}/{product}/{device}/{kind}
Content-Type: application/json
X-Device-Key: <设备 AccessKey>
X-Device-Secret: <设备 Secret>
```

成功返回 202、`messageId`、`created` 和 `status:ACCEPTED`，表示原始接收链路接受请求，异步解析和规则结果须继续查询。标准凭据不能通过旧 RawMessage 接口自选租户、Parser 或协议。

同一租户、产品、设备、kind 和消息 ID 的重试须保持正文逐字节一致；HTTP/MQTT 重传使用同一原文 ID。不同正文返回 `409 MESSAGE_CONFLICT`，新的读数使用新 ID。每台设备上报限流为每秒 20 次；配置 Redis 时多副本共享额度，未配置时按进程计算（见 [跨实例一致性](DEPLOYMENT.md#按设备业务流与跨实例一致性)）。凭据校验时数据库暂不可用返回 `503 UNAVAILABLE` 与 `Retry-After`，设备稍后重试即可，不应视为凭据失效。平台解析与存储积压超过 `IOT_INGEST_MAX_BACKLOG`（默认 50000）时暂停接收新原文，HTTP 返回 `429 BACKPRESSURE` 与 `Retry-After`，积压降到 80% 以下恢复。

### MQTT 上报

携带上述两个凭据头调用 `POST /api/v1/device-mqtt/token`，用返回的 `username` 和 `token` 作为 MQTT 用户名和 password，Client ID 使用接入指南给出的值。Broker 在令牌到期时断开会话，设备须按返回的 `expiresIn` 在到期前重新取令牌并重连。未配置 EMQX 管理 API 时有效期固定 300 秒（到期是唯一的撤销方式）；配置后平台可即时封禁并断开被撤销的凭据，有效期改用 `IOT_MQTT_DEVICE_TOKEN_TTL`（默认 24 小时），避免大量设备每 5 分钟集中重连。

| 用途 | Topic |
| --- | --- |
| 属性 | `/iot/up/{tenant}/{product}/{device}/property` |
| 事件 | `/iot/up/{tenant}/{product}/{device}/event` |
| 告警 | `/iot/up/{tenant}/{product}/{device}/alarm` |
| 状态 | `/iot/up/{tenant}/{product}/{device}/state` |
| 命令回执 | `/iot/up/{tenant}/{product}/{device}/command-reply` |
| 订阅命令 | `/iot/down/{tenant}/{product}/{device}/command` |
| 订阅归档确认 | `/iot/down/{tenant}/{product}/{device}/receipt` |

标准设备 ACL 只授予自身上行与下行主题；平台拒绝 retained 上行，并重新检查设备和产品启用状态。HTTP/MQTT 的设备连接状态来自标准 state 上报，不是 Broker 实时在线查询。

设备收到 PUBACK 只表示 Broker 接收。先订阅令牌响应的 `receiptTopic`，再以 QoS 1 发布。平台完成原文持久归档和索引后，在本设备的 receipt 主题发送 QoS 1、非 retained 确认：

```json
{"id":"设备原始消息ID","rawMessageId":"raw_std_...","payloadHash":"原上行JSON字节的SHA256十六进制摘要","status":"archived","archivedAt":1790000000000}
```

设备用 `id + payloadHash` 关联确认，保存尚未确认的消息；超时或重连后重发完全相同的字节、ID 和 timestamp，使用退避并限制重试速率。收到确认后才能从设备待发队列移除。重复确认允许出现；同 ID 不同正文会被拒绝。`archived` 不表示解析、入库、告警或 AI 已完成，后续按 `rawMessageId` 查询。原始报文入口 `/external/raw/...` 的摘要针对 RawMessage 的 Payload 字节。没有匹配应用确认时，设备必须保留“结果未知”，不能用 PUBACK 代替。队列容量、隔离记录和磁盘持久性见 [MQTT 接收保障](#mqtt-接收保障)。

## 凭据与命令

日常操作位于“设备管理 → 详情 → 设备控制”，根据产品物模型的 `commands[].fields` 生成文本、数值、布尔、对象或列表参数输入项，仅允许选择已定义命令。MQTT 参数提交到 `data`，Go 协议参数作为命令对象的顶层字段传给 encode，`type` 为命令标识；协议实现须与产品命令定义一致。

未定义命令的模板不开放自由输入；执行命令需人工确认及相应菜单、操作权限和设备范围。

Secret 仅首次创建或轮换返回，服务端保存哈希；详情返回 `credentialSupported` 标识。TCP、UDP、Modbus 与子设备不生成平台 Secret，调用凭据接口返回 422。显式设备通信方式优先于产品默认值；旧协议设备曾生成的密钥不能绕过当前认证检查。

HTTP Secret 在轮换后立即失效。MQTT 已签发令牌的及时撤销需配置 `IOT_EMQX_API_URL`、`IOT_EMQX_API_KEY`、`IOT_EMQX_API_SECRET`；URL 是 Broker 管理根地址，不带 `/api/v5`。凭据变更和撤销意图一同保存，平台封禁旧 username 并断开其会话；失败保留 PENDING，每 30 秒重试。未配置管理适配器时不能把轮换等同于即时断连，仍依赖 JWT 到期与 Broker 的到期断连。配置见 [EMQX 认证与授权](PLATFORM.md#权限与设备范围)。

标准 MQTT 命令请求：

```http
POST /api/v1/device-registry/{id}/commands
Content-Type: application/json
Authorization: Bearer <authorized-user-token>

{"confirmed":true,"id":"cmd-001","type":"setThreshold","data":{"value":30}}
```

命令要求设备、产品和凭据启用，MQTT publisher 可用；产品定义了命令模型时校验名称与参数。同租户命令 ID 幂等，同一请求不重复发送，不同请求冲突。下行 QoS 1、非 retained，包含 v1 `command/params` 和兼容 `type/data`。

回执示例：

```json
{"version":"1.0","id":"reply-001","timestamp":1789000000001,"commandId":"cmd-001","success":true,"data":{"result":"ok"}}
```

`SENT` 仅表示发布给 Broker，设备回执才确定 `SUCCEEDED` / `FAILED`；发送或等待超时显示 `UNKNOWN`，不自动重发物理命令。设备需按命令 ID 去重执行。回执先归档再解析，并按租户、设备和命令关联；迟到回执可补充真实结果。

Go TCP/UDP 命令同样要求 `confirmed:true`，另需 encode 能力和有效会话；接口、关联 ID 及超时行为见 [协议调用契约](#go-协议)。

## Go 协议

在“协议开发 → 上传源码”下载 Go 函数模板，修改 `protocol.go`；模板提供 `Definition`、`Message`、`Context`、`Frame` 和适配器，无需自行编写 stdin/stdout。上传 `.go` 或包含整个 module 的 ZIP。复杂独立项目可自行维护 `protocol.json`、Worker 和样例目录。

| 函数 / 字段 | 契约 |
| --- | --- |
| `Decode func([]byte, Context) (Message, error)` | 完整帧转换成标准消息，必填 |
| `Ingress func([]byte, Context) (Frame, error)` | 分帧、识别、校验与应答，可选 |
| `Encode func(Command, Context) (Frame, error)` | 命令编码，可选 |
| `Samples` / `Operations` | 独立填写真实输入与期望结果，每项能力须覆盖成功样例 |

在协议目录运行 `go test ./...` 做本地预检；不能用 Decode 返回值生成期望值。平台上传时独立编译和试跑，不执行上传包的 `_test.go`、脚本或 `go generate`。入口与模板以 `internal/protocolbuild/functiontemplates/`、`GET /api/v2/protocol-source-template` 为准。

### 上传与发布

Go 源码与制品包会在平台上编译和执行，`source-releases`、`package-releases` 两个上传接口与备份、运维数据使用同一平台边界：只有内置管理员，或 `IOT_OPS_TENANTS` 运维租户中获授权的用户可以上传、试跑；业务租户的权限目录不提供这两项，历史授予也不再生效，业务租户使用已发布协议、JSON 路径映射与固定字段映射。发布、预览、切换和下载仍按原有菜单与操作权限执行。

完整项目 ZIP 直接包含 `go.mod`、源码、`protocol.json`、`samples/cases.json` 和可选 `samples/operations.json`（允许一层外目录）。第三方依赖先 `go mod vendor`；构建关闭 CGO、网络下载、工作区与自动工具链下载，不能使用目录外 replace。服务器 Go 工具链须可用。

`POST /api/v2/protocols/{id}/source-releases` 使用 multipart：`file`、`version`、`name`、`transport`、`payloadFormat`、`entrypoint`、`runtime`、`capabilities`、`targetPlatforms`、`cases`、`publish`、`productId`。数组字段为 JSON；表单非空值覆盖包元数据，ID 与 URL 一致。`publish` 默认 false，构建与样例校验成功后保存为 `VALIDATED`。页面按「构建并校验 → 发布协议 → 用于设备模板」操作，不在上传时切换模板。接口仅在显式传入 `publish=true` 且模板尚无登记设备时允许同时传 `productId` 完成初次绑定；已有设备的组合上传在构建前返回 409。已发布版本可独立用于模板，正式版本更新须通过隔离试验、现场验收和应用流程；未发布版本不可绑定。 若编译期间模板发生并发变化导致最终绑定失败，仍返回 201 和已创建的 `release`，通过 `bindingWarning`、`stepResults` 明确标记模板应用失败；保留版本与源码制品，无需重复上传。

版本不可覆盖。编译、样例、哈希或任一所选目标失败都保留旧版本；发布后在模板的协议版本中切换/回滚，原文保存实际版本和帧前状态。只删除某个未被绑定、回滚或接入点引用的版本；有引用返回 409。

| 限制 | 值 |
| --- | --- |
| 源码 / ZIP 展开 | 32 MiB / 128 MiB，最多 4096 条目 |
| 构建 / 样例 | 每平台 120 秒；每进程一个上传任务；1–100 条样例、总 60 秒、单例 10 秒 |
| Worker 输出 / 默认运行超时 | 1 MiB / 5 秒 |
| 平台目标 | Linux/Windows/macOS 的 amd64/arm64；始终包含发布端 |
| 制品 | 单 Worker 64 MiB、总 Worker 128 MiB、最终 ZIP 64 MiB |

发布端实际试跑为 `PASSED`；其他目标仅编译为 `COMPILED`，未试跑的预编译目标为 `UNTESTED`。构建成功不等于该系统已验收。

在线、离线与集群部署中，源码编译、样例试跑和运行时解析都在独立的 `protocol-runner` 容器内执行（`IOT_PROTOCOL_SANDBOX=runner`）：该容器无网络、只读根文件系统、去除全部 capabilities、限制进程数与内存，不挂载平台数据卷，也不持有任何数据库或平台密钥；平台进程只通过共享卷中的 Unix 套接字与它通信，Worker 按 SHA-256 缓存在运行器内，首次使用时由平台发送。编译前用 `go list` 检查上传代码及 vendor 依赖的直接导入，禁止 `os/exec`、`syscall`、`unsafe`、`plugin`、`net`、`net/http` 等系统与网络访问包以及汇编、cgo 源文件。预编译制品包不经过该检查，但同样只在运行器中执行。宿主机源码调试未配置 `IOT_PROTOCOL_RUNNER_SOCKET` 时仍在平台进程内执行并在启动日志中警告，不能作为隔离环境。

下载 `/api/v2/protocols/{id}/releases/{version}/source` 返回原始上传字节，`/package` 返回完整制品 ZIP；两者检查租户、操作权限和 SHA-256。没有自动远程仓库拉取，CI 可使用源码上传接口。页面只展示当前版本支持的解析测试、发布和源码下载。

### 协议解析预览

从“协议开发 → 管理版本 → 详情 → 解析预览”进入，设备模板中的“协议解析预览”也会跳转到此处，调用 `POST /api/v2/protocols/{id}/releases/{version}/preview`，始终提交 `readOnly: true`。`operation` 默认为 `decode`（`payload` 为 HEX 字符串或 JSON 值）；具有相应能力的 Go v2 版本还支持 `ingress`（`chunks` 为按接收顺序排列的 HEX 片段，`datagram: true` 要求每段为完整 UDP 数据报）和 `encode`（`command` 为命令对象）。可填写模拟 `deviceId`、帧前 `state`、毫秒时间 `now`，以及需要核对的 `expected` 字段；响应保留 `standardMessage`，并返回 `operationResult`、可选 `comparison` 字段差异。对象按填写字段比较，数组同时比较长度与位置。拆帧显示每次消费、半帧余留、帧前后状态、ACK / 应答、命令关联标识和逐帧解析结果；子设备仅作为观察结果展示。

平台标准协议 `iot-standard@1.0.0` 同样支持只读 `decode`，不需要映射配置；使用标准 `id`、`timestamp`、`data` 信封和显式 `messageKind`（`property`、`event`、`alarm`、`state`、`command-reply`）。页面可选择消息类型并手动填入示例；不会自动发送。来自原文时类型沿用已授权 `raw.headers.messageKind`，不允许在保持原文关联时改为另一种类型。标准预览仅调用 `StandardParser.Parse`，不推进发布状态、创建设备字段、写入告警或完成命令。

解析预览可从设备模板或设备连接诊断进入，也可输入原文编号。载入原文只读取样本，仍须点击「只读试跑」才执行。提交 `rawMessageId` 后，服务端再次检查原文读取权限、租户和设备范围，要求所选版本与原文归档版本完全一致，并使用归档的完整 RawMessage（模板、设备、报文头、厂商元数据、帧前状态和接收时间）。允许编辑本次样本字节和帧前状态，不修改归档；切换版本或设备会解除原文关联。不存在或已撤销的归档版本不会自动换用当前版本，试跑也不会创建回放任务。

预览从服务端版本读取制品、校验 SHA-256，以单次 Worker 调用执行操作，不复用现场常驻 Worker。一次请求最多 32 个片段、64 次拆帧，单次缓冲和帧前状态各不超过 64 KiB，总执行时间最多 10 秒，保留既有最小环境及输出限制。平台不创建网络连接、不向设备发送命令、不登记设备、不产生业务原文、标准消息、状态或告警；试跑结果不能替代真实网络、鉴权、设备应答与现场验收。Worker 仍具有服务进程的操作系统权限，这不是 OS 沙箱。

### Worker 契约

以 `protocol-packages/gb26875-dahua` 为独立 module 示例，可复制到任何 Git 仓库。目录结构：

```text
go.mod
protocol.json
main.go
gb26875/...
samples/cases.json
samples/operations.json
```

`protocol.json` 示例：

```json
{"id":"gb26875-dahua","name":"GB26875 大华消防终端","version":"1.0.0","runtime":"go-protocol-v2","transport":"TCP_UDP","payloadFormat":"hex","capabilities":["decode","ingress","encode"],"entrypoint":"."}
```

`TCP` / `UDP` 只支持对应网络，`TCP_UDP` 支持两种网络实例。每个平台连接配置固定一个租户和产品；同一网络监听端口只允许一个启用实例，TCP 主动连接不占用监听端口。新增版本须继续支持产品上所有已启用实例的网络。

平台仅接受明确声明的 `go-protocol-v2`；不接受旧版本请求、裸标准消息返回或未声明运行时的制品。租户、产品和设备归属由原文确定，Worker 返回值不能更改。

Worker 有两种运行方式，请求与结果格式相同，日志都写 stderr：

- **单次调用**：未设置 `IOT_PROTOCOL_WORKER_MODE` 时，从 stdin 读取一个 JSON 请求，向 stdout 输出一个 JSON 结果并退出。
- **常驻服务**：环境变量 `IOT_PROTOCOL_WORKER_MODE=serve` 时，逐行读取请求（每行一个 JSON），每个请求向 stdout 写一行 JSON 结果，并原样带回请求中的 `requestId`；stdin 关闭后退出。stdout 不能输出结果以外的内容，否则平台判定应答错位并重启该进程。请求之间不得依赖进程内存，状态只通过 `state` 传递；平台可能随时重启或回收进程。

平台生成的函数模板适配代码、解析模板和 GB26875 示例已同时支持两种方式。发布校验在全部样例通过后，用同一 decode 请求对一个常驻进程连续调用两次，结果都与单次调用一致时才在制品上记录 `workerMode: "serve"`；未声明 `workerMode: "serve"` 的版本按单次调用启动。常驻进程按制品路径和 SHA-256 分组，不同租户、协议或版本不共用进程；每个制品最多 4 个进程，空闲 2 分钟回收。单次请求超时、进程退出、输出超过 1 MiB 或 `requestId` 不匹配时，结束该进程并在下次调用时重新启动。

`version` 固定为 2，操作如下：

| operation | 输入 | 输出 |
|---|---|---|
| `decode` | `raw`：完整 RawMessage；`state`：归档的帧前状态；`now`：receivedAt | `standardMessage`：标准消息 |
| `ingress` | `data`：当前缓冲区 HEX；`state`：前次状态；`now`：当前 Unix 毫秒 | `consumed`：本次完整帧字节数；`deviceId`；可选 `deviceName`、`reply`、`state`、`correlationId`、`children` |
| `encode` | `command`：包含 `type` 的协议自定义对象；`state`；`now` | `reply`：下行 HEX；可选 `state`、`correlationId` |

帧尚未完整时仅返回 `{"needMore":true}`，不得消耗数据、登记设备或发送应答。完整帧必须从缓冲起点开始，`consumed` 为正且不超过输入字节数。TCP 会继续处理剩余缓冲；UDP 一个数据报必须恰好是一帧，不能返回 needMore。错误返回 `{"error":"原因"}`：TCP 断开连接，UDP 丢弃该报文，不发送应答。

`deviceId` 最长 128 字节，无空白、控制字符或路径分隔符；同一会话不能更换设备 ID。平台只接受该租户、该产品下启用的设备；实例开启 autoRegister 时才创建未知设备。协议的状态须为可序列化 JSON，最多 64 KiB；入站缓冲、完整帧及应答最多 64 KiB（UDP 下行还受 65507 字节限制）。TCP 半帧最多等待 30 秒，默认连接空闲两分钟关闭；配置定时查询时，空闲期限延长至最长查询周期加两分钟。每实例会话上限由 `IOT_PROTOCOL_LISTENER_MAX_SESSIONS` 配置（默认 1024）、每进程最多 32 个监听操作并发；Worker 按制品记录的运行方式单次启动或常驻复用。

平台将帧前状态写入 `raw.metadata.protocolState` 并在 decode 请求的 state 中传递，历史回放同样读取这份快照，不依赖仍在线的连接。完整帧先进入正常原始消息归档及事件投递，成功后才发送 `reply`。这表示平台已接收原文，不表示 Kafka 下游异步解析和告警已经完成。`decode` 应对 ingress 接收的每种完整帧都返回有效标准消息，包括 ACK。协议必须避免对 ACK 再 ACK。

`encode` 返回 correlationId 时，平台等待后续 ingress 的相同 ID；无 ID 时只返回 `sent`，有匹配成功入站则返回 `acknowledged`。命令超时不自动重发，并断开当前连接以隔离迟到响应；主动连接会退避重连，被动连接等待设备重连。每连接一次只执行一条等待应答的命令；定时查询必须返回 correlationId。半帧和待应答命令保留旧版本，版本切换清空状态；新版本可从下一次设备上报重建状态。Worker 的单次执行最长仍受 5 秒默认值约束（配置最大 10 秒），命令等待按实例 timeoutMs，最多 30 秒。

## 配置驱动协议

通用 JSON 可使用 `custom_json_parser`；字段名称或字节布局不同的报文可使用以下两种配置映射。变长、会话或厂商专用协议使用本文的 Go 源码包流程。

### JSON 路径映射

解析器选择 `configurable_json_parser`，协议标识使用 `json`，载荷格式使用 `json`。配置示例：

```json
{
  "properties": {
    "temperature": {"path": "$.data.temp", "type": "number", "scale": 0.1},
    "smoke": "$.data.smoke"
  },
  "tags": {
    "deviceType": "$.kind"
  },
  "timestampPath": "$.occurredAt",
  "timestampUnit": "s",
  "messageType": "PROPERTY_REPORT"
}
```

字段支持 `number`、`integer`、`boolean`、`string` 和 `json`，未找到的路径可用 `default` 提供默认值。路径是安全的 JSONPath-lite，只读对象字段和数组下标，不执行脚本。

### 固定字段十六进制映射

解析器选择 `configurable_hex_parser`，协议标识使用 `config-hex`，载荷格式使用 `hex`：

```json
{
  "startHex": "AA",
  "endHex": "55",
  "checksum": "sum8",
  "checksumStartOffset": 1,
  "fields": [
    {"name": "temperature", "offset": 1, "length": 2, "type": "int16", "endian": "little", "scale": 0.1}
  ]
}
```

字段偏移从完整报文第 0 字节开始，支持 `uint8`、`int8`、`uint16`、`int16`、`uint32`、`int32`、`float32`、`ascii` 和 `hex`。这适用于固定长度传感器报文；变长、TLV、多信息体和复杂会话协议统一使用上传的 Go 源码包，包括 GB26875。

发布前在“协议开发”的版本详情中执行“解析测试”，确认标准消息结果，再发布并用于设备模板。

### 从报文或点表生成协议

入口为“协议开发 → 协议生成”。选择输入类型后按以下流程操作：

1. 报文支持上传 `.json/.txt/.hex/.bin` 或填写样本；JSON 自动提取路径，HEX 需提供字段偏移、长度、端序等说明，由已配置 AI 辅助生成固定字段映射。样本最大 1 MiB。
2. 点表支持 `.xlsx/.csv`，文件最大 32 MiB，也可粘贴 CSV；平台直接生成 Modbus TCP / RTU 寄存器或线圈映射及读取块，不调用 AI。当前页面不提供 PDF、DOCX 上传。
3. 在「编辑字段映射」中对照输入字段标识、JSON 路径、Modbus 地址或 HEX 偏移；支持新增、删除，展开行调整倍率、字节序等参数。无需直接编辑源码。
4. 用真实样本执行「解析预览」。Modbus 需填写该响应帧对应的零基起始地址。编辑字段后旧预览清除，须重新验证。
5. 「保存草稿」创建不可变 `DRAFT` 版本，再用真实样本执行「校验草稿」，成功后变为 `VALIDATED`，最后「发布协议」。从模板流程内生成时，点击「用于当前模板」返回所选的协议标识、版本及协议包标识，由模板流程保存绑定；独立协议页则返回「设备模板」选择。已保存映射通过「新建版本」修改，不能覆盖原版本。

点表常用列为 `identifier,name,functionCode,address,addressNotation,dataType,scale`。零基地址明确填写 `addressNotation=zero_based`；未声明基准的 `40001` 等传统地址按 Modbus 表区换算。表单中的 Modbus 地址统一从 0 开始。单个报文不能推断完整的变长、会话或厂商协议，这类接入使用 Go 源码包。

| 接口 | 用途 |
| --- | --- |
| `POST /api/v1/ai/protocol-assistant/generate` | multipart 上传，`inputKind=sample\|point-table`，支持 `file`、`pointTable`、`samplePayload` |
| `POST /api/v1/ai/protocol-assistant/preview` | 未保存映射的解析预览 |
| `POST /api/v1/ai/protocol-assistant/publish` | 保存 v2 草稿或已校验版本 |
| `POST /api/v2/protocols/{id}/releases/{version}/preview` | 校验已保存映射或试跑 Go v2 ingress / decode / encode；`readOnly=true` 不改变版本状态，带 `rawMessageId` 强制只读 |
| `POST /api/v2/protocols/{id}/releases/{version}/publish` | 发布已经校验的版本 |

已发布协议可在[解析预览](#协议解析预览)中复查。标准协议的链路模拟须在“模拟设备测试”显式点击“准备测试设备”后再发送报文，会写入测试设备数据并可能产生告警；不计入现场验收。真实设备从“设备管理 → 详情”核对连接诊断，并通过“设备控制”执行已定义命令。

#### 消息类型定义

界面显示中文名称，括号内是接口和 Worker 使用的稳定代码：

| 中文名称 | 代码 | 用途 |
| --- | --- | --- |
| 属性上报 | `PROPERTY_REPORT` | 测点、开关量和当前状态值 |
| 事件上报 | `EVENT_REPORT` | 一次性发生的复位、心跳或测试事件 |
| 告警上报 | `ALARM_REPORT` | 设备明确上报的告警事件；无需告警规则即可生成平台告警，规则可额外提供分类、等级或联动动作 |
| 状态变化 | `STATE_CHANGE` | 在线、离线或业务状态变化 |
| 指令应答 | `COMMAND_REPLY` | 设备对平台指令的响应 |
| 日志上报 | `LOG_REPORT` | 运行日志或诊断信息 |

Excel 点表中的文本仅作为协议资料；平台不会执行其中的脚本、URL 或其他指令。保存、预览及发布需要对应菜单和操作授权，详见 [用户权限](PLATFORM.md#权限与设备范围)。Modbus 映射可先保存再用真实样本校验，专用协议必须上传 Go 源码包并通过样例验证后发布。

### 已有协议

旧 JavaScript、厂商及 Modbus 解析器保留用于显式绑定和历史回放。JavaScript 新建/更新入口已关闭，新专用协议使用 Go 源码；Modbus 点表接入按 [统一设备接入](#设备接入) 配置。

## 部件状态契约

协议继续返回一个 StandardMessage，在 `event.components` 中声明本次观察到的部件。标准 MQTT/HTTP 的 event 上报对应 `data.components`。不要求每次上报控制器全部部件，也不把部件伪装成跨租户设备。

Go 协议可以直接构造普通 Go 数据，例如：

```go
Event: map[string]any{
    "components": []map[string]any{
        {
            "id": "loop-1/node-7",
            "name": "烟感探测器",
            "location": "二楼走廊",
            "timestamp": observedAt.UnixMilli(),
            "alarms": map[string]bool{"FIRE": true, "DEVICE_FAULT": false},
        },
    },
},
```

- `id` 在所属控制器内稳定、唯一，不使用临时消息 ID；名称和位置可更新。最多 256 个部件，每个部件最多 32 种状态。重复 ID、空列表、无效时间、非布尔值及 `null` 拒绝解析。
- `timestamp` 是部件状态发生的毫秒时间，省略或为零时使用 StandardMessage 的时间。比消息时间超前五分钟的部件时间被拒绝。
- `alarms` 的键为明确的告警类型，如 `FIRE`、`DEVICE_FAULT`；`true` 表示该类型活动，`false` 表示该类型恢复。缺少的部件、缺少的类型均保持原状。
- 火警和故障可同时活动，恢复其中一项不恢复另一项。连接、注册、保活与普通 `STATE_CHANGE` 不构成告警恢复证据。
- 同一个部件的同类型状态按发生时间推进，较旧补传/回放不覆盖较新状态。同一时间的冲突采取保守策略：报警优先，正常状态不能清除同时间报警；需要后续明确的正常状态。秒级设备时钟也遵守此约定。
- 告警保留父设备的租户、设备 ID、摄像头及区域归属，新增 `componentId`、`componentName`、`componentLocation`。告警详情仍保留 StandardMessage 和 Raw 关联，不改变原文下载和回放标识。

`component_alarm_state` 保存每个租户/设备/部件/告警类型的状态时间和告警生命周期引用，与 `alarm_record` 在同一事务提交。启动时的 PostgreSQL 幂等 schema 迁移创建该表，无需清空旧数据。内存仓库用于开发/测试，不具有跨进程持久化保证。

部件来源告警独立于控制器级自定义规则维护。已有规则仍按原标准消息匹配，不会因为规则命中而吞掉部件来源告警；已有规则不自动转换为部件规则。确认/关闭同时检查当前菜单、操作权限和设备范围，设备整体业务状态按剩余活动或已确认告警重算。弹窗按部件告警 ID 去重，同一原始报文里的多个告警不会互相遮蔽。

## TCP 与主子设备接入

平台提供两种 Go 协议 TCP 连接方向。先在“设备模板 → 连接与验收 → 公共连接与验收规则”准备共享配置并完成首台验收；日常“添加设备”选择已有共享监听或填写单台主动连接地址，共享监听不在日常向导中重复创建：

- **设备连接平台**：`mode=listener`、`network=tcp`，`connectionMode=listen`（旧配置留空等价），分别填写平台监听 IP、设备端填写的对外地址和端口。`0.0.0.0` 只能作监听地址。每个共享连接关联一套主设备模板，可按协议识别多个主设备。
- **平台连接设备**：设置 `connectionMode=dial`，填写设备/串口服务器地址、端口及预配置的设备标识。平台保持连接，失败退避 1～30 秒重连；目标解析和 IP 校验复用 `IOT_MODBUS_ALLOWED_CIDRS` 的出站网络策略。在“添加设备”中选择“平台主动连接设备”时，设备与专属连接在同一请求中创建，后续从设备详情的连接诊断中修正地址和查询参数。

TCP 建立成功不等于协议注册或认证成功。协议必须真实校验注册报文，在 Ingress 中返回错误可拒绝连接；主设备通过平台归属/启用检查、原文接收成功后才发送协议回复。平台不会仅凭一个设备 ID 伪造注册成功。配置的设备标识会传入 `Context.DeviceID`，它是路由信息，不能代替协议认证。

### 一问一答和主动上报

实例 `queries` 配置查询 `type`、`intervalSec`（1～86400 秒）及可选 `params`。界面提供类型与周期表单；具体报文写在协议 Go `Encode` 中。主动连接建立后即可调度，用于需要平台先发起握手的设备；被动接入需先由入站帧识别设备。协议可根据 `Context.State` 控制注册前后哪些命令可发送。

平台一次只发送一条等待应答的命令，定时查询必须返回 `CorrelationID`，对应入站响应返回相同 ID。等待期间仍持续接收设备主动上报，不能把任意上报当成查询应答。已有手动命令在未提供关联 ID 时仍明确返回 `sent`，不冒充应答成功。

超时或请求取消后关闭连接，避免迟到应答匹配下一条查询；不会自动重发可能具有副作用的手动命令。定时查询会按周期继续执行，应配置读取类命令。失败握手后不能继续发送已注册状态下的查询；这由协议中的实际状态校验决定。同一实例下相同主设备重新注册后替换旧会话，避免两条会话重复查询。

协议版本在半帧及待应答命令期间保持原版本。主设备版本切换会清空主会话状态，新版本须能从后续报文重建状态或要求重新注册。没有新增任意连接事件回调；常驻 Worker 只复用解析进程，会话状态仍只通过 `state` 传递。

### 串口服务器与 Modbus

- 标准 Modbus TCP 转换网关：沿用 `MODBUS_TCP` 点表采集。
- 原始 RTU 字节透明转发：**Modbus RTU 串口透传 TCP**（`MODBUS_RTU_TCP`）在模板中准备协议及验收规则，新设备在“添加设备”中填写串口服务器地址、端口和站号，已有设备在连接诊断中维护单台参数。实例保存 `mode=poll`、`network=tcp`、`wireFormat=rtu_over_tcp`，使用 `MODBUS_RTU` 版本化点表解析器。平台主动连接目标，发送带 CRC 的 RTU 查询，处理分片、站号、功能码、字节数和 CRC；归档保留原始 RTU 帧，不把转换后的 MBAP 帧冒充原文。

同一进程内，同一个解析后 IP/端口的多个站号串行读取。独立接入副本应将同一物理串口服务器的轮询实例部署到同一执行端；当前执行租约按实例分配，不提供跨进程的物理串口总线调度。串口服务器以 TCP 客户端向平台连接时，使用前述 Go TCP 入站协议及查询调度，按实际设备报文实现组包和校验；保留的内置 RTU 点表接入 API 使用平台主动连接。

平台通过 TCP 与设备或串口服务器通信，须保证网络可达。

### 首次配置主设备与子设备协议

1. 发布主设备 Go 协议，在主设备模板中选择该版本。
2. 为每类子设备准备模板，分别选择已发布的 HEX 报文解析协议；协议可以和主设备不同。
3. 在主设备模板的公共连接中配置 `childProducts`：`type` 是主协议识别的子设备类型，`productId` 是相应子设备模板。模板未启用、未绑定协议或格式不兼容会被服务端拒绝；保存候选后按模板准备流程应用。
4. 从主设备模板添加首台验证设备。主设备注册后，再接收子设备登记或上报；也可以在主设备“详情 → 子设备”中按类型和地址预先登记。核对两层真实原文及标准消息，再分别保存模板验收结果。

子设备通过产品继承协议版本，不逐台复制协议。修改子设备产品绑定会影响该产品下的子设备；正在等待应答的子设备命令仍使用发送时的子协议版本解析回复，后续新报文使用当前绑定。已有子设备地址映射到不同类型/产品会被拒绝，不自动迁移原记录。

Go 函数模板通过 `Child` 和 `Frame.Children`，业务代码无需维护内部 JSON：

```go
return Frame{
    Consumed: frameLength,
    DeviceID: mainDeviceID, // 始终是当前连接的主设备
    State:    ctx.State,
    Reply:    acknowledgement,
    Children: []Child{
        {Address: "1-7", Type: "smoke", Name: "二层烟感", Data: sensorBytes},
    },
}, nil
```

`Data` 是从外层报文提取的子设备原文。空 Data 只登记台账，不制造遥测数据。每帧最多 256 个子设备观察，每项子设备报文至多 64 KiB，整体仍受 Worker 输出限制。初始注册帧不能同时登记子设备，必须先完成主设备注册和应答。

平台按“租户＋主设备 ID＋子设备地址”生成稳定 ID，事务内创建或更新台账，保留 `deviceRole=CHILD`、`gatewayId`、地址和类型。并发重复注册只创建一条，跨主设备的相同地址互不冲突；已禁用子设备不能通过上报重新启用。主设备首次接收已配置子设备后标记为 GATEWAY，表示业务主设备，不是现场 Agent。

主设备完整帧先归档，发现信息保存在 `metadata.children`；子设备原文单独进入 Raw→Parser→存储/告警链路。子原文固定子协议版本，记录 `parentRawMessageId` 和外层协议版本。登记或子原文接收失败时不发送本帧成功应答。未知类型的发现信息仍可从主设备原文查看，错误显示在对应接入点；配置完成后由设备重新上报处理，不会猜测产品。

主设备原文回放只回放主协议解析，不重复执行设备注册和子报文分发；回放子原文使用归档的子协议版本。主设备在线不会自动将所有子设备标为在线，子设备台账、最近上报和业务状态分别显示。

### 子设备命令与页面

子设备的 Go `Encode` 先产生内部命令。平台再调用主设备协议 `Encode`，命令类型为 `child`，参数包含 `address`、`childType`、`payload`（HEX）和内部 `correlationId`。主协议按厂商格式封装外层数据，返回自己的关联 ID；入站应答必须包含对应子设备观察，避免其他子设备上报误完成命令。子设备独立会话状态暂不存储；需要跨帧握手状态时在主协议的 `State` 中按子设备地址维护。

设备管理按「全部、独立设备、主设备、子设备、待登记」分组，并支持关键字、设备类型和运行状态筛选。主设备“详情”展示分页子设备列表、产品/协议、最近上报及状态，并可按接入点已映射的类型添加子设备。只有分别授权后，用户才能进入关联主设备或子设备详情；主设备授权不会自动覆盖其子设备。全部设备范围用户可查看父连接，指定设备用户不展示共享接入点和未授权父设备信息。

接口：

- `GET /api/v1/device-registry/{id}/children`：租户内主设备的分页子设备台账。
- `POST /api/v1/device-registry/{id}/children`：按接入点映射的类型和地址登记子设备，同一地址重试返回已有设备。
- `GET /api/v2/products/{id}/protocol-binding`：查看产品协议绑定。
- `POST /api/v2/device-access-profiles/{profileId}/devices/{childId}/commands`：沿主设备会话发送子设备命令，继续要求对应菜单/命令操作权限、设备范围及人工确认。

### TCP 与主子设备验证

`internal/protocolruntime` 的回归覆盖 Socket 主动连接/重连、查询互斥、RTU CRC 与点表；`TestTCPParentChildSourceChain` 上传两份 Go 源码、编译发布，并通过两种方向的 TCP、归档/解析和 API 验证分层链路，配置 `IOT_TEST_BROWSER` 时还运行 Chrome 检查。数据库原子性由 memory / PostgreSQL 的共享仓储契约验证。执行条件与命令统一见[设备接入回归](DEVELOPMENT.md#设备接入回归)和[浏览器验证](DEVELOPMENT.md#浏览器验证)。

这些是协议模拟器和测试环境验证，不能代替厂商真实协议、设备或生产网络验收。

用户级设备范围不改变协议内部的主子报文路由或自动注册。未获授权的子设备会继续接收和归档数据，但不会出现在该用户的列表、总数、告警或实时提醒中。授权步骤见 [用户权限](PLATFORM.md#权限与设备范围)。

## MQTT 接收保障

API/Gateway 装配使用 `NewDurableWithCredentials`，接收过程为：

1. Broker 按现有 JWT/账号和 ACL 完成身份、主题授权；平台继续拒绝 retained、超限及未知路由消息。平台自身发布的 `/iot/device/state/{tenant}/{product}/{device}` retained 状态快照在重新订阅时仅确认并忽略，记录为调试日志，不作为新上报入队或刷新设备状态；其他拒收警告通过 `reason` 区分 `retained`、`payload_too_large` 和 `unknown_topic`。
2. 入站原始字节和实际主题写入 `IOT_DATA_DIR/mqtt-inbox/<processRole>/`（显式设置 `IOT_INSTANCE_ID` 时为 `<processRole>/<实例>`）。文件刷新并原子落盘后才向 Broker 确认此投递；内存入队不构成成功接收。
3. 后台按原路径执行设备/产品状态校验、Raw 归档、幂等索引及内部消息发布。数据库或队列暂时失败时保留磁盘记录并重试。第一次接收时间保存在队列中，补传不改成重试时间。
4. 完成处理后删除并刷新队列目录；进程在删除前退出可能重试，因此业务仍必须幂等。标准报文使用已有 `id`，原始 MQTT 信封必须提供 `messageId`，视频信封必须提供 `eventId`，不得依赖平台每次生成随机 ID。

队列使用公共 `internal/durablequeue` 的文件锁、原子写入和隔离机制，32 个固定分片，默认总上限 1 GiB / 50000 项，均分到各分片；热点主题可能先达到分片上限。接收回调在自身生命周期内完成 fsync 和 ACK，不能先返回再异步调用旧连接的 ACK。Paho 保持有序回调；每个分片的后台处理仍并行运行。回调被磁盘阻塞时，由 Broker 会话队列承接等待，设备仍须等待应用确认。同一分片内按文件名顺序处理，不保证同一设备的报文顺序。主题和原文字节的摘要用于接收队列去重，不替代业务消息 ID。满容量、写入错误或损坏隔离导致无法确认时，不 ACK，并通过重连请求 Broker 重投；没有启动无界 goroutine 或无限内存队列。

平台持久会话尚未取走的报文由 EMQX 按会话队列缓存。Compose 将 `max_mqueue_len` 设为 100000（`IOT_EMQX_MAX_MQUEUE_LEN`）、`max_inflight` 设为 128（`IOT_EMQX_MAX_INFLIGHT`）；队列满时 Broker 会丢弃报文，而设备已经收到 PUBACK。

不可解析的磁盘记录保留为 `.corrupt`；业务明确拒收的记录保留为 `.rejected`，都计入容量。未知错误默认可重试；数据库读取失败不能冒充设备认证失败而永久隔离。隔离原文仅供受信任运维人员检查，不能作为有效标准消息发布。目前没有队列管理页面；不得直接删除文件来宣称补传完成。

客户端 ID 保存在同一目录的 `client-id`，跨进程重启保持不变，使用持久会话并在重连时恢复订阅。每个运行实例必须拥有独立且持久的目录；同一目录的文件锁禁止并发占用。扩副本不能复制同一个 `client-id` 供多个活跃进程使用。共享订阅继续按既有配置工作，各实例对自己接收并确认的报文负责。

`mqtt_subscription_count` 只统计当前连接已成功恢复的订阅；`mqtt_broker_observation_ok=0` 表示未配置、断连或管理查询失败，此时旧的 Broker 数值不能解释为当前零丢弃。`mqtt_archive_receipt_total` 包含重试确认，不等于唯一归档数；唯一归档以 `raw_archive_success_total` 和数据库 message ID 审计为准。

可观测性：`mqtt_inbox_pending`、`mqtt_inbox_rejected`、`mqtt_inbox_corrupt` 每五秒投影到既有指标；配置 `IOT_EMQX_API_URL`、`IOT_EMQX_API_KEY`、`IOT_EMQX_API_SECRET` 后，每 15 秒读取 Broker 上平台会话的 `mqtt_broker_queue`（排队数）与 `mqtt_broker_dropped`（累计丢弃数），丢弃数增加时记错误日志，表示接收速度低于设备发布速度且已有报文丢失；写入和处理失败记入健康检查与日志。隔离数量独立展示；已隔离记录本身不会让整个服务退出就绪状态，也不被计作处理成功。

### 保障边界

- 设备收到的 Broker PUBACK 仍不等于平台已归档或生成告警。设备可沿用接收查询及相同消息 ID 重试。平台 ACK 表示本机持久队列接收了该投递，业务校验仍可能拒收。
- 进程重启前已经落盘的记录不依赖 Broker 内存恢复；尚未抵达平台的报文依赖设备 QoS 1、Broker 会话保留/容量及其自身持久化设置。QoS 0、Broker 数据丢失、队列目录丢失、磁盘损坏和物理断电不因此获得额外保证。
- `IOT_DATA_DIR` 必须挂载持久磁盘。接收队列是本机持久化，不是跨主机复制；主机损坏后的接管需要恢复原数据盘。
- 保留 `New` / `NewWithCredentials` 供嵌入式调用和测试使用：它们保留旧的非持久内存队列、clean session 和自动 ACK 行为，不提供本节的重启保障；生产装配已改用持久构造函数，没有自动降级到旧构造函数。

## 消息主题管理

管理端“设备与接入 → 消息主题”用于给外部系统订阅业务数据。一个主题对应一份查询：在同一表单中设置 MQTT / Kafka 主题、业务数据、返回字段、查询条件、设备范围和授权订阅的开放接口密钥，保存后按约定时机发布。平台内置主题（内部队列、设备接入、解析结果和告警转发）只读展示，不能修改或停用。

### 创建数据订阅主题

1. 填写名称、协议和主题。例如 MQTT `/device` 或 Kafka `device`。
2. 选择“设备上报”“新告警”“告警恢复”或“告警确认”。默认发送全部业务字段，也可选择字段并设置输出名称，例如把 `properties.temperature` 输出为 `temperature`。
3. 设置查询条件及设备范围；条件支持全部满足（AND）或任一满足（OR）。
4. 预览输出并选择授权订阅的开放接口密钥（须开通“订阅消息主题”能力）。没有密钥时可先保存，再到“用户与权限 → 开放接口”创建。
5. 外部系统用该密钥换取临时连接凭据，连接并订阅后接收新消息。

实际主题包含租户前缀：MQTT `/iot/external/<租户十六进制>/`、Kafka `iot.external.<租户十六进制>.`。填写 `/device` 时按本租户后缀处理，也可直接填写本租户完整地址；以页面保存后的完整地址为准。MQTT 支持多级路径，禁止 `+` / `#` 通配符；Kafka 使用字母、数字、点、下划线和短横线。协议与地址创建后固定，查询、名称、说明、授权和启停可修改。Kafka 创建要求新的物理 Topic，已存在地址会拒绝，以免带入旧历史；分区和副本数使用集群默认值。MQTT 无需预建物理 Topic。

主题只供外部只读订阅，不允许外部发布或手动发送。同一主题的所有订阅者接收相同内容，不能在 Broker 中按各订阅者再次筛选设备；需要不同数据范围时创建不同主题。发布成功表示平台向 Broker 的发布调用成功，不代表外部系统已接收或处理。

### 数据、字段与条件

查询针对平台提供的业务数据集，不直接开放底层 PostgreSQL / ClickHouse 表或任意 SQL：

| 数据集 | 页面名称 | 发送方式 |
| --- | --- | --- |
| `device_reports` | 设备上报 | 每条成功解析的新上报 |
| `alarms` | 新告警 | 告警触发时 |
| `alarm_recoveries` | 告警恢复 | 告警恢复时 |
| `alarm_confirmations` | 告警确认 | 告警确认时 |
| `devices` | 当前设备信息 | 定时查询当前设备状态 |
| `alarms_current` | 当前告警记录 | 定时查询当前告警记录 |

实时模式对新业务事件筛选、投影后发布，不会在外部每次订阅时执行查询，也不会自动发送过去的数据或首次全量。解析失败的数据不会进入设备上报数据集。输出为 JSON 对象，默认保留数据集允许的完整业务字段；指定字段时按输出名称生成对象并保留 JSON 类型。字段支持点路径，如 `properties.temperature`、`tags.site`。当前设备信息的 `status` 是设备资料状态；在线情况使用 `online` / `connectionStatus`，尚未接入时 `online` 为 `null`。

条件支持 `=`、`!=`、`>`、`>=`、`<`、`<=`、`IN`、`NOT IN`、`CONTAINS`、`IS NULL`、`IS NOT NULL`，支持 AND / OR 和 SQL 中的括号组合。比较按实际类型执行，不把数字字符串静默转换成数值；图形表单中数字和布尔值按对应类型解析，数字形式的文本应加双引号（如 `"001"`）。动态对象可使用自定义子路径；静态业务字段不存在时拒绝保存；动态子字段缺失时投影为 `null`，普通 WHERE 比较不匹配（`IS NULL` 除外），应通过预览核对实际字段路径。最多 64 个返回字段、100 个条件、8 层条件、每个 IN 列表 100 项；生成消息最大 256 KiB。

高级设置提供 SQL 编辑，例如：

```sql
SELECT deviceId, timestamp, properties.temperature AS temperature
FROM device_reports
WHERE deviceId IN ('A', 'B') AND properties.temperature >= 26
```

SQL 是上述查询的另一种编辑方式，支持 SELECT 字段 / `*`、AS 别名、FROM 业务数据和 WHERE 条件，不支持任意函数、关联表、聚合、脚本和写入。包含特殊字符的字段路径使用双引号包住完整路径，如 `"properties.temp-c"`；字符串使用单引号，内部单引号写为两个单引号。最多 16 KiB。简单条件可预览后转换回表单；嵌套条件或超出 JavaScript 安全整数范围的值保持 SQL，避免转换丢失含义。设备范围和发送周期独立于 SQL 保存。

“数据预览”读取当前租户授权范围内的真实样本，也可填写 JSON 样例。无样本显示空态，不生成假数据；有样本但未匹配时明确提示。预览只计算，不保存、不发送。实时预览从有限数量的近期业务样本中寻找匹配，不是历史数据全文查询。

### 定时快照

高级设置中选择“定时快照”，业务数据切换为当前设备信息或当前告警记录。周期为 10–86400 秒，默认 60 秒。平台由现有 `jobs` 单实例调度每 5 秒检查到期查询；首次保存后在下一次调度发送当时的完整结果，实际间隔受调度和 Broker 调用耗时影响。

每次发送一条完整快照消息：

```json
{"dataset":"devices","generatedAt":1790985600000,"items":[{"deviceId":"A","name":"烟感 A","online":true}]}
```

`generatedAt` 为毫秒时间戳。没有匹配记录时也发送 `items:[]`，用于表达本次结果为空；这不是增量变更消息。最多扫描 10000 条、返回 1000 条匹配记录，消息不超过 256 KiB；超限或查询失败报错并不发送截断结果，应收窄条件、设备范围或拆分主题。进程重启或主节点切换可能重新发送最新快照，不承诺逐次恰好一次，也不补发停机期间的历史快照。

### 历史数据范围

主题累计记录曾配置的业务事件权限和设备范围，包括停用的查询。修改条件或缩小范围不能缩小已有历史数据的读取要求；需要更小订阅权限时新建主题。删除主题只移除平台配置和订阅授权，不清空 Broker 历史；已删除的地址不能被新主题复用。

### 订阅密钥与连接凭据

运维工具管理账号见[工具连接账号](DEPLOYMENT.md#工具连接账号)。外部业务订阅使用开放接口密钥和临时 Broker 凭据，不使用工具管理员账号。

1. 在“用户与权限 → 开放接口”创建密钥，绑定平台用户并勾选“订阅消息主题”。绑定用户须有消息主题菜单，且覆盖主题累计的业务权限和设备范围；设备数据需要设备管理菜单，告警需要告警及摄像头资料权限。主、子设备分别授权。
2. 在主题表单中授权该密钥订阅。修改授权需要开放接口密钥的编辑权限；授权时校验绑定用户是否覆盖主题全部历史范围。
3. 外部服务端用密钥换取临时凭据。MQTT 使用精确主题 JWT ACL；Kafka 使用 SCRAM-SHA-256、Topic READ / DESCRIBE 及该凭据独立消费组 READ。凭据不能登录平台管理端。

```http
POST /api/open/v1/message-topics/credentials
Authorization: Bearer <开放接口密钥>
Content-Type: application/json

{"protocol":"mqtt"}
```

响应包含 `protocol`、`broker`、`username`、`password`、`subscribeTopics`、`groupId`、`mechanism`、`securityProtocol`、`tls`、`expiresAt`。Kafka 订阅使用返回的精确消费组。临时凭据最长一小时，不晚于密钥到期时间；到期前 10 分钟内续期，保持仍有效的用户名和地址。MQTT 客户端使用新 JWT 重新连接。

修改主题、查询或授权会撤销当前租户已有凭据，需重新获取；密钥停用、轮换、到期、删除，以及绑定用户、角色和设备范围变更也使旧凭据失效。旧授权撤销未确认、权限快照发生变化或凭据过期时，主题自动发送暂停，直到完成撤销；实时消息不补发，定时查询恢复后发送当时快照。已发送和在途消息不会撤回。

撤销先持久标记、再调用 Broker，失败由 `jobs` 每 30 秒重试。MQTT 还依靠 JWT 到期断开；Kafka 依靠平台撤销 SCRAM 和 ACL，应持续运行 `jobs` 与管理 API。平台或 Broker 管理 API 故障期间，历史读取权限是否已撤销以实际 Broker 状态为准。

### 接口与权限

| 接口 | 行为 |
| --- | --- |
| `GET /api/v1/message-topics` | 主题、只读内置主题 `builtin`、业务数据目录 `datasets`、可授权密钥 `keys`、修订及运行状态；主题包含 `query`、`querySql`、`keyIds` |
| `POST /api/v1/message-topics` | 新增主题，原子保存查询及可选的订阅密钥授权 |
| `PUT /api/v1/message-topics/{id}` | 编辑主题、查询和可选订阅授权；不能修改协议/地址 |
| `POST /api/v1/message-topics/query/preview` | 保存前预览查询，返回规范化 `query`、`querySql`、`sampled`、`matched` 和字符串 `payload` |
| `DELETE /api/v1/message-topics/{id}?revision=N` | 删除主题及订阅授权，保留 Broker 历史并保留地址 |

主题保存正文为 `{revision,name,protocol,topic,enabled,description,keyIds}`，以及 `query` 或 `querySql`（二选一，新增时必填）。图形查询结构为 `{dataset,fields,filter,deviceScope,deviceIds,mode,intervalSeconds}`：`fields` 为输出名称到字段路径的映射，空对象表示全部字段；`filter` 为 `{logic:"and|or",children:[...]}` 分组或 `{field,operator,value}` 条件，操作符为 `eq/ne/gt/gte/lt/lte/in/not_in/contains/is_null/not_null`。SQL 方式通过 `queryOptions` 指定 `{mode,intervalSeconds,deviceScope,deviceIds}`。`mode` 为 `realtime` 或 `interval`，由数据集约束；实时周期为 0。`keyIds` 省略表示保留既有授权，空数组表示撤销全部授权。

查看需要 `menu:messageTopics`；管理与预览使用精确路由权限，并要求设备管理菜单和当前租户全部设备范围，查询另校验对应业务权限。租户由登录身份确定，正文不能切换租户。配置及撤销记录保存在 PostgreSQL `message_topic_configs`，使用 `revision` 乐观锁，冲突返回 409。实时发布按 2 秒缓存跳过没有匹配主题的租户，命中时读取持久策略；读取失败只跳过主题投递，不影响平台内置发布。Kafka 密码由服务端密钥及凭据 ID 派生。

`IOT_PUBLISH_EXTERNAL_TOPICS=false` 仍关闭 Kafka 解析事件，因此依赖它们的 Kafka 实时设备上报查询不触发；页面不覆盖部署开关。原文归档、标准消息存储和内部处理不受影响。

### Broker 就绪条件

MQTT 需对外地址 `IOT_DEVICE_MQTT_PUBLIC_URL`、有效 EMQX 管理凭据、JWT 认证链及仅含已知工具账号的可选密码认证、用户名绑定、JWT ACL、到期断开、监听器认证和默认拒绝规则。Kafka 需对外地址 `IOT_KAFKA_PUBLIC_BROKERS`、实际开启认证与 ACL，并配置能管理 SCRAM / ACL / Topic 的 Redpanda 管理账号；服务端检查实际配置、匿名访问拒绝和无全用户通配授权后才发凭据。Kafka 新主题创建也需要管理连接就绪。配置保存不代表 Broker 授权已生效。部署见 [Kafka 对接账号认证与授权](DEPLOYMENT.md#kafka-对接账号认证与授权)。

实现入口：`internal/messagetopics/`、`internal/httpapi/message_topics.go`、`internal/httpapi/message_topic_queries.go`、`internal/httpapi/message_topic_credentials.go`、`internal/adapters/kafka/consumer_admin.go` 和 `iot_front/src/views/MessageTopicsView.vue`。

## 外部数据接口管理

需要配置第三方原有接口、主动拉取或同时接收推送时，使用[外部数据接入](EXTERNAL_DATA.md)。该模块管理来源认证、字段转换、对象对应关系、持久接收和拉取任务；下文开放接口仍供使用固定平台格式的外部调用方使用。

## 开放接口

外部系统（园区平台、物业系统、上级监管平台等）通过开放接口查询与处置告警、上报设备消息（含告警）、查询设备数据、订阅消息主题，以及调用智能助手问答，例如在对方首页嵌入问答机器人。实现入口为 [open_api.go](../internal/httpapi/open_api.go)。

开放密钥只支持下表列出的能力；排班、灭火器和消防站管理通过登录用户的 `/api/v1` 接口使用，不在 `/api/open/v1` 的能力清单中。管理接口与权限见 [消防管理](FIRE_SAFETY.md#接口与权限)。

### 授权模型

- 密钥在“用户与权限 → 开放接口”创建，需要该页面的新增、编辑、删除操作权限。
- 每个密钥绑定一个平台用户。外部请求按该用户的菜单、操作权限和设备范围执行，与其登录控制台时一致；密钥的开放能力只能进一步收窄。停用或删除用户后，其密钥立即失效；删除用户会同时删除其密钥。
- 建议为每个外部系统单独建用户和角色，只授予需要的功能和设备。
- 密钥明文只在创建或轮换时返回一次，平台只保存 SHA-256 摘要，存于租户的 `platform_access` 配置中，需随独立整库备份保管；设备数据及 FULL 导出不包含账户和密钥。密钥可停用、设置有效期、轮换或删除；轮换保留密钥 ID、能力与主题授权，旧密钥及其签发的主题凭据立即失效。
- 密钥应只保存在对方服务端。浏览器页面中的问答机器人应由对方后端转发请求，不能把密钥下发到前端。

| 能力 | 可调用的接口 | 绑定用户还需要 |
| --- | --- | --- |
| `alarms:read` 查询告警 | `GET /alarms`、`GET /alarms/{alarmId}` | 告警中心菜单 |
| `alarms:handle` 处置告警 | `POST /alarms/{alarmId}/actions` | 告警中心菜单及“处置告警”操作 |
| `messages:read` 查询设备与数据 | `GET /devices`、`GET /devices/{deviceId}/latest`、`GET /devices/{deviceId}/properties/history` | 设备管理菜单 |
| `messages:report` 上报设备消息 | `POST /device-messages` | 设备管理菜单；设备在其设备范围内 |
| `ai:chat` 智能问答 | `GET /ai/workflows`、`POST /ai/chat`、`POST /ai/chat/stream` | 智能助手菜单及“发送提问” / “流式问答”操作 |
| `topics:subscribe` 订阅消息主题 | `POST /message-topics/credentials` | 消息主题菜单；覆盖所订阅主题的历史数据范围（见[消息主题管理](#订阅密钥与连接凭据)） |

### 调用约定

- 基础路径：`{平台地址}/api/open/v1`。
- 认证：`Authorization: Bearer <密钥>`，或 `X-API-Key: <密钥>`。
- 请求和响应均为 JSON；错误响应沿用 `{"status","detail"}` 格式。401 表示密钥无效、停用、过期或绑定用户不可用；403 表示密钥缺少能力，或绑定用户没有对应权限或设备访问权限。
- 限流：每个密钥每秒 100 次请求；每台设备的上报与设备自身上报共享每秒 20 次额度。超限返回 429 和 `Retry-After`。配置 Redis 时多副本共享额度（Redis 故障时按 `IOT_CLUSTER_INSTANCES` 均分退化），未配置时按进程计算。
- `GET /me` 不要求能力项，返回租户、密钥、绑定用户、能力和设备范围，可用于联调。

### 上报设备消息

```http
POST /api/open/v1/device-messages
Authorization: Bearer <密钥>
Content-Type: application/json

{"messages":[
  {"deviceId":"smoke-101","kind":"property","id":"ext-20260926-0001","timestamp":1790000000000,"data":{"temperature":26.5,"battery":88}},
  {"deviceId":"smoke-101","kind":"state","id":"ext-20260926-0002","timestamp":1790000000000,"online":true},
  {"deviceId":"smoke-102","kind":"event","id":"ext-20260926-0003","timestamp":1790000000000,"event":"self-test","data":{"result":"ok"}}
]}
```

- 每批 1 至 100 条，请求体最多 1 MiB。`kind` 为 `property`、`event`、`alarm` 或 `state`；字段含义同[标准报文](#http--mqtt-标准报文)。
- 设备须已在平台登记、已启用、模板已启用，且在绑定用户的设备范围内。外部系统不能自行指定租户、协议或解析器。
- 消息按内置标准协议写入原始报文，经解析、存储、规则和告警链路处理，原文来源记为 `open-api`。
- `id` 在同一设备和 `kind` 下唯一：相同内容重试返回 `created:false`，同一 `id` 换内容返回 `MESSAGE_CONFLICT`。
- 全部接受返回 202；部分接受返回 207；全部被拒绝返回 422（全部限流或暂停接收时返回 429）。`results` 逐条给出 `status`、`messageId` 或 `errorCode`，错误码包括 `INVALID_MESSAGE`、`DEVICE_NOT_FOUND`、`DEVICE_DISABLED`、`RATE_LIMITED`、`BACKPRESSURE`（平台处理积压超过上限，稍后重试）、`MESSAGE_CONFLICT`、`INGEST_FAILED`。
- 202 表示已进入原始接收链路，解析与告警为异步处理。

### 上报告警

告警作为 `kind:"alarm"` 的设备消息上报，例如 `{"deviceId":"smoke-101","kind":"alarm","id":"alarm-20260926-0001","timestamp":1790000000000,"data":{"alarmType":"FIRE","alarmLevel":"CRITICAL","content":"3 层东侧走廊烟感报警"}}`。告警生成、等级及类型默认值遵守 [标准告警报文](#http--mqtt-标准报文)；生成告警的 `triggerId` 为 `msg_` 加返回的 `messageId`，可用 `GET /alarms?deviceId=...` 查找。通过 [标准告警恢复属性](#http--mqtt-标准报文) 上报恢复，或调用下述处置接口的 `RECOVERED`。

### 查询与处置告警

- `GET /alarms` 支持 `deviceId`、`status`（`ACTIVE`、`ACKED`、`RECOVERED`、`CLOSED`）、`level`、`source`、`start`、`end`（毫秒时间戳）及 `page`、`pageSize` 分页，返回 `items`、`total`。只返回绑定用户可见设备的告警。
- `POST /alarms/{alarmId}/actions`，正文 `{"action":"ACKED"}`。`ACKED` 确认活动告警，`RECOVERED` 恢复活动或已确认告警，`CLOSED` 关闭告警；处置后重算设备状态，并记录操作人为“绑定用户 (API 密钥名)”。

### 查询设备与数据

- `GET /devices`：设备清单及运行状态，筛选与分页参数同控制台设备列表。
- `GET /devices/{deviceId}/latest`：设备状态与最新一条标准消息。
- `GET /devices/{deviceId}/properties/history?property=temperature&start=...&end=...`：属性历史，分页返回。

### 智能问答

```http
POST /api/open/v1/ai/chat

{"question":"3 号楼今天有哪些未处理告警？","conversationId":"park-user-1024"}
```

- 返回 `{"runId","workflowId","model","answer"}`；`answer` 为 Markdown。`POST /ai/chat/stream` 以 SSE 返回 `run.started`、`text.delta`、`tool.started`、`tool.completed`、`run.completed`、`run.failed` 事件，每个事件为 `event: <类型>` 与 `data: <JSON>`。
- 可选 `workflowId` 选择聊天智能体，可用列表见 `GET /ai/workflows`。模型由平台模型管理决定，请求不能覆盖。
- 问答作为 Harness 工作流运行，启用知识时在首次模型请求前附带授权检索证据；检索和工具均受绑定用户的当前权限及设备范围限制，权限变化使待发送输入失效。Harness 不可用时返回 503，完整知识策略见 [AI 与知识库](PLATFORM.md#ai-与知识库)。
- `conversationId` 由对方系统为其每个终端用户生成，平台按租户、绑定用户和该值隔离多轮会话；不同终端用户须使用不同值。

## 内置协议示例

各目录都是独立 Go module，进入目录运行 `go test ./...`，打包源码上传平台；根 module 测试不覆盖它们。修改后递增版本，先校验再切换产品，源码更新不会改写已经发布的 Worker。

| 目录 | 识别与连接 | 重点 |
| --- | --- | --- |
| `protocol-packages/gb26875-dahua` | TCP/UDP；`gb26875_<6 字节源地址小写HEX>` | 注册、保活、部件火警/故障、校时；BCD 按 UTC+08:00 |
| `dev/fb2018` | TCP；6 字节小端源地址，`fb2018_<十进制>` | 系统/部件/传输装置状态、火警及故障 |
| `dev/fb2024` | TCP；6 字节小端源地址，`fb_<十进制>` | 开关、模拟量及传输装置状态，不把非零值一律当火警 |
| `dev/fb-hydraulic` / `dev/fb-liquid-level` | TCP；15 位 IMEI | 压力/液位、电池、信号、阈值、历史值、报警及写参数 |
| `dev/kuka-modbus` | 主动 TCP，预先绑定设备 ID | `coil-0/3001/3002/3003/3013/3042` 六个查询，功能码 01、站号 1 |
| `dev/sp-cannon` | 主动 TCP，预先绑定设备 ID | `host`、`fault-1`～`fault-8`、`status-1`～`status-8`，功能码 04、站号 1 |

FB/液压/液位用监听模式，不同协议不能共用监听端口；全零源地址会造成身份冲突，须先配置唯一地址或独立主动连接身份。液压/液位校验 RTU CRC，支持 IMEI 后多个连续帧；参数单位沿用原寄存器的百分之一单位。KUKA/水炮使用保持连接的串行查询，设备必须支持，目标地址受白名单限制。多对象放在 `properties.objects`，独立部件报警/恢复使用 `event.components`，不是跨设备授权。

GB26875 用模板接入点监听 26875（TCP/UDP 分别配置），普通报文归档成功后回复同流水号 ACK，ACK 不再触发 ACK。设备在线时可发送 `{"type":"time-sync","confirmed":true}`，可选 `timestamp` 为 Unix 毫秒。在示例目录执行 `go run ./cmd/simulator --address 127.0.0.1:26875 --network tcp --hold 1m` 验证注册、火警与校时；会产生测试告警，只用测试产品。多部件报文不以首个对象代表整帧位置；已有版本需发布新 Worker，历史无部件 ID 的告警不自动拆分。

## 验证入口

接入用例集中在 `internal/onboarding`、`internal/protocolruntime`、`internal/parser` 和 `internal/httpapi`；数据库契约在 `internal/repositorytest` 中由 Memory / PostgreSQL 实现共同执行。具体回归命令、浏览器夹具与真实依赖条件见 [开发与测试](DEVELOPMENT.md)。现场成功需核对当前产品、设备、接入点、协议版本和现场原文；模拟、回放、保存成功或 Broker ACK 都不替代现场解析。
