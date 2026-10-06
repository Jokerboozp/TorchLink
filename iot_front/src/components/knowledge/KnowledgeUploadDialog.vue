<script setup>
// 上传知识文档并绑定智能体。进度只反映浏览器已发送的字节；发送完成后等待服务器保存原件，不估算后续进度。
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { Upload } from '@lucide/vue'
import { can } from '../../permissions'
import { UiMessage } from '../../ui/feedback.js'
import { notifyError } from '../../api'
import { uploadWithProgress } from '../../transfer'
import { useUnsavedGuard } from '../../composables/unsavedGuard.js'
import { formatBytes } from '../../format'
import { agentKey, agentName } from '../../knowledge'

const visible = defineModel({ type: Boolean, default: false })
// 可关联的智能体；默认关联第一个。
const props = defineProps({ agents: { type: Array, default: () => [] } })
// uploaded：上传成功；changed：取消时文件可能已送达，页面应刷新列表核对。
const emit = defineEmits(['uploaded', 'changed'])
const uploadRef = ref(null)
const uploading = ref(false)
const selectedFile = ref(null)
const workflowId = ref('')
const category = ref('manual')
const tags = ref([])
const canUpload = computed(() => can('POST /api/v1/knowledge/documents'))
let disposed = false
watch(
  () => props.agents,
  agents => {
    if (!workflowId.value && agents.length) workflowId.value = agentKey(agents[0])
  },
  { immediate: true }
)
function agentLabel(id) {
  if (!id) return '未关联智能体'
  const agent = props.agents.find(item => agentKey(item) === id)
  return agent ? agentName(agent) : id
}
function chooseFile(file) {
  if (Number(file.size || file.raw?.size || 0) > 32 * 1024 * 1024) {
    selectedFile.value = null
    // 上传控件在回调之后才把文件加入列表，下一轮再清空，被拒绝的文件才不会留在列表里。
    void nextTick(() => uploadRef.value?.clearFiles())
    UiMessage.error('知识库文件不能超过 32 MB')
    return
  }
  selectedFile.value = file.raw || null
}
function removeFile() {
  selectedFile.value = null
}
function rejectExtra() {
  UiMessage.warning('每次只能上传一个知识库文件')
}

// 上传使用 uploadWithProgress（XMLHttpRequest 读取真实的已发送字节），可中止。
function uploadTaskFor(path, body, onProgress) {
  const controller = new AbortController()
  return { promise: uploadWithProgress(path, body, onProgress, controller.signal), abort: () => controller.abort() }
}
const uploadProgress = ref(null)
const uploadPercent = computed(() => {
  const value = uploadProgress.value
  return value?.total ? Math.min(100, Math.floor((value.loaded / value.total) * 100)) : 0
})
const uploadSent = computed(() => Boolean(uploadProgress.value?.total) && uploadProgress.value.loaded >= uploadProgress.value.total)
let uploadTask = null
useUnsavedGuard(() => uploading.value)

async function upload() {
  if (!workflowId.value) return UiMessage.warning('请选择要关联的智能体')
  if (!selectedFile.value) return UiMessage.warning('请先选择知识库文件')
  if (uploading.value) return
  uploading.value = true
  uploadProgress.value = { loaded: 0, total: Number(selectedFile.value.size || 0) }
  let task = null
  try {
    const form = new FormData()
    form.append('file', selectedFile.value)
    form.append('workflowId', workflowId.value)
    if (category.value.trim()) form.append('category', category.value.trim())
    if (tags.value.length) form.append('tags', tags.value.join(','))
    task = uploadTaskFor('/api/v1/knowledge/documents', form, (loaded, total) => {
      if (uploadTask === task) uploadProgress.value = { loaded, total }
    })
    uploadTask = task
    const created = await task.promise
    // 用户已取消或离开页面时，迟到的结果不再关闭弹窗或切换标签。
    if (disposed || uploadTask !== task) return
    UiMessage.success(`文档已上传并绑定到 ${agentLabel(created.workflowId)}，索引将在后台建立`)
    selectedFile.value = null
    category.value = 'manual'
    tags.value = []
    uploadRef.value?.clearFiles()
    visible.value = false
    emit('uploaded')
  } catch (error) {
    if (!disposed && error?.name !== 'AbortError' && (!task || uploadTask === task)) notifyError(error)
  } finally {
    if (!task || uploadTask === task) {
      uploadTask = null
      uploading.value = false
      uploadProgress.value = null
    }
  }
}

function cancelUpload() {
  const task = uploadTask
  if (task) {
    const sent = uploadSent.value
    uploadTask = null
    task.abort()
    uploading.value = false
    uploadProgress.value = null
    if (sent) {
      UiMessage.warning('文件已发送完毕，服务器可能已接收；请在文档列表中确认，必要时删除')
      emit('changed')
    } else UiMessage.info('已取消上传')
  }
  visible.value = false
}
onBeforeUnmount(() => {
  disposed = true
  uploadTask?.abort()
  uploadTask = null
})
</script>

