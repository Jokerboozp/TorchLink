# 设备接入改造实施进度

本次任务依据：`DEVICE_ACCESS_REFACTOR.md`（完整 1637 行，保持原文）。
日期：2026-09-09；环境：当前 macOS 本地工作区。根目录已核实为 `/Users/peterson/Developer/iot-platform`，适用根目录 AGENTS.md，未发现子目录规则。

## 范围与初始状态

- 保留用户原有 `.gitignore` 修改及未跟踪任务说明；不切分支、不克隆、不提交、不推送。
- 本文件此前不存在。其他文档中的历史测试记录不算本次验证。
- 用户已确认：P2 按任务说明保留扩展边界、记录待办，不实施整套新协议/进程。

## 已核实的现有能力

| 模块 | 当前代码依据及结论 |
| --- | --- |
| 标准 HTTP/MQTT | `onboarding/service.go`、`httpapi/onboarding.go`、`parser/standard.go`、`adapters/mqtt/standard.go` 已连接认证、Raw 和 Parser；需补报文冲突与格式边界 |
| 设备凭据 | 使用安全随机凭据、摘要存储、设备令牌、精确 JWT ACL；已有 EMQX 管理撤销及持久化重试，待本次实测 |
| 向导与原子保存 | 已有 `DeviceOnboarding.vue` 和 memory/PostgreSQL 原子 SaveOnboarding；重复提交目前报错，缺恢复语义 |
| Modbus/TCP/UDP | 已有真实只读预览、共享监听、协议 V2 运行时；需补并发上限与执行节点归属检查 |
| P1 | 已有连接详情、事件/告警、命令回执、物模型和 Edge 节点登记；不能据此声称 Edge 运行时已实现 |

## 实施顺序

1. P0：补向导幂等恢复、标准消息冲突与解析边界、Connector 能力及受限测试；逐模块测试。
2. P0：核实 MQTT 实际认证授权与失败窗口、监听/采集链路；补向导首条数据确认和详情入口。
3. P1：修复执行节点边界，核实详情/命令/物模型；回归及文档同步。
4. P2：完整 Edge Agent、独立 Access Gateway、RTU/RS485、OPC UA/BACnet/SNMP、ONVIF/GB28181、分布式调度、数字孪生/影子、插件市场维持待办。

## 关键设计决定

- 继续以 DeviceAccessProfile 为通信配置唯一事实来源，复用既有 Runtime 与原始归档/业务链路。
- 凭据明文仅首次创建或轮换返回；重试恢复不得重新生成或再次读取原 Secret。
- 配置保存、监听/采集成功、收到原文、成功解析分别报告。
- 视频按当前代码保留元数据/设备关联，不恢复已移除的视频流功能。

## 实际测试与阻塞

- 基线、模块测试及最终验证结果见下表，未配置集成环境的跳过项单独执行验证。
- 厂商真机、生产部署未执行；本次只允许测试环境或模拟设备，不操作真实消防设备。

## 模块更新：P0 一致性、预览与交互

已实施：

- 用租户 + Device ID 作为向导幂等键，保存请求摘要；相同请求重试返回已保存资源，不重发 Secret，不重复创建产品/凭据；修改请求返回冲突。摘要保存在既有设备 JSONB 标签，设备编辑保留系统接入标签。
- 标准消息 ID 重复但正文变化返回 `409 MESSAGE_CONFLICT`，同进程并发按消息锁串行化，保留原文。字节级正文必须相同，跨传输重试仍使用同一 ID。
- 标准 Parser 接受 v1 顶层 event/online/commandId/success，也兼容旧 data 格式；限制 64 KiB、16 层嵌套、重复 JSON 键、版本和时间范围。设备时间超过接收时间 5 分钟拒收，历史补传保留设备时间。
- Connector 增加类型/能力查询、unsupported 明确结果；测试并发最多 8，Modbus 读取最多 32，取消会中断等待中的 Socket。
- MQTT 使用固定 8 worker、128 项队列，去除逐消息 goroutine；过载日志明确拒收，PUBACK 仍不代表持久化成功。断线/进程崩溃前未归档消息仍依赖设备查询接收结果并重试。
- 向导完成后显示独立的保存、运行、Raw 接收与解析状态，最多轮询 2 分钟，可手动继续；一次性 Secret 丢失时指导轮换；新增进入详情操作、对外地址配置、Modbus 重试/周期/字节序/字序/单位表单，过期预览结果不覆盖新配置。
- 原始索引新增 `parse_attempted_at` / `parse_error`（启动时幂等 ALTER、默认 0/空串）；memory/PostgreSQL 同步实现，解析失败保留证据且不产生 StandardMessage。

