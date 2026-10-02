import assert from 'node:assert/strict'
import test from 'node:test'
import { entryLevel, formatKpi, formatValue, parseLogFields, seriesName } from '../src/ops/format.js'
import { parseRelative, rangeLabel, resolveRange } from '../src/ops/timeRange.js'
import { alignSeries, framesToChart, framesToLogs, framesToTable, metricResultToChart, reduceValues, thresholdColor } from '../src/ops/frames.js'
import { addPanel, compact, dependsOn, duplicatePanel, movePanel, newPanel, normalizeLayout, removePanel, sections, toggleRow } from '../src/ops/dashboard.js'
import { setupScript } from './helpers/vue.mjs'
import vm from 'node:vm'
import { readFileSync } from 'node:fs'
import { computed, isReactive, nextTick, reactive, ref, shallowRef, watch } from 'vue'
import { clampAlertPage, pageAlertGroups, prepareAlertGroups, sortAlerts, summarizeAlerts } from '../src/ops/alerts.js'
import * as capacity from '../src/ops/capacity.js'

test('运维数值按 Grafana 单位格式化，空值显示为占位', () => {
  assert.equal(formatValue(null), '—')
  assert.equal(formatValue(1536, 'bytes'), '1.50 KiB')
  assert.equal(formatValue(0.456, 'percentunit'), '45.6%')
  assert.equal(formatValue(12.345, 'percent', 0), '12%')
  assert.equal(formatValue(90, 's'), '1 分 30 秒')
  assert.equal(formatValue(3, 'suffix:条/秒'), '3 条/秒')
  assert.equal(formatValue(2.5, 'suffix:条/秒'), '2.50 条/秒')
  assert.equal(formatValue(25000), '2.50 万')
  assert.equal(formatKpi(0, '条/5分钟'), '0 条/5分钟')
  assert.equal(formatKpi(7200, '秒'), '2 小时 0 分')
})

test('序列名称支持 legend 模板，缺失标签不报错', () => {
  assert.equal(seriesName({ job: 'api', instance: 'a:1' }, '{{job}} @ {{instance}} {{missing}}'), 'api @ a:1 ')
  assert.equal(seriesName({ __name__: 'up', job: 'api' }), 'up{job="api"}')
  assert.equal(seriesName({}), '值')
})

test('日志字段解析 JSON 与 logfmt，级别统一为标准值', () => {
  assert.deepEqual(parseLogFields('{"level":"INFO","msg":"ok","n":1}'), { level: 'INFO', msg: 'ok', n: '1' })
  assert.deepEqual(parseLogFields('level=warn msg="disk \\"full\\"" code=7'), { level: 'warn', msg: 'disk "full"', code: '7' })
  assert.deepEqual(parseLogFields('plain text line'), {})
  assert.equal(entryLevel({ labels: { level: 'WARNING' } }), 'warn')
  assert.equal(entryLevel({ metadata: { detected_level: 'error' } }), 'error')
})

test('相对时间范围按查询时刻计算', () => {
  const now = Date.UTC(2026, 0, 2, 12, 30)
  assert.equal(parseRelative('now-1h', now), now - 3600e3)
  assert.equal(parseRelative(123456789012, now), 123456789012)
  const range = resolveRange({ from: 'now-6h', to: 'now' }, now)
  assert.equal(range.to - range.from, 6 * 3600e3)
  assert.equal(rangeLabel({ from: 'now-24h', to: 'now' }), '近 24 小时')
})

test('仪表盘单点和稀疏时序固定到查询时间窗，刷新和缩放更新坐标轴', async () => {
  const source = setupScript(new URL('../src/components/ops/TimeSeriesChart.vue', import.meta.url))
  const to = Date.UTC(2026, 8, 27, 10)
  const from = to - 3600e3
  const props = reactive({ times: [to - 1800e3], series: [{ name: 'errors', values: [2] }], timeRange: { from, to }, height: 240, unit: 'short' })
  let plot
  class Plot {
    constructor(options, data) { this.options = options; plot = this; this.setData(data) }
    setData(data) { this.data = data; this.range = this.options.scales.x.range?.(this, data[0][0], data[0].at(-1)) }
    destroy() {}
  }
  let cleanup
  const context = vm.createContext({ ref, watch, defineProps: () => props, defineEmits: () => () => {}, onMounted() {}, onBeforeUnmount: fn => { cleanup = fn }, uPlot: Plot, formatValue, document: { documentElement: {} }, getComputedStyle: () => ({ getPropertyValue: () => '' }) })
  const chart = vm.runInContext(source + '\n;({host,build})', context)
  chart.host.value = { clientWidth: 800 }
  chart.build()
  assert.deepEqual(Array.from(plot.range), [from / 1000, to / 1000])
  assert.equal(plot.data[0].length, 1)

  props.timeRange = { from: from + 60e3, to: to + 60e3 }
  await nextTick()
  assert.deepEqual(Array.from(plot.range), [(from + 60e3) / 1000, (to + 60e3) / 1000])
  props.timeRange = { from: to - 2400e3, to: to - 1200e3 }
  props.times = [to - 2200e3, to - 1800e3]
  props.series = [{ name: 'errors', values: [1, 2] }]
  await nextTick()
  assert.deepEqual(Array.from(plot.range), [(to - 2400e3) / 1000, (to - 1200e3) / 1000])
  cleanup()
})

