import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { LiveVideoStatus } from '../types/api.ts'

/** A reported module status, or unknown when it could not be read. */
export type LiveModuleStatus = LiveVideoStatus | { state: 'unknown'; message: string }

/**
 * The live video module's status and the viewer's rights, cached per tenant
 * and user in memory only (never in browser storage) and cleared on sign-out.
 * liveVideo.ts loads it.
 */
export const useLiveVideoStore = defineStore('liveVideo', () => {
  /** Tenant and user the cached status belongs to. */
  const key = ref('')
  const status = ref<LiveModuleStatus | null>(null)
  const canWatch = ref(false)
  const canManageModule = ref(false)
  const loading = ref(false)
  function reset() {
    key.value = ''
    status.value = null
    canWatch.value = false
    canManageModule.value = false
    loading.value = false
  }
  return { key, status, canWatch, canManageModule, loading, reset }
})
