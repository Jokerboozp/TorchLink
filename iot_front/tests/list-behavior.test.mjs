import fs from 'node:fs'
import vm from 'node:vm'
import assert from 'node:assert/strict'
import test from 'node:test'
import { createRequire } from 'node:module'
import { loadAllPages } from '../src/listPagination.js'
const require = createRequire(import.meta.url)
const { ref, reactive, computed } = require('vue')
const root = new URL('../src/views/', import.meta.url)
// Execute the real setup code with Vue reactivity; replace external I/O and
// lifecycle hooks so response ordering is deterministic without a browser.
function component(file, api, exports, notifyError = e => { throw e }) {
  const source = fs.readFileSync(new URL(file, root), 'utf8').match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm, '')
  const context = vm.createContext({ref, reactive, computed, api, apiAll:(path, options)=>loadAllPages(api,path,options), onMounted(){}, onBeforeUnmount(){}, defineEmits:()=>()=>{}, pretty:JSON.stringify, notifyError, ElMessage:{success(){},warning(){},info(){}}, sessionStorage:{getItem(){return null}}, URLSearchParams})
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