实际测试（本次）：

| 命令/环境 | 结果 |
| --- | --- |
| 相关 7 个后端包基线测试 | 通过；未设置集成变量的集成用例明确跳过 |
| `go test ./internal/connector ./internal/onboarding ./internal/protocolruntime -count=1` | 通过，含新增幂等、并发、取消、unsupported |
| `go test ./internal/httpapi ./internal/core ./internal/onboarding ./internal/adapters/postgres ./internal/adapters/memory -count=1` | 通过；最终全量回归也已通过 |
| `npm test`、`npm run build` | 最终 56 项通过、构建通过；有既存大 chunk 和插件耗时提示 |
| 真实 PostgreSQL 临时 schema：`TestDeviceOperationsMigrationAndAtomicity` | 新增解析错误列迁移、重复迁移、跨租户更新拒绝、向导重试/冲突通过；只清理自身 schema |
| 现有真实 EMQX：`TestStandardMQTTLiveBroker` | 认证上报、Raw/Standard、命令回执、重连与告警幂等通过；管理撤销因未配置测试 API Key 跳过 |
| Chrome + 现有真实 EMQX WebSocket：`TestOnboardingBrowser` | 通过（52.49 秒），含向导等待上报与详情跳转、移动端、HTTP 错误凭据、MQTT 认证/重连/去重 |

## 模块更新：P1 边界与命令

- 中心 Modbus/Listener 不执行 `edgeNodeId` 非空的任务；已有监听重新分配后停止，详情显示 UNSUPPORTED。CollectorID 继续为采集来源标记，不能当成已实现分布式调度。
- 节点库存登记仍可使用；向导禁用远端执行选择，不再把仅登记节点当成采集已运行。
- MQTT/协议命令增加人工确认；API 必须携带 `confirmed:true`，租户/角色/协议和原命令幂等继续有效。新增无确认拒绝回归。旧 API 用例因缺少确认字段失败，已更新其明确模拟授权并复测通过。30 秒无回执的 SENT/DISPATCHING 查询投影为 UNKNOWN，不假设执行失败，不自动重发，迟到的真实回执仍可更新终态。
- EMQX 配置新增 username claim 绑定和显式到期断连。当前业务 Broker 未改配置；独立临时 EMQX 5.8.8 的严格认证/ACL/撤销实测通过，见下表。

## P0 / P1 交付核对

| 范围 | 本轮完成与复用 |
| --- | --- |
| P0 Connector 与编排 | 类型/能力 API、unsupported、受限测试；复用 Onboarding Service 和原子 Bundle 保存，补并发幂等恢复 |
| P0 HTTP / MQTT | 标准 v1 及旧格式、设备真实认证、精确主题 ACL、冲突/限流/过载错误；不允许标准凭据使用旧原文入口自选协议 |
| P0 Modbus TCP | 原有只读 Runtime、点表编译和 Parser；补周期/字节序等输入、版本摘要包含周期、取消与并发边界 |
| P0 TCP / UDP | 复用 Listener、Go V2 ingress/decode、半帧/粘包/应答/热切换/回放；向导复用共享监听与原产品绑定 |
| P0 向导与证据 | 参数草稿、预览失效、一次性凭据、保存失败重试、首条数据轮询、详情及原文导航；样例与网络读取明确区分 |
| P0 迁移与兼容 | 两个原始索引列的幂等迁移；旧设备无幂等摘要仍冲突；旧接入指南保留嵌套结构，更新标准上报样例及对外 URL |
| P1 接入详情与状态 | 复用连接/事件/告警历史，补 Raw 接收与解析失败证据，历史补传不回退当前连接状态 |
| P1 命令与物模型 | 复用产品属性/事件/命令基础模型、权限和命令回执；增加人工确认及未知结果显示；不增加通用控制引擎 |
| P1 Edge / Collector | 库存、来源字段与中心执行边界明确；远端 Profile 不在中心启动，不返回虚假已运行 |

