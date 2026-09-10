# 统一设备接入

[返回 README](../README.md) · [Go 协议开发](GO_PROTOCOL_PACKAGES.md) · [TCP 与主子设备](TCP_CHILD_DEVICE_ACCESS.md)

## 选择接入方式

入口为 **设备管理 → 添加设备**：选择或新建产品 → 选择通信方式 → 配置参数 → 测试与预览 → 完成并启用。

| 方式 | 准备内容 | 平台设备凭据 |
| --- | --- | --- |
| HTTP / MQTT 标准上报 | 设备实现下文标准 JSON；MQTT 需可达的 Broker | AccessKey 和 Secret |
| Modbus TCP | 目标地址、端口、站号、点表及采集周期 | 不需要 |
| Modbus RTU over TCP | 串口服务器地址、端口、站号及 RTU 点表 | 不需要 |
| Go TCP / UDP | 产品绑定已发布协议；配置监听或 TCP 主动连接 | 不需要；协议自行完成身份识别与认证 |
| 主设备下的子设备 | 主设备接入实例和子类型到产品的映射 | 不需要；沿主设备协议接入 |

简单 JSON 路径或固定 HEX 映射见 [配置驱动协议](CONFIGURABLE_PROTOCOLS.md)；专用协议使用 Go 源码。摄像头从独立的摄像头管理入口维护，见 [视频集成](VIDEO_SDK_ADAPTER.md)。

完成页分别展示配置保存、运行时状态、原文接收和解析结果。HTTP/MQTT 获得凭据后再发送首条数据；TCP/UDP 按协议连接并上报。配置保存成功不能证明设备在线。已有产品继续使用原协议绑定，向导不会替换该产品其他设备的解析链路。

## 测试、保存与诊断

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

Secret 仅首次创建或轮换返回，服务端保存哈希；详情返回 `credentialSupported` 标识。TCP、UDP、Modbus 与子设备不生成平台 Secret，调用凭据接口返回 422。显式设备通信方式优先于产品默认值；旧协议设备曾生成的密钥不能绕过当前认证检查。

HTTP Secret 在轮换后立即失效。MQTT 已签发令牌的及时撤销需配置 `IOT_EMQX_API_URL`、`IOT_EMQX_API_KEY`、`IOT_EMQX_API_SECRET`；URL 是 Broker 管理根地址，不带 `/api/v5`。凭据变更和撤销意图一同保存，平台封禁旧 username 并断开其会话；失败保留 PENDING，每 30 秒重试。未配置管理适配器时不能把轮换等同于即时断连，仍依赖 JWT 到期与 Broker 的到期断连。配置见 [EMQX 认证与授权](../ops/emqx/PRODUCTION_SECURITY.md)。

标准 MQTT 命令请求：

```http
POST /api/v1/device-registry/{id}/commands
Content-Type: application/json
Authorization: Bearer <operator-token>

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

控制面使用登录令牌及租户隔离。测试、创建、凭据管理要求 admin，命令要求 operator/admin，读取按 viewer 授权。

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/v1/connectors/types` | 当前通信能力 |
| POST | `/api/v1/onboarding/test` | 测试与解析预览 |
| POST | `/api/v1/onboarding` | 原子保存并启用 |
| GET | `/api/v1/connectors` | 接入实例、运行状态与会话 |
| GET | `/api/v1/device-registry/{id}/connection` | 设备连接和解析详情 |
| POST / DELETE | `/api/v1/device-registry/{id}/credentials` | 轮换 / 禁用受管凭据 |
| GET | `/api/v1/device-registry/{id}/history?kind=connection` | 连接历史；`kind=event` 查询事件 |
| GET | `/api/v1/device-registry/{id}/commands` | 命令与回执记录 |

请求结构见 [onboarding/service.go](../internal/onboarding/service.go)，分页约定见 [列表分页](LIST_PAGINATION.md)。产品可选 `thingModel` 描述 properties/events/commands 及参数类型，它提供描述与命令校验，不替代协议解析器。

## 接入指南与测试设备

**接入与测试** 包含两个标签：

- **设备接入**：选择已登记设备，查看连接参数和报文示例；协议设备进入实际连接详情。按指南配置真实设备后，在原始报文中核对接收与解析。
- **测试设备**：准备测试设备及关联产品/协议，选择报文模板或快捷发送，查看结果并跳转告警。测试告警进入正常业务链路，不自动创建规则。

切换标签保留页面测试状态。新设备仍通过“设备管理 → 添加设备”创建；标准 HTTP/MQTT 真机联调使用本文的凭据与上报接口。

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

浏览器检查按当前“添加设备 → 接入与测试 → 原始报文 / 告警”路径执行。仓库现有 `TestOnboardingBrowser` 仍引用已移除的联调面板，脚本更新前不能用于当前页面验收。

测试使用隔离业务仓库或临时 schema、随机身份和非 retained 消息；凭据通过环境变量安全注入。缺少环境的集成分支会跳过。模拟器与受控故障测试不能代替厂商真机、固件补传或生产网络验收。
