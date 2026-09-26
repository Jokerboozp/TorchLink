# 运维中心

运维中心把 Prometheus、Loki、Grafana 与 Alertmanager 的能力做成平台原生页面：指标查询、日志检索、仪表盘、监控告警和通知都在平台内完成，不嵌入组件页面，也不跳转外部地址。组件仍负责存储、查询、规则评估和资源管理；平台负责认证授权、受控的 API 适配、配置文件的安全写入、审计以及统一的中文界面。

运维数据（指标、日志、仪表盘、基础设施告警）属于全平台数据，不能按业务租户或设备隔离，因此只授予全局运维人员。消防业务告警仍在“告警中心”处理，两套告警互不替代。

## 架构

```text
浏览器 ──JWT──> 平台 API /api/v1/ops/*  ──> 权限（菜单 + 操作）─> 审计
                    │
                    ├─ internal/opscenter        业务：查询构造、范围/条数限制、规则与配置写入流程
                    ├─ internal/ports            依赖接口
                    └─ internal/adapters/observability
                          ├─ Prometheus HTTP API（查询、元数据、目标、规则、运行时状态）
                          ├─ Loki HTTP API + WebSocket tail（查询、标签、规则、删除、限制与运行时指标）
                          ├─ Grafana HTTP API（仪表盘、文件夹、数据源、/api/ds/query）
                          ├─ Alertmanager API v2（告警、分组、静默、/-/reload）
                          └─ 受管文件：规则目录、Loki 运行时覆盖文件、alertmanager.yml（原子写入）

采集链路：
  平台与各组件 /metrics ──> Prometheus（scrape）
  node-exporter（主机 CPU、内存、磁盘、网络）──> Prometheus
  容器标准输出 ──> Alloy（按 Compose 项目发现容器）──> Loki
  源码运行的 API ──> 直接推送 Loki（IOT_LOG_LOKI_URL，异步批量，失败不阻塞业务）

告警链路：
  Prometheus 规则（指标）──┐
  Loki ruler 规则（日志）──┴──> Alertmanager ──> 分组 / 抑制 / 静默 ──> Webhook、邮件
```

浏览器只调用平台接口，从不接触组件地址、组件管理员凭据或服务令牌。平台没有“透传任意 URL / 路径 / 请求头”的通用代理：每个接口对应一个固定的组件调用，Grafana 数据源资源只用于变量查询且限制路径。

### 数据归属

| 数据 | 保存位置 | 说明 |
| --- | --- | --- |
| 指标、日志 | Prometheus、Loki | 平台只查询，不复制 |
| 仪表盘、文件夹、数据源 | Grafana | 平台编辑 Grafana JSON，未识别字段原样保留；Grafana 为唯一事实来源 |
| 指标规则、日志规则 | 规则目录中的 YAML 文件 | 平台管理的规则组每组一个文件；部署内置规则只读 |
| 日志保留策略 | Loki 运行时覆盖文件 | 只修改当前 Loki 租户的保留键，其他覆盖项保留 |
| 通知路由、接收人 | `alertmanager.yml` | 以 YAML 节点树编辑，平台不识别的配置原样保留 |
| 静默 | Alertmanager | 通过 API v2 读写 |
| 收藏、保存的查询、查询历史 | 平台 `ops_user_item` 表 | 按租户 + 用户隔离，未配置 PostgreSQL 时仅保存在进程内存 |
| 受管文件历史版本 | `<IOT_DATA_DIR>/ops-state` | 规则组保留最近 20 个版本，停用的规则组保存在这里 |

### 告警职责

| 环节 | 负责组件 | 说明 |
| --- | --- | --- |
| 指标告警评估 | Prometheus | 记录规则与告警规则 |
| 日志告警评估 | Loki ruler | 只支持告警规则；表达式必须是统计查询 |
| 分组、抑制、静默、通知 | Alertmanager | 唯一发送通知的组件 |
| Grafana 告警 | 已停用 | `GF_UNIFIED_ALERTING_ENABLED=false`，避免重复评估和重复通知 |
| 消防业务告警 | 平台告警中心 | 设备上报与业务规则告警；处理状态留在告警中心，可通过本页配置新告警邮件，同一告警持续期间只通知一次 |

## 功能对照

状态说明：**已实测** 指下文 2026-09-25 历史验证记录，不表示本次部署变更重新验收；当时在真实组件上通过接口或浏览器操作验证；**已实现** 表示有代码与单元测试，但未在真实组件上逐项操作；**不支持** 表示明确不提供。

