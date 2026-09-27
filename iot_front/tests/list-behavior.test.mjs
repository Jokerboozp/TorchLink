import fs from 'node:fs'
import vm from 'node:vm'
import assert from 'node:assert/strict'
import test from 'node:test'
import { loadAllPages } from '../src/listPagination.js'
import { createClientId } from '../src/clientId.js'
import { computed, reactive, ref, watch } from 'vue'
import { readFile } from 'node:fs/promises'
import { alarmNavigation, alarmQuery } from '../src/alarmNavigation.js'
import { compactCount, dashboardDistributions, deviceSegments, productBars, ringSegments, statusSegments, trendGeometry } from '../src/dashboard.js'

const root = new URL('../src/views/', import.meta.url)
// Execute the real setup code with Vue reactivity; replace external I/O and
// lifecycle hooks so response ordering is deterministic without a browser.
function component(file, api, exports, notifyError = e => { throw e }, base = root) {
  const source = fs.readFileSync(new URL(file, base), 'utf8').match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm, '')
  const context = vm.createContext({ref, reactive, computed, watch, defineProps:()=>({section:'profiles'}), api, apiAll:(path, options)=>loadAllPages(api,path,options), onMounted(){}, onBeforeUnmount(){}, defineEmits:()=>()=>{}, pretty:JSON.stringify, parseJSON:JSON.parse, crypto:{getRandomValues:bytes=>crypto.getRandomValues(bytes)}, createClientId:()=>createClientId({getRandomValues:bytes=>crypto.getRandomValues(bytes)}), notifyError, UiMessage:{success(){},warning(){},info(){}}, sessionStorage:{getItem(){return null}}, URLSearchParams, setTimeout, clearTimeout}) /* 为 Naive UI 消息入口提供无副作用替身。 */
  return vm.runInContext(source + '\n;({' + exports + '})', context)
}
const items = Array.from({length:101}, (_, i)=>({id:`item-${i+1}`,name:`Item ${i+1}`}))

