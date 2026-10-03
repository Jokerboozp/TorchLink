import { setupScript } from './helpers/vue.mjs'
import vm from 'node:vm'
import test from 'node:test'
import assert from 'node:assert/strict'
import { computed, reactive, ref, watch } from 'vue'
import { mappingConfig, mappingRows } from '../src/protocolMapping.js'
import { createClientId } from '../src/clientId.js'
import { commandBody } from '../src/commandForm.js'
import { STANDARD_PROTOCOL, configurationText, enrollRequest, fieldConfiguration, preflightQuery, protocolOptions, usesPlatformIdentity } from '../src/onboardingPlan.js'

function setup(api,initialRelease=null,browserCrypto=crypto) {
 const script=setupScript(new URL('../src/views/ProtocolAssistantView.vue',import.meta.url))
 let mount,cleanup
 const events=[]
 const context=vm.createContext({mappingRows,mappingConfig,ref,computed,reactive,watch,api,createClientId:()=>createClientId(browserCrypto),crypto:browserCrypto,FormData,AbortController,defineProps:()=>({initialRelease,initialName:'Test'}),defineEmits:()=> (...args)=>events.push(args),onMounted(fn){mount=fn},onBeforeUnmount(fn){cleanup=fn},UiMessage:{success(){},warning(){}},notifyError(){},parseJSON:JSON.parse,pretty:JSON.stringify})
 const c=vm.runInContext(script+'\n;({generate,save,runPreview,publish,changeKind,newVersion,updateConfig,mapping,currentDraft,addMapping,removeMapping,form,file,draft,saved,preview,busy,error,step})',context)
 mount();return {...c,events,cleanup:()=>cleanup()}
}

test('JSON preserve-value type remains a selectable value instead of a blank placeholder',()=>{
 const script=setupScript(new URL('../src/components/ProtocolMappingEditor.vue',import.meta.url))
 const rows=mappingRows({properties:{temperature:'$.temperature'}},'configurable_json_parser')
 const context=vm.createContext({computed,defineProps:()=>({rows,parserType:'configurable_json_parser'}),defineEmits:()=>()=>{}})
 const c=vm.runInContext(script+'\n;({types,changeType})',context)
 assert.ok(c.types.value[0], 'the keep-original-value option must not use the empty value UiSelect treats as unselected')
 c.changeType(rows[0],c.types.value[0])
 assert.equal(mappingConfig({properties:{temperature:'$.temperature'}},'configurable_json_parser',rows).properties.temperature,'$.temperature')
})

test('editing a mapping while a preview is running discards the obsolete response',async()=>{
 let finish
 const release={protocolId:'json',version:'1',parserType:'configurable_json_parser',transport:'MQTT',payloadFormat:'json',config:{properties:{temperature:'$.temperature'}},status:'PUBLISHED'}
 const c=setup(()=>new Promise(resolve=>finish=resolve),release)
 c.newVersion();c.form.samplePayload='{"temperature":25}'
 const pending=c.runPreview()
 c.mapping.value[0].path='$.newTemperature';c.updateConfig()
 finish({standardMessage:{properties:{temperature:25}}});await pending
 assert.equal(c.preview.value,null)
})

test('saving generated mapping stores a draft and emits its exact reusable version',async()=>{
 let submitted
 const release={protocolId:'json',version:'1',parserType:'configurable_json_parser',transport:'MQTT',payloadFormat:'json',config:{properties:{temperature:'$.temperature'}},status:'PUBLISHED'}
 const c=setup(async(path,options)=>{submitted=JSON.parse(options.body);return {release:{...release,version:submitted.version,status:'DRAFT'}}},release)
 c.newVersion();c.form.samplePayload='{"temperature":25}';await c.save()
 assert.equal(Object.hasOwn(submitted,'payload'),false)
 assert.equal(c.saved.value.status,'DRAFT')
 const event=c.events.find(e=>e[0]==='saved')[1]
 assert.equal(event.protocolId,'json');assert.equal(event.version,c.form.version);assert.equal(event.protocolPackageId,`json@${c.form.version}`);assert.equal(event.status,'DRAFT')
})

