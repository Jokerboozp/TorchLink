<script setup>
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { api, isAbort } from '../api'
import { useListLoader } from '../composables/useListLoader'
import { can } from '../permissions'
import { UiMessage } from '../ui/feedback.js'

const config = ref(null)
const form = reactive({
  baseUrl: '',
  model: '',
  apiKey: '',
  clearAPIKey: false,
  dimensions: 1024,
  batchSize: 16,
  queryInstruction: '',
  timeoutSeconds: 60
})
const loading = ref(false)
const saving = ref(false)
const testing = ref(false)
const loadError = ref('')
const error = ref('')
const result = ref(null)
const canRead = computed(() => can('GET /api/v1/ai/embedding-config'))
const canSave = computed(() => can('PUT /api/v1/ai/embedding-config'))
const canTest = computed(() => can('POST /api/v1/ai/embedding-test'))
const busy = computed(() => loading.value || saving.value || testing.value)
const fingerprint = computed(() => JSON.stringify(candidateFields()))
const loader = useListLoader(loading)

function candidateFields() {
  return {
    baseUrl: form.baseUrl.trim(),
    model: form.model.trim(),
    apiKey: form.apiKey.trim(),
    clearAPIKey: form.clearAPIKey,
    dimensions: Number(form.dimensions),
    batchSize: Number(form.batchSize),
    queryInstruction: form.queryInstruction.trim(),
    timeoutSeconds: Number(form.timeoutSeconds)
  }
}

function candidate() {
  error.value = ''
  const body = candidateFields()
  if (!body.baseUrl || !body.model) {
    error.value = '请填写 Embedding 服务地址和模型名称'
    return null
  }
  try {
    const url = new URL(body.baseUrl)
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash) throw new Error()
  } catch {
    error.value = '请填写不含凭据和查询参数的 HTTP 或 HTTPS API 地址'
    return null
  }
  if (
    !Number.isSafeInteger(body.dimensions) ||
    body.dimensions <= 0 ||
    !Number.isSafeInteger(body.batchSize) ||
    body.batchSize <= 0 ||
    !Number.isSafeInteger(body.timeoutSeconds) ||
    body.timeoutSeconds <= 0
  ) {
    error.value = '向量维度、每批分片数和超时秒数必须为正整数'
    return null
  }
  if (body.clearAPIKey && body.apiKey) {
    error.value = '清除密钥时请先留空接口密钥'
    return null
  }
  return { body, fingerprint: fingerprint.value }
}

// The vector service deployed with the platform needs no API key.
const bundled = computed(() => config.value?.bundled || null)
const usingBundled = computed(() => Boolean(bundled.value) && form.baseUrl.trim().replace(/\/$/, '') === bundled.value.baseUrl)
// Reranking is deployment configuration (IOT_RERANK_URL), shown read-only.
const rerank = computed(() => config.value?.rerank || null)
function useBundled() {
  if (!bundled.value) return
  Object.assign(form, {
    baseUrl: bundled.value.baseUrl,
    model: bundled.value.model,
    dimensions: bundled.value.dimensions,
    queryInstruction: '',
    apiKey: '',
    clearAPIKey: Boolean(config.value?.apiKeyConfigured)
  })
}

function sync(value) {
  config.value = value
  Object.assign(form, {
    baseUrl: value.baseUrl || '',
    model: value.model || '',
    dimensions: value.dimensions || 1024,
    batchSize: value.batchSize || 16,
    queryInstruction: value.queryInstruction || '',
    timeoutSeconds: value.timeoutSeconds || 60,
    apiKey: '',
    clearAPIKey: false
  })
}

async function load() {
  if (!canRead.value) return
  loadError.value = ''
  try {
    sync(await loader.run(signal => api('/api/v1/ai/embedding-config', { signal })))
  } catch (cause) {
    if (!isAbort(cause)) loadError.value = cause.message || 'Embedding 配置读取失败'
  }
}

