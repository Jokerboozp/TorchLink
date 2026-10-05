import test from 'node:test'
import assert from 'node:assert/strict'
import { parsePath, pathFor } from '../src/routing.js'

const pages = { dashboard: {}, alarms: {}, opsOverview: {}, notifications: {} }

test('menu pages and alarm details round-trip through the address', () => {
  assert.equal(pathFor('opsOverview'), '/ops-overview')
  assert.deepEqual(parsePath('/ops-overview', pages), { page: 'opsOverview', detail: null })
  assert.equal(pathFor('alarms', { alarmId: 'alarm/1 x' }), '/alarms/alarm%2F1%20x')
  assert.deepEqual(parsePath('/alarms/alarm%2F1%20x', pages), { page: 'alarms', detail: { alarmId: 'alarm/1 x' } })
})

test('simple navigation targets survive refresh; complex objects stay out of the address', () => {
  const path = pathFor('raw', { deviceId: 'device 1', rawMessageId: 'raw/1', range: { from: 'now-6h' } })
  assert.equal(path, '/raw?deviceId=device+1&rawMessageId=raw%2F1')
  const [pathname, search] = path.split('?')
  assert.deepEqual(parsePath(pathname, { raw: {} }, '?' + search), { page: 'raw', detail: { deviceId: 'device 1', rawMessageId: 'raw/1' } })
  assert.deepEqual(parsePath('/alarms/a1', pages, '?deviceId=d1&other=x'), { page: 'alarms', detail: { alarmId: 'a1', deviceId: 'd1' } })
  assert.equal(pathFor('rules', { ruleDraft: { name: 'x' } }), '/rules')
})

test('unknown or malformed paths fall back to the first permitted page', () => {
  for (const path of ['/', '', '/missing', '/%E0%A4%A', '/constructor', '/__proto__']) {
    assert.equal(parsePath(path, pages).page, '', path)
  }
})
