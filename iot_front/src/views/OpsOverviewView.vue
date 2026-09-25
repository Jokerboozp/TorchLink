<script setup>
// 运维总览：组件连接状态、平台已有指标与主机资源；区分未配置、采集失败、无样本和正常零值。
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { Activity, FileSearch, LineChart } from '@lucide/vue'
import { can } from '../permissions'
import { formatKpi } from '../ops/format.js'
import { metricResultToChart } from '../ops/frames.js'
import { latest, opsErrorText, opsGet } from '../ops/opsApi.js'
import { resolveRange } from '../ops/timeRange.js'
import StatusDot from '../components/layout/StatusDot.vue'
import TimeRangeBar from '../components/ops/TimeRangeBar.vue'
import TimeSeriesChart from '../components/ops/TimeSeriesChart.vue'

const emit = defineEmits(['navigate'])
const range = ref({ from: 'now-3h', to: 'now' })
const refresh = ref(60e3)
const overview = ref(null)
const loading = ref(false)
const error = ref('')
const trends = reactive({})
const trendLoading = ref(false)
const snapshot = latest()
const series = latest()

const groups = [
  { id: 'platform', title: '平台链路', desc: '上报、解析、队列与 AI 调用' },
  { id: 'backup', title: '备份服务', desc: '最近成功备份与失败次数' },
  { id: 'host', title: '主机资源', desc: 'node-exporter 采集的主机指标' },
  { id: 'observability', title: '监控链路', desc: '日志接收与触发中的监控告警' }
]
const trendCharts = [
  { id: 'ingest_rate', title: '上报速率', unit: 'suffix:条/秒' },
  { id: 'parse_failed', title: '解析失败（5 分钟）', unit: 'short' },
  { id: 'mqtt_backlog', title: 'MQTT 接收积压', unit: 'short' },
  { id: 'host_cpu', title: 'CPU 使用率', unit: 'percent' },
  { id: 'host_memory', title: '内存使用率', unit: 'percent' },
  { id: 'log_ingest', title: '日志接收速率', unit: 'suffix:行/秒' }
]
const statusText = { unconfigured: '未配置', no_target: '未配置采集目标', scrape_failed: '采集失败', no_data: '暂无样本', zero: '正常（零值）', ok: '正常', error: '查询失败' }
const componentTone = { ok: 'success', degraded: 'warning', down: 'danger', unconfigured: 'neutral' }
const componentText = { ok: '正常', degraded: '部分异常', down: '无法连接', unconfigured: '未配置' }

const kpisByGroup = computed(() => groups.map(group => ({ ...group, items: (overview.value?.kpis || []).filter(k => k.group === group.id) })))
const jobs = computed(() => Object.entries(overview.value?.jobs || {}).map(([job, health]) => ({ job, ...health })).sort((a, b) => a.job.localeCompare(b.job)))
const failingJobs = computed(() => jobs.value.filter(job => job.up < job.total))

function kpiTone(kpi) {
  if (kpi.level === 'critical') return 'danger'
  if (kpi.level === 'warning') return 'warning'
  if (['scrape_failed', 'error'].includes(kpi.status)) return 'danger'
  if (['no_target', 'no_data', 'unconfigured'].includes(kpi.status)) return 'neutral'
  return 'success'
}

async function load() {
  loading.value = true
  try {
    overview.value = await snapshot.run(signal => opsGet('/api/v1/ops/overview', {}, signal))
    error.value = ''
  } catch (e) {
    if (e?.name === 'AbortError') return
    error.value = opsErrorText(e)
  } finally { loading.value = false }
  loadTrends()
}

async function loadTrends() {
  const { from, to } = resolveRange(range.value)
  trendLoading.value = true
  try {
    const data = await series.run(signal => opsGet('/api/v1/ops/overview/series', { ids: trendCharts.map(c => c.id).join(','), start: from, end: to, maxPoints: 300 }, signal))
    for (const item of data.items || []) trends[item.id] = { ...metricResultToChart(item.result), error: item.error }
  } catch (e) {
    if (e?.name !== 'AbortError') for (const chart of trendCharts) trends[chart.id] = { times: [], series: [], error: opsErrorText(e) }
  } finally { trendLoading.value = false }
}

function zoom({ from, to }) { range.value = { from: Math.round(from), to: Math.round(to) } }
function openMetrics(kpi) { emit('navigate', 'opsMetrics', { query: kpi.expr, range: range.value }) }
function openLogs(kpi) { emit('navigate', 'opsLogs', { filter: { services: [kpi.logService], keyword: kpi.logKeyword || '' }, range: { from: 'now-1h', to: 'now' } }) }
function openTargets() { emit('navigate', 'opsMetrics', { tab: 'targets' }) }

watch(range, loadTrends, { deep: true })
onMounted(load)
onBeforeUnmount(() => { snapshot.cancel(); series.cancel() })
</script>

