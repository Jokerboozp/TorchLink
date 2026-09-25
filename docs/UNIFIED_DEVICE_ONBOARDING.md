# 统一设备接入

## 从哪里开始

新增设备统一从 **设备管理 → 添加设备** 开始，按“选择型号 → 设备与连接 → 现场配置与验证”三步完成。设备模板沿用现有产品模型，表示同型号或共用通信协议的一套配置；同为烟感、液位计等分类并不表示能共用协议。

- **已有型号**：选择已启用的设备模板。选中后平台做接入预检，给出接入方式（标准上报、HTTP 接口上报、TCP / UDP 监听、平台主动连接或 Modbus 定时采集），以及模板、协议、对外地址、Broker、可用接入点等检查项。预检不写入资源，也不连接现场设备。
- **新型号**：在第一步填写模板名称、分类和通信协议，模板与第一台设备在同一请求中创建。新模板只能选择内置标准设备上报或已发布的协议版本；没有匹配协议时可在向导抽屉中用报文或点表生成并发布，复杂协议在“设备通信协议”上传 Go 源码。缺少资料时草稿会保留，可向厂家索取型号、协议说明、真实报文或点表。系统不会从任意厂家资料推断协议。
- **子设备**：在主设备的“详情 → 子设备”中点击“添加子设备”，选择主设备接入点已映射的子设备类型并填写协议地址。平台按租户、主设备和地址登记，同一地址重试不会重复建档；子设备沿用主设备连接，不另建监听或采集。子设备类型映射在设备模板的“接入点”中配置。

第二步填写设备名称和编号，并按接入方式填写连接：

- 标准上报选择 MQTT 或 HTTP 通道。标准上报和 HTTP 接口上报的设备可以使用平台生成的编号，并把它配置到设备的上报地址或 Topic。
- TCP / UDP 协议选择模板已有的共享监听，或新建共享监听：填写现场设备可访问的平台对外地址和端口；本机监听地址默认 `0.0.0.0`，只供平台绑定。TCP 协议也可以选择“平台主动连接设备”，填写设备地址和端口。
- Modbus 协议填写设备地址、端口（默认 502）和站号（默认 1）。

除标准上报和 HTTP 接口上报外，设备编号须与协议从报文中识别出的标识一致。新建模板需要设备模板的新增权限，新建共享监听需要平台接入点的新增权限。

保存后进入第三步。页面给出现场设备需要填写的地址、通道、AccessKey 和示例报文，并支持“复制全部”；标准上报和 HTTP 接口上报设备的 Secret 只在这一步显示一次。验证区每 5 秒刷新一次，收到该设备的实时消息时也会刷新，显示后端给出的接入结论，以及配置保存、接收服务、收到原始报文、解析结果、持续上报五项检查。**保存成功、TCP 会话、MQTT Broker 接收和协议样例校验均不等于现场解析成功。**本次检查从设备创建及其配置的最近修改时间开始，只采信当前产品、设备、接入点和协议版本匹配的现场原文；管理端测试报文、回放和历史数据不计为本次成功。收到原文但解析失败时可查看原文并核对协议。一次解析成功仍需等待后续上报：至少两次成功解析，且最新数据不超过 15 分钟，才显示持续更新。

向导草稿按租户和登录用户保存在当前浏览器，只含步骤和非敏感输入；Secret 和 MQTT token 不写入草稿，丢失后在设备详情重新生成凭据。离开后再次点击“添加设备”会恢复草稿；点击“完成”或从第三步返回列表后草稿清除。历史接入结果仍可在设备详情查看，但不能替代本次检查。

[返回 README](../README.md) · [Go 协议开发](GO_PROTOCOL_PACKAGES.md) · [TCP 与主子设备](TCP_CHILD_DEVICE_ACCESS.md)

## 选择接入方式

| 菜单 | 管理内容 |
| --- | --- |
| 设备管理 → 添加设备 | 选择型号、登记设备和连接、确认本次数据 |
| 设备管理 → 详情 | 连接与数据、接入结论、子设备、凭据与设备控制 |
| 设备模板 | 模板详情包含“基本信息”“协议版本”（绑定与回滚）和“接入点”（共享监听、主动连接、Modbus 采集、定时读取与子设备映射） |
| 设备通信协议 | 上传报文、点表或 Go 源码，校验并发布版本 |
| 模拟设备测试 | 进入页面自动准备测试设备，手动发送模拟报文，不用于现场接通判断 |

接入点即原“平台连接配置”，复用接入实例的配置与运行机制，接口 `/api/v2/device-access-profiles` 和内部标识不变。一个模板可以有多个接入点：共享监听可接入多台设备，主动连接和 Modbus 采集绑定单台设备，通常在添加设备时自动创建。侧栏不再单列接入点；只有“平台接入点”菜单权限、没有设备模板菜单权限的账号，仍可从侧栏进入独立页面。

标准 HTTP / MQTT 模板直接选择“标准设备上报”，无需上传源码；租户的标准协议版本在创建模板或添加设备时自动准备。已发布的协议版本可直接用于新建模板并建立绑定。修改已有模板的协议版本在模板详情的“协议版本”中进行，保留回滚和运行时兼容检查。