职责保持为：页面 / API → Connector 与 Onboarding 编排 → DeviceAccessProfile / ProtocolRelease / 产品模型 → 既有 Runtime → Raw 归档与幂等索引 → 内部队列 → Parser → StandardMessage → 原有存储、规则、告警。没有新建第二套连接配置库或接入进程。

主要文件：`internal/connector`、`internal/onboarding`（能力与编排）；`internal/httpapi/onboarding.go`、`connector_status.go`、`server.go`（接口）；`internal/core/engine.go`、`internal/parser/standard.go`（数据与状态）；`internal/protocolruntime`、`internal/adapters/mqtt`（运行时）；memory/PostgreSQL 仓储及 `schema.sql`（迁移）；`DeviceOnboarding.vue`、`DeviceConnection.vue`、`DevicesView.vue`、`RawView.vue`、`IntegrationView.vue`（页面）；Compose、配置示例及离线打包模板（地址与认证配置）。

新增 API 为 `GET /api/v1/connectors/types`；复用并增强 onboarding test/create、device connection/connection-guide、standard ingest、device-mqtt/token、credentials、commands 和 Raw 查询接口。完整接口、向导步骤、模拟器与验证命令见 `UNIFIED_DEVICE_ONBOARDING.md`，部署说明见 `DEPLOYMENT.md` / `OFFLINE_DEPLOYMENT.md`。

## 最终验证记录（2026-09-09）

| 命令 / 环境 | 实际结果 |
| --- | --- |
| 根目录 `go test ./...` | 通过；未提供环境变量的 opt-in 集成测试跳过，不将跳过算作集成通过 |
| `go test -race ./internal/onboarding ./internal/protocolruntime ./internal/adapters/mqtt -count=1` | 通过，并发保存、取消、共享运行时与 MQTT 包无竞态报告 |
| 独立 module `protocol-packages/gb26875-dahua`：`go test ./...` | 通过；根 module 不代替此项 |
| `iot_front`：`npm test`、`npm run build` | 56 项通过，构建退出码 0；保留既有构建体积/插件耗时提示 |
| PostgreSQL：`go test ./internal/adapters/postgres -run TestDeviceOperationsMigrationAndAtomicity -count=1 -v` | 实际数据库临时 schema 迁移、重复执行、租户隔离及原子保存通过，schema 已清理 |
| 独立 EMQX 5.8.8：`TestStandardMQTTLiveBroker`（严格身份 + 管理 API） | 全部通过：合法上报、错误 JWT/冒用 username 拒绝、其他设备/租户的发布订阅拒绝、平台订阅可用、轮换/禁用断连及旧 JWT 拒绝、命令回执和幂等 |
| `go test ./internal/httpapi -run TestGoProtocolListenerSourceHotSwitch -count=1 -v` | 最终通过，实际本地 TCP/UDP Socket、Go 协议包构建、共享 Listener 复用、热切换与历史回放；临时业务仓库，不是真机 |
| `go test ./internal/httpapi -run TestModbusOnboardingRuntimeChain -count=1 -v` | 通过（2.03 秒）：认证管理 API 测试/创建、新产品与设备原子保存、原 Runtime 定时读取本地 Modbus Socket、Raw → Standard → 详情；关闭模拟器后 ERROR 与已解析证据并存 |
| Chromium：`TestOnboardingBrowser` | 修改后第一次回归因 Chrome 清理挂起超时，已补 SIGKILL 兜底；新增保存失败模拟曾因响应字段写成 error 而断言失败，改为真实 API 的 detail 契约后通过（7.63 秒，退出码 0）。包含预览失效、失败重试、一次性凭据、详情跳转、窄屏、HTTP 错误凭据及真实 MQTT WebSocket 重连/去重。随后加入 Modbus 实际 Socket 读取后再次通过（8.41 秒，退出码 0），验证条件表单、必填禁用、读取值 42、逗号/引号单位字段 |
| OrbStack Linux ARM VM：`bash scripts/tests/deployment-smoke.sh /usr/local/lib/docker/cli-plugins/docker-compose` | 通过；使用真实 Compose 渲染，Docker/HTTP 操作为模拟，不是部署验收 |
| `git diff --check` | 最终文档更新后通过 |

