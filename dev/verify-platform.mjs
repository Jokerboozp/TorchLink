// Local verification of the six protocol packages against the running platform.
// Reads credentials from .env.local; never writes them to the result file.
import { readFile, writeFile } from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import { parseEnv } from 'node:util' /* 引入当前代码需要的依赖。 */
import { join } from 'node:path' /* 引入当前代码需要的依赖。 */
import { createConnection, createServer } from 'node:net' /* 引入当前代码需要的依赖。 */
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */

const env = parseEnv(await readFile('.env.local', 'utf8')) /* 声明 env。 */
const base = process.env.IOT_VERIFY_ORIGIN || 'http://127.0.0.1:8081' /* 声明 base。 */
const tenant = (env.IOT_ADMIN_TENANTS || 'tenant_001').split(',')[0].trim() /* 声明 tenant。 */
const names = ['fb2018', 'fb2024', 'fb-hydraulic', 'fb-liquid-level', 'kuka-modbus', 'sp-cannon'] /* 声明 names。 */
const reportPath = 'dev/verification-20260923.json' /* 声明 reportPath。 */
let report /* 声明 report。 */
try { report = JSON.parse(await readFile(reportPath, 'utf8')) } catch { report = { origin: base, tenant, packages: [] } } /* 执行当前语句并推进处理流程。 */
if (report.origin !== base || report.tenant !== tenant) throw new Error('验证报告属于另一个平台或租户') /* 判断条件并选择处理分支。 */
report.at = new Date().toISOString() /* 更新 report.at 的值。 */

