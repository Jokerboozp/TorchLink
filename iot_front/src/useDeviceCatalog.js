import { inject, onBeforeUnmount, provide, watch } from 'vue'
import { api, session } from './api.js'
import { permissionState } from './permissions.js'
import { createDeviceCatalog } from './deviceCatalog.js'

export const deviceCatalogKey = Symbol('device-catalog')
export function useDeviceLabels(selectedIds) {
  const catalog = inject(deviceCatalogKey, null)
  if (catalog) watch(selectedIds, ids => { void catalog.ensure(ids.filter(Boolean)).catch(() => {}) }, { immediate: true })
}
export function provideDeviceCatalog(rows, selectedIds = () => []) {
  const catalog = createDeviceCatalog(api, rows)
  provide(deviceCatalogKey, catalog)
  watch(() => [session.tenant, session.user, permissionState.accessVersion], catalog.clear)
  watch(selectedIds, ids => { void catalog.ensure(ids.flat().filter(Boolean)).catch(() => {}) }, { immediate: true, deep: true })
  onBeforeUnmount(catalog.clear)
  return catalog
}
