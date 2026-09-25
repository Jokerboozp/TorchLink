<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { UiMessage } from '../ui/feedback.js'
import { api, apiAll, formatTime, notifyError } from '../api'
import { confirmDelete } from '../deleteAction'
import { businessStatuses, businessStatusTones, categories, connectionStatuses, dataStatuses, deviceRoles, enabledStatuses, enabledStatusTones, label, tone } from '../labels'
import { Plus, RefreshCw, Search } from '@lucide/vue'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'
import DeviceConnection from '../components/DeviceConnection.vue'
import DeviceOnboarding from '../components/DeviceOnboarding.vue'

const emit = defineEmits(['navigate'])
const connectionDevice = ref('')
const onboarding = ref(false)
const tabs = { all: '全部', DIRECT: '独立设备', GATEWAY: '主设备', CHILD: '子设备', pending: '待登记' }
const deviceTab = ref('all')
const filters = reactive({ category: '', q: '', runtime: '' })
const products = ref([])
const registry = ref([]), registryTotal = ref(0), registryPage = ref(1), registryPageSize = ref(20)
const unregistered = ref([]), unregisteredTotal = ref(0), unregisteredPage = ref(1), unregisteredPageSize = ref(20)
const pendingCount = ref(0)
const loading = ref(false), listError = ref(''), updatesAvailable = ref(false)
const dialog = ref(false), saving = ref(false)
const gateways = ref([])
const credentialDialog = ref(false), credential = ref({})
const filtered = computed(() => Boolean(filters.category || filters.q.trim() || filters.runtime))
const pendingTab = computed(() => deviceTab.value === 'pending')

function categoryOf(productId) { return products.value.find(item => item.id === productId)?.category || 'other' }
function productName(id) { return products.value.find(item => item.id === id)?.name || id }
function roleOf(device) { return device.deviceRole || 'DIRECT' }
function relation(row) {
  const role = roleOf(row.device)
  if (role === 'GATEWAY') return `${row.childCount || 0} 个子设备`
  if (role === 'CHILD') return `所属主设备：${row.parent?.name || row.device.gatewayId || '不可查看'}`
  return '独立接入'
}

function registryQuery() {
  const query = new URLSearchParams({ page: String(registryPage.value), pageSize: String(registryPageSize.value) })
  if (deviceTab.value !== 'all') query.set('role', deviceTab.value)
  if (filters.category) query.set('category', filters.category)
  if (filters.q.trim()) query.set('q', filters.q.trim())
  if (filters.runtime) query.set('runtime', filters.runtime)
  return query.toString()
}

let loadVersion = 0, searchTimer = 0
async function load() {
  const version = ++loadVersion
  const pending = pendingTab.value
  loading.value = true
  updatesAvailable.value = false
  try {
    const [productData, list, pendingData] = await Promise.all([
      apiAll('/api/v1/products'),
      pending ? api(`/api/v1/devices?unregistered=true&page=${unregisteredPage.value}&pageSize=${unregisteredPageSize.value}`) : api(`/api/v1/device-registry?${registryQuery()}`),
      pending ? null : api('/api/v1/devices?unregistered=true&page=1&pageSize=1')
    ])
    if (version !== loadVersion) return
    products.value = productData.items || []
    listError.value = ''
    if (pending) {
      unregistered.value = list.items || []
      unregisteredTotal.value = pendingCount.value = Number(list.total ?? list.count ?? 0)
    } else {
      registry.value = list.items || []
      registryTotal.value = Number(list.total ?? list.count ?? 0)
      pendingCount.value = Number(pendingData?.total ?? pendingData?.count ?? 0)
    }
  } catch (error) {
    if (version === loadVersion) { listError.value = error?.message || '读取设备失败'; notifyError(error) }
  } finally {
    if (version === loadVersion) loading.value = false
  }
}
function changeFilter() { clearTimeout(searchTimer); registryPage.value = 1; unregisteredPage.value = 1; load() }
function search() { clearTimeout(searchTimer); searchTimer = setTimeout(changeFilter, 300) }
function changeRegistryPage(value) { registryPage.value = value; load() }
function changeRegistryPageSize(value) { registryPageSize.value = value; registryPage.value = 1; load() }
function changeUnregisteredPage(value) { unregisteredPage.value = value; load() }
function changeUnregisteredPageSize(value) { unregisteredPageSize.value = value; unregisteredPage.value = 1; load() }

