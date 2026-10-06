import { api, apiIfChanged, session } from './api'
import { permissionState, refreshPermissions, applyAccessVersion } from './permissions'
import type { EventSnapshot } from './types/api.ts'
import { pinia } from './stores/index.ts'
import { useRealtimeStore } from './stores/realtime.ts'
import type { MqttClient } from 'mqtt'

/** Receives each changed alarm or device state as an MQTT-style topic and JSON body. */
export type RealtimeHandler = (topic: string, body: string, meta?: { added: boolean }) => void

// 轮询间隔：页面可见 3 秒，后台 15 秒；连续失败按倍数退避，最长 60 秒。
const POLL_MS = 3000
const HIDDEN_POLL_MS = 15000
const MAX_BACKOFF_MS = 60000
// MQTT 凭据续签失败按 10 秒起倍数退避，最长 5 分钟；4xx 表示无权或配置问题，不再重试。
const BROKER_RETRY_MS = 10000
const MAX_BROKER_RETRY_MS = 300000

let client: MqttClient | undefined
let pollTimer: ReturnType<typeof setTimeout> | undefined
let brokerTimer: ReturnType<typeof setTimeout> | undefined
let generation = 0
let wakePoll: (() => void) | null = null

// 实时通道状态供外壳显示：connecting 首次连接中，ok 正常，retrying 连续失败正在退避重试，stopped 已停止。
// 状态保存在 Pinia 仓库，字段可直接读写。
export const realtimeStatus = useRealtimeStore(pinia)

// 立即重试一次，不等退避间隔。
export function retryRealtime() {
  wakePoll?.()
}

const pageHidden = () => Boolean(globalThis.document?.hidden)
function onVisibility() {
  if (!pageHidden()) wakePoll?.()
}

export function stopRealtime() {
  generation++
  clearTimeout(pollTimer)
  clearTimeout(brokerTimer)
  globalThis.document?.removeEventListener?.('visibilitychange', onVisibility)
  wakePoll = null
  client?.end(true)
  client = undefined
  realtimeStatus.reset()
}

