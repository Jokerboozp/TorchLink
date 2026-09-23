// Local verification of the six protocol packages against the running platform.
// Reads credentials from .env.local; never writes them to the result file.
import { readFile, writeFile } from 'node:fs/promises'
import { parseEnv } from 'node:util'
import { join } from 'node:path'
import { createConnection, createServer } from 'node:net'
import assert from 'node:assert/strict'

const env = parseEnv(await readFile('.env.local', 'utf8'))
const base = process.env.IOT_VERIFY_ORIGIN || 'http://127.0.0.1:8081'
const tenant = (env.IOT_ADMIN_TENANTS || 'tenant_001').split(',')[0].trim()
const names = ['fb2018', 'fb2024', 'fb-hydraulic', 'fb-liquid-level', 'kuka-modbus', 'sp-cannon']
const reportPath = 'dev/verification-20260923.json'
let report
try { report = JSON.parse(await readFile(reportPath, 'utf8')) } catch { report = { origin: base, tenant, packages: [] } }
if (report.origin !== base || report.tenant !== tenant) throw new Error('验证报告属于另一个平台或租户')
report.at = new Date().toISOString()

async function api(path, { method = 'GET', body, token, timeoutMs = 240000 } = {}) {
  const headers = token ? { Authorization: `Bearer ${token}` } : {}
  if (body && !(body instanceof FormData)) headers['Content-Type'] = 'application/json'
  const response = await fetch(base + path, {
    method, headers,
    body: body instanceof FormData ? body : body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(timeoutMs), redirect: 'error',
  })
  const result = await response.json().catch(() => ({}))
  if (!response.ok) {
    const error = new Error(`${method} ${path}: HTTP ${response.status}: ${String(result.detail || result.error || '').slice(0, 800)}`)
    error.status = response.status
    throw error
  }
  return result
}

const { accessToken } = await api('/api/v1/auth/login', {
  method: 'POST', body: { username: env.IOT_ADMIN_USER || 'admin', password: env.IOT_ADMIN_PASSWORD, tenantId: tenant },
})
if (!accessToken) throw new Error('平台登录未返回 token')

if ((process.argv[2] || 'upload') === 'upload') {
  report.packages = []
  const existing = await api('/api/v2/protocols?pageSize=100', { token: accessToken })
  for (const id of names) {
    const entry = { id, version: ['kuka-modbus', 'sp-cannon'].includes(id) ? '1.0.1' : '1.0.0', upload: 'pending' }
    report.packages.push(entry)
    try {
      const prior = existing.items?.find(item => item.definition?.id === id)?.releases?.find(release => release.version === entry.version)
      if (prior) {
        entry.upload = prior.status === 'PUBLISHED' ? 'already-published' : 'existing-unpublished'
        console.log(`已有版本 ${id}@${entry.version}: ${prior.status}`)
        continue
      }
      const file = await readFile(join('dev', 'dist', `${id}.zip`))
      const form = new FormData()
      form.append('file', new Blob([file], { type: 'application/zip' }), `${id}.zip`)
      form.append('publish', 'true')
      const result = await api(`/api/v2/protocols/${id}/source-releases`, { method: 'POST', body: form, token: accessToken })
      entry.upload = 'passed'
      entry.actualVersion = result.release?.version
      entry.sampleCount = result.release?.sampleCount ?? result.release?.testCount
      console.log(`上传通过 ${id}@${entry.actualVersion || entry.version}`)
    } catch (error) {
      entry.upload = 'failed'
      entry.error = error.message
      console.log(`上传失败 ${id}: ${error.message}`)
    }
    await writeFile(reportPath, JSON.stringify(report, null, 2))
  }
  await writeFile(reportPath, JSON.stringify(report, null, 2))
}

const versionOf = id => ['kuka-modbus', 'sp-cannon'].includes(id) ? '1.0.1' : '1.0.0'
const productOf = id => `verify-20260923-${id}`
const profileOf = id => `verify-20260923-${id}-tcp`
const testPorts = Object.fromEntries(names.map((id, index) => [id, 29101 + index]))
const deviceOf = { fb2018: 'fb2018_70001', fb2024: 'fb_1', 'fb-hydraulic': '868892074243446', 'fb-liquid-level': '868892074243447', 'kuka-modbus': 'verify-20260923-kuka', 'sp-cannon': 'verify-20260923-cannon' }

