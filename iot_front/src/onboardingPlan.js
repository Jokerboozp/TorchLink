// 添加设备向导的纯数据逻辑：请求体、设备端配置说明与诊断色调。接入方式由后端预检结果决定。
export const STANDARD_PROTOCOL = 'iot-standard@1.0.0'

export const modeLabels = {
  standard: '标准 MQTT / HTTP 上报',
  managed: 'HTTP 接口上报，按模板协议解析',
  listener: '设备连接平台（TCP / UDP 监听）',
  dial: '平台主动连接设备（TCP）',
  poll: '平台定时采集（Modbus）',
  unsupported: '暂不支持直接接入'
}

export const diagnosisTagTypes = { success: 'success', info: 'info', warning: 'warning', error: 'danger' }

// 标准协议和 HTTP 托管上报由平台签发凭据，可以使用平台生成的设备编号。
export const usesPlatformIdentity = mode => mode === 'standard' || mode === 'managed'

export function protocolOptions(catalog = []) {
  const options = [{ id: STANDARD_PROTOCOL, name: '标准设备上报', transport: 'MQTT_HTTP', payloadFormat: 'json' }]
  for (const item of catalog) {
    for (const release of item.releases || []) {
      if (release.status === 'PUBLISHED')
        options.push({
          id: `${item.definition.id}@${release.version}`,
          name: `${item.definition.name || item.definition.id} · ${release.version}`,
          transport: release.transport || '',
          payloadFormat: release.payloadFormat || ''
        })
    }
  }
  return options
}

export function preflightQuery(draft) {
  return new URLSearchParams({ productId: draft.productId }).toString()
}

function connectionMode(plan, choice) {
  if (plan?.mode !== 'listener') return plan?.mode || ''
  return choice === 'dial' ? 'dial' : 'listener'
}

function number(value) {
  const parsed = Number(value)
  return Number.isFinite(parsed) && value !== '' && value != null ? parsed : undefined
}

export function enrollRequest(draft, plan) {
  const tags = {}
  for (const row of draft.labels || []) if (row.key?.trim()) tags[row.key.trim()] = row.value ?? ''
  const device = { id: draft.device.id.trim(), name: draft.device.name.trim() }
  if (draft.device.role) device.deviceRole = draft.device.role
  if (draft.device.description?.trim()) device.description = draft.device.description.trim()
  if (Object.keys(tags).length) device.tags = tags
  const c = draft.connection
  const mode = connectionMode(plan, c.choice)
  const connection = { mode }
  if (mode === 'standard' && c.transport) connection.transport = c.transport
  if (mode === 'listener') connection.profileId = c.choice
  if (mode === 'dial' || mode === 'poll') {
    connection.host = c.host.trim()
    connection.port = number(c.port)
    if (mode === 'poll') {
      connection.unitId = number(c.unitId)
      connection.timeoutMs = number(c.timeoutMs)
    }
  }
  return { requestId: draft.requestId, productId: draft.productId, device, connection }
}

// 返回按“名称 / 值”排列的设备端配置，供页面展示和“复制全部”共用。
export function fieldConfiguration(result, accessInfo, credential) {
  const rows = []
  const add = (name, value) => {
    if (value !== undefined && value !== null && value !== '') rows.push({ name, value: String(value) })
  }
  const profile = result?.profile
  const mode = result?.mode
  if (accessInfo?.kind === 'standard' && result?.device?.connector === 'MQTT') {
    add('MQTT Broker', accessInfo.mqttBroker || '未配置平台对外 MQTT 地址')
    add('Client ID', accessInfo.clientId)
    add('上行 Topic', accessInfo.upTopic)
    add('下行 Topic', accessInfo.downTopic)
    add('令牌接口', `POST ${accessInfo.tokenEndpoint}（请求头 X-Device-Key、X-Device-Secret）`)
  } else if (accessInfo) {
    add('上报地址', accessInfo.httpUrl || '未配置平台对外 HTTP 地址')
    add('请求方式', 'POST，JSON 正文，请求头 X-Device-Key、X-Device-Secret')
  }
  if (accessInfo) {
    add('AccessKey', credential?.accessKey || accessInfo.username)
    if (credential?.secret) add('Secret', credential.secret)
  }
  if (mode === 'listener' && profile) {
    add('服务器地址', profile.publicHost ? `${profile.publicHost}:${profile.port}` : '接入点未配置平台对外地址')
    add('网络', String(profile.network || '').toUpperCase())
  }
  if ((mode === 'dial' || mode === 'poll') && profile) {
    add('设备地址', `${profile.host}:${profile.port}`)
    if (mode === 'poll') add('站号', profile.unitId)
  }
  return rows
}

