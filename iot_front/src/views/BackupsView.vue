<script setup>
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate'])
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, download, notifyError, pretty, session } from '../api'
import { backupStatuses, backupTypes, backupComponents, label } from '../labels'

const filters = reactive({ type: '', status: '' })
const records = ref([])
const total = ref(0)
const loading = ref(false)
const actionLoading = ref('')
const detailVisible = ref(false)
const detailLoading = ref(false)
const detail = ref(null)
const manifest = ref(null)
const manifestPage = ref(1)
const manifestPageSize = ref(20)
const manifestTotal = ref(0)
const page = ref(1)
const pageSize = ref(20)

const isAdmin = computed(() => session.role === 'admin')
const runningCount = computed(() => records.value.filter(item => item.status === 'RUNNING').length)
const latestCompleted = computed(() => records.value.find(item => item.status === 'COMPLETED' && ['FULL', 'DEVICE_DAILY', 'INCREMENTAL', 'RAW_LOGS'].includes(item.type)))

function statusType(value) {
  if (value === 'COMPLETED') return 'success'
  if (value === 'FAILED') return 'danger'
  if (value === 'RUNNING') return 'warning'
  return 'info'
}

function formatDate(value) {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString('zh-CN', { hour12:false })
}

function formatBytes(value) {
  const size = Number(value || 0)
  if (size < 1024) return `${size} 字节`
  if (size < 1024 ** 2) return `${(size / 1024).toFixed(1)} 千字节`
  if (size < 1024 ** 3) return `${(size / 1024 ** 2).toFixed(1)} 兆字节`
  return `${(size / 1024 ** 3).toFixed(2)} 吉字节`
}

function idPath(value) {
  return encodeURIComponent(String(value || ''))
}

