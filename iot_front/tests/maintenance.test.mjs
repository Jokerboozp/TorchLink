import assert from 'node:assert/strict'
import test from 'node:test'
import vm from 'node:vm'
import { computed,effectScope,nextTick,reactive,ref,watch } from 'vue'
import { setupScript } from './helpers/vue.mjs'
import { deviceChoices } from '../src/deviceCatalog.js'
import * as maintenanceHelpers from '../src/maintenance/helpers.js'
import { assetDraft,assetPayload,eligibleObservations,decimal,evidencePayload,metricCells,observationPayload,orderedAdjustment,scenarioPayload,verificationPayload,workPayload } from '../src/maintenance/helpers.js'
const t=Date.parse('2026-10-01T00:00:00Z'),a={id:'asset-v1',resourceId:'physical-old',version:1,deviceIds:['d1'],body:{deviceId:'d1',componentId:'',physicalId:'plate-1',name:'旧实物',boundaryStatus:'CONFIRMED',effectiveStart:t-10000,effectiveEnd:t+5000}},b={...a,id:'asset-v2',resourceId:'physical-new',body:{...a.body,physicalId:'plate-2',effectiveStart:t+5000,effectiveEnd:undefined}},work={resourceId:'repair',id:'work-v3',version:3,deviceIds:['d1'],body:{status:'COMPLETED',startedAt:t+4000,endedAt:t+6000}},human={kind:'HUMAN_CONFIRMATION',deviceId:'d1',description:'实际铭牌照片及现场核实',sourceId:'spoof',recordedAt:t,recorder:'spoof'}
test('actual asset dates remain unknown instead of platform registration date; confirmed boundary requires real evidence',()=>{const form=assetDraft({...a,body:{...a.body,createdAt:t}});assert.equal(form.manufacturedAt,null);assert.equal(form.commissionedAt,null);Object.assign(form,{importance:4,evidence:[human]});const original=structuredClone(form),p=assetPayload(form,'key');assert.ok(!('commissionedAt'in p.body));assert.ok(!('createdAt'in p.body));assert.deepEqual(p.deviceIds,['d1']);assert.deepEqual(form,original);assert.throws(()=>assetPayload({...form,evidence:[]},'key'),/实际依据/);assert.throws(()=>assetPayload({...form,importance:null},'key'),/人工重要性/);assert.throws(()=>assetPayload({...form,effectiveEnd:form.effectiveStart},'key'),/起点/)})
test('evidence binds actual scope, strips source/recorder authority from manual confirmations',()=>{assert.deepEqual(evidencePayload([human],['d1']),[{kind:'HUMAN_CONFIRMATION',sourceId:'',deviceId:'d1',description:human.description}]);assert.throws(()=>evidencePayload([{...human,deviceId:'hidden'}],['d1']),/范围/);assert.throws(()=>evidencePayload([{...human,kind:'ATTACHMENT',sourceId:''}],['d1']),/记录/);assert.throws(()=>evidencePayload([{...human,description:''}],['d1']),/依据/)})
test('work body is only a draft with immutable source, actual people remain external names and no fabricated completion',()=>{const f={name:' 实际修复 ',assetRevisionId:a.id,type:'REPAIR',reason:'现场确认故障',actions:'断电检查\n更换端子',people:'外部维修人员张某',parts:'端子1个',status:'COMPLETED',endedAt:t};const v=workPayload(f,[a],'key');assert.equal(v.body.status,'DRAFT');assert.deepEqual(v.body.people,['外部维修人员张某']);assert.deepEqual(v.body.actions,['断电检查','更换端子']);assert.ok(!('endedAt'in v.body));assert.equal(v.body.assetRevisionId,a.id);assert.throws(()=>workPayload({...f,assetRevisionId:'missing'},[a],'key'),/固定实物/)})
test('functional verification is independent, passing requires every actual item and evidence, preserves work completion',()=>{const form={requiredItems:'报警输入\n恢复输出',checkedItems:['报警输入','恢复输出'],result:'PASSED',explanation:'逐项现场核验',evidence:[human]},original=structuredClone(work);const p=verificationPayload(form,work,'key');assert.equal(p.expectedVersion,3);assert.equal(p.verification.result,'PASSED');assert.ok(!('status'in p));assert.deepEqual(work,original);assert.throws(()=>verificationPayload({...form,checkedItems:['报警输入']},work,'key'),/逐项/);assert.throws(()=>verificationPayload({...form,evidence:[]},work,'key'),/证据/);assert.throws(()=>verificationPayload({...form,checkedItems:['伪造项']},work,'key'),/规定/);assert.equal(verificationPayload({...form,result:'FAILED',checkedItems:[],evidence:[]},work,'key').verification.result,'FAILED')})
test('same-instance observation binds fixed assets, actual work clocks, no caller admission injection',()=>{const form={comparisonType:'SAME_INSTANCE_REPAIR',beforeAssetRevisionId:a.id,afterAssetRevisionId:a.id,before:[t,t+4000],after:[t+6000,t+10000],admissionRevisionId:'admission-v2',admission:{version:'fake'},useFinance:false};const p=observationPayload(form,work,[a],'key');assert.equal(p.parameters.beforeAssetRevisionId,a.id);assert.equal(p.parameters.admissionRevisionId,'admission-v2');assert.ok(!('admission'in p.parameters));assert.equal(p.useFinance,false);assert.throws(()=>observationPayload({...form,before:[t,t+5000]},work,[a],'key'),/实际维修/);assert.throws(()=>observationPayload({...form,after:[t+5000,t+8000]},work,[a],'key'),/结束后/);assert.throws(()=>observationPayload(form,{...work,body:{...work.body,status:'IN_PROGRESS'}},[a],'key'),/已结束/)})
test('replacement retaining platform ID must bind distinct physical IDs and confirmed common switch boundary',()=>{const form={comparisonType:'CROSS_INSTANCE_REPLACEMENT',beforeAssetRevisionId:a.id,afterAssetRevisionId:b.id,before:[t,t+4000],after:[t+6000,t+9000],switchAt:t+5000};const p=observationPayload(form,work,[a,b],'key');assert.equal(p.parameters.beforeAssetRevisionId,a.id);assert.equal(p.parameters.afterAssetRevisionId,b.id);assert.throws(()=>observationPayload({...form,switchAt:t+4000},work,[a,b],'key'),/切换/);assert.throws(()=>observationPayload(form,work,[a,{...b,body:{...b.body,boundaryStatus:'UNKNOWN'}}],'key'),/确认/);assert.throws(()=>observationPayload({...form,comparisonType:'SAME_INSTANCE_REPAIR'},work,[a,b],'key'),/同一实物/);assert.throws(()=>observationPayload(form,work,[a,{...b,body:{...b.body,componentId:'different'}}],'key'),/同设备/)})
test('hand-calculated unknown state denominator 9 unknown + 1 offline yields 100% known offline and 10% coverage',()=>{const side={windowMs:36000000,assetWindowMs:36000000,effectiveMs:36000000,excludedMs:0,onlineMs:0,offlineMs:3600000,stateUnknownMs:32400000,knownOfflineRatio:1,stateCoverage:.1,fullWindowOfflineRatio:.1,observationCoverage:1,confirmedFaultCycles:0,faultSourceKnownMs:0,faultSourceUnknownMs:36000000};const cells=new Map(metricCells(side).map(c=>[c.label,c.value]));assert.equal(cells.get('已知区间离线比例'),'100.0%');assert.equal(cells.get('状态覆盖率'),'10.0%');assert.equal(cells.get('完整窗口离线比例'),'未知');assert.equal(cells.get('故障来源覆盖'),'未知');assert.equal(cells.get('确认故障率（次/1000小时）'),'未知');assert.equal(cells.get('有效观察'),'10.0000 小时');const empty=new Map(metricCells({...side,effectiveMs:0,onlineMs:0,offlineMs:0}).map(c=>[c.label,c.value]));assert.equal(empty.get('确认故障率（次/1000小时）'),'不适用');assert.equal(empty.get('已知区间离线比例'),'不适用')})
test('exact decimal money preserves large values/zeros, unknown is null and no float/scientific/multi currency assumption',()=>{assert.equal(decimal('999999999999999999999999.123456'),'999999999999999999999999.123456');assert.equal(decimal('0.000000'),'0.000000');assert.equal(decimal(''),null);assert.equal(decimal(null),null);for(const v of ['1e6','-1','1.1234567','01','1234567890123456789012345'])assert.throws(()=>decimal(v),/十进制/)})
test('transparent candidate submits only fixed sources and manual required tier, finance-off explicitly strips budget and quote',()=>{const candidate={id:'c1',assetRevisionId:a.id,action:'REPAIR',requiredTier:1,tierBasis:'单位已确认必需事项',constraints:'停运窗口\n现场人员',quoteRevisionId:'restricted-money',assetId:'caller-spoof',importance:9,confirmedFaultRate:0};const form={name:'投入清单',planning:[t,t+10000],currency:'CNY',budget:'1.000001',useFinance:false,candidates:[candidate]};const v=scenarioPayload(form,[a],'key');assert.equal(v.body.budget,null);assert.equal(v.body.candidates[0].quoteRevisionId,'');assert.ok(!('importance'in v.body.candidates[0]));assert.ok(!('confirmedFaultRate'in v.body.candidates[0]));assert.ok(!('assetId'in v.body.candidates[0]));assert.deepEqual(v.body.candidates[0].constraints,['停运窗口','现场人员']);assert.equal(v.body.status,'DRAFT');assert.equal(v.body.policyVersion,'required-tier-transparent-v1');assert.equal(scenarioPayload({...form,useFinance:true},[a],'key').body.budget,'1.000001');assert.throws(()=>scenarioPayload({...form,candidates:[candidate,candidate]},[a],'key'),/唯一/);assert.throws(()=>scenarioPayload({...form,candidates:[{...candidate,requiredTier:null}]},[a],'key'),/人工必需/)})
test('manual adjustment is a complete permutation without dropping missing data candidates',()=>{const candidates=[{id:'known'},{id:'unknown'}];assert.deepEqual(orderedAdjustment(['unknown','known'],candidates),['unknown','known']);assert.throws(()=>orderedAdjustment(['known'],candidates),/每个/);assert.throws(()=>orderedAdjustment(['known','known'],candidates),/一次/);assert.throws(()=>orderedAdjustment(['known','fake'],candidates),/每个/)})

