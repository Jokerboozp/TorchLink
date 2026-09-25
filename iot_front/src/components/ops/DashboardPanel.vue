<script setup>
// 仪表盘面板的原生渲染：时序图、统计卡片、仪表、条形仪表、表格、日志与文本。
// 未实现的面板类型显示原因，内容仍完整保存在 Grafana 中。
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { AlertTriangle, ArrowDown, ArrowLeft, ArrowRight, ArrowUp, Copy, Info, Loader2, MoreHorizontal, Pencil, Trash2 } from '@lucide/vue'
import { formatValue } from '../../ops/format.js'
import { framesToChart, framesToLogs, framesToTable, reduceFrames, thresholdColor } from '../../ops/frames.js'
import MarkdownContent from '../MarkdownContent.vue'
import GaugeChart from './GaugeChart.vue'
import LogList from './LogList.vue'
import SparkLine from './SparkLine.vue'
import TimeSeriesChart from './TimeSeriesChart.vue'

const props = defineProps({
  panel: { type: Object, required: true },
  data: { type: Object, default: null },
  loading: { type: Boolean, default: false },
  error: { type: String, default: '' },
  support: { type: Object, default: null },
  editing: { type: Boolean, default: false },
  title: { type: String, default: '' },
  syncKey: { type: String, default: '' },
  menu: { type: Array, default: () => [] }
})
const emit = defineEmits(['zoom', 'command'])
const body = ref(null)
const bodyHeight = ref(160)
let observer = null

const type = computed(() => props.panel.type)
const defaults = computed(() => props.panel.fieldConfig?.defaults || {})
const options = computed(() => props.panel.options || {})
const legacyAxis = computed(() => props.panel.yaxes?.[0] || {})
const unit = computed(() => defaults.value.unit || legacyAxis.value.format || 'short')
const decimals = computed(() => defaults.value.decimals ?? legacyAxis.value.decimals ?? undefined)
const numberOrUndefined = value => (value == null || value === '' || !Number.isFinite(Number(value)) ? undefined : Number(value))
const min = computed(() => numberOrUndefined(defaults.value.min ?? legacyAxis.value.min))
const max = computed(() => numberOrUndefined(defaults.value.max ?? legacyAxis.value.max))
const thresholds = computed(() => defaults.value.thresholds || null)
const frames = computed(() => Object.values(props.data?.frames || {}).flat())
const errors = computed(() => Object.entries(props.data?.errors || {}))
const unsupported = computed(() => props.support?.status === 'unsupported')
const partialReasons = computed(() => (props.support?.status === 'partial' ? props.support.reasons || [] : []))
const calc = computed(() => options.value.reduceOptions?.calcs?.[0] || 'lastNotNull')

const chart = computed(() => framesToChart(frames.value))
const showLegend = computed(() => (type.value === 'graph' ? props.panel.legend?.show !== false : options.value.legend?.showLegend !== false && options.value.legend?.displayMode !== 'hidden'))
const drawBars = computed(() => defaults.value.custom?.drawStyle === 'bars' || props.panel.bars === true)
const reduced = computed(() => reduceFrames(frames.value, calc.value).slice(0, 24))
const gaugeMin = computed(() => min.value ?? 0)
const gaugeMax = computed(() => max.value ?? (unit.value === 'percentunit' ? 1 : 100))
const table = computed(() => framesToTable(frames.value))
const logs = computed(() => {
  const list = framesToLogs(frames.value)
  const asc = options.value.sortOrder === 'Ascending'
  return list.sort((a, b) => (asc ? a.timeMs - b.timeMs : b.timeMs - a.timeMs))
})
const textContent = computed(() => options.value.content ?? props.panel.content ?? '')
const textMode = computed(() => options.value.mode || props.panel.mode || 'markdown')
const colorMode = computed(() => options.value.colorMode || 'value')
const hasData = computed(() => frames.value.some(frame => frame.fields?.some(field => field.values?.length)))

