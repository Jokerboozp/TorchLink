<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { api, download, notifyError } from '../api'
import { uploadWithProgress, transferText } from '../transfer'
import { UiMessage } from '../ui/feedback.js'
import { platformLabel, transportLabel } from '../presentation'
import FilePicker from './FilePicker.vue'

const props = defineProps({
  initialProtocolId: { type: String, default: '' },
  initialName: { type: String, default: '' },
  context: { type: Object, default: null }
})
const emit = defineEmits(['saved', 'selected', 'busy'])
const source = reactive({ protocolId: props.initialProtocolId, name: props.initialName, version: '', transport: '' })
const file = ref(null),
  targetPlatforms = ref([]),
  template = ref(null),
  result = ref(null),
  busy = ref(''),
  error = ref('')
let disposed = false,
  controller
// 构建是一次阻塞请求，界面只显示实际已用时间，不估算进度。
const elapsed = ref(0)
// 源码上传进度：发送完成前显示已上传比例，之后显示构建状态。
const uploadProgress = ref({ loaded: 0, total: 0 })
const uploading = computed(
  () => busy.value === 'build' && uploadProgress.value.total > 0 && uploadProgress.value.loaded < uploadProgress.value.total
)
let elapsedTimer = 0
function stopElapsed() {
  clearInterval(elapsedTimer)
  elapsedTimer = 0
}
function startElapsed() {
  stopElapsed()
  const started = Date.now()
  elapsed.value = 0
  elapsedTimer = setInterval(() => {
    elapsed.value = Math.floor((Date.now() - started) / 1000)
  }, 1000)
}
const elapsedText = computed(() =>
  elapsed.value >= 60 ? `${Math.floor(elapsed.value / 60)} 分 ${elapsed.value % 60} 秒` : `${elapsed.value} 秒`
)
onBeforeUnmount(() => {
  disposed = true
  stopElapsed()
  controller?.abort()
})
onMounted(async () => {
  try {
    const value = await api('/api/v2/protocol-source-template')
    if (!disposed) template.value = value
  } catch (e) {
    if (!disposed) {
      error.value = e.message
      notifyError(e)
    }
  }
})
function selection() {
  const release = result.value.release
  return {
    protocolId: release.protocolId,
    version: release.version,
    protocolPackageId: `${release.protocolId}@${release.version}`,
    status: release.status,
    release,
    context: props.context
  }
}
function useForTemplate() {
  if (result.value?.release.status === 'PUBLISHED') emit('selected', selection())
}
async function work(kind, action) {
  if (busy.value) return
  busy.value = kind
  error.value = ''
  emit('busy', true)
  controller = new AbortController()
  if (kind === 'build') startElapsed()
  try {
    await action({ signal: controller.signal })
  } catch (e) {
    if (!disposed && e.name !== 'AbortError') error.value = e.message || String(e)
  } finally {
    stopElapsed()
    if (!disposed) {
      busy.value = ''
      emit('busy', false)
    }
  }
}
function chooseFile(event) {
  file.value = event.target.files?.[0] || null
  error.value = ''
}
async function downloadTemplate(kind = '') {
  try {
    await download(
      `/api/v2/protocol-source-template?format=go-functions&kind=${kind}`,
      kind === 'tcp' ? 'go-tcp-protocol.zip' : 'go-protocol.zip'
    )
  } catch (e) {
    notifyError(e)
  }
}
async function upload() {
  if (!file.value || !source.protocolId.trim()) return UiMessage.warning('请选择 Go 源码并填写协议标识')
  if (file.value.size > 32 * 1024 * 1024) return UiMessage.warning('源码文件不能超过 32 MiB')
  await work('build', async options => {
    const body = new FormData()
    body.append('file', file.value)
    body.append('publish', 'false')
    for (const [key, value] of Object.entries(source)) body.append(key, String(value).trim())
    if (targetPlatforms.value.length) body.append('targetPlatforms', JSON.stringify(targetPlatforms.value))
    uploadProgress.value = { loaded: 0, total: file.value.size }
    const value = await uploadWithProgress(
      `/api/v2/protocols/${encodeURIComponent(source.protocolId.trim())}/source-releases`,
      body,
      (loaded, total) => (uploadProgress.value = { loaded, total }),
      options.signal
    )
    if (disposed) return
    result.value = value
    emit('saved', selection())
    UiMessage.success('源码构建与样例校验通过，版本已保存')
  })
}
async function publish() {
  if (result.value?.release.status !== 'VALIDATED') return
  await work('publish', async options => {
    const release = result.value.release
    const value = await api(
      `/api/v2/protocols/${encodeURIComponent(release.protocolId)}/releases/${encodeURIComponent(release.version)}/publish`,
      { ...options, method: 'POST', body: '{}' }
    )
    if (disposed) return
    result.value = { ...result.value, release: value }
    emit('saved', selection())
    UiMessage.success('协议已发布，可用于设备模板')
  })
}
</script>

