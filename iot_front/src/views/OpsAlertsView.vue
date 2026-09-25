<script setup>
// 监控告警：Prometheus 与 Loki ruler 评估规则，Alertmanager 负责分组、静默与通知。
// 本页查看当前告警、规则状态和历史，管理静默与通知路由；消防业务告警不在此处理。
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { BellOff, LineChart, RefreshCw } from '@lucide/vue'
import { can } from '../permissions'
import { UiMessage, UiMessageBox } from '../ui/feedback.js'
import { formatDuration, relativeTime } from '../ops/format.js'
import { latest, opsErrorText, opsGet, opsSend, takeNavigation } from '../ops/opsApi.js'
import { refreshOptions, resolveRange } from '../ops/timeRange.js'
import StatusDot from '../components/layout/StatusDot.vue'
import MatcherEditor from '../components/ops/MatcherEditor.vue'
import NotificationPanel from '../components/ops/NotificationPanel.vue'
import SilenceDialog from '../components/ops/SilenceDialog.vue'
import TimeRangeBar from '../components/ops/TimeRangeBar.vue'

const emit = defineEmits(['navigate'])
const tab = ref('current')
const labelHints = ['alertname', 'severity', 'job', 'instance', 'service_name', 'rulegroup']
const severityTone = { critical: 'danger', error: 'danger', warning: 'warning', info: 'info' }
const severityText = { critical: '严重', error: '错误', warning: '警告', info: '提示' }
const canSilence = computed(() => can('POST /api/v1/ops/silences'))
const canEditSilence = computed(() => can('PUT /api/v1/ops/silences/:id'))
const canExpire = computed(() => can('DELETE /api/v1/ops/silences/:id'))
const canMetrics = computed(() => can('menu:opsMetrics') && can('POST /api/v1/ops/metrics/query'))
const otherLabels = labels => Object.entries(labels || {}).filter(([k]) => !['alertname', 'severity'].includes(k))

// 当前告警
const alerts = ref([])
const groups = ref([])
const grouped = ref(false)
const filters = ref([])
const showSilenced = ref(true)
const showInhibited = ref(true)
const receiver = ref('')
const alertsLoading = ref(false)
const alertsError = ref('')
const autoRefresh = ref(30e3)
const alertRunner = latest()
let alertTimer = null
let filterTimer = null
const receivers = computed(() => [...new Set(alerts.value.flatMap(a => a.receivers || []))].sort())
const summary = computed(() => ({ total: alerts.value.length, active: alerts.value.filter(a => a.state === 'active').length, suppressed: alerts.value.filter(a => a.state === 'suppressed').length, critical: alerts.value.filter(a => a.labels?.severity === 'critical').length }))

async function loadAlerts() {
  alertsLoading.value = true
  const params = { matchers: filters.value.filter(m => m.name), silenced: showSilenced.value, inhibited: showInhibited.value, receiver: receiver.value }
  try {
    const [a, g] = await alertRunner.run(signal => Promise.all([opsGet('/api/v1/ops/alerts', params, signal), grouped.value ? opsGet('/api/v1/ops/alerts/groups', params, signal) : Promise.resolve({ items: groups.value })]))
    alerts.value = (a.items || []).sort((x, y) => Date.parse(y.startsAt) - Date.parse(x.startsAt))
    groups.value = g.items || []
    alertsError.value = ''
  } catch (e) { if (e?.name !== 'AbortError') alertsError.value = opsErrorText(e) } finally { alertsLoading.value = false }
}
function scheduleAlerts() {
  clearInterval(alertTimer)
  if (autoRefresh.value > 0) alertTimer = setInterval(() => { if (!document.hidden && tab.value === 'current') loadAlerts() }, autoRefresh.value)
}
watch([showSilenced, showInhibited, receiver, grouped], loadAlerts)
watch(filters, () => { clearTimeout(filterTimer); filterTimer = setTimeout(loadAlerts, 500) }, { deep: true })
watch(autoRefresh, scheduleAlerts)
function openExpr(alert) { emit('navigate', 'opsMetrics', { query: alert.expr, range: { from: 'now-6h', to: 'now' } }) }
function filterBy(name, value) { filters.value = [...filters.value.filter(m => m.name !== name), { name, op: '=', value }] }

