<script setup>
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate'])
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, formatTime, notifyError, pretty } from '../api'
import { canAcknowledgeAlarm, canCloseAlarm } from '../alarmActions'
import { alarmNavigation, alarmQuery } from '../alarmNavigation'
import { alarmLevel, alarmLevels, alarmSources, alarmStatuses, alarmType, label, tagType } from '../labels'

const filters = reactive({ status:'', level:'', deviceId:'' })
const items = ref([])
const loading = ref(false)
const detail = ref(null)
const detailVisible = ref(false)
const analysis = ref(null)
const analysisLoading = ref(false)
const analysisProgress = ref(null)
const actionPending = reactive({})
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
let analysisPollTimer = 0
let analysisViewToken = 0

const progressPercent = computed(() => Math.max(0, Math.min(100, Number(analysisProgress.value?.progress || 0))))
const progressStatus = computed(() => analysisProgress.value?.status === 'failed' ? 'exception' : analysisProgress.value?.status === 'succeeded' ? 'success' : undefined)

async function load(resetPage = false) {
  if (resetPage) page.value = 1
  loading.value = true
  try {
    const q = alarmQuery(filters, page.value, pageSize.value)
    const d = await api('/api/v1/alarms?' + q)
    items.value = d.items || []
    total.value = Number(d.total ?? d.count ?? items.value.length)
  } catch (e) {
    notifyError(e)
  } finally {
    loading.value = false
  }
}

function changePage(value) { page.value = value; load() }
function changePageSize(value) { pageSize.value = value; page.value = 1; load() }

function stopAnalysisPolling() {
  if (analysisPollTimer) window.clearTimeout(analysisPollTimer)
  analysisPollTimer = 0
}

function handleDetailClosed() {
  analysisViewToken += 1
  stopAnalysisPolling()
  analysisLoading.value = false
  analysisProgress.value = null
}

function formatRemaining(ms) {
  const seconds = Math.ceil(Number(ms || 0) / 1000)
  if (seconds <= 0) return '即将完成'
  if (seconds < 60) return `预计还需约 ${seconds} 秒`
  return `预计还需约 ${Math.ceil(seconds / 60)} 分钟`
}

async function pollAnalysis(jobId, alarmId, viewToken = analysisViewToken) {
  if (viewToken !== analysisViewToken || !detailVisible.value) return
  try {
    const progress = await api(`/api/v1/ai/alarm-analysis/${encodeURIComponent(alarmId)}/progress/${encodeURIComponent(jobId)}`)
    if (viewToken !== analysisViewToken || !detailVisible.value) return
    analysisProgress.value = progress
    if (progress.status === 'succeeded') {
      analysis.value = progress.analysis || null
      analysisLoading.value = false
      ElMessage.success('智能研判已完成')
      return
    }
    if (progress.status === 'failed') {
      analysisLoading.value = false
      notifyError(progress.error || '智能研判失败')
      return
    }
    analysisPollTimer = window.setTimeout(() => { void pollAnalysis(jobId, alarmId, viewToken) }, 800)
  } catch (error) {
    if (viewToken !== analysisViewToken || !detailVisible.value) return
    analysisLoading.value = false
    stopAnalysisPolling()
    notifyError(error)
  }
}

async function show(id) {
  const viewToken = ++analysisViewToken
  stopAnalysisPolling()
  analysisLoading.value = false
  analysisProgress.value = null
  try {
    detail.value = await api(`/api/v1/alarms/${encodeURIComponent(id)}`)
    analysis.value = null
    detailVisible.value = true
    const [savedResult, progressResult] = await Promise.allSettled([
      api(`/api/v1/ai/alarm-analysis/${encodeURIComponent(id)}`),
      api(`/api/v1/ai/alarm-analysis/${encodeURIComponent(id)}/progress`)
    ])
    if (viewToken !== analysisViewToken || !detailVisible.value) return
    analysis.value = savedResult.status === 'fulfilled' ? savedResult.value : null
    if (progressResult.status !== 'fulfilled') return
    analysisProgress.value = progressResult.value
    if (progressResult.value.status === 'running') {
      analysisLoading.value = true
      void pollAnalysis(progressResult.value.jobId, id, viewToken)
    } else if (progressResult.value.status === 'succeeded') {
      analysis.value = progressResult.value.analysis || analysis.value
      analysisLoading.value = false
    } else {
      analysisLoading.value = false
    }
  } catch (e) {
    notifyError(e)
  }
}

