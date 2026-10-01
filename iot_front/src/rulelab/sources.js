// Query only the fixed experiment input, in bounded URLs, and merge shared rule revisions.
export async function readRuleSources(readAll, deviceIds, options = {}) {
  const ids = [...new Set(deviceIds.filter(Boolean))].sort()
  if (ids.length > 1000) throw new Error('单次规则实验最多选择 1000 台设备，请缩小固定数据集范围')
  const items = new Map()
  // Older servers omit maxRecords; preserve the service's default MaxRecords boundary.
  let maximum = 50000
  for (let offset = 0; offset < ids.length;) {
    options.signal?.throwIfAborted()
    const batch = []
    while (offset < ids.length && batch.length < 50) {
      const next = ids[offset]
      if (batch.length && new URLSearchParams({ deviceIds: [...batch, next].join(',') }).toString().length > 4000) break
      batch.push(next); offset++
    }
    const result = await readAll('rule-sources', '', '', { deviceIds: batch.join(',') }, options)
    options.signal?.throwIfAborted()
    if (Number.isSafeInteger(result.maxRecords) && result.maxRecords > 0) maximum = Math.min(maximum, result.maxRecords)
    for (const row of result.items || []) items.set(row.id, row)
    if (items.size > maximum) throw new Error(`规则来源集合超过 ${maximum} 条读取上限，请缩小实验设备范围`)
  }
  return { items: [...items.values()] }
}