async function waitFor(check, timeoutMs = 30000) {
  const until = Date.now() + timeoutMs
  while (Date.now() < until) {
    const value = await check()
    if (value) return value
    await new Promise(resolve => setTimeout(resolve, 500))
  }
  throw new Error('等待平台状态超时')
}

if (process.argv[2] === 'setup') {
  const products = await api('/api/v1/products?pageSize=100', { token: accessToken })
  const profiles = await api('/api/v2/device-access-profiles', { token: accessToken })
  for (const id of names) {
    const productId = productOf(id)
    if (!products.items?.some(product => product.id === productId)) {
      const body = { id: productId, name: `虚拟测试 · ${id}`, category: id.startsWith('fb20') ? 'gateway' : 'sensor', transport: 'TCP', payloadFormat: 'hex', protocolPackageId: `${id}@${versionOf(id)}`, status: 'ENABLED' }
      await api('/api/v1/products', { method: 'POST', body, token: accessToken })
    }
    const binding = await api(`/api/v2/products/${productId}/protocol-binding`, { token: accessToken })
    if (binding.protocolId !== id || binding.version !== versionOf(id)) throw new Error(`产品协议绑定错误: ${id}: ${JSON.stringify(binding)}`)
    if (id === 'kuka-modbus' || id === 'sp-cannon') {
      const deviceId = deviceOf[id]
      const listed = await api('/api/v1/device-registry?pageSize=100', { token: accessToken })
      if (!listed.items?.some(item => item.device?.id === deviceId)) {
        await api('/api/v1/device-registry', { method: 'POST', body: { id: deviceId, name: `虚拟测试 · ${id}`, productId, status: 'ENABLED' }, token: accessToken })
      }
    } else {
      const body = { id: profileOf(id), mode: 'listener', network: 'tcp', connectionMode: 'listen', host: '127.0.0.1', port: testPorts[id], timeoutMs: 5000, productId, protocolId: id, protocolVersion: versionOf(id), autoRegister: true, enabled: true }
      const exists = profiles.items?.some(profile => profile.id === profileOf(id))
      await api(exists ? `/api/v2/device-access-profiles/${profileOf(id)}` : '/api/v2/device-access-profiles', { method: exists ? 'PUT' : 'POST', body, token: accessToken })
    }
    console.log(`测试产品就绪 ${id}`)
  }
  for (const id of names.slice(0, 4)) {
    await waitFor(async () => {
      const value = await api('/api/v2/device-access-profiles', { token: accessToken })
      const profile = value.items?.find(item => item.id === profileOf(id))
      if (profile?.runtimeStatus === 'ERROR') throw new Error(`${id} 监听失败: ${profile.lastError}`)
      return profile?.runtimeStatus === 'LISTENING'
    })
    console.log(`网关监听就绪 ${id} 127.0.0.1:${testPorts[id]}`)
  }
  report.setup = { status: 'passed', products: names.map(productOf), listenerProfiles: names.slice(0, 4).map(profileOf), at: new Date().toISOString() }
  await writeFile(reportPath, JSON.stringify(report, null, 2))
}

function hexBytes(text) { return Buffer.from(text.replace(/\s+/g, ''), 'hex') }
function checksumFB(frame) {
  let sum = 0
  for (let i = 2; i < frame.length - 3; i++) sum = (sum + frame[i]) & 255
  frame[frame.length - 3] = sum
  return frame
}
function fbWithApp(template, app) {
  const frame = Buffer.alloc(30 + app.length)
  template.copy(frame, 0, 0, 27)
  frame.writeUInt16LE(app.length, 24)
  app.copy(frame, 27)
  frame[frame.length - 2] = 0x23
  frame[frame.length - 1] = 0x23
  return checksumFB(frame)
}
function modbusCRC(data) {
  let crc = 0xffff
  for (const byte of data) {
    crc ^= byte
    for (let i = 0; i < 8; i++) crc = crc & 1 ? (crc >>> 1) ^ 0xa001 : crc >>> 1
  }
  return Buffer.concat([data, Buffer.from([crc & 255, crc >>> 8])])
}
async function connect(port) {
  return await new Promise((resolve, reject) => {
    const socket = createConnection({ host: '127.0.0.1', port }, () => resolve(socket))
    socket.once('error', reject)
  })
}
const pause = ms => new Promise(resolve => setTimeout(resolve, ms))
const testStart = Date.now() - 1000
async function parsedFor(deviceId, atLeast) {
  return await waitFor(async () => {
    const listing = await api(`/api/v1/raw-messages?deviceId=${encodeURIComponent(deviceId)}&start=${testStart}&pageSize=100`, { token: accessToken })
    if ((listing.items || []).filter(item => item.parsed).length < atLeast) return false
    const details = await Promise.all(listing.items.map(item => api(`/api/v1/raw-messages/${item.messageId}`, { token: accessToken })))
    if (details.filter(item => item.parseStatus === 'PARSED').length < atLeast) return false
    return details
  }, 40000)
}
async function alarmFor(deviceId, status) {
  return await waitFor(async () => {
    const listing = await api(`/api/v1/alarms?deviceId=${encodeURIComponent(deviceId)}&start=${testStart}&pageSize=100`, { token: accessToken })
    return listing.items?.find(item => item.status === status) || false
  }, 30000)
}
async function legacyExample(id, functionName) {
  const code = await readFile(join('dev', id, 'protocol.go'), 'utf8')
  const match = code.match(new RegExp(`${functionName}\\("([0-9A-Fa-f ]+)"\\)`))
  if (!match) throw new Error(`未找到 ${id} 的旧项目样本`)
  return hexBytes(match[1])
}

