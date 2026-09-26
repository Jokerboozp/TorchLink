<script setup>
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
const emit = defineEmits(['navigate'])
import AccessPointsPanel from '../components/AccessPointsPanel.vue'
import ProtocolAssistantView from './ProtocolAssistantView.vue'
import FilePicker from '../components/FilePicker.vue'
import { transportLabel, statusLabel, platformLabel } from '../presentation'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { label, parsers } from '../labels'
import { UiMessage } from '../ui/feedback.js'
import { api, download, formatTime, notifyError, pretty } from '../api'
import { confirmDelete } from '../deleteAction'
import { RefreshCw, Upload, Wand2 } from '@lucide/vue'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'
import { can } from '../permissions' /* 根据当前账号权限决定版本详情中的可用操作。 */

const protocols = ref([])
const protocolPage = ref(1), protocolPageSize = ref(20)
const pagedProtocols = computed(() => protocols.value.slice((protocolPage.value - 1) * protocolPageSize.value, protocolPage.value * protocolPageSize.value))
watch(() => protocols.value.length, total => { protocolPage.value = Math.min(protocolPage.value, Math.max(1, Math.ceil(total / protocolPageSize.value))) })
const loading = ref(false)
const result = ref(null)
const sourceFile = ref(null)
const targetPlatforms = ref([])
const compiling = ref(false)
const sourceError = ref('')
const sourceTemplate = ref(null)
const products = ref([])
const switching = ref(false)
const source = reactive({ protocolId:'', name:'', version:'', productId:'', transport:'', publish:true })
const props = defineProps({ section: { type: String, default: 'protocols' } })
const sourceOpen = ref(false), assistantOpen = ref(false)
const releaseOpen = ref(false), selectedProtocol = ref(null), selectedRelease = ref(null) /* 保存当前查看的协议及版本。 */
const versionsOpen = ref(false), managedProtocolId = ref('')
const managedProtocol = computed(() => protocols.value.find(item => item.definition.id === managedProtocolId.value) || null)
function manageVersions(row) { managedProtocolId.value = row.definition.id; versionsOpen.value = true }
function viewRelease(row, release) { versionsOpen.value = false; selectedProtocol.value = row.definition; selectedRelease.value = release; releaseOpen.value = true } /* 所有版本通过同一入口查看详情。 */
const hasReleaseActions = computed(() => { /* 仅在版本能力和账号权限都满足时显示专项操作。 */
  const release = selectedRelease.value /* 读取当前版本。 */
  return Boolean(release && ((release.artifact?.generatedMapping && can('POST /api/v2/protocols/:id/releases/:version/preview')) || (release.status === 'VALIDATED' && can('POST /api/v2/protocols/:id/releases/:version/publish')) || (release.artifact?.build?.kind === 'go-source' && can('GET /api/v2/protocols/:id/releases/:version/source')))) /* 返回可用操作状态。 */
}) /* 结束版本操作判断。 */
const assistantRelease = ref(null), assistantName = ref('')
function openAssistant(release = null, name = '') { releaseOpen.value=false;assistantRelease.value=release;assistantName.value=name;assistantOpen.value=true } /* 从版本详情进入解析测试时关闭原弹窗。 */
function assistantNavigate(page) { assistantOpen.value=false;emit('navigate',page) }
const releaseCount = computed(() => protocols.value.reduce((total, item) => total + (item.releases?.length || 0), 0))
let loadVersion = 0

async function loadProducts() {
  const items = []
  for (let page = 1; ; page += 1) {
    const result = await api(`/api/v1/products?page=${page}&pageSize=100`)
    items.push(...(result.items || []))
    if (!result.items?.length || items.length >= Number(result.total ?? result.count ?? items.length)) return items
  }
}

async function load() {
  const version = ++loadVersion
  loading.value = true
  try {
    const [catalog, productList, template] = await Promise.all([api('/api/v2/protocols'), loadProducts(), api('/api/v2/protocol-source-template')])
    if (version !== loadVersion) return
    protocols.value = catalog.items || []
    products.value = productList
    sourceTemplate.value = template
  } catch (error) { if (version === loadVersion) notifyError(error) } finally { if (version === loadVersion) loading.value = false }
}