test('多条序列按时间对齐，缺失点保持空值', () => {
  const chart = alignSeries([{ name: 'a', timestamps: [1, 2], values: [1, 2] }, { name: 'b', timestamps: [2, 3], values: [5, null] }])
  assert.deepEqual(chart.times, [1, 2, 3])
  assert.deepEqual(chart.series[1].values, [null, 5, null])
  const fromMetric = metricResultToChart({ series: [{ labels: { job: 'x' }, timestamps: [10], values: [3] }] }, '{{job}}')
  assert.equal(fromMetric.series[0].name, 'x')
})

test('Grafana 数据帧转换为图表、表格和日志', () => {
  const frames = [
    { refId: 'A', fields: [{ name: 'Time', type: 'time', values: [1000, 2000] }, { name: 'Value', type: 'number', labels: { job: 'a' }, config: { displayNameFromDS: 'A 服务' }, values: [1, 3] }] },
    { refId: 'A', fields: [{ name: 'Time', type: 'time', values: [1000, 2000] }, { name: 'Value', type: 'number', labels: { job: 'b' }, values: [2, null] }] }
  ]
  const chart = framesToChart(frames)
  assert.equal(chart.series[0].name, 'A 服务')
  assert.equal(reduceValues(chart.series[1].values, 'lastNotNull'), 2)
  assert.equal(reduceValues([1, 3, 2], 'delta'), 2)
  const summary = framesToTable(frames)
  assert.equal(summary.rows.length, 2)
  assert.equal(summary.rows[0].__value, 3)
  const table = framesToTable([{ fields: [{ name: 'Time', type: 'time', values: [1] }, { name: 'job', type: 'string', values: ['x'] }, { name: 'Value', type: 'number', values: [1] }] }])
  assert.deepEqual(table.columns.map(c => c.key), ['Time', 'job', 'Value'])
  const logs = framesToLogs([{ fields: [{ name: 'labels', type: 'other', values: [{ service_name: 'api' }] }, { name: 'Time', type: 'time', values: [5] }, { name: 'Line', type: 'string', values: ['hello'] }, { name: 'tsNs', type: 'string', values: ['5000001'] }] }])
  assert.deepEqual(logs[0], { ts: '5000001', timeMs: 5, line: 'hello', labels: { service_name: 'api' } })
})

test('阈值颜色使用主题变量，未设置阈值时使用正文颜色', () => {
  const thresholds = { mode: 'absolute', steps: [{ color: 'green', value: null }, { color: 'orange', value: 80 }, { color: 'red', value: 95 }] }
  assert.equal(thresholdColor(10, thresholds), 'var(--success)')
  assert.equal(thresholdColor(90, thresholds), 'var(--warning)')
  assert.equal(thresholdColor(99, thresholds), 'var(--danger)')
  assert.equal(thresholdColor(5, undefined), 'var(--text-strong)')
  assert.equal(thresholdColor(50, { mode: 'percentage', steps: [{ color: 'green', value: null }, { color: 'red', value: 40 }] }, 0, 200), 'var(--success)')
})

const panel = (id, x, y, w, h, extra = {}) => ({ id, type: 'timeseries', gridPos: { x, y, w, h }, ...extra })

test('compact moves panels up without overlaps', () => {
  const panels = [panel(1, 0, 5, 12, 8), panel(2, 12, 20, 12, 8), panel(3, 0, 40, 24, 4)]
  compact(panels)
  assert.deepEqual(panels.map(p => p.gridPos.y), [0, 0, 8])
})

