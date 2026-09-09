// A commissioning client: reconnects with fresh credentials, but never silently
// queues telemetry while offline. Repeating a report is an explicit user action.
export async function deviceRequest(path, credential, body, fetcher = fetch) {
  if (path !== '/api/v1/device-mqtt/token' && !path.startsWith('/api/v1/device-ingest/standard/')) throw new Error('不支持的设备认证路径')
  const response = await fetcher(path, {
    method: 'POST', credentials: 'omit', signal: AbortSignal.timeout(8000),
    headers: { 'Content-Type': 'application/json', 'X-Device-Key': credential.key, 'X-Device-Secret': credential.secret }, body
  })
  const data = await response.json().catch(() => ({}))
  if (!response.ok) throw Object.assign(new Error(data.message || data.detail || `HTTP ${response.status}`), { status: response.status, code: data.code || data.errorCode })
  return data
}

export class StandardDeviceProbe {
  constructor({ token, connect, onState = () => {}, onCommand = () => {}, schedule = (fn, ms) => setTimeout(fn, ms), cancel = id => clearTimeout(id) }) {
    Object.assign(this, { token, connect, onState, onCommand, schedule, cancel })
    this.generation = 0
    this.attempt = 0
  }
  stop() {
    this.generation++
    this.cancel(this.timer)
    this.client?.end(true)
    this.client = null
    this.onState('DISCONNECTED')
  }
  start() {
    this.stop()
    this.attempt = 0
    return this.open(this.generation)
  }
  dropConnection() {
    // Drop only this commissioning client's transport; leave the platform and
    // other device sessions untouched. The normal close handler drives retry.
    this.client?.stream?.destroy()
  }
  retry(generation, error) {
    if (generation !== this.generation) return
    this.cancel(this.timer)
    this.client?.end(true)
    this.client = null
    // Revoked credentials need user action, not an infinite authentication loop.
    if (error?.status === 401 || error?.status === 403 || [4, 5, 134, 135].includes(error?.code)) {
      this.onState('AUTH_FAILED')
      return
    }
    const delay = Math.min(30000, 1000 * 2 ** Math.min(this.attempt++, 5))
    this.onState('RETRYING', delay)
    this.timer = this.schedule(() => this.open(generation), delay)
  }
  async open(generation) {
    if (generation !== this.generation) return
    this.onState('CONNECTING')
    try {
      const auth = await this.token()
      if (generation !== this.generation) return
      const client = this.connect(auth.websocketUrl, {
        username: auth.username, password: auth.token,
        clientId: `iot-commission-${crypto.randomUUID()}`, protocolVersion: 4,
        clean: true, reconnectPeriod: 0, connectTimeout: 8000, queueQoSZero: false
      })
      this.client = client
      let finished = false
      const current = () => generation === this.generation && this.client === client && !finished
      const failure = error => { if (!current()) return; finished = true; this.retry(generation, error) }
      client.on('error', failure)
      client.on('close', () => failure())
      client.on('message', (topic, payload) => { if (current()) this.onCommand(topic, payload.toString()) })
      client.on('connect', () => {
        if (!current()) return
        this.attempt = 0
        this.onState('CONNECTED')
        const commandTopic = auth.publishTopic.replace('/iot/up/', '/iot/down/').replace(/\/property$/, '/command')
        client.subscribe(commandTopic, { qos: 1 }, (error, granted) => {
          if (error || granted?.some(x => x.qos >= 128)) failure(error || { code: 135 })
        })
        if (!current()) return
        this.cancel(this.timer)
        this.timer = this.schedule(() => {
          if (!current()) return
          finished = true
          client.end(true)
          this.client = null
          this.open(generation)
        }, Math.max(1000, (auth.expiresIn - 30) * 1000))
      })
    } catch (error) { this.retry(generation, error) }
  }
  async publish(topic, body) {
    const client = this.client
    if (!client?.connected) throw new Error('设备未连接；恢复连接后可重发同一条消息')
    await new Promise((resolve, reject) => {
      const timer = this.schedule(() => reject(new Error('发布确认超时；可重发同一条消息验证幂等')), 8000)
      client.publish(topic, body, { qos: 1, retain: false }, error => {
        this.cancel(timer)
        error ? reject(error) : resolve()
      })
    })
  }
}

export async function standardRawID(tenant, product, device, kind, id) {
  const hash = await crypto.subtle.digest('SHA-256', new TextEncoder().encode([tenant, product, device, kind, id].join('\0')))
  return 'raw_std_' + Array.from(new Uint8Array(hash)).map(x => x.toString(16).padStart(2, '0')).join('').slice(0, 32)
}
