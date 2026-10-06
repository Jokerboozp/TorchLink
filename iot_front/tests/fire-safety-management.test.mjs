import test from 'node:test'
import assert from 'node:assert/strict'
import vm from 'node:vm'
import { computed, reactive, ref, toRef } from 'vue'
import { setupScript } from './helpers/vue.mjs'
import {
  managementPayload,
  inputTimestamp,
  localDateTimeInput,
  inspectionPayload,
  canReviewInspection,
  dispatchPayload,
  extinguisherLabels,
  fireQuery,
  requiredFieldErrors
} from '../src/fireSafetyManagement.js'

// 页面状态恢复与未保存检查由各自测试覆盖，此处替换为无副作用实现。
const pageStubs = {
  usePageState: () => ({ restored: false }),
  trackDialogForm: () => ({ dirty: () => false, reset() {}, clear() {} }),
  confirmClose: async () => true,
  toRef
}

test('编辑请求保留版本，排除服务端提醒与审计字段', () => {
  const stored = {
    id: 'asset',
    version: 7,
    code: 'EXT-1',
    stationId: 's',
    location: '门口',
    type: 'dry_powder',
    specification: '4kg',
    manufacturer: '厂商',
    serialNumber: 'n',
    manufacturedOn: '2026-01-01',
    serviceDueOn: '2027-01-01',
    retireOn: '2030-01-01',
    inspectionCycleDays: 30,
    status: 'active',
    notes: '',
    createdAt: 123,
    reminders: [{ kind: 'inspection' }],
    lastInspectedAt: 456
  }
  const payload = managementPayload('extinguishers', reactive(stored))
  assert.equal(payload.version, 7)
  assert.equal(payload.serviceDueOn, '2027-01-01')
  for (const field of ['id', 'createdAt', 'reminders', 'lastInspectedAt']) assert.equal(Object.hasOwn(payload, field), false)
  assert.equal(Object.hasOwn(managementPayload('stations', { code: 'new' }), 'version'), false)
})

test('巡检每项必须明确选择结果，失败必须说明问题', () => {
  assert.throws(() => inspectionPayload({ checks: [{ name: '外观', passed: null }], findings: '' }, 3), /明确选择/)
  assert.throws(() => inspectionPayload({ checks: [{ name: '外观', passed: false }], findings: ' ' }, 3), /发现的问题/)
  assert.throws(() => inspectionPayload({ checks: [], findings: '' }, 3), /至少填写/)
  assert.throws(
    () =>
      inspectionPayload(
        {
          checks: [
            { name: '外观', passed: true },
            { name: ' 外观 ', passed: true }
          ],
          findings: ''
        },
        3
      ),
    /重复/
  )
  assert.deepEqual(inspectionPayload({ checks: [{ name: ' 外观 ', passed: false }], findings: ' 筒体损伤 ' }, 3), {
    version: 3,
    checks: [{ name: '外观', passed: false }],
    findings: '筒体损伤'
  })
})

test('整改复核取最新轮次并禁止提交人自复核', () => {
  const task = {
    status: 'reviewing',
    rectifications: [
      { submittedBy: 'reviewer', status: 'rejected' },
      { submittedBy: 'worker', status: 'pending' }
    ]
  }
  assert.equal(canReviewInspection(task, 'worker'), false)
  assert.equal(canReviewInspection(task, 'reviewer'), true)
  assert.equal(canReviewInspection(task, ''), false)
  assert.equal(canReviewInspection({ ...task, status: 'completed' }, 'reviewer'), false)
  assert.equal(canReviewInspection({ ...task, rectifications: [] }, 'reviewer'), false)
})

test('多人出勤携带器材数量必须为正整数且不能重复', () => {
  const form = {
    stationId: 'station',
    title: '演练',
    type: 'drill',
    location: '操场',
    personnelIds: ['p1', 'p2'],
    equipment: [
      { equipmentId: 'e1', quantity: 2 },
      { equipmentId: 'e2', quantity: 1 }
    ],
    startedAtInput: '2026-10-01T09:00'
  }
  const value = dispatchPayload(form)
  assert.deepEqual(value.personnelIds, ['p1', 'p2'])
  assert.equal(value.startedAt, new Date(2026, 9, 1, 9).getTime())
  value.personnelIds.push('p3')
  assert.equal(form.personnelIds.length, 2)
  assert.throws(() => dispatchPayload({ ...form, equipment: [{ equipmentId: 'e1', quantity: 1.5 }] }), /正整数/)
  assert.throws(
    () =>
      dispatchPayload({
        ...form,
        equipment: [
          { equipmentId: 'e1', quantity: 1 },
          { equipmentId: 'e1', quantity: 1 }
        ]
      }),
    /重复/
  )
})

