<script setup>
// 统计卡片中的迷你趋势线。
import { computed } from 'vue'

const props = defineProps({ values: { type: Array, default: () => [] }, color: { type: String, default: 'var(--primary)' } })
const points = computed(() => {
  const values = props.values.filter(v => v != null)
  if (values.length < 2) return ''
  const min = Math.min(...values)
  const max = Math.max(...values)
  const range = max - min || 1
  const step = 100 / (props.values.length - 1)
  return props.values.map((v, i) => (v == null ? null : `${(i * step).toFixed(2)},${(28 - ((v - min) / range) * 26).toFixed(2)}`)).filter(Boolean).join(' ')
})
</script>

<template>
  <svg v-if="points" class="spark" viewBox="0 0 100 30" preserveAspectRatio="none" aria-hidden="true"><polyline :points="points" fill="none" :stroke="color" stroke-width="1.5" vector-effect="non-scaling-stroke" /></svg>
</template>

<style scoped>
.spark { display: block; width: 100%; height: 32px; opacity: 0.7; }
</style>
