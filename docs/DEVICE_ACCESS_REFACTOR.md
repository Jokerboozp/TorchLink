你现在要修改的是当前工作区中已打开的本地 iot-platform 项目。

本次任务不是重新创建一个项目，也不是只输出架构建议，而是在现有代码基础上，直接完成第一阶段的设备接入体系改造。

核心目标：

“统一设备接入向导 + Connector 抽象 + 标准 MQTT/HTTP 设备协议 + 设备级认证 + 现有 Modbus/TCP/UDP 接入能力整合”。

以本地当前实现为准，最大限度复用已有代码，不推倒重构，不一次性拆成大型微服务体系。

---

# 一、本地工作区与操作约束

## 1. 确认当前项目

开始前：

1. 确认当前工作目录和项目根目录。
2. 阅读根目录及相关子目录中的 AGENTS.md，遵守适用于当前文件的项目约定。
3. 查看 git status，了解当前分支、未提交修改和未跟踪文件。
4. 阅读 README、当前有效的设备接入文档、部署配置及测试入口。
5. 以本地代码为实现依据，包括用户尚未提交的修改。

下面提到的文件名、类型和能力，只是待核实的检查线索，不代表它们在当前本地版本中一定存在或已经完整实现。

不要根据旧文档、历史设计或文件名猜测功能状态。

## 2. 不操作远程仓库

本次直接修改当前本地工作区：

- 不要重新克隆 GitHub 仓库。
- 不要通过 GitHub API 修改远程文件。
- 不要主动执行 git pull。
- 不要主动切换分支或创建其他工作区。
- 未经明确要求，不要自动 commit、push 或创建 PR。
- 允许按测试和构建需要安装项目已有依赖，但不要因此升级整个技术栈。

## 3. 保护已有工作

禁止：

- git reset --hard。
- git clean。
- 撤销与本次任务无关的修改。
- 覆盖或删除用户已有的未提交代码。
- 为了整理代码而批量格式化无关文件。
- 删除生产数据或重建生产数据库。
- 自动修改真实设备配置。
- 将真实密钥、设备密码或 Token 写入源码、测试、日志和文档。

如果需要修改的文件已经存在本地改动，先理解它，再增量实现。

对于常规设计选择，按低风险、兼容现有实现的方式推进，不要反复要求用户确认。遇到可能丢失数据、覆盖用户工作或影响真实设备的操作时，不执行该危险操作，继续完成其他安全的实现部分，并明确记录阻塞原因。

---

# 二、先核实项目已有能力

重点检查以下对象及其实际调用关系：

- Product / ManagedDevice。
- DeviceState。
- RawMessage / StandardMessage。
- ProtocolDefinition / ProtocolRelease。
- ProductProtocolBinding。
- DeviceAccessProfile。
- PointTableRelease / ModbusReadBlock。
- Go Protocol Package。
- go-protocol-v2。
- MQTT Adapter。
- TCP/UDP Listener。
- Modbus TCP Runtime。
- 原始报文归档、解析、回放。
- 规则、告警、AI。
- 视频设备、摄像头映射和视频告警。

可能相关的路径：

```text
cmd/iot-platform/
internal/model/
internal/core/
internal/ports/
internal/httpapi/
internal/parser/
internal/protocolbuild/
internal/protocolworker/
internal/protocolruntime/
internal/adapters/mqtt/
internal/adapters/video/
internal/adapters/postgres/
iot_front/src/views/
docs/
compose*.yaml
ops/emqx/
```

尤其检查：

```text
internal/protocolruntime/listeners.go
internal/protocolruntime/modbus.go
internal/adapters/mqtt/client.go
internal/core/engine.go
internal/model/model.go
```

前端可能相关：

```text
ProductsView.vue
DevicesView.vue
ProtocolsView.vue
ProtocolAssistantView.vue
TestDeviceView.vue
IntegrationView.vue
RawView.vue
CameraMappingsView.vue
```

已经实现的功能直接复用、整合或补齐，不要重复建设。

注意区分：

- 当前可用实现。
- 仅有模型或接口的实现。
- 历史兼容实现。
- 文档中的计划功能。
- 已被新方案替代的旧入口。

如果文档和代码不一致，在实施计划中说明，并以实际代码与本次目标为依据选择兼容方案。

---

# 三、本次范围与优先级

本次只以第一阶段为必须交付范围。

## P0：必须形成可工作的完整链路

1. Connector 类型、能力描述和统一测试抽象。
2. Device Onboarding Service。
3. 统一设备接入向导。
4. 标准 MQTT 设备上报协议。
5. 标准 HTTP 设备上报接口。
6. 设备级凭据及实际生效的认证、授权。
7. 现有 Modbus TCP 能力的接入流程整合。
8. 现有 TCP/UDP Listener 的接入流程整合。
9. 连接诊断、原始报文和解析预览。
10. 保存、启用、等待首条数据的完整流程。
11. 必要的数据迁移、兼容处理、测试和文档。

## P1：在 P0 稳定后补齐

1. 设备详情中的接入信息和连接诊断。
2. Connector Runtime 状态。
3. 已有命令能力的设备详情入口。
4. 产品属性、事件、命令的基础描述。
5. 为 EdgeNode / Collector 和独立接入进程保留边界。

