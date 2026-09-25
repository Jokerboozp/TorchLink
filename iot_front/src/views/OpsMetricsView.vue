<script setup>
// 指标中心：PromQL 查询（需单独授权）、结构化指标浏览、采集目标与规则。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { BookMarked, CheckCircle2, Play, Save, Square } from '@lucide/vue'
import { can } from '../permissions'
import { UiMessage } from '../ui/feedback.js'
import { formatValue, labelString, relativeTime } from '../ops/format.js'
import { metricResultToChart } from '../ops/frames.js'
import { latest, opsErrorText, opsGet, opsSend, takeNavigation } from '../ops/opsApi.js'
import { resolveRange } from '../ops/timeRange.js'
import StatusDot from '../components/layout/StatusDot.vue'
import MatcherEditor from '../components/ops/MatcherEditor.vue'
import QueryLibrary from '../components/ops/QueryLibrary.vue'
import RuleGroupsPanel from '../components/ops/RuleGroupsPanel.vue'
import TimeRangeBar from '../components/ops/TimeRangeBar.vue'
import TimeSeriesChart from '../components/ops/TimeSeriesChart.vue'

const emit = defineEmits(['navigate'])
const canQuery = computed(() => can('POST /api/v1/ops/metrics/query'))
const tab = ref(canQuery.value ? 'query' : 'explore')
const range = ref({ from: 'now-1h', to: 'now' })
const refresh = ref(0)

// PromQL 查询
const query = ref('')
const instant = ref(false)
const step = ref('')
const unit = ref('short')
const result = ref(null)
const queryError = ref('')
const running = ref(false)
const validation = ref(null)
const libraryVisible = ref(false)
const saveVisible = ref(false)
const saveForm = ref({ name: '', description: '' })
const runner = latest()
let validateTimer = null

const chart = computed(() => (result.value && !instant.value ? metricResultToChart(result.value) : { times: [], series: [] }))
const rows = computed(() => (result.value?.series || []).map((s, i) => ({ id: i, labels: s.labels, series: labelString(s.labels), value: s.values.length ? s.values[s.values.length - 1] : null, time: s.timestamps[s.timestamps.length - 1] })))

async function run() {
  if (!query.value.trim()) return
  const { from, to } = resolveRange(range.value)
  running.value = true
  queryError.value = ''
  try {
    result.value = await runner.run(signal => opsSend('POST', '/api/v1/ops/metrics/query', { query: query.value, instant: instant.value, time: to, start: from, end: to, stepMs: stepMs(), maxPoints: 1000 }, signal))
  } catch (e) {
    if (e?.name === 'AbortError') { queryError.value = e.message === 'stale' ? '' : '查询已取消'; return }
    queryError.value = opsErrorText(e)
    result.value = null
  } finally { running.value = false }
}
function stepMs() {
  const m = String(step.value || '').match(/^(\d+)(s|m|h)$/)
  return m ? Number(m[1]) * { s: 1e3, m: 60e3, h: 3600e3 }[m[2]] : 0
}
function cancel() { runner.cancel(); running.value = false; queryError.value = '查询已取消' }
watch(query, value => {
  clearTimeout(validateTimer)
  validation.value = null
  if (!value.trim()) return
  validateTimer = setTimeout(async () => {
    try { await opsGet('/api/v1/ops/metrics/validate', { query: value }); validation.value = { ok: true } } catch (e) { validation.value = { error: opsErrorText(e) } }
  }, 600)
})
function useQuery(body) { query.value = body.query || ''; tab.value = 'query'; run() }
async function saveQuery() {
  try {
    await opsSend('POST', '/api/v1/ops/preferences/saved-queries', { name: saveForm.value.name, description: saveForm.value.description, language: 'promql', query: query.value })
    UiMessage.success('查询已保存')
    saveVisible.value = false
  } catch (e) { UiMessage.error(opsErrorText(e)) }
}
function zoom({ from, to }) { range.value = { from: Math.round(from), to: Math.round(to) } }