### 总览

| 用户功能 | 组件能力 | 平台接口 | 实现与限制 | 状态 |
| --- | --- | --- | --- | --- |
| 组件连接与版本 | 各组件健康与构建信息接口 | `GET /ops/status`、`/ops/overview` | 并发检查；未配置 / 无法连接 / 部分异常分别显示 | 已实测 |
| 采集目标健康 | Prometheus targets | `/ops/overview`、`/ops/metrics/targets` | 按任务统计，异常任务可跳转查看 | 已实测 |
| 关键指标 | PromQL 即时查询 | `/ops/overview` | 平台链路、备份、主机、监控链路共 15 项；区分未配置、未配置采集目标、采集失败、暂无样本、正常零值 | 已实测 |
| 趋势联动 | PromQL 范围查询 | `/ops/overview/series` | 统一时间范围与自动刷新；悬停同步、拖选放大全部图表 | 已实测 |
| 异常跳转 | — | 页面导航 | 指标跳指标中心，带日志服务的指标跳日志中心 | 已实测（浏览器） |

### 指标中心

| 用户功能 | 组件能力 | 平台接口 | 实现与限制 | 状态 |
| --- | --- | --- | --- | --- |
| 指标搜索与说明 | 元数据、标签 API | `/ops/metrics/catalog`、`labels`、`label-values` | 名称与说明搜索 | 已实测 |
| 结构化浏览 | — | `/ops/metrics/explore` | 仅查看权限即可使用；选择器由标签条件构造，值按字符串字面量转义 | 已实测 |
| PromQL 即时 / 范围查询 | `query`、`query_range` | `POST /ops/metrics/query` | 需要单独权限；步长可指定或自动；最长 31 天；最多 500 条序列并提示截断；超时 30 秒；可取消 | 已实测 |
| 语法校验 | `format_query` | `/ops/metrics/validate` | 输入后自动校验 | 已实测 |
| 采集目标 | targets | `/ops/metrics/targets` | 状态、耗时、错误 | 已实测 |
| 规则查看 | rules | `/ops/metrics/rules` | 部署内置规则只读，平台规则可编辑 | 已实测 |
| 规则新建、编辑、启停、删除 | 自动重新加载配置 | `/ops/metrics/rule-groups` | 校验 → 原子写入 → 等待 Prometheus 确认加载 → 失败恢复原文件；乐观并发（revision） | 已实测（含暂停 Prometheus 触发的超时回滚） |
| 保存的查询、历史 | — | `/ops/preferences/*` | 按账户与租户隔离 | 已实测 |

### 日志中心

| 用户功能 | 组件能力 | 平台接口 | 实现与限制 | 状态 |
| --- | --- | --- | --- | --- |
| 条件筛选 | LogQL | `GET /ops/logs/search` | 服务、级别、标签、关键词 / 正则、排除；查询由结构化条件生成，值转义 | 已实测 |
| LogQL 查询与校验 | `query_range`、`format_query` | `POST /ops/logs/query`、`/validate` | 需要单独权限；统计查询显示图表与表格 | 已实测 |
| 日志量分布 | `count_over_time` | `/ops/logs/volume` | 按级别堆叠，拖选缩小时间范围 | 已实测 |
| 分页、截断提示 | — | 游标参数 | 每页 200 行，单次最多 1000 行，时间范围最长 7 天，超过 24 小时提示 | 已实测 |
| 详情、字段、复制 | — | — | JSON / logfmt 字段拆分；区分流标签与结构化元数据 | 已实测（浏览器） |
| 上下文 | 同一日志流前后查询 | `/ops/logs/context` | 前后各最多 6 小时、100 行 | 已实测 |
| 实时追踪 | WebSocket tail | `GET /ops/logs/tail`（SSE） | 分片合并后渲染；断线退避重连；服务端 10 分钟结束后自动续接；离开页面或修改条件时关闭连接 | 已实测 |
| 导出 | 分页查询 | `POST /ops/logs/export` | JSON Lines / CSV / 纯文本；默认最多 5000 行并提示截断；CSV 防公式注入；审计 | 已实测 |
| 日志告警规则 | Loki ruler（本地规则目录） | `/ops/logs/rule-groups` | Loki 本地规则存储不支持 API 写入，平台写文件并等待 ruler 轮询加载；只接受统计查询 | 已实测 |
| 保留策略 | 运行时覆盖文件 | `GET/PUT /ops/logs/retention` | 租户保留时长与按日志流保留；通过 Loki 运行时配置哈希确认加载，失败恢复 | 已实测 |
| 删除请求 | 删除 API | `/ops/logs/delete-requests` | 必须指定服务或标签；取消期内可撤回；审计 | 已实测（用不匹配任何日志的条件创建后立即取消） |

