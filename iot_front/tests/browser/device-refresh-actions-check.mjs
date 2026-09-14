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

 const storage={iot_token:auth.accessToken,iot_tenant:tenant,iot_role:'admin',iot_user:user}
 await call('Page.addScriptToEvaluateOnNewDocument',{source:'for(const [k,v] of Object.entries('+JSON.stringify(storage)+'))localStorage.setItem(k,v);'})
 await call('Page.navigate',{url:'http://127.0.0.1:5173'})
 await until(()=>evaluate(`!!document.querySelector('.menu-item[aria-label="设备管理"]')`))
 await evaluate(`document.querySelector('.menu-item[aria-label="设备管理"]').click()`);await delay(1200)
 await evaluate(`(()=>{window.__registryRequests=0;const original=window.fetch;window.fetch=(...args)=>{if(String(args[0]).includes('/device-registry'))window.__registryRequests++;return original(...args)};for(let i=0;i<20;i++)window.dispatchEvent(new CustomEvent('iot:realtime',{detail:{topic:'device.state'}}))})()`)
 await delay(300)
 if(await evaluate('window.__registryRequests')!==0)throw Error('实时消息触发整表重载')
 if(!await evaluate(`document.body.innerText.includes('有新数据')`))throw Error('未提示新数据')
 await evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='刷新设备').click()`);await delay(600)
 if(await evaluate('window.__registryRequests')===0)throw Error('手动刷新失效')
 console.log('通过：20条实时事件无整表重载，更新提示与手动刷新正常')
 if(await evaluate(`document.querySelector('.table-card .el-table__body')?.innerText.includes('demo-20260914-child')`))throw Error('独立设备页混入子设备')
 if(await evaluate(`document.querySelector('.table-card .el-table__body')?.innerText.includes('demo-20260914-parent')`))throw Error('独立设备页混入主设备')
 await evaluate(`document.querySelector('#tab-children').click()`);await delay(250)
 if(!await evaluate(`document.querySelector('.table-card .el-table__body')?.innerText.includes('demo-20260914-child')`))throw Error('子设备未出现在独立标签页')
 if(await evaluate(`document.querySelector('.table-card .el-table__body')?.innerText.includes('demo-20260914-mqtt-sensor')`))throw Error('子设备页混入直接设备')
 await writeFile(dir+'/screenshots/child-devices.png',Buffer.from((await call('Page.captureScreenshot',{format:'png'})).data,'base64'))
 await evaluate(`document.querySelector('.device-filters .el-select').click()`)
 await until(()=>evaluate(`(()=>{const e=[...document.querySelectorAll('.el-select-dropdown__item')].find(e=>e.textContent.trim()==='摄像机');e?.click();return !!e})()`));await delay(200)
 if(await evaluate(`!!document.querySelector('.table-card .el-table__body .el-table__row')`))throw Error('设备类型筛选没有排除非摄像机子设备')
 await evaluate(`[...document.querySelectorAll('button')].find(b=>b.textContent.trim()==='重置筛选').click()`);await delay(200)
 if(!await evaluate(`document.querySelector('.table-card .el-table__body')?.innerText.includes('demo-20260914-child')`))throw Error('重置类型筛选失效')
 await evaluate(`document.querySelector('#tab-main').click()`);await delay(150)
 if(!await evaluate(`document.querySelector('.table-card .el-table__body')?.innerText.includes('demo-20260914-parent')`))throw Error('主设备页缺少网关设备')
 if(await evaluate(`document.querySelector('.table-card .el-table__body')?.innerText.includes('demo-20260914-mqtt-sensor')`))throw Error('主设备页混入独立设备')
 console.log('通过：独立设备、主设备、子设备三个标签页互不混合，类型筛选及重置正常')
 await evaluate(`document.querySelector('.menu-item[aria-label="协议管理"]').click()`);await delay(1000)
 const groups=await evaluate(`[...document.querySelectorAll('.release-buttons')].map(g=>[...g.querySelectorAll('button')].map(b=>b.textContent.trim()).join(','))`)
 if(!groups.length||groups.some(g=>g!=='解析测试,下载源码,下载制品,发布'))throw Error('协议操作项不统一')
 const buttons=await evaluate(`[...document.querySelectorAll('.release-buttons button')].map(b=>({text:b.textContent.trim(),link:b.classList.contains('is-link'),column:!!b.closest('td.el-table-fixed-column--right'),border:getComputedStyle(b).borderTopColor,background:getComputedStyle(b).backgroundColor}))`)
 if(buttons.some(b=>b.border==='rgba(0, 0, 0, 0)'||b.background==='rgba(0, 0, 0, 0)'))throw Error('协议按钮被全局样式显示成文字链接')
 for(const text of ['解析测试','下载源码','下载制品'])if(!buttons.some(b=>b.text===text&&!b.link&&b.column))throw Error(text+'未显示在固定操作栏')
 await writeFile(dir+'/screenshots/protocol-actions.png',Buffer.from((await call('Page.captureScreenshot',{format:'png'})).data,'base64'))
 await evaluate(`[...document.querySelectorAll('.release-buttons button')].find(b=>b.textContent.trim()==='解析测试'&&!b.disabled).click()`)
 await until(()=>evaluate(`[...document.querySelectorAll('.el-dialog')].some(e=>e.getClientRects().length)`))
 console.log('通过：协议按钮位于操作栏，解析测试可打开')
 if(errors.some(e=>e.error||e.path?.startsWith('/api/')))throw Error('页面请求或运行异常')
}finally{socket?.close();child.kill()}