async function testConnection() {
  if (!canTest.value || busy.value) return
  const value = candidate()
  if (!value) return
  testing.value = true
  result.value = null
  try {
    const response = await api('/api/v1/ai/embedding-test', { method: 'POST', body: JSON.stringify(value.body) })
    if (value.fingerprint !== fingerprint.value) return
    result.value = { ...response, fingerprint: value.fingerprint }
    if (!response.success) error.value = response.error || response.message || 'Embedding API 连接测试失败'
  } catch (cause) {
    if (value.fingerprint === fingerprint.value) error.value = cause.message || 'Embedding API 连接测试失败'
  } finally {
    testing.value = false
  }
}

async function save() {
  if (!canSave.value || busy.value) return
  const value = candidate()
  if (!value) return
  saving.value = true
  try {
    const response = await api('/api/v1/ai/embedding-config', { method: 'PUT', body: JSON.stringify(value.body) })
    sync(response)
    result.value = null
    UiMessage.success('Embedding 配置已保存；知识库将按当前配置建立索引')
  } catch (cause) {
    error.value = cause.message || 'Embedding 配置保存失败'
  } finally {
    saving.value = false
  }
}

watch(fingerprint, () => {
  if (result.value && result.value.fingerprint !== fingerprint.value) result.value = null
})
onMounted(load)
onBeforeUnmount(loader.cancel)
</script>

<template>
  <ui-card class="surface-card embedding-config" shadow="never" v-loading="loading">
    <template #header
      ><div class="embedding-heading">
        <div>
          <strong>知识库 Embedding</strong
          ><small
            >独立于对话模型，将知识分片转换为向量；知识与索引保存在 PostgreSQL /
            pgvector。默认使用随平台部署的本地向量服务（bge-m3），也可改用外部 HTTPS API。</small
          >
        </div>
        <ui-button v-if="canRead" size="small" plain :loading="loading" :disabled="saving || testing" @click="load">刷新配置</ui-button>
      </div></template
    >
    <ui-alert v-if="loadError" :title="loadError" type="error" :closable="false" show-icon />
    <div v-if="config && !loadError" class="embedding-status">
      <div class="embedding-bundled">
        <span class="embedding-label">向量计算</span
        ><ui-tag :type="config.local ? 'success' : 'info'" effect="light">{{ config.local ? '本地向量服务' : '外部 API' }}</ui-tag
        ><small v-if="bundled">本地服务：{{ bundled.model }}，{{ bundled.dimensions }} 维，无需接口密钥。</small
        ><small v-else>本部署未启用本地向量服务（IOT_EMBEDDING_URL 指向外部 API），按部署文档启用后可在此切换。</small
        ><ui-button v-if="bundled && canSave && !usingBundled" size="small" plain :disabled="busy" @click="useBundled"
          >切换为本地向量服务</ui-button
        >
      </div>
      <div v-if="rerank" class="embedding-bundled">
        <span class="embedding-label">检索重排</span
        ><ui-tag :type="rerank.enabled ? 'success' : 'warning'" effect="light">{{
          !rerank.enabled ? '未启用' : rerank.local ? '本地重排服务' : '外部重排 API'
        }}</ui-tag
        ><small
          >{{
            !rerank.enabled
              ? '检索结果按向量与关键词得分排序。'
              : rerank.model
                ? `${rerank.model}，对检索候选按相关性重新排序。`
                : `外部地址 ${rerank.baseUrl}。`
          }}由部署配置 IOT_RERANK_URL 决定，修改后重启平台生效。</small
        >
      </div>
    </div>
    <ui-form label-position="top" :model="form" :disabled="busy || (!canSave && !canTest) || Boolean(loadError)">
      <div class="embedding-grid">
        <ui-form-item label="Embedding 服务地址"
          ><ui-input v-model="form.baseUrl" placeholder="本地服务地址，或 https://API 服务地址/v1"
        /></ui-form-item>
        <ui-form-item label="Embedding 模型"><ui-input v-model="form.model" placeholder="填写服务支持的向量模型名称" /></ui-form-item>
        <div>
          <ui-form-item label="接口密钥"
            ><ui-input
              v-model="form.apiKey"
              type="password"
              show-password
              autocomplete="off"
              :disabled="form.clearAPIKey"
              placeholder="外部 API 填写 API Key；留空沿用已保存的密钥"
          /></ui-form-item>
          <p class="embedding-hint">
            {{
              usingBundled
                ? '本地向量服务无需接口密钥。'
                : config?.apiKeyConfigured
                  ? '已保存密钥，页面不会显示密钥内容。'
                  : '尚未保存接口密钥。'
            }}
          </p>
          <ui-checkbox v-model="form.clearAPIKey" :disabled="!config?.apiKeyConfigured">保存时清除已存密钥</ui-checkbox>
        </div>
        <div class="embedding-numbers">
          <ui-form-item label="向量维度"><ui-input-number v-model="form.dimensions" :min="1" :step="1" /></ui-form-item
          ><ui-form-item label="每批分片数"><ui-input-number v-model="form.batchSize" :min="1" :step="1" /></ui-form-item
          ><ui-form-item label="调用超时（秒）"><ui-input-number v-model="form.timeoutSeconds" :min="1" :step="1" /></ui-form-item>
        </div>
        <ui-form-item class="embedding-query" label="查询向量指令（可选）"
          ><ui-input v-model="form.queryInstruction" type="textarea" :rows="2" placeholder="模型要求查询前缀时填写；文档分片不附加此指令"
        /></ui-form-item>
      </div>
      <p class="embedding-hint">向量维度应与 API 实际输出一致。改变模型或维度会重建知识索引，进度可在知识库查看。</p>
      <div class="embedding-actions">
        <small>{{ canSave ? '可直接保存，连接测试为可选操作。' : '当前账号可查看配置。' }}</small>
        <div>
          <ui-button v-if="canTest" plain :loading="testing" :disabled="busy || Boolean(loadError)" @click="testConnection"
            >测试 Embedding</ui-button
          ><ui-button v-if="canSave" type="primary" :loading="saving" :disabled="busy || Boolean(loadError)" @click="save"
            >保存 Embedding 配置</ui-button
          >
        </div>
      </div>
    </ui-form>
    <ui-alert v-if="error" class="embedding-result" :title="error" type="error" :closable="false" show-icon />
    <div v-if="result" class="embedding-result">
      <ui-tag :type="result.success ? 'success' : 'danger'">{{ result.success ? '连接测试通过' : '连接测试失败' }}</ui-tag
      ><span v-if="result.success">返回 {{ result.dimensions }} 维向量</span
      ><span v-if="result.latencyMs != null">耗时 {{ result.latencyMs }} 毫秒</span><small>{{ result.message }}</small>
    </div>
  </ui-card>
