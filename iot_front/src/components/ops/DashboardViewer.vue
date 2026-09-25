<script setup>
// 仪表盘查看与编辑：变量、统一时间范围与刷新、按 24 列网格布局的原生面板。
// 未修改时面板数据按已保存的仪表盘在服务端执行；编辑中的改动通过预览接口执行。
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { ArrowLeft, ChevronDown, ChevronRight, Download, FileJson, Pencil, Plus, Save, Settings, Star, X } from '@lucide/vue'
import { can } from '../../permissions'
import { UiMessage, UiMessageBox } from '../../ui/feedback.js'
import { addPanel, currentValues, dependsOn, duplicatePanel, editablePanelTypes, emptyDashboard, findPanel, movePanel, newPanel, normalizeLayout, panelTypeNames, refreshMs, refreshText, removePanel, replacePanel, sections, toggleRow, ROW_HEIGHT } from '../../ops/dashboard.js'
import { downloadJSON, isAbort, opsErrorText, opsGet, opsSend } from '../../ops/opsApi.js'
import { resolveRange } from '../../ops/timeRange.js'
import DashboardPanel from './DashboardPanel.vue'
import DashboardSettings from './DashboardSettings.vue'
import PanelEditor from './PanelEditor.vue'
import TimeRangeBar from './TimeRangeBar.vue'

const props = defineProps({
  uid: { type: String, default: '' },
  initial: { type: Object, default: null },
  folderUid: { type: String, default: '' },
  folders: { type: Array, default: () => [] },
  dataSources: { type: Array, default: () => [] }
})
const emit = defineEmits(['close', 'saved', 'deleted', 'navigate'])
const ALL = '$__all'
const MAX_PARALLEL = 6

const view = ref(null)
const working = ref(null)
const folder = ref(props.folderUid)
const loading = ref(false)
const loadError = ref('')
const editing = ref(false)
const dirty = ref(false)
const range = ref({ from: 'now-6h', to: 'now' })
const refresh = ref(0)
const isNew = computed(() => !view.value?.dashboard?.uid)
const canEdit = computed(() => can(isNew.value ? 'POST /api/v1/ops/dashboards' : 'PUT /api/v1/ops/dashboards/:uid'))
const canPreview = computed(() => can('POST /api/v1/ops/dashboards/preview'))
const canCopy = computed(() => can('POST /api/v1/ops/dashboards/:uid/copy'))
const canDelete = computed(() => can('DELETE /api/v1/ops/dashboards/:uid'))
const canQueryMetrics = computed(() => can('menu:opsMetrics') && can('POST /api/v1/ops/metrics/query'))
const canQueryLogs = computed(() => can('menu:opsLogs') && can('POST /api/v1/ops/logs/query'))
const levelText = { full: '完整支持', partial: '部分支持', limited: '有限支持' }
const levelTone = { full: 'success', partial: 'warning', limited: 'danger' }
const supportByPanel = computed(() => Object.fromEntries((view.value?.support?.panels || []).map(item => [item.id, item])))
const layout = computed(() => (working.value ? sections(working.value) : []))
const variables = computed(() => (working.value?.templating?.list || []).filter(v => v && v.name))
const usePreview = computed(() => dirty.value || isNew.value)

// 变量状态
const varValues = reactive({})
const varOptions = reactive({})
const varErrors = reactive({})
const varLoading = ref(false)
const varsParam = () => Object.fromEntries(variables.value.filter(v => v.type !== 'constant' && varValues[v.name]).map(v => [v.name, varValues[v.name]]))

