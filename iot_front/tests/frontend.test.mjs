import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const root = new URL('..', import.meta.url)

test('alarm levels normalize lowercase and missing values', async () => {
  const labels = await import('../src/labels.js')
  assert.equal(labels.alarmLevel('CRITICAL'), '紧急')
  assert.equal(labels.alarmLevel('high'), '高')
  assert.equal(labels.alarmLevel(''), '未设置')
})

test('realtime alarms, fault events and quiet settings stay tenant scoped', async () => {
  const alerts = await import('../src/globalAlert.js')

  const raised = alerts.parseRealtimeAlert(
    '/iot/alarm/raised/city-1/district-1/building-1/smoke/device-1',
    JSON.stringify({ alarmId:'alarm-1', triggerId:'message-1', deviceId:'device-1', deviceName:'东区烟感', alarmType:'FIRE_RISK', alarmLevel:'CRITICAL', source:'device', lastTriggeredAt:1760000000000 })
  )
  assert.equal(raised.kind, 'alarm')
  assert.equal(raised.alarmId, 'alarm-1')
  assert.equal(raised.deviceName, '东区烟感')
  assert.equal(raised.detail, '检测到设备异常报警，请及时处理。')
  assert.deepEqual(alerts.alertKeys(raised), ['alarm-1', 'message-1'])
  assert.equal(alerts.parseRealtimeAlert('/iot/parsed/tenant-a/product-a/device-1/ALARM_REPORT', { messageId:'message-1', messageType:'ALARM_REPORT', deviceId:'device-1' }), null) /* 正式告警由 raised 事件通知，解析消息不再重复弹窗。 */

  const fault = alerts.parseRealtimeAlert(
    '/iot/parsed/tenant-a/product-a/device-1/EVENT_REPORT',
    { messageId:'message-fault', rawMessageId:'raw-message-fault', messageType:'EVENT_REPORT', deviceId:'device-1', event:{ type:'FAULT', description:'主电源故障' } }
  )
  assert.equal(fault.kind, 'fault')
  assert.equal(fault.messageId, 'raw-message-fault')
  assert.equal(fault.alarmType, 'DEVICE_FAULT')
  assert.equal(fault.detail, '主电源故障')
  assert.equal(alerts.parseRealtimeAlert('/iot/parsed/tenant-a/product-a/device-1/EVENT_REPORT', { messageType:'EVENT_REPORT', event:{ type:'HEARTBEAT' } }), null)

  const values = new Map()
  const storage = { getItem:key => values.get(key) ?? null, setItem:(key, value) => values.set(key, value) }
  const saved = alerts.saveAlertSettings(storage, { tenant:'tenant-a', user:'operator' }, { popupEnabled:false, soundEnabled:false, quietStart:'22:00', quietEnd:'07:00' })
  assert.equal(alerts.loadAlertSettings(storage, { tenant:'tenant-a', user:'operator' }).popupEnabled, false)
  assert.equal(alerts.loadAlertSettings(storage, { tenant:'tenant-b', user:'operator' }).popupEnabled, true)
  assert.equal(alerts.isWithinQuietHours(new Date(2026, 0, 1, 23, 30), saved), true)
  assert.equal(alerts.isWithinQuietHours(new Date(2026, 0, 1, 12, 0), saved), false)
})

test('AI answers render safe Markdown in chat and health inspection', async () => {
  const markdown = await import('../src/markdown.js')
  const html = markdown.renderMarkdown('# 标题\n\n- **重点**\n\n`code`')
  assert.match(html, /<h1>标题<\/h1>/)
  assert.match(html, /<ul>[\s\S]*<strong>重点<\/strong>[\s\S]*<\/ul>/)
  assert.match(html, /<code>code<\/code>/)
  const unsafeHtml = markdown.renderMarkdown('<script>alert(1)</script>')
  assert.match(unsafeHtml, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/)
  assert.doesNotMatch(unsafeHtml, /<script>/)
})