function sparkValues(name) { return chart.value.series.find(s => s.name === name)?.values || [] }
function statStyle(value) {
  const color = thresholdColor(value, thresholds.value, gaugeMin.value, gaugeMax.value)
  if (colorMode.value === 'background') return { background: color, color: 'var(--text-inverse)' }
  if (colorMode.value === 'none') return {}
  return { color }
}
function barRatio(value) { return value == null ? 0 : Math.min(1, Math.max(0, (value - gaugeMin.value) / ((gaugeMax.value - gaugeMin.value) || 1))) }
function cell(row, column) {
  const value = row[column.key]
  if (value == null) return '—'
  if (column.type === 'time') return new Date(Number(value)).toLocaleString('zh-CN', { hour12: false })
  if (column.type === 'number') return formatValue(value, column.unit || unit.value, decimals.value)
  return typeof value === 'object' ? JSON.stringify(value) : String(value)
}

onMounted(() => {
  observer = new ResizeObserver(entries => { bodyHeight.value = Math.max(80, Math.floor(entries[0].contentRect.height)) })
  if (body.value) observer.observe(body.value)
})
onBeforeUnmount(() => observer?.disconnect())
</script>

<template>
  <section class="dash-panel" :class="{ 'is-editing': editing, 'is-transparent': panel.transparent }" :aria-label="title || panel.title || '面板'">
    <header class="dash-panel__head">
      <strong :title="panel.description || title">{{ title || panel.title || '未命名面板' }}</strong>
      <span class="dash-panel__icons">
        <Loader2 v-if="loading" class="dash-panel__spin" aria-label="加载中" />
        <ui-tooltip v-if="panel.description" :content="panel.description" placement="top"><Info class="dash-panel__icon" /></ui-tooltip>
        <ui-tooltip v-if="partialReasons.length" :content="`部分支持：${partialReasons.join('；')}`" placement="top"><AlertTriangle class="dash-panel__icon is-warning" /></ui-tooltip>
        <ui-tooltip v-if="error || errors.length" :content="error || errors.map(([ref, msg]) => `${ref}：${msg}`).join('；')" placement="top"><AlertTriangle class="dash-panel__icon is-danger" /></ui-tooltip>
      </span>
      <div v-if="editing" class="dash-panel__layout">
        <button type="button" title="上移" @click="emit('command', 'up')"><ArrowUp /></button>
        <button type="button" title="下移" @click="emit('command', 'down')"><ArrowDown /></button>
        <button type="button" title="左移" @click="emit('command', 'left')"><ArrowLeft /></button>
        <button type="button" title="右移" @click="emit('command', 'right')"><ArrowRight /></button>
        <button type="button" title="编辑面板" @click="emit('command', 'edit')"><Pencil /></button>
        <button type="button" title="复制面板" @click="emit('command', 'duplicate')"><Copy /></button>
        <button type="button" title="删除面板" class="is-danger" @click="emit('command', 'remove')"><Trash2 /></button>
      </div>
      <ui-dropdown v-if="menu.length" class="dash-panel__menu" @command="value => emit('command', value)">
        <button type="button" class="dash-panel__menu-button" aria-label="面板菜单"><MoreHorizontal /></button>
        <template #dropdown><ui-dropdown-menu><ui-dropdown-item v-for="item in menu" :key="item.command" :command="item.command">{{ item.label }}</ui-dropdown-item></ui-dropdown-menu></template>
      </ui-dropdown>
    </header>

    <div ref="body" class="dash-panel__body">
      <div v-if="unsupported" class="dash-panel__notice">
        <strong>平台暂不支持显示此面板</strong>
        <span v-for="reason in support.reasons" :key="reason">{{ reason }}</span>
      </div>
      <p v-else-if="error && !hasData" class="dash-panel__error">{{ error }}</p>

      <template v-else-if="type === 'text'">
        <MarkdownContent v-if="textMode === 'markdown'" class="dash-panel__text" :source="textContent" />
        <pre v-else class="dash-panel__text dash-panel__pre">{{ textContent }}</pre>
      </template>

      <TimeSeriesChart v-else-if="type === 'timeseries' || type === 'graph'" :times="chart.times" :series="chart.series" :unit="unit" :decimals="decimals" :min="min" :max="max" :legend="showLegend" :bars="drawBars" :fill="(defaults.custom?.fillOpacity ?? 10) > 0" :height="Math.max(80, bodyHeight - (showLegend ? 34 : 4))" :sync-key="syncKey" empty-text="无数据" @zoom="value => emit('zoom', value)" />

      <div v-else-if="type === 'stat'" class="stat-grid" :class="{ 'is-single': reduced.length === 1 }">
        <div v-for="item in reduced" :key="item.name" class="stat-item" :style="statStyle(item.value)">
          <small v-if="reduced.length > 1 && options.textMode !== 'value'">{{ item.name }}</small>
          <strong :style="{ fontSize: reduced.length === 1 ? `${Math.min(56, Math.max(20, bodyHeight / 3))}px` : undefined }">{{ formatValue(item.value, unit, decimals) }}</strong>
          <SparkLine v-if="options.graphMode === 'area'" class="stat-item__spark" :values="sparkValues(item.name)" :color="colorMode === 'background' ? 'var(--text-inverse)' : statStyle(item.value).color || 'var(--primary)'" />
        </div>
        <p v-if="!reduced.length && !loading" class="dash-panel__empty">无数据</p>
      </div>

      <div v-else-if="type === 'gauge'" class="gauge-grid">
        <GaugeChart v-for="item in reduced" :key="item.name" :value="item.value" :min="gaugeMin" :max="gaugeMax" :unit="unit" :decimals="decimals" :thresholds="thresholds" :label="reduced.length > 1 ? item.name : ''" :show-markers="options.showThresholdMarkers !== false" />
        <p v-if="!reduced.length && !loading" class="dash-panel__empty">无数据</p>
      </div>

      <div v-else-if="type === 'bargauge'" class="bar-list" :class="{ 'is-vertical': options.orientation === 'vertical' }">
        <div v-for="item in reduced" :key="item.name" class="bar-item">
          <span class="bar-item__name" :title="item.name">{{ item.name }}</span>
          <span class="bar-item__track"><span class="bar-item__fill" :style="{ width: `${barRatio(item.value) * 100}%`, background: thresholdColor(item.value, thresholds, gaugeMin, gaugeMax) }" /></span>
          <strong :style="{ color: thresholdColor(item.value, thresholds, gaugeMin, gaugeMax) }">{{ formatValue(item.value, unit, decimals) }}</strong>
        </div>
        <p v-if="!reduced.length && !loading" class="dash-panel__empty">无数据</p>
      </div>

      <div v-else-if="type === 'table'" class="dash-table">
        <table v-if="table.rows.length">
          <thead><tr><th v-for="column in table.columns" :key="column.key" :class="{ 'is-number': column.type === 'number' }">{{ column.title }}</th></tr></thead>
          <tbody><tr v-for="(row, index) in table.rows.slice(0, 1000)" :key="index"><td v-for="column in table.columns" :key="column.key" :class="{ 'is-number': column.type === 'number' }">{{ cell(row, column) }}</td></tr></tbody>
        </table>
        <p v-else-if="!loading" class="dash-panel__empty">无数据</p>
        <p v-if="table.rows.length > 1000" class="dash-panel__empty">仅显示前 1000 行</p>
      </div>

      <div v-else-if="type === 'logs'" class="dash-logs">
        <LogList v-if="logs.length" :entries="logs" :wrap="options.wrapLogMessage !== false" :context-enabled="false" />
        <p v-else-if="!loading" class="dash-panel__empty">无数据</p>
      </div>
    </div>
  </section>
