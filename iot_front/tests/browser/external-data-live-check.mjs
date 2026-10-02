// Requires the dedicated, disposable external_browser fixture API (not a business database).
// UI creates configurations and bindings; HTTP only authenticates assertions, sends callbacks,
// and waits for background completion. The fixture owns and discards all test data.
import assert from 'node:assert/strict'
import { writeFile } from 'node:fs/promises'
import { startBrowser, delay } from '../helpers/browser.mjs'

const apiOrigin = process.env.IOT_EXTERNAL_FIXTURE_API || 'http://127.0.0.1:8089'
const origin = process.env.IOT_EXTERNAL_FIXTURE_UI || 'http://127.0.0.1:5189'
const tenant = 'external_browser'
const suffix = Date.now().toString(36)
const sourceName = `浏览器联调来源 ${suffix}`
const pullName = `主动拉取 ${suffix}`
const pushName = `告警推送 ${suffix}`
const withMedia = process.env.IOT_EXTERNAL_MEDIA === '1'
const mediaRequests = []
let token = '', browser
async function request(path, method = 'GET', body, extraHeaders = {}) {
  const response = await fetch(`${apiOrigin}${path}`, { method, headers:{ ...(token ? { Authorization:`Bearer ${token}` } : {}), ...(body === undefined ? {} : { 'Content-Type':'application/json' }), ...extraHeaders }, ...(body === undefined ? {} : { body:JSON.stringify(body) }) })
  const result = await response.json()
  assert.ok(response.ok, `${method} ${path}: ${response.status} ${result.detail || ''}`)
  return result
}
async function waitFor(fn, label) {
  for (let attempt = 0; attempt < 120; attempt++) { const result = await fn(); if (result) return result; await delay(500) }
  throw new Error(`Timeout: ${label}`)
}
try {
  token = (await request('/api/v1/auth/login', 'POST', { tenantId:tenant, username:'root', password:'external-browser-test-password' })).accessToken
  browser = await startBrowser({ args:['--use-mock-keychain', '--password-store=basic'], onEvent:event => { if (event.method === 'Network.requestWillBeSent' && /\/alarms\/[^/]+\/media\//.test(event.params.request.url)) mediaRequests.push(event.params.request.url) } })
  const { call, evaluate, until, errors } = browser
  await call('Page.enable'); await call('Runtime.enable'); await call('Network.enable')
  await call('Emulation.setDeviceMetricsOverride', { width:1440, height:1000, deviceScaleFactor:1, mobile:false })
  await call('Page.navigate', { url:origin })
  await until(() => evaluate(`Boolean(document.querySelector('.login-form'))`))
  await evaluate(`(() => {const values=${JSON.stringify([tenant, 'root', 'external-browser-test-password'])};document.querySelectorAll('.login-field input').forEach((input,index)=>{input.value=values[index];input.dispatchEvent(new Event('input',{bubbles:true}))});document.querySelector('.login-form button[type=submit]').click()})()`)
  await until(() => evaluate(`Boolean(document.querySelector('.nav-item[aria-label="外部数据接入"]'))`))
  await evaluate(`document.querySelector('.nav-item[aria-label="外部数据接入"]').click()`)
  await until(() => evaluate(`Boolean(document.querySelector('.external-data-view'))`))
  const click = async (text, selector = 'button') => {
    assert.equal(await evaluate(`(() => {const button=[...document.querySelectorAll(${JSON.stringify(selector)})].find(e=>e.getClientRects().length&&e.innerText.trim()===${JSON.stringify(text)});if(!button)return false;button.click();return true})()`), true, `click ${text}`)
  }
  const fill = async (label, value, root = '.n-modal') => {
    assert.equal(await evaluate(`(() => {const item=[...document.querySelectorAll(${JSON.stringify(root + ' .n-form-item')})].find(e=>e.getClientRects().length&&e.querySelector('.n-form-item-label')?.innerText.includes(${JSON.stringify(label)}));const input=item?.querySelector('input,textarea');if(!input)return false;input.value=${JSON.stringify(value)};input.dispatchEvent(new Event('input',{bubbles:true}));return true})()`), true, `fill ${label}`)
  }
  const select = async (label, option) => {
    assert.equal(await evaluate(`(() => {const item=[...document.querySelectorAll('.n-modal .n-form-item')].find(e=>e.getClientRects().length&&e.querySelector('.n-form-item-label')?.innerText.includes(${JSON.stringify(label)}));const input=item?.querySelector('.n-base-selection');if(!input)return false;input.click();return true})()`), true, `select ${label}`)
    await until(() => evaluate(`Boolean([...document.querySelectorAll('.n-base-select-option')].find(e=>e.getClientRects().length&&e.innerText.trim()===${JSON.stringify(option)}))`), `option ${option}`)
    await evaluate(`[...document.querySelectorAll('.n-base-select-option')].find(e=>e.getClientRects().length&&e.innerText.trim()===${JSON.stringify(option)}).click()`)
  }
  const close = async () => {
    await evaluate(`[...document.querySelectorAll('.n-modal')].find(e=>e.getClientRects().length)?.querySelector('.n-base-close')?.click()`)
    await until(() => evaluate(`![...document.querySelectorAll('.n-modal')].some(e=>e.getClientRects().length)`))
  }
  const rowAction = async (rowName, action) => {
    await until(() => evaluate(`Boolean([...document.querySelectorAll('.external-data-view tbody tr')].find(e=>e.innerText.includes(${JSON.stringify(rowName)})))`), `row ${rowName} loaded`)
    const direct = await evaluate(`(() => {const row=[...document.querySelectorAll('.external-data-view tbody tr')].find(e=>e.innerText.includes(${JSON.stringify(rowName)}));if(!row)return 'missing';const button=[...row.querySelectorAll('button')].find(e=>e.innerText.trim()===${JSON.stringify(action)});if(button){button.click();return 'direct'}row.querySelector('button[aria-label="更多操作"]')?.click();return 'menu'})()`)
    assert.notEqual(direct, 'missing', `row ${rowName}`)
    if (direct === 'direct') return
    await until(() => evaluate(`Boolean([...document.querySelectorAll('.n-dropdown-option')].find(e=>e.getClientRects().length&&e.innerText.trim()===${JSON.stringify(action)}))`), `action ${action}`)
    await evaluate(`[...document.querySelectorAll('.n-dropdown-option')].find(e=>e.getClientRects().length&&e.innerText.trim()===${JSON.stringify(action)}).querySelector('.n-dropdown-option-body').click()`)
  }
  const saveEditor = async () => {
    await click('保存配置')
    try { await until(() => evaluate(`![...document.querySelectorAll('.external-editor')].some(e=>e.getClientRects().length)`), 'configuration saved') }
    catch (error) {
      const feedback = await evaluate(`[...document.querySelectorAll('.editor-error,.n-message__content')].map(e=>e.innerText).join('; ')`)
      throw new Error(`${error.message}: ${feedback}`)
    }
  }
  await click('新增来源'); await fill('来源名称', sourceName); await fill('允许访问的主机', '127.0.0.1:8090')
  await evaluate(`document.querySelector('.external-editor .n-switch').click()`)
  await saveEditor()
  const source = (await request('/api/v1/external-data/sources?pageSize=100')).items.find(item => item.name === sourceName)
  assert.ok(source?.enabled)
  await rowAction(sourceName, '编辑'); await fill('说明', '浏览器验证已保存的来源可继续编辑'); await saveEditor()
  const editedSource = (await request('/api/v1/external-data/sources?pageSize=100')).items.find(item => item.id === source.id)
  assert.equal(editedSource.description, '浏览器验证已保存的来源可继续编辑')
  assert.ok(editedSource.revision > source.revision, 'UI edit must preserve and advance the saved revision')

  async function createEndpoint(name, mode) {
    await click('接入接口'); await click('新增接口'); await select('数据来源', sourceName); await fill('接口名称', name)
    await select('数据用途', '视频告警')
    if (mode === 'pull') { await select('接入方式', '平台主动拉取'); await fill('请求地址', 'http://127.0.0.1:8090/alarms') }
    await click('下一步'); await fill('数据列表路径', 'items')
    for (const [index, path] of [[0, 'eventId'], [1, 'camera'], [2, 'time'], [3, 'type'], [5, 'text']]) {
      assert.equal(await evaluate(`(() => {const row=document.querySelectorAll('.field-rule')[${index}];const item=[...row.querySelectorAll('.n-form-item')].find(e=>e.querySelector('.n-form-item-label')?.innerText.includes('原文字段路径'));const input=item?.querySelector('input');if(!input)return false;input.value=${JSON.stringify(path)};input.dispatchEvent(new Event('input',{bubbles:true}));return true})()`), true)
    }
    await click('下一步'); await evaluate(`document.querySelector('.external-editor section:not([style*="display: none"]) .n-switch').click()`); await saveEditor()
    return (await request(`/api/v1/external-data/endpoints?sourceId=${source.id}&pageSize=100`)).items.find(item => item.name === name)
  }
  const pull = await createEndpoint(pullName, 'pull')
  assert.ok(pull?.enabled)
  const beforePreview = (await request(`/api/v1/external-data/records?sourceId=${source.id}`)).total
  await rowAction(pullName, '请求预览'); await click('运行测试')
  await until(() => evaluate(`document.querySelector('.preview-result')?.innerText.includes('转换成功')`), 'real upstream preview')
  assert.equal(await evaluate(`document.querySelector('.preview-result').innerText.includes('vendor-camera')`), true)
  await close()
  assert.equal((await request(`/api/v1/external-data/records?sourceId=${source.id}`)).total, beforePreview, 'preview must not ingest')

  async function createBinding(externalId) {
    await click('编号绑定'); await click('新增绑定'); await select('数据来源', sourceName); await select('关联类型', '摄像头')
    await fill('外部系统编号', externalId); await fill('平台摄像头编号', 'ext-camera'); await saveEditor()
  }
  await createBinding('vendor-camera')
  const push = await createEndpoint(pushName, 'push')
  await rowAction(pushName, '接收设置'); await click('生成接收密钥')
  await until(() => evaluate(`Boolean([...document.querySelectorAll('.n-modal pre')][1]?.innerText)`), 'receive key shown once')
  const key = await evaluate(`[...document.querySelectorAll('.n-modal pre')][1].innerText.trim()`)
  const callback = `/api/external/v1/${tenant}/${push.id}`
  assert.equal(await evaluate(`document.querySelector('.n-modal pre').innerText.startsWith(location.origin)`), true)
  await close()
  const callbackEvent = { eventId:`callback-${suffix}`, camera:'vendor-camera', time:Date.now(), version:1, status:'ACTIVE', type:'FIRE', text:'浏览器联调：东门检测到烟雾', ...(withMedia ? { snapshotUrl:'http://127.0.0.1:8090/snapshot.png', videoClipUrl:'http://127.0.0.1:8090/missing.mp4' } : {}) }
  await request(callback, 'POST', { items:[callbackEvent] }, { 'X-API-Key':key })
  const callbackRecord = await waitFor(async () => (await request(`/api/v1/external-data/records?endpointId=${push.id}`)).items.find(row => row.body?.event?.id === callbackEvent.eventId && row.status === 'PROCESSED'), 'push processed')
  assert.equal(callbackRecord.body.deviceId, 'ext-device'); assert.equal(callbackRecord.body.cameraId, 'ext-camera')
  await click('接收记录'); await click('刷新')
  await until(() => evaluate(`document.querySelector('.external-data-view').innerText.includes(${JSON.stringify(callbackEvent.eventId)})`))
  await rowAction(callbackEvent.eventId, '查看详情')
  await until(() => evaluate(`document.querySelector('.raw-grid')?.innerText.includes('eventId')`))
  await call('Emulation.setDeviceMetricsOverride', { width:390, height:844, deviceScaleFactor:1, mobile:false })
  await until(() => evaluate(`(() => {const r=document.querySelector('.n-modal')?.getBoundingClientRect();return r&&r.left>=0&&r.right<=innerWidth+1&&getComputedStyle(document.querySelector('.raw-grid')).gridTemplateColumns.split(' ').length===1})()`))
  await delay(600)
  assert.equal(await evaluate(`(() => {const r=document.querySelector('.n-modal').getBoundingClientRect();return r.left>=0&&r.right<=innerWidth+1})()`),true,'settled modal fits narrow viewport')
  const image = await call('Page.captureScreenshot', { format:'png' })
  await writeFile('/tmp/torchlink-external-data-live-narrow.png', Buffer.from(image.data, 'base64'))
  await close(); await call('Emulation.setDeviceMetricsOverride', { width:1440, height:1000, deviceScaleFactor:1, mobile:false })
  if (withMedia) {
    await waitFor(async () => { const alarm=await request(`/api/v1/alarms/${callbackRecord.body.alarmId}`); return alarm.details?.videoEvent?.raw?.snapshotTransferStatus==='STORED'&&alarm.details?.videoEvent?.raw?.clipTransferStatus==='FAILED' }, 'independent attachment transfer states')
    await evaluate(`document.querySelector('.nav-item[aria-label="告警中心"]').click()`)
    await until(() => evaluate(`document.querySelector('.app-breadcrumb strong')?.innerText==='告警中心'&&Boolean(document.querySelector('.app-content tbody tr'))`))
    await click('查看详情')
    await until(() => evaluate(`document.querySelector('.alarm-media')?.innerText.includes('转存失败')`))
    assert.equal(mediaRequests.length, 0, 'detail opening must not fetch attachments')
    await click('查看截图')
    await until(() => evaluate(`document.querySelector('.alarm-media img')?.complete&&document.querySelector('.alarm-media img')?.naturalWidth>0`), 'protected image preview')
    assert.equal(mediaRequests.filter(url => url.endsWith('/snapshot')).length, 1)
    assert.equal(await evaluate(`document.querySelector('.alarm-media img').src.startsWith('blob:')`), true)
    await click('重新转存')
    await until(() => mediaRequests.some(url => url.endsWith('/retry')))
    const mediaImage = await call('Page.captureScreenshot', { format:'png' })
    await writeFile('/tmp/torchlink-external-media-live.png', Buffer.from(mediaImage.data, 'base64'))
    await close(); await evaluate(`document.querySelector('.nav-item[aria-label="外部数据接入"]').click()`)
    await until(() => evaluate(`Boolean(document.querySelector('.external-data-view'))`))
  }
  await click('接入接口'); await rowAction(pullName, '立即拉取'); await click('开始正式拉取')
  const job = await waitFor(async () => (await request(`/api/v1/external-data/jobs?endpointId=${pull.id}`)).items.find(row => row.status === 'COMPLETED'), 'pull completed')
  assert.equal(job.body.received, 1)
  const pulledRecord = await waitFor(async () => (await request(`/api/v1/external-data/records?endpointId=${pull.id}`)).items.find(row => row.status === 'PROCESSED'), 'pull record processed')
  assert.equal(pulledRecord.body.cameraId, 'ext-camera')
  await click('刷新'); await until(() => evaluate(`document.querySelector('.external-data-view')?.innerText.includes('1 条')`))

  const missingEvent = { ...callbackEvent, eventId:`unbound-${suffix}`, camera:`unbound-camera-${suffix}`, time:Date.now() }
  await request(callback, 'POST', { items:[missingEvent] }, { 'X-API-Key':key })
  await waitFor(async () => (await request(`/api/v1/external-data/records?endpointId=${push.id}`)).items.find(row => row.body?.event?.id === missingEvent.eventId && row.status === 'WAITING_BINDING'), 'unbound data preserved')
  await createBinding(missingEvent.camera)
  await click('接收记录'); await click('刷新'); await until(() => evaluate(`document.querySelector('.external-data-view').innerText.includes(${JSON.stringify(missingEvent.eventId)})`))
  await rowAction(missingEvent.eventId, '重试')
  await waitFor(async () => (await request(`/api/v1/external-data/records?endpointId=${push.id}`)).items.find(row => row.body?.event?.id === missingEvent.eventId && row.status === 'PROCESSED'), 'UI retry processed after binding')
  assert.deepEqual(errors, [])
  console.log(JSON.stringify({ result:'passed', sourceId:source.id, pushEndpointId:push.id, pullEndpointId:pull.id, jobId:job.id, checks:['real UI source and interface creation and revision-preserving source edit','real upstream preview without writes','camera binding','one-time receiving key','HTTP callback through raw ingest','390px raw and transformed detail','manual pull job and processed record','missing-binding preservation and UI retry', ...(withMedia ? ['independent media transfer failure, click-only protected image preview and UI retry'] : [])], screenshot:'/tmp/torchlink-external-data-live-narrow.png' }))
} finally { await browser?.close() }
