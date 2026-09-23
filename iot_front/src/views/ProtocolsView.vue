<script setup>
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
const emit = defineEmits(['navigate']) /* 声明 emit。 */
import ProtocolAccessSettings from '../components/ProtocolAccessSettings.vue' /* 引入当前代码需要的依赖。 */
import ProtocolAssistantView from './ProtocolAssistantView.vue' /* 引入当前代码需要的依赖。 */
import FilePicker from '../components/FilePicker.vue' /* 引入当前代码需要的依赖。 */
import { transportLabel, formatLabel, statusLabel, platformLabel } from '../presentation' /* 引入当前代码需要的依赖。 */
import { computed, onMounted, reactive, ref, watch } from 'vue' /* 引入当前代码需要的依赖。 */
import { label, parsers } from '../labels' /* 引入当前代码需要的依赖。 */
import { UiMessage } from '../ui/feedback.js' /* 引入当前代码需要的依赖。 */
import { api, download, formatTime, notifyError, pretty } from '../api' /* 引入当前代码需要的依赖。 */
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
function viewRelease(row, release) { selectedProtocol.value = row.definition; selectedRelease.value = release; releaseOpen.value = true } /* 所有版本通过同一入口查看详情。 */
const hasReleaseActions = computed(() => { /* 仅在版本能力和账号权限都满足时显示专项操作。 */
  const release = selectedRelease.value /* 读取当前版本。 */
  return Boolean(release && ((release.artifact?.generatedMapping && can('POST /api/v2/protocols/:id/releases/:version/preview')) || (release.status === 'VALIDATED' && can('POST /api/v2/protocols/:id/releases/:version/publish')) || (release.artifact?.build?.kind === 'go-source' && can('GET /api/v2/protocols/:id/releases/:version/source')))) /* 返回可用操作状态。 */
}) /* 结束版本操作判断。 */
const assistantRelease = ref(null), assistantName = ref('') /* 声明 assistantRelease。 */
function openAssistant(release = null, name = '') { releaseOpen.value=false;assistantRelease.value=release;assistantName.value=name;assistantOpen.value=true } /* 从版本详情进入解析测试时关闭原弹窗。 */
function assistantNavigate(page) { assistantOpen.value=false;emit('navigate',page) } /* 定义 assistantNavigate 函数。 */
const editingProfile = ref(false) /* 声明 editingProfile。 */
const blankListener = () => ({ id:'', productId:'', protocolId:'', protocolVersion:'', mode:'listener', network:'tcp', host:'0.0.0.0', port:26875, timeoutMs:5000, autoRegister:false, enabled:true, connectionMode:'listen', deviceId:'',queries:[],childProducts:[], unitId:1, intervalMs:10000, retries:0, wireFormat:'' }) /* 声明 blankListener。 */
const listener = reactive(blankListener()) /* 声明 listener。 */
function resetListener(value = {}) { for (const key of Object.keys(listener)) delete listener[key]; Object.assign(listener, blankListener(), value) } /* 定义 resetListener 函数。 */
function createProfile() { bindingRevision++; resetListener(); editingProfile.value=false; profileOpen.value=true } /* 定义 createProfile 函数。 */
watch(()=>listener.network,value=>{if(value!=='tcp'){listener.connectionMode='listen';listener.deviceId='';listener.queries=[]}}) /* 执行当前语句并推进处理流程。 */
function editProfile(profile){bindingRevision++;resetListener({...JSON.parse(JSON.stringify(profile)),mode:profile.mode || 'poll',network:profile.network || 'tcp',connectionMode:profile.mode === 'listener' ? profile.connectionMode || 'listen' : ''});editingProfile.value=true;profileOpen.value=true} /* 定义 editProfile 函数。 */
const savingListener = ref(false) /* 声明 savingListener。 */
const releaseCount = computed(() => protocols.value.reduce((total, item) => total + (item.releases?.length || 0), 0)) /* 声明 releaseCount。 */