// 规则
const rules = ref([])
const ruleWarnings = ref([])
const rulesLoading = ref(false)
const rulesError = ref('')
const ruleState = ref('')
const ruleRows = computed(() => rules.value.flatMap(group => group.rules.filter(r => r.kind === 'alert').map(rule => ({ ...rule, group: group.name, source: group.source, managed: group.managed, key: `${group.source}/${group.file}/${group.name}/${rule.name}` }))).filter(r => !ruleState.value || r.state === ruleState.value))
const stateText = { firing: '触发中', pending: '等待持续时间', inactive: '未触发' }
const stateTone = { firing: 'danger', pending: 'warning', inactive: 'success' }
async function loadRules() {
  rulesLoading.value = true
  try {
    const data = await opsGet('/api/v1/ops/alerts/rules')
    rules.value = data.items || []
    ruleWarnings.value = data.warnings || []
    rulesError.value = ''
  } catch (e) { rulesError.value = opsErrorText(e) } finally { rulesLoading.value = false }
}
function manageRules(source) { emit('navigate', source === 'loki' ? 'opsLogs' : 'opsMetrics', { tab: 'rules' }) }

// 历史
const historyRange = ref({ from: 'now-24h', to: 'now' })
const history = ref([])
const historyTruncated = ref(false)
const historyStep = ref(0)
const historyLoading = ref(false)
const historyError = ref('')
const historyWindow = ref({ from: 0, to: 0 })
const historyRunner = latest()
async function loadHistory() {
  const { from, to } = resolveRange(historyRange.value)
  historyLoading.value = true
  try {
    const data = await historyRunner.run(signal => opsGet('/api/v1/ops/alerts/history', { start: from, end: to }, signal))
    history.value = (data.items || []).sort((a, b) => b.start - a.start)
    historyTruncated.value = Boolean(data.truncated)
    historyStep.value = data.stepMs || 0
    historyWindow.value = { from, to }
    historyError.value = ''
  } catch (e) { if (e?.name !== 'AbortError') historyError.value = opsErrorText(e) } finally { historyLoading.value = false }
}
function barStyle(item) {
  const { from, to } = historyWindow.value
  const span = to - from || 1
  const left = Math.max(0, (item.start - from) / span) * 100
  const right = Math.min(1, ((item.active ? to : item.end) - from) / span) * 100
  return { left: `${left}%`, width: `${Math.max(0.5, right - left)}%` }
}
watch(historyRange, loadHistory, { deep: true })

// 静默
const silences = ref([])
const silenceState = ref('active')
const silencesLoading = ref(false)
const silencesError = ref('')
const silenceVisible = ref(false)
const editingSilence = ref(null)
const silenceText = { active: '生效中', pending: '未开始', expired: '已过期' }
const visibleSilences = computed(() => silences.value.filter(s => !silenceState.value || s.state === silenceState.value))
const matcherText = m => `${m.name}${m.isEqual ? (m.isRegex ? '=~' : '=') : m.isRegex ? '!~' : '!='}"${m.value}"`
async function loadSilences() {
  silencesLoading.value = true
  try { silences.value = (await opsGet('/api/v1/ops/silences')).items || []; silencesError.value = '' } catch (e) { silencesError.value = opsErrorText(e) } finally { silencesLoading.value = false }
}
function newSilence(alert) {
  const keys = ['alertname', 'instance', 'job', 'service_name', 'severity']
  editingSilence.value = alert ? { matchers: keys.filter(k => alert.labels?.[k]).map(k => ({ name: k, op: '=', value: alert.labels[k] })), comment: '' } : null
  silenceVisible.value = true
}
function editSilence(silence, recreate = false) {
  editingSilence.value = recreate ? { matchers: silence.matchers, comment: silence.comment } : silence
  silenceVisible.value = true
}
async function expire(silence) {
  try { await UiMessageBox.confirm('立即解除这个静默？匹配的告警会恢复通知。', '解除静默', { confirmButtonText: '解除' }) } catch { return }
  try {
    await opsSend('DELETE', `/api/v1/ops/silences/${encodeURIComponent(silence.id)}`)
    UiMessage.success('静默已解除')
    loadSilences()
    loadAlerts()
  } catch (e) { UiMessage.error(opsErrorText(e)) }
}
function silenceSaved() { loadSilences(); loadAlerts() }

