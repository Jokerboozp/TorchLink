import assert from 'node:assert/strict'
import test from 'node:test'
import vm from 'node:vm'
import { computed, reactive, ref, watch } from 'vue'
import { setupScript } from './helpers/vue.mjs'
import { restoreSummary } from '../src/backupPresentation.js'
import { backupComponents } from '../src/labels.js'

function backupView(api) {
  const notices = []
  let unmount = () => {}
  const context = vm.createContext({computed, reactive, ref, watch, api, can:()=>true, defineEmits:()=>()=>{}, onMounted(){}, onBeforeUnmount(fn){unmount=fn}, URLSearchParams, notifyError:error=>notices.push(error)})
  const script = setupScript(new URL('../src/views/BackupsView.vue', import.meta.url))
  const state = vm.runInContext(script + '\n;({load,filters,page,records,total,loading,showDetail,detail,detailVisible,detailLoading,manifest,manifestPage,manifestTotal,changeManifestPage})', context)
  return {...state,notices,unmount:()=>unmount()}
}

test('backup list returns to the last valid page after records shrink and retains filters', async () => {
  const requests = []
  const c = backupView(async path => {
    const query = new URL(path,'http://test').searchParams; requests.push(query)
    return {items:query.get('page')==='2'?[{id:'remaining'}]:[],total:21}
  })
  c.page.value = 3; c.filters.type = 'FULL'; c.filters.status = 'COMPLETED'
  await c.load()
  assert.equal(c.page.value, 2)
  assert.equal(c.records.value[0].id, 'remaining')
  assert.equal(c.total.value, 21)
  assert.equal(c.loading.value, false)
  assert.deepEqual(requests.map(q=>q.get('page')), ['3','2'])
  assert.ok(requests.every(q=>q.get('type')==='FULL'&&q.get('status')==='COMPLETED'))
  c.unmount()
})

test('backup detail ignores an earlier response and its loading state', async () => {
  const pending = new Map()
  const c = backupView(path=>new Promise((resolve,reject)=>pending.set(path,{resolve,reject})))
  const old = c.showDetail({id:'old',status:'FAILED'})
  const current = c.showDetail({id:'current',status:'FAILED'})
  pending.get('/api/v1/backups/old').resolve({id:'old'}); await old
  assert.equal(c.detail.value, null)
  assert.equal(c.detailLoading.value, true)
  pending.get('/api/v1/backups/current').resolve({id:'current'}); await current
  assert.equal(c.detail.value.id, 'current')
  assert.equal(c.detailLoading.value, false)
  c.unmount()
})

test('backup manifests retain the current backup and newest requested page', async () => {
  const pending = []
  const c = backupView(path=>path.includes('/files?')
    ? new Promise((resolve,reject)=>pending.push({path,resolve,reject}))
    : Promise.resolve({id:path.split('/').at(-1),status:'COMPLETED',type:'FULL'}))
  const old = c.showDetail({id:'old',status:'COMPLETED',type:'FULL'}); await new Promise(resolve=>setImmediate(resolve))
  const current = c.showDetail({id:'current',status:'COMPLETED',type:'FULL'}); await new Promise(resolve=>setImmediate(resolve))
  pending[1].resolve({artifacts:[{filename:'current'}],total:60}); await current
  pending[0].resolve({artifacts:[{filename:'old'}],total:99}); await old
  assert.equal(c.manifest.value.artifacts[0].filename, 'current')
  assert.equal(c.manifestTotal.value, 60)
  c.changeManifestPage(2); c.changeManifestPage(3)
  pending[3].resolve({artifacts:[{filename:'page3'}],total:60}); await new Promise(resolve=>setImmediate(resolve))
  pending[2].resolve({artifacts:[{filename:'page2'}],total:60}); await new Promise(resolve=>setImmediate(resolve))
  assert.equal(c.manifest.value.artifacts[0].filename, 'page3')
  c.unmount()
})

test('closing or leaving backup details suppresses pending results and errors', async () => {
  const pending = []
  const c = backupView(()=>new Promise((resolve,reject)=>pending.push({resolve,reject})))
  const closed = c.showDetail({id:'closed',status:'FAILED'}); c.detailVisible.value = false
  pending[0].resolve({id:'closed'}); await closed
  assert.equal(c.detail.value, null)
  assert.equal(c.detailVisible.value, false)
  assert.equal(c.detailLoading.value, false)
  const left = c.showDetail({id:'left',status:'FAILED'}); c.unmount()
  pending[1].reject(new Error('obsolete')); await left
  assert.equal(c.notices.length, 0)
})

