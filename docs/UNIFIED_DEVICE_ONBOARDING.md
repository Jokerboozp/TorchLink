# 统一设备接入

## 从哪里开始

日常新增统一从 **设备管理 → 接入设备** 开始。工作区按“选择设备模板 → 填写设备信息 → 完成连接设置 → 检查设备数据”推进；熟练用户仍可使用设备列表的“快捷添加”。设备模板沿用现有产品模型，表示同型号或共用通信协议的一套配置。同为烟感、液位计等分类并不表示能共用协议。

- **已有型号**：选择已启用的设备模板，填写设备名称、真实设备编号及必要连接。模板继承已发布协议、通信方式和数据格式。编号类型及查找位置只有在模板元数据已填写时才明确显示；缺失时需核对厂家协议与铭牌。标准 HTTP/MQTT 可显式选择平台生成编号，并把该编号配置到现场设备的上报地址或 Topic；其他协议须填写实际标识。
- **新型号**：在同一工作区创建模板，只能选择标准 HTTP/MQTT 或已发布的设备通信协议。没有匹配协议时可在向导抽屉中用真实报文或点表生成、校验并发布协议；复杂 Go 源码仍从“设备通信协议”独立页面开发。缺少资料时先保留向导草稿，向厂家索取型号、协议说明、真实报文样例或点表。系统不会从任意厂家资料自动推断正确协议。
- **子设备**：选择已关联平台连接配置的主设备及其协议映射的子设备模板，填写协议地址和类型；平台通过稳定的租户、主设备和地址关系登记，同一请求重试不会重复建档。已登记列表同时显示协议自动发现和人工登记的子设备；只有主设备协议支持时才会自动发现，没有记录时如实显示为空。子设备继承主设备网络连接，不另建监听或采集实例。

非标准协议先选择适用的“平台连接配置”。TCP/UDP 共享监听可供多台设备使用；监听 IP（例如 `0.0.0.0`）只供平台绑定，现场设备使用连接配置中的“平台对外地址”和端口。主动 TCP/Modbus 连接则在平台填写设备地址、端口与站号。缺少连接时可在向导抽屉中创建或补齐对外地址；需先登记设备的主动连接可保存设备后再配置并关联。修改共享连接不会自动删除或改写其他设备。

保存后以通过、等待、失败图标显示配置保存、接收服务、当前设备原文、解析结果与持续更新五项检查；详细判定可展开“了解详情”。**保存成功、TCP 会话、MQTT Broker 接收和协议样例校验均不等于现场解析成功。**本次检查从当前设备及其配置的保存时间开始，只采信当前产品、设备、连接和协议版本匹配的现场接收原文；管理端模拟发送、回放和历史数据不会计为本次成功。收到原文但解析失败时可查看原文并核对协议。一次解析成功仍需等待后续上报，至少两次成功解析且最新数据不超过 15 分钟才显示持续更新。刷新检查只查询数据，不发送报文或触发告警。

工作区草稿按租户和登录用户保存在当前浏览器，含步骤、非敏感输入和已创建资源 ID。Secret 和 MQTT token 不写入草稿；Secret 只在首次创建或轮换时显示，丢失后到连接详情重新生成。离开再进入设备管理时自动恢复草稿；若模板、设备或连接被删除或权限变化，会显示对应提示。历史接入结果仍可在设备连接详情查看，但不能替代本次检查。

[返回 README](../README.md) · [Go 协议开发](GO_PROTOCOL_PACKAGES.md) · [TCP 与主子设备](TCP_CHILD_DEVICE_ACCESS.md)

## 选择接入方式

设备新增使用设备管理中的单页工作区；日常维护仍可直达各资源页面：

| 菜单 | 管理内容 |
| --- | --- |
| 设备管理 → 接入设备 | 选择模板、登记现场设备、关联连接、检查本次数据 |
| 设备模板 | 维护共用协议、厂商型号和编号说明；“协议版本”用于绑定与回滚 |
| 平台连接配置 | 维护 TCP / UDP 监听与主动采集、对外地址、定时读取及子设备映射 |
| 设备通信协议 | 上传报文、点表或 Go 源码，校验并发布版本 |
| 模拟设备测试 | 进入页面自动准备测试设备，手动发送模拟报文，不用于现场接通判断 |

