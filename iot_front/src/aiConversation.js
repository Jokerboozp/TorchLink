import { effectScope, ref, watch } from 'vue'
import { loadAIHistory, saveAIHistory } from './aiHistory.js'

// 智能助手的对话与运行状态放在页面组件之外：切换到其他菜单页面时流式请求继续运行，
// 返回后接着显示进度和结果。状态绑定“租户 + 用户 + 授权版本”，身份或授权变化、
// 退出登录时停止未完成的运行；刷新或关闭浏览器页面仍会断开连接并停止运行。

let sequence = 0
const makeId = prefix => `${prefix}_${globalThis.crypto?.randomUUID?.() || `${Date.now()}_${++sequence}`}`
const welcomeMessage = () => ({ id:'welcome', role:'assistant', status:'succeeded', text:'你好，我是消防物联网智能运维助手。可以直接查询设备、告警和趋势；切换顶部工作流插件后，会显示该插件的对话记录。', tools:[] })

function timestamp(value) {
  if (typeof value === 'number' && Number.isFinite(value)) return value
  if (typeof value === 'string' && /^\d+$/.test(value)) return Number(value)
  const parsed = value ? Date.parse(value) : Number.NaN
  return Number.isNaN(parsed) ? Date.now() : parsed
}

function safeText(value, fallback = '') {
  if (value == null) return fallback
  const text = typeof value === 'string' ? value : JSON.stringify(value)
  return text.length > 1200 ? `${text.slice(0, 1200)}…` : text
}

function normalizeError(value, fallback = '智能运行失败') {
  const error = typeof value === 'string' ? { message:value } : value || {}
  return {
    message:safeText(error.message || error.detail || error.text, fallback),
    code:safeText(error.code),
    stage:safeText(error.stage),
    traceId:safeText(error.traceId),
    retryable:Boolean(error.retryable)
  }
}

function normalizeEvent(raw) {
  const nested = raw?.data && typeof raw.data === 'object' && !Array.isArray(raw.data) ? raw.data : {}
  return { ...raw, ...nested, type:raw?.type || nested.type || 'message' }
}

function addRunEvent(run, event, label, status = 'info', detail = '') {
  run.events.push({ id:event.eventId || makeId('event'), type:event.type, label, status, detail:safeText(detail), createdAt:timestamp(event.createdAt || event.timestamp) })
}

function toolCallKey(event) { return event.toolCallId || event.callId || event.tool?.toolCallId || event.tool?.id || event.id || '' }

function findOrCreateTool(run, assistant, event) {
  const callId = toolCallKey(event)
  let tool = run.tools.find(item => item.toolCallId === callId)
  if (!tool) {
    const toolName = event.toolName || event.name || (typeof event.tool === 'string' ? event.tool : event.tool?.name) || '未命名工具'
    run.tools.push({ id:makeId('tool'), toolCallId:callId || makeId('call'), name:toolName, status:'running', inputSummary:event.inputSummary || event.input?.summary || '', outputSummary:'', error:'', startedAt:timestamp(event.startedAt || event.createdAt), durationMs:null })
    tool = run.tools[run.tools.length - 1]
    assistant.tools = run.tools
  }
  return tool
}