test('产品可生成模板编号',async()=>{
  const requests=[]
  const c=component('ProductsView.vue',async(url,options)=>{if(options?.method==='POST')requests.push({url,body:JSON.parse(options.body)});return {items:[],total:0}},'form,save')
  Object.assign(c.form,{name:'HTTP 演示',protocolPackageId:'protocol'})
  await c.save()
  assert.equal(requests.length,1)
  assert.equal(requests[0].url,'/api/v1/products')
  assert.match(requests[0].body.id,/^product_[0-9a-f]{12}$/)
})
test('设备列表只编辑已有设备，新设备统一走添加向导',async()=>{
  const requests=[]
  const c=component('DevicesView.vue',async(url,options)=>{if(options?.method)requests.push({url,method:options.method,body:JSON.parse(options.body)});return {items:[],total:0}},'form,save')
  Object.assign(c.form,{name:'新设备',productId:'product-1'})
  await c.save()
  assert.equal(requests.length,0)
  Object.assign(c.form,{id:'real device/01',name:'一层烟感'})
  await c.save()
  assert.equal(requests.length,1)
  assert.equal(requests[0].method,'PUT')
  assert.equal(requests[0].url,'/api/v1/device-registry/real%20device%2F01')
})
test('协议列表分页覆盖所有记录并在列表缩小时修正当前页',async()=>{
 const c=component('ProtocolsView.vue',async()=>({items:[]}),'protocols,protocolPage,protocolPageSize,pagedProtocols')
 c.protocols.value=Array.from({length:45},(_,i)=>({definition:{id:`protocol-${i+1}`},releases:[]}))
 assert.equal(c.pagedProtocols.value.length,20)
 c.protocolPage.value=2;assert.equal(c.pagedProtocols.value[0].definition.id,'protocol-21')
 c.protocolPage.value=3;assert.equal(c.pagedProtocols.value.length,5)
 c.protocols.value=c.protocols.value.slice(0,3);await Promise.resolve();assert.equal(c.protocolPage.value,1)
 assert.equal(c.pagedProtocols.value.length,3)
})
function paginated(path) {
  const url = new URL(path, 'http://audit.invalid')
  const size = Math.min(100, Number(url.searchParams.get('pageSize') || 20))
  const start = (Number(url.searchParams.get('page') || 1)-1)*size
  return {items:items.slice(start,start+size),total:items.length}
}
test('camera association searches devices on the server instead of loading the whole registry', async()=>{
  const requests=[]
  const c=component('CameraMappingsView.vue', async path=>{requests.push(path); if(!path.includes('device-registry')) return {items:[],total:0}; const q=new URL(path,'http://x').searchParams.get('q'); return {items:items.filter(x=>!q||x.id.includes(q)).slice(0,50),total:items.length}}, 'load,open,camera,devices,searchDevices')
  await c.load()
  assert.ok(!requests.some(path=>path.includes('device-registry')), 'the camera list must not wait for the device registry')
  await c.searchDevices('item-101')
  assert.ok(c.devices.value.some(x=>x.id==='item-101'), 'a device beyond the first pages must be reachable by search')
  assert.ok(requests.every(path=>!path.includes('device-registry') || /pageSize=50/.test(path)), 'searches stay bounded')
  c.open({cameraId:'cam',deviceId:'far-away-device'})
  await c.searchDevices('')
  assert.ok(c.devices.value.some(x=>x.id==='far-away-device'), 'the linked device stays selectable when it is not in the results')
})
test('rule editor offers product 101', async()=>{
  const c=component('RulesView.vue', async path=>path.includes('/products') ? paginated(path) : {items:[],total:0}, 'load,products')
  await c.load()
  assert.ok(c.products.value.some(x=>x.id==='item-101'), `only ${c.products.value.length}/101 product options loaded`)
})
test('device registration offers product 101', async()=>{
  const c=component('DevicesView.vue', async path=>path.includes('/products') ? paginated(path) : {items:[],total:0}, 'load,products')
  await c.load()
  assert.ok(c.products.value.some(x=>x.id==='item-101'))
})
test('product editor offers published protocol 101', async()=>{
  const c=component('ProductsView.vue', async path=>path.includes('/protocol-packages') ? {...paginated(path),items:paginated(path).items.map(item=>({...item,status:'PUBLISHED'}))} : {items:[],total:0}, 'load,protocols')
  await c.load()
  assert.ok(c.protocols.value.some(x=>x.id==='item-101'))
})

test('editing a product preserves its existing thing model', async()=>{
  let saved
  const model={properties:[{identifier:'temperature',dataType:'number'}],commands:[{identifier:'reset'}]}
  const c=component('ProductsView.vue', async (_path,options)=>{
    if(options?.method==='PUT') saved=JSON.parse(options.body)
    return {items:[],total:0}
  }, 'edit,form,save')
  c.edit({id:'product-1',name:'传感器',protocolPackageId:'iot-standard@1.0.0',thingModel:model})
  c.form.description='更新说明'
  await c.save()
  assert.deepEqual(saved.thingModel,model)
})

