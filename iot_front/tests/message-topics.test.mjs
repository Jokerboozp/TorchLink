import assert from 'node:assert/strict'
import test from 'node:test'
import vm from 'node:vm'
import { computed, reactive, ref, watch } from 'vue'
import { setupScript } from './helpers/vue.mjs'
import { filterMessageTopics, topicDirectionLabel, topicStatus, validateSharedTopic } from '../src/messageTopics.js'
import * as topicHelpers from '../src/messageTopics.js'

const prefixes = { mqtt: '/iot/external/74656e616e74/', kafka: 'iot.external.74656e616e74.' }
const datasets = [
  { id: 'device_reports', name: '设备上报', mode: 'realtime', fields: [{ path: 'deviceId', name: '设备编号', type: 'string' }] },
  { id: 'devices', name: '当前设备信息', mode: 'interval', fields: [] }
]
const query = { dataset: 'device_reports', mode: 'realtime', intervalSeconds: 0, deviceScope: 'all', deviceIds: [], fields: {} }
const topic = {
  id: 'topic_1',
  name: '设备上报',
  protocol: 'mqtt',
  topic: `${prefixes.mqtt}device`,
  enabled: true,
  effectiveEnabled: true,
  description: '园区订阅',
  query,
  querySql: 'SELECT * FROM device_reports',
  keyIds: ['ak_1']
}
const builtin = {
  id: 'mqtt.parsed',
  name: '成功解析的设备数据',
  protocol: 'mqtt',
  direction: 'outbound',
  topic: '/iot/parsed/{tenantId}/{productId}/{deviceId}/{messageType}',
  reason: '仅成功解析的数据会发布。'
}
const keys = [{ id: 'ak_1', name: '园区平台', username: 'consumer', enabled: true }]
const catalog = (revision = 1, items = [topic]) => ({
  revision,
  items,
  builtin: [builtin],
  keys,
  prefixes,
  datasets,
  authorization: { mqtt: { ready: false, reason: 'MQTT 尚未配置授权' }, kafka: { ready: false, reason: 'Kafka 尚未配置授权' } },
  runtime: { mqttEnabled: true, kafkaEnabled: false, kafkaParsedEnabled: false }
})

function page(api, permissions = ['*']) {
  const hooks = {},
    alerts = [],
    requests = []
  const permissionState = reactive({ items: permissions, accessVersion: 'version-1' })
  const session = { token: 'token-a', tenant: 'tenant-a', user: 'operator-a' }
  const can = permission => permissionState.items.includes('*') || permissionState.items.includes(permission)
  const context = vm.createContext({
    computed,
    reactive,
    ref,
    watch,
    permissionState,
    session,
    can,
    AbortController,
    ...topicHelpers,
    api: (path, options) => {
      requests.push({ path, options })
      return api(path, options)
    },
    UiMessage: { success: message => alerts.push(message), warning: message => alerts.push(message) },
    UiMessageBox: { confirm: async () => {} },
    defineEmits: () => () => {},
    onMounted: fn => {
      hooks.mount = fn
    },
    onBeforeUnmount: fn => {
      hooks.unmount = fn
    },
    window: {
      location: { origin: 'https://test.invalid' },
      addEventListener: (key, fn) => {
        hooks[key] = fn
      },
      removeEventListener: key => {
        delete hooks[key]
      }
    },
    navigator: { clipboard: { writeText: async () => {} } }
  })
  const source = setupScript(new URL('../src/views/MessageTopicsView.vue', import.meta.url))
  const state = vm.runInContext(
    source +
      '\n;({ snapshot, loading, saving, error, notice, formError, conflicted, editing, creating, form, keyIds, filteredItems, builtinItems, filters, emptyText, load, openEditor, save, removeTopic, rowActions, keyName, identityChanged, queryForm, queryPreview, queryPreviewError, queryPreviewing, previewQuery, useQuerySql })',
    context
  )
  return { ...state, hooks, session, permissionState, requests, alerts, context }
}

test('消息主题按协议与内容筛选，区分停用和未部署通道', () => {
  const kafka = { ...topic, id: 'topic_2', protocol: 'kafka', topic: 'iot.event', effectiveEnabled: false }
  const disabled = { ...topic, id: 'topic_3', name: '停用主题', enabled: false }
  assert.deepEqual(filterMessageTopics([topic, kafka, disabled], { protocol: 'kafka' }), [kafka])
  assert.deepEqual(filterMessageTopics([topic, kafka], { keyword: 'IOT.EVENT' }), [kafka])
  assert.equal(topicStatus(kafka).label, '通道未启用')
  assert.equal(topicStatus(disabled).label, '已停用')
  assert.equal(topicDirectionLabel('inbound'), '设备接入')
})

