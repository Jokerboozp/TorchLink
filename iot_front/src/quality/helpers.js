export const qualityStates = {
  assessed: '已评估', partial: '部分数据', unknown: '证据不足', not_applicable: '不适用',
  QUEUED: '排队中', PREPARING: '准备事实', RUNNING: '计算中', SUCCEEDED: '已完成', PARTIAL: '部分完成', FAILED: '失败', CANCELLED: '已停止'
}

export const reviewStates = {
  CONFIRMED_PROBLEM: '已确认问题', NORMAL_EXPLANATION: '存在正常解释', RESOLVED: '已处理', OBSERVE: '继续观察'
}

export const findingKinds = {
  invalid_format: '格式或类型异常', range_below: '低于确认量程', range_above: '高于确认量程',
  missing_event_slots: '事件时间缺槽', missing_reception_slots: '接收时间缺槽', event_out_of_order: '事件时间乱序',
  future_event_time: '设备未来时间', long_stability: '长期无变化线索', rate_change: '变化率线索',
  historical_deviation: '历史基线偏离', cusum_drift: '小幅漂移线索'
}

export const runIsActive = run => ['QUEUED', 'PREPARING', 'RUNNING'].includes(run?.status)
export const qualityStage = value => /^calculating:\d+$/.test(value || '') ? `正在计算第 ${value.split(':')[1]} 组测点` : ({ preparing: '读取固定输入', 'inputs-frozen': '输入已固定', complete: '事实计算完成', 'time-limit': '达到运行时限', cancelled: '已停止' }[value] || value || '等待执行')
export const stateLabel = state => qualityStates[state] || state || '未知'
export const stateTone = state => ['FAILED', 'issue'].includes(state) ? 'danger' : ['PARTIAL', 'partial', 'unknown', 'verification_required'].includes(state) ? 'warning' : ['SUCCEEDED', 'assessed'].includes(state) ? 'success' : 'info'

// Only the algorithm's assessed/partial values are displayable percentages.
// A zero denominator and an unknown source are never rendered as 0% or 100%.
export function ratioLabel(ratio) {
  if (!ratio || ratio.state === 'unknown') return '证据不足'
  if (ratio.state === 'not_applicable') return '不适用'
  if (!(ratio.denominator > 0)) return ratio.state === 'partial' ? '证据不足' : '不适用'
  if (ratio.value == null || !Number.isFinite(Number(ratio.value))) return '证据不足'
  return `${(Number(ratio.value) * 100).toFixed(1)}%${ratio.state === 'partial' ? ' · 部分数据' : ''}`
}

export function timeMs(value) {
  if (value == null || value === '') return null
  if (typeof value === 'number') return Number.isFinite(value) && value > 0 ? value : null
  const timestamp = Date.parse(value)
  // Go's zero time is an absent clock, not a timestamp to display.
  return Number.isFinite(timestamp) && timestamp > 0 ? timestamp : null
}

export const qualityTime = value => {
  const timestamp = timeMs(value)
  return timestamp == null ? '未知' : new Date(timestamp).toLocaleString('zh-CN', { hour12: false })
}
export const compactNumber = value => value == null || !Number.isFinite(Number(value)) ? '—' : Number(value).toLocaleString('zh-CN', { maximumFractionDigits: 6 })

export function recordBody(value) {
  if (!value || typeof value !== 'object') return value
  const body = value.body && typeof value.body === 'object' ? value.body : value.summary && typeof value.summary === 'object' ? value.summary : {}
  return { ...value, ...body, revisionId: value.id, revisionVersion: value.version ?? body.version, resourceId: value.resourceId || body.resourceId || body.id || value.id, id: body.id || value.id }
}

