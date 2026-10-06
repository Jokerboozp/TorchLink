<script setup>
import { takeNavigation } from '../router/paths'
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate'])
import { errorMessage, transportLabel, formatLabel } from '../presentation'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { UiMessage } from '../ui/feedback.js'
import { api, apiAll, download, formatTime, isAbort, notifyError, pretty } from '../api'
import { downloadWithProgress, transferText } from '../transfer'
import { useListLoader } from '../composables/useListLoader'
import { can, permissionState } from '../permissions'
import { messageTypeLabel, messageTypes } from '../labels'
import { usePageState } from '../composables/usePageState.js'
import { Download, RefreshCw, Search, SlidersHorizontal } from '@lucide/vue'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'
import DeviceFilterSelect from '../components/DeviceFilterSelect.vue'

const emptyFilters = () => ({
  deviceId: '',
  messageId: '',
  productId: '',
  protocol: '',
  payloadFormat: '',
  parseStatus: '',
  messageType: '',
  parser: '',
  range: null
})
const filters = ref(emptyFilters())
const appliedFilters = ref(emptyFilters())
const expanded = ref(false)
const loadError = ref('')
const parseStatuses = { PARSED: '已解析', FAILED: '解析失败', UNPARSED: '待解析 / 未匹配' }
const hasFilters = computed(() => Object.values(filters.value).some(Boolean) || Object.values(appliedFilters.value).some(Boolean))
const advancedCount = computed(
  () => ['productId', 'protocol', 'payloadFormat', 'messageType', 'parser'].filter(key => filters.value[key]).length
)
const activeFilterCount = computed(() => Object.values(appliedFilters.value).filter(Boolean).length)
const items = ref([])
const selection = ref([])
const loading = ref(false)
const detail = ref(null)
const detailVisible = ref(false)
const detailTab = ref('parsed') /* 每次查看报文都从解析结果开始，并显式同步页签状态。 */
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
// 批量下载的已接收进度，只在下载期间显示在按钮上。
const batchProgress = ref('')
// 总数超过一万条时后端只计到 10001；未选时间、设备和消息时只查最近 7 天。
const totalCapped = ref(false)
const defaultWindow = ref(false)
const totalLabel = computed(() => (totalCapped.value ? '10000+' : String(total.value)))
const selectedIds = computed(() => selection.value.map(item => item.messageId))
const loader = useListLoader(loading)
// 已应用的筛选、页码和每页条数在刷新或切换菜单后恢复；输入框同步为已应用的条件。
usePageState('raw', { filters: appliedFilters, page, pageSize })
filters.value = { ...emptyFilters(), ...appliedFilters.value, range: appliedFilters.value.range ? [...appliedFilters.value.range] : null }

// 报文索引只保存设备与产品编号；名称按当前账户可读的设备和产品资料补充，读取失败时仍显示编号。
const names = reactive({ devices: {}, products: {} })
let productsRequested = false
async function resolveNames(rows) {
  if (!productsRequested && can('GET /api/v1/products')) {
    productsRequested = true
    apiAll('/api/v1/products')
      .then(data => {
        for (const product of data.items || []) if (product?.id) names.products[product.id] = product.name || ''
      })
      .catch(() => {
        productsRequested = false
      })
  }
  if (!can('GET /api/v1/device-registry')) return
  const pending = [...new Set(rows.map(row => row.deviceId).filter(id => id && !(id in names.devices)))]
  for (const id of pending) names.devices[id] = ''
  // 少量并发按编号检索；服务端按用户设备范围过滤，看不到的设备只显示编号。
  const worker = async () => {
    for (let id = pending.shift(); id; id = pending.shift()) {
      try {
        const data = await api(`/api/v1/device-registry?page=1&pageSize=20&q=${encodeURIComponent(id)}`)
        const found = (data.items || []).map(item => item.device || item).find(item => item.id === id)
        names.devices[id] = found?.name || ''
      } catch {
        delete names.devices[id]
      }
    }
  }
  await Promise.all(Array.from({ length: Math.min(4, pending.length) }, worker))
}
const deviceName = (id, fallback = '') => names.devices[id] || fallback || ''
const productName = id => names.products[id] || ''
// 权限或设备范围变化后丢弃已解析的名称，按新的范围重新读取。
watch(
  () => permissionState.accessVersion,
  () => {
    names.devices = {}
    names.products = {}
    productsRequested = false
    void resolveNames(items.value)
  }
)

