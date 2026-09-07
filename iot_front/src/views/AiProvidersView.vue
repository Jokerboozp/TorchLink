<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { api, notifyError, session } from '../api'

const emit = defineEmits(['navigate'])

const runtime = ref({ items:[], active:{ id:'disabled', name:'未启用', enabled:false }, config:null, healthy:false, healthMessage:'正在读取模型服务状态' })
const loading = ref(false)
const saving = ref(false)
const loadError = ref('')
const providerError = ref('')
const providerForm = reactive({ provider:'ollama', baseUrl:'http://localhost:11434', model:'qwen3:1.7b', apiKey:'' })
const providerOptions = [
  { id:'ollama', label:'本地 Ollama', description:'使用 CentOS 或本机部署的 Ollama，不需要 API Key。' },
  { id:'deepseek', label:'DeepSeek API', description:'使用 DeepSeek 的 OpenAI 兼容接口和 API Key。' },
  { id:'openai-compatible', label:'OpenAI 兼容 API', description:'连接其他实现 Chat Completions 的模型服务。' }
]
const capabilities = [
  { title:'AI 告警研判', description:'告警详情中的风险分析、原因判断和人工处置建议。', page:'alarms', label:'告警中心' },
  { title:'AI Workflow 对话', description:'通过受控工具查询设备、告警、趋势和运维知识。', page:'ai', label:'AI 工作流' },
  { title:'智能巡检', description:'生成设备健康巡检结论，并标记数据局限和优先处理设备。', page:'inspection', label:'智能巡检' },
  { title:'告警规则草稿', description:'根据自然语言生成待人工复核的自动化规则草稿。', page:'rules', label:'告警规则' },
  { title:'协议助手与运维报告', description:'协议配置辅助、结构化输出和平台运维报告共用当前 Provider。', page:'protocols', label:'设备接入' }
]

const isAdmin = computed(() => session.role === 'admin')
const selectedProviderOption = computed(() => providerOptions.find(item => item.id === providerForm.provider) || providerOptions[0])
const activeProvider = computed(() => runtime.value.config?.provider || runtime.value.active?.id || 'disabled')
const activeProviderName = computed(() => providerOptions.find(item => item.id === activeProvider.value)?.label || runtime.value.active?.name || '未配置')
const activeModel = computed(() => runtime.value.config?.model || runtime.value.active?.model || '未设置')
const activeStatusType = computed(() => runtime.value.healthy ? 'success' : activeProvider.value === 'disabled' ? 'info' : 'warning')
const activeStatusLabel = computed(() => runtime.value.healthy ? '连接正常' : runtime.value.healthMessage || '连接异常')

function providerLabel(provider) {
  return providerOptions.find(item => item.id === provider)?.label || provider || '未配置'
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
    loadError.value = error.message || 'AI Provider 状态读取失败'
  } finally {
    loading.value = false
  }
}

async function saveProviderConfig() {
  if (!isAdmin.value || saving.value) return
  providerError.value = ''
  const provider = providerForm.provider.trim()
  const baseUrl = providerForm.baseUrl.trim()
  const model = providerForm.model.trim()
  if (!provider || !baseUrl || !model) {
    providerError.value = '请填写 Provider、服务地址和模型名称'
    return
  }
  if (provider !== 'ollama' && !providerForm.apiKey.trim() && activeProvider.value !== provider) {
    providerError.value = '切换到 API Provider 时必须填写 API Key'
    return
  }
  saving.value = true
  try {
    const body = { provider, baseUrl, model }
    if (providerForm.apiKey.trim()) body.apiKey = providerForm.apiKey.trim()
    const result = await api('/api/v1/ai/providers/config', { method:'PUT', body:JSON.stringify(body) })
    syncProviderForm({ config:result })
    await loadRuntime()
    ElMessage.success(`已切换到 ${providerLabel(provider)}，所有 AI 功能立即生效`)
  } catch (error) {
    providerError.value = error.message || 'Provider 更新失败'
  } finally {
    saving.value = false
  }
}

onMounted(loadRuntime)
</script>

