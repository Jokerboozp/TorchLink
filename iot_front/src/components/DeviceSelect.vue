<script setup>
import { computed, inject, onBeforeUnmount, ref, watch } from 'vue'
import { api, session } from '../api.js'
import { permissionState } from '../permissions.js'
import { createDeviceCatalog } from '../deviceCatalog.js'
import { deviceCatalogKey } from '../useDeviceCatalog.js'
const props = defineProps({ modelValue: [String, Array], multiple: Boolean, disabled: Boolean, placeholder: String, clearable: Boolean })
const emit = defineEmits(['update:modelValue', 'change'])
const sharedCatalog = inject(deviceCatalogKey, null)
const catalog = sharedCatalog || createDeviceCatalog(api, ref([]))
const items = ref([]), loading = ref(false), error = ref(''), total = ref(0)
const selectedIds = computed(() => (Array.isArray(props.modelValue) ? props.modelValue : [props.modelValue]).filter(Boolean))
const options = computed(() => {
  const selected = selectedIds.value.map(id => catalog.rows.value.find(row => row.id === id) || { id })
  return [...new Map([...selected, ...items.value].map(row => [row.id, row])).values()]
})
let controller, timer, generation = 0, selectionGeneration = 0, page = 0, query = '', disposed = false
function cancel() { generation++; controller?.abort(); clearTimeout(timer); loading.value = false }
async function load(append = false) {
  if (disposed || (append && (loading.value || items.value.length >= total.value))) return
  if (!append) { cancel(); page = 0; items.value = [] }
  const token = generation, nextPage = page + 1
  controller = new AbortController(); loading.value = true; error.value = ''
  try {
    const data = await api('/api/v1/device-registry?' + new URLSearchParams({ page: nextPage, pageSize: 50, q: query }), { signal: controller.signal })
    if (disposed || token !== generation) return
    const rows = (data.items || []).map(row => row.device || row).filter(row => row.id)
    catalog.remember(data.items || []); items.value = append ? [...items.value, ...rows] : rows
    page = nextPage; total.value = data.total ?? items.value.length
  } catch (cause) { if (!disposed && token === generation && cause.name !== 'AbortError') error.value = cause.message }
  finally { if (token === generation) loading.value = false }
}
function search(value) { query = value; cancel(); timer = setTimeout(() => load(), 250) }
function opened(show) { if (show && !page && !loading.value) void load() }
function scroll(event) { const el = event.target; if (el.scrollTop + el.clientHeight >= el.scrollHeight - 40) void load(true) }
function changed(value) { emit('update:modelValue', value); emit('change', value) }
watch(selectedIds, ids => { const token = ++selectionGeneration; void catalog.ensure(ids).catch(cause => { if (!disposed && token === selectionGeneration) error.value = cause.message }) }, { immediate: true })
watch(() => [session.tenant, session.user, permissionState.accessVersion], () => { selectionGeneration++; cancel(); catalog.clear(); items.value = []; total.value = 0; page = 0; query = ''; error.value = '' })
onBeforeUnmount(() => { disposed = true; selectionGeneration++; cancel(); if (!sharedCatalog) catalog.clear() })
</script>
<template><div class="device-select"><ui-select :model-value="modelValue" :multiple="multiple" :disabled="disabled" :clearable="clearable" :placeholder="placeholder || '搜索设备名称或编号'" filterable remote :loading="loading" @search="search" @update:show="opened" @scroll="scroll" @update:model-value="changed"><ui-option v-for="row in options" :key="row.id" :value="row.id" :label="row.name || row.id"/></ui-select><p v-if="error" class="device-select-error">{{error}} <ui-button text size="small" @click="load()">重试</ui-button></p></div></template>
<style scoped>.device-select{width:100%;min-width:0;flex:1}.device-select-error{font-size:12px;color:var(--danger);margin:4px 0}</style>
