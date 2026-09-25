import { errorMessage } from './presentation' /* 引入当前代码需要的依赖。 */
import { UiMessage } from './ui/feedback.js' /* 引入当前代码需要的依赖。 */

import { consumeSSE } from './sse' /* 引入当前代码需要的依赖。 */
import { loadAllPages } from './listPagination.js' /* 引入当前代码需要的依赖。 */

export const session = { /* 执行当前语句并推进处理流程。 */
  get token() { return localStorage.getItem('iot_token') || '' }, /* 执行当前语句并推进处理流程。 */
  get tenant() { return localStorage.getItem('iot_tenant') || '' }, /* 执行当前语句并推进处理流程。 */
  get user() { return localStorage.getItem('iot_user') || '' }, /* 执行当前语句并推进处理流程。 */
  get accessVersion() { return localStorage.getItem('iot_access_version') || '' },
  get role() { return localStorage.getItem('iot_role') || '' }, /* 执行当前语句并推进处理流程。 */
  save(data, username = '') { /* 执行当前语句并推进处理流程。 */
    localStorage.setItem('iot_access_version', data.accessVersion || '')
    localStorage.setItem('iot_token', data.accessToken) /* 执行当前语句并推进处理流程。 */
    localStorage.setItem('iot_tenant', data.tenantId || '') /* 执行当前语句并推进处理流程。 */
    localStorage.setItem('iot_user', username) /* 执行当前语句并推进处理流程。 */
    localStorage.setItem('iot_role', data.role || '') /* 执行当前语句并推进处理流程。 */
    localStorage.setItem('iot_permissions', JSON.stringify(data.permissions || (data.role === 'admin' ? ['*'] : []))) /* 执行当前语句并推进处理流程。 */
  }, /* 结束当前表达式或代码块。 */
  clear() { /* 执行当前语句并推进处理流程。 */
    for (const key of ['iot_token', 'iot_tenant', 'iot_user', 'iot_role', 'iot_permissions', 'iot_access_version']) localStorage.removeItem(key) /* 循环处理当前数据。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

export class ApiError extends Error { /* 执行当前语句并推进处理流程。 */
  constructor(message, details = {}) { /* 执行当前语句并推进处理流程。 */
    super(message) /* 执行当前语句并推进处理流程。 */
    this.name = 'ApiError' /* 更新 this.name 的值。 */
    this.status = details.status || 0 /* 更新 this.status 的值。 */
    this.code = details.code || details.errorCode || '' /* 更新 this.code 的值。 */
    this.testResult = details.success === false && details.errorCode ? details : null /* 更新 this.testResult 的值。 */
    this.originalMessage = details.detail || details.message || '' /* 更新 this.originalMessage 的值。 */
    this.traceId = details.traceId || '' /* 更新 this.traceId 的值。 */
    this.runId = details.runId || '' /* 更新 this.runId 的值。 */
    this.stage = details.stage || '' /* 更新 this.stage 的值。 */
    this.retryable = Boolean(details.retryable) /* 更新 this.retryable 的值。 */
    this.retryAfterMs = Number(details.retryAfterMs || 0) /* 更新 this.retryAfterMs 的值。 */
    this.fieldErrors = details.fieldErrors || [] /* 更新 this.fieldErrors 的值。 */
    this.details = details // 保留完整错误体，例如运维中心的 reason、rolledBack、field。
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

function headersFor(options, accept = '') { /* 定义 headersFor 函数。 */
  const isForm = typeof FormData !== 'undefined' && options.body instanceof FormData /* 声明 isForm。 */
  const headers = { ...(!isForm && options.body != null ? { 'Content-Type':'application/json' } : {}), ...(options.headers || {}) } /* 声明 headers。 */
  if (accept && !headers.Accept) headers.Accept = accept /* 判断条件并选择处理分支。 */
  if (session.token) headers.Authorization = `Bearer ${session.token}` /* 判断条件并选择处理分支。 */
  return headers /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

function dispatchUnauthorized(path, status) { /* 定义 dispatchUnauthorized 函数。 */
  if (status === 401 && path !== '/api/v1/auth/login' && typeof window !== 'undefined') window.dispatchEvent(new Event('iot:unauthorized')) /* 判断条件并选择处理分支。 */
} /* 结束当前表达式或代码块。 */

async function responseError(path, response) { /* 定义 responseError 函数。 */
  const data = await response.json().catch(() => ({})) /* 声明 data。 */
  dispatchUnauthorized(path, response.status) /* 执行当前语句并推进处理流程。 */
  return new ApiError(errorMessage({ message:data.detail || data.message || '', status:response.status }), { ...data, status:response.status }) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

export async function api(path, options = {}) { /* 执行当前语句并推进处理流程。 */
  const headers = headersFor(options) /* 声明 headers。 */
  const request = { ...options, headers } /* 声明 request。 */
  // API responses are tenant-scoped and frequently change while an operator
  // is managing devices, rules, or workflow plugins. Do not let the browser
  // reuse an older GET response after a mutation followed by a refresh.
  if (request.cache == null && String(request.method || 'GET').toUpperCase() === 'GET') request.cache = 'no-store' /* 判断条件并选择处理分支。 */
  const response = await fetch(path, request) /* 声明 response。 */
  if (!response.ok) throw await responseError(path, response) /* 判断条件并选择处理分支。 */
  return response.json().catch(() => ({})) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

export function apiAll(path, options = {}) { /* 执行当前语句并推进处理流程。 */
  return loadAllPages(api, path, options) /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

export async function apiStream(path, options = {}, onEvent = () => {}) { /* 执行当前语句并推进处理流程。 */
  let response /* 声明 response。 */
  try { /* 执行当前语句并推进处理流程。 */
    response = await fetch(path, { ...options, headers:headersFor(options, 'text/event-stream') }) /* 更新 response 的值。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    if (error?.name === 'AbortError') throw error /* 判断条件并选择处理分支。 */
    throw new ApiError('无法连接智能流服务，请检查网络后重试', { code:'AI_STREAM_NETWORK_ERROR', retryable:true }) /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  if (!response.ok) throw await responseError(path, response) /* 判断条件并选择处理分支。 */
  if (!response.body) throw new ApiError('智能流响应不可用', { status:response.status, code:'AI_STREAM_UNAVAILABLE', retryable:true }) /* 判断条件并选择处理分支。 */
  try { /* 执行当前语句并推进处理流程。 */
    await consumeSSE(response.body, onEvent) /* 等待异步操作完成。 */
  } catch (error) { /* 结束当前表达式或代码块。 */
    if (error?.name === 'AbortError' || error instanceof ApiError) throw error /* 判断条件并选择处理分支。 */
    throw new ApiError(error?.message || '智能流解析失败', { code:error?.code || 'AI_STREAM_PARSE_ERROR', retryable:true }) /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

export async function download(path, filename, options = {}) { /* 执行当前语句并推进处理流程。 */
  const headers = headersFor({ ...options, body:null }) /* 声明 headers。 */
  const response = await fetch(path, { ...options, headers }) /* 声明 response。 */
  if (!response.ok) { /* 判断条件并选择处理分支。 */
    throw await responseError(path, response) /* 抛出当前错误。 */
  } /* 结束当前表达式或代码块。 */
  const url = URL.createObjectURL(await response.blob()) /* 声明 url。 */
  const anchor = document.createElement('a') /* 声明 anchor。 */
  anchor.href = url; anchor.download = filename; anchor.click() /* 更新 anchor.href 的值。 */
  setTimeout(() => URL.revokeObjectURL(url), 1000) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

export function notifyError(error) { UiMessage.error(errorMessage(error)) } /* 执行当前语句并推进处理流程。 */
export const formatTime = value => value ? new Date(Number(value)).toLocaleString('zh-CN', { hour12:false }) : '—' /* 执行当前语句并推进处理流程。 */
export const pretty = value => JSON.stringify(value, null, 2) /* 执行当前语句并推进处理流程。 */
export function parseJSON(value, label = '结构化数据') { /* 执行当前语句并推进处理流程。 */
  try { return JSON.parse(value || '{}') } catch { throw new Error(`${label} 格式不正确`) } /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
