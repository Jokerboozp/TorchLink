<script setup>
// 规则组列表：部署内置的规则只读，平台管理的规则组可编辑、启停与删除。
import { computed, onMounted, ref } from 'vue'
import { Plus, RefreshCw } from '@lucide/vue'
import { can } from '../../permissions'
import { UiMessage, UiMessageBox } from '../../ui/feedback.js'
import { opsErrorText, opsGet, opsSend } from '../../ops/opsApi.js'
import { relativeTime } from '../../ops/format.js'
import StatusDot from '../layout/StatusDot.vue'
import RuleGroupEditor from './RuleGroupEditor.vue'

const props = defineProps({ source: { type: String, required: true } })
const groups = ref([])
const writable = ref(false)
const loading = ref(false)
const error = ref('')
const busy = ref('')
const editorVisible = ref(false)
const editing = ref(null)
const isLoki = computed(() => props.source === 'loki')
const base = computed(() => (isLoki.value ? '/api/v1/ops/logs' : '/api/v1/ops/metrics'))
const permission = method => `${method} ${base.value}/rule-groups${method === 'POST' ? '' : '/:name'}`
const canCreate = computed(() => writable.value && can(permission('POST')))
const canEdit = computed(() => writable.value && can(permission('PUT')))
const canDelete = computed(() => writable.value && can(permission('DELETE')))
const stateTone = { firing: 'danger', pending: 'warning', inactive: 'success' }
const stateText = { firing: '触发中', pending: '等待持续时间', inactive: '未触发' }
const healthTone = { ok: 'success', err: 'danger', unknown: 'neutral' }
const healthText = { ok: '评估正常', err: '评估出错', unknown: '尚未评估' }

async function load() {
  loading.value = true
  try {
    const data = await opsGet(`${base.value}/rules`)
    groups.value = data.items || []
    writable.value = Boolean(data.writable)
    error.value = ''
  } catch (e) { error.value = opsErrorText(e) } finally { loading.value = false }
}
function edit(group) { editing.value = group; editorVisible.value = true }
function create() { editing.value = null; editorVisible.value = true }

async function toggle(group) {
  busy.value = group.name
  try {
    await opsSend('PUT', `${base.value}/rule-groups/${encodeURIComponent(group.name)}`, { name: group.name, revision: group.revision, toggle: !group.enabled, rules: [] })
    UiMessage.success(group.enabled ? '规则组已停用' : '规则组已启用')
    load()
  } catch (e) {
    UiMessage.error([opsErrorText(e), e.details?.reason].filter(Boolean).join('：'))
  } finally { busy.value = '' }
}

async function remove(group) {
  try {
    await UiMessageBox.confirm(`删除规则组“${group.name}”及其 ${group.rules.length} 条规则？删除后组件将停止评估这些规则。`, '删除规则组', { confirmButtonText: '删除' })
  } catch { return }
  busy.value = group.name
  try {
    await opsSend('DELETE', `${base.value}/rule-groups/${encodeURIComponent(group.name)}?revision=${encodeURIComponent(group.revision)}`)
    UiMessage.success('规则组已删除')
    load()
  } catch (e) { UiMessage.error(opsErrorText(e)) } finally { busy.value = '' }
}
onMounted(load)
defineExpose({ load })
</script>

