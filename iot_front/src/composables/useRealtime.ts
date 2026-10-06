import { onBeforeUnmount, onMounted } from 'vue'

// 实时事件：App 把轮询或 MQTT 收到的消息按主题分类后以 iot:realtime 派发。
// kind 是事件类别，data 是解析后的消息体（无法解析时为 null），topic 与 payload 保留原文。

export type RealtimeKind = 'alarm' | 'state' | 'deviceAdded' | 'refresh' | 'uiAction' | 'parsed' | 'other'

export interface RealtimeEvent {
  kind: RealtimeKind
  topic: string
  payload: string
  data: any
  /** The device the event is about, when the message names one. */
  deviceId: string
  /** A device state row that appeared for the first time. */
  added: boolean
}

export function realtimeKind(topic: string): RealtimeKind {
  if (topic.includes('/alarm/')) return 'alarm'
  if (topic.includes('/device/state/')) return 'state'
  if (topic.includes('/device/added/')) return 'deviceAdded'
  if (topic.includes('/snapshot/refresh/')) return 'refresh'
  if (topic.includes('/ui-action/')) return 'uiAction'
  if (topic.includes('/parsed/')) return 'parsed'
  return 'other'
}

export function toRealtimeEvent(topic: string, payload: unknown, added = false): RealtimeEvent {
  const text = typeof payload === 'string' ? payload : JSON.stringify(payload ?? '')
  let data: any
  try {
    data = typeof payload === 'string' ? JSON.parse(payload) : (payload ?? null)
  } catch {
    data = null
  }
  const deviceId = data && typeof data === 'object' && typeof data.deviceId === 'string' ? data.deviceId : ''
  return { kind: realtimeKind(topic), topic, payload: text, data, deviceId, added: Boolean(added) }
}

/**
 * Calls handler with realtime events of the given kinds while the component
 * is mounted. With debounce (milliseconds) events are collected and handed
 * over together once the stream pauses; a handler that returns false is
 * called again after another debounce period (for example while a list is
 * still loading).
 */
export function useRealtime(
  kinds: RealtimeKind[],
  handler: (events: RealtimeEvent[]) => unknown,
  { debounce = 0 }: { debounce?: number } = {}
) {
  let pending: RealtimeEvent[] = []
  let timer: ReturnType<typeof setTimeout> | 0 = 0
  let active = false
  const flush = () => {
    timer = 0
    if (!active) return
    const events = pending
    pending = []
    if (handler(events) === false && active) {
      pending = events.concat(pending)
      timer = setTimeout(flush, debounce)
    }
  }
  const listener = (event: Event) => {
    const detail = (event as CustomEvent<RealtimeEvent>).detail
    if (!detail || !kinds.includes(detail.kind)) return
    if (!debounce) {
      handler([detail])
      return
    }
    pending.push(detail)
    if (!timer) timer = setTimeout(flush, debounce)
  }
  onMounted(() => {
    active = true
    window.addEventListener('iot:realtime', listener)
  })
  onBeforeUnmount(() => {
    active = false
    window.removeEventListener('iot:realtime', listener)
    if (timer) clearTimeout(timer)
    pending = []
  })
}