| 方式 | 准备内容 | 平台设备凭据 |
| --- | --- | --- |
| 标准上报（HTTP / MQTT） | 设备实现下文标准 JSON；MQTT 需可达的 Broker | AccessKey 和 Secret |
| HTTP 接口上报 | 模板绑定传输方式为 MQTT / HTTP 的已发布协议；设备调用 `POST /api/v1/device-ingest/{deviceId}`，正文为 `{"payload": ...}`，平台按模板协议解析 | AccessKey 和 Secret |
| Modbus TCP | 已发布的 Modbus 协议版本（含点表）；设备地址、端口、站号 | 不需要 |
| Modbus RTU over TCP | 已发布的 RTU 协议版本；串口服务器地址、端口、站号 | 不需要 |
| Go TCP / UDP | 模板绑定具备 ingress 能力的已发布 Go 协议；共享监听或 TCP 主动连接 | 不需要；协议自行完成身份识别与认证 |
| 主设备下的子设备 | 主设备接入点中子设备类型到模板的映射 | 不需要；沿主设备协议接入 |

新的 Modbus 点表通过“设备通信协议”发布为协议版本，向导不再接受内联点表。简单 JSON 路径或固定 HEX 映射见 [配置驱动协议](CONFIGURABLE_PROTOCOLS.md)；专用协议使用 Go 源码。摄像头从独立的摄像头管理入口维护，见 [视频集成](VIDEO_SDK_ADAPTER.md)。

## 预检、保存与诊断

- `GET /api/v1/onboarding/preflight?productId={id}` 返回已有模板的接入方式、协议摘要、可用共享监听（含运行状态）和检查项；`ready=false` 时不能添加设备。新模板用 `protocolPackageId`、`transport`、`category` 代替 `productId`。
- `POST /api/v1/onboarding` 在一个事务中保存设备，并按需创建模板、协议绑定和接入点，任何一步失败都不留下部分资源。请求需带客户端生成的 `requestId`；`connection.mode` 取 `standard`、`managed`、`listener`、`dial` 或 `poll`，须与预检给出的接入方式一致。请求结构见 [onboarding/enroll.go](../internal/onboarding/enroll.go)。

幂等按租户和设备编号判断：首次成功返回 201；相同请求重试返回 200 和 `reused:true`，不再返回 Secret；同一编号的不同请求返回 409。并发的相同请求只创建一台设备、生成一个 Secret。共享监听端口已被占用，或模板、协议绑定在保存前发生变化时返回 409，刷新后重试。主动连接和 Modbus 目标若填写 IP，须在 `IOT_MODBUS_ALLOWED_CIDRS` 允许的网段内。原 `POST /api/v1/onboarding/test` 测试令牌流程已移除。

添加设备使用“新增设备”（`POST /api/v1/device-registry`）操作权限，不单列权限项。同时新建模板还需要设备模板的新增权限，新建共享监听还需要平台接入点的新增权限。只授权部分设备的账号不能添加设备，也不能调用预检。

`GET /api/v1/device-registry/{id}/connection` 返回 `diagnosis`，包括 `stage`、`tone`、`title`、`nextAction` 和五项 `checks`。结论按“模板与协议 → 设备 → 主设备 → 接入点 → 对外地址 → 解析 → 持续上报”的顺序给出最先需要处理的问题。传入 `since` 时只采信该时间之后的现场原文。以下来源计为现场数据：`device-http`（凭据认证的 HTTP 接口上报）、`standard-http`、`standard-mqtt`、Go 协议监听和 Modbus 采集；管理端测试报文和回放不计入。多接入点设备可用 `?profileId={id}` 选择。原文详情、下载和回放使用 `rawMessageId`，不是标准消息 ID。

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

日常操作位于“设备管理 → 详情 → 设备控制”，根据产品物模型的 `commands[].fields` 生成文本、数值、布尔、对象或列表参数输入项，仅允许选择已定义命令。MQTT 参数提交到 `data`，Go 协议参数作为命令对象的顶层字段传给 encode，`type` 为命令标识；协议实现须与产品命令定义一致。

模拟设备测试页面不提供原始命令调试。已定义命令仍可在设备详情中执行；未定义命令的产品不会开放自由输入。服务端继续要求人工确认，并检查菜单、操作与设备范围。


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

