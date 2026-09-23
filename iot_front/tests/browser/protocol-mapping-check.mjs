// Real Chromium + Vue components + isolated synthetic API. No browser packages needed.
import { spawn } from 'node:child_process' /* 引入当前代码需要的依赖。 */
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import { tmpdir } from 'node:os' /* 引入当前代码需要的依赖。 */
import { join } from 'node:path' /* 引入当前代码需要的依赖。 */
import { createServer } from 'vite' /* 引入当前代码需要的依赖。 */
import vue from '@vitejs/plugin-vue' /* 引入当前代码需要的依赖。 */
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */

// Isolated component fixture: synthetic releases and in-memory save only.
let savedBody /* 声明 savedBody。 */
const server = await createServer({ configFile:false, plugins:[vue()], server:{host:'127.0.0.1',port:0}, appType:'custom' }) /* 声明 server。 */
server.middlewares.use(async(req,res,next)=>{ /* 执行当前语句并推进处理流程。 */
 if(req.url.startsWith('/api/')) { /* 判断条件并选择处理分支。 */
  const chunks=[];for await(const chunk of req)chunks.push(chunk) /* 声明 chunks。 */
  savedBody=JSON.parse(Buffer.concat(chunks).toString()) /* 更新 savedBody 的值。 */
  res.setHeader('Content-Type','application/json');res.end(JSON.stringify({release:{protocolId:'fixture',version:'2',status:'DRAFT',config:savedBody.draft.config}}));return /* 执行当前语句并推进处理流程。 */
 } /* 结束当前表达式或代码块。 */
 if(req.url.startsWith('/mapping-fixture')) { /* 判断条件并选择处理分支。 */
  res.setHeader('Content-Type','text/html');res.end(await server.transformIndexHtml(req.url,`<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"></head><body><div id="app" style="padding:20px;max-width:1100px;margin:auto"></div><script type="module">
import {createApp,h} from 'vue';
import ElementPlus from 'element-plus';
import '/node_modules/element-plus/dist/index.css';
import '/src/styles.css';
import Assistant from '/src/views/ProtocolAssistantView.vue';
const kind=new URLSearchParams(location.search).get('kind');
const configs={json:{properties:{temperature:'$.temperature'}},hex:{startHex:'AA',fields:[{name:'temperature',offset:1,length:2,type:'uint16',endian:'big',scale:1}]},modbus:{points:[{identifier:'temperature',name:'温度',address:10,addressNotation:'zero_based',functionCode:3,dataType:'uint16',registerCount:1,byteOrder:'big',wordOrder:'ABCD',scale:0.1,offset:0,unit:'℃',pollIntervalSec:10}],blocks:[{startAddress:10}]}};
createApp({render:()=>h(Assistant,{initialName:'字段映射验收',initialRelease:{protocolId:'fixture',version:'1',status:'DRAFT',parserType:{json:'configurable_json_parser',hex:'configurable_hex_parser',modbus:'modbus_tcp_parser_v2'}[kind],transport:kind==='modbus'?'MODBUS_TCP':'MQTT',payloadFormat:kind==='json'?'json':'hex',config:configs[kind]}})}).use(ElementPlus).mount('#app');
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
  const fill=async(label,value)=>evaluate(`(()=>{const input=document.querySelector('[aria-label="'+${JSON.stringify(label)}+'"]');if(!input)throw new Error('missing '+${JSON.stringify(label)});input.value=${JSON.stringify(value)};input.dispatchEvent(new Event('input',{bubbles:true}));input.dispatchEvent(new Event('change',{bubbles:true}));input.blur()})()`) /* 声明 fill。 */
  for(const kind of ['json','modbus','hex']) { /* 循环处理当前数据。 */
   savedBody=null /* 更新 savedBody 的值。 */
   await call('Page.navigate',{url:origin+'mapping-fixture?kind='+kind}) /* 等待异步操作完成。 */
   await click('新建版本') /* 等待异步操作完成。 */
   await until(()=>evaluate(`document.querySelector('[aria-label="第1行字段标识"]')!==null`)) /* 等待异步操作完成。 */
   assert.equal(await evaluate(`document.querySelector('.mapping-editor textarea')!==null`),false) /* 验证实际结果符合预期。 */
   if(kind==='modbus' && process.env.IOT_TEST_SCREENSHOT) { /* 判断条件并选择处理分支。 */
    const screenshot=await call('Page.captureScreenshot',{format:'png'});await writeFile(process.env.IOT_TEST_SCREENSHOT,Buffer.from(screenshot.data,'base64')) /* 声明 screenshot。 */
   } /* 结束当前表达式或代码块。 */
   await fill('第1行字段标识','mapped_temperature') /* 等待异步操作完成。 */
   if(kind==='json')await fill('第1行JSON路径','$.data.temperature') /* 判断条件并选择处理分支。 */
   else await fill('第1行地址或偏移','12') /* 执行当前语句并推进处理流程。 */
   await click('添加字段') /* 等待异步操作完成。 */
   await until(()=>evaluate(`document.querySelector('[aria-label="第2行字段标识"]')!==null`)) /* 等待异步操作完成。 */
   await click('保存协议') /* 等待异步操作完成。 */
   await until(()=>evaluate(`document.body.textContent.includes('第 2 行请填写字段标识')`)) /* 等待异步操作完成。 */
   assert.equal(savedBody,null) /* 验证实际结果符合预期。 */
   await evaluate(`document.querySelector('[aria-label="删除第2行字段"]').click()`) /* 等待异步操作完成。 */
   await evaluate(`document.querySelector('.el-table__expand-icon').click()`) /* 等待异步操作完成。 */
   await until(()=>evaluate(`document.querySelector('[aria-label="第1行倍率"]')!==null`)) /* 等待异步操作完成。 */
   await fill('第1行倍率','0.5') /* 等待异步操作完成。 */
   await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true}) /* 等待异步操作完成。 */
   await delay(200) /* 等待异步操作完成。 */
   assert.equal(await evaluate(`document.documentElement.scrollWidth<=window.innerWidth+2`),true,kind+' narrow page overflow') /* 验证实际结果符合预期。 */
   await click('保存协议') /* 等待异步操作完成。 */
   await until(()=>savedBody) /* 等待异步操作完成。 */
   const config=savedBody.draft.config /* 声明 config。 */
   if(kind==='json')assert.deepEqual(config.properties.mapped_temperature,{path:'$.data.temperature',type:'',scale:0.5}) /* 判断条件并选择处理分支。 */
   else {assert.equal(config[kind==='modbus'?'points':'fields'][0][kind==='modbus'?'address':'offset'],12);assert.equal(config[kind==='modbus'?'points':'fields'][0].scale,0.5)} /* 验证实际结果符合预期。 */
   await call('Emulation.setDeviceMetricsOverride',{width:1280,height:900,deviceScaleFactor:1,mobile:false}) /* 等待异步操作完成。 */
  } /* 结束当前表达式或代码块。 */
  console.log('PASS: JSON / Modbus / HEX mapping inputs, add/delete, blank validation, scale, saved request and 390px layout (synthetic API)') /* 执行当前语句并推进处理流程。 */
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
