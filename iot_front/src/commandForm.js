export function commandBody(operation, values) { /* 执行当前语句并推进处理流程。 */
  if (!operation?.identifier) throw new Error('请选择设备支持的命令') /* 判断条件并选择处理分支。 */
  const entries = [] /* 声明 entries。 */
  for (const field of operation.fields || []) { /* 循环处理当前数据。 */
    const value = values[field.identifier] /* 声明 value。 */
    if (value == null || value === '') { /* 判断条件并选择处理分支。 */
      if (field.required) throw new Error(`请填写${field.name || field.identifier}`) /* 判断条件并选择处理分支。 */
      continue /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
    const valid = { string: () => typeof value === 'string', boolean: () => typeof value === 'boolean', /* 声明 valid。 */
      number: () => Number.isFinite(value), integer: () => Number.isSafeInteger(value), /* 执行当前语句并推进处理流程。 */
      object: () => typeof value === 'object' && !Array.isArray(value), array: () => Array.isArray(value) } /* 执行当前语句并推进处理流程。 */
    if (!valid[field.dataType]?.()) throw new Error(`${field.name || field.identifier}的参数类型不正确`) /* 判断条件并选择处理分支。 */
    entries.push([field.identifier, value]) /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
  return { type: operation.identifier, data: Object.fromEntries(entries) } /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
