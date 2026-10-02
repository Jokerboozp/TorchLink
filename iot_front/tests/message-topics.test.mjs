import assert from 'node:assert/strict'
import test from 'node:test'
import vm from 'node:vm'
import { computed, reactive, ref, watch } from 'vue'
import { setupScript } from './helpers/vue.mjs'
import { filterMessageTopics, topicDirectionLabel, topicDirections, topicStatus, topicVariableLabel, validateTopicTarget } from '../src/messageTopics.js'

const prefixes = { mqtt:'/iot/external/74656e616e74/', kafka:'iot.external.74656e616e74.' }
const topic = { id:'mqtt.parsed', name:'解析数据', protocol:'mqtt', direction:'outbound', topic:'/iot/parsed/{tenantId}/{deviceId}', defaultTopic:'/iot/parsed/{tenantId}/{deviceId}', enabled:true, effectiveEnabled:true, editable:true, variables:['tenantId', 'deviceId'], description:'解析成功后发布', overridden:false }
const catalog = (revision = 1, items = [topic]) => ({ revision, items, prefixes, runtime:{ mqttEnabled:true, kafkaEnabled:false, kafkaParsedEnabled:false } })

function page(api, permissions = ['*']) {
  const hooks = {}, alerts = [], requests = []
  const permissionState = reactive({ items:permissions, accessVersion:'version-1' })
  const session = { token:'token-a', tenant:'tenant-a', user:'operator-a' }
  const can = permission => permissionState.items.includes('*') || permissionState.items.includes(permission)
  const context = vm.createContext({
    computed, reactive, ref, watch, permissionState, session, can, AbortController,
    filterMessageTopics, topicDirectionLabel, topicDirections, topicStatus, topicVariableLabel, validateTopicTarget,
    api:(path, options) => { requests.push({ path, options }); return api(path, options) },
    UiMessage:{ success:message => alerts.push(message), warning:message => alerts.push(message) },
    defineEmits:() => () => {}, onMounted:fn => { hooks.mount = fn }, onBeforeUnmount:fn => { hooks.unmount = fn },
    window:{ addEventListener:(key, fn) => { hooks[key] = fn }, removeEventListener:key => { delete hooks[key] } },
    navigator:{ clipboard:{ writeText:async () => {} } }
  })
  const source = setupScript(new URL('../src/views/MessageTopicsView.vue', import.meta.url))
  const state = vm.runInContext(source + '\n;({ snapshot, loading, saving, error, formError, conflicted, editing, form, filteredItems, filters, emptyText, load, openEditor, save, reset, rowActions, identityChanged })', context)
  return { ...state, hooks, session, permissionState, requests, alerts }
}

test('消息主题按协议、用途与主题内容组合筛选，并区分停用和未部署通道', () => {
  const kafka = { ...topic, id:'kafka.event', protocol:'kafka', topic:'iot.event', effectiveEnabled:false }
  const inbound = { ...topic, id:'mqtt.ingress', name:'设备上报', direction:'inbound', enabled:false }
  assert.deepEqual(filterMessageTopics([topic, kafka, inbound], { protocol:'mqtt', direction:'outbound', keyword:'解析' }), [topic])
  assert.deepEqual(filterMessageTopics([topic, kafka], { keyword:'IOT.EVENT' }), [kafka])
  assert.equal(topicStatus(kafka).label, '通道未启用')
  assert.equal(topicStatus(inbound).label, '已停用')
})

test('主题目标允许默认值，只允许租户前缀内的自定义发布主题和已声明变量', () => {
  assert.equal(validateTopicTarget(topic, topic.defaultTopic, prefixes), '')
  assert.equal(validateTopicTarget(topic, `${prefixes.mqtt}parsed/{deviceId}`, prefixes), '')
  assert.match(validateTopicTarget(topic, '/iot/external/other/parsed', prefixes), /必须以/)
  assert.match(validateTopicTarget(topic, prefixes.mqtt.slice(0, -1) + '-other/parsed', prefixes), /必须以/)
  assert.match(validateTopicTarget(topic, `${prefixes.mqtt}parsed/#`, prefixes), /通配符/)
  assert.match(validateTopicTarget(topic, `${prefixes.mqtt}parsed/{unknown}`, prefixes), /不支持的变量/)
  assert.match(validateTopicTarget(topic, `${prefixes.mqtt}parsed/{deviceId`, prefixes), /不支持的变量/)
  assert.match(validateTopicTarget(topic, prefixes.mqtt, prefixes), /后续名称/)
  assert.match(validateTopicTarget(topic, `${prefixes.mqtt}parsed`, {}), /刷新/)
  const kafka = { ...topic, protocol:'kafka', variables:[], defaultTopic:'iot.parsed' }
  assert.equal(validateTopicTarget(kafka, `${prefixes.kafka}parsed-events`, prefixes), '')
  assert.match(validateTopicTarget(kafka, `${prefixes.kafka}bad name`, prefixes), /Kafka/)
})