async function loadOptions(variable) {
  const { from, to } = resolveRange(range.value)
  if (usePreview.value) {
    if (!canPreview.value) throw new Error('编辑中的变量需要面板预览权限')
    return (await opsSend('POST', '/api/v1/ops/dashboards/preview', { variable: variable.name, dashboard: working.value, vars: varsParam(), from, to })).items || []
  }
  return (await opsGet(`/api/v1/ops/dashboards/${encodeURIComponent(view.value.dashboard.uid)}/variables/${encodeURIComponent(variable.name)}/options`, { from, to, vars: varsParam() })).items || []
}
function pickValues(variable, options, previous) {
  if (variable.type === 'textbox') return previous.length ? previous : [String(variable.query ?? '')]
  const allowed = new Set(options.map(o => o.value))
  if (previous.includes(ALL) && variable.includeAll) return [ALL]
  const kept = previous.filter(v => allowed.has(v) || (variable.type === 'interval' && v.startsWith('$__auto')))
  if (kept.length) return variable.multi ? kept : kept.slice(0, 1)
  if (variable.includeAll && !options.length) return [ALL]
  return options.length ? [options[0].value] : previous
}
// loadVariables 按顺序加载可选值；only 指定时只刷新依赖这些变量的后续变量。
async function loadVariables(only = null, refreshOnTime = false) {
  varLoading.value = true
  const changed = new Set(only || [])
  try {
    for (const variable of variables.value) {
      if (!['query', 'custom', 'interval', 'datasource', 'textbox'].includes(variable.type)) continue
      if (only && ![...changed].some(name => dependsOn(variable, name))) continue
      if (refreshOnTime && !(variable.type === 'query' && variable.refresh === 2)) continue
      const previous = varValues[variable.name] ?? currentValues(variable)
      if (variable.type === 'textbox') { varValues[variable.name] = pickValues(variable, [], previous); continue }
      try {
        // “全部”由选择框单独提供，服务端返回的 $__all 选项不重复显示。
        varOptions[variable.name] = (await loadOptions(variable)).filter(option => option.value !== ALL)
        delete varErrors[variable.name]
      } catch (e) {
        if (isAbort(e)) return
        varOptions[variable.name] = varOptions[variable.name] || []
        varErrors[variable.name] = opsErrorText(e)
      }
      const next = pickValues(variable, varOptions[variable.name], previous)
      if (JSON.stringify(next) !== JSON.stringify(varValues[variable.name])) changed.add(variable.name)
      varValues[variable.name] = next
    }
  } finally { varLoading.value = false }
}
async function setVariable(variable, value) {
  let values = Array.isArray(value) ? value.map(String) : value == null || value === '' ? [] : [String(value)]
  if (values.length > 1 && values.includes(ALL)) values = values[values.length - 1] === ALL ? [ALL] : values.filter(v => v !== ALL)
  varValues[variable.name] = values
  await loadVariables([variable.name])
  loadPanels()
}
function variableText(name) {
  const values = varValues[name] || []
  if (values.includes(ALL)) return '全部'
  const options = varOptions[name] || []
  return values.map(v => options.find(o => o.value === v)?.text ?? v).join(', ')
}
function interpolate(text) {
  if (!text || !String(text).includes('$')) return text || ''
  return String(text).replace(/\$\{(\w+)(?::\w+)?\}|\$(\w+)|\[\[(\w+)(?::\w+)?\]\]/g, (match, a, b, c) => {
    const name = a || b || c
    return varValues[name] ? variableText(name) : match
  })
}

// 面板数据：同一面板只保留最新请求，整体并发不超过 MAX_PARALLEL。
const panelState = reactive({})
const controllers = new Map()
let queue = []
let active = 0
function visiblePanels() {
  return layout.value.flatMap(section => (section.row?.collapsed ? [] : section.panels)).filter(p => !['row', 'text'].includes(p.type) && supportByPanel.value[p.id]?.status !== 'unsupported')
}
function loadPanels() {
  queue = []
  for (const controller of controllers.values()) controller.abort()
  controllers.clear()
  active = 0
  for (const panel of visiblePanels()) {
    panelState[panel.id] = { ...(panelState[panel.id] || {}), loading: true, error: '' }
    queue.push(panel)
  }
  pump()
}
function pump() {
  while (active < MAX_PARALLEL && queue.length) {
    const panel = queue.shift()
    active++
    fetchPanel(panel).finally(() => { active = Math.max(0, active - 1); pump() })
  }
}
async function fetchPanel(panel) {
  const controller = new AbortController()
  controllers.get(panel.id)?.abort()
  controllers.set(panel.id, controller)
  const { from, to } = resolveRange(range.value)
  const maxDataPoints = Math.max(100, Math.min(1500, Math.round(((panel.gridPos?.w || 12) / 24) * (window.innerWidth || 1200))))
  try {
    let data
    if (usePreview.value) {
      if (!canPreview.value) throw new Error('当前账户没有预览权限，保存后可查看面板数据')
      data = await opsSend('POST', '/api/v1/ops/dashboards/preview', { dashboard: working.value, panel, from, to, vars: varsParam(), maxDataPoints }, controller.signal)
    } else {
      data = await opsGet(`/api/v1/ops/dashboards/${encodeURIComponent(view.value.dashboard.uid)}/panels/${panel.id}/data`, { from, to, vars: varsParam(), maxDataPoints }, controller.signal)
    }
    if (controllers.get(panel.id) !== controller) return
    panelState[panel.id] = { data, loading: false, error: '' }
  } catch (e) {
    if (controllers.get(panel.id) !== controller || isAbort(e)) return
    panelState[panel.id] = { data: null, loading: false, error: opsErrorText(e) }
  } finally {
    if (controllers.get(panel.id) === controller) controllers.delete(panel.id)
  }
}
function cancelAll() { queue = []; for (const c of controllers.values()) c.abort(); controllers.clear(); active = 0 }

