// Real Chromium + Vue components + isolated synthetic API. No browser packages needed.
import { spawn } from 'node:child_process' /* 引入当前代码需要的依赖。 */
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import { tmpdir } from 'node:os' /* 引入当前代码需要的依赖。 */
import { join } from 'node:path' /* 引入当前代码需要的依赖。 */
import { createServer } from 'vite' /* 引入当前代码需要的依赖。 */
import vue from '@vitejs/plugin-vue' /* 引入当前代码需要的依赖。 */
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */

// Isolated component fixture: synthetic releases and in-memory save only.
let savedBody;let transport="MQTT" /* 声明 savedBody。 */
const server = await createServer({ configFile:false, plugins:[vue()], server:{host:'127.0.0.1',port:0}, appType:'custom' }) /* 声明 server。 */
server.middlewares.use(async(req,res,next)=>{ /* 执行当前语句并推进处理流程。 */
 if(req.url.startsWith('/api/')) { /* 判断条件并选择处理分支。 */
  let value={items:[],total:0} /* 声明 value。 */
  if(req.method==='POST') {const chunks=[];for await(const chunk of req)chunks.push(chunk);savedBody=JSON.parse(Buffer.concat(chunks).toString());value={id:savedBody.id,status:transport==='MQTT'?'SENT':'acknowledged',...(transport==='MQTT'?{reply:{ok:true}}:{rawMessageId:'raw-reply'})}} /* 判断条件并选择处理分支。 */
  else if(req.url.includes('/raw-messages/'))value={messageId:'raw-reply',payload:'AA01',standardMessage:{messageType:'COMMAND_REPLY'}} /* 判断条件并选择处理分支。 */
  else if(req.url.includes('/connection'))value={device:{id:'d',name:'测试设备',productId:'p'},product:{name:'测试产品',thingModel:{commands:[{identifier:'set',name:'设置参数',fields:[{identifier:'value',name:'目标数值',dataType:'integer',required:true},{identifier:'enabled',name:'启用',dataType:'boolean',required:true},{identifier:'options',name:'扩展配置',dataType:'object'}]}]}},connector:transport,profile:transport==='MQTT'?null:{id:'gateway',enabled:true,mode:'listener'},profiles:[],canCommand:transport!=='MQTT',sessions:transport==='MQTT'?[]:[{deviceId:'d'}],mqttCommandAvailable:true,credentialEnabled:true} /* 判断条件并选择处理分支。 */
  res.setHeader('Content-Type','application/json');res.end(JSON.stringify(value));return /* 执行当前语句并推进处理流程。 */
 } /* 结束当前表达式或代码块。 */
 if(req.url.startsWith('/mapping-fixture')) { /* 判断条件并选择处理分支。 */
  res.setHeader('Content-Type','text/html');res.end(await server.transformIndexHtml(req.url,`<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"></head><body><div id="app" style="padding:20px;max-width:1100px;margin:auto"></div><script type="module">
import {createApp,h} from 'vue';
import {installUi} from '/src/ui/index.js';
import '/src/styles.css';
import '/src/naive-admin.css';
import DeviceConnection from '/src/components/DeviceConnection.vue';
localStorage.setItem('iot_role','operator');
const app=createApp({render:()=>h(DeviceConnection,{deviceId:'d'})});installUi(app);app.directive('permission',{mounted(){}});app.mount('#app');
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
  const fill=async(label,value)=>evaluate(`(()=>{const field=document.querySelector('[aria-label="'+${JSON.stringify(label)}+'"]');const input=field?.matches('input,textarea')?field:field?.querySelector('input,textarea');if(!input)throw new Error('missing '+${JSON.stringify(label)});input.focus();input.value=${JSON.stringify(value)};input.dispatchEvent(new Event('input',{bubbles:true}));input.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',bubbles:true}));input.dispatchEvent(new Event('change',{bubbles:true}));input.blur()})()`) /* 填写 Naive UI 命令字段。 */
  for(const mode of ['MQTT','TCP']) { /* 循环处理当前数据。 */
   transport=mode;savedBody=null /* 更新 transport 的值。 */
   await call('Page.navigate',{url:origin+'mapping-fixture'}) /* 等待异步操作完成。 */
   await until(()=>evaluate(`document.body.textContent.includes('设备控制')`)) /* 等待异步操作完成。 */
   assert.equal(await evaluate(`document.querySelector('.device-commands textarea')!==null`),false) /* 验证实际结果符合预期。 */
   await evaluate(`document.querySelector('[aria-label="设备命令"]').closest('.el-select').querySelector('.n-base-selection').click()`) /* 展开命令选择器。 */
   await until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('.n-base-select-option')].find(e=>e.textContent.trim()==='设置参数'&&e.getClientRects().length);if(!e)return false;e.click();return true})()`)) /* 选择设置参数命令。 */
   await fill('目标数值','0') /* 等待异步操作完成。 */
   await evaluate(`document.querySelector('[aria-label="启用"]').closest('.el-select').querySelector('.n-base-selection').click()`) /* 展开布尔参数选择器。 */
   await until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('.n-base-select-option')].find(e=>e.textContent.trim()==='否（false）'&&e.getClientRects().length);if(!e)return false;e.click();return true})()`)) /* 选择 false。 */
   await click('添加参数项') /* 等待异步操作完成。 */
   await fill('扩展配置.field','test') /* 等待异步操作完成。 */
   if(mode==='MQTT' && process.env.IOT_TEST_SCREENSHOT){await evaluate(`document.querySelector('.device-commands').scrollIntoView()`);await delay(350);await writeFile(process.env.IOT_TEST_SCREENSHOT,Buffer.from((await call('Page.captureScreenshot',{format:'png'})).data,'base64'))} /* 判断条件并选择处理分支。 */
   await click('执行命令');await click('确定');await until(()=>savedBody) /* 等待异步操作完成。 */
   assert.equal(savedBody.type,'set');assert.equal(savedBody.confirmed,true) /* 验证实际结果符合预期。 */
   assert.deepEqual(mode==='MQTT'?savedBody.data:{value:savedBody.value,enabled:savedBody.enabled,options:savedBody.options},{value:0,enabled:false,options:{field:'test'}}) /* 验证实际结果符合预期。 */
   await until(()=>evaluate(`document.querySelector('.device-commands').textContent.includes('发送状态')`)) /* 等待异步操作完成。 */
   if(mode==='TCP'){await click('查看应答报文');await until(()=>evaluate(`document.querySelector('.device-commands').textContent.includes('AA01')`))} /* 判断条件并选择处理分支。 */
   await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});await delay(200) /* 等待异步操作完成。 */
   assert.equal(await evaluate(`document.querySelector('.n-drawer-body-content-wrapper').scrollWidth<=document.querySelector('.n-drawer-body-content-wrapper').clientWidth+2`),true) /* 检查移动视口下抽屉内容。 */
   await call('Emulation.setDeviceMetricsOverride',{width:1280,height:900,deviceScaleFactor:1,mobile:false}) /* 等待异步操作完成。 */
  } /* 结束当前表达式或代码块。 */
  console.log('PASS: MQTT/Go device control forms, zero/false/object parameters, confirmation, response and mobile layout (synthetic API)') /* 执行当前语句并推进处理流程。 */
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
