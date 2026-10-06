<script setup>
import { can } from '../permissions'
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { FileText, Upload } from '@lucide/vue'
import { UiMessage } from '../ui/feedback.js'

import { api, apiAll, formatTime, notifyError } from '../api'
import DataTableCard from '../components/layout/DataTableCard.vue'
import RowActions from '../components/layout/RowActions.vue'
import KnowledgeIndexStatus from '../components/KnowledgeIndexStatus.vue'
import KnowledgeBindingPanel from '../components/knowledge/KnowledgeBindingPanel.vue'
import KnowledgeUploadDialog from '../components/knowledge/KnowledgeUploadDialog.vue'
import KnowledgeDocumentDialog from '../components/knowledge/KnowledgeDocumentDialog.vue'
import { useKnowledgeBinding } from '../composables/useKnowledgeBinding'
import { confirmDelete } from '../deleteAction'
import { usePagedList } from '../composables/usePagedList'
import { formatBytes } from '../format'
import { agentKey, agentName, categoryLabel, isIndexing, retryable } from '../knowledge'

const emit = defineEmits(['navigate'])
const documents = ref([])
const agents = ref([])
const loading = ref(false)
const documentsError = ref('')
const documentsLoaded = ref(false)
const retrying = ref([])
const uploadDialog = ref(false)
const detailDialog = ref(false)
const selectedDocument = ref(null)
const selectedDetail = ref(null)
const detailLoading = ref(false)
const detailError = ref('')
const runtime = ref({ indexMode: '', persistentIndex: false, embeddingModel: '', indexState: { state: 'ready' } })
let rebuildTimer = 0
let disposed = false
let detailVersion = 0
const agentError = ref('')
let loadVersion = 0
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const activeTab = ref('documents')
// 策略状态留在页面，切换到文档页签再回来时保留未保存的修改与检索测试结果。
const binding = useKnowledgeBinding(agents)

const canUpload = computed(() => can('POST /api/v1/knowledge/documents'))
// 概况统计由服务端按整个租户汇总，不随翻页变化。
const summary = ref({ documents: 0, indexed: 0, failed: 0, chunks: 0, bytes: 0 })

function agentLabel(id) {
  if (!id) return '未关联智能体'
  const agent = agents.value.find(item => agentKey(item) === id)
  return agent ? agentName(agent) : id
}

async function load(silent = false) {
  silent = silent === true
  const version = ++loadVersion
  if (!silent) loading.value = true
  if (!silent) agentError.value = ''
  try {
    const [documentResult, agentResult] = await Promise.allSettled([
      api(`/api/v1/knowledge/documents?page=${page.value}&pageSize=${pageSize.value}`),
      silent ? Promise.resolve({ items: agents.value }) : apiAll('/api/v1/ai/workflows?purpose=knowledge')
    ])
    if (version !== loadVersion) return
    if (documentResult.status === 'fulfilled') {
      const data = documentResult.value
      documentsError.value = ''
      documents.value = Array.isArray(data.items) ? data.items : []
      total.value = Number(data.total ?? documents.value.length)
      runtime.value = {
        indexMode: data.indexMode || '',
        persistentIndex: Boolean(data.persistentIndex),
        embeddingModel: data.embeddingModel || '',
        indexState: data.indexState || { state: 'ready' }
      }
      summary.value = { documents: 0, indexed: 0, failed: 0, chunks: 0, bytes: 0, ...data.summary }
      documentsLoaded.value = true
      if (detailDialog.value && selectedDocument.value) {
        const previous = selectedDocument.value
        const current = documents.value.find(item => item.id === previous.id)
        if (current) selectedDocument.value = current
        if (silent && isIndexing(previous)) await showDocument(current || previous, true)
      }
    } else {
      throw documentResult.reason
    }
    if (agentResult.status === 'fulfilled')
      agents.value = Array.isArray(agentResult.value.items) ? agentResult.value.items.filter(item => item.enabled !== false) : []
    else agentError.value = agentResult.reason?.message || '智能体列表读取失败'
    binding.syncAgents()
  } catch (error) {
    if (version === loadVersion) {
      documentsError.value = error.message || '知识文档列表读取失败'
    }
  } finally {
    if (version === loadVersion) {
      loading.value = false
      scheduleRebuildRefresh()
    }
  }
}