### 仪表盘

| 用户功能 | 组件能力 | 平台接口 | 实现与限制 | 状态 |
| --- | --- | --- | --- | --- |
| 列表、搜索、标签、文件夹 | search API | `/ops/dashboards`、`/ops/folders` | 支持嵌套文件夹显示；非空文件夹不允许删除 | 已实测 |
| 收藏 | — | `/ops/preferences/favorites` | 按账户保存在平台 | 已实测 |
| 查看、时间范围、刷新、变量 | `/api/ds/query`、数据源资源 | `/panels/:id/data`、`/variables/:name/options` | 面板查询在服务端按已保存的仪表盘执行；变量取值必须在可选范围内；按 Grafana 规则转义变量 | 已实测 |
| 新建、编辑、布局、保存 | 仪表盘保存 API | `POST/PUT /ops/dashboards` | 24 列网格；通过按钮移动与调整尺寸；版本冲突提示重新加载或覆盖 | 已实测（浏览器） |
| 面板查询与显示配置 | — | `POST /ops/dashboards/preview` | 预览需要单独权限；单位、小数、最值、阈值、统计方式、图例等 | 已实测（浏览器） |
| 复制、删除 | — | `/copy`、`DELETE` | 删除需确认并审计 | 已实测（复制）；删除见单元测试 |
| 导入、导出 | — | `/import`、`/export` | 导入先分析数据源映射与支持情况；导出可生成带 `__inputs` 的可共享 JSON；内置 3 个模板 | 已实测 |
| 数据源管理 | 数据源 API | `/ops/datasources` | 只创建 / 编辑 Prometheus 与 Loki；部署配置的数据源只读；密码和请求头值从不返回，编辑时保持 / 替换 / 清除 | 已实测 |
| 支持情况报告 | — | 随仪表盘返回 | 逐个面板、变量说明完整 / 部分 / 不支持的原因 | 已实测 |

面板渲染支持：时序图（含旧版 graph）、统计卡片、仪表、条形仪表、表格、日志、文本（Markdown；HTML 按纯文本显示）、分组行。变量支持：查询、自定义、常量、文本框、时间间隔、数据源。不支持的内容保留在 Grafana 中并在支持情况中列出，平台不声称兼容全部 Grafana 插件。

### 监控告警

| 用户功能 | 组件能力 | 平台接口 | 实现与限制 | 状态 |
| --- | --- | --- | --- | --- |
| 当前告警、分组 | API v2 alerts / groups | `/ops/alerts`、`/ops/alerts/groups` | 标签筛选、接收人、是否含已静默 / 抑制；可跳转表达式 | 已实测 |
| 告警规则状态 | 两个评估器的 rules | `/ops/alerts/rules` | 合并显示；在指标中心 / 日志中心维护 | 已实测 |
| 告警历史 | `ALERTS` 序列 | `/ops/alerts/history` | 只含指标告警，精度为查询步长 | 已实测 |
| 静默 | API v2 silences | `/ops/silences` | 至少一个非空“等于”条件、必须填写原因、最长 366 天；可从告警一键静默；编辑时 Alertmanager 生成新编号 | 已实测 |
| 通知路由与接收人 | 配置文件 + `/-/reload` | `/ops/notifications` | Webhook、邮件；子路由、分组、间隔；凭据保持 / 替换 / 清除；Alertmanager 拒绝时恢复原配置 | 已实测（含 Alertmanager 拒绝后回滚） |
| 测试通知 | — | `/notifications/receivers/:name/test` | 通过专用测试路由只发给指定接收人，需二次确认 | 已实测（隔离的本地 Webhook 接收端） |

## 权限

菜单：`opsOverview` 运维总览、`opsMetrics` 指标中心、`opsLogs` 日志中心、`opsDashboards` 仪表盘、`opsAlerts` 监控告警。菜单权限覆盖对应页面的查看类接口；以下操作需要单独授权：

