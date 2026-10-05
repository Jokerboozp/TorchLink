import mqtt from 'mqtt'
import { api, apiIfChanged, session } from './api'
import { permissionState, refreshPermissions, applyAccessVersion } from './permissions'

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
        if (previous?.get(key) === normalized || (delivered?.payload === normalized && delivered.expiresAt > Date.now())) return
        brokerDelivered.set(key, { payload: normalized, expiresAt: Date.now() + 10000 })
        while (brokerDelivered.size > 1000) brokerDelivered.delete(brokerDelivered.keys().next().value)
      }
    } catch {
      /* Other MQTT topics do not use the event snapshot. */
    }
    onMessage?.(topic, body)
  }
  // All accounts receive authoritative alarms over the authenticated API.
  // Broker availability and token renewal must not reset this snapshot.
  let etag = ''
  // The server answers only rows changed since this cursor (delta) and falls
  // back to the full snapshot when it cannot use the cursor.
  let cursor = ''
  let polls = 0
  let overflow = false
  let deviceTotal
  const poll = async () => {
    try {
      // Deltas never list rows that left the snapshot; a periodic full
      // snapshot drops them so the comparison map stays bounded.
      if (++polls % 100 === 0) cursor = ''
      const path = cursor ? `/api/v1/events?since=${encodeURIComponent(cursor)}` : '/api/v1/events'
      const result = await apiIfChanged(path, etag)
      if (run !== generation) return
      if (!result.changed) {
        // Changes outside the bounded window still require occasional list invalidation.
        if (overflow && polls % 10 === 0) onMessage?.(`/iot/snapshot/refresh/${session.tenant}`, '{}')
        pollTimer = setTimeout(poll, 3000)
        return
      }
      etag = result.etag
      const data = result.data
      cursor = data.cursor || ''
      overflow = Boolean(data.truncated)
      applyAccessVersion(data.accessVersion)
      permissionState.items = data.permissions || []
      // Count comes from the same scoped page query. Window rotation is not a
      // new device, but population growth must still notify a large registry.
      if (data.truncated && deviceTotal !== undefined && data.deviceTotal > deviceTotal) {
        onMessage?.(`/iot/device/added/${session.tenant}`, JSON.stringify({ total: data.deviceTotal }))
      }
      deviceTotal = data.deviceTotal
      const next = data.delta && previous ? new Map(previous) : new Map()
      for (const [kind, values] of [
        ['alarm', data.alarms],
        ['state', data.devices]
      ]) {
        for (const value of values || []) {
          const key = kind + ':' + (value.alarmId || value.deviceId)
          const payload = JSON.stringify(value)
          next.set(key, payload)
          if (previous && previous.get(key) !== payload) {
            const delivered = brokerDelivered.get(key)
            // Only an untruncated snapshot proves that an absent row is new.
            const added = kind === 'state' && !previous.has(key) && !data.truncated
            if (added || delivered?.payload !== payload || delivered.expiresAt <= Date.now())
              onMessage?.(`/iot/${kind === 'alarm' ? 'alarm/raised' : 'device/state'}/${session.tenant}`, payload, { added })
          }
        }
      }
      // The snapshot is a bounded notification window. Lists remain the source
      // of truth; overflow invalidates them instead of inventing missing rows.
      if (data.truncated && previous) onMessage?.(`/iot/snapshot/refresh/${session.tenant}`, '{}')
      while (next.size > 1000) next.delete(next.keys().next().value)
      previous = next
      for (const [key, delivered] of brokerDelivered) {
        if (delivered.expiresAt <= Date.now()) brokerDelivered.delete(key)
      }
    } catch (error) {
      if (run !== generation) return
      // Keep the last successful snapshot across transient network failures.
      if (error.status === 403) {
        await refreshPermissions().catch(() => {})
        return
      }
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
