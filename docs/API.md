# 接口清单

本文件由 `IOT_UPDATE_ROUTES=1 go test ./internal/httpapi -run TestRegisteredRoutesMatchSnapshot` 根据已注册路由生成，请勿手工编辑。“菜单”为可见该接口所需的菜单权限；“单独授权的操作”为角色与用户权限中需另外勾选的操作，“—”表示随菜单或登录状态授权。所有业务接口还受租户隔离与用户设备范围约束，开放接口 `/api/open/v1` 使用绑定用户的密钥，见 [设备接入与协议](INTEGRATION.md)。

## `/api/external/v1`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/external/v1/:tenantId/:id` | — | — |

## `/api/open/v1`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/open/v1/ai/chat/stream` | 智能助手 | — |
| `POST` | `/api/open/v1/ai/chat` | 智能助手 | — |
| `GET` | `/api/open/v1/ai/workflows` | 智能助手 | — |
| `POST` | `/api/open/v1/alarms/:id/actions` | 告警中心 | — |
| `GET` | `/api/open/v1/alarms/:id` | 告警中心 | — |
| `GET` | `/api/open/v1/alarms` | 告警中心 | — |
| `POST` | `/api/open/v1/device-messages` | — | — |
| `GET` | `/api/open/v1/devices/:deviceId/latest` | 设备管理 | — |
| `GET` | `/api/open/v1/devices/:deviceId/properties/history` | 设备管理 | — |
| `GET` | `/api/open/v1/devices` | 设备管理 | — |
| `GET` | `/api/open/v1/me` | — | — |
| `POST` | `/api/open/v1/message-topics/credentials` | — | — |

## `/api/v1/access`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/access/api-keys/:id/rotate` | 用户与权限 | 轮换开放接口密钥 |
| `DELETE` | `/api/v1/access/api-keys/:id` | 用户与权限 | 删除开放接口密钥 |
| `PUT` | `/api/v1/access/api-keys/:id` | 用户与权限 | 编辑开放接口密钥 |
| `GET` | `/api/v1/access/api-keys` | 用户与权限 | — |
| `POST` | `/api/v1/access/api-keys` | 用户与权限 | 添加开放接口密钥 |
| `GET` | `/api/v1/access/device-options` | 用户与权限 | — |
| `GET` | `/api/v1/access/permissions` | 用户与权限 | — |
| `DELETE` | `/api/v1/access/roles/:id` | 用户与权限 | 删除角色 |
| `PUT` | `/api/v1/access/roles/:id` | 用户与权限 | 编辑角色 |
| `GET` | `/api/v1/access/roles` | 用户与权限 | — |
| `POST` | `/api/v1/access/roles` | 用户与权限 | 添加角色 |
| `POST` | `/api/v1/access/users/:id/password` | 用户与权限 | 重置用户密码 |
| `DELETE` | `/api/v1/access/users/:id` | 用户与权限 | 删除用户 |
| `PUT` | `/api/v1/access/users/:id` | 用户与权限 | 编辑用户 |
| `GET` | `/api/v1/access/users` | 用户与权限 | — |
| `POST` | `/api/v1/access/users` | 用户与权限 | 添加用户 |

## `/api/v1/ai`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `GET` | `/api/v1/ai/alarm-analysis/:alarmId/progress/:jobId` | 告警中心 | — |
| `GET` | `/api/v1/ai/alarm-analysis/:alarmId/progress` | 告警中心 | — |
| `POST` | `/api/v1/ai/alarm-analysis/:alarmId/run` | 告警中心 | 执行巡检 |
| `GET` | `/api/v1/ai/alarm-analysis/:alarmId` | 告警中心 | — |
| `POST` | `/api/v1/ai/chat/stream` | 智能助手 | 流式问答 |
| `POST` | `/api/v1/ai/chat` | 智能助手 | 发送提问 |
| `DELETE` | `/api/v1/ai/conversations/:id` | 智能助手 | — |
| `GET` | `/api/v1/ai/conversations/:id` | 智能助手 | — |
| `GET` | `/api/v1/ai/conversations` | 智能助手 | — |
| `GET` | `/api/v1/ai/embedding-config` | 模型管理 | — |
| `PUT` | `/api/v1/ai/embedding-config` | 模型管理 | 编辑模型管理 |
| `POST` | `/api/v1/ai/embedding-test` | 模型管理 | 新增 / 执行模型管理 |
| `POST` | `/api/v1/ai/health-inspection/pdf` | 智能巡检 | 下载报告 |
| `GET` | `/api/v1/ai/health-inspection/progress/:jobId` | 智能巡检 | — |
| `GET` | `/api/v1/ai/health-inspection/progress` | 智能巡检 | — |
| `GET` | `/api/v1/ai/health-inspection/reports/:jobId` | 智能巡检 | — |
| `POST` | `/api/v1/ai/health-inspection/run` | 智能巡检 | 执行巡检 |
| `POST` | `/api/v1/ai/health-inspection` | 智能巡检 | 同步执行巡检 |
| `POST` | `/api/v1/ai/protocol-assistant/generate` | 协议开发 | 生成协议 |
| `POST` | `/api/v1/ai/protocol-assistant/preview` | 协议开发 | 解析测试 |
| `POST` | `/api/v1/ai/protocol-assistant/publish` | 协议开发 | 保存生成的协议 |
| `GET` | `/api/v1/ai/providers/config` | 模型管理 | — |
| `PUT` | `/api/v1/ai/providers/config` | 模型管理 | 编辑模型管理 |
| `POST` | `/api/v1/ai/providers/test` | 模型管理 | 连接测试 |
| `GET` | `/api/v1/ai/providers` | 模型管理 | — |
| `POST` | `/api/v1/ai/reports` | 智能助手 | 生成运维报告 |
| `POST` | `/api/v1/ai/rule-draft` | 告警规则 | 新增 / 执行告警规则 |
| `POST` | `/api/v1/ai/runs/:id/stop` | 模型管理 | 强制停止 AI 工作流 |
| `GET` | `/api/v1/ai/runs/history` | 模型管理 | 查看 AI 运行记录 |
| `GET` | `/api/v1/ai/runs/usage` | 模型管理 | 查看 AI 用量统计 |
| `GET` | `/api/v1/ai/runs` | 模型管理 | 查看运行中的 AI 工作流 |
| `POST` | `/api/v1/ai/workflows/:id/knowledge-binding/test` | 知识库 | 测试知识检索 |
| `GET` | `/api/v1/ai/workflows/:id/knowledge-binding` | 知识库 | — |
| `PUT` | `/api/v1/ai/workflows/:id/knowledge-binding` | 知识库 | 配置知识检索策略 |
| `DELETE` | `/api/v1/ai/workflows/:id` | 智能助手 | 删除智能体 |
| `PUT` | `/api/v1/ai/workflows/:id` | 智能助手 | 编辑 / 启停智能体 |
| `GET` | `/api/v1/ai/workflows/admin` | 智能助手 | 查看智能体配置 |
| `GET` | `/api/v1/ai/workflows` | 智能助手 | — |
| `POST` | `/api/v1/ai/workflows` | 智能助手 | 新建智能体 |

