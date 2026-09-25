import test from 'node:test' /* 引入当前代码需要的依赖。 */
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */
import fs from 'node:fs' /* 引入当前代码需要的依赖。 */
import vm from 'node:vm' /* 引入当前代码需要的依赖。 */
import { ref, computed } from 'vue' /* 引入当前代码需要的依赖。 */
import { deviceSegments, ringSegments, productBars, trendGeometry, statusSegments, dashboardDistributions } from '../src/dashboard.js' /* 引入当前代码需要的依赖。 */

test('device ring preserves all states including future unknown codes', () => { /* 执行当前语句并推进处理流程。 */
  const rows=ringSegments(deviceSegments({ONLINE:2,OFFLINE:1,SUSPECTED_OFFLINE:3,NEVER_SEEN:4,FUTURE:2})) /* 声明 rows。 */
  assert.equal(rows.reduce((sum,v)=>sum+v.count,0),12) /* 验证实际结果符合预期。 */
  assert.equal(Math.round(rows.reduce((sum,v)=>sum+v.percent,0)),100) /* 验证实际结果符合预期。 */
  assert.equal(rows.find(v=>v.key==='SUSPECTED_OFFLINE').name,'疑似离线') /* 验证实际结果符合预期。 */
  assert.ok(ringSegments(deviceSegments()).every(v=>v.percent===0 && v.offset===0)) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
test('product chart keeps top five and accounts for every remaining product',()=>{ /* 执行当前语句并推进处理流程。 */
  const products=Array.from({length:9},(_,i)=>({key:String(i),name:`Product ${i}`,count:i+1})) /* 声明 products。 */
  const bars=productBars(products) /* 声明 bars。 */
  assert.equal(bars.length,6);assert.equal(bars[0].count,9);assert.equal(bars[5].count,10) /* 验证实际结果符合预期。 */
  assert.equal(bars.reduce((sum,v)=>sum+v.count,0),45) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
test('trend handles no alarms, small counts and spikes without fractional count ticks',()=>{ /* 执行当前语句并推进处理流程。 */
  for (const values of [[],[0,0],[1,0,1],[0,12001,0]]) { /* 循环处理当前数据。 */
    const chart=trendGeometry(values.map((count,i)=>({date:String(i),count}))) /* 声明 chart。 */
    assert.ok(chart.ticks.every(t=>Number.isInteger(t.value))) /* 验证实际结果符合预期。 */
    assert.ok(chart.points.every(p=>Number.isFinite(p.x)&&p.y>=32&&p.y<=204)) /* 验证实际结果符合预期。 */
  } /* 结束当前表达式或代码块。 */
}) /* 结束当前表达式或代码块。 */
function setup(api) { /* 定义 setup 函数。 */
  const script=fs.readFileSync(new URL('../src/views/DashboardView.vue',import.meta.url),'utf8').match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm,'') /* 声明 script。 */
  let cleanup /* 声明 cleanup。 */
  const context=vm.createContext({ref,computed,api,deviceSegments,ringSegments,productBars,dashboardDistributions,alarmLevels:{},AbortController,setTimeout,clearTimeout,notifyError(){},defineEmits:()=>()=>{},onMounted(){},onBeforeUnmount(fn){cleanup=fn},window:{removeEventListener(){}}}) /* 声明 context。 */
  return {...vm.runInContext(script+'\n;({load,data,days,loading,loadError})',context),cleanup:()=>cleanup()} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
test('range switching rejects late responses and retains last good snapshot on failure',async()=>{ /* 执行当前语句并推进处理流程。 */
  const requests=[] /* 声明 requests。 */
  const c=setup(path=>path.includes('/alarms?')?Promise.resolve({items:[]}):new Promise((resolve,reject)=>requests.push({resolve,reject}))) /* 声明 c。 */
  const first=c.load();c.days.value=30;const second=c.load() /* 声明 first。 */
  requests[1].resolve({days:30});await second;requests[0].resolve({days:7});await first /* 执行当前语句并推进处理流程。 */
  assert.equal(c.data.value.days,30) /* 验证实际结果符合预期。 */
  const failed=c.load();requests[2].reject(new Error('offline'));await failed /* 声明 failed。 */
  assert.equal(c.data.value.days,30);assert.match(c.loadError.value,/刷新失败/);assert.equal(c.loading.value,false) /* 验证实际结果符合预期。 */
  const pending=c.load();c.cleanup();requests[3].resolve({days:7});await pending;assert.equal(c.data.value.days,30) /* 声明 pending。 */
}) /* 结束当前表达式或代码块。 */


test('new distributions preserve totals, unknown codes and type ranking overflow', () => {
  const source = { alarmStatuses:{ACTIVE:7, ACKED:2, FUTURE:1}, connections:{CONNECTED:3, UNKNOWN:1}, dataStatuses:{ACTIVE:2,SILENT:2}, alarmTypes:{FIRE:9,SMOKE_DETECTED:8,DEVICE_FAULT:7,DEVICE_OFFLINE:6,HIGH_TEMPERATURE:5,GAS_LEAK:4,WATER_PRESSURE_LOW:3,FUTURE:2,UNKNOWN:1} }
  const charts = dashboardDistributions(source)
  assert.ok(charts.every(chart => chart.available))
  assert.deepEqual(charts.map(chart => chart.items.reduce((sum,item) => sum+item.count,0)), [10,45,4,4])
  assert.equal(charts[0].items.at(-1).name, '其他')
  assert.equal(charts[1].items.length, 6)
  assert.equal(charts[1].items.at(-1).count, 10)
  assert.equal(charts[1].items[0].name, '火灾告警')
  assert.ok(dashboardDistributions().every(chart => !chart.available))
  assert.ok(dashboardDistributions({alarmStatuses:{},alarmTypes:{},connections:{},dataStatuses:{}}).every(chart => chart.available && chart.items.every(item => item.count === 0)))
  assert.equal(statusSegments({ACTIVE:-1,FUTURE:'bad'}, {ACTIVE:'活跃'})[0].count,0)
})
