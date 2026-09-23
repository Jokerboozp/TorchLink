<script setup>
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
const emit = defineEmits(['navigate']) /* 声明 emit。 */
import ProtocolAccessSettings from '../components/ProtocolAccessSettings.vue' /* 引入当前代码需要的依赖。 */
import ProtocolAssistantView from './ProtocolAssistantView.vue' /* 引入当前代码需要的依赖。 */
import FilePicker from '../components/FilePicker.vue' /* 引入当前代码需要的依赖。 */
import { transportLabel, formatLabel, statusLabel, platformLabel } from '../presentation' /* 引入当前代码需要的依赖。 */
import { computed, onMounted, reactive, ref, watch } from 'vue' /* 引入当前代码需要的依赖。 */
import { label, parsers } from '../labels' /* 引入当前代码需要的依赖。 */
import { ElMessage } from 'element-plus' /* 引入当前代码需要的依赖。 */
import { api, download, formatTime, notifyError, pretty } from '../api' /* 引入当前代码需要的依赖。 */

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
const assistantRelease = ref(null), assistantName = ref('') /* 声明 assistantRelease。 */
function openAssistant(release = null, name = '') { assistantRelease.value=release;assistantName.value=name;assistantOpen.value=true } /* 定义 openAssistant 函数。 */
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
  if (!sourceFile.value || !source.protocolId) return ElMessage.warning('请选择 Go 源码并填写协议标识') /* 判断条件并选择处理分支。 */
  if (sourceFile.value.size > 32 * 1024 * 1024) return ElMessage.warning('源码文件不能超过 32 兆字节') /* 判断条件并选择处理分支。 */
  compiling.value = true /* 更新 compiling.value 的值。 */
  sourceError.value = '' /* 更新 sourceError.value 的值。 */
  result.value = null /* 更新 result.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const body = new FormData() /* 声明 body。 */
    body.append('file', sourceFile.value) /* 执行当前语句并推进处理流程。 */
    if (targetPlatforms.value.length) body.append('targetPlatforms', JSON.stringify(targetPlatforms.value)) /* 判断条件并选择处理分支。 */
    for (const [key, value] of Object.entries(source)) body.append(key, key === 'productId' && !source.publish ? '' : String(value)) /* 循环处理当前数据。 */
    result.value = await api(`/api/v2/protocols/${encodeURIComponent(source.protocolId)}/source-releases`, { method:'POST', body }) /* 更新 result.value 的值。 */
    ElMessage.success(result.value.binding ? '编译与样例测试通过，产品已切换到新版本' : source.publish ? '编译与样例测试通过，协议已发布，可绑定产品使用' : '编译与样例测试通过，已保存校验版本') /* 执行当前语句并推进处理流程。 */
    sourceOpen.value = false /* 更新 sourceOpen.value 的值。 */
    await load() /* 等待异步操作完成。 */
  } catch (error) { sourceError.value = error?.message || String(error) } /* 结束当前表达式或代码块。 */
  finally { compiling.value = false } /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
async function publishRelease(protocolId, version) { /* 定义 publishRelease 函数。 */
  switching.value = true /* 更新 switching.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    await api(`/api/v2/protocols/${encodeURIComponent(protocolId)}/releases/${encodeURIComponent(version)}/publish`, { method:'POST', body:'{}' }) /* 等待异步操作完成。 */
    ElMessage.success('版本已发布，可绑定产品使用') /* 执行当前语句并推进处理流程。 */
    await load() /* 等待异步操作完成。 */
  } catch (error) { notifyError(error) } finally { switching.value = false } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
