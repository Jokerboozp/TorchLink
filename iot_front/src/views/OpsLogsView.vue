<script setup>
// 日志中心：结构化筛选（查看权限即可使用）与 LogQL（需单独授权）、日志量分布、实时追踪、
// 上下文、有限导出；以及日志告警规则、保留策略和删除请求。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { BookMarked, CheckCircle2, Download, Pause, Play, Radio, Save, Search, Square } from '@lucide/vue'
import { can } from '../permissions'
import { UiMessage } from '../ui/feedback.js'
import { formatValue, labelString, logLevelName, logLevelTone } from '../ops/format.js'
import { metricResultToChart } from '../ops/frames.js'
import { exportLogs, isAbort, latest, opsErrorText, opsGet, opsSend, tailLogs, takeNavigation } from '../ops/opsApi.js'
import { rangeDuration, resolveRange } from '../ops/timeRange.js'
import LogContextDrawer from '../components/ops/LogContextDrawer.vue'
import LogList from '../components/ops/LogList.vue'
import LogRetentionPanel from '../components/ops/LogRetentionPanel.vue'
import MatcherEditor from '../components/ops/MatcherEditor.vue'
import QueryLibrary from '../components/ops/QueryLibrary.vue'
import RuleGroupsPanel from '../components/ops/RuleGroupsPanel.vue'
import TimeRangeBar from '../components/ops/TimeRangeBar.vue'
import TimeSeriesChart from '../components/ops/TimeSeriesChart.vue'

const PAGE = 200
const TAIL_KEEP = 1000
const canRaw = computed(() => can('POST /api/v1/ops/logs/query'))
const canExport = computed(() => can('POST /api/v1/ops/logs/export'))
const tab = ref('search')
const mode = ref('filter')
const range = ref({ from: 'now-1h', to: 'now' })
const refresh = ref(0)
const filter = ref(emptyFilter())
const logql = ref('')
const levelOptions = ['error', 'warn', 'info', 'debug', 'fatal', 'unknown']

function emptyFilter() { return { services: [], levels: [], labels: [], keyword: '', regex: false, exclude: '' } }

// 候选项
const labelNames = ref([])
const services = ref([])
const extraLabels = computed(() => labelNames.value.filter(name => !['service_name', 'level'].includes(name)))
async function loadOptions() {
  try {
    const [labels, values] = await Promise.all([opsGet('/api/v1/ops/logs/labels'), opsGet('/api/v1/ops/logs/label-values', { label: 'service_name' })])
    labelNames.value = labels.items || []
    services.value = values.items || []
  } catch { /* 候选项失败不影响手工输入，查询本身会给出错误。 */ }
}
const loadLabelValues = async label => (await opsGet('/api/v1/ops/logs/label-values', { label })).items || []

// 查询
const entries = ref([])
const result = ref(null)
const metric = ref(null)
const volume = ref(null)
const volumeError = ref('')
const error = ref('')
const loading = ref(false)
const loadingMore = ref(false)
const searched = ref(null)
const wrap = ref(true)
const runner = latest()
const moreRunner = latest()
const volumeRunner = latest()
const wideRange = computed(() => rangeDuration(range.value) > 24 * 3600e3)
const cleanFilter = () => ({ ...filter.value, labels: filter.value.labels.filter(m => m.name) })
const entryKey = e => `${e.ts}\u0000${e.line}\u0000${JSON.stringify(e.labels)}`

async function run() {
  if (mode.value === 'logql' && !logql.value.trim()) return
  stopTail()
  moreRunner.cancel()
  const { from, to } = resolveRange(range.value)
  searched.value = mode.value === 'logql' ? { mode: 'logql', query: logql.value.trim(), from, to } : { mode: 'filter', filter: cleanFilter(), from, to }
  loading.value = true
  error.value = ''
  if (searched.value.mode === 'filter') loadVolume()
  else volume.value = null
  try {
    const data = await runner.run(signal => {
      const { from: start, to: end } = searched.value
      if (searched.value.mode === 'logql') return opsSend('POST', '/api/v1/ops/logs/query', { query: searched.value.query, start, end, limit: PAGE, maxPoints: 600 }, signal)
      return opsGet('/api/v1/ops/logs/search', { filter: searched.value.filter, start, end, limit: PAGE }, signal)
    })
    result.value = data
    if (data.resultType === 'streams') { entries.value = data.entries || []; metric.value = null } else { entries.value = []; metric.value = data }
  } catch (e) {
    if (isAbort(e)) { if (e.message !== 'stale') error.value = '查询已取消'; return }
    error.value = opsErrorText(e)
    result.value = null
    entries.value = []
    metric.value = null
  } finally { loading.value = false }
}