test('product pagination reuses the loaded protocol catalog', async()=>{
  const requests=[]
  const c=component('ProductsView.vue', async path=>{
    requests.push(path)
    return {items:[],total:60}
  }, 'load,changePage,changePageSize')
  await c.load()
  const catalogRequests=()=>requests.filter(path=>path.includes('/protocol-packages') || path==='/api/v2/protocols').length
  assert.equal(catalogRequests(),2)
  c.changePage(2)
  await new Promise(resolve=>setImmediate(resolve))
  c.changePageSize(50)
  await new Promise(resolve=>setImmediate(resolve))
  assert.equal(catalogRequests(),2)
  assert.ok(requests.some(path=>path.includes('page=2')))
})
test('product pagination does not discard an in-flight protocol catalog', async()=>{
  let finishCatalog
  const c=component('ProductsView.vue', async path=>{
    if(path.includes('/protocol-packages')) return new Promise(resolve=>{finishCatalog=resolve})
    return {items:[],total:60}
  }, 'load,changePage,protocols')
  const first=c.load()
  await new Promise(resolve=>setImmediate(resolve))
  c.changePage(2)
  finishCatalog({items:[{id:'late-protocol',name:'最新协议',status:'PUBLISHED'}],total:1})
  await first
  assert.ok(c.protocols.value.some(item=>item.id==='late-protocol'))
})
test('child device can choose a gateway outside current registry page', async()=>{
  const rows=Array.from({length:101},(_,i)=>({device:{id:`device-${i+1}`,deviceRole:'GATEWAY'}}))
  const paths=[]
  const c=component('DevicesView.vue', async path=>{
    paths.push(path)
    if (!path.includes('device-registry')) return {items:[],total:0}
    const query=new URL(path,'http://audit.invalid').searchParams
    const size=Number(query.get('pageSize') || 20)
    const start=(Number(query.get('page') || 1)-1)*size
    return {items:rows.slice(start,start+size),total:rows.length}
  }, 'loadGateways,gateways')
  await c.loadGateways()
  assert.ok(paths.every(path=>new URL(path,'http://audit.invalid').searchParams.get('role')==='GATEWAY'))
  assert.ok(c.gateways.value.some(item=>item.device.id==='device-101'), `only ${c.gateways.value.length}/101 gateways loaded`)
})
test('camera pagination retains latest requested page when responses arrive out of order', async()=>{
  const pending=[]
  const c=component('CameraMappingsView.vue', path=>path.includes('device-registry') ? Promise.resolve({items:[]}) : new Promise(resolve=>pending.push(resolve)), 'load,page,cameras')
  const first=c.load()
  c.page.value=2
  const second=c.load()
  pending[1]({items:[{cameraId:'page-2'}],total:40})
  await second
  pending[0]({items:[{cameraId:'page-1'}],total:40})
  await first
  assert.equal(c.cameras.value[0].cameraId,'page-2',`pager=${c.page.value}, displayed=${c.cameras.value[0].cameraId}`)
})
test('stale failure does not notify or stop the current camera loading state', async()=>{
  const pending=[]
  const errors=[]
  const c=component('CameraMappingsView.vue', path=>path.includes('device-registry') ? Promise.resolve({items:[]}) : new Promise((resolve,reject)=>pending.push({resolve,reject})), 'load,loading,cameras', e=>errors.push(e))
  const first=c.load()
  const second=c.load()
  pending[0].reject(new Error('old request failed'))
  await first
  assert.equal(errors.length,0)
  assert.equal(c.loading.value,true)
  pending[1].resolve({items:[{cameraId:'current'}],total:1})
  await second
  assert.equal(c.loading.value,false)
  const current=c.load()
  pending[2].reject(new Error('current request failed'))
  await current
  assert.equal(errors.length,1)
  assert.equal(c.loading.value,false)
})
test('control: camera page remains correct when responses arrive in order', async()=>{
  const pending=[]
  const c=component('CameraMappingsView.vue', path=>path.includes('device-registry') ? Promise.resolve({items:[]}) : new Promise(resolve=>pending.push(resolve)), 'load,page,cameras')
  const first=c.load()
  c.page.value=2
  const second=c.load()
  pending[0]({items:[{cameraId:'page-1'}],total:40})
  await first
  pending[1]({items:[{cameraId:'page-2'}],total:40})
  await second
  assert.equal(c.cameras.value[0].cameraId,'page-2')
})

for (const [file, endpoint, pageKey, rowsKey, totalKey] of [
  ['DevicesView.vue', '/device-registry', 'registryPage', 'registry', 'registryTotal'],
  ['ProductsView.vue', '/products', 'productPage', 'products', 'productTotal'],
  ['RulesView.vue', '/rules', 'page', 'rules', 'total']
]) {
  test(`${file} ignores old page data and totals`, async()=>{
    const pending=[]
    const c=component(file, path=>{
      const url=new URL(path,'http://audit.invalid')
      if (url.pathname.endsWith(endpoint) && (file === 'DevicesView.vue' || url.searchParams.get('pageSize')==='20')) {
        return new Promise(resolve=>pending.push(resolve))
      }
      return Promise.resolve({items:[],total:0})
    }, `load,loading,${pageKey},${rowsKey},${totalKey}`)
    const first=c.load()
    c[pageKey].value=2
    const second=c.load()
    const deviceList = file === 'DevicesView.vue'
    pending[1]({items:[deviceList ? {id:'new',device:{id:'new',deviceRole:'DIRECT'}} : {id:'new'}],total:deviceList ? 1 : 40})
    await second
    pending[0]({items:[deviceList ? {id:'old',device:{id:'old',deviceRole:'CHILD'}} : {id:'old'}],total:deviceList ? 1 : 20})
    await first
    assert.equal(c[rowsKey].value[0].id,'new')
    assert.equal(c[totalKey].value,deviceList ? 1 : 40)
    assert.equal(c.loading.value,false)
  })
}

