export const HEALTH_INSPECTION_STORAGE_PREFIX = 'iot:health-inspection:v1'

export function healthInspectionStorageKey(session) {
  return `${HEALTH_INSPECTION_STORAGE_PREFIX}:${session?.tenant || 'unknown'}:${session?.user || 'unknown'}`
}

export function saveHealthInspection(storage, session, report) {
  if (!storage || !report || typeof report !== 'object' || Array.isArray(report)) return false
  try {
    storage.setItem(healthInspectionStorageKey(session), JSON.stringify({ version:1, report, savedAt:Date.now() }))
    return true
  } catch {
    return false
  }
}

export function loadHealthInspection(storage, session) {
  if (!storage) return null
  const key = healthInspectionStorageKey(session)
  try {
    const saved = JSON.parse(storage.getItem(key) || 'null')
    if (saved?.version !== 1 || !saved.report || typeof saved.report !== 'object' || Array.isArray(saved.report)) return null
    return saved.report
  } catch {
    try { storage.removeItem(key) } catch { /* ignore storage cleanup failures */ }
    return null
  }
}
