import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import vm from 'node:vm'
import { computed, effectScope, nextTick, reactive, ref, watch } from 'vue'
import { errorMessage, formatLabel, platformLabel, statusLabel, toolName, transportLabel } from '../src/presentation.js'
import { createThemeOverrides, parseTokens, resolveToken } from '../src/theme/naiveTheme.js'
import { consumeSSE } from '../src/sse.js'
import { createClientId } from '../src/clientId.js'
import { loadAIHistory } from '../src/aiHistory.js'
import { resetAIConversation, useAIConversation } from '../src/aiConversation.js'
import { setupScript } from './helpers/vue.mjs'

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
    JSON.stringify({
      alarmId: 'alarm-1',
      triggerId: 'message-1',
      deviceId: 'device-1',
      deviceName: '东区烟感',
      alarmType: 'FIRE_RISK',
      alarmLevel: 'CRITICAL',
      source: 'device',
      lastTriggeredAt: 1760000000000
    })
  )
  assert.equal(raised.kind, 'alarm')
  assert.equal(raised.alarmId, 'alarm-1')
  assert.equal(raised.deviceName, '东区烟感')
  assert.equal(raised.detail, '检测到设备异常报警，请及时处理。')
  assert.deepEqual(alerts.alertKeys(raised), ['alarm-1', 'message-1'])
  assert.equal(
    alerts.parseRealtimeAlert('/iot/parsed/tenant-a/product-a/device-1/ALARM_REPORT', {
      messageId: 'message-1',
      messageType: 'ALARM_REPORT',
      deviceId: 'device-1'
    }),
    null
  ) /* 正式告警由 raised 事件通知，解析消息不再重复弹窗。 */

  const fault = alerts.parseRealtimeAlert('/iot/parsed/tenant-a/product-a/device-1/EVENT_REPORT', {
    messageId: 'message-fault',
    rawMessageId: 'raw-message-fault',
    messageType: 'EVENT_REPORT',
    deviceId: 'device-1',
    event: { type: 'FAULT', description: '主电源故障' }
  })
  assert.equal(fault.kind, 'fault')
  assert.equal(fault.messageId, 'raw-message-fault')
  assert.equal(fault.alarmType, 'DEVICE_FAULT')
  assert.equal(fault.detail, '主电源故障')
  assert.equal(
    alerts.parseRealtimeAlert('/iot/parsed/tenant-a/product-a/device-1/EVENT_REPORT', {
      messageType: 'EVENT_REPORT',
      event: { type: 'HEARTBEAT' }
    }),
    null
  )

  const values = new Map()
  const storage = { getItem: key => values.get(key) ?? null, setItem: (key, value) => values.set(key, value) }
  const saved = alerts.saveAlertSettings(
    storage,
    { tenant: 'tenant-a', user: 'operator' },
    { popupEnabled: false, soundEnabled: false, quietStart: '22:00', quietEnd: '07:00' }
  )
  assert.equal(alerts.loadAlertSettings(storage, { tenant: 'tenant-a', user: 'operator' }).popupEnabled, false)
  assert.equal(alerts.loadAlertSettings(storage, { tenant: 'tenant-b', user: 'operator' }).popupEnabled, true)
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
  const { HEALTH_INSPECTION_STORAGE_PREFIX, healthInspectionStorageKey, saveHealthInspection, loadHealthInspection } =
    await import('../src/healthInspectionState.js')
  const values = new Map()
  const storage = {
    getItem: key => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: key => values.delete(key)
  }
  const session = { tenant: 'tenant-a', user: 'alice' }
  const report = {
    generatedAt: 1760000000000,
    summary: '巡检完成',
    counts: { total: 3, healthy: 2 },
    items: [],
    aiAdvice: '需要复核一台设备。'
  }
  assert.equal(saveHealthInspection(storage, session, report), true)
  assert.ok([...values.keys()][0].startsWith(HEALTH_INSPECTION_STORAGE_PREFIX))
  assert.deepEqual(loadHealthInspection(storage, session), report)
  assert.equal(loadHealthInspection(storage, { tenant: 'tenant-b', user: 'alice' }), null)
  assert.equal(loadHealthInspection(storage, { tenant: 'tenant-a', user: 'bob' }), null)
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
  const storage = {
    getItem: key => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: key => values.delete(key)
  }
  const session = { tenant: 'tenant-a', user: 'alice' }
  assert.equal(
    saveAIHistory(storage, session, {
      conversationId: 'conversation-1',
      selectedWorkflowId: 'ops-assistant',
      messages: [
        { id: 'm1', role: 'user', status: 'succeeded', text: '温度超过 80' },
        { id: 'm2', role: 'assistant', status: 'streaming', text: '' }
      ],
      runs: [{ id: 'r1', status: 'running' }]
    }),
    true
  )
  assert.ok([...values.keys()][0].startsWith(AI_HISTORY_STORAGE_PREFIX))
  const restored = loadAIHistory(storage, session, 123456)
  assert.equal(restored.conversationId, 'conversation-1')
  assert.equal(restored.messages[0].text, '温度超过 80')
  assert.equal(restored.messages[1].status, 'canceled')
  assert.equal(restored.runs[0].status, 'canceled')
  assert.equal(restored.runs[0].finishedAt, 123456)
  assert.equal(loadAIHistory(storage, { tenant: 'tenant-b', user: 'alice' }), null)
})