async function loadProducts() { /* 定义 loadProducts 函数。 */
  const items = [] /* 声明 items。 */
  for (let page = 1; ; page += 1) { /* 循环处理当前数据。 */
    const result = await api(`/api/v1/products?page=${page}&pageSize=100`) /* 声明 result。 */
    items.push(...(result.items || [])) /* 执行当前语句并推进处理流程。 */
    if (!result.items?.length || items.length >= Number(result.total ?? result.count ?? items.length)) return items /* 判断条件并选择处理分支。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function load() { /* 定义 load 函数。 */
  loading.value = true /* 更新 loading.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const [catalog, access, productList, template, connectors] = await Promise.all([api('/api/v2/protocols'), props.section === 'profiles' ? api('/api/v2/device-access-profiles') : {}, loadProducts(), props.section === 'protocols' ? api('/api/v2/protocol-source-template') : null, props.section === 'profiles' ? api('/api/v1/connectors') : {}]) /* 执行当前语句并推进处理流程。 */
    protocols.value = catalog.items || [] /* 更新 protocols.value 的值。 */
    snapshots.value = Object.fromEntries((connectors.items || []).filter(x => x.profile).map(x => [x.profile.id, x])) /* 更新 snapshots.value 的值。 */
    profiles.value = (access.items || []).map(p => snapshot(p.id).profile || p) /* 更新 profiles.value 的值。 */
    products.value = productList /* 更新 products.value 的值。 */
    sourceTemplate.value = template /* 更新 sourceTemplate.value 的值。 */
  } catch (error) { notifyError(error) } finally { loading.value = false } /* 结束当前表达式或代码块。 */
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
    UiMessage.success('接入网关已保存') /* 执行当前语句并推进处理流程。 */
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
</script>

<template>
  <div class="page-toolbar"> <!-- 渲染 div 界面元素。 -->
    <template v-if="props.section === 'protocols'">
      <ui-button v-permission="'POST /api/v2/protocols/:id/source-releases'" type="primary" @click="sourceOpen=true">上传源码</ui-button> <!-- 渲染 ui-button 界面元素。 -->
      <ui-button v-permission="'POST /api/v1/ai/protocol-assistant/generate'" title="通过报文或 Excel / CSV 点表生成协议" @click="openAssistant()">协议生成</ui-button> <!-- 渲染 ui-button 界面元素。 -->
      <span>{{ protocols.length }} 个协议 · {{ releaseCount }} 个版本</span> <!-- 渲染 span 界面元素。 -->
    </template>
    <template v-else>
      <ui-button v-permission="'POST /api/v2/device-access-profiles'" type="primary" @click="createProfile">新建网关</ui-button> <!-- 渲染 ui-button 界面元素。 -->
      <span>{{ profiles.length }} 个接入网关</span> <!-- 渲染 span 界面元素。 -->
    </template>
    <ui-button :loading="loading" @click="load">刷新</ui-button>
  </div>
  <ui-card shadow="never" class="surface-card table-card">
    <template v-if="props.section === 'protocols'">
      <ui-table v-loading="loading" :data="pagedProtocols" stripe> <!-- 渲染 ui-table 界面元素。 -->
        <ui-table-column label="协议" min-width="230"><template #default="{ row }"><b>{{ row.definition.name }}</b><small class="subline">{{ row.definition.id }} · {{ row.definition.vendor || '通用' }}</small></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
        <ui-table-column label="最新版本" width="130"><template #default="{ row }">{{ newestRelease(row).version || '—' }}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
        <ui-table-column label="运行方式" min-width="180"><template #default="{ row }">{{ transportLabel(newestRelease(row).transport) }} · {{ label(parsers, newestRelease(row).parserType, '自定义协议程序') }}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
        <ui-table-column label="状态" width="110"><template #default="{ row }"><ui-tag :type="statusType(newestRelease(row).status)" round>{{ statusText(newestRelease(row).status) }}</ui-tag></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
        <ui-table-column label="版本历史" min-width="240"><template #default="{ row }"><div v-for="release in row.releases" :key="release.version" class="release-history"><ui-tag :type="statusType(release.status)" effect="plain">{{ release.version }} · {{ statusText(release.status) }}</ui-tag><small v-if="release.artifact?.platform" class="subline">{{ platformLabel(release.artifact.platform) }} · 发布端样例 {{ release.artifact.testCases || 0 }} 项</small><small v-for="(variant, platform) in (release.artifact?.variants || {})" :key="platform" class="subline">{{ platformLabel(platform) }} · {{ variant.validation === 'COMPILED' ? '已编译，待节点试跑' : '已上传，待节点试跑' }}</small></div></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
        <ui-table-column label="操作" fixed="right" width="120"><template #default="{ row }"> <!-- 每个已有版本使用相同的详情入口。 -->
          <div v-for="release in (row.releases || [])" :key="release.version" class="release-actions"> <!-- 按版本历史顺序排列入口。 -->
            <ui-button size="small" plain type="primary" @click="viewRelease(row, release)">查看版本</ui-button> <!-- 打开该版本详情。 -->
          </div> <!-- 结束当前版本入口。 -->
          <span v-if="!row.releases?.length" class="muted-text">暂无版本</span> <!-- 协议尚未创建版本时给出明确状态。 -->
        </template></ui-table-column> <!-- 结束当前界面区域。 -->
      </ui-table> <!-- 结束当前界面区域。 -->
      <div class="list-pagination"><ui-pagination v-model:current-page="protocolPage" v-model:page-size="protocolPageSize" :total="protocols.length" :page-sizes="[10,20,50,100]" layout="total, sizes, prev, pager, next, jumper" @size-change="protocolPage=1" /></div> <!-- 渲染 div 界面元素。 -->
    </template>
    <template v-else>

      <ui-table v-loading="loading" :data="profiles" stripe row-key="id"> <!-- 渲染 ui-table 界面元素。 -->
        <ui-table-column type="expand"><template #default="{row}"> <!-- 渲染 ui-table-column 界面元素。 -->
          <div class="instance-details"><h4>当前在线会话</h4> <!-- 渲染 div 界面元素。 -->
          <ui-table :data="snapshot(row.id).sessions" empty-text="暂无在线会话"><ui-table-column label="设备"><template #default="{row:session}">{{session.deviceId || '尚未识别设备'}}</template></ui-table-column><ui-table-column prop="remoteAddress" label="远端地址"/><ui-table-column prop="protocolId" label="协议"/><ui-table-column prop="protocolVersion" label="版本"/><ui-table-column label="最后有效报文"><template #default="{row:session}">{{formatTime(session.lastSeenAt)}}</template></ui-table-column></ui-table> <!-- 渲染 ui-table 界面元素。 -->
          <h4>最近接入设备（按创建时间，最多 20 台）</h4><ui-table :data="snapshot(row.id).recentDevices" empty-text="暂无关联设备"><ui-table-column prop="deviceId" label="设备标识"/><ui-table-column prop="name" label="名称"/><ui-table-column label="创建时间"><template #default="{row:device}">{{formatTime(device.createdAt)}}</template></ui-table-column></ui-table></div> <!-- 渲染 h4 界面元素。 -->
        </template></ui-table-column> <!-- 结束当前界面区域。 -->
        <ui-table-column label="在线会话" width="100"><template #default="{row}">{{snapshot(row.id).sessions?.length || 0}}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
        <ui-table-column label="接入网关" min-width="190"><template #default="{ row }"><b>{{ row.id }}</b><small v-if="row.deviceId" class="subline">目标设备：{{ row.deviceId }}</small></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
        <ui-table-column label="关联产品" min-width="180"><template #default="{ row }">{{ products.find(p => p.id === row.productId)?.name || row.productId }}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
        <ui-table-column label="协议版本" min-width="190"><template #default="{ row }">{{ row.protocolId }}@{{ row.protocolVersion }}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
        <ui-table-column label="接入地址 / 端口" min-width="200"><template #default="{ row }">{{ row.host }}:{{ row.port }}<small class="subline">{{ row.mode === 'listener' ? `${transportLabel(row.network)} · ${row.connectionMode === 'dial' ? '平台连接设备' : '设备连接平台'}` : `Modbus 采集 · 站号 ${row.unitId}` }}</small></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
        <ui-table-column label="状态" width="120"><template #default="{ row }"><ui-tag :type="statusType(row.runtimeStatus)" round>{{ statusText(row.runtimeStatus) }}</ui-tag></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
        <ui-table-column label="最近成功" min-width="170"><template #default="{ row }">{{ formatTime(row.lastSuccessAt) }}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
        <ui-table-column label="最近错误" min-width="220" show-overflow-tooltip><template #default="{ row }">{{ row.lastError || '—' }}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
        <ui-table-column label="操作" width="210" fixed="right"><template #default="{ row }"><ui-button v-permission="'POST /api/v2/device-access-profiles/:id/test'" v-if="row.mode !== 'listener'" plain type="primary" :loading="testingId===row.id" @click="testProfile(row)">连接测试</ui-button><ui-button v-permission="'PUT /api/v2/device-access-profiles/:id'" @click="editProfile(row)">编辑</ui-button><ui-button v-permission="'PUT /api/v2/device-access-profiles/:id'" @click="toggleProfile(row)">{{ row.enabled ? '停用' : '启用' }}</ui-button></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
      </ui-table> <!-- 结束当前界面区域。 -->

    </template>
  </ui-card>
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
  <ui-dialog v-model="profileOpen" :title="editingProfile ? '编辑接入网关' : '新建接入网关'" width="min(760px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!savingListener" :show-close="!savingListener">
      <p class="muted-text">接入网关是平台的软件接入服务，用于管理产品的设备连接与端口。一个产品可配置多个网关；现场实体网关在设备管理中登记。</p>
      <ui-form :disabled="savingListener" label-position="top" class="top-gap">
        <div class="form-grid">
          <ui-form-item label="接入网关标识"><ui-input v-model="listener.id" :disabled="editingProfile" placeholder="例如 dahua-tcp" /></ui-form-item>
          <ui-form-item label="关联产品"><ui-select v-model="listener.productId" filterable placeholder="选择需要接入的产品" @change="selectProduct"><ui-option v-for="p in products" :key="p.id" :label="p.name" :value="p.id" /></ui-select></ui-form-item>
          <ui-form-item label="产品绑定协议"><ui-input :model-value="protocols.find(p => p.definition.id === listener.protocolId)?.definition.name || listener.protocolId" readonly placeholder="选择产品后自动读取" /></ui-form-item>
          <ui-form-item label="产品当前绑定版本"><ui-input :model-value="listener.protocolVersion" readonly placeholder="请先在产品管理绑定已发布协议" /></ui-form-item>
          <ui-form-item v-if="listener.mode==='listener'" label="网络"><ui-select v-model="listener.network"><ui-option label="TCP" value="tcp" /><ui-option label="UDP" value="udp" /></ui-select></ui-form-item>
          <ui-form-item v-if="listener.mode==='listener' && listener.network==='tcp'" label="连接方向"><ui-select v-model="listener.connectionMode"><ui-option value="listen" label="设备连接平台"/><ui-option value="dial" label="平台连接设备"/></ui-select></ui-form-item>
<ui-form-item v-if="listener.connectionMode==='dial' || listener.mode==='poll'" label="已配置的设备标识"><ui-input v-model="listener.deviceId" /></ui-form-item>
<ui-form-item :label="listener.connectionMode==='dial' || listener.mode==='poll'?'设备地址 / 主机名':'本机监听地址'"><ui-input v-model="listener.host" /></ui-form-item>
          <ui-form-item label="端口"><ui-input-number v-model="listener.port" :min="1" :max="65535" /></ui-form-item>
          <ui-form-item label="操作超时（毫秒）"><ui-input-number v-model="listener.timeoutMs" :min="1" :max="30000" /></ui-form-item>
        </div>
        <ui-switch v-if="listener.mode==='listener'" v-model="listener.autoRegister" active-text="自动登记协议识别的新设备" />
        <div v-if="listener.mode==='poll'" class="form-grid">
          <ui-form-item label="站号"><ui-input-number v-model="listener.unitId" :min="0" :max="255" /></ui-form-item>
          <ui-form-item label="采集周期（毫秒）"><ui-input-number v-model="listener.intervalMs" :min="1000" /></ui-form-item>
          <ui-form-item label="重试次数"><ui-input-number v-model="listener.retries" :min="0" :max="3" /></ui-form-item>
        </div>
        <ui-switch v-model="listener.enabled" active-text="启用接入" />
        <ui-collapse v-if="listener.mode==='listener'"><ui-collapse-item title="定时查询与子设备" name="advanced"><ProtocolAccessSettings :profile="listener" :can-poll="listener.network==='tcp'" :products="products" :product-id="listener.productId"/></ui-collapse-item></ui-collapse><div class="dialog-actions"><ui-button v-permission="['POST /api/v2/device-access-profiles','PUT /api/v2/device-access-profiles/:id']" type="primary" :loading="savingListener" @click="saveListener">保存接入网关</ui-button></div>
      </ui-form>

  </ui-dialog>
  <details v-if="result" class="technical-details"><summary>最近操作结果</summary><pre>{{ pretty(result) }}</pre></details>
</template>
<style scoped>
.instance-details { padding: 16px 24px; } /* 定义当前元素的样式规则。 */
.release-history + .release-history,.release-actions + .release-actions { margin-top: 12px; } /* 定义当前元素的样式规则。 */
.release-buttons { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 6px; } /* 定义当前元素的样式规则。 */
.release-buttons .el-button + .el-button { margin-left: 0; } /* 定义当前元素的样式规则。 */
.release-detail-actions { align-items: center; margin-top: 18px; } /* 让专项操作在版本信息下保持整齐。 */
.source-error { white-space: pre-wrap; overflow-wrap: anywhere; max-height: 300px; overflow: auto; } /* 定义当前元素的样式规则。 */
.source-file-field { width: 100%; min-width: 0; display: grid; gap: 7px; } /* 文件操作独占一行，避免按钮与文件名相互挤压。 */
.source-file-field small, .source-template-panel small, .source-publish-panel small, .source-target-help, .source-compiling-help { display: block; color: #697386; font-size: 12px; line-height: 1.5; } /* 辅助说明换行显示并保持可读。 */
.source-template-panel { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 12px 20px; margin: 2px 0 16px; padding: 14px 16px; border: 1px solid #e1e8f0; border-radius: 8px; background: #f8fafc; } /* 将模板下载与文件上传分组。 */
.source-template-panel strong, .source-publish-panel strong { display: block; margin-bottom: 3px; font-size: 13px; color: #243145; } /* 明确每组操作的用途。 */
.source-template-actions { display: flex; flex-wrap: wrap; gap: 8px; } /* 模板按钮在窄视口自动换行。 */
.source-compile-options { margin-bottom: 16px; } /* 编译选项与发布方式分隔。 */
.source-target-item { margin: 8px 0 4px !important; } /* 下拉框与自身说明保持一组。 */
.source-target-help { margin-bottom: 12px; } /* 说明独占一行，避免挤到下拉框右侧。 */
.source-publish-panel { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 12px 20px; padding: 14px 16px; border: 1px solid #d9e8df; border-radius: 8px; background: #f7fbf8; } /* 发布行为及结果说明集中展示。 */
.source-compiling-help { margin-top: 12px; } /* 编译耗时提示放在发布设置下方。 */
.source-submit-row { display: flex; width: 100%; align-items: center; justify-content: space-between; gap: 14px; } /* 提交操作固定在弹窗页脚。 */
.source-submit-row span { color: #697386; font-size: 12px; line-height: 1.5; } /* 页脚简述实际执行顺序。 */
.source-submit-row button { flex: none; } /* 提交按钮不被说明文字压缩。 */
@media (max-width: 640px) { .source-template-actions, .source-template-actions button, .source-submit-row, .source-submit-row button { width: 100%; } .source-submit-row { flex-wrap: wrap; } } /* 窄屏使用单列按钮，避免横向溢出。 */
</style>
