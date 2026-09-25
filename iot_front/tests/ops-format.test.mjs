import assert from 'node:assert/strict'
import test from 'node:test'
import { formatValue, formatKpi, seriesName, parseLogFields, entryLevel } from '../src/ops/format.js'
import { parseRelative, resolveRange, rangeLabel } from '../src/ops/timeRange.js'
import { alignSeries, framesToChart, framesToTable, framesToLogs, reduceValues, thresholdColor, metricResultToChart } from '../src/ops/frames.js'

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