test('observation choices resolve immutable old physical revisions and never broaden to another instance on the same device',()=>{const old={...a,id:'old-revision'},latest={...a,id:'latest-revision'},runs=[{id:'same',status:'SUCCEEDED',parameters:{observation:{afterAssetRevisionId:old.id}}},{id:'other',status:'SUCCEEDED',parameters:{observation:{afterAssetRevisionId:b.id}}},{id:'missing',status:'SUCCEEDED',parameters:{observation:{afterAssetRevisionId:'expired'}}},{id:'working',status:'RUNNING',parameters:{observation:{afterAssetRevisionId:old.id}}}];assert.deepEqual(eligibleObservations(runs,latest,[old,latest,b]).map(r=>r.id),['same']);assert.deepEqual(eligibleObservations(runs,null,[a]),[])})

function deferred(){let resolve,reject;const promise=new Promise((yes,no)=>{resolve=yes;reject=no});return{promise,resolve,reject}}
const oldAsset={...a,id:'fixed-v1',resourceId:'same-physical',body:{...a.body,name:'维修当时的实物'}},latestAsset={...oldAsset,id:'current-v2',version:2,body:{...oldAsset.body,name:'当前实物资料'}}
const draftWork={id:'fixed-work',resourceId:'work',version:1,deviceIds:['d1'],body:{name:'原维修草稿',status:'DRAFT',assetRevisionId:oldAsset.id,type:'REPAIR',reason:'实际端子故障'}}
const draftScenario={id:'fixed-scenario',resourceId:'scenario',version:1,deviceIds:['d1'],body:{name:'原投入方案',status:'DRAFT',planningStart:t,planningEnd:t+10000,currency:'CNY',useFinance:false,candidates:[{id:'c1',assetRevisionId:oldAsset.id,action:'INSPECT',requiredTier:1,tierBasis:'已确认需现场检查'}]}}
function maintenanceComponent(file,{props={},read=async()=>oldAsset,write=async()=>draftWork}={}){
 const unmounts=[],events=[],scope=effectScope(),p=reactive({items:[],assets:[latestAsset],devices:[{id:'d1'}],records:[],costs:[],observations:[],deviceId:'',assetId:'',correctives:[],...props})
 const context=vm.createContext({computed,reactive,ref,watch,deviceChoices,defineProps:()=>p,defineEmits:()=>((...args)=>events.push(args)),onBeforeUnmount:fn=>unmounts.push(fn),can:()=>false,createClientId:()=> 'request-key',...maintenanceHelpers,URLSearchParams,apiAll:async()=>({items:[]}),dutyAll:async()=>({items:[]}),maintenanceAll:async()=>({items:[]}),maintenanceWrite:write,maintenanceRevision:read})
 const state=scope.run(()=>vm.runInContext(setupScript(new URL(`../src/components/maintenance/${file}.vue`,import.meta.url))+`;({edit,save,form,error,dialog,rows,assetName,assetOptions,historicalAssets,formLoading,busy${file==='MaintenanceInvestments'?',observationOptions,quoteOptions,defectOptions':',evDevices'}})`,context))
 return{state,props:p,events,unmount:()=>{for(const fn of unmounts)fn();scope.stop()}}
}
async function settle(){await nextTick();await new Promise(resolve=>setImmediate(resolve));await nextTick()}
test('fixed maintenance evidence preserves resource device IDs before device metadata arrives',async()=>{
 for(const devices of [[],[{id:'unrelated'}]]){
  const c=maintenanceComponent('MaintenanceRecords',{props:{items:[draftWork],devices}});await settle();await c.state.edit(draftWork);await settle()
  assert.deepEqual(Array.from(c.state.evDevices.value,row=>row.id),['d1']);c.unmount()
 }
})

