// 容量测试页的纯函数：状态文案、阶段进度与轮询节奏，便于单独测试。

export const runStatusText = {
  QUEUED: '排队中',
  PREFLIGHT: '预检',
  PREPARING: '准备测试设备',
  WARMUP: '预热',
  RUNNING: '测量中',
  DRAINING: '等待排空',
  VERIFYING: '逐条核对',
  REPORTING: '生成报告',
  FINISHED: '已完成',
  FAILED: '执行失败',
  CANCELLING: '正在停止',
  CANCELLED: '已停止'
}

export const verdictText = { passed: '通过', failed: '失败', inconclusive: '证据不足' }

export const classText = {
  bounded: '已找到边界',
  bounded_wide: '已找到边界（区间较宽）',
  lower_bound_only: '至少达到下界',
  no_pass: '首档即失败',
  unstable: '结果不稳定',
  inconclusive: '证据不足',
  regression: '回归',
  soak: '长稳',
  resilience: '故障恢复',
  unmeasured: '未测量'
}

export const presetText = { quick: '快速回归', capacity: '容量搜索', soak: '长稳', resilience: '故障恢复' }

const finished = new Set(['FINISHED', 'FAILED', 'CANCELLED'])

export const isFinished = status => finished.has(status)

export function statusTone(status, verdict) {
  if (status === 'FAILED') return 'danger'
  if (!isFinished(status)) return 'warning'
  if (verdict === 'passed') return 'success'
  if (verdict === 'failed') return 'danger'
  return 'info'
}

// 测量窗口内按实际时间给出进度；窗口外不估算，返回 null。
export function windowProgress(run, now = Date.now()) {
  if (!run || !run.measureFrom || !run.measureTo || run.measureTo <= run.measureFrom) return null
  if (now < run.measureFrom) return 0
  return Math.min(100, Math.round(((now - run.measureFrom) / (run.measureTo - run.measureFrom)) * 100))
}

// 活动运行或后台清理期间 3 秒刷新一次，结束后停止轮询；busy 覆盖不在当前页的运行。
export function pollDelay(runs, busy = false) {
  return busy || (runs || []).some(run => run.active || run.cleaning || !isFinished(run.status)) ? 3000 : 0
}

export const historyCleanupStatusText = { RUNNING: '正在清理测试数据', SUCCEEDED: '本次清理已完成', PARTIAL: '已完成可清理部分', FAILED: '测试数据清理失败' }
export const historyCleanupRunning = job => job?.status === 'RUNNING'
export const cleanupCountText = value => Number.isSafeInteger(value) && value >= 0 ? value.toLocaleString('zh-CN') : '—'
export function historyCleanupHasTargets(preview) {
  if (!preview) return false
  return (preview.products?.length || 0) > 0 || ['runs', 'devices', 'rawMessages'].some(key => Number.isSafeInteger(preview[key]) && preview[key] > 0)
}

// 只显示服务端的实际计数，不用耗时推算清理进度。
export function historyCleanupProgress(job) {
  return Number.isSafeInteger(job?.processed) && Number.isSafeInteger(job?.total) && job.total > 0 && job.processed >= 0 && job.processed <= job.total ? Math.round(job.processed / job.total * 100) : null
}

export function cleanupCountItems(counts = {}) {
  const labels = { products: '产品', devices: '设备', rawMessages: '测试原文', standardMessages: '解析记录', alarms: '告警', rules: '测试规则', resources: '业务任务与文档', audits: '测试审计记录', accessReferences: '用户设备引用' }
  return Object.entries(labels).filter(([key]) => Number.isSafeInteger(counts?.[key]) && counts[key] >= 0).map(([key, label]) => ({ key, label, value: counts[key] }))
}

export function cleanupRuntimeItems(counts = {}) {
  const labels = { retainedRequests: '已发送 retained 清除请求' }
  return Object.entries(labels).filter(([key]) => Number.isSafeInteger(counts?.[key]) && counts[key] > 0).map(([key, label]) => ({ key, label, value: counts[key] }))
}

export const historyCleanupWarnings = job => [...new Set([...(job?.warnings || []), ...(job?.counts?.warnings || [])])]
export const historyCleanupResultText = job => job?.status === 'SUCCEEDED' && job.error ? '清理结果需确认' : job?.status === 'SUCCEEDED' && historyCleanupWarnings(job).length ? '已完成可清理部分' : historyCleanupStatusText[job?.status] || job?.status

export function boundText(value) {
  return value == null ? '—' : `${Number(value).toLocaleString('zh-CN', { maximumFractionDigits: 1 })} 条/秒`
}

export function phaseSummary(completed = []) {
  const counts = { passed: 0, failed: 0, inconclusive: 0 }
  for (const phase of completed) counts[phase.verdict] = (counts[phase.verdict] || 0) + 1
  return counts
}

export const reportFormats = [
  { value: 'html', label: 'HTML 报告', ext: 'html' },
  { value: 'markdown', label: 'Markdown', ext: 'md' },
  { value: 'json', label: '摘要 JSON', ext: 'json' },
  { value: 'csv', label: '阶段 CSV', ext: 'csv' },
  { value: 'zip', label: '完整证据 ZIP', ext: 'zip' }
]

