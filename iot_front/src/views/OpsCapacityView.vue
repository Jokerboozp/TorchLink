<script setup>
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate'])
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { RefreshCw } from '@lucide/vue'
import { UiMessage, UiMessageBox } from '../ui/feedback.js'
import { download, formatTime, session } from '../api'
import { can } from '../permissions'
import { opsErrorText, opsGet, opsSend } from '../ops/opsApi'
import { boundText, buildPlan, classText, cleanupCountItems, cleanupCountText, cleanupRuntimeItems, defaultForm, formProblems, historyCleanupHasTargets, historyCleanupProgress, historyCleanupResultText, historyCleanupRunning, historyCleanupStatusText, historyCleanupWarnings, isFinished, loadDraft, perDeviceLimit, phaseSummary, pollDelay, presetDefaults, presetText, rateCeiling, reportFormats, runStatusText, saveDraft, statusTone, verdictText, windowProgress } from '../ops/capacity'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'

const draftStorage = typeof window !== 'undefined' ? window.sessionStorage : null
const draft = loadDraft(draftStorage, session)
const moduleStatus = ref(null)
const environments = ref([])
const environment = ref(draft?.environment || '')
const form = reactive(draft?.form || defaultForm('quick'))
// 高级模式直接编辑计划 YAML；进入时以当前表单生成的计划为起点。
const advanced = ref(draft?.advanced || false)
const planText = ref(draft?.planText || '')
const problems = computed(() => formProblems(form))
const plan = computed(() => advanced.value ? planText.value : buildPlan(form))
const check = ref(null)
const runs = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
// 运行与清理状态以控制服务为准，离开页面再回来也能看到。
const activeRunId = ref('')
const cleaningRunId = ref('')
const historyCleaning = ref(false)
const loading = ref(false)
const busy = ref('')
const notConfigured = ref('')
const loadError = ref('')
const detail = ref(null)
const now = ref(Date.now())
const historyJob = ref(null)
const historyPreview = ref(null)
const historyDialog = ref(false)
const historyLoading = ref(false)
const historyError = ref('')
const historyPending = ref(false)
const historyRunning = computed(() => historyPending.value || historyCleanupRunning(historyJob.value))
const cleanupBlocked = computed(() => historyCleaning.value || historyRunning.value)
const historyReady = ref(false)
let timer = null
let historyTimer = null
let historyVersion = 0
let historyActionVersion = 0
let loadVersion = 0
let disposed = false

const canRun = computed(() => can('POST /api/v1/ops/capacity/runs'))
const canValidate = computed(() => can('POST /api/v1/ops/capacity/plans/validate'))
const canCleanup = computed(() => can('DELETE /api/v1/ops/capacity/runs/:id'))
const selectedEnv = computed(() => environments.value.find(env => env.name === environment.value))
// 容量测试模块只有“本平台”一个环境时不需要选择。
const selfOnly = computed(() => environments.value.length === 1 && environments.value[0].name === 'self')
const moduleOff = computed(() => moduleStatus.value?.enabled === false)
const historyIdentity = () => `${session.tenant}:${session.user}:${session.accessVersion || ''}:${environment.value}`
const historyAvailable = computed(() => canCleanup.value && !moduleOff.value && !notConfigured.value && moduleStatus.value?.reachable !== false && Boolean(environment.value))

function scheduleHistory() {
  clearTimeout(historyTimer)
  if (!disposed && cleanupBlocked.value && historyAvailable.value) historyTimer = setTimeout(() => loadHistoryStatus(true), 3000)
}

function updateHistoryJob(job) {
  const previous = historyJob.value
  historyJob.value = job
  if (job?.id) historyPending.value = false
  if (previous?.id === job?.id && historyCleanupRunning(previous) && !historyCleanupRunning(job)) {
    if (job.status === 'SUCCEEDED' && !job.error && !historyCleanupWarnings(job).length) UiMessage.success('本次可清理的历史测试数据已清理，保留项目不受影响')
    else if (job.status === 'FAILED' || job.status === 'PARTIAL') UiMessage.warning(job.error || historyCleanupStatusText[job.status])
  }
}

async function loadHistoryStatus(silent = false) {
  if (!historyAvailable.value || disposed) return
  const version = ++historyVersion, identity = historyIdentity()
  if (!silent) historyLoading.value = true
  try {
    const data = await opsGet('/api/v1/ops/capacity/cleanup/history/status', { environment: environment.value })
    if (disposed || version !== historyVersion || identity !== historyIdentity() || !canCleanup.value) return
    if (!Object.hasOwn(data, 'job') || (!data.job && historyRunning.value) || (data.job && (!data.job.id || !historyCleanupStatusText[data.job.status]))) throw new Error('后台未返回有效清理结果，请刷新确认；尚不能判定清理完成。')
    updateHistoryJob(data.job)
    historyReady.value = true
    historyError.value = ''
  } catch (error) {
    if (!disposed && version === historyVersion && identity === historyIdentity()) historyError.value = opsErrorText(error)
  } finally {
    if (version === historyVersion) historyLoading.value = false
    scheduleHistory()
  }
}