## `/api/v1/alarms`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/alarms/:id/actions` | 告警中心 | 处置告警 |
| `DELETE` | `/api/v1/alarms/:id/attachments/:attachmentId` | 告警中心 | 删除告警附件 |
| `GET` | `/api/v1/alarms/:id/attachments/:attachmentId` | 告警中心 | — |
| `POST` | `/api/v1/alarms/:id/attachments` | 告警中心 | 上传告警附件 |
| `POST` | `/api/v1/alarms/:id/disposition` | 告警中心 | 填写告警核实结论 |
| `GET` | `/api/v1/alarms/:id/location-plan` | 告警中心 | — |
| `GET` | `/api/v1/alarms/:id/media/:kind` | 告警中心 | — |
| `POST` | `/api/v1/alarms/:id/media/retry` | 告警中心 | 重试告警媒体归档 |
| `GET` | `/api/v1/alarms/:id/notifications` | 告警中心 | — |
| `DELETE` | `/api/v1/alarms/:id` | 告警中心 | 删除告警中心 |
| `GET` | `/api/v1/alarms/:id` | 告警中心 | — |
| `GET` | `/api/v1/alarms/export` | 告警中心 | 导出告警 |
| `GET` | `/api/v1/alarms/reports/monthly` | 告警中心 | 下载告警月报 |
| `GET` | `/api/v1/alarms/statistics/ai-analysis` | 告警中心 | — |
| `GET` | `/api/v1/alarms/statistics/disposition` | 告警中心 | — |
| `GET` | `/api/v1/alarms` | 告警中心 | — |

## `/api/v1/auth`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/auth/login` | — | — |
| `GET` | `/api/v1/auth/me` | — | — |
| `POST` | `/api/v1/auth/password` | — | — |

## `/api/v1/backups`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `GET` | `/api/v1/backups/:id/files/:filename` | 备份中心 | 下载备份 |
| `GET` | `/api/v1/backups/:id/files` | 备份中心 | — |
| `POST` | `/api/v1/backups/:id/restore-drill` | 备份中心 | 校验备份 |
| `POST` | `/api/v1/backups/:id/restore` | 备份中心 | 恢复到独立库 |
| `DELETE` | `/api/v1/backups/:id` | 备份中心 | 删除备份中心 |
| `GET` | `/api/v1/backups/:id` | 备份中心 | — |
| `GET` | `/api/v1/backups` | 备份中心 | — |
| `POST` | `/api/v1/backups` | 备份中心 | 新增 / 执行备份中心 |

## `/api/v1/connectors`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `GET` | `/api/v1/connectors/types` | 平台接入点 | — |
| `GET` | `/api/v1/connectors` | 平台接入点 | — |

## `/api/v1/dashboard`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `GET` | `/api/v1/dashboard` | 运行总览 | — |

## `/api/v1/device-ingest`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/device-ingest/:deviceId` | — | — |
| `POST` | `/api/v1/device-ingest/standard/:tenantId/:productId/:deviceId/:kind` | — | — |

## `/api/v1/device-mqtt`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/device-mqtt/token` | — | — |

