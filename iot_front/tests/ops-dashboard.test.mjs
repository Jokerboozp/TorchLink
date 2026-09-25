import test from 'node:test'
import assert from 'node:assert/strict'
import { addPanel, compact, dependsOn, duplicatePanel, movePanel, newPanel, normalizeLayout, removePanel, sections, toggleRow } from '../src/ops/dashboard.js'

const panel = (id, x, y, w, h, extra = {}) => ({ id, type: 'timeseries', gridPos: { x, y, w, h }, ...extra })

test('compact moves panels up without overlaps', () => {
  const panels = [panel(1, 0, 5, 12, 8), panel(2, 12, 20, 12, 8), panel(3, 0, 40, 24, 4)]
  compact(panels)
  assert.deepEqual(panels.map(p => p.gridPos.y), [0, 0, 8])
})

test('normalizeLayout stacks rows and keeps collapsed children inside the row', () => {
  const dash = { panels: [panel(1, 0, 0, 24, 6), { id: 2, type: 'row', collapsed: false, gridPos: { x: 0, y: 10, w: 24, h: 1 }, panels: [] }, panel(3, 0, 30, 12, 5), { id: 4, type: 'row', collapsed: true, gridPos: { x: 0, y: 50, w: 24, h: 1 }, panels: [panel(5, 0, 90, 12, 4)] }] }
  normalizeLayout(dash)
  assert.deepEqual(dash.panels.map(p => [p.id, p.gridPos.y]), [[1, 0], [2, 6], [3, 7], [4, 12]])
  assert.equal(dash.panels[3].panels[0].gridPos.y, 13)
  assert.equal(sections(dash).length, 3)
})

test('toggleRow moves panels between the row and the top level', () => {
  const dash = { panels: [{ id: 1, type: 'row', collapsed: false, panels: [], gridPos: { x: 0, y: 0, w: 24, h: 1 } }, panel(2, 0, 1, 12, 4), panel(3, 12, 1, 12, 4)] }
  toggleRow(dash, 1)
  assert.equal(dash.panels.length, 1)
  assert.equal(dash.panels[0].panels.length, 2)
  toggleRow(dash, 1)
  assert.deepEqual(dash.panels.map(p => p.id), [1, 2, 3])
})

test('add, duplicate, move and remove keep ids unique and layout compact', () => {
  const dash = { panels: [] }
  addPanel(dash, newPanel(dash, 'stat'))
  addPanel(dash, newPanel(dash, 'timeseries'))
  duplicatePanel(dash, 2)
  assert.deepEqual(dash.panels.map(p => p.id), [1, 2, 3])
  movePanel(dash, 3, 'up')
  const ids = dash.panels.map(p => p.id)
  assert.equal(new Set(ids).size, 3)
  removePanel(dash, 1)
  assert.deepEqual(dash.panels.map(p => p.id).sort(), [2, 3])
  assert.ok(dash.panels.every(p => p.gridPos.y >= 0 && p.gridPos.x + p.gridPos.w <= 24))
})

test('dependsOn detects Grafana variable reference syntaxes', () => {
  assert.ok(dependsOn({ query: 'label_values(up{job="$job"}, instance)' }, 'job'))
  assert.ok(dependsOn({ query: { query: 'label_values(up{job=~"${job:regex}"}, instance)' } }, 'job'))
  assert.ok(dependsOn({ query: 'x', regex: '/[[job]]/' }, 'job'))
  assert.ok(!dependsOn({ query: 'label_values(up{job="$jobs"}, instance)' }, 'job'))
})
