<script setup>
import { can } from '../permissions'
import { aiProviderOptions as providerOptions, capabilityName } from '../presentation'
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { api, apiStream, formatTime, session } from '../api'
import { copyText } from '../ops/opsApi'
import { UiMessage } from '../ui/feedback.js'
import { useAIConversation } from '../aiConversation'
import { reconcileRuleDraftMessages } from '../ruleDraftStatus'
import HarnessTraceDrawer from '../components/HarnessTraceDrawer.vue'
import OpsReportDialog from '../components/OpsReportDialog.vue'
import AgentManager from '../components/AgentManager.vue'
import MarkdownContent from '../components/MarkdownContent.vue'
import ToolCallCard from '../components/ToolCallCard.vue'
import ConversationList from '../components/ai/ConversationList.vue'

const emit = defineEmits(['navigate'])

let scrollFrame = 0
let scrollQueued = false
// 用户向上翻看历史时不再自动滚到底部；回到底部或发送新问题后恢复跟随。
const followOutput = ref(true)

// 对话和运行状态保存在页面之外：切换到其他菜单时回答继续生成，返回后接着显示。
const conversation = useAIConversation(
  { tenant: session.tenant, user: session.user, accessVersion: session.accessVersion },
  { storage: localStorage, stream: apiStream }
)
const { messages, runs, conversationId, selectedWorkflowId, sending } = conversation
// 每轮结束后刷新历史对话列表。
const conversationsVersion = ref(0)
watch(sending, value => {
  if (!value) conversationsVersion.value++
})
const stopConversationUpdates = conversation.onUpdate(scheduleScroll)

const question = ref('')
const log = ref()
const selectedRunKey = ref('')
const traceVisible = ref(false)
const reportVisible = ref(false)
// 页面分为对话与智能体管理两部分；管理只对有权限的账号显示。
const view = ref('chat')

