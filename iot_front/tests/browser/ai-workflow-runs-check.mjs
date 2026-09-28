// Isolated browser acceptance: synthetic API only; never contacts a real Harness.
import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { spawn } from 'node:child_process'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { extname, join, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

const dist = fileURLToPath(new URL('../../dist/', import.meta.url))
const profile = await mkdtemp(join(tmpdir(), 'iot-ai-runs-'))
let stopRequests = 0
let listRequests = 0
let run = {runId:'ai_run_demo_01',tenantId:'demo',actor:'admin',workflowId:'alarm-handler',workflowName:'AI 告警研判',model:'deepseek-flash',status:'running',startedAt:Date.now()-45000}
const server = createServer(async (req,res) => {
  const path = new URL(req.url,'http://fixture').pathname
  if(path.startsWith('/api/')) {
    let body={items:[],total:0}
    if(path==='/api/v1/auth/login')body={accessToken:'fixture',username:'admin',tenantId:'demo',role:'admin',permissions:['*']}
    if(path==='/api/v1/auth/me')body={username:'admin',tenantId:'demo',role:'admin',permissions:['*']}
    if(path==='/api/v1/events')body={permissions:['*'],alarms:[],states:[],accessVersion:''}
    if(path==='/api/v1/ai/providers')body={items:[],active:{id:'deepseek',enabled:true},config:{provider:'deepseek',model:'deepseek-flash',baseUrl:'https://api.deepseek.com',apiKeyConfigured:true,maxTokens:2048},healthy:true}
    if(path==='/api/v1/ai/runs'){listRequests++;body={items:run?[run]:[],total:run?1:0}}
    if(path==='/api/v1/ai/runs/ai_run_demo_01/stop' && req.method==='POST') {
      stopRequests++;run.status='stopping';res.statusCode=202;body={status:'stopping'}
      setTimeout(()=>{run=null},1500)
    }
    res.setHeader('content-type','application/json');res.end(JSON.stringify(body));return
  }
  try {
    const file=resolve(dist, path==='/'?'index.html':path.slice(1))
    if(!file.startsWith(resolve(dist)+sep))throw new Error('path outside fixture')
    const data=await readFile(file)
    res.setHeader('content-type',({'.js':'text/javascript','.css':'text/css','.html':'text/html','.svg':'image/svg+xml'})[extname(file)]||'application/octet-stream');res.end(data)
  } catch {res.statusCode=404;res.end()}
})
await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve))
const origin=`http://127.0.0.1:${server.address().port}`
const browser=spawn(process.env.IOT_TEST_BROWSER||'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',[
  '--headless=new','--no-first-run','--no-default-browser-check','--disable-gpu','--disable-background-timer-throttling','--remote-debugging-port=0',`--user-data-dir=${profile}`,'about:blank',
],{windowsHide:true,stdio:'ignore'})
const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms))
async function until(check){for(let i=0;i<150;i++){const value=await check();if(value)return value;await delay(100)}throw new Error('browser condition timed out')}
let socket
try {
  const port=await until(async()=>{try{return (await readFile(join(profile,'DevToolsActivePort'),'utf8')).split('\n')[0]}catch{return false}})
  const targets=await(await fetch(`http://127.0.0.1:${port}/json/list`)).json()
  socket=new WebSocket(targets.find(item=>item.type==='page').webSocketDebuggerUrl)
  await new Promise((resolve,reject)=>{socket.onopen=resolve;socket.onerror=reject})
  let sequence=0
  const pending=new Map(),errors=[]
  socket.onmessage=event=>{
    const message=JSON.parse(event.data)
    if(message.method==='Runtime.exceptionThrown')errors.push(message.params.exceptionDetails.text)
    if(!message.id)return
    const item=pending.get(message.id);if(!item)return;pending.delete(message.id)
    message.error?item.reject(new Error(message.error.message)):item.resolve(message.result)
  }
  const call=(method,params={})=>new Promise((resolve,reject)=>{const id=++sequence;const timer=setTimeout(()=>reject(new Error(`CDP timeout: ${method}`)),20000);pending.set(id,{resolve:value=>{clearTimeout(timer);resolve(value)},reject:error=>{clearTimeout(timer);reject(error)}});socket.send(JSON.stringify({id,method,params}))})
  const evaluate=async expression=>{const r=await call('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});if(r.exceptionDetails)throw new Error(r.exceptionDetails.text);return r.result.value}
  await call('Runtime.enable')
  await call('Page.enable')
  await call('Emulation.setDeviceMetricsOverride',{width:1440,height:1100,deviceScaleFactor:1,mobile:false})
  await call('Page.addScriptToEvaluateOnNewDocument',{source:'localStorage.clear()'})
  await call('Page.navigate',{url:origin})
  await until(()=>evaluate(`Boolean(document.querySelector('.login-form input[type=password]'))`))
  await evaluate(`(()=>{const input=document.querySelector('.login-form input[type=password]');input.value='fixture';input.dispatchEvent(new Event('input',{bubbles:true}));document.querySelector('.login-form button[type=submit]').click()})()`)
  await until(()=>evaluate(`Boolean(document.querySelector('.nav-item[aria-label="模型管理"]'))`))
  await evaluate(`document.querySelector('.nav-item[aria-label="模型管理"]').click()`)
  await until(()=>evaluate(`document.querySelector('.workflow-runs')?.innerText.includes('ai_run_demo_01')`)).catch(async error=>{throw new Error(`${error.message}: ${await evaluate('document.querySelector(".app-content")?.innerText')} ${errors.join(';')}`)})
  assert.equal(await evaluate(`document.querySelectorAll('.workflow-runs button').length >= 2`),true)
  const screenshot=await call('Page.captureScreenshot',{format:'png'})
  const screenshotPath=fileURLToPath(new URL('../../../data/ai-workflow-runs-ui.png',import.meta.url))
  await writeFile(screenshotPath,Buffer.from(screenshot.data,'base64'))
  const clickStop=()=>evaluate(`[...document.querySelectorAll('.workflow-runs button')].find(b=>b.innerText.trim()==='强制停止').click()`)
  await clickStop()
  await until(()=>evaluate(`Boolean(document.querySelector('.n-dialog'))`))
  assert.equal(stopRequests,0)
  await evaluate(`[...document.querySelectorAll('.n-dialog button')].find(b=>b.innerText.trim()==='取消').click()`)
  await until(()=>evaluate(`!document.querySelector('.n-dialog')`))
  assert.equal(stopRequests,0)
  await clickStop()
  await until(()=>evaluate(`Boolean(document.querySelector('.n-dialog'))`))
  await evaluate(`[...document.querySelectorAll('.n-dialog button')].find(b=>b.innerText.trim()==='强制停止').click()`)
  await until(()=>evaluate(`document.querySelector('.workflow-runs')?.innerText.includes('正在停止')`))
  assert.equal(stopRequests,1)
  await until(()=>run===null)
  const requestsBeforeWait=listRequests
  await delay(3500)
  assert.equal(listRequests,requestsBeforeWait,'run list must not poll automatically')
  assert.equal(await evaluate(`document.querySelector('.workflow-runs')?.innerText.includes('正在停止')`),true)
  await evaluate(`[...document.querySelectorAll('.workflow-runs button')].find(b=>b.innerText.trim()==='刷新列表').click()`)
  await until(()=>evaluate(`document.querySelector('.workflow-runs')?.innerText.includes('当前没有运行中的 AI 工作流')`))
  assert.deepEqual(errors,[])
  console.log(`PASS: list, confirmation cancel, forced stop, stopping state, no polling, manual refresh. Screenshot: ${screenshotPath}`)
} finally {
  socket?.close()
  const exited=new Promise(resolve=>{if(browser.exitCode!==null)resolve();else browser.once('exit',resolve)})
  browser.kill();await exited
  server.closeAllConnections();await new Promise(resolve=>server.close(resolve))
  // Only remove the fresh, private browser profile created by this test.
  if(!resolve(profile).startsWith(resolve(tmpdir())+sep+'iot-ai-runs-'))throw new Error('unsafe profile cleanup path')
  await rm(profile,{recursive:true,force:true,maxRetries:5,retryDelay:200})
}
