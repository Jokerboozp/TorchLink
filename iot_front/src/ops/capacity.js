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

// 活动运行 3 秒刷新一次，结束后停止轮询。
export function pollDelay(runs) {
  return (runs || []).some(run => run.active || !isFinished(run.status)) ? 3000 : 0
}

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

export const planTemplate = `schemaVersion: 1
name: quick-check
suite: core
preset: quick
seed: 1
credentials:
  operatorSecretRef: capacity-operator
fixtures:
  tenant: tenant_001
  product: cap-standard
  deviceCount: 20
  reuseDevices: true
  messageBytes: 512
load:
  ingressShare: {http: 1}
  initialMessagesPerSecond: 10
  queryRequestsPerSecond: 1
  queryMix: {devices: 0.5, alarms: 0.5}
search:
  rates: [10, 20]
  warmup: 10s
  measure: 30s
  drainTimeout: 2m
budget:
  maximumWallTime: 30m
  maximumMessagesPerSecond: 200
  maximumEvidenceGiB: 1
`
