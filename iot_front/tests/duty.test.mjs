import assert from 'node:assert/strict'
import test from 'node:test'
import vm from 'node:vm'
import {readFileSync} from 'node:fs'
import {computed,reactive,ref,watch} from 'vue'
import {setupScript} from './helpers/vue.mjs'
import {monthDays,rosterCalendar,rosterPayload,itemPayload,handoverActionPayload,isAIRunning,dutyTime,dutyActionKey,clearDutyActionKey,dutyNoticeTarget} from '../src/duty/state.js'
import {createClientId} from '../src/clientId.js'

test('calendar handles leap month, Monday alignment and sorted overnight roster',()=>{
  assert.equal(monthDays('2024-02').filter(Boolean).length,29)
  assert.equal(monthDays('2026-09')[0],null)
  const rows=[{id:'late',startAt:new Date(2026,8,30,20).getTime()},{id:'early',startAt:new Date(2026,8,30,8).getTime()}]
  assert.deepEqual(rosterCalendar(rows,'2026-09').find(day=>day.date==='2026-09-30').items.map(row=>row.id),['early','late'])
})
test('real roster timestamps and accountable item transitions are required',()=>{
  assert.throws(()=>rosterPayload({stationId:'p',memberIds:['a'],leaderId:'a',startAt:30,endAt:20}),/结束时间/)
  assert.throws(()=>itemPayload({title:'故障',ownerId:'a',status:'DONE'}),/处理结果/)
  assert.throws(()=>itemPayload({title:'故障',ownerId:'a',status:'CANCELLED'}),/原因/)
  assert.equal(itemPayload({title:' 故障 ',ownerId:'a',status:'DONE',result:' 已核实 '}).result,'已核实')
})
test('signoff pins revision and fresh snapshot while AI stage has no estimated percentage',()=>{
  assert.deepEqual(handoverActionPayload({id:'h',version:4},{id:'r',revision:3,snapshotHash:'old'},{snapshotHash:'new'}),{version:4,revisionId:'r',revision:3,snapshotHash:'new'})
  assert.throws(()=>handoverActionPayload({id:'h'},null),/交接版本/)
  assert.equal(isAIRunning({status:'RUNNING'}),true);assert.equal(isAIRunning({status:'FAILED'}),false)
  assert.equal(dutyTime('bad'),'—')
})
test('uncertain signoff retries reuse a key across recreation without crossing identity or authorization',()=>{
  const values=new Map(),storage={getItem:key=>values.get(key),setItem:(key,value)=>values.set(key,value),removeItem:key=>values.delete(key)}
  const identity={tenant:'t',user:'a',accessVersion:'v1'}
  const key=dutyActionKey(storage,identity,'h','accept',3)
  assert.equal(dutyActionKey(storage,identity,'h','accept',3),key)
  assert.notEqual(dutyActionKey(storage,{...identity,user:'b'},'h','accept',3),key)
  assert.notEqual(dutyActionKey(storage,{...identity,accessVersion:'v2'},'h','accept',3),key)
  clearDutyActionKey(storage,identity,'h','accept',3)
  assert.notEqual(dutyActionKey(storage,identity,'h','accept',3),key)
})
test('near-handover reminders open the current shift instead of treating a roster ID as a handover ID',()=>{
  assert.deepEqual(dutyNoticeTarget({type:'HANDOVER_SOON',resourceId:'roster'}),{tab:'current',handoverId:''})
  assert.deepEqual(dutyNoticeTarget({type:'HANDOVER_PENDING',resourceId:'h'}),{tab:'handovers',handoverId:'h'})
  assert.deepEqual(dutyNoticeTarget({type:'ITEM_OVERDUE',resourceId:'i:2'}),{tab:'items',handoverId:''})
})

