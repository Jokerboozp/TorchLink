// 告警通知：渠道与策略的表单转换。凭据只写不读，编辑时留空表示保留原值。
export const channelTypes = [
  { value: 'dingtalk', label: '钉钉机器人', urlHint: 'https://oapi.dingtalk.com/robot/send?access_token=…', sign: true },
  { value: 'wecom', label: '企业微信机器人', urlHint: 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=…', sign: false },
  { value: 'feishu', label: '飞书机器人', urlHint: 'https://open.feishu.cn/open-apis/bot/v2/hook/…', sign: true },
  { value: 'webhook', label: '通用 Webhook', urlHint: 'https://…，请求头 X-IoT-Signature 为 HMAC-SHA256 签名', sign: true },
  { value: 'smtp', label: 'SMTP 邮件', urlHint: '', sign: false }
]

export const taskStatuses = { PENDING: '等待发送', SENDING: '发送中', SENT: '已发送', FAILED: '发送失败', CANCELLED: '已取消' }
export const taskTone = status => ({ SENT: 'success', FAILED: 'danger', CANCELLED: 'neutral', PENDING: 'warning', SENDING: 'warning' }[status] || 'neutral')

export function blankChannel() {
  return { id: '', name: '', type: 'dingtalk', enabled: true, version: 0, secretSet: false, config: { host: '', port: 465, security: 'tls', username: '', from: '' }, secret: { url: '', signSecret: '', password: '' } }
}

export function channelForm(value) {
  const base = blankChannel()
  if (!value) return base
  return { ...base, ...value, config: { ...base.config, ...(value.config || {}) }, secret: { url: '', signSecret: '', password: '' } }
}

// 新建时提交凭据；编辑时只有填写了任一凭据字段才提交，未填写的字段由服务端保留。
export function channelPayload(form) {
  const payload = { id: form.id, name: form.name.trim(), type: form.type, enabled: form.enabled, version: form.version || 0, config: {} }
  if (form.type === 'smtp') {
    const { host, port, security, username, from } = form.config
    payload.config = { host: host.trim(), port: Number(port) || 0, security, username: username.trim(), from: from.trim() }
  }
  const secret = Object.fromEntries(Object.entries(form.secret).map(([k, v]) => [k, String(v || '').trim()]).filter(([, v]) => v))
  if (!form.version || Object.keys(secret).length) payload.secret = secret
  return payload
}

export function blankStage(delaySeconds = 0) {
  return { delaySeconds, channelIds: [], users: [], roles: [], onDuty: false, stationIds: [], emails: [], mobiles: [] }
}

export function blankPolicy() {
  return { id: '', name: '', enabled: true, version: 0, levels: ['CRITICAL', 'HIGH'], alarmTypes: [], productIds: [], notifyRecovery: false, stages: [blankStage(0), blankStage(180)] }
}

export function policyForm(value) {
  if (!value) return blankPolicy()
  return JSON.parse(JSON.stringify({ ...blankPolicy(), ...value, stages: (value.stages || []).map(stage => ({ ...blankStage(), ...stage })) }))
}

const splitList = value => Array.isArray(value) ? value : String(value || '').split(/[,，\s]+/).map(item => item.trim()).filter(Boolean)

export function policyPayload(form) {
  return {
    id: form.id, name: form.name.trim(), enabled: form.enabled, version: form.version || 0,
    levels: [...form.levels], alarmTypes: [...form.alarmTypes], productIds: [...form.productIds], notifyRecovery: form.notifyRecovery,
    stages: form.stages.map((stage, index) => ({
      delaySeconds: index === 0 ? 0 : Math.max(0, Math.round(Number(stage.delaySeconds) || 0)),
      channelIds: [...stage.channelIds], users: [...stage.users], roles: [...stage.roles], onDuty: Boolean(stage.onDuty),
      stationIds: stage.onDuty ? [...stage.stationIds] : [], emails: splitList(stage.emails), mobiles: splitList(stage.mobiles)
    }))
  }
}

export function stageSummary(stage, index, channelName) {
  const when = index === 0 ? '立即' : `${Math.round(stage.delaySeconds / 60)} 分钟未确认`
  const who = [stage.users?.length && `${stage.users.length} 名用户`, stage.roles?.length && `${stage.roles.length} 个角色`, stage.onDuty && '当班人员', stage.emails?.length && `${stage.emails.length} 个邮箱`].filter(Boolean).join('、')
  return `${when} → ${(stage.channelIds || []).map(channelName).join('、') || '未选渠道'}${who ? `（${who}）` : ''}`
}
