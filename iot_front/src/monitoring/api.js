import { api, download } from '../api.js'
import { createClientId } from '../clientId.js'
export const monitoringPath = (collection, id = '', operation = '') => `/api/v1/monitoring-gaps/${collection}${id ? '/' + encodeURIComponent(id) : ''}${operation ? '/' + operation : ''}`
export function monitoringRead(collection, id = '', operation = '', query = {}) {
  const params = new URLSearchParams(Object.entries(query).filter(([, value]) => value != null && value !== ''))
  return api(monitoringPath(collection, id, operation) + (params.size ? '?' + params : ''))
}
export const monitoringWrite = (collection, id = '', operation = '', value = {}, method = 'POST') => api(monitoringPath(collection, id, operation), { method, body: JSON.stringify(value), headers: { 'Idempotency-Key': value.idempotencyKey || createClientId() } })
export async function monitoringAll(collection, id = '', operation = '', query = {}) {
  const items = []
  for (let offset = 0; ; offset += 100) {
    const page = await monitoringRead(collection, id, operation, { ...query, limit: 100, offset })
    items.push(...(page.items || []))
    if (!page.items?.length || (page.total != null ? items.length >= Number(page.total) : page.items.length < 100)) return { ...page, items }
  }
}
export const monitoringExport = id => download(monitoringPath('runs', id, 'export'), `监测连续性_${id}.json`)
