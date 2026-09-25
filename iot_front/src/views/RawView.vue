<script setup>
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate']) /* 执行当前语句并推进处理流程。 */
import { transportLabel, formatLabel } from '../presentation' /* 引入当前代码需要的依赖。 */
import { computed, onMounted, ref } from 'vue' /* 引入当前代码需要的依赖。 */
import { UiMessage } from '../ui/feedback.js' /* 引入当前代码需要的依赖。 */
import { api, download, formatTime, notifyError, pretty } from '../api' /* 引入当前代码需要的依赖。 */
import { messageTypeLabel } from '../labels' /* 引入当前代码需要的依赖。 */
import { Download, RefreshCw } from '@lucide/vue'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'

const query = ref('') /* 声明 query。 */
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
  loading.value = true /* 更新 loading.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const params = new URLSearchParams({ page: String(page.value), pageSize: String(pageSize.value) }) /* 声明 params。 */
    if (query.value) params.set('deviceId', query.value) /* 判断条件并选择处理分支。 */
    const data = await api(`/api/v1/raw-messages?${params.toString()}`) /* 声明 data。 */
    if (version !== loadVersion) return
    items.value = data.items || [] /* 更新 items.value 的值。 */
    total.value = Number(data.total ?? data.count ?? items.value.length) /* 更新 total.value 的值。 */
    selection.value = [] /* 更新 selection.value 的值。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    if (version === loadVersion) notifyError(error) /* 执行当前语句并推进处理流程。 */
  } finally { /* 结束当前表达式或代码块。 */
    if (version === loadVersion) loading.value = false /* 更新 loading.value 的值。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function search() { /* 定义 search 函数。 */
  page.value = 1 /* 更新 page.value 的值。 */
  await load() /* 等待异步操作完成。 */
} /* 结束当前表达式或代码块。 */

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
      query.value = navigation.deviceId || '' /* 更新 query.value 的值。 */
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
  <FilterBar>
    <ui-input v-model="query" clearable placeholder="按设备标识查询" aria-label="设备标识" @keyup.enter="search" @clear="search" />
    <ui-button v-if="query" text @click="query = ''; search()">重置筛选</ui-button>
    <template #actions>
      <ui-button v-permission="'POST /api/v1/raw-messages/download'" :disabled="!selectedIds.length" @click="downloadBatch"><Download />批量下载（{{ selectedIds.length }}）</ui-button>
      <ui-button :loading="loading" @click="search"><RefreshCw />刷新</ui-button>
    </template>
  </FilterBar>
  <DataTableCard :title="`原始报文 · ${total} 条`" :page="page" :page-size="pageSize" :total="total" @update:page="changePage" @update:page-size="changePageSize">
    <p class="raw-hint">保留原文证据链；详情同时展示标准解析结果。</p>
    <ui-table v-loading="loading" :data="items" :empty-text="query ? '没有该设备的原始报文' : '暂无原始报文'" @selection-change="selection = $event"> <!-- 渲染 ui-table 界面元素。 -->
      <ui-table-column type="selection" width="48" /><ui-table-column label="接收时间" min-width="170"><template #default="{ row }">{{ formatTime(row.receivedAt) }}</template></ui-table-column><ui-table-column prop="messageId" label="消息标识" min-width="220" /><ui-table-column prop="productId" label="产品" min-width="150" /><ui-table-column prop="deviceId" label="设备" min-width="170" /><ui-table-column prop="protocol" label="协议" width="100" /> <!-- 渲染 ui-table-column 界面元素。 -->
      <ui-table-column label="解析状态" width="145"><template #default="{ row }"><StatusDot :tone="row.parsed ? 'success' : 'neutral'" :label="row.parsed ? `已解析 · ${messageTypeLabel(row.parsedMessageType)}` : '待解析/未匹配'" /></template></ui-table-column><ui-table-column label="大小" width="90"><template #default="{ row }">{{ row.payloadSize }} 字节</template></ui-table-column><ui-table-column label="校验摘要" min-width="140"><template #default="{ row }"><ui-tooltip :content="row.payloadHash"><code>{{ row.payloadHash?.slice(0, 12) }}…</code></ui-tooltip></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
      <ui-table-column label="操作" fixed="right" width="130" align="right"><template #default="{ row }"><RowActions :actions="rowActions(row)" /></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
    </ui-table> <!-- 结束当前界面区域。 -->
  </DataTableCard>
  <ui-dialog v-model="detailVisible" title="报文详情与解析结果" width="min(900px, 94vw)"> <!-- 渲染 ui-dialog 界面元素。 -->
    <ui-descriptions v-if="detail" :column="2" border><ui-descriptions-item label="消息标识">{{ detail.message?.messageId }}</ui-descriptions-item><ui-descriptions-item label="解析状态"><ui-tag :type="detail.parseStatus === 'PARSED' ? 'success' : 'info'" round>{{ detail.parseStatus === 'PARSED' ? '已解析' : detail.parseStatus === 'FAILED' ? '解析失败' : '待解析/未匹配' }}</ui-tag></ui-descriptions-item><ui-descriptions-item label="设备 / 产品">{{ detail.message?.deviceId }} / {{ detail.message?.productId }}</ui-descriptions-item><ui-descriptions-item label="接收时间">{{ formatTime(detail.message?.receivedAt) }}</ui-descriptions-item><ui-descriptions-item label="协议 / 格式">{{ transportLabel(detail.message?.protocol) }} / {{ formatLabel(detail.message?.payloadFormat) }}</ui-descriptions-item><ui-descriptions-item label="解析器">{{ detail.standardMessage?.parser || '—' }} {{ detail.standardMessage?.parserVersion || '' }}</ui-descriptions-item><ui-descriptions-item label="完整性校验摘要" :span="2"><code class="break-all">{{ detail.archive?.payloadHash }}</code></ui-descriptions-item></ui-descriptions> <!-- 渲染 ui-descriptions 界面元素。 -->
    <ui-alert v-if="detail.parseStatus !== 'PARSED'" class="top-gap" title="当前没有可展示的标准解析结果" :description="detail.parseError || '可能仍在异步处理，或该协议包没有匹配的解析器。请检查协议开发中的样本调试结果。'" type="warning" :closable="false" show-icon /><pre v-if="detail.parseStatus !== 'PARSED'">{{pretty(detail.message)}}</pre><ui-tabs v-else v-model="detailTab" class="top-gap"><ui-tab-pane name="parsed" label="标准解析结果"><pre>{{ pretty(detail?.standardMessage) }}</pre></ui-tab-pane><ui-tab-pane name="raw" label="原始报文"><pre>{{ pretty(detail?.message) }}</pre></ui-tab-pane></ui-tabs> <!-- 渲染 ui-alert 界面元素。 -->
    <template #footer><ui-button @click="detailVisible = false">关闭</ui-button><ui-button v-permission="'GET /api/v1/raw-messages/:id/download'" type="primary" @click="downloadOne(detail.message.messageId)">下载原始报文</ui-button></template>
  </ui-dialog> <!-- 结束当前界面区域。 -->
</template>

<style scoped>
.raw-hint { margin: 0; padding: var(--space-2) var(--space-4); color: var(--text-muted); font-size: var(--font-size-xs); border-bottom: 1px solid var(--border); }
</style>
