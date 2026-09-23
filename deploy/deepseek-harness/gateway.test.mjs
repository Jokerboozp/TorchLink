import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import { createServer } from 'node:http' /* 引入当前代码需要的依赖。 */
import { tmpdir } from 'node:os' /* 引入当前代码需要的依赖。 */
import { dirname, join } from 'node:path' /* 引入当前代码需要的依赖。 */
import { after, test } from 'node:test' /* 引入当前代码需要的依赖。 */
import { fileURLToPath } from 'node:url' /* 引入当前代码需要的依赖。 */

import { createGateway, loadPluginCatalog } from './gateway.mjs' /* 引入当前代码需要的依赖。 */
import { apply as applyPolicy, forwardAssistantText } from './iot-ops-plugin.mjs' /* 引入当前代码需要的依赖。 */

const deploymentDir = dirname(fileURLToPath(import.meta.url)) /* 声明 deploymentDir。 */
const gatewayToken = 'test-harness-token-that-is-at-least-32-chars' /* 声明 gatewayToken。 */
const openedGateways = [] /* 声明 openedGateways。 */
const openedServers = [] /* 声明 openedServers。 */
const temporaryDirectories = [] /* 声明 temporaryDirectories。 */

after(async () => { /* 执行当前语句并推进处理流程。 */
  await Promise.allSettled(openedGateways.splice(0).map(gateway => gateway.close())) /* 等待异步操作完成。 */
  await Promise.allSettled(openedServers.splice(0).map(server => new Promise(resolve => server.close(resolve)))) /* 等待异步操作完成。 */
  await Promise.allSettled(temporaryDirectories.splice(0).map(path => rm(path, { recursive: true, force: true }))) /* 等待异步操作完成。 */
}) /* 结束当前表达式或代码块。 */