test('failed manifest pagination keeps the displayed page consistent with its files', async () => {
  let fail
  const c = backupView(path => {
    if (!path.includes('/files?')) return Promise.resolve({id:'backup',status:'COMPLETED',type:'FULL'})
    if (new URL(path,'http://test').searchParams.get('page')==='1') return Promise.resolve({artifacts:[{filename:'page1'}],total:40})
    return new Promise((_resolve,reject)=>{fail=reject})
  })
  await c.showDetail({id:'backup',status:'COMPLETED',type:'FULL'})
  c.changeManifestPage(2); fail(new Error('temporary manifest failure'))
  await new Promise(resolve=>setImmediate(resolve))
  assert.equal(c.manifestPage.value, 1)
  assert.equal(c.manifest.value.artifacts[0].filename, 'page1')
  assert.equal(c.notices.length, 1)
  c.unmount()
})

test('matching artifacts with cold source staging stays partial in the recovery result', () => {
  const result = restoreSummary({status:'PARTIAL', kinds:{raw:{restored:40,matches:true},parsed:{restored:40,matches:true}}, components:{application:{status:'restored',matches:true,partial:true,retiredExecutions:2,limitations:['冷存储仅恢复到制品暂存，未重建可查询来源']},applicationObjects:{status:'restored',objects:3,originalHashUnknown:1}}})
  assert.equal(result.tone,'warning')
  assert.ok(result.lines.some(line=>line.includes('80 条')))
  assert.ok(result.lines.some(line=>line.includes('2 项')&&line.includes('退役')))
  assert.ok(result.lines.some(line=>line.includes('旧附件')))
  assert.deepEqual(result.limitations,['冷存储仅恢复到制品暂存，未重建可查询来源'])
  assert.ok(!result.title.includes('不一致'))
})

test('a completed legacy device backup does not acquire application recovery coverage', () => {
  const result=restoreSummary({status:'COMPLETED',kinds:{raw:{restored:1}},components:{application:{status:'not_included'},governance:{status:'not_included'}}})
  assert.equal(result.tone,'success')
  assert.ok(result.lines.some(line=>line.includes('未包含')))
  assert.ok(!result.lines.some(line=>line.includes('已核对')))
  assert.ok(result.lines.some(line=>line.includes('未包含反复报警治理')))
})

test('combined FULL v5 shows application and governance coverage without double-counting retired executions', () => {
  // RestoreResult has no formatVersion. In v5 the shared analysis records and
  // pending executions belong to application; governance owns its domain data.
  const result = restoreSummary({status:'PARTIAL',kinds:{raw:{restored:7},parsed:{restored:7}},components:{
    application:{status:'restored',matches:true,retiredExecutions:2,partial:true,limitations:['冷存储来源仅恢复到制品暂存']},
    applicationObjects:{status:'restored',matches:true,objects:3},
    governance:{status:'restored',matches:true,analysisDocuments:'application',retiredExecutions:0},
    governanceObjects:{status:'restored',matches:true,objects:4},
  }})
  assert.equal(result.status,'PARTIAL')
  assert.equal(result.tone,'warning')
  assert.ok(result.lines.some(line=>line.includes('治理记录及版本关联已核对')))
  assert.ok(result.lines.some(line=>line.includes('4 份治理附件')))
  assert.ok(result.lines.some(line=>line.includes('3 份应用附件')))
  assert.equal(result.lines.filter(line=>line.includes('任务已退役')).length,1)
  assert.ok(result.lines.some(line=>line.includes('2 项待执行')))
  assert.deepEqual(result.limitations,['冷存储来源仅恢复到制品暂存'])
  assert.equal(backupComponents.governance,'反复报警治理')
})

test('legacy governance-only v4 retains its own fixed facts and retirement coverage', () => {
  const result = restoreSummary({status:'COMPLETED',kinds:{raw:{restored:1}},components:{
    application:{status:'not_included'},
    governance:{status:'restored',matches:true,analysisDocuments:'governance',retiredExecutions:1},
    governanceObjects:{status:'restored',objects:0,matches:true},
  }})
  assert.equal(result.tone,'success')
  assert.ok(result.lines.includes('治理固定分析事实已恢复并核对'))
  assert.ok(!result.lines.includes('本备份未包含固定分析与业务版本'))
  assert.ok(result.lines.some(line=>line.includes('1 项治理待执行')))
  assert.ok(result.lines.some(line=>line.includes('0 份治理附件')))
})
