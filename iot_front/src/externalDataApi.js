import { api } from './api'
export const externalBase = '/api/v1/external-data'
const id = value => encodeURIComponent(value)
const send = (path, method, body) => api(`${externalBase}${path}`, { method, body: JSON.stringify(body) })
export const externalApi = {
  list(kind, query = {}, options = {}) {
    const params = new URLSearchParams(Object.entries(query).filter(([, value]) => value !== '' && value != null))
    return api(`${externalBase}/${kind}?${params}`, options)
  },
  save(kind, value) {
    const { runtime, pushKey, pushKeySet, ...config } = value
    return send(`/${kind}${value.id ? `/${id(value.id)}` : ''}`, value.id ? 'PUT' : 'POST', config)
  },
  remove(kind, value) {
    return api(`${externalBase}/${kind}/${id(value.id)}?revision=${encodeURIComponent(value.revision)}`, { method: 'DELETE' })
  },
  detail(value) {
    return api(`${externalBase}/records/${id(value)}`)
  },
  action(kind, value, action, body = {}) {
    return send(`/${kind}/${id(value)}/${action}`, 'POST', body)
  }
}
