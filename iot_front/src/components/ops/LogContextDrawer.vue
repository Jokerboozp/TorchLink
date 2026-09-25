<script setup>
// 日志上下文：按同一日志流查询所选行前后的日志，按时间正序显示并标出当前行。
import { computed, nextTick, ref, watch } from 'vue'
import { latest, opsErrorText, opsGet } from '../../ops/opsApi.js'
import { labelString } from '../../ops/format.js'
import LogList from './LogList.vue'

const props = defineProps({ modelValue: Boolean, entry: { type: Object, default: null } })
const emit = defineEmits(['update:modelValue'])
const size = ref(25)
const context = ref({ before: [], after: [] })
const loading = ref(false)
const error = ref('')
const listHost = ref(null)
const runner = latest()
const same = (a, b) => a.ts === b.ts && a.line === b.line
const rows = computed(() => {
  if (!props.entry) return []
  const before = [...context.value.before].reverse().filter(e => !same(e, props.entry))
  const after = context.value.after.filter(e => !same(e, props.entry))
  return [...before, { ...props.entry, current: true }, ...after]
})
const index = computed(() => rows.value.findIndex(e => e.current))

async function load() {
  if (!props.entry) return
  loading.value = true
  error.value = ''
  try {
    context.value = await runner.run(signal => opsGet('/api/v1/ops/logs/context', { labels: props.entry.labels, ts: props.entry.ts, size: size.value }, signal))
    await nextTick()
    listHost.value?.querySelector('.log-row.is-current')?.scrollIntoView({ block: 'center' })
  } catch (e) {
    if (e?.name !== 'AbortError') { error.value = opsErrorText(e); context.value = { before: [], after: [] } }
  } finally { loading.value = false }
}
watch(() => [props.modelValue, props.entry], ([open]) => { if (open) { size.value = 25; load() } else runner.cancel() })
function more() { size.value = Math.min(size.value + 25, 100); load() }
</script>

<template>
  <ui-drawer :model-value="modelValue" title="日志上下文" size="min(920px, 96vw)" @update:model-value="value => emit('update:modelValue', value)">
    <div class="context">
      <p v-if="entry" class="context__stream">日志流 <code>{{ labelString(entry.labels) }}</code></p>
      <div class="context__actions">
        <span>前 {{ index < 0 ? 0 : index }} 行 · 后 {{ index < 0 ? 0 : rows.length - index - 1 }} 行（同一日志流，前后各最多 6 小时）</span>
        <ui-button size="small" :loading="loading" :disabled="size >= 100" @click="more">加载更多上下文</ui-button>
      </div>
      <ui-alert v-if="error" type="error" :title="error" :closable="false" show-icon />
      <div ref="listHost" class="context__list">
        <LogList :entries="rows" :show-service="false" :context-enabled="false" />
      </div>
    </div>
  </ui-drawer>
</template>

<style scoped>
.context { display: grid; gap: var(--space-3); min-width: 0; }
.context__stream { margin: 0; color: var(--text-secondary); font-size: var(--font-size-xs); overflow-wrap: anywhere; }
.context__stream code { font-family: var(--font-mono); }
.context__actions { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: var(--space-2); color: var(--text-muted); font-size: var(--font-size-xs); }
.context__list { min-width: 0; }
</style>