test('association pagination preserves filters and stops when the dataset shrinks', async()=>{
  const calls=[]
  const result=await loadAllPages(async path=>{
    const query=new URL(path,'http://audit.invalid').searchParams
    calls.push(query)
    return calls.length===1 ? {items:items.slice(0,100),total:101,mode:'test'} : {items:[],total:100}
  }, '/catalog?status=ENABLED&offset=50&limit=10')
  assert.equal(calls.length,2)
  assert.equal(calls[1].get('status'),'ENABLED')
  assert.equal(calls[1].get('page'),'2')
  assert.equal(calls[0].has('offset'),false)
  assert.equal(result.items.length,100)
  assert.equal(result.mode,'test')
})

test('association pagination supports count-only and uncounted responses', async()=>{
  for (const counted of [true,false]) {
    const result=await loadAllPages(async path=>{
      const data=paginated(path)
      return {items:data.items,...(counted ? {count:data.total} : {})}
    }, '/catalog')
    assert.equal(result.items.length,101)
  }
})

test('association pagination propagates a later-page failure instead of returning partial options', async()=>{
  await assert.rejects(loadAllPages(async path=>{
    if (new URL(path,'http://audit.invalid').searchParams.get('page')==='2') throw new Error('catalog unavailable')
    return paginated(path)
  }, '/catalog'), /catalog unavailable/)
})

test('device save suppresses duplicate submission and preserves fields after failure', async()=>{
  let rejectSave, writes=0
  const errors=[]
  const c=component('DevicesView.vue', async (path,options)=>{
    if (options?.method === 'PUT') { writes++; return new Promise((_,reject)=>{rejectSave=reject}) }
    return {items:[],total:0}
  }, 'save,form,saving,dialog', e=>errors.push(e))
  Object.assign(c.form,{id:'device-fixed',name:'烟感',productId:'product-1'})
  c.dialog.value=true
  const first=c.save()
  await c.save()
  assert.equal(writes,1)
  assert.equal(c.saving.value,true)
  rejectSave(new Error('offline')); await first
  assert.equal(c.saving.value,false)
  assert.equal(c.dialog.value,true)
  assert.equal(c.form.name,'烟感')
  assert.equal(errors.length,1)
})

test('device edit clears stale gateway links when the role changes', async()=>{
  let saved
  const c=component('DevicesView.vue', async(path,options)=>{
    if (options?.method === 'PUT') { saved=JSON.parse(options.body); return {} }
    return {items:[],total:0}
  }, 'save,form')
  Object.assign(c.form,{id:'device-1',name:'烟感',productId:'product-1',deviceRole:'DIRECT',gatewayId:'old-gateway'})
  await c.save()
  assert.equal(saved.gatewayId,'')
  assert.equal(saved.deviceRole,'DIRECT')
})

test('product creation offers a published Go version before a legacy package exists', async()=>{
  const c=component('ProductsView.vue',async path=>path==='/api/v2/protocols' ? {items:[{definition:{id:'fire',name:'消防协议'},releases:[{version:'1',status:'PUBLISHED',transport:'TCP',payloadFormat:'hex'},{version:'2',status:'VALIDATED'}]}]} : {items:[],total:0},'load,protocols')
  await c.load()
  assert.ok(c.protocols.value.some(p=>p.id==='iot-standard@1.0.0'))
  assert.ok(c.protocols.value.some(p=>p.id==='fire@1'))
  assert.ok(!c.protocols.value.some(p=>p.id==='fire@2'))
})

