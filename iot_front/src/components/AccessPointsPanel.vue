<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { api, apiAll, formatTime, notifyError } from '../api'
import { confirmDelete } from '../deleteAction'
import { runtimeStatusTones, tone } from '../labels'
import { statusLabel, transportLabel } from '../presentation'
import { UiMessage } from '../ui/feedback.js'
import { Plus, RefreshCw } from '@lucide/vue'
import DataTableCard from './layout/DataTableCard.vue'
import FilterBar from './layout/FilterBar.vue'
import RowActions from './layout/RowActions.vue'
import StatusDot from './layout/StatusDot.vue'
import ProtocolAccessSettings from './ProtocolAccessSettings.vue'

// 接入点（接口中的 device-access-profiles）：共享监听、主动连接和 Modbus 采集。传入 productId 时只管理该模板的接入点。
const props = defineProps({ productId: { type: String, default: '' } })
const profiles = ref([]), products = ref([]), protocols = ref([])
const snapshots = ref({})
const snapshot = id => snapshots.value[id] || { sessions: [], recentDevices: [] }
const loading = ref(false), testingId = ref(''), result = ref(null)
const profileOpen = ref(false), editingProfile = ref(false), savingListener = ref(false)
const blankListener = () => ({ id: '', productId: props.productId || '', protocolId: '', protocolVersion: '', mode: 'listener', network: 'tcp', host: '0.0.0.0', publicHost: '', port: 26875, timeoutMs: 5000, autoRegister: false, enabled: true, connectionMode: 'listen', deviceId: '', queries: [], childProducts: [], unitId: 1, intervalMs: 10000, retries: 0, wireFormat: '' })
const listener = reactive(blankListener())
const visibleProfiles = computed(() => props.productId ? profiles.value.filter(item => item.productId === props.productId) : profiles.value)
const productName = id => products.value.find(item => item.id === id)?.name || id

let loadVersion = 0
async function load() {
  const version = ++loadVersion
  loading.value = true
  try {
    const [access, connectors, productData, catalog] = await Promise.all([api('/api/v2/device-access-profiles'), api('/api/v1/connectors'), apiAll('/api/v1/products'), api('/api/v2/protocols')])
    if (version !== loadVersion) return
    snapshots.value = Object.fromEntries((connectors.items || []).filter(item => item.profile).map(item => [item.profile.id, item]))
    profiles.value = (access.items || []).map(item => snapshot(item.id).profile || item)
    products.value = productData.items || []
    protocols.value = catalog.items || []
  } catch (error) { if (version === loadVersion) notifyError(error) } finally { if (version === loadVersion) loading.value = false }
}

function resetListener(value = {}) { for (const key of Object.keys(listener)) delete listener[key]; Object.assign(listener, blankListener(), value) }
let bindingRevision = 0
async function selectProduct(id) {
  const revision = ++bindingRevision
  listener.protocolId = ''; listener.protocolVersion = ''
  if (!id) return
  try {
    const current = await api(`/api/v2/products/${encodeURIComponent(id)}/protocol-binding`)
    if (revision !== bindingRevision) return
    listener.protocolId = current.protocolId || ''; listener.protocolVersion = current.version || ''
  } catch (error) { if (revision === bindingRevision) notifyError(error) }
}
function createProfile() {
  bindingRevision++
  resetListener()
  editingProfile.value = false
  profileOpen.value = true
  if (listener.productId) selectProduct(listener.productId)
}
function editProfile(profile) {
  bindingRevision++
  resetListener({ ...JSON.parse(JSON.stringify(profile)), mode: profile.mode || 'poll', network: profile.network || 'tcp', connectionMode: profile.mode === 'listener' ? profile.connectionMode || 'listen' : '' })
  editingProfile.value = true
  profileOpen.value = true
}
watch(() => listener.network, value => { if (value !== 'tcp') { listener.connectionMode = 'listen'; listener.deviceId = ''; listener.queries = [] } })
watch(() => listener.connectionMode, value => { if (value === 'dial') listener.publicHost = '' })

