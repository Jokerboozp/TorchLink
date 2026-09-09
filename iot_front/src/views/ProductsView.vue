<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, apiAll, notifyError } from '../api'
import { categories, enabledStatuses, label, tagType } from '../labels'

const thingModelText = ref('')
const products = ref([])
const protocols = ref([])
const saving = ref(false)
const loading = ref(false)
const dialog = ref(false)
const readonly = ref(false)
const productPage = ref(1)
const productPageSize = ref(20)
const productTotal = ref(0)

const blank = () => ({ id:'', code:'', name:'', category:'smoke', protocolPackageId:'', transport:'MQTT', payloadFormat:'json', status:'ENABLED', description:'', thingModel:null })
const form = reactive(blank())

let loadVersion = 0
async function load() {
  const version = ++loadVersion
  loading.value = true
  try {
    const [p, pk] = await Promise.all([
      api(`/api/v1/products?page=${productPage.value}&pageSize=${productPageSize.value}`),
      apiAll('/api/v1/protocol-packages')
    ])
    if (version !== loadVersion) return
    products.value = p.items || []
    productTotal.value = Number(p.total ?? p.count ?? products.value.length)
    protocols.value = pk.items || []
  } catch (error) {
    if (version === loadVersion) notifyError(error)
  } finally {
    if (version === loadVersion) loading.value = false
  }
}

function changePage(value) {
  productPage.value = value
  load()
}

function changePageSize(value) {
  productPageSize.value = value
  productPage.value = 1
  load()
}

function reset() {
  Object.assign(form, blank())
 thingModelText.value=''
}

function openCreate() {
  reset()
  readonly.value = false
  dialog.value = true
}

function view(item) {
  Object.assign(form, { ...blank(), ...item, code:item.id })
 thingModelText.value=item.thingModel?JSON.stringify(item.thingModel,null,2):''
  readonly.value = true
  dialog.value = true
}

function edit(item) {
  Object.assign(form, { ...blank(), ...item, code:item.id })
 thingModelText.value=item.thingModel?JSON.stringify(item.thingModel,null,2):''
  readonly.value = false
  dialog.value = true
}

function startEdit() {
  readonly.value = false
}

async function save() {
  saving.value = true
  try {
    const value = { ...form, id:form.id || form.code }
    value.thingModel=thingModelText.value.trim()?JSON.parse(thingModelText.value):null
 delete value.code
    const editing = Boolean(value.id)
    await api(editing ? `/api/v1/products/${encodeURIComponent(value.id)}` : '/api/v1/products', {
      method: editing ? 'PUT' : 'POST',
      body: JSON.stringify(value)
    })
    ElMessage.success('产品已保存')
    dialog.value = false
    reset()
    await load()
  } catch (error) {
    notifyError(error)
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="page-toolbar">
    <el-button type="primary" @click="openCreate">新建产品</el-button>
    <el-button :loading="loading" @click="load">刷新</el-button>
    <span>共 {{ productTotal }} 个产品，详情和编辑操作位于列表右侧。</span>
  </div>

  <el-card shadow="never" class="surface-card table-card">
    <el-table v-loading="loading" :data="products" stripe>
      <el-table-column label="产品" min-width="230">
        <template #default="{ row }">
          <b>{{ row.name || row.id }}</b>
          <small class="subline">{{ row.id }}</small>
        </template>
      </el-table-column>
      <el-table-column label="分类" width="130">
        <template #default="{ row }">{{ label(categories, row.category, '其他设备') }}</template>
      </el-table-column>
      <el-table-column label="协议 / 格式" min-width="180">
        <template #default="{ row }">
          <b>{{ row.transport || '-' }}</b>
          <small class="subline">{{ row.payloadFormat || '-' }}</small>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="110" align="center">
        <template #default="{ row }"><el-tag :type="tagType(row.status)" round>{{ label(enabledStatuses, row.status, row.status) }}</el-tag></template>
      </el-table-column>
      <el-table-column label="说明" min-width="220" show-overflow-tooltip>
        <template #default="{ row }">{{ row.description || '-' }}</template>
      </el-table-column>
      <el-table-column label="操作" width="200" fixed="right" align="center">
        <template #default="{ row }">
          <div class="table-actions">
            <el-button plain type="primary" @click="view(row)">详情</el-button>
            <el-button plain type="primary" @click="edit(row)">编辑</el-button>
          </div>
        </template>
      </el-table-column>
      <template #empty><el-empty description="暂无产品" /></template>
    </el-table>
    <div class="list-pagination">
      <el-pagination v-model:current-page="productPage" v-model:page-size="productPageSize" :total="productTotal" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next, jumper" @current-change="changePage" @size-change="changePageSize" />
    </div>
  </el-card>

  <el-dialog v-model="dialog" :title="readonly ? `产品详情 · ${form.name || form.id}` : (form.id ? `编辑产品 · ${form.name}` : '新建产品')" width="min(760px, 94vw)">
    <el-form :model="form" label-position="top" :disabled="readonly">
      <el-form-item label="产品名称"><el-input v-model="form.name" /></el-form-item>
      <el-form-item label="产品标识"><el-input v-model="form.code" :disabled="readonly || !!form.id" placeholder="留空自动生成" /></el-form-item>
      <div class="form-grid">
        <el-form-item label="设备分类"><el-select v-model="form.category"><el-option v-for="(text,key) in categories" :key="key" :label="text" :value="key" /></el-select></el-form-item>
        <el-form-item label="协议包"><el-select v-model="form.protocolPackageId" filterable clearable><el-option v-for="item in protocols" :key="item.id" :label="`${item.name} · ${item.id}`" :value="item.id" /></el-select></el-form-item>
        <el-form-item label="传输协议"><el-select v-model="form.transport"><el-option v-for="x in ['MQTT','HTTP','TCP','MODBUS_TCP']" :key="x" :label="x" :value="x" /></el-select></el-form-item>
        <el-form-item label="数据格式"><el-select v-model="form.payloadFormat"><el-option label="结构化文本" value="json" /><el-option label="十六进制" value="hex" /><el-option label="二进制" value="binary" /></el-select></el-form-item>
      </div>
      <el-form-item label="产品状态"><el-select v-model="form.status"><el-option label="已启用" value="ENABLED" /><el-option label="已停用" value="DISABLED" /><el-option label="草稿" value="DRAFT" /></el-select></el-form-item>
      <el-form-item label="说明"><el-input v-model="form.description" type="textarea" :rows="3" /></el-form-item>
      <el-collapse><el-collapse-item title="物模型基础（高级）" name="thing-model"><p>定义属性、事件和命令。此定义用于描述数据及校验命令；属性设置 writable:true 后才允许写入设备影子期望状态。</p><el-input v-model="thingModelText" type="textarea" :rows="12" placeholder='{"properties":[{"identifier":"temperature","name":"温度","dataType":"number","unit":"℃"}],"events":[],"commands":[]}' /></el-collapse-item></el-collapse>
    </el-form>
    <template #footer>
      <el-button v-if="readonly" type="primary" @click="startEdit">编辑</el-button>
      <el-button @click="dialog=false">关闭</el-button>
      <el-button v-if="!readonly" type="primary" :loading="saving" @click="save">保存产品</el-button>
    </template>
  </el-dialog>
</template>
