<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { api, session } from '../api'

const emit = defineEmits(['navigate'])

const runtime = ref({ items:[], active:{ id:'disabled', name:'未启用', enabled:false }, config:null, healthy:false, healthMessage:'正在读取模型服务状态' })
const loading = ref(false)
const testing = ref(false)
const applying = ref(false)
const loadError = ref('')
const providerError = ref('')
const testResult = ref(null)
const testedFingerprint = ref('')
const providerForm = reactive({ provider:'ollama', baseUrl:'http://localhost:11434', model:'qwen3:1.7b', apiKey:'' })
const providerOptions = [
  { id:'ollama', label:'本地模型（Ollama）', description:'使用 CentOS 或本机部署的 Ollama，不需要接口密钥。' },
  { id:'deepseek', label:'DeepSeek 云端模型', description:'使用 DeepSeek 云端模型和接口密钥。' },
  { id:'openai-compatible', label:'兼容接口模型', description:'连接其他兼容 Chat Completions 接口的模型服务。' }
]
const capabilityLabels = {
  chat:'对话',
  'alarm-analysis':'告警研判',
  'rule-draft':'规则草稿',
  'json-output':'结构化输出',
  'local-model':'本地模型',
  fallback:'降级响应'
}
const capabilities = [
  { title:'AI 告警研判', description:'告警详情中的风险分析、原因判断和人工处置建议。', page:'alarms', label:'告警中心' },
  { title:'AI 工作流对话', description:'通过受控工具查询设备、告警、趋势和运维知识。', page:'ai', label:'工作流' },
  { title:'智能巡检', description:'生成设备健康巡检结论，并标记数据局限和优先处理设备。', page:'inspection', label:'智能巡检' },
  { title:'告警规则草稿', description:'根据自然语言生成待人工复核的自动化规则草稿。', page:'rules', label:'告警规则' },
  { title:'协议助手与运维报告', description:'协议配置辅助、结构化输出和平台运维报告共用当前模型服务。', page:'protocols', label:'设备接入' }
]

const isAdmin = computed(() => session.role === 'admin')
const selectedProviderOption = computed(() => providerOptions.find(item => item.id === providerForm.provider) || providerOptions[0])
const activeProvider = computed(() => runtime.value.config?.provider || runtime.value.active?.id || 'disabled')
const activeProviderName = computed(() => providerOptions.find(item => item.id === activeProvider.value)?.label || runtime.value.active?.name || '未配置')
const activeModel = computed(() => runtime.value.config?.model || runtime.value.active?.model || '未设置')
const activeStatusType = computed(() => runtime.value.healthy ? 'success' : activeProvider.value === 'disabled' ? 'info' : 'warning')
const activeStatusLabel = computed(() => runtime.value.healthy ? '连接正常' : runtime.value.healthMessage || '连接异常')
const busy = computed(() => testing.value || applying.value)
const candidateFingerprint = computed(() => [providerForm.provider.trim(), providerForm.baseUrl.trim(), providerForm.model.trim(), providerForm.apiKey.trim()].join('\u0000'))
const canApply = computed(() => Boolean(testResult.value?.success && testedFingerprint.value === candidateFingerprint.value))

function providerLabel(provider) {
  return providerOptions.find(item => item.id === provider)?.label || (provider === 'disabled' ? '未启用' : provider) || '未配置'
}

function providerDescription(item) {
  return providerOptions.find(option => option.id === item?.id)?.description || item?.description || '模型服务插件'
}

function capabilityLabel(value) {
  return capabilityLabels[value] || value
}

function syncProviderForm(value) {
  const config = value?.config
  if (!config) return
  if (providerOptions.some(item => item.id === config.provider)) providerForm.provider = config.provider
  providerForm.baseUrl = config.baseUrl || providerForm.baseUrl
  providerForm.model = config.model || providerForm.model
  providerForm.apiKey = ''
}

function providerChanged(provider) {
  if (provider === 'ollama') {
    if (!providerForm.baseUrl || providerForm.baseUrl.includes('api.deepseek.com')) providerForm.baseUrl = 'http://localhost:11434'
    if (!providerForm.model || providerForm.model.startsWith('deepseek')) providerForm.model = 'qwen3:1.7b'
    return
  }
  if (provider === 'deepseek') {
    if (!providerForm.baseUrl || providerForm.baseUrl.includes('localhost:11434')) providerForm.baseUrl = 'https://api.deepseek.com'
    if (!providerForm.model || providerForm.model.startsWith('qwen')) providerForm.model = 'deepseek-v4-flash'
    return
  }
  if (!providerForm.baseUrl || providerForm.baseUrl.includes('localhost:11434') || providerForm.baseUrl.includes('api.deepseek.com')) providerForm.baseUrl = ''
  if (!providerForm.model || providerForm.model.startsWith('qwen') || providerForm.model.startsWith('deepseek')) providerForm.model = ''
}

