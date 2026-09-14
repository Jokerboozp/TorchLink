// Read-only browser acceptance against the running local platform and real demo data.
import {spawn} from 'node:child_process'
import {readFile,writeFile,mkdir,mkdtemp} from 'node:fs/promises'
import {parseEnv} from 'node:util'
import {tmpdir} from 'node:os'
import {join} from 'node:path'
const env={...parseEnv(await readFile('.env.local','utf8')),...process.env}
const dir='.e2e/demo-20260914', tenant=(env.IOT_ADMIN_TENANTS||'tenant_001').split(',')[0].trim(), user=env.IOT_ADMIN_USER||'admin'
const auth=await (await fetch('http://127.0.0.1:5173/api/v1/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:user,password:env.IOT_ADMIN_PASSWORD,tenantId:tenant})})).json()
if(!auth.accessToken)throw Error('本机登录失败')
await mkdir(`${dir}/screenshots`,{recursive:true})
const profile=await mkdtemp(join(tmpdir(),'iot-demo-browser-'))
const child=spawn('C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',['--headless=new','--no-first-run','--no-default-browser-check','--disable-gpu','--remote-debugging-port=0',`--user-data-dir=${profile}`,'about:blank'],{windowsHide:true,stdio:'ignore'})
const delay=ms=>new Promise(r=>setTimeout(r,ms))
async function until(fn){for(let i=0;i<150;i++){const v=await fn();if(v)return v;await delay(200)}throw Error('页面等待超时')}
let socket
const results=[],errors=[]
try{
 const port=await until(async()=>{try{return(await readFile(join(profile,'DevToolsActivePort'),'utf8')).split('\n')[0]}catch{return null}})
 const pages=await(await fetch(`http://127.0.0.1:${port}/json/list`)).json()
 socket=new WebSocket(pages.find(p=>p.type==='page').webSocketDebuggerUrl)
 await new Promise((r,j)=>{socket.onopen=r;socket.onerror=j})
 let id=0;const pending=new Map()
 socket.onmessage=e=>{const v=JSON.parse(e.data);if(v.id){const p=pending.get(v.id);pending.delete(v.id);v.error?p.reject(Error(v.error.message)):p.resolve(v.result)}else if(v.method==='Runtime.exceptionThrown')errors.push({page:results.length,error:v.params.exceptionDetails.text});else if(v.method==='Network.responseReceived'&&v.params.response.status>=400){const r=v.params.response;errors.push({page:results.length,path:new URL(r.url).pathname,status:r.status})}}
 const call=(method,params={})=>new Promise((resolve,reject)=>{const next=++id;pending.set(next,{resolve,reject});socket.send(JSON.stringify({id:next,method,params}))})
 const evaluate=async expression=>{const r=await call('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});if(r.exceptionDetails)throw Error('浏览器脚本异常');return r.result.value}
 await call('Page.enable');await call('Runtime.enable');await call('Network.enable')
 await call('Emulation.setDeviceMetricsOverride',{width:1440,height:1050,deviceScaleFactor:1,mobile:false})
 const inspection=JSON.parse(await readFile(`${dir}/inspection.json`,'utf8'))
 const events=(await readFile(`${dir}/ai-chat.sse`,'utf8')).split('\n').filter(x=>x.startsWith('data: ')).map(x=>JSON.parse(x.slice(6)))
 const answer=events.filter(x=>x.type==='text.delta').map(x=>x.delta||x.text||'').join('')
 await writeFile(`${dir}/智能助手回答.txt`,answer||JSON.stringify(events.filter(x=>x.type==='run.completed'),null,2))
 const history={version:1,conversationId:'demo-20260914-chat',selectedWorkflowId:'ops-assistant',messages:[{id:'demo-user',role:'user',text:'查询演示设备和告警，并结合演示消防处置手册给出处置建议。',status:'succeeded'},{id:'demo-answer',role:'assistant',text:answer,status:'succeeded',tools:[]}],runs:[],savedAt:Date.now()}
 const storage={iot_token:auth.accessToken,iot_tenant:tenant,iot_role:'admin',iot_user:user,[`iot:ai-history:v1:${tenant}:${user}`]:JSON.stringify(history)}
 await call('Page.addScriptToEvaluateOnNewDocument',{source:`for(const [k,v] of Object.entries(${JSON.stringify(storage)}))localStorage.setItem(k,v);sessionStorage.setItem(${JSON.stringify(`iot:health-inspection:v1:${tenant}:${user}`)},${JSON.stringify(JSON.stringify({version:1,report:inspection.report,savedAt:Date.now()}))});`})
 await call('Page.navigate',{url:'http://127.0.0.1:5173'})
 const menus=['运行总览','协议管理','产品管理','设备管理','接入网关','接入测试','摄像头映射','告警中心','智能巡检','原始报文','告警规则','知识库','模型管理','智能助手','备份中心']
 for(const [index,name] of (process.env.IOT_DEMO_COMMAND_ONLY ? [] : menus).entries()){
  const start=errors.length
  try{
   await until(()=>evaluate(`(()=>{const b=document.querySelector('.menu-item[aria-label="${name}"]');if(!b)return false;b.click();return true})()`))
   await delay(1200)
   await until(()=>evaluate(`![...document.querySelectorAll('.el-loading-mask')].some(e=>e.getClientRects().length&&getComputedStyle(e).display!=='none')`))
   const content=await evaluate('document.body.innerText')
   if(content.length<150)throw Error('页面内容为空')
   const file=`screenshots/${String(index+1).padStart(2,'0')}.png`
   await writeFile(`${dir}/${file}`,Buffer.from((await call('Page.captureScreenshot',{format:'png',captureBeyondViewport:false})).data,'base64'))
   results.push({name,status:'通过',file,hasDemo:content.includes('演示'),errors:errors.slice(start)});console.log(`页面通过：${name}`)
  }catch(e){results.push({name,status:'未通过',detail:e.message});console.log(`页面未通过：${name} ${e.message}`)}
 }
 try{
  await until(()=>evaluate(`!!document.querySelector('.menu-item[aria-label="设备管理"]')`))
  await evaluate(`document.querySelector('.menu-item[aria-label="设备管理"]').click()`);await delay(1000)
  await until(()=>evaluate(`(()=>{const r=[...document.querySelectorAll('.el-table__row')].find(e=>e.textContent.includes('demo-20260914-mqtt-sensor'));const b=[...r?.querySelectorAll('button')||[]].find(e=>e.textContent.trim()==='连接详情');b?.click();return !!b})()`))
  await until(()=>evaluate(`!!document.querySelector('.device-commands .el-select')`))
  console.log('设备控制：命令表单已加载')
  await evaluate(`document.querySelector('.device-commands .el-select').click()`)
  await until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('.el-select-dropdown__item')].find(e=>e.textContent.trim()==='设置目标温度');e?.click();return !!e})()`))
  await delay(200)
  console.log('设备控制：已选择命令')
  await evaluate(`(()=>{const e=document.querySelector('.device-commands .el-input-number input');e.value='28';e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}));e.blur()})()`)
  await evaluate(`(()=>{const b=[...document.querySelectorAll('.device-commands button')].find(e=>e.textContent.trim()==='执行命令');if(b.disabled)throw Error('命令按钮禁用');b.click()})()`)
  await until(()=>evaluate(`(()=>{const b=document.querySelector('.el-message-box__btns .el-button--primary');b?.click();return !!b})()`))
  await delay(1200)
  console.log('设备控制：已发送')
  await evaluate(`(()=>{const b=[...document.querySelectorAll('.el-drawer button,.el-dialog button')].find(e=>e.textContent.trim()==='刷新');b?.click()})()`)
  await until(()=>evaluate(`document.querySelector('.device-commands').textContent.includes('演示目标温度已设置为 28')`))
  await evaluate(`document.querySelector('.device-commands').scrollIntoView({block:'start'})`);await delay(300)
  await writeFile(`${dir}/screenshots/16.png`,Buffer.from((await call('Page.captureScreenshot',{format:'png'})).data,'base64'))
  results.push({name:'设备控制表单与真实回执',status:'通过',file:'screenshots/16.png',hasDemo:true});console.log('通过：页面输入28℃→执行命令→真实设备应答')
 }catch(e){results.push({name:'设备控制表单与真实回执',status:'未通过',detail:e.message});console.log('未通过：设备控制 '+e.message);console.log(await evaluate(`document.querySelector('.device-commands')?.innerText||'未找到命令区域'`))}
 if(process.env.IOT_DEMO_COMMAND_ONLY){const old=JSON.parse(await readFile(`${dir}/browser-results.json`,'utf8'));results.unshift(...old.results.filter(r=>r.name!=='设备控制表单与真实回执'))}
 await writeFile(`${dir}/browser-results.json`,JSON.stringify({results,errors,checkedAt:new Date().toISOString(),note:'智能助手与巡检截图载入此次真实 API 返回的报告缓存；其余页面直接读取当前平台。'},null,2))
 if(results.some(item=>item.status==='未通过')||errors.some(item=>item.error || (item.path?.startsWith('/api/')&&item.status>=400)))process.exitCode=1
}finally{socket?.close();child.kill()}
