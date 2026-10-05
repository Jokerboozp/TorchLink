<script setup>
// 智能助手的历史对话：按当前账号、当前智能体与当前授权保存在服务端，保留期与 AI 日志一致。
import { onBeforeUnmount, ref, watch } from 'vue'
import { api, formatTime, isAbort, latest } from '../../api'
import { confirmDelete } from '../../deleteAction'

const props = defineProps({
  workflowId: { type: String, default: '' },
  activeId: { type: String, default: '' },
  disabled: { type: Boolean, default: false },
  // 递增时重新读取，例如一轮对话结束后。
  refreshKey: { type: Number, default: 0 }
})
const emit = defineEmits(['open', 'new', 'deleted'])
const items = ref([])
const loading = ref(false)
const error = ref('')
const request = latest()

async function load() {
  if (!props.workflowId) {
    items.value = []
    return
  }
  loading.value = true
  try {
    const result = await request.run(signal =>
      api(`/api/v1/ai/conversations?workflowId=${encodeURIComponent(props.workflowId)}`, { signal })
    )
    items.value = Array.isArray(result?.items) ? result.items : []
    error.value = ''
  } catch (e) {
    if (!isAbort(e)) error.value = e?.message || '历史对话读取失败'
  } finally {
    loading.value = false
  }
}

function remove(item) {
  return confirmDelete({
    label: item.title || '未命名对话',
    path: `/api/v1/ai/conversations/${encodeURIComponent(item.id)}`,
    warning: '删除后无法恢复。',
    onDeleted: async () => {
      emit('deleted', item.id)
      await load()
    }
  })
}

watch(() => [props.workflowId, props.refreshKey], load, { immediate: true })
onBeforeUnmount(() => request.cancel())
</script>

<template>
  <aside class="conversation-list" aria-label="历史对话">
    <div class="conversation-list__head">
      <strong>历史对话</strong>
      <ui-button size="small" type="primary" plain :disabled="disabled" @click="emit('new')">新对话</ui-button>
    </div>
    <ui-alert v-if="error" :title="error" type="error" :closable="false"
      ><ui-button size="small" plain @click="load">重新加载</ui-button></ui-alert
    >
    <ul v-else-if="items.length" class="conversation-list__items">
      <li v-for="item in items" :key="item.id" :class="{ active: item.id === activeId }">
        <button type="button" class="conversation-list__open" :disabled="disabled" :title="item.title" @click="emit('open', item.id)">
          <span>{{ item.title || '未命名对话' }}</span>
          <small>{{ formatTime(item.updatedAt) }} · {{ Math.ceil((item.messageCount || 0) / 2) }} 轮</small>
        </button>
        <ui-button size="small" text :disabled="disabled" aria-label="删除对话" @click="remove(item)">删除</ui-button>
      </li>
    </ul>
    <p v-else class="conversation-list__empty">{{ loading ? '正在读取…' : '暂无历史对话，提问后自动保存。' }}</p>
  </aside>
</template>

<style scoped>
.conversation-list {
  display: flex;
  flex-direction: column;
  gap: var(--space-2);
  min-height: 0;
  padding: var(--space-3);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
}
.conversation-list__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
}
.conversation-list__items {
  flex: 1 1 auto;
  min-height: 0;
  margin: 0;
  padding: 0;
  overflow: auto;
  list-style: none;
}
.conversation-list__items li {
  display: flex;
  padding-right: 6px;
  align-items: center;
  gap: var(--space-1);
  border-radius: var(--radius-md);
}
.conversation-list__items li.active,
.conversation-list__items li:hover {
  background: var(--surface-hover);
}
.conversation-list__open {
  flex: 1;
  min-width: 0;
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 2px;
  padding: 8px 10px;
  text-align: left;
  color: var(--text);
  background: none;
  border: 0;
  cursor: pointer;
}
.conversation-list__open span {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  font-size: var(--font-size-sm);
}
.conversation-list__open small,
.conversation-list__empty {
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}
.conversation-list__items li .ui-button {
  visibility: hidden;
}
.conversation-list__items li:hover .ui-button,
.conversation-list__items li.active .ui-button,
.conversation-list__items li:focus-within .ui-button {
  visibility: visible;
}
</style>
