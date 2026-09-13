import fs from 'node:fs'
import vm from 'node:vm'
import assert from 'node:assert/strict'
import test from 'node:test'
import { createRequire } from 'node:module'
import { loadAllPages } from '../src/listPagination.js'
const require = createRequire(import.meta.url)
const { ref, reactive, computed, watch } = require('vue')
const root = new URL('../src/views/', import.meta.url)
// Execute the real setup code with Vue reactivity; replace external I/O and
// lifecycle hooks so response ordering is deterministic without a browser.
function component(file, api, exports, notifyError = e => { throw e }) {
  const source = fs.readFileSync(new URL(file, root), 'utf8').match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm, '')
  const context = vm.createContext({ref, reactive, computed, watch, defineProps:()=>({section:'profiles'}), api, apiAll:(path, options)=>loadAllPages(api,path,options), onMounted(){}, onBeforeUnmount(){}, defineEmits:()=>()=>{}, pretty:JSON.stringify, parseJSON:JSON.parse, crypto, notifyError, ElMessage:{success(){},warning(){},info(){}}, sessionStorage:{getItem(){return null}}, URLSearchParams})
  return vm.runInContext(source + '\n;({' + exports + '})', context)
}
const items = Array.from({length:101}, (_, i)=>({id:`item-${i+1}`,name:`Item ${i+1}`}))
function paginated(path) {
  const url = new URL(path, 'http://audit.invalid')
  const size = Math.min(100, Number(url.searchParams.get('pageSize') || 20))
  const start = (Number(url.searchParams.get('page') || 1)-1)*size
  return {items:items.slice(start,start+size),total:items.length}
}
test('camera association offers device 101', async()=>{
  const c=component('CameraMappingsView.vue', async path=>path.includes('device-registry') ? paginated(path) : {items:[],total:0}, 'load,devices')
  await c.load()
  assert.ok(c.devices.value.some(x=>x.id==='item-101'), `only ${c.devices.value.length}/101 device options loaded`)
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
test('product editor offers protocol 101', async()=>{
  const c=component('ProductsView.vue', async path=>path.includes('/protocol-packages') ? paginated(path) : {items:[],total:0}, 'load,protocols')
  await c.load()
  assert.ok(c.protocols.value.some(x=>x.id==='item-101'))
})
test('child device can choose a gateway outside current registry page', async()=>{
  const rows=Array.from({length:21},(_,i)=>({device:{id:`device-${i+1}`,deviceRole:i===20?'GATEWAY':'DIRECT'}}))
  const c=component('DevicesView.vue', async path=>{
    if (!path.includes('device-registry')) return {items:[],total:0}
    const query=new URL(path,'http://audit.invalid').searchParams
    const size=Number(query.get('pageSize') || 20)
    const start=(Number(query.get('page') || 1)-1)*size
    return {items:rows.slice(start,start+size),total:rows.length}
  }, 'load,gateways,registryPage')
  await c.load()
  const firstPageGateways=c.gateways.value.length
  c.registryPage.value=2
  await c.load()
  assert.equal(c.gateways.value.length,1,'control: gateway is selectable on page 2')
  assert.equal(firstPageGateways,1,'gateway on page 2 is absent from registration dialog on page 1')
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
test('controls: camera list handles 100 available devices and backend fixture exposes page 2', async()=>{
  const c=component('CameraMappingsView.vue', async path=>path.includes('device-registry') ? {items:items.slice(0,100),total:100} : {items:[],total:0}, 'load,devices')
  await c.load()
  assert.equal(c.devices.value.length,100)
  assert.equal(paginated('/api/v1/device-registry?page=2&pageSize=100').items[0].id,'item-101')
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
      if (url.pathname.endsWith(endpoint) && url.searchParams.get('pageSize')==='20') {
        return new Promise(resolve=>pending.push(resolve))
      }
      return Promise.resolve({items:[],total:0})
    }, `load,loading,${pageKey},${rowsKey},${totalKey}`)
    const first=c.load()
    c[pageKey].value=2
    const second=c.load()
    pending[1]({items:[{id:'new'}],total:40})
    await second
    pending[0]({items:[{id:'old'}],total:20})
    await first
    assert.equal(c[rowsKey].value[0].id,'new')
    assert.equal(c[totalKey].value,40)
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
    if (options?.method === 'POST') { writes++; return new Promise((_,reject)=>{rejectSave=reject}) }
    return {items:[],total:0}
  }, 'save,form,saving,dialog', e=>errors.push(e))
  Object.assign(c.form,{name:'烟感',code:'device-fixed',productId:'product-1'})
  c.dialog.value=true
  const first=c.save()
  await c.save()
  assert.equal(writes,1)
  assert.equal(c.saving.value,true)
  rejectSave(new Error('offline')); await first
  assert.equal(c.saving.value,false)
  assert.equal(c.dialog.value,true)
  assert.equal(c.form.code,'device-fixed')
  assert.equal(errors.length,1)
})

test('device registration clears stale gateway links when the role changes', async()=>{
  let saved
  const c=component('DevicesView.vue', async(path,options)=>{
    if (options?.method === 'POST') { saved=JSON.parse(options.body); return {} }
    return {items:[],total:0}
  }, 'save,form')
  Object.assign(c.form,{name:'烟感',code:'new-device',productId:'product-1',deviceRole:'DIRECT',gatewayId:'old-gateway'})
  await c.save()
  assert.equal(saved.gatewayId,'')
  assert.equal(saved.deviceRole,'DIRECT')
})

test('new device form cannot overwrite an existing identifier', async()=>{
  let writes=0
  const c=component('DevicesView.vue',async()=>{writes++;return {}},'save,form,registryOptions')
  Object.assign(c.form,{name:'新名称',code:'existing',productId:'product-1'})
  c.registryOptions.value=[{device:{id:'existing'}}]
  await c.save()
  assert.equal(writes,0)
})

test('product creation offers a published Go version before a legacy package exists', async()=>{
  const c=component('ProductsView.vue',async path=>path==='/api/v2/protocols' ? {items:[{definition:{id:'fire',name:'消防协议'},releases:[{version:'1',status:'PUBLISHED',transport:'TCP',payloadFormat:'hex'},{version:'2',status:'VALIDATED'}]}]} : {items:[],total:0},'load,protocols')
  await c.load()
  assert.ok(c.protocols.value.some(p=>p.id==='iot-standard@1.0.0'))
  assert.ok(c.protocols.value.some(p=>p.id==='fire@1'))
  assert.ok(!c.protocols.value.some(p=>p.id==='fire@2'))
})

test('instance editor clears previous instance data when creating a new connection',()=>{
  const c=component('ProtocolsView.vue',async()=>({}),'editProfile,createProfile,listener')
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
  const c=component('ProtocolsView.vue',()=>new Promise(resolve=>{finish=resolve}),'selectProduct,editProfile,listener')
  const pending=c.selectProduct('old-product')
  c.editProfile({id:'current',mode:'listener',productId:'new-product',protocolId:'new-protocol',protocolVersion:'2'})
  finish({protocolId:'stale-protocol',version:'1'})
  await pending
  assert.equal(c.listener.protocolId,'new-protocol')
  assert.equal(c.listener.protocolVersion,'2')
})