## P2：本次不强行实现

- 完整 Edge Agent。
- 独立 Access Gateway 进程。
- Modbus RTU / RS485。
- OPC UA / BACnet / SNMP。
- ONVIF / GB28181。
- 分布式采集调度。
- 完整数字孪生和设备影子体系。
- 大型插件市场、远程代码发布平台。

不要为了 P2 阻塞 P0。

不要将只有页面、模型或空接口的功能标记为“已支持”。

---

# 四、目标架构与职责边界

总体方向：

```text
设备 / 网关 / 第三方系统
          ↓
      Connector
          ↓
      RawMessage
          ↓
    Ingest / Archive
          ↓
    Protocol / Codec
          ↓
    StandardMessage
          ↓
属性 / 事件 / 状态 / 规则 / 告警
          ↓
       查询 / AI
```

必须区分以下概念。

## 1. Connector：负责通信

包括：

- 监听或连接。
- 接收和发送。
- 重连。
- 轮询。
- 超时。
- 会话管理。
- 连接诊断。
- 通信资源生命周期。

Connector 不解释业务字段含义。

对于 MQTT，Broker 继续负责 MQTT 客户端连接。平台 Connector 负责接入配置、认证授权衔接、消息订阅和标准化入口，不要重新实现 MQTT Broker。

## 2. Protocol / Codec：负责协议语义

包括：

- 帧边界判定。
- 校验。
- 原始数据解析。
- 字段映射。
- decode / encode。
- 协议所需的有限会话状态。

宿主运行时负责管理字节缓冲、Socket 和超时，协议包负责说明哪些字节组成完整帧以及如何解释，不要让两者重复维护同一套分帧逻辑。

复杂协议继续复用现有 Go Protocol Package。

## 3. Product / Thing Model：负责数据含义

逐步描述：

- Properties。
- Events。
- Commands / Services。
- 类型、单位、枚举和读写能力。
- 对应的协议绑定。

第一阶段不要求完整物模型引擎，也不强制迁移所有历史产品。

---

# 五、Connector 抽象设计

新增轻量、可扩展的 Connector 层。

不要为了统一接口而重写已有 Runtime。

可参考：

```go
type ConnectorType string

const (
    ConnectorMQTT      ConnectorType = "MQTT"
    ConnectorHTTP      ConnectorType = "HTTP"
    ConnectorTCP      ConnectorType = "TCP"
    ConnectorUDP      ConnectorType = "UDP"
    ConnectorModbusTCP ConnectorType = "MODBUS_TCP"
    ConnectorEdge      ConnectorType = "EDGE"
)

type Connector interface {
    Type() ConnectorType
    Capabilities() ConnectorCapabilities

    Test(
        ctx context.Context,
        req ConnectorTestRequest,
    ) (ConnectorTestResult, error)
}
```

长期运行能力和命令能力采用可选接口，不要求所有类型实现没有意义的方法：

```go
type RuntimeConnector interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Status(ctx context.Context) (ConnectorRuntimeStatus, error)
}

type DeviceCommandSender interface {
    Send(
        ctx context.Context,
        req DeviceCommandRequest,
    ) (DeviceCommandResult, error)
}
```

以上只是接口设计参考，最终命名以项目习惯为准。

要求：

- 不支持的能力明确返回 unsupported。
- 不要用空方法返回 success。
- 支持 context 取消和明确超时。
- 测试和采集具有并发上限。
- 不要在收到每条消息后无限创建 goroutine。
- 不要让 Connector 直接依赖 HTTP Handler 或前端请求对象。
- 避免循环依赖。

可以使用 Registry 统一提供 Connector 类型、能力和表单描述，但不要引入复杂插件框架。

---

# 六、数据模型与现有 DeviceAccessProfile 的关系

第一阶段优先保持已有存储结构。

当前 DeviceAccessProfile 可能同时承载：

- 主动采集参数。
- Listener 参数。
- Product / Device。
- Protocol / Version。
- Collector。
- AutoRegister。
- Runtime 状态。

不要直接删除或大规模迁移。

优先采用以下方案之一：

1. 在现有 Profile 上增加必要字段并提供统一服务层。
2. 新增薄的 ConnectorInstance 对象，明确关联已有 Runtime Profile。
3. 如果现有模型已经足够，仅新增 DTO 和编排服务。

必须避免同时出现两份彼此独立、都可以被修改的连接配置。

需要明确：

- 哪个对象是配置事实来源。
- 哪些字段是派生展示。
- 如何同步运行时。
- 如何处理启用、停用和失败。

## 共享接入实例与单设备参数必须区分

例如：

```text
一个 TCP Listener
    ├── Device A
    ├── Device B
    └── Device C
```

不要每添加一台设备就启动一个相同端口的 Listener。

同样，产品协议配置可以复用，但设备 IP、Unit ID、凭据等不能放到所有设备共享的产品配置中。

必要时采用：

```text
ConnectorInstance：共享通信入口
DeviceEndpoint：单设备通信参数
DeviceCredential：单设备凭据
```

但不要为了形式完整而创建没有使用场景的表。

