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
  await click('设备管理')
  await until(()=>evaluate(`(()=>{const row=[...document.querySelectorAll('.el-table__row')].find(e=>e.textContent.includes('Shadow device'));const button=[...(row?.querySelectorAll('button')||[])].find(e=>e.textContent.trim()==='连接详情');if(!button)return false;button.click();return true})()`))
  await until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('.el-collapse-item__header')].find(e=>e.textContent.trim()==='设备孪生与拓扑');if(!e)return false;e.click();return true})()`))
  await until(()=>evaluate(`document.querySelector('.device-twin')?.textContent.includes('拓扑版本 1')`))
  await evaluate(`document.querySelector('.twin-graph [aria-label="查看 孪生邻居"]').dispatchEvent(new MouseEvent('click',{bubbles:true}))`)
  await until(()=>evaluate(`document.querySelector('.device-twin')?.textContent.includes('当前查看：孪生邻居')`))
  const choose=async(label,option)=>{
    await until(()=>evaluate(`(()=>{const item=[...document.querySelectorAll('.device-twin .el-form-item')].find(e=>e.querySelector('label')?.textContent.trim()===${JSON.stringify(label)});const e=item?.querySelector('.el-select__wrapper');if(!e)return false;e.click();return true})()`))
    await until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('.el-select-dropdown__item')].find(e=>e.textContent.trim()===${JSON.stringify(option)}&&e.getClientRects().length);if(!e)return false;e.click();return true})()`))
  }
  await choose('目标设备','Shadow device')
  await click('添加关系')
  await until(()=>evaluate(`document.querySelector('.device-twin .el-alert')?.textContent.includes('cycle')`))
  assert.equal(await evaluate(`document.querySelector('.device-twin')?.textContent.includes('拓扑版本 1')`),true)
  await choose('关系类型','监测')
  await click('添加关系')
  await until(()=>evaluate(`document.querySelector('.device-twin')?.textContent.includes('拓扑版本 2')`))
  await until(()=>evaluate(`(()=>{const row=[...document.querySelectorAll('.device-twin .el-table__row')].find(e=>e.textContent.includes('监测'));const e=[...(row?.querySelectorAll('button')||[])].find(e=>e.textContent.trim()==='解除');if(!e)return false;e.click();return true})()`))
  await until(()=>evaluate(`document.querySelector('.el-message-box')?.textContent.includes('解除')`))
  await evaluate(`document.querySelector('.el-message-box__btns .el-button--primary').click()`)
  await until(()=>evaluate(`document.querySelector('.device-twin')?.textContent.includes('拓扑版本 3')`))
  await click('返回当前设备')
  await until(()=>evaluate(`document.querySelector('.device-twin')?.textContent.includes('当前查看：Shadow device')`))
  await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true})
  await until(()=>evaluate(`document.querySelector('.twin-graph').getBoundingClientRect().width<=390`))
  assert.equal(await evaluate(`document.querySelector('.device-twin pre')?.textContent.includes('42')`),true)
  console.log('PASS: actual device twin state and shadow, topology navigation, cycle rejection, explicit relation create/remove, version updates and narrow layout')
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
