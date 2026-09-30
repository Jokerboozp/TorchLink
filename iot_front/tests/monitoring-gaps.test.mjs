import assert from 'node:assert/strict'
import test from 'node:test'
import { durationLabel, hypothesisPayload, metricRatio, metricRows, monitoringEvidenceRows, monitoringTime, overallMetrics, profileDraft, profilePayload, timelineLanes, unwrapOutputs } from '../src/monitoring/helpers.js'
const start = Date.parse('2026-10-01T00:00:00Z')
const draft = () => ({ ...profileDraft(), deviceIds: ['b','a','a'], effectiveFrom: start, periodSeconds: 10, toleranceSeconds: 2, messageTypes: ['PROPERTY_REPORT'], attributes: [{id:'pressure',valueType:'number',minimum:0,maximum:10}] })

test('unknown observation time cannot become complete-window availability', () => {
 const m = { availableMs:1000,unavailableMs:1000,unknownMs:2000,plannedMs:4000,notApplicableMs:0,knownAvailability:.5,knownCoverage:.5,fullWindowAvailability:1 }
 assert.equal(metricRatio(m,'knownAvailability'),'50.0%')
 assert.equal(metricRatio(m,'knownCoverage'),'50.0%')
 assert.equal(metricRatio(m,'fullWindowAvailability'),'存在未知区间，不能给出')
 assert.equal(metricRatio({...m,unknownMs:0,plannedMs:2000,fullWindowAvailability:.5},'fullWindowAvailability'),'50.0%')
 assert.equal(metricRatio({...m,knownAvailability:null},'knownAvailability'),'证据不足')
})
test('event and excluded windows never display invented periodic availability', () => {
 const event={plannedMs:10000,availableMs:0,unavailableMs:0,unknownMs:0,notApplicableMs:10000,knownAvailability:1,knownCoverage:1,fullWindowAvailability:1}
 for(const field of ['knownAvailability','knownCoverage','fullWindowAvailability'])assert.equal(metricRatio(event,field),'不适用')
 assert.equal(metricRatio({...event,plannedMs:0},'fullWindowAvailability'),'不适用')
 assert.equal(metricRatio({plannedMs:10,availableMs:0,unavailableMs:0,unknownMs:10,knownCoverage:0},'knownAvailability'),'证据不足')
})
test('overall and strategy or attribute metrics stay distinct without duplicate totals', () => {
 const outputs=[{id:'doc',deviceId:'a',body:{intervalCount:120,representedIntervalCount:100,metrics:[{id:'overall',deviceId:'a',track:'data',profileId:'',attributeId:'',unknownMs:20},{id:'policy',deviceId:'a',track:'data',profileId:'p',attributeId:'',unknownMs:20},{id:'attribute',deviceId:'a',track:'attribute',profileId:'p',attributeId:'pressure',unknownMs:20}]}}]
 const rows=metricRows(outputs)
 assert.deepEqual(overallMetrics(rows).map(row=>row.id),['overall'])
 assert.equal(rows[0].outputId,'doc')
 assert.equal(rows[0].intervalCount,120)
 assert.equal(rows[0].representedIntervalCount,100)
 assert.equal(unwrapOutputs([{id:'outer',body:{id:'inner',state:'UNKNOWN'}}])[0].id,'outer')
})
test('time lanes clip actual half-open intervals and keep blank coverage blank', () => {
 const rows=[{id:'second',deviceId:'a',track:'data',start:start+4000,end:start+8000,state:'UNKNOWN'},{id:'first',deviceId:'a',track:'data',start:start-1000,end:start+2000,state:'AVAILABLE'},{id:'attr',deviceId:'a',attributeId:'pressure',track:'attribute',start,end:start+1000,state:'UNAVAILABLE'},{id:'after',deviceId:'a',track:'data',start:start+10000,end:start+11000,state:'UNAVAILABLE'}]
 const before=structuredClone(rows),lanes=timelineLanes(rows,{start,end:start+10000})
 assert.equal(lanes.length,1)
 assert.deepEqual(lanes[0].items.map(row=>[row.id,row.left,row.width]),[['first',0,20],['second',40,40]])
 assert.equal(lanes[0].items.length,2,'missing region was not filled by invented state')
 assert.equal(timelineLanes(rows,{start,end:start+10000},true)[0].attributeId,'pressure')
 assert.deepEqual(rows,before)
})
test('monitoring profiles have no guessed cadence, importance or critical fields', () => {
 const value=profileDraft()
 assert.equal(value.periodSeconds,null);assert.equal(value.toleranceSeconds,null);assert.equal(value.importance,'');assert.equal(value.attributes[0].id,'');assert.deepEqual(value.messageTypes,[])
 assert.throws(()=>profilePayload(value),/明确设备/)
})
test('explicit profile durations use Unixms and preserve declared ALL or ANY policy', () => {
 const form=draft(),before=structuredClone(form),value=profilePayload(form)
 assert.deepEqual(value.deviceIds,['a','b']);assert.equal(value.body.periodMs,10000);assert.equal(value.body.toleranceMs,2000);assert.equal(value.body.effectiveFrom,start);assert.equal(value.body.merge,'ALL');assert.deepEqual(value.body.attributes,[{id:'pressure',valueType:'number',minimum:0,maximum:10}]);assert.deepEqual(form,before)
 assert.equal(profilePayload({...form,merge:'ANY',mode:'event',periodSeconds:null,toleranceSeconds:null}).body.periodMs,0)
 assert.equal(profilePayload({...form,mode:'event'}).body.toleranceMs,0)
})
test('profile validation rejects nonfinite timing, duplicate fields and undeclared message types', () => {
 const form=draft()
 for(const periodSeconds of [0,-1,Infinity,.0001])assert.throws(()=>profilePayload({...form,periodSeconds}),/预期周期/)
 assert.throws(()=>profilePayload({...form,messageTypes:['HEARTBEAT_UNKNOWN']}),/报文类型/)
 assert.throws(()=>profilePayload({...form,attributes:[...form.attributes,...form.attributes]}),/不能重复/)
 assert.throws(()=>profilePayload({...form,attributes:[{id:'x',valueType:'number',minimum:NaN}]}),/有限值/)
 assert.throws(()=>profilePayload({...form,attributes:[{id:'x',valueType:'number',minimum:10,maximum:1}]}),/下限/)
 assert.throws(()=>profilePayload({...form,targetType:'PRODUCT',productId:''}),/配置目标/)
 assert.throws(()=>profilePayload({...form,frequentGapCount:1.5}),/正整数/)
})
test('hypotheses bind the exact authorized snapshot without inventing missing members', () => {
 const current={id:'g',historyQuality:'CURRENT_ONLY',start,memberIds:['a','b']}
 assert.deepEqual(hypothesisPayload(current,start,3,'key'),{groupId:'g',at:start,expectedRunVersion:3,idempotencyKey:'key'})
 assert.throws(()=>hypothesisPayload(current,start-1,3,'key'),/快照范围/)
 const historical={...current,historyQuality:'KNOWN',end:start+1000}
 assert.equal(hypothesisPayload(historical,start+999,3,'key').at,start+999)
 assert.throws(()=>hypothesisPayload(historical,start+1000,3,'key'),/快照范围/)
 assert.throws(()=>hypothesisPayload({...current,memberIds:[]},start,3,'key'),/快照依据/)
 assert.deepEqual(current.memberIds,['a','b'])
})
test('durations remain factual, precise to milliseconds and reject absent clocks', () => {
 assert.equal(durationLabel(1234),'1.234 秒');assert.equal(durationLabel(0),'0 秒');assert.equal(durationLabel(60000),'1 分钟');assert.equal(durationLabel(-1),'未知');assert.equal(durationLabel(Infinity),'未知');assert.equal(durationLabel(null),'未知');assert.equal(durationLabel(undefined),'未知')
})
test('frozen nested evidence exposes original clocks and preserves document authority', () => {
 const source={id:'doc',deviceId:'a',sourceKind:'standard_measurement',sourceId:'standard',originalAvailability:'AVAILABLE_AT_FREEZE',summary:{measurement:{id:'sample',deviceId:'other',property:'pressure',eventAt:start,receivedAt:start+20,availableAt:start+40000,value:101},valueCopied:false}}
 const before=structuredClone(source),row=monitoringEvidenceRows([source])[0]
 assert.equal(row.id,'doc');assert.equal(row.deviceId,'a');assert.equal(row.sampleId,'sample');assert.equal(row.attributeId,'pressure')
 assert.deepEqual([row.eventAt,row.receivedAt,row.availableAt],[start,start+20,start+40000]);assert.equal(row.availability,'available_at_freeze');assert.equal(row.summary.valueCopied,false)
 assert.ok(monitoringTime(row.receivedAt).endsWith('.020'),'subsecond reception remains distinguishable from event time')
 assert.deepEqual(source,before)
})
