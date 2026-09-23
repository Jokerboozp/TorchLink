// Real Chromium + built Vue assets + isolated Go API. No browser packages needed.
import { spawn } from 'node:child_process' /* 引入当前代码需要的依赖。 */
import { mkdtemp, readFile, rm } from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import { tmpdir } from 'node:os' /* 引入当前代码需要的依赖。 */
import { join } from 'node:path' /* 引入当前代码需要的依赖。 */
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */
const profile = await mkdtemp(join(tmpdir(), 'iot-onboard-browser-')) /* 声明 profile。 */
const child = spawn(process.env.IOT_TEST_BROWSER, ['--headless=new','--no-first-run','--no-default-browser-check','--disable-gpu','--remote-debugging-port=0',`--user-data-dir=${profile}`,'about:blank'], { windowsHide:true, stdio:'ignore' }) /* 声明 child。 */
let socket /* 声明 socket。 */
const delay = ms => new Promise(resolve=>setTimeout(resolve,ms)) /* 声明 delay。 */
async function until(fn){for(let i=0;i<600;i++){const value=await fn();if(value)return value;await delay(100)}throw new Error('Browser condition timed out')} /* 定义 until 函数。 */
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
  await call('Page.enable') /* 等待异步操作完成。 */
  await call('Page.addScriptToEvaluateOnNewDocument',{source:`localStorage.setItem('iot_token',${JSON.stringify(process.env.IOT_TEST_TOKEN)});localStorage.setItem('iot_tenant','tenant');localStorage.setItem('iot_role','admin');localStorage.setItem('iot_user','browser-test');`}) /* 等待异步操作完成。 */
  await call('Page.navigate',{url:process.env.IOT_TEST_ORIGIN}) /* 等待异步操作完成。 */
  const click=async text=>until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('button')].find(e=>e.textContent.trim()===${JSON.stringify(text)}&&e.getClientRects().length&&!e.disabled);if(!e)return false;e.click();return true})()`)) /* 声明 click。 */
  const fill=async(label,value)=>evaluate(`(()=>{const item=[...document.querySelectorAll('.el-form-item')].find(e=>e.querySelector('label')?.textContent.trim()===${JSON.stringify(label)});const input=item?.querySelector('input');if(!input)throw new Error('missing input '+${JSON.stringify(label)});input.value=${JSON.stringify(value)};input.dispatchEvent(new Event('input',{bubbles:true}))})()`) /* 声明 fill。 */
  await click('协议管理'); await click('上传源码') /* 等待异步操作完成。 */
  await until(()=>evaluate(`document.querySelector('input[type=file]')`)) /* 等待异步操作完成。 */
  await fill('协议标识','functions-browser') /* 等待异步操作完成。 */
  assert.equal(await evaluate(`[...document.querySelectorAll('.el-collapse-item__content textarea')].some(e=>e.getClientRects().length>0)`),false) /* 验证实际结果符合预期。 */
  assert.equal(await evaluate(`document.querySelector('input[placeholder="Go 函数模式留空自动生成新版本"]').value`),'') /* 验证实际结果符合预期。 */
  // Template download uses the authenticated endpoint and a real ZIP response.
  await click('下载解析模板') /* 等待异步操作完成。 */
  await call('DOM.enable') /* 等待异步操作完成。 */
  const tree=await call('DOM.getDocument') /* 声明 tree。 */
  const input=await call('DOM.querySelector',{nodeId:tree.root.nodeId,selector:'input[type=file]'}) /* 声明 input。 */
  await call('DOM.setFileInputFiles',{nodeId:input.nodeId,files:[process.env.IOT_TEST_SOURCE_GO]}) /* 等待异步操作完成。 */
  await click('上传、编译并发布') /* 等待异步操作完成。 */
  await until(()=>evaluate(`document.body.textContent.includes('协议已发布，可绑定产品使用') || document.querySelector('.source-error')?.textContent`)) /* 等待异步操作完成。 */
  assert.equal(await evaluate(`document.querySelector('.source-error')?.textContent || ''`),'') /* 验证实际结果符合预期。 */
  await until(()=>evaluate(`document.body.textContent.includes('functions-browser') && document.body.textContent.includes('auto-')`)) /* 等待异步操作完成。 */
  await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true}) /* 等待异步操作完成。 */
  await click('上传源码') /* 等待异步操作完成。 */
  await delay(200) /* 等待异步操作完成。 */
  assert.equal(await evaluate(`[...document.querySelectorAll('button')].some(e=>e.textContent.trim()==='下载 TCP / UDP 模板'&&e.getClientRects().length)`),true) /* 验证实际结果符合预期。 */
  assert.equal(await evaluate(`document.documentElement.scrollWidth<=window.innerWidth+2`),true) /* 验证实际结果符合预期。 */
  // Send a compile failure through the real page; the previous version remains.
  await fill('版本','invalid-browser') /* 等待异步操作完成。 */
  await call('DOM.setFileInputFiles',{nodeId:input.nodeId,files:[process.env.IOT_TEST_SOURCE_GO+'.invalid.go']}) /* 等待异步操作完成。 */
  await click('上传、编译并发布') /* 等待异步操作完成。 */
  await until(()=>evaluate(`document.querySelector('.source-error')?.textContent`)) /* 等待异步操作完成。 */
  console.log('PASS: authenticated Go template download, single Go file upload without JSON/version/runtime, actual compile and samples, failure visible, narrow view') /* 执行当前语句并推进处理流程。 */
} finally { /* 结束当前表达式或代码块。 */
  if(socket)socket.close() /* 判断条件并选择处理分支。 */
  const exited = new Promise(resolve=>{if(child.exitCode!==null||child.signalCode!==null)resolve();else child.once('exit',resolve)}) /* 声明 exited。 */
  child.kill() /* 执行当前语句并推进处理流程。 */
  const forceStop=setTimeout(()=>child.kill('SIGKILL'),3000) /* 声明 forceStop。 */
  forceStop.unref() /* 执行当前语句并推进处理流程。 */
  await exited /* 等待异步操作完成。 */
  clearTimeout(forceStop) /* 执行当前语句并推进处理流程。 */
  await rm(profile,{recursive:true,force:true,maxRetries:5,retryDelay:100}) /* 等待异步操作完成。 */
} /* 结束当前表达式或代码块。 */
