<script setup>
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate'])
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { RefreshCw } from '@lucide/vue'
import { UiMessage, UiMessageBox } from '../ui/feedback.js'
import { download, formatTime } from '../api'
import { can } from '../permissions'
import { opsErrorText, opsGet, opsSend } from '../ops/opsApi'
import { boundText, classText, isFinished, phaseSummary, planTemplate, pollDelay, presetText, reportFormats, runStatusText, statusTone, verdictText, windowProgress } from '../ops/capacity'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'

const environments = ref([])
const environment = ref('')
const plan = ref(planTemplate)
const check = ref(null)
const runs = ref([])
const loading = ref(false)
const busy = ref('')
const notConfigured = ref('')
const loadError = ref('')
const detail = ref(null)
const now = ref(Date.now())
let timer = null
let loadVersion = 0

const canRun = computed(() => can('POST /api/v1/ops/capacity/runs'))
const canValidate = computed(() => can('POST /api/v1/ops/capacity/plans/validate'))
const activeRun = computed(() => runs.value.find(run => run.active))
const selectedEnv = computed(() => environments.value.find(env => env.name === environment.value))

function schedule() {
  clearTimeout(timer)
  const delay = pollDelay(runs.value)
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
    if (!environment.value && environments.value.length) environment.value = environments.value.find(env => !env.error)?.name || ''
  } catch (error) {
    handleError(error)
  }
}

