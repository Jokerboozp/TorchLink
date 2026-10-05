<script setup>
import { takeNavigation } from '../routing'
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate'])
import { computed, onBeforeUnmount, onMounted, reactive, ref, toRef } from 'vue'
import { UiMessage } from '../ui/feedback.js'
import { api, download, formatTime, isAbort, notifyError, pretty } from '../api'
import { useListLoader } from '../composables/useListLoader'
import { usePageState } from '../composables/usePageState.js'
import { confirmClose } from '../composables/unsavedGuard.js'
import DeviceFilterSelect from '../components/DeviceFilterSelect.vue'
import { confirmDelete } from '../deleteAction'
import { canAcknowledgeAlarm, canCloseAlarm } from '../alarmActions'
import { alarmNavigation, alarmQuery } from '../alarmNavigation'
import {
  alarmLevel,
  alarmLevels,
  alarmSources,
  alarmStatuses,
  alarmType,
  dispositionResults,
  label,
  requiresVerification,
  tagType
} from '../labels'
import { Download, FileText, RefreshCw } from '@lucide/vue'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'
import LinkedCameras from '../components/LinkedCameras.vue'
import AlarmNotifications from '../components/AlarmNotifications.vue'
import AlarmMediaPanel from '../components/AlarmMediaPanel.vue'
import AlarmDisposition from '../components/AlarmDisposition.vue'
import AlarmAttachments from '../components/AlarmAttachments.vue'
import AlarmLocation from '../components/AlarmLocation.vue'
import AiAnalysisQuality from '../components/AiAnalysisQuality.vue'

const filters = reactive({ status: '', level: '', deviceId: '' })
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
const mediaRefresh = useListLoader()
const loader = useListLoader(loading)
const statisticsLoader = useListLoader()
const loadError = ref('')
const opening = ref('')
const dispositionPanel = ref(null)
// 筛选、页码写入地址和会话存储，刷新或切换菜单回来后保持。
usePageState('alarms', {
  status: toRef(filters, 'status'),
  level: toRef(filters, 'level'),
  deviceId: toRef(filters, 'deviceId'),
  page,
  pageSize
})

// 进度与剩余时间只显示服务端返回的值；提交后、服务端回复前不显示估算。
const hasProgress = computed(() => Number.isFinite(Number(analysisProgress.value?.progress)) && analysisProgress.value?.progress != null)
const progressPercent = computed(() => Math.max(0, Math.min(100, Number(analysisProgress.value?.progress || 0))))
const progressStatus = computed(() =>
  analysisProgress.value?.status === 'failed' ? 'exception' : analysisProgress.value?.status === 'succeeded' ? 'success' : undefined
)

async function load(resetPage = false) {
  if (resetPage) page.value = 1
  try {
    const q = alarmQuery(filters, page.value, pageSize.value)
    const d = await loader.run(signal => api('/api/v1/alarms?' + q, { signal }))
    items.value = d.items || []
    total.value = Number(d.total ?? d.count ?? items.value.length)
    loadError.value = ''
    void loadStatistics()
  } catch (e) {
    if (!isAbort(e)) loadError.value = e?.message || '告警读取失败'
  }
}

