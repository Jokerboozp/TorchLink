// 运维中心数值、单位、时长与标签的显示格式。单位编号沿用 Grafana 的常用单位，
// 保证导入的仪表盘在平台中显示一致；未知单位原样作为后缀显示。

const SIZE_UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']
const DEC_UNITS = ['B', 'kB', 'MB', 'GB', 'TB', 'PB']

function fixed(value, decimals) {
  if (decimals != null && decimals !== '' && Number.isFinite(Number(decimals))) return value.toFixed(Number(decimals))
  if (Number.isInteger(value)) return String(value)
  const abs = Math.abs(value)
  if (abs === 0) return '0'
  if (abs >= 100) return value.toFixed(0)
  if (abs >= 10) return value.toFixed(1)
  if (abs >= 1) return value.toFixed(2)
  if (abs >= 0.01) return value.toFixed(3)
  return value.toPrecision(2)
}

function scaled(value, base, units, decimals) {
  let index = 0
  let v = value
  while (Math.abs(v) >= base && index < units.length - 1) { v /= base; index++ }
  return `${fixed(v, decimals)} ${units[index]}`
}

export function shortNumber(value, decimals) {
  const abs = Math.abs(value)
  if (abs >= 1e12) return `${fixed(value / 1e12, decimals)} 万亿`
  if (abs >= 1e8) return `${fixed(value / 1e8, decimals)} 亿`
  if (abs >= 1e4) return `${fixed(value / 1e4, decimals)} 万`
  return fixed(value, decimals)
}

export function formatDuration(seconds) {
  if (!Number.isFinite(seconds)) return '—'
  const s = Math.abs(seconds)
  if (s < 1) return `${Math.round(seconds * 1000)} 毫秒`
  if (s < 60) return `${fixed(seconds, s < 10 ? 1 : 0)} 秒`
  if (s < 3600) return `${Math.floor(seconds / 60)} 分 ${Math.round(seconds % 60)} 秒`
  if (s < 86400) return `${Math.floor(seconds / 3600)} 小时 ${Math.round((seconds % 3600) / 60)} 分`
  return `${Math.floor(seconds / 86400)} 天 ${Math.round((seconds % 86400) / 3600)} 小时`
}

export const unitOptions = [
  { value: 'short', label: '数值（自动缩写）' }, { value: 'none', label: '原始数值' }, { value: 'percent', label: '百分比（0-100）' },
  { value: 'percentunit', label: '百分比（0-1）' }, { value: 'bytes', label: '字节（IEC）' }, { value: 'decbytes', label: '字节（SI）' },
  { value: 'Bps', label: '字节/秒' }, { value: 's', label: '秒' }, { value: 'ms', label: '毫秒' }, { value: 'reqps', label: '请求/秒' },
  { value: 'ops', label: '次/秒' }, { value: 'dateTimeAsIso', label: '时间' }
]

// formatValue 按 Grafana 单位编号格式化数值；null 与非有限值显示为“—”。
export function formatValue(value, unit = 'short', decimals) {
  if (value == null || value === '' || !Number.isFinite(Number(value))) return '—'
  const v = Number(value)
  if (typeof unit === 'string' && unit.startsWith('suffix:')) return `${shortNumber(v, decimals)} ${unit.slice(7)}`
  if (typeof unit === 'string' && unit.startsWith('prefix:')) return `${unit.slice(7)}${shortNumber(v, decimals)}`
  switch (unit) {
    case 'none': case 'string': return fixed(v, decimals)
    case 'percent': return `${fixed(v, decimals ?? 1)}%`
    case 'percentunit': return `${fixed(v * 100, decimals ?? 1)}%`
    case 'bytes': return scaled(v, 1024, SIZE_UNITS, decimals)
    case 'decbytes': return scaled(v, 1000, DEC_UNITS, decimals)
    case 'Bps': case 'binBps': return `${scaled(v, 1024, SIZE_UNITS, decimals)}/s`
    case 'bps': return `${scaled(v, 1000, ['b', 'kb', 'Mb', 'Gb', 'Tb'], decimals)}/s`
    case 's': case 'dtdurations': return formatDuration(v)
    case 'ms': return v >= 1000 ? formatDuration(v / 1000) : `${fixed(v, decimals)} 毫秒`
    case 'reqps': return `${shortNumber(v, decimals)} 请求/秒`
    case 'ops': case 'rps': return `${shortNumber(v, decimals)} 次/秒`
    case 'dateTimeAsIso': case 'dateTimeAsSystem': return new Date(v).toLocaleString('zh-CN', { hour12: false })
    case '': case undefined: case null: case 'short': return shortNumber(v, decimals)
  }
  return `${shortNumber(v, decimals)} ${unit}`
}

