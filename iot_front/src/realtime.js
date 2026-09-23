import mqtt from 'mqtt'
import { api, session } from './api'
import { permissionState, refreshPermissions } from './permissions'

let client
let pollTimer
let brokerTimer
let generation = 0

export function stopRealtime() {
  generation++
  clearTimeout(pollTimer)
  clearTimeout(brokerTimer)
  client?.end(true)
  client = undefined
}

export async function startRealtime(onMessage) {
  stopRealtime()
  const run = generation
  let previous = null
  const brokerDelivered = new Map()
  const brokerMessage = (topic, payload) => {
    const body = payload.toString()
    try {
      const value = JSON.parse(body)
      const kind = topic.includes('/alarm/') ? 'alarm' : topic.includes('/device/state/') ? 'state' : ''
      const id = kind === 'alarm' ? value.alarmId : kind === 'state' ? value.deviceId : ''
      if (id) {
        const key = `${kind}:${id}`
        const normalized = JSON.stringify(value)
        const delivered = brokerDelivered.get(key)
        if (previous?.get(key) === normalized || delivered?.payload === normalized && delivered.expiresAt > Date.now()) return
        brokerDelivered.set(key, { payload: normalized, expiresAt: Date.now() + 10000 })
      }
    } catch { /* Other MQTT topics do not use the event snapshot. */ }
    onMessage?.(topic, body)
  }
  // All accounts receive authoritative alarms over the authenticated API.
  // Broker availability and token renewal must not reset this snapshot.
  const poll = async () => {
    try {
      const data = await api('/api/v1/events')
      if (run !== generation) return
      permissionState.items = data.permissions || []
      const next = new Map()
      for (const [kind, values] of [['alarm', data.alarms], ['state', data.devices]]) {
        for (const value of values || []) {
          const key = kind + ':' + (value.alarmId || value.deviceId)
          const payload = JSON.stringify(value)
          next.set(key, payload)
          if (previous && previous.get(key) !== payload) {
            const delivered = brokerDelivered.get(key)
            if (delivered?.payload !== payload || delivered.expiresAt <= Date.now()) onMessage?.(`/iot/${kind === 'alarm' ? 'alarm/raised' : 'device/state'}/${session.tenant}`, payload)
          }
        }
      }
      previous = next
      for (const [key, delivered] of brokerDelivered) {
        if (delivered.expiresAt <= Date.now()) brokerDelivered.delete(key)
      }
    } catch (error) {
      if (run !== generation) return
      // Keep the last successful snapshot across transient network failures.
      if (error.status === 403) { await refreshPermissions().catch(() => {}); return }
      if (error.status === 401) return
    }
    if (run === generation) pollTimer = setTimeout(poll, 3000)
  }
  void poll()
  if (session.role !== 'admin') return

  // Administrators also retain parsed events and rule UI actions via MQTT.
  const connectBroker = async () => {
    try {
      const auth = await api('/api/v1/mqtt/token', { method: 'POST' })
      if (run !== generation) return
      client?.end(true)
      // A client ID is only a connection identifier, not a credential.
      // randomUUID is unavailable on HTTP origins other than localhost.
      const id = globalThis.crypto?.randomUUID?.() || `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
      const connection = mqtt.connect(auth.websocketUrl, {
        username: auth.username,
        password: auth.token,
        clientId: `iot-web-${id}`,
        clean: true,
        reconnectPeriod: 3000,
        connectTimeout: 10000
      })
      client = connection
      connection.on('connect', () => {
        if (run === generation && client === connection) connection.subscribe(auth.subscriptions || [], { qos: 1 })
      })
      connection.on('message', (topic, payload) => {
        if (run === generation && client === connection) brokerMessage(topic, payload)
      })
      // Connection errors do not interrupt HTTP alarm delivery; MQTT retries itself.
      connection.on('error', () => {})
      brokerTimer = setTimeout(connectBroker, Math.max(60000, ((auth.expiresIn || 900) - 90) * 1000))
    } catch {
      if (run === generation) brokerTimer = setTimeout(connectBroker, 10000)
    }
  }
  await connectBroker()
}
