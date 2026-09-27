import { alarmStatuses, alarmTypes, connectionStatuses, dataStatuses } from './labels.js'

// 运行总览的图表数据；颜色只引用 tokens.css 中的变量：设备状态用状态色，产品分布为单一度量用单色。
export function statusSegments(values = {}, names = {}, colors = {}) {
  const items = Object.entries(names).map(([key, name]) => ({ key, name, count:count(values[key]), color:colors[key] || 'var(--gray-400)' }))
  const other = Object.entries(values).filter(([key]) => !names[key]).reduce((sum, [, value]) => sum + count(value), 0)
  if (other) items.push({ key:'__other', name:'其他', count:other, color:'var(--gray-400)' })
  return items
}

export function dashboardDistributions(stats = {}) {
  const typeCounts = new Map()
  for (const [key, value] of Object.entries(stats.alarmTypes || {})) {
    const name = alarmTypes[key] || '其他告警类型'
    typeCounts.set(name, (typeCounts.get(name) || 0) + count(value))
  }
  const types = [...typeCounts].map(([name, count]) => ({key:name, name, count, color:'var(--primary)'}))
    .filter(item => item.count > 0).sort((a,b) => b.count-a.count || a.key.localeCompare(b.key))
  const top = types.slice(0,5)
  if (types.length > 5) top.push({key:'__remaining', name:'其余类型', count:types.slice(5).reduce((sum,item) => sum+item.count,0), color:'var(--gray-400)'})
  return [
    { key:'alarmStatuses', title:'告警处置分布', subtitle:'所选时段新增告警的当前处置状态', unit:'条', variant:'ring', items:statusSegments(stats.alarmStatuses, alarmStatuses, {ACTIVE:'var(--danger)', ACKED:'var(--warning)', RECOVERED:'var(--success)', CLOSED:'var(--primary)'}) },
    { key:'alarmTypes', title:'告警类型排行', subtitle:'按新增告警记录统计，展示前五类及其余类型', unit:'条', variant:'bars', items:top },
    { key:'connections', title:'设备连接状态', subtitle:'当前连接快照，不代表业务在线状态', unit:'台', variant:'stack', items:statusSegments(stats.connections, connectionStatuses, {CONNECTED:'var(--success)', DISCONNECTED:'var(--gray-500)'}) },
    { key:'dataStatuses', title:'设备数据状态', subtitle:'当前上报活跃度，未产生状态的设备计入未知', unit:'台', variant:'ring', items:statusSegments(stats.dataStatuses, dataStatuses, {ACTIVE:'var(--success)', SILENT:'var(--warning)'}) },
  ].map(item => ({...item, available:stats[item.key] != null}))
}
export function count(value) { return Number.isFinite(Number(value)) ? Math.max(0, Number(value)) : 0 }
// 大数按“万 / 亿”缩写，避免撑破统计卡片、环形图中心和坐标轴；完整数值由调用处放在 title 中。
export function compactCount(value, digits = 1) {
  const n = count(value)
  if (n < 1e4) return Math.round(n).toLocaleString('zh-CN')
  let scaled = n / 1e4, unit = '万'
  if (Math.round(scaled) >= 1e4) { scaled = n / 1e8; unit = '亿' }
  return `${Number(scaled.toFixed(scaled < 100 ? digits : 0)).toLocaleString('zh-CN')}${unit}`
}
export function deviceSegments(states = {}) {
  const known = { ONLINE:['在线','var(--success)'], OFFLINE:['离线','var(--gray-500)'], SUSPECTED_OFFLINE:['疑似离线','var(--warning)'], NEVER_SEEN:['待连接','var(--gray-400)'], UNKNOWN:['未知','var(--info)'] }
  return Object.entries(known).map(([key, [name, color]]) => ({ key, name, color, count:count(states[key]) }))
    .filter(item => item.key !== 'UNKNOWN' || item.count > 0)
    .concat(Object.keys(states).some(key => !known[key]) ? [{ key:'OTHER', name:'其他', color:'var(--info)', count:Object.entries(states).filter(([key]) => !known[key]).reduce((sum, [,value]) => sum + count(value), 0) }] : [])
}
export function ringSegments(items) {
  const total = items.reduce((sum, item) => sum + count(item.count), 0)
  let offset = 0
  return items.map(item => { const percent = total ? count(item.count) / total * 100 : 0; const result = { ...item, percent, offset }; offset += percent; return result })
}
export function productBars(products = []) {
  const sorted = products.map(item => ({ ...item, count:count(item.count) })).sort((a,b) => b.count-a.count || a.key.localeCompare(b.key))
  const top = sorted.slice(0,5)
  if (sorted.length > 5) top.push({ key:'__other', name:'其他产品', count:sorted.slice(5).reduce((sum,item) => sum+item.count,0) })
  // 同一度量只用一种颜色；“其他产品”用中性色。
  return top.map(item => ({ ...item, color:item.key === '__other' ? 'var(--gray-400)' : 'var(--primary)' }))
}
export function trendGeometry(trend = []) {
  const max = Math.max(1, ...trend.map(item => count(item.count)))
  const magnitude = 10 ** Math.floor(Math.log10(max))
  const ceiling = Math.ceil(max / magnitude) * magnitude
  const step = Math.max(1, Math.ceil(ceiling / 4))
  const top = step * 4
  const labels = Array.from({length:5},(_,i) => compactCount(step*i, 2))
  // 纵轴刻度文字右对齐在绘图区左侧 14 处，按最长刻度估算宽度，避免大数刻度超出图表左边界。
  const left = Math.max(44, 18 + Math.ceil(Math.max(...labels.map(text => [...text].reduce((sum, ch) => sum + (/[\u4e00-\u9fff]/.test(ch) ? 12 : 7), 0)))))
  const width = 672 - left
  const points = trend.map((item, i) => ({ ...item, count:count(item.count), x:left + i * width / Math.max(1,trend.length-1), y:204-count(item.count)/top*172 }))
  return { points, top, left, ticks:labels.map((label,i) => ({ value:step*i, label, y:204-i*43 })), line:points.map(p => `${p.x},${p.y}`).join(' '), area:points.length ? `${left},204 ${points.map(p => `${p.x},${p.y}`).join(' ')} ${points.at(-1).x},204` : '' }
}
