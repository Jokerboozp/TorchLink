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

- 2026-09-10 增量验收：`TestDistributedCollectionProcessFailover` 在实际 PostgreSQL 临时 schema 和两个独立采集子进程上通过（14.34 秒）。当前所有者运行期间另一进程不重复读取；强制终止后等待真实租约到期接管，旧令牌无法释放后继者。测试只清理自身 schema/子进程，不操作业务设备。初次用例使用了错误的 ModbusReadBlock 字段，修正后实测通过。
- ONVIF 新页面浏览器验收通过：`TestEdgeONVIFMetadataWithAuthentication/browser`（3.51 秒，整个用例 5.64 秒），真实 Chrome、现场 Agent、TLS + Digest + WSSE 模拟摄像头，覆盖节点选择、缺失凭据拒绝、读取填表、窄屏及显式保存。首次浏览器脚本菜单名写错而超时，修正为当前“摄像头映射”后通过；厂商真机仍未执行。

- Git：Edge Worker 中文提交 `a61d7e0` 已成功推送至 `origin/main`。用户要求提交全部内容，本次也保留并纳入工作区新增的 `项目介绍.md`，未改写该文件。
- 设备影子基础链路：新增持久化 desired/reported/delta、独立期望版本、逐属性时间/消息顺序、可写物模型校验、人工确认与变更历史、设备凭据读取、详情页。真实 PostgreSQL 临时 schema（`TestDeviceOperationsMigrationAndAtomicity`，1.59 秒）及 Memory 16 并发条件更新/属性合并通过；认证 HTTP→标准上报→解析→影子差异收敛通过（0.02 秒，`-race`）。首次测试缺少 messageKind，已补为 property 并通过，未跳过认证。
- 影子超限错误可见且不阻断既有告警规则，相关竞态回归通过。前端 56 项测试/构建通过，真实 Chrome + Broker `TestOnboardingBrowser` 8.92 秒通过，新增影子编辑/人工确认/版本/差异检查。说明见 `DEVICE_SHADOW.md`；孪生拓扑、命名影子、MQTT 原生影子 RPC 等仍待实现，未称完整体系完成。
- 影子模块最终 `go test ./...` 通过；可选真实中间件测试未提供参数时依旧跳过，数据库与浏览器单独实测结果以上述记录为准。

- Git 更新：本批 95 个文件已以中文提交 `597d6ae`（“实现独立接入网关、边缘采集及现场协议认证链路”），`git push origin main` 成功，输出 `1d76571..597d6ae main -> main`。此前认证阻塞已解除。
- Edge Go Worker：节点凭据与当前分配版本检查、制品下载双端 SHA-256、平台匹配、本地显式执行/监听白名单、TCP/UDP Listener、配置更新保留会话、设备停用同步。`TestEdgeWorkerDownloadTCPUDPAndVersionSwitch` 使用实际 Go 子进程/HTTP/TCP/UDP，认证拒绝、未分配版本拒绝、损坏制品拒绝、半帧/粘包应答、Raw/Standard 及同连接版本切换通过（首次 6.48 秒，`-race`）。源码构建/发布沿用既有链路；物理网关、跨架构制品构建、远端命令和节点程序升级尚未验收/实施。
- Worker 后续验证：Edge/向导相关 `-race` 回归通过（HTTP API 包 17.723 秒）；新增节点本地执行开关、监听地址、平台、哈希与损坏下载拒绝测试通过。`go test ./...`、前端 56 项测试和构建通过；未配置的可选中间件测试保持明确跳过。

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

### GB28181 元数据完整链路（2026-09-10）

- 新增独立 Go module `protocol-packages/gb28181-metadata`：真实 TCP/UDP SIP Digest 注册、nonce 消费与重传缓存、保活、独立关联的 Catalog/DeviceInfo 查询、分批完整目录、超时保留旧目录、受限 worker/Socket/报文容量；密码仅保留现场私有配置。
- 专用平台节点认证、HTTPS 目录上报、本地原子快照与断网上报失败保留、凭据撤销停止。摄像头映射新增节点目录选择与显式导入；仅最近有效目录允许导入，目录分组不当摄像头，跨租户/低权限拒绝。Memory/PostgreSQL 原子插入防止并发覆盖，重复导入保留操作员编辑、来源和单设备关联。
- 实测：独立 module `go test -race ./... -count=1` 通过（3.446 秒），实际 TCP/UDP、独立 Digest 客户端计算、错误密码、nonce 重放、重复请求、来源/CSeq/传输检查、分批目录/冲突/超时；XML fuzz 188,753 次、SIP 分帧 fuzz 219,147 次通过。Windows amd64 交叉构建通过，未在 Windows 运行。
- `TestGB28181AuthenticatedCatalogImport` 实际 SIP→平台节点认证→目录→权限→导入及编辑保留通过（4.10 秒，`-race`）；其中真实 Chrome 子用例 2.56 秒通过，覆盖节点选择、目录查看、窄屏和重复导入。第一次测试假定查询顺序，第二次误用摄像头 POST 的预期状态码，均已修正并重新实测通过。
- 真实 PostgreSQL 临时 schema：`TestDeviceOperationsMigrationAndAtomicity` 1.47 秒通过，新增 16 并发目录导入、仅一个创建、既有名称和设备关联保留、租户隔离。前端 56 项测试与构建通过；启动说明见 module `README.md`。
- 实现范围为注册与元数据子集，不宣称 GB/T 28181 全项符合性；厂商真机、现场网络、2022 完整安全扩展未执行/未实现。依项目摄像头元数据约束，不恢复视频流、云台或设备配置。ONVIF 发现/事件等仍待实施。

