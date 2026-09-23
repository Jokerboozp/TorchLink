import { createServer } from 'node:http' /* 引入当前代码需要的依赖。 */
import { createHmac, randomUUID, timingSafeEqual } from 'node:crypto' /* 引入当前代码需要的依赖。 */
import { readFile } from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import { resolve, dirname } from 'node:path' /* 引入当前代码需要的依赖。 */
import { fileURLToPath } from 'node:url' /* 引入当前代码需要的依赖。 */

const here = dirname(fileURLToPath(import.meta.url)) /* 声明 here。 */
const maxBytes = 1024 * 1024 /* 声明 maxBytes。 */
export function readEnv(text) { /* 执行当前语句并推进处理流程。 */
  return Object.fromEntries(text.split(/\r?\n/).flatMap(line => { /* 返回当前处理结果。 */
    const m = line.match(/^\s*([A-Z0-9_]+)\s*=\s*(.*?)\s*$/) /* 声明 m。 */
    if (!m) return [] /* 判断条件并选择处理分支。 */
    let value = m[2] /* 声明 value。 */
    if ((value.startsWith("'") && value.endsWith("'")) || (value.startsWith('"') && value.endsWith('"'))) value = value.slice(1, -1) /* 判断条件并选择处理分支。 */
    return [[m[1], value]] /* 返回当前处理结果。 */
  })) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
export function signJWT(secret, claims) { /* 执行当前语句并推进处理流程。 */
  const encode = v => Buffer.from(JSON.stringify(v)).toString('base64url') /* 声明 encode。 */
  const payload = `${encode({ alg: 'HS256', typ: 'JWT' })}.${encode(claims)}` /* 声明 payload。 */
  return `${payload}.${createHmac('sha256', secret).update(payload).digest('base64url')}` /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
function same(a, b) { /* 定义 same 函数。 */
  const aa = Buffer.from(a ?? ''), bb = Buffer.from(b ?? '') /* 声明 aa。 */
  return aa.length === bb.length && timingSafeEqual(aa, bb) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
function modelJSON(raw) { /* 定义 modelJSON 函数。 */
  // DeepSeek's Dify plugin can prefix text with a complete reasoning block.
  // Strip only that prefix; never search for arbitrary JSON inside prose.
  const answer = raw.trim().replace(/^<think>[\s\S]*?<\/think>\s*/, '') /* 声明 answer。 */
  return JSON.parse(answer.replace(/^```(?:json)?\s*/i, '').replace(/\s*```$/, '')) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
export function decodePlan(raw, allowed, question) { /* 执行当前语句并推进处理流程。 */
  if (typeof raw !== 'string' || raw.length > 24000) throw new Error('invalid plan') /* 判断条件并选择处理分支。 */
  const plan = modelJSON(raw) /* 声明 plan。 */
  if (!Array.isArray(plan.calls) || plan.calls.length > 6) throw new Error('at most six tool calls are allowed') /* 判断条件并选择处理分支。 */
  return plan.calls.map(call => { /* 返回当前处理结果。 */
    if (!call || !allowed.includes(call.name) || !call.arguments || Array.isArray(call.arguments) || typeof call.arguments !== 'object') throw new Error('tool not authorized') /* 判断条件并选择处理分支。 */
    const args = { ...call.arguments } /* 声明 args。 */
    for (const key of ['tenantId', 'tenant', 'userId', 'username', 'url', 'token']) { /* 循环处理当前数据。 */
      if (key in args) throw new Error('identity and URLs cannot be supplied by the model') /* 判断条件并选择处理分支。 */
    } /* 结束当前表达式或代码块。 */
    if (call.name === 'create_rule_draft') { /* 判断条件并选择处理分支。 */
      // A model cannot substitute a different instruction for the user's words.
      args.inputText = question /* 更新 args.inputText 的值。 */
    } /* 结束当前表达式或代码块。 */
    return { name: call.name, arguments: args } /* 返回当前处理结果。 */
  }) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
export function validateAnswer(id, raw) { /* 执行当前语句并推进处理流程。 */
  if (typeof raw !== 'string' || raw.length > 64000) throw new Error('invalid answer') /* 判断条件并选择处理分支。 */
  const data = modelJSON(raw) /* 声明 data。 */
  if (!data || Array.isArray(data) || typeof data !== 'object') throw new Error('expected an object') /* 判断条件并选择处理分支。 */
  if (id === 'alarm-handler') { /* 判断条件并选择处理分支。 */
    if (typeof data.summary !== 'string' || !data.summary.trim()) throw new Error('missing summary') /* 判断条件并选择处理分支。 */
    if (!['CRITICAL', 'HIGH', 'MEDIUM', 'LOW', 'INFO'].includes(data.riskLevel)) throw new Error('invalid risk') /* 判断条件并选择处理分支。 */
    if (typeof data.confidence !== 'number' || !Number.isFinite(data.confidence) || data.confidence < 0 || data.confidence > 1) throw new Error('invalid confidence') /* 判断条件并选择处理分支。 */
    for (const key of ['possibleReasons', 'suggestions', 'evidence', 'pendingConfirmation']) { /* 循环处理当前数据。 */
      if (!Array.isArray(data[key]) || !data[key].every(v => typeof v === 'string')) throw new Error('invalid evidence array') /* 判断条件并选择处理分支。 */
    } /* 结束当前表达式或代码块。 */
  } else if (id === 'protocol-assistant') { /* 结束当前表达式或代码块。 */
    if (!Array.isArray(data.fields)) throw new Error('missing fields') /* 判断条件并选择处理分支。 */
  } else throw new Error('validation not supported') /* 结束当前表达式或代码块。 */
  return data /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
export function timeReferences(content) { /* 执行当前语句并推进处理流程。 */
  const references = {} /* 声明 references。 */
  function visit(value, key = '') { /* 定义 visit 函数。 */
    if (typeof value === 'number' && /(?:At|timestamp|time)$/i.test(key) && value >= 1000000000000 && value < 4102444800000) { /* 判断条件并选择处理分支。 */
      references[value] = new Date(value).toISOString() /* 更新 references[value] 的值。 */
    } else if (Array.isArray(value)) value.forEach(v => visit(v)) /* 结束当前表达式或代码块。 */
    else if (value && typeof value === 'object') Object.entries(value).forEach(([k, v]) => visit(v, k)) /* 判断条件并选择处理分支。 */
  } /* 结束当前表达式或代码块。 */
  for (const item of content ?? []) { /* 循环处理当前数据。 */
    if (item.type !== 'text') continue /* 判断条件并选择处理分支。 */
    try { visit(JSON.parse(item.text)) } catch { /* Plain tool text has no structured timestamps. */ } /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  return references /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
export async function createGateway(config, env, fetcher = fetch) { /* 执行当前语句并推进处理流程。 */
  const base = new URL(config.platformURL) /* 声明 base。 */
  if (!['http:', 'https:'].includes(base.protocol) || base.username || base.password || base.search || base.hash) throw new Error('invalid platform URL') /* 判断条件并选择处理分支。 */
  if (!env.IOT_JWT_SECRET || !env.IOT_ADMIN_PASSWORD || !config.tenantId) throw new Error('platform credentials and tenant are required') /* 判断条件并选择处理分支。 */
  const profiles = new Map() /* 声明 profiles。 */
  for (const [id, key] of Object.entries(config.keys ?? {})) { /* 循环处理当前数据。 */
    if (!/^[a-z][a-z0-9-]+$/.test(id) || typeof key !== 'string' || key.length < 48) throw new Error('invalid workflow credential') /* 判断条件并选择处理分支。 */
    const manifest = JSON.parse(await readFile(resolve(here, '../deepseek-harness/plugins', id + '.json'), 'utf8')) /* 声明 manifest。 */
    profiles.set(id, { ...manifest, tools: manifest.allowedTools.map(t => t.replace('mcp__iot__', '')) }) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  let accessToken = '', expires = 0 /* 声明 accessToken。 */
  async function request(path, options = {}) { /* 定义 request 函数。 */
    const response = await fetcher(new URL(path, base), { redirect: 'error', signal: AbortSignal.timeout(90000), ...options }) /* 声明 response。 */
    if (!response.ok) throw new Error(`platform request failed (${response.status})`) /* 判断条件并选择处理分支。 */
    const bytes = await response.text() /* 声明 bytes。 */
    if (bytes.length > 2 * maxBytes) throw new Error('platform response exceeds limit') /* 判断条件并选择处理分支。 */
    return JSON.parse(bytes) /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
  async function login() { /* 定义 login 函数。 */
    if (Date.now() < expires) return /* 判断条件并选择处理分支。 */
    const result = await request('/api/v1/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username: env.IOT_ADMIN_USER || 'admin', password: env.IOT_ADMIN_PASSWORD, tenantId: config.tenantId }) }) /* 声明 result。 */
    if (result.tenantId !== config.tenantId || result.role !== 'admin' || !result.accessToken) throw new Error('integration requires the configured tenant administrator') /* 判断条件并选择处理分支。 */
    accessToken = result.accessToken /* 更新 accessToken 的值。 */
    expires = Date.now() + 3600000 /* 更新 expires 的值。 */
  } /* 结束当前表达式或代码块。 */
  async function api(path) { /* 定义 api 函数。 */
    await login() /* 等待异步操作完成。 */
    return request(path, { headers: { Authorization: `Bearer ${accessToken}` } }) /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
  async function bindingFor(id) { /* 定义 bindingFor 函数。 */
    return api(`/api/v1/ai/workflows/${id}/knowledge-binding`) /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
  async function tool(profile, call, binding) { /* 定义 tool 函数。 */
    if (!profile.tools.includes(call.name)) throw new Error('tool not authorized') /* 判断条件并选择处理分支。 */
    if (call.name === 'query_knowledge_base' && binding.retrievalMode === 'disabled') return { name: call.name, success: false, error: '当前 Agent 已禁用知识检索' } /* 判断条件并选择处理分支。 */
    const now = Math.floor(Date.now() / 1000) /* 声明 now。 */
    const token = signJWT(env.IOT_JWT_SECRET, { /* 声明 token。 */
      iss: 'iot-platform', sub: `harness:dify:${profile.id}`, username: `dify:${profile.id}`, /* 执行当前语句并推进处理流程。 */
      tenantId: config.tenantId, role: 'viewer', tokenUse: 'harness', aud: ['iot-platform-mcp'], /* 执行当前语句并推进处理流程。 */
      runId: 'dify_' + randomUUID(), iat: now, exp: now + 120, /* 执行当前语句并推进处理流程。 */
      scopes: [`mcp:tool:${call.name}`], /* 执行当前语句并推进处理流程。 */
      knowledge: { workflowId: profile.id, topK: binding.topK || 5, minScore: binding.minScore ?? 0.25 }, /* 执行当前语句并推进处理流程。 */
    }) /* 结束当前表达式或代码块。 */
    const result = await request('/mcp/harness', { /* 声明 result。 */
      method: 'POST', headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream' }, /* 执行当前语句并推进处理流程。 */
      body: JSON.stringify({ jsonrpc: '2.0', id: randomUUID(), method: 'tools/call', params: call }), /* 执行当前语句并推进处理流程。 */
    }) /* 结束当前表达式或代码块。 */
    if (result.error || !result.result) return { name: call.name, success: false, error: '平台工具调用失败' } /* 判断条件并选择处理分支。 */
    return { name: call.name, success: !result.result.isError, content: result.result.content, timestampConversionsUTC: timeReferences(result.result.content) } /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
  // Validate ownership before opening the network listener.
  await login() /* 等待异步操作完成。 */
  const server = createServer(async (req, res) => { /* 声明 server。 */
    const send = (status, body) => { res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(body)) } /* 声明 send。 */
    if (req.url === '/health' && req.method === 'GET') return send(200, { status: 'ok' }) /* 判断条件并选择处理分支。 */
    const route = req.url?.match(/^\/v1\/(context|tools|validate)\/([a-z][a-z0-9-]+)$/) /* 声明 route。 */
    if (req.method !== 'POST' || !route || !profiles.has(route[2])) return send(404, { error: 'not found' }) /* 判断条件并选择处理分支。 */
    const profile = profiles.get(route[2]) /* 声明 profile。 */
    if (!same(req.headers.authorization, `Bearer ${config.keys[profile.id]}`)) return send(401, { error: 'unauthorized' }) /* 判断条件并选择处理分支。 */
    if (config.allowedClients?.length && !config.allowedClients.includes(req.socket.remoteAddress?.replace(/^::ffff:/, ''))) return send(403, { error: 'client not allowed' }) /* 判断条件并选择处理分支。 */
    try { /* 执行当前语句并推进处理流程。 */
      let size = 0, parts = [] /* 声明 size。 */
      for await (const part of req) { size += part.length; if (size > maxBytes) return send(413, { error: 'request too large' }); parts.push(part) } /* 循环处理当前数据。 */
      const body = Buffer.concat(parts).toString() /* 声明 body。 */
      const input = req.headers['content-type']?.startsWith('application/x-www-form-urlencoded') /* 声明 input。 */
        ? Object.fromEntries(new URLSearchParams(body)) : JSON.parse(body) /* 执行当前语句并推进处理流程。 */
      if (typeof input.question !== 'string' || !input.question.trim() || input.question.length > 32000) return send(400, { error: 'question is required (maximum 32000 characters)' }) /* 判断条件并选择处理分支。 */
      if (route[1] === 'validate') { /* 判断条件并选择处理分支。 */
        try { return send(200, validateAnswer(profile.id, input.answer)) } /* 执行当前语句并推进处理流程。 */
        catch { return send(422, { error: '模型结果未通过结构校验，请重新运行或补充资料' }) } /* 执行当前语句并推进处理流程。 */
      } /* 结束当前表达式或代码块。 */
      const binding = await bindingFor(profile.id) /* 声明 binding。 */
      if (route[1] === 'context') { /* 判断条件并选择处理分支。 */
        const evidence = { observedAt: new Date().toISOString(), workflowId: profile.id, knowledgePolicy: binding, tools: profile.tools, data: {} } /* 声明 evidence。 */
        if (profile.id !== 'protocol-assistant') { /* 判断条件并选择处理分支。 */
          const devices = await api('/api/v1/device-registry?limit=100') /* 声明 devices。 */
          const list = Array.isArray(devices) ? devices : devices.items ?? [] /* 声明 list。 */
          evidence.data.devices = { items: list.slice(0, 100).map(row => { /* 更新 evidence.data.devices 的值。 */
            // The registry API returns { device, runtimeState, ... }, not a flat device.
            // Keep an explicit allowlist: registration credentials must never reach a model.
            const d = row.device ?? row, state = row.runtimeState ?? {} /* 声明 d。 */
            return { id: d.id, name: d.name, productId: d.productId, status: d.status, /* 返回当前处理结果。 */
              connectionStatus: state.connectionStatus, dataStatus: state.dataStatus, /* 执行当前语句并推进处理流程。 */
              businessStatus: state.businessStatus, lastSeenAt: state.lastSeenAt } /* 执行当前语句并推进处理流程。 */
          }), total: devices.total ?? list.length, loaded: Math.min(list.length, 100), truncated: list.length > 100 || (devices.total ?? 0) > 100 } /* 结束当前表达式或代码块。 */
          evidence.data.alarms = await tool(profile, { name: 'query_alarm_list', arguments: { limit: 20 } }, binding) /* 更新 evidence.data.alarms 的值。 */
        } /* 结束当前表达式或代码块。 */
        if (profile.tools.includes('query_system_overview')) evidence.data.overview = await tool(profile, { name: 'query_system_overview', arguments: {} }, binding) /* 判断条件并选择处理分支。 */
        if (profile.tools.includes('query_knowledge_base') && binding.retrievalMode !== 'disabled') { /* 判断条件并选择处理分支。 */
          evidence.data.knowledge = await tool(profile, { name: 'query_knowledge_base', arguments: { question: input.question, workflowId: profile.id } }, binding) /* 更新 evidence.data.knowledge 的值。 */
          if (binding.noMatchPolicy === 'require-evidence') { /* 判断条件并选择处理分支。 */
            const item = evidence.data.knowledge /* 声明 item。 */
            const hits = item.success ? JSON.parse(item.content?.find(c => c.type === 'text')?.text || '[]') : [] /* 声明 hits。 */
            if (!Array.isArray(hits) || !hits.length) return send(422, { error: '当前 Agent 要求知识证据，但没有召回匹配内容' }) /* 判断条件并选择处理分支。 */
          } /* 结束当前表达式或代码块。 */
        } /* 结束当前表达式或代码块。 */
        return send(200, evidence) /* 返回当前处理结果。 */
      } /* 结束当前表达式或代码块。 */
      let calls /* 声明 calls。 */
      try { calls = decodePlan(input.plan, profile.tools, input.question) } /* 执行当前语句并推进处理流程。 */
      catch { /* 执行当前语句并推进处理流程。 */
        return send(400, { error: '工具计划无效：只可使用当前业务的允许工具，每次最多六项，不能传入租户、用户或 URL。请按 allowedTools 修正计划；告警研判使用 iot_alarm，设备详情使用 iot_ops 或 iot_health。', allowedTools: profile.tools, executed: false }) /* 返回当前处理结果。 */
      } /* 结束当前表达式或代码块。 */
      const results = [] /* 声明 results。 */
      for (const call of calls) results.push(await tool(profile, call, binding)) /* 循环处理当前数据。 */
      return send(200, { observedAt: new Date().toISOString(), results }) /* 返回当前处理结果。 */
    } catch (error) { /* 结束当前表达式或代码块。 */
      // Never include credentials, upstream response bodies or model input in errors/logs.
      return send(502, { error: error instanceof SyntaxError ? 'invalid JSON input or tool plan' : 'IoT 数据查询失败，请检查平台及该 Agent 的知识配置' }) /* 返回当前处理结果。 */
    } /* 结束当前表达式或代码块。 */
  }) /* 结束当前表达式或代码块。 */
  server.requestTimeout = 180000 /* 更新 server.requestTimeout 的值。 */
  server.headersTimeout = 10000 /* 更新 server.headersTimeout 的值。 */
  return server /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) { /* 判断条件并选择处理分支。 */
  const config = JSON.parse(await readFile(resolve(process.argv[2] || 'data/dify/config.json'), 'utf8')) /* 声明 config。 */
  const env = { ...readEnv(await readFile(config.envFile || '.env.local', 'utf8')), ...process.env } /* 声明 env。 */
  const server = await createGateway(config, env) /* 声明 server。 */
  server.listen(config.port || 8092, config.host || '127.0.0.1', () => console.log(`Dify IoT gateway listening on ${config.host || '127.0.0.1'}:${config.port || 8092}`)) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