async function saveListener() { /* 定义 saveListener 函数。 */
  if (savingListener.value) return /* 判断条件并选择处理分支。 */
  if (!listener.id || !listener.productId || !listener.protocolId || !listener.protocolVersion) return ElMessage.warning('请填写网关标识、产品及其当前绑定的设备协议版本') /* 判断条件并选择处理分支。 */
  if (!editingProfile.value && profiles.value.some(p => p.id === listener.id)) return ElMessage.warning('网关标识已存在，请在列表中编辑') /* 判断条件并选择处理分支。 */
  savingListener.value = true /* 更新 savingListener.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    result.value = await api(editingProfile.value ? `/api/v2/device-access-profiles/${encodeURIComponent(listener.id)}` : '/api/v2/device-access-profiles', { method:editingProfile.value ? 'PUT' : 'POST', body:JSON.stringify(listener) }) /* 更新 result.value 的值。 */
    ElMessage.success('接入网关已保存') /* 执行当前语句并推进处理流程。 */
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
    ElMessage.success('连接与单次采集测试通过') /* 执行当前语句并推进处理流程。 */
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
      <el-button v-permission="'POST /api/v2/protocols/:id/source-releases'" type="primary" @click="sourceOpen=true">上传源码</el-button> <!-- 渲染 el-button 界面元素。 -->
      <el-button v-permission="'POST /api/v1/ai/protocol-assistant/generate'" title="通过报文或 Excel / CSV 点表生成协议" @click="openAssistant()">协议生成</el-button> <!-- 渲染 el-button 界面元素。 -->
      <span>{{ protocols.length }} 个协议 · {{ releaseCount }} 个版本</span> <!-- 渲染 span 界面元素。 -->
    </template>
    <template v-else>
      <el-button v-permission="'POST /api/v2/device-access-profiles'" type="primary" @click="createProfile">新建网关</el-button> <!-- 渲染 el-button 界面元素。 -->
      <span>{{ profiles.length }} 个接入网关</span> <!-- 渲染 span 界面元素。 -->
    </template>
    <el-button :loading="loading" @click="load">刷新</el-button>
  </div>
  <el-card shadow="never" class="surface-card table-card">
    <template v-if="props.section === 'protocols'">
      <el-table v-loading="loading" :data="pagedProtocols" stripe> <!-- 渲染 el-table 界面元素。 -->
        <el-table-column label="协议" min-width="230"><template #default="{ row }"><b>{{ row.definition.name }}</b><small class="subline">{{ row.definition.id }} · {{ row.definition.vendor || '通用' }}</small></template></el-table-column> <!-- 渲染 el-table-column 界面元素。 -->
        <el-table-column label="最新版本" width="130"><template #default="{ row }">{{ newestRelease(row).version || '—' }}</template></el-table-column> <!-- 渲染 el-table-column 界面元素。 -->
        <el-table-column label="运行方式" min-width="180"><template #default="{ row }">{{ transportLabel(newestRelease(row).transport) }} · {{ label(parsers, newestRelease(row).parserType, '自定义协议程序') }}</template></el-table-column> <!-- 渲染 el-table-column 界面元素。 -->
        <el-table-column label="状态" width="110"><template #default="{ row }"><el-tag :type="statusType(newestRelease(row).status)" round>{{ statusText(newestRelease(row).status) }}</el-tag></template></el-table-column> <!-- 渲染 el-table-column 界面元素。 -->
        <el-table-column label="版本历史" min-width="240"><template #default="{ row }"><div v-for="release in row.releases" :key="release.version" class="release-history"><el-tag :type="statusType(release.status)" effect="plain">{{ release.version }} · {{ statusText(release.status) }}</el-tag><small v-if="release.artifact?.platform" class="subline">{{ platformLabel(release.artifact.platform) }} · 发布端样例 {{ release.artifact.testCases || 0 }} 项</small><small v-for="(variant, platform) in (release.artifact?.variants || {})" :key="platform" class="subline">{{ platformLabel(platform) }} · {{ variant.validation === 'COMPILED' ? '已编译，待节点试跑' : '已上传，待节点试跑' }}</small></div></template></el-table-column> <!-- 渲染 el-table-column 界面元素。 -->
        <el-table-column label="操作" fixed="right" width="190"><template #default="{ row }"> <!-- 渲染 el-table-column 界面元素。 -->
          <div v-for="release in (row.releases?.length ? row.releases : [{}])" :key="release.version || 'empty'" class="release-actions"> <!-- 渲染 div 界面元素。 -->
            <div class="release-buttons"> <!-- 渲染 div 界面元素。 -->
              <el-button v-if="release.artifact?.generatedMapping" v-permission="'POST /api/v2/protocols/:id/releases/:version/preview'" size="small" plain type="primary" @click="openAssistant(release, row.definition.name)">解析测试</el-button> <!-- 渲染 el-button 界面元素。 -->
              <el-button v-if="release.status === 'VALIDATED'" v-permission="'POST /api/v2/protocols/:id/releases/:version/publish'" size="small" plain type="primary" :loading="switching" @click="publishRelease(row.definition.id, release.version)">发布</el-button> <!-- 渲染 el-button 界面元素。 -->
              <el-button v-if="release.artifact?.build?.kind === 'go-source'" v-permission="'GET /api/v2/protocols/:id/releases/:version/source'" size="small" plain @click="downloadSourceRelease(row.definition.id, release)">源码</el-button> <!-- 渲染 el-button 界面元素。 -->
              <span v-if="!release.artifact?.generatedMapping && release.status !== 'VALIDATED' && release.artifact?.build?.kind !== 'go-source'">—</span> <!-- 渲染 span 界面元素。 -->
            </div> <!-- 结束当前界面区域。 -->
          </div> <!-- 结束当前界面区域。 -->
        </template></el-table-column> <!-- 结束当前界面区域。 -->
      </el-table> <!-- 结束当前界面区域。 -->
      <div class="list-pagination"><el-pagination v-model:current-page="protocolPage" v-model:page-size="protocolPageSize" :total="protocols.length" :page-sizes="[10,20,50,100]" layout="total, sizes, prev, pager, next, jumper" @size-change="protocolPage=1" /></div> <!-- 渲染 div 界面元素。 -->
    </template>
    <template v-else>

      <el-table v-loading="loading" :data="profiles" stripe row-key="id"> <!-- 渲染 el-table 界面元素。 -->
        <el-table-column type="expand"><template #default="{row}"> <!-- 渲染 el-table-column 界面元素。 -->
          <div class="instance-details"><h4>当前在线会话</h4> <!-- 渲染 div 界面元素。 -->
          <el-table :data="snapshot(row.id).sessions" empty-text="暂无在线会话"><el-table-column label="设备"><template #default="{row:session}">{{session.deviceId || '尚未识别设备'}}</template></el-table-column><el-table-column prop="remoteAddress" label="远端地址"/><el-table-column prop="protocolId" label="协议"/><el-table-column prop="protocolVersion" label="版本"/><el-table-column label="最后有效报文"><template #default="{row:session}">{{formatTime(session.lastSeenAt)}}</template></el-table-column></el-table> <!-- 渲染 el-table 界面元素。 -->
          <h4>最近接入设备（按创建时间，最多 20 台）</h4><el-table :data="snapshot(row.id).recentDevices" empty-text="暂无关联设备"><el-table-column prop="deviceId" label="设备标识"/><el-table-column prop="name" label="名称"/><el-table-column label="创建时间"><template #default="{row:device}">{{formatTime(device.createdAt)}}</template></el-table-column></el-table></div> <!-- 渲染 h4 界面元素。 -->
        </template></el-table-column> <!-- 结束当前界面区域。 -->
        <el-table-column label="在线会话" width="100"><template #default="{row}">{{snapshot(row.id).sessions?.length || 0}}</template></el-table-column> <!-- 渲染 el-table-column 界面元素。 -->
        <el-table-column label="接入网关" min-width="190"><template #default="{ row }"><b>{{ row.id }}</b><small v-if="row.deviceId" class="subline">目标设备：{{ row.deviceId }}</small></template></el-table-column> <!-- 渲染 el-table-column 界面元素。 -->
        <el-table-column label="关联产品" min-width="180"><template #default="{ row }">{{ products.find(p => p.id === row.productId)?.name || row.productId }}</template></el-table-column> <!-- 渲染 el-table-column 界面元素。 -->
        <el-table-column label="协议版本" min-width="190"><template #default="{ row }">{{ row.protocolId }}@{{ row.protocolVersion }}</template></el-table-column> <!-- 渲染 el-table-column 界面元素。 -->
        <el-table-column label="接入地址 / 端口" min-width="200"><template #default="{ row }">{{ row.host }}:{{ row.port }}<small class="subline">{{ row.mode === 'listener' ? `${transportLabel(row.network)} · ${row.connectionMode === 'dial' ? '平台连接设备' : '设备连接平台'}` : `Modbus 采集 · 站号 ${row.unitId}` }}</small></template></el-table-column> <!-- 渲染 el-table-column 界面元素。 -->
        <el-table-column label="状态" width="120"><template #default="{ row }"><el-tag :type="statusType(row.runtimeStatus)" round>{{ statusText(row.runtimeStatus) }}</el-tag></template></el-table-column> <!-- 渲染 el-table-column 界面元素。 -->
        <el-table-column label="最近成功" min-width="170"><template #default="{ row }">{{ formatTime(row.lastSuccessAt) }}</template></el-table-column> <!-- 渲染 el-table-column 界面元素。 -->
        <el-table-column label="最近错误" min-width="220" show-overflow-tooltip><template #default="{ row }">{{ row.lastError || '—' }}</template></el-table-column> <!-- 渲染 el-table-column 界面元素。 -->
        <el-table-column label="操作" width="210" fixed="right"><template #default="{ row }"><el-button v-permission="'POST /api/v2/device-access-profiles/:id/test'" v-if="row.mode !== 'listener'" plain type="primary" :loading="testingId===row.id" @click="testProfile(row)">连接测试</el-button><el-button v-permission="'PUT /api/v2/device-access-profiles/:id'" @click="editProfile(row)">编辑</el-button><el-button v-permission="'PUT /api/v2/device-access-profiles/:id'" @click="toggleProfile(row)">{{ row.enabled ? '停用' : '启用' }}</el-button></template></el-table-column> <!-- 渲染 el-table-column 界面元素。 -->
      </el-table> <!-- 结束当前界面区域。 -->

    </template>
  </el-card>
  <el-dialog v-model="assistantOpen" title="生成协议" width="min(980px, 94vw)" :close-on-click-modal="false" destroy-on-close><p v-if="!assistantRelease" class="muted-text bottom-gap">通过报文或 Excel / CSV 点表生成协议</p><ProtocolAssistantView v-if="assistantOpen" :initial-release="assistantRelease" :initial-name="assistantName" @saved="load" @navigate="assistantNavigate" /></el-dialog>
  <el-dialog v-model="sourceOpen" title="上传协议源码" width="min(720px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!compiling" :show-close="!compiling">

      <el-alert v-if="sourceTemplate && !sourceTemplate.compilerAvailable" class="top-gap" title="当前服务缺少源码编译环境，请联系管理员部署支持编译的后端服务。" type="warning" :closable="false" />
      <el-form :disabled="compiling" :model="source" label-position="top" class="top-gap">
        <div class="form-grid">
          <el-form-item label="协议标识"><el-input v-model="source.protocolId" placeholder="例如 vendor-fire" /></el-form-item>
          <el-form-item label="协议名称"><el-input v-model="source.name" placeholder="例如消防设备协议" /></el-form-item>
          <el-form-item label="版本"><el-input v-model="source.version" placeholder="Go 函数模式留空自动生成新版本" /></el-form-item>
          <el-form-item label="绑定产品（可选）"><el-select v-model="source.productId" filterable clearable :disabled="!source.publish" placeholder="选择后，发布成功立即切换"><el-option v-for="p in products" :key="p.id" :label="`${p.name} · ${p.id}`" :value="p.id" /></el-select></el-form-item>
          <el-form-item label="设备上报通道"><el-select v-model="source.transport" clearable placeholder="自动识别，可手动选择"><el-option v-for="value in ['MQTT','HTTP','TCP','UDP','TCP_UDP']" :key="value" :label="transportLabel(value)" :value="value" /></el-select></el-form-item>
        </div>
        <el-form-item label="源码文件或项目压缩包">
          <FilePicker accept=".go,.zip" :disabled="compiling" @change="chooseSourceFile" />
          <el-button plain class="left-gap" :disabled="!sourceTemplate" @click="downloadSourceTemplate()">下载解析模板</el-button>
          <el-button plain class="left-gap" :disabled="!sourceTemplate" @click="downloadSourceTemplate('tcp')">下载 TCP / UDP 模板</el-button>
          <small class="subline">支持 .go 或完整项目 ZIP，样例通过后才可发布。</small>
        </el-form-item>
        <el-collapse>
          <el-collapse-item title="编译选项" name="advanced">
            <el-form-item label="额外编译目标（可选）">
              <el-select v-model="targetPlatforms" multiple clearable :disabled="compiling" placeholder="默认仅构建发布端平台"><el-option v-for="platform in (sourceTemplate?.targetPlatforms || [])" :key="platform" :label="platformLabel(platform)" :value="platform" /></el-select>
              <small class="subline">其他平台仅生成编译制品；在对应平台实际试跑前不能视为验收通过。</small>
            </el-form-item>
          </el-collapse-item>
        </el-collapse>
        <el-switch v-model="source.publish" active-text="测试通过后立即发布" inactive-text="仅保存已校验版本" />
        <div class="dialog-actions"><el-button v-permission="'POST /api/v2/protocols/:id/source-releases'" type="primary" :loading="compiling" :disabled="sourceTemplate && !sourceTemplate.compilerAvailable" @click="uploadSource">{{ compiling ? '正在编译并试跑样例…' : source.publish ? '上传、编译并发布' : '上传、编译并校验' }}</el-button></div>
        <small v-if="compiling" class="subline">首次编译可能较慢，请保持页面打开。每个平台编译最长 120 秒，随后运行样例测试。</small>
        <el-alert v-if="sourceError" class="top-gap" title="操作未完成，请查看原因" type="error" :closable="false"><pre class="source-error">{{ sourceError }}</pre></el-alert>
      </el-form>

  </el-dialog>
  <el-dialog v-model="profileOpen" :title="editingProfile ? '编辑接入网关' : '新建接入网关'" width="min(760px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!savingListener" :show-close="!savingListener">
      <p class="muted-text">接入网关是平台的软件接入服务，用于管理产品的设备连接与端口。一个产品可配置多个网关；现场实体网关在设备管理中登记。</p>
      <el-form :disabled="savingListener" label-position="top" class="top-gap">
        <div class="form-grid">
          <el-form-item label="接入网关标识"><el-input v-model="listener.id" :disabled="editingProfile" placeholder="例如 dahua-tcp" /></el-form-item>
          <el-form-item label="关联产品"><el-select v-model="listener.productId" filterable placeholder="选择需要接入的产品" @change="selectProduct"><el-option v-for="p in products" :key="p.id" :label="p.name" :value="p.id" /></el-select></el-form-item>
          <el-form-item label="产品绑定协议"><el-input :model-value="protocols.find(p => p.definition.id === listener.protocolId)?.definition.name || listener.protocolId" readonly placeholder="选择产品后自动读取" /></el-form-item>
          <el-form-item label="产品当前绑定版本"><el-input :model-value="listener.protocolVersion" readonly placeholder="请先在产品管理绑定已发布协议" /></el-form-item>
          <el-form-item v-if="listener.mode==='listener'" label="网络"><el-select v-model="listener.network"><el-option label="TCP" value="tcp" /><el-option label="UDP" value="udp" /></el-select></el-form-item>
          <el-form-item v-if="listener.mode==='listener' && listener.network==='tcp'" label="连接方向"><el-select v-model="listener.connectionMode"><el-option value="listen" label="设备连接平台"/><el-option value="dial" label="平台连接设备"/></el-select></el-form-item>
<el-form-item v-if="listener.connectionMode==='dial' || listener.mode==='poll'" label="已配置的设备标识"><el-input v-model="listener.deviceId" /></el-form-item>
<el-form-item :label="listener.connectionMode==='dial' || listener.mode==='poll'?'设备地址 / 主机名':'本机监听地址'"><el-input v-model="listener.host" /></el-form-item>
          <el-form-item label="端口"><el-input-number v-model="listener.port" :min="1" :max="65535" /></el-form-item>
          <el-form-item label="操作超时（毫秒）"><el-input-number v-model="listener.timeoutMs" :min="1" :max="30000" /></el-form-item>
        </div>
        <el-switch v-if="listener.mode==='listener'" v-model="listener.autoRegister" active-text="自动登记协议识别的新设备" />
        <div v-if="listener.mode==='poll'" class="form-grid">
          <el-form-item label="站号"><el-input-number v-model="listener.unitId" :min="0" :max="255" /></el-form-item>
          <el-form-item label="采集周期（毫秒）"><el-input-number v-model="listener.intervalMs" :min="1000" /></el-form-item>
          <el-form-item label="重试次数"><el-input-number v-model="listener.retries" :min="0" :max="3" /></el-form-item>
        </div>
        <el-switch v-model="listener.enabled" active-text="启用接入" />
        <el-collapse v-if="listener.mode==='listener'"><el-collapse-item title="定时查询与子设备" name="advanced"><ProtocolAccessSettings :profile="listener" :can-poll="listener.network==='tcp'" :products="products" :product-id="listener.productId"/></el-collapse-item></el-collapse><div class="dialog-actions"><el-button v-permission="['POST /api/v2/device-access-profiles','PUT /api/v2/device-access-profiles/:id']" type="primary" :loading="savingListener" @click="saveListener">保存接入网关</el-button></div>
      </el-form>

  </el-dialog>
  <details v-if="result" class="technical-details"><summary>最近操作结果</summary><pre>{{ pretty(result) }}</pre></details>
</template>
<style scoped>
.instance-details { padding: 16px 24px; } /* 定义当前元素的样式规则。 */
.release-history + .release-history,.release-actions + .release-actions { margin-top: 12px; } /* 定义当前元素的样式规则。 */
.release-buttons { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 6px; } /* 定义当前元素的样式规则。 */
.release-buttons .el-button + .el-button { margin-left: 0; } /* 定义当前元素的样式规则。 */
.source-error { white-space: pre-wrap; overflow-wrap: anywhere; max-height: 300px; overflow: auto; } /* 定义当前元素的样式规则。 */
</style>
