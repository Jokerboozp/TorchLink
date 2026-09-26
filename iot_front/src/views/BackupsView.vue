<script setup>
import { can } from '../permissions'
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate'])
import { computed, onMounted, reactive, ref } from 'vue'
import { UiMessage, UiMessageBox } from '../ui/feedback.js'
import { api, download, notifyError, pretty } from '../api'
import { confirmDelete } from '../deleteAction'
import { backupStatuses, backupTypes, backupComponents, label } from '../labels'
import { RefreshCw } from '@lucide/vue'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'

const filters = reactive({ type: '', status: '' })
const records = ref([])
const total = ref(0)
const loading = ref(false)
const actionLoading = ref('')
// 部署未启用备份服务时明确提示，并停用触发入口，而不是弹出通用错误。
const serviceMissing = ref(false)
const detailVisible = ref(false)
const detailLoading = ref(false)
const detail = ref(null)
const manifest = ref(null)
const manifestPage = ref(1)
const manifestPageSize = ref(20)
const manifestTotal = ref(0)
const page = ref(1)
const pageSize = ref(20)
let loadVersion = 0

const isAdmin = computed(() => can(['POST /api/v1/backups','POST /api/v1/backups/:id/restore-drill','GET /api/v1/backups/:id/files/:filename','DELETE /api/v1/backups/:id']))
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
  const version = ++loadVersion
  if (resetPage) page.value = 1
  loading.value = true
  try {
    const query = new URLSearchParams({ page: String(page.value), pageSize: String(pageSize.value) })
    if (filters.type) query.set('type', filters.type)
    if (filters.status) query.set('status', filters.status)
    const data = await api(`/api/v1/backups?${query.toString()}`)
    if (version !== loadVersion) return
    serviceMissing.value = false
    records.value = data.items || []
    total.value = Number(data.total ?? data.count ?? records.value.length)
  } catch (error) {
    if (version !== loadVersion) return
    serviceMissing.value = error?.status === 503 && /not configured/i.test(error.originalMessage || '')
    if (!serviceMissing.value) notifyError(error)
  } finally {
    if (version === loadVersion) loading.value = false
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
    UiMessage.success(`${label(backupTypes, type)}已完成`)
    await load()
  } catch (error) {
    notifyError(error)
  } finally {
    actionLoading.value = ''
  }
}

async function restoreDrill(row) {
  try {
    await UiMessageBox.confirm(`将校验“${row.id}”中的备份文件，是否继续？`, '文件校验', { type: 'warning', confirmButtonText: '开始校验', cancelButtonText: '取消' })
  } catch {
    return
  }
  actionLoading.value = `drill:${row.id}`
  try {
    const result = await api(`/api/v1/backups/${idPath(row.id)}/restore-drill`, { method: 'POST' })
    UiMessage.success(`文件校验完成，已校验 ${result.artifactsChecked || 0} 个文件`)
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
    UiMessage.success(`已下载 ${artifact.filename}`)
  } catch (error) {
    notifyError(error)
  } finally {
    actionLoading.value = ''
  }
}

onMounted(load)
function removeBackup(row) { return confirmDelete({ label:row.id, path:`/api/v1/backups/${idPath(row.id)}`, onDeleted:async () => { if (detail.value?.id === row.id) detailVisible.value = false; await load() }, warning:'备份记录、对象存储文件和本地副本将一并清理，删除后无法恢复。', blockedHint:'备份正在运行，或被文件校验记录引用；请先删除关联的校验记录。' }) }
const statusTone = value => ({ danger:'danger', warning:'warning', success:'success', info:'info' })[statusType(value)] || 'neutral'
function rowActions(row) {
  return [
    { key:'detail', label:'详情 / 文件', onClick:() => showDetail(row) },
    { key:'drill', label:'文件校验', permission:'POST /api/v1/backups/:id/restore-drill', hidden:!isAdmin.value || row.status !== 'COMPLETED' || !['FULL', 'DEVICE_DAILY', 'INCREMENTAL', 'RAW_LOGS'].includes(row.type), loading:actionLoading.value === `drill:${row.id}`, onClick:() => restoreDrill(row) },
    { key:'delete', label:'删除', type:'danger', permission:'DELETE /api/v1/backups/:id', hidden:row.status === 'RUNNING', onClick:() => removeBackup(row) }
  ]
}
</script>

