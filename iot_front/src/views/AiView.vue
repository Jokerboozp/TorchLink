<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, apiStream, session } from '../api'
import { AI_HISTORY_STORAGE_PREFIX, loadAIHistory, saveAIHistory } from '../aiHistory'
import { reconcileRuleDraftMessages } from '../ruleDraftStatus'
import HarnessTraceDrawer from '../components/HarnessTraceDrawer.vue'
import MarkdownContent from '../components/MarkdownContent.vue'
import ToolCallCard from '../components/ToolCallCard.vue'

const emit = defineEmits(['navigate'])
const controlsExpanded = ref(false)

let sequence = 0
let abortController = null
let scrollFrame = 0
let scrollQueued = false
let historyTimer = 0
const pendingTextStates = new Map()
const makeId = prefix => `${prefix}_${globalThis.crypto?.randomUUID?.() || `${Date.now()}_${++sequence}`}`
const welcomeMessage = () => ({ id:'welcome', role:'assistant', status:'succeeded', text:'你好，我是消防物联网 AI 运维助手。选择一个工作流后，可以直接查询设备、告警和趋势；所有工具执行都会显示在运行轨迹中。', tools:[] })

const messages = ref([welcomeMessage()])
const runs = ref([])
const question = ref('')
const sending = ref(false)
const log = ref()
const conversationId = ref('')
const selectedRunKey = ref('')
const traceVisible = ref(false)

function persistConversation() {
  saveAIHistory(localStorage, session, { conversationId:conversationId.value, selectedWorkflowId:selectedWorkflowId.value, messages:messages.value, runs:runs.value })
}
function scheduleConversationPersist() {
  if (historyTimer) clearTimeout(historyTimer)
  historyTimer = setTimeout(() => { historyTimer = 0; persistConversation() }, 150)
}
function restoreConversation() {
  const saved = loadAIHistory(localStorage, session)
  if (!saved) return
  messages.value = saved.messages.length ? saved.messages : [welcomeMessage()]
  runs.value = saved.runs
  conversationId.value = saved.conversationId
  selectedWorkflowId.value = saved.selectedWorkflowId
}