async function runAnalysis() {
  if (!detail.value || analysisLoading.value) return
  const viewToken = analysisViewToken
  stopAnalysisPolling()
  analysisLoading.value = true
  analysisProgress.value = { status:'running', stage:'preparing', message:'正在准备告警上下文', progress:5, estimatedRemainingMs:45000 }
  try {
    const job = await api(`/api/v1/ai/alarm-analysis/${encodeURIComponent(detail.value.alarmId)}/run`, { method:'POST', body:'{}' })
    if (viewToken !== analysisViewToken || !detailVisible.value) return
    analysisProgress.value = job
    if (job.status === 'succeeded') {
      analysis.value = job.analysis || null
      analysisLoading.value = false
      ElMessage.success('智能研判已完成')
      return
    }
    await pollAnalysis(job.jobId, detail.value.alarmId, viewToken)
  } catch (e) {
    if (viewToken !== analysisViewToken || !detailVisible.value) return
    analysisLoading.value = false
    notifyError(e)
  }
}

async function action(id, value) {
  if (actionPending[id]) return
  actionPending[id] = value
  try {
    const updated = await api(`/api/v1/alarms/${encodeURIComponent(id)}/actions`, { method:'POST', body:JSON.stringify({ action:value }) })
    const row = items.value.find(item => item.alarmId === id)
    if (row && updated?.status) row.status = updated.status
    ElMessage.success('操作成功')
    await load()
  } catch (e) {
    notifyError(e)
  } finally {
    delete actionPending[id]
  }
}

const realtime = () => load()
onMounted(async () => {
  const navigation = alarmNavigation(sessionStorage.getItem('iot:navigation-detail'))
  sessionStorage.removeItem('iot:navigation-detail')
  filters.deviceId = navigation.deviceId
  window.addEventListener('iot:realtime', realtime)
  await load()
  if (navigation.alarmId) await show(navigation.alarmId)
})
onBeforeUnmount(() => {
  analysisViewToken += 1
  stopAnalysisPolling()
  window.removeEventListener('iot:realtime', realtime)
})
</script>