// 近 30 天核实统计，与列表使用同一组筛选条件。
const statistics = ref(null)
const statisticsError = ref(false)
const exporting = ref(false)
const reporting = ref(false)
// 月报默认上一个月（北京时间）。
const reportMonth = ref(
  (() => {
    const d = new Date(Date.now() + 8 * 3600_000)
    d.setUTCDate(1)
    d.setUTCMonth(d.getUTCMonth() - 1)
    return d.toISOString().slice(0, 7)
  })()
)
function reportQuery() {
  const q = new URLSearchParams()
  for (const key of ['status', 'level', 'deviceId']) if (filters[key]) q.set(key, filters[key])
  return q.toString()
}
async function loadStatistics() {
  try {
    statistics.value = await statisticsLoader.run(signal => api('/api/v1/alarms/statistics/disposition?' + reportQuery(), { signal }))
    statisticsError.value = false
  } catch (error) {
    if (isAbort(error)) return
    statistics.value = null
    statisticsError.value = true
  }
}
const formatDuration = ms => {
  const minutes = Math.round(Number(ms || 0) / 60000)
  if (!ms) return '—'
  if (minutes < 1) return '不足 1 分钟'
  return minutes < 60 ? `${minutes} 分钟` : `${(minutes / 60).toFixed(1)} 小时`
}
async function downloadMonthly() {
  if (!reportMonth.value) return
  reporting.value = true
  try {
    await download('/api/v1/alarms/reports/monthly?month=' + encodeURIComponent(reportMonth.value), `告警月报-${reportMonth.value}.pdf`)
  } catch (e) {
    notifyError(e)
  } finally {
    reporting.value = false
  }
}
async function exportAlarms() {
  exporting.value = true
  try {
    await download('/api/v1/alarms/export?' + reportQuery(), `告警导出-${new Date().toISOString().slice(0, 10)}.csv`)
  } catch (e) {
    notifyError(e)
  } finally {
    exporting.value = false
  }
}
function dispositionUpdated(updated) {
  if (updated && detail.value?.alarmId === updated.alarmId) detail.value = { ...detail.value, ...updated }
  void load()
}

function changePage(value) {
  page.value = value
  load()
}
function changePageSize(value) {
  pageSize.value = value
  page.value = 1
  load()
}

function stopAnalysisPolling() {
  if (analysisPollTimer) window.clearTimeout(analysisPollTimer)
  analysisPollTimer = 0
}

async function setDetailVisible(value) {
  if (!value && !(await confirmClose(dispositionPanel.value?.dirty?.(), '核实结论还未保存，关闭后将丢失。确定关闭？'))) return
  detailVisible.value = value
}
const detailOpen = computed(() => ['ACTIVE', 'ACKED'].includes(detail.value?.status))
const riskAlertType = value => ({ danger: 'error', warning: 'warning', success: 'success' })[tagType(value)] || 'info'

function handleDetailClosed() {
  // 关闭从深链接打开的详情后，地址回到告警列表。
  if (typeof window !== 'undefined' && window.location.pathname.startsWith('/alarms/')) window.history.replaceState(null, '', '/alarms')
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
  if (ms == null) return '研判进行中'
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
    analysisPollTimer = window.setTimeout(() => {
      void pollAnalysis(jobId, alarmId, viewToken)
    }, 800)
  } catch (error) {
    if (viewToken !== analysisViewToken || !detailVisible.value) return
    analysisLoading.value = false
    stopAnalysisPolling()
    notifyError(error)
  }
}

async function show(id) {
  if (opening.value === id) return
  opening.value = id
  try {
    await openDetail(id)
  } finally {
    if (opening.value === id) opening.value = ''
  }
}
async function openDetail(id) {
  const viewToken = ++analysisViewToken
  stopAnalysisPolling()
  analysisLoading.value = false
  analysisProgress.value = null
  try {
    const loaded = await api(`/api/v1/alarms/${encodeURIComponent(id)}`)
    if (viewToken !== analysisViewToken) return
    detail.value = loaded
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
    if (viewToken === analysisViewToken) notifyError(e)
  }
}

async function refreshMediaDetail() {
  const alarmId = detail.value?.alarmId,
    viewToken = analysisViewToken
  if (!alarmId || !detailVisible.value) return mediaRefresh.cancel()
  try {
    const loaded = await mediaRefresh.run(signal => api(`/api/v1/alarms/${encodeURIComponent(alarmId)}`, { signal }))
    if (viewToken === analysisViewToken && detailVisible.value && detail.value?.alarmId === alarmId) detail.value = loaded
  } catch {
    /* Attachment polling retains the last known detail; explicit reads report errors. */
  }
}

