// Real Chromium + Vue components + isolated synthetic API. No browser packages needed.
import { spawn } from 'node:child_process' /* 引入当前代码需要的依赖。 */
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import { tmpdir } from 'node:os' /* 引入当前代码需要的依赖。 */
import { join } from 'node:path' /* 引入当前代码需要的依赖。 */
import { createServer } from 'vite' /* 引入当前代码需要的依赖。 */
import vue from '@vitejs/plugin-vue' /* 引入当前代码需要的依赖。 */
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */

// Gateway fixture with synthetic products and in-memory profile writes.
let savedBody /* 声明 savedBody。 */
const profiles=[{id:'gateway-a',productId:'fire',protocolId:'fire-protocol',protocolVersion:'1',mode:'listener',network:'tcp',host:'0.0.0.0',port:26875,enabled:true},{id:'gateway-b',productId:'fire',protocolId:'fire-protocol',protocolVersion:'1',mode:'listener',network:'udp',host:'0.0.0.0',port:26876,enabled:true}] /* 声明 profiles。 */
const server = await createServer({ configFile:false, plugins:[vue()], server:{host:'127.0.0.1',port:0}, appType:'custom' }) /* 声明 server。 */
server.middlewares.use(async(req,res,next)=>{ /* 执行当前语句并推进处理流程。 */
 if(req.url.startsWith('/api/')) { /* 判断条件并选择处理分支。 */
  let value={items:[]} /* 声明 value。 */
  if(req.method==='POST'||req.method==='PUT') { /* 判断条件并选择处理分支。 */
   const chunks=[];for await(const chunk of req)chunks.push(chunk) /* 声明 chunks。 */
   savedBody=JSON.parse(Buffer.concat(chunks).toString());value=savedBody /* 更新 savedBody 的值。 */
   const index=profiles.findIndex(p=>p.id===savedBody.id);if(index<0)profiles.push(savedBody);else profiles[index]=savedBody /* 声明 index。 */
  } else if(req.url.startsWith('/api/v1/products'))value={items:[{id:'fire',name:'消防主机'}],total:1} /* 结束当前表达式或代码块。 */
  else if(req.url==='/api/v2/protocols')value={items:[{definition:{id:'fire-protocol',name:'消防协议'},releases:[{version:'1',status:'PUBLISHED'}]}]} /* 判断条件并选择处理分支。 */
  else if(req.url==='/api/v2/device-access-profiles')value={items:profiles} /* 判断条件并选择处理分支。 */
  else if(req.url.endsWith('/protocol-binding'))value={protocolId:'fire-protocol',version:'1'} /* 判断条件并选择处理分支。 */
  res.setHeader('Content-Type','application/json');res.end(JSON.stringify(value));return /* 执行当前语句并推进处理流程。 */
 } /* 结束当前表达式或代码块。 */
 if(req.url.startsWith('/mapping-fixture')) { /* 判断条件并选择处理分支。 */
  res.setHeader('Content-Type','text/html');res.end(await server.transformIndexHtml(req.url,`<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"></head><body><div id="app" style="padding:20px;max-width:1100px;margin:auto"></div><script type="module">
import {createApp,h} from 'vue';
import {installUi} from '/src/ui/index.js';
import '/src/styles.css';
import '/src/naive-admin.css';
import Gateways from '/src/views/ProtocolsView.vue';import {pageGuide} from '/src/pageGuide.js';
const app=createApp({render:()=>h('div',[h('h1',pageGuide.profiles.title),h('p',pageGuide.profiles.sub),h(Gateways,{section:'profiles'})])});installUi(app);app.directive('permission',{mounted(){}});app.mount('#app');
</script></body></html>`));return
 } /* 结束当前表达式或代码块。 */
 next() /* 执行当前语句并推进处理流程。 */
}) /* 结束当前表达式或代码块。 */
await server.listen() /* 等待异步操作完成。 */
const origin=server.resolvedUrls.local[0] /* 声明 origin。 */

