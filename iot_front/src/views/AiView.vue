<script setup>
import { can } from '../permissions'
import { aiProviderOptions as providerOptions, capabilityName } from '../presentation'
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { UiMessage, UiMessageBox } from '../ui/feedback.js'
import { api, apiStream, session } from '../api'
import { loadAIHistory, saveAIHistory } from '../aiHistory'
import { reconcileRuleDraftMessages } from '../ruleDraftStatus'
import HarnessTraceDrawer from '../components/HarnessTraceDrawer.vue'
import MarkdownContent from '../components/MarkdownContent.vue'
import ToolCallCard from '../components/ToolCallCard.vue'

const emit = defineEmits(['navigate'])

// Keep delayed saves bound to the identity that mounted this conversation.
const historyIdentity = { tenant: session.tenant, user: session.user, accessVersion: session.accessVersion }

let sequence = 0
let abortController = null
let scrollFrame = 0
let scrollQueued = false
let historyTimer = 0
let activeConversationWorkflowId = ''
let restoringConversation = false
const pendingTextStates = new Map()
const makeId = prefix => `${prefix}_${globalThis.crypto?.randomUUID?.() || `${Date.now()}_${++sequence}`}`
const welcomeMessage = () => ({ id:'welcome', role:'assistant', status:'succeeded', text:'你好，我是消防物联网智能运维助手。可以直接查询设备、告警和趋势；切换顶部工作流插件后，会显示该插件的对话记录。', tools:[] })

const messages = ref([welcomeMessage()])
const runs = ref([])
const question = ref('')
const sending = ref(false)
const log = ref()
const conversationId = ref('')
const selectedRunKey = ref('')
const traceVisible = ref(false)

function persistConversation() {
  if (!activeConversationWorkflowId) return
  const state = { conversationId:conversationId.value, selectedWorkflowId:activeConversationWorkflowId, messages:messages.value, runs:runs.value }
  saveAIHistory(localStorage, historyIdentity, state, activeConversationWorkflowId)
  saveAIHistory(localStorage, historyIdentity, state)
}
function scheduleConversationPersist() {
  if (historyTimer) clearTimeout(historyTimer)
  historyTimer = setTimeout(() => { historyTimer = 0; persistConversation() }, 150)
}
function restoreConversation() {
  const legacy = loadAIHistory(localStorage, historyIdentity)
  if (!legacy?.selectedWorkflowId) return
  const saved = loadAIHistory(localStorage, historyIdentity, Date.now(), legacy.selectedWorkflowId) || legacy
  restoringConversation = true
  activeConversationWorkflowId = saved.selectedWorkflowId
  messages.value = saved.messages.length ? saved.messages : [welcomeMessage()]
  runs.value = saved.runs
  conversationId.value = saved.conversationId
  selectedWorkflowId.value = saved.selectedWorkflowId
  restoringConversation = false
  persistConversation()
}

function switchConversation(workflowId) {
  if (restoringConversation || workflowId === activeConversationWorkflowId) return
  persistConversation()
  activeConversationWorkflowId = workflowId
  const saved = workflowId ? loadAIHistory(localStorage, historyIdentity, Date.now(), workflowId) : null
  messages.value = saved?.messages?.length ? saved.messages : [welcomeMessage()]
  runs.value = saved?.runs || []
  conversationId.value = saved?.conversationId || ''
  question.value = ''
  selectedRunKey.value = ''
  traceVisible.value = false
  nextTick(scheduleScroll)
  persistConversation()
}

async function refreshRuleDraftStatuses() {
  if (!can('menu:rules')) return
  if (!messages.value.some(message => message?.ruleDraftPersisted === true && message?.ruleDraft?.id)) return
  try {
    const response = await api('/api/v1/rules?page=1&pageSize=100')
    reconcileRuleDraftMessages(messages.value, response?.items || [])
    persistConversation()
  } catch { /* keep the last known card state when rule status cannot be loaded */ }
}

const runtimeLoading = ref(false)
const runtimeError = ref('')
const workflowError = ref('')
let runtimeRequestSequence = 0
const runtime = ref({ items:[], active:{ id:'disabled', name:'未启用', enabled:false }, config:null, healthy:false, healthMessage:'正在读取模型服务状态' })
const workflows = ref({ items:[], healthy:false, healthMessage:'正在读取工作流状态' })
const selectedWorkflowId = ref('')
const creatingAgent = ref(false)
const agentTemplate = {
  schemaVersion:1, id:'my-status-agent', name:'我的状态助手', description:'回答当前租户的系统统计和设备状态问题。', version:'1.0.0', enabled:true,
  persona:'你是物联网系统状态助手。回答统计问题前必须调用 query_system_overview；询问具体设备时调用 query_device_latest。只依据工具结果回答，不得执行控制或修改操作。回答使用简洁中文。',
  defaultModel:'deepseek-flash', maxTokens:4096,
  capabilities:['系统状态统计','设备状态查询'],
  allowedTools:['mcp__iot__query_system_overview','mcp__iot__query_device_latest']
}
const agentJson = ref(JSON.stringify(agentTemplate, null, 2))
const editingAgentId = ref('')
const agentEditorRef = ref(null)
const agentPreviewVisible = ref(false)
const agentPreview = ref(null)
const workflowManageLoading = ref(false)
const workflowManageError = ref('')
const workflowManageItems = ref([])
const workflowManagePage = ref(1)
const workflowManagePageSize = ref(20)
const workflowManageTotal = ref(0)
let workflowManageRequestSequence = 0

const agentFieldDocs = [
  { name:'schemaVersion', type:'整数', note:'清单格式版本，当前固定填写 1。' },
  { name:'id', type:'字符串', note:'智能体唯一标识，最长 128 字符；可使用字母、数字、点、下划线、冒号和连字符，不能覆盖内置智能体。' },
  { name:'name', type:'字符串', note:'界面显示名称，必填，最长 128 字符。' },
  { name:'description', type:'字符串', note:'说明智能体的用途和适用场景，必填，最长 1024 字符。' },
  { name:'version', type:'字符串', note:'智能体版本号，必填，最长 64 字符，建议使用 1.0.0 格式。' },
  { name:'enabled', type:'布尔值', note:'是否立即启用；填写 true 后创建完成即可被选择和运行。' },
  { name:'persona', type:'字符串', note:'系统提示词，定义角色、回答原则和工具调用规则，必填，最长 16384 字符。' },
  { name:'defaultModel', type:'字符串', note:'默认模型标识，必填；实际运行会跟随当前模型服务的活动模型。' },
  { name:'maxTokens', type:'整数', note:'单次最大输出令牌数，平台允许 1–8192。' },
  { name:'capabilities', type:'字符串数组', note:'展示给用户的能力名称，填写 1–32 项，每项 1–64 字符且不可重复。' },
  { name:'allowedTools', type:'字符串数组', note:'智能体可以调用的受控工具，至少 1 项、最多 6 项，只能从下方白名单选择且不可重复；规则工具只能保存禁用草稿。' }
]
const agentToolDocs = [
  { name:'mcp__iot__query_system_overview', label:'系统状态与数量统计' },
  { name:'mcp__iot__query_device_latest', label:'设备最新状态' },
  { name:'mcp__iot__query_alarm_list', label:'告警列表' },
  { name:'mcp__iot__query_property_history', label:'设备属性历史' },
  { name:'mcp__iot__query_similar_alarms', label:'相似告警查询' },
  { name:'mcp__iot__query_knowledge_base', label:'知识库检索' },
  { name:'mcp__iot__create_rule_draft', label:'生成待确认的自动化规则草稿' }
]
const managementVisible = ref(false)
const agentEditorVisible = ref(false)
const runConfig = reactive({ model:'' })
const quickQuestions = computed(() => {
  if (!selectedWorkflow.value) return []
  const name = workflowName(selectedWorkflow.value)
  const tools = selectedWorkflow.value.allowedTools || selectedWorkflow.value.tools || []
  const capabilities = selectedCapabilities.value.map(capabilityLabel)
  const prompts = [`请介绍「${name}」可以协助处理哪些任务？`]
  if (tools.some(tool => String(tool).includes('query_alarm_list')) || capabilities.some(item => item.includes('告警'))) prompts.push('当前有哪些高等级活动告警？')
  if (tools.some(tool => String(tool).includes('query_device_latest')) || capabilities.some(item => item.includes('设备'))) prompts.push('请概览当前设备状态和异常设备。')
  if (tools.some(tool => String(tool).includes('query_knowledge_base')) || capabilities.some(item => item.includes('知识'))) prompts.push('查找与当前消防巡检相关的处置知识。')
  if (prompts.length === 1) prompts.push(`请按「${name}」的职责给出今天的工作建议。`)
  return prompts.slice(0, 3)
})

