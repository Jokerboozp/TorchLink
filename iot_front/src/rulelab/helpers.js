import { recordBody, reviewStates, runIsActive, stateLabel, stateTone, timeMs } from '../quality/helpers.js'
import { durationLabel, monitoringTime } from '../monitoring/helpers.js'
export { recordBody, reviewStates, runIsActive, stateLabel, stateTone, timeMs, durationLabel }
export const ruleLabTime = monitoringTime
export const semantics = { 'processing-seconds-multistage-active-recovery-v1':'V1 · 处理时钟、逐阶段计时、仅 ACTIVE 自动恢复', 'processing-multistage-revisioned-retain-open-v2':'V2 · 新版本重置持续计时、产生版本恢复、保留活动告警' }
export const clockPolicies = { EVENT_AS_PROCESSING:'历史模拟：设备事件时间作处理时钟',RECEIVED_AS_PROCESSING:'历史模拟：平台接收时间作处理时钟',RECORDED_TRACE:'按实际逐规则、逐阶段 trace 时钟重现' }
export const initialPolicies = { EMPTY_UNKNOWN:'分别从空状态预热，起点初态未知',TRACE_INITIAL:'采用可核实 trace 初态，候选独立预热' }
export const labelConclusions = { CONFIRMED_EVENT:'已核实真实事件',TEST:'测试或演练',NO_ABNORMALITY_FOUND:'明确范围内未发现异常',UNKNOWN:'尚不确定' }
export const branchLabels = { BASELINE:'基线分支',CANDIDATE:'候选分支' }
export const categoryLabels = { RULE:'规则告警周期',DIRECT:'设备直接告警周期',COMPONENT:'部件告警周期',DEVICE_ASSERTION:'设备原始告警断言',rule:'规则告警周期',direct:'设备直接告警周期',component:'部件告警周期' }
export const differenceLabels = { BASELINE_ONLY:'基线独有',CANDIDATE_ONLY:'候选独有',COMMON:'共同事件' }
export const qualityLabels = { EXACT:'精确语义重现',HISTORICAL_SIMULATION:'历史模拟',UNKNOWN:'依据未知',KNOWN:'已核实',PARTIAL:'部分依据',NOT_EVALUABLE:'证据不足，不能评价检出',EVALUABLE:'共同区间可评价' }
export const findingKinds = { EVALUATION_ERROR:'规则求值错误',INSUFFICIENT_INPUT:'输入依据不足',UNMAPPED_MANUAL_ACTIONS:'人工动作未能映射',PENDING_AT_WINDOW_START:'主窗口起点仍处于持续计时',CLOCK_ASSUMPTION:'历史处理时钟假设',HOLDOUT_REUSE:'留出评价区间重复使用',BEHAVIOR_DIFFERENCE:'分支行为差异',UNRESOLVED_CYCLE:'周期尚未恢复' }
export const metricLabels = { inputCount:'固定输入',warmupInputCount:'预热消息',mainInputCount:'主窗口消息',matchedMessages:'匹配消息',newCycles:'新告警周期',repeatReports:'重复上报',unrecoveredCycles:'尚未恢复周期',retriggerAfterRecovery:'恢复后再触发',intentActions:'意向动作',deviceAssertions:'设备原始告警断言',ruleCycles:'规则告警周期',directCycles:'设备直接告警周期',componentCycles:'部件告警周期',evaluationErrors:'求值错误',unknownInitialCycles:'初态未知周期' }
export const ruleLabStage = value => ({preparing:'读取固定输入','inputs-frozen':'输入已固定','dataset-frozen':'数据集已固定',complete:'分支比较已完成',cancelled:'已停止','time-limit':'已到运行时限'}[value] || (/^calculating:/.test(value||'')?'正在独立计算分支':value||'等待执行'))
export const countLabel = value => value==null||!Number.isSafeInteger(Number(value))||Number(value)<0?'未知':Number(value).toLocaleString('zh-CN')
export function labelRatio(evaluation,field) {
 if(evaluation?.status!=='EVALUABLE')return '证据不足'
 const denominator=field==='precision'?evaluation.alarmCycles:evaluation.realEvents
 if(!(denominator>0))return '不适用'
 const value=evaluation[field]
 return value==null||!Number.isFinite(value)||value<0||value>1?'证据不足':`${(value*100).toFixed(1)}%`
}
export function datasetPayload(form,idempotencyKey) {
 const deviceIds=[...new Set(form.deviceIds||[])].sort(),start=timeMs(form.range?.[0]),end=timeMs(form.range?.[1]),warmupStart=timeMs(form.warmupStart)
 if(!deviceIds.length||!start||!end||end<=start||!warmupStart||warmupStart>start)throw new Error('请选择明确设备、有效主窗口与不晚于主窗口的预热起点')
 if(!['EVENT','RECEIVED'].includes(form.timeBasis)||!clockPolicies[form.clockPolicy]||!semantics[form.semanticsVersion]||!initialPolicies[form.initialStatePolicy])throw new Error('请明确排序、处理时钟、求值语义与初态政策')
 return{scope:form.scope||'personal',deviceIds,start,end,warmupStart,timeBasis:form.timeBasis,clockPolicy:form.clockPolicy,semanticsVersion:form.semanticsVersion,initialStatePolicy:form.initialStatePolicy,idempotencyKey}
}
export function candidateDraft(rule={}) {
 return{id:rule.id||'',productId:rule.productId||'',name:rule.name||'',description:rule.description||'',alarmType:rule.alarmType||'',level:rule.level||'',match:rule.match||'all',durationSeconds:rule.durationSeconds??0,expression:rule.expression||'',conditions:JSON.stringify(rule.conditions||[],null,2),recovery:JSON.stringify(rule.recovery||[],null,2),actions:JSON.stringify(rule.actions||[],null,2)}
}
export function candidatePayload(form) {
 const array=(text,label)=>{let value;try{value=JSON.parse(text||'[]')}catch{throw new Error(`${label}必须是有效 JSON`)}if(!Array.isArray(value))throw new Error(`${label}须为结构化数组`);return value}
 const conditions=array(form.conditions,'触发条件'),recovery=array(form.recovery,'恢复条件'),actions=array(form.actions,'意向动作')
 if(!form.id||!form.name.trim()||!form.alarmType||!form.level||!['all','any'].includes(form.match)||!Number.isSafeInteger(Number(form.durationSeconds))||form.durationSeconds<0)throw new Error('请填写候选名称、告警类型、等级、条件组合与非负整数持续秒数')
 if(!form.expression.trim()&&!conditions.length)throw new Error('请填写真实触发条件或规则表达式')
 return{id:form.id,productId:form.productId,name:form.name.trim(),description:form.description.trim(),alarmType:form.alarmType,level:form.level,match:form.match,durationSeconds:Number(form.durationSeconds),expression:form.expression.trim(),conditions,recovery,actions,enabled:false}
}
export function experimentPayload(form,dataset,revisions) {
 if(!dataset||!['SUCCEEDED','PARTIAL'].includes(dataset.status))throw new Error('请选择已经固定完成的数据集')
 const baselineRevisionIds=[...new Set(form.baselineRevisionIds||[])],selected=baselineRevisionIds.map(id=>revisions.find(row=>row.id===id))
 if(!baselineRevisionIds.length||selected.some(row=>!row)||new Set(selected.map(row=>row.ruleId)).size!==selected.length)throw new Error('基线每条规则须选择且仅选择一个不可变版本')
 const baseline=selected.find(row=>row.ruleId===form.candidateRuleId)
 if(!baseline||form.candidate.id!==baseline.ruleId||form.candidate.productId!==(baseline.rule.productId||''))throw new Error('候选只能替换基线中的同目标规则')
 if(!['FIXED_REVISIONS','RECORDED_ACTIVATIONS'].includes(form.baselinePolicy)||(form.baselinePolicy==='RECORDED_ACTIVATIONS'&&dataset.clockPolicy!=='RECORDED_TRACE'))throw new Error('实际生效历史只能用于 trace 时钟数据集')
 if(!form.hypothesis.trim()||!form.policyVersion.trim()||form.toleranceSeconds==null||!Number.isSafeInteger(Number(form.toleranceSeconds)*1000)||form.toleranceSeconds<0||!['EVENT','PROCESSING'].includes(form.matchTimeBasis)||!['TUNING','HOLDOUT'].includes(form.split))throw new Error('请固定行为假设、匹配策略版本、非负时间容差与调参/留出区间')
 if(form.representativeConfirmed&&!form.representativenessBasis.trim())throw new Error('代表性确认须填写正负区间及覆盖依据')
 return{resourceId:form.resourceId,expectedVersion:form.expectedVersion||0,scope:form.scope||'personal',deviceIds:dataset.deviceIds.slice(),body:{datasetId:dataset.id,baselineRevisionIds,baselinePolicy:form.baselinePolicy,candidateRuleId:form.candidateRuleId,candidate:candidatePayload(form.candidate),candidateEnabled:!!form.candidateEnabled,hypothesis:form.hypothesis.trim(),labelRevisionIds:[...new Set(form.labelRevisionIds||[])],evaluationPolicy:{version:form.policyVersion.trim(),toleranceMs:Number(form.toleranceSeconds)*1000,matchTimeBasis:form.matchTimeBasis,extraCyclePolicy:'COUNT_EACH_CYCLE',split:form.split,representativeConfirmed:!!form.representativeConfirmed,representativenessBasis:form.representativenessBasis.trim(),holdoutResourceId:form.holdoutResourceId||undefined}}}
}
export function inputSamples(inputs=[],deviceId='',property='',clock='event') {
 return inputs.filter(row=>(!deviceId||row.message?.deviceId===deviceId)&&typeof row.message?.properties?.[property]==='number'&&Number.isFinite(row.message.properties[property])&&timeMs(clock==='event'?row.message.timestamp:clock==='received'?row.receivedAt:row.availableAt)).map(row=>({...row,clockAt:clock==='event'?row.message.timestamp:clock==='received'?row.receivedAt:row.availableAt,value:row.message.properties[property]})).sort((a,b)=>a.clockAt-b.clockAt||String(a.id).localeCompare(String(b.id)))
}
export function inputChart(inputs,device,property,clock) {
 const rows=inputSamples(inputs,device,property,clock),times=[...new Set(rows.map(row=>row.clockAt))],groups=new Map()
 let collapsedCount=0
 for(const row of rows){const key=JSON.stringify([row.units?.[property]||'',row.protocolVersion||'',row.configurationVersion||'',row.pointTableVersion||'']);if(!groups.has(key))groups.set(key,new Map());const group=groups.get(key);if(group.has(row.clockAt))collapsedCount++;else group.set(row.clockAt,row)}
 const series=[...groups].map(([key,group])=>{const [unit,protocol,configuration,point]=JSON.parse(key);return{name:`${property} · ${unit||'单位未知'} · ${protocol||'协议未知'} · ${configuration||'配置未知'} · ${point||'点表未知'}`,values:times.map(t=>group.get(t)?.value??null)}})
 return{times,series,collapsedCount}
}
export function unwrapOutputs(items=[]){return items.map(row=>({...row.body,id:row.id,deviceId:row.deviceId||row.body?.deviceId||''}))}
export function publicationPayload(experiment,revisions,reason){
 const body=experiment?.body,baseline=revisions.find(row=>body?.baselineRevisionIds?.includes(row.id)&&row.ruleId===body.candidateRuleId)
 if(!body?.candidate||!baseline||body.candidate.id!==baseline.ruleId||!reason.trim())throw new Error('发布须引用已完成实验的固定候选、目标基线版本及明确原因')
 return{rule:{...body.candidate,enabled:!!body.candidateEnabled},expectedBaselineVersion:baseline.version,reason:reason.trim(),experimentId:experiment.id}
}
const reasons={RULE_INITIAL_STATE_UNKNOWN:'规则窗口起点状态未知',CANDIDATE_PENDING_RESTARTED_AT_WARMUP:'候选持续计时从预热起点重新开始',PREEXISTING_LIFECYCLE_RETAINED:'保留起点已有活动周期',RULE_STAGE_CLOCK_OR_STATE_UNAVAILABLE:'规则阶段时钟或状态不可用',EXPRESSION_EVALUATION_ERROR:'表达式求值错误',INVALID_COMPONENT_ASSERTION:'部件告警断言无效',COMPONENT_STAGE_CLOCK_MISSING:'缺少部件处理阶段时钟',COUNTERFACTUAL_DIRECT_RAISE_CLOCK_MISSING:'缺少设备直接告警的模拟触发时钟',DIRECT_RECOVERY_STAGE_CLOCK_MISSING:'缺少设备恢复阶段时钟',REPRESENTATIVE_CONFIRMED_POSITIVE_AND_NEGATIVE_INTERVALS_REQUIRED:'缺少已确认的代表性正负区间',SOURCE_INITIAL_STATE_OR_BRANCH_CALCULATION_UNKNOWN:'来源、初态或分支计算依据未知',HOLDOUT_WINDOW_REUSED_NOT_INDEPENDENT_VALIDATION:'留出区间已用于既往实验，不属于独立验证',RECALL_ONLY_FOR_FROZEN_COMMON_EVALUABLE_INTERVALS:'检出率仅适用于固定的共同可评价区间',UNMAPPED_MANUAL_ACTIONS_NOT_APPLIED_TO_COUNTERFACTUAL_ALARMS:'人工动作无法映射到模拟周期，未应用到分支',PUBLIC_OUTPUT_RECORD_CAP_REACHED:'结果明细达到资源上限，完整汇总保留',EVIDENCE_RECORD_CAP_REACHED:'代表证据达到资源上限，部分明细未展示',FROZEN_DATASET:'固定数据集原值保留',KNOWN_TRACE_INITIAL:'起点采用已核实 trace 状态',TRACE_COMPLETE:'已保留完整逐阶段 trace',EXACT_TRACE:'按真实 trace 重现',UNKNOWN_INITIAL:'起点初态未知',HISTORICAL_SIMULATION:'历史模拟',MISSING:'来源证据缺失',EXPIRED:'来源原件已过保留期'}
Object.assign(reasons,{COMPLETE_PRODUCTION_TRACE_OR_STAGE_CLOCKS_MISSING:'缺少完整生产求值记录或阶段时钟',EMPTY_INITIAL_BRANCH_STATE_IS_AN_ASSUMPTION:'采用空初态属于显式模拟假设',EXACT_TRACE_AVAILABLE:'完整实际 trace 可用',EXPLICIT_VIRTUAL_PROCESSING_CLOCK_NOT_PRODUCTION_REPRODUCTION:'采用明确虚拟处理时钟，不能视为生产重现',WINDOW_INITIAL_STATE_NOT_PROVED:'窗口起点初态未经证实',FIXED_REVISIONS_DO_NOT_REPRODUCE_RECORDED_ACTIVATION_HISTORY:'固定规则版本不重现实际生效切换历史',HISTORICAL_SIMULATION_EXPLICIT_SORT_AND_CLOCK_POLICY:'按明确排序与虚拟时钟进行历史模拟',TRACE_COMMIT_DIFFERS_FROM_PURE_EVALUATION:'trace 提交结果与纯求值结果不同',TRACE_ROUTING_COMMIT_DIFFERS_FROM_PURE_EVALUATION:'路由提交记录与纯求值结果不同',HISTORICAL_SIMULATION_TRACE_COMMIT_DIFFERENCE:'模拟分支与实际 trace 提交存在差异',TRIGGER_EVENT_BEFORE_DATASET_UNKNOWN:'数据集之前的触发事件时间未知',SOURCE_READ_FAILED:'读取来源失败',SOURCE_RECORD_MISSING:'来源记录缺失',NO_SOURCE_COVERAGE:'缺少来源覆盖',MANUAL_ACTION_SOURCE_UNAVAILABLE:'人工动作来源不可用',PRODUCTION_TRACE_SOURCE_UNAVAILABLE:'生产求值记录来源不可用',TRACE_HAS_UNAUTHORIZED_OR_UNAVAILABLE_REFERENCES:'trace 引用包含未授权或不可用来源',INDEPENDENT_SOURCE_READ_CUTOFF:'各来源保留独立读取截止时点',DATASET_BYTE_CAP_REACHED:'固定输入达到字节上限',DATASET_FREEZE_BYTE_CAP_REACHED:'数据集固定达到字节上限',DATASET_RECORD_OR_BYTE_CAP_REACHED:'数据集达到记录或字节上限',MANUAL_ACTION_READ_CAP_REACHED:'人工动作读取达到上限',TRACE_ATTEMPT_READ_CAP_REACHED:'求值尝试读取达到上限',TRACE_WORK_RECORD_CAP_REACHED:'trace 工作项达到上限',BRANCH_STATE_CAP_REACHED:'分支状态达到资源上限',RUN_TIME_BUDGET_EXHAUSTED:'运行达到时间预算',RUN_TIME_BUDGET_EXHAUSTED_BEFORE_DATASET_FREEZE:'固定输入完成前已达时间预算'})
export const reasonText=value=>String(value||'').split(';').map(part=>reasons[part.trim()]||part.trim()).join('；')