---

# 七、统一设备接入向导

新增统一入口：

```text
设备管理 → 添加设备
```

或：

```text
设备接入 → 新建设备接入
```

保持现有前端风格。

## Step 1：选择产品

支持：

- 选择已有产品。
- 快速创建产品。
- 展示产品已有协议和支持的接入方式。

重要限制：

- 添加一台设备时，不得静默修改整个产品的协议绑定。
- 如果新绑定会影响已有设备，显示影响范围。
- 默认复用现有绑定。
- 协议切换应通过明确的发布/绑定动作完成。
- 不得为了向导方便覆盖已发布协议版本。

## Step 2：选择接入方式

展示卡片：

- MQTT 上报。
- HTTP 上报。
- Modbus TCP。
- TCP / UDP。
- Edge Agent。
- 视频设备。

Edge Agent 当前没有完整实现时，标记“规划中”或禁用，不提供虚假的启用按钮。

视频设备只进入项目已实际支持的接入流程。

## Step 3：配置设备和通信参数

根据类型显示不同表单。

### MQTT

展示或生成：

- Device ID。
- Client ID。
- Username。
- Device Secret。
- 外部可访问的 Broker 地址。
- TLS 要求。
- 上报 Topic。
- 命令 Topic。
- 消息示例。

### HTTP

展示或生成：

- 设备标识。
- 凭据。
- 对外上报 URL。
- 请求 Header。
- 消息示例。
- 可复制的 curl 示例。

### Modbus TCP

配置：

- Host。
- Port。
- Unit ID。
- Timeout。
- Retry。
- Poll Interval。
- 点表或已有点表版本。
- 当前实现支持的读取选项。

### TCP / UDP

配置：

- 网络类型。
- 复用已有 Listener 或创建新 Listener。
- 监听 IP。
- 监听端口。
- Product。
- 已发布协议版本。
- 是否允许自动登记。
- 可选的固定 Device ID。

自动登记默认关闭。

## Step 4：测试与诊断

根据接入方式提供真实可执行的测试，不强行使用同一种测试流程。

主动采集设备可以先测试再保存。

被动上报设备需要地址、凭据或 Listener 后才能发送数据，因此允许：

```text
保存草稿 / 准备接入资源
          ↓
获取连接信息
          ↓
等待真实设备上报
          ↓
完成验证并正式启用
```

不要形成“尚未创建设备凭据，却要求设备先认证上报”的循环。

## Step 5：报文和字段预览

展示：

```text
原始报文
    ↓
协议与版本
    ↓
解析结果
    ↓
属性 / 事件 / 告警语义
```

显示：

- 原始 HEX / JSON。
- 协议 ID 和实际使用版本。
- 解析状态。
- 字段类型和单位。
- 失败位置或错误原因。
- 数据来源是真实设备、模拟器还是样例。

样例解析成功不能冒充真实设备接入成功。

## Step 6：保存、启用和首条数据确认

后端自动编排必要资源。

完成后进入设备详情，并展示：

- 配置是否保存。
- 运行时是否启用。
- 是否已收到真实报文。
- 是否已成功解析。
- 最近失败原因。
- 下一步应检查什么。

“保存成功”和“设备已在线”必须分开。

---

# 八、Onboarding Service 与一致性

新增独立的 Onboarding Service。

不要将资源编排全部写进 HTTP Handler。

负责：

- 校验请求。
- 检查租户权限。
- 校验产品和协议绑定。
- 复用或创建设备。
- 复用或创建接入实例。
- 生成必要凭据。
- 保存采集参数。
- 请求启用 Runtime。
- 返回接入说明和当前状态。

## 幂等要求

完成向导的操作必须支持幂等键或等效机制。

重复点击、浏览器重试、网络超时后重试，不得重复创建：

- Product。
- Device。
- Listener。
- 凭据。
- 采集任务。

同一幂等键配不同内容，应明确报冲突。

## 数据库与运行时分开处理

数据库保存和外部副作用不是同一事务。

建议采用：

```text
事务保存配置和目标状态
          ↓
Runtime reconcile / 启用
          ↓
记录实际运行状态
```

不要将数据库提交成功解释为 Socket 已监听成功。

如果启用失败：

- 保留可诊断状态。
- 允许修复后重试。
- 不删除用户原有资源。
- 不影响同一个共享 Listener 下的其他设备。
- 仅对本次创建且确定无人使用的临时资源执行清理。

---

# 九、统一测试与预览结果

设计统一测试结果，但允许不同类型返回不同步骤。

可参考：

```go
type ConnectorTestResult struct {
    Success     bool
    Stage       string
    Message     string
    LatencyMs   int64
    RawRequest  string
    RawResponse string
    DeviceID    string
    Parsed      any
    ErrorCode   string
    Source      string
}
```

复杂诊断可以增加：

```text
checks[]
warnings[]
runtimeStatus
receivedAt
protocolId
protocolVersion
```

需要区分：

- CONFIG_VALIDATED。
- NETWORK_REACHABLE。
- LISTENER_READY。
- AUTHENTICATED。
- WAITING_FOR_DATA。
- RAW_RECEIVED。
- PARSED。
- FAILED。
- UNSUPPORTED。