<template>
  <div class="ops-page">
    <div class="ops-toolbar">
      <span class="ops-toolbar__meta">{{ loading ? '正在检查组件与指标…' : overview ? `检查于 ${new Date(overview.checkedAt).toLocaleTimeString('zh-CN', { hour12: false })}` : '尚未获取数据' }}</span>
      <TimeRangeBar v-model:range="range" v-model:refresh="refresh" :loading="loading || trendLoading" @refresh="load" />
    </div>
    <ui-alert v-if="error" type="error" :title="error" :closable="false" show-icon />

    <section class="component-grid" aria-label="组件状态">
      <article v-for="item in overview?.components || []" :key="item.id" class="component-card">
        <header><strong>{{ item.name }}</strong><StatusDot :tone="componentTone[item.state] || 'neutral'" :label="componentText[item.state] || item.state" /></header>
        <p>{{ item.message || (item.version ? `版本 ${item.version}` : '') }}</p>
        <small v-if="item.id === 'prometheus' && item.details">采集目标 {{ item.details.targetsUp ?? '—' }} / {{ item.details.targetsTotal ?? '—' }} 正常</small>
        <small v-else-if="item.version && item.message">版本 {{ item.version }}</small>
      </article>
      <ui-skeleton v-if="!overview && loading" :rows="3" animated />
    </section>

    <ui-alert v-if="failingJobs.length" type="warning" :closable="false" show-icon :title="`有 ${failingJobs.length} 个采集任务异常：${failingJobs.map(j => j.job).join('、')}`">
      <ui-button v-permission="'menu:opsMetrics'" text @click="openTargets">查看采集目标</ui-button>
    </ui-alert>

    <section v-for="group in kpisByGroup" :key="group.id" class="kpi-section" :aria-label="group.title">
      <div class="section-heading"><h2>{{ group.title }}</h2><span>{{ group.desc }}</span></div>
      <div class="kpi-grid">
        <article v-for="kpi in group.items" :key="kpi.id" class="kpi-card" :class="`kpi-card--${kpiTone(kpi)}`">
          <span class="kpi-card__title" :title="kpi.description">{{ kpi.title }}</span>
          <strong>{{ kpi.value != null ? formatKpi(kpi.value, kpi.unit) : statusText[kpi.status] }}</strong>
          <StatusDot :tone="kpiTone(kpi)" :label="kpi.message || statusText[kpi.status]" />
          <div class="kpi-card__actions">
            <ui-button v-if="can('menu:opsMetrics') && kpi.status !== 'unconfigured'" text size="small" @click="openMetrics(kpi)"><LineChart />指标</ui-button>
            <ui-button v-if="kpi.logService && can('menu:opsLogs')" text size="small" @click="openLogs(kpi)"><FileSearch />日志</ui-button>
          </div>
        </article>
      </div>
    </section>

    <section class="kpi-section" aria-label="趋势">
      <div class="section-heading"><h2>趋势</h2><span>悬停同步查看各图同一时刻，拖选区域可放大全部图表的时间范围</span></div>
      <div class="trend-grid">
        <ui-card v-for="chart in trendCharts" :key="chart.id" shadow="never" class="surface-card">
          <template #header><div class="card-header"><strong>{{ chart.title }}</strong><Activity v-if="trendLoading" class="spin-icon" /></div></template>
          <p v-if="trends[chart.id]?.error" class="ops-muted">{{ trends[chart.id].error }}</p>
          <TimeSeriesChart :times="trends[chart.id]?.times || []" :series="trends[chart.id]?.series || []" :unit="chart.unit" :height="170" :legend="false" sync-key="ops-overview" @zoom="zoom" />
        </ui-card>
      </div>
    </section>
  </div>
</template>

<style scoped>
.ops-page { display: grid; grid-template-columns: minmax(0, 1fr); gap: var(--space-4); min-width: 0; }
.ops-toolbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: var(--space-3); }
.ops-toolbar__meta, .ops-muted { color: var(--text-muted); font-size: var(--font-size-xs); }
.component-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: var(--space-3); }
.component-card { display: grid; gap: 6px; padding: var(--space-4); background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-lg); box-shadow: var(--shadow-xs); }
.component-card header { display: flex; align-items: center; justify-content: space-between; gap: var(--space-2); }
.component-card p { min-height: 20px; margin: 0; color: var(--text-secondary); font-size: var(--font-size-sm); overflow-wrap: anywhere; }
.component-card small { color: var(--text-muted); font-size: var(--font-size-xs); }
.kpi-section { display: grid; gap: var(--space-3); min-width: 0; }
.section-heading { display: flex; align-items: baseline; flex-wrap: wrap; gap: var(--space-3); }
.section-heading h2 { margin: 0; color: var(--text-strong); font-size: var(--font-size-lg); font-weight: var(--font-weight-semibold); }
.section-heading span { color: var(--text-muted); font-size: var(--font-size-xs); }
.kpi-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(190px, 1fr)); gap: var(--space-3); }
.kpi-card { display: grid; align-content: start; gap: 6px; padding: var(--space-3) var(--space-4); background: var(--surface); border: 1px solid var(--border); border-left: 3px solid var(--border-strong); border-radius: var(--radius-lg); }
.kpi-card--success { border-left-color: var(--success); }
.kpi-card--warning { border-left-color: var(--warning); }
.kpi-card--danger { border-left-color: var(--danger); }
.kpi-card__title { color: var(--text-secondary); font-size: var(--font-size-sm); }
.kpi-card strong { color: var(--text-strong); font-size: var(--font-size-xl); font-weight: var(--font-weight-semibold); font-variant-numeric: tabular-nums; }
.kpi-card--danger strong { color: var(--danger-text); }
.kpi-card--warning strong { color: var(--warning-text); }
.kpi-card .status-dot { font-size: var(--font-size-xs); white-space: normal; }
.kpi-card__actions { display: flex; gap: var(--space-3); min-height: 24px; }
.trend-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(340px, 1fr)); gap: var(--space-3); }
.spin-icon { width: 14px; height: 14px; color: var(--text-muted); animation: spin 1.2s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (max-width: 767px) {
  .trend-grid { grid-template-columns: minmax(0, 1fr); }
  .kpi-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
</style>
