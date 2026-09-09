// Real Chromium + built Vue assets + isolated Go API. No browser packages needed.
import { spawn } from 'node:child_process'
import { mkdtemp, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import assert from 'node:assert/strict'
const profile = await mkdtemp(join(tmpdir(), 'iot-onboard-browser-'))
const child = spawn(process.env.IOT_TEST_BROWSER, ['--headless=new','--no-first-run','--no-default-browser-check','--disable-gpu','--remote-debugging-port=0',`--user-data-dir=${profile}`,'about:blank'], { windowsHide:true, stdio:'ignore' })
let socket
const delay = ms => new Promise(resolve=>setTimeout(resolve,ms))
async function until(fn){for(let i=0;i<100;i++){const value=await fn();if(value)return value;await delay(100)}throw new Error('Browser condition timed out')}
try {
  const port = await until(async()=>{try{return (await readFile(join(profile,'DevToolsActivePort'),'utf8')).split('\n')[0]}catch{return null}})
  const pages=await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()
  socket=new WebSocket(pages.find(p=>p.type==='page').webSocketDebuggerUrl)
  await new Promise((resolve,reject)=>{socket.onopen=resolve;socket.onerror=reject})
  let id=0;const pending=new Map()
  let blockDeviceToken = false, blockedTokenRequests = 0
  socket.onmessage=event=>{const value=JSON.parse(event.data);if(value.id){const entry=pending.get(value.id);pending.delete(value.id);value.error?entry.reject(new Error(value.error.message)):entry.resolve(value.result)}else if(value.method==='Fetch.requestPaused'){
    if(blockDeviceToken){blockedTokenRequests++;call('Fetch.failRequest',{requestId:value.params.requestId,errorReason:'InternetDisconnected'}).catch(()=>{})}
    else call('Fetch.continueRequest',{requestId:value.params.requestId}).catch(()=>{})
  }}
  const call=(method,params={})=>new Promise((resolve,reject)=>{const next=++id;pending.set(next,{resolve,reject});socket.send(JSON.stringify({id:next,method,params}))})
  const evaluate=async expression=>{const r=await call('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});if(r.exceptionDetails)throw new Error(r.exceptionDetails.text+' '+JSON.stringify(r.exceptionDetails.exception));return r.result.value}
  await call('Page.enable')
  await call('Page.addScriptToEvaluateOnNewDocument',{source:`localStorage.setItem('iot_token',${JSON.stringify(process.env.IOT_TEST_TOKEN)});localStorage.setItem('iot_tenant','tenant');localStorage.setItem('iot_role','admin');localStorage.setItem('iot_user','browser-test');`})
  await call('Page.navigate',{url:process.env.IOT_TEST_ORIGIN})
  const click=async text=>until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('button')].find(e=>e.textContent.trim()===${JSON.stringify(text)}&&e.getClientRects().length&&!e.disabled);if(!e)return false;e.click();return true})()`))
  const fill=async(label,value)=>evaluate(`(()=>{const item=[...document.querySelectorAll('.el-form-item')].find(e=>e.querySelector('label')?.textContent.trim()===${JSON.stringify(label)});const input=item?.querySelector('input');if(!input)throw new Error('missing input '+${JSON.stringify(label)});input.value=${JSON.stringify(value)};input.dispatchEvent(new Event('input',{bubbles:true}))})()`)
  await click('设备接入')
  await until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('[role=tab]')].find(e=>e.textContent.trim()==='TCP / UDP 接入');if(!e)return false;e.click();return true})()`))
  await until(()=>evaluate(`(()=>{const item=[...document.querySelectorAll('.el-form-item')].find(e=>e.querySelector('label')?.textContent.trim()==='接入实例');const e=item?.querySelector('.el-select__wrapper');if(!e)return false;e.click();return true})()`))
  await until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('.el-select-dropdown__item')].find(e=>e.textContent.trim()==='tcp'&&e.getClientRects().length);if(!e)return false;e.click();return true})()`))
  await fill('在线设备标识','device')
  await evaluate(`(()=>{const item=[...document.querySelectorAll('.el-form-item')].find(e=>e.querySelector('label')?.textContent.trim()==='协议包支持的命令 JSON');const e=item.querySelector('textarea');e.value='{"type":"test"}';e.dispatchEvent(new Event('input',{bubbles:true}))})()`)
  await click('发送命令')
  await until(()=>evaluate(`document.querySelector('.el-message-box')?.textContent.includes('确认向此设备发送协议命令')`))
  await evaluate(`(()=>{const e=document.querySelector('.el-message-box__btns .el-button--primary');if(!e)throw new Error('confirmation missing');e.click()})()`)
  await until(()=>evaluate(`document.body.textContent.includes('命令已记录')`))
  await until(async()=>{await click('查询命令结果');return evaluate(`document.body.textContent.includes('已收到协议应答')`)})
  assert.equal(await evaluate(`document.querySelector('.surface-card pre')?.textContent.includes('"token"')||false`),false)
  console.log('PASS: remote protocol command form, actual manual confirmation, queue and query, authenticated Edge TCP dispatch and real correlated reply')
} finally {
  if(socket)socket.close()
  const exited = new Promise(resolve=>{if(child.exitCode!==null||child.signalCode!==null)resolve();else child.once('exit',resolve)})
  child.kill()
  const forceStop=setTimeout(()=>child.kill('SIGKILL'),3000)
  forceStop.unref()
  await exited
  clearTimeout(forceStop)
  await rm(profile,{recursive:true,force:true,maxRetries:5,retryDelay:100})
}
