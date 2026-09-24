<script setup>
import {computed} from 'vue' /* 引入当前代码需要的依赖。 */
const props=defineProps({modelValue:{type:Array,default:()=>[]},catalog:{type:Array,default:()=>[]}}) /* 声明 props。 */
const emit=defineEmits(['update:modelValue']) /* 声明 emit。 */
const groups=computed(()=>props.catalog.filter(x=>x.kind==='menu').map(menu=>({...menu,actions:props.catalog.filter(x=>x.kind==='action'&&x.menu===menu.menu)}))) /* 声明 groups。 */
function toggle(id,checked){emit('update:modelValue',checked?[...new Set([...props.modelValue,id])]:props.modelValue.filter(x=>x!==id))} /* 定义 toggle 函数。 */
</script>
<template><div class="permission-picker"><p class="muted-text">勾选菜单可查看该功能；勾选操作可执行对应动作。执行操作时须同时拥有所属菜单权限。</p><div v-for="group in groups" :key="group.id" class="permission-group"><ui-checkbox :model-value="modelValue.includes(group.id)" @change="v=>toggle(group.id,v)"><strong>{{group.name}}</strong></ui-checkbox><div class="permission-actions"><ui-checkbox v-for="action in group.actions" :key="action.id" :model-value="modelValue.includes(action.id)" @change="v=>toggle(action.id,v)"><ui-tooltip :content="action.id"><span>{{action.name}}</span></ui-tooltip></ui-checkbox></div></div></div></template>
<style scoped>.permission-picker{width:100%;}.permission-group{padding:10px 0;border-bottom:1px solid var(--border)}.permission-actions{padding-left:24px;display:flex;flex-wrap:wrap;gap:4px 14px}.permission-actions .el-checkbox{margin-right:0}</style>
