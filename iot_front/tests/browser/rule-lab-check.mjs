// Actual source API and browser regression in an explicitly isolated fixture.
// Configuration/analysis records and temporary scoped users are the only writes.
// Source telemetry and connection/dependency histories are preseeded by the host.
import assert from 'node:assert/strict'
import { randomUUID } from 'node:crypto'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import WebSocket from 'ws'
import { startBrowser, delay } from '../helpers/browser.mjs'

if (process.env.IOT_TEST_RULELAB_FIXTURE !== '1') throw Error('请指定隔离规则实验验收环境：IOT_TEST_RULELAB_FIXTURE=1')
if (!process.env.IOT_TEST_RULELAB_INFO) throw Error('请指定 IOT_TEST_RULELAB_INFO 固定事实 JSON 文件')
const info = JSON.parse(await readFile(process.env.IOT_TEST_RULELAB_INFO, 'utf8')), fixture = info.ruleLab
if (!fixture?.expected?.synthetic || !fixture.deviceIds?.length || !(fixture.end > fixture.start)) throw Error('fixture 须明确明确隔离设备和受控事实区间')
if (fixture.start % 1000 || fixture.end % 1000 || fixture.warmupStart % 1000) throw Error('fixture 时间须为整秒，与实际日期编辑器精度一致')
const base = process.env.IOT_TEST_BASE_URL || 'http://127.0.0.1:5182', tenant = info.tenantId
const username = process.env.IOT_TEST_ADMIN_USER || 'admin', password = process.env.IOT_TEST_ADMIN_PASSWORD
if (!password) throw Error('验收登录密码未配置')
const out = resolve(process.env.IOT_TEST_OUTPUT || '.e2e/rule-lab-browser'), stamp = Date.now()
await mkdir(out, { recursive: true })
let auth = null, browser = null, permissionStage = false
const failedRequests = [], coverage = [], users = []
const path = id => `/api/v1/rule-lab/runs/${encodeURIComponent(id)}`
async function response(url, method = 'GET', body, token = auth?.accessToken) {
 const result = await fetch(base + url, { method, headers: { ...(token ? { Authorization: 'Bearer ' + token } : {}), ...(body ? { 'Content-Type': 'application/json' } : {}) }, body: body ? JSON.stringify(body) : undefined })
 return { status: result.status, data: await result.json().catch(() => ({})) }
}
async function request(url, method = 'GET', body, token = auth?.accessToken) {
 const result = await response(url, method, body, token)
 if (result.status >= 400) throw Error(`${method} ${url}: ${result.status} ${result.data.detail || result.data.message || ''}`)
 return result.data
}
const login = (user, secret) => request('/api/v1/auth/login', 'POST', { tenantId: tenant, username: user, password: secret }, '')
const all = async (url, token = auth.accessToken) => {
 const rows = []
 for (let offset = 0; ; offset += 100) {
  const page = await request(url + (url.includes('?') ? '&' : '?') + `limit=100&offset=${offset}`, 'GET', undefined, token)
  rows.push(...(page.items || []))
  if (!page.items?.length || rows.length >= page.total || page.items.length < 100) return rows
 }
}
try {
 auth = await login(username, password)
 const devices = await Promise.all(fixture.deviceIds.map(id => request(`/api/v1/device-registry/${encodeURIComponent(id)}/connection`)))
 const name = id => devices.find(value => value.device.id === id)?.device.name || id
 const [d1, d2] = fixture.deviceIds
 const baselines=await all('/api/v1/rule-lab/rule-sources?deviceIds='+fixture.deviceIds.join(',')), baseline=baselines.find(row=>row.id===fixture.baselineRevisionId);assert.ok(baseline,'实际固定规则基线可读');const productionBefore=await request(`/api/v1/rules/${encodeURIComponent(fixture.ruleId)}/revisions`), alarmsBefore=await request('/api/v1/alarms');
 browser = await startBrowser({ webSocketImplementation: WebSocket, timeout: 30000, interval: 150, onEvent: event => {
  if (event.method === 'Network.responseReceived') {
   const url = new URL(event.params.response.url)
   if (!permissionStage && url.pathname.startsWith('/api/v1/rule-lab/') && event.params.response.status >= 400) failedRequests.push({ path: url.pathname, status: event.params.response.status })
  }
 } })
 const { call, evaluate, until } = browser
 await call('Page.enable'); await call('Runtime.enable'); await call('Network.enable')
 await call('Browser.setDownloadBehavior', { behavior: 'allow', downloadPath: out })
 const viewport = (width = 1440, height = 1050) => call('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: 1, mobile: width < 640 })
 await viewport(); await call('Page.navigate', { url: base }); await until(() => evaluate('!!document.querySelector(".login-form")'))
 const setSession = async (identity, user) => {
  await evaluate(`(()=>{const values=${JSON.stringify({ iot_token: identity.accessToken, iot_tenant: tenant, iot_user: user, iot_role: identity.role || 'operator', iot_permissions: JSON.stringify(identity.permissions || []), iot_access_version: identity.accessVersion || '' })};for(const [key,value] of Object.entries(values))localStorage.setItem(key,value)})()`)
  await call('Page.reload')
 }
 const click = text => until(() => evaluate(`(()=>{const node=[...document.querySelectorAll('button')].find(row=>row.getClientRects().length&&!row.disabled&&row.textContent.trim()===${JSON.stringify(text)});node?.click();return !!node})()`), 'button ' + text)
 const clickSelector = selector => until(() => evaluate(`(()=>{const node=document.querySelector(${JSON.stringify(selector)});if(!node?.getClientRects().length)return false;node.click();return true})()`), selector)
 const tab = async text => { await until(() => evaluate(`(()=>{const node=[...document.querySelectorAll('.n-tabs-tab')].find(row=>row.getClientRects().length&&row.textContent.trim()===${JSON.stringify(text)});node?.click();return !!node})()`), 'tab ' + text); await delay(150) }
 const fill = async (label, value, index = 0) => evaluate(`(()=>{const dialog=[...document.querySelectorAll('.ui-dialog')].filter(node=>node.getClientRects().length).at(-1);const item=[...dialog.querySelectorAll('.ui-form-item')].find(node=>node.querySelector('label')?.textContent.trim().startsWith(${JSON.stringify(label)}));const field=item?.querySelectorAll('input,textarea')[${index}];if(!field)throw Error('字段不存在：'+${JSON.stringify(label)});field.focus();field.value=${JSON.stringify(String(value))};field.dispatchEvent(new Event('input',{bubbles:true}));field.dispatchEvent(new Event('change',{bubbles:true}));field.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',code:'Enter',bubbles:true}));field.blur()})()`)
 const date = async (label, at, index = 0) => fill(label, await evaluate(`(()=>{const d=new Date(${at}),p=n=>String(n).padStart(2,'0');return d.getFullYear()+'-'+p(d.getMonth()+1)+'-'+p(d.getDate())+' '+p(d.getHours())+':'+p(d.getMinutes())+':'+p(d.getSeconds())})()`), index)
 const range = async (label,start,end) => {for(const [index,at] of [start,end].entries()){const value=await evaluate(`(()=>{const d=new Date(${at}),p=n=>String(n).padStart(2,'0');return d.getFullYear()+'-'+p(d.getMonth()+1)+'-'+p(d.getDate())+' '+p(d.getHours())+':'+p(d.getMinutes())+':'+p(d.getSeconds())})()`);await evaluate(`(()=>{const dialog=[...document.querySelectorAll('.ui-dialog')].filter(n=>n.getClientRects().length).at(-1),item=[...dialog.querySelectorAll('.ui-form-item')].find(n=>n.querySelector('label')?.textContent.trim().startsWith(${JSON.stringify(label)})),field=item.querySelectorAll('input')[${index}];field.value=${JSON.stringify(value)};field.dispatchEvent(new Event('input',{bubbles:true}))})()`);await delay(100)}}
 const select = async (label, text, extra = '') => {
  await evaluate(`(()=>{const dialog=[...document.querySelectorAll('.ui-dialog')].filter(node=>node.getClientRects().length).at(-1);const item=[...dialog.querySelectorAll('.ui-form-item')].find(node=>node.querySelector('label')?.textContent.trim().startsWith(${JSON.stringify(label)}));const control=item?.querySelector('.n-base-selection');if(!control)throw Error('选择器不存在：'+${JSON.stringify(label)});control.click()})()`)
  await until(() => evaluate(`(()=>{const option=[...document.querySelectorAll('.n-base-select-option')].find(node=>node.getClientRects().length&&node.textContent.includes(${JSON.stringify(text)})&&node.textContent.includes(${JSON.stringify(extra)}));option?.click();return !!option})()`), 'option ' + text)
  const point=await evaluate(`(()=>{const r=[...document.querySelectorAll('.ui-dialog')].filter(node=>node.getClientRects().length).at(-1).querySelector('.n-card-header').getBoundingClientRect();return{x:r.left+15,y:r.top+15}})()`);await call('Input.dispatchMouseEvent',{type:'mousePressed',...point,button:'left',clickCount:1});await call('Input.dispatchMouseEvent',{type:'mouseReleased',...point,button:'left',clickCount:1})
  await delay(250) // Wait for a previous teleported menu to finish its close transition.
 }
 const closed = () => until(() => evaluate(`![...document.querySelectorAll('.ui-dialog')].some(node=>node.getClientRects().length)`), 'dialog closed')
 const visibleDialogText = () => evaluate(`([...document.querySelectorAll('.ui-dialog')].filter(node=>node.getClientRects().length).at(-1)?.innerText || '')`)
 const screenshot = async file => writeFile(`${out}/${file}.png`, Buffer.from((await call('Page.captureScreenshot', { format: 'png' })).data, 'base64'))
 const openPage = async () => {await clickSelector('.nav-item[aria-label="告警规则"]');await until(()=>evaluate(`!!document.querySelector('.rule-lab-page')||[...document.querySelectorAll('.n-tabs-tab')].some(n=>n.getClientRects().length&&n.textContent.trim()==='告警规则对比')`));if(await evaluate(`[...document.querySelectorAll('.n-tabs-tab')].some(node=>node.getClientRects().length&&node.textContent.trim()==='告警规则对比')`))await tab('告警规则对比');await until(()=>evaluate('!!document.querySelector(".rule-lab-page")&&!document.querySelector(".rule-lab-page>.ui-skeleton")'));await delay(250)}
 const terminal = async (id, token = auth.accessToken) => until(async () => { const value = await request(path(id), 'GET', undefined, token); return ['SUCCEEDED','PARTIAL','FAILED','CANCELLED'].includes(value.status) ? value : false }, 'real persistent facts complete', 60000)
 await setSession(auth, username); await openPage()
 console.log('规则实验验收：固定完整标准输入、历史时钟与初态')
 const beforeDatasets=new Set((await all('/api/v1/rule-lab/datasets')).map(row=>row.id))
 await click('新建固定数据集');for(const id of fixture.deviceIds)await select('明确设备范围',name(id));await range('主评价区间',fixture.start,fixture.end);await date('预热起点',fixture.warmupStart);await click('固定数据集');await closed()
 const dataset=await until(async()=> (await all('/api/v1/rule-lab/datasets')).find(row=>!beforeDatasets.has(row.id)),'dataset persisted');await terminal(dataset.runId)
 const frozen=await request('/api/v1/rule-lab/datasets/'+dataset.id), inputs=await all('/api/v1/rule-lab/datasets/'+dataset.id+'/inputs')
 assert.ok(['SUCCEEDED','PARTIAL'].includes(frozen.status));assert.equal(inputs.length,fixture.expected.sourceInputCount);assert.equal(frozen.inputCount,inputs.length);assert.equal(frozen.clockPolicy,'EVENT_AS_PROCESSING');assert.equal(frozen.initialStateQuality,'UNKNOWN');assert.equal(frozen.start,fixture.start);assert.equal(frozen.warmupStart,fixture.warmupStart)
 assert.ok(inputs.every(row=>row.message.properties&&row.message.timestamp&&row.receivedAt&&row.hash));assert.ok(inputs.some(row=>row.availableAt>row.message.timestamp));await until(()=>evaluate('document.querySelector(".rule-lab-page")?.innerText.includes("完整固定输入分页")'));await clickSelector(`[data-lab-input="${inputs[0].id}"]`);assert.ok((await visibleDialogText()).includes('接收'));assert.ok((await visibleDialogText()).includes('缺少该消息的实际求值 trace'));await click('关闭');await closed()
 coverage.push('真实标准输入、事件/接收/可查询时间与历史未知元数据固定，不补当前版本')
 await screenshot('datasets-desktop')
 console.log('规则实验验收：保存固定候选正文并独立比较')
 const beforeExperiments=new Set((await all('/api/v1/rule-lab/experiments')).map(row=>row.id))
 await tab('候选与比较');await click('新建规则实验');await select('固定数据集',dataset.id.slice(-8));await select('不可变基线规则版本',baseline.rule.name,'版本 '+baseline.version)
 const candidateConditions=structuredClone(baseline.rule.conditions);candidateConditions[0].value=101.5
 await fill('触发条件 JSON',JSON.stringify(candidateConditions));await fill('预先固定的行为假设','隔离受控样本：提高压力触发阈值，核实匹配消息与周期行为 '+stamp);await fill('匹配策略版本','browser-matching-v1');await fill('固定匹配容差（秒）',.1);await click('保存禁用候选与实验版本');await closed()
 const experiment=await until(async()=>(await all('/api/v1/rule-lab/experiments')).find(row=>!beforeExperiments.has(row.id)),'immutable experiment persisted');assert.equal(experiment.body.candidate.enabled,false);assert.equal(experiment.body.candidateEnabled,baseline.rule.enabled);assert.equal(experiment.body.evaluationPolicy.toleranceMs,100);assert.equal(experiment.body.datasetId,dataset.id)
 await click('运行当前固定实验');const runs=await until(async()=>{const rows=await all(`/api/v1/rule-lab/experiments/${experiment.id}/runs`);return rows.length?rows:false},'comparison run persisted'),run=await terminal(runs[0].id)
 assert.ok(['SUCCEEDED','PARTIAL'].includes(run.status),run.error);await until(()=>evaluate('!!document.querySelector(".lab-branch-metrics")'))
 const snapshot=await request(path(run.id)+'/snapshot'),metrics=await all(path(run.id)+'/metrics'),outcomes=await all(path(run.id)+'/outcomes'),diffs=await all(path(run.id)+'/diffs'),findings=await all(path(run.id)+'/findings')
 assert.equal(metrics.length,2);for(const branch of ['BASELINE','CANDIDATE']){const m=metrics.find(row=>row.body.branch===branch).body;assert.equal(m.counters.inputCount,inputs.length);assert.equal(m.labelEvaluation.status,'NOT_EVALUABLE');assert.equal(m.labelEvaluation.precision,undefined);assert.equal(m.labelEvaluation.recall,undefined);const expected=fixture.expected[branch==='BASELINE'?'baselineCycles':'candidateCycles'];if(expected!=null)assert.equal(m.counters.newCycles,expected)}
 assert.equal(snapshot.statistics.commonEvents+snapshot.statistics.baselineOnly,outcomes.filter(row=>row.body.branch==='BASELINE').length);assert.equal(snapshot.statistics.commonEvents+snapshot.statistics.candidateOnly,outcomes.filter(row=>row.body.branch==='CANDIDATE').length);assert.ok(diffs.length);assert.ok(outcomes.some(row=>row.body.recoveredAt>0))
 assert.deepEqual(await request(`/api/v1/rules/${encodeURIComponent(fixture.ruleId)}/revisions`),productionBefore,'实验不修改生产规则版本');assert.deepEqual(await request('/api/v1/alarms'),alarmsBefore,'实验不写生产告警')
 assert.ok(await evaluate('document.querySelector(".lab-evaluation").innerText.includes("证据不足")'));await clickSelector(`[data-lab-outcome="${outcomes[0].id}"]`);assert.ok((await visibleDialogText()).includes('周期产生版本'));assert.ok((await visibleDialogText()).includes('固定输入'));await click('关闭');await closed();await screenshot('comparison-desktop')
 coverage.push('固定基线/禁用候选、双分支真实行为计数、一对一差异、初态未知阻止准确率；生产规则与告警未变')
 console.log('规则实验验收：人工核实、标签确认、刷新保留旧版本')
 assert.ok(findings.length);const finding=findings[0],reviewText='浏览器核对固定输入和独立分支；模拟时钟与真实现场原因待确认。'
 await clickSelector(`[data-lab-finding="${finding.id}"]`);await select('核实结论','继续观察');await fill('实际核实依据',reviewText);await click('保存核实依据');const review=await until(async()=>(await all(path(run.id)+'/reviews')).find(row=>row.explanation===reviewText),'review persisted');assert.equal(review.result,'OBSERVE');await until(async()=>(await visibleDialogText()).includes(reviewText));await viewport(390,844);await delay(200);assert.ok(await evaluate(`(()=>{const dialog=[...document.querySelectorAll('.ui-dialog')].filter(n=>n.getClientRects().length).at(-1),body=dialog.querySelector('.lab-dialog-body');body.scrollTop=body.scrollHeight;return dialog.getBoundingClientRect().right<=innerWidth+2&&body.scrollTop>0})()`));await screenshot('review-390');await click('关闭');await closed();await viewport()
 const beforeLabels=new Set((await all('/api/v1/rule-lab/labels')).map(row=>row.id));await tab('真实标签');await click('新增真实标签');await select('设备',name(d1));await fill('事件类型',baseline.rule.alarmType);await select('核实结论','尚不确定');await range('实际核实区间',fixture.start+1000,fixture.start+4000);await fill('真实核实依据','隔离合成样本，现场事件未知，不作为负样本 '+stamp);await click('保存标签草稿');await closed();const draft=await until(async()=>(await all('/api/v1/rule-lab/labels')).find(row=>!beforeLabels.has(row.id)),'label draft persisted');assert.equal(draft.body.status,'DRAFT');await clickSelector(`[data-lab-label="${draft.id}"]`);const label=await until(async()=>(await all('/api/v1/rule-lab/labels')).find(row=>row.resourceId===draft.resourceId&&row.body.status==='CONFIRMED'),'confirmed new immutable label');assert.notEqual(label.id,draft.id);assert.ok(label.body.confirmedBy)
 await call('Page.reload');await openPage();await until(()=>evaluate('!!document.querySelector(".lab-branch-metrics")'));assert.deepEqual(await all(path(run.id)+'/metrics'),metrics);assert.deepEqual(await all('/api/v1/rule-lab/datasets/'+dataset.id+'/inputs'),inputs);coverage.push('人工核实持久化、未知标签草稿到人工确认、新标签不改变旧实验、刷新恢复固定版本')
 if(process.env.IOT_TEST_RULELAB_AI==='1'){console.log('规则实验验收：显式 AI 独立版本');await click('开始 AI 解读');const job=await until(async()=>(await all(`/api/v1/rule-lab/experiments/${experiment.id}/ai-jobs?runId=${run.id}`)).find(row=>['SUCCEEDED','FAILED','CANCELLED'].includes(row.status)),'AI terminal',60000);if(job.status==='SUCCEEDED')for(const key of ['behaviorDifferences','verificationSuggestions','limitations'])assert.ok(Array.isArray(job.interpretation[key]));assert.deepEqual(await all(path(run.id)+'/metrics'),metrics);coverage.push('显式 AI '+job.status+'；固定事实及核实保留')}
 await click('导出实验报告');const report=await until(async()=>{try{return JSON.parse(await readFile(`${out}/规则实验_${run.id}.json`,'utf8'))}catch{return false}},'real report download');assert.equal(report.run.id,run.id);assert.ok(report.reviews.some(row=>row.id===review.id));coverage.push('真实报告下载保留固定事实与人工核实')
 console.log('规则实验验收：明确发布与过时基线冲突')
 permissionStage=true
 await click('审阅并发布固定候选');await fill('发布原因','隔离fixture人工核实固定候选 '+stamp);await click('确认发布固定候选');await until(async()=>(await visibleDialogText()).includes('候选已发布'));await click('关闭');await closed()
 const published=await request(`/api/v1/rules/${encodeURIComponent(fixture.ruleId)}/revisions`);assert.equal(published.items[0].version,baseline.version+1);assert.equal(published.items[0].experimentId,experiment.id);assert.equal(published.items[0].rule.conditions[0].value,101.5)
 await click('审阅并发布固定候选');await fill('发布原因','同一旧基线应拒绝再次发布');await click('确认发布固定候选');await until(async()=>(await visibleDialogText()).includes('当前生产规则已发生变化'));assert.equal((await request(`/api/v1/rules/${encodeURIComponent(fixture.ruleId)}/revisions`)).items[0].version,baseline.version+1);await click('关闭');await closed();coverage.push('人工明确发布固定候选，过时生产基线409拒绝且未新增版本')
 await viewport(390,844);for(const label of ['固定数据集','候选与比较','真实标签']){await tab(label);await delay(200);assert.ok(await evaluate('document.documentElement.scrollWidth<=innerWidth+2&&document.querySelector(".rule-lab-page").getBoundingClientRect().right<=innerWidth+2'),label+'390无整页溢出');await screenshot(label+'-390')};await tab('固定数据集');await click('新建固定数据集');await delay(200);assert.ok(await evaluate(`(()=>{const dialog=[...document.querySelectorAll('.ui-dialog')].filter(n=>n.getClientRects().length).at(-1),body=dialog.querySelector('.lab-dialog-body');body.scrollTop=body.scrollHeight;return dialog.getBoundingClientRect().right<=innerWidth+2&&body.scrollTop>0})()`));await screenshot('dataset-dialog-390');await click('取消');await closed();await viewport();coverage.push('390px三页横向受控、数据集与核实弹窗独立滚动')
 console.log('规则实验验收：受限用户无需生产规则菜单、范围撤销')
 const makeUser=async(suffix,permissions)=>{const user=`rulelab-${stamp}-${suffix}`,secret=randomUUID()+'Aa1!';await request('/api/v1/access/users','POST',{username:user,displayName:'隔离规则实验 '+suffix,password:secret,enabled:true,roleIds:[],permissions,deviceScope:'selected',deviceIds:[d1]});users.push(user);return{user,secret,permissions,identity:await login(user,secret)}}
 const menu=await makeUser('menu',['menu:ruleLab']);assert.equal((await response('/api/v1/rule-lab/rule-sources?deviceIds='+d1,'GET',undefined,menu.identity.accessToken)).status,403);await setSession(menu.identity,menu.user);await openPage();await until(()=>evaluate('document.querySelector(".rule-lab-page")?.innerText.includes("需要设备管理读取权限")'));assert.ok(!await evaluate('!!document.querySelector(".lab-branch-metrics")'))
 const scoped=await makeUser('scoped',['menu:devices','menu:ruleLab','POST /api/v1/rule-lab/datasets','POST /api/v1/rule-lab/experiments','POST /api/v1/rule-lab/experiments/:id/runs','GET /api/v1/rule-lab/experiments/:id/report'])
 assert.ok(!scoped.identity.permissions.includes('menu:rules'));const sources=await all('/api/v1/rule-lab/rule-sources?deviceIds='+d1,scoped.identity.accessToken);assert.ok(sources.some(row=>row.ruleId===fixture.ruleId));assert.equal((await response(`/api/v1/rules/${encodeURIComponent(fixture.ruleId)}/revisions`,'GET',undefined,scoped.identity.accessToken)).status,403)
 await setSession(scoped.identity,scoped.user);await openPage();assert.ok(!await evaluate(`[...document.querySelectorAll('.n-tabs-tab')].some(n=>n.getClientRects().length&&n.textContent.trim()==='当前规则')`));await tab('候选与比较');await click('新建规则实验');assert.ok((await visibleDialogText()).includes('不可变基线规则版本'));await click('取消');await closed()
 const scopedDataset=await request('/api/v1/rule-lab/datasets','POST',{deviceIds:[d1],start:fixture.start,end:fixture.end,warmupStart:fixture.warmupStart,timeBasis:'EVENT',clockPolicy:'EVENT_AS_PROCESSING',semanticsVersion:frozen.semanticsVersion,initialStatePolicy:'EMPTY_UNKNOWN',idempotencyKey:randomUUID()},scoped.identity.accessToken);await terminal(scopedDataset.runId,scoped.identity.accessToken)
 const scopedExperiment=await request('/api/v1/rule-lab/experiments','POST',{resourceId:randomUUID(),expectedVersion:0,scope:'personal',deviceIds:[d1],body:{...experiment.body,datasetId:scopedDataset.id,baselineRevisionIds:[sources.find(row=>row.ruleId===fixture.ruleId).id],candidate:{...experiment.body.candidate,conditions:candidateConditions}}},scoped.identity.accessToken);const scopedRun=await request(`/api/v1/rule-lab/experiments/${scopedExperiment.id}/runs`,'POST',{idempotencyKey:randomUUID()},scoped.identity.accessToken);await terminal(scopedRun.id,scoped.identity.accessToken);await openPage();await click('刷新');await delay(300);await tab('候选与比较');await clickSelector(`[data-lab-experiment="${scopedExperiment.id}"]`);await clickSelector(`[data-lab-run="${scopedRun.id}"]`);await until(()=>evaluate('!!document.querySelector(".lab-branch-metrics")'));if(d2)assert.ok(!await evaluate(`document.querySelector('.rule-lab-page').innerText.includes(${JSON.stringify(name(d2))})`));assert.ok(!await evaluate(`[...document.querySelectorAll('.rule-lab-page button')].some(n=>n.textContent.includes('审阅并发布'))`))
 await request(`/api/v1/access/users/${scoped.user}`,'PUT',{displayName:'撤销隔离规则实验范围',enabled:true,roleIds:[],permissions:scoped.permissions,deviceScope:'none',deviceIds:[]});scoped.identity=await login(scoped.user,scoped.secret);assert.equal((await response(path(scopedRun.id),'GET',undefined,scoped.identity.accessToken)).status,403);await setSession(scoped.identity,scoped.user);await openPage();assert.ok(!await evaluate('!!document.querySelector(".lab-branch-metrics")'));coverage.push('应用菜单缺设备权限403，单设备用户无需生产规则菜单可实验、生产历史403、撤销后旧事实清除')
 assert.equal(browser.errors.length,0,'浏览器无运行异常');await writeFile(`${out}/report.json`,JSON.stringify({passed:true,coverage,datasetId:dataset.id,experimentId:experiment.id,runId:run.id,source:'isolated-real-postgresql-source-api',screenshots:out},null,2));console.log(JSON.stringify({passed:true,coverage,screenshots:out}))
}catch(error){if(browser)try{console.error(await browser.evaluate('document.body.innerText.slice(-6500)'));console.error(JSON.stringify(failedRequests));await Promise.race([writeFile(`${out}/failure.png`,Buffer.from((await browser.call('Page.captureScreenshot',{format:'png'})).data,'base64')),delay(5000)])}catch(diagnostic){console.error(diagnostic.message)}throw error}finally{await browser?.close();for(const user of users)try{await request(`/api/v1/access/users/${encodeURIComponent(user)}`,'DELETE')}catch(error){console.error('临时用户清理失败：'+user+' '+error.message)}}