function deferred(){let resolve,reject;const promise=new Promise((yes,no)=>{resolve=yes;reject=no});return {promise,resolve,reject}}
function dutyComponent(file,overrides={},expose=''){
  const errors=[],notices=[],unmounts=[],props={catalog:{stations:[],users:[],devices:[],stationName:id=>id,userName:id=>id},runId:'',stationId:'',openId:''}
  const context=vm.createContext({computed,reactive,ref,watch,defineProps:()=>props,defineExpose:()=>{},defineEmits:()=>()=>{},onMounted:()=>{},onBeforeUnmount:fn=>unmounts.push(fn),can:()=>true,dutyTime,isAIRunning,itemPayload,handoverActionPayload,dutyActionKey,clearDutyActionKey,createClientId,session:{tenant:'t',user:'a'},dutyRead:async()=>({items:[]}),dutyWrite:async()=>({id:'h',version:1}),dutyDoc:value=>value,notifyError:error=>errors.push(error),UiMessage:{success:value=>notices.push(value)},setTimeout:()=>1,clearTimeout:()=>{},Date,Promise,console,...overrides})
  vm.runInContext(setupScript(new URL(`../src/components/duty/${file}.vue`,import.meta.url))+`;globalThis.testState={${expose}}`,context)
  return {state:context.testState,errors,notices,unmounts,props}
}
const handover={id:'h',version:2,currentRevisionId:'r',status:'DRAFT'}
const rev={id:'r',number:1,snapshot:{cutoffAt:20},snapshotHash:'facts',humanNotes:'人工联系情况'}
test('API documents unwrap without erasing the envelope of immutable business evidence',()=>{
  const source=readFileSync(new URL('../src/duty/api.js',import.meta.url),'utf8').replace(/^import .*$/gm,'').replace(/export /g,'')
  const context=vm.createContext({api:()=>{},download:()=>{},createClientId,URLSearchParams})
  vm.runInContext(source+';globalThis.normalize=dutyDoc',context)
  const event={id:'event',type:'duty.record.create',occurredAt:10,recordedAt:20,body:{content:'现场确认'}}
  const normalized=context.normalize({items:[event,{id:'record',kind:'record',version:3,body:{content:'处置记录'}}]})
  assert.equal(normalized.items[0].type,event.type);assert.equal(normalized.items[0].occurredAt,10);assert.equal(normalized.items[0].recordedAt,20);assert.equal(normalized.items[0].body.content,'现场确认')
  assert.equal(normalized.items[1].content,'处置记录');assert.equal(normalized.items[1].version,3)
})
test('outgoing handover detail ignores a late response after a different handover opens',async()=>{
  const first=deferred()
  const {state}=dutyComponent('DutyHandovers',{dutyRead:async(kind,id)=>kind==='handovers'?(id==='old'?first.promise:{...handover,id:'new'}):kind==='revisions'&&id?rev:{items:[]},can:()=>false},'open,close,detail')
  const old=state.open('old');await state.open('new');first.resolve({...handover,id:'old'});await old
  assert.equal(state.detail.value.id,'new')
})
test('a receiver without AI permission can inspect and sign a manual handover',async()=>{
  const reads=[],writes=[]
  const {state,errors}=dutyComponent('DutyHandovers',{can:permission=>permission!=='action:duty:ai',dutyRead:async(kind,id)=>{reads.push(kind);if(kind==='handovers'&&id)return {...handover,status:'SUBMITTED'};if(kind==='revisions'&&id)return rev;if(kind==='handovers')return {items:[]};return kind==='handovers'&&id?handover:{items:[],snapshotHash:'facts',changed:false}},dutyWrite:async(...args)=>{writes.push(args);return handover}},'open,prepareAction,completeAction,detail,revision,delta')
  await state.open('h');assert.ok(!reads.includes('ai-jobs'));state.prepareAction('accept');await state.completeAction()
  assert.equal(writes[0][2],'accept');assert.equal(writes[0][3].humanNotes,'人工联系情况');assert.equal(writes[0][3].revisionId,'r');assert.equal(errors.length,0)
})
test('a receiver without historical run permission still reads the frozen handover',async()=>{
  const reads=[]
  const {state,errors}=dutyComponent('DutyHandovers',{can:permission=>!Array.isArray(permission) && !['action:duty:ai','action:duty:history','action:duty:settings','action:duty:roster'].includes(permission),dutyRead:async(kind,id)=>{reads.push([kind,id]);if(kind==='handovers'&&id)return {...handover,status:'SUBMITTED',runId:'previous-run',nextRosterId:'next-roster',submission:{userId:'previous-leader'}};if(kind==='revisions'&&id)return rev;if(kind==='runs')throw Object.assign(Error('无历史权限'),{status:403});if(kind==='handovers'&&id===undefined)return {snapshotHash:'facts',changed:false};return {items:[]}}},'open,detail,revision')
  await state.open('h');assert.equal(state.revision.value.id,'r');assert.equal(errors.length,0);assert.ok(!reads.some(([kind])=>kind==='runs'))
})
test('handover signoff preserves one idempotency key after an uncertain network response',async()=>{
  const writes=[]
  const {state}=dutyComponent('DutyHandovers',{dutyWrite:async(...args)=>{writes.push(args);throw Error('network')}},'detail,revision,delta,prepareAction,completeAction')
  state.detail.value=handover;state.revision.value=rev;state.delta.value={snapshotHash:'facts',changed:false};state.prepareAction('submit');await state.completeAction();await state.completeAction()
  assert.equal(writes.length,2);assert.equal(writes[0][3].idempotencyKey,writes[1][3].idempotencyKey)
})
test('unchanged unfinished items do not masquerade as new facts requiring acknowledgement',()=>{
  const {state}=dutyComponent('DutyHandovers',{},'delta,deltaChanged')
  state.delta.value={changed:false,events:[],items:[{id:'follow-up'}]}
  assert.equal(state.deltaChanged.value,false)
  state.delta.value={changed:true,events:[],items:[{id:'follow-up'}]}
  assert.equal(state.deltaChanged.value,true)
})
test('item edit saves content before status transition using the returned version',async()=>{
  const writes=[]
  const {state}=dutyComponent('DutyItems',{dutyWrite:async(...args)=>{writes.push(args);return {id:'i',version:8}}},'form,dialog,original,save')
  Object.assign(state.form,{id:'i',version:7,title:'核实故障',ownerId:'a',status:'DONE',result:'已现场核实',nextAction:'完成核实'});state.original.value={status:'OPEN'};state.dialog.value='edit';await state.save()
  assert.equal(writes[0][4],'PUT');assert.equal(writes[1][2],'status');assert.equal(writes[1][3].version,8);assert.equal(writes[1][3].result,'已现场核实')
})
test('item creation retries preserve the command key after an uncertain response',async()=>{
  const writes=[]
  const {state}=dutyComponent('DutyItems',{dutyWrite:async(...args)=>{writes.push(args);throw Error('network')}},'form,edit,save')
  state.edit();Object.assign(state.form,{runId:'run',title:'待核实现场',ownerId:'a',nextAction:'联系维修'})
  await state.save();await state.save()
  assert.ok(writes[0][3].idempotencyKey);assert.equal(writes[1][3].idempotencyKey,writes[0][3].idempotencyKey)
})
