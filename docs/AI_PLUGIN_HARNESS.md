# AI 工作流与知识库

平台把“模型 Provider”和“业务 AI 插件”拆成两层：Provider 只负责模型调用，业务插件负责角色提示、可见工具和执行边界。因此 AI 运维助手、AI 告警研判等能力可以独立增加、停用或切换，不需要修改告警核心逻辑。

```text
Web AI 工作台
  -> Go API（登录用户、租户隔离、SSE、审计）
    -> DeepSeek Harness 源码网关
      -> 业务插件 manifest
        -> DeepSeek Harness agent runtime
          -> 租户绑定的只读 MCP 工具
```

Harness 是必装组件，所有使用模型的业务功能都作为 Harness 工作流运行，共用同一套 Agent 角色、工具白名单、MCP 权限校验和运行记录；只有接入网关角色（`IOT_PROCESS_ROLE=gateway`）不需要 Harness，其余角色未配置 `IOT_AI_HARNESS_URL` 时拒绝启动。在线和离线部署默认使用 Ollama 的 `qwen3:1.7b`。

| 功能 | 工作流 | 发起身份 | 可用工具 |
| --- | --- | --- | --- |
| 告警自动研判 | `alarm-handler` | 系统身份 `system:alarm-analysis` | 告警列表、属性历史、相似告警 |
| 告警手动研判 | `alarm-handler` | 发起人 | 同上；角色有知识库权限时加 `alarm-handler` 知识 |
| 智能巡检建议 | `device-health-inspector` | 发起人 | 总览、设备状态、告警、属性历史，知识按该 Agent 绑定 |
| 运维报告 | `ops-assistant` | 发起人 | 设备状态、告警、属性历史、相似告警，知识按该 Agent 绑定；不能保存规则草稿 |
| 协议助手（AI 生成） | `protocol-assistant` | 发起人 | 知识按该 Agent 绑定 |
| 规则智能草稿 | `rule-drafter` | 发起人 | 系统总览；只返回草稿 JSON，由平台校验，不保存 |

告警研判对温度、烟雾、水压、电压、电流、燃气每个属性只查询一次最近 24 小时（最多 1000 条），压缩为最新值、最近 10 分钟至多 30 个点及 1 小时 / 24 小时统计后写入提示；巡检建议只列出按严重程度排序的前 40 台需关注设备，其余设备以计数表示，页面报告仍包含全部设备。平台先在 Go 侧准备核实过的上下文（告警、历史、巡检快照、报表数据等）写入提示，再由工作流补充查询并按规定格式输出；告警研判和规则草稿的结构化结果由 `internal/aioutput` 解析，格式无效时按失败处理。每次运行签发五分钟有效、绑定租户、Run ID 和工作流（`workflow` 声明）的 MCP 令牌，工具范围取“该功能所需”与“发起人可用”的交集。浏览器用户的令牌保留登录版本，MCP 每次调用重新读取其最新权限与设备范围，并按该功能的操作权限（而不是智能助手问答权限）放行；系统身份不属于任何用户，只能使用上表列出的工具，也不能检索知识库。

容量与限制：每次业务运行都会在侧车中启动一个独立会话，受 `IOT_HARNESS_MAX_CONCURRENCY`（默认 4）与 `IOT_HARNESS_MAX_CACHED_CONVERSATIONS`（默认 32）限制，API 等待上限为 `IOT_AI_HARNESS_TIMEOUT`（默认 90 秒）；告警集中爆发时自动研判会排队或失败并留下可读的失败记录。侧车 `/data/sessions` 与运行时目录中的会话记录目前不会自动清理，需要按磁盘容量定期维护。

