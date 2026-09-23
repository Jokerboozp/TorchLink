// Built Vue app in a real insecure HTTP browser origin; isolated HTTP fixtures,
// deliberately unavailable MQTT. Run after npm run build, with IOT_TEST_BROWSER.
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */
import { spawn } from 'node:child_process' /* 引入当前代码需要的依赖。 */
import { createServer } from 'node:http' /* 引入当前代码需要的依赖。 */
import { mkdtemp, readFile, rm, mkdir, writeFile } from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import { tmpdir } from 'node:os' /* 引入当前代码需要的依赖。 */
import { join, resolve, sep } from 'node:path' /* 引入当前代码需要的依赖。 */
import { fileURLToPath } from 'node:url' /* 引入当前代码需要的依赖。 */

const dist = fileURLToPath(new URL('../../dist/', import.meta.url)) /* 声明 dist。 */
const root = fileURLToPath(new URL('../../../', import.meta.url)) /* 声明 root。 */
let alarms = [{ alarmId:'historical', deviceId:'device', status:'ACTIVE' }] /* 声明 alarms。 */
let polls = 0, brokerAttempts = 0 /* 声明 polls。 */
let generationRequests = 0 /* 声明 generationRequests。 */
const server = createServer(async (req, res) => { /* 声明 server。 */
  const path = new URL(req.url, 'http://fixture').pathname /* 声明 path。 */
  if (path.startsWith('/api/')) { /* 判断条件并选择处理分支。 */
    assert.equal(req.headers.authorization, 'Bearer browser-fixture') /* 验证实际结果符合预期。 */
    let data = {} /* 声明 data。 */
    if (path === '/api/v1/auth/me') data = { permissions:['*'] } /* 判断条件并选择处理分支。 */
    if (path === '/api/v1/events') { polls++; data = { alarms, devices:[], permissions:['*'] } } /* 判断条件并选择处理分支。 */
    if (path === '/api/v1/mqtt/token') data = { websocketUrl:`ws://iot-alarm.test:${server.address().port}/mqtt`, subscriptions:[] } /* 判断条件并选择处理分支。 */
    if (path === '/api/v1/dashboard') data = { devices:1, online:1, activeAlarms:alarms.length, highAlarms:alarms.length, trend:[], states:[], products:[] } /* 判断条件并选择处理分支。 */
    if (path === '/api/v1/alarms') data = { items:alarms, total:alarms.length } /* 判断条件并选择处理分支。 */
    if (path === '/api/v1/ai/protocol-assistant/generate') { /* 判断条件并选择处理分支。 */
      let body='' /* 声明 body。 */
      for await (const chunk of req) body+=chunk.toString() /* 循环处理当前数据。 */
      assert.ok(body.includes('"temperature":25')) /* 验证实际结果符合预期。 */
      generationRequests++ /* 执行当前语句并推进处理流程。 */
      data={name:'演示 HTTP 协议',parserType:'configurable_json_parser',config:{properties:{temperature:'$.temperature'}},transport:'MQTT',payloadFormat:'json',samplePayload:{temperature:25}} /* 更新 data 的值。 */
    } /* 结束当前表达式或代码块。 */
    res.writeHead(200, { 'Content-Type':'application/json', 'Cache-Control':'no-store' }) /* 执行当前语句并推进处理流程。 */
    res.end(JSON.stringify(data)) /* 执行当前语句并推进处理流程。 */
    return /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
  const file = resolve(dist, '.' + (path === '/' ? '/index.html' : path)) /* 声明 file。 */
  if (!file.startsWith(resolve(dist) + sep)) { res.writeHead(403).end(); return } /* 判断条件并选择处理分支。 */
  try { /* 执行当前语句并推进处理流程。 */
    const type = file.endsWith('.js') ? 'text/javascript' : file.endsWith('.css') ? 'text/css' : file.endsWith('.svg') ? 'image/svg+xml' : 'text/html' /* 声明 type。 */
    const content = await readFile(file) /* 声明 content。 */
    res.writeHead(200, { 'Content-Type':type }); res.end(content) /* 执行当前语句并推进处理流程。 */
  } catch { res.writeHead(404).end() } /* 结束当前表达式或代码块。 */
}) /* 结束当前表达式或代码块。 */
server.on('upgrade', (req, socket) => { brokerAttempts++; socket.destroy() }) /* 执行当前语句并推进处理流程。 */
await new Promise(done => server.listen(0, '127.0.0.1', done)) /* 等待异步操作完成。 */
const profile = await mkdtemp(join(tmpdir(), 'iot-alarm-http-')) /* 声明 profile。 */
const browser = spawn(process.env.IOT_TEST_BROWSER, ['--headless=new', '--no-first-run', '--no-default-browser-check', '--disable-gpu', '--no-proxy-server', '--host-resolver-rules=MAP iot-alarm.test 127.0.0.1', '--remote-debugging-port=0', `--user-data-dir=${profile}`, 'about:blank'], { windowsHide:true, stdio:'ignore' }) /* 声明 browser。 */
let socket /* 声明 socket。 */
const delay = ms => new Promise(done => setTimeout(done, ms)) /* 声明 delay。 */
async function until(fn) { /* 定义 until 函数。 */
  for (let i=0; i<200; i++) { const value=await fn(); if(value) return value; await delay(100) } /* 循环处理当前数据。 */
  throw Error('Browser condition timed out') /* 抛出当前错误。 */
} /* 结束当前表达式或代码块。 */
try { /* 执行当前语句并推进处理流程。 */
  const port = await until(async () => { try { return (await readFile(join(profile,'DevToolsActivePort'),'utf8')).split('\n')[0] } catch { return null } }) /* 声明 port。 */
  const pages = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json() /* 声明 pages。 */
  socket = new WebSocket(pages.find(page => page.type === 'page').webSocketDebuggerUrl) /* 更新 socket 的值。 */
  await new Promise((done, fail) => { socket.onopen=done; socket.onerror=fail }) /* 等待异步操作完成。 */
  let id=0 /* 声明 id。 */
  const pending=new Map(), errors=[] /* 声明 pending。 */
  socket.onmessage = event => { /* 更新 socket.onmessage 的值。 */
    const value=JSON.parse(event.data) /* 声明 value。 */
    if(value.id) { const job=pending.get(value.id); pending.delete(value.id); value.error ? job.reject(Error(value.error.message)) : job.resolve(value.result) } /* 判断条件并选择处理分支。 */
    else if(value.method==='Runtime.exceptionThrown') errors.push(value.params.exceptionDetails.text) /* 判断条件并选择处理分支。 */
  } /* 结束当前表达式或代码块。 */
  const call = (method,params={}) => new Promise((resolve,reject) => { const next=++id; pending.set(next,{resolve,reject}); socket.send(JSON.stringify({id:next,method,params})) }) /* 声明 call。 */
  const evaluate = async expression => { /* 声明 evaluate。 */
    const result=await call('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true}) /* 声明 result。 */
    if(result.exceptionDetails) throw Error(JSON.stringify(result.exceptionDetails)) /* 判断条件并选择处理分支。 */
    return result.result.value /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
  await call('Page.enable'); await call('Runtime.enable') /* 等待异步操作完成。 */
  await call('Page.addScriptToEvaluateOnNewDocument',{source:"localStorage.setItem('iot_token','browser-fixture');localStorage.setItem('iot_role','admin');localStorage.setItem('iot_tenant','tenant');localStorage.setItem('iot_user','browser-test');"}) /* 等待异步操作完成。 */
  await call('Page.navigate',{url:`http://iot-alarm.test:${server.address().port}`}) /* 等待异步操作完成。 */
  await until(() => polls>0 && brokerAttempts>0) /* 等待异步操作完成。 */
  assert.equal(await evaluate('window.isSecureContext'),false) /* 验证实际结果符合预期。 */
  assert.equal(await evaluate('typeof crypto.randomUUID'),'undefined') /* 验证实际结果符合预期。 */
  assert.equal(await evaluate("document.querySelectorAll('.global-alert-popup').length"),0) /* 验证实际结果符合预期。 */
  alarms = [...alarms, { alarmId:'new-http-alarm', deviceId:'device', deviceName:'演示烟感', alarmType:'FIRE_RISK', alarmLevel:'HIGH', status:'ACTIVE', alarmContent:'演示火灾报警', lastTriggeredAt:Date.now() }] /* 更新 alarms 的值。 */
  await until(() => evaluate("document.querySelector('.global-alert-popup')?.textContent.includes('演示火灾报警')")) /* 等待异步操作完成。 */
  assert.equal(await evaluate("document.querySelectorAll('.global-alert-popup').length"),1) /* 验证实际结果符合预期。 */
  await delay(3500) /* 等待异步操作完成。 */
  assert.equal(await evaluate("document.querySelectorAll('.global-alert-popup').length"),1) /* 验证实际结果符合预期。 */
  assert.deepEqual(errors,[]) /* 验证实际结果符合预期。 */
  const output=join(root,'.e2e','alarm-http') /* 声明 output。 */
  await mkdir(output,{recursive:true}) /* 等待异步操作完成。 */
  const screenshot=await call('Page.captureScreenshot',{format:'png'}) /* 声明 screenshot。 */
  await writeFile(join(output,'popup.png'),Buffer.from(screenshot.data,'base64')) /* 等待异步操作完成。 */
  await evaluate("document.querySelector('.global-alert-close').click()") /* 等待异步操作完成。 */
  await delay(3500) /* 等待异步操作完成。 */
  assert.equal(await evaluate("document.querySelectorAll('.global-alert-popup').length"),0) /* 验证实际结果符合预期。 */
  await call('Emulation.setDeviceMetricsOverride',{width:1440,height:1000,deviceScaleFactor:1,mobile:false}) /* 等待异步操作完成。 */
  const click=async text=>until(()=>evaluate(`(()=>{const button=[...document.querySelectorAll('button')].find(item=>item.textContent.trim()===${JSON.stringify(text)} && item.getClientRects().length && !item.disabled);if(!button)return false;button.click();return true})()`)) /* 声明 click。 */
  await click('协议管理');await click('协议生成') /* 等待异步操作完成。 */
  await until(()=>evaluate("!!document.querySelector('.protocol-generator textarea')")) /* 等待异步操作完成。 */
  assert.equal(await evaluate("document.querySelector('.protocol-generator').textContent.includes('协议名称')"),true) /* 验证实际结果符合预期。 */
  await evaluate("[...document.querySelectorAll('.protocol-generator .n-radio-button')].find(item=>item.textContent.trim()==='点表').click()") /* 等待异步操作完成。 */
  await until(()=>evaluate("document.querySelector('.protocol-generator').textContent.includes('或粘贴 CSV 点表')")) /* 等待异步操作完成。 */
  assert.equal(await evaluate("document.querySelector('.protocol-generator input[type=file]').accept"),'.xlsx,.csv') /* 验证实际结果符合预期。 */
  await delay(500) /* 等待异步操作完成。 */ // Wait for the dialog enter animation before visual review.
  const inputScreenshot=await call('Page.captureScreenshot',{format:'png'}) /* 声明 inputScreenshot。 */
  await writeFile(join(output,'protocol-generator.png'),Buffer.from(inputScreenshot.data,'base64')) /* 等待异步操作完成。 */
  await evaluate("[...document.querySelectorAll('.protocol-generator .n-radio-button')].find(item=>item.textContent.trim()==='报文').click()") /* 等待异步操作完成。 */
  await until(()=>evaluate("document.querySelector('.protocol-generator').textContent.includes('或粘贴报文')")) /* 等待异步操作完成。 */
  await evaluate(`(()=>{const input=document.querySelector('.protocol-generator textarea');input.value='{"temperature":25}';input.dispatchEvent(new Event('input',{bubbles:true}))})()`) /* 等待异步操作完成。 */
  await click('生成协议') /* 等待异步操作完成。 */
  await until(()=>evaluate("!!document.querySelector('.protocol-generator .mapping-editor input')")) /* 等待异步操作完成。 */
  assert.equal(generationRequests,1) /* 验证实际结果符合预期。 */
  assert.equal(await evaluate("document.querySelector('.mapping-editor').textContent.includes('编辑字段映射')"),true) /* 验证实际结果符合预期。 */
  assert.deepEqual(errors,[]) /* 验证实际结果符合预期。 */
  console.log('PASS: real HTTP origin without randomUUID; MQTT handshake fails; historical alarm silent; new alarm popup arrives via polling, closes and does not repeat. API responses are fixtures.') /* 执行当前语句并推进处理流程。 */
  console.log('PASS: HTTP protocol generator renders report/Excel-CSV inputs and submits a report to the fixture API, then displays mapping input fields.') /* 执行当前语句并推进处理流程。 */
} finally { /* 结束当前表达式或代码块。 */
  socket?.close() /* 执行当前语句并推进处理流程。 */
  const exited = new Promise(done => { if(browser.exitCode!==null || browser.signalCode!==null) done(); else browser.once('exit',done) }) /* 声明 exited。 */
  browser.kill(); await exited /* 执行当前语句并推进处理流程。 */
  server.closeAllConnections(); await new Promise(done => server.close(done)) /* 执行当前语句并推进处理流程。 */
  if (!resolve(profile).startsWith(resolve(tmpdir()) + sep + 'iot-alarm-http-')) throw Error('Unexpected browser profile path') /* 判断条件并选择处理分支。 */
  await rm(profile,{recursive:true,force:true,maxRetries:5,retryDelay:200}) /* 等待异步操作完成。 */
} /* 结束当前表达式或代码块。 */