async function loadMore() {
  if (!result.value?.nextCursor) return
  loadingMore.value = true
  try {
    const cursor = result.value.nextCursor
    const data = await moreRunner.run(signal => {
      const { from: start, to: end } = searched.value
      if (searched.value.mode === 'logql') return opsSend('POST', '/api/v1/ops/logs/query', { query: searched.value.query, start, end, limit: PAGE, cursor }, signal)
      return opsGet('/api/v1/ops/logs/search', { filter: searched.value.filter, start, end, limit: PAGE, cursor }, signal)
    })
    const seen = new Set(entries.value.map(entryKey))
    entries.value = [...entries.value, ...(data.entries || []).filter(e => !seen.has(entryKey(e)))]
    result.value = { ...data, entries: undefined }
  } catch (e) { if (!isAbort(e)) UiMessage.error(opsErrorText(e)) } finally { loadingMore.value = false }
}

async function loadVolume() {
  volumeError.value = ''
  try {
    const { from, to } = searched.value
    const data = await volumeRunner.run(signal => opsGet('/api/v1/ops/logs/volume', { filter: searched.value.filter, start: from, end: to, maxPoints: 120 }, signal))
    const chart = metricResultToChart(data)
    const order = ['fatal', 'error', 'warn', 'info', 'debug', 'unknown']
    const tone = { danger: '--danger', warning: '--warning', info: '--info', neutral: '--neutral-dot' }
    chart.series = chart.series
      .map(s => { const level = s.labels?.level || 'unknown'; return { ...s, level, name: logLevelName[level] || level, color: tone[logLevelTone[level]] || '--neutral-dot' } })
      .sort((a, b) => order.indexOf(a.level) - order.indexOf(b.level))
    volume.value = chart
  } catch (e) { if (!isAbort(e)) { volume.value = null; volumeError.value = opsErrorText(e) } }
}

function cancel() { runner.cancel(); volumeRunner.cancel(); loading.value = false; error.value = '查询已取消' }
function zoom({ from, to }) { range.value = { from: Math.round(from), to: Math.round(to) } }
function resetFilter() { filter.value = emptyFilter(); run() }
const metricChart = computed(() => (metric.value?.resultType === 'matrix' ? metricResultToChart(metric.value) : { times: [], series: [] }))
const metricRows = computed(() => (metric.value?.series || []).map((s, i) => ({ id: i, series: labelString(s.labels), value: s.values.length ? s.values[s.values.length - 1] : null })))

// LogQL 语法检查
const validation = ref(null)
let validateTimer = null
watch(logql, value => {
  clearTimeout(validateTimer)
  validation.value = null
  if (!value.trim()) return
  validateTimer = setTimeout(async () => {
    try { await opsGet('/api/v1/ops/logs/validate', { query: value }); validation.value = { ok: true } } catch (e) { validation.value = { error: opsErrorText(e) } }
  }, 600)
})
// 切换到 LogQL 时带入当前筛选条件生成的语句，便于在此基础上修改。
watch(mode, value => { if (value === 'logql' && !logql.value.trim() && searched.value?.mode === 'filter' && result.value?.query) logql.value = result.value.query })

// 实时追踪：批量合并 SSE 分片后再更新列表，断线按退避重连，服务端 10 分钟结束后自动续接。
const tailing = ref(false)
const tailState = ref('')
const tailError = ref('')
const paused = ref(false)
const dropped = ref(0)
const pending = ref(0)
let tailController = null
let restartTimer = null
let buffer = []
let flushTimer = null
let tailSince = ''
const recent = new Set()

