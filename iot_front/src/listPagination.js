// Association selectors need the complete catalog, independently of table pages.
export async function loadAllPages(request, path, options = {}) {
  const { onProgress, maxItems, ...requestOptions } = options
  const [pathname, search = ''] = path.split('?')
  const query = new URLSearchParams(search)
  query.delete('offset')
  query.delete('limit')
  query.set('pageSize', '100')
  const items = []
  let first
  for (let page = 1; ; page += 1) {
    options.signal?.throwIfAborted()
    query.set('page', String(page))
    const data = await request(`${pathname}?${query}`, requestOptions)
    options.signal?.throwIfAborted()
    first ??= data
    const batch = data.items || []
    const total = data.total ?? data.count
    if (maxItems != null && (Number(total) > maxItems || items.length + batch.length > maxItems)) throw new Error(`所选范围超过 ${maxItems} 台设备，请选择更小产品或指定设备`)
    items.push(...batch)
    onProgress?.(items.length, total)
    if (!batch.length || (total != null ? items.length >= Number(total) : batch.length < 100)) {
      return { ...first, items, total: items.length, count: items.length }
    }
  }
}
