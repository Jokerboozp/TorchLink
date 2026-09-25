// 真实 Chromium + 已构建前端 + 隔离的 Go API：新建模板、添加设备、设备上报、连接详情、接入点会话、摄像头与模型来源。
import { spawn } from 'node:child_process'
import { mkdtemp, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import assert from 'node:assert/strict'

const origin = process.env.IOT_TEST_ORIGIN
const profile = await mkdtemp(join(tmpdir(), 'iot-onboard-browser-'))
const child = spawn(process.env.IOT_TEST_BROWSER, ['--headless=new', '--use-mock-keychain', '--password-store=basic', '--no-first-run', '--no-default-browser-check', '--disable-gpu', '--remote-debugging-port=0', `--user-data-dir=${profile}`, 'about:blank'], { windowsHide: true, stdio: 'ignore' })
let browserGone = ''
child.once('exit', (code, signal) => { browserGone = `浏览器进程已退出（code=${code}, signal=${signal}）` })
let socket
const delay = ms => new Promise(resolve => setTimeout(resolve, ms))
async function until(check, note = '') {
  for (let i = 0; i < 100; i++) {
    if (browserGone) throw new Error(`${browserGone}：${note}`)
    const value = await check()
    if (value) return value
    await delay(100)
  }
  throw new Error(`页面等待超时：${note}`)
}

try {
  const port = await until(async () => { try { return (await readFile(join(profile, 'DevToolsActivePort'), 'utf8')).split('\n')[0] } catch { return null } }, 'DevToolsActivePort')
  const pages = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()
  socket = new WebSocket(pages.find(page => page.type === 'page').webSocketDebuggerUrl)
  await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject })
  let id = 0
  const pending = new Map()
  const failures = []
  socket.onmessage = event => {
    const message = JSON.parse(event.data)
    if (message.id) { const entry = pending.get(message.id); pending.delete(message.id); message.error ? entry.reject(new Error(message.error.message)) : entry.resolve(message.result) }
    else if (message.method === 'Runtime.exceptionThrown') failures.push(message.params.exceptionDetails.exception?.description || message.params.exceptionDetails.text)
    else if (message.method === 'Inspector.detached') browserGone = `页面进程已断开：${message.params.reason}`
  }
  socket.onclose = () => { browserGone ||= '浏览器调试连接已关闭' }
  const call = (method, params = {}) => new Promise((resolve, reject) => {
    if (browserGone) return reject(new Error(browserGone))
    const next = ++id
    const timer = setTimeout(() => { pending.delete(next); reject(new Error(browserGone || `CDP ${method} 超过 30 秒未响应`)) }, 30000)
    pending.set(next, { resolve: value => { clearTimeout(timer); resolve(value) }, reject: error => { clearTimeout(timer); reject(error) } })
    socket.send(JSON.stringify({ id: next, method, params }))
  })
  const evaluate = async expression => { const value = await call('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true }); if (value.exceptionDetails) throw new Error(value.exceptionDetails.exception?.description || value.exceptionDetails.text); return value.result.value }
  const button = (text, scope = 'body') => evaluate(`(() => {const item=[...document.querySelectorAll(${JSON.stringify(`${scope} button`)})].find(b=>b.innerText.trim()===${JSON.stringify(text)}&&!b.disabled&&b.getClientRects().length);if(!item)return false;item.click();return true})()`)
  const setInput = (label, value, scope = 'body') => evaluate(`(() => {const input=document.querySelector(${JSON.stringify(`${scope} input[aria-label="${label}"]`)})||[...document.querySelectorAll(${JSON.stringify(`${scope} .n-form-item`)})].find(item=>item.querySelector('.n-form-item-label')?.innerText.replace('*','').trim()===${JSON.stringify(label)})?.querySelector('input');if(!input)return false;input.value=${JSON.stringify(value)};input.dispatchEvent(new Event('input',{bubbles:true}));input.dispatchEvent(new Event('change',{bubbles:true}));return true})()`)
  // 下拉选项：选项未出现时重新点开选择框，避免抽屉或表单刷新时首次点击落空。
  const pick = (selection, option, note) => until(() => evaluate(`(() => {const item=[...document.querySelectorAll('.n-base-select-option')].find(o=>o.getClientRects().length&&o.innerText.trim().startsWith(${JSON.stringify(option)}));if(item){item.click();return true}const box=${selection};if(box&&!document.querySelector('.n-base-select-menu'))box.click();return false})()`), note || option)
  const choose = (scope, label, option) => pick(`[...document.querySelectorAll(${JSON.stringify(`${scope} .n-form-item`)})].find(item=>item.innerText.includes(${JSON.stringify(label)}))?.querySelector('.n-base-selection')`, option, `${label}：${option}`)
  const openPage = async name => {
    await until(() => evaluate(`Boolean(document.querySelector('.nav-item[aria-label="${name}"]'))`), name)
    await evaluate(`document.querySelector('.nav-item[aria-label="${name}"]').click()`)
  }
  const drawer = "[...document.querySelectorAll('.n-drawer')].find(item=>item.getClientRects().length)"
  const closeDrawer = async () => {
    await evaluate(`${drawer}?.querySelector('.n-base-close')?.click()`)
    await until(() => evaluate(`!${drawer}`), '关闭抽屉')
  }
  const api = async (path, init = {}) => {
    const response = await fetch(origin + path, { ...init, headers: { Authorization: `Bearer ${process.env.IOT_TEST_TOKEN}`, 'Content-Type': 'application/json', ...init.headers } })
    return { status: response.status, body: await response.json().catch(() => ({})) }
  }

  await call('Page.enable')
  await call('Runtime.enable')
  await call('Inspector.enable').catch(() => {})
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false })
  await call('Page.addScriptToEvaluateOnNewDocument', { source: `localStorage.setItem('iot_token',${JSON.stringify(process.env.IOT_TEST_TOKEN)});localStorage.setItem('iot_tenant','tenant');localStorage.setItem('iot_role','admin');localStorage.setItem('iot_user','browser-test');` })
  await call('Page.navigate', { url: origin })
  await until(() => evaluate("document.querySelectorAll('.nav-item').length >= 10"), '主导航')

  // Modbus 协议版本由 Go 测试预先发布，这里通过统一的添加设备接口登记采集设备。
  const modbus = await api('/api/v1/onboarding', { method: 'POST', body: JSON.stringify({ requestId: 'browser-modbus', productId: 'browser-modbus-product', device: { id: 'browser-modbus-preview', name: 'Modbus 预览' }, connection: { mode: 'poll', host: '127.0.0.1', port: Number(process.env.IOT_TEST_MODBUS_PORT), unitId: 1, timeoutMs: 3000 } }) })
  assert.equal(modbus.status, 201, JSON.stringify(modbus.body))

  // 设备模板：通过页面新建使用标准协议的模板。
  await openPage('设备模板')
  await until(() => button('新建设备模板', '.filter-bar'), '新建设备模板')
  await until(() => setInput('模板名称', '浏览器标准产品', '.n-modal'), '模板名称')
  await setInput('模板标识', 'browser-standard-product', '.n-modal')
  await until(() => button('保存设备模板', '.n-modal'), '保存设备模板')
  await until(() => evaluate("!document.querySelector('.n-modal')"), '模板弹窗关闭')
  await until(() => evaluate("document.querySelector('.app-content')?.innerText.includes('浏览器标准产品')"), '模板出现在列表')

  // 添加设备向导：选择刚建的模板，HTTP 标准上报，获得一次性密钥。
  await openPage('设备管理')
  await until(() => button('添加设备', '.filter-bar'), '添加设备')
  await choose('.onboarding', '设备模板', '浏览器标准产品')
  await until(() => evaluate("document.querySelector('.onboarding__preflight')?.innerText.includes('标准 MQTT / HTTP 上报')"), '标准预检')
  await until(() => button('下一步', '.onboarding'), '下一步')
  await until(() => setInput('设备名称', '浏览器传感器', '.onboarding'), '设备名称')
  await setInput('设备编号', 'browser-device', '.onboarding')
  await evaluate("[...document.querySelectorAll('.onboarding .n-radio-button')].find(b=>b.innerText.trim()==='HTTP').click()")
  await until(() => button('保存并生成接入信息', '.onboarding'), '保存设备')
  await until(() => evaluate("Boolean(document.querySelector('.onboarding__secret code'))"), '一次性密钥')
  const [accessKey, secret] = await evaluate("[...document.querySelectorAll('.onboarding__secret code')].map(e=>e.innerText.trim())")
  assert.ok(accessKey && secret, '向导未显示 AccessKey 与 Secret')
  assert.ok(await evaluate(`!Object.values(localStorage).some(value=>String(value).includes(${JSON.stringify(secret)}))`), '设备密钥写入了浏览器存储')

  // 设备侧 HTTP 上报：错误密钥被拒绝，正确密钥解析成功，同一消息重发被去重。
  const report = JSON.stringify({ version: '1.0', id: 'browser-http-1', timestamp: Date.now(), data: { temperature: 26.5 } })
  const ingest = (key, value) => fetch(`${origin}/api/v1/device-ingest/standard/tenant/browser-standard-product/browser-device/property`, { method: 'POST', headers: { 'X-Device-Key': key, 'X-Device-Secret': value, 'Content-Type': 'application/json' }, body: report }).then(async r => ({ status: r.status, body: await r.json() }))
  const rejected = await ingest(accessKey, 'invalid-secret')
  assert.deepEqual([rejected.status, rejected.body.errorCode], [401, 'AUTH_FAILED'])
  const accepted = await ingest(accessKey, secret)
  assert.deepEqual([accepted.status, accepted.body.created], [202, true], JSON.stringify(accepted.body))
  const duplicate = await ingest(accessKey, secret)
  assert.deepEqual([duplicate.status, duplicate.body.created], [202, false], JSON.stringify(duplicate.body))
  if (process.env.IOT_TEST_MQTT_WEBSOCKET) {
    // 设备侧 MQTT：用设备凭据换取短期令牌，通过真实 Broker 的 WebSocket 上报。
    const { default: mqtt } = await import('mqtt')
    const grant = await fetch(`${origin}/api/v1/device-mqtt/token`, { method: 'POST', headers: { 'X-Device-Key': accessKey, 'X-Device-Secret': secret } }).then(r => r.json())
    const client = await mqtt.connectAsync(process.env.IOT_TEST_MQTT_WEBSOCKET, { username: grant.username, password: grant.token, clientId: `device-${accessKey}`, clean: true, reconnectPeriod: 0, connectTimeout: 10000 })
    await client.publishAsync(grant.publishTopic, JSON.stringify({ version: '1.0', id: 'browser-mqtt-1', timestamp: Date.now(), data: { temperature: 27 } }), { qos: 1 })
    await client.endAsync()
    await until(async () => (await api('/api/v1/device-registry/browser-device/history?kind=property')).body.total >= 2, 'MQTT 上报入库')
    console.log('PASS: device MQTT token exchange and property report through the live Broker WebSocket')
  }

  // 向导第三步刷新后确认已收到并解析上报。
  await until(() => button('我已保存', '.onboarding'), '我已保存')
  await until(() => button('立即刷新', '.onboarding'), '立即刷新')
  const latest = process.env.IOT_TEST_MQTT_WEBSOCKET ? '27' : '26.5'
  await until(() => evaluate(`(document.querySelector('.onboarding')?.innerText||'').includes(${JSON.stringify(latest)})`), '向导显示最新上报值')
  await until(() => button('完成', '.onboarding'), '完成')

  // 连接详情：历史分区与窄屏抽屉；密钥不再显示。
  const openDetail = name => until(() => evaluate(`(() => {const row=[...document.querySelectorAll('.n-data-table-tbody .n-data-table-tr')].find(e=>e.innerText.includes(${JSON.stringify(name)}));const item=[...(row?.querySelectorAll('.row-actions button')||[])].find(b=>b.innerText.trim()==='详情');if(!item)return false;item.click();return true})()`), `${name} 详情`)
  await openDetail('浏览器传感器')
  await until(() => evaluate(`${drawer}?.innerText.includes('连接与状态历史')`), '连接详情')
  const detailText = await evaluate(`${drawer}.innerText`)
  for (const section of ['最近事件', '最近告警', 'iot-standard']) assert.ok(detailText.includes(section), `连接详情缺少“${section}”`)
  assert.ok(!detailText.includes(secret), '连接详情显示了设备密钥')
  assert.ok(!/设备影子|设备孪生与拓扑/.test(detailText), '连接详情仍显示已移除的影子或拓扑')
  await call('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true })
  await until(() => evaluate(`${drawer}?.getBoundingClientRect().width<=391`), '窄屏抽屉宽度')
  await closeDrawer()
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false })

  // 历史设备关联多个接入点：选择后只展示该接入点的会话。
  await openDetail('历史设备')
  await until(() => evaluate(`${drawer}?.innerText.includes('设备关联了多个接入点')`), '多个接入点提示')
  await pick(`${drawer}?.querySelector('.profile-picker .n-base-selection')`, 'listener-a', '选择接入点 listener-a')
  await until(() => evaluate(`(${drawer}?.querySelector('.ui-descriptions')?.innerText||'').includes('TCP')`), '接入点信息')
  await until(() => evaluate(`(() => {const text=[...${drawer}.querySelectorAll('.ui-table')].map(t=>t.innerText).join(' ');return text.includes('listener-a')&&!text.includes('listener-b')})()`), '只显示所选接入点的会话')
  await closeDrawer()

  // 设备模板详情的接入点：展开后显示在线会话。
  await openPage('设备模板')
  await until(() => evaluate("(() => {const row=[...document.querySelectorAll('.n-data-table-tbody .n-data-table-tr')].find(e=>e.innerText.includes('历史产品'));const item=[...(row?.querySelectorAll('button')||[])].find(b=>b.innerText.trim()==='接入点');if(!item)return false;item.click();return true})()"), '模板接入点')
  await until(() => evaluate(`(() => {const row=[...(${drawer}?.querySelectorAll('.n-data-table-tbody .n-data-table-tr')||[])].find(e=>e.innerText.includes('listener-a'));const expand=row?.querySelector('.n-data-table-expand-trigger');if(!expand)return false;expand.click();return true})()`), '展开 listener-a')
  await until(() => evaluate(`(${drawer}?.innerText||'').includes('127.0.0.1:1234')`), '接入点在线会话')
  assert.ok(await evaluate(`${drawer}.innerText.includes('legacy')`), '接入点会话未显示设备')
  await closeDrawer()

  // 摄像头只登记元数据，不出现视频协议入口。
  await openPage('摄像头映射')
  await until(() => button('新增摄像头', '.filter-bar'), '新增摄像头')
  await until(() => setInput('摄像头标识', 'browser-camera', '.n-modal'), '摄像头标识')
  assert.equal(await evaluate("/国标视频目录|ONVIF/.test(document.querySelector('.n-modal').innerText)"), false)
  await setInput('摄像头名称', '直接登记摄像头', '.n-modal')
  await until(() => button('保存', '.n-modal'), '保存摄像头')
  await until(() => evaluate("document.querySelector('.app-content')?.innerText.includes('直接登记摄像头')"), '摄像头出现在列表')

  // 模型管理保留具体技术名称，只查看选项，不测试或应用配置。
  await openPage('模型管理')
  await until(() => evaluate("Boolean(document.querySelector('.provider-select .n-base-selection'))"), '模型来源')
  await evaluate("document.querySelector('.provider-select .n-base-selection').click()")
  await until(() => evaluate("[...document.querySelectorAll('.n-base-select-option')].filter(e=>e.getClientRects().length).length>=3"), '模型来源选项')
  assert.deepEqual(await evaluate("[...document.querySelectorAll('.n-base-select-option')].filter(e=>e.getClientRects().length).map(e=>e.innerText.trim())"), ['Ollama', 'DeepSeek', 'OpenAI 兼容 API'])

  assert.deepEqual(failures, [], `页面脚本异常：${failures.join(' | ')}`)
  console.log('PASS: template and device created in the UI, device HTTP credential rejection, parsing and deduplication, wizard verification, connection drawer and narrow layout, access point selection and sessions, camera metadata, provider names')
} finally {
  socket?.close()
  const exited = new Promise(resolve => { if (child.exitCode !== null || child.signalCode !== null) resolve(); else child.once('exit', resolve) })
  child.kill()
  const forceStop = setTimeout(() => child.kill('SIGKILL'), 3000)
  forceStop.unref()
  await exited
  clearTimeout(forceStop)
  await rm(profile, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 })
}
