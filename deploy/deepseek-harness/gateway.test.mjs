import assert from 'node:assert/strict'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { createServer } from 'node:http'
import { tmpdir } from 'node:os'
import { dirname, join } from 'node:path'
import { after, test } from 'node:test'
import { fileURLToPath } from 'node:url'

import { createGateway, loadPluginCatalog } from './gateway.mjs'
import { apply as applyPolicy } from './iot-ops-plugin.mjs'

const deploymentDir = dirname(fileURLToPath(import.meta.url))
const gatewayToken = 'test-harness-token-that-is-at-least-32-chars'
const openedGateways = []
const openedServers = []
const temporaryDirectories = []

after(async () => {
  await Promise.allSettled(openedGateways.splice(0).map(gateway => gateway.close()))
  await Promise.allSettled(openedServers.splice(0).map(server => new Promise(resolve => server.close(resolve))))
  await Promise.allSettled(temporaryDirectories.splice(0).map(path => rm(path, { recursive: true, force: true })))
})

function result(finalResponse = '') {
  return {
    finalResponse,
    events: [{ type: 'turn/end', data: { reason: { kind: 'completed' } } }],
  }
}

async function startGateway(factory, options = {}) {
  const gateway = createGateway({
    gatewayToken,
    pluginDir: join(deploymentDir, 'plugins'),
    patchFile: join(deploymentDir, 'cordis.yml'),
    runtimeBin: fileURLToPath(import.meta.url),
    sdkClientModule: fileURLToPath(import.meta.url),
    runtimeCwd: deploymentDir,
    workspace: deploymentDir,
    sessionRoot: join(tmpdir(), 'iot-harness-test-sessions'),
    proxyPort: 0,
    harnessFactory: factory,
    ...options,
  })
  openedGateways.push(gateway)
  const address = await gateway.listen(0, '127.0.0.1')
  return { gateway, baseUrl: `http://127.0.0.1:${address.port}` }
}

function requestBody(overrides = {}) {
  return {
    runId: 'run-1',
    conversationId: 'tenant-user-conversation-hash',
    workflowId: 'ops-assistant',
    question: '检查当前高等级告警',
    mcpUrl: 'http://platform-api:8080/mcp/harness',
    model: 'qwen3:1.7b',
    maxTokens: 1200,
    ...overrides,
  }
}

async function chat(baseUrl, body, bearer = 'short-lived-mcp-jwt') {
  return fetch(`${baseUrl}/v1/chat/stream`, {
    method: 'POST',
    headers: {
      authorization: `Bearer ${bearer}`,
      'content-type': 'application/json',
      'x-iot-harness-token': gatewayToken,
    },
    body: JSON.stringify(body),
  })
}

async function ndjson(response) {
  const payload = await response.text()
  return { payload, events: payload.trim().split('\n').filter(Boolean).map(line => JSON.parse(line)) }
}

test('catalog is manifest-driven and exposes capabilities without policy internals', async () => {
  const plugins = await loadPluginCatalog(join(deploymentDir, 'plugins'))
  assert.deepEqual(plugins.map(plugin => plugin.id), ['alarm-handler', 'device-health-inspector', 'ops-assistant', 'protocol-assistant', 'system-observer'])
  assert.ok(plugins.every(plugin => plugin.capabilities.length > 0))

  const { baseUrl } = await startGateway(async () => ({ run: async () => result(), close: async () => {} }))
  const unauthorized = await fetch(`${baseUrl}/v1/plugins`)
  assert.equal(unauthorized.status, 401)
  const response = await fetch(`${baseUrl}/v1/plugins`, {
    headers: { 'x-iot-harness-token': gatewayToken },
  })
  assert.equal(response.status, 200)
  const body = await response.json()
  assert.equal(body.items.length, 5)
  assert.ok(body.items.every(plugin => Array.isArray(plugin.capabilities)))
  assert.ok(body.items.every(plugin => plugin.persona === undefined && plugin.allowedTools === undefined))
})