// 有设备资料读取权限时按名称或编号选择设备；否则输入完整设备编号。
const canPickDevice = computed(() => can('GET /api/v1/device-registry'))

async function load() {
  loadError.value = ''
  selection.value = []
  try {
    const params = new URLSearchParams({ page: String(page.value), pageSize: String(pageSize.value) })
    for (const [key, value] of Object.entries(appliedFilters.value)) {
      if (key !== 'range' && value) params.set(key, value)
    }
    if (appliedFilters.value.range) {
      params.set('start', String(appliedFilters.value.range[0]))
      params.set('end', String(appliedFilters.value.range[1]))
    }
    const data = await loader.run(signal => api(`/api/v1/raw-messages?${params.toString()}`, { signal }))
    items.value = data.items || []
    total.value = Number(data.total ?? items.value.length)
    totalCapped.value = data.totalCapped === true
    defaultWindow.value = data.window?.defaulted === true
    selection.value = []
    void resolveNames(items.value)
  } catch (error) {
    // 失败只在表格上方显示一处可重试的错误，不再同时弹出提示和空表文案。
    if (isAbort(error)) return
    items.value = []
    total.value = 0
    totalCapped.value = false
    defaultWindow.value = false
    loadError.value = error?.status === 401 ? '' : errorMessage(error) || '原始报文查询失败'
  }
}

async function search() {
  const next = Object.fromEntries(
    Object.entries(filters.value).map(([key, value]) => [key, typeof value === 'string' ? value.trim() : value])
  )
  if (
    next.range &&
    (next.range.length !== 2 || next.range.some(value => !Number.isFinite(value) || value < 0) || next.range[0] > next.range[1])
  ) {
    UiMessage.warning('请选择有效的接收时间范围，开始时间不能晚于结束时间')
    return
  }
  appliedFilters.value = { ...next, range: next.range ? [...next.range] : null }
  page.value = 1
  await load()
}

function resetFilters() {
  filters.value = emptyFilters()
  return search()
}
function recentHours(hours) {
  const end = Date.now()
  filters.value.range = hours ? [end - hours * 3600000, end] : null
  return search()
}
function parseState(row) {
  if (row.parsed) return { tone: 'success', label: `已解析 · ${messageTypeLabel(row.parsedMessageType)}` }
  return row.parseError ? { tone: 'danger', label: '解析失败' } : { tone: 'neutral', label: '待解析 / 未匹配' }
}
// 详情的解析状态与列表一致：已解析为绿色，解析失败为红色，其余为灰色。
const detailParseTag = status =>
  ({ PARSED: { type: 'success', label: '已解析' }, FAILED: { type: 'danger', label: '解析失败' } })[status] || {
    type: 'info',
    label: '待解析 / 未匹配'
  }

function changePage(value) {
  page.value = value
  load()
}

function changePageSize(value) {
  pageSize.value = value
  page.value = 1
  load()
}

// 打开详情与下载期间记录进行中的报文，避免重复点击发出重复请求。
const opening = ref('')
const downloading = ref('')
async function show(id) {
  if (opening.value) return
  opening.value = id
  try {
    detail.value = await api(`/api/v1/raw-messages/${encodeURIComponent(id)}`)
    detailTab.value = 'parsed'
    detailVisible.value = true
    if (detail.value?.message?.deviceId) void resolveNames([detail.value.message])
  } catch (error) {
    notifyError(error)
  } finally {
    opening.value = ''
  }
}

async function downloadOne(id) {
  if (downloading.value) return
  downloading.value = id
  try {
    await download(`/api/v1/raw-messages/${encodeURIComponent(id)}/download`, `${id}.json`)
    UiMessage.success('报文已下载')
  } catch (error) {
    notifyError(error)
  } finally {
    downloading.value = ''
  }
}

