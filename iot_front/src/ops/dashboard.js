// 仪表盘 JSON 模型的布局与编辑辅助。仪表盘以 Grafana JSON 保存在 Grafana 中，
// 这里只调整面板、行和变量，其他字段原样保留。

export const GRID_COLUMNS = 24
export const ROW_HEIGHT = 30

export const panelTypeNames = { timeseries: '时序图', graph: '时序图（旧版）', stat: '统计卡片', gauge: '仪表', bargauge: '条形仪表', table: '表格', logs: '日志', text: '文本', row: '分组行' }
export const editablePanelTypes = ['timeseries', 'stat', 'gauge', 'bargauge', 'table', 'logs', 'text']

const clone = value => JSON.parse(JSON.stringify(value ?? null))

// sections 按分组行拆分顶层面板：展开的行包含其后直到下一行的面板，折叠的行包含 row.panels。
export function sections(dashboard) {
  const out = [{ row: null, panels: [] }]
  for (const panel of dashboard?.panels || []) {
    if (panel.type === 'row') out.push({ row: panel, panels: panel.collapsed ? panel.panels || [] : [] })
    else out[out.length - 1].panels.push(panel)
  }
  return out.filter((section, index) => index > 0 || section.panels.length)
}

const pos = panel => ({ x: 0, y: 0, w: 12, h: 8, ...(panel.gridPos || {}) })
const overlaps = (a, b) => a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h

// compact 按 (y, x) 顺序把面板向上压紧，避免重叠，行为与 Grafana 的网格布局一致。
export function compact(panels) {
  const placed = []
  const ordered = [...panels].sort((a, b) => pos(a).y - pos(b).y || pos(a).x - pos(b).x)
  for (const panel of ordered) {
    const p = pos(panel)
    p.w = Math.min(Math.max(1, Math.round(p.w)), GRID_COLUMNS)
    p.h = Math.max(1, Math.round(p.h))
    p.x = Math.min(Math.max(0, Math.round(p.x)), GRID_COLUMNS - p.w)
    p.y = 0
    while (placed.some(other => overlaps(p, other))) p.y++
    placed.push(p)
    panel.gridPos = p
  }
  return ordered
}

// normalizeLayout 重新计算每个分组的绝对纵坐标并写回仪表盘，保存前调用。
export function normalizeLayout(dashboard) {
  const top = []
  let cursor = 0
  for (const section of sections(dashboard)) {
    if (section.row) {
      section.row.gridPos = { x: 0, y: cursor, w: GRID_COLUMNS, h: 1 }
      top.push(section.row)
      cursor += 1
    }
    const base = cursor
    const offset = minY(section.panels)
    const ordered = compact(section.panels.map(panel => { const p = pos(panel); panel.gridPos = { ...p, y: p.y - offset }; return panel }))
    let bottom = base
    for (const panel of ordered) {
      panel.gridPos.y += base
      bottom = Math.max(bottom, panel.gridPos.y + panel.gridPos.h)
    }
    if (section.row?.collapsed) section.row.panels = ordered
    else {
      if (section.row) section.row.panels = []
      top.push(...ordered)
      cursor = bottom
    }
  }
  dashboard.panels = top
  return dashboard
}

function minY(panels) {
  return panels.length ? Math.min(...panels.map(p => pos(p).y)) : 0
}

export function nextPanelId(dashboard) {
  let max = 0
  for (const panel of dashboard?.panels || []) {
    max = Math.max(max, Number(panel.id) || 0)
    for (const child of panel.panels || []) max = Math.max(max, Number(child.id) || 0)
  }
  return max + 1
}

export function findPanel(dashboard, id) {
  for (const panel of dashboard?.panels || []) {
    if (panel.id === id) return panel
    for (const child of panel.panels || []) if (child.id === id) return child
  }
  return null
}

function sectionOf(dashboard, id) {
  return sections(dashboard).find(section => section.row?.id === id || section.panels.some(panel => panel.id === id))
}

