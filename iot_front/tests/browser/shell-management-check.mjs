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
 const storage={iot_token:auth.accessToken,iot_tenant:tenant,iot_role:'admin',iot_user:user,iot_permissions:'["*"]'} /* 声明 storage。 */
 await call('Page.addScriptToEvaluateOnNewDocument',{source:'for(const [k,v] of Object.entries('+JSON.stringify(storage)+'))localStorage.setItem(k,v);'}) /* 等待异步操作完成。 */
 await call('Page.navigate',{url:'http://127.0.0.1:5173'}) /* 等待异步操作完成。 */
 await until(()=>evaluate(`!!document.querySelector('.menu-item[aria-label="设备管理"]')`)) /* 等待异步操作完成。 */
 await evaluate(`(()=>{const b=[...document.querySelectorAll('.menu-group-toggle')].find(e=>e.textContent.trim()==='设备接入');b.click()})()`) /* 等待异步操作完成。 */
 if(await evaluate(`!!document.querySelector('.menu-item[aria-label="设备管理"]').getClientRects().length`))throw Error('菜单分组未折叠') /* 判断条件并选择处理分支。 */
 await evaluate(`(()=>{const b=[...document.querySelectorAll('.menu-group-toggle')].find(e=>e.textContent.trim()==='设备接入');b.click()})()`) /* 等待异步操作完成。 */
 const menus=await evaluate(`[...document.querySelectorAll('.menu-item')].map(e=>e.getAttribute('aria-label'))`) /* 声明 menus。 */
 for(const menu of menus){ /* 循环处理当前数据。 */
  await evaluate(`document.querySelector('.menu-item[aria-label="${menu}"]').click()`);await delay(900) /* 等待异步操作完成。 */
  await until(()=>evaluate(`!document.querySelector('.main-content .el-loading-mask')?.getClientRects().length`)) /* 等待异步操作完成。 */
  if(await evaluate(`document.querySelector('.main-content').innerText.length<5`))throw Error(menu+'页面空白') /* 判断条件并选择处理分支。 */
  const invisible=await evaluate(`[...document.querySelectorAll('.el-table button.el-button')].filter(e=>e.getClientRects().length).some(e=>getComputedStyle(e).borderTopColor==='rgba(0, 0, 0, 0)'||getComputedStyle(e).backgroundColor==='rgba(0, 0, 0, 0)')`) /* 声明 invisible。 */
  if(invisible)throw Error(menu+'操作仍显示成无边框文字') /* 判断条件并选择处理分支。 */
  console.log('菜单与按钮通过：'+menu) /* 执行当前语句并推进处理流程。 */
 } /* 结束当前表达式或代码块。 */
 // A controlled 45-record catalog tests real pagination without polluting the platform.
 await evaluate(`(()=>{const original=window.fetch;window.fetch=(input,options)=>{if(String(input)==='/api/v2/protocols')return Promise.resolve(new Response(JSON.stringify({items:Array.from({length:45},(_,i)=>({definition:{id:'page-fixture-'+(i+1),name:'分页验证协议 '+(i+1)},releases:[]}))}),{headers:{'Content-Type':'application/json'}}));return original(input,options)}})()`) /* 等待异步操作完成。 */
 await evaluate(`document.querySelector('.menu-item[aria-label="协议管理"]').click()`);await delay(700) /* 等待异步操作完成。 */
 if(await evaluate(`document.querySelectorAll('.el-table__body .el-table__row').length`)!==20)throw Error('协议首屏没有20条') /* 判断条件并选择处理分支。 */
 await evaluate(`[...document.querySelectorAll('.el-pager .number')].find(e=>e.textContent.trim()==='2').click()`);await delay(100) /* 等待异步操作完成。 */
 if(!await evaluate(`document.querySelector('.el-table__body').textContent.includes('page-fixture-21')`))throw Error('协议第二页错位') /* 判断条件并选择处理分支。 */
 await evaluate(`[...document.querySelectorAll('.el-pager .number')].find(e=>e.textContent.trim()==='3').click()`);await delay(100) /* 等待异步操作完成。 */
 if(await evaluate(`document.querySelectorAll('.el-table__body .el-table__row').length`)!==5)throw Error('协议末页条数错误') /* 判断条件并选择处理分支。 */
 await writeFile(dir+'/screenshots/protocol-pagination.png',Buffer.from((await call('Page.captureScreenshot',{format:'png'})).data,'base64')) /* 等待异步操作完成。 */
 console.log('通过：菜单分组折叠、全菜单操作按钮边框与背景、45条协议跨页及末页。分页数据为隔离浏览器内测试样本。') /* 执行当前语句并推进处理流程。 */
 if(errors.some(e=>e.error||e.path?.startsWith('/api/')&&e.status!==404))throw Error('存在页面或API异常') /* 判断条件并选择处理分支。 */
}finally{socket?.close();child.kill()} /* 结束当前表达式或代码块。 */
