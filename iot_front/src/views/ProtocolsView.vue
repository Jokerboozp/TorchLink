<script setup>
import { takeNavigation } from '../routing'
// 页面统一接收父级导航事件，避免多根节点透传监听器警告。
const emit = defineEmits(['navigate'])
import AccessPointsPanel from '../components/AccessPointsPanel.vue'
import ProtocolAssistantView from './ProtocolAssistantView.vue'
import ProtocolSourceUpload from '../components/ProtocolSourceUpload.vue'
import ProtocolPreviewPanel from '../components/ProtocolPreviewPanel.vue'
import { transportLabel, statusLabel } from '../presentation'
import { computed, onMounted, ref, watch } from 'vue'
import { label, parsers } from '../labels'
import { UiMessage } from '../ui/feedback.js'
import { api, download, formatTime, isAbort, notifyError, pretty } from '../api'
import { useListLoader } from '../composables/useListLoader'
import { confirmClose, useUnsavedGuard } from '../composables/unsavedGuard.js'
import { confirmDelete } from '../deleteAction'
import { RefreshCw, Upload, Wand2 } from '@lucide/vue'
import DataTableCard from '../components/layout/DataTableCard.vue'
import FilterBar from '../components/layout/FilterBar.vue'
import RowActions from '../components/layout/RowActions.vue'
import StatusDot from '../components/layout/StatusDot.vue'
import { can } from '../permissions'

const protocols = ref([])
const protocolPage = ref(1),
  protocolPageSize = ref(20)
const pagedProtocols = computed(() =>
  protocols.value.slice((protocolPage.value - 1) * protocolPageSize.value, protocolPage.value * protocolPageSize.value)
)
watch(
  () => protocols.value.length,
  total => {
    protocolPage.value = Math.min(protocolPage.value, Math.max(1, Math.ceil(total / protocolPageSize.value)))
  }
)
const loading = ref(false)
const result = ref(null)
const compiling = ref(false)
const switching = ref(false)
const props = defineProps({ section: { type: String, default: 'protocols' } })
const sourceOpen = ref(false),
  assistantOpen = ref(false)
const previewOpen = ref(false),
  previewContext = ref(null)
const releaseOpen = ref(false),
  selectedProtocol = ref(null),
  selectedRelease = ref(null)
const versionsOpen = ref(false),
  managedProtocolId = ref('')
const managedProtocol = computed(() => protocols.value.find(item => item.definition.id === managedProtocolId.value) || null)
function manageVersions(row) {
  managedProtocolId.value = row.definition.id
  versionsOpen.value = true
}
function viewRelease(row, release) {
  versionsOpen.value = false
  previewOpen.value = false
  previewContext.value = null
  selectedProtocol.value = row.definition
  selectedRelease.value = { ...release, protocolId: row.definition.id }
  releaseOpen.value = true
}
const canTestMapping = computed(() =>
  Boolean(selectedRelease.value?.artifact?.generatedMapping && can('POST /api/v2/protocols/:id/releases/:version/preview'))
)
const canPreviewRelease = computed(() => {
  const release = selectedRelease.value
  return Boolean(
    release &&
    !release.artifact?.generatedMapping &&
    ['go_protocol_parser', 'iot_standard_parser'].includes(release.parserType) &&
    release.status !== 'REVOKED' &&
    can('POST /api/v2/protocols/:id/releases/:version/preview')
  )
})
const canPublishRelease = computed(
  () => selectedRelease.value?.status === 'VALIDATED' && can('POST /api/v2/protocols/:id/releases/:version/publish')
)
const canDownloadSource = computed(
  () => selectedRelease.value?.artifact?.build?.kind === 'go-source' && can('GET /api/v2/protocols/:id/releases/:version/source')
)
const hasReleaseActions = computed(
  () => canTestMapping.value || canPreviewRelease.value || canPublishRelease.value || canDownloadSource.value
)
const assistantRelease = ref(null),
  assistantName = ref(''),
  assistantState = ref({ working: false, dirty: false })