</template>

<style scoped>
.dash-panel { display: flex; flex-direction: column; height: 100%; min-width: 0; overflow: hidden; background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-md); }
.dash-panel.is-transparent { background: transparent; border-color: transparent; }
.dash-panel.is-editing { border-style: dashed; border-color: var(--border-strong); }
.dash-panel__head { display: flex; align-items: center; gap: var(--space-2); min-height: 32px; padding: 4px var(--space-2) 0 var(--space-3); }
.dash-panel__head > strong { min-width: 0; overflow: hidden; color: var(--text-strong); font-size: var(--font-size-sm); font-weight: var(--font-weight-semibold); text-overflow: ellipsis; white-space: nowrap; }
.dash-panel__icons { display: inline-flex; align-items: center; gap: 4px; }
.dash-panel__icon, .dash-panel__spin { width: 14px; height: 14px; color: var(--text-muted); }
.dash-panel__icon.is-warning { color: var(--warning); }
.dash-panel__icon.is-danger { color: var(--danger); }
.dash-panel__spin { animation: spin 1.2s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
.dash-panel__layout { display: inline-flex; gap: 2px; margin-left: auto; }
.dash-panel__layout button, .dash-panel__menu-button { display: inline-grid; place-items: center; width: 24px; height: 24px; padding: 0; color: var(--text-secondary); background: none; border: 0; border-radius: var(--radius-sm); cursor: pointer; }
.dash-panel__layout button:hover, .dash-panel__menu-button:hover { color: var(--text-strong); background: var(--surface-hover); }
.dash-panel__layout button.is-danger:hover { color: var(--danger); }
.dash-panel__layout svg, .dash-panel__menu-button svg { width: 14px; height: 14px; }
.dash-panel__menu { margin-left: auto; }
.dash-panel__layout + .dash-panel__menu { margin-left: 0; }
.dash-panel__body { position: relative; flex: 1 1 auto; min-height: 0; padding: var(--space-1) var(--space-2) var(--space-2); overflow: hidden; }
.dash-panel__notice { display: grid; align-content: center; gap: 4px; height: 100%; color: var(--text-muted); font-size: var(--font-size-xs); text-align: center; }
.dash-panel__notice strong { color: var(--text-secondary); font-size: var(--font-size-sm); }
.dash-panel__error { margin: 0; color: var(--danger-text); font-size: var(--font-size-xs); overflow-wrap: anywhere; }
.dash-panel__empty { margin: 0; color: var(--text-muted); font-size: var(--font-size-xs); text-align: center; }
.dash-panel__text { height: 100%; overflow: auto; color: var(--text); font-size: var(--font-size-sm); }
.dash-panel__pre { margin: 0; white-space: pre-wrap; word-break: break-word; font-family: inherit; }
.stat-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); gap: var(--space-2); height: 100%; overflow: auto; }
.stat-grid.is-single { grid-template-columns: minmax(0, 1fr); }
.stat-item { position: relative; display: grid; align-content: center; justify-items: center; gap: 2px; min-height: 56px; padding: var(--space-2); overflow: hidden; border-radius: var(--radius-sm); }
.stat-item small { max-width: 100%; overflow: hidden; color: inherit; font-size: var(--font-size-xs); opacity: 0.8; text-overflow: ellipsis; white-space: nowrap; }
.stat-item strong { position: relative; z-index: 1; font-size: var(--font-size-xl); font-weight: var(--font-weight-semibold); font-variant-numeric: tabular-nums; line-height: 1.1; }
.stat-item__spark { position: absolute; right: 0; bottom: 0; left: 0; width: 100%; height: 40%; opacity: 0.45; }
.gauge-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(140px, 1fr)); align-items: center; gap: var(--space-2); height: 100%; overflow: auto; }
.bar-list { display: grid; align-content: center; gap: var(--space-2); height: 100%; overflow: auto; }
.bar-item { display: grid; grid-template-columns: minmax(60px, 30%) minmax(0, 1fr) auto; align-items: center; gap: var(--space-2); font-size: var(--font-size-xs); }
.bar-item__name { overflow: hidden; color: var(--text-secondary); text-overflow: ellipsis; white-space: nowrap; }
.bar-item__track { height: 12px; overflow: hidden; background: var(--surface-muted); border-radius: 3px; }
.bar-item__fill { display: block; height: 100%; border-radius: 3px; transition: width 0.3s; }
.bar-item strong { font-variant-numeric: tabular-nums; }
.dash-table { height: 100%; overflow: auto; }
.dash-table table { width: 100%; border-collapse: collapse; font-size: var(--font-size-xs); }
.dash-table th, .dash-table td { padding: 4px 8px; border-bottom: 1px solid var(--border); text-align: left; white-space: nowrap; }
.dash-table th { position: sticky; top: 0; color: var(--text-secondary); font-weight: var(--font-weight-medium); background: var(--surface); }
.dash-table .is-number { text-align: right; font-variant-numeric: tabular-nums; }
.dash-logs { height: 100%; overflow: auto; }
.dash-logs :deep(.log-list) { border: 0; }
</style>
