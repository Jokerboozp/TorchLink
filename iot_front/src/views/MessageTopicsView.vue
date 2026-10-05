<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { Plus, RefreshCw } from '@lucide/vue'
import { api, session } from '../api'
import { can, permissionState } from '../permissions'
import { UiMessage, UiMessageBox } from '../ui/feedback.js'
import {
  filterMessageTopics,
  topicDirectionLabel,
  topicStatus,
  validateSharedTopic,
  queryFormFrom,
  topicQueryRequest,
  validateTopicQuery,
  queryFormToSql,
  queryFilterIsFlat,
  queryHasUnsafeNumbers
} from '../messageTopics.js'
import MessageTopicQueryEditor from '../components/MessageTopicQueryEditor.vue'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'

// 一个主题对应一份业务数据查询；外部系统用开放接口密钥换取只读订阅凭据。
defineEmits(['navigate'])
const snapshot = ref(null),
  loading = ref(false),
  saving = ref(false),
  confirming = ref(false)
const error = ref(''),
  notice = ref(''),
  formError = ref(''),
  conflicted = ref(false)
const editing = ref(null),
  creating = ref(false)
const filters = reactive({ protocol: '', keyword: '' })
const form = reactive({ name: '', protocol: 'mqtt', enabled: true, topic: '', description: '' })
const queryForm = ref(queryFormFrom()),
  queryPreview = ref(null),
  queryPreviewError = ref(''),
  queryPreviewing = ref(false)
const keyIds = ref([])
const datasets = computed(() => snapshot.value?.datasets || [])
const items = computed(() => snapshot.value?.items || [])
const keys = computed(() => snapshot.value?.keys || [])
const builtinItems = computed(() => filterMessageTopics(snapshot.value?.builtin || [], filters))
const keysEditable = computed(() => can('PUT /api/v1/access/api-keys/:id'))
const filteredItems = computed(() => filterMessageTopics(items.value, filters))
const emptyText = computed(() =>
  loading.value ? '正在读取消息主题…' : items.value.length ? '没有符合筛选条件的主题' : '暂无消息主题，可新建 MQTT 或 Kafka 主题'
)
const busy = computed(() => loading.value || saving.value || confirming.value)
const editorOpen = computed({
  get: () => creating.value || Boolean(editing.value),
  set: value => {
    if (!value && !saving.value) {
      creating.value = false
      editing.value = null
    }
  }
})
const canSave = computed(() => can(creating.value ? 'POST /api/v1/message-topics' : 'PUT /api/v1/message-topics/:id'))
const exchangeEndpoint = computed(() => `${window.location.origin}/api/open/v1/message-topics/credentials`)
let queryPreviewVersion = 0,
  queryPreviewController = null
