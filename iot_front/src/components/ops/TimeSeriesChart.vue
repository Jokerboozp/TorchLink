<script setup>
// 时序图基于 uPlot 绘制：同一 syncKey 的图表共享十字光标，拖选区域触发时间范围联动。
// bars 模式按系列堆叠绘制柱状图（日志量分布），图例仍显示各系列自身数值。
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import uPlot from 'uplot'
import 'uplot/dist/uPlot.min.css'
import { formatValue } from '../../ops/format.js'

const props = defineProps({
  times: { type: Array, default: () => [] },
  series: { type: Array, default: () => [] },
  unit: { type: String, default: 'short' },
  decimals: { type: [Number, String], default: undefined },
  height: { type: Number, default: 240 },
  syncKey: { type: String, default: '' },
  fill: { type: Boolean, default: true },
  legend: { type: Boolean, default: true },
  min: { type: Number, default: undefined },
  max: { type: Number, default: undefined },
  bars: { type: Boolean, default: false },
  emptyText: { type: String, default: '所选时间范围内没有样本' }
})
const emit = defineEmits(['zoom'])
const host = ref(null)
let chart = null
let observer = null
let signature = ''

function token(name, fallback) {
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return value || fallback
}

function palette() {
  return Array.from({ length: 10 }, (_, i) => token(`--chart-${i + 1}`, 'currentColor'))
}

function withAlpha(color, alpha) {
  const hex = color.match(/^#([0-9a-f]{6})$/i)
  if (!hex) return color
  const n = parseInt(hex[1], 16)
  return `rgba(${n >> 16}, ${(n >> 8) & 255}, ${n & 255}, ${alpha})`
}

// 时间轴使用 24 小时制中文格式：跨天显示“月-日 时:分”，一天内只显示时间。
function timeTick(u) {
  const span = (u.scales.x.max ?? 0) - (u.scales.x.min ?? 0)
  const pad = n => String(n).padStart(2, '0')
  return t => {
    const d = new Date(t * 1000)
    const hm = `${pad(d.getHours())}:${pad(d.getMinutes())}`
    if (span > 2 * 86400) return `${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
    if (span > 86400 || (d.getHours() === 0 && d.getMinutes() === 0)) return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${hm}`
    if (span < 600) return `${hm}:${pad(d.getSeconds())}`
    return hm
  }
}

function build() {
  destroy()
  if (!host.value || !props.times.length || !props.series.length) return
  const colors = palette()
  const grid = token('--chart-grid', 'currentColor')
  const axis = token('--chart-axis', 'currentColor')
  const font = `12px ${token('--font-sans', 'sans-serif')}`
  const width = Math.max(host.value.clientWidth, 200)
  const fmt = value => formatValue(value, props.unit, props.decimals)
  const options = {
    width,
    height: props.height,
    cursor: { sync: props.syncKey ? { key: props.syncKey } : undefined, drag: { x: true, y: false } },
    select: { show: true },
    legend: { show: props.legend, live: true },
    scales: { x: { time: true }, y: { range: (u, min, max) => [props.min ?? (min > 0 ? 0 : min), props.max ?? (max === min ? max + 1 : max)] } },
    axes: [
      { stroke: axis, font, grid: { stroke: grid, width: 1 }, ticks: { stroke: grid }, values: (u, ticks) => ticks.map(timeTick(u)) },
      { stroke: axis, font, grid: { stroke: grid, width: 1 }, ticks: { stroke: grid }, size: 80, values: (u, ticks) => ticks.map(fmt) }
    ],
    series: [
      { label: '时间', value: (u, v) => (v == null ? '—' : new Date(v * 1000).toLocaleString('zh-CN', { hour12: false })) },
      ...ordered().map(({ s, i }) => {
        const color = s.color ? token(s.color, colors[i % colors.length]) : colors[i % colors.length]
        if (props.bars) return { label: s.name, stroke: color, fill: color, width: 0, paths: uPlot.paths.bars({ size: [0.9, 64], align: 1 }), points: { show: false }, value: (u, v, si, idx) => fmt(idx == null ? null : props.series[i]?.values[idx]) }
        return { label: s.name, stroke: color, width: 1.5, fill: props.fill && props.series.length <= 3 ? withAlpha(color, 0.08) : undefined, spanGaps: false, points: { show: props.times.length < 60 }, value: (u, v) => fmt(v) }
      })
    ],
    hooks: {
      setSelect: [u => {
        if (u.select.width < 8) return
        const from = u.posToVal(u.select.left, 'x') * 1000
        const to = u.posToVal(u.select.left + u.select.width, 'x') * 1000
        u.setSelect({ left: 0, width: 0, top: 0, height: 0 }, false)
        emit('zoom', { from, to })
      }]
    }
  }
  chart = new uPlot(options, chartData(), host.value)
  signature = props.series.map(s => s.name).join('\u0000') + '|' + props.unit + '|' + props.height + '|' + props.bars
}

// 堆叠柱状图：第 i 个系列绘制前 i 个系列之和，后绘制的较矮柱覆盖在较高柱上。
function chartData() {
  const times = props.times.map(t => t / 1000)
  if (!props.bars) return [times, ...props.series.map(s => s.values)]
  const totals = props.times.map(() => 0)
  const stacked = props.series.map(s => s.values.map((v, i) => (totals[i] += v ?? 0)))
  return [times, ...stacked.reverse()]
}

function ordered() {
  const items = props.series.map((s, i) => ({ s, i }))
  return props.bars ? items.reverse() : items
}

function update() {
  const next = props.series.map(s => s.name).join('\u0000') + '|' + props.unit + '|' + props.height + '|' + props.bars
  if (!chart || next !== signature || !props.times.length) { build(); return }
  chart.setData(chartData())
}

function destroy() {
  chart?.destroy()
  chart = null
}

watch(() => [props.times, props.series, props.unit, props.height, props.fill, props.bars], update)

onMounted(() => {
  build()
  observer = new ResizeObserver(() => { if (chart && host.value) chart.setSize({ width: Math.max(host.value.clientWidth, 200), height: props.height }) })
  observer.observe(host.value)
})
onBeforeUnmount(() => { observer?.disconnect(); destroy() })
</script>

<template>
  <div class="ops-chart">
    <div ref="host" class="ops-chart__canvas" :class="{ 'is-empty': !times.length || !series.length }" />
    <ui-empty v-if="!times.length || !series.length" :description="emptyText" :image-size="56" />
  </div>
</template>

<style scoped>
.ops-chart { min-width: 0; }
.ops-chart__canvas { width: 100%; min-width: 0; }
.ops-chart__canvas.is-empty { display: none; }
.ops-chart :deep(.u-legend) { max-height: 88px; overflow: auto; color: var(--text-secondary); font: var(--font-size-xs) var(--font-sans); text-align: left; }
.ops-chart :deep(.u-legend .u-marker) { width: 10px; height: 3px; border-radius: 2px; }
.ops-chart :deep(.u-legend th) { font-weight: 400; }
.ops-chart :deep(.u-legend td) { font-variant-numeric: tabular-nums; }
.ops-chart :deep(.u-select) { background: var(--primary-soft); }
</style>