// 加载
async function load() {
  loading.value = true
  loadError.value = ''
  cancelAll()
  try {
    view.value = props.uid ? await opsGet(`/api/v1/ops/dashboards/${encodeURIComponent(props.uid)}`) : { dashboard: props.initial || emptyDashboard(), meta: {}, support: { level: 'full', panels: [], variables: [], notes: [] }, favorite: false }
    working.value = JSON.parse(JSON.stringify(view.value.dashboard))
    working.value.panels = working.value.panels || []
    folder.value = view.value.meta?.folderUid ?? props.folderUid ?? ''
    range.value = { from: working.value.time?.from || 'now-6h', to: working.value.time?.to || 'now' }
    refresh.value = refreshMs(working.value.refresh)
    dirty.value = false
    editing.value = !props.uid
    for (const key of Object.keys(varValues)) delete varValues[key]
    for (const key of Object.keys(panelState)) delete panelState[key]
    await loadVariables()
    loadPanels()
  } catch (e) { loadError.value = opsErrorText(e) } finally { loading.value = false }
}
watch(() => props.uid, load, { immediate: true })
onBeforeUnmount(cancelAll)

function onRange(value) { range.value = value; loadVariables(null, true).then(loadPanels) }
function zoom({ from, to }) { onRange({ from: Math.round(from), to: Math.round(to) }) }

// 编辑操作
function change(mutator) {
  mutator(working.value)
  dirty.value = true
  loadPanels()
}
function toggleSection(row) {
  if (editing.value) { change(d => toggleRow(d, row.id)); return }
  toggleRow(working.value, row.id)
  loadPanels()
}
const addTypes = [...editablePanelTypes, 'row']
function add(type) {
  const ds = props.dataSources.find(d => d.supported && d.isDefault) || props.dataSources.find(d => d.supported)
  const panel = newPanel(working.value, type, type === 'logs' ? props.dataSources.find(d => d.type === 'loki') || ds : ds)
  change(d => addPanel(d, panel))
  if (type !== 'row') openEditor(panel)
}
const editorVisible = ref(false)
const editorPanel = ref(null)
function openEditor(panel) { editorPanel.value = JSON.parse(JSON.stringify(panel)); editorVisible.value = true }
function applyPanel(panel) { change(d => replacePanel(d, panel)) }
async function panelCommand(panel, command) {
  if (command === 'edit') openEditor(panel)
  else if (command === 'duplicate') change(d => duplicatePanel(d, panel.id))
  else if (command === 'remove') {
    try { await UiMessageBox.confirm(`从仪表盘中移除面板“${panel.title || panel.id}”？保存后生效。`, '移除面板') } catch { return }
    change(d => removePanel(d, panel.id))
  } else if (['up', 'down', 'left', 'right'].includes(command)) change(d => movePanel(d, panel.id, command))
  else if (command === 'inspect') { inspectPanel.value = panel; inspectVisible.value = true }
  else if (command === 'fullscreen') { fullPanel.value = panel }
  else if (command === 'metrics' || command === 'logs') explore(panel, command)
}
// 名称输入框：重命名分组与复制仪表盘共用。
const nameDialog = ref({ visible: false, title: '', label: '', value: '', resolve: null })
function askName(title, label, value) {
  return new Promise(resolve => { nameDialog.value = { visible: true, title, label, value, resolve } })
}
function closeName(ok) {
  const { resolve, value } = nameDialog.value
  nameDialog.value = { ...nameDialog.value, visible: false, resolve: null }
  resolve?.(ok && value.trim() ? value.trim() : null)
}
async function rowCommand(row, command) {
  if (command === 'rename') {
    const title = await askName('重命名分组', '分组名称', row.title || '')
    if (title) change(d => { findPanel(d, row.id).title = title })
  } else if (command === 'remove') {
    const count = row.collapsed ? (row.panels || []).length : 0
    try { await UiMessageBox.confirm(count ? `删除分组“${row.title}”及其中 ${count} 个面板？` : `删除分组“${row.title}”？分组下的面板会保留。`, '删除分组') } catch { return }
    change(d => removePanel(d, row.id))
  }
}
function panelMenu(panel) {
  const items = [{ command: 'fullscreen', label: '放大查看' }]
  if (!['text', 'row'].includes(panel.type)) items.push({ command: 'inspect', label: '查看查询与错误' })
  const exprs = executed(panel)
  if (exprs.some(q => q.type === 'prometheus') && canQueryMetrics.value) items.push({ command: 'metrics', label: '在指标中心打开' })
  if (exprs.some(q => q.type === 'loki') && canQueryLogs.value) items.push({ command: 'logs', label: '在日志中心打开' })
  return items
}
function executed(panel) { return panelState[panel.id]?.data?.queries || [] }
function explore(panel, kind) {
  const query = executed(panel).find(q => q.type === (kind === 'metrics' ? 'prometheus' : 'loki'))
  if (!query) return
  emit('navigate', kind === 'metrics' ? 'opsMetrics' : 'opsLogs', { query: query.expr, range: range.value })
}
const inspectVisible = ref(false)
const inspectPanel = ref(null)
const fullPanel = ref(null)
const supportVisible = ref(false)

