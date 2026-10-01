import assert from 'node:assert/strict'
import test from 'node:test'
import vm from 'node:vm'
import { computed, reactive, ref, watch } from 'vue'
import { setupScript } from './helpers/vue.mjs'
import { createDeviceCatalog } from '../src/deviceCatalog.js'
const tick = async () => { await new Promise(resolve => setImmediate(resolve)) }
function component(request, values = {}) {
  const props = reactive({ modelValue: '', ...values }), session = reactive({ tenant: 't', user: 'u' }), permissionState = reactive({ accessVersion: 'v' })
  const rows = ref([]), catalog = createDeviceCatalog(request, rows), unmounts = [], timers = new Map(), events = []
  const context = vm.createContext({ computed, reactive, ref, watch, defineProps: () => props, defineEmits: () => (...args) => events.push(args), inject: () => catalog, deviceCatalogKey: 'catalog', createDeviceCatalog, api: request, session, permissionState, AbortController, URLSearchParams, onBeforeUnmount: fn => unmounts.push(fn), setTimeout: fn => { timers.set(1, fn); return 1 }, clearTimeout: id => timers.delete(id) })
  const state = vm.runInContext(setupScript(new URL('../src/components/DeviceSelect.vue', import.meta.url)) + ';({load,opened,search,scroll,changed,options,items,error,loading})', context)
  return { state, props, catalog, rows, session, permissionState, events, flushSearch: async () => { for (const fn of timers.values()) await fn(); timers.clear(); await tick() }, unmount: () => unmounts.forEach(fn => fn()) }
}
test('selector reads bounded server pages on demand and preserves the selected device when search results replace options', async () => {
  const calls = []
  const c = component(async path => {
    calls.push(path)
    if (path.endsWith('/connection')) return { device: { id: 'old', name: '历史选中设备' } }
    const query = new URL(path, 'http://fixture').searchParams
    return { total: query.get('q') ? 1 : 60, items: Array.from({ length: query.get('q') ? 1 : query.get('page') === '1' ? 50 : 10 }, (_, i) => ({ device: { id: `${query.get('q') || query.get('page')}-${i}`, name: '实际设备' } })) }
  }, { modelValue: ['old'], multiple: true })
  await tick(); assert.equal(calls.filter(path => !path.endsWith('/connection')).length, 0)
  await c.state.load(); assert.equal(c.state.items.value.length, 50)
  await c.state.load(true); assert.equal(c.state.items.value.length, 60)
  c.state.search('消防'); await c.flushSearch()
  assert.equal(c.state.items.value.length, 1)
  assert.equal(c.state.options.value.find(row => row.id === 'old').name, '历史选中设备')
  assert(calls.some(path => new URL(path, 'http://fixture').searchParams.get('q') === '消防'))
  assert(calls.filter(path => !path.endsWith('/connection')).every(path => new URL(path, 'http://fixture').searchParams.get('pageSize') === '50'))
  c.unmount()
})
test('a replaced query aborts its request and a late old response cannot replace newer search data', async () => {
  const pending = []
  const c = component((path, options) => new Promise(resolve => pending.push({ path, options, resolve })))
  const first = c.state.load(); c.state.search('新名称')
  assert.equal(pending[0].options.signal.aborted, true)
  const search = c.flushSearch(); await tick()
  pending[1].resolve({ total: 1, items: [{ device: { id: 'new', name: '新名称' } }] }); await search
  pending[0].resolve({ total: 1, items: [{ device: { id: 'old', name: '旧数据' } }] }); await first
  assert.deepEqual(Array.from(c.state.items.value, row => row.id), ['new'])
  c.unmount()
})
test('leaving a selector aborts pending pagination and prevents writes after unmount', async () => {
  let finish, signal
  const c = component((_path, options) => { signal = options.signal; return new Promise(resolve => finish = resolve) })
  const work = c.state.load(); c.unmount(); assert.equal(signal.aborted, true)
  finish({ total: 100, items: [{ device: { id: 'late' } }] }); await work
  assert.equal(c.state.items.value.length, 0)
})
test('changing user clears selected metadata and ignores old identity lookups', async () => {
  let finish, signal
  const c = component((_path, options) => { signal = options.signal; return new Promise(resolve => finish = resolve) }, { modelValue: 'historical' })
  await tick(); c.session.user = 'other'; await tick()
  assert.equal(signal.aborted, true)
  finish({ device: { id: 'historical', name: '前一身份设备' } }); await tick()
  assert.equal(c.rows.value.length, 0)
  assert.equal(c.state.options.value[0].name, undefined)
  c.unmount()
})
test('failed lookup retains the selected ID without substituting another device', async () => {
  const c = component(async () => { throw Object.assign(new Error('设备已撤销或删除'), { status: 404 }) }, { modelValue: 'removed' })
  await tick(); assert.equal(c.state.options.value[0].id, 'removed'); assert.equal(c.rows.value.length, 0)
  c.unmount()
})