function tailParams() {
  const base = { since: tailSince }
  return mode.value === 'logql' ? { ...base, query: logql.value.trim() } : { ...base, filter: cleanFilter() }
}
function flush() {
  flushTimer = null
  if (paused.value || !buffer.length) return
  const incoming = buffer.sort((a, b) => (BigInt(b.ts) > BigInt(a.ts) ? 1 : -1))
  buffer = []
  pending.value = 0
  entries.value = [...incoming, ...entries.value].slice(0, TAIL_KEEP)
}
function onTail(event) {
  if (event.type === 'ready') { tailState.value = 'live'; return }
  if (event.type === 'error') throw Object.assign(new Error(event.body?.detail || '实时日志中断'), { status: event.status, code: event.body?.code })
  if (event.type !== 'entries') return
  dropped.value += event.dropped || 0
  for (const entry of event.entries || []) {
    const key = entryKey(entry)
    if (recent.has(key)) continue
    recent.add(key)
    if (recent.size > 5000) recent.delete(recent.values().next().value)
    buffer.push(entry)
    if (!tailSince || BigInt(entry.ts) > BigInt(tailSince)) tailSince = entry.ts
  }
  if (buffer.length > TAIL_KEEP) buffer = buffer.slice(-TAIL_KEEP)
  pending.value = buffer.length
  if (!flushTimer) flushTimer = setTimeout(flush, 300)
}
async function startTail() {
  if (mode.value === 'logql' && !logql.value.trim()) return
  runner.cancel(); volumeRunner.cancel(); moreRunner.cancel()
  stopTail()
  const controller = new AbortController()
  tailController = controller
  entries.value = []
  result.value = null
  metric.value = null
  volume.value = null
  error.value = ''
  tailError.value = ''
  dropped.value = 0
  paused.value = false
  tailSince = ''
  recent.clear()
  tailing.value = true
  let failures = 0
  while (!controller.signal.aborted) {
    tailState.value = tailSince ? 'reconnecting' : 'connecting'
    try {
      await tailLogs(tailParams(), onTail, controller.signal)
      failures = 0
    } catch (e) {
      if (controller.signal.aborted || isAbort(e)) break
      if (e.status && e.status < 500) { tailError.value = opsErrorText(e); break }
      if (++failures > 3) { tailError.value = `实时日志连接多次失败：${opsErrorText(e)}`; break }
    }
    await new Promise(resolve => setTimeout(resolve, 1000 * Math.min(2 ** failures, 10)))
  }
  if (tailController === controller) { tailing.value = false; tailState.value = '' }
}
function stopTail() {
  tailController?.abort()
  tailController = null
  tailing.value = false
  tailState.value = ''
  clearTimeout(flushTimer)
  clearTimeout(restartTimer)
  flushTimer = null
  buffer = []
  pending.value = 0
}
function togglePause() { paused.value = !paused.value; if (!paused.value) flush() }
// 追踪中修改条件时稍等输入完成再按新条件重新连接。
watch([mode, filter, logql], () => {
  if (!tailing.value) return
  clearTimeout(restartTimer)
  restartTimer = setTimeout(startTail, 800)
}, { deep: true })
const tailText = { connecting: '正在连接…', reconnecting: '连接中断，正在重连…', live: '实时接收中' }

// 导出、保存、历史
const exporting = ref(false)
async function doExport(format) {
  if (!searched.value) { UiMessage.warning('请先执行查询'); return }
  exporting.value = true
  try {
    const body = searched.value.mode === 'logql' ? { query: searched.value.query } : { filter: searched.value.filter }
    const out = await exportLogs({ ...body, start: searched.value.from, end: searched.value.to, format })
    if (out.truncated) UiMessage.warning(`已导出 ${out.lines} 行，达到导出上限，较早的日志未包含，请缩小时间范围`)
    else UiMessage.success(`已导出 ${out.lines} 行`)
  } catch (e) { UiMessage.error(opsErrorText(e)) } finally { exporting.value = false }
}
const libraryVisible = ref(false)
const saveVisible = ref(false)
const saveForm = ref({ name: '', description: '' })
async function saveQuery() {
  const body = mode.value === 'logql' ? { language: 'logql', query: logql.value } : { language: 'logfilter', filter: cleanFilter() }
  try {
    await opsSend('POST', '/api/v1/ops/preferences/saved-queries', { name: saveForm.value.name, description: saveForm.value.description, ...body })
    UiMessage.success('查询已保存')
    saveVisible.value = false
  } catch (e) { UiMessage.error(opsErrorText(e)) }
}
function useQuery(body) {
  if (body.language === 'logql') { if (!canRaw.value) return; logql.value = body.query || ''; mode.value = 'logql' } else { filter.value = { ...emptyFilter(), ...(body.filter || {}) }; mode.value = 'filter' }
  tab.value = 'search'
  run()
}

