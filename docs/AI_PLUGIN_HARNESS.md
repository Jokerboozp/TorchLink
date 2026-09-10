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

Eino/Provider 链路负责告警自动分析和规则草稿；Harness 负责可追踪、可选插件的交互式工作流。在线和离线部署默认把两条链路都指向 Ollama 的 `qwen3:1.7b`，因此所有 AI 功能共用同一个本地模型。

模型测试、切换及持久化配置见 [部署维护](DEPLOYMENT.md#在界面切换-ai-模型服务)。Harness 健康与模型 Provider 可用性需分别核对。

## 知识检索

知识文档在知识库页面上传与管理，保留租户及 Agent / `workflowId` 归属。业务工作流按授权范围检索；无法执行范围隔离时拒绝检索，不回退到全库。嵌入模型与向量存储配置见部署文档。

## 工作流与任务

| 工作流 | 入口与用途 |
| --- | --- |
| `ops-assistant` | 聊天工作台：设备、告警、趋势与知识辅助排障 |
| `alarm-handler` | 告警业务：核验事实、判断影响并给出处置建议 |
| `device-health-inspector` | 智能巡检：设备健康与异常分析 |
| `protocol-assistant` | 协议助手：解释资料与点表，保存映射草稿 |

只有交互式聊天 Agent 出现在聊天工作台。告警研判与智能巡检返回后台任务，重新打开页面可读取进度和结果；预计剩余时间是估算值。协议草稿仍须通过源码校验与发布。

内置 Manifest 只读，自定义聊天 Agent 通过管理员接口创建、编辑或停用，知识范围统一在知识库设置。新增 Agent 的工具白名单只能选取平台允许的只读工具，不能扩大权限。Manifest 格式、上游版本和内部接口集中在 [侧车开发说明](../deploy/deepseek-harness/README.md)。

## 请求与事件协议

Go API 暴露：

- `GET /api/v1/ai/workflows`：列出聊天工作台可选的已启用插件；业务专用工作流不返回。
- `GET /api/v1/ai/workflows/admin`：管理员读取聊天 Agent 管理清单；业务专用工作流不返回。
- `POST /api/v1/ai/workflows`：管理员创建动态 Agent。
- `PUT /api/v1/ai/workflows/{id}`：管理员编辑或启用/禁用动态 Agent。
- `DELETE /api/v1/ai/workflows/{id}`：管理员删除动态 Agent。
- `POST /api/v1/ai/chat`：兼容的非流式调用；未配置 Harness 时回退到原有本地助手。
- `POST /api/v1/ai/chat/stream`：SSE 流式运行插件。
- `GET /api/v1/ai/providers/config`：读取当前模型服务（管理员可看到地址和脱敏接口密钥提示）。
- `POST /api/v1/ai/providers/test`：管理员测试候选模型服务；只发送测试请求，不修改活动配置。
- `PUT /api/v1/ai/providers/config`：管理员应用已测试的模型服务；支持 `ollama`、`deepseek` 和 `openai-compatible`。
- `POST /api/v1/ai/health-inspection/run`：创建智能巡检任务并立即返回进度。
- `GET /api/v1/ai/health-inspection/progress`：按当前租户读取智能巡检任务，重新打开页面时无需保存 job ID。
- `GET /api/v1/ai/health-inspection/progress/{jobId}`：读取指定智能巡检任务的进度、预计剩余时间和完成报告。
- `POST /api/v1/ai/alarm-analysis/{alarmId}/run`：创建告警研判任务并立即返回任务进度。
- `GET /api/v1/ai/alarm-analysis/{alarmId}/progress`：按告警读取当前或最近一次研判任务，重新打开详情时无需保存 job ID。
- `GET /api/v1/ai/alarm-analysis/{alarmId}/progress/{jobId}`：读取进度、阶段、预计剩余时间和完成后的分析结果。

浏览器只提交 `workflowId`、`conversationId`、`question` 和可选的 `maxTokens`。每次运行由 Go API 生成 Run ID，并签发有效期两分钟、绑定租户、用户、Run ID、Audience 和只读 scopes 的 MCP JWT。浏览器拿不到该令牌。

流式事件为 `run.started`、`text.delta`、`tool.started`、`tool.completed`、`run.completed` 和 `run.failed`。reasoning 分片不会发给浏览器，工具事件仅提供名称、调用 ID、状态和安全摘要，不返回完整参数、原始结果或凭据。

## 安全边界

- Go API 与 Harness 的固定内部令牌使用 `X-IOT-Harness-Token`；短期 MCP JWT 单独使用 `Authorization: Bearer ...`，两者不混用。
- 专用 `/mcp/harness` 仅接受 POST，限制请求体大小，并校验 `tokenUse`、Audience、租户和每个工具的精确 scope。
- Harness 组合会禁用 shell、文件系统、skills、jobs、goal、todo 和 subagent 的模型工具面；最终工具执行仍由平台策略 guard 再次限制。
- 每个浏览器会话 ID 都由后端结合租户和用户派生为内部 Session ID，防止跨租户会话碰撞。
- 工具查询有条数上限；所有工具调用写入平台审计仓储。

## 部署与维护

本地、在线和离线分别使用 `.env.local`、`.env.online`、离线包内 `.env.offline`；由对应准备/部署脚本配置并启动 Harness，见 [技术详情](TECHNICAL_DETAILS.md)。不要省略环境文件而误用另一套 Compose 项目。

关键配置为 `IOT_AI_HARNESS_URL`、内部 `IOT_AI_HARNESS_TOKEN`、MCP 回调 `IOT_AI_HARNESS_MCP_URL` 和模型 Provider。容器回调必须能到达 API；无 Harness URL 时不连接侧车。源码版本锁定、独立镜像构建、内部接口及会话持久性见 [侧车开发说明](../deploy/deepseek-harness/README.md)。