function result(finalResponse = '') { /* 定义 result 函数。 */
  return { /* 返回当前处理结果。 */
    finalResponse, /* 执行当前语句并推进处理流程。 */
    events: [{ type: 'turn/end', data: { reason: { kind: 'completed' } } }], /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function startGateway(factory, options = {}) { /* 定义 startGateway 函数。 */
  const gateway = createGateway({ /* 声明 gateway。 */
    gatewayToken, /* 执行当前语句并推进处理流程。 */
    pluginDir: join(deploymentDir, 'plugins'), /* 执行当前语句并推进处理流程。 */
    patchFile: join(deploymentDir, 'cordis.yml'), /* 执行当前语句并推进处理流程。 */
    runtimeBin: fileURLToPath(import.meta.url), /* 执行当前语句并推进处理流程。 */
    sdkClientModule: fileURLToPath(import.meta.url), /* 执行当前语句并推进处理流程。 */
    runtimeCwd: deploymentDir, /* 执行当前语句并推进处理流程。 */
    workspace: deploymentDir, /* 执行当前语句并推进处理流程。 */
    sessionRoot: join(tmpdir(), 'iot-harness-test-sessions'), /* 执行当前语句并推进处理流程。 */
    proxyPort: 0, /* 执行当前语句并推进处理流程。 */
    harnessFactory: factory, /* 执行当前语句并推进处理流程。 */
    ...options, /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
  openedGateways.push(gateway) /* 执行当前语句并推进处理流程。 */
  const address = await gateway.listen(0, '127.0.0.1') /* 声明 address。 */
  return { gateway, baseUrl: `http://127.0.0.1:${address.port}` } /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function requestBody(overrides = {}) { /* 定义 requestBody 函数。 */
  return { /* 返回当前处理结果。 */
    runId: 'run-1', /* 执行当前语句并推进处理流程。 */
    conversationId: 'tenant-user-conversation-hash', /* 执行当前语句并推进处理流程。 */
    workflowId: 'ops-assistant', /* 执行当前语句并推进处理流程。 */
    question: '检查当前高等级告警', /* 执行当前语句并推进处理流程。 */
    mcpUrl: 'http://platform-api:8080/mcp/harness', /* 执行当前语句并推进处理流程。 */
    model: 'qwen3:1.7b', /* 执行当前语句并推进处理流程。 */
    maxTokens: 1200, /* 执行当前语句并推进处理流程。 */
    ...overrides, /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function chat(baseUrl, body, bearer = 'short-lived-mcp-jwt') { /* 定义 chat 函数。 */
  return fetch(`${baseUrl}/v1/chat/stream`, { /* 返回当前处理结果。 */
    method: 'POST', /* 执行当前语句并推进处理流程。 */
    headers: { /* 执行当前语句并推进处理流程。 */
      authorization: `Bearer ${bearer}`, /* 执行当前语句并推进处理流程。 */
      'content-type': 'application/json', /* 执行当前语句并推进处理流程。 */
      'x-iot-harness-token': gatewayToken, /* 执行当前语句并推进处理流程。 */
    }, /* 结束当前表达式或代码块。 */
    body: JSON.stringify(body), /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function ndjson(response) { /* 定义 ndjson 函数。 */
  const payload = await response.text() /* 声明 payload。 */
  return { payload, events: payload.trim().split('\n').filter(Boolean).map(line => JSON.parse(line)) } /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

test('catalog is manifest-driven and exposes capabilities without policy internals', async () => { /* 执行当前语句并推进处理流程。 */
  const plugins = await loadPluginCatalog(join(deploymentDir, 'plugins')) /* 声明 plugins。 */
  assert.deepEqual(plugins.map(plugin => plugin.id), ['alarm-handler', 'device-health-inspector', 'ops-assistant', 'protocol-assistant', 'system-observer']) /* 验证实际结果符合预期。 */
  assert.ok(plugins.every(plugin => plugin.capabilities.length > 0)) /* 验证实际结果符合预期。 */

  const { baseUrl } = await startGateway(async () => ({ run: async () => result(), close: async () => {} })) /* 执行当前语句并推进处理流程。 */
  const unauthorized = await fetch(`${baseUrl}/v1/plugins`) /* 声明 unauthorized。 */
  assert.equal(unauthorized.status, 401) /* 验证实际结果符合预期。 */
  const response = await fetch(`${baseUrl}/v1/plugins`, { /* 声明 response。 */
    headers: { 'x-iot-harness-token': gatewayToken }, /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
  assert.equal(response.status, 200) /* 验证实际结果符合预期。 */
  const body = await response.json() /* 声明 body。 */
  assert.equal(body.items.length, 5) /* 验证实际结果符合预期。 */
  assert.ok(body.items.every(plugin => Array.isArray(plugin.capabilities))) /* 验证实际结果符合预期。 */
  assert.ok(body.items.every(plugin => plugin.persona === undefined && plugin.allowedTools === undefined)) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('admin API atomically creates a persistent Agent manifest that is immediately listed', async () => { /* 执行当前语句并推进处理流程。 */
  const root = await mkdtemp(join(tmpdir(), 'iot-harness-dynamic-agent-')) /* 声明 root。 */
  temporaryDirectories.push(root) /* 执行当前语句并推进处理流程。 */
  const seed = JSON.parse(await readFile(join(deploymentDir, 'plugins', 'ops-assistant.json'), 'utf8')) /* 声明 seed。 */
  seed.id = 'seed-agent' /* 更新 seed.id 的值。 */
  seed.name = 'Seed Agent' /* 更新 seed.name 的值。 */
  await writeFile(join(root, 'seed-agent.json'), JSON.stringify(seed)) /* 等待异步操作完成。 */
  const { baseUrl } = await startGateway(async () => ({ run: async () => result(), close: async () => {} }), { pluginDir: root }) /* 执行当前语句并推进处理流程。 */
  const manifest = { /* 声明 manifest。 */
    ...seed, /* 执行当前语句并推进处理流程。 */
    id: 'dynamic-status-agent', /* 执行当前语句并推进处理流程。 */
    name: 'Dynamic Status Agent', /* 执行当前语句并推进处理流程。 */
    description: 'Created from the management UI', /* 执行当前语句并推进处理流程。 */
    allowedTools: ['mcp__iot__query_system_overview'], /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  const created = await fetch(`${baseUrl}/v1/plugins`, { /* 声明 created。 */
    method: 'POST', /* 执行当前语句并推进处理流程。 */
    headers: { 'content-type': 'application/json', 'x-iot-harness-token': gatewayToken }, /* 执行当前语句并推进处理流程。 */
    body: JSON.stringify(manifest), /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
  assert.equal(created.status, 201) /* 验证实际结果符合预期。 */
  assert.equal((await created.json()).id, manifest.id) /* 验证实际结果符合预期。 */
  const persisted = JSON.parse(await readFile(join(root, `${manifest.id}.json`), 'utf8')) /* 声明 persisted。 */
  assert.equal(persisted.name, manifest.name) /* 验证实际结果符合预期。 */
  const listed = await fetch(`${baseUrl}/v1/plugins`, { headers: { 'x-iot-harness-token': gatewayToken } }) /* 声明 listed。 */
  assert.deepEqual((await listed.json()).items.map(plugin => plugin.id), ['dynamic-status-agent', 'seed-agent']) /* 验证实际结果符合预期。 */
  const adminListed = await fetch(`${baseUrl}/v1/plugins/admin`, { headers: { 'x-iot-harness-token': gatewayToken } }) /* 声明 adminListed。 */
  assert.equal(adminListed.status, 200) /* 验证实际结果符合预期。 */
  const adminBody = await adminListed.json() /* 声明 adminBody。 */
  assert.equal(adminBody.count, 2) /* 验证实际结果符合预期。 */
  assert.equal(adminBody.items.find(plugin => plugin.id === manifest.id).persona, manifest.persona) /* 验证实际结果符合预期。 */
  assert.deepEqual(adminBody.items.find(plugin => plugin.id === manifest.id).allowedTools, manifest.allowedTools) /* 验证实际结果符合预期。 */
  const disabled = await fetch(`${baseUrl}/v1/plugins`, { /* 声明 disabled。 */
    method: 'POST', /* 执行当前语句并推进处理流程。 */
    headers: { 'content-type': 'application/json', 'x-iot-harness-token': gatewayToken }, /* 执行当前语句并推进处理流程。 */
    body: JSON.stringify({ ...manifest, enabled: false }), /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
  assert.equal(disabled.status, 200) /* 验证实际结果符合预期。 */
  const publicAfterDisable = await fetch(`${baseUrl}/v1/plugins`, { headers: { 'x-iot-harness-token': gatewayToken } }) /* 声明 publicAfterDisable。 */
  assert.deepEqual((await publicAfterDisable.json()).items.map(plugin => plugin.id), ['seed-agent']) /* 验证实际结果符合预期。 */
  const adminAfterDisable = await fetch(`${baseUrl}/v1/plugins/admin`, { headers: { 'x-iot-harness-token': gatewayToken } }) /* 声明 adminAfterDisable。 */
  assert.equal((await adminAfterDisable.json()).items.find(plugin => plugin.id === manifest.id).enabled, false) /* 验证实际结果符合预期。 */
  const deleted = await fetch(`${baseUrl}/v1/plugins/${manifest.id}`, { method: 'DELETE', headers: { 'x-iot-harness-token': gatewayToken } }) /* 声明 deleted。 */
  assert.equal(deleted.status, 200) /* 验证实际结果符合预期。 */
  const missing = await fetch(`${baseUrl}/v1/plugins/${manifest.id}`, { method: 'DELETE', headers: { 'x-iot-harness-token': gatewayToken } }) /* 声明 missing。 */
  assert.equal(missing.status, 404) /* 验证实际结果符合预期。 */
  const immutable = await fetch(`${baseUrl}/v1/plugins`, { /* 声明 immutable。 */
    method: 'POST', headers: { 'content-type': 'application/json', 'x-iot-harness-token': gatewayToken }, /* 执行当前语句并推进处理流程。 */
    body: JSON.stringify({ ...manifest, id: 'ops-assistant' }), /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
  assert.equal(immutable.status, 409) /* 验证实际结果符合预期。 */
  const immutableDelete = await fetch(`${baseUrl}/v1/plugins/ops-assistant`, { method: 'DELETE', headers: { 'x-iot-harness-token': gatewayToken } }) /* 声明 immutableDelete。 */
  assert.equal(immutableDelete.status, 409) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('gateway refuses internal tokens shorter than 32 characters', () => { /* 执行当前语句并推进处理流程。 */
  assert.throws( /* 验证实际结果符合预期。 */
    () => createGateway({ gatewayToken: 'too-short' }), /* 执行当前语句并推进处理流程。 */
    /32 to 512/, /* 执行当前语句并推进处理流程。 */
  ) /* 结束当前表达式或代码块。 */
}) /* 结束当前表达式或代码块。 */

test('gateway rejects an unsupported model provider', () => { /* 执行当前语句并推进处理流程。 */
  assert.throws( /* 验证实际结果符合预期。 */
    () => createGateway({ gatewayToken, modelProvider: 'unsupported' }), /* 执行当前语句并推进处理流程。 */
    /IOT_HARNESS_PROVIDER/, /* 执行当前语句并推进处理流程。 */
  ) /* 结束当前表达式或代码块。 */
}) /* 结束当前表达式或代码块。 */

test('provider endpoint switches the resident runtime and redacts API keys', async () => { /* 执行当前语句并推进处理流程。 */
  let factorySpec /* 声明 factorySpec。 */
  const { baseUrl } = await startGateway(async spec => { /* 执行当前语句并推进处理流程。 */
    factorySpec = spec /* 更新 factorySpec 的值。 */
    return { run: async () => result('provider switched'), close: async () => {} } /* 返回当前处理结果。 */
  }) /* 结束当前表达式或代码块。 */
  const initial = await fetch(`${baseUrl}/v1/provider`, { headers: { 'x-iot-harness-token': gatewayToken } }) /* 声明 initial。 */
  assert.equal(initial.status, 200) /* 验证实际结果符合预期。 */
  assert.deepEqual(await initial.json(), { /* 验证实际结果符合预期。 */
    provider: 'ollama', /* 执行当前语句并推进处理流程。 */
    baseUrl: 'http://ollama:11434/v1', /* 执行当前语句并推进处理流程。 */
    model: 'qwen3:1.7b', /* 执行当前语句并推进处理流程。 */
    apiKeyConfigured: false, /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
  const updated = await fetch(`${baseUrl}/v1/provider`, { /* 声明 updated。 */
    method: 'PUT', /* 执行当前语句并推进处理流程。 */
    headers: { 'content-type': 'application/json', 'x-iot-harness-token': gatewayToken }, /* 执行当前语句并推进处理流程。 */
    body: JSON.stringify({ provider: 'deepseek-official', baseUrl: 'https://api.deepseek.com', model: 'deepseek-chat', apiKey: 'provider-test-key' }), /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
  assert.equal(updated.status, 200) /* 验证实际结果符合预期。 */
  const updatedBody = await updated.json() /* 声明 updatedBody。 */
  assert.deepEqual(updatedBody, { /* 验证实际结果符合预期。 */
    provider: 'deepseek-official', /* 执行当前语句并推进处理流程。 */
    baseUrl: 'https://api.deepseek.com', /* 执行当前语句并推进处理流程。 */
    model: 'deepseek-chat', /* 执行当前语句并推进处理流程。 */
    apiKeyConfigured: true, /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
  assert.equal(updatedBody.apiKey, undefined) /* 验证实际结果符合预期。 */
  const response = await chat(baseUrl, requestBody({ runId: 'provider-switched', model: 'legacy-model' })) /* 声明 response。 */
  assert.equal(response.status, 200) /* 验证实际结果符合预期。 */
  await response.text() /* 等待异步操作完成。 */
  assert.equal(factorySpec.provider, 'deepseek-official') /* 验证实际结果符合预期。 */
  assert.equal(factorySpec.baseUrl, 'https://api.deepseek.com') /* 验证实际结果符合预期。 */
  assert.equal(factorySpec.model, 'deepseek-chat') /* 验证实际结果符合预期。 */
  assert.equal(factorySpec.apiKey, 'provider-test-key') /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('manifest security ceiling rejects a write-capable tool', async () => { /* 执行当前语句并推进处理流程。 */
  const root = await mkdtemp(join(tmpdir(), 'iot-harness-manifest-')) /* 声明 root。 */
  temporaryDirectories.push(root) /* 执行当前语句并推进处理流程。 */
  const valid = JSON.parse(await readFile(join(deploymentDir, 'plugins', 'ops-assistant.json'), 'utf8')) /* 声明 valid。 */
  valid.id = 'unsafe-plugin' /* 更新 valid.id 的值。 */
  valid.allowedTools = ['mcp__iot__control_device'] /* 更新 valid.allowedTools 的值。 */
  await writeFile(join(root, 'unsafe.json'), JSON.stringify(valid)) /* 等待异步操作完成。 */
  await assert.rejects(() => loadPluginCatalog(root), /outside the read-only security ceiling/) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('Cordis policy installs a monotonic global guard and prompt restriction', () => { /* 执行当前语句并推进处理流程。 */
  let guard /* 声明 guard。 */
  let created /* 声明 created。 */
  let restricted /* 声明 restricted。 */
  const ctx = { /* 声明 ctx。 */
    tools: { guard: callback => { guard = callback } }, /* 执行当前语句并推进处理流程。 */
    on: (name, callback) => { /* 执行当前语句并推进处理流程。 */
      assert.ok(['agent/created', 'agent/assistant-stream'].includes(name)) /* 验证实际结果符合预期。 */
      if (name === 'agent/created') created = callback /* 判断条件并选择处理分支。 */
    }, /* 结束当前表达式或代码块。 */
  } /* 结束当前表达式或代码块。 */
  applyPolicy(ctx, { allowedTools: ['mcp__iot__query_alarm_list', 'mcp__iot__create_rule_draft'] }) /* 执行当前语句并推进处理流程。 */
  assert.equal(guard({ name: 'mcp__iot__query_alarm_list' }), undefined) /* 验证实际结果符合预期。 */
  assert.equal(guard({ name: 'mcp__iot__create_rule_draft' }), undefined) /* 验证实际结果符合预期。 */
  assert.equal(guard({ name: 'mcp__iot__control_device' }), 'tool not allowed') /* 验证实际结果符合预期。 */
  created({ agent: { ctx: { tools: { restrict: config => { restricted = config } } } } }) /* 执行当前语句并推进处理流程。 */
  assert.deepEqual(restricted, { allow: ['mcp__iot__query_alarm_list', 'mcp__iot__create_rule_draft'] }) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('runtime stream bridge forwards only text with its owning session', () => { /* 执行当前语句并推进处理流程。 */
  const frames = [] /* 声明 frames。 */
  const write = line => frames.push(JSON.parse(line)) /* 声明 write。 */
  const agent = { session: { id: 'session-one' } } /* 声明 agent。 */
  for (const frame of [ /* 循环处理当前数据。 */
    { type: 'start' }, /* 执行当前语句并推进处理流程。 */
    { type: 'chunk', chunk: { type: 'reasoning-delta', text: 'SECRET_REASONING' } }, /* 执行当前语句并推进处理流程。 */
    { type: 'chunk', chunk: { type: 'tool-call', arguments: 'SECRET_ARGUMENTS' } }, /* 执行当前语句并推进处理流程。 */
    { type: 'chunk', chunk: { type: 'text-delta', text: '' } }, /* 执行当前语句并推进处理流程。 */
    { type: 'chunk', chunk: { type: 'text-delta', text: '可见结论' } }, /* 执行当前语句并推进处理流程。 */
    { type: 'end' }, /* 执行当前语句并推进处理流程。 */
  ]) forwardAssistantText({ agent, frame }, write) /* 结束当前表达式或代码块。 */
  assert.deepEqual(frames, [{ jsonrpc: '2.0', method: 'iot.text.delta', params: { sessionId: 'session-one', text: '可见结论' } }]) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('stream emits only the public NDJSON event vocabulary and suppresses reasoning/results', async () => { /* 执行当前语句并推进处理流程。 */
  let factorySpec /* 声明 factorySpec。 */
  const { baseUrl } = await startGateway(async spec => { /* 执行当前语句并推进处理流程。 */
    factorySpec = spec /* 更新 factorySpec 的值。 */
    return { /* 返回当前处理结果。 */
      async run(_question, options) { /* 执行当前语句并推进处理流程。 */
        const notify = event => options.onNotification({ /* 声明 notify。 */
          method: 'session.event', /* 执行当前语句并推进处理流程。 */
          params: { sessionId: options.sessionId, event }, /* 执行当前语句并推进处理流程。 */
        }) /* 结束当前表达式或代码块。 */
        notify({ type: 'assistant/chunk', data: { chunk: { type: 'reasoning-delta', text: 'SECRET_REASONING' } } }) /* 执行当前语句并推进处理流程。 */
        options.onNotification({ method: 'iot.text.delta', params: { sessionId: 'other-session', text: 'SECRET_OTHER_SESSION' } }) /* 执行当前语句并推进处理流程。 */
        options.onNotification({ method: 'iot.text.delta', params: { sessionId: options.sessionId, text: '可见结论' } }) /* 执行当前语句并推进处理流程。 */
        notify({ type: 'tool/call', data: { callId: 'call-direct', name: 'mcp__iot__query_alarm_list', arguments: { secret: true } } }) /* 执行当前语句并推进处理流程。 */
        notify({ type: 'tool/result', data: { callId: 'call-direct', result: 'SECRET_TOOL_RESULT' } }) /* 执行当前语句并推进处理流程。 */
        notify({ type: 'tool/call', data: { callId: 'call-legacy', name: 'mcp__iot__query_knowledge_base' } }) /* 执行当前语句并推进处理流程。 */
        notify({ type: 'tool/result', data: { message: { source: { callId: 'call-legacy' }, content: [{ text: 'SECRET_LEGACY_RESULT' }] } } }) /* 执行当前语句并推进处理流程。 */
        notify({ type: 'tool/call', data: { callId: 'call-draft', name: 'mcp__iot__create_rule_draft' } }) /* 执行当前语句并推进处理流程。 */
        notify({ type: 'tool/result', data: { message: { source: { callId: 'call-draft' }, content: [{ type:'tool-result', isError:false, content:[{ type:'text', text:JSON.stringify({ kind:'ruleDraft', persisted:true, draft:{ id:'draft-1', name:'高温联动', conditions:[{ field:'temperature', operator:'>', value:80 }], actions:[{ type:'OPEN_CAMERA', cameraId:'camera-001' }], internalSecret:'MUST_NOT_LEAK' } }) }] }] } } }) /* 执行当前语句并推进处理流程。 */
        return result('fallback must not duplicate streamed text') /* 返回当前处理结果。 */
      }, /* 结束当前表达式或代码块。 */
      async close() {}, /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
  }) /* 结束当前表达式或代码块。 */
  const response = await chat(baseUrl, requestBody({ model: 'legacy-model' })) /* 声明 response。 */
  assert.equal(response.status, 200) /* 验证实际结果符合预期。 */
  const { payload, events } = await ndjson(response) /* 执行当前语句并推进处理流程。 */
  assert.deepEqual(events.map(event => event.type), [ /* 验证实际结果符合预期。 */
    'run.started', /* 执行当前语句并推进处理流程。 */
    'text.delta', /* 执行当前语句并推进处理流程。 */
    'tool.started', /* 执行当前语句并推进处理流程。 */
    'tool.completed', /* 执行当前语句并推进处理流程。 */
    'tool.started', /* 执行当前语句并推进处理流程。 */
    'tool.completed', /* 执行当前语句并推进处理流程。 */
    'tool.started', /* 执行当前语句并推进处理流程。 */
    'tool.completed', /* 执行当前语句并推进处理流程。 */
    'run.completed', /* 执行当前语句并推进处理流程。 */
  ]) /* 结束当前表达式或代码块。 */
  assert.equal(events[3].success, true) /* 验证实际结果符合预期。 */
  assert.equal(events[5].success, true) /* 验证实际结果符合预期。 */
  assert.equal(events[7].data.clientAction.type, 'RULE_DRAFT_READY') /* 验证实际结果符合预期。 */
  assert.equal(events[7].data.clientAction.draft.actions[0].cameraId, 'camera-001') /* 验证实际结果符合预期。 */
  assert.equal(events[7].data.clientAction.persisted, true) /* 验证实际结果符合预期。 */
  assert.doesNotMatch(payload, /SECRET_REASONING|SECRET_TOOL_RESULT|SECRET_LEGACY_RESULT|SECRET_OTHER_SESSION|arguments/) /* 验证实际结果符合预期。 */
  assert.doesNotMatch(payload, /MUST_NOT_LEAK/) /* 验证实际结果符合预期。 */
  assert.equal(factorySpec.mcpToken, undefined) /* 验证实际结果符合预期。 */
  assert.equal(factorySpec.provider, 'ollama') /* 验证实际结果符合预期。 */
  assert.equal(factorySpec.model, 'qwen3:1.7b') /* 验证实际结果符合预期。 */
  assert.match(factorySpec.proxyMcpUrl, /^http:\/\/127\.0\.0\.1:\d+\/mcp$/) /* 验证实际结果符合预期。 */
  assert.equal(factorySpec.runtimeAccessKey.length, 43) /* 验证实际结果符合预期。 */
  assert.ok(factorySpec.plugin.persona.includes('AI 运维助手')) /* 验证实际结果符合预期。 */
  assert.equal(factorySpec.patchFile, join(deploymentDir, 'cordis.yml')) /* 验证实际结果符合预期。 */
  assert.ok(factorySpec.harnessHome.endsWith(`\\${factorySpec.sessionId}`) || factorySpec.harnessHome.endsWith(`/${factorySpec.sessionId}`)) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('upstream MCP path is exactly /mcp/harness', async () => { /* 执行当前语句并推进处理流程。 */
  let factoryCalls = 0 /* 声明 factoryCalls。 */
  const { baseUrl } = await startGateway(async () => { /* 执行当前语句并推进处理流程。 */
    factoryCalls += 1 /* 更新 factoryCalls 的值。 */
    return { run: async () => result(), close: async () => {} } /* 返回当前处理结果。 */
  }) /* 结束当前表达式或代码块。 */
  const wrong = await chat(baseUrl, requestBody({ mcpUrl: 'http://platform-api:8080/mcp' })) /* 声明 wrong。 */
  assert.equal(wrong.status, 422) /* 验证实际结果符合预期。 */
  const correct = await chat(baseUrl, requestBody({ runId: 'run-correct' })) /* 声明 correct。 */
  assert.equal(correct.status, 200) /* 验证实际结果符合预期。 */
  assert.equal(factoryCalls, 1) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('loopback proxy rotates short-lived JWT without rebuilding the resident conversation', async () => { /* 执行当前语句并推进处理流程。 */
  const authorizations = [] /* 声明 authorizations。 */
  const upstream = createServer(async (request, response) => { /* 声明 upstream。 */
    authorizations.push(request.headers.authorization) /* 执行当前语句并推进处理流程。 */
    for await (const _chunk of request) { /* drain */ } /* 循环处理当前数据。 */
    response.writeHead(200, { 'content-type': 'application/json', 'mcp-session-id': 'test-session' }) /* 执行当前语句并推进处理流程。 */
    response.end('{"jsonrpc":"2.0","id":1,"result":{}}') /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
  openedServers.push(upstream) /* 执行当前语句并推进处理流程。 */
  await new Promise(resolve => upstream.listen(0, '127.0.0.1', resolve)) /* 等待异步操作完成。 */
  const upstreamAddress = upstream.address() /* 声明 upstreamAddress。 */
  const mcpUrl = `http://127.0.0.1:${upstreamAddress.port}/mcp/harness` /* 声明 mcpUrl。 */
  let factoryCalls = 0 /* 声明 factoryCalls。 */
  let sdkSessionId /* 声明 sdkSessionId。 */
  const { baseUrl } = await startGateway(async spec => { /* 执行当前语句并推进处理流程。 */
    factoryCalls += 1 /* 更新 factoryCalls 的值。 */
    return { /* 返回当前处理结果。 */
      async run(_question, options) { /* 执行当前语句并推进处理流程。 */
        sdkSessionId ??= options.sessionId /* 执行当前语句并推进处理流程。 */
        assert.equal(options.sessionId, sdkSessionId) /* 验证实际结果符合预期。 */
        const proxied = await fetch(spec.proxyMcpUrl, { /* 声明 proxied。 */
          method: 'POST', /* 执行当前语句并推进处理流程。 */
          headers: { /* 执行当前语句并推进处理流程。 */
            accept: 'application/json', /* 执行当前语句并推进处理流程。 */
            'content-type': 'application/json', /* 执行当前语句并推进处理流程。 */
            'x-iot-runtime-key': spec.runtimeAccessKey, /* 执行当前语句并推进处理流程。 */
          }, /* 结束当前表达式或代码块。 */
          body: '{"jsonrpc":"2.0","id":1,"method":"tools/list"}', /* 执行当前语句并推进处理流程。 */
        }) /* 结束当前表达式或代码块。 */
        assert.equal(proxied.status, 200) /* 验证实际结果符合预期。 */
        return result('ok') /* 返回当前处理结果。 */
      }, /* 结束当前表达式或代码块。 */
      async close() {}, /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
  }, { allowedMcpOrigins: new URL(mcpUrl).origin }) /* 结束当前表达式或代码块。 */

  const first = await chat(baseUrl, requestBody({ runId: 'run-jwt-1', mcpUrl }), 'jwt-generation-one') /* 声明 first。 */
  assert.equal(first.status, 200) /* 验证实际结果符合预期。 */
  await first.text() /* 等待异步操作完成。 */
  const second = await chat(baseUrl, requestBody({ runId: 'run-jwt-2', mcpUrl }), 'jwt-generation-two') /* 声明 second。 */
  assert.equal(second.status, 200) /* 验证实际结果符合预期。 */
  await second.text() /* 等待异步操作完成。 */

  assert.equal(factoryCalls, 1) /* 验证实际结果符合预期。 */
  assert.deepEqual(authorizations, ['Bearer jwt-generation-one', 'Bearer jwt-generation-two']) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('requests sharing a conversation are serialized through one resident runtime', async () => { /* 执行当前语句并推进处理流程。 */
  let running = 0 /* 声明 running。 */
  let maximumRunning = 0 /* 声明 maximumRunning。 */
  let factoryCalls = 0 /* 声明 factoryCalls。 */
  const { baseUrl } = await startGateway(async () => { /* 执行当前语句并推进处理流程。 */
    factoryCalls += 1 /* 更新 factoryCalls 的值。 */
    return { /* 返回当前处理结果。 */
      async run() { /* 执行当前语句并推进处理流程。 */
        running += 1 /* 更新 running 的值。 */
        maximumRunning = Math.max(maximumRunning, running) /* 更新 maximumRunning 的值。 */
        await new Promise(resolve => setTimeout(resolve, 30)) /* 等待异步操作完成。 */
        running -= 1 /* 更新 running 的值。 */
        return result('done') /* 返回当前处理结果。 */
      }, /* 结束当前表达式或代码块。 */
      async close() {}, /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
  }) /* 结束当前表达式或代码块。 */
  const [first, second] = await Promise.all([ /* 执行当前语句并推进处理流程。 */
    chat(baseUrl, requestBody({ runId: 'run-serial-1' })), /* 执行当前语句并推进处理流程。 */
    chat(baseUrl, requestBody({ runId: 'run-serial-2' })), /* 执行当前语句并推进处理流程。 */
  ]) /* 结束当前表达式或代码块。 */
  assert.equal(first.status, 200) /* 验证实际结果符合预期。 */
  assert.equal(second.status, 200) /* 验证实际结果符合预期。 */
  await Promise.all([first.text(), second.text()]) /* 等待异步操作完成。 */
  assert.equal(maximumRunning, 1) /* 验证实际结果符合预期。 */
  assert.equal(factoryCalls, 1) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