// 上下文
const contextVisible = ref(false)
const contextEntry = ref(null)
function openContext(entry) { contextEntry.value = entry; contextVisible.value = true }

watch(range, () => { if (tab.value === 'search' && !tailing.value && searched.value) run() }, { deep: true })
onMounted(() => {
  const nav = takeNavigation()
  if (nav?.range) range.value = nav.range
  if (nav?.tab) tab.value = nav.tab
  if (nav?.filter) filter.value = { ...emptyFilter(), ...nav.filter, services: (nav.filter.services || []).filter(Boolean) }
  if (nav?.query && canRaw.value) { logql.value = nav.query; mode.value = 'logql' }
  loadOptions()
  if (tab.value === 'search') run()
})
onBeforeUnmount(() => { stopTail(); runner.cancel(); moreRunner.cancel(); volumeRunner.cancel(); clearTimeout(validateTimer) })
</script>

<template>
  <div class="ops-page">
    <ui-tabs v-model="tab">
      <ui-tab-pane name="search" label="日志查询">
        <div class="log-search">
          <div class="log-toolbar">
            <ui-radio-group v-if="canRaw" v-model="mode" size="small" class="segmented-choice-group" aria-label="查询方式">
              <ui-radio-button value="filter">条件筛选</ui-radio-button><ui-radio-button value="logql">LogQL</ui-radio-button>
            </ui-radio-group>
            <TimeRangeBar v-model:range="range" v-model:refresh="refresh" :loading="loading" :max-hours="24 * 7" :show-refresh="!tailing" @refresh="run" />
          </div>

          <div v-if="mode === 'filter'" class="log-filter">
            <ui-select v-model="filter.services" multiple filterable clearable collapse-tags placeholder="全部服务" aria-label="服务"><ui-option v-for="item in services" :key="item" :value="item" :label="item" /></ui-select>
            <ui-select v-model="filter.levels" multiple clearable placeholder="全部级别" aria-label="日志级别"><ui-option v-for="level in levelOptions" :key="level" :value="level" :label="logLevelName[level]" /></ui-select>
            <ui-input v-model="filter.keyword" clearable :placeholder="filter.regex ? '正则表达式（RE2）' : '包含关键词'" aria-label="关键词" @keydown.enter="run" />
            <ui-checkbox v-model="filter.regex">正则</ui-checkbox>
            <ui-input v-model="filter.exclude" clearable placeholder="排除包含…的行" aria-label="排除关键词" @keydown.enter="run" />
            <div class="log-filter__labels"><MatcherEditor v-model="filter.labels" :labels="extraLabels" :load-values="loadLabelValues" add-text="添加标签条件" :max="8" /></div>
          </div>
          <template v-else>
            <ui-input v-model="logql" type="textarea" :autosize="{ minRows: 2, maxRows: 8 }" class="mono-input" placeholder='输入 LogQL，例如 {service_name="platform-api"} |= "error" 或 sum by (level) (count_over_time({service_name=~".+"}[5m]))' aria-label="LogQL 查询语句" @keydown.ctrl.enter="run" @keydown.meta.enter="run" />
            <span v-if="validation?.ok" class="check-ok"><CheckCircle2 />语法正确</span>
            <span v-else-if="validation?.error" class="check-error">{{ validation.error }}</span>
          </template>

          <div class="log-actions">
            <div class="log-actions__left">
              <ui-button type="primary" :loading="loading" :disabled="tailing" @click="run"><Search />查询</ui-button>
              <ui-button v-if="loading" @click="cancel"><Square />取消</ui-button>
              <ui-button v-if="!tailing" @click="startTail"><Radio />实时追踪</ui-button>
              <template v-else>
                <ui-button type="warning" plain @click="stopTail"><Square />停止追踪</ui-button>
                <ui-button @click="togglePause"><component :is="paused ? Play : Pause" />{{ paused ? '继续显示' : '暂停显示' }}</ui-button>
              </template>
              <ui-button v-if="mode === 'filter'" text size="small" @click="resetFilter">清空条件</ui-button>
            </div>
            <div class="log-actions__right">
              <ui-checkbox v-model="wrap">自动换行</ui-checkbox>
              <ui-dropdown v-if="canExport" @command="doExport">
                <ui-button size="small" :loading="exporting" :disabled="!searched || tailing"><Download />导出</ui-button>
                <template #dropdown><ui-dropdown-menu><ui-dropdown-item command="jsonl">JSON Lines</ui-dropdown-item><ui-dropdown-item command="csv">CSV</ui-dropdown-item><ui-dropdown-item command="txt">纯文本</ui-dropdown-item></ui-dropdown-menu></template>
              </ui-dropdown>
              <ui-button size="small" @click="libraryVisible = true"><BookMarked />我的查询</ui-button>
              <ui-button size="small" :disabled="mode === 'logql' && !logql.trim()" @click="saveForm = { name: '', description: '' }; saveVisible = true"><Save />保存</ui-button>
            </div>
          </div>

          <ui-alert v-if="wideRange && !tailing" type="info" :closable="false" title="时间范围超过 24 小时，查询可能较慢；建议增加服务、级别或关键词条件。" />
          <ui-alert v-if="error" type="error" :title="error" :closable="false" show-icon />
          <ui-alert v-if="tailError" type="error" :title="tailError" :closable="false" show-icon />

          <div v-if="tailing" class="tail-status">
            <span class="tail-status__dot" :class="{ 'is-live': tailState === 'live' && !paused }" />
            <span>{{ paused ? `已暂停显示，暂存 ${pending} 行新日志` : tailText[tailState] || '' }}</span>
            <span>· 保留最近 {{ TAIL_KEEP }} 行</span>
            <span v-if="dropped">· Loki 丢弃 {{ dropped }} 行（速率过高）</span>
          </div>

          <ui-card v-if="volume && !tailing" shadow="never" class="surface-card volume-card">
            <template #header><div class="card-header"><strong>日志量分布</strong><small>按级别统计匹配的行数，拖选可缩小时间范围</small></div></template>
            <TimeSeriesChart :times="volume.times" :series="volume.series" :height="120" bars unit="short" empty-text="所选时间范围内没有匹配的日志" @zoom="zoom" />
          </ui-card>
          <p v-else-if="volumeError && !tailing" class="muted">日志量统计失败：{{ volumeError }}</p>

          <template v-if="metric">
            <div class="result-meta"><span>统计查询返回 {{ metric.series?.length || 0 }} 条序列</span><ui-tag v-if="metric.truncated" type="warning" size="small">结果已截断</ui-tag></div>
            <ui-card v-if="metric.resultType === 'matrix'" shadow="never" class="surface-card"><TimeSeriesChart :times="metricChart.times" :series="metricChart.series" :height="280" @zoom="zoom" /></ui-card>
            <ui-table :data="metricRows" size="small" row-key="id" max-height="320" empty-text="没有返回序列">
              <ui-table-column label="序列" min-width="320"><template #default="{ row }"><code class="query-code">{{ row.series }}</code></template></ui-table-column>
              <ui-table-column label="最新值" width="160" align="right"><template #default="{ row }">{{ formatValue(row.value) }}</template></ui-table-column>
            </ui-table>
          </template>

          <template v-else-if="entries.length || tailing">
            <div v-if="result" class="result-meta">
              <span>已显示 {{ entries.length }} 行（最新在前）</span>
              <ui-tag v-if="result.nextCursor" size="small">还有更早的日志</ui-tag>
              <span v-if="result.stats?.totalBytesProcessed">· 扫描 {{ formatValue(result.stats.totalBytesProcessed, 'bytes') }}</span>
              <span v-if="result.stats?.execTime">· 用时 {{ formatValue(result.stats.execTime, 's') }}</span>
              <code v-if="result.query" class="query-code query-code--inline" :title="result.query">{{ result.query }}</code>
            </div>
            <LogList :entries="entries" :wrap="wrap" :highlight="mode === 'filter' && !filter.regex ? filter.keyword : ''" max-height="calc(100vh - 360px)" @context="openContext" />
            <div v-if="result?.nextCursor && !tailing" class="log-more"><ui-button :loading="loadingMore" @click="loadMore">加载更早的 {{ PAGE }} 行</ui-button></div>
            <p v-else-if="result && !tailing" class="muted log-more">已到达所选时间范围的起点</p>
          </template>
          <ui-empty v-else-if="!loading && !error" :description="searched ? '所选时间范围内没有匹配的日志' : '设置条件后查询日志'" :image-size="64" />
        </div>
      </ui-tab-pane>

      <ui-tab-pane name="rules" label="日志告警规则">
        <RuleGroupsPanel v-if="tab === 'rules'" source="loki" />
      </ui-tab-pane>
      <ui-tab-pane name="retention" label="保留与删除">
        <LogRetentionPanel v-if="tab === 'retention'" :labels="labelNames" :services="services" :load-values="loadLabelValues" />
      </ui-tab-pane>
    </ui-tabs>

    <LogContextDrawer v-model="contextVisible" :entry="contextEntry" />
    <QueryLibrary v-model="libraryVisible" :languages="canRaw ? ['logfilter', 'logql'] : ['logfilter']" @use="useQuery" />
    <ui-dialog v-model="saveVisible" title="保存查询" width="min(480px, 94vw)">
      <div class="save-form">
        <label>名称<ui-input v-model="saveForm.name" maxlength="100" /></label>
        <label>说明<ui-input v-model="saveForm.description" type="textarea" :rows="2" maxlength="500" /></label>
        <code class="query-code">{{ mode === 'logql' ? logql : result?.query || '当前筛选条件' }}</code>
      </div>
      <template #footer><ui-button @click="saveVisible = false">取消</ui-button><ui-button type="primary" :disabled="!saveForm.name.trim()" @click="saveQuery">保存</ui-button></template>
    </ui-dialog>
  </div>
