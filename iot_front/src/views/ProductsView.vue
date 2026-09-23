<script setup>
import { createClientId } from '../clientId' /* 引入当前代码需要的依赖。 */
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate']) /* 执行当前语句并推进处理流程。 */
import ProductProtocolBinding from '../components/ProductProtocolBinding.vue' /* 引入当前代码需要的依赖。 */
import { transportLabel, formatLabel } from '../presentation' /* 引入当前代码需要的依赖。 */
import { onMounted, reactive, ref } from 'vue' /* 引入当前代码需要的依赖。 */
import { ElMessage } from 'element-plus' /* 引入当前代码需要的依赖。 */
import { api, apiAll, notifyError } from '../api' /* 引入当前代码需要的依赖。 */
import { categories, enabledStatuses, label, tagType } from '../labels' /* 引入当前代码需要的依赖。 */

const bindingProduct = ref(null) /* 声明 bindingProduct。 */
const products = ref([]) /* 声明 products。 */
const protocols = ref([]) /* 声明 protocols。 */
const saving = ref(false) /* 声明 saving。 */
const loading = ref(false) /* 声明 loading。 */
const dialog = ref(false) /* 声明 dialog。 */
const readonly = ref(false) /* 声明 readonly。 */
const productPage = ref(1) /* 声明 productPage。 */
const productPageSize = ref(20) /* 声明 productPageSize。 */
const productTotal = ref(0) /* 声明 productTotal。 */

const blank = () => ({ id:'', code:'', name:'', category:'smoke', protocolPackageId:'iot-standard@1.0.0', transport:'MQTT', payloadFormat:'json', status:'ENABLED', description:'', thingModel:null }) /* 声明 blank。 */
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
    if (pk && currentCatalog === catalogVersion) protocols.value = [...new Map([{ id:'iot-standard@1.0.0', name:'标准设备上报', transport:'MQTT', payloadFormat:'json' }, ...(pk[0].items || []), ...(pk[1].items || []).flatMap(p => (p.releases || []).filter(r => r.status === 'PUBLISHED').map(r => ({ id:`${p.definition.id}@${r.version}`, name:`${p.definition.name} · ${r.version}`, transport:r.transport, payloadFormat:r.payloadFormat })))].map(p => [p.id, p])).values()] /* 判断条件并选择处理分支。 */
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
  readonly.value = false /* 更新 readonly.value 的值。 */
  dialog.value = true /* 更新 dialog.value 的值。 */
} /* 结束当前表达式或代码块。 */

function view(item) { /* 定义 view 函数。 */
  Object.assign(form, { ...blank(), ...item, code:item.id }) /* 执行当前语句并推进处理流程。 */
  readonly.value = true /* 更新 readonly.value 的值。 */
  dialog.value = true /* 更新 dialog.value 的值。 */
} /* 结束当前表达式或代码块。 */

function edit(item) { /* 定义 edit 函数。 */
  Object.assign(form, { ...blank(), ...item, code:item.id }) /* 执行当前语句并推进处理流程。 */
  readonly.value = false /* 更新 readonly.value 的值。 */
  dialog.value = true /* 更新 dialog.value 的值。 */
} /* 结束当前表达式或代码块。 */

function startEdit() { /* 定义 startEdit 函数。 */
  readonly.value = false /* 更新 readonly.value 的值。 */
} /* 结束当前表达式或代码块。 */

