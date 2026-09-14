# 统一设备接入

[返回 README](../README.md) · [Go 协议开发](GO_PROTOCOL_PACKAGES.md) · [TCP 与主子设备](TCP_CHILD_DEVICE_ACCESS.md)

## 选择接入方式

管理端按资源提供独立菜单，各页先展示列表，新增与编辑在弹窗中完成：

| 菜单 | 管理内容 |
| --- | --- |
| 协议管理 | 上传报文或点表生成协议，或上传 Go 源码；预览、校验并发布版本 |
| 产品管理 | 创建产品、配置物模型；“协议版本”用于绑定与回滚 |
| 设备管理 | 选择已有产品，登记设备名称和标识，管理角色、主子关系与凭证 |
| 接入网关 | 新建或编辑 TCP / UDP 连接，维护查询和子设备产品映射，查看运行状态；已有 Modbus 实例可编辑与测试 |
| 接入测试 | 连接指南、测试设备及标准报文验证 |

“接入网关”是平台的软件接入服务，复用原接入实例的配置与运行机制；现有接口及数据标识保持不变。一个产品可以配置多个接入网关，监听型网关可接入多台设备，主动连接型网关配置目标设备。创建网关时选择产品，协议及版本自动沿用产品绑定；统一管理端口、连接方向、查询、子设备产品映射、启停及会话。现场实体网关仍在设备管理中登记，不与软件接入网关混用。

标准 HTTP / MQTT 产品可直接选择“标准设备上报”，无需先上传源码。创建产品时服务端准备当前租户的标准协议版本；登记设备后返回一次性凭证。已发布 Go 版本可直接用于创建产品并建立绑定，无需先有设备或实例。编辑现有产品的协议版本使用专门的“协议版本”操作，保留回滚和运行时兼容检查。

| 方式 | 准备内容 | 平台设备凭据 |
| --- | --- | --- |
| HTTP / MQTT 标准上报 | 设备实现下文标准 JSON；MQTT 需可达的 Broker | AccessKey 和 Secret |
| Modbus TCP | 目标地址、端口、站号、点表及采集周期 | 不需要 |
| Modbus RTU over TCP | 串口服务器地址、端口、站号及 RTU 点表 | 不需要 |
| Go TCP / UDP | 产品绑定已发布协议；配置监听或 TCP 主动连接 | 不需要；协议自行完成身份识别与认证 |
| 主设备下的子设备 | 主设备接入网关和子类型到产品的映射 | 不需要；沿主设备协议接入 |

简单 JSON 路径或固定 HEX 映射见 [配置驱动协议](CONFIGURABLE_PROTOCOLS.md)；专用协议使用 Go 源码。摄像头从独立的摄像头管理入口维护，见 [视频集成](VIDEO_SDK_ADAPTER.md)。

设备连接详情分别展示配置、运行时状态、原文接收和解析结果。HTTP/MQTT 获得凭据后再发送首条数据；TCP/UDP 按协议连接并上报。配置保存成功不能证明设备在线。添加设备复用所属产品配置，协议变更在产品管理中单独操作。

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

原始命令调试统一位于“接入测试 → 连接指南 → 命令调试”，先选择具体设备，再填写 MQTT 命令类型及参数或 Go 原始命令 JSON，查看发送状态和应答。接入网关不再提供下行命令入口。未定义命令的产品不会在设备控制中开放自由输入；没有下行能力的连接显示不支持提示。服务端继续要求人工确认，并检查菜单、操作与设备范围。


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