async function refreshRuleDraftStatuses() {
  if (!can('menu:rules')) return
  if (!messages.value.some(message => message?.ruleDraftPersisted === true && message?.ruleDraft?.id)) return
  try {
    const response = await api('/api/v1/rules?page=1&pageSize=100')
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

function scheduleScroll() {
  if (scrollQueued) return
  scrollQueued = true
  nextTick(() => {
    if (!scrollQueued) return
    scrollFrame = requestAnimationFrame(() => {
      if (log.value && followOutput.value) log.value.scrollTop = log.value.scrollHeight
      scrollFrame = 0
      scrollQueued = false
    })
  })
}

function trackScroll() {
  const el = log.value
  if (el) followOutput.value = el.scrollHeight - el.scrollTop - el.clientHeight < 48
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

// 会话切换由 conversation 完成；这里只重置当前页面的输入和轨迹面板。
watch(
  selectedWorkflowId,
  () => {
    question.value = ''
    selectedRunKey.value = ''
    traceVisible.value = false
    nextTick(scheduleScroll)
    applyWorkflowDefaults()
  },
  { flush: 'sync' }
)

function applyWorkflowDefaults() {
  const workflow = selectedWorkflow.value
  runConfig.model = runtime.value.config?.model || runtime.value.active?.model || workflow?.defaultModel || workflow?.model || ''
}

function send(textValue) {
  const text = (textValue || question.value).trim()
  if (!text || sending.value) return
  question.value = ''
  followOutput.value = true
  return conversation.send(text, {
    workflowName: workflowName(selectedWorkflow.value),
    model: runConfig.model || selectedWorkflow.value?.defaultModel || selectedWorkflow.value?.model || ''
  })
}

function stop() {
  conversation.stop()
}
// 模型服务按输入 / 输出分别上报词元；界面显示两者之和。
function usageTokens(usage) {
  return usage && (usage.inputTokens != null || usage.outputTokens != null)
    ? Number(usage.inputTokens || 0) + Number(usage.outputTokens || 0)
    : null
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
    followOutput.value = true
    scheduleScroll()
  } catch (error) {
    UiMessage.error(error?.message || '历史对话读取失败')
  }
}
function conversationDeleted(id) {
  if (id === conversationId.value) newConversation()
}
function backToBottom() {
  followOutput.value = true
  scheduleScroll()
}
async function copyMessage(message) {
  if (await copyText(message.text)) UiMessage.success('回答已复制')
  else UiMessage.warning('浏览器不允许复制，请手动选择文本')
}
// 只为最后一条已结束的回答提供“重新生成”，失败的回答在错误区重试。
function canRegenerate(message) {
  const last = messages.value[messages.value.length - 1]
  return message === last && message.role === 'assistant' && message.prompt && ['succeeded', 'canceled'].includes(message.status)
}
function openTrace(value) {
  selectedRunKey.value = value?.runKey || value?.id || ''
  traceVisible.value = true
}
function actionSummary(action) {
  return action?.type === 'OPEN_CAMERA'
    ? `打开摄像头 ${action.cameraId}`
    : action?.type === 'OPEN_PAGE'
      ? `打开页面 ${action.page}`
      : action?.type || '未知动作'
}
function ruleDraftStatusLabel(message) {
  return message.ruleDraftState === 'enabled'
    ? '已启用'
    : message.ruleDraftState === 'missing'
      ? '已删除'
      : message.ruleDraftPersisted
        ? '已保存草稿'
        : '待人工确认'
}
function ruleDraftStatusType(message) {
  return message.ruleDraftState === 'enabled' ? 'success' : message.ruleDraftState === 'missing' ? 'info' : 'warning'
}
function ruleDraftActionLabel(message) {
  return message.ruleDraftState === 'enabled'
    ? '规则已启用'
    : message.ruleDraftState === 'missing'
      ? '规则已删除'
      : message.ruleDraftPersisted
        ? '查看并启用规则'
        : '检查并保存规则'
}
function ruleDraftActionDisabled(message) {
  return message.ruleDraftState === 'enabled' || message.ruleDraftState === 'missing'
}
function editRuleDraft(draft, persisted, state) {
  if (state === 'enabled' || state === 'missing') return
  emit('navigate', 'rules', { ruleDraft: draft, persisted: Boolean(persisted) })
}

onMounted(() => {
  scheduleScroll()
  return Promise.all([loadRuntime(), refreshRuleDraftStatuses()])
})
// 离开页面只停止滚动并保存记录，不中断正在生成的回答。
onBeforeUnmount(() => {
  stopConversationUpdates()
  if (scrollFrame) cancelAnimationFrame(scrollFrame)
  scrollFrame = 0
  scrollQueued = false
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
    <ui-card shadow="never" class="surface-card chat-card ai-chat-card">
      <template #header>
        <div class="card-header chat-header">
          <div class="chat-workflow">
            <div class="chat-workflow-label"><strong>智能体</strong><small>对话自动保存，可在历史列表中找回</small></div>
            <ui-select
              v-model="selectedWorkflowId"
              class="chat-workflow-select"
              aria-label="智能体"
              placeholder="选择智能体"
              :disabled="sending || !workflowItems.length"
              ><ui-option v-for="item in workflowItems" :key="workflowKey(item)" :label="workflowName(item)" :value="workflowKey(item)"
            /></ui-select>
          </div>
          <div class="chat-header-actions">
            <ui-button plain size="small" :disabled="!runs.length" @click="openTrace(runs[0])">运行轨迹</ui-button>
          </div>
        </div>
      </template>
      <ui-alert v-if="workflowError" class="chat-workflow-error" :title="workflowError" type="error" :closable="false" show-icon
        ><ui-button plain size="small" @click="loadRuntime">重新加载</ui-button></ui-alert
      >
      <ui-alert
        v-if="!runtimeLoading && !workflowItems.length && !workflowError"
        class="chat-workflow-empty"
        title="暂无可用智能体：需由管理员部署并配置 AI 工作流服务（Harness）后才能提问。"
        type="info"
        :closable="false"
        show-icon
      />
      <div v-if="quickQuestions.length" class="quick-prompts">
        <span class="quick-prompts-label">快捷提问</span>
        <div class="quick-prompts-list">
          <button v-for="item in quickQuestions" :key="item" :disabled="sending || !workflowItems.length" @click="send(item)">
            {{ item }}
          </button>
        </div>
      </div>
      <div ref="log" class="chat-log" aria-live="polite" @scroll.passive="trackScroll">
        <div v-for="message in messages" :key="message.id" class="message-row" :class="message.role">
          <span class="message-avatar">{{ message.role === 'assistant' ? '智能' : '我' }}</span>
          <div class="message-content">
            <div class="chat-message" :class="[message.role, `is-${message.status}`]">
              <MarkdownContent
                v-if="message.text && message.role === 'assistant' && message.status !== 'streaming'"
                :source="message.text"
              />
              <p v-else-if="message.text">{{ message.text }}</p>
              <div v-else-if="message.status === 'streaming'" class="typing"><i /><i /><i /><span>正在生成回答</span></div>
              <ToolCallCard v-for="tool in message.tools" :key="tool.id || tool.toolCallId" :tool="tool" />
              <div v-if="message.ruleDraft" class="rule-draft-card">
                <div>
                  <strong>{{ message.ruleDraft.name || '自动化规则草稿' }}</strong
                  ><ui-tag :type="ruleDraftStatusType(message)" size="small">{{ ruleDraftStatusLabel(message) }}</ui-tag>
                </div>
                <small
                  >{{ message.ruleDraft.conditions?.length || 0 }} 个条件 ·
                  {{ message.ruleDraft.actions?.map(actionSummary).join('、') || '仅告警' }}</small
                ><ui-button
                  type="primary"
                  size="small"
                  :disabled="ruleDraftActionDisabled(message)"
                  @click="editRuleDraft(message.ruleDraft, message.ruleDraftPersisted, message.ruleDraftState)"
                  >{{ ruleDraftActionLabel(message) }}</ui-button
                >
              </div>
              <div v-if="message.error" class="message-error">
                <strong>{{ message.error.message }}</strong
                ><small v-if="message.error.code || message.error.stage">{{
                  [message.error.code, message.error.stage].filter(Boolean).join(' · ')
                }}</small
                ><small v-if="message.traceId || message.error.traceId">追踪编号 · {{ message.traceId || message.error.traceId }}</small
                ><ui-button
                  v-permission="'POST /api/v1/ai/chat/stream'"
                  v-if="message.prompt"
                  plain
                  size="small"
                  :disabled="sending"
                  @click="retry(message)"
                  >重新运行</ui-button
                >
              </div>
            </div>
            <div v-if="message.createdAt || message.runKey" class="message-meta">
              <time v-if="message.createdAt">{{ formatTime(message.createdAt) }}</time>
              <template v-if="message.role === 'assistant' && message.runKey"
                ><span v-if="message.status === 'streaming'">运行中</span
                ><span v-else>{{ message.status === 'succeeded' ? '已完成' : message.status === 'canceled' ? '已停止' : '运行失败' }}</span
                ><span v-if="message.durationMs != null">{{ message.durationMs }} 毫秒</span
                ><span v-if="usageTokens(message.usage) != null">{{ usageTokens(message.usage) }} 词元</span></template
              >
              <template v-if="message.role === 'assistant'"
                ><ui-button v-if="message.text && message.status !== 'streaming'" plain size="small" @click="copyMessage(message)"
                  >复制</ui-button
                ><ui-button
                  v-if="canRegenerate(message)"
                  v-permission="'POST /api/v1/ai/chat/stream'"
                  plain
                  size="small"
                  :disabled="sending"
                  @click="retry(message)"
                  >重新生成</ui-button
                ><ui-button v-if="message.runKey" plain size="small" @click="openTrace(message)">查看轨迹</ui-button></template
              >
            </div>
          </div>
        </div>
      </div>
      <ui-button v-if="!followOutput" class="chat-back-bottom" size="small" @click="backToBottom">回到底部</ui-button>
      <div class="chat-compose">
        <ui-input
          v-model="question"
          type="textarea"
          :autosize="{ minRows: 1, maxRows: 4 }"
          maxlength="4000"
          resize="none"
          placeholder="询问设备、告警、趋势或处置知识；Enter 发送，Shift+Enter 换行"
          :disabled="!workflowItems.length"
          @keydown.enter.exact.prevent="send()"
        /><ui-button v-if="sending" type="danger" plain @click="stop">停止</ui-button
        ><ui-button
          v-permission="'POST /api/v1/ai/chat/stream'"
          v-else
          type="primary"
          :disabled="!question.trim() || !workflowItems.length"
          @click="send()"
          >发送</ui-button
        >
      </div>
      <small class="chat-notice">智能输出仅供辅助判断，不会自动执行设备控制或启用规则。</small>
    </ui-card>
  </div>

  <HarnessTraceDrawer v-model="traceVisible" :run="selectedRun" />
  <OpsReportDialog v-model="reportVisible" />
</template>

<style scoped>
.chat-card {
  height: 100%;
  min-height: 520px;
}
.chat-log {
  flex: 1;
  padding: 10px var(--space-2);
  overflow: auto;
}
.chat-message {
  max-width: min(76%, 720px);
  margin: 9px 0;
  padding: 11px 14px;
  line-height: 1.7;
  white-space: pre-wrap;
  border-radius: 14px;
}
.chat-message.assistant {
  background: var(--surface-hover);
  border-bottom-left-radius: var(--radius-sm);
}
.chat-message.user {
  margin-left: auto;
  color: var(--text-inverse);
  background: var(--primary);
  border-bottom-right-radius: var(--radius-sm);
}
.chat-compose {
  display: flex;
  gap: 9px;
  padding-top: 14px;
  border-top: 1px solid var(--border);
}
.chat-compose .ui-input {
  flex: 1;
}
@media (max-width: 767px) {
  .chat-card {
    min-height: 440px;
  }
  .chat-message {
    max-width: 88%;
  }
}
.ai-chat-card :deep(.n-card-content) {
  flex: 1 1 auto;
  min-height: 0;
  overflow: hidden;
} /* Naive UI 卡片正文承接内部滚动区域。 */
.ai-chat-card :deep(.n-card-content) {
  display: flex;
  flex-direction: column;
} /* 对话记录可在固定高度卡片内独立滚动。 */
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
.ai-chat-card {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.ai-chat-card :deep(.n-card-header) {
  flex: none;
}
.ai-chat-card :deep(.n-card-content) {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}
.card-header > div {
  display: grid;
  gap: 3px;
}
.card-header small {
  display: block;
}
.ai-chat-card :deep(.n-card-content) {
  display: flex;
  flex-direction: column;
}
.chat-header > div:last-child {
  display: flex;
  align-items: center;
}
.quick-prompts {
  flex: none;
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border);
}
.quick-prompts button {
  padding: 6px 9px;
  color: var(--primary-text);
  background: var(--surface-muted);
  border: 1px solid var(--info-border);
  border-radius: 3px;
  font-size: 12px;
  cursor: pointer;
}
.quick-prompts button:hover:not(:disabled) {
  color: var(--surface);
  background: var(--primary-soft);
  border-color: var(--primary);
}
.quick-prompts button:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.chat-log {
  min-height: 0;
  flex: 1;
  padding: 15px 3px 6px;
  overflow: auto;
  overscroll-behavior: contain;
}
.message-row {
  display: flex;
  align-items: flex-start;
  gap: 9px;
}
.message-row.user {
  flex-direction: row-reverse;
}
.message-avatar {
  width: 28px;
  height: 28px;
  flex: 0 0 28px;
  display: grid;
  place-items: center;
  color: var(--surface);
  background: var(--text);
  border-radius: 4px;
  font-size: 12px;
  font-weight: 700;
}
.message-row.user .message-avatar {
  background: var(--primary-soft);
}
.message-content {
  width: 100%;
  min-width: 0;
  max-width: min(82%, 760px);
  margin-bottom: 14px;
}
.message-row.user .message-content {
  width: fit-content;
  max-width: min(82%, 760px);
  display: flex;
  flex-direction: column;
  align-items: flex-end;
}
.chat-message {
  width: 100%;
  max-width: none;
  box-sizing: border-box;
  margin: 0;
  padding: 10px 12px;
  color: var(--text);
  background: var(--surface-muted);
  border-radius: 4px;
  font-size: 13px;
  line-height: 1.75;
  word-break: break-word;
}
.message-row.user .chat-message {
  width: fit-content;
  max-width: 100%;
}
.chat-message.user {
  color: var(--surface);
  background: var(--primary-soft);
}
.chat-message.is-failed {
  background: var(--surface-muted);
  border: 1px solid var(--danger-border);
}
.chat-message.is-canceled {
  color: var(--text);
  background: var(--surface);
  border: 1px dashed var(--border-strong);
}
.chat-message p {
  margin: 0;
  white-space: pre-wrap;
}
.typing {
  min-width: 150px;
  display: flex;
  align-items: center;
  gap: 5px;
  color: var(--text-muted);
}
.typing i {
  width: 5px;
  height: 5px;
  background: var(--text-muted);
  border-radius: 50%;
  animation: pulse 1s infinite;
}
.typing i:nth-child(2) {
  animation-delay: 0.16s;
}
.typing i:nth-child(3) {
  animation-delay: 0.32s;
}
.typing span {
  margin-left: 4px;
  font-size: 12px;
}
.message-error {
  margin-top: 9px;
  padding-top: 9px;
  display: grid;
  gap: 3px;
  border-top: 1px solid var(--danger-border);
}
.message-error strong {
  color: var(--danger);
  font-size: 12px;
}
.message-error small {
  color: var(--text-muted);
  font-size: 12px;
  word-break: break-all;
}
.message-error .ui-button {
  width: max-content;
  height: auto;
  margin-top: 3px;
  padding: 0;
}
.message-meta {
  margin-top: 5px;
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--text-muted);
  font-size: 12px;
}
.message-meta button {
  padding: 0;
  color: var(--primary-text);
  background: none;
  border: 0;
  font-size: 12px;
  cursor: pointer;
}
.chat-compose {
  flex: none;
  display: flex;
  align-items: flex-end;
  gap: 9px;
  padding-top: 11px;
  border-top: 1px solid var(--border);
}
.chat-compose .ui-button {
  min-width: 72px;
}
.chat-notice {
  margin-top: 8px;
  color: var(--text-muted);
  text-align: center;
}
.rule-draft-card {
  margin-top: 10px;
  padding: 10px;
  display: grid;
  gap: 7px;
  background: var(--surface-muted);
  border: 1px solid var(--warning);
  border-radius: 4px;
}
.rule-draft-card > div {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.rule-draft-card small {
  color: var(--text);
}
.rule-draft-card .ui-button {
  width: max-content;
}
.ai-workbench {
  grid-template-columns: minmax(250px, 286px) minmax(0, 1fr);
}
.runtime-warning {
  margin-top: 10px;
}
@keyframes pulse {
  50% {
    opacity: 0.28;
    transform: translateY(-2px);
  }
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
  .quick-prompts {
    flex-wrap: nowrap;
    overflow-x: auto;
  }
  .quick-prompts button {
    flex: none;
  }
}
@media (max-width: 640px) {
  .quick-prompts {
    flex-wrap: wrap;
    overflow-x: visible;
  }
  .quick-prompts button {
    flex: 1 1 100%;
    min-width: 0;
    white-space: normal;
    text-align: left;
    overflow-wrap: anywhere;
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
  .chat-header small {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 220px;
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
  .chat-header {
    align-items: flex-start;
    gap: 8px;
  }
  .chat-header > div:last-child {
    flex-wrap: wrap;
    justify-content: flex-end;
  }
  .message-content {
    max-width: 88%;
  }
  .chat-compose .ui-button {
    min-width: 58px;
  }
}

.ai-workbench {
  grid-template-columns: minmax(200px, 250px) minmax(0, 1fr);
}
.ai-chat-card :deep(.n-card-content) {
  position: relative;
}
.chat-back-bottom {
  position: absolute;
  right: 24px;
  bottom: 96px;
  z-index: 1;
  box-shadow: var(--shadow-sm);
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
.ai-chat-card {
  min-width: 0;
}
.ai-chat-card :deep(.n-card-header) {
  padding: 14px 20px;
  border-bottom: 1px solid var(--border);
}
.chat-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
}
.ai-chat-card .chat-header > .chat-workflow {
  width: min(100%, 620px);
  min-width: 0;
  display: grid;
  grid-template-columns: 150px minmax(220px, 1fr);
  align-items: center;
  gap: 16px;
}
.chat-workflow-label {
  min-width: 0;
  display: grid;
  gap: 3px;
}
.chat-workflow-label strong {
  color: var(--text-strong);
  font-size: 14px;
  white-space: nowrap;
}
.chat-workflow-label small {
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.4;
}
.chat-workflow-select {
  width: 100%;
  min-width: 0;
}
.chat-header > .chat-header-actions {
  flex: none;
  display: flex;
  align-items: center;
  gap: 8px;
}
.quick-prompts {
  align-items: center;
  gap: 12px;
  padding: 0 0 14px;
}
.quick-prompts-label {
  flex: none;
  color: var(--text-muted);
  font-size: 12px;
  font-weight: 600;
  white-space: nowrap;
}
.quick-prompts-list {
  min-width: 0;
  display: flex;
  flex-wrap: wrap;
  gap: 7px;
}
.chat-workflow-error {
  flex: none;
  margin-bottom: 12px;
}
.chat-workflow-empty {
  flex: none;
}
@media (max-width: 900px) {
  .chat-header {
    align-items: stretch;
    flex-direction: column;
    gap: 12px;
  }
  .ai-chat-card .chat-header > .chat-workflow {
    width: 100%;
  }
  .chat-header > .chat-header-actions {
    justify-content: flex-end;
  }
}
@media (max-width: 640px) {
  .ai-chat-card .chat-header > .chat-workflow {
    grid-template-columns: minmax(0, 1fr);
    gap: 7px;
  }
  .chat-header > .chat-header-actions {
    justify-content: flex-start;
  }
  .quick-prompts {
    align-items: flex-start;
    flex-direction: column;
    gap: 8px;
  }
  .quick-prompts-list {
    width: 100%;
  }
  .quick-prompts-list button {
    flex: 1 1 100%;
  }
}
.ai-runtime small,
.typing,
.message-error small,
.message-meta,
.chat-notice,
.runtime-status,
.rule-draft-card small,
.chat-message.is-canceled {
  color: var(--text);
}
.typing i {
  background: var(--text-muted);
}
.message-error .ui-button {
  min-height: 24px;
  height: 24px;
  padding: 0 8px;
}
.message-meta button {
  min-height: 24px;
  padding: 3px 8px;
  color: var(--primary-text);
  background: var(--surface-muted);
  border: 1px solid var(--info-border);
  border-radius: 0.375rem;
  font-size: 12px;
  cursor: pointer;
}
.message-meta button:hover {
  background: var(--primary-soft);
  border-color: var(--primary);
}
.message-meta button:focus-visible {
  outline: 2px solid color-mix(in srgb, var(--primary) 35%, transparent);
  outline-offset: 2px;
}
.ai-runtime {
  border: 0;
  border-left: 3px solid var(--primary);
  border-radius: var(--radius-lg);
}
.quick-prompts button,
.message-meta button {
  font-size: 13px;
}
/* 深色用户消息与浅色悬停状态分别使用可读的前景色。 */
.message-row.user .message-avatar,
.chat-message.user {
  color: var(--text-inverse);
  background: var(--primary);
}
.chat-message.user.is-failed {
  color: var(--text);
  background: var(--surface-muted);
}
.quick-prompts button:hover:not(:disabled) {
  color: var(--text);
  background: var(--primary-soft);
}
.chat-log {
  padding: 20px 4px 8px;
}
.message-row {
  gap: 12px;
}
.message-avatar {
  width: 28px;
  height: 28px;
  flex: 0 0 28px;
  color: var(--text-inverse);
  background: var(--primary);
  border-radius: 50%;
  font-size: 11px;
  font-weight: var(--font-weight-semibold);
}
.message-row.user .message-avatar {
  display: none;
}
.message-content {
  margin-bottom: 22px;
}
.chat-message.assistant {
  padding: 3px 0 0;
  color: var(--text-strong);
  background: transparent;
  border-radius: 0;
  font-size: 14px;
  line-height: 1.8;
}
.message-row.user .chat-message,
.chat-message.user {
  padding: 10px 16px;
  color: var(--text-strong);
  background: var(--surface-hover);
  border-radius: var(--radius-lg);
  font-size: 14px;
}
.chat-message.assistant.is-failed {
  padding: 10px 14px;
  background: var(--danger-soft);
  border: 1px solid var(--danger-border);
  border-radius: var(--radius-lg);
}
.chat-message.user.is-failed {
  color: var(--text);
  background: var(--surface-muted);
  border: 1px solid var(--danger-border);
}
.quick-prompts button,
.quick-prompts button:disabled {
  padding: 6px 12px;
  color: var(--text-secondary);
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-full);
}
.quick-prompts button:hover:not(:disabled) {
  color: var(--text-strong);
  background: var(--surface-hover);
  border-color: var(--border-hover);
}
.message-meta button {
  color: var(--text-secondary);
  background: var(--surface);
  border-color: var(--border-strong);
  border-radius: var(--radius-full);
}
.message-meta button:hover {
  color: var(--text-strong);
  background: var(--surface-hover);
  border-color: var(--border-hover);
}
.chat-compose {
  align-items: flex-end;
  margin-top: 4px;
  padding: 10px 10px 10px 6px;
  background: var(--surface);
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-sm);
  transition: border-color 0.15s ease;
}
.chat-compose:focus-within {
  border-color: var(--border-hover);
}
.chat-compose :deep(.n-input) {
  background-color: transparent;
  box-shadow: none;
}
.chat-compose :deep(.n-input .n-input__border),
.chat-compose :deep(.n-input .n-input__state-border) {
  display: none;
}
.chat-compose .ui-button {
  border-radius: var(--radius-md);
}
</style>