async function refreshHistoryCleanup() {
  await loadStatus()
  await loadHistoryStatus()
}

async function previewHistoryCleanup() {
  if (!historyAvailable.value || cleanupBlocked.value || busy.value || disposed) return
  const version = ++historyActionVersion, identity = historyIdentity()
  busy.value = 'history-preview'
  historyError.value = ''
  try {
    const data = await opsGet('/api/v1/ops/capacity/cleanup/history', { environment: environment.value })
    if (disposed || version !== historyActionVersion || identity !== historyIdentity() || !canCleanup.value) return
    if (data.job && (!data.job.id || !historyCleanupStatusText[data.job.status])) throw new Error('后台未返回有效清理状态，请刷新确认。')
    if (data.job) updateHistoryJob(data.job)
    if (historyRunning.value) return
    if (!data.preview?.token) throw new Error('未取得历史清理预览，请重新加载。')
    historyPreview.value = data.preview
    historyDialog.value = true
  } catch (error) {
    if (!disposed && version === historyActionVersion && identity === historyIdentity()) historyError.value = opsErrorText(error)
  } finally {
    if (version === historyActionVersion) busy.value = ''
    scheduleHistory()
  }
}

async function confirmHistoryCleanup() {
  if (!historyAvailable.value || cleanupBlocked.value || activeRunId.value || cleaningRunId.value || busy.value || !historyDialog.value || !historyCleanupHasTargets(historyPreview.value) || disposed) return
  const version = ++historyActionVersion, identity = historyIdentity()
  busy.value = 'history-start'
  historyError.value = ''
  try {
    const data = await opsSend('POST', '/api/v1/ops/capacity/cleanup/history', { environment: environment.value, previewToken: historyPreview.value.token })
    if (disposed || version !== historyActionVersion || identity !== historyIdentity() || !canCleanup.value) return
    historyDialog.value = false
    historyPreview.value = null
    if (data.job?.id && historyCleanupStatusText[data.job.status]) updateHistoryJob(data.job)
    else {
      historyJob.value = null; historyPending.value = true
      historyError.value = '后台已受理，但未返回任务状态；请刷新确认，尚不能判定清理完成。'
    }
    if (historyRunning.value) UiMessage.info('已提交历史清理，可离开页面；完成情况以后台结果为准')
    await Promise.all([loadHistoryStatus(true), loadRuns(true)])
  } catch (error) {
    if (!disposed && version === historyActionVersion && identity === historyIdentity()) {
      historyError.value = error?.status === 409 ? '清理范围或后台状态已变化，请重新预览后确认。' : opsErrorText(error)
      if (error?.status === 409) historyPreview.value = null
    }
  } finally {
    if (version === historyActionVersion) busy.value = ''
    scheduleHistory()
  }
}

watch(() => [environment.value, canCleanup.value, session.tenant, session.user, session.accessVersion], () => {
  historyVersion++; historyActionVersion++
  clearTimeout(historyTimer)
  historyJob.value = null; historyPreview.value = null; historyDialog.value = false
  historyReady.value = false; historyLoading.value = false; historyError.value = ''; historyPending.value = false
  if (busy.value.startsWith('history-')) busy.value = ''
  loadHistoryStatus()
})
watch(historyCleaning, () => { if (historyAvailable.value) loadHistoryStatus(true) })

function choosePreset(preset) {
  Object.assign(form, presetDefaults[preset])
}
watch(advanced, on => { if (on) planText.value = buildPlan(form) })
watch(() => [JSON.stringify(form), planText.value, advanced.value], () => { check.value = null })
watch(() => [JSON.stringify(form), planText.value, advanced.value, environment.value], () => {
  saveDraft(draftStorage, session, { form: { ...form }, advanced: advanced.value, planText: planText.value, environment: environment.value })
})

async function loadStatus() {
  try {
    moduleStatus.value = await opsGet('/api/v1/ops/capacity/status')
  } catch (error) {
    moduleStatus.value = null
    if (error?.status !== 403) UiMessage.error(opsErrorText(error))
  }
}

function schedule() {
  clearTimeout(timer)
  if (disposed) return
  const delay = pollDelay(runs.value, Boolean(activeRunId.value || cleaningRunId.value || cleanupBlocked.value))
  if (delay) timer = setTimeout(() => { now.value = Date.now(); loadRuns(true) }, delay)
}

function handleError(error) {
  if (error?.code === 'CAPACITY_NOT_CONFIGURED') {
    notConfigured.value = error.originalMessage || error.message
    return
  }
  UiMessage.error(opsErrorText(error))
}

async function loadEnvironments() {
  try {
    const data = await opsGet('/api/v1/ops/capacity/environments')
    environments.value = data.items || []
    if (!environments.value.some(env => env.name === environment.value && !env.error)) environment.value = environments.value.find(env => !env.error)?.name || ''
  } catch (error) {
    handleError(error)
  }
}

