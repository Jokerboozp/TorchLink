<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, apiAll, formatTime, notifyError, parseJSON, pretty } from '../api'
import { businessStatuses, categories, connectionStatuses, dataStatuses, deviceRoles, enabledStatuses, label, tagType } from '../labels'
import DeviceConnection from '../components/DeviceConnection.vue'
const connectionDevice = ref('')

const emit = defineEmits(['navigate'])
const products = ref([])
const registryOptions = ref([])
const unregisteredOptions = ref([])
const deviceTab = ref('independent')
const deviceGroups = { independent:{label:'独立设备',role:'DIRECT'}, main:{label:'主设备',role:'GATEWAY'}, children:{label:'子设备',role:'CHILD'} }
const deviceCategory = ref('')
const loading = ref(false)
const updatesAvailable = ref(false)
const dialog = ref(false)
const saving = ref(false)
const credentialDialog = ref(false)
const credential = ref({})
const registryPage = ref(1)
const registryPageSize = ref(20)
const unregisteredPage = ref(1)
const unregisteredPageSize = ref(20)
const filteredRegistry = computed(() => registryOptions.value.filter(row =>
  roleOf(row.device) === deviceGroups[deviceTab.value].role && matchesCategory(row.device.productId)))
const registryTotal = computed(() => filteredRegistry.value.length)
const registry = computed(() => filteredRegistry.value.slice((registryPage.value - 1) * registryPageSize.value, registryPage.value * registryPageSize.value))
const filteredUnregistered = computed(() => unregisteredOptions.value.filter(row => matchesCategory(row.productId)))
const unregisteredTotal = computed(() => filteredUnregistered.value.length)
const unregistered = computed(() => filteredUnregistered.value.slice((unregisteredPage.value - 1) * unregisteredPageSize.value, unregisteredPage.value * unregisteredPageSize.value))
function categoryOf(productId) { return products.value.find(item => item.id === productId)?.category || 'other' }
function matchesCategory(productId) { return !deviceCategory.value || categoryOf(productId) === deviceCategory.value }
function changeDeviceFilter() { registryPage.value = 1; unregisteredPage.value = 1 }

const blank = () => ({ id:'', code:'', name:'', productId:'', deviceRole:'DIRECT', gatewayId:'', status:'ENABLED', tags:pretty({}), description:'' })
const form = reactive(blank())
const gateways = computed(() => registryOptions.value.filter(item => roleOf(item.device) === 'GATEWAY'))

function roleOf(device) {
  return device.deviceRole || (products.value.find(item => item.id === device.productId)?.category === 'gateway' ? 'GATEWAY' : 'DIRECT')
}
function productName(id) { return products.value.find(item => item.id === id)?.name || id }
function deviceName(id) { return registryOptions.value.find(item => item.device.id === id)?.device.name || id || '未设置' }
function relation(row) {
  const role = roleOf(row.device)
  if (role === 'GATEWAY') return `${row.childCount || 0} 个子设备`
  if (role === 'CHILD') return `所属网关：${deviceName(row.device.gatewayId)}`
  return '独立接入'
}

let loadVersion = 0
async function load() {
  const version = ++loadVersion
  loading.value = true
  updatesAvailable.value = false
  try {
    const [productData, runtimeData, optionData] = await Promise.all([
      apiAll('/api/v1/products'),
      apiAll('/api/v1/devices?unregistered=true'),
      apiAll('/api/v1/device-registry')
    ])
    if (version !== loadVersion) return
    products.value = productData.items || []
    registryOptions.value = optionData.items || []
    unregisteredOptions.value = runtimeData.items || []
    registryPage.value = Math.min(registryPage.value, Math.max(1, Math.ceil(registryTotal.value / registryPageSize.value)))
    unregisteredPage.value = Math.min(unregisteredPage.value, Math.max(1, Math.ceil(unregisteredTotal.value / unregisteredPageSize.value)))
  } catch (error) {
    if (version === loadVersion) notifyError(error)
  } finally {
    if (version === loadVersion) loading.value = false
  }
}
function changeRegistryPage(value) { registryPage.value = value }
function changeRegistryPageSize(value) { registryPageSize.value = value; registryPage.value = 1 }
function changeUnregisteredPage(value) { unregisteredPage.value = value }
function changeUnregisteredPageSize(value) { unregisteredPageSize.value = value; unregisteredPage.value = 1 }
function open(device) { Object.assign(form, blank(), device ? { ...device, code:device.id, tags:pretty(device.tags || {}) } : {deviceRole:deviceGroups[deviceTab.value].role}); dialog.value = true }