// 编辑设备；新设备统一通过“添加设备”向导创建。
const blank = () => ({ id: '', name: '', productId: '', deviceRole: 'DIRECT', gatewayId: '', status: 'ENABLED', tags: [{ key: '', value: '' }], description: '' })
const form = reactive(blank())
async function loadGateways() {
  try { gateways.value = ((await apiAll('/api/v1/device-registry?role=GATEWAY')).items || []).filter(item => roleOf(item.device) === 'GATEWAY') }
  catch (error) { notifyError(error) }
}
function open(device) {
  const tags = Object.entries(device.tags || {}).map(([key, value]) => ({ key, value }))
  Object.assign(form, blank(), { ...device, deviceRole: device.deviceRole || roleOf(device), tags: tags.length ? tags : [{ key: '', value: '' }] })
  dialog.value = true
  loadGateways()
}
async function save() {
  if (saving.value || !form.id) return
  if (!form.name.trim() || !form.productId) return UiMessage.warning('请填写设备名称并选择设备模板')
  if (form.deviceRole === 'CHILD' && !form.gatewayId) return UiMessage.warning('请选择所属主设备')
  saving.value = true
  try {
    const tags = Object.fromEntries(form.tags.filter(row => row.key.trim()).map(row => [row.key.trim(), row.value]))
    const value = { ...form, tags }
    if (value.deviceRole !== 'CHILD') value.gatewayId = ''
    await api(`/api/v1/device-registry/${encodeURIComponent(value.id)}`, { method: 'PUT', body: JSON.stringify(value) })
    dialog.value = false
    UiMessage.success('设备已保存')
    await load()
  } catch (error) { notifyError(error) } finally { saving.value = false }
}
async function register(id) {
  try {
    const result = await api(`/api/v1/discovered-devices/${encodeURIComponent(id)}/register`, { method: 'POST', body: '{}' })
    if (result.credential) showCredential(result.credential)
    UiMessage.success('设备已登记，可在设备列表中查看')
    await load()
  } catch (error) { notifyError(error) }
}
function showCredential(value) { credential.value = value; credentialDialog.value = true }
async function copyCredential() {
  try { await navigator.clipboard.writeText(`X-Device-Key: ${credential.value.accessKey}\nX-Device-Secret: ${credential.value.secret}`); UiMessage.success('凭证已复制') }
  catch { UiMessage.warning('浏览器不允许复制，请手动选择文本') }
}
function hasReported(row) { return Number(row.runtimeState?.lastSeenAt || 0) > 0 }
function openRaw(id) { emit('navigate', 'raw', { deviceId: id }) }
function removeDevice(row) { return confirmDelete({ label: row.name || row.id, path: `/api/v1/device-registry/${encodeURIComponent(row.id)}`, onDeleted: load }) }
function rowActions(row) {
  return [
    { key: 'detail', label: '详情', onClick: () => { connectionDevice.value = row.device.id } },
    { key: 'raw', label: '查看数据', permission: 'menu:raw', disabled: !hasReported(row), onClick: () => openRaw(row.device.id) }, // 未上报时禁用而不隐藏，各行操作位置保持一致。
    { key: 'edit', label: '编辑', permission: 'PUT /api/v1/device-registry/:id', onClick: () => open(row.device) },
    { key: 'delete', label: '删除', type: 'danger', permission: 'DELETE /api/v1/device-registry/:id', onClick: () => removeDevice(row.device) }
  ]
}
function startOnboarding() { connectionDevice.value = ''; onboarding.value = true }
function leaveOnboarding(id = '') { onboarding.value = false; connectionDevice.value = id; load() }

const realtime = () => { updatesAvailable.value = true }
onMounted(() => {
  let detail = {}
  try { detail = JSON.parse(sessionStorage.getItem('iot:navigation-detail') || '{}') } catch { detail = {} }
  sessionStorage.removeItem('iot:navigation-detail')
  if (detail.onboarding) onboarding.value = true
  if (detail.deviceId) connectionDevice.value = detail.deviceId
  load()
  window.addEventListener('iot:realtime', realtime)
})
onBeforeUnmount(() => { clearTimeout(searchTimer); window.removeEventListener('iot:realtime', realtime) })
</script>