test('health inspection report survives menu-driven view recreation and stays tenant scoped', async () => {
  const { HEALTH_INSPECTION_STORAGE_PREFIX, healthInspectionStorageKey, saveHealthInspection, loadHealthInspection } = await import('../src/healthInspectionState.js')
  const values = new Map()
  const storage = { getItem:key => values.get(key) ?? null, setItem:(key,value) => values.set(key,value), removeItem:key => values.delete(key) }
  const session = { tenant:'tenant-a', user:'alice' }
  const report = { generatedAt:1760000000000, summary:'巡检完成', counts:{ total:3, healthy:2 }, items:[], aiAdvice:'需要复核一台设备。' }
  assert.equal(saveHealthInspection(storage, session, report), true)
  assert.ok([...values.keys()][0].startsWith(HEALTH_INSPECTION_STORAGE_PREFIX))
  assert.deepEqual(loadHealthInspection(storage, session), report)
  assert.equal(loadHealthInspection(storage, { tenant:'tenant-b', user:'alice' }), null)
  assert.equal(loadHealthInspection(storage, { tenant:'tenant-a', user:'bob' }), null)
  values.set(healthInspectionStorageKey(session), '{invalid')
  assert.equal(loadHealthInspection(storage, session), null)
  assert.equal(values.has(healthInspectionStorageKey(session)), false)
})

test('alarm acknowledgement action is unavailable after the alarm is acknowledged', async () => {
  const actions = await import('../src/alarmActions.js')
  assert.equal(actions.canAcknowledgeAlarm('ACTIVE'), true)
  assert.equal(actions.canAcknowledgeAlarm('ACKED'), false)
  assert.equal(actions.canAcknowledgeAlarm('CLOSED'), false)
  assert.equal(actions.canCloseAlarm('ACKED'), true)
})

test('AI conversation history survives view recreation and stays tenant scoped', async () => {
  const { AI_HISTORY_STORAGE_PREFIX, loadAIHistory, saveAIHistory } = await import('../src/aiHistory.js')
  const values = new Map()
  const storage = { getItem:key => values.get(key) ?? null, setItem:(key,value) => values.set(key,value), removeItem:key => values.delete(key) }
  const session = { tenant:'tenant-a', user:'alice' }
  assert.equal(saveAIHistory(storage, session, { conversationId:'conversation-1', selectedWorkflowId:'ops-assistant', messages:[{ id:'m1', role:'user', status:'succeeded', text:'温度超过 80' }, { id:'m2', role:'assistant', status:'streaming', text:'' }], runs:[{ id:'r1', status:'running' }] }), true)
  assert.ok([...values.keys()][0].startsWith(AI_HISTORY_STORAGE_PREFIX))
  const restored = loadAIHistory(storage, session, 123456)
  assert.equal(restored.conversationId, 'conversation-1')
  assert.equal(restored.messages[0].text, '温度超过 80')
  assert.equal(restored.messages[1].status, 'canceled')
  assert.equal(restored.runs[0].status, 'canceled')
  assert.equal(restored.runs[0].finishedAt, 123456)
  assert.equal(loadAIHistory(storage, { tenant:'tenant-b', user:'alice' }), null)
})

test('AI conversations are isolated by workflow and legacy history stays readable', async () => {
  const { loadAIHistory, saveAIHistory } = await import('../src/aiHistory.js')
  const values = new Map()
  const storage = { getItem:key => values.get(key) ?? null, setItem:(key,value) => values.set(key,value), removeItem:key => values.delete(key) }
  const session = { tenant:'tenant-a', user:'alice' }
  const state = (workflow, text) => ({ conversationId:`conversation-${workflow}`, selectedWorkflowId:workflow, messages:[{ id:workflow, role:'user', status:'succeeded', text }], runs:[] })
  saveAIHistory(storage, session, state('workflow-a', 'A 的对话'), 'workflow-a')
  saveAIHistory(storage, session, state('workflow-b', 'B 的对话'), 'workflow-b')
  assert.equal(loadAIHistory(storage, session, Date.now(), 'workflow-a').messages[0].text, 'A 的对话')
  assert.equal(loadAIHistory(storage, session, Date.now(), 'workflow-b').messages[0].text, 'B 的对话')
  assert.equal(loadAIHistory(storage, session, Date.now(), 'workflow-c'), null)
})