async function save() { /* 定义 save 函数。 */
  if (saving.value) return /* 判断条件并选择处理分支。 */
  if (!form.name.trim() || !form.protocolPackageId) return ElMessage.warning('请填写产品名称并选择协议包') /* 判断条件并选择处理分支。 */
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
    ElMessage.success('产品已保存') /* 执行当前语句并推进处理流程。 */
    dialog.value = false /* 更新 dialog.value 的值。 */
    reset() /* 执行当前语句并推进处理流程。 */
    await load() /* 等待异步操作完成。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    notifyError(error) /* 执行当前语句并推进处理流程。 */
  } finally { /* 结束当前表达式或代码块。 */
    saving.value = false /* 更新 saving.value 的值。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

onMounted(load) /* 执行当前语句并推进处理流程。 */
</script>

<template>
  <ProductProtocolBinding v-if="bindingProduct" :key="bindingProduct.id" :product="bindingProduct" @close="bindingProduct=null" @saved="load" /> <!-- 渲染 ProductProtocolBinding 界面元素。 -->
  <div class="page-toolbar"> <!-- 渲染 div 界面元素。 -->
    <el-button v-permission="'POST /api/v1/products'" type="primary" @click="openCreate">新建产品</el-button> <!-- 渲染 el-button 界面元素。 -->
    <el-button :loading="loading" @click="load">刷新</el-button> <!-- 渲染 el-button 界面元素。 -->
    <span>{{ productTotal }} 个产品</span> <!-- 渲染 span 界面元素。 -->
  </div> <!-- 结束当前界面区域。 -->

  <el-card shadow="never" class="surface-card table-card"> <!-- 渲染 el-card 界面元素。 -->
    <el-table v-loading="loading" :data="products" stripe> <!-- 渲染 el-table 界面元素。 -->
      <el-table-column label="产品" min-width="230"> <!-- 渲染 el-table-column 界面元素。 -->
        <template #default="{ row }">
          <b>{{ row.name || row.id }}</b> <!-- 渲染 b 界面元素。 -->
          <small class="subline">{{ row.id }}</small> <!-- 渲染 small 界面元素。 -->
        </template>
      </el-table-column>
      <el-table-column label="分类" width="130">
        <template #default="{ row }">{{ label(categories, row.category, '其他设备') }}</template>
      </el-table-column> <!-- 结束当前界面区域。 -->
      <el-table-column label="协议 / 格式" min-width="180"> <!-- 渲染 el-table-column 界面元素。 -->
        <template #default="{ row }">
          <b>{{ transportLabel(row.transport) }}</b> <!-- 渲染 b 界面元素。 -->
          <small class="subline">{{ formatLabel(row.payloadFormat) }}</small> <!-- 渲染 small 界面元素。 -->
        </template>
      </el-table-column>
      <el-table-column label="状态" width="110" align="center">
        <template #default="{ row }"><el-tag :type="tagType(row.status)" round>{{ label(enabledStatuses, row.status) }}</el-tag></template>
      </el-table-column> <!-- 结束当前界面区域。 -->
      <el-table-column label="说明" min-width="220" show-overflow-tooltip> <!-- 渲染 el-table-column 界面元素。 -->
        <template #default="{ row }">{{ row.description || '-' }}</template>
      </el-table-column> <!-- 结束当前界面区域。 -->
      <el-table-column label="操作" width="240" fixed="right" align="center"> <!-- 渲染 el-table-column 界面元素。 -->
        <template #default="{ row }">
          <div class="table-actions"> <!-- 渲染 div 界面元素。 -->
            <el-button plain type="primary" @click="view(row)">详情</el-button> <!-- 渲染 el-button 界面元素。 -->
            <el-button plain @click="bindingProduct=row">协议版本</el-button><el-button v-permission="'PUT /api/v1/products/:id'" plain type="primary" @click="edit(row)">编辑</el-button> <!-- 渲染 el-button 界面元素。 -->
          </div> <!-- 结束当前界面区域。 -->
        </template>
      </el-table-column>
      <template #empty><el-empty description="暂无产品" /></template>
    </el-table> <!-- 结束当前界面区域。 -->
    <div class="list-pagination"> <!-- 渲染 div 界面元素。 -->
      <el-pagination v-model:current-page="productPage" v-model:page-size="productPageSize" :total="productTotal" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next, jumper" @current-change="changePage" @size-change="changePageSize" /> <!-- 渲染 el-pagination 界面元素。 -->
    </div> <!-- 结束当前界面区域。 -->
  </el-card> <!-- 结束当前界面区域。 -->

  <el-dialog v-model="dialog" :title="readonly ? `产品详情 · ${form.name || form.id}` : (form.id ? `编辑产品 · ${form.name}` : '新建产品')" width="min(760px, 94vw)"> <!-- 渲染 el-dialog 界面元素。 -->
    <el-form :model="form" label-position="top" :disabled="readonly"> <!-- 渲染 el-form 界面元素。 -->
      <el-form-item label="产品名称"><el-input v-model="form.name" /></el-form-item> <!-- 渲染 el-form-item 界面元素。 -->
      <el-form-item label="产品标识"><el-input v-model="form.code" :disabled="readonly || !!form.id" placeholder="留空自动生成" /></el-form-item> <!-- 渲染 el-form-item 界面元素。 -->
      <div class="form-grid"> <!-- 渲染 div 界面元素。 -->
        <el-form-item label="设备分类"><el-select v-model="form.category"><el-option v-for="(text,key) in categories" :key="key" :label="text" :value="key" /></el-select></el-form-item> <!-- 渲染 el-form-item 界面元素。 -->
        <el-form-item label="协议包"><el-select v-model="form.protocolPackageId" :disabled="!!form.id" filterable @change="id => { const p = protocols.find(p => p.id === id); if (p) { form.transport = p.transport === 'MQTT_HTTP' ? 'MQTT' : p.transport; form.payloadFormat = p.payloadFormat } }"><el-option v-for="item in protocols" :key="item.id" :label="item.name" :value="item.id" /></el-select></el-form-item> <!-- 渲染 el-form-item 界面元素。 -->
        <el-form-item label="传输协议"><el-select v-model="form.transport"><el-option v-for="x in ['MQTT','HTTP','MQTT_HTTP','TCP','UDP','TCP_UDP','MODBUS_TCP','MODBUS_RTU']" :key="x" :label="transportLabel(x)" :value="x" /></el-select></el-form-item> <!-- 渲染 el-form-item 界面元素。 -->
        <el-form-item label="数据格式"><el-select v-model="form.payloadFormat"><el-option label="JSON" value="json" /><el-option label="HEX（十六进制）" value="hex" /><el-option label="Binary（二进制）" value="binary" /></el-select></el-form-item> <!-- 渲染 el-form-item 界面元素。 -->
      </div> <!-- 结束当前界面区域。 -->
      <el-form-item label="产品状态"><el-select v-model="form.status"><el-option label="已启用" value="ENABLED" /><el-option label="已停用" value="DISABLED" /><el-option label="草稿" value="DRAFT" /></el-select></el-form-item> <!-- 渲染 el-form-item 界面元素。 -->
      <el-form-item label="说明"><el-input v-model="form.description" type="textarea" :rows="3" /></el-form-item> <!-- 渲染 el-form-item 界面元素。 -->
    </el-form> <!-- 结束当前界面区域。 -->
    <template #footer>
      <el-button v-permission="'PUT /api/v1/products/:id'" v-if="readonly" type="primary" @click="startEdit">编辑</el-button> <!-- 渲染 el-button 界面元素。 -->
      <el-button @click="dialog=false">关闭</el-button> <!-- 渲染 el-button 界面元素。 -->
      <el-button v-permission="['POST /api/v1/products','PUT /api/v1/products/:id']" v-if="!readonly" type="primary" :loading="saving" @click="save">保存产品</el-button> <!-- 渲染 el-button 界面元素。 -->
    </template>
  </el-dialog>
</template>
