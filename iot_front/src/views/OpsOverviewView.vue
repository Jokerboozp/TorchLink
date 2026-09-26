<script setup>
// 运维总览：组件连接状态、平台已有指标与主机资源；区分未配置、采集失败、无样本和正常零值。
// 每个组件、每组指标和趋势图分别请求、各自显示，某个组件变慢或不可达不会拖住整页；
// 再次进入时先显示本次登录内上一次的结果，后台刷新完成后替换。
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { Activity, FileSearch, LineChart } from '@lucide/vue'
import { can } from '../permissions'
import { formatKpi } from '../ops/format.js'
import { metricResultToChart } from '../ops/frames.js'
import { isAbort, latest, opsErrorText, opsGet } from '../ops/opsApi.js'
import { overviewSnapshot } from '../ops/overviewCache.js'
import { resolveRange } from '../ops/timeRange.js'
import StatusDot from '../components/layout/StatusDot.vue'
import TimeRangeBar from '../components/ops/TimeRangeBar.vue'
import TimeSeriesChart from '../components/ops/TimeSeriesChart.vue'

const emit = defineEmits(['navigate'])
const range = ref({ from: 'now-3h', to: 'now' })
const refresh = ref(60e3)

const componentList = [
  { id: 'prometheus', name: 'Prometheus' },
  { id: 'loki', name: 'Loki' },
  { id: 'grafana', name: 'Grafana' },
  { id: 'alertmanager', name: 'Alertmanager' }
]
const groups = [
  { id: 'platform', title: '平台链路', desc: '上报、解析、队列与 AI 调用', size: 8 },
  { id: 'backup', title: '备份服务', desc: '最近成功备份与失败次数', size: 2 },
  { id: 'host', title: '主机资源', desc: 'node-exporter 采集的主机指标', size: 3 },
  { id: 'observability', title: '监控链路', desc: '日志接收与触发中的监控告警', size: 2 }
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
const componentTone = { ok: 'success', degraded: 'warning', down: 'danger', unconfigured: 'neutral', error: 'danger' }
const componentText = { ok: '正常', degraded: '部分异常', down: '无法连接', unconfigured: '未配置', error: '检查失败' }

const snapshot = overviewSnapshot()
const rangeKey = () => JSON.stringify(range.value)
const components = reactive({ ...snapshot.components })
const kpiGroups = reactive({ ...snapshot.kpiGroups })
const jobHealth = ref(snapshot.jobs)
const trends = reactive(snapshot.trendRange === rangeKey() ? { ...snapshot.trends } : {})
const checkedAt = ref(snapshot.checkedAt)
const loadingParts = reactive({})
// 平台未配置运维组件时各部分都会失败，只在页面顶部提示一次。
const notConfigured = ref('')
const unconfigured = e => e?.code === 'OPS_NOT_CONFIGURED' && Boolean(notConfigured.value = opsErrorText(e))
const loading = computed(() => Object.values(loadingParts).some(Boolean))
const trendLoading = computed(() => Boolean(loadingParts.trends))

// 同一部分只保留最新请求；被新请求替换的旧请求不改变加载状态和结果。
const runners = {}
const sequence = {}
async function loadPart(key, task, onData, onError) {
  const runner = (runners[key] ||= latest())
  const current = (sequence[key] = (sequence[key] || 0) + 1)
  loadingParts[key] = true
  try {
    onData(await runner.run(task))
  } catch (e) {
    if (!isAbort(e) && sequence[key] === current) onError(e)
  } finally {
    if (sequence[key] === current) loadingParts[key] = false
  }
}

function loadComponent({ id, name }) {
  return loadPart(`component:${id}`, signal => opsGet(`/api/v1/ops/overview/components/${id}`, {}, signal),
    data => { components[id] = data; snapshot.components[id] = data; notConfigured.value = '' },
    e => { components[id] = unconfigured(e) ? { id, name, state: 'unconfigured', message: '未配置' } : { id, name, state: 'error', message: opsErrorText(e) } })
}

function loadGroup({ id }) {
  return loadPart(`kpis:${id}`, signal => opsGet('/api/v1/ops/overview/kpis', { group: id }, signal),
    data => {
      kpiGroups[id] = { kpis: data.kpis || [] }
      snapshot.kpiGroups[id] = kpiGroups[id]
      jobHealth.value = snapshot.jobs = data.jobs || {}
      notConfigured.value = ''
    },
    e => { kpiGroups[id] = { kpis: kpiGroups[id]?.kpis || [], error: unconfigured(e) ? '' : opsErrorText(e) } })
}

function loadTrends() {
  const { from, to } = resolveRange(range.value)
  const key = rangeKey()
  return loadPart('trends', signal => opsGet('/api/v1/ops/overview/series', { ids: trendCharts.map(c => c.id).join(','), start: from, end: to, maxPoints: 300 }, signal),
    data => {
      for (const item of data.items || []) trends[item.id] = { ...metricResultToChart(item.result), error: item.error }
      snapshot.trends = { ...trends }
      snapshot.trendRange = key
    },
    e => { const error = unconfigured(e) ? '' : opsErrorText(e); for (const chart of trendCharts) trends[chart.id] = { times: [], series: [], error } })
}

function load() {
  Promise.all([...componentList.map(loadComponent), ...groups.map(loadGroup), loadTrends()]).then(() => {
    if (!loading.value) checkedAt.value = snapshot.checkedAt = Date.now()
  })
}

const kpisByGroup = computed(() => groups.map(group => ({ ...group, items: kpiGroups[group.id]?.kpis || [], error: kpiGroups[group.id]?.error || '', pending: !kpiGroups[group.id] && loadingParts[`kpis:${group.id}`] })))
const jobs = computed(() => Object.entries(jobHealth.value || {}).map(([job, health]) => ({ job, ...health })).sort((a, b) => a.job.localeCompare(b.job)))
const failingJobs = computed(() => jobs.value.filter(job => job.up < job.total))
const toolbarText = computed(() => {
  if (!checkedAt.value) return loading.value ? '正在检查组件与指标…' : '尚未获取数据'
  const time = new Date(checkedAt.value).toLocaleTimeString('zh-CN', { hour12: false })
  return loading.value ? `上次检查于 ${time}，正在刷新…` : `检查于 ${time}`
})

function kpiTone(kpi) {
  if (kpi.level === 'critical') return 'danger'
  if (kpi.level === 'warning') return 'warning'
  if (['scrape_failed', 'error'].includes(kpi.status)) return 'danger'
  if (['no_target', 'no_data', 'unconfigured'].includes(kpi.status)) return 'neutral'
  return 'success'
}

function zoom({ from, to }) { range.value = { from: Math.round(from), to: Math.round(to) } }
function openMetrics(kpi) { emit('navigate', 'opsMetrics', { query: kpi.expr, range: range.value }) }
function openLogs(kpi) { emit('navigate', 'opsLogs', { filter: { services: [kpi.logService], keyword: kpi.logKeyword || '' }, range: { from: 'now-1h', to: 'now' } }) }
function openTargets() { emit('navigate', 'opsMetrics', { tab: 'targets' }) }

watch(range, loadTrends, { deep: true })
onMounted(load)
onBeforeUnmount(() => { for (const runner of Object.values(runners)) runner.cancel() })
</script>

<template>
  <div class="ops-page">
    <div class="ops-toolbar">
      <span class="ops-toolbar__meta">{{ toolbarText }}</span>
      <TimeRangeBar v-model:range="range" v-model:refresh="refresh" :loading="loading" @refresh="load" />
    </div>
    <ui-alert v-if="notConfigured" type="error" :title="notConfigured" :closable="false" show-icon />

    <section class="component-grid" aria-label="组件状态">
      <article v-for="entry in componentList" :key="entry.id" class="component-card">
        <template v-if="components[entry.id]">
          <header><strong>{{ entry.name }}</strong><StatusDot :tone="componentTone[components[entry.id].state] || 'neutral'" :label="componentText[components[entry.id].state] || components[entry.id].state" /></header>
          <p>{{ components[entry.id].message || (components[entry.id].version ? `版本 ${components[entry.id].version}` : '') }}</p>
          <small v-if="entry.id === 'prometheus' && components[entry.id].details">采集目标 {{ components[entry.id].details.targetsUp ?? '—' }} / {{ components[entry.id].details.targetsTotal ?? '—' }} 正常</small>
          <small v-else-if="components[entry.id].version && components[entry.id].message">版本 {{ components[entry.id].version }}</small>
        </template>
        <template v-else>
          <header><strong>{{ entry.name }}</strong><StatusDot tone="neutral" label="检查中…" /></header>
          <ui-skeleton :rows="1" animated />
        </template>
      </article>
    </section>

    <ui-alert v-if="failingJobs.length" type="warning" :closable="false" show-icon :title="`有 ${failingJobs.length} 个采集任务异常：${failingJobs.map(j => j.job).join('、')}`">
      <ui-button v-permission="'menu:opsMetrics'" text @click="openTargets">查看采集目标</ui-button>
    </ui-alert>

    <section v-for="group in kpisByGroup" :key="group.id" class="kpi-section" :aria-label="group.title">
      <div class="section-heading"><h2>{{ group.title }}</h2><span>{{ group.desc }}</span></div>
      <p v-if="group.error" class="ops-muted">指标读取失败：{{ group.error }}</p>
      <div class="kpi-grid">
        <template v-if="group.pending">
          <article v-for="n in group.size" :key="n" class="kpi-card" aria-busy="true"><ui-skeleton :rows="2" animated /></article>
        </template>
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
