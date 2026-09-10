<script setup>
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate'])
import ProtocolAccessSettings from '../components/ProtocolAccessSettings.vue'
import FilePicker from '../components/FilePicker.vue'
import { transportLabel, formatLabel, statusLabel, platformLabel } from '../presentation'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { label, parsers } from '../labels'
import ProtocolCatalog from '../components/ProtocolCatalog.vue'
import ProtocolMarket from '../components/ProtocolMarket.vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, download, formatTime, notifyError, pretty } from '../api'

const protocols = ref([])
const profiles = ref([])
const snapshots = ref({})
const snapshot = (id) => snapshots.value[id] || { sessions: [], recentDevices: [] }
const loading = ref(false)
const testingId = ref('')
const result = ref(null)
const sourceFile = ref(null)
const targetPlatforms = ref([])
const compiling = ref(false)
const sourceError = ref('')
const sourceTemplate = ref(null)
const products = ref([])
const switching = ref(false)
const source = reactive({ protocolId:'', name:'', version:'', productId:'', runtime:'', capabilities:'', transport:'', payloadFormat:'', entrypoint:'', publish:true, cases:'' })
const binding = reactive({ productId:'', protocolId:'', version:'' })
const publishedReleases = computed(() => protocols.value.find(item => item.definition.id === binding.protocolId)?.releases?.filter(item => item.status === 'PUBLISHED') || [])

const activeTab=ref('0')
const listener = reactive({ id:'', productId:'', protocolId:'', protocolVersion:'', mode:'listener', network:'tcp', host:'0.0.0.0', port:26875, timeoutMs:5000, autoRegister:false, enabled:true, connectionMode:'listen', deviceId:'',queries:[],childProducts:[] })
watch(()=>listener.network,value=>{if(value!=='tcp'){listener.connectionMode='listen';listener.deviceId='';listener.queries=[]}})
function editProfile(profile){Object.assign(listener,{connectionMode:'listen',deviceId:'',queries:[],childProducts:[]},JSON.parse(JSON.stringify(profile)));activeTab.value='network'}
const savingListener = ref(false)
const listenerReleases = computed(() => protocols.value.find(item => item.definition.id === listener.protocolId)?.releases?.filter(item => item.status === 'PUBLISHED' && item.artifact?.runtime === 'go-protocol-v2' && item.capabilities?.includes('ingress')) || [])
const command = reactive({ profileId:'', deviceId:'', body:'{"type":"time-sync"}' })
const sendingCommand = ref(false)
const pendingProtocolCommand = ref(null)
const releaseCount = computed(() => protocols.value.reduce((total, item) => total + (item.releases?.length || 0), 0))

async function loadProducts() {
  const items = []
  for (let page = 1; ; page += 1) {
    const result = await api(`/api/v1/products?page=${page}&pageSize=100`)
    items.push(...(result.items || []))
    if (!result.items?.length || items.length >= Number(result.total ?? result.count ?? items.length)) return items
  }
}

async function load() {
  loading.value = true
  try {
    const [catalog, access, productList, template, connectors] = await Promise.all([api('/api/v2/protocols'), api('/api/v2/device-access-profiles'), loadProducts(), api('/api/v2/protocol-source-template'), api('/api/v1/connectors')])
    protocols.value = catalog.items || []
    snapshots.value = Object.fromEntries((connectors.items || []).filter(x => x.profile).map(x => [x.profile.id, x]))
    profiles.value = (access.items || []).map(p => snapshot(p.id).profile || p)
    products.value = productList
    sourceTemplate.value = template
  } catch (error) { notifyError(error) } finally { loading.value = false }
}

