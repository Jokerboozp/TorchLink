// Actual source API and browser regression in an explicitly isolated fixture.
// Configuration/analysis records and temporary scoped users are the only writes.
// Source telemetry and connection/dependency histories are preseeded by the host.
import assert from 'node:assert/strict'
import { randomUUID } from 'node:crypto'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import WebSocket from 'ws'
import { startBrowser, delay } from '../helpers/browser.mjs'

if (process.env.IOT_TEST_MONITORING_FIXTURE !== '1') throw Error('请指定隔离连续性验收环境：IOT_TEST_MONITORING_FIXTURE=1')
if (!process.env.IOT_TEST_MONITORING_INFO) throw Error('请指定 IOT_TEST_MONITORING_INFO 固定事实 JSON 文件')
const info = JSON.parse(await readFile(process.env.IOT_TEST_MONITORING_INFO, 'utf8')), fixture = info.monitoring
if (!fixture?.expected?.synthetic || fixture.deviceIds?.length < 2 || !(fixture.end > fixture.start)) throw Error('fixture 须明确至少两台隔离设备和受控事实区间')
if (fixture.start % 1000 || fixture.end % 1000 || fixture.observationStart % 1000 || fixture.observationEnd % 1000) throw Error('fixture 时间须为整秒，与实际日期编辑器精度一致')
const base = process.env.IOT_TEST_BASE_URL || 'http://127.0.0.1:5182', tenant = info.tenantId
const username = process.env.IOT_TEST_ADMIN_USER || 'admin', password = process.env.IOT_TEST_ADMIN_PASSWORD
if (!password) throw Error('验收登录密码未配置')
const out = resolve(process.env.IOT_TEST_OUTPUT || '.e2e/monitoring-browser'), stamp = Date.now()
await mkdir(out, { recursive: true })
let auth = null, browser = null, permissionStage = false
const failedRequests = [], coverage = [], users = []
const path = id => `/api/v1/monitoring-gaps/runs/${encodeURIComponent(id)}`
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
const metrics = outputs => outputs.flatMap(row => row.body.metrics).filter(row => !row.profileId && !row.attributeId)
const metric = (outputs, device, track) => metrics(outputs).find(row => row.deviceId === device && row.track === track)
function assertDurations(outputs, run) {
 const rows = metrics(outputs)
 assert.equal(rows.length, run.deviceIds.length * 3, '每设备三种整体轨道，策略及属性层不重复聚合')
 for (const row of rows) {
  assert.equal(row.plannedMs + row.excludedMs, row.windowMs)
  assert.equal(row.windowMs, run.end - run.start)
  assert.equal(row.availableMs + row.unavailableMs + row.unknownMs + row.notApplicableMs, row.plannedMs)
  if (row.unknownMs > 0 || row.notApplicableMs > 0) assert.equal(row.fullWindowAvailability, undefined)
  const known=row.availableMs+row.unavailableMs
  if(row.plannedMs>0&&row.notApplicableMs===0)assert.equal(row.knownCoverage,known/row.plannedMs,'来源覆盖比例采用已知区间分母，不共用可用率变量')
  if(known>0)assert.equal(row.knownAvailability,row.availableMs/known)
 }
}

