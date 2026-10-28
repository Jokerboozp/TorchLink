<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { api } from '../api'
import { alarmLevels, dispositionResults } from '../labels'

const stats = ref(null)
const days = ref(90)
const promptVersion = ref('')
const versions = ref([])
const loading = ref(false)
const error = ref('')
const levels = Object.keys(alarmLevels)
const results = Object.keys(dispositionResults)
let request

const rows = computed(() =>
  levels.filter(level => stats.value?.matrix?.[level]).map(level => ({ level, counts: stats.value.matrix[level] }))
)
const rate = computed(() => (stats.value?.comparable ? `${Math.round(stats.value.agreementRate * 1000) / 10}%` : '—'))

async function load() {
  request?.abort()
  request = new AbortController()
  loading.value = true
  try {
    const params = new URLSearchParams({ start: String(Date.now() - days.value * 86400000) })
    if (promptVersion.value) params.set('promptVersion', promptVersion.value)
    const result = await api(`/api/v1/alarms/statistics/ai-analysis?${params}`, { signal: request.signal })
    stats.value = result.stats
    if (!promptVersion.value)
      versions.value = Object.keys(result.stats?.byPromptVersion || {})
        .filter(Boolean)
        .sort()
    error.value = ''
  } catch (e) {
    if (e.name !== 'AbortError') error.value = e.message || '研判质量统计读取失败'
  } finally {
    loading.value = false
  }
}
onMounted(load)
onUnmounted(() => request?.abort())
</script>

<template>
  <ui-card shadow="never" class="surface-card ai-quality">
    <template #header
      ><div class="quality-header">
        <div><strong>AI 研判质量</strong><small>按告警核实结论对照当时的 AI 风险等级 · 仅统计有权查看的设备</small></div>
        <ui-button size="small" :loading="loading" @click="load">刷新</ui-button>
      </div></template
    >
    <div class="quality-filters">
      <ui-select v-model="days" aria-label="统计范围" @change="load"
        ><ui-option :value="30" label="最近 30 天" /><ui-option :value="90" label="最近 90 天" /><ui-option :value="365" label="最近一年"
      /></ui-select>
      <ui-select v-model="promptVersion" clearable placeholder="全部提示词版本" aria-label="提示词版本" @change="load"
        ><ui-option v-for="item in versions" :key="item" :label="item" :value="item"
      /></ui-select>
    </div>
    <ui-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
    <template v-else-if="stats">
      <div class="quality-figures">
        <div>
          <span>研判与核实一致率</span><strong>{{ rate }}</strong
          ><small>{{ stats.agreement }} / {{ stats.comparable }} 条可比较</small>
        </div>
        <div>
          <span>已核实告警</span><strong>{{ stats.verified }}</strong
          ><small>其中 {{ stats.analyzed }} 条核实前有研判</small>
        </div>
        <div :class="{ warn: stats.missedFires > 0 }">
          <span>漏判真实火警</span><strong>{{ stats.missedFires }}</strong
          ><small>研判为低或提示但核实为真实火警</small>
        </div>
      </div>
      <p class="quality-note">一致：紧急或高风险对应真实火警，低或提示对应非火警；中风险不计入一致率。</p>
      <div v-if="rows.length" class="quality-matrix">
        <table>
          <thead>
            <tr>
              <th>AI 风险等级 \ 核实结论</th>
              <th v-for="result in results" :key="result">{{ dispositionResults[result] }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in rows" :key="row.level">
              <th>{{ alarmLevels[row.level] }}</th>
              <td v-for="result in results" :key="result">{{ row.counts[result] || 0 }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <ui-empty v-else description="所选范围内没有核实前已研判的告警" :image-size="56" />
    </template>
  </ui-card>
</template>

<style scoped>
.quality-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.quality-header > div {
  display: grid;
  gap: 4px;
  min-width: 0;
}
.quality-header small,
.quality-figures small,
.quality-note {
  color: var(--text-muted);
  font-size: 12px;
}
.quality-filters {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 12px;
}
.quality-filters > * {
  width: 170px;
}
.quality-figures {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 10px;
}
.quality-figures > div {
  display: grid;
  gap: 4px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
}
.quality-figures span {
  font-size: 13px;
}
.quality-figures strong {
  font-size: 20px;
}
.quality-figures .warn strong {
  color: var(--danger-text);
}
.quality-note {
  margin: 10px 0;
}
.quality-matrix {
  max-width: 100%;
  overflow-x: auto;
}
.quality-matrix table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}
.quality-matrix th,
.quality-matrix td {
  padding: 6px 10px;
  border: 1px solid var(--border);
  text-align: center;
  white-space: nowrap;
}
.quality-matrix thead th,
.quality-matrix tbody th {
  background: var(--surface);
  font-weight: 600;
}
.ai-quality {
  min-width: 0;
}
</style>