async function save() {
  if (saving.value) return
  if (!form.name.trim() || !form.productId) return ElMessage.warning('请填写设备名称并选择产品')
  if (form.deviceRole === 'CHILD' && !form.gatewayId) return ElMessage.warning('请选择所属网关')
  saving.value = true
  try {
    if (!form.id && form.code && registryOptions.value.some(item => item.device.id === form.code)) return ElMessage.warning('设备标识已存在，请在列表中编辑')
    if (!form.id && !form.code) form.code = `device_${crypto.randomUUID().replaceAll('-', '').slice(0, 12)}`
    const value = { ...form, id:form.id || form.code, tags:parseJSON(form.tags, '标签结构化数据') }
    delete value.code
    const editing = Boolean(form.id)
    if (value.deviceRole !== 'CHILD') value.gatewayId = ''
    const result = await api(editing ? `/api/v1/device-registry/${encodeURIComponent(value.id)}` : '/api/v1/device-registry', { method:editing ? 'PUT' : 'POST', body:JSON.stringify(value) })
    dialog.value = false
    if (result.credential) showCredential(result.credential)
    ElMessage.success('设备已保存')
    await load()
  } catch (error) { notifyError(error) } finally { saving.value = false }
}
async function register(id) {
  try {
    const result = await api(`/api/v1/discovered-devices/${encodeURIComponent(id)}/register`, { method:'POST', body:'{}' })
    if (result.credential) showCredential(result.credential)
    ElMessage.success('设备已注册并移入正式设备列表')
    await load()
  } catch (error) { notifyError(error) }
}
async function rotate(id) {
  try {
    await ElMessageBox.confirm('轮换后旧凭证立即失效，确定继续？', '轮换设备凭证', { type:'warning' })
    const result = await api(`/api/v1/device-registry/${encodeURIComponent(id)}/credentials`, { method:'POST', body:'{}' })
    showCredential(result.credential)
    await load()
  } catch (error) { if (error !== 'cancel') notifyError(error) }
}
function showCredential(value) { credential.value = value; credentialDialog.value = true }
async function copyCredential() { await navigator.clipboard.writeText(`X-Device-Key: ${credential.value.accessKey}\nX-Device-Secret: ${credential.value.secret}`); ElMessage.success('凭证已复制') }
function guide(id) { emit('navigate', 'integration', { deviceId:id }) }
function hasReported(row) { return Number(row.runtimeState?.lastSeenAt || 0) > 0 }
function openRaw(id) { emit('navigate', 'raw', { deviceId:id }) }

const realtime = () => { updatesAvailable.value = true }
onMounted(() => { load(); window.addEventListener('iot:realtime', realtime) })
onBeforeUnmount(() => window.removeEventListener('iot:realtime', realtime))
</script>