if (process.argv[2] === 'listen-test') {
  const outcomes = []
  const fb18 = await legacyExample('fb2018', 'mustHex')
  const fb18Socket = await connect(testPorts.fb2018)
  fb18Socket.write(fb18)
  const fb18Raw = await parsedFor(deviceOf.fb2018, 1)
  assert.equal(fb18Raw.find(item => item.standardMessage)?.standardMessage.properties.fireAlarm, true)
  await alarmFor(deviceOf.fb2018, 'ACTIVE')
  const fb18RecoveryApp = Buffer.from(fb18.subarray(27, fb18.length - 3))
  fb18RecoveryApp[0] = 0x87
  fb18RecoveryApp[2 + 7] = 0
  fb18RecoveryApp[2 + 8] = 0
  await pause(1100)
  fb18Socket.write(fbWithApp(fb18, fb18RecoveryApp))
  await parsedFor(deviceOf.fb2018, 2)
  await alarmFor(deviceOf.fb2018, 'RECOVERED')
  fb18Socket.end()
  outcomes.push({ id: 'fb2018', parsed: 2, fireAndRecovery: true })
  console.log('虚拟设备通过 fb2018：火警、恢复、2 条原文解析')

  const fb24 = await legacyExample('fb2024', 'hexBytes')
  const fb24Socket = await connect(testPorts.fb2024)
  fb24Socket.write(fb24.subarray(0, 10))
  await pause(50)
  fb24Socket.write(fb24.subarray(10))
  const fb24Raw = await parsedFor(deviceOf.fb2024, 1)
  assert.equal(fb24Raw.find(item => item.standardMessage)?.standardMessage.properties.objects?.[0]?.deviceType, 91)
  const statusApp = hexBytes('15 01 08 02 3A 09 1A 09 0C')
  fb24Socket.write(fbWithApp(fb24, statusApp))
  await parsedFor(deviceOf.fb2024, 2)
  await alarmFor(deviceOf.fb2024, 'ACTIVE')
  await pause(1100)
  statusApp[0] = 0x88
  fb24Socket.write(fbWithApp(fb24, statusApp))
  await parsedFor(deviceOf.fb2024, 3)
  await alarmFor(deviceOf.fb2024, 'RECOVERED')
  fb24Socket.end()
  outcomes.push({ id: 'fb2024', parsed: 3, switchInputAndFaultRecovery: true })
  console.log('虚拟设备通过 fb2024：半包、开关量、电源故障与恢复')

  const combined = hexBytes('38363838393230373432343334343601460000000D1A00000064003C0000000000160005000000000000000000010000503801460016000102000021B0')
  for (const id of ['fb-hydraulic', 'fb-liquid-level']) {
    const deviceId = deviceOf[id]
    const packet = Buffer.from(combined)
    Buffer.from(deviceId).copy(packet, 0, 0, 15)
    const socket = await connect(testPorts[id])
    let receivedCommand = false
    socket.on('data', bytes => {
      if (bytes.length >= 8 && bytes[1] === 6) { receivedCommand = true; socket.write(bytes.subarray(0, 8)) }
    })
    socket.write(packet)
    const initial = await parsedFor(deviceId, 2)
    assert.equal(initial.some(item => item.standardMessage?.properties?.batteryLevel === 100), true)
    const alarmOn = modbusCRC(hexBytes('01 46 00 16 00 01 02 00 01'))
    socket.write(alarmOn)
    await parsedFor(deviceId, 3)
    await alarmFor(deviceId, 'ACTIVE')
    await pause(1100)
    socket.write(modbusCRC(hexBytes('01 46 00 16 00 01 02 00 00')))
    await parsedFor(deviceId, 4)
    await alarmFor(deviceId, 'RECOVERED')
    const command = await api(`/api/v2/device-access-profiles/${profileOf(id)}/devices/${deviceId}/commands`, {
      method: 'POST', body: { type: 'setDetectionTime', value: 5, confirmed: true }, token: accessToken, timeoutMs: 12000,
    })
    assert.equal(command.status, 'acknowledged')
    assert.equal(receivedCommand, true)
    await parsedFor(deviceId, 5)
    socket.end()
    outcomes.push({ id, parsed: 5, combinedFrame: true, alarmAndRecovery: true, commandStatus: command.status })
    console.log(`虚拟设备通过 ${id}：组合报文、报警恢复、下行写入确认`)
  }
  report.listenerTests = outcomes
  await writeFile(reportPath, JSON.stringify(report, null, 2))
}

