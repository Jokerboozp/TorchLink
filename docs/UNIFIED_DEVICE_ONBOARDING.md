# 统一设备接入

本入口位于 **设备管理 → 添加设备**。流程为：选择或新建产品 → MQTT / HTTP / Modbus TCP / TCP / UDP → 参数配置 → 接入测试 → 数据预览 → 完成并启用。原有高级注册、协议发布/回滚、采集实例和报文回放仍然保留。视频设备跳转原摄像头管理；Edge Agent 暂不开放。

## 实现与资源边界

- `internal/connector`：通信控制面的 `Type`、`Connector.Test`、统一测试结果和 `Instance` 投影。不统一强制各 Runtime 的 Start/Stop。
- `internal/onboarding`：配置校验、协议选择、标准凭据、点表编译、测试和资源编排，不依赖 HTTP。监听状态、MQTT 健康检查由启动装配注入。
- `internal/model/onboarding.go`：一次原子保存的 `OnboardingBundle`。PostgreSQL 在事务内新增资源，内存实现使用同一写锁。重复设备、产品、端口冲突均拒绝，不覆盖原记录。
- Connector 类型和关联 Profile ID 保存在已有 `ManagedDevice.Tags` 中；连接配置复用 `DeviceAccessProfile`。Connector/Onboarding 本身不新增表；后续运维补齐新增三张向后兼容表，见文末。其余沿用现有 JSONB、哈希字段和不可变发布表。
- 新建产品时同时生成兼容的 ProtocolPackage 投影。标准上报显式固定 `iot-standard@1.0.0`，不会修改已有产品绑定。TCP/UDP/Modbus 复用产品现有绑定；对已有设备且仅使用旧协议配置的产品，不自动切换其解析链路。
- Modbus CSV 复用 `core.ParseModbusPointTable` 和 `CompileModbusReadBlocks`，生成不可变点表与 release，再交给已有 `protocolruntime.Runtime`。已绑定产品直接使用原点表；新点表仅用于未绑定产品。
- TCP/UDP 使用现有 Go V2 协议的 ingress/decode。平台继续负责半包、粘包、Session、自动注册、协议状态、reply 和 command；可以复用同产品的已启用监听实例。

## API

控制面接口使用现有登录令牌和租户隔离。创建、测试、凭据禁用要求 admin；读取使用既有 viewer 授权逻辑。

| 方法 | 路径 | 用途 |
|---|---|---|
| POST | `/api/v1/onboarding/test` | 不落业务数据的接入测试和解析预览 |
| POST | `/api/v1/onboarding` | 原子创建设备及关联资源并启用 |
| GET | `/api/v1/connectors` | 已有采集/监听 Profile、健康状态、在线会话 |
| GET | `/api/v1/device-registry/{id}/connection` | 设备、产品、连接、协议、最新消息和会话 |
| DELETE | `/api/v1/device-registry/{id}/credentials` | 清除凭据哈希，禁止后续设备鉴权 |
| POST | `/api/v1/device-ingest/standard/{tenant}/{product}/{device}/{kind}` | 标准 HTTP 上报 |

复用接口：`POST /api/v1/device-registry/{id}/credentials` 重新生成凭据；`POST /api/v1/device-mqtt/token` 换取 MQTT JWT；`POST /api/v2/device-access-profiles/{id}/devices/{deviceId}/commands` 发送已有 Session 命令。

测试请求示例：

```json
{
  "productId": "environment",
  "deviceId": "sensor-001",
  "name": "一楼温度传感器",
  "type": "HTTP",
  "messageKind": "property",
  "payload": {
    "id": "msg-001",
    "timestamp": 1788850000000,
    "data": {"temperature": 26.5, "smoke": 0}
  }
}
```

新建产品时额外传 `productName`，并使用新的 `productId`。MQTT/HTTP 不需要选择 Parser 或 ProtocolRelease。

成功测试返回 `testToken`，创建请求带上该令牌和**同一份配置**。令牌有效 10 分钟，签名绑定租户、配置、协议内容和实际监听参数；改动配置、协议或测试过期须重新测试。保存发生资源冲突时返回 409，已有数据不变。令牌使用平台 JWT 密钥签名，配置相同的 API 副本可验证。

## HTTP / MQTT 标准协议

统一 JSON 格式为 `id`、正数毫秒 `timestamp`、非空对象 `data`。`kind` 为 `property`、`event`、`state` 或 `command-reply`，分别形成 PROPERTY_REPORT、EVENT_REPORT、STATE_CHANGE、COMMAND_REPLY。`state.data.connectionStatus` 可使用 CONNECTED / DISCONNECTED / UNKNOWN，状态更新也发生在解析之后。告警继续由现有规则或原协议的 ALARM_REPORT 处理。

