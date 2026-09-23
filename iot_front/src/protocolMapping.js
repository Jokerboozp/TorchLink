// Keep the original mapping specification so optional/vendor settings survive edits.
export function mappingRows(config, parserType) { /* 执行当前语句并推进处理流程。 */
  if (parserType === 'configurable_json_parser') { /* 判断条件并选择处理分支。 */
    return Object.entries(config.properties || config.propertyMappings || {}).map(([name, spec]) => ({ /* 返回当前处理结果。 */
      name, path: typeof spec === 'string' ? spec : spec.path, /* 执行当前语句并推进处理流程。 */
      type: typeof spec === 'string' ? '' : spec.type || '', /* 执行当前语句并推进处理流程。 */
      scale: typeof spec === 'string' ? 1 : spec.scale ?? 1, /* 执行当前语句并推进处理流程。 */
      original: JSON.parse(JSON.stringify(spec)), source: `${name} → ${typeof spec === 'string' ? spec : spec.path}`, /* 执行当前语句并推进处理流程。 */
    })) /* 结束当前表达式或代码块。 */
  } /* 结束当前表达式或代码块。 */
  return (config.points || config.fields || []).map(point => ({ ...JSON.parse(JSON.stringify(point)), scale: point.scale ?? 1, source: config.points /* 返回当前处理结果。 */
    ? `${point.name || point.identifier} · 地址 ${point.address} · FC ${point.functionCode}` /* 执行当前语句并推进处理流程。 */
    : `${point.name} · 偏移 ${point.offset} · ${point.length} 字节` })) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */

export function mappingConfig(config, parserType, rows) { /* 执行当前语句并推进处理流程。 */
  const next = JSON.parse(JSON.stringify(config)) /* 声明 next。 */
  const json = parserType === 'configurable_json_parser' /* 声明 json。 */
  const modbus = parserType.startsWith('modbus_') /* 声明 modbus。 */
  if (!rows.length) throw new Error('请至少保留一个字段') /* 判断条件并选择处理分支。 */
  const names = new Set() /* 声明 names。 */
  const mapped = rows.map((row, index) => { /* 声明 mapped。 */
    const name = String(modbus ? row.identifier || '' : row.name || '').trim() /* 声明 name。 */
    if (!name) throw new Error(`第 ${index + 1} 行请填写字段标识`) /* 判断条件并选择处理分支。 */
    if (names.has(name)) throw new Error(`字段标识「${name}」重复`) /* 判断条件并选择处理分支。 */
    names.add(name) /* 执行当前语句并推进处理流程。 */
    if (json) { /* 判断条件并选择处理分支。 */
      if (!row.path?.trim()) throw new Error(`字段「${name}」请填写 JSON 路径`) /* 判断条件并选择处理分支。 */
      if (!Number.isFinite(row.scale)) throw new Error(`字段「${name}」请填写倍率`) /* 判断条件并选择处理分支。 */
      const spec = typeof row.original === 'object' && row.original !== null ? row.original : {} /* 声明 spec。 */
      return [name, typeof row.original === 'string' && !row.type && row.scale === 1 ? row.path.trim() /* 返回当前处理结果。 */
        : { ...spec, path: row.path.trim(), type: row.type || '', scale: row.scale }] /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
    const { source, ...point } = row /* 执行当前语句并推进处理流程。 */
    if (!Number.isFinite(point.scale)) throw new Error(`字段「${name}」请填写倍率`) /* 判断条件并选择处理分支。 */
    for (const key of modbus ? ['address', 'registerCount', 'functionCode'] : ['offset', 'length']) { /* 循环处理当前数据。 */
      if (!Number.isInteger(point[key]) || point[key] < (['length', 'registerCount', 'functionCode'].includes(key) ? 1 : 0)) { /* 判断条件并选择处理分支。 */
        throw new Error(`字段「${name}」的地址、偏移或长度无效`) /* 抛出当前错误。 */
      } /* 结束当前表达式或代码块。 */
    } /* 结束当前表达式或代码块。 */
    return { ...point, ...(modbus ? { name: point.name?.trim() || name } : {}), [modbus ? 'identifier' : 'name']: name } /* 返回当前处理结果。 */
  }) /* 结束当前表达式或代码块。 */
  if (json) next[Object.hasOwn(next, 'properties') ? 'properties' : Object.hasOwn(next, 'propertyMappings') ? 'propertyMappings' : 'properties'] = Object.fromEntries(mapped) /* 判断条件并选择处理分支。 */
  else { /* 执行当前语句并推进处理流程。 */
    next[modbus ? 'points' : 'fields'] = mapped /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  return next /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
