import { watch } from 'vue'

// 列表页的筛选、分页、查询语句和时间范围：写入地址栏查询参数（刷新、前进后退和分享后仍在），
// 同时按租户与用户保存在会话存储（切换菜单再回来时恢复）。地址栏中的值优先于会话存储。
// 只保存字符串、数字、布尔、数组与普通对象，不保存密钥等敏感字段。
const prefix = 's.'
// 身份取自登录时保存的 iot_tenant / iot_user（见 api.js session），不同用户互不读取。
function storageKey(page) {
  let tenant = '',
    user = ''
  try {
    tenant = localStorage.getItem('iot_tenant') || ''
    user = localStorage.getItem('iot_user') || ''
  } catch {
    // 存储不可用时退回空身份，写入同样会失败。
  }
  return `iot:page-state:${tenant}:${user}:${page}`
}
const MAX_URL_VALUE = 500

function readStored(page) {
  try {
    return JSON.parse(sessionStorage.getItem(storageKey(page)) || '{}') || {}
  } catch {
    return {}
  }
}

function writeStored(page, value) {
  try {
    sessionStorage.setItem(storageKey(page), JSON.stringify(value))
  } catch {
    // 存储不可用时只保留地址栏。
  }
}

function decode(raw, sample) {
  if (raw == null) return undefined
  if (typeof sample === 'string') return raw
  try {
    const value = JSON.parse(raw)
    if (typeof sample === 'number') return Number.isFinite(value) ? value : undefined
    if (typeof sample === 'boolean') return typeof value === 'boolean' ? value : undefined
    if (Array.isArray(sample)) return Array.isArray(value) ? value : undefined
    if (sample && typeof sample === 'object') return value && typeof value === 'object' && !Array.isArray(value) ? value : undefined
    return value
  } catch {
    return undefined
  }
}

// 值可能是响应式对象，按 JSON 复制一份作为默认值。
const clone = value => (value === undefined ? undefined : JSON.parse(JSON.stringify(value)))
const encode = value => (typeof value === 'string' ? value : JSON.stringify(value))
const same = (a, b) => JSON.stringify(a) === JSON.stringify(b)

// usePageState(page, { keyword: ref(''), page: ref(1) })：在组件 setup 中同步调用，
// 返回 restored（是否从地址或会话恢复了任一值），调用方可据此决定首个查询。
export function usePageState(page, refs) {
  const defaults = Object.fromEntries(Object.entries(refs).map(([key, target]) => [key, clone(target.value)]))
  const stored = readStored(page)
  const query = typeof window !== 'undefined' ? new URLSearchParams(window.location.search) : new URLSearchParams()
  let restored = false
  for (const [key, target] of Object.entries(refs)) {
    const fromUrl = decode(query.get(prefix + key) ?? undefined, defaults[key])
    const value = fromUrl !== undefined ? fromUrl : stored[key]
    if (value !== undefined && !same(value, defaults[key])) {
      target.value = value
      restored = true
    }
  }

  function sync() {
    const current = Object.fromEntries(Object.entries(refs).map(([key, target]) => [key, target.value]))
    writeStored(page, current)
    if (typeof window === 'undefined') return
    const params = new URLSearchParams(window.location.search)
    for (const [key, value] of Object.entries(current)) {
      const text = encode(value)
      if (same(value, defaults[key]) || text == null || text.length > MAX_URL_VALUE) params.delete(prefix + key)
      else params.set(prefix + key, text)
    }
    const search = params.toString()
    const next = window.location.pathname + (search ? `?${search}` : '') + window.location.hash
    if (next !== window.location.pathname + window.location.search + window.location.hash)
      window.history.replaceState(window.history.state, '', next)
  }
  watch(() => Object.values(refs).map(target => target.value), sync, { deep: true, immediate: true })
  return { restored }
}