<template>
  <div class="rule-panel">
    <div class="rule-panel__toolbar">
      <p>{{ isLoki ? '日志告警规则由 Loki ruler 评估，告警发送到 Alertmanager。' : '记录规则与告警规则由 Prometheus 评估，告警发送到 Alertmanager。' }}<span v-if="!writable">当前部署未配置规则目录，只能查看。</span></p>
      <div>
        <ui-button size="small" :loading="loading" @click="load"><RefreshCw />刷新</ui-button>
        <ui-button v-if="canCreate" size="small" type="primary" @click="create"><Plus />新建规则组</ui-button>
      </div>
    </div>
    <ui-alert v-if="error" type="error" :title="error" :closable="false" show-icon />
    <ui-empty v-if="!groups.length && !loading && !error" description="暂无规则" :image-size="64" />
    <ui-card v-for="group in groups" :key="`${group.file}/${group.name}`" shadow="never" class="surface-card rule-group">
      <template #header>
        <div class="card-header">
          <div>
            <strong>{{ group.name }}</strong>
            <small>{{ group.managed ? `平台管理${group.updatedBy ? ` · ${group.updatedBy} ${relativeTime(group.updatedAt)}更新` : ''}` : `部署配置 · ${group.file}` }}{{ group.interval ? ` · 每 ${group.interval} 评估` : '' }}</small>
          </div>
          <div class="rule-group__actions">
            <StatusDot v-if="group.managed" :tone="group.enabled ? (group.loaded ? 'success' : 'warning') : 'neutral'" :label="group.enabled ? (group.loaded ? '已加载' : '等待加载') : '已停用'" />
            <ui-tag v-else size="small">只读</ui-tag>
            <template v-if="group.managed">
              <ui-button v-if="canEdit" text size="small" :disabled="busy === group.name" @click="edit(group)">编辑</ui-button>
              <ui-button v-if="canEdit" text size="small" :loading="busy === group.name" @click="toggle(group)">{{ group.enabled ? '停用' : '启用' }}</ui-button>
              <ui-button v-if="canDelete" text size="small" type="danger" :disabled="busy === group.name" @click="remove(group)">删除</ui-button>
            </template>
          </div>
        </div>
      </template>
      <ui-table :data="group.rules" size="small" :row-key="row => `${row.kind}-${row.name}-${row.expr}`">
        <ui-table-column label="类型" width="90"><template #default="{ row }"><ui-tag size="small" :type="row.kind === 'alert' ? 'warning' : 'info'">{{ row.kind === 'alert' ? '告警' : '记录' }}</ui-tag></template></ui-table-column>
        <ui-table-column prop="name" label="名称" min-width="170" show-overflow-tooltip />
        <ui-table-column label="表达式" min-width="300"><template #default="{ row }"><code class="rule-expr">{{ row.expr }}</code></template></ui-table-column>
        <ui-table-column label="持续" width="80"><template #default="{ row }">{{ row.for || '—' }}</template></ui-table-column>
        <ui-table-column label="摘要 / 标签" min-width="200"><template #default="{ row }"><span class="rule-summary">{{ row.annotations?.summary || Object.entries(row.labels || {}).map(([k, v]) => `${k}=${v}`).join(', ') || '—' }}</span></template></ui-table-column>
        <ui-table-column label="运行状态" min-width="150">
          <template #default="{ row }">
            <StatusDot v-if="row.kind === 'alert' && row.state" :tone="stateTone[row.state] || 'neutral'" :label="`${stateText[row.state] || row.state}${row.activeAlerts ? `（${row.activeAlerts}）` : ''}`" />
            <StatusDot v-else-if="row.health" :tone="healthTone[row.health] || 'neutral'" :label="healthText[row.health] || row.health" />
            <span v-else class="muted">{{ group.enabled ? '—' : '未加载' }}</span>
            <p v-if="row.lastError" class="rule-error">{{ row.lastError }}</p>
          </template>
        </ui-table-column>
      </ui-table>
    </ui-card>
    <RuleGroupEditor v-model="editorVisible" :source="source" :group="editing" @saved="load" />
  </div>
</template>

<style scoped>
.rule-panel { display: grid; gap: var(--space-3); min-width: 0; }
.rule-panel__toolbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: var(--space-3); }
.rule-panel__toolbar p { margin: 0; color: var(--text-muted); font-size: var(--font-size-xs); }
.rule-panel__toolbar p span { margin-left: var(--space-2); color: var(--warning-text); }
.rule-panel__toolbar > div { display: flex; gap: var(--space-2); }
.rule-group__actions { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-3); }
.rule-expr { display: block; max-height: 72px; overflow: auto; color: var(--code-inline-text); font: var(--font-size-xs) var(--font-mono); white-space: pre-wrap; word-break: break-all; }
.rule-summary { color: var(--text-secondary); font-size: var(--font-size-xs); }
.rule-error { margin: 4px 0 0; color: var(--danger-text); font-size: var(--font-size-xs); overflow-wrap: anywhere; }
.muted { color: var(--text-muted); }
.rule-group.n-card > :deep(.n-card__content) { padding: 0; }
</style>
