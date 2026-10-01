// Real HTTP/UI regression against an explicitly isolated, preseeded fixture.
// This script creates analysis/configuration records, never device telemetry,
// production alarms, commands, or network mocks. Every browser is disposable.
import assert from 'node:assert/strict'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import WebSocket from 'ws'
import { startBrowser, delay } from '../helpers/browser.mjs'

if (process.env.IOT_TEST_QUALITY_FIXTURE !== '1') throw Error('请指定隔离质量验收环境：IOT_TEST_QUALITY_FIXTURE=1')
const infoPath = process.env.IOT_TEST_QUALITY_INFO
if (!infoPath) throw Error('请指定已预置真实持久测量的 IOT_TEST_QUALITY_INFO JSON 文件')
const info = JSON.parse(await readFile(infoPath, 'utf8'))
const base = process.env.IOT_TEST_BASE_URL || 'http://127.0.0.1:5182'
const tenant = info.tenantId
const username = process.env.IOT_TEST_ADMIN_USER || 'admin'
const password = process.env.IOT_TEST_ADMIN_PASSWORD
if (!password) throw Error('验收登录密码未配置')
const fixtureProfile = info.profileRequest.body
const deviceID = info.deviceId, attributeID = fixtureProfile.attributeId
if (!deviceID || !attributeID || !(info.end > info.start)) throw Error('fixture 缺少明确设备、属性和窗口')
const output = resolve(process.env.IOT_TEST_OUTPUT || '.e2e/data-quality-browser')
await mkdir(output, { recursive: true })
const stamp = Date.now(), failedRequests = [], coverage = []

async function request(path, method = 'GET', body, token = auth?.accessToken) {
  const response = await fetch(base + path, { method, headers: { ...(token ? { Authorization: 'Bearer ' + token } : {}), ...(body ? { 'Content-Type': 'application/json' } : {}) }, body: body ? JSON.stringify(body) : undefined })
  const data = await response.json().catch(() => ({}))
  if (!response.ok) throw Error(`${method} ${path}: ${response.status} ${data.detail || data.message || ''}`)
  return data
}
let auth = null, browser = null
auth = await request('/api/v1/auth/login', 'POST', { tenantId: tenant, username, password }, '')
const fixtureDevice = await request(`/api/v1/device-registry/${encodeURIComponent(deviceID)}/connection`)
assert.equal(fixtureDevice.device.id, deviceID)
const fixtureDeviceName = fixtureDevice.device.name || deviceID
const profilesBefore = new Set((await request('/api/v1/data-quality/profiles?limit=100')).items.map(row => row.id))

