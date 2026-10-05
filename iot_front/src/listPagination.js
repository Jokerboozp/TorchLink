import { computed, watch } from 'vue'

// Association selectors need the complete catalog, independently of table pages.
export async function loadAllPages(request, path, options = {}) {
  const [pathname, search = ''] = path.split('?')
  const query = new URLSearchParams(search)
  query.delete('offset')
  query.delete('limit')
  query.set('pageSize', '100')
  const items = []
  let first
  for (let page = 1; ; page += 1) {
    query.set('page', String(page))
    const data = await request(`${pathname}?${query}`, options)
    first ??= data
    const batch = data.items || []
    items.push(...batch)
    const total = data.total ?? data.count
    if (!batch.length || (total != null ? items.length >= Number(total) : batch.length < 100)) {
      return { ...first, items, total: items.length, count: items.length }
    }
  }
}

// 服务端一次返回全部记录的列表在前端分页：返回当前页记录与总数，列表变短时把页码收回到最后一页。
export function clientPagination(items, page, pageSize) {
  const list = computed(() => (typeof items === 'function' ? items() : items.value) || [])
  const total = computed(() => list.value.length)
  const paged = computed(() => list.value.slice((page.value - 1) * pageSize.value, page.value * pageSize.value))
  watch([total, pageSize], () => {
    // 列表重新加载期间暂时为空，不据此把页码改回第一页；数据回来后再按实际条数修正。
    if (!total.value) return
    const last = Math.max(1, Math.ceil(total.value / pageSize.value))
    if (page.value > last) page.value = last
  })
  return { paged, total }
}