async function runAnalysis() {
  if (!detail.value || analysisLoading.value) return
  const viewToken = analysisViewToken
  stopAnalysisPolling()
  analysisLoading.value = true
  analysisProgress.value = { status: 'running', stage: 'submitting', message: '正在提交研判任务' }
  try {
    const job = await api(`/api/v1/ai/alarm-analysis/${encodeURIComponent(detail.value.alarmId)}/run`, { method: 'POST', body: '{}' })
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
  const inDetail = detailVisible.value && detail.value?.alarmId === id
  const row = items.value.find(item => item.alarmId === id) || (inDetail ? detail.value : null)
  const target = inDetail ? detail.value : row
  if (value === 'CLOSED' && target && !target.disposition && requiresVerification(target)) {
    UiMessage.warning('火警及紧急告警须先填写核实结论再关闭')
    if (!inDetail) await show(id)
    return
  }
  if (value === 'CLOSED' && inDetail && dispositionPanel.value?.dirty?.()) {
    UiMessage.warning('核实结论还未保存，请先保存再关闭告警')
    return
  }
  actionPending[id] = value
  try {
    const updated = await api(`/api/v1/alarms/${encodeURIComponent(id)}/actions`, {
      method: 'POST',
      body: JSON.stringify({ action: value })
    })
    if (row && updated?.status) row.status = updated.status
    if (inDetail && detail.value?.alarmId === id)
      detail.value = { ...detail.value, ...(updated?.alarmId ? updated : { status: updated?.status || value }) }
    UiMessage.success(value === 'CLOSED' ? '告警已关闭' : '告警已确认')
    await load()
  } catch (e) {
    notifyError(e)
  } finally {
    delete actionPending[id]
  }
}
function removeAlarm(row) {
  return confirmDelete({
    label: `${row.deviceName || row.deviceId} · ${alarmType(row.alarmType)}`,
    path: `/api/v1/alarms/${encodeURIComponent(row.alarmId)}`,
    onDeleted: load,
    warning: '告警记录和研判结果将一并清理；活动告警请先关闭。'
  })
}

let realtimeTimer = 0
const realtime = event => {
  if ((!event?.detail?.topic?.includes('/alarm/') && !event?.detail?.topic?.includes('/snapshot/refresh/')) || realtimeTimer) return
  const alarmId = (() => {
    try {
      return JSON.parse(event.detail.payload || '{}')?.alarmId || ''
    } catch {
      return ''
    }
  })()
  // 打开中的详情被其他人确认或关闭时同步状态；核实表单的未保存输入不受影响。
  const refreshDetail = detailVisible.value && (!alarmId || alarmId === detail.value?.alarmId)
  realtimeTimer = window.setTimeout(() => {
    realtimeTimer = 0
    void load()
    if (refreshDetail) void refreshMediaDetail()
  }, 300)
}
onMounted(async () => {
  const navigation = alarmNavigation(takeNavigation())
  // 从设备等页面带入的设备优先于上次保存的筛选。
  if (navigation.deviceId) {
    filters.deviceId = navigation.deviceId
    page.value = 1
  }
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
const statusTone = value => ({ danger: 'danger', warning: 'warning', success: 'success', info: 'info' })[tagType(value)] || 'neutral'
const filtered = computed(() => Boolean(filters.status || filters.level || filters.deviceId))
function resetFilters() {
  filters.status = ''
  filters.level = ''
  filters.deviceId = ''
  load(true)
}
function rowActions(row) {
  const open = ['ACTIVE', 'ACKED'].includes(row.status)
  return [
    { key: 'detail', label: '查看详情', loading: opening.value === row.alarmId, onClick: () => show(row.alarmId) },
    {
      key: 'ack',
      label: '确认告警',
      permission: 'POST /api/v1/alarms/:id/actions',
      hidden: !open || !canAcknowledgeAlarm(row.status),
      loading: actionPending[row.alarmId] === 'ACKED',
      disabled: Boolean(actionPending[row.alarmId]),
      onClick: () => action(row.alarmId, 'ACKED')
    },
    {
      key: 'close',
      label: '关闭告警',
      type: 'danger',
      permission: 'POST /api/v1/alarms/:id/actions',
      hidden: !open || !canCloseAlarm(row.status),
      loading: actionPending[row.alarmId] === 'CLOSED',
      disabled: Boolean(actionPending[row.alarmId]),
      onClick: () => action(row.alarmId, 'CLOSED')
    },
    { key: 'delete', label: '删除', type: 'danger', permission: 'DELETE /api/v1/alarms/:id', hidden: open, onClick: () => removeAlarm(row) }
  ]
}
</script>

<template>
  <FilterBar>
    <DeviceFilterSelect v-model="filters.deviceId" @change="load(true)" />
    <ui-select v-model="filters.status" clearable placeholder="全部状态" aria-label="告警状态" @change="load(true)"
      ><ui-option v-for="(text, key) in alarmStatuses" :key="key" :label="text" :value="key"
    /></ui-select>
    <ui-select v-model="filters.level" clearable placeholder="全部等级" aria-label="告警等级" @change="load(true)"
      ><ui-option v-for="(text, key) in alarmLevels" :key="key" :label="text" :value="key"
    /></ui-select>
    <ui-button v-if="filtered" text @click="resetFilters">重置筛选</ui-button>
    <template #actions
      ><span v-permission="'GET /api/v1/alarms/reports/monthly'" class="monthly-report"
        ><ui-month v-model="reportMonth" aria-label="月报月份" placeholder="选择月份" /><ui-button
          :loading="reporting"
          :disabled="!reportMonth"
          @click="downloadMonthly"
          ><FileText />下载月报</ui-button
        ></span
      ><ui-button v-permission="'GET /api/v1/alarms/export'" :loading="exporting" @click="exportAlarms"><Download />导出近 30 天</ui-button
      ><ui-button :loading="loading" @click="load()"><RefreshCw />刷新</ui-button></template
    >
  </FilterBar>
  <div v-if="statistics" class="alarm-stats" aria-label="近 30 天核实统计">
    <div>
      <span>近 30 天告警</span><strong>{{ statistics.total }}</strong>
    </div>
    <div>
      <span>待核实火警</span><strong :class="{ danger: statistics.unverified > 0 }">{{ statistics.unverified }}</strong>
    </div>
    <div>
      <span>误报率</span><strong>{{ statistics.verified ? (statistics.falseAlarmRate * 100).toFixed(1) + '%' : '—' }}</strong
      ><small>已核实 {{ statistics.verified }} 条</small>
    </div>
    <div>
      <span>平均确认用时</span><strong>{{ formatDuration(statistics.acknowledge?.avgMs) }}</strong
      ><small v-if="statistics.acknowledge?.count">90% 在 {{ formatDuration(statistics.acknowledge.p90Ms) }} 内</small>
    </div>
    <div>
      <span>平均核实用时</span><strong>{{ formatDuration(statistics.verify?.avgMs) }}</strong
      ><small v-if="statistics.topFalseAlarmDevices?.length"
        >误报最多：{{ statistics.topFalseAlarmDevices[0].deviceName || statistics.topFalseAlarmDevices[0].deviceId }}</small
      >
    </div>
  </div>
  <div v-else-if="statisticsError" class="alarm-stats alarm-stats-error" role="alert">
    <span>近 30 天核实统计读取失败</span><ui-button size="small" plain @click="loadStatistics">重试</ui-button>
  </div>

  <DataTableCard
    :title="`告警 · ${total} 条`"
    :error="loadError"
    @retry="load()"
    :page="page"
    :page-size="pageSize"
    :total="total"
    @update:page="changePage"
    @update:page-size="changePageSize"
  >
    <div class="only-desktop">
      <ui-table v-loading="loading" :data="items" :empty-text="filtered ? '没有符合筛选条件的告警' : '暂无告警'">
        <ui-table-column label="时间" min-width="170"
          ><template #default="{ row }">{{ formatTime(row.lastTriggeredAt) }}</template></ui-table-column
        >
        <ui-table-column label="设备 / 来源" min-width="190"
          ><template #default="{ row }"
            ><b>{{ row.deviceName || row.deviceId }}</b
            ><small v-if="row.componentId" class="subline"
              >{{ row.componentName || row.componentId }} · {{ row.componentLocation || row.componentId }}</small
            ><small v-if="row.location" class="subline">{{
              [row.location.unitName, row.location.buildingName, row.location.floorName, row.location.pointName].filter(Boolean).join(' · ')
            }}</small
            ><small class="subline">{{ label(alarmSources, row.source, '其他来源') }}</small></template
          ></ui-table-column
        >
        <ui-table-column label="告警类型" min-width="150"
          ><template #default="{ row }">{{ alarmType(row.alarmType) }}</template></ui-table-column
        >
        <ui-table-column label="等级" width="90"
          ><template #default="{ row }"
            ><ui-tag :type="tagType(row.alarmLevel)" round>{{ label(alarmLevels, row.alarmLevel, '未设置') }}</ui-tag></template
          ></ui-table-column
        >
        <ui-table-column label="状态" width="100"
          ><template #default="{ row }"><StatusDot :tone="statusTone(row.status)" :label="label(alarmStatuses, row.status)" /></template
        ></ui-table-column>
        <ui-table-column label="核实结论" width="110"
          ><template #default="{ row }"
            ><span v-if="row.disposition">{{ label(dispositionResults, row.disposition.result) }}</span
            ><span v-else-if="requiresVerification(row)" class="pending-verify">待核实</span><span v-else class="subline">—</span></template
          ></ui-table-column
        >
        <ui-table-column label="操作" fixed="right" width="200" align="right"
          ><template #default="{ row }"><RowActions :actions="rowActions(row)" /></template
        ></ui-table-column>
      </ui-table>
    </div>
    <ul class="alarm-cards only-mobile" :class="{ 'ui-loading': loading }">
      <li v-for="row in items" :key="row.alarmId" class="alarm-card">
        <button type="button" class="alarm-card__title" @click="show(row.alarmId)">
          <strong>{{ row.deviceName || row.deviceId }}</strong
          ><small>{{ alarmType(row.alarmType) }} · {{ formatTime(row.lastTriggeredAt) }}</small>
        </button>
        <div class="alarm-card__meta">
          <ui-tag :type="tagType(row.alarmLevel)" round size="small">{{ label(alarmLevels, row.alarmLevel, '未设置') }}</ui-tag>
          <StatusDot :tone="statusTone(row.status)" :label="label(alarmStatuses, row.status)" />
          <span v-if="row.disposition">{{ label(dispositionResults, row.disposition.result) }}</span
          ><span v-else-if="requiresVerification(row)" class="pending-verify">待核实</span>
        </div>
        <RowActions :actions="rowActions(row)" />
      </li>
      <li v-if="!items.length && !loading" class="alarm-cards__empty">{{ filtered ? '没有符合筛选条件的告警' : '暂无告警' }}</li>
    </ul>
  </DataTableCard>
  <!-- 研判质量按需展开后才读取统计。 -->
  <ui-collapse class="alarm-ai-quality"
    ><ui-collapse-item title="AI 研判质量统计" name="quality"><AiAnalysisQuality /></ui-collapse-item
  ></ui-collapse>

  <ui-dialog
    :model-value="detailVisible"
    class="alarm-detail-dialog"
    title="告警详情"
    width="min(760px, 94vw)"
    @update:model-value="setDetailVisible"
    @closed="handleDetailClosed"
  >
    <!-- 告警详情的长报文跟随弹窗正文统一滚动。 -->
    <ui-descriptions v-if="detail" :column="1" border>
      <ui-descriptions-item label="告警内容">{{ detail.content || '—' }}</ui-descriptions-item>
      <ui-descriptions-item label="告警编号">{{ detail.alarmId }}</ui-descriptions-item
      ><ui-descriptions-item label="设备">{{ detail.deviceName || detail.deviceId }}</ui-descriptions-item
      ><ui-descriptions-item v-if="detail.componentId" label="部件"
        >{{ detail.componentName || detail.componentId }}（{{ detail.componentId }}）</ui-descriptions-item
      ><ui-descriptions-item v-if="detail.componentLocation" label="部件位置">{{ detail.componentLocation }}</ui-descriptions-item
      ><ui-descriptions-item label="告警类型">{{ alarmType(detail.alarmType) }}</ui-descriptions-item
      ><ui-descriptions-item label="等级 / 状态"
        ><ui-tag :type="tagType(detail.alarmLevel)">{{ label(alarmLevels, detail.alarmLevel) }}</ui-tag>
        {{ label(alarmStatuses, detail.status) }}</ui-descriptions-item
      ><ui-descriptions-item label="来源">{{ label(alarmSources, detail.source, '其他来源') }}</ui-descriptions-item
      ><ui-descriptions-item label="首次发生">{{ formatTime(detail.firstTriggeredAt) }}</ui-descriptions-item
      ><ui-descriptions-item label="最后发生">{{ formatTime(detail.lastTriggeredAt) }}</ui-descriptions-item
      ><ui-descriptions-item label="触发次数">{{ detail.triggerCount }}</ui-descriptions-item>
    </ui-descriptions>
    <ui-card v-if="detail" shadow="never" class="top-gap">
      <template #header><strong>关联摄像头</strong></template>
      <LinkedCameras :cameras="detail.cameras || []" />
    </ui-card>
    <AlarmLocation v-if="detailVisible && detail" :alarm="detail" />
    <AlarmDisposition v-if="detailVisible && detail" ref="dispositionPanel" :alarm="detail" @updated="dispositionUpdated" />
    <AlarmAttachments v-if="detailVisible && detail" :alarm="detail" @updated="dispositionUpdated" />
    <AlarmMediaPanel v-if="detailVisible && detail" :alarm="detail" @refresh="refreshMediaDetail" />
    <AlarmNotifications v-if="detailVisible && detail" :alarm-id="detail.alarmId" />
    <ui-card shadow="never" class="top-gap">
      <template #header
        ><div class="card-header">
          <strong>智能研判</strong
          ><ui-button
            v-permission="'POST /api/v1/ai/alarm-analysis'"
            size="small"
            type="primary"
            :loading="analysisLoading"
            :disabled="analysisLoading"
            @click="runAnalysis"
            >{{ analysisLoading ? '研判中…' : analysis ? '重新研判' : '开始研判' }}</ui-button
          >
        </div></template
      >
      <div v-if="analysisProgress" class="analysis-progress" aria-live="polite">
        <div class="analysis-progress-heading">
          <strong>{{ analysisProgress.message || '智能正在处理' }}</strong
          ><span v-if="hasProgress">{{ progressPercent }}%</span>
        </div>
        <ui-progress v-if="hasProgress" :percentage="progressPercent" :status="progressStatus" :stroke-width="10" />
        <small v-if="analysisProgress.status === 'running'">{{ formatRemaining(analysisProgress.estimatedRemainingMs) }}</small>
        <small v-else>{{ analysisProgress.status === 'succeeded' ? '处理完成' : analysisProgress.error || '处理失败' }}</small>
      </div>
      <ui-empty v-if="!analysis && !analysisLoading" description="该告警尚未研判，点击“开始研判”后执行" :image-size="52" />
      <div v-if="analysis" class="analysis-grid">
        <ui-alert :title="analysis.summary || '智能未返回摘要'" :type="riskAlertType(analysis.riskLevel)" :closable="false" show-icon />
        <div>
          <strong>风险等级：</strong>{{ alarmLevel(analysis.riskLevel) }}
          <span class="subline">置信度 {{ analysis.confidence == null ? '—' : Number(analysis.confidence).toFixed(2) }}</span>
        </div>
        <div v-if="analysis.possibleReasons?.length">
          <strong>可能原因</strong>
          <ul>
            <li v-for="item in analysis.possibleReasons" :key="item">{{ item }}</li>
          </ul>
        </div>
        <div v-if="analysis.suggestions?.length">
          <strong>建议处置</strong>
          <ul>
            <li v-for="item in analysis.suggestions" :key="item">{{ item }}</li>
          </ul>
        </div>
        <small class="subline">{{ analysisKnowledgeText(analysis) }}</small
        ><small class="subline">模型：{{ analysis.model || '—' }} · 生成时间：{{ formatTime(analysis.createdAt) }}</small>
      </div>
    </ui-card>
    <details v-if="detail" class="raw-detail top-gap">
      <summary>查看原始数据</summary>
      <pre>{{ pretty(detail) }}</pre>
    </details>
    <template #footer
      ><ui-button
        v-if="detailOpen && canAcknowledgeAlarm(detail.status)"
        v-permission="'POST /api/v1/alarms/:id/actions'"
        :loading="actionPending[detail.alarmId] === 'ACKED'"
        :disabled="Boolean(actionPending[detail.alarmId])"
        @click="action(detail.alarmId, 'ACKED')"
        >确认告警</ui-button
      ><ui-button
        v-if="detailOpen && canCloseAlarm(detail.status)"
        v-permission="'POST /api/v1/alarms/:id/actions'"
        type="danger"
        :loading="actionPending[detail.alarmId] === 'CLOSED'"
        :disabled="Boolean(actionPending[detail.alarmId])"
        @click="action(detail.alarmId, 'CLOSED')"
        >关闭告警</ui-button
      ><ui-button @click="setDetailVisible(false)">关闭详情</ui-button></template
    >
  </ui-dialog>
</template>

<style scoped>
.alarm-cards {
  position: relative;
  margin: 0;
  padding: 0;
  list-style: none;
}
.alarm-card {
  display: grid;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-4);
  border-bottom: 1px solid var(--border);
}
.alarm-card:last-child {
  border-bottom: 0;
}
.alarm-card__title {
  display: grid;
  gap: 2px;
  padding: 0;
  color: var(--text-strong);
  background: none;
  border: 0;
  font: inherit;
  text-align: left;
  cursor: pointer;
}
.alarm-card__title small {
  color: var(--text-muted);
  font-size: var(--font-size-xs);
}
.alarm-card__meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-1) var(--space-3);
  color: var(--text-secondary);
  font-size: var(--font-size-sm);
}
.alarm-card :deep(.row-actions) {
  justify-content: flex-start;
}
.alarm-cards__empty {
  padding: var(--space-8) var(--space-4);
  color: var(--text-muted);
  text-align: center;
}
.raw-detail summary {
  color: var(--text-secondary);
  cursor: pointer;
}
.alarm-ai-quality {
  margin-top: 16px;
}
.monthly-report {
  display: inline-flex;
  gap: var(--space-2);
  align-items: center;
}
.monthly-report .ui-month {
  width: 132px;
}
.alarm-stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
  gap: var(--space-3);
  margin-bottom: var(--space-3);
}
.alarm-stats-error {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  padding: var(--space-3);
  color: var(--danger-text, var(--danger));
  background: var(--surface);
  border: 1px solid var(--danger-border);
  border-radius: var(--radius-lg);
}
.alarm-stats > div {
  display: grid;
  gap: 2px;
  padding: var(--space-3);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
}
.alarm-stats span,
.alarm-stats small {
  color: var(--text-muted);
  font-size: 12px;
}
.alarm-stats strong {
  font-size: 20px;
  color: var(--text-strong);
}
.alarm-stats strong.danger,
.pending-verify {
  color: var(--danger);
}
.analysis-grid {
  display: grid;
  gap: 9px;
  line-height: 1.65;
}
.analysis-grid :deep(ul) {
  margin: 5px 0 0;
  padding-left: 20px;
  color: var(--text-muted);
}
.analysis-progress {
  margin: 12px 0;
  padding: 12px;
  background: var(--surface-muted);
  border: 1px solid var(--info-border);
  border-radius: 5px;
}
.analysis-progress-heading {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 7px;
  color: var(--primary-text);
  font-size: 13px;
}
.analysis-progress-heading span {
  color: var(--primary-text);
  font-weight: 700;
}
.analysis-progress small {
  display: block;
  margin-top: 7px;
  color: var(--text);
  font-size: 12px;
}
</style>
