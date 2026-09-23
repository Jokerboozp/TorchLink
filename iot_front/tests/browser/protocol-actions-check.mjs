// 用隔离的合成 API 数据检查不同来源协议的统一版本入口。
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtemp, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

// 样本覆盖生成映射、Go 源码、历史版本和尚未创建版本的协议。
const fixtures = [
  { definition: { id: 'generated-fixture', name: '生成映射协议' }, releases: [{ version: '1.0.0', status: 'PUBLISHED', parserType: 'json', transport: 'MQTT', artifact: { generatedMapping: true } }] },
  { definition: { id: 'source-fixture', name: 'Go 源码协议' }, releases: [{ version: '1.0.0', status: 'PUBLISHED', parserType: 'go-protocol-v2', transport: 'TCP', artifact: { build: { kind: 'go-source' }, filename: 'protocol.go' } }] },
  { definition: { id: 'legacy-fixture', name: '历史协议' }, releases: [{ version: '1.0.0', status: 'PUBLISHED', parserType: 'json', transport: 'HTTP', artifact: {} }] },
  { definition: { id: 'empty-fixture', name: '待创建版本的协议' }, releases: [] }
]
const browser = process.env.IOT_TEST_BROWSER || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'
const origin = process.env.IOT_UI_PREVIEW_ORIGIN || 'http://127.0.0.1:4173'
const profile = await mkdtemp(join(tmpdir(), 'iot-protocol-actions-'))
const child = spawn(browser, ['--headless=new', '--no-first-run', '--no-default-browser-check', '--disable-gpu', '--remote-debugging-port=0', `--user-data-dir=${profile}`, 'about:blank'], { windowsHide: true, stdio: 'ignore' })
const delay = ms => new Promise(resolve => setTimeout(resolve, ms))
async function until(check) { for (let i = 0; i < 100; i++) { const value = await check(); if (value) return value; await delay(100) } throw new Error('页面等待超时') }

let socket
try {
  // CDP 只连接本次创建的临时浏览器。
  const port = await until(async () => { try { return (await readFile(join(profile, 'DevToolsActivePort'), 'utf8')).split('\n')[0] } catch { return null } })
  const pages = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()
  socket = new WebSocket(pages.find(page => page.type === 'page').webSocketDebuggerUrl)
  await new Promise((resolve, reject) => { socket.onopen = resolve; socket.onerror = reject })
  let id = 0
  const pending = new Map()
  socket.onmessage = event => { const message = JSON.parse(event.data); if (!message.id) return; const entry = pending.get(message.id); pending.delete(message.id); message.error ? entry.reject(new Error(message.error.message)) : entry.resolve(message.result) }
  const call = (method, params = {}) => new Promise((resolve, reject) => { const next = ++id; pending.set(next, { resolve, reject }); socket.send(JSON.stringify({ id: next, method, params })) })
  const evaluate = async expression => { const value = await call('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true }); if (value.exceptionDetails) throw new Error(value.exceptionDetails.exception?.description || value.exceptionDetails.text); return value.result.value }

  // 登录、权限轮询和协议目录均返回合成数据，不连接真实业务服务。
  await call('Page.enable')
  await call('Page.addScriptToEvaluateOnNewDocument', { source: `
    localStorage.clear();
    const originalFetch = window.fetch.bind(window);
    const protocolFixture = ${JSON.stringify({ items: fixtures, total: fixtures.length })};
    window.fetch = (input, options) => {
      const path = String(input);
      const body = path === '/api/v1/auth/login' ? { accessToken: 'fixture-token', tenantId: 'fixture', role: 'admin', permissions: ['*'] }
        : path === '/api/v1/auth/me' ? { tenantId: 'fixture', role: 'admin', permissions: ['*'] }
        : path === '/api/v1/events' ? { permissions: ['*'], alarms: [], devices: [] }
        : path === '/api/v2/protocols' ? protocolFixture : null;
      return body ? Promise.resolve(new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } })) : originalFetch(input, options);
    };
  ` })
  await call('Page.navigate', { url: origin })
  await until(() => evaluate("Boolean(document.querySelector('.login-form button[type=submit]'))"))
  await evaluate("(() => { const input = document.querySelector('.login-form input[type=password]'); input.value = 'fixture'; input.dispatchEvent(new Event('input', { bubbles: true })) })()")
  await evaluate("document.querySelector('.login-form button[type=submit]').click()")
  await until(() => evaluate("Boolean(document.querySelector('.menu-item[aria-label=\"协议管理\"]'))"))
  await evaluate("document.querySelector('.menu-item[aria-label=\"协议管理\"]').click()")
  await until(() => evaluate("document.querySelectorAll('.el-table__body .el-table__row').length === 4"))

  // 三类版本均能打开详情，专项操作仍按制品能力分别显示。
  const labels = await evaluate("[...document.querySelectorAll('.el-table__body .el-table__row')].map(row => row.querySelector('td:last-child')?.innerText.trim())")
  assert.deepEqual(labels.map(label => label.includes('查看版本')), [true, true, true, false], `操作列不一致：${labels.join(' / ')}`)
  assert.ok(labels.every(label => !label.includes('—')), `操作列仍有横线：${labels.join(' / ')}`)
  assert.equal(labels[3], '暂无版本')
  for (const [index, expected] of ['解析测试', '源码', '暂无可执行操作'].entries()) {
    await evaluate(`document.querySelectorAll('.el-table__body .el-table__row')[${index}].querySelector('td:last-child button').click()`)
    await until(() => evaluate("Boolean([...document.querySelectorAll('.el-dialog')].find(dialog => dialog.getClientRects().length))"))
    const detail = await evaluate("[...document.querySelectorAll('.el-dialog')].find(dialog => dialog.getClientRects().length)?.innerText || ''")
    assert.ok(detail.includes(expected), `${fixtures[index].definition.name} 的详情缺少“${expected}”`)
    await evaluate("[...document.querySelectorAll('.el-dialog')].find(dialog => dialog.getClientRects().length)?.querySelector('.el-dialog__headerbtn')?.click()")
    await delay(120)
  }
  console.log('PASS: 三种协议版本均显示统一入口，无版本协议显示明确状态')
} finally {
  // 清理本次临时浏览器和用户目录。
  socket?.close()
  const exited = new Promise(resolve => { if (child.exitCode !== null || child.signalCode !== null) resolve(); else child.once('exit', resolve) })
  child.kill()
  const forceStop = setTimeout(() => child.kill('SIGKILL'), 3000)
  forceStop.unref()
  await exited
  clearTimeout(forceStop)
  await rm(profile, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 })
}
