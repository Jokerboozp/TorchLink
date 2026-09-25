<script setup>
import { createClientId } from '../clientId' /* 引入当前代码需要的依赖。 */
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate']) /* 执行当前语句并推进处理流程。 */
import AccessPointsPanel from '../components/AccessPointsPanel.vue'
import ProductProtocolBinding from '../components/ProductProtocolBinding.vue' /* 引入当前代码需要的依赖。 */
import { transportLabel, formatLabel } from '../presentation' /* 引入当前代码需要的依赖。 */
import { onMounted, reactive, ref } from 'vue' /* 引入当前代码需要的依赖。 */
import { UiMessage } from '../ui/feedback.js' /* 引入当前代码需要的依赖。 */
import { api, apiAll, notifyError } from '../api' /* 引入当前代码需要的依赖。 */
import { confirmDelete } from '../deleteAction'
import { categories, enabledStatuses, enabledStatusTones, label, tone } from '../labels'
import { can } from '../permissions'
import { Plus, RefreshCw } from '@lucide/vue'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'

// 模板详情抽屉：基本信息、协议版本和接入点。
const detail = ref(null), detailTab = ref('basic'), detailColumns = ref(2)
const products = ref([]) /* 声明 products。 */
const protocols = ref([]) /* 声明 protocols。 */
const saving = ref(false) /* 声明 saving。 */
const loading = ref(false) /* 声明 loading。 */
const dialog = ref(false) /* 声明 dialog。 */
const productPage = ref(1) /* 声明 productPage。 */
const productPageSize = ref(20) /* 声明 productPageSize。 */
const productTotal = ref(0) /* 声明 productTotal。 */

const blank = () => ({ id:'', code:'', name:'', category:'smoke', protocolPackageId:'iot-standard@1.0.0', transport:'MQTT', payloadFormat:'json', status:'ENABLED', description:'', thingModel:null, metadata:{manufacturer:'',model:'',idKind:'',idLocation:''} }) /* 声明 blank。 */
const form = reactive(blank()) /* 声明 form。 */