// 指标浏览
const search = ref('')
const catalog = ref({ items: [], total: 0, truncated: false })
const catalogLoading = ref(false)
const selected = ref(null)
const metricLabels = ref([])
const matchers = ref([])
const fn = ref('raw')
const windowSize = ref('5m')
const groupBy = ref('')
const explore = ref(null)
const exploreError = ref('')
const exploring = ref(false)
const catalogRunner = latest()
const exploreRunner = latest()
let searchTimer = null
const fnOptions = [
  { value: 'raw', label: '原始值' }, { value: 'rate', label: '每秒增长率 rate' }, { value: 'increase', label: '窗口增量 increase' },
  { value: 'sum', label: '求和 sum' }, { value: 'avg', label: '平均 avg' }, { value: 'max', label: '最大 max' }, { value: 'sum_rate', label: '增长率求和 sum(rate)' }
]
const exploreChart = computed(() => (explore.value ? metricResultToChart(explore.value) : { times: [], series: [] }))

async function loadCatalog() {
  catalogLoading.value = true
  try { catalog.value = await catalogRunner.run(signal => opsGet('/api/v1/ops/metrics/catalog', { search: search.value, limit: 300 }, signal)) } catch (e) { if (e?.name !== 'AbortError') UiMessage.error(opsErrorText(e)) } finally { catalogLoading.value = false }
}
watch(search, () => { clearTimeout(searchTimer); searchTimer = setTimeout(loadCatalog, 300) })
async function pick(item) {
  selected.value = item
  matchers.value = []
  fn.value = item.type === 'counter' ? 'rate' : 'raw'
  groupBy.value = ''
  try { metricLabels.value = (await opsGet('/api/v1/ops/metrics/labels', { metric: item.name })).items.filter(l => l !== '__name__') } catch { metricLabels.value = [] }
  runExplore()
}
const loadLabelValues = async label => (await opsGet('/api/v1/ops/metrics/label-values', { label, metric: selected.value?.name })).items
async function runExplore() {
  if (!selected.value) return
  const { from, to } = resolveRange(range.value)
  exploring.value = true
  exploreError.value = ''
  try {
    explore.value = await exploreRunner.run(signal => opsGet('/api/v1/ops/metrics/explore', { metric: selected.value.name, matchers: matchers.value.filter(m => m.name), fn: fn.value, window: windowSize.value, by: ['sum', 'avg', 'max', 'sum_rate'].includes(fn.value) ? groupBy.value : '', start: from, end: to, maxPoints: 600 }, signal))
  } catch (e) { if (e?.name !== 'AbortError') { exploreError.value = opsErrorText(e); explore.value = null } } finally { exploring.value = false }
}
function openInQuery() { query.value = explore.value?.query || ''; tab.value = 'query'; run() }

// 采集目标
const targets = ref([])
const targetFilter = ref('')
const targetsLoading = ref(false)
const targetsError = ref('')
const filteredTargets = computed(() => targets.value.filter(t => !targetFilter.value || (targetFilter.value === 'down' ? t.health !== 'up' : t.job === targetFilter.value)))
const jobs = computed(() => [...new Set(targets.value.map(t => t.job))])
async function loadTargets() {
  targetsLoading.value = true
  try { targets.value = (await opsGet('/api/v1/ops/metrics/targets')).items || []; targetsError.value = '' } catch (e) { targetsError.value = opsErrorText(e) } finally { targetsLoading.value = false }
}

function refreshCurrent() {
  if (tab.value === 'query') run()
  else if (tab.value === 'explore') runExplore()
  else if (tab.value === 'targets') loadTargets()
}
watch(range, () => refreshCurrent(), { deep: true })
watch(tab, value => { if (value === 'targets' && !targets.value.length) loadTargets(); if (value === 'explore' && !catalog.value.items.length) loadCatalog() })

onMounted(() => {
  const nav = takeNavigation()
  if (nav?.range) range.value = nav.range
  if (nav?.tab) tab.value = nav.tab
  if (nav?.query) {
    if (canQuery.value) { query.value = nav.query; tab.value = 'query'; run() } else { tab.value = 'explore' }
  }
  if (tab.value === 'explore') loadCatalog()
  if (tab.value === 'targets') loadTargets()
})
onBeforeUnmount(() => { runner.cancel(); exploreRunner.cancel(); catalogRunner.cancel(); clearTimeout(validateTimer); clearTimeout(searchTimer) })
</script>

