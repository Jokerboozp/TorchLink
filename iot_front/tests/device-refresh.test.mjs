import {test} from 'node:test'
import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
import vm from 'node:vm'

test('实时消息不重载设备列表，手动刷新仍读取最新数据',async()=>{
 const source=await readFile(new URL('../src/views/DevicesView.vue',import.meta.url),'utf8')
 const script=source.split('<script setup>')[1].split('</script>')[0].replace(/^import .*$/gm,'')
 let mounted,requests=0;const events=new Map()
 const context=vm.createContext({ref:value=>({value}),reactive:v=>v,computed:fn=>({get value(){return fn()}}),defineEmits:()=>()=>{},pretty:JSON.stringify,onMounted:fn=>mounted=fn,onBeforeUnmount:()=>{},window:{addEventListener:(k,fn)=>events.set(k,fn)},api:async()=>{requests++;return{items:[]}},apiAll:async()=>{requests++;return{items:[]}},notifyError:e=>{throw e},setTimeout,clearTimeout})
 vm.runInContext(script+'\nglobalThis.refresh=load;globalThis.busy=loading;',context)
 mounted();await new Promise(r=>setTimeout(r,0));const initial=requests
 for(let i=0;i<10;i++)events.get('iot:realtime')({detail:{topic:'device.state',payload:{deviceId:'demo'}}})
 await new Promise(r=>setTimeout(r,0))
 assert.equal(requests,initial,'实时上报不应重新请求整张设备列表')
 assert.equal(context.busy.value,false)
 await context.refresh();assert.ok(requests>initial,'手动刷新必须仍然有效')
})

test('设备类型和主子设备先筛选再分页，切换筛选回到第一页',async()=>{
 const source=await readFile(new URL('../src/views/DevicesView.vue',import.meta.url),'utf8')
 const script=source.split('<script setup>')[1].split('</script>')[0].replace(/^import .*$/gm,'')
 const rows=Array.from({length:45},(_,i)=>({device:{id:`d${i}`,productId:i%2?'smoke':'sensor',deviceRole:i<25?'DIRECT':'CHILD',gatewayId:i<25?'':'gateway'}}))
 rows.push({device:{id:'gateway',productId:'gateway'}})
 const context=vm.createContext({ref:value=>({value}),reactive:v=>v,computed:fn=>({get value(){return fn()}}),defineEmits:()=>()=>{},pretty:JSON.stringify,onMounted:()=>{},onBeforeUnmount:()=>{},apiAll:async path=>({items:path.includes('/products')?[{id:'smoke',category:'smoke'},{id:'sensor',category:'sensor'},{id:'gateway',category:'gateway'}]:path.includes('/device-registry')?rows:[]}),notifyError:e=>{throw e}})
 vm.runInContext(script+'\nglobalThis.state={load,deviceTab,deviceCategory,registryPage,registryPageSize,registryTotal,registry,changeDeviceFilter,open,form};',context)
 const s=context.state;await s.load()
 assert.equal(s.registryTotal.value,25);assert.equal(s.registry.value.length,20)
 s.registryPage.value=2;assert.equal(s.registry.value.length,5)
 s.deviceTab.value='children';s.changeDeviceFilter();assert.equal(s.registryPage.value,1);assert.equal(s.registryTotal.value,20)
 assert.ok(s.registry.value.every(r=>r.device.deviceRole==='CHILD'))
 s.deviceCategory.value='smoke';s.changeDeviceFilter();assert.equal(s.registryTotal.value,10)
 assert.ok(s.registry.value.every(r=>r.device.productId==='smoke'))
 s.open();assert.equal(s.form.deviceRole,'CHILD')
 s.deviceCategory.value='camera';s.changeDeviceFilter();assert.equal(s.registryTotal.value,0)
 s.deviceCategory.value='';s.deviceTab.value='main';s.changeDeviceFilter();assert.equal(s.registryTotal.value,1)
 assert.equal(s.registry.value[0].device.id,'gateway');s.open();assert.equal(s.form.deviceRole,'GATEWAY')
 s.deviceTab.value='independent';s.changeDeviceFilter();assert.equal(s.registryTotal.value,25)
 s.open();assert.equal(s.form.deviceRole,'DIRECT')
})