test('instance editor clears previous instance data when creating a new connection',()=>{
  const c=component('AccessPointsPanel.vue',async()=>({}),'editProfile,createProfile,listener',undefined,new URL('../src/components/', import.meta.url))
  c.editProfile({id:'old',mode:'poll',network:'tcp',wireFormat:'rtu_over_tcp',deviceId:'old-device',collectorId:'old-collector',connectionMode:''})
  assert.equal(c.listener.mode,'poll')
  c.createProfile()
  assert.equal(c.listener.mode,'listener')
  assert.equal(c.listener.id,'')
  assert.equal(c.listener.deviceId,'')
  assert.equal(c.listener.wireFormat,'')
  assert.equal(c.listener.collectorId,undefined)
})

test('late product binding response cannot change a different instance being edited',async()=>{
  let finish
  const c=component('AccessPointsPanel.vue',()=>new Promise(resolve=>{finish=resolve}),'selectProduct,editProfile,listener',undefined,new URL('../src/components/', import.meta.url))
  const pending=c.selectProduct('old-product')
  c.editProfile({id:'current',mode:'listener',productId:'new-product',protocolId:'new-protocol',protocolVersion:'2'})
  finish({protocolId:'stale-protocol',version:'1'})
  await pending
  assert.equal(c.listener.protocolId,'new-protocol')
  assert.equal(c.listener.protocolVersion,'2')
})

