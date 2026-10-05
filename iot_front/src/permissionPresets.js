// Presets change only the chosen feature. Existing custom grants stay untouched
// until an administrator explicitly chooses a preset or edits an action.
const chatActions = new Set(['POST /api/v1/ai/chat', 'POST /api/v1/ai/chat/stream'])
export const permissionSections = [
  { name: '日常使用', menus: ['dashboard', 'devices', 'alarms', 'ai', 'raw'] },
  { name: '设备接入', menus: ['products', 'protocols', 'profiles', 'integration', 'cameras', 'externalData', 'messageTopics'] },
  { name: '运维管理', menus: ['inspection', 'rules', 'notifications', 'knowledge'] },
  { name: '消防管理', menus: ['sites', 'duty', 'extinguishers', 'fireStations'] },
  { name: '系统管理', menus: ['aiProviders', 'backups', 'access'] },
  { name: '运维中心', menus: ['opsOverview', 'opsMetrics', 'opsLogs', 'opsDashboards', 'opsAlerts', 'opsCapacity'] }
]
function featureGrants(group, level) {
  if (level === 'none') return []
  return [
    group.id,
    ...group.actions.filter(action => level === 'manage' || (group.menu === 'ai' && chatActions.has(action.id))).map(action => action.id)
  ]
}
export function featureLevel(group, permissions) {
  const ids = new Set([group.id, ...group.actions.map(action => action.id)])
  const current = new Set(permissions.filter(id => ids.has(id)))
  for (const level of ['none', 'view', 'manage']) {
    const grants = featureGrants(group, level)
    if (current.size === grants.length && grants.every(id => current.has(id))) return level
  }
  return 'custom'
}
export function applyFeatureLevel(group, permissions, level) {
  const ids = new Set([group.id, ...group.actions.map(action => action.id)])
  return [...new Set([...permissions.filter(id => !ids.has(id)), ...featureGrants(group, level)])]
}
export function roleDeviceScope(roleIds, roles) {
  const assigned = roles.filter(role => roleIds.includes(role.id))
  if (assigned.some(role => role.deviceScope === 'all')) return { deviceScope: 'all', deviceIds: [] }
  const selected = assigned.filter(role => role.deviceScope === 'selected')
  const deviceIds = [...new Set(selected.flatMap(role => role.deviceIds || []))]
  const unitIds = [...new Set(selected.flatMap(role => role.unitIds || []))]
  return { deviceScope: deviceIds.length || unitIds.length ? 'selected' : 'none', deviceIds, unitIds }
}
