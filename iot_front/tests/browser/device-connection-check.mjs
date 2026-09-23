// Real Chromium + built Vue assets + isolated Go API. No browser packages needed.
import { spawn } from 'node:child_process' /* 引入当前代码需要的依赖。 */
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import { tmpdir } from 'node:os' /* 引入当前代码需要的依赖。 */
import { join } from 'node:path' /* 引入当前代码需要的依赖。 */
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */
const profile = await mkdtemp(join(tmpdir(), 'iot-onboard-browser-')) /* 声明 profile。 */
const child = spawn(process.env.IOT_TEST_BROWSER, ['--headless=new','--no-first-run','--no-default-browser-check','--disable-gpu','--remote-debugging-port=0',`--user-data-dir=${profile}`,'about:blank'], { windowsHide:true, stdio:'ignore' }) /* 声明 child。 */
let socket, snapshot /* 声明 socket。 */
const delay = ms => new Promise(resolve=>setTimeout(resolve,ms)) /* 声明 delay。 */
async function until(fn){for(let i=0;i<250;i++){const value=await fn();if(value)return value;await delay(100)}throw new Error('Browser condition timed out')} /* 定义 until 函数。 */
try { /* 执行当前语句并推进处理流程。 */
  const port = await until(async()=>{try{return (await readFile(join(profile,'DevToolsActivePort'),'utf8')).split('\n')[0]}catch{return null}}) /* 声明 port。 */
  const pages=await (await fetch(`http://127.0.0.1:${port}/json/list`)).json() /* 声明 pages。 */
  socket=new WebSocket(pages.find(p=>p.type==='page').webSocketDebuggerUrl) /* 更新 socket 的值。 */
  await new Promise((resolve,reject)=>{socket.onopen=resolve;socket.onerror=reject}) /* 等待异步操作完成。 */
  let id=0;const pending=new Map() /* 声明 id。 */
  socket.onmessage=event=>{const value=JSON.parse(event.data);if(value.id){const entry=pending.get(value.id);pending.delete(value.id);value.error?entry.reject(new Error(value.error.message)):entry.resolve(value.result)}} /* 更新 socket.onmessage 的值。 */
  const call=(method,params={})=>new Promise((resolve,reject)=>{const next=++id;pending.set(next,{resolve,reject});socket.send(JSON.stringify({id:next,method,params}))}) /* 声明 call。 */
  const evaluate=async expression=>{const r=await call('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});if(r.exceptionDetails)throw new Error(r.exceptionDetails.text+' '+JSON.stringify(r.exceptionDetails.exception));return r.result.value} /* 声明 evaluate。 */
  snapshot=()=>evaluate('document.body.innerText.slice(0,7000)') /* 更新 snapshot 的值。 */
  await call('Page.enable') /* 等待异步操作完成。 */
  await call('Page.addScriptToEvaluateOnNewDocument',{source:`localStorage.setItem('iot_token',${JSON.stringify(process.env.IOT_TEST_TOKEN)});localStorage.setItem('iot_tenant','tenant');localStorage.setItem('iot_role','admin');localStorage.setItem('iot_user','browser-test');`}) /* 等待异步操作完成。 */
  await call('Page.navigate',{url:process.env.IOT_TEST_ORIGIN}) /* 等待异步操作完成。 */
  const click=async text=>until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('button')].find(e=>e.textContent.trim()===${JSON.stringify(text)}&&e.getClientRects().length&&!e.disabled);if(!e)return false;e.click();return true})()`)) /* 声明 click。 */

  await call('Page.addScriptToEvaluateOnNewDocument',{source:`window.__detailRequests=[];window.__failHistory=false;const fetchOriginal=window.fetch;window.fetch=(url,options)=>{const path=String(url);if(path.includes('/device-registry/'))window.__detailRequests.push(path);if(window.__failHistory && /\\/history\\?page=/.test(path))return Promise.resolve(new Response(JSON.stringify({detail:'历史服务暂时不可用'}),{status:503,headers:{'Content-Type':'application/json'}}));return fetchOriginal(url,options)}`}) /* 等待异步操作完成。 */
  await call('Page.reload') /* 等待异步操作完成。 */
  await call('Emulation.setDeviceMetricsOverride',{width:1360,height:900,deviceScaleFactor:1,mobile:false}) /* 等待异步操作完成。 */
  await click('设备管理') /* 等待异步操作完成。 */
  await click('连接详情') /* 等待异步操作完成。 */
  await until(()=>evaluate(`document.querySelector('.el-drawer .el-descriptions')`)) /* 等待异步操作完成。 */
  await delay(400) /* 等待异步操作完成。 */
  assert.equal(await evaluate(`window.__detailRequests.filter(p=>p.endsWith('/children?page=1&pageSize=20')).length`),0,'ordinary device must not request children') /* 验证实际结果符合预期。 */
  assert.equal(await evaluate(`window.__detailRequests.some(p=>p.includes('/shadow') || p.includes('/commands'))`),false,'removed features must not issue requests') /* 验证实际结果符合预期。 */
  assert.equal(await evaluate(`document.querySelector('.el-drawer').textContent.includes('设备影子') || document.querySelector('.el-drawer').textContent.includes('设备孪生与拓扑')`),false,'removed features must not have controls') /* 验证实际结果符合预期。 */
  assert.ok(await evaluate(`document.querySelector('.device-access-info .el-descriptions')?.textContent.includes('/api/v1/device-ingest/')`),'access information must be in structured cells') /* 验证实际结果符合预期。 */
  assert.ok(await evaluate(`document.querySelector('.device-properties .el-table')?.textContent.includes('42')`),'properties must be in a table') /* 验证实际结果符合预期。 */
  await evaluate('window.__failHistory=true') /* 等待异步操作完成。 */
  await click('刷新') /* 等待异步操作完成。 */
  await until(()=>evaluate(`document.querySelector('.device-history .el-alert')?.textContent.includes('历史服务暂时不可用')`)) /* 等待异步操作完成。 */
  assert.ok(await evaluate(`document.querySelector('.device-properties')?.textContent.includes('42')`),'optional error must not hide device data') /* 验证实际结果符合预期。 */
  await evaluate('window.__failHistory=false') /* 等待异步操作完成。 */
  await click('刷新') /* 等待异步操作完成。 */
  await until(()=>evaluate(`!document.querySelector('.device-history .el-alert')`)) /* 等待异步操作完成。 */
  if(process.env.IOT_TEST_SCREENSHOT_DIR)await writeFile(join(process.env.IOT_TEST_SCREENSHOT_DIR,'device-connection-desktop.png'),Buffer.from((await call('Page.captureScreenshot',{format:'png'})).data,'base64')) /* 判断条件并选择处理分支。 */
  await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true}) /* 等待异步操作完成。 */
  await delay(300) /* 等待异步操作完成。 */
  assert.ok(await evaluate(`(()=>{const e=document.querySelector('.n-drawer-body-content-wrapper');return e.scrollWidth<=e.clientWidth+1})()`),'drawer content must not overflow') /* 验证实际结果符合预期。 */
  assert.ok(await evaluate(`[...document.querySelectorAll('.device-summary .n-descriptions-table tr')].every(e=>e.children.length===2)`),'mobile overview must use one label/value pair per row') /* 验证实际结果符合预期。 */
  assert.ok(await evaluate(`(()=>{const card=document.querySelector('.connection-section');return getComputedStyle(card).backgroundColor==='rgb(255, 255, 255)' && getComputedStyle(document.querySelector('.n-drawer-body-content-wrapper')).backgroundColor==='rgb(241, 244, 248)' && getComputedStyle(card).borderTopWidth!=='0px'})()`),'card boundaries must be distinct from background') /* 验证实际结果符合预期。 */
  if(process.env.IOT_TEST_SCREENSHOT_DIR)await writeFile(join(process.env.IOT_TEST_SCREENSHOT_DIR,'device-connection-mobile.png'),Buffer.from((await call('Page.captureScreenshot',{format:'png'})).data,'base64')) /* 判断条件并选择处理分支。 */
  await evaluate(`localStorage.setItem('iot_token',${JSON.stringify(process.env.IOT_TEST_VIEWER_TOKEN)});localStorage.setItem('iot_role','viewer')`) /* 等待异步操作完成。 */
  // Remove the startup admin token hook before reload.
  await call('Page.navigate',{url:'about:blank'}) /* 等待异步操作完成。 */
  await call('Page.addScriptToEvaluateOnNewDocument',{source:`localStorage.setItem('iot_token',${JSON.stringify(process.env.IOT_TEST_VIEWER_TOKEN)});localStorage.setItem('iot_role','viewer')`}) /* 等待异步操作完成。 */
  await call('Page.navigate',{url:process.env.IOT_TEST_ORIGIN}) /* 等待异步操作完成。 */
  await click('设备管理');await click('连接详情') /* 等待异步操作完成。 */
  await until(()=>evaluate(`document.querySelector('.el-drawer .el-descriptions')`)) /* 等待异步操作完成。 */
  assert.equal(await evaluate(`document.querySelector('.el-drawer').textContent.includes('重新生成凭据')`),false,'viewer must not be offered credential mutation') /* 验证实际结果符合预期。 */
  console.log('PASS: structured fields, distinct sections, mobile layout, no unrelated API requests, independent failure recovery and viewer permissions') /* 执行当前语句并推进处理流程。 */
} catch(e) {if(snapshot)console.error(await snapshot());throw e} finally { /* 结束当前表达式或代码块。 */
  if(socket)socket.close() /* 判断条件并选择处理分支。 */
  const exited = new Promise(resolve=>{if(child.exitCode!==null||child.signalCode!==null)resolve();else child.once('exit',resolve)}) /* 声明 exited。 */
  child.kill() /* 执行当前语句并推进处理流程。 */
  const forceStop=setTimeout(()=>child.kill('SIGKILL'),3000) /* 声明 forceStop。 */
  forceStop.unref() /* 执行当前语句并推进处理流程。 */
  await exited /* 等待异步操作完成。 */
  clearTimeout(forceStop) /* 执行当前语句并推进处理流程。 */
  await rm(profile,{recursive:true,force:true,maxRetries:5,retryDelay:100}) /* 等待异步操作完成。 */
} /* 结束当前表达式或代码块。 */