test('normalizeLayout stacks rows and keeps collapsed children inside the row', () => {
  const dash = { panels: [panel(1, 0, 0, 24, 6), { id: 2, type: 'row', collapsed: false, gridPos: { x: 0, y: 10, w: 24, h: 1 }, panels: [] }, panel(3, 0, 30, 12, 5), { id: 4, type: 'row', collapsed: true, gridPos: { x: 0, y: 50, w: 24, h: 1 }, panels: [panel(5, 0, 90, 12, 4)] }] }
  normalizeLayout(dash)
  assert.deepEqual(dash.panels.map(p => [p.id, p.gridPos.y]), [[1, 0], [2, 6], [3, 7], [4, 12]])
  assert.equal(dash.panels[3].panels[0].gridPos.y, 13)
  assert.equal(sections(dash).length, 3)
})

test('toggleRow moves panels between the row and the top level', () => {
  const dash = { panels: [{ id: 1, type: 'row', collapsed: false, panels: [], gridPos: { x: 0, y: 0, w: 24, h: 1 } }, panel(2, 0, 1, 12, 4), panel(3, 12, 1, 12, 4)] }
  toggleRow(dash, 1)
  assert.equal(dash.panels.length, 1)
  assert.equal(dash.panels[0].panels.length, 2)
  toggleRow(dash, 1)
  assert.deepEqual(dash.panels.map(p => p.id), [1, 2, 3])
})

test('add, duplicate, move and remove keep ids unique and layout compact', () => {
  const dash = { panels: [] }
  addPanel(dash, newPanel(dash, 'stat'))
  addPanel(dash, newPanel(dash, 'timeseries'))
  duplicatePanel(dash, 2)
  assert.deepEqual(dash.panels.map(p => p.id), [1, 2, 3])
  movePanel(dash, 3, 'up')
  const ids = dash.panels.map(p => p.id)
  assert.equal(new Set(ids).size, 3)
  removePanel(dash, 1)
  assert.deepEqual(dash.panels.map(p => p.id).sort(), [2, 3])
  assert.ok(dash.panels.every(p => p.gridPos.y >= 0 && p.gridPos.x + p.gridPos.w <= 24))
})

test('dependsOn detects Grafana variable reference syntaxes', () => {
  assert.ok(dependsOn({ query: 'label_values(up{job="$job"}, instance)' }, 'job'))
  assert.ok(dependsOn({ query: { query: 'label_values(up{job=~"${job:regex}"}, instance)' } }, 'job'))
  assert.ok(dependsOn({ query: 'x', regex: '/[[job]]/' }, 'job'))
  assert.ok(!dependsOn({ query: 'label_values(up{job="$jobs"}, instance)' }, 'job'))
})

const alert = (i, extra = {}) => ({ fingerprint: `alert-${String(i).padStart(5, '0')}`, startsAt: new Date(1700000000000 + i * 1000).toISOString(), state: i % 2 ? 'active' : 'suppressed', labels: { severity: i % 3 ? 'warning' : 'critical' }, receivers: ['mail'], ...extra })

test('万条告警排序只解析一次时间，分页不丢失记录且顺序稳定', () => {
  const input = Array.from({ length: 10000 }, (_, i) => alert(i))
  const sorted = sortAlerts(input)
  assert.equal(sorted[0].fingerprint, 'alert-09999')
  assert.equal(input[0].fingerprint, 'alert-00000')
  const groups = prepareAlertGroups([{ receiver: 'mail', labels: { job: 'api' }, alerts: input }])
  const seen = new Set()
  for (let page = 1; page <= 500; page++) {
    const visible = pageAlertGroups(groups, page, 20)
    assert.equal(visible.length, 1)
    assert.equal(visible[0].alerts.length, 20)
    assert.equal(visible[0].total, 10000)
    for (const row of visible[0].alerts) seen.add(row.fingerprint)
  }
  assert.equal(seen.size, 10000)
})

test('分组跨页共享容量，多接收人统计按告警指纹去重', () => {
  const groups = prepareAlertGroups([
    { receiver: 'a', labels: {}, alerts: [alert(0), alert(1), alert(2)] },
    { receiver: 'b', labels: {}, alerts: [alert(0, { receivers: ['other'] }), alert(3)] },
    { receiver: 'empty', labels: {}, alerts: [] }
  ])
  const visible = pageAlertGroups(groups, 2, 2)
  assert.deepEqual(visible.map(g => g.alerts.length), [1, 1])
  assert.deepEqual(visible.map(g => g.total), [3, 2])
  const stats = summarizeAlerts(groups.flatMap(g => g.alerts))
  assert.deepEqual(stats.summary, { total: 4, active: 2, suppressed: 2, critical: 2 })
  assert.deepEqual(stats.receivers, ['mail', 'other'])
  assert.equal(clampAlertPage(500, 21, 20), 2)
  assert.equal(clampAlertPage(3, 0, 20), 1)
})

