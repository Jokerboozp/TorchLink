<script setup>
// 知识库策略：按智能体设置检索模式、匹配要求与无匹配时的处理，并可试检索。
// 状态由页面的 useKnowledgeBinding 持有，切换页签不会丢失未保存的修改。
import { agentKey, agentName } from '../../knowledge'

const props = defineProps({
  // useKnowledgeBinding 的返回值。
  binding: { type: Object, required: true },
  agents: { type: Array, default: () => [] },
  // 当前页的文档，用于显示归属所选智能体的数量。
  documents: { type: Array, default: () => [] }
})
const {
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
  changeBindingAgent,
  testBinding,
  saveBinding
} = props.binding
</script>

<template>
  <section class="knowledge-panel policy-panel" aria-label="知识库策略">
    <div class="knowledge-panel-heading">
      <div>
        <h2>知识库策略</h2>
        <p>每个智能体只检索属于自己的文档，按需调整回答时的检索规则。</p>
      </div>
      <ui-button v-if="agents.length" :loading="bindingLoading" @click="loadBinding">刷新策略</ui-button>
    </div>
    <ui-empty v-if="!agents.length" description="暂无可配置的智能体；上传文档时仍可输入智能体标识。" />
    <template v-else>
      <ui-alert v-if="bindingError" :title="bindingError" type="warning" :closable="false" show-icon />
      <div class="knowledge-policy-target">
        <label for="knowledge-agent">当前智能体</label>
        <ui-select
          id="knowledge-agent"
          v-model="bindingWorkflowId"
          filterable
          :disabled="bindingSaving"
          placeholder="选择智能体"
          @change="changeBindingAgent"
          ><ui-option
            v-for="agent in agents"
            :key="agentKey(agent)"
            :label="agentName(agent) + ' · ' + agentKey(agent)"
            :value="agentKey(agent)"
        /></ui-select>
        <small>本页 {{ documents.filter(item => item.workflowId === bindingWorkflowId).length }} 份文档归属该智能体</small>
      </div>
      <ui-form
        v-loading="bindingLoading"
        class="knowledge-policy-form"
        label-position="top"
        :model="knowledgeBinding"
        :disabled="!canManageBinding || bindingLoading || bindingSaving"
      >
        <div class="knowledge-policy-section">
          <div class="knowledge-section-copy">
            <h3>何时检索</h3>
            <p>默认每次回答前检索本智能体的知识，召回片段作为模型 API 的参考依据。</p>
          </div>
          <ui-form-item label="检索模式"
            ><ui-radio-group v-model="knowledgeBinding.retrievalMode" class="segmented-choice-group" aria-label="检索模式"
              ><ui-radio-button value="auto">按需检索</ui-radio-button><ui-radio-button value="always">每次强制检索</ui-radio-button
              ><ui-radio-button value="disabled">禁用</ui-radio-button></ui-radio-group
            ></ui-form-item
          >
        </div>
        <div class="knowledge-policy-section">
          <div class="knowledge-section-copy">
            <h3>匹配要求</h3>
            <p>控制取回的片段数量，以及内容的最低相似度。</p>
          </div>
          <div class="knowledge-number-grid">
            <ui-form-item label="召回数量"
              ><ui-input-number v-model="knowledgeBinding.topK" :min="1" :max="20" controls-position="right" /></ui-form-item
            ><ui-form-item label="最低相似度"
              ><ui-input-number v-model="knowledgeBinding.minScore" :min="0" :max="1" :step="0.05" :precision="2" controls-position="right"
            /></ui-form-item>
          </div>
        </div>
        <div class="knowledge-policy-section">
          <div class="knowledge-section-copy">
            <h3>没有匹配时</h3>
            <p>明确缺少依据时，智能体是否还可以给出一般性回答。</p>
          </div>
          <ui-form-item label="无匹配知识时"
            ><ui-select v-model="knowledgeBinding.noMatchPolicy"
              ><ui-option label="允许模型回答，但必须说明证据不足" value="allow-model" /><ui-option
                label="阻止回答，必须先补充知识"
                value="require-evidence" /></ui-select
          ></ui-form-item>
        </div>
        <div class="knowledge-policy-actions">
          <small v-if="!canManageBinding">当前账号可查看策略；修改需要管理员或运维人员权限。</small
          ><ui-button
            v-permission="'PUT /api/v1/ai/workflows/:id/knowledge-binding'"
            type="primary"
            :loading="bindingSaving"
            :disabled="!canManageBinding || !bindingWorkflowId"
            @click="saveBinding"
            >保存知识库策略</ui-button
          >
        </div>
      </ui-form>
      <div v-if="canTestBinding" class="knowledge-policy-section knowledge-test">
        <div class="knowledge-section-copy">
          <h3>检索测试</h3>
          <p>按上方召回数量和最低相似度试检索一次，查看该智能体会引用哪些片段；不保存策略，也不调用对话模型。</p>
        </div>
        <div class="knowledge-test-body">
          <div class="knowledge-test-input">
            <ui-input
              v-model="testQuestion"
              maxlength="500"
              placeholder="例如：烟感持续报警如何处置"
              aria-label="测试问题"
              @keydown.enter.prevent="testBinding"
            /><ui-button :loading="testing" :disabled="!testQuestion.trim() || !bindingWorkflowId" @click="testBinding">试检索</ui-button>
          </div>
          <ui-alert v-if="testError" :title="testError" type="error" :closable="false" show-icon />
          <template v-else-if="testResult">
            <small class="knowledge-test-meta"
              >召回 {{ testResult.items.length }} 条 · 用时 {{ testResult.durationMs ?? 0 }} 毫秒{{
                testResult.keywordOnly ? ' · 向量服务不可用，仅按关键词匹配' : ''
              }}</small
            >
            <ol v-if="testResult.items.length" class="knowledge-test-hits">
              <li v-for="hit in testResult.items" :key="`${hit.documentId}-${hit.chunkIndex}`">
                <div>
                  <strong>{{ hit.filename || hit.documentId }}</strong
                  ><small>第 {{ Number(hit.chunkIndex) + 1 }} 段 · 相似度 {{ Number(hit.score).toFixed(3) }}</small>
                </div>
                <p>{{ hit.content }}</p>
              </li>
            </ol>
            <ui-empty v-else description="没有达到最低相似度的片段；可降低相似度或补充文档" :image-size="56" />
          </template>
        </div>
      </div>
    </template>
  </section>
