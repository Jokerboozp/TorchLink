export const HEALTH_INSPECTION_STORAGE_PREFIX = 'iot:health-inspection:v1' /* 执行当前语句并推进处理流程。 */

export function healthInspectionStorageKey(session) { /* 执行当前语句并推进处理流程。 */
  return `${HEALTH_INSPECTION_STORAGE_PREFIX}:${session?.tenant || 'unknown'}:${session?.user || 'unknown'}` /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

export function saveHealthInspection(storage, session, report) { /* 执行当前语句并推进处理流程。 */
  if (!storage || !report || typeof report !== 'object' || Array.isArray(report)) return false /* 判断条件并选择处理分支。 */
  try { /* 执行当前语句并推进处理流程。 */
    storage.setItem(healthInspectionStorageKey(session), JSON.stringify({ version:1, report, savedAt:Date.now() })) /* 执行当前语句并推进处理流程。 */
    return true /* 返回当前处理结果。 */
  } catch { /* 结束当前表达式或代码块。 */
    return false /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

export function loadHealthInspection(storage, session) { /* 执行当前语句并推进处理流程。 */
  if (!storage) return null /* 判断条件并选择处理分支。 */
  const key = healthInspectionStorageKey(session) /* 声明 key。 */
  try { /* 执行当前语句并推进处理流程。 */
    const saved = JSON.parse(storage.getItem(key) || 'null') /* 声明 saved。 */
    if (saved?.version !== 1 || !saved.report || typeof saved.report !== 'object' || Array.isArray(saved.report)) return null /* 判断条件并选择处理分支。 */
    return saved.report /* 返回当前处理结果。 */
  } catch { /* 结束当前表达式或代码块。 */
    try { storage.removeItem(key) } catch { /* ignore storage cleanup failures */ } /* 执行当前语句并推进处理流程。 */
    return null /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
