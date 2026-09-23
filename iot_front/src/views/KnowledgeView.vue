<script setup>
import {can} from '../permissions' /* 引入当前代码需要的依赖。 */
import { statusLabel } from '../presentation' /* 引入当前代码需要的依赖。 */
import { computed, onMounted, ref } from 'vue' /* 引入当前代码需要的依赖。 */
import { FileText, Upload } from '@lucide/vue' /* 引入当前代码需要的依赖。 */
import { UiMessage } from '../ui/feedback.js' /* 引入当前代码需要的依赖。 */

import { api, formatTime, notifyError } from '../api' /* 引入当前代码需要的依赖。 */

const emit = defineEmits(['navigate']) /* 声明 emit。 */
const uploadRef = ref(null) /* 声明 uploadRef。 */
const documents = ref([]) /* 声明 documents。 */
const agents = ref([]) /* 声明 agents。 */
const loading = ref(false) /* 声明 loading。 */
const documentsLoaded = ref(false) /* 声明 documentsLoaded。 */
const uploading = ref(false) /* 声明 uploading。 */
const uploadDialog = ref(false) /* 声明 uploadDialog。 */
const detailDialog = ref(false) /* 声明 detailDialog。 */
const selectedDocument = ref(null) /* 声明 selectedDocument。 */
const selectedDetail = ref(null) /* 声明 selectedDetail。 */
const detailLoading = ref(false) /* 声明 detailLoading。 */
const detailError = ref('') /* 声明 detailError。 */
const selectedFile = ref(null) /* 声明 selectedFile。 */
const workflowId = ref('') /* 声明 workflowId。 */
const category = ref('manual') /* 声明 category。 */
const tags = ref([]) /* 声明 tags。 */
const runtime = ref({ indexMode:'', persistentIndex:false }) /* 声明 runtime。 */
const agentError = ref('') /* 声明 agentError。 */
const bindingLoading = ref(false) /* 声明 bindingLoading。 */
const bindingSaving = ref(false) /* 声明 bindingSaving。 */
const bindingError = ref('') /* 声明 bindingError。 */
let bindingRequestId = 0 /* 声明 bindingRequestId。 */
const loadedBindingWorkflowId = ref('') /* 声明 loadedBindingWorkflowId。 */
const bindingWorkflowId = ref('') /* 声明 bindingWorkflowId。 */
const knowledgeBinding = ref({ retrievalMode:'auto', topK:5, minScore:0.25, noMatchPolicy:'allow-model' }) /* 声明 knowledgeBinding。 */
const page = ref(1) /* 声明 page。 */
const pageSize = ref(20) /* 声明 pageSize。 */
const total = ref(0) /* 声明 total。 */
const activeTab = ref('documents') /* 声明 activeTab。 */

const canUpload = computed(() => can('POST /api/v1/knowledge/documents')) /* 声明 canUpload。 */
const canManageBinding = computed(() => can('PUT /api/v1/ai/workflows/:id/knowledge-binding')) /* 声明 canManageBinding。 */
const indexedCount = computed(() => documents.value.filter(item => item.status === 'INDEXED').length) /* 声明 indexedCount。 */
const totalChunks = computed(() => documents.value.reduce((sum, item) => sum + Number(item.metadata?.chunks || 0), 0)) /* 声明 totalChunks。 */
const totalSize = computed(() => documents.value.reduce((sum, item) => sum + Number(item.metadata?.size || 0), 0)) /* 声明 totalSize。 */
const selectedBindingAgent = computed(() => agents.value.find(item => agentKey(item) === bindingWorkflowId.value)) /* 声明 selectedBindingAgent。 */

