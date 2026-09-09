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

目前已实现默认及命名影子协调、HTTP/MQTT 查询和独立孪生拓扑。设备固件自动应用、长期全量状态历史存储与三维仿真未实现，不将此模块称作完整数字孪生平台。

## 验证（2026-09-09）

`TestDeviceShadow` 共享仓储契约在 Memory 与真实 PostgreSQL 临时 schema 通过：16 并发写者同一期望版本只有一个成功，16 个不同属性并发上报均保留，迟到/重复/删除/差异收敛和租户隔离通过。`TestDeviceShadowAuthenticatedReconciliation` 使用真实 HTTP handler、设备凭据和标准上报解析链路，通过角色/确认/可写属性/类型/版本冲突/凭据撤销检查。`TestShadowProjectionLimitDoesNotBlockMessageProcessing` 验证影子超限不抑制原告警规则。

Chrome `TestOnboardingBrowser` 8.92 秒通过，含影子编辑、人工确认、版本和差异显示，以及原有真实 Broker WebSocket 接入回归。测试未控制物理设备；实际固件读取/执行和生产迁移未执行。

设备关系和统一状态查看入口见 `DEVICE_TWINS.md`。拓扑版本与影子版本独立，关系编辑不触发期望状态写入或设备控制。

## MQTT 读取默认影子

标准设备使用现有凭据换取 MQTT JWT。令牌接口现在为标准设备返回 `shadowRequestTopic` 与 `shadowResponseTopic`，分别是本设备的 `/iot/up/{tenant}/{product}/{device}/shadow-get` 与 `/iot/down/{tenant}/{product}/{device}/shadow`。先订阅应答主题，再以 QoS 1、非 retained 发送 `{"id":"shadow-query-1"}`。正常应答为 `{"id":"shadow-query-1","status":"ok","shadow":{...}}`，用 id 关联请求；仓储读取失败返回真实 `status:error`，没有应答时可重新查询。

此请求不支持修改 desired，不接受租户、设备身份或任意回调主题；Broker 校验真实 JWT 与精确 ACL，平台另检查当前启用设备、产品和凭据状态。错误凭据、跨设备/跨租户主题均不授权；共享订阅的 Gateway 实例也可处理。请求最多 1 KiB、每设备每秒 20 次，并沿用受限 MQTT worker/队列；读取不会进入 Raw/Parser 或伪造属性上报。应答不 retained，设备不能把 Broker PUBACK 当作已经取得影子；旧 JWT 需重新获取才能使用新增主题授权。

## 命名影子

同一物理设备可按用途保存最多 16 个命名影子，另有一个默认影子。名称为 1–64 位 ASCII 字母、数字、点、下划线或连字符，首位须为字母或数字；名称区分大小写。省略名称使用默认影子，字符串 `default` 仍是独立命名影子。不同名称的 desired、reported、delta、版本与最近 1,000 次期望变更分别持久化。

- 管理端原有 GET/PATCH `/shadow`、GET `/shadow/history` 接口加 `?name=control` 即选择名称，重复 name 参数拒绝。`GET /api/v1/device-registry/{id}/shadows` 返回该设备已持久化的命名影子列表，不含默认影子。
- 设备使用真实设备凭据 GET `/api/v1/device-shadow?name=control`；MQTT 原有 shadow-get 主题发送 `{"id":"query-control","name":"control"}`。使用本设备原精确 ACL，不新增通配主题，也不允许跨设备查询。
- 标准 HTTP/MQTT property 上报在原信封加入 `"shadow":"control"`，例如 `{"version":"1.0","id":"control-report-1","shadow":"control","timestamp":1788998400000,"data":{"targetTemperature":24}}`。时间戳使用实际设备时间；Raw 保留原信封，成功解析后的 StandardMessage 带 `shadowName`，只更新对应影子。其他 event/state/command-reply 类型不接受命名影子。
- 同一设备、同一报文类型的消息 ID 仍须唯一，名称不会放宽幂等范围。不同名称不能复用同一 id 发送不同内容。迟到报文比较、设备身份认证和凭据撤销规则继续有效。

设备详情可切换默认/命名影子，输入新名称后先显示空状态，实际保存合法期望值或成功解析属性上报后才持久化。切换时清除上一名称的编辑草稿；保存期间禁止切换，并在提交前核对设备/名称未变。名称不是另一台设备，业务消息、规则、连接状态和告警仍归属同一物理设备；需要独立业务身份时应登记独立设备。

每个名称继续遵循属性数量、大小和版本约束。16 个名称的创建上限在仓储锁/数据库事务内检查，满额后仍允许更新已有名称。超出名称上限的属性消息保留 Raw/Standard，并继续规则处理；默认影子记录对应名称和错误消息 ID，不伪造第 17 个状态。没有提供删除影子或重置版本接口，避免旧报文重放后重新出现被当作全新状态；清除期望使用现有 null 补丁。

PostgreSQL 自动迁移为影子和历史增加默认空名称列，将主键扩展到名称。旧默认影子、期望版本、已上报值和历史保持原值；迁移可重复执行。该模块仍不绕过物模型可写属性检查、不直接执行设备动作。