const { changePage, changePageSize } = usePagedList(() => load(), { page, pageSize })
function openUpload() {
  if (!canUpload.value) return
  uploadDialog.value = true
}
async function uploaded() {
  activeTab.value = 'documents'
  await load()
}
async function showDocument(document, silent = false) {
  const version = ++detailVersion
  if (!silent) {
    selectedDocument.value = document
    selectedDetail.value = null
    detailError.value = ''
    detailDialog.value = true
    detailLoading.value = true
  }
  try {
    const detail = await api(`/api/v1/knowledge/documents/${encodeURIComponent(document.id)}`)
    if (disposed || version !== detailVersion || selectedDocument.value?.id !== document.id) return
    selectedDocument.value = detail.document || document
    selectedDetail.value = detail
  } catch (error) {
    if (!disposed && version === detailVersion) detailError.value = error.message || '知识切片详情读取失败'
  } finally {
    if (!disposed && version === detailVersion) {
      detailLoading.value = false
      scheduleRebuildRefresh()
    }
  }
}

async function retryDocument(document) {
  if (!can('POST /api/v1/knowledge/documents/:id/retry') || !retryable(document) || retrying.value.includes(document.id)) return
  retrying.value = [...retrying.value, document.id]
  try {
    const updated = await api(`/api/v1/knowledge/documents/${encodeURIComponent(document.id)}/retry`, { method: 'POST' })
    documents.value = documents.value.map(item => (item.id === document.id ? updated : item))
    if (selectedDocument.value?.id === document.id) selectedDocument.value = updated
    UiMessage.success('已提交索引重试，完成后文档可供检索')
    await load(true)
  } catch (error) {
    notifyError(error)
  } finally {
    retrying.value = retrying.value.filter(id => id !== document.id)
  }
}

function documentActions(row) {
  return [
    { key: 'detail', label: '查看详情', onClick: () => showDocument(row) },
    {
      key: 'retry',
      label: '重试索引',
      hidden: !retryable(row),
      loading: retrying.value.includes(row.id),
      disabled: retrying.value.includes(row.id),
      permission: 'POST /api/v1/knowledge/documents/:id/retry',
      onClick: () => retryDocument(row)
    },
    {
      key: 'delete',
      label: row.status === 'DELETING' ? '删除中' : '删除',
      disabled: row.status === 'DELETING',
      type: 'danger',
      permission: 'DELETE /api/v1/knowledge/documents/:id',
      onClick: () => removeDocument(row)
    }
  ]
}

// Server-reported batch counts are refreshed while a document or rebuild runs.
function scheduleRebuildRefresh() {
  clearTimeout(rebuildTimer)
  if (
    !disposed &&
    (runtime.value.indexState?.state === 'rebuilding' ||
      documents.value.some(isIndexing) ||
      (detailDialog.value && isIndexing(selectedDocument.value)))
  )
    rebuildTimer = setTimeout(() => load(true), 3000)
}
onMounted(load)
onBeforeUnmount(() => {
  disposed = true
  ++loadVersion
  ++detailVersion
  clearTimeout(rebuildTimer)
})
function removeDocument(row) {
  return confirmDelete({
    label: row.filename,
    path: `/api/v1/knowledge/documents/${encodeURIComponent(row.id)}`,
    onDeleted: async () => {
      if (selectedDocument.value?.id === row.id) detailDialog.value = false
      await load()
    },
    warning: '原文件和检索索引将一并清理，删除后无法恢复。'
  })
}
</script>

