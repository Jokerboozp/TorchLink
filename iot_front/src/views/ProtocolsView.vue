<script setup>
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
const emit = defineEmits(['navigate']) /* 声明 emit。 */
import ProtocolAccessSettings from '../components/ProtocolAccessSettings.vue' /* 引入当前代码需要的依赖。 */
import ProtocolAssistantView from './ProtocolAssistantView.vue' /* 引入当前代码需要的依赖。 */
import FilePicker from '../components/FilePicker.vue' /* 引入当前代码需要的依赖。 */
import { transportLabel, statusLabel, platformLabel } from '../presentation' /* 引入当前代码需要的依赖。 */
import { computed, onMounted, reactive, ref, watch } from 'vue' /* 引入当前代码需要的依赖。 */
import { label, parsers } from '../labels' /* 引入当前代码需要的依赖。 */
import { UiMessage } from '../ui/feedback.js' /* 引入当前代码需要的依赖。 */
import { api, download, formatTime, notifyError, pretty } from '../api' /* 引入当前代码需要的依赖。 */
import { confirmDelete } from '../deleteAction'
import { Plus, RefreshCw, Upload, Wand2 } from '@lucide/vue'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'
import { runtimeStatusTones, tone } from '../labels'
import { can } from '../permissions' /* 根据当前账号权限决定版本详情中的可用操作。 */

const protocols = ref([]) /* 声明 protocols。 */
const protocolPage = ref(1), protocolPageSize = ref(20) /* 声明 protocolPage。 */
const pagedProtocols = computed(() => protocols.value.slice((protocolPage.value - 1) * protocolPageSize.value, protocolPage.value * protocolPageSize.value)) /* 声明 pagedProtocols。 */
watch(() => protocols.value.length, total => { protocolPage.value = Math.min(protocolPage.value, Math.max(1, Math.ceil(total / protocolPageSize.value))) }) /* 执行当前语句并推进处理流程。 */
const profiles = ref([]) /* 声明 profiles。 */
const snapshots = ref({}) /* 声明 snapshots。 */
const snapshot = (id) => snapshots.value[id] || { sessions: [], recentDevices: [] } /* 声明 snapshot。 */
const loading = ref(false) /* 声明 loading。 */
const testingId = ref('') /* 声明 testingId。 */
const result = ref(null) /* 声明 result。 */
const sourceFile = ref(null) /* 声明 sourceFile。 */
const targetPlatforms = ref([]) /* 声明 targetPlatforms。 */
const compiling = ref(false) /* 声明 compiling。 */
const sourceError = ref('') /* 声明 sourceError。 */
const sourceTemplate = ref(null) /* 声明 sourceTemplate。 */
const products = ref([]) /* 声明 products。 */
const switching = ref(false) /* 声明 switching。 */
const source = reactive({ protocolId:'', name:'', version:'', productId:'', transport:'', publish:true }) /* 声明 source。 */
const props = defineProps({ section: { type: String, default: 'protocols' } }) /* 声明 props。 */
const sourceOpen = ref(false), profileOpen = ref(false), assistantOpen = ref(false) /* 声明 sourceOpen。 */
const releaseOpen = ref(false), selectedProtocol = ref(null), selectedRelease = ref(null) /* 保存当前查看的协议及版本。 */
const versionsOpen = ref(false), managedProtocolId = ref('')
const managedProtocol = computed(() => protocols.value.find(item => item.definition.id === managedProtocolId.value) || null)
function manageVersions(row) { managedProtocolId.value = row.definition.id; versionsOpen.value = true }
function viewRelease(row, release) { versionsOpen.value = false; selectedProtocol.value = row.definition; selectedRelease.value = release; releaseOpen.value = true } /* 所有版本通过同一入口查看详情。 */
const hasReleaseActions = computed(() => { /* 仅在版本能力和账号权限都满足时显示专项操作。 */
  const release = selectedRelease.value /* 读取当前版本。 */
  return Boolean(release && ((release.artifact?.generatedMapping && can('POST /api/v2/protocols/:id/releases/:version/preview')) || (release.status === 'VALIDATED' && can('POST /api/v2/protocols/:id/releases/:version/publish')) || (release.artifact?.build?.kind === 'go-source' && can('GET /api/v2/protocols/:id/releases/:version/source')))) /* 返回可用操作状态。 */
}) /* 结束版本操作判断。 */
const assistantRelease = ref(null), assistantName = ref('') /* 声明 assistantRelease。 */
function openAssistant(release = null, name = '') { releaseOpen.value=false;assistantRelease.value=release;assistantName.value=name;assistantOpen.value=true } /* 从版本详情进入解析测试时关闭原弹窗。 */
function assistantNavigate(page) { assistantOpen.value=false;emit('navigate',page) } /* 定义 assistantNavigate 函数。 */
const editingProfile = ref(false) /* 声明 editingProfile。 */
const blankListener = () => ({ id:'', productId:'', protocolId:'', protocolVersion:'', mode:'listener', network:'tcp', host:'0.0.0.0', publicHost:'', port:26875, timeoutMs:5000, autoRegister:false, enabled:true, connectionMode:'listen', deviceId:'',queries:[],childProducts:[], unitId:1, intervalMs:10000, retries:0, wireFormat:'' }) /* 声明 blankListener。 */
const listener = reactive(blankListener()) /* 声明 listener。 */
function resetListener(value = {}) { for (const key of Object.keys(listener)) delete listener[key]; Object.assign(listener, blankListener(), value) } /* 定义 resetListener 函数。 */
function createProfile() { bindingRevision++; resetListener(); editingProfile.value=false; profileOpen.value=true } /* 定义 createProfile 函数。 */
watch(()=>listener.network,value=>{if(value!=='tcp'){listener.connectionMode='listen';listener.deviceId='';listener.queries=[]}}) /* 执行当前语句并推进处理流程。 */
watch(()=>listener.connectionMode,value=>{if(value==='dial')listener.publicHost=''})
function editProfile(profile){bindingRevision++;resetListener({...JSON.parse(JSON.stringify(profile)),mode:profile.mode || 'poll',network:profile.network || 'tcp',connectionMode:profile.mode === 'listener' ? profile.connectionMode || 'listen' : ''});editingProfile.value=true;profileOpen.value=true} /* 定义 editProfile 函数。 */
const savingListener = ref(false) /* 声明 savingListener。 */
const releaseCount = computed(() => protocols.value.reduce((total, item) => total + (item.releases?.length || 0), 0)) /* 声明 releaseCount。 */
let loadVersion = 0