test('source upload validates before explicit publication and never binds a template',async()=>{
 const events=[],requests=[];let mount,cleanup
 const props={initialProtocolId:'vendor-fire',initialName:'消防协议',context:{productId:'existing-template'}}
 const release={protocolId:'vendor-fire',version:'2',status:'VALIDATED'}
 const api=async(path,options)=>{requests.push({path,options});return path.endsWith('/publish')?{...release,status:'PUBLISHED'}:options?.method==='POST'?{release,testCases:2}:{compilerAvailable:true}}
 const context=vm.createContext({ref,reactive,api,FormData,AbortController,defineProps:()=>props,defineEmits:()=>(...args)=>events.push(args),onMounted(fn){mount=fn},onBeforeUnmount(fn){cleanup=fn},UiMessage:{success(){},warning(){}},notifyError(){}})
 const script=setupScript(new URL('../src/components/ProtocolSourceUpload.vue',import.meta.url))
 const c=vm.runInContext(script+'\n;({file,upload,publish,useForTemplate,result})',context)
 await mount();c.file.value=new Blob(['package main']);await c.upload()
 const upload=requests.find(item=>item.options?.body instanceof FormData)
 assert.equal(upload.options.body.get('publish'),'false');assert.equal(upload.options.body.get('productId'),null)
 c.useForTemplate();assert.equal(events.filter(e=>e[0]==='selected').length,0)
 await c.publish();c.useForTemplate()
 const selected=events.find(e=>e[0]==='selected')[1]
 assert.equal(selected.protocolPackageId,'vendor-fire@2');assert.equal(selected.status,'PUBLISHED');assert.equal(selected.context,props.context)
 cleanup()
})

test('opening simulation creates no resource and preparation requires an explicit action',async()=>{
 const calls=[];let mount
 const context=vm.createContext({ref,computed,reactive,defineProps:()=>({}),defineEmits:()=>()=>{},onMounted(fn){mount=fn},api:async(path,options)=>{calls.push({path,options});return {device:{id:'test-device'},templates:{}}},can:()=>true,session:{tenant:'t',user:'u'},localStorage:{getItem(){return null},setItem(){},removeItem(){}},pretty:JSON.stringify,notifyError(){}})
 const script=setupScript(new URL('../src/views/TestDeviceView.vue',import.meta.url))
 const c=vm.runInContext(script+'\n;({prepare,device})',context)
 mount();assert.equal(calls.length,0);assert.equal(c.device.value,null)
 await c.prepare();assert.equal(calls.length,1);assert.equal(calls[0].path,'/api/v1/test-devices/provision')
})

test('template preview pins the bound protocol version and invalidates a pending preview',async()=>{
 let mount,cleanup,finish;const calls=[]
 const release={protocolId:'fire',version:'2',status:'PUBLISHED',parserType:'go_protocol_parser',payloadFormat:'hex'}
 const context=vm.createContext({ref,computed,watch,AbortController,can:()=>true,defineProps:()=>({context:{productId:'template'}}),onMounted(fn){mount=fn},onBeforeUnmount(fn){cleanup=fn},apiAll:async()=>({items:[{id:'template',name:'烟感',protocolPackageId:'fire@2'}]}),api:async(path,options)=>{calls.push({path,options});if(!options)return {items:[{definition:{name:'协议'},releases:[release]}]};return new Promise(resolve=>finish=resolve)},parseJSON:JSON.parse})
 const script=setupScript(new URL('../src/components/ProtocolPreviewPanel.vue',import.meta.url))
 const c=vm.runInContext(script+'\n;({payload,preview,result})',context)
 await mount();c.payload.value='AA012A';const pending=c.preview()
 const request=calls.find(item=>item.options)
 assert.match(request.path,/fire\/releases\/2\/preview$/);assert.equal(JSON.parse(request.options.body).readOnly,true)
 c.payload.value='AA0130';finish({standardMessage:{properties:{temperature:42}}});await pending
 assert.equal(c.result.value,null);cleanup()
})