HTTP 示例（变量由设备配置提供，不在脚本里固定 Secret）：

```bash
curl -X POST "$PLATFORM/api/v1/device-ingest/standard/$TENANT/$PRODUCT/$DEVICE/property" \
  -H 'Content-Type: application/json' \
  -H "X-Device-Key: $DEVICE_KEY" \
  -H "X-Device-Secret: $DEVICE_SECRET" \
  --data '{"id":"msg-001","timestamp":1788850000000,"data":{"temperature":26.5}}'
```

使用 `/standard/` 前缀是为了兼容既有 `/api/v1/device-ingest/{deviceId}` 接口。标准入口强制校验租户、产品、设备、启用状态与凭据；正文限制 64 KiB，每进程每设备每秒最多 20 次。限流状态有容量和闲置清理上限；多副本总额度按实例数增加，尚未引入共享限流存储。

成功返回 202、`messageId` 和 `created`。这表示已交给原始归档/队列链路，不表示所有异步规则已执行完。相同租户/产品/设备/kind 下重复 `id` 使用同一 rawMessageId，重试不会重复处理；新的读数必须使用新 `id`。

MQTT 设备先带上述两个凭据请求头调用 `POST /api/v1/device-mqtt/token`，将返回的 `username`、`token` 用作 MQTT 用户名和 password，Client ID 使用向导给出的值。标准设备令牌有效 300 秒，设备应重新获取并重连。Broker 地址使用实际部署的 EMQX 地址；WebSocket 地址可从该接口的 `websocketUrl` 获取。

```text
/iot/up/{tenant}/{product}/{device}/property
/iot/up/{tenant}/{product}/{device}/event
/iot/up/{tenant}/{product}/{device}/state
/iot/down/{tenant}/{product}/{device}/command
```

JWT ACL 精确到设备的四个上行主题与一个下行主题；新标准设备不授予旧 raw topic 发布权限。平台服务令牌加入 `/iot/up/#` 订阅权限。Broker 必须沿用部署中的 JWT 校验和拒绝未授权访问配置；自定义服务账号须自行授予订阅权限。标准订阅拒绝 retained 消息，并再次校验当前设备/产品及凭据启用状态。

链路始终为：**传输 → 保留原始 JSON 的 RawMessage → 原始归档 / 幂等索引 → 内部队列 → StandardParser → StandardMessage → 原有存储 / 规则 / 告警 / AI**。设备正文不能指定租户、Parser、协议版本或跳过归档。标准下行 topic 已预留 ACL；本次不新增 MQTT Command 编码服务。

## 凭据边界

复用 `ManagedDevice.AccessKey / SecretHash`，Secret 使用密码学随机数生成，仅保存 SHA-256 哈希；首次创建/重新生成返回一次，列表和详情不返回哈希或原文。禁用清空哈希；重新生成沿用现有轮换接口。

HTTP 原 Secret 在轮换后立即失效。禁用后标准 MQTT 入站也拒绝数据。**未配置管理适配器时，轮换不主动断开已经通过认证的 MQTT 会话**；已签发 JWT/已连接会话的撤销取决于 Broker 的过期断连、重新鉴权或管理 API。现在可通过下述 EMQX 管理 API 适配器完成封禁及断连；未配置、请求失败或持久化失败时明确显示 PENDING，不能把平台凭据轮换等同于 Broker 实时撤销。

## 测试与预览的含义

- HTTP：格式与解析测试；设备鉴权通过自动回归覆盖，设备实际网络仍需上报验证。
- MQTT：平台到 Broker 的连接健康与样例解析；未启动 MQTT 时测试失败，不把样例解析成功当作 Broker 在线。
- TCP/UDP：新实例检查本机端口可绑定；复用实例检查运行时 LISTENING。使用真实协议 ingress 检查完整帧和设备 ID，再调用原 Parser 解码。此测试不等于已验证外部设备到平台的防火墙/路由。
- Modbus：按点表读取每个 block 一次，不启动持续轮询，展示所有响应、点位和 StandardMessage；异常保留请求/响应 HEX 与 exceptionCode。使用已有 `IOT_MODBUS_ALLOWED_CIDRS` 出站策略。
- 测试不写属性、规则或告警；预览显示 Raw、Protocol ID/Version、Parser、Parsed、StandardMessage 及属性/事件/告警类型映射。

