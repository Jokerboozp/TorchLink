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
