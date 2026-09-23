export function alarmNavigation(raw) { /* 执行当前语句并推进处理流程。 */
  let value /* 声明 value。 */
  try { value = JSON.parse(raw) } catch { /* 无效导航按普通列表处理 */ } /* 执行当前语句并推进处理流程。 */
  return { /* 返回当前处理结果。 */
    deviceId: typeof value?.deviceId === 'string' ? value.deviceId.trim() : '', /* 执行当前语句并推进处理流程。 */
    alarmId: typeof value?.alarmId === 'string' ? value.alarmId.trim() : '' /* 执行当前语句并推进处理流程。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

export function alarmQuery(filters, page, pageSize) { /* 执行当前语句并推进处理流程。 */
  const query = new URLSearchParams({ status: filters.status || '', level: filters.level || '', page: String(page), pageSize: String(pageSize) }) /* 声明 query。 */
  if (filters.deviceId) query.set('deviceId', filters.deviceId) /* 判断条件并选择处理分支。 */
  return query /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
