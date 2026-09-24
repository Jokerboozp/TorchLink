<script setup>
import { can } from '../permissions' /* 引入当前代码需要的依赖。 */
import { aiProviderOptions as providerOptions } from '../presentation' /* 引入当前代码需要的依赖。 */
import { computed, onMounted, reactive, ref, watch } from 'vue' /* 引入当前代码需要的依赖。 */
import { UiMessage } from '../ui/feedback.js' /* 引入当前代码需要的依赖。 */
import { api } from '../api' /* 引入当前代码需要的依赖。 */

const emit = defineEmits(['navigate']) /* 声明 emit。 */

const runtime = ref({ items:[], active:{ id:'disabled', name:'未启用', enabled:false }, config:null, healthy:false, healthMessage:'正在读取模型服务状态' }) /* 声明 runtime。 */
const loading = ref(false) /* 声明 loading。 */
const testing = ref(false) /* 声明 testing。 */
const applying = ref(false) /* 声明 applying。 */
const loadError = ref('') /* 声明 loadError。 */
const providerError = ref('') /* 声明 providerError。 */
const testResult = ref(null) /* 声明 testResult。 */
const testedFingerprint = ref('') /* 声明 testedFingerprint。 */
const providerForm = reactive({ provider:'ollama', baseUrl:'http://localhost:11434', model:'qwen3:1.7b', apiKey:'', maxTokens:2048 }) /* 声明 providerForm。 */
let loadVersion = 0

