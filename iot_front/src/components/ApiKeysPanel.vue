<script setup>
import { computed, reactive, ref } from 'vue'
import { Copy } from '@lucide/vue'
import { UiMessage, UiMessageBox } from '../ui/feedback.js'
import { api, formatTime, notifyError } from '../api'
import DataTableCard from './layout/DataTableCard.vue'
import RowActions from './layout/RowActions.vue'
import StatusDot from './layout/StatusDot.vue'

// 开放接口密钥绑定平台用户：外部系统按该用户的功能和设备范围访问，能力项只能进一步收窄。
const props = defineProps({ users: { type: Array, default: () => [] } })
const keys = ref([]), capabilities = ref([]), loading = ref(false), saving = ref(false), dialog = ref(''), editingId = ref(''), issuedKey = ref('')
const form = reactive({ name: '', username: '', capabilities: [], expiresAt: null })
const capabilityName = id => capabilities.value.find(item => item.id === id)?.name || id
const userName = username => { const user = props.users.find(item => item.username === username); return user ? `${user.displayName || user.username}（${user.username}）` : `${username}（已不存在）` }
const expired = key => key.expiresAt > 0 && key.expiresAt <= Date.now()
const keyStatus = key => !key.enabled ? { tone: 'neutral', label: '停用' } : expired(key) ? { tone: 'warning', label: '已过期' } : { tone: 'success', label: '启用' }
const baseUrl = computed(() => `${window.location.origin}/api/open/v1`)
let loadVersion = 0

async function load() {
  const version = ++loadVersion
  loading.value = true
  try {
    const data = await api('/api/v1/access/api-keys')
    if (version !== loadVersion) return
    keys.value = data.items || []
    capabilities.value = data.capabilities || []
  } catch (e) { if (version === loadVersion) notifyError(e) }
  finally { if (version === loadVersion) loading.value = false }
}
function edit(key) {
  editingId.value = key?.id || ''
  Object.assign(form, { name: key?.name || '', username: key?.username || '', capabilities: [...(key?.capabilities || [])], expiresAt: key?.expiresAt || null })
  dialog.value = 'edit'
}
function toggleCapability(id, checked) { form.capabilities = checked ? [...new Set([...form.capabilities, id])] : form.capabilities.filter(item => item !== id) }
async function save() {
  if (saving.value) return
  saving.value = true
  try {
    const body = JSON.stringify({ name: form.name, username: form.username, capabilities: form.capabilities, expiresAt: form.expiresAt || 0 })
    if (editingId.value) {
      await api(`/api/v1/access/api-keys/${encodeURIComponent(editingId.value)}`, { method: 'PUT', body })
      dialog.value = ''
      UiMessage.success('已保存')
    } else {
      const data = await api('/api/v1/access/api-keys', { method: 'POST', body })
      issuedKey.value = data.apiKey
      dialog.value = 'issued'
    }
    await load()
  } catch (e) { notifyError(e) }
  finally { saving.value = false }
}
async function setEnabled(key, enabled) {
  try {
    await api(`/api/v1/access/api-keys/${encodeURIComponent(key.id)}`, { method: 'PUT', body: JSON.stringify({ name: key.name, capabilities: key.capabilities, expiresAt: expired(key) ? 0 : key.expiresAt || 0, enabled }) })
    UiMessage.success(enabled ? '已启用' : '已停用，使用此密钥的请求将被拒绝')
    await load()
  } catch (e) { notifyError(e) }
}
async function remove(key) {
  try {
    await UiMessageBox.confirm(`确认删除密钥“${key.name}”？删除后使用此密钥的外部系统将无法访问。`, '删除确认', { type: 'warning' })
    await api(`/api/v1/access/api-keys/${encodeURIComponent(key.id)}`, { method: 'DELETE' })
    await load()
  } catch (e) { if (e !== 'cancel' && e !== 'close') notifyError(e) }
}
async function copy(text, message) {
  try { await navigator.clipboard.writeText(text); UiMessage.success(message) }
  catch { UiMessage.warning('复制失败，请手动选择文本复制') }
}
function closeIssued() { issuedKey.value = ''; dialog.value = '' }
function actions(key) {
  return [
    { key: 'edit', label: '编辑', permission: 'PUT /api/v1/access/api-keys/:id', onClick: () => edit(key) },
    { key: 'toggle', label: key.enabled ? '停用' : '启用', permission: 'PUT /api/v1/access/api-keys/:id', onClick: () => setEnabled(key, !key.enabled) },
    { key: 'delete', label: '删除', type: 'danger', permission: 'DELETE /api/v1/access/api-keys/:id', onClick: () => remove(key) }
  ]
}
defineExpose({ load, create: () => edit() })
load()
</script>

