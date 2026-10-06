import { defineStore } from 'pinia'
import { ref } from 'vue'

export type RealtimeState = 'idle' | 'connecting' | 'ok' | 'retrying' | 'stopped'

/**
 * Status of the realtime channel (event polling and the MQTT broker) for the
 * shell: connecting on the first attempt, ok, retrying with backoff after
 * consecutive failures, or stopped. realtime.ts drives it.
 */
export const useRealtimeStore = defineStore('realtime', () => {
  const state = ref<RealtimeState>('idle')
  const failures = ref(0)
  /** Time of the last successful poll or broker message (Unix ms). */
  const lastOk = ref(0)
  function reset() {
    state.value = 'idle'
    failures.value = 0
    lastOk.value = 0
  }
  return { state, failures, lastOk, reset }
})
