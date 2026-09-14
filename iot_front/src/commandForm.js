export function commandBody(operation, values) {
  if (!operation?.identifier) throw new Error('请选择设备支持的命令')
  const entries = []
  for (const field of operation.fields || []) {
    const value = values[field.identifier]
    if (value == null || value === '') {
      if (field.required) throw new Error(`请填写${field.name || field.identifier}`)
      continue
    }
    const valid = { string: () => typeof value === 'string', boolean: () => typeof value === 'boolean',
      number: () => Number.isFinite(value), integer: () => Number.isSafeInteger(value),
      object: () => typeof value === 'object' && !Array.isArray(value), array: () => Array.isArray(value) }
    if (!valid[field.dataType]?.()) throw new Error(`${field.name || field.identifier}的参数类型不正确`)
    entries.push([field.identifier, value])
  }
  return { type: operation.identifier, data: Object.fromEntries(entries) }
}
