import assert from 'node:assert/strict'
import test from 'node:test'
import vm from 'node:vm'
import { readFileSync } from 'node:fs'
import { computed, reactive, ref, watch } from 'vue'
import { setupScript } from './helpers/vue.mjs'
import { loadAllPages } from '../src/listPagination.js'
import { createDeviceCatalog } from '../src/deviceCatalog.js'
import { profileDraft as qualityDraft, profilePayload as qualityPayload } from '../src/quality/helpers.js'
import { profileDraft as monitoringDraft, profilePayload as monitoringPayload } from '../src/monitoring/helpers.js'
const tick = () => new Promise(resolve => setImmediate(resolve))
function component(file, { devices = [], request = async () => ({ items: [], total: 0 }) } = {}) {
  const props = reactive({ profiles: [], devices, deviceId: '' }), rows = ref(devices), catalog = createDeviceCatalog(request, rows), unmounts = [], calls = [], writes = []
  const context = vm.createContext({ computed, reactive, ref, watch, inject: () => catalog, deviceCatalogKey: 'catalog', session: reactive({ tenant: 't', user: 'u' }), permissionState: reactive({ accessVersion: 'v' }), onBeforeUnmount: fn => unmounts.push(fn), defineProps: () => props, defineEmits: () => () => {}, apiAll: (path, options) => loadAllPages(async (url, opts) => { calls.push(url); return request(url, opts) }, path, options), AbortController, URLSearchParams, can: () => true, profileDraft: file === 'QualityProfiles' ? qualityDraft : monitoringDraft, profilePayload: file === 'QualityProfiles' ? qualityPayload : monitoringPayload, createClientId: () => 'id', qualityWrite: async (...args) => writes.push(args), monitoringWrite: async (...args) => writes.push(args), notifyError() {}, UiMessage: { success() {} } })
  vm.runInContext(readFileSync(new URL('../src/useProfileTargets.js', import.meta.url), 'utf8').replace(/^import .*$/gm, '').replace(/export /g, ''), context)
  const folder = file === 'QualityProfiles' ? 'data-quality' : 'monitoring-gaps'
  const state = vm.runInContext(setupScript(new URL(`../src/components/${folder}/${file}.vue`, import.meta.url)) + ';({edit,form,dialog,targetChanged,targetLoading,products,loadProducts,progress,save,error:' + (file === 'QualityProfiles' ? 'formError' : 'error') + '})', context)
  return { state, calls, writes, props, unmount: () => unmounts.forEach(fn => fn()) }
}
for (const file of ['QualityProfiles', 'MonitoringProfiles']) {
  test(`${file}: cold and partial picker caches never truncate explicit product or tenant scopes`, async () => {
    for (const devices of [[], [{ id: 'cached', productId: 'p', productName: '只读过一页' }]]) {
      const c = component(file, { devices, request: async path => {
        const url = new URL(path, 'http://fixture'), page = Number(url.searchParams.get('page'))
        if (url.pathname.endsWith('/products')) return { items: [{ id: 'p', name: '实际产品' }, { id: 'unseen-product', name: '从未缓存产品' }], total: 2 }
        const total = url.searchParams.get('productId') ? 201 : 250
        return { total, items: Array.from({ length: Math.max(0, Math.min(100, total - (page - 1) * 100)) }, (_, i) => ({ device: { id: `actual-${(page - 1) * 100 + i}`, productId: 'p' } })) }
      } })
      c.state.edit(); assert.equal(c.calls.length, 0)
      await c.state.loadProducts(true); assert(c.state.products.value.some(row => row.id === 'unseen-product'))
      c.state.form.targetType = 'PRODUCT'; c.state.form.productId = 'p'; await c.state.targetChanged()
      assert.equal(c.state.form.deviceIds.length, 201); assert.equal(c.state.progress.value.read, 201)
      assert(c.calls.filter(path => path.includes('device-registry')).every(path => new URL(path, 'http://fixture').searchParams.get('productId') === 'p'))
      c.state.form.targetType = 'TENANT'; await c.state.targetChanged(); assert.equal(c.state.form.deviceIds.length, 250)
      c.unmount()
    }
  })
  test(`${file}: an oversized target reports its actual limit without selecting a cached or truncated subset`, async () => {
    const c = component(file, { devices: [{ id: 'cached' }], request: async () => ({ total: 1001, items: [{ device: { id: 'first' } }] }) })
    c.state.edit(); c.state.form.targetType = 'TENANT'; await c.state.targetChanged()
    assert.equal(c.calls.length, 1); assert.equal(c.state.form.deviceIds.length, 0); assert.match(c.state.error.value, /1000/)
    c.unmount()
  })
  test(`${file}: closing a range read aborts pagination, ignores late results and blocks saving while it is pending`, async () => {
    let finish, signal
    const c = component(file, { request: (_path, options) => { signal = options.signal; return new Promise(resolve => finish = resolve) } })
    c.state.edit(); c.state.form.targetType = 'TENANT'; const loading = c.state.targetChanged()
    await c.state.save(); assert.equal(c.writes.length, 0); assert.equal(c.state.targetLoading.value, true)
    c.state.dialog.value = false; await tick(); assert.equal(signal.aborted, true)
    finish({ total: 201, items: Array(100).fill({ device: { id: 'late' } }) }); await loading
    assert.equal(c.calls.length, 1); assert.equal(c.state.form.deviceIds.length, 0); c.unmount()
  })
}