test('日期时间保持本地时区，分页筛选保留0且排除空值', () => {
  const date = new Date(2026, 9, 1, 9, 30)
  assert.equal(localDateTimeInput(date.getTime()), '2026-10-01T09:30')
  assert.equal(inputTimestamp(localDateTimeInput(date.getTime())), date.getTime())
  assert.throws(() => inputTimestamp(''), /有效/)
  const query = new URLSearchParams(fireQuery({ q: '名称 / A', status: '', remindDays: 0 }, { page: 2, pageSize: 20 }))
  assert.equal(query.get('q'), '名称 / A')
  assert.equal(query.has('status'), false)
  assert.equal(query.get('remindDays'), '0')
  assert.equal(query.get('page'), '2')
})

function stationPage(api = async () => ({ items: [], total: 0 }), allowed = true) {
  const source = setupScript(new URL('../src/views/FireStationsView.vue', import.meta.url))
  const context = vm.createContext({
    ...pageStubs,
    computed,
    reactive,
    ref,
    watch() {},
    onMounted() {},
    defineEmits() {},
    can: () => allowed,
    api,
    notifyError() {},
    UiMessage: { success() {} },
    UiMessageBox: {},
    errorMessage: error => error.message,
    managementPayload,
    localDateTimeInput,
    inputTimestamp,
    dispatchPayload,
    fireQuery,
    requiredFieldErrors
  })
  vm.runInContext(source + '\nglobalThis.subject={open,form,dialog,tab,load,loadError,loading,save,fieldErrors}', context)
  return context.subject
}

test('真实Vue响应式行可打开编辑，未提交的修改不污染表格', () => {
  const row = reactive({ id: 'station', version: 4, code: 'S1', name: '原名称', type: 'micro', notes: '原备注', enabled: true })
  const page = stationPage()
  assert.doesNotThrow(() => page.open('stations', row))
  assert.equal(page.dialog.value, true)
  assert.equal(page.form.version, 4)
  page.form.name = '未提交名称'
  assert.equal(row.name, '原名称')
})

test('统计标签不发出不存在的列表请求', async () => {
  const requests = [],
    page = stationPage(async path => {
      requests.push(path)
      return {}
    })
  page.tab.value = 'statistics'
  await page.load()
  assert.deepEqual(requests, [])
  assert.equal(page.loadError.value, '')
  assert.equal(page.loading.value, false)
})

test('保存失败保留编辑内容和版本供用户核对', async () => {
  const row = reactive({ id: 'station', version: 4, code: 'S1', name: '原名称', type: 'micro', notes: '原备注', enabled: true })
  const requests = [],
    page = stationPage(async (path, options) => {
      if (options?.method === 'PUT') {
        requests.push(JSON.parse(options.body))
        throw Error('记录已更新，请刷新后重试')
      }
      return {}
    })
  page.open('stations', row)
  page.form.name = '新名称'
  await page.save()
  assert.equal(requests[0].version, 4)
  assert.equal(requests[0].name, '新名称')
  assert.equal(page.form.name, '新名称')
  assert.equal(page.dialog.value, true)
})

test('缺少必填项时逐项提示且不提交', async () => {
  const writes = [],
    page = stationPage(async (path, options) => {
      if (options?.method) writes.push(path)
      return {}
    })
  page.open('equipment')
  page.form.name = '空气呼吸器'
  await page.save()
  assert.deepEqual(writes, [])
  assert.deepEqual(Object.keys(page.fieldErrors.value).sort(), ['category', 'stationId'])
  page.open('equipment')
  assert.equal(Object.keys(page.fieldErrors.value).length, 0)
})

