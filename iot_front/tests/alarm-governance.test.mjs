import assert from 'node:assert/strict'
import test from 'node:test'
import vm from 'node:vm'
import { readFileSync } from 'node:fs'
import { computed, reactive, ref, watch } from 'vue'
import { setupScript } from './helpers/vue.mjs'
import { activeRun, conflictMessage, numericMetric, recordBody, resultOptions, sameSignal, time, verificationPayload } from '../src/governance/helpers.js'
const { canGovernance, governanceAction } = vm.runInNewContext(readFileSync(new URL('../src/governance/permissions.js',import.meta.url),'utf8').replace(/^import .*$/gm,'').replace(/export /g,'')+';({canGovernance,governanceAction})',{can:()=>false})

const point={deviceId:'a',componentId:'smoke-1',alarmType:'FIRE',originKind:'COMPONENT_STATE',signalKey:'component:smoke-1:FIRE'}
const observation={id:'event-a',...point}
function draft(){return {...point,observationIds:['event-a'],templateRevisionId:'template',scenePresetRevisionId:'scene',typeProfileRevisionId:'profile',verificationMethod:'UNVERIFIED',verifiedAt:null,fieldResult:'UNABLE_TO_DETERMINE',activityRelation:'UNKNOWN',facilityStatus:'UNKNOWN',fieldValues:{},idempotencyKey:'retry-key'}}

test('verification preserves explicit unknown values without inventing an actual visit or normal conclusion',()=>{
 const form=draft(), before=structuredClone(form), result=verificationPayload(form,[observation])
 assert.equal(result.verifiedAt,null);assert.equal(result.fieldResult,'UNABLE_TO_DETERMINE');assert.equal(result.fieldValues.facilityStatus,'UNKNOWN');assert.deepEqual(form,before)
 assert.throws(()=>verificationPayload({...form,verificationMethod:'ON_SITE'},[observation]),/实际核实时间/)
 assert.throws(()=>verificationPayload({...form,fieldResult:''},[observation]),/三项现场观测/)
})
test('verification cannot mix devices, components, types, origins, rules or missing event identities',()=>{
 for(const key of ['deviceId','componentId','alarmType','originKind','signalKey']){
  const other={...observation,id:'event-b',[key]:'different'}
  assert.equal(sameSignal(observation,other),false)
  assert.throws(()=>verificationPayload({...draft(),observationIds:['event-a','event-b']},[observation,other]),/同一个点位/)
 }
 assert.throws(()=>verificationPayload({...draft(),observationIds:['absent']},[observation]),/同一个点位/)
 assert.throws(()=>verificationPayload({...draft(),observationIds:[]},[observation]),/实际核实的事件/)
 assert.deepEqual(resultOptions('FAULT'),['TARGET_ABNORMALITY_OBSERVED','OTHER_ABNORMALITY_OBSERVED','NO_TARGET_ABNORMALITY_OBSERVED','UNABLE_TO_DETERMINE'])
})
test('immutable envelope identity, missing clocks and unknown denominators remain distinct from zero',()=>{
 assert.deepEqual(recordBody({id:'revision',version:4,body:{id:'body-id',status:'CONFIRMED'}}),{id:'revision',version:4,status:'CONFIRMED',createdAt:undefined,updatedAt:undefined})
 for(const value of [null,undefined,'',0])assert.equal(time(value),'未知')
 assert.equal(numericMetric(0),'0');assert.equal(numericMetric(null),'不可计算');assert.equal(numericMetric({numerator:0,denominator:0}),'不可计算');assert.equal(numericMetric({numerator:1,denominator:4,ratio:.25}),'1 / 4 · 25.0%')
 assert.equal(activeRun({status:'PARTIAL'}),false)
})
test('precise governance grants keep source permissions, acceptance, completion and reopening separate',()=>{
 const grants=new Set(['menu:alarmGovernance','menu:devices','menu:alarms','action:alarmGovernance:record','action:alarmGovernance:measures'])
 const check=id=>grants.has(id)
 assert.equal(canGovernance('POST /api/v1/alarm-governance/verifications',check),true)
 assert.equal(canGovernance('POST /api/v1/alarm-governance/measures/:id/implement',check),true)
 for(const path of ['measures/:id/verify','cases/:id/complete','cases/:id/reopen','runs/:id/ai-jobs'])assert.equal(canGovernance(`POST /api/v1/alarm-governance/${path}`,check),false)
 assert.equal(governanceAction('POST /api/v1/alarm-governance/rounds/:id/causes'),'cause')
 assert.equal(canGovernance('GET /api/v1/alarm-governance/video-events',check),false);grants.add('action:cameras:history');assert.equal(canGovernance('GET /api/v1/alarm-governance/video-events',check),true);assert.equal(canGovernance('GET /api/v1/alarm-governance/business-links/:id/media',check),false);grants.add('action:cameras:download');assert.equal(canGovernance('GET /api/v1/alarm-governance/business-links/:id/media',check),true);assert.equal(governanceAction('POST /api/v1/alarm-governance/historical-projections'),'analyse');
 grants.delete('menu:alarms');assert.equal(canGovernance('POST /api/v1/alarm-governance/verifications',check),false)
 grants.add('action:alarmGovernance:templates');assert.equal(canGovernance('POST /api/v1/alarm-governance/templates',check),true)
})

