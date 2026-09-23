import {test} from 'node:test' /* 引入当前代码需要的依赖。 */
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */
import {readFile} from 'node:fs/promises' /* 引入当前代码需要的依赖。 */
import vm from 'node:vm' /* 引入当前代码需要的依赖。 */

test('实时消息不重载设备列表，手动刷新仍读取最新数据',async()=>{ /* 执行当前语句并推进处理流程。 */
 const source=await readFile(new URL('../src/views/DevicesView.vue',import.meta.url),'utf8') /* 声明 source。 */
 const script=source.split('<script setup>')[1].split('</script>')[0].replace(/^import .*$/gm,'') /* 声明 script。 */
 let mounted,requests=0;const events=new Map() /* 声明 mounted。 */
 const context=vm.createContext({ref:value=>({value}),reactive:v=>v,computed:fn=>({get value(){return fn()}}),defineEmits:()=>()=>{},pretty:JSON.stringify,onMounted:fn=>mounted=fn,onBeforeUnmount:()=>{},window:{addEventListener:(k,fn)=>events.set(k,fn)},session:{tenant:'t',user:'u'},sessionStorage:{getItem:()=>null,removeItem(){}},localStorage:{getItem:()=>null},api:async()=>{requests++;return{items:[]}},apiAll:async()=>{requests++;return{items:[]}},notifyError:e=>{throw e},setTimeout,clearTimeout}) /* 声明 context。 */
 vm.runInContext(script+'\nglobalThis.refresh=load;globalThis.busy=loading;',context) /* 执行当前语句并推进处理流程。 */
 mounted();await new Promise(r=>setTimeout(r,0));const initial=requests /* 执行当前语句并推进处理流程。 */
 for(let i=0;i<10;i++)events.get('iot:realtime')({detail:{topic:'device.state',payload:{deviceId:'demo'}}}) /* 循环处理当前数据。 */
 await new Promise(r=>setTimeout(r,0)) /* 等待异步操作完成。 */
 assert.equal(requests,initial,'实时上报不应重新请求整张设备列表') /* 验证实际结果符合预期。 */
 assert.equal(context.busy.value,false) /* 验证实际结果符合预期。 */
 await context.refresh();assert.ok(requests>initial,'手动刷新必须仍然有效') /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('设备类型和主子设备先筛选再分页，切换筛选回到第一页',async()=>{ /* 执行当前语句并推进处理流程。 */
 const source=await readFile(new URL('../src/views/DevicesView.vue',import.meta.url),'utf8') /* 声明 source。 */
 const script=source.split('<script setup>')[1].split('</script>')[0].replace(/^import .*$/gm,'') /* 声明 script。 */
 const rows=Array.from({length:45},(_,i)=>({device:{id:`d${i}`,productId:i%2?'smoke':'sensor',deviceRole:i<25?'DIRECT':'CHILD',gatewayId:i<25?'':'gateway'}})) /* 声明 rows。 */
 rows.push({device:{id:'gateway',productId:'gateway'}}) /* 执行当前语句并推进处理流程。 */
 const context=vm.createContext({ref:value=>({value}),reactive:v=>v,computed:fn=>({get value(){return fn()}}),defineEmits:()=>()=>{},pretty:JSON.stringify,onMounted:()=>{},onBeforeUnmount:()=>{},apiAll:async path=>({items:path.includes('/products')?[{id:'smoke',category:'smoke'},{id:'sensor',category:'sensor'},{id:'gateway',category:'gateway'}]:path.includes('/device-registry')?rows:[]}),notifyError:e=>{throw e}}) /* 声明 context。 */
 vm.runInContext(script+'\nglobalThis.state={load,deviceTab,deviceCategory,registryPage,registryPageSize,registryTotal,registry,changeDeviceFilter,open,form};',context) /* 执行当前语句并推进处理流程。 */
 const s=context.state;await s.load() /* 声明 s。 */
 assert.equal(s.registryTotal.value,25);assert.equal(s.registry.value.length,20) /* 验证实际结果符合预期。 */
 s.registryPage.value=2;assert.equal(s.registry.value.length,5) /* 验证实际结果符合预期。 */
 s.deviceTab.value='children';s.changeDeviceFilter();assert.equal(s.registryPage.value,1);assert.equal(s.registryTotal.value,20) /* 验证实际结果符合预期。 */
 assert.ok(s.registry.value.every(r=>r.device.deviceRole==='CHILD')) /* 验证实际结果符合预期。 */
 s.deviceCategory.value='smoke';s.changeDeviceFilter();assert.equal(s.registryTotal.value,10) /* 验证实际结果符合预期。 */
 assert.ok(s.registry.value.every(r=>r.device.productId==='smoke')) /* 验证实际结果符合预期。 */
 s.open();assert.equal(s.form.deviceRole,'CHILD') /* 验证实际结果符合预期。 */
 s.deviceCategory.value='camera';s.changeDeviceFilter();assert.equal(s.registryTotal.value,0) /* 验证实际结果符合预期。 */
 s.deviceCategory.value='';s.deviceTab.value='main';s.changeDeviceFilter();assert.equal(s.registryTotal.value,1) /* 验证实际结果符合预期。 */
 assert.equal(s.registry.value[0].device.id,'gateway');s.open();assert.equal(s.form.deviceRole,'GATEWAY') /* 验证实际结果符合预期。 */
 s.deviceTab.value='independent';s.changeDeviceFilter();assert.equal(s.registryTotal.value,25) /* 验证实际结果符合预期。 */
 s.open();assert.equal(s.form.deviceRole,'DIRECT') /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
