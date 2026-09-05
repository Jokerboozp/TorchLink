<script setup>
import { computed, ref } from 'vue'

const props = defineProps({
  tool: { type:Object, required:true }
})

const expanded = ref(false)
const statusMeta = computed(() => ({
  running: { label:'执行中', type:'warning' },
  succeeded: { label:'已完成', type:'success' },
  failed: { label:'失败', type:'danger' },
  canceled: { label:'已停止', type:'info' }
}[props.tool.status] || { label:'等待中', type:'info' }))
const hasDetails = computed(() => Boolean(props.tool.inputSummary || props.tool.outputSummary || props.tool.error))

function safeSummary(value) {
  if (value == null) return ''
  const text = typeof value === 'string' ? value : JSON.stringify(value, null, 2)
  return text.length > 1600 ? `${text.slice(0, 1600)}\n…` : text
}
</script>

<template>
  <article class="tool-card" :class="`is-${tool.status || 'pending'}`">
    <header>
      <span class="tool-icon">T</span>
      <div class="tool-title">
        <small>工具调用</small>
        <strong>{{ tool.name || '未命名工具' }}</strong>
      </div>
      <el-tag :type="statusMeta.type" size="small" effect="light">{{ statusMeta.label }}</el-tag>
    </header>
    <div class="tool-meta">
      <span v-if="tool.toolCallId">ID · {{ tool.toolCallId }}</span>
      <span v-if="tool.durationMs != null">{{ tool.durationMs }} ms</span>
      <el-button v-if="hasDetails" plain size="small" @click="expanded=!expanded">{{ expanded ? '收起详情' : '查看详情' }}</el-button>
    </div>
    <div v-if="expanded" class="tool-details">
      <section v-if="tool.inputSummary"><strong>输入摘要</strong><pre>{{ safeSummary(tool.inputSummary) }}</pre></section>
      <section v-if="tool.outputSummary"><strong>输出摘要</strong><pre>{{ safeSummary(tool.outputSummary) }}</pre></section>
      <el-alert v-if="tool.error" :title="safeSummary(tool.error)" type="error" :closable="false" show-icon />
    </div>
  </article>
</template>

<style scoped>
.tool-card { margin-top:10px; padding:10px 11px; background:#fafafa; border:1px solid #e8e8e8; border-left:3px solid #d9d9d9; border-radius:4px; }
.tool-card.is-running { border-left-color:#faad14; }.tool-card.is-succeeded { border-left-color:#52c41a; }.tool-card.is-failed { border-left-color:#ff4d4f; }
.tool-card header { display:flex; align-items:center; gap:9px; }.tool-icon { width:25px; height:25px; flex:0 0 25px; display:grid; place-items:center; color:#1677ff; background:#e6f4ff; border-radius:4px; font-size:10px; font-weight:800; }
.tool-title { min-width:0; flex:1; display:grid; gap:1px; }.tool-title small { color:#8c8c8c; font-size:9px; letter-spacing:.08em; }.tool-title strong { overflow:hidden; font-size:12px; text-overflow:ellipsis; white-space:nowrap; }
.tool-meta { min-height:20px; margin-top:7px; padding-left:34px; display:flex; align-items:center; gap:10px; color:#8c8c8c; font-size:10px; }.tool-meta .el-button { margin-left:auto; padding:0; font-size:10px; }
.tool-details { margin:7px 0 0 34px; display:grid; gap:8px; }.tool-details section { display:grid; gap:4px; }.tool-details strong { color:#646c73; font-size:10px; }.tool-details pre { max-height:180px; margin:0; padding:8px; overflow:auto; color:#3d3d3d; background:#fff; border:1px solid #ededed; border-radius:3px; font:10px/1.6 ui-monospace,SFMono-Regular,Menlo,monospace; white-space:pre-wrap; word-break:break-word; }
.tool-meta .el-button { min-height:24px; height:24px; padding:0 8px; }
</style>
