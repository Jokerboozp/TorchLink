import fs from 'node:fs' /* 引入当前代码需要的依赖。 */
import vm from 'node:vm' /* 引入当前代码需要的依赖。 */
import assert from 'node:assert/strict' /* 引入当前代码需要的依赖。 */
import test from 'node:test' /* 引入当前代码需要的依赖。 */
import { createRequire } from 'node:module' /* 引入当前代码需要的依赖。 */

const require = createRequire(import.meta.url) /* 声明 require。 */
const { ref, reactive, computed } = require('vue') /* 执行当前语句并推进处理流程。 */

test('alarm list batches alarm events and ignores device state events', async () => { /* 执行当前语句并推进处理流程。 */
  const source = fs.readFileSync(new URL('../src/views/AlarmsView.vue', import.meta.url), 'utf8') /* 声明 source。 */
  const script = source.match(/<script setup>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm, '') /* 声明 script。 */
  const timers = [] /* 声明 timers。 */
  let requests = 0 /* 声明 requests。 */
  const context = vm.createContext({ /* 声明 context。 */
    ref, reactive, computed, /* 执行当前语句并推进处理流程。 */
    defineEmits: () => () => {}, onMounted() {}, onBeforeUnmount() {}, /* 执行当前语句并推进处理流程。 */
    api: async () => { requests++; return { items: [], total: 0 } }, /* 执行当前语句并推进处理流程。 */
    alarmQuery: () => '', notifyError: error => { throw error }, /* 执行当前语句并推进处理流程。 */
    window: { setTimeout: callback => { timers.push(callback); return timers.length }, clearTimeout() {} } /* 执行当前语句并推进处理流程。 */
  }) /* 结束当前表达式或代码块。 */
  const { realtime } = vm.runInContext(script + '\n;({realtime})', context) /* 执行当前语句并推进处理流程。 */
  realtime({ detail: { topic: '/iot/device/state/tenant/product/device' } }) /* 执行当前语句并推进处理流程。 */
  assert.equal(timers.length, 0) /* 验证实际结果符合预期。 */
  realtime({ detail: { topic: '/iot/alarm/raised/tenant/product/device' } }) /* 执行当前语句并推进处理流程。 */
  realtime({ detail: { topic: '/iot/alarm/recovered/tenant/product/device' } }) /* 执行当前语句并推进处理流程。 */
  assert.equal(timers.length, 1) /* 验证实际结果符合预期。 */
  timers[0]() /* 执行当前语句并推进处理流程。 */
  await new Promise(resolve => setImmediate(resolve)) /* 等待异步操作完成。 */
  assert.equal(requests, 1) /* 验证实际结果符合预期。 */
}) /* 结束当前表达式或代码块。 */
