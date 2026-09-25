<script setup>
// 面板编辑：类型、数据源、查询、显示选项、阈值与尺寸；预览通过平台在服务端执行查询。
import { computed, ref, watch } from 'vue'
import { Play, Plus, Trash2 } from '@lucide/vue'
import { can } from '../../permissions'
import { unitOptions } from '../../ops/format.js'
import { calcOptions } from '../../ops/frames.js'
import { defaultTarget, editablePanelTypes, panelTypeNames } from '../../ops/dashboard.js'
import { latest, opsErrorText, opsSend } from '../../ops/opsApi.js'
import DashboardPanel from './DashboardPanel.vue'

const props = defineProps({
  modelValue: Boolean,
  panel: { type: Object, default: null },
  dashboard: { type: Object, default: null },
  dataSources: { type: Array, default: () => [] },
  range: { type: Object, default: () => ({}) },
  vars: { type: Object, default: () => ({}) }
})
const emit = defineEmits(['update:modelValue', 'apply'])
const draft = ref(null)
const editorTab = ref('query')
const preview = ref(null)
const previewError = ref('')
const previewing = ref(false)
const runner = latest()
const canPreview = computed(() => can('POST /api/v1/ops/dashboards/preview'))
const colorChoices = [{ value: 'green', label: '绿色' }, { value: 'orange', label: '橙色' }, { value: 'red', label: '红色' }, { value: 'blue', label: '蓝色' }, { value: 'yellow', label: '黄色' }, { value: 'purple', label: '紫色' }, { value: 'text', label: '文字色' }]
const supported = computed(() => props.dataSources.filter(ds => ds.supported))
const typeOptions = computed(() => {
  const list = [...editablePanelTypes]
  if (draft.value && !list.includes(draft.value.type)) list.unshift(draft.value.type)
  return list
})
const isData = computed(() => draft.value && !['text', 'row'].includes(draft.value.type))
const panelDs = computed(() => dsFor(draft.value?.datasource))
const reduces = computed(() => ['stat', 'gauge', 'bargauge'].includes(draft.value?.type))
const customUnit = computed(() => draft.value && !unitOptions.some(u => u.value === draft.value.fieldConfig.defaults.unit))

function dsFor(ref) {
  const uid = typeof ref === 'string' ? ref : ref?.uid
  return props.dataSources.find(ds => ds.uid === uid || ds.name === uid) || props.dataSources.find(ds => ds.isDefault) || null
}
function targetDs(target) { return target.datasource?.uid ? dsFor(target.datasource) : panelDs.value }

watch(() => [props.modelValue, props.panel], ([open]) => {
  if (!open || !props.panel) return
  const copy = JSON.parse(JSON.stringify(props.panel))
  copy.fieldConfig = copy.fieldConfig || { defaults: {}, overrides: [] }
  copy.fieldConfig.defaults = copy.fieldConfig.defaults || {}
  copy.fieldConfig.defaults.thresholds = copy.fieldConfig.defaults.thresholds || { mode: 'absolute', steps: [{ color: 'green', value: null }] }
  copy.fieldConfig.defaults.custom = copy.fieldConfig.defaults.custom || {}
  copy.options = copy.options || {}
  copy.options.reduceOptions = copy.options.reduceOptions || { calcs: ['lastNotNull'], fields: '', values: false }
  copy.options.legend = copy.options.legend || { showLegend: true, displayMode: 'list', placement: 'bottom' }
  copy.targets = copy.targets || []
  copy.gridPos = copy.gridPos || { x: 0, y: 0, w: 12, h: 8 }
  draft.value = copy
  editorTab.value = 'query'
  preview.value = null
  previewError.value = ''
  if (canPreview.value && isData.value) runPreview()
}, { immediate: true })

