import fs from 'node:fs' /* 引入当前代码需要的依赖。 */
import vm from 'node:vm' /* 引入当前代码需要的依赖。 */
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */
import test from 'node:test' /* 引入当前代码需要的依赖。 */
import { createRequire } from 'node:module' /* 引入当前代码需要的依赖。 */
import { loadAllPages } from '../src/listPagination.js' /* 引入当前代码需要的依赖。 */
import { createClientId } from '../src/clientId.js' /* 引入当前代码需要的依赖。 */
const require = createRequire(import.meta.url) /* 声明 require。 */
const { ref, reactive, computed, watch } = require('vue') /* 执行当前语句并推进处理流程。 */
const root = new URL('../src/views/', import.meta.url) /* 声明 root。 */
// Execute the real setup code with Vue reactivity; replace external I/O and
// lifecycle hooks so response ordering is deterministic without a browser.
function component(file, api, exports, notifyError = e => { throw e }) { /* 定义 component 函数。 */
  const source = fs.readFileSync(new URL(file, root), 'utf8').match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm, '') /* 声明 source。 */
  const context = vm.createContext({ref, reactive, computed, watch, defineProps:()=>({section:'profiles'}), api, apiAll:(path, options)=>loadAllPages(api,path,options), onMounted(){}, onBeforeUnmount(){}, defineEmits:()=>()=>{}, pretty:JSON.stringify, parseJSON:JSON.parse, crypto:{getRandomValues:bytes=>crypto.getRandomValues(bytes)}, createClientId:()=>createClientId({getRandomValues:bytes=>crypto.getRandomValues(bytes)}), notifyError, UiMessage:{success(){},warning(){},info(){}}, sessionStorage:{getItem(){return null}}, URLSearchParams}) /* 为 Naive UI 消息入口提供无副作用替身。 */
  return vm.runInContext(source + '\n;({' + exports + '})', context) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
const items = Array.from({length:101}, (_, i)=>({id:`item-${i+1}`,name:`Item ${i+1}`})) /* 声明 items。 */