<template>
  <div class="ops-page">
    <ui-tabs v-model="tab">
      <ui-tab-pane v-if="canQuery" name="query" label="PromQL 查询">
        <div class="query-panel">
          <div class="query-toolbar">
            <ui-radio-group v-model="instant" size="small" class="segmented-choice-group" aria-label="查询类型"><ui-radio-button :value="false">范围查询</ui-radio-button><ui-radio-button :value="true">即时查询</ui-radio-button></ui-radio-group>
            <TimeRangeBar v-model:range="range" v-model:refresh="refresh" :loading="running" :max-hours="24 * 31" @refresh="run" />
          </div>
          <ui-input v-model="query" type="textarea" :autosize="{ minRows: 3, maxRows: 10 }" class="mono-input" placeholder="输入 PromQL，例如 sum by (job) (rate(raw_archive_success_total[5m]))" aria-label="PromQL 查询语句" @keydown.ctrl.enter="run" @keydown.meta.enter="run" />
          <div class="query-actions">
            <div class="query-actions__left">
              <ui-button type="primary" :loading="running" :disabled="!query.trim()" @click="run"><Play />执行（Ctrl+Enter）</ui-button>
              <ui-button v-if="running" @click="cancel"><Square />取消</ui-button>
              <ui-input v-if="!instant" v-model="step" size="small" class="step-input" placeholder="步长：自动" aria-label="步长" />
              <ui-select v-model="unit" size="small" class="unit-select" aria-label="显示单位">
                <ui-option value="short" label="数值" /><ui-option value="percent" label="百分比" /><ui-option value="bytes" label="字节" /><ui-option value="s" label="秒" /><ui-option value="ms" label="毫秒" />
              </ui-select>
              <span v-if="validation?.ok" class="check-ok"><CheckCircle2 />语法正确</span>
              <span v-else-if="validation?.error" class="check-error">{{ validation.error }}</span>
            </div>
            <div class="query-actions__right">
              <ui-button size="small" @click="libraryVisible = true"><BookMarked />我的查询</ui-button>
              <ui-button size="small" :disabled="!query.trim()" @click="saveForm = { name: '', description: '' }; saveVisible = true"><Save />保存</ui-button>
            </div>
          </div>
          <ui-alert v-if="queryError" type="error" :title="queryError" :closable="false" show-icon />
          <template v-if="result">
            <div class="result-meta">
              <span>{{ result.series.length }} 条序列<template v-if="result.stepMs"> · 步长 {{ result.stepMs / 1000 }} 秒</template></span>
              <ui-tag v-if="result.truncated" type="warning" size="small">结果已截断，只显示前 {{ result.limit }} 条序列</ui-tag>
              <ui-tag v-for="w in result.warnings || []" :key="w" type="warning" size="small">{{ w }}</ui-tag>
            </div>
            <ui-card v-if="!instant" shadow="never" class="surface-card"><TimeSeriesChart :times="chart.times" :series="chart.series" :unit="unit" :height="300" sync-key="ops-metrics" @zoom="zoom" /></ui-card>
            <ui-table :data="rows" size="small" row-key="id" max-height="360" empty-text="没有返回序列">
              <ui-table-column label="序列" min-width="360"><template #default="{ row }"><code class="series-code">{{ row.series }}</code></template></ui-table-column>
              <ui-table-column label="最新值" width="160" align="right"><template #default="{ row }">{{ formatValue(row.value, unit) }}</template></ui-table-column>
              <ui-table-column label="时间" width="180"><template #default="{ row }">{{ row.time ? new Date(row.time).toLocaleString('zh-CN', { hour12: false }) : '—' }}</template></ui-table-column>
            </ui-table>
          </template>
          <ui-empty v-else-if="!queryError && !running" description="输入 PromQL 后执行查询" :image-size="64" />
        </div>
      </ui-tab-pane>

      <ui-tab-pane name="explore" label="指标浏览">
        <div class="explore">
          <aside class="explore__list">
            <ui-input v-model="search" size="small" clearable placeholder="搜索指标名或说明" aria-label="搜索指标" />
            <p class="muted">{{ catalogLoading ? '正在读取…' : `共 ${catalog.total} 个指标${catalog.truncated ? `，显示前 ${catalog.items.length} 个` : ''}` }}</p>
            <div class="metric-list" role="listbox" aria-label="指标列表">
              <button v-for="item in catalog.items" :key="item.name" type="button" class="metric-item" :class="{ 'is-active': selected?.name === item.name }" role="option" :aria-selected="selected?.name === item.name" @click="pick(item)">
                <strong>{{ item.name }}</strong><small>{{ item.type || '未知类型' }}{{ item.help ? ` · ${item.help}` : '' }}</small>
              </button>
            </div>
          </aside>
          <section class="explore__main">
            <ui-empty v-if="!selected" description="从左侧选择一个指标" :image-size="64" />
            <template v-else>
              <div class="explore__head"><div><strong>{{ selected.name }}</strong><p class="muted">{{ selected.help || '无说明' }}</p></div><ui-button v-if="canQuery && explore" size="small" @click="openInQuery">在 PromQL 中打开</ui-button></div>
              <MatcherEditor v-model="matchers" :labels="metricLabels" :load-values="loadLabelValues" />
              <div class="explore__controls">
                <ui-select v-model="fn" size="small" aria-label="计算方式"><ui-option v-for="item in fnOptions" :key="item.value" :value="item.value" :label="item.label" /></ui-select>
                <ui-select v-if="fn !== 'raw' && fn !== 'sum' && fn !== 'avg' && fn !== 'max'" v-model="windowSize" size="small" aria-label="时间窗口"><ui-option value="1m" label="1 分钟窗口" /><ui-option value="5m" label="5 分钟窗口" /><ui-option value="15m" label="15 分钟窗口" /><ui-option value="1h" label="1 小时窗口" /></ui-select>
                <ui-select v-if="['sum', 'avg', 'max', 'sum_rate'].includes(fn)" v-model="groupBy" size="small" clearable placeholder="按标签分组（可选）" aria-label="分组标签"><ui-option v-for="label in metricLabels" :key="label" :value="label" :label="label" /></ui-select>
                <TimeRangeBar v-model:range="range" :show-refresh="false" :loading="exploring" @refresh="runExplore" />
                <ui-button type="primary" size="small" :loading="exploring" @click="runExplore">查看</ui-button>
              </div>
              <ui-alert v-if="exploreError" type="error" :title="exploreError" :closable="false" />
              <code v-if="explore" class="series-code">{{ explore.query }}</code>
              <ui-tag v-if="explore?.truncated" type="warning" size="small">结果已截断，只显示前 {{ explore.limit }} 条序列</ui-tag>
              <TimeSeriesChart :times="exploreChart.times" :series="exploreChart.series" :height="300" sync-key="ops-metrics" @zoom="zoom" />
            </template>
          </section>
        </div>
      </ui-tab-pane>

      <ui-tab-pane name="targets" label="采集目标">
        <div class="targets-toolbar">
          <ui-select v-model="targetFilter" size="small" clearable placeholder="全部采集任务" aria-label="筛选采集目标">
            <ui-option value="down" label="只看异常" />
            <ui-option v-for="job in jobs" :key="job" :value="job" :label="job" />
          </ui-select>
          <ui-button size="small" :loading="targetsLoading" @click="loadTargets">刷新</ui-button>
        </div>
        <ui-alert v-if="targetsError" type="error" :title="targetsError" :closable="false" />
        <ui-table :data="filteredTargets" size="small" :row-key="row => `${row.job}/${row.instance}`" empty-text="暂无采集目标">
          <ui-table-column prop="job" label="任务" width="150" />
          <ui-table-column prop="instance" label="实例" min-width="180" show-overflow-tooltip />
          <ui-table-column label="状态" width="100"><template #default="{ row }"><StatusDot :tone="row.health === 'up' ? 'success' : row.health === 'down' ? 'danger' : 'neutral'" :label="row.health === 'up' ? '正常' : row.health === 'down' ? '失败' : '未知'" /></template></ui-table-column>
          <ui-table-column label="最后抓取" width="130"><template #default="{ row }">{{ row.lastScrape && !row.lastScrape.startsWith('0001') ? relativeTime(row.lastScrape) : '—' }}</template></ui-table-column>
          <ui-table-column label="耗时" width="100"><template #default="{ row }">{{ formatValue(row.lastScrapeDuration, 's') }}</template></ui-table-column>
          <ui-table-column label="间隔" width="80"><template #default="{ row }">{{ row.scrapeInterval || '—' }}</template></ui-table-column>
          <ui-table-column label="错误" min-width="260"><template #default="{ row }"><span class="target-error">{{ row.lastError || '—' }}</span></template></ui-table-column>
        </ui-table>
      </ui-tab-pane>

      <ui-tab-pane name="rules" label="记录与告警规则">
        <RuleGroupsPanel source="prometheus" />
      </ui-tab-pane>
    </ui-tabs>

    <QueryLibrary v-model="libraryVisible" :languages="['promql']" @use="useQuery" />
    <ui-dialog v-model="saveVisible" title="保存查询" width="min(480px, 94vw)">
      <div class="save-form"><label>名称<ui-input v-model="saveForm.name" maxlength="100" /></label><label>说明<ui-input v-model="saveForm.description" type="textarea" :rows="2" maxlength="500" /></label><code class="series-code">{{ query }}</code></div>
      <template #footer><ui-button @click="saveVisible = false">取消</ui-button><ui-button type="primary" :disabled="!saveForm.name.trim()" @click="saveQuery">保存</ui-button></template>
    </ui-dialog>
  </div>
