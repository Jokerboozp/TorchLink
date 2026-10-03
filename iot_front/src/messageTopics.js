export const topicDirections = [
  { value:'outbound', label:'对外发布' },
  { value:'inbound', label:'设备接入' },
  { value:'internal', label:'内部消息' }
]

export function topicDirectionLabel(value) {
  return topicDirections.find(item => item.value === value)?.label || value
}

export function filterMessageTopics(items, { protocol = '', direction = '', keyword = '' } = {}) {
  const query = keyword.trim().toLowerCase()
  return items.filter(item => (!protocol || item.protocol === protocol) && (!direction || item.direction === direction) &&
    (!query || [item.name, item.topic, item.defaultTopic, item.description, item.id].some(value => String(value || '').toLowerCase().includes(query))))
}

export function topicStatus(item) {
  if (!item.enabled) return { tone:'neutral', label:'已停用' }
  if (!item.effectiveEnabled) return { tone:'warning', label:'通道未启用' }
  return { tone:'success', label:'已启用' }
}

export function formatTopicTime(seconds, empty = '长期有效') {
  return Number(seconds) > 0 ? new Date(Number(seconds) * 1000).toLocaleString('zh-CN', { hour12:false }) : empty
}

export function topicAccountStatus(account, now = Date.now()) {
  if (!account.enabled) return { tone:'neutral', label:'已停用' }
  if (Number(account.expiresAt) > 0 && Number(account.expiresAt) * 1000 <= now) return { tone:'warning', label:'已过期' }
  return { tone:'success', label:'已启用' }
}

export function credentialStatus(credential, now = Date.now()) {
  if (credential.status === 'revoking') return '撤销处理中'
  if (Number(credential.expiresAt) * 1000 <= now) return '已过期'
  return ({ active:'有效', provisioning:'开通中' })[credential.status] || '状态待确认'
}

export function validateManagedTopic(form, sources) {
  if (!String(form.name || '').trim()) return '请填写主题名称'
  if (!sources.some(source => source.id === form.sourceId)) return '请选择消息数据源'
  if (!/^[A-Za-z0-9_-]{1,48}$/.test(String(form.topic || '').trim())) return '主题标识只支持 1–48 个字母、数字、下划线或连字符'
  return ''
}