<template>
  <div class="api-keys">
    <ui-card class="api-keys-guide">
      <h3>对外开放接口</h3>
      <p>外部系统使用密钥调用 <code>{{ baseUrl }}</code>，请求头携带 <code>Authorization: Bearer &lt;密钥&gt;</code>。可查询和上报告警、上报设备消息、查询设备数据，以及调用智能助手问答。</p>
      <p>每个密钥绑定一个平台用户，按该用户的功能权限和设备范围访问；能力项只能进一步收窄。建议为每个外部系统单独创建用户和密钥，并只在对方服务端保存密钥，不要放在浏览器页面中。</p>
    </ui-card>
    <DataTableCard :title="`开放接口密钥 · ${keys.length} 个`">
      <ui-table v-loading="loading" :data="keys" empty-text="暂无密钥，点击新建密钥为外部系统授权">
        <ui-table-column prop="name" label="名称" min-width="140" />
        <ui-table-column label="绑定用户" min-width="160"><template #default="{ row }">{{ userName(row.username) }}</template></ui-table-column>
        <ui-table-column label="开放能力" min-width="220"><template #default="{ row }">{{ row.capabilities.map(capabilityName).join('、') }}</template></ui-table-column>
        <ui-table-column label="有效期至" width="170"><template #default="{ row }">{{ row.expiresAt ? formatTime(row.expiresAt) : '长期有效' }}</template></ui-table-column>
        <ui-table-column label="状态" width="100"><template #default="{ row }"><StatusDot v-bind="keyStatus(row)" /></template></ui-table-column>
        <ui-table-column label="创建" width="170"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></ui-table-column>
        <ui-table-column label="操作" width="200" fixed="right" align="right"><template #default="{ row }"><RowActions :actions="actions(row)" /></template></ui-table-column>
      </ui-table>
    </DataTableCard>
    <ui-dialog :model-value="dialog === 'edit'" :title="editingId ? '编辑密钥' : '新建密钥'" width="min(620px,94vw)" :close-on-click-modal="false" @close="dialog = ''">
      <ui-form label-position="top" :disabled="saving">
        <ui-form-item label="名称" required><ui-input v-model="form.name" maxlength="64" placeholder="例如 园区管理平台"/></ui-form-item>
        <ui-form-item label="绑定用户" required>
          <ui-select v-model="form.username" :disabled="!!editingId" filterable placeholder="选择外部系统使用的平台用户">
            <ui-option v-for="user in users" :key="user.username" :label="`${user.displayName || user.username}（${user.username}）`" :value="user.username" />
          </ui-select>
        </ui-form-item>
        <ui-form-item label="开放能力" required>
          <div class="api-keys-capabilities">
            <ui-checkbox v-for="item in capabilities" :key="item.id" :model-value="form.capabilities.includes(item.id)" @update:model-value="checked => toggleCapability(item.id, checked)">{{ item.name }}</ui-checkbox>
          </div>
        </ui-form-item>
        <ui-form-item label="有效期至"><ui-date-time v-model="form.expiresAt" clearable disable-past placeholder="不填写则长期有效"/></ui-form-item>
        <p class="api-keys-note">查询与处置告警、查询设备数据、智能问答还需要绑定用户拥有对应功能权限；上报消息和告警只接受绑定用户可见且已启用的设备。</p>
      </ui-form>
      <template #footer>
        <ui-button :disabled="saving" @click="dialog = ''">取消</ui-button>
        <ui-button type="primary" :loading="saving" :disabled="!form.name.trim() || !form.username || !form.capabilities.length" @click="save">{{ editingId ? '保存修改' : '创建密钥' }}</ui-button>
      </template>
    </ui-dialog>
    <ui-dialog :model-value="dialog === 'issued'" title="密钥已创建" width="min(620px,94vw)" :close-on-click-modal="false" @close="closeIssued">
      <ui-alert type="warning" :closable="false" title="密钥只显示这一次，关闭后无法再次查看。请立即复制并交给外部系统安全保存。" />
      <div class="api-keys-issued"><code>{{ issuedKey }}</code><ui-button size="small" @click="copy(issuedKey, '密钥已复制')"><Copy />复制</ui-button></div>
      <template #footer><ui-button type="primary" @click="closeIssued">我已保存</ui-button></template>
    </ui-dialog>
  </div>
</template>

<style scoped>
.api-keys { display: grid; gap: 14px; }
.api-keys-guide h3 { margin: 0 0 6px; font-size: 15px; }
.api-keys-guide p { margin: 6px 0 0; color: var(--text-muted); font-size: 13px; line-height: 1.6; }
.api-keys-guide code, .api-keys-issued code { overflow-wrap: anywhere; font-family: var(--font-mono, ui-monospace, monospace); font-size: 12px; }
.api-keys-capabilities { display: flex; flex-wrap: wrap; gap: 8px 18px; }
.api-keys-note { margin: 0; color: var(--text-muted); font-size: 12px; line-height: 1.6; }
.api-keys-issued { display: flex; align-items: center; gap: 10px; margin-top: 14px; padding: 12px; background: var(--surface-muted); border-radius: 6px; }
.api-keys-issued code { flex: 1; min-width: 0; user-select: all; }
</style>
