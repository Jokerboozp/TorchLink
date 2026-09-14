// Real Chromium + Vue components + isolated synthetic API. No browser packages needed.
import { spawn } from 'node:child_process'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import assert from 'node:assert/strict'

// Gateway fixture with synthetic products and in-memory profile writes.
let savedBody
const profiles=[{id:'gateway-a',productId:'fire',protocolId:'fire-protocol',protocolVersion:'1',mode:'listener',network:'tcp',host:'0.0.0.0',port:26875,enabled:true},{id:'gateway-b',productId:'fire',protocolId:'fire-protocol',protocolVersion:'1',mode:'listener',network:'udp',host:'0.0.0.0',port:26876,enabled:true}]
const server = await createServer({ configFile:false, plugins:[vue()], server:{host:'127.0.0.1',port:0}, appType:'custom' })
server.middlewares.use(async(req,res,next)=>{
 if(req.url.startsWith('/api/')) {
  let value={items:[]}
  if(req.method==='POST'||req.method==='PUT') {
   const chunks=[];for await(const chunk of req)chunks.push(chunk)
   savedBody=JSON.parse(Buffer.concat(chunks).toString());value=savedBody
   const index=profiles.findIndex(p=>p.id===savedBody.id);if(index<0)profiles.push(savedBody);else profiles[index]=savedBody
  } else if(req.url.startsWith('/api/v1/products'))value={items:[{id:'fire',name:'消防主机'}],total:1}
  else if(req.url==='/api/v2/protocols')value={items:[{definition:{id:'fire-protocol',name:'消防协议'},releases:[{version:'1',status:'PUBLISHED'}]}]}
  else if(req.url==='/api/v2/device-access-profiles')value={items:profiles}
  else if(req.url.endsWith('/protocol-binding'))value={protocolId:'fire-protocol',version:'1'}
  res.setHeader('Content-Type','application/json');res.end(JSON.stringify(value));return
 }
 if(req.url.startsWith('/mapping-fixture')) {
  res.setHeader('Content-Type','text/html');res.end(await server.transformIndexHtml(req.url,`<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"></head><body><div id="app" style="padding:20px;max-width:1100px;margin:auto"></div><script type="module">
import {createApp,h} from 'vue';
import ElementPlus from 'element-plus';
import '/node_modules/element-plus/dist/index.css';
import '/src/styles.css';
import Gateways from '/src/views/ProtocolsView.vue';import {pageGuide} from '/src/pageGuide.js';
createApp({render:()=>h('div',[h('h1',pageGuide.profiles.title),h('p',pageGuide.profiles.sub),h(Gateways,{section:'profiles'})])}).use(ElementPlus).mount('#app');
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
  const fill=async(label,value)=>evaluate(`(()=>{const item=[...document.querySelectorAll('.el-dialog .el-form-item')].find(e=>e.querySelector('label')?.textContent.trim()===${JSON.stringify(label)});const input=item?.querySelector('input');if(!input)throw new Error('missing input');input.value=${JSON.stringify(value)};input.dispatchEvent(new Event('input',{bubbles:true}));input.dispatchEvent(new Event('change',{bubbles:true}));input.blur()})()`)
  await call('Page.navigate',{url:origin+'mapping-fixture'})
  await until(()=>evaluate(`document.body.textContent.includes('gateway-b')`))
  assert.equal(await evaluate(`document.querySelector('h1').textContent`),'接入网关')
  await click('新建网关');await fill('接入网关标识','gateway-c')
  await evaluate(`(()=>{const item=[...document.querySelectorAll('.el-dialog .el-form-item')].find(e=>e.querySelector('label')?.textContent.trim()==='关联产品');item.querySelector('.el-select__wrapper').click()})()`)
  await until(()=>evaluate(`(()=>{const item=[...document.querySelectorAll('.el-select-dropdown__item')].find(e=>e.textContent.trim()==='消防主机'&&e.getClientRects().length);if(!item)return false;item.click();return true})()`))
  await until(()=>evaluate(`document.querySelector('.el-dialog input[placeholder="选择产品后自动读取"]')?.value==='消防协议'`))
  assert.equal(await evaluate(`document.querySelector('.el-dialog input[placeholder="选择产品后自动读取"]').readOnly`),true)
  await fill('端口','26877')
  await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});await delay(200)
  assert.equal(await evaluate(`document.documentElement.scrollWidth<=window.innerWidth+2`),true)
  await click('保存接入网关');await until(()=>savedBody)
  assert.equal(savedBody.productId,'fire');assert.equal(savedBody.protocolId,'fire-protocol');assert.equal(savedBody.protocolVersion,'1');assert.equal(savedBody.port,26877)
  await until(()=>evaluate(`document.body.textContent.includes('3 个接入网关')`))
  await call('Emulation.setDeviceMetricsOverride',{width:1280,height:900,deviceScaleFactor:1,mobile:false})
  await click('停用');await until(()=>profiles[0].enabled===false)
  await click('编辑');await until(()=>evaluate(`document.body.textContent.includes('编辑接入网关')`))
  assert.equal(await evaluate(`document.querySelector('.el-dialog input[placeholder="例如 dahua-tcp"]').disabled`),true)
  if(process.env.IOT_TEST_SCREENSHOT){await delay(350);const screenshot=await call('Page.captureScreenshot',{format:'png'});await writeFile(process.env.IOT_TEST_SCREENSHOT,Buffer.from(screenshot.data,'base64'))}
  console.log('PASS: gateway labels, multiple gateways per product, product-bound protocol, create/save, disable/edit and 390px layout (synthetic API)')
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
