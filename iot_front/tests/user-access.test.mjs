import test from 'node:test'
import assert from 'node:assert/strict'
import { userAccessPayload } from '../src/userAccess.js'
import { applyFeatureLevel, featureLevel, roleDeviceScope } from '../src/permissionPresets.js'
import fs from 'node:fs'
import vm from 'node:vm'

test('editing a fetched user excludes server-owned fields rejected by the save endpoint', () => {
  const stored = {
    username:'operator', displayName:'运维员', enabled:true, roleIds:['reader'],
    permissions:['menu:devices'], deviceScope:'selected', deviceIds:['east-smoke'],
    sessionVersion:123, tenantId:'tenant-a', passwordHash:'server-only'
  }
  const form = userAccessPayload(stored)
  form.deviceIds.push('west-smoke')
  form.permissions.push('menu:alarms')
  const payload = JSON.parse(JSON.stringify(userAccessPayload(form)))
  assert.deepEqual(payload, {
    username:'operator', displayName:'运维员', email:'', phone:'', password:'', mustChangePassword:true, enabled:true, roleIds:['reader'],
    permissions:['menu:devices','menu:alarms'], deviceScope:'selected', deviceIds:['east-smoke','west-smoke']
  })
  assert.deepEqual(stored.deviceIds, ['east-smoke'])
  assert.deepEqual(stored.permissions, ['menu:devices'])
})

test('unconfigured users default to no devices and preserve disabled accounts', () => {
  const form = userAccessPayload({ username:'operator', enabled:false, deviceIds:null, roleIds:null, permissions:null })
  assert.equal(form.deviceScope, 'none')
  assert.equal(form.enabled, false)
  assert.deepEqual(form.deviceIds, [])
  assert.deepEqual(form.roleIds, [])
  assert.deepEqual(form.permissions, [])
})

const group = {id:'menu:devices',menu:'devices',actions:[{id:'PUT /devices/:id'},{id:'DELETE /devices/:id'}]}
test('feature presets preserve other features and distinguish custom permissions', () => {
 const current = ['menu:devices','PUT /devices/:id','menu:alarms']
 assert.equal(featureLevel(group,current),'custom')
 assert.deepEqual(applyFeatureLevel(group,current,'view'),['menu:alarms','menu:devices'])
 assert.deepEqual(applyFeatureLevel(group,current,'none'),['menu:alarms'])
 const managed = applyFeatureLevel(group,current,'manage')
 assert.equal(featureLevel(group,managed),'manage')
 assert.deepEqual(current,['menu:devices','PUT /devices/:id','menu:alarms'])
})
test('assistant question preset grants both chat paths without granting workflow management', () => {
 const ai = {id:'menu:ai',menu:'ai',actions:['POST /api/v1/ai/chat','POST /api/v1/ai/chat/stream','POST /api/v1/ai/workflows'].map(id=>({id}))}
 assert.deepEqual(applyFeatureLevel(ai,[],'view'),['menu:ai','POST /api/v1/ai/chat','POST /api/v1/ai/chat/stream'])
 assert.equal(featureLevel(ai,['menu:ai','POST /api/v1/ai/chat']),'custom')
})
test('inherited device summary unions only assigned roles and handles unconfigured roles', () => {
 const roles=[{id:'east',deviceScope:'selected',deviceIds:['a','b']},{id:'west',deviceScope:'selected',deviceIds:['b','c']},{id:'all',deviceScope:'all'},{id:'legacy'}]
 assert.deepEqual(roleDeviceScope(['east','west'],roles),{deviceScope:'selected',deviceIds:['a','b','c']})
 assert.deepEqual(roleDeviceScope(['east','all'],roles),{deviceScope:'all',deviceIds:[]})
 assert.deepEqual(roleDeviceScope(['legacy','missing'],roles),{deviceScope:'none',deviceIds:[]})
})

