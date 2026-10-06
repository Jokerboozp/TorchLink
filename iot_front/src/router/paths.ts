// 页面地址：每个菜单对应一个路径（如 /alarms、/ops-overview），告警详情为 /alarms/<告警编号>；
// 跨页跳转携带的简单定位条件（设备、原文、产品、页签）写入查询参数，刷新和分享链接后仍能定位。
// 规则草稿等复杂对象只经会话存储传递，不进入地址。页面内的其他状态仍由各页面管理（s. 参数，见 usePageState）。

/** Navigation detail handed from one page to another. */
export type Detail = Record<string, unknown>

const linkKeys = ['deviceId', 'rawMessageId', 'messageId', 'productId', 'tab'] as const
const linkValue = (value: unknown): value is string => typeof value === 'string' && value.length > 0 && value.length <= 200

export const pathOf = (page: string) => '/' + page.replace(/[A-Z]/g, c => '-' + c.toLowerCase())

/** The address of a page with the detail fields that may appear in it. */
export function locationFor(page: string, detail?: Detail | null): { path: string; query: Record<string, string> } {
  let path = pathOf(page)
  if (page === 'alarms' && linkValue(detail?.alarmId)) path += `/${encodeURIComponent(detail.alarmId)}`
  const query: Record<string, string> = {}
  for (const key of linkKeys) {
    const value = detail?.[key]
    if (linkValue(value)) query[key] = value
  }
  return { path, query }
}

/** pathFor renders locationFor as one string, as written to the address bar. */
export function pathFor(page: string, detail?: Detail | null): string {
  if (!page) return '/'
  const { path, query } = locationFor(page, detail)
  const search = new URLSearchParams(query).toString()
  return search ? `${path}?${search}` : path
}

/**
 * The detail a page receives when it is opened from its address (refresh,
 * deep link, back and forward). An address that already carries page state
 * (s. parameters) was opened before: page state wins and only the alarm
 * detail is kept.
 */
export function detailFromLocation(params: Record<string, unknown>, query: Record<string, unknown>): Detail | null {
  const detail: Detail = {}
  const alarmId = params.alarmId
  if (typeof alarmId === 'string' && alarmId) detail.alarmId = alarmId
  const pageStateSaved = Object.keys(query).some(key => key.startsWith('s.'))
  if (!pageStateSaved) {
    for (const key of linkKeys) {
      const value = query[key]
      if (linkValue(value)) detail[key] = value
    }
  }
  return Object.keys(detail).length ? detail : null
}

// 跨页跳转携带的导航细节只经会话存储传递一次：读取后立即清除。没有或格式错误时返回空对象。
export const NAVIGATION_KEY = 'iot:navigation-detail'
export function takeNavigation(): Detail {
  let detail: unknown = null
  try {
    detail = JSON.parse(sessionStorage.getItem(NAVIGATION_KEY) || 'null')
  } catch {
    // 损坏的导航细节按无处理。
  }
  try {
    sessionStorage.removeItem(NAVIGATION_KEY)
  } catch {
    // 存储不可用时没有需要清除的内容。
  }
  return detail && typeof detail === 'object' ? (detail as Detail) : {}
}