function openAssistant(release = null, name = '') {
  releaseOpen.value = false
  assistantRelease.value = release
  assistantName.value = name
  assistantState.value = { working: false, dirty: false }
  assistantOpen.value = true
}
// 生成或校验进行中、或草稿与字段映射尚未保存时，关闭弹窗、切换菜单和刷新页面都先确认。
useUnsavedGuard(() => assistantOpen.value && assistantState.value.dirty)
async function closeAssistant() {
  const { working, dirty } = assistantState.value
  const message = working
    ? '协议正在生成或校验，关闭后将中止当前请求，已填写的资料和字段映射也会丢失。确定关闭？'
    : '生成的协议草稿尚未保存，关闭后已编辑的字段映射将丢失。确定关闭？'
  if (!(await confirmClose(dirty, message))) return
  assistantOpen.value = false
  assistantState.value = { working: false, dirty: false }
}
function assistantNavigate(page) {
  assistantOpen.value = false
  assistantState.value = { working: false, dirty: false }
  emit('navigate', page)
}
// 最近一次源码上传或发布结果的中文摘要，原始数据折叠在下方。
const resultSummary = computed(() => {
  const release = result.value?.release || result.value
  if (!release) return []
  const rows = [
    ['协议版本', `${release.protocolId || '—'} @ ${release.version || '—'}`],
    ['状态', statusText(release.status)],
    ['运行方式', `${transportLabel(release.transport)} · ${label(parsers, release.parserType, '自定义协议程序')}`]
  ]
  if (release.createdAt) rows.push(['创建时间', formatTime(release.createdAt)])
  return rows
})
const releaseCount = computed(() => protocols.value.reduce((total, item) => total + (item.releases?.length || 0), 0))
const loader = useListLoader(loading)
const loadError = ref('')

async function load() {
  try {
    const catalog = await loader.run(signal => api('/api/v2/protocols', { signal }))
    protocols.value = catalog.items || []
    loadError.value = ''
  } catch (error) {
    if (!isAbort(error)) loadError.value = error?.message || '协议读取失败'
  }
}

async function publishRelease(protocolId, version) {
  switching.value = true
  try {
    await api(`/api/v2/protocols/${encodeURIComponent(protocolId)}/releases/${encodeURIComponent(version)}/publish`, {
      method: 'POST',
      body: '{}'
    })
    UiMessage.success('版本已发布，可用于设备模板')
    releaseOpen.value = false
    await load()
  } catch (error) {
    notifyError(error)
  } finally {
    switching.value = false
  }
}
async function downloadSourceRelease(protocolId, release) {
  try {
    await download(
      `/api/v2/protocols/${encodeURIComponent(protocolId)}/releases/${encodeURIComponent(release.version)}/source`,
      `${protocolId}-${release.version}-source.${release.artifact?.filename?.toLowerCase().endsWith('.go') ? 'go' : 'zip'}`
    )
  } catch (error) {
    notifyError(error)
  }
}
function newestRelease(item) {
  return item.releases?.[0] || {}
}
function statusText(value) {
  return (
    {
      LISTENING: '监听中',
      DISABLED: '已停用',
      DRAFT: '草稿',
      VALIDATED: '已校验',
      PUBLISHED: '已发布',
      DEPRECATED: '已弃用',
      REVOKED: '已撤销',
      PENDING: '待启动',
      ONLINE: '在线采集',
      ERROR: '采集异常'
    }[value] || statusLabel(value)
  )
}
function statusType(value) {
  return (
    { PUBLISHED: 'success', ONLINE: 'success', ERROR: 'danger', REVOKED: 'danger', VALIDATED: 'warning', PENDING: 'info' }[value] || 'info'
  )
}

onMounted(async () => {
  if (props.section === 'profiles') return
  const context = takeNavigation()
  await load()
  if (context.protocolId && context.version) {
    const item = protocols.value.find(row => row.definition.id === context.protocolId),
      release = item?.releases?.find(row => row.version === context.version)
    if (item && release) {
      selectedProtocol.value = item.definition
      selectedRelease.value = { ...release, protocolId: context.protocolId }
      previewContext.value = context
      if (context.preview && can('POST /api/v2/protocols/:id/releases/:version/preview')) previewOpen.value = true
      else releaseOpen.value = true
    }
  }
})
function removeProtocol(row) {
  return confirmDelete({
    label: row.definition.name || row.definition.id,
    path: `/api/v2/protocols/${encodeURIComponent(row.definition.id)}`,
    onDeleted: load,
    warning: '未被引用的版本将一并删除，删除后无法恢复。',
    blockedHint: '协议仍被设备模板或接入点引用，请先解除绑定。'
  })
}
function removeRelease(row, release) {
  return confirmDelete({
    label: `${row.definition.name || row.definition.id} · ${release.version}`,
    path: `/api/v2/protocols/${encodeURIComponent(row.definition.id)}/releases/${encodeURIComponent(release.version)}`,
    onDeleted: load,
    warning: '仅删除此版本及其独有制品，删除后无法恢复。',
    blockedHint: '此版本仍被设备模板、回滚记录或接入点引用，请先切换关联版本。'
  })
}
function protocolActions(row) {
  return [
    { key: 'versions', label: '管理版本', onClick: () => manageVersions(row) },
    { key: 'delete', label: '删除协议', type: 'danger', permission: 'DELETE /api/v2/protocols/:id', onClick: () => removeProtocol(row) }
  ]
}
</script>