function chooseSourceFile(event) { sourceFile.value = event.target.files?.[0] || null; sourceError.value = '' }
async function downloadSourceTemplate(kind = '') {
  try { await download(`/api/v2/protocol-source-template?format=go-functions&kind=${kind}`, kind === 'tcp' ? 'go-tcp-protocol.zip' : 'go-protocol.zip') }
  catch (error) { notifyError(error) }
}
async function uploadSource() {
  if (!sourceFile.value || !source.protocolId) return ElMessage.warning('请选择 Go 源码并填写协议标识')
  if (sourceFile.value.size > 32 * 1024 * 1024) return ElMessage.warning('源码文件不能超过 32 兆字节')
  if (source.cases.trim()) {
    try { const cases = JSON.parse(source.cases); if (!Array.isArray(cases) || !cases.length) throw new Error() }
    catch { return ElMessage.warning('样例测试须为非空结构化数据数组') }
  }
  compiling.value = true
  sourceError.value = ''
  result.value = null
  try {
    const body = new FormData()
    body.append('file', sourceFile.value)
    if (targetPlatforms.value.length) body.append('targetPlatforms', JSON.stringify(targetPlatforms.value))
    for (const [key, value] of Object.entries(source)) body.append(key, key === 'productId' && !source.publish ? '' : String(value))
    result.value = await api(`/api/v2/protocols/${encodeURIComponent(source.protocolId)}/source-releases`, { method:'POST', body })
    ElMessage.success(result.value.binding ? '编译与样例测试通过，产品已切换到新版本' : source.publish ? '编译与样例测试通过，协议已发布，可绑定产品使用' : '编译与样例测试通过，已保存校验版本')
    await load()
  } catch (error) { sourceError.value = error?.message || String(error) }
  finally { compiling.value = false }
}
async function switchBinding(rollback = false) {
  if (!binding.productId || (!rollback && (!binding.protocolId || !binding.version))) return ElMessage.warning('请选择产品及已发布的协议版本')
  switching.value = true
  try {
    result.value = await api(`/api/v2/products/${encodeURIComponent(binding.productId)}/protocol-binding${rollback ? '/rollback' : ''}`, { method:'POST', body:JSON.stringify(rollback ? {} : { protocolId:binding.protocolId, version:binding.version }) })
    ElMessage.success(rollback ? '产品已回滚到上一版本' : '产品已切换协议版本，新报文立即使用')
    await load()
  } catch (error) { notifyError(error) } finally { switching.value = false }
}
async function publishRelease(protocolId, version) {
  switching.value = true
  try {
    await api(`/api/v2/protocols/${encodeURIComponent(protocolId)}/releases/${encodeURIComponent(version)}/publish`, { method:'POST', body:'{}' })
    ElMessage.success('版本已发布，可绑定产品使用')
    await load()
  } catch (error) { notifyError(error) } finally { switching.value = false }
}
async function saveListener() {
  if (!listener.id || !listener.productId || !listener.protocolId || !listener.protocolVersion) return ElMessage.warning('请填写实例标识、产品及其当前绑定的设备协议版本')
  savingListener.value = true
  try {
    result.value = await api('/api/v2/device-access-profiles', { method:'POST', body:JSON.stringify(listener) })
    ElMessage.success(listener.connectionMode==='dial'?'接入实例已保存，将开始连接设备':'接入实例已保存，将开始监听')
    await load()
  } catch (error) { notifyError(error) } finally { savingListener.value = false }
}
async function toggleProfile(profile) {
  try {
    await api(`/api/v2/device-access-profiles/${encodeURIComponent(profile.id)}`, { method:'PUT', body:JSON.stringify({ ...profile, enabled:!profile.enabled }) })
    await load()
  } catch (error) { notifyError(error) }
}
async function downloadRelease(protocolId, release, kind) {
  try { await download(`/api/v2/protocols/${encodeURIComponent(protocolId)}/releases/${encodeURIComponent(release.version)}/${kind}`, `${protocolId}-${release.version}-${kind}.${kind === 'source' && release.artifact?.filename?.toLowerCase().endsWith('.go') ? 'go' : 'zip'}`) }
  catch (error) { notifyError(error) }
}
async function sendCommand() {
  if (!command.profileId || !command.deviceId) return ElMessage.warning('请选择接入实例并填写在线设备标识')
  let body
  try { body = JSON.parse(command.body); if (!body || typeof body.type !== 'string' || !body.type.trim()) throw new Error() }
  catch { return ElMessage.warning('命令须为包含 type 的结构化数据对象') }
  try { await ElMessageBox.confirm('确认向此设备发送协议命令？请核对参数，结果未知时不要另建命令重发。','人工确认命令') } catch { return }
  const signature = JSON.stringify([command.profileId,command.deviceId,body])
  if (!pendingProtocolCommand.value || pendingProtocolCommand.value.signature !== signature) pendingProtocolCommand.value={signature,id:crypto.randomUUID()}
  body={...body,requestId:pendingProtocolCommand.value.id,confirmed:true}
  sendingCommand.value = true
  try { result.value = await api(`/api/v2/device-access-profiles/${encodeURIComponent(command.profileId)}/devices/${encodeURIComponent(command.deviceId)}/commands`, { method:'POST', body:JSON.stringify(body) }); ElMessage.success(result.value.status === 'acknowledged' ? '设备已应答' : '命令已发送') }
  catch (error) { notifyError(error) } finally { sendingCommand.value = false }
}