function previewSetup({origin={},api,allow=()=>true}={}) {
 let mount,cleanup
 const context=vm.createContext({ref,computed,watch,AbortController,can:allow,defineProps:()=>({context:origin}),onMounted(fn){mount=fn},onBeforeUnmount(fn){cleanup=fn},apiAll:async()=>({items:[{id:'template',name:'烟感',protocolPackageId:'fire@2'}]}),api,parseJSON:JSON.parse,pretty:JSON.stringify})
 const script=setupScript(new URL('../src/components/ProtocolPreviewPanel.vue',import.meta.url))
 const c=vm.runInContext(script+'\n;({payload,state,sampleTime,packageId,operation,chunks,command,expected,preview,result,error,loadRaw,rawMessageId,rawInfo,messageKind,fillStandardSample})',context)
 return {...c,mount,cleanup:()=>cleanup()}
}

test('authorized archived sample uses its exact version and frame state without automatic preview or replay',async()=>{
 const calls=[]
 const catalog={items:[{definition:{name:'协议'},releases:['1','2'].map(version=>({protocolId:'fire',version,status:'PUBLISHED',parserType:'go_protocol_parser',payloadFormat:'hex',capabilities:['decode','ingress','encode']}))}]}
 const c=previewSetup({origin:{productId:'template',deviceId:'device',rawMessageId:'raw-1'},api:async(path,options)=>{calls.push({path,options});if(path==='/api/v2/protocols')return catalog;if(path.includes('/raw-messages/'))return {message:{messageId:'raw-1',deviceId:'device',productId:'template',protocolId:'fire',protocolVersion:'1',payloadFormat:'hex',payload:'AA01072ADC',receivedAt:12345,metadata:{protocolState:{device:7,sequence:9}}},standardMessage:{messageType:'PROPERTY_REPORT',properties:{temperature:42}}};return {operation:'decode',operationResult:{},comparison:{matched:true}}}})
 await c.mount()
 assert.equal(c.packageId.value,'fire@1');assert.equal(c.payload.value,'AA01072ADC');assert.deepEqual(JSON.parse(c.state.value),{device:7,sequence:9});assert.equal(c.sampleTime.value,12345)
 assert.equal(calls.filter(call=>call.options?.method==='POST').length,0)
 await c.preview()
 const request=calls.find(call=>call.options?.method==='POST'),body=JSON.parse(request.options.body)
 assert.match(request.path,/fire\/releases\/1\/preview$/);assert.equal(body.readOnly,true);assert.equal(body.rawMessageId,'raw-1');assert.equal(body.now,12345);assert.deepEqual(body.state,{device:7,sequence:9});assert.equal(body.expected.standardMessage.properties.temperature,42);assert.equal(Object.hasOwn(body.expected.standardMessage,'event'),false)
 assert.equal(calls.some(call=>call.path.includes('replay')),false)
 c.packageId.value='fire@2';assert.equal(c.rawInfo.value,null);await c.preview();assert.equal(Object.hasOwn(JSON.parse(calls.at(-1).options.body),'rawMessageId'),false);c.cleanup()
})

test('sample workbench checks raw permission and never substitutes a missing archive version',async()=>{
 const paths=[]
 const denied=previewSetup({origin:{rawMessageId:'raw-denied'},allow:path=>!path.startsWith('GET /api/v1/raw-messages'),api:async path=>{paths.push(path);return {items:[]}}})
 await denied.mount();assert.deepEqual(paths,['/api/v2/protocols']);denied.cleanup()
 const missing=previewSetup({origin:{rawMessageId:'raw-old'},api:async path=>path==='/api/v2/protocols'?{items:[]}:{message:{deviceId:'device',payload:'AA'},parseStatus:'UNPARSED'}})
 await missing.mount();assert.match(missing.error.value,/没有归档协议版本/);assert.equal(missing.packageId.value,'');missing.cleanup()
})