let requestVersion = 0,
  controller = null,
  disposed = false,
  loadedIdentity = '',
  editorIdentity = '',
  editorRevision = 0

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
  return { version: requestVersion, identity: identityKey(), signal: controller.signal }
}
function current(request) {
  return !disposed && request.version === requestVersion && request.identity === identityKey()
}
function ready(permission) {
  return !disposed && !busy.value && !conflicted.value && Boolean(snapshot.value) && loadedIdentity === identityKey() && can(permission)
}
function clearDialogs() {
  clearQueryPreview()
  keyIds.value = []
  editing.value = null
  creating.value = false
  formError.value = ''
}
function clearIdentity() {
  invalidate()
  clearDialogs()
  snapshot.value = null
  loading.value = false
  saving.value = false
  confirming.value = false
  conflicted.value = false
  error.value = ''
  notice.value = ''
  loadedIdentity = ''
}
function applySnapshot(result, identity) {
  snapshot.value = result
  loadedIdentity = identity
  notice.value = typeof result.warning === 'string' ? result.warning : ''
}
async function load() {
  if (disposed || saving.value || confirming.value) return
  if (!can('menu:messageTopics')) {
    clearIdentity()
    return
  }
  const request = beginRequest()
  clearDialogs()
  snapshot.value = null
  error.value = ''
  notice.value = ''
  conflicted.value = false
  loading.value = true
  try {
    const result = await api('/api/v1/message-topics', { signal: request.signal })
    if (current(request)) applySnapshot(result, request.identity)
  } catch (cause) {
    if (current(request) && cause.name !== 'AbortError') error.value = cause.message || '消息主题读取失败'
  } finally {
    if (current(request)) loading.value = false
  }
}
function openEditor(row = null) {
  if (!ready(row ? 'PUT /api/v1/message-topics/:id' : 'POST /api/v1/message-topics')) return
  clearDialogs()
  editing.value = row
  creating.value = !row
  editorIdentity = loadedIdentity
  editorRevision = snapshot.value.revision
  queryForm.value = queryFormFrom(row?.query, row?.querySql || '')
  keyIds.value = [...(row?.keyIds || [])]
  Object.assign(form, {
    name: row?.name || '',
    protocol: row?.protocol || 'mqtt',
    enabled: row?.enabled ?? true,
    topic: row?.topic || '',
    description: row?.description || ''
  })
}
async function perform({ path, method, body, success }) {
  const request = beginRequest()
  saving.value = true
  formError.value = ''
  error.value = ''
  notice.value = ''
  try {
    const result = await api(path, { method, signal: request.signal, ...(body === undefined ? {} : { body: JSON.stringify(body) }) })
    if (!current(request)) return
    clearDialogs()
    applySnapshot(result, request.identity)
    UiMessage.success(success)
  } catch (cause) {
    if (!current(request) || cause.name === 'AbortError') return
    conflicted.value = cause.status === 409
    const message = conflicted.value ? '配置已被其他操作更新，请刷新后重新编辑。' : cause.message || '操作失败，请重试'
    if (editorOpen.value) formError.value = message
    else error.value = message
  } finally {
    if (current(request)) saving.value = false
  }
}
async function save() {
  const permission = creating.value ? 'POST /api/v1/message-topics' : 'PUT /api/v1/message-topics/:id'
  if (!ready(permission) || editorIdentity !== identityKey()) return
  formError.value =
    validateSharedTopic(form, snapshot.value.prefixes, !creating.value) || validateTopicQuery(queryForm.value, datasets.value)
  if (formError.value) return
  const body = {
    revision: editorRevision,
    name: form.name.trim(),
    enabled: form.enabled,
    description: form.description.trim(),
    protocol: creating.value ? form.protocol : editing.value.protocol,
    topic: creating.value ? form.topic.trim() : editing.value.topic,
    ...topicQueryRequest(queryForm.value)
  }
  if (keysEditable.value) body.keyIds = [...keyIds.value]
  return perform({
    path: creating.value ? '/api/v1/message-topics' : `/api/v1/message-topics/${encodeURIComponent(editing.value.id)}`,
    method: creating.value ? 'POST' : 'PUT',
    body,
    success: creating.value ? '主题已创建，符合查询条件的数据将自动发送' : '主题配置已保存'
  })
}
async function removeTopic(row) {
  const permission = 'DELETE /api/v1/message-topics/:id'
  if (!ready(permission)) return
  const identity = identityKey(),
    revision = snapshot.value.revision
  confirming.value = true
  try {
    await UiMessageBox.confirm(
      `删除“${row.name}”后将停止发布并撤销订阅授权。该主题地址不能再次使用，Broker 中已有消息不会删除。`,
      '删除消息主题',
      { type: 'warning', confirmButtonText: '确认删除', cancelButtonText: '取消' }
    )
    confirming.value = false
    if (!ready(permission) || identity !== identityKey() || revision !== snapshot.value.revision) return
    return perform({
      path: `/api/v1/message-topics/${encodeURIComponent(row.id)}?revision=${revision}`,
      method: 'DELETE',
      success: '主题已删除'
    })
  } catch (cause) {
    if (!disposed && identity === identityKey() && cause !== 'cancel' && cause !== 'close') error.value = cause.message || '操作未完成'
  } finally {
    if (!disposed && identity === identityKey()) confirming.value = false
  }
}
function keyName(id) {
  const key = keys.value.find(item => item.id === id)
  return key ? `${key.name}${key.enabled ? '' : '（已停用）'}` : id
}
async function copyTopic(row) {
  const identity = identityKey()
  try {
    await navigator.clipboard.writeText(row.topic)
    if (!disposed && identity === identityKey()) UiMessage.success('主题地址已复制')
  } catch {
    if (!disposed && identity === identityKey()) UiMessage.warning('复制失败，请选中文本复制')
  }
}
function rowActions(row) {
  return [
    { key: 'copy', label: '复制地址', disabled: busy.value, onClick: () => copyTopic(row) },
    {
      key: 'edit',
      label: '编辑',
      permission: 'PUT /api/v1/message-topics/:id',
      disabled: busy.value || conflicted.value,
      onClick: () => openEditor(row)
    },
    {
      key: 'delete',
      label: '删除',
      type: 'danger',
      permission: 'DELETE /api/v1/message-topics/:id',
      disabled: busy.value || conflicted.value,
      onClick: () => removeTopic(row)
    }
  ]
}
function clearQueryPreview() {
  queryPreviewVersion++
  queryPreviewController?.abort()
  queryPreviewController = null
  queryPreview.value = null
  queryPreviewError.value = ''
  queryPreviewing.value = false
}
async function previewQuery(asForm = false) {
  if (!editorOpen.value || queryPreviewing.value || !ready('POST /api/v1/message-topics/query/preview') || editorIdentity !== identityKey())
    return
  queryPreviewError.value = validateTopicQuery(queryForm.value, datasets.value)
  if (queryPreviewError.value) return
  clearQueryPreview()
  queryPreviewing.value = true
  queryPreviewController = new AbortController()
  const version = queryPreviewVersion,
    identity = identityKey()
  try {
    const body = {
      protocol: form.protocol,
      ...topicQueryRequest(queryForm.value),
      ...(queryForm.value.sample.trim() ? { payload: queryForm.value.sample } : {})
    }
    const result = await api('/api/v1/message-topics/query/preview', {
      method: 'POST',
      signal: queryPreviewController.signal,
      body: JSON.stringify(body)
    })
    if (disposed || version !== queryPreviewVersion || identity !== identityKey() || !editorOpen.value) return
    if (asForm) {
      if (!queryFilterIsFlat(result.query?.filter) || queryHasUnsafeNumbers(result.query?.filter)) {
        queryPreviewError.value = '这份 SQL 包含嵌套条件或高精度整数，请继续使用 SQL 编辑，避免改变查询含义。'
        return
      }
      const sample = queryForm.value.sample
      queryForm.value = { ...queryFormFrom(result.query, result.querySql), sample }
    }
    queryPreview.value = { ...result, sampled: result.sampled ?? Boolean(queryForm.value.sample.trim()) }
  } catch (cause) {
    if (!disposed && version === queryPreviewVersion && identity === identityKey() && cause.name !== 'AbortError')
      queryPreviewError.value = cause.message || '查询预览失败'
  } finally {
    if (!disposed && version === queryPreviewVersion && identity === identityKey()) queryPreviewing.value = false
  }
}
function useQuerySql() {
  formError.value = validateTopicQuery(queryForm.value, datasets.value)
  if (formError.value && !formError.value.includes('精确表示')) return
  formError.value = ''
  queryForm.value.sql = queryFormToSql(queryForm.value)
  queryForm.value.editor = 'sql'
}
function querySummary(row) {
  return `${datasets.value.find(item => item.id === row.query?.dataset)?.name || row.query?.dataset} · ${row.query?.mode === 'interval' ? `每 ${row.query.intervalSeconds} 秒查询` : '实时发送'}`
}
watch(() => JSON.stringify([queryForm.value, form.protocol]), clearQueryPreview, { flush: 'sync' })
function identityChanged() {
  clearIdentity()
  if (!disposed && can('menu:messageTopics')) load()
}
function storageChanged(event) {
  if (!event.key || ['iot_token', 'iot_tenant', 'iot_user', 'iot_permissions', 'iot_access_version'].includes(event.key)) identityChanged()
}
watch(() => [permissionState.accessVersion, ...permissionState.items].join('\n'), identityChanged, { flush: 'sync' })
onMounted(() => {
  window.addEventListener('storage', storageChanged)
  load()
})
onBeforeUnmount(() => {
  disposed = true
  invalidate()
  clearDialogs()
  window.removeEventListener('storage', storageChanged)
})
</script>

