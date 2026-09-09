<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, download, formatTime, notifyError, pretty } from '../api'

const protocols = ref([])
const profiles = ref([])
const snapshots = ref({})
const snapshot = (id) => snapshots.value[id] || { sessions: [], recentDevices: [] }
const loading = ref(false)
const testingId = ref('')
const result = ref(null)
const sourceFile = ref(null)
const compiling = ref(false)
const sourceError = ref('')
const sourceTemplate = ref(null)
const products = ref([])
const switching = ref(false)
const source = reactive({ protocolId:'', name:'', version:'', productId:'', runtime:'', capabilities:'', transport:'', payloadFormat:'', entrypoint:'', publish:true, cases:'' })
const binding = reactive({ productId:'', protocolId:'', version:'' })
const publishedReleases = computed(() => protocols.value.find(item => item.definition.id === binding.protocolId)?.releases?.filter(item => item.status === 'PUBLISHED') || [])

const listener = reactive({ id:'', productId:'', protocolId:'', protocolVersion:'', mode:'listener', network:'tcp', host:'0.0.0.0', port:26875, timeoutMs:5000, autoRegister:false, enabled:true })
const savingListener = ref(false)
const listenerReleases = computed(() => protocols.value.find(item => item.definition.id === listener.protocolId)?.releases?.filter(item => item.status === 'PUBLISHED' && item.artifact?.runtime === 'go-protocol-v2' && item.capabilities?.includes('ingress')) || [])
const command = reactive({ profileId:'', deviceId:'', body:'{"type":"time-sync"}' })
const sendingCommand = ref(false)
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
function downloadSourceTemplate() {
  if (!sourceTemplate.value) return
  if (!source.cases.trim()) source.cases = JSON.stringify(sourceTemplate.value.cases, null, 2)
  if (!source.version) source.version = '1.0.0'
  const url = URL.createObjectURL(new Blob([sourceTemplate.value.source], { type:'text/plain;charset=utf-8' }))
  const anchor = document.createElement('a'); anchor.href = url; anchor.download = sourceTemplate.value.filename; anchor.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
async function uploadSource() {
  if (!sourceFile.value || !source.protocolId) return ElMessage.warning('请选择 Go 源码并填写协议标识；版本可由 protocol.json 提供')
  if (sourceFile.value.size > 32 * 1024 * 1024) return ElMessage.warning('源码文件不能超过 32 MiB')
  if (source.cases.trim()) {
    try { const cases = JSON.parse(source.cases); if (!Array.isArray(cases) || !cases.length) throw new Error() }
    catch { return ElMessage.warning('样例测试须为非空 JSON 数组') }
  }
  compiling.value = true
  sourceError.value = ''
  result.value = null
  try {
    const body = new FormData()
    body.append('file', sourceFile.value)
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
  if (!listener.id || !listener.productId || !listener.protocolId || !listener.protocolVersion) return ElMessage.warning('请填写实例标识、产品及其当前绑定的 Go 协议版本')
  savingListener.value = true
  try {
    result.value = await api('/api/v2/device-access-profiles', { method:'POST', body:JSON.stringify(listener) })
    ElMessage.success('接入实例已保存，启用后约一秒内开始监听')
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
  catch { return ElMessage.warning('命令须为包含 type 的 JSON 对象') }
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
function statusText(value) { return ({ LISTENING:'监听中', DISABLED:'已停用', DRAFT:'草稿', VALIDATED:'已校验', PUBLISHED:'已发布', DEPRECATED:'已弃用', REVOKED:'已撤销', PENDING:'待启动', ONLINE:'在线采集', ERROR:'采集异常' })[value] || value || '—' }
function statusType(value) { return ({ PUBLISHED:'success', ONLINE:'success', ERROR:'danger', REVOKED:'danger', VALIDATED:'warning', PENDING:'info' })[value] || 'info' }

onMounted(load)
</script>

<template>
  <div class="page-toolbar">
    <el-button :loading="loading" @click="load">刷新</el-button>
    <el-tag type="success" round>协议运行时 v2</el-tag>
    <span>{{ protocols.length }} 个协议，{{ releaseCount }} 个不可变版本，{{ profiles.length }} 个设备接入实例</span>
  </div>

  <el-tabs type="border-card">
    <el-tab-pane label="Go 源码接入">
      <el-alert title="上传 Go 源码，接入自定义协议" description="平台自动编译并试跑样例；通过后发布，绑定产品的新报文立即使用，无需重启。支持单个 .go 文件和完整 Go 项目 ZIP。" type="info" :closable="false" show-icon />
      <el-alert v-if="sourceTemplate && !sourceTemplate.compilerAvailable" class="top-gap" title="当前服务尚未安装 Go 编译器，请先部署包含源码编译功能的 API 镜像。" type="warning" :closable="false" />
      <el-form :model="source" label-position="top" class="top-gap">
        <div class="form-grid">
          <el-form-item label="协议标识"><el-input v-model="source.protocolId" placeholder="例如 vendor-fire" /></el-form-item>
          <el-form-item label="协议名称"><el-input v-model="source.name" placeholder="例如 消防设备协议" /></el-form-item>
          <el-form-item label="版本"><el-input v-model="source.version" placeholder="留空读取 protocol.json，更新时使用新版本号" /></el-form-item>
          <el-form-item label="绑定产品（可选）"><el-select v-model="source.productId" filterable clearable :disabled="!source.publish" placeholder="选择后，发布成功立即切换"><el-option v-for="p in products" :key="p.id" :label="`${p.name} · ${p.id}`" :value="p.id" /></el-select></el-form-item>
          <el-form-item label="设备上报通道"><el-select v-model="source.transport" clearable placeholder="留空读取协议包"><el-option v-for="value in ['MQTT','HTTP','TCP','UDP','TCP_UDP']" :key="value" :label="value" :value="value" /></el-select></el-form-item>
          <el-form-item label="报文格式"><el-select v-model="source.payloadFormat" clearable placeholder="留空读取协议包"><el-option v-for="value in ['hex','json','text','base64']" :key="value" :label="value" :value="value" /></el-select></el-form-item>
        </div>
        <div class="form-grid">
          <el-form-item label="协议能力"><el-select v-model="source.runtime" clearable placeholder="留空读取协议包"><el-option label="报文解析（v1）" value="go-json-lines-v1" /><el-option label="完整接入（v2）" value="go-protocol-v2" /></el-select></el-form-item>
          <el-form-item label="操作能力（可留空读取协议包）"><el-input v-model="source.capabilities" placeholder='["decode","ingress","encode"]' /></el-form-item>
        </div>
        <el-form-item label="Go 源码文件或项目 ZIP">
          <input type="file" accept=".go,.zip" :disabled="compiling" @change="chooseSourceFile" />
          <el-button plain class="left-gap" :disabled="!sourceTemplate" @click="downloadSourceTemplate">下载完整 Go 模板</el-button>
          <small class="subline">修改模板里的 Decode 即可。使用普通 Go 语法、标准库及项目内的包；第三方依赖请先 go mod vendor 后随项目上传。最大 32 MiB。</small>
        </el-form-item>
        <el-form-item label="项目编译入口"><el-input v-model="source.entrypoint" placeholder="默认为 .，多目录项目可填 cmd/worker" /><small class="subline">ZIP 根目录放 go.mod、protocol.json 和源码；未填写的字段自动读取包内元数据。单文件默认入口为 .。</small></el-form-item>
        <el-form-item label="样例报文与预期解析结果">
          <el-input v-model="source.cases" type="textarea" :rows="7" spellcheck="false" />
          <small class="subline">请改成该协议的实际样例。支持多条测试；ZIP 内含 samples/cases.json 时可以清空此处。任何一条失败都会阻止发布。</small>
        </el-form-item>
        <el-switch v-model="source.publish" active-text="测试通过后立即发布" inactive-text="仅保存已校验版本" />
        <div class="dialog-actions"><el-button type="primary" :loading="compiling" :disabled="sourceTemplate && !sourceTemplate.compilerAvailable" @click="uploadSource">{{ compiling ? '正在编译并试跑样例…' : source.publish ? '上传、编译并发布' : '上传、编译并校验' }}</el-button></div>
        <small v-if="compiling" class="subline">首次编译可能较慢，请保持页面打开。编译最长 120 秒，随后运行样例测试。</small>
        <el-alert v-if="sourceError" class="top-gap" title="操作未完成，请查看原因" type="error" :closable="false"><pre class="source-error">{{ sourceError }}</pre></el-alert>
      </el-form>
    </el-tab-pane>
    <el-tab-pane label="TCP / UDP 接入">
      <el-alert title="上传完整 Go 协议包后，在这里启用设备监听端口" description="先在“协议与版本”绑定产品。Go 包负责分帧、识别设备、解析和应答；切换产品版本后，连接在完整帧及待应答命令结束后使用新版本。容器部署请使用已映射的端口。" type="info" :closable="false" show-icon />
      <el-form label-position="top" class="top-gap">
        <div class="form-grid">
          <el-form-item label="接入实例标识"><el-input v-model="listener.id" placeholder="例如 dahua-tcp" /></el-form-item>
          <el-form-item label="产品"><el-select v-model="listener.productId" filterable><el-option v-for="p in products" :key="p.id" :label="p.name" :value="p.id" /></el-select></el-form-item>
          <el-form-item label="协议"><el-select v-model="listener.protocolId" filterable @change="listener.protocolVersion = ''"><el-option v-for="p in protocols" :key="p.definition.id" :label="p.definition.name" :value="p.definition.id" /></el-select></el-form-item>
          <el-form-item label="产品当前绑定版本"><el-select v-model="listener.protocolVersion"><el-option v-for="release in listenerReleases" :key="release.version" :label="release.version" :value="release.version" /></el-select></el-form-item>
          <el-form-item label="网络"><el-select v-model="listener.network"><el-option label="TCP" value="tcp" /><el-option label="UDP" value="udp" /></el-select></el-form-item>
          <el-form-item label="本机监听 IP"><el-input v-model="listener.host" /></el-form-item>
          <el-form-item label="端口"><el-input-number v-model="listener.port" :min="1" :max="65535" /></el-form-item>
          <el-form-item label="操作超时（毫秒）"><el-input-number v-model="listener.timeoutMs" :min="1" :max="30000" /></el-form-item>
        </div>
        <el-switch v-model="listener.autoRegister" active-text="自动登记协议识别的新设备" /><small class="subline">关闭时，只接收该产品下已登记且启用的设备。</small>
        <el-switch v-model="listener.enabled" active-text="启用监听" />
        <div class="dialog-actions"><el-button type="primary" :loading="savingListener" @click="saveListener">保存接入实例</el-button></div>
      </el-form>
      <el-divider content-position="left">设备下行命令</el-divider>
      <el-form label-position="top"><div class="form-grid">
        <el-form-item label="接入实例"><el-select v-model="command.profileId"><el-option v-for="p in profiles.filter(p => p.mode === 'listener' && p.enabled)" :key="p.id" :label="p.id" :value="p.id" /></el-select></el-form-item>
        <el-form-item label="在线设备标识"><el-input v-model="command.deviceId" placeholder="由协议包识别的设备 ID" /></el-form-item>
      </div><el-form-item label="协议包支持的命令 JSON"><el-input v-model="command.body" type="textarea" :rows="3" /></el-form-item>
      <el-button type="primary" :loading="sendingCommand" @click="sendCommand">发送命令并等待应答</el-button></el-form>
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
        <el-table-column label="运行方式" min-width="180"><template #default="{ row }">{{ newestRelease(row).transport || '—' }} · {{ newestRelease(row).parserType || '—' }}</template></el-table-column>
        <el-table-column label="状态" width="110"><template #default="{ row }"><el-tag :type="statusType(newestRelease(row).status)" round>{{ statusText(newestRelease(row).status) }}</el-tag></template></el-table-column>
        <el-table-column label="版本历史" min-width="240"><template #default="{ row }"><span v-for="release in row.releases" :key="release.version" class="right-gap"><el-tag :type="statusType(release.status)" effect="plain">{{ release.version }} · {{ statusText(release.status) }}</el-tag><el-button v-if="release.status === 'VALIDATED'" plain type="primary" :loading="switching" @click="publishRelease(row.definition.id, release.version)">发布</el-button><el-button v-if="release.artifact?.build?.kind === 'go-source'" link @click="downloadRelease(row.definition.id, release, 'source')">下载源码</el-button><el-button v-if="release.artifact?.packagePath" link @click="downloadRelease(row.definition.id, release, 'package')">下载制品</el-button></span></template></el-table-column>
      </el-table>
    </el-tab-pane>

    <el-tab-pane label="设备接入实例">
      <el-table v-loading="loading" :data="profiles" stripe row-key="id">
        <el-table-column type="expand"><template #default="{row}">
          <div class="instance-details"><h4>当前在线会话</h4>
          <el-table :data="snapshot(row.id).sessions" empty-text="暂无在线会话"><el-table-column label="设备"><template #default="{row:session}">{{session.deviceId || '尚未识别设备'}}</template></el-table-column><el-table-column prop="remoteAddress" label="远端地址"/><el-table-column prop="protocolId" label="协议"/><el-table-column prop="protocolVersion" label="版本"/><el-table-column label="最后有效报文"><template #default="{row:session}">{{formatTime(session.lastSeenAt)}}</template></el-table-column></el-table>
          <h4>最近接入设备（按创建时间，最多 20 台）</h4><el-table :data="snapshot(row.id).recentDevices" empty-text="暂无关联设备"><el-table-column prop="deviceId" label="设备 ID"/><el-table-column prop="name" label="名称"/><el-table-column label="创建时间"><template #default="{row:device}">{{formatTime(device.createdAt)}}</template></el-table-column></el-table></div>
        </template></el-table-column>
        <el-table-column label="在线会话" width="100"><template #default="{row}">{{snapshot(row.id).sessions?.length || 0}}</template></el-table-column>
        <el-table-column label="设备" min-width="190"><template #default="{ row }"><b>{{ row.mode === 'listener' ? row.id : row.deviceId }}</b><small class="subline">{{ row.productId }}</small></template></el-table-column>
        <el-table-column label="协议版本" min-width="190"><template #default="{ row }">{{ row.protocolId }}@{{ row.protocolVersion }}</template></el-table-column>
        <el-table-column label="连接" min-width="170"><template #default="{ row }">{{ row.host }}:{{ row.port }} · {{ row.mode === 'listener' ? row.network?.toUpperCase() : `Unit ${row.unitId}` }}</template></el-table-column>
        <el-table-column label="状态" width="120"><template #default="{ row }"><el-tag :type="statusType(row.runtimeStatus)" round>{{ statusText(row.runtimeStatus) }}</el-tag></template></el-table-column>
        <el-table-column label="最近成功" min-width="170"><template #default="{ row }">{{ formatTime(row.lastSuccessAt) }}</template></el-table-column>
        <el-table-column label="最近错误" min-width="220" show-overflow-tooltip><template #default="{ row }">{{ row.lastError || '—' }}</template></el-table-column>
        <el-table-column label="操作" width="210" fixed="right"><template #default="{ row }"><el-button v-if="row.mode !== 'listener'" plain type="primary" :loading="testingId===row.id" @click="testProfile(row)">连接测试</el-button><el-button @click="toggleProfile(row)">{{ row.enabled ? '停用' : '启用' }}</el-button></template></el-table-column>
      </el-table>
    </el-tab-pane>
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
