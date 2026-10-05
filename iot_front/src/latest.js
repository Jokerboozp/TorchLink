// 只采用最后一次请求的结果：新请求中止上一个，过期结果以 AbortError 结束。
export function latest() {
  let controller = null
  let version = 0
  return {
    async run(task) {
      controller?.abort()
      controller = new AbortController()
      const current = ++version
      const stale = () => Object.assign(new Error('stale'), { name: 'AbortError' })
      let result
      try {
        result = await task(controller.signal)
      } catch (error) {
        // A failure of a request that a newer one replaced is stale too.
        throw current === version ? error : stale()
      }
      if (current !== version) throw stale()
      return result
    },
    cancel() {
      controller?.abort()
      version++
    }
  }
}

export const isAbort = error => error?.name === 'AbortError'
