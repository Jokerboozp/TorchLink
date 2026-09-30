export const recordBody = row => row ? { ...(row.body || row), id: row.id, version: row.version, createdAt: row.createdAt, updatedAt: row.updatedAt } : null
export const caseStates = { PENDING_INVESTIGATION:'待核查', INVESTIGATING:'核查中', IMPROVING:'改进中', OBSERVING:'观察中', COMPLETED:'本轮完成', CANCELLED:'已取消' }
export const labels = { ...caseStates, MEASURE_OVERDUE:'超期措施', ACCEPTANCE_PENDING:'待必要验收', OBSERVATION_INSUFFICIENT:'观察资料不足', POST_COMPLETION_REPORT:'完成后新事件待复核', UNREAD:'未读', READ:'已读', HANDLED:'已处理', ACTIVE:'进行中', DRAFT:'草稿', PUBLISHED:'已发布', RETIRED:'已退休', ON_SITE:'现场核实', PHONE:'电话核实', DOCUMENT_REVIEW:'资料核查', NOT_VERIFIED:'未核实', UNVERIFIED:'未核实 / 时间未验证', FIRE_OBSERVED:'发现火情', OTHER_ABNORMALITY_OBSERVED:'其他异常', NO_FIRE_OBSERVED:'未发现火情', UNABLE_TO_DETERMINE:'无法确定', TARGET_ABNORMALITY_OBSERVED:'发现目标异常', NO_TARGET_ABNORMALITY_OBSERVED:'未发现目标异常', UNKNOWN:'未知', SUSPECTED:'疑似相关', RELATED:'存在相关线索', CONFIRMED_RELATED:'已人工确认相关', CONFIRMED_UNRELATED:'已人工确认不相关', NOT_RELATED:'已核实不相关', UNRELATED:'已核实不相关', NORMAL:'已核实运行', ABNORMAL:'发现异常', FULL_DECLARED:'声明完整登记', PARTIAL:'部分登记 / 覆盖', ALARM_ONLY:'仅报警时登记', TRUSTED:'时间已核实', UNVERIFIED:'时间未核实', ESTIMATED:'估计时间', MISSING:'时间缺失', CONFLICTED:'时间有冲突', ASSERT:'报警断言', CLEAR:'明确恢复', REPORT:'报警上报', OPEN:'仍未明确恢复', CLEARED:'已明确恢复', TERMINATED_UNKNOWN:'终止边界未知', COMPLETE:'完整边界', LEFT_CENSORED:'开始边界未知', RIGHT_CENSORED:'结束边界未知', BOTH_CENSORED:'两端边界未知', CANDIDATE:'候选', PENDING:'待确认', CONFIRMED:'人工确认', DISPUTED:'存在争议', REJECTED:'已排除', PLANNED:'待实施', IN_PROGRESS:'实施中', IMPLEMENTED:'已实施', VERIFIED:'已验收', IMPROVED:'改善', NOT_IMPROVED:'未改善', WORSENED:'恶化', INSUFFICIENT_DATA:'资料不足', INSUFFICIENT_EVIDENCE:'资料不足', REPORT_COUNT:'去重上报次数达到人工阈值', KNOWN_CYCLE_STARTS:'可靠新开始达到人工阈值', OPEN_DURATION:'可靠持续时间达到人工阈值', NOT_COMPARABLE:'条件不可比较', QUEUED:'等待执行', RUNNING:'正在执行', SUCCEEDED:'完成', FAILED:'失败', STOPPED:'已停止', CANCEL_REQUESTED:'正在停止', COMPONENT_STATE:'部件状态', DEVICE_DIRECT:'设备直接上报', RULE_LIFECYCLE:'规则生命周期', COMPONENT_BOOLEAN:'部件明确布尔状态', DIRECT_EXPLICIT_STATE:'设备明确恢复语义', RECORDED_RULE_LIFECYCLE:'记录的规则周期', REPORT_ONLY:'仅上报，不计算物理周期' }
Object.assign(labels, { POINT_IDENTITY_UNCONFIRMED:'当前实物身份尚未人工确认', RECEIVED_TIME_BASIS_ONLY:'仅有平台接收时间，无法比较物理报警变化', CONDITIONS_DIFFER:'改善前后现场条件不一致', MINIMUM_OR_COVERAGE_NOT_MET:'尚未满足固定资料要求或登记覆盖要求', HISTORICAL_COLLECTION_COVERAGE_NOT_PROVEN:'历史采集完整性尚未证明', HISTORICAL_COVERAGE_NOT_PROVEN:'历史覆盖完整性尚未证明', ASSET_IDENTITY_HISTORY_UNAVAILABLE:'历史实物身份资料不可用', NO_VERIFIED_MONITORING_INTERVALS:'缺少已人工核实可用的监测区间', NO_ELIGIBLE_ACTIVITIES:'没有符合准入条件的实际活动', NO_FULL_ACTIVITY_DECLARATION:'缺少完整活动登记声明', DECLARED_COMPLETE_SUBWINDOW_ONLY:'仅部分窗口声明完整登记', REGISTERED_SUBSET_ONLY:'只覆盖已登记的活动子集', UNRESOLVED_ACTIVITY_RELATIONS:'仍有未核实或有争议的活动归属', UNCOMPARABLE_OBSERVATION_CLOCK:'事件时钟不可比较', RECEIVED_DISTRIBUTION_NOT_PHYSICAL_CYCLES:'接收时间分布不能替代物理报警周期', NO_CONFIRMED_OBSERVATION_PLAN:'缺少已确认的固定观察计划', READ_LIMIT_REACHED:'本次读取达到上限，仍有资料未包含', HISTORICAL_READ_LIMIT_REACHED:'历史读取达到本次上限', HISTORICAL_PROJECTION_ORIGINAL_ACCEPTANCE_UNKNOWN:'历史投影的原始生产接受状态未知', HISTORICAL_STANDARD_SOURCE_REQUIRES_EXPLICIT_REFRESH:'历史成功解析来源须由人工明确刷新', HISTORICAL_COLLECTION_COVERAGE_UNKNOWN:'历史采集覆盖未知', HISTORICAL_UNRESOLVED:'历史归一化完成，原始接受状态与覆盖仍待核实', ORIGINAL_PRODUCTION_ACCEPTANCE_UNKNOWN:'历史输入原始生产接受状态未知', RULE_EVALUATIONS_NOT_RECONSTRUCTED:'历史规则执行结果未重建', NORMALIZED_FACT_LIMIT_REACHED:'逐次事实归一化达到本次上限', NO_RECONSTRUCTABLE_ALARM_SIGNAL:'没有可重建的报警信号', CROSS_STORE_READ_CUTOFFS_ARE_INDEPENDENT:'各历史存储的读取截点独立，不能推断完整连续采集', ORIGINAL_STANDARD_METADATA_AND_ACCEPTANCE_UNKNOWN:'原始标准报文元数据与生产接受状态未知', EVENT_AT:'可信来源事件时间', RECEIVED_AT:'平台接收时间', VERIFIED_MONITORING:'已核实可用监测', TEST:'测试', OFFLINE:'明确离线', GAP:'采集缺口', CLOCK_UNKNOWN:'时钟不可比较' })
export const label = value => labels[value] || value || '未知'
export const time = value => value == null || value === '' || !Number.isFinite(Number(value)) || Number(value) <= 0 ? '未知' : new Date(Number(value)).toLocaleString('zh-CN', { hour12:false })
export const activeRun = row => ['QUEUED','RUNNING','CANCEL_REQUESTED'].includes(row?.status)
export const tone = state => ['FAILED','WORSENED','DISPUTED'].includes(state) ? 'danger' : ['COMPLETED','UNKNOWN','PARTIAL','INSUFFICIENT_DATA','INSUFFICIENT_EVIDENCE','NOT_COMPARABLE','UNABLE_TO_DETERMINE'].includes(state) ? 'warning' : 'info'
export const resultOptions = alarmType => alarmType === 'FIRE' ? ['FIRE_OBSERVED','OTHER_ABNORMALITY_OBSERVED','NO_FIRE_OBSERVED','UNABLE_TO_DETERMINE'] : ['TARGET_ABNORMALITY_OBSERVED','OTHER_ABNORMALITY_OBSERVED','NO_TARGET_ABNORMALITY_OBSERVED','UNABLE_TO_DETERMINE']
export function sameSignal(a, b) { return !!a && !!b && ['deviceId','componentId','alarmType','originKind','signalKey'].every(key => (a[key] || '') === (b[key] || '')) }
export function verificationPayload(form, observations) {
  if (!form.deviceId || !form.alarmType || !form.originKind || !form.signalKey) throw new Error('请选择一个明确点位和信号')
  if (!form.observationIds?.length) throw new Error('请选择本次实际核实的事件')
  const chosen = observations.filter(row => form.observationIds.includes(row.id))
  if (chosen.length !== form.observationIds.length || chosen.some(row => !sameSignal(row, form))) throw new Error('核实事件必须属于同一个点位、报警类型和信号')
  if (!form.templateRevisionId || !form.scenePresetRevisionId || !form.typeProfileRevisionId) throw new Error('请选择冻结的模板、场景及类型配置版本')
  if (!form.verificationMethod || !form.fieldResult || !form.activityRelation || !form.facilityStatus) throw new Error('请填写核实方式及三项现场观测；未知可以保存')
  if (form.verificationMethod !== 'UNVERIFIED' && !form.verifiedAt) throw new Error('请填写实际核实时间')
  return { ...form, observationIds:[...form.observationIds], fieldValues:{ ...form.fieldValues, facilityStatus:form.facilityStatus }, verifiedAt:form.verifiedAt || null }
}
export function numericMetric(value) {
  if (value == null || value.state === 'UNCOMPUTABLE' || value.computable === false) return '不可计算'
  if (typeof value === 'number') return value.toLocaleString('zh-CN', { maximumFractionDigits:3 })
  if (value.denominator != null) return Number(value.denominator) > 0 && Number.isFinite(Number(value.numerator)) ? `${value.numerator} / ${value.denominator}${value.ratio == null ? '' : ` · ${(Number(value.ratio)*100).toFixed(1)}%`}` : '不可计算'
  return value.value == null ? '未知' : numericMetric(value.value)
}
export function outputRows(outputs = []) { return outputs.flatMap(row => { const body = row.body || row; return Array.isArray(body.items) ? body.items : Array.isArray(body.metrics) ? body.metrics : [{ ...body, id:row.id || body.id, deviceId:body.deviceId || row.deviceId }] }) }
export const conflictMessage = error => error?.status === 409 ? '资料或版本已变化。已保留表单，请刷新最新证据和差异后重新核对。' : error?.message || '操作失败'
export function reportDifferences(previous, current) {
  if (!previous || !current) return []
  const names = { conclusion:'人工结论', roundId:'治理轮次', analysisSnapshotId:'固定分析快照', factsHash:'固定事实', dataRevision:'资料版本', followup:'后续安排', followupOwnerUserId:'后续负责人' }
  const changes = Object.entries(names).filter(([key]) => previous[key] !== current[key]).map(([key, name]) => ({ name, before:previous[key] ?? '未知', after:current[key] ?? '未知' }))
  if (JSON.stringify(previous.limitations || []) !== JSON.stringify(current.limitations || [])) changes.push({ name:'具体限制', before:(previous.limitations || []).join('；') || '未记录', after:(current.limitations || []).join('；') || '未记录' })
  const oldVersions = new Map((previous.resources || []).map(row => [row.id, row.version]))
  for (const resource of current.resources || []) if (oldVersions.get(resource.id) !== resource.version) changes.push({ name:`新增或变化资料 · ${resource.kind}`, before:oldVersions.has(resource.id) ? `版本 ${oldVersions.get(resource.id)}` : '本版本未引用', after:`${resource.id} · 版本 ${resource.version}` })
  return changes
}

export function eventLabel(value) {
  const parts=String(value||'').split(':'), kinds={case:'事项',round:'轮次','alarm-link':'事件关联','verification-link':'核实关联',verification:'现场核实',cause:'原因',measure:'措施','observation-plan':'观察计划','observation-review':'观察评价','business-link':'业务关联',attachment:'附件'}, actions={create:'新建',update:'编辑草稿',confirm:'人工确认',start:'开始',assign:'指派负责人','confirm-identity':'确认实物身份','start-observation':'开始观察',complete:'完成人工评价',cancel:'取消并保留后续',reopen:'复核复发并重开',implement:'记录实施',verify:'独立验收',dispute:'标记争议',reject:'排除',corrections:'追加更正',withdraw:'撤回'}
  return parts.map((part,index)=>index===0?kinds[part]||part:actions[part]||part).join(' · ')
}
