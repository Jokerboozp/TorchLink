<script setup>
import { computed } from 'vue'
import { statusLabel } from '../presentation'

const props = defineProps({ document: { type: Object, required: true } })
const failed = computed(() => props.document.status === 'INDEX_FAILED')
const deleting = computed(() => props.document.status === 'DELETING')
const active = computed(() => ['PENDING', 'UPLOADED', 'INDEXING'].includes(props.document.status))
const done = computed(() => Math.max(0, Number(props.document.metadata?.indexProgress?.done || 0)))
const total = computed(() => Math.max(0, Number(props.document.metadata?.indexProgress?.total || 0)))
const percentage = computed(() => (total.value > 0 ? Math.min(100, Math.floor((done.value / total.value) * 100)) : null))
const retryAt = computed(() => (props.document.status === 'UPLOADED' ? Number(props.document.metadata?.indexRetryAt || 0) : 0))
const label = computed(() =>
  retryAt.value
    ? '等待自动重试'
    : ['PENDING', 'UPLOADED'].includes(props.document.status)
      ? '等待建立索引'
      : statusLabel(props.document.status)
)
const retryLabel = computed(() =>
  retryAt.value
    ? `向量服务暂时不可用，将于 ${new Date(retryAt.value).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })} 第 ${Number(props.document.metadata?.indexAttempts || 1) + 1} 次尝试`
    : ''
)
const progressLabel = computed(() =>
  retryAt.value
    ? retryLabel.value
    : active.value && total.value > 0
      ? `${done.value} / ${total.value} 个分片已处理`
      : active.value
        ? '等待索引任务处理'
        : ''
)
</script>

<template>
  <div class="knowledge-index-status" :aria-label="label">
    <ui-tag :type="failed ? 'danger' : document.status === 'INDEXED' ? 'success' : 'info'" effect="light">{{ label }}</ui-tag>
    <template v-if="active"
      ><ui-progress v-if="percentage != null" :percentage="percentage" :stroke-width="5" :show-text="false" /><small>{{
        progressLabel
      }}</small></template
    >
    <small v-if="deleting">正在清理原文件和索引</small>
    <small v-if="(failed || retryAt) && document.metadata?.indexError" class="index-error">{{ document.metadata.indexError }}</small>
  </div>
</template>

<style scoped>
.knowledge-index-status {
  min-width: 0;
  display: grid;
  justify-items: start;
  gap: 5px;
}
.knowledge-index-status :deep(.n-progress) {
  width: 100%;
}
.knowledge-index-status small {
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.5;
  overflow-wrap: anywhere;
}
.knowledge-index-status .index-error {
  color: var(--danger-text);
}
</style>
