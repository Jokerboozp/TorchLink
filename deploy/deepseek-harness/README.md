# DeepSeek Harness 侧车开发

本目录将官方 DeepSeek Harness JSON-RPC 运行时封装为基于 Manifest 的 IoT 工作流服务。业务入口见仓库 `docs/AI_PLUGIN_HARNESS.md`；本文说明镜像、内部接口及会话机制。

## 构建与检查

上游源码由 `scripts/fetch-deepseek-harness.sh` 获取到 Git 忽略的 `upstream/deepseek-harness`，版本以本目录 `REVISION` 为准。构建上下文必须是仓库根目录：

```bash
bash ./scripts/fetch-deepseek-harness.sh
docker build -f deploy/deepseek-harness/Dockerfile -t iot-deepseek-harness:local .
node --test deploy/deepseek-harness/gateway.test.mjs
```

Dockerfile 校验上游版本标记，用固定 pnpm 版本和 `--frozen-lockfile` 安装依赖，再禁用网络执行上游构建；运行载体按 `python/sdk-runtime` 的依赖清单生成。镜像构建还执行网关测试、真实 SDK / Cordis 导入和模拟 MCP 启动握手。独立网关测试注入协议兼容的模拟运行时，无需先构建 Harness。

运行时使用 `@deepseek-ai/dsh/lib/bin.js` 的 `sdk-minimal` profile，应用本目录 `cordis.yml`。每个驻留会话有独立 `DSH_HOME`。升级时同步核对 `REVISION`、Dockerfile 构建参数和上游版本标记，再执行构建与检查。

## 服务配置

使用仓库在线/本地/离线部署入口启动侧车，避免手工创建另一套网络或凭据。对 Go API 开放的 HTTP 端口默认 `8091`；内部 MCP 凭据代理仅监听容器回环地址 `127.0.0.1:8092`，与独立备份服务端口不是同一监听器。

| 变量 | 默认值 / 约束 | 用途 |
| --- | --- | --- |
| `IOT_HARNESS_GATEWAY_TOKEN` | 至少 32 字符 | Go API 到侧车的内部令牌 |
| `IOT_HARNESS_PORT` | `8091` | HTTP 端口 |
| `IOT_HARNESS_MCP_PROXY_PORT` | `8092` | 回环 MCP 代理 |
| `IOT_HARNESS_MCP_ALLOWED_ORIGINS` | `http://platform-api:8080` | 允许的精确回调 origin，逗号分隔 |
| `IOT_HARNESS_SESSION_ROOT` | `/data/sessions` | JSONL 会话记录 |
| `IOT_HARNESS_HOME` | `/data/runtime-home` | 每个会话的运行时状态目录 |
| `IOT_HARNESS_WORKSPACE` | `/data/workspace` | 工作目录 |
| `IOT_HARNESS_CONVERSATION_TTL_MS` | `600000` | 空闲驻留期限 |
| `IOT_HARNESS_MAX_CACHED_CONVERSATIONS` | `32` | 驻留会话上限 |
| `IOT_HARNESS_MAX_CONCURRENCY` | `4` | 跨会话运行并发 |
| `IOT_HARNESS_RUN_TIMEOUT_MS` | `180000` | 单轮运行期限 |

Compose 将平台侧 `IOT_AI_HARNESS_*` 配置映射到侧车变量；API 等待超时和侧车运行超时是不同设置。配置文件与 `/data` 按部署环境持久保存。

## 内部 HTTP 契约

| 接口 | 用途 |
| --- | --- |
| `GET /health` | 容器健康检查，无需令牌 |
| `GET /v1/plugins` | 已启用插件公开元数据，不含 persona 和工具白名单 |
| `GET /v1/plugins/admin` | 完整 Manifest，含停用项 |
| `POST /v1/plugins` | 创建或修改自定义插件 |
| `DELETE /v1/plugins/{id}` | 删除自定义插件；内置项只读 |
| `PUT /v1/provider` | 同步模型 Provider 配置 |
| `POST /v1/chat/stream` | 流式运行工作流 |

除健康检查外使用 `X-IOT-Harness-Token`。聊天请求另带 `Authorization: Bearer <短期 MCP JWT>`；该 JWT 由 Go API 生成，不应由浏览器调用侧车。

聊天 JSON 示例：

```json
{
  "runId":"run-001",
  "conversationId":"tenant-user-namespaced-hash",
  "workflowId":"ops-assistant",
  "question":"当前有哪些高等级活动告警？",
  "mcpUrl":"http://platform-api:8080/mcp/harness",
  "model":"qwen3:1.7b",
  "maxTokens":1200
}
```

`mcpUrl` 必须匹配允许的 origin，路径精确为 `/mcp/harness`，不得含凭据、查询参数或 fragment。

响应为 `application/x-ndjson`，事件仅有 `run.started`、`text.delta`、`tool.started`、`tool.completed`、`run.completed`、`run.failed`。Go API 将其转换为浏览器 SSE；reasoning 分片被丢弃，工具事件仅带调用 ID、名称与状态，不泄露完整参数、结果或凭据。

## 插件与权限

工作流由 `plugins/*.json` 按 `workflowId` 选择。Manifest 提供 `schemaVersion`、`id`、名称、说明、版本、`enabled`、`persona`、`defaultModel`、`maxTokens`、`capabilities` 和 `allowedTools`。使用现有 JSON 文件作为模板：

- `ops-assistant`：设备、告警、趋势与知识检索，供聊天工作台使用。
- `alarm-handler`：告警研判。
- `device-health-inspector`：设备巡检。
- `protocol-assistant`：协议资料与点表辅助。

后面三类由对应业务页面调用。内置 Manifest 只读，自定义插件通过管理员入口管理；知识范围在平台知识库配置。

Manifest 不能扩大工具权限。网关校验和 Cordis 全局 `tools.guard` 同时限制到代码定义的只读集合；MCP 发现失败会拒绝创建 Agent。shell、文件系统、jobs、goal、skills、subagent 和设备控制不在工具面内。

## 会话与 JWT 轮换

Go API 将租户、用户和浏览器会话 ID 派生为内部 `conversationId`。侧车为每个会话保留一个运行时进程和 SDK session，同会话请求按 FIFO 排队，多轮复用驻留会话。

Harness 进程只持有随机运行时代理密钥，不接收 MCP JWT。回环代理在内存保存当前上游 URL / JWT，为每个 MCP POST 注入 Authorization，因此每轮可更新 JWT 而不重建会话。代理只接受 POST，请求上限 1 MiB、有超时、不记录正文或凭据，运行时回收时删除路由。

JSONL 用于持久历史与审计。当前锁定版本的 `session/create` 不调用 `agents.resume` 冷恢复路径；对话连续性依赖驻留池。进程重启会创建新一代会话，不应把 JSONL 文件存在描述为已经恢复模型上下文。
