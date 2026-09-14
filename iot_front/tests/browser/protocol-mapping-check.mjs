// Real Chromium + Vue components + isolated synthetic API. No browser packages needed.
import { spawn } from 'node:child_process'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import assert from 'node:assert/strict'

// Isolated component fixture: synthetic releases and in-memory save only.
let savedBody
const server = await createServer({ configFile:false, plugins:[vue()], server:{host:'127.0.0.1',port:0}, appType:'custom' })
server.middlewares.use(async(req,res,next)=>{
 if(req.url.startsWith('/api/')) {
  const chunks=[];for await(const chunk of req)chunks.push(chunk)
  savedBody=JSON.parse(Buffer.concat(chunks).toString())
  res.setHeader('Content-Type','application/json');res.end(JSON.stringify({release:{protocolId:'fixture',version:'2',status:'DRAFT',config:savedBody.draft.config}}));return
 }
 if(req.url.startsWith('/mapping-fixture')) {
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
 }
 next()
})
await server.listen()
const origin=server.resolvedUrls.local[0]

const profile = await mkdtemp(join(tmpdir(), 'iot-mapping-browser-'))
const child = spawn(process.env.IOT_TEST_BROWSER, ['--headless=new','--no-first-run','--no-default-browser-check','--disable-gpu','--remote-debugging-port=0',`--user-data-dir=${profile}`,'about:blank'], { windowsHide:true, stdio:'ignore' })
let socket
const delay = ms => new Promise(resolve=>setTimeout(resolve,ms))
async function until(fn){for(let i=0;i<600;i++){const value=await fn();if(value)return value;await delay(100)}throw new Error('Browser condition timed out')}
try {
  const port = await until(async()=>{try{return (await readFile(join(profile,'DevToolsActivePort'),'utf8')).split('\n')[0]}catch{return null}})
  const pages=await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()
  socket=new WebSocket(pages.find(p=>p.type==='page').webSocketDebuggerUrl)
  await new Promise((resolve,reject)=>{socket.onopen=resolve;socket.onerror=reject})
  let id=0;const pending=new Map()
  socket.onmessage=event=>{const value=JSON.parse(event.data);if(value.id){const entry=pending.get(value.id);pending.delete(value.id);value.error?entry.reject(new Error(value.error.message)):entry.resolve(value.result)}}
  const call=(method,params={})=>new Promise((resolve,reject)=>{const next=++id;pending.set(next,{resolve,reject});socket.send(JSON.stringify({id:next,method,params}))})
  const evaluate=async expression=>{const r=await call('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});if(r.exceptionDetails)throw new Error(r.exceptionDetails.text+' '+JSON.stringify(r.exceptionDetails.exception));return r.result.value}
  await call('Page.enable')
  await call('Emulation.setDeviceMetricsOverride',{width:1280,height:900,deviceScaleFactor:1,mobile:false})

  const click=async text=>until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('button')].find(e=>e.textContent.trim()===${JSON.stringify(text)}&&e.getClientRects().length&&!e.disabled);if(!e)return false;e.click();return true})()`))
  const fill=async(label,value)=>evaluate(`(()=>{const input=document.querySelector('[aria-label="'+${JSON.stringify(label)}+'"]');if(!input)throw new Error('missing '+${JSON.stringify(label)});input.value=${JSON.stringify(value)};input.dispatchEvent(new Event('input',{bubbles:true}));input.dispatchEvent(new Event('change',{bubbles:true}));input.blur()})()`)
  for(const kind of ['json','modbus','hex']) {
   savedBody=null
   await call('Page.navigate',{url:origin+'mapping-fixture?kind='+kind})
   await click('新建版本')
   await until(()=>evaluate(`document.querySelector('[aria-label="第1行字段标识"]')!==null`))
   assert.equal(await evaluate(`document.querySelector('.mapping-editor textarea')!==null`),false)
   if(kind==='modbus' && process.env.IOT_TEST_SCREENSHOT) {
    const screenshot=await call('Page.captureScreenshot',{format:'png'});await writeFile(process.env.IOT_TEST_SCREENSHOT,Buffer.from(screenshot.data,'base64'))
   }
   await fill('第1行字段标识','mapped_temperature')
   if(kind==='json')await fill('第1行JSON路径','$.data.temperature')
   else await fill('第1行地址或偏移','12')
   await click('添加字段')
   await until(()=>evaluate(`document.querySelector('[aria-label="第2行字段标识"]')!==null`))
   await click('保存协议')
   await until(()=>evaluate(`document.body.textContent.includes('第 2 行请填写字段标识')`))
   assert.equal(savedBody,null)
   await evaluate(`document.querySelector('[aria-label="删除第2行字段"]').click()`)
   await evaluate(`document.querySelector('.el-table__expand-icon').click()`)
   await until(()=>evaluate(`document.querySelector('[aria-label="第1行倍率"]')!==null`))
   await fill('第1行倍率','0.5')
   await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true})
   await delay(200)
   assert.equal(await evaluate(`document.documentElement.scrollWidth<=window.innerWidth+2`),true,kind+' narrow page overflow')
   await click('保存协议')
   await until(()=>savedBody)
   const config=savedBody.draft.config
   if(kind==='json')assert.deepEqual(config.properties.mapped_temperature,{path:'$.data.temperature',type:'',scale:0.5})
   else {assert.equal(config[kind==='modbus'?'points':'fields'][0][kind==='modbus'?'address':'offset'],12);assert.equal(config[kind==='modbus'?'points':'fields'][0].scale,0.5)}
   await call('Emulation.setDeviceMetricsOverride',{width:1280,height:900,deviceScaleFactor:1,mobile:false})
  }
  console.log('PASS: JSON / Modbus / HEX mapping inputs, add/delete, blank validation, scale, saved request and 390px layout (synthetic API)')
} finally {
  await server.close()
  if(socket)socket.close()
  const exited = new Promise(resolve=>{if(child.exitCode!==null||child.signalCode!==null)resolve();else child.once('exit',resolve)})
  child.kill()
  const forceStop=setTimeout(()=>child.kill('SIGKILL'),3000)
  forceStop.unref()
  await exited
  clearTimeout(forceStop)
  await rm(profile,{recursive:true,force:true,maxRetries:5,retryDelay:100})
}
