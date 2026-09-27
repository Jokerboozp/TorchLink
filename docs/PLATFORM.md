# 平台功能与边界

[权限](#权限与设备范围) · [AI](#ai-与知识库) · [运维](#运维中心) · [摄像头](#摄像头)

运行入口见 [部署](DEPLOYMENT.md)，设备报文和外部系统接口见 [接入与协议](INTEGRATION.md)。本页集中维护用户使用流程、授权与生命周期；实现入口可直接沿下列源码定位。

## 权限与设备范围

先在“用户与权限”创建角色，再给用户分配角色。功能权限为已分配角色与用户附加权限的并集；查看不含新增、删除、控制等操作，管理级别包含该功能的全部操作，选择预设不影响其他功能。

设备范围为无设备、指定设备或全部设备。新用户默认继承角色，多个角色范围取并集；用户显式设置的单独范围替代角色范围。历史未设置范围仍为无设备。主设备、子设备分别授权，必须同时有设备管理菜单和对应设备范围。

列表、详情、总数、历史、原文、告警处置、总览、实时事件与 AI 工具都由服务端过滤。全租户的巡检、备份、规则、接入配置、摄像头管理等入口要求全部设备范围及相应功能权限。设备关联不扩大读取或观看权限。

用户归属创建者当前租户；用户名 3–64 位字母/数字/点/横线/下划线，密码 10–72 字节并保存 bcrypt 哈希。内置管理员来自环境配置，不在用户页编辑。每账户 15 分钟内连续 10 次密码错误后锁定 15 分钟；限流按进程保存。

账户、角色、菜单、操作或设备范围变化后，服务端重新计算有效权限；AI 历史按授权版本隔离，旧会话不能恢复已失去权限的数据。普通用户不签发浏览器 MQTT 令牌，每 3 秒通过 `/api/v1/events` 获取当前授权事件，首次快照不重播历史告警，退出后的迟到响应丢弃。内置管理员保留 MQTT 链路。

存储为租户的 `platform_access`。请求范围写入上下文，不修改共享仓库全局状态；内置管理员也绑定当前登录租户。入口：`internal/httpapi/access_control.go`、`device_scope.go`、`user_events.go`、`internal/auth/`。开放密钥与绑定用户权限交集见 [开放接口](INTEGRATION.md#开放接口)。

### EMQX 授权

Compose 使用 HS256 JWT、令牌 ACL、文件规则兜底拒绝与匿名拒绝。平台和 Broker 的 JWT 密钥须一致。认证器须校验 username claim 与连接用户名一致，并开启到期断连；Compose 中 `${username}` 须转义为 `$${username}`。旧动态认证配置可能覆盖文件配置，升级后核对实际认证器。

设备仅可访问自身主题；平台、设备、浏览器和外部视频凭据用途分开。即时撤销需专用 EMQX 管理 API Key，未配置时依赖到期断连。普通用户旧 MQTT 会话需断开或等其旧令牌过期，不能靠隐藏菜单撤销。

管理员告警订阅 `/iot/alarm/{tenant}/#` 与当前 `Alarm.MQTTTopic` 的地理/设备路径结构仍不同，不能宣称已完成租户告警主题验收；不要放宽为全局通配。HTTP 事件接口按租户和设备范围执行。配置与持久接收见 [设备接入](INTEGRATION.md)。

## AI 与知识库

Harness 必装，所有业务模型调用作为工作流执行；Provider 仅负责模型管理的连接测试、健康及配置同步。只有 Gateway 角色可不配 Harness。DeepSeek Key 可首次留空，后续测试并应用；Ollama 只为知识库提供 `nomic-embed-text`，离线安装不代表 AI 断网可用。

| 功能 | 工作流 | 发起身份 | 可用工具 |
| --- | --- | --- | --- |
| 告警自动研判 | `alarm-handler` | 系统身份 `system:alarm-analysis` | 告警列表、属性历史、相似告警 |
| 告警手动研判 | `alarm-handler` | 发起人 | 同上；角色有知识库权限时加 `alarm-handler` 知识 |
| 智能巡检建议 | `device-health-inspector` | 发起人 | 总览、设备状态、告警、属性历史，知识按该 Agent 绑定 |
| 运维报告 | `ops-assistant` | 发起人 | 设备状态、告警、属性历史、相似告警，知识按该 Agent 绑定；不能保存规则草稿 |
| 协议助手（AI 生成） | `protocol-assistant` | 发起人 | 知识按该 Agent 绑定 |
| 规则智能草稿 | `rule-drafter` | 发起人 | 系统总览；只返回草稿 JSON，由平台校验，不保存 |

每次运行的 MCP JWT 绑定租户、Run ID、工作流，工具范围取功能所需与发起人权限的交集。普通用户回调重新读取当前权限和设备范围；系统自动研判身份只允许指定只读工具，不检索知识库。指定设备用户可问答，全租户巡检/报告/Agent 管理仍要求全部设备范围。

知识检索由 Ollama 嵌入与 Weaviate 持久索引提供，按租户及 Agent / `workflowId` 隔离；不支持该范围的索引拒绝查询。绑定控制检索模式、topK、分数和无匹配策略。手动研判是否使用 `alarm-handler` 知识由发起角色决定，自动研判不读取知识库；不同知识权限结果不可串用。

内置 Manifest 只读，自定义聊天 Agent 可管理；业务 Agent 不混入聊天工作台。规则智能草稿不自动启用；聊天中的规则草稿工具只校验并保存禁用草稿，不再调用第二次模型。协议生成结果仍须样例验证和发布。

### 工作流与会话

告警研判与巡检是后台任务，进度持久化到 PostgreSQL；同租户巡检只能一个运行任务，心跳超过 30 秒视为中断，可重新发起。PDF 复用 10 分钟报告，逐台列前 2000 台，其余汇总；同进程最多 2 个渲染，同报告并发共用结果，等待超过 30 秒返回 429。

浏览器 SSE 与内部 NDJSON 均使用 `run.started`、`text.delta`、`tool.started`、`tool.completed`、`run.completed`、`run.failed`。不向浏览器输出 reasoning、工具完整参数/结果或服务凭据。结构化结果由 `internal/aioutput` 校验，错误按失败处理。

Harness 并发与驻留上限以 `IOT_HARNESS_MAX_CONCURRENCY`、`IOT_HARNESS_MAX_CACHED_CONVERSATIONS` 控制，业务遇到 429 退避等待，交互问答不自动重试。自动研判并发单独受 `IOT_AI_ANALYSIS_CONCURRENCY` 控制，恢复/已成功处理的重复告警会跳过。

会话 ID 由租户、用户及浏览器会话派生，同会话 FIFO 执行并复用驻留进程。运行时只持有随机回环代理密钥；每轮 MCP JWT 在代理内更新，不交给子进程。JSONL 留存历史不等于模型冷恢复：当前上下文连续性依赖驻留池，重启创建新会话。会话目录需要按磁盘容量维护。

### Harness 维护

侧车源码在 `deploy/deepseek-harness/`，上游固定于 `REVISION`；构建上下文为仓库根目录：

```bash
bash scripts/fetch-deepseek-harness.sh
docker build -f deploy/deepseek-harness/Dockerfile -t iot-deepseek-harness:local .
node --test deploy/deepseek-harness/gateway.test.mjs
docker run --rm --network none --entrypoint node iot-deepseek-harness:local /harness/examples/iot-ops-agent/runtime-smoke.mjs
```

构建检查模拟模型/MCP、真实 SDK 导入、白名单和最终非 root 用户的依赖权限，不代表真实 Provider 验收。`cordis.yml` 使用 `sdk-minimal`；升级核对版本、提示词和流事件契约。

内部 HTTP 默认 8091，回环 MCP 代理默认 8092；除 `/health` 外需 `X-IOT-Harness-Token`，聊天另带短期 MCP JWT。`/v1/plugins` 返回公开元数据，`/v1/plugins/admin` 返回完整 Manifest，POST/DELETE 管理自定义项，`PUT /v1/provider` 同步模型，`POST /v1/chat/stream` 执行工作流。`mcpUrl` 只能匹配允许的精确 origin 与 `/mcp/harness`，不得含凭据、查询或 fragment。

Manifest 位于 `deploy/deepseek-harness/plugins/`，包含 schemaVersion、id、persona、defaultModel、maxTokens、capabilities 与 allowedTools。maxTokens 为 1–262144 的整数，并收紧到插件上限；Manifest 不能扩大代码白名单。网关、Cordis 与 MCP 服务端共同拒绝 shell、文件系统、jobs、goal、skills、subagent 及设备控制工具，MCP 发现失败即拒绝创建 Agent。

## 运维中心

浏览器只调用平台 `/api/v1/ops/*`。Go 适配 Prometheus、Loki、Grafana、Alertmanager；保留当前 Vue 原生页面，不通过 iframe 或通用 URL 代理访问组件。组件版本与地址见 Compose 和 `internal/config/ops.go`。

运维数据为全平台数据，只允许在 `IOT_OPS_TENANTS` 中授予菜单及对应操作权限；“能看页面”不等于可执行查询、导出、写规则或编辑通知。组件 URL、密码与令牌留在服务端，响应和审计脱敏。

| 页面 | 能力与边界 |
| --- | --- |
| 总览 | 组件与指标组分别加载，某组件异常不阻塞其他组；离开再进入可显示上次结果 |
| 指标 | PromQL、范围查询、模板与规则；默认最多 31 天、500 序列 |
| 日志 | LogQL、游标分页、上下文、导出与实时追踪；默认 7 天、1000 行、导出 5000 行 |
| 仪表盘 | Grafana 数据帧和变量查询，平台布局/编辑/收藏；未支持的面板、转换、重复、注释、库面板不渲染 |
| 告警 | Prometheus/Loki 规则、Alertmanager 静默及通知；历史只含指标告警，Grafana 统一告警关闭 |

API 启动时自动补齐全部内置仪表盘（炬联平台运行、主机资源、服务日志概览），保留已有修改；手动删除的仪表盘会在下次启动时补齐。Grafana 或对应数据源未就绪时后台重试，不阻塞启动。

平台只创建/编辑 Prometheus 和 Loki 数据源，其他类型只读。日志告警仅支持 Loki 本地 ruler 规则；删除由 compactor 异步处理，取消期后不可撤回。通知编辑支持 Webhook 与邮件，无法完整表示的路由只读；其他渠道、时间段、抑制和模板原样保留。

### 配置写入与通知

受管配置遵循结构校验、组件校验、同目录原子替换、确认加载、失败恢复。Prometheus 核对自动 reload 与规则组，Loki 核对 ruler/运行配置哈希，Alertmanager 调用 reload 并在失败时恢复再次加载。缺少共享可写目录即只读，不把文件写入成功当作组件已加载。

受管目录通过 `IOT_OPS_PROMETHEUS_RULES_DIR`、`IOT_OPS_LOKI_RULES_DIR`、`IOT_OPS_LOKI_RUNTIME_FILE`、`IOT_OPS_ALERTMANAGER_CONFIG_FILE` 配置。配置中可能含 SMTP 凭据，保护共享目录和备份。远程依赖无共享目录时不能修改这些文件；本地设置见 [部署](DEPLOYMENT.md#运维组件)。

消防业务告警和监控告警分开。设备邮件由持久通知队列送到 Alertmanager，需配置接收人/路由；未接收的记录重试，回执去重防止重复投递。SMTP 实际送达需单独验证，DeepSeek Key 不影响邮件。细节入口为 `internal/opscenter/device_notifications.go`、`device_email.go` 与 `amconfig.go`。

源码 API 可通过 `IOT_LOG_LOKI_URL` 直推日志，容器由 Alloy 采集；避免重复采集。API info 默认只记失败和慢请求，debug 才记录全部。Prometheus/Loki 的管理退出接口保持关闭；配置加载用各组件规定机制。

## 摄像头

摄像头资料、位置、设备关联与外部视频告警始终可用，不依赖直播；只登记摄像头资料时没有视频，其余功能照常。单摄像头最多关联一台设备，设备可关联多摄像头；直播配置与加密凭据单独保存，不进入资料摘要或日志。

直播默认启用：ZLMediaKit 从 RTSP 拉流或接收 GB28181 设备推送的 RTP，转为 WebRTC/HLS，必要时使用 FFmpeg 转码；视频只经过摄像头、媒体服务和浏览器。API 只管理会话和 SDP，不搬运/转码视频。版本固定在 `deploy/zlmediakit/Dockerfile`，部署入口见 [摄像头部署](DEPLOYMENT.md#摄像头部署)。

模块状态为 `not_deployed`、`misconfigured`、`disabled`、`enabled`、`degraded`，均不影响基础摄像头资料或 API 就绪。全平台业务开关默认开启，内置管理员关闭后保留该选择，关闭会撤销播放与拉流；单摄像头可单独关闭，部署 disable 才移除媒体容器。

### 连接与播放

填写 ONVIF 地址/端口/账号后查询媒体配置，选择码流、连接测试、保存；服务端重新查询流地址，忽略浏览器自选 URL。RTSP 可选品牌模板、NVR 通道或通用地址。测试区分地址、认证、通道、媒体和编码错误；实际收到可解码帧才报告可播放，浏览器真实出画面后更新状态。

### GB28181

平台作为 SIP 服务器（`internal/video/gb28181/`），监听 `IOT_GB28181_SIP_PORT`（默认 5060，UDP 与 TCP）。在摄像头页“国标设备”登记 20 位设备编号、名称、注册密码和媒体传输方式（UDP，或 TCP 由设备连接媒体服务），再把页面显示的 SIP 服务器编号、域、地址端口填到设备的平台接入配置中。设备编号全局唯一且只属于一个租户；未登记、已停用或密码错误的注册返回 403。

- 注册使用 MD5 摘要认证，nonce 由服务端签名并限时，不保存挑战状态；注册有效且 3 分钟内有心跳才算在线。心跳和其他消息只接受已注册设备、同一来源 IP 的请求，否则返回 403 促使设备重新注册。
- 注册成功后查询设备信息与通道目录（兼容 GB2312/GBK 与 UTF-8 报文），目录可手动刷新；直播配置选择设备和通道，未上报目录时可直接填写通道编号。国标接入只有一路码流。
- 播放时在 ZLMediaKit 打开一个 RTP 接收端口（`openRtpServer`，端口取自 `IOT_VIDEO_RTP_PORT_MIN`–`MAX`，默认 30000–30063，按 SSRC 过滤；设备自带 SSRC 时跟随应答更新），向设备发送 INVITE（SDP 媒体地址为 `IOT_GB28181_MEDIA_IP`），ACK 后等待真实帧。之后与 RTSP 源共享同一套会话、宽限、退避、转码与对账；最后观看者离开后 BYE 并关闭端口。
- 设备 BYE、停止推流（RTP 超时 15 秒）或媒体服务重启都会把任务标记为失联，下一次心跳重新 INVITE。API 重启后 SIP 对话不在内存中，对账会关闭没有对话的接收端口并在心跳时重新点播。`on_publish` 只接受平台正在点播的流。
- 暂不支持：录像回放与下载、云台控制、语音对讲、报警订阅、级联上级平台及跨副本共享 SIP 对话；设备报警 MESSAGE 只应答不处理。

H.264 通常只需转协议；H.265 是否支持由浏览器能力探测，不能靠转协议修复不支持的编码。转码需部署允许及摄像头策略允许，受并发/线程上限约束；WebRTC 失败只回退 HLS 一次。没有录像检索、云 SDK 或局域网自动发现。

播放需单独“观看摄像头直播”权限及关联设备范围；未关联摄像头只有全部设备范围且有摄像头菜单的用户可看。HLS 列表/分片逐请求鉴权，WebRTC 信令和媒体 Hook 再校验。权限变化、删除或重新关联摄像头、停用都会撤销会话。

同摄像头首播共享拉流，会话用心跳租约，最后观看者离开后宽限期释放。API 重启对账并清理孤儿，媒体重启由心跳恢复；不能靠无限拉流掩盖播放器失联。参数以 `internal/config/video.go` 为准。

### 目标与凭据边界

仅允许指定 CIDR 与端口，域名解析的全部地址都需通过白名单，并固定已验证 IP，ONVIF 不跟随重定向；云元数据、链路本地、组播和未指定地址拒绝。GB28181 信令的来源地址同样须在 `IOT_VIDEO_ALLOWED_CIDRS` 内，平台只向设备注册时的来源地址发请求。摄像头密码与国标设备注册密码用 AES-256-GCM，附加数据绑定租户与摄像头或设备，只写不读；留空保留，显式清除才删除。

媒体 API/Hook 不对浏览器公开，`on_publish` 仅接受本机转码输出和平台发起点播的 GB28181 流。跨租户、猜测 cameraId/会话号或持有旧播放地址都不构成授权。真实摄像头、NVR、ONVIF、GB28181 设备、网络与编码组合需现场验证，模拟设备和模拟媒体测试不能代替。

### 视频告警 Webhook

```http
POST /api/v1/integrations/video/alarm
Content-Type: application/json
X-Video-Platform-ID: video-platform-1
X-Timestamp: <Unix 秒>
X-Signature: <hex(HMAC-SHA256(secret, timestamp + rawBody))>
```

`timestamp` 使用请求头的原始字符串，直接拼接原始请求体字节计算签名，不插入分隔符。生产环境通过 `IOT_VIDEO_PLATFORM_SECRETS` 和 `IOT_VIDEO_PLATFORM_TENANTS` 将外部平台凭据绑定到租户；时间偏差超过五分钟被拒绝，`cameraId` 必须属于该租户且启用。

消息字段以 [VideoAlarmEvent](../internal/model/model.go) 和 [Webhook 处理器](../internal/httpapi/server.go) 为准。事件按 `eventId` 保存并进入视频告警及跨源融合链路，平台补充摄像头与位置元数据，不经过设备 Raw → Parser 链路，也不接触直播流；视频分析告警详情保留事件所属摄像头，直播可用时可直接观看。

MQTT 视频事件沿用 `/external/video/alarm/{tenantId}/{cameraId}`，需明确 `eventId` 并按平台/摄像头范围授权。传输重试保持同一业务 ID，接收保障见 [MQTT 持久接收](INTEGRATION.md#mqtt-接收保障)。
