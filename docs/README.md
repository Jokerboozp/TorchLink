# 文档索引

本文档集于2026-09-14按当前代码核对，用户设备权限实现对应提交 `ab59c101`。操作指南描述当前功能；历史报告保留当时环境和实测结果，不作为后续版本所有功能的自动验收证明。

## 使用平台

| 文档 | 内容 |
| --- | --- |
| [项目首页](../README.md) | 功能、架构和运行入口 |
| [统一设备接入](UNIFIED_DEVICE_ONBOARDING.md) | 协议、产品、设备、接入网关、接入测试、凭据及命令 |
| [用户与权限](USER_ACCESS_CONTROL.md) | 所属租户、角色与操作、设备范围、告警可见性及实时提醒 |
| [配置驱动协议](CONFIGURABLE_PROTOCOLS.md) | 上传报文或 Excel/CSV 点表、字段输入映射、预览与发布 |
| [列表分页](LIST_PAGINATION.md) | 设备三个标签、类型筛选、授权总数、协议分页和异步刷新 |

设备管理中的主设备是实体设备；接入网关是绑定产品的软件连接配置；独立的接入网关进程是部署组件。实际连接调试在接入测试完成，日常已定义命令从设备连接详情执行。

## 开发与协议

| 文档 | 内容 |
| --- | --- |
| [协作与开发规范](../AGENTS.md) | 仓库约定、业务边界和验证要求 |
| [技术详情](TECHNICAL_DETAILS.md) | 技术栈、环境准备、运行和开发检查 |
| [前端说明](../iot_front/README.md) | 本地开发、页面权限、实时事件和浏览器脚本 |
| [Go 协议包](GO_PROTOCOL_PACKAGES.md) | 源码上传、编译、样例、Worker 契约、制品、发布与回滚 |
| [TCP 与主子设备](TCP_CHILD_DEVICE_ACCESS.md) | 监听/主动连接、查询和主子设备映射 |
| [GB26875 大华协议](GB26875_DAHUA_V103.md) | 协议适配及平台配置 |
| [独立协议示例](../protocol-packages/gb26875-dahua/README.md) | 示例 module 的打包与测试 |
| [接收可靠性](DEVICE_RECEIVE_RELIABILITY.md) | 部件告警、MQTT 持久队列和接收确认 |
| [视频适配](VIDEO_SDK_ADAPTER.md) | 摄像头映射、视频接口及适配能力边界 |
| [AI 工作流](AI_PLUGIN_HARNESS.md) | Harness、模型、知识检索、MCP 与权限 |
| [Harness 服务](../deploy/deepseek-harness/README.md) | 独立服务配置、接口与平台授权边界 |

关键实现入口：[后端权限](../internal/httpapi/access_control.go)、[设备范围](../internal/httpapi/device_scope.go)、[用户事件](../internal/httpapi/user_events.go)、[前端权限](../iot_front/src/permissions.js)。文档与实现冲突时，应核对源码和针对性测试后修正文档。

## 部署与升级

| 文档 | 内容 |
| --- | --- |
| [部署配置与维护](DEPLOYMENT.md) | 配置、端口、数据库、用户权限升级和备份 |
| [离线部署](OFFLINE_DEPLOYMENT.md) | CentOS/Linux、Windows、macOS 打包；openEuler 目标包、镜像、模型及交付检查 |
| [EMQX 认证与授权](../ops/emqx/PRODUCTION_SECURITY.md) | 设备及管理端 MQTT 身份、ACL 和旧会话撤销 |
| [旧版本兼容](EDGE_REMOVAL.md) | 已移除入口、保留数据及权限迁移 |
| [网关部署边界](EDGE_AND_GATEWAY.md) | 进程部署与管理页面中不同网关概念 |

已有普通用户缺少设备范围时默认为无设备。部署新版本应同步前后端，并处理升级前签发的普通用户 MQTT 会话；完整步骤见 [用户权限升级](DEPLOYMENT.md#用户权限升级)。

## 验证记录

| 记录 | 环境与适用范围 |
| --- | --- |
| [权限与界面验收](testing/2026-09-14-权限与界面验收.md) | Windows 本机；新增用户、设备范围、告警、实时提醒及界面回归 |
| [功能演示验收](testing/2026-09-14-功能验收.md) | 较早的 Windows 本机15菜单和演示业务链路快照 |
| [项目功能审计](PROJECT_AUDIT_2026-09-14.md) | 较早的 macOS / OrbStack 协议、中间件和模拟设备验证 |

`.e2e` 截图、运行日志及图册是本机生成物，不随仓库分发。模拟器、浏览器样本、真实中间件及真实设备验证在报告中分别说明，本机通过不等于生产验收。
