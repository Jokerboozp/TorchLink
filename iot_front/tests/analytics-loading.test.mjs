import test from 'node:test'
import assert from 'node:assert/strict'
import vm from 'node:vm'
import { setupScript } from './helpers/vue.mjs'
import { createDeviceCatalog } from '../src/deviceCatalog.js'
import { readRuleSources } from '../src/rulelab/sources.js'
import { loadAllPages } from '../src/listPagination.js'
import { computed, reactive, ref } from 'vue'
function deferred() { let resolve; const promise = new Promise(r => resolve = r); return { promise, resolve } }
const tick = async () => { for (let i = 0; i < 10; i++) await Promise.resolve() }
function component(file, extra = {}) {
  const unmounts = [], calls = [], pageTwo = deferred(), sessionStorage = { getItem: () => null, setItem: () => {}, removeItem: () => {} }
  const output = async (resource, ...args) => { calls.push([resource, ...args]); return { items: [{ id: 'business-row', resourceId: 'business-row', version: 1, createdAt: 1, body: {}, deviceIds: ['d'] }], total: 1 } }
  const context = vm.createContext({
    createDeviceCatalog, readRuleSources, AbortController, computed, reactive, ref, provideDeviceCatalog: rows => createDeviceCatalog(output, rows), watch: () => {}, defineProps: () => ({ deviceId: '', ruleId: '' }), defineEmits: () => () => {}, onMounted: () => {}, onBeforeUnmount: fn => unmounts.push(fn),
    can: () => true, session: { tenant: 't', user: 'u' }, permissionState: { accessVersion: 'v' }, sessionStorage, createClientId: () => 'key', URLSearchParams, setTimeout: () => 1, clearTimeout: () => {},
    api: output, notifyError: () => {}, recordBody: v => ({ ...v.body, id: v.id, deviceIds: v.deviceIds }), runIsActive: () => false, activeRun: () => false,
    governanceCatalog: output, governanceRead: output, governanceWrite: output,
    qualityCatalog: output, qualityAll: output, qualityRead: output, qualityWrite: output,
    monitoringAll: output, monitoringRead: output, monitoringWrite: output,
    responseAll: output, responseRead: output, responseWrite: output,
    maintenanceAll: output, maintenanceRead: output, maintenanceWrite: output,
    ruleLabAll: output, ruleLabRead: output, ruleLabWrite: output,
    apiAll: (path, options) => loadAllPages(async url => {
      const page = Number(new URL(url, 'http://fixture').searchParams.get('page')); calls.push(['device-page', page, Boolean(options?.signal)])
      if (page === 2) await pageTwo.promise
      return { total: 201, items: Array.from({ length: page < 3 ? 100 : 1 }, (_, i) => ({ device: { id: `device-${page}-${i}` } })) }
    }, path, options),
    ...extra
  })
  vm.runInContext(setupScript(new URL(`../src/views/${file}.vue`, import.meta.url)) + ';globalThis.audit={load,loading,error,' + (file === 'AlarmGovernanceView' ? 'cases' : file === 'DataQualityView' || file === 'MonitoringGapsView' ? 'runs' : file === 'ResponseReviewView' ? 'executions' : file === 'MaintenanceInvestmentsView' ? 'assets' : 'datasets,devices,revisions,loadSources,sourceError') + '}', context)
  return { state: context.audit, unmounts, calls, pageTwo }
}
for (const [file, list] of [['AlarmGovernanceView', 'cases'], ['DataQualityView', 'runs'], ['MonitoringGapsView', 'runs'], ['ResponseReviewView', 'executions'], ['MaintenanceInvestmentsView', 'assets'], ['RuleLabView', 'datasets']]) {
  test(`${file}: independent business list is readable before the complete device selector catalog`, async () => {
    const c = component(file); const work = c.state.load(); await tick()
    const observed = { rows: c.state[list].value.length, loading: c.state.loading.value, requestedBusinessList: c.calls.some(row => row[0] === (list === 'executions' ? 'drills' : list)), devicePages: c.calls.filter(row => row[0] === 'device-page').map(row => row[1]) }
    c.pageTwo.resolve(); await work
    assert(observed.rows >= 1, JSON.stringify(observed))
  })
}
test('leaving governance stops issuing additional catalog pages', async () => {
  const c = component('AlarmGovernanceView'); const work = c.state.load(); await tick()
  for (const unmount of c.unmounts) unmount()
  c.pageTwo.resolve(); await work
  assert.equal(c.calls.filter(row => row[0] === 'device-page' && row[1] > 2).length, 0, JSON.stringify(c.calls.filter(row => row[0] === 'device-page')))
})

test('paginated catalog abort stops before a following page even when current I/O ignores the signal', async () => {
 const controller = new AbortController(), seen = []
 const result = loadAllPages(async path => { seen.push(path); controller.abort(); return { total: 201, items: Array(100).fill({ id: 'd' }) } }, '/api/v1/device-registry', { signal: controller.signal })
 await assert.rejects(result, { name: 'AbortError' }); assert.equal(seen.length, 1)
})
test('rule lab independently reads lists without requesting rule sources for unselected directory devices', async () => {
 const c = component('RuleLabView'); await c.state.load()
 assert.equal(c.state.datasets.value.length, 1)
 assert.equal(c.calls.filter(row => row[0] === 'rule-sources').length, 0)
 assert.equal(c.calls.filter(row => row[0] === 'device-page').length, 0)
})

test('rule source failure never erases independently loaded rule-lab records', async () => {
 const sourceCalls = []
 const c = component('RuleLabView', { ruleLabAll: async (kind,_id,_op,query) => {
  if(kind === 'rule-sources'){sourceCalls.push(query.deviceIds);throw new Error('来源暂时不可读')}
  return { items: [{ id: 'persisted', body: {}, deviceIds: ['selected'] }], total: 1 }
 } })
 c.state.devices.value = Array.from({length:1001}, (_,i) => ({id:'directory-'+i}))
 await c.state.load(); assert.equal(sourceCalls.length,0)
 await c.state.loadSources(['selected']); assert.deepEqual(sourceCalls,['selected'])
 assert.equal(c.state.datasets.value[0].id,'persisted');assert.match(c.state.sourceError.value,/来源暂时/)
})
test('replacing the actual experiment scope aborts earlier sources and keeps only the new selected input rules', async () => {
 const pending=[]
 const c=component('RuleLabView',{ruleLabAll: (_kind,_id,_op,query,options)=>new Promise(resolve=>pending.push({query,options,resolve}))})
 const first=c.state.loadSources(['old-input']),second=c.state.loadSources(['new-input'])
 assert.equal(pending[0].options.signal.aborted,true)
 pending[1].resolve({items:[{id:'new-revision',ruleId:'new-rule',rule:{id:'new-rule',name:'新范围规则'}}]});await second
 pending[0].resolve({items:[{id:'old-revision',ruleId:'old-rule',rule:{id:'old-rule',name:'旧范围规则'}}]});await first
 assert.deepEqual(Array.from(c.state.revisions.value,row=>row.id),['new-revision'])
})
