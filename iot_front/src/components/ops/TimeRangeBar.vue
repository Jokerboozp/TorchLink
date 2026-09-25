<script setup>
// 统一时间范围与自动刷新：相对范围每次刷新重新计算，绝对范围来自图表拖选或自定义。
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { RefreshCw, RotateCcw } from '@lucide/vue'
import { rangeLabel, rangePresets, refreshOptions } from '../../ops/timeRange.js'

const props = defineProps({
  range: { type: Object, required: true },
  refresh: { type: Number, default: 0 },
  loading: { type: Boolean, default: false },
  showRefresh: { type: Boolean, default: true },
  maxHours: { type: Number, default: 0 }
})
const emit = defineEmits(['update:range', 'update:refresh', 'refresh'])
const customVisible = ref(false)
const custom = ref([Date.now() - 3600e3, Date.now()])
const presets = computed(() => rangePresets.filter(p => !props.maxHours || p.ms <= props.maxHours * 3600e3))
const selected = computed({
  get: () => (typeof props.range.from === 'string' && (props.range.to ?? 'now') === 'now' ? props.range.from : 'custom'),
  set: value => {
    if (value === 'custom') { customVisible.value = true; return }
    emit('update:range', { from: value, to: 'now' })
  }
})
const absolute = computed(() => typeof props.range.from === 'number')
let timer = null
function schedule() {
  clearInterval(timer)
  timer = null
  if (props.refresh > 0) timer = setInterval(() => { if (!document.hidden) emit('refresh') }, props.refresh)
}
watch(() => props.refresh, schedule, { immediate: true })
onBeforeUnmount(() => clearInterval(timer))
function applyCustom() {
  const [from, to] = custom.value || []
  if (!from || !to || to <= from) return
  emit('update:range', { from, to })
  customVisible.value = false
}
</script>

<template>
  <div class="range-bar">
    <ui-select v-model="selected" size="small" class="range-bar__select" aria-label="时间范围">
      <ui-option v-for="item in presets" :key="item.value" :value="item.value" :label="item.label" />
      <ui-option value="custom" :label="absolute ? rangeLabel(range) : '自定义…'" />
    </ui-select>
    <ui-button v-if="absolute" size="small" title="回到近 1 小时" @click="emit('update:range', { from: 'now-1h', to: 'now' })"><RotateCcw />重置</ui-button>
    <ui-select v-if="showRefresh" :model-value="refresh" size="small" class="range-bar__refresh" aria-label="自动刷新" @update:model-value="value => emit('update:refresh', value)">
      <ui-option v-for="item in refreshOptions" :key="item.value" :value="item.value" :label="item.label" />
    </ui-select>
    <ui-button size="small" :loading="loading" @click="emit('refresh')"><RefreshCw />刷新</ui-button>
    <ui-dialog v-model="customVisible" title="自定义时间范围" width="min(520px, 94vw)">
      <ui-date-range v-model="custom" disable-future class="range-bar__picker" />
      <template #footer><ui-button @click="customVisible = false">取消</ui-button><ui-button type="primary" @click="applyCustom">应用</ui-button></template>
    </ui-dialog>
  </div>
</template>

<style scoped>
.range-bar { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-2); }
.range-bar__select { width: 150px; }
.range-bar__refresh { width: 130px; }
.range-bar__picker { width: 100%; }
@media (max-width: 767px) {
  .range-bar__select, .range-bar__refresh { flex: 1 1 130px; width: auto; }
}
</style>