隔离 EMQX 的测试二进制从当前 Go 源码交叉编译为 linux/arm64，在 OrbStack develop VM 内运行，使用随机容器、配置、租户与管理 API Key。Mac 无法访问临时 VM 映射端口的初次尝试未计通过，改为 VM 内测试后通过。EMQX 对错误 JWT 返回 CONNACK 4（另一种有效拒绝码）；测试已接受 4/5，仍严格要求连接失败。临时容器与管理凭据已清理，未改变业务 Broker 或主机防火墙。

## 上一阶段待办与边界（后续范围及实现见下节）

- 按用户确认，P2 八类能力均保留待办：完整 Edge Agent、独立 Access Gateway、RTU/RS485、OPC UA/BACnet/SNMP、ONVIF/GB28181、分布式调度、完整孪生/影子、插件市场。现有字段/接口不代表这些能力已实现。
- 未执行厂商真实消防设备、现场防火墙/证书/TLS、长时断网与固件离线缓存验证：没有本次授权的真机与现场环境。本地 Socket 模拟器、真实 Broker 与浏览器结果分别表述。
- 未执行业务服务部署、现有业务库迁移与业务 EMQX 动态认证配置更新：本次在工作区实施，真实中间件测试均隔离。实际升级后仍需核对认证链生效配置与设备可达地址。
- 未执行 PowerShell 部署冒烟：macOS 与当前 VM 均未安装 PowerShell；模板变更经源码检查，Bash/Compose 冒烟不能替代 PowerShell 执行结果。
- MQTT 接收为受限内存队列，满载会明确拒收；PUBACK 不是归档确认。可靠接收须查 Raw 后重试同一 ID；进程崩溃前尚未归档数据不保证保存。
- 当前只支持一个接入执行实例；本次按消息锁实现单进程冲突保护，分布式原子归档/调度、跨副本会话与共享限流不在本次实现范围。独立 Gateway 后续须复用现有 Raw 入口并解决所有权、持久化重试与路由，不能复制另一套业务链路。
- 凭据未配置 EMQX 管理 API 时按短期 JWT 到期策略生效，管理调用失败必须保留 PENDING 及重试状态；不能把数据库凭据轮换当作已断开所有 Broker 会话。
- 仓库已有 `.gitignore` 修改完整保留；任务说明未删改；未切换分支、提交或推送。

## 继续实施（2026-09-09，用户扩大 P2 范围）

- 用户现已授权提交并推送全部改动，且明确将 P2 扩大为任务文档所列全部八类能力；此前“P2 只保留边界”是上一阶段范围，不再作为后续实施限制。原任务说明不删改。
- 已将上一阶段全部 56 个文件提交为 `1d76571`，中文标题“完善设备统一接入链路与认证诊断”。向 `origin/main` 推送失败：HTTPS 无可用登录凭据；本机没有 gh，SSH 缺少已信任主机记录。已请用户完成本机 GitHub 登录，未绕过主机校验，未宣称推送成功。
- P0/P1 新发现并修复：Modbus 结束后的整配置回写可能覆盖操作员更新。现改为配置快照匹配后仅更新运行观察字段；禁用、换地址、分配 Edge 后的旧成功/失败结果不会恢复旧配置。新增共享仓储契约用例，memory 与真实 PostgreSQL 临时 schema 均通过。
- P2 实施顺序：独立 Gateway 与执行所有权 → Edge 配置同步/认证/持久化补传/诊断 → RTU、OPC UA、BACnet、SNMP、视频协议 → 孪生/影子 → 插件目录与受控远程发布。每项单独验收；开始实施不表示完整能力已完成。

