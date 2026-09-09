<script setup>
import { computed } from 'vue'
import ToolCallCard from './ToolCallCard.vue'

const props = defineProps({
  modelValue: { type:Boolean, default:false },
  run: { type:Object, default:null }
})
const emit = defineEmits(['update:modelValue'])

const statusMeta = computed(() => ({
  running: { label:'运行中', type:'warning' },
  succeeded: { label:'已完成', type:'success' },
  failed: { label:'失败', type:'danger' },
  canceled: { label:'已停止', type:'info' }
}[props.run?.status] || { label:'等待中', type:'info' }))

function formatClock(value) {
  if (!value) return '—'
  const date = new Date(Number(value))
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleTimeString('zh-CN', { hour12:false })
}
</script>

<template>
  <el-drawer :model-value="modelValue" size="min(520px, 94vw)" destroy-on-close @update:model-value="emit('update:modelValue',$event)">
    <template #header>
      <div class="drawer-heading">
        <span class="section-kicker">执行过程</span>
        <strong>运行轨迹</strong>
        <small>只展示服务端返回的摘要，不呈现工具原始敏感载荷。</small>
      </div>
    </template>

    <div v-if="run" class="trace-body">
      <section class="run-summary">
        <div><small>状态</small><el-tag :type="statusMeta.type" effect="light">{{ statusMeta.label }}</el-tag></div>
        <div><small>工作流</small><strong>{{ run.workflowName || run.workflowId || '默认工作流' }}</strong></div>
        <div><small>模型</small><strong>{{ [run.provider,run.model].filter(Boolean).join(' / ') || '由服务端选择' }}</strong></div>
        <div><small>耗时</small><strong>{{ run.durationMs != null ? `${run.durationMs} 毫秒` : '—' }}</strong></div>
      </section>

      <div class="trace-identifiers">
        <span v-if="run.runId">执行编号 · {{ run.runId }}</span>
        <span v-if="run.traceId">追踪编号 · {{ run.traceId }}</span>
      </div>

      <el-alert v-if="run.error" class="run-error" :title="run.error.message || '运行失败'" :description="[run.error.code,run.error.stage].filter(Boolean).join(' · ')" type="error" :closable="false" show-icon />

      <section class="trace-section">
        <div class="section-title"><strong>事件时间线</strong><span>{{ run.events?.length || 0 }} 个事件</span></div>
        <el-empty v-if="!run.events?.length" description="运行事件尚未到达" :image-size="64" />
        <ol v-else class="trace-list">
          <li v-for="event in run.events" :key="event.id" :class="`is-${event.status || 'info'}`">
            <i />
            <div><strong>{{ event.label }}</strong><small>{{ formatClock(event.createdAt) }}</small><p v-if="event.detail">{{ event.detail }}</p></div>
          </li>
        </ol>
      </section>

      <section v-if="run.tools?.length" class="trace-section">
        <div class="section-title"><strong>工具调用</strong><span>{{ run.tools.length }} 次</span></div>
        <ToolCallCard v-for="tool in run.tools" :key="tool.id || tool.toolCallId" :tool="tool" />
      </section>
    </div>
    <el-empty v-else description="选择一条智能消息查看运行轨迹" />
  </el-drawer>
</template>

<style scoped>
.drawer-heading { display:grid; gap:3px; }.drawer-heading strong { color:#1f1f1f; font-size:17px; }.drawer-heading small { color:#8c8c8c; font-size:11px; }.section-kicker { color:#1677ff; font-size:9px; font-weight:700; letter-spacing:.14em; }
.trace-body { display:grid; gap:18px; }.run-summary { padding:13px; display:grid; grid-template-columns:repeat(2,minmax(0,1fr)); gap:13px; background:#fafafa; border:1px solid #e8e8e8; border-radius:4px; }.run-summary>div { min-width:0; display:grid; gap:4px; }.run-summary small { color:#8c8c8c; font-size:10px; }.run-summary strong { overflow:hidden; color:#3d3d3d; font-size:12px; text-overflow:ellipsis; white-space:nowrap; }
.trace-identifiers { display:grid; gap:4px; color:#8c8c8c; font:10px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace; word-break:break-all; }.run-error { margin-top:-4px; }.trace-section { display:grid; gap:9px; }.section-title { display:flex; align-items:center; justify-content:space-between; padding-bottom:8px; border-bottom:1px solid #f0f0f0; }.section-title strong { font-size:13px; }.section-title span { color:#8c8c8c; font-size:10px; }
.trace-list { margin:0; padding:0; list-style:none; }.trace-list li { position:relative; min-height:52px; padding:0 0 14px 23px; }.trace-list li:not(:last-child)::before { content:''; position:absolute; left:5px; top:12px; bottom:-2px; width:1px; background:#e5e5e5; }.trace-list i { position:absolute; left:0; top:4px; width:11px; height:11px; background:#fff; border:3px solid #8c8c8c; border-radius:50%; }.trace-list .is-success i { border-color:#52c41a; }.trace-list .is-running i { border-color:#faad14; }.trace-list .is-danger i { border-color:#ff4d4f; }.trace-list li>div { display:grid; gap:3px; }.trace-list strong { color:#3d3d3d; font-size:12px; }.trace-list small { color:#8c8c8c; font-size:10px; }.trace-list p { margin:2px 0 0; color:#646c73; font-size:11px; line-height:1.55; }
@media (max-width:480px) { .run-summary { grid-template-columns:1fr; } }
</style>