test('AI conversations are isolated by workflow and legacy history stays readable', async () => {
  const { loadAIHistory, saveAIHistory } = await import('../src/aiHistory.js')
  const values = new Map()
  const storage = {
    getItem: key => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: key => values.delete(key)
  }
  const session = { tenant: 'tenant-a', user: 'alice' }
  const state = (workflow, text) => ({
    conversationId: `conversation-${workflow}`,
    selectedWorkflowId: workflow,
    messages: [{ id: workflow, role: 'user', status: 'succeeded', text }],
    runs: []
  })
  saveAIHistory(storage, session, state('workflow-a', 'A 的对话'), 'workflow-a')
  saveAIHistory(storage, session, state('workflow-b', 'B 的对话'), 'workflow-b')
  assert.equal(loadAIHistory(storage, session, Date.now(), 'workflow-a').messages[0].text, 'A 的对话')
  assert.equal(loadAIHistory(storage, session, Date.now(), 'workflow-b').messages[0].text, 'B 的对话')
  assert.equal(loadAIHistory(storage, session, Date.now(), 'workflow-c'), null)
})

function memoryStorage() {
  const values = new Map()
  return { getItem: key => values.get(key) ?? null, setItem: (key, value) => values.set(key, value), removeItem: key => values.delete(key) }
}

// Each call is one browser AI stream; the test pushes its SSE events by hand.
function manualAIStream() {
  const calls = []
  const stream = (_path, options, onEvent) =>
    new Promise((resolve, reject) => {
      options.signal.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true })
      calls.push({ body: JSON.parse(options.body), signal: options.signal, emit: onEvent, finish: resolve })
    })
  return { stream, calls }
}

// Runs the real AiView setup code with a component-like effect scope and lifecycle hooks.
const aiViewSetup = setupScript(new URL('../src/views/AiView.vue', import.meta.url))
function mountAiView(identity, storage, stream) {
  const hooks = { mounted: [], beforeUnmount: [] }
  const scope = effectScope()
  const context = vm.createContext({
    computed,
    nextTick,
    reactive,
    ref,
    watch,
    useAIConversation,
    session: identity,
    localStorage: storage,
    apiStream: stream,
    onMounted: fn => hooks.mounted.push(fn),
    onBeforeUnmount: fn => hooks.beforeUnmount.push(fn),
    defineEmits: () => () => {},
    can: () => true,
    providerOptions: [],
    capabilityName: value => value,
    reconcileRuleDraftMessages: () => 0,
    UiMessage: { info() {}, success() {}, warning() {}, error() {} },
    UiMessageBox: { confirm: async () => {} },
    api: async path =>
      path.startsWith('/api/v1/ai/workflows') ? { items: [{ id: 'ops-assistant', name: '运维助手', enabled: true }], healthy: true } : {},
    requestAnimationFrame: callback => {
      callback()
      return 0
    },
    cancelAnimationFrame: () => {}
  })
  const view = scope.run(() => vm.runInContext(`${aiViewSetup}\n;({ send, messages, sending })`, context))
  return {
    view,
    mount: () => Promise.all(hooks.mounted.map(fn => fn())),
    unmount: () => {
      hooks.beforeUnmount.forEach(fn => fn())
      scope.stop()
    }
  }
}