function component(file,{props={},read=async()=>({items:[]}),write=async()=>({}),can=()=>true}={}){
 let unmount=()=>{},count=0;const timers=new Map(),events=[]
 const context=vm.createContext({computed,reactive,ref,watch,onMounted(){},onBeforeUnmount(fn){unmount=fn},defineProps:()=>reactive(props),defineEmits:()=>((...args)=>events.push(args)),governanceRead:read,governanceWrite:write,governanceUpload:async()=>({}),governanceAttachment(){},governanceExport(){},can,createClientId:()=>`key-${++count}`,UiMessage:{success(){}},recordBody,label:value=>value,time,tone:()=>'',sameSignal,verificationPayload,conflictMessage,activeRun,numericMetric,outputRows:rows=>rows.map(row=>({...row.body,id:row.id})),setTimeout:fn=>{timers.set(++count,fn);return count},clearTimeout:id=>timers.delete(id)})
 const state=vm.runInContext(`${setupScript(new URL('../src/'+file,import.meta.url))}\n;({${file.endsWith('GovernanceResults.vue')?'load,startAI,cancelAI,snapshot,metrics,aiJobs,aiError,showFact,fact,factDialog':'load,form,observations,save,error,loading'}})`,context)
 return {state,timers,events,unmount:()=>unmount()}
}
test('reading a completed fixed analysis does not start AI and unmount discards late facts',async()=>{
 const pending=[],writes=[]
 const {state,unmount}=component('components/governance/GovernanceResults.vue',{props:{run:{id:'run-a',snapshotId:'snapshot-a',status:'SUCCEEDED'},devices:[]},read:(_collection,_id,operation)=>new Promise(resolve=>pending.push({operation,resolve})),write:async(...args)=>writes.push(args)})
 const running=state.load();unmount();for(const request of pending)request.resolve(request.operation==='snapshot'?{id:'snapshot-a'}:{items:[]});await running
 assert.equal(state.snapshot.value,null);assert.equal(state.metrics.value.length,0);assert.equal(writes.length,0)
})
test('denied AI action never submits and explicit AI failure preserves fixed facts',async()=>{
 const writes=[]
 const props={run:{id:'run-a',snapshotId:'snapshot-a',status:'SUCCEEDED'},devices:[]}
 const denied=component('components/governance/GovernanceResults.vue',{props,can:()=>false,write:async(...args)=>writes.push(args)})
 await denied.state.startAI();assert.equal(writes.length,0)
 const allowed=component('components/governance/GovernanceResults.vue',{props,read:async(_c,_id,operation)=>operation==='snapshot'?{id:'snapshot-a',factsHash:'fixed'}:operation?{items:[]}:{version:2},write:async()=>{throw Error('模型不可用')}})
 await allowed.state.load();await allowed.state.startAI();assert.equal(allowed.state.snapshot.value.id,'snapshot-a');assert.match(allowed.state.aiError.value,/模型不可用/)
})
test('verification retry reuses one idempotency key and an OCC conflict preserves the operator draft',async()=>{
 const requests=[]
 const {state}=component('components/governance/VerificationDialog.vue',{props:{modelValue:false,devices:[],configs:{},initial:{},roundId:''},write:async(_c,_id,_op,payload)=>{requests.push(payload);throw Object.assign(Error('old version'),{status:409})}})
 Object.assign(state.form,draft());state.observations.value=[observation]
 await state.save();await state.save()
 assert.equal(requests.length,2);assert.equal(requests[0].idempotencyKey,requests[1].idempotencyKey);assert.equal(state.form.fieldResult,'UNABLE_TO_DETERMINE');assert.match(state.error.value,/已保留表单/)
})