async function saveListener() {
  if (savingListener.value) return
  if (!listener.id || !listener.productId || !listener.protocolId || !listener.protocolVersion) return UiMessage.warning('请填写接入点标识，并确认设备模板已绑定已发布的协议版本')
  if (!editingProfile.value && profiles.value.some(item => item.id === listener.id)) return UiMessage.warning('接入点标识已存在，请在列表中编辑')
  savingListener.value = true
  try {
    result.value = await api(editingProfile.value ? `/api/v2/device-access-profiles/${encodeURIComponent(listener.id)}` : '/api/v2/device-access-profiles', { method: editingProfile.value ? 'PUT' : 'POST', body: JSON.stringify(listener) })
    UiMessage.success('接入点已保存')
    profileOpen.value = false
    await load()
  } catch (error) { notifyError(error) } finally { savingListener.value = false }
}
async function toggleProfile(profile) {
  try {
    await api(`/api/v2/device-access-profiles/${encodeURIComponent(profile.id)}`, { method: 'PUT', body: JSON.stringify({ ...profile, enabled: !profile.enabled }) })
    await load()
  } catch (error) { notifyError(error) }
}
async function testProfile(profile) {
  testingId.value = profile.id
  try {
    result.value = await api(`/api/v2/device-access-profiles/${encodeURIComponent(profile.id)}/test`, { method: 'POST', body: '{}' })
    UiMessage.success('连接与单次采集测试通过')
    await load()
  } catch (error) { notifyError(error) } finally { testingId.value = '' }
}
function removeProfile(row) { return confirmDelete({ label: row.id, path: `/api/v2/device-access-profiles/${encodeURIComponent(row.id)}`, onDeleted: load, blockedHint: '请先停用接入点，并解除关联设备后再删除。' }) }
function statusText(value) { return ({ LISTENING: '监听中', DISABLED: '已停用', PENDING: '待启动', ONLINE: '在线采集', ERROR: '运行异常' })[value] || statusLabel(value) }
function profileActions(row) {
  return [
    { key: 'edit', label: '编辑', permission: 'PUT /api/v2/device-access-profiles/:id', onClick: () => editProfile(row) },
    { key: 'test', label: '连接测试', permission: 'POST /api/v2/device-access-profiles/:id/test', hidden: row.mode === 'listener', loading: testingId.value === row.id, onClick: () => testProfile(row) },
    { key: 'toggle', label: row.enabled ? '停用' : '启用', permission: 'PUT /api/v2/device-access-profiles/:id', onClick: () => toggleProfile(row) },
    { key: 'delete', label: '删除', type: 'danger', permission: 'DELETE /api/v2/device-access-profiles/:id', onClick: () => removeProfile(row) }
  ]
}
function address(row) { return `${row.mode === 'listener' && row.connectionMode !== 'dial' && row.publicHost ? row.publicHost : row.host}:${row.port}` }
function modeText(row) { return row.mode === 'listener' ? `${transportLabel(String(row.network || '').toUpperCase())} · ${row.connectionMode === 'dial' ? '平台连接设备' : '设备连接平台'}` : `Modbus 采集 · 站号 ${row.unitId}` }

watch(() => props.productId, load)
onMounted(load)
</script>

