import { watchEffect } from 'vue'
import type { Directive } from 'vue'
import { pinia } from './stores/index.ts'
import { useSessionStore } from './stores/session.ts'

// Permission checks for components and modules; the state lives in the
// session store. permissionState is the store itself, so items, ready and
// accessVersion stay reactive for existing callers.
export const permissionState = useSessionStore(pinia)

export function can(permission: string | string[]): boolean {
  return permissionState.can(permission)
}

export async function refreshPermissions() {
  return permissionState.refresh()
}

export function applyAccessVersion(value = '') {
  permissionState.applyAccessVersion(value)
}

type PermissionElement = HTMLElement & { __permissionValue?: string | string[]; __permissionStop?: () => void }

/** v-permission: hides the element unless the user holds the permission. */
export const permissionDirective: Directive<PermissionElement, string | string[]> = {
  mounted(el, binding) {
    el.__permissionValue = binding.value
    el.__permissionStop = watchEffect(() => {
      el.style.display = can(el.__permissionValue ?? []) ? '' : 'none'
      el.setAttribute('data-permission', JSON.stringify(el.__permissionValue))
    })
  },
  updated(el, binding) {
    el.__permissionValue = binding.value
    el.style.display = can(binding.value) ? '' : 'none'
  },
  unmounted(el) {
    el.__permissionStop?.()
  }
}
