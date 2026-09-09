<script setup>
import { statusLabel } from '../presentation'
import { computed, onMounted, ref } from 'vue'
import { Collection, UploadFilled } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'

import { api, formatTime, notifyError, session } from '../api'

const emit = defineEmits(['navigate'])
const uploadRef = ref(null)
const documents = ref([])
const agents = ref([])
const loading = ref(false)
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
const bindingWorkflowId = ref('')
const knowledgeBinding = ref({ retrievalMode:'auto', topK:5, minScore:0.25, noMatchPolicy:'allow-model' })
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)

const canUpload = computed(() => session.role === 'admin' || session.role === 'operator')
const canManageBinding = computed(() => session.role === 'admin' || session.role === 'operator')
const indexedCount = computed(() => documents.value.filter(item => item.status === 'INDEXED').length)
const totalChunks = computed(() => documents.value.reduce((sum, item) => sum + Number(item.metadata?.chunks || 0), 0))
const totalSize = computed(() => documents.value.reduce((sum, item) => sum + Number(item.metadata?.size || 0), 0))
const selectedAgent = computed(() => agents.value.find(item => (item.id || item.workflowId) === workflowId.value))
const selectedBindingAgent = computed(() => agents.value.find(item => agentKey(item) === bindingWorkflowId.value))

function agentKey(item) { return item?.id || item?.workflowId || '' }
function agentName(item) { return item?.name || item?.label || agentKey(item) || '未命名智能体' }
function agentLabel(id) {
  if (!id) return '未关联智能体'
  const agent = agents.value.find(item => agentKey(item) === id)
  return agent ? agentName(agent) : id
}
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
    } else {
      throw documentResult.reason
    }
    if (agentResult.status === 'fulfilled') agents.value = Array.isArray(agentResult.value.items) ? agentResult.value.items.filter(item => item.enabled !== false) : []
    else agentError.value = agentResult.reason?.message || '智能体列表读取失败'
    if (!workflowId.value && agents.value.length) workflowId.value = agentKey(agents.value[0])
    if (!bindingWorkflowId.value && agents.value.length) bindingWorkflowId.value = agentKey(agents.value[0])
    if (bindingWorkflowId.value) void loadBinding()
  } catch (error) {
    if (error?.message?.includes('workflows')) agentError.value = error.message
    else notifyError(error)
  } finally {
    loading.value = false
  }
}