async function loadRuns(silent = false) {
  const version = ++loadVersion
  if (!silent) loading.value = true
  try {
    const data = await opsGet('/api/v1/ops/capacity/runs')
    if (version !== loadVersion) return
    runs.value = data.items || []
    loadError.value = ''
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
  if (!environment.value) return UiMessage.warning('请先选择测试环境')
  try {
    await UiMessageBox.confirm(`将在环境“${environment.value}”上按计划施加真实负载并创建测试设备，测试期间平台性能会受影响。是否开始？`, '启动容量测试', { type: 'warning', confirmButtonText: '开始测试', cancelButtonText: '取消' })
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

function rowActions(run) {
  const active = run.active || !isFinished(run.status)
  const actions = [{ key: 'detail', label: '详情', onClick: () => { detail.value = run } }]
  actions.push({ key: 'stop', label: '停止', permission: 'POST /api/v1/ops/capacity/runs/:id/stop', hidden: !active || run.status === 'CANCELLING', loading: busy.value === `stop:${run.runId}`, onClick: () => stop(run, false) })
  actions.push({ key: 'force', label: '强制停止', type: 'danger', permission: 'POST /api/v1/ops/capacity/runs/:id/stop', hidden: !active, onClick: () => stop(run, true) })
  if (run.reports?.includes('html')) actions.push({ key: 'html', label: '下载报告', permission: 'GET /api/v1/ops/capacity/runs/:id/report', loading: busy.value === `report:${run.runId}:html`, onClick: () => downloadReport(run, reportFormats[0]) })
  return actions
}

function stateLabel(run) {
  if (isFinished(run.status) && run.verdict) return `${runStatusText[run.status] || run.status} · ${verdictText[run.verdict] || run.verdict}`
  return runStatusText[run.status] || run.status
}

onMounted(() => { loadEnvironments(); loadRuns() })
onBeforeUnmount(() => clearTimeout(timer))
</script>

<template>
  <ui-alert v-if="notConfigured" class="cap-gap" type="warning" title="容量测试控制服务未配置" :description="notConfigured" :closable="false" show-icon />

  <section class="cap-editor surface-panel">
    <FilterBar>
      <ui-select v-model="environment" placeholder="选择测试环境" aria-label="测试环境" :disabled="Boolean(notConfigured)" class="cap-env">
        <ui-option v-for="env in environments" :key="env.name" :label="env.title ? `${env.name}（${env.title}）` : env.name" :value="env.name" :disabled="Boolean(env.error)" />
      </ui-select>
      <span v-if="selectedEnv" class="cap-env-meta">Agent {{ selectedEnv.agents }} 个 · 指标目标 {{ selectedEnv.metricsTargets }} 个{{ selectedEnv.mqtt ? ' · MQTT' : '' }}{{ selectedEnv.tcp ? ' · TCP' : '' }}{{ selectedEnv.web ? ' · Web' : '' }}</span>
      <template #actions>
        <ui-button v-if="canValidate" :loading="busy === 'validate'" :disabled="Boolean(notConfigured)" @click="validate">校验计划</ui-button>
        <ui-button v-if="canRun" type="primary" :loading="busy === 'start'" :disabled="Boolean(notConfigured) || Boolean(activeRun)" @click="start">启动测试</ui-button>
      </template>
    </FilterBar>
    <p class="cap-hint">环境由控制机上的受信任清单登记，页面只能选择名称；秘密、地址与故障命令不经过浏览器。<template v-if="activeRun">当前运行 {{ activeRun.runId }} 结束前不能启动新的测试。</template><template v-if="!canRun">当前账号只能查看运行与结论。</template></p>
    <ui-input v-if="canValidate || canRun" v-model="plan" type="textarea" :rows="14" spellcheck="false" aria-label="容量测试计划 YAML" class="cap-plan" />
    <div v-if="check" class="cap-check" :class="check.valid ? 'is-ok' : 'is-bad'">
      <template v-if="check.valid">
        <strong>校验通过</strong>
        <span>{{ check.name }} · {{ presetText[check.preset] || check.preset }} · 套件 {{ check.suite }} · 设备 {{ check.deviceCount }} 台<template v-if="check.rates?.length"> · 档位 {{ check.rates.join(' / ') }} 条/秒</template> · 时长上限 {{ check.maximumWallTime }}</span>
        <span v-if="check.modules?.length">业务场景：{{ check.modules.join('、') }}</span>
        <span v-if="check.faults">故障动作 {{ check.faults }} 个</span>
      </template>
      <template v-else>
        <strong>计划不可运行</strong>
        <ul><li v-for="(item, index) in check.errors" :key="index">{{ item }}</li></ul>
      </template>
    </div>
  </section>

  <DataTableCard class="cap-table" :error="loadError" @retry="loadRuns()">
    <template #header>
      <h2>运行记录 · {{ runs.length }} 次</h2>
      <ui-button size="small" :loading="loading" @click="loadRuns()"><RefreshCw />刷新</ui-button>
    </template>
    <ui-table v-loading="loading" :data="runs">
      <ui-table-column label="运行" min-width="230"><template #default="{ row }"><code>{{ row.runId }}</code><div class="cap-sub">{{ row.plan || '—' }} · {{ presetText[row.preset] || row.preset || '—' }}</div></template></ui-table-column>
      <ui-table-column label="状态" min-width="170"><template #default="{ row }"><StatusDot :tone="statusTone(row.status, row.verdict)" :label="stateLabel(row)" /><div v-if="row.message && !isFinished(row.status)" class="cap-sub">{{ row.message }}</div></template></ui-table-column>
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

  <ui-dialog :model-value="Boolean(detail)" :title="detail ? `容量测试 · ${detail.runId}` : ''" width="min(860px, 94vw)" @update:model-value="value => { if (!value) detail = null }">
    <template v-if="detail">
      <p class="cap-conclusion">{{ detail.conclusion || (isFinished(detail.status) ? '报告尚未生成' : '运行中，结束后生成结论') }}</p>
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

<style scoped>
.cap-gap { margin-bottom: var(--space-4); }
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
.cap-conclusion { margin: 0 0 var(--space-3); line-height: 1.6; }
.cap-downloads { display: flex; flex-wrap: wrap; gap: var(--space-2); margin-top: var(--space-4); }
.cap-table :deep(.data-table-card__header) h2 { margin: 0; font-size: 15px; }
@media (max-width: 767px) {
  .cap-env { min-width: 0; width: 100%; }
  .cap-editor { padding: var(--space-3); }
}
</style>
