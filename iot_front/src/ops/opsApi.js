// 运维中心接口封装：所有请求都走平台 API，浏览器不接触组件地址与凭据。
import { api, ApiError, session } from '../api'
import { consumeSSE } from '../sse'

export function withQuery(path, params = {}) {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value == null || value === '') continue
    query.set(key, typeof value === 'object' ? JSON.stringify(value) : String(value))
  }
  const text = query.toString()
  return text ? `${path}?${text}` : path
}

export const opsGet = (path, params, signal) => api(withQuery(path, params), { signal })
export const opsSend = (method, path, body, signal) => api(path, { method, body: body === undefined ? undefined : JSON.stringify(body), signal })

export const isAbort = error => error?.name === 'AbortError'

// latest 为同一数据块只保留最新请求：发起新请求时取消旧请求，旧结果不会覆盖新结果。
export function latest() {
  let controller = null
  let version = 0
  return {
    async run(task) {
      controller?.abort()
      controller = new AbortController()
      const current = ++version
      const result = await task(controller.signal)
      if (current !== version) throw Object.assign(new Error('stale'), { name: 'AbortError' })
      return result
    },
    cancel() { controller?.abort(); version++ }
  }
}

// ops 错误码对应的中文说明；后端 detail 已是中文时直接显示。
export function opsErrorText(error) {
  if (!error) return ''
  if (isAbort(error)) return '查询已取消'
  const detail = error.originalMessage || error.message || ''
  const byCode = {
    OPS_NOT_CONFIGURED: '该功能依赖的组件未配置',
    OPS_UPSTREAM_UNAVAILABLE: '无法连接组件，请检查组件是否运行',
    OPS_UPSTREAM_TIMEOUT: '组件响应超时，请缩小时间范围或简化查询',
    OPS_UPSTREAM_AUTH: '组件拒绝了平台凭据，请检查运维中心配置'
  }
  if (error.status === 403) return '当前账户没有该操作权限'
  return detail || byCode[error.code] || '请求失败'
}

// tailLogs 通过平台 SSE 接收实时日志，signal 取消时关闭连接。
export async function tailLogs(params, onEvent, signal) {
  const response = await fetch(withQuery('/api/v1/ops/logs/tail', params), { headers: { Accept: 'text/event-stream', Authorization: `Bearer ${session.token}` }, signal, cache: 'no-store' })
  if (!response.ok) {
    const data = await response.json().catch(() => ({}))
    if (response.status === 401) window.dispatchEvent(new Event('iot:unauthorized'))
    throw new ApiError(data.detail || '实时日志连接失败', { ...data, status: response.status })
  }
  await consumeSSE(response.body, onEvent)
}

// exportLogs 下载有限量导出文件，并返回实际行数与是否截断。
export async function exportLogs(body) {
  const response = await fetch('/api/v1/ops/logs/export', { method: 'POST', headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${session.token}` }, body: JSON.stringify(body) })
  if (!response.ok) {
    const data = await response.json().catch(() => ({}))
    throw new ApiError(data.detail || '导出失败', { ...data, status: response.status })
  }
  const disposition = response.headers.get('Content-Disposition') || ''
  const filename = disposition.match(/filename="([^"]+)"/)?.[1] || 'logs.jsonl'
  const blob = await response.blob()
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url; anchor.download = filename; anchor.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
  return { filename, lines: Number(response.headers.get('X-Export-Lines') || 0), truncated: response.headers.get('X-Export-Truncated') === 'true' }
}

export function downloadJSON(data, filename) {
  const url = URL.createObjectURL(new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' }))
  const anchor = document.createElement('a')
  anchor.href = url; anchor.download = filename; anchor.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

export async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    const area = document.createElement('textarea')
    area.value = text
    document.body.appendChild(area)
    area.select()
    const ok = document.execCommand('copy')
    area.remove()
    return ok
  }
}

// 跨页面跳转携带的查询条件，沿用平台 iot:navigation-detail 约定。
export function takeNavigation() {
  try {
    const detail = JSON.parse(sessionStorage.getItem('iot:navigation-detail') || 'null')
    sessionStorage.removeItem('iot:navigation-detail')
    return detail && typeof detail === 'object' ? detail : null
  } catch { return null }
}