test('AI answers keep streaming after leaving the assistant page and show on return', async t => {
  t.after(resetAIConversation)
  const storage = memoryStorage()
  const identity = { tenant: 'tenant-a', user: 'alice', accessVersion: 'v1' }
  const { stream, calls } = manualAIStream()
  const page = mountAiView(identity, storage, stream)
  await page.mount()
  const answer = page.view.send('当前有哪些告警？')
  const [request] = calls
  assert.equal(request.body.workflowId, 'ops-assistant')
  request.emit({ type: 'run.started', runId: 'run-1' })
  request.emit({ type: 'text.delta', delta: '共有 ' })
  page.unmount()
  assert.equal(request.signal.aborted, false)

  const running = mountAiView(identity, storage, stream)
  await running.mount()
  assert.equal(running.view.sending.value, true)
  assert.equal(running.view.messages.value.at(-1).status, 'streaming')
  running.unmount()
  request.emit({ type: 'text.delta', delta: '2 条活动告警。' })
  request.emit({ type: 'run.completed' })
  request.finish()
  await answer
  await new Promise(resolve => setTimeout(resolve, 200))
  assert.equal(loadAIHistory(storage, identity, Date.now(), 'ops-assistant').messages.at(-1).text, '共有 2 条活动告警。')

  const returned = mountAiView(identity, storage, stream)
  await returned.mount()
  assert.equal(returned.view.sending.value, false)
  assert.equal(returned.view.messages.value.at(-1).status, 'succeeded')
  assert.equal(returned.view.messages.value.at(-1).text, '共有 2 条活动告警。')
  assert.equal(request.signal.aborted, false)
  returned.unmount()
})

test('knowledge prefetch sources are kept on the tool card', async t => {
  t.after(resetAIConversation)
  const { stream, calls } = manualAIStream()
  const conversation = useAIConversation({ tenant: 'tenant-a', user: 'alice', accessVersion: 'v1' }, { storage: memoryStorage(), stream })
  conversation.selectedWorkflowId.value = 'ops-assistant'
  const answer = conversation.send('烟感告警怎么处置？', { workflowName: '运维助手' })
  const [request] = calls
  request.emit({ type: 'tool.started', callId: 'prefetch', tool: 'query_knowledge_base' })
  request.emit({
    type: 'tool.completed',
    callId: 'prefetch',
    tool: 'query_knowledge_base',
    success: true,
    data: { outputSummary: '召回 1 条绑定知识', sources: [{ documentId: 'doc-1', filename: '烟感手册.pdf', chunkIndex: 2, score: 0.83 }] }
  })
  request.emit({ type: 'run.completed' })
  request.finish()
  await answer
  assert.deepEqual(conversation.messages.value.at(-1).tools[0].sources, [{ filename: '烟感手册.pdf', chunkIndex: 2, score: 0.83 }])
})

test('AI background runs stop when authorization changes or the user logs out', async t => {
  t.after(resetAIConversation)
  const storage = memoryStorage()
  const alice = { tenant: 'tenant-a', user: 'alice', accessVersion: 'v1' }
  const { stream, calls } = manualAIStream()
  const first = useAIConversation(alice, { storage, stream })
  first.selectedWorkflowId.value = 'ops-assistant'
  const firstRun = first.send('设备 B 的告警')
  calls[0].emit({ type: 'text.delta', delta: '设备 B 有 1 条告警' })
  const narrowed = useAIConversation({ ...alice, accessVersion: 'v2' }, { storage, stream })
  assert.notEqual(narrowed, first)
  assert.equal(calls[0].signal.aborted, true)
  await firstRun
  assert.deepEqual(
    narrowed.messages.value.map(message => message.id),
    ['welcome']
  )

  narrowed.selectedWorkflowId.value = 'ops-assistant'
  const secondRun = narrowed.send('当前告警')
  resetAIConversation()
  assert.equal(calls[1].signal.aborted, true)
  await secondRun
  const restored = useAIConversation(alice, { storage, stream })
  assert.equal(restored.messages.value.at(-1).status, 'canceled')
  assert.equal(restored.messages.value.at(-1).text, '设备 B 有 1 条告警')
})

test('AI rule draft cards reconcile persisted snapshots with current rule state', async () => {
  const { reconcileRuleDraftMessages } = await import('../src/ruleDraftStatus.js')
  const messages = [
    { id: 'assistant-1', ruleDraftPersisted: true, ruleDraftState: 'draft', ruleDraft: { id: 'rule-1', name: '旧名称', enabled: false } }
  ]
  assert.equal(reconcileRuleDraftMessages(messages, [{ id: 'rule-1', name: '已启用规则', enabled: true, version: 2 }]), 1)
  assert.equal(messages[0].ruleDraftState, 'enabled')
  assert.equal(messages[0].ruleDraft.enabled, true)
  assert.equal(messages[0].ruleDraft.name, '已启用规则')
  reconcileRuleDraftMessages(messages, [])
  assert.equal(messages[0].ruleDraftState, 'missing')
})

