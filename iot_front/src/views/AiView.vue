<script setup>
import { can } from '../permissions'
import { aiProviderOptions as providerOptions, capabilityName } from '../presentation'
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { api, apiAll, apiStream, session } from '../api'
import { copyText } from '../clipboard'
import { UiMessage } from '../ui/feedback.js'
import { useAIConversation } from '../aiConversation'
import { reconcileRuleDraftMessages, ruleDraftView } from '../ruleDraftStatus'
import HarnessTraceDrawer from '../components/HarnessTraceDrawer.vue'
import OpsReportDialog from '../components/OpsReportDialog.vue'
import AgentManager from '../components/AgentManager.vue'
import ChatPanel from '../components/ai/ChatPanel.vue'
import ConversationList from '../components/ai/ConversationList.vue'

const emit = defineEmits(['navigate'])

// 对话和运行状态保存在页面之外：切换到其他菜单时回答继续生成，返回后接着显示。
const conversation = useAIConversation(
  { tenant: session.tenant, user: session.user, accessVersion: session.accessVersion },
  { storage: localStorage, stream: apiStream }
)
const { messages, runs, conversationId, selectedWorkflowId, sending } = conversation
// 浏览器存储写满时只提示一次（对话已在服务端保存）。
watch(
  conversation.historyWarning,
  value => {
    if (value) UiMessage.warning(value)
  },
  { immediate: true }
)
// 每轮结束后刷新历史对话列表。
const conversationsVersion = ref(0)
watch(sending, value => {
  if (!value) conversationsVersion.value++
})
const chatPanel = ref()
const stopConversationUpdates = conversation.onUpdate(() => chatPanel.value?.scheduleScroll())

const selectedRunKey = ref('')
const traceVisible = ref(false)
const reportVisible = ref(false)
// 页面分为对话与智能体管理两部分；管理只对有权限的账号显示。
const view = ref('chat')

async function refreshRuleDraftStatuses() {
  if (!can('menu:rules')) return
  if (!messages.value.some(message => message?.ruleDraftPersisted === true && message?.ruleDraft?.id)) return
  try {
    const response = await apiAll('/api/v1/rules')
    reconcileRuleDraftMessages(messages.value, response?.items || [])
    conversation.persist()
  } catch {
    /* keep the last known card state when rule status cannot be loaded */
  }
}

const runtimeLoading = ref(false)
const runtimeError = ref('')
const workflowError = ref('')
let runtimeRequestSequence = 0
const runtime = ref({
  items: [],
  active: { id: 'disabled', name: '未启用', enabled: false },
  config: null,
  healthy: false,
  healthMessage: '正在读取模型服务状态'
})
const workflows = ref({ items: [], healthy: false, healthMessage: '正在读取工作流状态' })
const runConfig = reactive({ model: '' })
const quickQuestions = computed(() => {
  if (!selectedWorkflow.value) return []
  const name = workflowName(selectedWorkflow.value)
  const tools = selectedWorkflow.value.allowedTools || selectedWorkflow.value.tools || []
  const capabilities = selectedCapabilities.value.map(capabilityLabel)
  const prompts = [`请介绍「${name}」可以协助处理哪些任务？`]
  if (tools.some(tool => String(tool).includes('query_alarm_list')) || capabilities.some(item => item.includes('告警')))
    prompts.push('当前有哪些高等级活动告警？')
  if (tools.some(tool => String(tool).includes('query_device_latest')) || capabilities.some(item => item.includes('设备')))
    prompts.push('请概览当前设备状态和异常设备。')
  if (tools.some(tool => String(tool).includes('query_knowledge_base')) || capabilities.some(item => item.includes('知识')))
    prompts.push('查找与当前消防巡检相关的处置知识。')
  if (prompts.length === 1) prompts.push(`请按「${name}」的职责给出今天的工作建议。`)
  return prompts.slice(0, 3)
})

const nonChatWorkflowIds = new Set(['alarm-handler', 'device-health-inspector', 'protocol-assistant', 'rule-drafter'])
const workflowItems = computed(() => (workflows.value.items || []).filter(item => item.enabled !== false && isChatWorkflow(item)))
const workflowOptions = computed(() => workflowItems.value.map(item => ({ value: workflowKey(item), label: workflowName(item) })))
const selectedWorkflow = computed(() => workflowItems.value.find(item => workflowKey(item) === selectedWorkflowId.value))
const selectedRun = computed(() => runs.value.find(run => run.id === selectedRunKey.value) || null)
const activeHealthy = computed(() => Boolean(workflows.value.healthy))
const activeTone = computed(() => (!workflowItems.value.length ? 'info' : activeHealthy.value ? 'success' : 'danger'))
const healthMessage = computed(() => workflows.value.healthMessage || 'AI 服务状态未知')
const isAdmin = computed(() => can('GET /api/v1/ai/workflows/admin'))
const selectedCapabilities = computed(() => {
  const value = selectedWorkflow.value?.capabilities || selectedWorkflow.value?.tools || []
  return Array.isArray(value) ? value : []
})

