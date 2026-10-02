<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { Plus, RefreshCw } from '@lucide/vue'
import { api, session } from '../api'
import { can, permissionState } from '../permissions'
import { UiMessage, UiMessageBox } from '../ui/feedback.js'
import { credentialStatus, filterMessageTopics, formatTopicTime, topicAccountPayload, topicAccountStatus, topicDirectionLabel, topicDirections, topicStatus, topicVariableLabel, validateManagedTopic, validateTopicAccount, validateTopicTarget } from '../messageTopics.js'
import MessageTopicDevicePicker from '../components/MessageTopicDevicePicker.vue'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'

defineEmits(['navigate'])
const snapshot = ref(null), loading = ref(false), saving = ref(false), confirming = ref(false)
const error = ref(''), notice = ref(''), formError = ref(''), conflicted = ref(false), tab = ref('topics')
const editing = ref(null), creating = ref(false), accountEditing = ref(null), accountOpen = ref(false)
const credentialAccount = ref(null), brokerCredential = ref(null), accountSecret = ref(null)
const filters = reactive({ protocol:'', direction:'', keyword:'' })
const form = reactive({ name:'', sourceId:'', enabled:true, topic:'', description:'' })
const accountForm = reactive({ name:'', username:'', enabled:true, topicIds:[], deviceScope:'all', deviceIds:[], expiresAt:null })
const items = computed(() => snapshot.value?.items || [])
const sources = computed(() => snapshot.value?.sources || [])
const accounts = computed(() => snapshot.value?.accounts || [])
const users = computed(() => snapshot.value?.users || [])
const availableTopics = computed(() => items.value.filter(item => item.editable))
const filteredItems = computed(() => filterMessageTopics(items.value, filters))
const emptyText = computed(() => loading.value ? '正在读取消息主题…' : items.value.length ? '没有符合筛选条件的主题' : '暂无消息主题，可从已有数据源新建')
const busy = computed(() => loading.value || saving.value || confirming.value)
const editorOpen = computed({ get:() => creating.value || Boolean(editing.value), set:value => { if (!value && !saving.value) { creating.value = false; editing.value = null } } })
const accountEditorOpen = computed({ get:() => accountOpen.value, set:value => { if (!value && !saving.value) accountOpen.value = false } })
const credentialOpen = computed({ get:() => Boolean(credentialAccount.value), set:value => { if (!value && !saving.value) closeCredentials() } })
const secretOpen = computed({ get:() => Boolean(accountSecret.value), set:value => { if (!value) accountSecret.value = null } })
const customEditor = computed(() => creating.value || editing.value?.custom)
const canSave = computed(() => can(creating.value ? 'POST /api/v1/message-topics' : 'PUT /api/v1/message-topics/:id'))
const canSaveAccount = computed(() => can(accountEditing.value ? 'PUT /api/v1/message-topic-accounts/:id' : 'POST /api/v1/message-topic-accounts'))
const exchangeEndpoint = computed(() => `${window.location.origin}/api/open/v1/message-topics/credentials`)
const credentialText = computed(() => brokerCredential.value ? JSON.stringify(brokerCredential.value, null, 2) : '')
let requestVersion = 0, controller = null, disposed = false, loadedIdentity = '', editorIdentity = '', editorRevision = 0