“平台连接配置”复用原接入实例的配置与运行机制；接口及内部标识保持不变。一个模板可以配置多个连接，监听型配置可接入多台设备，主动连接型配置绑定目标设备。现场实体主设备仍在设备管理中登记。

标准 HTTP / MQTT 产品可直接选择“标准设备上报”，无需先上传源码。创建产品时服务端准备当前租户的标准协议版本；登记设备后返回一次性凭证。已发布 Go 版本可直接用于创建产品并建立绑定，无需先有设备或实例。编辑现有产品的协议版本使用专门的“协议版本”操作，保留回滚和运行时兼容检查。

| 方式 | 准备内容 | 平台设备凭据 |
| --- | --- | --- |
| HTTP / MQTT 标准上报 | 设备实现下文标准 JSON；MQTT 需可达的 Broker | AccessKey 和 Secret |
| Modbus TCP | 目标地址、端口、站号、点表及采集周期 | 不需要 |
| Modbus RTU over TCP | 串口服务器地址、端口、站号及 RTU 点表 | 不需要 |
| Go TCP / UDP | 产品绑定已发布协议；配置监听或 TCP 主动连接 | 不需要；协议自行完成身份识别与认证 |
| 主设备下的子设备 | 主设备平台连接配置和子类型到产品的映射 | 不需要；沿主设备协议接入 |

简单 JSON 路径或固定 HEX 映射见 [配置驱动协议](CONFIGURABLE_PROTOCOLS.md)；专用协议使用 Go 源码。摄像头从独立的摄像头管理入口维护，见 [视频集成](VIDEO_SDK_ADAPTER.md)。

设备连接详情继续展示日常配置、运行状态、历史原文与解析结果。新增设备的本次验证先看“接入设备”工作区；HTTP/MQTT 获得凭据后再发送首条数据，TCP/UDP 按协议连接并上报。配置保存成功不能证明设备在线。

## 测试、保存与诊断

管理端登记设备使用 `/api/v1/device-registry`，不再要求完成六步向导。以下 `onboarding/test` 与 `onboarding` 的样例校验、令牌与幂等约定属于保留的原子接入 API；它们与独立资源管理接口是不同的调用流程。

- HTTP 检查样例格式与解析；MQTT 另以独立连接检查平台到 Broker 的认证与网络。
- TCP/UDP 检查监听配置或主动连接，并用实际 ingress/decode 验证样本。平台侧检查不代表外部设备的路由、防火墙和协议认证已完成。
- Modbus 按点表读取一次并展示响应与解析结果；持续轮询从保存启用后开始。出站目标受 `IOT_MODBUS_ALLOWED_CIDRS` 限制。
- 测试不写入属性、规则和告警。失败结果保留错误类别、请求/响应及解析信息；页面调整参数后应重新测试。

测试成功返回有效期 10 分钟的 `testToken`，创建请求必须携带同一份配置和令牌。令牌绑定租户、配置和协议内容；修改参数或协议后须重新测试。

创建按租户与设备 ID 幂等：首次成功 201，相同请求重试 200 且 `reused:true`，配置冲突 409。重复请求不再生成产品、设备或 Secret；HTTP/MQTT 首次响应丢失时需在设备详情轮换凭据。没有向导请求摘要的历史设备不会被当作可恢复的新建请求。

连接详情通过 `accessInfo`、`ingest`、`profiles` 展示接入参数、原文 ID、接收时间、解析时间、错误和标准消息。多实例设备可用 `?profileId={id}` 选择实例；仅产品相同不能推断会话归属。原文详情、下载和回放使用 `rawMessageId`，不是标准消息 ID。

## HTTP / MQTT 标准报文

标准协议固定为 `iot-standard@1.0.0`。推荐 envelope 包含 `version:"1.0"`、唯一 `id` 和正数毫秒 `timestamp`，按上报类型提供以下字段：

| kind | 业务字段 | 标准消息类型 |
| --- | --- | --- |
| `property` | 非空对象 `data` | `PROPERTY_REPORT` |
| `event` | 顶层 `event` 名称，可附 `data` | `EVENT_REPORT` |
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

请求体最多 64 KiB、嵌套最多 16 层；重复 JSON 键、未知 version 和超前平台时间五分钟以上的设备时间被拒绝。历史时间可用于补传，晚到消息不回退最新状态。不带 version 的旧 `data.connectionStatus`、`data.commandId` / `data.success` 格式仍可读取；顶层和 data 同时提供的对应字段不能冲突。

