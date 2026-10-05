// Built Vue app in a real insecure HTTP browser origin; isolated HTTP fixtures,
// deliberately unavailable MQTT. Run after npm run build, with IOT_TEST_BROWSER.
import { startBrowser, delay } from '../helpers/browser.mjs'
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { readFile, mkdir, writeFile } from 'node:fs/promises'
import { join, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

const dist = fileURLToPath(new URL('../../dist/', import.meta.url))
const root = fileURLToPath(new URL('../../../', import.meta.url))
let alarms = [{ alarmId: 'historical', deviceId: 'device', status: 'ACTIVE' }]
let polls = 0,
  brokerAttempts = 0
let generationRequests = 0
let analysisRequests = 0,
  analysisFinished = false
const analysisResult = { summary: '手动研判完成', riskLevel: 'HIGH', model: 'fixture', createdAt: Date.now() }
const server = createServer(async (req, res) => {
  const path = new URL(req.url, 'http://fixture').pathname
  if (path.startsWith('/api/')) {
    assert.equal(req.headers.authorization, 'Bearer browser-fixture')
    let data = {}
    if (path === '/api/v1/auth/me') data = { permissions: ['*'] }
    if (path === '/api/v1/events') {
      polls++
      data = { alarms, devices: [], permissions: ['*'] }
    }
    if (path === '/api/v1/mqtt/token') data = { websocketUrl: `ws://iot-alarm.test:${server.address().port}/mqtt`, subscriptions: [] }
    if (path === '/api/v1/dashboard')
      data = { devices: 1, online: 1, activeAlarms: alarms.length, highAlarms: alarms.length, trend: [], states: [], products: [] }
    if (path === '/api/v1/alarms') data = { items: alarms, total: alarms.length }
    if (path.startsWith('/api/v1/alarms/')) data = alarms.find(alarm => alarm.alarmId === path.split('/').at(-1)) || {}
    if (path.startsWith('/api/v1/ai/alarm-analysis/')) {
      if (req.method === 'POST') {
        analysisRequests++
        data = { jobId: 'manual-job', status: 'running', progress: 8, message: '正在准备告警上下文' }
      } else if (path.includes('/progress')) {
        data = analysisRequests
          ? {
              jobId: 'manual-job',
              status: analysisFinished ? 'succeeded' : 'running',
              progress: analysisFinished ? 100 : 20,
              analysis: analysisFinished ? analysisResult : null
            }
          : { status: 'idle' }
      } else {
        data = analysisFinished ? analysisResult : null
      }
    }
    if (path === '/api/v1/ai/protocol-assistant/generate') {
      let body = ''
      for await (const chunk of req) body += chunk.toString()
      assert.ok(body.includes('"temperature":25'))
      generationRequests++
      data = {
        name: '演示 HTTP 协议',
        parserType: 'configurable_json_parser',
        config: { properties: { temperature: '$.temperature' } },
        transport: 'MQTT',
        payloadFormat: 'json',
        samplePayload: { temperature: 25 }
      }
    }
    res.writeHead(200, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' })
    res.end(JSON.stringify(data))
    return
  }
  const file = resolve(dist, '.' + (path === '/' ? '/index.html' : path))
  if (!file.startsWith(resolve(dist) + sep)) {
    res.writeHead(403).end()
    return
  }
  try {
    const type = file.endsWith('.js')
      ? 'text/javascript'
      : file.endsWith('.css')
        ? 'text/css'
        : file.endsWith('.svg')
          ? 'image/svg+xml'
          : 'text/html'
    const content = await readFile(file)
    res.writeHead(200, { 'Content-Type': type })
    res.end(content)
  } catch {
    res.writeHead(404).end()
  }
})
server.on('upgrade', (req, socket) => {
  brokerAttempts++
  socket.destroy()
})
await new Promise(done => server.listen(0, '127.0.0.1', done))
let browser
try {
  browser = await startBrowser({ timeout: 20000, args: ['--no-proxy-server', '--host-resolver-rules=MAP iot-alarm.test 127.0.0.1'] })
  const { call, evaluate, until, errors } = browser
  await call('Page.enable')
  await call('Runtime.enable')
  await call('Page.addScriptToEvaluateOnNewDocument', {
    source:
      "localStorage.setItem('iot_token','browser-fixture');localStorage.setItem('iot_role','admin');localStorage.setItem('iot_tenant','tenant');localStorage.setItem('iot_user','browser-test');"
  })
  await call('Page.navigate', { url: `http://iot-alarm.test:${server.address().port}` })
  await until(() => polls > 0 && brokerAttempts > 0)
  assert.equal(await evaluate('window.isSecureContext'), false)
  assert.equal(await evaluate('typeof crypto.randomUUID'), 'undefined')
  assert.equal(await evaluate("document.querySelectorAll('.global-alert-popup').length"), 0)
  alarms = [
    ...alarms,
    {
      alarmId: 'new-http-alarm',
      deviceId: 'device',
      deviceName: '演示烟感',
      alarmType: 'FIRE_RISK',
      alarmLevel: 'HIGH',
      status: 'ACTIVE',
      alarmContent: '演示火灾报警',
      lastTriggeredAt: Date.now()
    }
  ]
  await until(() => evaluate("document.querySelector('.global-alert-popup')?.textContent.includes('演示火灾报警')"))
  assert.equal(await evaluate("document.querySelectorAll('.global-alert-popup').length"), 1)
  await delay(3500)
  assert.equal(await evaluate("document.querySelectorAll('.global-alert-popup').length"), 1)
  assert.deepEqual(errors, [])
  const output = join(root, '.e2e', 'alarm-http')
  await mkdir(output, { recursive: true })
  const screenshot = await call('Page.captureScreenshot', { format: 'png' })
  await writeFile(join(output, 'popup.png'), Buffer.from(screenshot.data, 'base64'))
  await evaluate("document.querySelector('.global-alert-close').click()")
  await delay(3500)
  assert.equal(await evaluate("document.querySelectorAll('.global-alert-popup').length"), 0)
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1000, deviceScaleFactor: 1, mobile: false })
  const click = async text =>
    until(() =>
      evaluate(
        `(()=>{const button=[...document.querySelectorAll('button')].find(item=>item.textContent.trim()===${JSON.stringify(text)} && item.getClientRects().length && !item.disabled);if(!button)return false;button.click();return true})()`
      )
    )
  await click('告警中心')
  await click('查看详情')
  await until(() => evaluate("document.querySelector('.alarm-detail-dialog')?.textContent.includes('该告警尚未研判')"))
  assert.equal(analysisRequests, 0, 'new alarms and opening details must not start analysis')
  await click('开始研判')
  await until(() => analysisRequests === 1)
  assert.ok(
    await until(
      () =>
        evaluate(
          "[...document.querySelectorAll('.alarm-detail-dialog button')].some(button=>button.textContent.includes('研判中') && button.disabled)"
        ),
      'running analysis must prevent duplicate submission'
    )
  )
  await click('关闭详情')
  await until(() => evaluate("![...document.querySelectorAll('.alarm-detail-dialog')].some(dialog=>dialog.getClientRects().length)"))
  await click('查看详情')
  await until(() => evaluate("document.querySelector('.alarm-detail-dialog')?.textContent.includes('研判中')"))
  assert.equal(analysisRequests, 1, 'reopening must resume progress without starting another job')
  analysisFinished = true
  await until(() => evaluate("document.querySelector('.alarm-detail-dialog')?.textContent.includes('手动研判完成')"))
  await click('关闭详情')
  await until(() => evaluate("![...document.querySelectorAll('.alarm-detail-dialog')].some(dialog=>dialog.getClientRects().length)"))
  await click('查看详情')
  await until(() => evaluate("document.querySelector('.alarm-detail-dialog')?.textContent.includes('手动研判完成')"))
  assert.equal(analysisRequests, 1, 'viewing a saved result must not rerun analysis')
  await click('重新研判')
  await until(() => analysisRequests === 2)
  await click('关闭详情')
  await until(() => evaluate("![...document.querySelectorAll('.alarm-detail-dialog')].some(dialog=>dialog.getClientRects().length)"))
  console.log(
    'PASS: alarm analysis starts only on click; running jobs prevent duplicate submission; reopening resumes progress or shows saved results; explicit rerun works. API responses are fixtures.'
  )
  if (!process.env.IOT_TEST_ALARM_ONLY) {
    await click('协议开发')
    await click('协议生成')
    await until(() => evaluate("!!document.querySelector('.protocol-generator textarea')"))
    assert.equal(await evaluate("document.querySelector('.protocol-generator').textContent.includes('协议名称')"), true)
    await evaluate(
      "[...document.querySelectorAll('.protocol-generator .n-radio-button')].find(item=>item.textContent.trim()==='点表').click()"
    )
    await until(() => evaluate("document.querySelector('.protocol-generator').textContent.includes('或粘贴 CSV 点表')"))
    assert.equal(await evaluate("document.querySelector('.protocol-generator input[type=file]').accept"), '.xlsx,.csv')
    await delay(500) // Wait for the dialog enter animation before visual review.
    const inputScreenshot = await call('Page.captureScreenshot', { format: 'png' })
    await writeFile(join(output, 'protocol-generator.png'), Buffer.from(inputScreenshot.data, 'base64'))
    await evaluate(
      "[...document.querySelectorAll('.protocol-generator .n-radio-button')].find(item=>item.textContent.trim()==='报文').click()"
    )
    await until(() => evaluate("document.querySelector('.protocol-generator').textContent.includes('或粘贴报文')"))
    await evaluate(
      `(()=>{const input=document.querySelector('.protocol-generator textarea');input.value='{"temperature":25}';input.dispatchEvent(new Event('input',{bubbles:true}))})()`
    )
    await click('生成协议')
    await until(() => evaluate("!!document.querySelector('.protocol-generator .mapping-editor input')"))
    assert.equal(generationRequests, 1)
    assert.equal(await evaluate("document.querySelector('.mapping-editor').textContent.includes('编辑字段映射')"), true)
    assert.deepEqual(errors, [])
    console.log(
      'PASS: real HTTP origin without randomUUID; MQTT handshake fails; historical alarm silent; new alarm popup arrives via polling, closes and does not repeat. API responses are fixtures.'
    )
    console.log(
      'PASS: HTTP protocol generator renders report/Excel-CSV inputs and submits a report to the fixture API, then displays mapping input fields.'
    )
  }
} finally {
  try {
    await browser?.close()
  } finally {
    server.closeAllConnections()
    await new Promise(resolve => server.close(resolve))
  }
}
