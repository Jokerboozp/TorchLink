import { api, download } from '../api.js'
import { createClientId } from '../clientId.js'

export const qualityPath = (collection, id = '', operation = '') => `/api/v1/data-quality/${collection}${id ? '/' + encodeURIComponent(id) : ''}${operation ? '/' + operation : ''}`

export function qualityRead(collection, id = '', operation = '', query = {}) {
  const params = new URLSearchParams(Object.entries(query).filter(([, value]) => value !== '' && value != null))
  return api(qualityPath(collection, id, operation) + (params.size ? '?' + params : ''))
}

export function qualityWrite(collection, id = '', operation = '', value = {}, method = 'POST') {
  return api(qualityPath(collection, id, operation), { method, body: JSON.stringify(value), headers: { 'Idempotency-Key': value.idempotencyKey || createClientId() } })
}

// Configuration lists are bounded paged APIs, unlike device page/pageSize lists.
export async function qualityCatalog(collection, query = {}) {
  return qualityAll(collection, '', '', query)
}

export async function qualityAll(collection, id = '', operation = '', query = {}) {
  const items = []
  for (let offset = 0; ; offset += 100) {
    const page = await qualityRead(collection, id, operation, { ...query, limit: 100, offset })
    items.push(...(page.items || []))
    if (!page.items?.length || (page.total != null ? items.length >= Number(page.total) : page.items.length < 100)) return { ...page, items }
  }
}

export const qualityExport = id => download(qualityPath('runs', id, 'export'), `数据质量_${id}.json`)

export async function qualityUpload(file, deviceIds, scope = 'personal') {
  const body = new FormData()
  body.append('file', file)
  body.append('deviceIdsJSON', JSON.stringify(deviceIds))
  body.append('scope', scope)
  return api(qualityPath('calibrations', 'attachments'), { method: 'POST', body })
}

export const qualityAttachment = (id, name) => download(qualityPath('calibrations', 'attachments', encodeURIComponent(id)), name || '校准附件')
