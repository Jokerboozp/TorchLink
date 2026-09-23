<script setup>
import { computed } from 'vue' /* 引入当前代码需要的依赖。 */
import { ElMessage } from 'element-plus' /* 引入当前代码需要的依赖。 */
const props = defineProps({ modelValue: {}, kind: String, label: String }) /* 声明 props。 */
const emit = defineEmits(['update:modelValue']) /* 声明 emit。 */
const entries = computed(() => Object.entries(props.modelValue || {})) /* 声明 entries。 */
const typeOf = value => Array.isArray(value) ? 'array' : value === null ? 'null' : typeof value /* 声明 typeOf。 */
const empty = kind => ({ string:'', number:0, boolean:false, null:null, object:{}, array:[] })[kind] /* 声明 empty。 */
function set(key, value) { /* 定义 set 函数。 */
  if (props.kind === 'array') { const next = [...props.modelValue]; next[Number(key)] = value; emit('update:modelValue', next) } /* 判断条件并选择处理分支。 */
  else emit('update:modelValue', Object.fromEntries([...entries.value.filter(([k]) => k !== key), [key, value]])) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
function rename(key, next) { /* 定义 rename 函数。 */
  if (!next || entries.value.some(([k]) => k !== key && k === next)) return ElMessage.warning('参数名称不能为空或重复') /* 判断条件并选择处理分支。 */
  emit('update:modelValue', Object.fromEntries(entries.value.map(([k,v]) => [k === key ? next : k, v]))) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
function add() { /* 定义 add 函数。 */
  if (props.kind === 'array') emit('update:modelValue', [...(props.modelValue || []), '']) /* 判断条件并选择处理分支。 */
  else { let key = 'field'; let n = 1; while (Object.hasOwn(props.modelValue || {}, key)) key = `field${n++}`; set(key, '') } /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
function remove(key) { emit('update:modelValue', props.kind === 'array' ? props.modelValue.filter((_,i) => i !== Number(key)) : Object.fromEntries(entries.value.filter(([k]) => k !== key))) } /* 定义 remove 函数。 */
</script>

<template>
  <el-input v-if="kind === 'string'" :model-value="modelValue" :aria-label="label" @update:model-value="emit('update:modelValue',$event)" /> <!-- 渲染 el-input 界面元素。 -->
  <el-input-number v-else-if="kind === 'number' || kind === 'integer'" :model-value="modelValue" :precision="kind === 'integer' ? 0 : undefined" :aria-label="label" @update:model-value="emit('update:modelValue',$event)" /> <!-- 渲染 el-input-number 界面元素。 -->
  <el-select v-else-if="kind === 'boolean'" :model-value="modelValue" :aria-label="label" clearable placeholder="请选择" @update:model-value="emit('update:modelValue', $event === '' ? null : $event)"><el-option :value="true" label="是（true）"/><el-option :value="false" label="否（false）"/></el-select> <!-- 渲染 el-select 界面元素。 -->
  <span v-else-if="kind === 'null'">空值</span> <!-- 渲染 span 界面元素。 -->
  <div v-else-if="kind === 'object' || kind === 'array'" class="command-object"> <!-- 渲染 div 界面元素。 -->
    <div v-for="([key,value]) in entries" :key="key" class="command-entry"> <!-- 渲染 div 界面元素。 -->
      <div class="command-entry-heading"><el-input v-if="kind === 'object'" :model-value="key" aria-label="参数名称" @change="rename(key,$event)"/><span v-else>第 {{Number(key)+1}} 项</span><el-select :model-value="typeOf(value)" aria-label="参数类型" @update:model-value="set(key,empty($event))"><el-option v-for="(name,type) in {string:'文本',number:'数值',boolean:'布尔',object:'对象',array:'列表',null:'空值'}" :key="type" :label="name" :value="type"/></el-select><el-button link type="danger" @click="remove(key)">删除</el-button></div> <!-- 渲染 div 界面元素。 -->
      <CommandValueInput :model-value="value" :kind="typeOf(value)" :label="`${label}.${key}`" @update:model-value="set(key,$event)"/> <!-- 渲染 CommandValueInput 界面元素。 -->
    </div> <!-- 结束当前界面区域。 -->
    <el-button size="small" @click="add">{{kind === 'array' ? '添加列表项' : '添加参数项'}}</el-button> <!-- 渲染 el-button 界面元素。 -->
    <el-button v-if="modelValue != null" size="small" @click="emit('update:modelValue',null)">清除</el-button> <!-- 渲染 el-button 界面元素。 -->
    <el-button v-else size="small" @click="emit('update:modelValue',empty(kind))">设为空{{kind === 'array' ? '列表' : '对象'}}</el-button> <!-- 渲染 el-button 界面元素。 -->
  </div> <!-- 结束当前界面区域。 -->
</template>
<style scoped>
.command-object { width:100%; min-width:0; border-left:2px solid #d5dde8; padding-left:12px; } /* 定义当前元素的样式规则。 */
.command-entry { margin-bottom:12px; } /* 定义当前元素的样式规则。 */
.command-entry-heading { display:flex; flex-wrap:wrap; gap:8px; margin-bottom:8px; } /* 定义当前元素的样式规则。 */
.command-entry-heading > .el-input,.command-entry-heading > .el-select { width:140px; } /* 定义当前元素的样式规则。 */
</style>
