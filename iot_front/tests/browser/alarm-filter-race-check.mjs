// 合成数据浏览器回归：旧筛选请求不得覆盖较新的告警列表。
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtemp, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const browser = process.env.IOT_TEST_BROWSER || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'
const origin = process.env.IOT_UI_PREVIEW_ORIGIN || 'http://127.0.0.1:4173'
const profile = await mkdtemp(join(tmpdir(), 'iot-alarm-filter-race-'))
const child = spawn(browser, ['--headless=new','--no-first-run','--no-default-browser-check','--disable-gpu','--remote-debugging-port=0',`--user-data-dir=${profile}`,'about:blank'], { windowsHide:true, stdio:'ignore' })
const delay = ms => new Promise(resolve => setTimeout(resolve, ms))
async function until(check, label) { for(let i=0;i<80;i++){const value=await check();if(value)return value;await delay(100)}throw new Error(`等待超时：${label}`) }
let socket
try {
  const port = await until(async()=>{try{return (await readFile(join(profile,'DevToolsActivePort'),'utf8')).split('\n')[0]}catch{return null}}, '浏览器调试端口')
  const targets = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()
  socket = new WebSocket(targets.find(target=>target.type==='page').webSocketDebuggerUrl)
  await new Promise((resolve,reject)=>{socket.onopen=resolve;socket.onerror=reject})
  let id=0
  const pending=new Map()
  const failures=[]
  socket.onmessage=event=>{const message=JSON.parse(event.data);if(message.method==='Runtime.exceptionThrown')failures.push(message.params.exceptionDetails?.exception?.description||message.params.exceptionDetails?.text);if(!message.id)return;const entry=pending.get(message.id);pending.delete(message.id);message.error?entry.reject(new Error(message.error.message)):entry.resolve(message.result)}
  const call=(method,params={})=>new Promise((resolve,reject)=>{const next=++id;pending.set(next,{resolve,reject});socket.send(JSON.stringify({id:next,method,params}))})
  const evaluate=async expression=>{const result=await call('Runtime.evaluate',{expression,returnByValue:true,awaitPromise:true});if(result.exceptionDetails)throw new Error(result.exceptionDetails.exception?.description||result.exceptionDetails.text);return result.result.value}
  await call('Page.enable')
  await call('Runtime.enable')
  await call('Page.addScriptToEvaluateOnNewDocument',{source:`
    localStorage.clear();
    window.__alarmQueries=[];
    const base=window.fetch.bind(window);
    window.fetch=(input,options)=>{
      const path=String(input);
      if(path==='/api/v1/auth/login')return Promise.resolve(new Response(JSON.stringify({accessToken:'fixture',tenantId:'fixture',role:'admin',permissions:['*']}),{headers:{'Content-Type':'application/json'}}));
      if(path==='/api/v1/auth/me')return Promise.resolve(new Response(JSON.stringify({tenantId:'fixture',role:'admin',permissions:['*']}),{headers:{'Content-Type':'application/json'}}));
      if(path==='/api/v1/events')return Promise.resolve(new Response(JSON.stringify({permissions:['*'],alarms:[],devices:[]}),{headers:{'Content-Type':'application/json'}}));
      if(path.startsWith('/api/v1/alarms?')){
        const deviceId=new URL(path,location.origin).searchParams.get('deviceId')||'';
        window.__alarmQueries.push(deviceId);
        const body={items:deviceId?[{alarmId:'alarm-'+deviceId,deviceId,deviceName:deviceId+'-设备',alarmType:'FIRE',alarmLevel:'HIGH',status:'ACTIVE',source:'DEVICE',lastTriggeredAt:Date.now()}]:[],total:deviceId?1:0};
        return new Promise(resolve=>setTimeout(()=>resolve(new Response(JSON.stringify(body),{headers:{'Content-Type':'application/json'}})),deviceId==='slow'?500:10));
      }
      if(path.startsWith('/api/'))return Promise.resolve(new Response(JSON.stringify({items:[],total:0}),{headers:{'Content-Type':'application/json'}}));
      return base(input,options);
    };
  `})
  await call('Page.navigate',{url:origin})
  await until(()=>evaluate("Boolean(document.querySelector('.login-form input[type=password]'))"),'登录页')
  await evaluate("(()=>{const input=document.querySelector('.login-form input[type=password]');input.value='fixture';input.dispatchEvent(new Event('input',{bubbles:true}));document.querySelector('.login-form button[type=submit]').click()})()")
  await until(()=>evaluate("Boolean(document.querySelector('.nav-item[aria-label=\"告警中心\"]'))"),'菜单')
  await evaluate("document.querySelector('.nav-item[aria-label=\"告警中心\"]').click()")
  await until(()=>evaluate("document.querySelector('.page-context h1')?.textContent==='告警中心'"),'告警页面')
  await until(()=>evaluate("Boolean(document.querySelector('input[placeholder=\"按设备标识筛选\"]'))"),'告警筛选框')
  const filter=async value=>evaluate(`(()=>{const input=document.querySelector('input[placeholder="按设备标识筛选"]');input.value=${JSON.stringify(value)};input.dispatchEvent(new Event('input',{bubbles:true}));input.dispatchEvent(new KeyboardEvent('keyup',{key:'Enter',bubbles:true}));return true})()`)
  await filter('slow')
  await until(()=>evaluate("window.__alarmQueries.includes('slow')"),'慢筛选请求')
  await filter('fast')
  await until(()=>evaluate("window.__alarmQueries.includes('fast')"),'快筛选请求')
  await until(()=>evaluate("document.querySelector('.n-data-table-tbody')?.innerText.includes('fast-设备')"),'快筛选结果')
  await delay(650)
  const tableText=await evaluate("document.querySelector('.n-data-table-tbody')?.innerText||''")
  assert.match(tableText,/fast-设备/,'旧请求返回后应保留最新筛选结果')
  assert.doesNotMatch(tableText,/slow-设备/,'旧请求不能覆盖当前列表')
  assert.deepEqual(failures,[],'浏览器不应出现未处理的脚本异常')
  console.log('PASS: 告警列表按最新筛选响应更新')
} finally {
  socket?.close()
  const exited=new Promise(resolve=>{if(child.exitCode!==null||child.signalCode!==null)resolve();else child.once('exit',resolve)})
  child.kill()
  const force=setTimeout(()=>child.kill('SIGKILL'),3000)
  force.unref()
  await exited
  clearTimeout(force)
  await rm(profile,{recursive:true,force:true,maxRetries:5,retryDelay:100})
}