test('revising an asset preserves old repair rows, fixed labels and draft edits without rebinding to the latest revision',async()=>{
 const writes=[],component=maintenanceComponent('MaintenanceRecords',{props:{items:[draftWork],assetId:oldAsset.resourceId},write:async(...args)=>{writes.push(args);return draftWork}})
 await settle()
 assert.equal(component.state.rows.value.length,1,'same physical asset must keep records pinned to v1 after v2 is published')
 await component.state.edit(draftWork);component.state.form.name='仅修改维修名称';await component.state.save()
 assert.equal(component.state.error.value,'');assert.equal(writes.length,1);assert.equal(writes[0][3].body.assetRevisionId,oldAsset.id);assert.equal(component.state.assetName(oldAsset.id),oldAsset.body.name)
 assert.ok(component.state.assetOptions.value.some(row=>row.id===oldAsset.id),'the selected fixed revision must have a real option label')
 component.unmount()
})
test('scenario editing resolves fixed v1 before validation and keeps historical observation and quote choices',async()=>{
 const writes=[],observation={id:'observation',deviceIds:['d1'],status:'SUCCEEDED',parameters:{observation:{afterAssetRevisionId:oldAsset.id}}},cost={id:'quote',body:{sourceKind:'ASSET_INSTANCE',sourceId:oldAsset.resourceId,type:'ESTIMATE',currency:'CNY',planningStart:t,planningEnd:t+10000}}
 const otherWork={...draftWork,id:'another-work',body:{...draftWork.body,assetRevisionId:b.id}},component=maintenanceComponent('MaintenanceInvestments',{props:{items:[draftScenario],assets:[latestAsset,b],records:[draftWork,otherWork],observations:[observation],costs:[cost]},write:async(...args)=>{writes.push(args);return draftScenario}})
 await component.state.edit(draftScenario);component.state.form.name='仅修改方案名称';await component.state.save()
 assert.equal(component.state.error.value,'');assert.equal(writes.length,1);assert.equal(writes[0][3].body.candidates[0].assetRevisionId,oldAsset.id)
 const candidate=component.state.form.candidates[0]
 assert.equal(component.state.assetName(oldAsset.id),oldAsset.body.name);assert.deepEqual(component.state.observationOptions(candidate).map(r=>r.id),['observation']);assert.deepEqual(component.state.quoteOptions(candidate).map(r=>r.id),['quote']);assert.deepEqual(component.state.defectOptions(candidate).map(r=>r.id),[draftWork.id]);assert.ok(component.state.assetOptions(candidate).some(row=>row.id===oldAsset.id))
 component.unmount()
})
test('unavailable fixed history blocks submission with an explicit error and retry preserves the original revision',async()=>{
 for(const file of ['MaintenanceRecords','MaintenanceInvestments']){
  let failed=true;const writes=[],row=file==='MaintenanceRecords'?draftWork:draftScenario,component=maintenanceComponent(file,{props:{items:[row]},read:async()=>{if(failed)throw Error('历史读取暂不可用');return oldAsset},write:async(...args)=>{writes.push(args);return row}})
  await component.state.edit(row);await component.state.save()
  assert.match(component.state.error.value,/历史实物/);assert.equal(writes.length,0);assert.ok(component.state.dialog.value)
  failed=false;await component.state.save();assert.equal(component.state.error.value,'');assert.equal(writes.length,1)
  const body=writes[0][3].body;assert.equal(file==='MaintenanceRecords'?body.assetRevisionId:body.candidates[0].assetRevisionId,oldAsset.id)
  component.unmount()
 }
})
test('late fixed history success or failure cannot replace a new editor, a closed dialog or an unmounted component',async()=>{
 for(const file of ['MaintenanceRecords','MaintenanceInvestments'])for(const action of ['replace','close','unmount'])for(const outcome of ['success','failure']){
  const request=deferred(),row=file==='MaintenanceRecords'?draftWork:draftScenario,component=maintenanceComponent(file,{read:()=>request.promise}),running=component.state.edit(row)
  if(action==='replace'){const current=structuredClone(row);current.body.name='最后选择';if(file==='MaintenanceRecords')current.body.assetRevisionId=latestAsset.id;else current.body.candidates[0].assetRevisionId=latestAsset.id;await component.state.edit(current)}
  else if(action==='close')component.state.dialog.value=file==='MaintenanceRecords'?'':false
  else component.unmount()
  await nextTick();if(outcome==='success')request.resolve(oldAsset);else request.reject(Error('过期历史读取失败'));await running;await settle()
  assert.equal(component.state.error.value,'',`${file} ${action} must ignore stale history failures`)
  assert.equal(component.state.historicalAssets.value.length,0,'an old editor response must not mutate current asset choices')
  if(action==='replace')assert.equal(component.state.form.name,'最后选择')
  if(action!=='unmount')component.unmount()
 }
})
test('late saving responses cannot close a newer editor or emit selection for its previous record',async()=>{
 for(const file of ['MaintenanceRecords','MaintenanceInvestments']){
  const request=deferred(),row=file==='MaintenanceRecords'?draftWork:draftScenario,component=maintenanceComponent(file,{write:()=>request.promise})
  await component.state.edit(row);const running=component.state.save();await settle()
  const current=structuredClone(row);current.body.name='最后选择';await component.state.edit(current)
  request.resolve(row);await running
  assert.ok(component.state.dialog.value);assert.equal(component.state.form.name,'最后选择');assert.equal(component.state.error.value,'');assert.equal(component.events.length,0);assert.equal(component.state.busy.value,false)
  component.unmount()
 }
})
