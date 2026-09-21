import test from 'node:test'
import assert from 'node:assert/strict'
import { once } from 'node:events'
import { createGateway, decodePlan, readEnv, signJWT, validateAnswer, timeReferences } from './gateway.mjs'
const config = { platformURL: 'http://platform.test', tenantId: 'tenant-a', keys: { 'ops-assistant': 'a'.repeat(64), 'system-observer': 'b'.repeat(64), 'protocol-assistant': 'c'.repeat(64) } }
const env = { IOT_JWT_SECRET: 'test-only-signing-key', IOT_ADMIN_PASSWORD: 'test-only-password' }
async function fixture(t, overrides = {}) {
  const calls = []
  const fetcher = async (url, options) => {
    const path = new URL(url).pathname
    if (path.endsWith('/auth/login')) return Response.json({ accessToken: 'internal-access', tenantId: 'tenant-a', role: 'admin' })
    if (path.endsWith('/knowledge-binding')) return Response.json({ retrievalMode: 'auto', topK: 3, minScore: 0.4, ...overrides })
    if (path.endsWith('/device-registry')) return Response.json({ items: [{ device: { id: 'device-a', name: '设备甲', status: 'ENABLED', password: 'must-not-be-forwarded', accessKey: 'private-access-key' }, runtimeState: { connectionStatus: 'CONNECTED', dataStatus: 'SILENT', businessStatus: 'OFFLINE', lastSeenAt: 1789614003489 }, credentialSupported: true }], total: 1 })
    if (path === '/mcp/harness') {
      const claims = JSON.parse(Buffer.from(options.headers.Authorization.split('.')[1], 'base64url'))
      const payload = JSON.parse(options.body)
      calls.push({ claims, params: payload.params })
      return Response.json({ jsonrpc: '2.0', id: payload.id, result: { content: [{ type: 'text', text: '[]' }] } })
    }
    throw new Error('unexpected request')
  }
  const server = await createGateway(config, env, fetcher)
  server.listen(0, '127.0.0.1'); await once(server, 'listening')
  t.after(() => new Promise(resolve => server.close(resolve)))
  async function post(path, body, key = config.keys['ops-assistant'], form = false) {
    const r = await fetch(`http://127.0.0.1:${server.address().port}${path}`, { method: 'POST', headers: { Authorization: `Bearer ${key}`, 'Content-Type': form ? 'application/x-www-form-urlencoded' : 'application/json' }, body: form ? new URLSearchParams(body) : JSON.stringify(body) })
    return { status: r.status, data: await r.json() }
  }
  return { post, calls }
}
test('dotenv parsing does not evaluate shell expressions', () => {
  assert.deepEqual(readEnv("# secret\nIOT_PORT='8081'\nOTHER=$(do-not-run)\n"), { IOT_PORT: '8081', OTHER: '$(do-not-run)' })
})
test('tool plans reject privilege escalation and preserve original draft request', () => {
  for (const calls of [[{ name: 'delete_alarm', arguments: {} }], [{ name: 'query_alarm_list', arguments: { tenantId: 'tenant-b' } }], Array(7).fill({ name: 'query_alarm_list', arguments: {} })]) {
    assert.throws(() => decodePlan(JSON.stringify({ calls }), ['query_alarm_list'], 'question'))
  }
  assert.deepEqual(decodePlan('{"calls":[{"name":"create_rule_draft","arguments":{"inputText":"changed"}}]}', ['create_rule_draft'], 'original')[0].arguments, { inputText: 'original' })
})
test('workflow credentials cannot be used for a different application', async t => {
  const { post, calls } = await fixture(t)
  const denied = await post('/v1/context/system-observer', { question: 'status' })
  assert.equal(denied.status, 401); assert.equal(calls.length, 0)
})
test('context uses tenant-bound short-lived exact-tool claims and redacts device fields', async t => {
  const { post, calls } = await fixture(t)
  const result = await post('/v1/context/ops-assistant', { question: 'alarms' })
  assert.equal(result.status, 200)
  assert.equal(result.data.data.devices.items[0].password, undefined)
  assert.deepEqual(result.data.data.devices.items[0], { id: 'device-a', name: '设备甲', status: 'ENABLED', connectionStatus: 'CONNECTED', dataStatus: 'SILENT', businessStatus: 'OFFLINE', lastSeenAt: 1789614003489 })
  assert.equal(result.data.data.devices.total, 1)
  assert.equal(result.data.data.devices.loaded, 1)
  assert.equal(result.data.data.devices.truncated, false)
  assert.ok(!JSON.stringify(result.data).includes('private-access-key'))
  for (const { claims, params } of calls) {
    assert.equal(claims.tenantId, 'tenant-a'); assert.equal(claims.exp - claims.iat, 120)
    assert.deepEqual(claims.scopes, ['mcp:tool:' + params.name])
    assert.equal(claims.knowledge.workflowId, 'ops-assistant')
    assert.equal(claims.knowledge.topK, 3); assert.equal(claims.knowledge.minScore, 0.4)
  }
  assert.ok(!JSON.stringify(result.data).includes(env.IOT_JWT_SECRET))
})
test('disabled knowledge is never queried and missing mandatory evidence fails', async t => {
  const disabled = await fixture(t, { retrievalMode: 'disabled' })
  assert.equal((await disabled.post('/v1/context/ops-assistant', { question: 'manual' })).status, 200)
  assert.ok(!disabled.calls.some(c => c.params.name === 'query_knowledge_base'))
  const required = await fixture(t, { noMatchPolicy: 'require-evidence' })
  assert.equal((await required.post('/v1/context/ops-assistant', { question: 'manual' })).status, 422)
})
test('protocol application cannot read alarms or save rule drafts', async t => {
  const { post, calls } = await fixture(t)
  const result = await post('/v1/tools/protocol-assistant', { question: 'draft', plan: '{"calls":[{"name":"create_rule_draft","arguments":{}}]}' }, config.keys['protocol-assistant'])
  assert.equal(result.status, 400); assert.equal(calls.length, 0)
  assert.equal(result.data.executed, false)
  assert.ok(!result.data.allowedTools.includes('create_rule_draft'))
})
test('generated JWT contains no admin or service password', () => {
  const jwt = signJWT('key', { tenantId: 'a', exp: 100 })
  assert.equal(jwt.split('.').length, 3)
  assert.deepEqual(JSON.parse(Buffer.from(jwt.split('.')[1], 'base64url')), { tenantId: 'a', exp: 100 })
})

