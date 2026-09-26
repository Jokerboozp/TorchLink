import fs from 'node:fs'
import vm from 'node:vm'
import test from 'node:test'
import assert from 'node:assert/strict'
import { ref, computed } from 'vue'

function fixture(api) {
  const source = fs.readFileSync(new URL('../src/views/RawView.vue', import.meta.url), 'utf8')
  const script = source.match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm, '')
  const warnings = []
  const context = vm.createContext({ ref, computed, defineEmits() {}, onMounted() {}, api, URLSearchParams, UiMessage: { warning: text => warnings.push(text) }, notifyError() {}, messageTypeLabel: x => x })
  return { ...vm.runInContext(script + '\n;({filters, appliedFilters, page, items, total, selection, load, search, resetFilters, recentHours, changePage, parseState, loadError})', context), warnings }
}

test('raw filters combine criteria, preserve applied filters on pagination and reset all fields', async () => {
  const requests = []
  const f = fixture(async url => { requests.push(new URL(url, 'http://test').searchParams); return { items: [], total: 0 } })
  Object.assign(f.filters.value, { deviceId: ' d ', messageId: 'raw-1', productId: 'p', protocol: 'json', payloadFormat: 'json', parseStatus: 'PARSED', messageType: 'ALARM_REPORT', parser: 'json_parser', range: [100, 200] })
  f.page.value = 3
  await f.search()
  const q = requests.at(-1)
  assert.equal(q.get('deviceId'), 'd')
  assert.equal(q.get('page'), '1')
  for (const key of ['messageId','productId','protocol','payloadFormat','parseStatus','messageType','parser']) assert.equal(q.get(key), f.filters.value[key])
  assert.equal(q.get('start'), '100'); assert.equal(q.get('end'), '200')
  f.filters.value.deviceId = 'not-applied'
  f.filters.value.range[0] = 50
  f.changePage(2)
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(requests.at(-1).get('deviceId'), 'd')
  assert.equal(requests.at(-1).get('start'), '100')
  assert.equal(requests.at(-1).get('page'), '2')
  await f.resetFilters()
  assert.deepEqual([...requests.at(-1).keys()].sort(), ['page','pageSize'])
  assert.equal(f.page.value, 1)
  f.filters.value.range = [300,100]
  const count = requests.length
  await f.search()
  assert.equal(requests.length, count)
  assert.equal(f.warnings.length, 1)
  await f.recentHours(24)
  assert.equal(Number(requests.at(-1).get('end')) - Number(requests.at(-1).get('start')), 86400000)
})

test('late raw responses cannot overwrite a newer filter result; failed queries clear stale rows', async () => {
  const pending = []
  const f = fixture(() => new Promise((resolve, reject) => pending.push({ resolve, reject })))
  f.filters.value.deviceId = 'old'; const old = f.search()
  f.filters.value.deviceId = 'new'; const current = f.search()
  pending[1].resolve({ items: [{ messageId:'new' }], total:1 }); await current
  pending[0].resolve({ items: [{ messageId:'old' }], total:99 }); await old
  assert.equal(f.items.value[0].messageId, 'new')
  assert.equal(f.total.value, 1)
  f.selection.value = [{messageId:'new'}]
  const failed = f.load()
  assert.equal(f.selection.value.length, 0)
  pending[2].reject(new Error('offline')); await failed
  assert.equal(f.items.value.length, 0)
  assert.equal(f.loadError.value, 'offline')
  assert.equal(f.parseState({parseError:'invalid'}).label, '解析失败')
  assert.equal(f.parseState({parsed:true, parseError:'old error', parsedMessageType:'ALARM_REPORT'}).tone, 'success')
})