function modbusServer(id) {
  const seen = new Map()
  const server = createServer(socket => {
    let pending = Buffer.alloc(0)
    socket.on('data', data => {
      pending = Buffer.concat([pending, data])
      while (pending.length >= 6) {
        const length = 6 + pending.readUInt16BE(4)
        if (pending.length < length) break
        const request = pending.subarray(0, length)
        pending = pending.subarray(length)
        const address = request.readUInt16BE(8)
        const count = request.readUInt16BE(10)
        const nth = (seen.get(address) || 0) + 1
        seen.set(address, nth)
        assert.equal(request[6], 1)
        if (id === 'kuka-modbus') {
          assert.equal(request[7], 1)
          assert.equal(count, 1)
          const value = address === 0 ? 1 : address === 3001 && nth === 1 ? 1 : 0
          socket.write(Buffer.from([request[0], request[1], 0, 0, 0, 4, 1, 1, 1, value]))
        } else {
          assert.equal(request[7], 4)
          const values = address === 200 ? [nth === 1 ? 1 : 0, 0, 1] : [nth === 1 && (address === 0 || address === 100) ? 1 : 0]
          assert.equal(count, values.length)
          const response = Buffer.alloc(9 + values.length * 2)
          request.copy(response, 0, 0, 2)
          response.writeUInt16BE(3 + values.length * 2, 4)
          response[6] = 1
          response[7] = 4
          response[8] = values.length * 2
          values.forEach((value, index) => response.writeUInt16BE(value, 9 + index * 2))
          socket.write(response)
        }
      }
    })
  })
  return { server, seen }
}

async function disableTestProfiles() {
  const listing = await api('/api/v2/device-access-profiles', { token: accessToken })
  for (const id of names) {
    const profile = listing.items?.find(item => item.id === profileOf(id))
    if (profile?.enabled) {
      await api(`/api/v2/device-access-profiles/${profile.id}`, { method: 'PUT', body: { ...profile, enabled: false }, token: accessToken })
    }
  }
}