<template>
  <DeviceConnection v-if="connectionDevice" :key="connectionDevice" :device-id="connectionDevice" @device="id=>connectionDevice=id" @close="connectionDevice=''" @navigate="(page,query)=>{connectionDevice='';emit('navigate',page,query)}" />
  <el-tabs v-model="deviceTab" @tab-change="changeDeviceFilter" aria-label="设备分组"><el-tab-pane v-for="(group, key) in deviceGroups" :key="key" :label="group.label" :name="key" /></el-tabs>
  <div class="page-toolbar"><el-button type="primary" @click="open()">添加{{ deviceGroups[deviceTab].label }}</el-button><el-button :loading="loading" @click="load">刷新设备</el-button><span>当前{{ deviceGroups[deviceTab].label }} {{ registryTotal }} 台</span><span v-if="updatesAvailable" role="status">有新数据，点击“刷新设备”更新</span></div>
  <el-form inline class="device-filters" @submit.prevent><el-form-item label="设备类型"><el-select v-model="deviceCategory" clearable placeholder="全部类型" aria-label="设备类型" style="width:220px" @change="changeDeviceFilter"><el-option v-for="(text, key) in categories" :key="key" :value="key" :label="text" /></el-select></el-form-item><el-form-item><el-button @click="deviceCategory='';changeDeviceFilter()">重置筛选</el-button></el-form-item></el-form>
  <el-card shadow="never" class="surface-card table-card">
    <el-table v-loading="loading" :data="registry" stripe>
      <el-table-column label="设备" min-width="190"><template #default="{ row }"><b>{{ row.device.name }}</b><small class="subline">{{ row.device.id }}</small></template></el-table-column>
      <el-table-column label="产品" min-width="160"><template #default="{ row }">{{ productName(row.device.productId) }}</template></el-table-column>
      <el-table-column label="设备类型" min-width="130"><template #default="{ row }">{{ label(categories, categoryOf(row.device.productId)) }}</template></el-table-column>
      <el-table-column label="运行状态" width="105"><template #default="{ row }"><el-tag :type="tagType(row.runtimeState?.businessStatus)" round>{{ label(businessStatuses, row.runtimeState?.businessStatus || 'NEVER_SEEN') }}</el-tag></template></el-table-column>
      <el-table-column label="启用状态" width="105"><template #default="{ row }"><el-tag :type="tagType(row.device.status)" round>{{ label(enabledStatuses, row.device.status) }}</el-tag></template></el-table-column>
      <el-table-column label="设备角色" width="120"><template #default="{ row }"><el-tag round>{{ label(deviceRoles, roleOf(row.device), '直接设备') }}</el-tag><small v-if="row.device.autoRegistered" class="subline">网关自动注册</small></template></el-table-column>
      <el-table-column label="所属关系" min-width="150"><template #default="{ row }">{{ relation(row) }}</template></el-table-column>
      <el-table-column label="最后活跃" min-width="160"><template #default="{ row }">{{ formatTime(row.runtimeState?.lastSeenAt) }}</template></el-table-column>
      <el-table-column label="操作" fixed="right" width="270" align="center"><template #default="{ row }"><div class="table-actions"><el-button plain @click="connectionDevice=row.device.id">连接详情</el-button><el-button v-if="!hasReported(row)" plain type="primary" @click="row.credentialSupported ? guide(row.device.id) : connectionDevice=row.device.id">配置接入</el-button><el-button v-else plain type="success" @click="openRaw(row.device.id)">查看数据</el-button><el-dropdown trigger="click"><el-button plain aria-label="更多设备操作">更多操作</el-button><template #dropdown><el-dropdown-menu><el-dropdown-item @click="open(row.device)">编辑设备</el-dropdown-item><el-dropdown-item v-if="row.credentialSupported" @click="rotate(row.device.id)">轮换凭证</el-dropdown-item></el-dropdown-menu></template></el-dropdown></div></template></el-table-column>
      <template #empty><el-empty description="暂无设备" /></template>
    </el-table>
    <div class="list-pagination"><el-pagination v-model:current-page="registryPage" v-model:page-size="registryPageSize" :total="registryTotal" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next, jumper" @current-change="changeRegistryPage" @size-change="changeRegistryPageSize" /></div>
  </el-card>

  <el-card v-if="deviceTab === 'independent' && unregisteredTotal" shadow="never" class="surface-card table-card top-gap">
    <template #header><div class="card-header"><strong>未注册设备</strong><small>{{ unregisteredTotal }} 台</small></div></template>
    <el-table :data="unregistered" stripe>
      <el-table-column prop="deviceId" label="设备标识" min-width="190" /><el-table-column label="产品" min-width="150"><template #default="{ row }">{{ productName(row.productId) }}</template></el-table-column><el-table-column label="业务状态" width="105"><template #default="{ row }"><el-tag :type="tagType(row.businessStatus)" round>{{ label(businessStatuses, row.businessStatus) }}</el-tag></template></el-table-column><el-table-column label="连接状态" width="110"><template #default="{ row }">{{ label(connectionStatuses, row.connectionStatus) }}</template></el-table-column><el-table-column label="数据状态" width="110"><template #default="{ row }">{{ label(dataStatuses, row.dataStatus) }}</template></el-table-column><el-table-column label="最后活跃" min-width="170"><template #default="{ row }">{{ formatTime(row.lastSeenAt) }}</template></el-table-column><el-table-column label="操作" fixed="right" width="120" align="center"><template #default="{ row }"><div class="table-actions"><el-button type="primary" plain @click="register(row.deviceId)">一键注册</el-button></div></template></el-table-column>
      <template #empty><el-empty description="当前没有未注册设备" /></template>
    </el-table>
    <div class="list-pagination"><el-pagination v-model:current-page="unregisteredPage" v-model:page-size="unregisteredPageSize" :total="unregisteredTotal" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next, jumper" @current-change="changeUnregisteredPage" @size-change="changeUnregisteredPageSize" /></div>
  </el-card>

  <el-dialog v-model="dialog" :title="form.id ? '编辑设备' : '添加设备'" width="min(560px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving">
    <el-form :model="form" label-position="top" :disabled="saving" @submit.prevent="save">
      <el-form-item label="所属产品" required>
        <el-select v-model="form.productId" filterable placeholder="选择产品"><el-option v-for="item in products" :key="item.id" :label="item.name" :value="item.id" /></el-select>
        <el-button v-if="!products.length" link @click="dialog=false;emit('navigate','products')">新建产品</el-button>
      </el-form-item>
      <el-form-item label="设备名称" required><el-input v-model="form.name" maxlength="256" placeholder="例如 一层东侧烟感" /></el-form-item>
      <el-form-item label="设备标识"><el-input v-model="form.code" :disabled="!!form.id" placeholder="与设备上报标识一致；留空自动生成" /></el-form-item>
      <el-form-item label="设备角色"><el-radio-group v-model="form.deviceRole"><el-radio-button v-for="(text, key) in deviceRoles" :key="key" :value="key">{{ text }}</el-radio-button></el-radio-group></el-form-item>
      <el-form-item v-if="form.deviceRole === 'CHILD'" label="所属网关" required><el-select v-model="form.gatewayId" filterable><el-option v-for="item in gateways" :key="item.device.id" :label="item.device.name" :value="item.device.id" /></el-select></el-form-item>
      <el-collapse><el-collapse-item title="更多设置" name="advanced">
        <el-form-item label="启用设备"><el-switch v-model="form.status" active-value="ENABLED" inactive-value="DISABLED" /></el-form-item>
        <el-form-item label="标签（JSON）"><el-input v-model="form.tags" type="textarea" :rows="3" /></el-form-item>
        <el-form-item label="备注"><el-input v-model="form.description" type="textarea" :rows="2" /></el-form-item>
      </el-collapse-item></el-collapse>
    </el-form>
    <template #footer><el-button :disabled="saving" @click="dialog = false">取消</el-button><el-button type="primary" :loading="saving" @click="save">保存设备</el-button></template>
  </el-dialog>

  <el-dialog v-model="credentialDialog" title="设备凭证" @closed="credential={}" width="min(520px, 92vw)"><el-alert title="密钥只显示这一次，请立即复制并安全保存。" type="warning" :closable="false" /><el-descriptions class="top-gap" :column="1" border><el-descriptions-item label="接入密钥"><code>{{ credential.accessKey }}</code></el-descriptions-item><el-descriptions-item label="设备密钥"><code class="break-all">{{ credential.secret }}</code></el-descriptions-item></el-descriptions><template #footer><el-button @click="credentialDialog = false">关闭</el-button><el-button type="primary" @click="copyCredential">复制凭证</el-button></template></el-dialog>
</template>