async function loadRuns(silent = false) {
  const version = ++loadVersion
  if (!silent) loading.value = true
  try {
    const data = await opsGet('/api/v1/ops/capacity/runs', { page: page.value, pageSize: pageSize.value })
    if (version !== loadVersion) return
    const cleaned = cleaningRunId.value
    runs.value = data.items || []
    total.value = data.total || 0
    activeRunId.value = data.activeRunId || ''
    cleaningRunId.value = data.cleaningRunId || ''
    historyCleaning.value = data.historyCleaning === true
    loadError.value = ''
    if (cleaned && !cleaningRunId.value) cleanupFinished(cleaned)
    // 清理后当前页可能已空，回到最后一个有数据的页。
    if (!runs.value.length && page.value > 1 && total.value > 0) {
      page.value = Math.ceil(total.value / pageSize.value)
      return loadRuns(silent)
    }
    if (detail.value) detail.value = runs.value.find(run => run.runId === detail.value.runId) || detail.value
  } catch (error) {
    if (version !== loadVersion) return
    if (error?.code === 'CAPACITY_NOT_CONFIGURED') notConfigured.value = error.originalMessage || error.message
    else loadError.value = opsErrorText(error)
  } finally {
    if (version === loadVersion) loading.value = false
    schedule()
  }
}

async function validate() {
  if (!environment.value) return UiMessage.warning('请先选择测试环境')
  if (!advanced.value && problems.value.length) return UiMessage.warning(problems.value[0])
  busy.value = 'validate'
  try {
    check.value = await opsSend('POST', '/api/v1/ops/capacity/plans/validate', { environment: environment.value, plan: plan.value })
    if (check.value.valid) UiMessage.success('计划校验通过')
  } catch (error) {
    handleError(error)
  } finally {
    busy.value = ''
  }
}

async function start() {
  if (cleanupBlocked.value) return
  if (!environment.value) return UiMessage.warning('请先选择测试环境')
  if (!advanced.value && problems.value.length) return UiMessage.warning(problems.value[0])
  try {
    const where = selfOnly.value ? '本平台' : `环境“${environment.value}”`
    await UiMessageBox.confirm(`将在${where}上施加真实负载，并以你的账号自动准备测试产品、测试规则与测试设备；测试期间平台性能会受影响。是否开始？`, '启动容量测试', { type: 'warning', confirmButtonText: '开始测试', cancelButtonText: '取消' })
  } catch {
    return
  }
  busy.value = 'start'
  try {
    const data = await opsSend('POST', '/api/v1/ops/capacity/runs', { environment: environment.value, plan: plan.value })
    UiMessage.success(`已启动 ${data.runId}`)
    check.value = null
    await loadRuns()
  } catch (error) {
    if (error?.status === 422 && error.details?.errors) check.value = { valid: false, errors: error.details.errors }
    else handleError(error)
  } finally {
    busy.value = ''
  }
}

async function stop(run, force) {
  const text = force ? '强制停止会跳过排空，未完成的消息只能记为未知。' : '将停止新负载，完成已发送消息的排空与核对后生成报告。'
  try {
    await UiMessageBox.confirm(`${text}是否停止 ${run.runId}？`, force ? '强制停止' : '停止测试', { type: 'warning', confirmButtonText: '停止', cancelButtonText: '取消' })
  } catch {
    return
  }
  busy.value = `stop:${run.runId}`
  try {
    await opsSend('POST', `/api/v1/ops/capacity/runs/${encodeURIComponent(run.runId)}/stop`, { force })
    UiMessage.success('已请求停止，报告生成后状态会更新')
    await loadRuns(true)
  } catch (error) {
    handleError(error)
  } finally {
    busy.value = ''
  }
}

async function downloadReport(run, format) {
  busy.value = `report:${run.runId}:${format.value}`
  try {
    await download(`/api/v1/ops/capacity/runs/${encodeURIComponent(run.runId)}/report?format=${format.value}`, `${run.runId}.${format.ext}`)
  } catch (error) {
    handleError(error)
  } finally {
    busy.value = ''
  }
}

function changePage(value) {
  page.value = value
  loadRuns()
}

function changePageSize(value) {
  pageSize.value = value
  page.value = 1
  loadRuns()
}

// 清理结束后按运行记录判断结果：记录已删除即成功，仍在则显示失败原因。
async function cleanupFinished(runId) {
  try {
    const run = await opsGet(`/api/v1/ops/capacity/runs/${encodeURIComponent(runId)}`)
    if (run.cleanupError) UiMessage.error(`${runId} 清理未完成，已保留运行记录，可重试：${run.cleanupError}`)
  } catch (error) {
    if (error?.status !== 404) return
    if (detail.value?.runId === runId) detail.value = null
    UiMessage.success(`${runId} 的可清理部分已处理，保留范围见预览`)
  }
}