## `/api/v1/device-registry`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `GET` | `/api/v1/device-registry/:id/children` | 设备管理 | — |
| `POST` | `/api/v1/device-registry/:id/children` | 设备管理 | 登记子设备 |
| `GET` | `/api/v1/device-registry/:id/commands` | 设备管理 | — |
| `POST` | `/api/v1/device-registry/:id/commands` | 设备管理 | 设备控制 |
| `GET` | `/api/v1/device-registry/:id/connection` | 设备管理 | — |
| `DELETE` | `/api/v1/device-registry/:id/credentials` | 设备管理 | 管理设备凭据 |
| `POST` | `/api/v1/device-registry/:id/credentials` | 设备管理 | 管理设备凭据 |
| `POST` | `/api/v1/device-registry/:id/debug` | 模拟设备测试 | 发送测试报文 |
| `GET` | `/api/v1/device-registry/:id/history` | 设备管理 | — |
| `GET` | `/api/v1/device-registry/:id/signals` | 设备管理 | — |
| `GET` | `/api/v1/device-registry/:id/verification` | 设备管理 | — |
| `POST` | `/api/v1/device-registry/:id/verification` | 设备管理 | 新增 / 执行设备管理 |
| `DELETE` | `/api/v1/device-registry/:id` | 设备管理 | 删除设备管理 |
| `PUT` | `/api/v1/device-registry/:id` | 设备管理 | 编辑设备管理 |
| `GET` | `/api/v1/device-registry` | 设备管理 | — |
| `POST` | `/api/v1/device-registry` | 设备管理 | 新增 / 执行设备管理 |

## `/api/v1/device-states`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/device-states` | 设备管理 | 新增 / 执行设备管理 |

## `/api/v1/devices`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `GET` | `/api/v1/devices/:deviceId/latest` | 设备管理 | — |
| `GET` | `/api/v1/devices/:deviceId/properties/history` | 设备管理 | — |
| `GET` | `/api/v1/devices` | 设备管理 | — |

## `/api/v1/discovered-devices`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/discovered-devices/:id/register` | 设备管理 | 新增 / 执行设备管理 |

## `/api/v1/duty`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `DELETE` | `/api/v1/duty/assignments/:id` | 排班 | 删除排班 |
| `GET` | `/api/v1/duty/assignments/:id` | 排班 | — |
| `PUT` | `/api/v1/duty/assignments/:id` | 排班 | 编辑排班 |
| `POST` | `/api/v1/duty/assignments/batch` | 排班 | 批量排班 |
| `GET` | `/api/v1/duty/assignments` | 排班 | — |
| `POST` | `/api/v1/duty/assignments` | 排班 | 新增排班 |
| `DELETE` | `/api/v1/duty/shifts/:id` | 排班 | 删除班次模板 |
| `GET` | `/api/v1/duty/shifts/:id` | 排班 | — |
| `PUT` | `/api/v1/duty/shifts/:id` | 排班 | 编辑班次模板 |
| `GET` | `/api/v1/duty/shifts` | 排班 | — |
| `POST` | `/api/v1/duty/shifts` | 排班 | 新增班次模板 |
| `POST` | `/api/v1/duty/swaps/:id/review` | 排班 | 审批换班 |
| `GET` | `/api/v1/duty/swaps/:id` | 排班 | — |
| `GET` | `/api/v1/duty/swaps` | 排班 | — |
| `POST` | `/api/v1/duty/swaps` | 排班 | 申请换班 |

## `/api/v1/events`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `GET` | `/api/v1/events` | — | — |

## `/api/v1/external-data`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `DELETE` | `/api/v1/external-data/bindings/:id` | 外部数据接入 | 删除外部数据接入 |
| `PUT` | `/api/v1/external-data/bindings/:id` | 外部数据接入 | 编辑外部数据接入 |
| `GET` | `/api/v1/external-data/bindings` | 外部数据接入 | — |
| `POST` | `/api/v1/external-data/bindings` | 外部数据接入 | 新增 / 执行外部数据接入 |
| `POST` | `/api/v1/external-data/endpoints/:id/pull` | 外部数据接入 | 新增 / 执行外部数据接入 |
| `POST` | `/api/v1/external-data/endpoints/:id/receive` | 外部数据接入 | 新增 / 执行外部数据接入 |
| `POST` | `/api/v1/external-data/endpoints/:id/rotate-key` | 外部数据接入 | 新增 / 执行外部数据接入 |
| `POST` | `/api/v1/external-data/endpoints/:id/test-fetch` | 外部数据接入 | 连接测试 |
| `POST` | `/api/v1/external-data/endpoints/:id/test` | 外部数据接入 | 连接测试 |
| `DELETE` | `/api/v1/external-data/endpoints/:id` | 外部数据接入 | 删除外部数据接入 |
| `PUT` | `/api/v1/external-data/endpoints/:id` | 外部数据接入 | 编辑外部数据接入 |
| `GET` | `/api/v1/external-data/endpoints` | 外部数据接入 | — |
| `POST` | `/api/v1/external-data/endpoints` | 外部数据接入 | 新增 / 执行外部数据接入 |
| `POST` | `/api/v1/external-data/jobs/:id/retry` | 外部数据接入 | 新增 / 执行外部数据接入 |
| `GET` | `/api/v1/external-data/jobs` | 外部数据接入 | — |
| `POST` | `/api/v1/external-data/records/:id/retry` | 外部数据接入 | 新增 / 执行外部数据接入 |
| `GET` | `/api/v1/external-data/records/:id` | 外部数据接入 | — |
| `GET` | `/api/v1/external-data/records` | 外部数据接入 | — |
| `DELETE` | `/api/v1/external-data/sources/:id` | 外部数据接入 | 下载源码 |
| `PUT` | `/api/v1/external-data/sources/:id` | 外部数据接入 | 下载源码 |
| `GET` | `/api/v1/external-data/sources` | 外部数据接入 | — |
| `POST` | `/api/v1/external-data/sources` | 外部数据接入 | 下载源码 |