<template>
  <AccessPointsPanel v-if="props.section === 'profiles'" />
  <template v-else>
    <FilterBar>
      <template #actions>
        <ui-button :loading="loading" @click="load"><RefreshCw />刷新</ui-button>
        <ui-button
          v-permission="'POST /api/v1/ai/protocol-assistant/generate'"
          title="通过报文或 Excel / CSV 点表生成协议"
          @click="openAssistant()"
          ><Wand2 />协议生成</ui-button
        >
        <ui-button v-permission="'POST /api/v2/protocols/:id/source-releases'" type="primary" @click="sourceOpen = true"
          ><Upload />上传源码</ui-button
        >
      </template>
    </FilterBar>
    <DataTableCard
      :title="`协议开发 · ${protocols.length} 个协议 · ${releaseCount} 个版本`"
      :error="loadError"
      @retry="load()"
      :page="protocolPage"
      :page-size="protocolPageSize"
      :page-sizes="[10, 20, 50, 100]"
      :total="protocols.length"
      @update:page="value => (protocolPage = value)"
      @update:page-size="
        value => {
          protocolPageSize = value
          protocolPage = 1
        }
      "
    >
      <ui-table :data="pagedProtocols" :loading="loading" empty-text="暂无协议，可上传 Go 源码或用报文、点表生成">
        <ui-table-column label="协议" min-width="230"
          ><template #default="{ row }"
            ><b>{{ row.definition.name }}</b
            ><small class="subline">{{ row.definition.id }} · {{ row.definition.vendor || '通用' }}</small></template
          ></ui-table-column
        >
        <ui-table-column label="最新版本" width="170" show-overflow-tooltip
          ><template #default="{ row }">{{ newestRelease(row).version || '—' }}</template></ui-table-column
        >
        <ui-table-column label="运行方式" min-width="200"
          ><template #default="{ row }"
            >{{ transportLabel(newestRelease(row).transport) }} ·
            {{ label(parsers, newestRelease(row).parserType, '自定义协议程序') }}</template
          ></ui-table-column
        >
        <ui-table-column label="状态" width="110"
          ><template #default="{ row }"
            ><StatusDot
              :tone="
                statusType(newestRelease(row).status) === 'success'
                  ? 'success'
                  : statusType(newestRelease(row).status) === 'danger'
                    ? 'danger'
                    : statusType(newestRelease(row).status) === 'warning'
                      ? 'warning'
                      : 'neutral'
              "
              :label="statusText(newestRelease(row).status)" /></template
        ></ui-table-column>
        <ui-table-column label="版本数量" width="100"
          ><template #default="{ row }">{{ row.releases?.length || 0 }}</template></ui-table-column
        >
        <ui-table-column label="操作" fixed="right" width="176" align="right"
          ><template #default="{ row }"><RowActions :actions="protocolActions(row)" /></template
        ></ui-table-column>
      </ui-table>
    </DataTableCard>
    <ui-dialog
      v-model="versionsOpen"
      class="protocol-versions-dialog"
      :title="`${managedProtocol?.definition.name || '协议'} · 版本管理`"
      width="min(880px, 96vw)"
      destroy-on-close
    >
      <template v-if="managedProtocol">
        <p class="versions-summary">
          {{ managedProtocol.definition.id }} · 共
          {{ managedProtocol.releases?.length || 0 }} 个版本。删除前请确认该版本未被设备模板或接入点引用。
        </p>
        <ui-table :data="managedProtocol.releases || []" stripe empty-text="暂无版本，可上传源码创建新版本">
          <ui-table-column label="版本" min-width="130"
            ><template #default="{ row }"
              ><strong>{{ row.version }}</strong></template
            ></ui-table-column
          >
          <ui-table-column label="状态" width="110"
            ><template #default="{ row }"
              ><ui-tag :type="statusType(row.status)" round>{{ statusText(row.status) }}</ui-tag></template
            ></ui-table-column
          >
          <ui-table-column label="运行方式" min-width="185"
            ><template #default="{ row }"
              >{{ transportLabel(row.transport) }} · {{ label(parsers, row.parserType, '自定义协议程序') }}</template
            ></ui-table-column
          >
          <ui-table-column label="创建时间" min-width="170"
            ><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></ui-table-column
          >
          <ui-table-column label="操作" width="120" align="right"
            ><template #default="{ row }"
              ><RowActions
                :actions="[
                  { key: 'detail', label: '详情', onClick: () => viewRelease(managedProtocol, row) },
                  {
                    key: 'delete',
                    label: '删除',
                    type: 'danger',
                    permission: 'DELETE /api/v2/protocols/:id/releases/:version',
                    onClick: () => removeRelease(managedProtocol, row)
                  }
                ]" /></template
          ></ui-table-column>
        </ui-table>
      </template>
    </ui-dialog>
    <ui-dialog v-model="releaseOpen" title="协议版本" width="min(620px, 94vw)" destroy-on-close>
      <template v-if="selectedProtocol && selectedRelease">
        <ui-descriptions :column="1" border>
          <ui-descriptions-item label="协议">{{ selectedProtocol.name }} · {{ selectedProtocol.id }}</ui-descriptions-item>
          <ui-descriptions-item label="版本">{{ selectedRelease.version }}</ui-descriptions-item>
          <ui-descriptions-item label="状态"
            ><ui-tag :type="statusType(selectedRelease.status)" round>{{
              statusText(selectedRelease.status)
            }}</ui-tag></ui-descriptions-item
          >
          <ui-descriptions-item label="运行方式"
            >{{ transportLabel(selectedRelease.transport) }} ·
            {{ label(parsers, selectedRelease.parserType, '自定义协议程序') }}</ui-descriptions-item
          >
        </ui-descriptions>
        <div class="release-buttons release-detail-actions">
          <ui-button
            v-if="canTestMapping"
            v-permission="'POST /api/v2/protocols/:id/releases/:version/preview'"
            plain
            type="primary"
            @click="openAssistant(selectedRelease, selectedProtocol.name)"
            >解析测试</ui-button
          >
          <ui-button
            v-else-if="canPreviewRelease"
            v-permission="'POST /api/v2/protocols/:id/releases/:version/preview'"
            plain
            type="primary"
            @click="
              () => {
                releaseOpen = false
                previewOpen = true
              }
            "
            >解析预览</ui-button
          >
          <ui-button
            v-if="canPublishRelease"
            v-permission="'POST /api/v2/protocols/:id/releases/:version/publish'"
            plain
            type="primary"
            :loading="switching"
            @click="publishRelease(selectedProtocol.id, selectedRelease.version)"
            >发布</ui-button
          >
          <ui-button
            v-if="canDownloadSource"
            v-permission="'GET /api/v2/protocols/:id/releases/:version/source'"
            plain
            @click="downloadSourceRelease(selectedProtocol.id, selectedRelease)"
            >源码</ui-button
          >
          <span v-if="!hasReleaseActions" class="muted-text">暂无可执行操作</span>
        </div>
      </template>
    </ui-dialog>
    <ui-dialog
      :model-value="assistantOpen"
      title="生成协议"
      width="min(980px, 94vw)"
      :close-on-click-modal="false"
      :close-on-press-escape="!assistantState.working"
      :show-close="!assistantState.working"
      destroy-on-close
      @update:model-value="value => (value ? (assistantOpen = true) : closeAssistant())"
      ><p v-if="!assistantRelease" class="muted-text bottom-gap">通过报文或 Excel / CSV 点表生成协议</p>
      <ProtocolAssistantView
        v-if="assistantOpen"
        :initial-release="assistantRelease"
        :initial-name="assistantName"
        @saved="load"
        @state="value => (assistantState = value)"
        @navigate="assistantNavigate"
    /></ui-dialog>
    <ui-dialog v-model="previewOpen" title="协议版本解析预览" width="min(860px, 94vw)" destroy-on-close
      ><ProtocolPreviewPanel
        v-if="previewOpen"
        :context="previewContext"
        :initial-protocol-id="selectedRelease.protocolId"
        :initial-version="selectedRelease.version"
    /></ui-dialog>
    <ui-dialog
      v-model="sourceOpen"
      class="source-upload-dialog"
      title="上传协议源码"
      width="min(760px, 94vw)"
      :close-on-click-modal="false"
      :close-on-press-escape="!compiling"
      :show-close="!compiling"
      destroy-on-close
    >
      <ProtocolSourceUpload
        v-if="sourceOpen"
        @busy="compiling = $event"
        @saved="
          value => {
            result = value
            load()
          }
        "
      />
    </ui-dialog>
    <section v-if="result" class="recent-result" aria-label="最近操作结果">
      <strong>最近操作结果</strong>
      <dl>
        <template v-for="[name, value] in resultSummary" :key="name"
          ><dt>{{ name }}</dt>
          <dd>{{ value }}</dd></template
        >
      </dl>
      <details class="technical-details">
        <summary>原始数据</summary>
        <pre>{{ pretty(result) }}</pre>
      </details>
    </section>
  </template>
</template>
<style scoped>
.versions-summary {
  margin: 0 0 var(--space-3);
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}
.recent-result {
  margin-top: var(--space-4);
  padding: var(--space-3) var(--space-4);
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  font-size: var(--font-size-sm);
}
.recent-result dl {
  display: grid;
  grid-template-columns: max-content minmax(0, 1fr);
  gap: var(--space-1) var(--space-3);
  margin: var(--space-2) 0;
}
.recent-result dt {
  color: var(--text-muted);
}
.recent-result dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.release-buttons {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2);
  margin-top: var(--space-4);
}
</style>
