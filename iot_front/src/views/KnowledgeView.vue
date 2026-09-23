<script setup>
import {can} from '../permissions'
import { statusLabel } from '../presentation'
import { computed, onMounted, ref } from 'vue'
import { FileText, Upload } from '@lucide/vue'
import { ElMessage } from 'element-plus'

import { api, formatTime, notifyError } from '../api'

const emit = defineEmits(['navigate'])
const uploadRef = ref(null)
const documents = ref([])
const agents = ref([])
const loading = ref(false)
const documentsLoaded = ref(false)
const uploading = ref(false)
const uploadDialog = ref(false)
const detailDialog = ref(false)
const selectedDocument = ref(null)
const selectedDetail = ref(null)
const detailLoading = ref(false)
const detailError = ref('')
const selectedFile = ref(null)
const workflowId = ref('')
const category = ref('manual')
const tags = ref([])
const runtime = ref({ indexMode:'', persistentIndex:false })
const agentError = ref('')
const bindingLoading = ref(false)
const bindingSaving = ref(false)
const bindingError = ref('')
let bindingRequestId = 0
const loadedBindingWorkflowId = ref('')
const bindingWorkflowId = ref('')
const knowledgeBinding = ref({ retrievalMode:'auto', topK:5, minScore:0.25, noMatchPolicy:'allow-model' })
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const activeTab = ref('documents')

const canUpload = computed(() => can('POST /api/v1/knowledge/documents'))
const canManageBinding = computed(() => can('PUT /api/v1/ai/workflows/:id/knowledge-binding'))
const indexedCount = computed(() => documents.value.filter(item => item.status === 'INDEXED').length)
const totalChunks = computed(() => documents.value.reduce((sum, item) => sum + Number(item.metadata?.chunks || 0), 0))
const totalSize = computed(() => documents.value.reduce((sum, item) => sum + Number(item.metadata?.size || 0), 0))
const selectedBindingAgent = computed(() => agents.value.find(item => agentKey(item) === bindingWorkflowId.value))

function agentKey(item) { return item?.id || item?.workflowId || '' }
function agentName(item) { return item?.name || item?.label || agentKey(item) || '未命名智能体' }
function agentLabel(id) {
  if (!id) return '未关联智能体'
  const agent = agents.value.find(item => agentKey(item) === id)
  return agent ? agentName(agent) : id
}
const categoryNames = { manual:'设备手册', 'alarm-sop':'告警处置操作规程', maintenance:'运维维修', regulation:'消防规范', faq:'常见问题' }
function categoryLabel(value) { return categoryNames[value] || value || '未分类' }
function formatBytes(value) {
  const size = Number(value || 0)
  if (size < 1024) return `${size} 字节`
  if (size < 1024 ** 2) return `${(size / 1024).toFixed(1)} 千字节`
  return `${(size / 1024 ** 2).toFixed(1)} 兆字节`
}

async function load() {
  loading.value = true
  agentError.value = ''
  try {
    const [documentResult, agentResult] = await Promise.allSettled([
      api(`/api/v1/knowledge/documents?page=${page.value}&pageSize=${pageSize.value}`),
      api('/api/v1/ai/workflows?page=1&pageSize=100')
    ])
    if (documentResult.status === 'fulfilled') {
      const data = documentResult.value
      documents.value = Array.isArray(data.items) ? data.items : []
      total.value = Number(data.total ?? data.count ?? documents.value.length)
      runtime.value = { indexMode:data.indexMode || '', persistentIndex:Boolean(data.persistentIndex) }
      documentsLoaded.value = true
    } else {
      throw documentResult.reason
    }
    if (agentResult.status === 'fulfilled') agents.value = Array.isArray(agentResult.value.items) ? agentResult.value.items.filter(item => item.enabled !== false) : []
    else agentError.value = agentResult.reason?.message || '智能体列表读取失败'
    if (!workflowId.value && agents.value.length) workflowId.value = agentKey(agents.value[0])
    if (agents.value.length && !agents.value.some(item => agentKey(item) === bindingWorkflowId.value)) bindingWorkflowId.value = agentKey(agents.value[0])
    if (bindingWorkflowId.value && loadedBindingWorkflowId.value !== bindingWorkflowId.value) void loadBinding()
  } catch (error) {
    if (error?.message?.includes('workflows')) agentError.value = error.message
    else notifyError(error)
  } finally {
    loading.value = false
  }
}