function pageHarness() {
  const pending = []
  const timers = new Map()
  let timerID = 0
  let poll
  let version = 0
  const runner = {
    async run(task) {
      const current = ++version
      const result = await task({})
      if (current !== version) throw Object.assign(new Error('stale'), { name: 'AbortError' })
      return result
    },
    cancel() { version++ }
  }
  const source = setupScript(new URL('../src/views/OpsAlertsView.vue', import.meta.url))
  const context = vm.createContext({ ref, shallowRef, computed, watch, can: () => true, defineEmits: () => () => {}, onMounted() {}, onBeforeUnmount() {}, latest: () => runner, summarizeAlerts, sortAlerts, prepareAlertGroups, pageAlertGroups, clampAlertPage, opsErrorText: e => e.message, document: { hidden: false }, opsGet: (path, params) => new Promise((resolve, reject) => pending.push({ path, params, resolve, reject })), setInterval: fn => { poll = fn; return 1 }, clearInterval() {}, setTimeout: fn => { timers.set(++timerID, fn); return timerID }, clearTimeout: id => timers.delete(id) })
  vm.runInContext(source + '\nthis.page = {loadAlerts, grouped, alerts, visibleAlerts, visibleGroups, alertStats, alertPage, alertPageSize, alertsLoading, alertsError, filters, scheduleAlerts, tab}', context)
  return { ...context.page, pending, poll: () => poll(), debounce: () => { for (const fn of timers.values()) fn(); timers.clear() } }
}

test('页面只渲染当前页，刷新保留页码并在数据减少时回退', async () => {
  const p = pageHarness()
  const first = p.loadAlerts()
  p.pending[0].resolve({ items: Array.from({ length: 10000 }, (_, i) => alert(i)) })
  await first
  assert.equal(p.visibleAlerts.value.length, 20)
  assert.equal(isReactive(p.alerts.value[0]), false)
  p.alertPage.value = 4
  const refresh = p.loadAlerts()
  p.pending[1].resolve({ items: Array.from({ length: 100 }, (_, i) => alert(i)) })
  await refresh
  assert.equal(p.alertPage.value, 4)
  const shrink = p.loadAlerts()
  p.pending[2].resolve({ items: [alert(1)] })
  await shrink
  assert.equal(p.alertPage.value, 1)
  p.alertPageSize.value = 50
  await nextTick()
  assert.equal(p.alertPage.value, 1)
})

test('分组模式只请求分组接口，过期请求不影响新请求的加载状态', async () => {
  const p = pageHarness()
  const first = p.loadAlerts()
  p.grouped.value = true
  await nextTick()
  assert.deepEqual(p.pending.map(r => r.path), ['/api/v1/ops/alerts', '/api/v1/ops/alerts/groups'])
  p.pending[0].resolve({ items: [alert(1)] })
  await first
  assert.equal(p.alertsLoading.value, true)
  p.pending[1].resolve({ items: [{ receiver: 'mail', labels: {}, alerts: Array.from({ length: 10000 }, (_, i) => alert(i)) }] })
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(p.alertsLoading.value, false)
  assert.equal(p.visibleGroups.value[0].alerts.length, 20)
  assert.equal(p.alertStats.value.summary.total, 10000)
})

test('筛选防抖期间废弃旧数据，自动刷新不叠加请求，离开当前页取消请求', async () => {
  const p = pageHarness()
  p.scheduleAlerts()
  const first = p.loadAlerts()
  p.poll()
  assert.equal(p.pending.length, 1)
  p.filters.value = [{ name: 'job', op: '=', value: 'api' }]
  await nextTick()
  p.poll()
  assert.equal(p.pending.length, 1)
  p.pending[0].resolve({ items: [alert(1)] })
  await first
  assert.equal(p.alerts.value.length, 0)
  p.debounce()
  assert.equal(p.pending.length, 2)
  p.tab.value = 'notifications'
  await nextTick()
  p.pending[1].resolve({ items: [alert(2)] })
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(p.alerts.value.length, 0)
  assert.equal(p.alertsLoading.value, false)
})