function setDataSource(uid) {
  const ds = props.dataSources.find(item => item.uid === uid)
  draft.value.datasource = ds ? { type: ds.type, uid: ds.uid } : undefined
  draft.value.targets = draft.value.targets.map(t => ({ ...defaultTarget(ds, t.refId), expr: t.expr || '', legendFormat: t.legendFormat }))
}
function addTarget() {
  const used = new Set(draft.value.targets.map(t => t.refId))
  const refId = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ'.split('').find(letter => !used.has(letter)) || `Q${used.size + 1}`
  draft.value.targets.push(defaultTarget(panelDs.value, refId))
}
// Grafana 的 Prometheus 查询用 range / instant 两个开关表示查询类型。
function setInstant(target, value) { target.instant = value !== 'range'; target.range = value !== 'instant' }
const instantMode = target => (target.instant ? (target.range === true ? 'both' : 'instant') : 'range')
function addStep() {
  const steps = draft.value.fieldConfig.defaults.thresholds.steps
  const last = steps[steps.length - 1]?.value
  steps.push({ color: 'red', value: last == null ? 80 : Number(last) + 10 })
}
function numberField(obj, key, value) {
  if (value === '' || value == null || !Number.isFinite(Number(value))) delete obj[key]
  else obj[key] = Number(value)
}

async function runPreview() {
  if (!draft.value || !canPreview.value) return
  previewing.value = true
  previewError.value = ''
  try {
    const dashboard = { ...props.dashboard, panels: [draft.value] }
    preview.value = await runner.run(signal => opsSend('POST', '/api/v1/ops/dashboards/preview', { dashboard, panel: draft.value, from: props.range.from, to: props.range.to, vars: props.vars, maxDataPoints: 600 }, signal))
  } catch (e) {
    if (e?.name !== 'AbortError') { previewError.value = opsErrorText(e); preview.value = null }
  } finally { previewing.value = false }
}
function apply() {
  const panel = draft.value
  if (panel.type === 'text') { delete panel.targets; delete panel.datasource }
  emit('apply', panel)
  emit('update:modelValue', false)
}
function close() { runner.cancel(); emit('update:modelValue', false) }
</script>