| 操作 | 权限项 |
| --- | --- |
| PromQL 查询 | `POST /api/v1/ops/metrics/query` |
| LogQL 查询（含 LogQL 实时追踪与导出） | `POST /api/v1/ops/logs/query` |
| 日志导出 | `POST /api/v1/ops/logs/export` |
| 指标 / 日志规则组新建、编辑启停、删除 | `POST`、`PUT`、`DELETE /api/v1/ops/{metrics,logs}/rule-groups[/:name]` |
| 修改保留策略 | `PUT /api/v1/ops/logs/retention` |
| 提交 / 取消日志删除 | `POST /api/v1/ops/logs/delete-requests`、`DELETE …/:id` |
| 仪表盘新建、编辑、删除、复制、导入、预览 | `POST/PUT/DELETE /api/v1/ops/dashboards…` |
| 文件夹新建、重命名、删除 | `POST/PUT/DELETE /api/v1/ops/folders…` |
| 查看数据源连接配置、新建、编辑、删除、测试 | `GET/POST/PUT/DELETE /api/v1/ops/datasources/:uid`、`…/test` |
| 静默新建、编辑、解除 | `POST/PUT/DELETE /api/v1/ops/silences…` |
| 修改通知配置、发送测试通知 | `PUT /api/v1/ops/notifications`、`POST …/receivers/:name/test` |

租户边界：

- `IOT_OPS_TENANTS` 列出可以授予运维权限的租户（运维租户）。其他租户的角色和用户既看不到、也保存不了运维权限；即使数据库中存在，也会在登录与每次请求时被剔除。
- 环境变量配置的内置管理员是平台级运维人员，在其允许的任何租户中都可以使用运维中心。
- 状态接口和个人偏好接口（收藏、保存的查询、历史）对拥有任一运维菜单的账户开放，数据按“租户 + 用户名”隔离，租户和用户名取自令牌，不信任请求体。
- 没有“查看数据源配置”权限时，数据源列表不含地址与连接参数。

审计：规则组（含失败）、保留策略（含失败）、日志删除与取消、日志导出、仪表盘新建 / 修改 / 复制 / 删除 / 导入、文件夹、数据源、静默、通知配置（含失败）与测试通知都会写入审计日志，动作名以 `ops.` 开头。

当前告警页面按完整查询结果统计总数，列表和通知分组均按告警条目分页，默认每页 20 条，可选 50 / 100 条；单个大分组也分批显示。切换筛选或显示方式回到首页，自动刷新保留当前页，数据减少时回退到有效页。分组模式只读取 groups 接口，避免重复拉取 alerts。Alertmanager 仍返回完整匹配快照，分页限制的是浏览器渲染数量；数据量极大时可先按标签或接收人缩小查询范围。

## 设备告警邮件

在“监控告警 → 通知路由”点击“新增设备告警邮件”，填写收件人、发件人、SMTP 服务器、用户名和客户端授权码，并在“设备告警邮件接收人”中选择该接收人，保存并重新加载。选择“关闭设备告警邮件”并保存即可停止接入。此项使用现有运维租户及通知配置权限，覆盖全平台设备，只能配置授权的全局运维邮箱。