<template>
  <section class="protocol-source-upload" :aria-busy="!!busy">
    <p class="muted-text">上传 Go 源码，构建并校验样例后保存版本。发布与用于设备模板分别操作。</p>
    <ui-alert
      v-if="template && !template.compilerAvailable"
      title="当前服务缺少源码编译环境，请联系管理员部署支持编译的后端服务。"
      type="warning"
      :closable="false"
    />
    <ui-alert v-if="error" title="操作未完成" type="error" :closable="false">
      <pre class="source-error">{{ error }}</pre>
    </ui-alert>
    <ui-form v-if="!result" :disabled="!!busy" label-position="top" @submit.prevent="upload">
      <div class="form-grid">
        <ui-form-item label="协议标识"><ui-input v-model="source.protocolId" placeholder="例如 vendor-fire" /></ui-form-item>
        <ui-form-item label="协议名称"><ui-input v-model="source.name" placeholder="例如消防设备协议" /></ui-form-item>
        <ui-form-item label="版本"><ui-input v-model="source.version" placeholder="Go 函数模式留空自动生成新版本" /></ui-form-item>
        <ui-form-item label="设备上报通道"
          ><ui-select v-model="source.transport" clearable placeholder="自动识别，可手动选择"
            ><ui-option
              v-for="value in ['MQTT', 'HTTP', 'TCP', 'UDP', 'TCP_UDP']"
              :key="value"
              :label="transportLabel(value)"
              :value="value" /></ui-select
        ></ui-form-item>
      </div>
      <ui-form-item label="源码文件或项目压缩包"
        ><div>
          <FilePicker accept=".go,.zip" :disabled="!!busy" @change="chooseFile" /><small class="subline">{{
            file?.name || '上传 .go 文件或完整 Go 项目 ZIP，最多 32 MiB。'
          }}</small>
        </div></ui-form-item
      >
      <div class="source-template-actions">
        <ui-button plain @click="downloadTemplate()">下载解析模板</ui-button
        ><ui-button plain @click="downloadTemplate('tcp')">下载 TCP / UDP 模板</ui-button>
      </div>
      <p class="muted-text">TCP / UDP 模板包含分帧、应答和命令编码示例。项目依赖请随 vendor 上传。</p>
      <ui-collapse
        ><ui-collapse-item title="编译选项" name="advanced"
          ><ui-form-item label="额外编译目标（可选）"
            ><ui-select v-model="targetPlatforms" multiple clearable :to="true" placeholder="默认仅构建当前服务平台"
              ><ui-option
                v-for="platform in template?.targetPlatforms || []"
                :key="platform"
                :label="platformLabel(platform)"
                :value="platform" /></ui-select></ui-form-item
          ><small>当前服务平台会运行样例；其他平台仅生成制品，需在目标平台实际试跑。</small></ui-collapse-item
        ></ui-collapse
      >
      <p v-if="uploading" role="status">正在上传源码 {{ transferText(uploadProgress.loaded, uploadProgress.total) }}，请保持页面打开。</p>
      <p v-else-if="busy === 'build'" role="status">
        正在构建并运行样例，每个平台编译最长 120 秒，请保持页面打开。<span aria-hidden="true">已用时 {{ elapsedText }}</span>
      </p>
      <div class="source-actions">
        <ui-button
          v-permission="'POST /api/v2/protocols/:id/source-releases'"
          native-type="submit"
          type="primary"
          :loading="busy === 'build'"
          :disabled="!!busy || template?.compilerAvailable === false"
          >构建并校验</ui-button
        >
      </div>
    </ui-form>
    <template v-else>
      <ui-descriptions :column="1" border>
        <ui-descriptions-item label="协议版本">{{ result.release.protocolId }} @ {{ result.release.version }}</ui-descriptions-item>
        <ui-descriptions-item label="状态">{{ result.release.status === 'PUBLISHED' ? '已发布' : '已校验，待发布' }}</ui-descriptions-item>
        <ui-descriptions-item label="样例校验">{{ result.testCases }} 条通过</ui-descriptions-item>
      </ui-descriptions>
      <div class="source-actions">
        <ui-button
          v-if="result.release.status === 'VALIDATED'"
          v-permission="'POST /api/v2/protocols/:id/releases/:version/publish'"
          type="primary"
          :loading="busy === 'publish'"
          @click="publish"
          >发布协议</ui-button
        >
        <ui-button v-if="context && result.release.status === 'PUBLISHED'" type="primary" @click="useForTemplate">用于当前模板</ui-button>
        <ui-button
          :disabled="!!busy"
          @click="
            () => {
              result = null
              source.version = ''
            }
          "
          >上传其他版本</ui-button
        >
      </div>
    </template>
  </section>
</template>

<style scoped>
.protocol-source-upload {
  min-width: 0;
}
.protocol-source-upload > * + * {
  margin-top: 16px;
}
.source-error {
  max-height: 260px;
  overflow: auto;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}
.source-actions,
.source-template-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 16px;
}
.source-actions {
  justify-content: flex-end;
}
</style>
