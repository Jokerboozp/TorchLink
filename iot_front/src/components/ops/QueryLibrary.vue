<script setup>
// 个人查询库：保存的查询与最近查询，按当前账户与租户隔离，由后端保存。
import { ref, watch } from 'vue'
import { Trash2 } from '@lucide/vue'
import { UiMessage, UiMessageBox } from '../../ui/feedback.js'
import { opsErrorText, opsGet, opsSend } from '../../ops/opsApi.js'
import { relativeTime } from '../../ops/format.js'

const props = defineProps({ modelValue: Boolean, languages: { type: Array, default: () => ['promql'] } })
const emit = defineEmits(['update:modelValue', 'use'])
const tab = ref('saved')
const saved = ref([])
const history = ref([])
const loading = ref(false)
const error = ref('')
const languageName = { promql: 'PromQL', logql: 'LogQL', logfilter: '日志筛选' }

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [s, h] = await Promise.all([opsGet('/api/v1/ops/preferences/saved-queries'), opsGet('/api/v1/ops/preferences/history')])
    saved.value = (s.items || []).filter(item => props.languages.includes(item.body.language))
    history.value = (h.items || []).filter(item => props.languages.includes(item.body.language))
  } catch (e) { error.value = opsErrorText(e) } finally { loading.value = false }
}
watch(() => props.modelValue, open => { if (open) load() })

function summary(item) {
  if (item.body.language !== 'logfilter') return item.body.query
  const f = item.body.filter || {}
  return [f.services?.length ? `服务：${f.services.join('、')}` : '全部服务', f.levels?.length ? `级别：${f.levels.join('、')}` : '', f.keyword ? `关键词：${f.keyword}` : ''].filter(Boolean).join('；')
}
function use(item) { emit('use', item.body); emit('update:modelValue', false) }
async function remove(item) {
  try {
    await UiMessageBox.confirm(`删除保存的查询“${item.name}”？`, '删除查询')
    await opsSend('DELETE', `/api/v1/ops/preferences/saved-queries/${encodeURIComponent(item.id)}`)
    UiMessage.success('已删除')
    load()
  } catch (e) { if (e !== 'cancel' && e !== 'close') UiMessage.error(opsErrorText(e)) }
}
async function clearHistory() {
  try {
    await UiMessageBox.confirm('清空最近查询记录？保存的查询不受影响。', '清空历史')
    await opsSend('DELETE', '/api/v1/ops/preferences/history')
    load()
  } catch (e) { if (e !== 'cancel' && e !== 'close') UiMessage.error(opsErrorText(e)) }
}
</script>

<template>
  <ui-drawer :model-value="modelValue" title="我的查询" size="min(520px, 94vw)" @update:model-value="value => emit('update:modelValue', value)">
    <ui-alert v-if="error" type="error" :title="error" :closable="false" />
    <ui-tabs v-model="tab">
      <ui-tab-pane name="saved" label="已保存">
        <ui-empty v-if="!saved.length" :description="loading ? '正在读取' : '还没有保存的查询'" :image-size="60" />
        <article v-for="item in saved" :key="item.id" class="query-item">
          <header><strong>{{ item.name }}</strong><ui-tag size="small">{{ languageName[item.body.language] }}</ui-tag></header>
          <code>{{ summary(item) }}</code>
          <p v-if="item.body.description">{{ item.body.description }}</p>
          <footer><ui-button size="small" type="primary" plain @click="use(item)">使用</ui-button><ui-button size="small" text type="danger" @click="remove(item)"><Trash2 />删除</ui-button></footer>
        </article>
      </ui-tab-pane>
      <ui-tab-pane name="history" label="最近查询">
        <div class="history-actions"><span>最多保留最近 100 条</span><ui-button v-if="history.length" size="small" text type="danger" @click="clearHistory">清空</ui-button></div>
        <ui-empty v-if="!history.length" :description="loading ? '正在读取' : '暂无查询记录'" :image-size="60" />
        <article v-for="item in history" :key="item.id" class="query-item query-item--compact" role="button" tabindex="0" @click="use(item)" @keydown.enter="use(item)">
          <header><ui-tag size="small">{{ languageName[item.body.language] }}</ui-tag><small>{{ relativeTime(item.updatedAt) }}</small></header>
          <code>{{ summary(item) }}</code>
        </article>
      </ui-tab-pane>
    </ui-tabs>
  </ui-drawer>
</template>

<style scoped>
.query-item { display: grid; gap: var(--space-2); padding: var(--space-3); margin-bottom: var(--space-2); border: 1px solid var(--border); border-radius: var(--radius-md); background: var(--surface); }
.query-item--compact { cursor: pointer; }
.query-item--compact:hover { border-color: var(--border-hover); background: var(--surface-muted); }
.query-item header, .query-item footer, .history-actions { display: flex; align-items: center; justify-content: space-between; gap: var(--space-2); }
.query-item code { display: block; padding: var(--space-2); overflow-x: auto; color: var(--code-inline-text); background: var(--code-inline-bg); border-radius: var(--radius-sm); font: var(--font-size-xs) var(--font-mono); white-space: pre-wrap; word-break: break-all; }
.query-item p, .query-item small, .history-actions span { margin: 0; color: var(--text-muted); font-size: var(--font-size-xs); }
.history-actions { margin-bottom: var(--space-2); }
</style>