控制面使用登录令牌，并检查租户、菜单、操作及用户设备范围。设备详情、凭据和命令只允许访问授权设备；接入点和添加设备涉及全租户配置，要求设备管理菜单、全部设备范围及对应菜单和操作权限。配置规则见 [用户权限](USER_ACCESS_CONTROL.md)。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/v1/connectors/types` | 当前通信能力 |
| GET | `/api/v1/onboarding/preflight` | 接入预检 |
| POST | `/api/v1/onboarding` | 添加设备，可同时创建模板和接入点 |
| GET | `/api/v1/device-registry` | 设备列表；`role`、`category`、`productId`、`q`、`status`、`runtime` 在服务端先筛选再分页 |
| GET | `/api/v1/connectors` | 接入点、运行状态与会话 |
| GET | `/api/v1/device-registry/{id}/connection` | 设备连接和解析详情 |
| POST / DELETE | `/api/v1/device-registry/{id}/credentials` | 轮换 / 禁用受管凭据 |
| GET | `/api/v1/device-registry/{id}/history?kind=connection` | 连接历史；`kind=event` 查询事件 |
| GET | `/api/v1/device-registry/{id}/commands` | 命令与回执记录 |

添加设备的请求结构见 [onboarding/enroll.go](../internal/onboarding/enroll.go)，分页约定见 [列表与分页](TECHNICAL_DETAILS.md#列表与分页)。API 仍可保存 `thingModel` 以描述 properties/events/commands 及参数类型；产品页面不再提供原始 JSON 编辑框，编辑产品时保留已有模型。

## 测试设备

**模拟设备测试** 只保留测试设备：有准备权限的用户打开页面时自动创建或读取租户内固定的测试设备及关联模板、协议，重复进入不会重置已有设备或本地报文草稿。准备失败可点击“重新准备测试设备”重试；“恢复默认配置”才会重置模板。模拟报文可检查解析与告警逻辑，但不能证明现场设备已接通。新设备通过“设备管理 → 添加设备”创建。

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

浏览器检查应覆盖“设备管理 → 添加设备”、设备详情和窄屏布局。设置 `IOT_TEST_BROWSER` 后，`go test ./internal/httpapi -run TestDeviceOnboardingBrowser` 会构建隔离的 Go API，并用真实浏览器运行 `iot_front/tests/browser/device-onboarding-check.mjs`：覆盖新型号与已有型号、一次性密钥、合成现场上报后的后端诊断、服务端筛选和 390px 布局（需先执行 `npm run build`）。`iot_front/tests/browser/onboarding-modes-check.mjs` 在本机合成夹具上检查共享监听与标准上报两种方式。`TestOnboardingBrowser` 通过页面新建设备模板、用向导添加 HTTP 设备，再以设备凭据验证错误密钥拒绝、解析与重发去重，并检查连接详情、多接入点选择、接入点会话、摄像头登记和模型来源名称；同时设置 `IOT_TEST_MQTT_WEBSOCKET`、`IOT_TEST_MQTT_BROKER` 与 `IOT_TEST_MQTT_JWT_SECRET` 时，设备还会换取 MQTT 令牌并经真实 Broker WebSocket 上报。普通单元测试通过不代表浏览器或真实设备验收。

测试使用隔离业务仓库或临时 schema、随机身份和非 retained 消息；凭据通过环境变量安全注入。缺少环境的集成分支会跳过。模拟器与受控故障测试不能代替厂商真机、固件补传或生产网络验收。


## 从报文或点表生成协议

在「设备通信协议 → 协议生成」上传报文或 Excel / CSV 点表，编辑字段映射，用真实样本预览，再保存、发布并绑定产品。文件格式、大小、地址基准及接口统一见 [配置驱动协议](CONFIGURABLE_PROTOCOLS.md#从报文或点表生成协议)。变长、会话或厂商专用协议使用 [Go 源码包](GO_PROTOCOL_PACKAGES.md)。

## 设备分组与可见性

设备管理按「全部 / 独立设备 / 主设备 / 子设备 / 待登记」分组，对应 `DIRECT/GATEWAY/CHILD`，可按关键字（名称或编号）、设备类型和运行状态筛选。筛选在服务端完成后再分页，总数为筛选后的数量；子设备行显示所属主设备名称。“待登记”列出平台已收到数据、尚未登记的设备，可以一键登记。接收实时消息时只提示「有新数据」，点击刷新后更新列表。

## 设备台账字段与协议绑定

- 接入方式写在设备的结构化字段中：`connector`（平台签发凭据的通道，如 `MQTT` / `HTTP`）、`connectorProfileId`（所用接入点）、`childAddress` / `childType`（子设备在主设备协议中的地址和类型）。`onboardingRequestHash` 仅供添加设备幂等判断，接口不返回。`tags` 只保存用户标签，以上字段名不能作为标签使用；旧数据中的同名标签在读取和启动迁移时转为字段。
- 设备角色只取 `deviceRole`（`DIRECT` / `GATEWAY` / `CHILD`，空值按 `DIRECT`），不再从模板分类推断；子设备的 `gatewayId` 必须指向 `deviceRole=GATEWAY` 的设备。
- 模板使用的协议版本以产品协议绑定（`GET /api/v2/products/{id}/protocol-binding`）为唯一来源。新建或保存模板时选择已发布版本会同时写入绑定；切换版本在一个事务中校验绑定未被他人修改，已变化时返回 409。

主设备是实体设备，接入点是设备模板的软件连接配置，二者分别管理。普通用户仅看到授权设备及其告警，主子关系不自动传递权限；受限设备的详情不展示共享网关和其他设备的会话。