test('encode sample sends a read-only command and changing expected result invalidates its pending response',async()=>{
 let finish;const calls=[]
 const c=previewSetup({origin:{protocolId:'fire',version:'1'},api:async(path,options)=>{calls.push({path,options});if(!options)return {items:[{definition:{name:'协议'},releases:[{protocolId:'fire',version:'1',parserType:'go_protocol_parser',status:'PUBLISHED',payloadFormat:'hex',capabilities:['decode','encode']}]}]};return new Promise(resolve=>finish=resolve)}})
 await c.mount();c.operation.value='encode';c.command.value='{"type":"ping"}';c.state.value='{"device":7}';c.expected.value='{"correlationId":"1"}'
 const pending=c.preview();const body=JSON.parse(calls.find(call=>call.options).options.body)
 assert.equal(body.operation,'encode');assert.equal(body.readOnly,true);assert.deepEqual(body.command,{type:'ping'});assert.equal(Object.hasOwn(body,'payload'),false)
 c.expected.value='{"correlationId":"2"}';finish({operation:'encode',operationResult:{reply:'AA'}});await pending;assert.equal(c.result.value,null);c.cleanup()
})

test('changing the raw identifier discards an in-flight sample result',async()=>{
 let finish
 const c=previewSetup({origin:{protocolId:'fire',version:'1'},api:async(path,options)=>!options?{items:[{definition:{name:'协议'},releases:[{protocolId:'fire',version:'1',parserType:'go_protocol_parser',status:'PUBLISHED',payloadFormat:'hex',capabilities:['decode']}]}]}:new Promise(resolve=>finish=resolve)})
 await c.mount();c.payload.value='AA012A';const pending=c.preview();c.rawMessageId.value='raw-other'
 finish({standardMessage:{properties:{temperature:42}}});await pending;assert.equal(c.result.value,null);c.cleanup()
})

test('standard preview selects message kind and filling samples never sends data',async()=>{
 const calls=[]
 const c=previewSetup({origin:{protocolId:'iot-standard',version:'1.0.0'},api:async(path,options)=>{calls.push({path,options});return !options?{items:[{definition:{name:'标准协议'},releases:[{protocolId:'iot-standard',version:'1.0.0',parserType:'iot_standard_parser',status:'PUBLISHED',payloadFormat:'json'}]}]}:{operation:'decode',standardMessage:{}}}})
 await c.mount();assert.equal(c.payload.value,'')
 for(const kind of ['property','event','alarm','state','command-reply']){
   c.messageKind.value=kind;c.fillStandardSample()
   const sample=JSON.parse(c.payload.value);assert.equal(sample.version,'1.0');assert.ok(sample.timestamp>0)
 }
 assert.equal(calls.filter(call=>call.options?.method==='POST').length,0)
 await c.preview();const body=JSON.parse(calls.at(-1).options.body)
 assert.equal(body.messageKind,'command-reply');assert.equal(body.operation,'decode');assert.equal(body.readOnly,true);assert.equal(body.payload.success,true);c.cleanup()
})

