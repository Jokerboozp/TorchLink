import { computed, watch } from 'vue'

// Association selectors need the complete catalog, independently of table pages.
// The first page gives the total; the remaining pages load a few at a time and
// keep their order. At most MAX_CATALOG_PAGES pages are read; a larger
// catalog returns truncated: true so the caller can say the list is partial.
const CATALOG_PAGE_SIZE = 100
const MAX_CATALOG_PAGES = 50
const CATALOG_CONCURRENCY = 4
export async function loadAllPages(request, path, options = {}) {
  const [pathname, search = ''] = path.split('?')
  const query = new URLSearchParams(search)
  query.delete('offset')
  query.delete('limit')
  query.set('pageSize', String(CATALOG_PAGE_SIZE))
  const fetchPage = page => {
    const params = new URLSearchParams(query)
    params.set('page', String(page))
    return request(`${pathname}?${params}`, options)
  }
  const first = await fetchPage(1)
  const items = [...(first.items || [])]
  if (first.total != null) {
    const total = Number(first.total)
    const pages = Math.min(Math.ceil(total / CATALOG_PAGE_SIZE), MAX_CATALOG_PAGES)
    const batches = []
    let next = 2
    const worker = async () => {
      while (next <= pages) {
        const page = next++
        batches[page - 2] = (await fetchPage(page)).items || []
      }
    }
    await Promise.all(Array.from({ length: Math.max(0, Math.min(CATALOG_CONCURRENCY, pages - 1)) }, worker))
    for (const batch of batches) items.push(...batch)
    return { ...first, items, total: items.length, truncated: total > items.length }
  }
  // Without a total, read page after page until a short page.
  let batch = first.items || []
  for (let page = 2; batch.length === CATALOG_PAGE_SIZE && page <= MAX_CATALOG_PAGES; page += 1) {
    batch = (await fetchPage(page)).items || []
    items.push(...batch)
  }
  return { ...first, items, total: items.length, truncated: batch.length === CATALOG_PAGE_SIZE }
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
