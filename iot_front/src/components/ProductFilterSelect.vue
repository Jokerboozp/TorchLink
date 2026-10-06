<script setup>
// 按设备模板名称或标识搜索并选择一个模板，值为模板标识；用于列表筛选和表单中的模板选择。
// 服务端按关键字检索（最多 20 条），不预先加载全部模板。custom 为 false 时只能选择检索到的模板。
import { onBeforeUnmount, ref, watch } from 'vue'
import { NSelect } from 'naive-ui'
import { api, isAbort } from '../api'
import { useListLoader } from '../composables/useListLoader'
import { can } from '../permissions'

const props = defineProps({
  modelValue: { type: String, default: '' },
  placeholder: { type: String, default: '按模板名称或标识筛选，可直接输入标识' },
  custom: { type: Boolean, default: true }
})
const emit = defineEmits(['update:modelValue', 'change'])
const options = ref([])
const loading = ref(false)
const loader = useListLoader(loading)
let timer = 0

// 没有模板读取权限时不搜索，仍可直接输入模板标识。
const searchable = () => can('GET /api/v1/products')
async function search(keyword) {
  if (!searchable()) return
  try {
    const query = new URLSearchParams({ page: '1', pageSize: '20' })
    if (keyword.trim()) query.set('q', keyword.trim())
    const data = await loader.run(signal => api(`/api/v1/products?${query}`, { signal }))
    const found = (data.items || []).map(row => ({
      label: `${row.name || row.id}（${row.id}）`,
      value: row.id
    }))
    // 已选中的模板不在本次结果中时仍保留，避免显示成裸标识。
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
// 外部带入的模板标识先显示标识，再查一次名称。
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
    class="product-filter-select"
    :value="modelValue || null"
    :options="options"
    :loading="loading"
    :placeholder="placeholder"
    filterable
    remote
    clearable
    :tag="custom"
    aria-label="设备模板筛选"
    @search="onSearch"
    @focus="options.length <= 1 && search('')"
    @update:value="update"
  />
</template>

<style scoped>
.product-filter-select {
  min-width: 220px;
}
</style>