// 预设表单的默认值：设备数、速率与时长都保持在单机可承受的起点。
export const presetDefaults = {
  quick: { devices: 20, startRate: 10, maxRate: 200, measureMinutes: 1 },
  capacity: { devices: 100, startRate: 20, maxRate: 1000, measureMinutes: 2 },
  soak: { devices: 100, startRate: 50, maxRate: 1000, measureMinutes: 60 }
}

export function defaultForm(preset = 'quick') {
  return { preset, ...presetDefaults[preset], mqtt: false, alarms: true, queries: true, realtime: false, exports: false, ai: false }
}

// 表单草稿按租户和用户保存在 sessionStorage：切换页面后恢复，切换身份不串用。
const draftPrefix = 'iot:capacity-draft:v1'
export const draftKey = session => `${draftPrefix}:${session?.tenant || 'unknown'}:${session?.user || 'unknown'}`

export function loadDraft(storage, session) {
  try {
    const saved = JSON.parse(storage?.getItem(draftKey(session)) || 'null')
    if (!saved || !presetDefaults[saved.form?.preset]) return null
    const form = defaultForm(saved.form.preset)
    for (const key of Object.keys(form)) if (typeof saved.form[key] === typeof form[key]) form[key] = saved.form[key]
    return { form, advanced: saved.advanced === true, planText: typeof saved.planText === 'string' ? saved.planText : '', environment: typeof saved.environment === 'string' ? saved.environment : '' }
  } catch {
    return null
  }
}

export function saveDraft(storage, session, draft) {
  try { storage?.setItem(draftKey(session), JSON.stringify(draft)) } catch { /* 存储不可用时只影响草稿恢复 */ }
}

// 平台对单台设备限速 20 条/秒，速率上限不能超过设备数 × 20，否则测到的是限速策略。
export const perDeviceLimit = 20
export const rateCeiling = form => Math.max(1, Math.floor(form.devices) * perDeviceLimit)

export function formProblems(form) {
  const problems = []
  if (!(form.devices >= 1 && form.devices <= 10000)) problems.push('设备数需在 1–10000 之间')
  if (!(form.startRate >= 1)) problems.push('起始速率至少 1 条/秒')
  if (form.startRate > form.maxRate) problems.push('起始速率不能高于速率上限')
  if (form.maxRate > rateCeiling(form)) problems.push(`速率上限不能超过设备数 × ${perDeviceLimit} = ${rateCeiling(form)} 条/秒（单台设备限速）`)
  if (!(form.measureMinutes >= 1)) problems.push('测量时长至少 1 分钟')
  return problems
}

// buildPlan 把表单转成计划 YAML；租户、操作员身份由平台补充，测试产品与规则由模块自动准备。
export function buildPlan(form) {
  const measure = `${Math.round(form.measureMinutes)}m`
  const devices = Math.round(form.devices)
  const mqttConnections = form.mqtt ? Math.max(1, Math.floor(devices / 2)) : 0
  const share = form.mqtt ? '{http: 0.5, mqtt: 0.5}' : '{http: 1}'
  const wall = { quick: '30m', capacity: '4h', soak: `${Math.round(form.measureMinutes) + 60}m` }[form.preset]
  const lines = [
    'schemaVersion: 1',
    `name: ui-${form.preset}`,
    'suite: core',
    `preset: ${form.preset}`,
    'seed: 1',
    'fixtures:',
    `  deviceCount: ${devices}`,
    '  reuseDevices: true',
    '  messageBytes: 512',
    '  fields: 6',
    '  autoProvision: true',
    `  alarmFraction: ${form.alarms ? 0.02 : 0}`,
    'load:',
    `  ingressShare: ${share}`,
    `  initialMessagesPerSecond: ${form.startRate}`
  ]
  if (mqttConnections) lines.push(`  mqttConnections: ${mqttConnections}`)
  if (form.queries) lines.push('  queryRequestsPerSecond: 1', '  queryMix: {devices: 0.4, alarms: 0.3, rawMessages: 0.3}')
  const modules = []
  if (form.realtime) modules.push('  realtime: {enabled: true, subscribers: 5}')
  if (form.exports) modules.push('  exports: {enabled: true, rawDownloadsPerMinute: 2, replaysPerMinute: 1, inspectionsPerMinute: 0}')
  if (form.ai) modules.push('  ai: {enabled: true, mode: real, runsPerMinute: 2, maxRuns: 20}')
  if (modules.length) lines.push('modules:', ...modules)
  lines.push('search:')
  if (form.preset === 'capacity') lines.push('  rampFactor: 1.5', '  warmup: 30s', `  measure: ${measure}`)
  else lines.push(`  rates: [${form.startRate}]`, '  warmup: 10s', `  measure: ${measure}`)
  lines.push('  drainTimeout: 5m', 'budget:', `  maximumWallTime: ${wall}`, `  maximumMessagesPerSecond: ${form.maxRate}`, `  maximumDevices: ${devices}`, '  maximumEvidenceGiB: 5')
  return lines.join('\n') + '\n'
}
