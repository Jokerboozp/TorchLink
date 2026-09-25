// 把 Prometheus 查询结果与 Grafana 数据帧转换为图表、表格、统计值和日志行。

import { seriesName } from './format.js'

// alignSeries 按时间戳并集对齐多条序列，缺失点为 null；时间单位为毫秒。
export function alignSeries(list) {
  const stamps = new Set()
  for (const item of list) for (const ts of item.timestamps) stamps.add(ts)
  const times = [...stamps].sort((a, b) => a - b)
  const index = new Map(times.map((ts, i) => [ts, i]))
  const series = list.map(item => {
    const values = new Array(times.length).fill(null)
    item.timestamps.forEach((ts, i) => { values[index.get(ts)] = item.values[i] ?? null })
    return { name: item.name, labels: item.labels || {}, values }
  })
  return { times, series }
}

export function metricResultToChart(result, legend = '') {
  return alignSeries((result?.series || []).map(s => ({ name: seriesName(s.labels, legend), labels: s.labels, timestamps: s.timestamps || [], values: s.values || [] })))
}

function frameTimeField(frame) {
  return frame.fields.find(field => field.type === 'time')
}

function fieldName(field, frame, multiFrames) {
  const display = field.config?.displayNameFromDS
  if (display) return display
  if (field.labels && Object.keys(field.labels).length) return seriesName(field.labels)
  if (multiFrames && frame.name) return frame.name
  return field.name || frame.refId || '值'
}

// framesToChart 把时间 + 数值字段组合成序列，适用于时序图与统计计算。
export function framesToChart(frames = []) {
  const list = []
  for (const frame of frames) {
    const time = frameTimeField(frame)
    if (!time) continue
    for (const field of frame.fields) {
      if (field === time || field.type !== 'number') continue
      list.push({ name: fieldName(field, frame, frames.length > 1), labels: field.labels || {}, timestamps: time.values.map(Number), values: field.values.map(v => (v == null ? null : Number(v))), unit: field.config?.unit })
    }
  }
  return alignSeries(list)
}

const calcs = {
  lastNotNull: v => { for (let i = v.length - 1; i >= 0; i--) if (v[i] != null) return v[i]; return null },
  last: v => (v.length ? v[v.length - 1] : null),
  firstNotNull: v => v.find(x => x != null) ?? null,
  first: v => (v.length ? v[0] : null),
  mean: v => { const n = v.filter(x => x != null); return n.length ? n.reduce((a, b) => a + b, 0) / n.length : null },
  max: v => { const n = v.filter(x => x != null); return n.length ? Math.max(...n) : null },
  min: v => { const n = v.filter(x => x != null); return n.length ? Math.min(...n) : null },
  sum: v => v.reduce((a, b) => a + (b ?? 0), 0),
  count: v => v.filter(x => x != null).length,
  delta: v => { const n = v.filter(x => x != null); let d = 0; for (let i = 1; i < n.length; i++) d += Math.max(0, n[i] - n[i - 1]); return n.length ? d : null },
  range: v => { const n = v.filter(x => x != null); return n.length ? Math.max(...n) - Math.min(...n) : null },
  diff: v => { const n = v.filter(x => x != null); return n.length ? n[n.length - 1] - n[0] : null }
}

export const calcOptions = [
  { value: 'lastNotNull', label: '最新非空值' }, { value: 'last', label: '最新值' }, { value: 'mean', label: '平均值' },
  { value: 'max', label: '最大值' }, { value: 'min', label: '最小值' }, { value: 'sum', label: '合计' }, { value: 'count', label: '数量' },
  { value: 'firstNotNull', label: '最早非空值' }, { value: 'delta', label: '累计增量' }, { value: 'range', label: '极差' }, { value: 'diff', label: '首尾差' }
]

export function reduceValues(values, calc = 'lastNotNull') {
  return (calcs[calc] || calcs.lastNotNull)(values || [])
}