<template>
  <FilterBar>
    <ui-select v-model="filters.type" clearable placeholder="全部备份类型" aria-label="备份类型" @change="load(true)">
      <ui-option v-for="(text, value) in backupTypes" :key="value" :label="text" :value="value" />
    </ui-select>
    <ui-select v-model="filters.status" clearable placeholder="全部执行状态" aria-label="执行状态" @change="load(true)">
      <ui-option v-for="(text, value) in backupStatuses" :key="value" :label="text" :value="value" />
    </ui-select>
    <ui-button v-if="filters.type || filters.status" text @click="filters.type = ''; filters.status = ''; load(true)">重置筛选</ui-button>
    <template #actions>
      <ui-button :loading="loading" @click="load()"><RefreshCw />刷新</ui-button>
      <template v-if="isAdmin">
        <ui-button v-permission="'POST /api/v1/backups'" :loading="actionLoading === 'run:DEVICE_DAILY'" :disabled="serviceMissing" @click="runBackup('DEVICE_DAILY')">备份昨日数据</ui-button>
        <ui-button v-permission="'POST /api/v1/backups'" type="primary" :loading="actionLoading === 'run:FULL'" :disabled="serviceMissing" @click="runBackup('FULL')">立即备份设备数据</ui-button>
      </template>
    </template>
  </FilterBar>
  <ui-alert v-if="serviceMissing" class="backup-missing" title="当前部署未启用备份服务" description="备份记录与手动备份暂不可用。请在部署配置中启用备份服务（IOT_BACKUP_URL）后刷新。" type="warning" :closable="false" show-icon />
  <p class="backup-hint">仅备份设备原始报文与解析数据，每日自动备份昨日数据。<template v-if="!isAdmin">当前账号只能查看，不能手动触发备份或文件校验。</template></p>

  <div class="backup-stat-grid">
    <ui-card shadow="never" class="surface-card"><span>历史记录</span><strong>{{ total }}</strong><small>设备数据备份与文件校验记录</small></ui-card>
    <ui-card shadow="never" class="surface-card"><span>当前执行中</span><strong>{{ runningCount }}</strong><small>备份任务正在进行时不可重复触发</small></ui-card>
    <ui-card shadow="never" class="surface-card"><span>最近完成</span><strong>{{ latestCompleted ? label(backupTypes, latestCompleted.type) : '暂无' }}</strong><small>{{ latestCompleted ? formatDate(latestCompleted.completedAt) : '等待首个成功任务' }}</small></ui-card>
  </div>

  <DataTableCard class="backup-table-card" :title="`备份记录 · ${total} 条`" :page="page" :page-size="pageSize" :total="total" @update:page="changePage" @update:page-size="changePageSize">
    <ui-table v-loading="loading" :data="records">
      <ui-table-column label="类型" width="130"><template #default="{ row }"><ui-tag :type="row.type === 'FULL' ? 'primary' : row.type === 'INCREMENTAL' ? 'success' : 'info'" round>{{ label(backupTypes, row.type) }}</ui-tag></template></ui-table-column>
      <ui-table-column label="任务标识" min-width="270"><template #default="{ row }"><code>{{ row.id }}</code></template></ui-table-column>
      <ui-table-column label="状态" width="110"><template #default="{ row }"><StatusDot :tone="statusTone(row.status)" :label="label(backupStatuses, row.status)" /></template></ui-table-column>
      <ui-table-column label="开始时间" min-width="170"><template #default="{ row }">{{ formatDate(row.startedAt) }}</template></ui-table-column>
      <ui-table-column label="完成时间" min-width="170"><template #default="{ row }">{{ formatDate(row.completedAt) }}</template></ui-table-column>
      <ui-table-column label="清单校验摘要" min-width="170"><template #default="{ row }"><ui-tooltip v-if="row.checksum" :content="row.checksum"><code>{{ row.checksum.slice(0, 12) }}…</code></ui-tooltip><span v-else>—</span></template></ui-table-column>
      <ui-table-column label="操作" fixed="right" width="200" align="right"><template #default="{ row }"><RowActions :actions="rowActions(row)" /></template></ui-table-column>
      <template #empty><ui-empty description="还没有备份记录；定时任务执行后会自动出现在这里" /></template>
    </ui-table>
  </DataTableCard>

  <ui-dialog v-model="detailVisible" :title="detail ? `${label(backupTypes, detail.type)} · ${detail.id}` : '备份详情'" width="min(1080px, 94vw)">
    <ui-skeleton v-if="detailLoading" :rows="6" animated />
    <template v-else-if="detail">
      <ui-descriptions :column="2" border>
        <ui-descriptions-item label="任务标识">{{ detail.id }}</ui-descriptions-item>
        <ui-descriptions-item label="状态"><ui-tag :type="statusType(detail.status)" round>{{ label(backupStatuses, detail.status) }}</ui-tag></ui-descriptions-item>
        <ui-descriptions-item label="开始时间">{{ formatDate(detail.startedAt) }}</ui-descriptions-item>
        <ui-descriptions-item label="完成时间">{{ formatDate(detail.completedAt) }}</ui-descriptions-item>
        <ui-descriptions-item label="对象存储清单" :span="2"><code class="break-all">{{ detail.objectKey || '—' }}</code></ui-descriptions-item>
        <ui-descriptions-item label="清单完整性校验摘要" :span="2"><code class="break-all">{{ detail.checksum || '—' }}</code></ui-descriptions-item>
      </ui-descriptions>
      <ui-alert v-if="detail.status === 'FAILED'" class="top-gap" type="error" title="备份任务失败" :description="detail.details?.error || '请查看 backup-service 日志'" :closable="false" show-icon />
      <template v-if="manifest">
        <div class="section-heading top-gap"><div><strong>备份文件</strong><span>清单中的每个文件都可以查看；文件下载和文件校验仅管理员可用</span></div><ui-button v-permission="'GET /api/v1/backups/:id/files/:filename'" v-if="isAdmin" plain type="primary" :loading="actionLoading === `download:${detail.id}:manifest.json`" @click="downloadArtifact(detail, { filename: 'manifest.json' })">下载文件清单</ui-button></div>
        <ui-table :data="manifest.artifacts" stripe>
          <ui-table-column label="组件" width="160"><template #default="{row}">{{ label(backupComponents, row.component, '其他组件') }}</template></ui-table-column>
          <ui-table-column prop="filename" label="文件名" min-width="240"><template #default="{ row }"><code>{{ row.filename }}</code></template></ui-table-column>
          <ui-table-column label="大小" width="110"><template #default="{ row }">{{ formatBytes(row.size) }}</template></ui-table-column>
          <ui-table-column label="完整性校验摘要" min-width="190"><template #default="{ row }"><ui-tooltip :content="row.sha256"><code>{{ row.sha256?.slice(0, 12) }}…</code></ui-tooltip></template></ui-table-column>
          <ui-table-column label="操作" width="100" align="center"><template #default="{ row }"><ui-button v-permission="'GET /api/v1/backups/:id/files/:filename'" v-if="isAdmin" plain type="primary" :loading="actionLoading === `download:${detail.id}:${row.filename}`" @click="downloadArtifact(detail, row)">下载</ui-button><span v-else class="muted-text">管理员可下载</span></template></ui-table-column>
        </ui-table>
        <div class="list-pagination">
          <ui-pagination v-model:current-page="manifestPage" v-model:page-size="manifestPageSize" :total="manifestTotal" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next, jumper" @current-change="changeManifestPage" @size-change="changeManifestPageSize" />
        </div>
        <div class="section-heading top-gap"><div><strong>组件说明</strong><span>由备份任务写入文件清单，用于确认本次备份覆盖范围</span></div></div>
        <pre>{{ pretty(manifest.components) }}</pre>
      </template>
      <ui-tabs v-if="detail.details && !manifest" class="top-gap"><ui-tab-pane label="任务详情"><pre>{{ pretty(detail.details) }}</pre></ui-tab-pane></ui-tabs>
    </template>
    <template #footer><ui-button @click="detailVisible = false">关闭</ui-button></template>
  </ui-dialog>
