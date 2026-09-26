<script setup>
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate']) /* 执行当前语句并推进处理流程。 */
import { transportLabel, formatLabel } from '../presentation' /* 引入当前代码需要的依赖。 */
import { computed, onMounted, ref } from 'vue' /* 引入当前代码需要的依赖。 */
import { UiMessage } from '../ui/feedback.js' /* 引入当前代码需要的依赖。 */
import { api, download, formatTime, notifyError, pretty } from '../api' /* 引入当前代码需要的依赖。 */
import { messageTypeLabel, messageTypes } from '../labels' /* 引入当前代码需要的依赖。 */
import { Download, RefreshCw, Search, SlidersHorizontal } from '@lucide/vue'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'

const emptyFilters = () => ({ deviceId: '', messageId: '', productId: '', protocol: '', payloadFormat: '', parseStatus: '', messageType: '', parser: '', range: null })
const filters = ref(emptyFilters())
const appliedFilters = ref(emptyFilters())
const expanded = ref(false)
const loadError = ref('')
const parseStatuses = { PARSED: '已解析', FAILED: '解析失败', UNPARSED: '待解析 / 未匹配' }
const hasFilters = computed(() => Object.values(filters.value).some(Boolean) || Object.values(appliedFilters.value).some(Boolean))
const advancedCount = computed(() => ['productId', 'protocol', 'payloadFormat', 'messageType', 'parser'].filter(key => filters.value[key]).length)
const activeFilterCount = computed(() => Object.values(appliedFilters.value).filter(Boolean).length)
const items = ref([]) /* 声明 items。 */
const selection = ref([]) /* 声明 selection。 */
const loading = ref(false) /* 声明 loading。 */
const detail = ref(null) /* 声明 detail。 */
const detailVisible = ref(false) /* 声明 detailVisible。 */
const detailTab = ref('parsed') /* 每次查看报文都从解析结果开始，并显式同步页签状态。 */
const page = ref(1) /* 声明 page。 */
const pageSize = ref(20) /* 声明 pageSize。 */
const total = ref(0) /* 声明 total。 */
const selectedIds = computed(() => selection.value.map(item => item.messageId)) /* 声明 selectedIds。 */
let loadVersion = 0

async function load() { /* 定义 load 函数。 */
  const version = ++loadVersion
  loading.value = true
  loadError.value = ''
  selection.value = [] /* 更新 loading.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const params = new URLSearchParams({ page: String(page.value), pageSize: String(pageSize.value) }) /* 声明 params。 */
    for (const [key, value] of Object.entries(appliedFilters.value)) {
      if (key !== 'range' && value) params.set(key, value)
    }
    if (appliedFilters.value.range) {
      params.set('start', String(appliedFilters.value.range[0]))
      params.set('end', String(appliedFilters.value.range[1]))
    } /* 判断条件并选择处理分支。 */
    const data = await api(`/api/v1/raw-messages?${params.toString()}`) /* 声明 data。 */
    if (version !== loadVersion) return
    items.value = data.items || [] /* 更新 items.value 的值。 */
    total.value = Number(data.total ?? data.count ?? items.value.length) /* 更新 total.value 的值。 */
    selection.value = [] /* 更新 selection.value 的值。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    if (version === loadVersion) {
      items.value = []; total.value = 0
      loadError.value = error?.message || '原始报文查询失败'
      notifyError(error)
    } /* 执行当前语句并推进处理流程。 */
  } finally { /* 结束当前表达式或代码块。 */
    if (version === loadVersion) loading.value = false /* 更新 loading.value 的值。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function search() {
  const next = Object.fromEntries(Object.entries(filters.value).map(([key, value]) => [key, typeof value === 'string' ? value.trim() : value]))
  if (next.range && (next.range.length !== 2 || next.range.some(value => !Number.isFinite(value) || value < 0) || next.range[0] > next.range[1])) {
    UiMessage.warning('请选择有效的接收时间范围，开始时间不能晚于结束时间')
    return
  }
  appliedFilters.value = { ...next, range: next.range ? [...next.range] : null }
  page.value = 1 /* 更新 page.value 的值。 */
  await load() /* 等待异步操作完成。 */
} /* 结束当前表达式或代码块。 */

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