function workflowKey(item) {
  return item?.id || item?.workflowId || ''
}
function workflowName(item) {
  return item?.name || item?.label || workflowKey(item) || '未命名智能体'
}
function capabilityLabel(item) {
  return capabilityName(typeof item === 'string' ? item : item?.name || item?.id)
}

function providerLabel(provider) {
  if (provider === 'disabled') return '未启用'
  return providerOptions.find(item => item.id === provider)?.label || provider || '未配置'
}

async function loadRuntime() {
  const requestSequence = ++runtimeRequestSequence
  runtimeLoading.value = true
  runtimeError.value = ''
  workflowError.value = ''
  try {
    const [providerResult, workflowResult] = await Promise.allSettled([apiAll('/api/v1/ai/providers'), apiAll('/api/v1/ai/workflows')])
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
        items: Array.isArray(workflowResult.value?.items) ? workflowResult.value.items : [],
        healthy: Boolean(workflowResult.value?.healthy),
        healthMessage: workflowResult.value?.healthMessage || ''
      }
      if (!workflowItems.value.some(item => workflowKey(item) === selectedWorkflowId.value))
        selectedWorkflowId.value = workflowKey(workflowItems.value[0])
      applyWorkflowDefaults()
    } else {
      workflowError.value = workflowResult.reason?.message || '工作流列表读取失败'
    }
  } finally {
    if (requestSequence === runtimeRequestSequence) runtimeLoading.value = false
  }
}

function isChatWorkflow(item) {
  return !nonChatWorkflowIds.has(workflowKey(item))
}

// 智能体增删改后刷新可选列表；新建的智能体直接选中。
async function agentsChanged(id) {
  await loadRuntime()
  if (id && workflowItems.value.some(item => workflowKey(item) === id)) selectedWorkflowId.value = id
}

// 会话切换由 conversation 完成；这里只重置轨迹面板，输入框由对话面板清空。
watch(
  selectedWorkflowId,
  () => {
    selectedRunKey.value = ''
    traceVisible.value = false
    applyWorkflowDefaults()
  },
  { flush: 'sync' }
)

function applyWorkflowDefaults() {
  const workflow = selectedWorkflow.value
  runConfig.model = runtime.value.config?.model || runtime.value.active?.model || workflow?.defaultModel || workflow?.model || ''
}

function send(textValue) {
  const text = String(textValue || '').trim()
  if (!text || sending.value) return
  chatPanel.value?.follow()
  return conversation.send(text, {
    workflowName: workflowName(selectedWorkflow.value),
    model: runConfig.model || selectedWorkflow.value?.defaultModel || selectedWorkflow.value?.model || ''
  })
}

function stop() {
  conversation.stop()
}
function retry(message) {
  if (!sending.value) send(message.prompt)
}
// 新对话不删除已保存的历史，之前的对话可在左侧列表中打开。
function newConversation() {
  conversation.clear()
  selectedRunKey.value = ''
  traceVisible.value = false
}
async function openConversation(id) {
  try {
    const result = await api(`/api/v1/ai/conversations/${encodeURIComponent(id)}`)
    conversation.openConversation(id, Array.isArray(result?.messages) ? result.messages : [])
    selectedRunKey.value = ''
    traceVisible.value = false
    chatPanel.value?.follow()
  } catch (error) {
    UiMessage.error(error?.message || '历史对话读取失败')
  }
}
function conversationDeleted(id) {
  if (id === conversationId.value) newConversation()
}
async function copyMessage(message) {
  if (await copyText(message.text)) UiMessage.success('回答已复制')
  else UiMessage.warning('浏览器不允许复制，请手动选择文本')
}
function openTrace(value) {
  selectedRunKey.value = value?.runKey || value?.id || ''
  traceVisible.value = true
}
function editRuleDraft(message) {
  if (ruleDraftView(message).disabled) return
  emit('navigate', 'rules', { ruleDraft: message.ruleDraft, persisted: Boolean(message.ruleDraftPersisted) })
}

onMounted(() => {
  return Promise.all([loadRuntime(), refreshRuleDraftStatuses()])
})
// 离开页面只停止滚动并保存记录，不中断正在生成的回答。
onBeforeUnmount(() => {
  stopConversationUpdates()
  conversation.persist()
})
</script>

