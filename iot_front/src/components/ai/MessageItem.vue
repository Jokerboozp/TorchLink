<script setup>
import { computed } from 'vue'
import { formatTime } from '../../api'
import { ruleDraftView } from '../../ruleDraftStatus'
import MarkdownContent from '../MarkdownContent.vue'
import ToolCallCard from '../ToolCallCard.vue'

// 对话中的一条消息：正文、工具调用、规则草稿、错误与运行信息。
const props = defineProps({
  message: { type: Object, required: true },
  // 最后一条已结束的回答才提供“重新生成”，失败的回答在错误区重试。
  last: { type: Boolean, default: false },
  sending: { type: Boolean, default: false }
})
const emit = defineEmits(['retry', 'copy', 'trace', 'edit-rule-draft'])

const draft = computed(() => ruleDraftView(props.message))
const canRegenerate = computed(
  () => props.last && props.message.role === 'assistant' && props.message.prompt && ['succeeded', 'canceled'].includes(props.message.status)
)
// 模型服务按输入 / 输出分别上报词元；界面显示两者之和。
const tokens = computed(() => {
  const usage = props.message.usage
  return usage && (usage.inputTokens != null || usage.outputTokens != null)
    ? Number(usage.inputTokens || 0) + Number(usage.outputTokens || 0)
    : null
})
function actionSummary(action) {
  return action?.type === 'OPEN_CAMERA'
    ? `打开摄像头 ${action.cameraId}`
    : action?.type === 'OPEN_PAGE'
      ? `打开页面 ${action.page}`
      : action?.type || '未知动作'
}
</script>

<template>
  <div class="message-row" :class="message.role">
    <span class="message-avatar">{{ message.role === 'assistant' ? '智能' : '我' }}</span>
    <div class="message-content">
      <div class="chat-message" :class="[message.role, `is-${message.status}`]">
        <MarkdownContent v-if="message.text && message.role === 'assistant' && message.status !== 'streaming'" :source="message.text" />
        <p v-else-if="message.text">{{ message.text }}</p>
        <div v-else-if="message.status === 'streaming'" class="typing"><i /><i /><i /><span>正在生成回答</span></div>
        <ToolCallCard v-for="tool in message.tools" :key="tool.id || tool.toolCallId" :tool="tool" />
        <div v-if="message.ruleDraft" class="rule-draft-card">
          <div>
            <strong>{{ message.ruleDraft.name || '自动化规则草稿' }}</strong
            ><ui-tag :type="draft.type" size="small">{{ draft.label }}</ui-tag>
          </div>
          <small
            >{{ message.ruleDraft.conditions?.length || 0 }} 个条件 ·
            {{ message.ruleDraft.actions?.map(actionSummary).join('、') || '仅告警' }}</small
          ><ui-button type="primary" size="small" :disabled="draft.disabled" @click="emit('edit-rule-draft', message)">{{
            draft.action
          }}</ui-button>
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
            @click="emit('retry', message)"
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
          ><span v-if="tokens != null">{{ tokens }} 词元</span></template
        >
        <template v-if="message.role === 'assistant'"
          ><ui-button v-if="message.text && message.status !== 'streaming'" plain size="small" @click="emit('copy', message)"
            >复制</ui-button
          ><ui-button
            v-if="canRegenerate"
            v-permission="'POST /api/v1/ai/chat/stream'"
            plain
            size="small"
            :disabled="sending"
            @click="emit('retry', message)"
            >重新生成</ui-button
          ><ui-button v-if="message.runKey" plain size="small" @click="emit('trace', message)">查看轨迹</ui-button></template
        >
      </div>
    </div>
  </div>
</template>

<style scoped>
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
@media (max-width: 767px) {
  .chat-message {
    max-width: 88%;
  }
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
@keyframes pulse {
  50% {
    opacity: 0.28;
    transform: translateY(-2px);
  }
}
@media (max-width: 640px) {
  .message-content {
    max-width: 88%;
  }
}
.typing,
.message-error small,
.message-meta,
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
.message-meta button {
  font-size: 13px;
}
.message-row.user .message-avatar,
.chat-message.user {
  color: var(--text-inverse);
  background: var(--primary);
}
.chat-message.user.is-failed {
  color: var(--text);
  background: var(--surface-muted);
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
</style>