async function loadBinding() {
  if (!bindingWorkflowId.value) return
  const workflow = bindingWorkflowId.value
  const requestId = ++bindingRequestId
  bindingLoading.value = true
  bindingError.value = ''
  try {
    const value = await api(`/api/v1/ai/workflows/${encodeURIComponent(workflow)}/knowledge-binding`)
    if (requestId !== bindingRequestId || bindingWorkflowId.value !== workflow) return
    knowledgeBinding.value = {
      retrievalMode:value.retrievalMode || 'auto',
      topK:Number(value.topK) || 5,
      minScore:Number(value.minScore ?? 0.25),
      noMatchPolicy:value.noMatchPolicy || 'allow-model'
    }
    loadedBindingWorkflowId.value = workflow
  } catch (error) {
    if (requestId === bindingRequestId) bindingError.value = error.message || '知识库策略读取失败'
  } finally {
    if (requestId === bindingRequestId) bindingLoading.value = false
  }
}

async function saveBinding() {
  if (!bindingWorkflowId.value || !canManageBinding.value) return
  bindingSaving.value = true
  bindingError.value = ''
  try {
    const value = await api(`/api/v1/ai/workflows/${encodeURIComponent(bindingWorkflowId.value)}/knowledge-binding`, { method:'PUT', body:JSON.stringify(knowledgeBinding.value) })
    knowledgeBinding.value = { ...knowledgeBinding.value, ...value }
    ElMessage.success(`已保存 ${agentName(selectedBindingAgent.value)} 的知识库策略`)
  } catch (error) {
    bindingError.value = error.message || '知识库策略保存失败'
  } finally {
    bindingSaving.value = false
  }
}

function changePage(value) { page.value = value; load() }
function changePageSize(value) { pageSize.value = value; page.value = 1; load() }
function openUpload() {
  if (!canUpload.value) return
  uploadDialog.value = true
}
function chooseFile(file) {
  if (Number(file.size || file.raw?.size || 0) > 32 * 1024 * 1024) {
    selectedFile.value = null
    uploadRef.value?.clearFiles()
    ElMessage.error('知识库文件不能超过 32 兆字节')
    return
  }
  selectedFile.value = file.raw || null
}
function removeFile() { selectedFile.value = null }
function rejectExtra() { ElMessage.warning('每次只能上传一个知识库文件') }
async function showDocument(document) {
  selectedDocument.value = document
  selectedDetail.value = null
  detailError.value = ''
  detailDialog.value = true
  detailLoading.value = true
  try {
    const detail = await api(`/api/v1/knowledge/documents/${encodeURIComponent(document.id)}`)
    selectedDocument.value = detail.document || document
    selectedDetail.value = detail
  } catch (error) {
    detailError.value = error.message || '知识切片详情读取失败'
  } finally {
    detailLoading.value = false
  }
}