### 可信协议目录与受控远程安装（2026-09-10）

- GB28181 中文提交 `8218780` 已成功推送；其后根目录 `go test ./...` 通过。期间用户另行提交的品牌展示 `ddf583b` 保留原样。
- 新增 HTTPS + Ed25519 签名目录、可信公钥/可选 CA 本机策略、1000 项查询搜索、管理员人工确认、下载大小/SHA-256 校验、同源及重定向限制；安装复用原有源码编译和实际样例管线，仅创建 VALIDATED 版本，再用既有发布/产品绑定/回滚。来源摘要和签名 Key ID 随不可变制品保存，已有版本不可覆盖。
- 新增独立签名命令 `cmd/iot-protocol-catalog`，私钥只在发布者本机，公钥用于消费方信任配置。浏览器不修改信任根、不提交任意下载 URL。运行说明见 `PROTOCOL_CATALOG.md`；Compose 和离线环境模板补配置入口。
- `go test -race ./internal/protocolcatalog -count=1` 通过（1.599 秒）：实际 TLS、错误证书/签名/未知公钥/过期/跨源/重定向/损坏下载拒绝。签名 CLI 实际构建、生成私钥、签名原始字节与防覆盖通过（0.83 秒）。
- `TestAuthenticatedCatalogSourceInstall` 认证下载→实际编译/样例→校验版本→显式发布、角色/人工确认/目录更新冲突/跨租户隔离通过。真实 Chrome 子用例通过（6.68 秒，总计 6.82 秒），覆盖目录、执行代码确认、真实安装与重复禁用。新增签名正确但样例失败的新版本拒绝、原发布版本保留也通过（5.11 秒，`-race`）。
- 根目录 `go test ./...`、前端 56 项测试及构建通过；Bash 部署冒烟通过（真实 Compose 渲染，部署操作模拟）。PowerShell 未安装，仍未执行。未部署公共目录服务器、第三方商业市场、跨组织审核或异构平台构建服务；私有可信分发是当前实现，其他大型市场能力继续待办。

### 当前推进重点

P0/P1 已有链路和新增一致性修复均已实测；发现缺陷继续修复。P2 八类已分别推进 Gateway、Edge、现场只读协议、ONVIF/GB28181 元数据、租约调度、影子与可信协议分发。仍不能将这些基础实现写作“全部 P2 完成”：接下来补 Edge 远端命令/程序升级、孪生关系与状态体系，以及协议、调度、市场的剩余边界。物理设备、生产和多主机现场验收仍未执行。

### Edge 远端协议命令及 P1 遗漏修复（2026-09-10）

- 可信协议目录中文提交 `2820bf5` 已成功推送至 `origin/main`。
- 修复 P1 遗漏：设备接入页的直接协议命令按钮没有提交人工确认字段，现补确认对话框；现场命令重试复用 requestId，查询结果和开始新命令分开，界面不把排队写成已发送。
- Edge 命令复用 `device_command` 及历史，新增执行元数据与 PostgreSQL 待领索引；原子容量、唯一领取、同请求恢复/异请求冲突、30 秒截止、一天内迟到结果、公开响应移除领取令牌。本机默认关闭，须显式允许 Go Worker 和协议命令，服务端还检查角色/确认/设备与实例归属/发布版本/近期心跳。
- 节点执行前重新认证同步配置，核对 Profile 摘要及协议版本，调用既有 Listener 的 encode→真实 Socket→相关 ingress 应答。落盘 UNKNOWN 前不执行，重启只补传结果，不重放命令；发送和应答状态分开，ACKNOWLEDGED 不代表设备业务执行成功。修复命令同步短期上下文可能影响常驻 Runtime 生命周期的问题，并串行配置刷新。
- Memory 16 并发创建/领取、错误归属拒绝、结果幂等及限额通过；真实 PostgreSQL `TestDeviceOperationsMigrationAndAtomicity`（1.49 秒）包含相同合约与迁移，实际通过。节点 journal 的 503 保留/重启补传/不领取新命令通过；初次用例遗漏必需网络白名单，补配置后通过，没有放宽真实校验。
- `TestEdgeWorkerDownloadTCPUDPAndVersionSwitch`（`-race`）真实 TCP 下发字节、未应答前 DISPATCHING、实际关联应答、原文 ID、重复请求不重发、同连接版本切换通过；真实 Chrome 命令子用例 3.10 秒、整体 11.21 秒通过，覆盖人工确认、排队、查询到真实应答。Edge/Memory/HTTP 相关竞态回归通过。
- 前端 56 项测试与构建通过；一次在仓库根目录误跑 npm 构建失败，已在 `iot_front` 正确执行并通过。厂商真机控制未执行；本轮仅测试本机 Socket 模拟设备，不提供任意 shell 或 Modbus 写功能。
- 本模块根目录 `go test ./...` 通过；公开命令接口移除令牌、MQTT/Edge 命令 ID 不混用的相关回归通过。Journal 复用现有原子落盘方法（Unix 同步目录），未引入重复文件写入实现。

