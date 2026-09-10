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

## 源码版本

Harness 源码不会复制进本仓库。`deploy/deepseek-harness/REVISION` 固定了经过适配的上游提交，脚本会将该提交拉到被 Git 忽略的 `upstream/deepseek-harness`：

```bash
./scripts/fetch-deepseek-harness.sh
```

脚本不会覆盖有本地修改的上游目录。升级时先验证新提交，再更新 `REVISION` 并重新生成 `upstream/deepseek-harness.revision`。

当前锁定的是上游 `dsh-v0.1.2-rc.1`（`a66e4702047846cdaa10c66c9d3df3951f5ea70d`）。侧车使用统一 `@deepseek-ai/dsh/lib/bin.js` 和 `sdk-minimal` profile，再通过 `cordis.yml` overlay 配置拆分后的 `system-prompt`、`agent-loop`、`tools`，并注入 IoT MCP 和只读策略。Docker 构建会按上游 `python/sdk-runtime` 的依赖清单生成独立 Node carrier，并执行真实 SDK、Cordis 与模拟 MCP 的启动握手；源码工作区的开发用软链接不会直接作为生产运行时。

## 业务插件

插件清单位于 `deploy/deepseek-harness/plugins/*.json`。网关启动时校验全部清单，并通过 `GET /v1/plugins` 提供启用插件的公开元数据。管理员管理使用 `GET /v1/plugins/admin` 读取完整清单（含禁用插件、persona 和工具白名单），通过 `POST /v1/plugins` 保存新建或修改，通过 `DELETE /v1/plugins/{id}` 删除自定义插件。内置插件始终只读。平台的聊天工作台和“已配置的工作流插件”清单只展示交互式聊天 Agent；`alarm-handler`、`device-health-inspector`、`protocol-assistant` 由告警、设备巡检和协议接入业务页面调用，不在上述两个界面中显示。

- `ops-assistant`：设备、告警、属性趋势、相似告警和知识库辅助排障（聊天工作台可选）。
- `alarm-handler`：聚焦告警事实核验、影响判断和人工处置建议（告警业务专用，不出现在聊天工作台）。

新增插件时复制一份清单并修改以下字段：

```json
{
  "schemaVersion": 1,
  "id": "my-workflow",
  "name": "我的 AI 插件",
  "description": "面向用户的说明",
  "version": "1.0.0",
  "enabled": true,
  "persona": "严格限定业务角色和禁止事项的系统提示",
  "defaultModel": "qwen3:1.7b",
  "maxTokens": 4096,
  "allowedTools": ["mcp__iot__query_device_latest"]
}
```

`id` 必须唯一。`allowedTools` 只能是部署代码内定义的只读上限集合；即使清单被误改，Harness 的全局单调拒绝 guard 也会拒绝 shell、文件、子 Agent、设备控制和写操作。修改清单后重新构建或重启侧车即可，不需要修改网关代码。

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

流式事件固定为：

- `run.started`
- `text.delta`
- `tool.started`
- `tool.completed`
- `run.completed`
- `run.failed`

Harness 的 reasoning 分片不会发给浏览器；工具事件只包含工具名、调用 ID、状态和安全摘要，不返回完整参数、原始结果或凭据。

## 安全边界

- Go API 与 Harness 的固定内部令牌使用 `X-IOT-Harness-Token`；短期 MCP JWT 单独使用 `Authorization: Bearer ...`，两者不混用。
- 专用 `/mcp/harness` 仅接受 POST，限制请求体大小，并校验 `tokenUse`、Audience、租户和每个工具的精确 scope。
- Harness 组合会禁用 shell、文件系统、skills、jobs、goal、todo 和 subagent 的模型工具面；最终工具执行仍由平台策略 guard 再次限制。
- 每个浏览器会话 ID 都由后端结合租户和用户派生为内部 Session ID，防止跨租户会话碰撞。
- 工具查询有条数上限；所有工具调用写入平台审计仓储。

## 部署与维护

本地、在线和离线分别使用 `.env.local`、`.env.online`、离线包内 `.env.offline`；由对应准备/部署脚本配置并启动 Harness，见 [技术详情](TECHNICAL_DETAILS.md)。不要省略环境文件而误用另一套 Compose 项目。

关键配置为 `IOT_AI_HARNESS_URL`、内部 `IOT_AI_HARNESS_TOKEN`、MCP 回调 `IOT_AI_HARNESS_MCP_URL` 和模型 Provider。容器回调必须能到达 API；无 Harness URL 时不连接侧车。源码版本锁定、独立镜像构建、内部接口及会话持久性见 [侧车开发说明](../deploy/deepseek-harness/README.md)。