普通 event 名称不会自动变为设备告警。协议直接输出 `ALARM_REPORT` 时可形成设备来源告警；部件状态使用 `data.components`，具体火警、故障与恢复契约见 [部件告警](DEVICE_RECEIVE_RELIABILITY.md#部件状态契约)。

### HTTP 上报

```http
POST /api/v1/device-ingest/standard/{tenant}/{product}/{device}/{kind}
Content-Type: application/json
X-Device-Key: <设备 AccessKey>
X-Device-Secret: <设备 Secret>
```

成功返回 202、`messageId`、`created` 和 `status:ACCEPTED`，表示原始接收链路接受请求，异步解析和规则结果须继续查询。标准凭据不能通过旧 RawMessage 接口自选租户、Parser 或协议。

同一租户、产品、设备、kind 和消息 ID 的重试须保持正文逐字节一致；HTTP/MQTT 重传使用同一原文 ID。不同正文返回 `409 MESSAGE_CONFLICT`，新的读数使用新 ID。每进程每设备上报限流为每秒 20 次，多副本不共享额度。

### MQTT 上报

携带上述两个凭据头调用 `POST /api/v1/device-mqtt/token`，用返回的 `username` 和 `token` 作为 MQTT 用户名和 password，Client ID 使用接入指南给出的值。标准设备 JWT 有效期 300 秒，到期前重新取令牌并重连。

| 用途 | Topic |
| --- | --- |
| 属性 | `/iot/up/{tenant}/{product}/{device}/property` |
| 事件 | `/iot/up/{tenant}/{product}/{device}/event` |
| 状态 | `/iot/up/{tenant}/{product}/{device}/state` |
| 命令回执 | `/iot/up/{tenant}/{product}/{device}/command-reply` |
| 订阅下行 | `/iot/down/{tenant}/{product}/{device}/command` |

标准设备 ACL 只授予自身上行与下行主题；平台拒绝 retained 上行，并重新检查设备和产品启用状态。HTTP/MQTT 的设备连接状态来自标准 state 上报，不是 Broker 实时在线查询。

设备收到 PUBACK 只表示 Broker 接收。平台运行连接先写本机持久队列再确认投递，随后归档与解析；队列容量、隔离记录和磁盘持久性见 [MQTT 接收保障](DEVICE_RECEIVE_RELIABILITY.md#mqtt-接收保障)。

## 凭据与命令

日常操作位于“设备管理 → 连接详情 → 设备控制”，根据产品物模型的 `commands[].fields` 生成文本、数值、布尔、对象或列表参数输入项，仅允许选择已定义命令。MQTT 参数提交到 `data`，Go 协议参数作为命令对象的顶层字段传给 encode，`type` 为命令标识；协议实现须与产品命令定义一致。

模拟设备测试页面不提供原始命令调试。已定义命令仍可在设备连接详情中执行；未定义命令的产品不会开放自由输入。服务端继续要求人工确认，并检查菜单、操作与设备范围。


Secret 仅首次创建或轮换返回，服务端保存哈希；详情返回 `credentialSupported` 标识。TCP、UDP、Modbus 与子设备不生成平台 Secret，调用凭据接口返回 422。显式设备通信方式优先于产品默认值；旧协议设备曾生成的密钥不能绕过当前认证检查。

HTTP Secret 在轮换后立即失效。MQTT 已签发令牌的及时撤销需配置 `IOT_EMQX_API_URL`、`IOT_EMQX_API_KEY`、`IOT_EMQX_API_SECRET`；URL 是 Broker 管理根地址，不带 `/api/v5`。凭据变更和撤销意图一同保存，平台封禁旧 username 并断开其会话；失败保留 PENDING，每 30 秒重试。未配置管理适配器时不能把轮换等同于即时断连，仍依赖 JWT 到期与 Broker 的到期断连。配置见 [EMQX 认证与授权](../ops/emqx/PRODUCTION_SECURITY.md)。

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

Go TCP/UDP 命令同样要求 `confirmed:true`，另需 encode 能力和有效会话；接口、关联 ID 及超时行为见 [协议调用契约](GO_PROTOCOL_PACKAGES.md#操作样例与监听-api)。

## 管理 API

控制面使用登录令牌，并检查租户、菜单、操作及用户设备范围。设备详情、凭据和命令只允许访问授权设备；平台连接配置与接入测试涉及全租户配置，要求设备管理菜单、全部设备范围及对应菜单和操作权限。配置规则见 [用户权限](USER_ACCESS_CONTROL.md)。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/v1/connectors/types` | 当前通信能力 |
| POST | `/api/v1/onboarding/test` | 测试与解析预览 |
| POST | `/api/v1/onboarding` | 原子保存并启用 |
| GET | `/api/v1/connectors` | 平台连接配置、运行状态与会话 |
| GET | `/api/v1/device-registry/{id}/connection` | 设备连接和解析详情 |
| POST / DELETE | `/api/v1/device-registry/{id}/credentials` | 轮换 / 禁用受管凭据 |
| GET | `/api/v1/device-registry/{id}/history?kind=connection` | 连接历史；`kind=event` 查询事件 |
| GET | `/api/v1/device-registry/{id}/commands` | 命令与回执记录 |

请求结构见 [onboarding/service.go](../internal/onboarding/service.go)，分页约定见 [列表与分页](TECHNICAL_DETAILS.md#列表与分页)。API 仍可保存 `thingModel` 以描述 properties/events/commands 及参数类型；产品页面不再提供原始 JSON 编辑框，编辑产品时保留已有模型。

## 测试设备

**模拟设备测试** 只保留测试设备：有准备权限的用户打开页面时自动创建或读取租户内固定的测试设备及关联模板、协议，重复进入不会重置已有设备或本地报文草稿。准备失败可点击“重新准备测试设备”重试；“恢复默认配置”才会重置模板。模拟报文可检查解析与告警逻辑，但不能证明现场设备已接通。新设备通过“设备管理 → 接入设备”创建。

## 验证入口

在仓库根目录按需运行：

```bash
go test ./internal/onboarding ./internal/httpapi
go test -race ./internal/protocolruntime ./internal/adapters/mqtt
```

| 实际集成环境 | 环境变量 | 测试命令 |
| --- | --- | --- |
| PostgreSQL 临时 schema | `IOT_TEST_POSTGRES_DSN` | `go test ./internal/adapters/postgres -run TestDeviceOperationsMigrationAndAtomicity -count=1 -v` |
| MQTT Broker | `IOT_TEST_MQTT_BROKER`、`IOT_TEST_MQTT_JWT_SECRET`；严格身份另设 `IOT_TEST_MQTT_STRICT_IDENTITY=true` | `go test ./internal/httpapi -run TestStandardMQTTLiveBroker -count=1 -v` |

Broker 撤销子用例另需 `IOT_TEST_EMQX_API_URL`、`IOT_TEST_EMQX_API_KEY`、`IOT_TEST_EMQX_API_SECRET`。

浏览器检查应覆盖“设备管理 → 接入设备”、连接详情和窄屏布局。已有 `TestOnboardingBrowser` 调用 `iot_front/tests/browser/onboarding-check.mjs`，属于可选的隔离测试环境检查；普通单元测试通过不代表浏览器或真实设备验收。

测试使用隔离业务仓库或临时 schema、随机身份和非 retained 消息；凭据通过环境变量安全注入。缺少环境的集成分支会跳过。模拟器与受控故障测试不能代替厂商真机、固件补传或生产网络验收。


## 从报文或点表生成协议

在「协议管理 → 协议生成」上传报文或 Excel / CSV 点表，编辑字段映射，用真实样本预览，再保存、发布并绑定产品。文件格式、大小、地址基准及接口统一见 [配置驱动协议](CONFIGURABLE_PROTOCOLS.md#从报文或点表生成协议)。变长、会话或厂商专用协议使用 [Go 源码包](GO_PROTOCOL_PACKAGES.md)。

## 设备分组与可见性

设备管理分为「独立设备」「主设备」「子设备」三个标签，对应 `DIRECT/GATEWAY/CHILD`。设备类型筛选来自产品分类，按当前分组和类型筛选后分页；新增设备默认沿用当前标签角色。接收实时消息时只提示「有新数据」，点击刷新后更新列表。

主设备是实体设备，平台连接配置是产品的软件连接配置，二者分别管理。普通用户仅看到授权设备及其告警，主子关系不自动传递权限；受限设备的连接详情不展示共享网关和其他设备会话。
