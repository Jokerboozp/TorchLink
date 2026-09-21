import { createServer } from 'node:http'
import { createHmac, randomUUID, timingSafeEqual } from 'node:crypto'
import { readFile } from 'node:fs/promises'
import { resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const maxBytes = 1024 * 1024
export function readEnv(text) {
  return Object.fromEntries(text.split(/\r?\n/).flatMap(line => {
    const m = line.match(/^\s*([A-Z0-9_]+)\s*=\s*(.*?)\s*$/)
    if (!m) return []
    let value = m[2]
    if ((value.startsWith("'") && value.endsWith("'")) || (value.startsWith('"') && value.endsWith('"'))) value = value.slice(1, -1)
    return [[m[1], value]]
  }))
}
export function signJWT(secret, claims) {
  const encode = v => Buffer.from(JSON.stringify(v)).toString('base64url')
  const payload = `${encode({ alg: 'HS256', typ: 'JWT' })}.${encode(claims)}`
  return `${payload}.${createHmac('sha256', secret).update(payload).digest('base64url')}`
}
function same(a, b) {
  const aa = Buffer.from(a ?? ''), bb = Buffer.from(b ?? '')
  return aa.length === bb.length && timingSafeEqual(aa, bb)
}
function modelJSON(raw) {
  // DeepSeek's Dify plugin can prefix text with a complete reasoning block.
  // Strip only that prefix; never search for arbitrary JSON inside prose.
  const answer = raw.trim().replace(/^<think>[\s\S]*?<\/think>\s*/, '')
  return JSON.parse(answer.replace(/^```(?:json)?\s*/i, '').replace(/\s*```$/, ''))
}
export function decodePlan(raw, allowed, question) {
  if (typeof raw !== 'string' || raw.length > 24000) throw new Error('invalid plan')
  const plan = modelJSON(raw)
  if (!Array.isArray(plan.calls) || plan.calls.length > 6) throw new Error('at most six tool calls are allowed')
  return plan.calls.map(call => {
    if (!call || !allowed.includes(call.name) || !call.arguments || Array.isArray(call.arguments) || typeof call.arguments !== 'object') throw new Error('tool not authorized')
    const args = { ...call.arguments }
    for (const key of ['tenantId', 'tenant', 'userId', 'username', 'url', 'token']) {
      if (key in args) throw new Error('identity and URLs cannot be supplied by the model')
    }
    if (call.name === 'create_rule_draft') {
      // A model cannot substitute a different instruction for the user's words.
      args.inputText = question
    }
    return { name: call.name, arguments: args }
  })
}
export function validateAnswer(id, raw) {
  if (typeof raw !== 'string' || raw.length > 64000) throw new Error('invalid answer')
  const data = modelJSON(raw)
  if (!data || Array.isArray(data) || typeof data !== 'object') throw new Error('expected an object')
  if (id === 'alarm-handler') {
    if (typeof data.summary !== 'string' || !data.summary.trim()) throw new Error('missing summary')
    if (!['CRITICAL', 'HIGH', 'MEDIUM', 'LOW', 'INFO'].includes(data.riskLevel)) throw new Error('invalid risk')
    if (typeof data.confidence !== 'number' || !Number.isFinite(data.confidence) || data.confidence < 0 || data.confidence > 1) throw new Error('invalid confidence')
    for (const key of ['possibleReasons', 'suggestions', 'evidence', 'pendingConfirmation']) {
      if (!Array.isArray(data[key]) || !data[key].every(v => typeof v === 'string')) throw new Error('invalid evidence array')
    }
  } else if (id === 'protocol-assistant') {
    if (!Array.isArray(data.fields)) throw new Error('missing fields')
  } else throw new Error('validation not supported')
  return data
}
export function timeReferences(content) {
  const references = {}
  function visit(value, key = '') {
    if (typeof value === 'number' && /(?:At|timestamp|time)$/i.test(key) && value >= 1000000000000 && value < 4102444800000) {
      references[value] = new Date(value).toISOString()
    } else if (Array.isArray(value)) value.forEach(v => visit(v))
    else if (value && typeof value === 'object') Object.entries(value).forEach(([k, v]) => visit(v, k))
  }
  for (const item of content ?? []) {
    if (item.type !== 'text') continue
    try { visit(JSON.parse(item.text)) } catch { /* Plain tool text has no structured timestamps. */ }
  }
  return references
}
export async function createGateway(config, env, fetcher = fetch) {
  const base = new URL(config.platformURL)
  if (!['http:', 'https:'].includes(base.protocol) || base.username || base.password || base.search || base.hash) throw new Error('invalid platform URL')
  if (!env.IOT_JWT_SECRET || !env.IOT_ADMIN_PASSWORD || !config.tenantId) throw new Error('platform credentials and tenant are required')
  const profiles = new Map()
  for (const [id, key] of Object.entries(config.keys ?? {})) {
    if (!/^[a-z][a-z0-9-]+$/.test(id) || typeof key !== 'string' || key.length < 48) throw new Error('invalid workflow credential')
    const manifest = JSON.parse(await readFile(resolve(here, '../deepseek-harness/plugins', id + '.json'), 'utf8'))
    profiles.set(id, { ...manifest, tools: manifest.allowedTools.map(t => t.replace('mcp__iot__', '')) })
  }
  let accessToken = '', expires = 0
  async function request(path, options = {}) {
    const response = await fetcher(new URL(path, base), { redirect: 'error', signal: AbortSignal.timeout(90000), ...options })
    if (!response.ok) throw new Error(`platform request failed (${response.status})`)
    const bytes = await response.text()
    if (bytes.length > 2 * maxBytes) throw new Error('platform response exceeds limit')
    return JSON.parse(bytes)
  }
  async function login() {
    if (Date.now() < expires) return
    const result = await request('/api/v1/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username: env.IOT_ADMIN_USER || 'admin', password: env.IOT_ADMIN_PASSWORD, tenantId: config.tenantId }) })
    if (result.tenantId !== config.tenantId || result.role !== 'admin' || !result.accessToken) throw new Error('integration requires the configured tenant administrator')
    accessToken = result.accessToken
    expires = Date.now() + 3600000
  }
  async function api(path) {
    await login()
    return request(path, { headers: { Authorization: `Bearer ${accessToken}` } })
  }
  async function bindingFor(id) {
    return api(`/api/v1/ai/workflows/${id}/knowledge-binding`)
  }
  async function tool(profile, call, binding) {
    if (!profile.tools.includes(call.name)) throw new Error('tool not authorized')
    if (call.name === 'query_knowledge_base' && binding.retrievalMode === 'disabled') return { name: call.name, success: false, error: '当前 Agent 已禁用知识检索' }
    const now = Math.floor(Date.now() / 1000)
    const token = signJWT(env.IOT_JWT_SECRET, {
      iss: 'iot-platform', sub: `harness:dify:${profile.id}`, username: `dify:${profile.id}`,
      tenantId: config.tenantId, role: 'viewer', tokenUse: 'harness', aud: ['iot-platform-mcp'],
      runId: 'dify_' + randomUUID(), iat: now, exp: now + 120,
      scopes: [`mcp:tool:${call.name}`],
      knowledge: { workflowId: profile.id, topK: binding.topK || 5, minScore: binding.minScore ?? 0.25 },
    })
    const result = await request('/mcp/harness', {
      method: 'POST', headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream' },
      body: JSON.stringify({ jsonrpc: '2.0', id: randomUUID(), method: 'tools/call', params: call }),
    })
    if (result.error || !result.result) return { name: call.name, success: false, error: '平台工具调用失败' }
    return { name: call.name, success: !result.result.isError, content: result.result.content, timestampConversionsUTC: timeReferences(result.result.content) }
  }
  // Validate ownership before opening the network listener.
  await login()
  const server = createServer(async (req, res) => {
    const send = (status, body) => { res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(body)) }
    if (req.url === '/health' && req.method === 'GET') return send(200, { status: 'ok' })
    const route = req.url?.match(/^\/v1\/(context|tools|validate)\/([a-z][a-z0-9-]+)$/)
    if (req.method !== 'POST' || !route || !profiles.has(route[2])) return send(404, { error: 'not found' })
    const profile = profiles.get(route[2])
    if (!same(req.headers.authorization, `Bearer ${config.keys[profile.id]}`)) return send(401, { error: 'unauthorized' })
    if (config.allowedClients?.length && !config.allowedClients.includes(req.socket.remoteAddress?.replace(/^::ffff:/, ''))) return send(403, { error: 'client not allowed' })
    try {
      let size = 0, parts = []
      for await (const part of req) { size += part.length; if (size > maxBytes) return send(413, { error: 'request too large' }); parts.push(part) }
      const body = Buffer.concat(parts).toString()
      const input = req.headers['content-type']?.startsWith('application/x-www-form-urlencoded')
        ? Object.fromEntries(new URLSearchParams(body)) : JSON.parse(body)
      if (typeof input.question !== 'string' || !input.question.trim() || input.question.length > 32000) return send(400, { error: 'question is required (maximum 32000 characters)' })
      if (route[1] === 'validate') {
        try { return send(200, validateAnswer(profile.id, input.answer)) }
        catch { return send(422, { error: '模型结果未通过结构校验，请重新运行或补充资料' }) }
      }
      const binding = await bindingFor(profile.id)
      if (route[1] === 'context') {
        const evidence = { observedAt: new Date().toISOString(), workflowId: profile.id, knowledgePolicy: binding, tools: profile.tools, data: {} }
        if (profile.id !== 'protocol-assistant') {
          const devices = await api('/api/v1/device-registry?limit=100')
          const list = Array.isArray(devices) ? devices : devices.items ?? []
          evidence.data.devices = { items: list.slice(0, 100).map(row => {
            // The registry API returns { device, runtimeState, ... }, not a flat device.
            // Keep an explicit allowlist: registration credentials must never reach a model.
            const d = row.device ?? row, state = row.runtimeState ?? {}
            return { id: d.id, name: d.name, productId: d.productId, status: d.status,
              connectionStatus: state.connectionStatus, dataStatus: state.dataStatus,
              businessStatus: state.businessStatus, lastSeenAt: state.lastSeenAt }
          }), total: devices.total ?? list.length, loaded: Math.min(list.length, 100), truncated: list.length > 100 || (devices.total ?? 0) > 100 }
          evidence.data.alarms = await tool(profile, { name: 'query_alarm_list', arguments: { limit: 20 } }, binding)
        }
        if (profile.tools.includes('query_system_overview')) evidence.data.overview = await tool(profile, { name: 'query_system_overview', arguments: {} }, binding)
        if (profile.tools.includes('query_knowledge_base') && binding.retrievalMode !== 'disabled') {
          evidence.data.knowledge = await tool(profile, { name: 'query_knowledge_base', arguments: { question: input.question, workflowId: profile.id } }, binding)
          if (binding.noMatchPolicy === 'require-evidence') {
            const item = evidence.data.knowledge
            const hits = item.success ? JSON.parse(item.content?.find(c => c.type === 'text')?.text || '[]') : []
            if (!Array.isArray(hits) || !hits.length) return send(422, { error: '当前 Agent 要求知识证据，但没有召回匹配内容' })
          }
        }
        return send(200, evidence)
      }
      let calls
      try { calls = decodePlan(input.plan, profile.tools, input.question) }
      catch {
        return send(400, { error: '工具计划无效：只可使用当前业务的允许工具，每次最多六项，不能传入租户、用户或 URL。请按 allowedTools 修正计划；告警研判使用 iot_alarm，设备详情使用 iot_ops 或 iot_health。', allowedTools: profile.tools, executed: false })
      }
      const results = []
      for (const call of calls) results.push(await tool(profile, call, binding))
      return send(200, { observedAt: new Date().toISOString(), results })
    } catch (error) {
      // Never include credentials, upstream response bodies or model input in errors/logs.
      return send(502, { error: error instanceof SyntaxError ? 'invalid JSON input or tool plan' : 'IoT 数据查询失败，请检查平台及该 Agent 的知识配置' })
    }
  })
  server.requestTimeout = 180000
  server.headersTimeout = 10000
  return server
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const config = JSON.parse(await readFile(resolve(process.argv[2] || 'data/dify/config.json'), 'utf8'))
  const env = { ...readEnv(await readFile(config.envFile || '.env.local', 'utf8')), ...process.env }
  const server = await createGateway(config, env)
  server.listen(config.port || 8092, config.host || '127.0.0.1', () => console.log(`Dify IoT gateway listening on ${config.host || '127.0.0.1'}:${config.port || 8092}`))
}
