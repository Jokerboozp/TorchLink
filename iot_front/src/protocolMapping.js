// Keep the original mapping specification so optional/vendor settings survive edits.
export function mappingRows(config, parserType) {
  if (parserType === 'configurable_json_parser') {
    return Object.entries(config.properties || config.propertyMappings || {}).map(([name, spec]) => ({
      name, path: typeof spec === 'string' ? spec : spec.path,
      type: typeof spec === 'string' ? '' : spec.type || '',
      scale: typeof spec === 'string' ? 1 : spec.scale ?? 1,
      original: JSON.parse(JSON.stringify(spec)), source: `${name} → ${typeof spec === 'string' ? spec : spec.path}`,
    }))
  }
  return (config.points || config.fields || []).map(point => ({ ...JSON.parse(JSON.stringify(point)), scale: point.scale ?? 1, source: config.points
    ? `${point.name || point.identifier} · 地址 ${point.address} · FC ${point.functionCode}`
    : `${point.name} · 偏移 ${point.offset} · ${point.length} 字节` }))
}

export function mappingConfig(config, parserType, rows) {
  const next = JSON.parse(JSON.stringify(config))
  const json = parserType === 'configurable_json_parser'
  const modbus = parserType.startsWith('modbus_')
  if (!rows.length) throw new Error('请至少保留一个字段')
  const names = new Set()
  const mapped = rows.map((row, index) => {
    const name = String(modbus ? row.identifier || '' : row.name || '').trim()
    if (!name) throw new Error(`第 ${index + 1} 行请填写字段标识`)
    if (names.has(name)) throw new Error(`字段标识「${name}」重复`)
    names.add(name)
    if (json) {
      if (!row.path?.trim()) throw new Error(`字段「${name}」请填写 JSON 路径`)
      if (!Number.isFinite(row.scale)) throw new Error(`字段「${name}」请填写倍率`)
      const spec = typeof row.original === 'object' && row.original !== null ? row.original : {}
      return [name, typeof row.original === 'string' && !row.type && row.scale === 1 ? row.path.trim()
        : { ...spec, path: row.path.trim(), type: row.type || '', scale: row.scale }]
    }
    const { source, ...point } = row
    if (!Number.isFinite(point.scale)) throw new Error(`字段「${name}」请填写倍率`)
    for (const key of modbus ? ['address', 'registerCount', 'functionCode'] : ['offset', 'length']) {
      if (!Number.isInteger(point[key]) || point[key] < (['length', 'registerCount', 'functionCode'].includes(key) ? 1 : 0)) {
        throw new Error(`字段「${name}」的地址、偏移或长度无效`)
      }
    }
    return { ...point, ...(modbus ? { name: point.name?.trim() || name } : {}), [modbus ? 'identifier' : 'name']: name }
  })
  if (json) next[Object.hasOwn(next, 'properties') ? 'properties' : Object.hasOwn(next, 'propertyMappings') ? 'propertyMappings' : 'properties'] = Object.fromEntries(mapped)
  else {
    next[modbus ? 'points' : 'fields'] = mapped
  }
  return next
}