test('admin API atomically creates a persistent Agent manifest that is immediately listed', async () => {
  const root = await mkdtemp(join(tmpdir(), 'iot-harness-dynamic-agent-'))
  temporaryDirectories.push(root)
  const seed = JSON.parse(await readFile(join(deploymentDir, 'plugins', 'ops-assistant.json'), 'utf8'))
  seed.id = 'seed-agent'
  seed.name = 'Seed Agent'
  await writeFile(join(root, 'seed-agent.json'), JSON.stringify(seed))
  const { baseUrl } = await startGateway(async () => ({ run: async () => result(), close: async () => {} }), { pluginDir: root })
  const manifest = {
    ...seed,
    id: 'dynamic-status-agent',
    name: 'Dynamic Status Agent',
    description: 'Created from the management UI',
    allowedTools: ['mcp__iot__query_system_overview'],
  }
  const created = await fetch(`${baseUrl}/v1/plugins`, {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-iot-harness-token': gatewayToken },
    body: JSON.stringify(manifest),
  })
  assert.equal(created.status, 201)
  assert.equal((await created.json()).id, manifest.id)
  const persisted = JSON.parse(await readFile(join(root, `${manifest.id}.json`), 'utf8'))
  assert.equal(persisted.name, manifest.name)
  const listed = await fetch(`${baseUrl}/v1/plugins`, { headers: { 'x-iot-harness-token': gatewayToken } })
  assert.deepEqual((await listed.json()).items.map(plugin => plugin.id), ['dynamic-status-agent', 'seed-agent'])
  const adminListed = await fetch(`${baseUrl}/v1/plugins/admin`, { headers: { 'x-iot-harness-token': gatewayToken } })
  assert.equal(adminListed.status, 200)
  const adminBody = await adminListed.json()
  assert.equal(adminBody.count, 2)
  assert.equal(adminBody.items.find(plugin => plugin.id === manifest.id).persona, manifest.persona)
  assert.deepEqual(adminBody.items.find(plugin => plugin.id === manifest.id).allowedTools, manifest.allowedTools)
  const disabled = await fetch(`${baseUrl}/v1/plugins`, {
    method: 'POST',
    headers: { 'content-type': 'application/json', 'x-iot-harness-token': gatewayToken },
    body: JSON.stringify({ ...manifest, enabled: false }),
  })
  assert.equal(disabled.status, 200)
  const publicAfterDisable = await fetch(`${baseUrl}/v1/plugins`, { headers: { 'x-iot-harness-token': gatewayToken } })
  assert.deepEqual((await publicAfterDisable.json()).items.map(plugin => plugin.id), ['seed-agent'])
  const adminAfterDisable = await fetch(`${baseUrl}/v1/plugins/admin`, { headers: { 'x-iot-harness-token': gatewayToken } })
  assert.equal((await adminAfterDisable.json()).items.find(plugin => plugin.id === manifest.id).enabled, false)
  const deleted = await fetch(`${baseUrl}/v1/plugins/${manifest.id}`, { method: 'DELETE', headers: { 'x-iot-harness-token': gatewayToken } })
  assert.equal(deleted.status, 200)
  const missing = await fetch(`${baseUrl}/v1/plugins/${manifest.id}`, { method: 'DELETE', headers: { 'x-iot-harness-token': gatewayToken } })
  assert.equal(missing.status, 404)
  const immutable = await fetch(`${baseUrl}/v1/plugins`, {
    method: 'POST', headers: { 'content-type': 'application/json', 'x-iot-harness-token': gatewayToken },
    body: JSON.stringify({ ...manifest, id: 'ops-assistant' }),
  })
  assert.equal(immutable.status, 409)
  const immutableDelete = await fetch(`${baseUrl}/v1/plugins/ops-assistant`, { method: 'DELETE', headers: { 'x-iot-harness-token': gatewayToken } })
  assert.equal(immutableDelete.status, 409)
})

test('gateway refuses internal tokens shorter than 32 characters', () => {
  assert.throws(
    () => createGateway({ gatewayToken: 'too-short' }),
    /32 to 512/,
  )
})

test('gateway rejects an unsupported model provider', () => {
  assert.throws(
    () => createGateway({ gatewayToken, modelProvider: 'unsupported' }),
    /IOT_HARNESS_PROVIDER/,
  )
})

test('provider endpoint switches the resident runtime and redacts API keys', async () => {
  let factorySpec
  const { baseUrl } = await startGateway(async spec => {
    factorySpec = spec
    return { run: async () => result('provider switched'), close: async () => {} }
  })
  const initial = await fetch(`${baseUrl}/v1/provider`, { headers: { 'x-iot-harness-token': gatewayToken } })
  assert.equal(initial.status, 200)
  assert.deepEqual(await initial.json(), {
    provider: 'ollama',
    baseUrl: 'http://ollama:11434/v1',
    model: 'qwen3:1.7b',
    apiKeyConfigured: false,
  })
  const updated = await fetch(`${baseUrl}/v1/provider`, {
    method: 'PUT',
    headers: { 'content-type': 'application/json', 'x-iot-harness-token': gatewayToken },
    body: JSON.stringify({ provider: 'deepseek-official', baseUrl: 'https://api.deepseek.com', model: 'deepseek-chat', apiKey: 'provider-test-key' }),
  })
  assert.equal(updated.status, 200)
  const updatedBody = await updated.json()
  assert.deepEqual(updatedBody, {
    provider: 'deepseek-official',
    baseUrl: 'https://api.deepseek.com',
    model: 'deepseek-chat',
    apiKeyConfigured: true,
  })
  assert.equal(updatedBody.apiKey, undefined)
  const response = await chat(baseUrl, requestBody({ runId: 'provider-switched', model: 'legacy-model' }))
  assert.equal(response.status, 200)
  await response.text()
  assert.equal(factorySpec.provider, 'deepseek-official')
  assert.equal(factorySpec.baseUrl, 'https://api.deepseek.com')
  assert.equal(factorySpec.model, 'deepseek-chat')
  assert.equal(factorySpec.apiKey, 'provider-test-key')
})

