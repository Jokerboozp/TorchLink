<script setup>
// 导入仪表盘：内置模板、粘贴或上传 Grafana 导出的 JSON；先分析数据源映射与平台支持情况，再写入 Grafana。
import { computed, ref, watch } from 'vue'
import { Upload } from '@lucide/vue'
import { UiMessage } from '../../ui/feedback.js'
import { opsErrorText, opsGet, opsSend } from '../../ops/opsApi.js'

const props = defineProps({ modelValue: Boolean, folders: { type: Array, default: () => [] }, dataSources: { type: Array, default: () => [] }, folderUid: { type: String, default: '' } })
const emit = defineEmits(['update:modelValue', 'imported'])
const source = ref('template')
const templates = ref([])
const template = ref('')
const text = ref('')
const parsed = ref(null)
const parseError = ref('')
const analysis = ref(null)
const analyzing = ref(false)
const error = ref('')
const inputs = ref({})
const folder = ref('')
const mode = ref('new')
const importing = ref(false)
const fileInput = ref(null)
const levelText = { full: '完整支持', partial: '部分支持', limited: '有限支持' }
const ready = computed(() => (source.value === 'template' ? Boolean(template.value) : Boolean(parsed.value)))
const missing = computed(() => (analysis.value?.inputs || []).filter(i => i.type === 'datasource' && !inputs.value[i.name] && !i.value))
const counts = computed(() => {
  const panels = analysis.value?.support?.panels || []
  return { total: panels.length, partial: panels.filter(p => p.status === 'partial').length, unsupported: panels.filter(p => p.status === 'unsupported').length }
})

watch(() => props.modelValue, async open => {
  if (!open) return
  source.value = 'template'; template.value = ''; text.value = ''; parsed.value = null; parseError.value = ''; analysis.value = null; error.value = ''; inputs.value = {}; mode.value = 'new'; folder.value = props.folderUid
  try { templates.value = (await opsGet('/api/v1/ops/dashboards/templates')).items || [] } catch { templates.value = [] }
})
watch(text, value => {
  parsed.value = null
  parseError.value = ''
  analysis.value = null
  if (!value.trim()) return
  try {
    const json = JSON.parse(value)
    if (!json || typeof json !== 'object' || Array.isArray(json)) throw new Error('不是仪表盘对象')
    parsed.value = json
  } catch (e) { parseError.value = `JSON 无法解析：${e.message}` }
})
watch([template, source], () => { analysis.value = null; error.value = '' })

async function readFile(event) {
  const file = event.target.files?.[0]
  event.target.value = ''
  if (!file) return
  if (file.size > 5 * 1024 * 1024) { UiMessage.error('文件超过 5 MB'); return }
  text.value = await file.text()
  source.value = 'json'
}
function body(dryRun) {
  const base = source.value === 'template' ? { template: template.value } : { dashboard: parsed.value }
  // 分析时始终按原 UID 检查冲突，导入时再按用户选择覆盖或生成新 UID。
  if (dryRun) return { ...base, inputs: inputs.value, folderUid: folder.value, dryRun }
  return { ...base, inputs: inputs.value, folderUid: folder.value, dryRun, overwrite: mode.value === 'overwrite', newUid: mode.value === 'copy' }
}
async function analyze() {
  if (!ready.value) return
  analyzing.value = true
  error.value = ''
  try {
    analysis.value = await opsSend('POST', '/api/v1/ops/dashboards/import', body(true))
    for (const input of analysis.value.inputs || []) if (input.value && !inputs.value[input.name]) inputs.value = { ...inputs.value, [input.name]: input.value }
    mode.value = analysis.value.existing ? (mode.value === 'overwrite' ? 'overwrite' : 'copy') : 'new'
  } catch (e) { error.value = opsErrorText(e); analysis.value = null } finally { analyzing.value = false }
}
async function submit() {
  importing.value = true
  error.value = ''
  try {
    const result = await opsSend('POST', '/api/v1/ops/dashboards/import', body(false))
    UiMessage.success(`已导入“${result.title}”`)
    emit('imported', result.saved?.uid)
    emit('update:modelValue', false)
  } catch (e) {
    if (e.status === 409 && e.details?.analysis) analysis.value = e.details.analysis
    error.value = opsErrorText(e)
  } finally { importing.value = false }
}
const dsFor = pluginId => props.dataSources.filter(ds => ds.type === pluginId)
</script>

