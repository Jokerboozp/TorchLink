import fs from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import assert from 'node:assert/strict'

function realtime(api){
 const timers=[],messages=[],permissionState={items:[]}
 const source=fs.readFileSync(new URL('../src/realtime.js',import.meta.url),'utf8').replace(/^import .*$/gm,'').replace(/export /g,'')
 const context=vm.createContext({api,session:{role:'operator',tenant:'tenant'},permissionState,refreshPermissions:async()=>{},mqtt:{connect(){throw Error('managed user connected to MQTT')}},setTimeout(fn){timers.push(fn);return timers.length},clearTimeout(){},Map,JSON})
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