<template>
  <div class="knowledge-page">
    <header class="knowledge-intro">
      <div class="knowledge-intro-actions">
        <ui-button v-permission="'menu:ai'" @click="emit('navigate', 'ai')">打开智能助手</ui-button>
        <ui-button v-permission="'POST /api/v1/knowledge/documents'" type="primary" :disabled="!canUpload" @click="openUpload"
          ><Upload :size="16" />上传知识文档</ui-button
        >
      </div>
    </header>

    <ui-alert v-if="agentError" :title="agentError" type="warning" :closable="false" show-icon />
    <ui-alert
      v-if="documentsLoaded && !runtime.persistentIndex"
      title="当前使用内存索引，服务重启后需要重新建立文档检索索引。"
      type="warning"
      :closable="false"
      show-icon
    />
    <ui-alert
      v-if="runtime.indexState?.state === 'rebuilding'"
      :title="`知识库索引正在按新的向量模型重建（${runtime.indexState.done || 0}/${runtime.indexState.total || 0}），完成前检索结果可能不完整。`"
      type="info"
      :closable="false"
      show-icon
    />
    <ui-alert
      v-else-if="runtime.indexState?.state === 'failed'"
      :title="`知识库索引重建未完成：${runtime.indexState.error || 'Embedding API 暂不可用'}。请检查配置并重试失败文档。`"
      type="warning"
      :closable="false"
      show-icon
    />

    <section class="knowledge-stats" aria-label="知识库概况">
      <div>
        <span>知识文档</span><strong>{{ documentsLoaded ? total : '—' }}</strong
        ><small>当前租户全部文档</small>
      </div>
      <div>
        <span>已索引</span><strong>{{ documentsLoaded ? summary.indexed : '—' }}</strong
        ><small :class="{ 'knowledge-stat-warn': summary.failed }">{{
          documentsLoaded ? (summary.failed ? `${summary.failed} 份索引失败` : '可供检索') : '等待读取'
        }}</small>
      </div>
      <div>
        <span>内容分片</span><strong>{{ documentsLoaded ? summary.chunks : '—' }}</strong
        ><small>{{ documentsLoaded ? `原件共 ${formatBytes(summary.bytes)}` : '等待读取' }}</small>
      </div>
      <div class="knowledge-index-state">
        <span>索引存储</span
        ><strong>{{
          documentsLoaded
            ? runtime.persistentIndex
              ? runtime.indexMode === 'postgres-pgvector'
                ? 'PostgreSQL / pgvector'
                : '持久化'
              : '内存'
            : '读取中'
        }}</strong
        ><small>{{ runtime.embeddingModel ? `Embedding API · ${runtime.embeddingModel}` : runtime.indexMode || '索引模式未返回' }}</small>
      </div>
    </section>

    <ui-tabs v-model="activeTab" class="knowledge-tabs">
      <ui-tab-pane name="documents" label="文档">
        <DataTableCard
          aria-label="已上传文档"
          :error="documentsError"
          :page="page"
          :page-size="pageSize"
          :total="total"
          @retry="load"
          @update:page="changePage"
          @update:page-size="changePageSize"
        >
          <template #header>
            <div class="documents-heading">
              <h2>已上传文档</h2>
              <p>查看文档的归属、索引状态和内容切片。</p>
            </div>
            <ui-button :loading="loading" @click="load">刷新列表</ui-button>
          </template>

          <ui-table v-loading="loading" :data="documents" class="knowledge-table">
            <ui-table-column label="文档" min-width="270"
              ><template #default="{ row }"
                ><div class="document-name">
                  <FileText class="document-icon" />
                  <div>
                    <strong>{{ row.filename }}</strong
                    ><small>{{ categoryLabel(row.category) }} · {{ formatBytes(row.metadata?.size) }}</small>
                  </div>
                </div></template
              ></ui-table-column
            >
            <ui-table-column label="关联智能体" min-width="175"
              ><template #default="{ row }">{{ agentLabel(row.workflowId) }}</template></ui-table-column
            >
            <ui-table-column label="索引状态与进度" min-width="200"
              ><template #default="{ row }"><KnowledgeIndexStatus :document="row" /></template
            ></ui-table-column>
            <ui-table-column label="内容分片" width="100" align="right"
              ><template #default="{ row }">{{ row.metadata?.chunks || 0 }}</template></ui-table-column
            >
            <ui-table-column label="上传时间" min-width="165"
              ><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></ui-table-column
            >
            <ui-table-column label="操作" width="210" align="right"
              ><template #default="{ row }"><RowActions :actions="documentActions(row)" /></template
            ></ui-table-column>
            <template #empty><ui-empty description="还没有知识文档" /></template>
          </ui-table>

          <div v-loading="loading" class="knowledge-mobile-list">
            <article v-for="row in documents" :key="row.id" class="knowledge-mobile-document">
              <div class="knowledge-mobile-document-head">
                <FileText class="document-icon" />
                <div>
                  <strong>{{ row.filename }}</strong
                  ><small>{{ categoryLabel(row.category) }} · {{ formatBytes(row.metadata?.size) }}</small>
                </div>
              </div>
              <div class="knowledge-mobile-document-meta">
                <span>{{ agentLabel(row.workflowId) }}</span
                ><KnowledgeIndexStatus :document="row" />
              </div>
              <div class="knowledge-mobile-document-foot">
                <small>{{ row.metadata?.chunks || 0 }} 个分片 · {{ formatTime(row.createdAt) }}</small
                ><RowActions :actions="documentActions(row)" />
              </div>
            </article>
            <ui-empty v-if="!loading && !documentsError && !documents.length" description="还没有知识文档" />
          </div>
        </DataTableCard>
      </ui-tab-pane>

      <ui-tab-pane name="policy" label="检索策略">
        <KnowledgeBindingPanel :binding="binding" :agents="agents" :documents="documents" />
      </ui-tab-pane>
    </ui-tabs>

    <KnowledgeUploadDialog v-model="uploadDialog" :agents="agents" @uploaded="uploaded" @changed="load" />

    <KnowledgeDocumentDialog
      v-model="detailDialog"
      :selected-document="selectedDocument"
      :selected-detail="selectedDetail"
      :detail-loading="detailLoading"
      :detail-error="detailError"
      :agents="agents"
      :retrying="retrying"
      @retry="retryDocument"
    />
  </div>