if (process.argv[2] === 'dial-test') {
  const ids = ['kuka-modbus', 'sp-cannon']
  const mocks = new Map()
  const outcomes = []
  try {
    for (const id of ids) {
      const mock = modbusServer(id)
      await new Promise((resolve, reject) => {
        mock.server.once('error', reject)
        mock.server.listen(testPorts[id], '127.0.0.1', resolve)
      })
      mocks.set(id, mock)
      const queries = id === 'kuka-modbus'
        ? [0, 3001, 3002, 3003, 3013, 3042].map(address => ({ type: `coil-${address}`, intervalSec: 5 }))
        : ['host', ...Array.from({ length: 8 }, (_, i) => `fault-${i + 1}`), ...Array.from({ length: 8 }, (_, i) => `status-${i + 1}`)].map(type => ({ type, intervalSec: 5 }))
      const profile = {
        id: profileOf(id), mode: 'listener', network: 'tcp', connectionMode: 'dial', host: '127.0.0.1',
        port: testPorts[id], timeoutMs: 5000, productId: productOf(id), deviceId: deviceOf[id],
        protocolId: id, protocolVersion: versionOf(id), queries, enabled: true,
      }
      const existing = await api('/api/v2/device-access-profiles', { token: accessToken })
      const exists = existing.items?.some(item => item.id === profile.id)
      await api(exists ? `/api/v2/device-access-profiles/${profile.id}` : '/api/v2/device-access-profiles', { method: exists ? 'PUT' : 'POST', body: profile, token: accessToken })
      console.log(`主动连接已建立 ${id}：${queries.length} 个查询点`)
    }
    await Promise.all(ids.map(async id => {
      const mock = mocks.get(id)
      const expected = id === 'kuka-modbus' ? [0, 3001, 3002, 3003, 3013, 3042] : [200, ...Array.from({ length: 8 }, (_, i) => i), ...Array.from({ length: 8 }, (_, i) => i + 100)]
      await waitFor(() => (mock.seen.get(id === 'kuka-modbus' ? 3001 : 200) || 0) >= 1, 40000)
      await alarmFor(deviceOf[id], 'ACTIVE')
      await waitFor(() => expected.every(address => (mock.seen.get(address) || 0) >= 1), 40000)
      await parsedFor(deviceOf[id], expected.length)
      await waitFor(() => expected.every(address => (mock.seen.get(address) || 0) >= 2), 40000)
      await parsedFor(deviceOf[id], expected.length * 2)
      await alarmFor(deviceOf[id], 'RECOVERED')
      const listing = await api(`/api/v1/raw-messages?deviceId=${encodeURIComponent(deviceOf[id])}&start=${testStart}&pageSize=100`, { token: accessToken })
      const parsed = listing.items.find(item => item.parsed)
      assert.ok(parsed, `${id} 缺少已解析原文`)
      const latest = await api(`/api/v1/raw-messages/${parsed.messageId}`, { token: accessToken })
      assert.equal(latest.parseStatus, 'PARSED')
      outcomes.push({ id, queryCount: expected.length, pollCycles: 2, parsedAtLeast: expected.length * 2, alarmAndRecovery: true, rawMessageId: parsed.messageId })
      console.log(`虚拟 Modbus 设备通过 ${id}：${expected.length} 点轮询两轮、报警与恢复`)
    }))
    report.dialTests = outcomes
    await writeFile(reportPath, JSON.stringify(report, null, 2))
  } finally {
    await disableTestProfiles()
    await Promise.all(Array.from(mocks.values(), mock => new Promise(resolve => mock.server.close(resolve))))
  }
}

if (process.argv[2] === 'final-check') {
  const protocols = await api('/api/v2/protocols?pageSize=100', { token: accessToken })
  const products = await api('/api/v1/products?pageSize=100', { token: accessToken })
  const profiles = await api('/api/v2/device-access-profiles', { token: accessToken })
  const checks = []
  for (const id of names) {
    const version = versionOf(id)
    const definition = protocols.items?.find(item => item.definition?.id === id)
    const release = definition?.releases?.find(item => item.version === version)
    assert.equal(release?.status, 'PUBLISHED', `${id} 发布状态`)
    assert.equal(products.items?.some(item => item.id === productOf(id)), true, `${id} 测试产品`)
    const binding = await api(`/api/v2/products/${productOf(id)}/protocol-binding`, { token: accessToken })
    assert.equal(`${binding.protocolId}@${binding.version}`, `${id}@${version}`)
    const profile = profiles.items?.find(item => item.id === profileOf(id))
    assert.equal(profile?.enabled, false, `${id} 测试实例应停用`)
    assert.equal(profile?.runtimeStatus, 'DISABLED', `${id} 实例运行状态`)
    const raw = await api(`/api/v1/raw-messages?deviceId=${encodeURIComponent(deviceOf[id])}&pageSize=100`, { token: accessToken })
    assert.equal(raw.items?.some(item => item.parsed), true, `${id} 解析记录`)
    const alarms = await api(`/api/v1/alarms?deviceId=${encodeURIComponent(deviceOf[id])}&pageSize=100`, { token: accessToken })
    assert.equal(alarms.items?.some(item => item.status === 'RECOVERED'), true, `${id} 告警恢复记录`)
    checks.push({ id, version, release: 'PUBLISHED', productBound: true, profile: 'DISABLED', parsedRaw: true, recoveredAlarm: true })
    console.log(`最终核对通过 ${id}@${version}`)
  }
  report.finalCheck = { status: 'passed', at: new Date().toISOString(), checks }
  await writeFile(reportPath, JSON.stringify(report, null, 2))
}