function identityKey() { return [session.token, session.tenant, session.user, permissionState.accessVersion, ...permissionState.items].join('\n') }
function invalidate() { requestVersion++; controller?.abort(); controller = null }
function beginRequest() { invalidate(); controller = new AbortController(); return { version:requestVersion, identity:identityKey(), signal:controller.signal } }
function current(request) { return !disposed && request.version === requestVersion && request.identity === identityKey() }
function ready(permission) { return !disposed && !busy.value && !conflicted.value && Boolean(snapshot.value) && loadedIdentity === identityKey() && can(permission) }
function closeCredentials() { credentialAccount.value = null; brokerCredential.value = null }
function clearDialogs() { editing.value = null; creating.value = false; accountOpen.value = false; accountEditing.value = null; accountSecret.value = null; closeCredentials(); formError.value = '' }
function clearIdentity() {
  invalidate(); clearDialogs(); snapshot.value = null; loading.value = false; saving.value = false; confirming.value = false
  conflicted.value = false; error.value = ''; notice.value = ''; loadedIdentity = ''
}
function applySnapshot(result, identity) {
  // One-time secrets stay in their own transient dialog, never the catalog.
  const { accountSecret:secret, ...catalog } = result
  snapshot.value = catalog; loadedIdentity = identity
  notice.value = typeof result.warning === 'string' ? result.warning : ''
  if (secret?.secret) accountSecret.value = { id:secret.id, secret:secret.secret, tenantId:session.tenant }
}
async function load() {
  if (disposed || saving.value || confirming.value) return
  if (!can('menu:messageTopics')) { clearIdentity(); return }
  const request = beginRequest()
  clearDialogs(); snapshot.value = null; error.value = ''; notice.value = ''; conflicted.value = false; loading.value = true
  try {
    const result = await api('/api/v1/message-topics', { signal:request.signal })
    if (current(request)) applySnapshot(result, request.identity)
  } catch (cause) {
    if (current(request) && cause.name !== 'AbortError') error.value = cause.message || '消息主题读取失败'
  } finally { if (current(request)) loading.value = false }
}
function captureEditor() { editorIdentity = loadedIdentity; editorRevision = snapshot.value.revision; formError.value = '' }
function openEditor(row = null) {
  if (!ready(row ? 'PUT /api/v1/message-topics/:id' : 'POST /api/v1/message-topics') || (row && !row.editable)) return
  clearDialogs(); editing.value = row; creating.value = !row; captureEditor()
  Object.assign(form, { name:row?.name || '', sourceId:row?.sourceId || '', enabled:row?.enabled ?? true, topic:row?.topic || '', description:row?.description || '' })
}
async function perform({ path, method, body, success, credentials = false, account = null }) {
  const request = beginRequest()
  saving.value = true; formError.value = ''; error.value = ''; notice.value = ''; accountSecret.value = null; brokerCredential.value = null
  try {
    const result = await api(path, { method, signal:request.signal, ...(body === undefined ? {} : { body:JSON.stringify(body) }) })
    if (!current(request)) return
    if (credentials) {
      brokerCredential.value = result
      snapshot.value = { ...snapshot.value, revision:result.revision }
      try {
        const catalog = await api('/api/v1/message-topics', { signal:request.signal })
        if (!current(request)) return
        applySnapshot(catalog, request.identity)
        credentialAccount.value = accounts.value.find(item => item.id === account.id) || account
        if (catalog.revision !== result.revision) {
          brokerCredential.value = null
          formError.value = '授权配置已变化，刚生成的凭据可能已撤销，请重新获取。'
          return
        }
      } catch (cause) {
        if (current(request) && cause.name !== 'AbortError') notice.value = '凭据已生成，账号状态刷新失败；关闭后请刷新列表核对。'
      }
    } else {
      clearDialogs(); applySnapshot(result, request.identity)
    }
    if (current(request)) UiMessage.success(success)
  } catch (cause) {
    if (!current(request) || cause.name === 'AbortError') return
    conflicted.value = cause.status === 409
    const message = conflicted.value ? '配置已被其他操作更新，请刷新后重新编辑。' : cause.message || '操作失败，请重试'
    if (editorOpen.value || accountOpen.value || credentialAccount.value) formError.value = message
    else error.value = message
  } finally { if (current(request)) saving.value = false }
}
async function save() {
  const permission = creating.value ? 'POST /api/v1/message-topics' : 'PUT /api/v1/message-topics/:id'
  if (!ready(permission) || editorIdentity !== identityKey() || (!creating.value && !editing.value?.editable)) return
  formError.value = customEditor.value ? validateManagedTopic(form, sources.value) : validateTopicTarget(editing.value, form.topic, snapshot.value.prefixes)
  if (formError.value) return
  const body = { revision:editorRevision, enabled:form.enabled, topic:form.topic.trim(), description:form.description.trim() }
  if (customEditor.value) Object.assign(body, { name:form.name.trim(), sourceId:form.sourceId })
  return perform({ path:creating.value ? '/api/v1/message-topics' : `/api/v1/message-topics/${encodeURIComponent(editing.value.id)}`, method:creating.value ? 'POST' : 'PUT', body, success:creating.value ? '主题已创建，请在对接账号中授权并获取凭据' : '主题配置已保存' })
}
async function confirmAction(permission, message, title, action) {
  if (!ready(permission)) return
  const identity = identityKey(), revision = snapshot.value.revision
  confirming.value = true
  try {
    await UiMessageBox.confirm(message, title, { type:'warning', confirmButtonText:'确认操作', cancelButtonText:'取消' })
    confirming.value = false
    if (!ready(permission) || identity !== identityKey() || revision !== snapshot.value.revision) return
    return action(revision)
  } catch (cause) {
    if (!disposed && identity === identityKey() && cause !== 'cancel' && cause !== 'close') error.value = cause.message || '操作未完成'
  } finally { if (!disposed && identity === identityKey()) confirming.value = false }
}
function removeTopic(row) {
  if (!row.editable) return
  return confirmAction('DELETE /api/v1/message-topics/:id', `删除“${row.name}”后将停止此主题发布，并移除对接账号中的关联授权。主题不会自动恢复，Broker 中已有消息不会删除。`, '删除消息主题', revision => perform({ path:`/api/v1/message-topics/${encodeURIComponent(row.id)}?revision=${revision}`, method:'DELETE', success:'主题已删除' }))
}
function reset(row) {
  if (!row?.editable || row.custom || !row.overridden || !ready('POST /api/v1/message-topics/:id/reset')) return
  return perform({ path:`/api/v1/message-topics/${encodeURIComponent(row.id)}/reset?revision=${snapshot.value.revision}`, method:'POST', success:'已恢复默认配置' })
}
function userLabel(username) { const user = users.value.find(item => item.username === username); return user ? `${user.displayName || user.username}（${user.username}）${user.enabled ? '' : ' · 已停用'}` : username }
function sourceLabel(id) { const source = sources.value.find(item => item.id === id); return source ? `${source.protocol === 'kafka' ? 'Kafka' : 'MQTT'} · ${source.name}` : id }
function topicName(id) { return items.value.find(item => item.id === id)?.name || id }
function openAccount(row = null) {
  if (!ready(row ? 'PUT /api/v1/message-topic-accounts/:id' : 'POST /api/v1/message-topic-accounts')) return
  clearDialogs(); accountEditing.value = row; accountOpen.value = true; captureEditor()
  Object.assign(accountForm, { name:row?.name || '', username:row?.username || '', enabled:row?.enabled ?? true, topicIds:[...(row?.topicIds || [])], deviceScope:row?.deviceScope || 'all', deviceIds:[...(row?.deviceIds || [])], expiresAt:row?.expiresAt ? Number(row.expiresAt) * 1000 : null })
}
function toggleTopic(id, checked) { accountForm.topicIds = checked ? [...new Set([...accountForm.topicIds, id])] : accountForm.topicIds.filter(value => value !== id) }
async function saveAccount() {
  const permission = accountEditing.value ? 'PUT /api/v1/message-topic-accounts/:id' : 'POST /api/v1/message-topic-accounts'
  if (!ready(permission) || !accountOpen.value || editorIdentity !== identityKey()) return
  formError.value = validateTopicAccount(accountForm, items.value, users.value)
  if (formError.value) return
  return perform({ path:accountEditing.value ? `/api/v1/message-topic-accounts/${encodeURIComponent(accountEditing.value.id)}` : '/api/v1/message-topic-accounts', method:accountEditing.value ? 'PUT' : 'POST', body:topicAccountPayload(accountForm, editorRevision), success:accountEditing.value ? '对接账号已保存' : '对接账号已创建，请保存一次性密钥' })
}
function setAccountEnabled(row) {
  if (!ready('PUT /api/v1/message-topic-accounts/:id')) return
  return perform({ path:`/api/v1/message-topic-accounts/${encodeURIComponent(row.id)}`, method:'PUT', body:topicAccountPayload({ ...row, enabled:!row.enabled, expiresAt:row.expiresAt ? Number(row.expiresAt) * 1000 : null }, snapshot.value.revision), success:row.enabled ? '对接账号已停用' : '对接账号已启用，请重新获取凭据' })
}
function removeAccount(row) {
  return confirmAction('DELETE /api/v1/message-topic-accounts/:id', `删除“${row.name}”后，此账号的密钥和临时凭据将失效，外部系统需要重新对接。`, '删除对接账号', revision => perform({ path:`/api/v1/message-topic-accounts/${encodeURIComponent(row.id)}?revision=${revision}`, method:'DELETE', success:'对接账号已删除' }))
}
function rotateAccount(row) {
  return confirmAction('POST /api/v1/message-topic-accounts/:id/rotate', `为“${row.name}”生成新密钥，旧密钥和已签发的临时凭据将失效。新密钥只显示一次。`, '轮换对接密钥', revision => perform({ path:`/api/v1/message-topic-accounts/${encodeURIComponent(row.id)}/rotate?revision=${revision}`, method:'POST', success:'密钥已轮换，请保存新密钥' }))
}
function openCredentials(row) {
  if (!ready('POST /api/v1/message-topic-accounts/:id/credentials')) return
  clearDialogs(); credentialAccount.value = row
}
function credentialReason(account, protocol) {
  const authorization = snapshot.value?.authorization?.[protocol]
  if (!authorization?.ready) return authorization?.reason || 'Broker 授权尚未就绪'
  if (!account?.enabled) return '请先启用对接账号'
  if (Number(account.expiresAt) > 0 && Number(account.expiresAt) * 1000 <= Date.now()) return '对接账号已过期'
  if (!(account.topicIds || []).some(id => items.value.some(topic => topic.id === id && topic.protocol === protocol && topic.enabled))) return `没有已启用的 ${protocol === 'kafka' ? 'Kafka' : 'MQTT'} 授权主题`
  return ''
}
function issueCredentials(protocol) {
  const account = credentialAccount.value
  if (!account || !ready('POST /api/v1/message-topic-accounts/:id/credentials') || credentialReason(account, protocol)) return
  return perform({ path:`/api/v1/message-topic-accounts/${encodeURIComponent(account.id)}/credentials`, method:'POST', body:{ revision:snapshot.value.revision, protocol }, credentials:true, account, success:'临时凭据已生成，请及时保存' })
}
async function copyText(value, message = '已复制') {
  if (loadedIdentity !== identityKey() || !value) return
  const identity = identityKey()
  try { await navigator.clipboard.writeText(value); if (!disposed && identity === identityKey()) UiMessage.success(message) }
  catch { if (!disposed && identity === identityKey()) UiMessage.warning('复制失败，请选中文本复制') }
}
function copyTopic(row) { return copyText(row.topic, row.custom ? '主题标识已复制；实际订阅地址请从对接账号获取' : '主题模板已复制；请替换其中的变量') }
function copyAccountSecret() { return copyText(JSON.stringify({ tenantId:accountSecret.value?.tenantId, accountId:accountSecret.value?.id, secret:accountSecret.value?.secret }, null, 2), '对接账号与密钥已复制') }
function rowActions(row) {
  return [
    { key:'copy', label:row.custom ? '复制标识' : '复制', disabled:busy.value, onClick:() => copyTopic(row) },
    { key:'edit', label:'编辑', hidden:!row.editable, permission:'PUT /api/v1/message-topics/:id', disabled:busy.value || conflicted.value, onClick:() => openEditor(row) },
    { key:'reset', label:'恢复默认', hidden:!row.editable || !row.overridden || row.custom, permission:'POST /api/v1/message-topics/:id/reset', disabled:busy.value || conflicted.value, onClick:() => reset(row) },
    { key:'delete', label:'删除', type:'danger', hidden:!row.editable, permission:'DELETE /api/v1/message-topics/:id', disabled:busy.value || conflicted.value, onClick:() => removeTopic(row) }
  ]
}
function accountActions(row) {
  const disabled = busy.value || conflicted.value
  return [
    { key:'credentials', label:'连接与凭据', permission:'POST /api/v1/message-topic-accounts/:id/credentials', disabled, onClick:() => openCredentials(row) },
    { key:'edit', label:'编辑', permission:'PUT /api/v1/message-topic-accounts/:id', disabled, onClick:() => openAccount(row) },
    { key:'toggle', label:row.enabled ? '停用' : '启用', permission:'PUT /api/v1/message-topic-accounts/:id', disabled, onClick:() => setAccountEnabled(row) },
    { key:'rotate', label:'轮换密钥', permission:'POST /api/v1/message-topic-accounts/:id/rotate', disabled, onClick:() => rotateAccount(row) },
    { key:'delete', label:'删除', type:'danger', permission:'DELETE /api/v1/message-topic-accounts/:id', disabled, onClick:() => removeAccount(row) }
  ]
}
function identityChanged() { clearIdentity(); if (!disposed && can('menu:messageTopics')) load() }
function storageChanged(event) { if (!event.key || ['iot_token', 'iot_tenant', 'iot_user', 'iot_permissions', 'iot_access_version'].includes(event.key)) identityChanged() }
watch(() => [permissionState.accessVersion, ...permissionState.items].join('\n'), identityChanged, { flush:'sync' })
onMounted(() => { window.addEventListener('storage', storageChanged); load() })
onBeforeUnmount(() => { disposed = true; invalidate(); clearDialogs(); window.removeEventListener('storage', storageChanged) })
</script>