export function profileDraft(value = {}) {
  const row = recordBody(value)
  return {
    resourceId: row.resourceId || '', expectedVersion: Number(row.revisionVersion || 0), scope: String(row.scope || 'personal').toLowerCase(),
    deviceIds: [...(row.deviceIds || [])], attributeId: row.attributeId || '', productId: row.productId || '', targetType: row.targetType || (row.productId ? 'PRODUCT' : 'DEVICE'),
    mode: row.mode || 'periodic', effectiveFrom: timeMs(row.effectiveFrom) ?? null, effectiveTo: timeMs(row.effectiveTo),
    scheduleAnchor: timeMs(row.scheduleAnchor), periodSeconds: row.periodMs == null ? null : row.periodMs / 1000,
    toleranceSeconds: row.toleranceMs == null ? null : row.toleranceMs / 1000,
    valueType: row.valueType || 'number', required: row.required ?? true, unit: row.unit || '', unitConfirmed: Boolean(row.unitConfirmed),
    rangeConfirmed: Boolean(row.rangeConfirmed), minimum: row.minimum ?? null, maximum: row.maximum ?? null,
    epsilon: row.epsilon ?? null, stableDurationSeconds: row.stableDurationMs ? row.stableDurationMs / 1000 : null,
    maxRate: row.maxRate ?? null, maxSequenceGapSeconds: row.maxSequenceGapMs ? row.maxSequenceGapMs / 1000 : null,
    futureToleranceSeconds: row.futureToleranceMs == null ? null : row.futureToleranceMs / 1000,
    clockCalibrated: Boolean(row.clockCalibrated), minimumSamples: row.minimumSamples || 30,
    deviationMethod: row.deviationMethod || '', madMultiplier: row.madMultiplier ?? null,
    absoluteDeviation: row.absoluteDeviation ?? null, quantileLower: row.quantileLower ?? .05, quantileUpper: row.quantileUpper ?? .95,
    cusumEnabled: Boolean(row.cusum), cusumVersion: row.cusum?.version || '',
    cusumAllowance: row.cusum?.allowance ?? null, cusumThreshold: row.cusum?.threshold ?? null
  }
}

const finite = (value, name) => {
  if (value == null || value === '' || !Number.isFinite(Number(value))) throw new Error(`请填写有限的${name}`)
  return Number(value)
}
const optional = (value, name) => value == null || value === '' ? undefined : finite(value, name)
const milliseconds = (value, name) => {
  const seconds = optional(value, name)
  if (seconds == null) return undefined
  if (seconds < 0 || !Number.isSafeInteger(seconds * 1000)) throw new Error(`${name}须为非负数，精确到毫秒`)
  return seconds * 1000
}

// Physical cadence/range/epsilon stay empty until an operator supplies evidence.
// All public timestamps and durations are milliseconds.
export function profilePayload(form) {
  const deviceIds = [...new Set(form.deviceIds || [])].filter(Boolean).sort()
  if (!deviceIds.length || !form.attributeId?.trim()) throw new Error('请选择设备并填写属性标识')
  if (!timeMs(form.effectiveFrom)) throw new Error('请填写配置生效时间')
  if (timeMs(form.effectiveTo) && form.effectiveTo <= form.effectiveFrom) throw new Error('配置失效时间必须晚于生效时间')
  const body = {
    attributeId: form.attributeId.trim(), productId: form.targetType === 'PRODUCT' ? form.productId || '' : '', targetType: form.targetType || 'DEVICE', mode: form.mode,
    effectiveFrom: form.effectiveFrom, effectiveTo: timeMs(form.effectiveTo) ?? undefined,
    valueType: form.valueType, required: Boolean(form.required), unit: form.unit?.trim() || '',
    unitConfirmed: Boolean(form.unitConfirmed), rangeConfirmed: Boolean(form.rangeConfirmed),
    minimum: optional(form.minimum, '量程下限'), maximum: optional(form.maximum, '量程上限'),
    epsilon: optional(form.epsilon, '精度 epsilon'), maxRate: optional(form.maxRate, '变化率阈值'),
    stableDurationMs: milliseconds(form.stableDurationSeconds, '允许稳定时长'),
    maxSequenceGapMs: milliseconds(form.maxSequenceGapSeconds, '连续样本最大间隔'),
    futureToleranceMs: milliseconds(form.futureToleranceSeconds, '未来时间容忍'),
    clockCalibrated: Boolean(form.clockCalibrated), minimumSamples: finite(form.minimumSamples, '最小样本数'),
    deviationMethod: form.deviationMethod || '', madMultiplier: optional(form.madMultiplier, 'MAD 倍数'),
    absoluteDeviation: optional(form.absoluteDeviation, '绝对偏差门槛'),
    quantileLower: optional(form.quantileLower, '下分位'), quantileUpper: optional(form.quantileUpper, '上分位')
  }
  if (body.targetType === 'PRODUCT' && !body.productId) throw new Error('请选择产品配置的目标产品')
  if (!Number.isInteger(body.minimumSamples) || body.minimumSamples < 30) throw new Error('统计最小样本数不能低于 30')
  if (body.mode === 'periodic') {
    body.scheduleAnchor = timeMs(form.scheduleAnchor)
    body.periodMs = milliseconds(form.periodSeconds, '上报周期')
    body.toleranceMs = milliseconds(form.toleranceSeconds, '周期容忍')
    if (!body.scheduleAnchor || !(body.periodMs > 0) || body.toleranceMs == null) throw new Error('周期测点须填写固定锚点、周期和容忍')
    if (body.toleranceMs >= body.periodMs / 2) throw new Error('容忍必须严格小于半个上报周期')
  } else if (body.mode !== 'event') throw new Error('请选择周期或事件上报模式')
  if (body.rangeConfirmed && (!body.unitConfirmed || body.minimum == null || body.maximum == null)) throw new Error('确认量程前须确认单位并填写上下限')
  if (body.minimum != null && body.maximum != null && body.minimum > body.maximum) throw new Error('量程下限不能大于上限')
  if (body.epsilon != null && body.epsilon <= 0) throw new Error('epsilon 必须大于 0')
  if (body.maxRate != null && body.maxRate < 0) throw new Error('变化率阈值不能为负数')
  if (body.madMultiplier != null && body.madMultiplier <= 0) throw new Error('MAD 倍数必须大于 0')
  if (body.absoluteDeviation != null && body.absoluteDeviation <= 0) throw new Error('绝对偏差门槛必须大于 0')
  if (body.deviationMethod === 'quantile' && !(body.quantileLower >= 0 && body.quantileUpper <= 1 && body.quantileLower < body.quantileUpper)) throw new Error('分位范围须满足 0 ≤ 下分位 < 上分位 ≤ 1')
  if (form.cusumEnabled) {
    body.cusum = { version: form.cusumVersion?.trim(), allowance: finite(form.cusumAllowance, 'CUSUM 容差'), threshold: finite(form.cusumThreshold, 'CUSUM 门槛') }
    if (!body.cusum.version || body.cusum.allowance < 0 || body.cusum.threshold <= 0) throw new Error('CUSUM 须填写参数版本、非负容差和正门槛')
  }
  return { resourceId: form.resourceId || '', expectedVersion: form.expectedVersion || 0, scope: form.scope || 'personal', deviceIds, body }
}