### 后续实测与实现

- ONVIF 基础信息：新增现场节点只读任务、operator/租户检查和摄像头表单读取入口，保持元数据管理范围。`TestONVIFAuthenticatedMetadata`（TLS Socket 模拟摄像头）实际验证 WSSE、HTTP Digest、错误密码/无凭据/不可信证书拒绝以及 HTTP 200 SOAP Fault 失败，`-race` 通过；`TestEdgeONVIFMetadataWithAuthentication` 的管理 API→节点认证→TLS 摄像头认证→元数据预览通过，预览不创建设备。Digest 畸形挑战 fuzz 3 秒执行 81,304 次通过。首次 API 编译因误用 ProtocolRelease 字段失败，已修正并复测通过。前端 56 项测试及构建通过；新摄像头入口浏览器验收尚未执行，真机未执行。
- Gateway 去除 AI/知识库依赖后再次运行两个真实进程、临时 PostgreSQL/Redpanda 恢复测试，30.73 秒通过。

- 本批最终验证：根目录 `go test ./...` 通过（可选真实中间件测试仍需显式环境，跳过不算通过）；`npm test` 56 项及 `npm run build` 通过；Windows amd64 Edge Agent 交叉编译通过，未在 Windows 上运行。实际认证服务器补测 SNMP v2c 正确/错误 community 后通过（6.58 秒，`-race`）；本批 `git diff --check` 通过。Gateway 启动进一步去除 AI Provider 与知识库依赖。

- BACnet/IP：只读 ReadProperty、地址/调用号/长度/对象/属性/标量校验、完整 HEX 证据和本地网络访问配置。原生协议无密码认证，明确记录 `none-native-bacnet-ip`；分段、路由和 BACnet/SC 不支持，不伪造成功。固定协议帧成功/畸形测试通过，`FuzzBACnetResponse` 3 秒执行 23,793 次通过；BACpypes3 0.0.106 实际服务器读取通过，三协议 `TestEdgeAuthenticatedFieldOnboarding` 的 BACNET 子用例通过（6.17 秒），整体 20.62 秒，包含节点预览/启用/归档解析。厂商 BACnet 真机未执行。

- 独立 Gateway 实际进程验收通过：`TestSplitProcessesPostgresKafkaRecovery`（30.77 秒），OrbStack develop / Linux ARM64、临时 PostgreSQL schema、一次性 Redpanda v25.2.11。真实登录认证、代理接入、独立 Gateway 归档/API 消费、API 停机期间接收并在重启后解析、Gateway 停机返回 503 均通过；临时 Broker 和 schema 已清理，未使用业务 Kafka 消费组。
- OPC UA / SNMP：新增现场凭据引用、版本读取点、实际服务响应归档和质量校验；向导、节点任务、周期调度均已接通。`TestAuthenticatedOPCUAAndSNMP` 使用 asyncua 1.1.8 / pysnmp 7.1.21 实际服务器，OPC UA 加密用户名认证、可信证书检查、SNMP v3 SHA256/AES、错误密码拒绝通过（2.38 秒）；`TestEdgeAuthenticatedFieldOnboarding` 通过（14.34 秒），两协议分别走现场预览→启用→定时读取→Raw/Standard，并检查缺失本地凭据不能通过。均执行 `-race`。实际厂商设备、SNMP v2c 本轮尚未验收。
- 前端增加 RTU、OPC UA、SNMP 参数和现场节点选择。56 项前端测试及构建通过；修复 TCP 页面节点管理入口被误隐藏后，真实 Chromium + Broker 原有接入回归通过（7.57 秒）。新增现场协议表单的完整浏览器操作尚未验收。