### 设备孪生关系与真实状态投影（2026-09-10）

- Edge 命令中文提交 `2e633b5` 已成功推送。
- 新增设备包含/监测/依赖关系、稳定关系 ID、全租户版本 CAS、事务归属检查、包含单父节点与包含/依赖无环约束、审计；新增 `device_twin_topology` 启动迁移，复用既有设备库存、状态、产品物模型、影子和已解析属性历史，未复制第二套状态库。
- 详情页增加交互拓扑、邻居导航、添加/解除关系、当前焦点影子和最近属性上报。单次查询深度 0–3、最多 200 节点，图最多绘制 16 节点；分页属性历史沿用原始业务消息，不伪造观测值。节点投影不含凭据和内部标签。
- Memory 16 并发版本冲突、重复关系、环、多父节点、跨租户和移除通过；真实 PostgreSQL `TestDeviceOperationsMigrationAndAtomicity` 通过（1.66 秒），包含相同合约和实际迁移。
- `TestDeviceShadowAuthenticatedReconciliation` 在实际设备认证上报/解析/影子收敛后读取拓扑和属性历史通过；真实 Chrome 子用例 4.24 秒、整体 4.27 秒（`-race`）通过，覆盖邻居点击、环拒绝、创建/解除、版本增长和窄屏。无凭据绕过，浏览器读取的是实际上报得到的 42。
- 根目录 `go test ./...`、前端 56 项测试和构建通过。设备关系不触发控制，不修改既有 GatewayID/摄像头归属；三维场景、物理仿真、命名影子和 MQTT 原生影子 RPC 尚未实现，实体设备与生产环境未验收。说明见 `DEVICE_TWINS.md`。

### MQTT 原生影子读取（2026-09-10）

- 孪生拓扑中文提交 `2483c7c` 已成功推送。
- 标准设备 JWT 新增仅本设备的 shadow-get 发布与 shadow 应答订阅 ACL，令牌接口返回对应主题；查询按请求 ID 关联，检查当前设备/产品/凭据状态，不接受身份覆盖或 desired 写入，不进入 Raw/Parser。沿用 Gateway 共享订阅、受限队列及平台发布连接，状态来自现有影子仓储。
- 现有真实 Broker 首次联调通过（0.46 秒）；该次用户名 claim 绑定未执行、管理撤销子用例因缺少配置跳过。随后从当前源码交叉编译 Linux ARM 测试二进制，在 OrbStack 独立 EMQX 5.8.8 严格配置上完整通过（0.46 秒）：原生影子、错误 JWT/冒用用户名、跨设备/跨租户影子请求及订阅拒绝、凭据轮换/禁用断连与旧 JWT 拒绝均实际执行，隔离容器与临时配置已清理。
- Onboarding/MQTT 包 `-race` 测试及根目录 `go test ./...` 通过；单测初次将 JSON 数值与 Go int 比较而失败，修正为 float64 断言后通过，没有改弱业务校验。默认影子现在具备 HTTP 与 MQTT 读取；命名影子仍待实现。

### Edge 程序签名升级与失败回退（2026-09-10）

