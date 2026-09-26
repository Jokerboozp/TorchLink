<script setup>
import { computed } from 'vue'
import { UiMessage } from '../ui/feedback.js'
const props = defineProps({ modelValue: {}, kind: String, label: String })
const emit = defineEmits(['update:modelValue'])
const entries = computed(() => Object.entries(props.modelValue || {}))
const typeOf = value => Array.isArray(value) ? 'array' : value === null ? 'null' : typeof value
const empty = kind => ({ string:'', number:0, boolean:false, null:null, object:{}, array:[] })[kind]
function set(key, value) {
  if (props.kind === 'array') { const next = [...props.modelValue]; next[Number(key)] = value; emit('update:modelValue', next) }
  else emit('update:modelValue', Object.fromEntries([...entries.value.filter(([k]) => k !== key), [key, value]]))
}
function rename(key, next) {
  if (!next || entries.value.some(([k]) => k !== key && k === next)) return UiMessage.warning('参数名称不能为空或重复')
  emit('update:modelValue', Object.fromEntries(entries.value.map(([k,v]) => [k === key ? next : k, v])))
}
function add() {
  if (props.kind === 'array') emit('update:modelValue', [...(props.modelValue || []), ''])
  else { let key = 'field'; let n = 1; while (Object.hasOwn(props.modelValue || {}, key)) key = `field${n++}`; set(key, '') }
}
function remove(key) { emit('update:modelValue', props.kind === 'array' ? props.modelValue.filter((_,i) => i !== Number(key)) : Object.fromEntries(entries.value.filter(([k]) => k !== key))) }
</script>

<template>
  <ui-input v-if="kind === 'string'" :model-value="modelValue" :aria-label="label" @update:model-value="emit('update:modelValue',$event)" />
  <ui-input-number v-else-if="kind === 'number' || kind === 'integer'" :model-value="modelValue" :precision="kind === 'integer' ? 0 : undefined" :aria-label="label" @update:model-value="emit('update:modelValue',$event)" />
  <ui-select v-else-if="kind === 'boolean'" :model-value="modelValue" :aria-label="label" clearable placeholder="请选择" @update:model-value="emit('update:modelValue', $event === '' ? null : $event)"><ui-option :value="true" label="是（true）"/><ui-option :value="false" label="否（false）"/></ui-select>
  <span v-else-if="kind === 'null'">空值</span>
  <div v-else-if="kind === 'object' || kind === 'array'" class="command-object">
    <div v-for="([key,value]) in entries" :key="key" class="command-entry">
      <div class="command-entry-heading"><ui-input v-if="kind === 'object'" :model-value="key" aria-label="参数名称" @change="rename(key,$event)"/><span v-else>第 {{Number(key)+1}} 项</span><ui-select :model-value="typeOf(value)" aria-label="参数类型" @update:model-value="set(key,empty($event))"><ui-option v-for="(name,type) in {string:'文本',number:'数值',boolean:'布尔',object:'对象',array:'列表',null:'空值'}" :key="type" :label="name" :value="type"/></ui-select><ui-button link type="danger" @click="remove(key)">删除</ui-button></div>
      <CommandValueInput :model-value="value" :kind="typeOf(value)" :label="`${label}.${key}`" @update:model-value="set(key,$event)"/>
    </div>
    <ui-button size="small" @click="add">{{kind === 'array' ? '添加列表项' : '添加参数项'}}</ui-button>
    <ui-button v-if="modelValue != null" size="small" @click="emit('update:modelValue',null)">清除</ui-button>
    <ui-button v-else size="small" @click="emit('update:modelValue',empty(kind))">设为空{{kind === 'array' ? '列表' : '对象'}}</ui-button>
  </div>
</template>
<style scoped>
.command-object { width:100%; min-width:0; border-left:2px solid var(--border); padding-left:12px; }
.command-entry { margin-bottom:12px; }
.command-entry-heading { display:flex; flex-wrap:wrap; gap:8px; margin-bottom:8px; }
.command-entry-heading > .ui-input,.command-entry-heading > .ui-select { width:140px; }
</style>