async function cleanupRun(run) {
  if (!canCleanup.value || cleanupBlocked.value || run.active || !isFinished(run.status) || activeRunId.value || cleaningRunId.value || busy.value) return
  busy.value = `cleanup:${run.runId}`
  try {
    const preview = await opsGet(`/api/v1/ops/capacity/runs/${encodeURIComponent(run.runId)}/cleanup`)
    const lines = [
      `将清理 ${run.runId} 的 ${preview.rawMessages} 条测试报文及派生告警、${preview.devices} 个专用测试设备${preview.resources ? `、${preview.resources} 项业务任务或文档` : ''}、报告和缓存。`,
      preview.sharedDevices ? `${preview.sharedDevices} 个共享设备及其凭据仍供其他运行使用，将保留，只删除本次报文。` : '',
      ...(preview.warnings || []),
      '删除后无法恢复，请先下载需要保留的报告。'
    ]
    try {
      await UiMessageBox.confirm(lines.filter(Boolean).join('\n'), '清理本次测试数据和缓存', { type: 'warning', confirmButtonText: '确认清理', cancelButtonText: '取消' })
    } catch { return }
    await opsSend('DELETE', `/api/v1/ops/capacity/runs/${encodeURIComponent(run.runId)}`, {})
    cleaningRunId.value = run.runId
    UiMessage.success('已开始清理，可离开本页；完成后该记录会自动移除')
    await loadRuns(true)
  } catch (error) { handleError(error) }
  finally { busy.value = '' }
}

function rowActions(run) {
  const active = run.active || !isFinished(run.status)
  const actions = [{ key: 'detail', label: '详情', onClick: () => { detail.value = run } }]
  actions.push({ key: 'stop', label: '停止', permission: 'POST /api/v1/ops/capacity/runs/:id/stop', hidden: !active || run.status === 'CANCELLING', loading: busy.value === `stop:${run.runId}`, onClick: () => stop(run, false) })
  actions.push({ key: 'force', label: '强制停止', type: 'danger', permission: 'POST /api/v1/ops/capacity/runs/:id/stop', hidden: !active, onClick: () => stop(run, true) })
  if (run.reports?.includes('html')) actions.push({ key: 'html', label: '下载报告', permission: 'GET /api/v1/ops/capacity/runs/:id/report', loading: busy.value === `report:${run.runId}:html`, onClick: () => downloadReport(run, reportFormats[0]) })
  actions.push({ key: 'cleanup', label: run.cleaning ? '清理中' : run.cleanupError ? '重试清理' : '清理数据', type: 'danger', permission: 'DELETE /api/v1/ops/capacity/runs/:id', hidden: active, disabled: Boolean(activeRunId.value || cleaningRunId.value || cleanupBlocked.value || busy.value), loading: run.cleaning || busy.value === `cleanup:${run.runId}`, onClick: () => cleanupRun(run) })
  return actions
}

function stateLabel(run) {
  if (run.cleaning) return '正在清理数据'
  if (isFinished(run.status) && run.verdict) return `${runStatusText[run.status] || run.status} · ${verdictText[run.verdict] || run.verdict}`
  return runStatusText[run.status] || run.status
}

onMounted(async () => {
  await loadStatus()
  if (moduleOff.value) return
  loadEnvironments()
  loadRuns()
  loadHistoryStatus()
})
onBeforeUnmount(() => { disposed = true; loadVersion++; historyVersion++; historyActionVersion++; clearTimeout(timer); clearTimeout(historyTimer) })
</script>

