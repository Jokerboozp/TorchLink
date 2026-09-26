# 文档索引

文档按使用、开发和部署分类，保留当前操作说明与维护边界。源码、配置和本次实际验证用于确认实现状态；已归档的验收报告可从 Git 历史追溯。

## 使用平台

| 文档 | 内容 |
| --- | --- |
| [项目首页](../README.md) | 功能、架构和运行入口 |
| [统一设备接入](UNIFIED_DEVICE_ONBOARDING.md) | 协议、设备模板、设备、接入点、模拟设备测试、凭据及命令 |
| [用户与权限](USER_ACCESS_CONTROL.md) | 所属租户、角色与操作、设备范围、告警可见性及实时提醒 |
| [配置驱动协议](CONFIGURABLE_PROTOCOLS.md) | 上传报文或 Excel/CSV 点表、字段输入映射、预览与发布 |
| [生成演示数据](DEMO_DATA.md) | 可指定服务器的一键脚本、样例文件与运行报告 |
| [运维中心](OPS_CENTER.md) | Prometheus、Loki、Grafana、Alertmanager 的原生指标、日志、仪表盘、监控告警与通知；权限、配置与限制 |

设备管理中的主设备是实体设备；接入点是设备模板的软件连接配置，在模板详情中维护；独立的接入网关进程是部署组件。新设备从“设备管理 → 添加设备”接入；模拟设备测试用于发送测试报文；连接状态和已定义命令在设备详情查看与操作。

## 开发与协议

| 文档 | 内容 |
| --- | --- |
| [协作与开发规范](../AGENTS.md) | 仓库约定、业务边界和验证要求 |
| [技术详情](TECHNICAL_DETAILS.md) | 环境准备、运行、分页契约、容量边界和开发检查 |
| [前端说明](../iot_front/README.md) | 本地开发、页面权限、实时事件和浏览器脚本 |
| [Go 协议包](GO_PROTOCOL_PACKAGES.md) | 源码上传、编译、样例、Worker 契约、制品、发布与回滚 |
| [TCP 与主子设备](TCP_CHILD_DEVICE_ACCESS.md) | 监听/主动连接、查询和主子设备映射 |
| [GB26875 大华协议](GB26875_DAHUA_V103.md) | 协议适配及平台配置 |
| [独立协议示例](../protocol-packages/gb26875-dahua/README.md) | 示例 module 的打包与测试 |
| [接收可靠性](DEVICE_RECEIVE_RELIABILITY.md) | 部件告警、MQTT 持久队列和接收确认 |
| [摄像头与视频事件](VIDEO_SDK_ADAPTER.md) | 摄像头关联与外部视频事件 |
| [AI 工作流](AI_PLUGIN_HARNESS.md) | Harness、模型、知识检索、MCP 与权限 |
| [项目静态检查记录](PROJECT_REVIEW.md) | 2026-09-26 部署与文档核对、待修问题及未验证范围 |
| [容量压测方案](CAPACITY_TEST_PLAN.md) | 端到端压测场景、观测项与记录模板 |
| [Harness 服务](../deploy/deepseek-harness/README.md) | 独立服务配置、接口与平台授权边界 |

关键实现入口：[后端权限](../internal/httpapi/access_control.go)、[设备范围](../internal/httpapi/device_scope.go)、[用户事件](../internal/httpapi/user_events.go)、[前端权限](../iot_front/src/permissions.js)。文档与实现冲突时，应核对源码和针对性测试后修正文档。

## 部署与升级

| 文档 | 内容 |
| --- | --- |
| [部署配置与维护](DEPLOYMENT.md) | 三种部署配置、DeepSeek API Key、端口、数据库、升级和备份 |
| [离线部署](OFFLINE_DEPLOYMENT.md) | CentOS/Linux、Windows、macOS 打包；openEuler 本地 RPM 源、旧包修复补丁、镜像与嵌入模型交付、AI 联网边界 |
| [EMQX 认证与授权](../ops/emqx/PRODUCTION_SECURITY.md) | 设备及管理端 MQTT 身份、ACL 和旧会话撤销 |
| [网关部署边界](EDGE_AND_GATEWAY.md) | 进程部署与管理页面中不同网关概念 |

已有普通用户缺少设备范围时默认为无设备。部署新版本应同步前后端，并处理升级前签发的普通用户 MQTT 会话；完整步骤见 [用户权限升级](DEPLOYMENT.md#用户权限升级)。

验证入口集中在 [开发检查](TECHNICAL_DETAILS.md#源码与开发检查)，协议与部署专题补充各自的集成条件。截图、运行日志、压测结果和图册保存在本机输出目录，不作为项目文档分发；本机通过不等于生产验收。