function realtime(api, options={}){
 const timers=[],messages=[],permissionState={items:[]}
 const source=fs.readFileSync(new URL('../src/realtime.js',import.meta.url),'utf8').replace(/^import .*$/gm,'').replace(/export /g,'')
 // Mirrors the server ETag: an unchanged snapshot answers "not modified".
 const polls={full:0}
 const apiIfChanged=async(path,etag)=>{const data=await api(path);const tag=JSON.stringify(data);if(tag===etag)return {changed:false,etag};polls.full++;return {changed:true,etag:tag,data}}
 const context=vm.createContext({api,apiIfChanged,session:{role:options.role || 'operator',tenant:'tenant'},permissionState,applyAccessVersion(value=''){permissionState.accessVersion=value},refreshPermissions:async()=>{},mqtt:options.mqtt || {connect(){throw Error('managed user connected to MQTT')}},crypto:{},setTimeout(fn){timers.push(fn);return timers.length},clearTimeout(){},Map,JSON})
 vm.runInContext(source+'\nglobalThis.subject={startRealtime,stopRealtime}',context)
 return {...context.subject,timers,messages,permissionState,polls,start(){return context.subject.startRealtime((...args)=>messages.push(args))}}
}
const settle=()=>new Promise(resolve=>setImmediate(resolve))
test('受限用户按服务端范围接收新告警，不重播历史告警',async()=>{
 let snapshot={alarms:[{alarmId:'old',deviceId:'allowed'}],devices:[],permissions:['menu:devices','menu:alarms']}
 const r=realtime(async()=>snapshot);await r.start();await settle();assert.equal(r.messages.length,0)
 snapshot={...snapshot,alarms:[...snapshot.alarms,{alarmId:'new',deviceId:'allowed'}]}
 // 验证新增告警只投递一次，且消息仍使用预期的 MQTT 主题。
 await r.timers.shift()();assert.equal(r.messages.length,1);assert.equal(JSON.parse(r.messages[0][1]).alarmId,'new');assert.match(r.messages[0][0],/\/iot\/alarm\/raised\//)
 await r.timers.shift()();assert.equal(r.messages.length,1)
 snapshot={alarms:[],devices:[],permissions:[]};await r.timers.shift()();assert.equal(r.permissionState.items.length,0)
})
test('退出登录后迟到的消息响应不能进入另一个用户的页面',async()=>{
 let resolve
 const r=realtime(()=>new Promise(done=>resolve=done));await r.start();r.stopRealtime()
 resolve({alarms:[{alarmId:'secret'}],devices:[],permissions:['menu:alarms']});await settle()
 assert.equal(r.messages.length,0);assert.equal(r.permissionState.items.length,0);assert.equal(r.timers.length,0)
})

test('管理员 HTTP 访问且 MQTT 不通时仍接收新告警，重试不丢失快照',async()=>{
 let snapshot={alarms:[],devices:[],permissions:['*']}, failed=false, mqttOptions
 const r=realtime(async path=>{
  if(path.includes('mqtt/token'))return {websocketUrl:'ws://server:8083/mqtt',subscriptions:[]}
  assert.equal(path,'/api/v1/events')
  if(failed)throw Error('temporary network failure')
  return snapshot
 },{role:'admin',mqtt:{connect(url,options){mqttOptions=options;throw Error('port blocked')}}})
 await r.start();await settle()
 assert.match(mqttOptions?.clientId || '',/^iot-web-.+/,'HTTP without randomUUID must still initialize MQTT')
 failed=true;await r.timers.shift()();await settle()
 failed=false;snapshot={...snapshot,alarms:[{alarmId:'offline-new',deviceId:'allowed'}]}
 // Includes the broker retry timer and the independent event timer.
 for(const timer of r.timers.splice(0)) {await timer();await settle()}
 assert.equal(r.messages.length,1)
 assert.equal(JSON.parse(r.messages[0][1]).alarmId,'offline-new')
 assert.deepEqual(r.permissionState.items,['*'])
})

test('MQTT 续期保留告警快照，旧连接及退出后的消息不可继续投递',async()=>{
 let snapshot={alarms:[],devices:[],permissions:['*']}
 const connections=[]
 const r=realtime(async path=>path.includes('mqtt/token') ? {websocketUrl:'ws://server/mqtt',subscriptions:[]} : snapshot,{
  role:'admin',mqtt:{connect(){
   const connection={handlers:{},ended:false,on(name,fn){this.handlers[name]=fn},subscribe(){},end(){this.ended=true}}
   connections.push(connection);return connection
  }}
 })
 await r.start();await settle()
 const [poll,renew]=r.timers.splice(0)
 snapshot={...snapshot,alarms:[{alarmId:'during-renewal'}]}
 await renew()
 assert.equal(connections[0].ended,true)
 await poll()
 assert.equal(r.messages.length,1)
 assert.equal(JSON.parse(r.messages[0][1]).alarmId,'during-renewal')
 connections[0].handlers.message('/iot/parsed/tenant/device','stale')
 assert.equal(r.messages.length,1)
 connections[1].handlers.message('/iot/ui-action/tenant','current')
 assert.equal(r.messages.length,2)
 r.stopRealtime()
 assert.equal(connections[1].ended,true)
 connections[1].handlers.message('/iot/parsed/tenant/device','after-logout')
 assert.equal(r.messages.length,2)
})

test('管理员 MQTT 消息与下一次 HTTP 快照只触发一次告警刷新',async()=>{
 let snapshot={alarms:[],devices:[],permissions:['*']}
 let connection
 const r=realtime(async path=>path.includes('mqtt/token') ? {websocketUrl:'ws://server/mqtt',subscriptions:[]} : snapshot,{
  role:'admin',mqtt:{connect(){connection={on(name,fn){this[name]=fn},subscribe(){},end(){}};return connection}}
 })
 await r.start();await settle()
 const alarm={alarmId:'same-alarm',deviceId:'device',status:'ACTIVE',lastTriggeredAt:123}
 connection.message('/iot/alarm/raised/tenant',JSON.stringify(alarm))
 connection.message('/iot/alarm/raised/tenant',JSON.stringify(alarm))
 assert.equal(r.messages.length,1)
 snapshot={...snapshot,alarms:[alarm]}
 await r.timers.shift()()
 assert.equal(r.messages.length,1)
})

test('设备状态行新出现时标记为新设备，已有设备的更新不标记',async()=>{
 let snapshot={alarms:[],devices:[{deviceId:'d1',lastSeenAt:1}],permissions:['menu:devices']}
 const r=realtime(async()=>snapshot);await r.start();await settle()
 snapshot={...snapshot,devices:[{deviceId:'d1',lastSeenAt:2}]};await r.timers.shift()()
 snapshot={...snapshot,devices:[{deviceId:'d1',lastSeenAt:2},{deviceId:'d2',lastSeenAt:3}]};await r.timers.shift()()
 assert.deepEqual(r.messages.map(message=>[JSON.parse(message[1]).deviceId,message[2].added]),[['d1',false],['d2',true]])
})

test('快照未变化时不重复处理且继续轮询',async()=>{
 const snapshot={alarms:[{alarmId:'a',deviceId:'d'}],devices:[],permissions:['menu:alarms']}
 const r=realtime(async()=>snapshot);await r.start();await settle()
 await r.timers.shift()();await r.timers.shift()()
 assert.equal(r.polls.full,1);assert.equal(r.messages.length,0);assert.equal(r.timers.length,1)
})

test('增量快照只含变化的告警，按游标请求并保留此前的快照',async()=>{
 const paths=[]
 const r=realtime(async path=>{
  paths.push(path)
  if(!path.includes('since='))return {alarms:[{alarmId:'a',status:'ACTIVE'},{alarmId:'b',status:'ACTIVE'}],devices:[],permissions:['menu:alarms'],delta:false,cursor:'p.1.x'}
  if(paths.length===2)return {alarms:[{alarmId:'b',status:'ACKED'}],devices:[],permissions:['menu:alarms'],delta:true,cursor:'p.2.x'}
  return {alarms:[],devices:[],permissions:['menu:alarms'],delta:true,cursor:'p.2.x'}
 })
 await r.start();await settle();assert.equal(r.messages.length,0)
 await r.timers.shift()()
 assert.equal(paths[1],'/api/v1/events?since=p.1.x')
 assert.equal(r.messages.length,1);assert.equal(JSON.parse(r.messages[0][1]).alarmId,'b')
 // An empty delta keeps the earlier rows, so an unchanged alarm is not re-announced.
 await r.timers.shift()();assert.equal(r.messages.length,1)
})

test('截断窗口轮转不伪报新设备，窗口不变时仍定期提示刷新列表',async()=>{
 let snapshot={alarms:[],devices:[{deviceId:'d1'}],deviceTotal:600,permissions:['menu:devices'],truncated:true}
 const r=realtime(async()=>snapshot);await r.start();await settle()
 snapshot={...snapshot,devices:[{deviceId:'d2'}]};await r.timers.shift()()
 const state=r.messages.find(m=>m[0].includes('/device/state/'))
 assert.equal(state[2].added,false)
 assert.ok(r.messages.some(m=>m[0].includes('/snapshot/refresh/')))
 r.messages.length=0
 for(let i=0;i<8;i++)await r.timers.shift()()
 assert.equal(r.messages.length,1)
 assert.match(r.messages[0][0],/snapshot\/refresh/)
 snapshot={...snapshot,deviceTotal:601,devices:[{deviceId:'brand-new'}]};await r.timers.shift()()
 assert.equal(r.messages.filter(m=>m[0].includes('/device/added/')).length,1,'新设备总数增加必须继续提示')
})