const nonChatWorkflowIds = new Set(['alarm-handler', 'device-health-inspector', 'protocol-assistant', 'rule-drafter'])
const workflowItems = computed(() => (workflows.value.items || []).filter(item => item.enabled !== false && isChatWorkflow(item)))
const selectedWorkflow = computed(() => workflowItems.value.find(item => workflowKey(item) === selectedWorkflowId.value))
const selectedRun = computed(() => runs.value.find(run => run.id === selectedRunKey.value) || null)
const activeHealthy = computed(() => Boolean(workflows.value.healthy))
const activeTone = computed(() => !workflowItems.value.length ? 'info' : activeHealthy.value ? 'success' : 'danger')
const healthMessage = computed(() => workflows.value.healthMessage || '工作流服务状态未知')
const isAdmin = computed(() => can('GET /api/v1/ai/workflows/admin'))
const selectedCapabilities = computed(() => {
  const value = selectedWorkflow.value?.capabilities || selectedWorkflow.value?.tools || []
  return Array.isArray(value) ? value : []
})
const agentPreviewJson = computed(() => agentPreview.value ? JSON.stringify(agentPreview.value, null, 2) : '')

function workflowKey(item) { return item?.id || item?.workflowId || '' }
function workflowName(item) { return item?.name || item?.label || workflowKey(item) || '未命名工作流' }
function capabilityLabel(item) { return capabilityName(typeof item === 'string' ? item : item?.name || item?.id) }
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

function scheduleScroll() {
  if (scrollQueued) return
  scrollQueued = true
  nextTick(() => {
    if (!scrollQueued) return
    scrollFrame = requestAnimationFrame(() => {
      if (log.value) log.value.scrollTop = log.value.scrollHeight
      scrollFrame = 0
      scrollQueued = false
    })
  })
}

function providerLabel(provider) {
  if (provider === 'disabled') return '未启用'
  return providerOptions.find(item => item.id === provider)?.label || provider || '未配置'
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
      scheduleScroll()
    }
    if (!state.value) pendingTextStates.delete(assistant)
  }, 50)
}

function flushAssistantText(assistant, shouldScroll = true) {
  const state = pendingTextStates.get(assistant)
  if (!state) return
  if (state.timer) clearTimeout(state.timer)
  state.timer = 0
  const buffered = state.value
  state.value = ''
  pendingTextStates.delete(assistant)
  if (buffered) {
    assistant.text += buffered
    if (shouldScroll) scheduleScroll()
  }
}

function flushPendingAssistantText(shouldScroll = true) {
  for (const assistant of pendingTextStates.keys()) flushAssistantText(assistant, shouldScroll)
}

async function loadRuntime() {
  const requestSequence = ++runtimeRequestSequence
  runtimeLoading.value = true
  runtimeError.value = ''
  workflowError.value = ''
  try {
    const [providerResult, workflowResult] = await Promise.allSettled([
      api('/api/v1/ai/providers?page=1&pageSize=100'),
      api('/api/v1/ai/workflows?page=1&pageSize=100')
    ])
    // A delete/create/update can start a newer refresh while this request is
    // still in flight. Never let the older response put a removed Agent back
    // into the workbench dropdown.
    if (requestSequence !== runtimeRequestSequence) return
    if (providerResult.status === 'fulfilled') {
      runtime.value = providerResult.value
    } else {
      runtimeError.value = providerResult.reason?.message || '模型服务状态读取失败'
    }
    if (workflowResult.status === 'fulfilled') {
      workflows.value = {
        items:Array.isArray(workflowResult.value?.items) ? workflowResult.value.items : [],
        healthy:Boolean(workflowResult.value?.healthy),
        healthMessage:workflowResult.value?.healthMessage || ''
      }
      if (!workflowItems.value.some(item => workflowKey(item) === selectedWorkflowId.value)) selectedWorkflowId.value = workflowKey(workflowItems.value[0])
      applyWorkflowDefaults()
    } else {
      workflowError.value = workflowResult.reason?.message || '工作流列表读取失败'
    }
  } finally {
    if (requestSequence === runtimeRequestSequence) runtimeLoading.value = false
  }
}

const builtinWorkflowIds = new Set(['alarm-handler', 'ops-assistant', 'system-observer', 'device-health-inspector', 'protocol-assistant', 'rule-drafter'])
function isBuiltinWorkflow(item) { return builtinWorkflowIds.has(workflowKey(item)) }
function isChatWorkflow(item) { return !nonChatWorkflowIds.has(workflowKey(item)) }

function resetAgentJson() { editingAgentId.value = ''; agentJson.value = JSON.stringify(agentTemplate, null, 2) }

function focusAgentEditor() {
  nextTick(() => {
    agentEditorRef.value?.focus?.()
    agentEditorRef.value?.$el?.scrollIntoView?.({ behavior:'auto', block:'center' })
  })
}

function startCreateAgent() {
  resetAgentJson()
  agentEditorVisible.value = true
  focusAgentEditor()
  UiMessage.info('已打开新建智能体，请填写配置清单结构化数据')
}

async function loadWorkflowManagement(force = false) {
  if (!isAdmin.value || workflowManageLoading.value && !force) return
  const requestSequence = ++workflowManageRequestSequence
  workflowManageLoading.value = true
  workflowManageError.value = ''
  try {
    const value = await api(`/api/v1/ai/workflows/admin?page=${workflowManagePage.value}&pageSize=${workflowManagePageSize.value}`)
    if (requestSequence !== workflowManageRequestSequence) return
    workflowManageItems.value = Array.isArray(value?.items) ? value.items.filter(isChatWorkflow) : []
    workflowManageTotal.value = Number(value?.total ?? value?.count ?? workflowManageItems.value.length)
  } catch (error) {
    if (requestSequence === workflowManageRequestSequence) workflowManageError.value = error.message || '工作流插件清单读取失败'
  } finally {
    if (requestSequence === workflowManageRequestSequence) workflowManageLoading.value = false
  }
}

function changeWorkflowManagePage(value) {
  workflowManagePage.value = value
  loadWorkflowManagement()
}

function changeWorkflowManagePageSize(value) {
  workflowManagePageSize.value = value
  workflowManagePage.value = 1
  loadWorkflowManagement()
}

function openAgentManagement() {
  managementVisible.value = true
  void loadWorkflowManagement()
}

function editAgent(item) {
  if (!isAdmin.value || isBuiltinWorkflow(item)) return
  editingAgentId.value = workflowKey(item)
  agentJson.value = JSON.stringify(item, null, 2)
  agentEditorVisible.value = true
  focusAgentEditor()
}

function cancelAgentEditor() {
  agentEditorVisible.value = false
  resetAgentJson()
}

function viewAgent(item) {
  if (!isAdmin.value || !isBuiltinWorkflow(item)) return
  agentPreview.value = item
  agentPreviewVisible.value = true
}