try {
  browser = await startBrowser({ webSocketImplementation: WebSocket, timeout: 30000, interval: 100, onEvent: event => {
    if (event.method === 'Network.responseReceived' && new URL(event.params.response.url).pathname.startsWith('/api/v1/data-quality/') && event.params.response.status >= 400) failedRequests.push({ path: new URL(event.params.response.url).pathname, status: event.params.response.status })
  } })
  const { call, evaluate, until } = browser
  await call('Page.enable'); await call('Runtime.enable'); await call('Network.enable')
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1050, deviceScaleFactor: 1, mobile: false })
  await call('Page.navigate', { url: base })
  await until(() => evaluate('!!document.querySelector(".login-form")'))
  await evaluate(`(()=>{const values=${JSON.stringify({ iot_token: auth.accessToken, iot_tenant: tenant, iot_user: username, iot_role: auth.role || 'admin', iot_permissions: JSON.stringify(auth.permissions || ['*']), iot_access_version: auth.accessVersion || '' })};for(const [key,value] of Object.entries(values))localStorage.setItem(key,value)})()`)
  await call('Page.reload')
  const click = text => until(() => evaluate(`(()=>{const button=[...document.querySelectorAll('button')].find(node=>node.getClientRects().length&&!node.disabled&&node.textContent.trim()===${JSON.stringify(text)});button?.click();return !!button})()`), 'button ' + text)
  const tab = async text => { await until(() => evaluate(`(()=>{const value=[...document.querySelectorAll('.n-tabs-tab')].find(node=>node.getClientRects().length&&node.textContent.trim()===${JSON.stringify(text)});value?.click();return !!value})()`), 'tab ' + text); await delay(150) }
  const fill = async (label, value, index = 0) => evaluate(`(()=>{const dialog=[...document.querySelectorAll('.ui-dialog')].filter(node=>node.getClientRects().length).at(-1);const item=[...dialog.querySelectorAll('.ui-form-item')].find(node=>node.querySelector('label')?.textContent.trim().startsWith(${JSON.stringify(label)}));const field=item?.querySelectorAll('input,textarea')[${index}];if(!field)throw Error('表单字段不存在：'+${JSON.stringify(label)});field.focus();field.value=${JSON.stringify(String(value))};field.dispatchEvent(new Event('input',{bubbles:true}));field.dispatchEvent(new Event('change',{bubbles:true}));field.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',code:'Enter',bubbles:true}));field.blur()})()`)
  const localDate = async timestamp => evaluate(`(()=>{const value=new Date(${timestamp});const pad=n=>String(n).padStart(2,'0');return value.getFullYear()+'-'+pad(value.getMonth()+1)+'-'+pad(value.getDate())+' '+pad(value.getHours())+':'+pad(value.getMinutes())+':'+pad(value.getSeconds())})()`)
  const date = async (label, timestamp, index = 0) => fill(label, await localDate(timestamp), index)
  const range = async (label, start, end) => { await date(label, start, 0); await date(label, end, 1) }
  const select = async (label, text, extra = '') => {
    await evaluate(`(()=>{const dialog=[...document.querySelectorAll('.ui-dialog')].filter(node=>node.getClientRects().length).at(-1);const item=[...dialog.querySelectorAll('.ui-form-item')].find(node=>node.querySelector('label')?.textContent.trim().startsWith(${JSON.stringify(label)}));const control=item?.querySelector('.n-base-selection');if(!control)throw Error('选择器不存在：'+${JSON.stringify(label)});control.click()})()`)
    await until(() => evaluate(`(()=>{const option=[...document.querySelectorAll('.n-base-select-option')].find(node=>node.getClientRects().length&&node.textContent.includes(${JSON.stringify(text)})&&node.textContent.includes(${JSON.stringify(extra)}));option?.click();return !!option})()`), 'option ' + text)
    // A click outside closes a multi-select without dismissing the dialog.
    await evaluate(`(()=>{document.activeElement?.blur();[...document.querySelectorAll('.ui-dialog')].filter(node=>node.getClientRects().length).at(-1)?.querySelector('.n-card-header')?.click()})()`)
  }
  const checkbox = text => evaluate(`(()=>{const node=[...document.querySelectorAll('.n-checkbox')].find(node=>node.getClientRects().length&&node.textContent.trim()===${JSON.stringify(text)});if(!node)throw Error('勾选项不存在：'+${JSON.stringify(text)});node.click()})()`)
  const closed = () => until(() => evaluate(`![...document.querySelectorAll('.ui-dialog')].some(node=>node.getClientRects().length)`), 'dialog closed')
  const screenshot = async name => writeFile(`${output}/${name}.png`, Buffer.from((await call('Page.captureScreenshot', { format: 'png' })).data, 'base64'))
  const openPage = async () => {
    const selector = JSON.stringify('.nav-item[aria-label="上报数据检查"]')
    await until(() => evaluate(`!!document.querySelector(${selector})`))
    await evaluate(`document.querySelector(${selector}).click()`)
    await until(() => evaluate('!!document.querySelector(".data-quality-page")&&!document.querySelector(".data-quality-page>.ui-skeleton")'))
  }
  console.log('数据质量验收：打开配置页面')
  await openPage()
  await tab('质量配置'); await click('新建配置')
  await select('设备范围', fixtureDeviceName)
  await fill('属性标识', attributeID)
  // Naive datetime editors have second precision. The fixed anchor carries the
  // same subsecond phase as fixture readings; rounded anchor + tolerance below
  // is intentionally recorded and checked as an operator-supplied profile.
  const start = Math.floor(info.start / 1000) * 1000, end = Math.ceil(info.end / 1000) * 1000
  await date('生效时间', start)
  await date('固定上报锚点', start)
  const periodSeconds = fixtureProfile.periodMs / 1000
  await fill('上报周期（秒）', periodSeconds)
  await fill('周期容忍（秒）', periodSeconds / 2)
  await fill('物理单位', fixtureProfile.unit)
  await checkbox('单位已确认')
  await fill('量程下限', fixtureProfile.minimum ?? 0)
  await fill('量程上限', fixtureProfile.maximum ?? 500)
  await checkbox('单位和量程已核实')
  await fill('精度 epsilon', fixtureProfile.epsilon ?? .1)
  await fill('允许稳定时长（秒）', fixtureProfile.stableDurationMs / 1000 || 20)
  await fill('连续样本最大间隔（秒）', fixtureProfile.maxSequenceGapMs / 1000 || 2)
  await click('保存配置版本')
  await until(() => evaluate('document.querySelector(".quality-dialog-body")?.innerText.includes("严格小于半个上报周期")'))
  coverage.push('半周期容忍通过真实表单被拒绝')
  await fill('周期容忍（秒）', Math.max(fixtureProfile.toleranceMs / 1000, .49 * periodSeconds))
  await click('保存配置版本'); await closed()
  const savedProfiles = (await request('/api/v1/data-quality/profiles?limit=100')).items.filter(row => !profilesBefore.has(row.id) && row.body.attributeId === attributeID && row.deviceIds.includes(deviceID)).sort((a, b) => b.createdAt - a.createdAt)
  const profile = savedProfiles[0]
  assert.ok(profile, '配置由 UI 真实持久化')
  assert.equal(profile.body.periodMs, fixtureProfile.periodMs)
  assert.equal(profile.body.scheduleAnchor, start)
  assert.equal(profile.body.minimumSamples, 30)
  coverage.push('真实 UI 配置保存与固定锚点持久化')
  console.log('数据质量验收：配置已持久化，开始正常样本分析')
  await screenshot('profiles-desktop')

  async function createRun(profileVersion) {
    await tab('分析任务'); await click('新建分析')
    await select('明确设备范围', fixtureDeviceName)
    const selectedProfile = profileVersion === 1 ? profile : strictProfile
    const createdAtLabel = await evaluate(`new Date(${selectedProfile.createdAt}).toLocaleString('zh-CN',{hour12:false})`)
    await select('固定质量配置版本', `${attributeID} · 版本 ${profileVersion}`, createdAtLabel)
    await range('分析时间区间', start, end)
    await click('开始事实计算'); await closed()
    const run = await until(async () => (await request(`/api/v1/data-quality/runs?deviceId=${encodeURIComponent(deviceID)}&limit=100`)).items.find(row => row.createdAt >= stamp && row.parameters.profileRevisionIds.includes(profileVersion === 1 ? profile.id : strictProfile.id)), 'persistent run created')
    const completed = await until(async () => { const value = await request(`/api/v1/data-quality/runs/${encodeURIComponent(run.id)}`); return ['SUCCEEDED', 'PARTIAL', 'FAILED', 'CANCELLED'].includes(value.status) ? value : false }, 'persistent facts completed', 60000)
    assert.ok(['SUCCEEDED', 'PARTIAL'].includes(completed.status), completed.error || 'facts did not succeed')
    await until(() => evaluate('!!document.querySelector(".quality-results .quality-ratio-grid")'), 'frozen metrics visible')
    return completed
  }
  const normalRun = await createRun(1)
  const normalMetrics = (await request(`/api/v1/data-quality/runs/${normalRun.id}/metrics?limit=100`)).items[0].body
  assert.equal(normalMetrics.metrics[0].format.numerator, 0)
  assert.equal(normalMetrics.metrics[0].range.numerator, 0)
  assert.equal(normalMetrics.metrics[0].range.denominator, info.sampleCount)
  const series = await request(`/api/v1/data-quality/runs/${normalRun.id}/series?deviceId=${encodeURIComponent(deviceID)}&attributeId=${encodeURIComponent(attributeID)}&limit=1000`)
  assert.equal(series.originalCount, info.sampleCount)
  assert.equal(series.returnedCount, info.sampleCount)
  assert.ok(series.items.every(row => row.receivedAt > row.eventAt && row.availableAt > row.receivedAt), '三种真实时间分别保留')
  await screenshot('normal-facts-desktop')
  coverage.push('40 条 SQL 真实持久序列指标与三种时间读取')
  console.log('数据质量验收：正常事实已完成，建立量程版本并核实')

  // A deliberately narrower isolated analysis threshold exercises review and
  // immutable recomputation; it never updates the device or raises an alarm.
  await tab('质量配置')
  await evaluate(`document.querySelector('[data-quality-revision="'+${JSON.stringify(profile.id)}+'"]')?.click()`)
  await fill('量程上限', 102)
  await click('保存配置版本'); await closed()
  const strictProfile = (await request('/api/v1/data-quality/profiles?limit=100')).items.find(row => row.resourceId === profile.resourceId && row.version === 2)
  assert.ok(strictProfile, '旧配置保留，新配置创建独立版本')
  const strictRun = await createRun(2)
  const strictMetrics = (await request(`/api/v1/data-quality/runs/${strictRun.id}/metrics?limit=100`)).items[0].body
  assert.equal(strictMetrics.metrics[0].range.denominator, info.sampleCount)
  const expectedAbove = series.items.filter(row => Number(row.value) > 102).length
  assert.equal(strictMetrics.metrics[0].range.numerator, expectedAbove)
  assert.ok(expectedAbove > 0)
  const findings = (await request(`/api/v1/data-quality/runs/${strictRun.id}/findings?limit=100`)).items
  const rangeFinding = findings.find(row => row.body.kind === 'range_above')
  assert.ok(rangeFinding)
  await evaluate(`(()=>{const row=[...document.querySelectorAll('.quality-results .n-data-table-tbody tr')].find(node=>node.innerText.includes('高于确认量程'));const button=[...row.querySelectorAll('button')].find(node=>node.textContent.trim()==='依据与核实');button.click()})()`)
  await until(() => evaluate('!!document.querySelector(".quality-details-body")'))
  await select('核实结果', '存在正常解释')
  await fill('实际核实依据', '隔离验收临时收窄阈值，正常样本未改变；该提示用于核实记录与冻结版本回归。')
  await click('保存核实记录')
  await until(async () => (await request(`/api/v1/data-quality/findings/${encodeURIComponent(rangeFinding.id)}/reviews?runId=${encodeURIComponent(strictRun.id)}`)).items.some(row => row.result === 'NORMAL_EXPLANATION'), 'human review persisted')
  coverage.push('新配置重算、明确分母与独立人工正常解释记录')
  await call('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true }); await delay(250)
  assert.ok(await evaluate(`(()=>{const dialog=[...document.querySelectorAll('.ui-dialog')].find(node=>node.getClientRects().length),body=document.querySelector('.quality-details-body');const bounds=dialog.getBoundingClientRect();body.scrollTop=body.scrollHeight;return bounds.left>=0&&bounds.right<=innerWidth+2&&body.scrollTop>0&&body.scrollWidth<=body.clientWidth+2})()`), '390px 发现正文独立滚动，表格内部滚动')
  await screenshot('finding-390'); await click('关闭'); await closed()
  await call('Emulation.setDeviceMetricsOverride', { width: 1440, height: 1050, deviceScaleFactor: 1, mobile: false })
  const firstAgain = (await request(`/api/v1/data-quality/runs/${normalRun.id}/metrics?limit=100`)).items[0].body
  assert.deepEqual(firstAgain, normalMetrics, '新配置和人工记录不更改原冻结指标')
  console.log('数据质量验收：核实记录已保存，开始基线计算和确认')

  await tab('确认基线'); await click('建立基线')
  await select('设备', fixtureDeviceName); await select('固定质量配置版本', `${attributeID} · 版本 1`, await evaluate(`new Date(${profile.createdAt}).toLocaleString('zh-CN',{hour12:false})`))
  await fill('人工确认工况', info.baselineRequest.operatingCondition)
  await fill('协议版本', info.baselineRequest.protocolVersion)
  await fill('点表 / 配置版本', info.baselineRequest.configurationVersion)
  await range('固定基线样本区间', start, end)
  await date('有效期开始', end); await date('有效期结束', Math.ceil(info.baselineRequest.validUntil / 1000) * 1000)
  await click('计算并保存基线'); await closed()
  const baseline = (await request('/api/v1/data-quality/baselines?limit=100')).items.find(row => row.body.profileRevisionId === profile.id)
  assert.equal(baseline.body.sampleCount, info.sampleCount)
  assert.equal(baseline.body.confirmedBy || '', '')
  await until(() => evaluate(`(()=>{const button=document.querySelector('[data-quality-baseline="'+${JSON.stringify(baseline.id)}+'"]');button?.click();return !!button})()`), 'confirm own baseline')
  await until(async () => (await request('/api/v1/data-quality/baselines?limit=100')).items.some(row => row.resourceId === baseline.resourceId && row.body.confirmedBy), 'human baseline confirmation')
  coverage.push('从真实来源计算40样本基线及独立人工确认')
  console.log('数据质量验收：基线确认完成，开始实际附件上传')

  await tab('校准记录'); await click('新增校准记录')
  await select('设备', fixtureDeviceName); await fill('属性标识', attributeID)
  await date('实际校准日期', start); await fill('实施人', '隔离验收实施人'); await fill('校准依据', '隔离来源校准记录，仅验证资料与附件持久化，不改写测量或告警。')
  const attachment = resolve(output, 'calibration-fixture.txt')
  const attachmentText = '校准附件隔离验收证据。'
  await writeFile(attachment, attachmentText)
  const document = await call('DOM.getDocument', { depth: -1 })
  const fileNode = await call('DOM.querySelector', { nodeId: document.root.nodeId, selector: '.quality-file-label input[type="file"]' })
  await call('DOM.setFileInputFiles', { nodeId: fileNode.nodeId, files: [attachment] })
  await until(() => evaluate('[...document.querySelectorAll(".ui-dialog")].filter(node=>node.getClientRects().length).at(-1)?.querySelector(".quality-dialog-body")?.innerText.includes("calibration-fixture.txt")'), 'attachment uploaded through file picker')
  await click('保存校准记录'); await closed()
  const calibration = (await request('/api/v1/data-quality/calibrations?limit=100')).items.find(row => row.createdAt >= stamp && row.body.deviceId === deviceID)
  assert.ok(calibration.body.attachments.length)
  const fileResponse = await fetch(base + `/api/v1/data-quality/calibrations/attachments/${encodeURIComponent(calibration.body.attachments[0])}`, { headers: { Authorization: 'Bearer ' + auth.accessToken } })
  assert.equal(fileResponse.status, 200)
  assert.equal(await fileResponse.text(), attachmentText)
  coverage.push('校准日期/依据与真实附件上传下载')
  console.log('数据质量验收：附件下载核对完成，验证刷新和报告')

  await tab('分析任务')
  await call('Page.reload'); await openPage()
  await until(() => evaluate('!!document.querySelector(".quality-results .quality-ratio-grid")'), 'persisted selected run restored')
  await call('Browser.setDownloadBehavior', { behavior: 'allow', downloadPath: output })
  await click('导出报告')
  const downloadedReport = await until(async () => {
    try { return JSON.parse(await readFile(`${output}/数据质量_${strictRun.id}.json`, 'utf8')) } catch (error) { if (error.code === 'ENOENT' || error instanceof SyntaxError) return false; throw error }
  }, 'report downloaded through real UI')
  assert.equal(downloadedReport.run.id, strictRun.id)
  assert.ok(downloadedReport.reviews.some(row => row.resourceId === rangeFinding.id && row.result === 'NORMAL_EXPLANATION'))
  const reportResponse = await fetch(base + `/api/v1/data-quality/runs/${encodeURIComponent(strictRun.id)}/export`, { headers: { Authorization: 'Bearer ' + auth.accessToken } })
  assert.equal(reportResponse.status, 200)
  const report = await reportResponse.json()
  await writeFile(`${output}/report.json`, JSON.stringify(report, null, 2))
  coverage.push('刷新恢复固定任务与无需模型的报告导出')
  if (process.env.IOT_TEST_QUALITY_AI === '1') {
    await click('开始解读')
    const ai = await until(async () => { const rows = (await request(`/api/v1/data-quality/runs/${strictRun.id}/ai-jobs`)).items; return rows.find(row => ['SUCCEEDED', 'FAILED', 'CANCELLED'].includes(row.status)) }, 'AI terminal state', 60000)
    assert.ok(['SUCCEEDED', 'FAILED', 'CANCELLED'].includes(ai.status))
    assert.deepEqual((await request(`/api/v1/data-quality/runs/${strictRun.id}/metrics?limit=100`)).items[0].body, strictMetrics, 'AI结果不改写事实')
    coverage.push(`显式 AI ${ai.status} 与事实分离`)
  }
  await call('Emulation.setDeviceMetricsOverride', { width: 390, height: 844, deviceScaleFactor: 1, mobile: true })
  console.log('数据质量验收：验证390px四条业务路径')
  for (const name of ['分析任务', '质量配置', '确认基线', '校准记录']) {
    await tab(name); await delay(250)
    assert.ok(await evaluate('document.documentElement.scrollWidth<=innerWidth+2&&document.querySelector(".data-quality-page").getBoundingClientRect().right<=innerWidth+2'), `${name}在390px视口无整页横向溢出`)
    await screenshot(`page-${name}-390`)
  }
  coverage.push('390px四条业务路径与发现弹窗')
  assert.equal(browser.errors.length, 0, '浏览器无运行异常')
  assert.equal(failedRequests.length, 0, '质量API无意外失败响应')
  console.log(JSON.stringify({ passed: true, coverage, screenshots: output, profileId: profile.id, baselineProfileId: baseline.body.profileRevisionId, normalRun: normalRun.id, reviewedRun: strictRun.id }))
} catch (error) {
  if (browser) {
    try { console.error(await browser.evaluate('document.body.innerText.slice(-4500)')); console.error(JSON.stringify(failedRequests)) } catch (diagnostic) { console.error('DOM诊断不可用：', diagnostic.message) }
    let screenshotTimer
    try {
      const shot = await Promise.race([browser.call('Page.captureScreenshot', { format: 'png' }), new Promise((_, reject) => { screenshotTimer = setTimeout(() => reject(new Error('截图诊断超过5秒')), 5000) })])
      await writeFile(`${output}/failure.png`, Buffer.from(shot.data, 'base64'))
    } catch (diagnostic) { console.error('截图诊断不可用：', diagnostic.message) }
    finally { clearTimeout(screenshotTimer) }
  }
  throw error
} finally { await browser?.close() }
