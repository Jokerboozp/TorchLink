// 页面地址：每个菜单对应一个路径（如 /alarms、/ops-overview），告警详情为 /alarms/<告警编号>，
// 支持刷新、浏览器前进后退和通知消息中的深链接。只做页面级路由，页面内状态仍由各页面管理。
const kebab = name => name.replace(/[A-Z]/g, c => '-' + c.toLowerCase())
const camel = path => path.replace(/-([a-z])/g, (_, c) => c.toUpperCase())

export function pathFor(page, detail) {
  if (!page) return '/'
  if (page === 'alarms' && typeof detail?.alarmId === 'string' && detail.alarmId) return `/alarms/${encodeURIComponent(detail.alarmId)}`
  return '/' + kebab(page)
}

// parsePath 返回页面名与导航细节；未知或空路径返回空页面，由调用方选择首个可用页面。
export function parsePath(pathname, pages) {
  const parts = String(pathname || '/').split('/').filter(Boolean)
  if (!parts.length) return { page: '', detail: null }
  let page = ''
  try { page = camel(decodeURIComponent(parts[0])) } catch { return { page: '', detail: null } }
  if (!Object.prototype.hasOwnProperty.call(pages, page)) return { page: '', detail: null }
  if (page === 'alarms' && parts[1]) {
    try { return { page, detail: { alarmId: decodeURIComponent(parts[1]) } } } catch { return { page, detail: null } }
  }
  return { page, detail: null }
}