// reduceFrames 为统计卡片与仪表生成每条序列的汇总值。
export function reduceFrames(frames, calc) {
  const chart = framesToChart(frames)
  if (chart.series.length) return chart.series.map(s => ({ name: s.name, value: reduceValues(s.values, calc) }))
  const out = []
  for (const frame of frames) {
    for (const field of frame.fields) {
      if (field.type === 'number') out.push({ name: fieldName(field, frame, frames.length > 1), value: reduceValues(field.values.map(v => (v == null ? null : Number(v))), calc) })
    }
  }
  return out
}

// framesToTable 支持 Prometheus 表格格式（多字段单帧）与多条时序（每行一条序列）。
const seriesLike = frame => frame.fields.length === 2 && Boolean(frameTimeField(frame)) && frame.fields.some(f => f.type === 'number')

export function framesToTable(frames = []) {
  if (frames.length <= 1 || !frames.every(seriesLike)) {
    const columns = []
    const rows = []
    for (const frame of frames) {
      const length = Math.max(0, ...frame.fields.map(f => f.values.length))
      for (const field of frame.fields) if (!columns.find(c => c.key === field.name)) columns.push({ key: field.name, title: field.name, type: field.type, unit: field.config?.unit })
      for (let i = 0; i < length; i++) rows.push(Object.fromEntries(frame.fields.map(field => [field.name, field.values[i]])))
    }
    return { columns, rows }
  }
  const chart = framesToChart(frames)
  const labelKeys = [...new Set(chart.series.flatMap(s => Object.keys(s.labels)))].filter(k => k !== '__name__').sort()
  return {
    columns: [{ key: '__series', title: '序列', type: 'string' }, ...labelKeys.map(k => ({ key: k, title: k, type: 'string' })), { key: '__value', title: '最新值', type: 'number' }],
    rows: chart.series.map(s => ({ __series: s.name, ...s.labels, __value: reduceValues(s.values, 'lastNotNull') }))
  }
}

// framesToLogs 读取 Grafana 的 Loki 帧（labels / Time / Line / tsNs）。
export function framesToLogs(frames = []) {
  const out = []
  for (const frame of frames) {
    const byName = Object.fromEntries(frame.fields.map(f => [f.name, f]))
    const line = byName.Line || byName.line || frame.fields.find(f => f.type === 'string' && f.name !== 'tsNs' && f.name !== 'id')
    const time = byName.Time || frameTimeField(frame)
    if (!line || !time) continue
    const labels = byName.labels
    for (let i = 0; i < line.values.length; i++) {
      const ms = Number(time.values[i])
      let entryLabels = labels?.values?.[i] || {}
      if (typeof entryLabels === 'string') { try { entryLabels = JSON.parse(entryLabels) } catch { entryLabels = {} } }
      out.push({ ts: String(byName.tsNs?.values?.[i] ?? ms * 1e6), timeMs: ms, line: String(line.values[i] ?? ''), labels: entryLabels })
    }
  }
  return out
}

const namedColors = { green: 'var(--success)', 'dark-green': 'var(--success)', 'semi-dark-green': 'var(--success)', red: 'var(--danger)', 'dark-red': 'var(--danger)', 'semi-dark-red': 'var(--danger)', orange: 'var(--warning)', 'dark-orange': 'var(--warning)', yellow: 'var(--warning)', 'dark-yellow': 'var(--warning)', blue: 'var(--info)', 'dark-blue': 'var(--info)', purple: 'var(--chart-5)', text: 'var(--text)', transparent: 'transparent' }

export function cssColor(color) {
  if (!color) return 'var(--text-strong)'
  return namedColors[color] || (/^(#[0-9a-f]{3,8}|rgba?\([\d\s.,%]+\))$/i.test(color) ? color : 'var(--text-strong)')
}

// thresholdColor 按 Grafana 绝对阈值返回颜色；百分比模式按 min/max 换算。
export function thresholdColor(value, thresholds, min = 0, max = 100) {
  const steps = thresholds?.steps || []
  if (value == null || !steps.length) return 'var(--text-strong)'
  const v = thresholds.mode === 'percentage' ? ((value - min) / ((max - min) || 1)) * 100 : value
  let color = steps[0]?.color
  for (const step of steps) if (step.value == null || v >= step.value) color = step.color
  return cssColor(color)
}