test('没有编辑权限时不发起写入请求', async () => {
  const writes = [],
    page = stationPage(async (path, options) => {
      if (options?.method) writes.push(path)
      return {}
    }, false)
  page.open('stations', { id: 's', version: 1, name: '消防站', code: 's' })
  await page.save()
  assert.deepEqual(writes, [])
})

test('巡检状态筛选不被错误用于灭火器资产统计', async () => {
  const requests = []
  const source = setupScript(new URL('../src/views/ExtinguishersView.vue', import.meta.url))
  const context = vm.createContext({
    ...pageStubs,
    computed,
    reactive,
    ref,
    watch() {},
    onMounted() {},
    defineEmits() {},
    api: async path => {
      requests.push(path)
      return {}
    },
    fireQuery,
    requiredFieldErrors,
    extinguisherLabels,
    extinguisherTypes: [],
    inspectionStates: []
  })
  vm.runInContext(source + '\nglobalThis.subject={tab,filters,loadStatistics}', context)
  const page = context.subject
  page.tab.value = 'inspections'
  Object.assign(page.filters, { stationId: 'station', status: 'rectifying', q: '问题' })
  await page.loadStatistics()
  const query = new URL(requests[0], 'http://localhost').searchParams
  assert.equal(query.get('stationId'), 'station')
  assert.equal(query.has('status'), false)
  assert.equal(query.has('q'), false)
  page.tab.value = 'assets'
  page.filters.status = 'active'
  await page.loadStatistics()
  assert.equal(new URL(requests[1], 'http://localhost').searchParams.get('status'), 'active')
})

test('巡检任务处理弹窗：提交人不能复核自己的整改，取消须填写原因，成功后返回最新任务', async () => {
  const writes = [],
    emitted = []
  const source = setupScript(new URL('../src/components/extinguishers/ExtinguisherActionDialog.vue', import.meta.url))
  const props = reactive({
    kind: 'review',
    task: { id: 'task/1', version: 3, status: 'reviewing', rectifications: [{ submittedBy: 'worker', status: 'pending' }] },
    options: { stations: [], personnel: [], extinguishers: [], inspectionChecks: ['外观'] }
  })
  const context = vm.createContext({
    ...pageStubs,
    computed,
    reactive,
    ref,
    watch() {},
    defineModel: () => ref(true),
    defineProps: () => props,
    defineEmits:
      () =>
      (...args) =>
        emitted.push(args),
    session: { user: 'worker' },
    can: () => true,
    api: async (path, request) => {
      writes.push({ path, body: JSON.parse(request.body) })
      return { id: 'task/1', version: 4 }
    },
    UiMessage: { success() {} },
    errorMessage: error => error.message,
    dateTimeLabel: String,
    canReviewInspection,
    extinguisherLabels,
    inspectionPayload
  })
  vm.runInContext(source + '\nglobalThis.subject={reset,actionForm,actionError,saveAction,visible}', context)
  const dialog = context.subject
  dialog.reset()
  assert.deepEqual(
    dialog.actionForm.checks.map(item => item.name),
    ['外观']
  )
  dialog.actionForm.note = '已核实'
  await dialog.saveAction()
  assert.match(dialog.actionError.value, /不能复核/)
  props.kind = 'cancel'
  await dialog.saveAction()
  assert.match(dialog.actionError.value, /取消原因/)
  dialog.actionForm.reason = ' 设备已报废 '
  await dialog.saveAction()
  assert.deepEqual(writes, [{ path: '/api/v1/extinguisher-inspections/task%2F1/cancel', body: { version: 3, reason: '设备已报废' } }])
  assert.equal(dialog.visible.value, false)
  assert.deepEqual(emitted, [['saved', { id: 'task/1', version: 4 }]])
})

test('灭火器名称查找在资料被删除时给出说明', () => {
  const labels = extinguisherLabels({ stations: [{ id: 's', name: '一站' }], personnel: [], extinguishers: [] })
  assert.equal(labels.stationName('s'), '一站')
  assert.equal(labels.stationName('missing'), '已移除消防站')
  assert.equal(labels.personName('p'), '已移除人员')
  assert.equal(labels.assetName('e'), '已移除灭火器')
  assert.equal(labels.typeName('dry_powder'), '干粉')
})