<template>
  <ui-dialog :model-value="modelValue" title="导入仪表盘" width="min(760px, 96vw)" @update:model-value="value => emit('update:modelValue', value)">
    <div class="import">
      <ui-radio-group v-model="source" size="small" class="segmented-choice-group" aria-label="导入来源">
        <ui-radio-button value="template">内置模板</ui-radio-button><ui-radio-button value="json">Grafana JSON</ui-radio-button>
      </ui-radio-group>
      <div v-if="source === 'template'" class="templates" role="listbox" aria-label="内置模板">
        <button v-for="t in templates" :key="t.id" type="button" class="template" :class="{ 'is-active': template === t.id }" role="option" :aria-selected="template === t.id" @click="template = t.id"><strong>{{ t.title }}</strong><small>{{ t.description }}</small></button>
        <ui-empty v-if="!templates.length" description="没有可用模板" :image-size="48" />
      </div>
      <template v-else>
        <div class="json-actions"><ui-button size="small" @click="fileInput.click()"><Upload />选择 JSON 文件</ui-button><span class="muted">或把 Grafana 导出的 JSON 粘贴到下方，最大 5 MB</span><input ref="fileInput" type="file" accept="application/json,.json" hidden @change="readFile" /></div>
        <ui-input v-model="text" type="textarea" :autosize="{ minRows: 6, maxRows: 14 }" class="mono-input" placeholder="{ &quot;title&quot;: …, &quot;panels&quot;: […] }" aria-label="仪表盘 JSON" />
        <ui-alert v-if="parseError" type="error" :title="parseError" :closable="false" />
      </template>
      <div class="import-actions"><ui-button type="primary" plain size="small" :disabled="!ready" :loading="analyzing" @click="analyze">分析</ui-button></div>
      <ui-alert v-if="error" type="error" :title="error" :closable="false" show-icon />

      <section v-if="analysis" class="analysis">
        <p><strong>{{ analysis.title }}</strong><span v-if="analysis.uid" class="muted"> · UID {{ analysis.uid }}</span></p>
        <p class="muted">平台显示：<strong :class="`level-${analysis.support.level}`">{{ levelText[analysis.support.level] }}</strong>（{{ counts.total }} 个面板，其中 {{ counts.partial }} 个部分支持，{{ counts.unsupported }} 个平台不显示）。完整 JSON 会保存到 Grafana。</p>
        <ul v-if="counts.partial + counts.unsupported" class="reasons">
          <li v-for="item in analysis.support.panels.filter(p => p.status !== 'supported')" :key="`${item.id}`">{{ item.name || `#${item.id}` }}：{{ (item.reasons || []).join('；') }}</li>
          <li v-for="note in analysis.support.notes" :key="note">{{ note }}</li>
        </ul>
        <div v-if="analysis.inputs.length" class="inputs">
          <strong>导入参数</strong>
          <label v-for="input in analysis.inputs" :key="input.name">{{ input.label || input.name }}<small>{{ input.type === 'datasource' ? `${input.pluginId} 数据源` : '常量' }}</small>
            <ui-select v-if="input.type === 'datasource'" :model-value="inputs[input.name]" :placeholder="dsFor(input.pluginId).length ? '选择数据源' : '没有该类型的数据源'" @update:model-value="value => { inputs = { ...inputs, [input.name]: value }; analyze() }">
              <ui-option v-for="ds in dsFor(input.pluginId)" :key="ds.uid" :value="ds.uid" :label="ds.name" />
            </ui-select>
            <ui-input v-else :model-value="inputs[input.name] ?? input.value" @update:model-value="value => (inputs = { ...inputs, [input.name]: value })" />
          </label>
        </div>
        <label class="folder">保存到文件夹<ui-select v-model="folder" aria-label="文件夹"><ui-option value="" label="根目录（General）" /><ui-option v-for="f in folders" :key="f.uid" :value="f.uid" :label="f.title" /></ui-select></label>
        <div v-if="analysis.existing" class="existing">
          <ui-alert type="warning" :closable="false" :title="`已存在 UID 相同的仪表盘“${analysis.existing.title}”`" />
          <ui-radio-group v-model="mode" size="small" aria-label="冲突处理"><ui-radio value="copy">作为新仪表盘导入（生成新 UID）</ui-radio><ui-radio value="overwrite">覆盖现有仪表盘</ui-radio></ui-radio-group>
        </div>
      </section>
    </div>
    <template #footer>
      <ui-button @click="emit('update:modelValue', false)">取消</ui-button>
      <ui-button type="primary" :disabled="!analysis || missing.length > 0" :loading="importing" @click="submit">{{ mode === 'overwrite' ? '覆盖导入' : '导入' }}</ui-button>
    </template>
  </ui-dialog>
</template>

<style scoped>
.import { display: grid; gap: var(--space-3); }
.templates { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: var(--space-2); }
.template { display: grid; gap: 4px; padding: var(--space-3); color: var(--text); text-align: left; background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius-md); cursor: pointer; }
.template:hover { border-color: var(--border-hover); }
.template.is-active { background: var(--primary-soft); border-color: var(--primary-border); }
.template small, .muted { color: var(--text-muted); font-size: var(--font-size-xs); }
.json-actions { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-2); }
.mono-input :deep(textarea) { font-family: var(--font-mono); font-size: var(--font-size-xs); }
.import-actions { display: flex; justify-content: flex-end; }
.analysis { display: grid; gap: var(--space-2); padding: var(--space-3); background: var(--surface-muted); border: 1px solid var(--border); border-radius: var(--radius-md); }
.analysis p { margin: 0; }
.reasons { max-height: 160px; margin: 0; padding-left: 20px; overflow: auto; color: var(--text-secondary); font-size: var(--font-size-xs); }
.level-full { color: var(--success-text); }
.level-partial { color: var(--warning-text); }
.level-limited { color: var(--danger-text); }
.inputs { display: grid; gap: var(--space-2); }
.inputs label, .folder { display: grid; gap: 4px; color: var(--text-secondary); font-size: var(--font-size-sm); }
.inputs small { margin-left: 6px; color: var(--text-muted); font-size: var(--font-size-xs); }
.existing { display: grid; gap: var(--space-2); }
</style>
