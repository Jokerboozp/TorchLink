<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api, formatTime } from '../api'
import { formatCount } from '../format'

const statusLabels = { SUCCEEDED: '成功', FAILED: '失败', STOPPED: '已停止', TIMEOUT: '超时' }
const statusTypes = { SUCCEEDED: 'success', FAILED: 'danger', STOPPED: 'warning', TIMEOUT: 'warning' }
const workflowLabels = {
  'alarm-handler': '告警研判',
  'device-health-inspector': '设备巡检',
  'ops-assistant': '运维助手',
  'protocol-assistant': '协议助手',
  'rule-drafter': '规则草稿',
  'system-observer': '系统状态助手'
}
const items = ref([])
const usage = ref([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const filters = ref({ workflowId: '', status: '', days: 30 })
const loading = ref(false)
const error = ref('')
const unavailable = ref(false)
let request

const workflowOptions = computed(() => {
  const ids = new Set(Object.keys(workflowLabels))
  for (const row of usage.value) ids.add(row.workflowId)
  return [...ids].map(id => ({ id, label: workflowName(id) }))
})
// Totals per workflow over the selected period, largest token use first.
const usageTotals = computed(() => {
  const byWorkflow = new Map()
  for (const row of usage.value) {
    const sum = byWorkflow.get(row.workflowId) || {
      workflowId: row.workflowId,
      runs: 0,
      failed: 0,
      input: 0,
      output: 0,
      cacheRead: 0,
      toolCalls: 0,
      durationMs: 0
    }
    sum.runs += row.runs
    sum.failed += row.failed
    sum.toolCalls += row.toolCalls
    sum.durationMs += row.durationMs
    sum.input += row.usage?.inputTokens || 0
    sum.output += row.usage?.outputTokens || 0
    sum.cacheRead += row.usage?.cacheReadTokens || 0
    byWorkflow.set(row.workflowId, sum)
  }
  return [...byWorkflow.values()].sort((a, b) => b.input + b.output - (a.input + a.output))
})

function workflowName(id) {
  return workflowLabels[id] || id
}
function tokens(value) {
  return formatCount(value)
}
function seconds(ms) {
  return ms >= 60000 ? `${Math.floor(ms / 60000)} 分 ${Math.round((ms % 60000) / 1000)} 秒` : `${(ms / 1000).toFixed(1)} 秒`
}
function query() {
  const params = new URLSearchParams({
    page: String(page.value),
    pageSize: String(pageSize.value),
    start: String(Date.now() - filters.value.days * 86400000)
  })
  if (filters.value.workflowId) params.set('workflowId', filters.value.workflowId)
  if (filters.value.status) params.set('status', filters.value.status)
  return params.toString()
}

async function load() {
  request?.abort()
  request = new AbortController()
  loading.value = true
  try {
    const q = query()
    const [history, sums] = await Promise.all([
      api(`/api/v1/ai/runs/history?${q}`, { signal: request.signal }),
      api(`/api/v1/ai/runs/usage?${q}`, { signal: request.signal })
    ])
    items.value = history.items || []
    total.value = history.total || 0
    usage.value = sums.items || []
    unavailable.value = history.available === false
    error.value = ''
  } catch (e) {
    if (e.name !== 'AbortError') error.value = e.message || 'AI 运行记录读取失败'
  } finally {
    loading.value = false
  }
}
function filter() {
  page.value = 1
  load()
}
onMounted(load)
onUnmounted(() => request?.abort())
</script>

<template>
  <ui-card shadow="never" class="surface-card ai-run-history">
    <template #header
      ><div class="history-header">
        <div><strong>AI 运行记录与用量</strong><small>已结束的工作流运行 · 词元用量由模型服务返回</small></div>
        <ui-button size="small" :loading="loading" @click="load">刷新</ui-button>
      </div></template
    >
    <div class="history-filters">
      <ui-select v-model="filters.days" aria-label="统计范围" @change="filter"
        ><ui-option :value="1" label="最近 24 小时" /><ui-option :value="7" label="最近 7 天" /><ui-option :value="30" label="最近 30 天"
      /></ui-select>
      <ui-select v-model="filters.workflowId" clearable placeholder="全部工作流" aria-label="工作流" @change="filter"
        ><ui-option v-for="item in workflowOptions" :key="item.id" :label="item.label" :value="item.id"
      /></ui-select>
      <ui-select v-model="filters.status" clearable placeholder="全部结果" aria-label="运行结果" @change="filter"
        ><ui-option v-for="(text, key) in statusLabels" :key="key" :label="text" :value="key"
      /></ui-select>
    </div>
    <ui-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
    <ui-alert v-else-if="unavailable" title="当前部署未启用 PostgreSQL，AI 运行记录不会保存。" type="info" :closable="false" show-icon />
    <div v-if="usageTotals.length" class="usage-grid">
      <div v-for="row in usageTotals" :key="row.workflowId" class="usage-item">
        <strong>{{ workflowName(row.workflowId) }}</strong>
        <span
          >{{ row.runs }} 次运行<template v-if="row.failed"> · {{ row.failed }} 次未成功</template></span
        >
        <small
          >输入 {{ tokens(row.input) }} · 输出 {{ tokens(row.output) }} 词元<template v-if="row.cacheRead">
            · 缓存命中 {{ tokens(row.cacheRead) }}</template
          ></small
        >
        <small>平均耗时 {{ seconds(row.durationMs / row.runs) }} · 工具调用 {{ row.toolCalls }} 次</small>
      </div>
    </div>
    <ui-table v-if="items.length" :data="items" stripe row-key="runId">
      <ui-table-column label="开始时间" min-width="160"
        ><template #default="{ row }">{{ formatTime(row.startedAt) }}</template></ui-table-column
      >
      <ui-table-column label="工作流" min-width="150"
        ><template #default="{ row }"
          ><div class="run-title">
            <strong>{{ workflowName(row.workflowId) }}</strong
            ><small>{{ row.promptVersion || '—' }}</small>
          </div></template
        ></ui-table-column
      >
      <ui-table-column prop="actor" label="发起人" min-width="100" />
      <ui-table-column prop="model" label="模型" min-width="140" />
      <ui-table-column label="耗时" min-width="90"
        ><template #default="{ row }">{{ seconds(row.durationMs) }}</template></ui-table-column
      >
      <ui-table-column label="词元（输入 / 输出）" min-width="150"
        ><template #default="{ row }">{{
          row.usageReported ? `${tokens(row.usage.inputTokens)} / ${tokens(row.usage.outputTokens)}` : '未上报'
        }}</template></ui-table-column
      >
      <ui-table-column prop="toolCalls" label="工具调用" min-width="90" />
      <ui-table-column label="结果" min-width="110"
        ><template #default="{ row }"
          ><ui-tag :type="statusTypes[row.status] || 'info'" size="small" :title="row.error">{{
            statusLabels[row.status] || row.status
          }}</ui-tag></template
        ></ui-table-column
      >
    </ui-table>
    <ui-empty v-else-if="!error && !loading" description="所选范围内没有 AI 运行记录" :image-size="56" />
    <ui-pagination
      v-if="total > pageSize"
      v-model:current-page="page"
      v-model:page-size="pageSize"
      :total="total"
      :page-sizes="[20, 50, 100]"
      layout="sizes, prev, pager, next"
      :disabled="loading"
      aria-label="AI 运行记录分页"
      @update:current-page="load"
      @update:page-size="filter"
    />
  </ui-card>
</template>

<style scoped>
.history-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.history-header > div,
.run-title,
.usage-item {
  display: grid;
  gap: 4px;
  min-width: 0;
}
.history-header small,
.run-title small,
.usage-item small {
  color: var(--text-muted);
  font-size: 12px;
}
.history-filters {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 12px;
}
.history-filters > * {
  width: 160px;
}
.usage-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 10px;
  margin-bottom: 14px;
}
.usage-item {
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
}
.usage-item span {
  font-size: 13px;
}
.ai-run-history {
  min-width: 0;
}
</style>