控制面使用登录令牌，并检查租户、菜单、操作及用户设备范围。设备详情、凭据和命令只允许访问授权设备；接入网关与接入测试涉及全租户配置，要求设备管理菜单、全部设备范围及对应菜单和操作权限。配置规则见 [用户权限](USER_ACCESS_CONTROL.md)。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/v1/connectors/types` | 当前通信能力 |
| POST | `/api/v1/onboarding/test` | 测试与解析预览 |
| POST | `/api/v1/onboarding` | 原子保存并启用 |
| GET | `/api/v1/connectors` | 接入网关、运行状态与会话 |
| GET | `/api/v1/device-registry/{id}/connection` | 设备连接和解析详情 |
| POST / DELETE | `/api/v1/device-registry/{id}/credentials` | 轮换 / 禁用受管凭据 |
| GET | `/api/v1/device-registry/{id}/history?kind=connection` | 连接历史；`kind=event` 查询事件 |
| GET | `/api/v1/device-registry/{id}/commands` | 命令与回执记录 |

请求结构见 [onboarding/service.go](../internal/onboarding/service.go)，分页约定见 [列表分页](LIST_PAGINATION.md)。产品可选 `thingModel` 描述 properties/events/commands 及参数类型，它提供描述与命令校验，不替代协议解析器。

## 接入指南与测试设备

**接入测试** 包含两个标签：

- **连接指南**：选择已登记设备，查看连接参数和报文示例；协议设备进入实际连接详情。按指南配置真实设备后，在原始报文中核对接收与解析。
- **测试设备**：准备测试设备及关联产品/协议，选择报文模板或快捷发送，查看结果并跳转告警。测试告警进入正常业务链路，不自动创建规则。

切换标签保留页面测试状态。新设备仍通过“设备管理 → 对应设备分组 → 添加设备”创建；标准 HTTP/MQTT 真机联调使用本文的凭据与上报接口。

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

浏览器检查按当前“设备管理 → 接入测试 → 原始报文 / 告警”路径执行。`TestOnboardingBrowser` 调用 `iot_front/tests/browser/onboarding-check.mjs`，已包含设备管理、接入网关及接入测试入口；它是可选集成检查，须提供相应测试环境，不能从普通单元测试通过推断浏览器已执行。

测试使用隔离业务仓库或临时 schema、随机身份和非 retained 消息；凭据通过环境变量安全注入。缺少环境的集成分支会跳过。模拟器与受控故障测试不能代替厂商真机、固件补传或生产网络验收。


## 早期管理界面验证（2026-09-14，历史记录）

以下为当时 macOS 环境的验证快照，测试数量和入口描述保留原记录。后续 Windows 界面与设备权限验证见 [最新验收记录](testing/2026-09-14-权限与界面验收.md)。

- 环境：本地 macOS，Vue 开发服务连接独立内存 API；未连接现有部署数据或真实设备。
- `iot_front`：`npm test`（57 项通过）、`npm run build`（通过，仍有主包超过 500 kB 的体积提示）。
- 仓库根目录：`go test ./internal/httpapi ./internal/onboarding ./internal/core` 通过。新增回归覆盖独立产品/设备登记、标准报文解析、Go 发布版本绑定、未发布版本拒绝和租户边界。
- 浏览器实测：创建标准产品、添加设备与一次性凭证返回、产品协议绑定、独立实例入口；桌面与 390px 窄屏表单、内容溢出及长弹窗滚动。
- 可选浏览器脚本已同步新入口并通过语法检查；本次未运行这些脚本，也未重做真实 MQTT、TCP / UDP、Modbus 设备联调。

## 从报文或点表生成协议

在“协议管理 → 协议生成”选择报文或点表，弹窗标题为“生成协议”。JSON 报文自动提取属性路径；CSV / Excel 点表生成 Modbus TCP / RTU 解析映射与读取块。HEX 报文需补充字段偏移、长度、端序及单位，由已配置 AI 辅助生成固定字段映射；单个样本不能可靠推断专用协议，变长与请求应答协议仍使用 Go 源码入口。

生成后在“编辑字段映射”对照表中填写字段标识与 JSON 路径、Modbus 地址或 HEX 字节偏移，不需要编辑 JSON 源码。支持添加、删除字段；展开行可调整倍率、字节序等参数。Modbus 地址统一从 0 开始，固定宽度类型自动同步寄存器数量。修改映射后旧预览清除，重新解析预览或保存时使用最新输入；已保存版本须先“新建版本”才能编辑。

生成后可查看字段、编辑映射并上传真实样本预览，再保存为不可变版本。没有样本时可先保存草稿；列表的“解析测试”可继续校验，成功后才可发布并在产品管理绑定。Modbus 预览需填写响应帧对应的零基起始地址。修改已保存的映射须创建新版本，不能覆盖旧版本。

- 报文文件：`.json` / `.txt` / `.hex` / `.bin`，最大 1 MiB；二进制文件按 HEX 处理。
- 点表文件：`.csv` / `.xlsx`，最大 32 MiB；也可粘贴 CSV。常用列为 `identifier,name,functionCode,address,addressNotation,dataType,scale`。零基地址应明确写 `addressNotation=zero_based`；未声明基准时传统 `40001` 等地址按 Modbus 表区换算。
- 生成接口：`POST /api/v1/ai/protocol-assistant/generate`，multipart 参数 `inputKind=sample|point-table`；保存接口沿用 `/publish` 路径，生成的映射实际保存为 v2 `DRAFT` 或已通过样本校验的 `VALIDATED` 版本。
- 已保存版本通过 `POST /api/v2/protocols/{id}/releases/{version}/preview` 校验样本，再通过既有 `/publish` 接口发布。以上写操作要求对应菜单和操作授权，并按登录租户隔离。

协议目录、组织发布及远程分发接口已移除；已有安装的协议版本和历史数据库内容不删除。

## 设备分组与可见性

设备管理分为「独立设备」「主设备」「子设备」三个标签，对应 `DIRECT/GATEWAY/CHILD`。设备类型筛选来自产品分类，按当前分组和类型筛选后分页；新增设备默认沿用当前标签角色。接收实时消息时只提示「有新数据」，点击刷新后更新列表。

主设备是实体设备，接入网关是产品的软件连接配置，二者分别管理。普通用户仅看到授权设备及其告警，主子关系不自动传递权限；受限设备的连接详情不展示共享网关和其他设备会话。
