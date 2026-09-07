# DeepSeek Harness IoT sidecar

This directory turns the official DeepSeek Harness JSON-RPC runtime into a
small, manifest-driven IoT workflow service. The Harness source is vendored at
`upstream/deepseek-harness` and pinned by `REVISION` to
`dsh-v0.1.2-rc.1` (`a66e4702047846cdaa10c66c9d3df3951f5ea70d`).

The public gateway listens on port `8091`. A separate MCP credential proxy
listens only on `127.0.0.1:8092`; it is not an externally exposed port.

## Build and test

Build context must be the repository root:

```bash
docker build \
  -f deploy/deepseek-harness/Dockerfile \
  -t iot-deepseek-harness:a66e470 .
```

The image installs `pnpm@11.7.0`, builds the complete upstream package tree
required by the unified `dsh` CLI, verifies the pinned revision marker, and
materializes the upstream `pnpm deploy` runtime carrier. The sidecar launches
`@deepseek-ai/dsh/lib/bin.js` with the `sdk-minimal` profile and applies the
deployment-owned `cordis.yml` overlay. Each resident conversation receives an
isolated `DSH_HOME`. The image build runs gateway tests, imports the real
agent/MCP plugins, and completes a real SDK + Cordis + mock-MCP startup
handshake. The gateway tests can also run without a Harness build because they
inject a protocol-compatible fake:

```bash
node --test deploy/deepseek-harness/gateway.test.mjs
```

## Run

`IOT_HARNESS_GATEWAY_TOKEN` is an internal service credential and must contain
at least 32 characters. `Authorization` on a chat request is a separate,
short-lived MCP JWT.

```bash
docker run --rm \
  --name iot-deepseek-harness \
  --network iot-platform_default \
  -p 8091:8091 \
  -e DEEPSEEK_API_KEY='replace-me' \
  -e IOT_HARNESS_GATEWAY_TOKEN='replace-with-at-least-32-random-characters' \
  -e IOT_HARNESS_MCP_ALLOWED_ORIGINS='http://platform-api:8080' \
  -v iot-harness-data:/data \
  iot-deepseek-harness:a66e470
```

Important settings:

| Variable | Default | Purpose |
| --- | --- | --- |
| `IOT_HARNESS_PORT` | `8091` | Public HTTP gateway port |
| `IOT_HARNESS_MCP_PROXY_PORT` | `8092` | Loopback-only MCP proxy port |
| `IOT_HARNESS_MCP_ALLOWED_ORIGINS` | `http://platform-api:8080` | Comma-separated exact upstream origins |
| `IOT_HARNESS_SESSION_ROOT` | `/data/sessions` | JSONL persistence root |
| `IOT_HARNESS_HOME` | `/data/runtime-home` | Base directory for per-conversation `DSH_HOME` state |
| `IOT_HARNESS_WORKSPACE` | `/data/workspace` | Runtime workspace root |
| `IOT_HARNESS_CONVERSATION_TTL_MS` | `600000` | Idle resident runtime lifetime |
| `IOT_HARNESS_MAX_CACHED_CONVERSATIONS` | `32` | Resident runtime pool ceiling |
| `IOT_HARNESS_MAX_CONCURRENCY` | `4` | Concurrent runs across conversations |
| `IOT_HARNESS_RUN_TIMEOUT_MS` | `180000` | End-to-end Harness turn deadline |

## HTTP contract

`GET /health` is unauthenticated for container health checks.
`GET /v1/plugins` requires `X-IOT-Harness-Token` and returns enabled workflow
metadata and human-readable capabilities. It intentionally omits personas and
tool allowlists.

`GET /v1/plugins/admin` uses the same service token and returns the complete
manifest catalog, including disabled plugins, personas and tool allowlists.
`POST /v1/plugins` creates or updates a custom manifest, while
`DELETE /v1/plugins/{id}` removes a custom manifest. Built-in manifests are
immutable. The Go platform API exposes these operations only to administrators
through its Agent management screen.

