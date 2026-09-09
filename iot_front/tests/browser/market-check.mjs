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
  await evaluate(`localStorage.setItem('iot_user','browser-publisher')`)
  await click('设备接入')
  const openMarket=async()=>until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('[role=tab]')].find(e=>e.textContent.trim()==='组织发布');if(!e)return false;e.click();return true})()`))
  await openMarket()
  await until(()=>evaluate(`document.body.textContent.includes('发布组织：验证组织')`))
  await until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('.el-collapse-item__header')].find(e=>e.textContent.includes('提交协议版本'));if(!e)return false;e.click();return true})()`))
  await until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('.el-form-item')].find(e=>e.querySelector('label')?.textContent.trim()==='待提交版本')?.querySelector('.el-select__wrapper');if(!e)return false;e.click();return true})()`))
  await until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('.el-select-dropdown__item')].find(e=>e.textContent.trim()==='market-demo@1.1.0'&&e.getClientRects().length);if(!e)return false;e.click();return true})()`))
  await fill('目录名称','浏览器审核版本')
  await fill('组织许可','组织内部使用')
  await fill('标签','消防,验证')
  await click('提交独立审核')
  await until(()=>evaluate(`document.querySelector('.el-message-box')?.textContent.includes('提交组织审核')`))
  await evaluate(`document.querySelector('.el-message-box__btns .el-button--primary').click()`)
  await until(()=>evaluate(`document.body.textContent.includes('等待其他管理员审核')`))
  assert.equal(await evaluate(`[...document.querySelectorAll('button')].some(e=>e.textContent.trim()==='审核上架'&&e.getClientRects().length)`),false)
  // Switch to another authenticated account; the API enforces this separately.
  await evaluate(`localStorage.setItem('iot_token',${JSON.stringify(process.env.IOT_TEST_REVIEW_TOKEN)});localStorage.setItem('iot_user','browser-reviewer')`)
  await click('刷新发布记录')
  await until(()=>evaluate(`[...document.querySelectorAll('button')].some(e=>e.textContent.trim()==='审核上架'&&e.getClientRects().length)`))
  await evaluate(`(()=>{const original=window.fetch;window.__marketReviews=[];window.fetch=async(...args)=>{const response=await original(...args);if(String(args[0]).endsWith('/review'))window.__marketReviews.push({status:response.status,text:await response.clone().text()});return response}})()`)
  await click('审核上架')
  await until(()=>evaluate(`[...document.querySelectorAll('.el-message-box__input input')].some(e=>e.getClientRects().length&&e.closest('.el-message-box').textContent.includes('审核并上架'))`))
  await evaluate(`(()=>{const e=[...document.querySelectorAll('.el-message-box__input input')].find(e=>e.getClientRects().length&&e.closest('.el-message-box').textContent.includes('审核并上架'));if(!e)throw new Error('visible review input missing');e.value='已核实源码、许可和样例';e.dispatchEvent(new Event('input',{bubbles:true}))})()`)
  await evaluate(`(()=>{const box=[...document.querySelectorAll('.el-message-box')].find(e=>e.getClientRects().length&&e.textContent.includes('审核并上架'));if(!box)throw new Error('review dialog missing');box.querySelector('.el-message-box__btns .el-button--primary').click()})()`)
  await until(async()=>{const state=await evaluate(`({reviews:window.__marketReviews,dialog:document.querySelector('.el-message-box')?.textContent,accepted:[...document.querySelectorAll('.el-table__row')].some(e=>e.textContent.includes('1.1.0')&&e.textContent.includes('已上架'))})`);if(state.reviews.some(r=>r.status!==200))throw new Error(JSON.stringify(state));return state.accepted})
  await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true})
  assert.equal(await evaluate(`document.body.textContent.includes('readerTokenHashes')||document.body.textContent.includes('privateKeyFile')`),false)
  console.log('PASS: source version submission, self-review unavailable, distinct authenticated reviewer approval, actual signed publication and narrow view')
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