async function saveAgent() {
  if (!isAdmin.value || creatingAgent.value) return
  let manifest
  try { manifest = JSON.parse(agentJson.value) }
  catch { UiMessage.error('智能体结构化数据格式不正确'); return }
  if (editingAgentId.value && manifest?.id !== editingAgentId.value) {
    UiMessage.error('编辑时不能修改智能体的唯一标识；如需新插件请先新建')
    return
  }
  const editing = Boolean(editingAgentId.value)
  creatingAgent.value = true
  try {
    const saved = editing
      ? await api(`/api/v1/ai/workflows/${encodeURIComponent(editingAgentId.value)}`, { method:'PUT', body:JSON.stringify(manifest) })
      : await api('/api/v1/ai/workflows', { method:'POST', body:JSON.stringify(manifest) })
    await loadRuntime()
    await loadWorkflowManagement()
    selectedWorkflowId.value = workflowKey(saved)
    editingAgentId.value = ''
    agentJson.value = JSON.stringify(agentTemplate, null, 2)
    agentEditorVisible.value = false
    UiMessage.success(`${editing ? '智能体已更新' : '智能体已创建'}：${workflowName(saved)}`)
  } catch (error) { workflowManageError.value = error.message || (editing ? '智能体更新失败' : '智能体创建失败') }
  finally { creatingAgent.value = false }
}

async function toggleWorkflow(item) {
  if (!isAdmin.value || isBuiltinWorkflow(item) || creatingAgent.value) return
  const manifest = { ...item, enabled: item.enabled === false }
  creatingAgent.value = true
  try {
    await api(`/api/v1/ai/workflows/${encodeURIComponent(workflowKey(item))}`, { method:'PUT', body:JSON.stringify(manifest) })
    await Promise.all([loadRuntime(), loadWorkflowManagement()])
    UiMessage.success(`${workflowName(item)}已${manifest.enabled ? '启用' : '禁用'}`)
  } catch (error) { workflowManageError.value = error.message || '工作流状态更新失败' }
  finally { creatingAgent.value = false }
}

async function deleteWorkflow(item) {
  if (!isAdmin.value || isBuiltinWorkflow(item) || creatingAgent.value) return
  const deletedWorkflowId = workflowKey(item)
  try {
    await UiMessageBox.confirm(`删除后将无法运行“${workflowName(item)}”，确定继续吗？`, '删除工作流插件', { type:'warning', confirmButtonText:'确定删除', cancelButtonText:'取消' })
  } catch { return }
  creatingAgent.value = true
  try {
    await api(`/api/v1/ai/workflows/${encodeURIComponent(deletedWorkflowId)}`, { method:'DELETE' })
    // Remove it immediately so the current dropdown cannot keep a deleted
    // option while the authoritative catalog refresh is in flight.
    const remaining = (workflows.value.items || []).filter(candidate => workflowKey(candidate) !== deletedWorkflowId)
    workflows.value = { ...workflows.value, items:remaining, count:remaining.length }
    workflowManageItems.value = workflowManageItems.value.filter(candidate => workflowKey(candidate) !== deletedWorkflowId)
    if (selectedWorkflowId.value === deletedWorkflowId) selectedWorkflowId.value = workflowKey(remaining.find(candidate => candidate.enabled !== false))
    if (editingAgentId.value === deletedWorkflowId) cancelAgentEditor()
    await Promise.all([loadRuntime(), loadWorkflowManagement(true)])
    UiMessage.success(`已删除工作流插件：${workflowName(item)}`)
  } catch (error) { workflowManageError.value = error.message || '工作流插件删除失败' }
  finally { creatingAgent.value = false }
}

watch(selectedWorkflowId, workflowId => { switchConversation(workflowId); applyWorkflowDefaults() }, { flush:'sync' })

function applyWorkflowDefaults() {
  const workflow = selectedWorkflow.value
  runConfig.model = runtime.value.config?.model || runtime.value.active?.model || workflow?.defaultModel || workflow?.model || ''
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
      assistant.status = 'failed'
      assistant.error = failure
      assistant.text ||= '运行未能完成。'
      run.status = 'failed'
      run.error = failure
      run.durationMs = event.durationMs ?? Math.max(0, Date.now() - run.startedAt)
      run.finishedAt = timestamp(event.failedAt || event.createdAt)
      for (const tool of run.tools.filter(item => item.status === 'running')) tool.status = 'failed'
      addRunEvent(run, event, '工作流服务运行失败', 'danger', failure.message)
      break
    }
  }
}

async function send(textValue) {
  const text = (textValue || question.value).trim()
  if (!text || sending.value) return
  if (!conversationId.value) conversationId.value = makeId('conversation')
  messages.value.push({ id:makeId('message'), role:'user', status:'succeeded', text, tools:[] })
  messages.value.push({ id:makeId('message'), role:'assistant', status:'streaming', text:'', prompt:text, tools:[], error:null })
  const assistant = messages.value[messages.value.length - 1]
  runs.value.unshift({ id:makeId('run'), runId:'', traceId:'', status:'running', workflowId:selectedWorkflowId.value, workflowName:workflowName(selectedWorkflow.value), provider:'', model:runConfig.model || selectedWorkflow.value?.defaultModel || selectedWorkflow.value?.model || '', startedAt:Date.now(), finishedAt:null, durationMs:null, usage:null, events:[], tools:[], error:null })
  const run = runs.value[0]
  assistant.runKey = run.id
  assistant.tools = run.tools
  question.value = ''
  sending.value = true
  scheduleScroll()

  const controller = new AbortController()
  abortController = controller
  const body = { question:text, conversationId:conversationId.value, workflowId:selectedWorkflowId.value, model:runConfig.model || selectedWorkflow.value?.defaultModel || selectedWorkflow.value?.model || '' }

  try {
    await apiStream('/api/v1/ai/chat/stream', { method:'POST', body:JSON.stringify(body), signal:controller.signal }, event => applyStreamEvent(event, assistant, run))
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
    scheduleScroll()
  }
}

function stop() { abortController?.abort() }
function retry(message) { if (!sending.value) send(message.prompt) }
function clearConversation() { abortController?.abort(); messages.value = [welcomeMessage()]; runs.value = []; conversationId.value = ''; selectedRunKey.value = ''; traceVisible.value = false; persistConversation() }
function openTrace(value) { selectedRunKey.value = value?.runKey || value?.id || ''; traceVisible.value = true }
function actionSummary(action) { return action?.type === 'OPEN_CAMERA' ? `打开摄像头 ${action.cameraId}` : action?.type === 'OPEN_PAGE' ? `打开页面 ${action.page}` : action?.type || '未知动作' }
function ruleDraftStatusLabel(message) { return message.ruleDraftState === 'enabled' ? '已启用' : message.ruleDraftState === 'missing' ? '已删除' : message.ruleDraftPersisted ? '已保存草稿' : '待人工确认' }
function ruleDraftStatusType(message) { return message.ruleDraftState === 'enabled' ? 'success' : message.ruleDraftState === 'missing' ? 'info' : 'warning' }
function ruleDraftActionLabel(message) { return message.ruleDraftState === 'enabled' ? '规则已启用' : message.ruleDraftState === 'missing' ? '规则已删除' : message.ruleDraftPersisted ? '查看并启用规则' : '检查并保存规则' }
function ruleDraftActionDisabled(message) { return message.ruleDraftState === 'enabled' || message.ruleDraftState === 'missing' }
function editRuleDraft(draft, persisted, state) { if (state === 'enabled' || state === 'missing') return; emit('navigate', 'rules', { ruleDraft:draft, persisted:Boolean(persisted) }) }

watch([messages, runs, conversationId, selectedWorkflowId], scheduleConversationPersist, { deep:true })
onMounted(() => { restoreConversation(); return Promise.all([loadRuntime(), refreshRuleDraftStatuses()]) })
onBeforeUnmount(() => { abortController?.abort(); flushPendingAssistantText(false); if (scrollFrame) cancelAnimationFrame(scrollFrame); scrollFrame = 0; scrollQueued = false; if (historyTimer) clearTimeout(historyTimer); persistConversation() })
</script>