async function load(resetPage = false) {
  if (resetPage) page.value = 1
  loading.value = true
  try {
    const query = new URLSearchParams({ page: String(page.value), pageSize: String(pageSize.value) })
    if (filters.type) query.set('type', filters.type)
    if (filters.status) query.set('status', filters.status)
    const data = await api(`/api/v1/backups?${query.toString()}`)
    records.value = data.items || []
    total.value = Number(data.total || records.value.length)
  } catch (error) {
    notifyError(error)
  } finally {
    loading.value = false
  }
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

async function showDetail(row) {
  detailVisible.value = true
  detailLoading.value = true
  detail.value = null
  manifest.value = null
  manifestPage.value = 1
  manifestTotal.value = 0
  try {
    detail.value = await api(`/api/v1/backups/${idPath(row.id)}`)
    if (row.status === 'COMPLETED' && ['FULL', 'DEVICE_DAILY', 'INCREMENTAL', 'RAW_LOGS'].includes(row.type)) {
      await loadManifest(row.id)
    }
  } catch (error) {
    notifyError(error)
  } finally {
    detailLoading.value = false
  }
}

async function loadManifest(id) {
  const query = new URLSearchParams({ page: String(manifestPage.value), pageSize: String(manifestPageSize.value) })
  manifest.value = await api(`/api/v1/backups/${idPath(id)}/files?${query.toString()}`)
  manifestTotal.value = Number(manifest.value.total ?? manifest.value.artifacts?.length ?? 0)
}

function changeManifestPage(value) {
  manifestPage.value = value
  if (detail.value?.id) loadManifest(detail.value.id).catch(notifyError)
}

function changeManifestPageSize(value) {
  manifestPageSize.value = value
  manifestPage.value = 1
  if (detail.value?.id) loadManifest(detail.value.id).catch(notifyError)
}

async function runBackup(type) {
  actionLoading.value = `run:${type}`
  try {
    await api('/api/v1/backups', { method: 'POST', body: JSON.stringify({ type }) })
    ElMessage.success(`${label(backupTypes, type)}已完成`)
    await load()
  } catch (error) {
    notifyError(error)
  } finally {
    actionLoading.value = ''
  }
}

async function restoreDrill(row) {
  try {
    await ElMessageBox.confirm(`将校验“${row.id}”中的备份文件，是否继续？`, '文件校验', { type: 'warning', confirmButtonText: '开始校验', cancelButtonText: '取消' })
  } catch {
    return
  }
  actionLoading.value = `drill:${row.id}`
  try {
    const result = await api(`/api/v1/backups/${idPath(row.id)}/restore-drill`, { method: 'POST' })
    ElMessage.success(`文件校验完成，已校验 ${result.artifactsChecked || 0} 个文件`)
    await load()
  } catch (error) {
    notifyError(error)
  } finally {
    actionLoading.value = ''
  }
}

async function downloadArtifact(row, artifact) {
  const key = `${row.id}:${artifact.filename}`
  actionLoading.value = `download:${key}`
  try {
    await download(`/api/v1/backups/${idPath(row.id)}/files/${encodeURIComponent(artifact.filename)}`, artifact.filename)
    ElMessage.success(`已下载 ${artifact.filename}`)
  } catch (error) {
    notifyError(error)
  } finally {
    actionLoading.value = ''
  }
}

onMounted(load)
</script>

<template>
  <div class="page-toolbar backups-toolbar">
    <el-select v-model="filters.type" clearable placeholder="备份类型" @change="load(true)">
      <el-option v-for="(text, value) in backupTypes" :key="value" :label="text" :value="value" />
    </el-select>
    <el-select v-model="filters.status" clearable placeholder="执行状态" @change="load(true)">
      <el-option v-for="(text, value) in backupStatuses" :key="value" :label="text" :value="value" />
    </el-select>
    <el-button :loading="loading" @click="load()">刷新记录</el-button><el-button :disabled="!filters.type && !filters.status" @click="filters.type = ''; filters.status = ''; load(true)">重置筛选</el-button>
    <span class="toolbar-hint">仅备份设备原始报文与解析数据；每日自动备份昨日数据</span>
    <span v-if="!isAdmin" class="toolbar-hint">查看权限：当前账号不能手动触发备份或文件校验</span>
    <template v-if="isAdmin">
      <el-button type="primary" :loading="actionLoading === 'run:FULL'" @click="runBackup('FULL')">立即备份设备数据</el-button>
      <el-button type="warning" :loading="actionLoading === 'run:DEVICE_DAILY'" @click="runBackup('DEVICE_DAILY')">备份昨日数据</el-button>
    </template>
  </div>

  <div class="backup-stat-grid">
    <el-card shadow="never" class="surface-card"><span>历史记录</span><strong>{{ total }}</strong><small>设备数据备份与文件校验记录</small></el-card>
    <el-card shadow="never" class="surface-card"><span>当前执行中</span><strong>{{ runningCount }}</strong><small>备份任务正在进行时不可重复触发</small></el-card>
    <el-card shadow="never" class="surface-card"><span>最近完成</span><strong>{{ latestCompleted ? label(backupTypes, latestCompleted.type) : '暂无' }}</strong><small>{{ latestCompleted ? formatDate(latestCompleted.completedAt) : '等待首个成功任务' }}</small></el-card>
  </div>

  <el-card shadow="never" class="surface-card table-card backup-table-card">
    <el-table v-loading="loading" :data="records" stripe>
      <el-table-column label="类型" width="130"><template #default="{ row }"><el-tag :type="row.type === 'FULL' ? 'primary' : row.type === 'INCREMENTAL' ? 'success' : 'info'" round>{{ label(backupTypes, row.type) }}</el-tag></template></el-table-column>
      <el-table-column label="任务标识" min-width="270"><template #default="{ row }"><code>{{ row.id }}</code></template></el-table-column>
      <el-table-column label="状态" width="110"><template #default="{ row }"><el-tag :type="statusType(row.status)" round>{{ label(backupStatuses, row.status) }}</el-tag></template></el-table-column>
      <el-table-column label="开始时间" min-width="170"><template #default="{ row }">{{ formatDate(row.startedAt) }}</template></el-table-column>
      <el-table-column label="完成时间" min-width="170"><template #default="{ row }">{{ formatDate(row.completedAt) }}</template></el-table-column>
      <el-table-column label="清单校验摘要" min-width="170"><template #default="{ row }"><el-tooltip v-if="row.checksum" :content="row.checksum"><code>{{ row.checksum.slice(0, 12) }}…</code></el-tooltip><span v-else>—</span></template></el-table-column>
      <el-table-column label="操作" fixed="right" min-width="210" align="center"><template #default="{ row }"><div class="table-actions"><el-button plain type="primary" @click="showDetail(row)">详情 / 文件</el-button><el-button v-if="isAdmin && row.status === 'COMPLETED' && ['FULL', 'DEVICE_DAILY', 'INCREMENTAL', 'RAW_LOGS'].includes(row.type)" plain type="warning" :loading="actionLoading === `drill:${row.id}`" @click="restoreDrill(row)">文件校验</el-button></div></template></el-table-column>
    </el-table>
    <el-empty v-if="!loading && !records.length" description="还没有备份记录；定时任务执行后会自动出现在这里" />
    <div class="list-pagination">
      <el-pagination v-model:current-page="page" v-model:page-size="pageSize" :total="total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next, jumper" @current-change="changePage" @size-change="changePageSize" />
    </div>
  </el-card>

  <el-dialog v-model="detailVisible" :title="detail ? `${label(backupTypes, detail.type)} · ${detail.id}` : '备份详情'" width="min(1080px, 94vw)">
    <el-skeleton v-if="detailLoading" :rows="6" animated />
    <template v-else-if="detail">
      <el-descriptions :column="2" border>
        <el-descriptions-item label="任务标识">{{ detail.id }}</el-descriptions-item>
        <el-descriptions-item label="状态"><el-tag :type="statusType(detail.status)" round>{{ label(backupStatuses, detail.status) }}</el-tag></el-descriptions-item>
        <el-descriptions-item label="开始时间">{{ formatDate(detail.startedAt) }}</el-descriptions-item>
        <el-descriptions-item label="完成时间">{{ formatDate(detail.completedAt) }}</el-descriptions-item>
        <el-descriptions-item label="对象存储清单" :span="2"><code class="break-all">{{ detail.objectKey || '—' }}</code></el-descriptions-item>
        <el-descriptions-item label="清单完整性校验摘要" :span="2"><code class="break-all">{{ detail.checksum || '—' }}</code></el-descriptions-item>
      </el-descriptions>
      <el-alert v-if="detail.status === 'FAILED'" class="top-gap" type="error" title="备份任务失败" :description="detail.details?.error || '请查看 backup-service 日志'" :closable="false" show-icon />
      <template v-if="manifest">
        <div class="section-heading top-gap"><div><strong>备份文件</strong><span>清单中的每个文件都可以查看；文件下载和文件校验仅管理员可用</span></div><el-button v-if="isAdmin" plain type="primary" :loading="actionLoading === `download:${detail.id}:manifest.json`" @click="downloadArtifact(detail, { filename: 'manifest.json' })">下载文件清单</el-button></div>
        <el-table :data="manifest.artifacts" stripe>
          <el-table-column label="组件" width="160"><template #default="{row}">{{ label(backupComponents, row.component, '其他组件') }}</template></el-table-column>
          <el-table-column prop="filename" label="文件名" min-width="240"><template #default="{ row }"><code>{{ row.filename }}</code></template></el-table-column>
          <el-table-column label="大小" width="110"><template #default="{ row }">{{ formatBytes(row.size) }}</template></el-table-column>
          <el-table-column label="完整性校验摘要" min-width="190"><template #default="{ row }"><el-tooltip :content="row.sha256"><code>{{ row.sha256?.slice(0, 12) }}…</code></el-tooltip></template></el-table-column>
          <el-table-column label="操作" width="100" align="center"><template #default="{ row }"><el-button v-if="isAdmin" plain type="primary" :loading="actionLoading === `download:${detail.id}:${row.filename}`" @click="downloadArtifact(detail, row)">下载</el-button><span v-else class="muted-text">管理员可下载</span></template></el-table-column>
        </el-table>
        <div class="list-pagination">
          <el-pagination v-model:current-page="manifestPage" v-model:page-size="manifestPageSize" :total="manifestTotal" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next, jumper" @current-change="changeManifestPage" @size-change="changeManifestPageSize" />
        </div>
        <div class="section-heading top-gap"><div><strong>组件说明</strong><span>由备份任务写入文件清单，用于确认本次备份覆盖范围</span></div></div>
        <pre>{{ pretty(manifest.components) }}</pre>
      </template>
      <el-tabs v-if="detail.details && !manifest" class="top-gap"><el-tab-pane label="任务详情"><pre>{{ pretty(detail.details) }}</pre></el-tab-pane></el-tabs>
    </template>
    <template #footer><el-button @click="detailVisible = false">关闭</el-button></template>
  </el-dialog>
</template>

<style scoped>
.backups-toolbar { flex-wrap: wrap; }
.backups-toolbar .el-select { width: 150px; }
.toolbar-hint { color: var(--muted); }
.muted-text { color: var(--muted); font-size: 11px; }
.backup-stat-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 14px; margin-bottom: 14px; }
.backup-stat-grid :deep(.el-card) { border-color: #cbd5e1; box-shadow: 0 2px 8px rgba(15, 23, 42, .06); }
.backup-stat-grid :deep(.el-card__body) { min-height: 116px; display: flex; flex-direction: column; justify-content: center; gap: 7px; }
.backup-stat-grid span { color: var(--foreground); font-size: 13px; font-weight: 700; line-height: 1.3; }
.backup-stat-grid strong { color: var(--ink); font-size: 28px; line-height: 1.1; }
.backup-stat-grid small { color: #475569; font-size: 11px; line-height: 1.5; }
.backup-table-card :deep(.el-table) { min-height: 280px; }
.section-heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.section-heading strong, .section-heading span { display: block; }
.section-heading span { color: var(--muted); margin-top: 4px; }
@media (max-width: 900px) {
  .backup-stat-grid { grid-template-columns: 1fr; }
  .section-heading { align-items: flex-start; flex-direction: column; }
}
</style>
