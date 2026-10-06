export const AI_HISTORY_STORAGE_PREFIX = 'iot:ai-history:v1'

function identityBase(session) {
  return `${AI_HISTORY_STORAGE_PREFIX}:${session?.tenant || 'unknown'}:${session?.user || 'unknown'}`
}

function aiHistoryStorageKey(session, workflowId = '') {
  const base = `${identityBase(session)}${session?.accessVersion ? `:access:${encodeURIComponent(session.accessVersion)}` : ''}`
  return workflowId ? `${base}:${encodeURIComponent(workflowId)}` : base
}

// 列出属于当前租户与用户的全部历史键（所有权限版本与智能体）；存储不支持枚举时返回空。
function identityKeys(storage, session) {
  const base = identityBase(session)
  const keys = []
  try {
    if (typeof storage?.key !== 'function') return keys
    for (let index = 0; index < storage.length; index += 1) {
      const key = storage.key(index)
      if (typeof key !== 'string') continue
      if (key === base) keys.push(key)
      else if (key.startsWith(`${base}:`)) {
        // 只匹配本用户的“权限版本”或“智能体”后缀，避免误删以本用户名开头的其他用户。
        const rest = key.slice(base.length + 1)
        if (rest.startsWith('access:') || !rest.includes(':')) keys.push(key)
      }
    }
  } catch {
    /* 存储不可读时跳过清理 */
  }
  return keys
}

function removeKeys(storage, keys) {
  for (const key of keys) {
    try {
      storage.removeItem(key)
    } catch {
      /* ignore storage cleanup failures */
    }
  }
}

// 权限版本变化后旧版本的对话不能再读取；直接删除，避免旧对话残留在浏览器中占用空间。
function purgeOtherAccessVersions(storage, session) {
  if (!session?.accessVersion) return
  const current = `${identityBase(session)}:access:${encodeURIComponent(session.accessVersion)}`
  removeKeys(
    storage,
    identityKeys(storage, session).filter(key => key !== current && !key.startsWith(`${current}:`))
  )
}

// 清除当前租户与用户在本浏览器中的全部智能助手历史（所有权限版本与智能体），供退出登录时调用。
export function clearAIHistory(storage, session) {
  if (!storage || !session?.tenant || !session?.user) return 0
  const keys = identityKeys(storage, session)
  removeKeys(storage, keys)
  return keys.length
}

// 返回 'saved'（完整保存）、'reduced'（浏览器存储已满，只保留最近 10 条消息、不含运行轨迹）或 'failed'。
export function saveAIHistory(storage, session, state, workflowId = '') {
  if (!storage) return 'failed'
  const payload = {
    version: 1,
    conversationId: typeof state?.conversationId === 'string' ? state.conversationId : '',
    selectedWorkflowId: typeof state?.selectedWorkflowId === 'string' ? state.selectedWorkflowId : '',
    messages: Array.isArray(state?.messages) ? state.messages.slice(-50) : [],
    runs: Array.isArray(state?.runs) ? state.runs.slice(0, 30) : [],
    savedAt: Date.now()
  }
  const key = aiHistoryStorageKey(session, workflowId)
  try {
    let encoded = JSON.stringify(payload)
    if (encoded.length > 512000)
      encoded = JSON.stringify({ ...payload, messages: payload.messages.slice(-20), runs: payload.runs.slice(0, 10) })
    storage.setItem(key, encoded)
    return 'saved'
  } catch {
    // Usually the storage quota: keep the latest exchange rather than nothing.
    try {
      storage.setItem(key, JSON.stringify({ ...payload, messages: payload.messages.slice(-10), runs: [] }))
      return 'reduced'
    } catch {
      return 'failed'
    }
  }
}

export function loadAIHistory(storage, session, now = Date.now(), workflowId = '') {
  if (!storage) return null
  purgeOtherAccessVersions(storage, session)
  const key = aiHistoryStorageKey(session, workflowId)
  try {
    const raw = storage.getItem(key)
    if (!raw) return null
    const saved = JSON.parse(raw)
    if (saved?.version !== 1 || !Array.isArray(saved.messages) || !Array.isArray(saved.runs)) return null
    return {
      conversationId: typeof saved.conversationId === 'string' ? saved.conversationId : '',
      selectedWorkflowId: typeof saved.selectedWorkflowId === 'string' ? saved.selectedWorkflowId : '',
      messages: saved.messages
        .slice(-50)
        .map(message =>
          message?.status === 'streaming' ? { ...message, status: 'canceled', text: message.text || '运行已中断。' } : message
        ),
      runs: saved.runs.slice(0, 30).map(run => (run?.status === 'running' ? { ...run, status: 'canceled', finishedAt: now } : run))
    }
  } catch {
    try {
      storage.removeItem(key)
    } catch {
      /* ignore storage cleanup failures */
    }
    return null
  }
}