function chooseSourceFile(event) { sourceFile.value = event.target.files?.[0] || null; sourceError.value = '' }
async function downloadSourceTemplate(kind = '') {
  try { await download(`/api/v2/protocol-source-template?format=go-functions&kind=${kind}`, kind === 'tcp' ? 'go-tcp-protocol.zip' : 'go-protocol.zip') }
  catch (error) { notifyError(error) }
}
async function uploadSource() {
  if (!sourceFile.value || !source.protocolId) return UiMessage.warning('请选择 Go 源码并填写协议标识')
  if (sourceFile.value.size > 32 * 1024 * 1024) return UiMessage.warning('源码文件不能超过 32 兆字节')
  compiling.value = true
  sourceError.value = ''
  result.value = null
  try {
    const body = new FormData()
    body.append('file', sourceFile.value)
    if (targetPlatforms.value.length) body.append('targetPlatforms', JSON.stringify(targetPlatforms.value))
    for (const [key, value] of Object.entries(source)) body.append(key, key === 'productId' && !source.publish ? '' : String(value))
    result.value = await api(`/api/v2/protocols/${encodeURIComponent(source.protocolId)}/source-releases`, { method:'POST', body })
    UiMessage.success(result.value.binding ? '编译与样例测试通过，产品已切换到新版本' : source.publish ? '编译与样例测试通过，协议已发布，可绑定产品使用' : '编译与样例测试通过，已保存校验版本')
    sourceOpen.value = false
    await load()
  } catch (error) { sourceError.value = error?.message || String(error) }
  finally { compiling.value = false }
}
async function publishRelease(protocolId, version) {
  switching.value = true
  try {
    await api(`/api/v2/protocols/${encodeURIComponent(protocolId)}/releases/${encodeURIComponent(version)}/publish`, { method:'POST', body:'{}' })
    UiMessage.success('版本已发布，可绑定产品使用')
    releaseOpen.value = false /* 发布后关闭旧状态的版本详情。 */
    await load()
  } catch (error) { notifyError(error) } finally { switching.value = false }
}
async function downloadSourceRelease(protocolId, release) {
  try { await download(`/api/v2/protocols/${encodeURIComponent(protocolId)}/releases/${encodeURIComponent(release.version)}/source`, `${protocolId}-${release.version}-source.${release.artifact?.filename?.toLowerCase().endsWith('.go') ? 'go' : 'zip'}`) }
  catch (error) { notifyError(error) }
}
function newestRelease(item) { return item.releases?.[0] || {} }
function statusText(value) { return ({ LISTENING:'监听中', DISABLED:'已停用', DRAFT:'草稿', VALIDATED:'已校验', PUBLISHED:'已发布', DEPRECATED:'已弃用', REVOKED:'已撤销', PENDING:'待启动', ONLINE:'在线采集', ERROR:'采集异常' })[value] || statusLabel(value) }
function statusType(value) { return ({ PUBLISHED:'success', ONLINE:'success', ERROR:'danger', REVOKED:'danger', VALIDATED:'warning', PENDING:'info' })[value] || 'info' }

onMounted(() => { if (props.section !== 'profiles') load() })
function removeProtocol(row) { return confirmDelete({ label:row.definition.name || row.definition.id, path:`/api/v2/protocols/${encodeURIComponent(row.definition.id)}`, onDeleted:load, warning:'未被引用的版本将一并删除，删除后无法恢复。', blockedHint:'协议仍被设备模板或接入点引用，请先解除绑定。' }) }
function removeRelease(row, release) { return confirmDelete({ label:`${row.definition.name || row.definition.id} · ${release.version}`, path:`/api/v2/protocols/${encodeURIComponent(row.definition.id)}/releases/${encodeURIComponent(release.version)}`, onDeleted:load, warning:'仅删除此版本及其独有制品，删除后无法恢复。', blockedHint:'此版本仍被设备模板、回滚记录或接入点引用，请先切换关联版本。' }) }
function protocolActions(row) {
  return [
    { key:'versions', label:'管理版本', onClick:() => manageVersions(row) },
    { key:'delete', label:'删除协议', type:'danger', permission:'DELETE /api/v2/protocols/:id', onClick:() => removeProtocol(row) }
  ]
}
</script>

