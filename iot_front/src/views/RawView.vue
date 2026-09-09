<script setup>
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, download, formatTime, notifyError, pretty } from '../api'
import { messageTypeLabel } from '../labels'

const query = ref('')
const items = ref([])
const selection = ref([])
const loading = ref(false)
const detail = ref(null)
const detailVisible = ref(false)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const selectedIds = computed(() => selection.value.map(item => item.messageId))

async function load() {
  loading.value = true
  try {
    const params = new URLSearchParams({ page: String(page.value), pageSize: String(pageSize.value) })
    if (query.value) params.set('deviceId', query.value)
    const data = await api(`/api/v1/raw-messages?${params.toString()}`)
    items.value = data.items || []
    total.value = Number(data.total ?? data.count ?? items.value.length)
    selection.value = []
  } catch (error) {
    notifyError(error)
  } finally {
    loading.value = false
  }
}

async function search() {
  page.value = 1
  await load()
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

async function show(id) {
  try {
    detail.value = await api(`/api/v1/raw-messages/${encodeURIComponent(id)}`)
    detailVisible.value = true
  } catch (error) {
    notifyError(error)
  }
}

async function downloadOne(id) {
  try {
    await download(`/api/v1/raw-messages/${encodeURIComponent(id)}/download`, `${id}.json`)
    ElMessage.success('报文已下载')
  } catch (error) {
    notifyError(error)
  }
}

async function downloadBatch() {
  if (!selectedIds.value.length) return
  try {
    const stamp = new Date().toISOString().replace(/[-:T]/g, '').slice(0, 14)
    await download('/api/v1/raw-messages/download', `原始报文_${stamp}_${selectedIds.value.length}条.zip`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ messageIds: selectedIds.value }) })
    ElMessage.success(`已将 ${selectedIds.value.length} 条报文整合为压缩包`)
  } catch (error) {
    notifyError(error)
  }
}

onMounted(async () => {
  let navigation = {}
  try {
    const raw = sessionStorage.getItem('iot:navigation-detail')
    if (raw) {
      navigation = JSON.parse(raw)
      query.value = navigation.deviceId || ''
      sessionStorage.removeItem('iot:navigation-detail')
    }
  } catch {
    // Ignore malformed navigation state.
  }
  await load()
  if (navigation.messageId) await show(navigation.messageId)
})
</script>

<template>
  <div class="page-toolbar"><el-input v-model="query" clearable placeholder="设备 ID" @keyup.enter="search" /><el-button type="primary" :loading="loading" @click="search">查询报文</el-button><el-button :disabled="!query" @click="query = ''; search()">重置筛选</el-button><el-button :disabled="!selectedIds.length" @click="downloadBatch">批量下载（{{ selectedIds.length }}）</el-button><span>共 {{ total }} 条原始报文，保留证据链，详情同时展示标准解析结果</span></div>
  <el-card shadow="never" class="surface-card table-card">
    <el-table v-loading="loading" :data="items" stripe @selection-change="selection = $event">
      <el-table-column type="selection" width="48" /><el-table-column label="接收时间" min-width="170"><template #default="{ row }">{{ formatTime(row.receivedAt) }}</template></el-table-column><el-table-column prop="messageId" label="消息 ID" min-width="220" /><el-table-column prop="productId" label="产品" min-width="150" /><el-table-column prop="deviceId" label="设备" min-width="170" /><el-table-column prop="protocol" label="协议" width="100" />
      <el-table-column label="解析状态" width="145"><template #default="{ row }"><el-tag :type="row.parsed ? 'success' : 'info'" round>{{ row.parsed ? `已解析 · ${messageTypeLabel(row.parsedMessageType)}` : '待解析/未匹配' }}</el-tag></template></el-table-column><el-table-column label="大小" width="90"><template #default="{ row }">{{ row.payloadSize }} B</template></el-table-column><el-table-column label="校验摘要" min-width="140"><template #default="{ row }"><el-tooltip :content="row.payloadHash"><code>{{ row.payloadHash?.slice(0, 12) }}…</code></el-tooltip></template></el-table-column>
      <el-table-column label="操作" fixed="right" width="180" align="center"><template #default="{ row }"><div class="table-actions"><el-button plain type="primary" @click="show(row.messageId)">详情</el-button><el-button plain @click="downloadOne(row.messageId)">下载</el-button></div></template></el-table-column>
    </el-table>
    <div class="list-pagination">
      <el-pagination v-model:current-page="page" v-model:page-size="pageSize" :total="total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next, jumper" @current-change="changePage" @size-change="changePageSize" />
    </div>
  </el-card>
  <el-dialog v-model="detailVisible" title="报文详情与解析结果" width="min(900px, 94vw)">
    <el-descriptions v-if="detail" :column="2" border><el-descriptions-item label="消息 ID">{{ detail.message?.messageId }}</el-descriptions-item><el-descriptions-item label="解析状态"><el-tag :type="detail.parseStatus === 'PARSED' ? 'success' : 'info'" round>{{ detail.parseStatus === 'PARSED' ? '已解析' : detail.parseStatus === 'FAILED' ? '解析失败' : '待解析/未匹配' }}</el-tag></el-descriptions-item><el-descriptions-item label="设备 / 产品">{{ detail.message?.deviceId }} / {{ detail.message?.productId }}</el-descriptions-item><el-descriptions-item label="接收时间">{{ formatTime(detail.message?.receivedAt) }}</el-descriptions-item><el-descriptions-item label="协议 / 格式">{{ detail.message?.protocol }} / {{ detail.message?.payloadFormat }}</el-descriptions-item><el-descriptions-item label="解析器">{{ detail.standardMessage?.parser || '—' }} {{ detail.standardMessage?.parserVersion || '' }}</el-descriptions-item><el-descriptions-item label="SHA-256" :span="2"><code class="break-all">{{ detail.archive?.payloadHash }}</code></el-descriptions-item></el-descriptions>
    <el-alert v-if="detail.parseStatus !== 'PARSED'" class="top-gap" title="当前没有可展示的标准解析结果" :description="detail.parseError || '可能仍在异步处理，或该协议包没有匹配的解析器。请检查协议开发中的样本调试结果。'" type="warning" :closable="false" show-icon /><pre v-if="detail.parseStatus !== 'PARSED'">{{pretty(detail.message)}}</pre><el-tabs v-else class="top-gap"><el-tab-pane label="标准解析结果"><pre>{{ pretty(detail?.standardMessage) }}</pre></el-tab-pane><el-tab-pane label="原始报文"><pre>{{ pretty(detail?.message) }}</pre></el-tab-pane></el-tabs>
    <template #footer><el-button @click="detailVisible = false">关闭</el-button><el-button type="primary" @click="downloadOne(detail.message.messageId)">下载原始报文</el-button></template>
  </el-dialog>
</template>