## `/api/v1/extinguisher-inspections`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/extinguisher-inspections/:id/cancel` | 灭火器管理 | 取消巡检任务 |
| `POST` | `/api/v1/extinguisher-inspections/:id/inspect` | 灭火器管理 | 提交巡检结果 |
| `POST` | `/api/v1/extinguisher-inspections/:id/rectify` | 灭火器管理 | 提交整改 |
| `POST` | `/api/v1/extinguisher-inspections/:id/review` | 灭火器管理 | 复核整改 |
| `GET` | `/api/v1/extinguisher-inspections/:id` | 灭火器管理 | — |
| `POST` | `/api/v1/extinguisher-inspections/batch` | 灭火器管理 | 批量创建巡检任务 |
| `GET` | `/api/v1/extinguisher-inspections` | 灭火器管理 | — |
| `POST` | `/api/v1/extinguisher-inspections` | 灭火器管理 | 创建巡检任务 |

## `/api/v1/extinguishers`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `DELETE` | `/api/v1/extinguishers/:id` | 灭火器管理 | 删除灭火器 |
| `GET` | `/api/v1/extinguishers/:id` | 灭火器管理 | — |
| `PUT` | `/api/v1/extinguishers/:id` | 灭火器管理 | 编辑灭火器 |
| `GET` | `/api/v1/extinguishers/statistics` | 灭火器管理 | — |
| `GET` | `/api/v1/extinguishers` | 灭火器管理 | — |
| `POST` | `/api/v1/extinguishers` | 灭火器管理 | 新增灭火器 |

## `/api/v1/fire-dispatches`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/fire-dispatches/:id/return` | 消防站管理 | 登记归队 |
| `GET` | `/api/v1/fire-dispatches/:id` | 消防站管理 | — |
| `GET` | `/api/v1/fire-dispatches` | 消防站管理 | — |
| `POST` | `/api/v1/fire-dispatches` | 消防站管理 | 登记出勤 |

## `/api/v1/fire-equipment`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `DELETE` | `/api/v1/fire-equipment/:id` | 消防站管理 | 删除消防器材 |
| `GET` | `/api/v1/fire-equipment/:id` | 消防站管理 | — |
| `PUT` | `/api/v1/fire-equipment/:id` | 消防站管理 | 编辑消防器材 |
| `GET` | `/api/v1/fire-equipment` | 消防站管理 | — |
| `POST` | `/api/v1/fire-equipment` | 消防站管理 | 新增消防器材 |

## `/api/v1/fire-personnel`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `DELETE` | `/api/v1/fire-personnel/:id` | 消防站管理 | 删除消防人员 |
| `GET` | `/api/v1/fire-personnel/:id` | 消防站管理 | — |
| `PUT` | `/api/v1/fire-personnel/:id` | 消防站管理 | 编辑消防人员 |
| `GET` | `/api/v1/fire-personnel` | 消防站管理 | — |
| `POST` | `/api/v1/fire-personnel` | 消防站管理 | 新增消防人员 |

## `/api/v1/fire-safety`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `GET` | `/api/v1/fire-safety/options` | 消防站管理 | — |

## `/api/v1/fire-stations`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `DELETE` | `/api/v1/fire-stations/:id` | 消防站管理 | 删除消防站 |
| `GET` | `/api/v1/fire-stations/:id` | 消防站管理 | — |
| `PUT` | `/api/v1/fire-stations/:id` | 消防站管理 | 编辑消防站 |
| `GET` | `/api/v1/fire-stations/statistics` | 消防站管理 | — |
| `GET` | `/api/v1/fire-stations` | 消防站管理 | — |
| `POST` | `/api/v1/fire-stations` | 消防站管理 | 新增消防站 |

## `/api/v1/integrations`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/integrations/video/cameras/:id/live/onvif-profiles` | 摄像头映射 | 查询 ONVIF 媒体配置 |
| `POST` | `/api/v1/integrations/video/cameras/:id/live/test` | 摄像头映射 | 直播连接测试 |
| `GET` | `/api/v1/integrations/video/cameras/:id/live` | 摄像头映射 | — |
| `PUT` | `/api/v1/integrations/video/cameras/:id/live` | 摄像头映射 | 配置摄像头直播 |
| `DELETE` | `/api/v1/integrations/video/cameras/:id` | 摄像头映射 | 删除摄像头映射 |
| `PUT` | `/api/v1/integrations/video/cameras/:id` | 摄像头映射 | 编辑摄像头映射 |
| `GET` | `/api/v1/integrations/video/cameras` | 摄像头映射 | — |
| `POST` | `/api/v1/integrations/video/cameras` | 摄像头映射 | 新增 / 执行摄像头映射 |
| `POST` | `/api/v1/integrations/video/gb28181/devices/:deviceId/refresh` | 摄像头映射 | 刷新国标设备目录 |
| `DELETE` | `/api/v1/integrations/video/gb28181/devices/:deviceId` | 摄像头映射 | 删除国标设备 |
| `PUT` | `/api/v1/integrations/video/gb28181/devices/:deviceId` | 摄像头映射 | 配置国标设备 |
| `GET` | `/api/v1/integrations/video/gb28181/devices` | 摄像头映射 | — |
| `GET` | `/api/v1/integrations/video/relations` | 摄像头映射 | — |

