# 对外开放接口

外部系统（园区平台、物业系统、上级监管平台等）通过开放接口查询和上报告警、上报设备消息、查询设备数据，以及调用智能助手问答，例如在对方首页嵌入问答机器人。实现入口为 [open_api.go](../internal/httpapi/open_api.go)。

## 授权模型

- 密钥在“用户与权限 → 开放接口”创建，需要该页面的新增、编辑、删除操作权限。
- 每个密钥绑定一个平台用户。外部请求按该用户的菜单、操作权限和设备范围执行，与其登录控制台时一致；密钥的开放能力只能进一步收窄。停用或删除用户后，其密钥立即失效；删除用户会同时删除其密钥。
- 建议为每个外部系统单独建用户和角色，只授予需要的功能和设备。
- 密钥明文只在创建时返回一次，平台只保存 SHA-256 摘要，存于租户的 `platform_access` 配置中，与用户、角色一起备份。密钥可停用、设置有效期或删除；需要更换时新建密钥后删除旧密钥。
- 密钥应只保存在对方服务端。浏览器页面中的问答机器人应由对方后端转发请求，不能把密钥下发到前端。

| 能力 | 可调用的接口 | 绑定用户还需要 |
| --- | --- | --- |
| `alarms:read` 查询告警 | `GET /alarms`、`GET /alarms/{alarmId}` | 告警中心菜单 |
| `alarms:report` 上报告警 | `POST /alarms` | 设备管理菜单；设备在其设备范围内 |
| `alarms:handle` 处置告警 | `POST /alarms/{alarmId}/actions` | 告警中心菜单及“处置告警”操作 |
| `messages:read` 查询设备与数据 | `GET /devices`、`GET /devices/{deviceId}/latest`、`GET /devices/{deviceId}/properties/history` | 设备管理菜单 |
| `messages:report` 上报设备消息 | `POST /device-messages` | 设备管理菜单；设备在其设备范围内 |
| `ai:chat` 智能问答 | `GET /ai/workflows`、`POST /ai/chat`、`POST /ai/chat/stream` | 智能助手菜单及“发送提问” / “流式问答”操作 |

## 调用约定

- 基础路径：`{平台地址}/api/open/v1`。
- 认证：`Authorization: Bearer <密钥>`，或 `X-API-Key: <密钥>`。
- 请求和响应均为 JSON；错误响应沿用 `{"status","detail"}` 格式。401 表示密钥无效、停用、过期或绑定用户不可用；403 表示密钥缺少能力，或绑定用户没有对应权限或设备访问权限。
- 限流：每个密钥每进程每秒 100 次请求；每台设备的上报与设备自身上报共享每秒 20 次额度。超限返回 429 和 `Retry-After`。多副本部署不共享额度。
- `GET /me` 不要求能力项，返回租户、密钥、绑定用户、能力和设备范围，可用于联调。

## 上报设备消息

```http
POST /api/open/v1/device-messages
Authorization: Bearer <密钥>
Content-Type: application/json

{"messages":[
  {"deviceId":"smoke-101","kind":"property","id":"ext-20260926-0001","timestamp":1790000000000,"data":{"temperature":26.5,"battery":88}},
  {"deviceId":"smoke-101","kind":"state","id":"ext-20260926-0002","timestamp":1790000000000,"online":true},
  {"deviceId":"smoke-102","kind":"event","id":"ext-20260926-0003","timestamp":1790000000000,"event":"self-test","data":{"result":"ok"}}
]}
```

