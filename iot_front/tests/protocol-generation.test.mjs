import { setupScript } from './helpers/vue.mjs'
import vm from 'node:vm'
import test from 'node:test'
import assert from 'node:assert/strict'
import { computed, reactive, ref } from 'vue'
import { mappingConfig, mappingRows } from '../src/protocolMapping.js'
import { createClientId } from '../src/clientId.js'
import { commandBody } from '../src/commandForm.js'
import { STANDARD_PROTOCOL, configurationText, connectionMode, enrollRequest, fieldConfiguration, preflightQuery, protocolOptions, transportChoices, usesPlatformIdentity } from '../src/onboardingPlan.js'

function setup(api,initialRelease=null,browserCrypto=crypto) {
 const script=setupScript(new URL('../src/views/ProtocolAssistantView.vue',import.meta.url))
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
  source: 'existing', productId: 'product-1', requestId: 'req-1',
  newProduct: { id: 'product_new', name: ' 新型号 ', category: 'smoke', protocolPackageId: 'fire@1.0.0', transport: 'TCP', manufacturer: ' 大华 ', model: '' },
  device: { id: ' device-1 ', name: ' 一层烟感 ', role: 'DIRECT', description: '' },
  labels: [{ key: ' 楼层 ', value: '一层' }, { key: ' ', value: 'ignored' }],
  connection: { choice: '', transport: '', network: '', port: null, publicHost: '', bindHost: '', host: '', unitId: 1, timeoutMs: 3000 },
  ...overrides
})

test('only published releases are offered after the standard protocol', () => {
  const options = protocolOptions([{ definition: { id: 'fire', name: '消防协议' }, releases: [{ version: '1', status: 'PUBLISHED', transport: 'TCP_UDP' }, { version: '2', status: 'VALIDATED' }] }])
  assert.deepEqual(options.map(item => item.id), [STANDARD_PROTOCOL, 'fire@1'])
  assert.deepEqual(transportChoices('TCP_UDP'), ['TCP', 'UDP'])
  assert.deepEqual(transportChoices('TCP'), [])
})

test('preflight uses the saved template or the template draft', () => {
  assert.equal(preflightQuery(draft()), 'productId=product-1')
  const query = new URLSearchParams(preflightQuery(draft({ source: 'new' })))
  assert.equal(query.get('protocolPackageId'), 'fire@1.0.0')
  assert.equal(query.get('transport'), 'TCP')
  assert.equal(query.get('productId'), null)
})

test('enroll requests carry only the fields of the chosen connection', () => {
  const standard = enrollRequest(draft({ connection: { ...draft().connection, transport: 'HTTP' } }), { mode: 'standard' })
  assert.deepEqual(standard, { requestId: 'req-1', productId: 'product-1', device: { id: 'device-1', name: '一层烟感', deviceRole: 'DIRECT', tags: { 楼层: '一层' } }, connection: { mode: 'standard', transport: 'HTTP' } })

  const listenerPlan = { mode: 'listener', networks: ['tcp', 'udp'], dial: true }
  const shared = enrollRequest(draft({ connection: { ...draft().connection, choice: 'fire-tcp-26875' } }), listenerPlan)
  assert.deepEqual(shared.connection, { mode: 'listener', profileId: 'fire-tcp-26875' })
  const created = enrollRequest(draft({ connection: { ...draft().connection, choice: 'new', publicHost: ' iot.example.com ', port: 26875 } }), listenerPlan)
  assert.deepEqual(created.connection, { mode: 'listener', listener: { network: 'tcp', host: '', publicHost: 'iot.example.com', port: 26875 } })
  assert.equal(connectionMode(listenerPlan, 'dial'), 'dial')
  const dial = enrollRequest(draft({ connection: { ...draft().connection, choice: 'dial', host: ' 10.0.0.8 ', port: 9000 } }), listenerPlan)
  assert.deepEqual(dial.connection, { mode: 'dial', host: '10.0.0.8', port: 9000 })

  const poll = enrollRequest(draft({ connection: { ...draft().connection, host: '192.168.1.20', port: null, unitId: 3 } }), { mode: 'poll' })
  assert.deepEqual(poll.connection, { mode: 'poll', host: '192.168.1.20', port: undefined, unitId: 3, timeoutMs: 3000 })
  assert.equal(JSON.parse(JSON.stringify(poll)).connection.port, undefined, 'an empty port lets the server apply 502')
})

test('a new template is created in the same request', () => {
  const body = enrollRequest(draft({ source: 'new' }), { mode: 'managed' })
  assert.equal(body.productId, undefined)
  assert.deepEqual(body.newProduct, { id: 'product_new', name: '新型号', category: 'smoke', protocolPackageId: 'fire@1.0.0', transport: 'TCP', metadata: { manufacturer: '大华' } })
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
