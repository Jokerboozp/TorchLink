import test from 'node:test'
import assert from 'node:assert/strict'
import vm from 'node:vm'
import { computed, reactive, ref, watch } from 'vue'
import { setupScript } from './helpers/vue.mjs'
import { parseDeviceRows, restoreEnrollDraft, protocolOptions, STANDARD_PROTOCOL } from '../src/onboardingPlan.js'

test('批量清单支持带逗号和换行的CSV字段并拒绝重复身份', () => {
  const rows = parseDeviceRows('设备编号,设备名称,位置备注\nsensor-1,"一层,东侧","安装位置\n走廊"\nsensor-2,西侧,一层')
  assert.equal(rows.length, 2)
  assert.equal(rows[0].device.name, '一层,东侧')
  assert.equal(rows[0].device.description, '安装位置\n走廊')
  assert.throws(() => parseDeviceRows('same,A\nsame,B'), /重复/)
  assert.throws(() => parseDeviceRows('dev,"未闭合'), /引号/)
})
test('批量主动连接需要逐行设备地址且保留站号0，不允许清单覆盖租户', () => {
  const [row] = parseDeviceRows('dev-1,设备,位置,192.0.2.1,502,0', 'poll')
  assert.deepEqual(row.connection, { mode: 'poll', host: '192.0.2.1', port: 502, unitId: 0, timeoutMs: 3000 })
  assert.throws(() => parseDeviceRows('dev-2,设备', 'dial'), /设备地址/)
  assert.throws(() => parseDeviceRows('dev-2,设备,,192.0.2.2,70000', 'dial'), /端口/)
  assert.equal(Object.hasOwn(row.device, 'tenantId'), false)
})
test('恢复草稿只重建接入表单，不恢复凭据或隐藏秘密标签', () => {
  const value = restoreEnrollDraft({
    productId: 'p',
    device: { id: 'd', name: 'name', tags: { floor: '1', secret: 'hidden', apiToken: 'hidden' } },
    connection: { mode: 'listener', profileId: 'listener-1' },
    credential: { secret: 'hidden' }
  })
  assert.equal(value.connection.choice, 'listener-1')
  assert.deepEqual(value.labels, [{ key: 'floor', value: '1' }])
  assert.equal(Object.hasOwn(value, 'credential'), false)
  assert.equal(JSON.stringify(value).includes('hidden'), false)
})
function preparation(api) {
  const product = {
    id: 'p',
    name: '消防模板',
    protocolPackageId: 'fire@1',
    transport: 'TCP',
    payloadFormat: 'hex',
    status: 'ENABLED',
    metadata: {}
  }
  const events = []
  const context = vm.createContext({
    computed,
    reactive,
    ref,
    watch,
    api,
    apiAll: api,
    createClientId: () => 'a1b2c3d4',
    STANDARD_PROTOCOL,
    protocolOptions,
    can: () => true,
    session: { tenant: 't', user: 'u' },
    defineProps: () => ({ product, initialStep: 0, draftId: '' }),
    defineEmits:
      () =>
      (...args) =>
        events.push(args),
    onMounted() {},
    onBeforeUnmount() {},
    setInterval,
    clearInterval,
    UiMessage: { success() {}, info() {} },
    formatTime: String
  })
  const script = setupScript(new URL('../src/components/ProductPreparation.vue', import.meta.url))
  const c = vm.runInContext(
    script +
      '\n;({candidate,setPreparation,saveDraft,trial,trialPorts,preparation,selectedProtocol,selectedPackage,protocols,productId,step,error,changeVerificationMode})',
    context
  )
  return { ...c, events, product }
}
const savedPreparation = product => ({
  revision: 4,
  product,
  candidate: {
    product,
    protocolId: 'fire',
    version: '1',
    profiles: [
      {
        id: 'listener',
        mode: 'listener',
        network: 'tcp',
        connectionMode: 'listen',
        host: '0.0.0.0',
        publicHost: 'platform.example',
        port: 20001,
        enabled: true
      }
    ],
    verificationRules: {
      mode: 'periodic',
      minMessages: 2,
      windowSeconds: 300,
      maxGapSeconds: 120,
      requiredMessageTypes: [],
      requiredProperties: [],
      requiredEvents: []
    }
  },
  applied: { fingerprint: 'old' },
  status: 'READY',
  affectedDevices: 5
})
test('未修改模板退出不会重新保存并使已验收配置失效；修改只保存候选', async () => {
  const requests = []
  const c = preparation(async (path, options) => {
    requests.push({ path, body: JSON.parse(options.body) })
    return { ...savedPreparation(c.product), revision: 5, candidate: JSON.parse(options.body).candidate }
  })
  c.setPreparation(savedPreparation(c.product))
  await c.saveDraft()
  assert.equal(requests.length, 0)
  c.candidate.product.name = '新名称'
  await c.saveDraft()
  assert.equal(requests.length, 1)
  assert.match(requests[0].path, /\/preparation$/)
  assert.equal(requests[0].body.revision, 4)
  assert.equal(requests[0].body.candidate.product.name, '新名称')
})
test('隔离试验端口只传给trial，保持候选生产端口与协议版本', async () => {
  const requests = []
  const c = preparation(async (path, options) => {
    if (options) {
      requests.push({ path, body: JSON.parse(options.body) })
      return { trialProductId: 'trial-p', revision: 5 }
    }
    return { ...savedPreparation(c.product), revision: 5, trialProductId: 'trial-p' }
  })
  c.setPreparation(savedPreparation(c.product))
  c.trialPorts.listener = 20002
  await c.trial()
  assert.equal(requests.length, 1)
  assert.match(requests[0].path, /\/trial$/)
  assert.equal(requests[0].body.profiles[0].port, 20002)
  assert.equal(c.candidate.profiles[0].port, 20001)
  assert.equal(c.preparation.value.trialProductId, 'trial-p')
  assert.equal(c.step.value, 3)
})
test('内联协议只接收已发布版本，自动回填当前模板并保留HEX格式', async () => {
  const c = preparation(async () => ({
    items: [
      {
        definition: { id: 'fire', name: '消防' },
        releases: [{ version: '2', status: 'PUBLISHED', transport: 'TCP', payloadFormat: 'hex' }]
      }
    ]
  }))
  c.selectedPackage.value = 'fire@1'
  await c.selectedProtocol({ protocolId: 'fire', version: '2', protocolPackageId: 'fire@2', status: 'VALIDATED' })
  assert.equal(c.selectedPackage.value, 'fire@1')
  await c.selectedProtocol({ protocolId: 'fire', version: '2', protocolPackageId: 'fire@2', status: 'PUBLISHED' })
  assert.equal(c.candidate.protocolId, 'fire')
  assert.equal(c.candidate.version, '2')
  assert.equal(c.candidate.product.payloadFormat, 'hex')
})

