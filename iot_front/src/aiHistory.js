export const AI_HISTORY_STORAGE_PREFIX = 'iot:ai-history:v1' /* 执行当前语句并推进处理流程。 */

export function aiHistoryStorageKey(session) { /* 执行当前语句并推进处理流程。 */
  return `${AI_HISTORY_STORAGE_PREFIX}:${session?.tenant || 'unknown'}:${session?.user || 'unknown'}` /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */

export function saveAIHistory(storage, session, state) { /* 执行当前语句并推进处理流程。 */
  if (!storage) return false /* 判断条件并选择处理分支。 */
  try { /* 执行当前语句并推进处理流程。 */
    const payload = { /* 声明 payload。 */
      version: 1, /* 执行当前语句并推进处理流程。 */
      conversationId: typeof state?.conversationId === 'string' ? state.conversationId : '', /* 执行当前语句并推进处理流程。 */
      selectedWorkflowId: typeof state?.selectedWorkflowId === 'string' ? state.selectedWorkflowId : '', /* 执行当前语句并推进处理流程。 */
      messages: Array.isArray(state?.messages) ? state.messages.slice(-50) : [], /* 执行当前语句并推进处理流程。 */
      runs: Array.isArray(state?.runs) ? state.runs.slice(0, 30) : [], /* 执行当前语句并推进处理流程。 */
      savedAt: Date.now(), /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
    let encoded = JSON.stringify(payload) /* 声明 encoded。 */
    if (encoded.length > 512000) encoded = JSON.stringify({ ...payload, messages:payload.messages.slice(-20), runs:payload.runs.slice(0, 10) }) /* 判断条件并选择处理分支。 */
    storage.setItem(aiHistoryStorageKey(session), encoded) /* 执行当前语句并推进处理流程。 */
    return true /* 返回当前处理结果。 */
  } catch { return false } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */

export function loadAIHistory(storage, session, now = Date.now()) { /* 执行当前语句并推进处理流程。 */
  if (!storage) return null /* 判断条件并选择处理分支。 */
  const key = aiHistoryStorageKey(session) /* 声明 key。 */
  try { /* 执行当前语句并推进处理流程。 */
    const raw = storage.getItem(key) /* 声明 raw。 */
    if (!raw) return null /* 判断条件并选择处理分支。 */
    const saved = JSON.parse(raw) /* 声明 saved。 */
    if (saved?.version !== 1 || !Array.isArray(saved.messages) || !Array.isArray(saved.runs)) return null /* 判断条件并选择处理分支。 */
    return { /* 返回当前处理结果。 */
      conversationId: typeof saved.conversationId === 'string' ? saved.conversationId : '', /* 执行当前语句并推进处理流程。 */
      selectedWorkflowId: typeof saved.selectedWorkflowId === 'string' ? saved.selectedWorkflowId : '', /* 执行当前语句并推进处理流程。 */
      messages: saved.messages.slice(-50).map(message => message?.status === 'streaming' ? { ...message, status:'canceled', text:message.text || '页面切换时运行已停止。' } : message), /* 执行当前语句并推进处理流程。 */
      runs: saved.runs.slice(0, 30).map(run => run?.status === 'running' ? { ...run, status:'canceled', finishedAt:now } : run), /* 执行当前语句并推进处理流程。 */
    } /* 结束当前表达式或代码块。 */
  } catch { /* 结束当前表达式或代码块。 */
    try { storage.removeItem(key) } catch { /* ignore storage cleanup failures */ } /* 执行当前语句并推进处理流程。 */
    return null /* 返回当前处理结果。 */
  } /* 结束当前表达式或代码块。 */
} /* 结束当前表达式或代码块。 */