具体枚举可以调整，但语义必须明确。

特别注意：

- TCP Listener 启动成功不代表设备已连接。
- MQTT Broker 可达不代表目标设备认证成功。
- 平台自发模拟消息不代表真实设备已上报。
- HTTP 测试请求成功不代表现场网络可达。
- 解析预览不得触发真实告警、设备控制或修改生产状态。

临时监听、测试连接和测试会话必须有超时及清理机制。

网络测试入口只允许有权限的用户使用，并限制可访问的目标范围，避免成为任意网络探测接口。

---

# 十、标准 MQTT 设备协议

新增平台默认设备协议，使符合协议的设备无需编写自定义 Parser。

保持旧 MQTT RawMessage 接入兼容。

建议新增 Topic：

```text
/iot/up/{tenantId}/{productId}/{deviceId}/property
/iot/up/{tenantId}/{productId}/{deviceId}/event
/iot/up/{tenantId}/{productId}/{deviceId}/state
/iot/up/{tenantId}/{productId}/{deviceId}/command_reply

/iot/down/{tenantId}/{productId}/{deviceId}/command
```

消息体增加明确的版本约定，至少在文档中固定为 v1。

## 属性上报

```json
{
  "id": "msg-001",
  "version": "1.0",
  "timestamp": 1788850000000,
  "data": {
    "temperature": 26.5,
    "smoke": 0,
    "battery": 87
  }
}
```

## 事件上报

```json
{
  "id": "msg-002",
  "version": "1.0",
  "timestamp": 1788850000000,
  "event": "fire_alarm",
  "data": {
    "zone": 3,
    "level": "HIGH"
  }
}
```

事件是否生成告警，要通过明确的事件映射或规则决定，不能仅根据字符串中包含 alarm 就直接生成生产告警。

## 状态上报

```json
{
  "id": "msg-003",
  "version": "1.0",
  "timestamp": 1788850000000,
  "online": true
}
```

## 命令下发

```json
{
  "id": "cmd-001",
  "version": "1.0",
  "timestamp": 1788850000000,
  "command": "read_status",
  "params": {}
}
```

## 命令应答

```json
{
  "id": "msg-004",
  "version": "1.0",
  "timestamp": 1788850001000,
  "commandId": "cmd-001",
  "success": true,
  "data": {
    "status": "normal"
  }
}
```

要求：

- Topic 身份必须与已认证身份和 ACL 一致。
- 消息体不能覆盖租户、设备或产品归属。
- 不接受设备自行指定内部协议版本、Parser 或 Collector 身份。
- 校验消息大小、字段类型、层级深度和时间格式。
- 同时保留设备时间和平台接收时间。
- 对异常设备时间给出明确处理策略。
- 命令默认不使用 retained 消息，避免重连后执行旧命令。
- QoS 1 不等于业务层恰好处理一次，必须有业务幂等。
- 不要因为重新订阅或重连重复产生告警。

数据链路：

```text
MQTT
→ 身份和 Topic 校验
→ RawMessage
→ 标准设备协议解析
→ StandardMessage
→ 现有存储、规则和告警
```

---

# 十一、设备凭据、MQTT 认证与授权

新增实际可用的 DeviceCredential 能力。

不能只生成一个 Device Secret 展示在前端，却没有任何接入端真正校验它。

## 凭据要求

至少支持：

- 生成。
- 使用。
- 轮换。
- 禁用。
- 撤销。
- 记录创建和变更审计。

Device Secret：

- 使用安全随机数生成。
- 不使用设备 ID、时间戳或固定字符串充当密码。
- 默认只保存不可逆摘要，沿用项目已有可靠方案。
- 明文只在创建或轮换成功时返回一次。
- 日志、错误和审计记录必须脱敏。
- 不放入 URL 查询参数。
- 不写入普通设备配置 JSON。
- 前端不要长期保存在 localStorage。

HTTP 可以采用 TLS 下的设备标识加 Secret 校验。

不要为本次任务设计未经验证的自定义密码协议。

## MQTT 必须接上真实认证链路

检查当前 EMQX 版本和配置，选择与现有部署兼容的实现，例如：

- EMQX HTTP Authentication。
- EMQX HTTP Authorization。
- 或项目已经使用且能够支持设备级权限的认证方式。

必须完成平台接口和部署配置的衔接，不仅是预留接口。

要求：

1. 错误凭据拒绝连接。
2. 禁用设备拒绝连接。
3. 设备只能发布自己的上行 Topic。
4. 设备只能订阅自己的下行 Topic。
5. 禁止订阅其他租户或其他设备 Topic。
6. 默认拒绝未匹配权限。
7. 认证服务不可用时不能退化为匿名放行。
8. 普通设备凭据不能访问后台管理 API。
9. 平台服务账号、第三方集成账号和设备账号分开授权。
10. 不破坏现有平台内部消息订阅。

凭据轮换和禁用时，需要处理认证缓存和现有连接。

如果当前 Broker 无法立即断开旧连接，明确记录实际生效边界和缓存时间，不得声称“立即撤销”却继续允许旧会话无限使用。

