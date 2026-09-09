# 设备影子

设备连接详情的“设备影子”显示成功解析属性报文形成的已上报状态、操作员保存的期望状态及两者差异。它是持久化的状态协调记录，保存期望状态不代表设备已执行。当前不将影子自动转换为 Modbus、TCP 或 MQTT 控制命令。

产品物模型属性默认只读；例如 `{"identifier":"targetTemperature","dataType":"number","writable":true}` 才允许设置期望值。支持现有 string、number、integer、boolean、object、array 类型校验，顶层属性补丁中的 `null` 表示删除期望值。对象作为完整属性值替换，不做隐式深层合并。

## API

- `GET /api/v1/device-registry/{id}/shadow`：viewer 及以上，租户隔离。
- `PATCH /api/v1/device-registry/{id}/shadow`：operator 及以上，正文例如 `{"expectedDesiredVersion":0,"confirmed":true,"desired":{"targetTemperature":24}}`。须人工确认，过期版本返回 409，刷新核对后重新提交。
- `GET /api/v1/device-registry/{id}/shadow/history?page=1&pageSize=20`：最近 1,000 次期望状态变更，含操作人、版本、时间与完整期望值快照。
- `GET /api/v1/device-shadow`：兼容设备使用现有 `X-Device-Key` / `X-Device-Secret` 凭据获取自己的影子。部署使用 HTTPS。此接口在 API 进程提供，拆分部署时使用 API 地址。错误、禁用或撤销凭据不能读取。

设备取回 `desired` / `delta` 后，根据自身能力决定处理方式；处理结果通过既有标准 HTTP/MQTT 属性上报进入 Raw→Parser→Standard 链路。只有实际属性值一致时 `delta` 才清空，没有提供绕过解析的 reported 写入接口。

`desiredVersion` 只随期望值变更，设备持续上报不会使操作员的编辑版本失效；`version` 记录所有影子变化。每个 reported 属性分别记录设备时间与消息 ID，迟到的部分报文可补充新属性，但不能回退已有较新的属性；相同时间按消息 ID 排序稳定处理。重复报文不会增加状态版本。

每侧最多 256 个属性，整体影子最大 128 KiB。超限报文完整保留在原文和标准消息中，原有规则/告警继续运行；影子显示 `lastError` 与 `errorMessageId`，不将失败展示为最新完整状态。数据库失败仍返回错误供原消息处理链路重试。

目前已实现设备影子基础协调；数字孪生拓扑/关系、命名影子、MQTT 原生影子 RPC、设备固件自动应用、长期状态历史存储尚未实现，不将此模块称作完整数字孪生平台。

## 验证（2026-09-09）

`TestDeviceShadow` 共享仓储契约在 Memory 与真实 PostgreSQL 临时 schema 通过：16 并发写者同一期望版本只有一个成功，16 个不同属性并发上报均保留，迟到/重复/删除/差异收敛和租户隔离通过。`TestDeviceShadowAuthenticatedReconciliation` 使用真实 HTTP handler、设备凭据和标准上报解析链路，通过角色/确认/可写属性/类型/版本冲突/凭据撤销检查。`TestShadowProjectionLimitDoesNotBlockMessageProcessing` 验证影子超限不抑制原告警规则。

Chrome `TestOnboardingBrowser` 8.92 秒通过，含影子编辑、人工确认、版本和差异显示，以及原有真实 Broker WebSocket 接入回归。测试未控制物理设备；实际固件读取/执行和生产迁移未执行。

设备关系和统一状态查看入口见 `DEVICE_TWINS.md`。拓扑版本与影子版本独立，关系编辑不触发期望状态写入或设备控制。

## MQTT 读取默认影子

标准设备使用现有凭据换取 MQTT JWT。令牌接口现在为标准设备返回 `shadowRequestTopic` 与 `shadowResponseTopic`，分别是本设备的 `/iot/up/{tenant}/{product}/{device}/shadow-get` 与 `/iot/down/{tenant}/{product}/{device}/shadow`。先订阅应答主题，再以 QoS 1、非 retained 发送 `{"id":"shadow-query-1"}`。正常应答为 `{"id":"shadow-query-1","status":"ok","shadow":{...}}`，用 id 关联请求；仓储读取失败返回真实 `status:error`，没有应答时可重新查询。

此请求不支持修改 desired，不接受租户、设备身份或任意回调主题；Broker 校验真实 JWT 与精确 ACL，平台另检查当前启用设备、产品和凭据状态。错误凭据、跨设备/跨租户主题均不授权；共享订阅的 Gateway 实例也可处理。请求最多 1 KiB、每设备每秒 20 次，并沿用受限 MQTT worker/队列；读取不会进入 Raw/Parser 或伪造属性上报。应答不 retained，设备不能把 Broker PUBACK 当作已经取得影子；旧 JWT 需重新获取才能使用新增主题授权。
