import { qualityTime, recordBody, runIsActive, reviewStates, stateLabel, stateTone, timeMs } from '../quality/helpers.js'
export { recordBody, runIsActive, reviewStates, stateLabel, stateTone, timeMs }
export const monitoringTime = value => {
 const timestamp=timeMs(value), text=qualityTime(value)
 return timestamp!=null&&timestamp%1000?`${text}.${String(timestamp%1000).padStart(3,'0')}`:text
}
export const trackLabels = { connection: '连接状态', received: '合格属性接收', attribute: '属性有效数据', data: '关键属性有效数据' }
export const intervalStates = { AVAILABLE: '已知可用', UNAVAILABLE: '已知不可用', UNKNOWN: '依据未知', EXCLUDED: '已确认停运 / 检修', NOT_APPLICABLE: '不适用' }
export const findingKinds = { LONG_DATA_GAP: '长数据缺口', FREQUENT_DATA_GAPS: '频繁数据中断', INSUFFICIENT_HISTORY: '历史依据不足', COMMON_MISSING_REPORT: '共同缺报现象', DEPENDENCY_CONCENTRATION: '接入依赖集中' }
export const dependencyKinds = { 'parent-device': '父设备依赖', 'access-profile': '接入配置依赖', collector: '采集器依赖', PARENT_DEVICE: '父设备依赖', ACCESS_PROFILE: '接入配置依赖', COLLECTOR: '采集器依赖' }
export const messageTypes = { PROPERTY_REPORT: '属性上报', EVENT_REPORT: '事件上报', ALARM_REPORT: '设备告警', STATE_CHANGE: '状态变化', COMMAND_REPLY: '命令回复', LOG_REPORT: '日志上报' }
export const reasonLabels = {
 EVENT_REPORTING_HAS_NO_FIXED_PERIOD: '事件上报没有固定周期', MONITORING_PROFILE_MISSING: '该区间缺少监测策略',
 SOURCE_COVERAGE_UNKNOWN: '来源覆盖未知', AVAILABLE_AT_UNKNOWN: '首次可查询时间未知', STATE_HISTORY_UNKNOWN: '连接历史依据未知',
 CURRENT_DEPENDENCY_SNAPSHOT_HAS_NO_HISTORICAL_COVERAGE: '依赖来源仅有当前快照，未覆盖历史关系', PARENT_RESOURCE_NOT_AUTHORIZED: '父资源不在本次获授权分析范围', DEPENDENCY_HISTORY_UNKNOWN: '依赖历史依据未知',
 ATTRIBUTE_HISTORY_UNKNOWN: '属性历史依据未知', CONFIRMED_OBSERVATION_EXCLUSION: '已人工确认的停运或检修窗口', CONNECTION_HISTORY_UNKNOWN_OR_CONFLICT: '连接历史依据未知或冲突', CONNECTION_SEED_OR_HISTORY_MISSING: '缺少连接起点状态或历史', SOURCE_SAMPLE_CLOCK_OR_AVAILABILITY_UNKNOWN: '样本时钟或首次可查询依据未知',
 CURRENT_ACCESS_PROFILE_UNAVAILABLE_OR_NOT_OWNED: '当前接入配置不可用或无法确认归属', CURRENT_DEPENDENCY_ASSUMPTION_ONLY: '仅支持固定当前依赖假设', CURRENT_DEPENDENCY_QUERY_UNAVAILABLE: '当前依赖资料无法读取',
 FIRST_AVAILABILITY_RECONSTRUCTED_AT_PROCESSING_STAGE: '首次可查询时间由处理阶段记录重建', FIRST_AVAILABILITY_UNKNOWN: '首次可查询时间未知', HISTORICAL_COLLECTION_OR_FUTURE_RANGE_NOT_COVERED: '历史采集起点或未来区间未覆盖', NO_SOURCE_COVERAGE: '没有来源覆盖依据', SOURCE_QUERY_FAILED_OR_UNAVAILABLE: '来源查询失败或不可用', SOURCE_RECORD_MISSING: '原始来源记录缺失', SOURCE_WARMUP_NOT_COVERED: '起点前的时效预热区间未覆盖',
 FROZEN_SOURCE_EXPIRED_OR_MISSING: '固定来源已过期或缺失', FROZEN_SOURCE_QUERY_FAILED: '固定来源读取失败', FROZEN_SOURCE_UNAVAILABLE: '固定来源不可用', FROZEN_SOURCE_VERSION_CHANGED: '固定来源版本已变化',
 CONNECTION_READ_CAP_REACHED: '连接历史读取达到数量上限', DEPENDENCY_READ_CAP_REACHED: '依赖历史读取达到数量上限', MEASUREMENT_READ_CAP_REACHED: '测量读取达到数量上限', RAW_READ_CAP_REACHED: '原文读取达到数量上限', CALCULATION_INTERVAL_CAP_REACHED: '区间计算达到数量上限', COMMON_GAP_CALCULATION_CAP_REACHED: '共同缺报计算达到数量上限', COMMON_GAP_INPUT_CAP_REACHED: '共同缺报输入达到数量上限', DEPENDENCY_GROUP_CALCULATION_CAP_REACHED: '依赖组计算达到数量上限', FINDING_REPRESENTATION_CAP_REACHED: '发现展示采用数量受限的代表记录', INTERVAL_REPRESENTATION_CAP_REACHED: '区间展示采用数量受限的代表记录',
 QUALITY_SNAPSHOT_NOT_SELECTED: '本任务未选定数据质量快照', RECEIVED_RAW_PARSE_FAILED: '已收到原文，但最后解析失败', RUN_TIME_BUDGET_EXHAUSTED: '运行时限已到，保留已完成事实', UNKNOWN_NO_IMMUTABLE_ATTEMPT_HISTORY: '缺少不可变的全部解析尝试历史', UNKNOWN_OR_UNPROCESSED_TRACKS: '存在未知或尚未处理的轨道', SOURCE_SEED_AND_HISTORY: '采用来源起点状态与历史记录',
 AVAILABLE: '来源可读取', UNKNOWN: '依据未知', RECORDED: '已有提交记录', PARTIAL: '部分覆盖', NOT_STARTED: '尚未回填', FIXTURE: '受控验收回填', CONTROLLED_SYNTHETIC: '受控合成验收来源', CURRENT_SNAPSHOT: '固定当前快照', DEPENDENCY_WINDOW_SEED_MISSING: '缺少依赖窗口的起点记录',
}
export const reasonText = value => typeof value==='string'&&value.includes(';')?value.split(';').map(item=>reasonLabels[item]||item).join('；'):reasonLabels[value] || value
export const monitoringStage = value => ({ preparing: '读取固定输入', 'inputs-frozen': '输入已固定', complete: '事实计算完成', 'time-limit': '达到运行时限', cancelled: '已停止' }[value] || (/^calculating:/.test(value || '') ? '正在计算监测区间' : value || '等待执行'))
export function durationLabel(value) {
 if (value == null || value === '' || !Number.isFinite(Number(value)) || Number(value) < 0) return '未知'
 const ms = Number(value)
 if (ms >= 3600000) return `${(ms / 3600000).toLocaleString('zh-CN', { maximumFractionDigits: 3 })} 小时`
 if (ms >= 60000) return `${(ms / 60000).toLocaleString('zh-CN', { maximumFractionDigits: 3 })} 分钟`
 return `${(ms / 1000).toLocaleString('zh-CN', { maximumFractionDigits: 3 })} 秒`
}
const percent = value => value != null && Number.isFinite(Number(value)) && Number(value) >= 0 && Number(value) <= 1 ? `${(Number(value) * 100).toFixed(1)}%` : '证据不足'
export function metricRatio(metric, field) {
 if (!metric || !(metric.plannedMs > 0)) return '不适用'
 if (field === 'knownAvailability') return metric.availableMs + metric.unavailableMs > 0 ? percent(metric.knownAvailability) : metric.notApplicableMs > 0 ? '不适用' : '证据不足'
 if (metric.notApplicableMs > 0) return '不适用'
 if (field === 'fullWindowAvailability' && metric.unknownMs > 0) return '存在未知区间，不能给出'
 return percent(metric[field])
}
export function unwrapOutputs(outputs = []) { return outputs.map(output => ({ ...output, ...(output.body || {}), id: output.id || output.body?.id, outputId: output.id })) }
export function metricRows(outputs = []) {
 return outputs.flatMap(output => (output.body?.metrics || []).map(row => ({ ...row, deviceId: row.deviceId || output.deviceId || output.body.deviceId, outputId: output.id, intervalCount: output.body.intervalCount, representedIntervalCount: output.body.representedIntervalCount, originalFindingCount: output.body.originalFindingCount, representedFindingCount: output.body.representedFindingCount, profiles: output.body.profiles || [], limitations: output.body.limitations || [] })))
}
// Source envelopes are authoritative. Measurements and intervals carry their
// own clocks in the frozen summary; expose them without replacing document IDs.
export function monitoringEvidenceRows(outputs = []) {
 return outputs.map(output => {
  const summary = output.summary || {}, fact = summary.measurement || summary.interval || summary.group || summary
  return { ...summary, ...fact, ...output, summary, sampleId: fact.id, attributeId: fact.property || fact.attributeId, availability: String(output.originalAvailability || 'unknown').toLowerCase() }
 })
}
export const overallMetrics = rows => rows.filter(row => !row.attributeId && !row.profileId)
export function timelineLanes(rows = [], window, detail = false) {
 const lanes = new Map()
 for (const row of rows) {
  if (detail ? !row.attributeId : !!row.attributeId) continue
  const start = Math.max(Number(window.start), Number(row.start)), end = Math.min(Number(window.end), Number(row.end))
  if (!(end > start) || !intervalStates[row.state]) continue
  const key = JSON.stringify([row.deviceId, row.track, row.attributeId || ''])
  if (!lanes.has(key)) lanes.set(key, { key, deviceId: row.deviceId, track: row.track, attributeId: row.attributeId || '', items: [] })
  lanes.get(key).items.push({ ...row, displayStart: start, displayEnd: end, left: 100 * (start - window.start) / (window.end - window.start), width: 100 * (end - start) / (window.end - window.start) })
 }
 return [...lanes.values()].sort((a, b) => String(a.deviceId).localeCompare(String(b.deviceId)) || String(a.attributeId).localeCompare(String(b.attributeId)) || ['connection','received','attribute','data'].indexOf(a.track) - ['connection','received','attribute','data'].indexOf(b.track)).map(lane => ({ ...lane, items: lane.items.sort((a, b) => a.displayStart - b.displayStart || String(a.id).localeCompare(String(b.id))) }))
}
export function profileDraft(value = {}) {
 const row = recordBody(value)
 return { resourceId: row.resourceId || '', expectedVersion: row.revisionVersion || 0, scope: String(row.scope || 'personal').toLowerCase(), deviceIds: [...(row.deviceIds || [])], targetType: row.targetType || 'DEVICE', productId: row.productId || '', effectiveFrom: row.effectiveFrom || null, effectiveTo: row.effectiveTo || null, mode: row.mode || 'periodic', merge: row.merge || 'ALL', periodSeconds: row.periodMs > 0 ? row.periodMs / 1000 : null, toleranceSeconds: row.toleranceMs != null ? row.toleranceMs / 1000 : null, longGapSeconds: row.longGapMs > 0 ? row.longGapMs / 1000 : null, frequentGapCount: row.frequentGapCount || null, importance: row.importance || '', messageTypes: [...(row.messageTypes || [])], attributes: (row.attributes?.length ? row.attributes : [{ id: '', valueType: 'number' }]).map(a => ({ id: a.id, valueType: a.valueType, minimum: a.minimum ?? null, maximum: a.maximum ?? null })) }
}
function millis(value, label, positive = false) {
 if (value == null || value === '') throw new Error(`请填写${label}`)
 const result = Number(value) * 1000
 if (!Number.isSafeInteger(result) || result < 0 || (positive && result === 0)) throw new Error(`${label}须为有限且至少精确到毫秒的${positive ? '正' : '非负'}值`)
 return result
}
export function profilePayload(form) {
 const deviceIds = [...new Set(form.deviceIds)].sort()
 if (!deviceIds.length || !timeMs(form.effectiveFrom)) throw new Error('请选择明确设备并填写生效时间')
 if (form.effectiveTo && form.effectiveTo <= form.effectiveFrom) throw new Error('失效时间须晚于生效时间')
 if (!['DEVICE','PRODUCT','TENANT'].includes(form.targetType) || (form.targetType === 'PRODUCT' && !form.productId)) throw new Error('请选择有效配置目标')
 if (!['periodic','event'].includes(form.mode) || !['ALL','ANY'].includes(form.merge)) throw new Error('请选择上报模式和合并策略')
 if (!form.messageTypes.length || form.messageTypes.some(value => !messageTypes[value])) throw new Error('请明确可接受的报文类型')
 if (!form.attributes.length || form.attributes.length > 50) throw new Error('关键属性须为1至50项')
 const attributes = form.attributes.map(a => {
  const id = a.id.trim()
  if (!id || !['number','integer','boolean','string'].includes(a.valueType)) throw new Error('请填写属性标识与物模型类型')
  for (const value of [a.minimum, a.maximum]) if (value != null && !Number.isFinite(Number(value))) throw new Error('有效值边界须为有限值')
  if (a.minimum != null && a.maximum != null && Number(a.minimum) > Number(a.maximum)) throw new Error('有效值下限不能大于上限')
  const numeric = ['number','integer'].includes(a.valueType)
  return { id, valueType: a.valueType, minimum: numeric && a.minimum != null ? Number(a.minimum) : undefined, maximum: numeric && a.maximum != null ? Number(a.maximum) : undefined }
 })
 if (new Set(attributes.map(row => row.id)).size !== attributes.length) throw new Error('关键属性标识不能重复')
 if (form.frequentGapCount != null && (!Number.isSafeInteger(Number(form.frequentGapCount)) || form.frequentGapCount <= 0)) throw new Error('频繁中断门槛须为正整数')
 const body = { targetType: form.targetType, productId: form.targetType === 'PRODUCT' ? form.productId : undefined, effectiveFrom: form.effectiveFrom, effectiveTo: form.effectiveTo || undefined, mode: form.mode, attributes, messageTypes: [...new Set(form.messageTypes)], merge: form.merge, importance: form.importance.trim() || undefined, periodMs: form.mode === 'periodic' ? millis(form.periodSeconds, '预期周期', true) : 0, toleranceMs: form.mode === 'periodic' ? millis(form.toleranceSeconds, '时效容忍') : 0, longGapMs: form.longGapSeconds == null ? undefined : millis(form.longGapSeconds, '长缺口门槛', true), frequentGapCount: form.frequentGapCount == null ? undefined : Number(form.frequentGapCount) }
 return { resourceId: form.resourceId, expectedVersion: form.expectedVersion, scope: form.scope, deviceIds, body }
}
export function hypothesisPayload(group, at, expectedRunVersion, idempotencyKey) {
 if (!group?.id || !group.memberIds?.length || !['KNOWN','CURRENT_ONLY'].includes(group.historyQuality)) throw new Error('该接入组缺少可用快照依据')
 if (!Number.isSafeInteger(at) || (group.historyQuality === 'CURRENT_ONLY' ? at !== group.start : at < group.start || at >= group.end)) throw new Error('假设时点须位于当前固定依赖快照范围内')
 return { groupId: group.id, at, expectedRunVersion, idempotencyKey }
}