// 窄屏时面板按顺序单列排列，高度按原比例但不超过 360 像素。
const narrowQuery = window.matchMedia('(max-width: 767px)')
const narrow = ref(narrowQuery.matches)
const onNarrow = event => { narrow.value = event.matches }
narrowQuery.addEventListener('change', onNarrow)
onBeforeUnmount(() => narrowQuery.removeEventListener('change', onNarrow))
function gridStyle(panels) {
  const minY = panels.length ? Math.min(...panels.map(p => p.gridPos?.y || 0)) : 0
  const ordered = narrow.value ? [...panels].sort((a, b) => (a.gridPos?.y || 0) - (b.gridPos?.y || 0) || (a.gridPos?.x || 0) - (b.gridPos?.x || 0)) : panels
  return ordered.map(panel => {
    const g = { x: 0, y: 0, w: 12, h: 8, ...(panel.gridPos || {}) }
    const height = g.h * ROW_HEIGHT + (g.h - 1) * 8
    if (narrow.value) return { panel, style: { gridColumn: '1 / -1', height: `${Math.min(height, 360)}px` } }
    return { panel, style: { gridColumn: `${g.x + 1} / span ${g.w}`, gridRow: `${g.y - minY + 1} / span ${g.h}` } }
  })
}

// 设置、保存、放弃
const settingsVisible = ref(false)
function applySettings({ dashboard, folderUid }) {
  working.value = normalizeLayout(dashboard)
  folder.value = folderUid
  dirty.value = true
  range.value = { from: working.value.time?.from || range.value.from, to: working.value.time?.to || 'now' }
  refresh.value = refreshMs(working.value.refresh)
  loadVariables().then(loadPanels)
}
const saveVisible = ref(false)
const saveForm = ref({ message: '', saveTime: false, saveVars: false })
const saving = ref(false)
const conflict = ref(false)
function openSave() { saveForm.value = { message: '', saveTime: false, saveVars: false }; conflict.value = false; saveVisible.value = true }
async function save(overwrite = false) {
  const dashboard = normalizeLayout(JSON.parse(JSON.stringify(working.value)))
  if (saveForm.value.saveTime) { dashboard.time = { from: range.value.from, to: range.value.to }; dashboard.refresh = refreshText(refresh.value) }
  if (saveForm.value.saveVars) {
    for (const variable of dashboard.templating?.list || []) {
      const values = varValues[variable.name]
      if (!values || variable.type === 'constant') continue
      const value = variable.multi ? values : values[0] ?? ''
      variable.current = { selected: true, text: values.includes(ALL) ? 'All' : variableText(variable.name), value }
    }
  }
  saving.value = true
  try {
    const body = { dashboard, folderUid: folder.value, message: saveForm.value.message, overwrite }
    const result = isNew.value ? await opsSend('POST', '/api/v1/ops/dashboards', body) : await opsSend('PUT', `/api/v1/ops/dashboards/${encodeURIComponent(dashboard.uid)}`, body)
    UiMessage.success(`已保存到 Grafana（版本 ${result.version ?? '—'}）`)
    saveVisible.value = false
    dirty.value = false
    editing.value = false
    if (isNew.value) emit('saved', result.uid)
    else { emit('saved', result.uid); load() }
  } catch (e) {
    if (e.status === 409 && e.code === 'OPS_CONFLICT') { conflict.value = true; return }
    UiMessage.error(opsErrorText(e))
  } finally { saving.value = false }
}
async function discard() {
  if (dirty.value) { try { await UiMessageBox.confirm('放弃所有未保存的修改？', '放弃修改') } catch { return } }
  if (isNew.value) { emit('close'); return }
  working.value = JSON.parse(JSON.stringify(view.value.dashboard))
  dirty.value = false
  editing.value = false
  await loadVariables()
  loadPanels()
}
async function back() {
  if (dirty.value) { try { await UiMessageBox.confirm('仪表盘有未保存的修改，离开将丢失这些修改。', '离开编辑', { confirmButtonText: '离开' }) } catch { return } }
  emit('close')
}