<template>
  <div class="ai-runtime" v-loading="runtimeLoading">
    <div><strong>智能助手</strong><small>查询设备、告警和知识，查看每次回答的依据与执行过程。</small></div>
    <div class="runtime-actions">
      <div class="runtime-status">
        <ui-tag :type="activeTone" effect="light">{{ selectedWorkflow ? 'AI 服务' : '未配置' }}</ui-tag
        ><span>{{ providerLabel(runtime.config?.provider || runtime.active?.id) }} · {{ runConfig.model || '无活动模型' }}</span
        ><i :class="{ online: activeHealthy }" />{{ healthMessage }}
      </div>
      <ui-button v-permission="'POST /api/v1/ai/reports'" size="small" @click="reportVisible = true">运维报告</ui-button
      ><ui-radio-group v-if="isAdmin" v-model="view" size="small" class="segmented-choice-group" aria-label="页面内容"
        ><ui-radio-button value="chat">对话</ui-radio-button><ui-radio-button value="agents">智能体</ui-radio-button></ui-radio-group
      ><ui-button size="small" :loading="runtimeLoading" @click="loadRuntime">刷新状态</ui-button>
    </div>
  </div>
  <ui-alert v-if="runtimeError" class="runtime-warning" :title="runtimeError" type="warning" :closable="false" show-icon />

  <AgentManager v-if="view === 'agents'" @changed="agentsChanged" />
  <div v-else class="ai-workbench">
    <ConversationList
      :workflow-id="selectedWorkflowId"
      :active-id="conversationId"
      :disabled="sending"
      :refresh-key="conversationsVersion"
      @open="openConversation"
      @new="newConversation"
      @deleted="conversationDeleted"
    />
    <ChatPanel
      ref="chatPanel"
      v-model:workflow-id="selectedWorkflowId"
      :workflows="workflowOptions"
      :messages="messages"
      :sending="sending"
      :quick-questions="quickQuestions"
      :workflow-error="workflowError"
      :runtime-loading="runtimeLoading"
      :has-runs="runs.length > 0"
      @send="send"
      @stop="stop"
      @retry="retry"
      @copy="copyMessage"
      @trace="openTrace"
      @trace-latest="openTrace(runs[0])"
      @edit-rule-draft="editRuleDraft"
      @reload="loadRuntime"
    />
  </div>

  <HarnessTraceDrawer v-model="traceVisible" :run="selectedRun" />
  <OpsReportDialog v-model="reportVisible" />
</template>

<style scoped>
.ai-runtime {
  flex: none;
  min-height: 74px;
  margin-bottom: 16px;
  padding: 15px 18px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  background: var(--surface);
  border: 1px solid var(--border);
  border-left: 3px solid var(--primary);
  border-radius: 4px;
}
.ai-runtime > div:first-child {
  display: grid;
  gap: 3px;
}
.section-kicker {
  color: var(--primary-text);
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.14em;
}
.ai-runtime strong {
  font-size: 16px;
}
.ai-runtime small {
  color: var(--text-muted);
}
.runtime-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}
.runtime-status {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--text);
  font-size: 13px;
  white-space: nowrap;
}
.runtime-status i {
  width: 7px;
  height: 7px;
  background: var(--danger);
  border-radius: 50%;
}
.runtime-status i.online {
  background: var(--success);
} /* 状态说明使用可读的前景色变量。 */
.ai-workbench {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  display: grid;
  grid-template-rows: minmax(0, 1fr);
  grid-template-columns: minmax(280px, 320px) minmax(0, 1fr);
  gap: 16px;
  align-items: stretch;
}
.ai-workbench {
  grid-template-columns: minmax(250px, 286px) minmax(0, 1fr);
}
.runtime-warning {
  margin-top: 10px;
}
@media (max-width: 1120px) {
  .ai-runtime {
    gap: 10px;
    flex-wrap: wrap;
  }
  .runtime-status {
    white-space: normal;
    flex-wrap: wrap;
  }
}
@media (max-width: 640px) {
  .ai-runtime > div:first-child {
    display: none;
  }
  .ai-runtime {
    padding: 10px 12px;
    min-height: 0;
  }
  .runtime-status {
    font-size: 12px;
  }
  .ai-runtime {
    align-items: flex-start;
    flex-direction: column;
  }
  .runtime-actions,
  .runtime-status {
    width: 100%;
    white-space: normal;
    flex-wrap: wrap;
  }
  .runtime-actions {
    align-items: flex-start;
  }
  .runtime-actions .ui-button:first-of-type {
    margin-left: auto;
  }
}

.ai-workbench {
  grid-template-columns: minmax(200px, 250px) minmax(0, 1fr);
}
@media (max-width: 900px) {
  .ai-workbench {
    grid-template-columns: minmax(0, 1fr);
    grid-template-rows: max-content minmax(520px, 1fr);
    overflow: auto;
  }
  .ai-workbench > .conversation-list {
    max-height: 168px;
  }
}
.ai-runtime small,
.runtime-status {
  color: var(--text);
}
.ai-runtime {
  border: 0;
  border-left: 3px solid var(--primary);
  border-radius: var(--radius-lg);
}
</style>