生产使用 TLS；本地明文调试必须明确限制在隔离测试网络，不要默认公开匿名 1883 服务。

---

# 十二、HTTP 标准设备上报

新增或复用设备专用入口，例如：

```text
POST /api/device-ingest/v1/{tenantId}/{productId}/{deviceId}/property
POST /api/device-ingest/v1/{tenantId}/{productId}/{deviceId}/event
POST /api/device-ingest/v1/{tenantId}/{productId}/{deviceId}/state
POST /api/device-ingest/v1/{tenantId}/{productId}/{deviceId}/command-reply
```

具体路径遵循现有 API 风格。

要求：

- 独立设备认证。
- 路径身份与凭据归属严格匹配。
- 服务端确定最终租户、产品和设备身份。
- Body Size Limit。
- 请求超时。
- 按设备的速率限制。
- 消息字段校验。
- 幂等处理。
- 清晰错误码。
- RawMessage 审计链路。

建议状态码语义：

- 400：格式错误。
- 401：凭据无效。
- 403：无权访问或设备禁用。
- 409：幂等键或资源冲突。
- 413：消息过大。
- 429：超过限制。
- 503：当前无法可靠接收。

根据现有项目约定合理调整。

返回“已接收”时，应明确是可靠接收还是解析完成，不要混淆两者。

平台已经可靠接收后，后续解析失败应有可查询状态，不能让客户端误认为必须不断重发同一条消息。

提供无真实密钥的调用示例和模拟器。

---

# 十三、TCP / UDP 接入整合

充分复用已有 Listener 和 go-protocol-v2。

不要重写已经存在的：

- TCP Accept。
- UDP 接收。
- 半包和粘包处理。
- ingress。
- Device ID 识别。
- Session State。
- Reply。
- Correlation ID。
- Command。
- 协议切换和历史版本固定。

本次主要补齐：

1. Connector 控制面。
2. 接入向导。
3. Listener 创建和复用。
4. 运行状态。
5. 在线会话数量。
6. 最近接收和解析状态。
7. 明确的错误诊断。
8. 已有下行能力的统一入口。

安全要求：

- 默认只接收已登记设备。
- 开启自动登记需要明确权限和提示。
- 识别出 Device ID 不等于完成密码学认证。
- 没有认证能力的传统协议应限制在可信网络、VPN 或明确的网络白名单内。
- 不得把任意来报设备直接归入任意租户。
- 限制连接数、缓冲大小、空闲时间和未识别连接。
- 不允许协议包决定数据库归属或绕过设备禁用状态。

部署要求：

- 检查监听端口是否已被占用。
- 明确区分程序监听地址与设备实际应连接的对外地址。
- 容器内启动监听不代表宿主机端口已经映射。
- 在 UI 和文档提示端口映射、防火墙和网络条件。
- 不要未经授权自动开放主机防火墙。

拓扑要求：

一个 Listener 可以接收多台设备，不等于一个设备会话天然支持任意多个子设备。

先核实当前会话身份约束。网关与子设备映射没有实现时，不要在向导中宣称已经支持；可以保留模型扩展点。

---

# 十四、Modbus TCP 接入整合

检查现有 Modbus 主动采集、点表导入和新旧协议策略。

特别注意：

如果当前版本已停止通过旧入口新增内置 Modbus 协议，不要简单恢复旧接口并绕过现有协议治理。

应复用已经存在的读取、点表解析和采集计划能力，将其包装成明确的标准 Connector 路径，或接入当前有效的版本化协议方案。

在实施说明中解释采用哪条兼容路径。

功能要求：

- CSV / XLSX 点表导入或已有点表选择。
- 导入预览。
- 读取块预览。
- 单次连接测试。
- 单次只读采集。
- Request HEX。
- Response HEX。
- 点位解析结果。
- 超时和异常码诊断。
- 保存并启用持续采集。

校验至少覆盖：

- 地址起始约定。
- 0-based 与 40001 等表示的歧义。
- 功能码。
- 寄存器数量。
- 数据类型。
- 字节序和字序。
- 缩放比例。
- 单位。
- 重复标识。
- 越界地址。
- 不兼容的读取块。

不确定的信息必须要求明确配置，禁止静默猜测。

运行要求：

- 限制并发。
- 同一设备避免重叠轮询。
- 超时、取消和重试可控。
- 校验事务 ID、功能码、响应长度和异常码。
- 无效数据不能填零后进入正常业务计算。
- 不得仅因为 Connector 配置为启用就显示设备在线。
- 必须保留请求上下文和实际协议/点表版本。
- 主动连接目标受已有网络策略或明确白名单限制。

本次默认只读，不自动增加真实设备写寄存器操作。

数据继续进入：

```text
Modbus Response
→ RawMessage
→ 现有协议解析
→ StandardMessage
```

---

# 十五、统一数据链路、幂等和可靠性

所有经过身份校验的设备业务上报，都应经过统一原始数据链路。

优先复用：

```text
Connector
→ RawMessage
→ engine.IngestRaw()
→ 可靠存储 / 归档
→ 消息总线
→ Parser
→ StandardMessage
→ 业务处理
```

不要机械改变项目现有存储顺序，先核实当前实际实现和可靠接收边界。