// 查看模式的其他操作
async function toggleFavorite() {
  const uid = view.value.dashboard.uid
  try {
    await opsSend(view.value.favorite ? 'DELETE' : 'PUT', `/api/v1/ops/preferences/favorites/${encodeURIComponent(uid)}`)
    view.value.favorite = !view.value.favorite
  } catch (e) { UiMessage.error(opsErrorText(e)) }
}
async function moreCommand(command) {
  const uid = view.value.dashboard.uid
  if (command === 'export' || command === 'export-external') {
    try { downloadJSON(await opsGet(`/api/v1/ops/dashboards/${encodeURIComponent(uid)}/export`, { external: command === 'export-external' }), `${working.value.title || uid}.json`) } catch (e) { UiMessage.error(opsErrorText(e)) }
  } else if (command === 'copy') {
    const title = await askName('复制仪表盘', '副本名称', `${working.value.title} 副本`)
    if (!title) return
    try {
      const result = await opsSend('POST', `/api/v1/ops/dashboards/${encodeURIComponent(uid)}/copy`, { title, folderUid: folder.value })
      UiMessage.success('已复制')
      emit('saved', result.uid)
    } catch (e) { UiMessage.error(opsErrorText(e)) }
  } else if (command === 'delete') {
    try { await UiMessageBox.confirm(`删除仪表盘“${working.value.title}”？删除后 Grafana 中的仪表盘也会被删除，无法在平台中恢复。`, '删除仪表盘', { confirmButtonText: '删除' }) } catch { return }
    try {
      await opsSend('DELETE', `/api/v1/ops/dashboards/${encodeURIComponent(uid)}`)
      UiMessage.success('仪表盘已删除')
      emit('deleted', uid)
    } catch (e) { UiMessage.error(opsErrorText(e)) }
  } else if (command === 'support') supportVisible.value = true
}
const moreItems = computed(() => {
  if (isNew.value) return []
  const items = [{ command: 'support', label: '支持情况' }, { command: 'export', label: '导出 JSON' }, { command: 'export-external', label: '导出为可共享 JSON（数据源改为导入参数）' }]
  if (canCopy.value) items.push({ command: 'copy', label: '复制仪表盘' })
  if (canDelete.value) items.push({ command: 'delete', label: '删除仪表盘' })
  return items
})
const statusTone = { supported: 'success', partial: 'warning', unsupported: 'danger' }
const statusText = { supported: '支持', partial: '部分支持', unsupported: '不支持' }
</script>