test('archived standard sample sends its ingress message kind and example selection releases its identity',async()=>{
 const calls=[]
 const c=previewSetup({origin:{rawMessageId:'raw-alarm'},api:async(path,options)=>{calls.push({path,options});if(path==='/api/v2/protocols')return {items:[{definition:{name:'标准协议'},releases:[{protocolId:'iot-standard',version:'1.0.0',parserType:'iot_standard_parser',status:'PUBLISHED',payloadFormat:'json'}]}]};if(path.includes('/raw-messages/'))return {message:{messageId:'raw-alarm',deviceId:'device',protocolId:'iot-standard',protocolVersion:'1.0.0',payloadFormat:'json',headers:{messageKind:'alarm'},payload:{id:'sample',timestamp:1,data:{alarmType:'SMOKE_DETECTED'}}}};return {operation:'decode',standardMessage:{}}}})
 await c.mount();assert.equal(c.messageKind.value,'alarm');await c.preview()
 const body=JSON.parse(calls.at(-1).options.body);assert.equal(body.messageKind,'alarm');assert.equal(body.rawMessageId,'raw-alarm')
 c.fillStandardSample();assert.equal(c.rawInfo.value,null);assert.equal(c.rawMessageId.value,'');assert.equal(JSON.parse(c.payload.value).data.alarmLevel,'HIGH');c.cleanup()
})

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

test('JSON object mappings retain defaults, type conversions and unrelated settings', () => {
  const config = reactive({ properties: { temperature: { path: '$.t', type: 'number', scale: 0.1, default: 0 } }, timestampPath: '$.time', tags: { room: '$.room' } })
  const rows = mappingRows(config, 'configurable_json_parser')
  rows[0].name = 'heat'; rows[0].path = '$.data.t'
  const result = mappingConfig(config, 'configurable_json_parser', rows)
  assert.deepEqual(result.properties.heat, { path: '$.data.t', type: 'number', scale: 0.1, default: 0 })
  assert.equal(result.timestampPath, '$.time'); assert.deepEqual(result.tags, { room: '$.room' })
  assert.equal(config.properties.temperature.path, '$.t')
})

test('Modbus edits preserve alarm mappings and do not mutate the original point table', () => {
  const config = { points: [{ identifier: 'temp', name: '温度', functionCode: 3, address: 10, registerCount: 1, dataType: 'uint16', alarmMapping: { 1: 'FIRE' }, scale: 0.1 }], blocks: [{ startAddress: 10 }] }
  const rows = mappingRows(config, 'modbus_tcp_parser_v2')
  rows[0].address = 12; rows[0].identifier = 'temperature'
  const result = mappingConfig(config, 'modbus_tcp_parser_v2', rows)
  assert.equal(result.points[0].address, 12); assert.equal(result.points[0].identifier, 'temperature')
  assert.deepEqual(result.points[0].alarmMapping, { 1: 'FIRE' })
  assert.equal(Object.hasOwn(result.points[0], 'source'), false)
  assert.equal(config.points[0].address, 10)
})

test('HEX edits retain framing and checksum settings and reject invalid offsets', () => {
  const config = { startHex: 'AA', checksum: 'sum8', fields: [{ name: 'temperature', offset: 1, length: 2, type: 'uint16', endian: 'big' }] }
  const rows = mappingRows(config, 'configurable_hex_parser'); rows[0].offset = 3
  const result = mappingConfig(config, 'configurable_hex_parser', rows)
  assert.equal(result.fields[0].offset, 3); assert.equal(result.startHex, 'AA'); assert.equal(result.checksum, 'sum8')
  rows[0].offset = -1
  assert.throws(() => mappingConfig(config, 'configurable_hex_parser', rows), /无效/)
})

test('legacy JSON mapping key survives and deleting every field cannot enable implicit passthrough', () => {
  const config = { propertyMappings: { temperature: '$.t' } }
  assert.deepEqual(mappingConfig(config, 'configurable_json_parser', mappingRows(config, 'configurable_json_parser')), config)
  assert.throws(() => mappingConfig(config, 'configurable_json_parser', []), /至少保留/)
})

