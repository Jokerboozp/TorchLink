<script setup>
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate'])
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { UiMessage } from '../ui/feedback.js'
import { api, formatTime, notifyError, pretty } from '../api'
import { confirmDelete } from '../deleteAction'
import { canAcknowledgeAlarm, canCloseAlarm } from '../alarmActions'
import { alarmNavigation, alarmQuery } from '../alarmNavigation'
import { alarmLevel, alarmLevels, alarmSources, alarmStatuses, alarmType, label, tagType } from '../labels'
import { RefreshCw } from '@lucide/vue'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'

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
let loadVersion = 0

const progressPercent = computed(() => Math.max(0, Math.min(100, Number(analysisProgress.value?.progress || 0))))
const progressStatus = computed(() => analysisProgress.value?.status === 'failed' ? 'exception' : analysisProgress.value?.status === 'succeeded' ? 'success' : undefined)

async function load(resetPage = false) {
  const version = ++loadVersion
  if (resetPage) page.value = 1
  loading.value = true
  try {
    const q = alarmQuery(filters, page.value, pageSize.value)
    const d = await api('/api/v1/alarms?' + q)
    if (version !== loadVersion) return
    items.value = d.items || []
    total.value = Number(d.total ?? d.count ?? items.value.length)
  } catch (e) {
    if (version === loadVersion) notifyError(e)
  } finally {
    if (version === loadVersion) loading.value = false
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

// 研判结果按知识范围分开保存，这里说明当前显示的结果依据了哪些知识。
function analysisKnowledgeText(item) {
  if (item?.knowledgeScope === 'alarm-handler') {
    const count = item.knowledgeDocuments?.length || 0
    return count ? `知识依据：告警研判智能体知识库，引用 ${count} 篇文档` : '知识依据：告警研判智能体知识库，未检索到匹配内容'
  }
  if (item?.knowledgeScope === 'legacy-tenant-knowledge') return '知识依据：早期结果，曾检索全租户知识库'
  return '知识依据：未引用知识库'
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
      UiMessage.success('智能研判已完成')
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
      UiMessage.success('智能研判已完成')
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
    UiMessage.success('操作成功')
    await load()
  } catch (e) {
    notifyError(e)
  } finally {
    delete actionPending[id]
  }
}
function removeAlarm(row) { return confirmDelete({ label:row.alarmType || row.alarmId, path:`/api/v1/alarms/${encodeURIComponent(row.alarmId)}`, onDeleted:load, warning:'告警记录和研判结果将一并清理；活动告警请先关闭。' }) }

let realtimeTimer = 0
const realtime = event => {
  if (!event?.detail?.topic?.includes('/alarm/') || realtimeTimer) return
  realtimeTimer = window.setTimeout(() => { realtimeTimer = 0; void load() }, 300)
}
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
  window.clearTimeout(realtimeTimer)
  window.removeEventListener('iot:realtime', realtime)
})
const statusTone = value => ({ danger:'danger', warning:'warning', success:'success', info:'info' })[tagType(value)] || 'neutral'
const filtered = computed(() => Boolean(filters.status || filters.level || filters.deviceId))
function resetFilters() { filters.status = ''; filters.level = ''; filters.deviceId = ''; load(true) }
function rowActions(row) {
  const open = ['ACTIVE','ACKED'].includes(row.status)
  return [
    { key:'detail', label:'查看详情', onClick:() => show(row.alarmId) },
    { key:'ack', label:'确认告警', permission:'POST /api/v1/alarms/:id/actions', hidden:!open || !canAcknowledgeAlarm(row.status), loading:actionPending[row.alarmId] === 'ACKED', disabled:Boolean(actionPending[row.alarmId]), onClick:() => action(row.alarmId,'ACKED') },
    { key:'close', label:'关闭告警', type:'danger', permission:'POST /api/v1/alarms/:id/actions', hidden:!open || !canCloseAlarm(row.status), loading:actionPending[row.alarmId] === 'CLOSED', disabled:Boolean(actionPending[row.alarmId]), onClick:() => action(row.alarmId,'CLOSED') },
    { key:'delete', label:'删除', type:'danger', permission:'DELETE /api/v1/alarms/:id', hidden:open, onClick:() => removeAlarm(row) }
  ]
}
</script>