</template>

<style scoped>
.knowledge-panel {
  min-width: 0;
  overflow: hidden;
  background: var(--surface);
  border-radius: var(--radius-lg);
  box-shadow: 0 1px 2px color-mix(in srgb, var(--primary) 5%, transparent);
}
.knowledge-panel-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 22px 24px 14px;
}
.knowledge-panel-heading h2 {
  margin: 0;
  font-size: 18px;
  line-height: 1.3;
}
.knowledge-panel-heading p {
  margin: 5px 0 0;
  color: var(--text-muted);
  font-size: 13px;
}
.policy-panel {
  padding-bottom: 8px;
}
.knowledge-policy-target {
  margin: 0 24px;
  padding: 16px 18px;
  display: grid;
  grid-template-columns: 130px minmax(0, 360px) 1fr;
  align-items: center;
  gap: 14px;
  background: var(--surface-muted);
  border-radius: 10px;
}
.knowledge-policy-target label {
  font-size: 13px;
  font-weight: 600;
}
.knowledge-policy-target small {
  color: var(--text-muted);
  font-size: 12px;
}
.knowledge-policy-form {
  padding: 6px 24px 16px;
}
.knowledge-policy-section.knowledge-test {
  margin: 0 24px;
  border-top: 1px solid var(--border);
  border-bottom: 0;
}
.knowledge-test-body {
  display: grid;
  gap: 12px;
  min-width: 0;
}
.knowledge-test-input {
  display: flex;
  gap: 8px;
}
.knowledge-test-meta {
  color: var(--text-muted);
  font-size: 12px;
}
.knowledge-test-hits {
  display: grid;
  gap: 10px;
  margin: 0;
  padding: 0;
  list-style: none;
}
.knowledge-test-hits li {
  padding: 10px 12px;
  background: var(--surface-muted);
  border: 1px solid var(--border);
  border-radius: 8px;
}
.knowledge-test-hits li > div {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 8px;
}
.knowledge-test-hits small {
  color: var(--text-muted);
  font-size: 12px;
}
.knowledge-test-hits p {
  margin: 6px 0 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  font-size: 13px;
  line-height: 1.6;
}
.knowledge-policy-section {
  padding: 20px 0;
  display: grid;
  grid-template-columns: minmax(170px, 0.8fr) minmax(0, 1.2fr);
  align-items: start;
  gap: 24px;
  border-bottom: 1px solid var(--border);
}
.knowledge-section-copy h3 {
  margin: 0;
  font-size: 15px;
}
.knowledge-section-copy p {
  max-width: 280px;
  margin: 5px 0 0;
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.6;
}
.knowledge-policy-section .ui-form-item {
  width: 100%;
  max-width: 430px;
  margin: 0;
}
.knowledge-policy-section :deep(.ui-radio-group),
.knowledge-policy-section :deep(.ui-select) {
  width: 100%;
}
.knowledge-policy-section :deep(.n-radio-button) {
  flex: 1;
}
.knowledge-policy-section :deep(.n-radio-button__label) {
  width: 100%;
  padding-inline: 8px;
}
.knowledge-number-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 16px;
  max-width: 430px;
}
.knowledge-number-grid :deep(.ui-input-number) {
  width: 100%;
}
.knowledge-policy-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 16px;
  padding-top: 20px;
}
.knowledge-policy-actions small {
  margin-right: auto;
  color: var(--text-muted);
  font-size: 12px;
}
@media (max-width: 900px) {
  .knowledge-policy-target {
    grid-template-columns: 120px minmax(0, 1fr);
  }
  .knowledge-policy-target small {
    grid-column: 2;
  }
}
@media (max-width: 640px) {
  .knowledge-panel-heading {
    padding: 18px 16px 10px;
  }
  .knowledge-panel-heading p {
    display: none;
  }
  .knowledge-policy-target {
    margin: 0 16px;
    grid-template-columns: 1fr;
    gap: 7px;
  }
  .knowledge-policy-target small {
    grid-column: 1;
  }
  .knowledge-policy-form {
    padding-inline: 16px;
  }
  .knowledge-policy-section {
    grid-template-columns: 1fr;
    gap: 12px;
  }
  .knowledge-section-copy p {
    max-width: none;
  }
  .knowledge-number-grid {
    gap: 8px;
  }
  .knowledge-policy-actions {
    flex-direction: column;
    align-items: stretch;
  }
}
</style>