<template>
  <div class="ai-runtime" v-loading="runtimeLoading">
    <div><span class="section-kicker">智能助手</span><strong>智能助手</strong><small>查询设备、告警和知识，查看每次回答的依据与执行过程。</small></div>
    <div class="runtime-actions"><div class="runtime-status"><ui-tag :type="activeTone" effect="light">{{ selectedWorkflow ? '工作流服务' : '未配置' }}</ui-tag><span>{{ providerLabel(runtime.config?.provider || runtime.active?.id) }} · {{ runConfig.model || '无活动模型' }}</span><i :class="{ online:activeHealthy }" />{{ healthMessage }}</div><ui-button v-permission="'GET /api/v1/ai/workflows/admin'" size="small" @click="openAgentManagement">智能体管理</ui-button><ui-button size="small" :loading="runtimeLoading" @click="loadRuntime">刷新状态</ui-button></div>
  </div>
  <ui-alert v-if="runtimeError" class="runtime-warning" :title="runtimeError" type="warning" :closable="false" show-icon />

  <div class="ai-workbench">
    <ui-card shadow="never" class="surface-card chat-card ai-chat-card">
      <template #header>
        <div class="card-header chat-header">
          <div class="chat-workflow">
            <div class="chat-workflow-label"><strong>工作流插件</strong><small>各插件独立保存会话</small></div>
            <ui-select v-model="selectedWorkflowId" class="chat-workflow-select" aria-label="工作流插件" placeholder="选择工作流插件" :disabled="sending || !workflowItems.length"><ui-option v-for="item in workflowItems" :key="workflowKey(item)" :label="workflowName(item)" :value="workflowKey(item)" /></ui-select>
          </div>
          <div class="chat-header-actions"><ui-button plain size="small" :disabled="!runs.length" @click="openTrace(runs[0])">运行轨迹</ui-button><ui-button plain type="warning" size="small" :disabled="sending" @click="clearConversation">清空对话</ui-button></div>
        </div>
      </template>
      <ui-alert v-if="workflowError" class="chat-workflow-error" :title="workflowError" type="error" :closable="false" show-icon><ui-button plain size="small" @click="loadRuntime">重新加载</ui-button></ui-alert>
      <ui-alert v-if="!runtimeLoading && !workflowItems.length && !workflowError" class="chat-workflow-empty" title="暂无可用工作流：需由管理员配置 AI 工作流服务后才能提问。" type="info" :closable="false" show-icon />
      <div v-if="quickQuestions.length" class="quick-prompts"><span class="quick-prompts-label">快捷提问</span><div class="quick-prompts-list"><button v-for="item in quickQuestions" :key="item" :disabled="sending || !workflowItems.length" @click="send(item)">{{ item }}</button></div></div>
      <div ref="log" class="chat-log" aria-live="polite">
        <div v-for="message in messages" :key="message.id" class="message-row" :class="message.role">
          <span class="message-avatar">{{ message.role === 'assistant' ? '智能' : '我' }}</span><div class="message-content"><div class="chat-message" :class="[message.role,`is-${message.status}`]"><MarkdownContent v-if="message.text && message.role === 'assistant' && message.status !== 'streaming'" :source="message.text" /><p v-else-if="message.text">{{ message.text }}</p><div v-else-if="message.status === 'streaming'" class="typing"><i/><i/><i/><span>正在运行工作流</span></div><ToolCallCard v-for="tool in message.tools" :key="tool.id || tool.toolCallId" :tool="tool" /><div v-if="message.ruleDraft" class="rule-draft-card"><div><strong>{{ message.ruleDraft.name || '自动化规则草稿' }}</strong><ui-tag :type="ruleDraftStatusType(message)" size="small">{{ ruleDraftStatusLabel(message) }}</ui-tag></div><small>{{ message.ruleDraft.conditions?.length || 0 }} 个条件 · {{ message.ruleDraft.actions?.map(actionSummary).join('、') || '仅告警' }}</small><ui-button type="primary" size="small" :disabled="ruleDraftActionDisabled(message)" @click="editRuleDraft(message.ruleDraft, message.ruleDraftPersisted, message.ruleDraftState)">{{ ruleDraftActionLabel(message) }}</ui-button></div><div v-if="message.error" class="message-error"><strong>{{ message.error.message }}</strong><small v-if="message.error.code || message.error.stage">{{ [message.error.code,message.error.stage].filter(Boolean).join(' · ') }}</small><small v-if="message.traceId || message.error.traceId">追踪编号 · {{ message.traceId || message.error.traceId }}</small><ui-button v-permission="'POST /api/v1/ai/chat/stream'" v-if="message.prompt" plain size="small" :disabled="sending" @click="retry(message)">重新运行</ui-button></div></div><div v-if="message.role === 'assistant' && message.runKey" class="message-meta"><span v-if="message.status === 'streaming'">运行中</span><span v-else>{{ message.status === 'succeeded' ? '已完成' : message.status === 'canceled' ? '已停止' : '运行失败' }}</span><span v-if="message.durationMs != null">{{ message.durationMs }} 毫秒</span><span v-if="message.usage?.totalTokens != null">{{ message.usage.totalTokens }} 词元</span><ui-button plain size="small" @click="openTrace(message)">查看轨迹</ui-button></div></div>
        </div>
      </div>
      <div class="chat-compose"><ui-input v-model="question" type="textarea" :autosize="{ minRows:1,maxRows:4 }" maxlength="4000" resize="none" placeholder="询问设备、告警、趋势或处置知识；上档键与回车键换行" :disabled="sending || !workflowItems.length" @keydown.enter.exact.prevent="send()" /><ui-button v-if="sending" type="danger" plain @click="stop">停止</ui-button><ui-button v-permission="'POST /api/v1/ai/chat/stream'" v-else type="primary" :disabled="!question.trim() || !workflowItems.length" @click="send()">发送</ui-button></div><small class="chat-notice">智能输出仅供辅助判断，不会自动执行设备控制或启用规则。</small>
    </ui-card>
  </div>

  <ui-drawer v-model="managementVisible" title="智能体管理" size="min(760px, 94vw)" class="workflow-manager" append-to-body>
    <section class="manager-panel panel-agent">
        <div class="manager-intro"><span>智能体管理</span><div><h3>智能体插件管理</h3><ui-tag size="small" type="primary" effect="plain">管理员</ui-tag></div><p>内置智能体仅可查看；动态智能体可编辑、启用/禁用或删除。点击“新建智能体”即可在弹窗中提交新的配置清单，保存后智能体会立即进入工作流列表。</p></div>
        <ui-alert v-if="!isAdmin" title="工作流插件管理仅限管理员。" type="warning" :closable="false" show-icon />
        <div v-else class="workflow-admin-panel">
          <div class="workflow-admin-toolbar"><div><strong>已配置的工作流插件</strong><small>{{ workflowManageTotal }} 个聊天插件 · 内置聊天智能体只读；告警研判、设备巡检和协议接入由业务页面调用</small></div><div><ui-button size="small" :loading="workflowManageLoading" @click="loadWorkflowManagement">刷新清单</ui-button><ui-button v-permission="'POST /api/v1/ai/workflows'" size="small" type="primary" plain @click="startCreateAgent">新建智能体</ui-button></div></div>
          <ui-alert v-if="workflowManageError" :title="workflowManageError" type="error" :closable="false" show-icon />
          <ui-skeleton v-if="workflowManageLoading && !workflowManageItems.length" :rows="4" animated />
          <ui-empty v-else-if="!workflowManageItems.length" description="暂无工作流插件" :image-size="56" />
          <div v-else class="workflow-admin-list">
            <div v-for="item in workflowManageItems" :key="workflowKey(item)" class="workflow-admin-item">
              <div class="workflow-admin-main"><div><strong>{{ workflowName(item) }}</strong><ui-tag size="small" :type="item.enabled === false ? 'info' : 'success'" effect="plain">{{ item.enabled === false ? '已禁用' : '已启用' }}</ui-tag><ui-tag v-if="isBuiltinWorkflow(item)" size="small" effect="plain">内置只读</ui-tag></div><small>{{ workflowKey(item) }} · {{ item.version ? `v${item.version}` : '无版本' }}</small><p>{{ item.description || '未填写插件说明' }}</p></div>
              <div class="workflow-admin-actions"><template v-if="isBuiltinWorkflow(item)"><ui-button size="small" type="primary" plain @click="viewAgent(item)">查看</ui-button></template><template v-else><ui-button v-permission="'PUT /api/v1/ai/workflows/:id'" size="small" @click="editAgent(item)">编辑</ui-button><ui-button v-permission="'PUT /api/v1/ai/workflows/:id'" size="small" @click="toggleWorkflow(item)">{{ item.enabled === false ? '启用' : '禁用' }}</ui-button><ui-button v-permission="'DELETE /api/v1/ai/workflows/:id'" size="small" type="danger" plain @click="deleteWorkflow(item)">删除</ui-button></template></div>
            </div>
          </div>
          <div v-if="workflowManageTotal" class="list-pagination">
            <ui-pagination v-model:current-page="workflowManagePage" v-model:page-size="workflowManagePageSize" :total="workflowManageTotal" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next, jumper" @current-change="changeWorkflowManagePage" @size-change="changeWorkflowManagePageSize" />
          </div>
        </div>
      </section>
  </ui-drawer>
  <ui-dialog v-model="agentEditorVisible" :title="editingAgentId ? '编辑智能体' : '新建智能体'" width="min(820px, 94vw)" class="agent-editor-dialog" append-to-body destroy-on-close>
    <ui-alert title="结构化数据标准不支持注释。内置智能体仅可查看，动态智能体可在管理清单中编辑、启用或删除。" type="info" :closable="false" show-icon />
    <ui-form class="drawer-form" label-position="top" :disabled="!isAdmin || creatingAgent">
      <ui-form-item label="智能体配置清单"><ui-input ref="agentEditorRef" v-model="agentJson" class="agent-json-editor" type="textarea" :rows="18" resize="vertical" spellcheck="false" /></ui-form-item>
      <div class="manifest-guide">
        <div class="manifest-guide-title"><div><strong>字段说明</strong><small>所有字段均为必填，请参考下方说明填写。</small></div><ui-tag size="small" effect="plain">11 个字段</ui-tag></div>
        <div class="manifest-field-list">
          <div v-for="field in agentFieldDocs" :key="field.name" class="manifest-field"><code>{{ field.name }}</code><span>{{ field.type }}</span><p>{{ field.note }}</p></div>
        </div>
        <div class="tool-whitelist"><strong>允许使用的工具</strong><div><span v-for="tool in agentToolDocs" :key="tool.name"><code>{{ tool.name }}</code><small>{{ tool.label }}</small></span></div></div>
      </div>
      <ui-alert title="只允许受控查询工具与“仅生成、不保存”的规则草稿工具；内置智能体不能覆盖，智能体不能直接启用规则。" type="info" :closable="false" show-icon />
    </ui-form>
    <template #footer><ui-button @click="cancelAgentEditor">取消</ui-button><ui-button v-permission="['POST /api/v1/ai/workflows','PUT /api/v1/ai/workflows/:id']" type="primary" :loading="creatingAgent" @click="saveAgent">{{ editingAgentId ? '校验并保存修改' : '校验并创建智能体' }}</ui-button></template>
  </ui-dialog>
  <ui-dialog v-model="agentPreviewVisible" title="查看内置智能体配置清单（只读）" width="min(760px, 92vw)" append-to-body>
    <ui-alert title="内置智能体仅供查看，不能编辑、启用/禁用或删除。" type="info" :closable="false" show-icon />
    <div v-if="agentPreview" class="agent-preview-summary"><div><strong>{{ workflowName(agentPreview) }}</strong><ui-tag size="small" effect="plain">内置只读</ui-tag></div><small>{{ workflowKey(agentPreview) }} · {{ agentPreview.version ? `v${agentPreview.version}` : '无版本' }} · {{ agentPreview.defaultModel || '未设置模型' }}</small><p>{{ agentPreview.description || '未填写插件说明' }}</p></div>
    <pre class="agent-manifest-preview">{{ agentPreviewJson }}</pre>
    <template #footer><ui-button @click="agentPreviewVisible = false">关闭</ui-button></template>
  </ui-dialog>
  <HarnessTraceDrawer v-model="traceVisible" :run="selectedRun" />
