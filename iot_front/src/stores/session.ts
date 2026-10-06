import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { api, session as storage, type LoginResult } from '../api.ts'

/** Who is signed in. Empty strings when signed out. */
export interface Identity {
  tenant: string
  user: string
  role: string
  /** Names of a platform user's roles; empty for the built-in administrator. */
  roleNames: string[]
}

function savedPermissions(): string[] | null {
  try {
    const value = JSON.parse(localStorage.getItem('iot_permissions') || 'null')
    return Array.isArray(value) ? value : null
  } catch {
    return null
  }
}

// Clean-up of state kept outside the store (live video cache, AI
// conversation, alert queue) registers here and runs on sign-out.
const resetHandlers = new Set<() => void>()

/**
 * The signed-in session: identity, effective permissions and their access
 * version. The token itself stays in localStorage (api.ts reads it per
 * request), which also lets other tabs observe sign-in and sign-out.
 */
export const useSessionStore = defineStore('session', () => {
  const authenticated = ref(Boolean(storage.token))
  const identity = ref<Identity>({ tenant: storage.tenant, user: storage.user, role: storage.role, roleNames: [] })
  const items = ref<string[]>(savedPermissions() || (storage.role === 'admin' ? ['*'] : []))
  /** True once permissions were confirmed by the server for this session. */
  const ready = ref(false)
  const accessVersion = ref(storage.accessVersion)
  /** Shown for troubleshooting; from /api/v1/auth/me and the login response. */
  const platformVersion = ref('')

  const isAdmin = computed(() => identity.value.role === 'admin')

  function can(permission: string | string[]): boolean {
    if (items.value.includes('*')) return true
    return Array.isArray(permission) ? permission.some(can) : items.value.includes(permission)
  }

  function applyAccessVersion(value = '') {
    localStorage.setItem('iot_access_version', value)
    accessVersion.value = value
  }

  function setPermissions(values: string[]) {
    items.value = values
    ready.value = true
    localStorage.setItem('iot_permissions', JSON.stringify(values))
  }

  /** Starts a session from a login or password-change response. */
  function start(data: LoginResult & { platformVersion?: string }, username: string) {
    storage.save(data, username)
    identity.value = { tenant: data.tenantId || '', user: username, role: data.role || '', roleNames: data.roleNames || [] }
    platformVersion.value = data.platformVersion || ''
    authenticated.value = true
    accessVersion.value = String(data.accessVersion || '')
    setPermissions(data.permissions || [])
  }

  /** Reloads permissions and the platform version from the server. */
  async function refresh() {
    const me = await api('/api/v1/auth/me')
    applyAccessVersion(String(me.accessVersion ?? ''))
    setPermissions(me.permissions || [])
    identity.value = { ...identity.value, roleNames: Array.isArray(me.roleNames) ? me.roleNames : [] }
    platformVersion.value = me.platformVersion || ''
    return me
  }

  /** Signs out locally: clears storage, the store and every registered module state. */
  function signOut() {
    for (const reset of resetHandlers) reset()
    storage.clear()
    authenticated.value = false
    identity.value = { tenant: '', user: '', role: '', roleNames: [] }
    items.value = []
    ready.value = false
    accessVersion.value = ''
    platformVersion.value = ''
  }

  return {
    authenticated,
    identity,
    items,
    ready,
    accessVersion,
    platformVersion,
    isAdmin,
    can,
    applyAccessVersion,
    setPermissions,
    start,
    refresh,
    signOut
  }
})

/** Registers clean-up that must run when the session ends; returns its removal. */
export function onSessionReset(reset: () => void): () => void {
  resetHandlers.add(reset)
  return () => resetHandlers.delete(reset)
}
