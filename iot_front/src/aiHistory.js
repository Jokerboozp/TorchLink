export const AI_HISTORY_STORAGE_PREFIX = 'iot:ai-history:v1'

export function aiHistoryStorageKey(session, workflowId = '') {
  const base = `${AI_HISTORY_STORAGE_PREFIX}:${session?.tenant || 'unknown'}:${session?.user || 'unknown'}${session?.accessVersion ? `:access:${encodeURIComponent(session.accessVersion)}` : ''}`
  return workflowId ? `${base}:${encodeURIComponent(workflowId)}` : base
}

export function saveAIHistory(storage, session, state, workflowId = '') {
  if (!storage) return false
  try {
    const payload = {
      version: 1,
      conversationId: typeof state?.conversationId === 'string' ? state.conversationId : '',
      selectedWorkflowId: typeof state?.selectedWorkflowId === 'string' ? state.selectedWorkflowId : '',
      messages: Array.isArray(state?.messages) ? state.messages.slice(-50) : [],
      runs: Array.isArray(state?.runs) ? state.runs.slice(0, 30) : [],
      savedAt: Date.now(),
    }
    let encoded = JSON.stringify(payload)
    if (encoded.length > 512000) encoded = JSON.stringify({ ...payload, messages:payload.messages.slice(-20), runs:payload.runs.slice(0, 10) })
    storage.setItem(aiHistoryStorageKey(session, workflowId), encoded)
    return true
  } catch { return false }
}

export function loadAIHistory(storage, session, now = Date.now(), workflowId = '') {
  if (!storage) return null
  const key = aiHistoryStorageKey(session, workflowId)
  try {
    const raw = storage.getItem(key)
    if (!raw) return null
    const saved = JSON.parse(raw)
    if (saved?.version !== 1 || !Array.isArray(saved.messages) || !Array.isArray(saved.runs)) return null
    return {
      conversationId: typeof saved.conversationId === 'string' ? saved.conversationId : '',
      selectedWorkflowId: typeof saved.selectedWorkflowId === 'string' ? saved.selectedWorkflowId : '',
      messages: saved.messages.slice(-50).map(message => message?.status === 'streaming' ? { ...message, status:'canceled', text:message.text || '页面切换时运行已停止。' } : message),
      runs: saved.runs.slice(0, 30).map(run => run?.status === 'running' ? { ...run, status:'canceled', finishedAt:now } : run),
    }
  } catch {
    try { storage.removeItem(key) } catch { /* ignore storage cleanup failures */ }
    return null
  }
}