## `/api/v1/knowledge`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/knowledge/documents/:id/retry` | 知识库 | 新增 / 执行知识库 |
| `DELETE` | `/api/v1/knowledge/documents/:id` | 知识库 | 删除知识库 |
| `GET` | `/api/v1/knowledge/documents/:id` | 知识库 | — |
| `GET` | `/api/v1/knowledge/documents` | 知识库 | — |
| `POST` | `/api/v1/knowledge/documents` | 知识库 | 新增 / 执行知识库 |

## `/api/v1/message-topics`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `DELETE` | `/api/v1/message-topics/:id` | 消息主题 | 删除消息主题 |
| `PUT` | `/api/v1/message-topics/:id` | 消息主题 | 编辑消息主题 |
| `POST` | `/api/v1/message-topics/query/preview` | 消息主题 | 预览主题数据查询 |
| `GET` | `/api/v1/message-topics` | 消息主题 | — |
| `POST` | `/api/v1/message-topics` | 消息主题 | 新增消息主题 |

## `/api/v1/mqtt`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/mqtt/load-token` | — | — |
| `POST` | `/api/v1/mqtt/token` | — | — |

## `/api/v1/notifications`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/notifications/channels/:id/test` | 告警通知 | 连接测试 |
| `DELETE` | `/api/v1/notifications/channels/:id` | 告警通知 | 删除告警通知 |
| `PUT` | `/api/v1/notifications/channels/:id` | 告警通知 | 编辑告警通知 |
| `GET` | `/api/v1/notifications/channels` | 告警通知 | — |
| `POST` | `/api/v1/notifications/channels` | 告警通知 | 新增 / 执行告警通知 |
| `GET` | `/api/v1/notifications/options` | 告警通知 | — |
| `DELETE` | `/api/v1/notifications/policies/:id` | 告警通知 | 删除告警通知 |
| `PUT` | `/api/v1/notifications/policies/:id` | 告警通知 | 编辑告警通知 |
| `GET` | `/api/v1/notifications/policies` | 告警通知 | — |
| `POST` | `/api/v1/notifications/policies` | 告警通知 | 新增 / 执行告警通知 |

## `/api/v1/onboarding`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/onboarding/batches/:id/credentials` | 设备管理 | — |
| `POST` | `/api/v1/onboarding/batches/:id/retry` | 设备管理 | — |
| `GET` | `/api/v1/onboarding/batches/:id` | 设备管理 | — |
| `POST` | `/api/v1/onboarding/batches/preflight` | 设备管理 | — |
| `GET` | `/api/v1/onboarding/batches` | 设备管理 | — |
| `POST` | `/api/v1/onboarding/batches` | 设备管理 | — |
| `GET` | `/api/v1/onboarding/drafts/:id` | 设备管理 | — |
| `PUT` | `/api/v1/onboarding/drafts/:id` | 设备管理 | — |
| `GET` | `/api/v1/onboarding/drafts` | 设备管理 | — |
| `GET` | `/api/v1/onboarding/preflight` | 设备管理 | — |
| `POST` | `/api/v1/onboarding` | 设备管理 | — |

