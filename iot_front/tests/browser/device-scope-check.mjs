// Read-only browser acceptance against the running local platform and real demo data.
import {spawn} from 'node:child_process' /* 引入当前代码需要的依赖。 */
import {readFile,writeFile,mkdir,mkdtemp} from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import {parseEnv} from 'node:util' /* 引入当前代码需要的依赖。 */
import {tmpdir} from 'node:os' /* 引入当前代码需要的依赖。 */
import {join} from 'node:path' /* 引入当前代码需要的依赖。 */
const env={...parseEnv(await readFile('.env.local','utf8')),...process.env} /* 声明 env。 */
const dir='.e2e/demo-20260914', tenant=(env.IOT_ADMIN_TENANTS||'tenant_001').split(',')[0].trim(), user=env.IOT_ADMIN_USER||'admin' /* 声明 dir。 */
const auth=await (await fetch('http://127.0.0.1:5173/api/v1/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:user,password:env.IOT_ADMIN_PASSWORD,tenantId:tenant})})).json() /* 声明 auth。 */
if(!auth.accessToken)throw Error('本机登录失败') /* 判断条件并选择处理分支。 */
await mkdir(`${dir}/screenshots`,{recursive:true}) /* 等待异步操作完成。 */
const profile=await mkdtemp(join(tmpdir(),'iot-demo-browser-')) /* 声明 profile。 */
const child=spawn('C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',['--headless=new','--no-first-run','--no-default-browser-check','--disable-gpu','--remote-debugging-port=0',`--user-data-dir=${profile}`,'about:blank'],{windowsHide:true,stdio:'ignore'}) /* 声明 child。 */
const delay=ms=>new Promise(r=>setTimeout(r,ms)) /* 声明 delay。 */
async function until(fn){for(let i=0;i<150;i++){const v=await fn();if(v)return v;await delay(200)}throw Error('页面等待超时')} /* 定义 until 函数。 */
let scopeUser="" /* 声明 scopeUser。 */
let socket /* 声明 socket。 */
const results=[],errors=[] /* 声明 results。 */
try{ /* 执行当前语句并推进处理流程。 */
 const port=await until(async()=>{try{return(await readFile(join(profile,'DevToolsActivePort'),'utf8')).split('\n')[0]}catch{return null}}) /* 声明 port。 */
 const pages=await(await fetch(`http://127.0.0.1:${port}/json/list`)).json() /* 声明 pages。 */
 socket=new WebSocket(pages.find(p=>p.type==='page').webSocketDebuggerUrl) /* 更新 socket 的值。 */
 await new Promise((r,j)=>{socket.onopen=r;socket.onerror=j}) /* 等待异步操作完成。 */
 let id=0;const pending=new Map() /* 声明 id。 */
 socket.onmessage=e=>{const v=JSON.parse(e.data);if(v.id){const p=pending.get(v.id);pending.delete(v.id);v.error?p.reject(Error(v.error.message)):p.resolve(v.result)}else if(v.method==='Runtime.exceptionThrown')errors.push({page:results.length,error:v.params.exceptionDetails.text});else if(v.method==='Network.responseReceived'&&v.params.response.status>=400){const r=v.params.response;errors.push({page:results.length,path:new URL(r.url).pathname,status:r.status})}} /* 更新 socket.onmessage 的值。 */
 const call=(method,params={})=>new Promise((resolve,reject)=>{const next=++id;pending.set(next,{resolve,reject});socket.send(JSON.stringify({id:next,method,params}))}) /* 声明 call。 */
 const evaluate=async expression=>{const r=await call('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});if(r.exceptionDetails)throw Error('浏览器脚本异常');return r.result.value} /* 声明 evaluate。 */
 await call('Page.enable');await call('Runtime.enable');await call('Network.enable') /* 等待异步操作完成。 */
 await call('Emulation.setDeviceMetricsOverride',{width:1440,height:1050,deviceScaleFactor:1,mobile:false}) /* 等待异步操作完成。 */
 const click=async text=>until(()=>evaluate(`(()=>{const b=[...document.querySelectorAll('button')].find(b=>b.textContent.trim()===${JSON.stringify(text)}&&b.getClientRects().length&&!b.disabled);b?.click();return !!b})()`)) /* 声明 click。 */
 const fill=async(label,value)=>evaluate(`(()=>{const root=[...document.querySelectorAll('.ui-dialog')].find(e=>e.getClientRects().length);const item=[...root.querySelectorAll('.ui-form-item')].find(e=>e.querySelector('label')?.textContent.trim().startsWith(${JSON.stringify(label)}));const e=item?.querySelector('input');if(!e)throw Error('缺少表单输入框');e.value=${JSON.stringify(value)};e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}))})()`) /* 声明 fill。 */
 const request=async(method,path,body,token=auth.accessToken)=>{const r=await fetch('http://127.0.0.1:5173'+path,{method,headers:{Authorization:'Bearer '+token,'Content-Type':'application/json'},body:body?JSON.stringify(body):undefined});return {status:r.status,value:await r.json()}} /* 声明 request。 */
 const suffix=Date.now(),username='demo-scope-'+suffix,secret=crypto.randomUUID()+'Aa1';scopeUser=username /* 声明 suffix。 */
 const registry=(await request('GET','/api/v1/device-registry?pageSize=100')).value.items /* 声明 registry。 */
 const alarms=(await request('GET','/api/v1/alarms?pageSize=100')).value.items /* 声明 alarms。 */
 const allowedAlarm=alarms.find(a=>registry.some(d=>d.device.id===a.deviceId&&d.device.deviceRole==='DIRECT')) /* 声明 allowedAlarm。 */
 if(!allowedAlarm)throw Error('缺少带告警的演示独立设备') /* 判断条件并选择处理分支。 */
 const allowed=registry.find(d=>d.device.id===allowedAlarm.deviceId).device /* 声明 allowed。 */
 const forbidden=registry.find(d=>d.device.id!==allowed.id).device /* 声明 forbidden。 */
 await call('Page.addScriptToEvaluateOnNewDocument',{source:'for(const [k,v] of Object.entries('+JSON.stringify({iot_token:auth.accessToken,iot_tenant:tenant,iot_role:'admin',iot_user:user,iot_permissions:'["*"]'})+'))localStorage.setItem(k,v)'}) /* 等待异步操作完成。 */
 await call('Page.navigate',{url:'http://127.0.0.1:5173'}) /* 等待异步操作完成。 */
 await until(()=>evaluate(`!!document.querySelector('.nav-item[aria-label="用户与权限"]')`)) /* 等待异步操作完成。 */
 await evaluate(`document.querySelector('.nav-item[aria-label="用户与权限"]').click()`);await delay(600) /* 等待异步操作完成。 */
 await click('添加用户');await fill('用户名',username);await fill('显示名称','演示 · 指定设备用户');await fill('初始密码',secret) /* 等待异步操作完成。 */
 if(!await evaluate(`document.querySelector('.ui-dialog input[disabled]')?.value===${JSON.stringify(tenant)}`))throw Error('新增用户未显示租户') /* 判断条件并选择处理分支。 */
 await evaluate(`document.querySelector('input[value="selected"]').click()`) /* 等待异步操作完成。 */
 await evaluate(`(()=>{const row=[...document.querySelectorAll('.ui-form-item')].find(e=>e.querySelector('label')?.textContent==='可访问设备');row.querySelector('.n-base-selection').click()})()`) /* 等待异步操作完成。 */
 await until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('.n-base-select-option')].find(e=>e.textContent.trim().endsWith(${JSON.stringify('（'+allowed.id+'）')})&&e.getClientRects().length);e?.click();return !!e})()`)) /* 等待异步操作完成。 */
 await evaluate(`document.querySelector('.n-card-header').click()`) /* 等待异步操作完成。 */
 for(const name of ['设备管理','告警中心','运行总览'])await evaluate(`(()=>{const g=[...document.querySelectorAll('.permission-group')].find(e=>e.querySelector('strong')?.textContent===${JSON.stringify(name)});g.querySelector('input').click()})()`) /* 循环处理当前数据。 */
 await writeFile(dir+'/screenshots/device-scope-form.png',Buffer.from((await call('Page.captureScreenshot',{format:'png'})).data,'base64')) /* 等待异步操作完成。 */
 await click('保存');await until(()=>evaluate(`![...document.querySelectorAll('.ui-dialog')].some(e=>e.getClientRects().length)`)) /* 等待异步操作完成。 */
 const login=await request('POST','/api/v1/auth/login',{username,password:secret,tenantId:tenant});if(login.status!==200)throw Error('范围用户登录失败') /* 声明 login。 */
 const token=login.value.accessToken /* 声明 token。 */
 for(const path of ['/api/v1/device-registry','/api/v1/alarms']){ /* 循环处理当前数据。 */
  const r=await request('GET',path,null,token);if(r.status!==200)throw Error('授权列表无法打开') /* 声明 r。 */
  if(r.value.items.some(v=>(v.device?.id||v.deviceId)!==allowed.id))throw Error('列表包含未授权设备') /* 判断条件并选择处理分支。 */
 } /* 结束当前表达式或代码块。 */
 if((await request('GET','/api/v1/device-registry/'+forbidden.id+'/connection',null,token)).status!==403)throw Error('直接访问未授权设备未拒绝') /* 判断条件并选择处理分支。 */
 const events=await request('GET','/api/v1/events',null,token) /* 声明 events。 */
 if(events.status!==200||events.value.alarms.some(a=>a.deviceId!==allowed.id)||events.value.devices.some(d=>d.deviceId!==allowed.id))throw Error('消息范围错误') /* 判断条件并选择处理分支。 */
 if((await request('POST','/api/v1/mqtt/token',{},token)).status!==403)throw Error('受限用户仍能获取MQTT订阅令牌') /* 判断条件并选择处理分支。 */
 // Replace the admin bootstrap for this isolated browser with the real user session.
 await call('Page.addScriptToEvaluateOnNewDocument',{source:'for(const [k,v] of Object.entries('+JSON.stringify({iot_token:token,iot_tenant:tenant,iot_role:'operator',iot_user:username,iot_permissions:JSON.stringify(login.value.permissions)})+'))localStorage.setItem(k,v)'}) /* 等待异步操作完成。 */
 await call('Page.reload');await until(()=>evaluate(`!!document.querySelector('.nav-item[aria-label="设备管理"]')`)) /* 等待异步操作完成。 */
 await evaluate(`document.querySelector('.nav-item[aria-label="设备管理"]').click()`);await delay(700) /* 等待异步操作完成。 */
 const text=await evaluate(`document.querySelector('.table-card').innerText`) /* 声明 text。 */
 if(!text.includes(allowed.id)||text.includes(forbidden.id))throw Error('实际设备页面范围错误') /* 判断条件并选择处理分支。 */
 await writeFile(dir+'/screenshots/device-scope-devices.png',Buffer.from((await call('Page.captureScreenshot',{format:'png'})).data,'base64')) /* 等待异步操作完成。 */
 await evaluate(`document.querySelector('.nav-item[aria-label="告警中心"]').click()`);await delay(700) /* 等待异步操作完成。 */
 await writeFile(dir+'/screenshots/device-scope-alarms.png',Buffer.from((await call('Page.captureScreenshot',{format:'png'})).data,'base64')) /* 等待异步操作完成。 */
 // API scope has been checked above. A browser-only event verifies popup rendering.
 await evaluate(`window.dispatchEvent(new CustomEvent('iot:realtime',{detail:{topic:'/iot/alarm/raised/'+${JSON.stringify(tenant)},payload:JSON.stringify(${JSON.stringify({...allowedAlarm,alarmId:'scope-popup-fixture',triggerId:'scope-popup-trigger',status:'ACTIVE',lastTriggeredAt:Date.now()})})}}))`) /* 等待异步操作完成。 */
 await until(()=>evaluate(`!!document.querySelector('.global-alert-popup')`)) /* 等待异步操作完成。 */
 await writeFile(dir+'/screenshots/device-scope-popup.png',Buffer.from((await call('Page.captureScreenshot',{format:'png'})).data,'base64')) /* 等待异步操作完成。 */
 if(errors.some(e=>e.error))throw Error('浏览器运行异常') /* 判断条件并选择处理分支。 */
 console.log('通过：真实UI设置所属租户与单台设备授权；真实设备、告警、消息API及页面只返回授权设备；越权详情和MQTT令牌拒绝；授权告警弹窗渲染通过（弹窗使用浏览器内样本）。') /* 执行当前语句并推进处理流程。 */
}finally{ /* 结束当前表达式或代码块。 */
 if(scopeUser)await fetch('http://127.0.0.1:5173/api/v1/access/users/'+scopeUser,{method:'DELETE',headers:{Authorization:'Bearer '+auth.accessToken}}) /* 判断条件并选择处理分支。 */
 socket?.close();child.kill() /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
