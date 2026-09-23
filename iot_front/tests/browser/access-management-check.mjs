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
 const click=async text=>until(()=>evaluate(`(()=>{const b=[...document.querySelectorAll('button')].find(b=>b.textContent.trim()===${JSON.stringify(text)}&&b.getClientRects().length&&!b.disabled);b?.click();return !!b})()`)) /* 声明 click。 */
 const fill=async(label,value)=>evaluate(`(()=>{const root=[...document.querySelectorAll('.el-dialog')].find(e=>e.getClientRects().length);const item=[...root.querySelectorAll('.el-form-item')].find(e=>e.querySelector('label')?.textContent.trim().startsWith(${JSON.stringify(label)}));const e=item?.querySelector('input');if(!e)throw Error('缺少表单输入框');e.value=${JSON.stringify(value)};e.dispatchEvent(new Event('input',{bubbles:true}));e.dispatchEvent(new Event('change',{bubbles:true}))})()`) /* 声明 fill。 */
 const request=async(method,path,body,token=auth.accessToken)=>{const r=await fetch('http://127.0.0.1:5173'+path,{method,headers:{Authorization:'Bearer '+token,'Content-Type':'application/json'},body:body?JSON.stringify(body):undefined});return {status:r.status,value:await r.json()}} /* 声明 request。 */
 const suffix=Date.now(),roleId='demo-role-'+suffix,username='demo-user-'+suffix,secret=crypto.randomUUID()+'Aa1' /* 声明 suffix。 */
 await call('Page.navigate',{url:'http://127.0.0.1:5173'}) /* 等待异步操作完成。 */
 await until(()=>evaluate(`!!document.querySelector('.login-intro h1')`));await delay(350) /* 等待异步操作完成。 */
 await writeFile(dir+'/screenshots/login-new.png',Buffer.from((await call('Page.captureScreenshot',{format:'png'})).data,'base64')) /* 等待异步操作完成。 */
 await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});await delay(150) /* 等待异步操作完成。 */
 if(!await evaluate('document.documentElement.scrollWidth<=window.innerWidth+2'))throw Error('登录页窄屏溢出') /* 判断条件并选择处理分支。 */
 await writeFile(dir+'/screenshots/login-mobile.png',Buffer.from((await call('Page.captureScreenshot',{format:'png'})).data,'base64')) /* 等待异步操作完成。 */
 await call('Emulation.setDeviceMetricsOverride',{width:1440,height:1050,deviceScaleFactor:1,mobile:false}) /* 等待异步操作完成。 */
 const setSession=async(token,role,username,permissions)=>evaluate(`(()=>{const s=${JSON.stringify({iot_token:token,iot_tenant:tenant,iot_role:role,iot_user:username,iot_permissions:JSON.stringify(permissions)})};for(const [k,v] of Object.entries(s))localStorage.setItem(k,v)})()`) /* 声明 setSession。 */
 await setSession(auth.accessToken,'admin',user,['*']);await call('Page.reload');await until(()=>evaluate(`!!document.querySelector('.menu-item[aria-label="用户与权限"]')`));await delay(400) /* 等待异步操作完成。 */
 await evaluate(`document.querySelector('.collapse-button').click()`);await delay(250) /* 等待异步操作完成。 */
 if(!await evaluate(`document.querySelector('.app-aside').getBoundingClientRect().width<80`))throw Error('侧栏未折叠') /* 判断条件并选择处理分支。 */
 await call('Page.reload');await until(()=>evaluate(`!!document.querySelector('.collapse-button')`));await delay(250) /* 等待异步操作完成。 */
 if(!await evaluate(`document.querySelector('.app-aside').getBoundingClientRect().width<80`))throw Error('侧栏折叠状态未保留') /* 判断条件并选择处理分支。 */
 await evaluate(`document.querySelector('.collapse-button').click()`) /* 等待异步操作完成。 */
 await evaluate(`document.querySelector('.account').click()`);await delay(100) /* 等待异步操作完成。 */
 if(await evaluate(`[...document.querySelectorAll('.n-dropdown-menu')].filter(e=>e.getClientRects().length).some(e=>e.innerText.includes('租户'))`))throw Error('用户下拉仍显示租户') /* 判断条件并选择处理分支。 */
 await evaluate(`document.querySelector('.account').click()`) /* 等待异步操作完成。 */
 await evaluate(`document.querySelector('.menu-item[aria-label="用户与权限"]').click()`);await delay(800) /* 等待异步操作完成。 */
 await evaluate(`document.querySelector('#tab-roles').click()`);await click('添加角色') /* 等待异步操作完成。 */
 await fill('角色标识',roleId);await fill('角色名称','演示 · 设备查看角色 '+suffix) /* 等待异步操作完成。 */
 await writeFile(dir+'/screenshots/access-role-form.png',Buffer.from((await call('Page.captureScreenshot',{format:'png'})).data,'base64')) /* 等待异步操作完成。 */
 await until(()=>evaluate(`(()=>{const group=[...document.querySelectorAll('.permission-group')].find(g=>g.querySelector('strong')?.textContent==='设备管理');const input=group?.querySelector('input');input?.click();return !!input})()`)) /* 等待异步操作完成。 */
 await click('保存');await until(()=>evaluate(`![...document.querySelectorAll('.el-dialog')].some(e=>e.getClientRects().length)`)) /* 等待异步操作完成。 */
 await evaluate(`document.querySelector('#tab-users').click()`);await click('添加用户');await fill('用户名',username);await fill('显示名称','演示 · 只读用户');await fill('初始密码',secret);await evaluate(`document.querySelector('input[value="all"]').click()`) /* 等待异步操作完成。 */
 await evaluate(`(()=>{const root=[...document.querySelectorAll('.el-dialog')].find(e=>e.getClientRects().length);const item=[...root.querySelectorAll('.el-form-item')].find(e=>e.querySelector('label')?.textContent.trim()==='角色');item.querySelector('.n-base-selection').click()})()`) /* 等待异步操作完成。 */
 await delay(500) /* 等待异步操作完成。 */
 await writeFile(dir+'/screenshots/access-user-form.png',Buffer.from((await call('Page.captureScreenshot',{format:'png'})).data,'base64')) /* 等待异步操作完成。 */
 await until(()=>evaluate(`(()=>{const item=[...document.querySelectorAll('.n-base-select-option')].find(e=>e.textContent.trim()===${JSON.stringify('演示 · 设备查看角色 '+suffix)}&&e.getClientRects().length);item?.click();return !!item})()`)) /* 等待异步操作完成。 */
 await click('保存');await until(()=>evaluate(`![...document.querySelectorAll('.el-dialog')].some(e=>e.getClientRects().length)`)) /* 等待异步操作完成。 */
 await writeFile(dir+'/screenshots/access-users.png',Buffer.from((await call('Page.captureScreenshot',{format:'png'})).data,'base64')) /* 等待异步操作完成。 */
 const login=await request('POST','/api/v1/auth/login',{username,password:secret,tenantId:tenant}) /* 声明 login。 */
 if(login.status!==200)throw Error('新用户登录失败') /* 判断条件并选择处理分支。 */
 const userToken=login.value.accessToken /* 声明 userToken。 */
 if((await request('POST','/api/v1/device-registry',{},userToken)).status!==403)throw Error('未授权新增设备未拒绝') /* 判断条件并选择处理分支。 */
 if((await request('GET','/api/v1/access/users',null,userToken)).status!==403)throw Error('用户管理越权未拒绝') /* 判断条件并选择处理分支。 */
 await setSession(userToken,'operator',username,login.value.permissions);await call('Page.reload');await until(()=>evaluate(`!!document.querySelector('.device-filters')`));await delay(500) /* 等待异步操作完成。 */
 const menus=await evaluate(`[...document.querySelectorAll('.menu-item')].map(e=>e.getAttribute('aria-label'))`) /* 声明 menus。 */
 if(JSON.stringify(menus)!==JSON.stringify(['设备管理']))throw Error('用户可见菜单不符合角色配置') /* 判断条件并选择处理分支。 */
 if(await evaluate(`[...document.querySelectorAll('button')].some(b=>b.textContent.trim()==='添加独立设备'&&b.getClientRects().length)`))throw Error('只读用户仍可见新增按钮') /* 判断条件并选择处理分支。 */
 await writeFile(dir+'/screenshots/access-readonly.png',Buffer.from((await call('Page.captureScreenshot',{format:'png'})).data,'base64')) /* 等待异步操作完成。 */
 const updated=await request('PUT','/api/v1/access/roles/'+roleId,{id:roleId,name:'演示 · 设备查看角色',permissions:['menu:devices','POST /api/v1/device-registry']});if(updated.status!==200)throw Error('角色授权更新失败') /* 声明 updated。 */
 await call('Page.reload');await until(()=>evaluate(`[...document.querySelectorAll('button')].some(b=>b.textContent.trim()==='添加独立设备'&&b.getClientRects().length)`)) /* 等待异步操作完成。 */
 if((await request('POST','/api/v1/device-registry',{},userToken)).status!==422)throw Error('新增权限未实时生效') /* 判断条件并选择处理分支。 */
 const reset=await request('POST','/api/v1/access/users/'+username+'/password',{password:crypto.randomUUID()+'Bb2'});if(reset.status!==200)throw Error('重置密码失败') /* 声明 reset。 */
 if((await request('GET','/api/v1/auth/me',null,userToken)).status!==401)throw Error('旧会话未失效') /* 判断条件并选择处理分支。 */
 await request('DELETE','/api/v1/access/users/'+username);await request('DELETE','/api/v1/access/roles/'+roleId) /* 等待异步操作完成。 */
 console.log('通过：登录页桌面与窄屏、侧栏折叠及持久化、无租户下拉、真实UI创建角色和用户、受限菜单和按钮、后端拒绝越权、授权变更生效、密码重置撤销旧会话。临时用户和角色已清理。') /* 执行当前语句并推进处理流程。 */
}finally{socket?.close();child.kill()} /* 结束当前表达式或代码块。 */