test('AI rule draft cards reconcile persisted snapshots with current rule state', async () => {
  const { reconcileRuleDraftMessages } = await import('../src/ruleDraftStatus.js')
  const messages = [{ id:'assistant-1', ruleDraftPersisted:true, ruleDraftState:'draft', ruleDraft:{ id:'rule-1', name:'旧名称', enabled:false } }]
  assert.equal(reconcileRuleDraftMessages(messages, [{ id:'rule-1', name:'已启用规则', enabled:true, version:2 }]), 1)
  assert.equal(messages[0].ruleDraftState, 'enabled')
  assert.equal(messages[0].ruleDraft.enabled, true)
  assert.equal(messages[0].ruleDraft.name, '已启用规则')
  reconcileRuleDraftMessages(messages, [])
  assert.equal(messages[0].ruleDraftState, 'missing')
})

test('frontend builds independently and proxies backend routes', async () => {
  const vite = await readFile(new URL('vite.config.js', root), 'utf8')
  const nginx = await readFile(new URL('nginx.conf', root), 'utf8')
  assert.match(vite, /outDir:\s*'dist'/)
  assert.doesNotMatch(vite, /internal\/httpapi\/static/)
  for (const route of ['/api/', '/health/', '/mcp']) assert.ok(nginx.includes(route), `nginx is missing ${route}`)
  assert.ok(nginx.includes('platform-api:8080'))
  assert.match(nginx, /location = \/api\/v1\/ai\/chat\/stream\s*\{[\s\S]*?proxy_buffering off;[\s\S]*?proxy_cache off;[\s\S]*?gzip off;[\s\S]*?proxy_read_timeout 3600s;[\s\S]*?proxy_set_header Connection "";/)
  assert.doesNotMatch(nginx, /IOT_VIDEO_PREVIEW_CSP_SOURCES/)
  assert.doesNotMatch(nginx, /connect-src[^;]*\bhttp:\s+https:/)
})

test('frontend rejects Node versions unsupported by the build toolchain', async () => {
  const packageJson = JSON.parse(await readFile(new URL('package.json', root), 'utf8'))
  const packageLock = JSON.parse(await readFile(new URL('package-lock.json', root), 'utf8'))
  const npmrc = await readFile(new URL('.npmrc', root), 'utf8')

  const supportedNodeVersions = '^20.19.0 || >=22.12.0'
  assert.equal(packageJson.engines?.node, supportedNodeVersions)
  assert.equal(packageLock.packages?.['']?.engines?.node, supportedNodeVersions)
  assert.match(npmrc, /^engine-strict=true\s*$/m)
})

test('component alarm popup identifies the actual part and location', async () => {
  const { parseRealtimeAlert, alertKeys } = await import('../src/globalAlert.js')
  const alert = parseRealtimeAlert('/iot/alarm/raised/c/d/b/fire/controller', {
    alarmId:'component-alarm', triggerId:'shared-report', deviceId:'controller', deviceName:'消防控制器',
    alarmType:'FIRE', componentId:'loop-1/node-7', componentName:'烟感探测器', componentLocation:'二楼走廊'
  })
  assert.equal(alert.deviceName, '消防控制器')
  assert.equal(alert.detail, '烟感探测器 · 二楼走廊 · 火灾告警')
  assert.deepEqual(alertKeys(alert), ['component-alarm'])
  assert.equal(parseRealtimeAlert('/iot/parsed/t/p/d/ALARM_REPORT', { messageType:'ALARM_REPORT', event:{components:[{id:'a'},{id:'b'}]} }), null)
})


test('AI history cannot restore data from a previous authorization scope', async () => {
  const { loadAIHistory, saveAIHistory } = await import('../src/aiHistory.js')
  const values = new Map()
  const storage = { getItem:key => values.get(key) ?? null, setItem:(key,value) => values.set(key,value), removeItem:key => values.delete(key) }
  const identity = { tenant:'tenant-a', user:'alice' }
  const previous = { ...identity, accessVersion:'all-devices' }
  const current = { ...identity, accessVersion:'device-a-only' }
  const state = { conversationId:'old', messages:[{ text:'设备 B 的告警', status:'succeeded' }], runs:[] }
  for (const workflow of ['', 'ops-assistant']) {
    saveAIHistory(storage, identity, state, workflow)
    assert.equal(loadAIHistory(storage, current, Date.now(), workflow), null)
    saveAIHistory(storage, previous, state, workflow)
    assert.equal(loadAIHistory(storage, current, Date.now(), workflow), null)
    assert.equal(loadAIHistory(storage, previous, Date.now(), workflow).conversationId, 'old')
  }
})