测试结果区分 NETWORK_ERROR、TIMEOUT、PROTOCOL_ERROR、PARSE_FAILED、DEVICE_IDENTIFY_FAILED、SUCCESS；上报鉴权使用 AUTH_FAILED，另有 BODY_TOO_LARGE、RATE_LIMITED、INGEST_FAILED。

## 本地验证与后续

```text
仓库根：go test ./...
protocol-packages/gb26875-dahua：go test ./...
iot_front：npm test
iot_front：npm run build
```

当前前端是 JavaScript，没有独立 lint/typecheck 脚本，不把构建报告为类型检查。可选真实浏览器测试：先构建前端，再将 `IOT_TEST_BROWSER` 指向 Chromium/Edge 可执行文件，运行 `go test ./internal/httpapi -run '^TestOnboardingBrowser$' -count=1 -v`。它使用临时内存 API、临时浏览器配置和本地构建产物，不连接真实业务库。

已增加连接历史、最近事件、最新属性、MQTT 命令与回执、物模型基础、EdgeNode 登记和 Broker 凭据撤销适配。完整物模型执行体系、独立 Access Gateway、完整 Edge Agent、Modbus RTU 和其他 P2 协议未实施。

下一阶段优先使用真实设备与实际 PostgreSQL/EMQX 验证完整接入、凭据轮换和长连接，再考虑共享限流与独立网关部署。

## 2026-09-09：P1 与 MQTT 运维补齐

设备详情新增分页连接/状态历史、最近事件、单独的最新属性、MQTT 命令记录、一次性凭据重置展示及 Broker 撤销状态。TCP/UDP 第一帧识别设备后记录 CONNECTED，最后一个会话关闭记录 DISCONNECTED；UDP 表示最近有效报文建立的逻辑会话，空闲超时仍沿用原 Runtime。多个会话共享设备计数，不把关闭其中一个误记为完全断开。HTTP/MQTT 的连接状态仍来自设备标准 state 上报，不能等同于 Broker 实时在线查询。

新增 API（均限定当前登录租户）：

| 接口 | 权限 | 用途 |
|---|---|---|
| GET `/api/v1/edge-nodes` | viewer | 节点列表 |
| POST `/api/v1/edge-nodes`、PUT `/api/v1/edge-nodes/:id` | admin | 节点登记、编辑、停用 |
| GET `/api/v1/device-registry/:id/history?kind=connection\|event&page=1&pageSize=20` | viewer | 连接/状态历史或最近事件 |
| GET `/api/v1/device-registry/:id/commands?page=1&pageSize=20` | viewer | 命令与回执记录 |
| POST `/api/v1/device-registry/:id/commands` | operator | 标准 MQTT 命令 |

历史与命令每页最多 100 条。现有凭据 POST/DELETE 接口增加 `revocation` 结果，连接详情返回各旧凭据撤销状态。未配置 Broker 管理时凭据变更仍成功、撤销任务保留 PENDING。

### 物模型和 Edge 扩展

Product 的可选 `thingModel` 保存 properties/events/commands，属性和命令参数字段包含 identifier/name/dataType/unit/required。支持 string、number、integer、boolean、object、array。产品编辑页高级区域提供 JSON 编辑；标识重复或类型非法返回 422。它是描述与命令参数校验基础，不替代 Protocol/Parser，不强制改变历史上报数据。

```json
{"properties":[{"identifier":"temperature","name":"温度","dataType":"number","unit":"℃"}],"events":[],"commands":[{"identifier":"setThreshold","name":"设置阈值","fields":[{"identifier":"value","dataType":"number","required":true}]}]}
```

EdgeNode 只有租户、标识、名称、ENABLED/DISABLED、描述、创建/修改时间。向导高级设置可登记及关联 `profile.edgeNodeId`，现有 CollectorID 保留。该关联仅说明归属，不部署 Agent、不改变任务运行位置，不假造心跳或远程在线状态。

持久化只新增 `edge_node`、`device_command`、`device_credential_revocation` 三张表及索引，状态历史复用 `device_state_event`。Product/DeviceAccessProfile 的新增字段使用现有 JSONB，无历史数据删除。启动时沿用现有幂等 schema migration；memory 和 PostgreSQL 都实现对应接口，ClickHouse/Redis 装饰器继续转发到业务仓库。

### MQTT 命令和回执

命令接口请求：

```json
{"id":"cmd-001","type":"setThreshold","data":{"value":30}}
```