- MQTT 原生影子提交 `b11f5a1` 已成功推送。新增独立 `iot-edge-launcher`：本地信任目录、公钥和初始程序授权，HTTPS/Ed25519/平台/大小/SHA-256 校验，真实 Agent 子进程切换、原子程序日志、上一版本回退、重启恢复、节点凭据拒绝后停止。协议目录区分源码与程序条目，不能把二进制送进源码安装入口。
- 新增目标版本 CAS、实际程序状态、启动迁移、管理员确认与租户/节点认证接口。Edge 管理增加直接入口及程序升级弹窗，展示实际/目标版本、请求号、上报时间和失败原因。Dockerfile 仅加入启动器构建产物，未更改默认启动入口，未部署现有服务。
- 就绪依据为本次进程实际配置同步和认证心跳，加节点身份、版本、PID、随机 nonce；旧配置缓存不能伪造就绪。切换前重新核对当前目标；失败请求不自动重复执行。已有补传和命令日志不迁移、不清空。程序缓存只清理启动器自己的哈希命名文件，清理失败阻止继续新增制品。
- `TestEdgeProgramAuthenticatedProcessUpgradeAndRollback` 实际构建/启动 v1、v2，访问真实 TLS 签名目录与 HTTP 节点认证；覆盖远端制品篡改拒绝、配置接口不可用时缓存不能通过就绪、升级、错误程序版本回退、控制接口短时失败、启动器重启、本地制品损坏回到上一版、节点禁用退出、磁盘队列保留。最后一次 `-race` 通过（8.07 秒），包含真实 Chrome 子用例（2.79 秒）：回退展示→人工确认 v2→实际进程升级→状态刷新→窄屏。首次运行浏览器未配置而跳过，之后上述子用例已实际执行。
- Memory 16 路目标/状态并发共享合约通过；真实 PostgreSQL 临时 schema 的迁移、并发目标 CAS、状态不覆盖目标及租户隔离通过（1.67 秒）。`go test -race ./internal/edgeupgrade ./internal/adapters/memory ./internal/protocolcatalog ./internal/edgeagent`、根目录 `go test ./...`、前端 56 项测试及构建通过。Windows amd64 的启动器和 Agent 交叉构建通过；Windows 实际运行、物理现场与生产升级未执行。
- 说明见 `EDGE_PROGRAM_UPGRADES.md`。此模块没有阻塞；启动器首次启动/重启需要认证服务可达，就绪不代表全部现场设备采集成功。仍待推进命名影子、协议自动跨架构构建和 Edge 自动登记、ONVIF 发现/事件、复杂 BACnet/厂商兼容、分布式多主机验收和大型市场剩余能力；分批升级及跨版本数据格式迁移未实现，不能据此宣称全部 P2 已完成。

### 命名影子与兼容迁移（2026-09-10）

- 程序升级提交 `6953d57` 已成功推送。命名影子复用现有状态协调逻辑，每台设备 16 个命名影子加默认影子，分别保存 desired/reported/delta、版本与变更历史；不复制物理设备身份或触发隐式控制。默认接口兼容，新增名称查询、列表、详情切换和新名称入口。
- 标准 HTTP/MQTT property 信封支持 shadow，Raw 保留原文，Parser 校验名称且只允许属性上报，StandardMessage 携带 shadowName。设备真实凭据读取 HTTP 命名影子或同一精确 ACL 主题上的 MQTT name 查询。消息 ID 幂等范围不因名称变化而放宽；没有 reported 直写捷径。
- 实际 PostgreSQL 临时 schema 先建立旧版两张表并写入状态/历史，再运行迁移两次：默认期望版本 7、状态版本 9、属性与操作历史保持原值。名称隔离、期望 CAS、32 并发创建最多 16 个名称、满额后已有名称仍可更新、跨租户列表隔离全部通过（1.68 秒）。同一合约的 Memory 测试通过。
- 超出名称上限保留 Raw/Standard，并将错误投影到默认影子；原告警规则继续执行。`go test -race ... -run 'Shadow|StandardVersion'` 覆盖模型使用方、标准 Parser、Core、Memory、Onboarding 和 API，通过；其中 model 包该筛选无匹配用例，完整模型测试随后由根目录全量测试执行，未把空筛选写成测试通过。
- 实际 Chrome 最后一次认证协调测试整体通过（4.63 秒），其中命名影子选择/真实上报值 7/人工确认期望值 9/默认值 42 不变/独立版本/窄屏子用例 1.81 秒，原孪生拓扑浏览器回归 2.80 秒。提交前新增了编辑期间身份切换复核，并重新构建前端、执行该次浏览器验收。
- 隔离 EMQX 5.8.8 完整认证配置通过（0.48 秒）：命名属性真实发布、归档投影、命名查询、默认影子不变、JWT/用户名绑定、越权主题拒绝、凭据轮换及停用断连均实际执行。独立 Broker 和临时配置已清理。根目录 `go test ./...`、前端 56 项测试及构建通过。
- 本模块无阻塞；命名影子不支持删除/版本重置，设备固件自动应用、长期全量状态历史与三维仿真未实现；真实设备、生产迁移未执行。下一项继续补 Edge 自动登记及现场接入流程，其他 P2 待办仍按前述边界推进。说明同步于 `DEVICE_SHADOW.md`。
