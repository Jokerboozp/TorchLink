import fs from 'node:fs' /* 引入当前代码需要的依赖。 */
import vm from 'node:vm' /* 引入当前代码需要的依赖。 */
import test from 'node:test' /* 引入当前代码需要的依赖。 */
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */
import {ref,computed,reactive} from 'vue' /* 引入当前代码需要的依赖。 */
import {mappingRows,mappingConfig} from '../src/protocolMapping.js' /* 引入当前代码需要的依赖。 */
import {createClientId} from '../src/clientId.js' /* 引入当前代码需要的依赖。 */

function setup(api,initialRelease=null,browserCrypto=crypto) { /* 定义 setup 函数。 */
 const script=fs.readFileSync(new URL('../src/views/ProtocolAssistantView.vue',import.meta.url),'utf8').match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm,'') /* 声明 script。 */
 let mount,cleanup /* 声明 mount。 */
 const context=vm.createContext({mappingRows,mappingConfig,ref,computed,reactive,api,createClientId:()=>createClientId(browserCrypto),crypto:browserCrypto,FormData,AbortController,defineProps:()=>({initialRelease,initialName:'Test'}),defineEmits:()=>()=>{},onMounted(fn){mount=fn},onBeforeUnmount(fn){cleanup=fn},ElMessage:{success(){},warning(){}},notifyError(){},parseJSON:JSON.parse,pretty:JSON.stringify}) /* 声明 context。 */
 const c=vm.runInContext(script+'\n;({generate,save,runPreview,publish,changeKind,newVersion,updateConfig,mapping,currentDraft,addMapping,removeMapping,form,file,draft,saved,preview,busy,error,step})',context) /* 声明 c。 */
 mount();return {...c,cleanup:()=>cleanup()} /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

