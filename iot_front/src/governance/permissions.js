import { can } from '../permissions.js'

export function governanceAction(permission) {
  const match = /^(GET|POST|PATCH|DELETE) \/api\/v1\/alarm-governance\/(.+)$/.exec(permission || '')
  if (!match) return ''
  const [, method, path] = match
  if (path.endsWith('/export')) return 'export'
  if (path.includes('/ai-jobs')) return method === 'GET' ? 'view' : 'ai'
  if (path.startsWith('historical-projections')) return method === 'GET' ? 'view' : 'analyse'
  if (path.startsWith('runs')) return method === 'GET' ? 'view' : 'analyse'
  if (path.startsWith('source-fields')) return 'templates'
  if (method === 'GET') return 'view'
  const parts = path.split('/'), resource = parts[0] === 'rounds' ? parts[2] : parts[0], operation = parts.at(-1)
  if (['templates','scene-presets','type-profiles'].includes(resource)) return ['publish','retire'].includes(operation) ? 'publish' : 'templates'
  if (path.includes('attachments') || path.endsWith('/business-links')) return 'record'
  if (resource === 'cases') return ['complete','reopen'].includes(operation) ? operation : 'cases'
  if (resource === 'causes') return 'cause'
  if (resource === 'measures') return operation === 'verify' ? 'acceptance' : 'measures'
  if (['observation-plans','observation-reviews'].includes(resource)) return 'observation'
  return 'record'
}
export function canGovernance(permission, check = can) {
  const action = governanceAction(permission)
  if (!action) return check(permission)
  if (!check('menu:alarmGovernance')) return false
  const config = /\/alarm-governance\/(templates|scene-presets|type-profiles)(\/|$)/.test(permission)
  if (!config && (!check('menu:devices') || !check('menu:alarms'))) return false
  if (permission.includes('/media')) return (check('menu:cameras') || check('action:cameras:history')) && check('action:cameras:download')
  if (permission.includes('/video-events')) return check('menu:cameras') || check('action:cameras:history')
  return action === 'view' || check(`action:alarmGovernance:${action}`)
}
