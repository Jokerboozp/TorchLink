import { createClientId } from '../clientId.js'
import { api, download } from '../api.js'

export const governancePath = (collection, id = '', operation = '') => `/api/v1/alarm-governance/${collection}${id ? '/' + encodeURIComponent(id) : ''}${operation ? '/' + operation : ''}`
export function governanceRead(collection, id = '', operation = '', query = {}) {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) if (value != null && value !== '') params.set(key, Array.isArray(value) ? value.join(',') : value)
  return api(governancePath(collection, id, operation) + (params.size ? '?' + params : ''))
}
// The body key is the single idempotency contract. Keep it stable across retries.
export const governanceWrite = (collection, id = '', operation = '', value = {}, method = 'POST') => api(governancePath(collection, id, operation), { method, body: JSON.stringify(value) })
export async function governanceCatalog(collection, query = {}, id = '', operation = '') {
  const items = []
  for (let offset = 0; ; offset += 100) {
    const page = await governanceRead(collection, id, operation, { ...query, limit: 100, offset })
    items.push(...(page.items || []))
    if (!page.items?.length || (page.total != null ? items.length >= Number(page.total) : page.items.length < 100)) return { ...page, items }
  }
}
export const governanceExport = (id, name = '') => download(governancePath('reports', id, 'export'), name || `治理报告_${id}.txt`)
export async function governanceUpload(collection, id, file, deviceIds, idempotencyKey = createClientId()) {
  const body = new FormData(); body.append('file', file); body.append('idempotencyKey', idempotencyKey); body.append('deviceIdsJSON', JSON.stringify(deviceIds))
  return api(governancePath(collection, id, 'attachments'), { method: 'POST', body })
}
export const governanceAttachment = (id, name) => download(governancePath('attachments', id, 'download'), name || '治理附件')

export const governanceMedia = (id, kind) => download(governancePath('business-links', id, 'media') + '?' + new URLSearchParams({kind}), kind === 'clip' ? '治理视频证据.mp4' : '治理视频截图.jpg')
