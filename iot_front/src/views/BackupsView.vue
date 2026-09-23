<script setup>
import { can } from '../permissions' /* 引入当前代码需要的依赖。 */
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate']) /* 执行当前语句并推进处理流程。 */
import { computed, onMounted, reactive, ref } from 'vue' /* 引入当前代码需要的依赖。 */
import { UiMessage, UiMessageBox } from '../ui/feedback.js' /* 引入当前代码需要的依赖。 */
import { api, download, notifyError, pretty, session } from '../api' /* 引入当前代码需要的依赖。 */
import { backupStatuses, backupTypes, backupComponents, label } from '../labels' /* 引入当前代码需要的依赖。 */

const filters = reactive({ type: '', status: '' }) /* 声明 filters。 */
const records = ref([]) /* 声明 records。 */
const total = ref(0) /* 声明 total。 */
const loading = ref(false) /* 声明 loading。 */
const actionLoading = ref('') /* 声明 actionLoading。 */
const detailVisible = ref(false) /* 声明 detailVisible。 */
const detailLoading = ref(false) /* 声明 detailLoading。 */
const detail = ref(null) /* 声明 detail。 */
const manifest = ref(null) /* 声明 manifest。 */
const manifestPage = ref(1) /* 声明 manifestPage。 */
const manifestPageSize = ref(20) /* 声明 manifestPageSize。 */
const manifestTotal = ref(0) /* 声明 manifestTotal。 */
const page = ref(1) /* 声明 page。 */
const pageSize = ref(20) /* 声明 pageSize。 */

const isAdmin = computed(() => can(['POST /api/v1/backups','POST /api/v1/backups/:id/restore-drill','GET /api/v1/backups/:id/files/:filename'])) /* 声明 isAdmin。 */
const runningCount = computed(() => records.value.filter(item => item.status === 'RUNNING').length) /* 声明 runningCount。 */
const latestCompleted = computed(() => records.value.find(item => item.status === 'COMPLETED' && ['FULL', 'DEVICE_DAILY', 'INCREMENTAL', 'RAW_LOGS'].includes(item.type))) /* 声明 latestCompleted。 */

