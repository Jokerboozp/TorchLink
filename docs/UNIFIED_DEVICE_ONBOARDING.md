# 统一设备接入

> 当前范围（2026-09-10）：保留中心 MQTT/HTTP、Modbus TCP、Go TCP/UDP 接入；边缘节点、RTU/OPC UA/SNMP/BACnet 现场入口及节点视频发现已移除。下文相关 P0/P1/P2 节点说明和接口表是历史阶段记录，不再代表当前可用接口。请以 [移除说明](EDGE_REMOVAL.md) 与当前源码为准。

## 当前接入契约补齐（2026-09-09，本次工作区）

本次核实、改动与实测见 [实施进度](DEVICE_ACCESS_REFACTOR_PROGRESS.md)。下方既有分日期记录属于历史验收；以本节、当前源码及本次进度表为准。

- 添加设备 → 选择/新建产品 → 选择通信方式 → 配置 → 测试与预览 → 完成并启用。完成页分别展示“配置已保存、运行时状态、原文接收、解析成功”；保存不能证明设备在线。被动设备获得凭据后再发送首条数据；页面每 2 秒查询，最多等待 2 分钟，可刷新继续。保存 Secret 后点击“进入设备详情”。
- `GET /api/v1/connectors/types` 返回通信能力。HTTP 只上报；MQTT 命令还需 publisher；TCP/UDP 命令还需已发布协议 encode 和有效会话。EDGE 明确 unsupported，视频仍走摄像头元数据流程。
- `POST /api/v1/onboarding` 使用租户 + Device ID 作为幂等键，并存储请求摘要。首次成功 201，相同请求重试 200 / `reused:true`；配置不同 409。恢复不重复生成产品、设备、凭据或 Listener，且不再次返回 Secret。上次响应丢失时在设备详情轮换凭据。该行为适用于本次改造后保存的向导记录；历史设备没有摘要，仍返回冲突。
- `GET /api/v1/device-registry/{id}/connection` 新增 `accessInfo`、`ingest`，包含原文 ID、平台接收时间、解析尝试时间、解析错误及实际 StandardMessage。原文详情也返回 `parseError`。解析失败不构造业务数据。
- `IOT_DEVICE_HTTP_PUBLIC_URL` 为设备可达的 HTTP/HTTPS 根地址，留空时返回相对 API 路径；`IOT_DEVICE_MQTT_PUBLIC_URL` 为设备可达的 MQTT/TLS Broker，留空明确显示未配置。绝不从容器内部 Broker 地址推断对外地址，不显示带用户密码/查询参数的配置 URL。WebSocket 沿用 `IOT_MQTT_WEBSOCKET_PUBLIC_URL`。
- 标准 v1 envelope 为 `version:"1.0"`、唯一 `id`、正数毫秒 `timestamp`。property 的 `data` 必须为非空对象；event 支持顶层 `event`；state 支持顶层 `online`；command-reply 支持顶层 `commandId`、`success`。不带 version 的旧 `data.connectionStatus` / `data.commandId` / `data.success` 格式继续支持。Topic/API 仍使用 `command-reply`（连字符）。event 名称不会自动转为设备来源告警。
- 限制 64 KiB、16 层嵌套、重复 JSON 键、未知 version；设备时间超过平台接收时间 5 分钟拒收。历史时间保留用于补传；晚到数据不会回退当前 Last Seen 或连接状态。Raw 元数据记录客户端消息 ID 与设备时间，身份/协议由宿主确定。标准凭据不能绕到旧 HTTP RawMessage 接口提交自选 Parser/协议。
- 同一标准消息 ID 的重试必须保持正文逐字节一致；不同正文返回 `409 MESSAGE_CONFLICT`，不覆盖原文。同一消息通过 HTTP/MQTT 重传仍使用同一 rawMessageId。当前单进程按消息加锁；不声称已解决跨多个接入实例的全部写入/调度竞争。
- 接入测试最多并发 8；Modbus 只读最多并发 32，Socket 等待支持取消。单设备不重叠采集。新点表可设置默认周期（1–3600 秒）、重试、字节序、字序、倍率和单位；CSV 可逐点设置周期，已绑定产品继续使用原点表。向导不更改已有产品协议绑定。
- MQTT 接收使用 8 个固定 worker 和 128 项队列。排满/处理失败明确记录拒收，未归档时设备需查询接收结果并用原消息 ID 重试；不提供无限内存排队或把 PUBACK 解释为可靠落库。已归档后的队列发布失败继续由现有 pending-raw 重试处理。进程崩溃前的内存队列不是持久化队列。
- EMQX 5.8.8 配置必须验证 JWT `username` 与连接 username 一致，并启用 `disconnect_after_expire`。Compose 已加入这两项；已有 Broker 若通过 Dashboard/集群动态配置修改过认证链，须核对实际生效配置，不能仅凭 base.hocon 文件认定生效。规则参考 [EMQX JWT 认证](https://docs.emqx.com/en/emqx/latest/access-control/authn/jwt.html)。未配置管理 API 时标准 JWT 最多 300 秒后失效；配置管理适配器才执行即时封禁/断连。
- MQTT 与 TCP/UDP 命令 API 要求 `confirmed:true`，详情页先人工确认；未确认返回 422。MQTT 下行同时提供 v1 `command/params` 和兼容的 `type/data`，QoS 1、非 retained。30 秒内无结果，查询显示 UNKNOWN，不自动重发；迟到的真实回执仍可确定最终结果。
- Edge 节点仅有库存登记。`edgeNodeId` 非空的 Profile 不在中心运行，已有 Listener 改为远端归属后会关闭；UI 显示 unsupported。CollectorID 继续表示来源，不是完整调度或路由能力。本阶段支持一个接入执行实例；多 API 副本不能无差别运行同一采集/监听任务。

### 本次迁移与部署

启动迁移只给 `raw_archive_index` 增加 `parse_attempted_at bigint default 0` 和 `parse_error text default ''`，保留所有历史行、协议和不可变版本。memory 与 PostgreSQL 的存储接口同步；向导摘要使用既有 Device JSONB 标签，无新连接配置表。已有无解析记录的历史原文显示未确认，不补造历史成功时间。

本次未部署业务服务或修改现有 Broker。在线 Compose 透传对外地址，离线打包的 Bash/PowerShell 模板增加空默认值；本地使用 `.env.local` 单独设置。生产配置设备 TLS 地址及证书；明文测试只在隔离网络，不自动修改主机防火墙。

### 可重复验证

```bash
# 仓库根目录
go test ./...
go test -race ./internal/onboarding ./internal/protocolruntime ./internal/adapters/mqtt
# 使用本地模拟 Socket、实际 Go 协议包及临时业务仓库
go test ./internal/httpapi -run TestGoProtocolListenerSourceHotSwitch -count=1 -v
go test ./internal/onboarding -run TestModbusPreviewAndException -count=1 -v
# 向导保存后持续采集、归档、解析及设备断开诊断
go test ./internal/httpapi -run TestModbusOnboardingRuntimeChain -count=1 -v
```

前端在 `iot_front` 执行 `npm test`、`npm run build`；独立协议在 `protocol-packages/gb26875-dahua` 执行 `go test ./...`。

真实数据库：安全注入 `IOT_TEST_POSTGRES_DSN` 后运行 `go test ./internal/adapters/postgres -run TestDeviceOperationsMigrationAndAtomicity -count=1 -v`，只创建/清理自己的临时 schema。

真实 Broker：注入 `IOT_TEST_MQTT_BROKER`、`IOT_TEST_MQTT_JWT_SECRET`，新配置设 `IOT_TEST_MQTT_STRICT_IDENTITY=true`；撤销测试另需 `IOT_TEST_EMQX_API_URL`、`IOT_TEST_EMQX_API_KEY`、`IOT_TEST_EMQX_API_SECRET`。执行 `go test ./internal/httpapi -run TestStandardMQTTLiveBroker -count=1 -v`。测试使用随机租户、临时内存业务库、非 retained 消息，只封禁自身随机用户名并清理。不要代入真实设备凭据。

浏览器：先构建，设置 `IOT_TEST_BROWSER` 为 Chrome/Edge 可执行文件，执行 `go test ./internal/httpapi -run TestOnboardingBrowser -count=1 -v`；真实 WebSocket 分支另需 MQTT 测试变量和 `IOT_TEST_MQTT_WEBSOCKET`。没有配置时对应集成测试明确跳过。


本入口位于 **设备管理 → 添加设备**。流程为：选择或新建产品 → MQTT / HTTP / Modbus TCP / TCP / UDP → 参数配置 → 接入测试 → 数据预览 → 完成并启用。原有高级注册、协议发布/回滚、采集实例和报文回放仍然保留。视频设备跳转原摄像头管理；Edge Agent 暂不开放。

## 标准设备现场联调

进入 **测试设备 → 标准设备联调（MQTT / HTTP）**，填写统一向导创建的设备标识、接入密钥和 Secret。无需再新建测试产品或切换协议。下方原测试烟感工具继续兼容原流程。

1. HTTP 选择上报类型并填写 `data JSON`，点击“发送新消息”。请求使用设备凭据，经过标准 Ingest 和 Raw 链路，不使用管理端 debug 接口。错误凭据不会登出操作员。
2. MQTT 点击“连接 / 重新认证”，通过设备令牌接口获取短期 JWT，再连接服务端配置的 `IOT_MQTT_WEBSOCKET_PUBLIC_URL`。需要浏览器可达的 WebSocket 地址；HTTP 和 MQTT 均使用当前平台的 API。页面会显示连接状态、成功连接次数和收到的下行命令，不自动执行设备命令。
3. 点击“模拟断链并重连”只销毁当前联调客户端的网络连接，触发正常自动恢复流程，不停止 Broker 或平台服务。网络失败按 1、2、4、8、16、30 秒退避，每次重连重新取令牌；正常连接在令牌到期前 30 秒重新认证。认证拒绝停止自动重试，修正凭据后手动连接。“手动断开”和离开页面会取消重试。
4. “重发同一条消息”保持原消息 ID、时间戳、类型和正文；“发送新消息”生成新 ID。离线不缓存、不自动重放业务报文，恢复后由操作员明确重发。HTTP 可显示平台返回的去重结果；MQTT QoS 1 确认只表示 Broker 接收，面板另行查询 Raw 归档和 StandardMessage，超时仍可“刷新解析结果”。
5. 联调发送的数据会进入所选设备的正常存储、规则和告警链路。Secret 仅保存在组件内存，离开页面清除。导出验收记录仅包括设备标识、消息 ID、时间、通信方式、动作和处理状态，不包括凭据、JWT 或原始业务报文。

代码入口：`iot_front/src/components/StandardDeviceCommissioning.vue`、`iot_front/src/standardDeviceProbe.js`。复用现有设备凭据、标准 Ingest、设备令牌和 Raw 查询接口；本阶段没有新增 API、数据库表或 migration。

2026-09-09 验证：前端 52 项测试通过，覆盖网络退避、令牌续期、认证拒绝、晚到请求取消及离线发送拒绝。真实 Edge 浏览器 + 虚拟机 EMQX WebSocket 跑通认证、Raw 解析、主动断链后的自动重连和同消息重发；隔离内存仓库核对重发未重复新增属性消息。HTTP 浏览器验证错误凭据、正确上报、幂等、无凭据导出和离页清理。

浏览器复测先构建前端，设置 `IOT_TEST_BROWSER` 为 Chromium/Edge 可执行文件，然后运行 `go test ./internal/httpapi -run '^TestOnboardingBrowser$' -count=1 -v`。真实 MQTT 分支额外需要 `IOT_TEST_MQTT_BROKER`、`IOT_TEST_MQTT_JWT_SECRET`、`IOT_TEST_MQTT_WEBSOCKET`；变量通过本地配置安全注入，不把密钥写进命令或测试文件。测试使用独立内存业务库、临时 Raw 目录、临时 clean-session 客户端和非保留消息，不部署业务服务。

上述是联调工具与受控故障验证，不代表已完成厂商真机、长时断网或设备固件补传验收；现场缓存和离线自动补传仍属于后续 Edge Agent 范围。

### 发送中断与确认超时续验（2026-09-09）

联调客户端现在显式跟踪待确认的 QoS 1 发布。手动停止、连接丢失或令牌续期切换连接时立即结束这些等待并清除确认定时器，分别显示 `CANCELED` 或 `CONNECTION_LOST`；超过 8 秒未收到发布确认返回 `PUBLISH_TIMEOUT`，销毁旧联调连接并重新认证。迟到的 ACK 不会改变已结束的结果，旧连接中未确认的报文不会自动转移到新连接。消息可能已经到达 Broker，因此这些结果表示“未确认”，操作员可重发相同消息 ID 验证幂等。

新增测试覆盖确认超时、迟到 ACK、发布中断链、手动停止、令牌续期中断发布和订阅权限拒绝。虚拟时钟推进 24 小时持续网络失败后恢复，确认始终只有一个重试定时器、无待确认操作累积、无自动补传；这是快速模拟测试，不是实际运行 24 小时。前端测试共 56 项通过，构建通过。真实 Edge + 虚拟机 Broker 再次通过凭据错误不登出操作员、正确认证、Raw 解析、自动重连和重复消息去重。

厂商真机验收已按用户要求跳过，保持未验收状态。未新增现场缓存、Edge Agent 或独立 Gateway，也未部署或重启业务服务。

### 跨令牌有效期的故障恢复测试

浏览器验收脚本支持 `IOT_TEST_BROWSER_OUTAGE_SECONDS`（1–600 秒，需要同时配置真实 MQTT 浏览器测试变量）。脚本先建立真实 MQTT WebSocket 连接并完成属性上报，再只断开联调客户端的连接；通过 Chromium 网络拦截持续让设备令牌请求返回网络故障。时间经过真实计时，不使用虚拟时钟，不停止 Broker、平台 API 或其他设备连接。

故障期间每秒检查联调客户端未重新连接且不能发送新消息。解除故障后等待现有退避逻辑自动重取令牌并连接，不手动触发重连；随后重发原消息，由隔离仓库检查未重复生成属性记录。330 秒用例跨越标准设备令牌的 300 秒有效期，覆盖持续认证服务不可达后的恢复。

在原浏览器测试变量基础上增加以下参数即可复测（前端须先构建）：

```powershell
$env:IOT_TEST_BROWSER_OUTAGE_SECONDS = '330'
go test ./internal/httpapi -run '^TestOnboardingBrowser$' -count=1 -v -timeout=15m
```

不设置该变量时仍运行原短时浏览器用例。此项验证的是浏览器联调客户端在受控网络故障下的恢复，不代表厂商设备固件、现场离线缓存或全天断网验收。

2026-09-09 实测通过：5 秒冒烟用例通过后，实际计时 330 秒持续故障用例发生 14 次设备令牌网络请求失败，解除故障后自动连接成功，重发原消息后的 Raw 解析和属性记录去重检查均通过。浏览器用例耗时 348.33 秒，Go 测试退出码为 0。测试程序现在持续输出每 30 秒的故障进度；本次本地日志保存在 Git 忽略目录 `.e2e/outage-20260909-200719.log`。此前一次执行会话的结果未保留，未计为通过。

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
| GET | `/api/v1/connectors/types` | 通信类型、支持情况和能力描述 |
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

链路始终为：**传输 → 保留原始 JSON 的 RawMessage → 原始归档 / 幂等索引 → 内部队列 → StandardParser → StandardMessage → 原有存储 / 规则 / 告警 / AI**。设备正文不能指定租户、Parser、协议版本或跳过归档。标准下行复用现有 MQTT Command 服务和设备命令回执，不引入另一套编码服务。

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

EdgeNode 只有租户、标识、名称、ENABLED/DISABLED、描述、创建/修改时间。向导高级设置可登记及关联 `profile.edgeNodeId`，现有 CollectorID 保留。该关联不部署 Agent、不假造心跳或远程在线状态；本次补齐后中心运行时不执行已指定 edgeNodeId 的任务。

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

参考 [EMQX 管理 API 认证](https://docs.emqx.com/en/emqx/latest/admin/api.html)、[封禁名单 API](https://docs.emqx.com/en/cloud/latest/api/dedicated.html) 和 [客户端断开 API](https://docs.emqx.com/en/cloud/latest/api/clients_v5.html)。本地 HTTP 模拟服务器覆盖封禁顺序、失败恢复、身份限制及重定向拒绝；2026-09-09 已在用户虚拟机上的真实 EMQX 完成凭据轮换/禁用、主动断连、旧 JWT 重连拒绝与新凭据上报验证，详见下文。


2026-09-09 续验：前轮 Go 全量、GB26875 独立 module、前端 45 项测试/构建及真实 Edge 浏览器均通过。本轮使用现有本地依赖配置，已在真实 PostgreSQL 的独立临时 schema 中通过新增表的重复迁移、凭据冲突事务回滚、命令去重、跨设备回执隔离、终态保护和历史查询；测试结束删除自身 schema。真实 MQTT Broker 验证设备向导、JWT 换取、属性 Raw 归档、MQTT 下行命令和 Raw 回执关联通过。没有部署业务服务，也未验收真实厂商设备。随后使用短时临时管理 API Key 完成真实 Broker 主动撤销验证；测试 Key 已删除，未将其配置为业务服务长期凭据。

可重复执行：

```text
# 配置 IOT_TEST_POSTGRES_DSN 后；仅在该数据库创建并清理临时 schema
 go test ./internal/adapters/postgres -run TestDeviceOperationsMigrationAndAtomicity -count=1 -v
# 配置 IOT_TEST_MQTT_BROKER 和 IOT_TEST_MQTT_JWT_SECRET 后
 go test ./internal/httpapi -run TestStandardMQTTLiveBroker -count=1 -v
```

MQTT 集成测试使用随机临时租户、独立内存业务库、临时 Raw 目录、clean session 和非保留消息；结束后关闭客户端。测试需要与 Broker 一致的 JWT 签名密钥，用环境变量注入，勿写入代码或命令历史。未配置时显式跳过。Broker 管理验收需要另外配置 `IOT_EMQX_API_URL/IOT_EMQX_API_KEY/IOT_EMQX_API_SECRET`，不能用 MQTT JWT 替代管理 API Key。

### 真实 Broker 撤销验收（2026-09-09）

使用现有本地配置访问用户虚拟机上的 EMQX 管理 API，创建有效期 20 分钟的专用临时 API Key，执行 `TestStandardMQTTLiveBroker/CredentialRevocation`。实际通过：

- 平台轮换设备凭据后，撤销任务状态为 REVOKED，原 MQTT 会话断开。
- 旧 Secret 无法再次换取设备 JWT；尚未过期的旧 JWT 重连收到 Broker CONNACK 5（未授权），不是用网络超时作为拒绝证据。
- 新凭据可换取 JWT、建立连接并上报属性，经 Raw 链路归档解析。
- 禁用新凭据后，新会话同样断开，其 JWT 重连被 Broker 拒绝。
- 重复撤销同一旧 username 成功，非目标的平台连接保持健康。

测试结束删除自身创建的封禁记录与 API Key，客户端关闭，无业务服务部署或 Broker 认证规则更改。长期业务服务仍需配置自己的管理 API Key；临时测试成功不表示已替业务进程启用该配置。

复测除 MQTT 测试变量外，提供 `IOT_TEST_EMQX_API_URL`、`IOT_TEST_EMQX_API_KEY`、`IOT_TEST_EMQX_API_SECRET`。子测试仅封禁该次测试随机生成的设备用户名，并注册清理；不要把真实设备凭据代入测试。

### 验收清单复核补齐（2026-09-09）

复核时现有 P0/P1 主体已实现，本轮没有新增重复模块或数据库表。设备连接详情新增当前租户/设备最近 5 条告警；非标准设备协议元数据优先使用当前产品绑定，避免发布或回滚后继续展示 Profile 中旧版本；标准设备仍固定 iot-standard@1.0.0。会话列表单独展示真实会话协议及版本，体现半包/未完成命令期间延迟切换的状态。

向导在 MQTT/HTTP 之间切换时，保留与所选上报类型一致的样例；完成页补充 command-reply 主题、关联字段及设备命令去重说明。接口回归覆盖绑定更新、命令能力和告警租户隔离；浏览器回归覆盖状态样例切换及最近告警区域。

本轮验收复核完成：Go 全量测试、GB26875 独立 module、前端 45 项测试及构建通过。真实 Edge 浏览器通过节点登记、MQTT/HTTP 状态样例切换、测试预览、设备启用、连接历史、最近告警区域及移动端抽屉检查。浏览器脚本下拉选择器已按现有 Element Plus 点击事件绑定修正。

### 本地长期配置与重连/告警续验（2026-09-09）

已在用户虚拟机 EMQX 创建本地业务专用管理 Key，并将根地址、Key、Secret 写入 Git 忽略的 `.env.local`；没有把秘密写入仓库或本报告。该 Key 用于后续本地 API 启动的凭据撤销能力，需按业务凭据管理与轮换；本次没有部署或重启业务服务。

使用这份长期配置再次运行真实 MQTT 集成测试，已通过 clean-session 断开后重连、属性 Raw 归档、阈值告警触发、相同消息 ID 重传去重和告警恢复；触发及恢复输入均能找到 Raw 归档，单次触发计数保持 1。同时重新通过命令/回执和轮换/禁用 Broker 断连及旧 JWT 拒绝验证。测试告警、规则及设备仅保存于独立临时存储，没有写入现有业务租户。

以上是受控短时重连与消息闭环验证，不等同于长时间断网、设备固件重试行为或厂商真机验收。P0/P1 编码已完成；剩余真机验收需要可访问的测试设备及其协议/报文，完整 Edge Agent 与独立 Gateway 仍按原要求保留为 P2。


### 三项代码与界面补齐（2026-09-09）

- 设备接入 → 设备接入实例：展开行显示当前在线会话（设备、远端地址、协议版本、最后有效报文）及最多 20 台关联设备，按创建时间倒序展示；刷新使用 `/api/v1/connectors` 的运行时快照。
- 历史设备详情：不要求存在 `connectorProfileId` 标签；依据同租户、同产品的明确 DeviceID 配置或已识别在线会话关联已有实例，不修改历史设备。多个候选实例返回 `profiles`，可通过 `GET /api/v1/device-registry/{id}/connection?profileId={profileId}` 选择；未选择且没有有效显式绑定时不提供默认命令路由。仅同产品不是关联证据，离线且无配置关联的历史 Listener 设备仍不能推断实例。
- 统一测试结果：验证失败仍返回 HTTP 422，同时携带 Connector Result；前端保留失败结果供查看。认证拒绝、超时、网络错误分别为 `AUTH_FAILED`、`TIMEOUT`、`NETWORK_ERROR`，解析失败保留原始报文和协议元数据。MQTT 使用独立 clean-session MQTT 3.1.1 握手验证平台当前凭据，不替换订阅连接，不发布业务数据，也不代表设备侧网络已验证。

新增回归覆盖 MQTT CONNACK 4/5、握手超时、统一错误字段、历史轮询/监听设备、多实例选择、租户及产品隔离。真实浏览器覆盖实例会话/关联设备展开、历史设备实例选择；真实虚拟机 Broker 复测包括主动握手、消息/命令链路、重连告警与凭据撤销。没有新增 migration，也没有部署或重启业务服务。

## MQTT 持久接收与部件告警（2026-09-10）

平台生产装配已由内存入队确认改为本机持久队列确认，固定客户端 ID 保存在持久目录中；满队列不确认，后续业务处理失败保留重试。部件状态可以通过标准 event 的 `data.components` 上报。当前契约、容量、认证拒收、运维指标和保障边界集中在 [部件告警与 MQTT 持久接收](DEVICE_RECEIVE_RELIABILITY.md)。本节取代历史记录中平台运行连接采用 clean session / 仅内存队列的描述；独立认证 Probe 仍使用 clean session。