async function downloadBatch() {
  if (!selectedIds.value.length || downloading.value) return
  downloading.value = 'batch'
  try {
    const stamp = new Date().toISOString().replace(/[-:T]/g, '').slice(0, 14)
    batchProgress.value = ''
    await downloadWithProgress(
      '/api/v1/raw-messages/download',
      `原始报文_${stamp}_${selectedIds.value.length}条.zip`,
      (loaded, total) => (batchProgress.value = transferText(loaded, total)),
      { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ messageIds: selectedIds.value }) }
    )
    UiMessage.success(`已将 ${selectedIds.value.length} 条报文整合为压缩包`)
  } catch (error) {
    notifyError(error)
  } finally {
    downloading.value = ''
  }
}

onMounted(async () => {
  const navigation = takeNavigation()
  // 从设备、告警等页面跳转来时以跳转条件为准，不沿用上次保存的筛选与页码。
  if (Object.keys(navigation).length) {
    filters.value = { ...emptyFilters(), deviceId: typeof navigation.deviceId === 'string' ? navigation.deviceId : '' }
    appliedFilters.value = { ...filters.value }
    page.value = 1
  }
  await load()
  // 告警弹窗传 messageId，接入与设备页传 rawMessageId，二者都指原始报文编号。
  const messageId = navigation.messageId || navigation.rawMessageId
  if (messageId) await show(messageId)
})
function rowActions(row) {
  return [
    {
      key: 'detail',
      label: '详情',
      loading: opening.value === row.messageId,
      disabled: Boolean(opening.value),
      onClick: () => show(row.messageId)
    },
    {
      key: 'download',
      label: '下载',
      permission: 'GET /api/v1/raw-messages/:id/download',
      loading: downloading.value === row.messageId,
      disabled: Boolean(downloading.value),
      onClick: () => downloadOne(row.messageId)
    }
  ]
}
</script>

