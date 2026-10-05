import test from 'node:test'
import assert from 'node:assert/strict'
import vm from 'node:vm'
import { computed, reactive, ref, toRef } from 'vue'
import { setupScript } from './helpers/vue.mjs'
import * as fireSafety from '../src/fireSafety.js'

function component(api, exports, overrides = {}) {
  const warnings = [],
    errors = []
  const context = vm.createContext({
    ...fireSafety,
    computed,
    reactive,
    ref,
    toRef,
    api,
    URLSearchParams,
    session: { user: 'operator' },
    can: () => true,
    defineEmits: () => () => {},
    onMounted() {},
    notifyError: error => errors.push(error),
    UiMessage: { success() {}, warning: value => warnings.push(value) },
    confirmDelete: async () => {},
    ...overrides
  })
  const source = setupScript(new URL('../src/views/DutyView.vue', import.meta.url))
  return { ...vm.runInContext(source + '\n;({' + exports + '})', context), warnings, errors }
}
const settle = () => new Promise(resolve => setImmediate(resolve))

test('班次按本地日期生成时段，跨日包含月底及年度边界', () => {
  const [startAt, endAt] = fireSafety.shiftRange('2026-12-31', '23:00', '07:00')
  assert.equal(fireSafety.toDateInput(startAt), '2026-12-31')
  assert.equal(fireSafety.toDateInput(endAt), '2027-01-01')
  assert.equal(new Date(startAt).getHours(), 23)
  assert.equal(new Date(endAt).getHours(), 7)
  const fullDay = fireSafety.shiftRange('2026-10-01', '08:00', '08:00')
  assert.equal(fireSafety.toDateInput(fullDay[1]), '2026-10-02')
  assert.throws(() => fireSafety.shiftRange('2026-02-30', '08:00', '17:00'))
  assert.throws(() => fireSafety.shiftRange('2026-02-28', '24:00', '17:00'))
  assert.throws(() => fireSafety.shiftRange('2026-02-28', '08:00', '8:00'))
})

test('月份与星期按日历边界生成，午夜结束的排班不重复进入次日', () => {
  const february = fireSafety.monthRange('2028-02-15')
  const days = fireSafety.calendarDays(february)
  assert.equal(days.length, 29)
  assert.equal(days[0].date, '2028-02-01')
  assert.equal(days.at(-1).date, '2028-02-29')
  const week = fireSafety.weekRange('2026-10-04')
  assert.equal(fireSafety.toDateInput(week[0]), '2026-09-28')
  assert.equal(fireSafety.toDateInput(week[1]), '2026-10-05')
  const row = { id: 'overnight', startAt: days[0].startAt + 20 * 3600e3, endAt: days[1].startAt }
  assert.equal(fireSafety.assignmentsForDay([row], days[0]).length, 1)
  assert.equal(fireSafety.assignmentsForDay([row], days[1]).length, 0)
  assert.equal(fireSafety.moveCalendar('2026-01-31', 'month', 1), '2026-02-01')
})

test('日历读取完整月份的有界分页，与普通列表页码相互独立', async () => {
  const requests = []
  const rows = Array.from({ length: 205 }, (_, i) => ({ id: `assignment-${i}`, personnelIds: [`person-${i % 3}`] }))
  const c = component(async path => {
    const query = new URL(path, 'http://test').searchParams
    requests.push(query)
    const start = (Number(query.get('page')) - 1) * 100
    return { items: rows.slice(start, start + 100), total: rows.length }
  }, 'loadCalendar,calendarRows,calendarTotal,calendarComplete,page,anchor,calendarRange,filters')
  c.page.assignments = 9
  c.anchor.value = '2026-10-15'
  c.filters.stationId = 'station/1'
  await c.loadCalendar()
  assert.equal(c.calendarRows.value.length, 205)
  assert.equal(c.calendarComplete.value, true)
  assert.equal(c.calendarTotal.value, 205)
  assert.deepEqual(
    requests.map(query => query.get('page')),
    ['1', '2', '3']
  )
  assert.ok(requests.every(query => query.get('pageSize') === '100' && query.get('stationId') === 'station/1'))
  assert.ok(
    requests.every(
      query => Number(query.get('fromAt')) === c.calendarRange.value[0] && Number(query.get('toAt')) === c.calendarRange.value[1]
    )
  )
  assert.equal(c.page.assignments, 9)
})

test('大月份按批次加载，明确保持未完成状态并可继续读取', async () => {
  const rows = Array.from({ length: 1001 }, (_, i) => ({ id: `assignment-${i}` }))
  const c = component(async path => {
    const query = new URL(path, 'http://test').searchParams
    const start = (Number(query.get('page')) - 1) * 100
    return { items: rows.slice(start, start + 100), total: rows.length }
  }, 'loadCalendar,calendarRows,calendarComplete,calendarNextPage')
  await c.loadCalendar()
  assert.equal(c.calendarRows.value.length, 1000)
  assert.equal(c.calendarComplete.value, false)
  assert.equal(c.calendarNextPage.value, 11)
  await c.loadCalendar({ append: true })
  assert.equal(c.calendarRows.value.length, 1001)
  assert.equal(c.calendarComplete.value, true)
})