<template>
  <div class="message-topics-page">
    <FilterBar>
      <ui-input v-model="filters.keyword" clearable placeholder="搜索名称、主题或说明" aria-label="搜索消息主题" />
      <ui-select v-model="filters.protocol" clearable placeholder="全部协议" aria-label="消息协议"
        ><ui-option label="MQTT" value="mqtt" /><ui-option label="Kafka" value="kafka"
      /></ui-select>
      <template #actions
        ><ui-button :loading="loading" :disabled="saving || confirming" @click="load"><RefreshCw />刷新</ui-button
        ><ui-button
          v-if="can('POST /api/v1/message-topics')"
          type="primary"
          :disabled="busy || !snapshot || conflicted"
          @click="openEditor()"
          ><Plus />新建主题</ui-button
        ></template
      >
    </FilterBar>
    <details v-if="snapshot" class="topic-connection-status">
      <summary>连接状态</summary>
      <div class="topic-authorization" aria-label="Broker 授权状态">
        <div v-for="protocol in ['mqtt', 'kafka']" :key="protocol">
          <StatusDot
            :tone="snapshot.authorization?.[protocol]?.ready ? 'success' : 'warning'"
            :label="`${protocol === 'kafka' ? 'Kafka' : 'MQTT'} 授权${snapshot.authorization?.[protocol]?.ready ? '已就绪' : '未就绪'}`"
          /><small>{{
            snapshot.authorization?.[protocol]?.reason ||
            (snapshot.authorization?.[protocol]?.ready ? '可签发受授权范围限制的临时订阅凭据' : '尚未确认 Broker 授权配置')
          }}</small>
        </div>
      </div>
    </details>
    <ui-alert v-if="notice" :title="notice" type="warning" :closable="false" class="topic-notice" />
    <DataTableCard :title="`消息主题${snapshot ? ` · ${filteredItems.length} 项` : ''}`" :error="error" @retry="load"
      ><ui-table v-if="!error" :data="filteredItems" :loading="loading" :empty-text="emptyText" row-key="id">
        <ui-table-column label="主题" width="200"
          ><template #default="{ row }"
            ><strong>{{ row.name }}</strong
            ><small class="subline">{{ row.protocol === 'kafka' ? 'Kafka' : 'MQTT' }}</small></template
          ></ui-table-column
        >
        <ui-table-column label="主题地址 / 数据" min-width="300"
          ><template #default="{ row }"
            ><code class="topic-text">{{ row.topic }}</code
            ><small class="subline">{{ querySummary(row) }}</small></template
          ></ui-table-column
        >
        <ui-table-column label="订阅密钥" min-width="200"
          ><template #default="{ row }"
            ><span class="topic-description">{{ row.keyIds?.length ? row.keyIds.map(keyName).join('、') : '尚未授权' }}</span></template
          ></ui-table-column
        >
        <ui-table-column label="主题状态" width="128"
          ><template #default="{ row }"><StatusDot v-bind="topicStatus(row)" /></template
        ></ui-table-column>
        <ui-table-column label="说明" min-width="220"
          ><template #default="{ row }"
            ><span class="topic-description">{{ row.description || '—' }}</span></template
          ></ui-table-column
        >
        <ui-table-column label="操作" width="180" fixed="right" align="right"
          ><template #default="{ row }"><RowActions :actions="rowActions(row)" /></template
        ></ui-table-column> </ui-table
    ></DataTableCard>
    <details v-if="snapshot" class="topic-legacy">
      <summary>平台内置主题（只读） · {{ builtinItems.length }} 项</summary>
      <div class="topic-runtime" aria-label="消息通道配置">
        <span>MQTT 通道{{ snapshot.runtime?.mqttEnabled ? '已启用' : '未启用' }}</span
        ><span>Kafka 通道{{ snapshot.runtime?.kafkaEnabled ? '已启用' : '未启用' }}</span
        ><span>Kafka 解析结果发布{{ snapshot.runtime?.kafkaParsedEnabled ? '已启用' : '未启用' }}</span>
      </div>
      <p class="topic-hint">平台内部及设备接入使用的固定主题。外部订阅请新建上方的数据主题。</p>
      <ui-table :data="builtinItems" empty-text="没有符合筛选条件的内置主题" row-key="id"
        ><ui-table-column label="主题" min-width="190"
          ><template #default="{ row }"
            ><strong>{{ row.name }}</strong
            ><small class="subline"
              >{{ row.protocol === 'kafka' ? 'Kafka' : 'MQTT' }} · {{ topicDirectionLabel(row.direction) }}</small
            ></template
          ></ui-table-column
        ><ui-table-column label="地址" min-width="280"
          ><template #default="{ row }"
            ><code class="topic-text">{{ row.topic }}</code
            ><small class="subline">{{ row.reason }}</small></template
          ></ui-table-column
        ></ui-table
      >
    </details>
    <p class="topic-footnote">
      外部系统使用开通“订阅消息主题”能力的开放接口密钥（用户与权限 → 开放接口），在服务端调用 <code>POST</code>
      <code class="topic-text">{{ exchangeEndpoint }}</code> 并提交 <code>{"protocol":"mqtt"}</code> 或
      <code>kafka</code>，获取临时连接凭据与可订阅主题。修改主题或授权后需重新获取；Broker 中的历史消息保留。
    </p>

    <ui-dialog
      v-model="editorOpen"
      :title="creating ? '新建消息主题' : `编辑主题 · ${editing?.name || ''}`"
      width="min(860px, 94vw)"
      :close-on-click-modal="false"
      :close-on-press-escape="!saving"
      :show-close="!saving"
      destroy-on-close
    >
      <div v-if="editorOpen" class="topic-editor">
        <ui-alert v-if="formError" :title="formError" type="error" :closable="false" /><ui-form
          :model="form"
          label-position="top"
          :disabled="saving || conflicted"
        >
          <ui-form-item label="主题名称" required
            ><ui-input v-model="form.name" :maxlength="100" placeholder="例如 园区消防告警" aria-label="主题名称"
          /></ui-form-item>
          <ui-form-item label="协议" required
            ><ui-select v-model="form.protocol" :disabled="!creating" aria-label="主题协议"
              ><ui-option label="MQTT" value="mqtt" /><ui-option label="Kafka" value="kafka" /></ui-select
          ></ui-form-item>
          <ui-form-item label="主题地址或后缀" required
            ><ui-input
              v-model="form.topic"
              :disabled="!creating"
              :placeholder="form.protocol === 'mqtt' ? '例如 /device' : '例如 device'"
              aria-label="主题地址"
          /></ui-form-item>
          <p class="topic-hint">
            {{ creating ? '填写主题名称或本租户完整地址，平台自动补齐租户前缀。' : '协议和地址创建后固定；修改数据查询不会清除已有消息。'
            }}<br />主题前缀：<code class="topic-text">{{ snapshot.prefixes?.[form.protocol] || '未取得，请刷新' }}</code>
          </p>
          <MessageTopicQueryEditor
            :model-value="queryForm"
            :datasets="datasets"
            :disabled="saving || conflicted"
            :previewing="queryPreviewing"
            :preview="queryPreview"
            :preview-error="queryPreviewError"
            :can-preview="can('POST /api/v1/message-topics/query/preview')"
            @preview="previewQuery()"
            @sql="useQuerySql"
            @form="previewQuery(true)"
          />
          <ui-form-item label="授权订阅密钥"
            ><ui-select
              v-model="keyIds"
              multiple
              filterable
              clearable
              placeholder="选择开放接口密钥，也可稍后授权"
              aria-label="授权订阅密钥"
              :disabled="saving || conflicted || !keysEditable"
              ><ui-option
                v-for="key in keys"
                :key="key.id"
                :label="`${key.name} · ${key.username}${key.enabled ? '' : '（已停用）'}`"
                :value="key.id" /></ui-select
          ></ui-form-item>
          <p class="topic-hint">
            {{
              keysEditable
                ? keys.length
                  ? '密钥的绑定用户须有权访问此主题曾发送的全部数据和设备。'
                  : '暂无开通“订阅消息主题”能力的密钥，请先在“用户与权限 → 开放接口”创建。'
                : '修改订阅授权需要开放接口密钥的编辑权限。'
            }}
          </p>
          <ui-form-item label="主题状态"
            ><div class="topic-enabled">
              <ui-switch v-model="form.enabled" /><span>{{ form.enabled ? '启用主题' : '停用主题' }}</span>
            </div></ui-form-item
          >
          <ui-form-item label="说明"
            ><ui-input
              v-model="form.description"
              type="textarea"
              :rows="3"
              :maxlength="500"
              placeholder="记录订阅用途或使用说明"
              aria-label="主题说明"
          /></ui-form-item>
        </ui-form>
      </div>
      <template #footer
        ><div class="topic-editor-actions">
          <span class="topic-action-spacer" /><ui-button :disabled="saving" @click="editorOpen = false">取消</ui-button
          ><ui-button v-if="conflicted" type="primary" @click="load">刷新配置</ui-button
          ><ui-button v-else-if="canSave" type="primary" :loading="saving" :disabled="loading" @click="save">{{
            creating ? '创建主题' : '保存配置'
          }}</ui-button>
        </div></template
      >
    </ui-dialog>
  </div>
