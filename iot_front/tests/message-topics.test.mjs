import assert from 'node:assert/strict'
import test from 'node:test'
import vm from 'node:vm'
import { computed, reactive, ref, watch } from 'vue'
import { setupScript } from './helpers/vue.mjs'
import { filterMessageTopics, topicDirectionLabel, topicDirections, topicStatus, topicVariableLabel, validateTopicTarget } from '../src/messageTopics.js'
import * as topicHelpers from '../src/messageTopics.js'

const prefixes = { mqtt:'/iot/external/74656e616e74/', kafka:'iot.external.74656e616e74.' }
const topic = { id:'mqtt.parsed', name:'解析数据', protocol:'mqtt', direction:'outbound', topic:'/iot/parsed/{tenantId}/{deviceId}', defaultTopic:'/iot/parsed/{tenantId}/{deviceId}', enabled:true, effectiveEnabled:true, editable:true, variables:['tenantId', 'deviceId'], description:'解析成功后发布', overridden:false }
const catalog = (revision = 1, items = [topic]) => ({ revision, items, prefixes, sources:[topic], rules:[], accounts:[], users:[{ username:'consumer', displayName:'外部系统', enabled:true }], authorization:{ mqtt:{ ready:false, reason:'MQTT 尚未配置授权' }, kafka:{ ready:false, reason:'Kafka 尚未配置授权' } }, runtime:{ mqttEnabled:true, kafkaEnabled:false, kafkaParsedEnabled:false } })