watch(tab, value => {
  if (value === 'rules' && !rules.value.length) loadRules()
  if (value === 'history' && !history.value.length) loadHistory()
  if (value === 'silences') loadSilences()
  if (value === 'current') loadAlerts()
})
onMounted(() => {
  const nav = takeNavigation()
  if (nav?.tab) tab.value = nav.tab
  if (nav?.matchers) filters.value = nav.matchers
  loadAlerts()
  scheduleAlerts()
  if (tab.value === 'silences') loadSilences()
  if (tab.value === 'rules') loadRules()
  if (tab.value === 'history') loadHistory()
})
onBeforeUnmount(() => { clearInterval(alertTimer); clearTimeout(filterTimer); alertRunner.cancel(); historyRunner.cancel() })
</script>

<template>
  <div class="ops-page">
    <ui-tabs v-model="tab">
      <ui-tab-pane name="current" :label="`当前告警${summary.active ? `（${summary.active}）` : ''}`">
        <div class="alert-panel">
          <div class="alert-toolbar">
            <div class="alert-summary">
              <span>共 {{ summary.total }} 条</span><span class="tone-danger">严重 {{ summary.critical }}</span><span>通知中 {{ summary.active }}</span><span>已静默 / 抑制 {{ summary.suppressed }}</span>
            </div>
            <div class="alert-toolbar__right">
              <ui-checkbox v-model="showSilenced">含已静默</ui-checkbox>
              <ui-checkbox v-model="showInhibited">含已抑制</ui-checkbox>
              <ui-select v-model="receiver" size="small" clearable placeholder="全部接收人" aria-label="接收人" class="w-160"><ui-option v-for="r in receivers" :key="r" :value="r" :label="r" /></ui-select>
              <ui-radio-group v-model="grouped" size="small" class="segmented-choice-group" aria-label="显示方式"><ui-radio-button :value="false">列表</ui-radio-button><ui-radio-button :value="true">按通知分组</ui-radio-button></ui-radio-group>
              <ui-select v-model="autoRefresh" size="small" aria-label="自动刷新" class="w-120"><ui-option v-for="r in refreshOptions" :key="r.value" :value="r.value" :label="r.label" /></ui-select>
              <ui-button size="small" :loading="alertsLoading" @click="loadAlerts"><RefreshCw />刷新</ui-button>
              <ui-button v-if="canSilence" size="small" @click="newSilence(null)"><BellOff />新建静默</ui-button>
            </div>
          </div>
          <MatcherEditor v-model="filters" :labels="labelHints" add-text="按标签筛选" :max="8" />
          <ui-alert v-if="alertsError" type="error" :title="alertsError" :closable="false" show-icon />

          <template v-if="!grouped">
            <ui-table :data="alerts" size="small" row-key="fingerprint" :empty-text="alertsLoading ? '正在读取…' : '当前没有监控告警'">
              <ui-table-column label="级别" width="80"><template #default="{ row }"><StatusDot :tone="severityTone[row.labels.severity] || 'neutral'" :label="severityText[row.labels.severity] || row.labels.severity || '—'" /></template></ui-table-column>
              <ui-table-column label="告警" min-width="260">
                <template #default="{ row }">
                  <button type="button" class="link" @click="filterBy('alertname', row.labels.alertname)">{{ row.labels.alertname }}</button>
                  <p class="alert-summary-text">{{ row.annotations?.summary || row.annotations?.description || '' }}</p>
                </template>
              </ui-table-column>
              <ui-table-column label="标签" min-width="260"><template #default="{ row }"><span class="labels"><ui-tag v-for="[k, v] in otherLabels(row.labels)" :key="k" size="small">{{ k }}={{ v }}</ui-tag></span></template></ui-table-column>
              <ui-table-column label="开始" width="120"><template #default="{ row }"><span :title="new Date(row.startsAt).toLocaleString('zh-CN', { hour12: false })">{{ relativeTime(row.startsAt) }}</span></template></ui-table-column>
              <ui-table-column label="通知状态" width="120"><template #default="{ row }"><StatusDot :tone="row.state === 'active' ? 'danger' : 'neutral'" :label="row.state === 'active' ? '通知中' : row.silencedBy?.length ? '已静默' : row.inhibitedBy?.length ? '已抑制' : row.state" /></template></ui-table-column>
              <ui-table-column label="接收人" min-width="120"><template #default="{ row }">{{ (row.receivers || []).join('、') || '—' }}</template></ui-table-column>
              <ui-table-column label="操作" width="130">
                <template #default="{ row }">
                  <div class="table-actions table-actions--start">
                    <ui-button v-if="canMetrics && row.expr" text size="small" title="在指标中心查看表达式" aria-label="在指标中心查看表达式" @click="openExpr(row)"><LineChart /></ui-button>
                    <ui-button v-if="canSilence" text size="small" @click="newSilence(row)">静默</ui-button>
                  </div>
                </template>
              </ui-table-column>
            </ui-table>
          </template>
          <template v-else>
            <ui-empty v-if="!groups.length && !alertsLoading" description="当前没有监控告警" :image-size="64" />
            <ui-card v-for="(group, index) in groups" :key="index" shadow="never" class="surface-card">
              <template #header><div class="card-header"><div><strong>{{ Object.entries(group.labels).map(([k, v]) => `${k}=${v}`).join('，') || '未分组' }}</strong><small>接收人 {{ group.receiver }} · {{ group.alerts.length }} 条告警</small></div></div></template>
              <div v-for="alert in group.alerts" :key="alert.fingerprint" class="group-alert">
                <StatusDot :tone="severityTone[alert.labels.severity] || 'neutral'" :label="alert.labels.alertname" />
                <span class="labels"><ui-tag v-for="[k, v] in otherLabels(alert.labels)" :key="k" size="small">{{ k }}={{ v }}</ui-tag></span>
                <small>{{ relativeTime(alert.startsAt) }}开始</small>
              </div>
            </ui-card>
          </template>
        </div>
      </ui-tab-pane>

      <ui-tab-pane name="rules" label="告警规则">
        <div class="alert-panel">
          <div class="alert-toolbar">
            <p class="muted">指标告警由 Prometheus 评估，日志告警由 Loki ruler 评估；规则在指标中心与日志中心维护。</p>
            <div class="alert-toolbar__right">
              <ui-select v-model="ruleState" size="small" clearable placeholder="全部状态" aria-label="规则状态" class="w-120"><ui-option value="firing" label="触发中" /><ui-option value="pending" label="等待持续时间" /><ui-option value="inactive" label="未触发" /></ui-select>
              <ui-button size="small" :loading="rulesLoading" @click="loadRules"><RefreshCw />刷新</ui-button>
              <ui-button v-if="can('menu:opsMetrics')" size="small" @click="manageRules('prometheus')">管理指标规则</ui-button>
              <ui-button v-if="can('menu:opsLogs')" size="small" @click="manageRules('loki')">管理日志规则</ui-button>
            </div>
          </div>
          <ui-alert v-if="rulesError" type="error" :title="rulesError" :closable="false" />
          <ui-alert v-for="w in ruleWarnings" :key="w" type="warning" :title="w" :closable="false" />
          <ui-table :data="ruleRows" size="small" row-key="key" :empty-text="rulesLoading ? '正在读取…' : '没有告警规则'">
            <ui-table-column label="状态" width="130"><template #default="{ row }"><StatusDot :tone="stateTone[row.state] || 'neutral'" :label="`${stateText[row.state] || row.state || '未知'}${row.activeAlerts ? `（${row.activeAlerts}）` : ''}`" /></template></ui-table-column>
            <ui-table-column label="规则" min-width="200"><template #default="{ row }"><strong>{{ row.name }}</strong><p class="alert-summary-text">{{ row.annotations?.summary || '' }}</p></template></ui-table-column>
            <ui-table-column label="来源" width="150"><template #default="{ row }">{{ row.source === 'loki' ? '日志（Loki）' : '指标（Prometheus）' }}<br /><small class="muted">{{ row.group }}{{ row.managed ? '' : ' · 部署配置' }}</small></template></ui-table-column>
            <ui-table-column label="级别" width="80"><template #default="{ row }">{{ severityText[row.labels?.severity] || row.labels?.severity || '—' }}</template></ui-table-column>
            <ui-table-column label="表达式" min-width="280"><template #default="{ row }"><code class="expr">{{ row.expr }}</code></template></ui-table-column>
            <ui-table-column label="持续" width="70"><template #default="{ row }">{{ row.for || '—' }}</template></ui-table-column>
            <ui-table-column label="评估" min-width="140"><template #default="{ row }"><span v-if="row.lastError" class="tone-danger">{{ row.lastError }}</span><span v-else class="muted">{{ row.lastEvaluation && !row.lastEvaluation.startsWith('0001') ? relativeTime(row.lastEvaluation) : '—' }}</span></template></ui-table-column>
          </ui-table>
        </div>
      </ui-tab-pane>

      <ui-tab-pane name="history" label="告警历史">
        <div class="alert-panel">
          <div class="alert-toolbar">
            <p class="muted">根据 Prometheus 记录的 ALERTS 序列还原指标告警的触发区间，最长可查询 31 天<template v-if="historyStep">，当前精度约 {{ formatDuration(historyStep / 1000) }}</template>。日志告警（Loki ruler）不写入该序列，历史中不包含。</p>
            <TimeRangeBar v-model:range="historyRange" :show-refresh="false" :loading="historyLoading" :max-hours="24 * 31" @refresh="loadHistory" />
          </div>
          <ui-alert v-if="historyError" type="error" :title="historyError" :closable="false" />
          <ui-alert v-if="historyTruncated" type="warning" :closable="false" title="结果过多，只显示部分告警序列，请缩小时间范围" />
          <ui-table :data="history" size="small" :row-key="row => `${JSON.stringify(row.labels)}-${row.start}`" :empty-text="historyLoading ? '正在读取…' : '所选时间范围内没有触发过的告警'">
            <ui-table-column label="告警" min-width="200"><template #default="{ row }"><StatusDot :tone="severityTone[row.labels.severity] || 'neutral'" :label="row.labels.alertname" /><span class="labels"><ui-tag v-for="[k, v] in otherLabels(row.labels).filter(([k]) => k !== 'alertstate')" :key="k" size="small">{{ k }}={{ v }}</ui-tag></span></template></ui-table-column>
            <ui-table-column label="开始" width="170"><template #default="{ row }">{{ new Date(row.start).toLocaleString('zh-CN', { hour12: false }) }}</template></ui-table-column>
            <ui-table-column label="结束" width="170"><template #default="{ row }"><span v-if="row.active" class="tone-danger">仍在触发</span><span v-else>{{ new Date(row.end).toLocaleString('zh-CN', { hour12: false }) }}</span></template></ui-table-column>
            <ui-table-column label="持续" width="120"><template #default="{ row }">{{ formatDuration(((row.active ? Date.now() : row.end) - row.start) / 1000) }}</template></ui-table-column>
            <ui-table-column label="时间线" min-width="220"><template #default="{ row }"><div class="timeline"><span class="timeline__bar" :class="{ 'is-active': row.active }" :style="barStyle(row)" /></div></template></ui-table-column>
          </ui-table>
        </div>
      </ui-tab-pane>

      <ui-tab-pane name="silences" label="静默">
        <div class="alert-panel">
          <div class="alert-toolbar">
            <ui-radio-group v-model="silenceState" size="small" class="segmented-choice-group" aria-label="静默状态"><ui-radio-button value="active">生效中</ui-radio-button><ui-radio-button value="pending">未开始</ui-radio-button><ui-radio-button value="expired">已过期</ui-radio-button><ui-radio-button value="">全部</ui-radio-button></ui-radio-group>
            <div class="alert-toolbar__right">
              <ui-button size="small" :loading="silencesLoading" @click="loadSilences"><RefreshCw />刷新</ui-button>
              <ui-button v-if="canSilence" size="small" type="primary" @click="newSilence(null)"><BellOff />新建静默</ui-button>
            </div>
          </div>
          <ui-alert v-if="silencesError" type="error" :title="silencesError" :closable="false" />
          <ui-table :data="visibleSilences" size="small" row-key="id" :empty-text="silencesLoading ? '正在读取…' : '没有静默'">
            <ui-table-column label="状态" width="90"><template #default="{ row }"><StatusDot :tone="row.state === 'active' ? 'warning' : row.state === 'pending' ? 'info' : 'neutral'" :label="silenceText[row.state] || row.state" /></template></ui-table-column>
            <ui-table-column label="匹配条件" min-width="260"><template #default="{ row }"><span class="labels"><ui-tag v-for="m in row.matchers" :key="matcherText(m)" size="small">{{ matcherText(m) }}</ui-tag></span></template></ui-table-column>
            <ui-table-column label="原因" min-width="180"><template #default="{ row }">{{ row.comment }}</template></ui-table-column>
            <ui-table-column label="时间" min-width="220"><template #default="{ row }">{{ new Date(row.startsAt).toLocaleString('zh-CN', { hour12: false }) }} 至 {{ new Date(row.endsAt).toLocaleString('zh-CN', { hour12: false }) }}</template></ui-table-column>
            <ui-table-column label="创建人" width="120"><template #default="{ row }">{{ row.createdBy }}</template></ui-table-column>
            <ui-table-column label="操作" width="130">
              <template #default="{ row }">
                <div class="table-actions table-actions--start">
                  <template v-if="row.state !== 'expired'">
                    <ui-button v-if="canEditSilence" text size="small" @click="editSilence(row)">编辑</ui-button>
                    <ui-button v-if="canExpire" text size="small" type="danger" @click="expire(row)">解除</ui-button>
                  </template>
                  <ui-button v-else-if="canSilence" text size="small" @click="editSilence(row, true)">再次静默</ui-button>
                </div>
              </template>
            </ui-table-column>
          </ui-table>
        </div>
      </ui-tab-pane>

      <ui-tab-pane name="notifications" label="通知路由">
        <NotificationPanel v-if="tab === 'notifications'" :labels="labelHints" />
      </ui-tab-pane>
    </ui-tabs>
    <SilenceDialog v-model="silenceVisible" :silence="editingSilence" :labels="labelHints" @saved="silenceSaved" />
  </div>
