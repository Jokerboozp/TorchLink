<script setup>
// 仪表盘设置：标题、标签、文件夹、默认时间与刷新、变量，以及完整 JSON 模型。
import { computed, ref, watch } from 'vue'
import { ArrowDown, ArrowUp, Pencil, Plus, Trash2 } from '@lucide/vue'
import { UiMessage } from '../../ui/feedback.js'
import { rangePresets, refreshOptions } from '../../ops/timeRange.js'
import { refreshMs, refreshText, variableTypeNames } from '../../ops/dashboard.js'

const props = defineProps({
  modelValue: Boolean,
  dashboard: { type: Object, default: null },
  folderUid: { type: String, default: '' },
  folders: { type: Array, default: () => [] },
  dataSources: { type: Array, default: () => [] }
})
const emit = defineEmits(['update:modelValue', 'apply'])
const tab = ref('general')
const draft = ref(null)
const folder = ref('')
const tagText = ref('')
const json = ref('')
const jsonError = ref('')
const variableVisible = ref(false)
const variable = ref(null)
const editingIndex = ref(-1)
const supported = computed(() => props.dataSources.filter(ds => ds.supported))

watch(() => props.modelValue, open => {
  if (!open || !props.dashboard) return
  draft.value = JSON.parse(JSON.stringify(props.dashboard))
  draft.value.templating = draft.value.templating || { list: [] }
  draft.value.time = draft.value.time || { from: 'now-6h', to: 'now' }
  folder.value = props.folderUid
  tagText.value = (draft.value.tags || []).join(', ')
  json.value = JSON.stringify(props.dashboard, null, 2)
  jsonError.value = ''
  tab.value = 'general'
})

function queryText(v) { return typeof v.query === 'object' && v.query ? v.query.query ?? v.definition ?? '' : v.query ?? '' }
function dsType(v) { return props.dataSources.find(ds => ds.uid === v.datasource?.uid)?.type || v.datasource?.type || '' }
function openVariable(index) {
  editingIndex.value = index
  const source = index >= 0 ? draft.value.templating.list[index] : { type: 'query', name: '', label: '', hide: 0, refresh: 1, multi: false, includeAll: false, sort: 1, regex: '', options: [], current: {} }
  variable.value = { ...JSON.parse(JSON.stringify(source)), queryText: source.type === 'query' ? queryText(source) : source.query ?? '' }
  if (variable.value.type === 'query' && !variable.value.datasource) {
    const ds = supported.value.find(item => item.isDefault) || supported.value[0]
    if (ds) variable.value.datasource = { type: ds.type, uid: ds.uid }
  }
  variableVisible.value = true
}
function saveVariable() {
  const v = variable.value
  const name = String(v.name || '').trim()
  if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(name)) { UiMessage.warning('变量名只能包含字母、数字和下划线，且不能以数字开头'); return }
  if (draft.value.templating.list.some((item, i) => item.name === name && i !== editingIndex.value)) { UiMessage.warning('变量名已存在'); return }
  const { queryText: text, ...rest } = v
  rest.name = name
  if (rest.type === 'query') {
    rest.query = typeof rest.query === 'object' && rest.query ? { ...rest.query, query: text } : dsType(rest) === 'prometheus' ? { query: text, refId: 'PrometheusVariableQueryEditor-VariableQuery' } : text
    rest.definition = text
  } else {
    rest.query = text
    if (rest.type !== 'query') delete rest.datasource
  }
  if (rest.type === 'datasource') rest.query = text || 'prometheus'
  if (!['query', 'custom', 'datasource'].includes(rest.type)) { rest.multi = false; rest.includeAll = false }
  if (!rest.current || !Object.keys(rest.current).length) rest.current = {}
  if (editingIndex.value >= 0) draft.value.templating.list.splice(editingIndex.value, 1, rest)
  else draft.value.templating.list.push(rest)
  variableVisible.value = false
}
function moveVariable(index, delta) {
  const list = draft.value.templating.list
  const target = index + delta
  if (target < 0 || target >= list.length) return
  ;[list[index], list[target]] = [list[target], list[index]]
}

function apply() {
  if (tab.value === 'json') {
    try {
      const parsed = JSON.parse(json.value)
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed) || !String(parsed.title || '').trim()) throw new Error('JSON 必须是包含 title 的仪表盘对象')
      if (props.dashboard.uid && parsed.uid !== props.dashboard.uid) throw new Error('不能在这里修改仪表盘 UID，请使用复制或导入')
      if (parsed.panels != null && !Array.isArray(parsed.panels)) throw new Error('panels 必须是数组')
      emit('apply', { dashboard: parsed, folderUid: folder.value })
    } catch (e) { jsonError.value = e.message; return }
  } else {
    const title = String(draft.value.title || '').trim()
    if (!title) { UiMessage.warning('请填写仪表盘标题'); return }
    draft.value.title = title
    draft.value.tags = tagText.value.split(/[,，]/).map(t => t.trim()).filter(Boolean).slice(0, 20)
    emit('apply', { dashboard: draft.value, folderUid: folder.value })
  }
  emit('update:modelValue', false)
}
</script>