// 概览指标使用中文单位描述，与后端 KPI 定义一致。
export function formatKpi(value, unit) {
  if (value == null) return '—'
  if (unit === '%') return `${fixed(value, 1)}%`
  if (unit === '秒') return formatDuration(value)
  if (unit === '条/秒' || unit === '行/秒') return `${fixed(value, 2)} ${unit}`
  return `${shortNumber(value, Number.isInteger(value) ? 0 : undefined)} ${unit}`
}

export function labelString(labels = {}, omit = []) {
  const parts = Object.keys(labels).filter(key => key !== '__name__' && !omit.includes(key)).sort().map(key => `${key}="${labels[key]}"`)
  return `${labels.__name__ || ''}{${parts.join(', ')}}`
}

// legend 支持 Prometheus 的 {{label}} 模板写法。
export function seriesName(labels = {}, legend = '') {
  if (legend) return legend.replace(/\{\{\s*([\w.]+)\s*\}\}/g, (_, key) => labels[key] ?? '')
  const keys = Object.keys(labels).filter(key => key !== '__name__')
  if (!keys.length) return labels.__name__ || '值'
  return labelString(labels)
}

export const logLevelTone = { error: 'danger', fatal: 'danger', warn: 'warning', info: 'info', debug: 'neutral', unknown: 'neutral' }
export const logLevelName = { error: '错误', fatal: '致命', warn: '警告', info: '信息', debug: '调试', unknown: '未知' }

// entryLevel 优先使用采集时写入的 level 标签，其次是 Loki 识别的级别。
export function entryLevel(entry = {}) {
  const raw = String(entry.labels?.level || entry.metadata?.detected_level || entry.parsed?.level || 'unknown').toLowerCase()
  if (raw.startsWith('warn')) return 'warn'
  if (raw.startsWith('err')) return 'error'
  if (['fatal', 'panic', 'critical', 'crit'].includes(raw)) return 'fatal'
  if (['debug', 'trace'].includes(raw)) return 'debug'
  if (['info', 'information', 'notice'].includes(raw)) return 'info'
  return 'unknown'
}

// 日志行若是 JSON 或 logfmt，拆出字段供详情展示；失败时返回空对象。
export function parseLogFields(line = '') {
  const text = String(line).trim()
  if (text.startsWith('{')) {
    try {
      const value = JSON.parse(text)
      if (value && typeof value === 'object' && !Array.isArray(value)) {
        return Object.fromEntries(Object.entries(value).map(([key, v]) => [key, typeof v === 'object' ? JSON.stringify(v) : String(v)]))
      }
    } catch { /* 不是合法 JSON 时尝试 logfmt。 */ }
  }
  const fields = {}
  for (const match of text.matchAll(/([A-Za-z_][\w.-]*)=("(?:[^"\\]|\\.)*"|[^\s"]*)/g)) {
    let value = match[2]
    if (value.startsWith('"')) { try { value = JSON.parse(value) } catch { value = value.slice(1, -1) } }
    fields[match[1]] = value
  }
  return Object.keys(fields).length >= 2 ? fields : {}
}

export function nsToLocal(ns) {
  const ms = Math.floor(Number(ns) / 1e6)
  const date = new Date(ms)
  const pad = (n, w = 2) => String(n).padStart(w, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}.${pad(date.getMilliseconds(), 3)}`
}

export function relativeTime(value) {
  const ms = typeof value === 'number' ? value : Date.parse(value)
  if (!Number.isFinite(ms)) return '—'
  const diff = (Date.now() - ms) / 1000
  if (Math.abs(diff) < 60) return diff >= 0 ? '刚刚' : '即将'
  if (diff < 0) return `${formatDuration(-diff)}后`
  return `${formatDuration(diff)}前`
}
