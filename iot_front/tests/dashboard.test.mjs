import test from 'node:test'
import assert from 'node:assert/strict'
import fs from 'node:fs'
import vm from 'node:vm'
import { ref, computed } from 'vue'
import { deviceSegments, ringSegments, productBars, trendGeometry } from '../src/dashboard.js'

test('device ring preserves all states including future unknown codes', () => {
  const rows=ringSegments(deviceSegments({ONLINE:2,OFFLINE:1,SUSPECTED_OFFLINE:3,NEVER_SEEN:4,FUTURE:2}))
  assert.equal(rows.reduce((sum,v)=>sum+v.count,0),12)
  assert.equal(Math.round(rows.reduce((sum,v)=>sum+v.percent,0)),100)
  assert.equal(rows.find(v=>v.key==='SUSPECTED_OFFLINE').name,'疑似离线')
  assert.ok(ringSegments(deviceSegments()).every(v=>v.percent===0 && v.offset===0))
})
test('product chart keeps top five and accounts for every remaining product',()=>{
  const products=Array.from({length:9},(_,i)=>({key:String(i),name:`Product ${i}`,count:i+1}))
  const bars=productBars(products)
  assert.equal(bars.length,6);assert.equal(bars[0].count,9);assert.equal(bars[5].count,10)
  assert.equal(bars.reduce((sum,v)=>sum+v.count,0),45)
})
test('trend handles no alarms, small counts and spikes without fractional count ticks',()=>{
  for (const values of [[],[0,0],[1,0,1],[0,12001,0]]) {
    const chart=trendGeometry(values.map((count,i)=>({date:String(i),count})))
    assert.ok(chart.ticks.every(t=>Number.isInteger(t.value)))
    assert.ok(chart.points.every(p=>Number.isFinite(p.x)&&p.y>=32&&p.y<=204))
  }
})
function setup(api) {
  const script=fs.readFileSync(new URL('../src/views/DashboardView.vue',import.meta.url),'utf8').match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm,'')
  let cleanup
  const context=vm.createContext({ref,computed,api,deviceSegments,ringSegments,productBars,alarmLevels:{},AbortController,setTimeout,clearTimeout,notifyError(){},defineEmits:()=>()=>{},onMounted(){},onBeforeUnmount(fn){cleanup=fn},window:{removeEventListener(){}}})
  return {...vm.runInContext(script+'\n;({load,data,days,loading,loadError})',context),cleanup:()=>cleanup()}
}
test('range switching rejects late responses and retains last good snapshot on failure',async()=>{
  const requests=[]
  const c=setup(path=>path.includes('/alarms?')?Promise.resolve({items:[]}):new Promise((resolve,reject)=>requests.push({resolve,reject})))
  const first=c.load();c.days.value=30;const second=c.load()
  requests[1].resolve({days:30});await second;requests[0].resolve({days:7});await first
  assert.equal(c.data.value.days,30)
  const failed=c.load();requests[2].reject(new Error('offline'));await failed
  assert.equal(c.data.value.days,30);assert.match(c.loadError.value,/刷新失败/);assert.equal(c.loading.value,false)
  const pending=c.load();c.cleanup();requests[3].resolve({days:7});await pending;assert.equal(c.data.value.days,30)
})
