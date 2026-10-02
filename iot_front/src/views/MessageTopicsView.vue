<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { RefreshCw } from '@lucide/vue'
import { api, session } from '../api'
import { can, permissionState } from '../permissions'
import { UiMessage } from '../ui/feedback.js'
import { filterMessageTopics, topicDirectionLabel, topicDirections, topicStatus, topicVariableLabel, validateTopicTarget } from '../messageTopics.js'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'

defineEmits(['navigate'])

const snapshot = ref(null)
const loading = ref(false)
const saving = ref(false)
const error = ref('')
const formError = ref('')
const conflicted = ref(false)
const editing = ref(null)
const filters = reactive({ protocol:'', direction:'', keyword:'' })
const form = reactive({ enabled:true, topic:'', description:'' })
const items = computed(() => snapshot.value?.items || [])
const filteredItems = computed(() => filterMessageTopics(items.value, filters))
const emptyText = computed(() => loading.value ? '正在读取消息主题…' : items.value.length ? '没有符合筛选条件的主题' : '当前环境没有可展示的消息主题')
const busy = computed(() => loading.value || saving.value)
const editorOpen = computed({ get:() => Boolean(editing.value), set:value => { if (!value && !saving.value) editing.value = null } })
const canSave = computed(() => can('PUT /api/v1/message-topics/:id'))
const canReset = computed(() => can('DELETE /api/v1/message-topics/:id'))
let requestVersion = 0
let controller = null
let disposed = false
let loadedIdentity = ''
let editorIdentity = ''
let editorRevision = 0

function identityKey() {
  return [session.token, session.tenant, session.user, permissionState.accessVersion, ...permissionState.items].join('\n')
}

function invalidate() {
  requestVersion++
  controller?.abort()
  controller = null
}

function beginRequest() {
  invalidate()
  controller = new AbortController()
  return { version:requestVersion, identity:identityKey(), signal:controller.signal }
}

function current(request) {
  return !disposed && request.version === requestVersion && request.identity === identityKey()
}

function clearIdentity() {
  invalidate()
  snapshot.value = null
  editing.value = null
  loading.value = false
  saving.value = false
  conflicted.value = false
  error.value = ''
  formError.value = ''
  loadedIdentity = ''
}

async function load() {
  if (disposed || saving.value) return
  if (!can('menu:messageTopics')) { clearIdentity(); return }
  const request = beginRequest()
  editing.value = null
  snapshot.value = null
  error.value = ''
  conflicted.value = false
  loading.value = true
  try {
    const result = await api('/api/v1/message-topics', { signal:request.signal })
    if (!current(request)) return
    snapshot.value = result
    loadedIdentity = request.identity
  } catch (cause) {
    if (current(request) && cause.name !== 'AbortError') error.value = cause.message || '消息主题读取失败'
  } finally {
    if (current(request)) loading.value = false
  }
}

function openEditor(row) {
  if (!row.editable || busy.value || conflicted.value || !canSave.value || loadedIdentity !== identityKey()) return
  editing.value = row
  editorIdentity = loadedIdentity
  editorRevision = snapshot.value.revision
  Object.assign(form, { enabled:row.enabled, topic:row.topic, description:row.description || '' })
  formError.value = ''
}

async function mutate(row, reset = false) {
  const authorized = reset ? canReset.value : canSave.value
  if (!row?.editable || !authorized || busy.value || conflicted.value || loadedIdentity !== identityKey()) return
  if (!reset && (editorIdentity !== identityKey() || editing.value?.id !== row.id)) return
  formError.value = ''
  error.value = ''
  if (!reset) {
    formError.value = validateTopicTarget(row, form.topic, snapshot.value.prefixes)
    if (formError.value) return
  }
  const revision = reset ? snapshot.value.revision : editorRevision
  const path = `/api/v1/message-topics/${encodeURIComponent(row.id)}`
  const request = beginRequest()
  saving.value = true
  try {
    const result = await api(reset ? `${path}?revision=${encodeURIComponent(revision)}` : path, {
      method:reset ? 'DELETE' : 'PUT', signal:request.signal,
      ...(reset ? {} : { body:JSON.stringify({ revision, enabled:form.enabled, topic:form.topic.trim(), description:form.description.trim() }) })
    })
    if (!current(request)) return
    snapshot.value = result
    loadedIdentity = request.identity
    editing.value = null
    UiMessage.success(reset ? '已恢复默认配置' : '主题配置已保存')
  } catch (cause) {
    if (!current(request) || cause.name === 'AbortError') return
    conflicted.value = cause.status === 409
    const message = conflicted.value ? '主题配置已被其他操作更新，请刷新后重新编辑。' : cause.message || '主题配置保存失败'
    if (editing.value) formError.value = message
    else error.value = message
  } finally {
    if (current(request)) saving.value = false
  }
}

