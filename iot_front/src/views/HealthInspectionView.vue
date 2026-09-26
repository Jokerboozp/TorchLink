<script setup>
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate'])
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { UiMessage } from '../ui/feedback.js'
import { api, download, formatTime, notifyError, session } from '../api'
import { businessStatuses, label, tagType } from '../labels'
import MarkdownContent from '../components/MarkdownContent.vue'
import { loadHealthInspection, saveHealthInspection } from '../healthInspectionState'

const inspectionStorage = typeof window !== 'undefined' ? window.sessionStorage : null
const report = ref(loadHealthInspection(inspectionStorage, session))
const progress = ref(null)
const loading = ref(false)
const downloading = ref(false)
const error = ref('')
const progressPercentage = computed(() => Math.max(0, Math.min(100, Number(progress.value?.progress || 0))))
const progressStatus = computed(() => progress.value?.status === 'failed' ? 'exception' : progress.value?.status === 'succeeded' ? 'success' : undefined)
const inspectionRunning = computed(() => loading.value || progress.value?.status === 'running')
const counts = computed(() => report.value?.counts || {})
let progressTimer = 0
let viewToken = 0

function stopProgressPolling() {
  if (progressTimer) clearTimeout(progressTimer)
  progressTimer = 0
}

function scheduleProgressPolling(token) {
  stopProgressPolling()
  progressTimer = setTimeout(() => pollProgress(token, true), 1000)
}

function formatRemaining(value) {
  const milliseconds = Number(value || 0)
  if (milliseconds <= 0) return '即将完成'
  const seconds = Math.ceil(milliseconds / 1000)
  if (seconds < 60) return `约 ${seconds} 秒`
  const minutes = Math.floor(seconds / 60)
  const remainder = seconds % 60
  return remainder ? `约 ${minutes} 分 ${remainder} 秒` : `约 ${minutes} 分钟`
}

function handleProgress(value, token, announce = false) {
  if (token !== viewToken || !value) return
  const previous = progress.value
  progress.value = value
  if (value.status === 'running') {
    error.value = ''
    loading.value = true
    scheduleProgressPolling(token)
    return
  }
  loading.value = false
  stopProgressPolling()
  if (value.status === 'succeeded' && value.report) {
    report.value = value.report
    saveHealthInspection(inspectionStorage, session, value.report)
    if (announce && previous?.status === 'running') UiMessage.success('设备健康巡检已完成')
  }
  if (value.status === 'failed') {
    error.value = value.error || '智能巡检失败，请稍后重试'
    if (announce) notifyError(new Error(error.value))
  }
}

async function pollProgress(token = viewToken, announce = true) {
  if (token !== viewToken) return
  try {
    const value = await api('/api/v1/ai/health-inspection/progress')
    handleProgress(value, token, announce)
  } catch (exception) {
    if (token !== viewToken) return
    stopProgressPolling()
    loading.value = false
    if (exception?.status === 404) {
      progress.value = null
      return
    }
    error.value = exception.message || '智能巡检进度读取失败'
    if (progress.value?.status === 'running') {
      loading.value = true
      scheduleProgressPolling(token)
    }
  }
}

async function loadProgress() {
  error.value = ''
  await pollProgress(viewToken, false)
}

async function run() {
  if (inspectionRunning.value) return
  error.value = ''
  progress.value = null
  loading.value = true
  stopProgressPolling()
  const token = viewToken
  try {
    const job = await api('/api/v1/ai/health-inspection/run', { method:'POST', body:'{}' })
    handleProgress(job, token)
  } catch (exception) {
    if (token !== viewToken) return
    loading.value = false
    error.value = exception.message || '智能巡检启动失败'
    notifyError(exception)
  }
}

async function downloadPDF() {
  if (!report.value || downloading.value || inspectionRunning.value) return
  downloading.value = true
  try {
    const stamp = new Date(Number(report.value.generatedAt || Date.now())).toISOString().replace(/[:.]/g, '-')
    await download('/api/v1/ai/health-inspection/pdf', `智能巡检结果_${stamp}.pdf`, { method:'POST', body:'{}' })
    UiMessage.success('巡检结果文档已下载')
  } catch (exception) {
    notifyError(exception)
  } finally {
    downloading.value = false
  }
}

