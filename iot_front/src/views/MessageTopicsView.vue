<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { Plus, RefreshCw } from '@lucide/vue'
import { api, session } from '../api'
import { can, permissionState } from '../permissions'
import { UiMessage, UiMessageBox } from '../ui/feedback.js'
import { filterMessageTopics, formatTopicTime, topicAccountPayload, topicAccountStatus, topicDirectionLabel, topicStatus, topicVariableLabel, validateManagedTopic, validateSharedTopic, validateTopicAccount, validateTopicTarget, validateTopicRule, topicRulePayload, validateTopicMessage, queryFormFrom, topicQueryRequest, validateTopicQuery, queryFormToSql, queryFilterIsFlat, queryHasUnsafeNumbers } from '../messageTopics.js'
import MessageTopicQueryEditor from '../components/MessageTopicQueryEditor.vue'
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
const form = reactive({ name:'', protocol:'mqtt', sourceId:'', enabled:true, topic:'', description:'' })
const queryForm = ref(queryFormFrom()), queryPreview = ref(null), queryPreviewError = ref(''), queryPreviewing = ref(false)
const queryAccountIds = ref([]), convertingLegacy = ref(false)
const queryEnabled = computed(() => sharedEditor.value && (creating.value || Boolean(editing.value?.query) || convertingLegacy.value))
const datasets = computed(() => snapshot.value?.datasets || [])
const legacyItems = computed(() => filterMessageTopics(items.value.filter(item => !item.custom && !item.shared), filters))
const queryAccountsEditable = computed(() => can('PUT /api/v1/message-topic-accounts/:id'))
const accountForm = reactive({ name:'', username:'', enabled:true, topicIds:[], publishTopicIds:[], deviceScope:'all', deviceIds:[], expiresAt:null })
const rulesTopic = ref(null), ruleEditing = ref(null), ruleMode = ref('list'), ruleRows = ref([])
const ruleForm = reactive({ name:'', sourceId:'', enabled:true, deviceScope:'all', deviceIds:[], format:'original', template:'' })
const previewInput = ref(''), previewOutput = ref(null), previewError = ref(''), previewing = ref(false)
const publishTopic = ref(null), publishResult = ref(null), publishForm = reactive({ format:'json', payload:'', key:'', qos:0 })
const items = computed(() => snapshot.value?.items || [])
const sources = computed(() => snapshot.value?.sources || [])
const rules = computed(() => snapshot.value?.rules || [])
const topicRules = computed(() => rules.value.filter(rule => rule.topicId === rulesTopic.value?.id))
const ruleSources = computed(() => sources.value.filter(source => source.protocol === rulesTopic.value?.protocol))
const accounts = computed(() => snapshot.value?.accounts || [])
const users = computed(() => snapshot.value?.users || [])
const availableTopics = computed(() => items.value.filter(item => item.editable))
const subscriptionTopics = computed(() => availableTopics.value.filter(item => item.custom || item.shared))
const legacySubscriptionTopics = computed(() => availableTopics.value.filter(item => !item.custom && !item.shared))
const filteredItems = computed(() => filterMessageTopics(items.value.filter(item => item.custom || item.shared), filters))
const emptyText = computed(() => loading.value ? '正在读取消息主题…' : items.value.some(item => item.custom || item.shared) ? '没有符合筛选条件的主题' : '暂无消息主题，可新建 MQTT 或 Kafka 主题')
const busy = computed(() => loading.value || saving.value || confirming.value)
const editorOpen = computed({ get:() => creating.value || Boolean(editing.value), set:value => { if (!value && !saving.value) { creating.value = false; editing.value = null } } })
const accountEditorOpen = computed({ get:() => accountOpen.value, set:value => { if (!value && !saving.value) accountOpen.value = false } })
const credentialOpen = computed({ get:() => Boolean(credentialAccount.value), set:value => { if (!value && !saving.value) closeCredentials() } })
const secretOpen = computed({ get:() => Boolean(accountSecret.value), set:value => { if (!value) accountSecret.value = null } })
const sharedEditor = computed(() => creating.value || editing.value?.shared)
const legacyEditor = computed(() => editing.value?.custom && !editing.value?.shared)
const rulesOpen = computed({ get:() => Boolean(rulesTopic.value), set:value => { if (!value && !saving.value) { rulesTopic.value = null; clearPreview() } } })
const publishOpen = computed({ get:() => Boolean(publishTopic.value), set:value => { if (!value && !saving.value) { publishTopic.value = null; publishResult.value = null; publishForm.payload = ''; publishForm.key = '' } } })
const canSaveRule = computed(() => can(ruleEditing.value ? 'PUT /api/v1/message-topics/:id/rules/:ruleId' : 'POST /api/v1/message-topics/:id/rules'))
const canSave = computed(() => can(creating.value ? 'POST /api/v1/message-topics' : 'PUT /api/v1/message-topics/:id'))
const canSaveAccount = computed(() => can(accountEditing.value ? 'PUT /api/v1/message-topic-accounts/:id' : 'POST /api/v1/message-topic-accounts'))
const exchangeEndpoint = computed(() => `${window.location.origin}/api/open/v1/message-topics/credentials`)
const credentialText = computed(() => brokerCredential.value ? JSON.stringify(brokerCredential.value, null, 2) : '')
let queryPreviewVersion = 0, queryPreviewController = null
let previewVersion = 0, previewController = null
let requestVersion = 0, controller = null, disposed = false, loadedIdentity = '', editorIdentity = '', editorRevision = 0

