import { errorMessage } from './presentation'
import { UiMessage } from './ui/feedback.js'

import { consumeSSE } from './sse'
import { loadAllPages } from './listPagination.js'

export const session = {
  get token() {
    return localStorage.getItem('iot_token') || ''
  },
  get tenant() {
    return localStorage.getItem('iot_tenant') || ''
  },
  get user() {
    return localStorage.getItem('iot_user') || ''
  },
  get accessVersion() {
    return localStorage.getItem('iot_access_version') || ''
  },
  get role() {
    return localStorage.getItem('iot_role') || ''
  },
  save(data, username = '') {
    localStorage.setItem('iot_access_version', data.accessVersion || '')
    localStorage.setItem('iot_token', data.accessToken)
    localStorage.setItem('iot_tenant', data.tenantId || '')
    localStorage.setItem('iot_user', username)
    localStorage.setItem('iot_role', data.role || '')
    localStorage.setItem('iot_permissions', JSON.stringify(data.permissions || (data.role === 'admin' ? ['*'] : [])))
  },
  clear() {
    for (const key of ['iot_token', 'iot_tenant', 'iot_user', 'iot_role', 'iot_permissions', 'iot_access_version'])
      localStorage.removeItem(key)
  }
}

export class ApiError extends Error {
  constructor(message, details = {}) {
    super(message)
    this.name = 'ApiError'
    this.status = details.status || 0
    this.code = details.code || details.errorCode || ''
    this.testResult = details.success === false && details.errorCode ? details : null
    this.originalMessage = details.detail || details.message || ''
    this.traceId = details.traceId || ''
    this.runId = details.runId || ''
    this.stage = details.stage || ''
    this.retryable = Boolean(details.retryable)
    this.retryAfterMs = Number(details.retryAfterMs || 0)
    this.fieldErrors = details.fieldErrors || []
    this.details = details // 保留完整错误体，例如运维中心的 reason、rolledBack、field。
  }
}

function headersFor(options, accept = '') {
  const isForm = typeof FormData !== 'undefined' && options.body instanceof FormData
  const headers = { ...(!isForm && options.body != null ? { 'Content-Type': 'application/json' } : {}), ...(options.headers || {}) }
  if (accept && !headers.Accept) headers.Accept = accept
  if (session.token && !headers.Authorization) headers.Authorization = `Bearer ${session.token}`
  return headers
}

function dispatchUnauthorized(path, status) {
  if (status === 401 && path !== '/api/v1/auth/login' && typeof window !== 'undefined') window.dispatchEvent(new Event('iot:unauthorized'))
}

async function responseError(path, response) {
  const data = await response.json().catch(() => ({}))
  dispatchUnauthorized(path, response.status)
  return new ApiError(errorMessage({ message: data.detail || data.message || '', status: response.status }), {
    ...data,
    status: response.status
  })
}

// 读取请求默认 60 秒超时，避免列表因挂起的连接一直处于加载中；写操作可能
// 触发编译、报告等长任务，只有调用方传入 timeout 时才设上限。
const DEFAULT_READ_TIMEOUT_MS = 60000

function withTimeout(signal, timeout) {
  if (!timeout || typeof AbortSignal?.timeout !== 'function') return signal
  const limit = AbortSignal.timeout(timeout)
  if (!signal) return limit
  return typeof AbortSignal.any === 'function' ? AbortSignal.any([signal, limit]) : signal
}

async function send(path, init) {
  try {
    return await fetch(path, init)
  } catch (error) {
    if (error?.name === 'TimeoutError') throw new ApiError('请求超时，请稍后重试', { code: 'REQUEST_TIMEOUT', retryable: true })
    throw error
  }
}

export async function api(path, options = {}) {
  const { timeout, ...rest } = options
  const read = String(rest.method || 'GET').toUpperCase() === 'GET'
  const request = { ...rest, headers: headersFor(rest), signal: withTimeout(rest.signal, timeout ?? (read ? DEFAULT_READ_TIMEOUT_MS : 0)) }
  // API responses are tenant-scoped and frequently change while an operator
  // is managing devices, rules, or workflow plugins. Do not let the browser
  // reuse an older GET response after a mutation followed by a refresh.
  if (request.cache == null && read) request.cache = 'no-store'
  const response = await send(path, request)
  if (!response.ok) throw await responseError(path, response)
  return response.json().catch(() => ({}))
}

// latest 为同一数据块只保留最新请求：发起新请求时取消旧请求，旧结果不会覆盖新结果。
export { isAbort, latest } from './latest.js'

// 浏览器下载由接口返回的文件或本地生成的内容。
export function saveBlob(blob, filename) {
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  anchor.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

// 带登录凭据的原始请求：供 SSE 与需要读取响应头的下载使用，错误处理与 api() 一致。
export async function apiResponse(path, options = {}, accept = '') {
  const response = await send(path, { cache: 'no-store', ...options, headers: headersFor(options, accept) })
  if (!response.ok) throw await responseError(path, response)
  return response
}

// Protected attachments use the same session/error handling as JSON requests.
// Callers own object URL lifetime and only request attachments after user action.
export async function apiBlob(path, options = {}) {
  const response = await send(path, { cache: 'no-store', ...options, headers: headersFor(options) })
  if (!response.ok) throw await responseError(path, response)
  return response.blob()
}

// Conditional GET for polled views: an unchanged response is 304 without a body.
export async function apiIfChanged(path, etag = '') {
  const response = await send(path, {
    cache: 'no-store',
    headers: headersFor({ headers: etag ? { 'If-None-Match': etag } : {} }),
    signal: withTimeout(undefined, 20000)
  })
  if (response.status === 304) return { changed: false, etag }
  if (!response.ok) throw await responseError(path, response)
  return { changed: true, etag: response.headers.get('ETag') || '', data: await response.json().catch(() => ({})) }
}

export function apiAll(path, options = {}) {
  return loadAllPages(api, path, options)
}

export async function apiStream(path, options = {}, onEvent = () => {}) {
  let response
  try {
    response = await send(path, { ...options, headers: headersFor(options, 'text/event-stream') })
  } catch (error) {
    if (error?.name === 'AbortError' || error instanceof ApiError) throw error
    throw new ApiError('无法连接智能流服务，请检查网络后重试', { code: 'AI_STREAM_NETWORK_ERROR', retryable: true })
  }
  if (!response.ok) throw await responseError(path, response)
  if (!response.body) throw new ApiError('智能流响应不可用', { status: response.status, code: 'AI_STREAM_UNAVAILABLE', retryable: true })
  try {
    await consumeSSE(response.body, onEvent)
  } catch (error) {
    if (error?.name === 'AbortError' || error instanceof ApiError) throw error
    throw new ApiError(error?.message || '智能流解析失败', { code: error?.code || 'AI_STREAM_PARSE_ERROR', retryable: true })
  }
}

export async function download(path, filename, options = {}) {
  const response = await send(path, { cache: 'no-store', ...options, headers: headersFor({ ...options, body: null }) })
  if (!response.ok) throw await responseError(path, response)
  saveBlob(await response.blob(), filename)
}

export function notifyError(error) {
  UiMessage.error(errorMessage(error))
}
export const formatTime = value => (value ? new Date(Number(value)).toLocaleString('zh-CN', { hour12: false }) : '—')
export const pretty = value => JSON.stringify(value, null, 2)
export function parseJSON(value, label = '结构化数据') {
  try {
    return JSON.parse(value || '{}')
  } catch {
    throw new Error(`${label} 格式不正确`)
  }
}