async function upload() {
  if (!workflowId.value) return ElMessage.warning('请选择要关联的智能体')
  if (!selectedFile.value) return ElMessage.warning('请先选择知识库文件')
  uploading.value = true
  try {
    const form = new FormData()
    form.append('file', selectedFile.value)
    form.append('workflowId', workflowId.value)
    if (category.value.trim()) form.append('category', category.value.trim())
    if (tags.value.length) form.append('tags', tags.value.join(','))
    const created = await api('/api/v1/knowledge/documents', { method:'POST', body:form })
    ElMessage.success(`知识库已索引并绑定到 ${agentLabel(created.workflowId)}`)
    selectedFile.value = null
    category.value = 'manual'
    tags.value = []
    uploadRef.value?.clearFiles()
    uploadDialog.value = false
    activeTab.value = 'documents'
    await load()
  } catch (error) {
    notifyError(error)
  } finally {
    uploading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="knowledge-page">
    <header class="knowledge-intro">
      <div class="knowledge-intro-copy">
        <span class="knowledge-kicker">智能体知识</span>
        <p>上传设备手册与处置规范，按智能体管理文档和检索方式。</p>
      </div>
      <div class="knowledge-intro-actions">
        <el-button v-permission="'menu:ai'" @click="emit('navigate', 'ai')">打开智能助手</el-button>
        <el-button v-permission="'POST /api/v1/knowledge/documents'" type="primary" :disabled="!canUpload" @click="openUpload"><Upload :size="16" />上传知识文档</el-button>
      </div>
    </header>

    <el-alert v-if="agentError" :title="agentError" type="warning" :closable="false" show-icon />
    <el-alert v-if="documentsLoaded && !runtime.persistentIndex" title="当前使用内存索引，服务重启后需要重新建立文档检索索引。" type="warning" :closable="false" show-icon />

    <section class="knowledge-stats" aria-label="知识库概况">
      <div><span>知识文档</span><strong>{{ documentsLoaded ? total : '—' }}</strong><small>当前租户全部文档</small></div>
      <div><span>本页已索引</span><strong>{{ documentsLoaded ? indexedCount : '—' }}</strong><small>当前页可供检索</small></div>
      <div><span>本页内容分片</span><strong>{{ documentsLoaded ? totalChunks : '—' }}</strong><small>{{ documentsLoaded ? formatBytes(totalSize) : '等待读取' }}</small></div>
      <div class="knowledge-index-state"><span>索引存储</span><strong>{{ documentsLoaded ? (runtime.persistentIndex ? '持久化' : '内存') : '读取中' }}</strong><small>{{ runtime.indexMode || '索引模式未返回' }}</small></div>
    </section>

    <el-tabs v-model="activeTab" class="knowledge-tabs">
      <el-tab-pane name="documents" label="文档">
        <section class="knowledge-panel documents-panel" aria-label="已上传文档">
          <div class="knowledge-panel-heading">
            <div><h2>已上传文档</h2><p>查看文档的归属、索引状态和内容切片。</p></div>
            <el-button :loading="loading" @click="load">刷新列表</el-button>
          </div>

          <el-table v-loading="loading" :data="documents" class="knowledge-table">
            <el-table-column label="文档" min-width="270"><template #default="{ row }"><div class="document-name"><FileText class="document-icon" /><div><strong>{{ row.filename }}</strong><small>{{ categoryLabel(row.category) }} · {{ formatBytes(row.metadata?.size) }}</small></div></div></template></el-table-column>
            <el-table-column label="关联智能体" min-width="175"><template #default="{ row }">{{ agentLabel(row.workflowId) }}</template></el-table-column>
            <el-table-column label="索引状态" width="110"><template #default="{ row }"><el-tag :type="row.status === 'INDEXED' ? 'success' : 'warning'" effect="light">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
            <el-table-column label="内容分片" width="100" align="right"><template #default="{ row }">{{ row.metadata?.chunks || 0 }}</template></el-table-column>
            <el-table-column label="上传时间" min-width="165"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></el-table-column>
            <el-table-column label="操作" width="96" align="right"><template #default="{ row }"><el-button plain type="primary" @click="showDocument(row)">查看详情</el-button></template></el-table-column>
            <template #empty><el-empty description="还没有知识文档" /></template>
          </el-table>

          <div v-loading="loading" class="knowledge-mobile-list">
            <article v-for="row in documents" :key="row.id" class="knowledge-mobile-document">
              <div class="knowledge-mobile-document-head"><FileText class="document-icon" /><div><strong>{{ row.filename }}</strong><small>{{ categoryLabel(row.category) }} · {{ formatBytes(row.metadata?.size) }}</small></div></div>
              <div class="knowledge-mobile-document-meta"><span>{{ agentLabel(row.workflowId) }}</span><el-tag :type="row.status === 'INDEXED' ? 'success' : 'warning'" effect="light">{{ statusLabel(row.status) }}</el-tag></div>
              <div class="knowledge-mobile-document-foot"><small>{{ row.metadata?.chunks || 0 }} 个分片 · {{ formatTime(row.createdAt) }}</small><el-button plain type="primary" @click="showDocument(row)">查看详情</el-button></div>
            </article>
            <el-empty v-if="!loading && !documents.length" description="还没有知识文档" />
          </div>

          <div class="list-pagination knowledge-pagination"><el-pagination v-model:current-page="page" v-model:page-size="pageSize" :total="total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next" hide-on-single-page @current-change="changePage" @size-change="changePageSize" /></div>
        </section>
      </el-tab-pane>

      <el-tab-pane name="policy" label="检索策略">
        <section class="knowledge-panel policy-panel" aria-label="知识库策略">
          <div class="knowledge-panel-heading">
            <div><h2>知识库策略</h2><p>每个智能体只检索属于自己的文档，按需调整回答时的检索规则。</p></div>
            <el-button v-if="agents.length" :loading="bindingLoading" @click="loadBinding">刷新策略</el-button>
          </div>
          <el-empty v-if="!agents.length" description="暂无可配置的智能体；上传文档时仍可输入智能体标识。" />
          <template v-else>
            <el-alert v-if="bindingError" :title="bindingError" type="warning" :closable="false" show-icon />
            <div class="knowledge-policy-target">
              <label for="knowledge-agent">当前智能体</label>
              <el-select id="knowledge-agent" v-model="bindingWorkflowId" filterable :disabled="bindingSaving" placeholder="选择智能体" @change="loadBinding"><el-option v-for="agent in agents" :key="agentKey(agent)" :label="agentName(agent) + ' · ' + agentKey(agent)" :value="agentKey(agent)" /></el-select>
              <small>本页 {{ documents.filter(item => item.workflowId === bindingWorkflowId).length }} 份文档归属该智能体</small>
            </div>
            <el-form v-loading="bindingLoading" class="knowledge-policy-form" label-position="top" :model="knowledgeBinding" :disabled="!canManageBinding || bindingLoading || bindingSaving">
              <div class="knowledge-policy-section">
                <div class="knowledge-section-copy"><h3>何时检索</h3><p>决定智能体在回答前是否查询知识文档。</p></div>
                <el-form-item label="检索模式"><el-radio-group v-model="knowledgeBinding.retrievalMode"><el-radio-button value="auto">按需检索</el-radio-button><el-radio-button value="always">每次强制检索</el-radio-button><el-radio-button value="disabled">禁用</el-radio-button></el-radio-group></el-form-item>
              </div>
              <div class="knowledge-policy-section">
                <div class="knowledge-section-copy"><h3>匹配要求</h3><p>控制取回的片段数量，以及内容的最低相似度。</p></div>
                <div class="knowledge-number-grid"><el-form-item label="召回数量"><el-input-number v-model="knowledgeBinding.topK" :min="1" :max="20" controls-position="right" /></el-form-item><el-form-item label="最低相似度"><el-input-number v-model="knowledgeBinding.minScore" :min="0" :max="1" :step="0.05" :precision="2" controls-position="right" /></el-form-item></div>
              </div>
              <div class="knowledge-policy-section">
                <div class="knowledge-section-copy"><h3>没有匹配时</h3><p>明确缺少依据时，智能体是否还可以给出一般性回答。</p></div>
                <el-form-item label="无匹配知识时"><el-select v-model="knowledgeBinding.noMatchPolicy"><el-option label="允许模型回答，但必须说明证据不足" value="allow-model" /><el-option label="阻止回答，必须先补充知识" value="require-evidence" /></el-select></el-form-item>
              </div>
              <div class="knowledge-policy-actions"><small v-if="!canManageBinding">当前账号可查看策略；修改需要管理员或运维人员权限。</small><el-button v-permission="'PUT /api/v1/ai/workflows/:id/knowledge-binding'" type="primary" :loading="bindingSaving" :disabled="!canManageBinding || !bindingWorkflowId" @click="saveBinding">保存知识库策略</el-button></div>
            </el-form>
          </template>
        </section>
      </el-tab-pane>
    </el-tabs>

    <el-dialog v-model="uploadDialog" title="上传知识文档并绑定智能体" width="min(620px, 94vw)">
      <div class="knowledge-upload-step"><span>1</span><div><strong>选择文档</strong><small>单个文件不超过 32 兆字节</small></div></div>
      <el-upload ref="uploadRef" drag :auto-upload="false" :disabled="!canUpload || uploading" :limit="1" accept=".pdf,.docx,.pptx,.xlsx,.odt,.odp,.ods,.txt,.md,.csv,.json,.html,.htm,.xml" :on-change="chooseFile" :on-remove="removeFile" :on-exceed="rejectExtra"><Upload class="upload-icon" /><div class="el-upload__text">拖放文件到这里，或<em>点击选择</em></div><template #tip><div class="el-upload__tip">支持 PDF、办公文档、网页和文本；扫描件需先进行文字识别。</div></template></el-upload>
      <div class="knowledge-upload-step knowledge-upload-step-gap"><span>2</span><div><strong>关联智能体</strong><small>每份文档只属于一个智能体</small></div></div>
      <el-form label-position="top" class="knowledge-upload-form">
        <el-form-item label="关联智能体（必选）"><el-select v-model="workflowId" filterable allow-create default-first-option :disabled="uploading" placeholder="选择或输入智能体标识"><el-option v-for="agent in agents" :key="agentKey(agent)" :label="agentName(agent) + ' · ' + agentKey(agent)" :value="agentKey(agent)" /></el-select><small class="field-tip">未启动工作流服务时，可以输入计划使用的智能体标识。</small></el-form-item>
        <div class="metadata-grid"><el-form-item label="知识分类（可选）"><el-select v-model="category" :disabled="uploading"><el-option label="设备手册" value="manual" /><el-option label="告警处置操作规程" value="alarm-sop" /><el-option label="运维维修" value="maintenance" /><el-option label="消防规范" value="regulation" /><el-option label="常见问题" value="faq" /></el-select></el-form-item><el-form-item label="知识标签（可选）"><el-select v-model="tags" multiple filterable allow-create default-first-option :disabled="uploading" placeholder="输入标签后回车" /></el-form-item></div>
      </el-form>
      <template #footer><el-button @click="uploadDialog=false">取消</el-button><el-button v-permission="'POST /api/v1/knowledge/documents'" type="primary" :loading="uploading" :disabled="!canUpload || !selectedFile || !workflowId" @click="upload">上传并建立索引</el-button></template>
    </el-dialog>

    <el-dialog v-model="detailDialog" title="知识文档详情与切片" width="min(900px, 96vw)">
      <el-alert v-if="detailError" :title="detailError" type="error" :closable="false" show-icon />
      <div v-loading="detailLoading" class="knowledge-detail">
        <div v-if="selectedDocument" class="knowledge-detail-file"><FileText class="document-icon" /><div><strong>{{ selectedDocument.filename }}</strong><small>{{ selectedDocument.id }}</small></div><el-tag :type="selectedDocument.status === 'INDEXED' ? 'success' : 'warning'">{{ statusLabel(selectedDocument.status) }}</el-tag></div>
        <dl v-if="selectedDocument" class="knowledge-detail-meta"><div><dt>关联智能体</dt><dd>{{ agentLabel(selectedDocument.workflowId) }}</dd></div><div><dt>知识分类</dt><dd>{{ categoryLabel(selectedDocument.category) }}</dd></div><div><dt>内容统计</dt><dd>{{ selectedDocument.metadata?.chunks || 0 }} 个分片 · {{ formatBytes(selectedDocument.metadata?.size) }}</dd></div><div><dt>上传时间</dt><dd>{{ formatTime(selectedDocument.createdAt) }}</dd></div><div v-if="selectedDocument.tags?.length"><dt>知识标签</dt><dd>{{ selectedDocument.tags.join('、') }}</dd></div></dl>
        <section v-if="selectedDetail?.index" class="knowledge-index-rules"><div class="knowledge-detail-section-heading"><h3>索引与切片规则</h3><span>{{ selectedDetail.index.mode }} · {{ selectedDetail.index.vectorizer }}</span></div><div class="knowledge-rule-grid"><div><small>切片策略</small><strong>{{ selectedDetail.index.chunking?.strategy === 'fixed-window-overlap' ? '固定窗口 + 重叠' : selectedDetail.index.chunking?.strategy }}</strong></div><div><small>窗口 / 重叠</small><strong>{{ selectedDetail.index.chunking?.size }} / {{ selectedDetail.index.chunking?.overlap }} 字符</strong></div><div><small>提取文本</small><strong>{{ selectedDetail.index.extractedChars || 0 }} 字符</strong></div><div><small>实际分片</small><strong>{{ selectedDetail.index.chunkCount }}</strong></div></div><p v-if="selectedDetail.index.embeddingModel">向量模型：{{ selectedDetail.index.embeddingModel }}</p></section>
        <section v-if="selectedDetail" class="knowledge-chunks"><div class="knowledge-detail-section-heading"><h3>切片内容</h3><span>{{ selectedDetail.chunks?.length || 0 }} 个分片，点击逐条查看</span></div><div v-if="selectedDetail.chunks?.length" class="knowledge-chunk-list"><details v-for="(row, index) in selectedDetail.chunks" :key="row.chunkId || index" :open="index === 0" class="knowledge-chunk"><summary><span class="knowledge-chunk-number">{{ index + 1 }}</span><span>字符范围 [{{ row.startChar }}, {{ row.endChar }})</span><el-tag :type="row.vectorized ? 'success' : 'info'" size="small">{{ row.vectorized ? '向量化完成' : '非向量索引' }}</el-tag></summary><div class="knowledge-chunk-body"><p>{{ row.content }}</p><small>重叠 {{ row.overlapChars || 0 }} 字符 · {{ row.characterCount }} 字符 · {{ row.chunkId }}</small></div></details></div><el-empty v-else description="索引中没有可查看的切片" :image-size="56" /></section>
      </div>
      <template #footer><el-button @click="detailDialog=false">关闭</el-button></template>
    </el-dialog>
  </div>
</template>

<style scoped>
.knowledge-page { display:grid; gap:18px; min-width:0; padding-bottom:24px; }
.knowledge-intro { display:flex; align-items:center; justify-content:space-between; gap:20px; padding:4px 0 2px; }
.knowledge-kicker { color:var(--brand-navy); font-size:14px; font-weight:700; }
.knowledge-intro-copy p { max-width:650px; margin:5px 0 0; color:var(--muted-foreground); font-size:14px; }
.knowledge-intro-actions { display:flex; gap:8px; flex:none; }
.knowledge-intro-actions .el-button { margin:0; }
.knowledge-intro-actions svg { width:16px; height:16px; }
.knowledge-stats { display:grid; grid-template-columns:repeat(4,minmax(0,1fr)); gap:1px; overflow:hidden; border-radius:var(--radius); background:#e9edf3; }
.knowledge-stats > div { min-width:0; min-height:104px; padding:16px 18px; display:grid; align-content:space-between; background:#fff; }
.knowledge-stats span,.knowledge-stats small { color:var(--muted-foreground); font-size:12px; }
.knowledge-stats strong { color:var(--foreground); font-size:26px; line-height:1.15; font-weight:650; font-variant-numeric:tabular-nums; }
.knowledge-stats .knowledge-index-state strong { color:var(--brand-navy); font-size:18px; }
.knowledge-tabs { min-width:0; }
.knowledge-tabs :deep(.el-tabs__header) { margin-bottom:14px; }
.knowledge-tabs :deep(.el-tabs__item) { height:42px; padding:0 22px; font-size:14px; }
.knowledge-tabs :deep(.el-tabs__active-bar) { height:3px; border-radius:3px; }
.knowledge-panel { min-width:0; overflow:hidden; background:#fff; border-radius:var(--radius); box-shadow:0 1px 2px rgba(19,56,108,.045); }
.knowledge-panel-heading { display:flex; align-items:center; justify-content:space-between; gap:16px; padding:22px 24px 14px; }
.knowledge-panel-heading h2 { margin:0; font-size:18px; line-height:1.3; }
.knowledge-panel-heading p { margin:5px 0 0; color:var(--muted-foreground); font-size:13px; }
.knowledge-table { width:100%; }
.knowledge-table :deep(.el-table__header th) { background:#fff; }
.knowledge-table :deep(.el-table__cell) { padding:12px 0; }
.document-name { min-width:0; display:flex; align-items:center; gap:12px; }
.document-icon { width:36px; height:36px; flex:none; padding:9px; color:var(--brand-navy); background:#eef4fc; border-radius:10px; }
.document-name > div { min-width:0; display:grid; gap:3px; }
.document-name strong { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; font-size:14px; }
.document-name small { color:var(--muted-foreground); font-size:12px; }
.knowledge-pagination { border-top:1px solid #f0f2f5; }
.knowledge-mobile-list { display:none; }
.policy-panel { padding-bottom:8px; }
.knowledge-policy-target { margin:0 24px; padding:16px 18px; display:grid; grid-template-columns:130px minmax(0,360px) 1fr; align-items:center; gap:14px; background:#f5f7fa; border-radius:10px; }
.knowledge-policy-target label { font-size:13px; font-weight:600; }
.knowledge-policy-target small { color:var(--muted-foreground); font-size:12px; }
.knowledge-policy-form { padding:6px 24px 16px; }
.knowledge-policy-section { padding:20px 0; display:grid; grid-template-columns:minmax(170px,.8fr) minmax(0,1.2fr); align-items:start; gap:24px; border-bottom:1px solid #eef0f3; }
.knowledge-section-copy h3 { margin:0; font-size:15px; }
.knowledge-section-copy p { max-width:280px; margin:5px 0 0; color:var(--muted-foreground); font-size:12px; line-height:1.6; }
.knowledge-policy-section .el-form-item { width:100%; max-width:430px; margin:0; }
.knowledge-policy-section :deep(.el-radio-group),.knowledge-policy-section :deep(.el-select) { width:100%; }
.knowledge-policy-section :deep(.el-radio-button) { flex:1; }
.knowledge-policy-section :deep(.el-radio-button__inner) { width:100%; padding-inline:8px; }
.knowledge-number-grid { display:grid; grid-template-columns:1fr 1fr; gap:16px; max-width:430px; }
.knowledge-number-grid :deep(.el-input-number) { width:100%; }
.knowledge-policy-actions { display:flex; align-items:center; justify-content:flex-end; gap:16px; padding-top:20px; }
.knowledge-policy-actions small { margin-right:auto; color:var(--muted-foreground); font-size:12px; }
.knowledge-upload-step { display:flex; align-items:center; gap:10px; margin-bottom:12px; }
.knowledge-upload-step > span { width:25px; height:25px; flex:none; display:grid; place-items:center; color:#fff; background:var(--brand-navy); border-radius:50%; font-size:12px; font-weight:700; }
.knowledge-upload-step > div { display:flex; align-items:baseline; gap:9px; }
.knowledge-upload-step strong { font-size:14px; }
.knowledge-upload-step small,.field-tip { color:var(--muted-foreground); font-size:12px; line-height:1.5; }
.knowledge-upload-step-gap { margin-top:26px; }
.upload-icon { width:32px; height:32px; color:var(--primary); }
.knowledge-upload-form :deep(.el-select) { width:100%; }
.metadata-grid { display:grid; grid-template-columns:1fr 1fr; gap:12px; }
.knowledge-detail { min-height:140px; }
.knowledge-detail-file { display:flex; align-items:center; gap:12px; padding:3px 0 18px; }
.knowledge-detail-file > div { min-width:0; flex:1; display:grid; gap:4px; }
.knowledge-detail-file strong { overflow:hidden; font-size:16px; text-overflow:ellipsis; white-space:nowrap; }
.knowledge-detail-file small { color:var(--muted-foreground); font-size:12px; overflow-wrap:anywhere; }
.knowledge-detail-meta { margin:0; padding:16px 18px; display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:16px; background:#f5f7fa; border-radius:10px; }
.knowledge-detail-meta div { min-width:0; }
.knowledge-detail-meta dt { color:var(--muted-foreground); font-size:12px; }
.knowledge-detail-meta dd { margin:5px 0 0; font-size:13px; overflow-wrap:anywhere; }
.knowledge-index-rules,.knowledge-chunks { margin-top:24px; }
.knowledge-detail-section-heading { display:flex; align-items:baseline; justify-content:space-between; gap:12px; margin-bottom:12px; }
.knowledge-detail-section-heading h3 { margin:0; font-size:15px; }
.knowledge-detail-section-heading span { color:var(--muted-foreground); font-size:12px; }
.knowledge-rule-grid { display:grid; grid-template-columns:repeat(4,minmax(0,1fr)); gap:8px; }
.knowledge-rule-grid > div { min-width:0; padding:12px; display:grid; gap:5px; background:#f5f7fa; border-radius:8px; }
.knowledge-rule-grid small { color:var(--muted-foreground); font-size:12px; }
.knowledge-rule-grid strong { font-size:13px; overflow-wrap:anywhere; }
.knowledge-index-rules p { margin:10px 0 0; color:var(--muted-foreground); font-size:12px; }
.knowledge-chunk-list { display:grid; gap:8px; }
.knowledge-chunk { border:1px solid #e9edf3; border-radius:9px; }
.knowledge-chunk summary { min-height:52px; padding:10px 14px; display:flex; align-items:center; gap:12px; cursor:pointer; list-style:none; font-size:13px; }
.knowledge-chunk summary::-webkit-details-marker { display:none; }
.knowledge-chunk summary .el-tag { margin-left:auto; }
.knowledge-chunk-number { width:26px; height:26px; flex:none; display:grid; place-items:center; color:var(--brand-navy); background:#eef4fc; border-radius:7px; font-size:12px; font-weight:700; }
.knowledge-chunk-body { padding:0 14px 14px 52px; }
.knowledge-chunk-body p { max-height:220px; margin:0; padding:12px; overflow:auto; white-space:pre-wrap; overflow-wrap:anywhere; background:#f5f7fa; border-radius:7px; font-size:13px; line-height:1.7; }
.knowledge-chunk-body small { display:block; margin-top:7px; color:var(--muted-foreground); font-size:12px; overflow-wrap:anywhere; }
:deep(.el-dialog__body) { overflow-x:hidden; }
:deep(.el-upload__text),:deep(.el-upload__tip) { color:#475569; }
@media (max-width:900px) { .knowledge-stats { grid-template-columns:repeat(2,minmax(0,1fr)); }.knowledge-policy-target { grid-template-columns:120px minmax(0,1fr); }.knowledge-policy-target small { grid-column:2; } }
@media (max-width:640px) {
  .knowledge-page { gap:14px; }
  .knowledge-intro { align-items:flex-start; flex-direction:column; gap:14px; }
  .knowledge-intro-actions { width:100%; }
  .knowledge-intro-actions .el-button { flex:1; padding-inline:8px; }
  .knowledge-stats > div { min-height:88px; padding:12px; }
  .knowledge-stats strong { font-size:22px; }
  .knowledge-stats .knowledge-index-state strong { font-size:16px; }
  .knowledge-panel-heading { padding:18px 16px 10px; }
  .knowledge-panel-heading p { display:none; }
  .knowledge-table { display:none; }
  .knowledge-mobile-list { min-height:90px; padding:0 16px 12px; display:grid; gap:9px; }
  .knowledge-mobile-document { padding:14px; display:grid; gap:12px; background:#f5f7fa; border-radius:10px; }
  .knowledge-mobile-document-head { min-width:0; display:flex; align-items:center; gap:10px; }
  .knowledge-mobile-document-head > div { min-width:0; display:grid; gap:3px; }
  .knowledge-mobile-document-head strong { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; font-size:14px; }
  .knowledge-mobile-document-head small { color:var(--muted-foreground); font-size:12px; }
  .knowledge-mobile-document-meta,.knowledge-mobile-document-foot { display:flex; align-items:center; justify-content:space-between; gap:8px; font-size:12px; }
  .knowledge-mobile-document-meta span { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .knowledge-mobile-document-foot small { color:var(--muted-foreground); font-size:12px; }
  .knowledge-pagination { justify-content:center; padding:10px; }
  .knowledge-pagination :deep(.el-pagination__sizes),.knowledge-pagination :deep(.el-pagination__total) { display:none; }
  .knowledge-policy-target { margin:0 16px; grid-template-columns:1fr; gap:7px; }
  .knowledge-policy-target small { grid-column:1; }
  .knowledge-policy-form { padding-inline:16px; }
  .knowledge-policy-section { grid-template-columns:1fr; gap:12px; }
  .knowledge-section-copy p { max-width:none; }
  .knowledge-number-grid { gap:8px; }
  .knowledge-policy-actions { flex-direction:column; align-items:stretch; }
  .knowledge-upload-step > div { display:grid; gap:0; }
  .metadata-grid,.knowledge-detail-meta { grid-template-columns:1fr; }
  .knowledge-rule-grid { grid-template-columns:repeat(2,minmax(0,1fr)); }
  .knowledge-detail-section-heading { align-items:flex-start; flex-direction:column; gap:3px; }
  .knowledge-chunk summary { flex-wrap:wrap; }
  .knowledge-chunk-body { padding-left:14px; }
}
</style>