## `/api/v1/ops`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `GET` | `/api/v1/ops/alerts/groups` | 监控告警 | — |
| `GET` | `/api/v1/ops/alerts/history` | 监控告警 | — |
| `GET` | `/api/v1/ops/alerts/rules` | 监控告警 | — |
| `GET` | `/api/v1/ops/alerts` | 监控告警 | — |
| `POST` | `/api/v1/ops/capacity/cleanup-data` | 容量测试 | — |
| `GET` | `/api/v1/ops/capacity/cleanup-fixtures` | 容量测试 | — |
| `GET` | `/api/v1/ops/capacity/cleanup/status` | 容量测试 | — |
| `GET` | `/api/v1/ops/capacity/cleanup` | 容量测试 | — |
| `POST` | `/api/v1/ops/capacity/cleanup` | 容量测试 | — |
| `GET` | `/api/v1/ops/capacity/environments` | 容量测试 | — |
| `POST` | `/api/v1/ops/capacity/plans/validate` | 容量测试 | 校验容量测试计划 |
| `GET` | `/api/v1/ops/capacity/runs/:id/cleanup` | 容量测试 | — |
| `GET` | `/api/v1/ops/capacity/runs/:id/report` | 容量测试 | 下载容量测试报告 |
| `POST` | `/api/v1/ops/capacity/runs/:id/stop` | 容量测试 | 停止容量测试 |
| `DELETE` | `/api/v1/ops/capacity/runs/:id` | 容量测试 | 清理容量测试数据和缓存 |
| `GET` | `/api/v1/ops/capacity/runs/:id` | 容量测试 | — |
| `GET` | `/api/v1/ops/capacity/runs` | 容量测试 | — |
| `POST` | `/api/v1/ops/capacity/runs` | 容量测试 | 启动容量测试 |
| `GET` | `/api/v1/ops/capacity/status` | 容量测试 | — |
| `POST` | `/api/v1/ops/dashboards/:uid/copy` | 仪表盘 | 复制仪表盘 |
| `GET` | `/api/v1/ops/dashboards/:uid/export` | 仪表盘 | — |
| `GET` | `/api/v1/ops/dashboards/:uid/panels/:panelId/data` | 仪表盘 | — |
| `GET` | `/api/v1/ops/dashboards/:uid/variables/:name/options` | 仪表盘 | — |
| `DELETE` | `/api/v1/ops/dashboards/:uid` | 仪表盘 | 删除仪表盘 |
| `GET` | `/api/v1/ops/dashboards/:uid` | 仪表盘 | — |
| `PUT` | `/api/v1/ops/dashboards/:uid` | 仪表盘 | 编辑仪表盘 |
| `POST` | `/api/v1/ops/dashboards/import` | 仪表盘 | 导入仪表盘 |
| `POST` | `/api/v1/ops/dashboards/preview` | 仪表盘 | 预览面板查询 |
| `GET` | `/api/v1/ops/dashboards/templates` | 仪表盘 | — |
| `GET` | `/api/v1/ops/dashboards` | 仪表盘 | — |
| `POST` | `/api/v1/ops/dashboards` | 仪表盘 | 新建仪表盘 |
| `POST` | `/api/v1/ops/datasources/:uid/test` | 仪表盘 | 测试数据源连接 |
| `DELETE` | `/api/v1/ops/datasources/:uid` | 仪表盘 | 删除数据源 |
| `GET` | `/api/v1/ops/datasources/:uid` | 仪表盘 | 查看数据源配置 |
| `PUT` | `/api/v1/ops/datasources/:uid` | 仪表盘 | 编辑数据源 |
| `GET` | `/api/v1/ops/datasources` | 仪表盘 | — |
| `POST` | `/api/v1/ops/datasources` | 仪表盘 | 新建数据源 |
| `DELETE` | `/api/v1/ops/folders/:uid` | 仪表盘 | 删除仪表盘文件夹 |
| `PUT` | `/api/v1/ops/folders/:uid` | 仪表盘 | 重命名仪表盘文件夹 |
| `GET` | `/api/v1/ops/folders` | 仪表盘 | — |
| `POST` | `/api/v1/ops/folders` | 仪表盘 | 新建仪表盘文件夹 |
| `GET` | `/api/v1/ops/logs/context` | 日志中心 | — |
| `DELETE` | `/api/v1/ops/logs/delete-requests/:id` | 日志中心 | 取消日志删除请求 |
| `GET` | `/api/v1/ops/logs/delete-requests` | 日志中心 | — |
| `POST` | `/api/v1/ops/logs/delete-requests` | 日志中心 | 提交日志删除请求 |
| `POST` | `/api/v1/ops/logs/export` | 日志中心 | 导出日志 |
| `GET` | `/api/v1/ops/logs/label-values` | 日志中心 | — |
| `GET` | `/api/v1/ops/logs/labels` | 日志中心 | — |
| `POST` | `/api/v1/ops/logs/query` | 日志中心 | 执行 LogQL 查询 |
| `GET` | `/api/v1/ops/logs/retention` | 日志中心 | — |
| `PUT` | `/api/v1/ops/logs/retention` | 日志中心 | 修改日志保留策略 |
| `DELETE` | `/api/v1/ops/logs/rule-groups/:name` | 日志中心 | 删除日志规则组 |
| `PUT` | `/api/v1/ops/logs/rule-groups/:name` | 日志中心 | 编辑 / 启停日志规则组 |
| `POST` | `/api/v1/ops/logs/rule-groups` | 日志中心 | 新建日志规则组 |
| `GET` | `/api/v1/ops/logs/rules` | 日志中心 | — |
| `GET` | `/api/v1/ops/logs/search` | 日志中心 | — |
| `GET` | `/api/v1/ops/logs/tail` | 日志中心 | — |
| `GET` | `/api/v1/ops/logs/validate` | 日志中心 | — |
| `GET` | `/api/v1/ops/logs/volume` | 日志中心 | — |
| `GET` | `/api/v1/ops/metrics/catalog` | 指标中心 | — |
| `GET` | `/api/v1/ops/metrics/explore` | 指标中心 | — |
| `GET` | `/api/v1/ops/metrics/label-values` | 指标中心 | — |
| `GET` | `/api/v1/ops/metrics/labels` | 指标中心 | — |
| `POST` | `/api/v1/ops/metrics/query` | 指标中心 | 执行 PromQL 查询 |
| `DELETE` | `/api/v1/ops/metrics/rule-groups/:name` | 指标中心 | 删除指标规则组 |
| `PUT` | `/api/v1/ops/metrics/rule-groups/:name` | 指标中心 | 编辑 / 启停指标规则组 |
| `POST` | `/api/v1/ops/metrics/rule-groups` | 指标中心 | 新建指标规则组 |
| `GET` | `/api/v1/ops/metrics/rules` | 指标中心 | — |
| `GET` | `/api/v1/ops/metrics/targets` | 指标中心 | — |
| `GET` | `/api/v1/ops/metrics/validate` | 指标中心 | — |
| `POST` | `/api/v1/ops/notifications/receivers/:name/test` | 监控告警 | 发送测试通知 |
| `GET` | `/api/v1/ops/notifications` | 监控告警 | — |
| `PUT` | `/api/v1/ops/notifications` | 监控告警 | 修改通知路由与渠道 |
| `GET` | `/api/v1/ops/overview/components/:id` | 运维总览 | — |
| `POST` | `/api/v1/ops/overview/dead-letters/:group/:partition/:offset/replay` | 运维总览 | 重新投递死信消息 |
| `GET` | `/api/v1/ops/overview/dead-letters` | 运维总览 | 查看死信消息 |
| `GET` | `/api/v1/ops/overview/kpis` | 运维总览 | — |
| `GET` | `/api/v1/ops/overview/series` | 运维总览 | — |
| `GET` | `/api/v1/ops/overview` | 运维总览 | — |
| `DELETE` | `/api/v1/ops/preferences/favorites/:uid` | — | — |
| `PUT` | `/api/v1/ops/preferences/favorites/:uid` | — | — |
| `DELETE` | `/api/v1/ops/preferences/history` | — | — |
| `GET` | `/api/v1/ops/preferences/history` | — | — |
| `DELETE` | `/api/v1/ops/preferences/saved-queries/:id` | — | — |
| `GET` | `/api/v1/ops/preferences/saved-queries` | — | — |
| `POST` | `/api/v1/ops/preferences/saved-queries` | — | — |
| `DELETE` | `/api/v1/ops/silences/:id` | 监控告警 | 解除静默 |
| `PUT` | `/api/v1/ops/silences/:id` | 监控告警 | 编辑静默 |
| `GET` | `/api/v1/ops/silences` | 监控告警 | — |
| `POST` | `/api/v1/ops/silences` | 监控告警 | 新建静默 |
| `GET` | `/api/v1/ops/status` | — | — |