</template>

<style scoped>
.embedding-config {
  min-width: 0;
}
.embedding-heading,
.embedding-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
}
.embedding-heading > div {
  display: grid;
  gap: 4px;
}
.embedding-heading small,
.embedding-actions small,
.embedding-hint {
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.6;
}
.embedding-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}
.embedding-grid :deep(.n-form-item),
.embedding-grid :deep(.n-input),
.embedding-grid :deep(.n-input-number) {
  min-width: 0;
  width: 100%;
}
.embedding-numbers {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}
.embedding-query {
  grid-column: 1 / -1;
}
.embedding-hint {
  margin: 5px 0 10px;
}
.embedding-status {
  display: grid;
  gap: 8px;
  margin-bottom: 14px;
}
.embedding-bundled {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
}
.embedding-label {
  color: var(--text-muted);
  font-size: 12px;
  min-width: 56px;
}
.embedding-bundled small {
  color: var(--text-muted);
  font-size: 12px;
}
.embedding-actions > div {
  display: flex;
  gap: 8px;
}
.embedding-result {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 14px;
  font-size: 12px;
}
@media (max-width: 760px) {
  .embedding-grid {
    grid-template-columns: 1fr;
  }
  .embedding-heading,
  .embedding-actions {
    align-items: stretch;
    flex-direction: column;
  }
  .embedding-actions > div {
    flex-wrap: wrap;
  }
  .embedding-query {
    grid-column: auto;
  }
}
@media (max-width: 420px) {
  .embedding-numbers {
    grid-template-columns: 1fr;
  }
  .embedding-actions > div :deep(.n-button) {
    width: 100%;
  }
}
</style>
