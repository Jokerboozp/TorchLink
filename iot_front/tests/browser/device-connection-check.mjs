// Real Chromium + built Vue assets + isolated Go API. No browser packages needed.
import { spawn } from 'node:child_process'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import assert from 'node:assert/strict'
const profile = await mkdtemp(join(tmpdir(), 'iot-onboard-browser-'))
const child = spawn(process.env.IOT_TEST_BROWSER, ['--headless=new','--no-first-run','--no-default-browser-check','--disable-gpu','--remote-debugging-port=0',`--user-data-dir=${profile}`,'about:blank'], { windowsHide:true, stdio:'ignore' })
let socket, snapshot
const delay = ms => new Promise(resolve=>setTimeout(resolve,ms))
async function until(fn){for(let i=0;i<250;i++){const value=await fn();if(value)return value;await delay(100)}throw new Error('Browser condition timed out')}
try {
  const port = await until(async()=>{try{return (await readFile(join(profile,'DevToolsActivePort'),'utf8')).split('\n')[0]}catch{return null}})
  const pages=await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()
  socket=new WebSocket(pages.find(p=>p.type==='page').webSocketDebuggerUrl)
  await new Promise((resolve,reject)=>{socket.onopen=resolve;socket.onerror=reject})
  let id=0;const pending=new Map()
  socket.onmessage=event=>{const value=JSON.parse(event.data);if(value.id){const entry=pending.get(value.id);pending.delete(value.id);value.error?entry.reject(new Error(value.error.message)):entry.resolve(value.result)}}
  const call=(method,params={})=>new Promise((resolve,reject)=>{const next=++id;pending.set(next,{resolve,reject});socket.send(JSON.stringify({id:next,method,params}))})
  const evaluate=async expression=>{const r=await call('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});if(r.exceptionDetails)throw new Error(r.exceptionDetails.text+' '+JSON.stringify(r.exceptionDetails.exception));return r.result.value}
  snapshot=()=>evaluate('document.body.innerText.slice(0,7000)')
  await call('Page.enable')
  await call('Page.addScriptToEvaluateOnNewDocument',{source:`localStorage.setItem('iot_token',${JSON.stringify(process.env.IOT_TEST_TOKEN)});localStorage.setItem('iot_tenant','tenant');localStorage.setItem('iot_role','admin');localStorage.setItem('iot_user','browser-test');`})
  await call('Page.navigate',{url:process.env.IOT_TEST_ORIGIN})
  const click=async text=>until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('button')].find(e=>e.textContent.trim()===${JSON.stringify(text)}&&e.getClientRects().length&&!e.disabled);if(!e)return false;e.click();return true})()`))

  await call('Page.addScriptToEvaluateOnNewDocument',{source:`window.__detailRequests=[];window.__failHistory=false;const fetchOriginal=window.fetch;window.fetch=(url,options)=>{const path=String(url);if(path.includes('/device-registry/'))window.__detailRequests.push(path);if(window.__failHistory && /\\/history\\?page=/.test(path))return Promise.resolve(new Response(JSON.stringify({detail:'历史服务暂时不可用'}),{status:503,headers:{'Content-Type':'application/json'}}));return fetchOriginal(url,options)}`})
  await call('Page.reload')
  await call('Emulation.setDeviceMetricsOverride',{width:1360,height:900,deviceScaleFactor:1,mobile:false})
  await click('设备管理')
  await click('连接详情')
  await until(()=>evaluate(`document.querySelector('.el-drawer .el-descriptions')`))
  await delay(400)
  assert.equal(await evaluate(`window.__detailRequests.filter(p=>p.endsWith('/children?page=1&pageSize=20')).length`),0,'ordinary device must not request children')
  assert.equal(await evaluate(`window.__detailRequests.some(p=>p.includes('/shadow') || p.includes('/commands'))`),false,'removed features must not issue requests')
  assert.equal(await evaluate(`document.querySelector('.el-drawer').textContent.includes('设备影子') || document.querySelector('.el-drawer').textContent.includes('设备孪生与拓扑')`),false,'removed features must not have controls')
  assert.ok(await evaluate(`document.querySelector('.device-access-info .el-descriptions')?.textContent.includes('/api/v1/device-ingest/')`),'access information must be in structured cells')
  assert.ok(await evaluate(`document.querySelector('.device-properties .el-table')?.textContent.includes('42')`),'properties must be in a table')
  await evaluate('window.__failHistory=true')
  await click('刷新')
  await until(()=>evaluate(`document.querySelector('.device-history .el-alert')?.textContent.includes('历史服务暂时不可用')`))
  assert.ok(await evaluate(`document.querySelector('.device-properties')?.textContent.includes('42')`),'optional error must not hide device data')
  await evaluate('window.__failHistory=false')
  await click('刷新')
  await until(()=>evaluate(`!document.querySelector('.device-history .el-alert')`))
  if(process.env.IOT_TEST_SCREENSHOT_DIR)await writeFile(join(process.env.IOT_TEST_SCREENSHOT_DIR,'device-connection-desktop.png'),Buffer.from((await call('Page.captureScreenshot',{format:'png'})).data,'base64'))
  await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true})
  await delay(300)
  assert.ok(await evaluate(`(()=>{const e=document.querySelector('.el-drawer__body');return e.scrollWidth<=e.clientWidth+1})()`),'drawer content must not overflow')
  assert.ok(await evaluate(`[...document.querySelectorAll('.device-summary .el-descriptions__body tr')].every(e=>e.children.length===2)`),'mobile overview must use one label/value pair per row')
  assert.ok(await evaluate(`(()=>{const card=document.querySelector('.connection-section');return getComputedStyle(card).backgroundColor==='rgb(255, 255, 255)' && getComputedStyle(document.querySelector('.el-drawer__body')).backgroundColor==='rgb(241, 244, 248)' && getComputedStyle(card).borderTopWidth!=='0px'})()`),'card boundaries must be distinct from background')
  if(process.env.IOT_TEST_SCREENSHOT_DIR)await writeFile(join(process.env.IOT_TEST_SCREENSHOT_DIR,'device-connection-mobile.png'),Buffer.from((await call('Page.captureScreenshot',{format:'png'})).data,'base64'))
  await evaluate(`localStorage.setItem('iot_token',${JSON.stringify(process.env.IOT_TEST_VIEWER_TOKEN)});localStorage.setItem('iot_role','viewer')`)
  // Remove the startup admin token hook before reload.
  await call('Page.navigate',{url:'about:blank'})
  await call('Page.addScriptToEvaluateOnNewDocument',{source:`localStorage.setItem('iot_token',${JSON.stringify(process.env.IOT_TEST_VIEWER_TOKEN)});localStorage.setItem('iot_role','viewer')`})
  await call('Page.navigate',{url:process.env.IOT_TEST_ORIGIN})
  await click('设备管理');await click('连接详情')
  await until(()=>evaluate(`document.querySelector('.el-drawer .el-descriptions')`))
  assert.equal(await evaluate(`document.querySelector('.el-drawer').textContent.includes('重新生成凭据')`),false,'viewer must not be offered credential mutation')
  console.log('PASS: structured fields, distinct sections, mobile layout, no unrelated API requests, independent failure recovery and viewer permissions')
} catch(e) {if(snapshot)console.error(await snapshot());throw e} finally {
  if(socket)socket.close()
  const exited = new Promise(resolve=>{if(child.exitCode!==null||child.signalCode!==null)resolve();else child.once('exit',resolve)})
  child.kill()
  const forceStop=setTimeout(()=>child.kill('SIGKILL'),3000)
  forceStop.unref()
  await exited
  clearTimeout(forceStop)
  await rm(profile,{recursive:true,force:true,maxRetries:5,retryDelay:100})
}