test('command forms preserve zero and false, omit empty optional fields and exclude undeclared values', () => {
  const op = {identifier:'set',fields:[{identifier:'value',dataType:'integer',required:true},{identifier:'enabled',dataType:'boolean',required:true},{identifier:'note',dataType:'string'}]}
  assert.deepEqual(commandBody(op,{value:0,enabled:false,note:'',extra:'ignored'}),{type:'set',data:{value:0,enabled:false}})
  assert.throws(()=>commandBody(op,{enabled:false}),/value/)
  assert.throws(()=>commandBody(op,{value:1.5,enabled:false}),/类型/)
  assert.throws(()=>commandBody(op,{value:1,enabled:'false'}),/类型/)
})

test('command forms accept structured parameters and require a defined command', () => {
  const op = {identifier:'configure',fields:[{identifier:'settings',dataType:'object',required:true},{identifier:'items',dataType:'array'}]}
  assert.deepEqual(commandBody(op,{settings:{threshold:0,enabled:false},items:[1,'x']}),{type:'configure',data:{settings:{threshold:0,enabled:false},items:[1,'x']}})
  assert.throws(()=>commandBody(op,{settings:[]}),/类型/)
  assert.throws(()=>commandBody(null,{}),/请选择/)
  assert.deepEqual(commandBody({identifier:'ping'},{}),{type:'ping',data:{}})
})

const draft = (overrides = {}) => ({
  productId: 'product-1', requestId: 'req-1',
  device: { id: ' device-1 ', name: ' 一层烟感 ', role: 'DIRECT', description: '' },
  labels: [{ key: ' 楼层 ', value: '一层' }, { key: ' ', value: 'ignored' }],
  connection: { choice: '', transport: '', port: null, host: '', unitId: 1, timeoutMs: 3000 },
  ...overrides
})

test('only published releases are offered after the standard protocol', () => {
  const options = protocolOptions([{ definition: { id: 'fire', name: '消防协议' }, releases: [{ version: '1', status: 'PUBLISHED', transport: 'TCP_UDP' }, { version: '2', status: 'VALIDATED' }] }])
  assert.deepEqual(options.map(item => item.id), [STANDARD_PROTOCOL, 'fire@1'])
})

test('daily preflight uses the saved template identity', () => {
  assert.equal(preflightQuery(draft()), 'productId=product-1')
})

test('enroll requests carry only the fields of the chosen connection', () => {
  const standard = enrollRequest(draft({ connection: { ...draft().connection, transport: 'HTTP' } }), { mode: 'standard' })
  assert.deepEqual(standard, { requestId: 'req-1', productId: 'product-1', device: { id: 'device-1', name: '一层烟感', deviceRole: 'DIRECT', tags: { 楼层: '一层' } }, connection: { mode: 'standard', transport: 'HTTP' } })

  const listenerPlan = { mode: 'listener', networks: ['tcp', 'udp'], dial: true }
  const shared = enrollRequest(draft({ connection: { ...draft().connection, choice: 'fire-tcp-26875' } }), listenerPlan)
  assert.deepEqual(shared.connection, { mode: 'listener', profileId: 'fire-tcp-26875' })
  const dial = enrollRequest(draft({ connection: { ...draft().connection, choice: 'dial', host: ' 10.0.0.8 ', port: 9000 } }), listenerPlan)
  assert.deepEqual(dial.connection, { mode: 'dial', host: '10.0.0.8', port: 9000 })

  const poll = enrollRequest(draft({ connection: { ...draft().connection, host: '192.168.1.20', port: null, unitId: 3 } }), { mode: 'poll' })
  assert.deepEqual(poll.connection, { mode: 'poll', host: '192.168.1.20', port: undefined, unitId: 3, timeoutMs: 3000 })
  assert.equal(JSON.parse(JSON.stringify(poll)).connection.port, undefined, 'an empty port lets the server apply 502')
})

test('only standard and managed modes use platform device identities', () => {
  assert.ok(usesPlatformIdentity('managed') && usesPlatformIdentity('standard') && !usesPlatformIdentity('listener'))
})

