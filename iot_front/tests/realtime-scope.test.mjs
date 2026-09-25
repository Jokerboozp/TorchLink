import fs from 'node:fs' /* 引入当前代码需要的依赖。 */
import vm from 'node:vm' /* 引入当前代码需要的依赖。 */
import test from 'node:test' /* 引入当前代码需要的依赖。 */
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */

function realtime(api, options={}){ /* 定义 realtime 函数。 */
 const timers=[],messages=[],permissionState={items:[]} /* 声明 timers。 */
 const source=fs.readFileSync(new URL('../src/realtime.js',import.meta.url),'utf8').replace(/^import .*$/gm,'').replace(/export /g,'') /* 声明 source。 */
 const context=vm.createContext({api,session:{role:options.role || 'operator',tenant:'tenant'},permissionState,applyAccessVersion(value=''){permissionState.accessVersion=value},refreshPermissions:async()=>{},mqtt:options.mqtt || {connect(){throw Error('managed user connected to MQTT')}},crypto:{},setTimeout(fn){timers.push(fn);return timers.length},clearTimeout(){},Map,JSON}) /* 声明 context。 */
 vm.runInContext(source+'\nglobalThis.subject={startRealtime,stopRealtime}',context) /* 执行当前语句并推进处理流程。 */
 return {...context.subject,timers,messages,permissionState,start(){return context.subject.startRealtime((...args)=>messages.push(args))}} /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
const settle=()=>new Promise(resolve=>setImmediate(resolve)) /* 声明 settle。 */
test('受限用户按服务端范围接收新告警，不重播历史告警',async()=>{ /* 执行当前语句并推进处理流程。 */
 let snapshot={alarms:[{alarmId:'old',deviceId:'allowed'}],devices:[],permissions:['menu:devices','menu:alarms']} /* 声明 snapshot。 */
 const r=realtime(async()=>snapshot);await r.start();await settle();assert.equal(r.messages.length,0) /* 声明 r。 */
 snapshot={...snapshot,alarms:[...snapshot.alarms,{alarmId:'new',deviceId:'allowed'}]} /* 更新 snapshot 的值。 */
 // 验证新增告警只投递一次，且消息仍使用预期的 MQTT 主题。
 await r.timers.shift()();assert.equal(r.messages.length,1);assert.equal(JSON.parse(r.messages[0][1]).alarmId,'new');assert.match(r.messages[0][0],/\/iot\/alarm\/raised\//)
 await r.timers.shift()();assert.equal(r.messages.length,1) /* 验证实际结果符合预期。 */
 snapshot={alarms:[],devices:[],permissions:[]};await r.timers.shift()();assert.equal(r.permissionState.items.length,0) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
test('退出登录后迟到的消息响应不能进入另一个用户的页面',async()=>{ /* 执行当前语句并推进处理流程。 */
 let resolve /* 声明 resolve。 */
 const r=realtime(()=>new Promise(done=>resolve=done));await r.start();r.stopRealtime() /* 声明 r。 */
 resolve({alarms:[{alarmId:'secret'}],devices:[],permissions:['menu:alarms']});await settle() /* 执行当前语句并推进处理流程。 */
 assert.equal(r.messages.length,0);assert.equal(r.permissionState.items.length,0);assert.equal(r.timers.length,0) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('管理员 HTTP 访问且 MQTT 不通时仍接收新告警，重试不丢失快照',async()=>{ /* 执行当前语句并推进处理流程。 */
 let snapshot={alarms:[],devices:[],permissions:['*']}, failed=false, mqttOptions /* 声明 snapshot。 */
 const r=realtime(async path=>{ /* 声明 r。 */
  if(path.includes('mqtt/token'))return {websocketUrl:'ws://server:8083/mqtt',subscriptions:[]} /* 判断条件并选择处理分支。 */
  assert.equal(path,'/api/v1/events') /* 验证实际结果符合预期。 */
  if(failed)throw Error('temporary network failure') /* 判断条件并选择处理分支。 */
  return snapshot /* 返回当前处理结果。 */
 },{role:'admin',mqtt:{connect(url,options){mqttOptions=options;throw Error('port blocked')}}}) /* 结束当前表达式或代码块。 */
 await r.start();await settle() /* 等待异步操作完成。 */
 assert.match(mqttOptions?.clientId || '',/^iot-web-.+/,'HTTP without randomUUID must still initialize MQTT') /* 验证实际结果符合预期。 */
 failed=true;await r.timers.shift()();await settle() /* 更新 failed 的值。 */
 failed=false;snapshot={...snapshot,alarms:[{alarmId:'offline-new',deviceId:'allowed'}]} /* 更新 failed 的值。 */
 // Includes the broker retry timer and the independent event timer.
 for(const timer of r.timers.splice(0)) {await timer();await settle()} /* 循环处理当前数据。 */
 assert.equal(r.messages.length,1) /* 验证实际结果符合预期。 */
 assert.equal(JSON.parse(r.messages[0][1]).alarmId,'offline-new') /* 验证实际结果符合预期。 */
 assert.deepEqual(r.permissionState.items,['*']) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('MQTT 续期保留告警快照，旧连接及退出后的消息不可继续投递',async()=>{ /* 执行当前语句并推进处理流程。 */
 let snapshot={alarms:[],devices:[],permissions:['*']} /* 声明 snapshot。 */
 const connections=[] /* 声明 connections。 */
 const r=realtime(async path=>path.includes('mqtt/token') ? {websocketUrl:'ws://server/mqtt',subscriptions:[]} : snapshot,{ /* 声明 r。 */
  role:'admin',mqtt:{connect(){ /* 执行当前语句并推进处理流程。 */
   const connection={handlers:{},ended:false,on(name,fn){this.handlers[name]=fn},subscribe(){},end(){this.ended=true}} /* 声明 connection。 */
   connections.push(connection);return connection /* 执行当前语句并推进处理流程。 */
  }} /* 结束当前表达式或代码块。 */
 }) /* 结束当前表达式或代码块。 */
 await r.start();await settle() /* 等待异步操作完成。 */
 const [poll,renew]=r.timers.splice(0) /* 执行当前语句并推进处理流程。 */
 snapshot={...snapshot,alarms:[{alarmId:'during-renewal'}]} /* 更新 snapshot 的值。 */
 await renew() /* 等待异步操作完成。 */
 assert.equal(connections[0].ended,true) /* 验证实际结果符合预期。 */
 await poll() /* 等待异步操作完成。 */
 assert.equal(r.messages.length,1) /* 验证实际结果符合预期。 */
 assert.equal(JSON.parse(r.messages[0][1]).alarmId,'during-renewal') /* 验证实际结果符合预期。 */
 connections[0].handlers.message('/iot/parsed/tenant/device','stale') /* 执行当前语句并推进处理流程。 */
 assert.equal(r.messages.length,1) /* 验证实际结果符合预期。 */
 connections[1].handlers.message('/iot/ui-action/tenant','current') /* 执行当前语句并推进处理流程。 */
 assert.equal(r.messages.length,2) /* 验证实际结果符合预期。 */
 r.stopRealtime() /* 执行当前语句并推进处理流程。 */
 assert.equal(connections[1].ended,true) /* 验证实际结果符合预期。 */
 connections[1].handlers.message('/iot/parsed/tenant/device','after-logout') /* 执行当前语句并推进处理流程。 */
 assert.equal(r.messages.length,2) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */

test('管理员 MQTT 消息与下一次 HTTP 快照只触发一次告警刷新',async()=>{ /* 执行当前语句并推进处理流程。 */
 let snapshot={alarms:[],devices:[],permissions:['*']} /* 声明 snapshot。 */
 let connection /* 声明 connection。 */
 const r=realtime(async path=>path.includes('mqtt/token') ? {websocketUrl:'ws://server/mqtt',subscriptions:[]} : snapshot,{ /* 声明 r。 */
  role:'admin',mqtt:{connect(){connection={on(name,fn){this[name]=fn},subscribe(){},end(){}};return connection}} /* 执行当前语句并推进处理流程。 */
 }) /* 结束当前表达式或代码块。 */
 await r.start();await settle() /* 等待异步操作完成。 */
 const alarm={alarmId:'same-alarm',deviceId:'device',status:'ACTIVE',lastTriggeredAt:123} /* 声明 alarm。 */
 connection.message('/iot/alarm/raised/tenant',JSON.stringify(alarm)) /* 执行当前语句并推进处理流程。 */
 connection.message('/iot/alarm/raised/tenant',JSON.stringify(alarm)) /* 执行当前语句并推进处理流程。 */
 assert.equal(r.messages.length,1) /* 验证实际结果符合预期。 */
 snapshot={...snapshot,alarms:[alarm]} /* 更新 snapshot 的值。 */
 await r.timers.shift()() /* 等待异步操作完成。 */
 assert.equal(r.messages.length,1) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