export function defaultTarget(dataSource, refId = 'A') {
  if (dataSource?.type === 'loki') return { refId, datasource: { type: 'loki', uid: dataSource.uid }, expr: '', queryType: 'range', editorMode: 'code' }
  return { refId, datasource: dataSource ? { type: dataSource.type, uid: dataSource.uid } : undefined, expr: '', legendFormat: '', range: true, instant: false, editorMode: 'code' }
}

export function newPanel(dashboard, type, dataSource) {
  const panel = { id: nextPanelId(dashboard), type, title: '新面板', gridPos: { x: 0, y: 9999, w: type === 'stat' || type === 'gauge' ? 6 : 12, h: type === 'stat' ? 4 : 8 } }
  if (type === 'row') return { ...panel, title: '新分组', collapsed: false, panels: [], gridPos: { x: 0, y: 9999, w: GRID_COLUMNS, h: 1 } }
  if (type === 'text') return { ...panel, options: { mode: 'markdown', content: '' } }
  panel.datasource = dataSource ? { type: dataSource.type, uid: dataSource.uid } : undefined
  panel.targets = [defaultTarget(dataSource)]
  panel.fieldConfig = { defaults: { unit: 'short', thresholds: { mode: 'absolute', steps: [{ color: 'green', value: null }, { color: 'red', value: 80 }] } }, overrides: [] }
  panel.options = type === 'logs' ? { showTime: true, wrapLogMessage: true, sortOrder: 'Descending', enableLogDetails: true } : { reduceOptions: { calcs: ['lastNotNull'], fields: '', values: false }, legend: { showLegend: true, displayMode: 'list', placement: 'bottom' } }
  return panel
}

// addPanel 把新面板放到最后一个分组末尾；分组行追加在最后。
export function addPanel(dashboard, panel) {
  dashboard.panels = dashboard.panels || []
  const all = sections(dashboard)
  const last = all[all.length - 1]
  if (panel.type !== 'row' && last?.row?.collapsed) last.row.panels.push(panel)
  else dashboard.panels.push(panel)
  return normalizeLayout(dashboard)
}

export function removePanel(dashboard, id) {
  const target = findPanel(dashboard, id)
  if (target?.type === 'row' && !target.collapsed) {
    dashboard.panels = dashboard.panels.filter(panel => panel.id !== id)
  } else {
    dashboard.panels = (dashboard.panels || []).filter(panel => panel.id !== id)
    for (const panel of dashboard.panels) if (panel.panels) panel.panels = panel.panels.filter(child => child.id !== id)
  }
  return normalizeLayout(dashboard)
}

export function duplicatePanel(dashboard, id) {
  const source = findPanel(dashboard, id)
  if (!source || source.type === 'row') return dashboard
  const copy = clone(source)
  copy.id = nextPanelId(dashboard)
  copy.title = `${source.title || '面板'} 副本`
  copy.gridPos = { ...pos(source), y: pos(source).y + pos(source).h }
  const holder = (dashboard.panels || []).find(panel => (panel.panels || []).some(child => child.id === id))
  if (holder) holder.panels.push(copy)
  else dashboard.panels.splice(dashboard.panels.indexOf(source) + 1, 0, copy)
  return normalizeLayout(dashboard)
}

// movePanel 在同一分组内移动面板：left/right 调整横坐标，up/down 与相邻面板交换位置。
export function movePanel(dashboard, id, direction) {
  const section = sectionOf(dashboard, id)
  const panel = findPanel(dashboard, id)
  if (!section || !panel || panel.type === 'row') return dashboard
  const p = pos(panel)
  if (direction === 'left') p.x = Math.max(0, p.x - Math.max(1, Math.round(p.w / 2)))
  if (direction === 'right') p.x = Math.min(GRID_COLUMNS - p.w, p.x + Math.max(1, Math.round(p.w / 2)))
  if (direction === 'up' || direction === 'down') {
    const ordered = [...section.panels].sort((a, b) => pos(a).y - pos(b).y || pos(a).x - pos(b).x)
    const index = ordered.indexOf(panel)
    const other = ordered[direction === 'up' ? index - 1 : index + 1]
    if (other) {
      const o = pos(other)
      panel.gridPos = { ...p, x: o.x, y: o.y }
      other.gridPos = { ...o, x: p.x, y: p.y }
      return normalizeLayout(dashboard)
    }
  }
  panel.gridPos = p
  return normalizeLayout(dashboard)
}

