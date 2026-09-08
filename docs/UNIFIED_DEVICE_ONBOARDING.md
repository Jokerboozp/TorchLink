# 统一设备接入

本入口位于 **设备管理 → 添加设备**。流程为：选择或新建产品 → MQTT / HTTP / Modbus TCP / TCP / UDP → 参数配置 → 接入测试 → 数据预览 → 完成并启用。原有高级注册、协议发布/回滚、采集实例和报文回放仍然保留。视频设备跳转原摄像头管理；Edge Agent 暂不开放。

## 实现与资源边界

- `internal/connector`：通信控制面的 `Type`、`Connector.Test`、统一测试结果和 `Instance` 投影。不统一强制各 Runtime 的 Start/Stop。
- `internal/onboarding`：配置校验、协议选择、标准凭据、点表编译、测试和资源编排，不依赖 HTTP。监听状态、MQTT 健康检查由启动装配注入。
- `internal/model/onboarding.go`：一次原子保存的 `OnboardingBundle`。PostgreSQL 在事务内新增资源，内存实现使用同一写锁。重复设备、产品、端口冲突均拒绝，不覆盖原记录。
- Connector 类型和关联 Profile ID 保存在已有 `ManagedDevice.Tags` 中；连接配置复用 `DeviceAccessProfile`。**没有新增表或 migration**，沿用现有 JSONB、哈希字段和不可变发布表。
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

统一 JSON 格式为 `id`、正数毫秒 `timestamp`、非空对象 `data`。`kind` 为 `property`、`event` 或 `state`，分别形成 PROPERTY_REPORT、EVENT_REPORT、STATE_CHANGE。`state.data.connectionStatus` 可使用 CONNECTED / DISCONNECTED / UNKNOWN，状态更新也发生在解析之后。告警继续由现有规则或原协议的 ALARM_REPORT 处理。

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

JWT ACL 精确到设备的三个上行主题与一个下行主题；新标准设备不授予旧 raw topic 发布权限。平台服务令牌加入 `/iot/up/#` 订阅权限。Broker 必须沿用部署中的 JWT 校验和拒绝未授权访问配置；自定义服务账号须自行授予订阅权限。标准订阅拒绝 retained 消息，并再次校验当前设备/产品及凭据启用状态。

链路始终为：**传输 → 保留原始 JSON 的 RawMessage → 原始归档 / 幂等索引 → 内部队列 → StandardParser → StandardMessage → 原有存储 / 规则 / 告警 / AI**。设备正文不能指定租户、Parser、协议版本或跳过归档。标准下行 topic 已预留 ACL；本次不新增 MQTT Command 编码服务。

## 凭据边界

复用 `ManagedDevice.AccessKey / SecretHash`，Secret 使用密码学随机数生成，仅保存 SHA-256 哈希；首次创建/重新生成返回一次，列表和详情不返回哈希或原文。禁用清空哈希；重新生成沿用现有轮换接口。

HTTP 原 Secret 在轮换后立即失效。禁用后标准 MQTT 入站也拒绝数据。**轮换并不主动断开已经通过认证的 MQTT 会话**；已签发 JWT/已连接会话的撤销取决于 Broker 的过期断连、重新鉴权或管理 API。当前未实现主动踢除会话接口，不能把平台凭据轮换等同于 Broker 实时撤销。

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

当前增加连接详情、已有 Session 命令入口和 Collector 字段复用；完整物模型、EdgeNode、历史连接事件汇总、MQTT 下行命令及 Broker 主动撤销仍属后续工作。独立 Access Gateway、完整 Edge Agent、Modbus RTU 和其他 P2 协议未实施。

下一阶段优先使用真实设备与实际 PostgreSQL/EMQX 验证完整接入、凭据轮换和长连接，再考虑 Broker 主动撤销、共享限流与独立网关部署。