function changePage(value) { /* 定义 changePage 函数。 */
  page.value = value /* 更新 page.value 的值。 */
  load() /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

function changePageSize(value) { /* 定义 changePageSize 函数。 */
  pageSize.value = value /* 更新 pageSize.value 的值。 */
  page.value = 1 /* 更新 page.value 的值。 */
  load() /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

async function show(id) { /* 定义 show 函数。 */
  try { /* 执行当前语句并推进处理流程。 */
    detail.value = await api(`/api/v1/raw-messages/${encodeURIComponent(id)}`) /* 更新 detail.value 的值。 */
    detailTab.value = 'parsed'
    detailVisible.value = true /* 更新 detailVisible.value 的值。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    notifyError(error) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function downloadOne(id) { /* 定义 downloadOne 函数。 */
  try { /* 执行当前语句并推进处理流程。 */
    await download(`/api/v1/raw-messages/${encodeURIComponent(id)}/download`, `${id}.json`) /* 等待异步操作完成。 */
    UiMessage.success('报文已下载') /* 执行当前语句并推进处理流程。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    notifyError(error) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function downloadBatch() { /* 定义 downloadBatch 函数。 */
  if (!selectedIds.value.length) return /* 判断条件并选择处理分支。 */
  try { /* 执行当前语句并推进处理流程。 */
    const stamp = new Date().toISOString().replace(/[-:T]/g, '').slice(0, 14) /* 声明 stamp。 */
    await download('/api/v1/raw-messages/download', `原始报文_${stamp}_${selectedIds.value.length}条.zip`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ messageIds: selectedIds.value }) }) /* 等待异步操作完成。 */
    UiMessage.success(`已将 ${selectedIds.value.length} 条报文整合为压缩包`) /* 执行当前语句并推进处理流程。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    notifyError(error) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

onMounted(async () => { /* 执行当前语句并推进处理流程。 */
  let navigation = {} /* 声明 navigation。 */
  try { /* 执行当前语句并推进处理流程。 */
    const raw = sessionStorage.getItem('iot:navigation-detail') /* 声明 raw。 */
    if (raw) { /* 判断条件并选择处理分支。 */
      navigation = JSON.parse(raw) /* 更新 navigation 的值。 */
      filters.value.deviceId = navigation.deviceId || ''
      appliedFilters.value = { ...filters.value }
      sessionStorage.removeItem('iot:navigation-detail') /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
  } catch { /* 结束当前表达式或代码块。 */
    // Ignore malformed navigation state.
  } /* 结束当前表达式或代码块。 */
  await load() /* 等待异步操作完成。 */
  if (navigation.messageId) await show(navigation.messageId) /* 判断条件并选择处理分支。 */
}) /* 结束当前表达式或代码块。 */
function rowActions(row) {
  return [
    { key:'detail', label:'详情', onClick:() => show(row.messageId) },
    { key:'download', label:'下载', permission:'GET /api/v1/raw-messages/:id/download', onClick:() => downloadOne(row.messageId) }
  ]
}
</script>

<template>
  <form class="raw-filters" @submit.prevent="search">
    <div class="raw-filter-grid">
      <label>设备标识<ui-input v-model="filters.deviceId" clearable placeholder="输入完整设备标识" aria-label="设备标识" /></label>
      <label>报文标识<ui-input v-model="filters.messageId" clearable placeholder="输入完整原始报文标识" aria-label="报文标识" /></label>
      <label>解析状态<ui-select v-model="filters.parseStatus" clearable placeholder="全部解析状态" aria-label="解析状态"><ui-option v-for="(name, value) in parseStatuses" :key="value" :label="name" :value="value" /></ui-select></label>
      <label class="raw-filter-range">接收时间<ui-date-range v-model="filters.range" clearable aria-label="接收时间范围" /></label>
      <div class="raw-quick-time"><span>最近</span><ui-button text size="small" @click="recentHours(1)">1 小时</ui-button><ui-button text size="small" @click="recentHours(24)">24 小时</ui-button><ui-button text size="small" @click="recentHours(168)">7 天</ui-button><ui-button text size="small" @click="recentHours(0)">不限时间</ui-button></div>
    </div>
    <div v-if="expanded" class="raw-filter-grid raw-filter-advanced">
      <label>产品标识<ui-input v-model="filters.productId" clearable placeholder="输入完整产品标识" aria-label="产品标识" /></label>
      <label>协议<ui-input v-model="filters.protocol" clearable placeholder="如 json、gb26875" aria-label="协议" /></label>
      <label>报文格式<ui-select v-model="filters.payloadFormat" clearable filterable allow-create placeholder="全部格式，可输入其他格式" aria-label="报文格式"><ui-option v-for="value in ['json', 'hex', 'text', 'binary']" :key="value" :value="value" :label="value.toUpperCase()" /></ui-select></label>
      <label>消息类型<ui-select v-model="filters.messageType" clearable placeholder="全部消息类型" aria-label="消息类型"><ui-option v-for="(type, value) in messageTypes" :key="value" :label="type.label" :value="value" /></ui-select></label>
      <label>解析器<ui-input v-model="filters.parser" clearable placeholder="输入完整解析器名称" aria-label="解析器" /></label>
    </div>
    <FilterBar class="raw-filter-actions">
      <ui-button type="primary" native-type="submit" :loading="loading"><Search />查询</ui-button>
      <ui-button :disabled="!hasFilters" @click="resetFilters">重置筛选</ui-button>
      <ui-button text :aria-expanded="expanded" @click="expanded = !expanded"><SlidersHorizontal />{{ expanded ? '收起更多筛选' : '更多筛选' }}{{ advancedCount ? `（${advancedCount}）` : '' }}</ui-button>
      <template #actions>
        <ui-button v-permission="'POST /api/v1/raw-messages/download'" :disabled="loading || !selectedIds.length" @click="downloadBatch"><Download />批量下载（{{ selectedIds.length }}）</ui-button>
        <ui-button :loading="loading" @click="load"><RefreshCw />刷新</ui-button>
      </template>
    </FilterBar>
  </form>
  <ui-alert v-if="loadError" title="查询失败" :description="loadError" type="error" :closable="false" class="raw-query-error" />
  <DataTableCard :title="`原始报文 · ${total} 条`" :page="page" :page-size="pageSize" :total="total" @update:page="changePage" @update:page-size="changePageSize">
    <p class="raw-hint">{{ activeFilterCount ? `已应用 ${activeFilterCount} 项筛选；` : '' }}保留原文证据链；详情同时展示标准解析结果。</p>
    <ui-table v-loading="loading" :data="items" :empty-text="loadError ? '查询失败，请重试' : activeFilterCount ? '没有符合筛选条件的原始报文' : '暂无原始报文'" @selection-change="selection = $event"> <!-- 渲染 ui-table 界面元素。 -->
      <ui-table-column type="selection" width="48" /><ui-table-column label="接收时间" width="160"><template #default="{ row }">{{ formatTime(row.receivedAt) }}</template></ui-table-column><ui-table-column prop="messageId" label="消息标识" min-width="200" show-overflow-tooltip /><ui-table-column label="设备 / 产品" min-width="220"><template #default="{ row }"><span class="raw-id" :title="row.deviceId">{{ row.deviceId }}</span><small class="subline raw-id" :title="row.productId">{{ row.productId }}</small></template></ui-table-column><ui-table-column prop="protocol" label="协议" min-width="130" show-overflow-tooltip /> <!-- 渲染 ui-table-column 界面元素。 -->
      <ui-table-column label="解析状态" width="145"><template #default="{ row }"><StatusDot :tone="parseState(row).tone" :label="parseState(row).label" /></template></ui-table-column><ui-table-column label="大小" width="90"><template #default="{ row }">{{ row.payloadSize }} 字节</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
      <ui-table-column label="操作" fixed="right" width="110" align="right"><template #default="{ row }"><RowActions :actions="rowActions(row)" /></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
    </ui-table> <!-- 结束当前界面区域。 -->
  </DataTableCard>
  <ui-dialog v-model="detailVisible" title="报文详情与解析结果" width="min(900px, 94vw)"> <!-- 渲染 ui-dialog 界面元素。 -->
    <ui-descriptions v-if="detail" :column="2" border><ui-descriptions-item label="消息标识">{{ detail.message?.messageId }}</ui-descriptions-item><ui-descriptions-item label="解析状态"><ui-tag :type="detail.parseStatus === 'PARSED' ? 'success' : 'info'" round>{{ detail.parseStatus === 'PARSED' ? '已解析' : detail.parseStatus === 'FAILED' ? '解析失败' : '待解析/未匹配' }}</ui-tag></ui-descriptions-item><ui-descriptions-item label="设备 / 产品">{{ detail.message?.deviceId }} / {{ detail.message?.productId }}</ui-descriptions-item><ui-descriptions-item label="接收时间">{{ formatTime(detail.message?.receivedAt) }}</ui-descriptions-item><ui-descriptions-item label="协议 / 格式">{{ transportLabel(detail.message?.protocol) }} / {{ formatLabel(detail.message?.payloadFormat) }}</ui-descriptions-item><ui-descriptions-item label="解析器">{{ detail.standardMessage?.parser || '—' }} {{ detail.standardMessage?.parserVersion || '' }}</ui-descriptions-item><ui-descriptions-item label="完整性校验摘要" :span="2"><code class="break-all">{{ detail.archive?.payloadHash }}</code></ui-descriptions-item></ui-descriptions> <!-- 渲染 ui-descriptions 界面元素。 -->
    <ui-alert v-if="detail.parseStatus !== 'PARSED'" class="top-gap" title="当前没有可展示的标准解析结果" :description="detail.parseError || '可能仍在异步处理，或该协议包没有匹配的解析器。请检查协议开发中的样本调试结果。'" type="warning" :closable="false" show-icon /><pre v-if="detail.parseStatus !== 'PARSED'">{{pretty(detail.message)}}</pre><ui-tabs v-else v-model="detailTab" class="top-gap"><ui-tab-pane name="parsed" label="标准解析结果"><pre>{{ pretty(detail?.standardMessage) }}</pre></ui-tab-pane><ui-tab-pane name="raw" label="原始报文"><pre>{{ pretty(detail?.message) }}</pre></ui-tab-pane></ui-tabs> <!-- 渲染 ui-alert 界面元素。 -->
    <template #footer><ui-button @click="detailVisible = false">关闭</ui-button><ui-button v-permission="'GET /api/v1/raw-messages/:id/download'" type="primary" @click="downloadOne(detail.message.messageId)">下载原始报文</ui-button></template>
  </ui-dialog> <!-- 结束当前界面区域。 -->
</template>

<style scoped>
.raw-filters { margin-bottom: var(--space-4); padding: var(--space-4); border: 1px solid var(--border); border-radius: var(--radius-lg, 12px); background: var(--surface); }
.raw-filter-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--space-3); }
.raw-filter-grid > label { display: grid; gap: var(--space-2); min-width: 0; color: var(--text-muted); font-size: var(--font-size-sm); }
.raw-filter-grid :deep(.ui-input), .raw-filter-grid :deep(.ui-select), .raw-filter-grid :deep(.ui-date-range) { width: 100%; min-width: 0; }
.raw-filter-range { grid-column: span 2; }
.raw-quick-time { display: flex; align-items: center; align-self: end; flex-wrap: wrap; gap: 8px; min-width: 0; min-height: 34px; font-size: var(--font-size-xs); color: var(--text-muted); }
.raw-filter-advanced { margin-top: var(--space-3); padding-top: var(--space-3); border-top: 1px solid var(--border); }
.raw-filter-actions { margin-top: var(--space-4); margin-bottom: 0; }
.raw-query-error { margin-bottom: var(--space-3); }
@media (max-width: 900px) { .raw-filter-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } .raw-quick-time { grid-column: 1 / -1; } }
@media (max-width: 600px) { .raw-filter-grid { grid-template-columns: minmax(0, 1fr); } .raw-filter-range { grid-column: auto; } }

.raw-id { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.raw-hint { margin: 0; padding: var(--space-2) var(--space-4); color: var(--text-muted); font-size: var(--font-size-xs); border-bottom: 1px solid var(--border); }
</style>