test('模板候选保存保留现有属性和命令定义', async () => {
  let saved
  const c = preparation(async (_path, options) => {
    saved = JSON.parse(options.body)
    return { ...savedPreparation(c.product), revision: 5, candidate: saved.candidate }
  })
  const original = savedPreparation(c.product)
  original.candidate.product.thingModel = {
    properties: [{ identifier: 'temperature', name: '温度', dataType: 'number', unit: '℃' }],
    commands: [{ identifier: 'reset', fields: [] }],
    events: []
  }
  c.setPreparation(original)
  c.candidate.product.description = '更新说明'
  await c.saveDraft()
  assert.deepEqual(saved.candidate.product.thingModel, original.candidate.product.thingModel)
})
test('设备草稿与批量记录切换时先清除旧行，迟到的草稿响应不能成为批量入口', async () => {
  const pending = []
  const context = vm.createContext({
    computed,
    reactive,
    ref,
    watch,
    api: () => new Promise(resolve => pending.push(resolve)),
    apiAll: async () => ({ items: [] }),
    createClientId: () => 'id',
    can: () => true,
    defineEmits: () => () => {},
    onMounted() {},
    onBeforeUnmount() {},
    setTimeout,
    clearTimeout
  })
  const script = setupScript(new URL('../src/views/DevicesView.vue', import.meta.url))
  const c = vm.runInContext(script + '\n;({showRecords,records,recordKind,recordsLoading})', context)
  c.records.value = [{ id: 'old-draft' }]
  const first = c.showRecords('drafts')
  assert.equal(c.records.value.length, 0)
  const second = c.showRecords('batches')
  pending[1]({ items: [{ id: 'batch-current' }] })
  await second
  pending[0]({ items: [{ id: 'draft-late', body: { step: 'connection' } }] })
  await first
  assert.equal(c.recordKind.value, 'batches')
  assert.equal(c.records.value[0].id, 'batch-current')
  assert.equal(c.recordsLoading.value, false)
})

