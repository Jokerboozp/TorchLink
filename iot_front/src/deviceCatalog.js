// A view-local cache of devices actually read or selected; never load the full registry.
export function deviceChoices(ids, rows) {
  return [...new Set(ids.filter(Boolean))].map(id => rows.find(row => row.id === id) || { id })
}
export function createDeviceCatalog(request, rows) {
  let controller = new AbortController(), version = 0
  const pending = new Map()
  function remember(items) {
    const known = new Map(rows.value.map(row => [row.id, row]))
    for (const item of items) {
      const row = item?.device ? { ...item.device, productName: item.product?.name || item.productName } : item
      if (row?.id) known.set(row.id, row)
    }
    rows.value = [...known.values()]
  }
  async function ensure(ids) {
    const token = version, signal = controller.signal
    const missing = [...new Set(ids.filter(Boolean))].filter(id => !rows.value.some(row => row.id === id))
    let cursor = 0
    // Restored multi-device selections resolve in bounded batches.
    await Promise.all(Array.from({ length: Math.min(6, missing.length) }, async () => {
      while (cursor < missing.length && token === version && !signal.aborted) {
        const id = missing[cursor++]
        if (!pending.has(id)) {
          pending.set(id, request(`/api/v1/device-registry/${encodeURIComponent(id)}/connection`, { signal })
            .then(value => { if (token === version && !signal.aborted) remember([value.device]) })
            .catch(cause => { if (token === version && !signal.aborted && cause.name !== 'AbortError' && ![403, 404].includes(cause.status)) throw cause })
            .finally(() => { if (token === version) pending.delete(id) }))
        }
        await pending.get(id)
      }
    }))
  }
  function clear() { version++; controller.abort(); controller = new AbortController(); pending.clear(); rows.value = [] }
  return { rows, remember, ensure, clear }
}