<template>
  <div class="access-points">
    <FilterBar>
      <p v-if="productId" class="access-points__intro">共享监听可供同模板的多台设备使用；主动连接和 Modbus 采集按设备单独配置，通常在添加设备时自动创建。</p>
      <template #actions>
        <ui-button :loading="loading" @click="load"><RefreshCw />刷新</ui-button>
        <ui-button v-permission="'POST /api/v2/device-access-profiles'" type="primary" @click="createProfile"><Plus />新建接入点</ui-button>
      </template>
    </FilterBar>
    <DataTableCard :title="`接入点 · ${visibleProfiles.length} 个`">
      <ui-table :data="visibleProfiles" :loading="loading" row-key="id" empty-text="暂无接入点。TCP / UDP 协议设备在添加设备时可以直接新建共享监听">
        <ui-table-column type="expand"><template #default="{ row }">
          <div class="access-points__details">
            <h4>当前在线会话</h4>
            <ui-table :data="snapshot(row.id).sessions" empty-text="暂无在线会话"><ui-table-column label="设备"><template #default="{ row: session }">{{ session.deviceId || '尚未识别设备' }}</template></ui-table-column><ui-table-column prop="remoteAddress" label="远端地址" /><ui-table-column prop="protocolId" label="协议" /><ui-table-column prop="protocolVersion" label="版本" /><ui-table-column label="最后有效报文"><template #default="{ row: session }">{{ formatTime(session.lastSeenAt) }}</template></ui-table-column></ui-table>
            <h4>最近接入设备（按创建时间，最多 20 台）</h4>
            <ui-table :data="snapshot(row.id).recentDevices" empty-text="暂无关联设备"><ui-table-column prop="deviceId" label="设备编号" /><ui-table-column prop="name" label="名称" /><ui-table-column label="创建时间"><template #default="{ row: device }">{{ formatTime(device.createdAt) }}</template></ui-table-column></ui-table>
          </div>
        </template></ui-table-column>
        <ui-table-column label="接入点" min-width="190"><template #default="{ row }"><b>{{ row.id }}</b><small v-if="row.deviceId" class="subline">目标设备：{{ row.deviceId }}</small></template></ui-table-column>
        <ui-table-column v-if="!productId" label="设备模板" min-width="170"><template #default="{ row }">{{ productName(row.productId) }}</template></ui-table-column>
        <ui-table-column label="地址 / 端口" min-width="210"><template #default="{ row }">{{ address(row) }}<small class="subline">{{ modeText(row) }}</small></template></ui-table-column>
        <ui-table-column label="协议版本" min-width="170"><template #default="{ row }">{{ row.protocolId }}@{{ row.protocolVersion }}</template></ui-table-column>
        <ui-table-column label="在线会话" width="90"><template #default="{ row }">{{ snapshot(row.id).sessions?.length || 0 }}</template></ui-table-column>
        <ui-table-column label="状态" width="110"><template #default="{ row }"><StatusDot :tone="tone(runtimeStatusTones, row.runtimeStatus)" :label="statusText(row.runtimeStatus)" /></template></ui-table-column>
        <ui-table-column label="最近错误" min-width="180" show-overflow-tooltip><template #default="{ row }">{{ row.lastError || '—' }}</template></ui-table-column>
        <ui-table-column label="操作" width="176" fixed="right" align="right"><template #default="{ row }"><RowActions :actions="profileActions(row)" /></template></ui-table-column>
      </ui-table>
    </DataTableCard>

    <ui-dialog v-model="profileOpen" :title="editingProfile ? `编辑接入点 · ${listener.id}` : '新建接入点'" width="min(820px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!savingListener" :show-close="!savingListener" class="profile-editor-dialog" destroy-on-close>
      <ui-form :disabled="savingListener" label-position="top">
        <section class="profile-editor-section">
          <header><h3>适用模板</h3><p>接入点使用设备模板当前绑定的协议版本。</p></header>
          <div class="profile-field-grid">
            <ui-form-item label="接入点标识" required><ui-input v-model="listener.id" :disabled="editingProfile" placeholder="例如 dahua-tcp" /></ui-form-item>
            <ui-form-item label="设备模板" required><ui-select v-model="listener.productId" filterable :disabled="Boolean(productId) || editingProfile" placeholder="选择设备模板" @change="selectProduct"><ui-option v-for="item in products" :key="item.id" :label="item.name" :value="item.id" /></ui-select></ui-form-item>
          </div>
          <p class="profile-protocol-summary">当前协议：<strong>{{ protocols.find(item => item.definition.id === listener.protocolId)?.definition.name || listener.protocolId || '选择模板后自动读取' }}</strong> · 版本 <strong>{{ listener.protocolVersion || '请先在设备模板绑定已发布协议' }}</strong></p>
        </section>
        <section class="profile-editor-section">
          <header><h3>连接方式</h3><p>{{ listener.mode === 'poll' ? '平台定时连接并采集设备数据。' : listener.connectionMode === 'dial' ? '平台主动连接指定设备，需要填写设备可达地址。' : listener.network === 'udp' ? '设备向平台监听端口发送 UDP 报文。' : '设备主动连接平台监听端口，可由多台设备共享。' }}</p></header>
          <div v-if="listener.mode === 'listener'" class="profile-field-grid">
            <ui-form-item label="网络"><ui-select v-model="listener.network"><ui-option label="TCP" value="tcp" /><ui-option label="UDP" value="udp" /></ui-select></ui-form-item>
            <ui-form-item v-if="listener.network === 'tcp'" label="连接方向"><ui-select v-model="listener.connectionMode"><ui-option value="listen" label="设备连接平台" /><ui-option value="dial" label="平台连接设备" /></ui-select></ui-form-item>
          </div>
        </section>
        <section class="profile-editor-section">
          <header><h3>地址与端口</h3><p>{{ listener.connectionMode === 'dial' || listener.mode === 'poll' ? '填写平台可以访问的设备地址。' : '本机监听地址只供服务绑定；现场设备须填写平台对外地址和端口。' }}</p></header>
          <div class="profile-field-grid">
            <ui-form-item v-if="listener.connectionMode === 'dial' || listener.mode === 'poll'" label="目标设备编号"><ui-input v-model="listener.deviceId" placeholder="已登记设备的编号" /></ui-form-item>
            <ui-form-item :label="listener.connectionMode === 'dial' || listener.mode === 'poll' ? '设备地址' : '本机监听地址'"><ui-input v-model="listener.host" :placeholder="listener.connectionMode === 'dial' || listener.mode === 'poll' ? '设备可达域名或 IP' : '例如 0.0.0.0'" /></ui-form-item>
            <ui-form-item v-if="listener.mode === 'listener' && listener.connectionMode !== 'dial'" label="平台对外地址"><ui-input v-model="listener.publicHost" placeholder="现场设备可访问的域名或 IP" /></ui-form-item>
            <ui-form-item label="端口"><ui-input-number v-model="listener.port" :min="1" :max="65535" /></ui-form-item>
          </div>
        </section>
        <section class="profile-editor-section">
          <header><h3>运行设置</h3><p>设置超时、自动登记及启用状态。</p></header>
          <div class="profile-field-grid">
            <ui-form-item label="操作超时（秒）"><ui-input-number :model-value="listener.timeoutMs / 1000" :min="0.001" :max="30" :step="0.5" @update:model-value="value => listener.timeoutMs = Math.round(Number(value) * 1000)" /></ui-form-item>
            <template v-if="listener.mode === 'poll'">
              <ui-form-item label="站号"><ui-input-number v-model="listener.unitId" :min="0" :max="255" /></ui-form-item>
              <ui-form-item label="采集周期（秒）"><ui-input-number :model-value="listener.intervalMs / 1000" :min="1" @update:model-value="value => listener.intervalMs = Math.round(Number(value) * 1000)" /></ui-form-item>
              <ui-form-item label="重试次数"><ui-input-number v-model="listener.retries" :min="0" :max="3" /></ui-form-item>
            </template>
          </div>
          <div class="profile-toggle-list">
            <div v-if="listener.mode === 'listener'" class="profile-toggle-row"><div><strong>自动登记新设备</strong><small>协议识别出未登记的设备后，自动加入设备列表。</small></div><ui-switch v-model="listener.autoRegister" /></div>
            <div class="profile-toggle-row"><div><strong>启用接入点</strong><small>保存后按此状态运行监听或采集。</small></div><ui-switch v-model="listener.enabled" /></div>
          </div>
          <ui-collapse v-if="listener.mode === 'listener'" class="profile-advanced"><ui-collapse-item :title="listener.network === 'tcp' ? '定时读取与子设备（可选）' : '子设备映射（可选）'" name="advanced"><ProtocolAccessSettings :profile="listener" :can-poll="listener.network === 'tcp'" :products="products" :product-id="listener.productId" /></ui-collapse-item></ui-collapse>
        </section>
      </ui-form>
      <template #footer><div class="profile-editor-footer"><ui-button :disabled="savingListener" @click="profileOpen = false">取消</ui-button><ui-button v-permission="['POST /api/v2/device-access-profiles', 'PUT /api/v2/device-access-profiles/:id']" type="primary" :loading="savingListener" @click="saveListener">保存接入点</ui-button></div></template>
    </ui-dialog>
    <details v-if="result" class="technical-details"><summary>最近操作结果</summary><pre>{{ JSON.stringify(result, null, 2) }}</pre></details>
  </div>