async function loadRuntime() {
  loading.value = true
  loadError.value = ''
  try {
    const value = await api('/api/v1/ai/providers?page=1&pageSize=100')
    runtime.value = value
    syncProviderForm(value)
  } catch (error) {
    loadError.value = error.message || '模型服务状态读取失败'
  } finally {
    loading.value = false
  }
}

function candidateConfig() {
  providerError.value = ''
  const provider = providerForm.provider.trim()
  const baseUrl = providerForm.baseUrl.trim()
  const model = providerForm.model.trim()
  const apiKey = providerForm.apiKey.trim()
  if (!provider || !baseUrl || !model) {
    providerError.value = '请填写模型来源、服务地址和模型名称'
    return null
  }
  if (provider !== 'ollama' && !apiKey && activeProvider.value !== provider) {
    providerError.value = '切换到云端或兼容接口模型时必须填写接口密钥'
    return null
  }
  const body = { provider, baseUrl, model }
  if (apiKey) body.apiKey = apiKey
  return { body, fingerprint: [provider, baseUrl, model, apiKey].join('\u0000') }
}

async function testProviderConfig() {
  if (!isAdmin.value || busy.value) return
  const candidate = candidateConfig()
  if (!candidate) return
  testing.value = true
  testResult.value = null
  testedFingerprint.value = ''
  try {
    const result = await api('/api/v1/ai/providers/test', { method:'POST', body:JSON.stringify(candidate.body) })
    testResult.value = { ...result, fingerprint:candidate.fingerprint }
    if (!result.success) {
      providerError.value = result.error || '模型服务测试失败，请检查地址、模型和接口密钥'
      return
    }
    testedFingerprint.value = candidate.fingerprint
    ElMessage.success('配置测试通过。确认无误后可点击“应用配置”')
  } catch (error) {
    providerError.value = error.message || '模型服务测试失败'
  } finally {
    testing.value = false
  }
}

async function applyProviderConfig() {
  if (!isAdmin.value || busy.value) return
  const candidate = candidateConfig()
  if (!candidate) return
  if (!canApply.value || testedFingerprint.value !== candidate.fingerprint) {
    providerError.value = '请先测试当前配置；修改地址、模型或接口密钥后需要重新测试'
    return
  }
  applying.value = true
  providerError.value = ''
  try {
    const result = await api('/api/v1/ai/providers/config', { method:'PUT', body:JSON.stringify(candidate.body) })
    syncProviderForm({ config:result })
    await loadRuntime()
    testResult.value = null
    testedFingerprint.value = ''
    ElMessage.success(`已应用${providerLabel(candidate.body.provider)}，所有 AI 功能立即生效`)
  } catch (error) {
    providerError.value = error.message || '模型服务应用失败'
  } finally {
    applying.value = false
  }
}

watch(candidateFingerprint, () => {
  if (testResult.value && testedFingerprint.value !== candidateFingerprint.value) {
    testResult.value = null
    testedFingerprint.value = ''
  }
})

onMounted(loadRuntime)
</script>