test('消息主题刷新忽略旧响应，错误状态不显示成真实空列表', async () => {
  const pending = []
  const p = page((path, options) => new Promise((resolve, reject) => pending.push({ resolve, reject, signal:options.signal })))
  const first = p.load(), second = p.load()
  assert.equal(pending[0].signal.aborted, true)
  pending[1].resolve(catalog(2, [{ ...topic, name:'最新配置' }]))
  await second
  pending[0].resolve(catalog(1))
  await first
  assert.equal(p.snapshot.value.revision, 2)
  assert.equal(p.filteredItems.value[0].name, '最新配置')
  const failed = p.load()
  pending[2].reject(new Error('读取失败'))
  await failed
  assert.equal(p.error.value, '读取失败')
  assert.equal(p.snapshot.value, null)
  const empty = p.load()
  pending[3].resolve(catalog(3, []))
  await empty
  assert.equal(p.error.value, '')
  assert.match(p.emptyText.value, /当前环境没有/)
})

test('主题保存携带打开编辑时的修订，阻止重复提交，使用服务端返回刷新状态', async () => {
  let finish
  const p = page((path, options) => options.method ? new Promise(resolve => { finish = resolve }) : Promise.resolve(catalog(7)))
  await p.load()
  p.openEditor(p.snapshot.value.items[0])
  Object.assign(p.form, { topic:`${prefixes.mqtt}parsed/{deviceId}`, enabled:false, description:'业务订阅' })
  const saving = p.save()
  await p.save()
  assert.equal(p.requests.length, 2)
  assert.equal(p.requests[1].options.method, 'PUT')
  assert.deepEqual(JSON.parse(p.requests[1].options.body), { revision:7, topic:`${prefixes.mqtt}parsed/{deviceId}`, enabled:false, description:'业务订阅' })
  finish(catalog(8, [{ ...topic, ...p.form, effectiveEnabled:false, overridden:true }]))
  await saving
  assert.equal(p.snapshot.value.revision, 8)
  assert.equal(p.snapshot.value.items[0].enabled, false)
  assert.equal(p.editing.value, null)
  assert.equal(p.saving.value, false)
})

test('主题并发冲突保留编辑内容，必须刷新后重新编辑才能继续写入', async () => {
  let revision = 2
  const p = page((path, options) => options.method ? Promise.reject(Object.assign(new Error('conflict'), { status:409 })) : Promise.resolve(catalog(revision)))
  await p.load()
  p.openEditor(p.snapshot.value.items[0])
  p.form.description = '未覆盖其他人的修改'
  await p.save()
  assert.equal(p.conflicted.value, true)
  assert.match(p.formError.value, /刷新/)
  assert.equal(p.form.description, '未覆盖其他人的修改')
  await p.save()
  await p.reset(topic)
  assert.equal(p.requests.length, 2)
  revision = 3
  await p.load()
  assert.equal(p.conflicted.value, false)
  assert.equal(p.editing.value, null)
  assert.equal(p.snapshot.value.revision, 3)
})

test('恢复主题默认配置使用 DELETE 和当前修订，保留服务器实际状态', async () => {
  const custom = { ...topic, id:'mqtt/topic 1', overridden:true, topic:`${prefixes.mqtt}custom`, enabled:false }
  const p = page((path, options) => Promise.resolve(options.method ? catalog(10) : catalog(9, [custom])))
  await p.load()
  await p.reset(p.snapshot.value.items[0])
  assert.equal(p.requests[1].path, '/api/v1/message-topics/mqtt%2Ftopic%201?revision=9')
  assert.equal(p.requests[1].options.method, 'DELETE')
  assert.equal(p.snapshot.value.items[0].topic, topic.defaultTopic)
  assert.equal(p.snapshot.value.items[0].enabled, true)
})

test('只读主题和只读权限不能写入，修改身份后旧编辑不能提交', async () => {
  const p = page(() => Promise.resolve(catalog()))
  await p.load()
  p.openEditor({ ...topic, editable:false })
  assert.equal(p.editing.value, null)
  await p.reset({ ...topic, editable:false })
  p.openEditor(topic)
  p.session.token = 'new-session'
  await p.save()
  assert.equal(p.requests.length, 1)
  const reader = page(() => Promise.resolve(catalog()), ['menu:messageTopics'])
  await reader.load()
  reader.openEditor(topic)
  await reader.reset(topic)
  assert.equal(reader.editing.value, null)
  assert.equal(reader.requests.length, 1)
})

test('卸载和身份变更后迟到的主题加载或保存响应不会污染新页面或通知成功', async () => {
  let resolveLoad
  const p = page(() => new Promise(resolve => { resolveLoad = resolve }))
  const loading = p.load()
  p.session.tenant = 'other-tenant'
  resolveLoad(catalog())
  await loading
  assert.equal(p.snapshot.value, null)

  let resolveSave
  const q = page((path, options) => options.method ? new Promise(resolve => { resolveSave = resolve }) : Promise.resolve(catalog()))
  await q.load()
  q.openEditor(topic)
  const saving = q.save()
  q.hooks.unmount()
  assert.equal(q.requests[1].options.signal.aborted, true)
  resolveSave(catalog(5))
  await saving
  assert.equal(q.snapshot.value.revision, 1)
  assert.equal(q.alerts.length, 0)
})

test('权限撤销立即清除主题内容并取消请求', async () => {
  const p = page(() => Promise.resolve(catalog()))
  await p.load()
  p.openEditor(topic)
  p.permissionState.items = []
  assert.equal(p.snapshot.value, null)
  assert.equal(p.editing.value, null)
  assert.equal(p.loading.value, false)
})
