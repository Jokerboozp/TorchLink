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
  const options = [{ id: STANDARD_PROTOCOL, name: '标准设备上报', transport: 'MQTT_HTTP' }]
  for (const item of catalog) {
    for (const release of item.releases || []) {
      if (release.status === 'PUBLISHED') options.push({ id: `${item.definition.id}@${release.version}`, name: `${item.definition.name || item.definition.id} · ${release.version}`, transport: release.transport || '' })
    }
  }
  return options
}

// 双通道协议需要在模板上选定一个默认通道。
export function transportChoices(transport) {
  if (transport === 'MQTT_HTTP') return ['MQTT', 'HTTP']
  if (transport === 'TCP_UDP') return ['TCP', 'UDP']
  return []
}

export function preflightQuery(draft) {
  const query = new URLSearchParams()
  if (draft.source === 'existing') query.set('productId', draft.productId)
  else {
    query.set('protocolPackageId', draft.newProduct.protocolPackageId)
    if (draft.newProduct.transport) query.set('transport', draft.newProduct.transport)
    if (draft.newProduct.category) query.set('category', draft.newProduct.category)
  }
  return query.toString()
}

// connectionChoice 为 listener 模式的选择：已有接入点 ID、new（新建共享监听）或 dial（平台主动连接）。
export function connectionMode(plan, choice) {
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
  if (mode === 'listener') {
    if (c.choice === 'new') connection.listener = { network: c.network || plan.networks?.[0] || '', host: c.bindHost?.trim() || '', publicHost: c.publicHost.trim(), port: number(c.port) }
    else connection.profileId = c.choice
  }
  if (mode === 'dial' || mode === 'poll') {
    connection.host = c.host.trim()
    connection.port = number(c.port)
    if (mode === 'poll') {
      connection.unitId = number(c.unitId)
      connection.timeoutMs = number(c.timeoutMs)
    }
  }
  const body = { requestId: draft.requestId, device, connection }
  if (draft.source === 'existing') body.productId = draft.productId
  else {
    const p = draft.newProduct
    const metadata = Object.fromEntries(Object.entries({ manufacturer: p.manufacturer?.trim(), model: p.model?.trim() }).filter(([, value]) => value))
    body.newProduct = { id: p.id.trim(), name: p.name.trim(), category: p.category, protocolPackageId: p.protocolPackageId, transport: p.transport || undefined, ...(Object.keys(metadata).length ? { metadata } : {}) }
  }
  return body
}

// 返回按“名称 / 值”排列的设备端配置，供页面展示和“复制全部”共用。
export function fieldConfiguration(result, accessInfo, credential) {
  const rows = []
  const add = (name, value) => { if (value !== undefined && value !== null && value !== '') rows.push({ name, value: String(value) }) }
  const profile = result?.profile
  const mode = result?.mode
  if (accessInfo?.kind === 'standard' && result?.device?.tags?.connector === 'MQTT') {
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
  const lines = [`设备：${result?.device?.name || ''}（${result?.device?.id || ''}）`, ...fieldConfiguration(result, accessInfo, credential).map(row => `${row.name}：${row.value}`)]
  if (accessInfo?.sample) lines.push('', '示例报文：', JSON.stringify(accessInfo.sample, null, 2))
  return lines.join('\n')
}