test('manifest security ceiling rejects a write-capable tool', async () => {
  const root = await mkdtemp(join(tmpdir(), 'iot-harness-manifest-'))
  temporaryDirectories.push(root)
  const valid = JSON.parse(await readFile(join(deploymentDir, 'plugins', 'ops-assistant.json'), 'utf8'))
  valid.id = 'unsafe-plugin'
  valid.allowedTools = ['mcp__iot__control_device']
  await writeFile(join(root, 'unsafe.json'), JSON.stringify(valid))
  await assert.rejects(() => loadPluginCatalog(root), /outside the read-only security ceiling/)
})

test('Cordis policy installs a monotonic global guard and prompt restriction', () => {
  let guard
  let created
  let restricted
  const ctx = {
    tools: { guard: callback => { guard = callback } },
    on: (name, callback) => {
      assert.equal(name, 'agent/created')
      created = callback
    },
  }
  applyPolicy(ctx, { allowedTools: ['mcp__iot__query_alarm_list', 'mcp__iot__create_rule_draft'] })
  assert.equal(guard({ name: 'mcp__iot__query_alarm_list' }), undefined)
  assert.equal(guard({ name: 'mcp__iot__create_rule_draft' }), undefined)
  assert.equal(guard({ name: 'mcp__iot__control_device' }), 'tool not allowed')
  created({ agent: { ctx: { tools: { restrict: config => { restricted = config } } } } })
  assert.deepEqual(restricted, { allow: ['mcp__iot__query_alarm_list', 'mcp__iot__create_rule_draft'] })
})

test('stream emits only the public NDJSON event vocabulary and suppresses reasoning/results', async () => {
  let factorySpec
  const { baseUrl } = await startGateway(async spec => {
    factorySpec = spec
    return {
      async run(_question, options) {
        const notify = event => options.onNotification({
          method: 'session.event',
          params: { sessionId: options.sessionId, event },
        })
        notify({ type: 'assistant/chunk', data: { chunk: { type: 'reasoning-delta', text: 'SECRET_REASONING' } } })
        notify({ type: 'assistant/chunk', data: { chunk: { type: 'text-delta', text: '可见结论' } } })
        notify({ type: 'tool/call', data: { callId: 'call-direct', name: 'mcp__iot__query_alarm_list', arguments: { secret: true } } })
        notify({ type: 'tool/result', data: { callId: 'call-direct', result: 'SECRET_TOOL_RESULT' } })
        notify({ type: 'tool/call', data: { callId: 'call-legacy', name: 'mcp__iot__query_knowledge_base' } })
        notify({ type: 'tool/result', data: { message: { source: { callId: 'call-legacy' }, content: [{ text: 'SECRET_LEGACY_RESULT' }] } } })
        notify({ type: 'tool/call', data: { callId: 'call-draft', name: 'mcp__iot__create_rule_draft' } })
        notify({ type: 'tool/result', data: { message: { source: { callId: 'call-draft' }, content: [{ type:'tool-result', isError:false, content:[{ type:'text', text:JSON.stringify({ kind:'ruleDraft', persisted:true, draft:{ id:'draft-1', name:'高温联动', conditions:[{ field:'temperature', operator:'>', value:80 }], actions:[{ type:'OPEN_CAMERA', cameraId:'camera-001' }], internalSecret:'MUST_NOT_LEAK' } }) }] }] } } })
        return result('fallback must not duplicate streamed text')
      },
      async close() {},
    }
  })
  const response = await chat(baseUrl, requestBody({ model: 'legacy-model' }))
  assert.equal(response.status, 200)
  const { payload, events } = await ndjson(response)
  assert.deepEqual(events.map(event => event.type), [
    'run.started',
    'text.delta',
    'tool.started',
    'tool.completed',
    'tool.started',
    'tool.completed',
    'tool.started',
    'tool.completed',
    'run.completed',
  ])
  assert.equal(events[3].success, true)
  assert.equal(events[5].success, true)
  assert.equal(events[7].data.clientAction.type, 'RULE_DRAFT_READY')
  assert.equal(events[7].data.clientAction.draft.actions[0].cameraId, 'camera-001')
  assert.equal(events[7].data.clientAction.persisted, true)
  assert.doesNotMatch(payload, /SECRET_REASONING|SECRET_TOOL_RESULT|SECRET_LEGACY_RESULT|arguments/)
  assert.doesNotMatch(payload, /MUST_NOT_LEAK/)
  assert.equal(factorySpec.mcpToken, undefined)
  assert.equal(factorySpec.provider, 'ollama')
  assert.equal(factorySpec.model, 'qwen3:1.7b')
  assert.match(factorySpec.proxyMcpUrl, /^http:\/\/127\.0\.0\.1:\d+\/mcp$/)
  assert.equal(factorySpec.runtimeAccessKey.length, 43)
  assert.ok(factorySpec.plugin.persona.includes('AI 运维助手'))
  assert.equal(factorySpec.patchFile, join(deploymentDir, 'cordis.yml'))
  assert.ok(factorySpec.harnessHome.endsWith(`\\${factorySpec.sessionId}`) || factorySpec.harnessHome.endsWith(`/${factorySpec.sessionId}`))
})