- 每批 1 至 100 条，请求体最多 1 MiB。`kind` 为 `property`、`event`、`alarm` 或 `state`；字段含义同[标准报文](UNIFIED_DEVICE_ONBOARDING.md#http--mqtt-标准报文)。
- 设备须已在平台登记、已启用、模板已启用，且在绑定用户的设备范围内。外部系统不能自行指定租户、协议或解析器。
- 消息按内置标准协议写入原始报文，经解析、存储、规则和告警链路处理，原文来源记为 `open-api`。
- `id` 在同一设备和 `kind` 下唯一：相同内容重试返回 `created:false`，同一 `id` 换内容返回 `MESSAGE_CONFLICT`。
- 全部接受返回 202；部分接受返回 207；全部被拒绝返回 422（全部限流或暂停接收时返回 429）。`results` 逐条给出 `status`、`messageId` 或 `errorCode`，错误码包括 `INVALID_MESSAGE`、`DEVICE_NOT_FOUND`、`DEVICE_DISABLED`、`RATE_LIMITED`、`BACKPRESSURE`（平台处理积压超过上限，稍后重试）、`MESSAGE_CONFLICT`、`INGEST_FAILED`。
- 202 表示已进入原始接收链路，解析与告警为异步处理。

## 上报告警

```http
POST /api/open/v1/alarms

{"deviceId":"smoke-101","id":"alarm-20260926-0001","timestamp":1790000000000,
 "alarmType":"FIRE","alarmLevel":"CRITICAL","content":"3 层东侧走廊烟感报警","data":{"zone":"3F-E"}}
```

- 等同于该设备上报一条 `alarm` 消息，形成设备来源告警：匹配告警规则时按规则处理，未匹配规则也保留告警。`alarmLevel` 取 `CRITICAL`、`HIGH`、`MEDIUM`、`LOW`、`INFO`，缺省为 `HIGH`；`alarmType` 缺省为 `MANUAL_ALARM`。`content` 显示为告警内容。
- 成功返回 202，其中 `triggerId` 与生成告警的 `triggerId` 相同，可用 `GET /alarms?deviceId=...` 查找。设备不存在或不可见返回 404，其余错误码同上。
- 恢复：`FIRE`、`SMOKE_DETECTED`、`DEVICE_FAULT`、`DEVICE_OFFLINE`、`MANUAL_ALARM` 可通过上报对应恢复属性自动恢复，例如 `{"kind":"property","data":{"fireAlarm":false}}`；其他告警类型用处置接口的 `RECOVERED`。

## 查询与处置告警

- `GET /alarms` 支持 `deviceId`、`status`（`ACTIVE`、`ACKED`、`RECOVERED`、`CLOSED`）、`level`、`source`、`start`、`end`（毫秒时间戳）及 `page`、`pageSize` 分页，返回 `items`、`total`。只返回绑定用户可见设备的告警。
- `POST /alarms/{alarmId}/actions`，正文 `{"action":"ACKED"}`。`ACKED` 确认活动告警，`RECOVERED` 恢复活动或已确认告警，`CLOSED` 关闭告警；处置后重算设备状态，并记录操作人为“绑定用户 (API 密钥名)”。

## 查询设备与数据

- `GET /devices`：设备清单及运行状态，筛选与分页参数同控制台设备列表。
- `GET /devices/{deviceId}/latest`：设备状态与最新一条标准消息。
- `GET /devices/{deviceId}/properties/history?property=temperature&start=...&end=...`：属性历史，分页返回。

## 智能问答

```http
POST /api/open/v1/ai/chat

{"question":"3 号楼今天有哪些未处理告警？","conversationId":"park-user-1024"}
```

- 返回 `{"runId","workflowId","model","answer"}`；`answer` 为 Markdown。`POST /ai/chat/stream` 以 SSE 返回 `run.started`、`text.delta`、`tool.started`、`tool.completed`、`run.completed`、`run.failed` 事件，每个事件为 `event: <类型>` 与 `data: <JSON>`。
- 可选 `workflowId` 选择聊天智能体，可用列表见 `GET /ai/workflows`。模型由平台模型管理决定，请求不能覆盖。
- 问答作为 Harness 工作流运行，工具查询受绑定用户的设备范围和知识库权限限制；Harness 未配置时返回 503。
- `conversationId` 由对方系统为其每个终端用户生成，平台按租户、绑定用户和该值隔离多轮会话；不同终端用户须使用不同值。