test('主题接受协议后缀与本租户地址，拒绝通配符和其他租户地址', () => {
  assert.equal(validateSharedTopic({ name: 'a', protocol: 'mqtt', topic: '/device' }, prefixes), '')
  assert.equal(validateSharedTopic({ name: 'a', protocol: 'kafka', topic: `${prefixes.kafka}device` }, prefixes), '')
  assert.match(validateSharedTopic({ name: 'a', protocol: 'mqtt', topic: '/device/#' }, prefixes), /通配符/)
  assert.match(validateSharedTopic({ name: 'a', protocol: 'mqtt', topic: '/iot/external/other/device' }, prefixes), /必须位于/)
  assert.match(validateSharedTopic({ name: 'a', protocol: 'kafka', topic: 'bad name' }, prefixes), /Kafka/)
  assert.match(validateSharedTopic({ name: '', protocol: 'mqtt', topic: '/device' }, prefixes), /名称/)
})

test('消息主题刷新忽略旧响应，错误状态不显示成真实空列表', async () => {
  const pending = []
  const p = page((path, options) => new Promise((resolve, reject) => pending.push({ resolve, reject, signal: options.signal })))
  const first = p.load(),
    second = p.load()
  assert.equal(pending[0].signal.aborted, true)
  pending[1].resolve(catalog(2, [{ ...topic, name: '最新配置' }]))
  await second
  pending[0].resolve(catalog(1))
  await first
  assert.equal(p.snapshot.value.revision, 2)
  assert.equal(p.filteredItems.value[0].name, '最新配置')
  assert.equal(p.builtinItems.value.length, 1)
  assert.equal(p.keyName('ak_1'), '园区平台')
  const failed = p.load()
  pending[2].reject(new Error('读取失败'))
  await failed
  assert.equal(p.error.value, '读取失败')
  assert.equal(p.snapshot.value, null)
  const empty = p.load()
  pending[3].resolve(catalog(3, []))
  await empty
  assert.match(p.emptyText.value, /暂无消息主题/)
})

test('新建主题同时提交查询与订阅密钥，编辑保持协议地址，阻止重复提交', async () => {
  let revision = 5,
    finish
  const p = page((path, options) => {
    if (!options.method) return Promise.resolve(catalog(revision))
    if (options.method === 'PUT')
      return new Promise(resolve => {
        finish = resolve
      })
    return Promise.resolve(catalog(++revision))
  })
  await p.load()
  p.openEditor()
  Object.assign(p.form, { name: '外部业务', protocol: 'mqtt', topic: 'smoke/reading' })
  p.keyIds.value = ['ak_1']
  await p.save()
  assert.equal(p.requests[1].options.method, 'POST')
  assert.deepEqual(JSON.parse(p.requests[1].options.body), {
    revision: 5,
    name: '外部业务',
    enabled: true,
    description: '',
    protocol: 'mqtt',
    topic: 'smoke/reading',
    query,
    keyIds: ['ak_1']
  })
  p.openEditor(p.snapshot.value.items[0])
  Object.assign(p.form, { name: '新名称', protocol: 'kafka', topic: 'must-not-change' })
  const saving = p.save()
  await p.save()
  assert.equal(p.requests.length, 3)
  const body = JSON.parse(p.requests[2].options.body)
  assert.equal(body.revision, 6)
  assert.equal(body.protocol, 'mqtt')
  assert.equal(body.topic, topic.topic)
  assert.deepEqual(body.keyIds, ['ak_1'])
  finish(catalog(7, [{ ...topic, name: '新名称' }]))
  await saving
  assert.equal(p.snapshot.value.revision, 7)
  assert.equal(p.editing.value, null)
})

test('没有密钥编辑权限时不提交订阅授权字段', async () => {
  const p = page(() => Promise.resolve(catalog()), ['menu:messageTopics', 'PUT /api/v1/message-topics/:id'])
  await p.load()
  p.openEditor(topic)
  await p.save()
  assert.equal(Object.hasOwn(JSON.parse(p.requests[1].options.body), 'keyIds'), false)
})