onMounted(loadProgress)
onBeforeUnmount(() => {
  viewToken += 1
  stopProgressPolling()
})
</script>

<template>
  <div class="inspection-page">
    <header class="inspection-hero">
      <div class="inspection-hero-copy"><h2>设备健康巡检</h2><p>汇总设备运行情况，生成需要关注的设备与处置建议。</p><div class="inspection-scope"><span>在线状态</span><span>数据新鲜度</span><span>活动告警</span></div></div>
      <div class="inspection-hero-side"><small>手动启动检查，不会执行设备控制</small><div class="inspection-hero-actions"><ui-button v-permission="'POST /api/v1/ai/health-inspection/pdf'" v-if="report" :disabled="inspectionRunning" :loading="downloading" @click="downloadPDF">下载文档</ui-button><ui-button v-permission="'POST /api/v1/ai/health-inspection/run'" type="primary" :disabled="inspectionRunning" :loading="loading" @click="run">{{ inspectionRunning ? '巡检进行中' : '立即巡检' }}</ui-button></div></div>
    </header>

    <ui-alert v-if="error" class="top-gap" :title="error" type="error" :closable="false" show-icon />

    <ui-card v-if="progress" shadow="never" class="surface-card inspection-progress top-gap">
      <div class="inspection-progress-heading"><div><strong>{{ progress.message }}</strong><small v-if="progress.status === 'running'">任务在后台继续执行，切换页面后会自动恢复</small><small v-else-if="progress.status === 'succeeded'">本次巡检结果已保存，可下载文档</small></div><strong>{{ progressPercentage }}%</strong></div>
      <ui-progress :percentage="progressPercentage" :status="progressStatus" :stroke-width="10" :show-text="false" />
      <div class="inspection-progress-meta"><span v-if="progress.status === 'running'">预计剩余 {{ formatRemaining(progress.estimatedRemainingMs) }}</span><span v-else-if="progress.status === 'failed'">请检查模型服务和设备数据后重试</span><span v-else>已完成</span><span v-if="progress.updatedAt">更新时间：{{ formatTime(progress.updatedAt) }}</span></div>
    </ui-card>

    <ui-card shadow="never" class="surface-card top-gap">
      <ui-skeleton v-if="loading && !report" :rows="5" animated />
      <template v-else-if="report">
        <ui-alert :title="report.summary" type="info" :closable="false" show-icon />
        <div class="inspection-counts top-gap"><div><span>设备总数</span><strong>{{ counts.total || 0 }}</strong></div><div class="healthy"><span>状态正常</span><strong>{{ counts.healthy || 0 }}</strong></div><div class="attention"><span>需关注</span><strong>{{ counts.attention || 0 }}</strong></div><div class="critical"><span>高风险</span><strong>{{ counts.critical || 0 }}</strong></div><div class="offline"><span>离线/疑似离线</span><strong>{{ counts.offline || 0 }}</strong></div><div><span>活动告警</span><strong>{{ counts.activeAlarms || 0 }}</strong></div></div>
        <ui-card v-if="report.aiAdvice" shadow="never" class="inner-card top-gap"><template #header><strong>智能巡检建议</strong></template><MarkdownContent class="report-text" :source="report.aiAdvice" /></ui-card>
        <ui-alert v-for="warning in report.warnings || []" :key="warning" class="top-gap" :title="warning" type="warning" :closable="false" />
        <ui-table class="top-gap" :data="report.items || []" stripe><ui-table-column label="设备" min-width="190"><template #default="{row}"><b>{{ row.deviceName || row.deviceId }}</b><small class="subline">{{ row.deviceId }} · {{ row.productId }}</small></template></ui-table-column><ui-table-column label="业务状态" width="130"><template #default="{row}"><ui-tag :type="tagType(row.businessStatus)" round>{{ label(businessStatuses, row.businessStatus, row.businessStatus) }}</ui-tag></template></ui-table-column><ui-table-column label="最近上报" min-width="170"><template #default="{row}">{{ formatTime(row.lastSeenAt) }}</template></ui-table-column><ui-table-column label="活动告警" width="100"><template #default="{row}">{{ row.activeAlarmCount }}</template></ui-table-column><ui-table-column label="巡检结论" min-width="280"><template #default="{row}"><ui-tag :type="tagType(row.severity)" size="small" round>{{ row.severity }}</ui-tag><span class="inspection-findings">{{ (row.findings || []).join('；') }}</span></template></ui-table-column></ui-table>
      </template>
      <div v-else class="inspection-empty"><strong>尚无巡检结果</strong><p>点击上方“立即巡检”开始检查，结果会显示在这里。</p></div>
    </ui-card>
  </div>
