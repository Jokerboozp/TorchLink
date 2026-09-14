import mqtt from 'mqtt'
import { api, session } from './api'
import { permissionState, refreshPermissions } from './permissions'

let client
let refreshTimer
let generation = 0

export function stopRealtime() {
  generation++
  clearTimeout(refreshTimer)
  client?.end(true)
  client = undefined
}

export async function startRealtime(onMessage) {
  stopRealtime()
  const run = generation
  if (session.role !== 'admin') {
    let previous = null
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
            if (previous && previous.get(key) !== payload) onMessage?.(`/iot/${kind === 'alarm' ? 'alarm/raised' : 'device/state'}/${session.tenant}`, payload)
          }
        }
        previous = next
      } catch (error) {
        if (run !== generation) return
        previous = null
        if (error.status === 403) { await refreshPermissions().catch(() => {}); return }
        if (error.status === 401) return
      }
      if (run === generation) refreshTimer = setTimeout(poll, 3000)
    }
    void poll()
    return
  }
  try {
    const auth = await api('/api/v1/mqtt/token', { method: 'POST' })
    if (run !== generation) return
    client = mqtt.connect(auth.websocketUrl, {
      username: auth.username,
      password: auth.token,
      clientId: `iot-web-${crypto.randomUUID()}`,
      clean: true,
      reconnectPeriod: 3000,
      connectTimeout: 10000
    })
    client.on('connect', () => client.subscribe(auth.subscriptions || [], { qos: 1 }))
    client.on('message', (topic, payload) => onMessage?.(topic, payload.toString()))
    refreshTimer = setTimeout(() => startRealtime(onMessage), Math.max(60000, ((auth.expiresIn || 900) - 90) * 1000))
  } catch {
    if (run === generation) refreshTimer = setTimeout(() => startRealtime(onMessage), 10000)
  }
}
