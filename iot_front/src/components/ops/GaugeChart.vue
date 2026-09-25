<script setup>
// 仪表盘半圆仪表：按阈值分段着色，数值颜色取当前所在阈值。
import { computed } from 'vue'
import { formatValue } from '../../ops/format.js'
import { cssColor, thresholdColor } from '../../ops/frames.js'

const props = defineProps({
  value: { type: Number, default: null },
  min: { type: Number, default: 0 },
  max: { type: Number, default: 100 },
  unit: { type: String, default: 'short' },
  decimals: { type: [Number, String], default: undefined },
  thresholds: { type: Object, default: null },
  label: { type: String, default: '' },
  showMarkers: { type: Boolean, default: true }
})
const span = computed(() => (props.max - props.min) || 1)
const ratio = value => Math.min(1, Math.max(0, (value - props.min) / span.value))
// 半圆弧从 180° 到 0°，路径长度按 100 归一。
const arc = 'M 20 100 A 80 80 0 0 1 180 100'
const outer = 'M 6 100 A 94 94 0 0 1 194 100'
const segments = computed(() => {
  const steps = props.thresholds?.steps || []
  if (!props.showMarkers || steps.length < 2) return []
  return steps.map((step, i) => {
    const start = step.value == null ? 0 : ratio(props.thresholds.mode === 'percentage' ? props.min + span.value * step.value / 100 : step.value)
    const nextStep = steps[i + 1]
    const end = nextStep ? ratio(props.thresholds.mode === 'percentage' ? props.min + span.value * nextStep.value / 100 : nextStep.value) : 1
    return { color: cssColor(step.color), dash: `${Math.max(0, (end - start) * 100)} 100`, offset: -start * 100 }
  })
})
const fill = computed(() => (props.value == null ? 0 : ratio(props.value) * 100))
const color = computed(() => thresholdColor(props.value, props.thresholds, props.min, props.max))
</script>

<template>
  <div class="gauge" role="img" :aria-label="`${label} ${formatValue(value, unit, decimals)}`">
    <svg viewBox="0 0 200 112">
      <path :d="arc" pathLength="100" fill="none" class="gauge__track" stroke-width="14" />
      <path v-for="(segment, index) in segments" :key="index" :d="outer" pathLength="100" fill="none" :stroke="segment.color" stroke-width="4" :stroke-dasharray="segment.dash" :stroke-dashoffset="segment.offset" />
      <path :d="arc" pathLength="100" fill="none" :stroke="color" stroke-width="14" stroke-linecap="butt" :stroke-dasharray="`${fill} 100`" />
    </svg>
    <strong :style="{ color }">{{ formatValue(value, unit, decimals) }}</strong>
    <span v-if="label" :title="label">{{ label }}</span>
  </div>
</template>

<style scoped>
.gauge { position: relative; display: grid; justify-items: center; min-width: 0; }
.gauge svg { width: 100%; max-width: 240px; overflow: visible; }
.gauge__track { stroke: var(--surface-hover); }
.gauge strong { margin-top: -40px; font-size: var(--font-size-xl); font-weight: var(--font-weight-semibold); font-variant-numeric: tabular-nums; }
.gauge span { max-width: 100%; overflow: hidden; color: var(--text-muted); font-size: var(--font-size-xs); text-overflow: ellipsis; white-space: nowrap; }
</style>
