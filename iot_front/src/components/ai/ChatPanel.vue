<script setup>
// 智能助手对话面板：智能体选择、快捷提问、消息列表（自动跟随与回到底部）和输入框。
// 对话状态由页面持有，面板只负责展示与交互，通过事件把操作交给页面。
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import MessageItem from './MessageItem.vue'

const props = defineProps({
  workflowId: { type: String, default: '' },
  // 可选智能体：{ value, label }
  workflows: { type: Array, default: () => [] },
  messages: { type: Array, default: () => [] },
  sending: { type: Boolean, default: false },
  quickQuestions: { type: Array, default: () => [] },
  workflowError: { type: String, default: '' },
  runtimeLoading: { type: Boolean, default: false },
  hasRuns: { type: Boolean, default: false }
})
const emit = defineEmits(['update:workflowId', 'send', 'stop', 'retry', 'copy', 'trace', 'trace-latest', 'edit-rule-draft', 'reload'])

const question = ref('')
const log = ref()
// 用户向上翻看历史时不再自动滚到底部；回到底部或发送新问题后恢复跟随。
const followOutput = ref(true)
let scrollFrame = 0
let scrollQueued = false

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
// 发送新问题或打开历史对话后回到底部并恢复跟随。
function follow() {
  followOutput.value = true
  scheduleScroll()
}
function submit(textValue) {
  const text = (textValue || question.value).trim()
  if (!text || props.sending) return
  question.value = ''
  emit('send', text)
}

// 切换智能体时清空未发送的输入。
watch(
  () => props.workflowId,
  () => {
    question.value = ''
    nextTick(scheduleScroll)
  }
)
onMounted(scheduleScroll)
onBeforeUnmount(() => {
  if (scrollFrame) cancelAnimationFrame(scrollFrame)
  scrollFrame = 0
  scrollQueued = false
})
defineExpose({ follow, scheduleScroll })
</script>

<template>
  <ui-card shadow="never" class="surface-card chat-card ai-chat-card">
    <template #header>
      <div class="card-header chat-header">
        <div class="chat-workflow">
          <div class="chat-workflow-label"><strong>智能体</strong><small>对话自动保存到历史</small></div>
          <ui-select
            :model-value="workflowId"
            class="chat-workflow-select"
            aria-label="智能体"
            placeholder="选择智能体"
            :disabled="sending || !workflows.length"
            @update:model-value="value => emit('update:workflowId', value)"
            ><ui-option v-for="item in workflows" :key="item.value" :label="item.label" :value="item.value"
          /></ui-select>
        </div>
        <div class="chat-header-actions">
          <ui-button plain size="small" :disabled="!hasRuns" @click="emit('trace-latest')">运行轨迹</ui-button>
        </div>
      </div>
    </template>
    <ui-alert v-if="workflowError" class="chat-workflow-error" :title="workflowError" type="error" :closable="false" show-icon
      ><ui-button plain size="small" @click="emit('reload')">重新加载</ui-button></ui-alert
    >
    <ui-alert
      v-if="!runtimeLoading && !workflows.length && !workflowError"
      class="chat-workflow-empty"
      title="暂无可用智能体：需由管理员部署并配置 AI 工作流服务（Harness）后才能提问。"
      type="info"
      :closable="false"
      show-icon
    />
    <div v-if="quickQuestions.length" class="quick-prompts">
      <span class="quick-prompts-label">快捷提问</span>
      <div class="quick-prompts-list">
        <button v-for="item in quickQuestions" :key="item" :disabled="sending || !workflows.length" @click="submit(item)">
          {{ item }}
        </button>
      </div>
    </div>
    <div ref="log" class="chat-log" aria-live="polite" @scroll.passive="trackScroll">
      <MessageItem
        v-for="(message, index) in messages"
        :key="message.id"
        :message="message"
        :last="index === messages.length - 1"
        :sending="sending"
        @retry="message => emit('retry', message)"
        @copy="message => emit('copy', message)"
        @trace="value => emit('trace', value)"
        @edit-rule-draft="message => emit('edit-rule-draft', message)"
      />
    </div>
    <ui-button v-if="!followOutput" class="chat-back-bottom" size="small" @click="follow">回到底部</ui-button>
    <div class="chat-compose">
      <ui-input
        v-model="question"
        type="textarea"
        :autosize="{ minRows: 1, maxRows: 4 }"
        maxlength="4000"
        resize="none"
        placeholder="询问设备、告警、趋势或处置知识；Enter 发送，Shift+Enter 换行"
        :disabled="!workflows.length"
        @keydown.enter.exact.prevent="submit()"
      /><ui-button v-if="sending" type="danger" plain @click="emit('stop')">停止</ui-button
      ><ui-button
        v-permission="'POST /api/v1/ai/chat/stream'"
        v-else
        type="primary"
        :disabled="!question.trim() || !workflows.length"
        @click="submit()"
        >发送</ui-button
      >
    </div>
    <small class="chat-notice">智能输出仅供辅助判断，不会自动执行设备控制或启用规则。</small>
  </ui-card>
</template>

<style scoped>
.chat-card {
  height: 100%;
  min-height: 520px;
}

.chat-compose .ui-input {
  flex: 1;
}
@media (max-width: 767px) {
  .chat-card {
    min-height: 440px;
  }
}
/* Naive UI 卡片正文承接内部滚动区域。 */
/* 对话记录可在固定高度卡片内独立滚动。 */

.card-header > div {
  display: grid;
  gap: 3px;
}
.card-header small {
  display: block;
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

.chat-compose .ui-button {
  min-width: 72px;
}

@media (max-width: 1120px) {
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
  .chat-header small {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 220px;
  }
  .chat-header {
    align-items: flex-start;
    gap: 8px;
  }
  .chat-header > div:last-child {
    flex-wrap: wrap;
    justify-content: flex-end;
  }
  .chat-compose .ui-button {
    min-width: 58px;
  }
}
.ai-chat-card :deep(.n-card-content) {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  position: relative;
}
.chat-back-bottom {
  position: absolute;
  right: 24px;
  bottom: 96px;
  z-index: 1;
  box-shadow: var(--shadow-sm);
}
.ai-chat-card {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  min-width: 0;
}
.ai-chat-card :deep(.n-card-header) {
  flex: none;
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
.chat-notice {
  margin-top: 8px;
  color: var(--text);
  text-align: center;
}
.quick-prompts button {
  font-size: 13px;
}
/* 深色用户消息与浅色悬停状态分别使用可读的前景色。 */

.quick-prompts button:hover:not(:disabled) {
  color: var(--text);
  background: var(--primary-soft);
}
.chat-log {
  flex: 1;
  padding: 20px 4px 8px;
  overflow: auto;
  min-height: 0;
  overscroll-behavior: contain;
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
.chat-compose {
  display: flex;
  gap: 9px;
  padding-top: 11px;
  border-top: 1px solid var(--border);
  flex: none;
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
