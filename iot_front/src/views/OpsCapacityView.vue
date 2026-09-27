<script setup>
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate'])
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { RefreshCw } from '@lucide/vue'
import { UiMessage, UiMessageBox } from '../ui/feedback.js'
import { download, formatTime } from '../api'
import { can } from '../permissions'
import { opsErrorText, opsGet, opsSend } from '../ops/opsApi'
import { boundText, buildPlan, classText, defaultForm, formProblems, isFinished, perDeviceLimit, phaseSummary, pollDelay, presetDefaults, presetText, rateCeiling, reportFormats, runStatusText, statusTone, verdictText, windowProgress } from '../ops/capacity'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'

const moduleStatus = ref(null)
const environments = ref([])
const environment = ref('')
const form = reactive(defaultForm('quick'))
// 高级模式直接编辑计划 YAML；进入时以当前表单生成的计划为起点。
const advanced = ref(false)
const planText = ref('')
const problems = computed(() => formProblems(form))
const plan = computed(() => advanced.value ? planText.value : buildPlan(form))
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
// 容量测试模块只有“本平台”一个环境时不需要选择。
const selfOnly = computed(() => environments.value.length === 1 && environments.value[0].name === 'self')
const moduleOff = computed(() => moduleStatus.value?.enabled === false)

function choosePreset(preset) {
  Object.assign(form, presetDefaults[preset])
}
watch(advanced, on => { if (on) planText.value = buildPlan(form) })
watch(() => [JSON.stringify(form), planText.value, advanced.value], () => { check.value = null })

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

onMounted(async () => {
  await loadStatus()
  if (moduleOff.value) return
  loadEnvironments()
  loadRuns()
})
onBeforeUnmount(() => clearTimeout(timer))
</script>

<template>
  <section v-if="moduleOff" class="cap-off">
    <h2>容量测试模块未部署</h2>
    <p>容量测试是可选模块，默认关闭。需要时由运维在部署机上开启；开启后本页直接选择测试类型即可运行，无需编写清单或配置凭据：</p>
    <ul>
      <li>单机在线部署：<code>bash scripts/capacity-module.sh enable</code>（或部署时加 <code>--capacity on</code>）</li>
      <li>单机离线部署：<code>bash scripts/capacity-module.sh enable --mode offline</code></li>
      <li>集群部署：<code>bash scripts/cluster-up.sh --name &lt;集群名称&gt; --capacity on</code></li>
    </ul>
    <p class="cap-sub">Windows 使用对应的 .ps1 脚本；关闭时用 disable 或 --capacity off。</p>
  </section>
  <template v-else>
  <ui-alert v-if="notConfigured" class="cap-gap" type="warning" title="容量测试服务未配置" :description="notConfigured" :closable="false" show-icon />
  <ui-alert v-else-if="moduleStatus && !moduleStatus.reachable" class="cap-gap" type="warning" title="容量测试服务暂不可达" description="模块已部署但服务未响应，请在部署机查看 capacity 容器状态（scripts/capacity-module.sh status）。" :closable="false" show-icon />

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
        <ui-button v-if="canRun" type="primary" :loading="busy === 'start'" :disabled="Boolean(notConfigured) || Boolean(activeRun)" @click="start">启动测试</ui-button>
      </template>
    </FilterBar>
    <p class="cap-hint">测试以你的账号权限运行：自动准备测试产品 cap-standard、测试规则 cap-stress-alarm 与测试设备（前缀 cap），测试后保留以便复测。<template v-if="activeRun">当前运行 {{ activeRun.runId }} 结束前不能启动新的测试。</template><template v-if="!canRun">当前账号只能查看运行与结论。</template></p>
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