async function testProfile(profile) {
  testingId.value = profile.id
  try {
    result.value = await api(`/api/v2/device-access-profiles/${encodeURIComponent(profile.id)}/test`, { method:'POST', body:'{}' })
    ElMessage.success('连接与单次采集测试通过')
    await load()
  } catch (error) { notifyError(error) } finally { testingId.value = '' }
}

function newestRelease(item) { return item.releases?.[0] || {} }
function statusText(value) { return ({ LISTENING:'监听中', DISABLED:'已停用', DRAFT:'草稿', VALIDATED:'已校验', PUBLISHED:'已发布', DEPRECATED:'已弃用', REVOKED:'已撤销', PENDING:'待启动', ONLINE:'在线采集', ERROR:'采集异常' })[value] || statusLabel(value) }
function statusType(value) { return ({ PUBLISHED:'success', ONLINE:'success', ERROR:'danger', REVOKED:'danger', VALIDATED:'warning', PENDING:'info' })[value] || 'info' }

onMounted(load)
</script>

<template>
  <div class="page-toolbar">
    <el-button :loading="loading" @click="load">刷新</el-button>
    <el-tag type="success" round>协议版本管理</el-tag>
    <span>{{ protocols.length }} 个协议，{{ releaseCount }} 个不可变版本，{{ profiles.length }} 个接入实例</span>
  </div>

  <el-tabs v-model="activeTab" type="border-card">
    <el-tab-pane label="源码接入">
      <el-alert title="上传协议源码，接入自定义协议" description="下载模板，只修改 Go 函数和 Go 样例，然后上传文件或 ZIP。平台自动识别能力、生成版本、编译并核对结果；选择产品后可直接发布绑定。" type="info" :closable="false" show-icon />
      <el-alert v-if="sourceTemplate && !sourceTemplate.compilerAvailable" class="top-gap" title="当前服务缺少源码编译环境，请联系管理员部署支持编译的后端服务。" type="warning" :closable="false" />
      <el-form :model="source" label-position="top" class="top-gap">
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
          <small class="subline">只修改 protocol.go。直接上传这个文件，或将整个项目打成 ZIP；无需编写 JSON、main 或调用入口。样例也使用 Go 编写，预期结果不符会阻止发布。</small>
        </el-form-item>
        <el-collapse>
          <el-collapse-item title="高级设置与旧协议包兼容" name="advanced">
            <small class="subline">旧协议包仍可使用 protocol.json 和样例 JSON；未填写的字段读取包内配置。</small>
            <div class="form-grid">
              <el-form-item label="报文格式"><el-select v-model="source.payloadFormat" clearable placeholder="Go 函数模式使用字节报文"><el-option v-for="value in ['hex','json','text','base64']" :key="value" :label="formatLabel(value)" :value="value" /></el-select></el-form-item>
              <el-form-item label="运行时（旧协议包）"><el-select v-model="source.runtime" clearable placeholder="Go 函数模式请留空"><el-option label="报文解析（第一版）" value="go-json-lines-v1" /><el-option label="完整接入（第二版）" value="go-protocol-v2" /></el-select></el-form-item>
              <el-form-item label="操作能力（旧协议包）"><el-input v-model="source.capabilities" placeholder='Go 函数模式请留空；旧包例如 ["decode"]' /></el-form-item>
            </div>
            <el-form-item label="额外编译目标（可选）">
              <el-select v-model="targetPlatforms" multiple clearable :disabled="compiling" placeholder="默认仅构建发布端平台"><el-option v-for="platform in (sourceTemplate?.targetPlatforms || [])" :key="platform" :label="platformLabel(platform)" :value="platform" /></el-select>
              <small class="subline">其他平台仅生成编译制品；在对应平台实际试跑前不能视为验收通过。</small>
            </el-form-item>
            <el-form-item label="项目编译入口（旧协议包）"><el-input v-model="source.entrypoint" placeholder="默认为 .，Go 函数模式请留空" /></el-form-item>
            <el-form-item label="覆盖样例（旧协议包）">
              <el-input v-model="source.cases" type="textarea" :rows="7" spellcheck="false" />
              <small class="subline">Go 函数模式在 Protocol 中填写 Samples。此处填写 JSON 会覆盖源码样例，仅用于兼容已有协议包。</small>
            </el-form-item>
          </el-collapse-item>
        </el-collapse>
        <el-switch v-model="source.publish" active-text="测试通过后立即发布" inactive-text="仅保存已校验版本" />
        <div class="dialog-actions"><el-button type="primary" :loading="compiling" :disabled="sourceTemplate && !sourceTemplate.compilerAvailable" @click="uploadSource">{{ compiling ? '正在编译并试跑样例…' : source.publish ? '上传、编译并发布' : '上传、编译并校验' }}</el-button></div>
        <small v-if="compiling" class="subline">首次编译可能较慢，请保持页面打开。每个平台编译最长 120 秒，随后运行样例测试。</small>
        <el-alert v-if="sourceError" class="top-gap" title="操作未完成，请查看原因" type="error" :closable="false"><pre class="source-error">{{ sourceError }}</pre></el-alert>
      </el-form>
    </el-tab-pane>
    <el-tab-pane label="网络接入" name="network">
      <el-alert title="配置设备连接平台或平台连接设备" description="先绑定主设备和子设备产品的协议。设备连接平台时配置本机监听端口；平台连接设备时配置设备地址，并按需添加定时查询。协议包负责握手、分帧、解析和应答。" type="info" :closable="false" show-icon />
      <el-form label-position="top" class="top-gap">
        <div class="form-grid">
          <el-form-item label="接入实例标识"><el-input v-model="listener.id" placeholder="例如 dahua-tcp" /></el-form-item>
          <el-form-item label="产品"><el-select v-model="listener.productId" filterable><el-option v-for="p in products" :key="p.id" :label="p.name" :value="p.id" /></el-select></el-form-item>
          <el-form-item label="协议"><el-select v-model="listener.protocolId" filterable @change="listener.protocolVersion = ''"><el-option v-for="p in protocols" :key="p.definition.id" :label="p.definition.name" :value="p.definition.id" /></el-select></el-form-item>
          <el-form-item label="产品当前绑定版本"><el-select v-model="listener.protocolVersion"><el-option v-for="release in listenerReleases" :key="release.version" :label="release.version" :value="release.version" /></el-select></el-form-item>
          <el-form-item label="网络"><el-select v-model="listener.network"><el-option label="长连接" value="tcp" /><el-option label="数据报" value="udp" /></el-select></el-form-item>
          <el-form-item v-if="listener.network==='tcp'" label="连接方向"><el-select v-model="listener.connectionMode"><el-option value="listen" label="设备连接平台"/><el-option value="dial" label="平台连接设备"/></el-select></el-form-item>