RawMessage 至少能够追溯：

- tenantId。
- productId。
- deviceId。
- profileId / connectorId。
- collectorId。
- protocolId。
- protocolVersion。
- pointTableVersion。
- remoteAddress。
- receivedAt。
- 客户端消息 ID。
- 设备时间。
- 数据来源。

可信身份字段由宿主设置，不直接信任设备传入。

## 幂等

建议以：

```text
tenantId + deviceId + messageType + clientMessageId
```

或项目已有等效键进行业务去重。

同一消息 ID 对应不同有效载荷时，应记录冲突，不能静默覆盖历史数据。

同一上报通过重试、重连或补传到达，不得重复产生业务告警。

## 可靠性

- 不要把收到 MQTT 消息或发送 PUBACK 等同于业务落库成功。
- 审查 MQTT 回调、队列和持久化之间的失败窗口。
- 对无法处理的消息提供重试、失败记录或明确的拒绝策略。
- 不要无界排队。
- 对 TCP/UDP 的协议 ACK，保留协议要求，并明确“可靠接收”与“业务处理完成”的区别。
- 不要等待 AI 分析完成才应答设备。
- 原始数据已经接收但解析失败时，保留原文和原因。
- 不伪造 StandardMessage，不使用默认零值掩盖解析失败。

## 连接生命周期事件例外

Socket 断开、Broker 断连等运行时事件可以通过统一的、可审计的设备状态服务更新。

不要为了“所有数据都必须有 RawMessage”而伪造不存在的设备原始报文。

连接状态、数据新鲜度和业务状态应保持区分。

历史回放和调试预览不能被当作设备刚刚在线，也不能默认触发真实命令和重复生产告警。

---

# 十六、设备详情页增强

接入完成后，设备详情应提供统一信息。

## 基本信息

- Device ID。
- Product。
- 接入方式。
- Connector / Profile。
- 实际使用的协议和版本。
- Collector / Edge 信息。
- 创建时间。

## 接入信息

- 对外 Broker 或 HTTP 地址。
- Client ID / Username。
- Topic。
- 消息示例。
- 凭据是否启用。
- 凭据轮换入口。

不要重复展示无法再次读取的明文 Secret。

## 运行状态

- 配置状态。
- Listener / Collector 状态。
- ConnectionStatus。
- DataStatus。
- BusinessStatus。
- Last Seen。
- 最近成功解析时间。
- 最近连接或采集错误。

没有可靠依据时显示 unknown，而不是猜测 online。

## 实时数据

- 最新属性。
- 最近事件。
- 最近告警。
- 数据时间和单位。

## 原始报文

- RawMessage。
- Parsed Result。
- 协议版本。
- 解析错误。
- 跳转已有原始报文页面。

优先复用现有查询接口，避免新增重复存储。

---

# 十七、命令下发与安全边界

优先复用现有在线 Session、encode 和应答关联能力。

命令入口需要：

- 租户与设备权限校验。
- 协议能力检查。
- 参数校验。
- 唯一 commandId。
- 超时。
- 审计。
- 结果查询或明确的同步结果。

状态至少区分：

```text
已创建
已发送
设备已确认接收
执行成功
执行失败
超时 / 结果未知
```

“发送成功”不能直接显示为“执行成功”。

没有设备执行结果的协议只能显示能够证明的状态。

默认不自动重试非幂等控制命令。

对于消防设备的复位、消音、启停等操作：

- 默认不开放自动执行。
- 需要明确权限和人工确认。
- 测试使用模拟设备。
- 不因 AI 输出而直接控制真实设备。

没有完整下行能力的接入类型，应隐藏或禁用命令按钮，不提供假的成功返回。

---

# 十八、产品物模型的最小演进

Product 逐步描述：

```text
Product
├── Properties
├── Events
├── Commands
└── Protocol Binding
```

本次仅做有实际使用价值的基础能力。

例如：

```text
Properties:
  identifier
  name
  dataType
  unit
  enum
  access

Events:
  identifier
  name
  parameters
  alarmMapping

Commands:
  identifier
  name
  inputSchema
  outputSchema
  riskLevel
```

要求：

- 旧产品没有物模型仍然可用。
- 已有协议解析结果不因缺少新模型被全部拒绝。
- 新模型可用于预览、展示和参数校验。
- 不同厂商字段可以映射到统一业务标识。
- 类型和单位不明确时，不猜测。
- 不把完整设备影子或 desired state 作为本次前置条件。

如果已有模型足够，直接复用，不再新建第二套。

---

# 十九、Edge Agent 的扩展边界

本次不要求完整实现 Edge Agent。

未来目标：

```text
现场设备
   ↓
Edge Agent
   ↓
MQTT / HTTPS
   ↓
平台接入链路
```

未来职责：

- 本地 Modbus TCP / RTU / Serial / TCP。
- 配置同步。
- 有限权限的协议执行。
- 本地持久化队列。
- 断线重连和补传。
- 心跳。
- 采集状态。
- 日志和诊断。

本次要求：