`POST /v1/chat/stream` requires both credentials:

```bash
curl --no-buffer http://127.0.0.1:8091/v1/chat/stream \
  -H 'Content-Type: application/json' \
  -H 'X-IOT-Harness-Token: replace-with-at-least-32-random-characters' \
  -H 'Authorization: Bearer short-lived-mcp-jwt' \
  --data '{
    "runId":"run-001",
    "conversationId":"tenant-user-namespaced-hash",
    "workflowId":"ops-assistant",
    "question":"当前有哪些高等级活动告警？",
    "mcpUrl":"http://platform-api:8080/mcp/harness",
    "model":"qwen3:1.7b",
    "maxTokens":1200
  }'
```

The response is `application/x-ndjson`. Its only event types are
`run.started`, `text.delta`, `tool.started`, `tool.completed`,
`run.completed`, and `run.failed`. Reasoning deltas are dropped. Tool
arguments and results never leave the gateway; completion events contain only
the call ID, tool name, and success flag.

The upstream URL must have an allowed origin and the exact path
`/mcp/harness`, with no credentials, query, or fragment.

## Workflow plugins

Workflow selection is data-driven. The gateway reloads every regular
`plugins/*.json` file and selects it by `workflowId`; there is no workflow ID
switch in gateway code. The bundled examples are:

- `ops-assistant`: device status, alarms, property history, similar alarms,
  and knowledge-base search.
- `alarm-handler`: alarms, property history, similar alarms, and
  knowledge-base search only.
- `device-health-inspector`: device status, activity alarms and property trend
  evidence for an operator-facing health inspection.
- `protocol-assistant`: protocol documentation and point-table interpretation;
  the platform UI performs the sample preview and human-approved publish.

A manifest supplies `persona`, `defaultModel`, `maxTokens`, `capabilities`, and
`allowedTools`. To add another read-only business workflow, add a manifest with
this shape:

```json
{
  "schemaVersion": 1,
  "id": "example-workflow",
  "name": "Example",
  "description": "Read-only example workflow",
  "version": "1.0.0",
  "enabled": true,
  "persona": "System persona",
  "defaultModel": "qwen3:1.7b",
  "maxTokens": 4096,
  "capabilities": ["用户可读能力名称"],
  "allowedTools": ["mcp__iot__query_alarm_list"]
}
```

Manifests cannot expand authority. Both gateway validation and the Cordis
policy reject tools outside the compiled read-only ceiling. The Cordis plugin
installs a global monotonic `tools.guard`; `agent/created` restriction only
hides tools from the model. Missing MCP discovery therefore fails agent
creation closed. Shell, filesystem, job, goal, skill, subagent, and device
control tools are not composed.

## Sessions and JWT rotation

The backend supplies a tenant-and-user-namespaced hash as `conversationId`.
The gateway keeps one resident `DeepSeekHarness` process and SDK session per
conversation, queues same-conversation requests in FIFO order, and reuses that
session across turns.

The Harness process never receives the short-lived MCP JWT. It receives a
random per-runtime key and calls the loopback proxy, which stores the current
upstream URL/JWT in memory and injects `Authorization` on each MCP POST. A new
JWT can therefore be used on every turn without rebuilding the resident
session. The proxy accepts POST only, caps requests at 1 MiB, applies a timeout,
does not log payloads or credentials, and removes the runtime route on eviction.

`@deepseek-ai/dsh-session-persistence-jsonl` writes session events under
`IOT_HARNESS_SESSION_ROOT`. In the pinned upstream implementation, JSON-RPC
`session/create` uses `agents.create`; it does not call the separate
`agents.resume` cold-restore path. Consequently, JSONL is durable audit/history
for this sidecar, while conversational continuation is guaranteed by the
resident pool. A process restart starts a uniquely named session generation
instead of pretending to restore an old one or colliding with its JSONL file.
