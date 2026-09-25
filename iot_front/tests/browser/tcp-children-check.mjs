// Real Chromium + built Vue assets + isolated Go API. No browser packages needed.
import { spawn } from 'node:child_process' /* 引入当前代码需要的依赖。 */
import { mkdtemp, readFile, rm } from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
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
  let blockDeviceToken = false, blockedTokenRequests = 0 /* 声明 blockDeviceToken。 */
  socket.onmessage=event=>{const value=JSON.parse(event.data);if(value.id){const entry=pending.get(value.id);pending.delete(value.id);value.error?entry.reject(new Error(value.error.message)):entry.resolve(value.result)}else if(value.method==='Fetch.requestPaused'){ /* 更新 socket.onmessage 的值。 */
    if(blockDeviceToken){blockedTokenRequests++;call('Fetch.failRequest',{requestId:value.params.requestId,errorReason:'InternetDisconnected'}).catch(()=>{})} /* 判断条件并选择处理分支。 */
    else call('Fetch.continueRequest',{requestId:value.params.requestId}).catch(()=>{}) /* 执行当前语句并推进处理流程。 */
  }} /* 结束当前表达式或代码块。 */
  const call=(method,params={})=>new Promise((resolve,reject)=>{const next=++id;pending.set(next,{resolve,reject});socket.send(JSON.stringify({id:next,method,params}))}) /* 声明 call。 */
  const evaluate=async expression=>{const r=await call('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});if(r.exceptionDetails)throw new Error(r.exceptionDetails.text+' '+JSON.stringify(r.exceptionDetails.exception));return r.result.value} /* 声明 evaluate。 */
  snapshot=()=>evaluate('document.body.innerText.slice(0,7000)') /* 更新 snapshot 的值。 */
  await call('Page.enable') /* 等待异步操作完成。 */
  await call('Page.addScriptToEvaluateOnNewDocument',{source:`localStorage.setItem('iot_token',${JSON.stringify(process.env.IOT_TEST_TOKEN)});localStorage.setItem('iot_tenant','tenant');localStorage.setItem('iot_role','admin');localStorage.setItem('iot_user','browser-test');`}) /* 等待异步操作完成。 */
  await call('Page.navigate',{url:process.env.IOT_TEST_ORIGIN}) /* 等待异步操作完成。 */
  const click=async text=>until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('button')].find(e=>e.textContent.trim()===${JSON.stringify(text)}&&e.getClientRects().length&&!e.disabled);if(!e)return false;e.click();return true})()`)) /* 声明 click。 */
  const fill=async(label,value)=>evaluate(`(()=>{const item=[...document.querySelectorAll('.ui-form-item')].find(e=>e.querySelector('label')?.textContent.trim()===${JSON.stringify(label)});const input=item?.querySelector('input');if(!input)throw new Error('missing input '+${JSON.stringify(label)});input.value=${JSON.stringify(value)};input.dispatchEvent(new Event('input',{bubbles:true}))})()`) /* 声明 fill。 */
  const drawer="[...document.querySelectorAll('.n-drawer')].find(e=>e.getClientRects().length)"
  const modal="[...document.querySelectorAll('.n-modal')].find(e=>e.getClientRects().length)"
  await click('设备管理')
  // 主设备行：按设备编号定位，打开连接详情。
  await until(()=>evaluate(`(()=>{const row=[...document.querySelectorAll('.n-data-table-tbody .n-data-table-tr')].find(e=>e.querySelector('small.subline')?.textContent.trim()==='main-1');const button=[...(row?.querySelectorAll('.row-actions button')||[])].find(e=>e.textContent.trim()==='详情');if(!button)return false;button.click();return true})()`))
  await until(()=>evaluate(`${drawer}?.textContent.includes('子设备（1）')`))
  assert.ok(await evaluate(`${drawer}.textContent.includes('sensor')`))
  await click('查看子设备')
  await until(()=>evaluate(`${drawer}?.textContent.includes('所属主设备')`))
  assert.ok(await evaluate(`${drawer}.textContent.includes('主设备通信会话')`))
  await click('main-1')
  await until(()=>evaluate(`${drawer}?.textContent.includes('子设备（1）')`))
  await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true})
  await until(()=>evaluate(`${drawer}.getBoundingClientRect().width<=390`))
  await evaluate(`${drawer}.querySelector('.n-base-close').click()`)
  await until(()=>evaluate(`!${drawer}`))
  await call('Emulation.setDeviceMetricsOverride',{width:1280,height:900,deviceScaleFactor:1,mobile:false})
  // 接入点在设备模板详情中维护：新建时可设置子设备映射，并显示子设备模板绑定的协议。
  await click('设备模板')
  await until(()=>evaluate(`(()=>{const row=[...document.querySelectorAll('.n-data-table-tbody .n-data-table-tr')].find(e=>e.querySelector('td')?.textContent.trim().startsWith('parent'));const button=[...(row?.querySelectorAll('button')||[])].find(e=>e.textContent.trim()==='接入点');if(!button)return false;button.click();return true})()`))
  await click('新建接入点')
  await until(()=>evaluate(`${modal}?.textContent.includes('连接方向')`))
  await evaluate(`${modal}.querySelector('.profile-advanced .n-collapse-item__header-main').click()`)
  await until(()=>evaluate(`${modal}.textContent.includes('子设备类型映射')`))
  await click('添加子设备产品')
  await until(()=>evaluate(`${modal}.querySelector('input[aria-label="子设备类型"]')`))
  await evaluate(`(()=>{const e=${modal}.querySelector('input[aria-label="子设备类型"]');e.value='smoke';e.dispatchEvent(new Event('input',{bubbles:true}))})()`)
  await evaluate(`(()=>{const e=${modal}.querySelector('[aria-label="子设备产品"]');(e.closest('.n-base-selection')||e.querySelector('.n-base-selection')||e.closest('.ui-select')?.querySelector('.n-base-selection')).click()})()`)
  await until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('.n-base-select-option')].find(e=>e.textContent.trim()==='sensor'&&e.getClientRects().length);if(!e)return false;e.click();return true})()`))
  await until(()=>evaluate(`${modal}.querySelector('.protocol-access-settings')?.textContent.includes('已绑定协议：sensor ·')`))
  await evaluate(`${modal}.querySelector('.n-base-close').click()`)
  await until(()=>evaluate(`!${modal}`))
  // 编辑已有的监听接入点：保留子设备映射并保存。
  await until(()=>evaluate(`(()=>{const row=[...${drawer}.querySelectorAll('.n-data-table-tbody .n-data-table-tr')].find(e=>e.querySelector('td b')?.textContent.trim()==='listen'&&e.getClientRects().length);const button=[...(row?.querySelectorAll('.row-actions button')||[])].find(e=>e.textContent.trim()==='编辑');if(!button)return false;button.click();return true})()`))
  await until(()=>evaluate(`${modal}?.querySelector('.profile-advanced .n-collapse-item__header-main')`))
  await evaluate(`${modal}.querySelector('.profile-advanced .n-collapse-item__header-main').click()`)
  await until(()=>evaluate(`${modal}.querySelector('input[aria-label="子设备类型"]')?.value==='smoke'`))
  await click('保存接入点')
  const saved=await until(()=>evaluate(`[...document.querySelectorAll('.n-message')].map(e=>e.textContent.trim()).find(Boolean)`))
  assert.equal(saved,'接入点已保存')
  console.log('PASS parent child detail, return navigation, narrow drawer, access point child mapping with bound protocol, edit and save') /* 执行当前语句并推进处理流程。 */
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