</template>

<style scoped>
.message-topics-page {
  min-width: 0;
}
.topic-connection-status,
.topic-legacy {
  margin: var(--space-3) 0;
}
.topic-connection-status > summary,
.topic-legacy > summary {
  cursor: pointer;
  color: var(--text-muted);
  font-size: var(--font-size-sm);
  margin-bottom: var(--space-3);
}
.topic-legacy > * + * {
  margin-top: var(--space-3);
}
.topic-authorization {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: var(--space-3);
  margin-bottom: var(--space-4);
}
.topic-authorization > div {
  display: grid;
  gap: var(--space-1);
  border: 1px solid var(--border);
  background: var(--surface);
  border-radius: var(--radius-md);
  padding: var(--space-3);
  min-width: 0;
}
.topic-authorization small {
  color: var(--text-muted);
  line-height: 1.6;
  overflow-wrap: anywhere;
}
.topic-runtime {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-3);
  margin-bottom: var(--space-3);
}
.topic-runtime > span,
.topic-footnote,
.topic-hint {
  color: var(--text-muted);
  font-size: var(--font-size-sm);
  line-height: 1.65;
}
.topic-text {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  word-break: break-word;
  font-size: var(--font-size-sm);
}
.topic-description {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
.topic-footnote {
  margin: var(--space-3) 0 0;
}
.topic-editor {
  display: grid;
  gap: var(--space-3);
  max-height: 65vh;
  overflow-y: auto;
  padding-right: var(--space-2);
  min-width: 0;
}
.topic-editor :deep(.ui-form) {
  display: grid;
  gap: var(--space-4);
}
.topic-hint p {
  margin: 0 0 var(--space-2);
}
.topic-hint .topic-text {
  display: block;
}
.topic-hint {
  margin: 0;
}
.topic-enabled {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}
.topic-editor-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: var(--space-2);
}
.topic-action-spacer {
  flex: 1;
}
.topic-notice {
  margin-bottom: var(--space-3);
}
@media (max-width: 767px) {
  .topic-authorization {
    grid-template-columns: minmax(0, 1fr);
  }
  .topic-editor-actions > .ui-button {
    flex: 1 1 auto;
  }
  .topic-action-spacer {
    display: none;
  }
  .topic-editor {
    max-height: 62vh;
  }
}
</style>