<template>
  <section v-if="moduleOff" class="cap-off">
    <h2>容量测试模块未部署</h2>
    <p>当前环境尚未启用容量测试模块。启用后重启平台，即可在本页选择测试类型运行：</p>
    <ul>
      <li>单机在线部署：<code>bash scripts/capacity-module.sh enable</code>（或部署时加 <code>--capacity on</code>）</li>
      <li>单机离线部署：<code>bash scripts/capacity-module.sh enable --mode offline</code></li>
      <li>本地源码：在 <code>.env.local</code> 设置 <code>IOT_OPS_CAPACITY_LOCAL=true</code> 和 <code>IOT_CAPACITY_MODULE=on</code>，重启 API。</li>
      <li>集群部署：<code>bash scripts/cluster-up.sh --name &lt;集群名称&gt; --capacity on</code></li>
    </ul>
    <p class="cap-sub">Windows 使用对应的 .ps1 脚本；关闭时用 disable 或 --capacity off。</p>
  </section>
  <template v-else>
  <ui-alert v-if="notConfigured" class="cap-gap" type="warning" title="容量测试服务未配置" :description="notConfigured" :closable="false" show-icon />
  <ui-alert v-else-if="moduleStatus && !moduleStatus.reachable" class="cap-gap" type="warning" title="容量测试服务暂不可达" description="模块已部署但服务未响应，请在部署机查看 capacity 容器状态（scripts/capacity-module.sh status）。" :closable="false" show-icon />

  <section v-if="canCleanup" class="cap-history surface-panel">
    <div class="cap-history-toolbar">
      <div><strong>历史测试数据</strong><p class="cap-sub">清理可确认归属的历史压测数据；活跃或已有业务引用的项目保留。</p></div>
      <div class="cap-history-actions">
        <ui-button size="small" :loading="historyLoading" @click="refreshHistoryCleanup"><RefreshCw />刷新状态</ui-button>
        <ui-button size="small" type="danger" :loading="busy === 'history-preview'" :disabled="!historyAvailable || !historyReady || cleanupBlocked || Boolean(busy)" @click="previewHistoryCleanup">{{ ['FAILED', 'PARTIAL'].includes(historyJob?.status) ? '重新预览并重试' : '清理历史测试数据' }}</ui-button>
      </div>
    </div>
    <p v-if="historyError" class="cap-sub cap-sub--danger" role="alert">{{ historyError }}</p>
    <div v-if="historyJob || historyPending" class="cap-history-result" aria-live="polite">
      <strong>{{ historyPending ? '正在确认后台清理任务' : historyCleanupResultText(historyJob) }}</strong>
      <p v-if="historyJob?.environment" class="cap-sub">清理环境：{{ historyJob.environment === 'self' ? '本平台' : historyJob.environment }}</p>
      <p v-if="historyJob?.phase" class="cap-sub">{{ historyJob.phase }}</p>
      <template v-if="historyCleanupProgress(historyJob) != null">
        <ui-progress :percentage="historyCleanupProgress(historyJob)" :stroke-width="6" :show-text="false" />
        <p class="cap-sub">实际处理 {{ cleanupCountText(historyJob.processed) }} / {{ cleanupCountText(historyJob.total) }} 个运行或产品</p>
      </template>
      <p v-if="historyJob?.error" class="cap-sub cap-sub--danger">{{ historyJob.error }}</p>
      <p v-if="cleanupCountItems(historyJob?.counts).length" class="cap-sub">已清理：<span v-for="(item, index) in cleanupCountItems(historyJob.counts)" :key="item.key">{{ index ? ' · ' : '' }}{{ item.label }} {{ cleanupCountText(item.value) }}</span></p>
      <details v-if="cleanupRuntimeItems(historyJob?.counts).length"><summary>更多处理结果</summary><p v-for="item in cleanupRuntimeItems(historyJob.counts)" :key="item.key" class="cap-sub">{{ item.label }}：{{ cleanupCountText(item.value) }}</p></details>
      <p v-for="(warning, index) in historyCleanupWarnings(historyJob)" :key="index" class="cap-sub">{{ warning }}</p>
      <p v-if="historyJob?.updatedAt" class="cap-sub">后台更新时间：{{ formatTime(historyJob.updatedAt) }}</p>
    </div>
    <p v-if="historyCleaning && !historyRunning" class="cap-sub">其他历史清理任务正在后台执行，结束后可再次操作。</p>
  </section>

  <section class="cap-editor surface-panel">
    <FilterBar>
      <span v-if="selfOnly" class="cap-env-meta">测试环境：本平台（{{ selectedEnv?.metricsTargets || 0 }} 个平台进程指标{{ selectedEnv?.mqtt ? ' · MQTT' : '' }}）</span>
      <template v-else>
        <ui-select v-model="environment" placeholder="选择测试环境" aria-label="测试环境" :disabled="Boolean(notConfigured)" class="cap-env">
          <ui-option v-for="env in environments" :key="env.name" :label="env.title ? `${env.name}（${env.title}）` : env.name" :value="env.name" :disabled="Boolean(env.error)" />
        </ui-select>
        <span v-if="selectedEnv" class="cap-env-meta">Agent {{ selectedEnv.agents }} 个 · 指标目标 {{ selectedEnv.metricsTargets }} 个{{ selectedEnv.mqtt ? ' · MQTT' : '' }}{{ selectedEnv.tcp ? ' · TCP' : '' }}{{ selectedEnv.web ? ' · Web' : '' }}</span>
      </template>
      <template #actions>
        <ui-button v-if="canValidate" :loading="busy === 'validate'" :disabled="Boolean(notConfigured)" @click="validate">校验</ui-button>
        <ui-button v-if="canRun" type="primary" :loading="busy === 'start'" :disabled="Boolean(notConfigured || activeRunId || cleaningRunId || cleanupBlocked)" @click="start">启动测试</ui-button>
      </template>
    </FilterBar>
    <p class="cap-hint">测试以你的账号权限运行：自动准备测试产品 cap-standard、测试规则 cap-stress-alarm 与测试设备（前缀 cap），测试后保留以便复测；不再需要时可在运行记录中清理数据和缓存。<template v-if="activeRunId">当前运行 {{ activeRunId }} 结束前不能启动新的测试。</template><template v-else-if="cleaningRunId">正在清理 {{ cleaningRunId }}，完成前不能启动新的测试。</template><template v-if="!canRun && !canCleanup">当前账号只能查看运行与结论。</template></p>
    <template v-if="canValidate || canRun">
      <div v-if="!advanced" class="cap-form">
        <div class="cap-field cap-field--wide">
          <span>测试类型</span>
          <ui-radio-group v-model="form.preset" size="small" class="segmented-choice-group" aria-label="测试类型" @change="choosePreset">
            <ui-radio-button value="quick">快速检查</ui-radio-button>
            <ui-radio-button value="capacity">容量搜索</ui-radio-button>
            <ui-radio-button value="soak">长稳</ui-radio-button>
          </ui-radio-group>
          <small>{{ { quick: '固定速率跑一档，确认链路正常，不给出最大容量', capacity: '从起始速率逐步加压并二分，找出稳定通过的最高速率', soak: '固定速率长时间运行，观察积压、时延和资源是否稳定' }[form.preset] }}</small>
        </div>
        <label class="cap-field"><span>测试设备数</span><ui-input-number v-model="form.devices" :min="1" :max="10000" controls-position="right" /></label>
        <label class="cap-field"><span>{{ form.preset === 'capacity' ? '起始速率（条/秒）' : '速率（条/秒）' }}</span><ui-input-number v-model="form.startRate" :min="1" controls-position="right" /></label>
        <label class="cap-field"><span>速率上限（条/秒）</span><ui-input-number v-model="form.maxRate" :min="1" controls-position="right" /><small>单台设备限速 {{ perDeviceLimit }} 条/秒，最多 {{ rateCeiling(form) }}</small></label>
        <label class="cap-field"><span>{{ form.preset === 'soak' ? '持续时长（分钟）' : '每档测量时长（分钟）' }}</span><ui-input-number v-model="form.measureMinutes" :min="1" :max="1440" controls-position="right" /></label>
        <div class="cap-field cap-field--wide">
          <span>同时测试</span>
          <div class="cap-checks">
            <ui-checkbox v-model="form.alarms">告警触发与恢复核对</ui-checkbox>
            <ui-checkbox v-model="form.queries">管理查询</ui-checkbox>
            <ui-checkbox v-model="form.mqtt" :disabled="Boolean(selectedEnv && !selectedEnv.mqtt)">一半设备走 MQTT</ui-checkbox>
            <ui-checkbox v-model="form.realtime" :disabled="Boolean(selectedEnv && !selectedEnv.mqtt)">实时推送</ui-checkbox>
            <ui-checkbox v-model="form.exports">原文下载与回放</ui-checkbox>
            <ui-checkbox v-model="form.ai">AI 研判（真实模型，最多 20 次）</ui-checkbox>
          </div>
        </div>
        <ul v-if="problems.length" class="cap-problems"><li v-for="item in problems" :key="item">{{ item }}</li></ul>
      </div>
      <ui-input v-else v-model="planText" type="textarea" :rows="16" spellcheck="false" aria-label="容量测试计划 YAML" class="cap-plan" />
      <div class="cap-advanced"><ui-switch v-model="advanced" size="small" /><span>高级：直接编辑计划 YAML</span></div>
    </template>
    <div v-if="check" class="cap-check" :class="check.valid ? 'is-ok' : 'is-bad'">
      <template v-if="check.valid">
        <strong>校验通过</strong>
        <span>{{ presetText[check.preset] || check.preset }} · 设备 {{ check.deviceCount }} 台<template v-if="check.rates?.length"> · 速率 {{ check.rates.join(' / ') }} 条/秒</template> · 时长上限 {{ check.maximumWallTime }}</span>
        <span v-if="check.modules?.length">业务场景：{{ check.modules.join('、') }}</span>
        <span v-if="check.faults">故障动作 {{ check.faults }} 个</span>
      </template>
      <template v-else>
        <strong>计划不可运行</strong>
        <ul><li v-for="(item, index) in check.errors" :key="index">{{ item }}</li></ul>
      </template>
    </div>
  </section>

  <DataTableCard class="cap-table" :error="loadError" :page="page" :page-size="pageSize" :total="total" @update:page="changePage" @update:page-size="changePageSize" @retry="loadRuns()">
    <template #header>
      <h2>运行记录 · {{ total }} 次</h2>
      <ui-button size="small" :loading="loading" @click="loadRuns()"><RefreshCw />刷新</ui-button>
    </template>
    <ui-table v-loading="loading" :data="runs">
      <ui-table-column label="运行" min-width="230"><template #default="{ row }"><code>{{ row.runId }}</code><div class="cap-sub">{{ row.plan || '—' }} · {{ presetText[row.preset] || row.preset || '—' }}</div></template></ui-table-column>
      <ui-table-column label="状态" min-width="170"><template #default="{ row }"><StatusDot :tone="row.cleaning ? 'warning' : statusTone(row.status, row.verdict)" :label="stateLabel(row)" /><div v-if="row.message" class="cap-sub">{{ row.message }}</div><div v-if="row.cleanupError && !row.cleaning" class="cap-sub cap-sub--danger">清理失败：{{ row.cleanupError }}</div></template></ui-table-column>
      <ui-table-column label="当前阶段" min-width="200"><template #default="{ row }">
        <template v-if="!isFinished(row.status) && row.phaseId">
          <div class="cap-sub">{{ row.phaseId }} · {{ boundText(row.targetRate) }}</div>
          <ui-progress v-if="windowProgress(row, now) != null" :percentage="windowProgress(row, now)" :stroke-width="6" :show-text="false" />
          <div class="cap-sub">测量窗口按实际时间推进</div>
        </template>
        <span v-else>已完成 {{ row.completed.length }} 档（通过 {{ phaseSummary(row.completed).passed }}，失败 {{ phaseSummary(row.completed).failed }}）</span>
      </template></ui-table-column>
      <ui-table-column label="结论" min-width="220"><template #default="{ row }">
        <template v-if="row.classification">{{ classText[row.classification] || row.classification }}<div class="cap-sub">通过 {{ boundText(row.lowerPassedBound) }} · 失败 {{ boundText(row.upperFailedBound) }}</div></template>
        <span v-else>—</span>
      </template></ui-table-column>
      <ui-table-column label="开始时间" min-width="160"><template #default="{ row }">{{ formatTime(row.startedAt) }}</template></ui-table-column>
      <ui-table-column label="操作" fixed="right" width="190" align="right"><template #default="{ row }"><RowActions :actions="rowActions(row)" /></template></ui-table-column>
      <template #empty><ui-empty description="还没有容量测试运行记录" /></template>
    </ui-table>
  </DataTableCard>

  <ui-dialog v-model="historyDialog" title="确认清理历史测试数据" width="min(780px, 94vw)" :close-on-click-modal="false">
    <div class="cap-history-preview">
      <p v-if="historyError" class="cap-sub cap-sub--danger" role="alert">{{ historyError }}</p>
      <template v-if="historyPreview">
        <p class="cap-sub">清理环境：{{ environment === 'self' ? '本平台' : environment }}</p>
        <p>本次待清理：{{ cleanupCountText(historyPreview.runs) }} 次运行、{{ cleanupCountText(historyPreview.products) }} 个测试产品、{{ cleanupCountText(historyPreview.devices) }} 个设备、{{ cleanupCountText(historyPreview.rawMessages) }} 条测试原文及关联数据。</p>
        <ul class="cap-history-items"><li v-for="(item, index) in historyPreview.items || []" :key="`${item.productId || item.runId}:${index}`"><strong>{{ item.title || item.productId || item.runId }}</strong><p class="cap-sub">{{ item.eligible ? '待清理' : '保留' }} · 设备 {{ cleanupCountText(item.devices) }} · 测试原文 {{ cleanupCountText(item.rawMessages) }}<template v-if="item.reason"> · {{ item.reason }}</template></p></li></ul>
        <p v-for="(warning, index) in historyPreview.warnings || []" :key="index" class="cap-sub">{{ warning }}</p>
        <p v-if="!historyCleanupHasTargets(historyPreview)" class="cap-sub">当前没有可安全清理的历史测试数据。</p>
        <p class="cap-sub">清理后无法恢复，需要保留的报告请先下载。仅执行本次预览中可清理的项目。</p>
      </template>
      <ui-button v-else size="small" :loading="busy === 'history-preview'" @click="previewHistoryCleanup">重新预览</ui-button>
      <p v-if="activeRunId || cleaningRunId || cleanupBlocked" class="cap-sub">当前测试或清理结束后，请重新预览再确认。</p>
    </div>
    <template #footer><ui-button :disabled="busy === 'history-start'" @click="historyDialog = false">取消</ui-button><ui-button type="danger" :loading="busy === 'history-start'" :disabled="!historyAvailable || cleanupBlocked || Boolean(activeRunId || cleaningRunId || busy) || !historyCleanupHasTargets(historyPreview)" @click="confirmHistoryCleanup">确认清理</ui-button></template>
  </ui-dialog>

  <ui-dialog :model-value="Boolean(detail)" :title="detail ? `容量测试 · ${detail.runId}` : ''" width="min(860px, 94vw)" @update:model-value="value => { if (!value) detail = null }">
    <template v-if="detail">
      <p class="cap-conclusion">{{ detail.conclusion || (isFinished(detail.status) ? '报告尚未生成' : '运行中，结束后生成结论') }}</p>
      <p v-if="detail.message" class="cap-sub">{{ detail.message }}</p>
      <ui-descriptions :column="2" border>
        <ui-descriptions-item label="状态">{{ stateLabel(detail) }}</ui-descriptions-item>
        <ui-descriptions-item label="搜索结论">{{ classText[detail.classification] || '—' }}</ui-descriptions-item>
        <ui-descriptions-item label="最高通过档">{{ boundText(detail.lowerPassedBound) }}</ui-descriptions-item>
        <ui-descriptions-item label="最低失败档">{{ boundText(detail.upperFailedBound) }}</ui-descriptions-item>
        <ui-descriptions-item label="建议运行值">{{ boundText(detail.recommendedOperatingValue) }}</ui-descriptions-item>
        <ui-descriptions-item label="开始 / 更新">{{ formatTime(detail.startedAt) }} / {{ formatTime(detail.updatedAt) }}</ui-descriptions-item>
      </ui-descriptions>
      <ui-table class="cap-gap-top" :data="detail.completed" size="small">
        <ui-table-column prop="phaseId" label="阶段" min-width="180" />
        <ui-table-column label="目标速率" min-width="120"><template #default="{ row }">{{ boundText(row.rate) }}</template></ui-table-column>
        <ui-table-column label="结论" min-width="120"><template #default="{ row }">{{ verdictText[row.verdict] || row.verdict }}</template></ui-table-column>
        <ui-table-column prop="stopReason" label="原因" min-width="140" />
        <template #empty><ui-empty description="尚无完成的阶段" /></template>
      </ui-table>
      <div v-if="detail.reports?.length && can('GET /api/v1/ops/capacity/runs/:id/report')" class="cap-downloads">
        <ui-button v-for="format in reportFormats.filter(item => detail.reports.includes(item.value))" :key="format.value" size="small" :loading="busy === `report:${detail.runId}:${format.value}`" @click="downloadReport(detail, format)">{{ format.label }}</ui-button>
      </div>
    </template>
    <template #footer><ui-button @click="detail = null">关闭</ui-button></template>
  </ui-dialog>
  </template>
