const colors = ['#0071e3', '#30a46c', '#ff9f0a', '#af52de', '#5ac8fa', '#86868b']
export function count(value) { return Number.isFinite(Number(value)) ? Math.max(0, Number(value)) : 0 }
export function deviceSegments(states = {}) {
  const known = { ONLINE:['在线','#30a46c'], OFFLINE:['离线','#a1a1aa'], SUSPECTED_OFFLINE:['疑似离线','#ff9f0a'], NEVER_SEEN:['待连接','#d9e7f5'], UNKNOWN:['未知','#af52de'] }
  return Object.entries(known).map(([key, [name, color]]) => ({ key, name, color, count:count(states[key]) }))
    .filter(item => item.key !== 'UNKNOWN' || item.count > 0)
    .concat(Object.keys(states).some(key => !known[key]) ? [{ key:'OTHER', name:'其他', color:'#af52de', count:Object.entries(states).filter(([key]) => !known[key]).reduce((sum, [,value]) => sum + count(value), 0) }] : [])
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
  return top.map((item,index) => ({ ...item, color:colors[index] }))
}
export function trendGeometry(trend = []) {
  const max = Math.max(1, ...trend.map(item => count(item.count)))
  const magnitude = 10 ** Math.floor(Math.log10(max))
  const ceiling = Math.ceil(max / magnitude) * magnitude
  const step = Math.max(1, Math.ceil(ceiling / 4))
  const top = step * 4
  const points = trend.map((item, i) => ({ ...item, count:count(item.count), x:44 + i * 628 / Math.max(1,trend.length-1), y:204-count(item.count)/top*172 }))
  return { points, top, ticks:Array.from({length:5},(_,i) => ({ value:step*i, y:204-i*43 })), line:points.map(p => `${p.x},${p.y}`).join(' '), area:points.length ? `44,204 ${points.map(p => `${p.x},${p.y}`).join(' ')} ${points.at(-1).x},204` : '' }
}
