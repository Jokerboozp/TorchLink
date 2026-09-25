<script setup>
// 键值对编辑，用于规则的标签与注释。
import { Plus, X } from '@lucide/vue'

const props = defineProps({ modelValue: { type: Object, default: () => ({}) }, keyPlaceholder: { type: String, default: '名称' }, valuePlaceholder: { type: String, default: '值' }, multiline: { type: Boolean, default: false }, addText: { type: String, default: '添加' } })
const emit = defineEmits(['update:modelValue'])
const entries = () => Object.entries(props.modelValue || {})
function set(index, key, value) {
  const list = entries()
  list[index] = [key, value]
  emit('update:modelValue', Object.fromEntries(list))
}
function add() {
  let key = 'key'
  for (let i = 1; key in (props.modelValue || {}); i++) key = `key${i}`
  emit('update:modelValue', { ...(props.modelValue || {}), [key]: '' })
}
function remove(index) { emit('update:modelValue', Object.fromEntries(entries().filter((_, i) => i !== index))) }
</script>

<template>
  <div class="kv">
    <div v-for="([key, value], index) in entries()" :key="index" class="kv-row">
      <ui-input :model-value="key" size="small" :placeholder="keyPlaceholder" @update:model-value="k => set(index, k, value)" />
      <ui-input :model-value="value" size="small" :type="multiline ? 'textarea' : 'text'" :autosize="multiline ? { minRows: 1, maxRows: 4 } : undefined" :placeholder="valuePlaceholder" @update:model-value="v => set(index, key, v)" />
      <ui-button text size="small" aria-label="删除" @click="remove(index)"><X /></ui-button>
    </div>
    <ui-button size="small" text type="primary" @click="add"><Plus />{{ addText }}</ui-button>
  </div>
</template>

<style scoped>
.kv { display: grid; gap: var(--space-2); }
.kv-row { display: grid; grid-template-columns: minmax(100px, 0.6fr) minmax(0, 1.4fr) auto; align-items: start; gap: var(--space-2); }
</style>