test('HTTP 页面没有 randomUUID 时仍能初始化协议生成表单',()=>{ /* 执行当前语句并推进处理流程。 */
 const c=setup(async()=>({}),null,{getRandomValues:array=>crypto.getRandomValues(array)}) /* 声明 c。 */
 assert.equal(c.step.value,'input') /* 验证实际结果符合预期。 */
 assert.match(c.form.protocol,/^protocol-[0-9a-f]{8}$/) /* 验证实际结果符合预期。 */
 c.form.inputKind='point-table';c.changeKind() /* 更新 c.form.inputKind 的值。 */
 assert.equal(c.form.transport,'MODBUS_TCP') /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
test('message-only generation does not require a document and prevents double submission',async()=>{ /* 执行当前语句并推进处理流程。 */
 let finish;const requests=[] /* 声明 finish。 */
 const c=setup((path,options)=>{requests.push({path,options});return new Promise(resolve=>{finish=resolve})}) /* 声明 c。 */
 c.form.samplePayload='{"temperature":25}' /* 更新 c.form.samplePayload 的值。 */
 const pending=c.generate();await c.generate();assert.equal(requests.length,1) /* 声明 pending。 */
 assert.equal(requests[0].options.body.get('samplePayload'),' {"temperature":25}'.trim()) /* 验证实际结果符合预期。 */
 finish({name:'JSON',parserType:'configurable_json_parser',config:{properties:{temperature:'$.temperature'}},samplePayload:{temperature:25},transport:'MQTT',payloadFormat:'json'}) /* 执行当前语句并推进处理流程。 */
 await pending;assert.equal(c.step.value,'review');assert.equal(c.busy.value,'') /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
test('saved point-table draft can resume preview and publish the same immutable version',async()=>{ /* 执行当前语句并推进处理流程。 */
 const release={protocolId:'points',version:'1',parserType:'modbus_tcp_parser_v2',transport:'MODBUS_TCP',payloadFormat:'hex',config:{points:[],blocks:[{startAddress:12}]},status:'DRAFT'} /* 声明 release。 */
 const calls=[] /* 声明 calls。 */
 const c=setup(async(path,options)=>{calls.push({path,body:JSON.parse(options.body)});return path.endsWith('/preview')?{release:{...release,status:'VALIDATED'},standardMessage:{properties:{temperature:25}}}:{...release,status:'PUBLISHED'}},release) /* 声明 c。 */
 assert.equal(c.form.inputKind,'point-table') /* 验证实际结果符合预期。 */
 c.form.samplePayload='00 01 00 00 00 05 01 03 02 00 FA' /* 更新 c.form.samplePayload 的值。 */
 await c.runPreview();assert.equal(c.saved.value.status,'VALIDATED');assert.equal(calls[0].body.startAddress,12) /* 验证实际结果符合预期。 */
 await c.publish();assert.equal(c.saved.value.status,'PUBLISHED');assert.match(calls[1].path,/points\/releases\/1\/publish$/) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
test('closing a pending generation cannot restore stale results',async()=>{ /* 执行当前语句并推进处理流程。 */
 let finish;const c=setup(()=>new Promise(resolve=>finish=resolve));c.form.samplePayload='{"x":1}' /* 声明 finish。 */
 const pending=c.generate();c.cleanup();finish({name:'late',config:{}});await pending;assert.equal(c.draft.value,null) /* 声明 pending。 */
}) /* 结束当前表达式或代码块。 */
test('switching upload kinds clears incompatible previous content',()=>{ /* 执行当前语句并推进处理流程。 */
 const c=setup(async()=>({}));c.form.samplePayload='{"x":1}';c.form.pointTable='old';c.file.value={name:'old.json'};c.form.inputKind='point-table';c.changeKind() /* 声明 c。 */
 assert.equal(c.form.samplePayload,'');assert.equal(c.form.pointTable,'');assert.equal(c.file.value,null);assert.equal(c.form.transport,'MODBUS_TCP') /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('editing a new version does not mutate the saved release',()=>{ /* 执行当前语句并推进处理流程。 */
 const release={protocolId:'json',version:'1',parserType:'configurable_json_parser',transport:'MQTT',payloadFormat:'json',config:{properties:{temperature:'$.temperature'}},status:'PUBLISHED'} /* 声明 release。 */
 const c=setup(async()=>({}),release);c.newVersion();c.mapping.value[0].path='$.data.temperature';c.updateConfig() /* 声明 c。 */
 assert.equal(c.saved.value,null);assert.notEqual(c.form.version,'1');assert.equal(release.config.properties.temperature,'$.temperature');assert.equal(c.currentDraft().config.properties.temperature,'$.data.temperature') /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */


test('edited mapping is sent to preview and save, and old preview is invalidated',async()=>{ /* 执行当前语句并推进处理流程。 */
 const calls=[] /* 声明 calls。 */
 const release={protocolId:'json',version:'1',parserType:'configurable_json_parser',transport:'MQTT',payloadFormat:'json',config:{properties:{temperature:'$.temperature'}},status:'PUBLISHED'} /* 声明 release。 */
 const c=setup(async(path,options)=>{const body=JSON.parse(options.body);calls.push({path,body});return path.endsWith('/preview')?{standardMessage:{properties:{heat:30}}}:{release:{...release,config:body.draft.config,status:'VALIDATED'}}},release) /* 声明 c。 */
 c.newVersion();c.form.samplePayload='{"data":{"t":30}}';c.preview.value={properties:{temperature:25}} /* 执行当前语句并推进处理流程。 */
 c.mapping.value[0].name='heat';c.mapping.value[0].path='$.data.t';c.updateConfig();assert.equal(c.preview.value,null) /* 验证实际结果符合预期。 */
 await c.runPreview();await c.save() /* 等待异步操作完成。 */
 assert.equal(calls[0].body.draft.config.properties.heat,'$.data.t');assert.equal(calls[1].body.draft.config.properties.heat,'$.data.t') /* 验证实际结果符合预期。 */
 assert.equal(release.config.properties.temperature,'$.temperature') /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('duplicate or blank field identifiers stop requests with a readable error',async()=>{ /* 执行当前语句并推进处理流程。 */
 let requests=0 /* 声明 requests。 */
 const release={protocolId:'json',version:'1',parserType:'configurable_json_parser',transport:'MQTT',payloadFormat:'json',config:{properties:{temperature:'$.temperature'}},status:'PUBLISHED'} /* 声明 release。 */
 const c=setup(async()=>{requests++},release);c.newVersion();c.addMapping();await c.save() /* 声明 c。 */
 assert.match(c.error.value,/字段标识/);assert.equal(requests,0) /* 验证实际结果符合预期。 */
 Object.assign(c.mapping.value[1],{name:'temperature',path:'$.other'});await c.save();assert.match(c.error.value,/重复/);assert.equal(requests,0) /* 验证实际结果符合预期。 */
 c.removeMapping(1);assert.equal(c.mapping.value.length,1) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
