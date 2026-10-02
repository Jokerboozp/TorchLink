export const externalTabs = { sources:'数据来源', endpoints:'接入接口', bindings:'编号绑定', records:'接收记录', jobs:'拉取任务' }
export const externalKinds = { video_alarm:'视频告警', alarm:'设备告警', property:'属性数据', state:'设备状态', event:'事件' }
export const externalStatuses = { PENDING:'待处理', RUNNING:'处理中', RETRY:'等待重试', PROCESSED:'已处理', COMPLETED:'已完成', WAITING_BINDING:'等待编号绑定', FAILED:'处理失败', IGNORED:'已忽略', FILTERED:'已过滤', DUPLICATE:'重复数据', CONFLICT:'内容冲突', REPLACED:'已按新规则重新提取' }
export const eventTargets = { id:'外部事件编号', objectId:'外部设备 / 摄像头编号', timestamp:'发生时间', version:'事件版本', status:'事件状态', alarmType:'告警类型', alarmLevel:'告警等级', content:'告警内容', confidence:'置信度', snapshotUrl:'截图地址', videoClipUrl:'录像地址', online:'在线状态', data:'业务数据' }
export const cloneExternal = value => JSON.parse(JSON.stringify(value))
export function jsonObject(text, label = 'JSON') {
  let value
  try { value = JSON.parse(text || '{}') } catch { throw new Error(`${label} 格式不正确`) }
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error(`${label} 必须是 JSON 对象`)
  return value
}
export function jsonValue(text, label = '值') {
  if (text === '' || text == null) return undefined
  try { return JSON.parse(text) } catch { throw new Error(`${label} 须填写 JSON 值；文本请使用双引号`) }
}
export function jsonSample(text) {
  const value = jsonValue(text, '报文样例')
  if (!value || typeof value !== 'object') throw new Error('报文样例须为 JSON 对象或数组')
  return value
}
export function blankAuth() { return { type:'none', secret:'', header:'', query:'', username:'', timestampHeader:'', tokenUrl:'', tokenPath:'', tokenExpiresPath:'', tokenBody:{} } }
export function blankSource(username = '') { return { id:'', revision:0, name:'', username, enabled:false, requestIntervalMillis:0, allowedHosts:[], auth:blankAuth(), description:'' } }
export function mappingFields(kind = 'alarm') {
  const identity = [{ target:'id', path:'id', type:'string', required:true }, { target:'objectId', path:kind === 'video_alarm' ? 'cameraId' : 'deviceId', type:'string', required:true }, { target:'timestamp', path:'timestamp', type:'timestamp', timeFormat:'milliseconds', required:true }]
  if (kind === 'property' || kind === 'event') return [...identity, { target:'data', path:'data', type:'json', required:true }]
  if (kind === 'state') return [...identity, { target:'online', path:'online', type:'boolean', required:true }]
  const alarm = [{ target:'alarmType', path:'alarmType', type:'string', required:true }, { target:'alarmLevel', path:'alarmLevel', type:'string', default:'HIGH' }, { target:'content', path:'content', type:'string' }, { target:'status', path:'status', type:'string', default:'ACTIVE' }]
  return [...identity, ...alarm, ...(kind === 'video_alarm' ? [{ target:'confidence', path:'confidence', type:'number' }, { target:'snapshotUrl', path:'snapshotUrl', type:'string' }, { target:'videoClipUrl', path:'videoClipUrl', type:'string' }] : [])]
}
export function blankEndpoint(sourceId = '') {
  return { id:'', revision:0, sourceId, name:'', enabled:false, mode:'push', kind:'alarm', method:'GET', url:'', headers:{}, query:{}, requestBody:{}, mapping:{ fields:mappingFields() }, pagination:{ mode:'none', pageSize:100, start:1, maxPages:100 }, intervalSeconds:0, requestIntervalMillis:0, overlapSeconds:60, timeoutSeconds:30, maxAttempts:8, responseStatus:200, responseBody:{ success:true } }
}
export function fieldsToForm(fields = []) { return fields.map(field => ({ ...cloneExternal(field), valueText:field.value === undefined ? '' : JSON.stringify(field.value), defaultText:field.default === undefined ? '' : JSON.stringify(field.default), valuesText:JSON.stringify(field.values || {}) })) }
export function fieldsFromForm(fields = []) {
  return fields.map(({ valueText, defaultText, valuesText, ...field }) => {
    const out = { ...field, target:field.target?.trim(), path:field.path?.trim() }
    if (!out.target) throw new Error('请选择每条字段规则的目标字段')
    if (!out.constant && !out.path) throw new Error(`请填写 ${eventTargets[out.target] || out.target} 的取值路径`)
    delete out.value; delete out.default; delete out.values
    if (out.constant) out.value = jsonValue(valueText, '固定值')
    if (out.constant && out.value === undefined) throw new Error('请填写固定值')
    if (defaultText !== '') out.default = jsonValue(defaultText, '默认值')
    const values = jsonObject(valuesText, '枚举映射')
    if (Object.keys(values).length) out.values = values
    return out
  })
}
function validateRequestInterval(value) {
  if (value != null && value !== 0 && (value < 100 || value > 3600000)) throw new Error('请求最小间隔为 100 至 3600000 毫秒；填 0 使用默认 1000 毫秒')
}
export function validateSource(value) {
  validateRequestInterval(value.requestIntervalMillis)
  if (!value.name.trim() || !value.username.trim()) throw new Error('请填写来源名称和执行用户')
  return { ...value, name:value.name.trim(), username:value.username.trim(), allowedHosts:[...new Set(value.allowedHosts.map(host => host.trim()).filter(Boolean))] }
}
export function validateEndpoint(value) {
  validateRequestInterval(value.requestIntervalMillis)
  if (!value.sourceId || !value.name.trim()) throw new Error('请选择数据来源并填写接口名称')
  if (value.mode === 'pull' && !/^https?:\/\//i.test(value.url)) throw new Error('请填写完整的 HTTP 或 HTTPS 请求地址')
  if (value.mode === 'pull' && value.intervalSeconds > 0 && value.intervalSeconds < 10) throw new Error('自动拉取间隔不能小于 10 秒，填 0 可仅手动拉取')
  if (!value.mapping.fields.length) throw new Error('至少配置一条字段规则')
  if (new Set(value.mapping.fields.map(field => field.target)).size !== value.mapping.fields.length) throw new Error('目标字段不能重复')
  return value
}
export function timeWindow(range) {
  const [from, to] = range || []
  if (!(from > 0 && to > from)) throw new Error('请选择有效的开始与结束时间')
  return { from:Number(from), to:Number(to) }
}
// List, preview and detail use independent lanes. Identity changes invalidate every lane.
export function requestFence(identity) {
  const versions = new Map()
  let disposed = false
  return {
    begin(lane) {
      const version = (versions.get(lane) || 0) + 1
      versions.set(lane, version)
      const owner = identity()
      return () => !disposed && versions.get(lane) === version && owner === identity()
    },
    invalidate(lane) { versions.set(lane, (versions.get(lane) || 0) + 1) },
    dispose() { disposed = true; versions.clear() }
  }
}
