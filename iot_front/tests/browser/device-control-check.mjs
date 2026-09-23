// Real Chromium + Vue components + isolated synthetic API. No browser packages needed.
import { spawn } from 'node:child_process'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import assert from 'node:assert/strict'

// Isolated component fixture: synthetic releases and in-memory save only.
let savedBody;let transport="MQTT"
const server = await createServer({ configFile:false, plugins:[vue()], server:{host:'127.0.0.1',port:0}, appType:'custom' })
server.middlewares.use(async(req,res,next)=>{
 if(req.url.startsWith('/api/')) {
  let value={items:[],total:0}
  if(req.method==='POST') {const chunks=[];for await(const chunk of req)chunks.push(chunk);savedBody=JSON.parse(Buffer.concat(chunks).toString());value={id:savedBody.id,status:transport==='MQTT'?'SENT':'acknowledged',...(transport==='MQTT'?{reply:{ok:true}}:{rawMessageId:'raw-reply'})}}
  else if(req.url.includes('/raw-messages/'))value={messageId:'raw-reply',payload:'AA01',standardMessage:{messageType:'COMMAND_REPLY'}}
  else if(req.url.includes('/connection'))value={device:{id:'d',name:'测试设备',productId:'p'},product:{name:'测试产品',thingModel:{commands:[{identifier:'set',name:'设置参数',fields:[{identifier:'value',name:'目标数值',dataType:'integer',required:true},{identifier:'enabled',name:'启用',dataType:'boolean',required:true},{identifier:'options',name:'扩展配置',dataType:'object'}]}]}},connector:transport,profile:transport==='MQTT'?null:{id:'gateway',enabled:true,mode:'listener'},profiles:[],canCommand:transport!=='MQTT',sessions:transport==='MQTT'?[]:[{deviceId:'d'}],mqttCommandAvailable:true,credentialEnabled:true}
  res.setHeader('Content-Type','application/json');res.end(JSON.stringify(value));return
 }
 if(req.url.startsWith('/mapping-fixture')) {
  res.setHeader('Content-Type','text/html');res.end(await server.transformIndexHtml(req.url,`<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"></head><body><div id="app" style="padding:20px;max-width:1100px;margin:auto"></div><script type="module">
import {createApp,h} from 'vue';
import ElementPlus from 'element-plus';
import zhCn from '/node_modules/element-plus/es/locale/lang/zh-cn.mjs';
import '/node_modules/element-plus/dist/index.css';
import '/src/styles.css';
import DeviceConnection from '/src/components/DeviceConnection.vue';
localStorage.setItem('iot_role','operator');
createApp({render:()=>h(DeviceConnection,{deviceId:'d'})}).use(ElementPlus,{locale:zhCn}).mount('#app');
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
  for(const mode of ['MQTT','TCP']) {
   transport=mode;savedBody=null
   await call('Page.navigate',{url:origin+'mapping-fixture'})
   await until(()=>evaluate(`document.body.textContent.includes('设备控制')`))
   assert.equal(await evaluate(`document.querySelector('.device-commands textarea')!==null`),false)
   await evaluate(`document.querySelector('[aria-label="设备命令"]').closest('.el-select').querySelector('.el-select__wrapper').click()`)
   await until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('.el-select-dropdown__item')].find(e=>e.textContent.trim()==='设置参数'&&e.getClientRects().length);if(!e)return false;e.click();return true})()`))
   await fill('目标数值','0')
   await evaluate(`document.querySelector('[aria-label="启用"]').closest('.el-select').querySelector('.el-select__wrapper').click()`)
   await until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('.el-select-dropdown__item')].find(e=>e.textContent.trim()==='否（false）'&&e.getClientRects().length);if(!e)return false;e.click();return true})()`))
   await click('添加参数项')
   await fill('扩展配置.field','test')
   if(mode==='MQTT' && process.env.IOT_TEST_SCREENSHOT){await evaluate(`document.querySelector('.device-commands').scrollIntoView()`);await delay(350);await writeFile(process.env.IOT_TEST_SCREENSHOT,Buffer.from((await call('Page.captureScreenshot',{format:'png'})).data,'base64'))}
   await click('执行命令');await click('确定');await until(()=>savedBody)
   assert.equal(savedBody.type,'set');assert.equal(savedBody.confirmed,true)
   assert.deepEqual(mode==='MQTT'?savedBody.data:{value:savedBody.value,enabled:savedBody.enabled,options:savedBody.options},{value:0,enabled:false,options:{field:'test'}})
   await until(()=>evaluate(`document.querySelector('.device-commands').textContent.includes('发送状态')`))
   if(mode==='TCP'){await click('查看应答报文');await until(()=>evaluate(`document.querySelector('.device-commands').textContent.includes('AA01')`))}
   await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});await delay(200)
   assert.equal(await evaluate(`document.querySelector('.el-drawer__body').scrollWidth<=document.querySelector('.el-drawer__body').clientWidth+2`),true)
   await call('Emulation.setDeviceMetricsOverride',{width:1280,height:900,deviceScaleFactor:1,mobile:false})
  }
  console.log('PASS: MQTT/Go device control forms, zero/false/object parameters, confirmation, response and mobile layout (synthetic API)')
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