function identityKey() { return [session.token, session.tenant, session.user, permissionState.accessVersion, ...permissionState.items].join('\n') }
function invalidate() { requestVersion++; controller?.abort(); controller = null }
function beginRequest() { invalidate(); controller = new AbortController(); return { version:requestVersion, identity:identityKey(), signal:controller.signal } }
function current(request) { return !disposed && request.version === requestVersion && request.identity === identityKey() }
function ready(permission) { return !disposed && !busy.value && !conflicted.value && Boolean(snapshot.value) && loadedIdentity === identityKey() && can(permission) }
function closeCredentials() { credentialAccount.value = null; brokerCredential.value = null }
function clearDialogs() { clearQueryPreview(); convertingLegacy.value = false; queryAccountIds.value = []; clearPreview(); rulesTopic.value = null; ruleEditing.value = null; ruleMode.value = 'list'; publishTopic.value = null; publishResult.value = null; publishForm.payload = ''; publishForm.key = ''; editing.value = null; creating.value = false; accountOpen.value = false; accountEditing.value = null; accountSecret.value = null; closeCredentials(); formError.value = '' }
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
  queryForm.value = queryFormFrom(row?.query, row?.querySql || ''); queryAccountIds.value = row?.accountIds ? [...row.accountIds] : accounts.value.filter(account => account.topicIds?.includes(row?.id)).map(account => account.id)
  Object.assign(form, { name:row?.name || '', protocol:row?.protocol || 'mqtt', sourceId:row?.sourceId || '', enabled:row?.enabled ?? true, topic:row?.topic || '', description:row?.description || '' })
}
async function perform({ path, method, body, success, credentials = false, account = null, manageRules = null }) {
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
      if (manageRules) rulesTopic.value = items.value.find(item => item.id === manageRules) || null
    }
    if (current(request)) UiMessage.success(success)
  } catch (cause) {
    if (!current(request) || cause.name === 'AbortError') return
    conflicted.value = cause.status === 409
    const message = conflicted.value ? '配置已被其他操作更新，请刷新后重新编辑。' : cause.message || '操作失败，请重试'
    if (editorOpen.value || accountOpen.value || credentialAccount.value || rulesTopic.value || publishTopic.value) formError.value = message
    else error.value = message
  } finally { if (current(request)) saving.value = false }
}
async function save() {
  const permission = creating.value ? 'POST /api/v1/message-topics' : 'PUT /api/v1/message-topics/:id'
  if (!ready(permission) || editorIdentity !== identityKey() || (!creating.value && !editing.value?.editable)) return
  formError.value = sharedEditor.value ? validateSharedTopic(form, snapshot.value.prefixes, !creating.value) : legacyEditor.value ? validateManagedTopic(form, sources.value) : validateTopicTarget(editing.value, form.topic, snapshot.value.prefixes)
  if (!formError.value && queryEnabled.value) formError.value = validateTopicQuery(queryForm.value, datasets.value)
  if (formError.value) return
  const body = { revision:editorRevision, enabled:form.enabled, description:form.description.trim() }
  if (sharedEditor.value) { body.name = form.name.trim(); Object.assign(body, creating.value ? { protocol:form.protocol, topic:form.topic.trim() } : { protocol:editing.value.protocol, topic:editing.value.topic }) }
  if (queryEnabled.value) { Object.assign(body, topicQueryRequest(queryForm.value)); if (queryAccountsEditable.value) body.accountIds = [...queryAccountIds.value]; if (convertingLegacy.value) body.replaceLegacyRules = true }
  else if (!sharedEditor.value) { body.topic = form.topic.trim(); if (legacyEditor.value) Object.assign(body, { name:form.name.trim(), sourceId:form.sourceId }) }
  return perform({ path:creating.value ? '/api/v1/message-topics' : `/api/v1/message-topics/${encodeURIComponent(editing.value.id)}`, method:creating.value ? 'POST' : 'PUT', body, success:creating.value ? '主题已创建，符合查询条件的数据将自动发送' : '主题配置已保存' })
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
  return confirmAction('DELETE /api/v1/message-topics/:id', `删除“${row.name}”后将停止此主题发布，并移除对接账号中的关联授权。主题不会自动恢复，${row.shared ? '该主题地址不能再次使用，' : ''}Broker 中已有消息不会删除。`, '删除消息主题', revision => perform({ path:`/api/v1/message-topics/${encodeURIComponent(row.id)}?revision=${revision}`, method:'DELETE', success:'主题已删除' }))
}
function reset(row) {
  if (!row?.editable || row.custom || row.shared || !row.overridden || !ready('POST /api/v1/message-topics/:id/reset')) return
  return perform({ path:`/api/v1/message-topics/${encodeURIComponent(row.id)}/reset?revision=${snapshot.value.revision}`, method:'POST', success:'已恢复默认配置' })
}
function userLabel(username) { const user = users.value.find(item => item.username === username); return user ? `${user.displayName || user.username}（${user.username}）${user.enabled ? '' : ' · 已停用'}` : username }
function sourceLabel(id) { const source = sources.value.find(item => item.id === id); return source ? `${source.protocol === 'kafka' ? 'Kafka' : 'MQTT'} · ${source.name}` : id }
function topicName(id) { return items.value.find(item => item.id === id)?.name || id }
function openAccount(row = null) {
  if (!ready(row ? 'PUT /api/v1/message-topic-accounts/:id' : 'POST /api/v1/message-topic-accounts')) return
  clearDialogs(); accountEditing.value = row; accountOpen.value = true; captureEditor()
  Object.assign(accountForm, { name:row?.name || '', username:row?.username || '', enabled:row?.enabled ?? true, topicIds:[...(row?.topicIds || [])], publishTopicIds:[...(row?.publishTopicIds || [])], deviceScope:row?.deviceScope || 'all', deviceIds:[...(row?.deviceIds || [])], expiresAt:row?.expiresAt ? Number(row.expiresAt) * 1000 : null })
}
function toggleTopic(id, checked, field = 'topicIds') { accountForm[field] = checked ? [...new Set([...accountForm[field], id])] : accountForm[field].filter(value => value !== id) }
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
  if (![...(account.topicIds || []), ...(account.publishTopicIds || [])].some(id => items.value.some(topic => topic.id === id && topic.protocol === protocol && topic.enabled))) return `没有已启用的 ${protocol === 'kafka' ? 'Kafka' : 'MQTT'} 授权主题`
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
function copyTopic(row) { return copyText(row.topic, row.shared ? '主题地址已复制' : row.custom ? '主题标识已复制；实际订阅地址请从对接账号获取' : '主题模板已复制；请替换其中的变量') }
function copyAccountSecret() { return copyText(JSON.stringify({ tenantId:accountSecret.value?.tenantId, accountId:accountSecret.value?.id, secret:accountSecret.value?.secret }, null, 2), '对接账号与密钥已复制') }
function rowActions(row) {
  return [
    { key:'copy', label:row.custom && !row.shared ? '复制标识' : '复制', disabled:busy.value, onClick:() => copyTopic(row) },
    { key:'edit', label:'编辑', hidden:!row.editable, permission:'PUT /api/v1/message-topics/:id', disabled:busy.value || conflicted.value, onClick:() => openEditor(row) },
    { key:'reset', label:'恢复默认', hidden:!row.editable || !row.overridden || row.custom || row.shared, permission:'POST /api/v1/message-topics/:id/reset', disabled:busy.value || conflicted.value, onClick:() => reset(row) },
    { key:'delete', label:'删除', type:'danger', hidden:!row.editable, permission:'DELETE /api/v1/message-topics/:id', disabled:busy.value || conflicted.value, onClick:() => removeTopic(row) }
  ]
}
function accountActions(row) {
  const disabled = busy.value || conflicted.value
  return [
    { key:'credentials', label:'订阅信息', permission:'POST /api/v1/message-topic-accounts/:id/credentials', disabled, onClick:() => openCredentials(row) },
    { key:'edit', label:'编辑', permission:'PUT /api/v1/message-topic-accounts/:id', disabled, onClick:() => openAccount(row) },
    { key:'toggle', label:row.enabled ? '停用' : '启用', permission:'PUT /api/v1/message-topic-accounts/:id', disabled, onClick:() => setAccountEnabled(row) },
    { key:'rotate', label:'轮换密钥', permission:'POST /api/v1/message-topic-accounts/:id/rotate', disabled, onClick:() => rotateAccount(row) },
    { key:'delete', label:'删除', type:'danger', permission:'DELETE /api/v1/message-topic-accounts/:id', disabled, onClick:() => removeAccount(row) }
  ]
}
function clearPreview() { previewVersion++; previewController?.abort(); previewController = null; previewing.value = false; previewOutput.value = null; previewError.value = '' }
function openRules(row) {
  if (!row.shared || row.query || !row.editable || !ready('menu:messageTopics')) return
  clearDialogs(); rulesTopic.value = row; captureEditor()
}
function editRule(row = null) {
  if (!rulesTopic.value || !ready(row ? 'PUT /api/v1/message-topics/:id/rules/:ruleId' : 'POST /api/v1/message-topics/:id/rules')) return
  clearPreview(); ruleEditing.value = row; ruleMode.value = 'edit'; captureEditor(); previewInput.value = ''
  Object.assign(ruleForm, { name:row?.name || '', sourceId:row?.sourceId || '', enabled:row?.enabled ?? true, deviceScope:row?.deviceScope || 'all', deviceIds:[...(row?.deviceIds || [])], format:row?.format || 'original', template:row?.template || '' })
  ruleRows.value = Object.entries(row?.fields || {}).map(([output, path]) => ({ output, path }))
  if (!ruleRows.value.length) ruleRows.value = [{ output:'', path:'' }]
}
function backToRules() { clearPreview(); ruleMode.value = 'list'; ruleEditing.value = null; formError.value = '' }
async function saveRule() {
  const permission = ruleEditing.value ? 'PUT /api/v1/message-topics/:id/rules/:ruleId' : 'POST /api/v1/message-topics/:id/rules'
  if (!rulesTopic.value || ruleMode.value !== 'edit' || !ready(permission) || editorIdentity !== identityKey()) return
  formError.value = validateTopicRule(ruleForm, ruleRows.value, ruleSources.value)
  if (formError.value) return
  const id = rulesTopic.value.id, path = `/api/v1/message-topics/${encodeURIComponent(id)}/rules`
  clearPreview()
  return perform({ path:ruleEditing.value ? `${path}/${encodeURIComponent(ruleEditing.value.id)}` : path, method:ruleEditing.value ? 'PUT' : 'POST', body:topicRulePayload(ruleForm, ruleRows.value, editorRevision), manageRules:id, success:'自动发送规则已保存' })
}
function removeRule(row) {
  const id = rulesTopic.value?.id
  if (!id) return
  return confirmAction('DELETE /api/v1/message-topics/:id/rules/:ruleId', `删除“${row.name}”后停止该规则的自动发送，主题及历史消息会保留。`, '删除自动发送规则', revision => perform({ path:`/api/v1/message-topics/${encodeURIComponent(id)}/rules/${encodeURIComponent(row.id)}?revision=${revision}`, method:'DELETE', manageRules:id, success:'自动发送规则已删除' }))
}
async function previewRule() {
  if (!rulesTopic.value || previewing.value || !ready('POST /api/v1/message-topics/:id/preview') || editorIdentity !== identityKey()) return
  previewError.value = validateTopicRule(ruleForm, ruleRows.value, ruleSources.value) || validateTopicMessage(previewInput.value, 'json')
  if (previewError.value) return
  clearPreview(); previewing.value = true; previewController = new AbortController()
  const version = previewVersion, identity = identityKey(), topicId = rulesTopic.value.id
  try {
    const { revision, ...rule } = topicRulePayload(ruleForm, ruleRows.value, editorRevision)
    const result = await api(`/api/v1/message-topics/${encodeURIComponent(topicId)}/preview`, { method:'POST', signal:previewController.signal, body:JSON.stringify({ rule, payload:previewInput.value }) })
    if (!disposed && version === previewVersion && identity === identityKey() && topicId === rulesTopic.value?.id) previewOutput.value = result.payload
  } catch (cause) {
    if (!disposed && version === previewVersion && identity === identityKey() && cause.name !== 'AbortError') previewError.value = cause.message || '预览失败'
  } finally { if (!disposed && version === previewVersion && identity === identityKey()) previewing.value = false }
}
function openPublish(row) {
  if (!row.shared || row.query || !row.editable || !row.enabled || !ready('POST /api/v1/message-topics/:id/publish')) return
  clearDialogs(); publishTopic.value = row; captureEditor(); Object.assign(publishForm, { format:'json', payload:'', key:'', qos:0 })
}
async function publish() {
  if (!publishTopic.value || !ready('POST /api/v1/message-topics/:id/publish') || editorIdentity !== identityKey()) return
  formError.value = validateTopicMessage(publishForm.payload, publishForm.format)
  if (formError.value) return
  const request = beginRequest(), row = publishTopic.value
  saving.value = true; publishResult.value = null
  try {
    const body = { revision:editorRevision, payload:publishForm.payload, format:publishForm.format, ...(row.protocol === 'kafka' ? { key:publishForm.key } : { qos:publishForm.qos }) }
    const result = await api(`/api/v1/message-topics/${encodeURIComponent(row.id)}/publish`, { method:'POST', signal:request.signal, body:JSON.stringify(body) })
    if (current(request)) publishResult.value = result
  } catch (cause) {
    if (!current(request) || cause.name === 'AbortError') return
    conflicted.value = cause.status === 409
    formError.value = conflicted.value ? '配置已被其他操作更新，请刷新后重新发送。' : cause.message || '发送未确认成功，请核对 Broker 状态后再决定是否重试。'
  } finally { if (current(request)) saving.value = false }
}
function clearQueryPreview() { queryPreviewVersion++; queryPreviewController?.abort(); queryPreviewController = null; queryPreview.value = null; queryPreviewError.value = ''; queryPreviewing.value = false }
async function previewQuery(asForm = false) {
  if (!queryEnabled.value || queryPreviewing.value || !ready('POST /api/v1/message-topics/query/preview') || editorIdentity !== identityKey()) return
  queryPreviewError.value = validateTopicQuery(queryForm.value, datasets.value)
  if (queryPreviewError.value) return
  clearQueryPreview(); queryPreviewing.value = true; queryPreviewController = new AbortController()
  const version = queryPreviewVersion, identity = identityKey()
  try {
    const body = { protocol:form.protocol, ...topicQueryRequest(queryForm.value), ...(queryForm.value.sample.trim() ? { payload:queryForm.value.sample } : {}) }
    const result = await api('/api/v1/message-topics/query/preview', { method:'POST', signal:queryPreviewController.signal, body:JSON.stringify(body) })
    if (disposed || version !== queryPreviewVersion || identity !== identityKey() || !editorOpen.value) return
    if (asForm) {
      if (!queryFilterIsFlat(result.query?.filter) || queryHasUnsafeNumbers(result.query?.filter)) { queryPreviewError.value = '这份 SQL 包含嵌套条件或高精度整数，请继续使用 SQL 编辑，避免改变查询含义。'; return }
      const sample = queryForm.value.sample
      queryForm.value = { ...queryFormFrom(result.query, result.querySql), sample }
    }
    queryPreview.value = { ...result, sampled:result.sampled ?? Boolean(queryForm.value.sample.trim()) }
  } catch (cause) { if (!disposed && version === queryPreviewVersion && identity === identityKey() && cause.name !== 'AbortError') queryPreviewError.value = cause.message || '查询预览失败' }
  finally { if (!disposed && version === queryPreviewVersion && identity === identityKey()) queryPreviewing.value = false }
}
function useQuerySql() {
  formError.value = validateTopicQuery(queryForm.value, datasets.value)
  if (formError.value && !formError.value.includes('精确表示')) return
  formError.value = ''
  queryForm.value.sql = queryFormToSql(queryForm.value); queryForm.value.editor = 'sql'
}
function convertLegacy() {
  return confirmAction('PUT /api/v1/message-topics/:id', '转换后将以当前表单中的一份数据查询替换此主题的全部旧发送规则，历史消息与授权边界继续保留。请先解除此主题的外部发布授权。', '改为数据订阅主题', () => { convertingLegacy.value = true; formError.value = '' })
}
function querySummary(row) {
  if (!row.query) return row.shared ? '旧消息通道 · 编辑中管理原有规则' : '旧账号隔离通道'
  return `${datasets.value.find(item => item.id === row.query.dataset)?.name || row.query.dataset} · ${row.query.mode === 'interval' ? `每 ${row.query.intervalSeconds} 秒查询` : '实时发送'}`
}
watch(() => JSON.stringify([queryForm.value, form.protocol]), clearQueryPreview, { flush:'sync' })
watch(() => JSON.stringify([ruleForm, ruleRows.value, previewInput.value]), clearPreview, { flush:'sync' })
watch(() => JSON.stringify(publishForm), () => { publishResult.value = null }, { flush:'sync' })
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
      </template>
      <p v-else class="topic-hint">为外部系统选择可订阅的主题，再获取连接信息。</p>
      <template #actions><ui-button :loading="loading" :disabled="saving || confirming" @click="load"><RefreshCw />刷新</ui-button><ui-button v-if="tab === 'topics' && can('POST /api/v1/message-topics')" type="primary" :disabled="busy || !snapshot || conflicted" @click="openEditor()"><Plus />新建主题</ui-button><ui-button v-if="tab === 'accounts' && can('POST /api/v1/message-topic-accounts')" type="primary" :disabled="busy || !snapshot || conflicted" @click="openAccount()"><Plus />新建对接账号</ui-button></template>
    </FilterBar>
    <details v-if="snapshot" class="topic-connection-status"><summary>连接状态</summary><div class="topic-authorization" aria-label="Broker 授权状态"><div v-for="protocol in ['mqtt', 'kafka']" :key="protocol"><StatusDot :tone="snapshot.authorization?.[protocol]?.ready ? 'success' : 'warning'" :label="`${protocol === 'kafka' ? 'Kafka' : 'MQTT'} 授权${snapshot.authorization?.[protocol]?.ready ? '已就绪' : '未就绪'}`" /><small>{{ snapshot.authorization?.[protocol]?.reason || (snapshot.authorization?.[protocol]?.ready ? '可生成受授权范围限制的临时连接凭据' : '尚未确认 Broker 授权配置') }}</small></div></div></details>
    <ui-alert v-if="notice" :title="notice" type="warning" :closable="false" class="topic-notice" />
    <template v-if="tab === 'topics'">
      <DataTableCard :title="`消息主题${snapshot ? ` · ${filteredItems.length} 项` : ''}`" :error="error" @retry="load"><ui-table v-if="!error" :data="filteredItems" :loading="loading" :empty-text="emptyText" row-key="id">
        <ui-table-column label="主题" width="200"><template #default="{ row }"><strong>{{ row.name }}</strong><small class="subline">{{ row.protocol === 'kafka' ? 'Kafka' : 'MQTT' }}</small></template></ui-table-column>
        <ui-table-column label="主题地址 / 标识" min-width="300"><template #default="{ row }"><code class="topic-text">{{ row.topic }}</code><small class="subline">{{ querySummary(row) }}</small></template></ui-table-column>
        <ui-table-column label="主题状态" width="128"><template #default="{ row }"><StatusDot v-bind="topicStatus(row)" /></template></ui-table-column>
        <ui-table-column label="说明" min-width="280"><template #default="{ row }"><div v-if="row.description" class="topic-description">{{ row.description }}</div><span v-if="row.reason || !row.editable" :class="{ subline:row.description }">{{ row.editable ? '' : '只读：' }}{{ row.reason || '由部署配置或设备协议管理' }}</span><span v-if="!row.description && !row.reason && row.editable">—</span></template></ui-table-column>
        <ui-table-column label="操作" width="180" fixed="right" align="right"><template #default="{ row }"><RowActions :actions="rowActions(row)" /></template></ui-table-column>
      </ui-table></DataTableCard>
      <details v-if="snapshot" class="topic-legacy"><summary>兼容配置与内置主题 · {{ legacyItems.length }} 项</summary>
      <div v-if="snapshot" class="topic-runtime" aria-label="消息通道配置"><span>MQTT 通道{{ snapshot.runtime?.mqttEnabled ? '已启用' : '未启用' }}</span><span>Kafka 通道{{ snapshot.runtime?.kafkaEnabled ? '已启用' : '未启用' }}</span><span>Kafka 解析结果发布{{ snapshot.runtime?.kafkaParsedEnabled ? '已启用' : '未启用' }}</span></div>
      <p class="topic-hint">保留已有平台转发和内部主题配置。新外部订阅请使用上方的“新建主题”。</p>
      <ui-table :data="legacyItems" empty-text="没有符合筛选条件的内置主题" row-key="id"><ui-table-column label="主题" min-width="190"><template #default="{ row }"><strong>{{ row.name }}</strong><small class="subline">{{ row.protocol === 'kafka' ? 'Kafka' : 'MQTT' }} · {{ topicDirectionLabel(row.direction) }}</small></template></ui-table-column><ui-table-column label="地址" min-width="280"><template #default="{ row }"><code class="topic-text">{{ row.topic }}</code><small class="subline">{{ row.reason || row.description }}</small></template></ui-table-column><ui-table-column label="状态" width="128"><template #default="{ row }"><StatusDot v-bind="topicStatus(row)" /></template></ui-table-column><ui-table-column label="操作" width="180"><template #default="{ row }"><RowActions :actions="rowActions(row)" /></template></ui-table-column></ui-table></details>
    </template>
    <DataTableCard v-else :title="`外部对接账号 · ${accounts.length} 个`" :error="error" @retry="load"><ui-table v-if="!error" :data="accounts" :loading="loading" :empty-text="loading ? '正在读取对接账号…' : '暂无对接账号，点击“新建对接账号”开始授权'" row-key="id">
      <ui-table-column label="对接账号" min-width="170"><template #default="{ row }"><strong>{{ row.name }}</strong><small class="subline">{{ row.id }}</small></template></ui-table-column>
      <ui-table-column label="绑定用户" min-width="170"><template #default="{ row }">{{ userLabel(row.username) }}</template></ui-table-column>
      <ui-table-column label="主题与设备范围" min-width="240"><template #default="{ row }"><div class="topic-description">{{ row.topicIds?.map(topicName).join('、') || '尚未授权订阅' }}<small v-if="row.publishTopicIds?.length" class="subline">旧发布授权：{{ row.publishTopicIds.map(topicName).join('、') }}</small></div><small class="subline">{{ row.deviceScope === 'all' ? '绑定用户范围内全部设备' : `指定 ${row.deviceIds?.length || 0} 台设备` }}</small></template></ui-table-column>
      <ui-table-column label="状态 / 有效期" min-width="180"><template #default="{ row }"><StatusDot v-bind="topicAccountStatus(row)" /><small class="subline">{{ formatTopicTime(row.expiresAt) }}</small></template></ui-table-column>

      <ui-table-column label="操作" width="224" fixed="right" align="right"><template #default="{ row }"><RowActions :actions="accountActions(row)" /></template></ui-table-column>
    </ui-table></DataTableCard>
    <p class="topic-footnote">一个主题对应一份数据查询，授权账号订阅实际主题地址即可接收。修改查询或授权后，请重新获取订阅凭据；历史消息仍保留在 Broker 中。</p>

    <ui-dialog v-model="editorOpen" :title="creating ? '新建消息主题' : `编辑主题 · ${editing?.name || ''}`" width="min(860px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving" destroy-on-close>
      <div v-if="editorOpen" class="topic-editor"><ui-alert v-if="formError" :title="formError" type="error" :closable="false" /><ui-form :model="form" label-position="top" :disabled="saving || conflicted">
        <template v-if="sharedEditor"><ui-form-item label="主题名称" required><ui-input v-model="form.name" :maxlength="100" placeholder="例如 园区消防告警" aria-label="主题名称" /></ui-form-item><ui-form-item label="协议" required><ui-select v-model="form.protocol" :disabled="!creating" aria-label="主题协议"><ui-option label="MQTT" value="mqtt" /><ui-option label="Kafka" value="kafka" /></ui-select></ui-form-item><ui-form-item label="主题地址或后缀" required><ui-input v-model="form.topic" :disabled="!creating" :placeholder="form.protocol === 'mqtt' ? '例如 /device' : '例如 device'" aria-label="主题地址" /></ui-form-item><p class="topic-hint">{{ creating ? '填写主题名称或本租户完整地址，平台自动补齐租户前缀。' : '协议和地址创建后固定；修改数据查询不会清除已有消息。' }}<br />主题前缀：<code class="topic-text">{{ snapshot.prefixes?.[form.protocol] || '未取得，请刷新' }}</code></p></template>
        <template v-else-if="legacyEditor"><ui-form-item label="主题名称" required><ui-input v-model="form.name" :maxlength="100" aria-label="主题名称" /></ui-form-item><ui-form-item label="消息数据源" required><ui-select v-model="form.sourceId" filterable aria-label="消息数据源"><ui-option v-for="source in sources" :key="source.id" :value="source.id" :label="sourceLabel(source.id)" /></ui-select></ui-form-item><ui-form-item label="主题标识" required><ui-input v-model="form.topic" :maxlength="48" aria-label="主题标识" /></ui-form-item><p class="topic-hint">此为已有的账号隔离转发通道，保留原数据源和订阅行为。实际订阅地址从对接账号的连接凭据获取。</p></template>
        <template v-else><ui-form-item label="主题模板" required><ui-input v-model="form.topic" type="textarea" :autosize="{ minRows:2, maxRows:5 }" aria-label="主题模板" /></ui-form-item><div class="topic-hint"><p v-if="editing.reason">{{ editing.reason }}</p><p>自定义主题前缀：<code class="topic-text">{{ snapshot.prefixes?.[editing.protocol] || '未取得，请刷新' }}</code></p><p v-if="editing.variables?.length">可用变量：{{ editing.variables.map(topicVariableLabel).join('；') }}。</p><p v-else>此主题不支持变量。</p><p>默认主题：<code class="topic-text">{{ editing.defaultTopic }}</code></p><ui-button size="small" text type="primary" :disabled="saving || conflicted" @click="form.topic = editing.defaultTopic">填入默认主题</ui-button></div></template>
        <MessageTopicQueryEditor v-if="queryEnabled" :model-value="queryForm" :datasets="datasets" :disabled="saving || conflicted" :previewing="queryPreviewing" :preview="queryPreview" :preview-error="queryPreviewError" :can-preview="can('POST /api/v1/message-topics/query/preview')" @preview="previewQuery()" @sql="useQuerySql" @form="previewQuery(true)" />
        <ui-form-item v-if="queryEnabled" label="授权订阅账号"><ui-select v-model="queryAccountIds" multiple filterable clearable placeholder="选择外部对接账号，也可稍后授权" aria-label="授权订阅账号" :disabled="saving || conflicted || !queryAccountsEditable"><ui-option v-for="account in accounts" :key="account.id" :label="`${account.name}${account.enabled ? '' : '（已停用）'}`" :value="account.id" /></ui-select></ui-form-item><p v-if="queryEnabled && !accounts.length" class="topic-hint">暂无对接账号。保存主题后，在“外部对接账号”新建账号并授权订阅。</p>
        <details v-if="editing?.shared && !editing?.query && !convertingLegacy" class="topic-legacy"><summary>旧消息通道配置</summary><p class="topic-hint">此主题沿用已有发送方式。可以管理旧规则，也可以明确替换为一份数据查询。</p><div class="topic-editor-actions"><ui-button :disabled="busy || conflicted" @click="openRules(editing)">管理旧发送规则</ui-button><ui-button v-if="can('POST /api/v1/message-topics/:id/publish')" :disabled="busy || conflicted || !editing.enabled" @click="openPublish(editing)">手动调试</ui-button><ui-button :disabled="busy || conflicted" @click="convertLegacy">改为数据订阅主题</ui-button></div></details>
        <ui-alert v-if="convertingLegacy" type="warning" :closable="false" title="保存后将用这份查询替换旧发送规则，历史数据范围继续保留。" />
        <ui-form-item label="主题状态"><div class="topic-enabled"><ui-switch v-model="form.enabled" /><span>{{ form.enabled ? '启用主题' : '停用主题' }}</span></div></ui-form-item><ui-form-item label="说明"><ui-input v-model="form.description" type="textarea" :rows="3" :maxlength="500" placeholder="记录订阅用途或使用说明" aria-label="主题说明" /></ui-form-item>
      </ui-form></div>
      <template #footer><div class="topic-editor-actions"><ui-button v-if="editing?.overridden && !editing?.custom && can('POST /api/v1/message-topics/:id/reset')" :disabled="busy || conflicted" @click="reset(editing)">恢复默认配置</ui-button><span class="topic-action-spacer" /><ui-button :disabled="saving" @click="editorOpen = false">取消</ui-button><ui-button v-if="conflicted" type="primary" @click="load">刷新配置</ui-button><ui-button v-else-if="canSave" type="primary" :loading="saving" :disabled="loading" @click="save">{{ creating ? '创建主题' : '保存配置' }}</ui-button></div></template>
    </ui-dialog>

    <ui-dialog v-model="rulesOpen" :title="`${ruleMode === 'edit' ? (ruleEditing ? '编辑自动发送规则' : '新建自动发送规则') : '自动发送规则'} · ${rulesTopic?.name || ''}`" width="min(940px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving" destroy-on-close>
      <div v-if="rulesTopic" class="topic-editor"><ui-alert v-if="formError" :title="formError" type="error" :closable="false" /><code class="topic-text">{{ rulesTopic.topic }}</code><p class="topic-hint">订阅账号须有权访问此主题所有曾自动发送的平台数据。删除或收窄规则不会缩小历史范围；需要更小订阅范围时请新建主题。</p><details v-if="rulesTopic.exposure?.length" class="topic-exposure"><summary>已包含的平台数据范围 · {{ rulesTopic.exposure.length }} 项</summary><p v-for="(scope, index) in rulesTopic.exposure" :key="index" class="topic-hint">{{ sourceLabel(scope.sourceId) }} · {{ scope.deviceScope === 'all' ? '全部设备' : `指定 ${scope.deviceIds?.length || 0} 台设备` }}<code v-if="scope.deviceScope === 'selected'" class="topic-text">{{ scope.deviceIds?.join('、') }}</code></p></details>
        <template v-if="ruleMode === 'list'"><p class="topic-hint">每条规则独立选择触发事件、设备和发送内容。没有启用的规则时，此主题不会自动发送平台数据。</p><ui-button v-if="can('POST /api/v1/message-topics/:id/rules')" type="primary" :disabled="busy || conflicted" @click="editRule()"><Plus />添加规则</ui-button><ui-table :data="topicRules" empty-text="暂无自动发送规则" row-key="id"><ui-table-column label="规则" min-width="210"><template #default="{ row }"><strong>{{ row.name }}</strong><small class="subline">{{ sourceLabel(row.sourceId) }}</small></template></ui-table-column><ui-table-column label="设备 / 格式" min-width="170"><template #default="{ row }">{{ row.deviceScope === 'all' ? '全部设备' : `指定 ${row.deviceIds?.length || 0} 台设备` }}<small class="subline">{{ ({ original:'原文 JSON', json:'字段映射', text:'文本模板' })[row.format] }}</small></template></ui-table-column><ui-table-column label="状态" width="100"><template #default="{ row }"><StatusDot :tone="row.enabled ? 'success' : 'neutral'" :label="row.enabled ? '已启用' : '已停用'" /></template></ui-table-column><ui-table-column label="操作" width="145"><template #default="{ row }"><RowActions :actions="[{ key:'edit', label:'编辑', permission:'PUT /api/v1/message-topics/:id/rules/:ruleId', disabled:busy || conflicted, onClick:() => editRule(row) }, { key:'delete', label:'删除', type:'danger', permission:'DELETE /api/v1/message-topics/:id/rules/:ruleId', disabled:busy || conflicted, onClick:() => removeRule(row) }]" /></template></ui-table-column></ui-table></template>
        <ui-form v-else :model="ruleForm" label-position="top" :disabled="saving || conflicted">
          <ui-form-item label="规则名称" required><ui-input v-model="ruleForm.name" :maxlength="100" aria-label="规则名称" /></ui-form-item>
          <ui-form-item label="触发事件" required><ui-select v-model="ruleForm.sourceId" filterable placeholder="选择触发事件" aria-label="触发事件"><ui-option v-for="source in ruleSources" :key="source.id" :value="source.id" :label="source.name" /></ui-select></ui-form-item>
          <ui-form-item label="设备范围"><ui-radio-group v-model="ruleForm.deviceScope" class="topic-scope-options"><ui-radio value="all">当前租户全部设备</ui-radio><ui-radio value="selected">指定设备</ui-radio></ui-radio-group></ui-form-item><MessageTopicDevicePicker v-if="ruleForm.deviceScope === 'selected'" v-model="ruleForm.deviceIds" :disabled="saving || conflicted" label="自动发送设备选择" hint="只在所选设备触发事件时发送。主设备与子设备分别选择。" />
          <ui-form-item label="发送内容"><ui-radio-group v-model="ruleForm.format" class="topic-scope-options"><ui-radio value="original">原文 JSON</ui-radio><ui-radio value="json">字段映射</ui-radio><ui-radio value="text">文本模板</ui-radio></ui-radio-group></ui-form-item>
          <p v-if="ruleForm.format === 'original'" class="topic-hint">发送触发事件的完整 JSON 内容。</p>
          <div v-else-if="ruleForm.format === 'json'" class="topic-mapping"><div v-for="(field, index) in ruleRows" :key="index" class="topic-mapping-row"><ui-input v-model="field.output" placeholder="输出字段名" :aria-label="`输出字段 ${index + 1}`" /><ui-input v-model="field.path" placeholder="来源路径，如 properties.temperature" :aria-label="`来源路径 ${index + 1}`" /><ui-button :disabled="saving || conflicted" @click="ruleRows.splice(index, 1)">移除</ui-button></div><ui-button :disabled="saving || conflicted" @click="ruleRows.push({ output:'', path:'' })">添加字段</ui-button><p class="topic-hint">输出为扁平 JSON 对象，来源支持点路径和数组索引；来源字段缺失时发送失败。</p></div>
          <ui-form-item v-else label="文本模板" required><ui-input v-model="ruleForm.template" type="textarea" :rows="5" placeholder="例如 temp={{properties.temperature}}" aria-label="文本模板" /></ui-form-item><p v-if="ruleForm.format === 'text'" class="topic-hint">用 <code v-pre>{{路径}}</code> 插入字段；字符串保留原值，其他类型按 JSON 编码，缺失字段报错。</p>
          <ui-form-item label="规则状态"><div class="topic-enabled"><ui-switch v-model="ruleForm.enabled" /><span>{{ ruleForm.enabled ? '启用自动发送' : '停用自动发送' }}</span></div></ui-form-item>
          <details v-if="can('POST /api/v1/message-topics/:id/preview')" class="topic-preview"><summary>预览发送内容</summary><p class="topic-hint">填写触发事件的 JSON 样例，仅计算输出，不发送消息。</p><ui-input v-model="previewInput" type="textarea" :rows="5" placeholder="粘贴事件 JSON 样例" aria-label="规则预览样例" /><ui-button :loading="previewing" :disabled="saving || conflicted" @click="previewRule">预览</ui-button><ui-alert v-if="previewError" :title="previewError" type="error" :closable="false" /><pre v-if="previewOutput !== null" class="topic-preview-result">{{ previewOutput }}</pre></details>
        </ui-form>
      </div><template #footer><div class="topic-editor-actions"><ui-button v-if="ruleMode === 'edit'" :disabled="saving" @click="backToRules">返回规则列表</ui-button><span class="topic-action-spacer" /><ui-button :disabled="saving" @click="rulesOpen = false">关闭</ui-button><ui-button v-if="conflicted" type="primary" @click="load">刷新配置</ui-button><ui-button v-else-if="ruleMode === 'edit' && canSaveRule" type="primary" :loading="saving" @click="saveRule">保存规则</ui-button></div></template>
    </ui-dialog>

    <ui-dialog v-model="publishOpen" :title="`发送消息 · ${publishTopic?.name || ''}`" width="min(760px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving" destroy-on-close><div v-if="publishTopic" class="topic-editor"><ui-alert v-if="formError" :title="formError" type="error" :closable="false" /><code class="topic-text">{{ publishTopic.topic }}</code><ui-form :model="publishForm" label-position="top" :disabled="saving || conflicted"><ui-form-item label="消息格式"><ui-radio-group v-model="publishForm.format"><ui-radio value="json">JSON</ui-radio><ui-radio value="text">文本</ui-radio></ui-radio-group></ui-form-item><ui-form-item label="消息内容" required><ui-input v-model="publishForm.payload" type="textarea" :rows="10" placeholder="填写要发送的内容，最多 256 KB" aria-label="手动发送内容" /></ui-form-item><ui-form-item v-if="publishTopic.protocol === 'kafka'" label="消息 key（选填）"><ui-input v-model="publishForm.key" aria-label="Kafka 消息 key" /></ui-form-item><ui-form-item v-else label="QoS"><ui-select v-model="publishForm.qos" aria-label="MQTT QoS"><ui-option v-for="value in [0, 1, 2]" :key="value" :label="`QoS ${value}`" :value="value" /></ui-select></ui-form-item></ui-form><ui-alert v-if="publishResult?.published" type="success" :closable="false" :title="`Broker 已确认发送 ${publishResult.bytes} 字节`" description="此结果不代表订阅方已接收或处理。" /><p class="topic-hint">点击发送会立即向 Broker 发布消息；失败不会自动重试。</p></div><template #footer><div class="topic-editor-actions"><span class="topic-action-spacer" /><ui-button :disabled="saving" @click="publishOpen = false">关闭</ui-button><ui-button v-if="conflicted" type="primary" @click="load">刷新配置</ui-button><ui-button v-else type="primary" :loading="saving" :disabled="!can('POST /api/v1/message-topics/:id/publish')" @click="publish">发送消息</ui-button></div></template></ui-dialog>

    <ui-dialog v-model="accountEditorOpen" :title="accountEditing ? `编辑对接账号 · ${accountEditing.name}` : '新建对接账号'" width="min(760px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving" destroy-on-close>
      <div v-if="accountOpen" class="topic-editor topic-account-editor"><ui-alert v-if="formError" :title="formError" type="error" :closable="false" /><ui-form :model="accountForm" label-position="top" :disabled="saving || conflicted">
        <ui-form-item label="对接账号名称" required><ui-input v-model="accountForm.name" :maxlength="100" placeholder="例如 园区管理平台" aria-label="对接账号名称" /></ui-form-item>
        <ui-form-item label="绑定平台用户" required><ui-select v-model="accountForm.username" filterable placeholder="选择平台用户" aria-label="绑定平台用户" @change="accountForm.deviceIds = []"><ui-option v-for="user in users" :key="user.username" :value="user.username" :label="userLabel(user.username)" /></ui-select></ui-form-item><p v-if="!users.length" class="topic-hint">暂无可绑定的平台用户，请先在“用户与权限”登记用户。</p>
        <ui-form-item label="可订阅主题" required><div class="topic-grants"><div v-for="topic in subscriptionTopics" :key="topic.id" class="topic-subscribe-row"><div><strong>{{ topic.name }}</strong><small class="subline">{{ topic.protocol === 'kafka' ? 'Kafka' : 'MQTT' }} · {{ querySummary(topic) }}{{ topic.enabled ? '' : ' · 已停用' }}</small></div><ui-checkbox :model-value="accountForm.topicIds.includes(topic.id)" :aria-label="`订阅 ${topic.name}`" @update:model-value="checked => toggleTopic(topic.id, checked)">订阅</ui-checkbox></div><p v-if="!subscriptionTopics.length" class="topic-hint">暂无可授权主题，请先新建消息主题。</p></div></ui-form-item><p class="topic-hint">同一主题的订阅者收到相同数据，账号及其绑定用户须有权访问该主题的数据范围。</p>
        <details v-if="legacySubscriptionTopics.length" class="topic-legacy"><summary>兼容主题订阅</summary><div v-for="topic in legacySubscriptionTopics" :key="topic.id" class="topic-subscribe-row"><span>{{ topic.name }} · {{ topic.protocol === 'kafka' ? 'Kafka' : 'MQTT' }}</span><ui-checkbox :model-value="accountForm.topicIds.includes(topic.id)" :aria-label="`订阅 ${topic.name}`" @update:model-value="checked => toggleTopic(topic.id, checked)">订阅</ui-checkbox></div></details>
        <details v-if="availableTopics.some(topic => topic.shared && !topic.query)" class="topic-legacy"><summary>旧主题发布权限</summary><p class="topic-hint">只用于已有消息通道。配置了数据查询的主题仅供外部订阅。</p><div v-for="topic in availableTopics.filter(topic => topic.shared && !topic.query)" :key="topic.id"><ui-checkbox :model-value="accountForm.publishTopicIds.includes(topic.id)" :aria-label="`发布 ${topic.name}`" @update:model-value="checked => toggleTopic(topic.id, checked, 'publishTopicIds')">发布到 {{ topic.name }}</ui-checkbox></div></details>
        <ui-form-item label="设备范围"><ui-radio-group v-model="accountForm.deviceScope" class="topic-scope-options"><ui-radio value="all">绑定用户范围内全部设备</ui-radio><ui-radio value="selected">指定设备</ui-radio></ui-radio-group></ui-form-item>
        <MessageTopicDevicePicker v-if="accountForm.deviceScope === 'selected'" :key="accountForm.username" v-model="accountForm.deviceIds" :disabled="saving || conflicted" /><p v-else class="topic-hint">随绑定用户当前设备范围变化，不能超出该用户的设备权限。</p>
        <ui-form-item label="账号有效期至"><ui-date-time v-model="accountForm.expiresAt" clearable disable-past placeholder="不填写则长期有效" /></ui-form-item><ui-form-item label="账号状态"><div class="topic-enabled"><ui-switch v-model="accountForm.enabled" /><span>{{ accountForm.enabled ? '启用对接' : '停用对接' }}</span></div></ui-form-item><p class="topic-hint">账号密钥用于外部系统自动换取订阅凭据，创建或轮换时仅显示一次。修改授权后需重新获取凭据。</p>
      </ui-form></div>
      <template #footer><div class="topic-editor-actions"><span class="topic-action-spacer" /><ui-button :disabled="saving" @click="accountEditorOpen = false">取消</ui-button><ui-button v-if="conflicted" type="primary" @click="load">刷新配置</ui-button><ui-button v-else-if="canSaveAccount" type="primary" :loading="saving" :disabled="loading" @click="saveAccount">{{ accountEditing ? '保存账号' : '创建账号' }}</ui-button></div></template>
    </ui-dialog>

    <ui-dialog v-model="secretOpen" title="保存对接账号密钥" width="min(680px, 94vw)" :close-on-click-modal="false" destroy-on-close><div v-if="accountSecret" class="topic-editor">
      <ui-alert type="warning" :closable="false" title="密钥只显示这一次，关闭后无法再次查看。请交给外部系统的服务端安全保存。" />
      <ui-descriptions :column="1"><ui-descriptions-item label="租户">{{ accountSecret.tenantId }}</ui-descriptions-item><ui-descriptions-item label="对接账号"><code class="topic-text">{{ accountSecret.id }}</code></ui-descriptions-item><ui-descriptions-item label="账号密钥"><code class="topic-text topic-secret">{{ accountSecret.secret }}</code></ui-descriptions-item></ui-descriptions>
      <ui-button @click="copyAccountSecret">复制账号与密钥</ui-button><p class="topic-hint">外部服务调用 <code>POST</code> <code class="topic-text">{{ exchangeEndpoint }}</code>，JSON 请求包含 <code>tenantId</code>、<code>accountId</code>、<code>secret</code> 和 <code>protocol</code>（mqtt 或 kafka）。临时凭据有效期为 1 小时，以接口返回的到期时间为准。</p>
    </div><template #footer><ui-button type="primary" @click="accountSecret = null">我已保存</ui-button></template></ui-dialog>

    <ui-dialog v-model="credentialOpen" :title="`订阅信息 · ${credentialAccount?.name || ''}`" width="min(780px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving" destroy-on-close><div v-if="credentialAccount" class="topic-editor">
      <ui-alert v-if="formError" :title="formError" type="error" :closable="false" /><p class="topic-hint">绑定用户：{{ userLabel(credentialAccount.username) }}。使用下方实际地址订阅消息；请在凭据到期前续期。</p>
      <div class="topic-protocol-credentials"><div v-for="protocol in ['mqtt', 'kafka']" :key="protocol"><ui-button type="primary" :loading="saving" :disabled="busy || conflicted || !!credentialReason(credentialAccount, protocol)" @click="issueCredentials(protocol)">获取 {{ protocol === 'kafka' ? 'Kafka' : 'MQTT' }} 凭据</ui-button><p>{{ credentialReason(credentialAccount, protocol) || '可生成有效期 1 小时的临时凭据' }}</p></div></div>
      <template v-if="brokerCredential"><ui-alert type="warning" :closable="false" title="连接密码只在本次展示，关闭后清除。修改授权或配置后，需重新获取临时凭据。" /><ui-descriptions :column="1">
        <ui-descriptions-item label="协议">{{ brokerCredential.protocol === 'kafka' ? 'Kafka' : 'MQTT' }}</ui-descriptions-item><ui-descriptions-item v-if="brokerCredential.broker" label="Broker"><code class="topic-text">{{ brokerCredential.broker }}</code></ui-descriptions-item><ui-descriptions-item label="用户名"><code class="topic-text">{{ brokerCredential.username }}</code></ui-descriptions-item><ui-descriptions-item label="连接密码"><code class="topic-text topic-secret">{{ brokerCredential.password }}</code></ui-descriptions-item><ui-descriptions-item v-if="brokerCredential.groupId" label="消费者组"><code class="topic-text">{{ brokerCredential.groupId }}</code></ui-descriptions-item><ui-descriptions-item v-if="brokerCredential.mechanism" label="认证机制">{{ brokerCredential.mechanism }}</ui-descriptions-item><ui-descriptions-item label="有效期至">{{ formatTopicTime(brokerCredential.expiresAt, '—') }}</ui-descriptions-item><ui-descriptions-item label="可订阅主题"><code v-for="topic in (brokerCredential.subscribeTopics || brokerCredential.topics || [])" :key="topic" class="topic-text topic-subscription">{{ topic }}</code><span v-if="!(brokerCredential.subscribeTopics || brokerCredential.topics || []).length">无</span></ui-descriptions-item><ui-descriptions-item v-if="brokerCredential.publishTopics?.length" label="旧主题发布权限"><code v-for="topic in (brokerCredential.publishTopics || [])" :key="topic" class="topic-text topic-subscription">{{ topic }}</code><span v-if="!brokerCredential.publishTopics?.length">无</span></ui-descriptions-item>
        <ui-descriptions-item v-if="brokerCredential.protocol === 'kafka' && brokerCredential.securityProtocol" label="安全协议">{{ brokerCredential.securityProtocol }}</ui-descriptions-item><ui-descriptions-item v-if="brokerCredential.protocol === 'kafka' && typeof brokerCredential.tls === 'boolean'" label="TLS">{{ brokerCredential.tls ? '启用' : '未启用' }}</ui-descriptions-item>
      </ui-descriptions><ui-button @click="copyText(credentialText, '连接凭据与主题权限已复制')">复制完整连接凭据</ui-button></template>
      <p class="topic-hint">服务端自动获取凭据：<code>POST</code> <code class="topic-text">{{ exchangeEndpoint }}</code>。提交账号的 tenantId、accountId、secret、protocol；密钥遗失时，请关闭此窗口并在账号操作中轮换。</p>
    </div><template #footer><div class="topic-editor-actions"><span class="topic-action-spacer" /><ui-button v-if="conflicted" type="primary" @click="load">刷新配置</ui-button><ui-button :disabled="saving" @click="closeCredentials">关闭</ui-button></div></template></ui-dialog>
  </div>