test('容量测试进度只按真实测量窗口计算，结束的运行停止轮询', async () => {
  const { boundText, isFinished, phaseSummary, pollDelay, statusTone, windowProgress } = await import('../src/ops/capacity.js')
  const run = { measureFrom: 1000, measureTo: 11000 }
  assert.equal(windowProgress(run, 500), 0)
  assert.equal(windowProgress(run, 6000), 50)
  assert.equal(windowProgress(run, 20000), 100)
  assert.equal(windowProgress({ status: 'DRAINING' }, 6000), null)
  assert.equal(pollDelay([{ status: 'FINISHED', active: false }]), 0)
  assert.equal(pollDelay([{ status: 'FINISHED' }, { status: 'RUNNING', active: true }]), 3000)
  assert.equal(pollDelay([{ status: 'VERIFYING', active: false }]), 3000)
  assert.equal(pollDelay([{ status: 'FINISHED', cleaning: true }]), 3000)
  assert.equal(pollDelay([], true), 3000)
  assert.equal(isFinished('CANCELLED'), true)
  assert.equal(statusTone('FINISHED', 'passed'), 'success')
  assert.equal(statusTone('FINISHED', 'inconclusive'), 'info')
  assert.equal(statusTone('RUNNING'), 'warning')
  assert.equal(boundText(null), '—')
  assert.match(boundText(1234.5), /1,234\.5 条\/秒/)
  assert.deepEqual(phaseSummary([{ verdict: 'passed' }, { verdict: 'failed' }, { verdict: 'passed' }]), { passed: 2, failed: 1, inconclusive: 0 })
})

test('容量测试表单草稿按租户和用户恢复，忽略损坏或越界字段', async () => {
  const { defaultForm, draftKey, loadDraft, saveDraft } = await import('../src/ops/capacity.js')
  const storage = new Map()
  const store = { getItem: key => storage.get(key) ?? null, setItem: (key, value) => storage.set(key, value) }
  const alice = { tenant: 't1', user: 'alice' }
  saveDraft(store, alice, { form: { ...defaultForm('capacity'), devices: 300, tenant: 'x' }, advanced: true, planText: 'schemaVersion: 1', environment: 'self' })
  const draft = loadDraft(store, alice)
  assert.equal(draft.form.preset, 'capacity')
  assert.equal(draft.form.devices, 300)
  assert.equal('tenant' in draft.form, false)
  assert.equal(draft.advanced, true)
  assert.equal(draft.planText, 'schemaVersion: 1')
  assert.equal(loadDraft(store, { tenant: 't1', user: 'bob' }), null)
  storage.set(draftKey(alice), '{broken')
  assert.equal(loadDraft(store, alice), null)
})

test('容量测试表单生成的计划只含受控字段，并按单台设备限速检查速率上限', async () => {
  const { buildPlan, defaultForm, formProblems, rateCeiling } = await import('../src/ops/capacity.js')
  const form = defaultForm('capacity')
  const plan = buildPlan(form)
  assert.match(plan, /preset: capacity/)
  assert.match(plan, /autoProvision: true/)
  assert.doesNotMatch(plan, /tenant|operatorSecretRef|operatorToken/)
  assert.deepEqual(formProblems(form), [])
  assert.equal(rateCeiling({ devices: 3 }), 60)
  assert.ok(formProblems({ ...form, devices: 3, maxRate: 100 }).some(p => p.includes('单台设备限速')))
  assert.ok(formProblems({ ...form, startRate: 50, maxRate: 10 }).some(p => p.includes('起始速率')))
  const soak = buildPlan({ ...defaultForm('soak'), measureMinutes: 90, mqtt: true })
  assert.match(soak, /rates: \[50\]/)
  assert.match(soak, /maximumWallTime: 150m/)
  assert.match(soak, /mqttConnections: 50/)
})