<template>
  <div class="ai-management-page">
    <el-card shadow="never" class="surface-card ai-management-hero" v-loading="loading">
      <div class="ai-management-hero-grid">
        <div class="ai-management-hero-copy"><span class="section-kicker">AI 模型管理</span><h3>统一管理 AI 模型与业务能力</h3><p>在这里选择模型服务和活动模型。应用后，告警研判、工作流、巡检、规则草稿及协议助手会共用新配置。</p></div>
        <div class="ai-active-provider"><div class="ai-active-provider-heading"><span>当前活动模型服务</span><el-tag :type="activeStatusType" effect="light">{{ activeStatusLabel }}</el-tag></div><strong>{{ activeProviderName }}</strong><small>{{ activeModel }} · {{ runtime.config?.apiKeyConfigured ? '接口密钥已配置' : '无需接口密钥' }}</small><small v-if="runtime.config?.baseUrl">{{ runtime.config.baseUrl }}</small></div>
      </div>
    </el-card>

    <el-alert v-if="loadError" :title="loadError" type="error" :closable="false" show-icon><el-button plain size="small" @click="loadRuntime">重新加载</el-button></el-alert>

    <div class="ai-management-grid">
      <el-card shadow="never" class="surface-card ai-provider-config">
        <template #header><div class="card-header"><div><strong>模型服务配置</strong><small>先测试当前填写内容，再选择是否应用到全部 AI 功能</small></div><el-tag effect="plain">管理员</el-tag></div></template>
        <template v-if="isAdmin">
          <el-form label-position="top" :model="providerForm" :disabled="busy">
            <el-form-item label="模型来源"><el-select v-model="providerForm.provider" class="provider-select" @change="providerChanged"><el-option v-for="item in providerOptions" :key="item.id" :label="item.label" :value="item.id" /></el-select></el-form-item>
            <p class="provider-description">{{ selectedProviderOption.description }}</p>
            <el-form-item label="服务地址"><el-input v-model="providerForm.baseUrl" placeholder="例如 http://192.168.24.133:11434 或 https://api.deepseek.com" /></el-form-item>
            <el-form-item label="模型名称"><el-input v-model="providerForm.model" placeholder="例如 qwen3:1.7b" /></el-form-item>
            <el-form-item v-if="providerForm.provider !== 'ollama'" label="接口密钥"><el-input v-model="providerForm.apiKey" type="password" show-password autocomplete="off" placeholder="留空表示沿用当前密钥" /></el-form-item>
            <div class="provider-actions"><el-button plain :loading="testing" @click="testProviderConfig">测试配置</el-button><el-button type="primary" :loading="applying" :disabled="!canApply" @click="applyProviderConfig">应用配置</el-button></div>
          </el-form>
          <el-alert v-if="providerError" class="provider-error" :title="providerError" type="error" :closable="false" show-icon />
          <div v-if="testResult" class="provider-test-result" :class="{ success:testResult.success, failed:!testResult.success }"><div><strong>{{ testResult.success ? '配置测试通过' : '配置测试失败' }}</strong><span v-if="testResult.latencyMs">耗时 {{ testResult.latencyMs }} ms</span></div><p v-if="testResult.answer">{{ testResult.answer }}</p><small v-if="testResult.success">当前填写内容未生效；确认无误后点击“应用配置”。</small></div>
          <small v-if="runtime.config?.apiKeyConfigured && providerForm.provider !== 'ollama'" class="provider-key-hint">当前已保存接口密钥：{{ runtime.config.apiKeyHint || '已配置' }}；留空测试或应用会继续使用它。</small>
        </template>
        <div v-else class="provider-viewer-summary"><el-alert title="模型服务配置仅限管理员修改。" type="info" :closable="false" show-icon /><strong>{{ activeProviderName }}</strong><span>{{ activeModel }} · {{ runtime.config?.apiKeyConfigured ? '接口密钥已配置' : '无需接口密钥' }}</span></div>
      </el-card>

      <el-card shadow="never" class="surface-card ai-capability-card">
        <template #header><div class="card-header"><div><strong>AI 业务能力</strong><small>所有能力跟随当前活动模型服务</small></div><el-tag type="success" effect="plain">{{ runtime.healthy ? '可用' : '待检查' }}</el-tag></div></template>
        <div class="ai-capability-list">
          <div v-for="item in capabilities" :key="item.title" class="ai-capability-item"><span class="ai-capability-dot" :class="{ online:runtime.healthy }" /><div><strong>{{ item.title }}</strong><p>{{ item.description }}</p></div><el-button size="small" plain @click="emit('navigate', item.page)">{{ item.label }}</el-button></div>
        </div>
      </el-card>
    </div>

    <el-card shadow="never" class="surface-card ai-provider-list">
      <template #header><div class="card-header"><div><strong>可用模型服务</strong><small>当前模型服务会显示“使用中”</small></div><el-button size="small" :loading="loading" @click="loadRuntime">刷新状态</el-button></div></template>
      <el-table v-loading="loading" :data="runtime.items || []" stripe>
        <el-table-column label="模型服务" min-width="190"><template #default="{row}"><div class="provider-name"><strong>{{ providerLabel(row.id) }}</strong><el-tag v-if="row.id === activeProvider" size="small" type="success" effect="plain">使用中</el-tag></div></template></el-table-column>
        <el-table-column label="说明" min-width="270"><template #default="{row}">{{ providerDescription(row) }}</template></el-table-column>
        <el-table-column label="默认模型" min-width="150"><template #default="{row}">{{ row.defaultModel || '由配置决定' }}</template></el-table-column>
        <el-table-column label="支持能力" min-width="240"><template #default="{row}"><div class="provider-capabilities"><el-tag v-for="capability in (row.capabilities || [])" :key="capability" size="small" effect="plain">{{ capabilityLabel(capability) }}</el-tag></div></template></el-table-column>
      </el-table>
    </el-card>

    <el-alert class="ai-management-note" title="配置说明" type="info" :closable="false" show-icon>本地模型地址填写 Ollama 服务根地址，例如 http://192.168.24.133:11434；误填 /v1 时平台会自动归一化。接口密钥只在测试、应用和服务端调用时使用，页面不会显示完整密钥。</el-alert>
  </div>
