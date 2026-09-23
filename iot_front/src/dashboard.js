const colors = ['#0071e3', '#30a46c', '#ff9f0a', '#af52de', '#5ac8fa', '#86868b'] /* 声明 colors。 */
export function count(value) { return Number.isFinite(Number(value)) ? Math.max(0, Number(value)) : 0 } /* 执行当前语句并推进处理流程。 */
export function deviceSegments(states = {}) { /* 执行当前语句并推进处理流程。 */
  const known = { ONLINE:['在线','#30a46c'], OFFLINE:['离线','#a1a1aa'], SUSPECTED_OFFLINE:['疑似离线','#ff9f0a'], NEVER_SEEN:['待连接','#d9e7f5'], UNKNOWN:['未知','#af52de'] } /* 声明 known。 */
  return Object.entries(known).map(([key, [name, color]]) => ({ key, name, color, count:count(states[key]) })) /* 返回当前处理结果。 */
    .filter(item => item.key !== 'UNKNOWN' || item.count > 0) /* 执行当前语句并推进处理流程。 */
    .concat(Object.keys(states).some(key => !known[key]) ? [{ key:'OTHER', name:'其他', color:'#af52de', count:Object.entries(states).filter(([key]) => !known[key]).reduce((sum, [,value]) => sum + count(value), 0) }] : []) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
export function ringSegments(items) { /* 执行当前语句并推进处理流程。 */
  const total = items.reduce((sum, item) => sum + count(item.count), 0) /* 声明 total。 */
  let offset = 0 /* 声明 offset。 */
  return items.map(item => { const percent = total ? count(item.count) / total * 100 : 0; const result = { ...item, percent, offset }; offset += percent; return result }) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
export function productBars(products = []) { /* 执行当前语句并推进处理流程。 */
  const sorted = products.map(item => ({ ...item, count:count(item.count) })).sort((a,b) => b.count-a.count || a.key.localeCompare(b.key)) /* 声明 sorted。 */
  const top = sorted.slice(0,5) /* 声明 top。 */
  if (sorted.length > 5) top.push({ key:'__other', name:'其他产品', count:sorted.slice(5).reduce((sum,item) => sum+item.count,0) }) /* 判断条件并选择处理分支。 */
  return top.map((item,index) => ({ ...item, color:colors[index] })) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
export function trendGeometry(trend = []) { /* 执行当前语句并推进处理流程。 */
  const max = Math.max(1, ...trend.map(item => count(item.count))) /* 声明 max。 */
  const magnitude = 10 ** Math.floor(Math.log10(max)) /* 声明 magnitude。 */
  const ceiling = Math.ceil(max / magnitude) * magnitude /* 声明 ceiling。 */
  const step = Math.max(1, Math.ceil(ceiling / 4)) /* 声明 step。 */
  const top = step * 4 /* 声明 top。 */
  const points = trend.map((item, i) => ({ ...item, count:count(item.count), x:44 + i * 628 / Math.max(1,trend.length-1), y:204-count(item.count)/top*172 })) /* 声明 points。 */
  return { points, top, ticks:Array.from({length:5},(_,i) => ({ value:step*i, y:204-i*43 })), line:points.map(p => `${p.x},${p.y}`).join(' '), area:points.length ? `44,204 ${points.map(p => `${p.x},${p.y}`).join(' ')} ${points.at(-1).x},204` : '' } /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