</template>

<style scoped>
.chat-card { height: 100%; min-height: 520px; }
.chat-log { flex: 1; padding: 10px var(--space-2); overflow: auto; }
.chat-message { max-width: min(76%, 720px); margin: 9px 0; padding: 11px 14px; line-height: 1.7; white-space: pre-wrap; border-radius: 14px; }
.chat-message.assistant { background: var(--surface-hover); border-bottom-left-radius: var(--radius-sm); }
.chat-message.user { margin-left: auto; color: var(--text-inverse); background: var(--primary); border-bottom-right-radius: var(--radius-sm); }
.chat-compose { display: flex; gap: 9px; padding-top: 14px; border-top: 1px solid var(--border); }
.chat-compose .ui-input { flex: 1; }
@media (max-width: 767px) {
  .chat-card { min-height: 440px; }
  .chat-message { max-width: 88%; }
}
.provider-select-row { width:100%; min-width:0; }
.ai-chat-card :deep(.n-card-content) { flex: 1 1 auto; min-height: 0; overflow: hidden; } /* Naive UI 卡片正文承接内部滚动区域。 */
.ai-chat-card :deep(.n-card-content) { display: flex; flex-direction: column; } /* 对话记录可在固定高度卡片内独立滚动。 */
.ai-runtime { flex:none; min-height:74px; margin-bottom:16px; padding:15px 18px; display:flex; align-items:center; justify-content:space-between; gap:20px; background:var(--surface); border:1px solid var(--border); border-left:3px solid var(--primary); border-radius:4px; }.ai-runtime>div:first-child { display:grid; gap:3px; }.section-kicker { color:var(--primary); font-size:12px; font-weight:700; letter-spacing:.14em; }.ai-runtime strong { font-size:16px; }.ai-runtime small { color:var(--text-muted); }.runtime-actions { display:flex; align-items:center; gap:12px; }.runtime-status { display:flex; align-items:center; gap:8px; color:var(--text); font-size:13px; white-space:nowrap; }.runtime-status i { width:7px; height:7px; background:var(--danger); border-radius:50%; }.runtime-status i.online { background:var(--success); } /* 状态说明使用可读的前景色变量。 */
.ai-workbench { flex:1; min-height:0; overflow:hidden; display:grid; grid-template-rows:minmax(0,1fr); grid-template-columns:minmax(280px,320px) minmax(0,1fr); gap:16px; align-items:stretch; }.control-card,.ai-chat-card { height:100%; min-height:0; display:flex; flex-direction:column; }.control-card :deep(.n-card-header),.ai-chat-card :deep(.n-card-header) { flex:none; }.control-card :deep(.n-card-content),.ai-chat-card :deep(.n-card-content) { flex:1; min-height:0; overflow:hidden; }.card-header>div { display:grid; gap:3px; }.card-header small { display:block; }.control-card :deep(.n-card-content) { display:flex; padding:0; }.control-scroll { flex:1; min-width:0; min-height:0; padding:16px; overflow:auto; overscroll-behavior:contain; }.control-scroll>.ui-alert { margin-bottom:14px; }.workflow-description { margin:-3px 0 14px; padding:12px; background:var(--surface-muted); border:1px solid var(--info-border); border-radius:4px; }.workflow-description>div:first-child { display:flex; align-items:center; gap:9px; }.workflow-description>div:first-child>div { display:grid; gap:2px; }.workflow-description strong { color:var(--primary); font-size:13px; }.workflow-description small { color:var(--text-muted); font-size:12px; }.workflow-description p { margin:9px 0; color:var(--text); font-size:12px; line-height:1.6; }.workflow-icon { width:30px; height:30px; display:grid; place-items:center; color:var(--surface); background:var(--primary-soft); border-radius:4px; font-size:12px; font-weight:800; }.capability-list { display:flex; flex-wrap:wrap; gap:5px; }.run-config { margin-bottom:2px; }.run-config>div { display:grid; grid-template-columns:minmax(0,1fr) 112px; gap:9px; }.run-config :deep(.ui-input-number) { width:100%; }.provider-summary { margin:0 0 14px; padding:11px; display:grid; gap:7px; background:var(--surface); border:1px solid var(--border); border-radius:4px; }.provider-summary>span { color:var(--text-muted); font-size:12px; }.provider-summary strong { font-size:13px; }.provider-summary small { color:var(--text); }.sandbox-collapse { border-top:1px solid var(--border); }.collapse-title { width:100%; padding-right:8px; display:flex; align-items:center; justify-content:space-between; }.collapse-title>div { display:grid; gap:2px; }.collapse-title strong { font-size:13px; }.collapse-title small { color:var(--text-muted); font-size:12px; }.plugin-description { margin:-4px 0 15px; display:grid; gap:4px; }.plugin-description strong { color:var(--primary); font-size:12px; }.plugin-description span { color:var(--text); font-size:12px; line-height:1.5; }.provider-select-row { display:grid; grid-template-columns:minmax(0,1fr) auto; gap:7px; }.provider-select-row :deep(.ui-select) { width:100%; }.provider-profile-toolbar { margin:-5px 0 12px; display:flex; align-items:center; justify-content:space-between; gap:8px; }.provider-profile-toolbar small { color:var(--text-muted); font-size:12px; line-height:1.5; }.provider-profile-toolbar>div { display:flex; gap:5px; flex:none; }.provider-profile-editor { margin:0 0 13px; padding:11px; background:var(--surface-muted); border:1px solid var(--primary); border-radius:5px; }.provider-profile-editor :deep(.ui-form-item) { margin-bottom:10px; }.provider-profile-actions { display:flex; justify-content:flex-end; gap:7px; margin-top:10px; }.admin-notice { margin-bottom:14px; }.test-button { width:100%; margin-top:12px; }.test-result { margin-top:14px; padding:11px; border:1px solid; border-radius:4px; }.test-result.success { background:var(--surface-muted); border-color:var(--success-border); }.test-result.failed { background:var(--surface-muted); border-color:var(--danger-border); }.test-result>div { display:flex; justify-content:space-between; align-items:center; }.test-result p { margin:8px 0; color:var(--text); font-size:12px; line-height:1.6; white-space:pre-wrap; }.test-result small { color:var(--text-muted); word-break:break-all; }
.knowledge-summary { margin:0 0 14px; padding:11px; display:grid; gap:5px; background:var(--surface-muted); border:1px solid var(--success-border); border-radius:4px; }.knowledge-summary>div { display:flex; align-items:center; justify-content:space-between; }.knowledge-summary span,.knowledge-summary small { color:var(--success-text); font-size:12px; }.knowledge-summary strong { color:var(--success-text); font-size:12px; }.binding-numbers { display:grid; grid-template-columns:1fr 1fr; gap:9px; }.binding-numbers :deep(.ui-input-number),.sandbox-collapse :deep(.ui-select),.sandbox-collapse :deep(.ui-radio-group) { width:100%; }.sandbox-collapse :deep(.n-radio-button) { flex:1; }.sandbox-collapse :deep(.n-radio-button__label) { width:100%; padding-left:7px; padding-right:7px; }
.agent-json-editor :deep(textarea) { font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace; font-size:12px; line-height:1.55; }.agent-actions { margin-top:12px; display:flex; justify-content:flex-end; gap:8px; }
.agent-preview-summary { margin-top:14px; padding:12px; display:grid; gap:5px; background:var(--surface-muted); border:1px solid var(--info-border); border-radius:5px; }.agent-preview-summary>div { display:flex; align-items:center; gap:7px; }.agent-preview-summary strong { color:var(--text-strong); font-size:13px; }.agent-preview-summary small { color:var(--text); font-size:12px; }.agent-preview-summary p { margin:0; color:var(--text); font-size:12px; line-height:1.6; }.agent-manifest-preview { max-height:min(58vh,560px); margin:12px 0 0; padding:14px; overflow:auto; color:var(--text-strong); background:var(--surface); border:1px solid var(--info-border); border-radius:5px; font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace; font-size:12px; line-height:1.65; white-space:pre-wrap; word-break:break-word; }
.workflow-admin-panel { margin-bottom:18px; padding:12px; background:var(--surface); border:1px solid var(--info-border); border-radius:6px; }.workflow-admin-toolbar { display:flex; align-items:center; justify-content:space-between; gap:12px; margin-bottom:10px; }.workflow-admin-toolbar>div:first-child { min-width:0; display:grid; gap:3px; }.workflow-admin-toolbar strong { color:var(--text-strong); font-size:13px; }.workflow-admin-toolbar small { color:var(--text-muted); font-size:12px; }.workflow-admin-toolbar>div:last-child { display:flex; gap:6px; flex:none; }.workflow-admin-list { display:grid; gap:7px; }.workflow-admin-item { padding:10px; display:flex; align-items:flex-start; justify-content:space-between; gap:12px; background:var(--surface); border:1px solid var(--border); border-radius:5px; }.workflow-admin-main { min-width:0; display:grid; gap:4px; }.workflow-admin-main>div { min-width:0; display:flex; align-items:center; flex-wrap:wrap; gap:5px; }.workflow-admin-main strong { max-width:260px; overflow:hidden; color:var(--text); font-size:12px; text-overflow:ellipsis; white-space:nowrap; }.workflow-admin-main small { overflow:hidden; color:var(--text-muted); font-size:12px; text-overflow:ellipsis; white-space:nowrap; }.workflow-admin-main p { margin:2px 0 0; overflow:hidden; color:var(--text); font-size:12px; line-height:1.5; text-overflow:ellipsis; white-space:nowrap; }.workflow-admin-actions { display:flex; flex:none; align-items:center; gap:5px; }.workflow-admin-actions .ui-button { margin-left:0; }
.manifest-guide { margin:-2px 0 16px; overflow:hidden; background:var(--surface); border:1px solid var(--info-border); border-radius:6px; }.manifest-guide-title { padding:12px 14px; display:flex; align-items:center; justify-content:space-between; gap:12px; background:var(--surface-muted); border-bottom:1px solid var(--info-border); }.manifest-guide-title>div { display:grid; gap:3px; }.manifest-guide-title strong { color:var(--primary); font-size:13px; }.manifest-guide-title small { color:var(--text); font-size:12px; }.manifest-field-list { display:grid; }.manifest-field { padding:10px 14px; display:grid; grid-template-columns:124px 74px minmax(0,1fr); align-items:start; gap:10px; border-bottom:1px solid var(--border); }.manifest-field:last-child { border-bottom:0; }.manifest-field code,.tool-whitelist code { color:var(--primary); font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace; font-size:12px; font-weight:700; word-break:break-all; }.manifest-field>span { width:max-content; padding:2px 6px; color:var(--text-muted); background:var(--surface-muted); border-radius:3px; font-size:12px; }.manifest-field p { margin:0; color:var(--text); font-size:12px; line-height:1.55; }.tool-whitelist { padding:13px 14px; display:grid; gap:10px; background:var(--surface); border-top:1px solid var(--info-border); }.tool-whitelist>strong { color:var(--text); font-size:12px; }.tool-whitelist>div { display:grid; grid-template-columns:1fr 1fr; gap:7px; }.tool-whitelist span { min-width:0; padding:8px 9px; display:grid; gap:3px; background:var(--surface); border-radius:4px; }.tool-whitelist small { color:var(--text); font-size:12px; }
.ai-chat-card :deep(.n-card-content) { display:flex; flex-direction:column; }.chat-header>div:last-child { display:flex; align-items:center; }.quick-prompts { flex:none; display:flex; flex-wrap:wrap; gap:7px; padding-bottom:12px; border-bottom:1px solid var(--border); }.quick-prompts button { padding:6px 9px; color:var(--primary); background:var(--surface-muted); border:1px solid var(--info-border); border-radius:3px; font-size:12px; cursor:pointer; }.quick-prompts button:hover:not(:disabled) { color:var(--surface); background:var(--primary-soft); border-color:var(--primary); }.quick-prompts button:disabled { opacity:.5; cursor:not-allowed; }.chat-log { min-height:0; flex:1; padding:15px 3px 6px; overflow:auto; overscroll-behavior:contain; }.message-row { display:flex; align-items:flex-start; gap:9px; }.message-row.user { flex-direction:row-reverse; }.message-avatar { width:28px; height:28px; flex:0 0 28px; display:grid; place-items:center; color:var(--surface); background:var(--text); border-radius:4px; font-size:12px; font-weight:700; }.message-row.user .message-avatar { background:var(--primary-soft); }.message-content { width:100%; min-width:0; max-width:min(82%,760px); margin-bottom:14px; }.message-row.user .message-content { width:fit-content; max-width:min(82%,760px); display:flex; flex-direction:column; align-items:flex-end; }.chat-message { width:100%; max-width:none; box-sizing:border-box; margin:0; padding:10px 12px; color:var(--text); background:var(--surface-muted); border-radius:4px; font-size:13px; line-height:1.75; word-break:break-word; }.message-row.user .chat-message { width:fit-content; max-width:100%; }.chat-message.user { color:var(--surface); background:var(--primary-soft); }.chat-message.is-failed { background:var(--surface-muted); border:1px solid var(--danger-border); }.chat-message.is-canceled { color:var(--text); background:var(--surface); border:1px dashed var(--border-strong); }.chat-message p { margin:0; white-space:pre-wrap; }.typing { min-width:150px; display:flex; align-items:center; gap:5px; color:var(--text-muted); }.typing i { width:5px; height:5px; background:var(--text-muted); border-radius:50%; animation:pulse 1s infinite; }.typing i:nth-child(2){animation-delay:.16s}.typing i:nth-child(3){animation-delay:.32s}.typing span { margin-left:4px; font-size:12px; }.message-error { margin-top:9px; padding-top:9px; display:grid; gap:3px; border-top:1px solid var(--danger-border); }.message-error strong { color:var(--danger); font-size:12px; }.message-error small { color:var(--text-muted); font-size:12px; word-break:break-all; }.message-error .ui-button { width:max-content; height:auto; margin-top:3px; padding:0; }.message-meta { margin-top:5px; display:flex; align-items:center; gap:8px; color:var(--text-muted); font-size:12px; }.message-meta button { padding:0; color:var(--primary); background:none; border:0; font-size:12px; cursor:pointer; }.chat-compose { flex:none; display:flex; align-items:flex-end; gap:9px; padding-top:11px; border-top:1px solid var(--border); }.chat-compose .ui-button { min-width:72px; }.chat-notice { margin-top:8px; color:var(--text-muted); text-align:center; }
.rule-draft-card { margin-top:10px; padding:10px; display:grid; gap:7px; background:var(--surface-muted); border:1px solid var(--warning); border-radius:4px; }.rule-draft-card>div { display:flex; align-items:center; justify-content:space-between; gap:8px; }.rule-draft-card small { color:var(--text); }.rule-draft-card .ui-button { width:max-content; }
.ai-workbench { grid-template-columns:minmax(250px,286px) minmax(0,1fr); }
.control-section-label { margin:2px 0 10px; display:flex; align-items:center; gap:7px; color:var(--text); font-size:12px; font-weight:700; letter-spacing:.02em; }.control-section-label span { width:22px; height:18px; display:grid; place-items:center; color:var(--primary); background:var(--surface-muted); border-radius:3px; font-size:12px; }
.runtime-overview { display:grid; grid-template-columns:1fr 1fr; gap:8px; }.overview-item { min-width:0; padding:10px; display:grid; gap:3px; color:inherit; text-align:left; background:var(--surface); border:1px solid var(--border); border-radius:4px; cursor:pointer; transition:border-color .15s,background .15s; }.overview-item:hover { background:var(--surface-muted); border-color:var(--primary); }.overview-item span { color:var(--text-muted); font-size:12px; }.overview-item strong { overflow:hidden; color:var(--text); font-size:12px; text-overflow:ellipsis; white-space:nowrap; }.overview-item small { overflow:hidden; color:var(--text); font-size:12px; text-overflow:ellipsis; white-space:nowrap; }.runtime-warning { margin-top:10px; }.manager-entry { width:100%; margin-top:12px; padding:11px 12px; display:flex; align-items:center; justify-content:space-between; color:inherit; text-align:left; background:var(--surface); border:1px solid var(--border-strong); border-radius:4px; cursor:pointer; }.manager-entry:hover { background:var(--surface-muted); border-color:var(--primary); }.manager-entry>span { display:grid; gap:2px; }.manager-entry strong { font-size:12px; }.manager-entry small { color:var(--text-muted); font-size:12px; }.manager-entry b { color:var(--primary); font-size:12px; }
.runtime-overview-single { grid-template-columns:1fr; }.overview-item-static { cursor:default; }.overview-item-static:hover { background:var(--surface); border-color:var(--border); }
.overview-item:first-child { background:var(--surface-muted); border-color:var(--primary); }.overview-item:first-child:hover { border-color:var(--primary); }.overview-item:first-child strong { color:var(--primary); }.overview-item:last-child { background:var(--surface-muted); border-color:var(--success-border); }.overview-item:last-child:hover { border-color:var(--success); }.overview-item:last-child strong { color:var(--success-text); }
.overview-item-static { background:var(--surface); border-color:var(--border); }.overview-item-static:hover { background:var(--surface); border-color:var(--border); }.overview-item-static strong { color:var(--text); }
.manager-panel { animation:manager-in .16s ease-out; }.manager-intro { margin-bottom:20px; padding:16px; background:var(--surface-muted); border:1px solid var(--info-border); border-left:4px solid var(--primary); border-radius:6px; }.manager-intro>span { color:var(--primary); font-size:12px; font-weight:700; letter-spacing:.12em; }.manager-intro>div { margin-top:5px; display:flex; align-items:center; justify-content:space-between; gap:12px; }.manager-intro h3 { margin:0; color:var(--text-strong); font-size:16px; }.manager-intro p { margin:7px 0 0; color:var(--text); font-size:12px; line-height:1.6; }.drawer-form :deep(.ui-select),.drawer-form :deep(.ui-radio-group) { width:100%; }.drawer-form :deep(.n-radio-button) { flex:1; }.drawer-form :deep(.n-radio-button__label) { width:100%; }.workflow-manager :deep(.n-drawer-header) { margin-bottom:0; padding-bottom:16px; border-bottom:1px solid var(--border); }.workflow-manager :deep(.n-drawer-body-content-wrapper) { padding-top:16px; }.agent-editor-dialog :deep(.n-card-content) { max-height:calc(100vh - 210px); overflow-y:auto; overscroll-behavior:contain; }.agent-editor-dialog .drawer-form { margin-top:14px; }
@keyframes manager-in { from { opacity:0; transform:translateX(6px); } }
@keyframes pulse { 50% { opacity:.28; transform:translateY(-2px); } }
@media (max-width:1120px) {.ai-runtime { gap:10px; flex-wrap:wrap; }.runtime-status { white-space:normal; flex-wrap:wrap; }.quick-prompts { flex-wrap:nowrap; overflow-x:auto; }.quick-prompts button { flex:none; } }
@media (max-width:640px) { .quick-prompts { flex-wrap:wrap; overflow-x:visible; }.quick-prompts button { flex:1 1 100%; min-width:0; white-space:normal; text-align:left; overflow-wrap:anywhere; } }
@media (max-width:640px) { .ai-runtime>div:first-child { display:none; }.ai-runtime { padding:10px 12px; min-height:0; }.runtime-status { font-size:12px; }.chat-header small { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; max-width:220px; }.ai-runtime { align-items:flex-start; flex-direction:column; }.runtime-actions,.runtime-status { width:100%; white-space:normal; flex-wrap:wrap; }.runtime-actions { align-items:flex-start; }.runtime-actions .ui-button:first-of-type { margin-left:auto; }.runtime-overview { grid-template-columns:1fr; }.chat-header { align-items:flex-start; gap:8px; }.chat-header>div:last-child { flex-wrap:wrap; justify-content:flex-end; }.message-content { max-width:88%; }.chat-compose .ui-button { min-width:58px; }.binding-numbers { grid-template-columns:1fr; }.manager-shell { grid-template-columns:1fr; gap:16px; }.workflow-admin-toolbar { align-items:flex-start; flex-direction:column; }.workflow-admin-toolbar>div:last-child { width:100%; }.workflow-admin-toolbar>div:last-child .ui-button { flex:1; }.workflow-admin-item { flex-direction:column; }.workflow-admin-actions { width:100%; }.workflow-admin-actions .ui-button { flex:1; } }
@media (max-width:520px) { .manifest-field { grid-template-columns:1fr auto; }.manifest-field p { grid-column:1 / -1; }.tool-whitelist>div { grid-template-columns:1fr; } }

