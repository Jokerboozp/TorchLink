<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api } from '../api'
import { can } from '../permissions'
import { UiMessage, UiMessageBox } from '../ui/feedback.js'

const items = ref([])
const loading = ref(false)
const error = ref('')
const stopping = ref(new Set())
const now = ref(Date.now())
const canStop = computed(() => can('POST /api/v1/ai/runs/:id/stop'))
const labels = { queued:'排队中', starting:'启动中', running:'运行中', stopping:'正在停止', stop_failed:'停止失败' }
let disposed = false
let request

async function loadRuns() {
  if (loading.value || disposed) return
  loading.value = true
  request = new AbortController()
  try {
    const result = await api('/api/v1/ai/runs', { signal:request.signal })
    if (disposed) return
    items.value = result.items || []
    now.value = Date.now()
    error.value = ''
  } catch (e) {
    if (!disposed && e.name !== 'AbortError') error.value = e.message || '运行列表读取失败'
  } finally { loading.value = false }
}

async function stopRun(row) {
  if (!canStop.value || stopping.value.has(row.runId) || ['stopping','stop_failed'].includes(row.status)) return
  try {
    await UiMessageBox.confirm(`强制停止“${row.workflowName || row.workflowId}”会中断当前执行，尚未完成的结果不会生成。确定停止吗？`, '强制停止工作流', {type:'warning',confirmButtonText:'强制停止',cancelButtonText:'取消'})
  } catch { return }
  if (disposed) return
  stopping.value = new Set([...stopping.value, row.runId])
  try {
    await api(`/api/v1/ai/runs/${encodeURIComponent(row.runId)}/stop`, {method:'POST'})
    row.status = 'stopping'
    UiMessage.success('停止请求已提交，请点击刷新列表查看最终状态')
    await loadRuns()
  } catch (e) {
    if (e.status === 404) { UiMessage.info('该工作流已结束'); await loadRuns() }
    else UiMessage.error(e.message || '停止请求失败，请刷新状态后重试')
  } finally {
    const pending = new Set(stopping.value)
    pending.delete(row.runId)
    stopping.value = pending
  }
}

function elapsed(row) {
  const seconds = Math.max(0, Math.floor((now.value - row.startedAt) / 1000))
  return seconds < 60 ? `${seconds} 秒` : `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`
}
onMounted(loadRuns)
onUnmounted(() => { disposed = true; request?.abort() })
</script>

<template>
  <ui-card shadow="never" class="surface-card workflow-runs">
    <template #header><div class="runs-header"><div><strong>运行中的 AI 工作流</strong><small>当前租户的任务 · 手动刷新 · {{ items.length }} 个任务</small></div><ui-button size="small" :loading="loading" @click="loadRuns">刷新列表</ui-button></div></template>
    <p class="runs-note">切换模型前，请等待任务结束或手动停止。停止操作仅影响所选任务，已经执行的业务操作不会撤销。</p>
    <ui-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
    <ui-alert v-if="items.some(row => row.status === 'stop_failed')" title="部分任务无法确认进程退出，请联系管理员重启 Harness 服务；并发名额仍保留。" type="error" :closable="false" show-icon />
    <ui-table v-if="items.length" :data="items" stripe row-key="runId">
      <ui-table-column label="工作流" min-width="190"><template #default="{row}"><div class="run-title"><strong>{{ row.workflowName || row.workflowId }}</strong><small>{{ row.runId }}</small></div></template></ui-table-column>
      <ui-table-column prop="actor" label="发起人" min-width="110" />
      <ui-table-column prop="model" label="模型" min-width="145" />
      <ui-table-column label="已用时间" min-width="100"><template #default="{row}">{{ elapsed(row) }}</template></ui-table-column>
      <ui-table-column label="状态" min-width="100"><template #default="{row}"><ui-tag :type="row.status === 'stopping' ? 'warning' : 'primary'" size="small">{{ labels[row.status] || row.status }}</ui-tag></template></ui-table-column>
      <ui-table-column v-if="canStop" label="操作" width="130" fixed="right"><template #default="{row}"><ui-button type="danger" plain size="small" :loading="stopping.has(row.runId)" :disabled="Boolean(error) || ['stopping','stop_failed'].includes(row.status)" @click="stopRun(row)">{{ ['stopping','stop_failed'].includes(row.status) ? labels[row.status] : '强制停止' }}</ui-button></template></ui-table-column>
    </ui-table>
    <ui-empty v-else-if="!error && !loading" description="当前没有运行中的 AI 工作流" :image-size="56" />
    <p v-else-if="loading && !items.length" class="runs-note">正在读取运行状态…</p>
  </ui-card>
</template>

<style scoped>
.runs-header { display:flex; align-items:center; justify-content:space-between; gap:12px; }
.runs-header>div,.run-title { display:grid; gap:5px; min-width:0; }
.runs-header small,.run-title small,.runs-note { color:var(--text-muted); font-size:12px; }
.run-title small { overflow-wrap:anywhere; }
.runs-note { margin:0 0 14px; line-height:1.6; }
.workflow-runs { min-width:0; }
</style>
