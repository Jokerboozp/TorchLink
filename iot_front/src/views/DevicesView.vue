<script setup>
import { createClientId } from '../clientId' /* 引入当前代码需要的依赖。 */
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue' /* 引入当前代码需要的依赖。 */
import { UiMessage } from '../ui/feedback.js' /* 引入当前代码需要的依赖。 */
import { api, apiAll, formatTime, notifyError, parseJSON, pretty } from '../api' /* 引入当前代码需要的依赖。 */
import { businessStatuses, categories, connectionStatuses, dataStatuses, deviceRoles, enabledStatuses, label, tagType } from '../labels' /* 引入当前代码需要的依赖。 */
import DeviceConnection from '../components/DeviceConnection.vue' /* 引入当前代码需要的依赖。 */
const connectionDevice = ref('') /* 声明 connectionDevice。 */

const emit = defineEmits(['navigate']) /* 声明 emit。 */
const products = ref([]) /* 声明 products。 */
const registryOptions = ref([]) /* 声明 registryOptions。 */
const unregisteredOptions = ref([]) /* 声明 unregisteredOptions。 */
const deviceTab = ref('independent') /* 声明 deviceTab。 */
const deviceGroups = { independent:{label:'独立设备',role:'DIRECT'}, main:{label:'主设备',role:'GATEWAY'}, children:{label:'子设备',role:'CHILD'} } /* 声明 deviceGroups。 */
const deviceCategory = ref('') /* 声明 deviceCategory。 */
const loading = ref(false) /* 声明 loading。 */
const updatesAvailable = ref(false) /* 声明 updatesAvailable。 */
const dialog = ref(false) /* 声明 dialog。 */
const saving = ref(false) /* 声明 saving。 */
const credentialDialog = ref(false) /* 声明 credentialDialog。 */
const credential = ref({}) /* 声明 credential。 */
const registryPage = ref(1) /* 声明 registryPage。 */
const registryPageSize = ref(20) /* 声明 registryPageSize。 */
const unregisteredPage = ref(1) /* 声明 unregisteredPage。 */
const unregisteredPageSize = ref(20) /* 声明 unregisteredPageSize。 */
const filteredRegistry = computed(() => registryOptions.value.filter(row => /* 声明 filteredRegistry。 */
  roleOf(row.device) === deviceGroups[deviceTab.value].role && matchesCategory(row.device.productId))) /* 执行当前语句并推进处理流程。 */
const registryTotal = computed(() => filteredRegistry.value.length) /* 声明 registryTotal。 */
const registry = computed(() => filteredRegistry.value.slice((registryPage.value - 1) * registryPageSize.value, registryPage.value * registryPageSize.value)) /* 声明 registry。 */
const filteredUnregistered = computed(() => unregisteredOptions.value.filter(row => matchesCategory(row.productId))) /* 声明 filteredUnregistered。 */
const unregisteredTotal = computed(() => filteredUnregistered.value.length) /* 声明 unregisteredTotal。 */
const unregistered = computed(() => filteredUnregistered.value.slice((unregisteredPage.value - 1) * unregisteredPageSize.value, unregisteredPage.value * unregisteredPageSize.value)) /* 声明 unregistered。 */
function categoryOf(productId) { return products.value.find(item => item.id === productId)?.category || 'other' } /* 定义 categoryOf 函数。 */
function matchesCategory(productId) { return !deviceCategory.value || categoryOf(productId) === deviceCategory.value } /* 定义 matchesCategory 函数。 */
function changeDeviceFilter() { registryPage.value = 1; unregisteredPage.value = 1 } /* 定义 changeDeviceFilter 函数。 */

const blank = () => ({ id:'', code:'', name:'', productId:'', deviceRole:'DIRECT', gatewayId:'', status:'ENABLED', tags:pretty({}), description:'' }) /* 声明 blank。 */
const form = reactive(blank()) /* 声明 form。 */
const gateways = computed(() => registryOptions.value.filter(item => roleOf(item.device) === 'GATEWAY')) /* 声明 gateways。 */