test('切换低频与事件验收使用单报文且不检查间隔，避免周期规则阻断', () => {
  const c = preparation(async () => ({}))
  c.candidate.verificationRules.mode = 'low_frequency'
  c.changeVerificationMode()
  assert.equal(c.candidate.verificationRules.minMessages, 1)
  assert.equal(c.candidate.verificationRules.maxGapSeconds, 0)
  assert.equal(c.candidate.verificationRules.windowSeconds, 86400)
  c.candidate.verificationRules.mode = 'event'
  c.changeVerificationMode()
  assert.equal(c.candidate.verificationRules.minMessages, 1)
  assert.equal(c.candidate.verificationRules.maxGapSeconds, 0)
})
function batchContext(api) {
  let dispose
  const session = { token: 'identity-one' },
    context = vm.createContext({
      computed,
      ref,
      watch,
      api,
      apiAll: api,
      session,
      createClientId: () => 'batch-id',
      parseDeviceRows,
      defineProps: () => ({ batchId: 'batch-id' }),
      defineEmits: () => () => {},
      onMounted() {},
      onBeforeUnmount(fn) {
        dispose = fn
      },
      setInterval,
      clearInterval,
      UiMessage: { success() {} }
    })
  const c = vm.runInContext(
    setupScript(new URL('../src/components/DeviceBatchOnboarding.vue', import.meta.url)) + '\n;({claim,secrets,refresh,batch})',
    context
  )
  return { ...c, dispose, session }
}
test('批量密钥领取的迟到响应不能在离开或身份改变后恢复秘密', async () => {
  for (const changeIdentity of [false, true]) {
    let resolve
    const c = batchContext(
      () =>
        new Promise(done => {
          resolve = done
        })
    )
    const result = c.claim()
    if (changeIdentity) c.session.token = 'identity-two'
    else c.dispose()
    resolve({ items: [{ credential: { secret: 'must-not-appear' } }] })
    await result
    assert.equal(c.secrets.value, null)
  }
})
test('批量详情较晚返回的旧页请求不能覆盖最新结果', async () => {
  const requests = [],
    c = batchContext(() => new Promise(done => requests.push(done)))
  const first = c.refresh(),
    second = c.refresh()
  requests[1]({ id: 'latest' })
  await second
  requests[0]({ id: 'old' })
  await first
  assert.equal(c.batch.value.id, 'latest')
  c.dispose()
})
function diagnosisContext(status, api, allowed = true) {
  const props = reactive({ status }),
    events = [],
    session = { token: 'first', role: 'admin' },
    context = vm.createContext({
      computed,
      reactive,
      ref,
      watch,
      api,
      session,
      can: () => allowed,
      formatTime: String,
      CheckCircle2: {},
      CircleDashed: {},
      XCircle: {},
      diagnosisTagTypes: {},
      defineProps: () => props,
      defineEmits:
        () =>
        (...args) =>
          events.push(args),
      onBeforeUnmount() {},
      UiMessage: { success() {} }
    })
  const c = vm.runInContext(
    setupScript(new URL('../src/components/OnboardingDiagnosis.vue', import.meta.url)) +
      '\n;({editConnection,saveConnection,connectionForm,canCorrect,debugProtocol})',
    context
  )
  return { ...c, events, session }
}
test('连接纠正仅修改本设备poll或dial参数，共享监听与无权限不可编辑', async () => {
  let saved
  const status = {
    device: { id: 'd', productId: 'p' },
    profile: {
      id: 'profile',
      deviceId: 'd',
      productId: 'p',
      mode: 'poll',
      host: '192.0.2.1',
      port: 502,
      unitId: 1,
      protocolId: 'modbus',
      protocolVersion: 'v1',
      queries: [{ id: 'q' }]
    }
  }
  const c = diagnosisContext(status, async (path, options) => {
    saved = { path, body: JSON.parse(options.body) }
  })
  assert.equal(c.canCorrect.value, true)
  c.editConnection()
  c.connectionForm.host = '192.0.2.2'
  c.connectionForm.unitId = 0
  await c.saveConnection()
  assert.equal(saved.path, '/api/v2/device-access-profiles/profile')
  assert.equal(saved.body.host, '192.0.2.2')
  assert.equal(saved.body.unitId, 0)
  assert.deepEqual(saved.body.queries, [{ id: 'q' }])
  assert.deepEqual(c.events, [['refresh']])
  assert.equal(
    diagnosisContext(
      { ...status, profile: { ...status.profile, mode: 'listener', connectionMode: 'listen', deviceId: '' } },
      async () => {}
    ).canCorrect.value,
    false
  )
  assert.equal(diagnosisContext(status, async () => {}, false).canCorrect.value, false)
})
test('解析纠正携带原文和实际版本进入预览，不发送或回放设备数据', () => {
  const c = diagnosisContext(
    {
      device: { id: 'd', productId: 'p' },
      protocolId: 'fire',
      protocolVersion: '2',
      ingest: { rawMessageId: 'raw-id', parseError: 'bad frame' }
    },
    () => {
      throw new Error('不应调用API')
    }
  )
  c.debugProtocol()
  assert.equal(c.events[0][0], 'navigate')
  assert.equal(c.events[0][1], 'protocols')
  assert.equal(c.events[0][2].rawMessageId, 'raw-id')
  assert.equal(c.events[0][2].version, '2')
  assert.equal(c.events[0][2].preview, true)
})