</template>

<style scoped>
.backup-missing { margin-bottom: var(--space-4); }
.backup-hint { margin: calc(-1 * var(--space-2)) 0 var(--space-4); color: var(--text-muted); font-size: var(--font-size-sm); }
.muted-text { color: var(--text-muted); font-size: 12px; }
.backup-stat-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 14px; margin-bottom: 14px; }
.backup-stat-grid :deep(.n-card) { border: 0; box-shadow: none; background: var(--surface-muted); } /* 卡片样式命中当前 Naive UI 结构。 */
.backup-stat-grid :deep(.n-card-content) { min-height: 116px; display: flex; flex-direction: column; justify-content: center; align-items: flex-start; gap: 7px; } /* 标题、数值和说明分行排列。 */
.backup-stat-grid span { color: var(--text); font-size: 13px; font-weight: 700; line-height: 1.3; }
.backup-stat-grid strong { color: var(--text); font-size: 28px; line-height: 1.1; }
.backup-stat-grid small { color: var(--text); font-size: 12px; line-height: 1.5; }
.backup-table-card :deep(.ui-table) { min-height: 280px; }
.section-heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.section-heading strong, .section-heading span { display: block; }
.section-heading span { color: var(--text-muted); margin-top: 4px; } /* 备份详情说明文字在白底上保持可读。 */
@media (max-width: 900px) {
  .backup-stat-grid { grid-template-columns: 1fr; }
  .section-heading { align-items: flex-start; flex-direction: column; }
} /* 结束当前样式规则。 */
</style>
