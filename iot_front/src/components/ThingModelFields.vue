<script setup>
defineProps({rows:{type:Array,required:true},properties:Boolean})
const emit=defineEmits(['add','remove'])
const types=[{value:'number',label:'数值'},{value:'integer',label:'整数'},{value:'string',label:'文本'},{value:'boolean',label:'布尔值'},{value:'object',label:'JSON 对象'},{value:'array',label:'数组'}]
</script>
<template>
  <div class="model-fields">
    <section v-for="(field,index) in rows" :key="index" class="model-field">
      <div class="model-field-grid"><ui-form-item label="字段标识" required><ui-input v-model="field.identifier" maxlength="128" placeholder="与解析结果一致，例如 temperature"/></ui-form-item><ui-form-item label="显示名称"><ui-input v-model="field.name" placeholder="例如 温度"/></ui-form-item><ui-form-item label="数据类型"><ui-select v-model="field.dataType"><ui-option v-for="item in types" :key="item.value" :value="item.value" :label="item.label"/></ui-select></ui-form-item><ui-form-item label="单位"><ui-input v-model="field.unit" placeholder="例如 ℃"/></ui-form-item></div>
      <div class="model-field-options"><label><ui-switch v-model="field.required"/> 必填</label><label v-if="properties"><ui-switch v-model="field.writable"/> 可写属性</label><ui-button size="small" @click="emit('remove',index)">移除字段</ui-button></div>
    </section>
    <ui-button size="small" :disabled="rows.length>=256" @click="emit('add')">添加字段</ui-button>
  </div>
</template>
<style scoped>
.model-fields {display:grid;gap:var(--space-3)}.model-field {padding:var(--space-3);border:1px solid var(--border);border-radius:var(--radius-md)}.model-field-grid {display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:0 var(--space-3)}.model-field-options {display:flex;flex-wrap:wrap;gap:var(--space-3);align-items:center}.model-field-options label{display:flex;gap:var(--space-2);align-items:center;font-size:var(--font-size-sm)}@media(max-width:767px){.model-field-grid{grid-template-columns:1fr}}
</style>