export async function startRealtime(onMessage?: RealtimeHandler) {
  stopRealtime()
  const run = generation
  let previous: Map<string, string> | null = null
  const brokerDelivered = new Map<string, { payload: string; expiresAt: number }>()
  const brokerMessage = (topic: string, payload: { toString(): string }) => {
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
        while (brokerDelivered.size > 1000) brokerDelivered.delete(brokerDelivered.keys().next().value as string)
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
  let deviceTotal: number | undefined
  let failures = 0
  let polling = false
  const schedule = () => {
    if (run !== generation) return
    const base = pageHidden() ? HIDDEN_POLL_MS : POLL_MS
    pollTimer = setTimeout(poll, failures ? Math.min(base * 2 ** failures, MAX_BACKOFF_MS) : base)
  }
  // 回到页面时立即刷新一次，不等后台的长间隔或失败退避。
  wakePoll = () => {
    if (polling || run !== generation) return
    clearTimeout(pollTimer)
    void poll()
  }
  globalThis.document?.addEventListener?.('visibilitychange', onVisibility)
  realtimeStatus.state = 'connecting'
  const poll = async () => {
    polling = true
    try {
      // Deltas never list rows that left the snapshot; a periodic full
      // snapshot drops them so the comparison map stays bounded.
      if (++polls % 100 === 0) cursor = ''
      const path = cursor ? `/api/v1/events?since=${encodeURIComponent(cursor)}` : '/api/v1/events'
      const result = await apiIfChanged<EventSnapshot>(path, etag)
      if (run !== generation) return
      failures = 0
      Object.assign(realtimeStatus, { state: 'ok', failures: 0, lastOk: Date.now() })
      if (!result.changed) {
        // Changes outside the bounded window still require occasional list invalidation.
        if (overflow && polls % 10 === 0) onMessage?.(`/iot/snapshot/refresh/${session.tenant}`, '{}')
      } else {
        etag = result.etag
        const data = result.data as EventSnapshot
        cursor = data.cursor || ''
        overflow = Boolean(data.truncated)
        applyAccessVersion(String(data.accessVersion ?? ''))
        permissionState.items = data.permissions || []
        // Count comes from the same scoped page query. Window rotation is not a
        // new device, but population growth must still notify a large registry.
        if (data.truncated && deviceTotal !== undefined && data.deviceTotal > deviceTotal) {
          onMessage?.(`/iot/device/added/${session.tenant}`, JSON.stringify({ total: data.deviceTotal }))
        }
        deviceTotal = data.deviceTotal
        const next = data.delta && previous ? new Map(previous) : new Map<string, string>()
        for (const [kind, values] of [
          ['alarm', data.alarms],
          ['state', data.devices]
        ] as const) {
          for (const value of values || []) {
            const key = kind + ':' + (value.alarmId || value.deviceId)
            const payload = JSON.stringify(value)
            next.set(key, payload)
            if (previous && previous.get(key) !== payload) {
              const delivered = brokerDelivered.get(key)
              // Only an untruncated snapshot proves that an absent row is new.
              const added = kind === 'state' && !previous.has(key) && !data.truncated
              if (added || delivered?.payload !== payload || delivered!.expiresAt <= Date.now())
                onMessage?.(`/iot/${kind === 'alarm' ? 'alarm/raised' : 'device/state'}/${session.tenant}`, payload, { added })
            }
          }
        }
        // The snapshot is a bounded notification window. Lists remain the source
        // of truth; overflow invalidates them instead of inventing missing rows.
        if (data.truncated && previous) onMessage?.(`/iot/snapshot/refresh/${session.tenant}`, '{}')
        while (next.size > 1000) next.delete(next.keys().next().value as string)
        previous = next
        for (const [key, delivered] of brokerDelivered) {
          if (delivered.expiresAt <= Date.now()) brokerDelivered.delete(key)
        }
      }
    } catch (error: any) {
      if (run !== generation) return
      // Keep the last successful snapshot across transient network failures.
      // 403 usually means the account's permissions just changed. Refresh them
      // and keep polling with backoff: the shell stops this loop when the
      // account no longer has any realtime menu, so a stall here would
      // silently drop alarm popups for the rest of the session.
      if (error.status === 403) await refreshPermissions().catch(() => {})
      if (run !== generation) return
      else if (error.status === 401) {
        polling = false
        realtimeStatus.state = 'stopped'
        return
      }
      failures++
      Object.assign(realtimeStatus, { state: 'retrying', failures })
    }
    polling = false
    schedule()
  }
  void poll()
  if (session.role !== 'admin') return

  // Administrators also retain parsed events and rule UI actions via MQTT.
  let brokerFailures = 0
  const connectBroker = async () => {
    try {
      const auth = await api('/api/v1/mqtt/token', { method: 'POST' })
      if (run !== generation) return
      // MQTT 客户端只有管理员需要，按需加载，不进入首屏包。
      const { default: mqttLib } = await import('mqtt')
      if (run !== generation) return
      client?.end(true)
      // A client ID is only a connection identifier, not a credential.
      // randomUUID is unavailable on HTTP origins other than localhost.
      const id = globalThis.crypto?.randomUUID?.() || `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
      const connection = mqttLib.connect(auth.websocketUrl, {
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
      connection.on('message', (topic: string, payload: { toString(): string }) => {
        if (run === generation && client === connection) brokerMessage(topic, payload)
      })
      // Connection errors do not interrupt HTTP alarm delivery; MQTT retries itself.
      connection.on('error', () => {})
      brokerFailures = 0
      brokerTimer = setTimeout(connectBroker, Math.max(60000, ((auth.expiresIn || 900) - 90) * 1000))
    } catch (error: any) {
      if (run !== generation) return
      // The old credential expires soon; a client kept on it would only be
      // disconnected by the broker and retry with a stale token.
      client?.end(true)
      client = undefined
      const status = Number(error?.status || 0)
      if (status >= 400 && status < 500 && status !== 408 && status !== 429) return
      brokerTimer = setTimeout(connectBroker, Math.min(BROKER_RETRY_MS * 2 ** brokerFailures++, MAX_BROKER_RETRY_MS))
    }
  }
  await connectBroker()
}
