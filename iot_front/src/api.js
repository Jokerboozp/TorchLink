import { ElMessage } from 'element-plus'

import { consumeSSE } from './sse'
import { loadAllPages } from './listPagination.js'

export const session = {
  get token() { return localStorage.getItem('iot_token') || '' },
  get tenant() { return localStorage.getItem('iot_tenant') || '' },
  get user() { return localStorage.getItem('iot_user') || '' },
  get role() { return localStorage.getItem('iot_role') || '' },
  save(data, username = '') {
    localStorage.setItem('iot_token', data.accessToken)
    localStorage.setItem('iot_tenant', data.tenantId || '')
    localStorage.setItem('iot_user', username)
    localStorage.setItem('iot_role', data.role || '')
  },
  clear() {
    for (const key of ['iot_token', 'iot_tenant', 'iot_user', 'iot_role']) localStorage.removeItem(key)
  }
}

export class ApiError extends Error {
  constructor(message, details = {}) {
    super(message)
    this.name = 'ApiError'
    this.status = details.status || 0
    this.code = details.code || details.errorCode || ''
    this.testResult = details.success === false && details.errorCode ? details : null
    this.traceId = details.traceId || ''
    this.runId = details.runId || ''
    this.stage = details.stage || ''
    this.retryable = Boolean(details.retryable)
    this.retryAfterMs = Number(details.retryAfterMs || 0)
    this.fieldErrors = details.fieldErrors || []
  }
}

function headersFor(options, accept = '') {
  const isForm = typeof FormData !== 'undefined' && options.body instanceof FormData
  const headers = { ...(!isForm && options.body != null ? { 'Content-Type':'application/json' } : {}), ...(options.headers || {}) }
  if (accept && !headers.Accept) headers.Accept = accept
  if (session.token) headers.Authorization = `Bearer ${session.token}`
  return headers
}

function dispatchUnauthorized(path, status) {
  if (status === 401 && path !== '/api/v1/auth/login' && typeof window !== 'undefined') window.dispatchEvent(new Event('iot:unauthorized'))
}

async function responseError(path, response) {
  const data = await response.json().catch(() => ({}))
  dispatchUnauthorized(path, response.status)
  return new ApiError(data.detail || data.message || `HTTP ${response.status}`, { ...data, status:response.status })
}

export async function api(path, options = {}) {
  const headers = headersFor(options)
  const request = { ...options, headers }
  // API responses are tenant-scoped and frequently change while an operator
  // is managing devices, rules, or workflow plugins. Do not let the browser
  // reuse an older GET response after a mutation followed by a refresh.
  if (request.cache == null && String(request.method || 'GET').toUpperCase() === 'GET') request.cache = 'no-store'
  const response = await fetch(path, request)
  if (!response.ok) throw await responseError(path, response)
  return response.json().catch(() => ({}))
}

export function apiAll(path, options = {}) {
  return loadAllPages(api, path, options)
}

export async function apiStream(path, options = {}, onEvent = () => {}) {
  let response
  try {
    response = await fetch(path, { ...options, headers:headersFor(options, 'text/event-stream') })
  } catch (error) {
    if (error?.name === 'AbortError') throw error
    throw new ApiError('无法连接 AI 流服务，请检查网络后重试', { code:'AI_STREAM_NETWORK_ERROR', retryable:true })
  }
  if (!response.ok) throw await responseError(path, response)
  if (!response.body) throw new ApiError('AI 流响应不可用', { status:response.status, code:'AI_STREAM_UNAVAILABLE', retryable:true })
  try {
    await consumeSSE(response.body, onEvent)
  } catch (error) {
    if (error?.name === 'AbortError' || error instanceof ApiError) throw error
    throw new ApiError(error?.message || 'AI 流解析失败', { code:error?.code || 'AI_STREAM_PARSE_ERROR', retryable:true })
  }
}

export async function download(path, filename, options = {}) {
  const headers = headersFor({ ...options, body:null })
  const response = await fetch(path, { ...options, headers })
  if (!response.ok) {
    throw await responseError(path, response)
  }
  const url = URL.createObjectURL(await response.blob())
  const anchor = document.createElement('a')
  anchor.href = url; anchor.download = filename; anchor.click()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

export function notifyError(error) { ElMessage.error(error?.message || String(error)) }
export const formatTime = value => value ? new Date(Number(value)).toLocaleString() : '—'
export const pretty = value => JSON.stringify(value, null, 2)
export function parseJSON(value, label = 'JSON') {
  try { return JSON.parse(value || '{}') } catch { throw new Error(`${label} 格式不正确`) }
}