<template>
  <div class="ai-management-page">
    <el-card shadow="never" class="surface-card ai-management-hero" v-loading="loading">
      <div class="ai-management-hero-grid">
        <div class="ai-management-hero-copy"><span class="section-kicker">AI CONTROL CENTER</span><h3>统一管理 AI Provider 与业务能力</h3><p>在这里选择模型服务和活动模型。保存后，告警研判、工作流、巡检、规则草稿及协议助手会共用新配置。</p></div>
        <div class="ai-active-provider"><div class="ai-active-provider-heading"><span>当前活动 Provider</span><el-tag :type="activeStatusType" effect="light">{{ activeStatusLabel }}</el-tag></div><strong>{{ activeProviderName }}</strong><small>{{ activeModel }} · {{ runtime.config?.apiKeyConfigured ? 'API Key 已配置' : '无需 API Key' }}</small><small v-if="runtime.config?.baseUrl">{{ runtime.config.baseUrl }}</small></div>
      </div>
    </el-card>

    <el-alert v-if="loadError" :title="loadError" type="error" :closable="false" show-icon><el-button plain size="small" @click="loadRuntime">重新加载</el-button></el-alert>

    <div class="ai-management-grid">
      <el-card shadow="never" class="surface-card ai-provider-config">
        <template #header><div class="card-header"><div><strong>Provider 配置</strong><small>连接测试通过后立即应用到全部 AI 功能</small></div><el-tag effect="plain">ADMIN</el-tag></div></template>
        <template v-if="isAdmin">
          <el-form label-position="top" :model="providerForm" :disabled="saving">
            <el-form-item label="模型来源"><el-select v-model="providerForm.provider" class="provider-select" @change="providerChanged"><el-option v-for="item in providerOptions" :key="item.id" :label="item.label" :value="item.id" /></el-select></el-form-item>
            <p class="provider-description">{{ selectedProviderOption.description }}</p>
            <el-form-item label="服务地址"><el-input v-model="providerForm.baseUrl" placeholder="例如 http://192.168.24.133:11434 或 https://api.deepseek.com" /></el-form-item>
            <el-form-item label="模型名称"><el-input v-model="providerForm.model" placeholder="例如 qwen3:1.7b" /></el-form-item>
            <el-form-item v-if="providerForm.provider !== 'ollama'" label="API Key"><el-input v-model="providerForm.apiKey" type="password" show-password autocomplete="off" placeholder="留空表示沿用当前密钥" /></el-form-item>
            <el-button class="provider-apply" type="primary" :loading="saving" @click="saveProviderConfig">测试并应用</el-button>
          </el-form>
          <el-alert v-if="providerError" class="provider-error" :title="providerError" type="error" :closable="false" show-icon />
          <small v-if="runtime.config?.apiKeyConfigured && providerForm.provider !== 'ollama'" class="provider-key-hint">当前已保存 API Key：{{ runtime.config.apiKeyHint || '已配置' }}；留空提交会继续使用它。</small>
        </template>
        <div v-else class="provider-viewer-summary"><el-alert title="Provider 配置仅限管理员修改。" type="info" :closable="false" show-icon /><strong>{{ activeProviderName }}</strong><span>{{ activeModel }} · {{ runtime.config?.apiKeyConfigured ? 'API Key 已配置' : '无需 API Key' }}</span></div>
      </el-card>

      <el-card shadow="never" class="surface-card ai-capability-card">
        <template #header><div class="card-header"><div><strong>AI 业务能力</strong><small>所有能力跟随当前活动 Provider</small></div><el-tag type="success" effect="plain">{{ runtime.healthy ? '可用' : '待检查' }}</el-tag></div></template>
        <div class="ai-capability-list">
          <div v-for="item in capabilities" :key="item.title" class="ai-capability-item"><span class="ai-capability-dot" :class="{ online:runtime.healthy }" /><div><strong>{{ item.title }}</strong><p>{{ item.description }}</p></div><el-button size="small" plain @click="emit('navigate', item.page)">{{ item.label }}</el-button></div>
        </div>
      </el-card>
    </div>

    <el-card shadow="never" class="surface-card ai-provider-list">
      <template #header><div class="card-header"><div><strong>可用 Provider</strong><small>当前 Provider 会显示“活动中”</small></div><el-button size="small" :loading="loading" @click="loadRuntime">刷新状态</el-button></div></template>
      <el-table v-loading="loading" :data="runtime.items || []" stripe>
        <el-table-column label="Provider" min-width="170"><template #default="{row}"><div class="provider-name"><strong>{{ row.name || row.id }}</strong><el-tag v-if="row.id === activeProvider" size="small" type="success" effect="plain">活动中</el-tag></div><small>{{ row.id }}</small></template></el-table-column>
        <el-table-column label="说明" min-width="270"><template #default="{row}">{{ row.description || '未填写说明' }}</template></el-table-column>
        <el-table-column label="默认模型" min-width="150"><template #default="{row}">{{ row.defaultModel || '由配置决定' }}</template></el-table-column>
        <el-table-column label="能力" min-width="240"><template #default="{row}"><div class="provider-capabilities"><el-tag v-for="capability in (row.capabilities || [])" :key="capability" size="small" effect="plain">{{ capability }}</el-tag></div></template></el-table-column>
      </el-table>
    </el-card>

    <el-alert class="ai-management-note" title="配置说明" type="info" :closable="false" show-icon>Ollama 地址填写服务根地址，例如 http://192.168.24.133:11434；误填 /v1 时平台会自动归一化。API Key 只在提交和服务端调用时使用，页面不会显示完整密钥。</el-alert>
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
.provider-apply { width:100%; margin-top:2px; }
.provider-error { margin-top:10px; }
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
