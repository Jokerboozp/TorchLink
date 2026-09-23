// Association selectors need the complete catalog, independently of table pages.
export async function loadAllPages(request, path, options = {}) { /* 执行当前语句并推进处理流程。 */
  const [pathname, search = ''] = path.split('?') /* 执行当前语句并推进处理流程。 */
  const query = new URLSearchParams(search) /* 声明 query。 */
  query.delete('offset') /* 执行当前语句并推进处理流程。 */
  query.delete('limit') /* 执行当前语句并推进处理流程。 */
  query.set('pageSize', '100') /* 执行当前语句并推进处理流程。 */
  const items = [] /* 声明 items。 */
  let first /* 声明 first。 */
  for (let page = 1; ; page += 1) { /* 循环处理当前数据。 */
    query.set('page', String(page)) /* 执行当前语句并推进处理流程。 */
    const data = await request(`${pathname}?${query}`, options) /* 声明 data。 */
    first ??= data /* 执行当前语句并推进处理流程。 */
    const batch = data.items || [] /* 声明 batch。 */
    items.push(...batch) /* 执行当前语句并推进处理流程。 */
    const total = data.total ?? data.count /* 声明 total。 */
    if (!batch.length || (total != null ? items.length >= Number(total) : batch.length < 100)) { /* 判断条件并选择处理分支。 */
      return { ...first, items, total: items.length, count: items.length } /* 返回当前处理结果。 */
    } /* 结束当前表达式或代码块。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