</template>

<style scoped>
.knowledge-page {
  display: grid;
  gap: 18px;
  min-width: 0;
  padding-bottom: 24px;
}
.knowledge-intro {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 20px;
  padding: 4px 0 2px;
}
.knowledge-intro-actions {
  display: flex;
  gap: 8px;
  flex: none;
}
.knowledge-intro-actions .ui-button {
  margin: 0;
}
.knowledge-intro-actions svg {
  width: 16px;
  height: 16px;
}
.knowledge-stats {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 1px;
  overflow: hidden;
  border-radius: var(--radius-lg);
  background: var(--surface-muted);
}
.knowledge-stats > div {
  min-width: 0;
  min-height: 104px;
  padding: 16px 18px;
  display: grid;
  align-content: space-between;
  background: var(--surface);
}
.knowledge-stats span,
.knowledge-stats small {
  color: var(--text-muted);
  font-size: 12px;
}
.knowledge-stats strong {
  color: var(--text);
  font-size: 26px;
  line-height: 1.15;
  font-weight: 650;
  font-variant-numeric: tabular-nums;
}
.knowledge-stats .knowledge-index-state strong {
  color: var(--primary-text);
  font-size: 18px;
}
.knowledge-tabs {
  min-width: 0;
}
.knowledge-tabs :deep(.n-tabs-nav) {
  margin-bottom: 14px;
}
.knowledge-tabs :deep(.n-tabs-tab) {
  height: 42px;
  padding: 0 22px;
  font-size: 14px;
}
.knowledge-tabs :deep(.n-tabs-bar) {
  height: 3px;
  border-radius: 3px;
}
.documents-heading {
  min-width: 0;
}
.documents-heading h2 {
  margin: 0;
  font-size: 18px;
  line-height: 1.3;
}
.documents-heading p {
  margin: 5px 0 0;
  color: var(--text-muted);
  font-size: 13px;
}
.knowledge-table {
  width: 100%;
}
.document-name {
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 12px;
}
.document-icon {
  width: 36px;
  height: 36px;
  flex: none;
  padding: 9px;
  color: var(--primary-text);
  background: var(--surface-muted);
  border-radius: 10px;
}
.document-name > div {
  min-width: 0;
  display: grid;
  gap: 3px;
}
.document-name strong {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 14px;
}
.document-name small {
  color: var(--text-muted);
  font-size: 12px;
}
.knowledge-mobile-list {
  display: none;
}
.knowledge-stat-warn {
  color: var(--warning-text) !important;
}
:deep(.n-card-content) {
  overflow-x: hidden;
} /* 设置  样式。 */
@media (max-width: 900px) {
  .knowledge-stats {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 640px) {
  .knowledge-page {
    gap: 14px;
  }
  .knowledge-intro {
    align-items: flex-start;
    flex-direction: column;
    gap: 14px;
  }
  .knowledge-intro-actions {
    width: 100%;
  }
  .knowledge-intro-actions .ui-button {
    flex: 1;
    padding-inline: 8px;
  }
  .knowledge-stats > div {
    min-height: 88px;
    padding: 12px;
  }
  .knowledge-stats strong {
    font-size: 22px;
  }
  .knowledge-stats .knowledge-index-state strong {
    font-size: 16px;
  }
  .documents-heading p {
    display: none;
  }
  .knowledge-table {
    display: none;
  }
  .knowledge-mobile-list {
    min-height: 90px;
    padding: 0 16px 12px;
    display: grid;
    gap: 9px;
  }
  .knowledge-mobile-document {
    padding: 14px;
    display: grid;
    gap: 12px;
    background: var(--surface-muted);
    border-radius: 10px;
  }
  .knowledge-mobile-document-head {
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .knowledge-mobile-document-head > div {
    min-width: 0;
    display: grid;
    gap: 3px;
  }
  .knowledge-mobile-document-head strong {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 14px;
  }
  .knowledge-mobile-document-head small {
    color: var(--text-muted);
    font-size: 12px;
  }
  .knowledge-mobile-document-meta,
  .knowledge-mobile-document-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    font-size: 12px;
  }
  .knowledge-mobile-document-meta span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .knowledge-mobile-document-foot small {
    color: var(--text-muted);
    font-size: 12px;
  }
}
</style>