test('reverse proxy preserves backend routes and unbuffered AI streaming', async () => {
  const nginx = await readFile(new URL('nginx.conf', root), 'utf8')
  for (const route of ['/api/', '/health/', '/mcp']) assert.ok(nginx.includes(route), `nginx is missing ${route}`)
  assert.ok(nginx.includes('http://${IOT_API_UPSTREAM}'))
  const dockerfile = await readFile(new URL('Dockerfile', root), 'utf8')
  assert.match(dockerfile, /ENV IOT_API_UPSTREAM=platform-api:8080 IOT_VIDEO_UPSTREAM=zlmediakit:80/)
  assert.match(
    nginx,
    /location = \/api\/v1\/ai\/chat\/stream\s*\{[\s\S]*?proxy_buffering off;[\s\S]*?proxy_cache off;[\s\S]*?gzip off;[\s\S]*?proxy_read_timeout 3600s;[\s\S]*?proxy_set_header Connection "";/
  )
  assert.doesNotMatch(nginx, /IOT_VIDEO_PREVIEW_CSP_SOURCES/)
  assert.doesNotMatch(nginx, /connect-src[^;]*\bhttp:\s+https:/)
})

test('component alarm popup identifies the actual part and location', async () => {
  const { parseRealtimeAlert, alertKeys } = await import('../src/globalAlert.js')
  const alert = parseRealtimeAlert('/iot/alarm/raised/c/d/b/fire/controller', {
    alarmId: 'component-alarm',
    triggerId: 'shared-report',
    deviceId: 'controller',
    deviceName: '消防控制器',
    alarmType: 'FIRE',
    componentId: 'loop-1/node-7',
    componentName: '烟感探测器',
    componentLocation: '二楼走廊'
  })
  assert.equal(alert.deviceName, '消防控制器')
  assert.equal(alert.detail, '烟感探测器 · 二楼走廊 · 火灾告警')
  assert.deepEqual(alertKeys(alert), ['component-alarm'])
  assert.equal(
    parseRealtimeAlert('/iot/parsed/t/p/d/ALARM_REPORT', {
      messageType: 'ALARM_REPORT',
      event: { components: [{ id: 'a' }, { id: 'b' }] }
    }),
    null
  )
})

test('AI history cannot restore data from a previous authorization scope', async () => {
  const { loadAIHistory, saveAIHistory } = await import('../src/aiHistory.js')
  const values = new Map()
  const storage = {
    getItem: key => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: key => values.delete(key)
  }
  const identity = { tenant: 'tenant-a', user: 'alice' }
  const previous = { ...identity, accessVersion: 'all-devices' }
  const current = { ...identity, accessVersion: 'device-a-only' }
  const state = { conversationId: 'old', messages: [{ text: '设备 B 的告警', status: 'succeeded' }], runs: [] }
  for (const workflow of ['', 'ops-assistant']) {
    saveAIHistory(storage, identity, state, workflow)
    assert.equal(loadAIHistory(storage, current, Date.now(), workflow), null)
    saveAIHistory(storage, previous, state, workflow)
    assert.equal(loadAIHistory(storage, current, Date.now(), workflow), null)
    assert.equal(loadAIHistory(storage, previous, Date.now(), workflow).conversationId, 'old')
  }
})

test('display names handle canonical and lowercase wire values without mutating data', () => {
  const data = { transport: 'MQTT', format: 'json', status: 'INDEXED' }
  assert.equal(transportLabel(data.transport), 'MQTT')
  assert.equal(transportLabel('iot-standard'), '标准设备接入')
  assert.equal(transportLabel('tcp'), 'TCP')
  assert.equal(transportLabel('MQTT_HTTP'), 'MQTT / HTTP')
  assert.equal(formatLabel(data.format), 'JSON')
  assert.equal(statusLabel(data.status), '已建立索引')
  assert.equal(statusLabel('indexed'), '已建立索引')
  assert.equal(statusLabel('new-backend-status'), '未知状态')
  assert.equal(statusLabel(null), '未知状态')
  assert.equal(statusLabel('待人工复核'), '待人工复核')
  assert.deepEqual(data, { transport: 'MQTT', format: 'json', status: 'INDEXED' })
  assert.equal(platformLabel('windows-arm64'), 'Windows · arm64')
  assert.equal(toolName('mcp__iot__query_alarm_list'), '查询告警')
})

test('errors give actionable Chinese feedback for network, permission and format failures', () => {
  assert.equal(errorMessage(new TypeError('Failed to fetch')), '无法连接服务，请检查网络后重试')
  assert.equal(errorMessage({ status: 403, message: 'forbidden' }), '当前账户没有操作权限')
  assert.equal(errorMessage({ status: 401, message: 'invalid credentials' }), '身份验证失败，请检查账户信息或重新登录')
  assert.match(errorMessage(new Error('relation would create a cycle')), /循环/)
  assert.match(errorMessage(new Error('local connection credential reference was not found')), /未找到现场连接凭据/)
  assert.equal(errorMessage(new SyntaxError('Unexpected token')), '数据格式不正确，请检查括号、引号和字段值')
  assert.equal(errorMessage({ message: '设备名称不能为空' }), '设备名称不能为空')
  assert.equal(errorMessage({ status: 502 }), '服务暂时不可用，请稍后重试')
})

const tokensCss = await readFile(new URL('../src/theme/tokens.css', import.meta.url), 'utf8')

test('Naive UI 主题完全由 tokens.css 生成，品牌色与设计变量一致', () => {
  const theme = createThemeOverrides(tokensCss)
  assert.equal(theme.common.primaryColor, resolveToken(parseTokens(tokensCss), '--primary'))
  assert.equal(theme.common.bodyColor, resolveToken(parseTokens(tokensCss), '--bg'))
  const values = JSON.stringify(theme)
  assert.doesNotMatch(values, /var\(/, '主题中不能残留未解析的 CSS 变量')
  assert.doesNotMatch(values, /undefined/)
})

test('缺失的设计变量会直接报错，而不是生成空主题', () => {
  assert.throws(() => resolveToken(parseTokens(':root { --a: var(--missing); }'), '--a'), /未定义/)
})

function byteStream(text, chunkSize = 1) {
  const bytes = new TextEncoder().encode(text)
  return new ReadableStream({
    start(controller) {
      for (let offset = 0; offset < bytes.length; offset += chunkSize) controller.enqueue(bytes.slice(offset, offset + chunkSize))
      controller.close()
    }
  })
}

test('SSE parser supports event fields, JSON event types, CRLF and split UTF-8 bytes', async () => {
  const source = [
    'event: run.started\r\nid: evt-1\r\ndata: {"runId":"run-1","conversationId":"conversation-1"}\r\n\r\n',
    'data: {"type":"text.delta","delta":"你好"}\n\n',
    'event: tool.started\ndata: {"toolCallId":"tool-1","toolName":"alarm.query","inputSummary":"高等级告警"}\n\n',
    'event: tool.completed\ndata: {"toolCallId":"tool-1","success":true,"outputSummary":"2 条"}\n\n',
    'event: run.completed\ndata: {"durationMs":\n',
    'data: 42}\n\n'
  ].join('')
  const events = []
  await consumeSSE(byteStream(source), event => events.push(event))

  assert.deepEqual(
    events.map(event => event.type),
    ['run.started', 'text.delta', 'tool.started', 'tool.completed', 'run.completed']
  )
  assert.equal(events[0].eventId, 'evt-1')
  assert.equal(events[1].delta, '你好')
  assert.equal(events[4].durationMs, 42)
})

test('SSE parser ignores a legacy DONE marker in favor of explicit terminal events', async () => {
  const events = []
  await consumeSSE(byteStream('data: [DONE]\n\n', 3), event => events.push(event))
  assert.deepEqual(events, [])
})

test('SSE parser rejects non-JSON event payloads without exposing their contents', async () => {
  await assert.rejects(
    consumeSSE(byteStream('event: run.failed\ndata: definitely-not-json\n\n', 5)),
    error => error.code === 'AI_STREAM_INVALID_EVENT' && !error.message.includes('definitely-not-json')
  )
})

test('HTTP fallback preserves UUID v4 version and variant using random bytes', () => {
  let calls = 0
  const provider = {
    getRandomValues(bytes) {
      calls++
      return bytes.fill(255)
    }
  }
  assert.equal(createClientId(provider), 'ffffffff-ffff-4fff-bfff-ffffffffffff')
  assert.equal(calls, 1)
  const ids = new Set(Array.from({ length: 100 }, () => createClientId({ getRandomValues: bytes => crypto.getRandomValues(bytes) })))
  assert.equal(ids.size, 100)
  for (const id of ids) assert.match(id, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/)
})

test('HTTPS keeps the browser native UUID implementation', () => {
  assert.equal(
    createClientId({
      randomUUID() {
        return 'native-id'
      },
      getRandomValues() {
        throw Error('unexpected fallback')
      }
    }),
    'native-id'
  )
})