test('Dify form transport preserves punctuation and original draft wording', async t => {
  const { post, calls } = await fixture(t)
  const question = '为“温度 > 80°C”生成草稿\n备注：A&B + "引号"'
  const result = await post('/v1/tools/ops-assistant', { question, plan: JSON.stringify({ calls: [{ name: 'create_rule_draft', arguments: {} }] }) }, config.keys['ops-assistant'], true)
  assert.equal(result.status, 200)
  assert.equal(calls[0].params.arguments.inputText, question)
})

test('reasoning prefix is excluded from strict JSON validation', () => {
  assert.deepEqual(decodePlan('<think>reasoning</think>\n```json\n{"calls":[]}\n```', [], 'q'), [])
  assert.throws(() => decodePlan('<think>unfinished {"calls":[]}', [], 'q'))
  assert.throws(() => decodePlan('prose {"calls":[]}', [], 'q'))
  const answer = { summary: '证据不足', riskLevel: 'INFO', confidence: 0, possibleReasons: [], suggestions: [], evidence: [], pendingConfirmation: ['需补充样本'] }
  assert.deepEqual(validateAnswer('alarm-handler', '<think>reasoning</think>' + JSON.stringify(answer)), answer)
  assert.throws(() => validateAnswer('alarm-handler', JSON.stringify({ ...answer, confidence: 2 })))
  assert.throws(() => validateAnswer('alarm-handler', JSON.stringify({ ...answer, evidence: 'invented' })))
  assert.throws(() => validateAnswer('protocol-assistant', '{}'))
})

test('business timestamps have deterministic UTC references; measurements are not dates', () => {
  const result = timeReferences([{ type: 'text', text: JSON.stringify({ lastSeenAt: 1789614003489, history: [{ timestamp: 1789614003747, temperature: 1789614003999 }] }) }])
  assert.equal(result[1789614003489], '2026-09-17T03:00:03.489Z')
  assert.equal(result[1789614003747], '2026-09-17T03:00:03.747Z')
  assert.equal(result[1789614003999], undefined)
})