async function loadProducts() { /* 定义 loadProducts 函数。 */
  const items = [] /* 声明 items。 */
  for (let page = 1; ; page += 1) { /* 循环处理当前数据。 */
    const result = await api(`/api/v1/products?page=${page}&pageSize=100`) /* 声明 result。 */
    items.push(...(result.items || [])) /* 执行当前语句并推进处理流程。 */
    if (!result.items?.length || items.length >= Number(result.total ?? result.count ?? items.length)) return items /* 判断条件并选择处理分支。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function load() { /* 定义 load 函数。 */
  const version = ++loadVersion
  loading.value = true /* 更新 loading.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const [catalog, access, productList, template, connectors] = await Promise.all([api('/api/v2/protocols'), props.section === 'profiles' ? api('/api/v2/device-access-profiles') : {}, loadProducts(), props.section === 'protocols' ? api('/api/v2/protocol-source-template') : null, props.section === 'profiles' ? api('/api/v1/connectors') : {}]) /* 执行当前语句并推进处理流程。 */
    if (version !== loadVersion) return
    protocols.value = catalog.items || [] /* 更新 protocols.value 的值。 */
    snapshots.value = Object.fromEntries((connectors.items || []).filter(x => x.profile).map(x => [x.profile.id, x])) /* 更新 snapshots.value 的值。 */
    profiles.value = (access.items || []).map(p => snapshot(p.id).profile || p) /* 更新 profiles.value 的值。 */
    products.value = productList /* 更新 products.value 的值。 */
    sourceTemplate.value = template /* 更新 sourceTemplate.value 的值。 */
  } catch (error) { if (version === loadVersion) notifyError(error) } finally { if (version === loadVersion) loading.value = false } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function chooseSourceFile(event) { sourceFile.value = event.target.files?.[0] || null; sourceError.value = '' } /* 定义 chooseSourceFile 函数。 */
async function downloadSourceTemplate(kind = '') { /* 定义 downloadSourceTemplate 函数。 */
  try { await download(`/api/v2/protocol-source-template?format=go-functions&kind=${kind}`, kind === 'tcp' ? 'go-tcp-protocol.zip' : 'go-protocol.zip') } /* 执行当前语句并推进处理流程。 */
  catch (error) { notifyError(error) } /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
async function uploadSource() { /* 定义 uploadSource 函数。 */
  if (!sourceFile.value || !source.protocolId) return UiMessage.warning('请选择 Go 源码并填写协议标识') /* 判断条件并选择处理分支。 */
  if (sourceFile.value.size > 32 * 1024 * 1024) return UiMessage.warning('源码文件不能超过 32 兆字节') /* 判断条件并选择处理分支。 */
  compiling.value = true /* 更新 compiling.value 的值。 */
  sourceError.value = '' /* 更新 sourceError.value 的值。 */
  result.value = null /* 更新 result.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const body = new FormData() /* 声明 body。 */
    body.append('file', sourceFile.value) /* 执行当前语句并推进处理流程。 */
    if (targetPlatforms.value.length) body.append('targetPlatforms', JSON.stringify(targetPlatforms.value)) /* 判断条件并选择处理分支。 */
    for (const [key, value] of Object.entries(source)) body.append(key, key === 'productId' && !source.publish ? '' : String(value)) /* 循环处理当前数据。 */
    result.value = await api(`/api/v2/protocols/${encodeURIComponent(source.protocolId)}/source-releases`, { method:'POST', body }) /* 更新 result.value 的值。 */
    UiMessage.success(result.value.binding ? '编译与样例测试通过，产品已切换到新版本' : source.publish ? '编译与样例测试通过，协议已发布，可绑定产品使用' : '编译与样例测试通过，已保存校验版本') /* 执行当前语句并推进处理流程。 */
    sourceOpen.value = false /* 更新 sourceOpen.value 的值。 */
    await load() /* 等待异步操作完成。 */
  } catch (error) { sourceError.value = error?.message || String(error) } /* 结束当前表达式或代码块。 */
  finally { compiling.value = false } /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
async function publishRelease(protocolId, version) { /* 定义 publishRelease 函数。 */
  switching.value = true /* 更新 switching.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    await api(`/api/v2/protocols/${encodeURIComponent(protocolId)}/releases/${encodeURIComponent(version)}/publish`, { method:'POST', body:'{}' }) /* 等待异步操作完成。 */
    UiMessage.success('版本已发布，可绑定产品使用') /* 执行当前语句并推进处理流程。 */
    releaseOpen.value = false /* 发布后关闭旧状态的版本详情。 */
    await load() /* 等待异步操作完成。 */
  } catch (error) { notifyError(error) } finally { switching.value = false } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
async function saveListener() { /* 定义 saveListener 函数。 */
  if (savingListener.value) return /* 判断条件并选择处理分支。 */
  if (!listener.id || !listener.productId || !listener.protocolId || !listener.protocolVersion) return UiMessage.warning('请填写网关标识、产品及其当前绑定的设备协议版本') /* 判断条件并选择处理分支。 */
  if (!editingProfile.value && profiles.value.some(p => p.id === listener.id)) return UiMessage.warning('网关标识已存在，请在列表中编辑') /* 判断条件并选择处理分支。 */
  savingListener.value = true /* 更新 savingListener.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    result.value = await api(editingProfile.value ? `/api/v2/device-access-profiles/${encodeURIComponent(listener.id)}` : '/api/v2/device-access-profiles', { method:editingProfile.value ? 'PUT' : 'POST', body:JSON.stringify(listener) }) /* 更新 result.value 的值。 */
    UiMessage.success('平台连接配置已保存')
    profileOpen.value = false /* 更新 profileOpen.value 的值。 */
    await load() /* 等待异步操作完成。 */
  } catch (error) { notifyError(error) } finally { savingListener.value = false } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
async function toggleProfile(profile) { /* 定义 toggleProfile 函数。 */
  try { /* 执行当前语句并推进处理流程。 */
    await api(`/api/v2/device-access-profiles/${encodeURIComponent(profile.id)}`, { method:'PUT', body:JSON.stringify({ ...profile, enabled:!profile.enabled }) }) /* 等待异步操作完成。 */
    await load() /* 等待异步操作完成。 */
  } catch (error) { notifyError(error) } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
async function downloadSourceRelease(protocolId, release) { /* 定义 downloadSourceRelease 函数。 */
  try { await download(`/api/v2/protocols/${encodeURIComponent(protocolId)}/releases/${encodeURIComponent(release.version)}/source`, `${protocolId}-${release.version}-source.${release.artifact?.filename?.toLowerCase().endsWith('.go') ? 'go' : 'zip'}`) } /* 执行当前语句并推进处理流程。 */
  catch (error) { notifyError(error) } /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
async function testProfile(profile) { /* 定义 testProfile 函数。 */
  testingId.value = profile.id /* 更新 testingId.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    result.value = await api(`/api/v2/device-access-profiles/${encodeURIComponent(profile.id)}/test`, { method:'POST', body:'{}' }) /* 更新 result.value 的值。 */
    UiMessage.success('连接与单次采集测试通过') /* 执行当前语句并推进处理流程。 */
    await load() /* 等待异步操作完成。 */
  } catch (error) { notifyError(error) } finally { testingId.value = '' } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

let bindingRevision = 0 /* 声明 bindingRevision。 */
async function selectProduct(id) { /* 定义 selectProduct 函数。 */
  const revision = ++bindingRevision /* 声明 revision。 */
  listener.protocolId = ''; listener.protocolVersion = '' /* 更新 listener.protocolId 的值。 */
  if (!id) return /* 判断条件并选择处理分支。 */
  try { /* 执行当前语句并推进处理流程。 */
    const current = await api(`/api/v2/products/${encodeURIComponent(id)}/protocol-binding`) /* 声明 current。 */
    if (revision !== bindingRevision) return /* 判断条件并选择处理分支。 */
    listener.protocolId = current.protocolId || ''; listener.protocolVersion = current.version || '' /* 更新 listener.protocolId 的值。 */
  } catch (error) { if (revision === bindingRevision) notifyError(error) } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
function newestRelease(item) { return item.releases?.[0] || {} } /* 定义 newestRelease 函数。 */
function statusText(value) { return ({ LISTENING:'监听中', DISABLED:'已停用', DRAFT:'草稿', VALIDATED:'已校验', PUBLISHED:'已发布', DEPRECATED:'已弃用', REVOKED:'已撤销', PENDING:'待启动', ONLINE:'在线采集', ERROR:'采集异常' })[value] || statusLabel(value) } /* 定义 statusText 函数。 */
function statusType(value) { return ({ PUBLISHED:'success', ONLINE:'success', ERROR:'danger', REVOKED:'danger', VALIDATED:'warning', PENDING:'info' })[value] || 'info' } /* 定义 statusType 函数。 */

onMounted(load) /* 执行当前语句并推进处理流程。 */
function removeProtocol(row) { return confirmDelete({ label:row.definition.name || row.definition.id, path:`/api/v2/protocols/${encodeURIComponent(row.definition.id)}`, onDeleted:load, warning:'未被引用的版本将一并删除，删除后无法恢复。', blockedHint:'协议仍被产品或平台连接配置引用，请先解除绑定。' }) }
function removeRelease(row, release) { return confirmDelete({ label:`${row.definition.name || row.definition.id} · ${release.version}`, path:`/api/v2/protocols/${encodeURIComponent(row.definition.id)}/releases/${encodeURIComponent(release.version)}`, onDeleted:load, warning:'仅删除此版本及其独有制品，删除后无法恢复。', blockedHint:'此版本仍被设备模板、回滚记录或平台连接配置引用，请先切换关联版本。' }) }
function protocolActions(row) {
  return [
    { key:'versions', label:'管理版本', onClick:() => manageVersions(row) },
    { key:'delete', label:'删除协议', type:'danger', permission:'DELETE /api/v2/protocols/:id', onClick:() => removeProtocol(row) }
  ]
}
function profileActions(row) {
  return [
    { key:'edit', label:'编辑', permission:'PUT /api/v2/device-access-profiles/:id', onClick:() => editProfile(row) },
    { key:'test', label:'连接测试', permission:'POST /api/v2/device-access-profiles/:id/test', hidden:row.mode === 'listener', loading:testingId.value === row.id, onClick:() => testProfile(row) },
    { key:'toggle', label:row.enabled ? '停用' : '启用', permission:'PUT /api/v2/device-access-profiles/:id', onClick:() => toggleProfile(row) },
    { key:'delete', label:'删除', type:'danger', permission:'DELETE /api/v2/device-access-profiles/:id', onClick:() => removeProfile(row) }
  ]
}
function removeProfile(row) { return confirmDelete({ label:row.id, path:`/api/v2/device-access-profiles/${encodeURIComponent(row.id)}`, onDeleted:load, blockedHint:'请先停用平台连接配置，并解除关联设备后再删除。' }) }
</script>

<template>
  <FilterBar>
    <template #actions>
      <ui-button :loading="loading" @click="load"><RefreshCw />刷新</ui-button>
      <template v-if="props.section === 'protocols'">
        <ui-button v-permission="'POST /api/v1/ai/protocol-assistant/generate'" title="通过报文或 Excel / CSV 点表生成协议" @click="openAssistant()"><Wand2 />协议生成</ui-button>
        <ui-button v-permission="'POST /api/v2/protocols/:id/source-releases'" type="primary" @click="sourceOpen=true"><Upload />上传源码</ui-button>
      </template>
      <ui-button v-else v-permission="'POST /api/v2/device-access-profiles'" type="primary" @click="createProfile"><Plus />新建平台连接配置</ui-button>
    </template>
  </FilterBar>
  <DataTableCard v-if="props.section === 'protocols'" :title="`设备通信协议 · ${protocols.length} 个协议 · ${releaseCount} 个版本`" :page="protocolPage" :page-size="protocolPageSize" :page-sizes="[10,20,50,100]" :total="protocols.length" @update:page="value => protocolPage = value" @update:page-size="value => { protocolPageSize = value; protocolPage = 1 }">
    <ui-table :data="pagedProtocols" :loading="loading" empty-text="暂无协议，可上传 Go 源码或用报文、点表生成">
      <ui-table-column label="协议" min-width="230"><template #default="{ row }"><b>{{ row.definition.name }}</b><small class="subline">{{ row.definition.id }} · {{ row.definition.vendor || '通用' }}</small></template></ui-table-column>
      <ui-table-column label="最新版本" width="130"><template #default="{ row }">{{ newestRelease(row).version || '—' }}</template></ui-table-column>
      <ui-table-column label="运行方式" min-width="200"><template #default="{ row }">{{ transportLabel(newestRelease(row).transport) }} · {{ label(parsers, newestRelease(row).parserType, '自定义协议程序') }}</template></ui-table-column>
      <ui-table-column label="状态" width="110"><template #default="{ row }"><StatusDot :tone="statusType(newestRelease(row).status) === 'success' ? 'success' : statusType(newestRelease(row).status) === 'danger' ? 'danger' : statusType(newestRelease(row).status) === 'warning' ? 'warning' : 'neutral'" :label="statusText(newestRelease(row).status)" /></template></ui-table-column>
      <ui-table-column label="版本数量" width="100"><template #default="{ row }">{{ row.releases?.length || 0 }}</template></ui-table-column>
      <ui-table-column label="操作" fixed="right" width="176" align="right"><template #default="{ row }"><RowActions :actions="protocolActions(row)" /></template></ui-table-column>
    </ui-table>
  </DataTableCard>
  <DataTableCard v-else :title="`平台连接配置 · ${profiles.length} 个`">
    <ui-table :data="profiles" :loading="loading" row-key="id" empty-text="暂无平台连接配置。TCP / UDP 监听或 Modbus 采集设备需要先建立连接配置">
      <ui-table-column type="expand"><template #default="{row}">
        <div class="instance-details">
          <h4>当前在线会话</h4>
          <ui-table :data="snapshot(row.id).sessions" empty-text="暂无在线会话"><ui-table-column label="设备"><template #default="{row:session}">{{session.deviceId || '尚未识别设备'}}</template></ui-table-column><ui-table-column prop="remoteAddress" label="远端地址"/><ui-table-column prop="protocolId" label="协议"/><ui-table-column prop="protocolVersion" label="版本"/><ui-table-column label="最后有效报文"><template #default="{row:session}">{{formatTime(session.lastSeenAt)}}</template></ui-table-column></ui-table>
          <h4>最近接入设备（按创建时间，最多 20 台）</h4>
          <ui-table :data="snapshot(row.id).recentDevices" empty-text="暂无关联设备"><ui-table-column prop="deviceId" label="设备标识"/><ui-table-column prop="name" label="名称"/><ui-table-column label="创建时间"><template #default="{row:device}">{{formatTime(device.createdAt)}}</template></ui-table-column></ui-table>
        </div>
      </template></ui-table-column>
      <ui-table-column label="平台连接配置" min-width="190"><template #default="{ row }"><b>{{ row.id }}</b><small v-if="row.deviceId" class="subline">目标设备：{{ row.deviceId }}</small></template></ui-table-column>
      <ui-table-column label="设备模板" min-width="170"><template #default="{ row }">{{ products.find(p => p.id === row.productId)?.name || row.productId }}</template></ui-table-column>
      <ui-table-column label="协议版本" min-width="190"><template #default="{ row }">{{ row.protocolId }}@{{ row.protocolVersion }}</template></ui-table-column>
      <ui-table-column label="地址 / 端口" min-width="210"><template #default="{ row }">{{ row.mode === 'listener' && row.connectionMode !== 'dial' && row.publicHost ? row.publicHost : row.host }}:{{ row.port }}<small class="subline">{{ row.mode === 'listener' ? `${transportLabel(row.network)} · ${row.connectionMode === 'dial' ? '平台连接设备' : '设备连接平台'}` : `Modbus 采集 · 站号 ${row.unitId}` }}</small></template></ui-table-column>
      <ui-table-column label="在线会话" width="90"><template #default="{row}">{{snapshot(row.id).sessions?.length || 0}}</template></ui-table-column>
      <ui-table-column label="状态" width="110"><template #default="{ row }"><StatusDot :tone="tone(runtimeStatusTones, row.runtimeStatus)" :label="statusText(row.runtimeStatus)" /></template></ui-table-column>
      <ui-table-column label="最近成功" min-width="160"><template #default="{ row }">{{ formatTime(row.lastSuccessAt) }}</template></ui-table-column>
      <ui-table-column label="最近错误" min-width="200" show-overflow-tooltip><template #default="{ row }">{{ row.lastError || '—' }}</template></ui-table-column>
      <ui-table-column label="操作" width="176" fixed="right" align="right"><template #default="{ row }"><RowActions :actions="profileActions(row)" /></template></ui-table-column>
    </ui-table>
  </DataTableCard>
  <ui-dialog v-model="versionsOpen" class="protocol-versions-dialog" :title="`${managedProtocol?.definition.name || '协议'} · 版本管理`" width="min(880px, 96vw)" destroy-on-close>
    <template v-if="managedProtocol">
      <p class="versions-summary">{{ managedProtocol.definition.id }} · 共 {{ managedProtocol.releases?.length || 0 }} 个版本。删除前请确认该版本未被设备模板或平台连接配置引用。</p>
      <ui-table :data="managedProtocol.releases || []" stripe empty-text="暂无版本，可上传源码创建新版本">
        <ui-table-column label="版本" min-width="130"><template #default="{ row }"><strong>{{ row.version }}</strong></template></ui-table-column>
        <ui-table-column label="状态" width="110"><template #default="{ row }"><ui-tag :type="statusType(row.status)" round>{{ statusText(row.status) }}</ui-tag></template></ui-table-column>
        <ui-table-column label="运行方式" min-width="185"><template #default="{ row }">{{ transportLabel(row.transport) }} · {{ label(parsers, row.parserType, '自定义协议程序') }}</template></ui-table-column>
        <ui-table-column label="创建时间" min-width="170"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></ui-table-column>
        <ui-table-column label="操作" width="160" fixed="right"><template #default="{ row }"><div class="table-actions"><ui-button size="small" plain @click="viewRelease(managedProtocol, row)">详情</ui-button><ui-button v-permission="'DELETE /api/v2/protocols/:id/releases/:version'" size="small" plain type="danger" @click="removeRelease(managedProtocol, row)">删除</ui-button></div></template></ui-table-column>
      </ui-table>
    </template>
  </ui-dialog>
  <ui-dialog v-model="releaseOpen" title="协议版本" width="min(620px, 94vw)" destroy-on-close> <!-- 集中展示版本信息和该版本支持的操作。 -->
    <template v-if="selectedProtocol && selectedRelease"> <!-- 仅在选中实际版本后渲染详情。 -->
      <ui-descriptions :column="1" border> <!-- 说明当前查看的是哪个协议版本。 -->
        <ui-descriptions-item label="协议">{{ selectedProtocol.name }} · {{ selectedProtocol.id }}</ui-descriptions-item> <!-- 展示协议名称和标识。 -->
        <ui-descriptions-item label="版本">{{ selectedRelease.version }}</ui-descriptions-item> <!-- 展示版本号。 -->
        <ui-descriptions-item label="状态"><ui-tag :type="statusType(selectedRelease.status)" round>{{ statusText(selectedRelease.status) }}</ui-tag></ui-descriptions-item> <!-- 展示版本状态。 -->
        <ui-descriptions-item label="运行方式">{{ transportLabel(selectedRelease.transport) }} · {{ label(parsers, selectedRelease.parserType, '自定义协议程序') }}</ui-descriptions-item> <!-- 展示通信和解析方式。 -->
      </ui-descriptions> <!-- 结束版本信息。 -->
      <div class="release-buttons release-detail-actions"> <!-- 根据制品类型及权限展示适用操作。 -->
        <ui-button v-if="selectedRelease.artifact?.generatedMapping" v-permission="'POST /api/v2/protocols/:id/releases/:version/preview'" plain type="primary" @click="openAssistant(selectedRelease, selectedProtocol.name)">解析测试</ui-button> <!-- 仅生成映射支持解析预览。 -->
        <ui-button v-if="selectedRelease.status === 'VALIDATED'" v-permission="'POST /api/v2/protocols/:id/releases/:version/publish'" plain type="primary" :loading="switching" @click="publishRelease(selectedProtocol.id, selectedRelease.version)">发布</ui-button> <!-- 仅已校验版本可以发布。 -->
        <ui-button v-if="selectedRelease.artifact?.build?.kind === 'go-source'" v-permission="'GET /api/v2/protocols/:id/releases/:version/source'" plain @click="downloadSourceRelease(selectedProtocol.id, selectedRelease)">源码</ui-button> <!-- 仅 Go 源码版本可以下载源码。 -->
        <span v-if="!hasReleaseActions" class="muted-text">暂无可执行操作</span> <!-- 对无适用操作的版本说明原因。 -->
      </div> <!-- 结束专项操作区域。 -->
    </template> <!-- 结束版本详情。 -->
  </ui-dialog> <!-- 结束协议版本弹窗。 -->
  <ui-dialog v-model="assistantOpen" title="生成协议" width="min(980px, 94vw)" :close-on-click-modal="false" destroy-on-close><p v-if="!assistantRelease" class="muted-text bottom-gap">通过报文或 Excel / CSV 点表生成协议</p><ProtocolAssistantView v-if="assistantOpen" :initial-release="assistantRelease" :initial-name="assistantName" @saved="load" @navigate="assistantNavigate" /></ui-dialog>
  <ui-dialog v-model="sourceOpen" class="source-upload-dialog" title="上传协议源码" width="min(760px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!compiling" :show-close="!compiling">
      <ui-alert v-if="sourceTemplate && !sourceTemplate.compilerAvailable" class="top-gap" title="当前服务缺少源码编译环境，请联系管理员部署支持编译的后端服务。" type="warning" :closable="false" />
      <ui-form :disabled="compiling" :model="source" label-position="top" class="top-gap">
        <div class="form-grid">
          <ui-form-item label="协议标识"><ui-input v-model="source.protocolId" placeholder="例如 vendor-fire" /></ui-form-item>
          <ui-form-item label="协议名称"><ui-input v-model="source.name" placeholder="例如消防设备协议" /></ui-form-item>
          <ui-form-item label="版本"><ui-input v-model="source.version" placeholder="Go 函数模式留空自动生成新版本" /></ui-form-item>
          <ui-form-item label="绑定产品（可选）"><ui-select v-model="source.productId" filterable clearable :disabled="!source.publish" placeholder="选择后，发布成功立即切换"><ui-option v-for="p in products" :key="p.id" :label="`${p.name} · ${p.id}`" :value="p.id" /></ui-select></ui-form-item>
          <ui-form-item label="设备上报通道"><ui-select v-model="source.transport" clearable placeholder="自动识别，可手动选择"><ui-option v-for="value in ['MQTT','HTTP','TCP','UDP','TCP_UDP']" :key="value" :label="transportLabel(value)" :value="value" /></ui-select></ui-form-item>
        </div>
        <ui-form-item label="源码文件或项目压缩包" class="source-file-item">
          <div class="source-file-field">
            <FilePicker accept=".go,.zip" :disabled="compiling" @change="chooseSourceFile" />
            <small>上传 .go 文件或完整 Go 项目 ZIP，文件不能超过 32 MB。</small>
          </div>
        </ui-form-item>
        <div class="source-template-panel">
          <div><strong>还没有源码？</strong><small>解析模板适合单纯解析上报；TCP / UDP 模板还包含分帧、应答和命令编码示例。</small></div>
          <div class="source-template-actions">
            <ui-button plain :disabled="!sourceTemplate || compiling" @click="downloadSourceTemplate()">下载解析模板</ui-button>
            <ui-button plain :disabled="!sourceTemplate || compiling" @click="downloadSourceTemplate('tcp')">下载 TCP / UDP 模板</ui-button>
          </div>
        </div>
        <ui-collapse class="source-compile-options">
          <ui-collapse-item title="编译选项" name="advanced">
            <ui-form-item label="额外编译目标（可选）" class="source-target-item">
              <ui-select v-model="targetPlatforms" multiple clearable :to="true" :disabled="compiling" placeholder="默认仅构建发布端平台"><ui-option v-for="platform in (sourceTemplate?.targetPlatforms || [])" :key="platform" :label="platformLabel(platform)" :value="platform" /></ui-select> <!-- 选项弹层挂到页面根部，避免被源码弹窗边界裁切。 -->
            </ui-form-item>
            <small class="source-target-help">发布端会编译并运行样例；额外目标仅生成编译制品，所有所选目标都须编译成功，实际试跑后才能确认适配。</small>
          </ui-collapse-item>
        </ui-collapse>
        <div class="source-publish-panel">
          <div><strong>校验通过后</strong><small>{{ source.publish ? '自动发布新版本；若已绑定产品，立即切换到新版本。' : '仅保存已校验版本，稍后可在协议版本中发布。' }}</small></div>
          <ui-switch v-model="source.publish" active-text="立即发布" inactive-text="暂不发布" />
        </div>
        <small v-if="compiling" class="source-compiling-help">首次编译可能较慢，请保持页面打开。每个平台编译最长 120 秒，随后运行样例测试。</small>
        <ui-alert v-if="sourceError" class="top-gap" title="操作未完成，请查看原因" type="error" :closable="false"><pre class="source-error">{{ sourceError }}</pre></ui-alert>
      </ui-form>
      <template #footer><div class="source-submit-row"><span>提交后先编译并试跑样例，全部通过才会按上方设置保存或发布。</span><ui-button v-permission="'POST /api/v2/protocols/:id/source-releases'" type="primary" :loading="compiling" :disabled="sourceTemplate && !sourceTemplate.compilerAvailable" @click="uploadSource">{{ compiling ? '正在编译并试跑样例…' : source.publish ? '上传、编译并发布' : '上传、编译并校验' }}</ui-button></div></template>
  </ui-dialog>
  <ui-dialog v-model="profileOpen" :title="editingProfile ? '编辑平台连接配置' : '新建平台连接配置'" width="min(820px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!savingListener" :show-close="!savingListener" class="profile-editor-dialog" destroy-on-close>
    <div class="profile-editor-layout">
      <p class="profile-editor-intro">配置平台如何连接现场设备。共享监听可供多台设备使用；具体设备在设备管理中登记。</p>
      <ui-form :disabled="savingListener" label-position="top">
        <section class="profile-editor-section">
          <div class="profile-section-heading"><span>01</span><div><h3>适用设备</h3><p>选择设备模板后，自动使用模板绑定的协议与版本。</p></div></div>
          <div class="profile-field-grid">
            <ui-form-item label="连接名称 / 标识" required><ui-input v-model="listener.id" :disabled="editingProfile" placeholder="例如 dahua-tcp" /></ui-form-item>
            <ui-form-item label="关联设备模板" required><ui-select v-model="listener.productId" filterable placeholder="选择需要接入的设备模板" @change="selectProduct"><ui-option v-for="p in products" :key="p.id" :label="p.name" :value="p.id" /></ui-select></ui-form-item>
          </div>
          <div class="profile-protocol-summary"><div><span>模板绑定协议</span><strong>{{ protocols.find(p => p.definition.id === listener.protocolId)?.definition.name || listener.protocolId || '选择模板后自动读取' }}</strong></div><div><span>模板当前绑定版本</span><strong>{{ listener.protocolVersion || '请先在设备模板绑定已发布协议' }}</strong></div></div>
        </section>

        <section class="profile-editor-section">
          <div class="profile-section-heading"><span>02</span><div><h3>连接方式</h3><p>确定由设备连接平台，还是由平台主动连接设备。</p></div></div>
          <div class="profile-field-grid" v-if="listener.mode==='listener'">
            <ui-form-item label="网络协议"><ui-select v-model="listener.network"><ui-option label="TCP" value="tcp" /><ui-option label="UDP" value="udp" /></ui-select></ui-form-item>
            <ui-form-item v-if="listener.network==='tcp'" label="连接方向"><ui-select v-model="listener.connectionMode"><ui-option value="listen" label="设备连接平台"/><ui-option value="dial" label="平台连接设备"/></ui-select></ui-form-item>
          </div>
          <div class="profile-mode-note">{{ listener.mode==='poll' ? '平台定时连接并采集设备数据。' : listener.connectionMode==='dial' ? '平台主动连接指定设备，需填写设备可达地址。' : listener.network==='udp' ? '设备向平台监听端口发送 UDP 报文。' : '设备主动连接平台监听端口，可由多台设备共享。' }}</div>
        </section>

        <section class="profile-editor-section">
          <div class="profile-section-heading"><span>03</span><div><h3>地址与端口</h3><p>{{ listener.connectionMode==='dial' || listener.mode==='poll' ? '填写目标设备在平台侧可达的地址。' : '区分平台本机监听地址和现场设备实际填写的对外地址。' }}</p></div></div>
          <div class="profile-field-grid">
            <ui-form-item v-if="listener.connectionMode==='dial' || listener.mode==='poll'" label="目标设备标识"><ui-input v-model="listener.deviceId" placeholder="填写已登记设备的标识" /></ui-form-item>
            <ui-form-item :label="listener.connectionMode==='dial' || listener.mode==='poll'?'设备地址 / 主机名':'本机监听地址'"><ui-input v-model="listener.host" :placeholder="listener.connectionMode==='dial' || listener.mode==='poll'?'设备可达域名或 IP':'例如 0.0.0.0'" /></ui-form-item>
            <ui-form-item v-if="listener.mode==='listener' && listener.connectionMode!=='dial'" label="平台对外地址"><ui-input v-model="listener.publicHost" placeholder="现场设备可达域名或 IP" /></ui-form-item>
            <ui-form-item label="端口"><ui-input-number v-model="listener.port" :min="1" :max="65535" /></ui-form-item>
          </div>
          <p v-if="listener.mode==='listener' && listener.connectionMode!=='dial'" class="profile-address-help">本机监听地址可用 0.0.0.0；现场设备须填写平台对外地址和端口，不能使用 0.0.0.0。</p>
        </section>

        <section class="profile-editor-section">
          <div class="profile-section-heading"><span>04</span><div><h3>运行设置</h3><p>设置超时、自动登记及启用状态。</p></div></div>
          <div class="profile-field-grid">
            <ui-form-item label="操作超时（秒）"><ui-input-number :model-value="listener.timeoutMs/1000" :min="0.001" :max="30" :step="0.5" @update:model-value="value=>listener.timeoutMs=Math.round(Number(value)*1000)" /></ui-form-item>
            <template v-if="listener.mode==='poll'">
              <ui-form-item label="站号"><ui-input-number v-model="listener.unitId" :min="0" :max="255" /></ui-form-item>
              <ui-form-item label="采集周期（秒）"><ui-input-number :model-value="listener.intervalMs/1000" :min="1" @update:model-value="value=>listener.intervalMs=Math.round(Number(value)*1000)" /></ui-form-item>
              <ui-form-item label="重试次数"><ui-input-number v-model="listener.retries" :min="0" :max="3" /></ui-form-item>
            </template>
          </div>
          <div class="profile-toggle-list">
            <div v-if="listener.mode==='listener'" class="profile-toggle-row"><div><strong>自动登记新设备</strong><small>协议识别出新设备后，自动加入设备列表。</small></div><ui-switch v-model="listener.autoRegister" /></div>
            <div class="profile-toggle-row"><div><strong>启用连接配置</strong><small>保存后按此状态运行监听或采集服务。</small></div><ui-switch v-model="listener.enabled" /></div>
          </div>
          <ui-collapse v-if="listener.mode==='listener'" class="profile-advanced"><ui-collapse-item :title="listener.network==='tcp' ? '定时读取与子设备（可选）' : '子设备映射（可选）'" name="advanced"><ProtocolAccessSettings :profile="listener" :can-poll="listener.network==='tcp'" :products="products" :product-id="listener.productId"/></ui-collapse-item></ui-collapse>
        </section>
      </ui-form>
    </div>
    <template #footer><div class="profile-editor-footer"><ui-button :disabled="savingListener" @click="profileOpen=false">取消</ui-button><ui-button v-permission="['POST /api/v2/device-access-profiles','PUT /api/v2/device-access-profiles/:id']" type="primary" :loading="savingListener" @click="saveListener">保存平台连接配置</ui-button></div></template>
  </ui-dialog>
  <details v-if="result" class="technical-details"><summary>最近操作结果</summary><pre>{{ pretty(result) }}</pre></details>
</template>
<style scoped>
.instance-details { display: grid; gap: var(--space-3); padding: var(--space-4) var(--space-6); }
.instance-details h4 { margin: 0; color: var(--text-strong); font-size: var(--font-size-sm); font-weight: var(--font-weight-semibold); }
.versions-summary { margin: 0 0 var(--space-3); color: var(--text-muted); font-size: var(--font-size-sm); }
.release-buttons { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); margin-top: var(--space-4); }
.source-error { max-height: 300px; overflow: auto; overflow-wrap: anywhere; white-space: pre-wrap; }
.source-file-field { display: grid; gap: var(--space-2); width: 100%; min-width: 0; }
.source-file-field small,
.source-template-panel small,
.source-publish-panel small,
.source-target-help,
.source-compiling-help { display: block; color: var(--text-muted); font-size: var(--font-size-xs); line-height: 1.5; }
.source-template-panel,
.source-publish-panel { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: var(--space-3) var(--space-5); padding: var(--space-3) var(--space-4); background: var(--surface-muted); border: 1px solid var(--border); border-radius: var(--radius-lg); }
.source-template-panel { margin: 2px 0 var(--space-4); }
.source-template-panel strong,
.source-publish-panel strong { display: block; margin-bottom: 2px; color: var(--text-strong); font-size: var(--font-size-sm); }
.source-template-actions { display: flex; flex-wrap: wrap; gap: var(--space-2); }
.source-compile-options { margin-bottom: var(--space-4); }
.source-target-item { margin: var(--space-2) 0 var(--space-1); }
.source-target-help { margin-bottom: var(--space-3); }
.source-compiling-help { margin-top: var(--space-3); }
.source-submit-row { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); width: 100%; }
.source-submit-row span { color: var(--text-muted); font-size: var(--font-size-xs); }
.source-submit-row button { flex: none; }
.profile-editor-intro { margin: 0 0 var(--space-4); color: var(--text-muted); font-size: var(--font-size-sm); }
.profile-editor-section + .profile-editor-section { margin-top: var(--space-4); padding-top: var(--space-4); border-top: 1px solid var(--border); }
.profile-section-heading { display: flex; align-items: flex-start; gap: var(--space-3); margin-bottom: var(--space-3); }
.profile-section-heading > span { display: grid; flex: none; place-items: center; width: 24px; height: 24px; color: var(--primary); background: var(--primary-soft); border-radius: var(--radius-md); font-size: var(--font-size-xs); font-weight: var(--font-weight-semibold); }
.profile-section-heading h3 { margin: 0; color: var(--text-strong); font-size: var(--font-size-md); font-weight: var(--font-weight-semibold); }
.profile-section-heading p { margin: 2px 0 0; color: var(--text-muted); font-size: var(--font-size-xs); }
.profile-field-grid,
.profile-protocol-summary { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--space-3) var(--space-4); }
.profile-field-grid > *,
.profile-protocol-summary > * { min-width: 0; }
.profile-field-grid :deep(.n-form-item) { margin-bottom: 0; }
.profile-field-grid :deep(.ui-input),
.profile-field-grid :deep(.ui-select),
.profile-field-grid :deep(.ui-input-number) { width: 100%; }
.profile-protocol-summary { margin-top: var(--space-3); padding: var(--space-3) var(--space-4); background: var(--surface-muted); border: 1px solid var(--border); border-radius: var(--radius-lg); }
.profile-protocol-summary span,
.profile-protocol-summary strong { display: block; overflow-wrap: anywhere; }
.profile-protocol-summary span { margin-bottom: 2px; color: var(--text-muted); font-size: var(--font-size-xs); }
.profile-protocol-summary strong { color: var(--text-strong); font-size: var(--font-size-sm); font-weight: var(--font-weight-semibold); }
.profile-mode-note,
.profile-address-help { margin: var(--space-3) 0 0; padding: var(--space-2) var(--space-3); color: var(--info-text); background: var(--info-soft); border-radius: var(--radius-md); font-size: var(--font-size-xs); }
.profile-toggle-list { display: grid; gap: var(--space-2); margin-top: var(--space-4); }
.profile-toggle-row { display: flex; align-items: center; justify-content: space-between; gap: var(--space-4); padding: var(--space-3); border: 1px solid var(--border); border-radius: var(--radius-lg); }
.profile-toggle-row strong,
.profile-toggle-row small { display: block; }
.profile-toggle-row strong { color: var(--text-strong); font-size: var(--font-size-sm); }
.profile-toggle-row small { margin-top: 2px; color: var(--text-muted); font-size: var(--font-size-xs); }
.profile-toggle-row :deep(.ui-switch-field) { flex: none; }
.profile-advanced { margin-top: var(--space-4); border-top: 1px solid var(--border); }
.profile-editor-footer { display: flex; justify-content: flex-end; gap: var(--space-2); width: 100%; }
@media (max-width: 767px) {
  .source-template-actions,
  .source-template-actions button,
  .source-submit-row,
  .source-submit-row button { width: 100%; }
  .source-submit-row { flex-wrap: wrap; }
  .profile-field-grid,
  .profile-protocol-summary { grid-template-columns: 1fr; }
}
</style>