async function refreshRuleDraftStatuses() {
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
  defaultModel:'qwen3:1.7b', maxTokens:4096,
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
const providerOptions = [
  { id:'ollama', label:'本地模型（Ollama）', description:'使用 CentOS 或本机部署的 Ollama，不需要接口密钥。' },
  { id:'deepseek', label:'DeepSeek 云端模型', description:'使用 DeepSeek 云端模型和接口密钥。' },
  { id:'openai-compatible', label:'兼容接口模型', description:'连接其他兼容 Chat Completions 接口的模型服务。' }
]
const agentFieldDocs = [
  { name:'schemaVersion', type:'整数', note:'清单格式版本，当前固定填写 1。' },
  { name:'id', type:'字符串', note:'Agent 唯一标识，最长 128 字符；可使用字母、数字、点、下划线、冒号和连字符，不能覆盖内置 Agent。' },
  { name:'name', type:'字符串', note:'界面显示名称，必填，最长 128 字符。' },
  { name:'description', type:'字符串', note:'说明 Agent 的用途和适用场景，必填，最长 1024 字符。' },
  { name:'version', type:'字符串', note:'Agent 版本号，必填，最长 64 字符，建议使用 1.0.0 格式。' },
  { name:'enabled', type:'布尔值', note:'是否立即启用；填写 true 后创建完成即可被选择和运行。' },
  { name:'persona', type:'字符串', note:'系统提示词，定义角色、回答原则和工具调用规则，必填，最长 16384 字符。' },
  { name:'defaultModel', type:'字符串', note:'默认模型标识，必填；实际运行会跟随当前模型服务的活动模型。' },
  { name:'maxTokens', type:'整数', note:'单次最大输出 Token 数，平台允许 1–8192。' },
  { name:'capabilities', type:'字符串数组', note:'展示给用户的能力名称，填写 1–32 项，每项 1–64 字符且不可重复。' },
  { name:'allowedTools', type:'字符串数组', note:'Agent 可以调用的受控工具，至少 1 项、最多 6 项，只能从下方白名单选择且不可重复；规则工具只能保存禁用草稿。' }
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
const runConfig = reactive({ model:'', maxTokens:1200 })
const quickQuestions = ['当前有哪些高等级活动告警？', 'device_001 最近温度趋势如何？', '给出今日消防巡检重点']

const nonChatWorkflowIds = new Set(['alarm-handler', 'device-health-inspector', 'protocol-assistant'])
const workflowItems = computed(() => (workflows.value.items || []).filter(item => item.enabled !== false && isChatWorkflow(item)))
const selectedWorkflow = computed(() => workflowItems.value.find(item => workflowKey(item) === selectedWorkflowId.value))
const selectedRun = computed(() => runs.value.find(run => run.id === selectedRunKey.value) || null)
const activeHealthy = computed(() => Boolean(workflows.value.healthy))
const activeTone = computed(() => !workflowItems.value.length ? 'info' : activeHealthy.value ? 'success' : 'danger')
const healthMessage = computed(() => workflows.value.healthMessage || '工作流服务状态未知')
const isAdmin = computed(() => session.role === 'admin')
const selectedCapabilities = computed(() => {
  const value = selectedWorkflow.value?.capabilities || selectedWorkflow.value?.tools || []
  return Array.isArray(value) ? value : []
})
const agentPreviewJson = computed(() => agentPreview.value ? JSON.stringify(agentPreview.value, null, 2) : '')

function workflowKey(item) { return item?.id || item?.workflowId || '' }
function workflowName(item) { return item?.name || item?.label || workflowKey(item) || '未命名工作流' }
function capabilityLabel(item) { return typeof item === 'string' ? item : item?.name || item?.id || '工具' }
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

function normalizeError(value, fallback = 'AI 运行失败') {
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

const builtinWorkflowIds = new Set(['alarm-handler', 'ops-assistant', 'system-observer', 'device-health-inspector', 'protocol-assistant'])
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
  ElMessage.info('已打开新建 Agent，请填写 Manifest JSON')
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
  catch { ElMessage.error('Agent JSON 格式不正确'); return }
  if (editingAgentId.value && manifest?.id !== editingAgentId.value) {
    ElMessage.error('编辑时不能修改 Agent 的唯一标识；如需新插件请先新建')
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
    ElMessage.success(`${editing ? 'Agent 已更新' : 'Agent 已创建'}：${workflowName(saved)}`)
  } catch (error) { workflowManageError.value = error.message || (editing ? 'Agent 更新失败' : 'Agent 创建失败') }
  finally { creatingAgent.value = false }
}

async function toggleWorkflow(item) {
  if (!isAdmin.value || isBuiltinWorkflow(item) || creatingAgent.value) return
  const manifest = { ...item, enabled: item.enabled === false }
  creatingAgent.value = true
  try {
    await api(`/api/v1/ai/workflows/${encodeURIComponent(workflowKey(item))}`, { method:'PUT', body:JSON.stringify(manifest) })
    await Promise.all([loadRuntime(), loadWorkflowManagement()])
    ElMessage.success(`${workflowName(item)}已${manifest.enabled ? '启用' : '禁用'}`)
  } catch (error) { workflowManageError.value = error.message || '工作流状态更新失败' }
  finally { creatingAgent.value = false }
}

async function deleteWorkflow(item) {
  if (!isAdmin.value || isBuiltinWorkflow(item) || creatingAgent.value) return
  const deletedWorkflowId = workflowKey(item)
  try {
    await ElMessageBox.confirm(`删除后将无法运行“${workflowName(item)}”，确定继续吗？`, '删除工作流插件', { type:'warning', confirmButtonText:'确定删除', cancelButtonText:'取消' })
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
    ElMessage.success(`已删除工作流插件：${workflowName(item)}`)
  } catch (error) { workflowManageError.value = error.message || '工作流插件删除失败' }
  finally { creatingAgent.value = false }
}

watch(selectedWorkflowId, () => { applyWorkflowDefaults() })

function applyWorkflowDefaults() {
  const workflow = selectedWorkflow.value
  runConfig.model = runtime.value.config?.model || runtime.value.active?.model || workflow?.defaultModel || workflow?.model || ''
  if (Number.isSafeInteger(workflow?.maxTokens)) runConfig.maxTokens = Math.max(128, Math.min(8192, workflow.maxTokens))
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
      addRunEvent(run, event, 'Harness 开始运行', 'running', [run.provider,run.model].filter(Boolean).join(' / '))
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
      addRunEvent(run, event, 'Harness 运行完成', 'success', run.durationMs != null ? `${run.durationMs} ms` : '')
      break
    case 'run.failed': {
      flushAssistantText(assistant)
      const failure = normalizeError(event.error || event, 'AI 运行失败')
      assistant.status = 'failed'
      assistant.error = failure
      assistant.text ||= '运行未能完成。'
      run.status = 'failed'
      run.error = failure
      run.durationMs = event.durationMs ?? Math.max(0, Date.now() - run.startedAt)
      run.finishedAt = timestamp(event.failedAt || event.createdAt)
      for (const tool of run.tools.filter(item => item.status === 'running')) tool.status = 'failed'
      addRunEvent(run, event, 'Harness 运行失败', 'danger', failure.message)
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
  const maxTokens = Math.max(128, Math.min(8192, Number(runConfig.maxTokens) || 1200))
  const body = { question:text, conversationId:conversationId.value, workflowId:selectedWorkflowId.value, model:runConfig.model || selectedWorkflow.value?.defaultModel || selectedWorkflow.value?.model || '', maxTokens }

  try {
    await apiStream('/api/v1/ai/chat/stream', { method:'POST', body:JSON.stringify(body), signal:controller.signal }, event => applyStreamEvent(event, assistant, run))
    if (assistant.status === 'streaming') {
      const failure = normalizeError({ code:'AI_STREAM_INCOMPLETE', retryable:true }, 'AI 流意外结束，请重试。')
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
    <div><span class="section-kicker">AI 工作流</span><strong>AI 工作流</strong><small>聊天 Agent 与受控工具解耦；每次运行都有可审计的运行轨迹。</small></div>
    <div class="runtime-actions"><div class="runtime-status"><el-tag :type="activeTone" effect="light">{{ selectedWorkflow ? '工作流服务' : '未配置' }}</el-tag><span>{{ providerLabel(runtime.config?.provider || runtime.active?.id) }} · {{ runConfig.model || '无活动模型' }}</span><i :class="{ online:activeHealthy }" />{{ healthMessage }}</div><el-button class="controls-toggle" size="small" :aria-expanded="controlsExpanded" @click="controlsExpanded = !controlsExpanded">{{ controlsExpanded ? '返回对话' : '运行参数' }}</el-button><el-button size="small" @click="openAgentManagement">Agent 管理</el-button><el-button size="small" :loading="runtimeLoading" @click="loadRuntime">刷新状态</el-button></div>
  </div>

  <div class="ai-workbench" :class="{ 'is-controls-open': controlsExpanded }">
    <el-card shadow="never" class="surface-card control-card">
      <template #header><div class="card-header"><div><strong>本次运行</strong><small>选择工作流并设置必要参数</small></div><el-tag effect="plain">RUN</el-tag></div></template>
      <div class="control-scroll">
        <el-alert v-if="workflowError" :title="workflowError" type="error" :closable="false" show-icon><el-button plain size="small" @click="loadRuntime">重新加载</el-button></el-alert>
        <div class="control-section-label"><span>01</span>模型服务</div>
        <div class="provider-summary">
          <span>当前模型服务</span><strong>{{ providerLabel(runtime.config?.provider || runtime.active?.id) }}</strong><small>{{ runtime.config?.model || runtime.active?.model || '由服务端选择' }} · {{ runtime.config?.apiKeyConfigured ? '接口密钥已配置' : '无需接口密钥' }}</small>
          <el-button type="primary" plain @click="emit('navigate', 'aiProviders')">管理模型服务</el-button>
        </div>
        <div class="control-section-label"><span>02</span>选择工作流</div>
        <el-form label-position="top"><el-form-item label="工作流插件"><el-select v-model="selectedWorkflowId" placeholder="选择 AI 工作流" :disabled="sending || !workflowItems.length"><el-option v-for="item in workflowItems" :key="workflowKey(item)" :label="workflowName(item)" :value="workflowKey(item)" /></el-select></el-form-item></el-form>
        <el-empty v-if="!runtimeLoading && !workflowItems.length" description="暂无可用工作流" :image-size="62" />
        <div v-if="selectedWorkflow" class="workflow-description"><div><span class="workflow-icon">WF</span><div><strong>{{ workflowName(selectedWorkflow) }}</strong><small>{{ selectedWorkflow.version ? `v${selectedWorkflow.version}` : '服务端托管' }}</small></div></div><p>{{ selectedWorkflow.description || '该工作流会按服务端策略调用受控工具。' }}</p><div v-if="selectedCapabilities.length" class="capability-list"><el-tag v-for="item in selectedCapabilities" :key="capabilityLabel(item)" size="small" effect="plain">{{ capabilityLabel(item) }}</el-tag></div></div>
        <div class="control-section-label"><span>03</span>运行参数</div>
        <el-form class="run-config" label-position="top" :model="runConfig" :disabled="sending"><div><el-form-item label="运行模型"><el-input v-model="runConfig.model" readonly placeholder="使用活动模型" /></el-form-item><el-form-item label="最大输出"><el-input-number v-model="runConfig.maxTokens" :min="128" :max="8192" :step="128" controls-position="right" /></el-form-item></div></el-form>
        <div class="control-section-label"><span>04</span>运行环境</div>
        <div class="runtime-overview runtime-overview-single">
          <div class="overview-item overview-item-static"><span>当前模型服务</span><strong>{{ providerLabel(runtime.config?.provider || runtime.active?.id) }}</strong><small>{{ runtime.config?.model || runtime.active?.model || '由服务端选择' }} · {{ runtime.config?.baseUrl || '地址由服务端配置' }}</small></div>
        </div>
        <el-alert v-if="runtimeError" class="runtime-warning" :title="runtimeError" type="warning" :closable="false" show-icon />
      </div>
    </el-card>

    <el-card shadow="never" class="surface-card chat-card ai-chat-card">
      <template #header><div class="card-header chat-header"><div><strong>{{ workflowName(selectedWorkflow) }}</strong><small>{{ conversationId ? `会话 · ${conversationId}` : '新会话 · 服务端受控租户上下文' }}</small></div><div><el-button plain :disabled="!runs.length" @click="openTrace(runs[0])">运行轨迹</el-button><el-button plain type="warning" :disabled="sending" @click="clearConversation">清空对话</el-button></div></div></template>
      <div class="quick-prompts"><button v-for="item in quickQuestions" :key="item" :disabled="sending || !workflowItems.length" @click="send(item)">{{ item }}</button></div>
      <div ref="log" class="chat-log" aria-live="polite">
        <div v-for="message in messages" :key="message.id" class="message-row" :class="message.role">
          <span class="message-avatar">{{ message.role === 'assistant' ? 'AI' : '我' }}</span><div class="message-content"><div class="chat-message" :class="[message.role,`is-${message.status}`]"><MarkdownContent v-if="message.text && message.role === 'assistant' && message.status !== 'streaming'" :source="message.text" /><p v-else-if="message.text">{{ message.text }}</p><div v-else-if="message.status === 'streaming'" class="typing"><i/><i/><i/><span>正在运行工作流</span></div><ToolCallCard v-for="tool in message.tools" :key="tool.id || tool.toolCallId" :tool="tool" /><div v-if="message.ruleDraft" class="rule-draft-card"><div><strong>{{ message.ruleDraft.name || '自动化规则草稿' }}</strong><el-tag :type="ruleDraftStatusType(message)" size="small">{{ ruleDraftStatusLabel(message) }}</el-tag></div><small>{{ message.ruleDraft.conditions?.length || 0 }} 个条件 · {{ message.ruleDraft.actions?.map(actionSummary).join('、') || '仅告警' }}</small><el-button type="primary" size="small" :disabled="ruleDraftActionDisabled(message)" @click="editRuleDraft(message.ruleDraft, message.ruleDraftPersisted, message.ruleDraftState)">{{ ruleDraftActionLabel(message) }}</el-button></div><div v-if="message.error" class="message-error"><strong>{{ message.error.message }}</strong><small v-if="message.error.code || message.error.stage">{{ [message.error.code,message.error.stage].filter(Boolean).join(' · ') }}</small><small v-if="message.traceId || message.error.traceId">Trace · {{ message.traceId || message.error.traceId }}</small><el-button v-if="message.prompt" plain size="small" :disabled="sending" @click="retry(message)">重新运行</el-button></div></div><div v-if="message.role === 'assistant' && message.runKey" class="message-meta"><span v-if="message.status === 'streaming'">运行中</span><span v-else>{{ message.status === 'succeeded' ? '已完成' : message.status === 'canceled' ? '已停止' : '运行失败' }}</span><span v-if="message.durationMs != null">{{ message.durationMs }} ms</span><span v-if="message.usage?.totalTokens != null">{{ message.usage.totalTokens }} tokens</span><el-button plain size="small" @click="openTrace(message)">查看轨迹</el-button></div></div>
        </div>
      </div>
      <div class="chat-compose"><el-input v-model="question" type="textarea" :autosize="{ minRows:1,maxRows:4 }" maxlength="4000" resize="none" placeholder="询问设备、告警、趋势或处置知识；Shift + Enter 换行" :disabled="sending || !workflowItems.length" @keydown.enter.exact.prevent="send()" /><el-button v-if="sending" type="danger" plain @click="stop">停止</el-button><el-button v-else type="primary" :disabled="!question.trim() || !workflowItems.length" @click="send()">发送</el-button></div><small class="chat-notice">AI 输出仅供辅助判断，不会自动执行设备控制或启用规则。</small>
    </el-card>
  </div>

  <el-drawer v-model="managementVisible" title="Agent 管理" size="min(760px, 94vw)" class="workflow-manager" append-to-body>
    <section class="manager-panel panel-agent">
        <div class="manager-intro"><span>AGENT MANAGEMENT</span><div><h3>Agent 插件管理</h3><el-tag size="small" type="primary" effect="plain">ADMIN</el-tag></div><p>内置 Agent 仅可查看；动态 Agent 可编辑、启用/禁用或删除。点击“新建 Agent”即可在弹窗中提交新的 Manifest，保存后 Agent 会立即进入工作流列表。</p></div>
        <el-alert v-if="!isAdmin" title="工作流插件管理仅限管理员。" type="warning" :closable="false" show-icon />
        <div v-else class="workflow-admin-panel">
          <div class="workflow-admin-toolbar"><div><strong>已配置的工作流插件</strong><small>{{ workflowManageTotal }} 个聊天插件 · 内置聊天 Agent 只读；告警研判、设备巡检和协议接入由业务页面调用</small></div><div><el-button size="small" :loading="workflowManageLoading" @click="loadWorkflowManagement">刷新清单</el-button><el-button size="small" type="primary" plain @click="startCreateAgent">新建 Agent</el-button></div></div>
          <el-alert v-if="workflowManageError" :title="workflowManageError" type="error" :closable="false" show-icon />
          <el-skeleton v-if="workflowManageLoading && !workflowManageItems.length" :rows="4" animated />
          <el-empty v-else-if="!workflowManageItems.length" description="暂无工作流插件" :image-size="56" />
          <div v-else class="workflow-admin-list">
            <div v-for="item in workflowManageItems" :key="workflowKey(item)" class="workflow-admin-item">
              <div class="workflow-admin-main"><div><strong>{{ workflowName(item) }}</strong><el-tag size="small" :type="item.enabled === false ? 'info' : 'success'" effect="plain">{{ item.enabled === false ? '已禁用' : '已启用' }}</el-tag><el-tag v-if="isBuiltinWorkflow(item)" size="small" effect="plain">内置只读</el-tag></div><small>{{ workflowKey(item) }} · {{ item.version ? `v${item.version}` : '无版本' }}</small><p>{{ item.description || '未填写插件说明' }}</p></div>
              <div class="workflow-admin-actions"><template v-if="isBuiltinWorkflow(item)"><el-button size="small" type="primary" plain @click="viewAgent(item)">查看</el-button></template><template v-else><el-button size="small" @click="editAgent(item)">编辑</el-button><el-button size="small" @click="toggleWorkflow(item)">{{ item.enabled === false ? '启用' : '禁用' }}</el-button><el-button size="small" type="danger" plain @click="deleteWorkflow(item)">删除</el-button></template></div>
            </div>
          </div>
          <div v-if="workflowManageTotal" class="list-pagination">
            <el-pagination v-model:current-page="workflowManagePage" v-model:page-size="workflowManagePageSize" :total="workflowManageTotal" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next, jumper" @current-change="changeWorkflowManagePage" @size-change="changeWorkflowManagePageSize" />
          </div>
        </div>
      </section>
  </el-drawer>
  <el-dialog v-model="agentEditorVisible" :title="editingAgentId ? '编辑 Agent' : '新建 Agent'" width="min(820px, 94vw)" class="agent-editor-dialog" append-to-body destroy-on-close>
    <el-alert title="JSON 标准不支持注释。内置 Agent 仅可查看，动态 Agent 可在管理清单中编辑、启用或删除。" type="info" :closable="false" show-icon />
    <el-form class="drawer-form" label-position="top" :disabled="!isAdmin || creatingAgent">
      <el-form-item label="Agent Manifest JSON"><el-input ref="agentEditorRef" v-model="agentJson" class="agent-json-editor" type="textarea" :rows="18" resize="vertical" spellcheck="false" /></el-form-item>
      <div class="manifest-guide">
        <div class="manifest-guide-title"><div><strong>字段说明</strong><small>所有字段均为必填，请参考下方说明填写。</small></div><el-tag size="small" effect="plain">11 个字段</el-tag></div>
        <div class="manifest-field-list">
          <div v-for="field in agentFieldDocs" :key="field.name" class="manifest-field"><code>{{ field.name }}</code><span>{{ field.type }}</span><p>{{ field.note }}</p></div>
        </div>
        <div class="tool-whitelist"><strong>allowedTools 可用工具</strong><div><span v-for="tool in agentToolDocs" :key="tool.name"><code>{{ tool.name }}</code><small>{{ tool.label }}</small></span></div></div>
      </div>
      <el-alert title="只允许受控查询工具与“仅生成、不保存”的规则草稿工具；内置 Agent 不能覆盖，Agent 不能直接启用规则。" type="info" :closable="false" show-icon />
    </el-form>
    <template #footer><el-button @click="cancelAgentEditor">取消</el-button><el-button type="primary" :loading="creatingAgent" @click="saveAgent">{{ editingAgentId ? '校验并保存修改' : '校验并创建 Agent' }}</el-button></template>
  </el-dialog>
  <el-dialog v-model="agentPreviewVisible" title="查看内置 Agent Manifest（只读）" width="min(760px, 92vw)" append-to-body>
    <el-alert title="内置 Agent 仅供查看，不能编辑、启用/禁用或删除。" type="info" :closable="false" show-icon />
    <div v-if="agentPreview" class="agent-preview-summary"><div><strong>{{ workflowName(agentPreview) }}</strong><el-tag size="small" effect="plain">内置只读</el-tag></div><small>{{ workflowKey(agentPreview) }} · {{ agentPreview.version ? `v${agentPreview.version}` : '无版本' }} · {{ agentPreview.defaultModel || '未设置模型' }}</small><p>{{ agentPreview.description || '未填写插件说明' }}</p></div>
    <pre class="agent-manifest-preview">{{ agentPreviewJson }}</pre>
    <template #footer><el-button @click="agentPreviewVisible = false">关闭</el-button></template>
  </el-dialog>
  <HarnessTraceDrawer v-model="traceVisible" :run="selectedRun" />
</template>

<style scoped>
.provider-select-row { width:100%; min-width:0; }
.ai-runtime { flex:none; min-height:74px; margin-bottom:16px; padding:15px 18px; display:flex; align-items:center; justify-content:space-between; gap:20px; background:#fff; border:1px solid #e8e8e8; border-left:3px solid #1677ff; border-radius:4px; }.ai-runtime>div:first-child { display:grid; gap:3px; }.section-kicker { color:#1677ff; font-size:9px; font-weight:700; letter-spacing:.14em; }.ai-runtime strong { font-size:16px; }.ai-runtime small { color:var(--muted); }.runtime-actions { display:flex; align-items:center; gap:12px; }.runtime-status { display:flex; align-items:center; gap:8px; color:#646c73; font-size:12px; white-space:nowrap; }.runtime-status i { width:7px; height:7px; background:#ff4d4f; border-radius:50%; }.runtime-status i.online { background:#52c41a; }
.ai-workbench { flex:1; min-height:0; overflow:hidden; display:grid; grid-template-rows:minmax(0,1fr); grid-template-columns:minmax(280px,320px) minmax(0,1fr); gap:16px; align-items:stretch; }.control-card,.ai-chat-card { height:100%; min-height:0; display:flex; flex-direction:column; }.control-card :deep(.el-card__header),.ai-chat-card :deep(.el-card__header) { flex:none; }.control-card :deep(.el-card__body),.ai-chat-card :deep(.el-card__body) { flex:1; min-height:0; overflow:hidden; }.card-header>div { display:grid; gap:3px; }.card-header small { display:block; }.control-card :deep(.el-card__body) { display:flex; padding:0; }.control-scroll { flex:1; min-width:0; min-height:0; padding:16px; overflow:auto; overscroll-behavior:contain; }.control-scroll>.el-alert { margin-bottom:14px; }.workflow-description { margin:-3px 0 14px; padding:12px; background:#f5f9ff; border:1px solid #d6e8ff; border-radius:4px; }.workflow-description>div:first-child { display:flex; align-items:center; gap:9px; }.workflow-description>div:first-child>div { display:grid; gap:2px; }.workflow-description strong { color:#1554ad; font-size:12px; }.workflow-description small { color:#8c8c8c; font-size:10px; }.workflow-description p { margin:9px 0; color:#646c73; font-size:11px; line-height:1.6; }.workflow-icon { width:30px; height:30px; display:grid; place-items:center; color:#fff; background:#1677ff; border-radius:4px; font-size:9px; font-weight:800; }.capability-list { display:flex; flex-wrap:wrap; gap:5px; }.run-config { margin-bottom:2px; }.run-config>div { display:grid; grid-template-columns:minmax(0,1fr) 112px; gap:9px; }.run-config :deep(.el-input-number) { width:100%; }.provider-summary { margin:0 0 14px; padding:11px; display:grid; gap:7px; background:#fafafa; border:1px solid #ededed; border-radius:4px; }.provider-summary>span { color:#8c8c8c; font-size:10px; }.provider-summary strong { font-size:12px; }.provider-summary small { color:#646c73; }.sandbox-collapse { border-top:1px solid #ededed; }.collapse-title { width:100%; padding-right:8px; display:flex; align-items:center; justify-content:space-between; }.collapse-title>div { display:grid; gap:2px; }.collapse-title strong { font-size:12px; }.collapse-title small { color:#8c8c8c; font-size:10px; }.plugin-description { margin:-4px 0 15px; display:grid; gap:4px; }.plugin-description strong { color:#1554ad; font-size:11px; }.plugin-description span { color:#646c73; font-size:10px; line-height:1.5; }.provider-select-row { display:grid; grid-template-columns:minmax(0,1fr) auto; gap:7px; }.provider-select-row :deep(.el-select) { width:100%; }.provider-profile-toolbar { margin:-5px 0 12px; display:flex; align-items:center; justify-content:space-between; gap:8px; }.provider-profile-toolbar small { color:#86909c; font-size:10px; line-height:1.5; }.provider-profile-toolbar>div { display:flex; gap:5px; flex:none; }.provider-profile-editor { margin:0 0 13px; padding:11px; background:#f9f0ff; border:1px solid #d3adf7; border-radius:5px; }.provider-profile-editor :deep(.el-form-item) { margin-bottom:10px; }.provider-profile-actions { display:flex; justify-content:flex-end; gap:7px; margin-top:10px; }.admin-notice { margin-bottom:14px; }.test-button { width:100%; margin-top:12px; }.test-result { margin-top:14px; padding:11px; border:1px solid; border-radius:4px; }.test-result.success { background:#f6ffed; border-color:#b7eb8f; }.test-result.failed { background:#fff2f0; border-color:#ffccc7; }.test-result>div { display:flex; justify-content:space-between; align-items:center; }.test-result p { margin:8px 0; color:#3d3d3d; font-size:11px; line-height:1.6; white-space:pre-wrap; }.test-result small { color:#8c8c8c; word-break:break-all; }
.knowledge-summary { margin:0 0 14px; padding:11px; display:grid; gap:5px; background:#f6ffed; border:1px solid #d9f7be; border-radius:4px; }.knowledge-summary>div { display:flex; align-items:center; justify-content:space-between; }.knowledge-summary span,.knowledge-summary small { color:#5b6b59; font-size:10px; }.knowledge-summary strong { color:#237804; font-size:11px; }.binding-numbers { display:grid; grid-template-columns:1fr 1fr; gap:9px; }.binding-numbers :deep(.el-input-number),.sandbox-collapse :deep(.el-select),.sandbox-collapse :deep(.el-radio-group) { width:100%; }.sandbox-collapse :deep(.el-radio-button) { flex:1; }.sandbox-collapse :deep(.el-radio-button__inner) { width:100%; padding-left:7px; padding-right:7px; }
.agent-json-editor :deep(textarea) { font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace; font-size:10px; line-height:1.55; }.agent-actions { margin-top:12px; display:flex; justify-content:flex-end; gap:8px; }
.agent-preview-summary { margin-top:14px; padding:12px; display:grid; gap:5px; background:#f6faff; border:1px solid #d6e8ff; border-radius:5px; }.agent-preview-summary>div { display:flex; align-items:center; gap:7px; }.agent-preview-summary strong { color:#1f2329; font-size:13px; }.agent-preview-summary small { color:#697386; font-size:10px; }.agent-preview-summary p { margin:0; color:#4e5969; font-size:11px; line-height:1.6; }.agent-manifest-preview { max-height:min(58vh,560px); margin:12px 0 0; padding:14px; overflow:auto; color:#1f2329; background:#fbfcfe; border:1px solid #d6e4ff; border-radius:5px; font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace; font-size:11px; line-height:1.65; white-space:pre-wrap; word-break:break-word; }
.workflow-admin-panel { margin-bottom:18px; padding:12px; background:#fafcff; border:1px solid #d6e4ff; border-radius:6px; }.workflow-admin-toolbar { display:flex; align-items:center; justify-content:space-between; gap:12px; margin-bottom:10px; }.workflow-admin-toolbar>div:first-child { min-width:0; display:grid; gap:3px; }.workflow-admin-toolbar strong { color:#1f2329; font-size:12px; }.workflow-admin-toolbar small { color:#86909c; font-size:10px; }.workflow-admin-toolbar>div:last-child { display:flex; gap:6px; flex:none; }.workflow-admin-list { display:grid; gap:7px; }.workflow-admin-item { padding:10px; display:flex; align-items:flex-start; justify-content:space-between; gap:12px; background:#fff; border:1px solid #edf0f5; border-radius:5px; }.workflow-admin-main { min-width:0; display:grid; gap:4px; }.workflow-admin-main>div { min-width:0; display:flex; align-items:center; flex-wrap:wrap; gap:5px; }.workflow-admin-main strong { max-width:260px; overflow:hidden; color:#303133; font-size:11px; text-overflow:ellipsis; white-space:nowrap; }.workflow-admin-main small { overflow:hidden; color:#86909c; font-size:9px; text-overflow:ellipsis; white-space:nowrap; }.workflow-admin-main p { margin:2px 0 0; overflow:hidden; color:#646c73; font-size:10px; line-height:1.5; text-overflow:ellipsis; white-space:nowrap; }.workflow-admin-actions { display:flex; flex:none; align-items:center; gap:5px; }.workflow-admin-actions .el-button { margin-left:0; }
.manifest-guide { margin:-2px 0 16px; overflow:hidden; background:#fbfcfe; border:1px solid #d6e4ff; border-radius:6px; }.manifest-guide-title { padding:12px 14px; display:flex; align-items:center; justify-content:space-between; gap:12px; background:#f0f5ff; border-bottom:1px solid #d6e4ff; }.manifest-guide-title>div { display:grid; gap:3px; }.manifest-guide-title strong { color:#10239e; font-size:12px; }.manifest-guide-title small { color:#697386; font-size:10px; }.manifest-field-list { display:grid; }.manifest-field { padding:10px 14px; display:grid; grid-template-columns:124px 74px minmax(0,1fr); align-items:start; gap:10px; border-bottom:1px solid #edf0f5; }.manifest-field:last-child { border-bottom:0; }.manifest-field code,.tool-whitelist code { color:#0958d9; font-family:ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace; font-size:10px; font-weight:700; word-break:break-all; }.manifest-field>span { width:max-content; padding:2px 6px; color:#595959; background:#f0f0f0; border-radius:3px; font-size:9px; }.manifest-field p { margin:0; color:#4e5969; font-size:10px; line-height:1.55; }.tool-whitelist { padding:13px 14px; display:grid; gap:10px; background:#fff; border-top:1px solid #d6e4ff; }.tool-whitelist>strong { color:#303133; font-size:11px; }.tool-whitelist>div { display:grid; grid-template-columns:1fr 1fr; gap:7px; }.tool-whitelist span { min-width:0; padding:8px 9px; display:grid; gap:3px; background:#f7f9fc; border-radius:4px; }.tool-whitelist small { color:#697386; font-size:9px; }
.ai-chat-card :deep(.el-card__body) { display:flex; flex-direction:column; }.chat-header>div:last-child { display:flex; align-items:center; }.quick-prompts { flex:none; display:flex; flex-wrap:wrap; gap:7px; padding-bottom:12px; border-bottom:1px solid #f0f0f0; }.quick-prompts button { padding:6px 9px; color:#1554ad; background:#f5f9ff; border:1px solid #d6e8ff; border-radius:3px; font-size:11px; cursor:pointer; }.quick-prompts button:hover:not(:disabled) { color:#fff; background:#1677ff; border-color:#1677ff; }.quick-prompts button:disabled { opacity:.5; cursor:not-allowed; }.chat-log { min-height:0; flex:1; padding:15px 3px 6px; overflow:auto; overscroll-behavior:contain; }.message-row { display:flex; align-items:flex-start; gap:9px; }.message-row.user { flex-direction:row-reverse; }.message-avatar { width:28px; height:28px; flex:0 0 28px; display:grid; place-items:center; color:#fff; background:#3f4960; border-radius:4px; font-size:10px; font-weight:700; }.message-row.user .message-avatar { background:#1677ff; }.message-content { width:100%; min-width:0; max-width:min(82%,760px); margin-bottom:14px; }.message-row.user .message-content { width:fit-content; max-width:min(82%,760px); display:flex; flex-direction:column; align-items:flex-end; }.chat-message { width:100%; max-width:none; box-sizing:border-box; margin:0; padding:10px 12px; color:#3d3d3d; background:#f5f5f5; border-radius:4px; font-size:12px; line-height:1.75; word-break:break-word; }.message-row.user .chat-message { width:fit-content; max-width:100%; }.chat-message.user { color:#fff; background:#1677ff; }.chat-message.is-failed { background:#fff7f6; border:1px solid #ffccc7; }.chat-message.is-canceled { color:#646c73; background:#fafafa; border:1px dashed #d9d9d9; }.chat-message p { margin:0; white-space:pre-wrap; }.typing { min-width:150px; display:flex; align-items:center; gap:5px; color:#8c8c8c; }.typing i { width:5px; height:5px; background:#8c8c8c; border-radius:50%; animation:pulse 1s infinite; }.typing i:nth-child(2){animation-delay:.16s}.typing i:nth-child(3){animation-delay:.32s}.typing span { margin-left:4px; font-size:10px; }.message-error { margin-top:9px; padding-top:9px; display:grid; gap:3px; border-top:1px solid #ffccc7; }.message-error strong { color:#cf1322; font-size:11px; }.message-error small { color:#8c8c8c; font-size:9px; word-break:break-all; }.message-error .el-button { width:max-content; height:auto; margin-top:3px; padding:0; }.message-meta { margin-top:5px; display:flex; align-items:center; gap:8px; color:#8c8c8c; font-size:9px; }.message-meta button { padding:0; color:#1677ff; background:none; border:0; font-size:9px; cursor:pointer; }.chat-compose { flex:none; display:flex; align-items:flex-end; gap:9px; padding-top:11px; border-top:1px solid #f0f0f0; }.chat-compose .el-button { min-width:72px; }.chat-notice { margin-top:8px; color:#8c8c8c; text-align:center; }
.rule-draft-card { margin-top:10px; padding:10px; display:grid; gap:7px; background:#fffbe6; border:1px solid #ffe58f; border-radius:4px; }.rule-draft-card>div { display:flex; align-items:center; justify-content:space-between; gap:8px; }.rule-draft-card small { color:#646c73; }.rule-draft-card .el-button { width:max-content; }
.ai-workbench { grid-template-columns:minmax(250px,286px) minmax(0,1fr); }
.control-section-label { margin:2px 0 10px; display:flex; align-items:center; gap:7px; color:#303133; font-size:11px; font-weight:700; letter-spacing:.02em; }.control-section-label span { width:22px; height:18px; display:grid; place-items:center; color:#1677ff; background:#eaf3ff; border-radius:3px; font-size:9px; }
.runtime-overview { display:grid; grid-template-columns:1fr 1fr; gap:8px; }.overview-item { min-width:0; padding:10px; display:grid; gap:3px; color:inherit; text-align:left; background:#fafafa; border:1px solid #ededed; border-radius:4px; cursor:pointer; transition:border-color .15s,background .15s; }.overview-item:hover { background:#f5f9ff; border-color:#91caff; }.overview-item span { color:#8c8c8c; font-size:9px; }.overview-item strong { overflow:hidden; color:#303133; font-size:11px; text-overflow:ellipsis; white-space:nowrap; }.overview-item small { overflow:hidden; color:#646c73; font-size:9px; text-overflow:ellipsis; white-space:nowrap; }.runtime-warning { margin-top:10px; }.manager-entry { width:100%; margin-top:12px; padding:11px 12px; display:flex; align-items:center; justify-content:space-between; color:inherit; text-align:left; background:#fff; border:1px solid #d9d9d9; border-radius:4px; cursor:pointer; }.manager-entry:hover { background:#f5f9ff; border-color:#1677ff; }.manager-entry>span { display:grid; gap:2px; }.manager-entry strong { font-size:11px; }.manager-entry small { color:#8c8c8c; font-size:9px; }.manager-entry b { color:#1677ff; font-size:10px; }
.runtime-overview-single { grid-template-columns:1fr; }.overview-item-static { cursor:default; }.overview-item-static:hover { background:#fafafa; border-color:#ededed; }
.overview-item:first-child { background:#f9f0ff; border-color:#efdbff; }.overview-item:first-child:hover { border-color:#b37feb; }.overview-item:first-child strong { color:#531dab; }.overview-item:last-child { background:#f6ffed; border-color:#d9f7be; }.overview-item:last-child:hover { border-color:#95de64; }.overview-item:last-child strong { color:#237804; }
.overview-item-static { background:#fafafa; border-color:#ededed; }.overview-item-static:hover { background:#fafafa; border-color:#ededed; }.overview-item-static strong { color:#303133; }
.manager-panel { animation:manager-in .16s ease-out; }.manager-intro { margin-bottom:20px; padding:16px; background:#f5f9ff; border:1px solid #d6e8ff; border-left:4px solid #1677ff; border-radius:6px; }.manager-intro>span { color:#1677ff; font-size:9px; font-weight:700; letter-spacing:.12em; }.manager-intro>div { margin-top:5px; display:flex; align-items:center; justify-content:space-between; gap:12px; }.manager-intro h3 { margin:0; color:#1f2329; font-size:16px; }.manager-intro p { margin:7px 0 0; color:#646c73; font-size:11px; line-height:1.6; }.drawer-form :deep(.el-select),.drawer-form :deep(.el-radio-group) { width:100%; }.drawer-form :deep(.el-radio-button) { flex:1; }.drawer-form :deep(.el-radio-button__inner) { width:100%; }.workflow-manager :deep(.el-drawer__header) { margin-bottom:0; padding-bottom:16px; border-bottom:1px solid #ededed; }.workflow-manager :deep(.el-drawer__body) { padding-top:16px; }.agent-editor-dialog :deep(.el-dialog__body) { max-height:calc(100vh - 210px); overflow-y:auto; overscroll-behavior:contain; }.agent-editor-dialog .drawer-form { margin-top:14px; }
@keyframes manager-in { from { opacity:0; transform:translateX(6px); } }
@keyframes pulse { 50% { opacity:.28; transform:translateY(-2px); } }
.controls-toggle { display:none; } @media (max-width:1120px) {.ai-runtime { gap:10px; flex-wrap:wrap; }.runtime-status { white-space:normal; flex-wrap:wrap; }.quick-prompts { flex-wrap:nowrap; overflow-x:auto; }.quick-prompts button { flex:none; } }
@media (max-width:760px) { .controls-toggle { display:inline-flex; }.ai-workbench { grid-template-columns:1fr; }.control-card { display:none; }.ai-workbench.is-controls-open .ai-chat-card { display:none; }.ai-workbench.is-controls-open .control-card { display:flex; } }
@media (max-width:640px) { .ai-runtime>div:first-child { display:none; }.ai-runtime { padding:10px 12px; min-height:0; }.runtime-status { font-size:10px; }.chat-header small { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; max-width:220px; }.ai-runtime { align-items:flex-start; flex-direction:column; }.runtime-actions,.runtime-status { width:100%; white-space:normal; flex-wrap:wrap; }.runtime-actions { align-items:flex-start; }.runtime-actions .el-button:first-of-type { margin-left:auto; }.runtime-overview { grid-template-columns:1fr; }.chat-header { align-items:flex-start; gap:8px; }.chat-header>div:last-child { flex-wrap:wrap; justify-content:flex-end; }.message-content { max-width:88%; }.chat-compose .el-button { min-width:58px; }.binding-numbers { grid-template-columns:1fr; }.manager-shell { grid-template-columns:1fr; gap:16px; }.manager-menu { display:grid; grid-template-columns:repeat(3,minmax(0,1fr)); }.manager-menu :deep(.el-menu-item) { height:54px; margin:0 3px 0 0; padding:0 7px !important; justify-content:center; }.manager-menu-copy small { display:none; }.manager-menu-icon { display:none; }.workflow-admin-toolbar { align-items:flex-start; flex-direction:column; }.workflow-admin-toolbar>div:last-child { width:100%; }.workflow-admin-toolbar>div:last-child .el-button { flex:1; }.workflow-admin-item { flex-direction:column; }.workflow-admin-actions { width:100%; }.workflow-admin-actions .el-button { flex:1; } }
@media (max-width:520px) { .manifest-field { grid-template-columns:1fr auto; }.manifest-field p { grid-column:1 / -1; }.tool-whitelist>div { grid-template-columns:1fr; } }

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
.manager-menu-copy small { color:var(--muted-foreground); }
.runtime-status,
.workflow-description p,
.provider-summary small,
.plugin-description span,
.workflow-admin-main p,
.rule-draft-card small,
.overview-item small,
.manager-intro p,
.chat-message.is-canceled { color:#475569; }
.knowledge-summary span,
.knowledge-summary small { color:#3f6212; }
.typing i { background:var(--muted-foreground); }
.message-error .el-button { min-height:24px; height:24px; padding:0 8px; }
.message-meta button { min-height:24px; padding:3px 8px; color:#1d4ed8; background:#eff6ff; border:1px solid #bfdbfe; border-radius:.375rem; font-size:9px; cursor:pointer; }
.message-meta button:hover { background:#dbeafe; border-color:#93c5fd; }
.message-meta button:focus-visible { outline:2px solid rgba(59,130,246,.35); outline-offset:2px; }
</style>