async function loadBinding() {
  if (!bindingWorkflowId.value) return
  bindingLoading.value = true
  bindingError.value = ''
  try {
    const value = await api(`/api/v1/ai/workflows/${encodeURIComponent(bindingWorkflowId.value)}/knowledge-binding`)
    knowledgeBinding.value = {
      retrievalMode:value.retrievalMode || 'auto',
      topK:Number(value.topK) || 5,
      minScore:Number(value.minScore ?? 0.25),
      noMatchPolicy:value.noMatchPolicy || 'allow-model'
    }
  } catch (error) {
    bindingError.value = error.message || '知识库策略读取失败'
  } finally {
    bindingLoading.value = false
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
  <section class="knowledge-hero">
    <div><span>专属知识管理</span><h3>知识库</h3><p>上传设备手册、维护记录和处置规范。每份文档归属于一个智能体，用于回答与该业务相关的问题。</p></div>
    <div class="hero-actions"><el-tag :type="runtime.persistentIndex ? 'success' : 'warning'" effect="dark">{{ runtime.persistentIndex ? '持久化索引' : '本地内存索引' }}</el-tag><el-button type="primary" plain @click="emit('navigate','ai')">打开智能助手</el-button></div>
  </section>

  <el-alert v-if="agentError" :title="agentError" type="warning" :closable="false" show-icon />
  <el-alert v-if="!runtime.persistentIndex" title="当前索引不是持久化向量库；文档记录会保存，但重启后检索索引需要重新建立。请启动本地向量数据库。" type="warning" :closable="false" show-icon />

  <div class="knowledge-stats"><el-card shadow="never" class="surface-card"><span>知识文档</span><strong>{{ total }}</strong><small>当前租户</small></el-card><el-card shadow="never" class="surface-card"><span>已完成索引</span><strong>{{ indexedCount }}</strong><small>可供智能体检索</small></el-card><el-card shadow="never" class="surface-card"><span>内容分片</span><strong>{{ totalChunks }}</strong><small>{{ formatBytes(totalSize) }}</small></el-card></div>

  <div class="page-toolbar knowledge-toolbar"><el-button type="primary" :disabled="!canUpload" @click="openUpload">上传并绑定智能体</el-button><el-button :loading="loading" @click="load">刷新</el-button><span>共 {{ total }} 份文档，上传时必须选择或输入一个智能体标识。</span></div>

  <el-card v-if="agents.length" shadow="never" class="surface-card knowledge-policy-card">
    <template #header><div class="card-header"><div><strong>知识库策略</strong><small>文档、绑定和检索策略统一在本页面维护</small></div><el-button size="small" :loading="bindingLoading" @click="loadBinding">刷新策略</el-button></div></template>
    <el-alert v-if="bindingError" :title="bindingError" type="warning" :closable="false" show-icon />
    <el-form label-position="top" :model="knowledgeBinding" :disabled="!canManageBinding || bindingLoading || bindingSaving">
      <div class="knowledge-policy-grid">
        <el-form-item label="当前智能体"><el-select v-model="bindingWorkflowId" filterable placeholder="选择智能体" @change="loadBinding"><el-option v-for="agent in agents" :key="agentKey(agent)" :label="`${agentName(agent)} · ${agentKey(agent)}`" :value="agentKey(agent)" /></el-select></el-form-item>
        <el-form-item label="检索模式"><el-radio-group v-model="knowledgeBinding.retrievalMode"><el-radio-button value="auto">按需检索</el-radio-button><el-radio-button value="always">每次强制检索</el-radio-button><el-radio-button value="disabled">禁用</el-radio-button></el-radio-group></el-form-item>
      </div>
      <el-alert :title="`${documents.filter(item => item.workflowId === bindingWorkflowId).length} 份文档已直接绑定当前智能体`" type="success" :closable="false" show-icon />
      <div class="binding-numbers"><el-form-item label="召回数量"><el-input-number v-model="knowledgeBinding.topK" :min="1" :max="20" controls-position="right" /></el-form-item><el-form-item label="最低相似度"><el-input-number v-model="knowledgeBinding.minScore" :min="0" :max="1" :step="0.05" :precision="2" controls-position="right" /></el-form-item></div>
      <el-form-item label="无匹配知识时"><el-select v-model="knowledgeBinding.noMatchPolicy"><el-option label="允许模型回答，但必须说明证据不足" value="allow-model" /><el-option label="阻止回答，必须先补充知识" value="require-evidence" /></el-select></el-form-item>
      <el-alert title="每个智能体只能检索自己的文档，关联范围由服务端校验。" type="info" :closable="false" show-icon />
      <div class="knowledge-policy-actions"><small v-if="!canManageBinding">当前账号可查看策略；修改需要管理员或运维人员权限。</small><el-button type="primary" :loading="bindingSaving" :disabled="!canManageBinding || !bindingWorkflowId" @click="saveBinding">保存知识库策略</el-button></div>
    </el-form>
  </el-card>

  <el-card shadow="never" class="surface-card table-card documents-card">
    <template #header><div class="card-header"><div><strong>已上传文档</strong><small>文档和智能体归属持久化保存</small></div><el-tag effect="plain">{{ runtime.persistentIndex ? '持久化索引' : '内存索引' }}</el-tag></div></template>
    <el-table v-loading="loading" :data="documents" stripe>
      <el-table-column label="文档" min-width="240"><template #default="{ row }"><div class="document-name"><el-icon><Collection /></el-icon><span><b>{{ row.filename }}</b><small>{{ row.id }}</small></span></div></template></el-table-column>
      <el-table-column label="关联智能体" min-width="190"><template #default="{ row }"><b>{{ agentLabel(row.workflowId) }}</b><small class="subline">{{ row.workflowId || '未关联' }}</small></template></el-table-column>
      <el-table-column label="索引" width="105" align="center"><template #default="{ row }"><el-tag :type="row.status === 'INDEXED' ? 'success' : 'warning'">{{ statusLabel(row.status) }}</el-tag></template></el-table-column>
      <el-table-column label="分片 / 大小" width="120" align="right"><template #default="{ row }">{{ row.metadata?.chunks || 0 }}<small class="subline">{{ formatBytes(row.metadata?.size) }}</small></template></el-table-column>
      <el-table-column label="上传时间" min-width="160"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></el-table-column>
      <el-table-column label="操作" width="110" fixed="right" align="center"><template #default="{ row }"><div class="table-actions"><el-button plain type="primary" @click="showDocument(row)">详情</el-button></div></template></el-table-column>
      <template #empty><el-empty description="暂无知识库文档" /></template>
    </el-table>
    <div class="list-pagination"><el-pagination v-model:current-page="page" v-model:page-size="pageSize" :total="total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next, jumper" @current-change="changePage" @size-change="changePageSize" /></div>
  </el-card>

  <el-dialog v-model="uploadDialog" title="上传知识文档并绑定智能体" width="min(650px, 94vw)">
    <el-upload ref="uploadRef" drag :auto-upload="false" :disabled="!canUpload || uploading" :limit="1" accept=".pdf,.docx,.pptx,.xlsx,.odt,.odp,.ods,.txt,.md,.csv,.json,.html,.htm,.xml" :on-change="chooseFile" :on-remove="removeFile" :on-exceed="rejectExtra"><el-icon class="upload-icon"><UploadFilled /></el-icon><div class="el-upload__text">拖放文件到这里，或<em>点击选择</em></div><template #tip><div class="el-upload__tip">支持文档、办公文档、开放文档、网页或标记文档和通用字符编码文本；扫描版文档需要先完成文字识别。</div></template></el-upload>
    <el-form label-position="top" class="top-gap">
      <el-form-item label="关联智能体（必选）"><el-select v-model="workflowId" filterable allow-create default-first-option :disabled="uploading" placeholder="选择或输入智能体标识"><el-option v-for="agent in agents" :key="agentKey(agent)" :label="`${agentName(agent)} · ${agentKey(agent)}`" :value="agentKey(agent)" /></el-select><small class="field-tip">上传后检索服务端会强制使用这个智能体标识，不会跨智能体检索；未启动工作流服务时也可以先输入计划使用的智能体标识。</small></el-form-item>
      <div class="metadata-grid"><el-form-item label="知识分类（可选）"><el-select v-model="category" :disabled="uploading"><el-option label="设备手册" value="manual" /><el-option label="告警处置操作规程" value="alarm-sop" /><el-option label="运维维修" value="maintenance" /><el-option label="消防规范" value="regulation" /><el-option label="常见问题" value="faq" /></el-select></el-form-item><el-form-item label="知识标签（可选）"><el-select v-model="tags" multiple filterable allow-create default-first-option :disabled="uploading" placeholder="输入标签后回车" /></el-form-item></div>
    </el-form>
    <template #footer><el-button @click="uploadDialog=false">取消</el-button><el-button type="primary" :loading="uploading" :disabled="!canUpload || !selectedFile || !workflowId" @click="upload">上传并建立索引</el-button></template>
  </el-dialog>

  <el-dialog v-model="detailDialog" title="知识文档详情与切片" width="min(1080px, 96vw)">
    <el-alert v-if="detailError" :title="detailError" type="error" :closable="false" show-icon />
    <div v-loading="detailLoading" class="detail-body">
      <el-descriptions v-if="selectedDocument" :column="2" border>
        <el-descriptions-item label="文件名">{{ selectedDocument.filename }}</el-descriptions-item>
        <el-descriptions-item label="文档标识">{{ selectedDocument.id }}</el-descriptions-item>
        <el-descriptions-item label="关联智能体">{{ agentLabel(selectedDocument.workflowId) }}（{{ selectedDocument.workflowId || '未关联' }}）</el-descriptions-item>
        <el-descriptions-item label="索引状态">{{ statusLabel(selectedDocument.status) }}</el-descriptions-item>
        <el-descriptions-item label="知识分类">{{ selectedDocument.category || '未分类' }}</el-descriptions-item>
        <el-descriptions-item label="内容统计">{{ selectedDocument.metadata?.chunks || 0 }} 个分片 / {{ formatBytes(selectedDocument.metadata?.size) }}</el-descriptions-item>
        <el-descriptions-item label="标签">{{ (selectedDocument.tags || []).join('、') || '无' }}</el-descriptions-item>
        <el-descriptions-item label="上传时间">{{ formatTime(selectedDocument.createdAt) }}</el-descriptions-item>
      </el-descriptions>
      <div v-if="selectedDetail?.index" class="chunk-policy">
        <div class="chunk-policy-title"><strong>索引与切片规则</strong><el-tag size="small" type="success" effect="plain">{{ selectedDetail.index.mode }}</el-tag><el-tag size="small" effect="plain">{{ selectedDetail.index.vectorizer }}</el-tag><el-tag v-if="selectedDetail.index.embeddingModel" size="small" type="success" effect="plain">{{ selectedDetail.index.embeddingModel }}</el-tag></div>
        <div class="chunk-policy-grid"><span>切片策略<strong>{{ selectedDetail.index.chunking?.strategy === 'fixed-window-overlap' ? '固定窗口 + 重叠' : selectedDetail.index.chunking?.strategy }}</strong></span><span>窗口<strong>{{ selectedDetail.index.chunking?.size }} 字符</strong></span><span>重叠<strong>{{ selectedDetail.index.chunking?.overlap }} 字符</strong></span><span>提取文本<strong>{{ selectedDetail.index.extractedChars || 0 }} 字符</strong></span><span>实际分片<strong>{{ selectedDetail.index.chunkCount }}</strong></span></div>
        <small>{{ selectedDetail.index.chunking?.normalization || '先提取并清洗文本，再进行固定窗口切片。' }}；字符范围采用左闭右开：包含起始位置，不包含结束位置。页面不展示高维向量本身，只展示切片文本和向量化状态。</small>
      </div>
      <el-table v-if="selectedDetail" :data="selectedDetail.chunks || []" stripe border class="chunk-table">
        <el-table-column label="#" prop="index" width="58" align="center" />
        <el-table-column label="字符范围" width="138"><template #default="{ row }">[{{ row.startChar }}, {{ row.endChar }})<small class="subline">{{ row.characterCount }} 字符</small></template></el-table-column>
        <el-table-column label="重叠" width="72" align="center"><template #default="{ row }">{{ row.overlapChars || 0 }}</template></el-table-column>
        <el-table-column label="向量化" width="92" align="center"><template #default="{ row }"><el-tag :type="row.vectorized ? 'success' : 'info'" size="small">{{ row.vectorized ? '已完成' : '非向量索引' }}</el-tag></template></el-table-column>
        <el-table-column label="切片内容" min-width="480"><template #default="{ row }"><div class="chunk-content">{{ row.content }}</div><small class="subline">{{ row.chunkId }}</small></template></el-table-column>
        <template #empty><el-empty description="索引中没有可查看的切片" :image-size="56" /></template>
      </el-table>
    </div>
    <template #footer><el-button @click="detailDialog=false">关闭</el-button></template>
  </el-dialog>
</template>

<style scoped>
.knowledge-hero { min-height:112px; margin-bottom:14px; padding:20px 22px; display:flex; align-items:center; justify-content:space-between; gap:20px; color:#fff; background:linear-gradient(125deg,#0d2850,#1554ad 68%,#1677ff); border-radius:6px; overflow:hidden; position:relative; }
.knowledge-hero::after { content:""; width:220px; height:220px; position:absolute; right:-70px; top:-115px; border:1px solid rgba(255,255,255,.16); border-radius:50%; box-shadow:0 0 0 42px rgba(255,255,255,.035),0 0 0 84px rgba(255,255,255,.025); }
.knowledge-hero>div { position:relative; z-index:1; }.knowledge-hero span { color:rgba(255,255,255,.68); font-size:9px; font-weight:700; letter-spacing:.16em; }.knowledge-hero h3 { margin:5px 0; font-size:22px; }.knowledge-hero p { margin:0; color:rgba(255,255,255,.72); font-size:12px; }.hero-actions { display:flex; align-items:center; gap:9px; }.hero-actions .el-button { color:#fff; background:rgba(255,255,255,.08); border-color:rgba(255,255,255,.28); }
.knowledge-stats { margin:14px 0; display:grid; grid-template-columns:repeat(3,1fr); gap:12px; }.knowledge-stats .el-card :deep(.el-card__body) { min-height:92px; display:grid; grid-template-columns:1fr auto; align-items:center; gap:2px 14px; }.knowledge-stats span,.knowledge-stats small { color:var(--muted-foreground); font-size:11px; }.knowledge-stats strong { grid-row:1/3; grid-column:2; color:#1554ad; font-size:28px; }.knowledge-stats small { grid-column:1; }
.knowledge-toolbar { margin-top:2px; }.upload-icon { color:#1677ff; font-size:38px; }.field-tip { display:block; margin-top:5px; color:#475569; font-size:10px; line-height:1.5; }.metadata-grid { display:grid; grid-template-columns:.8fr 1.2fr; gap:10px; }.document-name { display:flex; align-items:center; gap:9px; }.document-name>.el-icon { width:30px; height:30px; flex:none; color:#1677ff; background:#eaf3ff; border-radius:4px; }.document-name span { min-width:0; }.document-name b { display:block; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }.document-name small { display:block; margin-top:3px; color:#475569; font-size:9px; word-break:break-all; }
.knowledge-policy-card { margin-bottom:14px; }.knowledge-policy-card :deep(.el-alert) { margin-bottom:12px; }.knowledge-policy-grid { display:grid; grid-template-columns:minmax(0,1fr) minmax(0,1.35fr); gap:12px; }.knowledge-policy-grid :deep(.el-select),.knowledge-policy-grid :deep(.el-radio-group) { width:100%; }.knowledge-policy-grid :deep(.el-radio-button) { flex:1; }.knowledge-policy-grid :deep(.el-radio-button__inner) { width:100%; padding-left:8px; padding-right:8px; }.knowledge-policy-actions { display:flex; align-items:center; justify-content:space-between; gap:12px; margin-top:12px; }.knowledge-policy-actions small { color:#475569; font-size:10px; line-height:1.5; }
.detail-body { min-height:180px; }.chunk-policy { margin-top:14px; padding:14px; border:1px solid #dbeafe; border-radius:6px; background:#f8fbff; }.chunk-policy-title { display:flex; align-items:center; flex-wrap:wrap; gap:7px; }.chunk-policy-title strong { margin-right:auto; color:#16345f; }.chunk-policy-grid { display:grid; grid-template-columns:repeat(5,1fr); gap:8px; margin:12px 0 9px; }.chunk-policy-grid span { display:flex; flex-direction:column; gap:3px; color:#64748b; font-size:10px; }.chunk-policy-grid strong { color:#16345f; font-size:13px; }.chunk-policy>small { color:#475569; line-height:1.6; }.chunk-table { margin-top:14px; }.chunk-table :deep(.el-table__cell) { vertical-align:top; }.chunk-content { max-height:180px; overflow:auto; white-space:pre-wrap; word-break:break-word; color:#1e293b; line-height:1.6; font-size:12px; }.subline { display:block; margin-top:4px; color:#64748b; font-size:10px; word-break:break-all; }
@media (max-width:640px) { .knowledge-hero { align-items:flex-start; flex-direction:column; }.knowledge-hero p { line-height:1.6; }.hero-actions { width:100%; justify-content:space-between; }.knowledge-stats { grid-template-columns:1fr; }.documents-card { overflow:hidden; }.metadata-grid { grid-template-columns:1fr; }.knowledge-policy-grid { grid-template-columns:1fr; }.knowledge-policy-actions { align-items:flex-start; flex-direction:column; }.knowledge-policy-actions .el-button { width:100%; } }
:deep(.el-dialog__body) { overflow-x:hidden; }
:deep(.el-table .cell) { white-space:normal; }
:deep(.el-upload__text), :deep(.el-upload__tip) { color:#475569; }
</style>