function page(api, permissions = ['*']) {
  const hooks = {}, alerts = [], requests = []
  const permissionState = reactive({ items:permissions, accessVersion:'version-1' })
  const session = { token:'token-a', tenant:'tenant-a', user:'operator-a' }
  const can = permission => permissionState.items.includes('*') || permissionState.items.includes(permission)
  const context = vm.createContext({
    computed, reactive, ref, watch, permissionState, session, can, AbortController,
    ...topicHelpers,
    api:(path, options) => { requests.push({ path, options }); return api(path, options) },
    UiMessage:{ success:message => alerts.push(message), warning:message => alerts.push(message) },
    UiMessageBox:{ confirm:async () => {} },
    defineEmits:() => () => {}, onMounted:fn => { hooks.mount = fn }, onBeforeUnmount:fn => { hooks.unmount = fn },
    window:{ location:{ origin:'https://test.invalid' }, addEventListener:(key, fn) => { hooks[key] = fn }, removeEventListener:key => { delete hooks[key] } },
    navigator:{ clipboard:{ writeText:async () => {} } }
  })
  const source = setupScript(new URL('../src/views/MessageTopicsView.vue', import.meta.url))
  const state = vm.runInContext(source + '\n;({ snapshot, loading, saving, error, notice, formError, conflicted, editing, creating, form, filteredItems, filters, emptyText, load, openEditor, save, reset, removeTopic, rowActions, identityChanged, accountForm, accountEditing, accountOpen, accountSecret, brokerCredential, credentialAccount, secretOpen, credentialOpen, openAccount, saveAccount, setAccountEnabled, removeAccount, rotateAccount, openCredentials, closeCredentials, issueCredentials, credentialReason, accountActions, rulesTopic, ruleEditing, ruleMode, ruleForm, ruleRows, ruleSources, topicRules, previewInput, previewOutput, previewError, previewing, openRules, editRule, backToRules, saveRule, removeRule, previewRule, publishTopic, publishForm, publishResult, openPublish, publish, publishOpen })', context)
  return { ...state, hooks, session, permissionState, requests, alerts, context }
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
  assert.match(p.emptyText.value, /暂无消息主题/)
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

test('恢复主题默认配置使用独立 POST reset 和当前修订，保留服务器实际状态', async () => {
  const custom = { ...topic, id:'mqtt/topic 1', overridden:true, topic:`${prefixes.mqtt}custom`, enabled:false }
  const p = page((path, options) => Promise.resolve(options.method ? catalog(10) : catalog(9, [custom])))
  await p.load()
  await p.reset(p.snapshot.value.items[0])
  assert.equal(p.requests[1].path, '/api/v1/message-topics/mqtt%2Ftopic%201/reset?revision=9')
  assert.equal(p.requests[1].options.method, 'POST')
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

test('共享主题创建不绑定数据源，编辑保持原协议与地址，旧隔离转发仍可编辑数据源', async () => {
  let revision = 5
  const shared = { ...topic, id:'topic_new', name:'外部业务', custom:true, shared:true, topic:`${prefixes.mqtt}smoke/reading` }
  const legacy = { ...topic, id:'topic_old', sourceId:topic.id, name:'旧隔离转发', custom:true, topic:'business_data' }
  const p = page((path, options) => Promise.resolve(catalog(options.method ? ++revision : revision, [topic, shared, legacy])))
  await p.load(); p.openEditor()
  Object.assign(p.form, { name:'外部业务', protocol:'mqtt', topic:'smoke/reading' })
  await p.save()
  assert.equal(p.requests[1].path, '/api/v1/message-topics')
  assert.equal(p.requests[1].options.method, 'POST')
  assert.deepEqual(JSON.parse(p.requests[1].options.body), { revision:5, name:'外部业务', protocol:'mqtt', topic:'smoke/reading', enabled:true, description:'' })
  p.openEditor(shared); Object.assign(p.form, { name:'新名称', protocol:'kafka', topic:'must-not-change' }); await p.save()
  assert.deepEqual(JSON.parse(p.requests[2].options.body), { revision:6, name:'新名称', protocol:'mqtt', topic:shared.topic, enabled:true, description:'解析成功后发布' })
  p.openEditor(legacy); p.form.name = '原通道'; await p.save()
  assert.equal(JSON.parse(p.requests[3].options.body).sourceId, topic.id)
  assert.equal(JSON.parse(p.requests[3].options.body).topic, 'business_data')
  await p.reset(shared); assert.equal(p.requests.length, 4)
})

test('删除主题与恢复默认分离，系统主题不能删除，确认期间身份变化不能误删', async () => {
  const p = page((path, options) => Promise.resolve(catalog(options.method ? 2 : 1, options.method ? [] : [topic])))
  await p.load()
  await p.removeTopic({ ...topic, editable:false })
  assert.equal(p.requests.length, 1)
  await p.removeTopic(topic)
  assert.equal(p.requests[1].options.method, 'DELETE')
  assert.equal(p.requests[1].path, '/api/v1/message-topics/mqtt.parsed?revision=1')
  assert.equal(p.snapshot.value.items.length, 0)

  let approve
  const q = page(() => Promise.resolve(catalog()))
  q.context.UiMessageBox.confirm = () => new Promise(resolve => { approve = resolve })
  await q.load()
  const deleting = q.removeTopic(topic)
  q.session.user = 'different-user'
  approve()
  await deleting
  assert.equal(q.requests.length, 1)
})

const account = { id:'account_1', name:'外部管理系统', username:'consumer', enabled:true, topicIds:[topic.id], deviceScope:'all', deviceIds:[], expiresAt:1900000000, createdAt:1800000000, credentials:[] }
const withAccount = (revision = 1, value = account, ready = false) => ({ ...catalog(revision), accounts:[value], authorization:{ mqtt:{ ready, reason:ready ? '' : '未配置受控 MQTT 授权' }, kafka:{ ready:false, reason:'未配置 Kafka 授权' } } })

test('对接账号创建按 Unix 秒提交范围与期限，一次性密钥不进入目录且关闭立即清除', async () => {
  const p = page((path, options) => Promise.resolve(options.method ? { ...withAccount(2), accountSecret:{ id:account.id, secret:'one-time-test-secret' } } : catalog()))
  await p.load()
  p.openAccount()
  Object.assign(p.accountForm, { name:'外部管理系统', username:'consumer', topicIds:[topic.id], publishTopicIds:[], deviceScope:'selected', deviceIds:['device-a'], expiresAt:1900000000000 })
  await p.saveAccount()
  assert.equal(p.requests[1].path, '/api/v1/message-topic-accounts')
  assert.deepEqual(JSON.parse(p.requests[1].options.body), { revision:1, name:'外部管理系统', username:'consumer', enabled:true, topicIds:[topic.id], publishTopicIds:[], deviceScope:'selected', deviceIds:['device-a'], expiresAt:1900000000 })
  assert.equal(p.accountSecret.value.secret, 'one-time-test-secret')
  assert.equal(p.snapshot.value.accountSecret, undefined)
  assert.equal(JSON.stringify(p.snapshot.value).includes('one-time-test-secret'), false)
  p.secretOpen.value = false
  assert.equal(p.accountSecret.value, null)
  p.openAccount(account)
  assert.equal(p.accountForm.expiresAt, 1900000000000)
})

test('账号停用和密钥轮换使用各自精确路由，返回的撤销重试提示保留', async () => {
  const p = page((path, options) => Promise.resolve(options.method ? { ...withAccount(2, { ...account, enabled:false }), warning:'旧凭据撤销仍在重试', ...(path.includes('/rotate') ? { accountSecret:{ id:account.id, secret:'rotated-test-secret' } } : {}) } : withAccount()))
  await p.load()
  await p.setAccountEnabled(account)
  assert.equal(p.requests[1].options.method, 'PUT')
  assert.equal(JSON.parse(p.requests[1].options.body).enabled, false)
  assert.equal(JSON.parse(p.requests[1].options.body).expiresAt, account.expiresAt)
  assert.equal(p.notice.value, '旧凭据撤销仍在重试')
  await p.rotateAccount(account)
  assert.equal(p.requests[2].path, '/api/v1/message-topic-accounts/account_1/rotate?revision=2')
  assert.equal(p.accountSecret.value.secret, 'rotated-test-secret')
  p.permissionState.items = []
  assert.equal(p.accountSecret.value, null)
})

test('Broker 授权未就绪或账号已过期时不能签发临时凭据', async () => {
  const p = page(() => Promise.resolve(withAccount()))
  await p.load()
  p.openCredentials(account)
  assert.equal(p.credentialReason(account, 'mqtt'), '未配置受控 MQTT 授权')
  await p.issueCredentials('mqtt')
  assert.equal(p.requests.length, 1)
  p.snapshot.value.authorization.mqtt.ready = true
  p.credentialAccount.value = { ...account, expiresAt:1 }
  await p.issueCredentials('mqtt')
  assert.equal(p.requests.length, 1)
})

test('临时凭据使用真实返回地址和秒级期限，关闭后密码不可恢复，迟到响应不能跨身份', async () => {
  const issued = { revision:2, protocol:'mqtt', username:'broker-user', password:'temporary-test-password', topics:['/tenant/credential/scoped'], expiresAt:1900000000 }
  let issuedAlready = false
  const p = page((path, options) => { if (options.method) { issuedAlready = true; return Promise.resolve(issued) }; return Promise.resolve(withAccount(issuedAlready ? 2 : 1, account, true)) })
  await p.load()
  p.openCredentials(account)
  await p.issueCredentials('mqtt')
  assert.deepEqual(JSON.parse(p.requests[1].options.body), { revision:1, protocol:'mqtt' })
  assert.equal(p.brokerCredential.value.password, issued.password)
  assert.equal(p.brokerCredential.value.topics[0], '/tenant/credential/scoped')
  assert.equal(JSON.stringify(p.snapshot.value).includes(issued.password), false)
  p.credentialOpen.value = false
  assert.equal(p.brokerCredential.value, null)

  let resolve
  const q = page((path, options) => options.method ? new Promise(done => { resolve = done }) : Promise.resolve(withAccount(1, account, true)))
  await q.load()
  q.openCredentials(account)
  const issuing = q.issueCredentials('mqtt')
  q.permissionState.items = []
  resolve(issued)
  await issuing
  assert.equal(q.brokerCredential.value, null)
  assert.equal(q.credentialAccount.value, null)
  assert.equal(q.alerts.length, 0)
})

test('账号和凭据显示将 Unix 秒转换为时间，已撤销中凭据不会误显为有效', () => {
  assert.equal(topicHelpers.topicAccountStatus({ enabled:true, expiresAt:100 }, 100001).label, '已过期')
  assert.equal(topicHelpers.topicAccountStatus({ enabled:true, expiresAt:101 }, 100001).label, '已启用')
  assert.equal(topicHelpers.credentialStatus({ status:'revoking', expiresAt:9999999999 }), '撤销处理中')
  assert.equal(topicHelpers.formatTopicTime(1900000000), new Date(1900000000000).toLocaleString('zh-CN', { hour12:false }))
  const body = topicHelpers.topicAccountPayload({ name:'name', username:'user', deviceScope:'all', deviceIds:['should-drop'], topicIds:['a','a'], enabled:true, expiresAt:null }, 7)
  assert.deepEqual(body.deviceIds, [])
  assert.deepEqual(body.topicIds, ['a'])
  assert.equal(body.expiresAt, 0)
})

test('签发后读取到较新授权配置时清除刚生成的凭据，要求重新获取', async () => {
  let issued = false
  const p = page((path, options) => {
    if (options.method) { issued = true; return Promise.resolve({ revision:2, protocol:'mqtt', username:'user', password:'obsolete-secret', topics:['/private/topic'], expiresAt:1900000000 }) }
    return Promise.resolve(withAccount(issued ? 3 : 1, account, true))
  })
  await p.load(); p.openCredentials(account); await p.issueCredentials('mqtt')
  assert.equal(p.brokerCredential.value, null)
  assert.match(p.formError.value, /重新获取/)
  assert.equal(p.alerts.length, 0)
})

test('设备选择使用有界服务端搜索，旧查询与卸载后的结果不会覆盖当前设备', async () => {
  const hooks = {}, pending = [], emitted = []
  const props = reactive({ modelValue:['selected-device'], disabled:false })
  const context = vm.createContext({ computed, ref, watch, AbortController, encodeURIComponent, setTimeout, clearTimeout,
    session:{ token:'session', tenant:'tenant', user:'admin' }, permissionState:reactive({ accessVersion:'v1' }),
    defineProps:() => props, defineEmits:() => (event, value) => emitted.push({event,value}),
    onMounted:fn => { hooks.mount = fn }, onBeforeUnmount:fn => { hooks.unmount = fn },
    api:(path, options) => new Promise(resolve => pending.push({path,options,resolve}))
  })
  const p = vm.runInContext(setupScript(new URL('../src/components/MessageTopicDevicePicker.vue', import.meta.url))+'\n;({load,query,rows,toggle})', context)
  const first = p.load(); p.query.value = 'device-101'; const second = p.load()
  assert.equal(pending[0].options.signal.aborted, true)
  assert.match(pending[1].path, /pageSize=50&q=device-101/)
  pending[1].resolve({items:[{device:{id:'device-101',name:'最新搜索'}}],total:1}); await second
  pending[0].resolve({items:[{device:{id:'old'}}],total:10000}); await first
  assert.equal(p.rows.value[0].id, 'device-101')
  p.toggle('device-101', true)
  assert.deepEqual(Array.from(emitted[0].value), ['selected-device','device-101'])
  const late = p.load(); hooks.unmount(); pending[2].resolve({items:[{device:{id:'after-unmount'}}],total:1}); await late
  assert.equal(p.rows.value.length, 0)
})

const sharedTopic = { ...topic, id:'shared-1', custom:true, shared:true, topic:`${prefixes.mqtt}smoke/reading` }
const rule = { id:'rule-1', topicId:sharedTopic.id, name:'温度上报', sourceId:topic.id, enabled:true, deviceScope:'selected', deviceIds:['device-a'], format:'json', fields:{temperature:'properties.temperature'}, template:'' }
const sharedCatalog = (revision = 1, rules = [rule]) => ({ ...catalog(revision, [topic, sharedTopic]), rules, sources:[topic, {...topic, id:'kafka.property',protocol:'kafka'}] })

test('共享主题接受协议后缀与本租户地址，消息大小按 UTF-8 字节限制', () => {
  assert.equal(topicHelpers.validateSharedTopic({name:'name',protocol:'mqtt',topic:'smoke/reading'},prefixes),'')
  assert.equal(topicHelpers.validateSharedTopic({name:'name',protocol:'kafka',topic:'smoke.reading'},prefixes),'')
  assert.equal(topicHelpers.validateSharedTopic({name:'name',protocol:'mqtt',topic:sharedTopic.topic},prefixes),'')
  assert.match(topicHelpers.validateSharedTopic({name:'name',protocol:'mqtt',topic:'/iot/external/other/data'},prefixes),/完整主题/)
  assert.match(topicHelpers.validateSharedTopic({name:'name',protocol:'mqtt',topic:'smoke/+'},prefixes),/通配符/)
  assert.match(topicHelpers.validateTopicMessage('火'.repeat(90000),'text'),/256 KB/)
  assert.equal(topicHelpers.validateTopicMessage('火'.repeat(80000),'text'),'')
  assert.equal(topicHelpers.validateTopicMessage('[1,true,null]','json'),'')
  assert.match(topicHelpers.validateTopicMessage('{invalid','json'),/JSON/)
})

test('独立规则按主题协议筛选，保存映射设备范围，删除后留在规则列表', async () => {
  let revision=3
  const p=page((path,options)=>Promise.resolve(sharedCatalog(options.method?++revision:revision,path.includes('?revision=')?[]:[rule])))
  await p.load(); p.openRules(sharedTopic)
  assert.equal(p.topicRules.value.length,1); assert.equal(p.ruleSources.value.length,1)
  p.editRule(); Object.assign(p.ruleForm,{name:'告警字段',sourceId:topic.id,deviceScope:'selected',deviceIds:['device-a','device-a'],format:'json'})
  p.ruleRows.value=[{output:'temperature',path:'properties.temperature'},{output:'first',path:'readings[0].value'}]
  await p.saveRule()
  assert.equal(p.requests[1].path,'/api/v1/message-topics/shared-1/rules')
  assert.deepEqual(JSON.parse(p.requests[1].options.body),{revision:3,name:'告警字段',sourceId:topic.id,enabled:true,deviceScope:'selected',deviceIds:['device-a'],format:'json',fields:{temperature:'properties.temperature',first:'readings[0].value'},template:''})
  assert.equal(p.rulesTopic.value.id,sharedTopic.id);assert.equal(p.ruleMode.value,'list')
  p.editRule(rule);p.ruleForm.format='text';p.ruleForm.template='temp={{properties.temperature}}';await p.saveRule()
  assert.equal(p.requests[2].options.method,'PUT');assert.equal(JSON.parse(p.requests[2].options.body).template,'temp={{properties.temperature}}');assert.deepEqual(JSON.parse(p.requests[2].options.body).fields,{})
  await p.removeRule(rule)
  assert.equal(p.requests[3].path,'/api/v1/message-topics/shared-1/rules/rule-1?revision=5')
  assert.equal(p.rulesTopic.value.id,sharedTopic.id);assert.equal(p.topicRules.value.length,0)
})

test('规则拒绝跨协议数据源、空设备范围和重复输出字段', async () => {
  const p=page(()=>Promise.resolve(sharedCatalog()))
  await p.load();p.openRules(sharedTopic);p.editRule()
  Object.assign(p.ruleForm,{name:'rule',sourceId:'kafka.property'})
  await p.saveRule();assert.match(p.formError.value,/触发事件/)
  Object.assign(p.ruleForm,{sourceId:topic.id,deviceScope:'selected',deviceIds:[]})
  await p.saveRule();assert.match(p.formError.value,/至少选择/)
  Object.assign(p.ruleForm,{deviceScope:'all',format:'json'})
  p.ruleRows.value=[{output:'value',path:'a'},{output:'value',path:'b'}]
  await p.saveRule();assert.match(p.formError.value,/不能重复/);assert.equal(p.requests.length,1)
})

test('预览未保存规则不更新配置，修改返回及身份变化均丢弃迟到输出', async () => {
  const pending=[]
  const p=page((path,options)=>options.method?new Promise((resolve,reject)=>pending.push({resolve,reject,options})):Promise.resolve(sharedCatalog()))
  await p.load();p.openRules(sharedTopic);p.editRule(rule);p.previewInput.value='{"properties":{"temperature":25}}'
  const first=p.previewRule();await p.previewRule();assert.equal(pending.length,1)
  const body=JSON.parse(pending[0].options.body);assert.equal(body.rule.fields.temperature,'properties.temperature');assert.equal(Object.hasOwn(body.rule,'revision'),false)
  p.ruleRows.value[0].path='properties.newTemperature';assert.equal(pending[0].options.signal.aborted,true)
  pending[0].resolve({payload:'obsolete'});await first;assert.equal(p.previewOutput.value,null)
  const second=p.previewRule();pending[1].reject(new Error('字段不存在'));await second;assert.match(p.previewError.value,/不存在/)
  p.ruleRows.value[0].path='properties.temperature';const third=p.previewRule();pending[2].resolve({payload:'{"temperature":25}'});await third
  assert.equal(p.previewOutput.value,'{"temperature":25}');assert.equal(p.snapshot.value.revision,1)
  const fourth=p.previewRule();p.backToRules();pending[3].resolve({payload:'late'});await fourth;assert.equal(p.previewOutput.value,null)
  p.editRule(rule);p.previewInput.value='{}';const fifth=p.previewRule();p.permissionState.items=[];pending[4].resolve({payload:'cross identity'});await fifth
  assert.equal(p.previewOutput.value,null);assert.equal(p.rulesTopic.value,null)
})

test('手动发送保留内容和 MQTT QoS，阻止重复发送，回执不替换目录', async () => {
  let finish
  const p=page((path,options)=>options.method?new Promise(resolve=>{finish=resolve}):Promise.resolve(sharedCatalog(7)))
  await p.load();p.openPublish(sharedTopic);Object.assign(p.publishForm,{payload:' {"temperature":25} ',qos:2,key:'unused'})
  const sending=p.publish();await p.publish()
  assert.equal(p.requests.length,2);assert.deepEqual(JSON.parse(p.requests[1].options.body),{revision:7,payload:' {"temperature":25} ',format:'json',qos:2})
  finish({published:true,topic:sharedTopic.topic,bytes:20});await sending
  assert.equal(p.publishResult.value.published,true);assert.equal(p.snapshot.value.revision,7)
  p.publishForm.payload='changed';assert.equal(p.publishResult.value,null)
  await p.publish();assert.match(p.formError.value,/JSON/);assert.equal(p.requests.length,2)
  p.publishOpen.value=false;assert.equal(p.publishForm.payload,'')
})

test('Kafka 手动发送只带 key，失败不自动重试，卸载后不显示回执', async () => {
  const p=page((path,options)=>options.method?Promise.reject(new Error('Broker 未确认')):Promise.resolve(sharedCatalog()))
  await p.load();p.openPublish({...sharedTopic,protocol:'kafka'});Object.assign(p.publishForm,{format:'text',payload:'test',key:'device-a',qos:2})
  await p.publish();assert.equal(p.requests.length,2);assert.match(p.formError.value,/未确认/)
  const body=JSON.parse(p.requests[1].options.body);assert.equal(body.key,'device-a');assert.equal(Object.hasOwn(body,'qos'),false)
  let finish
  const q=page((path,options)=>options.method?new Promise(resolve=>{finish=resolve}):Promise.resolve(sharedCatalog()))
  await q.load();q.openPublish(sharedTopic);q.publishForm.payload='{}';const sending=q.publish();q.hooks.unmount();finish({published:true,bytes:2});await sending
  assert.equal(q.publishResult.value,null);assert.equal(q.publishTopic.value,null)
})

test('纯发布账号无需订阅，旧隔离主题不能授权发布，凭据同时检查发布主题', async () => {
  const form={name:'publisher',username:'consumer',enabled:true,topicIds:[],publishTopicIds:[sharedTopic.id],deviceScope:'all',deviceIds:[],expiresAt:null}
  assert.equal(topicHelpers.validateTopicAccount(form,[topic,sharedTopic],catalog().users),'')
  assert.match(topicHelpers.validateTopicAccount({...form,publishTopicIds:[topic.id]},[topic,sharedTopic],catalog().users),/共享主题/)
  const p=page(()=>Promise.resolve({...sharedCatalog(),authorization:{mqtt:{ready:true}}}))
  await p.load();assert.equal(p.credentialReason(form,'mqtt'),'')
  const body=topicHelpers.topicAccountPayload(form,3);assert.deepEqual(body.topicIds,[]);assert.deepEqual(body.publishTopicIds,[sharedTopic.id])
})

test('只读权限无法编辑规则或发送，旧主题不能进入共享入口', async () => {
  const p=page(()=>Promise.resolve(sharedCatalog()),['menu:messageTopics'])
  await p.load();p.openRules(sharedTopic);p.editRule(rule);await p.saveRule();p.openPublish(sharedTopic);await p.publish()
  assert.equal(p.ruleMode.value,'list');assert.equal(p.publishTopic.value,null);assert.equal(p.requests.length,1)
  const q=page(()=>Promise.resolve(sharedCatalog()));await q.load();q.openRules(topic);q.openPublish(topic)
  assert.equal(q.rulesTopic.value,null);assert.equal(q.publishTopic.value,null)
})