function agentKey(item) { return item?.id || item?.workflowId || '' } /* 定义 agentKey 函数。 */
function agentName(item) { return item?.name || item?.label || agentKey(item) || '未命名智能体' } /* 定义 agentName 函数。 */
function agentLabel(id) { /* 定义 agentLabel 函数。 */
  if (!id) return '未关联智能体' /* 判断条件并选择处理分支。 */
  const agent = agents.value.find(item => agentKey(item) === id) /* 声明 agent。 */
  return agent ? agentName(agent) : id /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
const categoryNames = { manual:'设备手册', 'alarm-sop':'告警处置操作规程', maintenance:'运维维修', regulation:'消防规范', faq:'常见问题' } /* 声明 categoryNames。 */
function categoryLabel(value) { return categoryNames[value] || value || '未分类' } /* 定义 categoryLabel 函数。 */
function formatBytes(value) { /* 定义 formatBytes 函数。 */
  const size = Number(value || 0) /* 声明 size。 */
  if (size < 1024) return `${size} 字节` /* 判断条件并选择处理分支。 */
  if (size < 1024 ** 2) return `${(size / 1024).toFixed(1)} 千字节` /* 判断条件并选择处理分支。 */
  return `${(size / 1024 ** 2).toFixed(1)} 兆字节` /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

async function load() { /* 定义 load 函数。 */
  loading.value = true /* 更新 loading.value 的值。 */
  agentError.value = '' /* 更新 agentError.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const [documentResult, agentResult] = await Promise.allSettled([ /* 执行当前语句并推进处理流程。 */
      api(`/api/v1/knowledge/documents?page=${page.value}&pageSize=${pageSize.value}`), /* 执行当前语句并推进处理流程。 */
      api('/api/v1/ai/workflows?page=1&pageSize=100') /* 执行当前语句并推进处理流程。 */
    ]) /* 结束当前表达式或代码块。 */
    if (documentResult.status === 'fulfilled') { /* 判断条件并选择处理分支。 */
      const data = documentResult.value /* 声明 data。 */
      documents.value = Array.isArray(data.items) ? data.items : [] /* 更新 documents.value 的值。 */
      total.value = Number(data.total ?? data.count ?? documents.value.length) /* 更新 total.value 的值。 */
      runtime.value = { indexMode:data.indexMode || '', persistentIndex:Boolean(data.persistentIndex) } /* 更新 runtime.value 的值。 */
      documentsLoaded.value = true /* 更新 documentsLoaded.value 的值。 */
    } else { /* 结束当前表达式或代码块。 */
      throw documentResult.reason /* 抛出当前错误。 */
    } /* 结束当前表达式或代码块。 */
    if (agentResult.status === 'fulfilled') agents.value = Array.isArray(agentResult.value.items) ? agentResult.value.items.filter(item => item.enabled !== false) : [] /* 判断条件并选择处理分支。 */
    else agentError.value = agentResult.reason?.message || '智能体列表读取失败' /* 执行当前语句并推进处理流程。 */
    if (!workflowId.value && agents.value.length) workflowId.value = agentKey(agents.value[0]) /* 判断条件并选择处理分支。 */
    if (agents.value.length && !agents.value.some(item => agentKey(item) === bindingWorkflowId.value)) bindingWorkflowId.value = agentKey(agents.value[0]) /* 判断条件并选择处理分支。 */
    if (bindingWorkflowId.value && loadedBindingWorkflowId.value !== bindingWorkflowId.value) void loadBinding() /* 判断条件并选择处理分支。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    if (error?.message?.includes('workflows')) agentError.value = error.message /* 判断条件并选择处理分支。 */
    else notifyError(error) /* 执行当前语句并推进处理流程。 */
  } finally { /* 结束当前表达式或代码块。 */
    loading.value = false /* 更新 loading.value 的值。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function loadBinding() { /* 定义 loadBinding 函数。 */
  if (!bindingWorkflowId.value) return /* 判断条件并选择处理分支。 */
  const workflow = bindingWorkflowId.value /* 声明 workflow。 */
  const requestId = ++bindingRequestId /* 声明 requestId。 */
  bindingLoading.value = true /* 更新 bindingLoading.value 的值。 */
  bindingError.value = '' /* 更新 bindingError.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const value = await api(`/api/v1/ai/workflows/${encodeURIComponent(workflow)}/knowledge-binding`) /* 声明 value。 */
    if (requestId !== bindingRequestId || bindingWorkflowId.value !== workflow) return /* 判断条件并选择处理分支。 */
    knowledgeBinding.value = { /* 更新 knowledgeBinding.value 的值。 */
      retrievalMode:value.retrievalMode || 'auto', /* 执行当前语句并推进处理流程。 */
      topK:Number(value.topK) || 5, /* 执行当前语句并推进处理流程。 */
      minScore:Number(value.minScore ?? 0.25), /* 执行当前语句并推进处理流程。 */
      noMatchPolicy:value.noMatchPolicy || 'allow-model' /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
    loadedBindingWorkflowId.value = workflow /* 更新 loadedBindingWorkflowId.value 的值。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    if (requestId === bindingRequestId) bindingError.value = error.message || '知识库策略读取失败' /* 判断条件并选择处理分支。 */
  } finally { /* 结束当前表达式或代码块。 */
    if (requestId === bindingRequestId) bindingLoading.value = false /* 判断条件并选择处理分支。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function saveBinding() { /* 定义 saveBinding 函数。 */
  if (!bindingWorkflowId.value || !canManageBinding.value) return /* 判断条件并选择处理分支。 */
  bindingSaving.value = true /* 更新 bindingSaving.value 的值。 */
  bindingError.value = '' /* 更新 bindingError.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const value = await api(`/api/v1/ai/workflows/${encodeURIComponent(bindingWorkflowId.value)}/knowledge-binding`, { method:'PUT', body:JSON.stringify(knowledgeBinding.value) }) /* 声明 value。 */
    knowledgeBinding.value = { ...knowledgeBinding.value, ...value } /* 更新 knowledgeBinding.value 的值。 */
    UiMessage.success(`已保存 ${agentName(selectedBindingAgent.value)} 的知识库策略`) /* 执行当前语句并推进处理流程。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    bindingError.value = error.message || '知识库策略保存失败' /* 更新 bindingError.value 的值。 */
  } finally { /* 结束当前表达式或代码块。 */
    bindingSaving.value = false /* 更新 bindingSaving.value 的值。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function changePage(value) { page.value = value; load() } /* 定义 changePage 函数。 */
function changePageSize(value) { pageSize.value = value; page.value = 1; load() } /* 定义 changePageSize 函数。 */
function openUpload() { /* 定义 openUpload 函数。 */
  if (!canUpload.value) return /* 判断条件并选择处理分支。 */
  uploadDialog.value = true /* 更新 uploadDialog.value 的值。 */
} /* 结束当前表达式或代码块。 */
function chooseFile(file) { /* 定义 chooseFile 函数。 */
  if (Number(file.size || file.raw?.size || 0) > 32 * 1024 * 1024) { /* 判断条件并选择处理分支。 */
    selectedFile.value = null /* 更新 selectedFile.value 的值。 */
    uploadRef.value?.clearFiles() /* 执行当前语句并推进处理流程。 */
    UiMessage.error('知识库文件不能超过 32 兆字节') /* 执行当前语句并推进处理流程。 */
    return /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
  selectedFile.value = file.raw || null /* 更新 selectedFile.value 的值。 */
} /* 结束当前表达式或代码块。 */
function removeFile() { selectedFile.value = null } /* 定义 removeFile 函数。 */
function rejectExtra() { UiMessage.warning('每次只能上传一个知识库文件') } /* 定义 rejectExtra 函数。 */
async function showDocument(document) { /* 定义 showDocument 函数。 */
  selectedDocument.value = document /* 更新 selectedDocument.value 的值。 */
  selectedDetail.value = null /* 更新 selectedDetail.value 的值。 */
  detailError.value = '' /* 更新 detailError.value 的值。 */
  detailDialog.value = true /* 更新 detailDialog.value 的值。 */
  detailLoading.value = true /* 更新 detailLoading.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const detail = await api(`/api/v1/knowledge/documents/${encodeURIComponent(document.id)}`) /* 声明 detail。 */
    selectedDocument.value = detail.document || document /* 更新 selectedDocument.value 的值。 */
    selectedDetail.value = detail /* 更新 selectedDetail.value 的值。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    detailError.value = error.message || '知识切片详情读取失败' /* 更新 detailError.value 的值。 */
  } finally { /* 结束当前表达式或代码块。 */
    detailLoading.value = false /* 更新 detailLoading.value 的值。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function upload() { /* 定义 upload 函数。 */
  if (!workflowId.value) return UiMessage.warning('请选择要关联的智能体') /* 判断条件并选择处理分支。 */
  if (!selectedFile.value) return UiMessage.warning('请先选择知识库文件') /* 判断条件并选择处理分支。 */
  uploading.value = true /* 更新 uploading.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const form = new FormData() /* 声明 form。 */
    form.append('file', selectedFile.value) /* 执行当前语句并推进处理流程。 */
    form.append('workflowId', workflowId.value) /* 执行当前语句并推进处理流程。 */
    if (category.value.trim()) form.append('category', category.value.trim()) /* 判断条件并选择处理分支。 */
    if (tags.value.length) form.append('tags', tags.value.join(',')) /* 判断条件并选择处理分支。 */
    const created = await api('/api/v1/knowledge/documents', { method:'POST', body:form }) /* 声明 created。 */
    UiMessage.success(`知识库已索引并绑定到 ${agentLabel(created.workflowId)}`) /* 执行当前语句并推进处理流程。 */
    selectedFile.value = null /* 更新 selectedFile.value 的值。 */
    category.value = 'manual' /* 更新 category.value 的值。 */
    tags.value = [] /* 更新 tags.value 的值。 */
    uploadRef.value?.clearFiles() /* 执行当前语句并推进处理流程。 */
    uploadDialog.value = false /* 更新 uploadDialog.value 的值。 */
    activeTab.value = 'documents' /* 更新 activeTab.value 的值。 */
    await load() /* 等待异步操作完成。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    notifyError(error) /* 执行当前语句并推进处理流程。 */
  } finally { /* 结束当前表达式或代码块。 */
    uploading.value = false /* 更新 uploading.value 的值。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

onMounted(load) /* 执行当前语句并推进处理流程。 */
</script>

<template>
  <div class="knowledge-page"> <!-- 渲染 div 界面元素。 -->
    <header class="knowledge-intro"> <!-- 渲染 header 界面元素。 -->
      <div class="knowledge-intro-copy"> <!-- 渲染 div 界面元素。 -->
        <span class="knowledge-kicker">智能体知识</span> <!-- 渲染 span 界面元素。 -->
        <p>上传设备手册与处置规范，按智能体管理文档和检索方式。</p> <!-- 渲染 p 界面元素。 -->
      </div> <!-- 结束当前界面区域。 -->
      <div class="knowledge-intro-actions"> <!-- 渲染 div 界面元素。 -->
        <ui-button v-permission="'menu:ai'" @click="emit('navigate', 'ai')">打开智能助手</ui-button> <!-- 渲染 ui-button 界面元素。 -->
        <ui-button v-permission="'POST /api/v1/knowledge/documents'" type="primary" :disabled="!canUpload" @click="openUpload"><Upload :size="16" />上传知识文档</ui-button> <!-- 渲染 ui-button 界面元素。 -->
      </div> <!-- 结束当前界面区域。 -->
    </header> <!-- 结束当前界面区域。 -->

    <ui-alert v-if="agentError" :title="agentError" type="warning" :closable="false" show-icon /> <!-- 渲染 ui-alert 界面元素。 -->
    <ui-alert v-if="documentsLoaded && !runtime.persistentIndex" title="当前使用内存索引，服务重启后需要重新建立文档检索索引。" type="warning" :closable="false" show-icon /> <!-- 渲染 ui-alert 界面元素。 -->

    <section class="knowledge-stats" aria-label="知识库概况"> <!-- 渲染 section 界面元素。 -->
      <div><span>知识文档</span><strong>{{ documentsLoaded ? total : '—' }}</strong><small>当前租户全部文档</small></div> <!-- 渲染 div 界面元素。 -->
      <div><span>本页已索引</span><strong>{{ documentsLoaded ? indexedCount : '—' }}</strong><small>当前页可供检索</small></div> <!-- 渲染 div 界面元素。 -->
      <div><span>本页内容分片</span><strong>{{ documentsLoaded ? totalChunks : '—' }}</strong><small>{{ documentsLoaded ? formatBytes(totalSize) : '等待读取' }}</small></div> <!-- 渲染 div 界面元素。 -->
      <div class="knowledge-index-state"><span>索引存储</span><strong>{{ documentsLoaded ? (runtime.persistentIndex ? '持久化' : '内存') : '读取中' }}</strong><small>{{ runtime.indexMode || '索引模式未返回' }}</small></div> <!-- 渲染 div 界面元素。 -->
    </section> <!-- 结束当前界面区域。 -->

    <ui-tabs v-model="activeTab" class="knowledge-tabs"> <!-- 渲染 ui-tabs 界面元素。 -->
      <ui-tab-pane name="documents" label="文档"> <!-- 渲染 ui-tab-pane 界面元素。 -->
        <section class="knowledge-panel documents-panel" aria-label="已上传文档"> <!-- 渲染 section 界面元素。 -->
          <div class="knowledge-panel-heading"> <!-- 渲染 div 界面元素。 -->
            <div><h2>已上传文档</h2><p>查看文档的归属、索引状态和内容切片。</p></div> <!-- 渲染 div 界面元素。 -->
            <ui-button :loading="loading" @click="load">刷新列表</ui-button> <!-- 渲染 ui-button 界面元素。 -->
          </div> <!-- 结束当前界面区域。 -->

          <ui-table v-loading="loading" :data="documents" class="knowledge-table"> <!-- 渲染 ui-table 界面元素。 -->
            <ui-table-column label="文档" min-width="270"><template #default="{ row }"><div class="document-name"><FileText class="document-icon" /><div><strong>{{ row.filename }}</strong><small>{{ categoryLabel(row.category) }} · {{ formatBytes(row.metadata?.size) }}</small></div></div></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
            <ui-table-column label="关联智能体" min-width="175"><template #default="{ row }">{{ agentLabel(row.workflowId) }}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
            <ui-table-column label="索引状态" width="110"><template #default="{ row }"><ui-tag :type="row.status === 'INDEXED' ? 'success' : 'warning'" effect="light">{{ statusLabel(row.status) }}</ui-tag></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
            <ui-table-column label="内容分片" width="100" align="right"><template #default="{ row }">{{ row.metadata?.chunks || 0 }}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
            <ui-table-column label="上传时间" min-width="165"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
            <ui-table-column label="操作" width="96" align="right"><template #default="{ row }"><ui-button plain type="primary" @click="showDocument(row)">查看详情</ui-button></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
            <template #empty><ui-empty description="还没有知识文档" /></template>
          </ui-table> <!-- 结束当前界面区域。 -->

          <div v-loading="loading" class="knowledge-mobile-list"> <!-- 渲染 div 界面元素。 -->
            <article v-for="row in documents" :key="row.id" class="knowledge-mobile-document"> <!-- 渲染 article 界面元素。 -->
              <div class="knowledge-mobile-document-head"><FileText class="document-icon" /><div><strong>{{ row.filename }}</strong><small>{{ categoryLabel(row.category) }} · {{ formatBytes(row.metadata?.size) }}</small></div></div> <!-- 渲染 div 界面元素。 -->
              <div class="knowledge-mobile-document-meta"><span>{{ agentLabel(row.workflowId) }}</span><ui-tag :type="row.status === 'INDEXED' ? 'success' : 'warning'" effect="light">{{ statusLabel(row.status) }}</ui-tag></div> <!-- 渲染 div 界面元素。 -->
              <div class="knowledge-mobile-document-foot"><small>{{ row.metadata?.chunks || 0 }} 个分片 · {{ formatTime(row.createdAt) }}</small><ui-button plain type="primary" @click="showDocument(row)">查看详情</ui-button></div> <!-- 渲染 div 界面元素。 -->
            </article> <!-- 结束当前界面区域。 -->
            <ui-empty v-if="!loading && !documents.length" description="还没有知识文档" /> <!-- 渲染 ui-empty 界面元素。 -->
          </div> <!-- 结束当前界面区域。 -->

          <div class="list-pagination knowledge-pagination"><ui-pagination v-model:current-page="page" v-model:page-size="pageSize" :total="total" :page-sizes="[20, 50, 100]" layout="total, sizes, prev, pager, next" hide-on-single-page @current-change="changePage" @size-change="changePageSize" /></div> <!-- 渲染 div 界面元素。 -->
        </section> <!-- 结束当前界面区域。 -->
      </ui-tab-pane> <!-- 结束当前界面区域。 -->

      <ui-tab-pane name="policy" label="检索策略"> <!-- 渲染 ui-tab-pane 界面元素。 -->
        <section class="knowledge-panel policy-panel" aria-label="知识库策略"> <!-- 渲染 section 界面元素。 -->
          <div class="knowledge-panel-heading"> <!-- 渲染 div 界面元素。 -->
            <div><h2>知识库策略</h2><p>每个智能体只检索属于自己的文档，按需调整回答时的检索规则。</p></div> <!-- 渲染 div 界面元素。 -->
            <ui-button v-if="agents.length" :loading="bindingLoading" @click="loadBinding">刷新策略</ui-button> <!-- 渲染 ui-button 界面元素。 -->
          </div> <!-- 结束当前界面区域。 -->
          <ui-empty v-if="!agents.length" description="暂无可配置的智能体；上传文档时仍可输入智能体标识。" /> <!-- 渲染 ui-empty 界面元素。 -->
          <template v-else>
            <ui-alert v-if="bindingError" :title="bindingError" type="warning" :closable="false" show-icon /> <!-- 渲染 ui-alert 界面元素。 -->
            <div class="knowledge-policy-target"> <!-- 渲染 div 界面元素。 -->
              <label for="knowledge-agent">当前智能体</label> <!-- 渲染 label 界面元素。 -->
              <ui-select id="knowledge-agent" v-model="bindingWorkflowId" filterable :disabled="bindingSaving" placeholder="选择智能体" @change="loadBinding"><ui-option v-for="agent in agents" :key="agentKey(agent)" :label="agentName(agent) + ' · ' + agentKey(agent)" :value="agentKey(agent)" /></ui-select> <!-- 渲染 ui-select 界面元素。 -->
              <small>本页 {{ documents.filter(item => item.workflowId === bindingWorkflowId).length }} 份文档归属该智能体</small> <!-- 渲染 small 界面元素。 -->
            </div> <!-- 结束当前界面区域。 -->
            <ui-form v-loading="bindingLoading" class="knowledge-policy-form" label-position="top" :model="knowledgeBinding" :disabled="!canManageBinding || bindingLoading || bindingSaving"> <!-- 渲染 ui-form 界面元素。 -->
              <div class="knowledge-policy-section"> <!-- 渲染 div 界面元素。 -->
                <div class="knowledge-section-copy"><h3>何时检索</h3><p>决定智能体在回答前是否查询知识文档。</p></div> <!-- 渲染 div 界面元素。 -->
                <ui-form-item label="检索模式"><ui-radio-group v-model="knowledgeBinding.retrievalMode" class="segmented-choice-group" aria-label="检索模式"><ui-radio-button value="auto">按需检索</ui-radio-button><ui-radio-button value="always">每次强制检索</ui-radio-button><ui-radio-button value="disabled">禁用</ui-radio-button></ui-radio-group></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
              </div> <!-- 结束当前界面区域。 -->
              <div class="knowledge-policy-section"> <!-- 渲染 div 界面元素。 -->
                <div class="knowledge-section-copy"><h3>匹配要求</h3><p>控制取回的片段数量，以及内容的最低相似度。</p></div> <!-- 渲染 div 界面元素。 -->
                <div class="knowledge-number-grid"><ui-form-item label="召回数量"><ui-input-number v-model="knowledgeBinding.topK" :min="1" :max="20" controls-position="right" /></ui-form-item><ui-form-item label="最低相似度"><ui-input-number v-model="knowledgeBinding.minScore" :min="0" :max="1" :step="0.05" :precision="2" controls-position="right" /></ui-form-item></div> <!-- 渲染 div 界面元素。 -->
              </div> <!-- 结束当前界面区域。 -->
              <div class="knowledge-policy-section"> <!-- 渲染 div 界面元素。 -->
                <div class="knowledge-section-copy"><h3>没有匹配时</h3><p>明确缺少依据时，智能体是否还可以给出一般性回答。</p></div> <!-- 渲染 div 界面元素。 -->
                <ui-form-item label="无匹配知识时"><ui-select v-model="knowledgeBinding.noMatchPolicy"><ui-option label="允许模型回答，但必须说明证据不足" value="allow-model" /><ui-option label="阻止回答，必须先补充知识" value="require-evidence" /></ui-select></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
              </div> <!-- 结束当前界面区域。 -->
              <div class="knowledge-policy-actions"><small v-if="!canManageBinding">当前账号可查看策略；修改需要管理员或运维人员权限。</small><ui-button v-permission="'PUT /api/v1/ai/workflows/:id/knowledge-binding'" type="primary" :loading="bindingSaving" :disabled="!canManageBinding || !bindingWorkflowId" @click="saveBinding">保存知识库策略</ui-button></div> <!-- 渲染 div 界面元素。 -->
            </ui-form> <!-- 结束当前界面区域。 -->
          </template>
        </section>
      </ui-tab-pane>
    </ui-tabs>

    <ui-dialog v-model="uploadDialog" title="上传知识文档并绑定智能体" width="min(680px, 94vw)" class="knowledge-upload-dialog">
      <section class="knowledge-upload-section">
        <div class="knowledge-upload-step"><span>1</span><div><strong>选择文档</strong><small>每次上传一个文件，最大 32 兆字节</small></div></div>
        <ui-upload ref="uploadRef" drag :auto-upload="false" :disabled="!canUpload || uploading" :limit="1" accept=".pdf,.docx,.pptx,.xlsx,.odt,.odp,.ods,.txt,.md,.csv,.json,.html,.htm,.xml" :on-change="chooseFile" :on-remove="removeFile" :on-exceed="rejectExtra"><Upload class="upload-icon" /><div class="el-upload__text">拖放文件到这里，或<em>点击选择</em></div><template #tip><div class="el-upload__tip">支持 PDF、办公文档、网页和文本；扫描件需先进行文字识别。</div></template></ui-upload>
      </section>
      <section class="knowledge-upload-section">
        <div class="knowledge-upload-step"><span>2</span><div><strong>指定归属</strong><small>文档只供所选智能体检索</small></div></div>
        <ui-form label-position="top" class="knowledge-upload-form">
          <ui-form-item label="关联智能体（必选）"><ui-select v-model="workflowId" filterable allow-create default-first-option :disabled="uploading" placeholder="选择智能体，或输入标识后按回车"><ui-option v-for="agent in agents" :key="agentKey(agent)" :label="agentName(agent) + ' · ' + agentKey(agent)" :value="agentKey(agent)" /></ui-select></ui-form-item>
          <p class="field-tip">列表中没有目标智能体时，可输入计划使用的智能体标识并按回车创建。</p>
          <div class="metadata-grid"><ui-form-item label="知识分类（可选）"><ui-select v-model="category" :disabled="uploading"><ui-option label="设备手册" value="manual" /><ui-option label="告警处置操作规程" value="alarm-sop" /><ui-option label="运维维修" value="maintenance" /><ui-option label="消防规范" value="regulation" /><ui-option label="常见问题" value="faq" /></ui-select></ui-form-item><ui-form-item label="知识标签（可选）"><ui-select v-model="tags" multiple filterable allow-create default-first-option :disabled="uploading" placeholder="输入标签后按回车" /></ui-form-item></div>
        </ui-form>
      </section>
      <template #footer><ui-button @click="uploadDialog=false">取消</ui-button><ui-button v-permission="'POST /api/v1/knowledge/documents'" type="primary" :loading="uploading" :disabled="!canUpload || !selectedFile || !workflowId" @click="upload">上传并建立索引</ui-button></template>
    </ui-dialog> <!-- 结束当前界面区域。 -->

    <ui-dialog v-model="detailDialog" title="知识文档详情与切片" width="min(900px, 96vw)"> <!-- 渲染 ui-dialog 界面元素。 -->
      <ui-alert v-if="detailError" :title="detailError" type="error" :closable="false" show-icon /> <!-- 渲染 ui-alert 界面元素。 -->
      <div v-loading="detailLoading" class="knowledge-detail"> <!-- 渲染 div 界面元素。 -->
        <div v-if="selectedDocument" class="knowledge-detail-file"><FileText class="document-icon" /><div><strong>{{ selectedDocument.filename }}</strong><small>{{ selectedDocument.id }}</small></div><ui-tag :type="selectedDocument.status === 'INDEXED' ? 'success' : 'warning'">{{ statusLabel(selectedDocument.status) }}</ui-tag></div> <!-- 渲染 div 界面元素。 -->
        <dl v-if="selectedDocument" class="knowledge-detail-meta"><div><dt>关联智能体</dt><dd>{{ agentLabel(selectedDocument.workflowId) }}</dd></div><div><dt>知识分类</dt><dd>{{ categoryLabel(selectedDocument.category) }}</dd></div><div><dt>内容统计</dt><dd>{{ selectedDocument.metadata?.chunks || 0 }} 个分片 · {{ formatBytes(selectedDocument.metadata?.size) }}</dd></div><div><dt>上传时间</dt><dd>{{ formatTime(selectedDocument.createdAt) }}</dd></div><div v-if="selectedDocument.tags?.length"><dt>知识标签</dt><dd>{{ selectedDocument.tags.join('、') }}</dd></div></dl> <!-- 渲染 dl 界面元素。 -->
        <section v-if="selectedDetail?.index" class="knowledge-index-rules"><div class="knowledge-detail-section-heading"><h3>索引与切片规则</h3><span>{{ selectedDetail.index.mode }} · {{ selectedDetail.index.vectorizer }}</span></div><div class="knowledge-rule-grid"><div><small>切片策略</small><strong>{{ selectedDetail.index.chunking?.strategy === 'fixed-window-overlap' ? '固定窗口 + 重叠' : selectedDetail.index.chunking?.strategy }}</strong></div><div><small>窗口 / 重叠</small><strong>{{ selectedDetail.index.chunking?.size }} / {{ selectedDetail.index.chunking?.overlap }} 字符</strong></div><div><small>提取文本</small><strong>{{ selectedDetail.index.extractedChars || 0 }} 字符</strong></div><div><small>实际分片</small><strong>{{ selectedDetail.index.chunkCount }}</strong></div></div><p v-if="selectedDetail.index.embeddingModel">向量模型：{{ selectedDetail.index.embeddingModel }}</p></section> <!-- 渲染 section 界面元素。 -->
        <section v-if="selectedDetail" class="knowledge-chunks"><div class="knowledge-detail-section-heading"><h3>切片内容</h3><span>{{ selectedDetail.chunks?.length || 0 }} 个分片，点击逐条查看</span></div><div v-if="selectedDetail.chunks?.length" class="knowledge-chunk-list"><details v-for="(row, index) in selectedDetail.chunks" :key="row.chunkId || index" :open="index === 0" class="knowledge-chunk"><summary><span class="knowledge-chunk-number">{{ index + 1 }}</span><span>字符范围 [{{ row.startChar }}, {{ row.endChar }})</span><ui-tag :type="row.vectorized ? 'success' : 'info'" size="small">{{ row.vectorized ? '向量化完成' : '非向量索引' }}</ui-tag></summary><div class="knowledge-chunk-body"><p>{{ row.content }}</p><small>重叠 {{ row.overlapChars || 0 }} 字符 · {{ row.characterCount }} 字符 · {{ row.chunkId }}</small></div></details></div><ui-empty v-else description="索引中没有可查看的切片" :image-size="56" /></section> <!-- 渲染 section 界面元素。 -->
      </div> <!-- 结束当前界面区域。 -->
      <template #footer><ui-button @click="detailDialog=false">关闭</ui-button></template>
    </ui-dialog> <!-- 结束当前界面区域。 -->
  </div> <!-- 结束当前界面区域。 -->
</template>

<style scoped>
.knowledge-page { display:grid; gap:18px; min-width:0; padding-bottom:24px; } /* 定义当前元素的样式规则。 */
.knowledge-intro { display:flex; align-items:center; justify-content:space-between; gap:20px; padding:4px 0 2px; } /* 定义当前元素的样式规则。 */
.knowledge-kicker { color:var(--brand-navy); font-size:14px; font-weight:700; } /* 定义当前元素的样式规则。 */
.knowledge-intro-copy p { max-width:650px; margin:5px 0 0; color:var(--muted-foreground); font-size:14px; } /* 定义当前元素的样式规则。 */
.knowledge-intro-actions { display:flex; gap:8px; flex:none; } /* 定义当前元素的样式规则。 */
.knowledge-intro-actions .el-button { margin:0; } /* 定义当前元素的样式规则。 */
.knowledge-intro-actions svg { width:16px; height:16px; } /* 定义当前元素的样式规则。 */
.knowledge-stats { display:grid; grid-template-columns:repeat(4,minmax(0,1fr)); gap:1px; overflow:hidden; border-radius:var(--radius); background:#e9edf3; } /* 定义当前元素的样式规则。 */
.knowledge-stats > div { min-width:0; min-height:104px; padding:16px 18px; display:grid; align-content:space-between; background:#fff; } /* 定义当前元素的样式规则。 */
.knowledge-stats span,.knowledge-stats small { color:var(--muted-foreground); font-size:12px; } /* 定义当前元素的样式规则。 */
.knowledge-stats strong { color:var(--foreground); font-size:26px; line-height:1.15; font-weight:650; font-variant-numeric:tabular-nums; } /* 定义当前元素的样式规则。 */
.knowledge-stats .knowledge-index-state strong { color:var(--brand-navy); font-size:18px; } /* 定义当前元素的样式规则。 */
.knowledge-tabs { min-width:0; } /* 定义当前元素的样式规则。 */
.knowledge-tabs :deep(.el-tabs__header) { margin-bottom:14px; } /* 定义当前元素的样式规则。 */
.knowledge-tabs :deep(.el-tabs__item) { height:42px; padding:0 22px; font-size:14px; } /* 定义当前元素的样式规则。 */
.knowledge-tabs :deep(.el-tabs__active-bar) { height:3px; border-radius:3px; } /* 定义当前元素的样式规则。 */
.knowledge-panel { min-width:0; overflow:hidden; background:#fff; border-radius:var(--radius); box-shadow:0 1px 2px rgba(19,56,108,.045); } /* 定义当前元素的样式规则。 */
.knowledge-panel-heading { display:flex; align-items:center; justify-content:space-between; gap:16px; padding:22px 24px 14px; } /* 定义当前元素的样式规则。 */
.knowledge-panel-heading h2 { margin:0; font-size:18px; line-height:1.3; } /* 定义当前元素的样式规则。 */
.knowledge-panel-heading p { margin:5px 0 0; color:var(--muted-foreground); font-size:13px; } /* 定义当前元素的样式规则。 */
.knowledge-table { width:100%; } /* 定义当前元素的样式规则。 */
.knowledge-table :deep(.el-table__header th) { background:#fff; } /* 定义当前元素的样式规则。 */
.knowledge-table :deep(.el-table__cell) { padding:12px 0; } /* 定义当前元素的样式规则。 */
.document-name { min-width:0; display:flex; align-items:center; gap:12px; } /* 定义当前元素的样式规则。 */
.document-icon { width:36px; height:36px; flex:none; padding:9px; color:var(--brand-navy); background:#eef4fc; border-radius:10px; } /* 定义当前元素的样式规则。 */
.document-name > div { min-width:0; display:grid; gap:3px; } /* 定义当前元素的样式规则。 */
.document-name strong { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; font-size:14px; } /* 定义当前元素的样式规则。 */
.document-name small { color:var(--muted-foreground); font-size:12px; } /* 定义当前元素的样式规则。 */
.knowledge-pagination { border-top:1px solid #f0f2f5; } /* 定义当前元素的样式规则。 */
.knowledge-mobile-list { display:none; } /* 定义当前元素的样式规则。 */
.policy-panel { padding-bottom:8px; } /* 定义当前元素的样式规则。 */
.knowledge-policy-target { margin:0 24px; padding:16px 18px; display:grid; grid-template-columns:130px minmax(0,360px) 1fr; align-items:center; gap:14px; background:#f5f7fa; border-radius:10px; } /* 定义当前元素的样式规则。 */
.knowledge-policy-target label { font-size:13px; font-weight:600; } /* 定义当前元素的样式规则。 */
.knowledge-policy-target small { color:var(--muted-foreground); font-size:12px; } /* 定义当前元素的样式规则。 */
.knowledge-policy-form { padding:6px 24px 16px; } /* 定义当前元素的样式规则。 */
.knowledge-policy-section { padding:20px 0; display:grid; grid-template-columns:minmax(170px,.8fr) minmax(0,1.2fr); align-items:start; gap:24px; border-bottom:1px solid #eef0f3; } /* 定义当前元素的样式规则。 */
.knowledge-section-copy h3 { margin:0; font-size:15px; } /* 定义当前元素的样式规则。 */
.knowledge-section-copy p { max-width:280px; margin:5px 0 0; color:var(--muted-foreground); font-size:12px; line-height:1.6; } /* 定义当前元素的样式规则。 */
.knowledge-policy-section .el-form-item { width:100%; max-width:430px; margin:0; } /* 定义当前元素的样式规则。 */
.knowledge-policy-section :deep(.el-radio-group),.knowledge-policy-section :deep(.el-select) { width:100%; } /* 定义当前元素的样式规则。 */
.knowledge-policy-section :deep(.el-radio-button) { flex:1; } /* 定义当前元素的样式规则。 */
.knowledge-policy-section :deep(.el-radio-button__inner) { width:100%; padding-inline:8px; } /* 定义当前元素的样式规则。 */
.knowledge-number-grid { display:grid; grid-template-columns:1fr 1fr; gap:16px; max-width:430px; } /* 定义当前元素的样式规则。 */
.knowledge-number-grid :deep(.el-input-number) { width:100%; } /* 定义当前元素的样式规则。 */
.knowledge-policy-actions { display:flex; align-items:center; justify-content:flex-end; gap:16px; padding-top:20px; } /* 定义当前元素的样式规则。 */
.knowledge-policy-actions small { margin-right:auto; color:var(--muted-foreground); font-size:12px; } /* 定义当前元素的样式规则。 */
.knowledge-upload-section { min-width:0; padding:0 0 20px; } /* 上传步骤各占独立区域，说明不与输入框挤在同一行。 */
.knowledge-upload-section + .knowledge-upload-section { padding-top:20px; border-top:1px solid var(--border); }
.knowledge-upload-section:last-of-type { padding-bottom:0; }
.knowledge-upload-step { display:flex; align-items:center; gap:10px; margin-bottom:14px; } /* 定义当前元素的样式规则。 */
.knowledge-upload-step > span { width:25px; height:25px; flex:none; display:grid; place-items:center; color:#fff; background:var(--brand-navy); border-radius:50%; font-size:12px; font-weight:700; } /* 定义当前元素的样式规则。 */
.knowledge-upload-step > div { display:flex; align-items:baseline; gap:9px; } /* 定义当前元素的样式规则。 */
.knowledge-upload-step strong { font-size:14px; } /* 定义当前元素的样式规则。 */
.knowledge-upload-step small,.field-tip { color:var(--muted-foreground); font-size:12px; line-height:1.5; } /* 定义当前元素的样式规则。 */
.upload-icon { width:32px; height:32px; color:var(--primary); } /* 定义当前元素的样式规则。 */
.knowledge-upload-form :deep(.n-form-item),.knowledge-upload-form :deep(.n-select) { width:100%; min-width:0; } /* 表单字段与选择器占据整行。 */
.knowledge-upload-form .field-tip { display:block; margin:-3px 0 16px; } /* 说明独立换行，避免覆盖选择器。 */
.metadata-grid { display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:16px; } /* 定义当前元素的样式规则。 */
.metadata-grid > * { min-width:0; }
.knowledge-detail { min-height:140px; } /* 定义当前元素的样式规则。 */
.knowledge-detail-file { display:flex; align-items:center; gap:12px; padding:3px 0 18px; } /* 定义当前元素的样式规则。 */
.knowledge-detail-file > div { min-width:0; flex:1; display:grid; gap:4px; } /* 定义当前元素的样式规则。 */
.knowledge-detail-file strong { overflow:hidden; font-size:16px; text-overflow:ellipsis; white-space:nowrap; } /* 定义当前元素的样式规则。 */
.knowledge-detail-file small { color:var(--muted-foreground); font-size:12px; overflow-wrap:anywhere; } /* 定义当前元素的样式规则。 */
.knowledge-detail-meta { margin:0; padding:16px 18px; display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:16px; background:#f5f7fa; border-radius:10px; } /* 定义当前元素的样式规则。 */
.knowledge-detail-meta div { min-width:0; } /* 定义当前元素的样式规则。 */
.knowledge-detail-meta dt { color:var(--muted-foreground); font-size:12px; } /* 定义当前元素的样式规则。 */
.knowledge-detail-meta dd { margin:5px 0 0; font-size:13px; overflow-wrap:anywhere; } /* 定义当前元素的样式规则。 */
.knowledge-index-rules,.knowledge-chunks { margin-top:24px; } /* 定义当前元素的样式规则。 */
.knowledge-detail-section-heading { display:flex; align-items:baseline; justify-content:space-between; gap:12px; margin-bottom:12px; } /* 定义当前元素的样式规则。 */
.knowledge-detail-section-heading h3 { margin:0; font-size:15px; } /* 定义当前元素的样式规则。 */
.knowledge-detail-section-heading span { color:var(--muted-foreground); font-size:12px; } /* 定义当前元素的样式规则。 */
.knowledge-rule-grid { display:grid; grid-template-columns:repeat(4,minmax(0,1fr)); gap:8px; } /* 定义当前元素的样式规则。 */
.knowledge-rule-grid > div { min-width:0; padding:12px; display:grid; gap:5px; background:#f5f7fa; border-radius:8px; } /* 定义当前元素的样式规则。 */
.knowledge-rule-grid small { color:var(--muted-foreground); font-size:12px; } /* 定义当前元素的样式规则。 */
.knowledge-rule-grid strong { font-size:13px; overflow-wrap:anywhere; } /* 定义当前元素的样式规则。 */
.knowledge-index-rules p { margin:10px 0 0; color:var(--muted-foreground); font-size:12px; } /* 定义当前元素的样式规则。 */
.knowledge-chunk-list { display:grid; gap:8px; } /* 定义当前元素的样式规则。 */
.knowledge-chunk { border:1px solid #e9edf3; border-radius:9px; } /* 定义当前元素的样式规则。 */
.knowledge-chunk summary { min-height:52px; padding:10px 14px; display:flex; align-items:center; gap:12px; cursor:pointer; list-style:none; font-size:13px; } /* 定义当前元素的样式规则。 */
.knowledge-chunk summary::-webkit-details-marker { display:none; } /* 定义当前元素的样式规则。 */
.knowledge-chunk summary .el-tag { margin-left:auto; } /* 定义当前元素的样式规则。 */
.knowledge-chunk-number { width:26px; height:26px; flex:none; display:grid; place-items:center; color:var(--brand-navy); background:#eef4fc; border-radius:7px; font-size:12px; font-weight:700; } /* 定义当前元素的样式规则。 */
.knowledge-chunk-body { padding:0 14px 14px 52px; } /* 定义当前元素的样式规则。 */
.knowledge-chunk-body p { max-height:220px; margin:0; padding:12px; overflow:auto; white-space:pre-wrap; overflow-wrap:anywhere; background:#f5f7fa; border-radius:7px; font-size:13px; line-height:1.7; } /* 定义当前元素的样式规则。 */
.knowledge-chunk-body small { display:block; margin-top:7px; color:var(--muted-foreground); font-size:12px; overflow-wrap:anywhere; } /* 定义当前元素的样式规则。 */
:deep(.el-dialog__body) { overflow-x:hidden; } /* 设置  样式。 */
:deep(.el-upload__text),:deep(.el-upload__tip) { color:#475569; } /* 设置  样式。 */
@media (max-width:900px) { .knowledge-stats { grid-template-columns:repeat(2,minmax(0,1fr)); }.knowledge-policy-target { grid-template-columns:120px minmax(0,1fr); }.knowledge-policy-target small { grid-column:2; } } /* 按屏幕条件调整样式。 */
@media (max-width:640px) { /* 按屏幕条件调整样式。 */
  .knowledge-page { gap:14px; } /* 定义当前元素的样式规则。 */
  .knowledge-intro { align-items:flex-start; flex-direction:column; gap:14px; } /* 定义当前元素的样式规则。 */
  .knowledge-intro-actions { width:100%; } /* 定义当前元素的样式规则。 */
  .knowledge-intro-actions .el-button { flex:1; padding-inline:8px; } /* 定义当前元素的样式规则。 */
  .knowledge-stats > div { min-height:88px; padding:12px; } /* 定义当前元素的样式规则。 */
  .knowledge-stats strong { font-size:22px; } /* 定义当前元素的样式规则。 */
  .knowledge-stats .knowledge-index-state strong { font-size:16px; } /* 定义当前元素的样式规则。 */
  .knowledge-panel-heading { padding:18px 16px 10px; } /* 定义当前元素的样式规则。 */
  .knowledge-panel-heading p { display:none; } /* 定义当前元素的样式规则。 */
  .knowledge-table { display:none; } /* 定义当前元素的样式规则。 */
  .knowledge-mobile-list { min-height:90px; padding:0 16px 12px; display:grid; gap:9px; } /* 定义当前元素的样式规则。 */
  .knowledge-mobile-document { padding:14px; display:grid; gap:12px; background:#f5f7fa; border-radius:10px; } /* 定义当前元素的样式规则。 */
  .knowledge-mobile-document-head { min-width:0; display:flex; align-items:center; gap:10px; } /* 定义当前元素的样式规则。 */
  .knowledge-mobile-document-head > div { min-width:0; display:grid; gap:3px; } /* 定义当前元素的样式规则。 */
  .knowledge-mobile-document-head strong { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; font-size:14px; } /* 定义当前元素的样式规则。 */
  .knowledge-mobile-document-head small { color:var(--muted-foreground); font-size:12px; } /* 定义当前元素的样式规则。 */
  .knowledge-mobile-document-meta,.knowledge-mobile-document-foot { display:flex; align-items:center; justify-content:space-between; gap:8px; font-size:12px; } /* 定义当前元素的样式规则。 */
  .knowledge-mobile-document-meta span { overflow:hidden; text-overflow:ellipsis; white-space:nowrap; } /* 定义当前元素的样式规则。 */
  .knowledge-mobile-document-foot small { color:var(--muted-foreground); font-size:12px; } /* 定义当前元素的样式规则。 */
  .knowledge-pagination { justify-content:center; padding:10px; } /* 定义当前元素的样式规则。 */
  .knowledge-pagination :deep(.el-pagination__sizes),.knowledge-pagination :deep(.el-pagination__total) { display:none; } /* 定义当前元素的样式规则。 */
  .knowledge-policy-target { margin:0 16px; grid-template-columns:1fr; gap:7px; } /* 定义当前元素的样式规则。 */
  .knowledge-policy-target small { grid-column:1; } /* 定义当前元素的样式规则。 */
  .knowledge-policy-form { padding-inline:16px; } /* 定义当前元素的样式规则。 */
  .knowledge-policy-section { grid-template-columns:1fr; gap:12px; } /* 定义当前元素的样式规则。 */
  .knowledge-section-copy p { max-width:none; } /* 定义当前元素的样式规则。 */
  .knowledge-number-grid { gap:8px; } /* 定义当前元素的样式规则。 */
  .knowledge-policy-actions { flex-direction:column; align-items:stretch; } /* 定义当前元素的样式规则。 */
  .knowledge-upload-step > div { display:grid; gap:0; } /* 定义当前元素的样式规则。 */
  .metadata-grid,.knowledge-detail-meta { grid-template-columns:1fr; } /* 定义当前元素的样式规则。 */
  .knowledge-rule-grid { grid-template-columns:repeat(2,minmax(0,1fr)); } /* 定义当前元素的样式规则。 */
  .knowledge-detail-section-heading { align-items:flex-start; flex-direction:column; gap:3px; } /* 定义当前元素的样式规则。 */
  .knowledge-chunk summary { flex-wrap:wrap; } /* 定义当前元素的样式规则。 */
  .knowledge-chunk-body { padding-left:14px; } /* 定义当前元素的样式规则。 */
} /* 结束当前样式规则。 */
</style>