<template>
  <ui-drawer :model-value="modelValue" title="编辑面板" size="min(1080px, 98vw)" @update:model-value="value => !value && close()">
    <div v-if="draft" class="panel-editor">
      <section class="panel-editor__preview">
        <div class="panel-editor__preview-head">
          <strong>预览</strong>
          <ui-button v-if="canPreview && isData" size="small" :loading="previewing" @click="runPreview"><Play />运行查询</ui-button>
        </div>
        <p v-if="!canPreview && isData" class="muted">当前账户没有面板预览权限，保存后可在仪表盘中查看效果。</p>
        <ui-alert v-if="previewError" type="error" :title="previewError" :closable="false" />
        <div class="panel-editor__canvas"><DashboardPanel :panel="draft" :data="preview" :loading="previewing" /></div>
        <div v-if="preview?.queries?.length" class="executed">
          <span>实际执行（{{ preview.intervalMs ? `间隔 ${preview.intervalMs / 1000} 秒` : '' }}）</span>
          <code v-for="q in preview.queries" :key="q.refId">{{ q.refId }} · {{ q.dataSource }}：{{ q.expr }}</code>
          <span v-for="w in preview.warnings || []" :key="w" class="warn">{{ w }}</span>
        </div>
      </section>

      <section class="panel-editor__form">
        <ui-tabs v-model="editorTab">
          <ui-tab-pane name="query" :label="isData ? '查询' : '内容'">
            <div class="form-grid">
              <label>标题<ui-input v-model="draft.title" maxlength="200" /></label>
              <label>类型<ui-select v-model="draft.type" aria-label="面板类型"><ui-option v-for="type in typeOptions" :key="type" :value="type" :label="panelTypeNames[type] || `${type}（平台不支持）`" /></ui-select></label>
              <label class="span-2">说明<ui-input v-model="draft.description" placeholder="悬停标题旁的图标时显示" /></label>
            </div>
            <template v-if="isData">
              <label class="field">数据源
                <ui-select :model-value="panelDs?.uid" aria-label="数据源" @update:model-value="setDataSource">
                  <ui-option v-for="ds in supported" :key="ds.uid" :value="ds.uid" :label="`${ds.name}（${ds.type}）${ds.isDefault ? ' · 默认' : ''}`" />
                </ui-select>
              </label>
              <article v-for="(target, index) in draft.targets" :key="index" class="target" :class="{ 'is-hidden': target.hide }">
                <header>
                  <strong>{{ target.refId }}</strong>
                  <small>{{ targetDs(target)?.type === 'loki' ? 'LogQL' : 'PromQL' }}</small>
                  <ui-checkbox :model-value="Boolean(target.hide)" @update:model-value="value => (target.hide = value)">暂停此查询</ui-checkbox>
                  <ui-button text size="small" type="danger" aria-label="删除查询" @click="draft.targets.splice(index, 1)"><Trash2 /></ui-button>
                </header>
                <ui-input v-model="target.expr" type="textarea" :autosize="{ minRows: 2, maxRows: 8 }" class="mono-input" :placeholder="targetDs(target)?.type === 'loki' ? '{service_name=&quot;platform-api&quot;} |= &quot;error&quot;' : 'sum(rate(ingest_messages_total[5m]))'" :aria-label="`查询 ${target.refId}`" />
                <div class="target__opts">
                  <template v-if="targetDs(target)?.type !== 'loki'">
                    <ui-input v-model="target.legendFormat" size="small" placeholder="图例，例如 {{instance}}" aria-label="图例格式" />
                    <ui-select :model-value="instantMode(target)" size="small" aria-label="查询类型" @update:model-value="value => setInstant(target, value)"><ui-option value="range" label="范围查询" /><ui-option value="instant" label="即时查询" /><ui-option value="both" label="范围 + 即时" /></ui-select>
                    <ui-input v-model="target.interval" size="small" placeholder="最小步长，如 1m" aria-label="最小步长" />
                  </template>
                  <template v-else>
                    <ui-select v-model="target.queryType" size="small" aria-label="查询类型"><ui-option value="range" label="范围查询" /><ui-option value="instant" label="即时查询" /></ui-select>
                    <ui-input-number :model-value="target.maxLines" size="small" :min="1" :max="5000" placeholder="最大行数" aria-label="最大行数" @update:model-value="value => (target.maxLines = value || undefined)" />
                  </template>
                </div>
              </article>
              <ui-button v-if="draft.targets.length < 20" size="small" text type="primary" @click="addTarget"><Plus />添加查询</ui-button>
            </template>
            <template v-else-if="draft.type === 'text'">
              <label class="field">格式<ui-select v-model="draft.options.mode" aria-label="文本格式"><ui-option value="markdown" label="Markdown" /><ui-option value="code" label="纯文本 / 代码" /></ui-select></label>
              <label class="field">内容<ui-input v-model="draft.options.content" type="textarea" :autosize="{ minRows: 6, maxRows: 18 }" /></label>
            </template>
          </ui-tab-pane>

          <ui-tab-pane v-if="isData" name="display" label="显示">
            <div class="form-grid">
              <label>单位
                <ui-select :model-value="customUnit ? '__custom' : draft.fieldConfig.defaults.unit" aria-label="单位" @update:model-value="value => (draft.fieldConfig.defaults.unit = value === '__custom' ? 'suffix:' : value)">
                  <ui-option v-for="u in unitOptions" :key="u.value" :value="u.value" :label="u.label" /><ui-option value="__custom" label="自定义后缀…" />
                </ui-select>
              </label>
              <label v-if="customUnit">单位编号<ui-input v-model="draft.fieldConfig.defaults.unit" placeholder="suffix:次 或 Grafana 单位编号" /></label>
              <label>小数位<ui-input-number :model-value="draft.fieldConfig.defaults.decimals" :min="0" :max="10" placeholder="自动" @update:model-value="value => numberField(draft.fieldConfig.defaults, 'decimals', value)" /></label>
              <label>最小值<ui-input :model-value="draft.fieldConfig.defaults.min ?? ''" placeholder="自动" @update:model-value="value => numberField(draft.fieldConfig.defaults, 'min', value)" /></label>
              <label>最大值<ui-input :model-value="draft.fieldConfig.defaults.max ?? ''" placeholder="自动" @update:model-value="value => numberField(draft.fieldConfig.defaults, 'max', value)" /></label>
              <label v-if="reduces">统计方式<ui-select v-model="draft.options.reduceOptions.calcs[0]" aria-label="统计方式"><ui-option v-for="c in calcOptions" :key="c.value" :value="c.value" :label="c.label" /></ui-select></label>
              <template v-if="draft.type === 'timeseries'">
                <label>绘制方式<ui-select v-model="draft.fieldConfig.defaults.custom.drawStyle" placeholder="折线" aria-label="绘制方式"><ui-option value="line" label="折线" /><ui-option value="bars" label="堆叠柱状" /></ui-select></label>
                <label>填充透明度<ui-input-number v-model="draft.fieldConfig.defaults.custom.fillOpacity" :min="0" :max="100" placeholder="10" /></label>
                <label class="check"><ui-checkbox v-model="draft.options.legend.showLegend">显示图例</ui-checkbox></label>
              </template>
              <template v-if="draft.type === 'stat'">
                <label>着色<ui-select v-model="draft.options.colorMode" placeholder="数值着色" aria-label="着色方式"><ui-option value="value" label="数值着色" /><ui-option value="background" label="背景着色" /><ui-option value="none" label="不着色" /></ui-select></label>
                <label>趋势<ui-select v-model="draft.options.graphMode" placeholder="不显示" aria-label="趋势"><ui-option value="none" label="不显示" /><ui-option value="area" label="迷你趋势" /></ui-select></label>
              </template>
              <label v-if="draft.type === 'gauge'" class="check"><ui-checkbox :model-value="draft.options.showThresholdMarkers !== false" @update:model-value="value => (draft.options.showThresholdMarkers = value)">显示阈值刻度</ui-checkbox></label>
              <label v-if="draft.type === 'bargauge'">方向<ui-select v-model="draft.options.orientation" placeholder="水平" aria-label="方向"><ui-option value="horizontal" label="水平" /><ui-option value="auto" label="自动" /></ui-select></label>
              <template v-if="draft.type === 'logs'">
                <label>排序<ui-select v-model="draft.options.sortOrder" aria-label="排序"><ui-option value="Descending" label="最新在前" /><ui-option value="Ascending" label="最早在前" /></ui-select></label>
                <label class="check"><ui-checkbox :model-value="draft.options.wrapLogMessage !== false" @update:model-value="value => (draft.options.wrapLogMessage = value)">自动换行</ui-checkbox></label>
              </template>
            </div>
            <div v-if="draft.type !== 'logs' && draft.type !== 'table'" class="thresholds">
              <div class="thresholds__head"><strong>阈值</strong><ui-select v-model="draft.fieldConfig.defaults.thresholds.mode" size="small" aria-label="阈值模式"><ui-option value="absolute" label="绝对值" /><ui-option value="percentage" label="百分比" /></ui-select></div>
              <div v-for="(step, index) in draft.fieldConfig.defaults.thresholds.steps" :key="index" class="threshold-row">
                <ui-select v-model="step.color" size="small" aria-label="颜色"><ui-option v-for="c in colorChoices" :key="c.value" :value="c.value" :label="c.label" /><ui-option v-if="!colorChoices.some(c => c.value === step.color)" :value="step.color" :label="step.color" /></ui-select>
                <span v-if="step.value == null" class="muted">基础</span>
                <ui-input-number v-else v-model="step.value" size="small" aria-label="阈值" />
                <ui-button v-if="step.value != null" text size="small" aria-label="删除阈值" @click="draft.fieldConfig.defaults.thresholds.steps.splice(index, 1)"><Trash2 /></ui-button>
              </div>
              <ui-button size="small" text type="primary" @click="addStep"><Plus />添加阈值</ui-button>
            </div>
          </ui-tab-pane>

          <ui-tab-pane name="layout" label="尺寸">
            <div class="form-grid">
              <label>宽度（1～24 列）<ui-input-number v-model="draft.gridPos.w" :min="2" :max="24" /></label>
              <label>高度（行，每行约 30 像素）<ui-input-number v-model="draft.gridPos.h" :min="2" :max="40" /></label>
              <label class="check"><ui-checkbox :model-value="Boolean(draft.transparent)" @update:model-value="value => (draft.transparent = value)">透明背景</ui-checkbox></label>
            </div>
            <p class="muted">位置可在编辑模式下用面板标题栏的箭头调整，面板会自动向上靠拢。</p>
          </ui-tab-pane>
        </ui-tabs>
      </section>
    </div>
    <template #footer>
      <div class="panel-editor__footer"><ui-button @click="close">取消</ui-button><ui-button type="primary" @click="apply">应用到仪表盘</ui-button></div>
    </template>
  </ui-drawer>
