<script setup>
// 按设备名称或编号搜索并选择一台设备，值为设备编号；用于列表的设备筛选。
// 只搜索当前用户可见的设备（设备登记接口按用户设备范围返回）。
import { onBeforeUnmount, ref, watch } from 'vue'
import { NSelect } from 'naive-ui'
import { api, isAbort } from '../api'
import { useListLoader } from '../composables/useListLoader'
import { can } from '../permissions'

const props = defineProps({
  modelValue: { type: String, default: '' },
  placeholder: { type: String, default: '按设备名称或编号筛选，可直接输入编号' }
})
const emit = defineEmits(['update:modelValue', 'change'])
const options = ref([])
const loading = ref(false)
const loader = useListLoader(loading)
let timer = 0

// 没有设备读取权限时不搜索，仍可直接输入设备编号（例如尚未登记的设备）。
const searchable = () => can('GET /api/v1/device-registry')
async function search(keyword) {
  if (!searchable()) return
  try {
    const query = new URLSearchParams({ page: '1', pageSize: '20' })
    if (keyword.trim()) query.set('q', keyword.trim())
    const data = await loader.run(signal => api(`/api/v1/device-registry?${query}`, { signal }))
    const found = (data.items || []).map(row => ({
      label: `${row.device?.name || row.device?.id}（${row.device?.id}）`,
      value: row.device?.id
    }))
    // 已选中的设备不在本次结果中时仍保留，避免显示成裸编号。
    const selected = options.value.find(option => option.value === props.modelValue)
    options.value = selected && !found.some(option => option.value === selected.value) ? [selected, ...found] : found
  } catch (error) {
    if (!isAbort(error)) options.value = options.value.filter(option => option.value === props.modelValue)
  }
}
function onSearch(keyword) {
  window.clearTimeout(timer)
  timer = window.setTimeout(() => void search(keyword), 250)
}
function update(value) {
  const next = value || ''
  emit('update:modelValue', next)
  emit('change', next)
}
// 外部带入的设备编号（如从设备页跳转）先显示编号，再查一次名称。
watch(
  () => props.modelValue,
  value => {
    if (value && !options.value.some(option => option.value === value)) {
      options.value = [{ label: value, value }, ...options.value]
      void search(value)
    }
  },
  { immediate: true }
)
onBeforeUnmount(() => {
  window.clearTimeout(timer)
  loader.cancel()
})
</script>

<template>
  <NSelect
    class="device-filter-select"
    :value="modelValue || null"
    :options="options"
    :loading="loading"
    :placeholder="placeholder"
    filterable
    remote
    clearable
    tag
    aria-label="设备筛选"
    @search="onSearch"
    @focus="options.length <= 1 && search('')"
    @update:value="update"
  />
</template>

<style scoped>
.device-filter-select {
  min-width: 220px;
}
</style>
