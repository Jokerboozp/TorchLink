# DeepSeek Harness 业务插件运行时

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

管理员可以在 Web 的“AI 模型管理”菜单切换模型服务。平台先通过独立的“测试配置”接口向候选服务发送测试请求；测试不会修改活动配置。用户确认后点击“应用配置”，平台再通过 Eino 检查新地址，并调用 Harness 的 `PUT /v1/provider` 同步模型服务、地址、模型和接口密钥；两条链路成功后才切换活动配置。因此告警研判、聊天、规则草稿、报告和 Harness 工作流使用同一份活动配置。接口密钥不会在响应中返回，重启时从 PostgreSQL 的 `ai_model_config` 活动记录恢复。

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

## Docker 启动

在 `.env` 中配置：

```text
IOT_AI_HARNESS_URL=http://deepseek-harness:8091
IOT_AI_HARNESS_TOKEN=<至少 32 字节随机内部密钥>
IOT_AI_HARNESS_MCP_URL=http://platform-api:8080/mcp/harness
IOT_AI_HARNESS_PROVIDER=ollama
IOT_AI_HARNESS_OLLAMA_BASE_URL=http://ollama:11434/v1
IOT_AI_HARNESS_CONTEXT_WINDOW=8192
IOT_AI_HARNESS_MODEL=qwen3:1.7b
IOT_AI_HARNESS_TIMEOUT=90s
```

然后运行：

```bash
./scripts/fetch-deepseek-harness.sh
docker compose --profile harness up -d --build platform-api deepseek-harness platform-web
docker compose logs -f deepseek-harness platform-api
```

没有配置 `IOT_AI_HARNESS_URL` 时，平台不会连接侧车，原有模型服务和非 Harness 功能仍可使用。
