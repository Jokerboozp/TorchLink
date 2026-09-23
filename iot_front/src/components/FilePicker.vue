<script setup>
import { ref } from 'vue' /* 引入当前代码需要的依赖。 */
defineProps({ accept: { type:String, default:'' }, disabled: Boolean }) /* 执行当前语句并推进处理流程。 */
const emit = defineEmits(['change']) /* 声明 emit。 */
const filename = ref('') /* 声明 filename。 */
function choose(event) { /* 定义 choose 函数。 */
  filename.value = event.target.files?.[0]?.name || '' /* 更新 filename.value 的值。 */
  emit('change', event) /* 执行当前语句并推进处理流程。 */
} /* 结束当前表达式或代码块。 */
</script>

<template>
  <div class="file-picker" :class="{ 'is-disabled': disabled }"> <!-- 渲染 div 界面元素。 -->
    <label class="file-picker-button"><span>选择文件</span><input type="file" :accept="accept" :disabled="disabled" aria-label="选择文件" @change="choose" /></label> <!-- 渲染 label 界面元素。 -->
    <span class="file-picker-name">{{ filename || '尚未选择文件' }}</span> <!-- 渲染 span 界面元素。 -->
  </div> <!-- 结束当前界面区域。 -->
</template>

<style scoped>
.file-picker { display: flex; align-items: center; gap: 10px; max-width: 100%; min-width: 0; } /* 定义当前元素的样式规则。 */
.file-picker-button { position: relative; display: inline-flex; align-items: center; min-height: 32px; padding: 0 12px; flex: none; border: 1px solid #cbd5e1; border-radius: 4px; color: #334155; background: #fff; font-size: 13px; } /* 定义当前元素的样式规则。 */
.file-picker-button:focus-within { outline: 2px solid var(--primary); outline-offset: 2px; } /* 定义当前元素的样式规则。 */
.file-picker-button input { position: absolute; inset: 0; width: 100%; height: 100%; padding: 0; opacity: 0; cursor: pointer; } /* 定义当前元素的样式规则。 */
.file-picker-name { color: #64748b; font-size: 13px; overflow-wrap: anywhere; } /* 定义当前元素的样式规则。 */
.is-disabled { opacity: .55; }.is-disabled input { cursor: not-allowed; } /* 定义当前元素的样式规则。 */
</style>
