<script setup>
// 知识文档详情：文件信息、索引与切片规则以及逐条切片；详情由页面读取并在索引进行中自动刷新。
import { FileText } from '@lucide/vue'
import { formatTime } from '../../api'
import { formatBytes } from '../../format'
import { agentKey, agentName, categoryLabel, retryable } from '../../knowledge'
import KnowledgeIndexStatus from '../KnowledgeIndexStatus.vue'

const visible = defineModel({ type: Boolean, default: false })
const props = defineProps({
  // 列表中的文档与详情接口返回的索引、切片。
  selectedDocument: { type: Object, default: null },
  selectedDetail: { type: Object, default: null },
  detailLoading: { type: Boolean, default: false },
  detailError: { type: String, default: '' },
  agents: { type: Array, default: () => [] },
  // 正在重试索引的文档编号。
  retrying: { type: Array, default: () => [] }
})
// retry(文档)：重试索引。
const emit = defineEmits(['retry'])
function agentLabel(id) {
  if (!id) return '未关联智能体'
  const agent = props.agents.find(item => agentKey(item) === id)
  return agent ? agentName(agent) : id
}
</script>

<template>
  <ui-dialog v-model="visible" title="知识文档详情与切片" width="min(900px, 96vw)">
    <ui-alert v-if="detailError" :title="detailError" type="error" :closable="false" show-icon />
    <div v-loading="detailLoading" class="knowledge-detail">
      <div v-if="selectedDocument" class="knowledge-detail-file">
        <FileText class="document-icon" />
        <div>
          <strong>{{ selectedDocument.filename }}</strong
          ><small>{{ selectedDocument.id }}</small>
        </div>
        <KnowledgeIndexStatus :document="selectedDocument" />
      </div>
      <dl v-if="selectedDocument" class="knowledge-detail-meta">
        <div>
          <dt>关联智能体</dt>
          <dd>{{ agentLabel(selectedDocument.workflowId) }}</dd>
        </div>
        <div>
          <dt>知识分类</dt>
          <dd>{{ categoryLabel(selectedDocument.category) }}</dd>
        </div>
        <div>
          <dt>内容统计</dt>
          <dd>{{ selectedDocument.metadata?.chunks || 0 }} 个分片 · {{ formatBytes(selectedDocument.metadata?.size) }}</dd>
        </div>
        <div>
          <dt>上传时间</dt>
          <dd>{{ formatTime(selectedDocument.createdAt) }}</dd>
        </div>
        <div v-if="selectedDocument.tags?.length">
          <dt>知识标签</dt>
          <dd>{{ selectedDocument.tags.join('、') }}</dd>
        </div>
      </dl>
      <section v-if="selectedDetail?.index" class="knowledge-index-rules">
        <div class="knowledge-detail-section-heading">
          <h3>索引与切片规则</h3>
          <span>{{ selectedDetail.index.mode }} · {{ selectedDetail.index.vectorizer }}</span>
        </div>
        <div class="knowledge-rule-grid">
          <div>
            <small>切片策略</small
            ><strong>{{
              selectedDetail.index.chunking?.strategy === 'fixed-window-overlap'
                ? '固定窗口 + 重叠'
                : selectedDetail.index.chunking?.strategy
            }}</strong>
          </div>
          <div>
            <small>窗口 / 重叠</small
            ><strong>{{ selectedDetail.index.chunking?.size }} / {{ selectedDetail.index.chunking?.overlap }} 字符</strong>
          </div>
          <div>
            <small>提取文本</small><strong>{{ selectedDetail.index.extractedChars || 0 }} 字符</strong>
          </div>
          <div>
            <small>实际分片</small><strong>{{ selectedDetail.index.chunkCount }}</strong>
          </div>
        </div>
        <p v-if="selectedDetail.index.embeddingModel">向量模型：{{ selectedDetail.index.embeddingModel }}</p>
      </section>
      <section v-if="selectedDetail" class="knowledge-chunks">
        <div class="knowledge-detail-section-heading">
          <h3>切片内容</h3>
          <span>{{ selectedDetail.chunks?.length || 0 }} 个分片，点击逐条查看</span>
        </div>
        <div v-if="selectedDetail.chunks?.length" class="knowledge-chunk-list">
          <details v-for="(row, index) in selectedDetail.chunks" :key="row.chunkId || index" :open="index === 0" class="knowledge-chunk">
            <summary>
              <span class="knowledge-chunk-number">{{ index + 1 }}</span
              ><span>字符范围 [{{ row.startChar }}, {{ row.endChar }})</span
              ><ui-tag :type="row.vectorized ? 'success' : 'info'" size="small">{{ row.vectorized ? '向量化完成' : '非向量索引' }}</ui-tag>
            </summary>
            <div class="knowledge-chunk-body">
              <p>{{ row.content }}</p>
              <small>重叠 {{ row.overlapChars || 0 }} 字符 · {{ row.characterCount }} 字符 · {{ row.chunkId }}</small>
            </div>
          </details>
        </div>
        <ui-empty v-else description="索引中没有可查看的切片" :image-size="56" />
      </section>
    </div>
    <template #footer
      ><ui-button
        v-if="retryable(selectedDocument)"
        v-permission="'POST /api/v1/knowledge/documents/:id/retry'"
        type="primary"
        :loading="retrying.includes(selectedDocument.id)"
        :disabled="retrying.includes(selectedDocument.id)"
        @click="emit('retry', selectedDocument)"
        >重试索引</ui-button
      ><ui-button @click="visible = false">关闭</ui-button></template
    >
  </ui-dialog>
