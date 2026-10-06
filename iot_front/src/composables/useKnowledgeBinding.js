import { computed, ref } from 'vue'
import { api, isAbort } from '../api'
import { can } from '../permissions'
import { UiMessage } from '../ui/feedback.js'
import { agentKey, agentName } from '../knowledge'
import { useListLoader } from './useListLoader'

// 智能体知识库策略：读取、保存与检索测试。状态由页面持有，切换页签后保留未保存的修改与测试结果。
export function useKnowledgeBinding(agents) {
  const bindingLoading = ref(false)
  const bindingSaving = ref(false)
  const bindingError = ref('')
  const bindingLoader = useListLoader(bindingLoading)
  const loadedBindingWorkflowId = ref('')
  const bindingWorkflowId = ref('')
  const knowledgeBinding = ref({ retrievalMode: 'always', topK: 5, minScore: 0.25, noMatchPolicy: 'allow-model' })
  const canManageBinding = computed(() => can('PUT /api/v1/ai/workflows/:id/knowledge-binding'))
  // 检索测试：用当前表单的召回数量和相似度试检索一次，不保存策略也不调用对话模型。
  const testQuestion = ref('')
  const testing = ref(false)
  const testResult = ref(null)
  const testError = ref('')
  const canTestBinding = computed(() => can('POST /api/v1/ai/workflows/:id/knowledge-binding/test'))
  const selectedBindingAgent = computed(() => agents.value.find(item => agentKey(item) === bindingWorkflowId.value))

  async function loadBinding() {
    if (!bindingWorkflowId.value) return
    const workflow = bindingWorkflowId.value
    bindingError.value = ''
    try {
      const value = await bindingLoader.run(signal =>
        api(`/api/v1/ai/workflows/${encodeURIComponent(workflow)}/knowledge-binding`, { signal })
      )
      if (bindingWorkflowId.value !== workflow) return
      knowledgeBinding.value = {
        retrievalMode: value.retrievalMode || 'always',
        topK: Number(value.topK) || 5,
        minScore: Number(value.minScore ?? 0.25),
        noMatchPolicy: value.noMatchPolicy || 'allow-model'
      }
      loadedBindingWorkflowId.value = workflow
    } catch (error) {
      if (!isAbort(error)) bindingError.value = error.message || '知识库策略读取失败'
    }
  }

  // 智能体列表更新后：当前智能体不在列表中时改选第一个，尚未读取的策略随即读取。
  function syncAgents() {
    if (agents.value.length && !agents.value.some(item => agentKey(item) === bindingWorkflowId.value))
      bindingWorkflowId.value = agentKey(agents.value[0])
    if (bindingWorkflowId.value && loadedBindingWorkflowId.value !== bindingWorkflowId.value) void loadBinding()
  }

  function changeBindingAgent() {
    testResult.value = null
    testError.value = ''
    loadBinding()
  }

  async function testBinding() {
    const question = testQuestion.value.trim()
    if (!question || !bindingWorkflowId.value || testing.value) return
    const workflow = bindingWorkflowId.value
    testing.value = true
    testError.value = ''
    try {
      const value = await api(`/api/v1/ai/workflows/${encodeURIComponent(workflow)}/knowledge-binding/test`, {
        method: 'POST',
        body: JSON.stringify({ question, topK: knowledgeBinding.value.topK, minScore: knowledgeBinding.value.minScore })
      })
      if (bindingWorkflowId.value === workflow) testResult.value = { ...value, items: Array.isArray(value.items) ? value.items : [] }
    } catch (error) {
      testError.value = error.message || '检索测试失败'
    } finally {
      testing.value = false
    }
  }

  async function saveBinding() {
    if (!bindingWorkflowId.value || !canManageBinding.value) return
    bindingSaving.value = true
    bindingError.value = ''
    try {
      const value = await api(`/api/v1/ai/workflows/${encodeURIComponent(bindingWorkflowId.value)}/knowledge-binding`, {
        method: 'PUT',
        body: JSON.stringify(knowledgeBinding.value)
      })
      knowledgeBinding.value = { ...knowledgeBinding.value, ...value }
      UiMessage.success(`已保存 ${agentName(selectedBindingAgent.value)} 的知识库策略`)
    } catch (error) {
      bindingError.value = error.message || '知识库策略保存失败'
    } finally {
      bindingSaving.value = false
    }
  }

  return {
    bindingLoading,
    bindingSaving,
    bindingError,
    bindingWorkflowId,
    knowledgeBinding,
    canManageBinding,
    testQuestion,
    testing,
    testResult,
    testError,
    canTestBinding,
    loadBinding,
    syncAgents,
    changeBindingAgent,
    testBinding,
    saveBinding
  }
}
