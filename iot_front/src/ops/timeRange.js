// 统一时间范围与刷新周期。相对范围在每次查询时重新计算，自动刷新才会滑动窗口。

export const rangePresets = [
  { value: 'now-5m', label: '近 5 分钟', ms: 5 * 60e3 },
  { value: 'now-15m', label: '近 15 分钟', ms: 15 * 60e3 },
  { value: 'now-1h', label: '近 1 小时', ms: 3600e3 },
  { value: 'now-3h', label: '近 3 小时', ms: 3 * 3600e3 },
  { value: 'now-6h', label: '近 6 小时', ms: 6 * 3600e3 },
  { value: 'now-12h', label: '近 12 小时', ms: 12 * 3600e3 },
  { value: 'now-24h', label: '近 24 小时', ms: 24 * 3600e3 },
  { value: 'now-2d', label: '近 2 天', ms: 2 * 86400e3 },
  { value: 'now-7d', label: '近 7 天', ms: 7 * 86400e3 },
  { value: 'now-30d', label: '近 30 天', ms: 30 * 86400e3 }
]

export const refreshOptions = [
  { value: 0, label: '不自动刷新' },
  { value: 10e3, label: '每 10 秒' },
  { value: 30e3, label: '每 30 秒' },
  { value: 60e3, label: '每 1 分钟' },
  { value: 300e3, label: '每 5 分钟' }
]

const unitMs = { s: 1e3, m: 60e3, h: 3600e3, d: 86400e3, w: 604800e3, M: 30 * 86400e3, y: 365 * 86400e3 }

// parseRelative 解析 Grafana 风格的 now、now-6h、now-1d/d 等表达式；毫秒时间戳原样返回。
export function parseRelative(value, now = Date.now()) {
  if (typeof value === 'number') return value
  const text = String(value || '').trim()
  if (/^\d{10,}$/.test(text)) return Number(text)
  const match = text.match(/^now(?:([+-])(\d+)([smhdwMy]))?(?:\/([smhdwMy]))?$/)
  if (!match) {
    const parsed = Date.parse(text)
    return Number.isFinite(parsed) ? parsed : now
  }
  let result = now
  if (match[1]) result += (match[1] === '-' ? -1 : 1) * Number(match[2]) * unitMs[match[3]]
  if (match[4]) {
    const d = new Date(result)
    if (match[4] === 'd') d.setHours(0, 0, 0, 0)
    else if (match[4] === 'h') d.setMinutes(0, 0, 0)
    else if (match[4] === 'm') d.setSeconds(0, 0)
    result = d.getTime()
  }
  return result
}

// resolveRange 把 {from,to} 转为本次查询的毫秒时间窗。
export function resolveRange(range = {}, now = Date.now()) {
  const from = parseRelative(range.from ?? 'now-1h', now)
  const to = parseRelative(range.to ?? 'now', now)
  return from < to ? { from, to } : { from: to - 3600e3, to }
}

export function rangeLabel(range = {}) {
  const preset = rangePresets.find(item => item.value === range.from && (range.to ?? 'now') === 'now')
  if (preset) return preset.label
  const { from, to } = resolveRange(range)
  const fmt = ms => new Date(ms).toLocaleString('zh-CN', { hour12: false, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
  return `${fmt(from)} 至 ${fmt(to)}`
}

export function rangeDuration(range) {
  const { from, to } = resolveRange(range)
  return to - from
}

// 选中图表区域后得到绝对时间范围，供多个图表联动缩放。
export function absoluteRange(from, to) {
  return { from: Math.round(from), to: Math.round(to) }
}
