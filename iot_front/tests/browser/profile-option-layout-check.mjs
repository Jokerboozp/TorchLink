// 本机合成数据浏览器检查：连接配置表单分区与单选项边界。
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const browser = process.env.IOT_TEST_BROWSER || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'
const origin = process.env.IOT_UI_PREVIEW_ORIGIN || 'http://127.0.0.1:4173'
const profile = await mkdtemp(join(tmpdir(), 'iot-profile-layout-'))
const child = spawn(browser, ['--headless=new', '--no-first-run', '--no-default-browser-check', '--disable-gpu', '--remote-debugging-port=0', `--user-data-dir=${profile}`, 'about:blank'], { windowsHide: true, stdio: 'ignore' })
const delay = ms => new Promise(resolve => setTimeout(resolve, ms))
async function until(check, label) {
  for (let index = 0; index < 100; index++) {
    const value = await check()
    if (value) return value
    await delay(100)
  }
  throw new Error(`等待超时：${label}`)
}

let socket
try {
  const port = await until(async () => {
    try { return (await readFile(join(profile, 'DevToolsActivePort'), 'utf8')).split('\n')[0] } catch { return null }
  }, '浏览器调试端口')
  const targets = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()
  socket = new WebSocket(targets.find(target => target.type === 'page').webSocketDebuggerUrl)
  await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject })
  let id = 0
  const pending = new Map()
  socket.onmessage = event => {
    const message = JSON.parse(event.data)
    if (!message.id) return
    const entry = pending.get(message.id)
    pending.delete(message.id)
    message.error ? entry.reject(new Error(message.error.message)) : entry.resolve(message.result)
  }
  const call = (method, params = {}) => new Promise((resolve, reject) => {
    const next = ++id
    pending.set(next, { resolve, reject })
    socket.send(JSON.stringify({ id: next, method, params }))
  })
  const evaluate = async expression => {
    const result = await call('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true })
    if (result.exceptionDetails) throw new Error(result.exceptionDetails.exception?.description || result.exceptionDetails.text)
    return result.result.value
  }
  const clickMenu = async label => {
    await until(() => evaluate(`document.querySelector('.nav-item[aria-label=${JSON.stringify(label)}]')?.getClientRects().length`), `${label}菜单`)
    await evaluate(`document.querySelector('.nav-item[aria-label=${JSON.stringify(label)}]').click()`)
    await until(() => evaluate(`document.querySelector('.page-header h1')?.textContent.trim() === ${JSON.stringify(label)}`), `${label}页面`)
  }
  const clickButton = async label => {
    await until(() => evaluate(`[...document.querySelectorAll('.app-content button')].some(button => button.getClientRects().length && button.textContent.trim() === ${JSON.stringify(label)})`), `${label}按钮`)
    await evaluate(`[...document.querySelectorAll('.app-content button')].find(button => button.getClientRects().length && button.textContent.trim() === ${JSON.stringify(label)}).click()`)
    await until(() => evaluate("Boolean([...document.querySelectorAll('.n-modal')].find(item => item.getClientRects().length && getComputedStyle(item).visibility !== 'hidden'))"), `${label}弹窗`)
    await delay(400)
  }
  await call('Page.enable')
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false })
  await call('Page.addScriptToEvaluateOnNewDocument', { source: `
    localStorage.clear();
    const originalFetch = window.fetch.bind(window);
    window.fetch = (input, options) => {
      const path = String(input);
      const list = items => ({ items, total: items.length });
      const product = { id: 'product-demo', name: '烟雾探测器', category: 'smoke', status: 'ENABLED' };
      const device = { id: 'device-demo', name: '一层走廊烟感', productId: product.id, deviceRole: 'DIRECT', status: 'ENABLED' };
      const connection = { id: 'gateway-demo', productId: product.id, protocolId: 'protocol-demo', protocolVersion: '1.0.0', mode: 'listener', network: 'tcp', connectionMode: 'listen', host: '0.0.0.0', publicHost: '', port: 26875, timeoutMs: 5000, enabled: true };
      const body = path === '/api/v1/auth/login' ? { accessToken: 'fixture', tenantId: 'fixture', role: 'admin', permissions: ['*'] }
        : path === '/api/v1/auth/me' ? { tenantId: 'fixture', role: 'admin', permissions: ['*'] }
        : path === '/api/v1/events' ? { permissions: ['*'], alarms: [], devices: [] }
        : path.startsWith('/api/v1/products') ? list([product])
        : path.startsWith('/api/v1/device-registry?') ? list([{ device, runtimeState: {} }])
        : path.startsWith('/api/v1/devices?') ? list([])
        : path === '/api/v2/protocols' ? list([{ definition: { id: 'protocol-demo', name: '演示消防协议' }, releases: [{ version: '1.0.0', status: 'PUBLISHED' }] }])
        : path === '/api/v2/device-access-profiles' ? list([connection])
        : path === '/api/v1/connectors' ? list([{ profile: connection }])
        : null;
      return body ? Promise.resolve(new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } })) : originalFetch(input, options);
    };
  ` })
  await call('Page.navigate', { url: origin })
  await until(() => evaluate("Boolean(document.querySelector('.login-form input[type=password]'))"), '登录表单')
  await evaluate("(() => { const input = document.querySelector('.login-form input[type=password]'); input.value = 'fixture'; input.dispatchEvent(new Event('input', { bubbles: true })); document.querySelector('.login-form button[type=submit]').click() })()")

  await clickMenu('设备管理')
  await clickButton('编辑')
  const choices = await evaluate(`(() => {
    const items = [...document.querySelectorAll('.n-modal .n-radio-button')].filter(item => item.getClientRects().length)
    const rects = items.map(item => item.getBoundingClientRect())
    return { count: items.length, className: items[0]?.parentElement?.className || '', gap: rects[1] ? rects[1].left - rects[0].right : 0, cssGap: items[0] ? getComputedStyle(items[0].parentElement).gap : '', margin: items[1] ? getComputedStyle(items[1]).marginLeft : '', border: items[0] ? getComputedStyle(items[0]).borderLeftWidth : '' }
  })()`)
  const deviceShot = await call('Page.captureScreenshot', { format: 'png' })
  await writeFile(join(tmpdir(), 'iot-device-options.png'), Buffer.from(deviceShot.data, 'base64'))
  await evaluate("[...document.querySelectorAll('.n-modal .n-radio-button')].find(item => item.textContent.trim() === '主设备').click()")
  assert.ok(await evaluate("[...document.querySelectorAll('.n-modal .n-radio-button')].some(item => item.textContent.trim() === '主设备' && item.classList.contains('n-radio-button--checked'))"), '接入关系切换未生效')
  await evaluate("document.querySelector('.n-modal .n-base-close').click()")

  // 接入点在设备模板详情中管理。
  await clickMenu('设备模板')
  await until(() => evaluate("Boolean(document.querySelector('.product-name'))"), '模板列表')
  await evaluate("document.querySelector('.product-name').click()")
  await until(() => evaluate("Boolean([...document.querySelectorAll('.product-detail .n-tabs-tab')].find(tab => tab.textContent.includes('接入点')))"), '模板详情')
  await evaluate("[...document.querySelectorAll('.product-detail .n-tabs-tab')].find(tab => tab.textContent.includes('接入点')).click()")
  await until(() => evaluate("Boolean([...document.querySelectorAll('.product-detail .row-actions button')].find(button => button.textContent.trim() === '编辑'))"), '接入点编辑')
  await evaluate("[...document.querySelectorAll('.product-detail .row-actions button')].find(button => button.textContent.trim() === '编辑').click()")
  await until(() => evaluate("Boolean([...document.querySelectorAll('.n-modal')].find(item => item.getClientRects().length && item.textContent.includes('保存接入点')))"), '接入点弹窗')
  await delay(400)
  const layout = await evaluate(`(() => {
    const modal = [...document.querySelectorAll('.n-modal')].find(item => item.getClientRects().length)
    return { sections: modal?.querySelectorAll('.profile-editor-section').length || 0, summary: Boolean(modal?.querySelector('.profile-protocol-summary')), footer: Boolean(modal?.querySelector('.n-card__footer')), horizontalOverflow: modal?.scrollWidth > modal?.clientWidth + 2 }
  })()`)
  const profileShot = await call('Page.captureScreenshot', { format: 'png' })
  await writeFile(join(tmpdir(), 'iot-profile-editor.png'), Buffer.from(profileShot.data, 'base64'))
  console.log(JSON.stringify({ choices, layout }))
  assert.ok(choices.count === 3 && choices.className.includes('segmented-choice-group') && choices.gap >= 6 && Number.parseFloat(choices.border) >= 1, '接入关系选项应有独立边框和间距')
  assert.ok(layout.sections >= 4 && layout.summary && layout.footer && !layout.horizontalOverflow, '接入点表单应按流程分区，协议摘要与保存操作应清晰可见')
  await evaluate("[...document.querySelectorAll('.n-modal .n-collapse-item__header-main')].find(item => item.textContent.includes('定时读取与子设备')).click()")
  await until(() => evaluate("Boolean(document.querySelector('.protocol-access-settings')?.getClientRects().length)"), '定时读取与子设备设置')
  await delay(200)
  const advanced = await evaluate(`(() => {
    const root = document.querySelector('.protocol-access-settings')
    return { sections: root?.querySelectorAll('.access-option-section').length || 0, emptyStates: root?.querySelectorAll('.access-empty').length || 0, horizontalOverflow: root?.scrollWidth > root?.clientWidth + 2 }
  })()`)
  await evaluate("(() => { const body = document.querySelector('.n-modal .n-card-content'); body.scrollTop = body.scrollHeight })()")
  await delay(120)
  const advancedShot = await call('Page.captureScreenshot', { format: 'png' })
  await writeFile(join(tmpdir(), 'iot-profile-advanced.png'), Buffer.from(advancedShot.data, 'base64'))
  console.log(JSON.stringify({ advanced }))
  assert.deepEqual(advanced, { sections: 2, emptyStates: 2, horizontalOverflow: false }, '展开区应明确区分定时读取和子设备映射，并说明空状态')
  await evaluate("[...document.querySelectorAll('.protocol-access-settings button')].find(button => button.textContent.trim() === '添加定时读取').click()")
  await evaluate("[...document.querySelectorAll('.protocol-access-settings button')].find(button => button.textContent.trim() === '添加子设备产品').click()")
  await until(() => evaluate("document.querySelectorAll('.protocol-access-settings .access-config-card').length === 2"), '新增读取与子设备映射')
  assert.ok(await evaluate("[...document.querySelectorAll('.access-config-card')].every(card => card.querySelectorAll('.access-field').length === 2 && card.querySelectorAll('input').length >= 2)"), '新增后应显示独立标注的两个输入字段')
  await call('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true })
  await delay(200)
  const mobile = await evaluate(`(() => {
    const modal = [...document.querySelectorAll('.n-modal')].find(item => item.getClientRects().length)
    const body = modal?.querySelector('.n-card-content')
    const footer = modal?.querySelector('.n-card__footer')
    const rect = modal?.getBoundingClientRect(), foot = footer?.getBoundingClientRect()
    if (body) body.scrollTop = body.scrollHeight
    return { withinViewport: rect.left >= -1 && rect.right <= innerWidth + 1 && rect.bottom <= innerHeight + 1, footerVisible: foot && foot.top >= 0 && foot.bottom <= innerHeight + 1, scrollable: body?.scrollHeight > body?.clientHeight && body.scrollTop > 0, horizontalOverflow: modal?.scrollWidth > modal?.clientWidth + 2 }
  })()`)
  const mobileShot = await call('Page.captureScreenshot', { format: 'png' })
  await writeFile(join(tmpdir(), 'iot-profile-editor-mobile.png'), Buffer.from(mobileShot.data, 'base64'))
  assert.deepEqual(mobile, { withinViewport: true, footerVisible: true, scrollable: true, horizontalOverflow: false }, '手机端连接配置无法完整滚动或保存')
  console.log('手机端连接配置可滚动，页脚可见')
} finally {
  socket?.close()
  child.kill()
  await rm(profile, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 })
}
