// 使用隔离的本机预览，检查登录页图片完整显示及设备管理的默认入口。
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const browser = process.env.IOT_TEST_BROWSER || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'
const origin = process.env.IOT_UI_PREVIEW_ORIGIN || 'http://127.0.0.1:4173'
const profile = await mkdtemp(join(tmpdir(), 'iot-login-device-'))
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
  await call('Page.enable')
  for (const [width, height] of [[1440, 900], [900, 768], [390, 844]]) {
    await call('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: 1, mobile: width < 600 })
    await call('Page.navigate', { url: origin })
    await until(() => evaluate("document.querySelector('.login-form img')?.complete"), '登录页图片')
    const visual = await evaluate(`(() => {
      const contained = selector => {
        const image = document.querySelector(selector), box = image?.parentElement
        if (!image?.naturalWidth || !box) return false
        const a = image.getBoundingClientRect(), b = box.getBoundingClientRect()
        return a.left >= b.left - 1 && a.right <= b.right + 1 && a.top >= b.top - 1 && a.bottom <= b.bottom + 1
      }
      return { logo: contained('.login-mark img'), hero: ${width < 600 ? 'true' : "contained('.login-brand img')"}, overflow: document.documentElement.scrollWidth > innerWidth + 2 }
    })()`)
    assert.deepEqual(visual, { logo: true, hero: true, overflow: false }, `${width}px 登录页图片裁切或横向溢出`)
    const shot = await call('Page.captureScreenshot', { format: 'png' })
    await writeFile(join(tmpdir(), `iot-login-${width}.png`), Buffer.from(shot.data, 'base64'))
    console.log(`${width}px 登录页图片完整显示`)
  }

  await call('Page.addScriptToEvaluateOnNewDocument', { source: `
    localStorage.clear();
    const originalFetch = window.fetch.bind(window);
    window.fetch = (input, options) => {
      const path = String(input);
      const body = path === '/api/v1/auth/login'
        ? { accessToken: 'fixture-token', tenantId: 'fixture', role: 'admin', permissions: ['*'] }
        : path === '/api/v1/auth/me'
          ? { tenantId: 'fixture', role: 'admin', permissions: ['*'] }
          : path === '/api/v1/events'
            ? { permissions: ['*'], alarms: [], devices: [] }
          : null;
      return body ? Promise.resolve(new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } })) : originalFetch(input, options);
    };
  ` })
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 900, deviceScaleFactor: 1, mobile: false })
  await call('Page.navigate', { url: origin })
  await until(() => evaluate("Boolean(document.querySelector('.login-form input[type=password]'))"), '登录表单')
  await evaluate("(() => { const input = document.querySelector('.login-form input[type=password]'); input.value = 'fixture'; input.dispatchEvent(new Event('input', { bubbles: true })); document.querySelector('.login-form button[type=submit]').click() })()")
  await until(() => evaluate("document.querySelector('.menu-item[aria-label=\"设备管理\"]')?.getClientRects().length"), '设备管理菜单')
  await evaluate("localStorage.setItem('iot:device-onboarding:fixture:admin', JSON.stringify({ step: 0, scenario: 'new', newName: '已保存的接入草稿' }))")
  await evaluate("document.querySelector('.menu-item[aria-label=\"设备管理\"]').click()")
  await until(() => evaluate("document.querySelector('.page-context h1')?.textContent === '设备管理'"), '设备管理页面')
  await until(() => evaluate("Boolean(document.querySelector('.n-data-table, .onboarding-workspace'))"), '设备页面内容')
  assert.equal(await evaluate("Boolean(document.querySelector('.onboarding-workspace'))"), false, '有接入草稿时，设备管理仍应先显示设备列表')
  assert.equal(await evaluate("Boolean(document.querySelector('.n-data-table'))"), true, '设备列表未显示')
  await evaluate("[...document.querySelectorAll('.main-content button')].find(button => button.textContent.trim() === '接入设备').click()")
  await until(() => evaluate("Boolean(document.querySelector('.onboarding-workspace'))"), '主动打开接入向导')
  assert.equal(await evaluate("document.querySelector('.onboarding-workspace input[placeholder=\"例如 厂商及型号\"]')?.value"), '已保存的接入草稿', '主动打开向导后应恢复原有草稿')
  console.log('设备管理默认打开列表，主动点击后恢复接入草稿')
} finally {
  socket?.close()
  child.kill()
  await rm(profile, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 })
}
