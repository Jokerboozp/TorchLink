import { computed, inject, onBeforeUnmount, ref, watch } from 'vue'
import { apiAll, session } from './api.js'
import { permissionState } from './permissions.js'
import { deviceCatalogKey } from './useDeviceCatalog.js'

// Whole product/tenant scopes are explicit form actions, independent of the visible picker cache.
export function useProfileTargets(form, dialog, error, knownDevices) {
  const catalog = inject(deviceCatalogKey, null), productRows = ref([]), targetLoading = ref(false), progress = ref({ read: 0, total: null })
  const products = computed(() => [...new Map([
    ...knownDevices().filter(row => row.productId).map(row => ({ id: row.productId, name: row.productName || row.productId })),
    ...(form.productId ? [{ id: form.productId, name: productRows.value.find(row => row.id === form.productId)?.name || form.productId }] : []),
    ...productRows.value,
  ].map(row => [row.id, row])).values()])
  let controller, productController, generation = 0, productGeneration = 0, disposed = false
  function cancel() { generation++; productGeneration++; controller?.abort(); productController?.abort(); targetLoading.value = false }
  async function loadProducts(show = true) {
    if (!show || productRows.value.length || disposed) return
    productController?.abort(); productController = new AbortController(); const token = ++productGeneration
    try { const value = await apiAll('/api/v1/products', { signal: productController.signal }); if (!disposed && token === productGeneration) productRows.value = value.items || [] }
    catch (cause) { if (!disposed && token === productGeneration && cause.name !== 'AbortError') error.value = cause.message }
  }
  async function targetChanged() {
    cancel()
    if (form.targetType === 'DEVICE') { form.productId = ''; return }
    form.deviceIds = []
    if (form.targetType === 'PRODUCT' && !form.productId) return
    const token = generation; controller = new AbortController(); targetLoading.value = true; error.value = ''; progress.value = { read: 0, total: null }
    const query = form.targetType === 'PRODUCT' ? '?' + new URLSearchParams({ productId: form.productId }) : ''
    try {
      const value = await apiAll('/api/v1/device-registry' + query, { signal: controller.signal, maxItems: 1000, onProgress: (read, total) => { if (!disposed && token === generation) progress.value = { read, total } } })
      if (disposed || token !== generation) return
      const rows = (value.items || []).map(row => row.device || row)
      catalog?.remember(value.items || []); form.deviceIds = rows.map(row => row.id)
      if (!form.deviceIds.length) error.value = '所选目标没有获授权的设备，请重新选择产品或设备'
    } catch (cause) { if (!disposed && token === generation && cause.name !== 'AbortError') error.value = cause.message }
    finally { if (token === generation) targetLoading.value = false }
  }
  watch(dialog, open => { if (!open) cancel() })
  watch(() => [session.tenant, session.user, permissionState.accessVersion], () => { cancel(); productRows.value = [] })
  onBeforeUnmount(() => { disposed = true; cancel() })
  return { products, targetLoading, progress, targetChanged, loadProducts, cancel }
}