<template>
  <form class="raw-filters" @submit.prevent="search">
    <div class="raw-filter-grid">
      <label
        >设备<DeviceFilterSelect v-if="canPickDevice" v-model="filters.deviceId" placeholder="按设备名称或编号选择" /><ui-input
          v-else
          v-model="filters.deviceId"
          clearable
          placeholder="输入完整设备标识"
          aria-label="设备标识"
      /></label>
      <label>报文标识<ui-input v-model="filters.messageId" clearable placeholder="输入完整原始报文标识" aria-label="报文标识" /></label>
      <label
        >解析状态<ui-select v-model="filters.parseStatus" clearable placeholder="全部解析状态" aria-label="解析状态"
          ><ui-option v-for="(name, value) in parseStatuses" :key="value" :label="name" :value="value" /></ui-select
      ></label>
      <label class="raw-filter-range">接收时间<ui-date-range v-model="filters.range" clearable aria-label="接收时间范围" /></label>
      <div class="raw-quick-time">
        <span>最近</span><ui-button text size="small" @click="recentHours(1)">1 小时</ui-button
        ><ui-button text size="small" @click="recentHours(24)">24 小时</ui-button
        ><ui-button text size="small" @click="recentHours(168)">7 天</ui-button
        ><ui-button text size="small" @click="recentHours(0)">不限时间</ui-button>
      </div>
    </div>
    <div v-if="expanded" class="raw-filter-grid raw-filter-advanced">
      <label>产品标识<ui-input v-model="filters.productId" clearable placeholder="输入完整产品标识" aria-label="产品标识" /></label>
      <label>协议<ui-input v-model="filters.protocol" clearable placeholder="如 json、gb26875" aria-label="协议" /></label>
      <label
        >报文格式<ui-select
          v-model="filters.payloadFormat"
          clearable
          filterable
          allow-create
          placeholder="全部格式，可输入其他格式"
          aria-label="报文格式"
          ><ui-option
            v-for="value in ['json', 'hex', 'text', 'binary']"
            :key="value"
            :value="value"
            :label="value.toUpperCase()" /></ui-select
      ></label>
      <label
        >消息类型<ui-select v-model="filters.messageType" clearable placeholder="全部消息类型" aria-label="消息类型"
          ><ui-option v-for="(type, value) in messageTypes" :key="value" :label="type.label" :value="value" /></ui-select
      ></label>
      <label>解析器<ui-input v-model="filters.parser" clearable placeholder="输入完整解析器名称" aria-label="解析器" /></label>
    </div>
    <FilterBar class="raw-filter-actions">
      <ui-button type="primary" native-type="submit" :loading="loading"><Search />查询</ui-button>
      <ui-button :disabled="!hasFilters" @click="resetFilters">重置筛选</ui-button>
      <ui-button text :aria-expanded="expanded" @click="expanded = !expanded"
        ><SlidersHorizontal />{{ expanded ? '收起更多筛选' : '更多筛选' }}{{ advancedCount ? `（${advancedCount}）` : '' }}</ui-button
      >
      <template #actions>
        <ui-button
          v-permission="'POST /api/v1/raw-messages/download'"
          :disabled="loading || !selectedIds.length || Boolean(downloading)"
          :loading="downloading === 'batch'"
          @click="downloadBatch"
          ><Download />{{
            downloading === 'batch' && batchProgress ? `正在下载 ${batchProgress}` : `批量下载（${selectedIds.length}）`
          }}</ui-button
        >
        <ui-button :loading="loading" @click="load"><RefreshCw />刷新</ui-button>
      </template>
    </FilterBar>
  </form>
  <DataTableCard
    :title="`原始报文 · ${totalLabel} 条`"
    :page="page"
    :page-size="pageSize"
    :total="total"
    :error="loadError"
    @update:page="changePage"
    @update:page-size="changePageSize"
    @retry="load"
  >
    <p class="raw-hint">
      {{ activeFilterCount ? `已应用 ${activeFilterCount} 项筛选；` : ''
      }}{{ defaultWindow ? '已限定为最近 7 天，查看更早的报文请选择接收时间或设备；' : '' }}保留原文证据链；详情同时展示标准解析结果。
    </p>
    <ui-table
      v-loading="loading"
      :data="items"
      :empty-text="activeFilterCount ? '没有符合筛选条件的原始报文' : '暂无原始报文'"
      @selection-change="selection = $event"
    >
      <ui-table-column type="selection" width="48" /><ui-table-column label="接收时间" width="160"
        ><template #default="{ row }">{{ formatTime(row.receivedAt) }}</template></ui-table-column
      ><ui-table-column prop="messageId" label="消息标识" min-width="200" show-overflow-tooltip /><ui-table-column
        label="设备 / 产品"
        min-width="220"
        ><template #default="{ row }"
          ><span class="raw-id" :title="row.deviceId">{{ deviceName(row.deviceId) || row.deviceId }}</span
          ><small class="subline raw-id" :title="`设备 ${row.deviceId} · 产品 ${row.productId}`"
            ><template v-if="deviceName(row.deviceId)">{{ row.deviceId }} · </template
            >{{ productName(row.productId) || row.productId }}</small
          ></template
        ></ui-table-column
      ><ui-table-column prop="protocol" label="协议" min-width="130" show-overflow-tooltip />
      <ui-table-column label="解析状态" width="145"
        ><template #default="{ row }"><StatusDot :tone="parseState(row).tone" :label="parseState(row).label" /></template></ui-table-column
      ><ui-table-column label="大小" width="90"
        ><template #default="{ row }">{{ row.payloadSize }} 字节</template></ui-table-column
      >
      <ui-table-column label="操作" fixed="right" width="110" align="right"
        ><template #default="{ row }"><RowActions :actions="rowActions(row)" /></template
      ></ui-table-column>
    </ui-table>
  </DataTableCard>
  <ui-dialog v-model="detailVisible" title="报文详情与解析结果" width="min(900px, 94vw)">
    <ui-descriptions v-if="detail" :column="2" border
      ><ui-descriptions-item label="消息标识">{{ detail.message?.messageId }}</ui-descriptions-item
      ><ui-descriptions-item label="解析状态"
        ><ui-tag :type="detailParseTag(detail.parseStatus).type" round>{{
          detailParseTag(detail.parseStatus).label
        }}</ui-tag></ui-descriptions-item
      ><ui-descriptions-item label="设备 / 产品"
        ><span>{{ deviceName(detail.message?.deviceId, detail.message?.deviceName) || detail.message?.deviceId }}</span
        ><small v-if="deviceName(detail.message?.deviceId, detail.message?.deviceName)" class="subline raw-id">{{
          detail.message?.deviceId
        }}</small
        ><small class="subline raw-id"
          >产品：{{ productName(detail.message?.productId) || detail.message?.productId
          }}<template v-if="productName(detail.message?.productId)"> · {{ detail.message?.productId }}</template></small
        ></ui-descriptions-item
      ><ui-descriptions-item label="接收时间">{{ formatTime(detail.message?.receivedAt) }}</ui-descriptions-item
      ><ui-descriptions-item label="协议 / 格式"
        >{{ transportLabel(detail.message?.protocol) }} / {{ formatLabel(detail.message?.payloadFormat) }}</ui-descriptions-item
      ><ui-descriptions-item label="解析器"
        >{{ detail.standardMessage?.parser || '—' }} {{ detail.standardMessage?.parserVersion || '' }}</ui-descriptions-item
      ><ui-descriptions-item label="完整性校验摘要" :span="2"
        ><code class="break-all">{{ detail.archive?.payloadHash }}</code></ui-descriptions-item
      ></ui-descriptions
    >
    <ui-alert
      v-if="detail && detail.parseStatus !== 'PARSED'"
      class="top-gap"
      title="当前没有可展示的标准解析结果"
      :description="detail.parseError || '可能仍在异步处理，或该协议包没有匹配的解析器。请检查协议开发中的样本调试结果。'"
      :type="detail.parseStatus === 'FAILED' ? 'error' : 'warning'"
      :closable="false"
      show-icon
    />
    <pre v-if="detail && detail.parseStatus !== 'PARSED'">{{ pretty(detail.message) }}</pre>
    <ui-tabs v-else v-model="detailTab" class="top-gap"
      ><ui-tab-pane name="parsed" label="标准解析结果">
        <pre>{{ pretty(detail?.standardMessage) }}</pre></ui-tab-pane
      ><ui-tab-pane name="raw" label="原始报文">
        <pre>{{ pretty(detail?.message) }}</pre>
      </ui-tab-pane></ui-tabs
    >
    <template #footer
      ><ui-button @click="detailVisible = false">关闭</ui-button
      ><ui-button
        v-permission="'GET /api/v1/raw-messages/:id/download'"
        type="primary"
        :loading="downloading === detail?.message?.messageId"
        :disabled="Boolean(downloading)"
        @click="downloadOne(detail.message.messageId)"
        >下载原始报文</ui-button
      ></template
    >
  </ui-dialog>