let loadVersion = 0 /* 声明 loadVersion。 */
let catalogVersion = 0 /* 声明 catalogVersion。 */
async function load({ catalog = true } = {}) { /* 定义 load 函数。 */
  const version = ++loadVersion /* 声明 version。 */
  const currentCatalog = catalog ? ++catalogVersion : 0 /* 声明 currentCatalog。 */
  loading.value = true /* 更新 loading.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const [p, pk] = await Promise.all([ /* 执行当前语句并推进处理流程。 */
      api(`/api/v1/products?page=${productPage.value}&pageSize=${productPageSize.value}`), /* 执行当前语句并推进处理流程。 */
      catalog ? Promise.all([apiAll('/api/v1/protocol-packages'), api('/api/v2/protocols')]) : null /* 执行当前语句并推进处理流程。 */
    ]) /* 结束当前表达式或代码块。 */
    if (pk && currentCatalog === catalogVersion) protocols.value = [...new Map([{ id:'iot-standard@1.0.0', name:'标准设备上报', transport:'MQTT_HTTP', payloadFormat:'json' }, ...(pk[0].items || []).filter(p => p.status === 'PUBLISHED'), ...(pk[1].items || []).flatMap(p => (p.releases || []).filter(r => r.status === 'PUBLISHED').map(r => ({ id:`${p.definition.id}@${r.version}`, name:`${p.definition.name} · ${r.version}`, transport:r.transport, payloadFormat:r.payloadFormat })))].map(p => [p.id, p])).values()] /* 判断条件并选择处理分支。 */
    if (version !== loadVersion) return /* 判断条件并选择处理分支。 */
    products.value = p.items || [] /* 更新 products.value 的值。 */
    productTotal.value = Number(p.total ?? p.count ?? products.value.length) /* 更新 productTotal.value 的值。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    if (version === loadVersion) notifyError(error) /* 判断条件并选择处理分支。 */
  } finally { /* 结束当前表达式或代码块。 */
    if (version === loadVersion) loading.value = false /* 判断条件并选择处理分支。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function changePage(value) { /* 定义 changePage 函数。 */
  productPage.value = value /* 更新 productPage.value 的值。 */
  load({ catalog:false }) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

function changePageSize(value) { /* 定义 changePageSize 函数。 */
  productPageSize.value = value /* 更新 productPageSize.value 的值。 */
  productPage.value = 1 /* 更新 productPage.value 的值。 */
  load({ catalog:false }) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

function reset() { /* 定义 reset 函数。 */
  Object.assign(form, blank()) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

function openCreate() { /* 定义 openCreate 函数。 */
  reset() /* 执行当前语句并推进处理流程。 */
  dialog.value = true /* 更新 dialog.value 的值。 */
} /* 结束当前表达式或代码块。 */

function openDetail(item, tab = 'basic') { detailColumns.value = window.innerWidth < 768 ? 1 : 2; detail.value = item; detailTab.value = tab }

function edit(item) { /* 定义 edit 函数。 */
  Object.assign(form, { ...blank(), ...item, metadata:{...blank().metadata,...item.metadata}, code:item.id }) /* 执行当前语句并推进处理流程。 */
  dialog.value = true /* 更新 dialog.value 的值。 */
} /* 结束当前表达式或代码块。 */

// 协议切换会改写模板的协议引用；刷新列表后同步详情中的模板。
async function refreshDetail() {
  await load({ catalog:false })
  const id = detail.value?.id
  if (!id) return
  detail.value = products.value.find(item => item.id === id) || (await apiAll('/api/v1/products')).items?.find(item => item.id === id) || detail.value
}

async function save() { /* 定义 save 函数。 */
  if (saving.value) return /* 判断条件并选择处理分支。 */
  if (!form.name.trim() || !form.protocolPackageId) return UiMessage.warning('请填写设备模板名称并选择已发布的通信协议')
  saving.value = true /* 更新 saving.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    if (!form.id && !form.code) form.code = `product_${createClientId().replaceAll('-', '').slice(0, 12)}` /* 判断条件并选择处理分支。 */
    // 编辑页不暴露物模型 JSON，但提交时保留已加载的模型，避免意外清空命令定义。
    const value = { ...form, id:form.id || form.code } /* 声明 value。 */
    delete value.code /* 执行当前语句并推进处理流程。 */
    const editing = Boolean(form.id) /* 声明 editing。 */
    await api(editing ? `/api/v1/products/${encodeURIComponent(value.id)}` : '/api/v1/products', { /* 等待异步操作完成。 */
      method: editing ? 'PUT' : 'POST', /* 执行当前语句并推进处理流程。 */
      body: JSON.stringify(value) /* 执行当前语句并推进处理流程。 */
    }) /* 结束当前表达式或代码块。 */
    UiMessage.success('设备模板已保存')
    dialog.value = false /* 更新 dialog.value 的值。 */
    reset() /* 执行当前语句并推进处理流程。 */
    await load() /* 等待异步操作完成。 */
    if (detail.value?.id === value.id) detail.value = products.value.find(item => item.id === value.id) || { ...detail.value, ...value }
  } catch (error) { /* 结束当前表达式或代码块。 */
    notifyError(error) /* 执行当前语句并推进处理流程。 */
  } finally { /* 结束当前表达式或代码块。 */
    saving.value = false /* 更新 saving.value 的值。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

onMounted(async () => {
  let navigation = {}
  try { navigation = JSON.parse(sessionStorage.getItem('iot:navigation-detail') || '{}') } catch { navigation = {} }
  sessionStorage.removeItem('iot:navigation-detail')
  await load()
  if (navigation.productId) {
    const item = products.value.find(p => p.id === navigation.productId) || (await apiAll('/api/v1/products').catch(() => ({ items:[] }))).items?.find(p => p.id === navigation.productId)
    if (item) openDetail(item, navigation.tab || 'basic')
  }
})
function remove(row) { return confirmDelete({ label:row.name || row.id, path:`/api/v1/products/${encodeURIComponent(row.id)}`, onDeleted:load }) }
function protocolName(id) { return protocols.value.find(item => item.id === id)?.name || id || '未绑定' }
function rowActions(row) {
  return [
    { key:'view', label:'详情', onClick:() => openDetail(row) },
    { key:'access', label:'接入点', permission:'menu:profiles', onClick:() => openDetail(row, 'access') },
    { key:'binding', label:'协议版本', onClick:() => openDetail(row, 'protocol') },
    { key:'edit', label:'编辑', permission:'PUT /api/v1/products/:id', onClick:() => edit(row) },
    { key:'delete', label:'删除', type:'danger', permission:'DELETE /api/v1/products/:id', onClick:() => remove(row) }
  ]
}
function metadataText(item, keys) { return keys.map(key => item?.metadata?.[key]).filter(Boolean).join(' · ') || '—' }
</script>

<template>
  <FilterBar>
    <template #actions>
      <ui-button :loading="loading" @click="load"><RefreshCw />刷新</ui-button>
      <ui-button v-permission="'POST /api/v1/products'" type="primary" @click="openCreate"><Plus />新建设备模板</ui-button>
    </template>
  </FilterBar>

  <DataTableCard :title="`设备模板 · ${productTotal} 个`" :page="productPage" :page-size="productPageSize" :total="productTotal" @update:page="changePage" @update:page-size="changePageSize">
    <ui-table :data="products" :loading="loading" empty-text="暂无设备模板，点击“新建设备模板”创建">
      <ui-table-column label="设备模板" min-width="220"><template #default="{ row }"><button type="button" class="product-name" @click="openDetail(row)">{{ row.name || row.id }}</button><small class="subline">{{ row.id }}</small></template></ui-table-column>
      <ui-table-column label="分类" min-width="120"><template #default="{ row }">{{ label(categories, row.category, '其他设备') }}</template></ui-table-column>
      <ui-table-column label="通信协议" min-width="220"><template #default="{ row }">{{ protocolName(row.protocolPackageId) }}<small class="subline">{{ transportLabel(row.transport) }} · {{ formatLabel(row.payloadFormat) }}</small></template></ui-table-column>
      <ui-table-column label="厂商 / 型号" min-width="160"><template #default="{ row }">{{ [row.metadata?.manufacturer, row.metadata?.model].filter(Boolean).join(' · ') || '—' }}</template></ui-table-column>
      <ui-table-column label="状态" width="100"><template #default="{ row }"><StatusDot :tone="tone(enabledStatusTones, row.status)" :label="label(enabledStatuses, row.status)" /></template></ui-table-column>
      <ui-table-column label="说明" min-width="200" show-overflow-tooltip><template #default="{ row }">{{ row.description || '—' }}</template></ui-table-column>
      <ui-table-column label="操作" width="176" fixed="right" align="right"><template #default="{ row }"><RowActions :actions="rowActions(row)" /></template></ui-table-column>
    </ui-table>
  </DataTableCard>

  <ui-drawer :model-value="Boolean(detail)" class="product-detail" :title="detail ? `设备模板 · ${detail.name || detail.id}` : ''" size="min(1040px, 100vw)" @close="detail=null">
    <ui-tabs v-if="detail" v-model="detailTab" class="product-detail__tabs">
      <ui-tab-pane name="basic" label="基本信息">
        <div class="product-detail__head">
          <StatusDot :tone="tone(enabledStatusTones, detail.status)" :label="label(enabledStatuses, detail.status)" />
          <ui-button v-permission="'PUT /api/v1/products/:id'" size="small" @click="edit(detail)">编辑模板</ui-button>
        </div>
        <ui-descriptions :column="detailColumns" border>
          <ui-descriptions-item label="模板名称">{{ detail.name }}</ui-descriptions-item>
          <ui-descriptions-item label="模板标识"><code>{{ detail.id }}</code></ui-descriptions-item>
          <ui-descriptions-item label="设备分类">{{ label(categories, detail.category, '其他设备') }}</ui-descriptions-item>
          <ui-descriptions-item label="通信协议">{{ protocolName(detail.protocolPackageId) }}</ui-descriptions-item>
          <ui-descriptions-item label="上报通道">{{ transportLabel(detail.transport) }}</ui-descriptions-item>
          <ui-descriptions-item label="数据格式">{{ formatLabel(detail.payloadFormat) }}</ui-descriptions-item>
          <ui-descriptions-item label="厂商 / 型号">{{ metadataText(detail, ['manufacturer','model']) }}</ui-descriptions-item>
          <ui-descriptions-item label="编号类型 / 位置">{{ metadataText(detail, ['idKind','idLocation']) }}</ui-descriptions-item>
          <ui-descriptions-item label="说明" :span="detailColumns">{{ detail.description || '—' }}</ui-descriptions-item>
        </ui-descriptions>
      </ui-tab-pane>
      <ui-tab-pane name="protocol" label="协议版本"><ProductProtocolBinding :key="detail.id" :product="detail" @saved="refreshDetail" /></ui-tab-pane>
      <ui-tab-pane v-if="can('menu:profiles')" name="access" label="接入点"><AccessPointsPanel :key="detail.id" :product-id="detail.id" /></ui-tab-pane>
    </ui-tabs>
  </ui-drawer>

  <ui-dialog v-model="dialog" :title="form.id ? `编辑设备模板 · ${form.name}` : '新建设备模板'" width="min(720px, 94vw)" destroy-on-close>
    <ui-form :model="form" label-position="top">
      <section class="editor-section">
        <header><h3>模板身份</h3><p>名称用于页面识别；模板标识创建后不可修改。</p></header>
        <div class="form-grid">
          <ui-form-item label="模板名称"><ui-input v-model="form.name" /></ui-form-item>
          <ui-form-item label="模板标识"><ui-input v-model="form.code" :disabled="!!form.id" placeholder="留空自动生成" /></ui-form-item>
        </div>
      </section>
      <section class="editor-section">
        <header><h3>分类与通信协议</h3><p>选择设备分类和已发布协议，通信方式会根据协议自动带入。</p></header>
        <div class="form-grid">
          <ui-form-item label="设备分类"><ui-select v-model="form.category"><ui-option v-for="(text,key) in categories" :key="key" :label="text" :value="key" /></ui-select></ui-form-item>
          <ui-form-item label="设备通信协议"><ui-select v-model="form.protocolPackageId" :disabled="!!form.id" filterable @change="id => { const p = protocols.find(p => p.id === id); if (p) { form.transport = p.transport === 'MQTT_HTTP' ? 'MQTT' : p.transport === 'TCP_UDP' ? 'TCP' : p.transport; form.payloadFormat = p.payloadFormat } }"><ui-option v-for="item in protocols" :key="item.id" :label="item.name" :value="item.id" /></ui-select></ui-form-item>
          <ui-form-item v-if="protocols.find(p=>p.id===form.protocolPackageId)?.transport==='MQTT_HTTP'" label="设备上报方式"><ui-select v-model="form.transport"><ui-option label="MQTT" value="MQTT" /><ui-option label="HTTP" value="HTTP" /></ui-select></ui-form-item>
          <ui-form-item v-if="protocols.find(p=>p.id===form.protocolPackageId)?.transport==='TCP_UDP'" label="设备上报方式"><ui-select v-model="form.transport"><ui-option label="TCP" value="TCP" /><ui-option label="UDP" value="UDP" /></ui-select></ui-form-item>
        </div>
      </section>
      <section class="editor-section">
        <header><h3>型号与编号线索</h3><p>帮助现场人员确认设备型号并找到真实上报编号，可按已知信息填写。</p></header>
        <div class="form-grid">
          <ui-form-item label="厂商（如已知）"><ui-input v-model="form.metadata.manufacturer" /></ui-form-item>
          <ui-form-item label="型号（如已知）"><ui-input v-model="form.metadata.model" /></ui-form-item>
          <ui-form-item label="编号类型（如已知）"><ui-input v-model="form.metadata.idKind" placeholder="IMEI、序列号或协议地址" /></ui-form-item>
          <ui-form-item label="编号位置（如已知）"><ui-input v-model="form.metadata.idLocation" placeholder="设备铭牌或厂家配置工具" /></ui-form-item>
        </div>
      </section>
      <section class="editor-section">
        <header><h3>状态与说明</h3><p>停用或草稿状态的模板不能用于添加新设备。</p></header>
        <div class="form-grid"><ui-form-item label="模板状态"><ui-select v-model="form.status"><ui-option label="已启用" value="ENABLED" /><ui-option label="已停用" value="DISABLED" /><ui-option label="草稿" value="DRAFT" /></ui-select></ui-form-item></div>
        <ui-form-item label="说明"><ui-input v-model="form.description" type="textarea" :rows="3" /></ui-form-item>
      </section>
      <ui-collapse class="editor-advanced">
        <ui-collapse-item title="高级通信设置（按需调整）" name="advanced">
          <p class="editor-advanced__hint">仅在协议要求不同的传输方式或数据格式时修改。</p>
          <div class="form-grid">
            <ui-form-item label="传输协议"><ui-select v-model="form.transport"><ui-option v-for="x in ['MQTT','HTTP','MQTT_HTTP','TCP','UDP','TCP_UDP','MODBUS_TCP','MODBUS_RTU']" :key="x" :label="transportLabel(x)" :value="x" /></ui-select></ui-form-item>
            <ui-form-item label="数据格式"><ui-select v-model="form.payloadFormat"><ui-option label="JSON" value="json" /><ui-option label="HEX（十六进制）" value="hex" /><ui-option label="Binary（二进制）" value="binary" /></ui-select></ui-form-item>
          </div>
        </ui-collapse-item>
      </ui-collapse>
    </ui-form>
    <template #footer>
      <ui-button @click="dialog=false">关闭</ui-button>
      <ui-button v-permission="['POST /api/v1/products','PUT /api/v1/products/:id']" type="primary" :loading="saving" @click="save">保存设备模板</ui-button>
    </template>
  </ui-dialog>
</template>

<style scoped>
.product-name { padding: 0; color: var(--text-strong); background: none; border: 0; font: inherit; font-weight: var(--font-weight-semibold); text-align: left; cursor: pointer; }
.product-name:hover { color: var(--primary-text); text-decoration: underline; }
.editor-section + .editor-section { margin-top: var(--space-2); padding-top: var(--space-4); border-top: 1px solid var(--border); }
.editor-section header { margin-bottom: var(--space-3); }
.editor-section h3 { margin: 0; color: var(--text-strong); font-size: var(--font-size-md); font-weight: var(--font-weight-semibold); }
.editor-section header p, .editor-advanced__hint { margin: 2px 0 0; color: var(--text-muted); font-size: var(--font-size-xs); }
.editor-advanced { margin-top: var(--space-2); padding-top: var(--space-2); border-top: 1px solid var(--border); }
.editor-advanced__hint { margin-bottom: var(--space-3); }
.product-detail__head { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); margin-bottom: var(--space-3); }
.product-detail__tabs :deep(.n-tab-pane) { padding-top: var(--space-4); }
@media (max-width: 767px) {
  .product-detail :deep(.n-descriptions-table) { table-layout: fixed; }
}
</style>