</template>

<style scoped>
.ops-page { display: grid; grid-template-columns: minmax(0, 1fr); gap: var(--space-4); min-width: 0; }
.query-panel, .explore__main { display: grid; gap: var(--space-3); min-width: 0; }
.query-toolbar, .query-actions, .targets-toolbar, .explore__head { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: var(--space-2); }
.query-actions__left, .query-actions__right, .explore__controls { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-2); }
.step-input { width: 110px; }
.unit-select { width: 100px; }
.mono-input :deep(textarea) { font-family: var(--font-mono); font-size: var(--font-size-sm); }
.check-ok { display: inline-flex; align-items: center; gap: 4px; color: var(--success-text); font-size: var(--font-size-xs); }
.check-ok svg { width: 14px; height: 14px; }
.check-error { max-width: 520px; color: var(--danger-text); font-size: var(--font-size-xs); overflow-wrap: anywhere; }
.result-meta { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-2); color: var(--text-muted); font-size: var(--font-size-xs); }
.series-code { display: block; color: var(--code-inline-text); font: var(--font-size-xs) var(--font-mono); white-space: pre-wrap; word-break: break-all; }
.explore { display: grid; grid-template-columns: 300px minmax(0, 1fr); gap: var(--space-4); min-width: 0; }
.explore__list { display: grid; align-content: start; gap: var(--space-2); min-width: 0; }
.metric-list { display: grid; max-height: 560px; overflow: auto; border: 1px solid var(--border); border-radius: var(--radius-md); background: var(--surface); }
.metric-item { display: grid; gap: 2px; padding: var(--space-2) var(--space-3); color: var(--text); text-align: left; background: none; border: 0; border-bottom: 1px solid var(--border); cursor: pointer; }
.metric-item:hover { background: var(--surface-hover); }
.metric-item.is-active { background: var(--primary-soft); }
.metric-item strong { font: var(--font-size-sm) var(--font-mono); word-break: break-all; }
.metric-item small, .muted { margin: 0; overflow: hidden; color: var(--text-muted); font-size: var(--font-size-xs); text-overflow: ellipsis; white-space: nowrap; }
.explore__controls .ui-select { width: 170px; }
.targets-toolbar .ui-select { width: 200px; }
.target-error { color: var(--danger-text); font-size: var(--font-size-xs); overflow-wrap: anywhere; }
.save-form { display: grid; gap: var(--space-3); }
.save-form label { display: grid; gap: 6px; color: var(--text-secondary); font-size: var(--font-size-sm); }
@media (max-width: 900px) {
  .explore { grid-template-columns: minmax(0, 1fr); }
  .metric-list { max-height: 240px; }
}
</style>
