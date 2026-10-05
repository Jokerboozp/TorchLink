import assert from 'node:assert/strict'
import test from 'node:test'
import vm from 'node:vm'
import { computed, ref } from 'vue'
import { setupScript } from './helpers/vue.mjs'

function component(api, { allowed = true, confirm = async () => {} } = {}) {
  const notices = []
  const timeouts = new Map(),
    intervals = new Map()
  let unmount,
    timerId = 0
  const context = vm.createContext({
    setTimeout: (fn, ms) => (timeouts.set(++timerId, { fn, ms }), timerId),
    clearTimeout: id => timeouts.delete(id),
    setInterval: (fn, ms) => (intervals.set(++timerId, { fn, ms }), timerId),
    clearInterval: id => intervals.delete(id),
    ref,
    computed,
    api,
    can: () => allowed,
    UiMessageBox: { confirm },
    UiMessage: Object.fromEntries(['info', 'success', 'error'].map(type => [type, message => notices.push({ type, message })])),
    AbortController,
    onMounted() {},
    onUnmounted(fn) {
      unmount = fn
    }
  })
  const source = setupScript(new URL('../src/components/AiWorkflowRuns.vue', import.meta.url))
  const panel = vm.runInContext(`${source}\n;({items,error,loadRuns,stopRun,now,elapsed})`, context)
  // 依次执行当前排队的定时器（只执行一轮），用于推进停止确认。
  async function runTimeouts() {
    const pending = [...timeouts.entries()]
    for (const [id, timer] of pending) {
      timeouts.delete(id)
      await timer.fn()
    }
  }
  return { panel, notices, timeouts, intervals, runTimeouts, unmount: () => unmount() }
}

test('elapsed time ticks every second while runs exist and stops after unmount', async () => {
  const { panel, intervals, unmount } = component(async () => ({ items: [{ runId: 'run-1', status: 'running', startedAt: 0 }] }))
  await panel.loadRuns()
  assert.equal(intervals.size, 1)
  const [timer] = intervals.values()
  assert.equal(timer.ms, 1000)
  panel.now.value = 0
  timer.fn()
  assert.ok(panel.now.value > 0, '计时器须更新当前时间')
  unmount()
  assert.equal(intervals.size, 0)
})

test('forced stop keeps checking until the run leaves the stopping state, with a bound', async () => {
  let polls = 0
  const { panel, notices, timeouts, runTimeouts } = component(async (_path, options) => {
    if (options?.method === 'POST') return { status: 'stopping' }
    polls += 1
    return { items: polls >= 3 ? [] : [{ runId: 'run-1', status: polls === 1 ? 'running' : 'stopping' }] }
  })
  await panel.loadRuns()
  await panel.stopRun(panel.items.value[0])
  assert.equal(timeouts.size, 1, '仍在停止时安排下一次确认')
  await runTimeouts()
  assert.equal(timeouts.size, 0)
  assert.equal(panel.items.value.length, 0)
  assert.equal(notices.at(-1).message, '任务已停止')

  const stuck = component(async (_path, options) =>
    options?.method === 'POST' ? { status: 'stopping' } : { items: [{ runId: 'run-2', status: 'stopping' }] }
  )
  await stuck.panel.stopRun({ runId: 'run-2', status: 'running' })
  for (let round = 0; round < 10 && stuck.timeouts.size; round += 1) await stuck.runTimeouts()
  assert.equal(stuck.timeouts.size, 0, '确认次数有上限')
  assert.match(stuck.notices.at(-1).message, /仍在停止中/)
})

test('run management waits for server disappearance after accepted stop', async () => {
  let ended = false
  let stopped = false
  const calls = []
  const { panel, notices } = component(async (path, options) => {
    calls.push({ path, method: options?.method })
    if (options?.method === 'POST') {
      stopped = true
      return { status: 'stopping' }
    }
    return { items: ended ? [] : [{ runId: 'run-1', status: stopped ? 'stopping' : 'running', workflowName: '智能巡检' }] }
  })
  await panel.loadRuns()
  await panel.stopRun(panel.items.value[0])
  assert.equal(panel.items.value.length, 1)
  assert.equal(panel.items.value[0].status, 'stopping')
  await panel.stopRun(panel.items.value[0])
  assert.equal(calls.filter(call => call.method === 'POST').length, 1)
  assert.match(notices[0].message, /请求已提交/)
  ended = true
  await panel.loadRuns()
  assert.equal(panel.items.value.length, 0)
})

test('canceling confirmation and missing stop permission never submit a stop', async () => {
  for (const options of [
    { allowed: false },
    {
      confirm: async () => {
        throw new Error('cancel')
      }
    }
  ]) {
    let called = false
    const { panel } = component(async () => {
      called = true
    }, options)
    await panel.stopRun({ runId: 'run-1', status: 'running' })
    assert.equal(called, false)
  }
})

test('completed run race refreshes and unavailable list preserves last known rows', async () => {
  let offline = false
  const { panel, notices } = component(async (_path, options) => {
    if (options?.method === 'POST') throw Object.assign(new Error('ended'), { status: 404 })
    if (offline) throw new Error('Harness unavailable')
    return { items: [{ runId: 'run-1', status: 'running' }] }
  })
  await panel.loadRuns()
  await panel.stopRun(panel.items.value[0])
  assert.equal(notices[0].type, 'info')
  offline = true
  await panel.loadRuns()
  assert.equal(panel.items.value.length, 1)
  assert.equal(panel.error.value, 'Harness unavailable')
})

test('unmount aborts list request and ignores late results', async () => {
  let finish, signal
  const { panel, unmount } = component((_path, options) => {
    signal = options.signal
    return new Promise(resolve => {
      finish = resolve
    })
  })
  const pending = panel.loadRuns()
  unmount()
  assert.equal(signal.aborted, true)
  finish({ items: [{ runId: 'late' }] })
  await pending
  assert.equal(panel.items.value.length, 0)
})