export function validateSharedTopic(form, prefixes, editing = false) {
  if (!String(form.name || '').trim()) return '请填写主题名称'
  if (editing) return ''
  if (!['mqtt', 'kafka'].includes(form.protocol)) return '请选择消息协议'
  const value = String(form.topic || '').trim(), prefix = prefixes?.[form.protocol]
  if (!value) return '请填写主题地址或后缀'
  if (!prefix) return '未取得当前租户的主题前缀，请刷新后重试'
  if (form.protocol === 'mqtt') {
    if (/[+#\u0000]/.test(value)) return 'MQTT 主题不能包含通配符 +、# 或空字符'
    if (value.startsWith('/') && !value.startsWith(prefix)) return `完整主题必须位于 ${prefix} 内`
  } else {
    if (!/^[A-Za-z0-9._-]+$/.test(value)) return 'Kafka 主题仅支持字母、数字、点、下划线和连字符'
    if (value.startsWith('iot.external.') && !value.startsWith(prefix)) return `完整主题必须位于 ${prefix} 内`
    if ((value.startsWith(prefix) ? value : prefix + value).length > 249) return 'Kafka 完整主题不能超过 249 个字符'
  }
  if (value === prefix) return '请填写主题前缀后的名称'
  return ''
}

export function topicRulePayload(form, rows, revision) {
  return { revision, name:String(form.name || '').trim(), sourceId:form.sourceId, enabled:Boolean(form.enabled),
    deviceScope:form.deviceScope, deviceIds:form.deviceScope === 'selected' ? [...new Set(form.deviceIds || [])] : [],
    format:form.format, fields:form.format === 'json' ? Object.fromEntries(rows.map(row => [row.output.trim(), row.path.trim()])) : {},
    template:form.format === 'text' ? form.template : '' }
}

export function validateTopicRule(form, rows, sources) {
  if (!String(form.name || '').trim()) return '请填写规则名称'
  if (!sources.some(source => source.id === form.sourceId)) return '请选择当前协议支持的触发事件'
  if (!['all', 'selected'].includes(form.deviceScope)) return '请选择设备范围'
  if (form.deviceScope === 'selected' && !form.deviceIds?.length) return '请至少选择一台设备'
  if (!['original', 'json', 'text'].includes(form.format)) return '请选择消息格式'
  if (form.format === 'json') {
    if (!rows.length || rows.some(row => !row.output.trim() || !row.path.trim())) return '请填写每个输出字段和来源路径'
    if (new Set(rows.map(row => row.output.trim())).size !== rows.length) return '输出字段名称不能重复'
  }
  if (form.format === 'text' && !String(form.template || '').trim()) return '请填写文本模板'
  return ''
}

export function validateTopicMessage(payload, format) {
  if (!String(payload || '').length) return '请填写消息内容'
  if (new TextEncoder().encode(payload).length > 256 * 1024) return '消息内容不能超过 256 KB'
  if (format === 'json') {
    try { JSON.parse(payload) } catch { return '消息内容不是有效的 JSON' }
  } else if (format !== 'text') return '请选择 JSON 或文本格式'
  return ''
}

export function topicAccountPayload(form, revision) {
  return {
    revision, name:String(form.name || '').trim(), username:form.username, enabled:Boolean(form.enabled),
    topicIds:[...new Set(form.topicIds || [])], publishTopicIds:[...new Set(form.publishTopicIds || [])], deviceScope:form.deviceScope,
    deviceIds:form.deviceScope === 'selected' ? [...new Set(form.deviceIds || [])] : [],
    expiresAt:form.expiresAt ? Math.floor(Number(form.expiresAt) / 1000) : 0
  }
}

export function validateTopicAccount(form, topics, users) {
  if (!String(form.name || '').trim()) return '请填写对接账号名称'
  if (!users.some(user => user.username === form.username)) return '请选择有效的平台用户'
  if (!form.topicIds?.length && !form.publishTopicIds?.length) return '请至少授权一个主题的订阅或发布权限'
  if ((form.topicIds || []).some(id => !topics.some(topic => topic.id === id && topic.editable))) return '订阅主题已发生变化，请刷新配置后重新选择'
  if ((form.publishTopicIds || []).some(id => !topics.some(topic => topic.id === id && topic.editable && topic.shared))) return '发布权限只能授权给当前可用的共享主题'
  if (!['all', 'selected'].includes(form.deviceScope)) return '请选择设备范围'
  if (form.expiresAt != null && (!Number.isFinite(Number(form.expiresAt)) || Number(form.expiresAt) < 0)) return '请设置有效的账号到期时间'
  return ''
}

export function topicVariableLabel(variable) {
  const name = String(variable).replace(/^\{|\}$/g, '')
  return `{${name}}：${({ tenantId:'当前租户标识', deviceId:'设备标识', productId:'设备模板标识', messageType:'消息类型', eventType:'事件类型', alarmId:'告警标识' })[name] || '由实际消息替换'}`
}

// Keep the built-in destination usable. Custom destinations stay below the
// server-provided tenant prefix; the server performs final protocol validation.
export function validateTopicTarget(item, value, prefixes = {}) {
  const topic = String(value || '').trim()
  if (!topic) return '请填写主题模板'
  if (topic === item.defaultTopic) return ''
  const prefix = prefixes[item.protocol]
  if (!prefix) return '未取得当前租户的主题前缀，请刷新后重试'
  const separator = item.protocol === 'mqtt' ? '/' : '.'
  const scopedPrefix = prefix.endsWith(separator) ? prefix : prefix + separator
  if (!topic.startsWith(scopedPrefix) || topic.length === scopedPrefix.length) return `自定义主题必须以 ${scopedPrefix} 开头，并填写后续名称`
  if (item.protocol === 'mqtt' && /[+#\u0000]/.test(topic)) return '发布主题不能包含 MQTT 通配符 +、# 或空字符'
  if (item.protocol === 'kafka' && (!/^[a-zA-Z0-9._-]+$/.test(topic) || topic.length > 249)) return 'Kafka 主题仅支持字母、数字、点、下划线和连字符，最长 249 个字符'
  const allowed = new Set((item.variables || []).map(variable => String(variable).replace(/^\{|\}$/g, '')))
  const variables = [...topic.matchAll(/\{([^{}]+)\}/g)]
  if (variables.some(([, name]) => !allowed.has(name)) || /[{}]/.test(topic.replace(/\{[^{}]+\}/g, ''))) return '主题模板包含不支持的变量，请使用下方列出的变量'
  return ''
}