async function api(path, { method = 'GET', body, token, timeoutMs = 240000 } = {}) { /* 定义 api 函数。 */
  const headers = token ? { Authorization: `Bearer ${token}` } : {} /* 声明 headers。 */
  if (body && !(body instanceof FormData)) headers['Content-Type'] = 'application/json' /* 判断条件并选择处理分支。 */
  const response = await fetch(base + path, { /* 声明 response。 */
    method, headers, /* 执行当前语句并推进处理流程。 */
    body: body instanceof FormData ? body : body === undefined ? undefined : JSON.stringify(body), /* 执行当前语句并推进处理流程。 */
    signal: AbortSignal.timeout(timeoutMs), redirect: 'error', /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
  const result = await response.json().catch(() => ({})) /* 声明 result。 */
  if (!response.ok) { /* 判断条件并选择处理分支。 */
    const error = new Error(`${method} ${path}: HTTP ${response.status}: ${String(result.detail || result.error || '').slice(0, 800)}`) /* 声明 error。 */
    error.status = response.status /* 更新 error.status 的值。 */
    throw error /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  return result /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

const { accessToken } = await api('/api/v1/auth/login', { /* 执行当前语句并推进处理流程。 */
  method: 'POST', body: { username: env.IOT_ADMIN_USER || 'admin', password: env.IOT_ADMIN_PASSWORD, tenantId: tenant }, /* 执行当前语句并推进处理流程。 */
}) /* 结束当前表达式或代码块。 */
if (!accessToken) throw new Error('平台登录未返回 token') /* 判断条件并选择处理分支。 */

if ((process.argv[2] || 'upload') === 'upload') { /* 判断条件并选择处理分支。 */
  report.packages = [] /* 更新 report.packages 的值。 */
  const existing = await api('/api/v2/protocols?pageSize=100', { token: accessToken }) /* 声明 existing。 */
  for (const id of names) { /* 循环处理当前数据。 */
    const entry = { id, version: ['kuka-modbus', 'sp-cannon'].includes(id) ? '1.0.1' : '1.0.0', upload: 'pending' } /* 声明 entry。 */
    report.packages.push(entry) /* 执行当前语句并推进处理流程。 */
    try { /* 执行当前语句并推进处理流程。 */
      const prior = existing.items?.find(item => item.definition?.id === id)?.releases?.find(release => release.version === entry.version) /* 声明 prior。 */
      if (prior) { /* 判断条件并选择处理分支。 */
        entry.upload = prior.status === 'PUBLISHED' ? 'already-published' : 'existing-unpublished' /* 更新 entry.upload 的值。 */
        console.log(`已有版本 ${id}@${entry.version}: ${prior.status}`) /* 执行当前语句并推进处理流程。 */
        continue /* 执行当前语句并推进处理流程。 */
      } /* 结束当前表达式或代码块。 */
      const file = await readFile(join('dev', 'dist', `${id}.zip`)) /* 声明 file。 */
      const form = new FormData() /* 声明 form。 */
      form.append('file', new Blob([file], { type: 'application/zip' }), `${id}.zip`) /* 执行当前语句并推进处理流程。 */
      form.append('publish', 'true') /* 执行当前语句并推进处理流程。 */
      const result = await api(`/api/v2/protocols/${id}/source-releases`, { method: 'POST', body: form, token: accessToken }) /* 声明 result。 */
      entry.upload = 'passed' /* 更新 entry.upload 的值。 */
      entry.actualVersion = result.release?.version /* 更新 entry.actualVersion 的值。 */
      entry.sampleCount = result.release?.sampleCount ?? result.release?.testCount /* 更新 entry.sampleCount 的值。 */
      console.log(`上传通过 ${id}@${entry.actualVersion || entry.version}`) /* 执行当前语句并推进处理流程。 */
    } catch (error) { /* 结束当前表达式或代码块。 */
      entry.upload = 'failed' /* 更新 entry.upload 的值。 */
      entry.error = error.message /* 更新 entry.error 的值。 */
      console.log(`上传失败 ${id}: ${error.message}`) /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
    await writeFile(reportPath, JSON.stringify(report, null, 2)) /* 等待异步操作完成。 */
  } /* 结束当前表达式或代码块。 */
  await writeFile(reportPath, JSON.stringify(report, null, 2)) /* 等待异步操作完成。 */
} /* 结束当前表达式或代码块。 */

const versionOf = id => ['kuka-modbus', 'sp-cannon'].includes(id) ? '1.0.1' : '1.0.0' /* 声明 versionOf。 */
const productOf = id => `verify-20260923-${id}` /* 声明 productOf。 */
const profileOf = id => `verify-20260923-${id}-tcp` /* 声明 profileOf。 */
const testPorts = Object.fromEntries(names.map((id, index) => [id, 29101 + index])) /* 声明 testPorts。 */
const deviceOf = { fb2018: 'fb2018_70001', fb2024: 'fb_1', 'fb-hydraulic': '868892074243446', 'fb-liquid-level': '868892074243447', 'kuka-modbus': 'verify-20260923-kuka', 'sp-cannon': 'verify-20260923-cannon' } /* 声明 deviceOf。 */

async function waitFor(check, timeoutMs = 30000) { /* 定义 waitFor 函数。 */
  const until = Date.now() + timeoutMs /* 声明 until。 */
  while (Date.now() < until) { /* 循环处理当前数据。 */
    const value = await check() /* 声明 value。 */
    if (value) return value /* 判断条件并选择处理分支。 */
    await new Promise(resolve => setTimeout(resolve, 500)) /* 等待异步操作完成。 */
  } /* 结束当前表达式或代码块。 */
  throw new Error('等待平台状态超时') /* 抛出当前错误。 */
} /* 结束当前表达式或代码块。 */

if (process.argv[2] === 'setup') { /* 判断条件并选择处理分支。 */
  const products = await api('/api/v1/products?pageSize=100', { token: accessToken }) /* 声明 products。 */
  const profiles = await api('/api/v2/device-access-profiles', { token: accessToken }) /* 声明 profiles。 */
  for (const id of names) { /* 循环处理当前数据。 */
    const productId = productOf(id) /* 声明 productId。 */
    if (!products.items?.some(product => product.id === productId)) { /* 判断条件并选择处理分支。 */
      const body = { id: productId, name: `虚拟测试 · ${id}`, category: id.startsWith('fb20') ? 'gateway' : 'sensor', transport: 'TCP', payloadFormat: 'hex', protocolPackageId: `${id}@${versionOf(id)}`, status: 'ENABLED' } /* 声明 body。 */
      await api('/api/v1/products', { method: 'POST', body, token: accessToken }) /* 等待异步操作完成。 */
    } /* 结束当前表达式或代码块。 */
    const binding = await api(`/api/v2/products/${productId}/protocol-binding`, { token: accessToken }) /* 声明 binding。 */
    if (binding.protocolId !== id || binding.version !== versionOf(id)) throw new Error(`产品协议绑定错误: ${id}: ${JSON.stringify(binding)}`) /* 判断条件并选择处理分支。 */
    if (id === 'kuka-modbus' || id === 'sp-cannon') { /* 判断条件并选择处理分支。 */
      const deviceId = deviceOf[id] /* 声明 deviceId。 */
      const listed = await api('/api/v1/device-registry?pageSize=100', { token: accessToken }) /* 声明 listed。 */
      if (!listed.items?.some(item => item.device?.id === deviceId)) { /* 判断条件并选择处理分支。 */
        await api('/api/v1/device-registry', { method: 'POST', body: { id: deviceId, name: `虚拟测试 · ${id}`, productId, status: 'ENABLED' }, token: accessToken }) /* 等待异步操作完成。 */
      } /* 结束当前表达式或代码块。 */
    } else { /* 结束当前表达式或代码块。 */
      const body = { id: profileOf(id), mode: 'listener', network: 'tcp', connectionMode: 'listen', host: '127.0.0.1', port: testPorts[id], timeoutMs: 5000, productId, protocolId: id, protocolVersion: versionOf(id), autoRegister: true, enabled: true } /* 声明 body。 */
      const exists = profiles.items?.some(profile => profile.id === profileOf(id)) /* 声明 exists。 */
      await api(exists ? `/api/v2/device-access-profiles/${profileOf(id)}` : '/api/v2/device-access-profiles', { method: exists ? 'PUT' : 'POST', body, token: accessToken }) /* 等待异步操作完成。 */
    } /* 结束当前表达式或代码块。 */
    console.log(`测试产品就绪 ${id}`) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  for (const id of names.slice(0, 4)) { /* 循环处理当前数据。 */
    await waitFor(async () => { /* 等待异步操作完成。 */
      const value = await api('/api/v2/device-access-profiles', { token: accessToken }) /* 声明 value。 */
      const profile = value.items?.find(item => item.id === profileOf(id)) /* 声明 profile。 */
      if (profile?.runtimeStatus === 'ERROR') throw new Error(`${id} 监听失败: ${profile.lastError}`) /* 判断条件并选择处理分支。 */
      return profile?.runtimeStatus === 'LISTENING' /* 返回当前处理结果。 */
    }) /* 结束当前表达式或代码块。 */
    console.log(`网关监听就绪 ${id} 127.0.0.1:${testPorts[id]}`) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  report.setup = { status: 'passed', products: names.map(productOf), listenerProfiles: names.slice(0, 4).map(profileOf), at: new Date().toISOString() } /* 更新 report.setup 的值。 */
  await writeFile(reportPath, JSON.stringify(report, null, 2)) /* 等待异步操作完成。 */
} /* 结束当前表达式或代码块。 */

function hexBytes(text) { return Buffer.from(text.replace(/\s+/g, ''), 'hex') } /* 定义 hexBytes 函数。 */
function checksumFB(frame) { /* 定义 checksumFB 函数。 */
  let sum = 0 /* 声明 sum。 */
  for (let i = 2; i < frame.length - 3; i++) sum = (sum + frame[i]) & 255 /* 循环处理当前数据。 */
  frame[frame.length - 3] = sum /* 执行当前语句并推进处理流程。 */
  return frame /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
function fbWithApp(template, app) { /* 定义 fbWithApp 函数。 */
  const frame = Buffer.alloc(30 + app.length) /* 声明 frame。 */
  template.copy(frame, 0, 0, 27) /* 执行当前语句并推进处理流程。 */
  frame.writeUInt16LE(app.length, 24) /* 执行当前语句并推进处理流程。 */
  app.copy(frame, 27) /* 执行当前语句并推进处理流程。 */
  frame[frame.length - 2] = 0x23 /* 执行当前语句并推进处理流程。 */
  frame[frame.length - 1] = 0x23 /* 执行当前语句并推进处理流程。 */
  return checksumFB(frame) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
function modbusCRC(data) { /* 定义 modbusCRC 函数。 */
  let crc = 0xffff /* 声明 crc。 */
  for (const byte of data) { /* 循环处理当前数据。 */
    crc ^= byte /* 执行当前语句并推进处理流程。 */
    for (let i = 0; i < 8; i++) crc = crc & 1 ? (crc >>> 1) ^ 0xa001 : crc >>> 1 /* 循环处理当前数据。 */
  } /* 结束当前表达式或代码块。 */
  return Buffer.concat([data, Buffer.from([crc & 255, crc >>> 8])]) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
async function connect(port) { /* 定义 connect 函数。 */
  return await new Promise((resolve, reject) => { /* 返回当前处理结果。 */
    const socket = createConnection({ host: '127.0.0.1', port }, () => resolve(socket)) /* 声明 socket。 */
    socket.once('error', reject) /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
const pause = ms => new Promise(resolve => setTimeout(resolve, ms)) /* 声明 pause。 */
const testStart = Date.now() - 1000 /* 声明 testStart。 */
async function parsedFor(deviceId, atLeast) { /* 定义 parsedFor 函数。 */
  return await waitFor(async () => { /* 返回当前处理结果。 */
    const listing = await api(`/api/v1/raw-messages?deviceId=${encodeURIComponent(deviceId)}&start=${testStart}&pageSize=100`, { token: accessToken }) /* 声明 listing。 */
    if ((listing.items || []).filter(item => item.parsed).length < atLeast) return false /* 判断条件并选择处理分支。 */
    const details = await Promise.all(listing.items.map(item => api(`/api/v1/raw-messages/${item.messageId}`, { token: accessToken }))) /* 声明 details。 */
    if (details.filter(item => item.parseStatus === 'PARSED').length < atLeast) return false /* 判断条件并选择处理分支。 */
    return details /* 返回当前处理结果。 */
  }, 40000) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
async function alarmFor(deviceId, status) { /* 定义 alarmFor 函数。 */
  return await waitFor(async () => { /* 返回当前处理结果。 */
    const listing = await api(`/api/v1/alarms?deviceId=${encodeURIComponent(deviceId)}&start=${testStart}&pageSize=100`, { token: accessToken }) /* 声明 listing。 */
    return listing.items?.find(item => item.status === status) || false /* 返回当前处理结果。 */
  }, 30000) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
async function legacyExample(id, functionName) { /* 定义 legacyExample 函数。 */
  const code = await readFile(join('dev', id, 'protocol.go'), 'utf8') /* 声明 code。 */
  const match = code.match(new RegExp(`${functionName}\\("([0-9A-Fa-f ]+)"\\)`)) /* 声明 match。 */
  if (!match) throw new Error(`未找到 ${id} 的旧项目样本`) /* 判断条件并选择处理分支。 */
  return hexBytes(match[1]) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

if (process.argv[2] === 'listen-test') { /* 判断条件并选择处理分支。 */
  const outcomes = [] /* 声明 outcomes。 */
  const fb18 = await legacyExample('fb2018', 'mustHex') /* 声明 fb18。 */
  const fb18Socket = await connect(testPorts.fb2018) /* 声明 fb18Socket。 */
  fb18Socket.write(fb18) /* 执行当前语句并推进处理流程。 */
  const fb18Raw = await parsedFor(deviceOf.fb2018, 1) /* 声明 fb18Raw。 */
  assert.equal(fb18Raw.find(item => item.standardMessage)?.standardMessage.properties.fireAlarm, true) /* 验证实际结果符合预期。 */
  await alarmFor(deviceOf.fb2018, 'ACTIVE') /* 等待异步操作完成。 */
  const fb18RecoveryApp = Buffer.from(fb18.subarray(27, fb18.length - 3)) /* 声明 fb18RecoveryApp。 */
  fb18RecoveryApp[0] = 0x87 /* 更新 fb18RecoveryApp[0] 的值。 */
  fb18RecoveryApp[2 + 7] = 0 /* 执行当前语句并推进处理流程。 */
  fb18RecoveryApp[2 + 8] = 0 /* 执行当前语句并推进处理流程。 */
  await pause(1100) /* 等待异步操作完成。 */
  fb18Socket.write(fbWithApp(fb18, fb18RecoveryApp)) /* 执行当前语句并推进处理流程。 */
  await parsedFor(deviceOf.fb2018, 2) /* 等待异步操作完成。 */
  await alarmFor(deviceOf.fb2018, 'RECOVERED') /* 等待异步操作完成。 */
  fb18Socket.end() /* 执行当前语句并推进处理流程。 */
  outcomes.push({ id: 'fb2018', parsed: 2, fireAndRecovery: true }) /* 执行当前语句并推进处理流程。 */
  console.log('虚拟设备通过 fb2018：火警、恢复、2 条原文解析') /* 执行当前语句并推进处理流程。 */

  const fb24 = await legacyExample('fb2024', 'hexBytes') /* 声明 fb24。 */
  const fb24Socket = await connect(testPorts.fb2024) /* 声明 fb24Socket。 */
  fb24Socket.write(fb24.subarray(0, 10)) /* 执行当前语句并推进处理流程。 */
  await pause(50) /* 等待异步操作完成。 */
  fb24Socket.write(fb24.subarray(10)) /* 执行当前语句并推进处理流程。 */
  const fb24Raw = await parsedFor(deviceOf.fb2024, 1) /* 声明 fb24Raw。 */
  assert.equal(fb24Raw.find(item => item.standardMessage)?.standardMessage.properties.objects?.[0]?.deviceType, 91) /* 验证实际结果符合预期。 */
  const statusApp = hexBytes('15 01 08 02 3A 09 1A 09 0C') /* 声明 statusApp。 */
  fb24Socket.write(fbWithApp(fb24, statusApp)) /* 执行当前语句并推进处理流程。 */
  await parsedFor(deviceOf.fb2024, 2) /* 等待异步操作完成。 */
  await alarmFor(deviceOf.fb2024, 'ACTIVE') /* 等待异步操作完成。 */
  await pause(1100) /* 等待异步操作完成。 */
  statusApp[0] = 0x88 /* 更新 statusApp[0] 的值。 */
  fb24Socket.write(fbWithApp(fb24, statusApp)) /* 执行当前语句并推进处理流程。 */
  await parsedFor(deviceOf.fb2024, 3) /* 等待异步操作完成。 */
  await alarmFor(deviceOf.fb2024, 'RECOVERED') /* 等待异步操作完成。 */
  fb24Socket.end() /* 执行当前语句并推进处理流程。 */
  outcomes.push({ id: 'fb2024', parsed: 3, switchInputAndFaultRecovery: true }) /* 执行当前语句并推进处理流程。 */
  console.log('虚拟设备通过 fb2024：半包、开关量、电源故障与恢复') /* 执行当前语句并推进处理流程。 */

  const combined = hexBytes('38363838393230373432343334343601460000000D1A00000064003C0000000000160005000000000000000000010000503801460016000102000021B0') /* 声明 combined。 */
  for (const id of ['fb-hydraulic', 'fb-liquid-level']) { /* 循环处理当前数据。 */
    const deviceId = deviceOf[id] /* 声明 deviceId。 */
    const packet = Buffer.from(combined) /* 声明 packet。 */
    Buffer.from(deviceId).copy(packet, 0, 0, 15) /* 执行当前语句并推进处理流程。 */
    const socket = await connect(testPorts[id]) /* 声明 socket。 */
    let receivedCommand = false /* 声明 receivedCommand。 */
    socket.on('data', bytes => { /* 执行当前语句并推进处理流程。 */
      if (bytes.length >= 8 && bytes[1] === 6) { receivedCommand = true; socket.write(bytes.subarray(0, 8)) } /* 判断条件并选择处理分支。 */
    }) /* 结束当前表达式或代码块。 */
    socket.write(packet) /* 执行当前语句并推进处理流程。 */
    const initial = await parsedFor(deviceId, 2) /* 声明 initial。 */
    assert.equal(initial.some(item => item.standardMessage?.properties?.batteryLevel === 100), true) /* 验证实际结果符合预期。 */
    const alarmOn = modbusCRC(hexBytes('01 46 00 16 00 01 02 00 01')) /* 声明 alarmOn。 */
    socket.write(alarmOn) /* 执行当前语句并推进处理流程。 */
    await parsedFor(deviceId, 3) /* 等待异步操作完成。 */
    await alarmFor(deviceId, 'ACTIVE') /* 等待异步操作完成。 */
    await pause(1100) /* 等待异步操作完成。 */
    socket.write(modbusCRC(hexBytes('01 46 00 16 00 01 02 00 00'))) /* 执行当前语句并推进处理流程。 */
    await parsedFor(deviceId, 4) /* 等待异步操作完成。 */
    await alarmFor(deviceId, 'RECOVERED') /* 等待异步操作完成。 */
    const command = await api(`/api/v2/device-access-profiles/${profileOf(id)}/devices/${deviceId}/commands`, { /* 声明 command。 */
      method: 'POST', body: { type: 'setDetectionTime', value: 5, confirmed: true }, token: accessToken, timeoutMs: 12000, /* 执行当前语句并推进处理流程。 */
    }) /* 结束当前表达式或代码块。 */
    assert.equal(command.status, 'acknowledged') /* 验证实际结果符合预期。 */
    assert.equal(receivedCommand, true) /* 验证实际结果符合预期。 */
    await parsedFor(deviceId, 5) /* 等待异步操作完成。 */
    socket.end() /* 执行当前语句并推进处理流程。 */
    outcomes.push({ id, parsed: 5, combinedFrame: true, alarmAndRecovery: true, commandStatus: command.status }) /* 执行当前语句并推进处理流程。 */
    console.log(`虚拟设备通过 ${id}：组合报文、报警恢复、下行写入确认`) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  report.listenerTests = outcomes /* 更新 report.listenerTests 的值。 */
  await writeFile(reportPath, JSON.stringify(report, null, 2)) /* 等待异步操作完成。 */
} /* 结束当前表达式或代码块。 */

function modbusServer(id) { /* 定义 modbusServer 函数。 */
  const seen = new Map() /* 声明 seen。 */
  const server = createServer(socket => { /* 声明 server。 */
    let pending = Buffer.alloc(0) /* 声明 pending。 */
    socket.on('data', data => { /* 执行当前语句并推进处理流程。 */
      pending = Buffer.concat([pending, data]) /* 更新 pending 的值。 */
      while (pending.length >= 6) { /* 循环处理当前数据。 */
        const length = 6 + pending.readUInt16BE(4) /* 声明 length。 */
        if (pending.length < length) break /* 判断条件并选择处理分支。 */
        const request = pending.subarray(0, length) /* 声明 request。 */
        pending = pending.subarray(length) /* 更新 pending 的值。 */
        const address = request.readUInt16BE(8) /* 声明 address。 */
        const count = request.readUInt16BE(10) /* 声明 count。 */
        const nth = (seen.get(address) || 0) + 1 /* 声明 nth。 */
        seen.set(address, nth) /* 执行当前语句并推进处理流程。 */
        assert.equal(request[6], 1) /* 验证实际结果符合预期。 */
        if (id === 'kuka-modbus') { /* 判断条件并选择处理分支。 */
          assert.equal(request[7], 1) /* 验证实际结果符合预期。 */
          assert.equal(count, 1) /* 验证实际结果符合预期。 */
          const value = address === 0 ? 1 : address === 3001 && nth === 1 ? 1 : 0 /* 声明 value。 */
          socket.write(Buffer.from([request[0], request[1], 0, 0, 0, 4, 1, 1, 1, value])) /* 执行当前语句并推进处理流程。 */
        } else { /* 结束当前表达式或代码块。 */
          assert.equal(request[7], 4) /* 验证实际结果符合预期。 */
          const values = address === 200 ? [nth === 1 ? 1 : 0, 0, 1] : [nth === 1 && (address === 0 || address === 100) ? 1 : 0] /* 声明 values。 */
          assert.equal(count, values.length) /* 验证实际结果符合预期。 */
          const response = Buffer.alloc(9 + values.length * 2) /* 声明 response。 */
          request.copy(response, 0, 0, 2) /* 执行当前语句并推进处理流程。 */
          response.writeUInt16BE(3 + values.length * 2, 4) /* 执行当前语句并推进处理流程。 */
          response[6] = 1 /* 更新 response[6] 的值。 */
          response[7] = 4 /* 更新 response[7] 的值。 */
          response[8] = values.length * 2 /* 更新 response[8] 的值。 */
          values.forEach((value, index) => response.writeUInt16BE(value, 9 + index * 2)) /* 执行当前语句并推进处理流程。 */
          socket.write(response) /* 执行当前语句并推进处理流程。 */
        } /* 结束当前表达式或代码块。 */
      } /* 结束当前表达式或代码块。 */
    }) /* 结束当前表达式或代码块。 */
  }) /* 结束当前表达式或代码块。 */
  return { server, seen } /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

async function disableTestProfiles() { /* 定义 disableTestProfiles 函数。 */
  const listing = await api('/api/v2/device-access-profiles', { token: accessToken }) /* 声明 listing。 */
  for (const id of names) { /* 循环处理当前数据。 */
    const profile = listing.items?.find(item => item.id === profileOf(id)) /* 声明 profile。 */
    if (profile?.enabled) { /* 判断条件并选择处理分支。 */
      await api(`/api/v2/device-access-profiles/${profile.id}`, { method: 'PUT', body: { ...profile, enabled: false }, token: accessToken }) /* 等待异步操作完成。 */
    } /* 结束当前表达式或代码块。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

if (process.argv[2] === 'dial-test') { /* 判断条件并选择处理分支。 */
  const ids = ['kuka-modbus', 'sp-cannon'] /* 声明 ids。 */
  const mocks = new Map() /* 声明 mocks。 */
  const outcomes = [] /* 声明 outcomes。 */
  try { /* 执行当前语句并推进处理流程。 */
    for (const id of ids) { /* 循环处理当前数据。 */
      const mock = modbusServer(id) /* 声明 mock。 */
      await new Promise((resolve, reject) => { /* 等待异步操作完成。 */
        mock.server.once('error', reject) /* 执行当前语句并推进处理流程。 */
        mock.server.listen(testPorts[id], '127.0.0.1', resolve) /* 执行当前语句并推进处理流程。 */
      }) /* 结束当前表达式或代码块。 */
      mocks.set(id, mock) /* 执行当前语句并推进处理流程。 */
      const queries = id === 'kuka-modbus' /* 声明 queries。 */
        ? [0, 3001, 3002, 3003, 3013, 3042].map(address => ({ type: `coil-${address}`, intervalSec: 5 })) /* 执行当前语句并推进处理流程。 */
        : ['host', ...Array.from({ length: 8 }, (_, i) => `fault-${i + 1}`), ...Array.from({ length: 8 }, (_, i) => `status-${i + 1}`)].map(type => ({ type, intervalSec: 5 })) /* 执行当前语句并推进处理流程。 */
      const profile = { /* 声明 profile。 */
        id: profileOf(id), mode: 'listener', network: 'tcp', connectionMode: 'dial', host: '127.0.0.1', /* 执行当前语句并推进处理流程。 */
        port: testPorts[id], timeoutMs: 5000, productId: productOf(id), deviceId: deviceOf[id], /* 执行当前语句并推进处理流程。 */
        protocolId: id, protocolVersion: versionOf(id), queries, enabled: true, /* 执行当前语句并推进处理流程。 */
      } /* 结束当前表达式或代码块。 */
      const existing = await api('/api/v2/device-access-profiles', { token: accessToken }) /* 声明 existing。 */
      const exists = existing.items?.some(item => item.id === profile.id) /* 声明 exists。 */
      await api(exists ? `/api/v2/device-access-profiles/${profile.id}` : '/api/v2/device-access-profiles', { method: exists ? 'PUT' : 'POST', body: profile, token: accessToken }) /* 等待异步操作完成。 */
      console.log(`主动连接已建立 ${id}：${queries.length} 个查询点`) /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
    await Promise.all(ids.map(async id => { /* 等待异步操作完成。 */
      const mock = mocks.get(id) /* 声明 mock。 */
      const expected = id === 'kuka-modbus' ? [0, 3001, 3002, 3003, 3013, 3042] : [200, ...Array.from({ length: 8 }, (_, i) => i), ...Array.from({ length: 8 }, (_, i) => i + 100)] /* 声明 expected。 */
      await waitFor(() => (mock.seen.get(id === 'kuka-modbus' ? 3001 : 200) || 0) >= 1, 40000) /* 等待异步操作完成。 */
      await alarmFor(deviceOf[id], 'ACTIVE') /* 等待异步操作完成。 */
      await waitFor(() => expected.every(address => (mock.seen.get(address) || 0) >= 1), 40000) /* 等待异步操作完成。 */
      await parsedFor(deviceOf[id], expected.length) /* 等待异步操作完成。 */
      await waitFor(() => expected.every(address => (mock.seen.get(address) || 0) >= 2), 40000) /* 等待异步操作完成。 */
      await parsedFor(deviceOf[id], expected.length * 2) /* 等待异步操作完成。 */
      await alarmFor(deviceOf[id], 'RECOVERED') /* 等待异步操作完成。 */
      const listing = await api(`/api/v1/raw-messages?deviceId=${encodeURIComponent(deviceOf[id])}&start=${testStart}&pageSize=100`, { token: accessToken }) /* 声明 listing。 */
      const parsed = listing.items.find(item => item.parsed) /* 声明 parsed。 */
      assert.ok(parsed, `${id} 缺少已解析原文`) /* 验证实际结果符合预期。 */
      const latest = await api(`/api/v1/raw-messages/${parsed.messageId}`, { token: accessToken }) /* 声明 latest。 */
      assert.equal(latest.parseStatus, 'PARSED') /* 验证实际结果符合预期。 */
      outcomes.push({ id, queryCount: expected.length, pollCycles: 2, parsedAtLeast: expected.length * 2, alarmAndRecovery: true, rawMessageId: parsed.messageId }) /* 执行当前语句并推进处理流程。 */
      console.log(`虚拟 Modbus 设备通过 ${id}：${expected.length} 点轮询两轮、报警与恢复`) /* 执行当前语句并推进处理流程。 */
    })) /* 结束当前表达式或代码块。 */
    report.dialTests = outcomes /* 更新 report.dialTests 的值。 */
    await writeFile(reportPath, JSON.stringify(report, null, 2)) /* 等待异步操作完成。 */
  } finally { /* 结束当前表达式或代码块。 */
    await disableTestProfiles() /* 等待异步操作完成。 */
    await Promise.all(Array.from(mocks.values(), mock => new Promise(resolve => mock.server.close(resolve)))) /* 等待异步操作完成。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

if (process.argv[2] === 'final-check') { /* 判断条件并选择处理分支。 */
  const protocols = await api('/api/v2/protocols?pageSize=100', { token: accessToken }) /* 声明 protocols。 */
  const products = await api('/api/v1/products?pageSize=100', { token: accessToken }) /* 声明 products。 */
  const profiles = await api('/api/v2/device-access-profiles', { token: accessToken }) /* 声明 profiles。 */
  const checks = [] /* 声明 checks。 */
  for (const id of names) { /* 循环处理当前数据。 */
    const version = versionOf(id) /* 声明 version。 */
    const definition = protocols.items?.find(item => item.definition?.id === id) /* 声明 definition。 */
    const release = definition?.releases?.find(item => item.version === version) /* 声明 release。 */
    assert.equal(release?.status, 'PUBLISHED', `${id} 发布状态`) /* 验证实际结果符合预期。 */
    assert.equal(products.items?.some(item => item.id === productOf(id)), true, `${id} 测试产品`) /* 验证实际结果符合预期。 */
    const binding = await api(`/api/v2/products/${productOf(id)}/protocol-binding`, { token: accessToken }) /* 声明 binding。 */
    assert.equal(`${binding.protocolId}@${binding.version}`, `${id}@${version}`) /* 验证实际结果符合预期。 */
    const profile = profiles.items?.find(item => item.id === profileOf(id)) /* 声明 profile。 */
    assert.equal(profile?.enabled, false, `${id} 测试实例应停用`) /* 验证实际结果符合预期。 */
    assert.equal(profile?.runtimeStatus, 'DISABLED', `${id} 实例运行状态`) /* 验证实际结果符合预期。 */
    const raw = await api(`/api/v1/raw-messages?deviceId=${encodeURIComponent(deviceOf[id])}&pageSize=100`, { token: accessToken }) /* 声明 raw。 */
    assert.equal(raw.items?.some(item => item.parsed), true, `${id} 解析记录`) /* 验证实际结果符合预期。 */
    const alarms = await api(`/api/v1/alarms?deviceId=${encodeURIComponent(deviceOf[id])}&pageSize=100`, { token: accessToken }) /* 声明 alarms。 */
    assert.equal(alarms.items?.some(item => item.status === 'RECOVERED'), true, `${id} 告警恢复记录`) /* 验证实际结果符合预期。 */
    checks.push({ id, version, release: 'PUBLISHED', productBound: true, profile: 'DISABLED', parsedRaw: true, recoveredAlarm: true }) /* 执行当前语句并推进处理流程。 */
    console.log(`最终核对通过 ${id}@${version}`) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  report.finalCheck = { status: 'passed', at: new Date().toISOString(), checks } /* 更新 report.finalCheck 的值。 */
  await writeFile(reportPath, JSON.stringify(report, null, 2)) /* 等待异步操作完成。 */
} /* 结束当前表达式或代码块。 */