test('历史清理只显示实际数量与进度，全部保留的预览不能提交', () => {
  assert.equal(capacity.cleanupCountText(undefined), '—')
  assert.equal(capacity.cleanupCountText(null), '—')
  assert.equal(capacity.cleanupCountText(''), '—')
  assert.equal(capacity.cleanupCountText(0), '0')
  assert.equal(capacity.historyCleanupProgress({ processed: 1, total: 4 }), 25)
  for (const job of [{ status:'SUCCEEDED' }, { processed:1,total:0 }, { processed:5,total:4 }, { processed:null,total:4 }]) assert.equal(capacity.historyCleanupProgress(job), null)
  assert.equal(capacity.historyCleanupHasTargets({ token:'preview', products:2, items:[{ eligible:false,reason:'业务引用' }] }), false)
  assert.equal(capacity.historyCleanupHasTargets({ token:'preview', items:[{ eligible:true }] }), true)
  assert.deepEqual(capacity.cleanupCountItems({ devices:0,rawMessages:4,cacheKeys:undefined }), [{ key:'devices',label:'设备',value:0 },{ key:'rawMessages',label:'测试原文',value:4 }])
  assert.deepEqual(capacity.cleanupRuntimeItems({ queueOffsetSpan:12,queueSkippedPartitions:2,inboxSkipped:0 }), [{ key:'queueOffsetSpan',label:'已清理队列偏移跨度',value:12 },{ key:'queueSkippedPartitions',label:'保留的共享队列分区',value:2 }])
  const partial={status:'SUCCEEDED',warnings:['共享队列保留'],counts:{warnings:['共享队列保留','收件箱保留']}}
  assert.deepEqual(capacity.historyCleanupWarnings(partial),['共享队列保留','收件箱保留'])
  assert.equal(capacity.historyCleanupResultText(partial),'已完成可清理部分')
  assert.equal(capacity.historyCleanupResultText({status:'SUCCEEDED',error:'结果存储失败'}),'清理结果需确认')
  const counts={rawMessages:1,standardMessages:2,alarms:3,devices:4,products:5,rules:6,resources:7,audits:8,profiles:9,accessReferences:10,protocols:11,inbox:12,inboxSkipped:13,retainedRequests:14,queueOffsetSpan:15,queueSkippedPartitions:16}
  const removed=capacity.cleanupCountItems(counts),runtime=capacity.cleanupRuntimeItems(counts)
  assert.equal(removed.length,12)
  assert.equal(runtime.length,4)
  for (const item of [...removed,...runtime]) assert.equal(item.value,counts[item.key])
  assert.equal(removed.some(item=>['inboxSkipped','queueSkippedPartitions','retainedRequests','queueOffsetSpan'].includes(item.key)),false,'skipped work, requests and offsets are never labelled as deleted messages')
})

function capacityPage({ get, send, allow = true } = {}) {
  const calls = [], messages = [], timers = [], grants = reactive(new Set(allow ? ['DELETE /api/v1/ops/capacity/runs/:id'] : []))
  const session = reactive({ tenant:'tenant',user:'operator',accessVersion:'v1' })
  let cleanup
  const job = { id:'history-job',environment:'self',status:'RUNNING',phase:'清理历史产品',processed:0,total:2 }
  const preview = { token:'scope-v1',runs:1,products:1,devices:3,rawMessages:10,items:[{ productId:'cap-product',eligible:true,devices:3,rawMessages:10 },{ productId:'business-product',eligible:false,reason:'已有摄像头关联' }],warnings:['系统审计保留'] }
  const context = vm.createContext({ ...capacity,computed,reactive,ref,watch,session,defineEmits:()=>()=>{},onMounted(){},onBeforeUnmount(fn){cleanup=fn},can:path=>grants.has(path),
    window:{sessionStorage:{getItem:()=>JSON.stringify({environment:'self',form:capacity.defaultForm()})}},
    UiMessage:Object.fromEntries(['info','success','warning','error'].map(kind=>[kind,value=>messages.push([kind,value])])),UiMessageBox:{confirm:async()=>{}},opsErrorText:error=>error.message,
    opsGet:async(path,params)=>{calls.push({method:'GET',path,params});if(get)return get(path,params);if(path.endsWith('/history/status'))return{job:structuredClone(job)};if(path.endsWith('/cleanup/history'))return{preview:structuredClone(preview),job:null};if(path.endsWith('/status'))return{enabled:true,reachable:true};return{items:[],total:0}},
    opsSend:async(method,path,body)=>{calls.push({method,path,body});return send ? send(method,path,body) : {job:structuredClone(job)}},
    setTimeout(fn,delay){timers.push({fn,delay});return timers.length},clearTimeout(id){if(timers[id-1])timers[id-1].cleared=true}
  })
  const state = vm.runInContext(setupScript(new URL('../src/views/OpsCapacityView.vue',import.meta.url))+';({loadHistoryStatus,previewHistoryCleanup,confirmHistoryCleanup,historyJob,historyPreview,historyDialog,historyError,historyRunning,historyPending,historyReady,historyCleaning,cleanupBlocked,environment,activeRunId,cleaningRunId,moduleStatus,loadRuns,start,cleanupRun,rowActions,stateLabel})',context)
  return {...state,calls,messages,timers,grants,session,job,preview,dispose:()=>cleanup()}
}

