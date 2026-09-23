// 合成数据浏览器回归：报文详情应能在解析结果与原文之间切换并关闭。
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { mkdtemp, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const browser = process.env.IOT_TEST_BROWSER || 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'
const origin = process.env.IOT_UI_PREVIEW_ORIGIN || 'http://127.0.0.1:4173'
const profile = await mkdtemp(join(tmpdir(), 'iot-raw-detail-'))
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
  const clickVisible=async(selector,label)=>{
    await until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll(${JSON.stringify(selector)})].find(e=>e.getClientRects().length&&e.textContent.trim().includes(${JSON.stringify(label)}));if(!e)return false;e.scrollIntoView({behavior:'instant',block:'nearest'});return true})()`),label)
    await delay(70)
    const point=await evaluate(`(()=>{const e=[...document.querySelectorAll(${JSON.stringify(selector)})].find(e=>e.getClientRects().length&&e.textContent.trim().includes(${JSON.stringify(label)}));const r=e.getBoundingClientRect(),x=r.left+r.width/2,y=r.top+r.height/2;return {x,y,hit:document.elementFromPoint(x,y)?.closest(${JSON.stringify(selector)})===e}})()`)
    assert.ok(point.hit,`点击目标被遮挡：${label}`)
    await call('Input.dispatchMouseEvent',{type:'mousePressed',x:point.x,y:point.y,button:'left',clickCount:1})
    await call('Input.dispatchMouseEvent',{type:'mouseReleased',x:point.x,y:point.y,button:'left',clickCount:1})
  }
  await call('Page.enable')
  await call('Runtime.enable')
  await call('Emulation.setDeviceMetricsOverride',{width:1440,height:900,deviceScaleFactor:1,mobile:false})
  await call('Page.addScriptToEvaluateOnNewDocument',{source:`
    localStorage.clear();
    const base=window.fetch.bind(window);
    window.fetch=(input,options)=>{
      const path=String(input);
      const body=path==='/api/v1/auth/login' ? {accessToken:'fixture',tenantId:'fixture',role:'admin',permissions:['*']}
        : path==='/api/v1/auth/me' ? {tenantId:'fixture',role:'admin',permissions:['*']}
        : path==='/api/v1/events' ? {permissions:['*'],alarms:[],devices:[]}
        : path.startsWith('/api/v1/raw-messages?') ? {items:[{messageId:'raw-demo',receivedAt:Date.now(),productId:'product-demo',deviceId:'device-demo',protocol:'MQTT',parsed:true,parsedMessageType:'PROPERTY_REPORT',payloadSize:20}],total:1}
        : path==='/api/v1/raw-messages/raw-demo' ? {parseStatus:'PARSED',message:{messageId:'raw-demo',payload:'RAW_MARKER',diagnostics:Array.from({length:90},(_,index)=>'第 '+(index+1)+' 条报文记录')},standardMessage:{messageType:'PROPERTY_REPORT',properties:{value:'PARSED_MARKER'}}}
        : null;
      return body ? Promise.resolve(new Response(JSON.stringify(body),{headers:{'Content-Type':'application/json'}})) : base(input,options);
    };
  `})
  await call('Page.navigate',{url:origin})
  await until(()=>evaluate("Boolean(document.querySelector('.login-form input[type=password]'))"),'登录页')
  await evaluate("(()=>{const input=document.querySelector('.login-form input[type=password]');input.value='fixture';input.dispatchEvent(new Event('input',{bubbles:true}));document.querySelector('.login-form button[type=submit]').click()})()")
  await until(()=>evaluate("Boolean(document.querySelector('.menu-item[aria-label=\"原始报文\"]'))"),'菜单')
  await clickVisible('.menu-item','原始报文')
  await until(()=>evaluate("document.querySelector('.page-context h1')?.textContent==='原始报文'"),'报文页面')
  await clickVisible('.n-data-table-tbody button','详情')
  await until(()=>evaluate("Boolean(document.querySelector('.n-modal .n-tabs'))"),'详情弹窗')
  const visibleText=()=>evaluate("[...document.querySelectorAll('.n-modal pre')].filter(e=>e.getClientRects().length).map(e=>e.innerText).join(' ')")
  assert.match(await visibleText(),/PARSED_MARKER/,'默认应展示解析结果')
  await clickVisible('.n-modal .n-tabs-tab','原始报文')
  await until(async()=>/RAW_MARKER/.test(await visibleText()),'原始报文内容')
  await clickVisible('.n-modal .n-tabs-tab','标准解析结果')
  const parsedAgain=await until(async()=>{const value=await visibleText();return /PARSED_MARKER/.test(value)?value:null},'重新显示解析结果')
  assert.doesNotMatch(parsedAgain,/RAW_MARKER/,'切回解析结果后原文不应仍占据内容区')
  await clickVisible('.n-modal .n-card__footer button','关闭')
  await until(()=>evaluate("![...document.querySelectorAll('.n-modal')].some(e=>e.getClientRects().length)"),'关闭详情弹窗')
  await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true})
  await clickVisible('.n-data-table-tbody button','详情')
  await until(async()=>/PARSED_MARKER/.test(await visibleText()),'再次打开时恢复解析结果')
  await clickVisible('.n-modal .n-tabs-tab','原始报文')
  await until(async()=>/RAW_MARKER/.test(await visibleText()),'再次查看原始报文')
  assert.ok(await evaluate("document.documentElement.scrollWidth<=innerWidth+2"),'手机视图不应横向溢出')
  await clickVisible('.n-modal .n-card__footer button','关闭')
  await until(()=>evaluate("![...document.querySelectorAll('.n-modal')].some(e=>e.getClientRects().length)"),'从原始报文关闭详情')
  assert.deepEqual(failures,[],'浏览器不应出现未处理的脚本异常')
  console.log('PASS: 原始报文与解析结果双向切换，关闭详情生效')
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
