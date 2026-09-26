import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import vm from 'node:vm'
import { ref, shallowRef, computed, watch, nextTick, isReactive } from 'vue'
import { clampAlertPage, pageAlertGroups, prepareAlertGroups, sortAlerts, summarizeAlerts } from '../src/ops/alerts.js'

const alert = (i, extra = {}) => ({ fingerprint: `alert-${String(i).padStart(5, '0')}`, startsAt: new Date(1700000000000 + i * 1000).toISOString(), state: i % 2 ? 'active' : 'suppressed', labels: { severity: i % 3 ? 'warning' : 'critical' }, receivers: ['mail'], ...extra })

test('万条告警排序只解析一次时间，分页不丢失记录且顺序稳定', () => {
  const input = Array.from({ length: 10000 }, (_, i) => alert(i))
  const sorted = sortAlerts(input)
  assert.equal(sorted[0].fingerprint, 'alert-09999')
  assert.equal(input[0].fingerprint, 'alert-00000')
  const groups = prepareAlertGroups([{ receiver: 'mail', labels: { job: 'api' }, alerts: input }])
  const seen = new Set()
  for (let page = 1; page <= 500; page++) {
    const visible = pageAlertGroups(groups, page, 20)
    assert.equal(visible.length, 1)
    assert.equal(visible[0].alerts.length, 20)
    assert.equal(visible[0].total, 10000)
    for (const row of visible[0].alerts) seen.add(row.fingerprint)
  }
  assert.equal(seen.size, 10000)
})

test('分组跨页共享容量，多接收人统计按告警指纹去重', () => {
  const groups = prepareAlertGroups([
    { receiver: 'a', labels: {}, alerts: [alert(0), alert(1), alert(2)] },
    { receiver: 'b', labels: {}, alerts: [alert(0, { receivers: ['other'] }), alert(3)] },
    { receiver: 'empty', labels: {}, alerts: [] }
  ])
  const visible = pageAlertGroups(groups, 2, 2)
  assert.deepEqual(visible.map(g => g.alerts.length), [1, 1])
  assert.deepEqual(visible.map(g => g.total), [3, 2])
  const stats = summarizeAlerts(groups.flatMap(g => g.alerts))
  assert.deepEqual(stats.summary, { total: 4, active: 2, suppressed: 2, critical: 2 })
  assert.deepEqual(stats.receivers, ['mail', 'other'])
  assert.equal(clampAlertPage(500, 21, 20), 2)
  assert.equal(clampAlertPage(3, 0, 20), 1)
})

function pageHarness() {
  const pending = []
  const timers = new Map()
  let timerID = 0
  let poll
  let version = 0
  const runner = {
    async run(task) {
      const current = ++version
      const result = await task({})
      if (current !== version) throw Object.assign(new Error('stale'), { name: 'AbortError' })
      return result
    },
    cancel() { version++ }
  }
  const source = readFileSync(new URL('../src/views/OpsAlertsView.vue', import.meta.url), 'utf8').split('<script setup>')[1].split('</script>')[0].replace(/^import .*$/gm, '')
  const context = vm.createContext({ ref, shallowRef, computed, watch, can: () => true, defineEmits: () => () => {}, onMounted() {}, onBeforeUnmount() {}, latest: () => runner, summarizeAlerts, sortAlerts, prepareAlertGroups, pageAlertGroups, clampAlertPage, opsErrorText: e => e.message, document: { hidden: false }, opsGet: (path, params) => new Promise((resolve, reject) => pending.push({ path, params, resolve, reject })), setInterval: fn => { poll = fn; return 1 }, clearInterval() {}, setTimeout: fn => { timers.set(++timerID, fn); return timerID }, clearTimeout: id => timers.delete(id) })
  vm.runInContext(source + '\nthis.page = {loadAlerts, grouped, alerts, visibleAlerts, visibleGroups, alertStats, alertPage, alertPageSize, alertsLoading, alertsError, filters, scheduleAlerts, tab}', context)
  return { ...context.page, pending, poll: () => poll(), debounce: () => { for (const fn of timers.values()) fn(); timers.clear() } }
}

test('页面只渲染当前页，刷新保留页码并在数据减少时回退', async () => {
  const p = pageHarness()
  const first = p.loadAlerts()
  p.pending[0].resolve({ items: Array.from({ length: 10000 }, (_, i) => alert(i)) })
  await first
  assert.equal(p.visibleAlerts.value.length, 20)
  assert.equal(isReactive(p.alerts.value[0]), false)
  p.alertPage.value = 4
  const refresh = p.loadAlerts()
  p.pending[1].resolve({ items: Array.from({ length: 100 }, (_, i) => alert(i)) })
  await refresh
  assert.equal(p.alertPage.value, 4)
  const shrink = p.loadAlerts()
  p.pending[2].resolve({ items: [alert(1)] })
  await shrink
  assert.equal(p.alertPage.value, 1)
  p.alertPageSize.value = 50
  await nextTick()
  assert.equal(p.alertPage.value, 1)
})

test('分组模式只请求分组接口，过期请求不影响新请求的加载状态', async () => {
  const p = pageHarness()
  const first = p.loadAlerts()
  p.grouped.value = true
  await nextTick()
  assert.deepEqual(p.pending.map(r => r.path), ['/api/v1/ops/alerts', '/api/v1/ops/alerts/groups'])
  p.pending[0].resolve({ items: [alert(1)] })
  await first
  assert.equal(p.alertsLoading.value, true)
  p.pending[1].resolve({ items: [{ receiver: 'mail', labels: {}, alerts: Array.from({ length: 10000 }, (_, i) => alert(i)) }] })
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(p.alertsLoading.value, false)
  assert.equal(p.visibleGroups.value[0].alerts.length, 20)
  assert.equal(p.alertStats.value.summary.total, 10000)
})

test('筛选防抖期间废弃旧数据，自动刷新不叠加请求，离开当前页取消请求', async () => {
  const p = pageHarness()
  p.scheduleAlerts()
  const first = p.loadAlerts()
  p.poll()
  assert.equal(p.pending.length, 1)
  p.filters.value = [{ name: 'job', op: '=', value: 'api' }]
  await nextTick()
  p.poll()
  assert.equal(p.pending.length, 1)
  p.pending[0].resolve({ items: [alert(1)] })
  await first
  assert.equal(p.alerts.value.length, 0)
  p.debounce()
  assert.equal(p.pending.length, 2)
  p.tab.value = 'notifications'
  await nextTick()
  p.pending[1].resolve({ items: [alert(2)] })
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(p.alerts.value.length, 0)
  assert.equal(p.alertsLoading.value, false)
})