</template>

<style scoped>
.message-topics-page { min-width:0; }
.topic-connection-status, .topic-legacy { margin:var(--space-3) 0; }
.topic-connection-status > summary, .topic-legacy > summary { cursor:pointer; color:var(--text-muted); font-size:var(--font-size-sm); margin-bottom:var(--space-3); }
.topic-legacy > * + * { margin-top:var(--space-3); }
.topic-subscribe-row { display:grid; grid-template-columns:minmax(0,1fr) auto; align-items:center; gap:var(--space-2); padding:var(--space-2) 0; border-bottom:1px solid var(--border); }
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
.topic-grants { display:grid; grid-template-columns:minmax(0,1fr); gap:var(--space-2); max-height:230px; overflow:auto; width:100%; }
.topic-grant-row { display:grid; grid-template-columns:minmax(0,1fr) 70px 90px; align-items:center; gap:var(--space-2); padding:var(--space-2) 0; border-bottom:1px solid var(--border); }
.topic-mapping, .topic-preview { display:grid; gap:var(--space-2); min-width:0; }
.topic-mapping-row { display:grid; grid-template-columns:minmax(0,.75fr) minmax(0,1.25fr) auto; gap:var(--space-2); }
.topic-preview summary { cursor:pointer; margin-bottom:var(--space-2); }
.topic-preview-result { color:var(--text); border:1px solid var(--border); white-space:pre-wrap; overflow-wrap:anywhere; background:var(--surface-muted); border-radius:var(--radius-md); padding:var(--space-3); margin:0; max-height:240px; overflow:auto; }
.topic-scope-options { display:flex; flex-wrap:wrap; gap:var(--space-3); }
.topic-protocol-credentials { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:var(--space-3); }
.topic-protocol-credentials p { margin:var(--space-2) 0 0; color:var(--text-muted); font-size:var(--font-size-sm); overflow-wrap:anywhere; }
.topic-secret { user-select:all; }
.topic-subscription { display:block; margin:var(--space-1) 0; }
.topic-credential-status + .topic-credential-status { margin-top:var(--space-2); }
@media (max-width:767px) { .topic-mapping-row { grid-template-columns:minmax(0,1fr); } .topic-grant-row { grid-template-columns:minmax(0,1fr) auto auto; } .topic-authorization, .topic-grants, .topic-protocol-credentials { grid-template-columns:minmax(0,1fr); } .topic-editor-actions > .ui-button { flex:1 1 auto; } .topic-action-spacer { display:none; } .topic-editor { max-height:62vh; } }
</style>