test('主题并发冲突保留编辑内容，必须刷新后重新编辑才能继续写入', async () => {
  let revision = 2
  const p = page((path, options) =>
    options.method ? Promise.reject(Object.assign(new Error('conflict'), { status: 409 })) : Promise.resolve(catalog(revision))
  )
  await p.load()
  p.openEditor(p.snapshot.value.items[0])
  p.form.description = '未覆盖其他人的修改'
  await p.save()
  assert.equal(p.conflicted.value, true)
  assert.match(p.formError.value, /刷新/)
  assert.equal(p.form.description, '未覆盖其他人的修改')
  await p.save()
  assert.equal(p.requests.length, 2)
  revision = 3
  await p.load()
  assert.equal(p.conflicted.value, false)
  assert.equal(p.editing.value, null)
})

test('删除主题使用当前修订，确认期间身份变化不能误删', async () => {
  const p = page((path, options) => Promise.resolve(catalog(options.method ? 2 : 1, options.method ? [] : [topic])))
  await p.load()
  await p.removeTopic(topic)
  assert.equal(p.requests[1].options.method, 'DELETE')
  assert.equal(p.requests[1].path, '/api/v1/message-topics/topic_1?revision=1')
  assert.equal(p.snapshot.value.items.length, 0)
  let approve
  const q = page(() => Promise.resolve(catalog()))
  q.context.UiMessageBox.confirm = () =>
    new Promise(resolve => {
      approve = resolve
    })
  await q.load()
  const deleting = q.removeTopic(topic)
  q.session.user = 'different-user'
  approve()
  await deleting
  assert.equal(q.requests.length, 1)
})

test('卸载、身份变更和权限撤销后迟到响应不会污染页面', async () => {
  let resolveLoad
  const p = page(
    () =>
      new Promise(resolve => {
        resolveLoad = resolve
      })
  )
  const loading = p.load()
  p.session.tenant = 'other-tenant'
  resolveLoad(catalog())
  await loading
  assert.equal(p.snapshot.value, null)
  let resolveSave
  const q = page((path, options) =>
    options.method
      ? new Promise(resolve => {
          resolveSave = resolve
        })
      : Promise.resolve(catalog())
  )
  await q.load()
  q.openEditor(topic)
  const saving = q.save()
  q.hooks.unmount()
  assert.equal(q.requests[1].options.signal.aborted, true)
  resolveSave(catalog(5))
  await saving
  assert.equal(q.snapshot.value.revision, 1)
  assert.equal(q.alerts.length, 0)
  const r = page(() => Promise.resolve(catalog()))
  await r.load()
  r.openEditor(topic)
  r.permissionState.items = []
  assert.equal(r.snapshot.value, null)
  assert.equal(r.editing.value, null)
})

test('设备选择使用有界服务端搜索，旧查询与卸载后的结果不会覆盖当前设备', async () => {
  const hooks = {},
    pending = [],
    emitted = []
  const props = reactive({ modelValue: ['selected-device'], disabled: false })
  const context = vm.createContext({
    computed,
    ref,
    watch,
    AbortController,
    encodeURIComponent,
    setTimeout,
    clearTimeout,
    session: { token: 'session', tenant: 'tenant', user: 'admin' },
    permissionState: reactive({ accessVersion: 'v1' }),
    defineProps: () => props,
    defineEmits: () => (event, value) => emitted.push({ event, value }),
    onMounted: fn => {
      hooks.mount = fn
    },
    onBeforeUnmount: fn => {
      hooks.unmount = fn
    },
    api: (path, options) => new Promise(resolve => pending.push({ path, options, resolve }))
  })
  const p = vm.runInContext(
    setupScript(new URL('../src/components/MessageTopicDevicePicker.vue', import.meta.url)) + '\n;({load,query,rows,toggle})',
    context
  )
  const first = p.load()
  p.query.value = 'device-101'
  const second = p.load()
  assert.equal(pending[0].options.signal.aborted, true)
  assert.match(pending[1].path, /pageSize=50&q=device-101/)
  pending[1].resolve({ items: [{ device: { id: 'device-101', name: '最新搜索' } }], total: 1 })
  await second
  pending[0].resolve({ items: [{ device: { id: 'old' } }], total: 10000 })
  await first
  assert.equal(p.rows.value[0].id, 'device-101')
  p.toggle('device-101', true)
  assert.deepEqual(Array.from(emitted[0].value), ['selected-device', 'device-101'])
  const late = p.load()
  hooks.unmount()
  pending[2].resolve({ items: [{ device: { id: 'after-unmount' } }], total: 1 })
  await late
  assert.equal(p.rows.value.length, 0)
})