</template>

<style scoped>
.inspection-counts { display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); gap: 9px; }
.inspection-counts > div { display: grid; align-content: space-between; min-height: 80px; padding: var(--space-3); background: var(--surface-hover); border-radius: var(--radius-lg); }
.inspection-counts span { color: var(--text-muted); font-size: var(--font-size-xs); }
.inspection-counts strong { color: var(--text-strong); font-size: 25px; line-height: 1; }
.inspection-counts .healthy { background: var(--success-soft); }
.inspection-counts .attention { background: var(--warning-soft); }
.inspection-counts .critical,
.inspection-counts .offline { background: var(--danger-soft); }
.inspection-findings { margin-left: var(--space-2); }
.report-text { color: var(--text-secondary); font-size: var(--font-size-sm); line-height: 1.75; white-space: pre-wrap; }
@media (max-width: 767px) {
  .inspection-counts { grid-template-columns: repeat(2, minmax(0, 1fr)); }
}
.inspection-page { min-width:0; }
.inspection-hero { box-sizing:border-box; width:100%; min-width:0; padding:19px 22px; display:flex; align-items:center; justify-content:space-between; gap:24px; background:var(--surface); border:1px solid var(--border); border-left:4px solid var(--primary); border-radius:10px; }
.inspection-hero-copy { min-width:0; }
.inspection-hero-copy h2 { margin:0; color:var(--text-strong); font-size:17px; line-height:1.4; }
.inspection-hero-copy p { margin:5px 0 10px; color:var(--text); font-size:13px; line-height:1.6; }
.inspection-scope { display:flex; flex-wrap:wrap; gap:7px; }
.inspection-scope span { padding:3px 9px; color:var(--primary); background:var(--surface-muted); border-radius:5px; font-size:12px; }
.inspection-hero-side { flex:none; display:grid; justify-items:end; gap:10px; }
.inspection-hero-side>small { color:var(--text); font-size:12px; }
.inspection-hero-actions { display:flex; align-items:center; gap:8px; }
.inspection-hero-actions :deep(.n-button) { min-width:112px; }
.inspection-hero-actions :deep(.n-button--primary-type) { min-width:132px; height:38px; }
.inspection-empty { min-height:122px; display:grid; align-content:center; justify-items:center; gap:6px; text-align:center; }
.inspection-empty strong { color:var(--text); font-size:14px; }
.inspection-empty p { margin:0; color:var(--text); font-size:13px; line-height:1.5; }
.inspection-progress { border-color:var(--info-border); background:var(--surface); }
.inspection-progress-heading { display:flex; align-items:flex-start; justify-content:space-between; gap:16px; margin-bottom:12px; }
.inspection-progress-heading>div { display:grid; gap:4px; }
.inspection-progress-heading strong { color:var(--primary); font-size:14px; }
.inspection-progress-heading>strong { font-size:20px; }
.inspection-progress-heading small,.inspection-progress-meta { color:var(--text); font-size:12px; line-height:1.5; }
.inspection-progress-meta { display:flex; justify-content:space-between; gap:12px; margin-top:8px; }
.inspection-findings { display:inline-block; margin-left:8px; color:var(--text); line-height:1.6; }
@media (max-width:760px) { .inspection-hero { align-items:stretch; flex-direction:column; gap:18px; padding:17px; }.inspection-hero-side { justify-items:stretch; }.inspection-hero-actions :deep(.n-button) { flex:1; min-width:0; } }
@media (max-width:560px) { .inspection-progress-meta { display:grid; gap:2px; } }
</style>
