<script setup>
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
const emit = defineEmits(['navigate'])
import ProductPreparation from '../components/ProductPreparation.vue'
import { transportLabel, formatLabel } from '../presentation'
import { onMounted, ref } from 'vue'
import { api, apiAll, isAbort } from '../api'
import { useListLoader } from '../composables/useListLoader'
import { confirmDelete } from '../deleteAction'
import { categories, label } from '../labels'
import { can } from '../permissions'
import { Plus, RefreshCw } from '@lucide/vue'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'

// 所有新建、详情和编辑都进入同一个模板准备页面。
const preparationOpen = ref(false),
  preparationProduct = ref(null),
  preparationStep = ref(0),
  preparationDraftId = ref(''),
  preparationDrafts = ref([]),
  draftsPage = ref(1),
  draftsTotal = ref(0),
  draftsError = ref(''),
  draftsLoading = ref(false)
const draftsLoader = useListLoader(draftsLoading)
async function loadDrafts(page = 1) {
  if (!can('PUT /api/v1/products/:id')) return
  draftsPage.value = page
  draftsError.value = ''
  try {
    const result = await draftsLoader.run(signal =>
      api(`/api/v1/onboarding/drafts?purpose=preparation&limit=20&offset=${(page - 1) * 20}`, { signal })
    )
    preparationDrafts.value = (result.items || []).filter(
      row =>
        String((typeof row.body === 'string' ? JSON.parse(row.body) : row.body)?.step || '').startsWith('preparation:') &&
        (typeof row.body === 'string' ? JSON.parse(row.body) : row.body)?.step !== 'preparation:linked'
    )
    draftsTotal.value = result.total || 0
  } catch (cause) {
    if (!isAbort(cause)) draftsError.value = cause.message
  }
}
function resumePreparation(row) {
  preparationProduct.value = null
  preparationDraftId.value = row.id
  preparationStep.value = 0
  preparationOpen.value = true
}
function preparationDraftName(row) {
  const body = typeof row.body === 'string' ? JSON.parse(row.body) : row.body
  return body?.request?.newProduct?.name || '未命名设备模板'
}
async function closePreparation() {
  preparationOpen.value = false
  await load()
  await loadDrafts()
}
const products = ref([])
const protocols = ref([])
const loading = ref(false)
const productPage = ref(1)
const productPageSize = ref(20)
const productTotal = ref(0)

let loadVersion = 0
const loadError = ref('')
let catalogVersion = 0
const unbound = ref([])
// Templates without a usable protocol only accept the platform's standard
// messages; other payloads are recorded as parse failures.
async function loadUnbound() {
  try {
    unbound.value = (await api('/api/v1/products/protocol-binding-check')).items || []
  } catch {
    unbound.value = []
  }
}
async function load({ catalog = true } = {}) {
  const version = ++loadVersion
  const currentCatalog = catalog ? ++catalogVersion : 0
  loading.value = true
  try {
    const [p, pk] = await Promise.all([
      api(`/api/v1/products?page=${productPage.value}&pageSize=${productPageSize.value}`),
      catalog ? Promise.all([apiAll('/api/v1/protocol-packages'), api('/api/v2/protocols')]) : null
    ])
    if (pk && currentCatalog === catalogVersion)
      protocols.value = [
        ...new Map(
          [
            { id: 'iot-standard@1.0.0', name: '标准设备上报', transport: 'MQTT_HTTP', payloadFormat: 'json' },
            ...(pk[0].items || []).filter(p => p.status === 'PUBLISHED'),
            ...(pk[1].items || []).flatMap(p =>
              (p.releases || [])
                .filter(r => r.status === 'PUBLISHED')
                .map(r => ({
                  id: `${p.definition.id}@${r.version}`,
                  name: `${p.definition.name} · ${r.version}`,
                  transport: r.transport,
                  payloadFormat: r.payloadFormat
                }))
            )
          ].map(p => [p.id, p])
        ).values()
      ]
    if (version !== loadVersion) return
    products.value = p.items || []
    productTotal.value = Number(p.total ?? p.count ?? products.value.length)
    loadError.value = ''
    loadUnbound()
  } catch (error) {
    if (version === loadVersion) loadError.value = error?.message || '设备模板读取失败'
  } finally {
    if (version === loadVersion) loading.value = false
  }
}

function changePage(value) {
  productPage.value = value
  load({ catalog: false })
}

function changePageSize(value) {
  productPageSize.value = value
  productPage.value = 1
  load({ catalog: false })
}

function openCreate() {
  preparationProduct.value = null
  preparationDraftId.value = ''
  preparationStep.value = 0
  preparationOpen.value = true
}
function openDetail(item, tab = 'basic') {
  preparationProduct.value = item
  preparationDraftId.value = ''
  preparationStep.value = tab === 'protocol' ? 1 : tab === 'access' ? 2 : 0
  preparationOpen.value = true
}

onMounted(async () => {
  let navigation = {}
  try {
    navigation = JSON.parse(sessionStorage.getItem('iot:navigation-detail') || '{}')
  } catch {
    navigation = {}
  }
  sessionStorage.removeItem('iot:navigation-detail')
  await load()
  loadDrafts()
  if (navigation.create) openCreate()
  if (navigation.productId) {
    const item =
      products.value.find(p => p.id === navigation.productId) ||
      (await apiAll('/api/v1/products').catch(() => ({ items: [] }))).items?.find(p => p.id === navigation.productId)
    if (item) openDetail(item, navigation.tab || 'basic')
  }
})
function remove(row) {
  return confirmDelete({ label: row.name || row.id, path: `/api/v1/products/${encodeURIComponent(row.id)}`, onDeleted: load })
}
function protocolName(id) {
  return protocols.value.find(item => item.id === id)?.name || id || '未绑定'
}
function rowActions(row) {
  return [
    { key: 'view', label: '详情', onClick: () => openDetail(row) },
    { key: 'access', label: '连接与验收', onClick: () => openDetail(row, 'access') },
    { key: 'binding', label: '通信协议', onClick: () => openDetail(row, 'protocol') },
    { key: 'edit', label: '编辑', permission: 'PUT /api/v1/products/:id', onClick: () => openDetail(row) },
    { key: 'delete', label: '删除', type: 'danger', permission: 'DELETE /api/v1/products/:id', onClick: () => remove(row) }
  ]
}
</script>

