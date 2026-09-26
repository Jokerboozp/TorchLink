# 项目静态检查记录

日期：2026-09-26。范围：当前仓库的部署入口、配置、模型初始化、文档，以及设备权限查询、告警通知和凭据持久化等关键路径。结论来自源码阅读与静态检查，不是全项目验收；未连接运行环境、执行测试、构建镜像或测量性能，未操作压测进程及其数据。

## 本次修改

- 本地、在线、离线的 Bash / PowerShell 入口统一选择 DeepSeek API；用户填写 API Key，地址与模型预填。不再下载 Qwen，对话模型参数明确拒绝。
- 离线包只归档 `nomic-embed-text` 的 manifest 和所引用的内容哈希文件，避免整个 Ollama 缓存中的旧 Qwen 和身份密钥进入包；知识库仍保留 Ollama / Weaviate。
- 首次未配置 DeepSeek 密钥时允许 API 初始化，在模型管理中提示待配置；Harness 拒绝无密钥的工作流，配置测试与应用仍校验密钥。
- 部署选择 DeepSeek 时不恢复旧的 Ollama/Qwen 活动配置，避免旧数据库记录盖过部署设置；其他显式外部提供方仍保留。
- 核对仓库 Markdown 文档的部署、模型、菜单入口与本地链接，更新受影响的 README、技术详情、部署、离线、AI、运维、协议接入等说明。协议契约和权限规则未因模型部署变化而改写；旧故障处理保留为历史条件说明。

本次已通过 Bash / JavaScript / JSON / YAML 静态语法检查、两处 Vue 页面的静态编译解析、Markdown 本地链接核对及 `git diff --check`。Go 文件已 `gofmt`；未进行 Go 编译。当前环境未提供 PowerShell 解析器，Windows 脚本只完成源码核对。

实际包体积变化、API 冷启动、DeepSeek 请求、容器互通与 SMTP 投递均未在本次验证。离线安装不代表 AI 可断网使用，详见 [部署配置](DEPLOYMENT.md#ai-与工作流)。

## 待修问题

P1 表示建议优先解决；P2 表示需要在规模化或多用户环境中改进。以下均未在本次顺带修改。

| 优先级 | 问题与触发条件 | 源码依据 | 影响与建议 |
| --- | --- | --- | --- |
| P1 | 新部署的管理员密码仍固定为 `admin123` | [Bash 默认配置](../scripts/lib/deployment.sh) `ensure_deployment_env`；[PowerShell](../scripts/lib/deployment.ps1) `Ensure-DeploymentEnv`；离线生成器也使用相同值 | 未主动修改就暴露登录入口时存在弱口令风险。改为随机初始密码或首次登录强制修改，并同步交付方式；已有密码不能自动轮换。 |
| P1 | 邮件通知到期后不再补投，不能保证所有告警最终到达 | [通知队列](../internal/opscenter/device_notifications.go) `enqueue` / `deliver` | 事件存活 24 小时，之后停止提交 Alertmanager；满足 48 小时清理条件后删除。Alertmanager 长时间不可用可能导致从未提交的告警到期；SMTP 失败、限流、静默也可能阻止邮件。应持久记录未投递/失败状态、补投入口和指标；提交 Alertmanager 不等于收件箱确认。 |
| P1 | 告警保存与通知事件发布没有事务发件箱 | [业务告警](../internal/core/engine.go) `raiseDirectAlarm` / `raiseRuleAlarm`；[上报事件](../internal/core/alarm_report.go) `publishAlarmReport` | 数据库写入成功后再单独发布事件，中间失败或进程退出存在通知缺口。当前发布错误会向上返回，可以由上游重试补偿，但不能据此保证所有失败窗口都覆盖。建议同一事务保存待发布事件，再幂等投递；补充故障注入验证。 |
| P2 | 普通用户的原文和告警分页会遍历所有匹配数据 | [设备范围仓储](../internal/httpapi/device_scope.go) `scopedRaw` / `scopedAlarms` | 从 offset 0 开始每批 500 条读取，在 Go 中过滤设备并累积后分页；总数查询再次遍历。大数据量下，小页面仍可能触发多轮查询和大量内存分配。应将授权设备条件下推 SQL / 原文仓储，在存储端计数、分页；保持租户与主子设备独立授权。此处是复杂度判断，未测定延迟。 |
| P2 | 模型 API Key 明文保存于数据库 JSON | [PostgreSQL 仓储](../internal/adapters/postgres/repository.go) `SaveAIProviderConfig` / `LoadAIProviderConfig` | `ai_model_config.config.apiKey` 随数据库及备份持久化。API 响应脱敏不能保护数据库副本。建议密钥管理服务或应用层加密，配套轮换、备份访问控制和恢复说明。 |
| P2 | 本地运维配置默认模式为 `0644`，SMTP 授权码可能被本机其他用户读取 | [本地准备](../scripts/setup-local.sh) 运维配置段；[通知配置](../internal/opscenter/amconfig.go) `auth_password` 写入；[文件仓储](../internal/adapters/observability/files.go) `atomicWrite` | 当父目录可遍历时，文件具有其他用户读取权限。需兼顾 API 写入与 Alertmanager 容器读取，使用共享组和更窄的模式，或独立 secret 文件挂载；不能直接改成容器无权读取的模式。当前只确认代码策略，未读取运行环境秘密。 |

另一个部署边界是默认 Compose 为单节点、消息副本数为 1；这不是本次新缺陷，也不提供跨主机高可用。故障恢复依赖数据卷和独立备份，见 [部署维护](DEPLOYMENT.md)。

## 后续验证入口

已更新部署冒烟断言，并增加“无密钥可以初始化、不可激活工作流”的 Go / Harness 回归用例，**本次未执行**。压测结束后在隔离环境分别验证：

1. Bash 与 PowerShell 的首次配置、旧 Qwen 配置迁移、已有 DeepSeek 密钥保留、其他提供方密钥不挪用、重复运行。
2. 无密钥 API 启动及页面提示；填写密钥后测试、应用、重启恢复；Harness 默认模型与真实请求。
3. 含历史 Qwen 缓存的模型卷只导出嵌入模型、缺失或损坏 blob 拒绝归档；离线安装后知识库索引正常。
4. 分别核对在线/离线 Compose 渲染、镜像构建、目标机安装。语法正确不代表这些步骤已成功。
5. 针对上表另行设计通知故障恢复、普通用户大数据量查询和凭据保护验证；不要复用正在运行的压测环境。
