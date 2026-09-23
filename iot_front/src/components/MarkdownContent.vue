<script setup>
import { computed } from 'vue' /* 引入当前代码需要的依赖。 */
import { renderMarkdown } from '../markdown' /* 引入当前代码需要的依赖。 */

const props = defineProps({ /* 声明 props。 */
  source: { type: [String, Number], default: '' } /* 执行当前语句并推进处理流程。 */
}) /* 结束当前表达式或代码块。 */

const html = computed(() => renderMarkdown(props.source)) /* 声明 html。 */
</script>

<template>
  <div class="markdown-content" v-html="html"></div> <!-- 渲染 div 界面元素。 -->
</template>