<template>
  <DeviceOnboarding v-if="onboarding" @close="leaveOnboarding()" @done="leaveOnboarding()" @detail="leaveOnboarding" @navigate="(page, query) => emit('navigate', page, query)" />
  <template v-else>
    <DeviceConnection v-if="connectionDevice" :key="connectionDevice" :device-id="connectionDevice" @device="id => connectionDevice = id" @close="connectionDevice = ''" @navigate="(page, query) => { connectionDevice = ''; emit('navigate', page, query) }" />
    <FilterBar>
      <ui-radio-group v-model="deviceTab" class="segmented-choice-group" aria-label="设备分组" @change="changeFilter">
        <ui-radio-button v-for="(text, key) in tabs" :key="key" :value="key">{{ text }}<template v-if="key === 'pending' && pendingCount"> · {{ pendingCount }}</template></ui-radio-button>
      </ui-radio-group>
      <template v-if="!pendingTab">
        <ui-input v-model="filters.q" class="devices-search" clearable maxlength="128" placeholder="搜索设备名称或编号" aria-label="搜索设备" @input="search" @keyup.enter="changeFilter"><template #prefix><Search class="devices-search-icon" /></template></ui-input>
        <ui-select v-model="filters.category" class="devices-select" clearable placeholder="全部设备类型" aria-label="设备类型" @change="changeFilter">
          <ui-option v-for="(text, key) in categories" :key="key" :value="key" :label="text" />
        </ui-select>
        <ui-select v-model="filters.runtime" class="devices-select" clearable placeholder="全部运行状态" aria-label="运行状态" @change="changeFilter">
          <ui-option v-for="(text, key) in businessStatuses" :key="key" :value="key" :label="text" />
        </ui-select>
      </template>
      <span v-if="updatesAvailable" class="devices-update-hint" role="status">有新数据，点击“刷新”查看</span>
      <template #actions>
        <ui-button :loading="loading" @click="load"><RefreshCw />刷新</ui-button>
        <ui-button v-permission="'POST /api/v1/device-registry'" type="primary" @click="startOnboarding"><Plus />添加设备</ui-button>
      </template>
    </FilterBar>

    <DataTableCard v-if="!pendingTab" :title="`${tabs[deviceTab]} · ${registryTotal} 台`" :page="registryPage" :page-size="registryPageSize" :total="registryTotal" :error="listError" @retry="load" @update:page="changeRegistryPage" @update:page-size="changeRegistryPageSize">
      <div class="only-desktop">
        <ui-table v-loading="loading" :data="registry" :empty-text="filtered ? '没有符合筛选条件的设备' : '暂无设备，点击“添加设备”开始'">
          <ui-table-column label="设备" min-width="200"><template #default="{ row }"><button type="button" class="device-name" @click="connectionDevice = row.device.id">{{ row.device.name }}</button><small class="subline">{{ row.device.id }}</small></template></ui-table-column>
          <ui-table-column label="设备模板" min-width="160"><template #default="{ row }">{{ productName(row.device.productId) }}</template></ui-table-column>
          <ui-table-column label="设备类型" min-width="120"><template #default="{ row }">{{ label(categories, categoryOf(row.device.productId)) }}</template></ui-table-column>
          <ui-table-column label="运行状态" width="110"><template #default="{ row }"><StatusDot :tone="tone(businessStatusTones, row.runtimeState?.businessStatus || 'NEVER_SEEN')" :label="label(businessStatuses, row.runtimeState?.businessStatus || 'NEVER_SEEN')" /></template></ui-table-column>
          <ui-table-column label="启用" width="90"><template #default="{ row }"><StatusDot :tone="tone(enabledStatusTones, row.device.status)" :label="label(enabledStatuses, row.device.status)" /></template></ui-table-column>
          <ui-table-column v-if="deviceTab !== 'DIRECT'" label="所属关系" min-width="170"><template #default="{ row }">{{ relation(row) }}<small v-if="row.device.autoRegistered" class="subline">由协议自动登记</small></template></ui-table-column>
          <ui-table-column label="最后活跃" min-width="160"><template #default="{ row }">{{ formatTime(row.runtimeState?.lastSeenAt) }}</template></ui-table-column>
          <ui-table-column label="操作" fixed="right" width="176" align="right"><template #default="{ row }"><RowActions :actions="rowActions(row)" /></template></ui-table-column>
        </ui-table>
      </div>
      <ul class="device-cards only-mobile" :class="{ 'ui-loading': loading }">
        <li v-for="row in registry" :key="row.device.id" class="device-card">
          <button type="button" class="device-card__title" @click="connectionDevice = row.device.id"><strong>{{ row.device.name }}</strong><small>{{ row.device.id }}</small></button>
          <div class="device-card__meta">
            <StatusDot :tone="tone(businessStatusTones, row.runtimeState?.businessStatus || 'NEVER_SEEN')" :label="label(businessStatuses, row.runtimeState?.businessStatus || 'NEVER_SEEN')" />
            <span>{{ productName(row.device.productId) }}</span>
            <span v-if="roleOf(row.device) !== 'DIRECT'">{{ relation(row) }}</span>
            <span>最后活跃 {{ formatTime(row.runtimeState?.lastSeenAt) }}</span>
          </div>
          <RowActions :actions="rowActions(row)" />
        </li>
        <li v-if="!registry.length && !loading" class="device-cards__empty">{{ filtered ? '没有符合筛选条件的设备' : '暂无设备，点击“添加设备”开始' }}</li>
      </ul>
    </DataTableCard>

    <DataTableCard v-else :title="`平台已收到数据、尚未登记的设备 · ${unregisteredTotal} 台`" :page="unregisteredPage" :page-size="unregisteredPageSize" :total="unregisteredTotal" :error="listError" @retry="load" @update:page="changeUnregisteredPage" @update:page-size="changeUnregisteredPageSize">
      <ui-table v-loading="loading" :data="unregistered" empty-text="当前没有待登记的设备">
        <ui-table-column prop="deviceId" label="设备编号" min-width="190" />
        <ui-table-column label="设备模板" min-width="150"><template #default="{ row }">{{ productName(row.productId) }}</template></ui-table-column>
        <ui-table-column label="运行状态" width="110"><template #default="{ row }"><StatusDot :tone="tone(businessStatusTones, row.businessStatus)" :label="label(businessStatuses, row.businessStatus)" /></template></ui-table-column>
        <ui-table-column label="连接状态" width="100"><template #default="{ row }">{{ label(connectionStatuses, row.connectionStatus) }}</template></ui-table-column>
        <ui-table-column label="数据状态" width="100"><template #default="{ row }">{{ label(dataStatuses, row.dataStatus) }}</template></ui-table-column>
        <ui-table-column label="最后活跃" min-width="160"><template #default="{ row }">{{ formatTime(row.lastSeenAt) }}</template></ui-table-column>
        <ui-table-column label="操作" fixed="right" width="110" align="right"><template #default="{ row }"><ui-button v-permission="'POST /api/v1/discovered-devices/:id/register'" size="small" text type="primary" @click="register(row.deviceId)">一键登记</ui-button></template></ui-table-column>
      </ui-table>
    </DataTableCard>

    <ui-dialog v-model="dialog" :title="`编辑设备 · ${form.name || form.id}`" width="min(560px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving" destroy-on-close>
      <ui-form :model="form" label-position="top" :disabled="saving" @submit.prevent="save">
        <ui-form-item label="设备编号"><ui-input :model-value="form.id" disabled /></ui-form-item>
        <ui-form-item label="设备名称" required><ui-input v-model="form.name" maxlength="256" placeholder="例如 一层东侧烟感" /></ui-form-item>
        <ui-form-item label="设备模板" required>
          <ui-select v-model="form.productId" filterable placeholder="选择设备模板"><ui-option v-for="item in products" :key="item.id" :label="item.name" :value="item.id" /></ui-select>
        </ui-form-item>
        <ui-form-item label="设备角色">
          <ui-radio-group v-model="form.deviceRole" class="segmented-choice-group" aria-label="设备角色"><ui-radio-button v-for="(text, key) in deviceRoles" :key="key" :value="key">{{ text }}</ui-radio-button></ui-radio-group>
        </ui-form-item>
        <ui-form-item v-if="form.deviceRole === 'CHILD'" label="所属主设备" required>
          <ui-select v-model="form.gatewayId" filterable placeholder="选择主设备"><ui-option v-for="item in gateways" :key="item.device.id" :label="`${item.device.name} · ${item.device.id}`" :value="item.device.id" /></ui-select>
        </ui-form-item>
        <ui-collapse class="device-advanced">
          <ui-collapse-item title="状态、标签与备注" name="advanced">
            <ui-form-item label="启用设备"><ui-switch v-model="form.status" active-value="ENABLED" inactive-value="DISABLED" active-text="已启用" inactive-text="已停用" /></ui-form-item>
            <div class="device-tags">
              <p class="device-tags__title">设备标签<small>用名称和内容记录楼层、区域等检索线索</small></p>
              <div v-for="(row, index) in form.tags" :key="index" class="device-tag-row"><ui-input v-model="row.key" placeholder="名称，例如楼层" aria-label="标签名称" /><ui-input v-model="row.value" placeholder="内容，例如一层" aria-label="标签内容" /><ui-button text @click="form.tags.splice(index, 1)">移除</ui-button></div>
              <ui-button size="small" @click="form.tags.push({ key: '', value: '' })">添加标签</ui-button>
            </div>
            <ui-form-item label="备注" class="top-gap"><ui-input v-model="form.description" type="textarea" :rows="2" /></ui-form-item>
          </ui-collapse-item>
        </ui-collapse>
      </ui-form>
      <template #footer><ui-button :disabled="saving" @click="dialog = false">取消</ui-button><ui-button v-permission="'PUT /api/v1/device-registry/:id'" type="primary" :loading="saving" @click="save">保存设备</ui-button></template>
    </ui-dialog>

    <ui-dialog v-model="credentialDialog" title="设备凭证" width="min(520px, 92vw)" @closed="credential = {}">
      <ui-alert title="密钥只显示这一次，请立即复制并安全保存。" type="warning" :closable="false" />
      <ui-descriptions class="top-gap" :column="1" border><ui-descriptions-item label="接入密钥"><code>{{ credential.accessKey }}</code></ui-descriptions-item><ui-descriptions-item label="设备密钥"><code class="break-all">{{ credential.secret }}</code></ui-descriptions-item></ui-descriptions>
      <template #footer><ui-button @click="credentialDialog = false">关闭</ui-button><ui-button type="primary" @click="copyCredential">复制凭证</ui-button></template>
    </ui-dialog>
  </template>