- 核实 CollectorID 已有用途。
- 在 Connector 和 Profile 中保留执行节点归属。
- 避免将采集逻辑和 HTTP API 强耦合。
- 不允许中心 Runtime 错误执行已分配给其他节点的采集任务。
- UI 中未实现的 Edge 能力明确标记。

可以提供未来模型草案，但没有实际消费方时，不必创建空表和假接口。

未来补传必须考虑幂等、乱序和数据时间，不要假定 MQTT QoS 能自动解决所有可靠性问题。

---

# 二十、视频接入保持独立领域

继续复用：

```text
internal/adapters/video/
```

以及已有摄像头映射、预览和视频告警能力。

统一向导可以展示“视频设备”，但进入独立流程。

要求：

- 只展示当前实际支持的厂商或协议。
- 不因存在 RTSP 地址字段，就宣称已支持完整视频接入。
- 不因存在某个适配器文件，就宣称整个 SDK 已联调。
- 不在本次顺带实现 ONVIF、GB28181 等新体系。
- 视频流不放进 RawMessage。
- 视频告警复用已有 VideoAlarmEvent 或等效模型，进入统一告警流程。
- 视频地址中的账号、密码和临时签名必须脱敏。
- 保持现有视频预览功能兼容。

---

# 二十一、API 与前端组织

## API

优先复用现有端点。

可能需要：

```text
GET    /api/connectors/types
GET    /api/connectors
POST   /api/connectors
GET    /api/connectors/{id}
PUT    /api/connectors/{id}
DELETE /api/connectors/{id}

POST   /api/connectors/test

POST   /api/device-onboarding/preview
POST   /api/device-onboarding/complete

POST   /api/devices/{id}/credentials/regenerate
POST   /api/devices/{id}/credentials/revoke

GET    /api/devices/{id}/connection
GET    /api/devices/{id}/latest-data
GET    /api/devices/{id}/raw-messages
```

这些是参考路径，不要为了命名一致重复实现已有 API。

后台配置接口使用用户权限体系；设备上报接口使用设备凭据体系，两者不能混用。

删除共享 Connector 时，检查引用关系，不得直接影响仍在使用它的设备。

对长时间等待首条数据的操作，使用项目已有 SSE、轮询或等效方式，不要让普通请求无限挂起。

## 前端

不要继续增加一堆互不关联的菜单。

建议整合为：

```text
设备管理
├── 设备列表
├── 产品管理
└── 添加设备

设备接入
├── 接入实例
├── 协议管理
└── 原始报文
```

允许遵循当前项目菜单风格调整。

新增或复用：

```text
DeviceOnboardingView.vue
DeviceDetailView.vue
ConnectorStatusPanel.vue
RawParsedPreview.vue
```

不要求机械采用这些文件名。

前端要求：

- 使用项目已有组件和样式。
- 不引入第二套大型 UI 框架。
- 表单按接入类型显示。
- 高级参数折叠。
- 切换步骤不意外丢失有效输入。
- 防止旧测试结果覆盖新配置的结果。
- 配置改变后，旧测试状态应失效。
- 明确展示真实设备、模拟器和样例来源。
- 请求失败和权限不足有明确提示。
- 一次性凭据不会因错误提示、截图日志或持久化状态泄露。
- 成功后跳转设备详情。

---

# 二十二、代码组织、迁移与部署兼容

## 代码组织

参考：

```text
internal/connectors/
    connector.go
    registry.go
    mqtt.go
    http.go
    tcp.go
    modbus.go

internal/onboarding/
    service.go
    service_test.go
```

如果现有项目更适合放在 internal/core 等位置，遵循现有风格。

不要为了 DDD 或接口形式做无意义拆包。

## 数据迁移

必须：

- 兼容已有数据。
- 保留旧协议和历史版本。
- 不删除现有表来重建。
- 不修改不可变协议内容。
- 为新字段提供兼容默认值。
- 为租户隔离和幂等提供必要约束。
- 同步所有实际使用的 Repository 实现。
- 不仅修改 PostgreSQL 而遗漏 memory 等测试实现。
- 提供迁移测试。

若项目使用启动时自动迁移，沿用既有机制，不平行引入另一套冲突方案。

## 旧链路兼容

保持：

- Go Protocol Package。
- Protocol V2。
- GB26875。
- 旧 Modbus 采集实例。
- 已有 MQTT RawMessage Topic。
- 协议发布和回滚。
- 原始报文回放。
- 规则、告警和 AI。
- 视频预览及视频告警。

## 为独立接入进程保留边界

未来允许拆分：

```text
cmd/iot-platform
cmd/iot-access-gateway
cmd/iot-edge-agent
```

本次可以继续单进程运行，但新增通信逻辑不能依赖 HTTP Handler。

明确当前部署的单实例或执行角色约束。

多实例问题需要正确区分：

- 同一网络命名空间下可能发生监听端口冲突。
- 独立容器不一定发生相同的端口冲突，但仍可能重复采集。
- 会话位于某个实例，命令需要到达该实例。
- Collector / Listener 配置不能被所有 API 副本无差别执行。

不要声称本次已经解决完整分布式调度。

## 配置与文档

更新：

- .env.example。
- 实际使用的 Compose 文件。
- EMQX 认证授权配置。
- 本地启动说明。
- 必要的在线和离线部署说明。