163 普通邮箱可以使用 `smtp.163.com:465`（SSL/TLS），SMTP 用户名和发件人为完整邮箱地址。在 163 网页邮箱“设置 → POP/SMTP/IMAP”开启 SMTP 服务后，使用客户端授权码作为 SMTP 密码。平台“使用 163 邮箱配置”按钮填写服务器并从发件人复制用户名；授权码只写服务端受管配置，API 读取时脱敏。不要将授权码写入仓库。服务端须能访问该邮箱服务器。端口 465 的隐式 TLS 由当前 Alertmanager v0.34.1 自动识别，仍校验服务器证书。参考 [SMTP 配置](https://publications.lexmark.com/publications/lexmark_hardware/CX331_MC3224_MC3326/UG/html/sc/configuring-smtp-server-for-e-mail-topic.html)、[客户端授权码操作](https://consumer.huawei.com/cn/support/content/zh-cn15872099/)、[Alertmanager 邮件配置](https://prometheus.io/docs/alerting/latest/configuration/#email_config)。

事件链路为 `报警上报持久化 → iot.alarm.reported → 本地通知队列 → Alertmanager → SMTP`。接入直接上报、业务规则、部件的设备报警上报，不接入摄像头告警；仅持久化触发次数为 1 的新告警进入邮件队列；同一告警在活动或已确认期间持续上报不重复发送，恢复或关闭后再次触发会生成新告警并重新通知。同一报文重试或重放不重复发信。每个通知保留首次上报的 `tenant_id + alarm_id + trigger_id` 作为稳定分组，默认等待 1 秒；邮件标题展示设备、类型和等级，正文包含时间、部件位置、可用的告警描述、规则及关联编号，不转发完整原始报文。默认邮件使用暖白卡片布局，突出设备、告警等级、类型和本次上报时间，正文分为告警内容、可用的位置与来源、记录信息；全部采用内联样式且不依赖外部图片。保存通知配置时会将旧版内置简易模板升级为新版，自定义 HTML 模板保持不变；旧队列通知与渠道测试使用摘要正文布局。

开启边界与接收人一起原子写入 Alertmanager 配置，加载失败一起恢复。普通编辑保留边界，关闭后重新开启或切换接收人重新开始；以首次上报处理时间判断启用边界，启用前已有告警及其后续上报均不补发。平台保留 `torchlink_device_notification` 标签和 `torchlink-device-discard` 接收人：专用路由先匹配设备报警上报，关闭或切换后遗留通知进入丢弃路由，避免落入基础设施默认渠道。

这是新告警首次发生时的通知事件，不是业务状态同步；仅确认告警不会重置通知资格。通知事件存活 24 小时，恢复通知强制关闭，重复通知间隔 48 小时；业务告警的确认、恢复、关闭仍以告警中心为准。既有 Alertmanager 静默、抑制、SMTP 失败及邮箱限流仍可能延迟或阻止投递，“保存成功”或“测试已提交”不能证明邮件已到达收件箱。每次点击“测试”使用独立测试标识并单独分组，避免相同接收人的多次测试被去重；升级后若提示测试路由需要更新，先保存一次通知配置。

队列位于 `IOT_DATA_DIR/ops-state/device-notifications`，进程在返回事件消费成功前写入文件并同步，API 重启后继续读取。上报事件与告警记录在同一数据库事务写入 `event_outbox` 发件箱，引擎提交后立即发布到内部消息队列，失败由后台每 5 秒重试，至少一次投递；通知消费者依据事务内保存的触发次数筛选首次上报，并按其告警与报文编号去重，回执过期或进程重启也不会让持续告警重新发信。未被 Alertmanager 接收的通知每 30 秒重试且不设截止时间；首次接收成功后 24 小时内每分钟刷新同一事件以跨越组件重启，不产生新的分组，之后只保留空的去重回执 30 天，超过 30 天的旧事件不接入。关闭或切换接收人后，未发送的旧通知直接结束，不发往新渠道。必须持久化 API 数据目录和 Alertmanager 数据目录。提交 Alertmanager 不等于收件箱确认，该机制不能保证 SMTP 的严格恰好一次；持续发布失败需检查 API 日志 / Kafka 死信。多实例应保留各实例通知队列，不能在队列尚未送达时删除副本数据卷。

升级启动时，旧队列中同一租户、同一告警的多条待发记录合并为一条，优先保留已提交 Alertmanager 的通知及原指纹，否则保留最早上报；其余记录结束刷新。已提交 Alertmanager 的旧通知可能仍在原 24 小时窗口内完成投递，已经发出的邮件无法撤回。

可选真实邮件链路测试 `TestDeviceMailIntegration` 使用临时 SMTP 接收端验证同一告警持续上报只成信一次、恢复或关闭后再次告警重新成信、详情内容及同一报文重试去重，不向真实邮箱发信。先准备**专用、可丢弃** Alertmanager，将其配置目录挂载为 `/etc/alertmanager` 并绑定独立端口，然后在仓库根目录执行：

```bash
IOT_TEST_ALERTMANAGER_URL=http://127.0.0.1:19094 \
IOT_TEST_ALERTMANAGER_DIR=/absolute/path/to/disposable-config \
IOT_TEST_SMTP_HOST=host.orb.internal \
go test ./internal/opscenter -run TestDeviceMailIntegration -v -count=1
```

测试会覆盖该目录内 `alertmanager.yml`，不得指向运行环境受管目录。`IOT_TEST_SMTP_HOST` 是测试 Alertmanager 可达的 Go 测试进程所在主机地址；测试自动分配临时 SMTP 端口。未设置测试环境变量时默认跳过。

## 配置

API 进程读取以下环境变量；未配置的组件在页面显示“未配置”，相关接口返回 `OPS_NOT_CONFIGURED`。

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `IOT_OPS_PROMETHEUS_URL` / `IOT_OPS_LOKI_URL` / `IOT_OPS_GRAFANA_URL` / `IOT_OPS_ALERTMANAGER_URL` | 空 | 平台服务器可达的组件地址，必须是不含账号密码、查询和片段的 HTTP(S) URL |
| `IOT_OPS_LOKI_TENANT` | 空 | Loki 多租户时的 `X-Scope-OrgID`；单租户部署留空（规则写入 `fake` 租户目录） |
| `IOT_OPS_GRAFANA_TOKEN` | 空 | 推荐使用 Grafana 服务账户令牌（Admin 角色）；设置后优先于用户名密码 |
| `IOT_OPS_GRAFANA_USER` / `IOT_OPS_GRAFANA_PASSWORD` | 空 | 未设置令牌时使用 Basic 认证；Compose 部署默认取 `GRAFANA_ADMIN_*` |
| `IOT_OPS_PROMETHEUS_RULES_DIR` | 空 | 平台管理的 Prometheus 规则目录；为空时规则只读 |
| `IOT_OPS_LOKI_RULES_DIR` | 空 | Loki ruler 本地规则目录下的租户子目录（单租户为 `…/rules/fake`） |
| `IOT_OPS_LOKI_RUNTIME_FILE` | 空 | Loki `runtime_config` 文件；为空时保留策略只读 |
| `IOT_OPS_ALERTMANAGER_CONFIG_FILE` | 空 | Alertmanager 配置文件；为空时通知配置只读 |
| `IOT_OPS_CONFIG_FILE_MODE` | `0640` | 写入 Alertmanager 配置文件的权限；本地共享目录使用 `0644` |
| `IOT_OPS_QUERY_TIMEOUT` / `IOT_OPS_RELOAD_TIMEOUT` | `30s` / `45s` | 单次查询超时；等待组件确认加载新配置的时长 |
| `IOT_OPS_MAX_SERIES` / `IOT_OPS_MAX_LOG_LINES` / `IOT_OPS_MAX_EXPORT_LINES` | `500` / `1000` / `5000` | 返回序列、单次日志行、导出行数上限 |
| `IOT_OPS_MAX_METRIC_RANGE` / `IOT_OPS_MAX_LOG_RANGE` | `744h` / `168h` | 指标、日志查询的最大时间范围 |
| `IOT_OPS_TENANTS` | 空 | 可授予运维权限的租户，逗号分隔 |
| `IOT_LOG_LOKI_URL` / `IOT_LOG_LOKI_TENANT` / `IOT_LOG_SERVICE_NAME` | 空 / 空 / `platform-api` | 源码运行时 API 直接推送自身日志；容器部署留空，由 Alloy 采集 |
| `IOT_LOG_LEVEL` | `info` | API 日志级别（`debug` / `info` / `warn` / `error`）；`info` 只记录失败（4xx 为 info、5xx 为 warn）和超过 3 秒的 HTTP 请求，`debug` 记录全部请求 |

组件版本固定为 Prometheus v3.5.0、Loki 3.5.3、Grafana 12.1.0、Alertmanager v0.34.1、Alloy v1.20.0、node-exporter v1.12.1。升级组件版本前需重新核对所用 API 与本页的写入 / 加载确认机制。

## 受管配置的写入流程

所有写入组件配置的操作都遵循：结构化校验 → 组件侧校验 → 同目录临时文件写入并 `fsync` 后原子替换 → 等待组件确认加载 → 失败时恢复原文件并再次确认。

| 对象 | 组件侧校验 | 加载确认 | 失败处理 |
| --- | --- | --- | --- |
| Prometheus 规则 | `format_query`、模板解析、名称与时长格式 | `--enable-feature=auto-reload-config`（每 5 秒检查）；比较 `lastConfigTime`、`reloadConfigSuccess` 并核对规则组名称 | Prometheus 当前加载失败时拒绝修改；超时或拒绝时恢复原文件 |
| Loki 规则 | `format_query`，并以即时查询确认表达式是统计查询 | ruler 每 10 秒轮询；核对规则组已加载 | 超时恢复原文件 |
| Loki 保留策略 | 时长格式，至少 24 小时；compactor 保留已启用 | `loki_runtime_config_hash` 变化且 `last_reload_successful=1` | 恢复原文件 |
| Alertmanager 配置 | 结构化校验（接收人、路由、邮件服务器等） | 同步调用 `/-/reload` | Alertmanager 拒绝时恢复原文件并再次加载 |

Prometheus 与 Loki 的管理接口（`/-/reload`、`/-/quit` 等）保持关闭；规则由组件自动重新加载。只改动停用规则组时，组件加载的规则不变，不等待重新加载。

## 部署

模型部署已统一改为 DeepSeek API，详见 [部署配置](DEPLOYMENT.md#ai-与工作流)。运维中心组件与 SMTP 通知独立于模型；未填写 DeepSeek 密钥不会阻止设备报警邮件。

在线 / 离线（`compose.yaml`）：

- 新增服务：`ops-init`（一次性创建受管目录与默认文件）、`alertmanager`、`alloy`（只读挂载 Docker 套接字，按 Compose 项目名发现容器）、`node-exporter`（只读挂载宿主机根目录，`pid: host`）。离线打包镜像清单已包含这些镜像。
- 新增数据卷：`prometheus-rules`、`loki-ops`、`alertmanager-config`、`alertmanager-data`、`alloy-data`。`platform-api` 以读写方式挂载受管目录，组件以只读方式挂载。
- Prometheus 端口默认只绑定 `127.0.0.1:9090`（`PROMETHEUS_BIND_ADDRESS`、`PROMETHEUS_PORT` 可改）；Loki、Alertmanager 只在容器网络中。Grafana 端口保持原有配置并启用认证。
- Grafana 统一告警已关闭；已有部署若在 Grafana 中配置过告警规则，升级前需迁移到 Prometheus / Loki 规则，否则这些规则不再评估。
- 在环境文件中设置 `IOT_OPS_TENANTS`（例如运维专用租户），重跑部署脚本后在“用户与权限”中授予运维菜单与操作。

本地（`compose.local.yaml`，Profile `ops`）：

```bash
bash ./scripts/setup-local.sh --include-ops
```

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\setup-local.ps1 -IncludeOps
```

端口：Prometheus `19090`、Loki `13100`、Grafana `13000`、Alertmanager `19093`，均默认只绑定本机。受管文件放在仓库 `data/ops/`（`IOT_LOCAL_OPS_DIR` 可改），Prometheus 通过 `IOT_LOCAL_API_HOST` 抓取源码运行的 API 与备份服务，API 日志经 `IOT_LOG_LOKI_URL` 直接推送。依赖运行在另一台主机时，规则、保留策略和通知配置在运维中心只读。

查看与停止沿用 [部署维护](DEPLOYMENT.md#查看状态日志与停止) 中的命令，本地加 `--profile ops`。

### 日志量

Alloy 采集 Compose 项目内全部容器的标准输出，组件自身的日志也会进入 Loki。为避免空闲时日志持续增长和“查询越多日志越多”：

- Loki、Alloy 的日志级别设为 `warn`。Loki 在 `info` 级别为每次查询及其按时间拆分的子查询写多行日志（实测一次 24 小时的日志检索加日志量统计约 220 行），这些日志又被采回 Loki。
- API 默认只记录失败和慢请求（见 `IOT_LOG_LEVEL`），Prometheus 抓取、页面轮询等成功请求不再写日志；`platform-web` 的 nginx 只记录非 2xx / 3xx 请求。
- 实时追踪结束时平台以正常关闭帧断开 Loki WebSocket，Loki 不再为每次停止追踪记录客户端异常。

已有部署需重跑部署脚本（或 `docker compose up -d`）使新的组件配置生效。定位仍然偏多的来源，可在日志中心 LogQL 中执行：

```logql
sum by (service_name) (count_over_time({service_name=~".+"}[5m]))
```

## 验证记录

2026-09-25，开发容器（Linux x86_64，Docker Engine + Compose v5.1.1），组件由 `compose.yaml` 启动（附加仅用于本机端口映射的覆盖文件），平台 API 以源码构建在宿主机运行，前端为 Vite 开发服务器。

| 类型 | 内容 | 结论 |
| --- | --- | --- |
| 单元测试 | `go test ./cmd/... ./internal/...`；`iot_front` 下 `npm test`、`npm run build` | 通过 |
| 真实组件（接口） | 总览、PromQL 查询与 500 序列截断、校验错误、规则组新建 / 停用 / 版本冲突 / 删除；暂停 Prometheus 容器触发的超时回滚；Loki 规则加载、日志规则告警触发并经 Alertmanager 送达；非统计查询被拒绝；日志检索、游标分页、日志量、上下文、导出截断、SSE 实时追踪；保留策略写入与加载确认；删除请求创建与取消；模板导入、UID 冲突、变量选项与非法取值拒绝、面板数据、保存与版本冲突、复制、收藏、导出；数据源新建（请求头凭据）、测试、保持凭据编辑、部署数据源只读、删除；静默新建 / 编辑 / 解除；通知接收人与路由保存、Alertmanager 拒绝后回滚、测试通知 | 通过 |
| 通知 | 仅使用同一 Docker 网络内的临时本地 Webhook 接收端，并用 `e2e="true"` 路由隔离 | 已送达告警与测试通知；未向任何真实人员或外部渠道发送 |
| 权限 | 运维租户中的只读运维角色：可查看各页面；PromQL、LogQL、导出、规则、保留、静默、通知、数据源详情、预览、删除仪表盘均返回 403；业务租户的角色不能保存运维权限 | 通过 |
| 浏览器 | Chromium（Playwright）桌面 1440×900 与手机 390×844：五个页面无页面错误、无横向溢出；仪表盘查看、面板编辑预览、新增统计卡片与保存；日志筛选、上下文、LogQL、实时追踪、保留策略；从告警新建与解除静默、历史、通知路由；只读账户界面不显示无权操作 | 通过 |
| 部署脚本 | `bash scripts/tests/deployment-smoke.sh <docker-compose>`（真实 Compose 解析、模拟 Docker 操作）；Compose 2.24.4 与 5.1.1 渲染项目名替换 | 通过；未在目标服务器部署，未验证离线包在目标机的实际运行 |

未验证：PowerShell 脚本（环境无 `pwsh`）、邮件渠道真实发送、多副本 API、Loki 多租户、Grafana 服务账户令牌方式、ARM64 镜像。

2026-09-26，macOS arm64，Docker 中运行 Loki 3.5.3、Alloy v1.20.0、Prometheus v3.5.0（配置取自 `compose.local.yaml`），宿主机源码运行 API（内存存储），日志生成容器约 400 行/秒；前端为 Vite 开发服务器，内置浏览器 1440×900。

| 类型 | 内容 | 结论 |
| --- | --- | --- |
| 日志量 | 同一次 24 小时日志检索 + 日志量统计：Loki 自身日志 220 行 → 2 行；Prometheus 抓取与成功的运维接口请求不再产生 API 日志，422 请求仍记录；停止实时追踪时 Loki 只剩 1 行内部取消日志 | 通过 |
| 浏览器 | 日志中心加载 2190 行后：展开一行 74 → 15 ms，切换自动换行 130 → 15 ms，关键词框每次输入 78 → 约 10 ms（脚本耗时，本机性能较高，普通办公电脑成比例更长）；查询后关键词高亮、展开详情正常 | 通过 |

未验证：该次未启动 Grafana、ClickHouse、Redpanda 等其他容器，其默认日志量未实测；未在目标服务器部署。

## 限制

- Grafana 面板：未实现的面板类型、数据转换、字段覆盖、按变量重复面板、注释查询、库面板不在平台渲染；布局通过按钮调整，不支持拖拽。
- 数据源：平台只创建和编辑 Prometheus、Loki；其他类型只读显示。
- 日志告警：只支持 Loki ruler 本地规则目录（单机 Loki）；Loki 记录规则需要远程写入，平台不支持。告警历史只含指标告警。
- 通知：平台编辑 Webhook 与邮件；其他渠道、`time_intervals`、抑制规则、模板原样保留并只读显示。存在平台无法安全表示的路由配置时，通知路由整体只读。
- 删除日志由 Loki compactor 异步执行，取消期后无法撤回；保留策略按天删除，实际清理滞后一个 compactor 周期。
- 查询限制：指标最长 31 天、500 条序列；日志最长 7 天、单次 1000 行、导出 5000 行；这些值可通过环境变量调整。
- 受管文件依赖 API 与组件共享卷；拆分部署时需保证 API 所在主机能写入组件读取的目录，否则相关功能为只读。
- 保存的查询与收藏在未配置 PostgreSQL 时仅保存在进程内存，重启后丢失。