<template>
  <div class="dash-view">
    <header class="dash-view__head">
      <div class="dash-view__title">
        <ui-button text size="small" aria-label="返回列表" @click="back"><ArrowLeft /></ui-button>
        <div>
          <h2>{{ working?.title || '仪表盘' }}<small v-if="dirty" class="dirty">未保存</small></h2>
          <p>
            <span>{{ folders.find(f => f.uid === folder)?.title || '根目录' }}</span>
            <ui-tag v-for="tag in working?.tags || []" :key="tag" size="small">{{ tag }}</ui-tag>
            <button v-if="view?.support && !isNew" type="button" class="support-chip" :class="`is-${levelTone[view.support.level]}`" @click="supportVisible = true">{{ levelText[view.support.level] }}</button>
          </p>
        </div>
        <ui-button v-if="!isNew && view" text size="small" :aria-label="view.favorite ? '取消收藏' : '收藏'" @click="toggleFavorite"><Star :class="{ 'is-favorite': view.favorite }" /></ui-button>
      </div>
      <div class="dash-view__actions">
        <TimeRangeBar :range="range" v-model:refresh="refresh" :loading="varLoading || Object.values(panelState).some(s => s.loading)" @update:range="onRange" @refresh="loadVariables(null, true).then(loadPanels)" />
        <template v-if="!editing">
          <ui-button v-if="canEdit && view" size="small" @click="editing = true"><Pencil />编辑</ui-button>
          <ui-dropdown v-if="moreItems.length" @command="moreCommand">
            <ui-button size="small"><Download />更多</ui-button>
            <template #dropdown><ui-dropdown-menu><ui-dropdown-item v-for="item in moreItems" :key="item.command" :command="item.command">{{ item.label }}</ui-dropdown-item></ui-dropdown-menu></template>
          </ui-dropdown>
        </template>
        <template v-else>
          <ui-dropdown @command="add">
            <ui-button size="small"><Plus />添加</ui-button>
            <template #dropdown><ui-dropdown-menu><ui-dropdown-item v-for="type in addTypes" :key="type" :command="type">{{ panelTypeNames[type] }}</ui-dropdown-item></ui-dropdown-menu></template>
          </ui-dropdown>
          <ui-button size="small" @click="settingsVisible = true"><Settings />设置</ui-button>
          <ui-button size="small" @click="discard"><X />{{ isNew ? '取消' : '退出编辑' }}</ui-button>
          <ui-button size="small" type="primary" :disabled="!dirty && !isNew" @click="openSave"><Save />保存</ui-button>
        </template>
      </div>
    </header>

    <ui-alert v-if="loadError" type="error" :title="loadError" :closable="false" show-icon />
    <ui-alert v-if="editing && !canPreview" type="info" :closable="false" title="当前账户没有面板预览权限：编辑中的面板不会执行查询，保存后可查看数据。" />

    <div v-if="variables.some(v => v.hide !== 2)" class="var-bar">
      <template v-for="variable in variables" :key="variable.name">
        <label v-if="variable.hide !== 2 && variable.type !== 'constant'" class="var-item">
          <span v-if="variable.hide !== 1">{{ variable.label || variable.name }}</span>
          <ui-input v-if="variable.type === 'textbox'" :model-value="(varValues[variable.name] || [])[0] || ''" size="small" :aria-label="variable.label || variable.name" @change="value => setVariable(variable, value)" @keydown.enter="event => setVariable(variable, event.target.value)" />
          <ui-select v-else :model-value="variable.multi ? varValues[variable.name] || [] : (varValues[variable.name] || [])[0]" :multiple="Boolean(variable.multi)" filterable size="small" collapse-tags :aria-label="variable.label || variable.name" @update:model-value="value => setVariable(variable, value)">
            <ui-option v-if="variable.includeAll" :value="ALL" label="全部" />
            <ui-option v-for="option in varOptions[variable.name] || []" :key="option.value" :value="option.value" :label="option.text" />
          </ui-select>
          <small v-if="varErrors[variable.name]" class="var-error" :title="varErrors[variable.name]">可选值加载失败</small>
        </label>
      </template>
    </div>

    <ui-skeleton v-if="loading && !working" :rows="6" animated />
    <ui-empty v-else-if="working && !layout.length" :description="editing ? '点击“添加”加入第一个面板' : '这个仪表盘还没有面板'" :image-size="64" />

    <template v-for="(section, index) in layout" :key="section.row?.id ?? `top-${index}`">
      <div v-if="section.row" class="dash-row">
        <button type="button" class="dash-row__toggle" :aria-expanded="!section.row.collapsed" @click="toggleSection(section.row)">
          <component :is="section.row.collapsed ? ChevronRight : ChevronDown" />
          <strong>{{ interpolate(section.row.title) || '分组' }}</strong>
          <small v-if="section.row.collapsed">（{{ section.panels.length }} 个面板）</small>
        </button>
        <template v-if="editing">
          <ui-button text size="small" @click="rowCommand(section.row, 'rename')">重命名</ui-button>
          <ui-button text size="small" type="danger" @click="rowCommand(section.row, 'remove')">删除分组</ui-button>
        </template>
      </div>
      <div v-if="!section.row?.collapsed" class="dash-grid">
        <div v-for="{ panel, style } in gridStyle(section.panels)" :key="panel.id" class="dash-grid__cell" :style="style">
          <DashboardPanel :panel="panel" :title="interpolate(panel.title)" :data="panelState[panel.id]?.data" :loading="panelState[panel.id]?.loading" :error="panelState[panel.id]?.error" :support="supportByPanel[panel.id]" :editing="editing" :menu="panelMenu(panel)" sync-key="ops-dashboard" @zoom="zoom" @command="command => panelCommand(panel, command)" />
        </div>
      </div>
    </template>

    <PanelEditor v-model="editorVisible" :panel="editorPanel" :dashboard="working" :data-sources="dataSources" :range="resolveRange(range)" :vars="varsParam()" @apply="applyPanel" />
    <DashboardSettings v-model="settingsVisible" :dashboard="working" :folder-uid="folder" :folders="folders" :data-sources="dataSources" @apply="applySettings" />

    <ui-dialog v-model="saveVisible" :title="isNew ? '保存新仪表盘' : '保存仪表盘'" width="min(520px, 94vw)">
      <div class="save-form">
        <ui-alert v-if="conflict" type="warning" :closable="false" title="仪表盘已被其他人修改（Grafana 版本冲突）。可以放弃修改重新加载，或确认后覆盖对方的修改。" />
        <label>修改说明<ui-input v-model="saveForm.message" type="textarea" :rows="2" maxlength="500" placeholder="可选，保存在 Grafana 版本记录中" /></label>
        <ui-checkbox v-model="saveForm.saveTime">把当前时间范围和刷新间隔保存为默认</ui-checkbox>
        <ui-checkbox v-if="variables.length" v-model="saveForm.saveVars">把当前变量选择保存为默认</ui-checkbox>
      </div>
      <template #footer>
        <ui-button @click="saveVisible = false">取消</ui-button>
        <template v-if="conflict"><ui-button @click="saveVisible = false; dirty = false; load()">放弃修改并重新加载</ui-button><ui-button type="danger" :loading="saving" @click="save(true)">覆盖保存</ui-button></template>
        <ui-button v-else type="primary" :loading="saving" @click="save(false)">保存</ui-button>
      </template>
    </ui-dialog>

    <ui-dialog :model-value="nameDialog.visible" :title="nameDialog.title" width="min(420px, 94vw)" @update:model-value="value => !value && closeName(false)">
      <label class="name-field">{{ nameDialog.label }}<ui-input v-model="nameDialog.value" maxlength="200" @keydown.enter="closeName(true)" /></label>
      <template #footer><ui-button @click="closeName(false)">取消</ui-button><ui-button type="primary" :disabled="!nameDialog.value.trim()" @click="closeName(true)">确定</ui-button></template>
    </ui-dialog>

    <ui-dialog v-model="inspectVisible" title="面板查询" width="min(760px, 94vw)">
      <div v-if="inspectPanel" class="inspect">
        <p v-if="!executed(inspectPanel).length" class="muted">尚未执行查询。</p>
        <article v-for="q in executed(inspectPanel)" :key="q.refId"><header><strong>{{ q.refId }}</strong><small>{{ q.dataSource }} · {{ q.type }}</small></header><pre>{{ q.expr }}</pre></article>
        <p v-if="panelState[inspectPanel.id]?.data?.intervalMs" class="muted">查询间隔 {{ panelState[inspectPanel.id].data.intervalMs / 1000 }} 秒</p>
        <ui-alert v-for="(msg, ref) in panelState[inspectPanel.id]?.data?.errors || {}" :key="ref" type="error" :closable="false" :title="`${ref}：${msg}`" />
        <ui-alert v-if="panelState[inspectPanel.id]?.error" type="error" :closable="false" :title="panelState[inspectPanel.id].error" />
        <ui-alert v-for="w in panelState[inspectPanel.id]?.data?.warnings || []" :key="w" type="warning" :closable="false" :title="w" />
      </div>
    </ui-dialog>

    <ui-dialog :model-value="Boolean(fullPanel)" :title="fullPanel ? interpolate(fullPanel.title) : ''" width="96vw" @update:model-value="value => !value && (fullPanel = null)">
      <div v-if="fullPanel" class="full-panel"><DashboardPanel :panel="fullPanel" :data="panelState[fullPanel.id]?.data" :loading="panelState[fullPanel.id]?.loading" :error="panelState[fullPanel.id]?.error" :support="supportByPanel[fullPanel.id]" @zoom="zoom" /></div>
    </ui-dialog>

    <ui-dialog v-model="supportVisible" title="平台支持情况" width="min(760px, 94vw)">
      <div v-if="view?.support" class="support">
        <p>整体：<strong :class="`tone-${levelTone[view.support.level]}`">{{ levelText[view.support.level] }}</strong>。仪表盘完整保存在 Grafana 中，平台原生渲染常用面板与 Prometheus / Loki 查询，下列未支持的部分不会显示，但也不会被删除。</p>
        <ui-table :data="view.support.panels" size="small" :row-key="row => `p-${row.id}`" empty-text="没有面板">
          <ui-table-column label="面板" min-width="160"><template #default="{ row }">{{ row.name || `#${row.id}` }}</template></ui-table-column>
          <ui-table-column label="类型" width="110"><template #default="{ row }">{{ panelTypeNames[row.type] || row.type }}</template></ui-table-column>
          <ui-table-column label="状态" width="100"><template #default="{ row }"><span :class="`tone-${statusTone[row.status]}`">{{ statusText[row.status] }}</span></template></ui-table-column>
          <ui-table-column label="说明" min-width="220"><template #default="{ row }">{{ (row.reasons || []).join('；') || '—' }}</template></ui-table-column>
        </ui-table>
        <ui-table v-if="view.support.variables.length" :data="view.support.variables" size="small" :row-key="row => `v-${row.name}`">
          <ui-table-column label="变量" min-width="160"><template #default="{ row }">${{ row.name }}</template></ui-table-column>
          <ui-table-column label="状态" width="100"><template #default="{ row }"><span :class="`tone-${statusTone[row.status]}`">{{ statusText[row.status] }}</span></template></ui-table-column>
          <ui-table-column label="说明" min-width="220"><template #default="{ row }">{{ (row.reasons || []).join('；') || '—' }}</template></ui-table-column>
        </ui-table>
        <ul v-if="view.support.notes.length"><li v-for="note in view.support.notes" :key="note">{{ note }}</li></ul>
      </div>
    </ui-dialog>
  </div>