export function configurationText(result, accessInfo, credential) {
  const lines = [
    `设备：${result?.device?.name || ''}（${result?.device?.id || ''}）`,
    ...fieldConfiguration(result, accessInfo, credential).map(row => `${row.name}：${row.value}`)
  ]
  if (accessInfo?.sample) lines.push('', '示例报文：', JSON.stringify(accessInfo.sample, null, 2))
  return lines.join('\n')
}

// 批量清单只接收设备身份及实例地址，不接受模板、租户或协议覆盖。
export function parseDeviceRows(text, mode = '', deviceRole = 'DIRECT') {
  const records = []
  let row = [],
    field = '',
    quoted = false
  for (let index = 0; index <= text.length; index++) {
    const char = text[index] ?? '\n'
    if (char === '"') {
      if (quoted && text[index + 1] === '"') {
        field += '"'
        index++
      } else quoted = !quoted
    } else if (!quoted && (char === ',' || char === '\t')) {
      row.push(field.trim())
      field = ''
    } else if (!quoted && (char === '\n' || char === '\r')) {
      row.push(field.trim())
      field = ''
      if (row.some(Boolean)) records.push(row)
      row = []
      if (char === '\r' && text[index + 1] === '\n') index++
    } else field += char
  }
  if (quoted) throw new Error('CSV 引号未闭合，请检查清单')
  if (records[0]?.[0]?.match(/^(设备编号|id|deviceId)$/i)) records.shift()
  if (!records.length) throw new Error('请填写至少一台设备')
  if (records.length > 1000) throw new Error('每次最多添加 1000 台设备')
  const seen = new Set()
  return records.map((cells, index) => {
    const [id, name, description = '', host = '', port = '', unitId = '1'] = cells
    if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(id || '')) throw new Error(`第 ${index + 1} 行设备编号无效`)
    if (!name) throw new Error(`第 ${index + 1} 行缺少设备名称`)
    if (seen.has(id)) throw new Error(`设备编号 ${id} 重复`)
    seen.add(id)
    const result = { device: { id, name, description, deviceRole: deviceRole === 'GATEWAY' ? 'GATEWAY' : 'DIRECT' } }
    if (mode === 'dial' || mode === 'poll') {
      if (!host) throw new Error(`第 ${index + 1} 行缺少设备地址`)
      const n = Number(port || (mode === 'poll' ? 502 : 0))
      if (!Number.isInteger(n) || n < 1 || n > 65535) throw new Error(`第 ${index + 1} 行端口无效`)
      result.connection = { mode, host, port: n, ...(mode === 'poll' ? { unitId: Number(unitId), timeoutMs: 3000 } : {}) }
      if (mode === 'poll' && (!Number.isInteger(Number(unitId)) || Number(unitId) < 0 || Number(unitId) > 255))
        throw new Error(`第 ${index + 1} 行站号无效`)
    }
    return result
  })
}

export function restoreEnrollDraft(request = {}) {
  const c = request.connection || {}
  return {
    productId: request.productId || '',
    requestId: request.requestId || '',
    device: {
      id: request.device?.id || '',
      name: request.device?.name || '',
      role: request.device?.deviceRole || 'DIRECT',
      description: request.device?.description || ''
    },
    labels: Object.entries(request.device?.tags || {})
      .filter(([key]) => !/(secret|token|password|access.?key|密钥|令牌|密码)/i.test(key))
      .map(([key, value]) => ({ key, value })),
    connection: {
      choice: c.mode === 'dial' ? 'dial' : c.profileId || '',
      transport: c.transport || '',
      host: c.host || '',
      port: c.port || null,
      unitId: c.unitId ?? 1,
      timeoutMs: c.timeoutMs ?? 3000
    }
  }
}
