// Run from the repository root against the existing local API/Vite and OrbStack dependencies.
// No configuration writes or model calls: the current Embedding key must be empty.
import assert from 'node:assert/strict'
import { createHash, createHmac, randomBytes } from 'node:crypto'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { parseEnv } from 'node:util'
import { fileURLToPath } from 'node:url'
import { startBrowser, delay } from '../helpers/browser.mjs'

const repository = fileURLToPath(new URL('../../../', import.meta.url))
const env = { ...parseEnv(await readFile(process.env.IOT_TEST_ENV_FILE || join(repository, '.env.local'), 'utf8')), ...process.env }
const origin = process.env.IOT_TEST_ORIGIN || 'http://127.0.0.1:5173'
assert.ok(['localhost', '127.0.0.1'].includes(new URL(origin).hostname), 'Live smoke requires a local platform URL')
const apiOrigin = process.env.IOT_TEST_API_ORIGIN || 'http://127.0.0.1:8081'
assert.ok(['localhost', '127.0.0.1'].includes(new URL(apiOrigin).hostname), 'Live smoke requires a local API URL')
const tenantId = (env.IOT_ADMIN_TENANTS || 'tenant_001').split(',')[0].trim()
const marker = `ai-cloud-live-smoke-${Date.now()}-${randomBytes(4).toString('hex')}`
const filename = `${marker}.txt`
const outputDir = join(tmpdir(), marker)
await mkdir(outputDir)
let token, document, browser, initialConfig, createdAgentId, phase = 'login', deleted = false
const agentsOnly = process.argv.includes('--agents-only')
const checked = [], requests = [], screenshots = []
const pass = name => { checked.push(name); console.log(`PASS: ${name}`) }

async function api(path, { method = 'GET', body, expected = 200 } = {}) {
  const headers = token ? { authorization:`Bearer ${token}` } : {}
  if (body !== undefined && !(body instanceof FormData)) headers['content-type'] = 'application/json'
  const response = await fetch(origin + path, { method, headers,
    body:body === undefined ? undefined : body instanceof FormData ? body : JSON.stringify(body), signal:AbortSignal.timeout(15000) })
  assert.equal(response.status, expected, `${method} ${path}: unexpected status`)
  if (response.status === 404) return null
  return response.json()
}

async function until(check, note, timeout = 30000) {
  const deadline = Date.now() + timeout
  do {
    const result = await check()
    if (result) return result
    await delay(200)
  } while (Date.now() < deadline)
  throw new Error(note)
}

function assertFixture() {
  assert.ok(document?.id && document.filename === filename && document.workflowId === 'ops-assistant', 'Cleanup is restricted to the exact smoke fixture')
  assert.equal(document.objectBucket, 'iot-knowledge-docs')
  assert.equal(document.objectKey, `${tenantId}/agents/ops-assistant/${document.id}/${filename}`)
}

// AWS v4 HEAD signs only the exact fixture object, using credentials in memory.
// Request headers, endpoint, credentials and response bodies are never printed.
async function originalStatus() {
  assertFixture()
  assert.ok(env.IOT_MINIO_ENDPOINT && env.IOT_MINIO_ACCESS_KEY && env.IOT_MINIO_SECRET_KEY, 'Existing MinIO configuration is required to verify original cleanup')
  const endpoint = new URL(`${env.IOT_MINIO_USE_TLS === 'true' ? 'https' : 'http'}://${env.IOT_MINIO_ENDPOINT}`)
  assert.ok(!endpoint.username && !endpoint.password && endpoint.pathname === '/', 'MinIO endpoint must not contain credentials or a path')
  const uri = '/' + [document.objectBucket, ...document.objectKey.split('/')].map(encodeURIComponent).join('/')
  const timestamp = new Date().toISOString().replace(/[-:]|\.\d{3}/g, '')
  const day = timestamp.slice(0, 8), scope = `${day}/us-east-1/s3/aws4_request`
  const digest = value => createHash('sha256').update(value).digest('hex')
  const hmac = (key, value) => createHmac('sha256', key).update(value).digest()
  const empty = digest('')
  const signedHeaders = 'host;x-amz-content-sha256;x-amz-date'
  const canonicalHeaders = `host:${endpoint.host}\nx-amz-content-sha256:${empty}\nx-amz-date:${timestamp}\n`
  const canonicalRequest = `HEAD\n${uri}\n\n${canonicalHeaders}\n${signedHeaders}\n${empty}`
  const signingKey = hmac(hmac(hmac(hmac(`AWS4${env.IOT_MINIO_SECRET_KEY}`, day), 'us-east-1'), 's3'), 'aws4_request')
  const signature = createHmac('sha256', signingKey).update(`AWS4-HMAC-SHA256\n${timestamp}\n${scope}\n${digest(canonicalRequest)}`).digest('hex')
  const response = await fetch(endpoint.origin + uri, { method:'HEAD', signal:AbortSignal.timeout(10000), headers:{
    'x-amz-content-sha256':empty, 'x-amz-date':timestamp,
    authorization:`AWS4-HMAC-SHA256 Credential=${env.IOT_MINIO_ACCESS_KEY}/${scope}, SignedHeaders=${signedHeaders}, Signature=${signature}`,
  } })
  return response.status
}