test('查询表单保留值类型、IN列表、字段别名与设备范围，SQL切换保持等价条件', () => {
  const original = {
    dataset: 'device_reports',
    mode: 'realtime',
    fields: { temperature: 'properties.temperature', device: 'deviceId' },
    deviceScope: 'selected',
    deviceIds: ['a'],
    filter: {
      logic: 'or',
      children: [
        { field: 'properties.temperature', operator: 'gte', value: 26 },
        { field: 'deviceId', operator: 'in', value: ['001', 'a'] },
        { field: 'tags.enabled', operator: 'eq', value: false }
      ]
    }
  }
  const form = topicHelpers.queryFormFrom(original)
  assert.equal(topicHelpers.validateTopicQuery(form, datasets), '')
  const query = topicHelpers.topicQueryRequest(form).query
  assert.deepEqual(query, { ...original, intervalSeconds: 0 })
  const sql = topicHelpers.queryToSql(query)
  assert.match(sql, /properties.temperature AS temperature/)
  assert.match(sql, /deviceId IN \('001', 'a'\)/)
  assert.match(sql, /tags.enabled = false/)
  form.fields[1].output = 'temperature'
  assert.match(topicHelpers.validateTopicQuery(form, datasets), /不能重复/)
  form.allFields = true
  form.conditions[0].value = '9007199254740993'
  assert.match(topicHelpers.validateTopicQuery(form, datasets), /精确/)
})

test('查询预览使用真实样本接口，修改查询或关闭后迟到响应不显示，空样本保持空态', async () => {
  let resolvePreview
  const p = page((path, options) =>
    path.endsWith('/query/preview')
      ? new Promise(resolve => {
          resolvePreview = resolve
        })
      : Promise.resolve(catalog())
  )
  await p.load()
  p.openEditor()
  Object.assign(p.form, { name: '设备数据', topic: '/device' })
  const pending = p.previewQuery()
  assert.equal(p.requests[1].path, '/api/v1/message-topics/query/preview')
  assert.equal(Object.hasOwn(JSON.parse(p.requests[1].options.body), 'payload'), false)
  p.queryForm.value.conditions.push({ field: 'deviceId', operator: 'eq', value: 'a' })
  assert.equal(p.requests[1].options.signal.aborted, true)
  resolvePreview({ sampled: true, matched: true, payload: 'old' })
  await pending
  assert.equal(p.queryPreview.value, null)
  const empty = p.previewQuery()
  resolvePreview({ sampled: false, matched: false, payload: '' })
  await empty
  assert.equal(p.queryPreview.value.sampled, false)
  const late = p.previewQuery()
  p.hooks.unmount()
  resolvePreview({ sampled: true, matched: true, payload: 'late' })
  await late
  assert.equal(p.queryPreview.value, null)
})

test('图形转换SQL遇到嵌套条件保持原文，不隐式丢弃查询条件', async () => {
  const nested = {
    dataset: 'device_reports',
    mode: 'realtime',
    deviceScope: 'all',
    fields: {},
    filter: { logic: 'and', children: [{ logic: 'or', children: [{ field: 'deviceId', operator: 'eq', value: 'a' }] }] }
  }
  const sql = "SELECT * FROM device_reports WHERE (deviceId = 'a')"
  const p = page(path =>
    Promise.resolve(
      path.endsWith('/query/preview') ? { query: nested, querySql: sql, matched: false, sampled: false, payload: '' } : catalog()
    )
  )
  await p.load()
  p.openEditor()
  p.queryForm.value.editor = 'sql'
  p.queryForm.value.sql = sql
  await p.previewQuery(true)
  assert.equal(p.queryForm.value.editor, 'sql')
  assert.equal(p.queryForm.value.sql, sql)
  assert.match(p.queryPreviewError.value, /嵌套条件/)
})

test('SQL转换引用特殊字段路径并精确保留表单输入的大整数和IN字符串', () => {
  const form = topicHelpers.queryFormFrom({
    dataset: 'device_reports',
    fields: { temperature: 'properties.temp-c' },
    filter: { field: 'tags.site:code', operator: 'eq', value: 'A' }
  })
  assert.match(topicHelpers.queryFormToSql(form), /"properties.temp-c" AS temperature/)
  assert.match(topicHelpers.queryFormToSql(form), /"tags.site:code" = 'A'/)
  form.conditions = [{ field: 'properties.counter', operator: 'in', value: '[ "a,b", 9007199254740993, "001" ]' }]
  assert.equal(
    topicHelpers.queryFormToSql(form),
    "SELECT \"properties.temp-c\" AS temperature\nFROM device_reports\nWHERE (properties.counter IN ('a,b', 9007199254740993, '001'))"
  )
})