</template>

<style scoped>
.ai-management-page { display:grid; gap:16px; }
.ai-management-hero { overflow:hidden; }
.ai-management-hero-grid { display:grid; grid-template-columns:minmax(0,1.5fr) minmax(260px,1fr); gap:18px; align-items:stretch; }
.ai-management-hero-copy { display:grid; align-content:center; gap:7px; padding:7px 2px; }
.ai-management-hero-copy h3 { margin:0; color:#1f2329; font-size:22px; letter-spacing:-.02em; }
.ai-management-hero-copy p { max-width:720px; margin:0; color:#646c73; font-size:12px; line-height:1.8; }
.ai-active-provider { display:grid; align-content:center; gap:5px; padding:16px; background:#f5f9ff; border:1px solid #d6e8ff; border-radius:6px; }
.ai-active-provider-heading { display:flex; align-items:center; justify-content:space-between; gap:10px; color:#64748b; font-size:11px; }
.ai-active-provider strong { color:#1554ad; font-size:16px; }
.ai-active-provider small { color:#64748b; font-size:10px; line-height:1.5; word-break:break-all; }
.ai-management-grid { display:grid; grid-template-columns:minmax(300px,420px) minmax(0,1fr); gap:16px; align-items:start; }
.ai-provider-config :deep(.el-form-item) { margin-bottom:12px; }
.ai-provider-config :deep(.el-select) { width:100%; }
.provider-description { margin:-4px 0 11px; color:#64748b; font-size:10px; line-height:1.5; }
.provider-actions { display:flex; justify-content:flex-end; gap:8px; margin-top:2px; }
.provider-actions .el-button { min-width:104px; }
.provider-error { margin-top:10px; }
.provider-test-result { margin-top:14px; padding:11px; border:1px solid; border-radius:4px; }
.provider-test-result.success { background:#f6ffed; border-color:#b7eb8f; }
.provider-test-result.failed { background:#fff2f0; border-color:#ffccc7; }
.provider-test-result>div { display:flex; justify-content:space-between; align-items:center; gap:8px; }
.provider-test-result p { margin:8px 0; color:#3d3d3d; font-size:11px; line-height:1.6; white-space:pre-wrap; }
.provider-test-result span,.provider-test-result small { color:#8c8c8c; font-size:10px; }
.provider-test-result small { display:block; line-height:1.5; }
.provider-key-hint { display:block; margin-top:9px; color:#64748b; font-size:10px; line-height:1.5; }
.provider-viewer-summary { display:grid; gap:10px; }
.provider-viewer-summary strong { color:#1554ad; font-size:16px; }
.provider-viewer-summary span { color:#64748b; font-size:11px; }
.ai-capability-list { display:grid; gap:7px; }
.ai-capability-item { display:grid; grid-template-columns:auto minmax(0,1fr) auto; gap:10px; align-items:center; padding:10px 0; border-bottom:1px solid #f0f0f0; }
.ai-capability-item:last-child { border-bottom:0; }
.ai-capability-dot { width:8px; height:8px; background:#cbd5e1; border-radius:50%; }
.ai-capability-dot.online { background:#52c41a; box-shadow:0 0 0 3px #f6ffed; }
.ai-capability-item strong { color:#303133; font-size:12px; }
.ai-capability-item p { margin:3px 0 0; color:#7b8490; font-size:10px; line-height:1.5; }
.provider-name { display:flex; align-items:center; gap:6px; }
.provider-name small { color:#86909c; }
.provider-capabilities { display:flex; flex-wrap:wrap; gap:4px; }
.ai-management-note { margin-bottom:2px; }
@media (max-width:860px) { .ai-management-hero-grid,.ai-management-grid { grid-template-columns:1fr; } }
@media (max-width:560px) { .ai-capability-item { grid-template-columns:auto minmax(0,1fr); }.ai-capability-item .el-button { grid-column:2; justify-self:start; }.ai-management-hero-copy h3 { font-size:18px; } }
</style>
