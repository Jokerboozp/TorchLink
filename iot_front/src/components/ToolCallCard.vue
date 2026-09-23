<script setup>
import { toolName } from '../presentation' /* 引入当前代码需要的依赖。 */
import { computed, ref } from 'vue' /* 引入当前代码需要的依赖。 */

const props = defineProps({ /* 声明 props。 */
  tool: { type:Object, required:true } /* 执行当前语句并推进处理流程。 */
}) /* 结束当前表达式或代码块。 */

const expanded = ref(false) /* 声明 expanded。 */
const statusMeta = computed(() => ({ /* 声明 statusMeta。 */
  running: { label:'执行中', type:'warning' }, /* 执行当前语句并推进处理流程。 */
  succeeded: { label:'已完成', type:'success' }, /* 执行当前语句并推进处理流程。 */
  failed: { label:'失败', type:'danger' }, /* 执行当前语句并推进处理流程。 */
  canceled: { label:'已停止', type:'info' } /* 执行当前语句并推进处理流程。 */
}[props.tool.status] || { label:'等待中', type:'info' })) /* 结束当前表达式或代码块。 */
const hasDetails = computed(() => Boolean(props.tool.inputSummary || props.tool.outputSummary || props.tool.error)) /* 声明 hasDetails。 */

function safeSummary(value) { /* 定义 safeSummary 函数。 */
  if (value == null) return '' /* 判断条件并选择处理分支。 */
  const text = typeof value === 'string' ? value : JSON.stringify(value, null, 2) /* 声明 text。 */
  return text.length > 1600 ? `${text.slice(0, 1600)}\n…` : text /* 返回当前处理结果。 */
} /* 结束当前表达式或代码块。 */
</script>

<template>
  <article class="tool-card" :class="`is-${tool.status || 'pending'}`"> <!-- 渲染 article 界面元素。 -->
    <header> <!-- 渲染 header 界面元素。 -->
      <span class="tool-icon">工具</span> <!-- 渲染 span 界面元素。 -->
      <div class="tool-title"> <!-- 渲染 div 界面元素。 -->
        <small>工具调用</small> <!-- 渲染 small 界面元素。 -->
        <strong>{{ toolName(tool.name) }}</strong> <!-- 渲染 strong 界面元素。 -->
      </div> <!-- 结束当前界面区域。 -->
      <ui-tag :type="statusMeta.type" size="small" effect="light">{{ statusMeta.label }}</ui-tag> <!-- 渲染 ui-tag 界面元素。 -->
    </header> <!-- 结束当前界面区域。 -->
    <div class="tool-meta"> <!-- 渲染 div 界面元素。 -->
      <span v-if="tool.toolCallId">标识 · {{ tool.toolCallId }}</span> <!-- 渲染 span 界面元素。 -->
      <span v-if="tool.durationMs != null">{{ tool.durationMs }} 毫秒</span> <!-- 渲染 span 界面元素。 -->
      <ui-button v-if="hasDetails" plain size="small" @click="expanded=!expanded">{{ expanded ? '收起详情' : '查看详情' }}</ui-button> <!-- 渲染 ui-button 界面元素。 -->
    </div> <!-- 结束当前界面区域。 -->
    <div v-if="expanded" class="tool-details"> <!-- 渲染 div 界面元素。 -->
      <section v-if="tool.inputSummary"><strong>输入摘要</strong><pre>{{ safeSummary(tool.inputSummary) }}</pre></section> <!-- 渲染 section 界面元素。 -->
      <section v-if="tool.outputSummary"><strong>输出摘要</strong><pre>{{ safeSummary(tool.outputSummary) }}</pre></section> <!-- 渲染 section 界面元素。 -->
      <ui-alert v-if="tool.error" :title="safeSummary(tool.error)" type="error" :closable="false" show-icon /> <!-- 渲染 ui-alert 界面元素。 -->
    </div> <!-- 结束当前界面区域。 -->
  </article> <!-- 结束当前界面区域。 -->
</template>

<style scoped>
.tool-card { margin-top:10px; padding:10px 11px; background:#fafafa; border:1px solid #e8e8e8; border-left:3px solid #d9d9d9; border-radius:4px; } /* 定义当前元素的样式规则。 */
.tool-card.is-running { border-left-color:#faad14; }.tool-card.is-succeeded { border-left-color:#52c41a; }.tool-card.is-failed { border-left-color:#ff4d4f; } /* 定义当前元素的样式规则。 */
.tool-card header { display:flex; align-items:center; gap:9px; }.tool-icon { width:25px; height:25px; flex:0 0 25px; display:grid; place-items:center; color:#1677ff; background:#e6f4ff; border-radius:4px; font-size:12px; font-weight:800; } /* 定义当前元素的样式规则。 */
.tool-title { min-width:0; flex:1; display:grid; gap:1px; }.tool-title small { color:#8c8c8c; font-size:12px; letter-spacing:.08em; }.tool-title strong { overflow:hidden; font-size:13px; text-overflow:ellipsis; white-space:nowrap; } /* 定义当前元素的样式规则。 */
.tool-meta { min-height:20px; margin-top:7px; padding-left:34px; display:flex; align-items:center; gap:10px; color:#8c8c8c; font-size:12px; }.tool-meta .el-button { margin-left:auto; padding:0; font-size:12px; } /* 定义当前元素的样式规则。 */
.tool-details { margin:7px 0 0 34px; display:grid; gap:8px; }.tool-details section { display:grid; gap:4px; }.tool-details strong { color:#646c73; font-size:12px; }.tool-details pre { max-height:180px; margin:0; padding:8px; overflow:auto; color:#3d3d3d; background:#fff; border:1px solid #ededed; border-radius:3px; font:12px/1.6 ui-monospace,SFMono-Regular,Menlo,monospace; white-space:pre-wrap; word-break:break-word; } /* 定义当前元素的样式规则。 */
.tool-meta .el-button { min-height:24px; height:24px; padding:0 8px; } /* 定义当前元素的样式规则。 */
</style>