.ai-workbench { grid-template-columns:minmax(0,1fr); }
.ai-chat-card { min-width:0; }
.ai-chat-card :deep(.n-card-header) { padding:14px 20px; border-bottom:1px solid var(--border); }
.chat-header { display:flex; align-items:center; justify-content:space-between; gap:20px; }
.ai-chat-card .chat-header>.chat-workflow { width:min(100%,620px); min-width:0; display:grid; grid-template-columns:150px minmax(220px,1fr); align-items:center; gap:16px; }
.chat-workflow-label { min-width:0; display:grid; gap:3px; }
.chat-workflow-label strong { color:var(--text-strong); font-size:14px; white-space:nowrap; }
.chat-workflow-label small { color:var(--text-muted); font-size:12px; line-height:1.4; }
.chat-workflow-select { width:100%; min-width:0; }
.chat-header>.chat-header-actions { flex:none; display:flex; align-items:center; gap:8px; }
.quick-prompts { align-items:center; gap:12px; padding:0 0 14px; }
.quick-prompts-label { flex:none; color:var(--text-muted); font-size:12px; font-weight:600; white-space:nowrap; }
.quick-prompts-list { min-width:0; display:flex; flex-wrap:wrap; gap:7px; }
.chat-workflow-error { flex:none; margin-bottom:12px; }
.chat-workflow-empty { flex:none; }
@media (max-width:900px) { .chat-header { align-items:stretch; flex-direction:column; gap:12px; }.ai-chat-card .chat-header>.chat-workflow { width:100%; }.chat-header>.chat-header-actions { justify-content:flex-end; } }
@media (max-width:640px) { .ai-chat-card .chat-header>.chat-workflow { grid-template-columns:minmax(0,1fr); gap:7px; }.chat-header>.chat-header-actions { justify-content:flex-start; }.quick-prompts { align-items:flex-start; flex-direction:column; gap:8px; }.quick-prompts-list { width:100%; }.quick-prompts-list button { flex:1 1 100%; } }

