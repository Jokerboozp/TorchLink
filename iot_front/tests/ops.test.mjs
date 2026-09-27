import assert from 'node:assert/strict'
import test from 'node:test'
import { entryLevel, formatKpi, formatValue, parseLogFields, seriesName } from '../src/ops/format.js'
import { parseRelative, rangeLabel, resolveRange } from '../src/ops/timeRange.js'
import { alignSeries, framesToChart, framesToLogs, framesToTable, metricResultToChart, reduceValues, thresholdColor } from '../src/ops/frames.js'
import { addPanel, compact, dependsOn, duplicatePanel, movePanel, newPanel, normalizeLayout, removePanel, sections, toggleRow } from '../src/ops/dashboard.js'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import { computed, isReactive, nextTick, reactive, ref, shallowRef, watch } from 'vue'
import { clampAlertPage, pageAlertGroups, prepareAlertGroups, sortAlerts, summarizeAlerts } from '../src/ops/alerts.js'

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
  const source = readFileSync(new URL('../src/components/ops/TimeSeriesChart.vue', import.meta.url), 'utf8').match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm, '')
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
  const source = readFileSync(new URL('../src/views/OpsAlertsView.vue', import.meta.url), 'utf8').split('<script setup>')[1].split('</script>')[0].replace(/^import .*$/gm, '')
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
  assert.equal(isFinished('CANCELLED'), true)
  assert.equal(statusTone('FINISHED', 'passed'), 'success')
  assert.equal(statusTone('FINISHED', 'inconclusive'), 'info')
  assert.equal(statusTone('RUNNING'), 'warning')
  assert.equal(boundText(null), '—')
  assert.match(boundText(1234.5), /1,234\.5 条\/秒/)
  assert.deepEqual(phaseSummary([{ verdict: 'passed' }, { verdict: 'failed' }, { verdict: 'passed' }]), { passed: 2, failed: 1, inconclusive: 0 })
})

test('容量测试页只调用平台运维接口，并在菜单与权限预设中登记', () => {
  const view = readFileSync(new URL('../src/views/OpsCapacityView.vue', import.meta.url), 'utf8')
  for (const path of view.match(/\/api\/v1\/[^`'"?$]+/g)) assert.ok(path.startsWith('/api/v1/ops/capacity/'), path)
  const app = readFileSync(new URL('../src/App.vue', import.meta.url), 'utf8')
  assert.match(app, /'opsAlerts', 'opsCapacity'\]/)
  const presets = readFileSync(new URL('../src/permissionPresets.js', import.meta.url), 'utf8')
  assert.match(presets, /'opsCapacity'/)
})
