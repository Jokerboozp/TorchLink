import test from 'node:test'
import assert from 'node:assert/strict'
import { STANDARD_PROTOCOL, configurationText, connectionMode, enrollRequest, fieldConfiguration, preflightQuery, protocolOptions, transportChoices, usesPlatformIdentity } from '../src/onboardingPlan.js'

const draft = (overrides = {}) => ({
  source: 'existing', productId: 'product-1', requestId: 'req-1',
  newProduct: { id: 'product_new', name: ' 新型号 ', category: 'smoke', protocolPackageId: 'fire@1.0.0', transport: 'TCP', manufacturer: ' 大华 ', model: '' },
  device: { id: ' device-1 ', name: ' 一层烟感 ', role: 'DIRECT', description: '' },
  labels: [{ key: ' 楼层 ', value: '一层' }, { key: ' ', value: 'ignored' }],
  connection: { choice: '', transport: '', network: '', port: null, publicHost: '', bindHost: '', host: '', unitId: 1, timeoutMs: 3000 },
  ...overrides
})

test('only published releases are offered after the standard protocol', () => {
  const options = protocolOptions([{ definition: { id: 'fire', name: '消防协议' }, releases: [{ version: '1', status: 'PUBLISHED', transport: 'TCP_UDP' }, { version: '2', status: 'VALIDATED' }] }])
  assert.deepEqual(options.map(item => item.id), [STANDARD_PROTOCOL, 'fire@1'])
  assert.deepEqual(transportChoices('TCP_UDP'), ['TCP', 'UDP'])
  assert.deepEqual(transportChoices('TCP'), [])
})

test('preflight uses the saved template or the template draft', () => {
  assert.equal(preflightQuery(draft()), 'productId=product-1')
  const query = new URLSearchParams(preflightQuery(draft({ source: 'new' })))
  assert.equal(query.get('protocolPackageId'), 'fire@1.0.0')
  assert.equal(query.get('transport'), 'TCP')
  assert.equal(query.get('productId'), null)
})

test('enroll requests carry only the fields of the chosen connection', () => {
  const standard = enrollRequest(draft({ connection: { ...draft().connection, transport: 'HTTP' } }), { mode: 'standard' })
  assert.deepEqual(standard, { requestId: 'req-1', productId: 'product-1', device: { id: 'device-1', name: '一层烟感', deviceRole: 'DIRECT', tags: { 楼层: '一层' } }, connection: { mode: 'standard', transport: 'HTTP' } })

  const listenerPlan = { mode: 'listener', networks: ['tcp', 'udp'], dial: true }
  const shared = enrollRequest(draft({ connection: { ...draft().connection, choice: 'fire-tcp-26875' } }), listenerPlan)
  assert.deepEqual(shared.connection, { mode: 'listener', profileId: 'fire-tcp-26875' })
  const created = enrollRequest(draft({ connection: { ...draft().connection, choice: 'new', publicHost: ' iot.example.com ', port: 26875 } }), listenerPlan)
  assert.deepEqual(created.connection, { mode: 'listener', listener: { network: 'tcp', host: '', publicHost: 'iot.example.com', port: 26875 } })
  assert.equal(connectionMode(listenerPlan, 'dial'), 'dial')
  const dial = enrollRequest(draft({ connection: { ...draft().connection, choice: 'dial', host: ' 10.0.0.8 ', port: 9000 } }), listenerPlan)
  assert.deepEqual(dial.connection, { mode: 'dial', host: '10.0.0.8', port: 9000 })

  const poll = enrollRequest(draft({ connection: { ...draft().connection, host: '192.168.1.20', port: null, unitId: 3 } }), { mode: 'poll' })
  assert.deepEqual(poll.connection, { mode: 'poll', host: '192.168.1.20', port: undefined, unitId: 3, timeoutMs: 3000 })
  assert.equal(JSON.parse(JSON.stringify(poll)).connection.port, undefined, 'an empty port lets the server apply 502')
})

test('a new template is created in the same request', () => {
  const body = enrollRequest(draft({ source: 'new' }), { mode: 'managed' })
  assert.equal(body.productId, undefined)
  assert.deepEqual(body.newProduct, { id: 'product_new', name: '新型号', category: 'smoke', protocolPackageId: 'fire@1.0.0', transport: 'TCP', metadata: { manufacturer: '大华' } })
  assert.ok(usesPlatformIdentity('managed') && usesPlatformIdentity('standard') && !usesPlatformIdentity('listener'))
})

test('device-side configuration never includes a secret that is no longer shown', () => {
  const result = { mode: 'standard', device: { id: 'd1', name: '烟感', connector: 'MQTT' } }
  const accessInfo = { kind: 'standard', mqttBroker: 'mqtts://iot.example.com:8883', clientId: 'device-dk_1', upTopic: '/iot/up/t/p/d1/property', downTopic: '/iot/down/t/p/d1/command', tokenEndpoint: '/api/v1/device-mqtt/token', username: 'dk_1', sample: { version: '1.0' } }
  const withSecret = configurationText(result, accessInfo, { accessKey: 'dk_1', secret: 'ds_secret' })
  assert.match(withSecret, /Secret：ds_secret/)
  assert.match(withSecret, /MQTT Broker：mqtts:\/\/iot.example.com:8883/)
  const later = configurationText(result, accessInfo, null)
  assert.doesNotMatch(later, /ds_secret|Secret：/)
  assert.match(later, /AccessKey：dk_1/)
  const listener = fieldConfiguration({ mode: 'listener', device: { id: 'gw' }, profile: { publicHost: '', port: 26875, network: 'tcp' } }, null, null)
  assert.deepEqual(listener, [{ name: '服务器地址', value: '接入点未配置平台对外地址' }, { name: '网络', value: 'TCP' }])
})