模型 Provider 仍负责“模型管理”页的连接测试、健康检查和配置同步：应用新配置时同步到 Harness，业务功能实际调用的是 Harness 中的同一模型。模型测试、切换及持久化配置见 [部署维护](DEPLOYMENT.md#在界面切换-ai-模型服务)。Provider 连接正常不代表 Harness 工作流可用，反之亦然，排查时两者分别核对。

也可在独立 Dify 工作区使用五个 IoT Workflow/Chatflow、两个原生 Agent 对比版及五个可复用 Skills，部署与边界见 [Dify 接入说明](../deploy/dify/README.md)。Dify 自行执行模型规划与对话，复用平台业务工具及 Agent 知识绑定；这是额外入口，平台页面和后台自动研判仍沿用上述链路。

## 知识检索

知识文档在知识库页面上传与管理，保留租户及 Agent / `workflowId` 归属。业务工作流按授权范围检索；无法执行范围隔离时拒绝检索，不回退到全库。嵌入模型与向量存储配置见部署文档。

## 工作流与任务

| 工作流 | 入口与用途 |
| --- | --- |
| `ops-assistant` | 聊天工作台：设备、告警、趋势与知识辅助排障 |
| `alarm-handler` | 告警业务：核验事实、判断影响并给出处置建议 |
| `device-health-inspector` | 智能巡检：设备健康与异常分析 |
| `protocol-assistant` | 协议助手：解释资料，生成报文字段映射；JSON 报文与 Modbus 点表可无需 AI 直接生成协议 |

只有交互式聊天 Agent 出现在聊天工作台；`alarm-handler`、`device-health-inspector`、`protocol-assistant`、`rule-drafter` 为业务专用工作流。`create_rule_draft` 工具不再调用第二次模型：聊天 Agent 按工具说明自行写出规则 JSON 作为 `ruleJson` 传入，工具只归一化、校验并保存禁用草稿。告警研判与智能巡检返回后台任务，重新打开页面可读取进度和结果；预计剩余时间是估算值。生成的字段映射须通过真实样本预览并发布；专用 Go 协议须通过源码编译及样例校验后发布。

内置 Manifest 只读，自定义聊天 Agent 通过管理员接口创建、编辑或停用，知识范围统一在知识库设置。新增 Agent 的工具白名单只能选取平台允许的只读工具，不能扩大权限。Manifest 格式、上游版本和内部接口集中在 [侧车开发说明](../deploy/deepseek-harness/README.md)。

## 请求与事件协议

Go API 暴露：

- `GET /api/v1/ai/workflows`：列出聊天工作台可选的已启用插件；业务专用工作流不返回。带 `purpose=knowledge` 时额外返回 `alarm-handler`，供知识库页上传告警研判文档和配置检索策略；未配置 Harness 时也返回该项。
- `GET /api/v1/ai/workflows/admin`：管理员读取聊天 Agent 管理清单；业务专用工作流不返回。
- `POST /api/v1/ai/workflows`：管理员创建动态 Agent。
- `PUT /api/v1/ai/workflows/{id}`：管理员编辑或启用/禁用动态 Agent。
- `DELETE /api/v1/ai/workflows/{id}`：管理员删除动态 Agent。
- `POST /api/v1/ai/chat`：兼容的非流式调用；Harness 不可用时返回 503，不再提供绕过 Harness 的本地助手。
- `POST /api/v1/ai/chat/stream`：SSE 流式运行插件。
- `GET /api/v1/ai/providers/config`：读取当前模型服务（管理员可看到地址和脱敏接口密钥提示）。
- `POST /api/v1/ai/providers/test`：管理员测试候选模型服务，可直接填写任意平台可达的 HTTP/HTTPS 地址，无需地址白名单；只发送测试请求，不修改活动配置。测试与应用统一校验根地址格式，密钥使用独立字段。
- `PUT /api/v1/ai/providers/config`：管理员应用已测试的模型服务；支持 `ollama`、`deepseek` 和 `openai-compatible`。
- `POST /api/v1/ai/health-inspection/run`：创建智能巡检任务并立即返回进度。
- `GET /api/v1/ai/health-inspection/progress`：按当前租户读取智能巡检任务，重新打开页面时无需保存 job ID。
- `GET /api/v1/ai/health-inspection/progress/{jobId}`：读取指定智能巡检任务的进度、预计剩余时间和完成报告。

智能巡检任务的进度和报告保存在数据库表 `health_inspection_job`（内存仓储模式下保存在进程内），API 重启后和多个 API 副本之间都能读取同一任务；每个租户同时最多一个运行中的任务，由数据库唯一索引保证。巡检本身仍在发起任务的进程内执行：运行中的任务约每 0.5 秒更新一次心跳，超过 30 秒未更新即视为该进程已停止，读取时标记为“已中断”，可重新开始。巡检报告 PDF 复用 10 分钟内最近一次完成的报告，任意副本均可下载。历史任务记录目前不自动清理。

告警研判任务的进度仍只保存在发起任务的进程内：研判结果本身已持久保存，但进程重启后进行中的任务进度会丢失，另一副本也读不到该进度。
- `POST /api/v1/ai/alarm-analysis/{alarmId}/run`：创建告警研判任务并立即返回任务进度。
- `GET /api/v1/ai/alarm-analysis/{alarmId}/progress`：按告警读取当前或最近一次研判任务，重新打开详情时无需保存 job ID。
- `GET /api/v1/ai/alarm-analysis/{alarmId}/progress/{jobId}`：读取进度、阶段、预计剩余时间和完成后的分析结果。
- `GET /api/v1/ai/alarm-analysis/{alarmId}`：返回当前角色可见的最新研判结果，`knowledgeScope` 与 `knowledgeDocuments` 标明所用知识范围和来源文档。

### 告警研判的知识范围

告警研判只检索 `alarm-handler` 智能体名下的文档，并遵循该智能体在知识库页保存的检索策略（禁用、召回数量、最低相关度、无命中时是否必须有证据）；索引不支持按智能体过滤时直接失败，不回退为全租户检索。是否检索由角色决定，规则与聊天 Agent 相同：角色拥有知识库菜单（`menu:knowledge`）才可使用知识库。

| 触发方式 | 是否检索知识 | 结果保存 | 可见范围 |
| --- | --- | --- | --- |
| 告警事件自动研判（无具体用户） | 否 | `knowledgeScope` 为空 | 能查看该告警的所有角色 |
| 无知识库权限的角色手动研判 | 否 | `knowledgeScope` 为空，覆盖上一份不含知识的结果 | 同上 |
| 有知识库权限的角色手动研判 | 是 | `knowledgeScope=alarm-handler`，单独保存 | 仅有知识库权限的角色 |

同一告警两份结果并存（表 `alarm_ai_analysis` 主键为租户、告警和知识范围）；有知识库权限的角色读取两者中较新的一份。引用知识的结果不广播到告警实时主题，研判任务和进度也按知识范围分开，只有同一范围的角色能读取。升级前保存的结果曾检索全租户知识库，迁移时标记为 `legacy-tenant-knowledge`，同样只对有知识库权限的角色可见；无知识库权限的角色在下一次自动研判或自行手动研判前看不到这些告警的旧结果。

浏览器只提交 `workflowId`、`conversationId`、`question` 和可选的 `maxTokens`。每次运行由 Go API 生成 Run ID，并签发有效期两分钟、绑定租户、用户、Run ID、Audience 和只读 scopes 的 MCP JWT。浏览器拿不到该令牌。

流式事件为 `run.started`、`text.delta`、`tool.started`、`tool.completed`、`run.completed` 和 `run.failed`。reasoning 分片不会发给浏览器，工具事件仅提供名称、调用 ID、状态和安全摘要，不返回完整参数、原始结果或凭据。

## 用户数据范围

普通用户使用智能助手需对应菜单及问答操作授权。短期 MCP JWT 保留普通用户标记及登录版本；工具回调重新读取有效权限，收紧签发时的工具 scopes，并将当前设备范围写入请求上下文。设备状态、属性历史、告警及总览均按此范围读取，不能通过工具参数扩大到其他设备或租户。总览按菜单权限隐藏无权读取的业务统计；知识库及规则草稿分别要求相应菜单和操作权限。指定设备用户可以问答，但巡检、全租户报告和 Agent 管理继续要求全部设备范围。

权限变更后，浏览器历史缓存和 Harness 会话 ID 按服务端 `accessVersion` 隔离，避免复用之前权限下的回答及模型上下文。具体授权规则见 [用户权限](USER_ACCESS_CONTROL.md)。

单条告警详情及告警研判接口同时检查该告警所属设备范围及相应菜单/操作权限。模型管理、知识库的配置权限仍独立分配，不能因为角色名称为 operator 就推断所有操作可用。规则、设备范围及权限升级见 [用户权限](USER_ACCESS_CONTROL.md)。

## 安全边界

- Go API 与 Harness 的固定内部令牌使用 `X-IOT-Harness-Token`；短期 MCP JWT 单独使用 `Authorization: Bearer ...`，两者不混用。
- 专用 `/mcp/harness` 仅接受 POST，限制请求体大小，并校验 `tokenUse`、Audience、租户和每个工具的精确 scope。
- Harness 组合会禁用 shell、文件系统、skills、jobs、goal、todo 和 subagent 的模型工具面；最终工具执行仍由平台策略 guard 再次限制。
- 每个浏览器会话 ID 都由后端结合租户、用户及普通用户的权限版本派生为内部 Session ID，防止跨租户会话碰撞。
- 工具查询有条数上限；所有工具调用写入平台审计仓储。

## 部署与维护

本地、在线和离线分别使用 `.env.local`、`.env.online`、离线包内 `.env.offline`；由对应准备/部署脚本配置并启动 Harness，见 [技术详情](TECHNICAL_DETAILS.md)。不要省略环境文件而误用另一套 Compose 项目。

关键配置为 `IOT_AI_HARNESS_URL`、内部 `IOT_AI_HARNESS_TOKEN`、MCP 回调 `IOT_AI_HARNESS_MCP_URL` 和模型 Provider。容器回调必须能到达 API；无 Harness URL 时不连接侧车。源码版本锁定、独立镜像构建、内部接口及会话持久性见 [侧车开发说明](../deploy/deepseek-harness/README.md)。
