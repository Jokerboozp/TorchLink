export const topicDirections = [
  { value: 'outbound', label: '对外发布' },
  { value: 'inbound', label: '设备接入' },
  { value: 'internal', label: '内部消息' }
]

export function topicDirectionLabel(value) {
  return topicDirections.find(item => item.value === value)?.label || value
}

export function filterMessageTopics(items, { protocol = '', direction = '', keyword = '' } = {}) {
  const query = keyword.trim().toLowerCase()
  return items.filter(
    item =>
      (!protocol || item.protocol === protocol) &&
      (!direction || item.direction === direction) &&
      (!query ||
        [item.name, item.topic, item.description, item.id].some(value =>
          String(value || '')
            .toLowerCase()
            .includes(query)
        ))
  )
}

export function topicStatus(item) {
  if (!item.enabled) return { tone: 'neutral', label: '已停用' }
  if (!item.effectiveEnabled) return { tone: 'warning', label: '通道未启用' }
  return { tone: 'success', label: '已启用' }
}

export function validateSharedTopic(form, prefixes, editing = false) {
  if (!String(form.name || '').trim()) return '请填写主题名称'
  if (editing) return ''
  if (!['mqtt', 'kafka'].includes(form.protocol)) return '请选择消息协议'
  const value = String(form.topic || '').trim(),
    prefix = prefixes?.[form.protocol]
  if (!value) return '请填写主题地址或后缀'
  if (!prefix) return '未取得当前租户的主题前缀，请刷新后重试'
  if (form.protocol === 'mqtt') {
    if (/[+#\u0000]/.test(value)) return 'MQTT 主题不能包含通配符 +、# 或空字符'
    if (value.startsWith('/iot/external/') && !value.startsWith(prefix)) return `完整主题必须位于 ${prefix} 内`
  } else {
    if (!/^[A-Za-z0-9._-]+$/.test(value)) return 'Kafka 主题仅支持字母、数字、点、下划线和连字符'
    if (value.startsWith('iot.external.') && !value.startsWith(prefix)) return `完整主题必须位于 ${prefix} 内`
    if ((value.startsWith(prefix) ? value : prefix + value).length > 249) return 'Kafka 完整主题不能超过 249 个字符'
  }
  if (value === prefix) return '请填写主题前缀后的名称'
  return ''
}

export const queryOperators = [
  ['eq', '等于'],
  ['ne', '不等于'],
  ['gt', '大于'],
  ['gte', '大于等于'],
  ['lt', '小于'],
  ['lte', '小于等于'],
  ['in', '属于列表'],
  ['not_in', '不属于列表'],
  ['contains', '包含文本'],
  ['is_null', '为空'],
  ['not_null', '不为空']
].map(([value, label]) => ({ value, label }))

export function queryFilterIsFlat(filter) {
  return (
    !filter ||
    (!filter.logic && Boolean(filter.field)) ||
    (['and', 'or'].includes(filter.logic) && (filter.children || []).every(child => !child.logic && child.field))
  )
}

export function queryHasUnsafeNumbers(value) {
  if (typeof value === 'number') return !Number.isFinite(value) || (Number.isInteger(value) && !Number.isSafeInteger(value))
  if (value && typeof value === 'object') return Object.values(value).some(queryHasUnsafeNumbers)
  return false
}

export function queryFormFrom(query, sql = '') {
  const filter = query?.filter
  const conditions = filter?.logic ? filter.children || [] : filter?.field ? [filter] : []
  return {
    editor: queryFilterIsFlat(filter) && !queryHasUnsafeNumbers(filter) ? 'form' : 'sql',
    sql,
    dataset: query?.dataset || 'device_reports',
    mode: query?.mode || 'realtime',
    intervalSeconds: query?.intervalSeconds || 60,
    deviceScope: query?.deviceScope || 'all',
    deviceIds: [...(query?.deviceIds || [])],
    allFields: !Object.keys(query?.fields || {}).length,
    fields: Object.entries(query?.fields || {}).map(([output, path]) => ({ output, path })),
    logic: filter?.logic || 'and',
    conditions: conditions.map(item => ({ field: item.field, operator: item.operator, value: JSON.stringify(item.value ?? null) })),
    sample: ''
  }
}

function queryValue(row) {
  if (['is_null', 'not_null'].includes(row.operator)) return undefined
  const value = String(row.value ?? '').trim()
  if (['in', 'not_in'].includes(row.operator)) {
    try {
      const parsed = JSON.parse(value)
      if (Array.isArray(parsed) && parsed.length) return parsed
    } catch {
      /* A comma-separated list is also accepted. */
    }
    return value
      .split(/[,，]/)
      .map(item => item.trim())
      .filter(Boolean)
      .map(item => {
        try {
          return JSON.parse(item)
        } catch {
          return item
        }
      })
  }
  try {
    return JSON.parse(value)
  } catch {
    return value
  }
}

export function topicQueryRequest(form) {
  const options = {
    mode: form.mode,
    intervalSeconds: form.mode === 'interval' ? Number(form.intervalSeconds) : 0,
    deviceScope: form.deviceScope,
    deviceIds: form.deviceScope === 'selected' ? [...new Set(form.deviceIds || [])] : []
  }
  if (form.editor === 'sql') return { querySql: form.sql.trim(), queryOptions: options }
  const children = (form.conditions || []).map(row => ({
    field: row.field.trim(),
    operator: row.operator,
    ...(['is_null', 'not_null'].includes(row.operator) ? {} : { value: queryValue(row) })
  }))
  return {
    query: {
      dataset: form.dataset,
      fields: form.allFields ? {} : Object.fromEntries(form.fields.map(row => [row.output.trim(), row.path.trim()])),
      ...(children.length ? { filter: { logic: form.logic, children } } : {}),
      ...options
    }
  }
}

export function validateTopicQuery(form, datasets) {
  if (form.deviceScope === 'selected' && !form.deviceIds?.length) return '请至少选择一台设备'
  if (
    form.mode === 'interval' &&
    (!Number.isInteger(Number(form.intervalSeconds)) || Number(form.intervalSeconds) < 10 || Number(form.intervalSeconds) > 86400)
  )
    return '查询周期必须是 10–86400 秒的整数'
  if (form.editor === 'sql') return form.sql.trim() ? '' : '请填写查询 SQL'
  const dataset = datasets.find(item => item.id === form.dataset)
  if (!dataset) return '请选择业务数据'
  if (dataset.mode !== form.mode) return '业务数据与发送方式不一致，请重新选择'
  if (!form.allFields) {
    if (!form.fields.length || form.fields.some(row => !row.output.trim() || !row.path.trim())) return '请填写返回字段及输出名称'
    if (new Set(form.fields.map(row => row.output.trim())).size !== form.fields.length) return '输出字段名称不能重复'
  }
  for (const row of form.conditions || []) {
    if (!row.field.trim() || !queryOperators.some(item => item.value === row.operator)) return '请填写查询条件的字段和比较方式'
    if (!['is_null', 'not_null'].includes(row.operator) && !String(row.value ?? '').trim()) return '请填写查询条件的比较值'
    if (queryHasUnsafeNumbers(queryValue(row))) return '比较值超出表单可精确表示的整数范围，请使用 SQL 编辑'
    if (['in', 'not_in'].includes(row.operator) && !queryValue(row).length) return '列表条件至少需要一个值'
  }
  return ''
}

function sqlIdentifier(value) {
  return /^[\p{L}_][\p{L}\p{N}_]*(?:\.[\p{L}\p{N}_]+)*$/u.test(value) ? value : `"${value.replaceAll('"', '""')}"`
}

export function queryToSql(query) {
  const literal = value => (typeof value === 'string' ? `'${value.replaceAll("'", "''")}'` : JSON.stringify(value))
  const condition = node => {
    if (node.logic) return `(${node.children.map(condition).join(` ${node.logic.toUpperCase()} `)})`
    const op = {
      eq: '=',
      ne: '!=',
      gt: '>',
      gte: '>=',
      lt: '<',
      lte: '<=',
      in: 'IN',
      not_in: 'NOT IN',
      is_null: 'IS NULL',
      not_null: 'IS NOT NULL'
    }[node.operator]
    if (['is_null', 'not_null'].includes(node.operator)) return `${sqlIdentifier(node.field)} ${op}`
    if (['in', 'not_in'].includes(node.operator)) return `${sqlIdentifier(node.field)} ${op} (${node.value.map(literal).join(', ')})`
    if (node.operator === 'contains') return `${sqlIdentifier(node.field)} CONTAINS ${literal(node.value)}`
    return `${sqlIdentifier(node.field)} ${op} ${literal(node.value)}`
  }
  const fields = Object.entries(query.fields || {})
  return `SELECT ${fields.length ? fields.map(([output, path]) => `${sqlIdentifier(path)} AS ${sqlIdentifier(output)}`).join(', ') : '*'}\nFROM ${sqlIdentifier(query.dataset)}${query.filter ? `\nWHERE ${condition(query.filter)}` : ''}`
}

export function queryFormToSql(form) {
  const query = topicQueryRequest(form).query
  if (!queryHasUnsafeNumbers(query.filter)) return queryToSql(query)
  const sqlValue = text => {
    const raw = String(text).trim()
    if (/^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?$/.test(raw)) return raw
    let value
    try {
      value = JSON.parse(raw)
    } catch {
      value = raw
    }
    return typeof value === 'string' ? `'${value.replaceAll("'", "''")}'` : JSON.stringify(value)
  }
  const conditions = form.conditions.map(row => {
    const field = sqlIdentifier(row.field.trim())
    const op = {
      eq: '=',
      ne: '!=',
      gt: '>',
      gte: '>=',
      lt: '<',
      lte: '<=',
      in: 'IN',
      not_in: 'NOT IN',
      contains: 'CONTAINS',
      is_null: 'IS NULL',
      not_null: 'IS NOT NULL'
    }[row.operator]
    if (['is_null', 'not_null'].includes(row.operator)) return `${field} ${op}`
    if (['in', 'not_in'].includes(row.operator)) {
      const raw = row.value.trim(),
        body = raw.startsWith('[') && raw.endsWith(']') ? raw.slice(1, -1) : raw
      const values = body.match(/\s*(?:"(?:\\.|[^"\\])*"|[^,，]+)\s*/g) || []
      return `${field} ${op} (${values.map(sqlValue).join(', ')})`
    }
    return `${field} ${op} ${sqlValue(row.value)}`
  })
  return (
    queryToSql({ ...query, filter: undefined }) + (conditions.length ? `\nWHERE (${conditions.join(` ${form.logic.toUpperCase()} `)})` : '')
  )
}
