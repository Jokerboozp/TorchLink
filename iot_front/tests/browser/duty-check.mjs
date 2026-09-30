// Runs against an explicitly selected, disposable real platform fixture.
// It never intercepts or replaces HTTP responses and owns its browser profile.
import assert from 'node:assert/strict'
import {mkdir,writeFile} from 'node:fs/promises'
import {startBrowser,delay} from '../helpers/browser.mjs'
import WebSocket from 'ws'

if(process.env.IOT_TEST_DUTY_FIXTURE!=='1')throw Error('请指定一次性值班验收环境：IOT_TEST_DUTY_FIXTURE=1')
const base=process.env.IOT_TEST_BASE_URL || 'http://127.0.0.1:5181'
const tenant=process.env.IOT_TEST_TENANT || 'duty-e2e'
const username=process.env.IOT_TEST_ADMIN_USER || 'admin'
const password=process.env.IOT_TEST_ADMIN_PASSWORD
if(!password)throw Error('验收登录密码未配置')
const out=process.env.IOT_TEST_OUTPUT || '.e2e/duty-browser'
await mkdir(out,{recursive:true})
async function request(token,path,method='GET',body){
 const response=await fetch(base+path,{method,headers:{...(token?{Authorization:'Bearer '+token}:{}),...(body?{'Content-Type':'application/json'}:{})},body:body?JSON.stringify(body):undefined})
 const data=await response.json().catch(()=>({}))
 if(!response.ok)throw Error(`${method} ${path}: ${response.status} ${data.detail || data.message || ''}`)
 return data
}
const login=(user,secret)=>request('', '/api/v1/auth/login','POST',{tenantId:tenant,username:user,password:secret})
const auth=await login(username,password)
const options=await request(auth.accessToken,'/api/v1/duty/options')
const existingStations=await request(auth.accessToken,'/api/v1/duty/stations?limit=100')
const assigned=new Set(existingStations.items.flatMap(station=>station.body.deviceIds || []))
const device=options.devices.find(device=>!assigned.has(device.id))
if(!device)throw Error('验收环境需预置至少一台设备')
const stamp=Date.now(),receiver=`duty-receiver-${stamp}`,receiverPassword=crypto.randomUUID()+'Aa1!'
const receiverPermissions=['menu:duty','menu:devices',...['participate','record','ai','handover','accept','item','export'].map(action=>'action:duty:'+action)]
await request(auth.accessToken,'/api/v1/access/users','POST',{username:receiver,displayName:'验收接班员',password:receiverPassword,enabled:true,roleIds:[],permissions:receiverPermissions,deviceScope:'selected',deviceIds:[device.id]})
const receiverAuth=await login(receiver,receiverPassword)
const command=(token,path,body,version=0,method='POST')=>request(token,path,method,{expectedVersion:version,idempotencyKey:crypto.randomUUID(),body})
let browser
const failedRequests=[]
try{
 browser=await startBrowser({webSocketImplementation:WebSocket,timeout:30000,interval:150,onEvent:message=>{if(message.method==='Network.responseReceived' && new URL(message.params.response.url).pathname.startsWith('/api/v1/duty/') && message.params.response.status>=400)failedRequests.push({path:new URL(message.params.response.url).pathname,status:message.params.response.status})}})
 const {call,evaluate,until}=browser
 await call('Page.enable');await call('Runtime.enable');await call('Network.enable')
 await call('Emulation.setDeviceMetricsOverride',{width:1440,height:1000,deviceScaleFactor:1,mobile:false})
 await call('Page.navigate',{url:base});await until(()=>evaluate('!!document.querySelector(".login-form")'))
 const setSession=async(identity,user)=>evaluate(`(()=>{const values=${JSON.stringify({iot_token:identity.accessToken,iot_tenant:tenant,iot_user:user,iot_role:identity.role || 'operator',iot_permissions:JSON.stringify(identity.permissions || ['*']),iot_access_version:identity.accessVersion || ''})};for(const [key,value] of Object.entries(values))localStorage.setItem(key,value)})()`)
 const click=async text=>until(()=>evaluate(`(()=>{const button=[...document.querySelectorAll('button')].find(node=>node.getClientRects().length && !node.disabled && node.textContent.trim()===${JSON.stringify(text)});button?.click();return !!button})()`),'button '+text)
 const tab=async name=>{const labels={current:'我的值班',rosters:'排班日历',handovers:'交接记录',items:'跟进事项',settings:'值班设置'};await until(()=>evaluate(`(()=>{const node=[...document.querySelectorAll('.n-tabs-tab')].find(node=>node.getClientRects().length && node.textContent.trim()===${JSON.stringify(labels[name])});node?.click();return !!node})()`),'tab '+name);await delay(200)}
 const fill=async(label,value)=>evaluate(`(()=>{const dialog=[...document.querySelectorAll('.ui-dialog')].filter(node=>node.getClientRects().length).at(-1);const item=[...dialog.querySelectorAll('.ui-form-item')].find(node=>node.querySelector('label')?.textContent.trim().startsWith(${JSON.stringify(label)}));const field=item?.querySelector('input,textarea');if(!field)throw Error('表单字段不存在');field.value=${JSON.stringify(value)};field.dispatchEvent(new Event('input',{bubbles:true}));field.dispatchEvent(new Event('change',{bubbles:true}))})()`)
 const select=async(label,text)=>{await evaluate(`(()=>{const dialog=[...document.querySelectorAll('.ui-dialog')].filter(node=>node.getClientRects().length).at(-1);const item=[...dialog.querySelectorAll('.ui-form-item')].find(node=>node.querySelector('label')?.textContent.trim().startsWith(${JSON.stringify(label)}));const control=item?.querySelector('.n-base-selection');if(!control)throw Error('选择器不存在');control.click()})()`);await until(()=>evaluate(`(()=>{const option=[...document.querySelectorAll('.n-base-select-option')].find(node=>node.getClientRects().length && node.textContent.includes(${JSON.stringify(text)}));option?.click();return !!option})()`),'option '+text)}
 const dialogsClosed=()=>until(()=>evaluate(`![...document.querySelectorAll('.ui-dialog')].some(node=>node.getClientRects().length)`))
 const openDuty=async()=>{await until(()=>evaluate(`!!document.querySelector('.nav-item[aria-label="值班管理"]')`));await evaluate(`document.querySelector('.nav-item[aria-label="值班管理"]').click()`);await until(()=>evaluate(`!!document.querySelector('.duty-management') && !document.querySelector('.duty-management>.ui-skeleton')`));await delay(350)}
 const screenshot=async name=>writeFile(`${out}/${name}.png`,Buffer.from((await call('Page.captureScreenshot',{format:'png'})).data,'base64'))
 await setSession(auth,username);await call('Page.reload');await openDuty();console.log('Browser stage: duty loaded')
 await tab('settings');await click('新增岗位');await fill('名称','验收值班岗位 '+stamp);await select('责任主管',username);await select('负责设备',device.name || device.id);await click('保存');await dialogsClosed()
 const station=(await request(auth.accessToken,'/api/v1/duty/stations?limit=100')).items.find(row=>row.body.name==='验收值班岗位 '+stamp)
 console.log('Browser stage: station saved');assert.ok(station,'岗位真实保存')
 await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});await delay(350);assert.ok(await evaluate('document.documentElement.scrollWidth<=window.innerWidth+2 && document.querySelector(".duty-management").getBoundingClientRect().right<=window.innerWidth+2'),'设置窄屏内部布局与整页都保持视口范围');await screenshot('settings-390');await call('Emulation.setDeviceMetricsOverride',{width:1440,height:1000,deviceScaleFactor:1,mobile:false});await delay(350)
 const now=Date.now(),end=now+60*1000
 const first=await command(auth.accessToken,'/api/v1/duty/rosters',{stationId:station.id,startAt:now-3600e3,endAt:end,memberIds:[username],leaderId:username})
 const next=await command(auth.accessToken,'/api/v1/duty/rosters',{stationId:station.id,startAt:end,endAt:end+12*3600e3,memberIds:[receiver],leaderId:receiver})
 await command(auth.accessToken,`/api/v1/duty/rosters/${first.id}/publish`,{},first.version)
 await command(auth.accessToken,`/api/v1/duty/rosters/${next.id}/publish`,{},next.version)
 await tab('current');await click('刷新');await delay(250)
 await click('到岗记录');await select('计划班次','验收值班岗位');await click('确认保存');await dialogsClosed()
 await click('首次开班');await select('计划班次','验收值班岗位');await fill('原因及安排','首次启用测试岗位');await click('确认保存');await dialogsClosed();await delay(250)
 await click('追加记录');await fill('现场情况','已联系维修，夜班需要继续核实到场及设备恢复情况。');await click('确认保存');await dialogsClosed()
 await click('新增事项');await fill('事项标题','核实维修到场');await fill('下一步动作','接班后联系维修人员，核实设备恢复并记录结果。');await click('保存');await dialogsClosed()
 console.log('Browser stage: current shift, record and item saved');await screenshot('current-desktop')
 await click('准备交接');console.log('Browser stage: preparing handover');await select('接班排班','验收接班员');console.log('Browser stage: next crew selected');await click('创建交接草稿');await until(()=>evaluate(`!!document.querySelector('.handover-stats')`));console.log('Browser stage: handover details opened')
 assert.ok(await evaluate(`(()=>{const rows=[...document.querySelectorAll('.duty-details .n-data-table-tbody tr')];return rows.length>0 && rows.some(row=>row.innerText.includes('追加处置记录')) && rows.every(row=>!row.querySelector('td')?.innerText.includes('—'))})()`),'冻结事件展示真实类型与发生/入库时间')
 await evaluate(`(()=>{const field=document.querySelector('.duty-details textarea');field.value='下一班请核实维修到场，故障原因尚未确认。';field.dispatchEvent(new Event('input',{bubbles:true}))})()`);console.log('Browser stage: human notes edited')
 await click('保存新版本');console.log('Browser stage: revision save clicked');await until(()=>evaluate(`!!document.querySelector('.handover-stats') && !document.querySelector('.duty-details')?.innerText.includes('有未保存的人工补充')`));await delay(200)
 console.log('Browser stage: handover revision saved');await screenshot('handover-desktop')
 await click('核对并提交交班');await click('确认');await until(()=>evaluate(`document.querySelector('.duty-details')?.innerText.includes('待接班')`))
 const handover=(await request(auth.accessToken,'/api/v1/duty/handovers?limit=100')).items.find(row=>row.body.stationId===station.id)
 assert.equal(handover.body.status,'SUBMITTED');console.log('Browser stage: submitted')
 // Receiver exercises a distinct account and the same real immutable handover.
 await setSession(receiverAuth,receiver);await call('Page.reload');await openDuty()
 await click('到岗记录');await select('计划班次','验收值班岗位');await click('确认保存');await dialogsClosed()
 await click('核对并接班');await until(()=>evaluate(`!!document.querySelector('.handover-stats')`))
 const changes=await evaluate(`document.querySelector('.duty-details')?.innerText.includes('已核对最新变化')`)
 if(changes)await evaluate(`(()=>{const label=[...document.querySelectorAll('.n-checkbox')].find(node=>node.textContent.includes('已核对最新变化'));label?.click()})()`)
 await click('确认接收并接班');await click('确认');await until(()=>evaluate(`document.querySelector('.duty-details')?.innerText.includes('已接收')`))
 const accepted=await request(receiverAuth.accessToken,`/api/v1/duty/handovers/${handover.id}`)
 assert.equal(accepted.body.status,'ACCEPTED');console.log('Browser stage: accepted')
 assert.equal(accepted.body.acceptance.revisionId,accepted.body.acceptanceRevisionId,'接班签署绑定接收时事实版本');assert.notEqual(accepted.body.acceptanceRevisionId,accepted.body.submission.revisionId,'交班与接班分别保留事实版本')
 const items=await request(receiverAuth.accessToken,`/api/v1/duty/items?stationId=${station.id}`)
 assert.equal(items.items[0].body.ownerId,receiver,'未完成事项真实转交给接班人')
 const pdf=await fetch(base+`/api/v1/duty/revisions/${accepted.body.currentRevisionId}/pdf`,{headers:{Authorization:'Bearer '+receiverAuth.accessToken}})
 assert.equal(pdf.status,200);const pdfBytes=Buffer.from(await pdf.arrayBuffer());assert.equal(pdfBytes.subarray(0,4).toString(),'%PDF');await writeFile(`${out}/handover.pdf`,pdfBytes)
 const acceptedPDF=await fetch(base+`/api/v1/duty/revisions/${accepted.body.acceptanceRevisionId}/pdf`,{headers:{Authorization:'Bearer '+receiverAuth.accessToken}});assert.equal(acceptedPDF.status,200);await writeFile(`${out}/handover-accepted.pdf`,Buffer.from(await acceptedPDF.arrayBuffer()))
 await screenshot('accepted-desktop');await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});await delay(200);assert.ok(await evaluate(`(()=>{const dialog=[...document.querySelectorAll('.ui-dialog')].find(node=>node.getClientRects().length),body=document.querySelector('.duty-details'),bounds=dialog.getBoundingClientRect();body.scrollTop=body.scrollHeight;return bounds.left>=0 && bounds.right<=innerWidth+2 && body.scrollTop>0 && body.scrollWidth<=body.clientWidth+2})()`),'390px 交接详情保持弹窗范围且正文可独立滚动');await screenshot('handover-detail-390');await click('关闭');await dialogsClosed()
 await call('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true})
 for(const name of ['current','rosters','handovers','items']){await tab(name);await delay(250);assert.ok(await evaluate('document.documentElement.scrollWidth<=window.innerWidth+2 && document.querySelector(".duty-management").getBoundingClientRect().right<=window.innerWidth+2'),`${name}窄屏布局保持视口范围`);await screenshot(`${name}-390`)}
 assert.equal(browser.errors.length,0,'无浏览器运行异常')
 assert.equal(failedRequests.length,0,'值班接口无失败响应')
 console.log(JSON.stringify({passed:true,coverage:['真实UI创建岗位','本人到岗与首次开班','人工处置','创建跨班事项','新版本交接与双账号签收','事项责任转移','PDF真实导出','390px五个业务路径无整页溢出'],screenshots:out}))
}catch(error){if(browser){try{await writeFile(`${out}/failure.png`,Buffer.from((await browser.call('Page.captureScreenshot',{format:'png'})).data,'base64'));console.error(await browser.evaluate('document.body.innerText.slice(-4000)'));console.error(JSON.stringify(failedRequests));}catch(diagnosticError){console.error('Browser diagnostic unavailable:',diagnosticError.message)}}throw error}finally{await browser?.close()}