function save() { return mutate(editing.value) }
function reset(row) { return mutate(row, true) }

async function copyTopic(row) {
  if (loadedIdentity !== identityKey()) return
  const identity = identityKey()
  try {
    await navigator.clipboard.writeText(row.topic)
    if (!disposed && identity === identityKey()) UiMessage.success('主题模板已复制；订阅时请替换其中的变量')
  } catch {
    if (!disposed && identity === identityKey()) UiMessage.warning('自动复制失败，请选中主题文本复制')
  }
}

function rowActions(row) {
  return [
    { key:'copy', label:'复制', disabled:busy.value, onClick:() => copyTopic(row) },
    { key:'edit', label:'编辑', hidden:!row.editable, permission:'PUT /api/v1/message-topics/:id', disabled:busy.value || conflicted.value, onClick:() => openEditor(row) },
    { key:'reset', label:'恢复默认', hidden:!row.editable || !row.overridden, permission:'DELETE /api/v1/message-topics/:id', disabled:busy.value || conflicted.value, onClick:() => reset(row) }
  ]
}

function identityChanged() {
  clearIdentity()
  if (!disposed && can('menu:messageTopics')) load()
}

function storageChanged(event) {
  if (!event.key || ['iot_token', 'iot_tenant', 'iot_user', 'iot_permissions', 'iot_access_version'].includes(event.key)) identityChanged()
}

watch(() => [permissionState.accessVersion, ...permissionState.items].join('\n'), identityChanged, { flush:'sync' })
onMounted(() => { window.addEventListener('storage', storageChanged); load() })
onBeforeUnmount(() => { disposed = true; invalidate(); window.removeEventListener('storage', storageChanged) })
</script>

