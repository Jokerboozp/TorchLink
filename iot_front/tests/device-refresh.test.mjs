import {test} from 'node:test'
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import vm from 'node:vm'

async function devicesView(globals) {
 const source=await readFile(new URL('../src/views/DevicesView.vue',import.meta.url),'utf8')
 const script=source.split('<script setup>')[1].split('</script>')[0].replace(/^import .*$/gm,'')
 const context=vm.createContext({ref:value=>({value}),reactive:v=>v,computed:fn=>({get value(){return fn()}}),defineEmits:()=>()=>{},pretty:JSON.stringify,onMounted:()=>{},onBeforeUnmount:()=>{},window:{addEventListener(){}},sessionStorage:{getItem:()=>null,removeItem(){}},notifyError:e=>{throw e},setTimeout,clearTimeout,URLSearchParams,...globals})
 vm.runInContext(script+'\nglobalThis.state={load,loading,deviceTab,filters,registryPage,registryTotal,registry,unregistered,pendingCount,changeFilter,changeRegistryPage};',context)
 return context
}

test('实时消息不重载设备列表，手动刷新仍读取最新数据',async()=>{
 let mounted,requests=0;const events=new Map()
 const context=await devicesView({onMounted:fn=>mounted=fn,window:{addEventListener:(k,fn)=>events.set(k,fn)},api:async()=>{requests++;return{items:[]}},apiAll:async()=>{requests++;return{items:[]}}})
 mounted();await new Promise(r=>setTimeout(r,0));const initial=requests
 for(let i=0;i<10;i++)events.get('iot:realtime')({detail:{topic:'device.state',payload:{deviceId:'demo'}}})
 await new Promise(r=>setTimeout(r,0))
 assert.equal(requests,initial,'实时上报不应重新请求整张设备列表')
 assert.equal(context.state.loading.value,false)
 await context.state.load();assert.ok(requests>initial,'手动刷新必须仍然有效')
})

test('设备分组、类型、关键字和运行状态交给服务端筛选，切换筛选回到第一页',async()=>{
 const requests=[]
 const context=await devicesView({
  api:async path=>{requests.push(path);return path.startsWith('/api/v1/devices')?{items:[{deviceId:'raw-1'}],total:3}:{items:[{device:{id:'d1'}}],total:41}},
  apiAll:async()=>({items:[{id:'smoke',category:'smoke'}]})
 })
 const s=context.state
 const registryQuery=()=>new URL(requests.filter(path=>path.startsWith('/api/v1/device-registry')).at(-1),'http://audit.invalid').searchParams
 await s.load()
 assert.equal(s.registryTotal.value,41);assert.equal(s.pendingCount.value,3)
 assert.equal(registryQuery().get('role'),null);assert.equal(registryQuery().get('page'),'1')
 s.changeRegistryPage(3);await new Promise(r=>setTimeout(r,0))
 assert.equal(registryQuery().get('page'),'3')
 s.deviceTab.value='CHILD';s.filters.category='smoke';s.filters.runtime='ALARM';s.filters.q=' 一层 '
 s.changeFilter();await new Promise(r=>setTimeout(r,0))
 const query=registryQuery()
 assert.equal(s.registryPage.value,1);assert.equal(query.get('page'),'1')
 assert.equal(query.get('role'),'CHILD');assert.equal(query.get('category'),'smoke');assert.equal(query.get('runtime'),'ALARM');assert.equal(query.get('q'),'一层')
 s.deviceTab.value='pending';s.changeFilter();await new Promise(r=>setTimeout(r,0))
 assert.match(requests.at(-1),/^\/api\/v1\/devices\?unregistered=true&page=1&pageSize=20$/)
 assert.equal(s.unregistered.value[0].deviceId,'raw-1')
})