<template>
  <ui-dialog
    v-model="visible"
    title="上传知识文档并绑定智能体"
    width="min(680px, 94vw)"
    class="knowledge-upload-dialog"
    :close-on-press-escape="!uploading"
    :show-close="!uploading"
  >
    <section class="knowledge-upload-section">
      <div class="knowledge-upload-step">
        <span>1</span>
        <div><strong>选择文档</strong><small>每次上传一个文件，最大 32 MB；上传后在后台建立索引</small></div>
      </div>
      <ui-upload
        ref="uploadRef"
        drag
        :auto-upload="false"
        :disabled="!canUpload || uploading"
        :limit="1"
        accept=".pdf,.docx,.pptx,.xlsx,.odt,.odp,.ods,.txt,.md,.csv,.json,.html,.htm,.xml"
        :on-change="chooseFile"
        :on-remove="removeFile"
        :on-exceed="rejectExtra"
        ><Upload class="upload-icon" />
        <div class="el-upload__text">拖放文件到这里，或<em>点击选择</em></div>
        <template #tip><div class="el-upload__tip">支持 PDF、办公文档、网页和文本；扫描件需先进行文字识别。</div></template></ui-upload
      >
    </section>
    <section class="knowledge-upload-section">
      <div class="knowledge-upload-step">
        <span>2</span>
        <div><strong>指定归属</strong><small>文档只供所选智能体检索</small></div>
      </div>
      <ui-form label-position="top" class="knowledge-upload-form">
        <ui-form-item label="关联智能体（必选）"
          ><ui-select v-model="workflowId" filterable :disabled="uploading" placeholder="选择智能体"
            ><ui-option
              v-for="agent in agents"
              :key="agentKey(agent)"
              :label="agentName(agent) + ' · ' + agentKey(agent)"
              :value="agentKey(agent)" /></ui-select
        ></ui-form-item>
        <div class="metadata-grid">
          <ui-form-item label="知识分类（可选）"
            ><ui-select v-model="category" :disabled="uploading"
              ><ui-option label="设备手册" value="manual" /><ui-option label="告警处置操作规程" value="alarm-sop" /><ui-option
                label="运维维修"
                value="maintenance" /><ui-option label="消防规范" value="regulation" /><ui-option
                label="常见问题"
                value="faq" /></ui-select></ui-form-item
          ><ui-form-item label="知识标签（可选）"
            ><ui-select
              v-model="tags"
              multiple
              filterable
              allow-create
              default-first-option
              :disabled="uploading"
              placeholder="输入标签后按回车"
          /></ui-form-item>
        </div>
      </ui-form>
    </section>
    <div v-if="uploading && uploadProgress" class="knowledge-upload-progress" role="status">
      <ui-progress :percentage="uploadPercent" :stroke-width="6" :show-text="false" />
      <small>{{
        uploadSent
          ? `已发送 ${formatBytes(uploadProgress.total)}，等待服务器保存原件…`
          : `已发送 ${formatBytes(uploadProgress.loaded)} / ${formatBytes(uploadProgress.total)}（${uploadPercent}%）`
      }}</small>
    </div>
    <template #footer
      ><ui-button @click="cancelUpload">{{ uploading ? '取消上传' : '取消' }}</ui-button
      ><ui-button
        v-permission="'POST /api/v1/knowledge/documents'"
        type="primary"
        :loading="uploading"
        :disabled="!canUpload || !selectedFile || !workflowId"
        @click="upload"
        >上传并建立索引</ui-button
      ></template
    >
  </ui-dialog>
</template>

<style scoped>
.knowledge-upload-progress {
  display: grid;
  gap: var(--space-1);
  margin-top: var(--space-3);
  color: var(--text-muted);
  font-size: var(--font-size-sm);
}
.knowledge-upload-section {
  min-width: 0;
  padding: 0 0 20px;
} /* 上传步骤各占独立区域，说明不与输入框挤在同一行。 */
.knowledge-upload-section + .knowledge-upload-section {
  padding-top: 20px;
  border-top: 1px solid var(--border);
}
.knowledge-upload-section:last-of-type {
  padding-bottom: 0;
}
.knowledge-upload-step {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 14px;
}
.knowledge-upload-step > span {
  width: 25px;
  height: 25px;
  flex: none;
  display: grid;
  place-items: center;
  color: var(--surface);
  background: var(--primary);
  border-radius: 50%;
  font-size: 12px;
  font-weight: 700;
}
.knowledge-upload-step > div {
  display: flex;
  align-items: baseline;
  gap: 9px;
}
.knowledge-upload-step strong {
  font-size: 14px;
}
.knowledge-upload-step small,
.field-tip {
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.5;
}
.upload-icon {
  width: 32px;
  height: 32px;
  color: var(--primary-text);
}
.knowledge-upload-form :deep(.n-form-item),
.knowledge-upload-form :deep(.n-select) {
  width: 100%;
  min-width: 0;
} /* 表单字段与选择器占据整行。 */
.knowledge-upload-form .field-tip {
  display: block;
  margin: -3px 0 16px;
} /* 说明独立换行，避免覆盖选择器。 */
.metadata-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.metadata-grid > * {
  min-width: 0;
}
@media (max-width: 640px) {
  .knowledge-upload-step > div {
    display: grid;
    gap: 0;
  }
  .metadata-grid {
    grid-template-columns: 1fr;
  }
}
</style>