</template>

<style scoped>
.access-points__intro { flex: 1 1 320px; margin: 0; color: var(--text-muted); font-size: var(--font-size-sm); }
.access-points__details { display: grid; gap: var(--space-3); padding: var(--space-4) var(--space-6); }
.access-points__details h4 { margin: 0; color: var(--text-strong); font-size: var(--font-size-sm); font-weight: var(--font-weight-semibold); }
.profile-editor-section + .profile-editor-section { margin-top: var(--space-4); padding-top: var(--space-4); border-top: 1px solid var(--border); }
.profile-editor-section header { margin-bottom: var(--space-3); }
.profile-editor-section h3 { margin: 0; color: var(--text-strong); font-size: var(--font-size-md); font-weight: var(--font-weight-semibold); }
.profile-editor-section header p { margin: 2px 0 0; color: var(--text-muted); font-size: var(--font-size-xs); }
.profile-field-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--space-3) var(--space-4); }
.profile-field-grid > * { min-width: 0; }
.profile-field-grid :deep(.n-form-item) { margin-bottom: 0; }
.profile-field-grid :deep(.ui-input-number) { width: 100%; }
.profile-protocol-summary { margin: var(--space-3) 0 0; padding: var(--space-2) var(--space-3); color: var(--text-secondary); background: var(--surface-muted); border-radius: var(--radius-md); font-size: var(--font-size-sm); overflow-wrap: anywhere; }
.profile-protocol-summary strong { color: var(--text-strong); font-weight: var(--font-weight-semibold); }
.profile-toggle-list { display: grid; gap: var(--space-2); margin-top: var(--space-4); }
.profile-toggle-row { display: flex; align-items: center; justify-content: space-between; gap: var(--space-4); padding: var(--space-3); border: 1px solid var(--border); border-radius: var(--radius-lg); }
.profile-toggle-row strong, .profile-toggle-row small { display: block; }
.profile-toggle-row strong { color: var(--text-strong); font-size: var(--font-size-sm); }
.profile-toggle-row small { margin-top: 2px; color: var(--text-muted); font-size: var(--font-size-xs); }
.profile-advanced { margin-top: var(--space-4); border-top: 1px solid var(--border); }
.profile-editor-footer { display: flex; justify-content: flex-end; gap: var(--space-2); width: 100%; }
.technical-details { margin-top: var(--space-4); }
@media (max-width: 767px) {
  .profile-field-grid { grid-template-columns: 1fr; }
}
</style>