</template>

<style scoped>
.panel-editor { display: grid; grid-template-columns: minmax(0, 1.1fr) minmax(360px, 1fr); gap: var(--space-4); min-width: 0; }
.panel-editor__preview, .panel-editor__form { display: grid; align-content: start; gap: var(--space-3); min-width: 0; }
.panel-editor__preview-head { display: flex; align-items: center; justify-content: space-between; }
.panel-editor__canvas { height: 320px; }
.executed { display: grid; gap: 4px; color: var(--text-muted); font-size: var(--font-size-xs); }
.executed code { color: var(--code-inline-text); font-family: var(--font-mono); word-break: break-all; }
.executed .warn { color: var(--warning-text); }
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: var(--space-3); margin-bottom: var(--space-3); }
.form-grid label, .field { display: grid; gap: 6px; color: var(--text-secondary); font-size: var(--font-size-sm); }
.field { margin-bottom: var(--space-3); }
.form-grid .span-2 { grid-column: 1 / -1; }
.form-grid .check { align-content: end; }
.target { display: grid; gap: var(--space-2); margin-bottom: var(--space-2); padding: var(--space-2) var(--space-3); background: var(--surface-muted); border: 1px solid var(--border); border-radius: var(--radius-md); }
.target.is-hidden { opacity: 0.6; }
.target header { display: flex; align-items: center; gap: var(--space-2); }
.target header small { flex: 1; color: var(--text-muted); font-size: var(--font-size-xs); }
.target__opts { display: grid; grid-template-columns: minmax(0, 1.4fr) minmax(0, 1fr) minmax(0, 1fr); gap: var(--space-2); }
.mono-input :deep(textarea) { font-family: var(--font-mono); font-size: var(--font-size-sm); }
.thresholds { display: grid; gap: var(--space-2); }
.thresholds__head { display: flex; align-items: center; justify-content: space-between; }
.thresholds__head .ui-select { width: 120px; }
.threshold-row { display: grid; grid-template-columns: 120px minmax(0, 1fr) auto; align-items: center; gap: var(--space-2); }
.muted { margin: 0; color: var(--text-muted); font-size: var(--font-size-xs); }
.panel-editor__footer { display: flex; justify-content: flex-end; gap: var(--space-2); }
@media (max-width: 900px) {
  .panel-editor { grid-template-columns: minmax(0, 1fr); }
  .panel-editor__canvas { height: 240px; }
  .target__opts, .form-grid { grid-template-columns: minmax(0, 1fr); }
}
</style>
