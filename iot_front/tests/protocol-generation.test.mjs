import fs from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import assert from 'node:assert/strict'
import {ref,computed,reactive} from 'vue'
import {mappingRows,mappingConfig} from '../src/protocolMapping.js'
import {createClientId} from '../src/clientId.js'

function setup(api,initialRelease=null,browserCrypto=crypto) {
 const script=fs.readFileSync(new URL('../src/views/ProtocolAssistantView.vue',import.meta.url),'utf8').match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm,'')
 let mount,cleanup
 const context=vm.createContext({mappingRows,mappingConfig,ref,computed,reactive,api,createClientId:()=>createClientId(browserCrypto),crypto:browserCrypto,FormData,AbortController,defineProps:()=>({initialRelease,initialName:'Test'}),defineEmits:()=>()=>{},onMounted(fn){mount=fn},onBeforeUnmount(fn){cleanup=fn},ElMessage:{success(){},warning(){}},notifyError(){},parseJSON:JSON.parse,pretty:JSON.stringify})
 const c=vm.runInContext(script+'\n;({generate,save,runPreview,publish,changeKind,newVersion,updateConfig,mapping,currentDraft,addMapping,removeMapping,form,file,draft,saved,preview,busy,error,step})',context)
 mount();return {...c,cleanup:()=>cleanup()}
}

test('HTTP 页面没有 randomUUID 时仍能初始化协议生成表单',()=>{
 const c=setup(async()=>({}),null,{getRandomValues:array=>crypto.getRandomValues(array)})
 assert.equal(c.step.value,'input')
 assert.match(c.form.protocol,/^protocol-[0-9a-f]{8}$/)
 c.form.inputKind='point-table';c.changeKind()
 assert.equal(c.form.transport,'MODBUS_TCP')
})
test('message-only generation does not require a document and prevents double submission',async()=>{
 let finish;const requests=[]
 const c=setup((path,options)=>{requests.push({path,options});return new Promise(resolve=>{finish=resolve})})
 c.form.samplePayload='{"temperature":25}'
 const pending=c.generate();await c.generate();assert.equal(requests.length,1)
 assert.equal(requests[0].options.body.get('samplePayload'),' {"temperature":25}'.trim())
 finish({name:'JSON',parserType:'configurable_json_parser',config:{properties:{temperature:'$.temperature'}},samplePayload:{temperature:25},transport:'MQTT',payloadFormat:'json'})
 await pending;assert.equal(c.step.value,'review');assert.equal(c.busy.value,'')
})
test('saved point-table draft can resume preview and publish the same immutable version',async()=>{
 const release={protocolId:'points',version:'1',parserType:'modbus_tcp_parser_v2',transport:'MODBUS_TCP',payloadFormat:'hex',config:{points:[],blocks:[{startAddress:12}]},status:'DRAFT'}
 const calls=[]
 const c=setup(async(path,options)=>{calls.push({path,body:JSON.parse(options.body)});return path.endsWith('/preview')?{release:{...release,status:'VALIDATED'},standardMessage:{properties:{temperature:25}}}:{...release,status:'PUBLISHED'}},release)
 assert.equal(c.form.inputKind,'point-table')
 c.form.samplePayload='00 01 00 00 00 05 01 03 02 00 FA'
 await c.runPreview();assert.equal(c.saved.value.status,'VALIDATED');assert.equal(calls[0].body.startAddress,12)
 await c.publish();assert.equal(c.saved.value.status,'PUBLISHED');assert.match(calls[1].path,/points\/releases\/1\/publish$/)
})
test('closing a pending generation cannot restore stale results',async()=>{
 let finish;const c=setup(()=>new Promise(resolve=>finish=resolve));c.form.samplePayload='{"x":1}'
 const pending=c.generate();c.cleanup();finish({name:'late',config:{}});await pending;assert.equal(c.draft.value,null)
})
test('switching upload kinds clears incompatible previous content',()=>{
 const c=setup(async()=>({}));c.form.samplePayload='{"x":1}';c.form.pointTable='old';c.file.value={name:'old.json'};c.form.inputKind='point-table';c.changeKind()
 assert.equal(c.form.samplePayload,'');assert.equal(c.form.pointTable,'');assert.equal(c.file.value,null);assert.equal(c.form.transport,'MODBUS_TCP')
})

test('editing a new version does not mutate the saved release',()=>{
 const release={protocolId:'json',version:'1',parserType:'configurable_json_parser',transport:'MQTT',payloadFormat:'json',config:{properties:{temperature:'$.temperature'}},status:'PUBLISHED'}
 const c=setup(async()=>({}),release);c.newVersion();c.mapping.value[0].path='$.data.temperature';c.updateConfig()
 assert.equal(c.saved.value,null);assert.notEqual(c.form.version,'1');assert.equal(release.config.properties.temperature,'$.temperature');assert.equal(c.currentDraft().config.properties.temperature,'$.data.temperature')
})


test('edited mapping is sent to preview and save, and old preview is invalidated',async()=>{
 const calls=[]
 const release={protocolId:'json',version:'1',parserType:'configurable_json_parser',transport:'MQTT',payloadFormat:'json',config:{properties:{temperature:'$.temperature'}},status:'PUBLISHED'}
 const c=setup(async(path,options)=>{const body=JSON.parse(options.body);calls.push({path,body});return path.endsWith('/preview')?{standardMessage:{properties:{heat:30}}}:{release:{...release,config:body.draft.config,status:'VALIDATED'}}},release)
 c.newVersion();c.form.samplePayload='{"data":{"t":30}}';c.preview.value={properties:{temperature:25}}
 c.mapping.value[0].name='heat';c.mapping.value[0].path='$.data.t';c.updateConfig();assert.equal(c.preview.value,null)
 await c.runPreview();await c.save()
 assert.equal(calls[0].body.draft.config.properties.heat,'$.data.t');assert.equal(calls[1].body.draft.config.properties.heat,'$.data.t')
 assert.equal(release.config.properties.temperature,'$.temperature')
})

test('duplicate or blank field identifiers stop requests with a readable error',async()=>{
 let requests=0
 const release={protocolId:'json',version:'1',parserType:'configurable_json_parser',transport:'MQTT',payloadFormat:'json',config:{properties:{temperature:'$.temperature'}},status:'PUBLISHED'}
 const c=setup(async()=>{requests++},release);c.newVersion();c.addMapping();await c.save()
 assert.match(c.error.value,/字段标识/);assert.equal(requests,0)
 Object.assign(c.mapping.value[1],{name:'temperature',path:'$.other'});await c.save();assert.match(c.error.value,/重复/);assert.equal(requests,0)
 c.removeMapping(1);assert.equal(c.mapping.value.length,1)
})