下发 `/iot/down/{tenant}/{product}/{device}/command`，内容为上述对象加平台生成的毫秒 `timestamp`，QoS 1、不保留。要求设备与产品启用、凭据有效、MQTT publisher 已配置。产品定义了 commands 时校验命令名与参数类型/必填字段。

同租户命令 ID 唯一，同一设备同一请求重复提交返回已保存记录，不再次发送；ID 对应不同请求则拒绝。状态 DISPATCHING → SENT 仅代表发送给 Broker，设备回复后才为 SUCCEEDED/FAILED。发布失败/超时标记 UNKNOWN，进程在发布期间退出可能保留 DISPATCHING；这两种情况均不自动重发物理命令，需核实设备后再明确开始一条新命令。QoS 1 仍可能在传输层重送，设备必须按命令 ID 去重执行。

设备使用自己的 ACL 上报 `/iot/up/{tenant}/{product}/{device}/command-reply`：

```json
{"id":"reply-001","timestamp":1788850000001,"data":{"commandId":"cmd-001","success":true,"result":"ok"}}
```

HTTP 标准上报也接受 command-reply。回执仍先归档 Raw，再经标准 Parser 形成 COMMAND_REPLY，消费时按租户、设备和 commandId 关联。重复/未知回执保留报文，终态不会被重复回执或迟到的发布状态覆盖。页面点击刷新查看最终执行结果。

### EMQX 主动撤销

可选环境变量：`IOT_EMQX_API_URL`（如本地 `http://127.0.0.1:18083` 或 Compose 内 `http://emqx:18083`，不含 `/api/v5`）、`IOT_EMQX_API_KEY`、`IOT_EMQX_API_SECRET`。使用专用管理 API Key，而不是 Dashboard 登录密码；模板不写入真实值。Compose 已透传，其他部署方式按环境注入。API Key 需要封禁名单和客户端管理权限。

凭据变更和撤销意图在同一事务内保存。适配器先以旧 AccessKey（MQTT username）创建永久封禁，再仅查询该 username 的会话并逐个断开；拒绝重定向和不匹配身份。完成后才标记 REVOKED，失败任务每 30 秒重试，重启后从持久化仓库恢复。多副本可重复执行同一撤销，操作保持幂等。封禁项不会自动删除；轮换使用新的随机 AccessKey，不会重新使用被封禁的旧用户名。

参考 [EMQX 管理 API 认证](https://docs.emqx.com/en/emqx/latest/admin/api.html)、[封禁名单 API](https://docs.emqx.com/en/cloud/latest/api/dedicated.html) 和 [客户端断开 API](https://docs.emqx.com/en/cloud/latest/api/clients_v5.html)。测试使用本地 HTTP 模拟服务器验证封禁顺序、失败恢复、身份限制及重定向拒绝；真实 EMQX 的 API 权限、版本行为和设备重连仍需部署环境验收。


2026-09-09 续验：前轮 Go 全量、GB26875 独立 module、前端 45 项测试/构建及真实 Edge 浏览器均通过。本轮使用现有本地依赖配置，已在真实 PostgreSQL 的独立临时 schema 中通过新增表的重复迁移、凭据冲突事务回滚、命令去重、跨设备回执隔离、终态保护和历史查询；测试结束删除自身 schema。真实 MQTT Broker 验证设备向导、JWT 换取、属性 Raw 归档、MQTT 下行命令和 Raw 回执关联通过。没有部署业务服务，也未验收真实厂商设备。EMQX 管理 API Key 尚未配置，主动封禁/断连仍仅有模拟 HTTP 测试证据。

可重复执行：

```text
# 配置 IOT_TEST_POSTGRES_DSN 后；仅在该数据库创建并清理临时 schema
 go test ./internal/adapters/postgres -run TestDeviceOperationsMigrationAndAtomicity -count=1 -v
# 配置 IOT_TEST_MQTT_BROKER 和 IOT_TEST_MQTT_JWT_SECRET 后
 go test ./internal/httpapi -run TestStandardMQTTLiveBroker -count=1 -v
```

MQTT 集成测试使用随机临时租户、独立内存业务库、临时 Raw 目录、clean session 和非保留消息；结束后关闭客户端。测试需要与 Broker 一致的 JWT 签名密钥，用环境变量注入，勿写入代码或命令历史。未配置时显式跳过。Broker 管理验收需要另外配置 `IOT_EMQX_API_URL/IOT_EMQX_API_KEY/IOT_EMQX_API_SECRET`，不能用 MQTT JWT 替代管理 API Key。
