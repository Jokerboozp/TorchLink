import fs from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import assert from 'node:assert/strict'

function realtime(api, options={}){
 const timers=[],messages=[],permissionState={items:[]}
 const source=fs.readFileSync(new URL('../src/realtime.js',import.meta.url),'utf8').replace(/^import .*$/gm,'').replace(/export /g,'')
 const context=vm.createContext({api,session:{role:options.role || 'operator',tenant:'tenant'},permissionState,refreshPermissions:async()=>{},mqtt:options.mqtt || {connect(){throw Error('managed user connected to MQTT')}},crypto:{},setTimeout(fn){timers.push(fn);return timers.length},clearTimeout(){},Map,JSON})
 vm.runInContext(source+'\nglobalThis.subject={startRealtime,stopRealtime}',context)
 return {...context.subject,timers,messages,permissionState,start(){return context.subject.startRealtime((...args)=>messages.push(args))}}
}
const settle=()=>new Promise(resolve=>setImmediate(resolve))
test('受限用户按服务端范围接收新告警，不重播历史告警',async()=>{
 let snapshot={alarms:[{alarmId:'old',deviceId:'allowed'}],devices:[],permissions:['menu:devices','menu:alarms']}
 const r=realtime(async()=>snapshot);await r.start();await settle();assert.equal(r.messages.length,0)
 snapshot={...snapshot,alarms:[...snapshot.alarms,{alarmId:'new',deviceId:'allowed'}]}
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