<template>
  <FilterBar>
    <ui-input v-model="filters.deviceId" clearable placeholder="按设备标识筛选" aria-label="设备标识筛选" @keyup.enter="load(true)" @clear="load(true)" />
    <ui-select v-model="filters.status" clearable placeholder="全部状态" aria-label="告警状态" @change="load(true)"><ui-option v-for="(text,key) in alarmStatuses" :key="key" :label="text" :value="key" /></ui-select>
    <ui-select v-model="filters.level" clearable placeholder="全部等级" aria-label="告警等级" @change="load(true)"><ui-option v-for="(text,key) in alarmLevels" :key="key" :label="text" :value="key" /></ui-select>
    <ui-button v-if="filtered" text @click="resetFilters">重置筛选</ui-button>
    <template #actions><ui-button :loading="loading" @click="load()"><RefreshCw />刷新</ui-button></template>
  </FilterBar>

  <DataTableCard :title="`告警 · ${total} 条`" :page="page" :page-size="pageSize" :total="total" @update:page="changePage" @update:page-size="changePageSize">
    <ui-table v-loading="loading" :data="items" :empty-text="filtered ? '没有符合筛选条件的告警' : '暂无告警'">
      <ui-table-column label="时间" min-width="170"><template #default="{row}">{{formatTime(row.lastTriggeredAt)}}</template></ui-table-column>
      <ui-table-column label="设备 / 来源" min-width="190"><template #default="{row}"><b>{{row.deviceName||row.deviceId}}</b><small v-if="row.componentId" class="subline">{{row.componentName||row.componentId}} · {{row.componentLocation||row.componentId}}</small><small class="subline">{{label(alarmSources,row.source,'其他来源')}}</small></template></ui-table-column>
      <ui-table-column label="告警类型" min-width="150"><template #default="{row}">{{alarmType(row.alarmType)}}</template></ui-table-column>
      <ui-table-column label="等级" width="90"><template #default="{row}"><ui-tag :type="tagType(row.alarmLevel)" round>{{label(alarmLevels,row.alarmLevel,'未设置')}}</ui-tag></template></ui-table-column>
      <ui-table-column label="状态" width="100"><template #default="{row}"><StatusDot :tone="statusTone(row.status)" :label="label(alarmStatuses,row.status)" /></template></ui-table-column>
      <ui-table-column label="操作" fixed="right" width="200" align="right"><template #default="{row}"><RowActions :actions="rowActions(row)" /></template></ui-table-column>
    </ui-table>
  </DataTableCard>

  <ui-dialog v-model="detailVisible" class="alarm-detail-dialog" title="告警详情" width="min(760px, 94vw)" @closed="handleDetailClosed"> <!-- 告警详情的长报文跟随弹窗正文统一滚动。 -->
    <ui-descriptions v-if="detail" :column="1" border>
      <ui-descriptions-item label="告警编号">{{detail.alarmId}}</ui-descriptions-item><ui-descriptions-item label="设备">{{detail.deviceName||detail.deviceId}}</ui-descriptions-item><ui-descriptions-item v-if="detail.componentId" label="部件">{{detail.componentName||detail.componentId}}（{{detail.componentId}}）</ui-descriptions-item><ui-descriptions-item v-if="detail.componentLocation" label="部件位置">{{detail.componentLocation}}</ui-descriptions-item><ui-descriptions-item label="告警类型">{{alarmType(detail.alarmType)}}</ui-descriptions-item><ui-descriptions-item label="等级 / 状态"><ui-tag :type="tagType(detail.alarmLevel)">{{label(alarmLevels,detail.alarmLevel)}}</ui-tag> {{label(alarmStatuses,detail.status)}}</ui-descriptions-item><ui-descriptions-item label="来源">{{label(alarmSources,detail.source,'其他来源')}}</ui-descriptions-item><ui-descriptions-item label="首次发生">{{formatTime(detail.firstTriggeredAt)}}</ui-descriptions-item><ui-descriptions-item label="最后发生">{{formatTime(detail.lastTriggeredAt)}}</ui-descriptions-item><ui-descriptions-item label="触发次数">{{detail.triggerCount}}</ui-descriptions-item>
    </ui-descriptions>
    <ui-card shadow="never" class="top-gap">
      <template #header><div class="card-header"><strong>智能自动研判</strong><ui-button v-permission="'POST /api/v1/ai/alarm-analysis'" size="small" type="primary" :loading="analysisLoading" :disabled="analysisLoading" @click="runAnalysis">{{analysisLoading ? '研判中…' : analysis ? '重新研判' : '立即研判'}}</ui-button></div></template>
      <div v-if="analysisProgress" class="analysis-progress" aria-live="polite">
        <div class="analysis-progress-heading"><strong>{{analysisProgress.message || '智能正在处理'}}</strong><span>{{progressPercent}}%</span></div>
        <ui-progress :percentage="progressPercent" :status="progressStatus" :stroke-width="10" />
        <small v-if="analysisProgress.status === 'running'">{{formatRemaining(analysisProgress.estimatedRemainingMs)}}</small>
        <small v-else>{{analysisProgress.status === 'succeeded' ? '处理完成' : analysisProgress.error || '处理失败'}}</small>
      </div>
      <ui-empty v-if="!analysis && !analysisLoading" description="该告警暂无研判结果，可点击立即研判" :image-size="52" />
      <div v-if="analysis" class="analysis-grid"><ui-alert :title="analysis.summary||'智能未返回摘要'" :type="tagType(analysis.riskLevel)==='danger'?'error':'warning'" :closable="false" show-icon /><div><strong>风险等级：</strong>{{alarmLevel(analysis.riskLevel)}} <span class="subline">置信度 {{Number(analysis.confidence||0).toFixed(2)}}</span></div><div v-if="analysis.possibleReasons?.length"><strong>可能原因</strong><ul><li v-for="item in analysis.possibleReasons" :key="item">{{item}}</li></ul></div><div v-if="analysis.suggestions?.length"><strong>建议处置</strong><ul><li v-for="item in analysis.suggestions" :key="item">{{item}}</li></ul></div><small class="subline">{{analysisKnowledgeText(analysis)}}</small><small class="subline">模型：{{analysis.model||'—'}} · 生成时间：{{formatTime(analysis.createdAt)}}</small></div>
    </ui-card>
    <pre>{{pretty(detail)}}</pre>
    <template #footer><ui-button @click="detailVisible = false">关闭详情</ui-button></template>
  </ui-dialog>
</template>

<style scoped>
.analysis-grid { display: grid; gap: 9px; line-height: 1.65; }
.analysis-grid :deep(ul) { margin: 5px 0 0; padding-left: 20px; color: var(--text-muted); }
.analysis-progress { margin: 12px 0; padding: 12px; background: var(--surface-muted); border: 1px solid var(--info-border); border-radius: 5px; }
.analysis-progress-heading { display:flex; justify-content:space-between; gap:12px; margin-bottom:7px; color:var(--primary); font-size:13px; }
.analysis-progress-heading span { color:var(--primary); font-weight:700; }
.analysis-progress small { display:block; margin-top:7px; color:var(--text); font-size:12px; }
</style>