try {
 auth = await login(username, password)
 const devices = await Promise.all(fixture.deviceIds.map(id => request(`/api/v1/device-registry/${encodeURIComponent(id)}/connection`)))
 const name = id => devices.find(value => value.device.id === id)?.device.name || id
 const [d1, d2] = fixture.deviceIds
 const before = new Set((await all('/api/v1/monitoring-gaps/profiles')).map(row => row.id))
 browser = await startBrowser({ webSocketImplementation: WebSocket, timeout: 30000, interval: 150, onEvent: event => {
  if (event.method === 'Network.responseReceived') {
   const url = new URL(event.params.response.url)
   if (!permissionStage && url.pathname.startsWith('/api/v1/monitoring-gaps/') && event.params.response.status >= 400) failedRequests.push({ path: url.pathname, status: event.params.response.status })
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
 const range = async (label, start, end) => { await date(label, start, 0); await date(label, end, 1) }
 const select = async (label, text, extra = '') => {
  await evaluate(`(()=>{const dialog=[...document.querySelectorAll('.ui-dialog')].filter(node=>node.getClientRects().length).at(-1);const item=[...dialog.querySelectorAll('.ui-form-item')].find(node=>node.querySelector('label')?.textContent.trim().startsWith(${JSON.stringify(label)}));const control=item?.querySelector('.n-base-selection');if(!control)throw Error('选择器不存在：'+${JSON.stringify(label)});control.click()})()`)
  await until(() => evaluate(`(()=>{const option=[...document.querySelectorAll('.n-base-select-option')].find(node=>node.getClientRects().length&&node.textContent.includes(${JSON.stringify(text)})&&node.textContent.includes(${JSON.stringify(extra)}));option?.click();return !!option})()`), 'option ' + text)
  await evaluate(`(()=>{document.activeElement?.blur();[...document.querySelectorAll('.ui-dialog')].filter(node=>node.getClientRects().length).at(-1)?.querySelector('.n-card-header')?.click()})()`)
  await delay(250) // Wait for a previous teleported menu to finish its close transition.
 }
 const closed = () => until(() => evaluate(`![...document.querySelectorAll('.ui-dialog')].some(node=>node.getClientRects().length)`), 'dialog closed')
 const visibleDialogText = () => evaluate(`([...document.querySelectorAll('.ui-dialog')].filter(node=>node.getClientRects().length).at(-1)?.innerText || '')`)
 const screenshot = async file => writeFile(`${out}/${file}.png`, Buffer.from((await call('Page.captureScreenshot', { format: 'png' })).data, 'base64'))
 const openPage = async () => { await clickSelector('.nav-item[aria-label="监测连续性"]'); await until(() => evaluate('!!document.querySelector(".monitoring-gaps-page")&&!document.querySelector(".monitoring-gaps-page>.ui-skeleton")')); await delay(200) }
 const terminal = async (id, token = auth.accessToken) => until(async () => { const value = await request(path(id), 'GET', undefined, token); return ['SUCCEEDED','PARTIAL','FAILED','CANCELLED'].includes(value.status) ? value : false }, 'real persistent facts complete', 60000)
 await setSession(auth, username); await openPage()
 console.log('连续性验收：配置实际策略')
 await tab('监测策略'); await click('新增监测策略')
 for (const id of fixture.deviceIds) await select('设备范围', name(id))
 await date('生效时间', fixture.start)
 await fill('人工重要性标签', '受控浏览器验收依据 ' + stamp)
 await fill('预期周期（秒）', fixture.periodMs / 1000); await fill('时效容忍（秒）', fixture.toleranceMs / 1000)
 await fill('长缺口门槛（秒', info.monitoringProfileRequest.body.longGapMs / 1000)
 await select('可接受报文类型', '属性上报'); await fill('属性标识 1', fixture.attributeId)
 await click('保存策略版本'); await closed()
 const profile = (await all('/api/v1/monitoring-gaps/profiles')).filter(row => !before.has(row.id)).sort((a,b) => b.createdAt-a.createdAt)[0]
 assert.ok(profile); assert.deepEqual(profile.deviceIds.slice().sort(), fixture.deviceIds.slice().sort()); assert.equal(profile.body.periodMs, fixture.periodMs)
 assert.equal(profile.body.merge, 'ALL'); assert.equal(profile.body.effectiveFrom, fixture.start)
 coverage.push('真实表单保存不可变关键属性、消息与时效策略')
 await screenshot('profiles-desktop')

 const created = []
 async function createRun(profileRow, confirmed = null) {
  await tab('分析任务'); await click('新增连续性分析')
  for (const id of fixture.deviceIds) await select('明确设备范围', name(id))
  await select('固定监测策略版本', `${fixture.attributeId} · 版本 ${profileRow.version}`, await evaluate(`new Date(${profileRow.createdAt}).toLocaleString('zh-CN',{hour12:false})`))
  await range('分析时间区间', fixture.start, fixture.end)
  if (confirmed) await select('已确认观察窗口', `${name(d1)} · 计划停运`)
  if (fixture.qualityRunId) await select('固定数据质量任务', name(d1), await evaluate(`new Date(${fixture.start}).toLocaleString('zh-CN',{hour12:false})`))
  const chosen=await evaluate(`(()=>{const dialog=[...document.querySelectorAll('.ui-dialog')].filter(node=>node.getClientRects().length).at(-1);return [...dialog.querySelectorAll('.ui-form-item')].map(item=>({label:item.querySelector('label')?.textContent,text:item.querySelector('.n-base-selection')?.innerText,inputs:[...item.querySelectorAll('input')].map(input=>input.value)}))})()`); console.log('连续性验收：所选参数 '+JSON.stringify(chosen))
  await click('开始连续性计算'); await closed()
  const run = await until(async () => (await all('/api/v1/monitoring-gaps/runs')).find(row => row.createdAt >= stamp && row.parameters.profileRevisionIds.includes(profileRow.id) && !created.includes(row.id)), 'run persisted')
  created.push(run.id)
  const ready = await terminal(run.id)
  assert.ok(['SUCCEEDED','PARTIAL'].includes(ready.status), ready.error || '事实计算失败')
  await until(() => evaluate('!!document.querySelector(".monitoring-results .monitor-stat-grid")'), 'actual fixed metrics rendered')
  assert.equal(ready.start, fixture.start); assert.equal(ready.end, fixture.end)
  return ready
 }
 console.log('连续性验收：连接、接收与首次可查询分别计算')
 const first = await createRun(profile)
 const firstMetrics = await all(path(first.id) + '/metrics'), firstSnapshot = await request(path(first.id) + '/snapshot')
 assertDurations(firstMetrics, first)
 assert.equal(metric(firstMetrics, d1, 'connection').availableMs, fixture.expected.connectionOnlineMs)
 assert.equal(metric(firstMetrics, d1, 'connection').unavailableMs, fixture.expected.connectionOfflineMs)
 assert.equal(metric(firstMetrics, d2, 'connection').availableMs, fixture.expected.connectionSecondOnlineMs)
 assert.equal(metric(firstMetrics, d1, 'data').availableMs, 0, '平台 ACK 迟于事件时效不能追溯制造有效数据')
 assert.equal(metric(firstMetrics, d2, 'data').availableMs, 0, '父设备上报不能刷新静默子设备')
 assert.ok(metric(firstMetrics, d1, 'received').availableMs > 0, '真实 receivedAt 与 ACK 可查询时间分开')
 assert.equal(firstSnapshot.statistics.tracks.connection.windowMs, 2 * (fixture.end-fixture.start))
 if (fixture.qualityRunId) assert.equal(firstSnapshot.statistics.qualitySnapshots[0].runId, fixture.qualityRunId)
 const firstEvidence = await all(path(first.id) + '/evidence')
 const measurement = firstEvidence.find(row => row.summary?.measurement)
 assert.ok(measurement?.summary.measurement.availableAt > fixture.end, '代表证据保留真实迟到 ACK 时钟')
 await clickSelector('.monitor-time-piece.monitor-state-AVAILABLE')
 await until(async () => (await visibleDialogText()).includes('区间与代表来源依据'))
 assert.ok((await visibleDialogText()).includes('可查询'), '真实证据详情展示接收与首次可查询时钟')
 await click('关闭'); await closed(); await screenshot('results-desktop')
 coverage.push('40秒独立手算连接30/10秒、静默子设备与迟到可用时钟不混淆')

 console.log('连续性验收：确认停运与版本重算')
 await tab('观察窗口'); await click('新增观察窗口'); await select('设备', name(d1)); await select('观察类型', '计划停运')
 await range('实际观察区间', fixture.observationStart, fixture.observationEnd)
 await fill('实际原因', '隔离环境已确认的五秒检修计划 ' + stamp); await fill('观察依据', '受控验收窗口，仅用于验证人工确认后分母排除。')
 await click('保存观察草稿'); await closed()
 const draft = (await all('/api/v1/monitoring-gaps/observations')).find(row => row.createdAt >= stamp && row.body.reason.includes(String(stamp)))
 assert.equal(draft.body.status, 'DRAFT')
 await clickSelector(`[data-monitor-observation="${draft.id}"]`)
 const confirmed = await until(async () => (await all('/api/v1/monitoring-gaps/observations')).find(row => row.resourceId === draft.resourceId && row.body.status === 'CONFIRMED'), 'human confirmation persisted')
 assert.ok(confirmed.body.confirmedBy); assert.notEqual(confirmed.id, draft.id)
 await tab('监测策略'); await clickSelector(`[data-monitor-profile="${profile.id}"]`)
 await fill('人工重要性标签', '第二版受控验证重点 ' + stamp); await click('保存策略版本'); await closed()
 const revision = (await all('/api/v1/monitoring-gaps/profiles')).find(row => row.resourceId === profile.resourceId && row.version === profile.version + 1)
 assert.ok(revision); assert.notEqual(revision.id, profile.id)
 const second = await createRun(revision, confirmed), secondMetrics = await all(path(second.id) + '/metrics')
 assertDurations(secondMetrics, second)
 const excluded = fixture.observationEnd-fixture.observationStart
 for (const track of ['connection','received','data']) {
  assert.equal(metric(secondMetrics, d1, track).excludedMs, excluded)
  assert.equal(metric(secondMetrics, d1, track).plannedMs, fixture.end-fixture.start-excluded)
  assert.equal(metric(secondMetrics, d2, track).excludedMs, 0)
 }
 assert.deepEqual(await all(path(first.id) + '/metrics'), firstMetrics, '新停运与策略版本不重写第一份固定事实')
 coverage.push('草稿另行人工确认、五秒排除仅作用于对应设备、新旧任务事实独立')

 console.log('连续性验收：授权依赖假设与人工核实')
 const groupOutputs = await all(path(second.id) + '/dependency-groups'), groups = groupOutputs.map(row => ({ ...row.body, id: row.id }))
 assert.ok(groups.length)
 for (const g of groups) {
  assert.ok(g.memberIds.every(id => fixture.deviceIds.includes(id))); assert.equal(g.analysisDeviceCount, fixture.deviceIds.length)
  assert.ok(!g.memberIds.includes(info.hiddenDeviceId)); assert.notEqual(g.resourceId, info.hiddenDeviceId)
 }
 const group = groups.find(row => row.kind === 'access-profile' && row.resourceId === info.dependencyProfileId) || groups.find(row => row.memberIds.length === 2)
 assert.ok(group); assert.equal(group.historyQuality, 'CURRENT_ONLY')
 await clickSelector(`[data-monitor-group="${group.id}"]`)
 assert.ok((await visibleDialogText()).includes('不进行历史推断'))
 await click('计算固定假设影响')
 const hypothesis = await until(async () => (await all(path(second.id) + '/hypotheses')).find(row => row.groupId === group.id), 'hypothesis persisted')
 assert.equal(hypothesis.at, group.start); assert.deepEqual(hypothesis.deviceIds.slice().sort(), group.memberIds.slice().sort())
 await until(async () => (await visibleDialogText()).includes('假设影响本次可见设备'))
 await viewport(390,844); await delay(250)
 assert.ok(await evaluate(`(()=>{const dialog=[...document.querySelectorAll('.ui-dialog')].filter(node=>node.getClientRects().length).at(-1),body=dialog.querySelector('.monitor-group-body');body.scrollTop=body.scrollHeight;const b=dialog.getBoundingClientRect();return b.left>=0&&b.right<=innerWidth+2&&body.scrollTop>0&&body.scrollWidth<=body.clientWidth+2})()`), '390px依赖详情独立滚动且不溢出')
 await screenshot('dependency-390'); await click('关闭'); await closed(); await viewport()
 const findings = await all(path(second.id) + '/findings'), finding = findings.find(row => row.deviceId === d1) || findings[0]
 assert.ok(finding)
 await clickSelector(`[data-monitor-finding="${finding.id}"]`); await select('核实结果', '继续观察')
 const explanation = '浏览器已核对三个时钟与检修分母；实际现场原因仍待核实。'
 await fill('实际核实依据', explanation); await click('保存监测核实记录')
 const review = await until(async () => (await all(path(second.id) + '/reviews')).find(row => row.resourceId === finding.id && row.explanation === explanation), 'actual human review persisted')
 assert.equal(review.result, 'OBSERVE'); await until(async () => (await visibleDialogText()).includes(explanation))
 await viewport(390,844); await delay(200)
 assert.ok(await evaluate(`(()=>{const dialog=[...document.querySelectorAll('.ui-dialog')].filter(node=>node.getClientRects().length).at(-1),body=dialog.querySelector('.monitor-fact-body');body.scrollTop=body.scrollHeight;const b=dialog.getBoundingClientRect();return b.left>=0&&b.right<=innerWidth+2&&body.scrollTop>0&&body.scrollWidth<=body.clientWidth+2})()`), '390px依据与核实详情独立滚动')
 await screenshot('finding-390'); await click('关闭'); await closed(); await viewport()
 coverage.push('当前依赖仅使用获授权固定成员、假设个人记录持久化、人工四状态之一真实提交')

 if (fixture.qualityRunId) {
  console.log('连续性验收：跳转精确固定质量任务')
  await until(() => evaluate(`(()=>{const node=[...document.querySelectorAll('.n-collapse-item__header-main')].find(row=>row.textContent.includes('本任务固定引用的数据质量版本'));node?.click();return !!node})()`))
  await clickSelector(`[data-monitor-quality-run="${fixture.qualityRunId}"]`)
  await until(() => evaluate('!!document.querySelector(".data-quality-page .quality-results .quality-ratio-grid")'), 'linked quality task rendered')
  const cached = await evaluate(`Object.entries(sessionStorage).filter(([key])=>key.startsWith('iot:quality-run:')).map(([,value])=>value)`)
  assert.ok(cached.includes(fixture.qualityRunId), '关联任务按指定ID读取与恢复，不退回旧缓存')
  await openPage(); await until(() => evaluate('!!document.querySelector(".monitoring-results .monitor-stat-grid")'))
  const unavailableId=randomUUID()
  // Use the public navigation state after the ordinary click, before Vue mounts
  // its destination; neither component state nor network facts are injected.
  await evaluate(`(()=>{document.querySelector(${JSON.stringify('.nav-item[aria-label="数据质量"]')}).click();sessionStorage.setItem('iot:navigation-detail',${JSON.stringify(JSON.stringify({runId:unavailableId}))})})()`)
  await until(() => evaluate('document.querySelector(".data-quality-page")?.innerText.includes("关联的固定数据质量任务已不存在或超出当前授权范围")'))
  assert.ok(!await evaluate('!!document.querySelector(".quality-results .quality-ratio-grid")'),'无效固定任务不能悄悄恢复其它缓存任务')
  await openPage(); await until(() => evaluate('!!document.querySelector(".monitoring-results .monitor-stat-grid")'))
  coverage.push('连续性固定质量引用按runId跳转；失效版本明确错误且不回退缓存')
 }
 await call('Page.reload'); await openPage(); await until(() => evaluate('!!document.querySelector(".monitoring-results .monitor-stat-grid")'))
 await clickSelector(`[data-monitor-group="${group.id}"]`); await until(async () => (await visibleDialogText()).includes('假设影响本次可见设备'))
 await click('关闭'); await closed()
 assert.deepEqual(await all(path(second.id) + '/metrics'), secondMetrics)
 coverage.push('刷新后恢复固定任务、人工核实及已保存假设')
 if (process.env.IOT_TEST_MONITORING_AI === '1') {
  console.log('连续性验收：显式AI解读与固定事实分离')
  await click('开始解读')
  const ai = await until(async () => (await all(path(second.id) + '/ai-jobs')).find(row => ['SUCCEEDED','FAILED','CANCELLED'].includes(row.status)), 'explicit AI reaches terminal', 60000)
  assert.ok(['SUCCEEDED','FAILED','CANCELLED'].includes(ai.status))
  if (ai.status === 'SUCCEEDED') for (const key of ['observedWeaknesses','prioritizedChecks','dependencyObservations','limitations']) assert.ok(Array.isArray(ai.interpretation[key]))
  assert.deepEqual(await all(path(second.id) + '/metrics'), secondMetrics)
  coverage.push(`显式AI ${ai.status}，原事实与人工核实仍可使用`)
 }
 await click('导出连续性报告')
 const report = await until(async () => { try { return JSON.parse(await readFile(`${out}/监测连续性_${second.id}.json`, 'utf8')) } catch { return false } }, 'real browser export download')
 assert.equal(report.run.id, second.id); assert.ok(report.reviews.some(row => row.id === review.id)); assert.ok(report.hypotheses.some(row => row.id === hypothesis.id))
 await viewport(390,844)
 for (const label of ['分析任务','监测策略','观察窗口']) {
  await tab(label); await delay(200)
  assert.ok(await evaluate('document.documentElement.scrollWidth<=innerWidth+2&&document.querySelector(".monitoring-gaps-page").getBoundingClientRect().right<=innerWidth+2'), label + ' 390px无整页横向溢出')
  await screenshot(`${label}-390`)
 }
 coverage.push('真实导出包含固定区间、来源、核实与假设；390px三页面无整页溢出')
 assert.equal(failedRequests.length, 0, '正常任务路径无失败HTTP响应')

 console.log('连续性验收：真实用户权限与撤销范围')
 permissionStage = true; await viewport()
 const makeUser = async (suffix, permissions) => {
  const user = `continuity-${stamp}-${suffix}`, secret = randomUUID() + 'Aa1!'
  await request('/api/v1/access/users','POST',{username:user,displayName:'隔离连续性验收 '+suffix,password:secret,enabled:true,roleIds:[],permissions,deviceScope:'selected',deviceIds:[d1]})
  users.push(user); return {user,secret,identity:await login(user,secret),permissions}
 }
 const menuOnly = await makeUser('menu', ['menu:monitoringGaps'])
 assert.equal((await response('/api/v1/monitoring-gaps/profiles','GET',undefined,menuOnly.identity.accessToken)).status,403)
 await setSession(menuOnly.identity,menuOnly.user); await openPage()
 await until(() => evaluate('document.querySelector(".monitoring-gaps-page")?.innerText.includes("需要设备管理读取权限")'))
 assert.ok(!await evaluate('!!document.querySelector(".monitoring-results .monitor-stat-grid")'))
 assert.ok(!await evaluate(`[...document.querySelectorAll('.monitoring-gaps-page button')].some(node=>node.textContent.includes('新增连续性分析'))`))
 const scoped = await makeUser('scope',['menu:devices','menu:monitoringGaps','POST /api/v1/monitoring-gaps/profiles','POST /api/v1/monitoring-gaps/runs','GET /api/v1/monitoring-gaps/runs/:id/export'])
 assert.equal((await response(path(second.id),'GET',undefined,scoped.identity.accessToken)).status,403,'单设备用户不能读取多设备完整快照')
 assert.equal((await response(path(second.id)+'/export','GET',undefined,scoped.identity.accessToken)).status,403)
 const scopedProfile = await request('/api/v1/monitoring-gaps/profiles','POST',{resourceId:randomUUID(),expectedVersion:0,scope:'personal',deviceIds:[d1],body:profile.body},scoped.identity.accessToken)
 const scopedRun = await request('/api/v1/monitoring-gaps/runs','POST',{deviceIds:[d1],start:fixture.start,end:fixture.end,configurationVersion:scopedProfile.id,parameters:{profileRevisionIds:[scopedProfile.id],observationRevisionIds:[]},idempotencyKey:randomUUID()},scoped.identity.accessToken)
 const scopedReady = await terminal(scopedRun.id,scoped.identity.accessToken)
 assert.ok(['SUCCEEDED','PARTIAL'].includes(scopedReady.status))
 const scopedGroups = await all(path(scopedRun.id)+'/dependency-groups',scoped.identity.accessToken)
 for(const row of scopedGroups){assert.equal(row.body.analysisDeviceCount,1);assert.deepEqual(row.body.memberIds,[d1]);assert.notEqual(row.body.resourceId,info.hiddenDeviceId)}
 await setSession(scoped.identity,scoped.user); await openPage(); await click('查看结果')
 await until(() => evaluate('!!document.querySelector(".monitoring-results .monitor-stat-grid")'))
 assert.ok(!await evaluate(`document.querySelector('.monitoring-gaps-page').innerText.includes(${JSON.stringify(name(d2))})`),'页面不泄露第二设备')
 await request(`/api/v1/access/users/${scoped.user}`,'PUT',{displayName:'隔离连续性验收 scope',enabled:true,roleIds:[],permissions:scoped.permissions,deviceScope:'none',deviceIds:[]})
 scoped.identity=await login(scoped.user,scoped.secret)
 assert.equal((await response(path(scopedRun.id),'GET',undefined,scoped.identity.accessToken)).status,403)
 assert.equal((await response(path(scopedRun.id)+'/export','GET',undefined,scoped.identity.accessToken)).status,403)
 await setSession(scoped.identity,scoped.user); await openPage()
 await until(() => evaluate('!!document.querySelector(".monitoring-gaps-page .ui-alert")'))
 assert.ok(!await evaluate('!!document.querySelector(".monitoring-results .monitor-stat-grid")'),'设备范围撤销后旧快照不可继续展示')
 coverage.push('仅应用菜单拒绝设备读、单设备成员分母、整任务及导出越权403、撤销范围后清除旧事实')
 assert.equal(browser.errors.length,0,'浏览器无运行异常')
 await writeFile(`${out}/report.json`,JSON.stringify({passed:true,coverage,runIds:created,source:'isolated-real-postgresql-source-api',screenshots:out},null,2))
 console.log(JSON.stringify({passed:true,coverage,screenshots:out}))
} catch (error) {
 if(browser){try{console.error(await browser.evaluate('document.body.innerText.slice(-6500)'));console.error(JSON.stringify(failedRequests));await Promise.race([writeFile(`${out}/failure.png`,Buffer.from((await browser.call('Page.captureScreenshot',{format:'png'})).data,'base64')),delay(5000)])}catch(diagnostic){console.error('Browser diagnostic:',diagnostic.message)}}
 throw error
} finally {
 await browser?.close()
 for(const user of users)try{await request(`/api/v1/access/users/${encodeURIComponent(user)}`,'DELETE')}catch(error){console.error('临时用户清理失败：'+user+' '+error.message)}
}