## `/api/v1/products`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/products/:id/preparation/apply` | 设备模板 | 新增 / 执行设备模板 |
| `POST` | `/api/v1/products/:id/preparation/rollback` | 设备模板 | 回滚 |
| `POST` | `/api/v1/products/:id/preparation/trial` | 设备模板 | 新增 / 执行设备模板 |
| `GET` | `/api/v1/products/:id/preparation` | 设备模板 | — |
| `PUT` | `/api/v1/products/:id/preparation` | 设备模板 | 编辑设备模板 |
| `POST` | `/api/v1/products/:id/verification` | 设备模板 | 新增 / 执行设备模板 |
| `DELETE` | `/api/v1/products/:id` | 设备模板 | 删除设备模板 |
| `PUT` | `/api/v1/products/:id` | 设备模板 | 编辑设备模板 |
| `GET` | `/api/v1/products/protocol-binding-check` | 设备模板 | — |
| `GET` | `/api/v1/products` | 设备模板 | — |
| `POST` | `/api/v1/products` | 设备模板 | 新增 / 执行设备模板 |

## `/api/v1/protocol-packages`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/protocol-packages/:id/test` | 协议开发 | 连接测试 |
| `PUT` | `/api/v1/protocol-packages/:id` | 协议开发 | 编辑协议开发 |
| `GET` | `/api/v1/protocol-packages` | 协议开发 | — |
| `POST` | `/api/v1/protocol-packages` | 协议开发 | 新增 / 执行协议开发 |

## `/api/v1/raw-messages`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `GET` | `/api/v1/raw-messages/:id/download` | 原始报文 | 下载报文 |
| `GET` | `/api/v1/raw-messages/:id` | 原始报文 | — |
| `POST` | `/api/v1/raw-messages/download` | 原始报文 | 下载报文 |
| `POST` | `/api/v1/raw-messages/replay` | 原始报文 | 回放报文 |
| `GET` | `/api/v1/raw-messages` | 原始报文 | — |
| `POST` | `/api/v1/raw-messages` | 原始报文 | 新增 / 执行原始报文 |

## `/api/v1/replays`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `GET` | `/api/v1/replays/:id` | 原始报文 | — |

## `/api/v1/rules`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `DELETE` | `/api/v1/rules/:id` | 告警规则 | 删除告警规则 |
| `PUT` | `/api/v1/rules/:id` | 告警规则 | 编辑告警规则 |
| `GET` | `/api/v1/rules/fields` | 告警规则 | — |
| `GET` | `/api/v1/rules` | 告警规则 | — |
| `POST` | `/api/v1/rules` | 告警规则 | 新增 / 执行告警规则 |

