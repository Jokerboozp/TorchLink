// 从仓库根目录执行：node scripts/tests/local-business-smoke.mjs
// 使用现有本地环境，通过 Vite 代理联调。只新增带唯一标识的联调数据；不删除历史数据。
import { readFile } from 'node:fs/promises'
import { parseEnv } from 'node:util'
import assert from 'node:assert/strict'

const env = { ...parseEnv(await readFile(process.env.IOT_TEST_ENV_FILE || '.env.local', 'utf8')), ...process.env }
const origin = process.env.IOT_TEST_ORIGIN || 'http://127.0.0.1:5173'
if (!['127.0.0.1', 'localhost'].includes(new URL(origin).hostname)) throw new Error('联调入口必须是本机地址')
const tenantId = (env.IOT_ADMIN_TENANTS || 'tenant_001').split(',')[0].trim()
const stamp = Date.now()
const deviceId = `local-check-${stamp}`
let token, credential, created, testRule
const pass = name => console.log(`通过：${name}`)
async function request(path, { method = 'GET', body, expected = 200, device = false } = {}) {
  const headers = { 'Content-Type': 'application/json' }
  if (device && credential) {
    headers['X-Device-Key'] = credential.accessKey
    headers['X-Device-Secret'] = credential.secret
  } else if (token) headers.Authorization = `Bearer ${token}`
  const response = await fetch(origin + path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(120000) })
  // 服务端错误可能含配置，不输出响应正文或请求凭据。
  assert.equal(response.status, expected, `${method} ${path} 状态异常`)
  return response.json()
}
async function until(path, predicate) {
  for (let attempt = 0; attempt < 40; attempt++) {
    const value = await request(path)
    if (predicate(value)) return value
    await new Promise(resolve => setTimeout(resolve, 500))
  }
  throw new Error(`等待业务状态超时：${path}`)
}

try {
  await request('/api/v1/products', { expected: 401 })
  const login = await request('/api/v1/auth/login', { method: 'POST', body: { username: env.IOT_ADMIN_USER || 'admin', password: env.IOT_ADMIN_PASSWORD, tenantId } })
  token = login.accessToken
  assert.ok(token)
  pass('前端代理、登录与未登录访问隔离')

  for (const path of ['products', 'device-registry', 'devices', 'protocol-packages', 'rules', 'alarms', 'raw-messages', 'integrations/video/cameras', 'knowledge/documents', 'ai/providers', 'ai/workflows', 'backups']) {
    await request(`/api/v1/${path}?page=1&pageSize=5`)
    pass(`页面数据接口 ${path}`)
  }

  const payload = { id: `property-${stamp}`, timestamp: stamp, data: { temperature: 26.5 } }
  const draft = { productId: `local-product-${stamp}`, productName: `本地联调产品 ${stamp}`, deviceId, name: '本地联调传感器', type: 'HTTP', messageKind: 'property', payload }
  const preview = await request('/api/v1/onboarding/test', { method: 'POST', body: draft })
  assert.equal(preview.success, true)
  created = await request('/api/v1/onboarding', { method: 'POST', body: { ...draft, testToken: preview.testToken }, expected: 201 })
  credential = created.credential
  assert.ok(credential?.secret)
  pass('接入样例测试、产品与设备持久化')

  const base = `/api/v1/device-ingest/standard/${tenantId}/${created.device.productId}/${deviceId}/`
  const received = await request(base + 'property', { method: 'POST', body: payload, device: true, expected: 202 })
  const rawPath = `/api/v1/raw-messages/${received.messageId}`
  const raw = await until(rawPath, value => value.parseStatus === 'PARSED')
  assert.equal(raw.standardMessage.properties.temperature, 26.5)
  const downloaded = await request(rawPath + '/download')
  assert.equal(downloaded.messageId, received.messageId)
  assert.deepEqual(downloaded.payload, payload)
  pass('设备上报、消息队列消费、原文归档、解析与下载')
  const duplicate = await request(base + 'property', { method: 'POST', body: payload, device: true, expected: 202 })
  assert.equal(duplicate.created, false)
  await request(base + 'property', { method: 'POST', body: { ...payload, data: { temperature: 99 } }, device: true, expected: 409 })
  assert.equal((await request(rawPath)).standardMessage.properties.temperature, 26.5)
  pass('重复上报幂等、冲突拒绝及原数据保护')

  await request(base + 'state', { method: 'POST', body: { id: `state-${stamp}`, timestamp: Date.now(), data: { connectionStatus: 'CONNECTED' } }, device: true, expected: 202 })
  await until(`/api/v1/devices/${deviceId}/latest`, value => value.state.connectionStatus === 'CONNECTED')
  const history = await request(`/api/v1/devices/${deviceId}/properties/history?property=temperature&start=${stamp - 1000}&end=${Date.now() + 1000}`)
  assert.ok(history.items.length > 0)
  pass('设备在线状态与时序属性查询')

  testRule = await request('/api/v1/rules', { method: 'POST', expected: 201, body: { id: `local-rule-${stamp}`, name: '本地联调专用规则', alarmType: 'DEVICE_FAULT', level: 'LOW', match: 'all', enabled: true, conditions: [{ field: 'localCheck', operator: 'eq', value: deviceId }] } })
  const alarmMessage = await request(base + 'property', { method: 'POST', body: { id: `alarm-${stamp}`, timestamp: Date.now(), data: { localCheck: deviceId, temperature: 26.5 } }, device: true, expected: 202 })
  await until(`/api/v1/raw-messages/${alarmMessage.messageId}`, value => value.parseStatus === 'PARSED')
  const alarms = await until(`/api/v1/alarms?deviceId=${deviceId}&pageSize=100`, value => value.items.some(item => item.deviceId === deviceId))
  const alarm = alarms.items.find(item => item.deviceId === deviceId)
  assert.ok(alarm.alarmId)
  assert.equal((await request(`/api/v1/alarms/${alarm.alarmId}/actions`, { method: 'POST', body: { action: 'ACKED' } })).status, 'ACKED')
  assert.equal((await request(`/api/v1/alarms/${alarm.alarmId}/actions`, { method: 'POST', body: { action: 'CLOSED' } })).status, 'CLOSED')
  pass('专用规则匹配、告警生成、确认与关闭')

  const replay = await request('/api/v1/raw-messages/replay', { method: 'POST', expected: 202, body: { deviceId, productId: created.device.productId, start: stamp - 1000, end: Date.now() + 1000, mode: 'DRY_RUN', ratePerSecond: 20 } })
  const completed = await until(`/api/v1/replays/${replay.id}`, value => ['COMPLETED', 'FAILED'].includes(value.status))
  assert.equal(completed.status, 'COMPLETED')
  assert.equal(completed.failed, 0)
  assert.ok(completed.processed >= 3)
  pass('原始报文试运行回放')
  console.log(`联调记录：设备 ${deviceId}；产品 ${created.device.productId}`)
} catch (error) {
  console.error(error instanceof assert.AssertionError ? error.message : '联调中断，请按最后通过步骤检查服务状态')
  process.exitCode = 1
} finally {
  if (testRule && token) {
    await request(`/api/v1/rules/${testRule.id}`, { method: 'PUT', body: { ...testRule, enabled: false } })
    pass('停用本次联调专用规则')
  }
  if (created && token) {
    await request(`/api/v1/device-registry/${deviceId}/credentials`, { method: 'DELETE' })
    pass('停用本次联调设备凭据，保留业务记录供复查')
  }
}