test('HTTP 页面新增产品和设备时可以生成默认编号并提交',async()=>{ /* 执行当前语句并推进处理流程。 */
 for(const [file,prefix,path] of [['ProductsView.vue','product','/api/v1/products'],['DevicesView.vue','device','/api/v1/device-registry']]) { /* 循环处理当前数据。 */
  const requests=[] /* 声明 requests。 */
  const c=component(file,async(url,options)=>{if(options?.method==='POST')requests.push({url,body:JSON.parse(options.body)});return {items:[],total:0}},'form,save') /* 声明 c。 */
  Object.assign(c.form,{name:'HTTP 演示',protocolPackageId:'protocol',productId:'product'}) /* 执行当前语句并推进处理流程。 */
  await c.save() /* 等待异步操作完成。 */
  assert.equal(requests.length,1) /* 验证实际结果符合预期。 */
  assert.equal(requests[0].url,path) /* 验证实际结果符合预期。 */
  assert.match(requests[0].body.id,new RegExp(`^${prefix}_[0-9a-f]{12}$`)) /* 验证实际结果符合预期。 */
 } /* 结束当前表达式或代码块。 */
}) /* 结束当前表达式或代码块。 */
test('协议列表分页覆盖所有记录并在列表缩小时修正当前页',async()=>{ /* 执行当前语句并推进处理流程。 */
 const c=component('ProtocolsView.vue',async()=>({items:[]}),'protocols,protocolPage,protocolPageSize,pagedProtocols') /* 声明 c。 */
 c.protocols.value=Array.from({length:45},(_,i)=>({definition:{id:`protocol-${i+1}`},releases:[]})) /* 更新 c.protocols.value 的值。 */
 assert.equal(c.pagedProtocols.value.length,20) /* 验证实际结果符合预期。 */
 c.protocolPage.value=2;assert.equal(c.pagedProtocols.value[0].definition.id,'protocol-21') /* 验证实际结果符合预期。 */
 c.protocolPage.value=3;assert.equal(c.pagedProtocols.value.length,5) /* 验证实际结果符合预期。 */
 c.protocols.value=c.protocols.value.slice(0,3);await Promise.resolve();assert.equal(c.protocolPage.value,1) /* 验证实际结果符合预期。 */
 assert.equal(c.pagedProtocols.value.length,3) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
function paginated(path) { /* 定义 paginated 函数。 */
  const url = new URL(path, 'http://audit.invalid') /* 声明 url。 */
  const size = Math.min(100, Number(url.searchParams.get('pageSize') || 20)) /* 声明 size。 */
  const start = (Number(url.searchParams.get('page') || 1)-1)*size /* 声明 start。 */
  return {items:items.slice(start,start+size),total:items.length} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
test('camera association offers device 101', async()=>{ /* 执行当前语句并推进处理流程。 */
  const c=component('CameraMappingsView.vue', async path=>path.includes('device-registry') ? paginated(path) : {items:[],total:0}, 'load,devices') /* 声明 c。 */
  await c.load() /* 等待异步操作完成。 */
  assert.ok(c.devices.value.some(x=>x.id==='item-101'), `only ${c.devices.value.length}/101 device options loaded`) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
test('rule editor offers product 101', async()=>{ /* 执行当前语句并推进处理流程。 */
  const c=component('RulesView.vue', async path=>path.includes('/products') ? paginated(path) : {items:[],total:0}, 'load,products') /* 声明 c。 */
  await c.load() /* 等待异步操作完成。 */
  assert.ok(c.products.value.some(x=>x.id==='item-101'), `only ${c.products.value.length}/101 product options loaded`) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
test('device registration offers product 101', async()=>{ /* 执行当前语句并推进处理流程。 */
  const c=component('DevicesView.vue', async path=>path.includes('/products') ? paginated(path) : {items:[],total:0}, 'load,products') /* 声明 c。 */
  await c.load() /* 等待异步操作完成。 */
  assert.ok(c.products.value.some(x=>x.id==='item-101')) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
test('product editor offers protocol 101', async()=>{ /* 执行当前语句并推进处理流程。 */
  const c=component('ProductsView.vue', async path=>path.includes('/protocol-packages') ? paginated(path) : {items:[],total:0}, 'load,protocols') /* 声明 c。 */
  await c.load() /* 等待异步操作完成。 */
  assert.ok(c.protocols.value.some(x=>x.id==='item-101')) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('editing a product preserves its existing thing model', async()=>{ /* 执行当前语句并推进处理流程。 */
  let saved /* 声明 saved。 */
  const model={properties:[{identifier:'temperature',dataType:'number'}],commands:[{identifier:'reset'}]} /* 声明 model。 */
  const c=component('ProductsView.vue', async (_path,options)=>{ /* 声明 c。 */
    if(options?.method==='PUT') saved=JSON.parse(options.body) /* 判断条件并选择处理分支。 */
    return {items:[],total:0} /* 返回当前处理结果。 */
  }, 'edit,form,save') /* 结束当前表达式或代码块。 */
  c.edit({id:'product-1',name:'传感器',protocolPackageId:'iot-standard@1.0.0',thingModel:model}) /* 执行当前语句并推进处理流程。 */
  c.form.description='更新说明' /* 更新 c.form.description 的值。 */
  await c.save() /* 等待异步操作完成。 */
  assert.deepEqual(saved.thingModel,model) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('product pagination reuses the loaded protocol catalog', async()=>{ /* 执行当前语句并推进处理流程。 */
  const requests=[] /* 声明 requests。 */
  const c=component('ProductsView.vue', async path=>{ /* 声明 c。 */
    requests.push(path) /* 执行当前语句并推进处理流程。 */
    return {items:[],total:60} /* 返回当前处理结果。 */
  }, 'load,changePage,changePageSize') /* 结束当前表达式或代码块。 */
  await c.load() /* 等待异步操作完成。 */
  const catalogRequests=()=>requests.filter(path=>path.includes('/protocol-packages') || path==='/api/v2/protocols').length /* 声明 catalogRequests。 */
  assert.equal(catalogRequests(),2) /* 验证实际结果符合预期。 */
  c.changePage(2) /* 执行当前语句并推进处理流程。 */
  await new Promise(resolve=>setImmediate(resolve)) /* 等待异步操作完成。 */
  c.changePageSize(50) /* 执行当前语句并推进处理流程。 */
  await new Promise(resolve=>setImmediate(resolve)) /* 等待异步操作完成。 */
  assert.equal(catalogRequests(),2) /* 验证实际结果符合预期。 */
  assert.ok(requests.some(path=>path.includes('page=2'))) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
test('product pagination does not discard an in-flight protocol catalog', async()=>{ /* 执行当前语句并推进处理流程。 */
  let finishCatalog /* 声明 finishCatalog。 */
  const c=component('ProductsView.vue', async path=>{ /* 声明 c。 */
    if(path.includes('/protocol-packages')) return new Promise(resolve=>{finishCatalog=resolve}) /* 判断条件并选择处理分支。 */
    return {items:[],total:60} /* 返回当前处理结果。 */
  }, 'load,changePage,protocols') /* 结束当前表达式或代码块。 */
  const first=c.load() /* 声明 first。 */
  await new Promise(resolve=>setImmediate(resolve)) /* 等待异步操作完成。 */
  c.changePage(2) /* 执行当前语句并推进处理流程。 */
  finishCatalog({items:[{id:'late-protocol',name:'最新协议'}],total:1}) /* 执行当前语句并推进处理流程。 */
  await first /* 等待异步操作完成。 */
  assert.ok(c.protocols.value.some(item=>item.id==='late-protocol')) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
test('child device can choose a gateway outside current registry page', async()=>{ /* 执行当前语句并推进处理流程。 */
  const rows=Array.from({length:21},(_,i)=>({device:{id:`device-${i+1}`,deviceRole:i===20?'GATEWAY':'DIRECT'}})) /* 声明 rows。 */
  const c=component('DevicesView.vue', async path=>{ /* 声明 c。 */
    if (!path.includes('device-registry')) return {items:[],total:0} /* 判断条件并选择处理分支。 */
    const query=new URL(path,'http://audit.invalid').searchParams /* 声明 query。 */
    const size=Number(query.get('pageSize') || 20) /* 声明 size。 */
    const start=(Number(query.get('page') || 1)-1)*size /* 声明 start。 */
    return {items:rows.slice(start,start+size),total:rows.length} /* 返回当前处理结果。 */
  }, 'load,gateways,registryPage') /* 结束当前表达式或代码块。 */
  await c.load() /* 等待异步操作完成。 */
  const firstPageGateways=c.gateways.value.length /* 声明 firstPageGateways。 */
  c.registryPage.value=2 /* 更新 c.registryPage.value 的值。 */
  await c.load() /* 等待异步操作完成。 */
  assert.equal(c.gateways.value.length,1,'control: gateway is selectable on page 2') /* 验证实际结果符合预期。 */
  assert.equal(firstPageGateways,1,'gateway on page 2 is absent from registration dialog on page 1') /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
test('camera pagination retains latest requested page when responses arrive out of order', async()=>{ /* 执行当前语句并推进处理流程。 */
  const pending=[] /* 声明 pending。 */
  const c=component('CameraMappingsView.vue', path=>path.includes('device-registry') ? Promise.resolve({items:[]}) : new Promise(resolve=>pending.push(resolve)), 'load,page,cameras') /* 声明 c。 */
  const first=c.load() /* 声明 first。 */
  c.page.value=2 /* 更新 c.page.value 的值。 */
  const second=c.load() /* 声明 second。 */
  pending[1]({items:[{cameraId:'page-2'}],total:40}) /* 执行当前语句并推进处理流程。 */
  await second /* 等待异步操作完成。 */
  pending[0]({items:[{cameraId:'page-1'}],total:40}) /* 执行当前语句并推进处理流程。 */
  await first /* 等待异步操作完成。 */
  assert.equal(c.cameras.value[0].cameraId,'page-2',`pager=${c.page.value}, displayed=${c.cameras.value[0].cameraId}`) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
test('stale failure does not notify or stop the current camera loading state', async()=>{ /* 执行当前语句并推进处理流程。 */
  const pending=[] /* 声明 pending。 */
  const errors=[] /* 声明 errors。 */
  const c=component('CameraMappingsView.vue', path=>path.includes('device-registry') ? Promise.resolve({items:[]}) : new Promise((resolve,reject)=>pending.push({resolve,reject})), 'load,loading,cameras', e=>errors.push(e)) /* 声明 c。 */
  const first=c.load() /* 声明 first。 */
  const second=c.load() /* 声明 second。 */
  pending[0].reject(new Error('old request failed')) /* 执行当前语句并推进处理流程。 */
  await first /* 等待异步操作完成。 */
  assert.equal(errors.length,0) /* 验证实际结果符合预期。 */
  assert.equal(c.loading.value,true) /* 验证实际结果符合预期。 */
  pending[1].resolve({items:[{cameraId:'current'}],total:1}) /* 执行当前语句并推进处理流程。 */
  await second /* 等待异步操作完成。 */
  assert.equal(c.loading.value,false) /* 验证实际结果符合预期。 */
  const current=c.load() /* 声明 current。 */
  pending[2].reject(new Error('current request failed')) /* 执行当前语句并推进处理流程。 */
  await current /* 等待异步操作完成。 */
  assert.equal(errors.length,1) /* 验证实际结果符合预期。 */
  assert.equal(c.loading.value,false) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
test('controls: camera list handles 100 available devices and backend fixture exposes page 2', async()=>{ /* 执行当前语句并推进处理流程。 */
  const c=component('CameraMappingsView.vue', async path=>path.includes('device-registry') ? {items:items.slice(0,100),total:100} : {items:[],total:0}, 'load,devices') /* 声明 c。 */
  await c.load() /* 等待异步操作完成。 */
  assert.equal(c.devices.value.length,100) /* 验证实际结果符合预期。 */
  assert.equal(paginated('/api/v1/device-registry?page=2&pageSize=100').items[0].id,'item-101') /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
test('control: camera page remains correct when responses arrive in order', async()=>{ /* 执行当前语句并推进处理流程。 */
  const pending=[] /* 声明 pending。 */
  const c=component('CameraMappingsView.vue', path=>path.includes('device-registry') ? Promise.resolve({items:[]}) : new Promise(resolve=>pending.push(resolve)), 'load,page,cameras') /* 声明 c。 */
  const first=c.load() /* 声明 first。 */
  c.page.value=2 /* 更新 c.page.value 的值。 */
  const second=c.load() /* 声明 second。 */
  pending[0]({items:[{cameraId:'page-1'}],total:40}) /* 执行当前语句并推进处理流程。 */
  await first /* 等待异步操作完成。 */
  pending[1]({items:[{cameraId:'page-2'}],total:40}) /* 执行当前语句并推进处理流程。 */
  await second /* 等待异步操作完成。 */
  assert.equal(c.cameras.value[0].cameraId,'page-2') /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

for (const [file, endpoint, pageKey, rowsKey, totalKey] of [ /* 循环处理当前数据。 */
  ['DevicesView.vue', '/device-registry', 'registryPage', 'registry', 'registryTotal'], /* 执行当前语句并推进处理流程。 */
  ['ProductsView.vue', '/products', 'productPage', 'products', 'productTotal'], /* 执行当前语句并推进处理流程。 */
  ['RulesView.vue', '/rules', 'page', 'rules', 'total'] /* 执行当前语句并推进处理流程。 */
]) { /* 结束当前表达式或代码块。 */
  test(`${file} ignores old page data and totals`, async()=>{ /* 执行当前语句并推进处理流程。 */
    const pending=[] /* 声明 pending。 */
    const c=component(file, path=>{ /* 声明 c。 */
      const url=new URL(path,'http://audit.invalid') /* 声明 url。 */
      if (url.pathname.endsWith(endpoint) && (file === 'DevicesView.vue' || url.searchParams.get('pageSize')==='20')) { /* 判断条件并选择处理分支。 */
        return new Promise(resolve=>pending.push(resolve)) /* 返回当前处理结果。 */
      } /* 结束当前表达式或代码块。 */
      return Promise.resolve({items:[],total:0}) /* 返回当前处理结果。 */
    }, `load,loading,${pageKey},${rowsKey},${totalKey}`) /* 结束当前表达式或代码块。 */
    const first=c.load() /* 声明 first。 */
    c[pageKey].value=2 /* 更新 c[pageKey].value 的值。 */
    const second=c.load() /* 声明 second。 */
    const deviceList = file === 'DevicesView.vue' /* 声明 deviceList。 */
    pending[1]({items:[deviceList ? {id:'new',device:{id:'new',deviceRole:'DIRECT'}} : {id:'new'}],total:deviceList ? 1 : 40}) /* 执行当前语句并推进处理流程。 */
    await second /* 等待异步操作完成。 */
    pending[0]({items:[deviceList ? {id:'old',device:{id:'old',deviceRole:'CHILD'}} : {id:'old'}],total:deviceList ? 1 : 20}) /* 执行当前语句并推进处理流程。 */
    await first /* 等待异步操作完成。 */
    assert.equal(c[rowsKey].value[0].id,'new') /* 验证实际结果符合预期。 */
    assert.equal(c[totalKey].value,deviceList ? 1 : 40) /* 验证实际结果符合预期。 */
    assert.equal(c.loading.value,false) /* 验证实际结果符合预期。 */
  }) /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

test('association pagination preserves filters and stops when the dataset shrinks', async()=>{ /* 执行当前语句并推进处理流程。 */
  const calls=[] /* 声明 calls。 */
  const result=await loadAllPages(async path=>{ /* 声明 result。 */
    const query=new URL(path,'http://audit.invalid').searchParams /* 声明 query。 */
    calls.push(query) /* 执行当前语句并推进处理流程。 */
    return calls.length===1 ? {items:items.slice(0,100),total:101,mode:'test'} : {items:[],total:100} /* 返回当前处理结果。 */
  }, '/catalog?status=ENABLED&offset=50&limit=10') /* 结束当前表达式或代码块。 */
  assert.equal(calls.length,2) /* 验证实际结果符合预期。 */
  assert.equal(calls[1].get('status'),'ENABLED') /* 验证实际结果符合预期。 */
  assert.equal(calls[1].get('page'),'2') /* 验证实际结果符合预期。 */
  assert.equal(calls[0].has('offset'),false) /* 验证实际结果符合预期。 */
  assert.equal(result.items.length,100) /* 验证实际结果符合预期。 */
  assert.equal(result.mode,'test') /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('association pagination supports count-only and uncounted responses', async()=>{ /* 执行当前语句并推进处理流程。 */
  for (const counted of [true,false]) { /* 循环处理当前数据。 */
    const result=await loadAllPages(async path=>{ /* 声明 result。 */
      const data=paginated(path) /* 声明 data。 */
      return {items:data.items,...(counted ? {count:data.total} : {})} /* 返回当前处理结果。 */
    }, '/catalog') /* 结束当前表达式或代码块。 */
    assert.equal(result.items.length,101) /* 验证实际结果符合预期。 */
  } /* 结束当前表达式或代码块。 */
}) /* 结束当前表达式或代码块。 */

test('association pagination propagates a later-page failure instead of returning partial options', async()=>{ /* 执行当前语句并推进处理流程。 */
  await assert.rejects(loadAllPages(async path=>{ /* 验证实际结果符合预期。 */
    if (new URL(path,'http://audit.invalid').searchParams.get('page')==='2') throw new Error('catalog unavailable') /* 判断条件并选择处理分支。 */
    return paginated(path) /* 返回当前处理结果。 */
  }, '/catalog'), /catalog unavailable/) /* 结束当前表达式或代码块。 */
}) /* 结束当前表达式或代码块。 */

test('device save suppresses duplicate submission and preserves fields after failure', async()=>{ /* 执行当前语句并推进处理流程。 */
  let rejectSave, writes=0 /* 声明 rejectSave。 */
  const errors=[] /* 声明 errors。 */
  const c=component('DevicesView.vue', async (path,options)=>{ /* 声明 c。 */
    if (options?.method === 'POST') { writes++; return new Promise((_,reject)=>{rejectSave=reject}) } /* 判断条件并选择处理分支。 */
    return {items:[],total:0} /* 返回当前处理结果。 */
  }, 'save,form,saving,dialog', e=>errors.push(e)) /* 结束当前表达式或代码块。 */
  Object.assign(c.form,{name:'烟感',code:'device-fixed',productId:'product-1'}) /* 执行当前语句并推进处理流程。 */
  c.dialog.value=true /* 更新 c.dialog.value 的值。 */
  const first=c.save() /* 声明 first。 */
  await c.save() /* 等待异步操作完成。 */
  assert.equal(writes,1) /* 验证实际结果符合预期。 */
  assert.equal(c.saving.value,true) /* 验证实际结果符合预期。 */
  rejectSave(new Error('offline')); await first /* 执行当前语句并推进处理流程。 */
  assert.equal(c.saving.value,false) /* 验证实际结果符合预期。 */
  assert.equal(c.dialog.value,true) /* 验证实际结果符合预期。 */
  assert.equal(c.form.code,'device-fixed') /* 验证实际结果符合预期。 */
  assert.equal(errors.length,1) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('device registration clears stale gateway links when the role changes', async()=>{ /* 执行当前语句并推进处理流程。 */
  let saved /* 声明 saved。 */
  const c=component('DevicesView.vue', async(path,options)=>{ /* 声明 c。 */
    if (options?.method === 'POST') { saved=JSON.parse(options.body); return {} } /* 判断条件并选择处理分支。 */
    return {items:[],total:0} /* 返回当前处理结果。 */
  }, 'save,form') /* 结束当前表达式或代码块。 */
  Object.assign(c.form,{name:'烟感',code:'new-device',productId:'product-1',deviceRole:'DIRECT',gatewayId:'old-gateway'}) /* 执行当前语句并推进处理流程。 */
  await c.save() /* 等待异步操作完成。 */
  assert.equal(saved.gatewayId,'') /* 验证实际结果符合预期。 */
  assert.equal(saved.deviceRole,'DIRECT') /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('new device form cannot overwrite an existing identifier', async()=>{ /* 执行当前语句并推进处理流程。 */
  let writes=0 /* 声明 writes。 */
  const c=component('DevicesView.vue',async()=>{writes++;return {}},'save,form,registryOptions') /* 声明 c。 */
  Object.assign(c.form,{name:'新名称',code:'existing',productId:'product-1'}) /* 执行当前语句并推进处理流程。 */
  c.registryOptions.value=[{device:{id:'existing'}}] /* 更新 c.registryOptions.value 的值。 */
  await c.save() /* 等待异步操作完成。 */
  assert.equal(writes,0) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('product creation offers a published Go version before a legacy package exists', async()=>{ /* 执行当前语句并推进处理流程。 */
  const c=component('ProductsView.vue',async path=>path==='/api/v2/protocols' ? {items:[{definition:{id:'fire',name:'消防协议'},releases:[{version:'1',status:'PUBLISHED',transport:'TCP',payloadFormat:'hex'},{version:'2',status:'VALIDATED'}]}]} : {items:[],total:0},'load,protocols') /* 声明 c。 */
  await c.load() /* 等待异步操作完成。 */
  assert.ok(c.protocols.value.some(p=>p.id==='iot-standard@1.0.0')) /* 验证实际结果符合预期。 */
  assert.ok(c.protocols.value.some(p=>p.id==='fire@1')) /* 验证实际结果符合预期。 */
  assert.ok(!c.protocols.value.some(p=>p.id==='fire@2')) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('instance editor clears previous instance data when creating a new connection',()=>{ /* 执行当前语句并推进处理流程。 */
  const c=component('ProtocolsView.vue',async()=>({}),'editProfile,createProfile,listener') /* 声明 c。 */
  c.editProfile({id:'old',mode:'poll',network:'tcp',wireFormat:'rtu_over_tcp',deviceId:'old-device',collectorId:'old-collector',connectionMode:''}) /* 执行当前语句并推进处理流程。 */
  assert.equal(c.listener.mode,'poll') /* 验证实际结果符合预期。 */
  c.createProfile() /* 执行当前语句并推进处理流程。 */
  assert.equal(c.listener.mode,'listener') /* 验证实际结果符合预期。 */
  assert.equal(c.listener.id,'') /* 验证实际结果符合预期。 */
  assert.equal(c.listener.deviceId,'') /* 验证实际结果符合预期。 */
  assert.equal(c.listener.wireFormat,'') /* 验证实际结果符合预期。 */
  assert.equal(c.listener.collectorId,undefined) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('late product binding response cannot change a different instance being edited',async()=>{ /* 执行当前语句并推进处理流程。 */
  let finish /* 声明 finish。 */
  const c=component('ProtocolsView.vue',()=>new Promise(resolve=>{finish=resolve}),'selectProduct,editProfile,listener') /* 声明 c。 */
  const pending=c.selectProduct('old-product') /* 声明 pending。 */
  c.editProfile({id:'current',mode:'listener',productId:'new-product',protocolId:'new-protocol',protocolVersion:'2'}) /* 执行当前语句并推进处理流程。 */
  finish({protocolId:'stale-protocol',version:'1'}) /* 执行当前语句并推进处理流程。 */
  await pending /* 等待异步操作完成。 */
  assert.equal(c.listener.protocolId,'new-protocol') /* 验证实际结果符合预期。 */
  assert.equal(c.listener.protocolVersion,'2') /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
