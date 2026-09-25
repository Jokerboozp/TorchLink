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

<style>
/* 渲染后的 Markdown 由 v-html 注入，样式以 markdown- 前缀限定在本组件。 */
.markdown-content { color: inherit; white-space: normal; overflow-wrap: anywhere; }
.markdown-content > :first-child { margin-top: 0; }
.markdown-content > :last-child { margin-bottom: 0; }
.markdown-content p { margin: 0 0 10px; white-space: normal; }
.markdown-content h1,
.markdown-content h2,
.markdown-content h3,
.markdown-content h4,
.markdown-content h5,
.markdown-content h6 { margin: 13px 0 7px; color: inherit; font-weight: var(--font-weight-semibold); line-height: var(--line-height-tight); }
.markdown-content h1 { font-size: 18px; }
.markdown-content h2 { font-size: var(--font-size-lg); }
.markdown-content h3 { font-size: var(--font-size-md); }
.markdown-content h4,
.markdown-content h5,
.markdown-content h6 { font-size: var(--font-size-sm); }
.markdown-content ul,
.markdown-content ol { margin: 7px 0 10px; padding-left: 22px; }
.markdown-content li { margin: 3px 0; }
.markdown-content blockquote { margin: 9px 0; padding: var(--space-1) var(--space-3); color: var(--text-muted); background: var(--info-soft); border-left: 3px solid var(--info-border); }
.markdown-content pre { margin: 10px 0; white-space: pre-wrap; }
.markdown-content a { color: var(--primary-text); text-decoration: underline; }
.markdown-content hr { margin: var(--space-3) 0; border: 0; border-top: 1px solid var(--border); }
.markdown-table-wrap { max-width: 100%; margin: 10px 0; overflow-x: auto; }
.markdown-content table { width: 100%; border-collapse: collapse; font-size: 0.95em; }
.markdown-content th,
.markdown-content td { padding: 6px var(--space-2); text-align: left; vertical-align: top; border: 1px solid var(--border); }
.markdown-content th { font-weight: var(--font-weight-semibold); background: var(--surface-muted); }
</style>
