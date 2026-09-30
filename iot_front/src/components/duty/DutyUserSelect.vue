<script setup>
defineProps({modelValue:[String,Array],users:{type:Array,default:()=>[]},multiple:Boolean,disabled:Boolean,placeholder:{type:String,default:'选择人员'}})
const emit=defineEmits(['update:modelValue','change'])
</script>
<template><ui-select :model-value="modelValue" :multiple="multiple" filterable clearable :disabled="disabled" :placeholder="placeholder" @update:model-value="value=>emit('update:modelValue',value)" @change="value=>emit('change',value)"><ui-option v-for="user in users" :key="user.username" :value="user.username" :label="`${user.displayName || user.username}${user.displayName ? ' · '+user.username : ''}`" :disabled="user.enabled===false"/></ui-select></template>