test('容量测试提前结束后，列表与详情仍展示后台失败原因', async () => {
  const Vue = await import('vue')
  const { parse: parseSFC } = await import('@vue/compiler-sfc')
  const { compile, parse } = await import('@vue/compiler-dom')
  const { renderToString } = await import('@vue/server-renderer')
  const source = readFileSync(new URL('../src/views/OpsCapacityView.vue', import.meta.url), 'utf8')
  const ast = parse(parseSFC(source).descriptor.template.content)
  function find(node, matches) {
    if (matches(node)) return node
    for (const child of node.children || []) {
      const found = find(child, matches)
      if (found) return found
    }
  }
  const statusColumn = find(ast, node => node.tag === 'ui-table-column' && node.props.some(prop => prop.name === 'label' && prop.value?.content === '状态'))
  const statusSlot = statusColumn.children.find(node => node.tag === 'template')
  const detailDialog = find(ast, node => node.tag === 'ui-dialog' && node.props.some(prop => prop.name === 'bind' && prop.arg?.content === 'model-value' && prop.exp?.content === 'Boolean(detail)'))
  const templates = [statusSlot.children.map(node => node.loc.source).join(''), detailDialog.loc.source]
  const page = capacityPage()
  const message = 'prepare failed: device enrolment incomplete (0/2); results: 409=2'
  for (const status of ['PREFLIGHT', 'FINISHED', 'FAILED', 'CANCELLED']) {
    const run = { runId: 'early-failure', status, verdict: 'inconclusive', classification: 'inconclusive', message, completed: [] }
    for (const template of templates) {
      const render = new Function('Vue', compile(template, { mode: 'function', prefixIdentifiers: true }).code)(Vue)
      const app = Vue.createSSRApp({ render, setup: () => ({ ...capacity, row: run, detail: run, stateLabel: page.stateLabel, formatTime: () => '—', can: () => false }) })
      app.component('StatusDot', { props: ['label'], setup: props => () => Vue.h('span', props.label) })
      for (const name of ['ui-dialog', 'ui-descriptions', 'ui-descriptions-item']) app.component(name, { setup: (_, { slots }) => () => Vue.h('div', slots.default?.()) })
      for (const name of ['ui-table', 'ui-table-column', 'ui-empty', 'ui-button']) app.component(name, { setup: () => () => Vue.h('div') })
      const html = await renderToString(app)
      assert.ok(html.includes(message), `${status} must retain its failure reason in both views`)
      assert.ok(html.includes(page.stateLabel(run)), 'the backend verdict remains unchanged')
    }
  }
  page.dispose()
})

test('历史清理取消和权限不足均不发送删除请求，提交范围只来自预览token', async () => {
  const denied=capacityPage({allow:false})
  await denied.previewHistoryCleanup();await denied.loadHistoryStatus();await denied.confirmHistoryCleanup()
  assert.equal(denied.calls.length,0)
  const page=capacityPage()
  await page.previewHistoryCleanup()
  assert.equal(page.historyPreview.value.items[1].reason,'已有摄像头关联')
  page.historyDialog.value=false;await page.confirmHistoryCleanup()
  assert.equal(page.calls.some(call=>call.method==='POST'),false)
  await page.previewHistoryCleanup();page.grants.clear();await nextTick();await page.confirmHistoryCleanup()
  assert.equal(page.calls.some(call=>call.method==='POST'),false)
  assert.equal(page.historyDialog.value,false)
})

test('历史清理202后继续轮询，只有服务端明确成功才报告完成', async () => {
  const page=capacityPage()
  await page.previewHistoryCleanup();await page.confirmHistoryCleanup()
  const sent=page.calls.find(call=>call.method==='POST')
  assert.deepEqual(JSON.parse(JSON.stringify(sent.body)),{environment:'self',previewToken:'scope-v1'})
  assert.equal(page.historyRunning.value,true)
  assert.equal(page.messages.some(([kind])=>kind==='success'),false)
  assert(page.timers.some(timer=>timer.delay===3000&&!timer.cleared))
  page.job.status='SUCCEEDED';page.job.processed=2
  await page.loadHistoryStatus(true)
  assert.equal(page.historyRunning.value,false)
  assert.equal(page.messages.filter(([kind])=>kind==='success').length,1)
  await page.loadHistoryStatus(true)
  assert.equal(page.messages.filter(([kind])=>kind==='success').length,1,'refreshing a finished job does not repeat success')
})