<template>
  <div class="message-topics-page">
    <ui-tabs v-model="tab"><ui-tab-pane label="消息主题" name="topics" /><ui-tab-pane label="外部对接账号" name="accounts" /></ui-tabs>
    <FilterBar>
      <template v-if="tab === 'topics'">
        <ui-input v-model="filters.keyword" clearable placeholder="搜索名称、主题或说明" aria-label="搜索消息主题" />
        <ui-select v-model="filters.protocol" clearable placeholder="全部协议" aria-label="消息协议"><ui-option label="MQTT" value="mqtt" /><ui-option label="Kafka" value="kafka" /></ui-select>
        <ui-select v-model="filters.direction" clearable placeholder="全部用途" aria-label="消息用途"><ui-option v-for="option in topicDirections" :key="option.value" :label="option.label" :value="option.value" /></ui-select>
      </template>
      <p v-else class="topic-hint">为外部系统绑定平台用户、授权主题和设备范围，再获取连接凭据。</p>
      <template #actions><ui-button :loading="loading" :disabled="saving || confirming" @click="load"><RefreshCw />刷新</ui-button><ui-button v-if="tab === 'topics' && can('POST /api/v1/message-topics')" type="primary" :disabled="busy || !snapshot || conflicted" @click="openEditor()"><Plus />新建主题</ui-button><ui-button v-if="tab === 'accounts' && can('POST /api/v1/message-topic-accounts')" type="primary" :disabled="busy || !snapshot || conflicted" @click="openAccount()"><Plus />新建对接账号</ui-button></template>
    </FilterBar>
    <div v-if="snapshot" class="topic-authorization" aria-label="Broker 授权状态"><div v-for="protocol in ['mqtt', 'kafka']" :key="protocol"><StatusDot :tone="snapshot.authorization?.[protocol]?.ready ? 'success' : 'warning'" :label="`${protocol === 'kafka' ? 'Kafka' : 'MQTT'} 授权${snapshot.authorization?.[protocol]?.ready ? '已就绪' : '未就绪'}`" /><small>{{ snapshot.authorization?.[protocol]?.reason || (snapshot.authorization?.[protocol]?.ready ? '可生成受授权范围限制的临时连接凭据' : '尚未确认 Broker 授权配置') }}</small></div></div>
    <ui-alert v-if="notice" :title="notice" type="warning" :closable="false" class="topic-notice" />
    <template v-if="tab === 'topics'">
      <div v-if="snapshot" class="topic-runtime" aria-label="消息通道配置"><span>MQTT 通道{{ snapshot.runtime?.mqttEnabled ? '已启用' : '未启用' }}</span><span>Kafka 通道{{ snapshot.runtime?.kafkaEnabled ? '已启用' : '未启用' }}</span><span>Kafka 解析结果发布{{ snapshot.runtime?.kafkaParsedEnabled ? '已启用' : '未启用' }}</span></div>
      <DataTableCard :title="`消息主题${snapshot ? ` · ${filteredItems.length} 项` : ''}`" :error="error" @retry="load"><ui-table v-if="!error" :data="filteredItems" :loading="loading" :empty-text="emptyText" row-key="id">
        <ui-table-column label="消息用途" width="200"><template #default="{ row }"><strong>{{ row.name }}</strong><small class="subline">{{ row.protocol === 'kafka' ? 'Kafka' : 'MQTT' }} · {{ topicDirectionLabel(row.direction) }}</small></template></ui-table-column>
        <ui-table-column label="主题 / 标识" min-width="300"><template #default="{ row }"><code class="topic-text">{{ row.topic }}</code><small class="subline">{{ row.custom ? `新增主题 · ${sourceLabel(row.sourceId)}` : row.overridden ? '使用自定义配置' : '使用默认配置' }}</small></template></ui-table-column>
        <ui-table-column label="发布状态" width="128"><template #default="{ row }"><StatusDot v-bind="topicStatus(row)" /></template></ui-table-column>
        <ui-table-column label="说明" min-width="280"><template #default="{ row }"><div v-if="row.description" class="topic-description">{{ row.description }}</div><span v-if="row.reason || !row.editable" :class="{ subline:row.description }">{{ row.editable ? '' : '只读：' }}{{ row.reason || '由部署配置或设备协议管理' }}</span><span v-if="!row.description && !row.reason && row.editable">—</span></template></ui-table-column>
        <ui-table-column label="操作" width="204" fixed="right" align="right"><template #default="{ row }"><RowActions :actions="rowActions(row)" /></template></ui-table-column>
      </ui-table></DataTableCard>
    </template>
    <DataTableCard v-else :title="`外部对接账号 · ${accounts.length} 个`" :error="error" @retry="load"><ui-table v-if="!error" :data="accounts" :loading="loading" :empty-text="loading ? '正在读取对接账号…' : '暂无对接账号，点击“新建对接账号”开始授权'" row-key="id">
      <ui-table-column label="对接账号" min-width="170"><template #default="{ row }"><strong>{{ row.name }}</strong><small class="subline">{{ row.id }}</small></template></ui-table-column>
      <ui-table-column label="绑定用户" min-width="170"><template #default="{ row }">{{ userLabel(row.username) }}</template></ui-table-column>
      <ui-table-column label="主题与设备范围" min-width="240"><template #default="{ row }"><div class="topic-description">{{ row.topicIds?.map(topicName).join('、') || '未授权主题' }}</div><small class="subline">{{ row.deviceScope === 'all' ? '绑定用户范围内全部设备' : `指定 ${row.deviceIds?.length || 0} 台设备` }}</small></template></ui-table-column>
      <ui-table-column label="状态 / 有效期" min-width="180"><template #default="{ row }"><StatusDot v-bind="topicAccountStatus(row)" /><small class="subline">{{ formatTopicTime(row.expiresAt) }}</small></template></ui-table-column>
      <ui-table-column label="临时凭据" min-width="180"><template #default="{ row }"><div v-for="(credential, index) in row.credentials" :key="index" class="topic-credential-status">{{ credential.protocol === 'kafka' ? 'Kafka' : 'MQTT' }} · {{ credentialStatus(credential) }}<small class="subline">{{ formatTopicTime(credential.expiresAt, '—') }}</small></div><span v-if="!row.credentials?.length" class="topic-hint">尚未生成</span></template></ui-table-column>
      <ui-table-column label="操作" width="224" fixed="right" align="right"><template #default="{ row }"><RowActions :actions="accountActions(row)" /></template></ui-table-column>
    </ui-table></DataTableCard>
    <p class="topic-footnote">新增主题需授权给对接账号，并生成连接凭据后才分发；新数据按绑定用户的实时功能权限和设备范围过滤。保存配置会撤销之前的临时凭据，请重新获取。Broker 中已有消息不会被删除。</p>
    <p v-if="tab === 'topics'" class="topic-footnote">保存后当前实例立即应用，其他进程按 2 秒缓存刷新，已在途发布可能使用旧配置。通道开关不代表连接健康，外部账号只有在 Broker 授权就绪后才能获取有效凭据。</p>

    <ui-dialog v-model="editorOpen" :title="creating ? '新建消息主题' : `编辑主题 · ${editing?.name || ''}`" width="min(720px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving" destroy-on-close>
      <div v-if="editorOpen" class="topic-editor"><ui-alert v-if="formError" :title="formError" type="error" :closable="false" /><ui-form :model="form" label-position="top" :disabled="saving || conflicted">
        <template v-if="customEditor"><ui-form-item label="主题名称" required><ui-input v-model="form.name" :maxlength="100" placeholder="例如 园区消防告警" aria-label="主题名称" /></ui-form-item><ui-form-item label="消息数据源" required><ui-select v-model="form.sourceId" filterable placeholder="选择要分发的数据" aria-label="消息数据源"><ui-option v-for="source in sources" :key="source.id" :value="source.id" :label="sourceLabel(source.id)" /></ui-select></ui-form-item><ui-form-item label="主题标识" required><ui-input v-model="form.topic" :maxlength="48" placeholder="1–48 个字母、数字、下划线或连字符" aria-label="主题标识" /></ui-form-item><p class="topic-hint">此标识用于管理分发用途。实际订阅地址由账号授权和临时凭据生成，请从“外部对接账号 → 连接与凭据”获取。</p></template>
        <template v-else><ui-form-item label="主题模板" required><ui-input v-model="form.topic" type="textarea" :autosize="{ minRows:2, maxRows:5 }" aria-label="主题模板" /></ui-form-item><div class="topic-hint"><p v-if="editing.reason">{{ editing.reason }}</p><p>自定义主题前缀：<code class="topic-text">{{ snapshot.prefixes?.[editing.protocol] || '未取得，请刷新' }}</code></p><p v-if="editing.variables?.length">可用变量：{{ editing.variables.map(topicVariableLabel).join('；') }}。</p><p v-else>此主题不支持变量。</p><p>默认主题：<code class="topic-text">{{ editing.defaultTopic }}</code></p><ui-button size="small" text type="primary" :disabled="saving || conflicted" @click="form.topic = editing.defaultTopic">填入默认主题</ui-button></div></template>
        <ui-form-item label="发布状态"><div class="topic-enabled"><ui-switch v-model="form.enabled" /><span>{{ form.enabled ? '启用发布' : '停止发布' }}</span></div></ui-form-item><ui-form-item label="说明"><ui-input v-model="form.description" type="textarea" :rows="3" :maxlength="500" placeholder="记录订阅用途或使用说明" aria-label="主题说明" /></ui-form-item>
      </ui-form></div>
      <template #footer><div class="topic-editor-actions"><ui-button v-if="editing?.overridden && !editing?.custom && can('POST /api/v1/message-topics/:id/reset')" :disabled="busy || conflicted" @click="reset(editing)">恢复默认配置</ui-button><span class="topic-action-spacer" /><ui-button :disabled="saving" @click="editorOpen = false">取消</ui-button><ui-button v-if="conflicted" type="primary" @click="load">刷新配置</ui-button><ui-button v-else-if="canSave" type="primary" :loading="saving" :disabled="loading" @click="save">{{ creating ? '创建主题' : '保存配置' }}</ui-button></div></template>
    </ui-dialog>

    <ui-dialog v-model="accountEditorOpen" :title="accountEditing ? `编辑对接账号 · ${accountEditing.name}` : '新建对接账号'" width="min(760px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving" destroy-on-close>
      <div v-if="accountOpen" class="topic-editor topic-account-editor"><ui-alert v-if="formError" :title="formError" type="error" :closable="false" /><ui-form :model="accountForm" label-position="top" :disabled="saving || conflicted">
        <ui-form-item label="对接账号名称" required><ui-input v-model="accountForm.name" :maxlength="100" placeholder="例如 园区管理平台" aria-label="对接账号名称" /></ui-form-item>
        <ui-form-item label="绑定平台用户" required><ui-select v-model="accountForm.username" filterable placeholder="选择平台用户" aria-label="绑定平台用户" @change="accountForm.deviceIds = []"><ui-option v-for="user in users" :key="user.username" :value="user.username" :label="userLabel(user.username)" /></ui-select></ui-form-item><p v-if="!users.length" class="topic-hint">暂无可绑定的平台用户，请先在“用户与权限”登记用户。</p>
        <ui-form-item label="授权主题" required><div class="topic-grants"><ui-checkbox v-for="topic in availableTopics" :key="topic.id" :model-value="accountForm.topicIds.includes(topic.id)" @update:model-value="checked => toggleTopic(topic.id, checked)">{{ topic.protocol === 'kafka' ? 'Kafka' : 'MQTT' }} · {{ topic.name }}<small v-if="!topic.enabled">（已停用）</small></ui-checkbox><p v-if="!availableTopics.length" class="topic-hint">暂无可授权主题，请先新建消息主题。</p></div></ui-form-item>
        <ui-form-item label="设备范围"><ui-radio-group v-model="accountForm.deviceScope" class="topic-scope-options"><ui-radio value="all">绑定用户范围内全部设备</ui-radio><ui-radio value="selected">指定设备</ui-radio></ui-radio-group></ui-form-item>
        <MessageTopicDevicePicker v-if="accountForm.deviceScope === 'selected'" :key="accountForm.username" v-model="accountForm.deviceIds" :disabled="saving || conflicted" /><p v-else class="topic-hint">随绑定用户当前设备范围变化，不能超出该用户的设备权限。</p>
        <ui-form-item label="账号有效期至"><ui-date-time v-model="accountForm.expiresAt" clearable disable-past placeholder="不填写则长期有效" /></ui-form-item><ui-form-item label="账号状态"><div class="topic-enabled"><ui-switch v-model="accountForm.enabled" /><span>{{ accountForm.enabled ? '启用对接' : '停用对接' }}</span></div></ui-form-item><p class="topic-hint">新数据同时检查绑定用户的当前功能权限、账号授权主题和设备范围。账号密钥用于自动换取临时连接凭据，创建或轮换时仅显示一次。</p>
      </ui-form></div>
      <template #footer><div class="topic-editor-actions"><span class="topic-action-spacer" /><ui-button :disabled="saving" @click="accountEditorOpen = false">取消</ui-button><ui-button v-if="conflicted" type="primary" @click="load">刷新配置</ui-button><ui-button v-else-if="canSaveAccount" type="primary" :loading="saving" :disabled="loading" @click="saveAccount">{{ accountEditing ? '保存账号' : '创建账号' }}</ui-button></div></template>
    </ui-dialog>

    <ui-dialog v-model="secretOpen" title="保存对接账号密钥" width="min(680px, 94vw)" :close-on-click-modal="false" destroy-on-close><div v-if="accountSecret" class="topic-editor">
      <ui-alert type="warning" :closable="false" title="密钥只显示这一次，关闭后无法再次查看。请交给外部系统的服务端安全保存。" />
      <ui-descriptions :column="1"><ui-descriptions-item label="租户">{{ accountSecret.tenantId }}</ui-descriptions-item><ui-descriptions-item label="对接账号"><code class="topic-text">{{ accountSecret.id }}</code></ui-descriptions-item><ui-descriptions-item label="账号密钥"><code class="topic-text topic-secret">{{ accountSecret.secret }}</code></ui-descriptions-item></ui-descriptions>
      <ui-button @click="copyAccountSecret">复制账号与密钥</ui-button><p class="topic-hint">外部服务调用 <code>POST</code> <code class="topic-text">{{ exchangeEndpoint }}</code>，JSON 请求包含 <code>tenantId</code>、<code>accountId</code>、<code>secret</code> 和 <code>protocol</code>（mqtt 或 kafka）。临时凭据有效期为 1 小时，以接口返回的到期时间为准。</p>
    </div><template #footer><ui-button type="primary" @click="accountSecret = null">我已保存</ui-button></template></ui-dialog>

    <ui-dialog v-model="credentialOpen" :title="`连接与凭据 · ${credentialAccount?.name || ''}`" width="min(780px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving" destroy-on-close><div v-if="credentialAccount" class="topic-editor">
      <ui-alert v-if="formError" :title="formError" type="error" :closable="false" /><p class="topic-hint">绑定用户：{{ userLabel(credentialAccount.username) }}。每次获取临时凭据后，使用返回的实际主题地址订阅；不要直接使用管理列表中的主题标识。</p>
      <div class="topic-protocol-credentials"><div v-for="protocol in ['mqtt', 'kafka']" :key="protocol"><ui-button type="primary" :loading="saving" :disabled="busy || conflicted || !!credentialReason(credentialAccount, protocol)" @click="issueCredentials(protocol)">生成 {{ protocol === 'kafka' ? 'Kafka' : 'MQTT' }} 凭据</ui-button><p>{{ credentialReason(credentialAccount, protocol) || '可生成有效期 1 小时的临时凭据' }}</p></div></div>
      <template v-if="brokerCredential"><ui-alert type="warning" :closable="false" title="连接密码只在本次展示，关闭后清除。修改授权或配置后，需重新获取临时凭据。" /><ui-descriptions :column="1">
        <ui-descriptions-item label="协议">{{ brokerCredential.protocol === 'kafka' ? 'Kafka' : 'MQTT' }}</ui-descriptions-item><ui-descriptions-item v-if="brokerCredential.broker" label="Broker"><code class="topic-text">{{ brokerCredential.broker }}</code></ui-descriptions-item><ui-descriptions-item label="用户名"><code class="topic-text">{{ brokerCredential.username }}</code></ui-descriptions-item><ui-descriptions-item label="连接密码"><code class="topic-text topic-secret">{{ brokerCredential.password }}</code></ui-descriptions-item><ui-descriptions-item v-if="brokerCredential.groupId" label="消费者组"><code class="topic-text">{{ brokerCredential.groupId }}</code></ui-descriptions-item><ui-descriptions-item v-if="brokerCredential.mechanism" label="认证机制">{{ brokerCredential.mechanism }}</ui-descriptions-item><ui-descriptions-item label="有效期至">{{ formatTopicTime(brokerCredential.expiresAt, '—') }}</ui-descriptions-item><ui-descriptions-item label="订阅主题"><code v-for="topic in brokerCredential.topics" :key="topic" class="topic-text topic-subscription">{{ topic }}</code></ui-descriptions-item>
        <ui-descriptions-item v-if="brokerCredential.protocol === 'kafka' && brokerCredential.securityProtocol" label="安全协议">{{ brokerCredential.securityProtocol }}</ui-descriptions-item><ui-descriptions-item v-if="brokerCredential.protocol === 'kafka' && typeof brokerCredential.tls === 'boolean'" label="TLS">{{ brokerCredential.tls ? '启用' : '未启用' }}</ui-descriptions-item>
      </ui-descriptions><ui-button @click="copyText(credentialText, '连接凭据与订阅主题已复制')">复制完整连接凭据</ui-button></template>
      <p class="topic-hint">服务端自动获取凭据：<code>POST</code> <code class="topic-text">{{ exchangeEndpoint }}</code>。提交账号的 tenantId、accountId、secret、protocol；密钥遗失时，请关闭此窗口并在账号操作中轮换。</p>
    </div><template #footer><div class="topic-editor-actions"><span class="topic-action-spacer" /><ui-button v-if="conflicted" type="primary" @click="load">刷新配置</ui-button><ui-button :disabled="saving" @click="closeCredentials">关闭</ui-button></div></template></ui-dialog>
  </div>