</template>

<style scoped>
.raw-filters {
  margin-bottom: var(--space-4);
  padding: var(--space-4);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg, 12px);
  background: var(--surface);
}
.raw-filter-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: var(--space-3);
}
.raw-filter-grid > label {
  display: grid;
  gap: var(--space-2);
  min-width: 0;
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}
.raw-filter-grid :deep(.ui-input),
.raw-filter-grid :deep(.ui-select),
.raw-filter-grid :deep(.ui-date-range) {
  width: 100%;
  min-width: 0;
}
.raw-filter-range {
  grid-column: span 2;
}
.raw-quick-time {
  display: flex;
  align-items: center;
  align-self: end;
  flex-wrap: wrap;
  gap: 8px;
  min-width: 0;
  min-height: 34px;
  font-size: var(--font-size-xs);
  color: var(--text-muted);
}
.raw-filter-advanced {
  margin-top: var(--space-3);
  padding-top: var(--space-3);
  border-top: 1px solid var(--border);
}
.raw-filter-actions {
  margin-top: var(--space-4);
  margin-bottom: 0;
}
@media (max-width: 900px) {
  .raw-filter-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .raw-quick-time {
    grid-column: 1 / -1;
  }
}
@media (max-width: 600px) {
  .raw-filter-grid {
    grid-template-columns: minmax(0, 1fr);
  }
  .raw-filter-range {
    grid-column: auto;
  }
}

.raw-id {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.raw-hint {
  margin: 0;
  padding: var(--space-2) var(--space-4);
  color: var(--text-muted);
  font-size: var(--font-size-xs);
  border-bottom: 1px solid var(--border);
}
</style>