export function metricRows(outputs = []) {
  return outputs.flatMap(output => {
    const body = output.body || output
    const result = body.result || body
    const rows = Array.isArray(result.metrics) ? result.metrics : [result]
    return rows.filter(row => row.profileId || row.eventCompleteness || row.format).map(row => ({ ...row, outputId: output.id, deviceId: output.deviceId || result.deviceId, attributeId: body.attributeId || result.attributeId, resultState: result.state, resultLimitations: result.limitations || [], parse: result.parse, originalFindingCount: result.originalFindingCount, representedFindingCount: result.representedFindingCount, originalEvidenceCount: result.originalEvidenceCount, representedEvidenceCount: result.representedEvidenceCount, representationPolicy: result.representationPolicy }))
  })
}

export function findingRows(outputs = []) {
  return outputs.flatMap(output => {
    const body = output.body || output
    const result = body.result || body
    const rows = Array.isArray(result.findings) ? result.findings : [result]
    return rows.filter(row => row.kind && row.metricId).map(row => ({ ...row, deviceId: output.deviceId || result.deviceId, attributeId: body.attributeId || result.attributeId }))
  })
}

export function evidenceRows(outputs = []) {
  return outputs.map(output => ({ ...output, ...(output.summary || output.body || {}), id: output.id, deviceId: output.deviceId, availability: String(output.originalAvailability || 'unknown').toLowerCase() }))
}

// Membership is selected by the original event window. Changing the displayed
// clock must retain late receptions and query visibility outside that window.
export function qualityChartSamples(items = [], clock = 'eventAt', window) {
  return items.filter(row => {
    const eventAt = timeMs(row.eventAt), shownAt = timeMs(row[clock])
    return shownAt != null && (!window || (eventAt != null && eventAt >= timeMs(window.start) && eventAt < timeMs(window.end)))
  }).map(row => ({ ...row, eventAt: row[clock], occurredAt: row[clock] }))
}

export function evidenceChart(evidence = [], baselineValue, threshold) {
  const samples = evidence.filter(row => timeMs(row.eventAt || row.occurredAt) && typeof row.value === 'number' && Number.isFinite(row.value)).sort((a, b) => timeMs(a.eventAt || a.occurredAt) - timeMs(b.eventAt || b.occurredAt) || String(a.messageId || a.id).localeCompare(String(b.messageId || b.id)))
  // Collapse tied timestamps for a chart only; the evidence table retains all.
  const unique = samples.filter((row, index) => !index || timeMs(row.eventAt || row.occurredAt) !== timeMs(samples[index - 1].eventAt || samples[index - 1].occurredAt))
  const times = unique.map(row => timeMs(row.eventAt || row.occurredAt))
  const series = [{ name: '代表证据原值', values: unique.map(row => row.value) }]
  if (typeof baselineValue === 'number' && Number.isFinite(baselineValue)) series.push({ name: '确认基线中位数', values: times.map(() => baselineValue) })
  if (typeof threshold === 'number' && Number.isFinite(threshold)) series.push({ name: '配置阈值', values: times.map(() => threshold) })
  return { times, series, collapsedCount: samples.length - unique.length }
}