export function resizePanel(dashboard, id, { w, h }) {
  const panel = findPanel(dashboard, id)
  if (!panel) return dashboard
  const p = pos(panel)
  if (w != null) p.w = Math.min(GRID_COLUMNS, Math.max(2, w))
  if (h != null) p.h = Math.min(40, Math.max(2, h))
  p.x = Math.min(p.x, GRID_COLUMNS - p.w)
  panel.gridPos = p
  return normalizeLayout(dashboard)
}

// toggleRow 与 Grafana 一致：折叠时把行下面板收进 row.panels，展开时放回顶层。
export function toggleRow(dashboard, id) {
  const index = (dashboard.panels || []).findIndex(panel => panel.id === id && panel.type === 'row')
  if (index < 0) return dashboard
  const row = dashboard.panels[index]
  if (row.collapsed) {
    const children = row.panels || []
    row.collapsed = false
    row.panels = []
    dashboard.panels.splice(index + 1, 0, ...children)
  } else {
    let end = index + 1
    while (end < dashboard.panels.length && dashboard.panels[end].type !== 'row') end++
    row.panels = dashboard.panels.splice(index + 1, end - index - 1)
    row.collapsed = true
  }
  return normalizeLayout(dashboard)
}

export function replacePanel(dashboard, panel) {
  const replace = list => list.map(item => (item.id === panel.id ? panel : { ...item, ...(item.panels ? { panels: replace(item.panels) } : {}) }))
  dashboard.panels = replace(dashboard.panels || [])
  return normalizeLayout(dashboard)
}

// 变量：当前值、引用关系与显示。
export const variableTypeNames = { query: '查询', custom: '自定义', constant: '常量', textbox: '文本框', interval: '时间间隔', datasource: '数据源' }

export function currentValues(variable) {
  const value = variable?.current?.value
  if (Array.isArray(value)) return value.map(String)
  if (value == null || value === '') return []
  return [String(value)]
}

export function variableRefs(text, name) {
  const escaped = name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return new RegExp(`\\$(?:${escaped}\\b|\\{${escaped}(?::[a-z]+)?\\})|\\[\\[${escaped}(?::[a-z]+)?\\]\\]`).test(String(text || ''))
}

// dependsOn 判断变量 b 的查询或正则是否引用了变量 a，用于级联刷新可选值。
export function dependsOn(variable, name) {
  const def = typeof variable.query === 'object' ? JSON.stringify(variable.query) : variable.query
  return variableRefs(def, name) || variableRefs(variable.regex, name) || variableRefs(JSON.stringify(variable.datasource || ''), name)
}

export function emptyDashboard(title = '新仪表盘') {
  return { title, tags: [], timezone: 'browser', schemaVersion: 39, editable: true, time: { from: 'now-6h', to: 'now' }, refresh: '', panels: [], templating: { list: [] }, annotations: { list: [] } }
}

// 仪表盘默认刷新间隔（如 "30s"）转成毫秒；空或无效时不自动刷新。
export function refreshMs(value) {
  const m = String(value || '').match(/^(\d+)(s|m|h)$/)
  return m ? Number(m[1]) * { s: 1e3, m: 60e3, h: 3600e3 }[m[2]] : 0
}

export function refreshText(ms) {
  if (!ms) return ''
  if (ms % 3600e3 === 0) return `${ms / 3600e3}h`
  if (ms % 60e3 === 0) return `${ms / 60e3}m`
  return `${Math.round(ms / 1e3)}s`
}