<template>
  <ProductPreparation
    v-if="preparationOpen"
    :key="preparationDraftId || preparationProduct?.id || 'new'"
    :product="preparationProduct"
    :initial-step="preparationStep"
    :draft-id="preparationDraftId"
    @close="closePreparation"
    @saved="load"
    @navigate="(page, detail) => emit('navigate', page, detail)"
  />
  <template v-else>
    <FilterBar>
      <template #actions>
        <ui-button :loading="loading" @click="load"><RefreshCw />刷新</ui-button>
        <ui-button v-if="can('PUT /api/v1/products/:id')" v-permission="'POST /api/v1/products'" type="primary" @click="openCreate"
          ><Plus />新建设备模板</ui-button
        >
      </template>
    </FilterBar>

    <ui-alert
      v-if="unbound.length"
      type="warning"
      :closable="false"
      show-icon
      :title="`${unbound.length} 个设备模板未绑定可用协议`"
      :description="`${unbound
        .slice(0, 5)
        .map(item => `${item.name || item.id}（${item.reason}）`)
        .join(
          '、'
        )}${unbound.length > 5 ? ' 等' : ''}。这些模板只能接收平台标准格式报文，其他报文会记为解析失败；请在模板准备中绑定已发布的协议版本。`"
    />
    <ui-alert v-if="draftsError" :title="draftsError" type="warning" :closable="false" /><ui-button
      v-if="draftsError"
      size="small"
      @click="loadDrafts(draftsPage)"
      >重试读取模板草稿</ui-button
    >
    <section v-if="preparationDrafts.length || draftsTotal > 20" class="template-drafts">
      <strong>继续准备设备模板</strong
      ><ui-button
        v-for="draft in preparationDrafts"
        :key="draft.id"
        size="small"
        :disabled="draftsLoading"
        @click="resumePreparation(draft)"
        >{{ preparationDraftName(draft) }}</ui-button
      ><ui-pagination
        v-if="draftsTotal > 20"
        :current-page="draftsPage"
        :page-size="20"
        :total="draftsTotal"
        layout="prev,pager,next"
        @update:current-page="loadDrafts"
      />
    </section>
    <DataTableCard
      :title="`设备模板 · ${productTotal} 个`"
      :error="loadError"
      @retry="load()"
      :page="productPage"
      :page-size="productPageSize"
      :total="productTotal"
      @update:page="changePage"
      @update:page-size="changePageSize"
    >
      <ui-table :data="products" :loading="loading" empty-text="暂无设备模板，点击“新建设备模板”创建">
        <ui-table-column label="设备模板" min-width="220"
          ><template #default="{ row }"
            ><button type="button" class="product-name" @click="openDetail(row)">{{ row.name || row.id }}</button
            ><small class="subline">{{ row.id }}</small></template
          ></ui-table-column
        >
        <ui-table-column label="分类" min-width="120"
          ><template #default="{ row }">{{ label(categories, row.category, '其他设备') }}</template></ui-table-column
        >
        <ui-table-column label="通信协议" min-width="220"
          ><template #default="{ row }"
            >{{ protocolName(row.protocolPackageId)
            }}<small class="subline">{{ transportLabel(row.transport) }} · {{ formatLabel(row.payloadFormat) }}</small></template
          ></ui-table-column
        >
        <ui-table-column label="厂商 / 型号" min-width="160"
          ><template #default="{ row }">{{
            [row.metadata?.manufacturer, row.metadata?.model].filter(Boolean).join(' · ') || '—'
          }}</template></ui-table-column
        >
        <ui-table-column label="准备状态" min-width="160"
          ><template #default="{ row }"
            ><StatusDot
              :tone="row.reusable ? 'success' : 'warning'"
              :label="
                row.reusable
                  ? '可以复用'
                  : {
                      DRAFT: '配置草稿',
                      AWAITING_VALIDATION: '待真实设备验证',
                      CONFIGURATION_CHANGED: '配置变化待验证',
                      UNVERIFIED: '尚未验证'
                    }[row.preparationStatus] || '待准备与验证'
              " /></template
        ></ui-table-column>
        <ui-table-column label="说明" min-width="200" show-overflow-tooltip
          ><template #default="{ row }">{{ row.description || '—' }}</template></ui-table-column
        >
        <ui-table-column label="操作" width="176" fixed="right" align="right"
          ><template #default="{ row }"><RowActions :actions="rowActions(row)" /></template
        ></ui-table-column>
      </ui-table>
    </DataTableCard>
  </template>
</template>

<style scoped>
.template-drafts {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  align-items: center;
  margin-bottom: var(--space-4);
}
.product-name {
  padding: 0;
  color: var(--text-strong);
  background: none;
  border: 0;
  font: inherit;
  font-weight: var(--font-weight-semibold);
  text-align: left;
  cursor: pointer;
}
.product-name:hover {
  color: var(--primary-text);
  text-decoration: underline;
}
</style>
