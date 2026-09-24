<script setup>
import { can } from '../permissions' /* 引入当前代码需要的依赖。 */
import { aiProviderOptions as providerOptions } from '../presentation' /* 引入当前代码需要的依赖。 */
import { computed, onMounted, reactive, ref, watch } from 'vue' /* 引入当前代码需要的依赖。 */
import { UiMessage } from '../ui/feedback.js' /* 引入当前代码需要的依赖。 */
import { api, session } from '../api' /* 引入当前代码需要的依赖。 */

const emit = defineEmits(['navigate']) /* 声明 emit。 */

const runtime = ref({ items:[], active:{ id:'disabled', name:'未启用', enabled:false }, config:null, healthy:false, healthMessage:'正在读取模型服务状态' }) /* 声明 runtime。 */
const loading = ref(false) /* 声明 loading。 */
const testing = ref(false) /* 声明 testing。 */
const applying = ref(false) /* 声明 applying。 */
const loadError = ref('') /* 声明 loadError。 */
const providerError = ref('') /* 声明 providerError。 */
const testResult = ref(null) /* 声明 testResult。 */
const testedFingerprint = ref('') /* 声明 testedFingerprint。 */
const providerForm = reactive({ provider:'ollama', baseUrl:'http://localhost:11434', model:'qwen3:1.7b', apiKey:'' }) /* 声明 providerForm。 */
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
const candidateFingerprint = computed(() => [providerForm.provider.trim(), providerForm.baseUrl.trim(), providerForm.model.trim(), providerForm.apiKey.trim()].join('\u0000')) /* 声明 candidateFingerprint。 */
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
  if (!provider || !baseUrl || !model) { /* 判断条件并选择处理分支。 */
    providerError.value = '请填写模型来源、服务地址和模型名称' /* 更新 providerError.value 的值。 */
    return null /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
  if (provider !== 'ollama' && !apiKey && activeProvider.value !== provider) { /* 判断条件并选择处理分支。 */
    providerError.value = '切换到云端或兼容接口模型时必须填写接口密钥' /* 更新 providerError.value 的值。 */
    return null /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
  const body = { provider, baseUrl, model } /* 声明 body。 */
  if (apiKey) body.apiKey = apiKey /* 判断条件并选择处理分支。 */
  return { body, fingerprint: [provider, baseUrl, model, apiKey].join('\u0000') } /* 返回当前处理结果。 */
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
    providerError.value = '请先测试当前配置；修改地址、模型或接口密钥后需要重新测试' /* 更新 providerError.value 的值。 */
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
  <div class="ai-management-page"> <!-- 渲染 div 界面元素。 -->
    <ui-card shadow="never" class="surface-card ai-management-hero" v-loading="loading"> <!-- 渲染 ui-card 界面元素。 -->
      <div class="ai-management-hero-grid"> <!-- 渲染 div 界面元素。 -->
        <div class="ai-management-hero-copy"><span class="section-kicker">智能模型管理</span><h3>统一管理智能模型与业务能力</h3><p>在这里选择模型服务和活动模型。应用后，告警研判、工作流、巡检、规则草稿及协议助手会共用新配置。</p></div> <!-- 渲染 div 界面元素。 -->
        <div class="ai-active-provider"><div class="ai-active-provider-heading"><span>当前活动模型服务</span><ui-tag :type="activeStatusType" effect="light">{{ activeStatusLabel }}</ui-tag></div><strong>{{ activeProviderName }}</strong><small>{{ activeModel }} · {{ runtime.config?.apiKeyConfigured ? '接口密钥已配置' : '无需接口密钥' }}</small><small v-if="runtime.config?.baseUrl">{{ runtime.config.baseUrl }}</small></div> <!-- 渲染 div 界面元素。 -->
      </div> <!-- 结束当前界面区域。 -->
    </ui-card> <!-- 结束当前界面区域。 -->

    <ui-alert v-if="loadError" :title="loadError" type="error" :closable="false" show-icon><ui-button plain size="small" @click="loadRuntime">重新加载</ui-button></ui-alert> <!-- 渲染 ui-alert 界面元素。 -->

    <div class="ai-management-grid"> <!-- 渲染 div 界面元素。 -->
      <ui-card shadow="never" class="surface-card ai-provider-config"> <!-- 渲染 ui-card 界面元素。 -->
        <template #header><div class="card-header"><div><strong>模型服务配置</strong><small>先测试当前填写内容，再选择是否应用到全部智能功能</small></div><ui-tag effect="plain">管理员</ui-tag></div></template>
        <template v-if="isAdmin">
          <ui-form label-position="top" :model="providerForm" :disabled="busy"> <!-- 渲染 ui-form 界面元素。 -->
            <ui-form-item label="模型来源"><ui-select v-model="providerForm.provider" class="provider-select" @change="providerChanged"><ui-option v-for="item in providerOptions" :key="item.id" :label="item.label" :value="item.id" /></ui-select></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
            <p class="provider-description">{{ selectedProviderOption.description }}</p> <!-- 渲染 p 界面元素。 -->
            <ui-form-item label="服务地址"><ui-input v-model="providerForm.baseUrl" placeholder="填写模型服务的 HTTP/HTTPS 地址，无需配置白名单" /></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
            <ui-form-item label="模型名称"><ui-input v-model="providerForm.model" placeholder="例如 qwen3:1.7b" /></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
            <ui-form-item v-if="providerForm.provider !== 'ollama'" label="接口密钥"><ui-input v-model="providerForm.apiKey" type="password" show-password autocomplete="off" placeholder="留空表示沿用当前密钥" /></ui-form-item> <!-- 渲染 ui-form-item 界面元素。 -->
            <div class="provider-actions"><ui-button v-permission="'POST /api/v1/ai/providers/test'" plain :loading="testing" @click="testProviderConfig">测试配置</ui-button><ui-button v-permission="'PUT /api/v1/ai/providers/config'" type="primary" :loading="applying" :disabled="!canApply" @click="applyProviderConfig">应用配置</ui-button></div> <!-- 渲染 div 界面元素。 -->
          </ui-form> <!-- 结束当前界面区域。 -->
          <ui-alert v-if="providerError" class="provider-error" :title="providerError" type="error" :closable="false" show-icon /> <!-- 渲染 ui-alert 界面元素。 -->
          <div v-if="testResult" class="provider-test-result" :class="{ success:testResult.success, failed:!testResult.success }"><div><strong>{{ testResult.success ? '配置测试通过' : '配置测试失败' }}</strong><span v-if="testResult.latencyMs">耗时 {{ testResult.latencyMs }} 毫秒</span></div><p v-if="testResult.answer">{{ testResult.answer }}</p><small v-if="testResult.success">当前填写内容未生效；确认无误后点击“应用配置”。</small></div> <!-- 渲染 div 界面元素。 -->
          <small v-if="runtime.config?.apiKeyConfigured && providerForm.provider !== 'ollama'" class="provider-key-hint">当前已保存接口密钥：{{ runtime.config.apiKeyHint || '已配置' }}；留空测试或应用会继续使用它。</small> <!-- 渲染 small 界面元素。 -->
        </template>
        <div v-else class="provider-viewer-summary"><ui-alert title="模型服务配置仅限管理员修改。" type="info" :closable="false" show-icon /><strong>{{ activeProviderName }}</strong><span>{{ activeModel }} · {{ runtime.config?.apiKeyConfigured ? '接口密钥已配置' : '无需接口密钥' }}</span></div>
      </ui-card>

      <ui-card shadow="never" class="surface-card ai-capability-card">
        <template #header><div class="card-header"><div><strong>智能业务能力</strong><small>所有能力跟随当前活动模型服务</small></div><ui-tag type="success" effect="plain">{{ runtime.healthy ? '可用' : '待检查' }}</ui-tag></div></template>
        <div class="ai-capability-list"> <!-- 渲染 div 界面元素。 -->
          <div v-for="item in capabilities" :key="item.title" class="ai-capability-item"><span class="ai-capability-dot" :class="{ online:runtime.healthy }" /><div><strong>{{ item.title }}</strong><p>{{ item.description }}</p></div><ui-button size="small" plain @click="emit('navigate', item.page)">{{ item.label }}</ui-button></div> <!-- 渲染 div 界面元素。 -->
        </div> <!-- 结束当前界面区域。 -->
      </ui-card> <!-- 结束当前界面区域。 -->
    </div> <!-- 结束当前界面区域。 -->

    <ui-card shadow="never" class="surface-card ai-provider-list"> <!-- 渲染 ui-card 界面元素。 -->
      <template #header><div class="card-header"><div><strong>可用模型服务</strong><small>当前模型服务会显示“使用中”</small></div><ui-button size="small" :loading="loading" @click="loadRuntime">刷新状态</ui-button></div></template>
      <ui-table v-loading="loading" :data="runtime.items || []" stripe> <!-- 渲染 ui-table 界面元素。 -->
        <ui-table-column label="模型服务" min-width="190"><template #default="{row}"><div class="provider-name"><strong>{{ providerLabel(row.id) }}</strong><ui-tag v-if="row.id === activeProvider" size="small" type="success" effect="plain">使用中</ui-tag></div></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
        <ui-table-column label="说明" min-width="270"><template #default="{row}">{{ providerDescription(row) }}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
        <ui-table-column label="默认模型" min-width="150"><template #default="{row}">{{ row.defaultModel || '由配置决定' }}</template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
        <ui-table-column label="支持能力" min-width="240"><template #default="{row}"><div class="provider-capabilities"><ui-tag v-for="capability in (row.capabilities || [])" :key="capability" size="small" effect="plain">{{ capabilityLabel(capability) }}</ui-tag></div></template></ui-table-column> <!-- 渲染 ui-table-column 界面元素。 -->
      </ui-table> <!-- 结束当前界面区域。 -->
    </ui-card> <!-- 结束当前界面区域。 -->

    <ui-alert class="ai-management-note" title="配置说明" type="info" :closable="false" show-icon>管理员可直接填写平台服务器能够访问的 HTTP/HTTPS 模型服务地址，无需配置白名单。Docker 内置 Ollama 使用 http://ollama:11434，其他模型服务填写实际地址；Ollama 误填 /v1 时平台会自动归一化。地址填写纯文本，接口密钥单独填写，页面不会显示完整密钥。</ui-alert> <!-- 渲染 ui-alert 界面元素。 -->
  </div> <!-- 结束当前界面区域。 -->
</template>

<style scoped>
.ai-management-page { display:grid; gap:16px; } /* 定义当前元素的样式规则。 */
.ai-management-hero { overflow:hidden; } /* 定义当前元素的样式规则。 */
.ai-management-hero-grid { display:grid; grid-template-columns:minmax(0,1.5fr) minmax(260px,1fr); gap:18px; align-items:stretch; } /* 定义当前元素的样式规则。 */
.ai-management-hero-copy { display:grid; align-content:center; gap:7px; padding:7px 2px; } /* 定义当前元素的样式规则。 */
.ai-management-hero-copy h3 { margin:0; color:#1f2329; font-size:22px; letter-spacing:-.02em; } /* 定义当前元素的样式规则。 */
.ai-management-hero-copy p { max-width:720px; margin:0; color:#646c73; font-size:13px; line-height:1.8; } /* 定义当前元素的样式规则。 */
.ai-active-provider { display:grid; align-content:center; gap:5px; padding:16px; background:#f5f7fa; border-radius:.625rem; } /* 定义当前元素的样式规则。 */
.ai-active-provider-heading { display:flex; align-items:center; justify-content:space-between; gap:10px; color:#52657d; font-size:12px; } /* 浅底状态卡的辅助文字保持足够对比。 */
.ai-active-provider strong { color:#1554ad; font-size:16px; } /* 定义当前元素的样式规则。 */
.ai-active-provider small { color:#52657d; font-size:12px; line-height:1.5; word-break:break-all; } /* 浅底状态卡的辅助文字保持足够对比。 */
.ai-management-grid { display:grid; grid-template-columns:minmax(300px,420px) minmax(0,1fr); gap:16px; align-items:start; } /* 定义当前元素的样式规则。 */
.ai-provider-config :deep(.el-form-item) { margin-bottom:12px; } /* 定义当前元素的样式规则。 */
.ai-provider-config :deep(.el-select) { width:100%; } /* 定义当前元素的样式规则。 */
.provider-description { margin:-4px 0 11px; color:#64748b; font-size:12px; line-height:1.5; } /* 定义当前元素的样式规则。 */
.provider-actions { display:flex; justify-content:flex-end; gap:8px; margin-top:2px; } /* 定义当前元素的样式规则。 */
.provider-actions .el-button { min-width:104px; } /* 定义当前元素的样式规则。 */
.provider-error { margin-top:10px; } /* 定义当前元素的样式规则。 */
.provider-test-result { margin-top:14px; padding:11px; border:1px solid; border-radius:4px; } /* 定义当前元素的样式规则。 */
.provider-test-result.success { background:#f6ffed; border-color:#b7eb8f; } /* 定义当前元素的样式规则。 */
.provider-test-result.failed { background:#fff2f0; border-color:#ffccc7; } /* 定义当前元素的样式规则。 */
.provider-test-result>div { display:flex; justify-content:space-between; align-items:center; gap:8px; } /* 定义当前元素的样式规则。 */
.provider-test-result p { margin:8px 0; color:#3d3d3d; font-size:12px; line-height:1.6; white-space:pre-wrap; } /* 定义当前元素的样式规则。 */
.provider-test-result span,.provider-test-result small { color:var(--muted-foreground); font-size:12px; } /* 定义当前元素的样式规则。 */
.provider-test-result small { display:block; line-height:1.5; } /* 定义当前元素的样式规则。 */
.provider-key-hint { display:block; margin-top:9px; color:#64748b; font-size:12px; line-height:1.5; } /* 定义当前元素的样式规则。 */
.provider-viewer-summary { display:grid; gap:10px; } /* 定义当前元素的样式规则。 */
.provider-viewer-summary strong { color:#1554ad; font-size:16px; } /* 定义当前元素的样式规则。 */
.provider-viewer-summary span { color:#64748b; font-size:12px; } /* 定义当前元素的样式规则。 */
.ai-capability-list { display:grid; gap:7px; } /* 定义当前元素的样式规则。 */
.ai-capability-item { display:grid; grid-template-columns:auto minmax(0,1fr) auto; gap:10px; align-items:center; padding:10px 0; border-bottom:1px solid #f0f0f0; } /* 定义当前元素的样式规则。 */
.ai-capability-item:last-child { border-bottom:0; } /* 定义当前元素的样式规则。 */
.ai-capability-dot { width:8px; height:8px; background:#cbd5e1; border-radius:50%; } /* 定义当前元素的样式规则。 */
.ai-capability-dot.online { background:#52c41a; box-shadow:0 0 0 3px #f6ffed; } /* 定义当前元素的样式规则。 */
.ai-capability-item strong { color:#303133; font-size:13px; } /* 定义当前元素的样式规则。 */
.ai-capability-item p { margin:3px 0 0; color:var(--muted-foreground); font-size:12px; line-height:1.5; } /* 能力说明在白底上保持可读。 */
.provider-name { display:flex; align-items:center; gap:6px; } /* 定义当前元素的样式规则。 */
.provider-name small { color:var(--muted-foreground); } /* 定义当前元素的样式规则。 */
.provider-capabilities { display:flex; flex-wrap:wrap; gap:4px; } /* 定义当前元素的样式规则。 */
.ai-management-note { margin-bottom:2px; } /* 定义当前元素的样式规则。 */
@media (max-width:860px) { .ai-management-hero-grid,.ai-management-grid { grid-template-columns:1fr; } } /* 按屏幕条件调整样式。 */
@media (max-width:560px) { .ai-capability-item { grid-template-columns:auto minmax(0,1fr); }.ai-capability-item .el-button { grid-column:2; justify-self:start; }.ai-management-hero-copy h3 { font-size:18px; } } /* 按屏幕条件调整样式。 */
</style>