/* Keep workflow metadata and helper copy distinct from the light panels. */
.ai-runtime small,
.workflow-description small,
.provider-summary>span,
.collapse-title small,
.provider-profile-toolbar small,
.test-result small,
.workflow-admin-toolbar small,
.workflow-admin-main small,
.manifest-guide-title small,
.tool-whitelist small,
.typing,
.message-error small,
.message-meta,
.chat-notice,
.overview-item span,
.manager-entry small,
.runtime-status,
.workflow-description p,
.provider-summary small,
.plugin-description span,
.workflow-admin-main p,
.rule-draft-card small,
.overview-item small,
.manager-intro p,
.chat-message.is-canceled { color:var(--text); }
.knowledge-summary span,
.knowledge-summary small { color:var(--success-text); }
.typing i { background:var(--text-muted); }
.message-error .ui-button { min-height:24px; height:24px; padding:0 8px; }
.message-meta button { min-height:24px; padding:3px 8px; color:var(--primary); background:var(--surface-muted); border:1px solid var(--info-border); border-radius:.375rem; font-size:12px; cursor:pointer; }
.message-meta button:hover { background:var(--primary-soft); border-color:var(--primary); }
.message-meta button:focus-visible { outline:2px solid color-mix(in srgb,var(--primary) 35%,transparent); outline-offset:2px; }
.workflow-brief { margin:0 0 14px; padding:10px 12px; color:var(--text); background:var(--surface-muted); border-radius:.5rem; font-size:12px; line-height:1.6; }