<template>
  <AccessPointsPanel v-if="props.section === 'profiles'" />
  <template v-else>
  <FilterBar>
    <template #actions>
      <ui-button :loading="loading" @click="load"><RefreshCw />刷新</ui-button>
      <ui-button v-permission="'POST /api/v1/ai/protocol-assistant/generate'" title="通过报文或 Excel / CSV 点表生成协议" @click="openAssistant()"><Wand2 />协议生成</ui-button>
      <ui-button v-permission="'POST /api/v2/protocols/:id/source-releases'" type="primary" @click="sourceOpen=true"><Upload />上传源码</ui-button>
    </template>
  </FilterBar>
  <DataTableCard :title="`设备通信协议 · ${protocols.length} 个协议 · ${releaseCount} 个版本`" :page="protocolPage" :page-size="protocolPageSize" :page-sizes="[10,20,50,100]" :total="protocols.length" @update:page="value => protocolPage = value" @update:page-size="value => { protocolPageSize = value; protocolPage = 1 }">
    <ui-table :data="pagedProtocols" :loading="loading" empty-text="暂无协议，可上传 Go 源码或用报文、点表生成">
      <ui-table-column label="协议" min-width="230"><template #default="{ row }"><b>{{ row.definition.name }}</b><small class="subline">{{ row.definition.id }} · {{ row.definition.vendor || '通用' }}</small></template></ui-table-column>
      <ui-table-column label="最新版本" width="170" show-overflow-tooltip><template #default="{ row }">{{ newestRelease(row).version || '—' }}</template></ui-table-column>
      <ui-table-column label="运行方式" min-width="200"><template #default="{ row }">{{ transportLabel(newestRelease(row).transport) }} · {{ label(parsers, newestRelease(row).parserType, '自定义协议程序') }}</template></ui-table-column>
      <ui-table-column label="状态" width="110"><template #default="{ row }"><StatusDot :tone="statusType(newestRelease(row).status) === 'success' ? 'success' : statusType(newestRelease(row).status) === 'danger' ? 'danger' : statusType(newestRelease(row).status) === 'warning' ? 'warning' : 'neutral'" :label="statusText(newestRelease(row).status)" /></template></ui-table-column>
      <ui-table-column label="版本数量" width="100"><template #default="{ row }">{{ row.releases?.length || 0 }}</template></ui-table-column>
      <ui-table-column label="操作" fixed="right" width="176" align="right"><template #default="{ row }"><RowActions :actions="protocolActions(row)" /></template></ui-table-column>
    </ui-table>
  </DataTableCard>
  <ui-dialog v-model="versionsOpen" class="protocol-versions-dialog" :title="`${managedProtocol?.definition.name || '协议'} · 版本管理`" width="min(880px, 96vw)" destroy-on-close>
    <template v-if="managedProtocol">
      <p class="versions-summary">{{ managedProtocol.definition.id }} · 共 {{ managedProtocol.releases?.length || 0 }} 个版本。删除前请确认该版本未被设备模板或接入点引用。</p>
      <ui-table :data="managedProtocol.releases || []" stripe empty-text="暂无版本，可上传源码创建新版本">
        <ui-table-column label="版本" min-width="130"><template #default="{ row }"><strong>{{ row.version }}</strong></template></ui-table-column>
        <ui-table-column label="状态" width="110"><template #default="{ row }"><ui-tag :type="statusType(row.status)" round>{{ statusText(row.status) }}</ui-tag></template></ui-table-column>
        <ui-table-column label="运行方式" min-width="185"><template #default="{ row }">{{ transportLabel(row.transport) }} · {{ label(parsers, row.parserType, '自定义协议程序') }}</template></ui-table-column>
        <ui-table-column label="创建时间" min-width="170"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></ui-table-column>
        <ui-table-column label="操作" width="120" align="right"><template #default="{ row }"><RowActions :actions="[{ key:'detail', label:'详情', onClick:() => viewRelease(managedProtocol, row) }, { key:'delete', label:'删除', type:'danger', permission:'DELETE /api/v2/protocols/:id/releases/:version', onClick:() => removeRelease(managedProtocol, row) }]" /></template></ui-table-column>
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
  <details v-if="result" class="technical-details"><summary>最近操作结果</summary><pre>{{ pretty(result) }}</pre></details>
  </template>
</template>
<style scoped>
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
@media (max-width: 767px) {
  .source-template-actions,
  .source-template-actions button,
  .source-submit-row,
  .source-submit-row button { width: 100%; }
  .source-submit-row { flex-wrap: wrap; }
}
</style>