test('formal review uses the server snapshot identity and version vector without client metrics',async()=>{
 const writes=[],body={deviceId:'a',alarmType:'FIRE',originKind:'DEVICE_DIRECT',signalKey:'device:FIRE',ownerUserId:'operator',currentRoundId:'round-a',dataRevision:5,status:'OBSERVING'}
 const snapshot={id:'snapshot-a',factsHash:'server-facts-hash',statistics:{dataRevision:5,sourceRevisionVector:[{dependencyKey:'server-key',version:8}],comparison:{conclusion:'INSUFFICIENT_DATA',limitations:['NOT_ENOUGH_VERIFIED_MONITORING_HOURS']}}}
 const props=reactive({caseId:'case-a',devices:[],configs:{},runs:[{id:'run-a',snapshotId:'snapshot-a',parameters:{caseId:'case-a',planId:'plan-a'}}]})
 const read=async(collection,_id,operation)=>collection==='cases'&&!operation?{id:'case-a',version:4,body}:collection==='cases'&&operation==='rounds'?{items:[{id:'round-a',version:1,body:{status:'ACTIVE'}}]}:operation==='snapshot'?snapshot:{items:[]}
 const context=vm.createContext({computed,reactive,ref,watch,onBeforeUnmount(){},defineProps:()=>props,defineEmits:()=>()=>{},governanceRead:read,governanceWrite:async(...args)=>{writes.push(args);return{}},governanceUpload:async()=>({}),governanceAttachment(){},governanceExport(){},dutyAll:async()=>({items:[]}),can:()=>true,createClientId:()=> 'stable-review-key',recordBody,sameSignal,reportDifferences:()=>[],label:x=>x,time,tone:()=>'',conflictMessage})
 const state=vm.runInContext(`${setupScript(new URL('../src/components/governance/GovernanceCaseDetail.vue',import.meta.url))};({load,open,mutate,form,data})`,context)
 await state.load();state.data['observation-plans']=[{id:'plan-a',version:2,status:'CONFIRMED'}];state.open('observation-reviews');Object.assign(state.form,{planId:'plan-a',runId:'run-a',followup:'补充监测资料',followupOwnerUserId:'operator'});await new Promise(resolve=>setImmediate(resolve));await state.mutate()
 assert.equal(writes.length,1);assert.deepEqual(writes[0].slice(0,3),['rounds','round-a','observation-reviews']);const payload=writes[0][3]
 assert.equal(payload.roundId,'round-a');assert.equal(payload.factsHash,'server-facts-hash');assert.equal(payload.analysisSnapshotId,'snapshot-a');assert.equal(payload.dataRevision,5);assert.equal(payload.planVersion,2);assert.equal(payload.conclusion,'INSUFFICIENT_DATA');assert.deepEqual(Array.from(payload.limitations),snapshot.statistics.comparison.limitations);assert.equal(payload.metrics,undefined);assert.equal(payload.idempotencyKey,'stable-review-key')
})

test('fact references use the precise frozen identity endpoint and preserve facts when source access is revoked',async()=>{
 const id='observations:device/alarm',calls=[]
 const allowed=component('components/governance/GovernanceResults.vue',{props:{run:{id:'run-a',snapshotId:'snapshot-a',status:'PARTIAL'},devices:[]},read:async(_c,_id,op)=>{calls.push(op);return op.startsWith('facts/')?{id,snapshotId:'snapshot-a',summary:{description:'固定事件'}}:op==='snapshot'?{id:'snapshot-a'}:{items:[]}}})
 await allowed.state.load();await allowed.state.showFact(id);assert(calls.includes('facts/'+encodeURIComponent(id)));assert.equal(allowed.state.fact.value.id,id);assert.equal(allowed.state.factDialog.value,true)
 const denied=component('components/governance/GovernanceResults.vue',{props:{run:{id:'run-a',snapshotId:'snapshot-a',status:'PARTIAL'},devices:[]},read:async(_c,_id,op)=>{if(op.startsWith('facts/'))throw Object.assign(Error('来源授权已撤回'),{status:403});return op==='snapshot'?{id:'snapshot-a'}:{items:[]}}})
 await denied.state.load();await denied.state.showFact(id);assert.equal(denied.state.snapshot.value.id,'snapshot-a');assert.equal(denied.state.factDialog.value,false);assert.match(denied.state.aiError.value,/授权已撤回/)
})
