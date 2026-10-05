import { ref } from 'vue'
import { latest } from '../latest.js'

// 列表与详情读取只采用最后一次请求的结果：新请求会中止上一个，过期的结果以
// AbortError 结束（调用方用 isAbort 忽略），加载状态只由最后一次请求结束。
// silent 用于后台定时刷新：不打开加载状态，但同样取代之前的请求。
export function useListLoader(loading = ref(false)) {
  const request = latest()
  let active = 0
  async function run(task, { silent = false } = {}) {
    const id = ++active
    if (!silent) loading.value = true
    try {
      return await request.run(task)
    } finally {
      if (id === active) loading.value = false
    }
  }
  function cancel() {
    active++
    loading.value = false
    request.cancel()
  }
  return { loading, run, cancel }
}
