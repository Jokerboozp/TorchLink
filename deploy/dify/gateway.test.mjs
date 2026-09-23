import test from 'node:test' /* 引入当前代码需要的依赖。 */
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */
import { once } from 'node:events' /* 引入当前代码需要的依赖。 */
import { createGateway, decodePlan, readEnv, signJWT, validateAnswer, timeReferences } from './gateway.mjs' /* 引入当前代码需要的依赖。 */
const config = { platformURL: 'http://platform.test', tenantId: 'tenant-a', keys: { 'ops-assistant': 'a'.repeat(64), 'system-observer': 'b'.repeat(64), 'protocol-assistant': 'c'.repeat(64) } } /* 声明 config。 */
const env = { IOT_JWT_SECRET: 'test-only-signing-key', IOT_ADMIN_PASSWORD: 'test-only-password' } /* 声明 env。 */
async function fixture(t, overrides = {}) { /* 定义 fixture 函数。 */
  const calls = [] /* 声明 calls。 */
  const fetcher = async (url, options) => { /* 声明 fetcher。 */
    const path = new URL(url).pathname /* 声明 path。 */
    if (path.endsWith('/auth/login')) return Response.json({ accessToken: 'internal-access', tenantId: 'tenant-a', role: 'admin' }) /* 判断条件并选择处理分支。 */
    if (path.endsWith('/knowledge-binding')) return Response.json({ retrievalMode: 'auto', topK: 3, minScore: 0.4, ...overrides }) /* 判断条件并选择处理分支。 */
    if (path.endsWith('/device-registry')) return Response.json({ items: [{ device: { id: 'device-a', name: '设备甲', status: 'ENABLED', password: 'must-not-be-forwarded', accessKey: 'private-access-key' }, runtimeState: { connectionStatus: 'CONNECTED', dataStatus: 'SILENT', businessStatus: 'OFFLINE', lastSeenAt: 1789614003489 }, credentialSupported: true }], total: 1 }) /* 判断条件并选择处理分支。 */
    if (path === '/mcp/harness') { /* 判断条件并选择处理分支。 */
      const claims = JSON.parse(Buffer.from(options.headers.Authorization.split('.')[1], 'base64url')) /* 声明 claims。 */
      const payload = JSON.parse(options.body) /* 声明 payload。 */
      calls.push({ claims, params: payload.params }) /* 执行当前语句并推进处理流程。 */
      return Response.json({ jsonrpc: '2.0', id: payload.id, result: { content: [{ type: 'text', text: '[]' }] } }) /* 返回当前处理结果。 */
    } /* 结束当前表达式或代码块。 */
    throw new Error('unexpected request') /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  const server = await createGateway(config, env, fetcher) /* 声明 server。 */
  server.listen(0, '127.0.0.1'); await once(server, 'listening') /* 执行当前语句并推进处理流程。 */
  t.after(() => new Promise(resolve => server.close(resolve))) /* 执行当前语句并推进处理流程。 */
  async function post(path, body, key = config.keys['ops-assistant'], form = false) { /* 定义 post 函数。 */
    const r = await fetch(`http://127.0.0.1:${server.address().port}${path}`, { method: 'POST', headers: { Authorization: `Bearer ${key}`, 'Content-Type': form ? 'application/x-www-form-urlencoded' : 'application/json' }, body: form ? new URLSearchParams(body) : JSON.stringify(body) }) /* 声明 r。 */
    return { status: r.status, data: await r.json() } /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
  return { post, calls } /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
test('dotenv parsing does not evaluate shell expressions', () => { /* 执行当前语句并推进处理流程。 */
  assert.deepEqual(readEnv("# secret\nIOT_PORT='8081'\nOTHER=$(do-not-run)\n"), { IOT_PORT: '8081', OTHER: '$(do-not-run)' }) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
test('tool plans reject privilege escalation and preserve original draft request', () => { /* 执行当前语句并推进处理流程。 */
  for (const calls of [[{ name: 'delete_alarm', arguments: {} }], [{ name: 'query_alarm_list', arguments: { tenantId: 'tenant-b' } }], Array(7).fill({ name: 'query_alarm_list', arguments: {} })]) { /* 循环处理当前数据。 */
    assert.throws(() => decodePlan(JSON.stringify({ calls }), ['query_alarm_list'], 'question')) /* 验证实际结果符合预期。 */
  } /* 结束当前表达式或代码块。 */
  assert.deepEqual(decodePlan('{"calls":[{"name":"create_rule_draft","arguments":{"inputText":"changed"}}]}', ['create_rule_draft'], 'original')[0].arguments, { inputText: 'original' }) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
test('workflow credentials cannot be used for a different application', async t => { /* 执行当前语句并推进处理流程。 */
  const { post, calls } = await fixture(t) /* 执行当前语句并推进处理流程。 */
  const denied = await post('/v1/context/system-observer', { question: 'status' }) /* 声明 denied。 */
  assert.equal(denied.status, 401); assert.equal(calls.length, 0) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
test('context uses tenant-bound short-lived exact-tool claims and redacts device fields', async t => { /* 执行当前语句并推进处理流程。 */
  const { post, calls } = await fixture(t) /* 执行当前语句并推进处理流程。 */
  const result = await post('/v1/context/ops-assistant', { question: 'alarms' }) /* 声明 result。 */
  assert.equal(result.status, 200) /* 验证实际结果符合预期。 */
  assert.equal(result.data.data.devices.items[0].password, undefined) /* 验证实际结果符合预期。 */
  assert.deepEqual(result.data.data.devices.items[0], { id: 'device-a', name: '设备甲', status: 'ENABLED', connectionStatus: 'CONNECTED', dataStatus: 'SILENT', businessStatus: 'OFFLINE', lastSeenAt: 1789614003489 }) /* 验证实际结果符合预期。 */
  assert.equal(result.data.data.devices.total, 1) /* 验证实际结果符合预期。 */
  assert.equal(result.data.data.devices.loaded, 1) /* 验证实际结果符合预期。 */
  assert.equal(result.data.data.devices.truncated, false) /* 验证实际结果符合预期。 */
  assert.ok(!JSON.stringify(result.data).includes('private-access-key')) /* 验证实际结果符合预期。 */
  for (const { claims, params } of calls) { /* 循环处理当前数据。 */
    assert.equal(claims.tenantId, 'tenant-a'); assert.equal(claims.exp - claims.iat, 120) /* 验证实际结果符合预期。 */
    assert.deepEqual(claims.scopes, ['mcp:tool:' + params.name]) /* 验证实际结果符合预期。 */
    assert.equal(claims.knowledge.workflowId, 'ops-assistant') /* 验证实际结果符合预期。 */
    assert.equal(claims.knowledge.topK, 3); assert.equal(claims.knowledge.minScore, 0.4) /* 验证实际结果符合预期。 */
  } /* 结束当前表达式或代码块。 */
  assert.ok(!JSON.stringify(result.data).includes(env.IOT_JWT_SECRET)) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
test('disabled knowledge is never queried and missing mandatory evidence fails', async t => { /* 执行当前语句并推进处理流程。 */
  const disabled = await fixture(t, { retrievalMode: 'disabled' }) /* 声明 disabled。 */
  assert.equal((await disabled.post('/v1/context/ops-assistant', { question: 'manual' })).status, 200) /* 验证实际结果符合预期。 */
  assert.ok(!disabled.calls.some(c => c.params.name === 'query_knowledge_base')) /* 验证实际结果符合预期。 */
  const required = await fixture(t, { noMatchPolicy: 'require-evidence' }) /* 声明 required。 */
  assert.equal((await required.post('/v1/context/ops-assistant', { question: 'manual' })).status, 422) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
test('protocol application cannot read alarms or save rule drafts', async t => { /* 执行当前语句并推进处理流程。 */
  const { post, calls } = await fixture(t) /* 执行当前语句并推进处理流程。 */
  const result = await post('/v1/tools/protocol-assistant', { question: 'draft', plan: '{"calls":[{"name":"create_rule_draft","arguments":{}}]}' }, config.keys['protocol-assistant']) /* 声明 result。 */
  assert.equal(result.status, 400); assert.equal(calls.length, 0) /* 验证实际结果符合预期。 */
  assert.equal(result.data.executed, false) /* 验证实际结果符合预期。 */
  assert.ok(!result.data.allowedTools.includes('create_rule_draft')) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
test('generated JWT contains no admin or service password', () => { /* 执行当前语句并推进处理流程。 */
  const jwt = signJWT('key', { tenantId: 'a', exp: 100 }) /* 声明 jwt。 */
  assert.equal(jwt.split('.').length, 3) /* 验证实际结果符合预期。 */
  assert.deepEqual(JSON.parse(Buffer.from(jwt.split('.')[1], 'base64url')), { tenantId: 'a', exp: 100 }) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('Dify form transport preserves punctuation and original draft wording', async t => { /* 执行当前语句并推进处理流程。 */
  const { post, calls } = await fixture(t) /* 执行当前语句并推进处理流程。 */
  const question = '为“温度 > 80°C”生成草稿\n备注：A&B + "引号"' /* 声明 question。 */
  const result = await post('/v1/tools/ops-assistant', { question, plan: JSON.stringify({ calls: [{ name: 'create_rule_draft', arguments: {} }] }) }, config.keys['ops-assistant'], true) /* 声明 result。 */
  assert.equal(result.status, 200) /* 验证实际结果符合预期。 */
  assert.equal(calls[0].params.arguments.inputText, question) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('reasoning prefix is excluded from strict JSON validation', () => { /* 执行当前语句并推进处理流程。 */
  assert.deepEqual(decodePlan('<think>reasoning</think>\n```json\n{"calls":[]}\n```', [], 'q'), []) /* 验证实际结果符合预期。 */
  assert.throws(() => decodePlan('<think>unfinished {"calls":[]}', [], 'q')) /* 验证实际结果符合预期。 */
  assert.throws(() => decodePlan('prose {"calls":[]}', [], 'q')) /* 验证实际结果符合预期。 */
  const answer = { summary: '证据不足', riskLevel: 'INFO', confidence: 0, possibleReasons: [], suggestions: [], evidence: [], pendingConfirmation: ['需补充样本'] } /* 声明 answer。 */
  assert.deepEqual(validateAnswer('alarm-handler', '<think>reasoning</think>' + JSON.stringify(answer)), answer) /* 验证实际结果符合预期。 */
  assert.throws(() => validateAnswer('alarm-handler', JSON.stringify({ ...answer, confidence: 2 }))) /* 验证实际结果符合预期。 */
  assert.throws(() => validateAnswer('alarm-handler', JSON.stringify({ ...answer, evidence: 'invented' }))) /* 验证实际结果符合预期。 */
  assert.throws(() => validateAnswer('protocol-assistant', '{}')) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('business timestamps have deterministic UTC references; measurements are not dates', () => { /* 执行当前语句并推进处理流程。 */
  const result = timeReferences([{ type: 'text', text: JSON.stringify({ lastSeenAt: 1789614003489, history: [{ timestamp: 1789614003747, temperature: 1789614003999 }] }) }]) /* 声明 result。 */
  assert.equal(result[1789614003489], '2026-09-17T03:00:03.489Z') /* 验证实际结果符合预期。 */
  assert.equal(result[1789614003747], '2026-09-17T03:00:03.747Z') /* 验证实际结果符合预期。 */
  assert.equal(result[1789614003999], undefined) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