<template>
  <div class="page-toolbar">
    <el-input v-model="filters.deviceId" clearable placeholder="按设备标识筛选" aria-label="设备标识筛选" @keyup.enter="load(true)" @clear="load(true)" />
    <el-select v-model="filters.status" clearable placeholder="全部状态" @change="load(true)"><el-option v-for="(text,key) in alarmStatuses" :key="key" :label="text" :value="key" /></el-select>
    <el-select v-model="filters.level" clearable placeholder="全部等级" @change="load(true)"><el-option v-for="(text,key) in alarmLevels" :key="key" :label="text" :value="key" /></el-select>
    <el-button type="primary" :loading="loading" @click="load()">刷新告警</el-button>
    <el-button :disabled="!filters.status && !filters.level && !filters.deviceId" @click="filters.status = ''; filters.level = ''; filters.deviceId = ''; load(true)">重置筛选</el-button>
    <span>共 {{total}} 条告警</span>
  </div>

  <el-card shadow="never" class="surface-card table-card">
    <el-table v-loading="loading" :data="items" stripe>
      <el-table-column label="时间" min-width="170"><template #default="{row}">{{formatTime(row.lastTriggeredAt)}}</template></el-table-column>
      <el-table-column label="设备 / 来源" min-width="190"><template #default="{row}"><b>{{row.deviceName||row.deviceId}}</b><small class="subline">{{label(alarmSources,row.source,'其他来源')}}</small></template></el-table-column>
      <el-table-column label="告警类型" min-width="150"><template #default="{row}">{{alarmType(row.alarmType)}}</template></el-table-column>
      <el-table-column label="等级" width="90"><template #default="{row}"><el-tag :type="tagType(row.alarmLevel)" round>{{label(alarmLevels,row.alarmLevel,'未设置')}}</el-tag></template></el-table-column>
      <el-table-column label="状态" width="100"><template #default="{row}"><el-tag :type="tagType(row.status)" round>{{label(alarmStatuses,row.status)}}</el-tag></template></el-table-column>
      <el-table-column label="操作" fixed="right" width="310" align="center"><template #default="{row}"><div class="table-actions"><el-button plain type="primary" @click="show(row.alarmId)">查看详情</el-button><template v-if="['ACTIVE','ACKED'].includes(row.status)"><el-button v-if="canAcknowledgeAlarm(row.status)" :loading="actionPending[row.alarmId] === 'ACKED'" :disabled="Boolean(actionPending[row.alarmId])" plain type="warning" @click="action(row.alarmId,'ACKED')">确认告警</el-button><el-button v-if="canCloseAlarm(row.status)" :loading="actionPending[row.alarmId] === 'CLOSED'" :disabled="Boolean(actionPending[row.alarmId])" plain type="danger" @click="action(row.alarmId,'CLOSED')">关闭告警</el-button></template></div></template></el-table-column>
    </el-table>
    <div class="list-pagination"><el-pagination v-model:current-page="page" v-model:page-size="pageSize" :total="total" :page-sizes="[20,50,100]" layout="total, sizes, prev, pager, next, jumper" @current-change="changePage" @size-change="changePageSize" /></div>
  </el-card>

  <el-dialog v-model="detailVisible" title="告警详情" width="min(760px, 94vw)" @closed="handleDetailClosed">
    <el-descriptions v-if="detail" :column="1" border>
      <el-descriptions-item label="告警编号">{{detail.alarmId}}</el-descriptions-item><el-descriptions-item label="设备">{{detail.deviceName||detail.deviceId}}</el-descriptions-item><el-descriptions-item label="告警类型">{{alarmType(detail.alarmType)}}</el-descriptions-item><el-descriptions-item label="等级 / 状态"><el-tag :type="tagType(detail.alarmLevel)">{{label(alarmLevels,detail.alarmLevel)}}</el-tag> {{label(alarmStatuses,detail.status)}}</el-descriptions-item><el-descriptions-item label="来源">{{label(alarmSources,detail.source,'其他来源')}}</el-descriptions-item><el-descriptions-item label="首次发生">{{formatTime(detail.firstTriggeredAt)}}</el-descriptions-item><el-descriptions-item label="最后发生">{{formatTime(detail.lastTriggeredAt)}}</el-descriptions-item><el-descriptions-item label="触发次数">{{detail.triggerCount}}</el-descriptions-item>
    </el-descriptions>
    <el-card shadow="never" class="top-gap">
      <template #header><div class="card-header"><strong>智能自动研判</strong><el-button size="small" type="primary" :loading="analysisLoading" :disabled="analysisLoading" @click="runAnalysis">{{analysisLoading ? '研判中…' : analysis ? '重新研判' : '立即研判'}}</el-button></div></template>
      <div v-if="analysisProgress" class="analysis-progress" aria-live="polite">
        <div class="analysis-progress-heading"><strong>{{analysisProgress.message || '智能正在处理'}}</strong><span>{{progressPercent}}%</span></div>
        <el-progress :percentage="progressPercent" :status="progressStatus" :stroke-width="10" />
        <small v-if="analysisProgress.status === 'running'">{{formatRemaining(analysisProgress.estimatedRemainingMs)}}</small>
        <small v-else>{{analysisProgress.status === 'succeeded' ? '处理完成' : analysisProgress.error || '处理失败'}}</small>
      </div>
      <el-empty v-if="!analysis && !analysisLoading" description="该告警暂无研判结果，可点击立即研判" :image-size="52" />
      <div v-if="analysis" class="analysis-grid"><el-alert :title="analysis.summary||'智能未返回摘要'" :type="tagType(analysis.riskLevel)==='danger'?'error':'warning'" :closable="false" show-icon /><div><strong>风险等级：</strong>{{alarmLevel(analysis.riskLevel)}} <span class="subline">置信度 {{Number(analysis.confidence||0).toFixed(2)}}</span></div><div v-if="analysis.possibleReasons?.length"><strong>可能原因</strong><ul><li v-for="item in analysis.possibleReasons" :key="item">{{item}}</li></ul></div><div v-if="analysis.suggestions?.length"><strong>建议处置</strong><ul><li v-for="item in analysis.suggestions" :key="item">{{item}}</li></ul></div><small class="subline">模型：{{analysis.model||'—'}} · 生成时间：{{formatTime(analysis.createdAt)}}</small></div>
    </el-card>
    <pre>{{pretty(detail)}}</pre>
    <template #footer><el-button @click="detailVisible = false">关闭详情</el-button></template>
  </el-dialog>
</template>

<style scoped>
.analysis-progress { margin: 12px 0; padding: 12px; background: #f5f9ff; border: 1px solid #d6e8ff; border-radius: 5px; }
.analysis-progress-heading { display:flex; justify-content:space-between; gap:12px; margin-bottom:7px; color:#1554ad; font-size:12px; }
.analysis-progress-heading span { color:#1677ff; font-weight:700; }
.analysis-progress small { display:block; margin-top:7px; color:#697386; font-size:10px; }
</style>
