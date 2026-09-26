export function alarmNavigation(raw) {
  let value
  try { value = JSON.parse(raw) } catch { /* 无效导航按普通列表处理 */ }
  return {
    deviceId: typeof value?.deviceId === 'string' ? value.deviceId.trim() : '',
    alarmId: typeof value?.alarmId === 'string' ? value.alarmId.trim() : ''
  }
}

export function alarmQuery(filters, page, pageSize) {
  const query = new URLSearchParams({ status: filters.status || '', level: filters.level || '', page: String(page), pageSize: String(pageSize) })
  if (filters.deviceId) query.set('deviceId', filters.deviceId)
  return query
}
