import assert from 'node:assert/strict'
import test from 'node:test'
import vm from 'node:vm'
import { computed, ref } from 'vue'
import { setupScript } from './helpers/vue.mjs'

function component(api, { allowed = true, confirm = async () => {} } = {}) {
  const notices = []
  let unmount
  const context = vm.createContext({ ref, computed, api, can:() => allowed,
    UiMessageBox:{ confirm }, UiMessage:Object.fromEntries(['info','success','error'].map(type => [type,message => notices.push({type,message})])),
    AbortController, setInterval, clearInterval, document:{hidden:false},
    onMounted(){}, onUnmounted(fn){unmount=fn},
  })
  const source = setupScript(new URL('../src/components/AiWorkflowRuns.vue', import.meta.url))
  const panel = vm.runInContext(`${source}\n;({items,error,loadRuns,stopRun})`, context)
  return {panel,notices,unmount:() => unmount()}
}

test('run management waits for server disappearance after accepted stop', async () => {
  let ended = false
  let stopped = false
  const calls = []
  const {panel,notices} = component(async (path,options) => {
    calls.push({path,method:options?.method})
    if (options?.method === 'POST') {stopped=true;return {status:'stopping'}}
    return {items:ended ? [] : [{runId:'run-1',status:stopped?'stopping':'running',workflowName:'智能巡检'}]}
  })
  await panel.loadRuns()
  await panel.stopRun(panel.items.value[0])
  assert.equal(panel.items.value.length,1)
  assert.equal(panel.items.value[0].status,'stopping')
  await panel.stopRun(panel.items.value[0])
  assert.equal(calls.filter(call=>call.method==='POST').length,1)
  assert.match(notices[0].message,/请求已提交/)
  ended=true
  await panel.loadRuns()
  assert.equal(panel.items.value.length,0)
})

test('canceling confirmation and missing stop permission never submit a stop', async () => {
  for (const options of [{allowed:false},{confirm:async()=>{throw new Error('cancel')}}]) {
    let called=false
    const {panel}=component(async()=>{called=true},options)
    await panel.stopRun({runId:'run-1',status:'running'})
    assert.equal(called,false)
  }
})

test('completed run race refreshes and unavailable list preserves last known rows', async () => {
  let offline=false
  const {panel,notices}=component(async (_path,options)=>{
    if(options?.method==='POST')throw Object.assign(new Error('ended'),{status:404})
    if(offline)throw new Error('Harness unavailable')
    return {items:[{runId:'run-1',status:'running'}]}
  })
  await panel.loadRuns()
  await panel.stopRun(panel.items.value[0])
  assert.equal(notices[0].type,'info')
  offline=true
  await panel.loadRuns()
  assert.equal(panel.items.value.length,1)
  assert.equal(panel.error.value,'Harness unavailable')
})

test('unmount aborts list request and ignores late results', async () => {
  let finish, signal
  const {panel,unmount}=component((_path,options)=>{signal=options.signal;return new Promise(resolve=>{finish=resolve})})
  const pending=panel.loadRuns()
  unmount()
  assert.equal(signal.aborted,true)
  finish({items:[{runId:'late'}]})
  await pending
  assert.equal(panel.items.value.length,0)
})