<template>
  <div class="message-topics-page">
    <FilterBar>
      <ui-input v-model="filters.keyword" clearable placeholder="搜索名称、主题或说明" aria-label="搜索消息主题" />
      <ui-select v-model="filters.protocol" clearable placeholder="全部协议" aria-label="消息协议"><ui-option label="MQTT" value="mqtt" /><ui-option label="Kafka" value="kafka" /></ui-select>
      <ui-select v-model="filters.direction" clearable placeholder="全部用途" aria-label="消息用途"><ui-option v-for="option in topicDirections" :key="option.value" :label="option.label" :value="option.value" /></ui-select>
      <template #actions><ui-button :loading="loading" :disabled="saving" @click="load"><RefreshCw />刷新</ui-button></template>
    </FilterBar>

    <div v-if="snapshot" class="topic-runtime" aria-label="消息通道配置">
      <StatusDot :tone="snapshot.runtime?.mqttEnabled ? 'success' : 'neutral'" :label="snapshot.runtime?.mqttEnabled ? 'MQTT 通道已启用' : 'MQTT 通道未启用'" />
      <StatusDot :tone="snapshot.runtime?.kafkaEnabled ? 'success' : 'neutral'" :label="snapshot.runtime?.kafkaEnabled ? 'Kafka 通道已启用' : 'Kafka 通道未启用'" />
      <StatusDot :tone="snapshot.runtime?.kafkaParsedEnabled ? 'success' : 'neutral'" :label="snapshot.runtime?.kafkaParsedEnabled ? 'Kafka 解析结果发布已启用' : 'Kafka 解析结果发布未启用'" />
      <span>通道开关来自部署配置，不代表 Broker 连接健康。</span>
    </div>

    <DataTableCard :title="`消息主题${snapshot ? ` · ${filteredItems.length} 项` : ''}`" :error="error" @retry="load">
      <ui-table v-if="!error" :data="filteredItems" :loading="loading" :empty-text="emptyText" row-key="id">
        <ui-table-column label="消息用途" width="190"><template #default="{ row }"><strong>{{ row.name }}</strong><small class="subline">{{ row.protocol === 'kafka' ? 'Kafka' : 'MQTT' }} · {{ topicDirectionLabel(row.direction) }}</small></template></ui-table-column>
        <ui-table-column label="主题模板" min-width="310"><template #default="{ row }"><code class="topic-text">{{ row.topic }}</code><small class="subline">{{ row.overridden ? '使用自定义配置' : '使用默认配置' }}</small></template></ui-table-column>
        <ui-table-column label="状态" width="128"><template #default="{ row }"><StatusDot v-bind="topicStatus(row)" /></template></ui-table-column>
        <ui-table-column label="说明" min-width="280"><template #default="{ row }"><div v-if="row.description" class="topic-description">{{ row.description }}</div><span v-if="row.reason || !row.editable" :class="{ subline:row.description }">{{ row.editable ? '' : '只读：' }}{{ row.reason || '由部署配置或设备协议管理' }}</span><span v-if="!row.description && !row.reason && row.editable">—</span></template></ui-table-column>
        <ui-table-column label="操作" width="188" fixed="right" align="right"><template #default="{ row }"><RowActions :actions="rowActions(row)" /></template></ui-table-column>
      </ui-table>
    </DataTableCard>

    <p class="topic-footnote">管理平台已有消息来源的发布主题。保存后当前实例立即应用，其他进程按 2 秒缓存刷新，已在途发布可能使用旧配置；停用不会删除 Broker 中已有的消息。订阅方账号和 ACL 需在 Broker 另行配置。</p>

    <ui-dialog v-model="editorOpen" :title="editing ? `编辑主题 · ${editing.name}` : '编辑主题'" width="min(720px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving" destroy-on-close>
      <div v-if="editing" class="topic-editor">
        <ui-alert v-if="formError" :title="formError" type="error" :closable="false" />
        <ui-form :model="form" label-position="top" :disabled="saving || conflicted">
          <ui-form-item label="发布状态"><div class="topic-enabled"><ui-switch v-model="form.enabled" aria-label="启用主题发布" /><span>{{ form.enabled ? '启用发布' : '停止发布' }}</span></div></ui-form-item>
          <ui-form-item label="主题模板" required><ui-input v-model="form.topic" type="textarea" :autosize="{ minRows:2, maxRows:5 }" aria-label="主题模板" /></ui-form-item>
          <div class="topic-hint">
            <p v-if="editing.reason">{{ editing.reason }}</p>
            <p>自定义主题前缀：<code class="topic-text">{{ snapshot.prefixes?.[editing.protocol] || '未取得，请刷新' }}</code></p>
            <p v-if="editing.variables?.length">可用变量：{{ editing.variables.map(topicVariableLabel).join('；') }}。</p>
            <p v-else>此主题不支持变量。</p>
            <p>默认主题：<code class="topic-text">{{ editing.defaultTopic }}</code></p>
            <ui-button size="small" text type="primary" :disabled="saving || conflicted" @click="form.topic = editing.defaultTopic">填入默认主题</ui-button>
          </div>
          <ui-form-item label="说明"><ui-input v-model="form.description" type="textarea" :rows="3" :maxlength="500" placeholder="记录订阅用途或使用说明" aria-label="主题说明" /></ui-form-item>
        </ui-form>
        <p class="topic-hint">保存会改变后续消息的发布目标或开关；更改主题后，请同步调整订阅端配置。</p>
      </div>
      <template #footer>
        <div class="topic-editor-actions">
          <ui-button v-if="editing?.overridden && canReset" :disabled="busy || conflicted" @click="reset(editing)">恢复默认配置</ui-button>
          <span class="topic-action-spacer" />
          <ui-button :disabled="saving" @click="editorOpen = false">取消</ui-button>
          <ui-button v-if="conflicted" type="primary" @click="load">刷新配置</ui-button>
          <ui-button v-else-if="canSave" type="primary" :loading="saving" :disabled="loading" @click="save">保存配置</ui-button>
        </div>
      </template>
    </ui-dialog>
  </div>
</template>

<style scoped>
.message-topics-page { min-width: 0; }
.topic-runtime { display:flex; align-items:center; flex-wrap:wrap; gap:var(--space-3); margin-bottom:var(--space-4); }
.topic-runtime > span, .topic-footnote, .topic-hint { color:var(--text-muted); font-size:var(--font-size-sm); line-height:1.65; }
.topic-text { white-space:pre-wrap; overflow-wrap:anywhere; word-break:break-word; font-size:var(--font-size-sm); }
.topic-description { white-space:pre-wrap; overflow-wrap:anywhere; }
.topic-footnote { margin:var(--space-3) 0 0; }
.topic-editor { display:grid; gap:var(--space-3); max-height:65vh; overflow-y:auto; padding-right:var(--space-2); }
.topic-editor :deep(.ui-form) { display:grid; gap:var(--space-4); }
.topic-hint p { margin:0 0 var(--space-2); }
.topic-hint .topic-text { display:block; }
.topic-hint { margin:0; }
.topic-enabled { display:flex; align-items:center; gap:var(--space-2); }
.topic-editor-actions { display:flex; align-items:center; flex-wrap:wrap; gap:var(--space-2); }
.topic-action-spacer { flex:1; }
@media (max-width: 767px) { .topic-runtime { align-items:flex-start; } .topic-runtime > span { flex-basis:100%; } .topic-editor-actions > .ui-button { flex:1 1 auto; } .topic-action-spacer { display:none; } }
</style>