test('device-side configuration never includes a secret that is no longer shown', () => {
  const result = { mode: 'standard', device: { id: 'd1', name: '烟感', connector: 'MQTT' } }
  const accessInfo = { kind: 'standard', mqttBroker: 'mqtts://iot.example.com:8883', clientId: 'device-dk_1', upTopic: '/iot/up/t/p/d1/property', downTopic: '/iot/down/t/p/d1/command', tokenEndpoint: '/api/v1/device-mqtt/token', username: 'dk_1', sample: { version: '1.0' } }
  const withSecret = configurationText(result, accessInfo, { accessKey: 'dk_1', secret: 'ds_secret' })
  assert.match(withSecret, /Secret：ds_secret/)
  assert.match(withSecret, /MQTT Broker：mqtts:\/\/iot.example.com:8883/)
  const later = configurationText(result, accessInfo, null)
  assert.doesNotMatch(later, /ds_secret|Secret：/)
  assert.match(later, /AccessKey：dk_1/)
  const listener = fieldConfiguration({ mode: 'listener', device: { id: 'gw' }, profile: { publicHost: '', port: 26875, network: 'tcp' } }, null, null)
  assert.deepEqual(listener, [{ name: '服务器地址', value: '接入点未配置平台对外地址' }, { name: '网络', value: 'TCP' }])
})

function protocolDetails(permissions=['POST /api/v2/protocols/:id/releases/:version/preview']) {
 const allowed=ref(permissions)
 const context=vm.createContext({computed,ref,watch,defineProps:()=>({section:'protocols'}),defineEmits:()=>()=>{},onMounted(){},can:permission=>allowed.value.includes(permission)})
 const c=vm.runInContext(setupScript(new URL('../src/views/ProtocolsView.vue',import.meta.url))+'\n;({viewRelease,selectedRelease,selectedProtocol,previewOpen,previewContext,canTestMapping,canPreviewRelease,canPublishRelease,canDownloadSource,hasReleaseActions})',context)
 return {...c,allowed}
}
test('standard and Go release details expose the same permitted read-only preview action',()=>{
 const c=protocolDetails()
 for(const parserType of ['iot_standard_parser','go_protocol_parser']) {
  for(const status of ['PUBLISHED','VALIDATED']) {
   c.viewRelease({definition:{id:'protocol'}},{version:'1',parserType,status})
   assert.equal(c.canPreviewRelease.value,true);assert.equal(c.hasReleaseActions.value,true)
  }
  c.selectedRelease.value.status='REVOKED';assert.equal(c.canPreviewRelease.value,false);assert.equal(c.hasReleaseActions.value,false)
  c.selectedRelease.value.status='PUBLISHED';c.allowed.value=[];assert.equal(c.canPreviewRelease.value,false);assert.equal(c.hasReleaseActions.value,false)
  c.allowed.value=['POST /api/v2/protocols/:id/releases/:version/preview']
 }
 c.viewRelease({definition:{id:'other'}},{version:'1',parserType:'unsupported',status:'PUBLISHED'})
 assert.equal(c.hasReleaseActions.value,false)
 c.selectedRelease.value.artifact={generatedMapping:{}};assert.equal(c.canTestMapping.value,true);assert.equal(c.canPreviewRelease.value,false);assert.equal(c.hasReleaseActions.value,true)
})
test('manual release switching clears old raw preview context and pins the selected protocol identity',()=>{
 const c=protocolDetails(),release={version:'2',parserType:'iot_standard_parser',status:'PUBLISHED'}
 c.previewContext.value={protocolId:'old',version:'1',rawMessageId:'old-raw'};c.previewOpen.value=true
 c.viewRelease({definition:{id:'iot-standard',name:'标准协议'}},release)
 assert.equal(c.previewContext.value,null);assert.equal(c.previewOpen.value,false);assert.equal(c.selectedRelease.value.protocolId,'iot-standard');assert.equal(c.selectedRelease.value.version,'2');assert.equal(Object.hasOwn(release,'protocolId'),false)
})
