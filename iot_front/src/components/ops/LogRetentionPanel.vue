<script setup>
// 日志保留与删除：保留时长写入 Loki 运行时覆盖配置；删除请求由 Loki compactor 执行，
// 在取消期内可以撤回。
import { computed, onMounted, ref } from 'vue'
import { Plus, RefreshCw, Trash2 } from '@lucide/vue'
import { can } from '../../permissions'
import { UiMessage, UiMessageBox } from '../../ui/feedback.js'
import { opsErrorText, opsGet, opsSend } from '../../ops/opsApi.js'
import { relativeTime } from '../../ops/format.js'
import StatusDot from '../layout/StatusDot.vue'
import MatcherEditor from './MatcherEditor.vue'

const props = defineProps({ labels: { type: Array, default: () => [] }, services: { type: Array, default: () => [] }, loadValues: { type: Function, default: null } })
const settings = ref(null)
const loading = ref(false)
const error = ref('')
const form = ref({ period: '', streams: [] })
const saving = ref(false)
const saveError = ref('')
const canSave = computed(() => settings.value?.writable && can('PUT /api/v1/ops/logs/retention'))
const dirty = computed(() => settings.value && JSON.stringify(normalize(form.value)) !== JSON.stringify(normalize(fromSettings(settings.value))))

function fromSettings(value) {
  return { period: value.period || '', streams: (value.streams || []).map(s => ({ matchers: (s.matchers || []).map(m => ({ ...m })), priority: s.priority || 0, period: s.period || '', selector: s.selector, parsed: Boolean(s.matchers?.length) })) }
}
function normalize(value) {
  return { period: value.period.trim(), streams: value.streams.map(s => ({ matchers: s.matchers.filter(m => m.name), priority: Number(s.priority) || 0, period: String(s.period).trim() })) }
}
async function load() {
  loading.value = true
  try {
    settings.value = await opsGet('/api/v1/ops/logs/retention')
    form.value = fromSettings(settings.value)
    error.value = ''
  } catch (e) { error.value = opsErrorText(e) } finally { loading.value = false }
  loadRequests()
}
function addStream() { form.value.streams.push({ matchers: [{ name: 'service_name', op: '=', value: '' }], priority: 1, period: '168h', parsed: true }) }
async function save() {
  const body = { revision: settings.value.revision, ...normalize(form.value) }
  if (body.streams.some(s => !s.matchers.length)) { saveError.value = '每条日志流规则至少需要一个标签条件'; return }
  saving.value = true
  saveError.value = ''
  try {
    settings.value = await opsSend('PUT', '/api/v1/ops/logs/retention', body)
    form.value = fromSettings(settings.value)
    UiMessage.success('保留策略已由 Loki 加载，compactor 将在下一轮清理时按新策略删除过期日志')
  } catch (e) {
    if (e.status === 409 && e.code === 'OPS_CONFLICT') { saveError.value = '配置已被他人修改，请刷新后重新编辑'; return }
    saveError.value = [opsErrorText(e), e.details?.reason, e.details?.rolledBack ? '已恢复原配置' : ''].filter(Boolean).join('：')
  } finally { saving.value = false }
}

// 删除请求
const requests = ref([])
const requestsLoading = ref(false)
const requestsError = ref('')
const deleteVisible = ref(false)
const deleteForm = ref({ services: [], labels: [], keyword: '', range: null })
const deleting = ref(false)
const statusText = { received: '等待执行', processed: '已完成', deleted: '已取消' }
const canCreateDelete = computed(() => can('POST /api/v1/ops/logs/delete-requests'))
const canCancelDelete = computed(() => can('DELETE /api/v1/ops/logs/delete-requests/:id'))
const deletionEnabled = computed(() => settings.value && settings.value.deletionMode && settings.value.deletionMode !== 'disabled')

async function loadRequests() {
  requestsLoading.value = true
  try { requests.value = ((await opsGet('/api/v1/ops/logs/delete-requests')).items || []).sort((a, b) => b.createdAt - a.createdAt); requestsError.value = '' } catch (e) { requestsError.value = opsErrorText(e) } finally { requestsLoading.value = false }
}
function openDelete() { deleteForm.value = { services: [], labels: [], keyword: '', range: [Date.now() - 86400e3, Date.now()] }; deleteVisible.value = true }
async function submitDelete() {
  const f = deleteForm.value
  const [start, end] = f.range || []
  if (!f.services.length && !f.labels.some(m => m.name && m.value)) { UiMessage.warning('至少选择一个服务或填写一个标签条件'); return }
  if (!start || !end) { UiMessage.warning('请选择删除的时间范围'); return }
  try {
    await UiMessageBox.confirm(`将删除 ${new Date(start).toLocaleString('zh-CN', { hour12: false })} 至 ${new Date(end).toLocaleString('zh-CN', { hour12: false })} 内匹配条件的日志。删除执行后不可恢复${settings.value?.cancelPeriod ? `，提交后 ${settings.value.cancelPeriod} 内可以取消` : ''}。`, '确认删除日志', { confirmButtonText: '提交删除请求', type: 'warning' })
  } catch { return }
  deleting.value = true
  try {
    const result = await opsSend('POST', '/api/v1/ops/logs/delete-requests', { filter: { services: f.services, labels: f.labels.filter(m => m.name), keyword: f.keyword }, start, end })
    UiMessage.success(`删除请求已提交：${result.query}`)
    deleteVisible.value = false
    loadRequests()
  } catch (e) { UiMessage.error(opsErrorText(e)) } finally { deleting.value = false }
}
async function cancelRequest(item) {
  try { await UiMessageBox.confirm('取消这个删除请求？取消后对应日志不会被删除。', '取消删除请求') } catch { return }
  try {
    await opsSend('DELETE', `/api/v1/ops/logs/delete-requests/${encodeURIComponent(item.requestId)}`)
    UiMessage.success('删除请求已取消')
    loadRequests()
  } catch (e) { UiMessage.error(opsErrorText(e)) }
}
const fmtSeconds = value => (value ? new Date(value * 1000).toLocaleString('zh-CN', { hour12: false }) : '—')
onMounted(load)
</script>