function roleOf(device) { /* 定义 roleOf 函数。 */
  return device.deviceRole || (products.value.find(item => item.id === device.productId)?.category === 'gateway' ? 'GATEWAY' : 'DIRECT') /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
function productName(id) { return products.value.find(item => item.id === id)?.name || id } /* 定义 productName 函数。 */
function deviceName(id) { return registryOptions.value.find(item => item.device.id === id)?.device.name || id || '未设置' } /* 定义 deviceName 函数。 */
function relation(row) { /* 定义 relation 函数。 */
  const role = roleOf(row.device) /* 声明 role。 */
  if (role === 'GATEWAY') return `${row.childCount || 0} 个子设备` /* 判断条件并选择处理分支。 */
  if (role === 'CHILD') return `所属网关：${deviceName(row.device.gatewayId)}` /* 判断条件并选择处理分支。 */
  return '独立接入' /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

let loadVersion = 0 /* 声明 loadVersion。 */
async function load() { /* 定义 load 函数。 */
  const version = ++loadVersion /* 声明 version。 */
  loading.value = true /* 更新 loading.value 的值。 */
  updatesAvailable.value = false /* 更新 updatesAvailable.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const [productData, runtimeData, optionData] = await Promise.all([ /* 执行当前语句并推进处理流程。 */
      apiAll('/api/v1/products'), /* 执行当前语句并推进处理流程。 */
      apiAll('/api/v1/devices?unregistered=true'), /* 执行当前语句并推进处理流程。 */
      apiAll('/api/v1/device-registry') /* 执行当前语句并推进处理流程。 */
    ]) /* 结束当前表达式或代码块。 */
    if (version !== loadVersion) return /* 判断条件并选择处理分支。 */
    products.value = productData.items || [] /* 更新 products.value 的值。 */
    registryOptions.value = optionData.items || [] /* 更新 registryOptions.value 的值。 */
    unregisteredOptions.value = runtimeData.items || [] /* 更新 unregisteredOptions.value 的值。 */
    registryPage.value = Math.min(registryPage.value, Math.max(1, Math.ceil(registryTotal.value / registryPageSize.value))) /* 更新 registryPage.value 的值。 */
    unregisteredPage.value = Math.min(unregisteredPage.value, Math.max(1, Math.ceil(unregisteredTotal.value / unregisteredPageSize.value))) /* 更新 unregisteredPage.value 的值。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    if (version === loadVersion) notifyError(error) /* 判断条件并选择处理分支。 */
  } finally { /* 结束当前表达式或代码块。 */
    if (version === loadVersion) loading.value = false /* 判断条件并选择处理分支。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
function changeRegistryPage(value) { registryPage.value = value } /* 定义 changeRegistryPage 函数。 */
function changeRegistryPageSize(value) { registryPageSize.value = value; registryPage.value = 1 } /* 定义 changeRegistryPageSize 函数。 */
function changeUnregisteredPage(value) { unregisteredPage.value = value } /* 定义 changeUnregisteredPage 函数。 */
function changeUnregisteredPageSize(value) { unregisteredPageSize.value = value; unregisteredPage.value = 1 } /* 定义 changeUnregisteredPageSize 函数。 */
function open(device) { Object.assign(form, blank(), device ? { ...device, code:device.id, tags:pretty(device.tags || {}) } : {deviceRole:deviceGroups[deviceTab.value].role}); dialog.value = true } /* 定义 open 函数。 */

async function save() { /* 定义 save 函数。 */
  if (saving.value) return /* 判断条件并选择处理分支。 */
  if (!form.name.trim() || !form.productId) return UiMessage.warning('请填写设备名称并选择产品') /* 判断条件并选择处理分支。 */
  if (form.deviceRole === 'CHILD' && !form.gatewayId) return UiMessage.warning('请选择所属网关') /* 判断条件并选择处理分支。 */
  saving.value = true /* 更新 saving.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    if (!form.id && form.code && registryOptions.value.some(item => item.device.id === form.code)) return UiMessage.warning('设备标识已存在，请在列表中编辑') /* 判断条件并选择处理分支。 */
    if (!form.id && !form.code) form.code = `device_${createClientId().replaceAll('-', '').slice(0, 12)}` /* 判断条件并选择处理分支。 */
    const value = { ...form, id:form.id || form.code, tags:parseJSON(form.tags, '标签结构化数据') } /* 声明 value。 */
    delete value.code /* 执行当前语句并推进处理流程。 */
    const editing = Boolean(form.id) /* 声明 editing。 */
    if (value.deviceRole !== 'CHILD') value.gatewayId = '' /* 判断条件并选择处理分支。 */
    const result = await api(editing ? `/api/v1/device-registry/${encodeURIComponent(value.id)}` : '/api/v1/device-registry', { method:editing ? 'PUT' : 'POST', body:JSON.stringify(value) }) /* 声明 result。 */
    dialog.value = false /* 更新 dialog.value 的值。 */
    if (result.credential) showCredential(result.credential) /* 判断条件并选择处理分支。 */
    UiMessage.success('设备已保存') /* 执行当前语句并推进处理流程。 */
    await load() /* 等待异步操作完成。 */
  } catch (error) { notifyError(error) } finally { saving.value = false } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
async function register(id) { /* 定义 register 函数。 */
  try { /* 执行当前语句并推进处理流程。 */
    const result = await api(`/api/v1/discovered-devices/${encodeURIComponent(id)}/register`, { method:'POST', body:'{}' }) /* 声明 result。 */
    if (result.credential) showCredential(result.credential) /* 判断条件并选择处理分支。 */
    UiMessage.success('设备已注册并移入正式设备列表') /* 执行当前语句并推进处理流程。 */
    await load() /* 等待异步操作完成。 */
  } catch (error) { notifyError(error) } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
function showCredential(value) { credential.value = value; credentialDialog.value = true } /* 定义 showCredential 函数。 */
async function copyCredential() { await navigator.clipboard.writeText(`X-Device-Key: ${credential.value.accessKey}\nX-Device-Secret: ${credential.value.secret}`); UiMessage.success('凭证已复制') } /* 定义 copyCredential 函数。 */
function hasReported(row) { return Number(row.runtimeState?.lastSeenAt || 0) > 0 } /* 定义 hasReported 函数。 */
function openRaw(id) { emit('navigate', 'raw', { deviceId:id }) } /* 定义 openRaw 函数。 */

const realtime = () => { updatesAvailable.value = true } /* 声明 realtime。 */
onMounted(() => { load(); window.addEventListener('iot:realtime', realtime) }) /* 执行当前语句并推进处理流程。 */
onBeforeUnmount(() => window.removeEventListener('iot:realtime', realtime)) /* 执行当前语句并推进处理流程。 */
</script>

<template>
  <DeviceConnection v-if="connectionDevice" :key="connectionDevice" :device-id="connectionDevice" @device="id=>connectionDevice=id" @close="connectionDevice=''" @navigate="(page,query)=>{connectionDevice='';emit('navigate',page,query)}" /> <!-- 渲染 DeviceConnection 界面元素。 -->
  <ui-tabs v-model="deviceTab" @tab-change="changeDeviceFilter" aria-label="设备分组"><ui-tab-pane v-for="(group, key) in deviceGroups" :key="key" :label="group.label" :name="key" /></ui-tabs> <!-- 渲染 ui-tabs 界面元素。 -->
  <div class="page-toolbar"><ui-button v-permission="'POST /api/v1/device-registry'" type="primary" @click="open()">添加{{ deviceGroups[deviceTab].label }}</ui-button><ui-button :loading="loading" @click="load">刷新设备</ui-button><span>当前{{ deviceGroups[deviceTab].label }} {{ registryTotal }} 台</span><span v-if="updatesAvailable" role="status">有新数据，点击“刷新设备”更新</span></div> <!-- 渲染 div 界面元素。 -->
  <ui-form inline class="device-filters" @submit.prevent><ui-form-item label="设备类型"><ui-select v-model="deviceCategory" clearable placeholder="全部类型" aria-label="设备类型" style="width:220px" @change="changeDeviceFilter"><ui-option v-for="(text, key) in categories" :key="key" :value="key" :label="text" /></ui-select></ui-form-item><ui-form-item><ui-button @click="deviceCategory='';changeDeviceFilter()">重置筛选</ui-button></ui-form-item></ui-form> <!-- 渲染 ui-form 界面元素。 -->
  <ui-card shadow="never" class="surface-card table-card"> <!-- 渲染 ui-card 界面元素。 -->
    <ui-table v-loading="loading" :data="registry" stripe> <!-- 渲染 ui-table 界面元素。 -->
      <ui-table-column label="设备" min-width="190"><template #default="{ row }"><b>{{ row.device.name }}</b><small class="subline">{{ row.device.id }}</small></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
      <ui-table-column label="产品" min-width="160"><template #default="{ row }">{{ productName(row.device.productId) }}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
      <ui-table-column label="设备类型" min-width="130"><template #default="{ row }">{{ label(categories, categoryOf(row.device.productId)) }}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
      <ui-table-column label="运行状态" width="105"><template #default="{ row }"><ui-tag :type="tagType(row.runtimeState?.businessStatus)" round>{{ label(businessStatuses, row.runtimeState?.businessStatus || 'NEVER_SEEN') }}</ui-tag></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
      <ui-table-column label="启用状态" width="105"><template #default="{ row }"><span class="device-enabled-state" :class="{ 'is-enabled': row.device.status === 'ENABLED' }"><i aria-hidden="true" />{{ label(enabledStatuses, row.device.status) }}</span></template></ui-table-column> <!-- 启用状态用圆点和文字，减少重复标签。 -->
      <ui-table-column label="设备角色" width="120"><template #default="{ row }"><span class="device-role-text">{{ label(deviceRoles, roleOf(row.device), '直接设备') }}</span><small v-if="row.device.autoRegistered" class="subline">网关自动注册</small></template></ui-table-column> <!-- 设备角色使用普通文字，保留自动注册说明。 -->
      <ui-table-column label="所属关系" min-width="150"><template #default="{ row }">{{ relation(row) }}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
      <ui-table-column label="最后活跃" min-width="160"><template #default="{ row }">{{ formatTime(row.runtimeState?.lastSeenAt) }}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
      <ui-table-column label="操作" fixed="right" width="240" align="center"><template #default="{ row }"><div class="table-actions"><ui-button plain @click="connectionDevice=row.device.id">连接详情</ui-button><ui-button v-if="hasReported(row)" v-permission="'menu:raw'" plain type="success" @click="openRaw(row.device.id)">查看数据</ui-button><ui-button v-permission="'PUT /api/v1/device-registry/:id'" plain @click="open(row.device)">编辑</ui-button></div></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
      <template #empty><ui-empty description="暂无设备" /></template>
    </ui-table> <!-- 结束当前界面区域。 -->
    <div class="list-pagination"><ui-pagination v-model:current-page="registryPage" v-model:page-size="registryPageSize" :total="registryTotal" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next, jumper" @current-change="changeRegistryPage" @size-change="changeRegistryPageSize" /></div> <!-- 渲染 div 界面元素。 -->
  </ui-card> <!-- 结束当前界面区域。 -->

  <ui-card v-if="deviceTab === 'independent' && unregisteredTotal" shadow="never" class="surface-card table-card top-gap"> <!-- 渲染 ui-card 界面元素。 -->
    <template #header><div class="card-header"><strong>未注册设备</strong><small>{{ unregisteredTotal }} 台</small></div></template>
    <ui-table :data="unregistered" stripe> <!-- 渲染 ui-table 界面元素。 -->
      <ui-table-column prop="deviceId" label="设备标识" min-width="190" /><ui-table-column label="产品" min-width="150"><template #default="{ row }">{{ productName(row.productId) }}</template></ui-table-column><ui-table-column label="业务状态" width="105"><template #default="{ row }"><ui-tag :type="tagType(row.businessStatus)" round>{{ label(businessStatuses, row.businessStatus) }}</ui-tag></template></ui-table-column><ui-table-column label="连接状态" width="110"><template #default="{ row }">{{ label(connectionStatuses, row.connectionStatus) }}</template></ui-table-column><ui-table-column label="数据状态" width="110"><template #default="{ row }">{{ label(dataStatuses, row.dataStatus) }}</template></ui-table-column><ui-table-column label="最后活跃" min-width="170"><template #default="{ row }">{{ formatTime(row.lastSeenAt) }}</template></ui-table-column><ui-table-column label="操作" fixed="right" width="120" align="center"><template #default="{ row }"><div class="table-actions"><ui-button v-permission="'POST /api/v1/discovered-devices/:id/register'" type="primary" plain @click="register(row.deviceId)">一键注册</ui-button></div></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
      <template #empty><ui-empty description="当前没有未注册设备" /></template>
    </ui-table> <!-- 结束当前界面区域。 -->
    <div class="list-pagination"><ui-pagination v-model:current-page="unregisteredPage" v-model:page-size="unregisteredPageSize" :total="unregisteredTotal" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next, jumper" @current-change="changeUnregisteredPage" @size-change="changeUnregisteredPageSize" /></div> <!-- 渲染 div 界面元素。 -->
  </ui-card> <!-- 结束当前界面区域。 -->

  <ui-dialog v-model="dialog" :title="form.id ? '编辑设备' : '添加设备'" width="min(560px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!saving" :show-close="!saving"> <!-- 渲染 ui-dialog 界面元素。 -->
    <ui-form :model="form" label-position="top" :disabled="saving" @submit.prevent="save"> <!-- 渲染 ui-form 界面元素。 -->
      <ui-form-item label="所属产品" required> <!-- 渲染 ui-form-item 界面元素。 -->
        <ui-select v-model="form.productId" filterable placeholder="选择产品"><ui-option v-for="item in products" :key="item.id" :label="item.name" :value="item.id" /></ui-select> <!-- 渲染 ui-select 界面元素。 -->
        <ui-button v-permission="'menu:products'" v-if="!products.length" link @click="dialog=false;emit('navigate','products')">新建产品</ui-button> <!-- 渲染 ui-button 界面元素。 -->
      </ui-form-item> <!-- 结束当前界面区域。 -->
      <ui-form-item label="设备名称" required><ui-input v-model="form.name" maxlength="256" placeholder="例如 一层东侧烟感" /></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
      <ui-form-item label="设备标识"><ui-input v-model="form.code" :disabled="!!form.id" placeholder="与设备上报标识一致；留空自动生成" /></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
      <ui-form-item label="设备角色"><ui-radio-group v-model="form.deviceRole"><ui-radio-button v-for="(text, key) in deviceRoles" :key="key" :value="key">{{ text }}</ui-radio-button></ui-radio-group></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
      <ui-form-item v-if="form.deviceRole === 'CHILD'" label="所属网关" required><ui-select v-model="form.gatewayId" filterable><ui-option v-for="item in gateways" :key="item.device.id" :label="item.device.name" :value="item.device.id" /></ui-select></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
      <ui-collapse><ui-collapse-item title="更多设置" name="advanced"> <!-- 渲染 ui-collapse 界面元素。 -->
        <ui-form-item label="启用设备"><ui-switch v-model="form.status" active-value="ENABLED" inactive-value="DISABLED" /></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
        <ui-form-item label="标签（JSON）"><ui-input v-model="form.tags" type="textarea" :rows="3" /></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
        <ui-form-item label="备注"><ui-input v-model="form.description" type="textarea" :rows="2" /></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
      </ui-collapse-item></ui-collapse> <!-- 结束当前界面区域。 -->
    </ui-form> <!-- 结束当前界面区域。 -->
    <template #footer><ui-button :disabled="saving" @click="dialog = false">取消</ui-button><ui-button v-permission="['POST /api/v1/device-registry','PUT /api/v1/device-registry/:id']" type="primary" :loading="saving" @click="save">保存设备</ui-button></template>
  </ui-dialog> <!-- 结束当前界面区域。 -->

  <ui-dialog v-model="credentialDialog" title="设备凭证" @closed="credential={}" width="min(520px, 92vw)"><ui-alert title="密钥只显示这一次，请立即复制并安全保存。" type="warning" :closable="false" /><ui-descriptions class="top-gap" :column="1" border><ui-descriptions-item label="接入密钥"><code>{{ credential.accessKey }}</code></ui-descriptions-item><ui-descriptions-item label="设备密钥"><code class="break-all">{{ credential.secret }}</code></ui-descriptions-item></ui-descriptions><template #footer><ui-button @click="credentialDialog = false">关闭</ui-button><ui-button type="primary" @click="copyCredential">复制凭证</ui-button></template></ui-dialog> <!-- 渲染 ui-dialog 界面元素。 -->
</template>