</template>

<style scoped>
.ops-page { display: grid; gap: var(--space-4); min-width: 0; }
.alert-panel { display: grid; gap: var(--space-3); min-width: 0; }
.alert-toolbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: var(--space-2); }
.alert-toolbar__right { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-2); }
.alert-summary { display: flex; flex-wrap: wrap; gap: var(--space-3); color: var(--text-secondary); font-size: var(--font-size-sm); }
.w-120 { width: 120px; }
.w-160 { width: 160px; }
.link { padding: 0; color: var(--primary-text); font-weight: var(--font-weight-semibold); background: none; border: 0; cursor: pointer; }
.alert-summary-text { margin: 2px 0 0; color: var(--text-secondary); font-size: var(--font-size-xs); }
.labels { display: inline-flex; flex-wrap: wrap; gap: 4px; }
.group-alert { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-2); padding: 6px 0; border-bottom: 1px solid var(--border); }
.group-alert small, .muted { margin: 0; color: var(--text-muted); font-size: var(--font-size-xs); }
.expr { display: block; max-height: 64px; overflow: auto; color: var(--code-inline-text); font: var(--font-size-xs) var(--font-mono); white-space: pre-wrap; word-break: break-all; }
.tone-danger { color: var(--danger-text); }
.timeline { position: relative; height: 10px; background: var(--surface-muted); border-radius: 3px; }
.timeline__bar { position: absolute; top: 0; bottom: 0; background: var(--warning); border-radius: 3px; }
.timeline__bar.is-active { background: var(--danger); }
</style>