async function devicesView(globals) {
 const source=await readFile(new URL('../src/views/DevicesView.vue',import.meta.url),'utf8')
 const script=source.split('<script setup>')[1].split('</script>')[0].replace(/^import .*$/gm,'')
 const context=vm.createContext({ref:value=>({value}),reactive:v=>v,computed:fn=>({get value(){return fn()}}),defineEmits:()=>()=>{},pretty:JSON.stringify,onMounted:()=>{},onBeforeUnmount:()=>{},window:{addEventListener(){}},sessionStorage:{getItem:()=>null,removeItem(){}},notifyError:e=>{throw e},setTimeout,clearTimeout,URLSearchParams,...globals})
 vm.runInContext(script+'\nglobalThis.state={load,loading,deviceTab,filters,registryPage,registryTotal,registry,unregistered,pendingCount,changeFilter,changeRegistryPage,updatesAvailable,realtime};',context)
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

test('设备心跳只更新最后活跃时间，运行状态变化或新设备才提示有新数据',async()=>{
 const context=await devicesView({api:async()=>({items:[]}),apiAll:async()=>({items:[]})})
 const s=context.state,state=(value,extra={})=>({detail:{topic:'/iot/device/state/tenant',payload:JSON.stringify({deviceId:'d1',businessStatus:'ONLINE',connectionStatus:'ONLINE',dataStatus:'NORMAL',lastSeenAt:value}),...extra}})
 s.registry.value=[{device:{id:'d1'},runtimeState:{deviceId:'d1',businessStatus:'ONLINE',lastSeenAt:1}}]
 s.realtime(state(2));s.realtime({detail:{topic:'/iot/parsed/tenant/p/d1/PROPERTY_REPORT',payload:'{}'}})
 assert.equal(s.updatesAvailable.value,false,'心跳和解析报文不应提示刷新')
 assert.equal(s.registry.value[0].runtimeState.lastSeenAt,2)
 s.realtime({detail:{topic:'/iot/device/state/tenant',payload:{deviceId:'other',businessStatus:'OFFLINE'}}})
 assert.equal(s.updatesAvailable.value,false,'不在当前页且不影响筛选的设备不提示')
 s.realtime({detail:{topic:'/iot/device/state/tenant',payload:{deviceId:'d1',businessStatus:'ALARM'}}})
 assert.equal(s.updatesAvailable.value,true)
 s.updatesAvailable.value=false;s.realtime(state(3,{added:true}))
 assert.equal(s.updatesAvailable.value,true,'新出现的设备需要提示')
 s.updatesAvailable.value=false;s.filters.runtime='OFFLINE'
 s.realtime({detail:{topic:'/iot/device/state/tenant',payload:{deviceId:'other',businessStatus:'OFFLINE'}}})
 assert.equal(s.updatesAvailable.value,true,'可能进入运行状态筛选结果的设备需要提示')
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

function fixture(api) {
  const source = fs.readFileSync(new URL('../src/views/RawView.vue', import.meta.url), 'utf8')
  const script = source.match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm, '')
  const warnings = []
  const context = vm.createContext({ ref, computed, defineEmits() {}, onMounted() {}, api, URLSearchParams, UiMessage: { warning: text => warnings.push(text) }, notifyError() {}, messageTypeLabel: x => x })
  return { ...vm.runInContext(script + '\n;({filters, appliedFilters, page, items, total, selection, load, search, resetFilters, recentHours, changePage, parseState, loadError})', context), warnings }
}

test('raw filters combine criteria, preserve applied filters on pagination and reset all fields', async () => {
  const requests = []
  const f = fixture(async url => { requests.push(new URL(url, 'http://test').searchParams); return { items: [], total: 0 } })
  Object.assign(f.filters.value, { deviceId: ' d ', messageId: 'raw-1', productId: 'p', protocol: 'json', payloadFormat: 'json', parseStatus: 'PARSED', messageType: 'ALARM_REPORT', parser: 'json_parser', range: [100, 200] })
  f.page.value = 3
  await f.search()
  const q = requests.at(-1)
  assert.equal(q.get('deviceId'), 'd')
  assert.equal(q.get('page'), '1')
  for (const key of ['messageId','productId','protocol','payloadFormat','parseStatus','messageType','parser']) assert.equal(q.get(key), f.filters.value[key])
  assert.equal(q.get('start'), '100'); assert.equal(q.get('end'), '200')
  f.filters.value.deviceId = 'not-applied'
  f.filters.value.range[0] = 50
  f.changePage(2)
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(requests.at(-1).get('deviceId'), 'd')
  assert.equal(requests.at(-1).get('start'), '100')
  assert.equal(requests.at(-1).get('page'), '2')
  await f.resetFilters()
  assert.deepEqual([...requests.at(-1).keys()].sort(), ['page','pageSize'])
  assert.equal(f.page.value, 1)
  f.filters.value.range = [300,100]
  const count = requests.length
  await f.search()
  assert.equal(requests.length, count)
  assert.equal(f.warnings.length, 1)
  await f.recentHours(24)
  assert.equal(Number(requests.at(-1).get('end')) - Number(requests.at(-1).get('start')), 86400000)
})

test('late raw responses cannot overwrite a newer filter result; failed queries clear stale rows', async () => {
  const pending = []
  const f = fixture(() => new Promise((resolve, reject) => pending.push({ resolve, reject })))
  f.filters.value.deviceId = 'old'; const old = f.search()
  f.filters.value.deviceId = 'new'; const current = f.search()
  pending[1].resolve({ items: [{ messageId:'new' }], total:1 }); await current
  pending[0].resolve({ items: [{ messageId:'old' }], total:99 }); await old
  assert.equal(f.items.value[0].messageId, 'new')
  assert.equal(f.total.value, 1)
  f.selection.value = [{messageId:'new'}]
  const failed = f.load()
  assert.equal(f.selection.value.length, 0)
  pending[2].reject(new Error('offline')); await failed
  assert.equal(f.items.value.length, 0)
  assert.equal(f.loadError.value, 'offline')
  assert.equal(f.parseState({parseError:'invalid'}).label, '解析失败')
  assert.equal(f.parseState({parsed:true, parseError:'old error', parsedMessageType:'ALARM_REPORT'}).tone, 'success')
})

test('alarm list batches alarm events and ignores device state events', async () => {
  const source = fs.readFileSync(new URL('../src/views/AlarmsView.vue', import.meta.url), 'utf8')
  const script = source.match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm, '')
  const timers = []
  let requests = 0
  const context = vm.createContext({
    ref, reactive, computed,
    defineEmits: () => () => {}, onMounted() {}, onBeforeUnmount() {},
    api: async () => { requests++; return { items: [], total: 0 } },
    alarmQuery: () => '', notifyError: error => { throw error },
    window: { setTimeout: callback => { timers.push(callback); return timers.length }, clearTimeout() {} }
  })
  const { realtime } = vm.runInContext(script + '\n;({realtime})', context)
  realtime({ detail: { topic: '/iot/device/state/tenant/product/device' } })
  assert.equal(timers.length, 0)
  realtime({ detail: { topic: '/iot/alarm/raised/tenant/product/device' } })
  realtime({ detail: { topic: '/iot/alarm/recovered/tenant/product/device' } })
  assert.equal(timers.length, 1)
  timers[0]()
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(requests, 1)
})

test('设备详情跳转保留设备范围，分页和状态筛选不会扩大查询范围', () => {
  const navigation = alarmNavigation('{"deviceId":"device-a"}')
  const query = alarmQuery({ ...navigation, status: 'CLOSED', level: 'LOW' }, 2, 20)
  assert.equal(query.get('deviceId'), 'device-a')
  assert.equal(query.get('status'), 'CLOSED')
  assert.equal(query.get('page'), '2')
  assert.equal(alarmQuery({}, 1, 20).has('deviceId'), false)
})

test('告警详情跳转与无效导航数据兼容', () => {
  assert.deepEqual(alarmNavigation('{"alarmId":"alarm-a"}'), { deviceId: '', alarmId: 'alarm-a' })
  for (const raw of [null, '{broken', 'null', '{"deviceId":{}}']) {
    assert.deepEqual(alarmNavigation(raw), { deviceId: '', alarmId: '' })
  }
})

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
    assert.ok(chart.points.every(p=>Number.isFinite(p.x)&&p.x>=chart.left&&p.x<=672&&p.y>=32&&p.y<=204))
  }
  const spike=trendGeometry([{date:'a',count:0},{date:'b',count:99999999}])
  assert.deepEqual(spike.ticks.map(t=>t.label),['0','2,500万','5,000万','7,500万','1亿'])
  assert.ok(spike.left>44)
  assert.deepEqual(trendGeometry([{date:'a',count:9999}]).ticks.map(t=>t.label),['0','2,500','5,000','7,500','1万'])
})
test('dashboard counts abbreviate large values so cards and ring centers stay inside their bounds',()=>{
  assert.deepEqual([0,9999,12345,99996,12345678,99995000,123456789,-3,'bad'].map(v=>compactCount(v)),['0','9,999','1.2万','10万','1,235万','1亿','1.2亿','0','0'])
})
function dashboardSetup(api) {
  const script=fs.readFileSync(new URL('../src/views/DashboardView.vue',import.meta.url),'utf8').match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm,'')
  let cleanup
  const context=vm.createContext({ref,computed,api,deviceSegments,ringSegments,productBars,dashboardDistributions,alarmLevels:{},AbortController,setTimeout,clearTimeout,notifyError(){},defineEmits:()=>()=>{},onMounted(){},onBeforeUnmount(fn){cleanup=fn},window:{removeEventListener(){}}})
  return {...vm.runInContext(script+'\n;({load,data,days,loading,loadError})',context),cleanup:()=>cleanup()}
}
test('range switching rejects late responses and retains last good snapshot on failure',async()=>{
  const requests=[]
  const c=dashboardSetup(path=>path.includes('/alarms?')?Promise.resolve({items:[]}):new Promise((resolve,reject)=>requests.push({resolve,reject})))
  const first=c.load();c.days.value=30;const second=c.load()
  requests[1].resolve({days:30});await second;requests[0].resolve({days:7});await first
  assert.equal(c.data.value.days,30)
  const failed=c.load();requests[2].reject(new Error('offline'));await failed
  assert.equal(c.data.value.days,30);assert.match(c.loadError.value,/刷新失败/);assert.equal(c.loading.value,false)
  const pending=c.load();c.cleanup();requests[3].resolve({days:7});await pending;assert.equal(c.data.value.days,30)
})


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