<template>
  <ui-drawer :model-value="modelValue" title="仪表盘设置" size="min(760px, 96vw)" @update:model-value="value => emit('update:modelValue', value)">
    <ui-tabs v-if="draft" v-model="tab">
      <ui-tab-pane name="general" label="常规">
        <div class="settings-form">
          <label>标题<ui-input v-model="draft.title" maxlength="200" /></label>
          <label>说明<ui-input v-model="draft.description" type="textarea" :rows="2" /></label>
          <label>标签<ui-input v-model="tagText" placeholder="多个标签用逗号分隔" /></label>
          <label>文件夹<ui-select v-model="folder" aria-label="文件夹"><ui-option value="" label="根目录（General）" /><ui-option v-for="f in folders" :key="f.uid" :value="f.uid" :label="f.title" /></ui-select></label>
          <label>默认时间范围<ui-select v-model="draft.time.from" aria-label="默认时间范围" @update:model-value="draft.time.to = 'now'"><ui-option v-for="p in rangePresets" :key="p.value" :value="p.value" :label="p.label" /><ui-option v-if="!rangePresets.some(p => p.value === draft.time.from)" :value="draft.time.from" :label="`${draft.time.from} 至 ${draft.time.to}`" /></ui-select></label>
          <label>默认自动刷新<ui-select :model-value="refreshMs(draft.refresh)" aria-label="默认自动刷新" @update:model-value="value => (draft.refresh = refreshText(value))"><ui-option v-for="r in refreshOptions" :key="r.value" :value="r.value" :label="r.label" /></ui-select></label>
          <p class="muted">UID：{{ draft.uid || '保存后由 Grafana 生成' }}<template v-if="draft.version"> · 版本 {{ draft.version }}</template></p>
        </div>
      </ui-tab-pane>
      <ui-tab-pane name="variables" label="变量">
        <p class="muted">面板查询中用 $变量名 或 ${变量名} 引用；变量按顺序加载，后面的变量可以引用前面的变量。</p>
        <ui-table :data="draft.templating.list" size="small" :row-key="row => row.name" empty-text="还没有变量">
          <ui-table-column label="名称" min-width="120"><template #default="{ row }"><code>${{ row.name }}</code></template></ui-table-column>
          <ui-table-column label="显示名" min-width="100"><template #default="{ row }">{{ row.label || '—' }}</template></ui-table-column>
          <ui-table-column label="类型" width="100"><template #default="{ row }">{{ variableTypeNames[row.type] || `${row.type}（不支持）` }}</template></ui-table-column>
          <ui-table-column label="定义" min-width="200"><template #default="{ row }"><span class="definition">{{ row.type === 'query' ? queryText(row) : row.query }}</span></template></ui-table-column>
          <ui-table-column label="操作" width="150">
            <template #default="{ row, $index }">
              <ui-button text size="small" aria-label="上移" @click="moveVariable($index, -1)"><ArrowUp /></ui-button>
              <ui-button text size="small" aria-label="下移" @click="moveVariable($index, 1)"><ArrowDown /></ui-button>
              <ui-button v-if="variableTypeNames[row.type]" text size="small" aria-label="编辑" @click="openVariable($index)"><Pencil /></ui-button>
              <ui-button text size="small" type="danger" aria-label="删除" @click="draft.templating.list.splice($index, 1)"><Trash2 /></ui-button>
            </template>
          </ui-table-column>
        </ui-table>
        <ui-button size="small" text type="primary" class="add-variable" @click="openVariable(-1)"><Plus />添加变量</ui-button>
      </ui-tab-pane>
      <ui-tab-pane name="json" label="JSON 模型">
        <p class="muted">完整的 Grafana 仪表盘 JSON。平台不渲染的字段也会原样保存；应用后仍需点击“保存”写入 Grafana。</p>
        <ui-input v-model="json" type="textarea" :autosize="{ minRows: 16, maxRows: 32 }" class="mono-input" aria-label="仪表盘 JSON" @update:model-value="jsonError = ''" />
        <ui-alert v-if="jsonError" type="error" :title="jsonError" :closable="false" />
      </ui-tab-pane>
    </ui-tabs>
    <template #footer><div class="settings-footer"><ui-button @click="emit('update:modelValue', false)">取消</ui-button><ui-button type="primary" @click="apply">应用</ui-button></div></template>

    <ui-dialog v-model="variableVisible" :title="editingIndex >= 0 ? '编辑变量' : '添加变量'" width="min(600px, 94vw)">
      <div v-if="variable" class="settings-form">
        <div class="two">
          <label>名称<ui-input v-model="variable.name" placeholder="例如 instance" /></label>
          <label>显示名<ui-input v-model="variable.label" placeholder="可选" /></label>
        </div>
        <div class="two">
          <label>类型<ui-select v-model="variable.type" aria-label="变量类型"><ui-option v-for="(name, type) in variableTypeNames" :key="type" :value="type" :label="name" /></ui-select></label>
          <label>显示方式<ui-select v-model="variable.hide" aria-label="显示方式"><ui-option :value="0" label="显示名称和选择框" /><ui-option :value="1" label="只显示选择框" /><ui-option :value="2" label="隐藏" /></ui-select></label>
        </div>
        <template v-if="variable.type === 'query'">
          <label>数据源<ui-select :model-value="variable.datasource?.uid" aria-label="数据源" @update:model-value="uid => { const ds = supported.find(d => d.uid === uid); variable.datasource = ds ? { type: ds.type, uid: ds.uid } : null }"><ui-option v-for="ds in supported" :key="ds.uid" :value="ds.uid" :label="`${ds.name}（${ds.type}）`" /></ui-select></label>
          <label>查询<ui-input v-model="variable.queryText" class="mono-input" :placeholder="dsType(variable) === 'loki' ? 'label_values(service_name)' : 'label_values(up, instance)'" /></label>
          <p class="muted">Prometheus 支持 label_names()、label_values()、metrics()、query_result()；Loki 支持 label_names() 与 label_values()。</p>
          <label>正则过滤<ui-input v-model="variable.regex" class="mono-input" placeholder="可选，例如 /^(.*):\d+$/" /></label>
          <div class="two">
            <label>排序<ui-select v-model="variable.sort" aria-label="排序"><ui-option :value="0" label="按返回顺序" /><ui-option :value="1" label="字母升序" /><ui-option :value="2" label="字母降序" /><ui-option :value="3" label="数值升序" /><ui-option :value="4" label="数值降序" /></ui-select></label>
            <label>刷新时机<ui-select v-model="variable.refresh" aria-label="刷新时机"><ui-option :value="1" label="加载仪表盘时" /><ui-option :value="2" label="时间范围变化时" /></ui-select></label>
          </div>
        </template>
        <label v-else-if="variable.type === 'custom'">可选值<ui-input v-model="variable.queryText" placeholder="用逗号分隔，例如 a,b,c 或 显示名 : 值" /></label>
        <label v-else-if="variable.type === 'interval'">可选间隔<ui-input v-model="variable.queryText" placeholder="1m,5m,10m,30m,1h" /></label>
        <label v-else-if="variable.type === 'datasource'">数据源类型<ui-select v-model="variable.queryText" aria-label="数据源类型"><ui-option value="prometheus" label="Prometheus" /><ui-option value="loki" label="Loki" /></ui-select></label>
        <label v-else>值<ui-input v-model="variable.queryText" /></label>
        <div v-if="['query', 'custom', 'datasource'].includes(variable.type)" class="checks">
          <ui-checkbox v-model="variable.multi">允许多选</ui-checkbox>
          <ui-checkbox v-model="variable.includeAll">包含“全部”选项</ui-checkbox>
          <ui-input v-if="variable.includeAll" v-model="variable.allValue" size="small" class="mono-input" placeholder="“全部”的自定义值，可选，例如 .*" />
        </div>
      </div>
      <template #footer><ui-button @click="variableVisible = false">取消</ui-button><ui-button type="primary" @click="saveVariable">确定</ui-button></template>
    </ui-dialog>
  </ui-drawer>
</template>

<style scoped>
.settings-form { display: grid; gap: var(--space-3); }
.settings-form label { display: grid; gap: 6px; color: var(--text-secondary); font-size: var(--font-size-sm); }
.two { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--space-3); }
.checks { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-3); }
.checks .ui-input { width: 240px; }
.muted { margin: 0 0 var(--space-2); color: var(--text-muted); font-size: var(--font-size-xs); }
.definition { color: var(--text-secondary); font: var(--font-size-xs) var(--font-mono); word-break: break-all; }
.add-variable { margin-top: var(--space-2); }
.mono-input :deep(textarea), .mono-input :deep(input) { font-family: var(--font-mono); font-size: var(--font-size-sm); }
.settings-footer { display: flex; justify-content: flex-end; gap: var(--space-2); }
@media (max-width: 767px) {
  .two { grid-template-columns: minmax(0, 1fr); }
}
</style>