<template>
  <div class="retention">
    <ui-alert v-if="error" type="error" :title="error" :closable="false" show-icon />
    <ui-card shadow="never" class="surface-card">
      <template #header>
        <div class="card-header">
          <div><strong>保留策略</strong><small>{{ settings?.updatedBy ? `${settings.updatedBy} ${relativeTime(settings.updatedAt)}修改` : '保留时长写入 Loki 运行时配置，约 10 秒内生效' }}</small></div>
          <ui-button size="small" :loading="loading" @click="load"><RefreshCw />刷新</ui-button>
        </div>
      </template>
      <template v-if="settings">
        <dl class="retention-facts">
          <div><dt>compactor 保留</dt><dd><StatusDot :tone="settings.retentionEnabled ? 'success' : 'warning'" :label="settings.retentionEnabled ? '已启用' : '未启用，保留设置不会生效'" /></dd></div>
          <div><dt>部署默认保留</dt><dd>{{ settings.globalPeriod || '—' }}</dd></div>
          <div><dt>运行时配置</dt><dd><StatusDot v-if="settings.loaded != null" :tone="settings.loaded ? 'success' : 'danger'" :label="settings.loaded ? '最近一次加载成功' : '最近一次加载失败'" /><span v-else>—</span></dd></div>
          <div><dt>删除接口</dt><dd>{{ deletionEnabled ? `已启用（${settings.deletionMode}）` : '未启用' }}</dd></div>
        </dl>
        <ui-alert v-if="!settings.writable" type="info" :closable="false" title="当前部署未配置 Loki 运行时配置文件（IOT_OPS_LOKI_RUNTIME_FILE），只能查看。" />
        <div class="retention-form">
          <label class="retention-form__period">当前租户保留时长
            <ui-input v-model="form.period" size="small" :disabled="!canSave" :placeholder="`留空使用部署默认（${settings.globalPeriod || '未设置'}）`" aria-label="保留时长" />
            <small>至少 24h，例如 168h、30d；删除按天进行，实际清理可能滞后一个 compactor 周期。</small>
          </label>
          <div class="retention-streams">
            <div class="retention-streams__head"><strong>按日志流单独保留</strong><small>匹配多条规则时取优先级高的规则</small></div>
            <article v-for="(stream, index) in form.streams" :key="index" class="stream-rule">
              <ui-alert v-if="!stream.parsed" type="warning" :closable="false" :title="`无法解析的选择器：${stream.selector}，保存将删除此规则`" />
              <MatcherEditor v-model="stream.matchers" :labels="labels" :load-values="loadValues" add-text="添加条件" :max="5" />
              <div class="stream-rule__meta">
                <label>保留时长<ui-input v-model="stream.period" size="small" :disabled="!canSave" placeholder="例如 72h" /></label>
                <label>优先级<ui-input-number v-model="stream.priority" size="small" :min="0" :max="1000" :disabled="!canSave" /></label>
                <ui-button v-if="canSave" text size="small" type="danger" @click="form.streams.splice(index, 1)"><Trash2 />删除规则</ui-button>
              </div>
            </article>
            <ui-button v-if="canSave && form.streams.length < 50" size="small" text type="primary" @click="addStream"><Plus />添加日志流规则</ui-button>
          </div>
          <ui-alert v-if="saveError" type="error" :title="saveError" :closable="false" show-icon />
          <div v-if="canSave" class="retention-form__actions">
            <ui-button size="small" :disabled="!dirty || saving" @click="form = fromSettings(settings)">还原</ui-button>
            <ui-button size="small" type="primary" :loading="saving" :disabled="!dirty" @click="save">保存并加载</ui-button>
          </div>
        </div>
      </template>
      <ui-skeleton v-else-if="loading" :rows="3" animated />
    </ui-card>

    <ui-card shadow="never" class="surface-card">
      <template #header>
        <div class="card-header">
          <div><strong>删除请求</strong><small>按服务、标签和时间范围删除已写入的日志</small></div>
          <div class="retention__actions">
            <ui-button size="small" :loading="requestsLoading" @click="loadRequests"><RefreshCw />刷新</ui-button>
            <ui-button v-if="canCreateDelete" size="small" type="danger" plain :disabled="!deletionEnabled" @click="openDelete"><Trash2 />新建删除请求</ui-button>
          </div>
        </div>
      </template>
      <ui-alert v-if="settings && !deletionEnabled" type="info" :closable="false" title="Loki 未启用删除接口（需要 compactor 保留与 deletion_mode），当前只能查看。" />
      <ui-alert v-if="requestsError" type="error" :title="requestsError" :closable="false" />
      <ui-table :data="requests" size="small" :row-key="row => `${row.requestId}-${row.startTime}`" empty-text="暂无删除请求">
        <ui-table-column label="条件" min-width="280"><template #default="{ row }"><code class="query-code">{{ row.query }}</code></template></ui-table-column>
        <ui-table-column label="时间范围" min-width="260"><template #default="{ row }">{{ fmtSeconds(row.startTime) }} 至 {{ fmtSeconds(row.endTime) }}</template></ui-table-column>
        <ui-table-column label="状态" width="110"><template #default="{ row }"><StatusDot :tone="row.status === 'processed' ? 'success' : row.status === 'received' ? 'warning' : 'neutral'" :label="statusText[row.status] || row.status" /></template></ui-table-column>
        <ui-table-column label="提交时间" width="120"><template #default="{ row }">{{ row.createdAt ? relativeTime(row.createdAt * 1000) : '—' }}</template></ui-table-column>
        <ui-table-column label="操作" width="90"><template #default="{ row }"><ui-button v-if="canCancelDelete && row.status === 'received'" text size="small" @click="cancelRequest(row)">取消</ui-button></template></ui-table-column>
      </ui-table>
    </ui-card>

    <ui-dialog v-model="deleteVisible" title="新建日志删除请求" width="min(620px, 94vw)">
      <div class="delete-form">
        <label>服务<ui-select v-model="deleteForm.services" multiple filterable clearable placeholder="选择服务" aria-label="服务"><ui-option v-for="item in services" :key="item" :value="item" :label="item" /></ui-select></label>
        <div class="delete-form__labels"><span>其他标签</span><MatcherEditor v-model="deleteForm.labels" :labels="labels.filter(l => l !== 'service_name')" :load-values="loadValues" :max="5" /></div>
        <label>包含关键词（可选）<ui-input v-model="deleteForm.keyword" placeholder="只删除包含该文本的日志行" /></label>
        <label>时间范围<ui-date-range v-model="deleteForm.range" disable-future /></label>
        <p class="delete-form__hint">删除由 Loki compactor 异步执行{{ settings?.cancelPeriod ? `，提交后 ${settings.cancelPeriod} 内可取消` : '' }}。至少需要一个服务或标签条件，不能删除全部日志。</p>
      </div>
      <template #footer><ui-button @click="deleteVisible = false">取消</ui-button><ui-button type="danger" :loading="deleting" @click="submitDelete">提交</ui-button></template>
    </ui-dialog>
  </div>