test('切换月份后迟到的旧日历响应不会覆盖新月份', async () => {
  const pending = []
  const c = component(() => new Promise(resolve => pending.push(resolve)), 'loadCalendar,anchor,calendarRows,calendarLoading,calendarTotal')
  c.anchor.value = '2026-10-01'
  const old = c.loadCalendar()
  c.anchor.value = '2026-11-01'
  const current = c.loadCalendar()
  pending[1]({ items: [{ id: 'november' }], total: 1 })
  await current
  pending[0]({ items: [{ id: 'october' }], total: 999 })
  await old
  assert.equal(c.calendarRows.value[0].id, 'november')
  assert.equal(c.calendarTotal.value, 1)
  assert.equal(c.calendarLoading.value, false)
})

test('旧日历请求失败不会终止当前加载或显示过期错误', async () => {
  const pending = []
  const c = component(
    () => new Promise((resolve, reject) => pending.push({ resolve, reject })),
    'loadCalendar,calendarLoading,calendarError'
  )
  const old = c.loadCalendar(),
    current = c.loadCalendar()
  pending[0].reject(new Error('old error'))
  await old
  assert.equal(c.calendarLoading.value, true)
  assert.equal(c.calendarError.value, '')
  pending[1].resolve({ items: [], total: 0 })
  await current
  assert.equal(c.calendarLoading.value, false)
})

test('排班列表只接纳最近一次请求，删除末页后修正页码', async () => {
  const pending = []
  const c = component(() => new Promise(resolve => pending.push(resolve)), 'loadList,page,assignments,total,loading')
  const first = c.loadList('assignments')
  c.page.assignments = 2
  const second = c.loadList('assignments')
  pending[1]({ items: [{ id: 'current' }], total: 40 })
  await second
  pending[0]({ items: [{ id: 'old' }], total: 21 })
  await first
  assert.equal(c.assignments.value[0].id, 'current')
  assert.equal(c.total.assignments, 40)
  const deleted = c.loadList('assignments')
  pending[2]({ items: [], total: 10 })
  await settle()
  assert.equal(c.page.assignments, 1)
  pending[3]({ items: [{ id: 'remaining' }], total: 10 })
  await deleted
  assert.equal(c.assignments.value[0].id, 'remaining')
  assert.equal(c.loading.assignments, false)
})

test('多人排班提交保留版本与实际时段，重复点击只发送一次', async () => {
  let resolveSave
  const mutations = []
  const c = component(async (path, request) => {
    if (request?.method) {
      mutations.push({ path, request, body: JSON.parse(request.body) })
      await new Promise(resolve => {
        resolveSave = resolve
      })
    }
    return { items: [], total: 0 }
  }, 'assignment,saveAssignment,saving')
  const [startAt, endAt] = fireSafety.shiftRange('2026-10-01', '08:00', '17:00')
  Object.assign(c.assignment, {
    id: 'schedule/1',
    version: 7,
    stationId: 'station',
    shiftId: 'day',
    personnelIds: ['p1', 'p2'],
    startAt,
    endAt: startAt + 3 * 3600e3
  })
  const first = c.saveAssignment()
  await c.saveAssignment()
  assert.equal(mutations.length, 1)
  assert.equal(c.saving.value, true)
  assert.equal(mutations[0].path, '/api/v1/duty/assignments/schedule%2F1')
  assert.equal(mutations[0].body.version, 7)
  assert.deepEqual(mutations[0].body.personnelIds, ['p1', 'p2'])
  assert.notEqual(mutations[0].body.endAt, endAt, 'the edited actual range is preserved')
  resolveSave()
  await first
  assert.equal(c.saving.value, false)
})

test('换站清除跨站人员，自申请审批既不显示也不提交', async () => {
  const mutations = []
  const c = component(async (path, request) => {
    if (request?.method) mutations.push(path)
    return { items: [], total: 0 }
  }, 'assignment,options,changeAssignmentStation,canReview,reviewTarget,saveReview,openSwap,swap,swapToChoices')
  c.options.personnel = [
    { id: 'p1', stationId: 'one', enabled: true },
    { id: 'p2', stationId: 'two', enabled: true },
    { id: 'p3', stationId: 'two', enabled: true }
  ]
  Object.assign(c.assignment, { stationId: 'two', personnelIds: ['p1', 'p2'] })
  c.changeAssignmentStation()
  assert.deepEqual([...c.assignment.personnelIds], ['p2'])
  c.openSwap({ id: 'assignment', stationId: 'two', personnelIds: ['p2'] })
  assert.deepEqual(
    c.swapToChoices.value.map(row => row.id),
    ['p3']
  )
  c.reviewTarget.value = { id: 'swap', version: 2, status: 'pending', requestedBy: 'operator' }
  assert.equal(c.canReview(c.reviewTarget.value), false)
  await c.saveReview()
  assert.equal(mutations.length, 0)
})

