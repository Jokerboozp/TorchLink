<script setup>
// 标签匹配条件编辑：名称可从已有标签中选择，值由调用方按需加载候选项。
import { ref } from 'vue'
import { Plus, X } from '@lucide/vue'

const props = defineProps({
  modelValue: { type: Array, default: () => [] },
  labels: { type: Array, default: () => [] },
  loadValues: { type: Function, default: null },
  addText: { type: String, default: '添加标签条件' },
  max: { type: Number, default: 10 }
})
const emit = defineEmits(['update:modelValue'])
const values = ref({})
const ops = [{ value: '=', label: '等于' }, { value: '!=', label: '不等于' }, { value: '=~', label: '正则匹配' }, { value: '!~', label: '正则不匹配' }]

function update(index, patch) {
  const next = props.modelValue.map((item, i) => (i === index ? { ...item, ...patch } : item))
  emit('update:modelValue', next)
  if (patch.name) loadFor(patch.name)
}
async function loadFor(name) {
  if (!props.loadValues || !name || values.value[name]) return
  try { values.value = { ...values.value, [name]: await props.loadValues(name) } } catch { values.value = { ...values.value, [name]: [] } }
}
function add() { emit('update:modelValue', [...props.modelValue, { name: '', op: '=', value: '' }]) }
function remove(index) { emit('update:modelValue', props.modelValue.filter((_, i) => i !== index)) }
</script>

<template>
  <div class="matchers">
    <div v-for="(item, index) in modelValue" :key="index" class="matcher-row">
      <ui-select :model-value="item.name" filterable allow-create size="small" placeholder="标签" class="matcher-row__name" @update:model-value="value => update(index, { name: value })">
        <ui-option v-for="label in labels" :key="label" :value="label" :label="label" />
      </ui-select>
      <ui-select :model-value="item.op" size="small" class="matcher-row__op" @update:model-value="value => update(index, { op: value })">
        <ui-option v-for="op in ops" :key="op.value" :value="op.value" :label="op.label" />
      </ui-select>
      <ui-select v-if="(values[item.name] || []).length && !item.op.includes('~')" :model-value="item.value" filterable allow-create size="small" placeholder="值" class="matcher-row__value" @update:model-value="value => update(index, { value })" @focus="loadFor(item.name)">
        <ui-option v-for="value in values[item.name]" :key="value" :value="value" :label="value" />
      </ui-select>
      <ui-input v-else :model-value="item.value" size="small" :placeholder="item.op.includes('~') ? '正则表达式' : '值'" class="matcher-row__value" @update:model-value="value => update(index, { value })" @focus="loadFor(item.name)" />
      <ui-button text size="small" aria-label="删除条件" @click="remove(index)"><X /></ui-button>
    </div>
    <ui-button v-if="modelValue.length < max" size="small" text type="primary" @click="add"><Plus />{{ addText }}</ui-button>
  </div>
</template>

<style scoped>
.matchers { display: grid; gap: var(--space-2); min-width: 0; }
.matcher-row { display: grid; grid-template-columns: minmax(110px, 1fr) 110px minmax(120px, 1.4fr) auto; align-items: center; gap: var(--space-2); }
@media (max-width: 767px) {
  .matcher-row { grid-template-columns: minmax(0, 1fr) 96px; }
  .matcher-row__value { grid-column: 1 / 2; }
}
</style>