</template>

<style scoped>
.retention { display: grid; gap: var(--space-4); min-width: 0; }
.retention__actions { display: flex; flex-wrap: wrap; gap: var(--space-2); }
.retention-facts { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: var(--space-3); margin: 0 0 var(--space-3); }
.retention-facts dt { color: var(--text-muted); font-size: var(--font-size-xs); }
.retention-facts dd { margin: 4px 0 0; color: var(--text); font-size: var(--font-size-sm); }
.retention-form { display: grid; gap: var(--space-4); }
.retention-form__period { display: grid; gap: 6px; max-width: 420px; color: var(--text-secondary); font-size: var(--font-size-sm); }
.retention-form__period small, .retention-streams__head small, .delete-form__hint { color: var(--text-muted); font-size: var(--font-size-xs); }
.retention-streams { display: grid; gap: var(--space-2); }
.retention-streams__head { display: flex; align-items: baseline; flex-wrap: wrap; gap: var(--space-2); }
.stream-rule { display: grid; gap: var(--space-2); padding: var(--space-3); background: var(--surface-muted); border: 1px solid var(--border); border-radius: var(--radius-md); }
.stream-rule__meta { display: flex; align-items: end; flex-wrap: wrap; gap: var(--space-3); }
.stream-rule__meta label { display: grid; gap: 4px; color: var(--text-secondary); font-size: var(--font-size-xs); }
.stream-rule__meta .ui-input { width: 140px; }
.retention-form__actions { display: flex; justify-content: flex-end; gap: var(--space-2); }
.query-code { color: var(--code-inline-text); font: var(--font-size-xs) var(--font-mono); word-break: break-all; }
.delete-form { display: grid; gap: var(--space-3); }
.delete-form label, .delete-form__labels { display: grid; gap: 6px; color: var(--text-secondary); font-size: var(--font-size-sm); }
.delete-form__hint { margin: 0; }
</style>