/* Secondary groups use spacing and a quiet fill instead of stacked outlines. */
.ai-runtime { border:0; border-left:3px solid var(--primary); border-radius:var(--radius-lg); }
.workflow-description,.provider-summary,.knowledge-summary,.agent-preview-summary,.workflow-admin-panel,.manager-entry { border:0; border-radius:.625rem; }
.workflow-admin-item,.overview-item { border:1px solid transparent; border-radius:.625rem; }
.overview-item:first-child,.overview-item:last-child,.overview-item-static { border-color:transparent; }
.overview-item:hover,.overview-item:first-child:hover,.overview-item:last-child:hover,.overview-item-static:hover { border-color:transparent; }
.overview-item strong,.manager-entry strong,.workflow-admin-main strong,.knowledge-summary strong,.quick-prompts button,.message-meta button { font-size:13px; }
.agent-json-editor :deep(textarea),.agent-manifest-preview { font-size:13px; }
/* 深色用户消息与浅色悬停状态分别使用可读的前景色。 */
.message-row.user .message-avatar,.chat-message.user { color:var(--text-inverse); background:var(--primary); }
.chat-message.user.is-failed { color:var(--text); background:var(--surface-muted); }
.quick-prompts button:hover:not(:disabled) { color:var(--text); background:var(--primary-soft); }

/* 对话区参考 Claude：助手回答不加气泡、正文舒展；用户消息为暖灰气泡；输入框为圆角整体。 */
.chat-log { padding:20px 4px 8px; }
.message-row { gap:12px; }
.message-avatar { width:28px; height:28px; flex:0 0 28px; color:var(--text-inverse); background:var(--primary); border-radius:50%; font-size:11px; font-weight:var(--font-weight-semibold); }
.message-row.user .message-avatar { display:none; }
.message-content { margin-bottom:22px; }
.chat-message.assistant { padding:3px 0 0; color:var(--text-strong); background:transparent; border-radius:0; font-size:14px; line-height:1.8; }
.message-row.user .chat-message,.chat-message.user { padding:10px 16px; color:var(--text-strong); background:var(--surface-hover); border-radius:var(--radius-lg); font-size:14px; }
.chat-message.assistant.is-failed { padding:10px 14px; background:var(--danger-soft); border:1px solid var(--danger-border); border-radius:var(--radius-lg); }
.chat-message.user.is-failed { color:var(--text); background:var(--surface-muted); border:1px solid var(--danger-border); }
.quick-prompts button,.quick-prompts button:disabled { padding:6px 12px; color:var(--text-secondary); background:var(--surface); border:1px solid var(--border-strong); border-radius:var(--radius-full); }
.quick-prompts button:hover:not(:disabled) { color:var(--text-strong); background:var(--surface-hover); border-color:var(--border-hover); }
.message-meta button { color:var(--text-secondary); background:var(--surface); border-color:var(--border-strong); border-radius:var(--radius-full); }
.message-meta button:hover { color:var(--text-strong); background:var(--surface-hover); border-color:var(--border-hover); }
.chat-compose { align-items:flex-end; margin-top:4px; padding:10px 10px 10px 6px; background:var(--surface); border:1px solid var(--border-strong); border-radius:var(--radius-xl); box-shadow:var(--shadow-sm); transition:border-color .15s ease; }
.chat-compose:focus-within { border-color:var(--border-hover); }
.chat-compose :deep(.n-input) { background-color:transparent; box-shadow:none; }
.chat-compose :deep(.n-input .n-input__border),.chat-compose :deep(.n-input .n-input__state-border) { display:none; }
.chat-compose .ui-button { border-radius:var(--radius-md); }
</style>