- 原文多实例预留：16 路并发按租户/Raw ID/内容摘要固定首次协议快照，冲突拒绝；Memory、真实 PostgreSQL 合约及 Core/Memory/MQTT 竞态测试通过。
- MQTT 多实例：`TestMQTTSharedGatewaySubscription` 使用真实 Broker，两接入实例共享订阅时仅一个处理单次上报，通过（0.32 秒）。
- Edge 拒收：永久失败移入磁盘隔离区并保留容量计数，不阻挡其他项；`--retry-rejected` 显式恢复。队列重启、容量、独占和拒收测试通过。
- 现场诊断：数据库只读任务队列、原子领取、截止时间、归属校验；原始结果在平台解析后才签发接入凭证，不写入业务链路。Memory 和真实 PostgreSQL 16 并发领取只有一个成功，错误令牌和重复结果拒绝，测试通过。
- RTU：新增串口白名单、8 位 N/E/O 校验、读功能码 01–04、CRC/站号/功能码/长度校验、版本点表解析及现场节点接入表单。`TestRTUReadAndParseWire` 成功/CRC/站号/功能码/异常/长度/超时与竞态测试通过。
- `go test -race ./internal/httpapi -run '^TestEdgeRTUOnboardingWithActualSerialRead$' -count=1 -v`：通过（6.19 秒），真实 OS 伪终端与 HTTP，非法串口不能取得凭证，读取预览→保存→定时采集→Raw→Standard。`TestModbusRTUOSSerial` 通过。物理 RS485、Windows 串口及手动 RTS 方向控制未执行/未实现。
- 部署补齐：离线包包含可选 Gateway 覆盖层及说明，Bash/PowerShell 模板补充拆分配置。`bash scripts/tests/deployment-smoke.sh /usr/local/bin/docker-compose` 通过（Compose 真实解析，Docker/HTTP 操作模拟）；PowerShell 本轮未执行。
- 当前仍待完成：完整 Edge Worker/升级能力、BACnet 复杂对象/分段、ONVIF 发现/事件与 GB28181、分布式调度完整验收、孪生/影子、插件市场与远程发布。基础协议读取通过不代表整类能力全部完成。
- Gateway 首个实现：提取共享启动装配到 `internal/platformapp`，新增 `cmd/iot-access-gateway`；combined 保持旧模式，api 不启动通信 Runtime/外部 MQTT 订阅，gateway 不消费 Raw 业务队列。拆分模式校验共享 PostgreSQL/Kafka，API 将接入测试、上报、连接详情和会话命令转发给 Gateway 并保留原身份；Gateway 不开放登录和一般管理路由。该首个版本之后已补执行租约、路由与进程实测，见本节记录。
- 已执行 `go test ./internal/config ./internal/httpapi ./internal/platformapp ./cmd/iot-platform ./cmd/iot-access-gateway`，通过；新增 `TestSplitGatewayHTTPFlow` 用独立 Engine + 共享测试仓库/队列验证认证、创建、转发上报、API 解析及 Gateway 不可用返回 503。该用例是进程职责集成模拟，不是两个生产进程/Kafka 部署验收。

- Gateway/调度增量：新增 `execution_lease` 启动迁移；并发唯一拥有者、续租、过期接管、旧 token 不可释放新租约通过 memory/真实 PostgreSQL 共享契约测试。`TestCoordinatorLossCancelsOldExecution` 与 `TestExecutionRouteUsesTenantLeaseAndPreservesAuth` 的竞态检查通过，覆盖所有权丢失取消、配置禁用和租户/转发上限。
- Edge 第一条链路：新增 `cmd/iot-edge-agent`，节点凭据仅保存摘要，认证后的配置同步、24 小时离线缓存上限、心跳、Modbus TCP 采集、64 MiB/10,000 项磁盘队列、受限退避补传。`TestQueueRestartRetryAndCapacity` 与 `TestEdgeAgentDurableModbusChain` 通过竞态检查；后者使用本地真实 Socket/HTTP 和模拟 503，重启后同 ID 数据进入 Raw/Standard。首个节点版本之后已补 RTU、OPC UA 和 SNMP；Go Worker 与升级等仍待实施，未标记“完整 Edge Agent”完成。
- 新增 `compose.access.yaml` 可选拆分层、节点管理中的凭据/心跳/分配入口，启动方式与限制见 `EDGE_AND_GATEWAY.md`。本次只渲染 Compose，不启动/升级原有业务容器。