function statusType(value) { /* 定义 statusType 函数。 */
  if (value === 'COMPLETED') return 'success' /* 判断条件并选择处理分支。 */
  if (value === 'FAILED') return 'danger' /* 判断条件并选择处理分支。 */
  if (value === 'RUNNING') return 'warning' /* 判断条件并选择处理分支。 */
  return 'info' /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function formatDate(value) { /* 定义 formatDate 函数。 */
  if (!value) return '—' /* 判断条件并选择处理分支。 */
  const date = new Date(value) /* 声明 date。 */
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString('zh-CN', { hour12:false }) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function formatBytes(value) { /* 定义 formatBytes 函数。 */
  const size = Number(value || 0) /* 声明 size。 */
  if (size < 1024) return `${size} 字节` /* 判断条件并选择处理分支。 */
  if (size < 1024 ** 2) return `${(size / 1024).toFixed(1)} 千字节` /* 判断条件并选择处理分支。 */
  if (size < 1024 ** 3) return `${(size / 1024 ** 2).toFixed(1)} 兆字节` /* 判断条件并选择处理分支。 */
  return `${(size / 1024 ** 3).toFixed(2)} 吉字节` /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function idPath(value) { /* 定义 idPath 函数。 */
  return encodeURIComponent(String(value || '')) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

async function load(resetPage = false) { /* 定义 load 函数。 */
  if (resetPage) page.value = 1 /* 判断条件并选择处理分支。 */
  loading.value = true /* 更新 loading.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const query = new URLSearchParams({ page: String(page.value), pageSize: String(pageSize.value) }) /* 声明 query。 */
    if (filters.type) query.set('type', filters.type) /* 判断条件并选择处理分支。 */
    if (filters.status) query.set('status', filters.status) /* 判断条件并选择处理分支。 */
    const data = await api(`/api/v1/backups?${query.toString()}`) /* 声明 data。 */
    records.value = data.items || [] /* 更新 records.value 的值。 */
    total.value = Number(data.total || records.value.length) /* 更新 total.value 的值。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    notifyError(error) /* 执行当前语句并推进处理流程。 */
  } finally { /* 结束当前表达式或代码块。 */
    loading.value = false /* 更新 loading.value 的值。 */
  } /* 结束当前表达式或代码块。 */
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

async function showDetail(row) { /* 定义 showDetail 函数。 */
  detailVisible.value = true /* 更新 detailVisible.value 的值。 */
  detailLoading.value = true /* 更新 detailLoading.value 的值。 */
  detail.value = null /* 更新 detail.value 的值。 */
  manifest.value = null /* 更新 manifest.value 的值。 */
  manifestPage.value = 1 /* 更新 manifestPage.value 的值。 */
  manifestTotal.value = 0 /* 更新 manifestTotal.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    detail.value = await api(`/api/v1/backups/${idPath(row.id)}`) /* 更新 detail.value 的值。 */
    if (row.status === 'COMPLETED' && ['FULL', 'DEVICE_DAILY', 'INCREMENTAL', 'RAW_LOGS'].includes(row.type)) { /* 判断条件并选择处理分支。 */
      await loadManifest(row.id) /* 等待异步操作完成。 */
    } /* 结束当前表达式或代码块。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    notifyError(error) /* 执行当前语句并推进处理流程。 */
  } finally { /* 结束当前表达式或代码块。 */
    detailLoading.value = false /* 更新 detailLoading.value 的值。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function loadManifest(id) { /* 定义 loadManifest 函数。 */
  const query = new URLSearchParams({ page: String(manifestPage.value), pageSize: String(manifestPageSize.value) }) /* 声明 query。 */
  manifest.value = await api(`/api/v1/backups/${idPath(id)}/files?${query.toString()}`) /* 更新 manifest.value 的值。 */
  manifestTotal.value = Number(manifest.value.total ?? manifest.value.artifacts?.length ?? 0) /* 更新 manifestTotal.value 的值。 */
} /* 结束当前表达式或代码块。 */

function changeManifestPage(value) { /* 定义 changeManifestPage 函数。 */
  manifestPage.value = value /* 更新 manifestPage.value 的值。 */
  if (detail.value?.id) loadManifest(detail.value.id).catch(notifyError) /* 判断条件并选择处理分支。 */
} /* 结束当前表达式或代码块。 */

function changeManifestPageSize(value) { /* 定义 changeManifestPageSize 函数。 */
  manifestPageSize.value = value /* 更新 manifestPageSize.value 的值。 */
  manifestPage.value = 1 /* 更新 manifestPage.value 的值。 */
  if (detail.value?.id) loadManifest(detail.value.id).catch(notifyError) /* 判断条件并选择处理分支。 */
} /* 结束当前表达式或代码块。 */

async function runBackup(type) { /* 定义 runBackup 函数。 */
  actionLoading.value = `run:${type}` /* 更新 actionLoading.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    await api('/api/v1/backups', { method: 'POST', body: JSON.stringify({ type }) }) /* 等待异步操作完成。 */
    UiMessage.success(`${label(backupTypes, type)}已完成`) /* 执行当前语句并推进处理流程。 */
    await load() /* 等待异步操作完成。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    notifyError(error) /* 执行当前语句并推进处理流程。 */
  } finally { /* 结束当前表达式或代码块。 */
    actionLoading.value = '' /* 更新 actionLoading.value 的值。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function restoreDrill(row) { /* 定义 restoreDrill 函数。 */
  try { /* 执行当前语句并推进处理流程。 */
    await UiMessageBox.confirm(`将校验“${row.id}”中的备份文件，是否继续？`, '文件校验', { type: 'warning', confirmButtonText: '开始校验', cancelButtonText: '取消' }) /* 等待异步操作完成。 */
  } catch { /* 结束当前表达式或代码块。 */
    return /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
  actionLoading.value = `drill:${row.id}` /* 更新 actionLoading.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const result = await api(`/api/v1/backups/${idPath(row.id)}/restore-drill`, { method: 'POST' }) /* 声明 result。 */
    UiMessage.success(`文件校验完成，已校验 ${result.artifactsChecked || 0} 个文件`) /* 执行当前语句并推进处理流程。 */
    await load() /* 等待异步操作完成。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    notifyError(error) /* 执行当前语句并推进处理流程。 */
  } finally { /* 结束当前表达式或代码块。 */
    actionLoading.value = '' /* 更新 actionLoading.value 的值。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function downloadArtifact(row, artifact) { /* 定义 downloadArtifact 函数。 */
  const key = `${row.id}:${artifact.filename}` /* 声明 key。 */
  actionLoading.value = `download:${key}` /* 更新 actionLoading.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    await download(`/api/v1/backups/${idPath(row.id)}/files/${encodeURIComponent(artifact.filename)}`, artifact.filename) /* 等待异步操作完成。 */
    UiMessage.success(`已下载 ${artifact.filename}`) /* 执行当前语句并推进处理流程。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    notifyError(error) /* 执行当前语句并推进处理流程。 */
  } finally { /* 结束当前表达式或代码块。 */
    actionLoading.value = '' /* 更新 actionLoading.value 的值。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

onMounted(load) /* 执行当前语句并推进处理流程。 */
</script>

<template>
  <div class="page-toolbar backups-toolbar"> <!-- 渲染 div 界面元素。 -->
    <ui-select v-model="filters.type" clearable placeholder="备份类型" @change="load(true)"> <!-- 渲染 ui-select 界面元素。 -->
      <ui-option v-for="(text, value) in backupTypes" :key="value" :label="text" :value="value" /> <!-- 渲染 ui-option 界面元素。 -->
    </ui-select> <!-- 结束当前界面区域。 -->
    <ui-select v-model="filters.status" clearable placeholder="执行状态" @change="load(true)"> <!-- 渲染 ui-select 界面元素。 -->
      <ui-option v-for="(text, value) in backupStatuses" :key="value" :label="text" :value="value" /> <!-- 渲染 ui-option 界面元素。 -->
    </ui-select> <!-- 结束当前界面区域。 -->
    <ui-button :loading="loading" @click="load()">刷新记录</ui-button><ui-button :disabled="!filters.type && !filters.status" @click="filters.type = ''; filters.status = ''; load(true)">重置筛选</ui-button> <!-- 渲染 ui-button 界面元素。 -->
    <span class="toolbar-hint">仅备份设备原始报文与解析数据；每日自动备份昨日数据</span> <!-- 渲染 span 界面元素。 -->
    <span v-if="!isAdmin" class="toolbar-hint">查看权限：当前账号不能手动触发备份或文件校验</span> <!-- 渲染 span 界面元素。 -->
    <template v-if="isAdmin">
      <ui-button v-permission="'POST /api/v1/backups'" type="primary" :loading="actionLoading === 'run:FULL'" @click="runBackup('FULL')">立即备份设备数据</ui-button> <!-- 渲染 ui-button 界面元素。 -->
      <ui-button v-permission="'POST /api/v1/backups'" type="warning" :loading="actionLoading === 'run:DEVICE_DAILY'" @click="runBackup('DEVICE_DAILY')">备份昨日数据</ui-button> <!-- 渲染 ui-button 界面元素。 -->
    </template>
  </div>

  <div class="backup-stat-grid">
    <ui-card shadow="never" class="surface-card"><span>历史记录</span><strong>{{ total }}</strong><small>设备数据备份与文件校验记录</small></ui-card>
    <ui-card shadow="never" class="surface-card"><span>当前执行中</span><strong>{{ runningCount }}</strong><small>备份任务正在进行时不可重复触发</small></ui-card>
    <ui-card shadow="never" class="surface-card"><span>最近完成</span><strong>{{ latestCompleted ? label(backupTypes, latestCompleted.type) : '暂无' }}</strong><small>{{ latestCompleted ? formatDate(latestCompleted.completedAt) : '等待首个成功任务' }}</small></ui-card>
  </div>

  <ui-card shadow="never" class="surface-card table-card backup-table-card">
    <ui-table v-loading="loading" :data="records" stripe>
      <ui-table-column label="类型" width="130"><template #default="{ row }"><ui-tag :type="row.type === 'FULL' ? 'primary' : row.type === 'INCREMENTAL' ? 'success' : 'info'" round>{{ label(backupTypes, row.type) }}</ui-tag></template></ui-table-column>
      <ui-table-column label="任务标识" min-width="270"><template #default="{ row }"><code>{{ row.id }}</code></template></ui-table-column>
      <ui-table-column label="状态" width="110"><template #default="{ row }"><ui-tag :type="statusType(row.status)" round>{{ label(backupStatuses, row.status) }}</ui-tag></template></ui-table-column>
      <ui-table-column label="开始时间" min-width="170"><template #default="{ row }">{{ formatDate(row.startedAt) }}</template></ui-table-column>
      <ui-table-column label="完成时间" min-width="170"><template #default="{ row }">{{ formatDate(row.completedAt) }}</template></ui-table-column>
      <ui-table-column label="清单校验摘要" min-width="170"><template #default="{ row }"><ui-tooltip v-if="row.checksum" :content="row.checksum"><code>{{ row.checksum.slice(0, 12) }}…</code></ui-tooltip><span v-else>—</span></template></ui-table-column>
      <ui-table-column label="操作" fixed="right" min-width="210" align="center"><template #default="{ row }"><div class="table-actions"><ui-button plain type="primary" @click="showDetail(row)">详情 / 文件</ui-button><ui-button v-permission="'POST /api/v1/backups/:id/restore-drill'" v-if="isAdmin && row.status === 'COMPLETED' && ['FULL', 'DEVICE_DAILY', 'INCREMENTAL', 'RAW_LOGS'].includes(row.type)" plain type="warning" :loading="actionLoading === `drill:${row.id}`" @click="restoreDrill(row)">文件校验</ui-button></div></template></ui-table-column>
    </ui-table>
    <ui-empty v-if="!loading && !records.length" description="还没有备份记录；定时任务执行后会自动出现在这里" />
    <div class="list-pagination">
      <ui-pagination v-model:current-page="page" v-model:page-size="pageSize" :total="total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next, jumper" @current-change="changePage" @size-change="changePageSize" />
    </div>
  </ui-card>

  <ui-dialog v-model="detailVisible" :title="detail ? `${label(backupTypes, detail.type)} · ${detail.id}` : '备份详情'" width="min(1080px, 94vw)">
    <ui-skeleton v-if="detailLoading" :rows="6" animated />
    <template v-else-if="detail">
      <ui-descriptions :column="2" border> <!-- 渲染 ui-descriptions 界面元素。 -->
        <ui-descriptions-item label="任务标识">{{ detail.id }}</ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
        <ui-descriptions-item label="状态"><ui-tag :type="statusType(detail.status)" round>{{ label(backupStatuses, detail.status) }}</ui-tag></ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
        <ui-descriptions-item label="开始时间">{{ formatDate(detail.startedAt) }}</ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
        <ui-descriptions-item label="完成时间">{{ formatDate(detail.completedAt) }}</ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
        <ui-descriptions-item label="对象存储清单" :span="2"><code class="break-all">{{ detail.objectKey || '—' }}</code></ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
        <ui-descriptions-item label="清单完整性校验摘要" :span="2"><code class="break-all">{{ detail.checksum || '—' }}</code></ui-descriptions-item> <!-- 渲染 ui-descriptions-item 界面元素。 -->
      </ui-descriptions> <!-- 结束当前界面区域。 -->
      <ui-alert v-if="detail.status === 'FAILED'" class="top-gap" type="error" title="备份任务失败" :description="detail.details?.error || '请查看 backup-service 日志'" :closable="false" show-icon /> <!-- 渲染 ui-alert 界面元素。 -->
      <template v-if="manifest">
        <div class="section-heading top-gap"><div><strong>备份文件</strong><span>清单中的每个文件都可以查看；文件下载和文件校验仅管理员可用</span></div><ui-button v-permission="'GET /api/v1/backups/:id/files/:filename'" v-if="isAdmin" plain type="primary" :loading="actionLoading === `download:${detail.id}:manifest.json`" @click="downloadArtifact(detail, { filename: 'manifest.json' })">下载文件清单</ui-button></div> <!-- 渲染 div 界面元素。 -->
        <ui-table :data="manifest.artifacts" stripe> <!-- 渲染 ui-table 界面元素。 -->
          <ui-table-column label="组件" width="160"><template #default="{row}">{{ label(backupComponents, row.component, '其他组件') }}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
          <ui-table-column prop="filename" label="文件名" min-width="240"><template #default="{ row }"><code>{{ row.filename }}</code></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
          <ui-table-column label="大小" width="110"><template #default="{ row }">{{ formatBytes(row.size) }}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
          <ui-table-column label="完整性校验摘要" min-width="190"><template #default="{ row }"><ui-tooltip :content="row.sha256"><code>{{ row.sha256?.slice(0, 12) }}…</code></ui-tooltip></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
          <ui-table-column label="操作" width="100" align="center"><template #default="{ row }"><ui-button v-permission="'GET /api/v1/backups/:id/files/:filename'" v-if="isAdmin" plain type="primary" :loading="actionLoading === `download:${detail.id}:${row.filename}`" @click="downloadArtifact(detail, row)">下载</ui-button><span v-else class="muted-text">管理员可下载</span></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
        </ui-table> <!-- 结束当前界面区域。 -->
        <div class="list-pagination"> <!-- 渲染 div 界面元素。 -->
          <ui-pagination v-model:current-page="manifestPage" v-model:page-size="manifestPageSize" :total="manifestTotal" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next, jumper" @current-change="changeManifestPage" @size-change="changeManifestPageSize" /> <!-- 渲染 ui-pagination 界面元素。 -->
        </div> <!-- 结束当前界面区域。 -->
        <div class="section-heading top-gap"><div><strong>组件说明</strong><span>由备份任务写入文件清单，用于确认本次备份覆盖范围</span></div></div> <!-- 渲染 div 界面元素。 -->
        <pre>{{ pretty(manifest.components) }}</pre> <!-- 渲染 pre 界面元素。 -->
      </template>
      <ui-tabs v-if="detail.details && !manifest" class="top-gap"><ui-tab-pane label="任务详情"><pre>{{ pretty(detail.details) }}</pre></ui-tab-pane></ui-tabs>
    </template>
    <template #footer><ui-button @click="detailVisible = false">关闭</ui-button></template>
  </ui-dialog> <!-- 结束当前界面区域。 -->
</template>

<style scoped>
.backups-toolbar { flex-wrap: wrap; } /* 定义当前元素的样式规则。 */
.backups-toolbar .el-select { width: 150px; } /* 定义当前元素的样式规则。 */
.toolbar-hint { color: var(--muted-foreground); } /* 定义当前元素的样式规则。 */
.muted-text { color: var(--muted-foreground); font-size: 12px; } /* 定义当前元素的样式规则。 */
.backup-stat-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 14px; margin-bottom: 14px; } /* 定义当前元素的样式规则。 */
.backup-stat-grid :deep(.n-card) { border: 0; box-shadow: none; background: #f5f7fa; } /* 卡片样式命中当前 Naive UI 结构。 */
.backup-stat-grid :deep(.n-card-content) { min-height: 116px; display: flex; flex-direction: column; justify-content: center; align-items: flex-start; gap: 7px; } /* 标题、数值和说明分行排列。 */
.backup-stat-grid span { color: var(--foreground); font-size: 13px; font-weight: 700; line-height: 1.3; } /* 定义当前元素的样式规则。 */
.backup-stat-grid strong { color: var(--ink); font-size: 28px; line-height: 1.1; } /* 定义当前元素的样式规则。 */
.backup-stat-grid small { color: #475569; font-size: 12px; line-height: 1.5; } /* 定义当前元素的样式规则。 */
.backup-table-card :deep(.el-table) { min-height: 280px; } /* 定义当前元素的样式规则。 */
.section-heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; } /* 定义当前元素的样式规则。 */
.section-heading strong, .section-heading span { display: block; } /* 定义当前元素的样式规则。 */
.section-heading span { color: var(--muted-foreground); margin-top: 4px; } /* 备份详情说明文字在白底上保持可读。 */
@media (max-width: 900px) { /* 按屏幕条件调整样式。 */
  .backup-stat-grid { grid-template-columns: 1fr; } /* 定义当前元素的样式规则。 */
  .section-heading { align-items: flex-start; flex-direction: column; } /* 定义当前元素的样式规则。 */
} /* 结束当前样式规则。 */
</style>