async function cleanup() {
  if (!document || deleted) return
  assertFixture()
  const detailPath = `/api/v1/knowledge/documents/${encodeURIComponent(document.id)}`
  const existing = await fetch(origin + detailPath, { headers:{ authorization:`Bearer ${token}` }, signal:AbortSignal.timeout(15000) })
  if (existing.status !== 404) {
    assert.equal(existing.status, 200, 'Fixture cleanup preflight failed')
    await api(detailPath, { method:'DELETE', expected:202 })
  }
  await until(async () => (await fetch(origin + detailPath, { headers:{ authorization:`Bearer ${token}` }, signal:AbortSignal.timeout(15000) })).status === 404,
    'Fixture record cleanup timed out')
  await until(async () => (await originalStatus()) === 404, 'Fixture original cleanup timed out')
  deleted = true
}

async function cleanupAgent() {
  if (!createdAgentId) return
  assert.equal(createdAgentId, `${marker}-agent`, 'Agent cleanup is restricted to the exact smoke fixture')
  const catalog = await api('/api/v1/ai/workflows/admin?page=1&pageSize=100')
  if (catalog.items.some(item => item.id === createdAgentId)) {
    await api(`/api/v1/ai/workflows/${encodeURIComponent(createdAgentId)}`, { method:'DELETE' })
  }
  const after = await api('/api/v1/ai/workflows/admin?page=1&pageSize=100')
  assert.equal(after.items.some(item => item.id === createdAgentId), false, 'Smoke Agent must be removed from Harness')
  createdAgentId = undefined
}

async function checkAgents(model) {
  phase = 'real Harness Agent CRUD'
  const id = `${marker}-agent`
  const manifest = { schemaVersion:1, id, name:'云 API 真实联调 Agent', description:`临时只读验收 ${marker}`,
    version:'1.0.0', enabled:true, persona:`只查询平台只读概览。测试标识 ${marker}`, defaultModel:model || 'deepseek-flash',
    maxTokens:512, capabilities:['运行状态查询'], allowedTools:['mcp__iot__query_system_overview'] }
  const before = await api('/api/v1/ai/workflows/admin?page=1&pageSize=100')
  assert.equal(before.items.some(item => item.id === id), false, 'Smoke Agent ID must be new')
  const created = await api('/api/v1/ai/workflows', { method:'POST', body:manifest, expected:201 })
  createdAgentId = id
  assert.equal(created.id, id)
  let catalog = await api('/api/v1/ai/workflows/admin?page=1&pageSize=100')
  const saved = catalog.items.find(item => item.id === id)
  assert.equal(saved.persona, manifest.persona)
  assert.deepEqual(saved.allowedTools, manifest.allowedTools)
  const binding = await api(`/api/v1/ai/workflows/${encodeURIComponent(id)}/knowledge-binding`)
  assert.equal(binding.retrievalMode, 'always', 'New Agent must default to knowledge prefetch')
  const updated = { ...manifest, name:'云 API 真实联调 Agent（已更新）', maxTokens:256, enabled:false }
  await api(`/api/v1/ai/workflows/${encodeURIComponent(id)}`, { method:'PUT', body:updated })
  catalog = await api('/api/v1/ai/workflows/admin?page=1&pageSize=100')
  assert.equal(catalog.items.find(item => item.id === id).enabled, false)
  assert.equal(catalog.items.find(item => item.id === id).maxTokens, 256)
  const publicCatalog = await api('/api/v1/ai/workflows?page=1&pageSize=100')
  assert.equal(publicCatalog.items.some(item => item.id === id), false, 'Disabled Agent must not remain callable')
  await cleanupAgent()
  pass('real Harness dynamic Agent create/read/update/delete, manifest persistence and default knowledge binding; no model run')
}

