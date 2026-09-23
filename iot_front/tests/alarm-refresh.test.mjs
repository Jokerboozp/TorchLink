import fs from 'node:fs'
import vm from 'node:vm'
import assert from 'node:assert/strict'
import test from 'node:test'
import { createRequire } from 'node:module'

const require = createRequire(import.meta.url)
const { ref, reactive, computed } = require('vue')

test('alarm list batches alarm events and ignores device state events', async () => {
  const source = fs.readFileSync(new URL('../src/views/AlarmsView.vue', import.meta.url), 'utf8')
  const script = source.match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm, '')
  const timers = []
  let requests = 0
  const context = vm.createContext({
    ref, reactive, computed,
    defineEmits: () => () => {}, onMounted() {}, onBeforeUnmount() {},
    api: async () => { requests++; return { items: [], total: 0 } },
    alarmQuery: () => '', notifyError: error => { throw error },
    window: { setTimeout: callback => { timers.push(callback); return timers.length }, clearTimeout() {} }
  })
  const { realtime } = vm.runInContext(script + '\n;({realtime})', context)
  realtime({ detail: { topic: '/iot/device/state/tenant/product/device' } })
  assert.equal(timers.length, 0)
  realtime({ detail: { topic: '/iot/alarm/raised/tenant/product/device' } })
  realtime({ detail: { topic: '/iot/alarm/recovered/tenant/product/device' } })
  assert.equal(timers.length, 1)
  timers[0]()
  await new Promise(resolve => setImmediate(resolve))
  assert.equal(requests, 1)
})