</template>

<style scoped>
.cap-gap { margin-bottom: var(--space-4); }
.cap-history { margin-bottom: var(--space-4); padding: var(--space-3) var(--space-4); border: 1px solid var(--border); border-radius: var(--radius-lg); }
.cap-history-toolbar,.cap-history-actions { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: var(--space-2); }
.cap-history-result { display: grid; gap: 5px; margin-top: var(--space-3); }
.cap-history-preview { max-height: 60vh; overflow: auto; overflow-wrap: anywhere; }
.cap-history-items { list-style: none; margin: var(--space-3) 0; padding: 0; }
.cap-history-items li { border-bottom: 1px solid var(--border); padding: var(--space-2) 0; }
.cap-gap-top { margin-top: var(--space-4); }
.cap-editor { margin-bottom: var(--space-4); padding: var(--space-4); background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-lg); }
.cap-env { min-width: 220px; }
.cap-env-meta { color: var(--text-muted); font-size: var(--font-size-sm); }
.cap-hint { margin: var(--space-2) 0 var(--space-3); color: var(--text-muted); font-size: var(--font-size-sm); }
.cap-plan :deep(textarea) { font-family: var(--font-mono, ui-monospace, monospace); font-size: 12px; }
.cap-check { margin-top: var(--space-3); padding: var(--space-3); border-radius: var(--radius-md); display: flex; flex-direction: column; gap: 4px; font-size: var(--font-size-sm); }
.cap-check.is-ok { background: var(--success-soft, #ecfdf5); }
.cap-check.is-bad { background: var(--danger-soft, #fef2f2); }
.cap-check ul { margin: 0; padding-left: 18px; }
.cap-sub { color: var(--text-muted); font-size: 12px; margin-top: 2px; }
.cap-sub--danger { color: var(--danger-text, #b91c1c); }
.cap-conclusion { margin: 0 0 var(--space-3); line-height: 1.6; }
.cap-off { padding: var(--space-5); background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-lg); line-height: 1.7; }
.cap-off h2 { margin: 0 0 var(--space-2); font-size: 16px; }
.cap-off code { font-size: 12px; overflow-wrap: anywhere; }
.cap-form { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: var(--space-3) var(--space-4); margin-top: var(--space-2); }
.cap-field { display: flex; flex-direction: column; gap: 6px; min-width: 0; font-size: var(--font-size-sm); }
.cap-field > span { font-weight: 600; }
.cap-field small { color: var(--text-muted); font-size: 12px; }
.cap-field--wide { grid-column: 1 / -1; }
.cap-checks { display: flex; flex-wrap: wrap; gap: var(--space-2) var(--space-4); }
.cap-problems { grid-column: 1 / -1; margin: 0; padding-left: 18px; color: var(--danger-text, #b91c1c); font-size: var(--font-size-sm); }
.cap-advanced { display: flex; align-items: center; gap: var(--space-2); margin-top: var(--space-3); color: var(--text-muted); font-size: var(--font-size-sm); }
.cap-downloads { display: flex; flex-wrap: wrap; gap: var(--space-2); margin-top: var(--space-4); }
.cap-table :deep(.data-table-card__header) h2 { margin: 0; font-size: 15px; }
@media (max-width: 767px) {
  .cap-form { grid-template-columns: 1fr 1fr; }
  .cap-env { min-width: 0; width: 100%; }
  .cap-editor { padding: var(--space-3); }
}
@media (max-width: 480px) {
  .cap-form { grid-template-columns: 1fr; }
}
</style>