</template>

<style scoped>
.dash-view { display: grid; gap: var(--space-3); min-width: 0; }
.dash-view__head { display: flex; align-items: flex-start; justify-content: space-between; flex-wrap: wrap; gap: var(--space-3); }
.dash-view__title { display: flex; align-items: flex-start; gap: var(--space-2); min-width: 0; }
.dash-view__title h2 { display: flex; align-items: center; gap: var(--space-2); margin: 0; color: var(--text-strong); font-size: var(--font-size-lg); font-weight: var(--font-weight-semibold); overflow-wrap: anywhere; }
.dash-view__title p { display: flex; align-items: center; flex-wrap: wrap; gap: 6px; margin: 4px 0 0; color: var(--text-muted); font-size: var(--font-size-xs); }
.dirty { padding: 1px 6px; color: var(--warning-text); font-size: var(--font-size-xs); font-weight: 400; background: var(--warning-soft); border-radius: var(--radius-sm); }
.is-favorite { color: var(--warning); fill: currentColor; }
.support-chip { padding: 1px 8px; font-size: var(--font-size-xs); background: var(--surface-muted); border: 1px solid var(--border); border-radius: 999px; cursor: pointer; }
.support-chip.is-success { color: var(--success-text); }
.support-chip.is-warning { color: var(--warning-text); border-color: var(--warning-border); }
.support-chip.is-danger { color: var(--danger-text); border-color: var(--danger-border); }
.dash-view__actions { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-2); }
.var-bar { display: flex; flex-wrap: wrap; gap: var(--space-2) var(--space-4); }
.var-item { display: flex; align-items: center; gap: var(--space-2); color: var(--text-secondary); font-size: var(--font-size-sm); }
.var-item > span { flex: none; white-space: nowrap; }
.var-item .ui-select, .var-item .ui-input { min-width: 160px; max-width: 320px; }
.var-error { color: var(--danger-text); font-size: var(--font-size-xs); }
.dash-row { display: flex; align-items: center; gap: var(--space-2); padding-bottom: 4px; border-bottom: 1px solid var(--border); }
.dash-row__toggle { display: inline-flex; align-items: center; gap: 6px; padding: 0; color: var(--text-strong); background: none; border: 0; cursor: pointer; }
.dash-row__toggle svg { width: 16px; height: 16px; }
.dash-row__toggle small { color: var(--text-muted); font-size: var(--font-size-xs); }
.dash-grid { display: grid; grid-template-columns: repeat(24, minmax(0, 1fr)); grid-auto-rows: 30px; gap: 8px; min-width: 0; }
.dash-grid__cell { min-width: 0; min-height: 0; }
.save-form { display: grid; gap: var(--space-3); }
.name-field { display: grid; gap: 6px; color: var(--text-secondary); font-size: var(--font-size-sm); }
.save-form label { display: grid; gap: 6px; color: var(--text-secondary); font-size: var(--font-size-sm); }
.inspect { display: grid; gap: var(--space-2); }
.inspect article header { display: flex; align-items: baseline; gap: var(--space-2); }
.inspect small, .muted { color: var(--text-muted); font-size: var(--font-size-xs); }
.inspect pre { margin: 4px 0 0; padding: var(--space-2); color: var(--code-text); background: var(--code-bg); border-radius: var(--radius-sm); font: var(--font-size-xs) var(--font-mono); white-space: pre-wrap; word-break: break-all; }
.full-panel { height: 70vh; }
.support { display: grid; gap: var(--space-3); font-size: var(--font-size-sm); }
.support p { margin: 0; color: var(--text-secondary); }
.support ul { margin: 0; padding-left: 20px; color: var(--text-secondary); }
.tone-success { color: var(--success-text); }
.tone-warning { color: var(--warning-text); }
.tone-danger { color: var(--danger-text); }
@media (max-width: 767px) {
  .dash-grid { grid-template-columns: minmax(0, 1fr); grid-auto-rows: auto; }
}
</style>