test('历史清理失败可重新预览重试，范围变化409使旧确认失效', async () => {
  let reject=false
  const page=capacityPage({send:async()=>{if(reject)throw Object.assign(new Error('范围变化'),{status:409});return{job:{id:'history-job',environment:'self',status:'RUNNING',processed:0,total:2}}}})
  await page.previewHistoryCleanup();await page.confirmHistoryCleanup()
  page.job.status='FAILED';page.job.error='收件箱仍在处理中'
  await page.loadHistoryStatus(true)
  assert.equal(page.historyJob.value.error,'收件箱仍在处理中')
  assert.equal(page.messages.some(([kind])=>kind==='success'),false)
  reject=true;page.preview.token='scope-v2'
  await page.previewHistoryCleanup();await page.confirmHistoryCleanup()
  assert.equal(page.calls.filter(call=>call.method==='POST').at(-1).body.previewToken,'scope-v2')
  assert.equal(page.historyPreview.value,null)
  const attempts=page.calls.filter(call=>call.method==='POST').length
  await page.confirmHistoryCleanup()
  assert.equal(page.calls.filter(call=>call.method==='POST').length,attempts)
})

test('清理任务消失或查询失败都不能误报成功，单次清理与历史清理互斥', async () => {
  let response={job:{id:'history-job',status:'RUNNING',processed:0,total:2}}
  let failed=false
  const page=capacityPage({get:async path=>{if(failed)throw new Error('暂时无法连接控制服务');return path.endsWith('/history/status')?response:{items:[],total:0}}})
  await page.loadHistoryStatus()
  response={job:null};await page.loadHistoryStatus(true)
  assert.equal(page.historyRunning.value,true)
  assert.match(page.historyError.value,/尚不能判定/)
  failed=true;await page.loadHistoryStatus(true)
  assert.equal(page.historyRunning.value,true)
  assert.match(page.historyError.value,/无法连接/)
  await page.cleanupRun({runId:'finished',status:'FINISHED'})
  assert.equal(page.calls.some(call=>call.method==='DELETE'),false)
  assert.equal(page.messages.some(([kind])=>kind==='success'),false)
})

test('全局历史清理状态独立于单次运行，阻止并行操作并持续刷新', async () => {
  const page=capacityPage({get:async path=>path.endsWith('/runs')?{items:[],total:0,historyCleaning:true,cleaningRunId:''}:{job:null}})
  await page.loadRuns();await nextTick()
  assert.equal(page.historyCleaning.value,true)
  assert.equal(page.cleaningRunId.value,'')
  assert.equal(page.cleanupBlocked.value,true)
  assert(page.timers.some(timer=>timer.delay===3000&&!timer.cleared))
  await page.start();await page.cleanupRun({runId:'finished',status:'FINISHED'});await page.previewHistoryCleanup()
  assert.equal(page.calls.some(call=>call.method==='POST'||call.method==='DELETE'),false)
  assert.equal(page.calls.some(call=>call.path.endsWith('/runs/finished')),false,'history cleanup must never trigger a per-run 404 completion check')
  assert.equal(page.rowActions({runId:'finished',status:'FINISHED'}).find(action=>action.key==='cleanup').disabled,true)
})

test('受理但未返回有效job时保持待确认，控制服务不可达时不能预览或提交', async () => {
  const page=capacityPage({send:async()=>({}),get:async path=>path.endsWith('/cleanup/history')?{preview:{token:'token',items:[{eligible:true}]} ,job:null}:path.endsWith('/history/status')?{job:null}:{items:[],total:0}})
  await page.previewHistoryCleanup();await page.confirmHistoryCleanup()
  assert.equal(page.historyPending.value,true)
  assert.equal(page.historyRunning.value,true)
  assert.match(page.historyError.value,/尚不能判定/)
  assert.equal(page.messages.some(([kind])=>kind==='success'),false)
  const unreachable=capacityPage()
  unreachable.moduleStatus.value={enabled:true,reachable:false}
  await unreachable.previewHistoryCleanup();await unreachable.confirmHistoryCleanup()
  assert.equal(unreachable.calls.length,0)
})

test('历史清理恢复最近任务，身份切换或离页后的迟到预览不会打开确认框', async () => {
  const restored=capacityPage();await restored.loadHistoryStatus()
  assert.equal(restored.historyJob.value.id,'history-job')
  assert.equal(restored.historyRunning.value,true)
  let finish
  const page=capacityPage({get:async path=>path.endsWith('/cleanup/history')?new Promise(resolve=>finish=resolve):{job:null}})
  const request=page.previewHistoryCleanup()
  page.session.user='another';await nextTick()
  finish({preview:page.preview,job:null});await request
  assert.equal(page.historyDialog.value,false)
  const disposed=capacityPage({get:async()=>new Promise(resolve=>finish=resolve)})
  const late=disposed.previewHistoryCleanup();disposed.dispose();finish({preview:disposed.preview});await late
  assert.equal(disposed.historyDialog.value,false)
})
