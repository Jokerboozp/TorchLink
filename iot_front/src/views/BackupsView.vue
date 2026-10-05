<script>
import { ref as moduleRef } from 'vue'
import { registerUnloadWarning } from '../composables/unsavedGuard.js'
// 备份、文件校验和恢复验证是同步长请求（最长 15 / 60 分钟）。正在执行的任务保存在模块级状态，
// 切换菜单再回来仍显示进行中并禁止重复触发；请求本身不随页面卸载中断。
const activeTask = moduleRef(null)
// 刷新或关闭浏览器会中断请求并中断服务端任务；任务进行中时无论当前在哪个页面都先提示。
registerUnloadWarning(() => Boolean(activeTask.value))
</script>

<script setup>
import { can } from '../permissions'
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
defineEmits(['navigate'])
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { UiMessage, UiMessageBox } from '../ui/feedback.js'
import { api, isAbort, notifyError, pretty, session } from '../api'
import { useListLoader } from '../composables/useListLoader'
import { formatElapsed } from '../ops/format.js'
import { downloadWithProgress } from '../ops/opsApi.js'
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
// 当前浏览器发起、尚未返回的备份类任务；只显示给发起它的租户与用户。
const owner = () => `${session.tenant}:${session.user}`
const busy = computed(() => (activeTask.value?.owner === owner() ? activeTask.value : null))
const now = ref(Date.now())
const busyElapsed = computed(() => (busy.value ? formatElapsed(now.value - busy.value.startedAt) : ''))
// 文件下载按“备份标识:文件名”分别记录已接收字节，不阻塞备份任务。
const downloads = reactive({})
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
const loader = useListLoader(loading)
const loadError = ref('')

const isAdmin = computed(() =>
  can([
    'POST /api/v1/backups',
    'POST /api/v1/backups/:id/restore-drill',
    'POST /api/v1/backups/:id/restore',
    'GET /api/v1/backups/:id/files/:filename',
    'DELETE /api/v1/backups/:id'
  ])
)
const runningCount = computed(() => records.value.filter(item => item.status === 'RUNNING').length)
const latestCompleted = computed(() =>
  records.value.find(
    item => item.status === 'COMPLETED' && ['DATABASE', 'FULL', 'DEVICE_DAILY', 'INCREMENTAL', 'RAW_LOGS'].includes(item.type)
  )
)

function statusType(value) {
  if (value === 'COMPLETED') return 'success'
  if (value === 'FAILED') return 'danger'
  if (value === 'RUNNING') return 'warning'
  return 'info'
}

