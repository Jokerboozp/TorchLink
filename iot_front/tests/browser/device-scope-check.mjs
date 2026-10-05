// Browser acceptance against a running test platform; creates temporary users and roles.
import { startBrowser, delay } from '../helpers/browser.mjs'
import { readFile, writeFile, mkdir } from 'node:fs/promises'
import { parseEnv } from 'node:util'
const env = { ...parseEnv(await readFile('.env.local', 'utf8')), ...process.env }
const dir = '.e2e/demo-20260914',
  tenant = (env.IOT_ADMIN_TENANTS || 'tenant_001').split(',')[0].trim(),
  user = env.IOT_ADMIN_USER || 'admin'
const auth = await (
  await fetch('http://127.0.0.1:5173/api/v1/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: user, password: env.IOT_ADMIN_PASSWORD, tenantId: tenant })
  })
).json()
if (!auth.accessToken) throw Error('本机登录失败')
await mkdir(`${dir}/screenshots`, { recursive: true })
let scopeUser = ''
const results = [],
  errors = []
let browser
try {
  browser = await startBrowser({
    timeout: 30000,
    interval: 200,
    onEvent: message => {
      if (message.method === 'Runtime.exceptionThrown') errors.push({ page: results.length, error: message.params.exceptionDetails.text })
      if (message.method === 'Network.responseReceived' && message.params.response.status >= 400) {
        const response = message.params.response
        errors.push({ page: results.length, path: new URL(response.url).pathname, status: response.status })
      }
    }
  })
  const { call, evaluate, until } = browser
  await call('Page.enable')
  await call('Runtime.enable')
  await call('Network.enable')
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1050, deviceScaleFactor: 1, mobile: false })
  const click = async text =>
    until(() =>
      evaluate(
        `(()=>{const b=[...document.querySelectorAll('button')].find(b=>b.textContent.trim()===${JSON.stringify(text)}&&b.getClientRects().length&&!b.disabled);b?.click();return !!b})()`
      )
    )
  const fill = async (label, value) =>
    evaluate(
      `(()=>{const root=[...document.querySelectorAll('.ui-dialog')].find(e=>e.getClientRects().length);const item=[...root.querySelectorAll('.ui-form-item')].find(e=>e.querySelector('label')?.textContent.trim().startsWith(${JSON.stringify(label)}));const e=item?.querySelector('input');if(!e)throw Error('缺少表单输入框');e.value=${JSON.stringify(value)};e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}))})()`
    )
  const request = async (method, path, body, token = auth.accessToken) => {
    const r = await fetch('http://127.0.0.1:5173' + path, {
      method,
      headers: { Authorization: 'Bearer ' + token, 'Content-Type': 'application/json' },
      body: body ? JSON.stringify(body) : undefined
    })
    return { status: r.status, value: await r.json() }
  }
  const suffix = Date.now(),
    username = 'demo-scope-' + suffix,
    secret = crypto.randomUUID() + 'Aa1'
  scopeUser = username
  const registry = (await request('GET', '/api/v1/device-registry?pageSize=100')).value.items
  const alarms = (await request('GET', '/api/v1/alarms?pageSize=100')).value.items
  const allowedAlarm = alarms.find(a => registry.some(d => d.device.id === a.deviceId && d.device.deviceRole === 'DIRECT'))
  if (!allowedAlarm) throw Error('缺少带告警的演示独立设备')
  const allowed = registry.find(d => d.device.id === allowedAlarm.deviceId).device
  const forbidden = registry.find(d => d.device.id !== allowed.id).device
  await call('Page.addScriptToEvaluateOnNewDocument', {
    source:
      'for(const [k,v] of Object.entries(' +
      JSON.stringify({ iot_token: auth.accessToken, iot_tenant: tenant, iot_role: 'admin', iot_user: user, iot_permissions: '["*"]' }) +
      '))localStorage.setItem(k,v)'
  })
  await call('Page.navigate', { url: 'http://127.0.0.1:5173' })
  await until(() => evaluate(`!!document.querySelector('.nav-item[aria-label="用户与权限"]')`))
  await evaluate(`document.querySelector('.nav-item[aria-label="用户与权限"]').click()`)
  await delay(600)
  await click('添加用户')
  await fill('用户名', username)
  await fill('显示名称', '演示 · 指定设备用户')
  await fill('初始密码', secret)
  if (!(await evaluate(`document.querySelector('.ui-dialog input[disabled]')?.value===${JSON.stringify(tenant)}`)))
    throw Error('新增用户未显示租户')
  await evaluate(`document.querySelector('input[value="selected"]').click()`)
  await evaluate(
    `(()=>{const row=[...document.querySelectorAll('.ui-form-item')].find(e=>e.querySelector('label')?.textContent==='可访问设备');row.querySelector('.n-base-selection').click()})()`
  )
  await until(() =>
    evaluate(
      `(()=>{const e=[...document.querySelectorAll('.n-base-select-option')].find(e=>e.textContent.trim().endsWith(${JSON.stringify('（' + allowed.id + '）')})&&e.getClientRects().length);e?.click();return !!e})()`
    )
  )
  await evaluate(`document.querySelector('.n-card-header').click()`)
  for (const name of ['设备管理', '告警中心', '运行总览'])
    await evaluate(
      `(()=>{const g=[...document.querySelectorAll('.permission-group')].find(e=>e.querySelector('strong')?.textContent===${JSON.stringify(name)});g.querySelector('input').click()})()`
    )
  await writeFile(
    dir + '/screenshots/device-scope-form.png',
    Buffer.from((await call('Page.captureScreenshot', { format: 'png' })).data, 'base64')
  )
  await click('保存')
  await until(() => evaluate(`![...document.querySelectorAll('.ui-dialog')].some(e=>e.getClientRects().length)`))
  const login = await request('POST', '/api/v1/auth/login', { username, password: secret, tenantId: tenant })
  if (login.status !== 200) throw Error('范围用户登录失败')
  const token = login.value.accessToken
  for (const path of ['/api/v1/device-registry', '/api/v1/alarms']) {
    const r = await request('GET', path, null, token)
    if (r.status !== 200) throw Error('授权列表无法打开')
    if (r.value.items.some(v => (v.device?.id || v.deviceId) !== allowed.id)) throw Error('列表包含未授权设备')
  }
  if ((await request('GET', '/api/v1/device-registry/' + forbidden.id + '/connection', null, token)).status !== 403)
    throw Error('直接访问未授权设备未拒绝')
  const events = await request('GET', '/api/v1/events', null, token)
  if (
    events.status !== 200 ||
    events.value.alarms.some(a => a.deviceId !== allowed.id) ||
    events.value.devices.some(d => d.deviceId !== allowed.id)
  )
    throw Error('消息范围错误')
  if ((await request('POST', '/api/v1/mqtt/token', {}, token)).status !== 403) throw Error('受限用户仍能获取MQTT订阅令牌')
  // Replace the admin bootstrap for this isolated browser with the real user session.
  await call('Page.addScriptToEvaluateOnNewDocument', {
    source:
      'for(const [k,v] of Object.entries(' +
      JSON.stringify({
        iot_token: token,
        iot_tenant: tenant,
        iot_role: 'operator',
        iot_user: username,
        iot_permissions: JSON.stringify(login.value.permissions)
      }) +
      '))localStorage.setItem(k,v)'
  })
  await call('Page.reload')
  await until(() => evaluate(`!!document.querySelector('.nav-item[aria-label="设备管理"]')`))
  await evaluate(`document.querySelector('.nav-item[aria-label="设备管理"]').click()`)
  await delay(700)
  const text = await evaluate(`document.querySelector('.table-card').innerText`)
  if (!text.includes(allowed.id) || text.includes(forbidden.id)) throw Error('实际设备页面范围错误')
  await writeFile(
    dir + '/screenshots/device-scope-devices.png',
    Buffer.from((await call('Page.captureScreenshot', { format: 'png' })).data, 'base64')
  )
  await evaluate(`document.querySelector('.nav-item[aria-label="告警中心"]').click()`)
  await delay(700)
  await writeFile(
    dir + '/screenshots/device-scope-alarms.png',
    Buffer.from((await call('Page.captureScreenshot', { format: 'png' })).data, 'base64')
  )
  // API scope has been checked above. A browser-only event verifies popup rendering.
  await evaluate(
    `window.dispatchEvent(new CustomEvent('iot:realtime',{detail:{topic:'/iot/alarm/raised/'+${JSON.stringify(tenant)},payload:JSON.stringify(${JSON.stringify({ ...allowedAlarm, alarmId: 'scope-popup-fixture', triggerId: 'scope-popup-trigger', status: 'ACTIVE', lastTriggeredAt: Date.now() })})}}))`
  )
  await until(() => evaluate(`!!document.querySelector('.global-alert-popup')`))
  await writeFile(
    dir + '/screenshots/device-scope-popup.png',
    Buffer.from((await call('Page.captureScreenshot', { format: 'png' })).data, 'base64')
  )
  if (errors.some(e => e.error)) throw Error('浏览器运行异常')
  console.log(
    '通过：真实UI设置所属租户与单台设备授权；真实设备、告警、消息API及页面只返回授权设备；越权详情和MQTT令牌拒绝；授权告警弹窗渲染通过（弹窗使用浏览器内样本）。'
  )
} finally {
  if (scopeUser)
    await fetch('http://127.0.0.1:5173/api/v1/access/users/' + scopeUser, {
      method: 'DELETE',
      headers: { Authorization: 'Bearer ' + auth.accessToken }
    })
  await browser?.close()
}