<el-form-item v-if="listener.connectionMode==='dial'" label="已配置的设备标识"><el-input v-model="listener.deviceId" /></el-form-item>
<el-form-item :label="listener.connectionMode==='dial'?'设备地址 / 主机名':'本机监听地址'"><el-input v-model="listener.host" /></el-form-item>
          <el-form-item label="端口"><el-input-number v-model="listener.port" :min="1" :max="65535" /></el-form-item>
          <el-form-item label="操作超时（毫秒）"><el-input-number v-model="listener.timeoutMs" :min="1" :max="30000" /></el-form-item>
        </div>
        <el-switch v-model="listener.autoRegister" active-text="自动登记协议识别的新设备" /><small class="subline">关闭时，只接收该产品下已登记且启用的设备。</small>
        <el-switch v-model="listener.enabled" active-text="启用接入" />
        <ProtocolAccessSettings :profile="listener" :can-poll="listener.network==='tcp'" :products="products" :product-id="listener.productId"/><div class="dialog-actions"><el-button type="primary" :loading="savingListener" @click="saveListener">保存接入实例</el-button></div>
      </el-form>
      <el-divider content-position="left">设备下行命令</el-divider>
      <el-form label-position="top"><div class="form-grid">
        <el-form-item label="接入实例"><el-select v-model="command.profileId"><el-option v-for="p in profiles.filter(p => p.mode === 'listener' && p.enabled)" :key="p.id" :label="p.id" :value="p.id" /></el-select></el-form-item>
        <el-form-item label="在线设备标识"><el-input v-model="command.deviceId" placeholder="由协议包识别的设备标识" /></el-form-item>
      </div><el-form-item label="协议包支持的命令结构化数据"><el-input v-model="command.body" type="textarea" :rows="3" /></el-form-item>
      <el-button type="primary" :loading="sendingCommand" @click="sendCommand">发送命令</el-button><el-button v-if="pendingProtocolCommand" @click="pendingProtocolCommand=null">开始一条新命令</el-button></el-form>
    </el-tab-pane>

    <el-tab-pane label="协议与版本">
      <el-form :model="binding" label-position="top">
        <div class="form-grid">
          <el-form-item label="产品"><el-select v-model="binding.productId" filterable placeholder="选择要切换的产品"><el-option v-for="p in products" :key="p.id" :label="`${p.name} · ${p.id}`" :value="p.id" /></el-select></el-form-item>
          <el-form-item label="协议"><el-select v-model="binding.protocolId" filterable @change="binding.version = ''"><el-option v-for="p in protocols" :key="p.definition.id" :label="p.definition.name" :value="p.definition.id" /></el-select></el-form-item>
          <el-form-item label="已发布版本"><el-select v-model="binding.version"><el-option v-for="release in publishedReleases" :key="release.version" :label="release.version" :value="release.version" /></el-select></el-form-item>
        </div>
        <div class="dialog-actions"><el-button :loading="switching" @click="switchBinding(true)">回滚产品至上一版本</el-button><el-button type="primary" :loading="switching" @click="switchBinding(false)">切换版本并立即生效</el-button></div>
      </el-form>
      <el-table v-loading="loading" :data="protocols" stripe>
        <el-table-column label="协议" min-width="230"><template #default="{ row }"><b>{{ row.definition.name }}</b><small class="subline">{{ row.definition.id }} · {{ row.definition.vendor || '通用' }}</small></template></el-table-column>
        <el-table-column label="最新版本" width="130"><template #default="{ row }">{{ newestRelease(row).version || '—' }}</template></el-table-column>
        <el-table-column label="运行方式" min-width="180"><template #default="{ row }">{{ transportLabel(newestRelease(row).transport) }} · {{ label(parsers, newestRelease(row).parserType, '自定义协议程序') }}</template></el-table-column>
        <el-table-column label="状态" width="110"><template #default="{ row }"><el-tag :type="statusType(newestRelease(row).status)" round>{{ statusText(newestRelease(row).status) }}</el-tag></template></el-table-column>
        <el-table-column label="版本历史" min-width="240"><template #default="{ row }"><span v-for="release in row.releases" :key="release.version" class="right-gap"><el-tag :type="statusType(release.status)" effect="plain">{{ release.version }} · {{ statusText(release.status) }}</el-tag><el-button v-if="release.status === 'VALIDATED'" plain type="primary" :loading="switching" @click="publishRelease(row.definition.id, release.version)">发布</el-button><el-button v-if="release.artifact?.build?.kind === 'go-source'" link @click="downloadRelease(row.definition.id, release, 'source')">下载源码</el-button><el-button v-if="release.artifact?.packagePath" link @click="downloadRelease(row.definition.id, release, 'package')">下载制品</el-button><small v-if="release.artifact?.platform" class="subline">{{ platformLabel(release.artifact.platform) }} · 发布端样例 {{ release.artifact.testCases || 0 }} 项</small><small v-for="(variant, platform) in (release.artifact?.variants || {})" :key="platform" class="subline">{{ platformLabel(platform) }} · {{ variant.validation === 'COMPILED' ? '已编译，待节点试跑' : '已上传，待节点试跑' }}</small></span></template></el-table-column>
      </el-table>
    </el-tab-pane>

    <el-tab-pane label="接入实例">
      <el-table v-loading="loading" :data="profiles" stripe row-key="id">
        <el-table-column type="expand"><template #default="{row}">
          <div class="instance-details"><h4>当前在线会话</h4>
          <el-table :data="snapshot(row.id).sessions" empty-text="暂无在线会话"><el-table-column label="设备"><template #default="{row:session}">{{session.deviceId || '尚未识别设备'}}</template></el-table-column><el-table-column prop="remoteAddress" label="远端地址"/><el-table-column prop="protocolId" label="协议"/><el-table-column prop="protocolVersion" label="版本"/><el-table-column label="最后有效报文"><template #default="{row:session}">{{formatTime(session.lastSeenAt)}}</template></el-table-column></el-table>
          <h4>最近接入设备（按创建时间，最多 20 台）</h4><el-table :data="snapshot(row.id).recentDevices" empty-text="暂无关联设备"><el-table-column prop="deviceId" label="设备标识"/><el-table-column prop="name" label="名称"/><el-table-column label="创建时间"><template #default="{row:device}">{{formatTime(device.createdAt)}}</template></el-table-column></el-table></div>
        </template></el-table-column>
        <el-table-column label="在线会话" width="100"><template #default="{row}">{{snapshot(row.id).sessions?.length || 0}}</template></el-table-column>
        <el-table-column label="设备" min-width="190"><template #default="{ row }"><b>{{ row.mode === 'listener' ? row.id : row.deviceId }}</b><small class="subline">{{ row.productId }}</small></template></el-table-column>
        <el-table-column label="协议版本" min-width="190"><template #default="{ row }">{{ row.protocolId }}@{{ row.protocolVersion }}</template></el-table-column>
        <el-table-column label="连接" min-width="170"><template #default="{ row }">{{ row.host }}:{{ row.port }} · {{ row.mode === 'listener' ? transportLabel(row.network) : `单元 ${row.unitId}` }}</template></el-table-column>
        <el-table-column label="状态" width="120"><template #default="{ row }"><el-tag :type="statusType(row.runtimeStatus)" round>{{ statusText(row.runtimeStatus) }}</el-tag></template></el-table-column>
        <el-table-column label="最近成功" min-width="170"><template #default="{ row }">{{ formatTime(row.lastSuccessAt) }}</template></el-table-column>
        <el-table-column label="最近错误" min-width="220" show-overflow-tooltip><template #default="{ row }">{{ row.lastError || '—' }}</template></el-table-column>
        <el-table-column label="操作" width="210" fixed="right"><template #default="{ row }"><el-button v-if="row.mode !== 'listener'" plain type="primary" :loading="testingId===row.id" @click="testProfile(row)">连接测试</el-button><el-button v-if="row.mode==='listener'" @click="editProfile(row)">编辑接入</el-button><el-button @click="toggleProfile(row)">{{ row.enabled ? '停用' : '启用' }}</el-button></template></el-table-column>
      </el-table>
    </el-tab-pane>
    <el-tab-pane label="组织发布" lazy><ProtocolMarket :protocols="protocols"/></el-tab-pane>
    <el-tab-pane label="协议目录" lazy><ProtocolCatalog :protocols="protocols" @installed="load"/></el-tab-pane>
  </el-tabs>

  <el-card v-if="result" shadow="never" class="surface-card top-gap">
    <template #header><strong>最近一次操作结果</strong></template>
    <pre>{{ pretty(result) }}</pre>
  </el-card>
</template>

<style scoped>
.instance-details { padding: 12px 24px; }
.source-error { white-space: pre-wrap; overflow-wrap: anywhere; max-height: 300px; overflow: auto; }
</style>
