import { spawn } from 'node:child_process'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import assert from 'node:assert/strict'

// 由 internal/httpapi/device_onboarding_browser_test.go 启动：隔离的 Go API、真实浏览器、合成设备上报。
const profile = await mkdtemp(join(tmpdir(), 'iot-device-flow-'))
const child = spawn(process.env.IOT_TEST_BROWSER, ['--headless=new','--no-first-run','--no-default-browser-check','--disable-gpu','--remote-debugging-port=0',`--user-data-dir=${profile}`,'about:blank'], {windowsHide:true,stdio:'ignore'})
let socket
const delay = ms => new Promise(resolve => setTimeout(resolve,ms))
async function until(fn, note, tries = 120) { for(let i=0;i<tries;i++){const value=await fn();if(value)return value;await delay(100)}throw new Error(`Browser condition timed out: ${note}`) }
try {
  const port = await until(async()=>{try{return (await readFile(join(profile,'DevToolsActivePort'),'utf8')).split('\n')[0]}catch{return null}},'Chrome debugger')
  const pages = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()
  socket = new WebSocket(pages.find(p=>p.type==='page').webSocketDebuggerUrl)
  await new Promise((resolve,reject)=>{socket.onopen=resolve;socket.onerror=reject})
  let id=0;const pending=new Map()
  socket.onmessage=event=>{const value=JSON.parse(event.data);if(value.id){const p=pending.get(value.id);pending.delete(value.id);value.error?p.reject(new Error(value.error.message)):p.resolve(value.result)}}
  const call=(method,params={})=>new Promise((resolve,reject)=>{const next=++id;pending.set(next,{resolve,reject});socket.send(JSON.stringify({id:next,method,params}))})
  const evaluate=async expression=>{const r=await call('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});if(r.exceptionDetails)throw new Error(r.exceptionDetails.text+' '+JSON.stringify(r.exceptionDetails.exception));return r.result.value}
  const click=async text=>until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('button,label')].find(e=>(e.getAttribute('aria-label')===${JSON.stringify(text)}||e.textContent.trim()===${JSON.stringify(text)})&&e.getClientRects().length&&!e.disabled&&!e.classList.contains('n-radio-button--disabled'));if(!e)return false;e.click();return true})()`),`click: ${text}`)
  const fill=async(label,value)=>until(()=>evaluate(`(()=>{const item=[...document.querySelectorAll('.ui-form-item,.n-form-item')].find(e=>e.querySelector('.n-form-item-label,.ui-form-item__label,label')?.textContent.trim().startsWith(${JSON.stringify(label)}));const input=item?.querySelector('input');if(!input)return false;input.value=${JSON.stringify(value)};input.dispatchEvent(new Event('input',{bubbles:true}));return true})()`),`input: ${label}`)
  const select=async(label,match)=>{await until(()=>evaluate(`(()=>{const item=[...document.querySelectorAll('.ui-form-item,.n-form-item')].find(e=>e.querySelector('.n-form-item-label,.ui-form-item__label,label')?.textContent.trim().startsWith(${JSON.stringify(label)}));const control=item?.querySelector('.n-base-selection');if(!control)return false;control.click();return true})()`),`select: ${label}`);await until(()=>evaluate(`(()=>{const option=[...document.querySelectorAll('.n-base-select-option')].find(e=>e.textContent.includes(${JSON.stringify(match)})&&e.getClientRects().length);if(!option)return false;option.click();return true})()`),`option: ${match}`)}
  const text=()=>evaluate(`document.querySelector('.onboarding')?.innerText || ''`)
  const waitText=async(value,note,tries=120)=>{let last='';try{return await until(async()=>(last=await text()).includes(value),note,tries)}catch(error){throw new Error(`${error.message}\n--- page text ---\n${last.slice(0,1500)}`)}}
  const capture=async name=>{await evaluate('new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))');assert.ok(await evaluate('document.documentElement.scrollWidth<=innerWidth+2'),`${name} horizontal overflow`);if(!name.endsWith('-bottom'))assert.ok(await evaluate("(()=>{const heading=document.querySelector('.onboarding__card .onboarding__title'),content=document.querySelector('.app-content');if(!heading||!content)return false;const h=heading.getBoundingClientRect(),c=content.getBoundingClientRect();return h.top>=c.top-1&&h.top<c.bottom})()"),`${name} step heading not visible`);const shot=await call('Page.captureScreenshot',{format:'png',captureBeyondViewport:true});await writeFile(join(tmpdir(),`iot-onboarding-${name}.png`),Buffer.from(shot.data,'base64'))}
  await call('Page.enable')
  await call('Emulation.setDeviceMetricsOverride',{width:1440,height:900,deviceScaleFactor:1,mobile:false})
  await call('Page.addScriptToEvaluateOnNewDocument',{source:`localStorage.setItem('iot_token',${JSON.stringify(process.env.IOT_TEST_TOKEN)});localStorage.setItem('iot_tenant','tenant');localStorage.setItem('iot_role','admin');localStorage.setItem('iot_user','browser-test');`})
  await call('Page.navigate',{url:process.env.IOT_TEST_ORIGIN})
  await click('设备管理')
  await click('添加设备')
  await waitText('这台设备是什么型号','wizard start')
  await capture('desktop-start')

  // 新型号：模板在保存设备的同一请求中创建。
  await click('新型号')
  await fill('模板名称','浏览器新型号')
  await select('通信协议','标准设备上报')
  await click('HTTP')
  await waitText('标准 MQTT / HTTP 上报','standard preflight')
  await waitText('平台对外 HTTP 地址已配置','address check')
  await capture('desktop-new-template')
  await click('下一步')
  await waitText('设备与连接','device step')
  await fill('设备名称','浏览器现场设备')
  await click('使用平台编号')
  await capture('desktop-device')
  await click('保存并生成接入信息')
  await waitText('设备密钥只显示这一次','credential shown once')
  await waitText('https://devices.example.test/api/v1/device-ingest/standard/tenant/','device-side address')
  await waitText('等待设备本次上报','waiting diagnosis')
  assert.equal(await evaluate(`Object.values(localStorage).some(v=>typeof v==='string'&&v.includes('ds_'))`),false,'secret persisted in browser storage')
  await capture('desktop-verify-waiting')

  // 用页面给出的设备端信息模拟现场上报，诊断须由后端确认解析成功。
  const field = await evaluate(`(()=>{const rows=[...document.querySelectorAll('.onboarding__kv')].map(row=>[row.querySelector('span')?.innerText,row.querySelector('code')?.innerText]);const get=name=>rows.find(([key])=>key===name)?.[1];return {url:get('上报地址'),key:get('AccessKey'),secret:get('Secret')}})()`)
  assert.ok(field.url && field.key && field.secret?.startsWith('ds_'), 'device-side configuration incomplete')
  const report = await fetch(field.url.replace('https://devices.example.test', process.env.IOT_TEST_ORIGIN), {method:'POST',headers:{'Content-Type':'application/json','X-Device-Key':field.key,'X-Device-Secret':field.secret},body:JSON.stringify({version:'1.0',id:'browser-1',timestamp:Date.now(),data:{temperature:26.5}})})
  assert.equal(report.status,202,'standard device report rejected')
  await waitText('本次解析成功，等待下一次上报','backend diagnosis after the field report',160)
  await waitText('temperature','parsed values')
  await capture('desktop-verify-parsed')

  await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true})
  await evaluate('new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve)))')
  assert.ok(await evaluate(`document.querySelector('.onboarding').getBoundingClientRect().width<=390`),'narrow wizard overflow')
  await capture('mobile-verify')
  await evaluate("document.querySelector('.app-content').scrollTop=document.querySelector('.app-content').scrollHeight")
  await capture('mobile-verify-bottom')

  // 刷新后恢复验证步骤，但不能再显示密钥。
  await call('Page.navigate',{url:process.env.IOT_TEST_ORIGIN})
  await click('设备管理')
  await click('添加设备')
  await waitText('现场配置与验证','draft recovery')
  const recovered = await text()
  assert.ok(recovered.includes('浏览器现场设备') && !recovered.includes('设备密钥只显示这一次') && recovered.includes('设备密钥只在首次创建时显示'),'recovered verification step')
  await click('完成')
  await until(()=>evaluate(`(document.querySelector('.app-content')?.innerText||'').includes('浏览器现场设备')`),'device list after finishing')

  // 已有型号：第二台设备选择 MQTT 通道。
  await click('添加设备')
  await select('设备模板','浏览器新型号')
  await waitText('标准 MQTT / HTTP 上报','existing template preflight')
  await capture('mobile-existing-template')
  await click('下一步')
  await fill('设备名称','同型号第二台设备')
  await fill('设备编号','site-device-02')
  await click('MQTT')
  await capture('mobile-device')
  await click('保存并生成接入信息')
  await waitText('mqtts://devices.example.test:8883','MQTT device-side information')
  await waitText('site-device-02','second device')
  await capture('mobile-existing-verify')
  await click('完成')

  // 设备列表由服务端筛选。
  await until(()=>evaluate(`Boolean(document.querySelector('input[aria-label="搜索设备"]'))`),'device list')
  await evaluate(`(()=>{const input=document.querySelector('input[aria-label="搜索设备"]');input.value='site-device';input.dispatchEvent(new Event('input',{bubbles:true}))})()`)
  await until(()=>evaluate(`(()=>{const text=document.querySelector('.app-content')?.innerText||'';return text.includes('site-device-02')&&!text.includes('浏览器现场设备')})()`),'server-side keyword filter')

  // 新型号草稿在离开向导后保留。
  await click('添加设备')
  await click('新型号')
  await fill('模板名称','等待厂家协议的型号')
  await click('前往协议页面')
  await until(()=>evaluate(`document.body.innerText.includes('设备通信协议') && !document.querySelector('.onboarding')`),'protocol page navigation')
  await click('设备管理')
  await click('添加设备')
  await until(()=>evaluate(`document.querySelector('input[aria-label="模板名称"]')?.value==='等待厂家协议的型号'`),'return to incomplete draft')
  await capture('mobile-new-template-draft')
  await click('返回设备列表')

  // 模板详情中查看协议版本与接入点。
  await call('Emulation.setDeviceMetricsOverride',{width:1440,height:900,deviceScaleFactor:1,mobile:false})
  await click('设备模板')
  await until(()=>evaluate(`[...document.querySelectorAll('.product-name')].some(e=>e.textContent.includes('浏览器新型号'))`),'template list')
  await evaluate(`[...document.querySelectorAll('.product-name')].find(e=>e.textContent.includes('浏览器新型号')).click()`)
  await until(()=>evaluate(`(document.querySelector('.product-detail')?.innerText||'').includes('模板标识')`),'template detail')
  await evaluate(`[...document.querySelectorAll('.product-detail .n-tabs-tab')].find(e=>e.textContent.includes('接入点')).click()`)
  await until(()=>evaluate(`(document.querySelector('.product-detail')?.innerText||'').includes('接入点 · 0 个')`),'template access points')
  const shot=await call('Page.captureScreenshot',{format:'png'});await writeFile(join(tmpdir(),'iot-onboarding-desktop-template-access.png'),Buffer.from(shot.data,'base64'))
  console.log('PASS: isolated Go API and real browser — new and existing templates, one-time secret, field report confirmed by the backend diagnosis, MQTT information, server-side filter, draft recovery, template access points and 390px layout')
} finally {
  socket?.close()
  child.kill()
  try { await rm(profile,{recursive:true,force:true,maxRetries:10,retryDelay:200}) } catch { /* Browser teardown must not hide the assertion. */ }
}