const capabilityLabels = { /* 声明 capabilityLabels。 */
  chat:'对话', /* 执行当前语句并推进处理流程。 */
  'alarm-analysis':'告警研判', /* 执行当前语句并推进处理流程。 */
  'rule-draft':'规则草稿', /* 执行当前语句并推进处理流程。 */
  'json-output':'JSON 输出', /* 执行当前语句并推进处理流程。 */
  'local-model':'本地模型', /* 执行当前语句并推进处理流程。 */
  fallback:'降级响应' /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
const capabilities = [ /* 声明 capabilities。 */
  { title:'智能告警研判', description:'告警详情中的风险分析、原因判断和人工处置建议。', page:'alarms', label:'告警中心' }, /* 执行当前语句并推进处理流程。 */
  { title:'智能助手对话', description:'通过受控工具查询设备、告警、趋势和运维知识。', page:'ai', label:'工作流' }, /* 执行当前语句并推进处理流程。 */
  { title:'智能巡检', description:'生成设备健康巡检结论，并标记数据局限和优先处理设备。', page:'inspection', label:'智能巡检' }, /* 执行当前语句并推进处理流程。 */
  { title:'告警规则草稿', description:'根据自然语言生成待人工复核的自动化规则草稿。', page:'rules', label:'告警规则' }, /* 执行当前语句并推进处理流程。 */
  { title:'协议助手与运维报告', description:'协议配置辅助、结构化输出和平台运维报告共用当前模型服务。', page:'protocols', label:'设备接入' } /* 执行当前语句并推进处理流程。 */
] /* 结束当前表达式或代码块。 */

const isAdmin = computed(() => can(['PUT /api/v1/ai/providers/config','POST /api/v1/ai/providers/test'])) /* 声明 isAdmin。 */
const selectedProviderOption = computed(() => providerOptions.find(item => item.id === providerForm.provider) || providerOptions[0]) /* 声明 selectedProviderOption。 */
const activeProvider = computed(() => runtime.value.config?.provider || runtime.value.active?.id || 'disabled') /* 声明 activeProvider。 */
const activeProviderName = computed(() => providerOptions.find(item => item.id === activeProvider.value)?.label || runtime.value.active?.name || '未配置') /* 声明 activeProviderName。 */
const activeModel = computed(() => runtime.value.config?.model || runtime.value.active?.model || '未设置') /* 声明 activeModel。 */
const activeStatusType = computed(() => runtime.value.healthy ? 'success' : activeProvider.value === 'disabled' ? 'info' : 'warning') /* 声明 activeStatusType。 */
const activeStatusLabel = computed(() => runtime.value.healthy ? '连接正常' : runtime.value.healthMessage || '连接异常') /* 声明 activeStatusLabel。 */
const busy = computed(() => testing.value || applying.value) /* 声明 busy。 */
const candidateFingerprint = computed(() => [providerForm.provider.trim(), providerForm.baseUrl.trim(), providerForm.model.trim(), providerForm.apiKey.trim(), providerForm.maxTokens].join('\u0000')) /* 声明 candidateFingerprint。 */
const canApply = computed(() => Boolean(testResult.value?.success && testedFingerprint.value === candidateFingerprint.value)) /* 声明 canApply。 */

function providerLabel(provider) { /* 定义 providerLabel 函数。 */
  return providerOptions.find(item => item.id === provider)?.label || (provider === 'disabled' ? '未启用' : provider) || '未配置' /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function providerDescription(item) { /* 定义 providerDescription 函数。 */
  return providerOptions.find(option => option.id === item?.id)?.description || item?.description || '模型服务插件' /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function capabilityLabel(value) { /* 定义 capabilityLabel 函数。 */
  return capabilityLabels[value] || '扩展能力' /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function syncProviderForm(value) { /* 定义 syncProviderForm 函数。 */
  const config = value?.config /* 声明 config。 */
  if (!config) return /* 判断条件并选择处理分支。 */
  if (providerOptions.some(item => item.id === config.provider)) providerForm.provider = config.provider /* 判断条件并选择处理分支。 */
  providerForm.baseUrl = config.baseUrl || providerForm.baseUrl /* 更新 providerForm.baseUrl 的值。 */
  providerForm.model = config.model || providerForm.model /* 更新 providerForm.model 的值。 */
  providerForm.maxTokens = config.maxTokens || 2048
  providerForm.apiKey = '' /* 更新 providerForm.apiKey 的值。 */
} /* 结束当前表达式或代码块。 */

function providerChanged(provider) { /* 定义 providerChanged 函数。 */
  if (provider === 'ollama') { /* 判断条件并选择处理分支。 */
    if (!providerForm.baseUrl || providerForm.baseUrl.includes('api.deepseek.com')) providerForm.baseUrl = 'http://localhost:11434' /* 判断条件并选择处理分支。 */
    if (!providerForm.model || providerForm.model.startsWith('deepseek')) providerForm.model = 'qwen3:1.7b' /* 判断条件并选择处理分支。 */
    return /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
  if (provider === 'deepseek') { /* 判断条件并选择处理分支。 */
    if (!providerForm.baseUrl || providerForm.baseUrl.includes('localhost:11434')) providerForm.baseUrl = 'https://api.deepseek.com' /* 判断条件并选择处理分支。 */
    if (!providerForm.model || providerForm.model.startsWith('qwen')) providerForm.model = 'deepseek-v4-flash' /* 判断条件并选择处理分支。 */
    return /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
  if (!providerForm.baseUrl || providerForm.baseUrl.includes('localhost:11434') || providerForm.baseUrl.includes('api.deepseek.com')) providerForm.baseUrl = '' /* 判断条件并选择处理分支。 */
  if (!providerForm.model || providerForm.model.startsWith('qwen') || providerForm.model.startsWith('deepseek')) providerForm.model = '' /* 判断条件并选择处理分支。 */
} /* 结束当前表达式或代码块。 */

async function loadRuntime() { /* 定义 loadRuntime 函数。 */
  const version = ++loadVersion
  loading.value = true /* 更新 loading.value 的值。 */
  loadError.value = '' /* 更新 loadError.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const value = await api('/api/v1/ai/providers?page=1&pageSize=100') /* 声明 value。 */
    if (version !== loadVersion) return
    runtime.value = value /* 更新 runtime.value 的值。 */
    syncProviderForm(value) /* 执行当前语句并推进处理流程。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    if (version === loadVersion) loadError.value = error.message || '模型服务状态读取失败' /* 更新 loadError.value 的值。 */
  } finally { /* 结束当前表达式或代码块。 */
    if (version === loadVersion) loading.value = false /* 更新 loading.value 的值。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function candidateConfig() { /* 定义 candidateConfig 函数。 */
  providerError.value = '' /* 更新 providerError.value 的值。 */
  const provider = providerForm.provider.trim() /* 声明 provider。 */
  const baseUrl = providerForm.baseUrl.trim() /* 声明 baseUrl。 */
  const model = providerForm.model.trim() /* 声明 model。 */
  const apiKey = providerForm.apiKey.trim() /* 声明 apiKey。 */
  const maxTokens = Number(providerForm.maxTokens)
  if (!provider || !baseUrl || !model) { /* 判断条件并选择处理分支。 */
    providerError.value = '请填写模型来源、服务地址和模型名称' /* 更新 providerError.value 的值。 */
    return null /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
  if (provider !== 'ollama' && !apiKey && activeProvider.value !== provider) { /* 判断条件并选择处理分支。 */
    providerError.value = '切换到云端或兼容接口模型时必须填写接口密钥' /* 更新 providerError.value 的值。 */
    return null /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
  if (!Number.isSafeInteger(maxTokens) || maxTokens < 128 || maxTokens > 8192) {
    providerError.value = '最大输出词元必须在 128 到 8192 之间'
    return null
  }
  const body = { provider, baseUrl, model, maxTokens } /* 声明 body。 */
  if (apiKey) body.apiKey = apiKey /* 判断条件并选择处理分支。 */
  return { body, fingerprint: [provider, baseUrl, model, apiKey, maxTokens].join('\u0000') } /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

async function testProviderConfig() { /* 定义 testProviderConfig 函数。 */
  if (!isAdmin.value || busy.value) return /* 判断条件并选择处理分支。 */
  const candidate = candidateConfig() /* 声明 candidate。 */
  if (!candidate) return /* 判断条件并选择处理分支。 */
  testing.value = true /* 更新 testing.value 的值。 */
  testResult.value = null /* 更新 testResult.value 的值。 */
  testedFingerprint.value = '' /* 更新 testedFingerprint.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const result = await api('/api/v1/ai/providers/test', { method:'POST', body:JSON.stringify(candidate.body) }) /* 声明 result。 */
    testResult.value = { ...result, fingerprint:candidate.fingerprint } /* 更新 testResult.value 的值。 */
    if (!result.success) { /* 判断条件并选择处理分支。 */
      providerError.value = result.error || '模型服务测试失败，请检查地址、模型和接口密钥' /* 更新 providerError.value 的值。 */
      return /* 返回当前处理结果。 */
    } /* 结束当前表达式或代码块。 */
    testedFingerprint.value = candidate.fingerprint /* 更新 testedFingerprint.value 的值。 */
    UiMessage.success('配置测试通过。确认无误后可点击“应用配置”') /* 执行当前语句并推进处理流程。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    providerError.value = error.message || '模型服务测试失败' /* 更新 providerError.value 的值。 */
  } finally { /* 结束当前表达式或代码块。 */
    testing.value = false /* 更新 testing.value 的值。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

async function applyProviderConfig() { /* 定义 applyProviderConfig 函数。 */
  if (!isAdmin.value || busy.value) return /* 判断条件并选择处理分支。 */
  const candidate = candidateConfig() /* 声明 candidate。 */
  if (!candidate) return /* 判断条件并选择处理分支。 */
  if (!canApply.value || testedFingerprint.value !== candidate.fingerprint) { /* 判断条件并选择处理分支。 */
    providerError.value = '请先测试当前配置；修改任一配置后需要重新测试' /* 更新 providerError.value 的值。 */
    return /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
  applying.value = true /* 更新 applying.value 的值。 */
  providerError.value = '' /* 更新 providerError.value 的值。 */
  try { /* 执行当前语句并推进处理流程。 */
    const result = await api('/api/v1/ai/providers/config', { method:'PUT', body:JSON.stringify(candidate.body) }) /* 声明 result。 */
    syncProviderForm({ config:result }) /* 执行当前语句并推进处理流程。 */
    await loadRuntime() /* 等待异步操作完成。 */
    testResult.value = null /* 更新 testResult.value 的值。 */
    testedFingerprint.value = '' /* 更新 testedFingerprint.value 的值。 */
    UiMessage.success(`已应用${providerLabel(candidate.body.provider)}，所有智能功能立即生效`) /* 执行当前语句并推进处理流程。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    providerError.value = error.message || '模型服务应用失败' /* 更新 providerError.value 的值。 */
  } finally { /* 结束当前表达式或代码块。 */
    applying.value = false /* 更新 applying.value 的值。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

watch(candidateFingerprint, () => { /* 执行当前语句并推进处理流程。 */
  if (testResult.value && testedFingerprint.value !== candidateFingerprint.value) { /* 判断条件并选择处理分支。 */
    testResult.value = null /* 更新 testResult.value 的值。 */
    testedFingerprint.value = '' /* 更新 testedFingerprint.value 的值。 */
  } /* 结束当前表达式或代码块。 */
}) /* 结束当前表达式或代码块。 */

onMounted(loadRuntime) /* 执行当前语句并推进处理流程。 */
</script>

<template>
  <div class="ai-management-page">
    <ui-card shadow="never" class="surface-card ai-management-hero" v-loading="loading">
      <div class="ai-management-hero-grid">
        <div class="ai-management-hero-copy"><span class="section-kicker">智能模型管理</span><h3>连接并启用模型服务</h3><p>选择模型来源，填写连接信息；测试通过后应用到平台智能功能。</p></div>
        <div class="ai-active-provider">
          <div class="ai-active-provider-heading"><span>当前生效配置</span><ui-tag :type="activeStatusType" effect="light">{{ activeStatusLabel }}</ui-tag></div>
          <div class="ai-active-provider-main"><strong>{{ activeProviderName }}</strong><span>{{ activeModel }}</span></div>
          <small v-if="runtime.config?.baseUrl">{{ runtime.config.baseUrl }}</small>
          <div class="ai-active-provider-footer"><span>{{ activeProvider === 'disabled' ? '尚未配置模型' : `${runtime.config?.maxTokens || 2048} 词元上限` }}</span><ui-button size="small" plain :loading="loading" @click="loadRuntime">刷新状态</ui-button></div>
        </div>
      </div>
    </ui-card>

    <ui-alert v-if="loadError" :title="loadError" type="error" :closable="false" show-icon><ui-button plain size="small" @click="loadRuntime">重新加载</ui-button></ui-alert>

    <div class="ai-management-grid">
      <ui-card shadow="never" class="surface-card ai-provider-config">
        <template #header><div class="card-header"><div><strong>模型服务配置</strong><small>选择服务，测试连接，确认后启用</small></div><ui-tag v-if="isAdmin" effect="plain">管理员</ui-tag></div></template>
        <template v-if="isAdmin">
          <ui-form label-position="top" :model="providerForm" :disabled="busy">
            <section class="config-section">
              <div class="config-section-heading"><span>01</span><div><strong>选择模型来源</strong><small>决定使用本地服务还是云端接口</small></div></div>
              <ui-form-item label="模型来源"><ui-select v-model="providerForm.provider" class="provider-select" @change="providerChanged"><ui-option v-for="item in providerOptions" :key="item.id" :label="item.label" :value="item.id" /></ui-select></ui-form-item>
              <p class="provider-description">{{ selectedProviderOption.description }}</p>
            </section>
            <section class="config-section">
              <div class="config-section-heading"><span>02</span><div><strong>填写连接信息</strong><small>地址必须能从平台服务器访问</small></div></div>
              <ui-form-item label="服务地址"><ui-input v-model="providerForm.baseUrl" placeholder="例如 http://ollama:11434 或 https://api.deepseek.com" /></ui-form-item>
              <p v-if="providerForm.provider === 'ollama'" class="provider-field-hint">Docker 内置 Ollama 通常使用 http://ollama:11434。</p>
              <template v-if="providerForm.provider !== 'ollama'"><ui-form-item class="cloud-key-field" label="接口密钥"><ui-input v-model="providerForm.apiKey" type="password" show-password autocomplete="off" placeholder="填写 API Key；留空沿用已保存的密钥" /></ui-form-item><p v-if="runtime.config?.apiKeyConfigured && providerForm.provider === activeProvider" class="provider-field-hint">已保存密钥 {{ runtime.config.apiKeyHint || '***' }}，留空测试或应用会继续使用。</p></template>
            </section>
            <section class="config-section">
              <div class="config-section-heading"><span>03</span><div><strong>设置模型与输出</strong><small>选择实际可用的模型，设置助手回复长度</small></div></div>
              <div class="config-field-grid"><ui-form-item label="模型名称"><ui-input v-model="providerForm.model" placeholder="例如 qwen3:1.7b" /></ui-form-item><div><ui-form-item label="最大输出词元"><ui-input-number v-model="providerForm.maxTokens" :min="128" :max="8192" :step="128" controls-position="right" /></ui-form-item><p class="provider-field-hint">智能助手单次回复上限，范围 128–8192。</p></div></div>
            </section>
            <div class="provider-actions"><span :class="{ ready:canApply }">{{ canApply ? '测试通过，点击应用后生效' : '请先测试当前填写的配置' }}</span><div><ui-button v-permission="'POST /api/v1/ai/providers/test'" plain :loading="testing" @click="testProviderConfig">测试配置</ui-button><ui-button v-permission="'PUT /api/v1/ai/providers/config'" type="primary" :loading="applying" :disabled="!canApply" @click="applyProviderConfig">应用配置</ui-button></div></div>
          </ui-form>
          <ui-alert v-if="providerError" class="provider-error" :title="providerError" type="error" :closable="false" show-icon />
          <div v-if="testResult" class="provider-test-result" :class="{ success:testResult.success, failed:!testResult.success }"><div><strong>{{ testResult.success ? '配置测试通过' : '配置测试失败' }}</strong><span v-if="testResult.latencyMs">耗时 {{ testResult.latencyMs }} 毫秒</span></div><p v-if="testResult.answer">{{ testResult.answer }}</p><small v-if="testResult.success">测试只验证连接，点击“应用配置”后才会生效。</small></div>
        </template>
        <div v-else class="provider-viewer-summary"><ui-alert title="当前账号可查看模型状态，配置修改仅限管理员。" type="info" :closable="false" show-icon /></div>
      </ui-card>

      <ui-card shadow="never" class="surface-card ai-capability-card">
        <template #header><div class="capability-card-title"><strong>智能业务能力</strong><small>应用配置后共同使用当前模型</small></div></template>
        <div class="ai-capability-list"><div v-for="item in capabilities" :key="item.title" class="ai-capability-item"><div><strong>{{ item.title }}</strong><p>{{ item.description }}</p></div><ui-button size="small" plain @click="emit('navigate', item.page)">{{ item.label }}</ui-button></div></div>
      </ui-card>
    </div>

    <ui-card shadow="never" class="surface-card ai-provider-list"><ui-collapse><ui-collapse-item title="可用模型服务 · 查看来源与支持能力" name="providers"><ui-table v-loading="loading" :data="runtime.items || []" stripe>
      <ui-table-column label="模型服务" min-width="190"><template #default="{row}"><div class="provider-name"><strong>{{ providerLabel(row.id) }}</strong><ui-tag v-if="row.id === activeProvider" size="small" type="success" effect="plain">使用中</ui-tag></div></template></ui-table-column>
      <ui-table-column label="说明" min-width="270"><template #default="{row}">{{ providerDescription(row) }}</template></ui-table-column>
      <ui-table-column label="默认模型" min-width="150"><template #default="{row}">{{ row.defaultModel || '由配置决定' }}</template></ui-table-column>
      <ui-table-column label="支持能力" min-width="240"><template #default="{row}"><div class="provider-capabilities"><ui-tag v-for="capability in (row.capabilities || [])" :key="capability" size="small" effect="plain">{{ capabilityLabel(capability) }}</ui-tag></div></template></ui-table-column>
    </ui-table></ui-collapse-item></ui-collapse></ui-card>
  </div>
</template>

<style scoped>
.ai-management-page { display:grid; gap:16px; min-width:0; }
.ai-management-hero { overflow:hidden; }
.ai-management-hero-grid { display:grid; grid-template-columns:minmax(0,1fr) minmax(280px,390px); gap:24px; align-items:center; }
.ai-management-hero-copy { display:grid; align-content:center; gap:6px; }
.ai-management-hero-copy h3 { margin:0; color:var(--text-strong); font-size:22px; letter-spacing:-.02em; }
.ai-management-hero-copy p { margin:0; color:var(--accent-foreground); font-size:13px; line-height:1.65; }
.ai-active-provider { min-width:0; display:grid; gap:7px; padding:15px 17px; background:var(--surface-subtle); border:1px solid var(--border); border-radius:9px; }
.ai-active-provider-heading,.ai-active-provider-footer { display:flex; align-items:center; justify-content:space-between; gap:10px; color:var(--accent-foreground); font-size:12px; }
.ai-active-provider-main { min-width:0; display:flex; align-items:baseline; gap:10px; }
.ai-active-provider-main strong { color:var(--primary); font-size:17px; }
.ai-active-provider-main span { overflow:hidden; color:var(--accent-foreground); font-size:13px; text-overflow:ellipsis; white-space:nowrap; }
.ai-active-provider small { overflow:hidden; color:var(--accent-foreground); font-size:12px; text-overflow:ellipsis; white-space:nowrap; }
.ai-active-provider-footer { padding-top:3px; }
.ai-management-grid { display:grid; grid-template-columns:minmax(0,1fr) minmax(280px,330px); gap:16px; align-items:start; }
.ai-provider-config,.ai-capability-card { min-width:0; }
.ai-provider-config :deep(.n-form-item) { margin-bottom:0; min-width:0; }
.ai-provider-config :deep(.n-select),.ai-provider-config :deep(.n-input),.ai-provider-config :deep(.n-input-number) { width:100%; min-width:0; }
.config-section { min-width:0; padding:3px 0 17px; margin-bottom:17px; border-bottom:1px solid var(--border); }
.config-section-heading { display:flex; align-items:start; gap:11px; margin-bottom:14px; }
.config-section-heading>span { flex:none; display:grid; place-items:center; width:27px; height:27px; color:var(--primary); background:var(--surface-subtle); border-radius:7px; font-size:12px; font-weight:700; }
.config-section-heading>div { display:grid; gap:2px; }
.config-section-heading strong { color:var(--accent-foreground); font-size:14px; }
.config-section-heading small { color:var(--accent-foreground); font-size:12px; line-height:1.5; }
.provider-description,.provider-field-hint { margin:7px 0 0; color:var(--accent-foreground); font-size:12px; line-height:1.6; }
.config-field-grid { display:grid; grid-template-columns:minmax(0,1fr) minmax(0,1fr); gap:16px; align-items:start; }
.cloud-key-field { margin-top:13px; }
.provider-actions { display:flex; align-items:center; justify-content:space-between; gap:14px; }
.provider-actions>span { color:var(--accent-foreground); font-size:12px; line-height:1.5; }
.provider-actions>span.ready { color:var(--success-foreground); }
.provider-actions>div { flex:none; display:flex; gap:8px; }
.provider-actions :deep(.n-button) { min-width:94px; }
.provider-error { margin-top:14px; }
.provider-test-result { margin-top:14px; padding:12px 14px; border:1px solid; border-radius:8px; }
.provider-test-result.success { background:var(--surface-subtle); border-color:var(--border-success); }
.provider-test-result.failed { background:var(--surface-subtle); border-color:var(--border-danger); }
.provider-test-result>div { display:flex; justify-content:space-between; align-items:center; gap:8px; }
.provider-test-result p { margin:8px 0; color:var(--foreground); font-size:12px; line-height:1.6; white-space:pre-wrap; }
.provider-test-result span,.provider-test-result small { color:var(--accent-foreground); font-size:12px; }
.provider-test-result small { display:block; line-height:1.5; }
.ai-capability-list { display:grid; }
.capability-card-title { display:grid; gap:3px; }
.capability-card-title small { color:var(--accent-foreground); font-size:12px; line-height:1.5; }
.ai-capability-item { display:grid; grid-template-columns:minmax(0,1fr) auto; gap:8px; align-items:center; padding:13px 0; border-bottom:1px solid var(--border); }
.ai-capability-item:last-child { border-bottom:0; }
.ai-capability-item strong { color:var(--foreground); font-size:13px; }
.ai-capability-item p { margin:3px 0 0; color:var(--accent-foreground); font-size:12px; line-height:1.5; }
.ai-provider-list :deep(.n-collapse-item__header) { font-weight:600; }
.provider-name { display:flex; align-items:center; gap:6px; }
.provider-capabilities { display:flex; flex-wrap:wrap; gap:4px; }
@media (max-width:980px) { .ai-management-hero-grid,.ai-management-grid { grid-template-columns:1fr; } }
@media (max-width:560px) { .ai-management-hero-copy h3 { font-size:18px; }.config-field-grid { grid-template-columns:1fr; gap:13px; }.provider-actions { flex-direction:column; align-items:stretch; }.provider-actions>div { width:100%; }.provider-actions :deep(.n-button) { flex:1; min-width:0; }.ai-capability-item { grid-template-columns:minmax(0,1fr); }.ai-capability-item :deep(.n-button) { justify-self:start; } }
</style>