新增配置必须有说明和安全默认值。

对外接入地址应可配置，不要把 Docker 服务名、localhost 或固定个人 IP 当作所有设备的接入地址。

---

# 二十三、测试与验收

不能只用编译成功证明功能完成。

## 后端测试

至少覆盖：

- Connector 类型和能力。
- 不支持能力的明确返回。
- 连接测试超时、取消和并发限制。
- 标准 MQTT 消息解析。
- HTTP 设备认证和上报。
- DeviceCredential 生成、轮换、禁用。
- MQTT Auth / Authz。
- 跨设备和跨租户访问拒绝。
- Onboarding 幂等。
- 保存失败和启用失败。
- 共享 Listener 复用。
- 产品绑定不会被单设备接入静默修改。
- Modbus 测试读取。
- TCP/UDP 原有行为回归。
- 历史协议版本和回放。
- 非法数据不会产生伪造业务结果。

## 端到端链路

使用测试环境或模拟设备验证：

```text
创建或选择产品
→ 创建设备
→ 配置接入
→ 完成认证
→ 发送上报
→ 保存 RawMessage
→ 生成 StandardMessage
→ 查询设备数据和状态
```

至少分别覆盖：

- HTTP。
- MQTT。
- TCP/UDP 已有模拟器或测试程序。
- Modbus 本地模拟服务。

不要自动连接和控制真实消防设备。

## MQTT 安全验收

有可用本地 EMQX 环境时，验证：

1. 正确凭据可以连接。
2. 错误凭据被拒绝。
3. 自己的上报 Topic 可用。
4. 其他设备 Topic 被拒绝。
5. 跨租户 Topic 被拒绝。
6. 设备不能使用平台服务账号权限。
7. 凭据禁用或轮换符合实际生效策略。
8. 平台原有服务订阅仍能正常工作。

## 前端测试

覆盖：

- 切换接入类型。
- 条件表单。
- 必填和范围校验。
- 测试状态变化。
- 过期测试结果失效。
- 一次性凭据展示。
- MQTT/HTTP 接入说明。
- Modbus 表单与预览。
- TCP Listener 复用。
- 保存失败重试。
- 完成后跳转详情。

## 最终验收标准

至少证明：

1. 用户不必手工跨多个页面创建底层资源，也能完成设备接入配置。
2. 合法设备上报能够进入 RawMessage 和 StandardMessage 链路。
3. 错误凭据、越权 Topic 和跨租户请求确实被拒绝。
4. “配置成功、监听成功、收到报文、解析成功”在界面上可以区分。
5. 原有协议、采集和业务链路没有被无意破坏。

没有实际执行的测试，必须标记“未执行”，并说明缺少的具体环境。

不要把单元测试或 Mock 测试描述为真实硬件联调通过。

---

# 二十四、执行顺序与最终交付

请直接动手修改，不要停留在设计文档。

建议顺序：

1. 确认本地目录、AGENTS.md 和 Git 状态。
2. 核实已有实现和当前文档。
3. 给出简短计划，说明复用点、改动点和兼容策略。
4. 先建立必要的模型、服务和 Connector 边界。
5. 优先打通一条标准 HTTP 接入链路。
6. 完成设备凭据和 MQTT 的真实认证授权接入。
7. 完成 MQTT 标准上报。
8. 整合现有 Modbus 和 TCP/UDP。
9. 实现统一向导和设备详情入口。
10. 执行测试、修复回归。
11. 更新迁移、部署配置和文档。
12. 汇总实际交付结果。

可以根据依赖关系合理调整，但不要同时启动多套互相冲突的架构改造。

每完成一个主要模块，运行相关测试，再继续下一部分。

遇到环境限制时：

- 完成能够安全完成的代码和测试。
- 精确记录受阻环节。
- 不用假数据伪装真实成功。
- 不因为无法连接真实设备而停止其他实现。
- 不用占位按钮、TODO 或永远返回成功的接口充当已完成能力。

最终输出：

1. 实际完成的功能和未完成项。
2. 改造后的架构及职责边界。
3. 新增和修改的主要文件。
4. 数据模型、迁移和兼容说明。
5. 新增或复用的 API。
6. 用户如何通过向导接入设备。
7. 本地验证命令和模拟器使用方式。
8. 复用了哪些现有 Runtime、Parser 和业务能力。
9. 测试与构建命令、实际结果。
10. 未执行的集成验证及原因。
11. 已知风险和技术债务。
12. 后续 Edge Agent / Access Gateway 的演进边界。

最终目标：

```text
添加设备
    ↓
选择产品
    ↓
选择通信方式
    ↓
填写必要参数
    ↓
获取接入信息或执行连接测试
    ↓
查看真实报文和解析结果
    ↓
启用并确认首条数据
    ↓
进入设备详情
```

用户不应该为了接入一台设备，先理解并手工操作：

```text
ProtocolRelease
ProductProtocolBinding
DeviceAccessProfile
ParserType
Go Worker Runtime
```

底层仍可以使用这些对象，但应由编排服务和向导管理。

现在开始检查当前本地项目，给出简短实施计划，然后直接修改代码。