</template>

<style scoped>
.document-icon {
  width: 36px;
  height: 36px;
  flex: none;
  padding: 9px;
  color: var(--primary-text);
  background: var(--surface-muted);
  border-radius: 10px;
}
.knowledge-detail {
  min-height: 140px;
}
.knowledge-detail-file {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 3px 0 18px;
}
.knowledge-detail-file > div {
  min-width: 0;
  flex: 1;
  display: grid;
  gap: 4px;
}
.knowledge-detail-file strong {
  overflow: hidden;
  font-size: 16px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.knowledge-detail-file small {
  color: var(--text-muted);
  font-size: 12px;
  overflow-wrap: anywhere;
}
.knowledge-detail-meta {
  margin: 0;
  padding: 16px 18px;
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
  background: var(--surface-muted);
  border-radius: 10px;
}
.knowledge-detail-meta div {
  min-width: 0;
}
.knowledge-detail-meta dt {
  color: var(--text-muted);
  font-size: 12px;
}
.knowledge-detail-meta dd {
  margin: 5px 0 0;
  font-size: 13px;
  overflow-wrap: anywhere;
}
.knowledge-index-rules,
.knowledge-chunks {
  margin-top: 24px;
}
.knowledge-detail-section-heading {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}
.knowledge-detail-section-heading h3 {
  margin: 0;
  font-size: 15px;
}
.knowledge-detail-section-heading span {
  color: var(--text-muted);
  font-size: 12px;
}
.knowledge-rule-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 8px;
}
.knowledge-rule-grid > div {
  min-width: 0;
  padding: 12px;
  display: grid;
  gap: 5px;
  background: var(--surface-muted);
  border-radius: 8px;
}
.knowledge-rule-grid small {
  color: var(--text-muted);
  font-size: 12px;
}
.knowledge-rule-grid strong {
  font-size: 13px;
  overflow-wrap: anywhere;
}
.knowledge-index-rules p {
  margin: 10px 0 0;
  color: var(--text-muted);
  font-size: 12px;
}
.knowledge-chunk-list {
  display: grid;
  gap: 8px;
}
.knowledge-chunk {
  border: 1px solid var(--border);
  border-radius: 9px;
}
.knowledge-chunk summary {
  min-height: 52px;
  padding: 10px 14px;
  display: flex;
  align-items: center;
  gap: 12px;
  cursor: pointer;
  list-style: none;
  font-size: 13px;
}
.knowledge-chunk summary::-webkit-details-marker {
  display: none;
}
.knowledge-chunk summary .ui-tag {
  margin-left: auto;
}
.knowledge-chunk-number {
  width: 26px;
  height: 26px;
  flex: none;
  display: grid;
  place-items: center;
  color: var(--primary-text);
  background: var(--surface-muted);
  border-radius: 7px;
  font-size: 12px;
  font-weight: 700;
}
.knowledge-chunk-body {
  padding: 0 14px 14px 52px;
}
.knowledge-chunk-body p {
  max-height: 220px;
  margin: 0;
  padding: 12px;
  overflow: auto;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  background: var(--surface-muted);
  border-radius: 7px;
  font-size: 13px;
  line-height: 1.7;
}
.knowledge-chunk-body small {
  display: block;
  margin-top: 7px;
  color: var(--text-muted);
  font-size: 12px;
  overflow-wrap: anywhere;
}
@media (max-width: 640px) {
  .knowledge-detail-meta {
    grid-template-columns: 1fr;
  }
  .knowledge-rule-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .knowledge-detail-section-heading {
    align-items: flex-start;
    flex-direction: column;
    gap: 3px;
  }
  .knowledge-chunk summary {
    flex-wrap: wrap;
  }
  .knowledge-chunk-body {
    padding-left: 14px;
  }
}
</style>