test('没有对应写权限时保存排班及审批都不会发起请求', async () => {
  const mutations = []
  const c = component(
    async (path, request) => {
      if (request?.method) mutations.push(path)
      return { items: [], total: 0 }
    },
    'assignment,saveAssignment,reviewTarget,saveReview',
    { can: () => false }
  )
  Object.assign(c.assignment, { stationId: 'station', shiftId: 'day', personnelIds: ['person'], startAt: 1, endAt: 2 })
  await c.saveAssignment()
  c.reviewTarget.value = { id: 'swap', version: 2, status: 'pending', requestedBy: 'another-user' }
  await c.saveReview()
  assert.equal(mutations.length, 0)
})

test('换班列表和审批使用服务端关联排班，跨月及列表分页外的排班仍可核对', async () => {
  const [startAt, endAt] = fireSafety.shiftRange('2026-12-31', '23:00', '07:00')
  const linked = { id: 'outside-current-page', stationId: 'station-outside', shiftId: 'night', personnelIds: ['p1'], startAt, endAt }
  const row = { id: 'swap', assignmentId: linked.id, assignment: linked, status: 'pending', requestedBy: 'another-user' }
  const c = component(
    async () => ({ items: [row], total: 1 }),
    'options,loadList,swaps,calendarRows,assignments,assignmentLabel,swapContext,openReview,reviewAssignment'
  )
  c.options.stations = [{ id: 'station-outside', name: '西区站' }]
  c.options.shifts = [{ id: 'night', name: '夜班' }]
  c.calendarRows.value = [{ id: 'this-month-only' }]
  c.assignments.value = [{ id: 'first-page-only' }]
  await c.loadList('swaps')
  const loaded = c.swaps.value[0]
  assert.equal(c.swapContext(loaded).id, linked.id)
  const expected = `西区站 · 夜班 · ${fireSafety.dateTimeLabel(startAt)} — ${fireSafety.dateTimeLabel(endAt)}`
  assert.equal(c.assignmentLabel(loaded), expected)
  c.openReview(loaded)
  assert.equal(c.reviewAssignment.value.startAt, startAt)
  assert.equal(c.reviewAssignment.value.endAt, endAt)
  // A stale local calendar copy must not override the attached current record.
  c.calendarRows.value = [{ ...linked, startAt: startAt - 86400e3 }]
  assert.equal(c.assignmentLabel(loaded), expected)
  const unavailable = { assignmentId: 'missing' }
  assert.equal(c.swapContext(null), undefined)
  assert.equal(c.swapContext(unavailable), undefined)
  assert.notEqual(c.assignmentLabel(unavailable), unavailable.assignmentId)
  assert.equal(c.swapContext({ assignmentId: linked.id }), c.calendarRows.value[0])
})

test('创建换班只提交申请字段，服务端关联与审批字段不会回传', async () => {
  const mutations = []
  const c = component(async (path, request) => {
    if (request?.method) mutations.push({ path, body: JSON.parse(request.body) })
    return { items: [], total: 0 }
  }, 'openSwap,swap,saveSwap')
  c.openSwap({ id: 'assignment', personnelIds: ['from'], assignment: { id: 'unrelated' }, requestedBy: 'server', version: 8 })
  Object.assign(c.swap, {
    toPersonnelId: 'to',
    reason: ' 调整人员 ',
    assignment: { id: 'extra' },
    status: 'approved',
    reviewedBy: 'server'
  })
  await c.saveSwap()
  assert.deepEqual(mutations, [
    { path: '/api/v1/duty/swaps', body: { assignmentId: 'assignment', fromPersonnelId: 'from', toPersonnelId: 'to', reason: '调整人员' } }
  ])
})

test('批量排班按日期逐日生成并按组轮换，超出范围或缺少人员时拒绝', () => {
  const shift = { id: 'night', startTime: '22:00', endTime: '06:00' }
  const rows = fireSafety.batchAssignments({
    stationId: 's1',
    shift,
    from: '2026-10-30',
    to: '2026-11-02',
    groups: [['a', 'a'], ['b']],
    notes: ' 轮值 '
  })
  assert.equal(rows.length, 4)
  assert.deepEqual(
    rows.map(row => row.personnelIds.join()),
    ['a', 'b', 'a', 'b']
  )
  assert.deepEqual(rows[0], {
    stationId: 's1',
    shiftId: 'night',
    personnelIds: ['a'],
    startAt: fireSafety.shiftRange('2026-10-30', '22:00', '06:00')[0],
    endAt: fireSafety.shiftRange('2026-10-30', '22:00', '06:00')[1],
    notes: '轮值'
  })
  assert.equal(fireSafety.toDateInput(rows[2].startAt), '2026-11-01')
  assert.throws(
    () => fireSafety.batchAssignments({ stationId: 's1', shift, from: '2026-10-02', to: '2026-10-01', groups: [['a']] }),
    /结束日期/
  )
  assert.throws(() => fireSafety.batchAssignments({ stationId: 's1', shift, from: '2026-10-01', to: '2026-10-02', groups: [[]] }), /一组/)
  assert.throws(() => fireSafety.batchAssignments({ stationId: 's1', shift, from: '2026-01-01', to: '2026-03-31', groups: [['a']] }), /62/)
})