const profile = await mkdtemp(join(tmpdir(), 'iot-mapping-browser-')) /* 声明 profile。 */
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
  socket.onmessage=event=>{const value=JSON.parse(event.data);if(value.id){const entry=pending.get(value.id);pending.delete(value.id);value.error?entry.reject(new Error(value.error.message)):entry.resolve(value.result)}} /* 更新 socket.onmessage 的值。 */
  const call=(method,params={})=>new Promise((resolve,reject)=>{const next=++id;pending.set(next,{resolve,reject});socket.send(JSON.stringify({id:next,method,params}))}) /* 声明 call。 */
  const evaluate=async expression=>{const r=await call('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});if(r.exceptionDetails)throw new Error(r.exceptionDetails.text+' '+JSON.stringify(r.exceptionDetails.exception));return r.result.value} /* 声明 evaluate。 */
  await call('Page.enable') /* 等待异步操作完成。 */
  await call('Emulation.setDeviceMetricsOverride',{width:1280,height:900,deviceScaleFactor:1,mobile:false}) /* 等待异步操作完成。 */

  const click=async text=>until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('button')].find(e=>e.textContent.trim()===${JSON.stringify(text)}&&e.getClientRects().length&&!e.disabled);if(!e)return false;e.click();return true})()`)) /* 声明 click。 */
  const fill=async(label,value)=>evaluate(`(()=>{const item=[...document.querySelectorAll('.ui-dialog .ui-form-item')].find(e=>e.querySelector('label')?.textContent.trim()===${JSON.stringify(label)});const input=item?.querySelector('input');if(!input)throw new Error('missing input');input.focus();input.value=${JSON.stringify(value)};input.dispatchEvent(new Event('input',{bubbles:true}));input.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',bubbles:true}));input.dispatchEvent(new Event('change',{bubbles:true}));input.blur()})()`) /* 填写 Naive UI 表单并提交数字字段。 */
  await call('Page.navigate',{url:origin+'mapping-fixture'}) /* 等待异步操作完成。 */
  await until(()=>evaluate(`document.body?.textContent.includes('gateway-b')`)) /* 等待页面文档和合成数据就绪。 */
  assert.equal(await evaluate(`document.querySelector('h1').textContent`),'接入网关') /* 验证实际结果符合预期。 */
  await click('新建网关');await fill('接入网关标识','gateway-c') /* 等待异步操作完成。 */
  await evaluate(`(()=>{const item=[...document.querySelectorAll('.ui-dialog .ui-form-item')].find(e=>e.querySelector('label')?.textContent.trim()==='关联产品');item.querySelector('.n-base-selection').click()})()`) /* 展开 Naive UI 产品选择器。 */
  await until(()=>evaluate(`(()=>{const item=[...document.querySelectorAll('.n-base-select-option')].find(e=>e.textContent.trim()==='消防主机'&&e.getClientRects().length);if(!item)return false;item.click();return true})()`)) /* 选择夹具产品。 */
  await until(()=>evaluate(`document.querySelector('.ui-dialog input[placeholder="选择产品后自动读取"]')?.value==='消防协议'`)) /* 等待异步操作完成。 */
  assert.equal(await evaluate(`document.querySelector('.ui-dialog input[placeholder="选择产品后自动读取"]').readOnly`),true) /* 验证实际结果符合预期。 */
  await fill('端口','26877') /* 等待异步操作完成。 */
  await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});await delay(200) /* 等待异步操作完成。 */
  assert.equal(await evaluate(`document.documentElement.scrollWidth<=window.innerWidth+2`),true) /* 验证实际结果符合预期。 */
  await click('保存接入网关');await until(()=>savedBody) /* 等待异步操作完成。 */
  assert.equal(savedBody.productId,'fire');assert.equal(savedBody.protocolId,'fire-protocol');assert.equal(savedBody.protocolVersion,'1');assert.equal(savedBody.port,26877) /* 验证实际结果符合预期。 */
  await until(()=>evaluate(`document.body.textContent.includes('3 个接入网关')`)) /* 等待异步操作完成。 */
  await call('Emulation.setDeviceMetricsOverride',{width:1280,height:900,deviceScaleFactor:1,mobile:false}) /* 等待异步操作完成。 */
  await click('停用');await until(()=>profiles[0].enabled===false) /* 等待异步操作完成。 */
  await click('编辑');await until(()=>evaluate(`document.body.textContent.includes('编辑接入网关')`)) /* 等待异步操作完成。 */
  assert.equal(await evaluate(`document.querySelector('.ui-dialog input[placeholder="例如 dahua-tcp"]').disabled`),true) /* 验证实际结果符合预期。 */
  if(process.env.IOT_TEST_SCREENSHOT){await delay(350);const screenshot=await call('Page.captureScreenshot',{format:'png'});await writeFile(process.env.IOT_TEST_SCREENSHOT,Buffer.from(screenshot.data,'base64'))} /* 判断条件并选择处理分支。 */
  console.log('PASS: gateway labels, multiple gateways per product, product-bound protocol, create/save, disable/edit and 390px layout (synthetic API)') /* 执行当前语句并推进处理流程。 */
} finally { /* 结束当前表达式或代码块。 */
  await server.close() /* 等待异步操作完成。 */
  if(socket)socket.close() /* 判断条件并选择处理分支。 */
  const exited = new Promise(resolve=>{if(child.exitCode!==null||child.signalCode!==null)resolve();else child.once('exit',resolve)}) /* 声明 exited。 */
  child.kill() /* 执行当前语句并推进处理流程。 */
  const forceStop=setTimeout(()=>child.kill('SIGKILL'),3000) /* 声明 forceStop。 */
  forceStop.unref() /* 执行当前语句并推进处理流程。 */
  await exited /* 等待异步操作完成。 */
  clearTimeout(forceStop) /* 执行当前语句并推进处理流程。 */
  await rm(profile,{recursive:true,force:true,maxRetries:5,retryDelay:100}) /* 等待异步操作完成。 */
} /* 结束当前表达式或代码块。 */