function formatDate(value) {
  if (!value) return '—'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString('zh-CN', { hour12: false })
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

async function load(resetPage = false, silent = false) {
  if (resetPage) page.value = 1
  try {
    const query = new URLSearchParams({ page: String(page.value), pageSize: String(pageSize.value) })
    if (filters.type) query.set('type', filters.type)
    if (filters.status) query.set('status', filters.status)
    const data = await loader.run(signal => api(`/api/v1/backups?${query.toString()}`, { signal }), { silent })
    serviceMissing.value = false
    loadError.value = ''
    records.value = data.items || []
    total.value = Number(data.total ?? data.count ?? records.value.length)
  } catch (error) {
    if (isAbort(error) || silent) return
    serviceMissing.value = error?.status === 503 && /not configured/i.test(error.originalMessage || '')
    loadError.value = serviceMissing.value ? '' : error?.message || '备份记录读取失败'
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
    if (row.status === 'COMPLETED' && ['DATABASE', 'FULL', 'DEVICE_DAILY', 'INCREMENTAL', 'RAW_LOGS'].includes(row.type)) {
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

// runTask 同一时间只允许一个备份、文件校验或恢复验证请求；返回前其他触发入口全部停用。
async function runTask(task, work) {
  if (busy.value) return
  const entry = { ...task, owner: owner(), startedAt: Date.now() }
  activeTask.value = entry
  try {
    await work()
  } finally {
    if (activeTask.value === entry) activeTask.value = null
    load(false, true)
  }
}

function runBackup(type) {
  const name = label(backupTypes, type)
  return runTask({ key: `run:${type}`, label: name }, async () => {
    try {
      await api('/api/v1/backups', { method: 'POST', body: JSON.stringify({ type }) })
      UiMessage.success(`${name}已完成`)
    } catch (error) {
      notifyError(error)
    }
  })
}

async function restoreDrill(row) {
  if (busy.value) return
  try {
    await UiMessageBox.confirm(`将校验“${row.id}”中的备份文件，是否继续？`, '文件校验', {
      type: 'warning',
      confirmButtonText: '开始校验',
      cancelButtonText: '取消'
    })
  } catch {
    return
  }
  await runTask({ key: `drill:${row.id}`, label: '文件校验' }, async () => {
    try {
      const result = await api(`/api/v1/backups/${idPath(row.id)}/restore-drill`, { method: 'POST' })
      if (result.status === 'COMPLETED') UiMessage.success(`文件校验完成，已校验 ${result.artifactsChecked || 0} 个文件`)
      else UiMessage.error(`文件校验未通过，已校验 ${result.artifactsChecked || 0} 个文件，请在校验记录详情中查看原因`)
    } catch (error) {
      notifyError(error)
    }
  })
}

async function restoreToTarget(row) {
  if (busy.value) return
  try {
    await UiMessageBox.confirm(`将把“${row.id}”恢复到备份服务配置的独立恢复库并核对条数，不会写入当前业务库。是否继续？`, '恢复验证', {
      type: 'warning',
      confirmButtonText: '开始恢复',
      cancelButtonText: '取消'
    })
  } catch {
    return
  }
  await runTask({ key: `restore:${row.id}`, label: '恢复验证' }, async () => {
    try {
      const result = await api(`/api/v1/backups/${idPath(row.id)}/restore`, { method: 'POST' })
      const kinds = Object.values(result.kinds || {})
      const restored = kinds.reduce((sum, item) => sum + (item.restored || 0), 0)
      if (result.status === 'COMPLETED') UiMessage.success(`恢复验证完成：写入独立库 ${restored} 条，与备份清单一致`)
      else if (result.status === 'MISMATCH')
        UiMessage.warning(`恢复验证未通过：写入独立库 ${restored} 条，与备份清单条数不一致，请在恢复验证记录详情中核对`)
      else UiMessage.error(`恢复验证失败${result.error ? `：${result.error}` : ''}`)
    } catch (error) {
      notifyError(error)
    }
  })
}

const downloadKey = (row, artifact) => `${row.id}:${artifact.filename}`
function downloadText(row, artifact, idle = '下载') {
  const state = downloads[downloadKey(row, artifact)]
  if (!state) return idle
  if (state.total) return `下载中 ${Math.min(99, Math.floor((state.loaded / state.total) * 100))}%`
  return state.loaded ? `已接收 ${formatBytes(state.loaded)}` : '下载中…'
}

async function downloadArtifact(row, artifact) {
  const key = downloadKey(row, artifact)
  if (downloads[key]) return
  downloads[key] = { loaded: 0, total: 0 }
  try {
    await downloadWithProgress(
      `/api/v1/backups/${idPath(row.id)}/files/${encodeURIComponent(artifact.filename)}`,
      artifact.filename,
      (loaded, total) => {
        downloads[key] = { loaded, total }
      }
    )
    UiMessage.success(`${artifact.filename} 已取回，浏览器已开始保存`)
  } catch (error) {
    notifyError(error)
  } finally {
    delete downloads[key]
  }
}

// 任务进行中每秒更新已用时间，并定时静默刷新列表，使服务端写入的“执行中”记录及时出现。
let tickTimer = 0
let pollTimer = 0
function stopBusyTimers() {
  clearInterval(tickTimer)
  clearInterval(pollTimer)
  tickTimer = pollTimer = 0
}
watch(
  busy,
  value => {
    stopBusyTimers()
    if (!value) return
    now.value = Date.now()
    tickTimer = setInterval(() => (now.value = Date.now()), 1000)
    pollTimer = setInterval(() => load(false, true), 5000)
  },
  { immediate: true }
)
onBeforeUnmount(stopBusyTimers)

onMounted(load)
function removeBackup(row) {
  return confirmDelete({
    label: row.id,
    path: `/api/v1/backups/${idPath(row.id)}`,
    onDeleted: async () => {
      if (detail.value?.id === row.id) detailVisible.value = false
      await load()
    },
    warning: '备份记录、对象存储文件和本地副本将一并清理，删除后无法恢复。',
    blockedHint: '备份正在运行，或被文件校验记录引用；请先删除关联的校验记录。'
  })
}
const statusTone = value => ({ danger: 'danger', warning: 'warning', success: 'success', info: 'info' })[statusType(value)] || 'neutral'
function rowActions(row) {
  return [
    { key: 'detail', label: '详情 / 文件', onClick: () => showDetail(row) },
    {
      key: 'drill',
      label: '文件校验',
      permission: 'POST /api/v1/backups/:id/restore-drill',
      hidden:
        !isAdmin.value || row.status !== 'COMPLETED' || !['DATABASE', 'FULL', 'DEVICE_DAILY', 'INCREMENTAL', 'RAW_LOGS'].includes(row.type),
      loading: busy.value?.key === `drill:${row.id}`,
      disabled: Boolean(busy.value),
      onClick: () => restoreDrill(row)
    },
    {
      key: 'restore',
      label: '恢复验证',
      permission: 'POST /api/v1/backups/:id/restore',
      hidden:
        !isAdmin.value || row.status !== 'COMPLETED' || !['DATABASE', 'FULL', 'DEVICE_DAILY', 'INCREMENTAL', 'RAW_LOGS'].includes(row.type),
      loading: busy.value?.key === `restore:${row.id}`,
      disabled: Boolean(busy.value),
      onClick: () => restoreToTarget(row)
    },
    {
      key: 'delete',
      label: '删除',
      type: 'danger',
      permission: 'DELETE /api/v1/backups/:id',
      hidden: row.status === 'RUNNING',
      onClick: () => removeBackup(row)
    }
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
    <ui-button
      v-if="filters.type || filters.status"
      text
      @click="
        () => {
          filters.type = ''
          filters.status = ''
          load(true)
        }
      "
      >重置筛选</ui-button
    >
    <template #actions>
      <ui-button :loading="loading" @click="load()"><RefreshCw />刷新</ui-button>
      <template v-if="isAdmin">
        <ui-button
          v-permission="'POST /api/v1/backups'"
          :loading="busy?.key === 'run:DEVICE_DAILY'"
          :disabled="serviceMissing || Boolean(busy)"
          @click="runBackup('DEVICE_DAILY')"
          >备份昨日数据</ui-button
        >
        <ui-button
          v-permission="'POST /api/v1/backups'"
          :loading="busy?.key === 'run:DATABASE'"
          :disabled="serviceMissing || Boolean(busy)"
          @click="runBackup('DATABASE')"
          >立即整库备份</ui-button
        >
        <ui-button
          v-permission="'POST /api/v1/backups'"
          type="primary"
          :loading="busy?.key === 'run:FULL'"
          :disabled="serviceMissing || Boolean(busy)"
          @click="runBackup('FULL')"
          >立即完整备份</ui-button
        >
      </template>
    </template>
  </FilterBar>
  <ui-alert
    v-if="serviceMissing"
    class="backup-missing"
    title="当前部署未启用备份服务"
    description="备份记录与手动备份暂不可用。请在部署配置中启用备份服务（IOT_BACKUP_URL）后刷新。"
    type="warning"
    :closable="false"
    show-icon
  />
  <ui-alert
    v-if="busy"
    class="backup-missing"
    type="info"
    :title="`${busy.label}正在执行 · 已进行 ${busyElapsed}`"
    description="完成前不能再触发其他备份、文件校验或恢复验证。切换菜单不会中断任务；刷新或关闭页面会中断请求，可能导致任务失败。"
    :closable="false"
    show-icon
  />
  <p class="backup-hint">
    完整备份包含设备数据、知识库与原件、Agent 和会话；每日自动备份昨日设备数据。<template v-if="!isAdmin"
      >当前账号只能查看，不能手动触发备份或文件校验。</template
    >
  </p>

  <div class="backup-stat-grid">
    <ui-card shadow="never" class="surface-card"
      ><span>历史记录</span><strong>{{ total }}</strong
      ><small>设备数据备份与文件校验记录</small></ui-card
    >
    <ui-card shadow="never" class="surface-card"
      ><span>当前执行中</span><strong>{{ runningCount }}</strong
      ><small>备份任务正在进行时不可重复触发</small></ui-card
    >
    <ui-card shadow="never" class="surface-card"
      ><span>最近完成</span><strong>{{ latestCompleted ? label(backupTypes, latestCompleted.type) : '暂无' }}</strong
      ><small>{{ latestCompleted ? formatDate(latestCompleted.completedAt) : '等待首个成功任务' }}</small></ui-card
    >
  </div>

  <DataTableCard
    class="backup-table-card"
    :title="`备份记录 · ${total} 条`"
    :error="loadError"
    @retry="load()"
    :page="page"
    :page-size="pageSize"
    :total="total"
    @update:page="changePage"
    @update:page-size="changePageSize"
  >
    <ui-table v-loading="loading" :data="records">
      <ui-table-column label="类型" width="130"
        ><template #default="{ row }"
          ><ui-tag
            :type="row.type === 'FULL' || row.type === 'DATABASE' ? 'primary' : row.type === 'INCREMENTAL' ? 'success' : 'info'"
            round
            >{{ label(backupTypes, row.type) }}</ui-tag
          ></template
        ></ui-table-column
      >
      <ui-table-column label="任务标识" min-width="270"
        ><template #default="{ row }"
          ><code>{{ row.id }}</code></template
        ></ui-table-column
      >
      <ui-table-column label="状态" width="110"
        ><template #default="{ row }"><StatusDot :tone="statusTone(row.status)" :label="label(backupStatuses, row.status)" /></template
      ></ui-table-column>
      <ui-table-column label="开始时间" min-width="170"
        ><template #default="{ row }">{{ formatDate(row.startedAt) }}</template></ui-table-column
      >
      <ui-table-column label="完成时间" min-width="170"
        ><template #default="{ row }">{{ formatDate(row.completedAt) }}</template></ui-table-column
      >
      <ui-table-column label="清单校验摘要" min-width="170"
        ><template #default="{ row }"
          ><ui-tooltip v-if="row.checksum" :content="row.checksum"
            ><code>{{ row.checksum.slice(0, 12) }}…</code></ui-tooltip
          ><span v-else>—</span></template
        ></ui-table-column
      >
      <ui-table-column label="操作" fixed="right" width="200" align="right"
        ><template #default="{ row }"><RowActions :actions="rowActions(row)" /></template
      ></ui-table-column>
      <template #empty><ui-empty description="还没有备份记录；定时任务执行后会自动出现在这里" /></template>
    </ui-table>
  </DataTableCard>

  <ui-dialog
    v-model="detailVisible"
    :title="detail ? `${label(backupTypes, detail.type)} · ${detail.id}` : '备份详情'"
    width="min(1080px, 94vw)"
  >
    <ui-skeleton v-if="detailLoading" :rows="6" animated />
    <template v-else-if="detail">
      <ui-descriptions :column="2" border>
        <ui-descriptions-item label="任务标识">{{ detail.id }}</ui-descriptions-item>
        <ui-descriptions-item label="状态"
          ><ui-tag :type="statusType(detail.status)" round>{{ label(backupStatuses, detail.status) }}</ui-tag></ui-descriptions-item
        >
        <ui-descriptions-item label="开始时间">{{ formatDate(detail.startedAt) }}</ui-descriptions-item>
        <ui-descriptions-item label="完成时间">{{ formatDate(detail.completedAt) }}</ui-descriptions-item>
        <ui-descriptions-item label="对象存储清单" :span="2"
          ><code class="break-all">{{ detail.objectKey || '—' }}</code></ui-descriptions-item
        >
        <ui-descriptions-item label="清单完整性校验摘要" :span="2"
          ><code class="break-all">{{ detail.checksum || '—' }}</code></ui-descriptions-item
        >
      </ui-descriptions>
      <ui-alert
        v-if="detail.status === 'FAILED'"
        class="top-gap"
        type="error"
        title="备份任务失败"
        :description="detail.details?.error || '请查看 backup-service 日志'"
        :closable="false"
        show-icon
      />
      <template v-if="manifest">
        <div class="section-heading top-gap">
          <div><strong>备份文件</strong><span>清单中的每个文件都可以查看；文件下载和文件校验仅管理员可用</span></div>
          <ui-button
            v-permission="'GET /api/v1/backups/:id/files/:filename'"
            v-if="isAdmin"
            plain
            type="primary"
            :loading="Boolean(downloads[`${detail.id}:manifest.json`])"
            @click="downloadArtifact(detail, { filename: 'manifest.json' })"
            >{{ downloadText(detail, { filename: 'manifest.json' }, '下载文件清单') }}</ui-button
          >
        </div>
        <ui-table :data="manifest.artifacts" stripe>
          <ui-table-column label="组件" width="160"
            ><template #default="{ row }">{{ label(backupComponents, row.component, '其他组件') }}</template></ui-table-column
          >
          <ui-table-column prop="filename" label="文件名" min-width="240"
            ><template #default="{ row }"
              ><code>{{ row.filename }}</code></template
            ></ui-table-column
          >
          <ui-table-column label="大小" width="110"
            ><template #default="{ row }">{{ formatBytes(row.size) }}</template></ui-table-column
          >
          <ui-table-column label="完整性校验摘要" min-width="190"
            ><template #default="{ row }"
              ><ui-tooltip :content="row.sha256"
                ><code>{{ row.sha256?.slice(0, 12) }}…</code></ui-tooltip
              ></template
            ></ui-table-column
          >
          <ui-table-column label="操作" width="130" align="center"
            ><template #default="{ row }"
              ><ui-button
                v-permission="'GET /api/v1/backups/:id/files/:filename'"
                v-if="isAdmin"
                plain
                type="primary"
                :loading="Boolean(downloads[`${detail.id}:${row.filename}`])"
                @click="downloadArtifact(detail, row)"
                >{{ downloadText(detail, row) }}</ui-button
              ><span v-else class="muted-text">管理员可下载</span></template
            ></ui-table-column
          >
        </ui-table>
        <div class="list-pagination">
          <ui-pagination
            v-model:current-page="manifestPage"
            v-model:page-size="manifestPageSize"
            :total="manifestTotal"
            :page-sizes="[20, 50, 100]"
            layout="total, sizes, prev, pager, next, jumper"
            @current-change="changeManifestPage"
            @size-change="changeManifestPageSize"
          />
        </div>
        <div class="section-heading top-gap">
          <div><strong>组件说明</strong><span>由备份任务写入文件清单，用于确认本次备份覆盖范围</span></div>
        </div>
        <pre>{{ pretty(manifest.components) }}</pre>
      </template>
      <ui-tabs v-if="detail.details && !manifest" class="top-gap"
        ><ui-tab-pane label="任务详情">
          <pre>{{ pretty(detail.details) }}</pre>
        </ui-tab-pane></ui-tabs
      >
    </template>
    <template #footer><ui-button @click="detailVisible = false">关闭</ui-button></template>
  </ui-dialog>
</template>

<style scoped>
.backup-missing {
  margin-bottom: var(--space-4);
}
.backup-hint {
  margin: calc(-1 * var(--space-2)) 0 var(--space-4);
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}
.muted-text {
  color: var(--text-muted);
  font-size: 12px;
}
.backup-stat-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
  margin-bottom: 14px;
}
.backup-stat-grid :deep(.n-card) {
  border: 0;
  box-shadow: none;
  background: var(--surface-muted);
} /* 卡片样式命中当前 Naive UI 结构。 */
.backup-stat-grid :deep(.n-card-content) {
  min-height: 116px;
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: flex-start;
  gap: 7px;
} /* 标题、数值和说明分行排列。 */
.backup-stat-grid span {
  color: var(--text);
  font-size: 13px;
  font-weight: 700;
  line-height: 1.3;
}
.backup-stat-grid strong {
  color: var(--text);
  font-size: 28px;
  line-height: 1.1;
}
.backup-stat-grid small {
  color: var(--text);
  font-size: 12px;
  line-height: 1.5;
}
.backup-table-card :deep(.ui-table) {
  min-height: 280px;
}
.section-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.section-heading strong,
.section-heading span {
  display: block;
}
.section-heading span {
  color: var(--text-muted);
  margin-top: 4px;
} /* 备份详情说明文字在白底上保持可读。 */
@media (max-width: 900px) {
  .backup-stat-grid {
    grid-template-columns: 1fr;
  }
  .section-heading {
    align-items: flex-start;
    flex-direction: column;
  }
} /* 结束当前样式规则。 */
</style>
