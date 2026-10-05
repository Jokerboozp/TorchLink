// 页面地址：每个菜单对应一个路径（如 /alarms、/ops-overview），告警详情为 /alarms/<告警编号>；
// 跨页跳转携带的简单定位条件（设备、原文、产品、页签）写入查询参数，刷新和分享链接后仍能定位。
// 规则草稿等复杂对象只经会话存储传递，不进入地址。页面内的其他状态仍由各页面管理。
const kebab = name => name.replace(/[A-Z]/g, c => '-' + c.toLowerCase())
const camel = path => path.replace(/-([a-z])/g, (_, c) => c.toUpperCase())
const linkKeys = ['deviceId', 'rawMessageId', 'messageId', 'productId', 'tab']
const linkValue = value => typeof value === 'string' && value.length > 0 && value.length <= 200

export function pathFor(page, detail) {
  if (!page) return '/'
  let path = '/' + kebab(page)
  if (page === 'alarms' && linkValue(detail?.alarmId)) path += `/${encodeURIComponent(detail.alarmId)}`
  const query = new URLSearchParams()
  for (const key of linkKeys) if (linkValue(detail?.[key])) query.set(key, detail[key])
  const search = query.toString()
  return search ? `${path}?${search}` : path
}

// parsePath 返回页面名与导航细节；未知或空路径返回空页面，由调用方选择首个可用页面。
export function parsePath(pathname, pages, search = '') {
  const parts = String(pathname || '/')
    .split('/')
    .filter(Boolean)
  if (!parts.length) return { page: '', detail: null }
  let page
  try {
    page = camel(decodeURIComponent(parts[0]))
  } catch {
    return { page: '', detail: null }
  }
  if (!Object.prototype.hasOwnProperty.call(pages, page)) return { page: '', detail: null }
  const detail = {}
  if (page === 'alarms' && parts[1]) {
    try {
      detail.alarmId = decodeURIComponent(parts[1])
    } catch {
      // 损坏的告警编号按无详情处理。
    }
  }
  const query = new URLSearchParams(search)
  for (const key of linkKeys) if (linkValue(query.get(key))) detail[key] = query.get(key)
  return { page, detail: Object.keys(detail).length ? detail : null }
}