## `/api/v1/sites`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `DELETE` | `/api/v1/sites/buildings/:id` | 单位建筑 | 删除建筑 |
| `PUT` | `/api/v1/sites/buildings/:id` | 单位建筑 | 编辑建筑 |
| `POST` | `/api/v1/sites/buildings` | 单位建筑 | 新增建筑 |
| `GET` | `/api/v1/sites/floors/:id/plan` | 单位建筑 | — |
| `PUT` | `/api/v1/sites/floors/:id/plan` | 单位建筑 | 上传楼层平面图 |
| `DELETE` | `/api/v1/sites/floors/:id` | 单位建筑 | 删除楼层 |
| `PUT` | `/api/v1/sites/floors/:id` | 单位建筑 | 编辑楼层 |
| `POST` | `/api/v1/sites/floors` | 单位建筑 | 新增楼层 |
| `GET` | `/api/v1/sites/import-template` | 单位建筑 | — |
| `POST` | `/api/v1/sites/import` | 单位建筑 | 批量导入单位与点位 |
| `DELETE` | `/api/v1/sites/points/:id` | 单位建筑 | 删除设备点位 |
| `PUT` | `/api/v1/sites/points/:id` | 单位建筑 | 编辑设备点位 |
| `POST` | `/api/v1/sites/points` | 单位建筑 | 新增设备点位 |
| `DELETE` | `/api/v1/sites/units/:id` | 单位建筑 | 删除单位 |
| `PUT` | `/api/v1/sites/units/:id` | 单位建筑 | 编辑单位 |
| `POST` | `/api/v1/sites/units` | 单位建筑 | 新增单位 |
| `GET` | `/api/v1/sites` | 单位建筑 | — |

## `/api/v1/test-devices`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/test-devices/provision` | 模拟设备测试 | 连接测试 |

## `/api/v1/video`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/v1/video/cameras/:id/play-sessions` | 设备管理 | 观看摄像头直播 |
| `GET` | `/api/v1/video/cameras/:id` | 设备管理 | — |
| `GET` | `/api/v1/video/devices/:deviceId/cameras` | 设备管理 | — |
| `POST` | `/api/v1/video/hooks/:event` | — | — |
| `GET` | `/api/v1/video/media-auth` | — | — |
| `PUT` | `/api/v1/video/module` | — | — |
| `POST` | `/api/v1/video/play-sessions/:id/heartbeat` | 设备管理 | — |
| `POST` | `/api/v1/video/play-sessions/:id/whep` | 设备管理 | — |
| `DELETE` | `/api/v1/video/play-sessions/:id` | 设备管理 | — |
| `GET` | `/api/v1/video/status` | — | — |

## `/api/v2/device-access-profiles`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/v2/device-access-profiles/:id/devices/:deviceId/commands` | 平台接入点 | 设备控制 |
| `POST` | `/api/v2/device-access-profiles/:id/test` | 平台接入点 | 连接测试 |
| `DELETE` | `/api/v2/device-access-profiles/:id` | 平台接入点 | 删除平台接入点 |
| `PUT` | `/api/v2/device-access-profiles/:id` | 平台接入点 | 编辑平台接入点 |
| `GET` | `/api/v2/device-access-profiles` | 平台接入点 | — |
| `POST` | `/api/v2/device-access-profiles` | 平台接入点 | 新增 / 执行平台接入点 |

## `/api/v2/modbus-tcp`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/v2/modbus-tcp/import` | 协议开发 | 新增 / 执行协议开发 |

## `/api/v2/products`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/v2/products/:id/protocol-binding/rollback` | 设备模板 | 回滚 |
| `GET` | `/api/v2/products/:id/protocol-binding` | 设备模板 | — |
| `POST` | `/api/v2/products/:id/protocol-binding` | 设备模板 | 绑定协议 |

## `/api/v2/protocol-source-template`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `GET` | `/api/v2/protocol-source-template` | 协议开发 | — |

## `/api/v2/protocols`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/api/v2/protocols/:id/package-releases` | 协议开发 | 上传制品 |
| `GET` | `/api/v2/protocols/:id/releases/:version/package` | 协议开发 | 下载制品 |
| `POST` | `/api/v2/protocols/:id/releases/:version/preview` | 协议开发 | 解析测试 |
| `POST` | `/api/v2/protocols/:id/releases/:version/publish` | 协议开发 | 发布 |
| `GET` | `/api/v2/protocols/:id/releases/:version/source` | 协议开发 | 下载源码 |
| `DELETE` | `/api/v2/protocols/:id/releases/:version` | 协议开发 | 删除协议版本 |
| `GET` | `/api/v2/protocols/:id/releases` | 协议开发 | — |
| `POST` | `/api/v2/protocols/:id/releases` | 协议开发 | 新增 / 执行协议开发 |
| `POST` | `/api/v2/protocols/:id/source-releases` | 协议开发 | 上传源码 |
| `DELETE` | `/api/v2/protocols/:id` | 协议开发 | 删除协议开发 |
| `GET` | `/api/v2/protocols` | 协议开发 | — |
| `POST` | `/api/v2/protocols` | 协议开发 | 新增 / 执行协议开发 |

## `/health`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `GET` | `/health/live` | — | — |
| `GET` | `/health/ready` | — | — |

## `/mcp`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `POST` | `/mcp/harness` | — | — |
| `DELETE` | `/mcp` | — | — |
| `GET` | `/mcp` | — | — |
| `POST` | `/mcp` | — | — |

## `/metrics`

| 方法 | 路径 | 菜单 | 单独授权的操作 |
| --- | --- | --- | --- |
| `GET` | `/metrics` | — | — |
