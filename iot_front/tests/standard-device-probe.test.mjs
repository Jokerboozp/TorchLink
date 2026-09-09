import test from 'node:test'
import assert from 'node:assert/strict'
import { EventEmitter } from 'node:events'
import { createHash } from 'node:crypto'
import { StandardDeviceProbe, standardRawID, deviceRequest } from '../src/standardDeviceProbe.js'

function fixture(tokenOverride) {
  const clients = [], states = [], timers = new Map()
  let tokenCalls = 0, next = 0
  const auth = { username: 'device-key', token: 'short-lived-token', websocketUrl: 'ws://broker/mqtt', expiresIn: 300, publishTopic: '/iot/up/t/p/d/property' }
  const probe = new StandardDeviceProbe({
    token: async () => { tokenCalls++; return tokenOverride ? tokenOverride() : auth },
    onState: (...args) => states.push(args),
    schedule: (fn, delay) => { timers.set(++next, { fn, delay }); return next },
    cancel: id => timers.delete(id),
    connect: (url, options) => {
      const c = new EventEmitter()
      Object.assign(c, { url, options, connected: false, ended: false, messages: [], end() { this.ended = true; this.connected = false; this.emit('close') }, subscribe(topic, options, cb) { this.subscription = topic; cb(null, [{ qos: 1 }]) }, publish(topic, body, options, cb) { this.messages.push({ topic, body, options }); cb() } })
      c.stream = { destroy() { c.connected=false; c.emit('close') } }
      clients.push(c); return c
    }
  })
  return { probe, clients, states, timers, tokenCalls: () => tokenCalls, connected() { const c = clients.at(-1); c.connected = true; c.emit('connect'); return c }, async tick() { const [id, timer] = timers.entries().next().value; timers.delete(id); await timer.fn(); return timer.delay } }
}

test('network loss retries with a fresh token, explicit stop cancels retries', async () => {
  const f = fixture(); await f.probe.start(); const first = f.connected()
  assert.equal(first.subscription, '/iot/down/t/p/d/command')
  assert.equal(first.options.reconnectPeriod, 0)
  f.probe.dropConnection()
  assert.deepEqual(f.states.at(-1), ['RETRYING', 1000])
  await f.tick(); assert.equal(f.tokenCalls(), 2)
  const second = f.connected(); assert.notEqual(first.options.clientId, second.options.clientId)
  f.probe.stop(); assert.equal(second.ended, true); assert.equal(f.timers.size, 0)
  first.emit('connect'); assert.equal(f.states.at(-1)[0], 'DISCONNECTED')
})

test('expired tokens are renewed before reconnect; a late token cannot resurrect stopped clients', async () => {
  const f = fixture(); await f.probe.start(); const first = f.connected()
  assert.equal(await f.tick(), 270000)
  assert.ok(first.ended); assert.equal(f.tokenCalls(), 2)
  f.probe.stop()
  let resolve
  const pending = fixture(() => new Promise(r => { resolve = r }))
  const opening = pending.probe.start(); pending.probe.stop(); resolve({}); await opening
  assert.equal(pending.clients.length, 0)
})

test('credential and broker authentication rejection stop automatic retries', async () => {
  const f = fixture(() => { throw Object.assign(new Error('revoked'), { status: 401 }) })
  await f.probe.start(); assert.equal(f.states.at(-1)[0], 'AUTH_FAILED'); assert.equal(f.timers.size, 0)
  const broker = fixture(); await broker.probe.start()
  broker.clients[0].emit('error', { code: 5 }); broker.clients[0].emit('close')
  assert.equal(broker.states.at(-1)[0], 'AUTH_FAILED'); assert.equal(broker.timers.size, 0)
})

test('offline reports are rejected; explicit retransmission keeps identical bytes and never retained', async () => {
  const f = fixture(); await assert.rejects(f.probe.publish('/topic', '{}'), /未连接/)
  await f.probe.start(); const client = f.connected()
  const body = '{"id":"same-id","timestamp":123,"data":{"temperature":20}}'
  await f.probe.publish('/topic', body); await f.probe.publish('/topic', body)
  assert.equal(client.messages.length, 2); assert.deepEqual(client.messages[0], client.messages[1])
  assert.deepEqual(client.messages[0].options, { qos: 1, retain: false })
  f.probe.stop()
})

test('raw lookup ID matches platform identity scoping and preserves tenant isolation', async () => {
  const expected = 'raw_std_' + createHash('sha256').update('t\0p\0d\0property\0message-1').digest('hex').slice(0, 32)
  assert.equal(await standardRawID('t', 'p', 'd', 'property', 'message-1'), expected)
  assert.notEqual(await standardRawID('other', 'p', 'd', 'property', 'message-1'), expected)
})

test('device credential failures are separate from operator authentication', async () => {
  let sent
  await assert.rejects(deviceRequest('/api/v1/device-mqtt/token', { key: 'key', secret: 'invalid' }, undefined, async (path, options) => {
    sent = options
    return new Response(JSON.stringify({ detail: 'invalid device credentials' }), { status: 401 })
  }), error => error.status === 401)
  assert.equal(sent.headers.Authorization, undefined)
  assert.equal(sent.credentials, 'omit')
  assert.equal(sent.headers['X-Device-Secret'], 'invalid')
  await assert.rejects(deviceRequest('https://other.example/ingest', { key: 'key', secret: 'secret' }), /不支持/)
})

test('repeated network failures back off to 30 seconds and stop cleanly', async () => {
  const f = fixture(() => { throw new TypeError('network unavailable') })
  await f.probe.start()
  for (const delay of [1000, 2000, 4000, 8000, 16000, 30000, 30000]) {
    assert.equal(await f.tick(), delay)
  }
  f.probe.stop(); assert.equal(f.timers.size, 0)
})