</template>

<style scoped>
.ops-page { display: grid; grid-template-columns: minmax(0, 1fr); gap: var(--space-4); min-width: 0; }
.log-search { display: grid; gap: var(--space-3); min-width: 0; }
.log-toolbar, .log-actions { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: var(--space-2); }
.log-actions__left, .log-actions__right { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-2); }
.log-filter { display: grid; grid-template-columns: minmax(180px, 1.2fr) minmax(140px, 0.8fr) minmax(180px, 1.4fr) auto minmax(150px, 1fr); align-items: center; gap: var(--space-2); }
.log-filter__labels { grid-column: 1 / -1; }
.mono-input :deep(textarea) { font-family: var(--font-mono); font-size: var(--font-size-sm); }
.check-ok { display: inline-flex; align-items: center; gap: 4px; color: var(--success-text); font-size: var(--font-size-xs); }
.check-ok svg { width: 14px; height: 14px; }
.check-error { color: var(--danger-text); font-size: var(--font-size-xs); overflow-wrap: anywhere; }
.result-meta { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-2); min-width: 0; color: var(--text-muted); font-size: var(--font-size-xs); }
.query-code { color: var(--code-inline-text); font: var(--font-size-xs) var(--font-mono); word-break: break-all; }
.query-code--inline { flex: 1 1 200px; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.tail-status { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-2); color: var(--text-secondary); font-size: var(--font-size-xs); }
.tail-status__dot { width: 8px; height: 8px; background: var(--neutral-dot); border-radius: 50%; }
.tail-status__dot.is-live { background: var(--success); animation: pulse 1.6s ease-in-out infinite; }
@keyframes pulse { 50% { opacity: 0.35; } }
.volume-card.n-card > :deep(.n-card__content) { padding-top: 0; }
.log-more { display: flex; justify-content: center; margin: 0; }
.muted { margin: 0; color: var(--text-muted); font-size: var(--font-size-xs); }
.save-form { display: grid; gap: var(--space-3); }
.save-form label { display: grid; gap: 6px; color: var(--text-secondary); font-size: var(--font-size-sm); }
@media (max-width: 1100px) {
  .log-filter { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
@media (max-width: 767px) {
  .log-filter { grid-template-columns: minmax(0, 1fr); }
}
</style>