</template>

<style scoped>
.message-topics-page { min-width:0; }
.topic-authorization { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:var(--space-3); margin-bottom:var(--space-4); }
.topic-authorization > div { display:grid; gap:var(--space-1); border:1px solid var(--border); background:var(--surface); border-radius:var(--radius-md); padding:var(--space-3); min-width:0; }
.topic-authorization small { color:var(--text-muted); line-height:1.6; overflow-wrap:anywhere; }
.topic-runtime { display:flex; align-items:center; flex-wrap:wrap; gap:var(--space-3); margin-bottom:var(--space-3); }
.topic-runtime > span, .topic-footnote, .topic-hint { color:var(--text-muted); font-size:var(--font-size-sm); line-height:1.65; }
.topic-text { white-space:pre-wrap; overflow-wrap:anywhere; word-break:break-word; font-size:var(--font-size-sm); }
.topic-description { white-space:pre-wrap; overflow-wrap:anywhere; }
.topic-footnote { margin:var(--space-3) 0 0; }
.topic-editor { display:grid; gap:var(--space-3); max-height:65vh; overflow-y:auto; padding-right:var(--space-2); min-width:0; }
.topic-editor :deep(.ui-form) { display:grid; gap:var(--space-4); }
.topic-hint p { margin:0 0 var(--space-2); }
.topic-hint .topic-text { display:block; }
.topic-hint { margin:0; }
.topic-enabled { display:flex; align-items:center; gap:var(--space-2); }
.topic-editor-actions { display:flex; align-items:center; flex-wrap:wrap; gap:var(--space-2); }
.topic-action-spacer { flex:1; }
.topic-notice { margin-bottom:var(--space-3); }
.topic-grants { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:var(--space-2); max-height:230px; overflow:auto; width:100%; }
.topic-scope-options { display:flex; flex-wrap:wrap; gap:var(--space-3); }
.topic-protocol-credentials { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:var(--space-3); }
.topic-protocol-credentials p { margin:var(--space-2) 0 0; color:var(--text-muted); font-size:var(--font-size-sm); overflow-wrap:anywhere; }
.topic-secret { user-select:all; }
.topic-subscription { display:block; margin:var(--space-1) 0; }
.topic-credential-status + .topic-credential-status { margin-top:var(--space-2); }
@media (max-width:767px) { .topic-authorization, .topic-grants, .topic-protocol-credentials { grid-template-columns:minmax(0,1fr); } .topic-editor-actions > .ui-button { flex:1 1 auto; } .topic-action-spacer { display:none; } .topic-editor { max-height:62vh; } }
</style>