async function capture(name, full = false) {
  const screenshot = await browser.call('Page.captureScreenshot', { format:'png', captureBeyondViewport:full })
  const path = join(outputDir, `${name}.png`)
  await writeFile(path, Buffer.from(screenshot.data, 'base64'))
  screenshots.push(path)
}

try {
  await until(async () => {
    try { return (await fetch(apiOrigin + '/health/ready', { signal:AbortSignal.timeout(3000) })).status === 200 } catch { return false }
  }, 'Source API readiness did not become healthy')
  pass('source API readiness')
  const login = await api('/api/v1/auth/login', { method:'POST', body:{ username:env.IOT_ADMIN_USER || 'admin', password:env.IOT_ADMIN_PASSWORD, tenantId } })
  token = login.accessToken
  assert.ok(token, 'Login did not return a session')
  initialConfig = await api('/api/v1/ai/embedding-config')
  assert.equal(Object.hasOwn(initialConfig, 'apiKey'), false, 'Embedding key must be redacted')
  assert.equal(initialConfig.apiKeyConfigured, false, 'Smoke requires the existing Embedding key to be empty; configuration is never changed')
  const providers = await api('/api/v1/ai/providers')
  assert.equal(Object.hasOwn(providers.config || {}, 'apiKey'), false, 'Conversation model key must be redacted')
  const { apiKeyConfigured, ...candidate } = initialConfig
  const testResult = await api('/api/v1/ai/embedding-test', { method:'POST', body:{ ...candidate, apiKey:'' } })
  assert.equal(testResult.success, false)
  assert.equal(testResult.latencyMs, 0, 'Missing-key test must exit before an external Embedding request')
  assert.match(testResult.error, /API Key/)
  pass('real login, model/Embedding key redaction, missing-key test exits before network request')

  if (agentsOnly || process.env.IOT_TEST_HARNESS_READY === '1') await checkAgents(providers.config?.model)
  if (!agentsOnly) {

    phase = 'upload and index failure'
    const form = new FormData()
    form.append('file', new Blob([`消防设备告警处置测试文档。\n标识：${marker}\n现场复核设备状态后记录处置过程。\n`], { type:'text/plain' }), filename)
    form.append('workflowId', 'ops-assistant')
    form.append('category', 'manual')
    form.append('tags', marker)
    document = await api('/api/v1/knowledge/documents', { method:'POST', body:form, expected:202 })
    assertFixture()
    assert.equal(document.status, 'UPLOADED')
    assert.equal(await originalStatus(), 200, 'Uploaded original must exist in MinIO')
    const detailPath = `/api/v1/knowledge/documents/${encodeURIComponent(document.id)}`
    const failed = await until(async () => {
      const value = await api(detailPath)
      return value.document.status === 'INDEX_FAILED' && value
    }, 'Async index failure was not persisted')
    assert.match(failed.document.metadata.indexError, /API Key/)
    assert.equal(failed.document.metadata.indexProgress.done, 0)
    assert.ok(Number.isInteger(failed.document.metadata.indexProgress.total) && failed.document.metadata.indexProgress.total >= 0,
      'Progress must retain actual server batch counts; missing key may fail before any batch starts')
    assert.equal(failed.index.mode, 'postgres-pgvector')
    pass('upload 202, original persisted, actual async INDEX_FAILED/progress/source metadata')

    phase = 'desktop model UI'
    browser = await startBrowser({ timeout:20000, args:['--disable-background-timer-throttling'], onEvent:event => {
      if (event.method !== 'Network.responseReceived') return
      const response = event.params.response
      const url = new URL(response.url)
      if (url.origin !== origin || !url.pathname.startsWith('/api/')) return
      requests.push({ id:event.params.requestId, path:url.pathname, status:response.status })
    } })
    const { call, evaluate } = browser
    await call('Runtime.enable')
    await call('Page.enable')
    await call('Network.enable')
    await call('Emulation.setDeviceMetricsOverride', { width:1440, height:1100, deviceScaleFactor:1, mobile:false })
    // The session lives only in the throwaway browser profile, which close() removes.
    await call('Page.addScriptToEvaluateOnNewDocument', { source:`localStorage.clear();for(const [key,value] of Object.entries(${JSON.stringify({ iot_token:token, iot_tenant:tenantId,
      iot_role:login.role || 'admin', iot_user:login.username || env.IOT_ADMIN_USER || 'admin', iot_permissions:JSON.stringify(login.permissions || ['*']) })}))localStorage.setItem(key,value)` })
    await call('Page.navigate', { url:origin })
    await browser.until(() => evaluate(`Boolean(document.querySelector('.nav-item[aria-label="模型管理"]'))`), 'signed-in navigation')
    await evaluate(`document.querySelector('.nav-item[aria-label="模型管理"]').click()`)
    await browser.until(() => evaluate(`document.querySelector('.embedding-config')?.innerText.includes('尚未保存接口密钥')`), 'real Embedding configuration')
    assert.equal(await evaluate(`document.querySelector('.embedding-config input[type=password]').value`), '')
    await evaluate(`[...document.querySelectorAll('.embedding-config button')].find(button=>button.innerText.trim()==='测试 Embedding').click()`)
    await browser.until(() => evaluate(`document.querySelector('.embedding-config')?.innerText.includes('请填写 Embedding API Key')`), 'missing key error in UI')
    await evaluate(`document.querySelector('.embedding-config').scrollIntoView({block:'center'})`)
    await capture('model-desktop', true)
    pass('desktop real model management displays redacted key and missing-key diagnostic')

    phase = 'desktop knowledge and retry polling'
    await evaluate(`document.querySelector('.nav-item[aria-label="知识库"]').click()`)
    const rowExpression = `([...document.querySelectorAll('.knowledge-table tr')].find(row=>row.innerText.includes(${JSON.stringify(filename)})))`
    await browser.until(() => evaluate(`${rowExpression}?.innerText.includes('索引失败')`), 'fixture failure row')
    assert.equal(await evaluate(`document.querySelector('.knowledge-page').innerText.includes('PostgreSQL / pgvector')`), true)
    assert.equal(await evaluate(`${rowExpression}.innerText.includes('Embedding API Key 尚未配置')`), true)
    const previousReads = requests.filter(item => item.path === '/api/v1/knowledge/documents').length
    await evaluate(`[...${rowExpression}.querySelectorAll('button')].find(button=>button.innerText.trim()==='重试索引').click()`)
    await browser.until(() => Promise.resolve(requests.some(item => item.path === detailPath + '/retry' && item.status === 202)), 'real retry 202')
    await browser.until(() => Promise.resolve(requests.filter(item => item.path === '/api/v1/knowledge/documents').length >= previousReads + 2), 'automatic refresh after retry')
    await browser.until(() => evaluate(`${rowExpression}?.innerText.includes('索引失败')`), 'refreshed retry failure')
    await capture('knowledge-desktop', true)
    pass('desktop real knowledge failure, retry 202 and automatic async status refresh')

    phase = '390px knowledge UI'
    await call('Emulation.setDeviceMetricsOverride', { width:390, height:844, deviceScaleFactor:1, mobile:true })
    await browser.until(() => evaluate(`getComputedStyle(document.querySelector('.knowledge-mobile-list')).display !== 'none'`), 'mobile knowledge list')
    await browser.until(() => evaluate(`document.querySelector('.app-sidebar').getBoundingClientRect().right <= 1`), 'collapsed mobile navigation')
    assert.equal(await evaluate(`document.documentElement.scrollWidth <= innerWidth`), true, 'Knowledge page must fit 390px')
    await capture('knowledge-mobile')
    // Navigate through the application's own visible mobile menu.
    await evaluate(`document.querySelector('.mobile-menu-button, .mobile-menu-toggle, .app-menu-toggle, button[aria-label="打开菜单"]')?.click()`)
    await evaluate(`document.querySelector('.nav-item[aria-label="模型管理"]').click()`)
    await browser.until(() => evaluate(`document.querySelector('.embedding-config')?.innerText.includes('尚未保存接口密钥')`), 'mobile model configuration')
    await browser.until(() => evaluate(`document.querySelector('.app-sidebar').getBoundingClientRect().right <= 1`), 'collapsed model navigation')
    assert.equal(await evaluate(`document.documentElement.scrollWidth <= innerWidth`), true, 'Model management must fit 390px')
    await evaluate(`document.querySelector('.embedding-config').scrollIntoView({block:'start'})`)
    await capture('model-mobile')
    pass('390px knowledge/model views fit viewport and show real persisted state')

    phase = 'UI accepted deletion and cleanup'
    await call('Emulation.setDeviceMetricsOverride', { width:1440, height:1100, deviceScaleFactor:1, mobile:false })
    await evaluate(`document.querySelector('.nav-item[aria-label="知识库"]').click()`)
    await browser.until(() => evaluate(`Boolean(${rowExpression})`), 'fixture row before cleanup')
    await evaluate(`[...${rowExpression}.querySelectorAll('button')].find(button=>button.innerText.trim()==='删除').click()`)
    await browser.until(() => evaluate(`Boolean([...document.querySelectorAll('.n-dialog button')].find(button=>button.innerText.trim()==='确定删除'))`), 'delete confirmation')
    await evaluate(`[...document.querySelectorAll('.n-dialog button')].find(button=>button.innerText.trim()==='确定删除').click()`)
    await browser.until(() => Promise.resolve(requests.some(item => item.path === detailPath && item.status === 202)), 'real delete 202')
    await browser.until(() => evaluate(`!${rowExpression}`), 'document disappears after async deletion')
    await cleanup()
    assert.equal(await originalStatus(), 404)
    const afterConfig = await api('/api/v1/ai/embedding-config')
    assert.ok(JSON.stringify(afterConfig) === JSON.stringify(initialConfig), 'Smoke must preserve the existing Embedding configuration')
    assert.equal(browser.errors.length, 0, 'Browser encountered a JavaScript exception')
    pass('UI delete 202, original/record gone, no configuration mutation or browser exceptions')
    await writeFile(join(outputDir, 'result.json'), JSON.stringify({ marker, filename, documentId:document.id, deleted,
      checked, screenshots, javascriptErrors:browser.errors.length, fixtureRequestStatuses:requests.filter(item => item.path === detailPath || item.path === detailPath + '/retry').map(({ path,status }) => ({ path,status })) }, null, 2))
    console.log(`Live smoke evidence: ${outputDir}`)
  }
} catch (error) {
  const safeReason = error instanceof assert.AssertionError ? error.message.split('\n')[0]
    : /^(Browser condition timed out|Browser exited|CDP timeout)/.test(error.message || '') ? error.message : 'check interrupted (no credentials or response bodies printed)'
  console.error(`FAIL: ${phase}; ${safeReason}`)
  if (browser) {
    try { await capture('failure'); console.error(`UI evidence: ${screenshots.at(-1)}`) } catch {}
    console.error(`UI exceptions: ${browser.errors.length}; API responses observed: ${requests.length}`)
  }
  process.exitCode = 1
} finally {
  try { await cleanup() } catch { console.error(`Fixture cleanup requires retry: ${document?.id || marker}`); process.exitCode = 1 }
  try { await cleanupAgent() } catch { console.error(`Agent fixture cleanup requires retry: ${createdAgentId || marker}`); process.exitCode = 1 }
  await browser?.close()
}