</template>

<style scoped>
.devices-update-hint { color: var(--info-text); font-size: var(--font-size-sm); }
.devices-search-icon { width: 14px; height: 14px; color: var(--text-muted); }
:deep(.filter-bar__filters .ui-select.devices-select) { width: 150px; }
.device-name { padding: 0; color: var(--text-strong); background: none; border: 0; font: inherit; font-weight: var(--font-weight-semibold); text-align: left; cursor: pointer; }
.device-name:hover { color: var(--primary-text); text-decoration: underline; }
.device-cards { position: relative; margin: 0; padding: 0; list-style: none; }
.device-card { display: grid; gap: var(--space-2); padding: var(--space-3) var(--space-4); border-bottom: 1px solid var(--border); }
.device-card:last-child { border-bottom: 0; }
.device-card__title { display: grid; gap: 2px; padding: 0; color: var(--text-strong); background: none; border: 0; font: inherit; text-align: left; cursor: pointer; }
.device-card__title strong { font-weight: var(--font-weight-semibold); }
.device-card__title small { color: var(--text-muted); font-size: var(--font-size-xs); overflow-wrap: anywhere; }
.device-card__meta { display: flex; flex-wrap: wrap; gap: var(--space-1) var(--space-3); color: var(--text-secondary); font-size: var(--font-size-sm); }
.device-card :deep(.row-actions) { justify-content: flex-start; }
.device-cards__empty { padding: var(--space-8) var(--space-4); color: var(--text-muted); text-align: center; }
.device-advanced { margin-top: var(--space-1); }
.device-tags { display: grid; gap: var(--space-2); }
.device-tags__title { display: grid; gap: 2px; margin: 0; color: var(--text-secondary); font-size: var(--font-size-sm); font-weight: var(--font-weight-medium); }
.device-tags__title small { color: var(--text-muted); font-size: var(--font-size-xs); font-weight: 400; }
.device-tag-row { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto; gap: var(--space-2); align-items: center; }
@media (max-width: 767px) {
  :deep(.filter-bar__filters .ui-input.devices-search) { flex-basis: 100%; }
  :deep(.filter-bar__filters .ui-select.devices-select) { width: auto; }
}
</style>