test('upstream MCP path is exactly /mcp/harness', async () => {
  let factoryCalls = 0
  const { baseUrl } = await startGateway(async () => {
    factoryCalls += 1
    return { run: async () => result(), close: async () => {} }
  })
  const wrong = await chat(baseUrl, requestBody({ mcpUrl: 'http://platform-api:8080/mcp' }))
  assert.equal(wrong.status, 422)
  const correct = await chat(baseUrl, requestBody({ runId: 'run-correct' }))
  assert.equal(correct.status, 200)
  assert.equal(factoryCalls, 1)
})

test('loopback proxy rotates short-lived JWT without rebuilding the resident conversation', async () => {
  const authorizations = []
  const upstream = createServer(async (request, response) => {
    authorizations.push(request.headers.authorization)
    for await (const _chunk of request) { /* drain */ }
    response.writeHead(200, { 'content-type': 'application/json', 'mcp-session-id': 'test-session' })
    response.end('{"jsonrpc":"2.0","id":1,"result":{}}')
  })
  openedServers.push(upstream)
  await new Promise(resolve => upstream.listen(0, '127.0.0.1', resolve))
  const upstreamAddress = upstream.address()
  const mcpUrl = `http://127.0.0.1:${upstreamAddress.port}/mcp/harness`
  let factoryCalls = 0
  let sdkSessionId
  const { baseUrl } = await startGateway(async spec => {
    factoryCalls += 1
    return {
      async run(_question, options) {
        sdkSessionId ??= options.sessionId
        assert.equal(options.sessionId, sdkSessionId)
        const proxied = await fetch(spec.proxyMcpUrl, {
          method: 'POST',
          headers: {
            accept: 'application/json',
            'content-type': 'application/json',
            'x-iot-runtime-key': spec.runtimeAccessKey,
          },
          body: '{"jsonrpc":"2.0","id":1,"method":"tools/list"}',
        })
        assert.equal(proxied.status, 200)
        return result('ok')
      },
      async close() {},
    }
  }, { allowedMcpOrigins: new URL(mcpUrl).origin })

  const first = await chat(baseUrl, requestBody({ runId: 'run-jwt-1', mcpUrl }), 'jwt-generation-one')
  assert.equal(first.status, 200)
  await first.text()
  const second = await chat(baseUrl, requestBody({ runId: 'run-jwt-2', mcpUrl }), 'jwt-generation-two')
  assert.equal(second.status, 200)
  await second.text()

  assert.equal(factoryCalls, 1)
  assert.deepEqual(authorizations, ['Bearer jwt-generation-one', 'Bearer jwt-generation-two'])
})

test('requests sharing a conversation are serialized through one resident runtime', async () => {
  let running = 0
  let maximumRunning = 0
  let factoryCalls = 0
  const { baseUrl } = await startGateway(async () => {
    factoryCalls += 1
    return {
      async run() {
        running += 1
        maximumRunning = Math.max(maximumRunning, running)
        await new Promise(resolve => setTimeout(resolve, 30))
        running -= 1
        return result('done')
      },
      async close() {},
    }
  })
  const [first, second] = await Promise.all([
    chat(baseUrl, requestBody({ runId: 'run-serial-1' })),
    chat(baseUrl, requestBody({ runId: 'run-serial-2' })),
  ])
  assert.equal(first.status, 200)
  assert.equal(second.status, 200)
  await Promise.all([first.text(), second.text()])
  assert.equal(maximumRunning, 1)
  assert.equal(factoryCalls, 1)
})