function createAIConversation({ identity, storage, stream }) {
  const scope = effectScope(true)
  const messages = ref([welcomeMessage()])
  const runs = ref([])
  const conversationId = ref('')
  const selectedWorkflowId = ref('')
  const sending = ref(false)
  const listeners = new Set()
  const pendingTextStates = new Map()
  let activeWorkflowId = ''
  let abortController = null
  let historyTimer = 0
  let disposed = false

  // 页面挂载时登记滚动回调；页面离开后运行照常更新状态，只是不再滚动。
  function notify() { for (const listener of listeners) listener() }
  function onUpdate(listener) {
    listeners.add(listener)
    return () => listeners.delete(listener)
  }

  function persist() {
    if (historyTimer) clearTimeout(historyTimer)
    historyTimer = 0
    if (!activeWorkflowId) return
    const state = { conversationId:conversationId.value, selectedWorkflowId:activeWorkflowId, messages:messages.value, runs:runs.value }
    saveAIHistory(storage, identity, state, activeWorkflowId)
    saveAIHistory(storage, identity, state)
  }
  function schedulePersist() {
    if (historyTimer) clearTimeout(historyTimer)
    historyTimer = setTimeout(persist, 150)
  }

  function restore() {
    const legacy = loadAIHistory(storage, identity)
    if (!legacy?.selectedWorkflowId) return
    const saved = loadAIHistory(storage, identity, Date.now(), legacy.selectedWorkflowId) || legacy
    activeWorkflowId = saved.selectedWorkflowId
    messages.value = saved.messages.length ? saved.messages : [welcomeMessage()]
    runs.value = saved.runs
    conversationId.value = saved.conversationId
    selectedWorkflowId.value = saved.selectedWorkflowId
    persist()
  }

  function switchConversation(workflowId) {
    if (workflowId === activeWorkflowId) return
    // 运行中下拉框不可用；工作流被删除、停用或由管理操作切换时，先停止旧运行，避免它继续写入已离开的会话。
    abortController?.abort()
    flushPendingAssistantText(false)
    persist()
    activeWorkflowId = workflowId
    const saved = workflowId ? loadAIHistory(storage, identity, Date.now(), workflowId) : null
    messages.value = saved?.messages?.length ? saved.messages : [welcomeMessage()]
    runs.value = saved?.runs || []
    conversationId.value = saved?.conversationId || ''
    persist()
  }

  function queueAssistantText(assistant, delta) {
    if (!delta) return
    let state = pendingTextStates.get(assistant)
    if (!state) {
      state = { value:'', timer:0 }
      pendingTextStates.set(assistant, state)
    }
    state.value += delta
    if (state.timer) return
    state.timer = setTimeout(() => {
      state.timer = 0
      const buffered = state.value
      state.value = ''
      if (buffered) {
        assistant.text += buffered
        notify()
      }
      if (!state.value) pendingTextStates.delete(assistant)
    }, 50)
  }

  function flushAssistantText(assistant, shouldNotify = true) {
    const state = pendingTextStates.get(assistant)
    if (!state) return
    if (state.timer) clearTimeout(state.timer)
    state.timer = 0
    const buffered = state.value
    state.value = ''
    pendingTextStates.delete(assistant)
    if (buffered) {
      assistant.text += buffered
      if (shouldNotify) notify()
    }
  }

  function flushPendingAssistantText(shouldNotify = true) {
    for (const assistant of pendingTextStates.keys()) flushAssistantText(assistant, shouldNotify)
  }

  function applyStreamEvent(raw, assistant, run) {
    const event = normalizeEvent(raw)
    if (event.messageId) assistant.serverMessageId = event.messageId
    if (event.runId) { assistant.runId = event.runId; run.runId = event.runId }
    if (event.traceId) { assistant.traceId = event.traceId; run.traceId = event.traceId }

    switch (event.type) {
      case 'run.started':
        assistant.status = 'streaming'
        run.status = 'running'
        run.provider = event.provider || run.provider
        run.model = event.model || run.model
        run.startedAt = timestamp(event.startedAt || event.createdAt)
        addRunEvent(run, event, '工作流服务开始运行', 'running', [run.provider,run.model].filter(Boolean).join(' / '))
        break
      case 'text.delta': {
        const delta = event.delta ?? event.text ?? event.content ?? ''
        if (typeof delta === 'string') queueAssistantText(assistant, delta)
        break
      }
      case 'tool.started': {
        const tool = findOrCreateTool(run, assistant, event)
        tool.status = 'running'
        tool.inputSummary = event.inputSummary || event.input?.summary || tool.inputSummary
        addRunEvent(run, event, `调用工具 · ${tool.name}`, 'running', tool.inputSummary)
        break
      }
      case 'tool.completed': {
        const tool = findOrCreateTool(run, assistant, event)
        const toolError = event.error ? normalizeError(event.error, '工具调用失败') : null
        tool.status = event.success === false || ['failed','error'].includes(event.status) || toolError ? 'failed' : 'succeeded'
        tool.outputSummary = event.outputSummary || event.output?.summary || ''
        tool.error = toolError?.message || ''
        tool.durationMs = event.durationMs ?? (event.completedAt ? Math.max(0, timestamp(event.completedAt) - tool.startedAt) : null)
        addRunEvent(run, event, `工具${tool.status === 'failed' ? '失败' : '完成'} · ${tool.name}`, tool.status === 'failed' ? 'danger' : 'success', tool.error || tool.outputSummary)
        if (event.clientAction?.type === 'RULE_DRAFT_READY' && event.clientAction.draft && typeof event.clientAction.draft === 'object') {
          assistant.ruleDraft = event.clientAction.draft
          assistant.ruleDraftPersisted = event.clientAction.persisted === true
          assistant.ruleDraftState = assistant.ruleDraftPersisted ? 'draft' : 'unsaved'
        }
        break
      }
      case 'run.completed':
        flushAssistantText(assistant)
        if (!assistant.text) assistant.text = safeText(event.answer || event.text, '本次运行已完成，但没有返回文本。')
        assistant.status = 'succeeded'
        assistant.usage = event.usage || null
        assistant.durationMs = event.durationMs ?? Math.max(0, Date.now() - run.startedAt)
        run.status = 'succeeded'
        run.usage = event.usage || null
        run.durationMs = assistant.durationMs
        run.finishedAt = timestamp(event.completedAt || event.createdAt)
        addRunEvent(run, event, '工作流服务运行完成', 'success', run.durationMs != null ? `${run.durationMs} 毫秒` : '')
        break
      case 'run.failed': {
        flushAssistantText(assistant)
        const failure = normalizeError(event.error || event, '智能运行失败')
        const stopped = failure.code === 'RUN_STOPPED'
        assistant.status = stopped ? 'canceled' : 'failed'
        assistant.error = failure
        assistant.text ||= '运行未能完成。'
        run.status = stopped ? 'canceled' : 'failed'
        run.error = failure
        run.durationMs = event.durationMs ?? Math.max(0, Date.now() - run.startedAt)
        run.finishedAt = timestamp(event.failedAt || event.createdAt)
        for (const tool of run.tools.filter(item => item.status === 'running')) tool.status = 'failed'
        addRunEvent(run, event, stopped ? '工作流已被管理员停止' : '工作流服务运行失败', stopped ? 'warning' : 'danger', failure.message)
        break
      }
    }
  }

  async function send(text, { workflowName = '', model = '' } = {}) {
    if (!text || sending.value || disposed) return
    if (!conversationId.value) conversationId.value = makeId('conversation')
    messages.value.push({ id:makeId('message'), role:'user', status:'succeeded', text, tools:[] })
    messages.value.push({ id:makeId('message'), role:'assistant', status:'streaming', text:'', prompt:text, tools:[], error:null })
    const assistant = messages.value[messages.value.length - 1]
    runs.value.unshift({ id:makeId('run'), runId:'', traceId:'', status:'running', workflowId:selectedWorkflowId.value, workflowName, provider:'', model, startedAt:Date.now(), finishedAt:null, durationMs:null, usage:null, events:[], tools:[], error:null })
    const run = runs.value[0]
    assistant.runKey = run.id
    assistant.tools = run.tools
    sending.value = true
    notify()

    const controller = new AbortController()
    abortController = controller
    const body = { question:text, conversationId:conversationId.value, workflowId:selectedWorkflowId.value, model }

    try {
      await stream('/api/v1/ai/chat/stream', { method:'POST', body:JSON.stringify(body), signal:controller.signal }, event => applyStreamEvent(event, assistant, run))
      if (assistant.status === 'streaming') {
        const failure = normalizeError({ code:'AI_STREAM_INCOMPLETE', retryable:true }, '智能流意外结束，请重试。')
        assistant.status = 'failed'; assistant.error = failure; assistant.text ||= '响应流未完整结束。'
        run.status = 'failed'; run.error = failure; run.durationMs = Math.max(0, Date.now() - run.startedAt)
        addRunEvent(run, { type:'run.failed' }, '响应流意外结束', 'danger', failure.message)
      }
    } catch (error) {
      if (error?.name === 'AbortError') {
        assistant.status = 'canceled'; assistant.text ||= '已停止生成。'; run.status = 'canceled'; run.durationMs = Math.max(0, Date.now() - run.startedAt)
        for (const tool of run.tools.filter(item => item.status === 'running')) tool.status = 'canceled'
        addRunEvent(run, { type:'run.canceled' }, '用户停止运行', 'info')
      } else {
        const failure = normalizeError(error)
        assistant.status = 'failed'; assistant.error = failure; assistant.text ||= '运行未能完成。'
        run.status = 'failed'; run.error = failure; run.traceId ||= failure.traceId; run.durationMs = Math.max(0, Date.now() - run.startedAt)
        addRunEvent(run, { type:'run.failed' }, '请求失败', 'danger', failure.message)
      }
    } finally {
      flushAssistantText(assistant)
      run.finishedAt ||= Date.now()
      if (abortController === controller) { abortController = null; sending.value = false }
      notify()
    }
  }

  function stop() { abortController?.abort() }

  function clear() {
    abortController?.abort()
    messages.value = [welcomeMessage()]
    runs.value = []
    conversationId.value = ''
    persist()
  }

  function dispose() {
    if (disposed) return
    disposed = true
    scope.stop()
    abortController?.abort()
    flushPendingAssistantText(false)
    persist()
    listeners.clear()
  }

  restore()
  scope.run(() => {
    watch(selectedWorkflowId, workflowId => switchConversation(workflowId), { flush:'sync' })
    watch([messages, runs, conversationId, selectedWorkflowId], schedulePersist, { deep:true })
  })

  return { identity, messages, runs, selectedWorkflowId, sending, send, stop, clear, persist, onUpdate, dispose }
}

let active = null
const sameIdentity = (left, right) => ['tenant', 'user', 'accessVersion'].every(key => (left?.[key] || '') === (right?.[key] || ''))

// 同一身份在页面重新进入时复用正在运行的对话；身份或授权版本不同则先停止旧对话。
export function useAIConversation(identity, options) {
  if (active && !sameIdentity(active.identity, identity)) resetAIConversation()
  active ||= createAIConversation({ ...options, identity:{ tenant:identity?.tenant || '', user:identity?.user || '', accessVersion:identity?.accessVersion || '' } })
  return active
}

export function resetAIConversation() {
  active?.dispose()
  active = null
}
