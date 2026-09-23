// 从仓库根目录执行：node scripts/tests/local-business-smoke.mjs
// 使用现有本地环境，通过 Vite 代理联调。只新增带唯一标识的联调数据；不删除历史数据。
import { readFile } from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import { parseEnv } from 'node:util' /* 引入当前代码需要的依赖。 */
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */

const env = { ...parseEnv(await readFile(process.env.IOT_TEST_ENV_FILE || '.env.local', 'utf8')), ...process.env } /* 声明 env。 */
const origin = process.env.IOT_TEST_ORIGIN || 'http://127.0.0.1:5173' /* 声明 origin。 */
if (!['127.0.0.1', 'localhost'].includes(new URL(origin).hostname)) throw new Error('联调入口必须是本机地址') /* 判断条件并选择处理分支。 */
const tenantId = (env.IOT_ADMIN_TENANTS || 'tenant_001').split(',')[0].trim() /* 声明 tenantId。 */
const stamp = Date.now() /* 声明 stamp。 */
const deviceId = `local-check-${stamp}` /* 声明 deviceId。 */
let token, credential, created, testRule /* 声明 token。 */
const pass = name => console.log(`通过：${name}`) /* 声明 pass。 */
async function request(path, { method = 'GET', body, expected = 200, device = false } = {}) { /* 定义 request 函数。 */
  const headers = { 'Content-Type': 'application/json' } /* 声明 headers。 */
  if (device && credential) { /* 判断条件并选择处理分支。 */
    headers['X-Device-Key'] = credential.accessKey /* 执行当前语句并推进处理流程。 */
    headers['X-Device-Secret'] = credential.secret /* 执行当前语句并推进处理流程。 */
  } else if (token) headers.Authorization = `Bearer ${token}` /* 结束当前表达式或代码块。 */
  const response = await fetch(origin + path, { method, headers, body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(120000) }) /* 声明 response。 */
  // 服务端错误可能含配置，不输出响应正文或请求凭据。
  assert.equal(response.status, expected, `${method} ${path} 状态异常`) /* 验证实际结果符合预期。 */
  return response.json() /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
async function until(path, predicate) { /* 定义 until 函数。 */
  for (let attempt = 0; attempt < 40; attempt++) { /* 循环处理当前数据。 */
    const value = await request(path) /* 声明 value。 */
    if (predicate(value)) return value /* 判断条件并选择处理分支。 */
    await new Promise(resolve => setTimeout(resolve, 500)) /* 等待异步操作完成。 */
  } /* 结束当前表达式或代码块。 */
  throw new Error(`等待业务状态超时：${path}`) /* 抛出当前错误。 */
} /* 结束当前表达式或代码块。 */

try { /* 执行当前语句并推进处理流程。 */
  await request('/api/v1/products', { expected: 401 }) /* 等待异步操作完成。 */
  const login = await request('/api/v1/auth/login', { method: 'POST', body: { username: env.IOT_ADMIN_USER || 'admin', password: env.IOT_ADMIN_PASSWORD, tenantId } }) /* 声明 login。 */
  token = login.accessToken /* 更新 token 的值。 */
  assert.ok(token) /* 验证实际结果符合预期。 */
  pass('前端代理、登录与未登录访问隔离') /* 执行当前语句并推进处理流程。 */

  for (const path of ['products', 'device-registry', 'devices', 'protocol-packages', 'rules', 'alarms', 'raw-messages', 'integrations/video/cameras', 'knowledge/documents', 'ai/providers', 'ai/workflows', 'backups']) { /* 循环处理当前数据。 */
    await request(`/api/v1/${path}?page=1&pageSize=5`) /* 等待异步操作完成。 */
    pass(`页面数据接口 ${path}`) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */

  const payload = { id: `property-${stamp}`, timestamp: stamp, data: { temperature: 26.5 } } /* 声明 payload。 */
  const draft = { productId: `local-product-${stamp}`, productName: `本地联调产品 ${stamp}`, deviceId, name: '本地联调传感器', type: 'HTTP', messageKind: 'property', payload } /* 声明 draft。 */
  const preview = await request('/api/v1/onboarding/test', { method: 'POST', body: draft }) /* 声明 preview。 */
  assert.equal(preview.success, true) /* 验证实际结果符合预期。 */
  created = await request('/api/v1/onboarding', { method: 'POST', body: { ...draft, testToken: preview.testToken }, expected: 201 }) /* 更新 created 的值。 */
  credential = created.credential /* 更新 credential 的值。 */
  assert.ok(credential?.secret) /* 验证实际结果符合预期。 */
  pass('接入样例测试、产品与设备持久化') /* 执行当前语句并推进处理流程。 */

  const base = `/api/v1/device-ingest/standard/${tenantId}/${created.device.productId}/${deviceId}/` /* 声明 base。 */
  const received = await request(base + 'property', { method: 'POST', body: payload, device: true, expected: 202 }) /* 声明 received。 */
  const rawPath = `/api/v1/raw-messages/${received.messageId}` /* 声明 rawPath。 */
  const raw = await until(rawPath, value => value.parseStatus === 'PARSED') /* 声明 raw。 */
  assert.equal(raw.standardMessage.properties.temperature, 26.5) /* 验证实际结果符合预期。 */
  const downloaded = await request(rawPath + '/download') /* 声明 downloaded。 */
  assert.equal(downloaded.messageId, received.messageId) /* 验证实际结果符合预期。 */
  assert.deepEqual(downloaded.payload, payload) /* 验证实际结果符合预期。 */
  pass('设备上报、消息队列消费、原文归档、解析与下载') /* 执行当前语句并推进处理流程。 */
  const duplicate = await request(base + 'property', { method: 'POST', body: payload, device: true, expected: 202 }) /* 声明 duplicate。 */
  assert.equal(duplicate.created, false) /* 验证实际结果符合预期。 */
  await request(base + 'property', { method: 'POST', body: { ...payload, data: { temperature: 99 } }, device: true, expected: 409 }) /* 等待异步操作完成。 */
  assert.equal((await request(rawPath)).standardMessage.properties.temperature, 26.5) /* 验证实际结果符合预期。 */
  pass('重复上报幂等、冲突拒绝及原数据保护') /* 执行当前语句并推进处理流程。 */

  await request(base + 'state', { method: 'POST', body: { id: `state-${stamp}`, timestamp: Date.now(), data: { connectionStatus: 'CONNECTED' } }, device: true, expected: 202 }) /* 等待异步操作完成。 */
  await until(`/api/v1/devices/${deviceId}/latest`, value => value.state.connectionStatus === 'CONNECTED') /* 等待异步操作完成。 */
  const history = await request(`/api/v1/devices/${deviceId}/properties/history?property=temperature&start=${stamp - 1000}&end=${Date.now() + 1000}`) /* 声明 history。 */
  assert.ok(history.items.length > 0) /* 验证实际结果符合预期。 */
  pass('设备在线状态与时序属性查询') /* 执行当前语句并推进处理流程。 */

  testRule = await request('/api/v1/rules', { method: 'POST', expected: 201, body: { id: `local-rule-${stamp}`, name: '本地联调专用规则', alarmType: 'DEVICE_FAULT', level: 'LOW', match: 'all', enabled: true, conditions: [{ field: 'localCheck', operator: 'eq', value: deviceId }] } }) /* 更新 testRule 的值。 */
  const alarmMessage = await request(base + 'property', { method: 'POST', body: { id: `alarm-${stamp}`, timestamp: Date.now(), data: { localCheck: deviceId, temperature: 26.5 } }, device: true, expected: 202 }) /* 声明 alarmMessage。 */
  await until(`/api/v1/raw-messages/${alarmMessage.messageId}`, value => value.parseStatus === 'PARSED') /* 等待异步操作完成。 */
  const alarms = await until(`/api/v1/alarms?deviceId=${deviceId}&pageSize=100`, value => value.items.some(item => item.deviceId === deviceId)) /* 声明 alarms。 */
  const alarm = alarms.items.find(item => item.deviceId === deviceId) /* 声明 alarm。 */
  assert.ok(alarm.alarmId) /* 验证实际结果符合预期。 */
  assert.equal((await request(`/api/v1/alarms/${alarm.alarmId}/actions`, { method: 'POST', body: { action: 'ACKED' } })).status, 'ACKED') /* 验证实际结果符合预期。 */
  assert.equal((await request(`/api/v1/alarms/${alarm.alarmId}/actions`, { method: 'POST', body: { action: 'CLOSED' } })).status, 'CLOSED') /* 验证实际结果符合预期。 */
  pass('专用规则匹配、告警生成、确认与关闭') /* 执行当前语句并推进处理流程。 */

  const replay = await request('/api/v1/raw-messages/replay', { method: 'POST', expected: 202, body: { deviceId, productId: created.device.productId, start: stamp - 1000, end: Date.now() + 1000, mode: 'DRY_RUN', ratePerSecond: 20 } }) /* 声明 replay。 */
  const completed = await until(`/api/v1/replays/${replay.id}`, value => ['COMPLETED', 'FAILED'].includes(value.status)) /* 声明 completed。 */
  assert.equal(completed.status, 'COMPLETED') /* 验证实际结果符合预期。 */
  assert.equal(completed.failed, 0) /* 验证实际结果符合预期。 */
  assert.ok(completed.processed >= 3) /* 验证实际结果符合预期。 */
  pass('原始报文试运行回放') /* 执行当前语句并推进处理流程。 */
  console.log(`联调记录：设备 ${deviceId}；产品 ${created.device.productId}`) /* 执行当前语句并推进处理流程。 */
} catch (error) { /* 结束当前表达式或代码块。 */
  console.error(error instanceof assert.AssertionError ? error.message : '联调中断，请按最后通过步骤检查服务状态') /* 验证实际结果符合预期。 */
  process.exitCode = 1 /* 更新 process.exitCode 的值。 */
} finally { /* 结束当前表达式或代码块。 */
  if (testRule && token) { /* 判断条件并选择处理分支。 */
    await request(`/api/v1/rules/${testRule.id}`, { method: 'PUT', body: { ...testRule, enabled: false } }) /* 等待异步操作完成。 */
    pass('停用本次联调专用规则') /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  if (created && token) { /* 判断条件并选择处理分支。 */
    await request(`/api/v1/device-registry/${deviceId}/credentials`, { method: 'DELETE' }) /* 等待异步操作完成。 */
    pass('停用本次联调设备凭据，保留业务记录供复查') /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
